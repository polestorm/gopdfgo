package tests

import (
	"context"
	"github.com/polestorm/gopdfgo"
	"strings"
	"testing"
)

// Dokumenty osadzone w <iframe>, <object> i <embed> (HTML § 4.8.5–4.8.7) są drukowane: ułożone
// w rozmiarze elementu i przycięte do niego, jak w Chromium. Wcześniej ramka była pusta, a <object>
// z dokumentem HTML pokazywał treść zastępczą.
func TestEmbeddedDocuments(t *testing.T) {
	dir := t.TempDir()
	page := `<style>@page { size: 200px 120px; margin: 0 } body { margin: 0 } iframe, object, embed { display: block; border: 0 }</style>`
	inner := `<style>body { margin: 0 }</style><div style="width:60px;height:300px;background:green"></div>`
	resolver := func(url string) (gopdfgo.Resource, error) {
		if strings.HasSuffix(url, "inner.html") {
			return gopdfgo.Resource{Data: []byte(inner), MediaType: "text/html"}, nil
		}
		return gopdfgo.DenyAll(url)
	}
	render := func(html string) []byte {
		t.Helper()
		result, err := gopdfgo.Render(context.Background(), []byte(page+html), gopdfgo.Options{Fonts: testFonts(t), Resolver: resolver, BaseURL: "file:///doc/"})
		if err != nil {
			t.Fatal(err)
		}
		return result.PDF
	}
	// zielony prostokąt 60 px szeroki, ucięty do wysokości elementu (40 px), przesunięty o 10 px
	reference := rasterize(t, render(`<div style="margin:10px 0 0 10px;width:60px;height:40px;background:green"></div>`), dir, "ref")
	for name, html := range map[string]string{
		"iframe-srcdoc": `<iframe style="margin:10px 0 0 10px;width:100px;height:40px" srcdoc='` + inner + `'></iframe>`,
		"iframe-src":    `<iframe style="margin:10px 0 0 10px;width:100px;height:40px" src="inner.html"></iframe>`,
		"object":        `<object style="margin:10px 0 0 10px;width:100px;height:40px" data="inner.html" type="text/html">treść zastępcza</object>`,
		"embed":         `<embed style="margin:10px 0 0 10px;width:100px;height:40px" src="inner.html">`,
	} {
		got := rasterize(t, render(html), dir, name)
		if len(got) != len(reference) {
			t.Errorf("%s: %d stron, wzorzec ma %d", name, len(got), len(reference))
			continue
		}
		if n := differentPixels(got[0], reference[0]); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	// ramka wskazująca na własny dokument nie zapętla silnika
	self := func(url string) (gopdfgo.Resource, error) {
		return gopdfgo.Resource{Data: []byte(`<iframe src="self.html"></iframe>`), MediaType: "text/html"}, nil
	}
	if _, err := gopdfgo.Render(context.Background(), []byte(`<iframe src="self.html"></iframe>`), gopdfgo.Options{Fonts: testFonts(t), Resolver: self, BaseURL: "file:///doc/"}); err != nil {
		t.Fatal(err)
	}
}
