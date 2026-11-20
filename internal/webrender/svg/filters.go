package svg

import (
	"math"
	"strconv"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	"github.com/polestorm/gopdfgo/internal/webrender/css/parser"
)

// gopdfgo: filtry SVG bez rastrowania (Filter Effects 1). PDF nie ma operacji rozmycia, więc graf
// prymitywów filtra jest oceniany symbolicznie: wynik każdego prymitywu to lista warstw (od spodu),
// z których każda jest albo samym elementem (SourceGraphic), albo jego sylwetką w jednym kolorze,
// albo jednolitym wypełnieniem obszaru, z przesunięciem i odchyleniem standardowym rozmycia. Takie
// warstwy da się narysować wektorowo: sylwetka to element użyty jako maska miękka alfa, a rozmycie
// to suma przesuniętych kopii tej maski (zob. newBlurKernel). Graf, którego nie da się tak opisać
// (feTurbulence, feMorphology, feImage, macierz koloru na samym elemencie itd.), jest pomijany w całości
// z ostrzeżeniem, a element rysowany bez filtra, jak przed tą zmianą.

// svgFilter to element <filter> z prymitywami.
type svgFilter struct {
	region        box  // x, y, width, height
	userSpace     bool // filterUnits="userSpaceOnUse"
	primitiveBBox bool // primitiveUnits="objectBoundingBox"
	primitives    []*cascadedNode

	attrs    nodeAttributes // atrybuty elementu <filter> (do dziedziczenia przez href)
	href     string         // odwołanie do innego filtra
	resolved bool           // href rozwiązany
	warned   bool           // ostrzeżenie o nieobsługiwanym grafie wypisane
}

// parse a <filter> node
func newFilter(node *cascadedNode) (*svgFilter, error) {
	out := &svgFilter{attrs: node.attrs, href: parseURLFragment(node.attrs["href"])}
	for _, child := range node.children {
		if strings.HasPrefix(child.tag, "fe") {
			out.primitives = append(out.primitives, child)
		}
	}
	if out.href == "" {
		out.resolved = true
		return out, out.parseAttributes(node.attrs)
	}
	return out, nil
}

// parseAttributes czyta obszar filtra i jednostki.
func (f *svgFilter) parseAttributes(attrs nodeAttributes) error {
	f.userSpace = attrs["filterUnits"] == "userSpaceOnUse"
	f.primitiveBBox = attrs["primitiveUnits"] == "objectBoundingBox"
	// domyślny obszar filtra: -10%, -10%, 120%, 120% (Filter Effects 1 § 7.1)
	defaults := [4]Value{{-10, Perc}, {-10, Perc}, {120, Perc}, {120, Perc}}
	for i, name := range [4]string{"x", "y", "width", "height"} {
		v, err := parseValue(attrs[name])
		if err != nil {
			return err
		}
		if v.U == 0 {
			v = defaults[i]
		}
		switch i {
		case 0:
			f.region.x = v
		case 1:
			f.region.y = v
		case 2:
			f.region.width = v
		case 3:
			f.region.height = v
		}
	}
	return nil
}

// resolveFilterHref dziedziczy przez href atrybuty niepodane w elemencie i prymitywy, gdy element
// nie ma własnych (Filter Effects 1 § 7, atrybut href).
func (defs definitions) resolveFilterHref(f *svgFilter) {
	if f.resolved {
		return
	}
	f.resolved = true
	attrs := nodeAttributes{}
	for k, v := range f.attrs {
		attrs[k] = v
	}
	seen := map[*svgFilter]bool{f: true}
	for ref := defs.filters[f.href]; ref != nil && !seen[ref]; ref = defs.filters[parseURLFragment(ref.attrs["href"])] {
		seen[ref] = true
		for _, name := range [6]string{"x", "y", "width", "height", "filterUnits", "primitiveUnits"} {
			if _, has := attrs[name]; !has {
				if v, has := ref.attrs[name]; has {
					attrs[name] = v
				}
			}
		}
		if len(f.primitives) == 0 {
			f.primitives = ref.primitives
		}
	}
	if err := f.parseAttributes(attrs); err != nil {
		f.parseAttributes(nil)
	}
}

