package tests

import (
	"fmt"
	"testing"
)

// Bloki zawierające z transform (CSS Transforms 1 § 3), clip-path (CSS Masking 1 § 5.1),
// background-blend-mode (CSS Compositing 1 § 3.2) i równoważenie kolumn z elementem pływającym
// break-inside: avoid. Każdy przypadek porównuje układ z wzorcem z bloków pozycjonowanych.
func TestMulticolBackgroundsRest(t *testing.T) {
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 }</style>`
	dir := t.TempDir()
	// tolerance: liczba pikseli, które mogą się różnić (wygładzanie krawędzi grupy przezroczystości)
	tolerance := 0
	same := func(name, test, reference string) {
		t.Helper()
		got := rasterize(t, renderString(t, head+test).PDF, dir, name)
		want := rasterize(t, renderString(t, head+reference).PDF, dir, name+"-ref")
		if len(got) != len(want) {
			t.Errorf("%s: %d stron, wzorzec ma %d", name, len(got), len(want))
			return
		}
		for i := range got {
			if n := differentPixels(got[i], want[i]); n < 0 || n > tolerance {
				t.Errorf("%s, strona %d: %d pikseli różni się od wzorca", name, i+1, n)
			}
		}
	}
	block := func(left, top, width, height int, color string) string {
		return fmt.Sprintf(`<div style="position:absolute;left:%dpx;top:%dpx;width:%dpx;height:%dpx;background:%s"></div>`,
			left, top, width, height, color)
	}

	// Pudełko przekształcone jest blokiem zawierającym dla potomka position: fixed, więc top i left
	// liczą się od niego, nie od strony.
	same("transform-contains-fixed", `<div style="transform:translateX(0);margin:50px 0 0 30px;height:100px">`+
		`<div style="position:fixed;top:10px;left:20px;width:50px;height:50px;background:green"></div></div>`,
		block(50, 60, 50, 50, "green"))

	// To samo dla potomka position: absolute bez przodka pozycjonowanego.
	same("transform-contains-absolute", `<div style="transform:translateX(0);margin:50px 0 0 30px;height:100px">`+
		`<div><div style="position:absolute;top:10px;left:20px;width:50px;height:50px;background:green"></div></div></div>`,
		block(50, 60, 50, 50, "green"))

	// Kontener kolumn z transform: potomek fixed w zagnieżdżonych kolumnach leży względem kontenera.
	same("transform-multicol-contains-fixed", `<div style="columns:2;column-fill:auto;column-gap:0;width:100px;height:100px;margin-top:100px;transform:translateX(0)">`+
		`<div style="position:relative"><div style="position:absolute"><div style="position:fixed;top:-100px;left:0;width:100px;height:100px;background:green"></div></div></div></div>`,
		block(0, 0, 100, 100, "green"))

	// clip-path: inset() przycina pudełko i jego potomków do prostokąta wewnątrz obramowania. Prawa
	// krawędź przycięcia leży na krawędzi treści: pdftoppm wygładza krawędź ścieżki przycięcia o piksel
	// na zewnątrz, czego wzorzec z bloków nie ma.
	same("clip-path-inset", `<div style="width:100px;height:100px;background:green;clip-path:inset(10px 0 30px 40px)">`+
		`<div style="height:20px;background:blue"></div></div>`,
		block(40, 10, 60, 10, "blue")+block(40, 20, 60, 50, "green"))

	// clip-path: polygon() z procentami pudełka odniesienia (content-box).
	same("clip-path-polygon-content-box", `<div style="width:100px;height:80px;padding:10px;background:green;clip-path:polygon(0 0, 50% 0, 50% 100%, 0 100%) content-box"></div>`,
		block(10, 10, 50, 80, "green"))

	// Wartość niepoprawna (url() nie jest obsługiwane jako kształt podstawowy) nie przycina.
	same("clip-path-invalid", `<div style="width:100px;height:100px;background:green;clip-path:circle(10px) foo"></div>`,
		block(0, 0, 100, 100, "green"))

	// background-blend-mode: difference czerwieni nad żółtym daje zieleń (0, 255, 0); warstwa miesza się
	// tylko z tłem elementu, nie z tym, co leży pod nim.
	// Grupa przezroczystości wygładza swoją krawędź inaczej niż samo tło: dopuszczamy kilka pikseli.
	// Gradient jest rysowany cieniowaniem (sh) w przycięciu, a pdftoppm maluje je w grupie o kolumnę
	// pikseli za prawą krawędź przycięcia (Quick Look nie): dopuszczamy jedną kolumnę wysokości 50px.
	tolerance = 54
	same("background-blend-difference", `<div style="background:blue;padding:10px;width:100px"><div style="width:100px;height:50px;`+
		`background:linear-gradient(red, red), yellow;background-blend-mode:difference"></div></div>`,
		`<div style="background:blue;padding:10px;width:100px"><div style="width:100px;height:50px;background:linear-gradient(lime, lime)"></div></div>`)

	tolerance = 0

	// Równoważenie kolumn nie dzieli elementu pływającego z break-inside: avoid: kolumny mają jego wysokość.
	same("balance-float-avoid", `<div style="width:100px;height:100px;background:red"><div style="columns:3"><span></span>`+
		`<div style="float:left;break-inside:avoid;width:100px;height:100px;background:green"></div></div></div>`,
		block(0, 0, 100, 100, "green"))

	// Blok o podanej wysokości z potomkiem pozycjonowanym dzieli się między kolumny razem z nim.
	same("split-containing-block-of-absolute", `<div style="columns:2;column-gap:0;width:200px"><div style="position:relative;height:200px">`+
		`<div style="overflow:hidden"><div style="position:absolute;width:100%;height:200px;background:green"></div></div></div></div>`,
		block(0, 0, 200, 100, "green"))
}
