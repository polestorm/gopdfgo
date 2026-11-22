package tests

import (
	"context"
	"github.com/polestorm/gopdfgo"
	"testing"
)

// Testy poprawek z CSS Text 3 i 4 spoza white-space: text-align-last, justowanie, text-transform,
// wrap-inside, hyphenate-limit-lines, word-space-transform, łamanie na granicy elementów. Każdy
// przypadek porównuje stronę z wzorcem ułożonym bez badanej właściwości.

// notoComparer działa jak ahemComparer, ale z fontem NotoSans, w którym widać wielkość liter.
func notoComparer(t *testing.T) func(name, test, reference string) {
	t.Helper()
	return fontComparer(t, testFonts(t), `body { font: 20px/30px "Noto Sans" }`)
}

// fontComparer renderuje test i wzorzec podanymi fontami i arkuszem; zgłasza różnicę pikseli.
func fontComparer(t *testing.T, fonts []gopdfgo.Font, css string) func(name, test, reference string) {
	t.Helper()
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 } ` + css + `</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := gopdfgo.Render(context.Background(), []byte(head+body), gopdfgo.Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	return func(name, test, reference string) {
		t.Helper()
		if n := differentPixels(page(name, test), page(name+"-ref", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
}

func TestTextAlignLastAuto(t *testing.T) {
	same := ahemComparer(t)
	// text-align-last: auto po justify wyrównuje ostatni wiersz do początku, a skrót text-align
	// ustawia text-align-last na auto
	same("justify-auto", `<div style="width:200px;text-align:justify;text-align-last:auto">XX XX</div>`,
		`<div>XX XX</div>`)
	same("last-justify", `<div style="width:200px;text-align:start;text-align-last:justify">XX XX</div>`,
		`<div style="white-space:pre">XX      XX</div>`)
	same("shorthand-resets-last", `<div style="width:200px;text-align-last:justify;text-align:start">XX XX</div>`,
		`<div>XX XX</div>`)
	same("justify-all", `<div style="width:200px;text-align:justify-all">XX XX</div>`,
		`<div style="white-space:pre">XX      XX</div>`)
}

func TestJustifyAfterLastTab(t *testing.T) {
	same := ahemComparer(t)
	// justowana jest tylko część wiersza po ostatnim tabulatorze
	same("tab", `<div style="width:240px;white-space:pre-wrap;tab-size:4;text-align:justify">X X&#9;X X XXXXXXXXXXXX</div>`,
		`<div style="white-space:pre;tab-size:4">X X&#9;X<span style="margin-left:120px">X</span><br>XXXXXXXXXXXX</div>`)
}

func TestTextTransformRules(t *testing.T) {
	same := notoComparer(t)
	// capitalize przez granicę elementu pozycjonowanego: słowo trwa dalej (wzorzec dzielony tak samo, bo
	// szerokość kawałka tekstu jest w górę do 1/64 px i podział przesuwa dalszy tekst o ułamek piksela, ADR 0043)
	same("capitalize-abspos", `<p style="text-transform:capitalize;font-kerning:none">p<span style="position:absolute"></span>ass go</p>`,
		`<p style="font-kerning:none">P<span style="position:absolute"></span>ass Go</p>`)
	// litera tytułowa z odwzorowaniem pełnym i reguła niderlandzka
	same("titlecase", `<p style="text-transform:capitalize">ﬁsh <span lang="nl">ijsland</span></p>`,
		`<p>Fish <span lang="nl">IJsland</span></p>`)
	// greckie wielkie litery: bez tonosu, dialytika z rozdzielonego dyftongu, samodzielne ή zachowuje akcent
	same("greek", `<p lang="el" style="text-transform:uppercase">νεράιδα ή καλημέρα</p>`,
		`<p lang="el">ΝΕΡΑΪΔΑ Ή ΚΑΛΗΜΕΡΑ</p>`)
	// math-auto: tylko pojedynczy znak w kursywie matematycznej
	same("math-auto", `<p><span style="text-transform:math-auto">x</span> <span style="text-transform:math-auto">xy</span></p>`,
		`<p>&#x1D465; xy</p>`)
}

func TestWrapInsideAvoid(t *testing.T) {
	same := ahemComparer(t)
	same("keep", `<div style="width:110px">XX <span style="wrap-inside:avoid">YY ZZ</span></div>`,
		`<div>XX<br>YY ZZ</div>`)
	same("siblings", `<div style="width:170px"><span style="wrap-inside:avoid">AA BB</span> <span style="wrap-inside:avoid">CC DD</span></div>`,
		`<div>AA BB<br>CC DD</div>`)
	// zewnętrzne pudełko ma pierwszeństwo
	same("nested", `<div style="width:110px">X <span style="wrap-inside:avoid">Y <span style="wrap-inside:avoid">Z W</span></span></div>`,
		`<div>X<br>Y Z W</div>`)
	// w ostateczności pudełko łamie się w środku
	same("last-resort", `<div style="width:70px"><span style="wrap-inside:avoid">YY ZZ</span></div>`,
		`<div>YY<br>ZZ</div>`)
}

func TestHyphenationLimits(t *testing.T) {
	same := ahemComparer(t)
	const style = `width:120px;hyphens:auto;hyphenate-character:'-'`
	same("no-limit", `<div lang="en" style="`+style+`">implementation</div>`,
		`<div>imple-<br>menta-<br>tion</div>`)
	same("limit-1", `<div lang="en" style="`+style+`;hyphenate-limit-lines:1">implementation</div>`,
		`<div>imple-<br>mentation</div>`)
	same("limit-0", `<div lang="en" style="`+style+`;hyphenate-limit-lines:0">implementation</div>`,
		`<div>implementation</div>`)
	// miękki łącznik kończący pudełko tekstu pokazuje znak dzielenia
	same("shy-at-element-end", `<div style="width:100px;hyphenate-character:'-'"><span>XXXX&shy;</span>XX</div>`,
		`<div>XXXX-<br>XX</div>`)
}

func TestWordSpaceTransform(t *testing.T) {
	same := ahemComparer(t)
	same("space", `<div style="word-space-transform:space">X<wbr>X&#x200B;X</div>`, `<div>X X X</div>`)
	same("off-on-wbr", `<div style="word-space-transform:space">X<wbr style="word-space-transform:none">X&#x200B;X</div>`,
		`<div>XX X</div>`)
	same("collapse", `<div style="word-space-transform:space">X<wbr> X</div>`, `<div>X X</div>`)
}

func TestBreaksAtElementBoundary(t *testing.T) {
	same := ahemComparer(t)
	// litery z word-break: break-all liczą się jak ideogramy także na granicy elementu
	same("break-all", `<div style="width:100px">XXXXXXXXXX<span style="word-break:break-all">XXXXXXXXX</span>XXX</div>`,
		`<div>XXXXXXXXXX<br>XXXXX<br>XXXX<br>XXX</div>`)
	// overflow-wrap: anywhere pozwala awaryjnie złamać wiersz tuż za elementem
	same("anywhere", `<div style="width:40px">X<span style="overflow-wrap:anywhere">XX</span>XX</div>`,
		`<div>XX<br>X<br>XX</div>`)
	// przed elementem atomowym i po nim wolno złamać wiersz także obok dwukropka
	same("atomic-colon", `<div style="width:0"><span style="display:inline-block">X</span>:</div>`,
		`<div><span style="display:inline-block">X</span><br>:</div>`)
	// ale nie obok znaku klasy GL
	same("atomic-glue", `<div style="width:20px">X&#x2060;<span style="display:inline-block">X</span></div>`,
		`<div style="white-space:nowrap">X&#x2060;<span style="display:inline-block">X</span></div>`)
}

func TestJustifyWordSeparators(t *testing.T) {
	same := ahemComparer(t)
	// separatory słów innych pism są miejscami justowania jak spacja
	same("ethiopic", `<div style="width:240px;text-align:justify">XXXX<span style="color:transparent">&#x1361;</span>XXXX XXXX</div>`,
		`<div style="width:240px;text-align:justify">XXXX XXXX XXXX</div>`)
}

func TestTextIndentMisc(t *testing.T) {
	same := ahemComparer(t)
	// przy ujemnym wcięciu pozycją tabulatora może być krawędź bloku
	same("tab-outdent", `<div style="margin-left:40px;text-indent:-40px;white-space:pre;tab-size:4">X&#9;X</div>`,
		`<div style="white-space:pre">X X</div>`)
	// wiersz z samym elementem pływającym nie zużywa wcięcia
	same("below-float", `<div style="width:100px;text-indent:40px"><span><div style="float:left;width:100px;height:20px;background:blue"></div>X</span></div>`,
		`<div style="width:100px"><div style="height:20px;background:blue"></div><span style="margin-left:40px">X</span></div>`)
}
