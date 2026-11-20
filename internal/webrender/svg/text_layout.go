package svg

import (
	"math"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/matrix"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	drawText "github.com/polestorm/gopdfgo/internal/webrender/text/draw"
)

// gopdfgo: skład tekstu SVG (SVG 2 § 11.8, algorytm układu tekstu). Źródło rysowało każdy znak
// osobno, więc nie było kerningu, ligatur ani form kontekstowych, szerokości różniły się od
// przeglądarek, a text-anchor przesuwał cały element <text> zamiast każdego fragmentu (text chunk).
//
// Tu cały element <text> jest składany naraz:
//  1. znaki wszystkich potomków (tspan, textPath, a, tekst między nimi) tworzą jeden ciąg znaków
//     adresowalnych, a listy x, y, dx, dy i rotate każdego elementu przypisują wartości jego znakom
//     i znakom potomków (§ 11.8.2);
//  2. każdy kawałek tekstu jednego elementu, do najbliższej pozycji bezwzględnej, jest kształtowany
//     w całości tym samym silnikiem co HTML (font-kerning, font-variant-ligatures,
//     font-feature-settings, letter-spacing, word-spacing);
//  3. pozycje, dx/dy i rotate dostają znaki typograficzne (skupienia z kształtowania: ligatura,
//     znak z akcentami), a wartości dla dalszych znaków skupienia są pomijane (§ 11.8.1);
//  4. textLength, text-anchor (dla każdego fragmentu, § 11.8.3) i textPath (§ 11.10.3) działają
//     na przesunięciach z kształtowania;
//  5. glify jednego elementu leżące na jednej linii bazowej idą do PDF jednym ciągiem (operator TJ
//     z korektami), więc tekst daje się zaznaczać i wyszukiwać, a plik jest mniejszy.

// textLayoutSpan to element tekstu (text, tspan, textPath, a) w składanym elemencie <text>.
type textLayoutSpan struct {
	span     *textSpan
	fontSize Fl
	// znaki własne [start, end) i znaki całego poddrzewa [start, subEnd)
	start, end, subEnd int
	path               int // indeks ścieżki (textPath) albo -1
	yAlign             Fl  // przesunięcie linii bazowej (dominant-baseline, display-anchor)
}

// textSegment to kawałek tekstu kształtowany w całości.
type textSegment struct {
	span       int // indeks w spans
	start, end int // znaki [start, end)
	drawing    backend.TextDrawing
	baseline   Fl // odległość linii bazowej od góry pola wiersza
	height     Fl
}

// textGlyph to glif ukształtowanego kawałka.
type textGlyph struct {
	segment  int
	run      int // indeks przebiegu w segment.drawing albo -1, gdy glifu się nie rysuje
	glyph    backend.TextGlyph
	advance  Fl // przesunięcie z kształtowania, w jednostkach użytkownika
	inOffset Fl // odległość pióra przed glifem od pióra przed skupieniem
	// kerningBase to Kerning minus Offset glifu z rysunku silnika (w tysięcznych rozmiaru przebiegu):
	// różnica szerokości glifu w foncie i przesunięcia z kształtowania
	kerningBase Fl
}

// textCluster to znak typograficzny: glify jednego skupienia z kształtowania.
type textCluster struct {
	start   int // pierwszy znak (adresowalny)
	glyphs  [2]int
	advance Fl // od początku skupienia (x) do pióra za nim
	// lead to przesunięcie poziome pierwszego glifu względem pióra (połowa letter-spacing przed
	// znakiem, XOffset z GPOS): wchodzi do pozycji skupienia, aby obrót (rotate, textPath) działał
	// wokół początku glifu, a nie pióra
	lead            Fl
	segment         int
	x, y            Fl
	angle           Fl // stopnie
	scaleX          Fl
	chunk           int
	rotated, hidden bool
}

// textPathGeometry to ścieżka textPath spłaszczona do łamanej.
type textPathGeometry struct {
	points []point
	// lengths[i] to długość łamanej do punktu i; moves[i] mówi, że do punktu i prowadzi skok
	lengths []Fl
	moves   []bool
	offset  Fl // startOffset
}

func (g *textPathGeometry) length() Fl {
	if len(g.lengths) == 0 {
		return 0
	}
	return g.lengths[len(g.lengths)-1]
}