// bboxFraction zamienia wartość w jednostkach objectBoundingBox (liczba albo procent) na ułamek.
func bboxFraction(v Value) Fl {
	if v.U == Perc {
		return v.V / 100
	}
	return v.V
}

// resolveRegion wyznacza obszar filtra w układzie użytkownika elementu.
func (f *svgFilter) resolveRegion(bbox Rectangle, hasBBox bool, dims drawingDims) (Rectangle, bool) {
	if f.userSpace {
		x, y := dims.point(f.region.x, f.region.y)
		w, h := dims.point(f.region.width, f.region.height)
		return Rectangle{x, y, w, h}, true
	}
	if !hasBBox {
		return Rectangle{}, false
	}
	return Rectangle{
		X:      bbox.X + bboxFraction(f.region.x)*bbox.Width,
		Y:      bbox.Y + bboxFraction(f.region.y)*bbox.Height,
		Width:  bboxFraction(f.region.width) * bbox.Width,
		Height: bboxFraction(f.region.height) * bbox.Height,
	}, true
}

// objectBoundingBox zwraca ramkę elementu razem z potomkami (SVG 2 § 8.10), bez obrysu. Tekst
// w potomkach jest składany wcześniej niż przy rysowaniu, bo jego ramka jest znana dopiero po składzie.
func (svg *SVGImage) objectBoundingBox(dst backend.Canvas, node *svgNode, dims drawingDims) (Rectangle, bool) {
	out := emptyBbox
	if _, isImage := node.graphicContent.(image); isImage {
		x, y := dims.point(node.attributes.x, node.attributes.y)
		w, h := dims.point(node.attributes.width, node.attributes.height)
		out = Rectangle{x, y, w, h}
	} else if t, isText := node.graphicContent.(*textSpan); isText && t.isText {
		// ramka elementu <text> to suma ramek jego fragmentów
		var collect func(n *svgNode)
		collect = func(n *svgNode) {
			if span, ok := n.graphicContent.(*textSpan); ok && n.attributes.display {
				if span.textBoundingBox != emptyBbox && n.visible {
					out.union(span.textBoundingBox)
				}
				for _, child := range n.children {
					collect(child)
				}
			}
		}
		collect(node)
		return out, out != emptyBbox
	} else if bb, ok := node.resolveBoundingBox(dims, false); ok {
		out = bb
	}
	if !node.attributes.display {
		return out, out != emptyBbox
	}
	for _, child := range node.children {
		childDims := nestedViewportDims(node, dims) // gopdfgo: okno zagnieżdżonego <svg>
		childDims.fontSize = child.attributes.fontSize.Resolve(dims.fontSize, dims.fontSize)
		if t, ok := child.graphicContent.(*textSpan); ok && t.isText {
			svg.layoutText(dst, child, childDims)
		}
		cb, ok := svg.objectBoundingBox(dst, child, childDims)
		if !ok {
			continue
		}
		if len(child.attributes.transforms) != 0 {
			mat := svg.nodeTransform(dst, child, childDims) // gopdfgo: z transform-origin
			moved := emptyBbox
			for _, corner := range [4][2]Fl{{cb.X, cb.Y}, {cb.X + cb.Width, cb.Y}, {cb.X, cb.Y + cb.Height}, {cb.X + cb.Width, cb.Y + cb.Height}} {
				x, y := mat.Apply(corner[0], corner[1])
				moved.add(x, y)
			}
			cb = moved
		}
		out.union(cb)
	}
	return out, out != emptyBbox
}

// warstwy wyniku prymitywu

type filterLayerKind uint8

const (
	layerSource     filterLayerKind = iota // sam element (SourceGraphic)
	layerSilhouette                        // kanał alfa elementu w jednym kolorze
	layerFlood                             // jednolite wypełnienie obszaru
)

type filterLayer struct {
	kind filterLayerKind
	// color: kolor sylwetki i wypełnienia (nieprzemnożony, A to krycie); dla elementu liczy się tylko A
	color  parser.RGBA
	dx, dy Fl // przesunięcie w układzie użytkownika
	sx, sy Fl // odchylenie standardowe rozmycia w układzie użytkownika
	clip   *Rectangle
	blend  string // tryb mieszania z warstwami pod spodem (feBlend)
	// knockout: warstwa jest wycięta tam, gdzie leży element (feComposite operator="out" z SourceAlpha)
	knockout bool
}

