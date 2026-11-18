package gopdfgo

import (
	"context"
	"os"
	"testing"
)

// TestFlexMisc sprawdza poprawki układu flex znalezione przy testach WPT css-flexbox: visibility: collapse
// (CSS Flexbox 1 § 4.4), linia bazowa w rozmiarze linii (§ 9.4 krok 8), tabele jako elementy flex
// (CSS Tables 3 § 3.9.1), kontenery wielowierszowe z jedną linią i „safe” (CSS Align 3 § 4.4). Font Ahem:
// przy font: 20px/1 linia bazowa leży 16 px pod górą wiersza tekstu.
func TestFlexMisc(t *testing.T) {
	ahem, err := os.ReadFile("testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	head := `<style>@page { size: 300px 300px; margin: 0 } body { margin: 0; font: 20px/1 Ahem; color: transparent }
		.g { background: green } .b { background: blue } .a { position: absolute }</style>`
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

	// zwinięty element: rozpórka o wysokości swojej linii i zerowej szerokości; odstęp przy niej znika,
	// kontener float ma szerokość bez niego
	same("collapse-strut", `<div style="display:flex;float:left;gap:10px;background:blue">
		<div style="width:20px;height:40px;visibility:collapse"></div><div class=g style="width:20px;height:10px"></div>
		<div class=g style="width:20px;height:10px"></div></div>`,
		`<div class=b style="width:50px;height:40px"><div class="g a" style="width:20px;height:10px"></div>
		<div class="g a" style="left:30px;width:20px;height:10px"></div></div>`)
	// align-items: baseline w kontenerze (nie align-self) też poszerza linię o różnicę linii bazowych
	same("align-items-baseline-line", `<div style="display:flex;align-items:baseline;background:blue">
		<div class=g style="padding-top:10px">X</div><div class=g style="font-size:40px">X</div></div>`,
		`<div class=b style="height:40px"><div class="g a" style="top:6px;width:20px;height:30px"></div>
		<div class="g a" style="left:20px;width:40px;height:40px"></div></div>`)
	// linia bazowa poza jedynym elementem nie powiększa linii
	same("baseline-outside-item", `<div style="display:flex;align-items:baseline;background:blue;width:100px">
		<div class=g style="height:50px;min-width:100px"><div style="margin-top:100px">X</div></div></div>`,
		`<div class=g style="width:100px;height:50px"></div>`)
	// last baseline: grupa stoi przy końcu linii
	same("last-baseline", `<div style="display:flex;align-items:last baseline;height:100px;background:blue">
		<div class=g>X</div><div class=g>X<br>X</div></div>`,
		`<div class=b style="height:100px"><div class="g a" style="top:80px;width:20px;height:20px"></div>
		<div class="g a" style="top:60px;left:20px;width:20px;height:40px"></div></div>`)
	// tabela z obramowaniem w kolumnie: flex-basis to wysokość treści tabeli, szerokość się nie powiększa
	same("table-in-column", `<div style="display:flex;flex-direction:column">
		<div class=g style="display:table;border:10px solid green;width:80px;flex:0 0 80px"></div></div>`,
		`<div class=g style="width:100px;height:100px"></div>`)
	// min-height tabeli nie wchodzi do rozmiaru bazowego
	same("table-min-height", `<div style="display:flex;flex-direction:column;height:200px">
		<div class=g style="display:table;min-height:80px;flex:1 0 auto;width:100px"><div style="display:table-cell">
		<div style="height:1px"></div></div></div><div style="flex:1 0 1px"></div></div>`,
		`<div class=g style="width:100px;height:100px"></div>`)
	// min-width tabeli w procentach ogranicza element flex
	same("table-min-width", `<div style="display:flex;width:200px"><div class=g style="display:table;min-width:50%;flex-shrink:0;height:20px"></div></div>`,
		`<div class=g style="width:100px;height:20px"></div>`)
	// kontener z zawijaniem, który ma jedną linię, jest wielowierszowy: align-content: flex-start
	same("wrap-single-line", `<div style="display:flex;flex-direction:column;flex-wrap:wrap;width:200px;height:50px;align-content:flex-start">
		<div class=g style="height:50px;width:40px;align-self:center"></div></div>`,
		`<div class=g style="width:40px;height:50px"></div>`)
	// przepełnione linie nie są zmniejszane przez align-content: stretch
	same("wrap-overflow-stretch", `<div style="display:flex;flex-wrap:wrap;width:50px;height:40px;overflow:hidden">
		<div class=g style="width:50px;height:20px"></div><div class=b style="width:50px;height:100px"></div></div>`,
		`<div class=g style="width:50px;height:20px"></div><div class=b style="width:50px;height:20px"></div>`)
	// „safe center”: element wyższy niż linia stoi na jej początku
	same("safe-center", `<div style="display:flex;align-items:safe center;height:20px"><div class=g style="width:20px;height:60px"></div></div>`,
		`<div class=g style="width:20px;height:60px"></div>`)
	// procent wysokości w elemencie nierozciąganym (margines auto) liczy się jak auto
	same("unstretched-percent", `<div style="display:flex;width:100px"><div class=g style="margin:auto">
		<div class=b style="height:100%;width:100px"></div><div style="height:50px"></div></div></div>`,
		`<div class=g style="width:100px;height:50px"></div>`)
	// kolumna z zawijaniem łamana przez max-height ma wysokość najdłuższej linii
	same("column-wrap-height", `<div style="display:flex;flex-flow:column wrap;max-height:50px;width:100px;background:blue;align-content:flex-start">
		<div class=g style="width:50px;height:20px"></div><div class=g style="width:50px;height:40px"></div></div><div class=g style="width:10px;height:10px"></div>`,
		`<div class=b style="width:100px;height:40px"><div class="g a" style="width:50px;height:20px"></div>
		<div class="g a" style="left:50px;width:50px;height:40px"></div></div><div class=g style="width:10px;height:10px"></div>`)
	// kontener pozycjonowany z top i bottom: obraz rozciągnięty do wysokości ma szerokość z proporcji
	same("abspos-inset-ratio", `<div style="position:relative;height:100px"><div style="display:flex;position:absolute;top:0;bottom:0">
		<img src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='1' height='1' viewBox='0 0 1 1'%3E%3Crect width='1' height='1' fill='green'/%3E%3C/svg%3E" style="display:block"></div></div>`,
		`<div class=g style="width:100px;height:100px"></div>`)
	// kontener z aspect-ratio ma określoną wysokość, do której rozciągają się elementy
	same("container-aspect-ratio", `<div style="display:flex;width:200px;aspect-ratio:2/1"><div class=g style="width:50px"></div></div>`,
		`<div class=g style="width:50px;height:100px"></div>`)
	// element kolumny wyrównany do początku mierzy wysokość przy szerokości fit-content (procentowy padding)
	same("column-fit-content-basis", `<div style="display:flex;flex-direction:column"><div style="align-self:start">
		<div class=g style="width:100px;height:50px"></div><div class=g style="padding-bottom:50%"></div></div></div>
		<div class=b style="width:10px;height:10px"></div>`,
		`<div class=g style="width:100px;height:100px"></div><div class=b style="width:10px;height:10px"></div>`)
	// pusty blok z aspect-ratio i min-height: szerokość wewnętrzna z proporcji
	same("transferred-min-height", `<div style="display:flex;flex-direction:column;float:left">
		<div class=g style="min-height:60px;aspect-ratio:1/1;padding:0 10px;box-sizing:border-box"></div></div>`,
		`<div class=g style="width:60px;height:60px"></div>`)
}
