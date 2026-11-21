// Package svg implements parsing of SVG images.
// It transforms SVG text files into an in-memory structure
// that is easy to draw.
// CSS is supported via the `css` package.
package svg

import (
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	"github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	"github.com/polestorm/gopdfgo/internal/webrender/matrix"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
	"golang.org/x/net/html"
)

// convert from an svg tree to the final form

// nodes that are not directly draw but may be referenced by ID
// from other nodes
type definitions struct {
	filters      map[string]*svgFilter
	clipPaths    map[string]*clipPath
	masks        map[string]mask
	paintServers map[string]paintServer
	markers      map[string]*marker
	nodes        map[string]*svgNode
}

func newDefinitions() definitions {
	return definitions{
		filters:      make(map[string]*svgFilter),
		clipPaths:    make(map[string]*clipPath),
		masks:        make(map[string]mask),
		paintServers: make(map[string]paintServer),
		markers:      make(map[string]*marker),
		nodes:        make(map[string]*svgNode),
	}
}

type SVGImage struct {
	root *svgNode

	// background (gopdfgo): kolor tła elementu głównego <svg> z właściwości CSS background(-color);
	// w dokumencie SVG wyświetlanym jako obraz tło elementu głównego wypełnia cały obszar obrazu
	// (CSS Backgrounds 3 § 2.11.2, tło płótna; test WPT background-image-007). Źródło je pomijało.
	background *parser.RGBA

	// setup when calling Draw
	textContext text.TextLayoutContext

	definitions definitions

	// synthesizedViewBox (gopdfgo): viewBox dopisany przez synthesizeViewBox; DrawAsDocument rysuje bez niego
	synthesizedViewBox bool
	// hostBaseScale (gopdfgo): skala z układu bieżącego strumienia (strony albo grupy) do układu
	// gospodarza, zob. hostScale; ustawiana w Draw i przy tworzeniu grup
	hostBaseScale Fl
	// paintContext (gopdfgo): bieżący element kontekstu dla context-fill/context-stroke
	paintContext *paintContext
	// parentFontSize (gopdfgo): rozmiar pisma rodzica HTML osadzonego <svg>, do którego odnoszą się
	// względne font-size elementu głównego (em, %); 0 dla dokumentu samodzielnego (rozmiar domyślny)
	parentFontSize Fl
	// hasLinks (gopdfgo): drzewo ma element <a> z celem (zob. LinkAreas)
	hasLinks bool
	// views (gopdfgo): elementy <view> według id, dla fragmentu adresu (zob. view.go)
	views map[string]viewElement
}

// DisplayedSize returns the value of the "width" and "height" attributes
// of the <svg> root element, which discribe the displayed size of the rectangular viewport.
// If no value is specified, it default to 100% (auto).
func (svg *SVGImage) DisplayedSize() (width, height Value) {
	w, h := svg.root.width, svg.root.height
	if w.U == 0 {
		w = Value{100, Perc}
	}
	if h.U == 0 {
		h = Value{100, Perc}
	}
	return w, h
}

// ViewBox returns the optional value of the "viewBox" attribute
func (svg *SVGImage) ViewBox() *Rectangle { return svg.root.viewbox }

// Draw draws the parsed SVG image into the given `dst` output,
// with the given `width` and `height`.
// `textContext` is required to properly layout <text> tags. It may be ommited
// for svg content not displaying text.
func (svg *SVGImage) Draw(dst backend.Canvas, width, height Fl, textContext text.TextLayoutContext) {
	// gopdfgo: układem gospodarza (hostScale) jest układ, w którym obraz jest rysowany
	hostBaseScale := Fl(1)
	if scale := transformScale(dst.State().GetTransform()); scale > 0 {
		hostBaseScale = 1 / scale
	}
	svg.draw(dst, width, height, textContext, hostBaseScale)
}

// draw (gopdfgo) to Draw z podanym układem gospodarza (zob. hostBaseScale).
func (svg *SVGImage) draw(dst backend.Canvas, width, height Fl, textContext text.TextLayoutContext, hostBaseScale Fl) {
	var dims drawingDims

	dims.concreteWidth, dims.concreteHeight = width, height

	if vb := svg.ViewBox(); vb != nil {
		dims.innerWidth, dims.innerHeight = vb.Width, vb.Height
	} else {
		dims.innerWidth, dims.innerHeight = width, height
	}

	dims.fontSize = defaultFontSize
	if svg.parentFontSize > 0 {
		dims.fontSize = svg.parentFontSize
	}

	dims.setupDiagonal()

	svg.textContext = textContext
	svg.hostBaseScale = hostBaseScale
	if svg.background != nil && svg.background.A > 0 {
		dst.OnNewStack(func() {
			dst.Rectangle(0, 0, width, height)
			dst.State().SetColorRgba(*svg.background, false)
			dst.Paint(backend.FillNonZero)
		})
	}
	svg.drawNode(dst, svg.root, dims, true)
}

