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
