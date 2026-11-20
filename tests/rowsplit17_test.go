package tests

import (
	"fmt"
	"strings"
	"testing"
)

// Wiersz tabeli wyższy niż cała strona (po odjęciu powtarzanego nagłówka i stopki) jest dzielony tam, gdzie
// wypada, a nie najpierw przenoszony na następną stronę (decyzja użytkownika z 2026-11-20, uzupełnienie
// ADR 0020; CSS Fragmentation 3 § 4.4 i § 5.1). Wiersz niższy od strony nadal przechodzi w całości (CSS
// Tables 3 § 6.1). Wzorce to bloki ustawione bezwzględnie na kolejnych stronach.
func TestTallRowSplitInPlace(t *testing.T) {
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
	pages := func(page string, fragments ...string) string {
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
	page := `<style>@page { size: 200px 100px; margin: 0 } body { margin: 0 }
		table { border-spacing: 0 } td, th { padding: 0; width: 50px; vertical-align: top }
		.l { height: 10px; background: green }</style>`
	lead := `<div style="height:40px;background:blue"></div>`
	lines := func(n int) string { return strings.Repeat(`<div class="l"></div>`, n) }
	after := `<tr><td style="height:20px;background:red"></td></tr>`

	// Komórka o podanej wysokości 150px pod blokiem 40px: 60px na stronie 1, 90px na stronie 2
	// (wcześniej strona 1 bez tabeli, wiersz od strony 2).
	same("cell-height",
		page+lead+`<table><tr><td style="height:150px;background:green"></td></tr>`+after+`</table>`,
		pages(page,
			box(0, 0, 200, 40, "blue")+box(0, 40, 50, 60, "green"),
			box(0, 0, 50, 90, "green"),
			box(0, 0, 50, 20, "red")))

	// Podana wysokość wiersza (tr) 150px: dzielona tak samo; wcześniej wiersz przechodził na stronę 2
	// i jego część za końcem tej strony ginęła.
	same("row-height",
		page+lead+`<table><tr style="height:150px;background:green"><td></td></tr>`+after+`</table>`,
		pages(page,
			box(0, 0, 200, 40, "blue")+box(0, 40, 50, 60, "green"),
			box(0, 0, 50, 90, "green"),
			box(0, 0, 50, 20, "red")))

	// Wiersz (nie pierwszy w grupie) z treścią 150px i z blokiem niepodzielnym 70px, który nie mieści się
	// pod nim na stronie 1: wiersz jest dzielony na stronie 1, a blok przechodzi do dalszego ciągu
	// komórki na stronie 2. Wcześniej cały wiersz przechodził na stronę 2.
	same("content-monolithic",
		page+lead+`<table><tr><td style="height:10px;background:red"></td></tr>`+
			`<tr><td>`+lines(15)+`</td><td><div style="height:70px;overflow:hidden;background:yellow"></div></td></tr>`+after+`</table>`,
		pages(page,
			box(0, 0, 200, 40, "blue")+box(0, 40, 50, 10, "red")+box(0, 50, 50, 50, "green"),
			box(0, 0, 50, 100, "green")+box(50, 0, 50, 70, "yellow"),
			box(0, 0, 50, 20, "red")))

	// Ten sam wiersz z treścią 50px jest niższy od strony, więc przechodzi na stronę 2 w całości
	// (CSS Tables 3 § 6.1, ADR 0020).
	same("content-monolithic-short",
		page+lead+`<table><tr><td style="height:10px;background:red"></td></tr>`+
			`<tr><td>`+lines(5)+`</td><td><div style="height:70px;overflow:hidden;background:yellow"></div></td></tr>`+after+`</table>`,
		pages(page,
			box(0, 0, 200, 40, "blue")+box(0, 40, 50, 10, "red"),
			box(0, 0, 50, 50, "green")+box(50, 0, 50, 70, "yellow")+box(0, 70, 50, 20, "red")))

	// Wiersz o podanej wysokości 70px (niższy od strony) przechodzi w całości.
	same("cell-height-short",
		page+lead+`<table><tr><td style="height:70px;background:green"></td></tr></table>`,
		pages(page,
			box(0, 0, 200, 40, "blue"),
			box(0, 0, 50, 70, "green")))

	// Powtarzany nagłówek i stopka po 20px: wierszom zostaje na każdej stronie 60px. Wiersz 140px pod
	// wierszem 30px jest dzielony od razu (30 + 60 + 50), a wiersz 60px przechodzi w całości.
	parts := `<style>th { height: 20px; background: blue } tfoot th { background: black }</style>`
	table := func(rows string) string {
		return `<table><thead><tr><th></th></tr></thead><tfoot><tr><th></th></tr></tfoot>` +
			`<tr><td style="height:30px;background:yellow"></td></tr>` + rows + `</table>`
	}
	same("repeated-parts",
		page+parts+table(`<tr><td style="height:140px;background:green"></td></tr>`),
		pages(page,
			box(0, 0, 50, 20, "blue")+box(0, 20, 50, 30, "yellow")+box(0, 50, 50, 30, "green")+box(0, 80, 50, 20, "black"),
			box(0, 0, 50, 20, "blue")+box(0, 20, 50, 60, "green")+box(0, 80, 50, 20, "black"),
			box(0, 0, 50, 20, "blue")+box(0, 20, 50, 50, "green")+box(0, 70, 50, 20, "black")))
	same("repeated-parts-fits",
		page+parts+table(`<tr><td style="height:60px;background:green"></td></tr>`),
		pages(page,
			box(0, 0, 50, 20, "blue")+box(0, 20, 50, 30, "yellow")+box(0, 50, 50, 20, "black"),
			box(0, 0, 50, 20, "blue")+box(0, 20, 50, 60, "green")+box(0, 80, 50, 20, "black")))

	// Komórka z rowspan obok wysokiego wiersza: dalszy ciąg komórki rozpiętej (spanResumeKey) idzie
	// razem z dalszym ciągiem wiersza, a na stronę 3 razem z wierszem 20px, który nie mieści się na stronie 2.
	same("rowspan",
		page+lead+`<table><tr><td rowspan="2" style="background:yellow"></td><td style="height:150px;background:green"></td></tr>`+
			`<tr><td style="height:20px;background:red"></td></tr></table>`,
		pages(page,
			box(0, 0, 200, 40, "blue")+box(0, 40, 50, 60, "yellow")+box(50, 40, 50, 60, "green"),
			box(0, 0, 50, 90, "yellow")+box(50, 0, 50, 90, "green"),
			box(0, 0, 50, 20, "yellow")+box(50, 0, 50, 20, "red")))
}
