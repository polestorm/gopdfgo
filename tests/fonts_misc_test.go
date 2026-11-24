package tests

import (
	"context"
	"github.com/polestorm/gopdfgo/htmlpdf"
	"os"
	"strings"
	"testing"
)

// TestFontsMisc sprawdza poprawki z grupy testów WPT css-fonts: deskryptory unicode-range, size-adjust
// i nadpisania metryk w @font-face, właściwości font-synthesis, jednostki rch, nazwę rodziny generycznej
// w cudzysłowie, tekst o zerowym rozmiarze fontu i syntezę indeksu górnego. Font Ahem (każdy znak to
// kwadrat 1 em, wydłużenie górne 0,8 em, dolne 0,2 em) jest podawany przez Resolver pod adresem
// Ahem.ttf, więc @font-face może go wczytywać z różnymi deskryptorami.
func TestFontsMisc(t *testing.T) {
	ahem, err := os.ReadFile("../testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]htmlpdf.Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	resolver := func(url string) (htmlpdf.Resource, error) {
		if strings.HasSuffix(url, "Ahem.ttf") {
			return htmlpdf.Resource{Data: ahem, MediaType: "font/ttf"}, nil
		}
		return htmlpdf.DenyAll(url)
	}
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0; font: 20px/1 serif }
		.g { background: green } .a { position: absolute }</style>`
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
	different := func(name, test, reference string) {
		t.Helper()
		if differentPixels(page(name, test), page(name+"-ref", reference)) == 0 {
			t.Errorf("%s: wynik nie różni się od wzorca, a powinien", name)
		}
	}

	// unicode-range bez U+0020: twarz nie jest pierwszym dostępnym fontem, więc 1ch liczy się z Ahem
	// (1 em), a nie z fontu ograniczonego do litery A
	same("unicode-range-first-available", `<style>@font-face { font-family: OnlyA; src: url(Ahem.ttf);
		unicode-range: U+41 } @font-face { font-family: Square; src: url(Ahem.ttf) }</style>
		<div class=g style="font-family: OnlyA, Square; width: 1ch; height: 20px"></div>`,
		`<div class=g style="width: 20px; height: 20px"></div>`)
	// znak spoza unicode-range idzie do następnej rodziny
	same("unicode-range-fallback", `<style>@font-face { font-family: OnlyA; src: url(Ahem.ttf);
		unicode-range: U+41 }</style><div style="font-family: OnlyA, Tinos">AxA</div>`,
		`<div style="font-family: Tinos"><span style="font-family: Ahem">A</span>x<span style="font-family: Ahem">A</span></div>`)
	// size-adjust: 200% podwaja rozmiar glifów i metryk twarzy
	same("size-adjust", `<style>@font-face { font-family: Big; src: url(Ahem.ttf); size-adjust: 200% }</style>
		<div style="font: 10px/normal Big; color: green">X</div>`,
		`<div class=g style="width: 20px; height: 20px"></div>`)
	// ascent-override i descent-override wyznaczają wysokość wiersza przy line-height: normal
	same("metrics-override", `<style>@font-face { font-family: Tall; src: url(Ahem.ttf);
		ascent-override: 100%; descent-override: 50%; line-gap-override: 0% }</style>
		<div class=g style="font: 20px/normal Tall; color: transparent">X</div>`,
		`<div class=g style="height: 30px"></div>`)
	// font-synthesis i jego części są znane (@supports)
	same("font-synthesis-supports", `<style>.t { width: 20px; height: 20px; background: red }
		@supports (font-synthesis: weight style) and (font-synthesis-weight: none) and
		(font-synthesis-style: oblique-only) and (font-synthesis-position: none) { .t { background: green } }</style>
		<div class=t></div>`,
		`<div class=g style="width: 20px; height: 20px"></div>`)
	// rch liczy się z fontu elementu głównego, nie z fontu przodka
	same("rch-unit", `<style>html { font: 20px Ahem }</style><div style="font: 10px serif">
		<div class=g style="width: 1rch; height: 10px"></div></div>`,
		`<div class=g style="width: 20px; height: 10px"></div>`)
	// "serif" w cudzysłowie to nazwa fontu, którego nie ma, a nie rodzina generyczna: zostaje Ahem
	same("quoted-generic", `<div style='font-family: "serif", Ahem; color: green'>X</div>`,
		`<div class=g style="width: 20px; height: 20px"></div>`)
	// tekst o zerowym rozmiarze fontu zostawia wiersz, więc marginesy nie zwijają się przez jego blok
	same("font-size-zero-margins", `<p style="margin: 20px 0">a</p><p style="font-size: 0">zero</p>
		<p style="margin: 20px 0">b</p>`,
		`<p style="margin: 20px 0 40px">a</p><p style="margin: 20px 0">b</p>`)
	// niezerowy letter-spacing wyłącza ligaturę fi, a ZWNJ nie dostaje własnego odstępu
	same("letter-spacing-ligatures", `<div style="font: 40px Noto Sans; letter-spacing: 4px">fi fl</div>`,
		`<div style="font: 40px Noto Sans; letter-spacing: 4px">f&zwnj;i f&zwnj;l</div>`)
	// bez odstępu ligatura zostaje (wzorzec bez ZWNJ różni się od tekstu z ZWNJ)
	different("ligatures-without-spacing", `<div style="font: 40px Noto Sans">fi fl</div>`,
		`<div style="font: 40px Noto Sans">f&zwnj;i f&zwnj;l</div>`)
	// Ahem nie ma wariantów 'sups', więc indeks górny jest syntetyzowany (mniejszy i podniesiony)...
	different("synthesized-superscript", `<div style="font-family: Ahem">X<span style="font-variant-position: super">X</span></div>`,
		`<div style="font-family: Ahem">XX</div>`)
	// ...chyba że font-synthesis-position: none
	same("no-synthesized-superscript", `<div style="font-family: Ahem">X<span style="font-variant-position: super;
		font-synthesis-position: none">X</span></div>`,
		`<div style="font-family: Ahem">XX</div>`)
}
