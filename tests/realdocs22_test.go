package tests

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
)

// Rozbieżności z Chromium znalezione na dokumentacji Markdown i diagramach mermaid (bench/realdocs,
// 2026-11-22; docs/feature-audit.md, ADR 0038).

// textLines zwraca niepuste wiersze tekstu PDF w kolejności z pdftotext, z pojedynczymi spacjami.
func textLines(t *testing.T, pdf []byte) []string {
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
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// Po spacji U+0020 wiersz wolno złamać przed każdym znakiem, także przed dwukropkiem, nawiasem
// zamykającym czy wykrzyknikiem i po nawiasie otwierającym (ADR 0038; UAX #14 LB13–LB14 tego
// zabraniają, Chromium łamie). Przypadek z dokumentacji: „@page :first/:left/:right” w komórce tabeli
// trzymane w jednym kawałku poszerzało tabelę ponad stronę i uruchamiało pomniejszanie całej treści.
func TestBreakAfterSpaceBeforePunctuation(t *testing.T) {
	head := `<!DOCTYPE html><style>div { width: min-content; font: 12px monospace }</style>`
	for _, c := range []struct{ text, first string }{
		{"@page :first/:left/:right", "@page"},
		{"ab ) cd", "ab"},
		{"ab ( cd", "ab"},
		{"mot !", "mot"},
	} {
		lines := textLines(t, renderString(t, head+`<div>`+c.text+`</div>`).PDF)
		if len(lines) == 0 || lines[0] != c.first {
			t.Errorf("%q: wiersze %q, pierwszy powinien być %q", c.text, lines, c.first)
		}
	}
	// spacja nierozdzielająca zostaje przy UAX #14, jak w Chromium
	for _, text := range []string{"ab\u00a0:x"} {
		lines := textLines(t, renderString(t, head+`<div>`+text+`</div>`).PDF)
		if len(lines) != 1 {
			t.Errorf("%q złamane: %q", text, lines)
		}
	}
	// tabela z wstawką kodu mieści się w szerokości strony, więc treść nie jest pomniejszana:
	// słowo przed tabelą ma pełny rozmiar 16 px, czyli zajmuje swój wiersz tak samo jak bez tabeli
	cell := strings.Repeat(`<td>tekst <code>@page :first/:left/:right</code> dalej</td>`, 3)
	table := `<!DOCTYPE html><style>@page { size: 450px 600px; margin: 0 } body { margin: 0 }
		td { padding: 1px } code { font: 12px "Noto Sans Mono" }</style>
		<p>` + strings.Repeat("słowo ", 12) + `</p><table><tr>` + cell + `</tr></table>`
	plain := `<!DOCTYPE html><style>@page { size: 450px 600px; margin: 0 } body { margin: 0 }</style>
		<p>` + strings.Repeat("słowo ", 12) + `</p>`
	if got, want := textLines(t, renderString(t, table).PDF)[0], textLines(t, renderString(t, plain).PDF)[0]; got != want {
		t.Errorf("pierwszy wiersz z tabelą %q, bez tabeli %q: treść pomniejszona", got, want)
	}
}

// Ukośnik na początku słowa („/Lang”, „/opt/google”) nie daje okazji do złamania po sobie, także gdy
// wstawka kodu stoi tuż po nawiasie (ADR 0035, uzupełnienie z 2026-11-22; Chromium tak samo).
func TestLeadingSlashKeepsWord(t *testing.T) {
	head := `<!DOCTYPE html><style>div { width: min-content; font: 12px monospace }</style>`
	for _, c := range []struct{ html, word string }{
		{`xx /Lang`, "/Lang"},
		{`xx (<code>/Lang</code>)`, "(/Lang)"},
		{`xx /opt/google/chrome`, "/opt/google/chrome"},
	} {
		lines := textLines(t, renderString(t, head+`<div>`+c.html+`</div>`).PDF)
		found := false
		for _, line := range lines {
			found = found || line == c.word
		}
		if !found {
			t.Errorf("%q: wiersze %q, brak wiersza %q", c.html, lines, c.word)
		}
	}
}

// Selektory dokumentu z kombinatorem potomka obejmują treść foreignObject przez jego przodków w SVG
// („#my-svg p”, „.label div .edgeLabel” w diagramach mermaid; CSS Selectors 4 § 16.1).
func TestForeignObjectDescendantSelectors(t *testing.T) {
	html := `<!DOCTYPE html><style>#s .hide, g.label > foreignObject div span.x, svg p.y { display: none }</style>
		<svg id="s" width="300" height="100"><g class="label"><foreignObject width="300" height="100">
		<div xmlns="http://www.w3.org/1999/xhtml"><span class="hide">UKRYTE1</span><span class="x">UKRYTE2</span>
		<p class="y">UKRYTE3</p>WIDOCZNE</div></foreignObject></g></svg>`
	text := string(extractText(t, renderString(t, html).PDF))
	if !strings.Contains(text, "WIDOCZNE") {
		t.Fatalf("brak treści foreignObject: %q", text)
	}
	for _, word := range []string{"UKRYTE1", "UKRYTE2", "UKRYTE3"} {
		if strings.Contains(text, word) {
			t.Errorf("%s widoczne: reguła z selektorem przez przodków foreignObject nie zadziałała (%q)", word, text)
		}
	}
}

// Kawałek tekstu, którego nie da się złamać i który wystaje poza wiersz o ułamek piksela (przecinek tuż
// za wstawką kodu), zgłasza pełną szerokość, więc wiersz łamie się wcześniej, jak w Chromium. Wcześniej
// szerokość była przycinana do dostępnej (tolerancja zaokrągleń z LineWrap) i przecinek zostawał
// w wierszu, wystając o 0,1 px.
func TestUnbreakableTailDoesNotOverflow(t *testing.T) {
	mono, err := os.ReadFile("../testdata/fonts/NotoSansMono-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	fonts := append(testFonts(t), htmlpdf.Font{Name: "NotoSansMono-Regular.ttf", Data: mono})
	render := func(body string) []byte {
		result, err := htmlpdf.Render(context.Background(), []byte(`<!DOCTYPE html><style>body { font: 10px "Noto Sans Mono" }
			code { font: inherit }</style>`+body), htmlpdf.Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		return result.PDF
	}
	lines := textLines(t, render(`<div style="width: 47.9px">aaa <code>bbb</code>, ccc</div>`))
	if len(lines) != 3 || lines[0] != "aaa" || lines[1] != "bbb," {
		t.Errorf("wiersze %q, oczekiwane [aaa bbb, ccc]", lines)
	}
	// mieści się co do piksela: zostaje w jednym wierszu
	lines = textLines(t, render(`<div style="width: 48px">aaa <code>bbb</code>, ccc</div>`))
	if len(lines) == 0 || lines[0] != "aaa bbb," {
		t.Errorf("wiersze %q, pierwszy powinien być „aaa bbb,”", lines)
	}
}
