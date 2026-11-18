package gopdfgo

import (
	"context"
	"testing"
)

// TestBackgroundsMisc sprawdza poprawki znalezione przy testach WPT css-backgrounds: tła, obramowania
// i cienie (CSS Backgrounds 3) oraz właściwość clip (CSS 2.2 § 11.1.2).
func TestBackgroundsMisc(t *testing.T) {
	fonts := testFonts(t)
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 } .a { position: absolute }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(head+body), Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	same := func(name, test, reference string) {
		t.Helper()
		if n := differentPixels(page(name, test), page(name+"-ref", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}

	// obraz tła (tu gradient) nie dziedziczy przezroczystości półprzezroczystego obramowania
	// poprzedniego pudełka
	same("pattern-alpha-reset", `<div style="height:20px;border:5px solid rgba(0,0,255,0.4)"></div>
		<div style="height:50px;background-image:linear-gradient(green,green)"></div>`,
		`<div style="height:20px;border:5px solid rgba(0,0,255,0.4)"></div>
		<div style="height:50px;background:green"></div>`)
	// clip: rect(top, right, bottom, left) – right i left liczone od lewej krawędzi obramowania
	same("clip-rect", `<div class=a style="top:10px;left:10px;width:100px;height:100px;background:green;
		clip:rect(10px, 60px, auto, 20px)"></div>`,
		`<div class=a style="top:20px;left:30px;width:40px;height:90px;background:green"></div>`)
}
