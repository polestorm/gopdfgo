package tests

import (
	"fmt"
	"strings"
	"testing"
)

// Element pływający, który nie mieści się w reszcie strony i nie może się podzielić (treść niepodzielna albo
// break-inside: avoid), przechodzi na następną stronę sam; treść w przepływie, która stoi za nim w źródle,
// zostaje na tej stronie (CSS 2.1 § 9.5.1, reguły 5 i 6 ograniczają położenie elementu pływającego tylko
// treścią wcześniejszą; tak drukują Chromium i Gecko, wzorzec WPT CSS2/pagination/float-page-break-inside-avoid-8,
// pomiar w Chrome 157 z 2026-11-21). Wcześniej cały akapit przechodził razem z elementem pływającym
// (pusta reszta strony), a element pływający w środku wiersza był ucinany na dole strony.
//
// Strona 200×100 px; treść w przepływie to bloki liniowe 60×10 px w wierszu o font-size 0, wzorce to bloki
// ustawione bezwzględnie na kolejnych stronach.
func TestPushedFloats(t *testing.T) {
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
	page := `<style>@page { size: 200px 100px; margin: 0 } body { margin: 0 }
		.ib { display: inline-block; width: 60px; height: 10px; vertical-align: top }
		.f { float: left; width: 40px; height: 50px; break-inside: avoid; background: green }</style>`
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
	filler := `<div style="height:70px;background:gray"></div>`
	first := box(0, 0, 200, 70, "gray")

	// Element pływający przed treścią liniową: treść zostaje na stronie 1 na całej szerokości.
	same("before-line",
		page+filler+`<div style="font-size:0"><div class="f"></div><span class="ib" style="background:blue"></span></div>`,
		pages(first+box(0, 70, 60, 10, "blue"), box(0, 0, 40, 50, "green")))

	// Element pływający w środku wiersza: wiersz zostaje cały, element nie jest ucinany na dole strony.
	same("mid-line",
		page+filler+`<div style="font-size:0"><span class="ib" style="background:blue"></span><div class="f"></div>`+
			`<span class="ib" style="background:red"></span></div>`,
		pages(first+box(0, 70, 60, 10, "blue")+box(60, 70, 60, 10, "red"), box(0, 0, 40, 50, "green")))

	// Następny element pływający, który sam by się zmieścił, nie może stanąć wyżej niż przeniesiony
	// (CSS 2.1 § 9.5.1, reguła 5): przechodzi razem z nim.
	same("later-float-follows",
		page+filler+`<div style="font-size:0"><div class="f"></div>`+
			`<div style="float:right;width:20px;height:20px;background:orange"></div>`+
			`<span class="ib" style="background:blue"></span></div>`,
		pages(first+box(0, 70, 60, 10, "blue"), box(0, 0, 40, 50, "green")+box(180, 0, 20, 20, "orange")))

	// Blok z clear staje na następnej stronie pod przeniesionym elementem, treść przed nim zostaje.
	same("clear-after-pushed",
		page+filler+`<div style="font-size:0"><div class="f"></div><span class="ib" style="background:blue"></span></div>`+
			`<div style="clear:left;width:100px;height:10px;background:black"></div>`,
		pages(first+box(0, 70, 60, 10, "blue"), box(0, 0, 40, 50, "green")+box(0, 50, 100, 10, "black")))

	// Element pływający jako dziecko blokowe (obok bloków, nie w wierszu): blok za nim zostaje na stronie 1.
	same("block-sibling",
		page+filler+`<div><div class="f"></div><div style="width:60px;height:10px;background:blue"></div></div>`,
		pages(first+box(0, 70, 60, 10, "blue"), box(0, 0, 40, 50, "green")))
}
