package tests

import (
	"context"
	"github.com/polestorm/gopdfgo"
	"os"
	"testing"
)

// Reguła @font-feature-values i funkcje font-variant-alternates (CSS Fonts 4 § 6.8 i § 6.9):
// nazwy z reguły wybierają cechy OpenType tak samo jak odpowiadające im font-feature-settings.
// Source Sans 3 (OFL, z próbki faktury WeasyPrint) ma salt, ss01–ss06 i cv01–cv05.
func TestFontFeatureValues(t *testing.T) {
	data, err := os.ReadFile("../testdata/fonts/features/SourceSans3-Regular.ttf")
	if err != nil {
		t.Skip("brak fontu Source Sans 3")
	}
	fonts := append([]gopdfgo.Font{{Name: "SourceSans3-Regular.ttf", Data: data}}, testFonts(t)...)
	const text = "abcdefghijklmnopqrstuvwxyz ABCDEFGHIJKLMNOPQRSTUVWXYZ 0123456789 &?!@#"
	head := `<style>@page { size: 900px 60px; margin: 0 } body { margin: 0 }
		div { font: 20px/30px "Source Sans 3"; color: black }</style>`
	dir := t.TempDir()
	page := func(name, css, style string) []byte {
		t.Helper()
		html := head + "<style>" + css + "</style><div style=\"" + style + "\">" + text + "</div>"
		result, err := gopdfgo.Render(context.Background(), []byte(html), gopdfgo.Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	plain := page("plain", "", "")
	same := func(name, css, style, reference string) {
		t.Helper()
		if n := differentPixels(page(name, css, style), page(name+"-ref", "", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	// wzorce z font-feature-settings muszą różnić się od tekstu bez cech, inaczej test niczego nie sprawdza
	for _, reference := range []string{"font-feature-settings: 'salt' 1", "font-feature-settings: 'ss02', 'ss03', 'ss05'",
		"font-feature-settings: 'cv04'", "font-feature-settings: 'ss01'"} {
		if differentPixels(plain, page("check", "", reference)) == 0 {
			t.Fatalf("%s nie zmienia tekstu", reference)
		}
	}
	rules := `@font-feature-values "Source Sans 3" {
		@stylistic { alt: 1; }
		@styleset { two: 2; many: 3 5; one: 1; }
		@character-variant { four: 4; off: 4 0; }
	}`
	same("stylistic", rules, "font-variant-alternates: stylistic(alt)", "font-feature-settings: 'salt' 1")
	same("styleset", rules, "font-variant-alternates: styleset(two, many)", "font-feature-settings: 'ss02', 'ss03', 'ss05'")
	same("character-variant", rules, "font-variant-alternates: character-variant(four)", "font-feature-settings: 'cv04'")
	same("character-variant-value", rules, "font-variant-alternates: character-variant(off)", "")
	// nieznana nazwa jest pomijana, nie unieważnia wartości
	same("unknown-name", rules, "font-variant-alternates: stylistic(alt) styleset(nope)", "font-feature-settings: 'salt' 1")
	// nazwy rozróżniają wielkość liter, rodziny nie
	same("name-case", rules, "font-variant-alternates: stylistic(ALT)", "")
	same("family-case", `@font-feature-values "SOURCE sans 3" { @styleset { one: 1 } }`,
		"font-variant-alternates: styleset(one)", "font-feature-settings: 'ss01'")
	// skrót font-variant przyjmuje funkcje
	same("shorthand", rules, "font-variant: stylistic(alt)", "font-feature-settings: 'salt' 1")
	// font-feature-settings ma pierwszeństwo przed font-variant-alternates
	same("feature-settings-wins", rules, "font-variant-alternates: stylistic(alt); font-feature-settings: 'salt' 0", "")
	// późniejsza reguła zastępuje nazwę, pozostałe zostają
	same("later-rule", `@font-feature-values "Source Sans 3" { @styleset { a: 2; b: 3 } }
		@font-feature-values "Source Sans 3" { @styleset { a: 1 } }`,
		"font-variant-alternates: styleset(a, b)", "font-feature-settings: 'ss01', 'ss03'")
	// przy powtórzonej nazwie wygrywa późniejsza warstwa kaskady, nie późniejsza reguła
	same("layers", `@layer one, two;
		@layer two { @font-feature-values "Source Sans 3" { @styleset { a: 1 } } }
		@layer one { @font-feature-values "Source Sans 3" { @styleset { a: 2 } } }`,
		"font-variant-alternates: styleset(a)", "font-feature-settings: 'ss01'")
	// reguła innej rodziny nie działa
	same("other-family", `@font-feature-values Other { @stylistic { alt: 1 } }`, "font-variant-alternates: stylistic(alt)", "")
	// błędna wartość (dwie funkcje stylistic) jest pomijana w całości
	same("invalid-value", rules, "font-variant-alternates: stylistic(alt) stylistic(alt)", "")
}
