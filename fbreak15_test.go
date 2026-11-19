package gopdfgo

import (
	"context"
	"os"
	"testing"
)

// TestForcedBreakBeforeOutOfFlow sprawdza, że element pływający i pudełko absolute w pozycji statycznej
// stojące za wymuszonym podziałem (break-after poprzednika, break-before elementu pływającego) zaczynają
// się na następnej stronie (CSS Fragmentation 3 § 4), oraz rozmiary wewnętrzne pudełek dopasowujących
// się do treści wokół dzieci z box-sizing: border-box i min-width/max-width (CSS Box Sizing 3 § 3).
func TestForcedBreakBeforeOutOfFlow(t *testing.T) {
	ahem, err := os.ReadFile("testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	head := `<style>@page { size: 300px 200px; margin: 0 } :root { print-color-adjust: exact }
		body { margin: 0; font: 10px/1 Ahem } .a { position: absolute }</style>`
	dir := t.TempDir()
	render := func(name, html string) [][]byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(head+html), Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		return rasterize(t, result.PDF, dir, name)
	}
	same := func(name, test, reference string) {
		t.Helper()
		a, b := render(name, test), render(name+"-ref", reference)
		if len(a) != len(b) {
			t.Errorf("%s: %d stron, wzorzec %d", name, len(a), len(b))
			return
		}
		for i := range a {
			if n := differentPixels(a[i], b[i]); n != 0 {
				t.Errorf("%s: strona %d: %d pikseli różni się od wzorca", name, i+1, n)
			}
		}
	}
	twoPages := `<div style="height:20px;background:blue"></div>
		<div style="break-before:page;width:50px;height:80px;background:lime"></div>`

	// element pływający za blokiem z break-after: page stoi na drugiej stronie, tekst obok niego
	same("float-after-break", `<div style="display:flow-root"><div style="break-after:page;height:20px;background:blue"></div>
		<div style="float:left;width:50px;height:80px;background:lime"></div><span style="color:navy">x</span></div>`,
		`<div style="height:20px;background:blue"></div>
		<div style="break-before:page"><div style="float:left;width:50px;height:80px;background:lime"></div><span style="color:navy">x</span></div>`)
	// break-before: page na samym elemencie pływającym
	same("float-break-before", `<div style="display:flow-root"><div style="height:20px;background:blue"></div>
		<div style="float:left;break-before:page;width:50px;height:80px;background:lime"></div></div>`, twoPages)
	// element pływający na końcu kontenera: podział za ostatnim blokiem nie ginie
	same("float-last-child", `<div style="display:flow-root"><div style="break-after:page;height:20px;background:blue"></div>
		<div style="float:left;width:50px;height:80px;background:lime"></div></div>`, twoPages)
	// pudełko absolute w pozycji statycznej za podziałem zaczyna się u góry drugiej strony
	same("abspos-static-after-break", `<div style="position:relative"><div style="break-after:page;height:20px;background:blue"></div>
		<div class=a style="width:50px;height:80px;background:lime"></div></div>`, twoPages)

	// inline-block wokół dziecka z min-width i box-sizing: border-box ma szerokość min-width, a element
	// pływający wokół dziecka z max-width — szerokość max-width (bez ponownie doliczonego obramowania
	// i odstępu)
	same("border-box-min-width", `<div style="display:inline-block;background:blue;vertical-align:top">
		<div style="box-sizing:border-box;min-width:100px;padding:0 10px;border-left:20px solid lime;height:20px"></div></div>`,
		`<div class=a style="width:100px;height:20px;background:blue"></div><div class=a style="width:20px;height:20px;background:lime"></div>`)
	same("border-box-max-width", `<div style="float:left;background:blue"><div style="box-sizing:border-box;max-width:60px;padding:0 10px;height:20px">
		<span style="display:inline-block;width:200px;height:5px"></span></div></div>`,
		`<div class=a style="width:60px;height:20px;background:blue"></div>`)
	// obraz o stałej szerokości z box-sizing: border-box w elemencie pływającym
	same("border-box-replaced", `<div style="float:left;background:blue;line-height:0">
		<svg style="box-sizing:border-box;width:60px;height:20px;padding-left:20px" width="10" height="10"></svg></div>`,
		`<div class=a style="width:60px;height:20px;background:blue"></div>`)
	// kontener flex o stałej szerokości z box-sizing: border-box w elemencie pływającym
	same("border-box-flex", `<div style="float:left;background:blue"><div style="display:flex;box-sizing:border-box;width:60px;height:20px;padding-left:20px"></div></div>`,
		`<div class=a style="width:60px;height:20px;background:blue"></div>`)
}
