package tests

import (
	"fmt"
	"strings"
	"testing"
)

// Komórka z rowspan, której wiersze przechodzą na następną stronę, jest dzielona razem z nimi
// (CSS Tables 3 § 6, CSS Fragmentation 3): fragment na każdej stronie sięga dołu wierszy tej strony,
// obramowanie jest krojone (box-decoration-break: slice), a treść, która nie zmieściła się na stronie,
// przechodzi do dalszego ciągu komórki. Wzorce to bloki ustawione bezwzględnie na kolejnych stronach.
func TestRowspanFragmentation(t *testing.T) {
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
	// box rysuje prostokąt; pages składa strony z prostokątów
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
		table { border-spacing: 0 } td { padding: 0; width: 50px } .rs { background: green }
		.r td:last-child { background: yellow } .mark { height: 10px; background: black }</style>`
	rows := func(n, height int) string {
		var b strings.Builder
		for range n {
			fmt.Fprintf(&b, `<tr class="r"><td style="height:%dpx"></td></tr>`, height)
		}
		return b.String()
	}

	// Cztery wiersze po 40px na stronach po 100px: na każdej stronie dwa wiersze i fragment komórki
	// rozpiętej o ich wysokości (80px). Treść komórki (10px) jest w pierwszym fragmencie, u góry.
	same("rowspan-basic",
		page+`<table><tr class="r"><td class="rs" rowspan="4"><div class="mark"></div></td><td style="height:40px"></td></tr>`+
			rows(3, 40)+`</table>`,
		pages(page,
			box(0, 0, 50, 80, "green")+box(0, 0, 50, 10, "black")+box(50, 0, 50, 80, "yellow"),
			box(0, 0, 50, 80, "green")+box(50, 0, 50, 80, "yellow")))

	// Obramowanie komórki krojone: pierwszy fragment bez dolnego, dalszy ciąg bez górnego.
	bordered := `<style>.rs { border: 4px solid blue }</style>`
	same("rowspan-border-slice",
		page+bordered+`<table><tr class="r"><td class="rs" rowspan="4"><div class="mark"></div></td><td style="height:40px"></td></tr>`+
			rows(3, 40)+`</table>`,
		pages(page,
			box(0, 0, 58, 80, "blue")+box(4, 4, 50, 76, "green")+box(4, 4, 50, 10, "black")+box(58, 0, 50, 80, "yellow"),
			box(0, 0, 58, 80, "blue")+box(4, 0, 50, 76, "green")+box(58, 0, 50, 80, "yellow")))

	// Powtarzany nagłówek (20px): na stronie 120px pod nagłówkiem mieszczą się dwa wiersze.
	headed := `<style>@page { size: 200px 120px } th { padding: 0; height: 20px; background: blue }</style>`
	same("rowspan-thead",
		page+headed+`<table><thead><tr><th></th><th></th></tr></thead>`+
			`<tr class="r"><td class="rs" rowspan="4"><div class="mark"></div></td><td style="height:40px"></td></tr>`+
			rows(3, 40)+`</table>`,
		pages(page+headed,
			box(0, 0, 100, 20, "blue")+box(0, 20, 50, 80, "green")+box(0, 20, 50, 10, "black")+box(50, 20, 50, 80, "yellow"),
			box(0, 0, 100, 20, "blue")+box(0, 20, 50, 80, "green")+box(50, 20, 50, 80, "yellow")))

	// Obramowania zwinięte: na końcu pierwszego fragmentu i na początku dalszego ciągu nie ma poziomej
	// linii w kolumnie komórki rozpiętej (to wnętrze komórki); druga kolumna ma linie wierszy jak zwykle.
	collapsed := `<style>table { border-collapse: collapse } td { border: 4px solid blue }</style>`
	same("rowspan-collapse",
		page+collapsed+`<table><tr class="r"><td class="rs" rowspan="4"><div class="mark"></div></td><td style="height:32px"></td></tr>`+
			rows(3, 32)+`</table>`,
		pages(page,
			box(0, 0, 112, 74, "blue")+box(54, 74, 58, 2, "blue")+box(4, 4, 50, 70, "green")+box(4, 4, 50, 10, "black")+
				box(58, 4, 50, 32, "yellow")+box(58, 40, 50, 32, "yellow"),
			box(54, 0, 58, 2, "blue")+box(0, 2, 112, 74, "blue")+box(4, 2, 50, 70, "green")+
				box(58, 4, 50, 32, "yellow")+box(58, 40, 50, 32, "yellow")))

	// Treść komórki dłuższa niż strona (25 pasków po 10px): podział treści nie dzieli pierwszego wiersza,
	// wiersze 1–3 zostają na pierwszej stronie, ostatni z nich sięga końca strony, a treść płynie dalej
	// przez dwie następne strony; za nią czwarty wiersz. Nic nie ginie ani się nie powtarza.
	var tall strings.Builder
	for i := range 25 {
		color := "black"
		if i%2 == 1 {
			color = "red"
		}
		fmt.Fprintf(&tall, `<div style="height:10px;background:%s"></div>`, color)
	}
	stripes := func(top, from, count int) string {
		var b strings.Builder
		for i := range count {
			color := "black"
			if (from+i)%2 == 1 {
				color = "red"
			}
			b.WriteString(box(0, top+10*i, 50, 10, color))
		}
		return b.String()
	}
	topAligned := `<style>td { vertical-align: top }</style>`
	same("rowspan-tall-content",
		page+topAligned+`<table><tr class="r"><td class="rs" rowspan="3">`+tall.String()+`</td><td style="height:30px"></td></tr>`+
			rows(2, 30)+`<tr><td style="height:30px;background:blue"></td><td style="height:30px;background:blue"></td></tr></table>`,
		pages(page,
			stripes(0, 0, 10)+box(50, 0, 50, 100, "yellow"),
			stripes(0, 10, 10)+box(50, 0, 50, 100, "yellow"),
			stripes(0, 20, 5)+box(50, 0, 50, 50, "yellow")+box(0, 50, 100, 30, "blue")))
}
