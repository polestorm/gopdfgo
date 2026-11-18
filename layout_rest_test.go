package gopdfgo

import (
	"context"
	"os"
	"testing"
)

// TestLayoutRest sprawdza poprawki tabel, flex i siatki z siódmej rundy (testy WPT css-tables,
// css-flexbox, css-grid): wkład kolumn procentowych, wysokości procentowe komórek, tabele zwinięte
// z border-box, pozycjonowane wiersze, tabele pozycjonowane, pudełka z aspect-ratio i inne.
func TestLayoutRest(t *testing.T) {
	ahem, err := os.ReadFile("testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0; font: 10px/1 Ahem; color: transparent }
		.g { background: green } .a { position: absolute }</style>`
	dir := t.TempDir()
	page := func(name, document string) []byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(document), Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	near := func(name, test, reference string, tolerance int) {
		t.Helper()
		if n := differentPixels(page(name, head+test), page(name+"-ref", head+reference)); n > tolerance {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	same := func(name, test, reference string) { t.Helper(); near(name, test, reference, 0) }

	// tabela z komórką width: 100% obok innej komórki w elemencie flex nie rozpycha go do „nieskończonej”
	// szerokości: w kontenerze flex wkład kolumn procentowych się nie liczy
	same("percent-cell-in-flex-item", `<div style="display:flex;width:100px;height:50px;align-items:flex-start" class=g>
		<div style="flex:1 0"><table class=g style="height:50px" cellspacing=0 cellpadding=0><tr><td style="width:100%">X<td>X</table></div></div>`,
		`<div class=g style="width:100px;height:50px"></div>`)
	// to samo dla ścieżki max-content siatki
	same("percent-cell-in-grid-track", `<div style="display:grid;grid-template-columns:max-content;width:100px;height:50px" class=g>
		<table class=g style="height:50px" cellspacing=0 cellpadding=0><tr><td style="width:100%">X<td>X</table></div>`,
		`<div class=g style="width:100px;height:50px"></div>`)
	// width: min-content tabeli: komórki procentowe nie wypełniają bloku zawierającego
	same("table-width-min-content", `<table style="width:min-content" cellspacing=0 cellpadding=0><tr>
		<td class=g style="width:100%">XX<td class=g>XX</table>`,
		`<div class=g style="width:40px;height:10px"></div>`)
	// procentowa wysokość komórki nie jest minimum wiersza: dostaje resztę podanej wysokości tabeli
	same("percent-cell-height", `<table class=g style="width:100px;height:100px;border-spacing:0">
		<tr><td style="height:20px;padding:0"></td></tr><tr><td style="height:100%;padding:0"></td></tr></table>
		<div style="height:10px;background:blue"></div>`,
		`<div class=g style="width:100px;height:100px"></div><div style="height:10px;background:blue"></div>`)
	// tabela zwinięta z box-sizing: border-box (domyślnym) i width: 100px ma 100 px łącznie z obramowaniem
	// (jeden piksel narożnika różni się wygładzaniem obramowania zwiniętego)
	near("collapsed-border-box-width", `<table style="border-collapse:collapse;border:4px solid green;width:100px">
		<tr><td style="padding:0;height:20px"></td></tr></table>`,
		`<div style="width:92px;height:20px;border:4px solid green"></div>`, 2)
	// wiersz z position: relative rysuje tła swoich komórek
	same("positioned-row-cells", `<table style="border-spacing:0"><tr style="position:relative">
		<td class=g style="width:50px;height:50px;padding:0"></td></tr></table>`,
		`<div class=g style="width:50px;height:50px"></div>`)
	// tabela pozycjonowana z ujemnym left ma do dyspozycji najwyżej szerokość bloku zawierającego
	same("abspos-table-available", `<div style="position:relative;width:100px;height:100px;margin-left:100px">
		<table class=g style="position:absolute;left:-100px;height:100px;border-spacing:0"><td style="padding:0">X X X X X X X X X</td></table></div>`,
		`<div class=g style="width:100px;height:100px"></div>`)
	// elementy pływające w inline-block przesuwają się z nim przy vertical-align: top
	same("inline-block-floats-top", `<div><div style="display:inline-block;vertical-align:top;margin-top:20px;width:40px">
		<div class=g style="float:left;width:40px;height:30px"></div></div><span style="display:inline-block;height:80px"></span></div>`,
		`<div class="g a" style="top:20px;width:40px;height:30px"></div>`)
	// ::before z display: table-row w kontenerze flex jest blokowane i podlega order
	same("flex-table-row-pseudo", `<style>.f::before { display: table-row; content: ""; order: 1; width: 20px; height: 20px; background: green }</style>
		<div class=f style="display:flex"><div style="width:20px;height:20px;background:blue"></div></div>`,
		`<div style="width:20px;height:20px;background:blue"></div><div class="g a" style="top:0;left:20px;width:20px;height:20px"></div>`)
	// kontener flex z aspect-ratio i podaną wysokością ma szerokość z proporcji także jako wkład do rodzica
	same("flex-container-aspect-ratio", `<div style="display:inline-block;vertical-align:top"><div class=g
		style="display:flex;height:20px;aspect-ratio:1"><span>X X</span></div></div>`,
		`<div class=g style="width:20px;height:20px"></div>`)
	// ...ale nie mniejszą niż szerokość min-content treści
	same("aspect-ratio-min-content-floor", `<div style="display:inline-block;vertical-align:top"><div class=g
		style="height:20px;aspect-ratio:1">XXX X</div></div>`,
		`<div class=g style="width:30px;height:20px"></div>`)
	// element siatki z aspect-ratio i podaną wysokością nie jest rozciągany w poziomie
	same("grid-aspect-ratio-no-stretch", `<div style="display:grid;width:100px"><div class=g style="height:20px;aspect-ratio:1"></div></div>`,
		`<div class=g style="width:20px;height:20px"></div>`)
	// wieloliniowy kontener kolumnowy pływający dostaje szerokość z shrink-to-fit, nie sumę kolumn
	same("multiline-column-float", `<div style="width:150px"><div class=g style="display:flex;flex-flow:column wrap;float:left;height:30px;align-content:flex-start">
		<div style="flex:none;width:100px;height:20px"></div><div style="flex:none;width:100px;height:20px"></div></div></div>`,
		`<div class=g style="width:150px;height:30px"></div>`)
	// komentarz XML w <style> dokumentu XHTML nie psuje następnej reguły
	xhtml := `<html xmlns="http://www.w3.org/1999/xhtml"><head><style>@page { size: 300px 200px; margin: 0 } body { margin: 0 }
		<!-- komentarz --> div.g { width: 30px; height: 30px; background: green }</style></head><body><div class="g"/></body></html>`
	if n := differentPixels(page("xhtml-style-comment", xhtml), page("xhtml-style-comment-ref",
		`<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 }</style><div style="width:30px;height:30px;background:green"></div>`)); n != 0 {
		t.Errorf("xhtml-style-comment: %d pikseli różni się od wzorca", n)
	}
}
