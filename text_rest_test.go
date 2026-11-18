package gopdfgo

import (
	"context"
	"os"
	"testing"
)

// TestTextRest sprawdza poprawki z rundy pozostałych testów CSS Text: text-justify: inter-character,
// pudełka pozycjonowane przy miejscu podziału wiersza, -webkit-line-clamp i line-clamp z blokami
// zagnieżdżonymi. Font Ahem: każdy znak to kwadrat o boku równym rozmiarowi pisma.
func TestTextRest(t *testing.T) {
	ahem, err := os.ReadFile("testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0; font: 20px/1 Ahem; color: green }
		.a { position: absolute } .g { background: green }</style>`
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
	same := func(name, test, reference string) {
		t.Helper()
		if n := differentPixels(page(name, test), page(name+"-ref", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}

	// inter-character: 100 px - 4 znaki = 20 px na trzy odstępy między znakami
	same("inter-character", `<div style="width:140px;text-align-last:justify;text-justify:inter-character">XXXX</div>`,
		`<div class="g a" style="width:20px;height:20px"></div><div class="g a" style="left:40px;width:20px;height:20px"></div>
		<div class="g a" style="left:80px;width:20px;height:20px"></div><div class="g a" style="left:120px;width:20px;height:20px"></div>`)
	// ciąg elementów atomowych jest jedną jednostką: dwa odstępy po 20 px
	same("inter-character-atomic", `<div style="width:120px;text-align-last:justify;text-justify:inter-character">X<span
		style="display:inline-block;width:20px;height:20px;background:green;vertical-align:top"></span><span
		style="display:inline-block;width:20px;height:20px;background:green;vertical-align:top"></span>X</div>`,
		`<div class="g a" style="width:20px;height:20px"></div><div class="g a" style="left:40px;width:40px;height:20px"></div>
		<div class="g a" style="left:100px;width:20px;height:20px"></div>`)
	// pudełko pozycjonowane tuż przed miejscem podziału przechodzi z następnym słowem do drugiego wiersza
	same("abspos-at-break", `<div style="width:60px">XX <span class=a style="width:10px;height:10px;background:blue"></span>XXX</div>`,
		`<div>XX<br>XXX</div><div class=a style="top:20px;width:10px;height:10px;background:blue"></div>`)
	// -webkit-line-clamp w pudełku -webkit-box ułożonym pionowo obcina do dwóch wierszy
	same("webkit-line-clamp", `<div style="width:60px;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;
		block-ellipsis:none">XX XX XX XX</div><div class=g style="width:10px;height:10px"></div>`,
		`<div>XX<br>XX</div><div class=g style="width:10px;height:10px"></div>`)
	// bez -webkit-box-orient: vertical -webkit-line-clamp nie działa
	same("webkit-line-clamp-horizontal", `<div style="width:60px;display:-webkit-box;-webkit-line-clamp:2">XX XX XX</div>`,
		`<div>XX<br>XX<br>XX</div>`)
	// wiersze bloku zagnieżdżonego liczą się do line-clamp kontenera
	same("line-clamp-nested", `<div style="width:60px;line-clamp:2 '';overflow:hidden">XX<div>XX XX</div>XX</div>`,
		`<div>XX<br>XX</div>`)
}
