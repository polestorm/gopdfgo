package tests

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo"
)

// Fonty o konturach CFF (OpenType .otf) jako podzbiory CIDFontType0C (ADR 0040).

func cffFont(t *testing.T, path string) gopdfgo.Font {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "fonts", "cff", path))
	if err != nil {
		t.Fatal(err)
	}
	return gopdfgo.Font{Name: filepath.Base(path), Data: data}
}

const cffDocument = `<!DOCTYPE html><html lang="pl"><style>body{font-family:"Noto Sans";font-size:14pt}
.v{font-family:"Source Sans 3";font-style:italic}</style>
<p>Zażółć gęślą jaźń. The quick brown fox jumps over the lazy dog 0123456789.</p>
<p>Ελληνικά, кириллица — „cudzysłów” € ½</p>
<p class=v>Variable italic: Zażółć gęślą jaźń 0123456789</p>`

// pdffontsReport zwraca wynik pdffonts (z komunikatami poppler) albo pomija test bez narzędzia
func pdffontsReport(t *testing.T, pdf []byte) string {
	t.Helper()
	if _, err := exec.LookPath("pdffonts"); err != nil {
		t.Skip("brak pdffonts")
	}
	file := filepath.Join(t.TempDir(), "fonts.pdf")
	if err := os.WriteFile(file, pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("pdffonts", file).CombinedOutput()
	if err != nil {
		t.Fatalf("pdffonts: %v: %s", err, out)
	}
	return string(out)
}

// Font CFF (Noto Sans .otf), CFF CID-keyed (ta sama czcionka przepisana na CID, dwa słowniki FD) i CFF2
// (Source Sans 3 VF): typ CID Type 0C, podzbiór, ToUnicode, bez ostrzeżeń poppler, mały plik.
func TestCFFSubsetEmbedding(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fonts []gopdfgo.Font
		want  []string // fragmenty nazw fontów w pdffonts
	}{
		{"cff", []gopdfgo.Font{cffFont(t, "noto/NotoSans-Regular.otf"), cffFont(t, "adobe/SourceSans3VF-Italic.otf")},
			[]string{"+NotoSans ", "+SourceSans3VF "}},
		{"cid", []gopdfgo.Font{cffFont(t, "noto/NotoSansCID-Regular.otf")}, []string{"+NotoSans "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := gopdfgo.Render(context.Background(), []byte(cffDocument), gopdfgo.Options{Fonts: tc.fonts})
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range result.Warnings {
				if strings.Contains(w.Message, "CFF") {
					t.Errorf("ostrzeżenie: %s", w.Message)
				}
			}
			if len(result.PDF) > 20000 {
				t.Errorf("PDF ma %d B, oczekiwany podzbiór fontów (< 20 kB)", len(result.PDF))
			}
			if !bytes.Contains(result.PDF, []byte("/Subtype /CIDFontType0C")) ||
				!bytes.Contains(result.PDF, []byte("/FontFile3 ")) ||
				bytes.Contains(result.PDF, []byte("/CIDFontType2")) {
				t.Error("oczekiwany /FontFile3 /CIDFontType0C i potomek CIDFontType0")
			}
			report := pdffontsReport(t, result.PDF)
			if strings.Contains(report, "Mismatch") || strings.Contains(report, "Error") {
				t.Errorf("pdffonts zgłasza problem:\n%s", report)
			}
			lines := 0
			for _, line := range strings.Split(report, "\n") {
				if !strings.Contains(line, "Identity-H") {
					continue
				}
				lines++
				if !regexp.MustCompile(`CID Type 0C\s+Identity-H\s+yes yes yes`).MatchString(line) {
					t.Errorf("font: %q, oczekiwany CID Type 0C, emb yes sub yes uni yes", line)
				}
			}
			if lines != len(tc.want) {
				t.Errorf("pdffonts wypisuje %d fontów, oczekiwane %d:\n%s", lines, len(tc.want), report)
			}
			for _, w := range tc.want {
				if !strings.Contains(report, w) {
					t.Errorf("brak fontu %q:\n%s", w, report)
				}
			}
			text := strings.Join(textLines(t, result.PDF), " ")
			for _, want := range []string{"Zażółć gęślą jaźń. The quick brown fox", "Ελληνικά, кириллица — „cudzysłów” € ½"} {
				if !strings.Contains(text, want) {
					t.Errorf("pdftotext: brak %q w %q", want, text)
				}
			}
			if tc.name == "cff" && !strings.Contains(text, "Variable italic: Zażółć gęślą jaźń 0123456789") {
				t.Errorf("pdftotext: brak tekstu fontu CFF2 w %q", text)
			}
		})
	}
}