func (l filterLayer) blurred() bool { return l.sx > 0 || l.sy > 0 }

// hard mówi, że warstwa to element albo jego sylwetka bez przesunięcia, rozmycia i przycięcia.
func (l filterLayer) hard() bool {
	return l.kind != layerFlood && !l.blurred() && l.dx == 0 && l.dy == 0 && l.clip == nil && !l.knockout && l.blend == ""
}

// sameShape mówi, że dwie warstwy pokrywają ten sam obszar (ta sama sylwetka w tym samym miejscu).
func sameShape(a, b filterLayer) bool {
	return a.kind != layerFlood && b.kind != layerFlood && a.dx == b.dx && a.dy == b.dy && a.sx == b.sx &&
		a.sy == b.sy && a.clip == nil && b.clip == nil && !a.knockout && !b.knockout
}

type filterResult []filterLayer

func (r filterResult) clone() filterResult { return append(filterResult(nil), r...) }

// filterEval ocenia graf prymitywów jednego użycia filtra.
type filterEval struct {
	filter  *svgFilter
	dims    drawingDims
	bbox    Rectangle
	region  Rectangle
	results map[string]filterResult
	last    filterResult
	first   bool
	reason  string // przyczyna odrzucenia grafu
}

func (ev *filterEval) unsupported(reason string) (filterResult, bool) {
	ev.reason = reason
	return nil, false
}

// input zwraca wejście prymitywu według atrybutu in/in2 (Filter Effects 1 § 9.4).
func (ev *filterEval) input(name string) (filterResult, bool) {
	switch strings.TrimSpace(name) {
	case "SourceGraphic":
		return filterResult{{kind: layerSource, color: parser.RGBA{A: 1}}}, true
	case "SourceAlpha":
		return filterResult{{kind: layerSilhouette, color: parser.RGBA{A: 1}}}, true
	case "BackgroundImage", "BackgroundAlpha", "FillPaint", "StrokePaint":
		// tło i farby nie są dostępne (Chrome traktuje je jak przezroczystą czerń)
		return nil, true
	}
	if r, has := ev.results[name]; has {
		return r.clone(), true
	}
	// brak nazwy albo odwołanie do nieistniejącego wyniku: wynik poprzedniego prymitywu, a dla
	// pierwszego SourceGraphic
	if ev.first {
		return ev.input("SourceGraphic")
	}
	return ev.last.clone(), true
}

// numbers czyta listę liczb rozdzielonych spacjami lub przecinkami.
func numbers(s string) ([]Fl, bool) {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r' })
	out := make([]Fl, 0, len(fields))
	for _, f := range fields {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil, false
		}
		out = append(out, Fl(v))
	}
	return out, true
}

func number(s string, def Fl) Fl {
	if v, ok := numbers(s); ok && len(v) == 1 {
		return v[0]
	}
	return def
}

// scaleX, scaleY zamieniają liczbę z atrybutu prymitywu na jednostki użytkownika (primitiveUnits).
func (ev *filterEval) scaleX(v Fl) Fl {
	if ev.filter.primitiveBBox {
		return v * ev.bbox.Width
	}
	return v
}

func (ev *filterEval) scaleY(v Fl) Fl {
	if ev.filter.primitiveBBox {
		return v * ev.bbox.Height
	}
	return v
}

