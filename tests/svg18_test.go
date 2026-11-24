package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
)

// Poprawki SVG z korpusu testdata/svg (2026-11-20), sprawdzane pikselami wobec wzorców w HTML:
//   - plik SVG bez viewBox, z width i height, rozciąga się do rozmiaru obrazu w <img> i w tle (Chrome
//     dopisuje viewBox "0 0 width height" i preserveAspectRatio="none"); w <object> i <embed> nie skaluje
//     się, bo jest osobnym dokumentem; wcześniej był zawsze rysowany w skali 1:1 i ucinany;
//   - filtr z nieobsługiwanym prymitywem (feMorphology; cień z feGaussianBlur jest już rysowany wektorowo,
//     zob. svgfilter18_test.go) jest pomijany, a element narysowany w całości;
//     wcześniej feOffset z łańcucha cienia przesuwał cały element;
//   - gradient w jednostkach objectBoundingBox w prostokącie innym niż kwadrat pokrywa cały kształt;
//     wcześniej był przesunięty o (a-1)·x.
func TestSVGCorpusFixes(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"noviewbox.svg": `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100"><rect width="100" height="100" fill="green"/><rect x="100" width="100" height="100" fill="blue"/></svg>`,
		"filter.svg": `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100" viewBox="0 0 200 100"><defs><filter id="s">` +
			`<feMorphology in="SourceAlpha" radius="4"/><feOffset dx="30" dy="30" result="b"/><feMerge><feMergeNode/><feMergeNode in="SourceGraphic"/></feMerge></filter></defs>` +
			`<rect x="20" y="20" width="160" height="60" fill="green" filter="url(#s)"/></svg>`,
		"gradient.svg": `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100" viewBox="0 0 200 100"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="0">` +
			`<stop offset="0" stop-color="green"/><stop offset="1" stop-color="green"/></linearGradient></defs><rect x="40" y="20" width="140" height="40" fill="url(#g)"/></svg>`,
	}
	resolver := func(url string) (htmlpdf.Resource, error) {
		for name, data := range files {
			if strings.HasSuffix(url, "/"+name) {
				return htmlpdf.Resource{Data: []byte(data), MediaType: "image/svg+xml"}, nil
			}
		}
		return htmlpdf.DenyAll(url)
	}
	page := `<style>@page { size: 220px 120px; margin: 0 } body { margin: 0 } img { display: block }</style>`
	render := func(html string) []byte {
		t.Helper()
		result, err := htmlpdf.Render(context.Background(), []byte(page+html), htmlpdf.Options{Fonts: testFonts(t), Resolver: resolver, BaseURL: "file:///doc/"})
		if err != nil {
			t.Fatal(err)
		}
		return result.PDF
	}
	box := func(left, top, width, height int, color string) string {
		return `<div style="position:absolute;left:` + itoa(left) + `px;top:` + itoa(top) + `px;width:` + itoa(width) + `px;height:` + itoa(height) + `px;background:` + color + `"></div>`
	}
	for name, c := range map[string][2]string{
		// 200×100 narysowane w 100×50
		"noviewbox": {`<img src="noviewbox.svg" style="width:100px;height:50px">`, box(0, 0, 50, 50, "green") + box(50, 0, 50, 50, "blue")},
		// <object> to osobny dokument: plik bez viewBox nie skaluje się, tylko jest ucinany (Chrome 157)
		"object-noviewbox": {`<object data="noviewbox.svg" type="image/svg+xml" style="display:block;width:100px;height:50px"></object>`, box(0, 0, 100, 50, "green")},
		"filter":           {`<img src="filter.svg">`, box(20, 20, 160, 60, "green")},
		"gradient":         {`<img src="gradient.svg">`, box(40, 20, 140, 40, "green")},
	} {
		got := rasterize(t, render(c[0]), dir, name)
		want := rasterize(t, render(c[1]), dir, name+"-ref")
		if len(got) != 1 || len(want) != 1 {
			t.Fatalf("%s: strony %d i %d", name, len(got), len(want))
		}
		if n := differentPixels(got[0], want[0]); n > 20 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
}
