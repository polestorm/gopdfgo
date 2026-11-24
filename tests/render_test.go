package tests

import (
	"context"
	"errors"
	"github.com/polestorm/gopdfgo/htmlpdf"
	"os"
	"strings"
	"testing"
)

func testFonts(t testing.TB) []htmlpdf.Font {
	t.Helper()
	data, err := os.ReadFile("../testdata/fonts/NotoSans-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	return []htmlpdf.Font{{Name: "NotoSans-Regular.ttf", Data: data}}
}

func TestRenderRequiresFonts(t *testing.T) {
	_, err := htmlpdf.Render(context.Background(), []byte("<p>tekst</p>"), htmlpdf.Options{})
	if !errors.Is(err, htmlpdf.ErrNoFonts) {
		t.Fatalf("błąd: %v, oczekiwany ErrNoFonts", err)
	}
}

func TestRenderReportsWarnings(t *testing.T) {
	html := `<p style="frobnicate: 1px; clip-path: path('M 0 0 L 10 10')">tekst</p>`
	result, err := htmlpdf.Render(context.Background(), []byte(html), htmlpdf.Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Pages != 1 || len(result.PDF) == 0 || result.EngineName != htmlpdf.EngineName {
		t.Errorf("wynik: %d stron, %d B, silnik %q", result.Pages, len(result.PDF), result.EngineName)
	}
	var joined []string
	for _, warning := range result.Warnings {
		joined = append(joined, warning.Message)
	}
	all := strings.Join(joined, "\n")
	for _, want := range []string{"frobnicate", "clip-path"} {
		if !strings.Contains(all, want) {
			t.Errorf("brak ostrzeżenia o %q; ostrzeżenia:\n%s", want, all)
		}
	}
}

func TestRenderDeniesResourcesByDefault(t *testing.T) {
	html := `<link rel="stylesheet" href="https://example.org/style.css"><img src="file:///etc/hosts"><p>tekst</p>`
	result, err := htmlpdf.Render(context.Background(), []byte(html), htmlpdf.Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) == 0 {
		t.Error("odmowa zasobu nie została zgłoszona jako ostrzeżenie")
	}
}

func TestRenderHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := htmlpdf.Render(ctx, []byte("<p>tekst</p>"), htmlpdf.Options{Fonts: testFonts(t)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("błąd: %v, oczekiwany context.Canceled", err)
	}
}
