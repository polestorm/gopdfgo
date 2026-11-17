package gopdfgo

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
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
	// line-height: 2, bo pdftotext ustala kolejność czytania z ramek słów: przy line-height: normal ramki
	// kolejnych wierszy Noto Sans stykają się (wydłużenia 1,362 em wobec wiersza 19 px) i lista
	// zagnieżdżona wychodzi w innej kolejności, tak samo w wydruku Chromium
	const style = "<style>@page { size: 400px 400px; margin: 10px } body { margin: 0; font-size: 14px; line-height: 2 }</style>"
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
		// blok niższy niż strona, ale pierwszy na stronie (przed nim tylko odstęp przodka): nie da się go
		// przenieść, więc jest dzielony zamiast wystawać poza stronę
		{"pierwszy na stronie", `<div style="padding-top: 100px"><div style="height: 120px; background: #9cf"></div></div><p>koniec</p>`, 2},
		// max-height ogranicza wysokość przed podziałem: blok ma 50 px i mieści się na stronie
		{"max-height", `<div style="height: 2000px; max-height: 50px; background: #9cf"></div><p>koniec</p>`, 1},
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

// Blok pozycjonowany bezwzględnie, wyższy niż strona, jest kontynuowany na następnych stronach, także gdy
// treść w przepływie już się skończyła. Pudełko przycięte przez blok zawierający stron nie tworzy.
func TestTallAbsoluteBlockContinues(t *testing.T) {
	const page = "<style>@page { size: 200px 200px; margin: 20px } body { margin: 0; font-size: 14px } p { margin: 0 }</style>"
	cases := []struct {
		name, html string
		pages      int
	}{
		{"bez przycinania", `<div style="position: absolute; top: 0; left: 0; width: 50px; height: 400px; background: #00f"></div><p>tekst</p>`, 3},
		{"overflow: hidden", `<div style="position: absolute; overflow: hidden; width: 10px; height: 10px">` +
			`<div style="position: absolute; height: 400px; width: 5px"></div></div><p>tekst</p>`, 1},
		{"overflow: clip", `<div style="position: absolute; overflow: clip; width: 10px; height: 10px">` +
			`<div style="position: absolute; height: 400px; width: 5px"></div></div><p>tekst</p>`, 1},
	}
	for _, c := range cases {
		if result := renderString(t, page+c.html); result.Pages != c.pages {
			t.Errorf("%s: %d stron, oczekiwane %d", c.name, result.Pages, c.pages)
		}
	}
}

