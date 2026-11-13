package gopdfgo

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