// if paint is false, only the path operations are executed, not the actual filling or drawing
// moreover, no new graphic stack is created
func (svg *SVGImage) drawNode(dst backend.Canvas, node *svgNode, dims drawingDims, paint bool) {
	dims.fontSize = node.attributes.fontSize.Resolve(dims.fontSize, dims.fontSize)

	// gopdfgo: element <text> jest składany w całości przed rysowaniem (zob. text_layout.go): jego
	// potomkowie dostają gotowe ciągi glifów, a prostokąt otaczający jest znany od razu (wypełnienie
	// objectBoundingBox, filtry, maski)
	if t, ok := node.graphicContent.(*textSpan); ok && t.isText {
		svg.layoutText(dst, node, dims)
	}

	paintTask := func() {
		// gopdfgo: filtr działa w układzie użytkownika elementu, czyli po jego przekształceniu, a przed
		// przycięciem, maską i kryciem (Filter Effects 1 § 5, CSS Masking 1 § 3); źródło przesuwało
		// (feOffset) cały element przed przekształceniem
		effect := svg.definitions.filters[node.filterID]
		if !paint {
			effect = nil
		} else if effect != nil {
			svg.definitions.resolveFilterHref(effect)
		}

		// apply transform attribute
		svg.applyTransform(dst, node, dims)

		// create sub group for opacity
		opacity := node.attributes.opacity
		var originalDst1 backend.Canvas
		if paint && 0 <= opacity && opacity < 1 {
			var x, y, width, height Fl = 0, 0, dims.innerWidth, dims.innerHeight
			if box, ok := node.resolveBoundingBox(dims, true); ok {
				x, y, width, height = box.X, box.Y, box.Width, box.Height
			}
			// gopdfgo: wynik filtra (np. cień) wychodzi poza ramkę elementu: grupa krycia obejmuje
			// cały obszar filtra
			if effect != nil {
				bbox, hasBBox := svg.objectBoundingBox(dst, node, dims)
				if region, ok := effect.resolveRegion(bbox, hasBBox, dims); ok {
					x, y, width, height = region.X, region.Y, region.Width, region.Height
				}
			}
			originalDst1 = dst
			// gopdfgo: grupa zaczyna od tożsamości, a jest rysowana w bieżącym układzie (hostScale)
			savedHostBase := svg.hostBaseScale
			svg.hostBaseScale *= transformScale(dst.State().GetTransform())
			defer func() { svg.hostBaseScale = savedHostBase }()
			dst = dst.NewGroup(x, y, width, height)
		}

		// clip
		if cp, has := svg.definitions.clipPaths[node.clipPathID]; has {
			svg.applyClipPath(dst, cp, node, dims)
		}

		// gopdfgo: maska działa na cały element razem z potomkami (CSS Masking 1 § 7), więc nakładamy
		// ją przed rysowaniem; źródło robiło to po narysowaniu dzieci, przez co maska na <g> i na
		// foreignObject nie obejmowała treści (test WPT will-change-under-mask)
		if ma, has := svg.definitions.masks[node.maskID]; has {
			svg.applyMask(dst, ma, node, dims)
		}

		// gopdfgo: treść elementu rysowana na podanym płótnie: wprost albo do grupy źródłowej filtra
		content := func(dst backend.Canvas) {
			// gopdfgo: text-anchor działa na każdy fragment tekstu osobno już przy składzie (layoutText);
			// źródło przesuwało tu całą grupę elementu <text> o szerokość jego prostokąta otaczającego
			text, isText := node.graphicContent.(*textSpan)

			// manage display and visibility
			display := node.attributes.display
			visible := node.attributes.visible

			// draw the node itself : it is done in three steps
			// 	1) resolve paint options and apply it
			// 	2) apply the path operation
			// 	3) conclude by calling Paint

			// gopdfgo: wypełnienie gradientem rysujemy bez wzorca kafelkowego: ścieżka kształtu staje się
			// przycięciem, w którym malujemy cieniowanie, a obrys (jeśli jest) rysujemy po nim drugą kopią
			// ścieżki, jak w kolejności malowania SVG (fill, potem stroke). Zob. gradient.directFill.
			var directFill func(dst backend.Canvas)
			if paint && visible && isPlainShape(node.graphicContent) {
				directFill = svg.directGradientFill(node, dims)
			}

			var vertices []vertex
			if directFill != nil {
				dst.OnNewStack(func() {
					vertices = node.graphicContent.draw(dst, &node.attributes, svg, dims)
					dst.State().Clip(node.isFillEvenOdd)
					directFill(dst)
				})
			}

			doFill, doStroke := svg.applyPainters(dst, node, dims, directFill != nil)

			// gopdfgo: niewidoczny tekst (visibility: hidden) nadal zajmuje miejsce w składzie (layoutText),
			// więc dalsze fragmenty stoją tam, gdzie powinny (SVG 2 § 11.8, test WPT
			// text-visibility-hidden-tspan-visible)
			if visible && node.graphicContent != nil && (directFill == nil || doStroke) {
				vertices = node.graphicContent.draw(dst, &node.attributes, svg, dims)
			}

			// then recurse
			if display {
				childDims := nestedViewportDims(node, dims)
				for _, child := range node.children {
					svg.drawNode(dst, child, childDims, paint)

					childText, isChildText := child.graphicContent.(*textSpan)
					if visibleTextChild := (isText && isChildText && child.visible); visibleTextChild {
						if bb := childText.textBoundingBox; bb != emptyBbox {
							text.textBoundingBox.union(bb)
						}
					}
				}
			}

			// do the actual painting :
			// paint by filling and stroking the given node onto the graphic target
			if paint && !isText && (directFill == nil || doStroke) {
				dst.Paint(newPaintOp(doFill, doStroke, node.isFillEvenOdd))
			}

			// draw markers
			if len(vertices) != 0 {
				svg.drawMarkers(dst, vertices, node, dims, paint)
			}
		}
		if effect == nil || !svg.drawFiltered(dst, effect, node, dims, content) {
			content(dst)
		}

		// apply opacity group and restore original target
		if paint && 0 <= opacity && opacity < 1 {
			originalDst1.DrawWithOpacity(opacity, dst)
			dst = originalDst1 // actually not used
		}
	}

	if paint {
		dst.OnNewStack(paintTask)
	} else {
		paintTask()
	}
}

