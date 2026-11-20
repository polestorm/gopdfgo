package tests

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo"
)

// inkColumns zwraca skrajne kolumny pikseli z farbą (ciemniejszych niż 128) w obrazie PPM, -1 bez farby.
func inkColumns(ppm []byte) (left, right int) {
	header, data := splitPPM(ppm)
	fields := strings.Fields(header)
	width, _ := strconv.Atoi(fields[1])
	left, right = -1, -1
	for i := 0; i+2 < len(data); i += 3 {
		if int(data[i])+int(data[i+1])+int(data[i+2]) < 3*128 {
			x := (i / 3) % width
			if left == -1 || x < left {
				left = x
			}
			if x > right {
				right = x
			}
		}
	}
	return left, right
}

// TestSVGTextShaping sprawdza skład tekstu SVG tym samym kształtowaniem co HTML (SVG 2 § 11.8):
// kerning i ligatury w obrębie fragmentu, text-anchor dla każdego fragmentu osobno, textLength
// liczony z przesunięć kształtowania, kolejność tekstu za elementami tspan i tekst wyodrębniany
// z PDF w kolejności dokumentu.
func TestSVGTextShaping(t *testing.T) {
	head := `<style>@page { size: 300px 120px; margin: 0 } body { margin: 0 } svg { display: block }</style>`
	dir := t.TempDir()
	render := func(body string) []byte {
		t.Helper()
		result, err := gopdfgo.Render(context.Background(), []byte(head+body),
			gopdfgo.Options{Fonts: testFonts(t), Resolver: gopdfgo.DenyAll})
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
	svg := func(content string) string {
		return `<svg width="300" height="120" font-family="Noto Sans">` + content + `</svg>`
	}

	// kerning: para AV jest węższa niż bez kerningu (font-kerning: none)
	_, kerned := inkColumns(page("kern", svg(`<text x="10" y="80" font-size="60">AVAV</text>`)))
	_, plain := inkColumns(page("kern-none", svg(`<text x="10" y="80" font-size="60" style="font-kerning: none">AVAV</text>`)))
	if kerned < 0 || plain < 0 || kerned >= plain-3 {
		t.Errorf("kerning: prawa krawędź %d px z kerningiem, %d px bez", kerned, plain)
	}

	// text-anchor: end dla każdego fragmentu osobno; oba wiersze kończą się przy x = 250
	// (źródło przesuwało cały element o szerokość jego prostokąta otaczającego)
	for _, word := range []string{"Wave", "x"} {
		_, right := inkColumns(page("anchor-"+word, svg(`<text text-anchor="end" font-size="40"><tspan x="250" y="40">Wave</tspan>`+
			`<tspan x="250" y="100" fill="white">Wave</tspan></text><text text-anchor="end" font-size="40" x="250" y="100">`+word+`</text>`)))
		if right < 244 || right > 250 {
			t.Errorf("text-anchor end (%s): prawa krawędź farby %d px, oczekiwana tuż przed 250", word, right)
		}
	}
	// drugi fragment wyrównany do swojego x, nie do szerokości całego elementu
	twoChunks := page("anchor-chunks", svg(`<text text-anchor="end" font-size="40"><tspan x="250" y="40" fill="white">Wavewave</tspan>`+
		`<tspan x="250" y="100">x</tspan></text>`))
	if left, right := inkColumns(twoChunks); right < 244 || right > 250 || left < 200 {
		t.Errorf("text-anchor end, dwa fragmenty: farba od %d do %d px", left, right)
	}

	// ligatura fi: z ligaturami tekst wygląda inaczej niż bez nich (font-variant-ligatures: none),
	// a wyodrębniony tekst to nadal „fi”
	liga := page("liga", svg(`<text x="10" y="80" font-size="60">fifi</text>`))
	noLiga := page("liga-none", svg(`<text x="10" y="80" font-size="60" font-variant-ligatures="none">fifi</text>`))
	if differentPixels(liga, noLiga) == 0 {
		t.Errorf("ligatura fi: obraz taki sam jak bez ligatur")
	}

	// textLength rozciąga tekst do podanej długości, odstępami albo także glifami
	for _, adjust := range []string{"spacing", "spacingAndGlyphs"} {
		left, right := inkColumns(page("textlength-"+adjust, svg(`<text x="20" y="80" font-size="40" textLength="260" lengthAdjust="`+adjust+`">abc</text>`)))
		if left < 20 || left > 30 || right < 265 || right > 280 {
			t.Errorf("textLength (%s): farba od %d do %d px, oczekiwana od ok. 20 do ok. 280", adjust, left, right)
		}
	}

	// tekst za elementem tspan stoi za nim (źródło łączyło go z tekstem przed tspan), a pdftotext
	// wyodrębnia całość w kolejności dokumentu
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("brak pdftotext")
	}
	pdf := render(svg(`<text x="10" y="60" font-size="30">Start <tspan fill="red">middle</tspan> end</text>`))
	cmd := exec.Command("pdftotext", "-", "-")
	cmd.Stdin = bytes.NewReader(pdf)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(strings.Trim(string(out), "\f\n ")); got != "Start middle end" {
		t.Errorf("pdftotext: %q, oczekiwane \"Start middle end\"", got)
	}
}
