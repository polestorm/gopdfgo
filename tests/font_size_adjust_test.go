package tests

import (
	"context"
	"github.com/polestorm/gopdfgo"
	"os"
	"testing"
)

// font-size-adjust (CSS Fonts 5 § 2.7): rozmiar używany tekstu to rozmiar obliczony razy podana liczba
// podzielona przez stosunek metryki fontu do firetu. Ahem ma wysokość x i wysokość wersalików 0,8 em,
// a szerokość zera 1 em. Jednostki ex i ch idą za rozmiarem używanym, em zostaje przy obliczonym.
func TestFontSizeAdjust(t *testing.T) {
	ahem, err := os.ReadFile("../testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]gopdfgo.Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 } div { font: 20px/20px Ahem; color: green }
		.big { font-size: 40px } .box { background: green }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := gopdfgo.Render(context.Background(), []byte(head+body), gopdfgo.Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	same := func(name, test, reference string) {
		t.Helper()
		if n := differentPixels(page(name, test), page(name+"-ref", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	plain := `<div>XX X</div>`
	// sama liczba to metryka ex-height: 40px * 0,4 / 0,8 = 20px
	same("number", `<div class="big" style="font-size-adjust:0.4">XX X</div>`, plain)
	same("ex-height", `<div class="big" style="font-size-adjust:ex-height 0.4">XX X</div>`, plain)
	same("cap-height", `<div style="font-size:10px;font-size-adjust:cap-height 1.6">XX X</div>`, plain)
	same("ch-width", `<div class="big" style="font-size-adjust:ch-width 0.5">XX X</div>`, plain)
	// Ahem nie ma znaku U+6C34, więc miarą ic jest 1em
	same("ic-width", `<div class="big" style="font-size-adjust:ic-width 0.5">XX X</div>`, plain)
	same("calc", `<div class="big" style="font-size-adjust:calc(0.8 / 2)">XX X</div>`, plain)
	// from-font i none nie zmieniają rozmiaru fontu podstawowego; błędna wartość jest odrzucana
	same("from-font", `<div style="font-size-adjust:from-font">XX X</div>`, plain)
	same("none", `<div style="font-size-adjust:0.4"><div style="font-size-adjust:none">XX X</div></div>`, plain)
	same("invalid", `<div style="font-size-adjust:ex-height none">XX X</div>`, plain)
	same("negative", `<div style="font-size-adjust:-1">XX X</div>`, plain)
	// właściwość jest dziedziczona
	same("inherited", `<div class="big" style="font-size-adjust:0.4"><span>XX X</span></div>`, plain)
	// zero: tekst niewidoczny, wiersz zachowuje wysokość z line-height
	marker := `<div class="box" style="width:20px;height:20px"></div>`
	same("zero", `<div style="font-size-adjust:0">XX X</div>`+marker, `<div style="color:transparent">XX X</div>`+marker)
	// ex i ch skalują się z rozmiarem używanym, em nie
	same("ex-ch", `<div class="box" style="font-size-adjust:0.4;width:4ch;height:5ex"></div>`,
		`<div class="box" style="width:40px;height:40px"></div>`)
	same("em", `<div class="box" style="font-size-adjust:0.4;width:2em;height:2em"></div>`,
		`<div class="box" style="width:40px;height:40px"></div>`)
	// kontrola: bez właściwości rozmiary się różnią
	if differentPixels(page("control", `<div class="big">XX X</div>`), page("control-ref", plain)) == 0 {
		t.Error("wzorzec nie odróżnia rozmiaru 40px od 20px")
	}
}