// subregion zwraca jawny podobszar prymitywu (x, y, width, height), brakujące wartości z obszaru filtra.
func (ev *filterEval) subregion(attrs nodeAttributes) *Rectangle {
	out := ev.region
	given := false
	for i, name := range [4]string{"x", "y", "width", "height"} {
		s, has := attrs[name]
		if !has {
			continue
		}
		v, err := parseValue(s)
		if err != nil || v.U == 0 {
			continue
		}
		given = true
		var r Fl
		if ev.filter.primitiveBBox {
			ref, base := ev.bbox.Width, ev.bbox.X
			if i%2 == 1 {
				ref, base = ev.bbox.Height, ev.bbox.Y
			}
			if i >= 2 {
				base = 0
			}
			r = base + bboxFraction(v)*ref
		} else if i%2 == 0 {
			r = v.Resolve(ev.dims.fontSize, ev.dims.innerWidth)
		} else {
			r = v.Resolve(ev.dims.fontSize, ev.dims.innerHeight)
		}
		switch i {
		case 0:
			out.X = r
		case 1:
			out.Y = r
		case 2:
			out.Width = r
		case 3:
			out.Height = r
		}
	}
	if !given {
		return nil
	}
	return &out
}

func intersect(a, b Rectangle) Rectangle {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.Width, b.X+b.Width), min(a.Y+a.Height, b.Y+b.Height)
	return Rectangle{x0, y0, max(0, x1-x0), max(0, y1-y0)}
}

// run ocenia cały graf; ok == false, gdy grafu nie da się narysować wektorowo.
func (ev *filterEval) run() (filterResult, bool) {
	ev.results = make(map[string]filterResult)
	ev.first = true
	for _, prim := range ev.filter.primitives {
		res, ok := ev.primitive(prim)
		if !ok {
			return nil, false
		}
		if sub := ev.subregion(prim.attrs); sub != nil {
			for i := range res {
				clip := *sub
				if res[i].clip != nil {
					clip = intersect(clip, *res[i].clip)
				}
				res[i].clip = &clip
			}
		}
		if name := prim.attrs["result"]; name != "" {
			ev.results[name] = res
		}
		ev.last, ev.first = res, false
	}
	return ev.last, true
}

func (ev *filterEval) primitive(prim *cascadedNode) (filterResult, bool) {
	attrs := prim.attrs
	switch prim.tag {
	case "feFlood":
		c := floodColor(attrs)
		if c.A <= 0 {
			return nil, true
		}
		return filterResult{{kind: layerFlood, color: c}}, true
	case "feOffset":
		in, _ := ev.input(attrs["in"])
		dx, dy := ev.scaleX(number(attrs["dx"], 0)), ev.scaleY(number(attrs["dy"], 0))
		for i := range in {
			in[i].offset(dx, dy)
		}
		return in, true
	case "feGaussianBlur":
		in, _ := ev.input(attrs["in"])
		sx, sy, ok := stdDeviation(attrs["stdDeviation"], 0)
		if !ok {
			return in, true // wartość ujemna lub zero wyłącza prymityw (Filter Effects 1 § 9.11)
		}
		for i := range in {
			in[i].blur(ev.scaleX(sx), ev.scaleY(sy))
		}
		return in, true
	case "feDropShadow", "fedropshadow":
		// parser HTML z golang.org/x/net nie zna feDropShadow w tabeli nazw SVG (HTML § 13.2.6.5)
		// i zostawia nazwę małymi literami
		in, _ := ev.input(attrs["in"])
		shadow, ok := silhouetteOf(in)
		if !ok {
			return ev.unsupported("feDropShadow of a composite input")
		}
		if shadow == nil {
			return in, true
		}
		c := floodColor(attrs)
		shadow.color.R, shadow.color.G, shadow.color.B = c.R, c.G, c.B
		shadow.color.A *= c.A
		shadow.offset(ev.scaleX(number(attrs["dx"], 2)), ev.scaleY(number(attrs["dy"], 2)))
		if sx, sy, ok := stdDeviation(attrs["stdDeviation"], 2); ok {
			shadow.blur(ev.scaleX(sx), ev.scaleY(sy))
		}
		out := filterResult{}
		if shadow.color.A > 0 {
			out = append(out, *shadow)
		}
		return append(out, in...), true
	case "feMerge":
		var out filterResult
		for _, node := range prim.children {
			if node.tag != "feMergeNode" {
				continue
			}
			in, _ := ev.input(node.attrs["in"])
			out = append(out, in...)
		}
		return out, true
	case "feBlend":
		in, _ := ev.input(attrs["in"])
		in2, _ := ev.input(attrs["in2"])
		mode := strings.TrimSpace(attrs["mode"])
		if mode == "" || mode == "normal" || len(in2) == 0 {
			return append(in2, in...), true
		}
		for i := range in {
			if in[i].blend != "" {
				return ev.unsupported("nested feBlend modes")
			}
			in[i].blend = mode
		}
		return append(in2, in...), true
	case "feComposite":
		in, _ := ev.input(attrs["in"])
		in2, _ := ev.input(attrs["in2"])
		return ev.composite(strings.TrimSpace(attrs["operator"]), in, in2, attrs)
	case "feColorMatrix":
		in, _ := ev.input(attrs["in"])
		m, ok := colorMatrix(attrs)
		if !ok {
			return ev.unsupported("invalid feColorMatrix")
		}
		linear := isLinearRGB(attrs)
		return ev.mapColors(in, func(c parser.RGBA) parser.RGBA { return m.apply(c, linear) },
			func(alpha Fl) Fl { return m.alpha(alpha) })
	case "feComponentTransfer":
		in, _ := ev.input(attrs["in"])
		ct := newComponentTransfer(prim)
		linear := isLinearRGB(attrs)
		return ev.mapColors(in, func(c parser.RGBA) parser.RGBA { return ct.apply(c, linear) },
			func(alpha Fl) Fl { return ct.funcs[3].eval(alpha) })
	}
	return ev.unsupported("filter primitive " + prim.tag)
}

