package tests

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
)

// target-counter() w ::after elementu, który jest elementem flex albo grid (spis treści z odnośnikami
// display: flex i kropkowaną linią do numeru): pseudoelement liniowy staje się blokiem (blockify, CSS
// Display 3 § 2.7), a numer strony z kolejnego obiegu paginacji musi trafić do tego bloku. Od 2026-11-15
// do 2026-11-24 trafiał do zastąpionego pudełka liniowego i spis pokazywał „0” (dokument korpusu 18).
func TestTargetCounterInFlexAndGridItems(t *testing.T) {
	for _, display := range []string{"flex", "grid", "inline-flex", "block", "inline-block"} {
		t.Run(display, func(t *testing.T) {
			html := fmt.Sprintf(`<style>
.toc a { display: %s; }
.toc a::after { content: " p" target-counter(attr(href), page); }
h2 { break-before: page; }
</style>
<div class="toc"><a href="#c1">One</a> <a href="#c2">Two</a></div>
<h2 id="c1">C1</h2><h2 id="c2">C2</h2>`, display)
			result, err := htmlpdf.Render(context.Background(), []byte(html), htmlpdf.Options{Fonts: testFonts(t)})
			if err != nil {
				t.Fatal(err)
			}
			first := pageTexts(t, result.PDF)[0]
			if !strings.Contains(first, "p2") || !strings.Contains(first, "p3") {
				t.Errorf("strona 1: %q, oczekiwane numery p2 i p3", first)
			}
		})
	}
}

// Dokument korpusu 18: pięć pozycji spisu z numerami stron 2–6 (expect.json).
func TestCorpusTableOfContentsPageNumbers(t *testing.T) {
	html, err := os.ReadFile("../testdata/documents/18-toc-running-elements.html")
	if err != nil {
		t.Fatal(err)
	}
	result, err := htmlpdf.Render(context.Background(), html, htmlpdf.Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	// pdftotext bez -layout wypisuje numery spisu (flex, po prawej) osobno, po tytułach pozycji.
	first := pageTexts(t, result.PDF)[0]
	if !strings.Contains(first, "5. Wnioski") || !strings.Contains(first, "2 3 4 5 6") {
		t.Errorf("spis treści bez numerów stron 2–6: %q", first)
	}
}
