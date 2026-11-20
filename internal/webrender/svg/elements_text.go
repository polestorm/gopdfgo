package svg

import (
	"strings"
	"unicode"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	pa "github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/css/validation"
)

// text tags
type textSpan struct {
	style  pr.Properties
	text   string
	rotate []Fl // angles in degrees

	x, y, dx, dy []Value

	// gopdfgo: odstępy liter i słów (CSS Text 3 § 8.1, 8.2) trafiają do stylu kształtowania;
	// hasXxxSpacing jest fałszywe dla wartości normal
	letterSpacing, wordSpacing       Value
	hasLetterSpacing, hasWordSpacing bool

	textLength   Value
	lengthAdjust bool // true for spacingAndGlyphs

	textAnchor, displayAnchor anchor

	baseline baseline

	isText          bool // only true for tag 'text'
	textBoundingBox Rectangle

	// gopdfgo: textPath (SVG 2 § 11.10.3): ścieżka wskazana przez href albo podana atrybutem path
	// oraz przesunięcie początku tekstu wzdłuż niej
	isTextPath  bool
	pathHref    string
	pathData    []pathItem
	startOffset Value

	// gopdfgo: wynik składu całego elementu <text> (layoutText): ciągi glifów tego fragmentu
	// w układzie użytkownika, rysowane przez draw
	drawings []backend.TextDrawing
}

// shapingProperties (gopdfgo) to właściwości fontu przekazywane z kaskady SVG do kształtowania
// tekstu tak jak w HTML (CSS Fonts 4 § 6, 7): kerning, ligatury, warianty i ustawienia OpenType.
var shapingProperties = []string{
	"font-stretch", "font-variant", "font-variant-ligatures", "font-variant-caps",
	"font-variant-numeric", "font-variant-east-asian", "font-variant-position",
	"font-variant-alternates", "font-kerning", "font-feature-settings",
	"font-variation-settings", "font-language-override", "font-synthesis",
	"font-size-adjust",
}

func newTextSpan(node *cascadedNode) (drawable, error) {
	var out textSpan

	out.text = applyTextTransform(string(node.text), node.attrs["text-transform"])
	out.style = pr.InitialValues.Copy()

	family := "sans-serif"
	if f, has := node.attrs["font-family"]; has {
		family = f
	}
	out.style.SetFontFamily(strings.Split(family, ","))

	if w, has := node.attrs["font-weight"]; has {
		out.style.SetFontWeight(pr.IntString{Int: parseFontWeight(w)})
	}

	if s, has := node.attrs["font-style"]; has {
		out.style.SetFontStyle(pr.String(s))
	}
	applyShapingProperties(out.style, node.attrs)

	// get rotations and translations
	var err error
	out.x, err = parseValues(node.attrs["x"])
	if err != nil {
		return nil, err
	}
	out.y, err = parseValues(node.attrs["y"])
	if err != nil {
		return nil, err
	}

	out.dx, err = parseValues(node.attrs["dx"])
	if err != nil {
		return nil, err
	}
	out.dy, err = parseValues(node.attrs["dy"])
	if err != nil {
		return nil, err
	}

	out.rotate, err = parsePoints(node.attrs["rotate"], nil, false)
	if err != nil {
		return nil, err
	}

	// gopdfgo: wartość normal (i każda niepoprawna) to brak odstępu; źródło zwracało dla niej błąd,
	// który usuwał cały obraz SVG
	if v := strings.TrimSpace(node.attrs["letter-spacing"]); v != "" && v != "normal" {
		if ls, err := parseValue(v); err == nil {
			out.letterSpacing, out.hasLetterSpacing = ls, true
		}
	}
	if v := strings.TrimSpace(node.attrs["word-spacing"]); v != "" && v != "normal" {
		if ws, err := parseValue(v); err == nil {
			out.wordSpacing, out.hasWordSpacing = ws, true
		}
	}
	out.textLength, err = parseValue(node.attrs["textLength"])
	if err != nil {
		return nil, err
	}

	out.textAnchor = parseAnchor(node.attrs["text-anchor"])
	out.displayAnchor = parseAnchor(node.attrs["display-anchor"])

	baseline, has := node.attrs["dominant-baseline"]
	if !has {
		baseline = node.attrs["alignment-baseline"]
	}
	out.baseline = parseBaseline(baseline)

	out.lengthAdjust = node.attrs["lengthAdjust"] == "spacingAndGlyphs"

	out.isText = node.tag == "text"
	out.textBoundingBox = emptyBbox

	if node.tag == "textPath" {
		out.isTextPath = true
		out.pathHref = parseURLFragment(node.attrs["href"])
		// gopdfgo: atrybut path ma pierwszeństwo przed href (SVG 2 § 11.10.3)
		if d := strings.TrimSpace(node.attrs["path"]); d != "" {
			var parser pathParser
			if path, err := parser.parsePath(d); err == nil {
				out.pathData = path
			}
		}
		out.startOffset, _ = parseValue(node.attrs["startOffset"])
	}

	return &out, nil
}

