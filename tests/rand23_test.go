package tests

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/polestorm/gopdfgo/htmlpdf"
)

// Błędy układu znalezione na losowych dokumentach z generatora (bench/realdocs/genrand, 2026-11-23;
// docs/feature-audit.md, sekcja „Dokumenty losowe: rozbiór rozbieżności”).

// renderAllFonts renderuje dokument ze wszystkimi fontami z testdata/fonts (jak program gopdfgo z -fonts).
func renderAllFonts(t *testing.T, html string) []byte {
	t.Helper()
	fonts, families, err := corpusFonts()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := htmlpdf.Render(ctx, []byte(html), htmlpdf.Options{Fonts: fonts, GenericFamilies: families})
	if err != nil {
		t.Fatal(err)
	}
	return result.PDF
}

// wordPlace to strona (od 1) i lewy górny róg słowa w punktach.
type wordPlace struct {
	page int
	x, y float64
}

var bboxWord = regexp.MustCompile(`xMin="([\d.-]+)" yMin="([\d.-]+)" xMax="[\d.-]+" yMax="[\d.-]+">(.*)</word>`)

// wordPlaces zwraca położenie pierwszego wystąpienia każdego słowa w PDF.
func wordPlaces(t *testing.T, pdf []byte) map[string]wordPlace {
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
	places := map[string]wordPlace{}
	page := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "<page ") {
			page++
		}
		if m := bboxWord.FindStringSubmatch(line); m != nil {
			if _, seen := places[m[3]]; !seen {
				x, _ := strconv.ParseFloat(m[1], 64)
				y, _ := strconv.ParseFloat(m[2], 64)
				places[m[3]] = wordPlace{page, x, y}
			}
		}
	}
	return places
}

// Kontener wielokolumnowy zakłada własny kontekst formatowania (CSS Multicol 1 § 2), więc obok elementu
// pływającego zwęża się i staje obok niego (CSS 2.1 § 9.5), jak w Chromium; wcześniej zajmował całą
// szerokość i nachodził na element pływający (dokument losowy rand-23). Element rozpinający kolumny
// omija element pływający razem z kontenerem, a nie drugi raz.
func TestMulticolBesideFloat(t *testing.T) {
	html := `<!DOCTYPE html><style>@page { size: 400px 400px; margin: 0 } body { margin: 0; font: 10px/12px Tinos }</style>
		<div style="float: left; width: 100px; height: 100px">F</div>
		<div style="column-count: 2; column-gap: 20px">AAA BBB CCC DDD EEE FFF GGG HHH III JJJ KKK LLL a1 a2 a3 a4 a5 a6 a7 a8 a9 b1 b2 b3 b4 b5 b6 b7 b8 b9 c1 c2 c3 c4 c5 c6 c7 c8 c9 d1 d2 d3 d4 d5 d6 d7 d8 d9
		<div style="column-span: all">SPAN</div>MMM NNN OOO PPP QQQ RRR SSS TTT UUU VVV WWW XXX YYY ZZZ</div>`
	places := wordPlaces(t, renderAllFonts(t, html))
	// kontener od x = 100px (75 pt), szerokość 300px: kolumny po 140px, druga od 260px (195 pt)
	for _, word := range []string{"AAA", "SPAN", "MMM"} {
		if x := places[word].x; math.Abs(x-75) > 1.5 {
			t.Errorf("%s na x = %.2f pt, oczekiwane 75 pt (obok elementu pływającego)", word, x)
		}
	}
	maxX := 0.0
	for word, place := range places {
		if word != "F" {
			maxX = max(maxX, place.x)
		}
	}
	if maxX < 190 || maxX > 290 {
		t.Errorf("druga kolumna kończy się na x = %.2f pt, oczekiwane od 195 pt", maxX)
	}
}

// Element pływający za nagłówkiem z break-after: avoid, który przechodzi na następną stronę razem
// z nagłówkiem, staje tam pod nagłówkiem (CSS 2.1 § 9.5.1, reguła 5). Wcześniej zgłoszenie przeniesienia
// elementu z pierwszej próby zostawało i element stawał na samej górze strony, obok nagłówka.
func TestFloatAfterAvoidBreakHeadingStaysBelowIt(t *testing.T) {
	html := `<!DOCTYPE html><style>@page { size: 400px 200px; margin: 0 } body { margin: 0; font: 10px/20px Tinos }
		h2 { break-after: avoid; margin: 10px 0 5px; font-size: 10px } p { margin: 0 }</style>
		<div style="height: 150px">filler</div><h2>Head</h2>
		<div style="float: right; width: 100px">FLOAT</div><p>AAA BBB</p>`
	places := wordPlaces(t, renderAllFonts(t, html))
	head, float, text := places["Head"], places["FLOAT"], places["AAA"]
	if head.page != 2 || float.page != 2 || text.page != 2 {
		t.Fatalf("strony: nagłówek %d, element pływający %d, akapit %d; oczekiwane 2", head.page, float.page, text.page)
	}
	if math.Abs(float.y-text.y) > 1.5 || float.y < head.y+10 {
		t.Errorf("element pływający na y = %.2f pt, akapit %.2f pt, nagłówek %.2f pt", float.y, text.y, head.y)
	}
}

