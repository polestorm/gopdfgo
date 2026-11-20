package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo"
)

// TestSVGForeignObject sprawdza <foreignObject> (SVG 2 § 23.2): treść HTML ułożona w prostokącie
// (x, y, width, height) układu SVG, przycięta do niego (chyba że overflow: visible), w SVG wstawionym
// w HTML i w pliku SVG czytanym jako XML; dalej <switch> (SVG 2 § 5.8.2) rysujący tylko pierwsze dziecko
// ze spełnionymi warunkami, <use> wskazujący <symbol> bez rozmiaru i przycięcie clipPathUnits=objectBoundingBox.
func TestSVGForeignObject(t *testing.T) {
	dir := t.TempDir()
	head := `<style>@page { size: 200px 150px; margin: 0 } body { margin: 0 } svg, img { display: block }
		.a { position: absolute } .g { background: green } .b { background: blue }</style>`
	files := map[string]string{
		// plik SVG (XML): treść XHTML i switch w stylu draw.io; requiredFeatures nie istnieje w SVG 2
		"drawio.svg": `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="150"><switch>
			<foreignObject x="20" y="10" width="50" height="40" requiredFeatures="http://www.w3.org/TR/SVG11/feature#Extensibility">
			<div xmlns="http://www.w3.org/1999/xhtml" style="width:50px;height:40px;background:green"></div></foreignObject>
			<rect x="100" y="10" width="50" height="40" fill="red"/></switch></svg>`,
	}
	resolver := func(url string) (gopdfgo.Resource, error) {
		for name, data := range files {
			if strings.HasSuffix(url, "/"+name) {
				return gopdfgo.Resource{Data: []byte(data), MediaType: "image/svg+xml"}, nil
			}
		}
		return gopdfgo.DenyAll(url)
	}
	render := func(name, body string) []byte {
		t.Helper()
		result, err := gopdfgo.Render(context.Background(), []byte(head+body), gopdfgo.Options{Fonts: testFonts(t), Resolver: resolver, BaseURL: "file:///doc/"})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	for _, c := range []struct{ name, test, reference string }{
		// treść przesunięta o (x, y) i przekształcenie rodzica, przycięta do 60 na 30
		{"position-clip", `<svg width="200" height="150"><g transform="translate(10 5)"><foreignObject x="20" y="10" width="60" height="30">
			<div class=g style="width:100px;height:100px"></div></foreignObject></g></svg>`,
			`<div class="a g" style="left:30px;top:15px;width:60px;height:30px"></div>`},
		// overflow: visible wyłącza przycięcie
		{"overflow-visible", `<svg width="200" height="150"><foreignObject width="60" height="30" style="overflow:visible">
			<div class=g style="width:100px;height:100px"></div></foreignObject></svg>`,
			`<div class="a g" style="width:100px;height:100px"></div>`},
		// brak width i height (auto = 0): element nie jest rysowany
		{"auto-size", `<svg width="200" height="150"><foreignObject><div class=b style="width:50px;height:50px"></div></foreignObject></svg>`,
			``},
		// color z kontekstu SVG jest dziedziczony; arkusz dokumentu dotyczy treści foreignObject
		{"inherit-color", `<style>.box { width: 40px; height: 20px }</style><svg width="200" height="150" color="green">
			<foreignObject width="100" height="100"><div class=box style="background:currentColor"></div></foreignObject></svg>`,
			`<div class="a g" style="width:40px;height:20px"></div>`},
		// switch w pliku SVG: rysowany tylko foreignObject, bez zamiennika
		{"switch-svg-file", `<img src="drawio.svg">`,
			`<div class="a g" style="left:20px;top:10px;width:50px;height:40px"></div>`},
		// switch: dziecko z niespełnionym systemLanguage jest pomijane, rysowane jest tylko następne
		{"switch-system-language", `<svg width="200" height="150"><switch><rect width="50" height="50" fill="red" systemLanguage="xx"/>
			<rect width="50" height="50" fill="green"/><rect x="60" width="50" height="50" fill="red"/></switch></svg>`,
			`<div class="a g" style="width:50px;height:50px"></div>`},
		// requiredExtensions z przestrzenią nazw XHTML jest spełnione, nieznane rozszerzenie nie jest
		{"required-extensions", `<svg width="200" height="150"><switch><rect width="50" height="50" fill="red" requiredExtensions="urn:x"/>
			<foreignObject width="50" height="50" requiredExtensions="http://www.w3.org/1999/xhtml"><div class=g style="height:50px"></div></foreignObject>
			<rect width="50" height="50" fill="red"/></switch></svg>`,
			`<div class="a g" style="width:50px;height:50px"></div>`},
		// <symbol> nie jest rysowany wprost; <use> rysuje go w rozmiarze 100%
		{"use-symbol", `<svg width="200" height="150"><symbol id="s"><rect width="50" height="50" fill="green"/></symbol>
			<use href="#s" x="10"/></svg>`,
			`<div class="a g" style="left:10px;width:50px;height:50px"></div>`},
		// clipPathUnits=objectBoundingBox: górna połowa prostokąta 100 na 120
		{"clip-bbox", `<svg width="200" height="150"><clipPath id="c" clipPathUnits="objectBoundingBox"><rect width="1" height="0.5"/></clipPath>
			<rect x="10" y="10" width="100" height="120" fill="green" clip-path="url(#c)"/></svg>`,
			`<div class="a g" style="left:10px;top:10px;width:100px;height:60px"></div>`},
	} {
		if n := differentPixels(render(c.name, c.test), render(c.name+"-ref", c.reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", c.name, n)
		}
	}
}