// applyShapingProperties (gopdfgo) przenosi do stylu tekstu właściwości fontu z kaskady SVG,
// sprawdzone tym samym walidatorem co w HTML; niepoprawne wartości są pomijane.
func applyShapingProperties(style pr.Properties, attrs nodeAttributes) {
	var css strings.Builder
	for _, name := range shapingProperties {
		if v := strings.TrimSpace(attrs[name]); v != "" && v != "inherit" {
			css.WriteString(name + ":" + v + ";")
		}
	}
	if css.Len() == 0 {
		return
	}
	declarations := validation.PreprocessDeclarations("", pa.ParseDeclarationListString(css.String(), true, true))
	for _, decl := range declarations {
		if v, ok := decl.Value.(pr.CssProperty); ok && decl.Name.Var == "" {
			style[decl.Name.KnownProp] = v
		}
	}
}

// draw rysuje ciągi glifów przygotowane dla tego fragmentu przez layoutText.
func (t *textSpan) draw(dst backend.Canvas, attrs *attributes, svg *SVGImage, dims drawingDims) []vertex {
	// gopdfgo: źródło składało i rysowało tu każdy znak osobno, bez kształtowania całego tekstu
	// (bez kerningu i ligatur, z osobnym operatorem tekstu dla każdego znaku); skład całego elementu
	// <text> robi teraz layoutText (text_layout.go), a tu zostaje tylko rysowanie
	if len(t.drawings) == 0 {
		return nil
	}
	doFill, doStroke := svg.applyPainters(dst, &svgNode{graphicContent: t, attributes: *attrs}, dims)
	// gopdfgo: bez wypełnienia i obrysu tekst tylko zajmuje miejsce (zob. drawNode, visibility)
	if !doFill && !doStroke {
		return nil
	}
	dst.State().SetTextPaint(newPaintOp(doFill, doStroke, false))
	texts := t.drawings
	dst.OnNewStack(func() {
		dst.DrawText(texts)
	})
	return nil
}

// isClusterContinuation (gopdfgo) mówi, czy znak dołącza do poprzedniego znaku adresowalnego.
func isClusterContinuation(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc) || r == 0x200C || r == 0x200D ||
		unicode.Is(unicode.Variation_Selector, r)
}

// applyTextTransform (gopdfgo) stosuje text-transform (CSS Text 3 § 2.1) do treści tekstu SVG;
// źródło tę właściwość pomijało. capitalize zmienia pierwszą literę każdego słowa (słowo kończy
// znak niebędący literą, cyfrą ani znakiem łączącym), w granicach jednego węzła tekstu.
func applyTextTransform(s, transform string) string {
	switch strings.TrimSpace(transform) {
	case "uppercase":
		return strings.ToUpper(s)
	case "lowercase":
		return strings.ToLower(s)
	case "capitalize":
		var (
			b      strings.Builder
			inWord bool
		)
		for _, r := range s {
			isWordChar := unicode.IsLetter(r) || unicode.IsDigit(r) || isClusterContinuation(r)
			if isWordChar && !inWord && unicode.IsLetter(r) {
				r = unicode.ToTitle(r)
			}
			inWord = isWordChar
			b.WriteRune(r)
		}
		return b.String()
	}
	return s
}
