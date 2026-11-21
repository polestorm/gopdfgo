package tests

import (
	"bytes"
	"os/exec"
	"testing"
)

// Drobne poprawki SVG ważne dla wykresów i plików z edytorów grafiki: dziedziczenie treści gradientu
// i wzorca przez href, kreskowanie z nieparzystą listą i ujemnym przesunięciem, overflow: clip
// w zagnieżdżonym <svg>, naturalny rozmiar obrazu SVG o zdegenerowanych albo ujemnych wymiarach.
// Każdy przypadek porównuje piksele z zapisem równoważnym, który działał już wcześniej.

func TestSVGDocumentFixes(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("brak pdftoppm")
	}
	cases := []struct {
		name, test, reference string
	}{
		{
			// gradient z samym href przejmuje stopnie wskazanego (SVG 2 § 14.2.3); href ma
			// pierwszeństwo przed xlink:href
			"gradient-href",
			`<svg width="200" height="100" xmlns:xlink="http://www.w3.org/1999/xlink">
			  <linearGradient id="a"><stop offset="5%" stop-color="green"/><stop offset="95%" stop-color="gold"/></linearGradient>
			  <linearGradient id="b"><stop offset="0" stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>
			  <linearGradient id="c" href="#a" xlink:href="#b" x2="0.5" spreadMethod="reflect"/>
			  <rect width="200" height="100" fill="url(#c)"/></svg>`,
			`<svg width="200" height="100">
			  <linearGradient id="c" x2="0.5" spreadMethod="reflect"><stop offset="5%" stop-color="green"/><stop offset="95%" stop-color="gold"/></linearGradient>
			  <rect width="200" height="100" fill="url(#c)"/></svg>`,
		},
		{
			// wzorzec bez dzieci bierze dzieci wskazanego (SVG 2 § 14.3.3)
			"pattern-href",
			`<svg width="200" height="100">
			  <pattern id="p" width="20" height="20" patternUnits="userSpaceOnUse"><rect width="10" height="10" fill="green"/></pattern>
			  <pattern id="q" href="#p"/>
			  <rect width="200" height="100" fill="url(#q)"/></svg>`,
			`<svg width="200" height="100">
			  <pattern id="q" width="20" height="20" patternUnits="userSpaceOnUse"><rect width="10" height="10" fill="green"/></pattern>
			  <rect width="200" height="100" fill="url(#q)"/></svg>`,
		},
		{
			// lista nieparzysta jest powtarzana, więc okres to podwójna suma (SVG 2 § 13.5.6)
			"dash-odd-negative-offset",
			`<svg width="300" height="60">
			  <path d="M10,20 L290,20" stroke="black" stroke-width="8" stroke-dasharray="40 20 10" stroke-dashoffset="-15"/>
			  <path d="M10,45 L290,45" stroke="black" stroke-width="8" stroke-dasharray="30" stroke-dashoffset="-45"/></svg>`,
			`<svg width="300" height="60">
			  <path d="M10,20 L290,20" stroke="black" stroke-width="8" stroke-dasharray="40 20 10 40 20 10" stroke-dashoffset="-15"/>
			  <path d="M10,45 L290,45" stroke="black" stroke-width="8" stroke-dasharray="30 30" stroke-dashoffset="-45"/></svg>`,
		},
		{
			// overflow: clip przycina okno widoku tak jak hidden (SVG 2 § 8.13)
			"nested-svg-overflow-clip",
			`<svg width="100" height="100" style="overflow: visible"><svg width="60" height="60" style="overflow: clip"><rect width="250" height="250" fill="green"/></svg></svg>`,
			`<svg width="100" height="100" style="overflow: visible"><svg width="60" height="60" style="overflow: hidden"><rect width="250" height="250" fill="green"/></svg></svg>`,
		},
		{
			// width="0": proporcje z viewBox, nie zdegenerowane 0/50 (CSS Values 4 § 7.3)
			"intrinsic-degenerate-ratio",
			`<img style="width: 100px; background: green" src="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' width='0' height='50px' viewBox='0 0 1 1'></svg>">`,
			`<img style="width: 100px; background: green" src="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' width='1' height='1' viewBox='0 0 1 1'></svg>">`,
		},
		{
			// ujemne wymiary są błędne i działają jak auto (SVG 2 § 8.2)
			"intrinsic-negative-size",
			`<img style="width: 100px; background: green" src="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' width='-1px' height='-50px' viewBox='0 0 1 1'></svg>">`,
			`<img style="width: 100px; background: green" src="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' width='1' height='1' viewBox='0 0 1 1'></svg>">`,
		},
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			test := rasterize(t, renderString(t, "<!DOCTYPE html>"+c.test).PDF, dir, c.name+"-test")
			reference := rasterize(t, renderString(t, "<!DOCTYPE html>"+c.reference).PDF, dir, c.name+"-ref")
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
