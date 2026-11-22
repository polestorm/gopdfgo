package tests

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Błędy znalezione na dokumentach biurowych (Word, OpenOffice) przekonwertowanych do HTML narzędziem
// textutil i na losowych dokumentach z generatora (bench/realdocs, 2026-11-22; docs/feature-audit.md).

// wordTops zwraca górną krawędź (punkty) pierwszego wystąpienia każdego słowa w PDF.
func wordTops(t *testing.T, pdf []byte) map[string]float64 {
	t.Helper()
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("brak pdftotext")
	}
	file := filepath.Join(t.TempDir(), "out.pdf")
	if err := os.WriteFile(file, pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("pdftotext", "-bbox", file, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	tops := map[string]float64{}
	for _, line := range strings.Split(string(out), "\n") {
		if m := wordBox.FindStringSubmatch(line); m != nil {
			if _, seen := tops[m[3]]; !seen {
				tops[m[3]], _ = strconv.ParseFloat(m[1], 64)
			}
		}
	}
	return tops
}

// Spacja z własnym letter-spacing na początku pudełka w komórce tabeli: przy liczeniu szerokości
// min-content wiersz z samej zdjętej spacji miał pusty tekst i silnik kończył się paniką (indeks -1).
func TestLetterSpacedSpaceInMinContent(t *testing.T) {
	for _, html := range []string{
		`<table><tr><td>a<span style="letter-spacing:-0.4px"> b</span>,c</td></tr></table>`,
		`<div style="width:min-content">a<span style="letter-spacing:-1px"> b</span>,<span style="letter-spacing:2px"> c</span></div>`,
	} {
		text := string(extractText(t, renderString(t, `<!DOCTYPE html>`+html).PDF))
		for _, word := range []string{"a", "b,", "c"} {
			if !strings.Contains(text, word) {
				t.Errorf("%s: brak %q w %q", html, word, text)
			}
		}
	}
}

// min-height komórki tabeli nie wpływa na wysokość wiersza (CSS 2.2 § 10.7: skutek nieokreślony;
// Chromium pomija, tak jak min-height wiersza i max-height komórki). textutil daje każdej komórce
// min-height z wysokości wiersza w Wordzie.
func TestTableCellMinHeightIgnored(t *testing.T) {
	doc := func(cell string) string {
		return `<!DOCTYPE html><style>td { border: 1px solid }</style><table><tr><td style="` + cell +
			`">A</td></tr></table><table><tr><td style="display:block; min-height:60px">C</td></tr></table><p>B</p>`
	}
	with := wordTops(t, renderString(t, doc("min-height:60px")).PDF)
	without := wordTops(t, renderString(t, doc("")).PDF)
	if math.Abs(with["B"]-without["B"]) > 0.01 {
		t.Errorf("akapit pod tabelą na %.2f pt, bez min-height komórki %.2f pt", with["B"], without["B"])
	}
	// komórka zamieniona w blok (display: block) zachowuje min-height
	if with["B"]-with["C"] < 45 {
		t.Errorf("blok z min-height 60px zajmuje tylko %.2f pt", with["B"]-with["C"])
	}
}

// vertical-align: sub i super przesuwają linię bazową o jedną piątą i jedną trzecią rozmiaru pisma
// rodzica (CSS Inline 3 § 4.2.3, wartości domyślne; ADR 0042). Wcześniej ±0,5 em własnego rozmiaru.
func TestSubSuperDefaultOffsets(t *testing.T) {
	html := `<!DOCTYPE html><style>p { font: 30px Tinos; margin: 0 } sub, sup { font-size: inherit }
		.small sub, .small sup { font-size: smaller }</style>
		<p>A <sub>B</sub> C <sup>D</sup></p><p class="small">E <sub>F</sub> G <sup>H</sup></p>`
	tops := wordTops(t, renderString(t, html).PDF)
	for _, c := range []struct {
		word, base string
		want       float64 // przesunięcie w punktach, dodatnie w dół
	}{
		{"B", "A", 30.0 / 5 * 0.75},
		{"D", "C", -30.0 / 3 * 0.75},
	} {
		if got := tops[c.word] - tops[c.base]; math.Abs(got-c.want) > 0.05 {
			t.Errorf("%s względem %s: %.2f pt, oczekiwane %.2f pt", c.word, c.base, got, c.want)
		}
	}
	// mniejsze pismo: przesunięcie nadal od rozmiaru rodzica, więc linia bazowa F jest 6 px niżej
	// (porównujemy dół słów, który dla tej samej rodziny zależy od linii bazowej i rozmiaru)
	if tops["F"] <= tops["E"] {
		t.Errorf("indeks dolny F (%.2f) nie jest niżej niż E (%.2f)", tops["F"], tops["E"])
	}
}

// Kontener wielokolumnowy za elementem pływającym z tabelą szerszą niż on: próba ułożenia kolumn usuwała
// element pływający z listy wykluczeń, lista była krótsza niż przed kolumnami i przycięcie kończyło się
// paniką (dokument losowy rand-23, bench/realdocs).
func TestColumnsAfterFloatWithWideTable(t *testing.T) {
	html := `<!DOCTYPE html><style>@page { size: letter; margin: 15mm 18mm } html { font: 11pt/1.5 serif }
		body { margin: 0 } td { border: 1px solid; padding: 2px 4px }</style>
		<div style="float: left; width: 39%; padding: 4px"><table style="width: 100%"><tr style="break-inside: avoid">
		<td>analiza dokument</td><td>Brutto udział bank liczba</td><td>Także norma tabela etap lub tabela liczba
		załącznik udział kolumna uchwała koszty pomiar</td><td>pasywa oraz i zarząd dla zgodność od numer</td>
		<td>Spółka miesiąc</td></tr></table></div>
		<div>Protokół był numer analiza</div>
		<div style="column-count: 2"><p>Dokument w spadek zgodność suma które procent umowa spadek.</p></div>`
	text := string(extractText(t, renderString(t, html).PDF))
	for _, word := range []string{"Protokół", "Dokument", "Spółka"} {
		if !strings.Contains(text, word) {
			t.Errorf("brak %q w %q", word, text)
		}
	}
}

// Długa tabela, która nie mieści się obok elementu pływającego, zaczyna się pod nim na tej samej stronie
// i dzieli się na następne (CSS 2.1 § 9.5); wcześniej była przesuwana w dół po ułożeniu i jako
// wystająca poza stronę trafiała w całości na następną, zostawiając resztę strony pustą.
func TestLongTableBelowFloatSplits(t *testing.T) {
	var rows strings.Builder
	for i := range 80 {
		rows.WriteString("<tr><td>R" + strconv.Itoa(i) + "</td><td>x</td></tr>")
	}
	for _, layout := range []string{"auto", "fixed"} {
		html := `<!DOCTYPE html><style>@page { size: 600px 800px; margin: 0 } body { margin: 0; font: 12px "Noto Sans" }
			td { border: 1px solid }</style>
			<div style="float: right; width: 30%; height: 100px">F</div><p>P</p>
			<table style="width: 100%; table-layout: ` + layout + `"><tr><td>T1</td><td>T2</td></tr>` + rows.String() + `</table>`
		result := renderString(t, html)
		lines := textLines(t, result.PDF)
		// pierwsza strona: P, F, potem tabela od razu pod elementem pływającym
		if len(lines) < 3 || !strings.HasPrefix(lines[2], "T1") {
			t.Errorf("table-layout: %s: tabela nie zaczyna się na pierwszej stronie pod elementem pływającym: %q", layout, lines[:min(4, len(lines))])
		}
		if top := wordTops(t, result.PDF)["T1"]; top < 75 {
			t.Errorf("table-layout: %s: tabela na %.2f pt nachodzi na element pływający (do 75 pt)", layout, top)
		}
	}
}

// Element listy, w którym poza znacznikiem są tylko puste pudełka wierszowe („<li><b></b></li>”
// z Worda), ma wiersz dla znacznika jak pusty element listy (Chromium tak samo); wcześniej wiersz
// był fantomowy, a lista nie miała wysokości.
func TestListItemWithEmptyInlinesKeepsMarkerLine(t *testing.T) {
	doc := func(item string) string {
		return `<!DOCTYPE html><style>ul { margin: 0 } p { margin: 0 }</style><p>A</p><ul><li>` + item + `</li></ul><p>B</p>`
	}
	reference := wordTops(t, renderString(t, doc("")).PDF)
	for _, item := range []string{"<b></b>", "<span> </span> <i></i>"} {
		tops := wordTops(t, renderString(t, doc(item)).PDF)
		if math.Abs(tops["B"]-reference["B"]) > 0.01 {
			t.Errorf("%q: akapit za listą na %.2f pt, za pustym elementem listy %.2f pt", item, tops["B"], reference["B"])
		}
	}
	// pudełko z odstępem w poziomie nie jest puste: wiersz powstaje z niego samego, bez zmiany wysokości
	tops := wordTops(t, renderString(t, doc(`<span style="padding-left:4px"></span>`)).PDF)
	if math.Abs(tops["B"]-reference["B"]) > 0.01 {
		t.Errorf("pudełko z odstępem: akapit za listą na %.2f pt, oczekiwane %.2f pt", tops["B"], reference["B"])
	}
}