func (l *filterLayer) offset(dx, dy Fl) {
	if l.kind == layerFlood {
		return
	}
	l.dx += dx
	l.dy += dy
	if l.clip != nil {
		c := *l.clip
		c.X += dx
		c.Y += dy
		l.clip = &c
	}
}

func (l *filterLayer) blur(sx, sy Fl) {
	if l.kind == layerFlood {
		return
	}
	// złożenie rozmyć Gaussa to rozmycie o sumie wariancji
	l.sx = Fl(math.Hypot(float64(l.sx), float64(sx)))
	l.sy = Fl(math.Hypot(float64(l.sy), float64(sy)))
}

// stdDeviation czyta jedną albo dwie wartości; ok == false, gdy rozmycie jest wyłączone.
func stdDeviation(s string, def Fl) (sx, sy Fl, ok bool) {
	v, valid := numbers(s)
	switch {
	case !valid || len(v) == 0:
		sx, sy = def, def
	case len(v) == 1:
		sx, sy = v[0], v[0]
	default:
		sx, sy = v[0], v[1]
	}
	if sx < 0 || sy < 0 || (sx == 0 && sy == 0) {
		return 0, 0, false
	}
	return sx, sy, true
}

// silhouetteOf zwraca sylwetkę (kanał alfa) wyniku złożonego z jednej warstwy; nil dla pustego wyniku.
func silhouetteOf(in filterResult) (*filterLayer, bool) {
	if len(in) == 0 {
		return nil, true
	}
	if len(in) != 1 || in[0].kind == layerFlood || in[0].blend != "" {
		return nil, false
	}
	out := in[0]
	out.kind = layerSilhouette
	out.color.R, out.color.G, out.color.B = 0, 0, 0
	return &out, true
}

// floodColor czyta flood-color i flood-opacity (Filter Effects 1 § 9.13).
func floodColor(attrs nodeAttributes) parser.RGBA {
	s := strings.TrimSpace(attrs["flood-color"])
	if s == "" {
		s = "black"
	}
	c := parser.ParseColorString(s)
	if c.IsNone() {
		c = parser.ParseColorString("black")
	}
	out := c.RGBA
	opacity, err := parseOpacity(attrs["flood-opacity"])
	if err != nil {
		opacity = 1
	}
	out.A *= max(0, min(1, opacity))
	return out
}

