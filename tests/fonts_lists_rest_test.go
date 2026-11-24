package tests

import (
	"context"
	"github.com/polestorm/gopdfgo/htmlpdf"
	"os"
	"testing"
)

// Testy poprawek z końcówki CSS Fonts, CSS Lists i CSS Content: dopasowanie nazw rodzin (CSS Fonts 4
// § 5.1), syntetyczne pochylenie i pogrubienie (§ 7.2, 7.3), znacznik elementu listy liniowego
// (CSS Lists 3 § 3.3), obraz znacznika (§ 3.2) i align-content w kontenerze blokowym (CSS Box
// Alignment 3 § 5.4). Każdy przypadek porównuje stronę z wzorcem ułożonym bez badanej funkcji.

// csstestFonts wczytuje fonty testowe CSSTest z WPT (ADR 0018) i Ahem.
func csstestFonts(t *testing.T) []htmlpdf.Font {
	t.Helper()
	var fonts []htmlpdf.Font
	for _, name := range []string{"csstest/csstest-familyname.ttf", "csstest/csstest-fallback.ttf", "csstest/csstest-verify.ttf"} {
		data, err := os.ReadFile("../testdata/fonts/" + name)
		if err != nil {
			t.Skip("brak fontów CSSTest")
		}
		fonts = append(fonts, htmlpdf.Font{Name: name, Data: data})
	}
	ahem, err := os.ReadFile("../testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	return append(fonts, htmlpdf.Font{Name: "Ahem.ttf", Data: ahem})
}

// fontsPage zwraca funkcję renderującą stronę 300 × 200 px podanymi fontami.
func fontsPage(t *testing.T, fonts []htmlpdf.Font) func(name, body string) []byte {
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0; font: 36px/1 "CSSTest Fallback" }
		div.a { font: 20px/20px Ahem; color: green }</style>`
	dir := t.TempDir()
	return func(name, body string) []byte {
		t.Helper()
		result, err := htmlpdf.Render(context.Background(), []byte(head+body), htmlpdf.Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
}

func TestFontFamilyNameMatching(t *testing.T) {
	page := fontsPage(t, csstestFonts(t))
	same := func(name, test, reference string) {
		t.Helper()
		if n := differentPixels(page(name, test), page(name+"-ref", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	// spacje w nazwie w cudzysłowie są znaczące: nazwa nie pasuje i działa następna rodzina
	same("quoted-spaces", `<p style="font-family: 'CSSTest   FamilyName', 'CSSTest Fallback'">1234</p>`,
		`<p style="font-family: 'CSSTest Fallback'">1234</p>`)
	// wielkość liter ASCII bez znaczenia
	same("ascii-case", `<p style="font-family: csstest familyname">1234</p>`,
		`<p style="font-family: 'CSSTest FamilyName'">1234</p>`)
	// zlokalizowana nazwa z tablicy name pasuje do fontu
	same("localized", `<p style="font-family: ＣＳＳテスト　フォント名, 'CSSTest Fallback'">5678</p>`,
		`<p style="font-family: 'CSSTest FamilyName'">5678</p>`)
}

func TestFontSynthesis(t *testing.T) {
	page := fontsPage(t, csstestFonts(t))
	compare := func(name, test, reference string, wantSame bool) {
		t.Helper()
		n := differentPixels(page(name, test), page(name+"-ref", reference))
		if wantSame && n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		} else if !wantSame && n == 0 {
			t.Errorf("%s: render taki sam jak wzorzec, a synteza miała go zmienić", name)
		}
	}
	// krój bez wersji pochyłej i pogrubionej: synteza zmienia rysunek
	compare("italic", `<div class="a" style="font-style: italic">XX</div>`, `<div class="a">XX</div>`, false)
	compare("oblique-angle", `<div class="a" style="font-style: oblique 30deg">XX</div>`,
		`<div class="a" style="font-style: oblique">XX</div>`, false)
	compare("bold", `<div class="a" style="font-weight: bold">XX</div>`, `<div class="a">XX</div>`, false)
	// font-synthesis: none ją wyłącza, a oblique-only nie pochyla dla italic
	compare("italic-none", `<div class="a" style="font-style: italic; font-synthesis: none">XX</div>`,
		`<div class="a">XX</div>`, true)
	compare("italic-oblique-only", `<div class="a" style="font-style: italic; font-synthesis-style: oblique-only">XX</div>`,
		`<div class="a">XX</div>`, true)
	compare("bold-none", `<div class="a" style="font-weight: bold; font-synthesis-weight: none">XX</div>`,
		`<div class="a">XX</div>`, true)
	// synteza nie zmienia szerokości: tekst za pogrubionym stoi w tym samym miejscu
	compare("bold-advance", `<div class="a"><span style="font-weight: bold; color: white">XX</span>X</div>`,
		`<div class="a"><span style="color: white">XX</span>X</div>`, true)
}

func TestListMarkersAndBlockAlignment(t *testing.T) {
	same := ahemComparer(t)
	// element listy liniowy: znacznik zewnętrzny działa jak wewnętrzny
	same("inline-list-item", `<div><li style="display: inline list-item; list-style-type: square">X</li></div>`,
		`<div><li style="display: inline list-item; list-style: square inside">X</li></div>`)
	// align-content: center w bloku o podanej wysokości przesuwa treść o połowę wolnego miejsca
	same("align-content-center", `<div style="height: 100px; align-content: center">XX</div>`,
		`<div style="height: 100px; padding-top: 40px; box-sizing: border-box">XX</div>`)
	same("align-content-end", `<div style="height: 100px; align-content: end">XX</div>`,
		`<div style="height: 100px; padding-top: 80px; box-sizing: border-box">XX</div>`)
	// przy przepełnieniu treść zostaje na początku (safe)
	same("align-content-overflow", `<div style="height: 10px; align-content: center">XX</div>`,
		`<div style="height: 10px">XX</div>`)

	// obraz znacznika w url("…") z cudzysłowem; wyższy od tekstu powiększa wiersz elementu listy,
	// a obraz stoi 7 px przed treścią (wzorzec jak w teście WPT outside-marker-taller-than-content)
	image := `url('data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><rect width="40" height="40" fill="green"/></svg>')`
	list := `<style>ul { margin: 0; padding-left: 60px; font: 10px/1 Ahem } li { background: silver }</style>`
	same("marker-image-line", list+`<style>ul { list-style-image: `+image+` }</style><ul><li></li></ul><ul><li>x</li></ul>`,
		list+`<style>ul { list-style: none } li::before { content: `+image+`; margin-left: -47px; margin-right: 7px }</style><ul><li></li></ul><ul><li>x</li></ul>`)
	// gradient jako obraz znacznika ma domyślny rozmiar 1em × 1em
	same("marker-gradient", `<ul style="margin: 0; padding-left: 60px; font: 20px/1 Ahem; list-style-image: linear-gradient(green, green)"><li><div style="display: inline">X</div></li></ul>`,
		`<ul style="margin: 0; padding-left: 60px; font: 20px/1 Ahem; list-style: none"><li><span style="display: inline-block; width: 20px; height: 20px; background: green; margin-left: -27px; margin-right: 7px"></span><div style="display: inline">X</div></li></ul>`)
}

func TestFillColorAndFallbackLineHeight(t *testing.T) {
	same := ahemComparer(t)
	// -webkit-text-fill-color maluje glify, także znaczniki list
	same("text-fill-color", `<div style="color: red; -webkit-text-fill-color: green">XX</div>`,
		`<div style="color: green">XX</div>`)
	same("text-fill-color-marker", `<ul style="margin: 0; padding-left: 40px; font: 20px/20px Ahem; color: red; -webkit-text-fill-color: green"><li>X</li></ul>`,
		`<ul style="margin: 0; padding-left: 40px; font: 20px/20px Ahem; color: green"><li>X</li></ul>`)
	// przy line-height: normal znak z fontu zastępczego powiększa wiersz o metryki tego fontu,
	// tak jak element liniowy pisany tym fontem
	same("fallback-line-height", `<div style="font: 20px Ahem">X&#x436;</div><div style="font: 20px Ahem">X</div>`,
		`<div style="font: 20px Ahem">X<span style="font-family: 'Noto Sans'">&#x436;</span></div><div style="font: 20px Ahem">X</div>`)
}

func TestFontSizeXxxLarge(t *testing.T) {
	same := ahemComparer(t)
	// słowo kluczowe xxx-large to 3 × medium, czyli 48 px (CSS Fonts 4 § 2.5)
	same("xxx-large", `<div style="font-size: xxx-large; line-height: 1">X</div>`,
		`<div style="font-size: 48px; line-height: 1">X</div>`)
}
