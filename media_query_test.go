package gopdfgo

import (
	"bytes"
	"testing"
)

// Zapytania o medium z cechami (Media Queries 4) oceniane względem medium odniesienia z ADR 0007:
// druk, strona A4 w pionie (794 na 1123 px). Reguła prawdziwa ma dać dokument z zielonym tekstem,
// fałszywa dokument bez zmian.
func TestMediaQueriesWithFeatures(t *testing.T) {
	const body = `<p>tekst</p>`
	cases := []struct {
		query string
		holds bool
	}{
		{"print", true},
		{"screen", false},
		{"all and (min-width: 600px)", true},
		{"print and (min-width: 10cm)", true},
		{"(min-width: 768px)", true},
		{"(min-width: 800px)", false},
		{"(max-width: 600px)", false},
		{"(max-width: 50em)", true},
		{"screen and (min-width: 600px)", false},
		{"not screen", true},
		{"not print", false},
		{"not screen and (min-width: 600px)", true},
		{"only print and (orientation: portrait)", true},
		{"(orientation: landscape)", false},
		{"(width >= 600px)", true},
		{"(width < 600px)", false},
		{"(600px <= width <= 800px)", true},
		{"(600px <= width <= 700px)", false},
		{"(min-width: 600px) and (max-width: 800px)", true},
		{"(max-width: 600px) or (min-height: 1000px)", true},
		{"not (min-width: 800px)", true},
		{"(prefers-color-scheme: dark)", false},
		{"(prefers-color-scheme: light)", true},
		{"(hover: hover)", false},
		{"(hover)", false},
		{"(color)", true},
		{"(monochrome)", false},
		{"(min-resolution: 2dppx)", false},
		{"(frobnicate: 3)", false},
		{"screen, print", true},
		{"screen, (max-width: 100px)", false},
		{"(frobnicate), print", true},
	}
	options := Options{Fonts: testFonts(t)}
	plain, err := Render(t.Context(), []byte(body), options)
	if err != nil {
		t.Fatal(err)
	}
	green, err := Render(t.Context(), []byte(`<style>p { color: green }</style>`+body), options)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		want := plain.PDF
		if c.holds {
			want = green.PDF
		}
		rule, err := Render(t.Context(), []byte(`<style>@media `+c.query+` { p { color: green } }</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(rule.PDF, want) {
			t.Errorf("@media %s: oczekiwano %v", c.query, c.holds)
		}
		attribute, err := Render(t.Context(), []byte(`<style media="`+c.query+`">p { color: green }</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(attribute.PDF, want) {
			t.Errorf("media=%q: oczekiwano %v", c.query, c.holds)
		}
	}
}
