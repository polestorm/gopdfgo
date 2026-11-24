package tests

import (
	"bytes"
	"github.com/polestorm/gopdfgo/htmlpdf"
	"regexp"
	"strconv"
	"testing"
)

// Zapytania o medium z cechami (Media Queries 4) oceniane względem obszaru strony arkusza domyślnego
// (ADR 0019); tutaj arkusz użytkownika zeruje marginesy, więc medium to A4 w pionie (794 na 1123 px).
// Reguła prawdziwa ma dać dokument z zielonym tekstem, fałszywa dokument bez zmian.
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
	options := htmlpdf.Options{Fonts: testFonts(t), UserCSS: `@page { margin: 0 }`}
	plain, err := htmlpdf.Render(t.Context(), []byte(body), options)
	if err != nil {
		t.Fatal(err)
	}
	green, err := htmlpdf.Render(t.Context(), []byte(`<style>p { color: green }</style>`+body), options)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		want := plain.PDF
		if c.holds {
			want = green.PDF
		}
		rule, err := htmlpdf.Render(t.Context(), []byte(`<style>@media `+c.query+` { p { color: green } }</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(rule.PDF, want) {
			t.Errorf("@media %s: oczekiwano %v", c.query, c.holds)
		}
		attribute, err := htmlpdf.Render(t.Context(), []byte(`<style media="`+c.query+`">p { color: green }</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(attribute.PDF, want) {
			t.Errorf("media=%q: oczekiwano %v", c.query, c.holds)
		}
	}
}

// TestMediaQueriesUseDefaultSheet pilnuje ADR 0019 (CSS Page 3 § 7.1): szerokość, wysokość i orientacja
// w @media to obszar strony arkusza domyślnego (A4 w pionie bez marginesów 75 px: ok. 644 na 973 px albo
// rozmiar i marginesy z @page arkusza użytkownika); @page autora nie ma na nie wpływu.
func TestMediaQueriesUseDefaultSheet(t *testing.T) {
	const body = `<p>tekst</p>`
	cases := []struct {
		name, page, user, query string
		holds                   bool
	}{
		{"A4 z domyślnymi marginesami: obszar węższy niż 768 px", ``, ``, "(min-width: 768px)", false},
		{"A4 z domyślnymi marginesami: szerokość 644 px", ``, ``, "(640px <= width <= 650px)", true},
		{"A4 z domyślnymi marginesami: wysokość 973 px", ``, ``, "(970px <= height <= 980px)", true},
		{"marginesy autora bez wpływu", `@page { margin: 0 }`, ``, "(min-width: 768px)", false},
		{"rozmiar autora bez wpływu", `@page { size: A3 landscape }`, ``, "(min-width: 700px)", false},
		{"orientacja autora bez wpływu", `@page { size: A4 landscape }`, ``, "(orientation: landscape)", false},
		{"wąski wydruk autora bez wpływu", `@page { size: 80mm 200mm; margin: 5mm }`, ``, "(max-width: 300px)", false},
		{"orientacja domyślna", ``, ``, "(orientation: portrait)", true},
		{"marginesy użytkownika", `@page { margin: 2cm }`, `@page { margin: 0 }`, "(min-width: 768px)", true},
		{"A3 w poziomie od użytkownika", ``, `@page { size: A3 landscape }`, "(min-width: 1400px)", true},
		{"orientacja od użytkownika", `@page { size: A4 }`, `@page { size: A4 landscape }`, "(orientation: landscape)", true},
		{"wąski wydruk od użytkownika", ``, `@page { size: 80mm 200mm; margin: 5mm }`, "(max-width: 300px)", true},
		{"wysokość od użytkownika", ``, `@page { size: 80mm 200mm; margin: 5mm }`, "(min-height: 800px)", false},
	}
	for _, c := range cases {
		options := htmlpdf.Options{Fonts: testFonts(t), UserCSS: c.user}
		plain, err := htmlpdf.Render(t.Context(), []byte(`<style>`+c.page+`</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		green, err := htmlpdf.Render(t.Context(), []byte(`<style>`+c.page+` p { color: green }</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		want := plain.PDF
		if c.holds {
			want = green.PDF
		}
		for _, order := range []string{
			`@media ` + c.query + ` { p { color: green } } ` + c.page,
			c.page + ` @media ` + c.query + ` { p { color: green } }`,
		} {
			rule, err := htmlpdf.Render(t.Context(), []byte(`<style>`+order+`</style>`+body), options)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(rule.PDF, want) {
				t.Errorf("%s: %s, oczekiwano %v", c.name, order, c.holds)
			}
		}
		// @page w dokumencie, @media w arkuszu użytkownika
		options.UserCSS = c.user + ` @media ` + c.query + ` { p { color: green } }`
		user, err := htmlpdf.Render(t.Context(), []byte(`<style>`+c.page+`</style>`+body), options)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(user.PDF, want) {
			t.Errorf("%s: arkusz użytkownika, @media %s, oczekiwano %v", c.name, c.query, c.holds)
		}
	}
}

// TestPageSizeIgnoredInSizeMedia: size w @page wewnątrz @media zależnego od rozmiaru medium (także przez
// atrybut media i @import) jest pomijane, pozostałe deklaracje @page działają (CSS Page 3 § 7.1).
func TestPageSizeIgnoredInSizeMedia(t *testing.T) {
	options := htmlpdf.Options{Fonts: testFonts(t)}
	pageSize := func(html string) [2]float64 {
		t.Helper()
		result, err := htmlpdf.Render(t.Context(), []byte(html+`<p>tekst</p>`), options)
		if err != nil {
			t.Fatal(err)
		}
		width, height := firstPageSize(t, result.PDF)
		return [2]float64{width, height}
	}
	a4 := pageSize(``)
	a5 := pageSize(`<style>@page { size: A5 }</style>`)
	if a4 == a5 {
		t.Fatalf("A4 i A5 dają ten sam rozmiar %v", a4)
	}
	for _, c := range []struct {
		html string
		want [2]float64
	}{
		{`<style>@media (min-width: 100px) { @page { size: A5 } }</style>`, a4},
		{`<style>@media (orientation: portrait) { @page { size: A5 } }</style>`, a4},
		{`<style>@media (min-aspect-ratio: 1/10) { @page { size: A5 } }</style>`, a4},
		{`<style>@media print and (width > 100px) { @supports (color: red) { @page { size: A5 } } }</style>`, a4},
		{`<style media="(min-width: 100px)">@page { size: A5 }</style>`, a4},
		{`<style>@media print { @page { size: A5 } }</style>`, a5},
		{`<style>@media (color) { @page { size: A5 } }</style>`, a5},
		{`<style>@media (min-width: 100px) { p { color: green } } @page { size: A5 }</style>`, a5},
	} {
		if got := pageSize(c.html); got != c.want {
			t.Errorf("%s: strona %v, oczekiwano %v", c.html, got, c.want)
		}
	}
}

var mediaBoxPattern = regexp.MustCompile(`/MediaBox\s*\[\s*0 0 ([0-9.]+) ([0-9.]+)\s*\]`)

// firstPageSize czyta rozmiar pierwszej strony z /MediaBox pliku PDF.
func firstPageSize(t *testing.T, pdf []byte) (width, height float64) {
	t.Helper()
	match := mediaBoxPattern.FindSubmatch(pdf)
	if match == nil {
		t.Fatal("brak /MediaBox w pliku PDF")
	}
	width, _ = strconv.ParseFloat(string(match[1]), 64)
	height, _ = strconv.ParseFloat(string(match[2]), 64)
	return width, height
}
