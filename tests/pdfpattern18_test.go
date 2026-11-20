package tests

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo"
)

// Gradienty i wzorce w grupach przezroczystości (opacity, filtry, maski) nie mogą zależeć od tego,
// jak przeglądarka PDF odczytuje macierz wzorca w formie XObject: Apple PDFKit odnosi ją do układu
// strony, a nie formy (zob. internal/goweasyprint/pdf/placement.go). Zwykłe wypełnienia gradientem
// rysujemy bez wzorca kafelkowego, a wzorce, które zostają (kafle tła, obrys gradientem), mają
// macierz względem układu strony, bo grupa jest wywoływana pod jednostkowym CTM.
func TestPatternsInTransparencyGroups(t *testing.T) {
	fonts := testFonts(t)
	dir := t.TempDir()
	head := `<style>@page { size: 300px 150px; margin: 0 } body { margin: 0 }</style>`
	render := func(name, body string) ([]byte, []byte) {
		t.Helper()
		result, err := gopdfgo.Render(context.Background(), []byte(head+body), gopdfgo.Options{Fonts: fonts, Uncompressed: true})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return result.PDF, pages[0]
	}
	// barwa gradientu od (0x4f,0x8e,0xf7) do (0x1b,0x3f,0x8f) w punkcie f, pod krycie 0.8 na bieli
	gradient := func(f float64) [3]float64 {
		from, to := [3]float64{0x4f, 0x8e, 0xf7}, [3]float64{0x1b, 0x3f, 0x8f}
		f = math.Max(0, math.Min(1, f))
		var out [3]float64
		for i := range out {
			out[i] = 0.8*(from[i]+(to[i]-from[i])*f) + 0.2*255
		}
		return out
	}
	check := func(name string, ppm []byte, x, y int, want [3]float64) {
		t.Helper()
		got := pixelRGB(t, ppm, x, y)
		for i := range got {
			if math.Abs(float64(got[i])-want[i]) > 6 {
				t.Errorf("%s: piksel (%d, %d) = %v, oczekiwano %.0f", name, x, y, got, want)
				return
			}
		}
	}
	noTiling := func(name string, pdf []byte) {
		t.Helper()
		if bytes.Contains(pdf, []byte("/PatternType 1")) {
			t.Errorf("%s: wypełnienie gradientem używa wzorca kafelkowego", name)
		}
	}

	// gradient tła w pudełku z opacity (grupa przezroczystości)
	pdf, ppm := render("css-opacity", `<div style="width:200px;height:70px;margin:20px;border-radius:14px;opacity:.8;
		background:linear-gradient(135deg,#4f8ef7,#1b3f8f)"></div>`)
	noTiling("css-opacity", pdf)
	// 135deg: kierunek (1, 1)/√2, długość linii gradientu (200+70)/√2
	css := func(x, y float64) float64 { return ((x-120)+(y-55))/math.Sqrt2/(270/math.Sqrt2) + 0.5 }
	for _, p := range [][2]int{{30, 30}, {120, 55}, {210, 80}, {40, 80}, {200, 25}} {
		check("css-opacity", ppm, p[0], p[1], gradient(css(float64(p[0])+.5, float64(p[1])+.5)))
	}

	// gradient SVG (objectBoundingBox) w <g opacity>, obraz w <img>
	svg := `<svg xmlns='http://www.w3.org/2000/svg' width='300' height='150'><defs><linearGradient id='g' x1='0' y1='0' x2='1' y2='1'>` +
		`<stop offset='0' stop-color='%234f8ef7'/><stop offset='1' stop-color='%231b3f8f'/></linearGradient></defs>` +
		`<g opacity='0.8'><rect x='20' y='20' width='200' height='70' rx='14' fill='url(%23g)'/></g></svg>`
	pdf, ppm = render("svg-opacity", `<img style="display:block" src="data:image/svg+xml,`+svg+`">`)
	noTiling("svg-opacity", pdf)
	// w jednostkach prostokąta otaczającego gradient biegnie wzdłuż przekątnej: f = (u + v) / 2
	bbox := func(x, y float64) float64 { return ((x-20)/200 + (y-20)/70) / 2 }
	for _, p := range [][2]int{{30, 30}, {120, 55}, {210, 80}, {40, 80}, {200, 25}} {
		check("svg-opacity", ppm, p[0], p[1], gradient(bbox(float64(p[0])+.5, float64(p[1])+.5)))
	}

	// gradient w elemencie z filtrem (cień) też jest rysowany bez wzorca
	pdf, _ = render("css-filter", `<div style="width:200px;height:70px;margin:20px;filter:drop-shadow(4px 4px 3px #000);
		background:linear-gradient(135deg,#4f8ef7,#1b3f8f)"></div>`)
	noTiling("css-filter", pdf)

	// Kafle tła zostają wzorcem; jego macierz musi odnosić się do układu strony (skala 0.75 i odbicie
	// osi y z przekształcenia strony), a nie do układu grupy.
	pdf, ppm = render("tiles-opacity", `<div style="width:200px;height:50px;margin:20px;opacity:.8;
		background:linear-gradient(90deg,#c00 50%,#00c 50%) 0 0/20px 20px"></div>`)
	checkPageSpacePatterns(t, "tiles-opacity", pdf)
	red, blue := [3]float64{0.8*0xcc + 51, 51, 51}, [3]float64{51, 51, 0.8*0xcc + 51}
	for _, x := range []int{25, 45, 125, 205} {
		check("tiles-opacity", ppm, x, 30, red)
		check("tiles-opacity", ppm, x+10, 60, blue)
	}

	// obrys gradientem w <g opacity> (wzorzec zostaje)
	stroke := `<svg xmlns='http://www.w3.org/2000/svg' width='300' height='150'><defs><linearGradient id='g'>` +
		`<stop offset='0.5' stop-color='%23c00'/><stop offset='0.5' stop-color='%2300c'/></linearGradient></defs>` +
		`<g opacity='0.8'><rect x='30' y='30' width='200' height='80' fill='none' stroke='url(%23g)' stroke-width='20'/></g></svg>`
	pdf, ppm = render("svg-stroke", `<img style="display:block" src="data:image/svg+xml,`+stroke+`">`)
	checkPageSpacePatterns(t, "svg-stroke", pdf)
	// prostokąt otaczający obrysu: x od 20 do 240, połowa w x = 130
	check("svg-stroke", ppm, 40, 30, red)
	check("svg-stroke", ppm, 220, 30, blue)
	check("svg-stroke", ppm, 120, 110, red)
	check("svg-stroke", ppm, 140, 110, blue)
}

