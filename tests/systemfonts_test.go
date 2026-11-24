//go:build systemfonts

package tests

// Fonty systemowe w wariancie ze znacznikiem systemfonts (ADR 0039):
//
//	go test -tags systemfonts ./tests/ -run SystemFonts
//
// Katalogi systemu zastępujemy katalogami z testdata/fonts (systemfonts.SetDirectoriesForTest), żeby
// wynik nie zależał od fontów maszyny.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
	"github.com/polestorm/gopdfgo/internal/systemfonts"
)

// useSystemFontDirs ustawia udawane katalogi systemowe (względem testdata/fonts) na czas testu.
func useSystemFontDirs(t *testing.T, dirs ...string) {
	t.Helper()
	var absolute []string
	for _, dir := range dirs {
		path, err := filepath.Abs(filepath.Join("..", "testdata", "fonts", dir))
		if err != nil {
			t.Fatal(err)
		}
		absolute = append(absolute, path)
	}
	t.Cleanup(systemfonts.SetDirectoriesForTest(absolute))
}

func readTestFont(t *testing.T, name string) htmlpdf.Font {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "fonts", name))
	if err != nil {
		t.Fatal(err)
	}
	return htmlpdf.Font{Name: filepath.Base(name), Data: data}
}

var baseFontName = regexp.MustCompile(`/BaseFont /[A-Z]{6}\+([^\s/]+)`)

