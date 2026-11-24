package tests

import (
	"context"
	"github.com/polestorm/gopdfgo/htmlpdf"
	"os"
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
	ahem, err := os.ReadFile("../testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]htmlpdf.Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	page := func(name, body string) []byte {
		t.Helper()
		result, err := htmlpdf.Render(context.Background(), []byte(head+body), htmlpdf.Options{Fonts: fonts})
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
	// zagnieżdżony kontener o podanej wysokości dzieli się między kolumny zewnętrzne także wtedy, gdy jego
	// treść skończyła się wcześniej (multicol-breaking-001)
	same("nested-fixed-height-remainder", `<div style="columns:3;column-gap:0;column-fill:auto;width:150px;height:100px">
		<div class=g style="columns:2;column-gap:0;height:300px"><div style="height:20px"></div></div></div>`,
		`<div class=g style="width:150px;height:100px"></div>`)
	// box-decoration-break: clone daje każdemu fragmentowi dolne obramowanie (multicol-breaking-004)
	same("nested-clone-decoration", `<div style="columns:2;column-gap:0;column-fill:auto;width:100px;height:100px">
		<div style="columns:1;column-gap:0;height:150px;border-bottom:25px solid green;box-decoration-break:clone">
		<div class=g style="height:150px"></div></div></div>`, square)
	// pusty wiersz po końcowym <br> nie zawyża wysokości kolumn przy równoważeniu (multicol-breaking-000):
	// wynik jak bez końcowego <br>
	nested := func(tail string) string {
		return `<div style="columns:2;column-gap:0;column-fill:auto;width:200px;height:100px">
		<div style="columns:2;column-gap:0;height:300px;font:16px/1.25 Ahem;color:green;background:blue">
		X<br>X<br>X<br>X<br>X` + tail + `</div></div>`
	}
	same("trailing-br-balance", nested("<br>\n"), nested("\n"))
	// wymuszony podział kolumny przed elementem rozpinającym i za nim dzieli kolumny zewnętrzne
	// (spanner-fragmentation-010, -012)
	same("spanner-break-before-column", `<div style="width:100px;columns:2;column-fill:auto;column-gap:0;height:300px">
		<div class=g style="height:50px"></div><div style="columns:2;column-gap:0"><div class=g style="height:100px"></div>
		<div class=g style="column-span:all;break-before:column;height:100px"></div></div></div>`, square)
	same("spanner-break-after-column", `<div style="width:100px;columns:2;column-fill:auto;column-gap:0;height:300px">
		<div class=g style="height:50px"></div><div style="columns:2;column-gap:0">
		<div class=g style="column-span:all;break-after:column;height:50px"></div><div class=g style="height:200px"></div></div></div>`, square)
	// kolumny o zerowej wysokości mają 1px, a element pływający ciągnie się przez kolejne kolumny
	// (multicol-zero-height-001)
	same("zero-height-columns", `<div style="height:0;width:200px;column-count:10;column-gap:0">
		<div class=g style="float:left;width:100%;height:10px"></div></div>`, `<div class=g style="width:200px;height:1px"></div>`)
	// dalszy ciąg elementu pływającego w trzeciej kolumnie i dalszych stoi w swojej kolumnie
	same("float-split-three-columns", `<div style="columns:3;column-gap:0;column-fill:auto;width:150px;height:100px">
		<div class=g style="float:left;width:50px;height:300px"></div></div>`, `<div class=g style="width:150px;height:100px"></div>`)
}
