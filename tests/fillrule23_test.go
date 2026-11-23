package tests

import (
	"bytes"
	"os/exec"
	"testing"
)

// fill-rule i clip-rule: evenodd zostawia otwór w ścieżce, której podścieżki się zawierają (SVG 2
// § 13.3.4, CSS Masking 1 § 6.1). Przejęty kod czytał atrybut „fill-rull” i obie reguły pomijał, więc
// pierścień wypełniał się w całości. Wzorzec: ta sama ścieżka z wewnętrzną podścieżką w przeciwnym kierunku,
// która przy regule nonzero daje ten sam otwór (te same krawędzie, więc te same piksele).
func TestFillRuleAndClipRuleEvenOdd(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("brak pdftoppm")
	}
	const ring = "M10 10h100v100H10z M40 40h40v40H40z"
	const holed = `<svg width="120" height="120"><path d="M10 10h100v100H10z M40 40v40h40v-40z" fill="green"/></svg>`
	cases := []struct{ name, test, reference string }{
		{"fill-rule", `<svg width="120" height="120"><path d="` + ring + `" fill="green" fill-rule="evenodd"/></svg>`,
			holed},
		{"fill-rule-inherited", `<svg width="120" height="120"><g fill-rule="evenodd"><path d="` + ring + `" fill="green"/></g></svg>`,
			holed},
		{"fill-rule-css", `<style>path { fill-rule: evenodd }</style><svg width="120" height="120"><path d="` + ring + `" fill="green"/></svg>`,
			holed},
		{"clip-rule", `<svg width="120" height="120"><clipPath id="c"><path d="` + ring + `" clip-rule="evenodd"/></clipPath><rect width="120" height="120" fill="green" clip-path="url(#c)"/></svg>`,
			`<svg width="120" height="120"><clipPath id="c"><path d="M10 10h100v100H10z M40 40v40h40v-40z"/></clipPath><rect width="120" height="120" fill="green" clip-path="url(#c)"/></svg>`},
		{"nonzero-default", `<svg width="120" height="120"><path d="` + ring + `" fill="green"/></svg>`,
			`<svg width="120" height="120"><path d="` + ring + `" fill="green" fill-rule="nonzero"/></svg>`},
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := rasterize(t, renderString(t, "<!DOCTYPE html>"+c.test).PDF, dir, c.name+"-t")
			want := rasterize(t, renderString(t, "<!DOCTYPE html>"+c.reference).PDF, dir, c.name+"-r")
			if len(got) != 1 || len(want) != 1 || !bytes.Equal(got[0], want[0]) {
				t.Errorf("%s: raster różni się od wzorca", c.name)
			}
		})
	}
}
