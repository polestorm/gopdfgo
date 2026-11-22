package tests

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo"
	"github.com/polestorm/gopdfgo/internal/goweasyprint/pdf"
)

// Treść krojona między strony (obraz, SVG, wiersz, pudełko z przycięciem wyższe niż strona, ADR 0041):
// każda strona pokazuje kolejny wycinek, a do PDF strony trafia tylko tekst, który przecina jej wycinek.
// Tekst niewidoczny nie może się powtarzać w warstwie tekstu (pdftotext, wyszukiwanie, kopiowanie),
// w treści oznaczonej PDF z tagami ani w odnośnikach.

// pageTexts zwraca słowa każdej strony z pdftotext, połączone pojedynczymi spacjami.
func pageTexts(t *testing.T, data []byte) []string {
	t.Helper()
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("brak pdftotext")
	}
	file := filepath.Join(t.TempDir(), "out.pdf")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var out []string
	for page := 1; ; page++ {
		text, err := exec.Command("pdftotext", "-f", fmt.Sprint(page), "-l", fmt.Sprint(page), file, "-").Output()
		if err != nil {
			break // strona poza dokumentem
		}
		out = append(out, strings.Join(strings.Fields(string(text)), " "))
	}
	return out
}

// renderSliced renderuje dokument z rysowaniem w trakcie układu i bez niego (wynik musi być ten sam)
// oraz bez pomijania tekstu; zwraca wynik i sprawdza, że rastry obu wersji są identyczne.
func renderSliced(t *testing.T, html string, adjust func(*gopdfgo.Options)) []byte {
	t.Helper()
	opts := streamOptions(t)
	if adjust != nil {
		adjust(&opts)
	}
	streamed, classic, _ := streamPair(t, []byte(html), opts)
	if !bytes.Equal(streamed, classic) {
		t.Errorf("wynik różni się od rysowania po ułożeniu całego dokumentu")
	}
	pdf.TextCullingDisabled = true
	whole, err := gopdfgo.Render(context.Background(), []byte(html), opts)
	pdf.TextCullingDisabled = false
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	culled, full := rasterize(t, streamed, dir, "culled"), rasterize(t, whole.PDF, dir, "full")
	if len(culled) != len(full) {
		t.Fatalf("stron %d, bez pomijania tekstu %d", len(culled), len(full))
	}
	for i := range culled {
		if n := differentPixels(culled[i], full[i]); n != 0 {
			t.Errorf("strona %d: pomijanie tekstu zmienia %d pikseli", i+1, n)
		}
	}
	return streamed
}

// Obraz SVG wyższy niż strona z marginesami (viewBox 100×300 w 200×600 px, strona pokazuje 200 px, czyli
// 100 jednostek viewBox). Etykiety Gamma i Theta leżą tuż nad granicą wycinków, Delta tuż pod nią: na
// sąsiedniej stronie wypadają w marginesie (na stronie, choć poza przycięciem) i bez pomijania pdftotext
// widział je dwa razy. Żadna etykieta nie przecina granicy.
const slicedSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 300" width="200" height="600">` +
	`<rect x="1" y="1" width="98" height="298" fill="none" stroke="black"/>` +
	`<text x="10" y="12" font-size="10">Alpha</text><text x="10" y="45" font-size="10">Beta</text>` +
	`<text x="10" y="97" font-size="10">Gamma</text><text x="10" y="118" font-size="10">Delta</text>` +
	`<text x="10" y="150" font-size="10">Eta</text><text x="10" y="197" font-size="10">Theta</text>` +
	`<text x="10" y="290" font-size="10">Zeta</text></svg>`

const slicedPage = `<!DOCTYPE html><style>@page { size: 300px 300px; margin: 50px } body { margin: 0 }</style>`

func TestSlicedSVGTextOnce(t *testing.T) {
	for _, c := range []struct{ name, body string }{
		{"blok", `<div style="width:200px">` + strings.Replace(slicedSVG, "<svg ", `<svg style="display:block" `, 1) + `</div>`},
		{"w wierszu", `<div style="width:200px">` + slicedSVG + `</div>`},
		{"img", `<img style="display:block" src="data:image/svg+xml,` + urlEscape(slicedSVG) + `">`},
	} {
		t.Run(c.name, func(t *testing.T) {
			pages := pageTexts(t, renderSliced(t, slicedPage+c.body, nil))
			checkOnce(t, pages, "Alpha", "Beta", "Gamma", "Delta", "Eta", "Theta", "Zeta")
		})
	}
}

