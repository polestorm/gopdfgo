package tests

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
)

// TestFontFaceWeightAuto sprawdza dobór twarzy z rodziny @font-face, w której reguła zwykłej twarzy nie ma
// deskryptora font-weight. Wartość początkowa deskryptora to auto, czyli grubość samego fontu (CSS Fonts 4
// § 4.5). Przed poprawką taka twarz miała grubość 0, więc zwykły tekst (400) dostawał twarz bold (700 jest
// bliżej 400 niż 0) i ilustrowana książka z testdata/books drukowała się w całości pogrubionym fontem.
func TestFontFaceWeightAuto(t *testing.T) {
	files := map[string]string{
		"Regular.ttf": "../testdata/fonts/tinos/Tinos-Regular.ttf",
		"Bold.ttf":    "../testdata/fonts/tinos/Tinos-Bold.ttf",
		"Italic.ttf":  "../testdata/fonts/tinos/Tinos-Italic.ttf",
	}
	// wzorzec: ta sama rodzina Tinos podana jako fonty dokumentu (dopasowanie z tabel OS/2 fontów)
	var fonts []htmlpdf.Font
	data := map[string][]byte{}
	for name, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Skip("brak fontu Tinos")
		}
		data[name] = content
		fonts = append(fonts, htmlpdf.Font{Name: "Tinos-" + name, Data: content})
	}
	resolver := func(url string) (htmlpdf.Resource, error) {
		for name, content := range data {
			if strings.HasSuffix(url, "/"+name) {
				return htmlpdf.Resource{Data: content, MediaType: "font/ttf"}, nil
			}
		}
		return htmlpdf.DenyAll(url)
	}
	head := `<style>@page { size: 300px 120px; margin: 0 } body { margin: 0; font: 24px/30px serif }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := htmlpdf.Render(context.Background(), []byte(head+body),
			htmlpdf.Options{Fonts: fonts, Resolver: resolver, BaseURL: "https://example.test/doc.html"})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	same := func(name, test, reference string) {
		t.Helper()
		if n := differentPixels(page(name, test), page(name+"-ref", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	text := `<p style="font-family: X">the men and women</p><p style="font-family: X"><b>the men</b></p>`
	reference := `<p style="font-family: Tinos">the men and women</p><p style="font-family: Tinos"><b>the men</b></p>`

	// zwykła twarz bez font-weight, bold z font-weight: bold; kolejność reguł bez znaczenia
	same("regular-then-bold", `<style>@font-face { font-family: X; src: url(Regular.ttf) }
		@font-face { font-family: X; font-weight: bold; src: url(Bold.ttf) }</style>`+text, reference)
	same("bold-then-regular", `<style>@font-face { font-family: X; font-weight: bold; src: url(Bold.ttf) }
		@font-face { font-family: X; src: url(Regular.ttf) }</style>`+text, reference)
	// kursywa bez font-weight obok zwykłej twarzy bez deskryptorów i bold: zwykły tekst w kursywie
	// bierze twarz Italic, a nie syntezę z bold
	same("italic-auto-weight", `<style>@font-face { font-family: X; src: url(Regular.ttf) }
		@font-face { font-family: X; font-weight: bold; src: url(Bold.ttf) }
		@font-face { font-family: X; font-style: italic; src: url(Italic.ttf) }</style>
		<p style="font-family: X"><i>the men and women</i></p>`,
		`<p style="font-family: Tinos"><i>the men and women</i></p>`)
}
