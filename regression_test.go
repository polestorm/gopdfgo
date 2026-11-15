package gopdfgo

import (
	"bytes"
	"context"
	"errors"
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
