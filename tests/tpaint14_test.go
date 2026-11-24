package tests

import (
	"context"
	"github.com/polestorm/gopdfgo/htmlpdf"
	"testing"
)

// TestTableOverflowUnderRepeatedParts sprawdza treść niepodzielną wyższą niż strona w tabeli
// z powtarzanym nagłówkiem i stopką: wycinki sięgają pod nagłówek i stopkę (jak w Chromium; testy WPT
// css-page/monolithic-overflow-016, -017), a nagłówek i stopka są malowane na nich. W tabeli niepodzielonej
// porządek malowania zostaje jak w CSS 2.1 dodatek E.2: tła wszystkich części tabeli przed blokami
// w komórkach.
func TestTableOverflowUnderRepeatedParts(t *testing.T) {
	fonts := testFonts(t)
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 } div { width: 200px }
		.g { background: green } .b { background: blue } .k { background: black }</style>`
	dir := t.TempDir()
	pages := func(name, body string) [][]byte {
		t.Helper()
		result, err := htmlpdf.Render(context.Background(), []byte(head+body), htmlpdf.Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		return rasterize(t, result.PDF, dir, name)
	}
	same := func(name, test, reference string) {
		t.Helper()
		got, want := pages(name, test), pages(name+"-ref", reference)
		if len(got) != len(want) {
			t.Errorf("%s: %d stron, wzorzec ma %d", name, len(got), len(want))
			return
		}
		for i := range got {
			if n := differentPixels(got[i], want[i]); n != 0 {
				t.Errorf("%s: strona %d: %d pikseli różni się od wzorca", name, i+1, n)
			}
		}
	}

	// 450 px czerni: strona 1 od 20 do dołu (pod stopką), strona 2 cała, strona 3 do 70 px; nagłówek
	// i stopka na wierzchu, na ostatniej stronie stopka tuż pod czernią
	same("header-footer", `<div style="display:table">
		<div class=g style="display:table-header-group;break-inside:avoid"><div style="height:20px"></div></div>
		<div class=b style="display:table-footer-group;break-inside:avoid"><div style="height:20px"></div></div>
		<div class=k style="contain:size;height:450px"></div></div>`,
		`<div class=g style="height:20px"></div><div class=k style="height:160px"></div><div class=b style="height:20px"></div>
		<div class=g style="height:20px;break-before:page"></div><div class=k style="height:160px"></div><div class=b style="height:20px"></div>
		<div class=g style="height:20px;break-before:page"></div><div class=k style="height:50px"></div><div class=b style="height:20px"></div>`)
	// reszta krótsza niż nagłówek leży cała pod nim; następny wiersz stoi tuż pod nagłówkiem
	same("remainder-behind-header", `<div style="display:table">
		<div class=g style="display:table-header-group;break-inside:avoid"><div style="height:40px"></div></div>
		<div style="display:table-row"><div class=k style="contain:size;height:180px"></div></div>
		<div style="display:table-row"><div class=b style="height:30px"></div></div></div>`,
		`<div class=g style="height:40px"></div><div class=k style="height:160px"></div>
		<div class=g style="height:40px;break-before:page"></div><div class=b style="height:30px"></div>`)
	// tabela na jednej stronie: blok wylewający się z komórki zakrywa tło stopki (CSS 2.1 dodatek E.2)
	same("unfragmented-order", `<div style="display:table">
		<div class=b style="display:table-footer-group"><div style="height:20px"></div></div>
		<div style="display:table-row"><div style="display:table-cell"><div style="height:20px"><div class=k
		style="height:40px"></div></div></div></div></div>`,
		`<div class=k style="height:40px"></div>`)
}
