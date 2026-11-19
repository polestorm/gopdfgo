package gopdfgo

import (
	"context"
	"os"
	"testing"
)

// TestMonolithicPrint12 sprawdza poprawki podziału treści niepodzielnej i przycinanej w druku z rundy mono12
// (CSS Break 3 § 4.4, CSS Grid 2 § 11): test i wzorzec renderowane na stronach 200×100 px muszą dać tyle samo
// stron i te same piksele.
func TestMonolithicPrint12(t *testing.T) {
	head := `<style>@page { size: 200px 100px; margin: 0 } body { margin: 0 } :root { print-color-adjust: exact }
		.y { background: yellow } .b { background: blue } .g { background: green }</style>`
	fonts := testFonts(t)
	if ahem, err := os.ReadFile("testdata/wpt/fonts/Ahem.ttf"); err == nil {
		fonts = append([]Font{{Name: "Ahem.ttf", Data: ahem}}, fonts...)
	}
	dir := t.TempDir()
	pages := func(name, body string) [][]byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(head+body), Options{Fonts: fonts})
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

	// wiersz siatki po elemencie niepodzielnym krojonym przez strony stoi tuż pod jego końcem
	// (monolithic-overflow-007), a nie na nowej stronie
	same("grid-row-after-sliced", `<div style="display:grid" class=y>
		<div class=b style="contain:size;height:250px;width:50px"></div><div class=g style="height:20px"></div></div>`,
		`<div class=y><div class=b style="height:250px;width:50px"></div><div class=g style="height:20px"></div></div>`)
	// tak samo z odstępem między wierszami; następny wiersz, który się nie mieści, przechodzi dalej
	same("grid-row-gap-after-sliced", `<div style="display:grid;row-gap:10px">
		<div class=b style="contain:size;height:230px;width:50px"></div><div class=g style="height:20px"></div>
		<div class=y style="height:50px"></div></div>`,
		`<div class=b style="height:230px;width:50px"></div><div class=g style="height:20px;margin-top:10px"></div>
		<div class=y style="height:50px;margin-top:40px"></div>`)
	// pudełko absolute z overflow: clip wyższe niż strona jest krojone jako całość przez strony
	// (monolithic-overflow-028), a jego treść poniżej wysokości pudełka jest ucięta
	same("abspos-clip-sliced", `<div style="position:absolute;top:0;left:0;overflow:clip;width:50px;height:250px">
		<div class=b style="height:120px"></div><div class=g style="height:400px"></div></div>`,
		`<div class=b style="height:120px;width:50px"></div><div class=g style="height:130px;width:50px"></div>`)
	// górny margines elementu głównego wyższy niż strona: pierwsza strona pusta, treść od góry drugiej,
	// nic nie ginie (root-margin-001)
	same("root-margin-fills-page", `<style>html { margin-top: 150px }</style><div class=b style="height:20px;width:50px"></div>`,
		`<div style="height:100px"></div><div class=b style="height:20px;width:50px"></div>`)
	// top pudełka absolute, którego zastępnik stoi w dalszym ciągu bloku relative, liczy się od początku
	// całego bloku, a nie od góry fragmentu na stronie zastępnika (abspos-002)
	same("relative-cb-continuation", `<div style="height:100px"></div><div style="position:relative">
		<div style="height:150px"></div><div class=b style="position:absolute;top:20px;width:50px;height:30px"></div></div>`,
		`<div style="height:120px"></div><div class=b style="width:50px;height:30px"></div><div style="height:100px"></div>`)
	// bottom: 0 w takim bloku liczy się od dołu całości (abspos-001)
	same("relative-cb-bottom", `<div style="position:relative"><div class=b style="position:absolute;bottom:0;width:50px;height:30px"></div>
		<div style="height:150px"></div></div>`,
		`<div style="height:120px"></div><div class=b style="width:50px;height:30px"></div>`)
	// procentowa wysokość w dalszym ciągu body z height: 100% liczy się od pełnej wysokości body,
	// nie od reszty na tej stronie (wcześniej zero)
	same("percent-height-continuation", `<style>html, body { height: 100% }</style><div style="height:10px"></div>
		<div class=b style="break-before:page;height:50%"></div>`,
		`<div style="height:10px"></div><div class=b style="break-before:page;height:50px"></div>`)
	// pudełko fixed w powtarzanej stopce tabeli jest rysowane na każdej stronie raz (półprzezroczyste tło
	// narysowane dwa razy byłoby ciemniejsze)
	same("fixed-in-repeated-footer", `<style>.f { position:fixed; bottom:0; width:20px; height:20px; background:rgba(0,0,255,.3) }</style>
		<div style="display:table"><div style="display:table-row"><div style="height:60px"></div>
		<div style="break-before:page;height:60px"></div><div style="break-before:page;height:60px"></div></div>
		<div style="display:table-footer-group"><div style="height:20px"></div><div class=f></div></div></div>`,
		`<style>.f { position:absolute; left:0; width:20px; height:20px; background:rgba(0,0,255,.3) }</style>
		<div style="height:100px"></div><div style="height:100px"></div><div style="height:80px"></div>
		<div class=f style="top:80px"></div><div class=f style="top:180px"></div><div class=f style="top:280px"></div>`)
}