// Ten sam tekst fontem CFF i fontem CID-keyed o tych samych konturach daje identyczne rastry, a wynik nie
// zależy od przebiegu.
func TestCFFSubsetRasters(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("brak pdftoppm")
	}
	doc := []byte(`<!DOCTYPE html><style>body{font-family:"Noto Sans";font-size:24pt}</style>
<p>Zażółć gęślą jaźń. Agfx 0123456789 ffi — Ελληνικά кириллица</p>`)
	render := func(font gopdfgo.Font) []byte {
		result, err := gopdfgo.Render(context.Background(), doc, gopdfgo.Options{Fonts: []gopdfgo.Font{font}})
		if err != nil {
			t.Fatal(err)
		}
		return result.PDF
	}
	plain, again := render(cffFont(t, "noto/NotoSans-Regular.otf")), render(cffFont(t, "noto/NotoSans-Regular.otf"))
	if !bytes.Equal(plain, again) {
		t.Error("dwa przebiegi dały różne bajty")
	}
	cid := render(cffFont(t, "noto/NotoSansCID-Regular.otf"))
	dir := t.TempDir()
	a, b := rasterize(t, plain, dir, "cff"), rasterize(t, cid, dir, "cid")
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("stron: %d i %d", len(a), len(b))
	}
	if n := differentPixels(a[0], b[0]); n != 0 {
		t.Errorf("CFF i CID-keyed różnią się o %d pikseli", n)
	}
}

// PDF/A-2b i 2a z fontami CFF, CFF2 i CID-keyed przyjmowane przez veraPDF (szerokości /W zgodne z programem fontu,
// ISO 19005-2 § 6.2.11.5).
func TestCFFSubsetPDFA(t *testing.T) {
	if _, err := exec.LookPath("verapdf"); err != nil {
		t.Skip("brak verapdf")
	}
	for _, tc := range []struct {
		conformance gopdfgo.Conformance
		flavour     string
		fonts       []gopdfgo.Font
	}{
		{gopdfgo.PDFA2b, "2b", []gopdfgo.Font{cffFont(t, "noto/NotoSans-Regular.otf"), cffFont(t, "adobe/SourceSans3VF-Italic.otf")}},
		{gopdfgo.PDFA2a, "2a", []gopdfgo.Font{cffFont(t, "noto/NotoSans-Regular.otf"), cffFont(t, "adobe/SourceSans3VF-Italic.otf")}},
		{gopdfgo.PDFA2b, "2b", []gopdfgo.Font{cffFont(t, "noto/NotoSansCID-Regular.otf")}},
	} {
		result, err := gopdfgo.Render(context.Background(), []byte(cffDocument),
			gopdfgo.Options{Fonts: tc.fonts, Conformance: tc.conformance})
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(t.TempDir(), "cff.pdf")
		if err := os.WriteFile(file, result.PDF, 0o644); err != nil {
			t.Fatal(err)
		}
		report, _ := exec.Command("verapdf", "-f", tc.flavour, "--format", "text", file).CombinedOutput()
		if !strings.Contains(string(report), "\nPASS ") && !strings.HasPrefix(string(report), "PASS ") {
			t.Errorf("veraPDF odrzuca plik jako PDF/A-%s:\n%s", tc.flavour, report)
		}
	}
}