// vertices are the resolved vertices computed when drawing the shape
func (svg *SVGImage) drawMarkers(dst backend.Canvas, vertices []vertex, node *svgNode, dims drawingDims, paint bool) {
	const (
		start uint8 = iota
		mid
		end
	)
	commonMarker := svg.definitions.markers[node.markerID]

	// [start, mid, end] defautling to the common marker
	markers := [3]*marker{commonMarker, commonMarker, commonMarker}

	if marker := svg.definitions.markers[node.markerStartID]; marker != nil {
		markers[start] = marker
	}
	if marker := svg.definitions.markers[node.markerMidID]; marker != nil {
		markers[mid] = marker
	}
	if marker := svg.definitions.markers[node.markerEndID]; marker != nil {
		markers[end] = marker
	}

	for i, vertex := range vertices {
		position := mid
		if i == 0 {
			position = start
		} else if i == len(vertices)-1 {
			position = end
		}

		marker := markers[position]
		if marker == nil {
			continue
		}

		// calculate position, scale and clipping
		var (
			clipBox        Rectangle
			scaleX, scaleY Fl
		)
		translateX, translateY := dims.point(marker.refX, marker.refY)
		markerWidth, markerHeight := dims.point(marker.markerWidth, marker.markerHeight)
		if vb := marker.viewbox; vb != nil {
			scaleX, scaleY, _, _ = marker.preserveAspectRatio.resolveTransforms(markerWidth, markerHeight, marker.viewbox, &point{translateX, translateY})

			clipViewbox := *vb
			if marker.viewbox != nil {
				clipViewbox = *marker.viewbox
			}

			xPosition, yPosition := marker.preserveAspectRatio.xPosition, marker.preserveAspectRatio.yPosition

			if xPosition == "mid" {
				clipViewbox.X += (clipViewbox.Width - markerWidth/scaleX) / 2
			} else if xPosition == "max" {
				clipViewbox.X += clipViewbox.Width - markerWidth/scaleX
			}

			if yPosition == "mid" {
				clipViewbox.Y += (clipViewbox.Height - markerHeight/scaleY) / 2
			} else if yPosition == "max" {
				clipViewbox.Y += clipViewbox.Height - markerHeight/scaleY
			}

			clipBox = Rectangle{clipViewbox.X, clipViewbox.Y, markerWidth / scaleX, markerHeight / scaleY}
		} else {
			scaleX, scaleY = 1, 1
			clipBox = Rectangle{0, 0, markerWidth, markerHeight}
		}

		// scale
		if !marker.isUnitsUserSpace {
			scale := dims.length(node.attributes.strokeWidth)
			// gopdfgo: przy vector-effect: non-scaling-stroke markerUnits="strokeWidth" bierze grubość
			// obrysu w układzie użytkownika, czyli podzieloną przez skalę względem gospodarza
			// (SVG 2 § 13.7.2, test WPT marker-units-strokewidth-non-scaling-stroke)
			if node.nonScalingStroke {
				if hs := svg.hostScale(dst); hs > 0 {
					scale /= hs
				}
			}
			scaleX *= scale
			scaleY *= scale
		}

		// override angle
		angle := vertex.angle
		nodeAngle := marker.orient
		if nodeAngle.U != auto && nodeAngle.U != autoStartReverse {
			angle = nodeAngle.V * math.Pi / 180 // convert from degrees to radians
		} else if nodeAngle.U == autoStartReverse && position == start {
			angle += math.Pi
		}

		// draw marker path
		for _, child := range marker.children {
			dst.OnNewStack(func() {
				// gopdfgo: źródło wyznaczało kąt, ale go nie stosowało; układ znacznika to
				// translate(wierzchołek) · rotate(kąt) · scale (SVG 2 § 13.7.3)
				dst.State().Transform(matrix.Translation(vertex.x, vertex.y))
				if angle != 0 {
					dst.State().Transform(matrix.Rotation(angle))
				}
				dst.State().Transform(matrix.Scaling(scaleX, scaleY))
				dst.State().Transform(matrix.Translation(-translateX, -translateY))

				overflow := marker.overflow
				if overflowClips(overflow, true) { // gopdfgo: także clip (zob. overflowClips)
					dst.Rectangle(clipBox.X, clipBox.Y, clipBox.Width, clipBox.Height)
					dst.State().Clip(false)
				}

				// gopdfgo: kształt jest elementem kontekstu dla treści znacznika (SVG 2 § 13.4.1)
				svg.withPaintContext(node, func() { svg.drawNode(dst, child, dims, paint) })
			})
		}

	}
}

// compute scale and translation needed to preserve ratio.
// [translate] is optional :
// for marker tags, [translate] should be the resolved refX and refY values;
// otherwise, it should be nil.
func (pr preserveAspectRatio) resolveTransforms(width, height Fl, viewbox *Rectangle, translate *point) (scaleX, scaleY, translateX, translateY Fl) {
	if viewbox == nil {
		return 1, 1, 0, 0
	}

	viewboxWidth, viewboxHeight := viewbox.Width, viewbox.Height

	scaleX, scaleY = 1, 1
	if viewboxWidth != 0 {
		scaleX = width / viewboxWidth
	}
	if viewboxHeight != 0 {
		scaleY = height / viewboxHeight
	}

	if !pr.none {
		if pr.slice {
			scaleX = max(scaleX, scaleY)
		} else {
			scaleX = min(scaleX, scaleY)
		}
		scaleY = scaleX
	}

	if translate != nil {
		translateX, translateY = translate.x, translate.y
	} else {
		if pr.xPosition == "mid" {
			translateX = (width - viewboxWidth*scaleX) / 2
		} else if pr.xPosition == "max" {
			translateX = width - viewboxWidth*scaleX
		}

		if pr.yPosition == "mid" {
			translateY += (height - viewboxHeight*scaleY) / 2
		} else if pr.yPosition == "max" {
			translateY += height - viewboxHeight*scaleY
		}
	}

	translateX -= viewbox.X * scaleX
	translateY -= viewbox.Y * scaleY

	return
}

// resolve units and compose the transforms
func aggregateTransforms(transforms []transform, fontSize, diagonal Fl) matrix.Transform {
	// aggregate the transformations
	mat := matrix.Identity()
	for _, transform := range transforms {
		transform.applyTo(&mat, fontSize, diagonal)
	}
	return mat
}

// gopdfgo: przekształcenie elementu liczy nodeTransform (transform-origin, transform-box); dst służy
// do wyznaczenia ramki elementu <text> i grup, może być nil
func (svg *SVGImage) applyTransform(dst backend.Canvas, node *svgNode, dims drawingDims) {
	if len(node.attributes.transforms) == 0 { // do not apply a useless identity transform
		return
	}

	mat := svg.nodeTransform(dst, node, dims)
	if mat.Determinant() != 0 {
		dst.State().Transform(mat)
	}
}