// at zwraca punkt i kąt stycznej (radiany) w odległości d od początku ścieżki.
func (g *textPathGeometry) at(d Fl) (point, Fl, bool) {
	if len(g.points) < 2 || d < 0 || d > g.length() {
		return point{}, 0, false
	}
	for i := 1; i < len(g.points); i++ {
		segLen := g.lengths[i] - g.lengths[i-1]
		if g.moves[i] || segLen <= 0 || d > g.lengths[i] {
			continue
		}
		p0, p1 := g.points[i-1], g.points[i]
		t := (d - g.lengths[i-1]) / segLen
		angle := Fl(math.Atan2(float64(p1.y-p0.y), float64(p1.x-p0.x)))
		return point{p0.x + (p1.x-p0.x)*t, p0.y + (p1.y-p0.y)*t}, angle, true
	}
	return point{}, 0, false
}

func newTextPathGeometry(items []pathItem, mat matrix.Transform) *textPathGeometry {
	out := &textPathGeometry{}
	var current, subpathStart point
	add := func(p point, move bool) {
		x, y := mat.Apply(p.x, p.y)
		q := point{x, y}
		length := Fl(0)
		if n := len(out.points); n != 0 {
			length = out.lengths[n-1]
			if !move {
				prev := out.points[n-1]
				length += Fl(math.Hypot(float64(q.x-prev.x), float64(q.y-prev.y)))
			}
		}
		out.points = append(out.points, q)
		out.lengths = append(out.lengths, length)
		out.moves = append(out.moves, move)
	}
	for _, item := range items {
		switch item.op {
		case moveTo:
			current, subpathStart = item.args[0], item.args[0]
			add(current, true)
		case lineTo:
			current = item.args[0]
			add(current, false)
		case close:
			current = subpathStart
			add(current, false)
		case cubicTo:
			const steps = 32
			p0, p1, p2, p3 := current, item.args[0], item.args[1], item.args[2]
			for i := 1; i <= steps; i++ {
				t := Fl(i) / steps
				mt := 1 - t
				a, b, c, d := mt*mt*mt, 3*mt*mt*t, 3*mt*t*t, t*t*t
				add(point{
					a*p0.x + b*p1.x + c*p2.x + d*p3.x,
					a*p0.y + b*p1.y + c*p2.y + d*p3.y,
				}, false)
			}
			current = p3
		}
	}
	return out
}

// textPathFor zwraca geometrię ścieżki elementu textPath albo nil.
func (svg *SVGImage) textPathFor(span *textSpan, dims drawingDims) *textPathGeometry {
	var geometry *textPathGeometry
	if span.pathData != nil {
		geometry = newTextPathGeometry(span.pathData, matrix.Identity())
	} else if node := svg.definitions.nodes[span.pathHref]; node != nil {
		p, ok := node.graphicContent.(path)
		if !ok {
			return nil
		}
		// przekształcenie elementu path działa dodatkowo w układzie tekstu (SVG 2 § 11.10.3)
		geometry = newTextPathGeometry(p, aggregateTransforms(node.attributes.transforms, dims.fontSize, dims.innerDiagonal))
	}
	if geometry == nil || len(geometry.points) < 2 {
		return nil
	}
	geometry.offset = span.startOffset.Resolve(dims.fontSize, geometry.length())
	return geometry
}

