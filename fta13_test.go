package gopdfgo

import (
	"context"
	"testing"
)

// TestPrintLayoutFTA13 sprawdza poprawki druku z rundy fta13 (pudełka pozycjonowane, elementy pływające,
// tabele; CSS Fragmentation 3, CSS Position 3, CSS 2.1 § 9.5): test i wzorzec renderowane na stronach
// 200×100 px muszą dać tyle samo stron i te same piksele.
func TestPrintLayoutFTA13(t *testing.T) {
	head := `<style>@page { size: 200px 100px; margin: 0 } body { margin: 0 } :root { print-color-adjust: exact }
		.y { background: yellow } .b { background: blue } .g { background: green } .h { background: rgba(0, 0, 255, 0.5) }
		.i { display: inline-block; width: 10px; height: 20px }</style>`
	dir := t.TempDir()
	pages := func(name, body string) [][]byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(head+body), Options{Fonts: testFonts(t)})
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

	// pudełko absolute w wierszu, który przechodzi na następną stronę, jest rysowane raz: wiersze liczone
	// dla widows nie zostawiają zastępnika na bieżącej stronie (fixedpos-in-footer-forced-break-print-ref);
	// półprzezroczyste tło pokazałoby podwójne rysowanie
	same("abspos-in-pushed-line-once", `<div style="height:100px"></div><span class=i></span><br><span class=i></span>
		<div class=h style="position:absolute;bottom:0;width:20px;height:20px"></div>
		<div class=h style="position:absolute;bottom:-100vh;left:50px;width:20px;height:20px"></div>`,
		`<div class=h style="position:absolute;top:80px;width:20px;height:20px"></div>
		<div class=h style="position:absolute;top:180px;left:50px;width:20px;height:20px"></div>
		<div style="height:100px"></div><span class=i></span><br><span class=i></span>`)
}
