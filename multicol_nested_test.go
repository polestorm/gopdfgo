package gopdfgo

import (
	"fmt"
	"testing"
)

// Kontenery kolumn zagnieżdżone w kolumnach i fragmenty kontenerów kolumn (CSS Multicol 1 § 8,
// CSS Fragmentation 3 § 5). Każdy przypadek porównuje układ z wzorcem z bloków pozycjonowanych.
func TestNestedMulticolFragmentation(t *testing.T) {
	head := `<style>@page { size: 400px 300px; margin: 0 } body { margin: 0 }
		.outer { columns: 2; column-fill: auto; column-gap: 0; width: 200px; height: 100px }</style>`
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

	// Blok z break-inside: avoid nie mieści się w drugiej kolumnie wewnętrznej, więc przechodzi do
	// następnej kolumny zewnętrznej, gdzie jest więcej miejsca; druga kolumna wewnętrzna zostaje pusta.
	same("deferred-to-next-outer-column", `<div class="outer"><div style="height:50px;background:green"></div>`+
		`<div style="columns:2;column-fill:auto;column-gap:0"><div style="width:200%;height:50px;background:green"></div>`+
		`<div style="width:200%;height:100px;break-inside:avoid;background:green"></div></div></div>`,
		block(0, 0, 200, 100, "green"))

	// Margines pierwszego bloku kolejnej kolumny przylega do podziału kolumny i jest obcinany; margines
	// na początku kontenera zostaje.
	same("margin-truncated-after-column-break", `<div class="outer">`+
		`<div style="margin-top:20px;height:60px;background:green"></div><div style="margin-top:20px;height:60px;background:blue"></div></div>`,
		block(0, 20, 100, 60, "green")+block(100, 0, 100, 60, "blue"))

	// Zagnieżdżone kontenery zrównoważone: drugi zaczyna się u góry drugiej kolumny zewnętrznej bez
	// marginesu, więc kolumny zewnętrzne mają po 80px.
	inner := func(color string) string {
		return `<div style="columns:2;column-gap:0;margin:20px 0"><div style="height:120px;background:` + color + `"></div></div>`
	}
	same("balanced-nested-margins", `<div style="columns:2;column-gap:0;width:200px;background:yellow">`+
		`<div style="columns:2;column-gap:0;margin:0 0 20px"><div style="height:120px;background:green"></div></div>`+inner("blue")+`</div>`,
		block(0, 0, 200, 80, "yellow")+block(0, 0, 100, 60, "green")+block(100, 0, 100, 60, "blue"))

	// min-height dotyczy sumy fragmentów: pierwszy fragment kończy się na dole kolumny zewnętrznej,
	// drugi ma resztę (125 - 100 = 25px), choć jego treść ma tylko 5px.
	same("min-height-across-fragments", `<div class="outer">`+
		`<div style="columns:1;column-fill:auto;min-height:125px;background:green"><div style="height:105px"></div></div>`+
		`<div style="height:20px;background:blue"></div></div>`,
		block(0, 0, 100, 100, "green")+block(100, 0, 100, 25, "green")+block(100, 25, 100, 20, "blue"))

	// Element pływający może być kontenerem kolumn: treść 100px rozkłada się po 50px na dwie kolumny.
	same("floated-multicol", `<div style="float:left;columns:2;column-gap:0;width:200px"><div style="height:100px;background:green"></div></div>`,
		block(0, 0, 200, 50, "green"))
}
