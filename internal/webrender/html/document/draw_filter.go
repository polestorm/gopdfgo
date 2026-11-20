package document

// Plik dodany w gopdfgo, nie pochodzi ze źródła webrender.
//
// Właściwość filter z funkcjami filtra (Filter Effects 1 § 13) bez rastrowania treści wektorowej:
//   - funkcje barwne (grayscale, sepia, saturate, hue-rotate, invert, brightness, contrast) to macierze
//     barw (§ 13.2, § 15.10, § 15.11). Stosujemy je dokładnie do każdego koloru jednolitego użytego przy
//     rysowaniu elementu i jego potomków (tekst, tła, obramowania, punkty gradientów, wypełnienia SVG)
//     i do pikseli obrazów rastrowych (filterCanvas). Wynik różni się od normy tam, gdzie norma filtruje
//     już złożony obraz: nakładające się półprzezroczyste warstwy, tryby mieszania, gradienty z obcięciem
//     wartości pośrednich, maski luminancji wewnątrz elementu;
//   - opacity() to krycie grupy, jak właściwość opacity;
//   - drop-shadow() rysuje cień o kształcie treści elementu: treść trafia do grupy, której kanał alfa
//     (rozmyty, zob. gaussianMask) jest maską miękką dla prostokąta w kolorze cienia, potem rysujemy
//     treść;
//   - blur() rysuje kopie treści przesunięte na okręgach z wagami rozkładu Gaussa, a krycie wyniku
//     ustala rozmyta maska alfa (zob. drawBlurred);
//   - url() (odwołanie do elementu <filter>) nie jest obsługiwane: łańcuch z nim rysujemy bez filtra
//     z ostrzeżeniem, jak przy odwołaniu do nieistniejącego filtra (§ 5).

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"strings"

	_ "image/gif"
	_ "image/jpeg"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	"github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/matrix"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// colorMatrix to macierz barw dla R, G, B: kolumny r, g, b i przesunięcie, na wartościach
// nieprzemnożonych przez alfę w [0, 1]. Funkcje filtra nie zmieniają kanału alfa (opacity() to osobny
// etap), więc wiersz alfa macierzy feColorMatrix pomijamy.
type colorMatrix [3][4]float64

// colorTransform to ciąg macierzy stosowanych po kolei z obcięciem do [0, 1] po każdej, jak kolejne
// prymitywy feColorMatrix i feComponentTransfer (§ 15.10, § 15.11); kolejne funkcje barwne łączymy
// w jeden etap, a ciąg z obcięciami jest dokładny także tam, gdzie iloczyn macierzy by nie był.
type colorTransform []colorMatrix

func clamp01(v float64) float64 { return min(1, max(0, v)) }

func (t colorTransform) apply(r, g, b float64) (float64, float64, float64) {
	for _, m := range t {
		r, g, b = clamp01(m[0][0]*r+m[0][1]*g+m[0][2]*b+m[0][3]),
			clamp01(m[1][0]*r+m[1][1]*g+m[1][2]*b+m[1][3]),
			clamp01(m[2][0]*r+m[2][1]*g+m[2][2]*b+m[2][3])
	}
	return r, g, b
}

func (t colorTransform) rgba(c parser.RGBA) parser.RGBA {
	r, g, b := t.apply(float64(c.R), float64(c.G), float64(c.B))
	return parser.RGBA{R: fl(r), G: fl(g), B: fl(b), A: c.A}
}

func (t colorTransform) key() string {
	var b strings.Builder
	for _, m := range t {
		fmt.Fprint(&b, m)
	}
	return b.String()
}

