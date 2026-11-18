package gopdfgo

import (
	"context"
	"testing"
)

// TestFlexGridTail sprawdza poprawki z rundy „ogon” flex i grid: wkład elementu flex do szerokości
// wewnętrznej (CSS Flexbox 1 § 9.9.1), wysokości procentowe znane przy liczeniu szerokości z treści,
// rozmiar bazowy obrazu z samymi proporcjami, podpisy tabeli w kolumnie, wolne miejsce w siatce
// o wysokości auto (CSS Grid 1 § 12.6), powtórzenia auto-fill z min-height i aspect-ratio oraz skróty
// place-* z wartościami wielowyrazowymi (CSS Box Alignment 3 § 5.2, § 6).
func TestFlexGridTail(t *testing.T) {
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 } .g { background: green }</style>`
	dir := t.TempDir()
	fonts := testFonts(t)
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
	square := `<div class=g style="width:100px;height:100px"></div>`
	green := `data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' width='20' height='20'><rect width='100%' height='100%' fill='green'/></svg>`

	// element, który nie rośnie, wnosi do szerokości max-content najwyżej swój flex-basis
	same("basis-caps-contribution", `<div style="display:flex;float:left;height:100px;background:green">
		<div style="flex:0 1 100px;min-width:0"><div style="width:200px"></div></div></div>`, square)
	// a element, który rośnie, przynajmniej szerokość treści, także gdy ma mniejsze width
	same("width-below-content", `<div style="display:flex;float:left;background:green">
		<div style="flex:auto;width:10px"><div style="width:100px;height:100px"></div></div></div>`, square)
	// rozciągnięty obraz: minimum automatyczne z wysokości linii przez proporcje, nie z rozmiaru własnego
	same("stretched-image-auto-min", `<div style="display:flex;width:0;height:100px"><img src="`+green+`" style="width:999px"></div>`, square)
	// width: max-content elementu kolumny nie jest rozciągany; wysokość 100% obrazu liczy się od 100px
	same("max-content-item-percent-height", `<div style="display:flex;flex-direction:column">
		<div style="width:max-content;height:100px;background:red"><img src="`+green+`" style="height:100%;display:block"></div></div>`, square)
	// min-height: 100% obrazu w elemencie o wysokości 100% kontenera o wysokości 100px
	same("min-height-percent-image", `<div style="display:flex;flex-direction:column;width:min-content;height:100px;background:green">
		<div style="height:100%"><img style="min-height:100%;display:block" src="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' width='1' height='1'/>"></div></div>`, square)
	// SVG z samym viewBox: rozmiar bazowy to dostępna szerokość bez marginesu
	same("ratio-only-svg-basis", `<div style="display:flex;width:150px"><img style="flex:1 0 auto;margin-right:50px"
		src="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 2 2'><rect width='2' height='2' fill='green'/></svg>"></div>`, square)
	// podpis tabeli wchodzi do rozmiaru bazowego tabeli w kolumnie
	same("table-caption-in-column", `<div style="display:flex;flex-direction:column"><div style="display:table;border:10px solid green;
		background:green;width:80px;flex:0 0 50px"><div style="display:table-caption;height:10px;border:10px solid green;background:green"></div></div></div>`, square)
	// kontener flex z wysokością procentową w nieokreślonym wierszu siatki ma wysokość treści (tło
	// kontenera flex rysuje kwadrat 100×100, choć siatka ma 50px wysokości)
	same("percent-height-flex-in-grid", `<div style="display:grid;height:50px;width:100px"><div style="display:flex;height:100%;background:green">
		<div style="width:10px;height:100px"></div></div></div>`, square)
	// wiersz minmax(50px, max-content) w siatce o wysokości auto rośnie do wysokości treści
	same("grid-auto-height-maximize", `<div style="display:grid;grid-auto-rows:minmax(50px,max-content);width:100px">
		<div class=g style="height:100px"></div></div><div style="width:100px;height:10px;background:blue"></div>`,
		square+`<div style="width:100px;height:10px;background:blue"></div>`)
	// auto-fill przy aspect-ratio: min-height 60px daje minimalną szerokość 60px, czyli dwie kolumny
	same("auto-fill-aspect-ratio", `<div style="display:inline-grid;background:green;aspect-ratio:1/1;min-height:60px;
		grid-template-columns:repeat(auto-fill,50px);vertical-align:top"></div>`, square)
	// place-self: safe end zachowuje safe: element większy niż obszar stoi na jego początku
	same("place-self-safe", `<div style="display:grid;grid-template-columns:100px;width:100px">
		<div class=g style="place-self:safe end;width:100px;height:100px"></div></div>`, square)
	same("place-self-safe-overflow", `<div style="display:grid;grid-template-columns:50px;width:50px">
		<div class=g style="place-self:safe end;width:100px;height:100px"></div></div>`, square)
	// adres data: z nowymi wierszami (usuwane przez parser URL)
	same("url-with-newlines", `<img style="display:block;width:100px;height:100px" src="data:image/svg+xml,
		<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 2 2'><rect width='2' height='2' fill='green'/></svg>">`, square)
}