var patternMatrix = regexp.MustCompile(`/Matrix \[([^\]]*)\][^>]*/PatternType 1`)

// checkPageSpacePatterns sprawdza, że w pliku jest wzorzec kafelkowy i każdy ma macierz z przekształceniem
// strony (px → pt: w osi y skala 0.75 z odwróceniem), czyli odnosi się do układu strony, a nie grupy.
func checkPageSpacePatterns(t *testing.T, name string, pdf []byte) {
	t.Helper()
	matches := patternMatrix.FindAllSubmatch(pdf, -1)
	if len(matches) == 0 {
		t.Errorf("%s: brak wzorca kafelkowego", name)
		return
	}
	for _, m := range matches {
		f := strings.Fields(string(m[1]))
		if len(f) != 6 {
			t.Errorf("%s: macierz wzorca %q", name, m[1])
			continue
		}
		// (skala w x bywa inna: gradient objectBoundingBox rozciąga się do proporcji prostokąta)
		d, _ := strconv.ParseFloat(f[3], 64)
		if math.Abs(d+0.75) > 1e-3 {
			t.Errorf("%s: macierz wzorca [%s] nie odnosi się do układu strony", name, m[1])
		}
	}
}

// pixelRGB zwraca barwę piksela (x, y) obrazu PPM (P6).
func pixelRGB(t *testing.T, ppm []byte, x, y int) [3]byte {
	t.Helper()
	header, data := splitPPM(ppm)
	var w, h int
	if _, err := fmt.Sscan(strings.TrimPrefix(header, "P6"), &w, &h); err != nil || x >= w || y >= h {
		t.Fatalf("PPM %q, piksel (%d, %d)", header, x, y)
	}
	i := 3 * (y*w + x)
	return [3]byte{data[i], data[i+1], data[i+2]}
}
