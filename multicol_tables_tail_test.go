package gopdfgo

import (
	"fmt"
	"testing"
)

// Ogon kolumn i tabel: przypadki z sondażu testów W3C css-multicol i css-tables. Każdy przypadek
// porównuje układ z wzorcem z bloków pozycjonowanych.
func TestMulticolTablesTail(t *testing.T) {
	head := `<style>@page { size: 400px 300px; margin: 0 } body { margin: 0 }
		.outer { columns: 2; column-fill: auto; column-gap: 0; width: 200px; height: 200px }</style>`
	dir := t.TempDir()
	same := func(name, test, reference string) {
		t.Helper()
		got := rasterize(t, renderString(t, head+test).PDF, dir, name)
		want := rasterize(t, renderString(t, head+reference).PDF, dir, name+"-ref")
		if len(got) != len(want) {
			t.Errorf("%s: %d stron, wzorzec ma %d", name, len(got), len(want))
			return
		}
		for i := range got {
			if n := differentPixels(got[i], want[i]); n != 0 {
				t.Errorf("%s, strona %d: %d pikseli różni się od wzorca", name, i+1, n)
			}
		}
	}
	block := func(left, top, width, height int, color string) string {
		return fmt.Sprintf(`<div style="position:absolute;left:%dpx;top:%dpx;width:%dpx;height:%dpx;background:%s"></div>`,
			left, top, width, height, color)
	}

	// Arkusz domyślny HTML nie zabrania podziału za nagłówkiem: kolumna kończy się za nagłówkiem
	// i jego dolnym marginesem, następny blok zaczyna drugą kolumnę.
	same("heading-break-after-allowed", `<div class="outer"><div style="height:140px;background:blue"></div>`+
		`<h4 style="margin:0 0 20px;height:20px;background:green"></h4><div style="height:60px;break-inside:avoid;background:blue"></div></div>`,
		block(0, 0, 100, 140, "blue")+block(0, 140, 100, 20, "green")+block(100, 0, 100, 60, "blue"))

	// Tabela z nagłówkiem, z której w pierwszej kolumnie zmieściłby się tylko nagłówek, przechodzi
	// w całości do drugiej kolumny; nagłówek nie traci powtarzania w próbach równoważenia.
	rows := ""
	for range 3 {
		rows += `<tr><td style="padding:0;height:20px"></td></tr>`
	}
	same("table-header-not-alone", `<div class="outer"><div style="height:170px;background:blue"></div>`+
		`<table style="border-spacing:0;width:100%"><thead><tr><th style="padding:0;height:20px;background:green"></th></tr></thead>`+
		`<tbody style="background:navy">`+rows+`</tbody></table></div>`,
		block(0, 0, 100, 170, "blue")+block(100, 0, 100, 20, "green")+block(100, 20, 100, 60, "navy"))
}
