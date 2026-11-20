package tests

import (
	"context"
	"github.com/polestorm/gopdfgo"
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
		result, err := gopdfgo.Render(context.Background(), []byte(html), gopdfgo.Options{Fonts: testFonts(t)})
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
	// pudełko z własnym kontekstem, które obok górnego elementu pływającego byłoby za szerokie dla
	// niższego, jest układane ponownie w węższym pasie i zostaje na górze
	same("bfc-relayout-beside-floats", page+`<div style="width:300px">
		<div style="float:left;clear:left;width:100px;height:25px"></div>
		<div style="float:left;clear:left;width:200px;height:25px"></div>
		<div style="overflow:hidden;background:green"><div style="float:left;width:100px;height:50px"></div>
		<div style="float:left;width:100px;height:50px"></div></div></div>`,
		page+`<div style="margin-left:200px;width:100px;height:100px;background:green"></div>`)
	// tabela o szerokości auto zwęża się do pasa obok obu elementów pływających
	same("table-among-floats", page+`<div style="width:200px;overflow:hidden">
		<div style="width:75px;height:100px;float:left;clear:left;background:green"></div>
		<div style="width:125px;height:100px;float:left;clear:left;background:green"></div>
		<table cellpadding="0" cellspacing="0"><td><div style="float:left;width:75px;height:100px;background:green"></div>
		<div style="float:left;width:75px;height:100px;background:green"></div></td></table>
		<div style="position:absolute;left:75px;top:0;width:50px;height:100px;background:green"></div></div>`,
		page+`<div style="width:200px;height:200px;background:green"></div>`)
	// margines auto pudełka z własnym kontekstem dzieli pas obok elementu pływającego
	same("auto-margin-beside-float", page+`<div style="width:300px"><div style="float:right;width:100px;height:50px"></div>
		<div style="overflow:hidden;width:100px;height:50px;margin-left:auto;background:green"></div></div>`,
		page+`<div style="margin-left:100px;width:100px;height:50px;background:green"></div>`)
	// element pływający w zagnieżdżonym pudełku liniowym z odstępami stoi przy krawędzi bloku
	same("float-in-nested-inline", page+`<span style="padding:30px;margin:40px"><span>
		<span style="float:left;width:100px;height:100px;background:green"></span></span></span>`,
		page+`<div style="width:100px;height:100px;background:green"></div>`)
	// wiersz z samym <br> mieści się obok elementu pływającego; dalsza treść schodzi pod niego
	same("br-line-beside-float", page+`<div style="width:100px;line-height:0">
		<div style="float:left;width:50px;height:50px;background:green"></div><br>
		<span style="display:inline-block;vertical-align:top;width:100px;height:20px;background:blue"></span></div>`,
		page+`<div style="width:50px;height:50px;background:green"></div><div style="width:100px;height:20px;background:blue"></div>`)
	// clear podpisu tabeli nie widzi elementów pływających spoza tabeli
	same("caption-clear-inside-wrapper", page+`<div style="float:left;width:100px;height:50px"></div>
		<div style="display:table-caption;clear:both;width:50px;height:50px;background:green"></div>`,
		page+`<div style="margin-left:100px;width:50px;height:50px;background:green"></div>`)
	// element pływający szerszy niż pusty wiersz stoi na jego wysokości, a treść schodzi pod niego
	same("wide-float-in-empty-line", page+`<div style="width:40px;line-height:0"><span>
		<div style="float:left;width:100px;height:20px;background:green"></div><span style="display:inline-block;vertical-align:top;width:20px;height:20px;background:blue"></span></span></div>`,
		page+`<div style="width:100px;height:20px;background:green"></div><div style="width:20px;height:20px;background:blue"></div>`)
	// element pływający o zerowej wysokości stoi tam, gdzie wypada, i skraca wiersz obejmujący jego położenie
	same("zero-height-float", page+`<div style="width:200px;line-height:0">
		<div style="float:left;width:10px;height:30px"></div><div style="float:left;clear:left;width:100px;height:0"></div>
		<span style="display:inline-block;vertical-align:bottom;height:20px;width:150px;background:blue"></span>
		<span style="display:inline-block;vertical-align:bottom;height:20px;width:100px;background:green"></span></div>`,
		page+`<div style="margin-left:10px;width:150px;height:20px;background:blue"></div><div style="margin-left:100px;width:100px;height:20px;background:green"></div>`)
}
