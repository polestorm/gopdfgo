package tests

import (
	"context"
	"testing"

	"github.com/polestorm/gopdfgo"
)

// TestTransforms3DAndRelativeColors sprawdza spłaszczanie funkcji 3D (CSS Transforms 2 § 6, § 12),
// backface-visibility (§ 11), kontekst preserve-3d bez perspektywy, pomijaną perspektywę, składnię
// względną barw (CSS Color 5 § 4), color() (CSS Color 4 § 10) i color-mix() w innych przestrzeniach.
// Każdy przypadek porównuje obraz z równoważnym zapisem 2D albo sRGB.
func TestTransforms3DAndRelativeColors(t *testing.T) {
	head := `<style>@page { size: 200px 200px; margin: 0 } body { margin: 0 }
		.b { width: 100px; height: 60px; background: green; margin: 40px }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := gopdfgo.Render(context.Background(), []byte(head+body), gopdfgo.Options{Fonts: testFonts(t)})
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

	// funkcje 3D spłaszczone do części 2D macierzy (rzut prostokątny)
	same("rotateZ", `<div class=b style="transform: rotateZ(30deg)"></div>`,
		`<div class=b style="transform: rotate(30deg)"></div>`)
	same("rotate3d-z", `<div class=b style="transform: rotate3d(0, 0, 2, 30deg)"></div>`,
		`<div class=b style="transform: rotate(30deg)"></div>`)
	same("translate3d", `<div class=b style="transform: translate3d(20px, 10px, 50px)"></div>`,
		`<div class=b style="transform: translate(20px, 10px)"></div>`)
	same("translateZ-scaleZ", `<div class=b style="transform: translateZ(40px) scaleZ(3)"></div>`,
		`<div class=b></div>`)
	same("rotateY", `<div class=b style="transform: rotateY(60deg)"></div>`,
		`<div class=b style="transform: scaleX(0.5)"></div>`)
	same("rotateX", `<div class=b style="transform: rotateX(60deg)"></div>`,
		`<div class=b style="transform: scaleY(0.5)"></div>`)
	same("matrix3d", `<div class=b style="transform: matrix3d(1,0.2,0,0, 0.3,1,0,0, 0,0,1,0, 10,5,7,1)"></div>`,
		`<div class=b style="transform: matrix(1, 0.2, 0.3, 1, 10, 5)"></div>`)
	same("scale3d", `<div class=b style="transform: scale3d(50%, 2, 4)"></div>`,
		`<div class=b style="transform: scale(0.5, 2)"></div>`)
	// łańcuch składany w 4×4 i spłaszczany raz: rotateX(60deg) rotateY(60deg) ma składową b = sin²60° =
	// 0,75, której nie dałoby spłaszczenie każdej funkcji z osobna (scaleY(0.5) scaleX(0.5))
	same("chain-3d", `<div class=b style="transform: rotateX(60deg) rotateY(60deg); transform-origin: 0 0"></div>`,
		`<div class=b style="transform: matrix(0.5, 0.75, 0, 0.5, 0, 0); transform-origin: 0 0"></div>`)
	// z w transform-origin: obrót wokół osi przesuniętej o 20 px w głąb przesuwa rzut o 20·sin 60°
	same("origin-z", `<div class=b style="transform: rotateY(60deg); transform-origin: 0 0 -20px"></div>`,
		`<div class=b style="transform: matrix(0.5, 0, 0, 1, 17.320508, 0); transform-origin: 0 0"></div>`)
	// właściwość rotate z osią w przestrzeni składa się z transform w 4×4
	same("rotate-property-3d", `<div class=b style="rotate: y 30deg; transform: rotateY(30deg)"></div>`,
		`<div class=b style="transform: scaleX(0.5)"></div>`)
	// kąt zerowy nie unieważnia deklaracji: transform tworzy blok zawierający dla fixed
	same("zero-angle-containing-block", `<div style="transform: rotate(0deg); margin: 30px"><div style="position: fixed; top: 10px; left: 10px; width: 20px; height: 20px; background: green"></div></div>`,
		`<div style="position: absolute; top: 40px; left: 40px; width: 20px; height: 20px; background: green"></div>`)

	// backface-visibility: hidden ukrywa element odwrócony tyłem
	same("backface-hidden", `<div class=b style="transform: rotateX(180deg); backface-visibility: hidden"></div>`,
		`<div class=b style="background: none"></div>`)
	same("backface-visible", `<div class=b style="transform: rotateX(180deg)"></div>`,
		`<div class=b></div>`)
	// odbicie 2D (scaleX(-1)) nie pokazuje tylnej strony
	same("backface-2d-flip", `<div class=b style="transform: scaleX(-1); backface-visibility: hidden"></div>`,
		`<div class=b></div>`)

	// preserve-3d bez perspektywy: dwa obroty rotateX(30deg) dają rotateX(60deg), a obrót rodzica o 90°
	// (płaszczyzna widziana z boku) nie ukrywa dziecka obróconego dalej
	same("preserve-3d-chain", `<div style="transform: rotateX(30deg); transform-style: preserve-3d; width: 100px; margin: 40px">
		<div style="transform: rotateX(30deg); height: 60px; background: green"></div></div>`,
		`<div class=b style="transform: scaleY(0.5)"></div>`)
	same("preserve-3d-edge-on-parent", `<div style="transform: rotateX(90deg); transform-style: preserve-3d; width: 100px; height: 60px; margin: 40px; background: red">
		<div style="transform: rotateX(90deg); height: 60px; background: green"></div></div>`,
		`<div class=b style="transform: scaleY(-1)"></div>`)
	// odwracana karta: przód bez własnego przekształcenia znika, tył obrócony o 180° jest widoczny wprost
	same("card-flip", `<div style="transform: rotateY(180deg); transform-style: preserve-3d; width: 100px; height: 60px; margin: 40px; position: relative">
		<div style="position: absolute; inset: 0; background: red; backface-visibility: hidden"></div>
		<div style="position: absolute; inset: 0; background: green; backface-visibility: hidden; transform: rotateY(180deg)"></div></div>`,
		`<div class=b></div>`)

	// perspektywa pomijana (rzut prostokątny), deklaracje przyjęte
	same("perspective-function", `<div class=b style="transform: perspective(300px) rotateZ(30deg)"></div>`,
		`<div class=b style="transform: rotate(30deg)"></div>`)
	same("perspective-property", `<div style="perspective: 300px; perspective-origin: left"><div class=b style="transform: rotateY(60deg)"></div></div>`,
		`<div class=b style="transform: scaleX(0.5)"></div>`)

	// składnia względna barw i color()
	sq := func(color string) string {
		return `<div style="width: 100px; height: 100px; background: ` + color + `"></div>`
	}
	same("rgb-from", sq("rgb(from red r g 255)"), sq("rgb(255 0 255)"))
	same("rgb-from-calc", sq("rgb(from rgb(200 100 50) calc(r / 2) calc(g + 50) b / calc(alpha * 0.5))"), sq("rgb(100 150 50 / 0.5)"))
	same("hsl-from-hue", sq("hsl(from blue calc(h + 120) s l)"), sq("rgb(255 0 0)"))
	same("hwb-from", sq("hwb(from green h w b)"), sq("green"))
	same("lab-from", sq("lab(from green l a b)"), sq("green"))
	same("oklch-from", sq("oklch(from green l c h / 50%)"), sq("rgb(0 128 0 / 0.5)"))
	same("color-from", sq("color(from green srgb r g b)"), sq("green"))
	same("color-from-xyz", sq("color(from green xyz-d65 x y z)"), sq("green"))
	same("relative-nested", sq("rgb(from rgb(from blue b g r) r g b)"), sq("red"))
	same("color-srgb", sq("color(srgb 0 0.5 0)"), sq("rgb(0 127.5 0)"))
	same("color-srgb-percent-none", sq("color(srgb 100% none 0% / 50%)"), sq("rgb(255 0 0 / 0.5)"))
	// barwa spoza gamy sRGB przycinana kanał po kanale (czerwień P3 to czerwień sRGB)
	same("color-display-p3", sq("color(display-p3 1 0 0)"), sq("rgb(255 0 0)"))
	same("color-rec2020", sq("color(rec2020 0.1264752 0.1264752 0.1264752)"), sq("#141414"))
	same("color-prophoto", sq("color(prophoto-rgb 0.2861 0.49131 0.16133)"), sq("#009900"))
	same("color-a98", sq("color(a98-rgb 0.281363 0.498012 0.116746)"), sq("#009900"))
	same("calc-in-rgb", sq("rgb(calc(255 / 5) 0 0)"), sq("rgb(51 0 0)"))
	same("rebeccapurple", sq("rebeccapurple"), sq("#663399"))
	// currentColor jako barwa wyjściowa: wartość obliczona zachowuje funkcję, dziedziczy ją dziecko
	same("relative-currentcolor-inherit", `<div style="color: red; background: rgb(from currentColor r g b); width: 100px">
		<div style="color: green; background: inherit; width: 100px; height: 100px"></div></div>`, sq("green"))
	same("relative-currentcolor-color", `<div style="color: rgb(0 128 0)"><div style="color: rgb(from currentColor b g r); background: currentColor; width: 100px; height: 100px"></div></div>`,
		sq("rgb(0 128 0)"))
	same("color-mix-currentcolor", `<div style="color: blue; background: color-mix(in srgb, currentColor, red); width: 100px; height: 100px"></div>`,
		sq("rgb(127.5 0 127.5)"))
	same("color-mix-lch", sq("color-mix(in lch, green, green)"), sq("green"))
	same("color-mix-hsl-longer", sq("color-mix(in hsl longer hue, hsl(0 100% 50%), hsl(120 100% 50%))"), sq("hsl(240 100% 50%)"))
}
