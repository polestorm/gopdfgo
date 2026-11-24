package tests

import (
	"context"
	"encoding/base64"
	"github.com/polestorm/gopdfgo/htmlpdf"
	"os"
	"path/filepath"
	"testing"
)

// TestCSS2Rest sprawdza poprawki z rundy pozostałych testów CSS 2: znaczniki listy z url("…")
// i gradientem, ::first-letter za pustymi elementami i za interpunkcją z ::before, dziedziczenie
// szerokości obramowania bez stylu, elementy pływające w kontekście nowrap i atrybut charset arkusza.
func TestCSS2Rest(t *testing.T) {
	ahem, err := os.ReadFile("../testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]htmlpdf.Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0; font: 10px/1 Ahem }
		.a { position: absolute }</style>`
	const css = "body { color: green }"
	sheets := map[string][]byte{
		"file:///t/utf16be.css": func() []byte {
			var out []byte
			for _, r := range css {
				out = append(out, byte(r>>8), byte(r))
			}
			return out
		}(),
	}
	resolver := func(url string) (htmlpdf.Resource, error) {
		if data, ok := sheets[url]; ok {
			return htmlpdf.Resource{Data: data, MediaType: "text/css"}, nil
		}
		return htmlpdf.DenyAll(url)
	}
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := htmlpdf.Render(context.Background(), []byte(head+body), htmlpdf.Options{Fonts: fonts, Resolver: resolver, BaseURL: "file:///t/"})
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
	const svg = `data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' style='background: green'></svg>`
	svgBase64 := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(
		[]byte(`<svg xmlns='http://www.w3.org/2000/svg' style='background: green'></svg>`))

	// url("…") w cudzysłowie to ten sam <url> co bez niego
	same("list-style-image-quoted", `<ul style="margin:0;padding-left:40px"><li style="list-style-image: url(&quot;`+svg+`&quot;)">X</li></ul>`,
		`<ul style="margin:0;padding-left:40px"><li style="list-style-image: url(`+svgBase64+`)">X</li></ul>`)
	// gradient jako znacznik: obraz bez własnego rozmiaru, kwadrat 1em jak SVG bez width i height
	same("list-style-image-gradient", `<ul style="margin:0;padding-left:40px"><li style="list-style-image: linear-gradient(green, green)">X</li></ul>`,
		`<ul style="margin:0;padding-left:40px"><li style="list-style-image: url(`+svgBase64+`)">X</li></ul>`)
	// ::first-letter pomija element z samą spacją i obejmuje cudzysłów z ::before razem z literą
	same("first-letter-after-space-only-inline", `<style>div::first-letter { color: green }</style><div><span> </span> <span>XX</span></div>`,
		`<div><span style="color:green">X</span>X</div>`)
	same("first-letter-quote-in-before", `<style>div::first-letter { color: green } span::before { content: "(" }</style><div><span>XX</span></div>`,
		`<div><span style="color:green">(X</span>X</div>`)
	// inherit bierze obliczoną szerokość obramowania rodzica, choć ten obramowania nie ma
	same("border-width-inherit", `<div style="border-width:2em"><p style="margin:0;border-width:inherit;border-style:solid;height:10px"></p></div>`,
		`<p style="margin:0;border:20px solid;height:10px"></p>`)
	same("outline-width-inherit", `<div style="outline:none 15px;margin:20px"><div style="outline-style:solid;outline-width:inherit;height:10px"></div></div>`,
		`<div style="outline:solid 15px;margin:20px;height:10px"></div>`)
	// nowrap: element pływający w nierozdzielnym ciągu, który się nie mieści, staje pod wierszem
	same("float-in-nowrap-run", `<div style="width:100px;white-space:nowrap">X<span style="float:right;width:50px;height:20px;background:blue"></span>XX XXXXXXXXXX</div>`,
		`<div style="width:100px;white-space:nowrap">XXX XXXXXXXXXX<span style="float:right;width:50px;height:20px;background:blue"></span></div>`)
	// ten sam ciąg mieszczący się w wierszu zostawia element obok treści
	same("float-in-nowrap-run-fits", `<div style="width:100px;white-space:nowrap">X<span style="float:left;width:50px;height:20px;background:blue"></span>XX</div>`,
		`<div style="width:100px;white-space:nowrap"><span style="float:left;width:50px;height:20px;background:blue"></span>XXX</div>`)
	// atrybut charset elementu <link> podaje kodowanie arkusza bez znacznika kolejności bajtów
	same("link-charset", `<link rel=stylesheet href="utf16be.css" charset="UTF-16BE"><div style="width:20px;height:20px;background:currentcolor"></div>`,
		`<div style="width:20px;height:20px;background:green"></div>`)
}

// TestUnknownFamilyFallsBackToSerif sprawdza font domyślny: lista rodzin bez żadnej dostępnej
// rodziny kończy się na rodzinie generycznej serif (jak w Chromium), a rodzina dostępna przez
// podstawienie zgodne metrycznie (Arial → Arimo) zostaje przy swoim foncie.
func TestUnknownFamilyFallsBackToSerif(t *testing.T) {
	var fonts []htmlpdf.Font
	for _, name := range []string{"NotoSans-Regular.ttf", "NotoSerif-Regular.ttf", "tinos/Tinos-Regular.ttf", "arimo/Arimo-Regular.ttf"} {
		data, err := os.ReadFile(filepath.Join("../testdata/fonts", name))
		if err != nil {
			t.Fatal(err)
		}
		fonts = append(fonts, htmlpdf.Font{Name: filepath.Base(name), Data: data})
	}
	generic := map[string][]string{"serif": {"Tinos"}, "sans-serif": {"Arimo"}}
	dir := t.TempDir()
	page := func(name, family string) []byte {
		t.Helper()
		html := `<style>@page { size: 300px 100px; margin: 0 } body { margin: 0; font-size: 20px }</style><p style="font-family: ` +
			family + `">Zażółć gęślą jaźń</p>`
		result, err := htmlpdf.Render(context.Background(), []byte(html), htmlpdf.Options{Fonts: fonts, GenericFamilies: generic})
		if err != nil {
			t.Fatal(err)
		}
		return rasterize(t, result.PDF, dir, name)[0]
	}
	serif, sans := page("serif", "serif"), page("sans", "Arimo")
	if n := differentPixels(page("unknown", "UnknownFont"), serif); n != 0 {
		t.Errorf("nieznana rodzina: %d pikseli różni się od serif", n)
	}
	if n := differentPixels(page("unknown-quoted-generic", `"monospace"`), serif); n != 0 {
		t.Errorf("nazwa rodziny generycznej w cudzysłowie: %d pikseli różni się od serif", n)
	}
	if n := differentPixels(page("arial", "Arial"), sans); n != 0 {
		t.Errorf("Arial: %d pikseli różni się od Arimo", n)
	}
	if n := differentPixels(page("unknown-then-sans", "UnknownFont, sans-serif"), sans); n != 0 {
		t.Errorf("nieznana rodzina przed sans-serif: %d pikseli różni się od Arimo", n)
	}
}