// filterColorMatrix zwraca macierz funkcji barwnej (Filter Effects 1 § 13.2) albo false dla funkcji
// innej niż barwna.
func filterColorMatrix(f pr.FilterFunction) (colorMatrix, bool) {
	a := float64(f.Amount)
	switch f.Name {
	case "grayscale":
		s := 1 - a
		return colorMatrix{
			{0.2126 + 0.7874*s, 0.7152 - 0.7152*s, 0.0722 - 0.0722*s, 0},
			{0.2126 - 0.2126*s, 0.7152 + 0.2848*s, 0.0722 - 0.0722*s, 0},
			{0.2126 - 0.2126*s, 0.7152 - 0.7152*s, 0.0722 + 0.9278*s, 0},
		}, true
	case "sepia":
		s := 1 - a
		return colorMatrix{
			{0.393 + 0.607*s, 0.769 - 0.769*s, 0.189 - 0.189*s, 0},
			{0.349 - 0.349*s, 0.686 + 0.314*s, 0.168 - 0.168*s, 0},
			{0.272 - 0.272*s, 0.534 - 0.534*s, 0.131 + 0.869*s, 0},
		}, true
	case "saturate": // feColorMatrix type="saturate" (§ 15.10)
		return colorMatrix{
			{0.213 + 0.787*a, 0.715 - 0.715*a, 0.072 - 0.072*a, 0},
			{0.213 - 0.213*a, 0.715 + 0.285*a, 0.072 - 0.072*a, 0},
			{0.213 - 0.213*a, 0.715 - 0.715*a, 0.072 + 0.928*a, 0},
		}, true
	case "hue-rotate": // feColorMatrix type="hueRotate" (§ 15.10)
		c, s := math.Cos(a), math.Sin(a)
		return colorMatrix{
			{0.213 + c*0.787 - s*0.213, 0.715 - c*0.715 - s*0.715, 0.072 - c*0.072 + s*0.928, 0},
			{0.213 - c*0.213 + s*0.143, 0.715 + c*0.285 + s*0.140, 0.072 - c*0.072 - s*0.283, 0},
			{0.213 - c*0.213 - s*0.787, 0.715 - c*0.715 + s*0.715, 0.072 + c*0.928 + s*0.072, 0},
		}, true
	case "invert": // feComponentTransfer type="table" tableValues="a 1-a"
		k := 1 - 2*a
		return colorMatrix{{k, 0, 0, a}, {0, k, 0, a}, {0, 0, k, a}}, true
	case "brightness": // feComponentTransfer type="linear" slope="a"
		return colorMatrix{{a, 0, 0, 0}, {0, a, 0, 0}, {0, 0, a, 0}}, true
	case "contrast": // feComponentTransfer type="linear" slope="a" intercept="-a/2 + 1/2"
		o := 0.5 - 0.5*a
		return colorMatrix{{a, 0, 0, o}, {0, a, 0, o}, {0, 0, a, o}}, true
	}
	return colorMatrix{}, false
}

type filterStageKind uint8

const (
	stageColors filterStageKind = iota
	stageOpacity
	stageShadow
	stageBlur
)

// filterStage to jeden etap łańcucha filtra: przekształcenie barw (kolejne funkcje barwne razem),
// krycie, cień albo rozmycie.
type filterStage struct {
	kind    filterStageKind
	colors  colorTransform
	opacity fl
	dx, dy  fl
	sigma   fl
	color   parser.RGBA
}

// filterStages buduje etapy filtra elementu ze stylu, w kolejności stosowania (od lewej).
func filterStages(style pr.StyleAccessor) []filterStage {
	filters := style.GetFilter()
	var out []filterStage
	for _, f := range filters {
		if f.Name == "url" {
			logger.WarningLogger.Printf("Ignored `filter` with url(%s): SVG filter references are not supported for HTML elements, drawn unfiltered\n", f.URL)
			return nil
		}
	}
	for _, f := range filters {
		last := len(out) - 1
		if m, ok := filterColorMatrix(f); ok {
			if last >= 0 && out[last].kind == stageColors {
				out[last].colors = append(out[last].colors, m)
			} else {
				out = append(out, filterStage{kind: stageColors, colors: colorTransform{m}})
			}
			continue
		}
		switch f.Name {
		case "opacity":
			if last >= 0 && out[last].kind == stageOpacity {
				out[last].opacity *= f.Amount
			} else {
				out = append(out, filterStage{kind: stageOpacity, opacity: f.Amount})
			}
		case "blur":
			if sigma := fl(f.Length.Value); sigma > 0 {
				out = append(out, filterStage{kind: stageBlur, sigma: sigma})
			}
		case "drop-shadow":
			color := f.Shadow.Color.ResolveCurrentColor(style.GetColor()).RGBA
			out = append(out, filterStage{
				kind: stageShadow, dx: fl(f.Shadow.X.Value), dy: fl(f.Shadow.Y.Value),
				// trzecia długość to odchylenie standardowe rozmycia: odpowiednik z § 13.2 to
				// <feDropShadow stdDeviation="[radius]">; tak też Chromium (inaczej niż w box-shadow,
				// gdzie promień rozmycia to dwa odchylenia)
				sigma: fl(f.Shadow.Blur.Value), color: color,
			})
		}
	}
	return out
}

