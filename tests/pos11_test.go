package tests

import (
	"context"
	"github.com/polestorm/gopdfgo/htmlpdf"
	"testing"
)

// TestPositionedPrint11 sprawdza poprawki pozycjonowania w druku z rundy pos11 (CSS 2.1 § 10.1, CSS Break 3,
// CSS Page 3, CSS Backgrounds 3): test i wzorzec renderowane na stronach 200×100 px muszą dać tyle samo
// stron i te same piksele.
func TestPositionedPrint11(t *testing.T) {
	head := `<style>@page { size: 200px 100px; margin: 0 } body { margin: 0 } :root { print-color-adjust: exact }
		.y { background: yellow } .b { background: blue } .g { background: green }</style>`
	dir := t.TempDir()
	pages := func(name, body string) [][]byte {
		t.Helper()
		result, err := htmlpdf.Render(context.Background(), []byte(head+body), htmlpdf.Options{Fonts: testFonts(t)})
		if err != nil {
			t.Fatal(err)
		}
		return rasterize(t, result.PDF, dir, name)
	}
	same := func(name, test, reference string) {
		t.Helper()
		got, want := pages(name, test), pages(name+"-ref", reference)
		if len(got) != len(want) {
			t.Errorf("%s: %d stron, wzorzec %d", name, len(got), len(want))
			return
		}
		for i := range got {
			if n := differentPixels(got[i], want[i]); n != 0 {
				t.Errorf("%s: strona %d: %d pikseli różni się od wzorca", name, i+1, n)
			}
		}
	}

	// top pudełka bez pozycjonowanego przodka liczymy od pierwszej strony, nie od strony zastępnika:
	// top: 100px to góra drugiej strony
	same("icb-next-page", `<div class=y style="height:125px"></div>
		<div class=b style="position:absolute;top:100px;width:50px;height:25px"></div>`,
		`<div class=y style="height:125px"><div style="height:100px"></div><div class=b style="width:50px;height:25px"></div></div>`)
	// zastępnik na trzeciej stronie, pudełko na pierwszej (dokładane do strony już ułożonej)
	same("icb-earlier-page", `<div class=y style="height:225px"></div>
		<div class=b style="position:absolute;top:50px;width:50px;height:25px"></div>`,
		`<div class=y style="height:225px"><div style="height:50px"></div><div class=b style="width:50px;height:25px"></div></div>`)
	// pudełko wyższe niż strona zaczęte na wcześniejszej stronie ciągnie się przez następne
	same("icb-earlier-page-split", `<div class=y style="height:225px"></div>
		<div class=b style="position:absolute;top:50px;width:50px;height:150px"></div>`,
		`<div class=y style="height:100px"><div style="height:50px"></div><div class=b style="width:50px;height:50px"></div></div>
		<div class=y style="height:100px"><div class=b style="width:50px;height:100px"></div></div><div class=y style="height:25px"></div>`)
	// pudełko odłożone o dwie strony i wyższe niż strona nie gubi ostatniej strony (bezpiecznik stron)
	same("deferred-tall", `<div class=b style="position:absolute;top:200px;width:50px;height:300px"></div>`,
		`<div style="height:200px"></div><div class=b style="width:50px;height:300px"></div>`)
	// dalszy ciąg pudełka, którego blokiem zawierającym jest strona, stoi przy lewej krawędzi obszaru
	// bieżącej strony (strona lewa ma inny margines)
	same("continuation-page-margins", `<style>@page :left { margin-left: 50px }</style>
		<div class=b style="position:absolute;width:20px;height:250px"></div>`,
		`<style>@page :left { margin-left: 50px }</style><div class=b style="width:20px;height:250px"></div>`)
	// pudełko w pozycji statycznej, którego zastępnik stoi na dolnej krawędzi strony, zaczyna się
	// w całości na następnej stronie
	same("static-at-page-bottom", `<div class=y style="height:100px"></div>
		<div style="position:absolute;width:20px"><div class=b style="height:50px"></div></div>`,
		`<div class=y style="height:100px"></div><div class=b style="width:20px;height:50px"></div>`)
	// potomek dalszego ciągu pudełka absolute liczy top od niepodzielonego pudełka (fixedpos-008)
	same("fragmented-cb", `<div style="position:absolute;top:50px"><div style="height:150px"></div>
		<div style="position:absolute;top:0"><div class=b style="position:absolute;top:250px;width:10px;height:10px"></div></div></div>`,
		`<div style="height:300px"></div><div class=b style="width:10px;height:10px"></div>`)
	// tło płótna bez powtarzania jest tylko na pierwszej stronie, także na trzeciej się nie powtarza
	same("canvas-no-repeat", `<style>body { background: linear-gradient(blue, blue) no-repeat; background-size: 50px 50px }</style>
		<div style="height:100px"></div><div style="height:100px"></div><div style="height:100px"></div>`,
		`<div class=b style="position:absolute;top:0;width:50px;height:50px"></div><div style="height:300px"></div>`)
	// element zastępowany absolute wyższy niż strona jest krojony między strony jak w przepływie
	same("abspos-replaced-sliced", `<svg style="position:absolute;top:0;left:0;display:block" width="50" height="150"><rect width="50" height="150" fill="blue"/></svg>`,
		`<svg style="display:block" width="50" height="150"><rect width="50" height="150" fill="blue"/></svg>`)
}
