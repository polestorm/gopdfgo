package tests

import (
	"fmt"
	"strings"
	"testing"
)

// Podana wysokość (height, min-height) elementu i kontenera flex wyższa niż strona dzieli się między
// fragmenty (CSS Fragmentation 3 § 5.1, box-decoration-break: slice; CSS Flexbox 1 § 12): każdy fragment
// sięga dolnej krawędzi strony, a ostatni ma resztę wysokości. Wcześniej element flex nie korzystał
// z podziału bloku o podanej wysokości: wystawał poza stronę i jego dalsza część (tło) ginęła.
// Blok niższy niż strona, który nie zaczyna się na jej początku, nadal przechodzi w całości (ADR 0004).
//
// Strona 200×100 px; wzorce to bloki ustawione bezwzględnie na kolejnych stronach.
func TestFlexSpecifiedHeightFragmentation(t *testing.T) {
	dir := t.TempDir()
	same := func(name, test, reference string) {
		t.Helper()
		got := rasterize(t, renderString(t, test).PDF, dir, name)
		want := rasterize(t, renderString(t, reference).PDF, dir, name+"-ref")
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
	box := func(left, top, width, height int, color string) string {
		return fmt.Sprintf(`<div style="position:absolute;left:%dpx;top:%dpx;width:%dpx;height:%dpx;background:%s"></div>`,
			left, top, width, height, color)
	}
	page := `<style>@page { size: 200px 100px; margin: 0 } body { margin: 0 }</style>`
	pages := func(fragments ...string) string {
		var b strings.Builder
		b.WriteString(page)
		for i, f := range fragments {
			breakBefore := ""
			if i > 0 {
				breakBefore = "break-before:page;"
			}
			fmt.Fprintf(&b, `<div style="%sposition:relative;height:10px">%s</div>`, breakBefore, f)
		}
		return b.String()
	}
	tall := func(color string) []string { // fragmenty 100, 100, 50 px w kolumnie 0–50
		return []string{box(0, 0, 50, 100, color), box(0, 0, 50, 100, color), box(0, 0, 50, 50, color)}
	}
	g := tall("green")

	// Element wiersza o height 250px obok niższego sąsiada: trzy fragmenty, sąsiad tylko na pierwszej
	// stronie (wcześniej element przechodził na stronę 2 i tracił 150 px).
	same("item-height",
		page+`<div style="display:flex"><div style="height:250px;width:50px;background:green"></div>`+
			`<div style="width:50px;height:30px;background:blue"></div></div>`,
		pages(g[0]+box(50, 0, 50, 30, "blue"), g[1], g[2]))

	// To samo z min-height; sąsiad rozciągany (align-items: normal) ma wysokość linii, więc też się dzieli.
	same("item-min-height-stretch",
		page+`<div style="display:flex"><div style="min-height:250px;width:50px;background:green"></div>`+
			`<div style="width:50px;background:blue"></div></div>`,
		pages(g[0]+box(50, 0, 50, 100, "blue"), g[1]+box(50, 0, 50, 100, "blue"), g[2]+box(50, 0, 50, 50, "blue")))

	// Kontener o height albo min-height 250px z niskim elementem: wcześniej jedna strona.
	for _, prop := range []string{"height", "min-height"} {
		same("container-"+prop,
			page+`<div style="display:flex;`+prop+`:250px;width:50px;background:green">`+
				`<div style="width:20px;height:10px;background:blue"></div></div>`,
			pages(g[0]+box(0, 0, 20, 10, "blue"), g[1], g[2]))
	}

	// Kontener wierszowy o height 250px, rozciągnięty element dzieli treść; po kontenerze czarny pas.
	same("container-height-split-item",
		page+`<div style="display:flex;height:250px;width:50px;background:green"><div style="width:20px">`+
			`<div style="height:60px;background:blue"></div><div style="height:60px;background:red"></div></div></div>`+
			`<div style="height:10px;width:100px;background:black"></div>`,
		pages(g[0]+box(0, 0, 20, 60, "blue"), g[1]+box(0, 0, 20, 60, "red"), g[2]+box(0, 50, 100, 10, "black")))

	// Kontener kolumnowy z min-height 250px.
	same("column-min-height",
		page+`<div style="display:flex;flex-direction:column;min-height:250px;background:green;width:50px">`+
			`<div style="height:60px;background:blue"></div><div style="height:60px;background:red"></div></div>`+
			`<div style="height:10px;width:100px;background:black"></div>`,
		pages(g[0]+box(0, 0, 50, 60, "blue"), g[1]+box(0, 0, 50, 60, "red"), g[2]+box(0, 50, 100, 10, "black")))

	// Kolumna o height 250px z dwoma elementami flex-grow: po 125 px. Element wznowiony na stronie 2 zachowuje
	// swój rozmiar (wcześniej rósł o wolne miejsce drugiego i ten tracił część wysokości).
	same("column-grow",
		page+`<div style="display:flex;flex-direction:column;height:250px;width:50px">`+
			`<div style="flex-grow:1;background:green"></div><div style="flex-grow:1;background:blue"></div></div>`+
			`<div style="height:10px;width:100px;background:black"></div>`,
		pages(box(0, 0, 50, 100, "green"), box(0, 0, 50, 25, "green")+box(0, 25, 50, 75, "blue"),
			box(0, 0, 50, 50, "blue")+box(0, 50, 100, 10, "black")))

	// Kolumna o height 250px: element flex-grow i element 20px na końcu.
	same("column-grow-tail",
		page+`<div style="display:flex;flex-direction:column;height:250px;width:50px">`+
			`<div style="flex-grow:1;background:green"></div><div style="height:20px;background:blue"></div></div>`,
		pages(g[0], g[1], box(0, 0, 50, 30, "green")+box(0, 30, 50, 20, "blue")))

	// Element z obramowaniem (box-sizing: border-box): dolne obramowanie dopiero na ostatniej stronie,
	// a linia ostatniego fragmentu nie jest wyższa o górne obramowanie (wcześniej czerwony pas stał 5px niżej).
	same("item-border",
		page+`<div style="display:flex;align-items:flex-start"><div style="height:250px;width:50px;background:green;`+
			`border:5px solid black;box-sizing:border-box"></div><div style="width:50px;height:30px;background:blue"></div></div>`+
			`<div style="height:10px;background:red"></div>`,
		pages(box(0, 0, 50, 100, "black")+box(5, 5, 40, 95, "green")+box(50, 0, 50, 30, "blue"),
			box(0, 0, 50, 100, "black")+box(5, 0, 40, 100, "green"),
			box(0, 0, 50, 50, "black")+box(5, 0, 40, 45, "green")+box(0, 50, 200, 10, "red")))

	// Sąsiedzi wyrównani w linii wyższej niż strona przechodzą na stronę, na której wypadają
	// (wcześniej ginęli): center (y 110 i 116) i flex-end (y 220).
	same("siblings-center",
		page+`<div style="display:flex;align-items:center"><div style="height:250px;width:50px;background:green"></div>`+
			`<div style="width:50px;height:30px;background:blue"></div><div style="width:50px;background:red">`+
			`<div style="height:18px"></div></div></div>`,
		pages(g[0], g[1]+box(50, 10, 50, 30, "blue")+box(100, 16, 50, 18, "red"), g[2]))
	same("siblings-flex-end",
		page+`<div style="display:flex;align-items:flex-end"><div style="height:250px;width:50px;background:green"></div>`+
			`<div style="width:50px;height:30px;background:blue"></div></div>`,
		pages(g[0], g[1], g[2]+box(50, 20, 50, 30, "blue")))

	// Zagnieżdżony kontener flex o height 250px jako element wiersza (wcześniej panika przy wznowieniu).
	same("nested-container",
		page+`<div style="display:flex"><div style="display:flex;height:250px;width:50px;background:green">`+
			`<div style="width:20px;height:10px;background:blue"></div></div>`+
			`<div style="width:50px;height:30px;background:blue"></div></div>`,
		pages(g[0]+box(0, 0, 20, 10, "blue")+box(50, 0, 50, 30, "blue"), g[1], g[2]))

	// Zwykły blok z min-height 250px: reszta min-height przechodzi na dalsze strony, także gdy treść
	// dzieli się między strony (wcześniej ostatni fragment wystawał poza stronę).
	same("block-min-height",
		page+`<div style="min-height:250px;background:green;width:50px"><div style="width:20px">`+
			`<div style="height:60px;background:blue"></div><div style="height:60px;background:red"></div></div></div>`+
			`<div style="height:10px;width:100px;background:black"></div>`,
		pages(g[0]+box(0, 0, 20, 60, "blue"), g[1]+box(0, 0, 20, 60, "red"), g[2]+box(0, 50, 100, 10, "black")))
	same("block-min-height-short",
		page+`<div style="min-height:250px;background:green;width:50px"><div style="height:10px;width:20px;background:blue"></div></div>`+
			`<div style="height:10px;width:100px;background:black"></div>`,
		pages(g[0]+box(0, 0, 20, 10, "blue"), g[1], g[2]+box(0, 50, 100, 10, "black")))

	// Kontener niższy niż strona, który się na niej nie mieści, przechodzi w całości (ADR 0004).
	same("container-fits-page",
		page+`<div style="height:50px;background:blue"></div><div style="display:flex;height:80px;background:green;width:50px">`+
			`<div style="width:20px;height:10px;background:red"></div></div>`,
		pages(box(0, 0, 200, 50, "blue"), box(0, 0, 50, 80, "green")+box(0, 0, 20, 10, "red")))
}
