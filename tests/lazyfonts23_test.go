package tests

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
)

// TestLazyFontsSameOutput: fonty parsowane w całości dopiero przy pierwszym użyciu (ADR 0047) dają te same
// bajty co parsowane przy rejestracji, na wszystkich dokumentach korpusu (wszystkie fonty testdata/fonts
// w Options.Fonts i reguły @font-face), także z pamięcią podręczną fontów; dokument parsuje w całości tylko
// fonty użyte (do kształtowania albo metryk), tu najwyżej jedną czwartą podanych.
func TestLazyFontsSameOutput(t *testing.T) {
	documents, _ := filepath.Glob("../testdata/documents/*.html")
	if len(documents) == 0 {
		t.Fatal("brak dokumentów w testdata/documents")
	}
	fonts, _, err := corpusFonts()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { text.LazyFontsDisabled, text.OnFontParsed = false, nil }()
	cache := gopdfgo.NewFontCache(0)
	for _, path := range documents {
		name := strings.TrimSuffix(filepath.Base(path), ".html")
		text.LazyFontsDisabled = true
		eager, err := renderFileResult(path, "../testdata", "")
		if err != nil {
			t.Fatal(err)
		}
		text.LazyFontsDisabled = false
		parsed := map[string]bool{}
		text.OnFontParsed = func(content []byte) { parsed[string(content[:min(len(content), 4096)])] = true }
		lazy, err := renderFileResult(path, "../testdata", "")
		text.OnFontParsed = nil
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(eager.PDF, lazy.PDF) || fmt.Sprint(eager.Warnings) != fmt.Sprint(lazy.Warnings) {
			t.Errorf("%s: wynik z parsowaniem fontów przy użyciu różni się od parsowania przy rejestracji", name)
		}
		if len(parsed) == 0 || 4*len(parsed) > len(fonts) {
			t.Errorf("%s: sparsowano %d plików fontów z %d", name, len(parsed), len(fonts))
		}
		cached, err := renderFileOptions(path, "../testdata", "", func(o *gopdfgo.Options) { o.FontCache = cache })
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(eager.PDF, cached.PDF) {
			t.Errorf("%s: wynik z pamięcią podręczną fontów różni się od wyniku bez niej", name)
		}
	}
}

// TestLazyFontsErrors: plik, który nie jest fontem, i font bez tablicy wymaganej (cmap) dają przy rejestracji
// ten sam błąd co przy parsowaniu w całości.
func TestLazyFontsErrors(t *testing.T) {
	data, err := os.ReadFile("../testdata/fonts/NotoSans-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	// znacznik tablicy cmap w katalogu tablic zamieniony na nieznany: font bez cmap
	noCmap := bytes.Replace(bytes.Clone(data), []byte("cmap"), []byte("xmap"), 1)
	defer func() { text.LazyFontsDisabled = false }()
	for _, font := range []gopdfgo.Font{{Name: "bad.ttf", Data: []byte("not a font file")}, {Name: "nocmap.ttf", Data: noCmap}} {
		var messages [2]string
		for i, disabled := range []bool{true, false} {
			text.LazyFontsDisabled = disabled
			_, err := gopdfgo.Render(context.Background(), []byte("<p>tekst</p>"), gopdfgo.Options{Fonts: []gopdfgo.Font{font}})
			if err == nil {
				t.Fatalf("%s: brak błędu", font.Name)
			}
			messages[i] = err.Error()
		}
		if messages[0] != messages[1] {
			t.Errorf("%s: błąd %q, przy parsowaniu w całości %q", font.Name, messages[1], messages[0])
		}
	}
}