// checkOnce sprawdza, że każde słowo występuje w tekście stron dokładnie raz.
func checkOnce(t *testing.T, pages []string, words ...string) {
	t.Helper()
	count := map[string]int{}
	for _, w := range strings.Fields(strings.Join(pages, " ")) {
		count[w]++
	}
	for _, w := range words {
		if count[w] != 1 {
			t.Errorf("%s: %d razy w tekście stron %q", w, count[w], pages)
		}
	}
}

// urlEscape koduje SVG do adresu data: (znaki zarezerwowane i spacje).
func urlEscape(s string) string {
	return strings.NewReplacer("%", "%25", "#", "%23", "\"", "%22", "<", "%3C", ">", "%3E", " ", "%20").Replace(s)
}

// <svg> w wierszu (overflow: hidden tworzy kontekst nakładania, rysowany poza wierszem) wyższy niż
// strona: na drugiej stronie dalsza część obrazu. Wcześniej każda strona pokazywała początek obrazu,
// a jego dalsza część ginęła (diagramy mermaid w bench/realdocs, 2026-11-22).
func TestSlicedInlineSVGContinues(t *testing.T) {
	html := `<!DOCTYPE html><style>@page { size: 300px 300px; margin: 0 } body { margin: 0 }</style>` +
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 200"><text x="10" y="20">ALPHA</text>` +
		`<text x="10" y="120">BETA</text><text x="10" y="190">GAMMA</text></svg>`
	pages := pageTexts(t, renderSliced(t, html, nil))
	if len(pages) < 2 || pages[0] != "ALPHA" || pages[1] != "BETA GAMMA" {
		t.Errorf("tekst stron %q, oczekiwano ALPHA, potem BETA GAMMA", pages)
	}
}

// Pudełko pozycjonowane bezwzględnie z przycięciem, wyższe niż strona (krojone jako całość,
// BoxFields.SliceVisible): wiersze tekstu spoza wycinka nie trafiają na stronę.
func TestSlicedClippedBoxTextOnce(t *testing.T) {
	var lines strings.Builder
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&lines, "<div style=\"height:50px\">line%d</div>", i)
	}
	html := slicedPage + `<div style="position:absolute; top:0; width:150px; height:600px; overflow:hidden">` +
		lines.String() + `</div>`
	var words []string
	for i := 1; i <= 12; i++ {
		words = append(words, fmt.Sprintf("line%d", i))
	}
	checkOnce(t, pageTexts(t, renderSliced(t, html, nil)), words...)
}

// Odnośnik SVG przecinający granicę wycinków: na każdej stronie adnotacja tylko w widocznej części,
// w PDF z tagami jeden element Link z dwiema adnotacjami i jeden Figure; plik zgodny z PDF/UA-1. Tekst
// na każdej stronie: strona bez zasobów nie ma dziś słownika /Resources (osobne zgłoszenie, ADR 0041).
func TestSlicedSVGLinkOnce(t *testing.T) {
	html := `<!DOCTYPE html><html lang="pl"><title>Wycinki</title><style>@page { size: 400px 300px; margin: 0 }
		body { margin: 0 } svg { display: block }</style>
		<svg xmlns="http://www.w3.org/2000/svg" width="400" height="500" role="img" aria-label="Diagram">
		<a href="https://example.com/rect"><rect x="10" y="250" width="100" height="100" fill="blue"/></a>
		<a href="https://example.com/top"><rect x="200" y="10" width="20" height="20" fill="green"/></a>
		<text x="300" y="100">one</text><text x="300" y="400">two</text>
		</svg>`
	data := renderSliced(t, html, func(o *gopdfgo.Options) { o.Accessibility, o.Uncompressed = gopdfgo.PDFUA1, true })
	links := pdfLinks(t, data)
	// pdfLinks liczy y od góry strony o wysokości 300px: część 250–300 na stronie 1, 0–50 na stronie 2
	checkLinks(t, links, []pdfLink{
		{"https://example.com/rect", 10, 250, 110, 300},
		{"https://example.com/top", 200, 10, 220, 30},
		{"https://example.com/rect", 10, 0, 110, 50},
	})
	if n := len(structureElements(t, data, "Figure")); n != 1 {
		t.Errorf("elementów Figure %d, oczekiwano 1", n)
	}
	var annotations []int
	for _, obj := range structureElements(t, data, "Link") {
		annotations = append(annotations, strings.Count(obj, "/OBJR"))
	}
	if fmt.Sprint(annotations) != "[2 1]" {
		t.Errorf("adnotacje elementów Link %v, oczekiwano [2 1]", annotations)
	}
	verapdf(t, data, "ua1")
}
