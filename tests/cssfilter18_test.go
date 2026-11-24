package tests

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
)

// filterPNG zwraca obraz PNG 4×4 w jednym kolorze jako adres data:.
func filterPNG(t *testing.T, c color.NRGBA) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// ppmPixel zwraca barwę piksela (x, y) obrazu PPM.
func ppmPixel(ppm []byte, x, y int) [3]int {
	header, data := splitPPM(ppm)
	var width int
	for i, field := range strings.Fields(header) {
		if i == 1 {
			for _, r := range field {
				width = width*10 + int(r-'0')
			}
		}
	}
	at := (y*width + x) * 3
	return [3]int{int(data[at]), int(data[at+1]), int(data[at+2])}
}

// TestCSSFilter sprawdza właściwość filter (Filter Effects 1 § 5, § 13): macierze barw na kolorach
// jednolitych i na pikselach obrazów, cień drop-shadow() o kształcie treści, kolejność funkcji, krycie
// opacity(), rozmycie blur(), blok zawierający dla fixed i wydobywanie tekstu bez powtórzeń.
func TestCSSFilter(t *testing.T) {
	ahem, err := os.ReadFile("../testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]htmlpdf.Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 } .t { font: 20px/20px Ahem; color: green }
		.b { width: 100px; height: 60px; margin: 20px; background: red }</style>`
	dir := t.TempDir()
	render := func(body string) []byte {
		t.Helper()
		result, err := htmlpdf.Render(context.Background(), []byte(head+body), htmlpdf.Options{Fonts: fonts})
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
	// near dopuszcza allowed pikseli różnicy: krawędzie kształtu narysowanego przez maskę miękką
	// pdftoppm wygładza inaczej niż krawędzie wypełnienia
	near := func(name string, allowed int, test, reference string) {
		t.Helper()
		if n := differentPixels(page(name, test), page(name+"-ref", reference)); n != 0 && (n < 0 || n > allowed) {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	same := func(name, test, reference string) {
		t.Helper()
		near(name, 0, test, reference)
	}
	// color sprawdza barwę piksela (x, y) z dokładnością do 2 na kanał
	color := func(name, body string, x, y int, want [3]int) {
		t.Helper()
		got := ppmPixel(page(name, body), x, y)
		for i := range got {
			if d := got[i] - want[i]; d > 2 || d < -2 {
				t.Errorf("%s: piksel (%d, %d) ma barwę %v, oczekiwano %v", name, x, y, got, want)
				return
			}
		}
	}

	// macierze barw (§ 13.2) na kolorze jednolitym: grayscale(1) z czerwieni daje 0,2126 (54/255)
	color("grayscale-box", `<div class=b style="filter: grayscale(1)"></div>`, 70, 50, [3]int{54, 54, 54})
	color("grayscale-half", `<div class=b style="filter: grayscale(50%)"></div>`, 70, 50, [3]int{155, 27, 27})
	color("invert", `<div class=b style="filter: invert(1)"></div>`, 70, 50, [3]int{0, 255, 255})
	color("invert-half", `<div class=b style="filter: invert(0.25); background: black"></div>`, 70, 50, [3]int{64, 64, 64})
	// sepia(1) z bieli: (1,351; 1,203; 0,937) obcięte do [0, 1]
	color("sepia", `<div class=b style="filter: sepia(1); background: white"></div>`, 70, 50, [3]int{255, 255, 239})
	color("brightness", `<div class=b style="filter: brightness(0.5)"></div>`, 70, 50, [3]int{128, 0, 0})
	color("contrast", `<div class=b style="filter: contrast(0)"></div>`, 70, 50, [3]int{128, 128, 128})
	color("hue-rotate", `<div class=b style="filter: hue-rotate(180deg)"></div>`, 70, 50, [3]int{0, 109, 109})
	// funkcje po kolei z obcięciem po każdej: brightness(2) obcina czerwień do 1, potem brightness(0.5)
	color("chain-clamp", `<div class=b style="filter: brightness(2) brightness(0.5); background: rgb(200, 100, 0)"></div>`,
		70, 50, [3]int{128, 100, 0})
	// tekst, obramowanie i potomkowie też
	same("grayscale-descendants", `<div style="filter: grayscale(1); margin: 20px"><div style="border: 10px solid red; height: 20px"></div><div class=t>XX</div></div>`,
		`<div style="margin: 20px"><div style="border: 10px solid rgb(54, 54, 54); height: 20px"></div><div class=t style="color: rgb(92, 92, 92)">XX</div></div>`)

	// obraz rastrowy: piksele przekształcone przed osadzeniem
	red := filterPNG(t, color2(255, 0, 0))
	same("grayscale-png", `<img src="`+red+`" style="filter: grayscale(1); width: 80px; height: 80px">`,
		`<img src="`+filterPNG(t, color2(54, 54, 54))+`" style="width: 80px; height: 80px">`)
	same("invert-png", `<img src="`+red+`" style="filter: invert(1); width: 80px; height: 80px">`,
		`<img src="`+filterPNG(t, color2(0, 255, 255))+`" style="width: 80px; height: 80px">`)

	// drop-shadow() bez rozmycia: cień o kształcie treści, przesunięty, pod treścią
	same("drop-shadow-text", `<div class=t style="filter: drop-shadow(10px 5px 0 blue); margin: 20px">XX X</div>`,
		`<div class=t style="text-shadow: 10px 5px 0 blue; margin: 20px">XX X</div>`)
	near("drop-shadow-rounded", 20, `<div class=b style="filter: drop-shadow(15px 10px 0 blue); border-radius: 20px; background: green"></div>`,
		`<div class=b style="position: absolute; left: 15px; top: 10px; border-radius: 20px; background: blue"></div>
		<div class=b style="position: absolute; left: 0; top: 0; border-radius: 20px; background: green"></div>`)
	// domyślny kolor cienia to currentColor
	same("drop-shadow-currentcolor", `<div class=b style="filter: drop-shadow(15px 10px); color: blue; background: green"></div>`,
		`<div class=b style="box-shadow: 15px 10px 0 blue; background: green"></div>`)
	// kolejność: funkcja barwna po cieniu przekształca też cień, przed cieniem tylko treść
	same("order-shadow-then-invert", `<div class=b style="filter: drop-shadow(15px 10px 0 blue) invert(1)"></div>`,
		`<div class=b style="box-shadow: 15px 10px 0 yellow; background: aqua"></div>`)
	same("order-invert-then-shadow", `<div class=b style="filter: invert(1) drop-shadow(15px 10px 0 blue)"></div>`,
		`<div class=b style="box-shadow: 15px 10px 0 blue; background: aqua"></div>`)
	// opacity() to krycie grupy
	same("opacity", `<div class=b style="filter: opacity(50%)"><div class=b style="margin: 10px; background: blue"></div></div>`,
		`<div class=b style="opacity: 0.5"><div class=b style="margin: 10px; background: blue"></div></div>`)

	// blur(): wnętrze dużego pudełka ma pełne krycie, daleko od krawędzi nie ma nic, na krawędzi około połowy
	blur := `<div style="filter: blur(4px); width: 200px; height: 120px; margin: 40px; background: blue"></div>`
	color("blur-inside", blur, 140, 100, [3]int{0, 0, 255})
	color("blur-outside", blur, 140, 15, [3]int{255, 255, 255})
	if edge := ppmPixel(page("blur-edge", blur), 140, 40); edge[0] < 80 || edge[0] > 180 {
		t.Errorf("blur-edge: piksel na krawędzi %v, oczekiwano około połowy krycia", edge)
	}
	// drop-shadow() z rozmyciem: cień pełny pod środkiem treści
	color("drop-shadow-blur", `<div style="filter: drop-shadow(0 100px 5px blue); width: 200px; height: 60px; margin: 20px; background: white"></div>`,
		120, 140, [3]int{0, 0, 255})

	// filter tworzy blok zawierający dla potomka fixed (Filter Effects 1 § 5)
	same("containing-block-fixed", `<div style="filter: grayscale(0); margin: 30px"><div style="position: fixed; top: 10px; left: 10px; width: 20px; height: 20px; background: green"></div></div>`,
		`<div style="position: absolute; top: 40px; left: 40px; width: 20px; height: 20px; background: green"></div>`)
	// filtr pudełka liniowego obejmuje wydzielony z niego blok i jest blokiem zawierającym dla absolute
	color("inline-split-block", `<span style="filter: grayscale(1)"><div class=b></div></span>`, 70, 50, [3]int{54, 54, 54})
	same("inline-containing-block", `<div class=t style="margin: 40px">X<span style="filter: grayscale(0)">XX<span style="position: absolute; left: 0; top: 0; width: 20px; height: 20px; background: blue"></span></span></div>`,
		`<div class=t style="margin: 40px">X<span style="position: relative">XX<span style="position: absolute; left: 0; top: 0; width: 20px; height: 20px; background: blue"></span></span></div>`)
	// url() bez obsługi: rysujemy bez filtra
	same("url-ignored", `<div class=b style="filter: url(#missing) grayscale(1)"></div>`, `<div class=b></div>`)

	// tekst rozmyty i z cieniem wydobywa się raz
	text := extractText(t, render(`<p style="filter: blur(2px)">Blurred words</p><p style="filter: drop-shadow(2px 2px 3px red)">Shadowed words</p>`))
	for word, want := range map[string]int{"Blurred": 1, "Shadowed": 1, "words": 2} {
		if got := bytes.Count(text, []byte(word)); got != want {
			t.Errorf("wydobyty tekst %q: %q %d razy, oczekiwano %d", text, word, got, want)
		}
	}
}

func color2(r, g, b uint8) color.NRGBA { return color.NRGBA{R: r, G: g, B: b, A: 255} }