// layoutText składa element <text> z potomkami i zapisuje ciągi glifów w ich textSpan.
func (svg *SVGImage) layoutText(dst backend.Canvas, root *svgNode, dims drawingDims) {
	var (
		spans []textLayoutSpan
		chars []rune
		owner []int // indeks elementu dla każdego znaku
		paths []*textPathGeometry
	)
	var collect func(node *svgNode, fontSize Fl, path int, isRoot bool)
	collect = func(node *svgNode, fontSize Fl, path int, isRoot bool) {
		span, ok := node.graphicContent.(*textSpan)
		if !ok || !node.attributes.display {
			return
		}
		span.drawings = nil
		span.textBoundingBox = emptyBbox
		if !isRoot {
			fontSize = node.attributes.fontSize.Resolve(fontSize, fontSize)
		}
		if span.isTextPath && path == -1 {
			path = -2 // textPath bez poprawnej ścieżki nie jest rysowany (SVG 2 § 11.10.3)
			if geometry := svg.textPathFor(span, dims); geometry != nil {
				paths = append(paths, geometry)
				path = len(paths) - 1
			}
		}
		index := len(spans)
		spans = append(spans, textLayoutSpan{span: span, fontSize: fontSize, start: len(chars), path: path})
		for _, r := range span.text {
			chars = append(chars, r)
			owner = append(owner, index)
		}
		spans[index].end = len(chars)
		for _, child := range node.children {
			collect(child, fontSize, path, false)
		}
		spans[index].subEnd = len(chars)
	}
	collect(root, dims.fontSize, -1, true)
	if len(chars) == 0 || svg.textContext == nil {
		return
	}

	// 1. wartości list x, y, dx, dy i rotate dla znaków (SVG 2 § 11.8.2, rozwiązywanie pozycji)
	n := len(chars)
	nan := Fl(math.NaN())
	xs, ys, dxs, dys, rotates := make([]Fl, n), make([]Fl, n), make([]Fl, n), make([]Fl, n), make([]Fl, n)
	for i := range xs {
		xs[i], ys[i], rotates[i] = nan, nan, nan
	}
	for _, s := range spans {
		t := s.span
		set := func(dst []Fl, values []Value, reference Fl) {
			for k, v := range values {
				if s.start+k < s.subEnd {
					dst[s.start+k] = v.Resolve(s.fontSize, reference)
				}
			}
		}
		set(xs, t.x, dims.innerWidth)
		set(ys, t.y, dims.innerHeight)
		set(dxs, t.dx, dims.innerWidth)
		set(dys, t.dy, dims.innerHeight)
		if L := len(t.rotate); L != 0 {
			for i := s.start; i < s.subEnd; i++ {
				if k := i - s.start; k < L {
					rotates[i] = t.rotate[k]
				} else {
					rotates[i] = t.rotate[L-1]
				}
			}
		}
	}
	absolute := func(i int) bool {
		return !isClusterContinuation(chars[i]) && spans[owner[i]].path == -1 && (!isNaN(xs[i]) || !isNaN(ys[i]))
	}

	// 2. kształtowanie kawałków: jeden element, do następnej pozycji bezwzględnej
	var (
		segments []textSegment
		glyphs   []textGlyph
		clusters []textCluster
	)
	fonts := svg.textContext.Fonts()
	drawer := drawText.Context{Output: dst, Fonts: fonts}
	// tekst jest kształtowany w rozmiarze, w jakim trafi na stronę (skala z układu użytkownika do
	// układu gospodarza), a wyniki są przeliczane z powrotem, jak w Chromium (LayoutSVGInlineText,
	// scaled font). Przesunięcia z kształtowania są w 1/64 piksela, więc tekst o rozmiarze 1 w
	// powiększającym viewBox rozjeżdżałby się o ułamki piksela na każdym znaku.
	scale := svg.hostScale(dst) * transformScale(aggregateTransforms(root.attributes.transforms, dims.fontSize, dims.innerDiagonal))
	if !(scale > 0) || math.IsInf(float64(scale), 0) || math.Abs(float64(scale-1)) < 1e-3 {
		scale = 1
	}
	for i := 0; i < n; {
		end := i + 1
		for end < n && owner[end] == owner[i] && !absolute(end) {
			end++
		}
		s := spans[owner[i]]
		style := s.span.style.Copy()
		style.SetFontSize(pr.FToV(s.fontSize * scale))
		// spacje są już przetworzone (handleText), więc skład zachowuje je takie, jakie są
		style.SetWhiteSpace(pr.Pre)
		if s.span.hasLetterSpacing {
			style.SetLetterSpacing(pr.FToPx(pr.Float(s.span.letterSpacing.Resolve(s.fontSize, dims.innerDiagonal) * scale)))
		}
		if s.span.hasWordSpacing {
			style.SetWordSpacing(pr.FToPx(pr.Float(s.span.wordSpacing.Resolve(s.fontSize, dims.innerDiagonal) * scale)))
		}
		segText := chars[i:end]
		line := text.SplitFirstLine(segText, style, svg.textContext, pr.Inf, false, true)
		layout, ok := line.Layout.(*text.TextLayoutGotext)
		if !ok {
			i = end
			continue
		}
		// odstęp między literami za ostatnim znakiem fragmentu (text chunk) znika, jak na końcu
		// wiersza (CSS Text 3 § 7.2); liczy się to dla text-anchor i textLength
		if end == n || absolute(end) || spans[owner[end]].path != s.path {
			layout.TrimTrailingLetterSpacing()
		}
		segment := textSegment{span: owner[i], start: i, end: end, baseline: Fl(line.Baseline) / scale, height: Fl(line.Height) / scale}
		segment.drawing = drawer.CreateFirstLine(layout, "none", pr.TaggedString{Tag: pr.None}, 1, 0, 0, 0)
		segment.drawing.FontSize /= scale
		segIndex := len(segments)

		// glify kształtowania i glify do rysowania idą w tej samej kolejności
		var drawn []struct{ run, index int }
		for r, run := range segment.drawing.Runs {
			for k := range run.Glyphs {
				drawn = append(drawn, struct{ run, index int }{r, k})
			}
		}
		var shapedCount int
		for _, run := range layout.Line {
			shapedCount += len(run.Glyphs)
		}
		canDraw := shapedCount == len(drawn)

		k := 0
		for _, run := range layout.Line {
			for _, g := range run.Glyphs {
				tg := textGlyph{segment: segIndex, run: -1, advance: Fl(g.Advance) / 64 / scale}
				if canDraw {
					tg.run = drawn[k].run
					tg.glyph = segment.drawing.Runs[drawn[k].run].Glyphs[drawn[k].index]
					tg.kerningBase = Fl(tg.glyph.Kerning) - tg.glyph.Offset
				}
				k++
				charIndex := i + g.TextIndex()
				// nowe skupienie zaczyna glif innego znaku, chyba że to znak łączący
				last := len(clusters) - 1
				if last >= 0 && clusters[last].segment == segIndex &&
					(clusters[last].start == charIndex || isClusterContinuation(chars[min(charIndex, n-1)])) {
					c := &clusters[last]
					tg.inOffset = c.lead + c.advance
					c.advance += tg.advance
					c.glyphs[1]++
				} else {
					lead := Fl(g.XOffset) / 64 / scale
					tg.glyph.Offset = 0 // zob. textCluster.lead
					tg.inOffset = lead
					clusters = append(clusters, textCluster{
						start: charIndex, glyphs: [2]int{len(glyphs), len(glyphs) + 1},
						advance: tg.advance - lead, lead: lead, segment: segIndex, scaleX: 1,
					})
				}
				glyphs = append(glyphs, tg)
			}
		}
		segments = append(segments, segment)
		i = end
	}

	// przesunięcie linii bazowej elementu (dominant-baseline, display-anchor), jak w źródle
	for si := range spans {
		s := &spans[si]
		var height Fl
		for _, seg := range segments {
			if seg.span == si {
				height = seg.height
				break
			}
		}
		ascentL, descentL := s.fontSize*.8, s.fontSize*.2
		switch t := s.span; {
		case t.displayAnchor == middle:
			s.yAlign = -height / 2
		case t.displayAnchor == top:
		case t.displayAnchor == bottom:
			s.yAlign = -height
		case t.baseline == central:
			s.yAlign = (ascentL+descentL)/2 - descentL
		case t.baseline == ascent:
			s.yAlign = ascentL
		case t.baseline == descent:
			s.yAlign = -descentL
		}
	}

	// 3. pozycje znaków typograficznych
	var (
		pen        point
		chunk      = -1
		lastPath   = -1
		chunkStart []int // pierwszy klaster każdego fragmentu
	)
	for ci := range clusters {
		c := &clusters[ci]
		i := c.start
		path := spans[owner[i]].path
		newChunk := ci == 0
		if path != lastPath {
			if path >= 0 {
				pen = point{paths[path].offset, 0}
				newChunk = true
			} else if lastPath >= 0 && len(paths) > lastPath {
				// tekst za textPath zaczyna się w punkcie, w którym skończył się tekst na ścieżce
				if p, _, ok := paths[lastPath].at(min(pen.x, paths[lastPath].length())); ok {
					pen = p
				}
			}
			lastPath = path
		}
		if path == -1 {
			if !isNaN(xs[i]) {
				pen.x = xs[i]
				newChunk = true
			}
			if !isNaN(ys[i]) {
				pen.y = ys[i]
				newChunk = true
			}
		}
		pen.x += dxs[i]
		pen.y += dys[i]
		if newChunk {
			chunk++
			chunkStart = append(chunkStart, ci)
		}
		c.chunk = chunk
		c.x, c.y = pen.x+c.lead, pen.y
		if !isNaN(rotates[i]) && rotates[i] != 0 {
			c.angle, c.rotated = rotates[i], true
		}
		c.hidden = path == -2
		pen.x = c.x + c.advance
	}

	// 4. textLength (SVG 2 § 11.8.2, krok „resolve text length”), od najgłębszych elementów
	for si := len(spans) - 1; si >= 0; si-- {
		s := spans[si]
		length := s.span.textLength.Resolve(s.fontSize, dims.innerWidth)
		if length <= 0 {
			continue
		}
		first, last := -1, -1
		a, b := Fl(math.Inf(1)), Fl(math.Inf(-1))
		for ci, c := range clusters {
			if c.start >= s.start && c.start < s.subEnd {
				if first == -1 {
					first = ci
				}
				last = ci
				a = min(a, c.x, c.x+c.advance*c.scaleX)
				b = max(b, c.x, c.x+c.advance*c.scaleX)
			}
		}
		if first == -1 || b <= a {
			continue
		}
		delta := length - (b - a)
		if s.span.lengthAdjust { // spacingAndGlyphs: glify rozciągnięte w poziomie
			scale := length / (b - a)
			for ci := first; ci <= last; ci++ {
				clusters[ci].x = a + (clusters[ci].x-a)*scale
				clusters[ci].scaleX *= scale
			}
		} else if m := last - first; m > 0 {
			for ci := first; ci <= last; ci++ {
				clusters[ci].x += Fl(ci-first) * delta / Fl(m)
			}
		}
		// dalsze znaki fragmentu przesuwają się o tyle samo
		for ci := last + 1; ci < len(clusters) && clusters[ci].chunk == clusters[last].chunk; ci++ {
			clusters[ci].x += delta
		}
	}

	// 5. text-anchor dla każdego fragmentu (SVG 2 § 11.8.3, krok „apply anchoring”)
	for k, first := range chunkStart {
		last := len(clusters)
		if k+1 < len(chunkStart) {
			last = chunkStart[k+1]
		}
		a, b := Fl(math.Inf(1)), Fl(math.Inf(-1))
		for ci := first; ci < last; ci++ {
			c := clusters[ci]
			a = min(a, c.x, c.x+c.advance*c.scaleX)
			b = max(b, c.x, c.x+c.advance*c.scaleX)
		}
		var shift Fl
		switch spans[owner[clusters[first].start]].span.textAnchor {
		case middle:
			shift = clusters[first].x - (a+b)/2
		case end:
			shift = clusters[first].x - b
		default:
			shift = clusters[first].x - a
		}
		if shift != 0 && !math.IsInf(float64(a), 0) {
			for ci := first; ci < last; ci++ {
				clusters[ci].x += shift
			}
		}
	}

	// 6. textPath: znak typograficzny stoi środkiem na ścieżce, obrócony wzdłuż stycznej
	// (SVG 2 § 11.10.3); znaki poza ścieżką nie są rysowane
	for ci := range clusters {
		c := &clusters[ci]
		path := spans[owner[c.start]].path
		if path < 0 {
			continue
		}
		half := c.advance * c.scaleX / 2
		p, angle, ok := paths[path].at(c.x + half)
		if !ok {
			c.hidden = true
			continue
		}
		cos, sin := Fl(math.Cos(float64(angle))), Fl(math.Sin(float64(angle)))
		offset := c.y
		c.x, c.y = p.x-half*cos-offset*sin, p.y-half*sin+offset*cos
		c.angle += angle * 180 / math.Pi
		c.rotated = true
	}

	// 7. ciągi glifów i prostokąty otaczające
	for ci := range clusters {
		c := clusters[ci]
		if c.hidden {
			continue
		}
		s := spans[owner[c.start]]
		seg := segments[c.segment]
		y := c.y + s.yAlign
		box := Rectangle{c.x, y - seg.baseline, c.advance * c.scaleX, seg.height}
		if c.rotated {
			box = Rectangle{c.x, y - seg.baseline, 0, seg.height}
		}
		// prostokąt elementu obejmuje jego poddrzewo (zob. drawNode)
		for si := range spans {
			if spans[si].start <= c.start && c.start < spans[si].subEnd {
				if spans[si].span.textBoundingBox == emptyBbox {
					spans[si].span.textBoundingBox = box
				} else {
					spans[si].span.textBoundingBox.union(box)
				}
			}
		}
	}
	svg.buildTextDrawings(spans, segments, glyphs, clusters, owner)
}