// transformBox (gopdfgo) to wartość transform-box (CSS Transforms 1 § 7) dla elementu SVG.
type transformBox uint8

const (
	viewBoxTransform   transformBox = iota // view-box, wartość początkowa
	fillBoxTransform                       // fill-box, także content-box
	strokeBoxTransform                     // stroke-box, także border-box
)

// parseTransformBox (gopdfgo): element SVG bez pola układu CSS używa fill-box w miejsce content-box
// i stroke-box w miejsce border-box (CSS Transforms 1 § 7); wartość nieznana to wartość początkowa.
func parseTransformBox(s string) transformBox {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "fill-box", "content-box":
		return fillBoxTransform
	case "stroke-box", "border-box":
		return strokeBoxTransform
	}
	return viewBoxTransform
}

// parseTransformOrigin (gopdfgo) czyta transform-origin (CSS Transforms 1 § 6): jedną, dwie albo
// trzy wartości (słowa kluczowe, długości, procenty); składowa z nie ma znaczenia w 2D. Dla elementów
// SVG wartością początkową jest 0 0; wartość niepoprawna też daje 0 0.
func parseTransformOrigin(s string) (out [2]Value) {
	fields := strings.Fields(strings.ToLower(strings.ReplaceAll(s, ",", " ")))
	if len(fields) == 0 || len(fields) > 3 {
		return out
	}
	// słowo kluczowe: wartość procentowa i oś (0 dowolna, 1 pozioma, 2 pionowa)
	keyword := func(f string) (Value, int, bool) {
		switch f {
		case "left":
			return Value{0, Perc}, 1, true
		case "right":
			return Value{100, Perc}, 1, true
		case "top":
			return Value{0, Perc}, 2, true
		case "bottom":
			return Value{100, Perc}, 2, true
		case "center":
			return Value{50, Perc}, 0, true
		}
		return Value{}, 0, false
	}
	type item struct {
		v    Value
		axis int
		kw   bool
	}
	var items []item
	for _, f := range fields[:min(len(fields), 2)] {
		if v, axis, ok := keyword(f); ok {
			items = append(items, item{v, axis, true})
			continue
		}
		v, err := parseValue(f)
		if err != nil {
			return [2]Value{}
		}
		items = append(items, item{v: v})
	}
	center := Value{50, Perc}
	if len(items) == 1 {
		if items[0].axis == 2 {
			return [2]Value{center, items[0].v}
		}
		return [2]Value{items[0].v, center}
	}
	a, b := items[0], items[1]
	if a.axis == 2 || b.axis == 1 {
		// para słów kluczowych w odwrotnej kolejności („top left”); długość nie może stać po słowie
		// pionowym na pierwszym miejscu ani przed poziomym na drugim
		if !a.kw || !b.kw || a.axis == 1 || b.axis == 2 {
			return [2]Value{}
		}
		a, b = b, a
	}
	if a.axis == 2 || b.axis == 1 {
		return [2]Value{}
	}
	return [2]Value{a.v, b.v}
}

// nodeTransform (gopdfgo) zwraca przekształcenie elementu: translate(punkt odniesienia) ×
// przekształcenia × translate(−punkt odniesienia) (CSS Transforms 1 § 6, macierz przekształcenia
// bieżącego dla atrybutu i właściwości transform oraz translate, rotate, scale), gdzie punkt to
// transform-origin w polu odniesienia (§ 7): ramce obiektu (fill-box), ramce z obrysem (stroke-box)
// albo najbliższym oknie widoku SVG (view-box: początek układu współrzędnych i rozmiar viewBox).
// Procenty translate też odnoszą się do pola odniesienia (§ 13). Ramkę elementu <text> i grupy
// liczymy z dst (zob. objectBoundingBox); bez dst bierzemy ramkę samego kształtu.
func (svg *SVGImage) nodeTransform(dst backend.Canvas, node *svgNode, dims drawingDims) matrix.Transform {
	attrs := &node.attributes
	if len(attrs.transforms) == 0 {
		return matrix.Identity()
	}
	needBox := attrs.transformOrigin[0].V != 0 || attrs.transformOrigin[1].V != 0 || attrs.transformBox != viewBoxTransform
	for _, tr := range attrs.transforms {
		if tr.kind == translate && (tr.args[0].U == Perc || tr.args[1].U == Perc) {
			needBox = true
		}
	}
	if !needBox {
		return aggregateTransforms(attrs.transforms, dims.fontSize, dims.innerDiagonal)
	}
	box := Rectangle{0, 0, dims.innerWidth, dims.innerHeight}
	if attrs.transformBox != viewBoxTransform {
		var ok bool
		// ramki obrysu nieskalowanego (vector-effect: non-scaling-stroke) nie da się wyznaczyć przed
		// przekształceniem, więc stroke-box działa wtedy jak fill-box (csswg-drafts#9640, testy WPT
		// transform-box/svgbox-stroke-box-003, -004)
		if attrs.transformBox == strokeBoxTransform && !attrs.nonScalingStroke {
			box, ok = node.resolveBoundingBox(dims, true)
		}
		if !ok && dst != nil {
			box, ok = svg.objectBoundingBox(dst, node, dims)
		} else if !ok {
			box, ok = node.resolveBoundingBox(dims, false)
		}
		if !ok {
			box = Rectangle{} // element bez ramki: pole 0 0 0 0
		}
	}
	ox := box.X + attrs.transformOrigin[0].Resolve(dims.fontSize, box.Width)
	oy := box.Y + attrs.transformOrigin[1].Resolve(dims.fontSize, box.Height)
	mat := matrix.Translation(ox, oy)
	for _, tr := range attrs.transforms {
		if tr.kind == translate {
			for i, ref := range [2]Fl{box.Width, box.Height} {
				if tr.args[i].U == Perc {
					tr.args[i] = Value{tr.args[i].V * ref / 100, Px}
				}
			}
		}
		tr.applyTo(&mat, dims.fontSize, dims.innerDiagonal)
	}
	mat.Translate(-ox, -oy)
	return mat
}

