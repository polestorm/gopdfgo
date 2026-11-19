package gopdfgo

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestPageViewportUnits sprawdza jednostki vw i vh w kontekście @page i pól marginesowych: odnoszą się
// do arkusza domyślnego (A4 albo size z arkusza użytkownika), nie do strony, którą same wyznaczają
// (testy WPT css-page/page-size-015..017, margin-boxes/dimensions-015). Wcześniej dawały zero.
// W dokumencie vw i vh dalej odnoszą się do obszaru pierwszej strony.
func TestPageViewportUnits(t *testing.T) {
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
	pageSize := func(ppm []byte) string {
		header, _ := splitPPM(ppm)
		fields := strings.Fields(header)
		return fields[1] + "x" + fields[2]
	}
	same := func(name, test, reference, userCSS string) {
		t.Helper()
		a, b := render(name, test, userCSS), render(name+"-ref", reference, "")
		if len(a) != len(b) {
			t.Fatalf("%s: %d stron, wzorzec %d", name, len(a), len(b))
		}
		for i := range a {
			if pageSize(a[i]) != pageSize(b[i]) {
				t.Errorf("%s: strona %d: rozmiar %s, wzorzec %s", name, i+1, pageSize(a[i]), pageSize(b[i]))
			} else if n := differentPixels(a[i], b[i]); n != 0 {
				t.Errorf("%s: strona %d: %d pikseli różni się od wzorca", name, i+1, n)
			}
		}
	}
	user := "@page { size: 400px 300px; margin: 20px }"

	// size w vw i vh względem arkusza z arkusza użytkownika (400x300), nie jego obszaru
	same("size-from-user-sheet", `<style>@page { size: 50vw 100vh; margin: 0 }</style>x`,
		`<style>@page { size: 200px 300px; margin: 0 }</style>x`, user)
	// bez arkusza użytkownika arkuszem domyślnym jest A4 (794x1123 px)
	same("size-default-a4", `<style>@page { size: 25vw 10vh; margin: 0 }</style>x`,
		`<style>@page { size: 198.425px 112.25px; margin: 0 }</style>x`, "")
	// width i height obszaru strony, margines i pole marginesowe w vw; tło dokumentu w vh odnosi się
	// do obszaru pierwszej strony (100vh = 200px)
	same("margin-box", `<style>@page { width: 75vw; height: 50vw; margin: 0; margin-top: 10vh;
		@top-left { content: ""; width: 10vw; margin-bottom: 5vw; background: blue } }
		body { height: 100vh; background: lime }</style>`,
		`<style>@page { size: 300px 230px; margin: 0 }</style>
		<div class=a style="width:40px;height:10px;background:blue"></div>
		<div class=a style="top:30px;width:300px;height:200px;background:lime"></div>`, user)
}
