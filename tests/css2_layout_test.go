package tests

import (
	"context"
	"github.com/polestorm/gopdfgo/htmlpdf"
	"testing"
)

// TestCSS2LayoutFixes sprawdza poprawki układu z testów W3C CSS 2.1 (normal-flow, positioning,
// margin-padding-clear, linebox, tables, visuren): każdy przypadek porównuje stronę z wzorcem
// zbudowanym bez badanej własności.
func TestCSS2LayoutFixes(t *testing.T) {
	page := `<style>@page { size: 300px 300px; margin: 0 } body { margin: 0 }</style>`
	dir := t.TempDir()
	render := func(name, html string) []byte {
		t.Helper()
		result, err := htmlpdf.Render(context.Background(), []byte(html), htmlpdf.Options{Fonts: testFonts(t)})
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
		if n := differentPixels(render(name, test), render(name+"-ref", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}

	// dolny margines pustego dziecka należy do górnego marginesu rodzica z min-height, więc element
	// pływający z tego dziecka stoi na górze rodzica (WPT margin-collapse-min-height-001)
	same("collapse-through-float", page+`<div style="min-height:20px;background:yellow"><div style="margin-bottom:40px">
		<div style="float:left;width:50px;height:20px;background:green"></div></div></div>`,
		page+`<div style="margin-top:40px;height:20px;background:yellow"><div style="width:50px;height:20px;background:green"></div></div>`)
	// pozycjonowany potomek pustego bloku przesuwa się razem z nim (WPT margin-collapse-137)
	same("collapse-through-abspos", page+`<div style="position:relative;margin-bottom:40px">
		<div style="position:absolute;top:0;left:0;width:50px;height:20px;background:green"></div></div>`,
		page+`<div style="margin-top:40px;width:50px;height:20px;background:green"></div>`)
	// podpis poszerza tabelę do swojej szerokości minimalnej (WPT padding-right-applies-to-015)
	same("caption-min-width", page+`<div style="float:left"><div style="display:table"><div style="display:table-caption;padding-right:50px">
		<div style="width:30px;height:20px;background:green"></div></div><div style="display:table-cell"></div></div></div>
		<div style="float:left;width:10px;height:20px;background:blue"></div>`,
		page+`<div style="width:30px;height:20px;background:green;float:left"></div><div style="margin-left:80px;width:10px;height:20px;background:blue"></div>`)
	// padding kolumny nie poszerza tabeli (WPT padding-right-applies-to-006)
	same("column-padding", page+`<div style="float:left"><div style="display:table"><div style="display:table-column;padding-right:50px"></div>
		<div style="display:table-cell"><div style="width:30px;height:20px;background:green"></div></div></div></div>
		<div style="float:left;width:10px;height:20px;background:blue"></div>`,
		page+`<div style="width:30px;height:20px;background:green;float:left"></div><div style="width:10px;height:20px;background:blue;float:left"></div>`)
	// max-width ogranicza element pływający o podanej szerokości (WPT max-width-applies-to-018)
	same("float-max-width", page+`<div style="float:left;width:200px;max-width:100px;height:20px;background:green"></div>`,
		page+`<div style="width:100px;height:20px;background:green"></div>`)
	// procentowa wysokość obrazu w inline-block o podanej wysokości wyznacza jego szerokość
	// (WPT intrinsic-size-with-anonymous-block)
	same("percent-height-in-shrink-to-fit", page+`<div style="display:inline-block;height:50px;background:green;vertical-align:top">
		<img src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='10'/%3E" style="height:100%;vertical-align:top"><p style="margin:0"></p></div>`,
		page+`<div style="width:50px;height:50px;background:green"></div>`)
	// margines -100% liczy się w szerokości dopasowanej do treści jako zero (WPT intrinsic-size-with-negative-margins)
	same("percent-margin-intrinsic", page+`<div style="display:inline-block;border:solid green 25px;line-height:0">
		<div style="display:inline-block;width:50px;height:50px;margin-right:-100%;background:green"></div></div>`,
		page+`<div style="width:100px;height:100px;background:green"></div>`)
	// element pływający z overflow: hidden leży pod pudełkiem pozycjonowanym (WPT max-width-106)
	same("clipping-float-below-positioned", page+`<div style="position:absolute;width:100px;height:50px;background:green"></div>
		<div style="float:left;overflow:hidden;width:100px;height:50px;background:red"></div>`,
		page+`<div style="width:100px;height:50px;background:green"></div>`)
	// blok z overflow: auto leży pod wcześniejszym pudełkiem pozycjonowanym (WPT abspos-overflow-011)
	same("clipping-block-below-positioned", page+`<div style="position:relative"><div style="position:absolute;top:0;width:100px;height:50px;background:green"></div>
		<div style="overflow:auto;width:100px;height:50px;background:red"></div></div>`,
		page+`<div style="width:100px;height:50px;background:green"></div>`)
	// blok wydzielony z pudełka liniowego z position: relative przesuwa się razem z nim (WPT block-in-inline-relpos-001)
	same("block-in-relative-inline", page+`<span style="position:relative;left:40px"><span style="display:block;width:50px;height:20px;background:green"></span></span>`,
		page+`<div style="margin-left:40px;width:50px;height:20px;background:green"></div>`)
	// ostatni pusty wiersz inline-block nie wyznacza jego linii bazowej (WPT block-in-inline-baseline-001)
	same("empty-last-line-baseline", page+`<div style="line-height:20px;font-size:0"><div style="display:inline-block;width:20px;height:10px;background:blue"></div><div style="display:inline-block;width:50px">
		<div style="height:20px;background:green"></div><span><div style="height:20px;background:green"></div></span></div></div>`,
		page+`<div style="line-height:20px;font-size:0"><div style="display:inline-block;width:20px;height:10px;background:blue"></div><div style="display:inline-block;width:50px;vertical-align:bottom">
		<div style="height:20px;background:green"></div><div style="height:20px;background:green"></div></div></div>`)
	// wiersz bez komórek ma podaną wysokość (WPT border-collapse-empty-row)
	same("empty-row-height", page+`<table style="border-spacing:0"><tr><td style="padding:0"><div style="width:20px;height:20px;background:green"></div></td></tr>
		<tr style="height:30px"></tr><tr><td style="padding:0"><div style="width:20px;height:20px;background:green"></div></td></tr></table>`,
		page+`<div style="width:20px;height:20px;background:green"></div><div style="margin-top:30px;width:20px;height:20px;background:green"></div>`)
	// position: fixed tworzy kontekst nakładania, więc potomek z ujemnym z-index zostaje nad jego tłem
	// (WPT fixed-pos-stacking-001)
	same("fixed-stacking-context", page+`<div style="position:fixed;left:0;top:0;width:50px;height:20px;background:red">
		<div style="position:absolute;z-index:-1;width:50px;height:20px;background:green"></div></div>`,
		page+`<div style="width:50px;height:20px;background:green"></div>`)
	// procentowe top przy wysokości bloku zawierającego zależnej od treści liczy się jak auto (WPT top-115)
	same("relative-percent-top-auto-height", page+`<div><div style="position:relative;top:50%;width:50px;height:20px;background:green"></div><div style="height:200px"></div></div>`,
		page+`<div style="width:50px;height:20px;background:green"></div>`)
	// vertical-align: bottom wyższe niż wiersz powiększa go w górę (WPT vertical-align-121)
	same("vertical-align-bottom-grows-up", page+`<div style="font-size:0;line-height:0"><div style="display:inline-block;width:20px;height:20px;background:blue"></div><div style="display:inline-block;width:20px;height:50px;background:green;vertical-align:bottom"></div></div>`,
		page+`<div style="font-size:0;line-height:0"><div style="display:inline-block;width:20px;height:20px;background:blue"></div><div style="display:inline-block;width:20px;height:50px;background:green"></div></div>`)
}
