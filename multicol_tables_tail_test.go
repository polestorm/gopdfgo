package gopdfgo

import (
	"fmt"
	"testing"
)

// Ogon kolumn i tabel: przypadki z sondażu testów W3C css-multicol i css-tables. Każdy przypadek
// porównuje układ z wzorcem z bloków pozycjonowanych.
func TestMulticolTablesTail(t *testing.T) {
	head := `<style>@page { size: 400px 300px; margin: 0 } body { margin: 0 }
		.outer { columns: 2; column-fill: auto; column-gap: 0; width: 200px; height: 200px }</style>`
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

	// Arkusz domyślny HTML nie zabrania podziału za nagłówkiem: kolumna kończy się za nagłówkiem
	// i jego dolnym marginesem, następny blok zaczyna drugą kolumnę.
	same("heading-break-after-allowed", `<div class="outer"><div style="height:140px;background:blue"></div>`+
		`<h4 style="margin:0 0 20px;height:20px;background:green"></h4><div style="height:60px;break-inside:avoid;background:blue"></div></div>`,
		block(0, 0, 100, 140, "blue")+block(0, 140, 100, 20, "green")+block(100, 0, 100, 60, "blue"))

	// Tabela z nagłówkiem, z której w pierwszej kolumnie zmieściłby się tylko nagłówek, przechodzi
	// w całości do drugiej kolumny; nagłówek nie traci powtarzania w próbach równoważenia.
	rows := ""
	for range 3 {
		rows += `<tr><td style="padding:0;height:20px"></td></tr>`
	}
	same("table-header-not-alone", `<div class="outer"><div style="height:170px;background:blue"></div>`+
		`<table style="border-spacing:0;width:100%"><thead><tr><th style="padding:0;height:20px;background:green"></th></tr></thead>`+
		`<tbody style="background:navy">`+rows+`</tbody></table></div>`,
		block(0, 0, 100, 170, "blue")+block(100, 0, 100, 20, "green")+block(100, 20, 100, 60, "navy"))

	// Górny margines przodka zwija się z górnym marginesem wyniesionego elementu rozpinającego, który
	// jest jego pierwszym dzieckiem, ale nie z dolnym marginesem poprzedniego elementu rozpinającego.
	spanner := `<div style="column-span:all;margin:20px 0;height:20px;background:black"></div>`
	same("hoisted-spanner-margins", `<div style="columns:2;column-gap:0;width:200px;background:yellow">`+
		`<div style="height:40px;background:navy"></div>`+spanner+`<div style="margin-top:20px">`+spanner+`</div>`+
		`<div style="height:40px;background:navy"></div></div>`,
		block(0, 0, 200, 160, "yellow")+block(0, 0, 200, 20, "navy")+block(0, 40, 200, 20, "black")+
			block(0, 100, 200, 20, "black")+block(0, 140, 200, 20, "navy"))

	// Element rozpinający zakłada nowy kontekst formatowania blokowego: margines jego dziecka nie
	// wychodzi poza niego.
	same("spanner-establishes-bfc", `<div style="columns:2;column-gap:0;width:200px;background:yellow">`+
		`<div style="column-span:all;background:black"><div style="margin:20px 0;height:20px;background:green"></div></div></div>`,
		block(0, 0, 200, 60, "black")+block(0, 20, 200, 20, "green"))

	// Komórka rozpięta na zwiniętym wierszu (visibility: collapse) kończy się na poprzednim wierszu, a jej
	// treść jest przycinana.
	same("rowspan-over-collapsed-row", `<table style="border-spacing:0">`+
		`<tr><td style="padding:0;width:50px;height:20px;background:navy"></td>`+
		`<td rowspan="2" style="padding:0;width:50px;vertical-align:top"><div style="height:100px;background:green"></div></td></tr>`+
		`<tr style="visibility:collapse"><td style="padding:0;height:20px"></td></tr>`+
		`<tr><td style="padding:0;height:20px;background:navy"></td><td style="padding:0;background:navy"></td></tr></table>`,
		block(0, 0, 50, 20, "navy")+block(50, 0, 50, 20, "green")+block(0, 20, 100, 20, "navy"))

	// Linia między kolumnami leży nad obramowaniem kontenera: szeroka linia zakrywa boczne obramowania.
	same("column-rule-over-border", `<div style="columns:2;column-gap:0;width:100px;margin-left:20px;border:10px solid gray;`+
		`column-rule:140px solid blue"><div style="height:40px"></div></div>`,
		block(20, 0, 120, 40, "gray")+block(10, 10, 140, 20, "blue"))

	// Pudełko pozycjonowane bezwzględnie dzieli się między kolumny i ciągnie od góry następnych kolumn.
	quad := `<div style="columns:4;column-fill:auto;column-gap:0;width:200px;height:100px"><div style="position:relative">`
	same("abspos-fragmented-in-columns", quad+`<div style="height:150px;background:navy"></div>`+
		`<div style="position:absolute;width:25px;height:200px;background:green"></div></div></div>`,
		block(0, 0, 50, 100, "navy")+block(50, 0, 50, 50, "navy")+block(50, 50, 25, 50, "green")+
			block(100, 0, 25, 100, "green")+block(150, 0, 25, 50, "green"))

	// Pudełko, którego pozycja statyczna wypada na samym dole kolumny, zaczyna się w następnej kolumnie.
	same("abspos-at-column-end", quad+`<div style="height:200px;background:navy"></div>`+
		`<div style="position:absolute;width:25px;height:150px;background:green"></div></div></div>`,
		block(0, 0, 100, 100, "navy")+block(100, 0, 25, 100, "green")+block(150, 0, 25, 50, "green"))

	// Znacznik listy umieszczony na zewnątrz stoi obok pierwszego wiersza elementu, a nie na jego górze.
	same("marker-beside-first-line", `<div style="display:list-item;margin-left:40px"><div style="height:30px"></div>tekst</div>`,
		`<div style="display:list-item;margin-left:40px;padding-top:30px">tekst</div>`)

	// Znacznik elementu, z którego w pierwszej kolumnie zmieścił się tylko pusty blok, przechodzi razem
	// z pierwszym wierszem do następnej kolumny.
	same("marker-follows-first-line", `<div style="columns:2;column-fill:auto;column-gap:0;width:300px;height:100px;line-height:50px;margin-left:40px">`+
		`<div style="height:80px"></div><div style="display:list-item"><div style="height:10px"></div>tekst</div></div>`,
		`<div style="columns:2;column-fill:auto;column-gap:0;width:300px;height:100px;line-height:50px;margin-left:40px">`+
			`<div style="height:100px"></div><div style="display:list-item">tekst</div></div>`)

	// Tabela pozycjonowana bezwzględnie między left/right i top/bottom ma własny rozmiar, a marginesy
	// automatyczne ją wyśrodkowują.
	same("abspos-table-auto-margins", `<div style="position:relative;width:160px;height:160px">`+
		`<div style="display:table;position:absolute;inset:0;margin:auto;width:100px;height:100px;background:green"></div></div>`,
		block(30, 30, 100, 100, "green"))

	// Kolumna z visibility: collapse nie zajmuje miejsca, a komórka rozpięta także na niej jest przycinana.
	cell := func(color string) string {
		return `<td style="padding:0;width:50px;height:20px;background:` + color + `"></td>`
	}
	same("collapsed-column", `<table style="border-spacing:0"><col><col style="visibility:collapse"><col>`+
		`<tr>`+cell("navy")+cell("red")+cell("green")+`</tr>`+
		`<tr><td colspan="3" style="padding:0;height:20px"><div style="width:150px;height:20px;background:blue"></div></td></tr></table>`,
		block(0, 0, 50, 20, "navy")+block(50, 0, 50, 20, "green")+block(0, 20, 100, 20, "blue"))
}
