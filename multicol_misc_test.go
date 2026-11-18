package gopdfgo

import (
	"context"
	"testing"
)

// TestMulticolMisc sprawdza poprawki kontenerów kolumn znalezione przy testach WPT css-multicol:
// wysokość kolumn z min-height, max-height i rozciągnięcia flex, column-width: 0, niepodzielność
// kontenera przewijania, krycie przodka elementu rozpinającego, kontener kolumn pozycjonowany
// bezwzględnie i elementy pływające dzielone między kolumny.
func TestMulticolMisc(t *testing.T) {
	head := `<style>@page { size: 300px 300px; margin: 0 } body { margin: 0 }
		.g { background: green } .a { position: absolute }</style>`
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

	// wysokość kolumn to height ograniczone przez min-height (test WPT multicol-fill-balance-005)
	same("min-height-clamps-height", `<div style="columns:2;column-gap:0;width:100px;height:20px;max-height:40px;min-height:100px">
		<div class=g style="height:200px"></div></div>`, square)
	// max-height ogranicza kolumny przy column-fill: auto (multicol-fill-auto-block-children-003)
	same("max-height-limits-columns", `<div style="columns:2;column-gap:0;column-fill:auto;width:100px;max-height:100px">
		<div class=g style="height:200px"></div></div>`, square)
	// wysokość z rozciągnięcia elementu flex ogranicza kolumny (multicol-fill-balance-029)
	same("flex-stretched-height", `<div style="display:flex;width:100px;height:100px">
		<div style="columns:1;width:50px;column-gap:0"><div class=g style="break-inside:avoid;height:100px"></div>
		<div class=g style="break-inside:avoid;height:100px"></div></div></div>`, square)
	// column-width: 0 daje kolumny szerokości 1px (zero-column-width-layout)
	same("zero-column-width", `<div style="width:100px;height:100px;column-width:0;column-gap:0">
		<div class=g style="height:10000px"></div></div>`, square)
	// kontener przewijania nie dzieli się między kolumny (overflow-unsplittable-003)
	same("overflow-hidden-unsplittable", `<div style="columns:2;column-gap:0;width:200px;height:50px">
		<div class=g style="overflow:hidden;width:100px"><div style="height:100px"></div></div></div>`, square)
	// niepodzielny kontener przewijania przechodzi do następnej kolumny, a kolumny rosną do jego wysokości
	same("overflow-hidden-next-column", `<div style="columns:2;column-gap:0;width:200px">
		<div class=g style="height:20px"></div><div class=g style="overflow:hidden"><div style="height:60px"></div></div></div>`,
		`<div class="g a" style="width:100px;height:20px"></div><div class="g a" style="left:100px;width:100px;height:60px"></div>`)
	// element rozpinający dziedziczy krycie przodka (spanner-in-opacity)
	same("spanner-in-opacity", `<div style="columns:2;column-gap:0;width:100px;background:blue">
		<div style="opacity:0.5"><div style="column-span:all;height:100px;background:red"></div></div></div>`,
		`<div style="width:100px;background:blue"><div style="opacity:0.5;height:100px;background:red"></div></div>`)
	// kontener kolumn pozycjonowany bezwzględnie (column-count-used-001)
	same("absolute-multicol", `<div class=a style="width:100px;columns:2;column-gap:0">
		<div class=g style="height:200px"></div></div>`, square)
	// element pływający dzieli się między zrównoważone kolumny (float-and-block)
	same("float-split-balanced", `<div style="columns:2;column-gap:0;width:100px">
		<div class=g style="float:left;width:50px;height:200px"></div><div style="height:200px;background:red"></div></div>`, square)
	// element pływający w kolumnach o podanej wysokości ciągnie się w następnej kolumnie
	same("float-split-fill-auto", `<div style="columns:2;column-gap:0;column-fill:auto;width:100px;height:100px">
		<div class=g style="float:left;width:50px;height:200px"></div></div>`, square)
}
