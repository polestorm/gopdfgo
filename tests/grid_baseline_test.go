package tests

import (
	"context"
	"github.com/polestorm/gopdfgo/htmlpdf"
	"os"
	"testing"
)

// TestGridBaselineAlignment sprawdza wyrównanie do linii bazowej w siatce (CSS Grid 1 § 10.6, § 10.7,
// § 11.5; CSS Align 3 § 9) i poprawki znalezione przy okazji testów WPT tej grupy. Font Ahem: przy
// font: 20px/1 linia bazowa leży 16 px pod górą wiersza tekstu.
func TestGridBaselineAlignment(t *testing.T) {
	ahem, err := os.ReadFile("../testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]htmlpdf.Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0; font: 20px/1 Ahem; color: transparent }
		.g { background: green } .b { background: blue } .a { position: absolute }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := htmlpdf.Render(context.Background(), []byte(head+body), htmlpdf.Options{Fonts: fonts})
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

	// last baseline: ostatnie linie bazowe na wspólnej linii, grupa przy dolnej krawędzi wiersza; wiersz
	// rośnie o odsunięcie (ostatnia linia bazowa 4 px nad dołem pierwszego elementu, 8 px nad dołem drugiego)
	same("last-baseline", `<div style="display:grid;grid-template-columns:40px 40px;align-items:last baseline">
		<div class=g>X<br>X</div><div class=b style="font-size:40px">X</div></div>`,
		`<div class=g style="width:40px;height:40px"></div><div class="b a" style="top:4px;left:40px;width:40px;height:40px"></div>`)
	// element rozpięty na dwa wiersze uczestniczy w grupie swojego pierwszego wiersza; wkład przesuniętego
	// elementu z odsunięciem wchodzi do rozmiaru wiersza, więc siatka nie rośnie ponad wysokość rozpiętego
	same("spanning-item-baseline", `<div style="display:grid;grid-template-columns:40px 40px;align-items:baseline">
		<div class=g>X</div><div class=b style="grid-area:1/2/span 2;font-size:40px">X<br>X</div></div>
		<div class=g style="width:10px;height:10px"></div>`,
		`<div class="g a" style="top:16px;width:40px;height:20px"></div><div class="b a" style="left:40px;width:40px;height:80px"></div>
		<div class="g a" style="top:80px;width:10px;height:10px"></div>`)
	// linia bazowa kontenera: z pierwszego elementu pierwszego wiersza w porządku siatki (kolumna 1),
	// nie z pierwszego dziecka w dokumencie
	same("container-baseline-grid-order", `<div><div style="display:inline-grid;grid-template-columns:40px 40px;align-items:start">
		<div class=b style="grid-area:1/2;font-size:40px">X</div><div class=g style="grid-area:1/1">X</div></div><span class=b
		style="display:inline-block;width:20px;height:20px"></span></div>`,
		`<div class="g a" style="top:4px;width:40px;height:20px"></div><div class="b a" style="top:4px;left:40px;width:40px;height:40px"></div>
		<div class="b a" style="left:80px;width:20px;height:20px"></div>`)
	// pusty inline-grid stoi na dolnej krawędzi marginesu (8 px pod obramowaniem)
	same("empty-inline-grid", `<div><div class=g style="display:inline-grid;width:30px;height:30px;margin:8px 0"></div><span class=b
		style="display:inline-block;width:20px;height:20px"></span></div>`,
		`<div class="g a" style="top:8px;width:30px;height:30px"></div><div class="b a" style="top:26px;left:30px;width:20px;height:20px"></div>`)
	// element z wysokością procentową w wierszu auto nie uczestniczy w wyrównaniu (cykl), stoi na początku
	same("percent-height-fallback", `<div style="display:grid;grid-template:auto / 50px 50px;align-items:baseline;width:100px">
		<canvas class=g style="width:100%;height:100%"></canvas><div class=g style="height:100px"><br></div></div>`,
		`<div class=g style="width:100px;height:100px"></div>`)
	// align-content: baseline przesuwa treść rozciągniętego bloku, nie sam blok
	same("align-content-baseline", `<div style="display:grid;grid-template-columns:40px 40px">
		<div style="align-content:baseline;border-top:2px solid blue"><span class=g style="color:green">X</span></div>
		<div class=b style="align-self:baseline;font-size:40px">X</div></div>`,
		`<div class="b a" style="width:40px;height:2px"></div><div class="a" style="top:16px;color:green">X</div>
		<div class="b a" style="left:40px;width:40px;height:40px"></div>`)
	// rozciąganie elementu respektuje max-width
	same("stretch-max-width", `<div style="display:grid"><div class=g style="max-width:50px;height:20px"></div></div>`,
		`<div class=g style="width:50px;height:20px"></div>`)
	// tabela jako element siatki też jest rozciągana do obszaru
	same("table-item-stretch", `<div style="display:grid;grid-template:60px / 60px"><div style="display:table;border:3px solid blue"></div></div>`,
		`<div style="width:54px;height:54px;border:3px solid blue"></div>`)
	// pudełko pozycjonowane z align-self: baseline między top i bottom ma wysokość treści
	same("abspos-align-self", `<div style="display:grid;position:relative;width:100px;height:100px">
		<div class=g style="position:absolute;top:10px;bottom:0;left:0;width:20px;align-self:baseline"><div style="height:30px"></div></div></div>`,
		`<div class="g a" style="top:10px;width:20px;height:30px"></div>`)
	// inline-block z kontenerem flex bez linii bazowej: linia syntetyzowana z dołu obramowania elementu flex
	same("flex-synthesized-baseline", `<div><span style="display:inline-block"><span style="display:flex;padding-bottom:10px">
		<span class=g style="width:20px;height:20px"></span></span></span><span class=b style="display:inline-block;width:10px;height:10px"></span></div>`,
		`<div class="g a" style="width:20px;height:20px"></div><div class="b a" style="top:10px;left:20px;width:10px;height:10px"></div>`)
}