// Obraz w przepływie wyższy niż strona jest krojony między strony, a treść za nim trafia za ostatni
// fragment; break-inside: avoid zostawia obraz w całości. Obraz mieszczący się na stronie jest przenoszony.
func TestTallImageIsSliced(t *testing.T) {
	data, err := os.ReadFile("testdata/wpt/css/css-page/support/cat.png")
	if err != nil {
		t.Fatal(err)
	}
	render := func(html string) Result {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result, err := Render(ctx, []byte("<style>@page { size: 200px 200px; margin: 20px } body { margin: 0; font-size: 14px } "+
			"p { margin: 0 } img { display: block; width: 100px }</style>"+html), Options{
			Fonts:    testFonts(t),
			BaseURL:  "file:///doc/index.html",
			Resolver: func(string) (Resource, error) { return Resource{Data: data, MediaType: "image/png"}, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	// obszar strony ma 160 px; akapit zajmuje pierwszy wiersz
	sliced := render(`<p>przed</p><img src="cat.png" style="height: 400px"><p>po</p>`)
	if sliced.Pages != 3 {
		t.Errorf("obraz 400 px: %d stron, oczekiwane 3", sliced.Pages)
	}
	if got := string(extractText(t, sliced.PDF)); got != "przed po" {
		t.Errorf("tekst %q", got)
	}
	// obraz w całości na pierwszej stronie (wystaje poza nią), akapit na drugiej
	if whole := render(`<img src="cat.png" style="height: 400px; break-inside: avoid"><p>po</p>`); whole.Pages != 2 {
		t.Errorf("break-inside: avoid: %d stron, oczekiwane 2", whole.Pages)
	}
	if moved := render(`<p>przed</p><img src="cat.png" style="height: 150px">`); moved.Pages != 2 {
		t.Errorf("obraz 150 px: %d stron, oczekiwane 2", moved.Pages)
	}
}

// Podana szerokość tabeli obejmuje obramowanie i odstęp wewnętrzny (box-sizing: border-box z arkusza HTML).
func TestTableWidthIsBorderBox(t *testing.T) {
	const style = "<style>@page { size: 400px 100px; margin: 0 } body { margin: 0 } td { padding: 0; height: 20px }</style>"
	dir := t.TempDir()
	measure := func(html string) int {
		pages := rasterize(t, renderString(t, style+html).PDF, dir, "table")
		header, data := splitPPM(pages[0])
		var width int
		fmt.Sscan(strings.Fields(header)[1], &width)
		first, last := -1, -1
		for x := 0; x < width; x++ {
			if data[(5*width+x)*3] < 60 {
				if first < 0 {
					first = x
				}
				last = x
			}
		}
		return last - first + 1
	}
	const table = `border: 20px solid #000; padding: 10px; border-spacing: 0"><tr><td></td></tr></table>`
	if got := measure(`<table style="width: 200px; ` + table); got != 200 {
		t.Errorf("border-box: %d px, oczekiwane 200", got)
	}
	if got := measure(`<table style="width: 200px; box-sizing: content-box; ` + table); got != 260 {
		t.Errorf("content-box: %d px, oczekiwane 260", got)
	}
}

// Dwa pudełka pozycjonowane bezwzględnie kontynuowane na następnej stronie dają za każdym razem ten sam
// plik: kolejność ich układania nie może zależeć od iteracji po mapie.
func TestContinuedAbsoluteBoxesAreDeterministic(t *testing.T) {
	const html = "<style>@page { size: 200px 200px; margin: 20px } body { margin: 0; font-size: 14px }" +
		" div { position: absolute; top: 0; width: 60px; height: 300px }</style>" +
		`<div style="left: 0; background: #f00">a</div><div style="left: 30px; background: #00f">b</div>` +
		`<div style="left: 60px; background: #0a0">c</div><p>tekst</p>`
	first := renderString(t, html)
	if first.Pages != 2 {
		t.Fatalf("%d stron, oczekiwane 2", first.Pages)
	}
	for i := 0; i < 20; i++ {
		if again := renderString(t, html); !bytes.Equal(first.PDF, again.PDF) {
			t.Fatalf("przebieg %d dał inny plik", i+2)
		}
	}
}

// Nagłówek i stopka tabeli powtarzają się na każdej stronie także wtedy, gdy jedyny wiersz jest wyższy niż
// strona i jego zawartość trzeba podzielić.
func TestTableHeaderRepeatsWithTallRow(t *testing.T) {
	const html = "<style>@page { size: 300px 200px; margin: 20px } body { margin: 0; font-size: 12px }" +
		" table { border-spacing: 0; width: 200px } td { padding: 0 }</style>" +
		`<table><thead><tr><td>NAGLOWEK</td></tr></thead>` +
		`<tbody><tr><td><div style="height: 300px; background: #9cf"></div></td></tr></tbody>` +
		`<tfoot><tr><td>STOPKA</td></tr></tfoot></table>`
	result := renderString(t, html)
	if result.Pages < 2 {
		t.Fatalf("%d stron, oczekiwane co najmniej 2", result.Pages)
	}
	text := string(extractText(t, result.PDF))
	if got := strings.Count(text, "NAGLOWEK"); got != result.Pages {
		t.Errorf("nagłówek %d razy na %d stronach: %q", got, result.Pages, text)
	}
	if got := strings.Count(text, "STOPKA"); got != result.Pages {
		t.Errorf("stopka %d razy na %d stronach: %q", got, result.Pages, text)
	}
}

// Ścieżki siatki podane w em mają rozmiar liczony z fontu elementu: wynik jak dla tych samych rozmiarów
// w pikselach.
func TestGridTracksInEm(t *testing.T) {
	const style = "<style>@page { size: 300px 300px; margin: 0 } body { margin: 0; font-size: 10px } " +
		".grid { display: grid; height: 300px } .grid div { background: #9cf; border-top: 2px solid #036 }</style>"
	const items = `<div>a</div><div>b</div><div>c</div></div>`
	dir := t.TempDir()
	em := rasterize(t, renderString(t, style+`<div class="grid" style="grid-template-rows: 4em auto 5em">`+items).PDF, dir, "em")
	px := rasterize(t, renderString(t, style+`<div class="grid" style="grid-template-rows: 40px auto 50px">`+items).PDF, dir, "px")
	if len(em) != 1 || len(px) != 1 {
		t.Fatalf("strony %d i %d", len(em), len(px))
	}
	if n := differentPixels(em[0], px[0]); n != 0 {
		t.Errorf("%d pikseli różni się", n)
	}
}

// Pola formularza: input stoi w wierszu z etykietą, hasło jest zasłonięte, puste pole pokazuje tekst
// zastępczy, a zwinięte details tylko summary.
func TestFormControlsAndDetails(t *testing.T) {
	const html = `<p>Etykieta <input value="wartosc"> koniec</p>` +
		`<p><input type="password" value="tajnehaslo"><input placeholder="podpowiedz"><input type="hidden" value="ukryte"></p>` +
		`<details><summary>Zwinieta</summary><p>niewidoczne</p></details>` +
		`<details open><summary>Rozwinieta</summary><p>widoczne</p></details>`
	text := string(extractText(t, renderString(t, html).PDF))
	for _, want := range []string{"Etykieta wartosc", "koniec", "podpowiedz", "Zwinieta", "Rozwinieta", "widoczne"} {
		if !strings.Contains(text, want) {
			t.Errorf("brak %q w tekście %q", want, text)
		}
	}
	for _, unwanted := range []string{"tajnehaslo", "ukryte", "niewidoczne"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("tekst zawiera %q: %q", unwanted, text)
		}
	}
}

// Szerokość tekstu rośnie proporcjonalnie do rozmiaru fontu także dla rozmiarów niecałkowitych w pikselach:
// 10 pt (13,33 px) leży między 13 px a 14 px, a nie jest składane jak 14 px.
func TestFractionalFontSizeWidth(t *testing.T) {
	width := func(size string) float64 {
		result := renderString(t, `<style>@page { size: 600px 100px; margin: 0 } body { margin: 0 }</style>`+
			`<span style="font-size: `+size+`; background: #000; color: #000">Zestawienie liczby niezgodności w roku</span>`)
		pages := rasterize(t, result.PDF, t.TempDir(), "width")
		header, data := splitPPM(pages[0])
		var pageWidth int
		fmt.Sscan(strings.Fields(header)[1], &pageWidth)
		last := 0
		for x := 0; x < pageWidth; x++ {
			if data[(8*pageWidth+x)*3] < 60 {
				last = x
			}
		}
		return float64(last + 1)
	}
	w13, w1333, w14 := width("13px"), width("13.33px"), width("14px")
	expected := w13 + (w14-w13)*0.33
	if diff := w1333 - expected; diff < -1.5 || diff > 1.5 {
		t.Errorf("13,33 px: %.0f px szerokości, oczekiwane ok. %.1f (13 px: %.0f, 14 px: %.0f)", w1333, expected, w13, w14)
	}
	if pt := width("10pt"); pt != w1333 {
		t.Errorf("10pt: %.0f px, 13.33px: %.0f px", pt, w1333)
	}
}

// Tabulator po tekście kończącym się dokładnie na pozycji tabulatora przesuwa do następnej pozycji:
// kolumny w <pre> nie zlewają się.
func TestTabAfterTextEndingAtTabStop(t *testing.T) {
	result := renderString(t, "<pre>kolumna1\tkolumna2\tkolumna3</pre>")
	text := string(extractText(t, result.PDF))
	if !strings.Contains(text, "kolumna1 kolumna2 kolumna3") {
		t.Errorf("kolumny zlane: %q", text)
	}
}

// text-transform: pełne odwzorowanie wielkości liter z Unicode (ß -> SS).
func TestTextTransformFullCaseMapping(t *testing.T) {
	text := string(extractText(t, renderString(t, `<p style="text-transform: uppercase">straße zażółć</p>`+
		`<p style="text-transform: capitalize">text-transform gęślą o'neil 2x</p>`+
		`<p style="text-transform: lowercase">ZAŻÓŁĆ</p>`).PDF))
	for _, want := range []string{"STRASSE ZAŻÓŁĆ", "Text-transform Gęślą O'neil 2x", "zażółć"} {
		if !strings.Contains(text, want) {
			t.Errorf("brak %q w %q", want, text)
		}
	}
}

// word-break: break-all łamie w dowolnym miejscu, więc wiersz jest wypełniany do końca, także gdy wcześniej
// jest granica wyrazu: tekst zajmuje mniej wierszy niż przy łamaniu na granicach wyrazów.
func TestWordBreakBreakAll(t *testing.T) {
	const style = "<style>@page { size: 300px 300px; margin: 10px } body { margin: 0; font-size: 14px } p { width: 200px; margin: 0 }</style>"
	const text = "Niezaprzeczalność elektronicznych poświadczeń międzywydziałowych"
	lines := func(html string) int {
		pages := rasterize(t, renderString(t, style+html).PDF, t.TempDir(), "lines")
		header, data := splitPPM(pages[0])
		var width, height int
		fmt.Sscan(strings.Fields(header)[1], &width)
		fmt.Sscan(strings.Fields(header)[2], &height)
		last := 0
		for y := 0; y < height; y++ {
			if data[(y*width+15)*3] < 100 {
				last = y
			}
		}
		return last
	}
	normal := lines(`<p style="background: #000; color: #000">` + text + `</p>`)
	breakAll := lines(`<p style="background: #000; color: #000; word-break: break-all">` + text + `</p>`)
	if normal == 0 || breakAll == 0 {
		t.Fatalf("brak tła: %d, %d", normal, breakAll)
	}
	// ten sam akapit: trzy wiersze przy obu ustawieniach, ale break-all nie może zająć więcej
	if breakAll > normal {
		t.Errorf("break-all zajmuje więcej miejsca (%d px) niż łamanie zwykłe (%d px)", breakAll, normal)
	}
	got := string(extractText(t, renderString(t, style+`<p style="word-break: break-all">`+text+`</p>`).PDF))
	if strings.Contains(got, "elektronicznych") {
		t.Errorf("słowo nie zostało przełamane w środku: %q", got)
	}
}

// Pudełko tworzące nowy kontekst formatowania, o szerokości auto, staje obok elementu pływającego
// i zajmuje resztę wiersza, zamiast spadać pod niego z pełną szerokością (CSS 2.1 § 9.5).
func TestFormattingContextNarrowsBesideFloat(t *testing.T) {
	const style = "<style>@page { size: 400px 200px; margin: 0 } body { margin: 0 } " +
		".box { width: 300px; height: 100px } .float { float: left; width: 100px; height: 100px; background: #00f }</style>"
	dir := t.TempDir()
	tested := rasterize(t, renderString(t, style+`<div class="box"><div class="float"></div>`+
		`<div style="overflow: hidden; background: #fc0"><div style="height: 50px"></div></div></div>`).PDF, dir, "tested")
	expected := rasterize(t, renderString(t, style+`<div class="box"><div class="float"></div>`+
		`<div style="float: left; width: 200px; height: 50px; background: #fc0"></div></div>`).PDF, dir, "expected")
	if len(tested) != 1 || len(expected) != 1 {
		t.Fatalf("strony %d i %d", len(tested), len(expected))
	}
	if n := differentPixels(tested[0], expected[0]); n != 0 {
		t.Errorf("%d pikseli różni się od wzorca", n)
	}
}

// Kontener flex wyższy niż strona dzieli się między strony i żaden element nie ginie: w kolumnie podział
// wypada między elementami, w wierszu z zawijaniem między liniami; działa też dla body i przy zagnieżdżeniu.
func TestFlexContainerSplitsAcrossPages(t *testing.T) {
	const style = "<style>@page { size: 300px 200px; margin: 20px } body { margin: 0; font-size: 12px } p { margin: 0 } " +
		".i { height: 50px; box-sizing: border-box; border: 1px solid #000 }</style>"
	var items, wide string
	for i := 1; i <= 7; i++ {
		items += fmt.Sprintf(`<div class="i">e%d</div>`, i)
		wide += fmt.Sprintf(`<div class="i" style="width: 50%%">e%d</div>`, i)
	}
	cases := []struct {
		name, html string
		pages      int
	}{
		// obszar strony ma 160 px: trzy elementy po 50 px na stronę
		{"kolumna", `<div style="display: flex; flex-direction: column">` + items + `</div><p>koniec</p>`, 3},
		{"kolumna po akapicie", `<p>start</p><div style="display: flex; flex-direction: column">` + items + `</div><p>koniec</p>`, 3},
		{"body", `<style>body { display: flex; flex-direction: column }</style>` + items + `<p>koniec</p>`, 3},
		{"zagnieżdżona", `<div style="display: flex; flex-direction: column"><div style="display: flex; flex-direction: column">` +
			items + `</div><div class="i">e8</div></div><p>koniec</p>`, 3},
		{"wiersz z zawijaniem", `<div style="display: flex; flex-wrap: wrap">` + wide + `</div><p>koniec</p>`, 2},
	}
	for _, c := range cases {
		result := renderString(t, style+c.html)
		if result.Pages != c.pages {
			t.Errorf("%s: %d stron, oczekiwane %d", c.name, result.Pages, c.pages)
		}
		text := " " + string(extractText(t, result.PDF)) + " "
		for i := 1; i <= 7; i++ {
			if !strings.Contains(text, fmt.Sprintf(" e%d ", i)) {
				t.Errorf("%s: brak elementu e%d w %q", c.name, i, text)
			}
		}
		if !strings.Contains(text, " koniec ") {
			t.Errorf("%s: brak akapitu końcowego w %q", c.name, text)
		}
	}
	// kontener, z którego nic nie mieści się na stronie, przechodzi cały: element e1 jest na drugiej stronie
	moved := renderString(t, style+`<div style="height: 130px"></div><div style="display: flex; border: 2px solid #000">`+
		`<div class="i">e1</div></div>`)
	if moved.Pages != 2 {
		t.Errorf("kontener przenoszony w całości: %d stron, oczekiwane 2", moved.Pages)
	}
}

// Dwie kolumny obok siebie w wierszu flex, wyższe niż strona, dzielą się między strony równolegle: druga
// kolumna zaczyna się na pierwszej stronie obok pierwszej, a nie dopiero po jej zakończeniu.
func TestFlexRowColumnsSplitInParallel(t *testing.T) {
	const style = "<style>@page { size: 300px 200px; margin: 20px } body { margin: 0; font-size: 12px } p { margin: 0 } " +
		".i { height: 50px; box-sizing: border-box; border: 1px solid #000 }</style>"
	var left, right string
	for i := 1; i <= 5; i++ {
		left += fmt.Sprintf(`<div class="i">a%d</div>`, i)
	}
	for i := 1; i <= 4; i++ {
		right += fmt.Sprintf(`<div class="i">b%d</div>`, i)
	}
	result := renderString(t, style+`<div style="display: flex"><div style="flex: 1">`+left+`</div><div style="flex: 1">`+right+
		`</div><div style="width: 40px">bok</div></div><p>koniec</p>`)
	if result.Pages != 2 {
		t.Fatalf("%d stron, oczekiwane 2", result.Pages)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "out.pdf")
	if err := os.WriteFile(file, result.PDF, 0o644); err != nil {
		t.Fatal(err)
	}
	page := func(n string) string {
		out, err := exec.Command("pdftotext", "-f", n, "-l", n, file, "-").Output()
		if err != nil {
			t.Skip("brak pdftotext")
		}
		return " " + strings.Join(strings.Fields(string(out)), " ") + " "
	}
	first, second := page("1"), page("2")
	for _, want := range []string{"a1", "a3", "b1", "b3", "bok"} {
		if !strings.Contains(first, " "+want+" ") {
			t.Errorf("strona 1 bez %q: %q", want, first)
		}
	}
	for _, want := range []string{"a4", "a5", "b4", "koniec"} {
		if !strings.Contains(second, " "+want+" ") {
			t.Errorf("strona 2 bez %q: %q", want, second)
		}
	}
	if strings.Contains(second, " bok ") {
		t.Errorf("element zakończony na stronie 1 powtórzony na stronie 2: %q", second)
	}
}

// Elementy siatki wyższe niż strona dzielą się między strony równolegle i nic nie ginie; dalszy ciąg
// elementu ma wysokość swojej treści, a nie całego wiersza.
func TestGridItemsSplitAcrossPages(t *testing.T) {
	const style = "<style>@page { size: 300px 200px; margin: 20px } body { margin: 0; font-size: 12px } p { margin: 0 } " +
		".i { height: 50px; box-sizing: border-box; border: 1px solid #000 }</style>"
	var left, right string
	for i := 1; i <= 5; i++ {
		left += fmt.Sprintf(`<div class="i">a%d</div>`, i)
	}
	for i := 1; i <= 4; i++ {
		right += fmt.Sprintf(`<div class="i">b%d</div>`, i)
	}
	result := renderString(t, style+`<div style="display: grid; grid-template-columns: 1fr 1fr"><div>`+left+`</div><div>`+right+
		`</div></div><p>koniec</p>`)
	if result.Pages != 2 {
		t.Errorf("%d stron, oczekiwane 2", result.Pages)
	}
	text := " " + string(extractText(t, result.PDF)) + " "
	for _, want := range []string{"a1", "a5", "b1", "b4", "koniec"} {
		if !strings.Contains(text, " "+want+" ") {
			t.Errorf("brak %q w %q", want, text)
		}
	}
}

// Kontener z własnym kontekstem formatowania, którego wysokość wyznacza element pływający wyższy niż
// strona, ciągnie się przez strony razem z nim: tło kontenera jest na każdej stronie, na której jest
// element pływający.
func TestContainerContinuesWithTallFloat(t *testing.T) {
	const html = "<style>@page { size: 300px 200px; margin: 20px } body { margin: 0 }</style>" +
		`<div style="display: flow-root; background: #ff0"><div style="float: left; width: 50px; height: 400px; background: #f0f"></div></div>`
	result := renderString(t, html)
	if result.Pages != 3 {
		t.Fatalf("%d stron, oczekiwane 3", result.Pages)
	}
	pages := rasterize(t, result.PDF, t.TempDir(), "float")
	for number, ppm := range pages {
		header, data := splitPPM(ppm)
		var width int
		fmt.Sscan(strings.Fields(header)[1], &width)
		// punkt w kontenerze na prawo od elementu pływającego, 10 px pod górną krawędzią obszaru strony
		i := (30*width + 200) * 3
		if !(data[i] > 200 && data[i+1] > 200 && data[i+2] < 80) {
			t.Errorf("strona %d: brak tła kontenera (kolor %v)", number+1, data[i:i+3])
		}
	}
}

// Panel boczny w wierszu flex, rozciągany do wysokości linii, ciągnie się przez strony razem z sąsiednią
// kolumną, która dzieli się między strony: jego tło jest na każdej stronie, na której jest treść kolumny.
func TestStretchedFlexItemContinuesAcrossPages(t *testing.T) {
	var text string
	for i := 1; i <= 8; i++ {
		text += fmt.Sprintf("<p>A%d lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor</p>", i)
	}
	result := renderString(t, "<style>@page { size: 300px 200px; margin: 20px } body { margin: 0; font-size: 10px } p { margin: 0 0 4px }</style>"+
		`<div style="display: flex; gap: 10px"><div style="flex: 2">`+text+`</div><div style="flex: 1; background: #fc0"><p>PANEL</p></div></div>`)
	if result.Pages < 2 {
		t.Fatalf("%d stron, oczekiwane co najmniej 2", result.Pages)
	}
	for number, ppm := range rasterize(t, result.PDF, t.TempDir(), "panel") {
		header, data := splitPPM(ppm)
		var width int
		fmt.Sscan(strings.Fields(header)[1], &width)
		i := (30*width + 250) * 3 // punkt w panelu, 10 px pod górną krawędzią obszaru strony
		if !(data[i] > 200 && data[i+1] > 170 && data[i+2] < 80) {
			t.Errorf("strona %d: brak tła panelu (kolor %v)", number+1, data[i:i+3])
		}
	}
}

// Dokument, w którym wiersz z siatką (inline-grid, elementy z height: fit-content) dostaje nieskończoną
// wysokość, nie może dać nieskończenie wielu stron: krojenie i podział dotyczą tylko skończonych wysokości.
// Plik pochodzi z testów W3C (wzorzec grid-lanes-item-placement-006); przed poprawką renderowanie kończyło
// się przekroczeniem limitu czasu.
func TestInfiniteHeightDoesNotLoop(t *testing.T) {
	html, err := os.ReadFile("testdata/regression/endless-line-slicing.html")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := Render(ctx, html, Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Pages > 5 {
		t.Errorf("%d stron dla kilku małych siatek", result.Pages)
	}
}

// Legenda ramki fieldset leży na górnym obramowaniu, a obramowanie jest pod nią przerwane: w wierszu
// obramowania nie ma ciemnych pikseli na szerokości legendy, a są po jej prawej stronie.
func TestFieldsetLegendOnBorder(t *testing.T) {
	result := renderString(t, "<style>@page { size: 400px 200px; margin: 20px } body { margin: 0; font-size: 16px }"+
		" fieldset { border: 4px solid #000; margin: 0; padding: 10px } legend { color: #fff }</style>"+
		`<p style="margin: 0 0 10px">nad ramką</p><fieldset><legend>Legenda ramki</legend>treść</fieldset>`)
	pages := rasterize(t, result.PDF, t.TempDir(), "legend")
	header, data := splitPPM(pages[0])
	var width, height int
	fmt.Sscan(strings.Fields(header)[1], &width)
	fmt.Sscan(strings.Fields(header)[2], &height)
	dark := func(x, y int) bool { return data[(y*width+x)*3] < 80 }
	// wiersz górnego obramowania: pierwszy od góry z ciemnym pikselem przy prawej krawędzi ramki
	border := -1
	for y := 0; y < height && border < 0; y++ {
		if dark(width-40, y) {
			border = y + 1
		}
	}
	if border < 0 {
		t.Fatal("nie znaleziono górnego obramowania ramki")
	}
	// lewe obramowanie jest przy x = 20..24; legenda zaczyna się za odstępem wewnętrznym ramki
	if dark(60, border) {
		t.Errorf("obramowanie nie jest przerwane pod legendą (wiersz %d)", border)
	}
	if !dark(width-60, border) {
		t.Errorf("brak obramowania po prawej stronie legendy (wiersz %d)", border)
	}
}

// Elementy dopasowane przez te same reguły dzielą jedną kaskadę. Element z
// atrybutem style nie może ani jej przejąć, ani zmienić rodzeństwu: dokument
// z atrybutem musi dać ten sam PDF co dokument, w którym tę samą deklarację
// niesie reguła dopasowana tylko do tego elementu (bez id, bo id tworzy kotwicę w PDF).
func TestSharedCascadeKeepsOwnDeclarations(t *testing.T) {
	const rules = `<style>p { margin: 4px 0; color: navy } p::before { content: "- " } .wide { letter-spacing: 2px }</style>`
	withAttribute := rules + `<p class="wide">pierwszy</p><p class="wide" style="margin-left: 50px; color: green">drugi</p><p class="wide">trzeci</p>`
	withRule := rules + `<style>p.wide.own { margin-left: 50px; color: green }</style>` +
		`<p class="wide">pierwszy</p><p class="wide own">drugi</p><p class="wide">trzeci</p>`
	options := Options{Fonts: testFonts(t)}
	first, err := Render(t.Context(), []byte(withAttribute), options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(t.Context(), []byte(withRule), options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.PDF, second.PDF) {
		t.Error("atrybut style i równoważna reguła dały różne dokumenty")
	}
	plain, err := Render(t.Context(), []byte(rules+`<p class="wide">pierwszy</p><p class="wide">drugi</p><p class="wide">trzeci</p>`), options)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.PDF, plain.PDF) {
		t.Error("deklaracje z atrybutu style nie zostały zastosowane")
	}
}

// Elementy o tej samej kaskadzie dzielą tablicę wartości obliczonych. Wartość
// zależna od atrybutu elementu (attr()) albo zmieniona tylko u jednego przodka
// nie może przejść do rodzeństwa.
func TestSharedComputedValuesStayPerElement(t *testing.T) {
	html := `<style>
  @page { size: 400px 300px; margin: 20px }
  li { list-style: none }
  li::before { content: attr(data-code) " " }
  section { margin: 0 }
</style>
<ul>
  <li data-code="alfa">jeden</li>
  <li data-code="beta">dwa</li>
  <li data-code="gamma">trzy</li>
</ul>
<section style="color: red"><p><span>czerwony</span></p></section>
<section><p><span>czarny</span></p></section>`
	result, err := Render(t.Context(), []byte(html), Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	words := visibleWords(t, result.PDF, 225)
	for _, want := range []string{"alfa", "beta", "gamma", "jeden", "dwa", "trzy"} {
		if !words[want] {
			t.Errorf("brak słowa %q; attr() dało wartość wspólną dla rodzeństwa", want)
		}
	}
	// Ten sam dokument bez czerwieni musi się różnić: kolor odziedziczony po
	// pierwszej sekcji nie może ani zniknąć, ani objąć drugiej.
	bothRed, err := Render(t.Context(), []byte(strings.Replace(html, `<section><p>`, `<section style="color: red"><p>`, 1)), Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	noneRed, err := Render(t.Context(), []byte(strings.Replace(html, ` style="color: red"`, ``, 1)), Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(result.PDF, bothRed.PDF) || bytes.Equal(result.PDF, noneRed.PDF) {
		t.Error("kolor odziedziczony po jednej sekcji przeszedł do drugiej albo zniknął")
	}
}

// Błędy składni CSS: lewa strona musi dać ten sam dokument co prawa.
// Każdy przypadek był w przejętym kodzie obsłużony inaczej, niż każe norma.
func TestCSSSyntaxErrorHandling(t *testing.T) {
	const body = `<p>akapit</p><div id="a" class="k">tekst</div>`
	cases := []struct{ name, got, want string }{
		{"błędny kolor nie ukrywa tekstu", `div { color: green; color: rgb(255, 0) }`, `div { color: green }`},
		{"nieznana nazwa koloru", `div { color: green; color: redish }`, `div { color: green }`},
		{"kolor z kanałem alfa", `div { color: #0080007f }`, `div { color: rgba(0, 128, 0, 0.498039) }`},
		{"blok bez selektora w regule", `div { { color: red } }`, ``},
		{"błędna reguła zagnieżdżona nie psuje rodzica", `div { color: green; 12 { color: red } }`, `div { color: green }`},
		{"identyfikator od cyfry", `#6a, div { color: red }`, ``},
		{"nazwa funkcji url wielkimi literami", `div { background: UrL("missing.png") green }`, `div { background: url("missing.png") green }`},
	}
	options := Options{Fonts: testFonts(t)}
	for _, c := range cases {
		got, err := Render(t.Context(), []byte(`<style>`+c.got+`</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		want, err := Render(t.Context(), []byte(`<style>`+c.want+`</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.PDF, want.PDF) {
			t.Errorf("%s: %q dało inny dokument niż %q", c.name, c.got, c.want)
		}
	}
}

// Dopasowanie selektorów: lewy arkusz musi dać ten sam dokument co prawy.
func TestSelectorMatchingFixes(t *testing.T) {
	const body = `<p title=" ">akapit</p><div lang="de"><span lang="PL-pl">tekst</span><em>inny</em></div>`
	cases := []struct{ name, got, want string }{
		{"element główny jest pierwszym dzieckiem", `:root:first-child { color: green }`, `html { color: green }`},
		{"pusta wartość w [attr~=]", `[title~=""] { color: red }`, ``},
		{"kombinator po pseudoelemencie", `p::first-line em, p { color: red }`, ``},
		{"język bliższego przodka wygrywa", `span:lang(de) { color: red }`, ``},
		{"język bez rozróżniania wielkości liter", `span:lang(pl) { color: green }`, `span { color: green }`},
		{"język dziedziczony", `em:lang(de) { color: green }`, `em { color: green }`},
	}
	options := Options{Fonts: testFonts(t)}
	for _, c := range cases {
		got, err := Render(t.Context(), []byte(`<style>`+c.got+`</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		want, err := Render(t.Context(), []byte(`<style>`+c.want+`</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.PDF, want.PDF) {
			t.Errorf("%s: %q dało inny dokument niż %q", c.name, c.got, c.want)
		}
	}
}

// Atrybut style wygrywa z selektorem identyfikatora, a przegrywa tylko
// z deklaracją !important (CSS 2.1 § 6.4.1 i § 6.4.3).
func TestStyleAttributeBeatsIDSelector(t *testing.T) {
	options := Options{Fonts: testFonts(t)}
	render := func(html string) []byte {
		t.Helper()
		result, err := Render(t.Context(), []byte(html), options)
		if err != nil {
			t.Fatal(err)
		}
		return result.PDF
	}
	green := render(`<style>#a { color: green }</style><div id="a">tekst</div>`)
	if got := render(`<style>#a { color: red }</style><div id="a" style="color: green">tekst</div>`); !bytes.Equal(got, green) {
		t.Error("reguła #a przesłoniła atrybut style")
	}
	if got := render(`<style>#a { color: green !important }</style><div id="a" style="color: red">tekst</div>`); !bytes.Equal(got, green) {
		t.Error("atrybut style przesłonił deklarację !important")
	}
}

// @import z adresem w cudzysłowie wewnątrz url() wczytuje arkusz tak samo
// jak pozostałe postaci.
func TestImportWithQuotedURL(t *testing.T) {
	sheet := []byte(`div { color: green }`)
	resolver := func(url string) (Resource, error) {
		if strings.HasSuffix(url, "sheet.css") {
			return Resource{Data: sheet, MediaType: "text/css"}, nil
		}
		return Resource{}, errors.New("nieznany zasób " + url)
	}
	var documents [][]byte
	for _, rule := range []string{`@import url("sheet.css");`, `@import url(sheet.css);`, `@import "sheet.css";`} {
		result, err := Render(t.Context(), []byte(`<style>`+rule+`</style><div>tekst</div>`),
			Options{Fonts: testFonts(t), Resolver: resolver, BaseURL: "https://example.test/doc.html"})
		if err != nil {
			t.Fatal(err)
		}
		for _, warning := range result.Warnings {
			t.Errorf("%s: ostrzeżenie %q", rule, warning.Message)
		}
		documents = append(documents, result.PDF)
	}
	plain, err := Render(t.Context(), []byte(`<style></style><div>tekst</div>`), Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(documents[0], documents[1]) || !bytes.Equal(documents[0], documents[2]) {
		t.Error("trzy postaci @import dały różne dokumenty")
	}
	if bytes.Equal(documents[0], plain.PDF) {
		t.Error("arkusz z @import nie został zastosowany")
	}
}

// min-width, max-width, min-height i max-height działają na tabelach
// (CSS 2.1 § 10.4 i § 10.7): tabela z ograniczeniem musi wyjść tak samo jak
// tabela z podanym wprost wymiarem wynikowym.
func TestTableMinMaxSizes(t *testing.T) {
	const cell = `<tr><td style="background: #ccc">a</td></tr></table>`
	cases := []struct{ name, got, want string }{
		{"max-width przy podanej szerokości", `width: 300px; max-width: 120px`, `width: 120px`},
		{"min-width przy szerokości automatycznej", `min-width: 250px`, `width: 250px`},
		{"max-height przy podanej wysokości", `width: 100px; height: 300px; max-height: 80px`, `width: 100px; height: 80px`},
		{"min-height przy wysokości automatycznej", `width: 100px; min-height: 90px`, `width: 100px; height: 90px`},
	}
	options := Options{Fonts: testFonts(t)}
	for _, c := range cases {
		got, err := Render(t.Context(), []byte(`<table style="`+c.got+`">`+cell), options)
		if err != nil {
			t.Fatal(err)
		}
		want, err := Render(t.Context(), []byte(`<table style="`+c.want+`">`+cell), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.PDF, want.PDF) {
			t.Errorf("%s: %q dało inną tabelę niż %q", c.name, c.got, c.want)
		}
	}
}

// Wiersz z visibility: collapse nie zajmuje miejsca i nie jest rysowany, ale
// nadal wyznacza szerokość kolumn (CSS 2.1 § 17.5.5).
func TestCollapsedTableRow(t *testing.T) {
	html := `<style>@page { size: 400px 300px; margin: 20px } td { padding: 0; height: 40px }</style>
<table style="border-spacing: 0"><tr style="visibility: collapse"><td>ukryty-szeroki-wiersz</td></tr><tr><td>widoczny</td></tr></table><p>akapit</p>`
	result, err := Render(t.Context(), []byte(html), Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	reference, err := Render(t.Context(), []byte(strings.Replace(html, `<tr style="visibility: collapse"><td>ukryty-szeroki-wiersz</td></tr>`, ``, 1)), Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	words := visibleWords(t, result.PDF, 225)
	if words["ukryty-szeroki-wiersz"] {
		t.Error("tekst zwiniętego wiersza jest widoczny")
	}
	position := func(pdf []byte) string {
		t.Helper()
		file := filepath.Join(t.TempDir(), "out.pdf")
		if err := os.WriteFile(file, pdf, 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("pdftotext", "-bbox", file, "-").Output()
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, ">akapit<") {
				return wordBox.FindStringSubmatch(line)[1]
			}
		}
		return ""
	}
	if got, want := position(result.PDF), position(reference.PDF); got == "" || got != want {
		t.Errorf("akapit pod tabelą na wysokości %q, bez zwiniętego wiersza %q", got, want)
	}
}

// Pudełko pozycjonowane bezwzględnie względem strony nie jest przycinane przez
// przodka z overflow: hidden, który nie jest jego bryłą zawierającą; pudełko,
// dla którego przodek jest bryłą zawierającą, jest przycinane (CSS 2.1 § 11.1.1).
func TestOverflowClipsOnlyContainedAbsoluteBoxes(t *testing.T) {
	page := `<style>@page { size: 400px 300px; margin: 20px } body { margin: 0 }
.clip { width: 100px; height: 40px; overflow: hidden; margin-top: 150px }
.out { position: absolute; top: 10px; left: 200px }</style>`
	options := Options{Fonts: testFonts(t)}
	escaped, err := Render(t.Context(), []byte(page+`<div class="clip"><div class="out">ucieka</div></div>`), options)
	if err != nil {
		t.Fatal(err)
	}
	if !visibleWords(t, escaped.PDF, 225)["ucieka"] {
		t.Error("pudełko pozycjonowane względem strony zostało przycięte przez przodka z overflow")
	}
	contained, err := Render(t.Context(), []byte(page+`<div class="clip" style="position: relative"><div class="out">zostaje</div></div>`), options)
	if err != nil {
		t.Fatal(err)
	}
	// Tekst przycięty pozostaje w PDF (przycięcie to ścieżka, nie usunięcie),
	// więc zamiast szukać słowa porównujemy z tym samym dokumentem bez przycinania.
	visible, err := Render(t.Context(), []byte(page+`<div class="clip" style="position: relative; overflow: visible"><div class="out">zostaje</div></div>`), options)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(contained.PDF, visible.PDF) {
		t.Error("pudełko pozycjonowane względem przodka z overflow nie zostało przycięte")
	}
}

// Marginesy automatyczne pudełka pozycjonowanego liczą się z wysokości po
// zastosowaniu max-height, a pojedynczy margines automatyczny uwzględnia drugi.
func TestAbsoluteAutoMargins(t *testing.T) {
	const frame = `<style>@page { size: 400px 300px; margin: 0 } body { margin: 0 }
.cb { position: relative; width: 200px; height: 200px }
.box { position: absolute; left: 0; width: 50px; background: #333 }</style><div class="cb"><div class="box" style="`
	cases := []struct{ name, got, want string }{
		{"max-height przed wyśrodkowaniem", `top: 20px; bottom: 20px; height: 120px; max-height: 40px; margin: auto 0`, `top: 80px; height: 40px`},
		{"margines górny automatyczny przy dolnym podanym", `top: 10px; bottom: 10px; height: 50px; margin-top: auto; margin-bottom: 30px`, `top: 110px; height: 50px`},
	}
	options := Options{Fonts: testFonts(t)}
	for _, c := range cases {
		got, err := Render(t.Context(), []byte(frame+c.got+`"></div></div>`), options)
		if err != nil {
			t.Fatal(err)
		}
		want, err := Render(t.Context(), []byte(frame+c.want+`"></div></div>`), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.PDF, want.PDF) {
			t.Errorf("%s: %q dało inne położenie niż %q", c.name, c.got, c.want)
		}
	}
}

// W kontenerze flex o nieokreślonej wysokości (kolumna, height: auto) procenty
// w osi głównej nie mają podstawy: wysokość procentowa działa jak auto,
// a procentowe flex-basis jak content (CSS Flexbox 1 § 7.2.3 i § 9.8).
// Przejęty kod liczył je z nieskończoności i zapisywał NaN albo Inf w PDF.
func TestFlexPercentagesInIndefiniteColumn(t *testing.T) {
	const open = `<div style="display: flex; flex-direction: column">`
	cases := []struct{ name, got, want string }{
		{"wysokość procentowa", `<div style="height: 50%; background: red"></div><div style="height: 80px; width: 80px; background: green"></div>`,
			`<div style="background: red"></div><div style="height: 80px; width: 80px; background: green"></div>`},
		{"procentowe flex-basis", `<div style="flex: 1 0 100%; border: 1px solid blue">tekst</div>`,
			`<div style="flex: 1 0 content; border: 1px solid blue">tekst</div>`},
	}
	options := Options{Fonts: testFonts(t)}
	for _, c := range cases {
		got, err := Render(t.Context(), []byte(open+c.got+`</div>`), options)
		if err != nil {
			t.Fatal(err)
		}
		want, err := Render(t.Context(), []byte(open+c.want+`</div>`), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.PDF, want.PDF) {
			t.Errorf("%s: %q dało inny dokument niż %q", c.name, c.got, c.want)
		}
	}
}

// Flex: wysokość nadana kontenerowi przez rozciągnięcie jest podstawą procentów
// jego elementów; justify-content ma wyrównania zastępcze, gdy brakuje miejsca
// albo w linii jest jeden element (CSS Flexbox 1 § 9.4, CSS Box Alignment 3 § 4).
func TestFlexDefiniteSizesAndFallbackAlignment(t *testing.T) {
	const row = `<style>.f { display: flex } .i { flex: 0 0 60px; height: 20px; background: #555 }</style>`
	cases := []struct{ name, got, want string }{
		{"procentowe min-height w rozciągniętym kontenerze",
			`<div style="display: flex; height: 80px"><div style="display: flex; width: 60px; align-items: center"><span style="min-height: 100%; width: 100%; background: green"></span></div></div>`,
			`<div style="display: flex; height: 80px"><div style="display: flex; width: 60px; align-items: center"><span style="min-height: 80px; width: 100%; background: green"></span></div></div>`},
		{"space-between bez miejsca", row + `<div class="f" style="width: 100px; justify-content: space-between"><div class="i"></div><div class="i"></div></div>`,
			row + `<div class="f" style="width: 100px; justify-content: flex-start"><div class="i"></div><div class="i"></div></div>`},
		{"space-around bez miejsca", row + `<div class="f" style="width: 100px; justify-content: space-around"><div class="i"></div><div class="i"></div></div>`,
			row + `<div class="f" style="width: 100px; justify-content: flex-start"><div class="i"></div><div class="i"></div></div>`},
		{"space-around z jednym elementem", row + `<div class="f" style="width: 200px; justify-content: space-around"><div class="i"></div></div>`,
			row + `<div class="f" style="width: 200px; justify-content: center"><div class="i"></div></div>`},
		{"right w kolumnie", row + `<div class="f" style="height: 100px; flex-direction: column; justify-content: right"><div class="i"></div></div>`,
			row + `<div class="f" style="height: 100px; flex-direction: column; justify-content: start"><div class="i"></div></div>`},
	}
	options := Options{Fonts: testFonts(t)}
	for _, c := range cases {
		got, err := Render(t.Context(), []byte(c.got), options)
		if err != nil {
			t.Fatal(err)
		}
		want, err := Render(t.Context(), []byte(c.want), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.PDF, want.PDF) {
			t.Errorf("%s: dokument różni się od wzorcowego", c.name)
		}
	}
}

// To samo co TestFormattingContextNarrowsBesideFloat, ale z marginesem body,
// który składa się z marginesami potomków: pozycja pudełka nie zawiera go
// jeszcze w chwili szukania miejsca obok elementu pływającego.
func TestFormattingContextNarrowsBesideFloatWithCollapsedMargins(t *testing.T) {
	const style = "<style>@page { size: 400px 200px; margin: 0 } body { margin: 20px } " +
		".box { width: 300px; height: 100px } .float { float: left; width: 100px; height: 100px; background: #00f }</style>"
	dir := t.TempDir()
	tested := rasterize(t, renderString(t, style+`<div class="box"><div class="float"></div>`+
		`<div style="overflow: hidden; background: #fc0"><div style="height: 50px"></div></div></div>`).PDF, dir, "tested")
	expected := rasterize(t, renderString(t, style+`<div class="box"><div class="float"></div>`+
		`<div style="float: left; width: 200px; height: 50px; background: #fc0"></div></div>`).PDF, dir, "expected")
	if len(tested) != 1 || len(expected) != 1 {
		t.Fatalf("strony %d i %d", len(tested), len(expected))
	}
	if n := differentPixels(tested[0], expected[0]); n != 0 {
		t.Errorf("%d pikseli różni się od wzorca", n)
	}
}

// Element pływający wewnątrz pudełka liniowego z marginesem, obramowaniem
// i odstępem leży przy krawędzi bloku zawierającego, a nie za tym odstępem.
func TestFloatInsideInlineWithSpacing(t *testing.T) {
	const style = "<style>@page { size: 400px 200px; margin: 0 } body { margin: 0 } " +
		".f { float: left; width: 80px; height: 60px; background: #00f }</style>"
	dir := t.TempDir()
	tested := rasterize(t, renderString(t, style+
		`<span style="padding: 20px; border: 10px solid transparent; margin: 30px"><span class="f"></span></span>`).PDF, dir, "tested")
	expected := rasterize(t, renderString(t, style+`<div class="f"></div>`).PDF, dir, "expected")
	if len(tested) != 1 || len(expected) != 1 {
		t.Fatalf("strony %d i %d", len(tested), len(expected))
	}
	if n := differentPixels(tested[0], expected[0]); n != 0 {
		t.Errorf("%d pikseli różni się od wzorca", n)
	}
}

// Warstwy kaskady (@layer, CSS Cascade 5 § 6.4): późniejsza warstwa wygrywa
// z wcześniejszą niezależnie od wagi selektora, reguły poza warstwami wygrywają
// z warstwami, a dla !important kolejność jest odwrotna.
func TestCascadeLayers(t *testing.T) {
	const body = `<div id="a" class="k">tekst</div>`
	cases := []struct{ name, got, want string }{
		{"późniejsza warstwa wygrywa mimo słabszego selektora", `@layer one { #a { color: red } } @layer two { div { color: green } }`, `div { color: green }`},
		{"kolejność ogłoszenia, nie reguł", `@layer one, two; @layer two { div { color: green } } @layer one { #a { color: red } }`, `div { color: green }`},
		{"reguły poza warstwami wygrywają", `div { color: green } @layer one { #a.k { color: red } }`, `div { color: green }`},
		{"!important odwraca kolejność warstw", `@layer one { div { color: green !important } } @layer two { #a { color: red !important } }`, `div { color: green }`},
		{"warstwa zagnieżdżona przegrywa z regułami rodzica", `@layer one { div { color: green } @layer inner { #a { color: red } } }`, `div { color: green }`},
		{"warstwa bez nazwy", `@layer { #a { color: red } } div { color: green }`, `div { color: green }`},
		{"warstwa wewnątrz @media", `@media print { @layer one { div { color: green } } }`, `div { color: green }`},
	}
	options := Options{Fonts: testFonts(t)}
	for _, c := range cases {
		got, err := Render(t.Context(), []byte(`<style>`+c.got+`</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		want, err := Render(t.Context(), []byte(`<style>`+c.want+`</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.PDF, want.PDF) {
			t.Errorf("%s: %q dało inny dokument niż %q", c.name, c.got, c.want)
		}
		for _, warning := range got.Warnings {
			t.Errorf("%s: ostrzeżenie %q", c.name, warning.Message)
		}
	}
}

// repeat(auto-fit, …): puste ścieżki są zwijane razem z odstępami, więc
// elementy rozkładają się tak, jakby siatka miała tylko zajęte ścieżki
// (CSS Grid 2 § 7.2.3.2). Element flex lub grid z z-index tworzy kontekst
// nakładania bez pozycjonowania.
func TestGridAutoFitAndItemZIndex(t *testing.T) {
	const item = `<div style="background: #555; height: 30px"></div>`
	cases := []struct{ name, got, want string }{
		{"auto-fit zwija puste ścieżki",
			`<div style="display: grid; width: 400px; gap: 10px; grid-template-columns: repeat(auto-fit, minmax(50px, 1fr))">` + item + item + `</div>`,
			`<div style="display: grid; width: 400px; gap: 10px; grid-template-columns: repeat(2, minmax(50px, 1fr))">` + item + item + `</div>`},
		{"auto-fit z wyrównaniem do środka",
			`<div style="display: grid; width: 400px; grid-template-columns: repeat(auto-fit, 50px); justify-content: center">` + item + `</div>`,
			`<div style="display: grid; width: 400px; grid-template-columns: 50px; justify-content: center">` + item + `</div>`},
		{"auto-fill zostawia puste ścieżki",
			`<div style="display: grid; width: 400px; grid-template-columns: repeat(auto-fill, 100px); justify-content: end">` + item + `</div>`,
			`<div style="display: grid; width: 400px; grid-template-columns: repeat(4, 100px); justify-content: end">` + item + `</div>`},
		{"z-index elementu grid bez pozycjonowania",
			`<div style="display: grid; width: 100px"><div style="grid-area: 1 / 1; z-index: 2; background: green; height: 40px"></div><div style="grid-area: 1 / 1; background: red; height: 40px"></div></div>`,
			`<div style="display: grid; width: 100px"><div style="grid-area: 1 / 1; background: red; height: 40px"></div><div style="grid-area: 1 / 1; z-index: 2; background: green; height: 40px"></div></div>`},
	}
	dir := t.TempDir()
	for i, c := range cases {
		got := rasterize(t, renderString(t, c.got).PDF, dir, fmt.Sprintf("got%d", i))
		want := rasterize(t, renderString(t, c.want).PDF, dir, fmt.Sprintf("want%d", i))
		if len(got) != 1 || len(want) != 1 {
			t.Fatalf("%s: strony %d i %d", c.name, len(got), len(want))
		}
		if n := differentPixels(got[0], want[0]); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", c.name, n)
		}
	}
}

// Odstępy w siatce: dawne nazwy grid-gap są aliasami, odstęp procentowy liczy
// się od rozmiaru kontenera, a odstępy nie są wolnym miejscem dla
// justify-content (CSS Box Alignment 3 § 8).
func TestGridGaps(t *testing.T) {
	const items = `<div style="background: #555; height: 30px"></div><div style="background: #555; height: 30px"></div>`
	grid := func(style string) string {
		return `<div style="display: grid; width: 200px; grid-template-columns: 50px 50px; ` + style + `">` + items + `</div>`
	}
	cases := []struct{ name, got, want string }{
		{"grid-gap jako alias", grid(`grid-gap: 20px`), grid(`gap: 20px`)},
		{"grid-column-gap jako alias", grid(`grid-column-gap: 20px`), grid(`column-gap: 20px`)},
		{"odstęp procentowy", grid(`column-gap: 10%`), grid(`column-gap: 20px`)},
		{"space-between z odstępem", grid(`column-gap: 20px; justify-content: space-between`), grid(`column-gap: 100px`)},
	}
	dir := t.TempDir()
	for i, c := range cases {
		got := rasterize(t, renderString(t, c.got).PDF, dir, fmt.Sprintf("got%d", i))
		want := rasterize(t, renderString(t, c.want).PDF, dir, fmt.Sprintf("want%d", i))
		if len(got) != 1 || len(want) != 1 {
			t.Fatalf("%s: strony %d i %d", c.name, len(got), len(want))
		}
		if n := differentPixels(got[0], want[0]); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", c.name, n)
		}
	}
}

// Liczniki odwrócone (CSS Lists 3 § 4.1 i § 4.4.2): counter-reset: reversed(…)
// wyznacza wartość początkową z przyrostów i ustawień w zasięgu licznika,
// a elementy listy zmniejszają go domyślnie o 1.
func TestReversedCounters(t *testing.T) {
	list := func(open, items string) string { return open + items + `</ol>` }
	const three = `<li>a</li><li>b</li><li>c</li>`
	cases := []struct{ name, got, want string }{
		{"reversed w CSS bez atrybutu", list(`<ol style="counter-reset: reversed(list-item)">`, three),
			`<ol><li value="3">a</li><li value="2">b</li><li value="1">c</li></ol>`},
		{"reversed z wartością początkową", list(`<ol style="counter-reset: reversed(list-item) 11">`, three),
			`<ol><li value="10">a</li><li value="9">b</li><li value="8">c</li></ol>`},
		{"przyrost -2", list(`<ol reversed>`, `<li style="counter-increment: list-item -2">a</li><li style="counter-increment: list-item -2">b</li>`),
			`<ol><li value="4">a</li><li value="2">b</li></ol>`}, // ostatni element dostaje wartość swojego przyrostu
		{"counter-set w środku", list(`<ol reversed>`, `<li>a</li><li>b</li><li style="counter-set: list-item 7">c</li><li>d</li>`),
			`<ol><li value="9">a</li><li value="8">b</li><li value="7">c</li><li value="6">d</li></ol>`},
		{"lista zagnieżdżona ma własny licznik", list(`<ol reversed>`, `<li>a<ol><li>x</li><li>y</li></ol></li><li>b</li>`),
			`<ol><li value="2">a<ol><li>x</li><li>y</li></ol></li><li value="1">b</li></ol>`},
	}
	options := Options{Fonts: testFonts(t)}
	for _, c := range cases {
		got, err := Render(t.Context(), []byte(c.got), options)
		if err != nil {
			t.Fatal(err)
		}
		want, err := Render(t.Context(), []byte(c.want), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.PDF, want.PDF) {
			t.Errorf("%s: numeracja różni się od wzorcowej", c.name)
		}
		for _, warning := range got.Warnings {
			t.Errorf("%s: ostrzeżenie %q", c.name, warning.Message)
		}
	}
}

// Kolumna z szerokością procentową zachowuje swój udział także wtedy, gdy
// w tabeli jest wiersz z komórką rozpinającą obie kolumny.
func TestPercentageColumnWithSpanningCell(t *testing.T) {
	const style = `<style>@page { size: 500px 200px; margin: 0 } body { margin: 0 }
table { border-collapse: collapse; width: 400px } td { border: 1px solid black; padding: 0 }
.m { height: 20px; background: #00f }</style>`
	const spanning = `<tr><td colspan="2" style="white-space: nowrap">Lorem ipsum dolor sit amet consectetuer adipiscing</td></tr></table>`
	dir := t.TempDir()
	tested := rasterize(t, renderString(t, style+`<table><tr><td style="width: 90%"><div class="m"></div></td><td></td></tr>`+spanning).PDF, dir, "tested")
	expected := rasterize(t, renderString(t, style+`<table><tr><td style="width: 90%"><div class="m"></div></td><td style="width: 10%"></td></tr>`+spanning).PDF, dir, "expected")
	if len(tested) != 1 || len(expected) != 1 {
		t.Fatalf("strony %d i %d", len(tested), len(expected))
	}
	if n := differentPixels(tested[0], expected[0]); n != 0 {
		t.Errorf("%d pikseli różni się od wzorca", n)
	}
}

// Poprawki układu blokowego i liniowego, list i tabel z rundy CSS 2 (2026-11-15); każdy przypadek
// ma dać ten sam obraz co wzorzec zbudowany z prostszych konstrukcji.
func TestSecondCSS2RoundFixes(t *testing.T) {
	const style = "<style>@page { size: 300px 300px; margin: 0 } body { margin: 0 } " +
		".sq { display: inline-block; width: 20px; height: 20px; background: #080; vertical-align: top }</style>"
	const square = `<div style="width:100px;height:100px;background:#080"></div>`
	const image = `data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='10'%3E%3Crect width='10' height='10' fill='%23080'/%3E%3C/svg%3E`
	cases := []struct{ name, tested, expected string }{
		{
			"min-height zatrzymuje dolny margines dziecka",
			`<div style="width:100px;min-height:50px;background:#080"><div style="height:49px;margin-bottom:10px"></div></div>` +
				`<div style="width:100px;height:50px;background:#080"></div>`,
			square,
		},
		{
			"max-height zatrzymuje dolny margines dziecka",
			`<div style="width:100px;max-height:50px;background:#080"><div style="height:51px;margin-bottom:10px"></div></div>` +
				`<div style="width:100px;height:50px;background:#080"></div>`,
			square,
		},
		{
			"szerokość obrazu z wysokości po max-height",
			`<img style="display:block;height:200px;max-height:100px" src="` + image + `">`,
			`<img style="display:block;width:100px;height:100px" src="` + image + `">`,
		},
		{
			"szerokość obrazu z wysokości po min-height",
			`<img style="display:block;height:0;min-height:100px" src="` + image + `">`,
			`<img style="display:block;width:100px;height:100px" src="` + image + `">`,
		},
		{
			"lewy odstęp pudełka liniowego zaczynającego się od spacji",
			`<div><span style="padding-left:40px"> <b class="sq"></b></span></div>`,
			`<div><span style="padding-left:40px"><b class="sq"></b></span></div>`,
		},
		{
			"lewy odstęp pudełka liniowego zmniejsza miejsce w wierszu",
			`<div style="width:100px;font-size:10px;line-height:20px"><span style="padding-left:40px"><b class="sq"></b> <b class="sq"></b> <b class="sq"></b> <b class="sq"></b></span></div>`,
			`<div style="width:100px;font-size:10px;line-height:20px"><span style="padding-left:40px"><b class="sq"></b> <b class="sq"></b><br><b class="sq"></b> <b class="sq"></b></span></div>`,
		},
		{
			"wysokość procentowa od wysokości po max-height",
			`<div style="width:100px;height:200px;max-height:100px"><div style="height:100%;background:#080"></div></div>`,
			square,
		},
		{
			"marginesy dzieci zostają w podpisie tabeli",
			`<table style="border-spacing:0"><caption style="width:100px;background:#080">` +
				`<div style="margin:10px;height:35px"></div><div style="margin:10px;height:35px"></div></caption></table>`,
			`<table style="border-spacing:0"><caption style="width:100px;height:100px;background:#080"></caption></table>`,
		},
		{
			"margines po pustym pudełku z clear zostaje w rodzicu",
			`<p style="margin:0 0 16px;height:10px"></p><div style="width:100px;background:#080">` +
				`<div style="float:left;height:1px"></div><div style="clear:left"></div><div style="margin-top:99px"></div></div>`,
			`<p style="margin:0 0 16px;height:10px"></p>` + square,
		},
		{
			"marginesy pustego pudełka z clear zwijają się z następnymi",
			`<div style="border-top:1px solid #080;width:100px;background:#080"><div style="float:left;width:50px;height:100px"></div>` +
				`<div style="clear:left;margin:40px 0 80px"></div><div style="margin-bottom:140px"></div></div>`,
			`<div style="width:100px;height:201px;background:#080"></div>`,
		},
	}
	const band = "border:5px solid #00f;border-left:none;border-right:none;"
	const inner = `<span style="display:block;height:20px;background:#080"></span>`
	cases = append(cases, []struct{ name, tested, expected string }{
		{
			"fragment pudełka liniowego po bloku nie powtarza lewego odstępu",
			`<span style="` + band + `padding-left:10px"> ` + inner + ` </span>`,
			`<span style="` + band + `padding-left:10px"></span>` + inner,
		},
		{
			"fragment przed blokiem bez odstępu w osi wiersza nie zajmuje wiersza",
			`<span style="` + band + `padding-right:10px"> ` + inner + ` </span>`,
			inner + `<span style="` + band + `padding-right:10px"></span>`,
		},
		{
			"th jest domyślnie wyśrodkowane",
			`<table><tr><th style="width:200px"><b class="sq"></b></th></tr></table>`,
			`<table><tr><td style="width:200px;font-weight:bold;text-align:center"><b class="sq"></b></td></tr></table>`,
		},
		{
			"th dziedziczy wyrównanie ustawione wyżej",
			`<table style="text-align:right"><tr><th style="width:200px"><b class="sq"></b></th></tr></table>`,
			`<table><tr><td style="width:200px;font-weight:bold;text-align:right"><b class="sq"></b></td></tr></table>`,
		},
		{
			"th wyśrodkowane, gdy wyżej podano wartość początkową",
			`<table style="text-align:start"><tr><th style="width:200px"><b class="sq"></b></th></tr></table>`,
			`<table><tr><td style="width:200px;font-weight:bold;text-align:center"><b class="sq"></b></td></tr></table>`,
		},
		{
			"marginesy zwijają się przez pusty blok z pustym dzieckiem",
			`<div style="display:flow-root;width:100px;background:#080"><div style="margin:50px 0"><div style="margin:50px 0"></div></div>` +
				`<div style="margin:50px 0"></div></div><div style="width:100px;height:50px;background:#080"></div>`,
			square,
		},
		{
			"tabela bez wierszy ma zerową wysokość",
			`<table></table>` + square,
			square,
		},
		{
			"kolumna zadeklarowana przez col istnieje bez komórek",
			`<table style="border-spacing:10px;background:#080"><col style="width:40px"><col style="width:40px"><col style="width:40px">` +
				`<tr><td style="padding:0;height:20px"></td></tr></table>`,
			`<table style="border-spacing:10px;background:#080"><tr><td style="padding:0;height:20px;width:40px"></td>` +
				`<td style="padding:0;width:40px"></td><td style="padding:0;width:40px"></td></tr></table>`,
		},
		{
			"calc z procentem w szerokości kolumny działa jak auto",
			`<table style="border-spacing:0;background:#080"><col style="width:calc(100px + 1%)"><tr><td style="padding:0"><b class="sq"></b></td></tr></table>`,
			`<table style="border-spacing:0;background:#080"><tr><td style="padding:0"><b class="sq"></b></td></tr></table>`,
		},
		{
			"skróty logiczne obramowań",
			`<div style="width:50px;height:20px;border-inline:10px solid #00f;border-block-width:5px 7px;border-block-style:solid;border-block-color:#080"></div>`,
			`<div style="width:50px;height:20px;border-left:10px solid #00f;border-right:10px solid #00f;border-top:5px solid #080;border-bottom:7px solid #080"></div>`,
		},
		{
			"podpis tabeli jest wyśrodkowany",
			`<table style="width:200px"><caption><b class="sq"></b></caption><tr><td></td></tr></table>`,
			`<table style="width:200px"><caption style="text-align:center"><b class="sq"></b></caption><tr><td></td></tr></table>`,
		},
		{
			"obraz data: z SVG wpisanym wprost (spacje i goły znak procentu)",
			`<img style="display:block" src="data:image/svg+xml,%3Csvg viewBox='0 0 100 100' width='100px' xmlns='http://www.w3.org/2000/svg' %3E%3Crect width='100%' height='100%' fill='%23080' /%3E%3C/svg%3E">`,
			square,
		},
		{
			"obraz rozciągnięty w wierszu flex zachowuje proporcje",
			`<div style="display:flex;height:100px"><img src="data:image/svg+xml,%3Csvg viewBox='0 0 200 200' width='200' height='200' xmlns='http://www.w3.org/2000/svg'%3E%3Crect width='200' height='200' fill='%23080'/%3E%3C/svg%3E"></div>`,
			square,
		},
		{
			"obraz rozciągnięty w kolumnie flex zachowuje proporcje",
			`<div style="display:flex;flex-direction:column;width:100px"><img src="data:image/svg+xml,%3Csvg viewBox='0 0 200 200' width='200' height='200' xmlns='http://www.w3.org/2000/svg'%3E%3Crect width='200' height='200' fill='%23080'/%3E%3C/svg%3E"></div>`,
			square,
		},
		{
			"svg z atrybutem height nie jest rozciągany w wierszu flex",
			`<div style="display:flex;height:10px"><svg height="100" width="100" viewBox="0 0 100 100" style="background:#080"></svg></div>`,
			square,
		},
		{
			"tabela jako element flex przyjmuje flex-basis",
			`<div style="display:flex"><div style="display:table;height:100px;background:#080;flex:0 0 100px"></div></div>`,
			square,
		},
		{
			"tabela jako element flex rośnie z flex-grow",
			`<div style="display:flex;width:100px"><table style="border-spacing:0;height:100px;background:#080;flex-grow:1"><tr><td></td></tr></table></div>`,
			square,
		},
		{
			"tabela jako element flex jest rozciągana na wysokość",
			`<div style="display:flex;width:100px;height:100px"><table style="border-spacing:0;background:#080;flex-grow:1"><tr><td></td></tr></table></div>`,
			square,
		},
		{
			"span jako element flex nie dostaje odstępu dwa razy",
			`<div style="display:flex"><span style="padding:0 30px;background:#ccc"><b class="sq"></b></span></div>`,
			`<div style="display:flex"><div style="padding:0 30px;background:#ccc"><b class="sq"></b></div></div>`,
		},
		{
			"flex-basis z box-sizing: border-box obejmuje obramowanie",
			`<div style="display:flex;width:300px"><div style="flex:0 0 100px;box-sizing:border-box;border:10px solid #080;height:100px;background:#080"></div></div>`,
			square,
		},
		{
			"aspect-ratio wyznacza wysokość bloku",
			`<div style="width:100px;aspect-ratio:1/1;background:#080"></div>`,
			square,
		},
		{
			"aspect-ratio z box-sizing: border-box",
			`<div style="width:100px;aspect-ratio:1;box-sizing:border-box;border:10px solid #080;background:#080"></div>`,
			square,
		},
		{
			"aspect-ratio wyznacza szerokość pudełka dopasowanego do treści",
			`<div style="float:left;height:100px;aspect-ratio:1/1;background:#080"></div>`,
			square,
		},
		{
			"aspect-ratio na elemencie grid",
			`<div style="display:grid;grid-template-columns:100px"><div style="aspect-ratio:1/1;background:#080"></div></div>`,
			square,
		},
		{
			"treść wyższa niż wysokość z aspect-ratio powiększa pudełko",
			`<div style="width:100px;aspect-ratio:2/1;background:#080"><div style="height:100px"></div></div>`,
			square,
		},
		{
			"aspect-ratio na obrazie zastępuje proporcję obrazu",
			`<img style="display:block;width:100px;aspect-ratio:1/1" src="data:image/svg+xml,%3Csvg viewBox='0 0 200 100' width='200' height='100' xmlns='http://www.w3.org/2000/svg'%3E%3Crect width='200' height='100' fill='%23080'/%3E%3C/svg%3E">` +
				`<div style="height:20px;background:#00f"></div>`,
			`<img style="display:block;width:100px;height:100px" src="data:image/svg+xml,%3Csvg viewBox='0 0 200 100' width='200' height='100' xmlns='http://www.w3.org/2000/svg'%3E%3Crect width='200' height='100' fill='%23080'/%3E%3C/svg%3E">` +
				`<div style="height:20px;background:#00f"></div>`,
		},
		{
			"canvas zajmuje miejsce według atrybutów",
			`<canvas width="100" height="100" style="display:block;background:#080"></canvas>`,
			square,
		},
		{
			"procentowa wysokość obrazu obok bloku liczy się od rodzica, nie od anonimowego bloku",
			`<div style="width:100px;height:100px"><div></div><canvas width="10" height="10" style="height:100%;width:100px;background:#080"></canvas></div>`,
			square,
		},
		{
			"procent wnuka elementu flex o wysokości z flex-basis liczy się jak auto",
			`<div style="display:flex;flex-direction:column"><div style="flex:1 1 content;height:100px;min-height:0">` +
				`<div style="height:100px;width:100px;background:#080"></div><div style="height:100%;width:100px;background:#f00"></div></div></div>`,
			square,
		},
		{
			"obramowanie zwinięte tabeli leży nad blokiem w komórce",
			`<table style="border-collapse:collapse"><td style="border:50px solid #080;padding:0"><div style="border:50px solid #f00;margin:-50px"></div></td></table>`,
			square,
		},
		{
			"obramowanie zwinięte tabeli zewnętrznej leży nad tabelą zagnieżdżoną",
			`<table style="border-collapse:collapse"><td style="border:50px solid #080;padding:0">` +
				`<table style="border-collapse:collapse;margin:-50px"><td style="border:50px solid #f00;padding:0"></td></table></td></table>`,
			square,
		},
		{
			"wysokość procentowa dziecka komórki o podanej wysokości",
			`<div style="display:table-cell;width:100px;height:100px"><div style="height:100%;background:#080"></div></div>`,
			square,
		},
		{
			"tabela z obramowaniami zwiniętymi oraz elementem pozycjonowanym i inline-block w komórce (bez paniki)",
			`<table style="border-collapse:collapse"><td style="border:50px solid #080;padding:0;line-height:0;font-size:0">` +
				`<div style="position:relative;z-index:1"></div><span style="display:inline-block"></span></td></table>`,
			square,
		},
		{
			"blok po tabeli leży nad jej obramowaniem zwiniętym",
			`<table style="border-collapse:collapse"><td style="border:50px solid #f00;padding:0"></td></table>` +
				`<div style="border:50px solid #080;width:0;margin-top:-100px"></div>`,
			square,
		},
		{
			"pusty inline-flex stoi na linii bazowej wiersza",
			`<div style="font-size:40px;line-height:100px">A<div style="display:inline-flex;width:20px;height:20px;background:#080"></div></div>`,
			`<div style="font-size:40px;line-height:100px">A<div style="display:inline-block;width:20px;height:20px;background:#080"></div></div>`,
		},
		{
			"box-shadow: cień zewnętrzny z przesunięciem",
			`<div style="width:80px;height:80px;background:#fff;box-shadow:20px 20px 0 #080"></div>`,
			`<div style="position:relative;height:100px"><div style="position:absolute;left:20px;top:20px;width:80px;height:80px;background:#080"></div>` +
				`<div style="position:absolute;width:80px;height:80px;background:#fff"></div></div>`,
		},
		{
			"box-shadow: cień wewnętrzny",
			`<div style="width:100px;height:100px;background:#fff;box-shadow:inset 0 0 0 10px #080"></div>`,
			`<div style="box-sizing:border-box;width:100px;height:100px;border:10px solid #080;background:#fff"></div>`,
		},
		{
			"display: contents oddaje dzieci rodzicowi",
			`<div style="display:flex;gap:10px"><div style="display:contents;color:#080"><b class="sq"></b><b class="sq"></b></div><b class="sq"></b></div>`,
			`<div style="display:flex;gap:10px"><b class="sq"></b><b class="sq"></b><b class="sq"></b></div>`,
		},
		{
			"outline-offset odsuwa obrys",
			`<div style="margin:20px;width:50px;height:50px;outline:5px solid #080;outline-offset:10px"></div>`,
			`<div style="margin:5px;width:70px;height:70px;border:5px solid #080"></div>`,
		},
		{
			"counter() z zastrzeżoną nazwą unieważnia deklarację",
			`<style>div::after { content: " x " counter(inherit) }</style><div>7</div>`,
			`<div>7</div>`,
		},
		{
			"lista reversed z własnym counter-reset liczy w górę",
			`<ol reversed style="counter-reset: list-item 3"><li>a<li>b</ol>`,
			`<ol start="4"><li>a<li>b</ol>`,
		},
		{
			"counter-increment: none nie wyłącza numeracji listy",
			`<ol><li>a<li style="counter-increment: none">b<li>c</ol>`,
			`<ol><li>a<li>b<li>c</ol>`,
		},
	}...)
	for _, c := range cases {
		dir := t.TempDir()
		tested := rasterize(t, renderString(t, style+c.tested).PDF, dir, "tested")
		expected := rasterize(t, renderString(t, style+c.expected).PDF, dir, "expected")
		if len(tested) != 1 || len(expected) != 1 {
			t.Errorf("%s: strony %d i %d", c.name, len(tested), len(expected))
			continue
		}
		if n := differentPixels(tested[0], expected[0]); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", c.name, n)
		}
	}
}

// Przełącznik z ADR 0006: domyślnie dolny margines dziecka zostaje w pudełku, którego wysokość zmieniło
// min-height (jak w Chromium); z MarginsEscapeHeightLimits wychodzi poza pudełko (CSS 2.2 § 10.7 dosłownie).
func TestMarginsEscapeHeightLimits(t *testing.T) {
	const style = "<style>@page { size: 300px 300px; margin: 0 } body { margin: 0 } div { width: 100px }</style>"
	const tested = `<div style="min-height:50px;background:#080"><div style="height:40px;margin-bottom:30px"></div></div>` +
		`<div style="height:50px;background:#080"></div>`
	expected := map[bool]string{
		false: `<div style="height:100px;background:#080"></div>`,
		true: `<div style="height:50px;background:#080"></div>` +
			`<div style="height:50px;margin-top:30px;background:#080"></div>`,
	}
	for escape, want := range expected {
		dir := t.TempDir()
		options := Options{Fonts: testFonts(t), MarginsEscapeHeightLimits: escape}
		got, err := Render(t.Context(), []byte(style+tested), options)
		if err != nil {
			t.Fatal(err)
		}
		reference, err := Render(t.Context(), []byte(style+want), options)
		if err != nil {
			t.Fatal(err)
		}
		a, b := rasterize(t, got.PDF, dir, "tested"), rasterize(t, reference.PDF, dir, "expected")
		if len(a) != 1 || len(b) != 1 {
			t.Fatalf("MarginsEscapeHeightLimits=%v: strony %d i %d", escape, len(a), len(b))
		}
		if n := differentPixels(a[0], b[0]); n != 0 {
			t.Errorf("MarginsEscapeHeightLimits=%v: %d pikseli różni się od wzorca", escape, n)
		}
	}
}

// Wymuszony podział strony na elementach flex i grid (CSS Flexbox 1 § 12, CSS Grid 2 § 11): break-before
// i break-after dzielą kontener między liniami wiersza, elementami kolumny i wierszami siatki.
func TestForcedBreaksInFlexAndGrid(t *testing.T) {
	const style = "<style>@page { size: 300px 200px; margin: 10px } div > div { height: 20px }</style>"
	cases := []struct {
		name, html string
		pages      int
	}{
		{"kolumna flex, break-before", `<div style="display:flex;flex-direction:column"><div>a</div><div style="break-before:page">b</div><div>c</div></div>`, 2},
		{"kolumna flex, break-after i break-before", `<div style="display:flex;flex-direction:column"><div style="break-after:page">a</div><div>b</div><div style="break-before:page">c</div></div>`, 3},
		{"wiersz flex z zawijaniem", `<div style="display:flex;flex-wrap:wrap"><div style="width:50%">a</div><div style="width:50%">b</div><div style="width:50%;break-before:page">c</div><div style="width:50%">d</div></div>`, 2},
		{"grid, break-before", `<div style="display:grid"><div>a</div><div style="break-before:page">b</div><div>c</div></div>`, 2},
		{"grid dwukolumnowy, break-after", `<div style="display:grid;grid-template-columns:1fr 1fr"><div>a</div><div>b</div><div style="break-after:page">c</div><div>d</div><div>e</div><div>f</div></div>`, 2},
		{"bez wymuszenia", `<div style="display:flex;flex-direction:column"><div>a</div><div>b</div></div><div style="display:grid"><div>c</div><div>d</div></div>`, 1},
	}
	for _, c := range cases {
		result := renderString(t, style+c.html)
		if result.Pages != c.pages {
			t.Errorf("%s: stron %d, oczekiwano %d", c.name, result.Pages, c.pages)
		}
		text := string(extractText(t, result.PDF))
		for _, letter := range []string{"a", "b", "c"} {
			if !strings.Contains(text, letter) {
				t.Errorf("%s: brak tekstu %q po podziale", c.name, letter)
			}
		}
	}
}

// Składnia i selektory z nowszych modułów CSS, częste w zwykłych arkuszach: każda para ma dać ten sam
// dokument co zapis podstawowy, bez ostrzeżeń o odrzuconej deklaracji.
func TestModernSyntaxEquivalents(t *testing.T) {
	const body = `<div class="a"><p>tekst</p></div>`
	cases := []struct{ name, modern, plain string }{
		{"rgb ze spacjami i alfą", `.a { color: rgb(0 128 0 / 50%) }`, `.a { color: rgba(0, 128, 0, 0.5) }`},
		{"rgb z ułamkami", `.a { color: rgb(25.5 51 0) }`, `.a { color: rgb(10%, 20%, 0%) }`},
		{"hsl z jednostką kąta", `.a { color: hsl(120deg 100% 25%) }`, `.a { color: hsl(120, 100%, 25%) }`},
		{"hsl w obrotach", `.a { color: hsl(0.5turn 100% 50%) }`, `.a { color: hsl(180, 100%, 50%) }`},
		{"skrót inset", `.a { position: relative; inset: 5px 10px }`, `.a { position: relative; top: 5px; right: 10px; bottom: 5px; left: 10px }`},
		{"place-items", `.a { display: grid; place-items: center end }`, `.a { display: grid; align-items: center; justify-items: end }`},
		{"place-content", `.a { display: flex; place-content: center }`, `.a { display: flex; align-content: center; justify-content: center }`},
		{"overflow-x: hidden", `.a { overflow-x: hidden }`, `.a { overflow: hidden }`},
		{"overflow-y: auto jak overflow: auto", `.a { overflow-y: auto }`, `.a { overflow: auto }`},
		{"overflow-x: visible bez skutku", `.a { overflow-x: visible }`, `.a { }`},
		{"position: sticky", `.a { position: sticky; top: 0 }`, `.a { position: relative; top: 0 }`},
		{"float: inline-start", `p { float: inline-start }`, `p { float: left }`},
		{"logiczny promień narożnika", `.a { border: 2px solid; border-start-start-radius: 8px }`, `.a { border: 2px solid; border-top-left-radius: 8px }`},
		{":where bez wagi", `.a p { color: green } :where(.a, .b) p { color: red }`, `.a p { color: green }`},
		{":where dopasowuje", `:where(.a) > p { color: green }`, `.a > p { color: green }`},
		{"@supports spełnione", `@supports (display: grid) { p { color: green } }`, `p { color: green }`},
		{"@supports niespełnione", `p { color: green } @supports (frobnicate: 1px) { p { color: red } }`, `p { color: green }`},
		{"@supports not", `@supports not (frobnicate: 1px) { p { color: green } }`, `p { color: green }`},
		{"@supports and/or", `@supports ((display: grid) and (gap: 1em)) or (frobnicate: 1) { p { color: green } }`, `p { color: green }`},
		{"@supports selector()", `@supports selector(:where(p)) { p { color: green } }`, `p { color: green }`},
		{"word-break: keep-all", `p { word-break: keep-all }`, `p { word-break: normal }`},
		{":has z kombinatorem dziecka", `.a:has(> p) { color: green } .a:has(> span) { color: red }`, `.a { color: green }`},
		{":has z rodzeństwem bez dopasowania", `p:has(+ p) { color: red }`, `p { }`},
		{":nth-child(of S)", `p:nth-child(1 of p) { color: green } p:nth-child(2 of p) { color: red }`, `p { color: green }`},
		{":nth-last-child(of S)", `p:nth-last-child(1 of .a p, p) { color: green }`, `p { color: green }`},
		{":any-link bez odnośników", `p { color: green } :any-link { color: red } p:focus-visible { color: red }`, `p { color: green }`},
		{"gradient z dwiema pozycjami", `.a { background: linear-gradient(to right, red 0 50%, blue 50% 100%) }`,
			`.a { background: linear-gradient(to right, red 0, red 50%, blue 50%, blue 100%) }`},
		{"gradient ze wskazówką", `.a { background: linear-gradient(90deg, red, 30%, blue) }`,
			`.a { background: linear-gradient(90deg, red, rgb(127.5 0 127.5) 30%, blue) }`},
		{"gradient z przestrzenią interpolacji", `.a { background: linear-gradient(in oklch, red, blue) }`,
			`.a { background: linear-gradient(red, blue) }`},
		{"gradient z kierunkiem i przestrzenią", `.a { background: linear-gradient(to right in srgb, red, blue) }`,
			`.a { background: linear-gradient(to right, red, blue) }`},
		{"gradient powtarzany z dwiema pozycjami", `.a { background: repeating-linear-gradient(45deg, red 0 2px, blue 2px 4px) }`,
			`.a { background: repeating-linear-gradient(45deg, red 0, red 2px, blue 2px, blue 4px) }`},
		{"color-mix", `p { color: color-mix(in srgb, red, blue) }`, `p { color: rgb(127.5 0 127.5) }`},
		{"text-shadow none", `p { text-shadow: none }`, `p { }`},
		{"text-shadow przezroczysty", `p { text-shadow: 1px 1px transparent }`, `p { }`},
		{"text-shadow dziedziczony", `.a { text-shadow: 2px 3px red }`, `p { text-shadow: 2px 3px red }`},
		{"zagnieżdżanie z &", `.a { & p { color: green } }`, `.a p { color: green }`},
		{"zagnieżdżanie bez &", `.a { p { color: green } }`, `.a p { color: green }`},
		{"zagnieżdżanie z & na końcu", `p { .a & { color: green } }`, `.a p { color: green }`},
	}
	options := Options{Fonts: testFonts(t)}
	for _, c := range cases {
		modern, err := Render(t.Context(), []byte(`<style>`+c.modern+`</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := Render(t.Context(), []byte(`<style>`+c.plain+`</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(modern.PDF, plain.PDF) {
			t.Errorf("%s: %q dało inny dokument niż %q", c.name, c.modern, c.plain)
		}
		for _, warning := range modern.Warnings {
			if strings.Contains(warning.Message, "Ignored") || strings.Contains(warning.Message, "Unknown rule") ||
				strings.Contains(warning.Message, "unsupported selector") {
				t.Errorf("%s: ostrzeżenie %q", c.name, warning.Message)
			}
		}
	}
}

// Paczka z 2026-11-15: elementy pozycjonowane bezwzględnie w siatce, rozciąganie tylko elementów auto,
// linia bazowa w wierszu siatki, min()/max()/clamp() z procentami oraz funkcje barw. Każdy przypadek
// ma dać ten sam obraz co zapis podstawowy.
func TestGridPositionedAndComparisonFunctions(t *testing.T) {
	const style = "<style>@page { size: 300px 300px; margin: 0 } body { margin: 0 } " +
		".g { display: grid; width: 200px; grid-template-columns: 100px 100px; grid-template-rows: 50px 50px } " +
		".k { background: #080 }</style>"
	cases := []struct{ name, tested, expected string }{
		{
			"element bezwzględny wypełnia wskazany obszar siatki",
			`<div class="g" style="position:relative"><div class="k" style="position:absolute;grid-column:2;grid-row:2;inset:0"></div></div>`,
			`<div class="k" style="width:100px;height:50px;margin:50px 0 0 100px"></div>`,
		},
		{
			"element bezwzględny nie zajmuje komórki siatki",
			`<div class="g" style="position:relative"><div style="position:absolute;width:10px;height:10px"></div>` +
				`<div class="k"></div></div>`,
			`<div class="k" style="width:100px;height:50px"></div>`,
		},
		{
			"element bezwzględny bez linii: blok zawierający to pudełko dopełnienia siatki",
			`<div class="g" style="position:relative;padding:10px"><div class="k" style="position:absolute;inset:0"></div></div>`,
			`<div class="k" style="width:220px;height:120px"></div>`,
		},
		{
			"element siatki o podanej szerokości nie jest rozciągany",
			`<div class="g"><div class="k" style="width:40px;height:20px"></div></div>`,
			`<div class="k" style="width:40px;height:20px"></div>`,
		},
		{
			"element siatki auto wypełnia komórkę",
			`<div class="g"><div class="k"></div></div>`,
			`<div class="k" style="width:100px;height:50px"></div>`,
		},
		{
			"min() z procentem zależy od bloku zawierającego",
			`<div style="width:200px"><div class="k" style="width:min(50%, 150px);height:20px"></div></div>` +
				`<div style="width:300px"><div class="k" style="width:min(50%, 200px);height:20px"></div></div>`,
			`<div class="k" style="width:100px;height:20px"></div><div class="k" style="width:150px;height:20px"></div>`,
		},
		{
			"clamp() i max() z procentem",
			`<div style="width:200px"><div class="k" style="width:clamp(20px, 10%, 50px);height:20px"></div>` +
				`<div class="k" style="width:max(25%, 80px);height:20px"></div></div>`,
			`<div class="k" style="width:20px;height:20px"></div><div class="k" style="width:80px;height:20px"></div>`,
		},
		{
			"kontener flex w kolumnie z zawijaniem: szerokość max-content to suma kolumn",
			`<div class="k" style="display:flex;flex-flow:column wrap;height:100px;width:max-content">` +
				`<div style="width:50px;flex:0 0 100px;min-height:0"></div><div style="width:30px;flex:0 0 100px;min-height:0"></div></div>`,
			`<div class="k" style="width:80px;height:100px"></div>`,
		},
		{
			"kolumna z zawijaniem: linie według order i max-height, z odstępem kolumn",
			`<div class="k" style="display:flex;flex-flow:column wrap;max-height:100px;width:max-content;column-gap:10px">` +
				`<div style="width:50px;height:90px;flex:none;min-height:0"></div>` +
				`<div style="width:50px;height:100px;flex:none;min-height:0;order:2"></div>` +
				`<div style="width:50px;height:10px;flex:none;min-height:0;order:1"></div></div>`,
			`<div class="k" style="width:110px;height:100px"></div>`,
		},
		{
			"linia bazowa powiększa wiersz siatki",
			`<div style="display:grid;grid-template-columns:50px 50px;align-items:baseline;font:10px/1 Ahem;color:#080;width:100px">` +
				`<div>X</div><div style="font-size:20px">X</div><div class="k" style="grid-column:1/3;height:10px"></div></div>`,
			// mały znak stoi na linii bazowej dużego (8 px niżej), wiersz ma 20 px, drugi wiersz zaczyna się pod nim
			`<div style="position:relative;font:10px/1 Ahem;color:#080;height:30px">` +
				`<div style="position:absolute;top:8px;left:0">X</div>` +
				`<div style="position:absolute;top:0;left:50px;font-size:20px">X</div>` +
				`<div class="k" style="position:absolute;top:20px;left:0;width:100px;height:10px"></div></div>`,
		},
		{
			"oklch i lab jako tło",
			`<div style="width:50px;height:50px;background:oklch(51.975% 0.17686 142.495)"></div>` +
				`<div style="width:50px;height:50px;background:lab(46.2775% -47.5621 48.5837)"></div>`,
			`<div style="width:50px;height:100px;background:rgb(0 128 0)"></div>`,
		},
	}
	for _, c := range cases {
		dir := t.TempDir()
		tested := rasterize(t, renderString(t, style+c.tested).PDF, dir, "tested")
		expected := rasterize(t, renderString(t, style+c.expected).PDF, dir, "expected")
		if len(tested) != 1 || len(expected) != 1 {
			t.Errorf("%s: strony %d i %d", c.name, len(tested), len(expected))
			continue
		}
		if n := differentPixels(tested[0], expected[0]); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", c.name, n)
		}
	}
}

// text-shadow (CSS Text Decoration 3 § 4): cień ostry to kopia tekstu przesunięta o podany wektor,
// narysowana pod tekstem. Sprawdzamy to obrazem: czerwony cień o przesunięciu większym niż tekst
// daje ten sam obraz co czerwony tekst w miejscu cienia pod czarnym tekstem.
func TestTextShadowOffsetCopy(t *testing.T) {
	const style = "<style>@page { size: 300px 200px; margin: 0 } body { margin: 0; font: 20px/1 Ahem } " +
		"p { margin: 0; position: absolute; left: 10px; top: 10px }</style>"
	dir := t.TempDir()
	tested := rasterize(t, renderString(t, style+`<p style="text-shadow: 100px 50px red">XX</p>`).PDF, dir, "tested")
	expected := rasterize(t, renderString(t, style+`<p style="left:110px;top:60px;color:red">XX</p><p>XX</p>`).PDF, dir, "expected")
	if len(tested) != 1 || len(expected) != 1 {
		t.Fatalf("strony %d i %d", len(tested), len(expected))
	}
	if n := differentPixels(tested[0], expected[0]); n != 0 {
		t.Errorf("%d pikseli różni się od wzorca", n)
	}
	// cień rozmyty musi zostawić ślad poza obrysem tekstu
	blurred := rasterize(t, renderString(t, style+`<p style="text-shadow: 0 0 10px red">XX</p>`).PDF, dir, "blurred")
	plain := rasterize(t, renderString(t, style+`<p>XX</p>`).PDF, dir, "plain")
	if n := differentPixels(blurred[0], plain[0]); n == 0 {
		t.Error("cień rozmyty nie zmienił obrazu")
	}
}

// Jeden krój to jeden osadzony font, bez względu na liczbę rozmiarów pisma. Błąd wyszedł na fakturze
// z xml.pro: osiem rozmiarów pisma dawało osiem kopii Noto Sans i plik 199 kB zamiast 69 kB.
func TestOneEmbeddedFontPerFace(t *testing.T) {
	var body strings.Builder
	for _, size := range []int{8, 9, 10, 11, 12, 14, 18, 24} {
		fmt.Fprintf(&body, `<p style="font-size:%dpx">Zwykły tekst <b>i gruby</b></p>`, size)
	}
	result := renderString(t, `<style>body { font-family: "Noto Sans" }</style>`+body.String())
	single := renderString(t, `<style>body { font-family: "Noto Sans" }</style><p>Zwykły tekst <b>i gruby</b></p>`)
	want := bytes.Count(single.PDF, []byte("/FontFile2"))
	if n := bytes.Count(result.PDF, []byte("/FontFile2")); n != want || n == 0 {
		t.Errorf("osadzonych fontów: %d, a w dokumencie o jednym rozmiarze pisma %d", n, want)
	}
	// wynik nie może zależeć od tego, co proces renderował wcześniej
	renderString(t, `<p style="font-size:40px;font-family:'Noto Sans'">inny rozmiar jako pierwszy</p>`)
	again := renderString(t, `<style>body { font-family: "Noto Sans" }</style>`+body.String())
	if !bytes.Equal(result.PDF, again.PDF) {
		t.Error("ten sam dokument dał inny plik po renderowaniu innego dokumentu")
	}
}

// ADR 0008: pismo monospace bez podanego rozmiaru ma 13 px; parametr MonospaceFontSize: 16 wyłącza regułę.
// Każda para ma dać ten sam dokument.
func TestMonospaceFontSize(t *testing.T) {
	cases := []struct {
		name, tested, expected string
		size                   float64
	}{
		{"code w akapicie", `<p>a <code>kod</code> b</p>`, `<p>a <span style="font:13px monospace">kod</span> b</p>`, 0},
		{"pre", `<pre style="margin:0">kod</pre>`, `<div style="font:13px monospace;white-space:pre">kod</div>`, 0},
		{"em względem rodzica", `<div style="font-family:monospace"><span style="font-size:2em">x</span></div>`,
			`<div style="font:13px monospace"><span style="font-size:26px">x</span></div>`, 0},
		{"wyjście z monospace", `<div style="font-family:monospace">a<span style="font-family:serif">x</span></div>`,
			`<div style="font:13px monospace">a<span style="font:16px serif">x</span></div>`, 0},
		{"lista rodzin to nie samo monospace", `<div style="font-family:monospace, serif">x</div>`,
			`<div style="font:16px monospace, serif">x</div>`, 0},
		{"rozmiar podany długością", `<div style="font-size:16px"><code>x</code></div>`,
			`<div style="font-size:16px"><span style="font:16px monospace">x</span></div>`, 0},
		{"słowo kluczowe na elemencie", `<code style="font-size:large">x</code>`,
			`<span style="font:15.6px monospace">x</span>`, 0},
		{"reguła wyłączona", `<p>a <code>kod</code> b</p>`, `<p>a <span style="font:16px monospace">kod</span> b</p>`, 16},
	}
	for _, c := range cases {
		options := Options{Fonts: testFonts(t), MonospaceFontSize: c.size}
		tested, err := Render(t.Context(), []byte(c.tested), options)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := Render(t.Context(), []byte(c.expected), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(tested.PDF, expected.PDF) {
			t.Errorf("%s: %q dało inny dokument niż %q", c.name, c.tested, c.expected)
		}
	}
}

// quotes: auto: język dopasowany według BCP 47 i brany z rodzica elementu (CSS Content 3 § 4.1).
func TestAutoQuotesByLanguage(t *testing.T) {
	cases := []struct{ html, want string }{
		{`<p lang="fr-CH"><q>un <q>deux</q></q></p>`, "«un ‹deux›»"},
		{`<p lang="FR"><q>un</q></p>`, "«un»"},
		{`<p lang="fr_CA"><q>un <q>deux</q></q></p>`, "«un ”deux“»"},
		{`<p lang="pl"><q>raz <q>dwa</q></q></p>`, "„raz «dwa»”"},
		{`<p lang="en"><q>one <q lang="pl">two</q></q></p>`, "“one ‘two’”"},
		{`<p lang="xx-Unknown"><q>one</q></p>`, "“one”"},
	}
	for _, c := range cases {
		for range 3 { // wynik nie może zależeć od kolejności przeglądania mapy
			if got := string(extractText(t, renderString(t, c.html).PDF)); got != c.want {
				t.Errorf("%s: %q, oczekiwane %q", c.html, got, c.want)
				break
			}
		}
	}
}

// ADR 0009: szerokości obramowań, konturów i linii między kolumnami są przyciągane do całych pikseli
// (CSS Values 4 § 6.1, piksel urządzenia równy pikselowi CSS, jak w Chromium); ExactLengths to wyłącza.
func TestBorderWidthSnap(t *testing.T) {
	page := func(style string) []byte {
		return []byte(`<div style="width:100px;height:20px;` + style + `">a</div><p>b</p>`)
	}
	cases := []struct {
		name, tested, expected string
		exact, same            bool
	}{
		{"0.5pt rośnie do 1px", "border:0.5pt solid", "border:1px solid", false, true},
		{"0.4pt rośnie do 1px", "border:0.4pt solid", "border:1px solid", false, true},
		{"1.2pt maleje do 1px", "border:1.2pt solid", "border:1px solid", false, true},
		{"1.5pt to dokładnie 2px", "border:1.5pt solid", "border:2px solid", false, true},
		{"2.9px maleje do 2px", "border-top:2.9px solid", "border-top:2px solid", false, true},
		{"zero zostaje zerem", "border:0 solid", "", false, true},
		{"kontur", "outline:0.5pt solid", "outline:1px solid", false, true},
		{"linia między kolumnami", "columns:2;column-rule:0.5pt solid", "columns:2;column-rule:1px solid", false, true},
		{"wyłączone: 0.5pt to nie 1px", "border:0.5pt solid", "border:1px solid", true, false},
		{"wyłączone: 0.75pt to 1px", "border:0.75pt solid", "border:1px solid", true, true},
	}
	for _, c := range cases {
		options := Options{Fonts: testFonts(t), ExactLengths: c.exact}
		tested, err := Render(t.Context(), page(c.tested), options)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := Render(t.Context(), page(c.expected), options)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(tested.PDF, expected.PDF) != c.same {
			t.Errorf("%s: %q wobec %q: zgodność dokumentów %v, oczekiwano %v", c.name, c.tested, c.expected, !c.same, c.same)
		}
	}
}

// CSS Fragmentation 3 § 5.2: górny margines przylegający do zwykłego podziału strony jest obcinany do zera,
// także dla kontenera flex i siatki; po wymuszonym podziale zostaje.
func TestMarginTruncatedAtPageTop(t *testing.T) {
	page := func(display, margin, breakBefore string) []byte {
		return []byte(`<style>@page { size: 200px 200px; margin: 10px } body { margin: 0 }</style>
<div style="height:150px">a</div>
<div style="display:` + display + `;height:100px;break-inside:avoid;margin-top:` + margin + `;break-before:` + breakBefore + `">b</div>`)
	}
	options := Options{Fonts: testFonts(t)}
	render := func(source []byte) []byte {
		result, err := Render(t.Context(), source, options)
		if err != nil {
			t.Fatal(err)
		}
		return result.PDF
	}
	for _, display := range []string{"block", "flex", "grid", "table"} {
		if !bytes.Equal(render(page(display, "40px", "auto")), render(page(display, "0", "auto"))) {
			t.Errorf("display: %s: margines po zwykłym podziale strony nie został obcięty", display)
		}
		if bytes.Equal(render(page(display, "40px", "page")), render(page(display, "0", "page"))) {
			t.Errorf("display: %s: margines po wymuszonym podziale strony zniknął", display)
		}
	}
}

// Siatka z automatycznym rozmieszczaniem ma tyle wierszy, ile zajmują elementy: przy row-gap wysokość
// kontenera to suma wierszy i odstępów między nimi (CSS Grid 1 § 8.5). Dalszy ciąg siatki na następnej
// stronie zaczyna się od góry strony, bez odstępu.
func TestGridImplicitRowsAndGap(t *testing.T) {
	render := func(source string) []byte {
		result, err := Render(t.Context(), []byte(source), Options{Fonts: testFonts(t)})
		if err != nil {
			t.Fatal(err)
		}
		return result.PDF
	}
	items := func(count int) string {
		return strings.Repeat(`<div style="height:20px;background:blue"></div>`, count)
	}
	head := `<style>@page { size: 300px 400px; margin: 20px } body { margin: 0 }</style>`
	grid := head + `<div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:10px">` + items(15) + `</div><p style="margin:0">po</p>`
	// 5 wierszy po 20 px i 4 odstępy po 10 px: 140 px
	block := head + `<div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:10px;height:140px">` + items(15) + `</div><p style="margin:0">po</p>`
	if !bytes.Equal(render(grid), render(block)) {
		t.Error("siatka 5 wierszy z odstępem 10 px nie ma wysokości 140 px")
	}
	// 3 wiersze po 100 px mieszczą się na stronie 360 px (320 px z odstępami), czwarty zaczyna drugą stronę
	tall := func(marker string) string {
		return head + `<div style="display:grid;gap:10px">` +
			strings.Repeat(`<div style="height:100px"></div>`, 3) + `<div style="height:100px">x</div></div>` + marker
	}
	// wzorzec też z elementami siatki: są rysowane jak inline-block, więc zapis różni się od zwykłych bloków
	plain := head + `<div style="display:grid;gap:10px">` + strings.Repeat(`<div style="height:100px"></div>`, 3) +
		`</div><div style="display:grid;break-before:page"><div style="height:100px">x</div></div>`
	if !bytes.Equal(render(tall("")), render(plain)) {
		t.Error("dalszy ciąg siatki nie zaczyna się od góry następnej strony")
	}
}

// Siatka: element z podanym wierszem i kolumną auto trafia do pierwszej wolnej kolumny; ścieżki
// minmax(…, auto) są rozciągane; odstęp calc() z procentem; szerokość inline-grid wynika z kolumn.
func TestGridPlacementStretchGapAndIntrinsicWidth(t *testing.T) {
	cell := `<div style="background:green"></div>`
	cases := []struct{ name, tested, expected string }{
		{"wiersz podany, kolumna auto",
			`<div style="display:grid;width:100px;grid-template-columns:50px 50px;grid-template-rows:50px"><div style="grid-row:1;background:green"></div></div>`,
			`<div style="display:grid;width:100px;grid-template-columns:50px 50px;grid-template-rows:50px"><div style="grid-row:1;grid-column:1;background:green"></div></div>`},
		{"rozciąganie minmax(25px, auto)",
			`<div style="display:grid;width:200px;height:100px;grid-template-columns:minmax(25px,auto) minmax(25px,auto)">` + cell + cell + `</div>`,
			`<div style="display:grid;width:200px;height:100px;grid-template-columns:100px 100px">` + cell + cell + `</div>`},
		{"minmax(auto, 50px) nie rośnie",
			`<div style="display:grid;width:200px;height:100px;grid-template-columns:minmax(auto,50px) minmax(auto,50px)">` + cell + cell + `</div>`,
			`<div style="display:grid;width:200px;height:100px;grid-template-columns:50px 50px">` + cell + cell + `</div>`},
		{"odstęp calc() z procentem",
			`<div style="display:grid;width:200px;height:220px;gap:calc(15% + 7px) calc(10px + 5%);grid-template-columns:90px 90px;grid-template-rows:90px 90px">` + cell + cell + cell + cell + `</div>`,
			`<div style="display:grid;width:200px;height:220px;gap:40px 20px;grid-template-columns:90px 90px;grid-template-rows:90px 90px">` + cell + cell + cell + cell + `</div>`},
		{"szerokość inline-grid",
			`<div style="display:inline-grid;border:2px solid;gap:16px;grid-template-columns:40px 40px;grid-template-rows:40px">` + cell + cell + `</div>`,
			`<div style="display:inline-grid;border:2px solid;gap:16px;grid-template-columns:40px 40px;grid-template-rows:40px;width:96px">` + cell + cell + `</div>`},
		{"odstęp procentowy w inline-grid",
			`<div style="display:inline-grid;border:2px solid;gap:20%;grid-template-columns:40px 40px;grid-template-rows:40px 40px">` + cell + cell + cell + cell + `</div>`,
			`<div style="display:inline-grid;border:2px solid;gap:16px;grid-template-columns:40px 40px;grid-template-rows:40px 40px;width:80px;height:80px">` + cell + cell + cell + cell + `</div>`},
	}
	for _, c := range cases {
		tested, err := Render(t.Context(), []byte(c.tested), Options{Fonts: testFonts(t)})
		if err != nil {
			t.Fatal(err)
		}
		expected, err := Render(t.Context(), []byte(c.expected), Options{Fonts: testFonts(t)})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(tested.PDF, expected.PDF) {
			t.Errorf("%s: inny dokument niż wzorzec", c.name)
		}
	}
}

// Paczka 2026-11-16 (druga): element siatki rozpięty na kilka kolumn mieści się w ścieżkach,
// obraz w siatce zachowuje proporcje,
// vh przy @page { height } liczy się od obszaru strony.
func TestGridSpanAndReplacedItems(t *testing.T) {
	cases := []struct{ name, tested, expected string }{
		{"element rozpięty na trzy kolumny auto",
			`<div style="display:grid;float:left;grid-template-columns:auto auto auto"><div style="width:20px;height:10px;background:blue"></div><div style="grid-column:3;width:20px;height:10px;background:blue"></div><div style="grid-column:span 3;width:120px;height:10px;background:green"></div></div>`,
			`<div style="display:grid;float:left;width:120px;grid-template-columns:20px 80px 20px"><div style="width:20px;height:10px;background:blue"></div><div style="grid-column:3;width:20px;height:10px;background:blue"></div><div style="grid-column:span 3;width:120px;height:10px;background:green"></div></div>`},
		{"svg w siatce zachowuje proporcje",
			`<div style="display:grid;grid-template-columns:200px"><svg style="background:green;height:100px" viewBox="0 0 1 1"></svg></div>`,
			`<div style="display:grid;grid-template-columns:200px"><svg style="background:green;height:100px;width:100px" viewBox="0 0 1 1"></svg></div>`},
		{"svg z justify-self: stretch",
			`<div style="display:grid;grid-template-columns:200px"><svg style="background:green;height:100px;justify-self:stretch" viewBox="0 0 1 1" preserveAspectRatio="none"></svg></div>`,
			`<div style="display:grid;grid-template-columns:200px"><svg style="background:green;height:100px;width:200px" viewBox="0 0 1 1" preserveAspectRatio="none"></svg></div>`},
		{"vh przy @page height",
			`<style>@page { margin: 0; width: 300px; height: 200px } body { margin: 0 }</style><div style="height:100vh;background:green"></div>`,
			`<style>@page { margin: 0; width: 300px; height: 200px } body { margin: 0 }</style><div style="height:200px;background:green"></div>`},
	}
	for _, c := range cases {
		tested, err := Render(t.Context(), []byte(c.tested), Options{Fonts: testFonts(t)})
		if err != nil {
			t.Fatal(err)
		}
		expected, err := Render(t.Context(), []byte(c.expected), Options{Fonts: testFonts(t)})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(tested.PDF, expected.PDF) {
			t.Errorf("%s: inny dokument niż wzorzec", c.name)
		}
	}
}

// TestFirstLineFloatWidthAndGridDescendants pilnuje poprawek z 2026-11-16: `::first-line` zmienia styl
// treści pierwszego wiersza (wcześniej był pomijany), szerokość max-content sumuje stojące obok siebie
// elementy pływające, dalszy potomek pozycjonowany wskazuje obszar siatki właściwościami grid-column,
// a pozycjonowanie względem komórki tabeli liczy się od jej wysokości w wierszu.
func TestFirstLineFloatWidthAndGridDescendants(t *testing.T) {
	float := `<div style="float:left;width:20px;height:40px;background:green"></div>`
	cases := []struct{ name, tested, expected string }{
		{"::first-line zmienia kolor pierwszego wiersza",
			`<style>p::first-line { color: green }</style><p>pierwszy<br>drugi</p>`,
			`<style>span { color: green }</style><p><span>pierwszy</span><br>drugi</p>`},
		{"::first-line przodka obejmuje pierwszy wiersz bloku zagnieżdżonego",
			`<style>section::first-line { color: green }</style><section><p>pierwszy<br>drugi</p><p>trzeci</p></section>`,
			`<style>span { color: green }</style><section><p><span>pierwszy</span><br>drugi</p><p>trzeci</p></section>`},
		{"dwa elementy pływające w kontenerze o szerokości z treści",
			`<div style="position:absolute;background:red">` + float + float + `</div>`,
			`<div style="position:absolute;background:red;width:40px">` + float + float + `</div>`},
		{"clear zaczyna nowy rząd elementów pływających",
			`<div style="position:absolute;background:red">` + float + `<div style="clear:left"></div>` + float + `</div>`,
			`<div style="position:absolute;background:red;width:20px">` + float + `<div style="clear:left"></div>` + float + `</div>`},
		{"bottom względem komórki tabeli o podanej wysokości",
			`<table style="border-spacing:0"><tr><td style="position:relative;height:100px;width:50px;padding:0"><div style="position:absolute;bottom:0;left:0;width:10px;height:10px;background:green"></div></td></tr></table>`,
			`<table style="border-spacing:0"><tr><td style="position:relative;height:100px;width:50px;padding:0"><div style="position:absolute;top:90px;left:0;width:10px;height:10px;background:green"></div></td></tr></table>`},
		{"bottom względem komórki rozciągniętej przez sąsiednią",
			`<table style="border-spacing:0"><tr><td style="position:relative;width:50px;padding:0;vertical-align:top"><div style="position:absolute;bottom:0;left:0;width:10px;height:10px;background:green"></div></td><td style="padding:0"><div style="height:80px;width:10px"></div></td></tr></table>`,
			`<table style="border-spacing:0"><tr><td style="position:relative;width:50px;padding:0;vertical-align:top"><div style="position:absolute;top:70px;left:0;width:10px;height:10px;background:green"></div></td><td style="padding:0"><div style="height:80px;width:10px"></div></td></tr></table>`},
		{"dalszy potomek pozycjonowany w obszarze siatki",
			`<div style="position:relative;display:grid;grid:40px/10px 33px;width:43px"><div><div style="position:absolute;grid-column:2/3;left:0;width:100%;height:10px;background:green"></div></div></div>`,
			`<div style="position:relative;display:grid;grid:40px/10px 33px;width:43px"><div><div style="position:absolute;left:10px;width:33px;height:10px;background:green"></div></div></div>`},
	}
	for _, c := range cases {
		tested, err := Render(t.Context(), []byte(c.tested), Options{Fonts: testFonts(t)})
		if err != nil {
			t.Fatal(err)
		}
		expected, err := Render(t.Context(), []byte(c.expected), Options{Fonts: testFonts(t)})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(tested.PDF, expected.PDF) {
			t.Errorf("%s: inny dokument niż wzorzec", c.name)
		}
	}
}

// TestFlexRowPushAndFixedHeightFragments pilnuje poprawek podziału między strony z 2026-11-16: blok
// o podanej wysokości dzielony razem z dziećmi przenosi na następną stronę tylko resztę wysokości,
// element wiersza flex, który nie mieści się na stronie, przechodzi na następną sam, a żądanie podziału
// na pierwszym elemencie zagnieżdżonego kontenera flex dzieli strony przed tym kontenerem.
func TestFlexRowPushAndFixedHeightFragments(t *testing.T) {
	page := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 } .i { width: 50px; height: 60px; background: green }</style>`
	cases := []struct {
		name, tested, expected string
		pages                  int
	}{
		{"blok o podanej wysokości z dziećmi dzielonymi między strony",
			page + `<div style="height:50px"></div><div style="height:250px;background:silver"><div style="height:100px"></div><div style="height:100px"></div></div><div style="height:20px;background:blue"></div>`,
			page + `<div style="height:50px"></div><div style="height:150px;background:silver"><div style="height:100px"></div></div><div style="height:100px;background:silver"><div style="height:100px"></div></div><div style="height:20px;background:blue"></div>`,
			2},
		{"element wiersza flex przechodzi na następną stronę sam",
			page + `<div style="height:100px"></div><div style="display:flex;align-items:flex-start"><div class="i"></div><div class="i" style="height:150px"></div></div>`,
			page + `<div style="height:100px"></div><div style="display:flex;align-items:flex-start"><div class="i"></div></div><div style="display:flex;align-items:flex-start;break-before:page"><div class="i" style="height:0;visibility:hidden"></div><div class="i" style="height:150px"></div></div>`,
			2},
		{"break-before na pierwszym elemencie zagnieżdżonego kontenera flex",
			page + `<div style="display:flex;flex-direction:column"><div style="display:flex;flex-direction:column"><div class="i"></div></div><div style="display:flex;flex-direction:column"><div class="i" style="break-before:page"></div></div></div>`,
			page + `<div style="display:flex;flex-direction:column"><div style="display:flex;flex-direction:column"><div class="i"></div></div><div style="display:flex;flex-direction:column;break-before:page"><div class="i"></div></div></div>`,
			2},
	}
	for _, c := range cases {
		tested, err := Render(t.Context(), []byte(c.tested), Options{Fonts: testFonts(t)})
		if err != nil {
			t.Fatal(err)
		}
		expected, err := Render(t.Context(), []byte(c.expected), Options{Fonts: testFonts(t)})
		if err != nil {
			t.Fatal(err)
		}
		if tested.Pages != c.pages {
			t.Errorf("%s: %d stron, oczekiwano %d", c.name, tested.Pages, c.pages)
		}
		if !bytes.Equal(tested.PDF, expected.PDF) {
			t.Errorf("%s: inny dokument niż wzorzec", c.name)
		}
	}
}

// TestBorderSpacingTruncation pilnuje ADR 0013: border-spacing jest domyślnie obcinane do całych pikseli
// jak w Chromium, a ExactLengths zostawia wartość z arkusza.
func TestBorderSpacingTruncation(t *testing.T) {
	table := func(spacing string) []byte {
		return []byte(`<table style="border-spacing:` + spacing + `"><tr><td style="background:green;padding:0;width:20px;height:20px"></td><td style="background:green;padding:0;width:20px;height:20px"></td></tr></table>`)
	}
	render := func(source []byte, exact bool) []byte {
		result, err := Render(t.Context(), source, Options{Fonts: testFonts(t), ExactLengths: exact})
		if err != nil {
			t.Fatal(err)
		}
		return result.PDF
	}
	if !bytes.Equal(render(table("1mm"), false), render(table("3px"), false)) {
		t.Error("border-spacing: 1mm nie daje 3px")
	}
	if !bytes.Equal(render(table("3.9px 0.6px"), false), render(table("3px 0px"), false)) {
		t.Error("border-spacing: 3.9px 0.6px nie daje 3px 0px")
	}
	if bytes.Equal(render(table("1mm"), true), render(table("3px"), true)) {
		t.Error("ExactLengths: border-spacing 1mm zostało obcięte")
	}
}

// TestShrinkContentWiderThanPage pilnuje ADR 0014: treść szersza niż obszar strony jest pomniejszana
// (domyślnie najwyżej 1,5 raza, jak w Chromium), MaxShrink: 1 to wyłącza, a pola marginesów zostają
// w skali 1.
func TestShrinkContentWiderThanPage(t *testing.T) {
	page := `<style>@page { size: 400px 300px; margin: 50px; @top-center { content: "N" } } body { margin: 0 }</style>`
	wide := func(width string) string {
		return page + `<div style="width:` + width + `;height:60px;background:green"></div>` +
			`<div style="height:150px;background:silver"></div><div style="height:150px;background:gray"></div>`
	}
	render := func(source string, shrink float64) Result {
		result, err := Render(t.Context(), []byte(source), Options{Fonts: testFonts(t), MaxShrink: shrink})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	// trzy bloki (60, 150 i 150 px) na stronie 200 px: 3 strony, bo bloki o podanej wysokości nie są
	// dzielone; przy pomniejszeniu 1,5 raza (strona 300 px) 2 strony, przy 2 razy (400 px) jedna
	if fits := render(wide("300px"), 0); fits.Pages != 3 {
		t.Errorf("treść mieszcząca się na szerokość: %d stron, oczekiwano 3", fits.Pages)
	}
	if !bytes.Equal(render(wide("300px"), 0).PDF, render(wide("300px"), 1).PDF) {
		t.Error("treść mieszcząca się na szerokość zmienia się pod wpływem MaxShrink")
	}
	if bytes.Equal(render(wide("450px"), 0).PDF, render(wide("450px"), 1).PDF) {
		t.Error("treść szersza niż strona nie została pomniejszona")
	}
	if limited, unlimited := render(wide("600px"), 0), render(wide("600px"), math.Inf(1)); limited.Pages != 2 || unlimited.Pages != 1 {
		t.Errorf("treść dwa razy za szeroka: %d stron z limitem 1,5 i %d bez limitu, oczekiwano 2 i 1", limited.Pages, unlimited.Pages)
	}
	// pomniejszenie 1,5 raza: pas 450 px na 60 px wygląda jak pas 300 px na 40 px na stronie bez skali
	dir := t.TempDir()
	shrunk := rasterize(t, render(page+`<div style="width:450px;height:60px;background:green"></div>`, 0).PDF, dir, "shrunk")
	plain := rasterize(t, render(page+`<div style="width:300px;height:40px;background:green"></div>`, 1).PDF, dir, "plain")
	if len(shrunk) != 1 || len(plain) != 1 {
		t.Fatalf("strony %d i %d", len(shrunk), len(plain))
	}
	if n := differentPixels(shrunk[0], plain[0]); n != 0 {
		t.Errorf("pomniejszony pas różni się od wzorca o %d pikseli", n)
	}
}

// TestRealDocumentFindings pilnuje błędów znalezionych 2026-11-16 przy porównaniu korpusu z wydrukiem
// Chromium: justowany wiersz o tym samym tekście rysowany drugi raz dostawał odstępy podwójnie i wystawał
// za margines, pusty element liniowy między blokami rozrywał zwijanie marginesów, a znacznik listy
// wewnątrz elementu był węższy niż w przeglądarce.
func TestRealDocumentFindings(t *testing.T) {
	head := `<style>@page { size: 300px 150px; margin: 10px } body { margin: 0; font: 12px/1.3 sans-serif }</style>`
	dir := t.TempDir()

	paragraph := `<p style="text-align:justify;margin:0">Zapis uznaje się za kompletny dopiero po złożeniu podpisów przez osobę wykonującą, sprawdzającą i zatwierdzającą wynik.</p>`
	twice := rasterize(t, renderString(t, head+paragraph+`<div style="break-before:page"></div>`+paragraph).PDF, dir, "justify")
	if len(twice) != 2 {
		t.Fatalf("justowanie: %d stron, oczekiwano 2", len(twice))
	}
	if n := differentPixels(twice[0], twice[1]); n != 0 {
		t.Errorf("ten sam justowany akapit na dwóch stronach różni się o %d pikseli", n)
	}

	blocks := `<h3 style="margin:10px 0 4px">A</h3>%s<ul style="margin:10px 0"></ul><p style="margin:0">T</p>`
	withSpan := rasterize(t, renderString(t, head+fmt.Sprintf(blocks, `<p style="margin:0 0 8px"></p><span></span>`)).PDF, dir, "span")
	withoutSpan := rasterize(t, renderString(t, head+fmt.Sprintf(blocks, `<p style="margin:0 0 8px"></p>`)).PDF, dir, "nospan")
	if n := differentPixels(withSpan[0], withoutSpan[0]); n != 0 {
		t.Errorf("pusty <span> między blokami zmienia układ: %d pikseli", n)
	}

	// znacznik-symbol wewnątrz elementu zajmuje 1,36 em (pomiar Chromium), niezależnie od glifu
	inside := rasterize(t, renderString(t, head+`<ul style="list-style-position:inside;margin:0;padding:0;font-size:20px"><li>Abc</li></ul>`).PDF, dir, "inside")
	fixed := rasterize(t, renderString(t, head+`<div style="font-size:20px"><span style="display:inline-block;width:27.2px">•</span>Abc</div>`).PDF, dir, "fixed")
	if n := differentPixels(inside[0], fixed[0]); n != 0 {
		t.Errorf("znacznik listy wewnątrz elementu: %d pikseli różni się od wzorca o stałej szerokości", n)
	}
}

// Kolumny (CSS Multicol 1): element rozpinający w głębi drzewa, dzielenie bloków o podanej wysokości
// między kolumny, górny margines kontenera, style linii między kolumnami oraz contain: size.
func TestColumnsSpannersSlicingAndContain(t *testing.T) {
	head := `<style>@page { size: 300px 300px; margin: 0 } body { margin: 0 }
		.mc { columns: 2; column-gap: 0; width: 100px } .g { background: green }</style>`
	dir := t.TempDir()
	same := func(name, test, reference string) {
		t.Helper()
		got := rasterize(t, renderString(t, head+test).PDF, dir, name)
		want := rasterize(t, renderString(t, head+reference).PDF, dir, name+"-ref")
		if len(got) != len(want) {
			t.Errorf("%s: %d stron, wzorzec ma %d", name, len(got), len(want))
			return
		}
		if n := differentPixels(got[0], want[0]); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	square := `<div class="g" style="width:100px;height:100px"></div>`

	// blok 200px rozkłada się na dwie kolumny po 100px
	same("slice", `<div class="mc"><div class="g" style="height:200px"></div></div>`, square)
	// element rozpinający zagnieżdżony w zwykłym bloku dzieli przodka i rozpina obie kolumny
	same("nested-spanner", `<div class="mc"><div class="g" style="height:60px"></div>
		<div><div class="g" style="column-span:all;height:70px"></div></div></div>`, square)
	// sąsiednie elementy rozpinające zwijają marginesy: 10 + 80 + 10 = 100
	same("spanner-margins", `<div class="mc g"><div>
		<div style="column-span:all;height:10px;margin-bottom:50px"></div>
		<div style="column-span:all;height:10px;margin-top:80px"></div></div></div>`, square)
	// grupa pudełek, których nie wolno rozdzielić, wyznacza krok równoważenia
	same("avoid-group", `<div class="mc g"><div style="height:25px"></div><div style="height:50px"></div>
		<div style="height:25px;break-before:avoid"></div><div style="height:50px"></div></div>`, square)
	// contain: size: pudełko niepodzielne, wysokość auto jak dla pudełka bez treści
	same("contain-monolithic", `<div style="width:100px;height:100px;overflow:hidden"><div class="mc" style="width:200px">
		<div class="g" style="contain:size;height:100px"></div><div style="contain:size;height:100px"></div></div></div>`, square)
	same("contain-auto-height", square+`<div style="contain:size"><div style="width:100px;height:50px;background:red"></div></div>`+
		`<div class="g" style="width:100px;height:50px"></div>`, square+`<div class="g" style="width:100px;height:50px"></div>`)
	// górny margines kontenera kolumn zwija się z marginesem poprzednika
	same("container-margin", `<div style="height:10px;margin-bottom:10px"></div>
		<div class="mc" style="margin-top:30px"><div class="g" style="height:200px"></div></div>`,
		`<div style="height:40px"></div>`+square)
	// inset i outset na linii między kolumnami to ridge i groove
	rule := `<div style="columns:2;column-gap:20px;width:220px;column-rule:10px %s lime;background:black"><div style="height:100px"></div></div>`
	same("rule-inset", fmt.Sprintf(rule, "inset"), fmt.Sprintf(rule, "ridge"))
	same("rule-outset", fmt.Sprintf(rule, "outset"), fmt.Sprintf(rule, "groove"))
}

// Kolumny dzielone między strony i wysokości w kolumnach (CSS Multicol 1, CSS Break 3); oczekiwane
// położenia zmierzone na wydruku Chromium 157 (2026-11-16).
func TestColumnsAcrossPagesAndHeights(t *testing.T) {
	head := `<style>@page { size: 400px 300px; margin: 0 } body { margin: 0 }
		.two { columns: 2; column-gap: 20px } p { margin: 0 0 8px; font: 10px/14px sans-serif }</style>`
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
	box := func(height int) string {
		return fmt.Sprintf(`<div style="height:%dpx;background:green;margin:0 0 10px"></div>`, height)
	}
	col := func(left int, content string) string {
		return fmt.Sprintf(`<div style="position:absolute;left:%dpx;width:190px">%s</div>`, left, content)
	}
	filler := `<div style="height:200px;margin-bottom:10px"></div>`

	// Bloki 60px z marginesem 10px, 90px miejsca na pierwszej stronie: blok bez obramowania jest
	// przecinany na dole kolumny także wtedy, gdy stoi pod marginesem poprzednika.
	same("slice-at-page-end", filler+`<div class="two">`+strings.Repeat(box(60), 7)+`</div>`,
		filler+`<div style="position:relative">`+col(0, box(60)+box(20))+col(210, box(40)+box(40))+`</div>`+
			`<div style="break-before:page;position:relative">`+col(0, box(20)+box(60)+box(55))+col(210, box(5)+box(60)+box(60))+`</div>`)

	// Przed wymuszonym podziałem kolumny dolny margines zostaje: kolumny mają 2 x (42 + 8) = 100px.
	same("forced-break-margin", `<div class="two" style="background:green"><p>a<br>b<br>c</p><p>d<br>e<br>f</p>`+
		`<p style="break-before:column">g</p><p>h</p></div>`,
		`<div class="two" style="background:green;height:100px;column-fill:auto"><p>a<br>b<br>c</p><p>d<br>e<br>f</p>`+
			`<p style="break-before:column">g</p><p>h</p></div>`)

	// Kontener kolumn o podanej wysokości zaczyna się w reszcie strony i kończy na następnej.
	fixed := `<div class="two" style="height:160px;column-fill:auto;background:green">%s</div><div style="height:20px;background:blue"></div>`
	same("fixed-height-container", filler+fmt.Sprintf(fixed, strings.Repeat(`<div style="height:80px"></div>`, 4)),
		filler+`<div style="height:90px;background:green"></div><div style="break-before:page;height:70px;background:green"></div>`+
			`<div style="height:20px;background:blue"></div>`)

	// Wysokość procentowa dziecka liczy się od podanej wysokości kontenera kolumn: 50% z 200px
	// rozkłada się po 50px na kolumnę.
	same("percentage-height", `<div class="two" style="height:200px;column-gap:0;width:200px"><div style="height:50%;background:green"></div></div>`,
		`<div style="width:200px;height:50px;background:green"></div>`)

	// Przodek o podanej wysokości podzielony elementem rozpinającym: fragment przed elementem ma
	// wysokość treści, ostatni resztę podanej wysokości (250 - 100 - 100 = 50).
	same("split-ancestor-height", `<div style="columns:1;width:200px"><div style="height:250px;background:green">`+
		`<div style="height:100px"></div><div style="column-span:all;height:50px;background:blue"></div>`+
		`<div style="height:100px"></div><div style="column-span:all;height:50px;background:blue"></div></div></div>`,
		`<div style="width:200px"><div style="height:100px;background:green"></div><div style="height:50px;background:blue"></div>`+
			`<div style="height:100px;background:green"></div><div style="height:50px;background:blue"></div><div style="height:50px;background:green"></div></div>`)
}

// Wiersz tabeli na granicy stron: gdy któraś komórka nie zmieściłaby na stronie żadnej treści (trzy
// wiersze tekstu przy domyślnych orphans i widows), cały wiersz przechodzi na następną stronę, jak
// w przeglądarkach. Znalezione na fakturze KSeF z 60 pozycjami (2026-11-16): numer i kwoty zostawały
// na dole strony, a nazwa pozycji lądowała na następnej.
func TestTableRowMovesWholeWhenCellWouldBeEmpty(t *testing.T) {
	html := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0; font: 10px/20px sans-serif }
		table { border-collapse: collapse; width: 100% } td { padding: 0; vertical-align: top }</style>
		<table>` + strings.Repeat(`<tr><td>POPRZEDNI</td><td>x</td></tr>`, 8) +
		`<tr><td>NUMER</td><td>pierwszy<br>drugi<br>trzeci</td></tr></table>`
	result := renderString(t, html)
	if result.Pages != 2 {
		t.Fatalf("stron: %d, oczekiwane 2", result.Pages)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "row.pdf")
	if err := os.WriteFile(file, result.PDF, 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := exec.Command("pdftotext", "-f", "1", "-l", "1", file, "-").Output()
	if err != nil {
		t.Skip("brak pdftotext")
	}
	if strings.Contains(string(first), "NUMER") {
		t.Errorf("komórka NUMER została na pierwszej stronie, choć treść sąsiedniej komórki przeszła na drugą")
	}
	second, _ := exec.Command("pdftotext", "-f", "2", "-l", "2", file, "-").Output()
	for _, word := range []string{"NUMER", "pierwszy", "trzeci"} {
		if !strings.Contains(string(second), word) {
			t.Errorf("na drugiej stronie brakuje %q", word)
		}
	}
}

// Białe znaki (CSS Text 3 § 2.1, § 3, § 4.1.3): znaki sterujące są widoczne, spacje zachowane przez
// pre-wrap wiszą na końcu zawiniętego wiersza, a przy break-spaces zajmują miejsce i przechodzą dalej.
func TestWhiteSpaceControlsHangingAndBreakSpaces(t *testing.T) {
	head := `<style>@page { size: 200px 120px; margin: 0 } body { margin: 0; font: 20px/24px sans-serif }
		div { width: 70px }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		pages := rasterize(t, renderString(t, head+body).PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron, oczekiwana 1", name, len(pages))
		}
		return pages[0]
	}

	if n := differentPixels(page("control", "<p>a\u0001b</p>"), page("control-ref", `<p>ab</p>`)); n == 0 {
		t.Error("znak sterujący U+0001 nie jest widoczny")
	}

	spaces := strings.Repeat(" ", 12)
	wrapped := page("pre-wrap", `<div style="white-space:pre-wrap;text-align:right">aa`+spaces+`bb</div>`)
	if n := differentPixels(wrapped, page("pre-wrap-ref", `<div style="text-align:right">aa<br>bb</div>`)); n != 0 {
		t.Errorf("pre-wrap: spacje na końcu zawiniętego wiersza przesuwają wyrównanie (%d pikseli)", n)
	}

	broken := page("break-spaces", `<div style="white-space:break-spaces">aa`+spaces+`bb</div>`)
	if n := differentPixels(broken, page("break-spaces-ref", `<div>aa<br>bb</div>`)); n == 0 {
		t.Error("break-spaces: spacje nie zajmują miejsca (wynik jak bez nich)")
	}
	// każdy wiersz mieści się w szerokości pudełka: na prawo od niego nie ma treści
	if n := differentPixels(broken, page("break-spaces-clip", `<div style="white-space:break-spaces;overflow:hidden">aa`+spaces+`bb</div>`)); n != 0 {
		t.Errorf("break-spaces: treść wystaje poza szerokość pudełka (%d pikseli)", n)
	}
}

// background-clip: text (CSS Backgrounds 4 § 3.6): tło jest widoczne tylko w kształcie liter elementu.
// Wzorzec to ten sam tekst w kolorze tła; maska i zwykłe wypełnienie mogą różnić się wygładzaniem
// krawędzi, więc dopuszczamy niewielką liczbę różnych pikseli (tło namalowane w całym pudełku daje
// ich kilkadziesiąt tysięcy).
func TestBackgroundClipText(t *testing.T) {
	head := `<style>@page { size: 400px 120px; margin: 0 } body { margin: 10px; font: bold 40px sans-serif }</style>`
	dir := t.TempDir()
	clipped := rasterize(t, renderString(t, head+`<div style="background:#c60;background-clip:text;color:transparent">Tekst w tle</div>`).PDF, dir, "clip")
	plain := rasterize(t, renderString(t, head+`<div style="color:#c60">Tekst w tle</div>`).PDF, dir, "plain")
	if n := differentPixels(clipped[0], plain[0]); n < 0 || n > 1500 {
		t.Errorf("tło przycięte do tekstu różni się od tekstu w kolorze tła o %d pikseli", n)
	}
	filled := rasterize(t, renderString(t, head+`<div style="background:#c60;color:transparent">Tekst w tle</div>`).PDF, dir, "filled")
	if n := differentPixels(clipped[0], filled[0]); n < 5000 {
		t.Errorf("background-clip: text nie zmienia wyniku (różnica %d pikseli wobec pełnego tła)", n)
	}
}

// Tryb ciągły (ADR 0017): jedna strona, która rośnie do wysokości treści; wysokość z @page zostaje
// podstawą procentów, jednostek vh i elementów pozycjonowanych, a wymuszone podziały nie tworzą stron.
func TestContinuousMode(t *testing.T) {
	html := `<style>@page { size: 200px 100px; margin: 0 } body { margin: 0 }
		.b { height: 80px; background: green } .h { height: 50vh; background: blue }
		.a { position: absolute; bottom: 0; left: 0; width: 10px; height: 10px; background: red }</style>
		<div class="b"></div><div class="b" style="break-before:page"></div><div class="h"></div>
		<div class="b"></div><div class="a"></div>`
	paged := renderString(t, html)
	if paged.Pages < 3 {
		t.Fatalf("dokument z podziałami ma %d stron, oczekiwane co najmniej 3", paged.Pages)
	}
	result, err := Render(context.Background(), []byte(html), Options{Fonts: testFonts(t), Continuous: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Pages != 1 {
		t.Fatalf("tryb ciągły: %d stron, oczekiwana 1", result.Pages)
	}
	// 80 + 80 + 50 (połowa wysokości z @page) + 80 = 290 px; element pozycjonowany stoi przy dole
	// okna z arkusza (100 px), nie przy dole treści
	reference := `<style>@page { size: 200px 290px; margin: 0 } body { margin: 0 }
		.b { height: 80px; background: green } .h { height: 50px; background: blue }
		.a { position: absolute; top: 90px; left: 0; width: 10px; height: 10px; background: red }</style>
		<div class="b"></div><div class="b"></div><div class="h"></div><div class="b"></div><div class="a"></div>`
	dir := t.TempDir()
	got, want := rasterize(t, result.PDF, dir, "continuous"), rasterize(t, renderString(t, reference).PDF, dir, "reference")
	if len(want) != 1 {
		t.Fatalf("wzorzec ma %d stron", len(want))
	}
	if n := differentPixels(got[0], want[0]); n != 0 {
		t.Errorf("tryb ciągły różni się od wzorca o %d pikseli", n)
	}

	// kolumny: trzy wymuszone kolumny w kontenerze dwukolumnowym stoją obok siebie, a column-fill: auto
	// bez ograniczonej wysokości równoważy; strona nie rośnie do umownej wysokości układu
	for _, body := range []string{
		`<div style="columns:2;column-gap:0;width:100px"><div style="height:50px"></div>
			<div style="break-before:column;height:40px"></div><div style="break-before:column;height:10px"></div></div>`,
		`<div style="columns:2;column-fill:auto;width:100px"><div style="height:30px"></div><div style="height:30px"></div></div>`,
		`<div style="columns:2;width:100px;height:0"><div style="height:60px"></div></div>`,
	} {
		columns, err := Render(context.Background(), []byte(`<style>@page { size: 200px 100px; margin: 0 } body { margin: 0 }</style>`+body),
			Options{Fonts: testFonts(t), Continuous: true})
		if err != nil {
			t.Fatal(err)
		}
		if columns.Pages != 1 {
			t.Errorf("kolumny w trybie ciągłym: %d stron, oczekiwana 1", columns.Pages)
		}
		if pages := rasterize(t, columns.PDF, dir, "columns"); len(pages) != 1 || len(pages[0]) > 200*400*3 {
			t.Errorf("kolumny w trybie ciągłym: strona za wysoka (%d bajtów obrazu)", len(pages[0]))
		}
	}

	// treść krótsza niż strona z arkusza: strona nie maleje
	short, err := Render(context.Background(), []byte(`<style>@page { size: 200px 100px; margin: 0 }</style><p>x</p>`),
		Options{Fonts: testFonts(t), Continuous: true})
	if err != nil {
		t.Fatal(err)
	}
	plain := renderString(t, `<style>@page { size: 200px 100px; margin: 0 }</style><p>x</p>`)
	if n := differentPixels(rasterize(t, short.PDF, dir, "short")[0], rasterize(t, plain.PDF, dir, "plain")[0]); n != 0 {
		t.Errorf("krótki dokument w trybie ciągłym różni się od zwykłego o %d pikseli", n)
	}
}

// Łamanie wierszy (CSS Text 3 § 5): line-break: anywhere, word-break: break-all ze znakami
// przestankowymi, spacja nierozdzielająca przy elemencie atomowym oraz wysokość wiersza z glifem
// z fontu zastępczego (CSS Inline 3 § 4.2).
func TestLineBreakingRules(t *testing.T) {
	ahem, err := os.ReadFile("testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 } div { font: 20px/20px Ahem; color: green }
		.r { position: absolute; z-index: -1; color: red }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(head+body), Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	same := func(name, test, reference string) {
		t.Helper()
		if n := differentPixels(page(name, test), page(name+"-ref", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	// podział między dowolnymi znakami, także przed kropką
	same("anywhere", `<div style="width:40px;line-break:anywhere">XXX.X</div>`, `<div>XX<br>X.<br>X</div>`)
	// break-all: litery tak, ale wiersz nie zaczyna się od kropki
	same("break-all", `<div style="width:160px;word-break:break-all">XX XXX...</div>`, `<div>XX XX<br>X...</div>`)
	// U+00A0 nie skleja z elementem atomowym
	same("nbsp-atomic", `<div style="width:40px">XX&nbsp;<span style="display:inline-block">XX</span></div>`,
		`<div>XX<br><span style="display:inline-block">XX</span></div>`)
	// znak spoza fontu podstawowego (U+2011 z fontu zastępczego) nie zmienia wysokości wiersza
	marker := `<div style="width:20px;height:20px;background:blue"></div>`
	same("fallback-line-height", `<div style="color:transparent">X&#x2011;X</div>`+marker, `<div style="color:transparent">XXX</div>`+marker)
}

// Wiszące odstępy i tabulatory (CSS Text 3 § 4.1.3 i § 4.2): spacja ideograficzna i spacje pre-wrap na
// końcu wiersza zachowują szerokość, ale nie liczą się do wyrównania; spacja nierozdzielająca nie
// wisi; pozycje tabulacji liczą się od początku wiersza, z progiem pół znaku i ułamkowym tab-size.
func TestHangingSpacesAndTabStops(t *testing.T) {
	ahem, err := os.ReadFile("testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 } div { font: 20px/20px Ahem; color: green }
		.pre { white-space: pre } .blue { background: blue }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(head+body), Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	same := func(name, test, reference string) {
		t.Helper()
		if n := differentPixels(page(name, test), page(name+"-ref", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	// spacja ideograficzna na końcu wiersza wisi: zostaje w wierszu razem z tłem
	same("ideographic-hang", `<div style="width:60px">X&#x3000;<span class=blue>X&#x3000;</span></div>`,
		`<div class=pre>X&nbsp;<span class=blue>X&nbsp;</span></div>`)
	// ...i nie liczy się do szerokości wewnętrznej
	same("ideographic-min-content", `<div style="width:min-content;background:blue">&#x3000;</div><div>X</div>`, `<div><br>X</div>`)
	// spacja nierozdzielająca na końcu wiersza zachowuje szerokość i tło
	same("nbsp-keeps-width", `<div style="width:100px"><span class=blue>X&nbsp;</span><br>X</div>`,
		`<div><span class=blue>X<span style="color:blue">X</span></span><br>X</div>`)
	// spacja nierozdzielająca nie wisi: wiersz łamie się przed słowem, które z nią się nie mieści
	same("nbsp-does-not-hang", `<div style="width:100px">XX XX&nbsp;<span>X</span></div>`, `<div>XX<br>XX&nbsp;X</div>`)
	// pre-wrap: spacja, po której wiersz zawinięto na granicy elementów, nie przesuwa wyrównania
	same("pre-wrap-right", `<div style="width:140px;white-space:pre-wrap;text-align:right"><span>XX XX </span><span>XXX</span></div>`,
		`<div class=pre>  XX XX<br>    XXX</div>`)
	// pre-wrap jest justowany, bez wiszącej spacji
	same("pre-wrap-justify", `<div style="width:140px;white-space:pre-wrap;text-align:justify">XX XX XXX</div>`,
		`<div class=pre>XX   XX<br>XXX</div>`)
	// break-spaces z line-break: anywhere łamie także przed spacją
	same("break-spaces-anywhere", `<div style="width:80px;white-space:break-spaces;line-break:anywhere">X XX X</div>`,
		`<div class=pre>X XX<br> X</div>`)
	// odstępy inne niż spacja (tu U+2003) wiszą tak jak ideograficzny
	same("em-space-min-content", `<div style="width:min-content;background:blue">&#x2003;</div><div>X</div>`, `<div><br>X</div>`)
	// N znaków w pudełku o szerokości N ch mieści się mimo zaokrągleń szerokości glifów
	same("exact-fit", `<div style="font:16px/20px monospace;width:31ch;color:black">0123456789012345678901234567 89</div>`,
		`<div class=pre style="font:16px/20px monospace;color:black">0123456789012345678901234567 89</div>`)
	// overflow-wrap: anywhere: wiersz bez miejsca podziału łamie się na granicy elementów...
	same("anywhere-between-inlines", `<div style="width:20px;overflow-wrap:anywhere"><span>X</span><span>X</span><span>X</span></div>`,
		`<div>X<br>X<br>X</div>`)
	// ...a słowo zaczęte w elemencie liniowym dopełnia wiersz
	same("anywhere-fills-line", `<div style="width:80px;overflow-wrap:anywhere">XX<span>XXXX</span>XX</div>`, `<div>XXXX<br>XXXX</div>`)
	// słowo, które mieści się w następnym wierszu, przechodzi do niego całe
	same("anywhere-keeps-word", `<div style="width:80px;overflow-wrap:anywhere"><span>XX </span><span>XXX</span></div>`, `<div>XX<br>XXX</div>`)
	// word-break: break-word działa jak overflow-wrap: anywhere, także dla szerokości min-content
	same("break-word-min-content", `<div style="width:min-content;word-break:break-word">XXX</div>`, `<div>X<br>X<br>X</div>`)
	// tabulator po elemencie liniowym: pozycja liczona od początku wiersza
	same("tab-after-inline", `<div class=pre style="tab-size:4"><span>X</span>&#9;X</div>`, `<div class=pre>X   X</div>`)
	// mniej niż pół znaku do pozycji tabulacji: obowiązuje następna
	same("tab-threshold", `<div class=pre style="tab-size:4"><span style="display:inline-block;width:72px"></span>&#9;X</div>`,
		`<div class=pre>        X</div>`)
	// tab-size ułamkowy
	same("tab-size-fraction", `<div class=pre style="tab-size:2.5">&#9;&#9;X</div>`, `<div class=pre>     X</div>`)
	// odstęp tabulacji wynika z fontu bloku, nie elementu liniowego
	same("tab-unit-from-block", `<div class=pre style="tab-size:4"><span style="font-size:10px">&#9;</span>X</div>`, `<div class=pre>    X</div>`)
}

// Flex (CSS Flexbox 1 § 8.1, § 9.5, § 9.7, § 9.9.1): szerokość wewnętrzna kontenera o podanej
// szerokości i z odstępami, marginesy auto w kolumnie i przy przepełnieniu, ułamkowy i ujemny flex-shrink.
func TestFlexIntrinsicWidthAutoMarginsAndShrink(t *testing.T) {
	head := `<style>@page { size: 400px 300px; margin: 0 } body { margin: 0 } .g { background: green } .b { background: blue }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(head+body), Options{Fonts: testFonts(t)})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	same := func(name, test, reference string) {
		t.Helper()
		if n := differentPixels(page(name, test), page(name+"-ref", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	// element pływający dopasowuje się do kontenera flex o podanej szerokości, nie do sumy jego elementów
	same("definite-width", `<div class=g style="float:left"><div style="display:flex;flex-wrap:wrap;width:100px;height:40px">
		<div style="width:80px"></div><div style="width:80px"></div></div></div>`, `<div class=g style="width:100px;height:40px"></div>`)
	// column-gap liczy się do szerokości max-content
	same("gap-in-max-content", `<div class=g style="display:flex;column-gap:60px;width:max-content;height:40px">
		<div style="width:20px"></div><div style="width:20px"></div></div>`, `<div class=g style="width:100px;height:40px"></div>`)
	// element nieelastyczny wnosi swoje flex-basis
	same("inflexible-basis", `<div class=g style="display:flex;width:max-content;height:40px">
		<div style="flex:0 0 30px"></div><div style="flex:0 0 70px"></div></div>`, `<div class=g style="width:100px;height:40px"></div>`)
	// margines auto w kierunku kolumny zabiera wolne miejsce (justify-content nie działa)
	same("auto-margin-column", `<div style="display:flex;flex-direction:column;justify-content:space-around;height:200px;width:50px">
		<div class=g style="height:40px"></div><div class=b style="height:40px;margin-top:auto"></div></div>`,
		`<div class=g style="height:40px;width:50px"></div><div class=b style="height:40px;width:50px;margin-top:120px"></div>`)
	// element z marginesem auto w osi poprzecznej nie przesuwa swojej linii przy align-content
	same("auto-cross-margin-wrap", `<div style="display:flex;flex-flow:column wrap;align-content:space-around;height:100px;width:200px">
		<div class=g style="width:40px;height:60px"></div><div class=b style="width:40px;height:60px;margin-left:auto"></div></div>`,
		`<div style="position:relative;height:100px"><div class=g style="position:absolute;left:30px;width:40px;height:60px"></div>
		<div class=b style="position:absolute;left:130px;width:40px;height:60px"></div></div>`)
	// przy przepełnieniu marginesy auto są zerem, elementy nie nachodzą na siebie
	same("auto-margin-overflow", `<div style="display:flex;width:100px;height:40px"><div class=g style="flex:none;width:80px;margin:0 auto"></div>
		<div class=b style="flex:none;width:80px;margin:0 auto"></div></div>`,
		`<div style="display:flex;width:160px;height:40px"><div class=g style="flex:none;width:80px"></div><div class=b style="flex:none;width:80px"></div></div>`)
	// automatyczne minimum obrazu wynika z podanej wysokości i proporcji, nie z rozmiaru własnego
	square := `src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='10'%3E%3Crect width='10' height='10' fill='green'/%3E%3C/svg%3E"`
	same("image-auto-minimum", `<div style="display:flex;width:0"><img style="height:60px;flex-basis:10px" `+square+`></div>`,
		`<img style="height:60px;width:60px;display:block" `+square+`>`)
	// linia bazowa kontenera inline-flex uwzględnia przesunięcie pierwszej linii przez align-content
	baselineHead := `<style>div { font: 20px/20px sans-serif } span { display: inline-block; width: 20px; height: 20px; background: blue }</style>`
	same("baseline-align-content", baselineHead+`<div><span></span><div class=g style="display:inline-flex;flex-wrap:wrap;align-content:center;width:40px;height:100px">
		<div style="width:40px">x</div><div style="width:40px">x</div></div></div>`,
		baselineHead+`<div><span></span><div class=g style="display:inline-flex;flex-wrap:wrap;align-content:flex-start;width:40px;height:70px;padding-top:30px">
		<div style="width:40px">x</div><div style="width:40px">x</div></div></div>`)
	// inline-flex o szerokości auto dopasowuje się do treści
	same("inline-flex-shrink-to-fit", `<div style="font-size:0"><div class=g style="display:inline-flex"><div><div style="width:100px;height:40px"></div></div></div></div>`,
		`<div style="font-size:0"><div class=g style="display:inline-block;width:100px;height:40px"></div></div>`)
	// dwa elementy zastępowane obok siebie nie nakładają się
	same("replaced-side-by-side", `<div style="display:flex;height:50px"><canvas class=b width=100 height=50></canvas><canvas class=g width=100 height=50></canvas></div>`,
		`<div style="display:flex;height:50px"><div class=b style="width:100px"></div><div class=g style="width:100px"></div></div>`)
	// position: relative przesuwa element flex i element siatki
	same("relative-flex-item", `<div style="display:flex;width:200px;height:100px"><div class=g style="position:relative;left:50px;top:20px;width:40px;height:40px"></div></div>`,
		`<div class=g style="margin:20px 0 0 50px;width:40px;height:40px"></div>`)
	same("relative-grid-item", `<div style="display:grid;width:200px;height:100px"><div class=g style="position:relative;left:50px;top:20px;width:40px;height:40px"></div></div>`,
		`<div class=g style="margin:20px 0 0 50px;width:40px;height:40px"></div>`)
	// clear nie działa na elemencie flex; kontener flex o szerokości auto zwęża się obok elementu pływającego
	same("flex-beside-float", `<div class=b style="float:left;width:60px;height:60px"></div>
		<div style="display:flex"><div class=g style="clear:both;flex:1;height:40px"></div></div>`,
		`<div class=b style="float:left;width:60px;height:60px"></div><div class=g style="margin-left:60px;height:40px"></div>`)
	// min-width ogranicza szerokość kontenera kolumnowego wyliczoną z (braku) linii
	same("column-wrap-min-width", `<div class=g style="display:flex;flex-flow:column wrap;column-gap:10px;width:max-content;min-width:100px;height:40px"></div>`,
		`<div class=g style="width:100px;height:40px"></div>`)
	// element, który nie może się kurczyć, wnosi flex-basis do szerokości kontenera także gdy może rosnąć
	same("basis-grow-no-shrink", `<div class=g style="display:flex;float:left;height:40px"><div style="flex:1 0 100px"></div></div>`,
		`<div class=g style="width:100px;height:40px"></div>`)
	// flex-basis i max-width przy box-sizing: border-box opisują pudełko obramowania
	same("basis-border-box", `<div class=g style="display:flex;width:max-content;height:40px">
		<div style="flex:0 0 200px;border:10px solid transparent;box-sizing:border-box;max-width:100px"></div></div>`,
		`<div class=g style="width:100px;height:40px"></div>`)
	// rozmiar bazowy z treści nie zależy od max-width: oba elementy kurczą się od 300 px
	same("base-size-ignores-max-width", `<div style="display:flex;width:300px"><div class=g style="min-width:0;max-width:100px;height:40px"><div style="width:300px"></div></div>
		<div class=b style="min-width:0;height:40px"><div style="width:300px"></div></div></div>`,
		`<div style="display:flex;width:300px"><div class=g style="width:100px;height:40px"></div><div class=b style="width:200px;height:40px"></div></div>`)
	// justify-content: end przy row-reverse to prawa krawędź (koniec w kierunku pisma), start to lewa
	same("end-row-reverse", `<div style="display:flex;flex-direction:row-reverse;justify-content:end;width:200px"><div class=g style="width:50px;height:40px"></div></div>`,
		`<div class=g style="margin-left:150px;width:50px;height:40px"></div>`)
	// safe: element szerszy niż kontener zostaje przy początku zamiast wystawać w lewo
	same("safe-overflow", `<div style="display:flex;flex-direction:row-reverse;justify-content:safe flex-start;width:90px;margin-left:50px"><div class=g style="flex:none;width:120px;height:40px"></div></div>`,
		`<div class=g style="margin-left:50px;width:120px;height:40px"></div>`)
	// suma flex-shrink poniżej 1: kurczy się tylko o ułamek brakującego miejsca
	same("fractional-shrink", `<div style="display:flex;width:50px"><div class=g style="flex-shrink:0.5;width:150px;height:40px"></div></div>`,
		`<div class=g style="width:100px;height:40px"></div>`)
	// ujemny flex-shrink jest nieprawidłowy i nie zmienia wartości początkowej
	same("negative-shrink", `<div style="display:flex;width:100px"><div class=g style="flex-shrink:-2;width:200px;height:40px"></div></div>`,
		`<div class=g style="width:100px;height:40px"></div>`)
}

// Siatka (CSS Grid 1 § 6.2, § 11.1, § 11.5; CSS Align 3 § 6.1): element bez rozciągania ma szerokość
// treści, element z wysokością max-content nie jest rozciągany, wyrównanie do linii bazowej powiększa
// wiersz i kontener, min-height kontenera rozciąga wiersze auto.
func TestGridItemSizingBaselineRowsAndMinHeight(t *testing.T) {
	head := `<style>@page { size: 400px 300px; margin: 0 } body { margin: 0 } .g { background: green } .b { background: blue }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(head+body), Options{Fonts: testFonts(t)})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	same := func(name, test, reference string) {
		t.Helper()
		if n := differentPixels(page(name, test), page(name+"-ref", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	// justify-items: start: szerokość treści bez doliczania marginesu, obramowania i odstępów
	same("start-item-width", `<div style="display:grid;width:300px;justify-items:start">
		<div class=g style="margin:0 10px;padding:0 20px;border:solid blue;border-width:0 5px"><div style="width:40px;height:30px"></div></div></div>`,
		`<div class=g style="margin:0 10px;padding:0 20px;border:solid blue;border-width:0 5px;width:40px;height:30px"></div>`)
	// wysokość max-content to nie auto: element nie rozciąga się na cały wiersz
	same("max-content-height", `<div style="display:grid;width:100px;height:100px;grid-template:100% / 100%">
		<div class=g style="height:max-content"><div style="height:40px"></div></div></div>`, `<div class=g style="width:100px;height:40px"></div>`)
	// wyrównanie do linii bazowej: wiersz i kontener rosną, gdy przesunięty element by wystawał
	same("baseline-grows-row", `<div class=b style="display:grid;grid-auto-flow:column;justify-content:start;align-items:baseline;font:20px/20px sans-serif;color:transparent">
		<div style="padding-bottom:60px">x</div><div style="padding-top:40px">x</div></div><div class=g style="height:20px;width:50px"></div>`,
		`<div class=b style="height:120px"></div><div class=g style="height:20px;width:50px"></div>`)
	// repeat(auto-fill, minmax(50px, 25px)) liczy się jak 50px: dwie kolumny w 100px
	same("auto-fill-minmax", `<div style="display:grid;width:100px;grid-template:50px 50px / repeat(auto-fill, minmax(50px, 25px))">
		<div class=g></div><div class=g></div><div class=g></div><div class=g></div></div>`, `<div class=g style="width:100px;height:100px"></div>`)
	// obszar nazwany daje nazwę <obszar>-end swojej krawędzi także wtedy, gdy tę nazwę ma jawnie dalsza linia
	same("implicit-area-names", `<div style="display:grid;width:300px;height:300px;grid-template-areas:'area';
		grid-template-columns:repeat(auto-fill, 100px);grid-template-rows:repeat(auto-fill, 100px) [area-end] 100px">
		<div class=g style="grid-area:area"></div></div>`, `<div class=g style="width:100px;height:100px"></div>`)
	// kontener siatki o podanej szerokości staje obok elementu pływającego, nie pod nim
	same("grid-avoids-float", `<div class=b style="float:left;width:50px;height:100px"></div><div class=g style="display:grid;width:50px;height:100px"></div>`,
		`<div class=b style="float:left;width:50px;height:100px"></div><div class=g style="margin-left:50px;width:50px;height:100px"></div>`)
	// wiersze fr przy wysokości auto: ułamek z treści elementu, a przy sumie współczynników poniżej 1
	// drugi przebieg w wysokości z pierwszego (2 x 0.4fr, element 100 px w drugim wierszu: wiersze po 32 px)
	same("fr-rows-indefinite", `<div class=b style="display:grid;width:100px;grid-template-rows:repeat(2, minmax(0,0.4fr))">
		<div class=g style="grid-row:2;height:100px"></div></div>`,
		`<div class=b style="width:100px;height:80px"><div style="height:32px"></div><div class=g style="height:100px"></div></div>`)
	// element pozycjonowany wskazuje linie także za zwiniętymi ścieżkami auto-fit
	same("abspos-collapsed-auto-fit", `<div style="display:grid;position:relative;width:300px;column-gap:10px;grid-template-columns:repeat(auto-fit, 50px)">
		<div style="grid-column:3;height:40px"><div class=g style="position:absolute;inset:0;grid-column:3 / 4"></div></div>
		<div style="grid-column:5;height:40px"><div class=b style="position:absolute;inset:0;grid-column:5 / 6"></div></div></div>`,
		`<div style="display:flex;gap:10px"><div class=g style="width:50px;height:40px"></div><div class=b style="width:50px;height:40px"></div></div>`)
	// max-height ogranicza wysokość siatki wyliczoną z wierszy
	same("grid-max-height", `<div style="display:grid;position:relative;grid-template-columns:100px;grid-template-rows:200px;max-width:100px;max-height:60px">
		<div class=g style="position:absolute;width:100%;height:100%"></div></div>`, `<div class=g style="width:100px;height:60px"></div>`)
	// min-height kontenera o wysokości auto rozciąga wiersz auto, a z nim element
	same("min-height-stretches", `<div style="display:grid;width:100px;min-height:100px"><div class=g></div></div>`,
		`<div class=g style="width:100px;height:100px"></div>`)
}

// border-image (CSS Backgrounds 3 § 6): powiększany wycinek obrazu rastrowego nie miesza się na
// krawędzi z sąsiednim obszarem obrazu; szerokości przekraczające obszar są zmniejszane proporcjonalnie.
func TestBorderImageSlicesDoNotBleed(t *testing.T) {
	// obraz 3x3: zielona ramka, czerwony środek
	source := image.NewNRGBA(image.Rect(0, 0, 3, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			source.Set(x, y, color.NRGBA{G: 128, A: 255})
		}
	}
	source.Set(1, 1, color.NRGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	head := `<style>@page { size: 300px 300px; margin: 0 } body { margin: 0 }
		div { width: 100px; height: 100px; border: 30px solid green }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(head+body), Options{Fonts: testFonts(t)})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	reference := page("ref", `<div></div>`)
	// (do kilku pikseli na narożnikach; z przeciekaniem koloru środka różni się cała wewnętrzna krawędź)
	if n := differentPixels(page("slices", `<div style="border-image:url(`+uri+`) 1"></div>`), reference); n > 4 {
		t.Errorf("wycinki: %d pikseli różni się od jednolitej ramki", n)
	}
	// szerokości po przeciwnych stronach większe niż obszar są skalowane tak, żeby się zmieścić
	if n := differentPixels(page("scaled-down", `<div style="width:0;height:0;border-width:80px;border-image:url(`+uri+`) 1 / 80px 80px 160px 80px"></div>`),
		page("scaled-ref", `<div style="width:0;height:0;border-width:80px;border-image:url(`+uri+`) 1 / 53.333px 53.333px 106.667px 53.333px"></div>`)); n != 0 {
		t.Errorf("zmniejszanie szerokości: %d pikseli różni się od wzorca", n)
	}
}

// CSS 2.1 z erratą: element główny pozycjonowany bezwzględnie (§ 10.1), tło tabeli będącej elementem
// głównym na płótnie (§ 14.2), overflow na tabeli i na jej wierszach (§ 11.1.1), łańcuch w counter(),
// linia bazowa inline-block z overflow (§ 10.8.1, CSS Align 3 § 9.1).
func TestRootPositioningTableOverflowAndInlineBlockBaseline(t *testing.T) {
	page := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 }</style>`
	dir := t.TempDir()
	render := func(name, html string) []byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(html), Options{Fonts: testFonts(t)})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	same := func(name, test, reference string) {
		t.Helper()
		if n := differentPixels(render(name, test), render(name+"-ref", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	// element główny pozycjonowany: stoi tam, gdzie każe left i top, a jego tło idzie na płótno
	same("root-absolute", `<html style="position:absolute;left:50px;top:40px;width:60px;height:30px;background:yellow;border:5px solid black">`+page,
		`<html style="background:yellow">`+page+`<div style="position:absolute;left:50px;top:40px;width:60px;height:30px;border:5px solid black"></div>`)
	// tabela jako element główny: tło tabeli na płótnie
	same("root-table-canvas", `<html style="display:table;background:yellow">`+page+`<p style="margin:0;height:20px">`,
		`<html style="background:yellow">`+page+`<p style="margin:0;height:20px">`)
	// overflow: hidden na tabeli przycina treść komórki wystającą ponad tabelę; podpis leży poza przycięciem
	same("table-overflow", page+`<style>table { overflow: hidden; border-spacing: 0 } td { padding: 0 } caption { margin-bottom: 10px }
		div { width: 20px; height: 25px; border-top: 10px solid red; margin-top: -15px; background: black }</style>
		<table><caption></caption><tr><td><div></div></table>`,
		page+`<div style="margin-top:10px;width:20px;height:20px;background:black"></div>`)
	// tła komórek tabeli, która sama tworzy kontekst nakładania
	same("table-overflow-cells", page+`<table style="overflow:hidden;border-spacing:0"><tr><td style="padding:0;width:20px;height:20px;background:black"></table>`,
		page+`<div style="width:20px;height:20px;background:black"></div>`)
	// overflow nie dotyczy grupy wierszy: treść wystająca poza tabelę zostaje widoczna
	same("row-group-overflow", page+`<div style="display:table;width:40px;table-layout:fixed"><div style="display:table-row-group;overflow:hidden">
		<div style="display:table-cell"><div style="width:100px;height:20px;background:green"></div></div></div></div>`,
		page+`<div style="width:100px;height:20px;background:green"></div>`)
	// counter(c, ".") jest nieprawidłowe: zostaje wcześniejsza deklaracja content
	same("counter-string-style", page+`<style>p { margin: 0; counter-increment: c } p::before { content: counter(c); content: counter(c, ".") }</style><p>x`,
		page+`<p style="margin:0">1x`)
	// iframe to element zastępowany 300 na 150, bez proporcji własnych
	same("iframe-default-size", page+`<iframe style="display:block;border:0;background:green"></iframe>`,
		page+`<div style="width:300px;height:150px;background:green"></div>`)
	same("iframe-no-ratio", page+`<iframe style="display:block;border:0;background:green;width:100px"></iframe>`,
		page+`<div style="width:100px;height:150px;background:green"></div>`)
	// clear: margines pierwszego dziecka zwija się przed liczeniem clearance, więc blok staje tuż pod
	// elementem pływającym, a nie o ten margines niżej
	same("clearance-child-margin", page+`<div style="float:left;width:50px;height:50px;background:blue"></div>
		<div style="clear:left"><div style="margin-top:30px;width:50px;height:50px;background:green"></div></div>`,
		page+`<div style="width:50px;height:50px;background:blue"></div><div style="width:50px;height:50px;background:green"></div>`)
	same("no-clearance-large-child-margin", page+`<div style="float:left;width:50px;height:50px;background:blue"></div>
		<div style="clear:left"><div style="margin-top:80px;width:50px;height:50px;background:green"></div></div>`,
		page+`<div style="width:50px;height:50px;background:blue"></div><div style="margin-top:30px;width:50px;height:50px;background:green"></div>`)
	// inline-block z overflow: linia bazowa z ostatniego wiersza, gdy ten leży w pudełku
	baseline := page + `<style>p { margin: 0; font: 20px/60px sans-serif } span { display: inline-block; line-height: 20px; height: 40px; margin-bottom: 8px }</style>`
	same("inline-block-baseline", baseline+`<p>a <span style="overflow:hidden">b</span>`, baseline+`<p>a <span>b</span>`)
}
