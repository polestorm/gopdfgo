package tests

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
)

// TestSVGFilterShadows sprawdza wektorowe filtry SVG (Filter Effects 1): cień z grafu prymitywów
// (feGaussianBlur, feOffset, feFlood, feComposite, feMerge), feDropShadow, cień w stylu eksportu Figmy
// (feColorMatrix, feComposite out, feBlend), dziedziczenie przez href i pomijanie filtrów, których nie
// da się narysować bez rastrowania. Cień jest sylwetką elementu użytą jako maska miękka, a rozmycie
// sumą przesuniętych kopii tej maski z funkcją przejścia (internal/webrender/svg/filters.go).
func TestSVGFilterShadows(t *testing.T) {
	head := `<style>@page { size: 200px 120px; margin: 0 } body { margin: 0 } svg { display: block }</style>`
	dir := t.TempDir()
	fonts := testFonts(t)
	render := func(body string) []byte {
		t.Helper()
		result, err := htmlpdf.Render(context.Background(), []byte(head+body),
			htmlpdf.Options{Fonts: fonts, Resolver: htmlpdf.DenyAll})
		if err != nil {
			t.Fatal(err)
		}
		return result.PDF
	}
	page := func(name, body string) []byte {
		t.Helper()
		pages := rasterize(t, render(body), dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	within := func(name, test, reference string, limit int) {
		t.Helper()
		if n := differentPixels(page(name, test), page(name+"-ref", reference)); n > limit || n < 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	svg := func(defs, content string) string {
		return `<svg width="200" height="120"><defs>` + defs + `</defs>` + content + `</svg>`
	}

	// cień ostry: sylwetka w kolorze wypełnienia pod elementem, przesunięta o (dx, dy); element nad nim
	// bez zmian
	shadow := `<filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="200" height="120">
		<feOffset in="SourceAlpha" dx="60" dy="30" result="o"/><feFlood flood-color="blue"/>
		<feComposite in2="o" operator="in"/><feMerge><feMergeNode/><feMergeNode in="SourceGraphic"/></feMerge></filter>`
	within("hard-shadow", svg(shadow, `<rect x="20" y="20" width="60" height="40" fill="green" filter="url(#f)"/>`),
		svg("", `<rect x="80" y="50" width="60" height="40" fill="blue"/><rect x="20" y="20" width="60" height="40" fill="green"/>`), 0)
	// to samo przez feDropShadow bez rozmycia (stdDeviation="0" wyłącza rozmycie)
	within("drop-shadow-hard", svg(`<filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="200" height="120">
		<feDropShadow dx="60" dy="30" stdDeviation="0" flood-color="blue"/></filter>`,
		`<rect x="20" y="20" width="60" height="40" fill="green" filter="url(#f)"/>`),
		svg("", `<rect x="80" y="50" width="60" height="40" fill="blue"/><rect x="20" y="20" width="60" height="40" fill="green"/>`), 0)
	// filtr dziedziczący prymitywy przez href
	within("href", svg(shadow+`<filter id="g" href="#f"/>`, `<rect x="20" y="20" width="60" height="40" fill="green" filter="url(#g)"/>`),
		svg("", `<rect x="80" y="50" width="60" height="40" fill="blue"/><rect x="20" y="20" width="60" height="40" fill="green"/>`), 0)
	// nieobsługiwany prymityw (feTurbulence): filtr pominięty w całości, element narysowany bez filtra
	within("unsupported", svg(`<filter id="f"><feTurbulence baseFrequency="0.05"/><feOffset dx="40"/></filter>`,
		`<rect x="20" y="20" width="60" height="40" fill="green" filter="url(#f)"/>`),
		svg("", `<rect x="20" y="20" width="60" height="40" fill="green"/>`), 0)

	// cień rozmyty: środek dużego kształtu ma krycie flood-opacity, krawędź połowę, a 3σ dalej prawie nic
	gray := page("blur-gray", svg(`<filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="200" height="120">
		<feGaussianBlur in="SourceAlpha" stdDeviation="4" result="b"/><feFlood flood-color="black" flood-opacity="0.5"/>
		<feComposite in2="b" operator="in"/></filter>`,
		`<rect x="40" y="20" width="120" height="80" fill="red" filter="url(#f)"/>`))
	for _, probe := range []struct {
		x, y     int
		min, max int // dopuszczalna jasność kanału
	}{
		{100, 60, 124, 132}, // środek: 50% czerni na bieli
		{40, 60, 180, 202},  // krawędź: połowa krycia środka (1 − 0,25)
		{36, 60, 205, 235},  // 1σ na zewnątrz: 0,16 krycia środka (1 − 0,08)
		{28, 60, 245, 255},  // 3σ na zewnątrz: prawie biel
	} {
		r, g, b := pixelAt(t, gray, probe.x, probe.y)
		if r != g || g != b {
			t.Errorf("blur (%d,%d): kolor %d %d %d, oczekiwano szarości", probe.x, probe.y, r, g, b)
		}
		if r < probe.min || r > probe.max {
			t.Errorf("blur (%d,%d): jasność %d poza [%d, %d]", probe.x, probe.y, r, probe.min, probe.max)
		}
	}

	// cień w stylu eksportu Figmy (twarda alfa, przesunięcie, rozmycie, wycięcie pod kształtem, krycie
	// macierzą koloru, feBlend) wygląda jak feDropShadow o tych samych parametrach
	figma := `<filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="200" height="120" color-interpolation-filters="sRGB">
		<feFlood flood-opacity="0" result="BackgroundImageFix"/>
		<feColorMatrix in="SourceAlpha" type="matrix" values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0" result="hardAlpha"/>
		<feOffset dx="6" dy="8"/><feGaussianBlur stdDeviation="3"/><feComposite in2="hardAlpha" operator="out"/>
		<feColorMatrix type="matrix" values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.4 0"/>
		<feBlend mode="normal" in2="BackgroundImageFix" result="effect1"/>
		<feBlend mode="normal" in="SourceGraphic" in2="effect1" result="shape"/></filter>`
	drop := `<filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="200" height="120">
		<feDropShadow dx="6" dy="8" stdDeviation="3" flood-opacity="0.4"/></filter>`
	within("figma", svg(figma, `<rect x="40" y="20" width="100" height="60" rx="8" fill="green" filter="url(#f)"/>`),
		svg(drop, `<rect x="40" y="20" width="100" height="60" rx="8" fill="green" filter="url(#f)"/>`), 0)
	// … i cień rzeczywiście jest narysowany (filtr nie został pominięty)
	if n := differentPixels(page("figma-shadow", svg(figma, `<rect x="40" y="20" width="100" height="60" rx="8" fill="green" filter="url(#f)"/>`)),
		page("figma-plain", svg("", `<rect x="40" y="20" width="100" height="60" rx="8" fill="green"/>`))); n < 500 {
		t.Errorf("figma: cień nie został narysowany (%d pikseli różnicy)", n)
	}

	// tekst w grupie z filtrem: kopie w masce cienia nie powtarzają treści przy wyodrębnianiu tekstu
	if _, err := exec.LookPath("pdftotext"); err == nil {
		pdf := render(svg(drop, `<g filter="url(#f)"><text x="20" y="60" font-size="24" font-family="sans-serif">Cień tekstu</text></g>`))
		cmd := exec.Command("pdftotext", "-", "-")
		cmd.Stdin = bytes.NewReader(pdf)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(strings.Trim(string(out), "\f\n ")); got != "Cień tekstu" {
			t.Errorf("tekst wyodrębniony z grupy z filtrem: %q", got)
		}
	}
}

// pixelAt odczytuje kolor piksela z obrazu PPM (P6, 8 bitów).
func pixelAt(t *testing.T, ppm []byte, x, y int) (r, g, b int) {
	t.Helper()
	header, data := splitPPM(ppm)
	fields := strings.Fields(header)
	if len(fields) < 3 {
		t.Fatalf("nagłówek PPM: %q", header)
	}
	width, _ := strconv.Atoi(fields[1])
	i := 3 * (y*width + x)
	if i+2 >= len(data) {
		t.Fatalf("piksel (%d,%d) poza obrazem", x, y)
	}
	return int(data[i]), int(data[i+1]), int(data[i+2])
}