// embeddedFonts zwraca nazwy fontów osadzonych w PDF (/BaseFont bez przedrostka podzbioru: nazwa
// rodziny bez spacji).
func embeddedFonts(pdf []byte) []string {
	seen := map[string]bool{}
	var out []string
	for _, match := range baseFontName.FindAllSubmatch(pdf, -1) {
		if name := string(match[1]); !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func warningsText(result htmlpdf.Result) string {
	var lines []string
	for _, warning := range result.Warnings {
		lines = append(lines, warning.Message)
	}
	return strings.Join(lines, "\n")
}

func renderSystem(t *testing.T, html string, opts htmlpdf.Options) htmlpdf.Result {
	t.Helper()
	result, err := htmlpdf.Render(t.Context(), []byte(html), opts)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// Rodzina, której nie ma w Options.Fonts, pochodzi z katalogu systemowego; ostrzeżenie podaje
// rodzinę i plik, a wynik jest powtarzalny przy tym samym zestawie fontów.
func TestSystemFontsMissingFamily(t *testing.T) {
	useSystemFontDirs(t, "arimo")
	opts := htmlpdf.Options{Fonts: []htmlpdf.Font{readTestFont(t, "tinos/Tinos-Regular.ttf")},
		GenericFamilies: map[string][]string{"serif": {"Tinos"}}, SystemFonts: true}
	html := `<p style="font-family: Arimo">Zażółć gęślą jaźń</p><p>Tinos</p>`
	result := renderSystem(t, html, opts)
	if got := embeddedFonts(result.PDF); strings.Join(got, ",") != "Arimo,Tinos" {
		t.Errorf("osadzone fonty: %v", got)
	}
	warnings := warningsText(result)
	if !strings.Contains(warnings, "system font used: Arimo (") ||
		!strings.Contains(warnings, filepath.Join("testdata", "fonts", "arimo", "Arimo-Regular.ttf")+")") {
		t.Errorf("brak ostrzeżenia o Arimo z pliku Arimo-Regular.ttf; ostrzeżenia:\n%s", warnings)
	}
	if strings.Count(warnings, "system font used") != 1 {
		t.Errorf("ostrzeżenia o fontach systemowych:\n%s", warnings)
	}
	if again := renderSystem(t, html, opts); !bytes.Equal(again.PDF, result.PDF) {
		t.Error("drugie renderowanie dało inny wynik")
	}
	// pogrubienie: font Arimo Bold z katalogu systemowego, wybrany według grubości (CSS Fonts 4 § 5.2)
	bold := renderSystem(t, `<p style="font-family: Arimo; font-weight: bold; font-style: italic">Zażółć</p>`, opts)
	if warnings := warningsText(bold); !strings.Contains(warnings, "Arimo-BoldItalic.ttf)") || strings.Count(warnings, "system font used") != 1 {
		t.Errorf("pogrubiona kursywa: ostrzeżenia:\n%s", warnings)
	}
}

// Fonty wywołującego mają pierwszeństwo: przy rodzinie dostępnej w Options.Fonts wynik jest taki sam
// jak bez fontów systemowych i nie ma ostrzeżeń o nich; dotyczy też rodzin generycznych.
func TestSystemFontsCallerFirst(t *testing.T) {
	useSystemFontDirs(t, "arimo", "tinos")
	fonts := []htmlpdf.Font{readTestFont(t, "NotoSans-Regular.ttf"), readTestFont(t, "arimo/Arimo-Regular.ttf")}
	for _, html := range []string{
		`<p style="font-family: Arimo">Zażółć gęślą jaźń</p>`,
		`<p style="font-family: sans-serif">Zażółć gęślą jaźń</p>`,
		`<p>Zażółć gęślą jaźń</p>`, // serif: font domyślny wybrany z fontów wywołującego, nie Tinos z systemu
	} {
		with := renderSystem(t, html, htmlpdf.Options{Fonts: fonts, SystemFonts: true})
		without := renderSystem(t, html, htmlpdf.Options{Fonts: fonts})
		if !bytes.Equal(with.PDF, without.PDF) {
			t.Errorf("%s: fonty systemowe zmieniły wynik: %v wobec %v", html, embeddedFonts(with.PDF), embeddedFonts(without.PDF))
		}
		if warnings := warningsText(with); strings.Contains(warnings, "system font") {
			t.Errorf("%s: ostrzeżenia o fontach systemowych:\n%s", html, warnings)
		}
	}
	// rodzina wskazana w GenericFamilies wygrywa z systemową
	html := `<p style="font-family: serif">Zażółć</p>`
	opts := htmlpdf.Options{Fonts: fonts, GenericFamilies: map[string][]string{"serif": {"Noto Sans"}}}
	without := renderSystem(t, html, opts)
	opts.SystemFonts = true
	if with := renderSystem(t, html, opts); !bytes.Equal(with.PDF, without.PDF) {
		t.Errorf("GenericFamilies: fonty systemowe zmieniły wynik: %v", embeddedFonts(with.PDF))
	}
}

// Znak, którego nie ma font wywołującego, pochodzi z fontu systemowego (CSS Fonts 4 § 5.3).
func TestSystemFontsMissingCharacter(t *testing.T) {
	useSystemFontDirs(t, "arimo")
	opts := htmlpdf.Options{Fonts: []htmlpdf.Font{readTestFont(t, "csstest/csstest-ascii.ttf")}, SystemFonts: true}
	result := renderSystem(t, `<p style="font-family: 'CSSTest ASCII'">Zazolc ż</p>`, opts)
	got := embeddedFonts(result.PDF)
	if strings.Join(got, ",") != "Arimo,CSSTestASCII" {
		t.Errorf("osadzone fonty: %v", got)
	}
	if warnings := warningsText(result); !strings.Contains(warnings, "system font used: Arimo (") {
		t.Errorf("ostrzeżenia:\n%s", warnings)
	}
	// bez fontów systemowych znak zostaje bez glifu, a Arimo nie jest osadzany
	opts.SystemFonts = false
	if got := embeddedFonts(renderSystem(t, `<p style="font-family: 'CSSTest ASCII'">Zazolc ż</p>`, opts).PDF); len(got) != 1 {
		t.Errorf("osadzone fonty bez fontów systemowych: %v", got)
	}
}

// Bez fontów wywołującego wszystkie fonty pochodzą z systemu. Rodzinę generyczną rozstrzyga
// GenericFamilies (może wskazywać rodziny systemowe), a bez wpisu tablica podstawień go-text, jak dla
// fontów wywołującego (sans-serif daje Arimo; serif nigdy nie daje Tinos, ADR 0005). Bez fontów
// w katalogach systemowych Render zwraca ErrNoFonts.
func TestSystemFontsOnly(t *testing.T) {
	useSystemFontDirs(t, "arimo", "tinos")
	html := `<p>Zażółć</p><p style="font-family: sans-serif">Zażółć</p>`
	result := renderSystem(t, html, htmlpdf.Options{SystemFonts: true, GenericFamilies: map[string][]string{"serif": {"Tinos"}}})
	if got := embeddedFonts(result.PDF); strings.Join(got, ",") != "Arimo,Tinos" {
		t.Errorf("osadzone fonty: %v", got)
	}
	if warnings := warningsText(result); strings.Count(warnings, "system font used") != 2 {
		t.Errorf("ostrzeżenia:\n%s", warnings)
	}
	result = renderSystem(t, `<p style="font-family: sans-serif">Zażółć</p>`, htmlpdf.Options{SystemFonts: true})
	if got := embeddedFonts(result.PDF); strings.Join(got, ",") != "Arimo" {
		t.Errorf("osadzone fonty dla sans-serif: %v", got)
	}

	useSystemFontDirs(t, "nonexistent")
	if _, err := htmlpdf.Render(t.Context(), []byte(`<p>a</p>`), htmlpdf.Options{SystemFonts: true}); !errors.Is(err, htmlpdf.ErrNoFonts) {
		t.Errorf("błąd: %v, oczekiwany ErrNoFonts", err)
	}
}

// Font, którego licencja zabrania osadzania (OS/2 fsType 0x0002), jest pomijany z ostrzeżeniem;
// tekst dostaje font wywołującego.
func TestSystemFontsRestrictedEmbedding(t *testing.T) {
	useSystemFontDirs(t, filepath.Join("system", "restricted"))
	opts := htmlpdf.Options{Fonts: []htmlpdf.Font{readTestFont(t, "tinos/Tinos-Regular.ttf")}, SystemFonts: true}
	result := renderSystem(t, `<p style="font-family: 'Noto Sans Mono'">Zażółć</p>`, opts)
	if got := embeddedFonts(result.PDF); strings.Join(got, ",") != "Tinos" {
		t.Errorf("osadzone fonty: %v", got)
	}
	warnings := warningsText(result)
	if !strings.Contains(warnings, "system font skipped: Noto Sans Mono (") ||
		!strings.Contains(warnings, "NotoSansMono-RestrictedEmbedding.ttf") ||
		!strings.Contains(warnings, "fsType 0x0002") {
		t.Errorf("brak ostrzeżenia o pominiętym foncie; ostrzeżenia:\n%s", warnings)
	}
	if strings.Contains(warnings, "system font used") {
		t.Errorf("font z zakazem osadzania użyty:\n%s", warnings)
	}
}

// Fonty systemowe o konturach CFF (OTF, np. Hiragino na macOS) i CFF2 (fonty zmienne OTF) są osadzane
// jako podzbiory CIDFontType0C, tak samo jak te same pliki podane w Options.Fonts (ADR 0040).
func TestSystemFontsCFF(t *testing.T) {
	useSystemFontDirs(t, filepath.Join("cff", "adobe"))
	opts := htmlpdf.Options{Fonts: []htmlpdf.Font{readTestFont(t, "tinos/Tinos-Regular.ttf")}, SystemFonts: true}
	html := `<p style="font-family: 'Source Sans 3 VF'; font-style: italic">Zażółć gęślą jaźń</p>`
	result := renderSystem(t, html, opts)
	if got := embeddedFonts(result.PDF); strings.Join(got, ",") != "SourceSans3VF" {
		t.Errorf("osadzone fonty: %v", got)
	}
	if !bytes.Contains(result.PDF, []byte("/Subtype /CIDFontType0C")) || len(result.PDF) > 15000 {
		t.Errorf("oczekiwany podzbiór CIDFontType0C, PDF ma %d B", len(result.PDF))
	}
	if !strings.Contains(warningsText(result), "system font used: SourceSans3VF (") {
		t.Errorf("ostrzeżenia:\n%s", warningsText(result))
	}
}
