package gopdfgo

import (
	"context"
	"os"
	"testing"
)

// TestTabFloat12 sprawdza poprawki z rundy tabel, elementów pływających i układu liniowego: grupa wierszy
// z break-inside: avoid przy podziale wymuszonym, procentowa wysokość elementu pływającego w anonimowym
// bloku, podział wymuszony z drugiego elementu pierwszej linii zagnieżdżonego kontenera flex,
// dziedziczenie po ::first-line w elemencie pływającym, ujemny text-indent w szerokości max-content
// i linie między kolumnami pudełka liniowo-blokowego.
func TestTabFloat12(t *testing.T) {
	ahem, err := os.ReadFile("testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	dir := t.TempDir()
	render := func(name, css, body string) [][]byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(`<!DOCTYPE html><style>`+css+`</style>`+body), Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		return rasterize(t, result.PDF, dir, name)
	}
	same := func(name, css, test, reference string) {
		t.Helper()
		got, want := render(name, css, test), render(name+"-ref", css, reference)
		if len(got) != len(want) {
			t.Errorf("%s: %d stron, wzorzec %d", name, len(got), len(want))
			return
		}
		for i := range got {
			if n := differentPixels(got[i], want[i]); n != 0 {
				t.Errorf("%s: strona %d: %d pikseli różni się od wzorca", name, i+1, n)
			}
		}
	}
	const pageCSS = `@page { size: 200px 100px; margin: 0 } html, body { margin: 0 } body { font: 20px/1 Ahem }
		.g { background: green }`

	// podział wymuszony między wierszami grupy z break-inside: avoid jest dozwolony (avoid dotyczy tylko
	// podziałów niewymuszonych), więc grupa zostaje na stronie 1 za tekstem, a nie przechodzi na stronę 2
	same("rowgroup-avoid-forced-break", pageCSS, `X<table style="border-spacing:0"><tbody style="break-inside:avoid">
		<tr><td style="padding:0"><div class=g style="width:40px;height:30px"></div></td></tr>
		<tr style="break-before:page"><td style="padding:0"><div class=g style="width:40px;height:90px"></div></td></tr></tbody></table>`,
		`X<table style="border-spacing:0"><tbody>
		<tr><td style="padding:0"><div class=g style="width:40px;height:30px"></div></td></tr>
		<tr style="break-before:page"><td style="padding:0"><div class=g style="width:40px;height:90px"></div></td></tr></tbody></table>`)

	// element pływający z height: 50% w anonimowym bloku (tekst przed nim, blok po nim) liczy procent od
	// body (100 px), nie od anonimowego bloku o wysokości auto (CSS 2 § 9.2.1.1)
	same("float-percent-in-anonymous-block", pageCSS+` html, body { height: 100% }`,
		`<span style="color:green">X</span><div class=g style="float:left;width:20px;height:50%"></div><div style="clear:both"></div>`,
		`<div class=g style="position:absolute;top:0;left:0;width:20px;height:50px"></div>
		<div class=g style="position:absolute;top:0;left:20px;width:20px;height:20px"></div>`)

	// break-before na drugim elemencie pierwszej linii zagnieżdżonego kontenera flex przechodzi na ten
	// kontener (CSS Flexbox 1 § 12): drugi kontener zaczyna stronę 2, tak jak przy break-before na
	// pierwszym elemencie linii
	flexCSS := pageCSS + ` .f { display: flex; flex-wrap: wrap } .f > .f { width: 100% } .f > .f > div { width: 50%; height: 30px }`
	same("flex-nested-line-break-before", flexCSS,
		`<div class=f><div class=f><div class=g></div><div class=g></div></div>
		<div class=f><div class=g></div><div class=g style="break-before:page"></div></div></div>`,
		`<div class=f><div class=f><div class=g></div><div class=g></div></div>
		<div class=f><div class=g style="break-before:page"></div><div class=g></div></div></div>`)
	same("flex-nested-line-break-after", flexCSS,
		`<div class=f><div class=f><div class=g></div><div class=g></div><div class=g style="break-after:page"></div><div class=g></div></div>
		<div class=f><div class=g></div><div class=g></div></div></div>`,
		`<div class=f><div class=f><div class=g></div><div class=g></div><div class=g></div><div class=g style="break-after:page"></div></div>
		<div class=f><div class=g></div><div class=g></div></div></div>`)

	// element pływający w pudełku liniowym pierwszego wiersza dziedziczy po nim, czyli po ::first-line;
	// stojący wprost w bloku dziedziczy po bloku
	same("first-line-float-in-span", pageCSS+` div { color: red } div::first-line { color: green }`,
		`<div><span><span style="float:left">XX</span></span></div>`,
		`<span style="float:left;color:green">XX</span>`)
	same("first-line-float-in-block", pageCSS+` div { color: green } div::first-line { color: red }`,
		`<div><span style="float:left">XX</span></div>`,
		`<span style="float:left;color:green">XX</span>`)

	// przy ujemnym wcięciu szerokość max-content nie jest mniejsza od min-content: element pływający
	// z dwoma pudełkami 20 px rozdzielonymi ZWSP ma szerokość 20 px, nie zero (oba pudełka mieszczą się
	// wtedy w pierwszym wierszu, wysuniętym o 60 px w lewo poza stronę)
	same("negative-indent-shrink-to-fit", pageCSS+` span { display: inline-block; width: 20px; height: 20px; background: green }`,
		`<div style="width:1px"><div style="float:left;text-indent:-60px;height:40px;background:blue"><span></span>&#8203;<span></span></div></div>`,
		`<div style="position:absolute;top:0;left:0;width:20px;height:40px;background:blue"></div>`)

	// linie między kolumnami pudełka liniowo-blokowego (kolumny w anonimowym bloku, zob.
	// boxes.MulticolWrappers) są rysowane jak dla zwykłego bloku
	columnsCSS := pageCSS + ` .m { columns: 2; column-gap: 20px; column-rule: 4px solid green; width: 100px }`
	same("inline-block-column-rule", columnsCSS,
		`<span class=m style="display:inline-block;vertical-align:top">X X X X</span>`,
		`<span style="display:inline-block;vertical-align:top"><div class=m>X X X X</div></span>`)
}
