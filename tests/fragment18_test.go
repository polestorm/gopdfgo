package tests

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// Fragment adresu obrazu (ADR 0030): wymiar przestrzenny Media Fragments URI 1.0 (#xywh=…), widok SVG
// #svgView(…) i element <view> wskazany nazwą. Dokument obrazu jest wspólny dla fragmentów, a każdy
// fragment daje osobny obraz (własny rozmiar naturalny, własna forma współdzielona).

// quadrantsSVG: 200 na 200, ćwiartki czerwona, zielona / niebieska, żółta; widok #tr to prawa górna.
const quadrantsSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="200" viewBox="0 0 200 200">
<view id="tr" viewBox="100 0 100 100"/>
<view id="bad" viewBox="0 0 -5 5" preserveAspectRatio="none"/>
<rect width="100" height="100" fill="rgb(255,0,0)"/><rect x="100" width="100" height="100" fill="rgb(0,128,0)"/>
<rect y="100" width="100" height="100" fill="rgb(0,0,255)"/><rect x="100" y="100" width="100" height="100" fill="rgb(255,255,0)"/>
</svg>`

var (
	red    = [3]byte{255, 0, 0}
	green  = [3]byte{0, 128, 0}
	blue   = [3]byte{0, 0, 255}
	yellow = [3]byte{255, 255, 0}
	white  = [3]byte{255, 255, 255}
)

func quadrantsURL() string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(quadrantsSVG))
}

// renderFragments renderuje obrazy w rzędzie, każdy w polu 40 na 40 (img bez wymiarów dostaje rozmiar
// naturalny, więc wymiar podajemy w stylu tylko, gdy width jest ustawione).
func renderFragments(t *testing.T, body string) []byte {
	t.Helper()
	html := `<!DOCTYPE html><style>@page { size: 400px 100px; margin: 0 } body { margin: 0 }