// drawFiltered rysuje treść elementu (paint) przez etapy filtra stages: etap ostatni obejmuje wynik
// wszystkich wcześniejszych (Filter Effects 1 § 5: funkcje stosuje się po kolei do wyniku poprzedniej).
func (ctx drawContext) drawFiltered(stages []filterStage, paint func(drawContext)) {
	if len(stages) == 0 {
		paint(ctx)
		return
	}
	stage, rest := stages[len(stages)-1], stages[:len(stages)-1]
	switch stage.kind {
	case stageColors:
		inner := ctx
		inner.dst = &filterCanvas{out: ctx.dst, colors: stage.colors, images: ctx.filteredImages}
		inner.drawFiltered(rest, paint)
	case stageOpacity:
		group := ctx.contentGroup(rest, paint)
		ctx.dst.OnNewStack(func() { ctx.dst.DrawWithOpacity(stage.opacity, group) })
	case stageShadow:
		group := ctx.contentGroup(rest, paint)
		ctx.drawShadow(group, stage)
		ctx.dst.OnNewStack(func() { ctx.dst.DrawWithOpacity(1, group) })
	case stageBlur:
		group := ctx.contentGroup(rest, paint)
		ctx.drawBlurred(group, stage.sigma)
	}
}

// groupExtent: grupy filtra nie przycinają treści (wychodzi ona poza pudełko elementu)
const groupExtent = 1e5

func newFilterGroup(dst backend.Canvas) backend.Canvas {
	return dst.NewGroup(-groupExtent, -groupExtent, 2*groupExtent, 2*groupExtent)
}

// contentGroup rysuje do nowej grupy treść elementu przefiltrowaną etapami stages.
func (ctx drawContext) contentGroup(stages []filterStage, paint func(drawContext)) backend.Canvas {
	inner := ctx
	inner.dst = newFilterGroup(ctx.dst)
	inner.drawFiltered(stages, paint)
	return inner.dst
}

// blurSample to przesunięcie kopii treści i jej waga w przybliżeniu rozmycia Gaussa.
type blurSample struct{ dx, dy, w fl }

// gaussianSamples przybliża dwuwymiarowy rozkład Gaussa o odchyleniu sigma punktami: środkiem i K
// okręgami o promieniach k·h (h = 3σ/K), po około 6k punktów na okręgu. Waga punktu to masa rozkładu
// w jego pierścieniu [(k−½)h, (k+½)h] podzielona przez liczbę punktów okręgu; ogon poza ostatnim
// pierścieniem dochodzi do ostatniego okręgu, więc wagi sumują się do 1.
func gaussianSamples(sigma fl) []blurSample {
	s := float64(sigma)
	rings := int(math.Ceil(1.5 * s))
	rings = min(5, max(1, rings))
	h := 3 * s / float64(rings)
	mass := func(r float64) float64 { return math.Exp(-r * r / (2 * s * s)) } // masa poza promieniem r
	out := []blurSample{{w: fl(1 - mass(h/2))}}
	for k := 1; k <= rings; k++ {
		inner, outer := mass((float64(k)-0.5)*h), mass((float64(k)+0.5)*h)
		w := inner - outer
		if k == rings {
			w = inner
		}
		n := 6 * k
		phase := 0.0
		if k%2 == 0 {
			phase = math.Pi / float64(n)
		}
		for j := 0; j < n; j++ {
			angle := phase + 2*math.Pi*float64(j)/float64(n)
			r := float64(k) * h
			out = append(out, blurSample{dx: fl(r * math.Cos(angle)), dy: fl(r * math.Sin(angle)), w: fl(w / float64(n))})
		}
	}
	return out
}

