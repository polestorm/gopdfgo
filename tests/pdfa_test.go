package tests

import (
	"bytes"
	"context"
	"encoding/base64"
	"github.com/polestorm/gopdfgo"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	// pdfaDocument zbiera to, co w PDF/A jest ograniczone: przezroczystość, obraz z kanałem alfa, gradient,
	// SVG, linki wewnętrzne i zewnętrzne, zakładki, metadane ze znakami wymagającymi kodowania.
	"github.com/polestorm/gopdfgo/internal/pdfa"
)

const pdfaDocument = `<!DOCTYPE html><html lang="pl"><head><meta charset="utf-8">
<title>Zażółć &amp; gęślą &lt;jaźń&gt;</title><meta name="author" content="Jan Kowalski">
<meta name="description" content="Dokument próbny"><meta name="keywords" content="pdf, archiwum">
<style>@page { size: 300px 300px; margin: 20px } body { margin: 0; font: 12px sans-serif }
h1 { bookmark-level: 1; font-size: 16px } .t { opacity: 0.5; background: rgba(200, 30, 30, 0.4); padding: 4px }
.g { height: 20px; background: linear-gradient(to right, #06c, #fc0) }</style></head><body>
<h1 id="start">Nagłówek</h1><p class="t">Tekst półprzezroczysty z <b>pogrubieniem</b>.</p><div class="g"></div>
<p><a href="#start">link wewnętrzny</a> i <a href="https://example.com/">zewnętrzny</a></p>
<svg width="60" height="30" viewBox="0 0 60 30"><circle cx="15" cy="15" r="12" fill="#090" fill-opacity="0.6"/><rect x="30" y="5" width="25" height="20" fill="#909"/></svg>
<img alt="" width="2" height="2" src="data:image/png;base64,PNGDATA">
</body></html>`

// translucentPNG to obraz 2x2 z kanałem alfa.
func translucentPNG(t *testing.T) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	img.Set(1, 0, color.NRGBA{G: 255, A: 128})
	img.Set(0, 1, color.NRGBA{B: 255, A: 64})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestPDFA2b(t *testing.T) {
	pdfaDocument := strings.Replace(pdfaDocument, "PNGDATA", translucentPNG(t), 1)
	options := gopdfgo.Options{Fonts: testFonts(t), Conformance: gopdfgo.PDFA2b}
	first, err := gopdfgo.Render(context.Background(), []byte(pdfaDocument), options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := gopdfgo.Render(context.Background(), []byte(pdfaDocument), options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.PDF, second.PDF) {
		t.Error("dwa przebiegi w trybie PDF/A dały różne bajty")
	}
	for _, needle := range []string{"<pdfaid:part>2</pdfaid:part>", "<pdfaid:conformance>B</pdfaid:conformance>",
		"Zażółć &amp; gęślą &lt;jaźń&gt;", "/GTS_PDFA1", "/ID [<"} {
		if !bytes.Contains(first.PDF, []byte(needle)) {
			t.Errorf("w pliku brakuje %q", needle)
		}
	}

	// tryb PDF/A nie zmienia wyglądu stron (obraz w naturalnym rozmiarze: przy powiększaniu PDF/A zabrania
	// wygładzania, więc powiększony obraz może być rysowany ostrzej)
	plain, err := gopdfgo.Render(context.Background(), []byte(pdfaDocument), gopdfgo.Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(plain.PDF, []byte("pdfaid")) || bytes.Contains(plain.PDF, []byte("/OutputIntents")) {
		t.Error("zwykły PDF zawiera wpisy PDF/A")
	}
	dir := t.TempDir()
	archival, ordinary := rasterize(t, first.PDF, dir, "pdfa"), rasterize(t, plain.PDF, dir, "plain")
	if len(archival) != len(ordinary) {
		t.Fatalf("stron: %d w PDF/A, %d w zwykłym", len(archival), len(ordinary))
	}
	for i := range archival {
		if n := differentPixels(archival[i], ordinary[i]); n != 0 {
			t.Errorf("strona %d: PDF/A różni się od zwykłego PDF o %d pikseli", i+1, n)
		}
	}

	if _, err := gopdfgo.Render(context.Background(), []byte("<p>x</p>"), gopdfgo.Options{Fonts: testFonts(t), Conformance: "PDF/A-9z"}); err == nil {
		t.Error("nieznana zgodność nie dała błędu")
	}

	// walidator wzorcowy (veraPDF, narzędzie testowe); bez niego ta część jest pomijana
	if _, err := exec.LookPath("verapdf"); err != nil {
		t.Skip("brak verapdf: zgodność z PDF/A-2b nie została sprawdzona walidatorem")
	}
	file := filepath.Join(dir, "pdfa.pdf")
	if err := os.WriteFile(file, first.PDF, 0o644); err != nil {
		t.Fatal(err)
	}
	report, _ := exec.Command("verapdf", "-f", "2b", "--format", "text", file).CombinedOutput()
	if !strings.Contains(string(report), "\nPASS ") && !strings.HasPrefix(string(report), "PASS ") {
		t.Errorf("veraPDF odrzuca plik jako PDF/A-2b:\n%s", report)
	}
	if strings.Contains(string(report), "WARNING") {
		t.Errorf("veraPDF zgłasza ostrzeżenia:\n%s", report)
	}
}

func TestSRGBProfileIsStable(t *testing.T) {
	profile := pdfa.SRGBProfile()
	if len(profile)%4 != 0 || string(profile[36:40]) != "acsp" || string(profile[16:20]) != "RGB " {
		t.Fatalf("nagłówek profilu ICC jest błędny (długość %d)", len(profile))
	}
	if !bytes.Equal(profile, pdfa.SRGBProfile()) {
		t.Error("profil sRGB nie jest powtarzalny")
	}
}
