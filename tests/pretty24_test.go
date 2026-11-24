package tests

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
)

// text-wrap-style: pretty (ADR 0054): w języku polskim, czeskim i słowackim jednoliterowy spójnik albo
// przyimek nie zostaje na końcu wiersza (sierotka), przechodzi do następnego razem z wyrazem za nim.
// Domyślnie (auto) łamiemy jak UAX #14 i Chromium.

func prettyLines(t *testing.T, lang, width, body string) []string {
	t.Helper()
	html := fmt.Sprintf(`<html lang="%s"><p style="width: %s; font-family: 'Noto Sans'; text-wrap: pretty">%s</p>`, lang, width, body)
	result, err := htmlpdf.Render(context.Background(), []byte(html), htmlpdf.Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	return pdfLines(t, result.PDF)
}

// pageTextsRaw zwraca tekst każdej strony z pdftotext, z podziałem na wiersze.
func pageTextsRaw(t *testing.T, data []byte) []string {
	t.Helper()
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("brak pdftotext")
	}
	file := filepath.Join(t.TempDir(), "out.pdf")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := exec.Command("pdftotext", file, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(text), "\f")
}

// pdfLines zwraca niepuste wiersze tekstu PDF (pdftotext bez -layout).
func pdfLines(t *testing.T, pdf []byte) []string {
	t.Helper()
	var out []string
	for _, page := range pageTextsRaw(t, pdf) {
		for _, line := range strings.Split(page, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				out = append(out, line)
			}
		}
	}
	return out
}

func TestPrettyKeepsSingleLetterWordsWithNext(t *testing.T) {
	cases := []struct{ name, lang, body, glued string }{
		{"pl", "pl", "przechowuje się w archiwum przez lata i z tego powodu", "w archiwum"},
		{"pl-tag-after", "pl", "przechowuje się w <i>archiwum</i> przez lata", "w archiwum"},
		{"pl-bracket", "pl-PL", "tekst sprawdzony oraz (w nawiasie) inny tekst", "(w nawiasie)"},
		{"cs", "cs", "dokumenty jsou uloženy v archivu a k dispozici", "v archivu"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found := false
			for _, width := range []string{"110px", "130px", "150px", "170px", "190px"} {
				lines := prettyLines(t, c.lang, width, c.body)
				for _, line := range lines {
					if strings.Contains(line, c.glued) {
						found = true
					}
					if last := line[strings.LastIndex(line, " ")+1:]; len([]rune(last)) == 1 && strings.ContainsAny(last, "aiouwzkvsAIOUWZKVS") {
						t.Errorf("szerokość %s: wiersz kończy się jednoliterowym wyrazem: %q", width, line)
					}
				}
				if got, want := strings.Join(strings.Fields(strings.Join(lines, " ")), " "), regexp.MustCompile(`<[^>]+>`).ReplaceAllString(c.body, ""); got != want {
					t.Errorf("szerokość %s: tekst %q, oczekiwany %q", width, got, want)
				}
			}
			if !found {
				t.Errorf("żadna szerokość nie dała wiersza z %q", c.glued)
			}
		})
	}
}

// Bez pretty i w innych językach łamiemy po każdej spacji, jak UAX #14 i Chromium.
func TestSingleLetterWordsBreakByDefault(t *testing.T) {
	html := `<html lang="pl"><p style="width: 150px; font-family: 'Noto Sans'">przechowuje się w archiwum przez lata</p>`
	result, err := htmlpdf.Render(context.Background(), []byte(html), htmlpdf.Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	if lines := pdfLines(t, result.PDF); !strings.HasSuffix(lines[0], "się w") {
		t.Errorf("bez pretty pierwszy wiersz %q, oczekiwany zakończony „się w” (jak w Chromium)", lines[0])
	}
	if lines := prettyLines(t, "en", "150px", "documents stored a long time in archive"); !strings.HasSuffix(lines[0], " a") {
		t.Errorf("po angielsku pretty nie zmienia łamania: %q", lines[0])
	}
}

// Dokument korpusu 22 z text-wrap: pretty: żaden wiersz nie kończy się jednoliterowym wyrazem, liczba stron
// i tekst bez zmian.
func TestPrettyCorpusMulticol(t *testing.T) {
	html, err := os.ReadFile("../testdata/documents/22-multicol-pagination.html")
	if err != nil {
		t.Fatal(err)
	}
	render := func(css string) htmlpdf.Result {
		result, err := htmlpdf.Render(context.Background(), html, htmlpdf.Options{Fonts: testFonts(t), UserCSS: css})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	plain, pretty := render(""), render("html { text-wrap: pretty }")
	if plain.Pages != pretty.Pages {
		t.Errorf("strony: %d bez pretty, %d z pretty", plain.Pages, pretty.Pages)
	}
	words := func(r htmlpdf.Result) string {
		return strings.Join(strings.Fields(strings.Join(pdfLines(t, r.PDF), " ")), " ")
	}
	if words(plain) != words(pretty) {
		t.Error("tekst dokumentu różni się z pretty")
	}
	single := regexp.MustCompile(`(^|\s)[aiouwzAIOUWZ]$`)
	n := 0
	for _, line := range pdfLines(t, pretty.PDF) {
		if single.MatchString(line) {
			n++
			t.Errorf("wiersz kończy się jednoliterowym wyrazem: %q", line)
		}
	}
	plainCount := 0
	for _, line := range pdfLines(t, plain.PDF) {
		if single.MatchString(line) {
			plainCount++
		}
	}
	if plainCount == 0 {
		t.Error("bez pretty dokument nie ma takich wierszy; test nic nie sprawdza")
	}
}
