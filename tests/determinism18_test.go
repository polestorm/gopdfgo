package tests

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/polestorm/gopdfgo"
)

// TestSharedFamilyNameDeterministic sprawdza, że przy dwóch fontach o tej samej zlokalizowanej nazwie
// rodziny wynik nie zależy od uruchomienia i wygrywa font podany wcześniej w Options.Fonts. Przed
// poprawką fonty były rejestrowane w kolejności iteracji po mapie, więc test WPT
// css-fonts/font-family-name-010 dawał przy kolejnych uruchomieniach różne pliki PDF.
func TestSharedFamilyNameDeterministic(t *testing.T) {
	var fonts []gopdfgo.Font
	for _, name := range []string{"csstest-familyname.ttf", "csstest-familyname-funkyA.ttf", "csstest-familyname-funkyB.ttf", "csstest-familyname-funkyC.ttf"} {
		data, err := os.ReadFile("../testdata/fonts/csstest/" + name)
		if err != nil {
			t.Skip("brak fontów CSSTest")
		}
		fonts = append(fonts, gopdfgo.Font{Name: name, Data: data})
	}
	fonts = append(fonts, testFonts(t)...)
	html := []byte(`<p style="font: 36px 'ＣＳＳテスト　フォント名', CSSTest Fallback">5678</p>`)
	render := func(fonts []gopdfgo.Font) []byte {
		t.Helper()
		result, err := gopdfgo.Render(context.Background(), html, gopdfgo.Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		return result.PDF
	}
	first := render(fonts)
	for i := 0; i < 20; i++ {
		if !bytes.Equal(render(fonts), first) {
			t.Fatalf("uruchomienie %d dało inny plik PDF niż pierwsze", i+2)
		}
	}
	// wygrywa font podany wcześniej: odwrócona kolejność daje inny wynik
	reversed := append([]gopdfgo.Font{fonts[3], fonts[2], fonts[1], fonts[0]}, fonts[4:]...)
	if bytes.Equal(render(reversed), first) {
		t.Errorf("kolejność fontów w Options.Fonts nie wpływa na wybór twarzy o wspólnej nazwie rodziny")
	}
}