// Komórka tabeli przerwana przy podziale strony (abort w blockContainerLayout) nie zamykała swojego kontekstu
// formatowania; lista wykluczeń zostawała otwarta i następne konteksty zamykały cudzą listę. Skutek
// (dokument losowy rand-27, zminimalizowany): tabela za elementem pływającym z tabelą nachodziła na niego,
// bo element pływający znikał z listy, którą widziała. Według CSS 2.1 § 9.5 tabela staje pod nim.
func TestBrokenCellClosesFormattingContext(t *testing.T) {
	html := `<!DOCTYPE html><head><style>@page{size:A4;margin:2cm;@top-center{content:"Także zarząd na test";font:8pt "Noto Sans";color:#555}} html{font:9pt/normal "Noto Sans";color:#111} body{margin:0} h1,h2,h3{break-after:avoid;margin:.9em 0 .4em} p{margin:0 0 .6em} td,th{border:1px solid #999;padding:2px 4px;vertical-align:top} th{background:#eee} thead{display:table-header-group} code,pre{font-family:"Noto Sans Mono";font-size:.9em} pre{background:#f6f6f6;padding:4px;white-space:pre-wrap} </style></head>
		<body> <div style="float:right;width:28%;margin:0 0 6px 8px;padding:4px;border:1px solid #888;background:#f4f4f4"><table style="border-collapse:collapse;width:100%"> 
		<tr><td> </td><td> </td><td> </td><td rowspan="4"> </td><td>Po </td><td> </td></tr> </tbody></table></div>
		<p style="widows:4;orphans:2"> </p><p> płatności.</p> 
		<table style="border-spacing:1px;width:100%;table-layout:fixed"> <thead>
		<tr><th>Dla</th><th> </th><th> </th><th> </th><th> </th><th> </th><th> </th></tr></thead> 
		<tr><td></td><td> </td><td> </td><td> </td><td> </td><td></td><td rowspan="2"> </td></tr> 
		<tr><td> </td><td></td><td colspan="4"></td></tr> 
		<tr><td> </td><td> </td><td colspan="4"></td><td> </td></tr> 
		<tr><td rowspan="3"> </td><td></td><td> </td><td> </td><td colspan="3"> </td></tr> 
		<tr><td></td><td> jednak pasywa </td><td></td><td colspan="2"> </td><td> </td></tr> 
		<tr><td rowspan="4"> </td><td></td><td> wiersz załącznik zobowiązania spółka miesiąc wspólnik wiersz inwestycje jakość od lub po</td><td> </td><td> </td><td> </td></tr> 
		<tr><td> </td><td> </td><td> </td><td></td><td rowspan="2"> </td><td> </td></tr> 
		<tr><td> </td><td> </td><td> <b>aneks</b> rada udział kontrola faktura numer sprawozdanie <b>podatek</b> która lub lub a</td><td> </td><td> </td></tr> 
		<tr><td></td><td> </td><td>Ryzyko rok oraz nadzorcza próba wspólnik ryzyko kwartał płatności okres i <code>kapitał</code></td><td colspan="3"></td></tr> 
		<tr><td> </td><td> </td><td> </td><td>Kapitał uchwała kontrola kwartał zaliczka inwestycje <i>sprawozdanie</i> netto na pomiar</td><td> </td><td> </td><td> </td></tr> 
		<tr><td> była kwartał, data zysk uchwała stawka <i>na</i> audyt </td><td></td><td colspan="2"> </td><td rowspan="4"></td><td> </td><td> </td></tr> 
		<tr><td> </td><td></td><td></td><td> </td><td> </td><td> bank</td></tr> 
		<tr><td> </td><td> </td><td>Dokument numer należności </td><td> </td><td> </td><td> </td></tr></tbody></table>`
	places := wordPlaces(t, renderAllFonts(t, html))
	po, dla := places["Po"], places["Dla"]
	// element pływający kończy się kilka punktów pod słowem „Po”; tabela zaczyna się pod nim
	if dla.page != 1 || dla.y < po.y+20 {
		t.Errorf("nagłówek tabeli na y = %.2f pt (strona %d), element pływający ze słowem „Po” na %.2f pt", dla.y, dla.page, po.y)
	}
}

