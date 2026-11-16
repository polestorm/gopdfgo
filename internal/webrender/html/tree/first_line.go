package tree

import (
	"strings"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
)

// gopdfgo: obsługa `::first-line` (CSS Pseudo-Elements 4 § 2.1). W źródle styl pseudoelementu
// był wyliczany i nigdzie nie używany.

// firstLinePrefixes wymienia właściwości, które wolno ustawić na `::first-line`
// (CSS Pseudo-Elements 4 § 2.1.1): fonty, kolor, tło, odstępy liter i słów, dekoracje tekstu,
// `text-transform`, `line-height`, `vertical-align`, `text-shadow`.
var firstLinePrefixes = []string{
	"font", "color", "background", "word_spacing", "letter_spacing", "text_decoration",
	"text_transform", "line_height", "vertical_align", "text_shadow",
}

func appliesToFirstLine(key pr.PropKey) bool {
	if key.Var != "" {
		return true
	}
	name := strings.ReplaceAll(key.KnownProp.String(), "-", "_")
	for _, prefix := range firstLinePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// FirstLineStyle zwraca styl `::first-line` ograniczony do właściwości, które ten pseudoelement
// przyjmuje. Wynik służy jako rodzic dla stylów treści pierwszego wiersza. Zwraca nil, gdy
// po odfiltrowaniu nie zostaje żadna deklaracja.
func FirstLineStyle(style pr.ElementStyle) pr.ElementStyle {
	computed, ok := style.(*ComputedStyle)
	if !ok || computed == nil {
		return nil
	}
	filtered := cascadedStyle{}
	for key, value := range computed.cascaded {
		if appliesToFirstLine(key) {
			filtered[key] = value
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	out := newComputedStyle(computed.parentStyle, filtered, computed.element, computed.pseudoType,
		computed.rootStyle, computed.baseUrl, computed.textContext, computed.share)
	out.viewport = computed.viewport
	out.monospaceScale = computed.monospaceScale
	return out
}

// Reparent wylicza styl na nowo z tą samą kaskadą, ale z innym rodzicem: tak treść pierwszego
// wiersza dziedziczy po `::first-line` zamiast po bloku. Wartości ustawione na stylu już po
// kaskadzie (przez Set…) przenoszone są tylko dla stylów anonimowych, bo tylko tam da się je
// odróżnić od wartości wyliczonych.
func Reparent(style, parent pr.ElementStyle) pr.ElementStyle {
	switch style := style.(type) {
	case *ComputedStyle:
		out := newComputedStyle(parent, style.cascaded, style.element, style.pseudoType,
			style.rootStyle, style.baseUrl, style.textContext, style.share)
		out.viewport = style.viewport
		out.monospaceScale = style.monospaceScale
		return out
	case *AnonymousStyle:
		out := newAnonymousStyle(parent)
		out.propsCache.updateWith(style.propsCache)
		return out
	}
	return style
}

// firstLineParent jest rodzicem stylów treści pierwszego wiersza: właściwości dziedziczone
// i dekoracje tekstu pochodzą z `::first-line`, pozostałe (osiągalne tylko przez jawne `inherit`)
// z bloku, jak w przeglądarkach (testy W3C first-line-inherit-002 i 003).
type firstLineParent struct {
	pr.ElementStyle
	block pr.ElementStyle
}

func (f firstLineParent) Get(key pr.PropKey) pr.CssProperty {
	if key.Var != "" || pr.Inherited.Has(key.KnownProp) || key.KnownProp.IsTextDecoration() {
		return f.ElementStyle.Get(key)
	}
	return f.block.Get(key)
}

// FirstLineParent łączy styl `::first-line` ze stylem bloku w rodzica dla treści pierwszego wiersza.
func FirstLineParent(firstLine, block pr.ElementStyle) pr.ElementStyle {
	if block == nil {
		return firstLine
	}
	return firstLineParent{ElementStyle: firstLine, block: block}
}