// maskGain: krycie kopii w masce rozmycia to 1 − exp(−maskGain·w), więc złożenie kopii (każda nad
// poprzednią) daje w punkcie pokrytym przez kopie o wagach w_i krycie 1 − exp(−maskGain·Σw_i);
// funkcja przejścia maski (blurTransfer) odwraca to przekształcenie i zostaje Σw_i, czyli splot
// kanału alfa z rozkładem Gaussa (dokładnie dla treści o alfie 0 albo 1, w przybliżeniu dla
// półprzezroczystej).
//
// Przeglądarki PDF składają krycie w 8 bitach: kopia o kryciu 1/255 nałożona na krycie ponad 1/2 nic
// nie dodaje, więc przy kilkudziesięciu kopiach wnętrze rozmytego kształtu zostawało o kilkanaście
// procent przezroczyste. Dlatego krycie kopii zaokrąglamy do 1/255, kopie rysujemy od najlżejszej
// (zewnętrzne okręgi najpierw, gdy krycie jest jeszcze małe), a funkcję przejścia skalujemy tak, by
// nasycała się przy 98% krycia punktu pokrytego przez wszystkie kopie (symulacja składania w 8 bitach
// daje wtedy we wnętrzu pełne krycie przy każdym odchyleniu).
const maskGain = 2

func blurTransfer(alphas []fl) []fl {
	var full float64 // −ln(1 − krycie punktu pokrytego przez wszystkie kopie)
	for _, a := range alphas {
		full -= math.Log(1 - float64(a))
	}
	full *= 0.98
	out := make([]fl, 256)
	for i := range out {
		if i == 255 {
			out[i] = 1
			continue
		}
		out[i] = fl(min(1, -math.Log(1-float64(i)/255)/full))
	}
	return out
}

// gaussianMask zwraca grupę, której kanał alfa po funkcji przejścia transfer to kanał alfa grupy
// content rozmyty z odchyleniem sigma (zob. maskGain).
func gaussianMask(dst, content backend.Canvas, sigma fl) (mask backend.Canvas, transfer []fl) {
	mask = newFilterGroup(dst)
	samples := gaussianSamples(sigma)
	alphas := make([]fl, len(samples))
	// wagi maleją od środka na zewnątrz, więc od końca rysujemy od najlżejszej kopii
	for i := len(samples) - 1; i >= 0; i-- {
		sample := samples[i]
		alpha := 1 - math.Exp(-maskGain*float64(sample.w))
		alpha = max(1, math.Round(alpha*255)) / 255
		alphas[i] = fl(alpha)
		mask.OnNewStack(func() {
			mask.State().Transform(matrix.New(1, 0, 0, 1, sample.dx, sample.dy))
			drawCopy(mask, fl(alpha), content)
		})
	}
	return mask, blurTransfer(alphas)
}

// drawCopy rysuje grupę group na dst jako powtórzenie treści, bez wydobywania jej tekstu (zob.
// backend.CopyDrawer), a gdy dst tego nie umie, zwyczajnie.
func drawCopy(dst backend.Canvas, opacity fl, group backend.Canvas) {
	if copier, ok := dst.(backend.CopyDrawer); ok {
		copier.DrawCopyWithOpacity(opacity, group)
		return
	}
	dst.DrawWithOpacity(opacity, group)
}

// drawShadow rysuje cień drop-shadow() treści content: prostokąt w kolorze cienia przez maskę alfa
// treści (rozmytą przy sigma > 0), przesunięty o (dx, dy).
func (ctx drawContext) drawShadow(content backend.Canvas, stage filterStage) {
	if stage.color.A == 0 {
		return
	}
	if _, ok := ctx.dst.State().(backend.AlphaMaskFromAlpha); !ok {
		logger.WarningLogger.Println("Ignored drop-shadow(): alpha masks are not supported by the output")
		return
	}
	var (
		mask     backend.Canvas
		transfer []fl
	)
	if stage.sigma > 0 {
		mask, transfer = gaussianMask(ctx.dst, content, stage.sigma)
	} else {
		// maska to sama treść, ale w osobnej grupie jako kopia, by jej tekst nie był wydobywany
		mask = newFilterGroup(ctx.dst)
		drawCopy(mask, 1, content)
	}
	ctx.dst.OnNewStack(func() {
		ctx.dst.State().Transform(matrix.New(1, 0, 0, 1, stage.dx, stage.dy))
		ctx.dst.State().(backend.AlphaMaskFromAlpha).SetAlphaMaskFromAlpha(mask, transfer)
		ctx.dst.State().SetColorRgba(stage.color, false)
		ctx.dst.Rectangle(-groupExtent, -groupExtent, 2*groupExtent, 2*groupExtent)
		ctx.dst.Paint(backend.FillNonZero)
	})
}