func (svg *SVGImage) applyClipPath(dst backend.Canvas, clipPath *clipPath, node *svgNode, dims drawingDims) {
	oldCtm := dst.State().GetTransform()

	if clipPath.isUnitsBBox {
		x, y := dims.point(node.attributes.x, node.attributes.y)
		width, height := dims.point(node.attributes.width, node.attributes.height)
		dst.State().Transform(matrix.New(width, 0, 0, height, x, y))
	}

	svg.drawNode(dst, &clipPath.svgNode, dims, false)

	// At least set the clipping area to an empty path, so that it’s
	// totally clipped when the clipping path is empty.
	dst.Rectangle(0, 0, 0, 0)
	dst.State().Clip(false)
	newCtm := dst.State().GetTransform()
	// gopdfgo: Transform dokłada macierz po prawej stronie (CTM' = CTM × M, zob. matrix.Mul3), więc
	// powrót do oldCtm wymaga M = newCtm⁻¹ × oldCtm; źródło mnożyło w odwrotnej kolejności, co przy
	// clipPathUnits="objectBoundingBox" przesuwało i skalowało dalsze rysowanie elementu (zwykle poza
	// obszar przycięcia).
	if err := newCtm.Invert(); err == nil {
		dst.State().Transform(matrix.Mul(newCtm, oldCtm))
	}
}

func (svg *SVGImage) applyMask(dst backend.Canvas, mask mask, node *svgNode, dims drawingDims) {
	// gopdfgo: maskUnits domyślnie objectBoundingBox, a obszar maski w tych jednostkach to ułamki
	// (lub procenty) ramki elementu, łącznie z jej położeniem; maskContentUnits="objectBoundingBox"
	// rysuje treść w układzie ramki (CSS Masking 1 § 7.2–7.3, test WPT foreignObject/masked).
	// Źródło brało za ramkę tylko szerokość i wysokość elementu, domyślnie userSpaceOnUse,
	// a maskContentUnits pomijało. Bez ramki (np. <g>) zostaje obszar względem okna.
	bbox, hasBBox := node.resolveBoundingBox(dims, false)
	var x, y, width, height Fl
	if mask.isUnitsBBox && hasBBox {
		frac := func(v Value) Fl {
			if v.U == Perc {
				return v.V / 100
			}
			return v.V
		}
		x = bbox.X + frac(mask.x)*bbox.Width
		y = bbox.Y + frac(mask.y)*bbox.Height
		width = frac(mask.width) * bbox.Width
		height = frac(mask.height) * bbox.Height
	} else {
		x = mask.x.Resolve(dims.fontSize, dims.innerWidth)
		y = mask.y.Resolve(dims.fontSize, dims.innerHeight)
		width = mask.width.Resolve(dims.fontSize, dims.innerWidth)
		height = mask.height.Resolve(dims.fontSize, dims.innerHeight)
	}
	mask.x, mask.y = Value{x, Px}, Value{y, Px}
	mask.width, mask.height = Value{width, Px}, Value{height, Px}
	if mask.isContentBBox && hasBBox {
		mask.transforms = append([]transform{{kind: customMatrix, args: [6]Value{
			{bbox.Width, Px}, {}, {}, {bbox.Height, Px}, {bbox.X, Px}, {bbox.Y, Px},
		}}}, mask.transforms...)
	}

	alpha := dst.NewGroup(x, y, width, height)
	// gopdfgo: tam, gdzie maska nic nie rysuje, jej luminancja wynosi 0 i element jest ukryty (CSS
	// Masking 1 § 7.1); pusta grupa maski bywała w PDF traktowana jak brak maski (poppler), więc
	// obszar maski wypełniamy najpierw czernią (test WPT symbol-in-mask)
	alpha.OnNewStack(func() {
		alpha.Rectangle(x, y, width, height)
		alpha.State().SetColorRgba(parser.RGBA{A: 1}, false)
		alpha.Paint(backend.FillNonZero)
	})
	svg.drawNode(alpha, &mask.svgNode, dims, true)
	dst.State().SetAlphaMask(alpha)
}

// ImageLoader is used to resolve and process image url found in SVG files.
type ImageLoader = func(url string) (backend.Image, error)

// Parse parsed the given SVG source data. Warnings are
// logged for unsupported elements.
// An error is returned for invalid documents.
// `baseURL` is used as base path for url resources.
// `urlFetcher` is required to handle linked SVG documents like in <use> tags.
// `imageLoader` is required to handle inner images.
func Parse(svg io.Reader, baseURL string, imageLoader ImageLoader, urlFetcher utils.UrlFetcher) (*SVGImage, error) {
	root, err := parseSVGDocument(svg) // gopdfgo: XML, zob. parseSVGDocument
	if err != nil {
		return nil, err
	}

	return parseNode(root, baseURL, imageLoader, urlFetcher, true, nil)
}

// ParseNode is the same as Parse but works with an already parsed
// svg input.
func ParseNode(root *html.Node, baseURL string, imageLoader ImageLoader, urlFetcher utils.UrlFetcher) (*SVGImage, error) {
	return parseNode(root, baseURL, imageLoader, urlFetcher, false, nil)
}

// ParseInlineNode (gopdfgo) to ParseNode dla elementu <svg> osadzonego w dokumencie HTML, stylowanego
// kaskadą tego dokumentu (zob. DocumentStyle); doc == nil działa jak ParseNode.
func ParseInlineNode(root *html.Node, baseURL string, imageLoader ImageLoader, urlFetcher utils.UrlFetcher, doc *DocumentStyle) (*SVGImage, error) {
	return parseNode(root, baseURL, imageLoader, urlFetcher, false, doc)
}

