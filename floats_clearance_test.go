package gopdfgo

import (
	"context"
	"testing"
)

// TestFloatsAndClearance sprawdza elementy pływające i clearance (CSS 2.1 § 9.5, § 9.5.1, § 9.5.2, § 8.3.1):
// przylegające elementy pływające przesuwane razem ze zwiniętym marginesem rodzica, clearance wymuszone
// przez nie, oddzielanie marginesu pudełka z własnym kontekstem formatowania, regułę 3 i elementy
// pływające o zerowej szerokości. Każdy przypadek porównuje stronę z wzorcem zbudowanym bez elementów
// pływających.
func TestFloatsAndClearance(t *testing.T) {
	page := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 }</style>`
	dir := t.TempDir()
	render := func(name, html string) []byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(html), Options{Fonts: testFonts(t)})
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
		if n := differentPixels(render(name, test), render(name+"-ref", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	square := page + `<div style="width:100px;height:100px;background:green"></div>`

	// element pływający w pustym bloku przylega do górnej krawędzi rodzica, więc schodzi razem z marginesem
	// następnego dziecka zwiniętym z marginesem rodzica; pudełko z własnym kontekstem staje obok niego
	same("adjoining-float-moves-with-margin", page+`<div style="overflow:hidden;width:100px;background:green">
		<div style="width:150px;margin-top:30px;background:red"><div><div style="float:left;width:100px;height:10px;background:green"></div></div>
		<div style="overflow:hidden;width:50px;height:10px;margin-top:90px;background:red"></div></div></div>`,
		square)
	// clear przy przylegającym elemencie pływającym: krawędź pudełka staje pod nim bez względu na margines
	same("forced-clearance", page+`<div style="width:100px">
		<div><div style="float:left;width:100px;height:50px;background:green"></div></div>
		<div style="clear:left;margin-top:400px;height:50px;background:green"></div></div>`,
		square)
	// to samo, gdy duży margines należy do pierwszego dziecka pudełka z clear
	same("forced-clearance-child-margin", page+`<div style="width:100px">
		<div style="float:left;width:100px;height:50px;background:green"></div>
		<div style="clear:left"><div style="margin-top:300px;height:50px;background:green"></div></div></div>`,
		square)
	// pudełko z własnym kontekstem, które nie mieści się obok przylegającego elementu pływającego,
	// oddziela od niego swój margines i staje pod nim
	same("new-fc-separates-margin", page+`<div style="overflow:hidden;width:100px;height:100px">
		<div><div><div style="float:left;width:100px;height:50px;background:green"></div></div>
		<div style="overflow:hidden;width:100px;height:50px;margin-top:500px;background:green"></div></div></div>`,
		square)
	// margines potomka z clear, który mija elementy pływające, zwija się z marginesami przodków; puste
	// pudełka z tłem nie zostawiają linii
	same("no-clearance-nested-margin", page+`<div style="float:left;width:50px;height:50px"></div>
		<div style="float:right;width:50px;height:100px"></div>
		<div style="width:100px;height:100px;background:green"><div style="height:15px;margin-bottom:20px"></div>
		<div style="background:red"><div style="clear:left;background:red"><div style="clear:right;background:red">
		<div style="margin-top:185px;background:red"></div></div></div></div></div>`,
		square)
	// reguła 3: element pływający szerszy niż blok zawierający zostaje obok elementu pływającego przeciwnej
	// strony, dopóki do niego nie sięga
	same("float-rule3-overflowing", page+`<div style="width:200px">
		<div style="float:right;width:50px;height:100px"></div>
		<div style="margin-right:100px"><div style="float:left;width:140px;height:20px;background:green"></div></div></div>`,
		page+`<div style="width:140px;height:20px;background:green"></div>`)
	// element pływający o zerowej szerokości nie zwęża pudełka z własnym kontekstem
	same("zero-width-float", page+`<div style="margin-left:50px;width:100px">
		<div style="float:left;width:0;height:50px"></div>
		<div style="overflow:hidden;margin:0 -50px;height:100px;background:green"></div></div>`,
		page+`<div style="width:200px;height:100px;background:green"></div>`)
}