// drawBlurred rysuje treść content rozmytą z odchyleniem sigma (blur()). Bez rastrowania przybliżamy to
// tak: grupa barw to najpierw wszystkie kopie treści (gaussianSamples) w pełnym kryciu, co pokrywa cały
// obszar rozmycia barwą najbliższej treści, a na nich te same kopie z kryciem w_k/(w_1+…+w_k), co we
// wnętrzu daje średnią barw ważoną rozkładem Gaussa; krycie wyniku ustala maska alfa gaussianMask.
// Dla treści jednobarwnej (tekst, jednolite tło) barwa i krycie są poprawne; na granicach różnych barw
// mieszanie jest tylko przybliżone.
func (ctx drawContext) drawBlurred(content backend.Canvas, sigma fl) {
	if _, ok := ctx.dst.State().(backend.AlphaMaskFromAlpha); !ok {
		logger.WarningLogger.Println("Ignored blur(): alpha masks are not supported by the output")
		ctx.dst.OnNewStack(func() { ctx.dst.DrawWithOpacity(1, content) })
		return
	}
	samples := gaussianSamples(sigma)
	colors := newFilterGroup(ctx.dst)
	// tekst wydobywa się tylko z kopii bez przesunięcia w drugim przebiegu (zob. drawCopy)
	draw := func(sample blurSample, alpha fl, extractable bool) {
		colors.OnNewStack(func() {
			colors.State().Transform(matrix.New(1, 0, 0, 1, sample.dx, sample.dy))
			if extractable {
				colors.DrawWithOpacity(alpha, content)
			} else {
				drawCopy(colors, alpha, content)
			}
		})
	}
	for i := len(samples) - 1; i >= 0; i-- {
		draw(samples[i], 1, false)
	}
	var total fl
	for i, sample := range samples {
		total += sample.w
		if total > 0 {
			draw(sample, sample.w/total, i == 0)
		}
	}
	mask, transfer := gaussianMask(ctx.dst, content, sigma)
	ctx.dst.OnNewStack(func() {
		ctx.dst.State().(backend.AlphaMaskFromAlpha).SetAlphaMaskFromAlpha(mask, transfer)
		ctx.dst.DrawWithOpacity(1, colors)
	})
}

// filterCanvas rysuje na out z barwami przekształconymi przez colors. W grupach (NewGroup) rysuje
// równolegle na raw bez przekształcenia: grupa użyta jako maska luminancji (SetAlphaMask, np. maska
// background-clip: text albo <mask> SVG) ma zostać nieprzefiltrowana, bo filtr działa na wynik
// rysowania elementu, a nie na maski wewnątrz niego. Nieużyta kopia nie trafia do pliku.
type filterCanvas struct {
	out, raw backend.Canvas
	colors   colorTransform
	images   map[string]*filteredImage // zob. drawContext.filteredImages; nil: bez pamięci
}

var (
	_ backend.Canvas             = (*filterCanvas)(nil)
	_ backend.GraphicState       = (*filterCanvas)(nil)
	_ backend.AlphaMaskFromAlpha = (*filterCanvas)(nil)
)

// both wykonuje op na out i na raw (jeśli jest).
func (c *filterCanvas) both(op func(backend.Canvas)) {
	op(c.out)
	if c.raw != nil {
		op(c.raw)
	}
}

// branches zwraca odpowiedniki grupy g dla out i raw: grupę filterCanvas rozkłada na jej gałęzie.
func branches(g backend.Canvas) (out, raw backend.Canvas) {
	if fc, ok := g.(*filterCanvas); ok {
		if fc.raw == nil {
			return fc.out, fc.out
		}
		return fc.out, fc.raw
	}
	return g, g
}

func (c *filterCanvas) GetBoundingBox() (left, top, right, bottom fl) { return c.out.GetBoundingBox() }