// parseNode: standalone (gopdfgo) mówi, że SVG jest osobnym dokumentem (obrazem), a nie elementem
// osadzonym w HTML, którego tło rysuje układ HTML.
func parseNode(root *html.Node, baseURL string, imageLoader ImageLoader, urlFetcher utils.UrlFetcher, standalone bool, doc *DocumentStyle) (*SVGImage, error) {
	tree, err := newSVGContext(root, baseURL, urlFetcher, doc)
	if err != nil {
		return nil, err
	}

	tree.imageLoader = imageLoader

	var out SVGImage
	if doc != nil && doc.FontSize > 0 {
		out.parentFontSize = doc.FontSize
	}
	out.definitions = newDefinitions()
	if standalone {
		out.background = rootBackground(tree.root)
	}
	// Build the drawable items by parsing attributes
	out.root, err = tree.processNode(tree.root, out.definitions)
	if err != nil {
		return nil, err
	}
	if standalone {
		out.synthesizedViewBox = synthesizeViewBox(out.root)
	}
	out.hasLinks = resolveLinkTargets(out.root, baseURL) // gopdfgo: odnośniki <a>, zob. links.go
	out.views = collectViews(tree)                       // gopdfgo: widoki <view>, zob. view.go
	// gopdfgo: przekształcenia zewnętrznego <svg> osadzonego w HTML (atrybut transform, właściwości
	// transform, translate, rotate, scale) stosuje układ HTML jako transform pola CSS (SVG 2 § 8.5,
	// CSS Transforms 1 § 12; zob. findStyleAttributes). Źródło stosowało je drugi raz wewnątrz obrazu.
	if !standalone && out.root != nil {
		out.root.transforms = nil
	}

	return &out, nil
}

// synthesizeViewBox (gopdfgo): dokument SVG wyświetlany jako obraz (img, background-image, object, embed),
// który ma width i height w jednostkach bezwzględnych, ale nie ma viewBox, dostaje viewBox
// "0 0 width height" i skaluje się do rozmiaru obrazu, jak w przeglądarkach (HTML, przetwarzanie obrazów
// SVG: „synthesize a viewBox”; SVG Integration § 4). Bez tego obraz był rysowany w skali 1:1 i ucinany:
// flagi i herby z Wikimedia Commons, zapisane bez viewBox, pokazywały tylko lewy górny róg. SVG osadzone
// w HTML się nie skaluje, więc dotyczy to tylko dokumentów samodzielnych.
func synthesizeViewBox(root *svgNode) bool {
	if root == nil || root.viewbox != nil {
		return false
	}
	w, h := root.width, root.height
	if w.U == 0 || h.U == 0 || w.U == Perc || h.U == Perc || w.U == Em || h.U == Em || w.U == Ex || h.U == Ex || w.U == Rem || h.U == Rem {
		return false
	}
	width, height := w.Resolve(defaultFontSize, 0), h.Resolve(defaultFontSize, 0)
	if width > 0 && height > 0 {
		root.viewbox = &Rectangle{Width: width, Height: height}
		// obraz rozciąga się niezależnie w poziomie i w pionie, jak przy preserveAspectRatio="none":
		// tak rysuje Chrome 157 w <img> i w background-image (pomiar 2026-11-20); przy tych samych
		// proporcjach daje to zwykłe skalowanie
		if s, ok := root.graphicContent.(svg); ok {
			s.preserveRatio.none = true
			root.graphicContent = s
		}
		return true
	}
	return false
}

// DrawAsDocument (gopdfgo) rysuje obraz jak osobny dokument (<object>, <embed>): bez viewBox dopisanego
// przez synthesizeViewBox, więc dokument bez viewBox nie skaluje się, tylko dostaje obszar o rozmiarze
// elementu, jak w Chrome 157.
func (img *SVGImage) DrawAsDocument(dst backend.Canvas, width, height Fl, textContext text.TextLayoutContext) {
	if !img.synthesizedViewBox {
		img.Draw(dst, width, height, textContext)
		return
	}
	viewbox, content := img.root.viewbox, img.root.graphicContent
	img.root.viewbox = nil
	if s, ok := content.(svg); ok {
		s.preserveRatio.none = false
		img.root.graphicContent = s
	}
	// dokument jest przycinany do obszaru elementu, jak w Chrome
	dst.OnNewStack(func() {
		dst.Rectangle(0, 0, width, height)
		dst.State().Clip(false)
		img.Draw(dst, width, height, textContext)
	})
	img.root.viewbox, img.root.graphicContent = viewbox, content
}

// rootBackground (gopdfgo) odczytuje kolor tła elementu głównego: background-color albo background
// złożone z samego koloru.
func rootBackground(root *cascadedNode) *parser.RGBA {
	if root == nil {
		return nil
	}
	for _, name := range []string{"background-color", "background"} {
		value := strings.TrimSpace(root.attrs[name])
		if value == "" {
			continue
		}
		color := parser.ParseColorString(value)
		if color.IsNone() {
			continue
		}
		rgba := color.RGBA
		return &rgba
	}
	return nil
}

// svgNode is a node in a drawable SVG tree
type svgNode struct {
	graphicContent drawable
	children       []*svgNode
	attributes
}

// drawingDims stores the configuration to use
// when drawing
type drawingDims struct {
	// width and height as requested by the user
	// when calling Draw.
	concreteWidth, concreteHeight Fl

	fontSize Fl

	// either the root viewbox width and height,
	// or the concreteWidth, concreteHeight if
	// no viewBox is provided
	innerWidth, innerHeight Fl

	// cached value of norm(innerWidth, innerHeight) / sqrt(2)
	innerDiagonal Fl

	// cached value of norm(concreteWidth, concreteHeight) / sqrt(2)
	normalizedDiagonal Fl
}

// nestedViewportDims (gopdfgo): zagnieżdżony <svg> (także z <use> i <symbol>) ustanawia nowe okno
// widoku, względem którego potomkowie liczą procenty (SVG 2 § 8.9) i transform-box: view-box (CSS
// Transforms 1 § 7): rozmiar viewBox albo, bez niego, szerokość i wysokość elementu. Źródło liczyło
// je zawsze względem okna elementu głównego.
func nestedViewportDims(node *svgNode, dims drawingDims) drawingDims {
	s, ok := node.graphicContent.(svg)
	if !ok || s.isRoot {
		return dims
	}
	if vb := node.attributes.viewbox; vb != nil && vb.Width > 0 && vb.Height > 0 {
		dims.innerWidth, dims.innerHeight = vb.Width, vb.Height
	} else {
		w, h := node.attributes.width, node.attributes.height
		if w.U == 0 {
			w = Value{100, Perc}
		}
		if h.U == 0 {
			h = Value{100, Perc}
		}
		dims.innerWidth, dims.innerHeight = dims.point(w, h)
	}
	dims.setupDiagonal()
	return dims
}

