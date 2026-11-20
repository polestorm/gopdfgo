package tests

import (
	"context"
	"testing"

	"github.com/polestorm/gopdfgo"
)

// TestSVGTransformOrigin sprawdza transform-origin i transform-box elementów wewnątrz SVG (CSS
// Transforms 1 § 6–7): wartości początkowe view-box i 0 0, ramkę obiektu, procenty względem okna
// widoku (także zagnieżdżonego <svg>), atrybut prezentacyjny, arkusz strony dla SVG osadzonego w
// HTML, właściwość rotate i funkcje przekształceń CSS z jednostkami kąta. Każdy przypadek porównuje
// obraz z kształtem obróconym zawczasu.
func TestSVGTransformOrigin(t *testing.T) {
	head := `<style>@page { size: 200px 100px; margin: 0 } body { margin: 0 } svg { display: block }`
	dir := t.TempDir()
	fonts := testFonts(t)
	page := func(name, style, body string) []byte {
		t.Helper()
		result, err := gopdfgo.Render(context.Background(), []byte(head+style+"</style>"+body),
			gopdfgo.Options{Fonts: fonts, Resolver: gopdfgo.DenyAll})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	// limit: piksele różniące się wygładzeniem krawędzi (obrót daje ułamkowe współrzędne)
	same := func(name, style, test, reference string) {
		t.Helper()
		if n := differentPixels(page(name, style, test), page(name+"-ref", "", reference)); n > 8 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	svg := func(content string) string {
		return `<svg width="200" height="100">` + content + `</svg>`
	}

	// fill-box + center: obrót o 90° wokół środka prostokąta zostawia go w miejscu
	same("fill-box-center", "",
		svg(`<rect x="10" y="30" width="80" height="40" fill="green"
			style="transform-box: fill-box; transform-origin: center; transform: rotate(90deg)"/>`),
		svg(`<rect x="30" y="10" width="40" height="80" fill="green"/>`))
	// wartości początkowe: view-box i 0 0, czyli obrót wokół początku układu jak dotąd
	same("default-origin", "",
		svg(`<rect x="10" y="-60" width="40" height="20" fill="green" transform="rotate(90)"/>`),
		svg(`<rect x="40" y="10" width="20" height="40" fill="green"/>`))
	// procenty względem okna widoku (view-box): środek 200x100 to (100, 50)
	same("view-box-percent", "",
		svg(`<rect width="40" height="20" fill="green" transform="rotate(180)" style="transform-origin: 50% 50%"/>`),
		svg(`<rect x="160" y="80" width="40" height="20" fill="green"/>`))
	// view-box z viewBox: pole zaczyna się w początku układu (nie w -50 0), a ma rozmiar viewBox
	// (100x50), więc punkt 0 50% to (0, 25)
	same("view-box-viewbox", "",
		`<svg width="200" height="100" viewBox="-50 0 100 50"><rect x="-50" width="10" height="10" fill="green"
			style="transform-origin: 0 50%; transform: rotate(180deg)"/></svg>`,
		`<svg width="200" height="100" viewBox="-50 0 100 50"><rect x="40" y="40" width="10" height="10" fill="green"/></svg>`)
	// atrybut prezentacyjny transform-origin (SVG 2 § 8.5) przy atrybucie transform
	same("presentation-attribute", "",
		svg(`<rect x="20" y="40" width="60" height="20" fill="green" transform="rotate(90)" transform-origin="50 50"/>`),
		svg(`<rect x="40" y="20" width="20" height="60" fill="green"/>`))
	// arkusz strony dla SVG osadzonego w HTML: ikona obrócona wokół środka swojej ramki
	same("page-css-icon", `.icon path { transform-box: fill-box; transform-origin: center; transform: rotate(180deg) }`,
		`<svg class="icon" width="200" height="100"><path d="M10,10L50,10L10,30Z" fill="green"/></svg>`,
		svg(`<path d="M50,30L10,30L50,10Z" fill="green"/>`))
	// właściwość rotate (CSS Transforms 2 § 5.1) z transform-origin
	same("rotate-property", "",
		svg(`<rect x="20" y="40" width="60" height="20" fill="green" style="rotate: 90deg; transform-origin: 50px 50px"/>`),
		svg(`<rect x="40" y="20" width="20" height="60" fill="green"/>`))
	// translate, rotate, scale przed transform, wszystkie wokół tego samego punktu; kąt w turn
	same("individual-and-transform", "",
		svg(`<rect x="40" y="45" width="20" height="10" fill="green"
			style="transform-box: fill-box; transform-origin: center; translate: 50px; rotate: 0.25turn; transform: scaleX(2)"/>`),
		svg(`<rect x="95" y="30" width="10" height="40" fill="green"/>`))
	// stroke-box: ramka z obrysem; lewy górny róg ramki obrysu (55, 5) jest punktem obrotu
	same("stroke-box", "",
		svg(`<rect x="60" y="10" width="40" height="20" fill="green" stroke="navy" stroke-width="10"
			style="transform-box: stroke-box; transform-origin: 0 0; transform: rotate(90deg)"/>`),
		svg(`<rect x="30" y="10" width="20" height="40" fill="green" stroke="navy" stroke-width="10"/>`))
	// zagnieżdżony <svg> to nowe okno widoku: procenty potomków i view-box liczą się względem niego
	same("nested-viewport", "",
		svg(`<svg x="100" width="100" height="100"><rect width="50%" height="50%" fill="green"
			style="transform-origin: 50% 50%; transform: rotate(180deg)"/></svg>`),
		svg(`<rect x="150" y="50" width="50" height="50" fill="green"/>`))
	// transform: none i translate z procentami pola odniesienia (szerokość ramki)
	same("none-and-percent-translate", "",
		svg(`<rect width="40" height="20" fill="green" style="transform: none"/>
			<rect x="10" y="50" width="40" height="20" fill="green" style="transform-box: fill-box; transform: translate(100%, 50%)"/>`),
		svg(`<rect width="40" height="20" fill="green"/><rect x="50" y="60" width="40" height="20" fill="green"/>`))
}