func (c *filterCanvas) SetBoundingBox(left, top, right, bottom fl) {
	c.both(func(d backend.Canvas) { d.SetBoundingBox(left, top, right, bottom) })
}

func (c *filterCanvas) OnNewStack(f func()) {
	c.out.OnNewStack(func() {
		if c.raw != nil {
			c.raw.OnNewStack(f)
		} else {
			f()
		}
	})
}

func (c *filterCanvas) State() backend.GraphicState { return c }

func (c *filterCanvas) NewGroup(x, y, width, height fl) backend.Canvas {
	raw := c.raw
	if raw == nil {
		raw = c.out
	}
	return &filterCanvas{out: c.out.NewGroup(x, y, width, height), raw: raw.NewGroup(x, y, width, height), colors: c.colors, images: c.images}
}

func (c *filterCanvas) DrawWithOpacity(opacity fl, group backend.Canvas) {
	out, raw := branches(group)
	c.out.DrawWithOpacity(opacity, out)
	if c.raw != nil {
		c.raw.DrawWithOpacity(opacity, raw)
	}
}

func (c *filterCanvas) DrawCopyWithOpacity(opacity fl, group backend.Canvas) {
	out, raw := branches(group)
	drawCopy(c.out, opacity, out)
	if c.raw != nil {
		drawCopy(c.raw, opacity, raw)
	}
}

func (c *filterCanvas) Paint(op backend.PaintOp) { c.both(func(d backend.Canvas) { d.Paint(op) }) }

func (c *filterCanvas) Rectangle(x, y, width, height fl) {
	c.both(func(d backend.Canvas) { d.Rectangle(x, y, width, height) })
}

func (c *filterCanvas) MoveTo(x, y fl) { c.both(func(d backend.Canvas) { d.MoveTo(x, y) }) }

func (c *filterCanvas) LineTo(x, y fl) { c.both(func(d backend.Canvas) { d.LineTo(x, y) }) }

func (c *filterCanvas) CubicTo(x1, y1, x2, y2, x3, y3 fl) {
	c.both(func(d backend.Canvas) { d.CubicTo(x1, y1, x2, y2, x3, y3) })
}

func (c *filterCanvas) ClosePath() { c.both(func(d backend.Canvas) { d.ClosePath() }) }

func (c *filterCanvas) AddFont(font backend.Font, content []byte) *backend.FontChars {
	return c.out.AddFont(font, content)
}

func (c *filterCanvas) DrawText(texts []backend.TextDrawing) {
	c.both(func(d backend.Canvas) { d.DrawText(texts) })
}

func (c *filterCanvas) DrawRasterImage(img backend.RasterImage, width, height fl) {
	filtered, original := filterRasterImage(img, c.colors, c.images)
	c.out.DrawRasterImage(filtered, width, height)
	if c.raw != nil {
		c.raw.DrawRasterImage(original, width, height)
	}
}

func (c *filterCanvas) DrawGradient(gradient backend.GradientLayout, width, height fl) {
	// gopdfgo: barwy pośrednie gradientu to interpolacja barw punktów, a macierz jest afiniczna, więc
	// przekształcenie punktów jest dokładne, dopóki nie zadziała obcięcie do [0, 1]
	filtered := gradient
	filtered.Colors = make([]parser.RGBA, len(gradient.Colors))
	for i, color := range gradient.Colors {
		filtered.Colors[i] = c.colors.rgba(color)
	}
	c.out.DrawGradient(filtered, width, height)
	if c.raw != nil {
		c.raw.DrawGradient(gradient, width, height)
	}
}

// GraphicState

func (c *filterCanvas) SetAlphaMask(mask backend.Canvas) {
	_, raw := branches(mask)
	c.both(func(d backend.Canvas) { d.State().SetAlphaMask(raw) })
}

func (c *filterCanvas) SetAlphaMaskFromAlpha(mask backend.Canvas, transfer []fl) {
	// kanał alfa obu gałęzi jest ten sam (funkcje barwne go nie zmieniają)
	out, raw := branches(mask)
	if m, ok := c.out.State().(backend.AlphaMaskFromAlpha); ok {
		m.SetAlphaMaskFromAlpha(out, transfer)
	}
	if c.raw != nil {
		if m, ok := c.raw.State().(backend.AlphaMaskFromAlpha); ok {
			m.SetAlphaMaskFromAlpha(raw, transfer)
		}
	}
}

