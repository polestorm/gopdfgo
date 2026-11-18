package gopdfgo

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func testFonts(t testing.TB) []Font {
	t.Helper()
	data, err := os.ReadFile("testdata/fonts/NotoSans-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	return []Font{{Name: "NotoSans-Regular.ttf", Data: data}}
}

func TestRenderRequiresFonts(t *testing.T) {
	_, err := Render(context.Background(), []byte("<p>tekst</p>"), Options{})
	if !errors.Is(err, ErrNoFonts) {
		t.Fatalf("błąd: %v, oczekiwany ErrNoFonts", err)
	}
}

func TestRenderReportsWarnings(t *testing.T) {
	html := `<p style="frobnicate: 1px; clip-path: path('M 0 0 L 10 10')">tekst</p>`
	result, err := Render(context.Background(), []byte(html), Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Pages != 1 || len(result.PDF) == 0 || result.EngineName != EngineName {
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
	result, err := Render(context.Background(), []byte(html), Options{Fonts: testFonts(t)})
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
	_, err := Render(ctx, []byte("<p>tekst</p>"), Options{Fonts: testFonts(t)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("błąd: %v, oczekiwany context.Canceled", err)
	}
}
