package svg

import (
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/matrix"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// gopdfgo: obszary odnośników elementów <a> (SVG 2 § 16.1, ADR 0029). Plik własny gopdfgo.

// LinkArea to obszar odnośnika elementu <a> w układzie obrazu: (0, 0) to lewy górny róg, a jednostką
// jest jednostka rozmiaru podanego w LinkAreas (piksel CSS dla obrazu w dokumencie HTML).
type LinkArea struct {
	// Href to "#nazwa" dla odnośnika wewnętrznego albo adres bezwzględny.
	Href string
	// prostokąt otaczający narysowaną treść elementu po wszystkich przekształceniach
	X, Y, Width, Height Fl
	// Index to numer wystąpienia elementu <a> w kolejności dokumentu (także pustych): ten sam element
	// w kolejnych rysowaniach obrazu ma ten sam numer (wycinki obrazu krojonego między strony, ADR 0041)
	Index int
}

// resolveLinkTargets rozwiązuje cele elementów <a> względem adresu dokumentu: odnośnik do fragmentu
// zostaje jako "#nazwa", inny staje się adresem bezwzględnym (jak atrybut href elementu <a> w HTML,
// zob. parseURLToken w pakiecie validation); cel, którego nie da się rozwiązać, jest pomijany.
// Zwraca, czy drzewo ma choć jeden odnośnik.
func resolveLinkTargets(root *svgNode, baseURL string) bool {
	visited := map[*svgNode]bool{}
	found := false
	var walk func(node *svgNode)
	walk = func(node *svgNode) {
		if node == nil || visited[node] {
			return
		}
		visited[node] = true
		switch content := node.graphicContent.(type) {
		case *textSpan:
			if href := content.linkHref; href != "" && !strings.HasPrefix(href, "#") {
				joined, err := utils.SafeUrljoin(baseURL, href, false)
				if err != nil {
					joined = ""
				}
				content.linkHref = joined
			}
			if content.linkHref == "#" {
				content.linkHref = ""
			}
			found = found || content.linkHref != ""
		case use:
			walk(content.target)
		}
		for _, child := range node.children {
			walk(child)
		}
	}
	walk(root)
	return found
}

// HasLinks mówi, czy obraz ma element <a> z celem.
func (img *SVGImage) HasLinks() bool { return img != nil && img.hasLinks }

// LinkAreas zwraca obszary odnośników obrazu narysowanego w rozmiarze width na height, w kolejności
// dokumentu (zewnętrzny <a> przed zagnieżdżonym, więc w PDF zagnieżdżony leży wyżej). Treść należy do
// najbliższego elementu <a>, który ją zawiera; obszar to prostokąt otaczający tej treści przycięty do
// okna widoku obrazu i zagnieżdżonych <svg> z overflow: hidden. Element bez narysowanej treści
// (display: none, visibility: hidden, pusty) nie ma obszaru.
//
// Ramki tekstu pochodzą ze składu wykonanego przy rysowaniu, dlatego LinkAreas woła się po Draw (albo
// DrawAsDocument, gdy asDocument) w tym samym rozmiarze; przed pierwszym rysowaniem tekst nie ma ramki.
// Funkcja nie zmienia stanu obrazu ani wyniku następnego rysowania.
func (img *SVGImage) LinkAreas(width, height Fl, asDocument bool) []LinkArea {
	if img == nil || !img.hasLinks || img.root == nil {
		return nil
	}
	root := img.root
	viewbox, content := root.viewbox, root.graphicContent
	if asDocument && img.synthesizedViewBox {
		// jak DrawAsDocument: bez viewBox dopisanego przez synthesizeViewBox
		root.viewbox = nil
		if s, ok := content.(svg); ok {
			s.preserveRatio.none = false
			root.graphicContent = s
		}
		defer func() { root.viewbox, root.graphicContent = viewbox, content }()
	}

	// jak draw
	var dims drawingDims
	dims.concreteWidth, dims.concreteHeight = width, height
	if vb := img.ViewBox(); vb != nil {
		dims.innerWidth, dims.innerHeight = vb.Width, vb.Height
	} else {
		dims.innerWidth, dims.innerHeight = width, height
	}
	dims.fontSize = defaultFontSize
	if img.parentFontSize > 0 {
		dims.fontSize = img.parentFontSize
	}
	dims.setupDiagonal()

	walker := linkWalker{img: img}
	walker.walk(root, dims, matrix.Identity(), Rectangle{0, 0, width, height}, -1)

	var out []LinkArea
	for _, area := range walker.areas {
		if area.Width > 0 && area.Height > 0 {
			out = append(out, area)
		}
	}
	return out
}

// linkWalker przechodzi drzewo tak jak drawNode, składając przekształcenia zamiast je rysować.
type linkWalker struct {
	img   *SVGImage
	areas []LinkArea // w kolejności otwarcia elementów <a>; pusty obszar ma Width == 0
}

// walk: m przenosi układ użytkownika rodzica do układu obrazu, clip to obszar widoczny w układzie
// obrazu, link to indeks bieżącego obszaru w areas albo -1 poza elementem <a>.
func (w *linkWalker) walk(node *svgNode, dims drawingDims, m matrix.Transform, clip Rectangle, link int) {
	dims.fontSize = node.attributes.fontSize.Resolve(dims.fontSize, dims.fontSize)

	// jak applyTransform; ramka elementu dla transform-box liczona bez płótna (zob. nodeTransform)
	if len(node.attributes.transforms) != 0 {
		if t := w.img.nodeTransform(nil, node, dims); t.Determinant() != 0 {
			m = matrix.Mul(m, t)
		}
	}

	span, isSpan := node.graphicContent.(*textSpan)
	if isSpan && span.linkHref != "" {
		w.areas = append(w.areas, LinkArea{Href: span.linkHref, Index: len(w.areas)})
		link = len(w.areas) - 1
	}

	if node.attributes.visible && link >= 0 {
		if box, ok := w.contentBox(node, dims); ok {
			w.add(link, m, clip, box)
		}
	}

	// jak drawNode: treść elementu (także przekształcenie <svg> i cel <use>) jest rysowana tylko,
	// gdy element jest widoczny
	switch content := node.graphicContent.(type) {
	case svg:
		if !node.attributes.visible {
			break
		}
		// jak svg.draw: przesunięcie, przycięcie zagnieżdżonego okna widoku i przekształcenie viewBox
		x, y := dims.point(node.attributes.x, node.attributes.y)
		m = matrix.Mul(m, matrix.Translation(x, y))
		width, height := dims.concreteWidth, dims.concreteHeight
		if !content.isRoot {
			width, height = dims.point(node.attributes.width, node.attributes.height)
			if content.isOverflowHidden {
				clip = intersect(clip, transformedBox(m, Rectangle{0, 0, width, height}))
			}
		}
		sx, sy, tx, ty := content.preserveRatio.resolveTransforms(width, height, node.attributes.viewbox, nil)
		m = matrix.Mul(m, matrix.New(sx, 0, 0, sy, tx, ty))
	case use:
		// jak use.draw: cel rysowany po przesunięciu o x, y
		if content.target != nil && node.attributes.visible {
			x, y := dims.point(node.attributes.x, node.attributes.y)
			w.walk(content.target, dims, matrix.Mul(m, matrix.Translation(x, y)), clip, link)
		}
	}

	if !node.attributes.display {
		return
	}
	childDims := nestedViewportDims(node, dims)
	for _, child := range node.children {
		w.walk(child, childDims, m, clip, link)
	}
}

// contentBox zwraca ramkę treści rysowanej przez sam element (bez potomków) w jego układzie użytkownika.
func (w *linkWalker) contentBox(node *svgNode, dims drawingDims) (Rectangle, bool) {
	switch content := node.graphicContent.(type) {
	case nil, svg, use:
		return Rectangle{}, false
	case *textSpan:
		// ramka fragmentu ze składu (layoutText); obejmuje tylko jego własny tekst
		bb := content.textBoundingBox
		return bb, bb != emptyBbox
	case image:
		x, y := dims.point(node.attributes.x, node.attributes.y)
		width, height := dims.point(node.attributes.width, node.attributes.height)
		return Rectangle{x, y, width, height}, true
	}
	return node.resolveBoundingBox(dims, true)
}

func (w *linkWalker) add(link int, m matrix.Transform, clip Rectangle, box Rectangle) {
	box = intersect(clip, transformedBox(m, box))
	if box.Width <= 0 || box.Height <= 0 {
		return
	}
	area := &w.areas[link]
	if area.Width == 0 && area.Height == 0 {
		area.X, area.Y, area.Width, area.Height = box.X, box.Y, box.Width, box.Height
		return
	}
	minX, minY := min(area.X, box.X), min(area.Y, box.Y)
	maxX, maxY := max(area.X+area.Width, box.X+box.Width), max(area.Y+area.Height, box.Y+box.Height)
	area.X, area.Y, area.Width, area.Height = minX, minY, maxX-minX, maxY-minY
}

// transformedBox zwraca prostokąt otaczający obrazu prostokąta r przez m.
func transformedBox(m matrix.Transform, r Rectangle) Rectangle {
	minX, minY := m.Apply(r.X, r.Y)
	maxX, maxY := minX, minY
	for _, corner := range [3][2]Fl{{r.X + r.Width, r.Y}, {r.X, r.Y + r.Height}, {r.X + r.Width, r.Y + r.Height}} {
		x, y := m.Apply(corner[0], corner[1])
		minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
	}
	return Rectangle{minX, minY, maxX - minX, maxY - minY}
}
