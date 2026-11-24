package tests

import (
	"bytes"
	"context"
	"regexp"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
)

// Każda strona ma słownik zasobów, także pusta (ISO 32000-1 § 7.7.3.3, tabela 30: /Resources jest
// wymagany, dziedziczony z drzewa stron). Strona bez tekstu i obrazów dostaje pusty słownik.
func TestEveryPageHasResources(t *testing.T) {
	html := `<!DOCTYPE html><p>a</p><div style="break-before: page; height: 10px"></div>`
	result, err := htmlpdf.Render(context.Background(), []byte(html), htmlpdf.Options{Fonts: testFonts(t), Uncompressed: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Pages != 2 {
		t.Fatalf("%d stron, oczekiwano 2", result.Pages)
	}
	pages := regexp.MustCompile(`/Type\s*/Page\b`).FindAllIndex(result.PDF, -1)
	if len(pages) != 2 {
		t.Fatalf("%d słowników stron", len(pages))
	}
	if n := bytes.Count(result.PDF, []byte("/Resources")); n < 2 {
		t.Errorf("/Resources w %d miejscach, oczekiwano na każdej z 2 stron", n)
	}
	if !bytes.Contains(result.PDF, []byte("/Resources <<>>")) {
		t.Error("pusta strona bez pustego słownika /Resources")
	}
}
