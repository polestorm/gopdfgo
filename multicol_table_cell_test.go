package gopdfgo

import (
	"fmt"
	"testing"
)

// Komórka tabeli i pudełko liniowo-blokowe z column-count są kontenerami kolumn (CSS Multicol 1 § 2).
// Przed poprawką ich treść stała w jednej kolumnie (testy WPT css-multicol/table/*).
func TestMulticolTableCell(t *testing.T) {
	head := `<style>@page { size: 400px 300px; margin: 0 } body { margin: 0 }</style>`
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
	boxes := func(n, height int) (s string) {
		for range n {
			s += fmt.Sprintf(`<div style="height:%dpx;background:green"></div>`, height)
		}
		return s
	}
	at := func(left, top, width, height int, color string) string {
		return fmt.Sprintf(`<div style="position:absolute;left:%dpx;top:%dpx;width:%dpx;height:%dpx;background:%s"></div>`,
			left, top, width, height, color)
	}

	// Komórka o wysokości auto równoważy kolumny: sześć bloków po 20px daje dwie kolumny po 60px,
	// a wiersz ma wysokość zrównoważonych kolumn.
	same("balanced-cell", `<div style="display:table-cell;width:200px;columns:2;column-gap:20px;background:yellow">`+
		boxes(6, 20)+`</div><div style="height:10px;background:blue"></div>`,
		at(0, 0, 200, 60, "yellow")+at(0, 0, 90, 60, "green")+at(110, 0, 90, 60, "green")+at(0, 60, 400, 10, "blue"))

	// Komórka o szerokości auto: szerokość wewnętrzna kontenera kolumn to liczba kolumn razy szerokość
	// treści kolumny plus odstępy (3 x 30 + 2 x 10 = 110px).
	same("shrink-to-fit-cell", `<div style="display:table-cell;columns:3;column-gap:10px;background:red">`+
		`<div style="width:30px;height:20px;background:green"></div><div style="width:30px;height:20px;background:green"></div>`+
		`<div style="width:30px;height:20px;background:green"></div></div>`,
		at(0, 0, 110, 20, "red")+at(0, 0, 30, 20, "green")+at(40, 0, 30, 20, "green")+at(80, 0, 30, 20, "green"))

	// Komórka o podanej wysokości wyższej niż treść: kolumny są równoważone, a vertical-align: middle
	// przesuwa je na środek komórki.
	same("vertical-align-middle", `<div style="display:table-cell;width:100px;height:200px;vertical-align:middle;columns:2;column-gap:0;background:blue">`+
		boxes(4, 20)+`</div>`,
		at(0, 0, 100, 200, "blue")+at(0, 80, 100, 40, "green"))

	// max-height nie ogranicza komórki: wiersz ma wysokość treści.
	same("cell-ignores-max-height", `<div style="display:table-cell;width:100px;height:10px;max-height:10px;columns:2;column-gap:0;background:red">`+
		boxes(4, 20)+`</div><div style="height:10px;background:blue"></div>`,
		at(0, 0, 100, 40, "green")+at(0, 40, 400, 10, "blue"))

	// Komórka o podanej wysokości dzielona między kolumny zewnętrzne zajmuje w pierwszej resztę
	// kolumny, a w drugiej resztę podanej wysokości (100 - 50 = 50px).
	same("fixed-height-cell-across-columns", `<div style="columns:2;gap:0;column-fill:auto;width:100px;height:100px">`+
		`<div style="height:50px;background:green"></div>`+
		`<div style="display:table-cell;columns:2;gap:0;height:100px;width:50px;background:yellow">`+
		`<div style="height:150px;background:green"></div></div><div style="height:10px;background:blue"></div></div>`,
		at(0, 0, 50, 100, "green")+at(50, 0, 50, 50, "yellow")+at(50, 0, 50, 25, "green")+at(50, 50, 50, 10, "blue"))

	// Pudełko liniowo-blokowe z kolumnami.
	same("inline-block", `<div style="display:inline-block;width:100px;columns:2;column-gap:0;vertical-align:top">`+
		boxes(4, 20)+`</div>`,
		at(0, 0, 100, 40, "green"))
}
