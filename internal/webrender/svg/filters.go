package svg

import (
	"math"
	"strconv"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	"github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/matrix"
)

// gopdfgo: filtry SVG bez rastrowania (Filter Effects 1). PDF nie ma operacji rozmycia, więc graf
// prymitywów filtra jest oceniany symbolicznie: wynik każdego prymitywu to lista warstw (od spodu),
// z których każda jest albo samym elementem (SourceGraphic), albo jego sylwetką w jednym kolorze,
// albo jednolitym wypełnieniem obszaru, z przesunięciem i odchyleniem standardowym rozmycia. Takie
// warstwy da się narysować wektorowo: sylwetka to element użyty jako maska miękka alfa, a rozmycie
// to suma przesuniętych kopii tej maski (zob. gaussianTaps). Graf, którego nie da się tak opisać
// (feTurbulence, feMorphology, feImage, macierz koloru na samym elemencie itd.), jest pomijany w całości
// z ostrzeżeniem, a element rysowany bez filtra, jak przed tą zmianą.

// svgFilter to element <filter> z prymitywami.
type svgFilter struct {
	region        box  // x, y, width, height
	userSpace     bool // filterUnits="userSpaceOnUse"
	primitiveBBox bool // primitiveUnits="objectBoundingBox"
	primitives    []*cascadedNode

	warned bool // ostrzeżenie o nieobsługiwanym grafie wypisane
}

