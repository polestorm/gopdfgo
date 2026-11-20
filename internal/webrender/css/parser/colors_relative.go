package parser

// Plik dodany w gopdfgo, nie pochodzi ze źródła webrender.
//
// Składnia względna barw (CSS Color 5 § 4: rgb(from <barwa> r g b) itd.), funkcja color() z
// przestrzeniami predefiniowanymi (CSS Color 4 § 10) i wyrażenia matematyczne (calc() i pokrewne,
// CSS Values 4 § 10) w kanałach barwy. Wynik sprowadzamy do sRGB; barwy spoza gamy przycinamy kanał
// po kanale (CSS Color 4 § 13 zaleca odwzorowanie gamy w OKLCH, którego tu nie robimy; Chromium też
// przycina przy druku). Barwa wyjściowa jest już przechowywana w sRGB, więc kanały składni względnej
// liczone są z barwy po przycięciu do gamy sRGB (uproszczenie).
//
// Barwa względna, której barwą wyjściową jest currentColor (wprost albo przez inną barwę względną lub
// color-mix()), zależy od koloru elementu: wartość obliczona zachowuje funkcję (CSS Color 5 § 4.2), a
// barwę konkretną daje dopiero Color.Resolve z kolorem elementu. Taka wartość ma typ
// ColorCurrentColor i niepuste pole Deferred; kod, który zna tylko currentColor, potraktuje ją jak
// currentColor (bez dalszych zmian kanałów).

