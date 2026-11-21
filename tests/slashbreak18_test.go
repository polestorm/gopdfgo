package tests

import (
	"strings"
	"testing"
)

// Po ukośniku między literami albo cyframi wiersz się nie łamie, gdy następna okazja do złamania jest
// blisko (UAX #14 § 5.1, klasa SY; ADR 0035): „zyskiem/stratą”, „and/or”, adres URL. Przypadek z noty
// podatkowej sprawozdań e-SF, gdzie Chromium trzyma „zyskiem/stratą” w jednym wierszu.
func TestSlashKeepsShortWordsTogether(t *testing.T) {
	for _, word := range []string{"zyskiem/stratą", "and/or", "abc/123", "http://example.com/abc/def"} {
		html := `<!DOCTYPE html><p style="width: 3em; font-size: 12px">` + word + `</p>`
		text := string(extractText(t, renderString(t, html).PDF))
		if !strings.Contains(text, word) {
			t.Errorf("%q złamane w środku: %q", word, text)
		}
	}
	// długi ciąg za ukośnikiem: okazja po ukośniku zostaje (odległość od niej do następnej okazji
	// przekracza slashBreakMinDistance)
	long := "abc/" + strings.Repeat("x", 30)
	text := string(extractText(t, renderString(t, `<!DOCTYPE html><p style="width: 3em; font-size: 12px">`+long+`</p>`).PDF))
	if strings.Contains(text, long) {
		t.Errorf("%q nie złamane po ukośniku: %q", long, text)
	}
}
