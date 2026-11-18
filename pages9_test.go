package gopdfgo

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestPageModelPrint sprawdza poprawki modelu strony (CSS Page 3): rozmiary pól marginesowych (§ 5.3),
// kolejność ich rysowania i z-index (§ 3.1), liczniki strony (§ 4.3), size z samą orientacją (§ 7.1),
// page-orientation (§ 7.2), vw na nazwanej pierwszej stronie i element główny z display: none.
func TestPageModelPrint(t *testing.T) {
	ahem, err := os.ReadFile("testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	head := `<style>:root { print-color-adjust: exact } body { margin: 0; font: 10px/1 Ahem } .a { position: absolute }</style>`
	dir := t.TempDir()
	render := func(name, html, userCSS string) [][]byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(head+html), Options{Fonts: fonts, UserCSS: userCSS})
		if err != nil {
			t.Fatal(err)
		}
		return rasterize(t, result.PDF, dir, name)
	}
	same := func(name, test, reference string) {
		t.Helper()
		a, b := render(name, test, ""), render(name+"-ref", reference, "")
		if len(a) != len(b) {
			t.Fatalf("%s: %d stron, wzorzec %d", name, len(a), len(b))
		}
		for i := range a {
			if n := differentPixels(a[i], b[i]); n != 0 {
				t.Errorf("%s: strona %d: %d pikseli różni się od wzorca", name, i+1, n)
			}
		}
	}
	// size of a PPM page, "P6\n<w> <h>\n255\n"
	pageSize := func(ppm []byte) string {
		header, _ := splitPPM(ppm)
		fields := strings.Fields(header)
		return fields[1] + "x" + fields[2]
	}
	refPage := `<style>@page { size: 300px 200px; margin: 0 }</style>`

	// trzy puste pola (content: "") dzielą krawędź po równo, a pole środkowe auto obok pól o ustalonej
	// szerokości bierze resztę krawędzi
	same("empty-boxes-split", `<style>@page { size: 300px 200px; margin: 50px 45px;
		@top-left { content: ""; background: blue } @top-center { content: ""; background: lime }
		@top-right { content: ""; background: red }
		@bottom-left { content: ""; width: 20px; background: blue } @bottom-center { content: ""; background: lime }
		@bottom-right { content: ""; width: 20px; background: red } }</style>`,
		refPage+`<div class=a style="left:45px;width:70px;height:50px;background:blue"></div>
		<div class=a style="left:115px;width:70px;height:50px;background:lime"></div>
		<div class=a style="left:185px;width:70px;height:50px;background:red"></div>
		<div class=a style="top:150px;left:45px;width:20px;height:50px;background:blue"></div>
		<div class=a style="top:150px;left:65px;width:170px;height:50px;background:lime"></div>
		<div class=a style="top:150px;left:235px;width:20px;height:50px;background:red"></div>`)
	// procent w margin-top pola górnego liczy się od wysokości marginesu strony (50px), nie od szerokości
	same("vertical-percent-margin", `<style>@page { size: 300px 200px; margin: 50px;
		@top-left { content: ""; margin-top: 20%; background: blue } }</style>`,
		refPage+`<div class=a style="top:10px;left:50px;width:200px;height:40px;background:blue"></div>`)
	// height: max-content pola górnego to wysokość jego treści, a margin-bottom: auto bierze resztę
	same("intrinsic-height", `<style>@page { size: 300px 200px; margin: 50px;
		@top-left { content: "x"; height: max-content; margin-bottom: auto; background: blue; color: blue;
		font: 10px/1 Ahem } }</style>`,
		refPage+`<div class=a style="left:50px;width:200px;height:10px;background:blue"></div>`)
	// pole o ujemnym z-index leży pod płótnem dokumentu (tłem elementu głównego, które wypełnia obszar strony)
	same("negative-z-index", `<style>@page { size: 300px 200px; margin: 50px;
		@top-left-corner { content: ""; width: 100px; height: 100px; margin-right: -50px; margin-bottom: -50px;
		background: red; z-index: -1 } } html { background: lime }</style>`,
		refPage+`<div class=a style="width:100px;height:100px;background:red"></div>
		<div class=a style="top:50px;left:50px;width:200px;height:100px;background:lime"></div>`)
	// pola rysowane zgodnie z ruchem wskazówek zegara: top-left-corner pod top-left
	same("paint-order", `<style>@page { size: 300px 200px; margin: 50px;
		@top-left-corner { content: ""; background: red }
		@top-left { content: ""; margin-left: -50px; background: blue } }</style>`,
		refPage+`<div class=a style="width:250px;height:50px;background:blue"></div>`)
	// counter-reset strony na liczniku dokumentu obowiązuje tylko na swojej stronie
	same("page-local-counter-reset", `<style>html { counter-reset: n 10 } @page { size: 300px 200px; margin: 50px;
		counter-increment: n; @top-left { content: counter(n); vertical-align: top } }
		@page :left { counter-reset: n 5 }</style>
		<div>a</div><div style="break-before:page">b</div><div style="break-before:page">c</div>`,
		`<style>@page { size: 300px 200px; margin: 50px; @top-left { vertical-align: top } }
		@page :nth(1) { @top-left { content: "11" } } @page :nth(2) { @top-left { content: "6" } }
		@page :nth(3) { @top-left { content: "12" } }</style>
		<div>a</div><div style="break-before:page">b</div><div style="break-before:page">c</div>`)
	// element główny z display: none daje pustą stronę, bez tła i pól @page
	same("root-display-none", `<style>@page { size: 300px 200px; background: red; @top-left { content: "x" } }
		html { display: none }</style>x`, refPage)
	// vw na nazwanej pierwszej stronie odnosi się do jej obszaru
	same("vw-first-named-page", `<style>@page { size: 300px 200px; margin: 0 } @page small { size: 100px }</style>
		<div style="page: small">x</div><div style="width:100vw;height:100vh;background:blue"></div>`,
		`<style>@page { size: 300px 200px; margin: 0 } @page :first { size: 100px }</style>
		<div>x</div><div style="break-before:page;width:100px;height:100px;background:blue"></div>`)

	// size: landscape i portrait obracają rozmiar arkusza z arkusza użytkownika, a nie A4
	user := "@page { size: 480px 288px; margin: 0 }"
	if got := pageSize(render("portrait", `<style>@page { size: portrait }</style>x`, user)[0]); got != "288x480" {
		t.Errorf("size: portrait przy arkuszu 480x288: strona %s, oczekiwano 288x480", got)
	}
	if got := pageSize(render("landscape", `<style>@page { size: landscape }</style>x`, user)[0]); got != "480x288" {
		t.Errorf("size: landscape przy arkuszu 480x288: strona %s, oczekiwano 480x288", got)
	}
	// page-orientation: rotate-right obraca stronę w PDF, ułożenie się nie zmienia
	rotated := render("rotate", `<style>@page { size: 300px 200px; margin: 0; page-orientation: rotate-right }</style>
		<div style="width:300px;height:20px;background:blue"></div>`, "")
	if got := pageSize(rotated[0]); got != "200x300" {
		t.Errorf("page-orientation: rotate-right: strona %s, oczekiwano 200x300", got)
	}
}
