package gopdfgo

import (
	"context"
	"os"
	"testing"
)

// Testy właściwości tekstu z CSS Text 3 i 4: text-justify, text-indent (each-line, hanging),
// word-spacing w procentach, text-wrap-mode. Każdy przypadek porównuje stronę z wzorcem
// ułożonym bez badanej właściwości.

// ahemComparer zwraca funkcję, która renderuje test i wzorzec fontem Ahem i zgłasza różnicę pikseli.
func ahemComparer(t *testing.T) func(name, test, reference string) {
	t.Helper()
	ahem, err := os.ReadFile("testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 } div { font: 20px/20px Ahem; color: green }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(head+body), Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	return func(name, test, reference string) {
		t.Helper()
		if n := differentPixels(page(name, test), page(name+"-ref", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
}

func TestTextJustifyNone(t *testing.T) {
	same := ahemComparer(t)
	// cały akapit bez miejsc justowania: wiersze zostają przy lewej krawędzi
	same("none", `<div style="width:230px;text-align:justify;text-justify:none">XXX XXX XXX XXX XXX</div>`,
		`<div>XXX XXX XXX<br>XXX XXX</div>`)
	// właściwość działa na pudełku liniowym: spacja w <span> się nie rozciąga, pozostałe biorą całą resztę
	same("inline", `<div style="width:220px;text-align:justify">X <span style="text-justify:none">X X</span> X XXXXXXXXXXX</div>`,
		`<div style="white-space:pre">X   X X   X<br>XXXXXXXXXXX</div>`)
	// inter-word justuje jak auto
	same("inter-word", `<div style="width:220px;text-align:justify;text-justify:inter-word">X X X XXXXXXXXXXX</div>`,
		`<div style="white-space:pre">X    X    X<br>XXXXXXXXXXX</div>`)
}
