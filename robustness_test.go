package gopdfgo

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// printPageCSS to domyślna strona reftestów druku WPT (docs/writing-tests/print-reftests w WPT).
const printPageCSS = "@page { size: 5in 3in; margin: 0.5in }"

// TestDeepNesting pilnuje, żeby czas układu nie rósł wykładniczo z głębokością zagnieżdżenia
// (wymaganie D3). Przed poprawką PageValues 30 poziomów trwało minuty.
func TestDeepNesting(t *testing.T) {
	for _, depth := range []int{60, 400} {
		html := "<html><body>" + strings.Repeat(`<div style="padding-left:1px;border-left:1px solid">`, depth) +
			"dno" + strings.Repeat("</div>", depth)
		file := filepath.Join(t.TempDir(), fmt.Sprintf("nested-%d.html", depth))
		if err := os.WriteFile(file, []byte(html), 0o644); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if _, err := renderFile(file, t.TempDir(), ""); err != nil {
			t.Errorf("głębokość %d: %v", depth, err)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("głębokość %d: %v, oczekiwane poniżej 2 s", depth, elapsed)
		}
	}
}

// TestNestedFlex pilnuje kosztu zagnieżdżonych kontenerów flex. Układ nadal rośnie wykładniczo
// (ok. 2 razy na poziom), ale przed ograniczeniem liczby przejść 12 poziomów trwało ponad 6 s.
func TestNestedFlex(t *testing.T) {
	html := strings.Repeat(`<div style="display:flex">`, 12) + "x" + strings.Repeat("</div>", 12)
	file := filepath.Join(t.TempDir(), "nested-flex.html")
	if err := os.WriteFile(file, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := renderFile(file, t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("12 poziomów flex: %v, oczekiwane poniżej 2 s", elapsed)
	}
}

// TestWPTNoPanic renderuje testy druku WPT i ich wzorce dwukrotnie; żaden nie może wywołać paniki
// silnika ani dać różnych bajtów. Nie porównuje wyglądu (to robi benchmark).
func TestWPTNoPanic(t *testing.T) {
	manifest, err := os.Open("testdata/wpt/manifest.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()
	reader := csv.NewReader(manifest)
	reader.Comma = '\t'
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	files := map[string]bool{}
	for _, row := range rows[1:] {
		if row[0] == "layout" {
			continue
		}
		files[row[2]] = true
		for _, reference := range strings.Split(row[4], ";") {
			if reference != "-" {
				files[reference] = true
			}
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	panics := map[string][]string{}
	var unstable []string
	for _, name := range names {
		first, err := renderFile(filepath.Join("testdata/wpt", name), "testdata/wpt", printPageCSS)
		if errors.Is(err, ErrEngine) {
			panics[panicSite(err.Error())] = append(panics[panicSite(err.Error())], name)
			continue
		}
		if errors.Is(err, context.DeadlineExceeded) {
			// silnik nie ma punktów przerwania: po przekroczeniu czasu pracuje dalej i blokuje kolejne
			// renderowania, więc dalszy przebieg nie ma sensu
			t.Fatalf("przekroczony limit czasu na %s", name)
		}
		// Y2 na szerszym materiale niż dokumenty korpusu: drugi przebieg musi dać te same bajty.
		second, _ := renderFile(filepath.Join("testdata/wpt", name), "testdata/wpt", printPageCSS)
		if !bytes.Equal(first, second) {
			unstable = append(unstable, name)
		}
	}
	if len(unstable) != 0 {
		t.Errorf("%d plików daje różne bajty w dwóch przebiegach, np. %s", len(unstable), strings.Join(unstable[:min(5, len(unstable))], ", "))
	}
	sites := make([]string, 0, len(panics))
	for site := range panics {
		sites = append(sites, site)
	}
	sort.Strings(sites)
	for _, site := range sites {
		t.Errorf("%d plików: %s\n\tnp. %s", len(panics[site]), site, panics[site][0])
	}
	t.Logf("%d plików wyrenderowanych", len(names))
}

// panicSite zwraca komunikat paniki i pierwszą ramkę stosu z kodu modułu.
func panicSite(message string) string {
	lines := strings.Split(message, "\n")
	for i, line := range lines {
		if strings.Contains(line, "/internal/") && strings.Contains(line, ".go:") && i > 8 {
			return lines[0] + " @ " + strings.TrimSpace(line[strings.Index(line, "/internal/"):])
		}
	}
	return lines[0]
}

// Trzy awarie znalezione 2026-11-16 w testach W3C z obszaru css-text; każda przerywała renderowanie
// poprawnego dokumentu, pierwsza przepełnieniem stosu, którego proces nie przeżywa.
func TestTextCrashesFromW3CSurvey(t *testing.T) {
	documents := map[string]string{
		// jednostka ch w tab-size: obliczenie wartości pytało o nią samą
		"tab-size-ch": `<div style="white-space:pre;tab-size:0.4ch">a&#9;b</div>`,
		// justowanie tekstu od prawej do lewej: numer skupienia glifu poza tekstem wiersza
		"justify-rtl": `<p dir="rtl" style="text-align:justify;text-align-last:center;width:200px">` +
			strings.Repeat("אמר המחבר החיבור הזה לא חברתיו ללמד לבני האדם ", 4) + `ממנה&nbsp;מתעלם</p>`,
		// znak podziału wiersza spoza listy znanej silnikowi (U+000B, U+000C)
		"line-break-controls": "<div style=\"white-space:pre\">a\u000bb\u000cc</div><div>d\u000be f\u000cg</div>",
	}
	for name, html := range documents {
		t.Run(name, func(t *testing.T) {
			result, err := Render(context.Background(), []byte(html), Options{Fonts: testFonts(t)})
			if err != nil {
				t.Fatalf("renderowanie nie powiodło się: %v", err)
			}
			if result.Pages != 1 {
				t.Errorf("stron: %d, oczekiwana 1", result.Pages)
			}
		})
	}
}
