package parser

import (
	"fmt"
	"image/color"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

var (
	// ColorKeywords maps color names to RGBA values
	ColorKeywords = map[string]Color{}

	hashRegexps = [...]hashRegexp{
		{multiplier: 2., regexp: regexp.MustCompile(`(?i)^([\da-f])([\da-f])([\da-f])$`)},
		{multiplier: 1., regexp: regexp.MustCompile(`(?i)^([\da-f]{2})([\da-f]{2})([\da-f]{2})$`)},
		// gopdfgo: zapis z kanałem alfa, #rgba i #rrggbbaa (CSS Color 4 § 5.2)
		{multiplier: 2., regexp: regexp.MustCompile(`(?i)^([\da-f])([\da-f])([\da-f])([\da-f])$`)},
		{multiplier: 1., regexp: regexp.MustCompile(`(?i)^([\da-f]{2})([\da-f]{2})([\da-f]{2})([\da-f]{2})$`)},
	}

	// (r, g, b) := range 0..255
	basicColorKeywords = map[string][3]uint8{
		"black":   {0, 0, 0},
		"silver":  {192, 192, 192},
		"gray":    {128, 128, 128},
		"white":   {255, 255, 255},
		"maroon":  {128, 0, 0},
		"red":     {255, 0, 0},
		"purple":  {128, 0, 128},
		"fuchsia": {255, 0, 255},
		"green":   {0, 128, 0},
		"lime":    {0, 255, 0},
		"olive":   {128, 128, 0},
		"yellow":  {255, 255, 0},
		"navy":    {0, 0, 128},
		"blue":    {0, 0, 255},
		"teal":    {0, 128, 128},
		"aqua":    {0, 255, 255},
	}

	// (r, g, b) := range 0..255
	extendedColorKeywords = map[string][3]uint8{
		"aliceblue":            {240, 248, 255},
		"antiquewhite":         {250, 235, 215},
		"aqua":                 {0, 255, 255},
		"aquamarine":           {127, 255, 212},
		"azure":                {240, 255, 255},
		"beige":                {245, 245, 220},
		"bisque":               {255, 228, 196},
		"black":                {0, 0, 0},
		"blanchedalmond":       {255, 235, 205},
		"blue":                 {0, 0, 255},
		"blueviolet":           {138, 43, 226},
		"brown":                {165, 42, 42},
		"burlywood":            {222, 184, 135},
		"cadetblue":            {95, 158, 160},
		"chartreuse":           {127, 255, 0},
		"chocolate":            {210, 105, 30},
		"coral":                {255, 127, 80},
		"cornflowerblue":       {100, 149, 237},
		"cornsilk":             {255, 248, 220},
		"crimson":              {220, 20, 60},
		"cyan":                 {0, 255, 255},
		"darkblue":             {0, 0, 139},
		"darkcyan":             {0, 139, 139},
		"darkgoldenrod":        {184, 134, 11},
		"darkgray":             {169, 169, 169},
		"darkgreen":            {0, 100, 0},
		"darkgrey":             {169, 169, 169},
		"darkkhaki":            {189, 183, 107},
		"darkmagenta":          {139, 0, 139},
		"darkolivegreen":       {85, 107, 47},
		"darkorange":           {255, 140, 0},
		"darkorchid":           {153, 50, 204},
		"darkred":              {139, 0, 0},
		"darksalmon":           {233, 150, 122},
		"darkseagreen":         {143, 188, 143},
		"darkslateblue":        {72, 61, 139},
		"darkslategray":        {47, 79, 79},
		"darkslategrey":        {47, 79, 79},
		"darkturquoise":        {0, 206, 209},
		"darkviolet":           {148, 0, 211},
		"deeppink":             {255, 20, 147},
		"deepskyblue":          {0, 191, 255},
		"dimgray":              {105, 105, 105},
		"dimgrey":              {105, 105, 105},
		"dodgerblue":           {30, 144, 255},
		"firebrick":            {178, 34, 34},
		"floralwhite":          {255, 250, 240},
		"forestgreen":          {34, 139, 34},
		"fuchsia":              {255, 0, 255},
		"gainsboro":            {220, 220, 220},
		"ghostwhite":           {248, 248, 255},
		"gold":                 {255, 215, 0},
		"goldenrod":            {218, 165, 32},
		"gray":                 {128, 128, 128},
		"green":                {0, 128, 0},
		"greenyellow":          {173, 255, 47},
		"grey":                 {128, 128, 128},
		"honeydew":             {240, 255, 240},
		"hotpink":              {255, 105, 180},
		"indianred":            {205, 92, 92},
		"indigo":               {75, 0, 130},
		"ivory":                {255, 255, 240},
		"khaki":                {240, 230, 140},
		"lavender":             {230, 230, 250},
		"lavenderblush":        {255, 240, 245},
		"lawngreen":            {124, 252, 0},
		"lemonchiffon":         {255, 250, 205},
		"lightblue":            {173, 216, 230},
		"lightcoral":           {240, 128, 128},
		"lightcyan":            {224, 255, 255},
		"lightgoldenrodyellow": {250, 250, 210},
		"lightgray":            {211, 211, 211},
		"lightgreen":           {144, 238, 144},
		"lightgrey":            {211, 211, 211},
		"lightpink":            {255, 182, 193},
		"lightsalmon":          {255, 160, 122},
		"lightseagreen":        {32, 178, 170},
		"lightskyblue":         {135, 206, 250},
		"lightslategray":       {119, 136, 153},
		"lightslategrey":       {119, 136, 153},
		"lightsteelblue":       {176, 196, 222},
		"lightyellow":          {255, 255, 224},
		"lime":                 {0, 255, 0},
		"limegreen":            {50, 205, 50},
		"linen":                {250, 240, 230},
		"magenta":              {255, 0, 255},
		"maroon":               {128, 0, 0},
		"mediumaquamarine":     {102, 205, 170},
		"mediumblue":           {0, 0, 205},
		"mediumorchid":         {186, 85, 211},
		"mediumpurple":         {147, 112, 219},
		"mediumseagreen":       {60, 179, 113},
		"mediumslateblue":      {123, 104, 238},
		"mediumspringgreen":    {0, 250, 154},
		"mediumturquoise":      {72, 209, 204},
		"mediumvioletred":      {199, 21, 133},
		"midnightblue":         {25, 25, 112},
		"mintcream":            {245, 255, 250},
		"mistyrose":            {255, 228, 225},
		"moccasin":             {255, 228, 181},
		"navajowhite":          {255, 222, 173},
		"navy":                 {0, 0, 128},
		"oldlace":              {253, 245, 230},
		"olive":                {128, 128, 0},
		"olivedrab":            {107, 142, 35},
		"orange":               {255, 165, 0},
		"orangered":            {255, 69, 0},
		"orchid":               {218, 112, 214},
		"palegoldenrod":        {238, 232, 170},
		"palegreen":            {152, 251, 152},
		"paleturquoise":        {175, 238, 238},
		"palevioletred":        {219, 112, 147},
		"papayawhip":           {255, 239, 213},
		"peachpuff":            {255, 218, 185},
		"peru":                 {205, 133, 63},
		"pink":                 {255, 192, 203},
		"plum":                 {221, 160, 221},
		"powderblue":           {176, 224, 230},
		"purple":               {128, 0, 128},
		"red":                  {255, 0, 0},
		"rosybrown":            {188, 143, 143},
		"royalblue":            {65, 105, 225},
		"saddlebrown":          {139, 69, 19},
		"salmon":               {250, 128, 114},
		"sandybrown":           {244, 164, 96},
		"seagreen":             {46, 139, 87},
		"seashell":             {255, 245, 238},
		"sienna":               {160, 82, 45},
		"silver":               {192, 192, 192},
		"skyblue":              {135, 206, 235},
		"slateblue":            {106, 90, 205},
		"slategray":            {112, 128, 144},
		"slategrey":            {112, 128, 144},
		"snow":                 {255, 250, 250},
		"springgreen":          {0, 255, 127},
		"steelblue":            {70, 130, 180},
		"tan":                  {210, 180, 140},
		"teal":                 {0, 128, 128},
		"thistle":              {216, 191, 216},
		"tomato":               {255, 99, 71},
		"turquoise":            {64, 224, 208},
		"violet":               {238, 130, 238},
		"wheat":                {245, 222, 179},
		"white":                {255, 255, 255},
		"whitesmoke":           {245, 245, 245},
		"yellow":               {255, 255, 0},
		"yellowgreen":          {154, 205, 50},
	}

	// (r, g, b, a) in 0..1 or a string marker
	specialColorKeywords = map[string]Color{
		"currentcolor": {Type: ColorCurrentColor},
		"transparent":  {Type: ColorRGBA, RGBA: RGBA{R: 0., G: 0., B: 0., A: 0.}},
	}
)

func init() {
	for k, v := range specialColorKeywords {
		ColorKeywords[k] = v
	}
	// 255 maps to 1, 0 to 0, the rest is linear.
	for k, v := range basicColorKeywords {
		ColorKeywords[k] = Color{Type: ColorRGBA, RGBA: RGBA{utils.Fl(v[0]) / 255., utils.Fl(v[1]) / 255., utils.Fl(v[2]) / 255., 1.}}
	}
	for k, v := range extendedColorKeywords {
		ColorKeywords[k] = Color{Type: ColorRGBA, RGBA: RGBA{utils.Fl(v[0]) / 255., utils.Fl(v[1]) / 255., utils.Fl(v[2]) / 255., 1.}}
	}
}

// values in [-1, 1]
type RGBA struct {
	R, G, B, A utils.Fl
}

func (color RGBA) Unpack() (r, g, b, a utils.Fl) {
	return color.R, color.G, color.B, color.A
}

func (color RGBA) IsNone() bool {
	return color == RGBA{}
}

func clamp(v utils.Fl) utils.Fl {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

var _ color.Color = RGBA{}

// RGBA returns the alpha-premultiplied red, green, blue and alpha values
// for the color. Each value ranges within [0, 0xffff], but is represented
// by a uint32 so that multiplying by a blend factor up to 0xffff will not
// overflow.
//
// An alpha-premultiplied color component c has been scaled by alpha (a),
// so has valid values 0 <= c <= a.
func (c RGBA) RGBA() (r, g, b, a uint32) {
	c.R, c.G, c.B, c.A = clamp(c.R), clamp(c.G), clamp(c.B), clamp(c.A)
	sa := 0xFFFF * c.A
	return uint32(c.R * sa), uint32(c.G * sa), uint32(c.B * sa), uint32(sa)
}

type ColorType uint8

const (
	// ColorInvalid is an empty or invalid color specification.
	ColorInvalid ColorType = iota
	// ColorCurrentColor represents the special value "currentColor"
	// which need document context to be resolved.
	ColorCurrentColor
	// ColorRGBA is a standard rgba color.
	ColorRGBA
)

type Color struct {
	Type ColorType
	RGBA RGBA
}

func (c Color) IsNone() bool {
	return c.Type == ColorInvalid
}

type hashRegexp struct {
	regexp     *regexp.Regexp
	multiplier int
}

func mustParseHexa(s string) utils.Fl {
	out, err := strconv.ParseInt(s, 16, 0)
	if err != nil {
		panic(fmt.Sprintf("unexpected error: %s", err))
	}
	return utils.Fl(out)
}

// ParseColorString tokenize the input before calling `ParseColor`.
func ParseColorString(color string) Color {
	l := Tokenize([]byte(color), true)
	return ParseColor(ParseOneComponentValue(l))
}

// Parse a color value as defined in `CSS Color Level 3  <http://www.w3.org/TR/css3-color/>`.
// Returns :
//   - zero Color if the input is not a valid color value. (No error is returned.)
//   - CurrentColor for the *currentColor* keyword
//   - RGBA color for every other values (including keywords, HSL && HSLA.)
//     The alpha channel is clipped to [0, 1] but red, green, or blue can be out of range
//     (eg. “rgb(-10%, 120%, 0%)“ is represented as “(-0.1, 1.2, 0, 1)“.
func ParseColor(token Token) Color {
	switch token := token.(type) {
	case Ident:
		return ColorKeywords[utils.AsciiLower(token.Value)]
	case Hash:
		for _, hashReg := range hashRegexps {
			match := hashReg.regexp.FindStringSubmatch(token.Value)
			if len(match) >= 4 {
				r := mustParseHexa(strings.Repeat(match[1], hashReg.multiplier)) / 255
				g := mustParseHexa(strings.Repeat(match[2], hashReg.multiplier)) / 255
				b := mustParseHexa(strings.Repeat(match[3], hashReg.multiplier)) / 255
				a := utils.Fl(1)
				if len(match) == 5 {
					a = mustParseHexa(strings.Repeat(match[4], hashReg.multiplier)) / 255
				}
				return Color{Type: ColorRGBA, RGBA: RGBA{R: r, G: g, B: b, A: a}}
			}
		}
	case FunctionBlock:
		// gopdfgo: składnia CSS Color 4: składowe rozdzielone spacjami, alfa po ukośniku, liczby i procenty
		// (także ułamkowe) w rgb(), kąt z jednostką w hsl(), słowo none jako zero; rgba() i hsla() to
		// aliasy. Źródło przyjmowało tylko składnię z przecinkami, całkowite składowe rgb() i hue bez jednostki.
		name := utils.AsciiLower(token.Name)
		if name == "color-mix" {
			return parseColorMix(token.Arguments)
		}
		switch name {
		case "rgb", "rgba", "hsl", "hsla", "hwb", "lab", "lch", "oklab", "oklch":
		default:
			return Color{}
		}
		var components, alphaTokens []Token
		legacy, afterSlash := false, false
		for _, arg := range RemoveWhitespace(token.Arguments) {
			switch {
			case IsLiteral(arg, ","):
				legacy = true
			case IsLiteral(arg, "/"):
				if afterSlash || legacy {
					return Color{}
				}
				afterSlash = true
			case afterSlash:
				alphaTokens = append(alphaTokens, arg)
			default:
				components = append(components, arg)
			}
		}
		if legacy {
			// składnia z przecinkami: przecinek między każdą parą wartości
			raw := RemoveWhitespace(token.Arguments)
			if len(raw) != 5 && len(raw) != 7 {
				return Color{}
			}
			for index, arg := range raw {
				if IsLiteral(arg, ",") != (index%2 == 1) {
					return Color{}
				}
			}
			if len(components) == 4 {
				components, alphaTokens = components[:3], components[3:]
			}
		}
		if len(components) != 3 || len(alphaTokens) > 1 {
			return Color{}
		}
		alpha := utils.Fl(1)
		if len(alphaTokens) == 1 {
			switch a := alphaTokens[0].(type) {
			case Number:
				alpha = a.ValueF
			case Percentage:
				alpha = a.ValueF / 100
			case Ident:
				if utils.AsciiLower(a.Value) != "none" || legacy {
					return Color{}
				}
				alpha = 0
			default:
				return Color{}
			}
			alpha = min(1, max(0, alpha))
		}
		// składowa jako ułamek zakresu: liczba dzielona przez [scale], procent przez 100
		component := func(t Token, scale utils.Fl) (utils.Fl, bool) {
			switch v := t.(type) {
			case Number:
				return v.ValueF / scale, true
			case Percentage:
				return v.ValueF / 100, true
			case Ident:
				if utils.AsciiLower(v.Value) == "none" && !legacy {
					return 0, true
				}
			}
			return 0, false
		}
		if name == "rgb" || name == "rgba" {
			r, okR := component(components[0], 255)
			g, okG := component(components[1], 255)
			b, okB := component(components[2], 255)
			if !okR || !okG || !okB {
				return Color{}
			}
			if legacy {
				// w składni z przecinkami wszystkie trzy składowe muszą być tego samego rodzaju
				_, n0 := components[0].(Number)
				_, n1 := components[1].(Number)
				_, n2 := components[2].(Number)
				if n0 != n1 || n1 != n2 {
					return Color{}
				}
			}
			return Color{Type: ColorRGBA, RGBA: RGBA{R: r, G: g, B: b, A: alpha}}
		}
		// kąt barwy w stopniach: liczba albo wymiar kątowy
		parseHue := func(t Token) (utils.Fl, bool) {
			switch h := t.(type) {
			case Number:
				return h.ValueF, true
			case Dimension:
				switch utils.AsciiLower(h.Unit) {
				case "deg":
					return h.ValueF, true
				case "grad":
					return h.ValueF * 360 / 400, true
				case "rad":
					return h.ValueF * 180 / math.Pi, true
				case "turn":
					return h.ValueF * 360, true
				}
			case Ident:
				if utils.AsciiLower(h.Value) == "none" && !legacy {
					return 0, true
				}
			}
			return 0, false
		}
		if name != "hsl" && name != "hsla" {
			// gopdfgo: hwb(), lab(), lch(), oklab(), oklch() z CSS Color 4, tylko w składni ze spacjami.
			// Wynik sprowadzamy do sRGB; barwy spoza gamy przycinamy składowa po składowej (norma
			// zaleca odwzorowanie gamy w OKLCH, którego tu nie robimy).
			if legacy {
				return Color{}
			}
			var r, g, b float64
			switch name {
			case "hwb":
				hue, okH := parseHue(components[0])
				white, okW := component(components[1], 100)
				black, okB := component(components[2], 100)
				if !okH || !okW || !okB {
					return Color{}
				}
				w, k := float64(min(1, max(0, white))), float64(min(1, max(0, black)))
				if w+k >= 1 {
					r, g, b = w/(w+k), w/(w+k), w/(w+k)
				} else {
					pr, pg, pb := hslToRgb(hue, 100, 50)
					r, g, b = float64(pr)*(1-w-k)+w, float64(pg)*(1-w-k)+w, float64(pb)*(1-w-k)+w
				}
				return Color{Type: ColorRGBA, RGBA: RGBA{R: utils.Fl(r), G: utils.Fl(g), B: utils.Fl(b), A: alpha}}
			case "lab", "lch", "oklab", "oklch":
				ok := name == "oklab" || name == "oklch"
				polar := name == "lch" || name == "oklch"
				// zakresy odpowiadające 100%: L 100 (lab) albo 1 (oklab); a, b 125 albo 0,4; C 150 albo 0,4
				lScale, abScale, cScale := utils.Fl(100), utils.Fl(125), utils.Fl(150)
				if ok {
					lScale, abScale, cScale = 1, 0.4, 0.4
				}
				l, okL := component(components[0], lScale)
				if !okL {
					return Color{}
				}
				lightness := float64(min(1, max(0, l)) * lScale)
				var ca, cb float64
				if polar {
					chroma, okC := component(components[1], cScale)
					hue, okH := parseHue(components[2])
					if !okC || !okH {
						return Color{}
					}
					c := float64(max(0, chroma) * cScale)
					ca, cb = c*math.Cos(float64(hue)*math.Pi/180), c*math.Sin(float64(hue)*math.Pi/180)
				} else {
					va, okA := component(components[1], abScale)
					vb, okB := component(components[2], abScale)
					if !okA || !okB {
						return Color{}
					}
					ca, cb = float64(va*abScale), float64(vb*abScale)
				}
				if ok {
					r, g, b = oklabToLinearSRGB(lightness, ca, cb)
				} else {
					r, g, b = labToLinearSRGB(lightness, ca, cb)
				}
				return Color{Type: ColorRGBA, RGBA: RGBA{R: srgbGamma(r), G: srgbGamma(g), B: srgbGamma(b), A: alpha}}
			}
			return Color{}
		}
		hue, okH := parseHue(components[0])
		if !okH {
			return Color{}
		}
		saturation, okS := component(components[1], 100)
		lightness, okL := component(components[2], 100)
		if _, isNumber := components[1].(Number); isNumber && legacy {
			okS = false
		}
		if _, isNumber := components[2].(Number); isNumber && legacy {
			okL = false
		}
		if !okS || !okL {
			return Color{}
		}
		r, g, b := hslToRgb(hue, saturation*100, lightness*100)
		return Color{Type: ColorRGBA, RGBA: RGBA{R: r, G: g, B: b, A: alpha}}
	}
	return Color{}
}

// srgbGamma koduje składową liniową sRGB i przycina ją do zakresu [0, 1].
func srgbGamma(v float64) utils.Fl {
	if v <= 0.0031308 {
		v *= 12.92
	} else {
		v = 1.055*math.Pow(v, 1/2.4) - 0.055
	}
	return utils.Fl(min(1, max(0, v)))
}

// oklabToLinearSRGB przelicza OKLab na liniowe sRGB (macierze z CSS Color 4, rozdz. 10).
func oklabToLinearSRGB(l, a, b float64) (float64, float64, float64) {
	l_ := l + 0.3963377774*a + 0.2158037573*b
	m_ := l - 0.1055613458*a - 0.0638541728*b
	s_ := l - 0.0894841775*a - 1.2914855480*b
	l_, m_, s_ = l_*l_*l_, m_*m_*m_, s_*s_*s_
	return 4.0767416621*l_ - 3.3077115913*m_ + 0.2309699292*s_,
		-1.2684380046*l_ + 2.6097574011*m_ - 0.3413193965*s_,
		-0.0041960863*l_ - 0.7034186147*m_ + 1.7076147010*s_
}

// labToLinearSRGB przelicza CIE Lab (biel D50) na liniowe sRGB: Lab → XYZ D50 → XYZ D65
// (adaptacja Bradforda) → sRGB.
func labToLinearSRGB(l, a, b float64) (float64, float64, float64) {
	const epsilon, kappa = 216.0 / 24389, 24389.0 / 27
	fy := (l + 16) / 116
	fx := a/500 + fy
	fz := fy - b/200
	x, z := fx*fx*fx, fz*fz*fz
	if x <= epsilon {
		x = (116*fx - 16) / kappa
	}
	if z <= epsilon {
		z = (116*fz - 16) / kappa
	}
	y := l / kappa
	if l > kappa*epsilon {
		y = fy * fy * fy
	}
	x, z = x*0.3457/0.3585, z*(1-0.3457-0.3585)/0.3585
	x, y, z = 0.9554734527042182*x-0.023098536874261423*y+0.0632593086610217*z,
		-0.028369706963208136*x+1.0099954580058226*y+0.021041398966943008*z,
		0.012314001688319899*x-0.020507696433477912*y+1.3303659366080753*z
	return 3.2409699419045226*x - 1.537383177570094*y - 0.4986107602930034*z,
		-0.9692436362808796*x + 1.8759675015077202*y + 0.04155505740717559*z,
		0.05563007969699366*x - 0.20397695888897652*y + 1.0569715142428786*z
}

func hslToRgb(_hue utils.Fl, saturation, lightness utils.Fl) (utils.Fl, utils.Fl, utils.Fl) {
	hue := float64(_hue) / 360
	hue = hue - math.Floor(hue)
	saturation = min(1., max(0, saturation/100))
	lightness = min(1, max(0, lightness/100))

	// Translated from ABC: http://www.w3.org/TR/css3-color/#hsl-color
	hueToRgb := func(m1, m2, h float64) utils.Fl {
		if h < 0 {
			h += 1.
		}
		if h > 1 {
			h -= 1.
		}
		if h*6 < 1 {
			return utils.Fl(m1 + (m2-m1)*h*6)
		}
		if h*2 < 1 {
			return utils.Fl(m2)
		}
		if h*3 < 2 {
			return utils.Fl(m1 + (m2-m1)*(2./3-h)*6)
		}
		return utils.Fl(m1)
	}
	var m1, m2 float64
	if lightness <= 0.5 {
		m2 = float64(lightness * (saturation + 1.))
	} else {
		m2 = float64(lightness + saturation - lightness*saturation)
	}
	m1 = float64(lightness*2) - m2
	return hueToRgb(m1, m2, hue+1./3), hueToRgb(m1, m2, hue), hueToRgb(m1, m2, hue-1./3)
}

// linearSRGBToOKLab: odwrotność oklabToLinearSRGB.
func linearSRGBToOKLab(r, g, b float64) (float64, float64, float64) {
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return 0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*s
}

func srgbToLinear(v utils.Fl) float64 {
	f := float64(v)
	if f <= 0.04045 {
		return f / 12.92
	}
	return math.Pow((f+0.055)/1.055, 2.4)
}

// parseColorMix (gopdfgo): color-mix(in <przestrzeń>, <barwa> <procent>?, <barwa> <procent>?)
// z CSS Color 5 § 3. Obsługiwane przestrzenie: srgb, srgb-linear, oklab i oklch (krótsza droga po
// kącie barwy); pozostałe są odrzucane. currentColor w argumentach nie jest obsługiwany, bo wynik
// musiałby powstać dopiero przy obliczaniu stylu.
func parseColorMix(arguments []Token) Color {
	var parts [][]Token
	for _, part := range SplitOnComma(RemoveWhitespace(arguments)) {
		parts = append(parts, RemoveWhitespace(part))
	}
	if len(parts) != 3 || len(parts[0]) < 2 {
		return Color{}
	}
	keyword := func(t Token) string {
		if ident, ok := t.(Ident); ok {
			return utils.AsciiLower(ident.Value)
		}
		return ""
	}
	if keyword(parts[0][0]) != "in" {
		return Color{}
	}
	space := keyword(parts[0][1])
	switch {
	case space == "oklch" && len(parts[0]) == 4 && keyword(parts[0][2]) == "shorter" && keyword(parts[0][3]) == "hue":
	case len(parts[0]) == 2 && (space == "srgb" || space == "srgb-linear" || space == "oklab" || space == "oklch"):
	default:
		return Color{}
	}
	var colors [2]RGBA
	var weights [2]float64
	var hasWeight [2]bool
	for i, part := range parts[1:] {
		found := false
		for _, t := range part {
			if p, ok := t.(Percentage); ok {
				if hasWeight[i] || p.ValueF < 0 || p.ValueF > 100 {
					return Color{}
				}
				weights[i], hasWeight[i] = float64(p.ValueF)/100, true
				continue
			}
			c := ParseColor(t)
			if c.Type != ColorRGBA || found {
				return Color{}
			}
			colors[i], found = c.RGBA, true
		}
		if !found {
			return Color{}
		}
	}
	switch {
	case !hasWeight[0] && !hasWeight[1]:
		weights = [2]float64{0.5, 0.5}
	case !hasWeight[0]:
		weights[0] = 1 - weights[1]
	case !hasWeight[1]:
		weights[1] = 1 - weights[0]
	}
	sum := weights[0] + weights[1]
	if sum == 0 {
		return Color{}
	}
	// suma poniżej 100% zmniejsza krycie wyniku
	alphaScale := min(sum, 1)
	w0, w1 := weights[0]/sum, weights[1]/sum

	// składowe w przestrzeni mieszania
	var comps [2][3]float64
	for i, c := range colors {
		switch space {
		case "srgb":
			comps[i] = [3]float64{float64(c.R), float64(c.G), float64(c.B)}
		case "srgb-linear":
			comps[i] = [3]float64{srgbToLinear(c.R), srgbToLinear(c.G), srgbToLinear(c.B)}
		default:
			l, a, b := linearSRGBToOKLab(srgbToLinear(c.R), srgbToLinear(c.G), srgbToLinear(c.B))
			if space == "oklch" {
				comps[i] = [3]float64{l, math.Hypot(a, b), math.Atan2(b, a)}
			} else {
				comps[i] = [3]float64{l, a, b}
			}
		}
	}
	a0, a1 := float64(colors[0].A), float64(colors[1].A)
	alpha := a0*w0 + a1*w1
	var mixed [3]float64
	if alpha != 0 {
		for k := range mixed {
			if space == "oklch" && k == 2 {
				// kąt barwy: krótsza droga, bez przemnażania przez krycie; barwa bezbarwna przejmuje kąt drugiej
				h0, h1 := comps[0][2], comps[1][2]
				if comps[0][1] < 1e-5 {
					h0 = h1
				} else if comps[1][1] < 1e-5 {
					h1 = h0
				}
				if h1-h0 > math.Pi {
					h0 += 2 * math.Pi
				} else if h1-h0 < -math.Pi {
					h1 += 2 * math.Pi
				}
				mixed[k] = h0*w0 + h1*w1
				continue
			}
			mixed[k] = (comps[0][k]*a0*w0 + comps[1][k]*a1*w1) / alpha
		}
	}
	out := RGBA{A: utils.Fl(alpha * alphaScale)}
	switch space {
	case "srgb":
		clamp := func(v float64) utils.Fl { return utils.Fl(min(1, max(0, v))) }
		out.R, out.G, out.B = clamp(mixed[0]), clamp(mixed[1]), clamp(mixed[2])
	case "srgb-linear":
		out.R, out.G, out.B = srgbGamma(mixed[0]), srgbGamma(mixed[1]), srgbGamma(mixed[2])
	default:
		l, a, b := mixed[0], mixed[1], mixed[2]
		if space == "oklch" {
			a, b = mixed[1]*math.Cos(mixed[2]), mixed[1]*math.Sin(mixed[2])
		}
		r, g, bl := oklabToLinearSRGB(l, a, b)
		out.R, out.G, out.B = srgbGamma(r), srgbGamma(g), srgbGamma(bl)
	}
	return Color{Type: ColorRGBA, RGBA: out}
}