// Wiersz tabeli, który mieści się na stronie, zostaje na niej, choć odstęp border-spacing pod nim już się
// nie mieści: podział wypada w tym odstępie (tak drukuje Chromium; dokument losowy rand-09). Za ostatnim
// wierszem tabeli musi się jednak zmieścić jej dolne obramowanie.
func TestTableRowFitsWithoutTrailingSpacing(t *testing.T) {
	doc := func(filler int, table, rows string) string {
		return `<!DOCTYPE html><style>@page { size: 400px 200px; margin: 0 } body { margin: 0; font: 10px/20px Tinos }
			td { border: 1px solid; padding: 2px }</style><div style="height: ` + strconv.Itoa(filler) + `px">filler</div>
			<table style="border-spacing: 3px; ` + table + `">` + rows + `</table><p style="margin: 0">AFTER</p>`
	}
	// wiersz 26px; przy 140px wypełnienia drugi wiersz kończy się na 198px, a odstęp pod nim na 201px
	for _, c := range []struct {
		name, table, rows string
		filler            int
		pages             map[string]int
	}{
		{"środkowy wiersz", "", "<tr><td>R1</td></tr><tr><td>R2</td></tr><tr><td>R3</td></tr>", 140, map[string]int{"R2": 1, "R3": 2}},
		{"ostatni wiersz", "", "<tr><td>R1</td></tr><tr><td>R2</td></tr>", 142, map[string]int{"R2": 1, "AFTER": 2}},
		// z obramowaniem 5px: R2 kończy się na 197px, obramowanie tabeli na 205px
		{"ostatni wiersz z obramowaniem", "border: 5px solid", "<tr><td>R1</td></tr><tr><td>R2</td></tr>", 134, map[string]int{"R1": 1, "R2": 2}},
	} {
		places := wordPlaces(t, renderAllFonts(t, doc(c.filler, c.table, c.rows)))
		for word, page := range c.pages {
			if places[word].page != page {
				t.Errorf("%s: %s na stronie %d, oczekiwana %d", c.name, word, places[word].page, page)
			}
		}
	}
}

// Powtarzana stopka tabeli podzielonej między wierszami stoi o jeden odstęp border-spacing pod ostatnim
// wierszem fragmentu; wcześniej o dwa (odstęp za wierszem był już doliczony).
func TestRepeatedFooterSpacing(t *testing.T) {
	html := `<!DOCTYPE html><style>@page { size: 400px 200px; margin: 0 } body { margin: 0; font: 10px/20px Tinos }
		td { border: 1px solid; padding: 2px }</style><div style="height: 70px">filler</div>
		<table style="border-spacing: 4px"><tfoot><tr><td>FOOT</td></tr></tfoot><tbody>` +
		strings.Repeat("<tr><td>ROW</td></tr>", 6) + `</tbody></table>`
	places := wordPlaces(t, renderAllFonts(t, html))
	row, foot := places["ROW"], places["FOOT"]
	// wiersze co 26px + 4px odstępu od 74px: mieszczą się trzy (do 160px, stopka 164–190px), więc stopka
	// stoi 90px (67,5 pt) pod pierwszym wierszem; z podwójnym odstępem byłoby 94px (Chromium: 90px)
	if foot.page != 1 || math.Abs(foot.y-row.y-67.5) > 0.5 {
		t.Errorf("stopka na y = %.2f pt (strona %d), pierwszy wiersz na %.2f pt; oczekiwane 67,5 pt różnicy", foot.y, foot.page, row.y)
	}
}