img, div { display: block; float: left; margin-right: 10px }</style>` + body
	result := renderSharing(t, html, true, false)
	pages := rasterize(t, result.PDF, t.TempDir(), "fragment")
	if len(pages) != 1 {
		t.Fatalf("%d stron", len(pages))
	}
	return pages[0]
}

func expectPixel(t *testing.T, page []byte, x, y int, want [3]byte, what string) {
	t.Helper()
	if got := pixelRGB(t, page, x, y); got != want {
		t.Errorf("%s (%d, %d): %v, oczekiwano %v", what, x, y, got, want)
	}
}

func TestImageFragmentSpatial(t *testing.T) {
	u := quadrantsURL()
	// wycinek ma rozmiar naturalny prostokąta: img bez wymiarów ma 100 na 100, nie 200 na 200
	page := renderFragments(t, `<img src="`+u+`#xywh=100,100,100,100">`+
		`<div style="width:40px;height:40px;background:url('`+u+`#xywh=percent:0,50,50,50') 0 0 / 100% 100%"></div>`+
		`<img src="`+u+`#xywh=0,0,0,10" width="40" height="40">`)
	expectPixel(t, page, 50, 50, yellow, "xywh=100,100,100,100")
	expectPixel(t, page, 105, 50, white, "obok wycinka 100 na 100")
	expectPixel(t, page, 130, 20, blue, "percent:0,50,50,50")
	// niepoprawny wymiar (zerowa szerokość) jest pomijany: cały obraz
	expectPixel(t, page, 165, 5, red, "xywh z zerową szerokością")
	expectPixel(t, page, 185, 35, yellow, "xywh z zerową szerokością")
}

func TestImageFragmentSVGView(t *testing.T) {
	u := quadrantsURL()
	page := renderFragments(t, `<img src="`+u+`#svgView(viewBox(100,100,100,100))" width="40" height="40">`+
		`<img src="`+u+`#svgView(viewBox(0,100,100,100);preserveAspectRatio(none))" width="80" height="40">`+
		`<img src="`+u+`#svgView(transform(scale(2)))" width="40" height="40">`+
		`<img src="`+u+`#svgView(viewBox(0,0,0,0))" width="40" height="40" style="background: rgb(0,128,0)">`)
	expectPixel(t, page, 20, 20, yellow, "svgView(viewBox)")
	expectPixel(t, page, 52, 20, blue, "svgView z preserveAspectRatio(none)")
	expectPixel(t, page, 128, 20, blue, "svgView z preserveAspectRatio(none)")
	expectPixel(t, page, 160, 35, red, "svgView(transform(scale(2)))")
	// viewBox o zerowym wymiarze wyłącza rysowanie: widać tło elementu
	expectPixel(t, page, 200, 20, green, "svgView(viewBox(0,0,0,0))")
}

func TestImageFragmentViewElement(t *testing.T) {
	u := quadrantsURL()
	page := renderFragments(t, `<img src="`+u+`#tr" width="40" height="40">`+
		`<img src="`+u+`#bad" width="80" height="40">`+
		`<img src="`+u+`#missing" width="40" height="40">`)
	expectPixel(t, page, 20, 20, green, "<view id=tr>")
	// niepoprawny viewBox widoku ustępuje viewBox elementu głównego, a preserveAspectRatio="none"
	// obowiązuje: cały obraz rozciągnięty na 80 na 40
	expectPixel(t, page, 52, 5, red, "<view> z niepoprawnym viewBox")
	expectPixel(t, page, 128, 35, yellow, "<view> z niepoprawnym viewBox")
	// fragment, który nie jest widokiem, pokazuje dokument w widoku domyślnym
	expectPixel(t, page, 145, 5, red, "fragment bez <view>")
	expectPixel(t, page, 175, 35, yellow, "fragment bez <view>")
}

// Dwa fragmenty tego samego pliku w jednym dokumencie dają różne obrazy, także przy formie
// współdzielonej, a wynik jest taki sam jak przy rysowaniu każdego wystąpienia osobno.
func TestImageFragmentsShareDocumentNotImage(t *testing.T) {
	u := quadrantsURL()
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><style>@page { size: 400px 100px; margin: 0 } body { margin: 0 }
img { display: block; float: left }</style>`)
	for _, f := range []string{"", "#tr", "#svgView(viewBox(0,100,100,100))", "#xywh=100,100,100,100", "#xywh=0,0,100,100", "#tr", ""} {
		b.WriteString(`<img src="` + u + f + `" width="40" height="40">`)
	}
	shared := renderSharing(t, b.String(), true, false)
	separate := renderSharing(t, b.String(), false, false)
	dir := t.TempDir()
	a, c := rasterize(t, shared.PDF, dir, "shared"), rasterize(t, separate.PDF, dir, "separate")
	if len(a) != 1 || len(c) != 1 {
		t.Fatalf("%d i %d stron", len(a), len(c))
	}
	if !bytes.Equal(a[0], c[0]) {
		t.Errorf("%d różnych pikseli między formą współdzieloną a rysowaniem osobno", differentPixels(a[0], c[0]))
	}
	for i, want := range [][3]byte{red, green, blue, yellow, red, green, red} {
		expectPixel(t, a[0], i*40+10, 10, want, "obraz "+string(rune('1'+i)))
	}
	again := renderSharing(t, b.String(), true, false)
	if !bytes.Equal(shared.PDF, again.PDF) {
		t.Error("dwa renderowania tego samego dokumentu dają różne bajty")
	}
}

// Wymiar przestrzenny dotyczy też obrazu rastrowego.
func TestImageFragmentRaster(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 20, 10))
	for y := range 10 {
		for x := range 20 {
			c := color.NRGBA{255, 0, 0, 255}
			if x >= 10 {
				c = color.NRGBA{0, 0, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	u := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	page := renderFragments(t, `<img src="`+u+`#xywh=10,0,10,10" style="width:40px">`+
		`<img src="`+u+`" style="width:40px">`)
	expectPixel(t, page, 5, 20, blue, "wycinek rastrowy, lewa krawędź")
	expectPixel(t, page, 35, 35, blue, "wycinek rastrowy, prawa krawędź")
	// wycinek 10 na 10 ma proporcje 1:1, cały obraz 2:1
	expectPixel(t, page, 55, 5, red, "cały obraz")
	expectPixel(t, page, 55, 25, white, "pod całym obrazem")
}
