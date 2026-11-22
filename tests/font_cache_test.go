package tests

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo"
)

// TestFontCacheSameOutput: pamięć podręczna fontów (ADR 0045) nie zmienia wyniku. Dokumenty korpusu
// renderowane z jedną pamięcią podręczną (zimną i ciepłą, na przemian z innymi dokumentami) dają te
// same bajty co bez niej.
func TestFontCacheSameOutput(t *testing.T) {
	names := []string{"20-invoice", "16-typography-fonts", "03-report-svg-chart", "20-invoice"}
	cache := gopdfgo.NewFontCache(64 << 20)
	for _, name := range names {
		path := filepath.Join("../testdata/documents", name+".html")
		plain, err := renderFileResult(path, "../testdata", "")
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			cached, err := renderFileOptions(path, "../testdata", "", func(o *gopdfgo.Options) { o.FontCache = cache })
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(plain.PDF, cached.PDF) {
				t.Fatalf("%s: wynik z pamięcią podręczną fontów różni się od wyniku bez niej", name)
			}
		}
	}
}

// TestFontCacheSharedFile: ten sam plik fontu w dwóch regułach @font-face (różne rodziny, jedna
// z unicode-range) i w Options.Fonts; każda reguła dostaje własną twarz, więc unicode-range jednej
// nie zmienia drugiej, a wynik z ciepłą pamięcią podręczną jest taki sam jak bez niej.
func TestFontCacheSharedFile(t *testing.T) {
	data, err := os.ReadFile("../testdata/fonts/NotoSans-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	html := []byte(`<style>
@font-face { font-family: A; src: url(f.ttf); unicode-range: U+0041-005A; }
@font-face { font-family: B; src: url(f.ttf); }
.a { font-family: A, serif; } .b { font-family: B; }
</style><p class="a">ABC abc</p><p class="b">ABC abc</p>`)
	resolver := func(target string) (gopdfgo.Resource, error) {
		if strings.HasSuffix(target, "/f.ttf") {
			return gopdfgo.Resource{Data: data}, nil
		}
		return gopdfgo.DenyAll(target)
	}
	serif, err := os.ReadFile("../testdata/fonts/tinos/Tinos-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	fonts := []gopdfgo.Font{{Name: "noto.ttf", Data: data}, {Name: "tinos.ttf", Data: serif}}
	opts := gopdfgo.Options{Fonts: fonts, Resolver: resolver, BaseURL: "file:///doc/a.html",
		GenericFamilies: map[string][]string{"serif": {"Tinos"}}}
	plain, err := gopdfgo.Render(context.Background(), html, opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.FontCache = gopdfgo.NewFontCache(0)
	for range 3 {
		cached, err := gopdfgo.Render(context.Background(), html, opts)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(plain.PDF, cached.PDF) {
			t.Fatal("wynik z pamięcią podręczną fontów różni się od wyniku bez niej")
		}
	}
}

// TestFontCacheEviction: limit długości plików usuwa fonty najdawniej użyte, a font usunięty
// i wczytany ponownie działa jak wcześniej.
func TestFontCacheEviction(t *testing.T) {
	var fonts []gopdfgo.Font
	for _, name := range []string{"NotoSans-Regular.ttf", "NotoSerif-Regular.ttf", "NotoSansMono-Regular.ttf"} {
		data, err := os.ReadFile("../testdata/fonts/" + name)
		if err != nil {
			t.Fatal(err)
		}
		fonts = append(fonts, gopdfgo.Font{Name: name, Data: data})
	}
	html := []byte(`<p>tekst</p>`)
	cache := gopdfgo.NewFontCache(int64(len(fonts[0].Data)) + 1) // mieści jeden plik
	var first []byte
	for i := range 6 {
		font := fonts[i%len(fonts)]
		result, err := gopdfgo.Render(context.Background(), html, gopdfgo.Options{Fonts: []gopdfgo.Font{font}, FontCache: cache})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = result.PDF
		}
		if i == 3 && !bytes.Equal(first, result.PDF) {
			t.Fatal("font wczytany ponownie po usunięciu z pamięci podręcznej daje inny wynik")
		}
	}
}