// parse a <filter> node
func newFilter(node *cascadedNode) (*svgFilter, error) {
	out := &svgFilter{
		userSpace:     node.attrs["filterUnits"] == "userSpaceOnUse",
		primitiveBBox: node.attrs["primitiveUnits"] == "objectBoundingBox",
	}
	// domyślny obszar filtra: -10%, -10%, 120%, 120% (Filter Effects 1 § 7.1)
	defaults := [4]Value{{-10, Perc}, {-10, Perc}, {120, Perc}, {120, Perc}}
	for i, name := range [4]string{"x", "y", "width", "height"} {
		v, err := parseValue(node.attrs[name])
		if err != nil {
			return nil, err
		}
		if v.U == 0 {
			v = defaults[i]
		}
		switch i {
		case 0:
			out.region.x = v
		case 1:
			out.region.y = v
		case 2:
			out.region.width = v
		case 3:
			out.region.height = v
		}
	}
	for _, child := range node.children {
		if strings.HasPrefix(child.tag, "fe") {
			out.primitives = append(out.primitives, child)
		}
	}
	return out, nil
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

// objectBoundingBox zwraca ramkę elementu razem z potomkami (SVG 2 § 8.10), bez obrysu.
func objectBoundingBox(node *svgNode, dims drawingDims) (Rectangle, bool) {
	out := emptyBbox
	if _, isImage := node.graphicContent.(image); isImage {
		x, y := dims.point(node.attributes.x, node.attributes.y)
		w, h := dims.point(node.attributes.width, node.attributes.height)
		out = Rectangle{x, y, w, h}
	} else if bb, ok := node.resolveBoundingBox(dims, false); ok {
		out = bb
	}
	if !node.attributes.display {
		return out, out != emptyBbox
	}
	for _, child := range node.children {
		cb, ok := objectBoundingBox(child, dims)
		if !ok {
			continue
		}
		if len(child.attributes.transforms) != 0 {
			mat := aggregateTransforms(child.attributes.transforms, dims.fontSize, dims.innerDiagonal)
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
	case "feDropShadow":
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

// rysowanie

// blurDensity to łączna „gęstość optyczna” U kopii rozmytej sylwetki: kopie o wagach g_k (Σ g_k = 1)
// nakładane zwykłym składaniem z kryciem w_k = 1 − exp(−U·g_k) dają w masce alfę L = 1 − exp(−U·Σ g_k·c_k),
// gdzie c_k ∈ {0, 1} mówi, czy kopia k pokrywa punkt. Funkcja przejścia maski f(L) = −ln(1 − L)/U
// odtwarza z tego dokładnie sumę Σ g_k·c_k, czyli splot sylwetki z dyskretnym jądrem Gaussa. Małe U
// trzyma L z dala od 1 (dokładność masek 8-bitowych), U = 1 daje L ≤ 0,632.
const blurDensity = 1.0

// blurTransfer: próbki funkcji przejścia f(L) = min(1, −ln(1 − L)/U); między próbkami czytnik
// interpoluje liniowo, a przy 64 próbkach błąd interpolacji jest poniżej 0,001
var blurTransfer = func() []Fl {
	out := make([]Fl, 64)
	for i := range out {
		l := float64(i) / float64(len(out)-1)
		v := 1.0
		if l < 1 {
			v = min(1, -math.Log(1-l)/blurDensity)
		}
		out[i] = Fl(v)
	}
	return out
}()

// invertTransfer: f(L) = 1 − L (wycięcie kształtu elementu)
var invertTransfer = []Fl{1, 0}

type kernelTap struct{ x, y, w Fl }

// gaussianTaps rozkłada dwuwymiarowy rozkład Gaussa o σ = 1 na punkty: środek i pierścienie co `step`
// (w jednostkach σ) do promienia około 2,5σ; waga pierścienia to masa rozkładu w jego paśmie
// (z dystrybuanty radialnej 1 − exp(−r²/2)), a ogon za ostatnim pasmem dokładamy do ostatniego
// pierścienia, więc wagi sumują się do 1 i środek dużego kształtu ma pełne krycie. Promień pierścienia
// to średni promień w paśmie, liczba kopii rośnie z obwodem (odstęp około 1,5·step), a kolejne pierścienie
// są obrócone o pół kroku, żeby krawędzie kopii nie układały się w promienie.
func gaussianTaps(step Fl) []kernelTap {
	cdf := func(r float64) float64 { return 1 - math.Exp(-r*r/2) }
	meanRadius := func(a, b float64) float64 {
		const n = 64
		var num, den float64
		for i := 0; i < n; i++ {
			r := a + (b-a)*(float64(i)+0.5)/n
			d := r * math.Exp(-r*r/2)
			num += r * d
			den += d
		}
		return num / den
	}
	s := float64(step)
	rings := max(1, int(math.Round(2.5/s)))
	taps := []kernelTap{{0, 0, Fl(cdf(s / 2))}}
	for j := 1; j <= rings; j++ {
		a, b := (float64(j)-0.5)*s, (float64(j)+0.5)*s
		mass := cdf(b) - cdf(a)
		if j == rings {
			mass = 1 - cdf(a)
			b = a + 4
		}
		r := meanRadius(a, b)
		n := max(6, int(math.Ceil(2*math.Pi*r/(1.5*s))))
		for k := 0; k < n; k++ {
			angle := 2 * math.Pi * (float64(k) + 0.5*float64(j%2)) / float64(n)
			taps = append(taps, kernelTap{Fl(r * math.Cos(angle)), Fl(r * math.Sin(angle)), Fl(mass / float64(n))})
		}
	}
	return taps
}

// blurStep dobiera odstęp pierścieni (w σ) do wielkości rozmycia w pikselach gospodarza: co pół σ,
// ale nie gęściej niż co piksel; małe rozmycia dostają mniej kopii.
func blurStep(sigmaHost Fl) Fl {
	if sigmaHost <= 0 {
		return 1
	}
	return min(1.25, max(0.5, 1/sigmaHost))
}

// drawFiltered rysuje element przez filtr; false, gdy filtra nie da się narysować (element zostaje
// wtedy narysowany bez filtra przez wołającego). content rysuje sam element bez filtra.
func (svg *SVGImage) drawFiltered(dst backend.Canvas, f *svgFilter, node *svgNode, dims drawingDims, content func(backend.Canvas)) bool {
	if _, ok := dst.(backend.FilterCanvas); !ok {
		return false
	}
	bbox, hasBBox := objectBoundingBox(node, dims)
	region, ok := f.resolveRegion(bbox, hasBBox, dims)
	if !ok {
		// ramka pusta przy objectBoundingBox: obszar filtra jest pusty, element nie jest rysowany
		// (Filter Effects 1 § 7.1)
		return true
	}
	if region.Width <= 0 || region.Height <= 0 {
		return true
	}
	if len(f.primitives) == 0 {
		// filtr bez prymitywów daje przezroczystą czerń (Filter Effects 1 § 7.1)
		return true
	}
	ev := filterEval{filter: f, dims: dims, bbox: bbox, region: region}
	layers, ok := ev.run()
	if !ok {
		if !f.warned {
			logger.WarningLogger.Printf("SVG: unsupported filter (%s): filter skipped", ev.reason)
			f.warned = true
		}
		return false
	}
	if len(layers) == 0 {
		return true
	}

	// element bez filtra w grupie o rozmiarze obszaru filtra; grupa jest potem wskazywana przez
	// wszystkie warstwy (kopie rozmycia, sylwetki, sam element)
	savedHostBase := svg.hostBaseScale
	hostScale := svg.hostScale(dst)
	svg.hostBaseScale *= transformScale(dst.State().GetTransform())
	source := dst.NewGroup(region.X, region.Y, region.Width, region.Height)
	content(source)
	svg.hostBaseScale = savedHostBase

	needsGroup := false
	for _, l := range layers {
		if l.blend != "" {
			needsGroup = true
		}
	}
	dst.OnNewStack(func() {
		dst.Rectangle(region.X, region.Y, region.Width, region.Height)
		dst.State().Clip(false)
		target := dst
		if needsGroup {
			// tryby feBlend mieszają warstwę tylko z warstwami wyniku pod nią, nie z tłem strony
			target = dst.NewGroup(region.X, region.Y, region.Width, region.Height)
		}
		for _, l := range layers {
			drawFilterLayer(target, l, source, region, hostScale)
		}
		if needsGroup {
			dst.DrawWithOpacity(1, target)
		}
	})
	return true
}

func drawFilterLayer(dst backend.Canvas, l filterLayer, source backend.Canvas, region Rectangle, hostScale Fl) {
	if l.knockout {
		inner := dst.NewGroup(region.X, region.Y, region.Width, region.Height)
		l.knockout = false
		drawFilterLayer(inner, l, source, region, hostScale)
		dst.OnNewStack(func() {
			mask := dst.NewGroup(region.X, region.Y, region.Width, region.Height)
			mask.(backend.FilterCanvas).DrawSharedGroup(1, source)
			dst.(backend.FilterCanvas).SetSoftMask(mask, true, invertTransfer)
			dst.DrawWithOpacity(1, inner)
		})
		return
	}
	fc := dst.(backend.FilterCanvas)
	dst.OnNewStack(func() {
		if l.blend != "" {
			dst.State().SetBlendingMode(l.blend)
		}
		if l.clip != nil {
			dst.Rectangle(l.clip.X, l.clip.Y, l.clip.Width, l.clip.Height)
			dst.State().Clip(false)
		}
		if l.kind == layerFlood {
			dst.Rectangle(region.X, region.Y, region.Width, region.Height)
			dst.State().SetColorRgba(l.color, false)
			dst.Paint(backend.FillNonZero)
			return
		}
		dst.State().Transform(matrix.Translation(l.dx, l.dy))
		// obszar filtra w układzie przesuniętej warstwy
		area := Rectangle{region.X - l.dx, region.Y - l.dy, region.Width, region.Height}

		var taps []kernelTap
		if l.blurred() {
			taps = gaussianTaps(blurStep(max(l.sx, l.sy) * hostScale))
		}
		// copies rysuje kopie elementu w punktach jądra z podanym kryciem
		copies := func(target backend.Canvas, taps []kernelTap, opacity func(kernelTap) Fl) {
			tfc := target.(backend.FilterCanvas)
			for _, tap := range taps {
				target.OnNewStack(func() {
					target.State().Transform(matrix.Translation(tap.x*l.sx, tap.y*l.sy))
					tfc.DrawSharedGroup(opacity(tap), source)
				})
			}
		}
		blurMask := func() backend.Canvas {
			mask := dst.NewGroup(area.X, area.Y, area.Width, area.Height)
			copies(mask, taps, func(t kernelTap) Fl { return Fl(1 - math.Exp(-blurDensity*float64(t.w))) })
			return mask
		}

		switch {
		case l.kind == layerSource && !l.blurred():
			fc.DrawSharedGroup(l.color.A, source)
		case l.kind == layerSource:
			// kolor: kopie nieprzezroczyste od zewnętrznego pierścienia do środka (na wierzchu kopie
			// najbliższe położenia elementu), krycie: rozmyta maska
			colors := dst.NewGroup(area.X, area.Y, area.Width, area.Height)
			reversed := make([]kernelTap, len(taps))
			for i, t := range taps {
				reversed[len(taps)-1-i] = t
			}
			copies(colors, reversed, func(kernelTap) Fl { return 1 })
			fc.SetSoftMask(blurMask(), true, blurTransfer)
			dst.DrawWithOpacity(l.color.A, colors)
		default: // sylwetka
			if l.blurred() {
				fc.SetSoftMask(blurMask(), true, blurTransfer)
			} else {
				mask := dst.NewGroup(area.X, area.Y, area.Width, area.Height)
				mask.(backend.FilterCanvas).DrawSharedGroup(1, source)
				fc.SetSoftMask(mask, true, nil)
			}
			dst.Rectangle(area.X, area.Y, area.Width, area.Height)
			dst.State().SetColorRgba(l.color, false)
			dst.Paint(backend.FillNonZero)
		}
	})
}
