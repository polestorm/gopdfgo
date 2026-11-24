package tests

import (
	"context"
	"github.com/polestorm/gopdfgo/htmlpdf"
	"testing"
)

// TestFragmentationPrint9 sprawdza poprawki fragmentacji druku z rundy frag9 (CSS Fragmentation 3,
// CSS Page 3, CSS Position 3): test i wzorzec renderowane na stronach 200×100 px muszą dać tyle samo stron
// i te same piksele.
func TestFragmentationPrint9(t *testing.T) {
	head := `<style>@page { size: 200px 100px; margin: 0 } body { margin: 0 } :root { print-color-adjust: exact }
		.y { background: yellow } .b { background: blue } .g { background: green } .r { background: red }</style>`
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

	// element contain:size wyższy niż strona w kolumnie flex przechodzi przez strony, a następny element
	// stoi tuż za nim (monolithic-overflow-005)
	same("flex-column-monolithic", `<div class=y style="display:flex;flex-direction:column">
		<div class=b style="contain:size;width:50px;height:250px"></div><div class=g style="height:20px"></div></div>`,
		`<div class=y><div class=b style="width:50px;height:250px"></div><div class=g style="height:20px"></div></div>`)
	// pudełko fixed zagnieżdżone w absolute powtarza się na każdej stronie (fixedpos-002)
	same("nested-fixed-repeats", `<div style="position:absolute;height:300px">
		<div class=g style="position:fixed;bottom:0;width:20px;height:20px"></div></div>`,
		`<div class=g style="position:absolute;top:80px;width:20px;height:20px"></div>
		<div class=g style="position:absolute;top:180px;width:20px;height:20px"></div>
		<div class=g style="position:absolute;top:280px;width:20px;height:20px"></div>`)
	// pudełko absolute z bottom: 0 w bloku podzielonym między strony trafia na dół całego bloku (abspos-001)
	same("bottom-anchored-abspos", `<div style="position:relative">
		<div class=g style="position:absolute;bottom:0;left:50px;width:50px;height:50px"></div>
		<div style="height:250px"></div></div>`,
		`<div style="height:200px"></div><div class=g style="margin-left:50px;width:50px;height:50px"></div>`)
	// element pływający z break-inside: avoid przechodzi w całości na następną stronę
	// (float-page-break-inside-avoid-3)
	same("float-avoid-moves", `<div class=b style="height:60px;width:20px"></div>
		<div style="float:left;break-inside:avoid"><div class=g style="width:20px;height:50px"></div><div class=y style="width:20px;height:50px"></div></div>`,
		`<div class=b style="height:60px;width:20px"></div>
		<div class=g style="break-before:page;width:20px;height:50px"></div><div class=y style="width:20px;height:50px"></div>`)
	// element pływający zagnieżdżony w elemencie flex ma na następnych stronach jeden dalszy ciąg
	// (break-nested-float-in-flex-item-001)
	same("nested-float-in-flex", `<div style="display:flex;flex-direction:column"><div><div>
		<div class=g style="float:left;width:50px;height:250px"></div></div></div></div>`,
		`<div class=g style="width:50px;height:250px"></div>`)
	// blok o wysokości strony z wyższym dzieckiem kończy się na pierwszej stronie razem z dolnym
	// obramowaniem; dziecko ciągnie się dalej (overflowing-block)
	same("fixed-height-ends-with-overflow", `<div style="height:80px;border-bottom:20px solid blue">
		<div class=g style="width:20px;height:150px"></div></div>`,
		`<div style="position:absolute;top:80px;left:20px;width:180px;height:20px" class=b></div>
		<div class=g style="width:20px;height:150px"></div>`)
	// zmiana nazwy strony wewnątrz inline-block nie łamie strony i nie gubi treści (page-name-inline-block-001)
	same("page-name-in-inline-block", `<div style="display:inline-block;vertical-align:top">
		<div class=g style="page:a;width:20px;height:20px"></div><div class=b style="page:b;width:20px;height:20px"></div></div>`,
		`<div class=g style="width:20px;height:20px"></div><div class=b style="width:20px;height:20px"></div>`)
}