// update `innerDiagonal` and `normalizedDiagonal`
func (dims *drawingDims) setupDiagonal() {
	dims.innerDiagonal = Fl(math.Hypot(float64(dims.innerWidth), float64(dims.innerHeight)) / math.Sqrt2)
	dims.normalizedDiagonal = Fl(math.Hypot(float64(dims.concreteWidth), float64(dims.concreteHeight)) / math.Sqrt2)
}

// resolve the size of an x/y or width/height couple.
func (dims drawingDims) point(xv, yv Value) (x, y Fl) {
	x = xv.Resolve(dims.fontSize, dims.innerWidth)
	y = yv.Resolve(dims.fontSize, dims.innerHeight)
	return
}

// resolve a length
func (dims drawingDims) length(length Value) Fl {
	return length.Resolve(dims.fontSize, dims.innerDiagonal)
}

// box is a shared type for dimensions
// found in several SVG elements, which may be expressed with units
type box struct {
	x, y, width, height Value
}

// attributes stores the SVG attributes
// shared by all node types in the final rendering tree
type attributes struct {
	viewbox *Rectangle

	transforms []transform
	// transformOrigin, transformBox (gopdfgo): transform-origin i transform-box (CSS Transforms 1 § 6,
	// § 7); źródło ich nie znało i obracało każdy element wokół (0, 0)
	transformOrigin [2]Value
	transformBox    transformBox

	clipPathID, maskID, filterID                      string
	markerID, markerStartID, markerMidID, markerEndID string

	dashArray []Value

	fill, stroke painter // fill default to black, stroke to nothing

	box

	fontSize      Value
	strokeWidth   Value // default to 1px
	strokeOptions backend.StrokeOptions

	dashOffset Value

	opacity, strokeOpacity, fillOpacity Fl // default to 1

	isFillEvenOdd    bool
	display, visible bool

	// nonScalingStroke (gopdfgo): vector-effect: non-scaling-stroke (SVG 2 § 13.2); źródło tej
	// właściwości nie znało
	nonScalingStroke bool
}

func (tree *svgContext) processNode(node *cascadedNode, defs definitions) (*svgNode, error) {
	var children []*svgNode
	for _, c := range node.children {
		child, err := tree.processNode(c, defs)
		if err != nil {
			return nil, err
		}
		if child == nil {
			continue // do not add useless node to the tree
		}
		// gopdfgo: element z niespełnionym atrybutem warunkowym nie jest rysowany (SVG 2 § 5.8.1), choć
		// pozostaje w definicjach; <switch> rysuje tylko pierwsze dziecko, którego warunki są spełnione
		// (SVG 2 § 5.8.2), z pominięciem elementów opisowych. Źródło rysowało wszystkie dzieci.
		// <symbol> nie jest rysowany wprost, tylko przez <use> (SVG 2 § 5.5; resolveUse zamienia go
		// na svg), a źródło rysowało go jak grupę (test WPT symbol-in-mask).
		if c.tag == "symbol" || !conditionsPass(c) || (node.tag == "switch" && (len(children) != 0 || isDescriptive(c.tag))) {
			continue
		}
		children = append(children, child)
	}

	// actual processing of the node, with the following cases
	//	- node used as definition, extracted from the svg tree
	//	- graphic element to display -> processGraphicNode

	// gopdfgo: definicja bez id nie może być wskazana; źródło zapisywało ją pod kluczem "",
	// przez co stosowała się do każdego elementu bez clip-path/mask/filter (np. <clipPath> bez id
	// zawierający <polygon> prowadził do nieskończonej rekursji, test WPT polygon-with-filtered-marker).
	id := node.attrs["id"]
	switch node.tag {
	case "filter":
		filters, err := newFilter(node)
		if err != nil {
			return nil, err
		}
		if id != "" {
			defs.filters[id] = filters
		}
	case "clipPath":
		cp, err := newClipPath(node, children)
		if err != nil {
			return nil, err
		}
		if id != "" {
			defs.clipPaths[id] = cp
		}
	case "mask":
		ma, err := newMask(node, children)
		if err != nil {
			return nil, err
		}
		if id != "" {
			defs.masks[id] = ma
		}
	case "marker":
		ma, err := newMarker(node, children)
		if err != nil {
			return nil, err
		}
		if id != "" {
			defs.markers[id] = ma
		}
	case "linearGradient", "radialGradient":
		grad, err := newGradient(node)
		if err != nil {
			return nil, err
		}
		if id != "" {
			defs.paintServers[id] = grad
		}
	case "pattern":
		pat, err := newPattern(node, children)
		if err != nil {
			return nil, err
		}
		if id != "" {
			defs.paintServers[id] = pat
		}
	case "use": // special case
		resolved, err := tree.resolveUse(node, defs)
		if err != nil {
			return nil, err
		}
		return resolved, nil
	case "defs":
		// children has been processed and registred,
		// so we discard the node, which is not needed anymore
	default:
		out, err := tree.processGraphicNode(node, children)
		if err != nil {
			return nil, err
		}
		// register node with id
		if id != "" {
			defs.nodes[id] = out
		}
		return out, nil
	}

	return nil, nil
}

