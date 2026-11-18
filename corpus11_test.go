package gopdfgo

import (
	"context"
	"os"
	"testing"
)

// TestCorpus11 sprawdza poprawki znalezione przy porównaniu korpusu dokumentów z wydrukami Chromium:
// legenda ramki fieldset, znacznik summary, puste pole tekstowe, larger/smaller i dzielenie wyrazów
// angielskich. Font Ahem: przy font: 20px/1 linia bazowa leży 16 px pod górą wiersza tekstu.
func TestCorpus11(t *testing.T) {
	ahem, err := os.ReadFile("testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0; font: 20px/1 Ahem }
		.g { background: green } .a { position: absolute }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(head+body), Options{Fonts: fonts})
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

	// legenda: górna krawędź ramki to górna krawędź legendy; margines ramki (10 px) zwija się z marginesem
	// poprzednika (20 px) bez przesunięcia obramowania, więc legenda stoi 40 px od góry, a treść ramki
	// tuż pod legendą (HTML § 15.3.13)
	same("fieldset-legend-collapse", `<div class=g style="width:20px;height:20px;margin-bottom:20px"></div>
		<fieldset style="margin:10px 0 0;padding:0;border:2px solid transparent;width:100px"><legend style="color:green;padding:0">X</legend>
		<div class=g style="width:20px;height:20px"></div></fieldset>`,
		`<div class="g a" style="width:20px;height:20px"></div><div class="g a" style="top:40px;left:2px;width:20px;height:20px"></div>
		<div class="g a" style="top:60px;left:2px;width:20px;height:20px"></div>`)
	// znacznik summary (disclosure-open wewnątrz) zajmuje 1,06 em, jak w Chromium: przy piśmie 50 px
	// tekst zaczyna się 53 px od krawędzi
	same("summary-marker-width", `<details open style="font-size:50px"><summary style="color:green">X</summary></details>
		<style>summary::marker { color: transparent }</style>`,
		`<div class="g a" style="left:53px;width:50px;height:50px"></div>`)
	// puste pole tekstowe ma linię bazową tekstu, więc wiersz z nim jest tak samo wysoki jak wiersz
	// z polem z wartością (wcześniej linią bazową była dolna krawędź pola i wiersz rósł)
	same("empty-input-baseline", `<div style="font-size:10px">X <input style="border:0;padding:0;height:10px;width:30px"></div>
		<div class=g style="width:20px;height:20px"></div>`,
		`<div style="font-size:10px">X <input value="&#160;" style="border:0;padding:0;height:10px;width:30px"></div>
		<div class=g style="width:20px;height:20px"></div>`)
	// smaller przy rozmiarze rodzica z tablicy przechodzi na sąsiedni wpis (medium 16 px -> small
	// 14,22 px), a między wpisami interpoluje (CSS Fonts 4 § 2.5): 13,33 px -> ok. 11,02 px, nie 12 px
	same("smaller-keyword", `<div style="font-size:16px;color:green"><span style="font-size:smaller">XXXXXXXXX</span></div>`,
		`<div style="font-size:16px;color:green"><span style="font-size:14.2222px">XXXXXXXXX</span></div>`)
	same("smaller-interpolated", `<div style="font-size:10pt;color:green"><span style="font-size:smaller">XXXXXXXXXX</span></div>`,
		`<div style="font-size:10pt;color:green"><span style="font-size:11.0237px">XXXXXXXXXX</span></div>`)
	// lang="en" dzieli wyrazy według słownika en-US (responsi-bilities), nie en-GB (respons-ibilities)
	same("hyphenation-en-us", `<p lang="en" style="hyphens:auto;width:190px;margin:0;color:green;hyphenate-character:'-'">responsibilities</p>`,
		`<p style="margin:0;color:green">responsi-<br>bilities</p>`)
}