// buildTextDrawings łączy glify w ciągi: kolejne glify elementu na jednej linii bazowej, bez
// obrotu i z tą samą skalą idą jednym ciągiem; ich pozycje oddają korekty TJ (Kerning).
func (svg *SVGImage) buildTextDrawings(spans []textLayoutSpan, segments []textSegment,
	glyphs []textGlyph, clusters []textCluster, owner []int,
) {
	type placed struct {
		glyph   int
		x, y    Fl
		cluster int
	}
	var current []placed
	flush := func() {
		if len(current) == 0 {
			return
		}
		defer func() { current = current[:0] }()
		first := current[0]
		c := clusters[first.cluster]
		seg := segments[c.segment]
		drawing := backend.TextDrawing{
			FontSize: seg.drawing.FontSize,
			ScaleX:   c.scaleX,
			X:        first.x,
			Y:        first.y,
			Text:     seg.drawing.Text,
		}
		if c.rotated {
			drawing.Angle = -c.angle * math.Pi / 180
		}
		var carry Fl
		lastRun := -1
		for k, p := range current {
			g := glyphs[p.glyph]
			srcRun := seg.drawing.Runs[g.run]
			if g.run != lastRun {
				drawing.Runs = append(drawing.Runs, backend.TextRun{
					Font: srcRun.Font, SizeScale: srcRun.SizeScale, Skew: srcRun.Skew,
				})
				lastRun = g.run
			}
			out := g.glyph
			runSize := drawing.FontSize
			if srcRun.SizeScale != 0 {
				runSize *= srcRun.SizeScale
			}
			if runSize > 0 {
				out.XAdvance = (p.x - first.x) / c.scaleX * 1000 / drawing.FontSize
				if k+1 < len(current) {
					// Kerning przesuwa pióro o różnicę między szerokością glifu w foncie a przesunięciem;
					// zmieniamy go o różnicę między przesunięciem z kształtowania a żądanym
					wanted := g.kerningBase + out.Offset + g.advance*1000/runSize -
						(current[k+1].x-p.x)*1000/(runSize*c.scaleX) + carry
					out.Kerning = int(math.Floor(float64(wanted) + 1e-3))
					carry = wanted - Fl(out.Kerning)
				}
			}
			// syntetyczne pogrubienie obrysem nie łączy się z wypełnieniem i obrysem z SVG
			run := &drawing.Runs[len(drawing.Runs)-1]
			run.Glyphs = append(run.Glyphs, out)
		}
		span := spans[owner[c.start]].span
		span.drawings = append(span.drawings, drawing)
	}

	for ci, c := range clusters {
		if c.hidden {
			flush()
			continue
		}
		s := spans[owner[c.start]]
		y := c.y + s.yAlign
		for gi := c.glyphs[0]; gi < c.glyphs[1]; gi++ {
			g := glyphs[gi]
			if g.run < 0 {
				flush()
				continue
			}
			x := c.x + (g.inOffset-c.lead)*c.scaleX
			if len(current) != 0 {
				prev := current[len(current)-1]
				pc := clusters[prev.cluster]
				if pc.segment != c.segment || owner[pc.start] != owner[c.start] ||
					(c.rotated || pc.rotated) && prev.cluster != ci ||
					pc.scaleX != c.scaleX || math.Abs(float64(prev.y-y)) > 1e-4 {
					flush()
				}
			}
			current = append(current, placed{glyph: gi, x: x, y: y, cluster: ci})
		}
		if c.rotated {
			flush()
		}
	}
	flush()
}

func isNaN(f Fl) bool { return f != f }
