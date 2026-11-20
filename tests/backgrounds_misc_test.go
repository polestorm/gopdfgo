package tests

import (
	"context"
	"github.com/polestorm/gopdfgo"
	"os"
	"testing"
)

// TestBackgroundsMisc sprawdza poprawki znalezione przy testach WPT css-backgrounds: tła, obramowania
// i cienie (CSS Backgrounds 3) oraz właściwość clip (CSS 2.2 § 11.1.2).
func TestBackgroundsMisc(t *testing.T) {
	fonts := testFonts(t)
	if ahem, err := os.ReadFile("../testdata/wpt/fonts/Ahem.ttf"); err == nil {
		fonts = append([]gopdfgo.Font{{Name: "Ahem.ttf", Data: ahem}}, fonts...)
	}
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 } .a { position: absolute }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := gopdfgo.Render(context.Background(), []byte(head+body), gopdfgo.Options{Fonts: fonts})
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
	// border-image-width w em i calc() jest sprowadzane do pikseli
	same("border-image-width-units", `<div style="width:0;height:0;border:40px solid red;background:red;font-size:10px;
		border-image:linear-gradient(green,green) 1 / 4em calc(20px + 20px) calc(50%) 40px"></div>`,
		`<div style="width:80px;height:80px;background:green"></div>`)
	// tekst zaczynający się od nierozdzielającej spacji nie ma ::first-letter
	same("first-letter-nbsp", `<style>div::first-letter { background: red }</style><div>&nbsp;</div>`,
		`<div>&nbsp;</div>`)
	// element główny z display: none nie daje tła płótna
	same("root-display-none", `<style>html { display: none; background: red }</style>`, ``)
	// nadmiar wysokości tabeli dostaje wiersz bez wysokości, nie wiersze z komórkami o podanej wysokości
	same("table-extra-height-auto-row", `<div style="display:table;height:100px;width:40px">
		<div style="display:table-row"><div style="display:table-cell;height:20px;background:blue"></div></div>
		<div style="display:table-row"><div style="display:table-cell;background:green"></div></div>
		<div style="display:table-row"><div style="display:table-cell;height:20px;background:blue"></div></div></div>`,
		`<div style="width:40px;height:20px;background:blue"></div><div style="width:40px;height:60px;background:green"></div>
		<div style="width:40px;height:20px;background:blue"></div>`)
	// cień samej tabeli
	same("table-shadow", `<table style="border-spacing:0;box-shadow:10px 10px green;margin:10px"><tr>
		<td style="padding:0;width:40px;height:30px;background:blue"></td></tr></table>`,
		`<div style="margin:10px;width:40px;height:30px;background:blue;box-shadow:10px 10px green"></div>`)
	// treść elementu zastępowanego jest przycinana do zaokrąglonej krawędzi treści
	same("replaced-content-radius", `<img style="display:block;padding:10px;border-radius:30px;background:green"
		src="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' width='40' height='40'><rect width='40' height='40' fill='blue'/></svg>">`,
		`<div style="width:40px;height:40px;padding:10px;border-radius:30px;
		background:linear-gradient(blue,blue) content-box,green"></div>`)
	// round skaluje obraz, a background-position działa dalej
	same("round-with-position", `<div style="width:60px;height:60px;background:linear-gradient(green,green) 5px 5px / 25px 25px round,blue"></div>`,
		`<div style="width:60px;height:60px;background:linear-gradient(green,green) 5px 5px / 30px 30px repeat,blue"></div>`)
	// background-attachment: local w pudełku przewijanym nie maluje tła pod obramowaniem
	same("attachment-local-padding-box", `<div style="width:40px;height:40px;border:10px solid transparent;overflow:hidden;
		background:green local"></div>`,
		`<div style="width:40px;height:40px;border:10px solid transparent;background:green padding-box"></div>`)
	// currentcolor w gradiencie to kolor pudełka, nie czerń
	same("gradient-currentcolor", `<div style="color:green;height:20px;background:linear-gradient(currentcolor,currentcolor 50%,blue 50%)"></div>`,
		`<div style="height:20px;background:linear-gradient(green,green 50%,blue 50%)"></div>`)
	// czarne tło maski tekstu nie dziedziczy koloru wypełnienia z miejsca użycia maski
	same("text-clip-mask-color", `<div style="font:10px/1 Ahem;color:white">X</div>
		<div style="font:20px/1 Ahem;color:transparent;background:green;background-clip:text;transform:translateX(0)">X</div>`,
		`<div style="height:10px"></div><div style="font:20px/1 Ahem;color:green">X</div>`)
}
