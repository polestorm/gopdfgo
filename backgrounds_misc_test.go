package gopdfgo

import (
	"context"
	"testing"
)

// TestBackgroundsMisc sprawdza poprawki znalezione przy testach WPT css-backgrounds: tła, obramowania
// i cienie (CSS Backgrounds 3) oraz właściwość clip (CSS 2.2 § 11.1.2).
func TestBackgroundsMisc(t *testing.T) {
	fonts := testFonts(t)
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 } .a { position: absolute }</style>`
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

	// obraz tła (tu gradient) nie dziedziczy przezroczystości półprzezroczystego obramowania
	// poprzedniego pudełka
	same("pattern-alpha-reset", `<div style="height:20px;border:5px solid rgba(0,0,255,0.4)"></div>
		<div style="height:50px;background-image:linear-gradient(green,green)"></div>`,
		`<div style="height:20px;border:5px solid rgba(0,0,255,0.4)"></div>
		<div style="height:50px;background:green"></div>`)
	// clip: rect(top, right, bottom, left) – right i left liczone od lewej krawędzi obramowania
	same("clip-rect", `<div class=a style="top:10px;left:10px;width:100px;height:100px;background:green;
		clip:rect(10px, 60px, auto, 20px)"></div>`,
		`<div class=a style="top:20px;left:30px;width:40px;height:90px;background:green"></div>`)
	// background-clip: border-area – tło tylko pod obramowaniem, bez boku hidden
	same("border-area", `<div style="width:60px;height:40px;border:10px solid transparent;border-right-style:hidden;
		background:linear-gradient(green,green) border-area"></div>`,
		`<div style="width:60px;height:40px;border:10px solid green;border-right-style:hidden"></div>`)
	// background-clip: border-area z obramowaniem double – tylko dwa paski
	same("border-area-double", `<div style="width:60px;height:40px;border:15px double transparent;
		background-color:green;background-clip:border-area"></div>`,
		`<div style="width:60px;height:40px;border:15px double green"></div>`)
	// każda warstwa ma własne background-clip; kolor tła bierze je z ostatniej warstwy
	same("clip-per-layer", `<div style="width:60px;height:40px;padding:10px;background-color:red;
		background-image:linear-gradient(green,green),none;background-clip:border-box,content-box"></div>`,
		`<div style="width:80px;height:60px;background:green"></div>`)
	// SVG bez viewBox nie jest skalowany: procenty liczą się względem rozmiaru obrazu tła
	same("svg-without-viewbox", `<div style="width:40px;height:80px;background:url(&quot;data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' width='8' height='32'><rect width='100%25' height='50%25' fill='green'/></svg>&quot;)
		0 0 / 40px 80px no-repeat"></div>`,
		`<div style="width:40px;height:40px;background:green"></div>`)
	// duży spread cienia wewnętrznego wypełnia całe pudełko odstępu
	same("inset-shadow-large-spread", `<div style="width:60px;height:40px;border:10px solid transparent;
		box-shadow:inset 0 0 0 100px green"></div>`,
		`<div style="width:60px;height:40px;border:10px solid transparent;background:green;background-clip:padding-box"></div>`)
	// cień wiersza tabeli
	same("table-row-shadow", `<div style="display:table;margin:20px"><div style="display:table-row;box-shadow:5px 5px green">
		<div style="display:table-cell;width:50px;height:30px"></div></div></div>`,
		`<div style="margin:20px;width:50px;height:30px;box-shadow:5px 5px green"></div>`)
	// box-decoration-break: slice – cień fragmentów w kolumnach jest wycinkiem cienia całego pudełka
	same("slice-shadow-columns", `<div style="columns:2;column-fill:auto;gap:20px;width:120px;height:40px;margin:10px">
		<div style="box-shadow:5px 5px green;height:80px"></div></div>`,
		`<div class=a style="left:15px;top:15px;width:50px;height:35px;background:green"></div>
		<div class=a style="left:10px;top:10px;width:50px;height:40px;background:white"></div>
		<div class=a style="left:85px;top:10px;width:50px;height:45px;background:green"></div>
		<div class=a style="left:80px;top:10px;width:50px;height:40px;background:white"></div>`)
	// tło body nie przechodzi na płótno, gdy element główny ma contain: paint
	same("body-propagation-contain", `<style>html { contain: paint } body { background: green; margin: 0; height: 50px }</style>`,
		`<div style="height:50px;background:green"></div>`)
	// element główny z display: none nie daje tła płótna
	same("root-display-none", `<style>html { display: none; background: red }</style>`, ``)
}
