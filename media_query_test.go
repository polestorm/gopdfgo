package gopdfgo

import (
	"bytes"
	"testing"
)

// Zapytania o medium z cechami (Media Queries 4) oceniane względem obszaru strony (ADR 0012); tutaj
// strona A4 w pionie bez marginesów (794 na 1123 px). Reguła prawdziwa ma dać dokument z zielonym tekstem,
// fałszywa dokument bez zmian.
func TestMediaQueriesWithFeatures(t *testing.T) {
	const body = `<style>@page { margin: 0 }</style><p>tekst</p>`
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

// TestMediaQueriesUsePageArea pilnuje ADR 0012: szerokość, wysokość i orientacja w @media to obszar
// strony wynikający z @page (rozmiar arkusza bez marginesów), także gdy @page stoi po regule @media
// albo w arkuszu użytkownika.
func TestMediaQueriesUsePageArea(t *testing.T) {
	const body = `<p>tekst</p>`
	cases := []struct {
		name, page, query string
		holds             bool
	}{
		{"A4 z domyślnymi marginesami: obszar węższy niż 768 px", ``, "(min-width: 768px)", false},
		{"A4 bez marginesów", `@page { margin: 0 }`, "(min-width: 768px)", true},
		{"A4 z marginesami 2 cm: 643 px", `@page { margin: 2cm }`, "(max-width: 650px)", true},
		{"A3 w poziomie", `@page { size: A3 landscape }`, "(min-width: 1200px)", true},
		{"orientacja z @page", `@page { size: A4 landscape }`, "(orientation: landscape)", true},
		{"orientacja domyślna", ``, "(orientation: landscape)", false},
		{"wąski wydruk", `@page { size: 80mm 200mm; margin: 5mm }`, "(max-width: 300px)", true},
		{"wysokość obszaru", `@page { size: 80mm 200mm; margin: 5mm }`, "(min-height: 800px)", false},
	}
	for _, c := range cases {
		options := Options{Fonts: testFonts(t)}
		plain, err := Render(t.Context(), []byte(`<style>`+c.page+`</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		green, err := Render(t.Context(), []byte(`<style>`+c.page+` p { color: green }</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		want := plain.PDF
		if c.holds {
			want = green.PDF
		}
		// reguła @media przed @page: rozmiar nie zależy od kolejności w arkuszu
		rule, err := Render(t.Context(), []byte(`<style>@media `+c.query+` { p { color: green } } `+c.page+`</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(rule.PDF, want) {
			t.Errorf("%s: @media %s, oczekiwano %v", c.name, c.query, c.holds)
		}
		// @page w dokumencie, @media w arkuszu użytkownika
		options.UserCSS = `@media ` + c.query + ` { p { color: green } }`
		user, err := Render(t.Context(), []byte(`<style>`+c.page+`</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(user.PDF, want) {
			t.Errorf("%s: arkusz użytkownika, @media %s, oczekiwano %v", c.name, c.query, c.holds)
		}
	}
}