func (ev *filterEval) composite(operator string, in, in2 filterResult, attrs nodeAttributes) (filterResult, bool) {
	switch operator {
	case "", "over":
		return append(in2, in...), true
	case "in":
		if len(in) == 0 || len(in2) == 0 {
			return nil, true
		}
		if len(in2) == 1 && in2[0].kind == layerFlood && in2[0].clip == nil {
			for i := range in {
				in[i].color.A *= in2[0].color.A
			}
			return in, true
		}
		if len(in) == 1 && in[0].kind == layerFlood && len(in2) == 1 && in2[0].blend == "" {
			// kolor wypełnienia w kształcie in2 (zabarwienie cienia)
			out := in2[0]
			out.kind = layerSilhouette
			out.color = parser.RGBA{R: in[0].color.R, G: in[0].color.G, B: in[0].color.B, A: in[0].color.A * in2[0].color.A}
			if in[0].clip != nil {
				out.clip = in[0].clip
			}
			return filterResult{out}, true
		}
		if len(in) == 1 && len(in2) == 1 && sameShape(in[0], in2[0]) {
			out := in[0]
			out.color.A *= in2[0].color.A
			return filterResult{out}, true
		}
		return ev.unsupported("feComposite in of two shapes")
	case "out":
		if len(in2) == 0 {
			return in, true
		}
		if len(in2) == 1 && in2[0].hard() {
			for i := range in {
				if in[i].knockout {
					return ev.unsupported("nested feComposite out")
				}
				in[i].knockout = true
			}
			return in, true
		}
		return ev.unsupported("feComposite out of a shifted or blurred shape")
	case "atop":
		if len(in2) == 0 {
			return nil, true
		}
		if len(in) == 0 {
			return in2, true
		}
		if len(in) == 1 && in[0].kind == layerFlood && len(in2) == 1 && in2[0].kind != layerFlood {
			top := in2[0]
			top.kind = layerSilhouette
			top.color = parser.RGBA{R: in[0].color.R, G: in[0].color.G, B: in[0].color.B, A: in[0].color.A * in2[0].color.A}
			return filterResult{in2[0], top}, true
		}
		if len(in) == 1 && len(in2) == 1 && sameShape(in[0], in2[0]) {
			return filterResult{in2[0], in[0]}, true
		}
		return ev.unsupported("feComposite atop")
	case "arithmetic":
		k1, k2 := number(attrs["k1"], 0), number(attrs["k2"], 0)
		k3, k4 := number(attrs["k3"], 0), number(attrs["k4"], 0)
		if k1 != 0 || k4 != 0 || k2 < 0 || k3 < 0 || k2 > 1 || k3 > 1 {
			return ev.unsupported("feComposite arithmetic")
		}
		// k2·in + k3·in2: dla rozłącznych kształtów to samo co nałożenie warstw o kryciu k2 i k3
		var out filterResult
		for _, l := range in2 {
			l.color.A *= k3
			if l.color.A > 0 {
				out = append(out, l)
			}
		}
		for _, l := range in {
			l.color.A *= k2
			if l.color.A > 0 {
				out = append(out, l)
			}
		}
		return out, true
	}
	return ev.unsupported("feComposite operator " + operator)
}

// mapColors stosuje przekształcenie koloru do warstw jednobarwnych; dla samego elementu dopuszcza
// tylko zmianę krycia (przekształcenie koloru elementu wymagałoby rastrowania).
func (ev *filterEval) mapColors(in filterResult, color func(parser.RGBA) parser.RGBA, alpha func(Fl) Fl) (filterResult, bool) {
	var out filterResult
	for _, l := range in {
		switch l.kind {
		case layerFlood:
			l.color = color(l.color)
		case layerSource:
			// przekształcenie musi zostawiać kolor (sprawdzane na kilku barwach) i skalować alfę
			for _, probe := range [3]parser.RGBA{{R: 1, A: 1}, {G: 0.5, B: 1, A: 1}, {R: 0.2, G: 0.7, B: 0.4, A: 1}} {
				c := color(probe)
				if math.Abs(float64(c.R-probe.R)) > 0.01 || math.Abs(float64(c.G-probe.G)) > 0.01 || math.Abs(float64(c.B-probe.B)) > 0.01 {
					return ev.unsupported("color transform of SourceGraphic")
				}
			}
			k, ok := alphaScale(alpha, 1)
			if !ok {
				return ev.unsupported("non-linear alpha transform of SourceGraphic")
			}
			l.color.A *= min(1, k)
		case layerSilhouette:
			a0 := l.color.A
			c := color(l.color)
			// poza kształtem piksele są przezroczyste: przekształcenie musi je takimi zostawić
			if outside := color(parser.RGBA{}); outside.A > 0.002 {
				return ev.unsupported("color transform makes transparent pixels visible")
			}
			if l.blurred() || a0 < 1 {
				// alfa rozmytej sylwetki zmienia się płynnie: przekształcenie musi być liniowe w alfie
				k, ok := alphaScaleColor(color, l.color)
				if !ok {
					return ev.unsupported("non-linear alpha transform of a blurred shape")
				}
				c.A = k * a0
			}
			l.color = c
		}
		if l.color.A > 0 {
			out = append(out, l)
		}
	}
	return out, true
}

