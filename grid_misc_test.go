package gopdfgo

import (
	"context"
	"fmt"
	"testing"
)

// TestGridMisc sprawdza poprawki układu siatki znalezione przy testach WPT css/css-grid: pozycję
// statyczną i obszar dzieci pozycjonowanych (CSS Grid 1 § 9), minimum automatyczne elementów (§ 6.6),
// rozmiar ułamka fr (§ 11.7.1), linie nazwane (§ 8.3) i procenty elementów siatki (§ 6.5).
func TestGridMisc(t *testing.T) {
	fonts := testFonts(t)
	square := func(size int) string { // obraz SVG z wymiarami wewnętrznymi size × size, zielony
		return fmt.Sprintf(`data:image/svg+xml,%%3Csvg xmlns='http://www.w3.org/2000/svg' width='%d' height='%d'%%3E`+
			`%%3Crect width='%d' height='%d' fill='green'/%%3E%%3C/svg%%3E`, size, size, size, size)
	}
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 }
		.g { background: green } .b { background: blue } .r { background: red } .a { position: absolute }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(head+body), Options{Fonts: fonts})
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

	// siatka bez position nie jest pudełkiem zawierającym: grid-area dziecka pozycjonowanego nic nie
	// znaczy, prostokątem pozycji statycznej są krawędzie treści kontenera (tu wyrównanie do końca)
	same("abspos-static-content-box", `<div style="display:grid;grid:10px 40px / 10px 40px;width:100px;height:100px;padding:10px">
		<div class="g" style="position:absolute;grid-area:2/2;width:20px;height:20px;align-self:end;justify-self:center"></div></div>`,
		`<div class="g a" style="top:90px;left:50px;width:20px;height:20px"></div>`)
	// last baseline wyrównuje pudełko pozycjonowane do końca obszaru
	same("abspos-last-baseline", `<div style="display:grid;position:relative;grid:50px / 50px;width:100px;height:100px">
		<div class="g" style="position:absolute;grid-area:1/1/2/2;width:20px;height:20px;align-self:last baseline"></div></div>`,
		`<div class="g a" style="top:30px;width:20px;height:20px"></div>`)
	// safe: pudełko większe od obszaru wychodzi przed początek pudełka zawierającego, więc wraca do
	// krawędzi treści tego pudełka
	same("abspos-safe", `<div style="display:grid;position:relative;grid:20px 20px / 20px 20px;width:40px;height:40px;padding:5px">
		<div class="g" style="position:absolute;grid-area:2/2;width:60px;height:60px;align-self:safe end;justify-self:safe end"></div></div>`,
		`<div class="g a" style="top:5px;left:5px;width:60px;height:60px"></div>`)
	// równe linie (2 / 2): linia końcowa znika, obszar sięga krawędzi odstępu wewnętrznego
	same("abspos-equal-lines", `<div style="display:grid;position:relative;grid:50px 50px / 50px 50px;width:100px">
		<div class="g" style="position:absolute;grid-area:2/2/2/2;left:0;bottom:0;width:100%;height:100%"></div></div>`,
		`<div class="g a" style="top:50px;left:50px;width:50px;height:50px"></div>`)
	// span liczony od określonej linii
	same("abspos-span", `<div style="display:grid;position:relative;grid:50px / repeat(4, 50px)">
		<div class="g" style="position:absolute;grid-row:1;grid-column:2 / span 2;width:100%;height:100%"></div></div>`,
		`<div class="g a" style="left:50px;width:100px;height:50px"></div>`)
	// dzieci pozycjonowane malują się w kolejności dokumentu: późniejszy element siatki z position:
	// relative i jego potomek przykrywają wcześniejsze dziecko pozycjonowane
	same("abspos-paint-order", `<div style="display:grid;grid:100px / 100px">
		<div class="r" style="position:absolute;width:100px;height:100px;grid-area:1/1"></div>
		<div style="position:relative;grid-area:1/1"><div class="g a" style="width:100%;height:100%"></div></div></div>`,
		`<div class="g" style="width:100px;height:100px"></div>`)
	// minimum automatyczne ograniczone stałą funkcją maksimum ścieżki: ścieżki mają zero
	same("min-auto-fixed-max", `<div class="g a" style="width:100px;height:100px;z-index:-1"></div>
		<div style="display:grid;width:10px;height:10px;grid:minmax(auto, 0px) / minmax(auto, 0px)">
		<div class="r"><div style="width:100px;height:100px"></div></div></div>`,
		`<div class="g" style="width:100px;height:100px"></div>`)
	// kontener przewijania ma minimum automatyczne zero
	same("min-auto-scroll-container", `<div style="display:grid;grid-template-columns:minmax(auto, 30px) 1fr;width:100px">
		<div class="g" style="overflow:hidden;height:20px"><div style="width:80px"></div></div><div class="b"></div></div>`,
		`<div class="g a" style="width:30px;height:20px"></div><div class="b a" style="left:30px;width:70px;height:20px"></div>`)
	// obraz (overflow: hidden z arkusza domyślnego) zachowuje wysokość wiersza
	same("image-row-height", `<div style="display:grid;width:100px"><img src="`+square(40)+`"></div>
		<div class="b" style="width:20px;height:20px"></div>`,
		`<div class="g" style="width:40px;height:40px"></div><div class="b" style="width:20px;height:20px"></div>`)
	// ścieżka fr uznana za nieelastyczną nie oddaje swojego rozmiaru do rozdziału: 0 + 30 + 50 + 20
	same("fr-restart", `<div style="display:grid;width:100px;height:50px;grid-template-columns:repeat(2, 0fr 1fr)" class="r">
		<div class="g"></div><div class="g" style="width:30px"></div><div class="g" style="width:50px"></div><div class="g"></div></div>`,
		`<div class="g" style="width:100px;height:50px"></div>`)
	// linia nazwana bez dopasowania to pierwsza linia niejawna za siatką jawną, która bez szablonów nie
	// ma ścieżek: element trafia do drugiej ścieżki niejawnej (100%)
	same("named-line-implicit", `<div style="display:grid;width:100px;height:100px;grid-auto-columns:0px 100%;grid-auto-rows:0px 100%" class="r">
		<div class="g" style="grid-area:area"></div></div>`,
		`<div class="g" style="width:100px;height:100px"></div>`)
	// procentowe marginesy elementu odnoszą się do szerokości obszaru siatki, nie kontenera
	same("percent-margin-grid-area", `<div style="display:grid;width:200px;grid:100px / 100px 100px">
		<div class="g" style="margin:10%"></div></div>`,
		`<div class="g" style="margin:10px;width:80px;height:80px"></div>`)
	// obraz z max-width: 100% jest ściśliwy: kolumna auto nie rośnie do jego szerokości
	same("compressible-replaced", `<div style="display:grid;width:100px"><img src="`+square(200)+`"
		style="width:200px;height:auto;max-width:100%"></div>`,
		`<div class="g" style="width:100px;height:100px"></div>`)
	// wiersz procentowy przy wysokości auto: najpierw jak auto (wysokość 20 + 60 + 20), potem 20% ze 100,
	// a wiersze auto rozciągają się do 40; za siatką blok zaczyna się na 100
	same("percent-rows-auto-height", `<div style="display:grid;width:50px;grid-template-rows:auto 20% auto">
		<div class="g" style="height:20px"></div><div class="b" style="height:60px"></div><div class="g" style="height:20px"></div></div>
		<div class="b" style="width:10px;height:10px"></div>`,
		`<div class="g a" style="width:50px;height:20px"></div><div class="b a" style="top:40px;width:50px;height:60px"></div>
		<div class="g a" style="top:60px;width:50px;height:20px"></div><div class="b a" style="top:100px;width:10px;height:10px"></div>`)
	// procentowy row-gap odnosi się do wysokości po min-height
	same("percent-row-gap-min-height", `<div style="display:grid;grid:40px 40px / 40px;row-gap:20%;min-height:140px">
		<div class="g"></div><div class="b"></div></div>`,
		`<div class="g" style="width:40px;height:40px;margin-bottom:28px"></div><div class="b" style="width:40px;height:40px"></div>`)
	// auto-fill w siatce blokowej o szerokości auto: liczba powtórzeń z szerokości bloku (300 - 140) / 70 = 2
	same("auto-fill-block-width", `<div style="display:grid;grid-template-columns:10px 20px repeat(auto-fill, 30px 40px) 50px 60px">
		<div></div><div></div><div></div><div></div><div class="g" style="height:20px"></div><div class="b"></div></div>`,
		`<div class="g a" style="left:100px;width:30px;height:20px"></div><div class="b a" style="left:130px;width:40px;height:20px"></div>`)
	// kolumny rozwiązywane ponownie po wierszach: obraz z height: 100% dostaje wysokość wiersza (100),
	// więc z proporcji szerokość 100, i tyle ma pierwsza kolumna
	same("columns-after-rows", `<div style="display:grid;width:0;grid-template:auto / auto auto">
		<div class="g" style="height:100%"><img src="`+square(10)+`" style="height:100%;display:block"></div>
		<div><div style="height:100px"></div></div></div>`,
		`<div class="g" style="width:100px;height:100px"></div>`)
	// aspect-ratio kontenera: wysokość treści 100 / 2.5 = 40, plus odstępy i obramowanie do 100;
	// treść wyższa niż proporcja powiększa kontener
	same("aspect-ratio-container", `<div style="display:grid;width:100px;border-block:10px solid green;padding-block:20px;
		background:green;aspect-ratio:5/2"><div></div></div>
		<div class="g" style="display:grid;width:100px;aspect-ratio:2"><div style="height:60px"></div></div>`,
		`<div class="g" style="width:100px;height:160px"></div>`)
	// auto-fit przy szerokości max-content: powtórzenia z max-width, puste ścieżki zwinięte (50 + 10 + 50)
	same("auto-fit-max-content", `<div class="b" style="display:grid;width:max-content;max-width:400px;column-gap:10px;
		grid-template-columns:repeat(auto-fit, 50px);grid-auto-columns:50px">
		<div class="g" style="grid-column:1;height:20px"></div><div class="g" style="grid-column:3"></div></div>`,
		`<div class="g a" style="width:50px;height:20px"></div><div class="b a" style="left:50px;width:10px;height:20px"></div>
		<div class="g a" style="left:60px;width:50px;height:20px"></div>`)
}
