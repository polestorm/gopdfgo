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

func TestTextIndentHangingEachLine(t *testing.T) {
	same := ahemComparer(t)
	indented := `<span style="padding-left:40px">XX</span>`
	// samo wcięcie: tylko pierwszy wiersz, nie wiersz po <br>
	same("plain", `<div style="text-indent:40px">XX<br>XX</div>`, `<div>`+indented+`<br>XX</div>`)
	// each-line: wiersz po wymuszonym złamaniu tak, po miękkim nie
	same("each-line", `<div style="width:100px;text-indent:40px each-line">XX XX<br>XX</div>`,
		`<div>`+indented+`<br>XX<br>`+indented+`</div>`)
	// hanging: wszystkie wiersze poza pierwszym; kolejność części wartości jest dowolna
	same("hanging", `<div style="width:100px;text-indent:hanging 40px">XXXX XX<br>XX</div>`,
		`<div>XXXX<br>`+indented+`<br>`+indented+`</div>`)
	// oba słowa: wcięte są tylko wiersze po miękkim złamaniu
	same("each-line-hanging", `<div style="width:100px;text-indent:40px each-line hanging">XXXX XX<br>XX</div>`,
		`<div>XXXX<br>`+indented+`<br>XX</div>`)
	// tekst po pudełku blokowym nie jest pierwszym sformatowanym wierszem kontenera...
	block := `<div style="height:10px;background:blue"></div>`
	same("after-block", `<div style="text-indent:40px">XX`+block+`XX</div>`, `<div>`+indented+block+`XX</div>`)
	// ...ale z each-line pudełko blokowe liczy się jak wymuszone złamanie
	same("each-line-after-block", `<div style="text-indent:40px each-line">XX`+block+`XX</div>`,
		`<div>`+indented+block+indented+`</div>`)
	// hanging powiększa szerokość max-content wierszy po wymuszonym złamaniu
	same("hanging-max-content", `<div style="float:left;background:blue;text-indent:40px hanging">XX<br>XX</div>`,
		`<div style="float:left;background:blue">XX<br>`+indented+`</div>`)
}
