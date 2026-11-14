package gopdfgo

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Testy błędów znalezionych sondażem reftestów W3C (docs/feature-audit.md): każdy z nich był kiedyś
// paniką, zawieszeniem albo zgubioną treścią.

func renderString(t *testing.T, html string) Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := Render(ctx, []byte(html), Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func extractText(t *testing.T, pdf []byte) []byte {
	t.Helper()
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("brak pdftotext")
	}
	file := filepath.Join(t.TempDir(), "out.pdf")
	if err := os.WriteFile(file, pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("pdftotext", file, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Join(bytes.Fields(out), []byte(" "))
}

// ::first-letter nie może gubić pierwszej litery.
func TestFirstLetterKeepsText(t *testing.T) {
	for _, css := range []string{
		"p::first-letter { color: green }",
		"p::first-letter { font-size: 36px }",
		"p::first-letter { float: left; font-size: 40px; line-height: 1 }",
	} {
		result := renderString(t, "<style>"+css+"</style><p>Test zdania</p>")
		text := extractText(t, result.PDF)
		if !bytes.Contains(bytes.ReplaceAll(text, []byte(" "), nil), []byte("Testzdania")) {
			t.Errorf("%s: tekst %q, oczekiwane „Test zdania”", css, text)
		}
	}
}

// Inicjał z ::first-letter łamie wiersze tak samo jak inicjał z pływającego elementu.
func TestDropCapMatchesFloatedSpan(t *testing.T) {
	const page = "@page { size: 300px 110px; margin: 10px } p { font-size: 14px; margin: 0 } "
	const rest = "nicjał na początku akapitu, który ma kilka wierszy tekstu opływającego dużą literę."
	pseudo := renderString(t, "<style>"+page+"p::first-letter { float: left; font-size: 40px; line-height: 1 }</style><p>I"+rest+"</p>")
	span := renderString(t, "<style>"+page+".d { float: left; font-size: 40px; line-height: 1 }</style><p><span class=d>I</span>"+rest+"</p>")
	a, b := extractText(t, pseudo.PDF), extractText(t, span.PDF)
	if !bytes.Equal(a, b) {
		t.Errorf("tekst różny:\n::first-letter %q\nspan           %q", a, b)
	}
	if pseudo.Pages != span.Pages {
		t.Errorf("strony: %d i %d", pseudo.Pages, span.Pages)
	}
}

// Znak, którego nie ma w żadnym podanym foncie, nie może wywrócić zapisu PDF.
func TestMissingGlyphDoesNotFail(t *testing.T) {
	renderString(t, "<p>przed ༻中 po</p><p>༻</p>")
}

// Float wyższy niż kolumna nie może tworzyć kolumn bez końca.
func TestTallFloatInColumns(t *testing.T) {
	renderString(t, `<div style="width:300px; column-width:100px; column-fill:auto; height:100px">
<div style="float:left; width:15px; height:250px; background:aqua"></div><div>tekst</div></div>`)
}

// Siatka z dzieckiem wyższym niż strona nie może tworzyć stron bez końca.
func TestTallGridItem(t *testing.T) {
	result := renderString(t, `<style>@page { size: 200px 200px; margin: 0 }</style>
<div style="display:grid"><div style="height:700px; width:50px; background:pink"></div>tekst</div>po`)
	if result.Pages > 10 {
		t.Errorf("stron: %d", result.Pages)
	}
}

// display: inline-grid nie może kończyć rysowania paniką.
func TestInlineGridRenders(t *testing.T) {
	renderString(t, `<p>przed <span style="display:inline-grid; grid-template-columns:20px 20px"><b>a</b><b>b</b></span> po</p>`)
}

// Słowa kluczowe rozmiaru wewnętrznego (CSS Sizing 3 § 2.1): pudełko o szerokości max-content albo
// fit-content wygląda jak pudełko pływające z tą samą treścią, a min-content jak pudełko, w którym
// każdy wyraz stoi w osobnym wierszu.
func TestIntrinsicSizingKeywords(t *testing.T) {
	const style = "<style>@page { size: 400px 200px; margin: 10px } body { margin: 0; font-size: 16px; width: 300px } " +
		".box { background: #9cf } .narrow { width: 60px }</style>"
	cases := []struct{ name, tested, expected string }{
		{"max-content", `<div class="box" style="width: max-content">raz dwa trzy</div>`, `<div class="box" style="float: left">raz dwa trzy</div>`},
		{"fit-content", `<div class="box" style="width: fit-content">raz dwa trzy</div>`, `<div class="box" style="float: left">raz dwa trzy</div>`},
		{"min-content", `<div class="box" style="width: min-content">raz dwa trzy</div>`, `<div class="box" style="float: left">raz<br>dwa<br>trzy</div>`},
		{"min-width", `<div class="box" style="width: 10px; min-width: max-content">raz dwa trzy</div>`, `<div class="box" style="float: left">raz dwa trzy</div>`},
		{"max-width", `<div class="box" style="max-width: min-content">raz dwa trzy</div>`, `<div class="box" style="float: left">raz<br>dwa<br>trzy</div>`},
		{"overflow", `<div class="narrow"><div class="box" style="width: max-content">raz dwa trzy</div></div>`, `<div class="box" style="float: left">raz dwa trzy</div>`},
	}
	for _, c := range cases {
		tested := renderString(t, style+c.tested)
		expected := renderString(t, style+c.expected)
		dir := t.TempDir()
		a, b := rasterize(t, tested.PDF, dir, "tested"), rasterize(t, expected.PDF, dir, "expected")
		if len(a) != 1 || len(b) != 1 {
			t.Fatalf("%s: strony %d i %d", c.name, len(a), len(b))
		}
		if n := differentPixels(a[0], b[0]); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", c.name, n)
		}
	}
}

// Element z column-span: all ma szerokość kontenera kolumn, nie bloku nadrzędnego; tabela też może
// rozpinać kolumny (CSS Multicol 1 § 6.1).
func TestColumnSpannerWidth(t *testing.T) {
	const style = "<style>@page { size: 500px 300px; margin: 10px } body { margin: 0; font-size: 16px } " +
		".columns { column-count: 3; column-gap: 0; width: 240px } .box { background: #9cf } table { border-spacing: 0 } td { padding: 0 }</style>"
	const expected = `<div style="width: 240px"><div>jeden</div><div class="box">rozpięty</div><div>dwa</div></div>`
	cases := []struct{ name, tested string }{
		{"block", `<div class="columns"><div>jeden</div><div class="box" style="column-span: all">rozpięty</div><div>dwa</div></div>`},
		{"flex", `<div class="columns"><div>jeden</div><div class="box" style="column-span: all; display: flex">rozpięty</div><div>dwa</div></div>`},
		{"table", `<div class="columns"><div>jeden</div><table class="box" style="column-span: all; width: 100%"><tr><td>rozpięty</td></tr></table><div>dwa</div></div>`},
	}
	dir := t.TempDir()
	want := rasterize(t, renderString(t, style+expected).PDF, dir, "expected")
	for _, c := range cases {
		got := rasterize(t, renderString(t, style+c.tested).PDF, dir, "tested")
		if len(got) != 1 || len(want) != 1 {
			t.Fatalf("%s: strony %d i %d", c.name, len(got), len(want))
		}
		if n := differentPixels(got[0], want[0]); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", c.name, n)
		}
	}
}