// process a node to be displayed by building its content
func (tree *svgContext) processGraphicNode(node *cascadedNode, children []*svgNode) (*svgNode, error) {
	out := svgNode{children: children}

	var (
		err    error
		isText bool
	)
	switch node.tag {
	case "circle", "ellipse":
		out.graphicContent, err = newEllipse(node, tree)
	case "image":
		out.graphicContent, err = newImage(node, tree)
	case "line":
		out.graphicContent, err = newLine(node, tree)
	case "path":
		out.graphicContent, err = newPath(node, tree)
	case "polyline":
		out.graphicContent, err = newPolyline(node, tree)
	case "polygon":
		out.graphicContent, err = newPolygon(node, tree)
	case "rect":
		out.graphicContent, err = newRect(node, tree)
	case "svg":
		out.graphicContent, err = newSvg(node, tree)
	case "a", "text", "textPath", "tspan":
		out.graphicContent, err = newTextSpan(node)
		isText = true
	case "foreignObject": // gopdfgo: SVG 2 § 23.2, zob. foreign.go
		out.graphicContent, err = newForeignObject(node, tree)
	}

	if err != nil {
		return nil, fmt.Errorf("invalid element %s: %s", node.tag, err)
	}

	if isText {
		err = node.attrs.parseCommonAttributesForText(&out.attributes)
	} else {
		err = node.attrs.parseCommonAttributes(&out.attributes)
	}

	if err != nil {
		return nil, err
	}
	if node.tag == "svg" && node != tree.root {
		// gopdfgo: width i height zagnieżdżonego <svg> (także <symbol> wstawionego przez <use>) mają
		// wartość auto, czyli 100% (SVG 2 § 5.1.2 i § 5.6); źródło liczyło brak atrybutu jako 0, więc
		// symbol bez rozmiaru na <use> był w całości przycinany
		if out.width.U == 0 {
			out.width = Value{100, Perc}
		}
		if out.height.U == 0 {
			out.height = Value{100, Perc}
		}
	}
	if node.tag == "foreignObject" {
		// gopdfgo: visibility: hidden na foreignObject ukrywa tylko jego własne pudełko; potomkowie
		// z visibility: visible są rysowani (CSS Display 3 § 5.1), więc o widoczności treści decyduje
		// dokument HTML, któremu przekazujemy visibility (zob. inheritedForeignProperties)
		out.visible = out.display
	}

	return &out, nil
}

func (na nodeAttributes) parseBox(out *box) (err error) {
	out.x, err = parseValue(na["x"])
	if err != nil {
		return err
	}
	out.y, err = parseValue(na["y"])
	if err != nil {
		return err
	}
	out.width, err = parseValue(na["width"])
	if err != nil {
		return err
	}
	out.height, err = parseValue(na["height"])
	if err != nil {
		return err
	}

	return nil
}

func (na nodeAttributes) parseCommonAttributes(out *attributes) error {
	err := na.parseBox(&out.box)
	if err != nil {
		return err
	}
	err = na.parseSharedAttributes(out)
	return err
}

func (na nodeAttributes) parseSharedAttributes(out *attributes) error {
	var err error
	// gopdfgo: niepoprawny viewBox (np. trzy liczby) działa jak brak atrybutu (SVG 2, atrybut viewBox;
	// test WPT view-viewbox-invalid-fallback); źródło odrzucało cały dokument
	if out.viewbox, err = na.viewBox(); err != nil {
		out.viewbox = nil
	}

	out.fontSize, err = na.fontSize()
	if err != nil {
		return err
	}
	out.strokeWidth, err = na.strokeWidth()
	if err != nil {
		return err
	}

	out.opacity, err = parseOpacity(na["opacity"])
	if err != nil {
		return err
	}
	out.strokeOpacity, err = parseOpacity(na["stroke-opacity"])
	if err != nil {
		return err
	}
	out.fillOpacity, err = parseOpacity(na["fill-opacity"])
	if err != nil {
		return err
	}

	out.transforms, err = parseTransform(na["transform"])
	if err != nil {
		return err
	}
	// gopdfgo: właściwości translate i scale (CSS Transforms 2 § 5.1) przed transform (§ 6); źródło
	// ich nie znało
	out.transforms = append(na.individualTransforms(), out.transforms...)
	out.transformOrigin = parseTransformOrigin(na["transform-origin"])
	out.transformBox = parseTransformBox(na["transform-box"])

	out.stroke, err = newPainter(na["stroke"])
	if err != nil {
		return err
	}
	out.fill, err = na.fill()
	if err != nil {
		return err
	}
	out.isFillEvenOdd = na["fill-rull"] == "evenodd"

	out.dashOffset, err = parseValue(na["stroke-dashoffset"])
	if err != nil {
		return err
	}
	out.dashArray, err = parseValues(na["stroke-dasharray"])
	if err != nil {
		return err
	}

	out.strokeOptions.LineCap = na.lineCap()
	out.strokeOptions.LineJoin = na.lineJoin()
	out.strokeOptions.MiterLimit, err = na.miterLimit()
	if err != nil {
		return err
	}

	out.filterID = parseURLFragment(na["filter"])
	out.clipPathID = parseURLFragment(na["clip-path"])
	out.maskID = parseURLFragment(na["mask"])

	out.markerID = parseURLFragment(na["marker"])
	out.markerStartID = parseURLFragment(na["marker-start"])
	out.markerMidID = parseURLFragment(na["marker-mid"])
	out.markerEndID = parseURLFragment(na["marker-end"])

	out.display = na.display()
	out.visible = na.visible()
	out.nonScalingStroke = strings.TrimSpace(na["vector-effect"]) == "non-scaling-stroke"
	return nil
}

// does not parse "x" and "y" fields
func (na nodeAttributes) parseCommonAttributesForText(out *attributes) error {
	var err error
	out.width, err = parseValue(na["width"])
	if err != nil {
		return err
	}
	out.height, err = parseValue(na["height"])
	if err != nil {
		return err
	}

	err = na.parseSharedAttributes(out)
	return err
}

// isPlainShape (gopdfgo) mówi, czy element jest kształtem (rect, circle, ellipse, line, polyline,
// polygon, path), którego ścieżkę można narysować drugi raz bez skutków ubocznych.
func isPlainShape(d drawable) bool {
	switch d.(type) {
	case rect, *rect, line, *line, polyline, *polyline, ellipse, *ellipse, path, *path:
		return true
	}
	return false
}
