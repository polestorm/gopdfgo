package tests

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"testing"
)

// Pojedyncze poprawki SVG z sondażu W3C svg/ (2026-11-21): visibility: collapse, var() w atrybutach
// geometrii, overflow: auto, zerowy viewBox, preserveAspectRatio dokumentu wstawionego przez <image>,
// mask na zewnętrznym <svg>, mix-blend-mode i isolation w HTML, dziedziczenie w drzewie <use>,
// text-shadow tekstu SVG, @font-face w obrazie SVG, opacity z atrybutu style zewnętrznego <svg>.
// Każdy przypadek porównuje piksele z zapisem równoważnym, który działał już wcześniej.

func TestSVGSingleFixes(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("brak pdftoppm")
	}
	font, err := os.ReadFile("../testdata/fonts/NotoSerif-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	// obraz SVG z regułą @font-face o źródle data: (dozwolone w trybie statycznym obrazu)
	fontURL := `data:font/ttf;base64,` + base64.StdEncoding.EncodeToString(font)
	fontSVG := `<svg xmlns="http://www.w3.org/2000/svg" width="300" height="60"><style>@font-face { font-family: Embedded; src: url("` + fontURL + `") } text { font: 30px Embedded, monospace }</style><text x="5" y="40">Hamburg</text></svg>`
	cases := []struct {
		name, test, reference string
		quirks                bool
	}{
		{
			// visibility: collapse działa w SVG jak hidden (CSS Display 3 § 4)
			"visibility-collapse",
			`<svg width="200" height="100"><rect width="100" height="100" fill="green"/><rect x="100" width="100" height="100" fill="red" style="visibility: collapse"/></svg>`,
			`<svg width="200" height="100"><rect width="100" height="100" fill="green"/></svg>`, false,
		},
		{
			// var() w atrybucie geometrii (CSS Variables 1 § 3)
			"var-length-attribute",
			`<style>circle { --r: 40px }</style><svg width="100" height="100"><circle cx="50" cy="50" r="var(--r)" fill="green"/></svg>`,
			`<svg width="100" height="100"><circle cx="50" cy="50" r="40" fill="green"/></svg>`, false,
		},
		{
			// liczba bez jednostki z var() jest nieważna poza trybem zgodności wstecznej
			"var-unitless-standards",
			`<style>circle { --r: 40 }</style><svg width="100" height="100"><circle cx="50" cy="50" r="var(--r)" fill="red"/></svg>`,
			`<svg width="100" height="100"></svg>`, false,
		},
		{
			"var-unitless-quirks",
			`<style>circle { --r: 40 }</style><svg width="100" height="100"><circle cx="50" cy="50" r="var(--r)" fill="green"/></svg>`,
			`<svg width="100" height="100"><circle cx="50" cy="50" r="40" fill="green"/></svg>`, true,
		},
		{
			// overflow: auto przycina jak hidden (CSS Overflow 3 § 3, svgwg #1072)
			"overflow-auto",
			`<svg width="100" height="100" style="overflow: visible"><svg width="60" height="60" style="overflow: auto"><rect width="250" height="250" fill="green"/></svg></svg>`,
			`<svg width="100" height="100" style="overflow: visible"><svg width="60" height="60" style="overflow: hidden"><rect width="250" height="250" fill="green"/></svg></svg>`, false,
		},
		{
			// viewBox o zerowej szerokości wyłącza rysowanie (SVG 2 § 8.6)
			"zero-viewbox",
			`<svg width="100" height="100" viewBox="0 0 0 100"><rect width="100" height="100" fill="red"/></svg>` +
				`<svg width="100" height="100"><svg viewBox="0 0 100 0"><rect width="100" height="100" fill="red"/></svg></svg>`,
			`<svg width="100" height="100"></svg><svg width="100" height="100"></svg>`, false,
		},
		{
			// <image> pomija preserveAspectRatio elementu głównego wstawianego dokumentu (SVG 2 § 12.3)
			"image-ignores-root-aspect-ratio",
			`<svg width="220" height="120"><image x="10" y="10" width="200" height="100" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' width='100' height='100' viewBox='0 0 200 100'><rect width='200' height='100' fill='green'/></svg>"/></svg>`,
			`<svg width="220" height="120"><rect x="60" y="10" width="100" height="100" fill="green"/></svg>`, false,
		},
		{
			// mask z arkusza na zewnętrznym <svg> stosuje rysowanie SVG, jak maskę elementu wewnątrz
			"outer-svg-mask",
			`<style>svg.m { mask: url(#m) }</style><svg class="m" width="100" height="100"><mask id="m" maskUnits="userSpaceOnUse" x="0" y="0" width="100" height="100"><rect width="50" height="100" fill="white"/></mask><rect width="100" height="100" fill="green"/></svg>`,
			`<svg width="100" height="100"><mask id="m" maskUnits="userSpaceOnUse" x="0" y="0" width="100" height="100"><rect width="50" height="100" fill="white"/></mask><g mask="url(#m)"><rect width="100" height="100" fill="green"/></g></svg>`, false,
		},
		{
			// opacity z atrybutu style zewnętrznego <svg> stosuje tylko pudełko CSS (wcześniej podwójnie)
			"outer-svg-style-opacity",
			`<svg style="opacity: 0.5; display: block" width="100" height="100"><rect width="100" height="100" fill="black"/></svg>`,
			`<div style="opacity: 0.5; width: 100px; height: 100px; background: black"></div>`, false,
		},
		{
			// mix-blend-mode na elemencie HTML (CSS Compositing 1 § 3.1): czerwień na zieleni daje czerń
			// w trybie multiply i darken; bez mieszania byłaby czerwona
			"mix-blend-mode",
			`<div style="background: lime; padding: 10px"><div style="mix-blend-mode: multiply; width: 100px; height: 100px; background: red"></div></div>`,
			`<div style="background: lime; padding: 10px"><div style="mix-blend-mode: darken; width: 100px; height: 100px; background: red"></div></div>`, false,
		},
		{
			// isolation: isolate tworzy kontekst nakładania: z-index potomka nie wynosi go nad rodzeństwo
			"isolation-isolate",
			`<div style="isolation: isolate"><div style="position: absolute; z-index: 5; width: 100px; height: 100px; background: red"></div></div><div style="position: relative; width: 100px; height: 100px; background: green"></div>`,
			`<div style="width: 100px; height: 100px; background: green"></div>`, false,
		},
		{
			// treść <use> dziedziczy po <use>, a nie po miejscu w dokumencie (SVG 2 § 5.6.3)
			"use-inheritance",
			`<style>#t rect { stroke: red; stroke-width: 10px }</style><svg width="120" height="120"><defs><g id="s" fill="red"><rect width="100" height="100"/></g></defs><g id="t"><use href="#s" fill="green"/></g></svg>`,
			`<svg width="120" height="120"><g fill="red"><rect width="100" height="100"/></g></svg>`, false,
		},
		{
			// text-shadow bez rozmycia to kopia tekstu przesunięta i w kolorze cienia
			"text-shadow",
			`<svg width="300" height="80"><text x="10" y="40" style="font: 30px sans-serif; text-shadow: 4px 6px 0 red" fill="green">Shadow</text></svg>`,
			`<svg width="300" height="80"><text x="14" y="46" style="font: 30px sans-serif" fill="red">Shadow</text><text x="10" y="40" style="font: 30px sans-serif" fill="green">Shadow</text></svg>`, false,
		},
		{
			// @font-face ze źródłem data: w obrazie SVG (font szeryfowy, inny niż font zastępczy testów)
			"svg-image-font-face",
			`<img src="data:image/svg+xml;base64,` + base64.StdEncoding.EncodeToString([]byte(fontSVG)) + `">`,
			`<style>@font-face { font-family: Reference; src: url("` + fontURL + `") }</style><svg width="300" height="60" style="display: inline"><text x="5" y="40" style="font: 30px Reference">Hamburg</text></svg>`, false,
		},
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doctype := "<!DOCTYPE html>"
			if c.quirks {
				doctype = ""
			}
			test := rasterize(t, renderString(t, doctype+c.test).PDF, dir, c.name+"-test")
			reference := rasterize(t, renderString(t, doctype+c.reference).PDF, dir, c.name+"-ref")
			if len(test) != len(reference) {
				t.Fatalf("%d stron, wzorzec ma %d", len(test), len(reference))
			}
			for i := range test {
				if !bytes.Equal(test[i], reference[i]) {
					t.Errorf("strona %d różni się od wzorca", i+1)
				}
			}
		})
	}
}