// Numeracja list według HTML i CSS Lists 3: atrybuty start, reversed, value oraz zasięg licznika
// utworzonego przez element, którego rodzic ma już licznik o tej nazwie.
func TestListNumbering(t *testing.T) {
	const style = "<style>@page { size: 400px 400px; margin: 10px } body { margin: 0; font-size: 14px }</style>"
	cases := []struct{ name, html, want string }{
		{"start", `<ol start="5"><li>a<li>b</ol>`, "5. a 6. b"},
		{"value", `<ol><li>a<li value="7">b<li>c</ol>`, "1. a 7. b 8. c"},
		{"reversed", `<ol reversed><li>a<li>b<li>c</ol>`, "3. a 2. b 1. c"},
		{"reversed start", `<ol reversed start="10"><li>a<li>b</ol>`, "10. a 9. b"},
		{"reversed value", `<ol reversed><li>a<li value="6">b<li>c</ol>`, "7. a 6. b 5. c"},
		{"nested", `<ol><li>a<ol><li>x<li>y</ol><li>b</ol>`, "1. a 1. x 2. y 2. b"},
		{"sibling scope", `<style>#t { counter-reset: c } #t span { counter-increment: c } #t span::before { content: counter(c) }</style>` +
			`<div id="t"><span></span> <span></span> <span style="counter-reset: c 98"></span> <span></span></div>`, "1 2 99 3"},
		{"set after increment", `<style>p { counter-increment: c; counter-set: c 5 } p::before { content: counter(c) }</style><p></p>`, "5"},
	}
	for _, c := range cases {
		if got := string(extractText(t, renderString(t, style+c.html).PDF)); got != c.want {
			t.Errorf("%s: %q, oczekiwane %q", c.name, got, c.want)
		}
	}
}

