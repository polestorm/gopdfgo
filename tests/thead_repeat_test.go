package tests

import (
	"fmt"
	"strings"
	"testing"
)

// Powtarzanie nagłówka i stopki tabeli (CSS Tables 3 § 6.2): każda z tych grup jest powtarzana tylko
// wtedy, gdy mieści się w ćwierci wysokości fragmentatora, a ocenia się je osobno. Przypadki stron
// odtwarzają pomiar w Chrome 157 (strona 600×1000px, nagłówek 20% i 30% strony, stopka 40px, 60 wierszy
// po 40px): przy 20% nagłówek jest na wszystkich stronach, przy 30% tylko na pierwszej, a stopka nadal na
// każdej. Wiersze mają wysokość wiersza tekstu, żeby wynik nie zależał od dzielenia wierszy tabeli.
func TestTheadRepeat(t *testing.T) {
	dir := t.TempDir()
	same := func(name, head, test, reference string) {
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
	table := func(rows, header, footer int) string {
		var b strings.Builder
		fmt.Fprintf(&b, `<table style="border-spacing:0;width:100px"><thead><tr><th style="height:%dpx;background:green"></th></tr></thead>`, header)
		fmt.Fprintf(&b, `<tfoot><tr><th style="height:%dpx;background:blue"></th></tr></tfoot><tbody>`, footer)
		for range rows {
			b.WriteString(`<tr><td style="height:40px;background:navy"></td></tr>`)
		}
		b.WriteString(`</tbody></table>`)
		return b.String()
	}
	block := func(height int, color string) string {
		return fmt.Sprintf(`<div style="width:100px;height:%dpx;background:%s"></div>`, height, color)
	}
	// fragment: nagłówek (0 = brak), wiersze, stopka (0 = brak)
	fragment := func(header, rows, footer int) string {
		s := ""
		if header > 0 {
			s += block(header, "green")
		}
		s += block(rows*40, "navy")
		if footer > 0 {
			s += block(footer, "blue")
		}
		return s
	}
	pages := func(fragments ...string) string {
		var b strings.Builder
		for i, f := range fragments {
			if i > 0 {
				b.WriteString(`<div style="break-before:page"></div>`)
			}
			b.WriteString(f)
		}
		return b.String()
	}
	paged := `<style>@page { size: 600px 1000px; margin: 0 } body { margin: 0 } th, td { padding: 0 }</style>`

	// Nagłówek 200px (ćwierć strony to 250px): powtarzany na każdej z 4 stron, 19 wierszy na stronę.
	same("thead20", paged, table(60, 200, 40),
		pages(fragment(200, 19, 40), fragment(200, 19, 40), fragment(200, 19, 40), fragment(200, 3, 40)))

	// Nagłówek 300px: tylko na pierwszej stronie; stopka 40px nadal na każdej, razem 3 strony.
	same("thead30", paged, table(60, 300, 40),
		pages(fragment(300, 16, 40), fragment(0, 24, 40), fragment(0, 20, 40)))

	// Stopka wyższa niż ćwierć strony stoi tylko na końcu tabeli, nagłówek jest nadal powtarzany.
	same("tfoot30", paged, table(40, 40, 300),
		pages(fragment(40, 24, 0), fragment(40, 16, 300)))

	// W kolumnach fragmentatorem jest kolumna (400px, ćwierć to 100px).
	multicol := `<style>@page { size: 400px 600px; margin: 0 } body { margin: 0 } th, td { padding: 0 }
		.outer { columns: 2; column-fill: auto; column-gap: 0; width: 200px; height: 400px }</style>`
	at := func(left, top, height int, color string) string {
		return fmt.Sprintf(`<div style="position:absolute;left:%dpx;top:%dpx;width:100px;height:%dpx;background:%s"></div>`,
			left, top, height, color)
	}
	// nagłówek 120px: tylko w pierwszej kolumnie, stopka 20px w obu
	same("thead-multicol-tall", multicol, `<div class="outer">`+table(12, 120, 20)+`</div>`,
		at(0, 0, 120, "green")+at(0, 120, 240, "navy")+at(0, 360, 20, "blue")+
			at(100, 0, 240, "navy")+at(100, 240, 20, "blue"))
	// nagłówek 80px: powtarzany w obu kolumnach
	same("thead-multicol-short", multicol, `<div class="outer">`+table(12, 80, 20)+`</div>`,
		at(0, 0, 80, "green")+at(0, 80, 280, "navy")+at(0, 360, 20, "blue")+
			at(100, 0, 80, "green")+at(100, 80, 200, "navy")+at(100, 280, 20, "blue"))
}