func (c *filterCanvas) Clip(evenOdd bool) { c.both(func(d backend.Canvas) { d.State().Clip(evenOdd) }) }

func (c *filterCanvas) SetAlpha(alpha fl, stroke bool) {
	c.both(func(d backend.Canvas) { d.State().SetAlpha(alpha, stroke) })
}

func (c *filterCanvas) SetColorRgba(color parser.RGBA, stroke bool) {
	c.out.State().SetColorRgba(c.colors.rgba(color), stroke)
	if c.raw != nil {
		c.raw.State().SetColorRgba(color, stroke)
	}
}

func (c *filterCanvas) SetColorPattern(pattern backend.Canvas, contentWidth, contentHeight fl, mat matrix.Transform, stroke bool) {
	out, raw := branches(pattern)
	c.out.State().SetColorPattern(out, contentWidth, contentHeight, mat, stroke)
	if c.raw != nil {
		c.raw.State().SetColorPattern(raw, contentWidth, contentHeight, mat, stroke)
	}
}

func (c *filterCanvas) SetBlendingMode(mode string) {
	c.both(func(d backend.Canvas) { d.State().SetBlendingMode(mode) })
}

func (c *filterCanvas) SetLineWidth(width fl) {
	c.both(func(d backend.Canvas) { d.State().SetLineWidth(width) })
}

func (c *filterCanvas) SetDash(dashes []fl, offset fl) {
	c.both(func(d backend.Canvas) { d.State().SetDash(dashes, offset) })
}

func (c *filterCanvas) SetStrokeOptions(options backend.StrokeOptions) {
	c.both(func(d backend.Canvas) { d.State().SetStrokeOptions(options) })
}

func (c *filterCanvas) GetTransform() matrix.Transform { return c.out.State().GetTransform() }

func (c *filterCanvas) Transform(mt matrix.Transform) {
	c.both(func(d backend.Canvas) { d.State().Transform(mt) })
}

func (c *filterCanvas) SetTextPaint(op backend.PaintOp) {
	c.both(func(d backend.Canvas) { d.State().SetTextPaint(op) })
}

// filteredImage: obraz rastrowy po przekształceniu barw, zakodowany jako PNG
type filteredImage struct {
	data []byte
	id   int
	err  error
}

// filterRasterImage zwraca obraz img z barwami pikseli przekształconymi przez colors (zapamiętany według
// obrazu i przekształcenia w cache) i obraz img gotowy do ponownego odczytu. Obrazu, którego nie da się
// zdekodować, nie filtrujemy.
func filterRasterImage(img backend.RasterImage, colors colorTransform, cache map[string]*filteredImage) (filtered, original backend.RasterImage) {
	original = img
	key := fmt.Sprint(img.ID, colors.key())
	cached := cache[key]
	if cached == nil {
		var data []byte
		if seeker, ok := img.Content.(io.Seeker); ok {
			seeker.Seek(0, io.SeekStart)
			data, _ = io.ReadAll(img.Content)
			seeker.Seek(0, io.SeekStart)
		} else {
			data, _ = io.ReadAll(img.Content)
			original.Content = bytes.NewReader(data)
		}
		cached = &filteredImage{id: utils.Hash("filter " + key)}
		cached.data, cached.err = transformImageColors(data, colors)
		if cache != nil {
			cache[key] = cached
		}
	}
	if cached.err != nil {
		logger.WarningLogger.Printf("filter not applied to image: %s\n", cached.err)
		return original, original
	}
	filtered = img
	filtered.Content = bytes.NewReader(cached.data)
	filtered.MimeType = "image/png"
	filtered.ID = cached.id
	return filtered, original
}

func transformImageColors(data []byte, colors colorTransform) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := src.Bounds()
	dst := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
			r, g, b := colors.apply(float64(c.R)/255, float64(c.G)/255, float64(c.B)/255)
			dst.SetNRGBA(x, y, color.NRGBA{
				R: uint8(math.Round(r * 255)), G: uint8(math.Round(g * 255)), B: uint8(math.Round(b * 255)), A: c.A,
			})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
