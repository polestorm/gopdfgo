package tests

import (
	"context"
	"github.com/polestorm/gopdfgo/htmlpdf"
	"testing"
)

// TestParallelOverflowFlows sprawdza, że rodzeństwo bloku o podanej wysokości, z którego treść wylewa się
// poza koniec strony, stoi zaraz za obramowaniem tego bloku, a wylewająca się treść biegnie na następnych
// stronach równolegle z nim (CSS 2.1 § 11.1, CSS Fragmentation 3 § 4). Test i wzorzec renderowane na
// stronach 200×100 px muszą dać tyle samo stron i te same piksele; nic nie może zginąć ani się powtórzyć.
func TestParallelOverflowFlows(t *testing.T) {
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

	// blok o wysokości zero z treścią sięgającą drugiej strony: następny blok zaczyna się na pierwszej
	// stronie, a nie tam, gdzie kończy się wylewająca się treść (wzorzec monolithic-overflow-031)
	same("zero-height-overflow", `<div style="height:0"><div style="height:150px"></div>
		<div class=b style="height:20px"></div></div><div class=y style="width:50px;height:30px"></div>`,
		`<div class=y style="width:50px;height:30px"></div><div style="height:120px"></div>
		<div class=b style="height:20px"></div>`)
	// blok o podanej wysokości mieszczący się na stronie, z treścią przechodzącą na następną: rodzeństwo
	// stoi zaraz za nim na pierwszej stronie
	same("fixed-height-overflow-sibling", `<div class=g style="height:20px"></div>
		<div style="height:50px"><div class=b style="width:30px;height:120px"></div></div>
		<div class=y style="height:20px;margin-left:50px"></div>`,
		`<div class=g style="height:20px"></div><div class=b style="width:30px;height:120px"></div>
		<div class=y style="position:absolute;top:70px;left:50px;width:150px;height:20px"></div>`)
	// rodzic o wysokości auto kończy się (z dolnym obramowaniem) zaraz za blokiem, nie na dole strony
	same("auto-parent-ends-after-block", `<div style="border-bottom:10px solid blue">
		<div style="height:30px"><div class=g style="width:20px;height:150px"></div></div></div>
		<div class=y style="height:10px;width:20px;margin-left:40px"></div>`,
		`<div class=g style="width:20px;height:150px"></div>
		<div class=b style="position:absolute;top:30px;left:20px;width:180px;height:10px"></div>
		<div class=y style="position:absolute;top:40px;left:40px;width:20px;height:10px"></div>`)
	// rodzeństwo samo dzielone między strony biegnie równolegle z wylewającą się treścią, a blok za nim
	// trafia na następną stronę zaraz za nim; nic się nie powtarza
	same("sibling-breaks-in-parallel", `<div style="height:10px"><div class=g style="width:20px;height:150px"></div></div>
		<div class=y style="margin-left:40px;width:20px;height:150px"></div>
		<div class=b style="margin-left:80px;width:20px;height:20px"></div>`,
		`<div class=g style="width:20px;height:150px"></div>
		<div class=y style="position:absolute;top:10px;left:40px;width:20px;height:150px"></div>
		<div class=b style="position:absolute;top:160px;left:80px;width:20px;height:20px"></div>`)
	// wylewająca się treść dłuższa niż rodzeństwo: kończy się na trzeciej stronie, rodzeństwo się nie powtarza
	same("overflow-outlasts-siblings", `<div style="height:10px"><div class=g style="width:20px;height:250px"></div></div>
		<div class=y style="margin-left:40px;width:20px;height:120px"></div>`,
		`<div class=g style="width:20px;height:250px"></div>
		<div class=y style="position:absolute;top:10px;left:40px;width:20px;height:120px"></div>`)
	// wymuszony podział przed rodzeństwem zachodzi raz: rodzeństwo staje u góry drugiej strony obok dalszego
	// ciągu wylewającej się treści, a nie dopiero za nim
	same("forced-break-beside-overflow", `<div style="height:10px"><div class=g style="width:20px;height:150px"></div></div>
		<div class=y style="break-before:page;margin-left:40px;width:20px;height:20px"></div>`,
		`<div class=g style="width:20px;height:150px"></div>
		<div class=y style="position:absolute;top:100px;left:40px;width:20px;height:20px"></div>`)
	// zagnieżdżenie: rodzic o wysokości auto z obramowaniem kończy się za blokiem, a jego rodzeństwo stoi
	// zaraz za nim; wylewająca się treść sięga trzeciej strony
	same("nested-overflow", `<div style="border:2px solid blue"><div style="height:10px">
		<div class=g style="width:20px;height:250px"></div></div></div>
		<div class=y style="margin-left:40px;width:20px;height:20px"></div>`,
		`<div style="position:absolute;top:0;left:0;width:196px;height:10px;border:2px solid blue"></div>
		<div class=g style="position:absolute;top:2px;left:2px;width:20px;height:250px"></div>
		<div class=y style="position:absolute;top:14px;left:40px;width:20px;height:20px"></div>`)
}