// Tło elementu liniowego wypełnia cały wiersz przy line-height: normal: obszar treści pudełka liniowego
// to wydłużenie górne i dolne fontu (CSS Inline 3 § 4.1), nie pole em.
func TestInlineBackgroundFillsLine(t *testing.T) {
	const style = "<style>@page { size: 200px 100px; margin: 0 } body { margin: 0; font-size: 16px } " +
		".line { background: #f00; width: 200px } .fill { background: #0f0; color: #0f0 }</style>"
	dir := t.TempDir()
	inline := rasterize(t, renderString(t, style+`<div class="line"><span class="fill">Wiersz</span></div>`).PDF, dir, "inline")
	block := rasterize(t, renderString(t, style+`<div class="line" style="overflow: hidden"><div class="fill" style="float: left">Wiersz</div></div>`).PDF, dir, "block")
	if len(inline) != 1 || len(block) != 1 {
		t.Fatalf("strony %d i %d", len(inline), len(block))
	}
	if n := differentPixels(inline[0], block[0]); n != 0 {
		t.Errorf("%d pikseli różni tło elementu liniowego od tła bloku o tej samej treści", n)
	}
}

// Wiersz zawierający tylko zwijalne odstępy ma zerową wysokość (CSS 2.1 § 9.4.2): dwa bloki wewnątrz
// elementów liniowych, rozdzielone w źródle znakiem nowego wiersza, leżą tak samo jak dwa zwykłe bloki.
func TestWhitespaceOnlyLineHasNoHeight(t *testing.T) {
	const style = "<style>@page { size: 200px 200px; margin: 0 } body { margin: 0; font-size: 16px } " +
		".a { height: 10px; margin-bottom: 30px; background: #00f } .b { height: 10px; background: #080 }</style>"
	dir := t.TempDir()
	tested := rasterize(t, renderString(t, style+"<div><span><div class=\"a\"></div></span>\n<span><div class=\"b\"></div></span></div>").PDF, dir, "tested")
	expected := rasterize(t, renderString(t, style+`<div><div class="a"></div><div class="b"></div></div>`).PDF, dir, "expected")
	if len(tested) != 1 || len(expected) != 1 {
		t.Fatalf("strony %d i %d", len(tested), len(expected))
	}
	if n := differentPixels(tested[0], expected[0]); n != 0 {
		t.Errorf("%d pikseli różni się od wzorca", n)
	}
}

// Tabela z podaną wysokością większą niż treść rozdziela nadmiar między wiersze (CSS Tables 3 § 3.10.5),
// a komórka zachowuje vertical-align.
func TestTableHeightStretchesRows(t *testing.T) {
	const style = "<style>@page { size: 200px 200px; margin: 0 } body { margin: 0; font-size: 16px } " +
		"table { border-spacing: 0 } td { padding: 0; background: #9cf; vertical-align: bottom } " +
		".mark { width: 50px; height: 10px; background: #036 }</style>"
	dir := t.TempDir()
	tested := rasterize(t, renderString(t, style+`<table style="height: 120px"><tr><td><div class="mark"></div></td></tr></table>`).PDF, dir, "tested")
	expected := rasterize(t, renderString(t, style+`<table><tr><td style="height: 120px"><div class="mark"></div></td></tr></table>`).PDF, dir, "expected")
	if len(tested) != 1 || len(expected) != 1 {
		t.Fatalf("strony %d i %d", len(tested), len(expected))
	}
	if n := differentPixels(tested[0], expected[0]); n != 0 {
		t.Errorf("%d pikseli różni się od wzorca", n)
	}
}

// Pudełko tworzące nowy kontekst formatowania mieści się obok elementu pływającego mimo marginesu po
// stronie końca wiersza: przy nadokreśleniu ten margines jest liczony od nowa (CSS 2.1 § 10.3.3).
func TestFormattingContextBesideFloatIgnoresEndMargin(t *testing.T) {
	const style = "<style>@page { size: 200px 200px; margin: 0 } body { margin: 0 } " +
		".box { width: 100px; height: 100px; background: #f00 } .half { width: 50px; height: 100px; background: #080 }</style>"
	dir := t.TempDir()
	tested := rasterize(t, renderString(t, style+`<div class="box"><div class="half" style="float: left"></div>`+
		`<div class="half" style="overflow: hidden; margin-right: 1px"></div></div>`).PDF, dir, "tested")
	expected := rasterize(t, renderString(t, style+`<div class="box" style="background: #080"></div>`).PDF, dir, "expected")
	if len(tested) != 1 || len(expected) != 1 {
		t.Fatalf("strony %d i %d", len(tested), len(expected))
	}
	if n := differentPixels(tested[0], expected[0]); n != 0 {
		t.Errorf("%d pikseli różni się od wzorca", n)
	}
}

