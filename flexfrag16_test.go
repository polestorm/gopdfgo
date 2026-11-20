package gopdfgo

import (
	"fmt"
	"strings"
	"testing"
)

// Podział wielowierszowego kontenera flex (row, wrap) między strony (CSS Flexbox 1 § 12):
//   - górny margines elementu linii, która zaczyna się w środku strony, nie przylega do podziału, więc
//     nie jest obcinany (CSS Fragmentation 3 § 5.2); gdy margines sięga za stronę, element przechodzi na
//     następną i stoi tak daleko od jej góry, jak daleko za podział sięgał (test WPT
//     css-break/flexbox/multi-line-row-flex-fragmentation-064-print);
//   - linia, która w całości zaczyna się poniżej strony, przechodzi na następną cała, a row-gap przed nią
//     jest obcinany na podziale (CSS Align 3 § 8.1).
//
// Wzorce to bloki ustawione bezwzględnie na kolejnych stronach.
func TestFlexRowFragmentationMargins(t *testing.T) {
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
		.flex { display: flex; flex-wrap: wrap } .flex > div { flex: 0 0 50%; height: 30px; background: green }
		.mark { display: inline-block; vertical-align: top; width: 10px; height: 10px; background: black }</style>`
	mark := `<span class="mark"></span>`

	// Linia 3 zaczyna się na stronie 2 pod przeniesionym elementem 2-2 (y 30). Element 3-1 ma górny
	// margines 80px, więc jego obramowanie wypadłoby na y 110, 10px za stroną: przechodzi na stronę 3
	// (y 10). Wcześniej tracił margines (pierwszy wiersz za wysoki na „pustą” stronę) i stał obok 3-2.
	same("flex-later-line-margin",
		page+`<div class="flex">`+
			`<div style="flex-basis:100%;height:60px">`+mark+`</div>`+
			`<div>`+mark+`</div><div style="margin-top:20px">`+mark+`</div>`+
			`<div style="margin-top:80px">`+mark+`</div><div>`+mark+`</div></div>`,
		pages(page,
			box(0, 0, 200, 60, "green")+box(0, 0, 10, 10, "black")+box(0, 60, 100, 30, "green")+box(0, 60, 10, 10, "black"),
			box(100, 0, 100, 30, "green")+box(100, 0, 10, 10, "black")+box(100, 30, 100, 30, "green")+box(100, 30, 10, 10, "black"),
			box(0, 10, 100, 30, "green")+box(0, 10, 10, 10, "black")))

	// Pas 5px nad kontenerem, linie po 40px z row-gap 8px: trzecia zaczynałaby się na y 101, w całości
	// poniżej strony. Przechodzi na stronę 2 i stoi u jej góry; wcześniej stała 1px niżej (reszta odstępu
	// za podziałem).
	same("flex-line-below-page-gap",
		page+`<style>.gap { row-gap: 8px } .gap > div { flex-basis: 100%; height: 40px }</style>`+
			`<div style="height:5px;background:blue"></div>`+
			`<div class="flex gap"><div>`+mark+`</div><div>`+mark+`</div><div>`+mark+`</div></div>`,
		pages(page,
			box(0, 0, 200, 5, "blue")+box(0, 5, 200, 40, "green")+box(0, 5, 10, 10, "black")+
				box(0, 53, 200, 40, "green")+box(0, 53, 10, 10, "black"),
			box(0, 0, 200, 40, "green")+box(0, 0, 10, 10, "black")))
}

// Kolumna flex, której szerokość wyznacza kontener nadrzędny (flex-basis elementu wiersza) albo procent
// (width: 50%), łamie wiersze swoich elementów w tej szerokości: wysokości elementów odpowiadają liczbie
// ich wierszy i elementy na siebie nie nachodzą. Wzorcem jest zwykły blok o tej samej szerokości.
// Wcześniej elementy były mierzone w szerokości bloku zawierającego (flex-basis) albo w linii o szerokości
// równej liczbie procentów w pikselach (width: 50% dawało 50px).
func TestFlexColumnItemWidth(t *testing.T) {
	dir := t.TempDir()
	page := `<style>@page { size: 300px 300px; margin: 0 } body { margin: 0; font: 16px serif } p { margin: 0 }
		.col { display: flex; flex-direction: column; background: yellow } .blk { width: 150px; background: yellow }</style>`
	text := `<p>aaa bbb ccc ddd eee fff ggg hhh</p><p>iii jjj kkk lll mmm nnn ooo</p><p>end</p>`
	want := rasterize(t, renderString(t, page+`<div class="blk">`+text+`</div>`).PDF, dir, "ref")
	for name, test := range map[string]string{
		"flex-basis":   `<div style="display:flex"><div class="col" style="flex:0 0 150px">` + text + `</div></div>`,
		"flex-percent": `<div style="display:flex"><div class="col" style="flex:0 0 50%">` + text + `</div></div>`,
		"width-50":     `<div class="col" style="width:50%">` + text + `</div>`,
		"row-width-50": `<div style="display:flex"><div class="col" style="width:50%">` + text + `</div></div>`,
	} {
		got := rasterize(t, renderString(t, page+test).PDF, dir, name)
		if len(got) != len(want) {
			t.Errorf("%s: %d stron, wzorzec ma %d", name, len(got), len(want))
			continue
		}
		if n := differentPixels(got[0], want[0]); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
}
