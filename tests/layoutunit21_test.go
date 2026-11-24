package tests

import (
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
)

// TestLayoutUnits pilnuje zaokrągleń układu jak w Chromium (ADR 0013, pomiary Chrome 157 z 2026-11-21):
// długości pudełek i wysokość wiersza obcinane do 1/64 px (LayoutUnit w Blink), interlinia nad tekstem
// w dół do całego piksela, górna krawędź obszaru strony do najbliższego piksela. ExactLengths zostawia
// wartości z arkusza. Do tego dwie poprawki niezależne od przełącznika, które wyszły na tych samych
// dokumentach: nadwyżka szerokości tabeli o układzie stałym rozdzielana proporcjonalnie do szerokości
// kolumn (CSS Tables 3 § 3.9.4) i kontener flex, z którego nic nie mieści się na stronie, przenoszony cały.
func TestLayoutUnits(t *testing.T) {
	dir := t.TempDir()
	render := func(source string, exact bool) [][]byte {
		t.Helper()
		result, err := htmlpdf.Render(t.Context(), []byte(source), htmlpdf.Options{Fonts: testFonts(t), ExactLengths: exact})
		if err != nil {
			t.Fatal(err)
		}
		return rasterize(t, result.PDF, dir, "page")
	}
	check := func(name, tested, expected string, exact, same bool) {
		t.Helper()
		got, want := render(tested, exact), render(expected, exact)
		equal := len(got) == len(want)
		for i := 0; equal && i < len(got); i++ {
			equal = differentPixels(got[i], want[i]) == 0
		}
		if equal != same {
			t.Errorf("%s (ExactLengths %v): zgodność ze wzorcem %v, oczekiwano %v", name, exact, equal, same)
		}
	}
	const page = `<style>@page { size: 200px 1000px; margin: 0 } body { margin: 0 }</style>`
	block := func(height string) string {
		return page + `<div style="width:50px;height:` + height + `;background:black"></div>`
	}

	// 64 bloki z padding 2.2px: 64 × (10 + 2 × 2,1875) = 920 px zamiast 921,6 px.
	rows := page + `<div style="width:50px;background:black">` +
		strings.Repeat(`<div style="padding:2.2px 0;height:10px"></div>`, 64) + `</div>`
	check("padding", rows, block("920px"), false, true)
	check("padding", rows, block("921.6px"), true, true)

	// 64 wiersze z line-height 1.3 przy 8.9375px: 64 × 11,609375 = 743 px zamiast 743,6 px.
	lines := page + `<div style="width:50px;background:black;color:transparent;font-size:8.9375px;line-height:1.3">` +
		strings.Repeat(`<div>x</div>`, 64) + `</div>`
	check("line-height", lines, block("743px"), false, true)
	check("line-height", lines, block("743.6px"), true, true)

	// Interlinia: font 10px ma wydłużenia 11 i 3 px, line-height 15px zostawia 1 px; nad tekstem idzie 0 px
	// (w dół do całego piksela), więc linia bazowa stoi na 11 px, a nie na 11,5 px.
	baseline := page + `<div style="font-size:10px;line-height:15px"><span style="display:inline-block;width:4px;height:4px;background:black"></span></div>`
	at := func(top string) string {
		return page + `<div style="position:absolute;left:0;top:` + top + `;width:4px;height:4px;background:black"></div>`
	}
	check("interlinia", baseline, at("7px"), false, true)
	check("interlinia", baseline, at("7.5px"), true, true)

	// Górna krawędź obszaru strony: margines 60.4px działa jak 60px, a 60.6px jak 61px. Obramowanie zamiast
	// tła, bo tło jest przycinane ścieżką, którą rasteryzator wyrównuje do pikseli.
	top := func(margin string) string {
		return `<style>@page { size: 200px 300px; margin: ` + margin + ` 0 0 } body { margin: 0 }</style>` +
			`<div style="width:50px;border-top:20px solid black"></div>`
	}
	check("górna krawędź strony", top("60.4px"), top("60px"), false, true)
	check("górna krawędź strony", top("60.6px"), top("61px"), false, true)
	check("górna krawędź strony", top("60.4px"), top("60px"), true, false)

	// Tabela o układzie stałym: kolumny 50 i 100 px w tabeli 300 px dostają nadwyżkę proporcjonalnie
	// (100 i 200 px), a nie po równo (125 i 175 px).
	wide := `<style>@page { size: 400px 100px; margin: 0 } body { margin: 0 }</style>`
	table := wide + `<table style="table-layout:fixed;width:300px;border-spacing:0"><tr>` +
		`<td style="width:50px;height:20px;padding:0;background:black"></td><td style="width:100px;padding:0"></td></tr></table>`
	for _, exact := range []bool{false, true} {
		check("tabela stała", table, wide+`<div style="width:100px;height:20px;background:black"></div>`, exact, true)
	}

	// Kontener flex, którego pierwszy element nie mieści się na stronie, przechodzi cały: na stronie 1
	// nie zostaje samo górne dopełnienie z tłem.
	short := `<style>@page { size: 200px 100px; margin: 0 } body { margin: 0 }</style>`
	flex := short + `<div style="height:90px"></div><div style="display:flex;padding:4px;background:black">` +
		`<div style="height:20px;width:20px"></div></div>`
	pushed := short + `<div style="height:90px"></div><div style="break-before:page;height:28px;background:black"></div>`
	check("kontener flex", flex, pushed, false, true)
}