// Trzy przypadki, które kończyły proces albo gubiły treść: font-size w jednostkach zależnych od fontu
// (nieskończona rekurencja), licznik poza zakresem 32 bitów (nieskończona rekurencja stylów zastępczych)
// i PNG z przeplotem (obraz pomijany).
func TestFontRelativeFontSize(t *testing.T) {
	result := renderString(t, `<div style="font-size: 20px"><p style="font-size: 3ex">a</p><p style="font-size: 2ch">b</p><p style="font-size: 1lh">c</p></div>`)
	if got := string(extractText(t, result.PDF)); got != "a b c" {
		t.Errorf("tekst %q", got)
	}
}

func TestCounterRangeIsClamped(t *testing.T) {
	const html = `<style>div { counter-reset: c 2000000000 } p { counter-increment: c 100000000 } p::before { content: counter(c) }</style>` +
		`<div><p></p><p></p><p></p></div>`
	// drugie zwiększenie wyszłoby poza zakres, więc jest pomijane (CSS Lists 3 § 4.2)
	if got := string(extractText(t, renderString(t, html).PDF)); got != "2100000000 2100000000 2100000000" {
		t.Errorf("tekst %q", got)
	}
}

func TestInterlacedPNG(t *testing.T) {
	data, err := os.ReadFile("testdata/wpt/css/css-page/support/cat.png")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := Render(ctx, []byte(`<img src="cat.png">`), Options{
		Fonts:    testFonts(t),
		BaseURL:  "file:///doc/index.html",
		Resolver: func(string) (Resource, error) { return Resource{Data: data, MediaType: "image/png"}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result.PDF, []byte("/Subtype/Image")) && !bytes.Contains(result.PDF, []byte("/Subtype /Image")) {
		t.Error("w PDF nie ma obrazu")
	}
}

// Blok o podanej wysokości, wyższy niż strona, jest dzielony między strony zamiast wystawać poza stronę
// (CSS Fragmentation 3 § 5.1). Treść za blokiem ląduje za jego ostatnim fragmentem.
func TestTallFixedHeightBlockIsSplit(t *testing.T) {
	const page = "<style>@page { size: 200px 200px; margin: 20px } body { margin: 0; font-size: 14px } p { margin: 0 }</style>"
	cases := []struct {
		name, html string
		pages      int
	}{
		// obszar strony ma 160 px: 400 px to 160 + 160 + 80
		{"na początku strony", `<div style="height: 400px; background: #9cf"></div><p>koniec</p>`, 3},
		// blok zaczyna się po akapicie i jest dzielony w miejscu
		{"w środku strony", `<p>start</p><div style="height: 400px; background: #9cf"></div><p>koniec</p>`, 3},
		// break-inside: avoid przenosi blok na początek strony, a tam i tak trzeba go podzielić
		{"break-inside: avoid", `<p>start</p><div style="height: 300px; break-inside: avoid; background: #9cf"></div><p>koniec</p>`, 3},
		// blok, który mieści się na stronie, jest przenoszony w całości
		{"mieści się na stronie", `<p>start</p><div style="height: 150px; background: #9cf"></div>`, 2},
		// overflow inny niż visible: bloku nie dzielimy
		{"overflow: hidden", `<div style="height: 400px; overflow: hidden; background: #9cf"></div>`, 1},
	}
	for _, c := range cases {
		result := renderString(t, page+c.html)
		if result.Pages != c.pages {
			t.Errorf("%s: %d stron, oczekiwane %d", c.name, result.Pages, c.pages)
		}
	}
	// wysokości fragmentów sumują się do wysokości bloku
	dir := t.TempDir()
	pages := rasterize(t, renderString(t, page+`<div style="height: 400px; background: #00f"></div>`).PDF, dir, "split")
	total := 0
	for _, ppm := range pages {
		header, data := splitPPM(ppm)
		var width, height int
		fmt.Sscan(strings.Fields(header)[1], &width)
		fmt.Sscan(strings.Fields(header)[2], &height)
		for y := 0; y < height; y++ {
			i := (y*width + width/2) * 3
			if data[i] < 80 && data[i+2] > 200 {
				total++
			}
		}
	}
	if total != 400 {
		t.Errorf("łączna wysokość fragmentów %d px, oczekiwane 400", total)
	}
}
