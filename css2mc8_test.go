package gopdfgo

import (
	"fmt"
	"testing"
)

// Elementy pływające dzielone między kolumny i clear (CSS 2.1 § 9.5, CSS Multicol 1, CSS Break 3 § 4.4).
// Każdy przypadek porównuje układ z wzorcem z bloków pozycjonowanych.
func TestCSS2MC8FloatsAcrossColumns(t *testing.T) {
	head := `<style>@page { size: 400px 300px; margin: 0 } body { margin: 0 }
		.mc { width: 300px; column-width: 100px; column-gap: 0; column-fill: auto; height: 100px }
		.f { width: 15px; height: 250px; background: aqua }</style>`
	dir := t.TempDir()
	same := func(name, test, reference string) {
		t.Helper()
		got := rasterize(t, renderString(t, head+test).PDF, dir, name)
		want := rasterize(t, renderString(t, head+reference).PDF, dir, name+"-ref")
		if len(got) != len(want) {
			t.Errorf("%s: %d stron, wzorzec ma %d", name, len(got), len(want))
			return
		}
		for i := range got {
			if n := differentPixels(got[i], want[i]); n != 0 {
				t.Errorf("%s, strona %d: %d pikseli różni się od wzorca", name, i+1, n)
			}
		}
	}
	block := func(left, top, width, height int, color string) string {
		return fmt.Sprintf(`<div style="position:absolute;left:%dpx;top:%dpx;width:%dpx;height:%dpx;background:%s"></div>`,
			left, top, width, height, color)
	}
	floats := block(0, 0, 15, 100, "aqua") + block(85, 0, 15, 100, "aqua") +
		block(100, 0, 15, 100, "aqua") + block(185, 0, 15, 100, "aqua") +
		block(200, 0, 15, 50, "aqua") + block(285, 0, 15, 50, "aqua")

	// Dalszy ciąg pływającego w prawo stoi obok dalszego ciągu pływającego w lewo w każdej kolumnie,
	// a nie pod nim; blok po <br clear="all"> zaczyna się za nimi w trzeciej kolumnie.
	same("left-and-right-continue", `<div class="mc"><div><div class="f" style="float:left"></div>`+
		`<div class="f" style="float:right"></div></div><div style="border-bottom:3px solid orange"><br clear="all"></div></div>`,
		floats+block(200, 50, 100, 3, "orange"))

	// <br clear="all"> na końcu bloku z elementami pływającymi: blok ciągnie się do trzeciej kolumny,
	// a następny blok stoi za nim.
	same("br-clear-inside-container", `<div class="mc"><div><div class="f" style="float:left"></div>`+
		`<div class="f" style="float:right"></div><br clear="all"></div><div style="border-bottom:3px solid orange"></div></div>`,
		floats+block(200, 50, 100, 3, "orange"))

	// Równoważenie liczy elementy pływające w potomkach kolumny: 250px w trzech kolumnach i 5px za nimi,
	// czyli po 85px.
	same("balancing-counts-nested-floats", `<div class="mc" style="column-fill:balance"><div>`+
		`<div class="f" style="float:left"></div></div><div style="clear:left;border-bottom:5px solid orange"></div></div>`,
		block(0, 0, 15, 85, "aqua")+block(100, 0, 15, 85, "aqua")+block(200, 0, 15, 80, "aqua")+
			block(200, 80, 100, 5, "orange"))
}