// alphaScale sprawdza, że f(a) ≈ k·a na przedziale [0, a0], i zwraca k.
func alphaScale(f func(Fl) Fl, a0 Fl) (Fl, bool) {
	if a0 <= 0 {
		return 0, true
	}
	k := f(a0) / a0
	for _, t := range [4]Fl{0, 0.25, 0.5, 0.75} {
		if math.Abs(float64(f(t*a0)-k*t*a0)) > 0.02 {
			return 0, false
		}
	}
	return k, true
}

// alphaScaleColor to alphaScale dla pełnego przekształcenia koloru przy stałej barwie sylwetki.
func alphaScaleColor(f func(parser.RGBA) parser.RGBA, c parser.RGBA) (Fl, bool) {
	return alphaScale(func(a Fl) Fl {
		p := c
		p.A = a
		return f(p).A
	}, max(c.A, 1e-6))
}

// isLinearRGB: color-interpolation-filters, domyślnie linearRGB (Filter Effects 1 § 17.7)
func isLinearRGB(attrs nodeAttributes) bool {
	v := strings.TrimSpace(attrs["color-interpolation-filters"])
	return v == "" || v == "linearRGB"
}

func toLinear(c Fl) Fl {
	if c <= 0.04045 {
		return c / 12.92
	}
	return Fl(math.Pow(float64((c+0.055)/1.055), 2.4))
}

func toSRGB(c Fl) Fl {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return Fl(1.055*math.Pow(float64(c), 1/2.4) - 0.055)
}

func clamp01(v Fl) Fl { return max(0, min(1, v)) }

type colorMatrixValues [20]Fl

// colorMatrix czyta feColorMatrix (Filter Effects 1 § 9.6).
func colorMatrix(attrs nodeAttributes) (colorMatrixValues, bool) {
	identity := colorMatrixValues{1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0}
	values, valid := numbers(attrs["values"])
	_, hasValues := attrs["values"]
	switch strings.TrimSpace(attrs["type"]) {
	case "", "matrix":
		if !hasValues {
			return identity, true
		}
		if !valid || len(values) != 20 {
			return identity, true
		}
		var m colorMatrixValues
		copy(m[:], values)
		return m, true
	case "saturate":
		s := Fl(1)
		if hasValues {
			if !valid || len(values) != 1 {
				return identity, true
			}
			s = values[0]
		}
		return colorMatrixValues{
			0.213 + 0.787*s, 0.715 - 0.715*s, 0.072 - 0.072*s, 0, 0,
			0.213 - 0.213*s, 0.715 + 0.285*s, 0.072 - 0.072*s, 0, 0,
			0.213 - 0.213*s, 0.715 - 0.715*s, 0.072 + 0.928*s, 0, 0,
			0, 0, 0, 1, 0,
		}, true
	case "hueRotate":
		var deg Fl
		if hasValues && valid && len(values) == 1 {
			deg = values[0]
		}
		rad := float64(deg) * math.Pi / 180
		c, s := Fl(math.Cos(rad)), Fl(math.Sin(rad))
		return colorMatrixValues{
			0.213 + c*0.787 - s*0.213, 0.715 - c*0.715 - s*0.715, 0.072 - c*0.072 + s*0.928, 0, 0,
			0.213 - c*0.213 + s*0.143, 0.715 + c*0.285 + s*0.140, 0.072 - c*0.072 - s*0.283, 0, 0,
			0.213 - c*0.213 - s*0.787, 0.715 - c*0.715 + s*0.715, 0.072 + c*0.928 + s*0.072, 0, 0,
			0, 0, 0, 1, 0,
		}, true
	case "luminanceToAlpha":
		return colorMatrixValues{
			0, 0, 0, 0, 0,
			0, 0, 0, 0, 0,
			0, 0, 0, 0, 0,
			0.2125, 0.7154, 0.0721, 0, 0,
		}, true
	}
	return identity, false
}

