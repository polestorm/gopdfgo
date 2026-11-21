package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/polestorm/gopdfgo"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// knownHangs to dokumenty, na których silnik się zawiesza (docs/benchmark.md); pomijane do czasu poprawki.
var knownHangs = map[string]string{}

// knownNondeterminism wyłącza błąd przy różnych bajtach z dwóch przebiegów. Przyczyny (iteracje po mapach
// w zapisie PDF i w kolejności rysowania) są poprawione; flaga zostaje na wypadek regresji w trakcie prac.
const knownNondeterminism = false

// knownPageCounts to dokumenty, w których liczba stron różni się od Chromium, z powodem.
var knownPageCounts = map[string]string{
	"11-edge-cases": "blok z break-inside: avoid wyższy niż strona przechodzi na nową stronę i tam jest dzielony (ADR 0004); Chromium zostawia go za nagłówkiem",
}

type expectation struct {
	Pages    *int `json:"pages"`
	Chromium struct {
		Pages int `json:"pages"`
	} `json:"chromium"`
}

var pageObject = regexp.MustCompile(`/Type\s*/Page[^s]`)

// renderDocument renderuje plik HTML z korpusu. Fonty wyłącznie z testdata/fonts,
// zasoby wyłącznie spod testdata/.
func renderDocument(t *testing.T, path string) []byte {
	t.Helper()
	out, err := renderFile(path, "../testdata", "")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// renderFile renderuje plik HTML przez interfejs publiczny; zasoby wolno czytać tylko spod resourceRoot,
// a ścieżki bezwzględne (jak /fonts/ahem.css w testach WPT) liczą się od tego katalogu.
func renderFile(path, resourceRoot, userCSS string) ([]byte, error) {
	result, err := renderFileResult(path, resourceRoot, userCSS)
	return result.PDF, err
}

func renderFileResult(path, resourceRoot, userCSS string) (gopdfgo.Result, error) {
	return renderFileOptions(path, resourceRoot, userCSS, nil)
}

// renderFileOptions działa jak renderFileResult, a adjust (gdy nie nil) zmienia opcje przed renderowaniem.
func renderFileOptions(path, resourceRoot, userCSS string, adjust func(*gopdfgo.Options)) (gopdfgo.Result, error) {
	root, err := filepath.Abs(resourceRoot)
	if err != nil {
		return gopdfgo.Result{}, err
	}
	resolver := func(target string) (gopdfgo.Resource, error) {
		u, err := url.Parse(target)
		if err != nil || u.Scheme != "file" {
			return gopdfgo.DenyAll(target)
		}
		file := filepath.Clean(u.Path)
		if !strings.HasPrefix(file, root+string(filepath.Separator)) {
			file = filepath.Join(root, file)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return gopdfgo.DenyAll(target)
		}
		return gopdfgo.Resource{Data: data}, nil
	}

	fonts, families, err := corpusFonts()
	if err != nil {
		return gopdfgo.Result{}, err
	}

	html, err := os.ReadFile(path)
	if err != nil {
		return gopdfgo.Result{}, err
	}
	absolute, _ := filepath.Abs(path)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opts := gopdfgo.Options{Fonts: fonts, Resolver: resolver, BaseURL: "file://" + absolute, UserCSS: userCSS,
		GenericFamilies: families}
	if adjust != nil {
		adjust(&opts)
	}
	return gopdfgo.Render(ctx, html, opts)
}

// corpusFonts wczytuje wszystkie fonty z testdata/fonts i jego podkatalogów oraz przypisanie fontów
// do rodzin generycznych z testdata/fonts/families.json (ADR 0005). Nazwy rodzin są tylko w tym pliku.
func corpusFonts() ([]gopdfgo.Font, map[string][]string, error) {
	var fonts []gopdfgo.Font
	for _, pattern := range []string{"*.ttf", "*/*.ttf"} {
		files, _ := filepath.Glob(filepath.Join("..", "testdata", "fonts", pattern))
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				return nil, nil, err
			}
			fonts = append(fonts, gopdfgo.Font{Name: filepath.Base(file), Data: data})
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "testdata", "fonts", "families.json"))
	if err != nil {
		return nil, nil, err
	}
	families := map[string][]string{}
	if err := json.Unmarshal(data, &families); err != nil {
		return nil, nil, fmt.Errorf("../testdata/fonts/families.json: %w", err)
	}
	return fonts, families, nil
}

