package gopdfgo

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
)

// hyphenatedLines renderuje słowo w wąskiej kolumnie z hyphens: auto i zwraca wiersze tekstu.
func hyphenatedLines(t *testing.T, lang, word string, dicts map[string][]byte) []string {
	t.Helper()
	html := `<style>@page{margin:0;size:400px 400px}body{margin:0;font-size:16px}p{width:60px;hyphens:auto}</style>` +
		`<p lang="` + lang + `">` + word + `</p>`
	result, err := Render(context.Background(), []byte(html), Options{Fonts: testFonts(t), Hyphenation: dicts})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("pdftotext", "-layout", "-", "-")
	cmd.Stdin = bytes.NewReader(result.PDF)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(strings.Trim(l, "\f")); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// Dywiz wstawiany przy dzieleniu to U+2010 (hyphenate-character: auto).
//
// Wbudowany słownik polski z hyph-utf8 (MIT, ADR 0024) dzieli wyrazy; szwedzkiego nie ma w binarce
// (LPPL), więc dzieli dopiero słownik podany przez wywołującego.
func TestHyphenationDictionaries(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("brak pdftotext")
	}
	if lines := hyphenatedLines(t, "pl", "przedsiębiorstwo", nil); len(lines) < 2 || !strings.HasSuffix(lines[0], "‐") {
		t.Errorf("polski: %q, oczekiwane dzielenie z dywizem", lines)
	}
	if lines := hyphenatedLines(t, "sv", "abababababab", nil); len(lines) != 1 {
		t.Errorf("szwedzki bez słownika: %q, oczekiwany jeden wiersz", lines)
	}
	dicts := map[string][]byte{"sv": []byte("% wzorzec testowy\n1ba\n")}
	if lines := hyphenatedLines(t, "sv", "abababababab", dicts); len(lines) < 2 || !strings.HasSuffix(lines[0], "‐") {
		t.Errorf("szwedzki ze słownikiem: %q, oczekiwane dzielenie z dywizem", lines)
	}
	// słownik obowiązuje tylko w wywołaniu, które go podało
	if lines := hyphenatedLines(t, "sv", "abababababab", nil); len(lines) != 1 {
		t.Errorf("szwedzki po wywołaniu ze słownikiem: %q, oczekiwany jeden wiersz", lines)
	}
}