func (m colorMatrixValues) apply(c parser.RGBA, linear bool) parser.RGBA {
	r, g, b := c.R, c.G, c.B
	if linear {
		r, g, b = toLinear(r), toLinear(g), toLinear(b)
	}
	a := c.A
	row := func(i int) Fl { return m[i]*r + m[i+1]*g + m[i+2]*b + m[i+3]*a + m[i+4] }
	out := parser.RGBA{R: clamp01(row(0)), G: clamp01(row(5)), B: clamp01(row(10)), A: clamp01(row(15))}
	if linear {
		out.R, out.G, out.B = toSRGB(out.R), toSRGB(out.G), toSRGB(out.B)
	}
	return out
}

// alpha to wiersz alfa macierzy dla piksela czarnego (dla samego elementu sprawdzamy, że kolor nie wpływa)
func (m colorMatrixValues) alpha(a Fl) Fl { return clamp01(m[18]*a + m[19]) }

// feComponentTransfer (Filter Effects 1 § 9.7)

type transferFunc struct {
	kind                              string
	table                             []Fl
	slope, intercept, amplitude, expo Fl
	offset                            Fl
}

func (f transferFunc) eval(c Fl) Fl {
	switch f.kind {
	case "table":
		n := len(f.table)
		if n == 0 {
			return c
		}
		if n == 1 {
			return clamp01(f.table[0])
		}
		pos := c * Fl(n-1)
		k := int(pos)
		if k >= n-1 {
			return clamp01(f.table[n-1])
		}
		return clamp01(f.table[k] + (pos-Fl(k))*(f.table[k+1]-f.table[k]))
	case "discrete":
		n := len(f.table)
		if n == 0 {
			return c
		}
		k := min(n-1, int(c*Fl(n)))
		return clamp01(f.table[k])
	case "linear":
		return clamp01(f.slope*c + f.intercept)
	case "gamma":
		return clamp01(f.amplitude*Fl(math.Pow(float64(c), float64(f.expo))) + f.offset)
	}
	return c
}

type componentTransfer struct{ funcs [4]transferFunc }

func newComponentTransfer(node *cascadedNode) componentTransfer {
	var out componentTransfer
	for _, child := range node.children {
		index := strings.Index("RGBA", strings.TrimPrefix(child.tag, "feFunc"))
		if !strings.HasPrefix(child.tag, "feFunc") || len(child.tag) != 7 || index < 0 {
			continue
		}
		f := transferFunc{
			kind:      strings.TrimSpace(child.attrs["type"]),
			slope:     number(child.attrs["slope"], 1),
			intercept: number(child.attrs["intercept"], 0),
			amplitude: number(child.attrs["amplitude"], 1),
			expo:      number(child.attrs["exponent"], 1),
			offset:    number(child.attrs["offset"], 0),
		}
		f.table, _ = numbers(child.attrs["tableValues"])
		out.funcs[index] = f
	}
	return out
}

func (ct componentTransfer) apply(c parser.RGBA, linear bool) parser.RGBA {
	r, g, b := c.R, c.G, c.B
	if linear {
		r, g, b = toLinear(r), toLinear(g), toLinear(b)
	}
	out := parser.RGBA{R: ct.funcs[0].eval(r), G: ct.funcs[1].eval(g), B: ct.funcs[2].eval(b), A: ct.funcs[3].eval(c.A)}
	if linear {
		out.R, out.G, out.B = toSRGB(out.R), toSRGB(out.G), toSRGB(out.B)
	}
	if c == (parser.RGBA{}) {
		// przezroczysta czerń: kolor nieprzemnożony jest nieokreślony, liczy się tylko alfa
		out.R, out.G, out.B = 0, 0, 0
	}
	return out
}