func TestCorpusDocuments(t *testing.T) {
	documents, _ := filepath.Glob("../testdata/documents/*.html")
	if len(documents) == 0 {
		t.Fatal("brak dokumentów w testdata/documents")
	}
	_, pdftotextErr := exec.LookPath("pdftotext")

	for _, path := range documents {
		name := strings.TrimSuffix(filepath.Base(path), ".html")
		t.Run(name, func(t *testing.T) {
			if reason, ok := knownHangs[name]; ok {
				t.Skip("znane zawieszenie: " + reason)
			}
			var expect expectation
			raw, err := os.ReadFile(strings.TrimSuffix(path, ".html") + ".expect.json")
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &expect); err != nil {
				t.Fatal(err)
			}

			start := time.Now()
			first := renderDocument(t, path)
			elapsed := time.Since(start)
			second := renderDocument(t, path)

			// Y2: te same wejścia dają te same bajty.
			if !bytes.Equal(first, second) {
				if knownNondeterminism {
					t.Logf("znany błąd: dwa przebiegi dały różne bajty (%d i %d)", len(first), len(second))
				} else {
					// oba pliki zostają w katalogu tymczasowym do porównania
					dir, _ := os.MkdirTemp("", "gopdfgo-"+name+"-")
					os.WriteFile(filepath.Join(dir, "first.pdf"), first, 0o644)
					os.WriteFile(filepath.Join(dir, "second.pdf"), second, 0o644)
					t.Errorf("dwa przebiegi dały różne bajty (%d i %d); pliki w %s", len(first), len(second), dir)
				}
			}

			// Liczba stron: z konstrukcji dokumentu, a gdy nie jest określona, z Chromium jako punkt odniesienia.
			want, source := expect.Chromium.Pages, "Chromium"
			if expect.Pages != nil {
				want, source = *expect.Pages, "specyfikacja"
			}
			if got := len(pageObject.FindAll(first, -1)); got != want {
				if reason, ok := knownPageCounts[name]; ok {
					t.Logf("strony: %d, oczekiwane %d (%s); znany błąd: %s", got, want, source, reason)
				} else {
					t.Errorf("strony: %d, oczekiwane %d (%s)", got, want, source)
				}
			}

			// Y1: tekst daje się wyodrębnić jako słowa, nie pojedyncze litery.
			if pdftotextErr == nil {
				file := filepath.Join(t.TempDir(), name+".pdf")
				if err := os.WriteFile(file, first, 0o644); err != nil {
					t.Fatal(err)
				}
				extracted, err := exec.Command("pdftotext", file, "-").Output()
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(extracted, []byte("Strona")) && !bytes.Contains(extracted, []byte("strona")) &&
					!bytes.Contains(extracted, []byte("Rejestr")) && !bytes.Contains(extracted, []byte("Procedura")) &&
					!bytes.Contains(extracted, []byte("Raport")) && !bytes.Contains(extracted, []byte("pionowa")) {
					t.Errorf("w wyodrębnionym tekście nie ma żadnego oczekiwanego słowa; początek: %.80q", extracted)
				}
			}
			// GOPDFGO_OUT=katalog zachowuje wyniki do obejrzenia
			if dir := os.Getenv("GOPDFGO_OUT"); dir != "" {
				if err := os.WriteFile(filepath.Join(dir, name+".pdf"), first, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("%d B, %d ms", len(first), elapsed.Milliseconds())
		})
	}
}