// Tabela o szerokości auto zwężona obok elementu pływającego i dzielona między strony zachowuje na następnej
// stronie szerokości kolumn z pierwszego fragmentu (CSS Tables 3 § 3.9: szerokości kolumn wyznacza się dla
// całej tabeli); wcześniej dalszy ciąg, już bez elementu pływającego obok, rozszerzał się na całą szerokość.
// Pierwszy fragment staje obok elementu pływającego także wtedy, gdy tabela się dzieli (dokument losowy rand-30).
func TestNarrowedTableKeepsWidthOnNextPage(t *testing.T) {
	html := `<!DOCTYPE html><style>@page { size: 400px 300px; margin: 0 } body { margin: 0; font: 10px/20px Tinos }
		td, th { border: 1px solid } thead { display: table-header-group }</style>
		<div style="float: right; width: 100px; height: 250px">F</div><p style="margin: 0">P</p>
		<table style="border-collapse: collapse"><thead><tr><th>HEAD</th><th>SECOND</th></tr></thead>
		<tr><td>A</td><td>aaa bbb ccc ddd eee fff ggg hhh iii jjj kkk lll mmm nnn ooo ppp qqq rrr sss ttt uuu vvv www xxx</td></tr>` +
		strings.Repeat("<tr><td>x</td><td>y</td></tr>", 20) + `</table>`
	pdf := renderAllFonts(t, html)
	places := wordPlaces(t, pdf)
	// element pływający sięga 250px (187,5 pt); pod nim tabela zaczynałaby się niżej
	if places["A"].page != 1 || places["A"].y > 150 {
		t.Fatalf("tabela nie zaczyna się obok elementu pływającego: A na stronie %d, y = %.2f pt", places["A"].page, places["A"].y)
	}
	// nagłówek powtórzony na stronie 2 stoi w tych samych kolumnach co na stronie 1
	var second []wordPlace
	page := 0
	for _, line := range strings.Split(string(bboxLines(t, pdf)), "\n") {
		if strings.Contains(line, "<page ") {
			page++
		}
		if m := bboxWord.FindStringSubmatch(line); m != nil && m[3] == "SECOND" {
			x, _ := strconv.ParseFloat(m[1], 64)
			second = append(second, wordPlace{page, x, 0})
		}
	}
	// Chromium: druga kolumna od 108 pt (tabela kończy się przed elementem pływającym od 225 pt); wcześniej
	// tabela miała pełną szerokość i nachodziła na element pływający (druga kolumna od 146 pt)
	if len(second) < 2 || second[1].page != 2 || math.Abs(second[0].x-second[1].x) > 0.5 || math.Abs(second[0].x-108) > 2 {
		t.Errorf("nagłówek SECOND: %v; oczekiwany na stronach 1 i 2 w tym samym miejscu", second)
	}
}

// bboxLines zwraca wynik pdftotext -bbox.
func bboxLines(t *testing.T, pdf []byte) []byte {
	t.Helper()
	file := filepath.Join(t.TempDir(), "out.pdf")
	if err := os.WriteFile(file, pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("pdftotext", "-bbox", file, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Komórka rozpięta (rowspan) należy do wiersza, w którym się zaczyna, więc break-inside: avoid tego wiersza
// albo komórki obejmuje też granice między wierszami, przez które komórka biegnie (CSS Fragmentation 3 § 3.2;
// Chromium tak samo; dokument losowy rand-02). Wcześniej tabela dzieliła się pod pierwszym wierszem komórki.
func TestRowspanInAvoidRowKeepsRowsTogether(t *testing.T) {
	doc := func(row, cell string) string {
		return `<!DOCTYPE html><style>@page { size: 400px 200px; margin: 0 } body { margin: 0; font: 10px/20px Tinos }
			td { border: 1px solid; padding: 2px }</style><div style="height: 100px">filler</div>
			<table style="border-collapse: collapse; width: 300px"><tr><td>R0</td><td>x</td></tr>
			<tr style="` + row + `"><td>R1</td><td rowspan="3" style="` + cell + `">SPAN</td></tr>
			<tr><td>R2</td></tr><tr><td>R3</td></tr><tr><td>R4</td><td>y</td></tr></table>`
	}
	for _, c := range []struct{ row, cell string }{{"break-inside: avoid", ""}, {"", "break-inside: avoid"}} {
		places := wordPlaces(t, renderAllFonts(t, doc(c.row, c.cell)))
		if places["R0"].page != 1 || places["R1"].page != 2 || places["R3"].page != 2 {
			t.Errorf("wiersz %q, komórka %q: R0 na stronie %d, R1 na %d, R3 na %d; oczekiwane 1, 2, 2",
				c.row, c.cell, places["R0"].page, places["R1"].page, places["R3"].page)
		}
	}
	// bez break-inside: avoid tabela dzieli się pod R2, jak wcześniej
	places := wordPlaces(t, renderAllFonts(t, doc("", "")))
	if places["R2"].page != 1 || places["R3"].page != 2 {
		t.Errorf("bez avoid: R2 na stronie %d, R3 na %d; oczekiwane 1 i 2", places["R2"].page, places["R3"].page)
	}
}
