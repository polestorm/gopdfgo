package svg

import (
	"fmt"
	"io"
	"math"
	"net/url"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/css/selector"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/matrix"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type drawable interface {
	// Draws the node onto `dst` with the given dimensions.
	// It should return the vertices of the path for path, line, polyline and polygon elements
	// or nil
	draw(dst backend.Canvas, attrs *attributes, img *SVGImage, dims drawingDims) []vertex

	// computes the bounding box of the node, or returns false
	// if the node has no valid bounding box, like empty paths.
	boundingBox(attrs *attributes, dims drawingDims) (Rectangle, bool)
}

// <line> tag
type line struct {
	x1, y1, x2, y2 Value
}

func newLine(node *cascadedNode, _ *svgContext) (drawable, error) {
	var (
		out line
		err error
	)
	out.x1, err = parseValue(node.attrs["x1"])
	if err != nil {
		return nil, err
	}
	out.y1, err = parseValue(node.attrs["y1"])
	if err != nil {
		return nil, err
	}
	out.x2, err = parseValue(node.attrs["x2"])
	if err != nil {
		return nil, err
	}
	out.y2, err = parseValue(node.attrs["y2"])
	if err != nil {
		return nil, err
	}

	return out, nil
}

func atan2(x, y Fl) Fl { return Fl(math.Atan2(float64(x), float64(y))) }

func (l line) draw(dst backend.Canvas, _ *attributes, _ *SVGImage, dims drawingDims) []vertex {
	x1, y1 := dims.point(l.x1, l.y1)
	x2, y2 := dims.point(l.x2, l.y2)
	dst.MoveTo(x1, y1)
	dst.LineTo(x2, y2)

	angle := atan2(y2-y1, x2-x1)
	return []vertex{
		{x1, y1, angle},
		{x2, y2, angle},
	}
}

// <rect> tag
type rect struct {
	// x, y, width, height are common attributes

	rx, ry Value
	// gopdfgo: rx/ry = auto (SVG 2 § 10.2) — zob. ellipse.
	autoX, autoY bool
}

func newRect(node *cascadedNode, _ *svgContext) (drawable, error) {
	rx_, ry_ := strings.TrimSpace(node.attrs["rx"]), strings.TrimSpace(node.attrs["ry"])

	var (
		out rect
		err error
	)
	if rx_ == "" || strings.EqualFold(rx_, "auto") {
		out.autoX, rx_ = true, ""
	}
	if ry_ == "" || strings.EqualFold(ry_, "auto") {
		out.autoY, ry_ = true, ""
	}
	out.rx, err = parseValue(rx_)
	if err != nil {
		return nil, err
	}
	// gopdfgo: źródło parsowało tu rx_ zamiast ry_, więc ry zawsze równało się rx.
	// Według SVG 2 § 10.2 promień auto przyjmuje bezwzględną wartość drugiego.
	out.ry, err = parseValue(ry_)
	if err != nil {
		return nil, err
	}

	return out, nil
}

func (r rect) draw(dst backend.Canvas, attrs *attributes, _ *SVGImage, dims drawingDims) []vertex {
	width, height := dims.point(attrs.width, attrs.height)
	if width <= 0 || height <= 0 { // nothing to draw
		return nil
	}
	x, y := dims.point(attrs.x, attrs.y)
	rx, ry := dims.point(r.rx, r.ry)
	if r.autoX {
		rx = ry
	} else if r.autoY {
		ry = rx
	}

	if rx == 0 || ry == 0 { // no border radius
		dst.Rectangle(x, y, width, height)
		return nil
	}

	if rx > width/2 {
		rx = width / 2
	}
	if ry > height/2 {
		ry = height / 2
	}

	// Inspired by Cairo Cookbook
	// http://cairographics.org/cookbook/roundedrectangles/
	const arcToBezier = 4 * (math.Sqrt2 - 1) / 3
	c1, c2 := arcToBezier*rx, arcToBezier*ry

	dst.MoveTo(x+rx, y)
	dst.LineTo(x+width-rx, y)
	dst.CubicTo(x+width-rx+c1, y, x+width, y+c2, x+width, y+ry)
	dst.LineTo(x+width, y+height-ry)
	dst.CubicTo(
		x+width, y+height-ry+c2, x+width+c1-rx, y+height,
		x+width-rx, y+height)
	dst.LineTo(x+rx, y+height)
	dst.CubicTo(x+rx-c1, y+height, x, y+height-c2, x, y+height-ry)
	dst.LineTo(x, y+ry)
	dst.CubicTo(x, y+ry-c2, x+rx-c1, y, x+rx, y)
	dst.LineTo(x+rx, y)

	return nil
}

// polyline or polygon
type polyline struct {
	points []point // x, y
	close  bool    // true for polygon
}

func newPolyline(node *cascadedNode, _ *svgContext) (drawable, error) {
	return parsePoly(node)
}

func newPolygon(node *cascadedNode, _ *svgContext) (drawable, error) {
	out, err := parsePoly(node)
	out.close = true
	return out, err
}

func parsePoly(node *cascadedNode) (polyline, error) {
	var out polyline

	pts, err := parsePoints(node.attrs["points"], nil, false)
	if err != nil {
		return out, err
	}

	// "If the attribute contains an odd number of coordinates, the last one will be ignored."
	out.points = make([]point, len(pts)/2)
	for i := range out.points {
		out.points[i].x = pts[2*i]
		out.points[i].y = pts[2*i+1]
	}

	return out, nil
}

func (r polyline) draw(dst backend.Canvas, _ *attributes, _ *SVGImage, _ drawingDims) []vertex {
	if len(r.points) == 0 {
		return nil
	}
	p1, points := r.points[0], r.points[1:]
	dst.MoveTo(p1.x, p1.y)

	// use 0 as the angle for the first point
	vertices := []vertex{{p1.x, p1.y, 0}}

	oldPoint := p1
	for _, point := range points {
		dst.LineTo(point.x, point.y)
		angle := atan2(point.x-oldPoint.x, point.y-oldPoint.y)
		vertices = append(vertices, vertex{point.x, point.y, angle})
	}

	if r.close {
		dst.ClosePath()
	}

	return vertices
}

// ellipse or circle
type ellipse struct {
	rx, ry, cx, cy Value
	// gopdfgo: rx/ry = auto (SVG 2 § 10.4) — promień „auto” przyjmuje
	// rozwiązaną (bezwzględną) wartość drugiego promienia.
	autoX, autoY bool
}

func newEllipse(node *cascadedNode, _ *svgContext) (drawable, error) {
	var rx_, ry_ string
	// gopdfgo: źródło brało dla <circle> i <ellipse> rx/ry z fallbackiem na r.
	// Według SVG 2 § 10.3–10.4 <circle> ma tylko r, a <ellipse> tylko rx/ry,
	// których wartością początkową jest auto (także jako właściwości CSS).
	if node.tag == "circle" {
		rx_, ry_ = node.attrs["r"], node.attrs["r"]
	} else {
		rx_, ry_ = strings.TrimSpace(node.attrs["rx"]), strings.TrimSpace(node.attrs["ry"])
	}

	var (
		out ellipse
		err error
	)
	if node.tag != "circle" {
		if rx_ == "" || strings.EqualFold(rx_, "auto") {
			out.autoX, rx_ = true, ""
		}
		if ry_ == "" || strings.EqualFold(ry_, "auto") {
			out.autoY, ry_ = true, ""
		}
	}
	out.rx, err = parseValue(rx_)
	if err != nil {
		return nil, err
	}
	out.ry, err = parseValue(ry_)
	if err != nil {
		return nil, err
	}
	out.cx, err = parseValue(node.attrs["cx"])
	if err != nil {
		return nil, err
	}
	out.cy, err = parseValue(node.attrs["cy"])
	if err != nil {
		return nil, err
	}

	return out, nil
}

// radii zwraca rozwiązane promienie z uwzględnieniem wartości auto.
func (e ellipse) radii(dims drawingDims) (rx, ry Fl) {
	rx, ry = dims.point(e.rx, e.ry)
	switch {
	case e.autoX && e.autoY:
		return 0, 0
	case e.autoX:
		rx = ry
	case e.autoY:
		ry = rx
	}
	return rx, ry
}

func (e ellipse) draw(dst backend.Canvas, _ *attributes, _ *SVGImage, dims drawingDims) []vertex {
	rx, ry := e.radii(dims)
	if rx == 0 || ry == 0 {
		return nil
	}
	ratioX := rx / math.SqrtPi
	ratioY := ry / math.SqrtPi
	cx, cy := dims.point(e.cx, e.cy)

	dst.MoveTo(cx+rx, cy)
	dst.CubicTo(cx+rx, cy+ratioY, cx+ratioX, cy+ry, cx, cy+ry)
	dst.CubicTo(cx-ratioX, cy+ry, cx-rx, cy+ratioY, cx-rx, cy)
	dst.CubicTo(cx-rx, cy-ratioY, cx-ratioX, cy-ry, cx, cy-ry)
	dst.CubicTo(cx+ratioX, cy-ry, cx+rx, cy-ratioY, cx+rx, cy)
	dst.LineTo(cx+rx, cy)

	return nil
}

// <path> tag
type path []pathItem

func newPath(node *cascadedNode, context *svgContext) (drawable, error) {
	out, err := context.pathParser.parsePath(node.attrs["d"])
	if err != nil {
		return nil, err
	}

	return path(out), err
}

func (p path) draw(dst backend.Canvas, _ *attributes, _ *SVGImage, _ drawingDims) []vertex {
	var (
		segmentStart point
		out          []vertex
	)
	for _, item := range p {
		item.draw(dst)
		angle := item.endAngle(segmentStart)
		segmentStart = item.endPoint() // update the starting point
		out = append(out, vertex{segmentStart.x, segmentStart.y, angle})
	}
	return out
}

// <svg> tag
type svg struct {
	preserveRatio    preserveAspectRatio
	isRoot           bool
	isOverflowHidden bool
}

func newSvg(node *cascadedNode, tree *svgContext) (drawable, error) {
	var out svg
	out.preserveRatio = node.attrs.aspectRatio()
	out.isRoot = node == tree.root
	if overflow := node.attrs["overflow"]; overflow == "" || overflow == "hidden" {
		out.isOverflowHidden = true
	}

	return out, nil
}

func (s svg) draw(dst backend.Canvas, attrs *attributes, img *SVGImage, dims drawingDims) []vertex {
	x, y := dims.point(attrs.x, attrs.y)
	dst.State().Transform(matrix.Translation(x, y))
	width, height := dims.concreteWidth, dims.concreteHeight
	if !s.isRoot {
		// gopdfgo: brak width/height zagnieżdżonego <svg> (także <symbol> i <svg> z <use>) to auto,
		// czyli 100% (SVG 2 § 8.2, § 5.6); źródło brało 0 i przycinało całą treść
		w, h := attrs.width, attrs.height
		if w.U == 0 {
			w = Value{100, Perc}
		}
		if h.U == 0 {
			h = Value{100, Perc}
		}
		width, height = dims.point(w, h)
	}

	// gopdfgo: korzeń bez viewBox nie jest skalowany: obszarem widoku jest konkretny rozmiar obrazu,
	// a długości procentowe liczą się względem niego (SVG 2 § 8.2 i 8.7, CSS Images 3 § 5.2; testy WPT
	// background-size/vector/*--nonpercent-width-nonpercent-height). Źródło tworzyło viewBox z atrybutów
	// width i height, więc obraz rysowany w innym rozmiarze był skalowany.
	viewbox := attrs.viewbox

	sx, sy, tx, ty := s.preserveRatio.resolveTransforms(width, height, viewbox, nil)
	if !s.isRoot && s.isOverflowHidden {
		dst.Rectangle(0, 0, width, height)
		dst.State().Clip(false)
	}

	dst.State().Transform(matrix.New(sx, 0, 0, sy, tx, ty))
	return nil
}

// <image> tag
type image struct {
	// width, height are common attributes

	img           backend.Image
	preserveRatio preserveAspectRatio
}

func newImage(node *cascadedNode, context *svgContext) (drawable, error) {
	baseURL := node.attrs["base"]
	if baseURL == "" {
		baseURL = context.baseURL
	}

	href := node.attrs["href"]
	url, err := utils.SafeUrljoin(baseURL, href, true)
	if err != nil {
		return nil, fmt.Errorf("invalid image source: %s", err)
	}
	img, err := context.imageLoader(url)
	if err != nil {
		return nil, fmt.Errorf("failed to load image: %s", err)
	}

	var out image
	out.img = img
	out.preserveRatio = node.attrs.aspectRatio()

	return out, nil
}

func (img image) draw(dst backend.Canvas, attrs *attributes, svg *SVGImage, dims drawingDims) []vertex {
	x, y := dims.point(attrs.x, attrs.y)
	dst.State().Transform(matrix.Translation(x, y))
	// base_url = node.get("{http://www.w3.org/XML/1998/namespace}base")
	// url = node.get_href(base_url || svg.url)
	// image = svg.context.get_image_from_uri(url, "image/*")
	// if image == nil {
	// 	return
	// }

	width, height := dims.point(attrs.width, attrs.height)
	intrinsicWidth, intrinsicHeight, intrinsicRatio := img.img.GetIntrinsicSize(1, pr.Float(dims.fontSize))
	if intrinsicWidth == nil && intrinsicHeight == nil {
		if intrinsicRatio == nil && width != 0 && height != 0 {
			// gopdfgo: obraz bez rozmiaru i proporcji (SVG bez width, height i viewBox) wypełnia
			// obszar elementu <image> (SVG 2 § 5.7, CSS Images 3 § 5.2: rozmiar domyślny obiektu);
			// źródło rysowało go w rozmiarze 300x150 pomniejszonym do obszaru
			intrinsicWidth, intrinsicHeight = pr.Float(width), pr.Float(height)
		} else if intrinsicRatio == nil || (width == 0 && height == 0) {
			intrinsicWidth, intrinsicHeight = pr.Float(300), pr.Float(150)
		} else if width == 0 {
			intrinsicWidth, intrinsicHeight = intrinsicRatio.V()*pr.Float(height), pr.Float(height)
		} else {
			intrinsicWidth, intrinsicHeight = pr.Float(width), pr.Float(width)/intrinsicRatio.V()
		}
	} else if intrinsicWidth == nil {
		intrinsicWidth = intrinsicRatio.V() * intrinsicHeight.V()
	} else if intrinsicHeight == nil {
		intrinsicHeight = intrinsicWidth.V() / intrinsicRatio.V()
	}
	intrinsic := Rectangle{0, 0, Fl(intrinsicWidth.V()), Fl(intrinsicHeight.V())}
	if width == 0 {
		width = intrinsic.Width
	}
	if height == 0 {
		height = intrinsic.Height
	}

	scale_x, scale_y, translate_x, translate_y := img.preserveRatio.resolveTransforms(width, height, &intrinsic, nil)
	dst.Rectangle(0, 0, width, height)
	dst.State().Clip(false)
	dst.OnNewStack(func() {
		dst.State().Transform(matrix.Transform{A: scale_x, D: scale_y, E: translate_x, F: translate_y})
		// gopdfgo: obraz SVG wstawiony elementem <image> ma układ gospodarza dokumentu, który go
		// wstawia, łącznie z dopasowaniem preserveAspectRatio (testy WPT non-scaling-stroke-non-uniform-
		// scale, -with-none-preserve-aspect-ratio); zob. hostScale
		if holder, ok := img.img.(svgImageHolder); ok && holder.SVG() != nil {
			holder.SVG().draw(dst, intrinsic.Width, intrinsic.Height, svg.textContext, svg.hostBaseScale)
			return
		}
		img.img.Draw(dst, svg.textContext, intrinsic.Width, intrinsic.Height, "auto")
	})

	return nil
}

// svgImageHolder (gopdfgo) to obraz, pod którym leży dokument SVG (images.SVGImage).
type svgImageHolder interface {
	SVG() *SVGImage
}

// wraps a node
// it is a special case of content, handled in drawNode
type use struct {
	target *svgNode
}

// resolves and returns the <use> target content
// wrapped in graphicContent field
func (context *svgContext) resolveUse(node *cascadedNode, defs definitions) (*svgNode, error) {
	href, err := parseURL(node.attrs["href"])
	if err != nil {
		return nil, err
	}
	svgURL, err := parseURL(context.baseURL)
	if err != nil {
		return nil, err
	}
	if svgURL.Scheme == "data" {
		svgURL, _ = parseURL("")
	}
	sameOrigin := href.Path == "" || href.Path == svgURL.Path
	var useTarget cascadedNode
	processContext := context // gopdfgo: kontekst dokumentu, z którego pochodzi cel
	if ID := href.Fragment; ID != "" && sameOrigin {
		if context.inUseIDs.Has(ID) {
			return nil, fmt.Errorf("invalid recursive <use>")
		}
		if context.defs[ID] == nil {
			logger.WarningLogger.Printf("SVG: <use> content not defined")
			return nil, nil
		}
		context.inUseIDs.Add(ID)

		// update after resolving
		defer func() {
			delete(context.inUseIDs, ID)
		}()

		// inner child
		useTarget = context.defs[ID].copy()
	} else {
		// gopdfgo: cel z innego pliku (SVG 2 § 5.6). Źródło brało zawsze pierwszy <svg> dokumentu,
		// pomijając fragment adresu, i przetwarzało go w kontekście bieżącego dokumentu (adresy względne
		// i odwołania wewnątrz celu rozwiązywały się względem złego pliku). Teraz cel to element o id
		// z fragmentu (bez fragmentu: element <svg> dokumentu), z kaskadą i :target pliku docelowego,
		// przetwarzany w jego kontekście; cykl między plikami jest przerywany.
		external, target, key := context.fetchExternalUse(href)
		if target == nil {
			return nil, nil
		}
		context.externalUses.Add(key)
		defer delete(context.externalUses, key)
		useTarget = target.copy()
		processContext = external
	}

	if useTarget.tag == "svg" || useTarget.tag == "symbol" {
		// Explicitely specified
		// http://www.w3.org/TR/SVG11/struct.html#UseElement
		useTarget.tag = "svg"
		width, hasWidth := node.attrs["width"]
		height, hasHeight := node.attrs["height"]
		if hasWidth && hasHeight {
			useTarget.attrs["width"] = width
			useTarget.attrs["height"] = height
		}
	}

	// cascade
	for key, value := range node.attrs {
		if notInheritedAttributes.Has(key) {
			continue
		}
		if _, specified := useTarget.attrs[key]; !specified {
			useTarget.attrs[key] = value
		}
	}

	target, err := processContext.processNode(&useTarget, defs)
	if err != nil {
		return nil, err
	}

	out := &svgNode{
		graphicContent: use{target: target},
	}
	err = node.attrs.parseCommonAttributes(&out.attributes)
	return out, err
}

// fetchExternalUse (gopdfgo) pobiera dokument wskazany przez <use> z innego pliku (przez urlFetcher,
// czyli tylko zasoby dopuszczone przez wywołującego) i zwraca jego kontekst, element docelowy oraz
// klucz adresu do ochrony przed cyklem; nil, gdy celu nie ma.
func (context *svgContext) fetchExternalUse(href *url.URL) (*svgContext, *cascadedNode, string) {
	id := href.Fragment
	document := *href
	document.Fragment = ""
	docURL, err := utils.SafeUrljoin(context.baseURL, document.String(), true)
	if err != nil {
		logger.WarningLogger.Printf("SVG: invalid <use> url: %s", err)
		return nil, nil, ""
	}
	key := docURL + "#" + id
	if context.externalUses.Has(key) {
		logger.WarningLogger.Printf("SVG: recursive <use> of %s", key)
		return nil, nil, ""
	}
	if context.urlFetcher == nil {
		return nil, nil, ""
	}
	content, err := context.urlFetcher(docURL)
	if err != nil {
		logger.WarningLogger.Printf("SVG: fetching <use> content: %s", err)
		return nil, nil, ""
	}
	data, err := io.ReadAll(content.Content)
	if err != nil {
		return nil, nil, ""
	}
	root, err := parseExternalDocument(data)
	if err != nil {
		logger.WarningLogger.Printf("SVG: parsing <use> content: %s", err)
		return nil, nil, ""
	}

	var element *html.Node // element główny kontekstu
	if id == "" {
		// bez fragmentu: element <svg> dokumentu (test WPT use-external-svg-resource-no-fragment-id)
		iter := utils.NewHtmlIterator(root, atom.Svg)
		if !iter.HasNext() {
			return nil, nil, ""
		}
		element = (*html.Node)(iter.Next())
	} else {
		// cały dokument, aby kaskada i :root działały jak w nim samym; cel pasuje do :target
		var found bool
		for n := root.FirstChild; n != nil; n = n.NextSibling {
			if n.Type == html.ElementNode {
				element = n
				break
			}
		}
		if element == nil {
			return nil, nil, ""
		}
		iter := (*utils.HTMLNode)(element).Iter()
		for iter.HasNext() {
			if n := iter.Next(); n.Get("id") == id {
				n.Attr = append(n.Attr, html.Attribute{Key: selector.TargetAttribute})
				found = true
				break
			}
		}
		if !found {
			logger.WarningLogger.Printf("SVG: <use> target %s not found", key)
			return nil, nil, ""
		}
	}

	external := buildSVGContext(element, docURL, context.urlFetcher)
	external.imageLoader = context.imageLoader
	external.externalUses = context.externalUses
	if id == "" {
		return external, external.root, key
	}
	target := external.defs[id]
	if target == nil {
		return nil, nil, ""
	}
	return external, target, key
}

func (use) boundingBox(attrs *attributes, dims drawingDims) (Rectangle, bool) {
	return Rectangle{}, false
}

func (u use) draw(dst backend.Canvas, attrs *attributes, svg *SVGImage, dims drawingDims) []vertex {
	if u.target == nil {
		return nil
	}
	x, y := dims.point(attrs.x, attrs.y)

	dst.OnNewStack(func() {
		dst.State().Transform(matrix.Translation(x, y))
		svg.drawNode(dst, u.target, dims, true) // actually draw the target
	})
	return nil
}

// definitions

type filter interface {
	isFilter()
}

func (filterOffset) isFilter() {}
func (filterBlend) isFilter()  {}

type filterOffset struct {
	dx, dy      Value
	isUnitsBBox bool
}

type filterBlend string

// parse a <filter> node
//
// gopdfgo: filtr z prymitywem, którego silnik nie obsługuje, albo z feOffset działającym na czymś innym niż
// sam element (in="SourceAlpha", wynik poprzedniego prymitywu), jest pomijany w całości: element jest
// rysowany bez filtra (Filter Effects 1 § 7.1 wymaga wtedy, żeby element nie był rysowany wcale, ale
// w druku brak cienia jest mniejszą szkodą niż brak logo). Źródło stosowało same obsługiwane prymitywy
// z łańcucha: przy cieniu (feGaussianBlur, feOffset, feMerge) przesuwało cały element zamiast cienia.
func newFilter(node *cascadedNode) (out []filter, err error) {
	for _, child := range node.children {
		in := child.attrs["in"]
		if child.tag == "feOffset" && in != "" && in != "SourceGraphic" {
			logger.WarningLogger.Printf("unsupported filter input %q of feOffset: filter skipped", in)
			return nil, nil
		}
		switch child.tag {
		case "feOffset":
			fi := filterOffset{
				isUnitsBBox: node.attrs["primitiveUnits"] == "objectBoundingBox",
			}
			fi.dx, err = parseValue(child.attrs["dx"])
			if err != nil {
				return nil, err
			}
			fi.dy, err = parseValue(child.attrs["dy"])
			if err != nil {
				return nil, err
			}
			out = append(out, fi)
		case "feBlend":
			fi := filterBlend("normal")
			if mode, has := child.attrs["mode"]; has {
				fi = filterBlend(mode)
			}
			out = append(out, fi)
		default:
			logger.WarningLogger.Printf("unsupported filter element: %s: filter skipped", child.tag)
			return nil, nil
		}
	}

	return out, nil
}

// clipPath is a container for
// graphic nodes, which will use as clipping path,
// that is drawn but not stroked nor filled.
type clipPath struct {
	svgNode
	isUnitsBBox bool
}

func newClipPath(node *cascadedNode, children []*svgNode) (*clipPath, error) {
	out := clipPath{
		svgNode:     svgNode{children: children},
		isUnitsBBox: node.attrs["clipPathUnits"] == "objectBoundingBox",
	}
	err := node.attrs.parseCommonAttributes(&out.attributes)
	return &out, err
}

// marker is a container for a marker to draw
// on a path (start, middle or end)
type marker struct {
	viewbox *Rectangle

	children []*svgNode

	overflow string // default to "hidden"

	preserveAspectRatio preserveAspectRatio // default to "xMidYMid"

	orient Value // angle or "auto" or "auto-start-reverse"

	markerWidth, markerHeight Value
	refX, refY                Value

	isUnitsUserSpace bool
}

func newMarker(node *cascadedNode, children []*svgNode) (out *marker, err error) {
	out = &marker{
		children:         children,
		isUnitsUserSpace: node.attrs["markerUnits"] == "userSpaceOnUse",
	}

	out.viewbox, err = node.attrs.viewBox()
	if err != nil {
		return nil, err
	}

	out.orient, err = parseOrientation(node.attrs["orient"])
	if err != nil {
		return nil, err
	}

	out.preserveAspectRatio = node.attrs.aspectRatio()

	out.overflow = "hidden"
	if s, has := node.attrs["overflow"]; has {
		out.overflow = s
	}

	mw, mh := "3", "3"
	if s, has := node.attrs["markerWidth"]; has {
		mw = s
	}
	if s, has := node.attrs["markerHeight"]; has {
		mh = s
	}
	out.markerWidth, err = parseValue(mw)
	if err != nil {
		return nil, err
	}
	out.markerHeight, err = parseValue(mh)
	if err != nil {
		return nil, err
	}

	out.refX, err = parseValue(node.attrs["refX"])
	if err != nil {
		return nil, err
	}
	out.refY, err = parseValue(node.attrs["refY"])
	if err != nil {
		return nil, err
	}

	return out, nil
}

// mask is a container for shape that will
// be used as an alpha mask
type mask struct {
	svgNode
	isUnitsBBox bool
}

func newMask(node *cascadedNode, children []*svgNode) (mask, error) {
	out := mask{
		svgNode: svgNode{
			children: children,
		},
		isUnitsBBox: node.attrs["maskUnits"] == "objectBoundingBox",
	}
	err := node.attrs.parseCommonAttributes(&out.svgNode.attributes)
	if err != nil {
		return mask{}, err
	}
	// default values
	if out.x.U == 0 {
		out.x = Value{-10, Perc} // -10%
	}
	if out.y.U == 0 {
		out.y = Value{-10, Perc} // -10%
	}
	if out.width.U == 0 {
		out.width = Value{120, Perc} // 120%
	}
	if out.height.U == 0 {
		out.height = Value{120, Perc} // 120%
	}
	return out, err
}

// transformation matrices

type transformKind uint8

const (
	_                transformKind = iota
	rotate                         // 1 argument
	rotateWithOrigin               // 3 arguments
	translate                      // 2 arguments
	skew                           // 2 argument
	scale                          // 2 arguments
	customMatrix                   // 6 arguments
)

type transform struct {
	kind transformKind
	args [6]Value // the actual number of arguments depends on kind
}

// right multiply to `mat` to apply the transformation, after resolving units
func (tr transform) applyTo(mat *matrix.Transform, fontSize, diagonal Fl) {
	switch tr.kind {
	case rotate:
		angle := tr.args[0].Resolve(fontSize, diagonal)
		mat.Rotate(angle * math.Pi / 180)
	case rotateWithOrigin:
		x, y := tr.args[1].Resolve(fontSize, diagonal), tr.args[2].Resolve(fontSize, diagonal)
		angle := tr.args[0].Resolve(fontSize, diagonal)
		mat.Translate(x, y)
		mat.Rotate(angle * math.Pi / 180)
		mat.Translate(-x, -y)
	case translate:
		x, y := tr.args[0].Resolve(fontSize, diagonal), tr.args[1].Resolve(fontSize, diagonal)
		mat.Translate(x, y)
	case skew:
		thetaX, thetaY := tr.args[0].Resolve(fontSize, diagonal), tr.args[1].Resolve(fontSize, diagonal)
		mat.Skew(thetaX*math.Pi/180, thetaY*math.Pi/180)
	case scale:
		sx, sy := tr.args[0].Resolve(fontSize, diagonal), tr.args[1].Resolve(fontSize, diagonal)
		mat.Scale(sx, sy)
	case customMatrix:
		mat.RightMultBy(matrix.New(
			tr.args[0].Resolve(fontSize, diagonal),
			tr.args[1].Resolve(fontSize, diagonal),
			tr.args[2].Resolve(fontSize, diagonal),
			tr.args[3].Resolve(fontSize, diagonal),
			tr.args[4].Resolve(fontSize, diagonal),
			tr.args[5].Resolve(fontSize, diagonal),
		))
	}
}