import (
	"math"

	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// colorModel opisuje kanały funkcji barwy w postaci liczbowej, której używa składnia względna:
// nazwy kanałów, wartość odpowiadającą 100% (0 dla kąta barwy) i indeks kanału kąta (albo -1).
type colorModel struct {
	channels [3]string
	percent  [3]float64
	hue      int
}

var colorModels = map[string]colorModel{
	"rgb":   {[3]string{"r", "g", "b"}, [3]float64{255, 255, 255}, -1},
	"hsl":   {[3]string{"h", "s", "l"}, [3]float64{0, 100, 100}, 0},
	"hwb":   {[3]string{"h", "w", "b"}, [3]float64{0, 100, 100}, 0},
	"lab":   {[3]string{"l", "a", "b"}, [3]float64{100, 125, 125}, -1},
	"lch":   {[3]string{"l", "c", "h"}, [3]float64{100, 150, 0}, 2},
	"oklab": {[3]string{"l", "a", "b"}, [3]float64{1, 0.4, 0.4}, -1},
	"oklch": {[3]string{"l", "c", "h"}, [3]float64{1, 0.4, 0}, 2},
	// przestrzenie predefiniowane funkcji color(): 100% to 1 (CSS Color 4 § 10.1)
	"srgb":              {[3]string{"r", "g", "b"}, [3]float64{1, 1, 1}, -1},
	"srgb-linear":       {[3]string{"r", "g", "b"}, [3]float64{1, 1, 1}, -1},
	"display-p3":        {[3]string{"r", "g", "b"}, [3]float64{1, 1, 1}, -1},
	"display-p3-linear": {[3]string{"r", "g", "b"}, [3]float64{1, 1, 1}, -1},
	"a98-rgb":           {[3]string{"r", "g", "b"}, [3]float64{1, 1, 1}, -1},
	"prophoto-rgb":      {[3]string{"r", "g", "b"}, [3]float64{1, 1, 1}, -1},
	"rec2020":           {[3]string{"r", "g", "b"}, [3]float64{1, 1, 1}, -1},
	"xyz":               {[3]string{"x", "y", "z"}, [3]float64{1, 1, 1}, -1},
	"xyz-d50":           {[3]string{"x", "y", "z"}, [3]float64{1, 1, 1}, -1},
	"xyz-d65":           {[3]string{"x", "y", "z"}, [3]float64{1, 1, 1}, -1},
}

// Resolve zwraca barwę konkretną: currentColor (także w barwie względnej albo color-mix()) zastępuje
// kolorem elementu [current].
func (c Color) Resolve(current RGBA) Color {
	if c.Type != ColorCurrentColor {
		return c
	}
	if c.Deferred == nil {
		return Color{Type: ColorRGBA, RGBA: current}
	}
	return parseColor(*c.Deferred, &current)
}

// deferredColor zwraca wartość zależną od currentColor albo barwę pustą, gdy funkcja jest
// niepoprawna; poprawność sprawdzamy, licząc ją dla przykładowego koloru elementu.
func deferredColor(token FunctionBlock) Color {
	if parseColor(token, &RGBA{A: 1}).Type != ColorRGBA {
		return Color{}
	}
	return Color{Type: ColorCurrentColor, Deferred: &token}
}

// parseModernColor czyta funkcję barwy w składni ze spacjami z barwą wyjściową (from) albo funkcję
// color(). [current] to kolor elementu albo nil, gdy nie jest jeszcze znany.
func parseModernColor(token FunctionBlock, name string, current *RGBA) Color {
	tokens := RemoveWhitespace(token.Arguments)
	var (
		origin    RGBA
		hasOrigin bool
	)
	if len(tokens) >= 2 && isIdent(tokens[0], "from") {
		originColor := parseColor(tokens[1], current)
		switch {
		case originColor.Type == ColorRGBA:
			origin, hasOrigin = originColor.RGBA, true
		case originColor.Type == ColorCurrentColor && current == nil:
			return deferredColor(token)
		default:
			return Color{}
		}
		tokens = tokens[2:]
	}
	space := name
	if name == "color" {
		if len(tokens) == 0 {
			return Color{}
		}
		ident, ok := tokens[0].(Ident)
		if !ok {
			return Color{}
		}
		space = utils.AsciiLower(ident.Value)
		if _, known := colorModels[space]; !known || space == "rgb" || space == "hsl" || space == "hwb" ||
			space == "lab" || space == "lch" || space == "oklab" || space == "oklch" {
			return Color{}
		}
		tokens = tokens[1:]
	}
	model := colorModels[space]

	var components, alphaTokens []Token
	afterSlash := false
	for _, t := range tokens {
		switch {
		case IsLiteral(t, ","):
			return Color{} // składnia względna i color() nie znają przecinków
		case IsLiteral(t, "/"):
			if afterSlash {
				return Color{}
			}
			afterSlash = true
		case afterSlash:
			alphaTokens = append(alphaTokens, t)
		default:
			components = append(components, t)
		}
	}
	if len(components) != 3 || len(alphaTokens) > 1 || (afterSlash && len(alphaTokens) == 0) {
		return Color{}
	}

	// słowa kluczowe kanałów barwy wyjściowej w postaci liczbowej przestrzeni docelowej
	var keywords map[string]float64
	alpha := 1.
	if hasOrigin {
		values := channelsFromRGB(space, origin)
		keywords = map[string]float64{"alpha": float64(origin.A)}
		for i, k := range model.channels {
			keywords[k] = values[i]
		}
		alpha = float64(origin.A)
	}
	var channels [3]float64
	for i, t := range components {
		v, ok := channelValue(t, keywords, model.percent[i], i == model.hue)
		if !ok {
			return Color{}
		}
		channels[i] = v
	}
	if len(alphaTokens) == 1 {
		v, ok := channelValue(alphaTokens[0], keywords, 1, false)
		if !ok {
			return Color{}
		}
		alpha = v
	}
	if math.IsNaN(alpha) {
		alpha = 0
	}
	r, g, b := channelsToRGB(space, channels)
	return Color{Type: ColorRGBA, RGBA: RGBA{R: r, G: g, B: b, A: utils.Fl(min(1, max(0, alpha)))}}
}

func isIdent(t Token, value string) bool {
	ident, ok := t.(Ident)
	return ok && utils.AsciiLower(ident.Value) == value
}

// channelValue czyta kanał: liczbę, procent (ułamek [percent]), kąt (tylko kanał barwy, w stopniach),
// none (zero), słowo kluczowe kanału barwy wyjściowej albo wyrażenie matematyczne.
func channelValue(t Token, keywords map[string]float64, percent float64, hue bool) (float64, bool) {
	if ident, ok := t.(Ident); ok {
		name := utils.AsciiLower(ident.Value)
		if name == "none" {
			return 0, true
		}
		v, ok := keywords[name]
		return v, ok
	}
	v, ok := evalMath(t, keywords)
	if !ok {
		return 0, false
	}
	switch v.kind {
	case mathNumber:
		return v.v, true
	case mathPercent:
		if percent == 0 {
			return 0, false
		}
		return v.v / 100 * percent, true
	case mathAngle:
		return v.v, hue
	}
	return 0, false
}

// channelsFromRGB przelicza barwę sRGB na kanały przestrzeni [space] w postaci liczbowej.
func channelsFromRGB(space string, c RGBA) [3]float64 {
	r, g, b := float64(c.R), float64(c.G), float64(c.B)
	switch space {
	case "rgb":
		return [3]float64{r * 255, g * 255, b * 255}
	case "srgb":
		return [3]float64{r, g, b}
	case "hsl", "hwb":
		h, s, l := rgbToHSL(r, g, b)
		if space == "hsl" {
			return [3]float64{h, s * 100, l * 100}
		}
		return [3]float64{h, min(r, g, b) * 100, (1 - max(r, g, b)) * 100}
	}
	lr, lg, lb := srgbToLinear(utils.Fl(r)), srgbToLinear(utils.Fl(g)), srgbToLinear(utils.Fl(b))
	switch space {
	case "srgb-linear":
		return [3]float64{lr, lg, lb}
	case "oklab", "oklch":
		l, a, bb := linearSRGBToOKLab(lr, lg, lb)
		if space == "oklab" {
			return [3]float64{l, a, bb}
		}
		return polar(l, a, bb)
	}
	xyz := mulMatrix3(linearSRGBToXYZD65, [3]float64{lr, lg, lb})
	switch space {
	case "xyz", "xyz-d65":
		return xyz
	case "xyz-d50":
		return mulMatrix3(d65ToD50, xyz)
	case "lab", "lch":
		l, a, bb := xyzD50ToLab(mulMatrix3(d65ToD50, xyz))
		if space == "lab" {
			return [3]float64{l, a, bb}
		}
		return polar(l, a, bb)
	case "display-p3":
		return encode(mulMatrix3(xyzD65ToLinearP3, xyz), srgbEncode)
	case "display-p3-linear":
		return mulMatrix3(xyzD65ToLinearP3, xyz)
	case "a98-rgb":
		return encode(mulMatrix3(xyzD65ToLinearA98, xyz), a98Encode)
	case "rec2020":
		return encode(mulMatrix3(xyzD65ToLinearRec2020, xyz), rec2020Encode)
	case "prophoto-rgb":
		return encode(mulMatrix3(xyzD50ToLinearProPhoto, mulMatrix3(d65ToD50, xyz)), prophotoEncode)
	}
	return [3]float64{}
}

// channelsToRGB przelicza kanały przestrzeni [space] w postaci liczbowej na sRGB. Ograniczenia z
// CSS Color 4 dla składni bezwzględnej (L w [0, 100] albo [0, 1], chroma nieujemna, w i b w hwb()
// w [0, 100]) obowiązują tak samo; wynik spoza gamy jest przycinany (poza rgb(), które, jak w
// składni bezwzględnej, przycina dopiero rysowanie).
func channelsToRGB(space string, c [3]float64) (utils.Fl, utils.Fl, utils.Fl) {
	switch space {
	case "rgb":
		return utils.Fl(c[0] / 255), utils.Fl(c[1] / 255), utils.Fl(c[2] / 255)
	case "srgb":
		return utils.Fl(min(1, max(0, c[0]))), utils.Fl(min(1, max(0, c[1]))), utils.Fl(min(1, max(0, c[2])))
	case "hsl":
		return hslToRgb(utils.Fl(c[0]), utils.Fl(c[1]), utils.Fl(c[2]))
	case "hwb":
		w, k := min(1, max(0, c[1]/100)), min(1, max(0, c[2]/100))
		if w+k >= 1 {
			return utils.Fl(w / (w + k)), utils.Fl(w / (w + k)), utils.Fl(w / (w + k))
		}
		pr, pg, pb := hslToRgb(utils.Fl(c[0]), 100, 50)
		return utils.Fl(float64(pr)*(1-w-k) + w), utils.Fl(float64(pg)*(1-w-k) + w), utils.Fl(float64(pb)*(1-w-k) + w)
	}
	var linear [3]float64
	switch space {
	case "srgb-linear":
		linear = c
	case "lab", "lch":
		l, a, b := min(100, max(0, c[0])), c[1], c[2]
		if space == "lch" {
			a, b = cartesian(max(0, c[1]), c[2])
		}
		linear[0], linear[1], linear[2] = labToLinearSRGB(l, a, b)
	case "oklab", "oklch":
		l, a, b := min(1, max(0, c[0])), c[1], c[2]
		if space == "oklch" {
			a, b = cartesian(max(0, c[1]), c[2])
		}
		linear[0], linear[1], linear[2] = oklabToLinearSRGB(l, a, b)
	default:
		var xyz [3]float64
		switch space {
		case "xyz", "xyz-d65":
			xyz = c
		case "xyz-d50":
			xyz = mulMatrix3(d50ToD65, c)
		case "display-p3":
			xyz = mulMatrix3(linearP3ToXYZD65, encode(c, srgbDecode))
		case "display-p3-linear":
			xyz = mulMatrix3(linearP3ToXYZD65, c)
		case "a98-rgb":
			xyz = mulMatrix3(linearA98ToXYZD65, encode(c, a98Decode))
		case "rec2020":
			xyz = mulMatrix3(linearRec2020ToXYZD65, encode(c, rec2020Decode))
		case "prophoto-rgb":
			xyz = mulMatrix3(d50ToD65, mulMatrix3(linearProPhotoToXYZD50, encode(c, prophotoDecode)))
		}
		linear = mulMatrix3(xyzD65ToLinearSRGB, xyz)
	}
	return srgbGamma(linear[0]), srgbGamma(linear[1]), srgbGamma(linear[2])
}

// rgbToHSL: kąt w stopniach, nasycenie i jasność w [0, 1] (CSS Color 4 § 7.1, kod przykładowy).
func rgbToHSL(r, g, b float64) (h, s, l float64) {
	hi, lo := max(r, g, b), min(r, g, b)
	l = (hi + lo) / 2
	d := hi - lo
	if d != 0 {
		if l != 0 && l != 1 {
			s = (hi - l) / min(l, 1-l)
		}
		switch hi {
		case r:
			h = (g-b)/d + 6*b2f(g < b)
		case g:
			h = (b-r)/d + 2
		default:
			h = (r-g)/d + 4
		}
		h *= 60
	}
	if s < 0 {
		h += 180
		s = -s
	}
	if h >= 360 {
		h -= 360
	}
	return h, s, l
}

func b2f(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

// polar zamienia (l, a, b) na (l, c, h) z kątem w stopniach w [0, 360).
func polar(l, a, b float64) [3]float64 {
	h := math.Atan2(b, a) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return [3]float64{l, math.Hypot(a, b), h}
}

func cartesian(c, hueDegrees float64) (float64, float64) {
	return c * math.Cos(hueDegrees*math.Pi/180), c * math.Sin(hueDegrees*math.Pi/180)
}

// xyzD50ToLab: CIE Lab z bielą D50 (CSS Color 4 § 9).
func xyzD50ToLab(xyz [3]float64) (float64, float64, float64) {
	const epsilon, kappa = 216.0 / 24389, 24389.0 / 27
	white := [3]float64{0.3457 / 0.3585, 1, (1 - 0.3457 - 0.3585) / 0.3585}
	var f [3]float64
	for i := range f {
		v := xyz[i] / white[i]
		if v > epsilon {
			f[i] = math.Cbrt(v)
		} else {
			f[i] = (kappa*v + 16) / 116
		}
	}
	return 116*f[1] - 16, 500 * (f[0] - f[1]), 200 * (f[1] - f[2])
}

type matrix3 [3][3]float64

func mulMatrix3(m matrix3, v [3]float64) [3]float64 {
	return [3]float64{
		m[0][0]*v[0] + m[0][1]*v[1] + m[0][2]*v[2],
		m[1][0]*v[0] + m[1][1]*v[1] + m[1][2]*v[2],
		m[2][0]*v[0] + m[2][1]*v[1] + m[2][2]*v[2],
	}
}

func encode(v [3]float64, f func(float64) float64) [3]float64 {
	return [3]float64{f(v[0]), f(v[1]), f(v[2])}
}

// Funkcje przejścia (CSS Color 4 § 10); symetryczne względem zera, jak w kodzie przykładowym normy.
func signed(v float64, f func(float64) float64) float64 {
	if v < 0 {
		return -f(-v)
	}
	return f(v)
}

func srgbDecode(v float64) float64 {
	return signed(v, func(a float64) float64 {
		if a <= 0.04045 {
			return a / 12.92
		}
		return math.Pow((a+0.055)/1.055, 2.4)
	})
}

func srgbEncode(v float64) float64 {
	return signed(v, func(a float64) float64 {
		if a <= 0.0031308 {
			return a * 12.92
		}
		return 1.055*math.Pow(a, 1/2.4) - 0.055
	})
}

func a98Decode(v float64) float64 {
	return signed(v, func(a float64) float64 { return math.Pow(a, 563.0/256) })
}

func a98Encode(v float64) float64 {
	return signed(v, func(a float64) float64 { return math.Pow(a, 256.0/563) })
}

// rec2020: czysta funkcja potęgowa 2,4 (CSS Color 4, zmiana z 2025 r.; testy WPT rec2020-gamma-*)
func rec2020Decode(v float64) float64 {
	return signed(v, func(a float64) float64 { return math.Pow(a, 2.4) })
}

func rec2020Encode(v float64) float64 {
	return signed(v, func(a float64) float64 { return math.Pow(a, 1/2.4) })
}

func prophotoDecode(v float64) float64 {
	return signed(v, func(a float64) float64 {
		if a <= 16.0/512 {
			return a / 16
		}
		return math.Pow(a, 1.8)
	})
}

func prophotoEncode(v float64) float64 {
	return signed(v, func(a float64) float64 {
		if a >= 1.0/512 {
			return math.Pow(a, 1/1.8)
		}
		return 16 * a
	})
}

// Macierze z CSS Color 4 § 18 (kod przykładowy przeliczeń).
var (
	linearSRGBToXYZD65 = matrix3{
		{506752.0 / 1228815, 87881.0 / 245763, 12673.0 / 70218},
		{87098.0 / 409605, 175762.0 / 245763, 12673.0 / 175545},
		{7918.0 / 409605, 87881.0 / 737289, 1001167.0 / 1053270},
	}
	xyzD65ToLinearSRGB = matrix3{
		{12831.0 / 3959, -329.0 / 214, -1974.0 / 3959},
		{-851781.0 / 878810, 1648619.0 / 878810, 36519.0 / 878810},
		{705.0 / 12673, -2585.0 / 12673, 705.0 / 667},
	}
	linearP3ToXYZD65 = matrix3{
		{608311.0 / 1250200, 189793.0 / 714400, 198249.0 / 1000160},
		{35783.0 / 156275, 247089.0 / 357200, 198249.0 / 2500400},
		{0, 32229.0 / 714400, 5220557.0 / 5000800},
	}
	xyzD65ToLinearP3 = matrix3{
		{446124.0 / 178915, -333277.0 / 357830, -72051.0 / 178915},
		{-14852.0 / 17905, 63121.0 / 35810, 423.0 / 17905},
		{11844.0 / 330415, -50337.0 / 660830, 316169.0 / 330415},
	}
	linearA98ToXYZD65 = matrix3{
		{573536.0 / 994567, 263643.0 / 1176654, 187206.0 / 994567},
		{591459.0 / 1989134, 6239551.0 / 9411232, 374823.0 / 1989134},
		{53769.0 / 1989134, 351524.0 / 4706616, 4929758.0 / 4973835},
	}
	xyzD65ToLinearA98 = matrix3{
		{1829569.0 / 896150, -506331.0 / 896150, -308931.0 / 896150},
		{-851781.0 / 878810, 1648619.0 / 878810, 36519.0 / 878810},
		{16779.0 / 1248040, -147721.0 / 1248040, 1266979.0 / 1248040},
	}
	linearRec2020ToXYZD65 = matrix3{
		{63426534.0 / 99577255, 20160776.0 / 139408157, 47086771.0 / 278816314},
		{26158966.0 / 99577255, 472592308.0 / 697040785, 8267143.0 / 278816314},
		{0, 19567812.0 / 697040785, 295819943.0 / 278816314},
	}
	xyzD65ToLinearRec2020 = matrix3{
		{30757411.0 / 17917100, -6372589.0 / 17917100, -4539589.0 / 17917100},
		{-19765991.0 / 29648200, 47925759.0 / 29648200, 467509.0 / 29648200},
		{792561.0 / 44930125, -1921689.0 / 44930125, 42328811.0 / 44930125},
	}
	linearProPhotoToXYZD50 = matrix3{
		{0.79776664490064230, 0.13518129740053308, 0.03134773412839220},
		{0.28807482881940130, 0.71183523424187300, 0.00008993693872564},
		{0, 0, 0.82510460251046020},
	}
	xyzD50ToLinearProPhoto = matrix3{
		{1.34578688164715830, -0.25557208737979464, -0.05110186497554526},
		{-0.54463070512490190, 1.50824774284514680, 0.02052744743642139},
		{0, 0, 1.21196754563894520},
	}
	// adaptacja chromatyczna Bradforda
	d65ToD50 = matrix3{
		{1.0479297925449969, 0.022946870601609652, -0.05019226628920524},
		{0.02962780877005599, 0.9904344267538799, -0.017073799063418826},
		{-0.009243040646204504, 0.015055191490298152, 0.7518742814281371},
	}
	d50ToD65 = matrix3{
		{0.955473421488075, -0.02309845494876471, 0.06325924320057072},
		{-0.0283697093338637, 1.0099953980813041, 0.021041441191917323},
		{0.012314014864481998, -0.020507649298898964, 1.330365926242124},
	}
)

// Wyrażenia matematyczne w kanałach barwy (CSS Values 4 § 10): liczby, procenty i kąty (w stopniach),
// słowa kluczowe kanałów i stałe e, pi, infinity, nan; funkcje calc, min, max, clamp, abs, sign,
// sqrt, pow, exp, log, hypot, sin, cos, tan.
type mathKind uint8

const (
	mathNumber mathKind = iota
	mathPercent
	mathAngle
)

type mathValue struct {
	v    float64
	kind mathKind
}

var angleToDegrees = map[string]float64{"deg": 1, "grad": 360.0 / 400, "rad": 180 / math.Pi, "turn": 360}

// evalMath liczy pojedynczy składnik: wartość prostą albo funkcję matematyczną.
func evalMath(t Token, keywords map[string]float64) (mathValue, bool) {
	switch t := t.(type) {
	case Number:
		return mathValue{float64(t.ValueF), mathNumber}, true
	case Percentage:
		return mathValue{float64(t.ValueF), mathPercent}, true
	case Dimension:
		if f, ok := angleToDegrees[utils.AsciiLower(t.Unit)]; ok {
			return mathValue{float64(t.ValueF) * f, mathAngle}, true
		}
	case FunctionBlock:
		return evalMathFunction(utils.AsciiLower(t.Name), t.Arguments, keywords)
	}
	return mathValue{}, false
}

func evalMathFunction(name string, arguments []Token, keywords map[string]float64) (mathValue, bool) {
	var args []mathValue
	for _, part := range SplitOnComma(arguments) {
		v, ok := evalSum(RemoveWhitespace(part), keywords)
		if !ok {
			return mathValue{}, false
		}
		args = append(args, v)
	}
	sameKind := func() bool {
		for _, a := range args[1:] {
			if a.kind != args[0].kind {
				return false
			}
		}
		return len(args) > 0
	}
	numbers := func(n int) bool {
		if len(args) != n {
			return false
		}
		for _, a := range args {
			if a.kind != mathNumber {
				return false
			}
		}
		return true
	}
	switch name {
	case "calc":
		if len(args) == 1 {
			return args[0], true
		}
	case "min", "max":
		if sameKind() {
			out := args[0]
			for _, a := range args[1:] {
				if (name == "min") == (a.v < out.v) {
					out = a
				}
			}
			return out, true
		}
	case "clamp":
		if len(args) == 3 && sameKind() {
			return mathValue{max(args[0].v, min(args[1].v, args[2].v)), args[0].kind}, true
		}
	case "abs":
		if len(args) == 1 {
			return mathValue{math.Abs(args[0].v), args[0].kind}, true
		}
	case "sign":
		if len(args) == 1 {
			s := 0.
			if args[0].v > 0 {
				s = 1
			} else if args[0].v < 0 {
				s = -1
			}
			return mathValue{s, mathNumber}, true
		}
	case "sqrt":
		if numbers(1) {
			return mathValue{math.Sqrt(args[0].v), mathNumber}, true
		}
	case "exp":
		if numbers(1) {
			return mathValue{math.Exp(args[0].v), mathNumber}, true
		}
	case "log":
		if numbers(1) {
			return mathValue{math.Log(args[0].v), mathNumber}, true
		}
		if numbers(2) {
			return mathValue{math.Log(args[0].v) / math.Log(args[1].v), mathNumber}, true
		}
	case "pow":
		if numbers(2) {
			return mathValue{math.Pow(args[0].v, args[1].v), mathNumber}, true
		}
	case "hypot":
		if sameKind() {
			s := 0.
			for _, a := range args {
				s += a.v * a.v
			}
			return mathValue{math.Sqrt(s), args[0].kind}, true
		}
	case "sin", "cos", "tan":
		if len(args) == 1 && args[0].kind != mathPercent {
			rad := args[0].v
			if args[0].kind == mathAngle {
				rad *= math.Pi / 180
			}
			f := map[string]func(float64) float64{"sin": math.Sin, "cos": math.Cos, "tan": math.Tan}[name]
			return mathValue{f(rad), mathNumber}, true
		}
	}
	return mathValue{}, false
}

// evalSum: suma iloczynów; + i - muszą być osobnymi znakami (CSS Values 4 § 10.1).
func evalSum(tokens []Token, keywords map[string]float64) (mathValue, bool) {
	value, rest, ok := evalProduct(tokens, keywords)
	for ok && len(rest) > 0 {
		op := rest[0]
		if !IsLiteral(op, "+") && !IsLiteral(op, "-") {
			return mathValue{}, false
		}
		var right mathValue
		right, rest, ok = evalProduct(rest[1:], keywords)
		if !ok || right.kind != value.kind {
			return mathValue{}, false
		}
		if IsLiteral(op, "+") {
			value.v += right.v
		} else {
			value.v -= right.v
		}
	}
	return value, ok
}

func evalProduct(tokens []Token, keywords map[string]float64) (mathValue, []Token, bool) {
	value, ok := evalTerm(tokens, keywords)
	if !ok {
		return mathValue{}, nil, false
	}
	rest := tokens[1:]
	for len(rest) > 0 && (IsLiteral(rest[0], "*") || IsLiteral(rest[0], "/")) {
		if len(rest) < 2 {
			return mathValue{}, nil, false
		}
		right, ok := evalTerm(rest[1:], keywords)
		if !ok {
			return mathValue{}, nil, false
		}
		if IsLiteral(rest[0], "*") {
			switch {
			case value.kind == mathNumber:
				value = mathValue{value.v * right.v, right.kind}
			case right.kind == mathNumber:
				value.v *= right.v
			default:
				return mathValue{}, nil, false
			}
		} else {
			switch {
			case right.kind == mathNumber:
				value.v /= right.v
			case right.kind == value.kind:
				value = mathValue{value.v / right.v, mathNumber}
			default:
				return mathValue{}, nil, false
			}
		}
		rest = rest[2:]
	}
	return value, rest, true
}

func evalTerm(tokens []Token, keywords map[string]float64) (mathValue, bool) {
	if len(tokens) == 0 {
		return mathValue{}, false
	}
	switch t := tokens[0].(type) {
	case Ident:
		name := utils.AsciiLower(t.Value)
		switch name {
		case "e":
			return mathValue{math.E, mathNumber}, true
		case "pi":
			return mathValue{math.Pi, mathNumber}, true
		case "infinity":
			return mathValue{math.Inf(1), mathNumber}, true
		case "-infinity":
			return mathValue{math.Inf(-1), mathNumber}, true
		case "nan":
			return mathValue{math.NaN(), mathNumber}, true
		}
		if v, ok := keywords[name]; ok {
			return mathValue{v, mathNumber}, true
		}
		return mathValue{}, false
	case ParenthesesBlock:
		return evalSum(RemoveWhitespace(t.Arguments), keywords)
	}
	return evalMath(tokens[0], keywords)
}
