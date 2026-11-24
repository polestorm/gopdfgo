package tests

import (
	"context"
	"github.com/polestorm/gopdfgo/htmlpdf"
	"strings"
	"testing"
)

// TestCSS2Tail sprawdza poprawki z rundy „ogon CSS 2 i CSS Backgrounds”: każdy przypadek porównuje
// stronę z wzorcem zbudowanym bez sprawdzanej cechy. Odpowiadające testy WPT podane są przy przypadkach.
func TestCSS2Tail(t *testing.T) {
	page := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 }</style>`
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
	// near: jak same, ale z tolerancją na wygładzanie krawędzi, które cieniowanie rysuje inaczej niż prostokąty
	near := func(name string, limit int, test, reference string) {
		t.Helper()
		if n := differentPixels(render(name, test), render(name+"-ref", reference)); n < 0 || n > limit {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	square := page + `<div style="width:100px;height:100px;background:green"></div>`

	// :lang() porównuje wielkość liter tylko w ASCII: „İ” ani znak kelwina nie pasują do „i”, „k”
	// (case-sensitive-004)
	same("lang-ascii-case", page+`<style>p{margin:0;height:20px;width:100px;background:green}
		:lang(fİ) { background: red } :lang(Kl) { background: red }</style><p lang="fi"></p><p lang="KL"></p>`,
		page+`<div style="width:100px;height:40px;background:green"></div>`)
	// element główny z position: relative jest przesuwany (abspos-containing-block-initial-007)
	same("root-relative", `<html style="position:relative;top:50px;left:50px;width:200px"><style>@page { size: 300px 200px; margin: 0 }
		body { margin: 0 }</style><div style="width:100px;height:100px;background:green"></div></html>`,
		page+`<div style="margin:50px;width:100px;height:100px;background:green"></div>`)
	// blok pozycjonowany w pudełku liniowym bez treści przed nim: położenie statyczne to góra wiersza
	// (static-inside-inline-001)
	same("static-position-in-inline", page+`<div style="line-height:100px;color:transparent"><span>
		<div style="position:absolute;width:100px;height:100px;background:green"></div> X</span></div>`,
		square)
	// clear na <br> przenosi następny wiersz pod elementy pływające (border-conflict-style-107)
	same("br-clear", page+`<div style="float:left;width:100px;height:50px;background:green"></div><br style="clear:both">
		<div style="float:left;width:100px;height:50px;background:green"></div>`,
		square)
	// podpis tabeli z ujemnym marginesem zakrywa obramowanie tabeli (root-box-002)
	same("caption-over-table-border", page+`<table style="border-collapse:separate;border-spacing:0;border-top:20px solid red">
		<caption style="height:20px;margin-bottom:-20px;background:green"></caption>
		<tr><td style="padding:0;width:100px;height:80px;background:green"></td></table>`,
		square)
	// element główny pływający ma szerokość dopasowaną do treści (float-root)
	same("floating-root", `<html style="float:right"><style>@page { size: 300px 200px; margin: 0 } body { margin: 0 }</style>
		<div style="width:100px;height:100px;background:green"></div></html>`,
		page+`<div style="margin-left:200px;width:100px;height:100px;background:green"></div>`)
	// spacja po reszcie pudełka liniowego rozdzielonego blokiem znika z początku wiersza
	// (inline-text-after-block-in-inline-with-intervening-float)
	same("space-after-block-in-inline", page+`<div style="width:200px"><span><div style="height:20px"></div></span>
		<div style="float:left;width:50px;height:20px;background:green"></div>
		<span style="display:inline-block;width:50px;height:20px;background:green"></span></div>`,
		page+`<div style="margin-top:20px;width:100px;height:20px;background:green"></div>`)
	// element pływający o ujemnej wysokości pudełka marginesowego staje pod wcześniejszym, a nie obok
	// (overhanging-float-paint-order)
	same("negative-height-float", page+`<div style="width:100px"><div style="float:left;width:100px;height:50px;background:red"></div>
		<div style="float:left;width:100px;height:100px;margin-top:-50px;background:red"></div>
		<div style="float:left;width:100px;height:100px;margin-top:-100px;background:green"></div></div>`,
		square)
	// treść zwykłego <span> w pudełku z vertical-align: top przesuwa się razem z nim (anonymous-inline-inherit-001)
	same("top-align-nested", page+`<div style="line-height:0"><span style="vertical-align:top"><span>
		<span style="display:inline-block;width:100px;height:100px;background:green"></span></span></span></div>`,
		square)
	// pudełko pozycjonowane względem pudełka liniowego układane po wyrównaniu wiersza (abspos-inline-007)
	same("abspos-inline-cb", page+`<div style="font-size:20px;line-height:1">x <span style="position:relative;border:1px solid black">tl<span style="display:inline-block;width:30px;height:10px"><span style="position:absolute;bottom:0;right:0;width:10px;height:10px;background:green"></span></span></span></div>`,
		page+`<div style="font-size:20px;line-height:1">x <span style="border:1px solid black">tl<span style="display:inline-block;width:20px;height:10px"></span><span style="display:inline-block;width:10px;height:10px;vertical-align:text-bottom;background:green"></span></span></div>`)
	// blok w pudełku liniowym z opacity dziedziczy krycie (opacity-affects-block-in-inline)
	same("opacity-block-in-inline", page+`<span style="opacity:0.5"><div style="width:100px;height:100px;background:green"></div></span>`,
		page+`<div style="opacity:0.5;width:100px;height:100px;background:green"></div>`)
	// min/max obrazu bez proporcji ograniczają wymiary niezależnie (replaced-elements-max-height-20)
	same("replaced-no-ratio", page+`<img style="display:block;max-height:100px" src="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' width='100'><rect width='100%' height='100%' fill='green'/></svg>">`,
		square)
	// dziesięć elementów pływających po 0,87em mieści się w 8,7em (units-005)
	same("float-width-tolerance", page+`<div style="font-size:16px;width:8.7em;border-left:3px solid white">`+
		strings.Repeat(`<span style="float:left;width:0.87em;height:1em"></span> `, 10)+
		`<div style="clear:both;height:84px;width:100px;background:green"></div></div>`,
		page+`<div style="margin:16px 0 0 3px;width:100px;height:84px;background:green"></div>`)
	// conic-gradient: ćwiartki od kąta początkowego zgodnie z ruchem wskazówek zegara
	near("conic-gradient", 300, page+`<div style="width:100px;height:100px;background:conic-gradient(green 0 25%, blue 0 50%, green 0 75%, blue 0)"></div>`,
		page+`<div style="width:100px;height:100px;background:linear-gradient(to right, transparent 50%, green 0) 0 0/100% 50% no-repeat,
		linear-gradient(to right, green 50%, transparent 0) 0 100%/100% 50% no-repeat, blue"></div>`)
	// background-attachment: local w pudełku przewijanym: obszar pozycjonowania obejmuje całą treść
	// (background-attachment-350)
	same("attachment-local", page+`<div style="overflow:hidden;width:100px;height:100px;background:green url(&quot;data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' width='100' height='100'><rect width='100' height='100' fill='red'/></svg>&quot;) local no-repeat right bottom">
		<div style="height:200px"></div></div>`,
		square)
	// background-attachment: fixed w elemencie przekształconym działa jak scroll
	// (background-attachment-fixed-inside-transform-1)
	same("attachment-fixed-in-transform", page+`<div style="margin-top:100px;transform:translate(0,-100px)"><div style="width:100px;height:100px;background:linear-gradient(green,green) fixed no-repeat 0 0/100px 100px"></div></div>`,
		square)
	// tło elementu głównego SVG wyświetlanego jako obraz wypełnia obraz (background-image-007)
	same("svg-root-background", page+`<div style="width:100px;height:100px;background:red url(&quot;data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' style='background: green'></svg>&quot;)"></div>`,
		square)
	// ::first-letter z literą w ::before dziedziczy po ::before (before-first-letter-selector-001)
	same("first-letter-in-before", page+`<style>div::before{content:"AB";color:green} div::first-letter{font-size:40px}</style><div></div>`,
		page+`<div><span style="color:green"><span style="font-size:40px">A</span>B</span></div>`)
	// XHTML zachowuje znak nowego wiersza tuż po <pre> (c548-ln-ht-000, white-space-pre-001)
	same("xhtml-pre-newline", `<html xmlns="http://www.w3.org/1999/xhtml">`+page+`<pre style="margin:0">
<span style="display:inline-block;width:100px;height:10px;background:green"></span></pre></html>`,
		page+`<pre style="margin:0">

<span style="display:inline-block;width:100px;height:10px;background:green"></span></pre>`)
	// ::first-letter nie usuwa złamania wiersza z <br> na początku bloku (first-letter-selector-005)
	same("first-letter-after-br", page+`<style>div::first-letter{color:red}</style><div><br>AB</div>`,
		page+`<div><br>AB</div>`)
}
