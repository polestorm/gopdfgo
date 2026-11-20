package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo"
)

// TestSVGStrokeTransformUse sprawdza trzy cechy SVG 2: vector-effect: non-scaling-stroke (§ 13.2;
// grubość obrysu w układzie gospodarza, bez przekształceń elementu i viewBox, ale bez przekształceń
// CSS przodków), atrybut transform zewnętrznego <svg> w HTML jako transform pola CSS (§ 8.5) oraz
// <use> z elementem z innego pliku (§ 5.6), z kaskadą i :target pliku docelowego.
func TestSVGStrokeTransformUse(t *testing.T) {
	head := `<style>@page { size: 200px 120px; margin: 0 } body { margin: 0 } svg { display: block }</style>`
	files := map[string]string{
		"sprites.svg": `<svg xmlns="http://www.w3.org/2000/svg"><style>:target { fill: green }</style>
			<rect width="10" height="10" fill="red"/><rect id="r" width="100" height="100"/></svg>`,
		"nested.svg": `<svg xmlns="http://www.w3.org/2000/svg"><symbol id="s"><use href="sprites.svg#r"/></symbol>
			<symbol id="loop"><use href="nested.svg#loop"/><rect width="100" height="100" fill="green"/></symbol></svg>`,
		"doc.html": `<html><svg xmlns="http://www.w3.org/2000/svg"><symbol id="g"><rect width="100" height="100" fill="green"/></symbol></svg></html>`,
	}
	resolver := func(url string) (gopdfgo.Resource, error) {
		for name, data := range files {
			if strings.HasSuffix(url, "/"+name) {
				return gopdfgo.Resource{Data: []byte(data)}, nil
			}
		}
		return gopdfgo.DenyAll(url)
	}
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := gopdfgo.Render(context.Background(), []byte(head+body),
			gopdfgo.Options{Fonts: testFonts(t), Resolver: resolver, BaseURL: "file:///doc/"})
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
	green := `<div style="width:100px;height:100px;background:green"></div>`

	// viewBox powiększa 10 razy, a obrys 10px zostaje 10px: od 15 do 25 w pikselach CSS
	same("non-scaling-viewbox", `<svg width="100" height="100" viewBox="0 0 10 10">
		<line x1="0" y1="2" x2="10" y2="2" stroke="green" stroke-width="10px" vector-effect="non-scaling-stroke"/></svg>`,
		`<div style="margin-top:15px;width:100px;height:10px;background:green"></div>`)
	// przekształcenie CSS przodka nie wchodzi do układu gospodarza: obrys rośnie razem z nim
	same("non-scaling-css-transform", `<div style="transform:scale(2);transform-origin:0 0"><svg width="50" height="50">
		<g transform="scale(4)"><line x1="0" y1="2.5" x2="12.5" y2="2.5" stroke="green" stroke-width="10" vector-effect="non-scaling-stroke"/></g></svg></div>`,
		`<div style="margin-top:10px;width:100px;height:20px;background:green"></div>`)
	// grupa przezroczystości wewnątrz SVG nie gubi skali układu
	same("non-scaling-opacity-group", `<svg width="100" height="100" viewBox="0 0 10 10"><g opacity="0.999">
		<line x1="0" y1="2" x2="10" y2="2" stroke="green" stroke-width="10" vector-effect="non-scaling-stroke"/></g></svg>`,
		`<div style="margin-top:15px;width:100px;height:10px;background:green"></div>`)
	// atrybut transform zewnętrznego <svg> działa jak transform CSS (środek pola jako punkt odniesienia)
	// i nie jest stosowany drugi raz wewnątrz rysunku
	same("outer-svg-transform", `<svg width="100" height="100" transform="translate(50, 10) scale(0.5)">
		<rect width="100" height="100" fill="green"/></svg>`,
		`<div style="margin:35px 0 0 75px;width:50px;height:50px;background:green"></div>`)
	// deklaracja CSS zastępuje atrybut
	same("outer-svg-transform-css-wins", `<svg width="100" height="100" transform="scale(0.5)" style="transform:translate(10px,0)">
		<rect width="100" height="100" fill="green"/></svg>`,
		`<div style="margin-left:10px;width:100px;height:100px;background:green"></div>`)
	// <use> z elementem innego pliku: fragment wskazuje element, :target pliku docelowego pasuje do celu
	same("use-external-target", `<svg width="100" height="100"><use href="sprites.svg#r"/></svg>`, green)
	// <symbol> z <use> do trzeciego pliku: adres względny względem pliku symbolu; cykl jest przerywany
	same("use-external-nested", `<svg width="100" height="100"><use href="nested.svg#s"/></svg>`, green)
	same("use-external-cycle", `<svg width="100" height="100"><use href="nested.svg#loop"/></svg>`, green)
	// dokument HTML z osadzonym <svg>
	same("use-external-html", `<svg width="100" height="100"><use href="doc.html#g"/></svg>`, green)
}
