package tests

import (
	"fmt"
	"github.com/polestorm/gopdfgo/internal/htmlinput"
	"testing"
)

// Elementy pływające dzielone między kolumny i clear (CSS 2.1 § 9.5, CSS Multicol 1, CSS Break 3 § 4.4).
// Każdy przypadek porównuje układ z wzorcem z bloków pozycjonowanych.
func TestCSS2MC8FloatsAcrossColumns(t *testing.T) {
	head := `<style>@page { size: 400px 300px; margin: 0 } body { margin: 0 }
		.mc { width: 300px; column-width: 100px; column-gap: 0; column-fill: auto; height: 100px }
		.f { width: 15px; height: 250px; background: aqua }</style>`
	dir := t.TempDir()
	same := func(name, test, reference string) {
		t.Helper()
		got := rasterize(t, renderString(t, head+test).PDF, dir, name)
		want := rasterize(t, renderString(t, head+reference).PDF, dir, name+"-ref")
		if len(got) != len(want) {
			t.Errorf("%s: %d stron, wzorzec ma %d", name, len(got), len(want))
			return
		}
		for i := range got {
			if n := differentPixels(got[i], want[i]); n != 0 {
				t.Errorf("%s, strona %d: %d pikseli różni się od wzorca", name, i+1, n)
			}
		}
	}
	block := func(left, top, width, height int, color string) string {
		return fmt.Sprintf(`<div style="position:absolute;left:%dpx;top:%dpx;width:%dpx;height:%dpx;background:%s"></div>`,
			left, top, width, height, color)
	}
	floats := block(0, 0, 15, 100, "aqua") + block(85, 0, 15, 100, "aqua") +
		block(100, 0, 15, 100, "aqua") + block(185, 0, 15, 100, "aqua") +
		block(200, 0, 15, 50, "aqua") + block(285, 0, 15, 50, "aqua")

	// Dalszy ciąg pływającego w prawo stoi obok dalszego ciągu pływającego w lewo w każdej kolumnie,
	// a nie pod nim; blok po <br clear="all"> zaczyna się za nimi w trzeciej kolumnie.
	same("left-and-right-continue", `<div class="mc"><div><div class="f" style="float:left"></div>`+
		`<div class="f" style="float:right"></div></div><div style="border-bottom:3px solid orange"><br clear="all"></div></div>`,
		floats+block(200, 50, 100, 3, "orange"))

	// <br clear="all"> na końcu bloku z elementami pływającymi: blok ciągnie się do trzeciej kolumny,
	// a następny blok stoi za nim.
	same("br-clear-inside-container", `<div class="mc"><div><div class="f" style="float:left"></div>`+
		`<div class="f" style="float:right"></div><br clear="all"></div><div style="border-bottom:3px solid orange"></div></div>`,
		floats+block(200, 50, 100, 3, "orange"))

	// Równoważenie liczy elementy pływające w potomkach kolumny: 250px w trzech kolumnach i 5px za nimi,
	// czyli po 85px.
	same("balancing-counts-nested-floats", `<div class="mc" style="column-fill:balance"><div>`+
		`<div class="f" style="float:left"></div></div><div style="clear:left;border-bottom:5px solid orange"></div></div>`,
		block(0, 0, 15, 85, "aqua")+block(100, 0, 15, 85, "aqua")+block(200, 0, 15, 80, "aqua")+
			block(200, 80, 100, 5, "orange"))
}

// Fragmentacja w kolumnach: element pływający bez miejsca na dole kolumny, kolumny o zerowej wysokości,
// pseudoelement ::column.
func TestCSS2MC8ColumnFragmentation(t *testing.T) {
	head := `<style>@page { size: 400px 300px; margin: 0 } body { margin: 0 }</style>`
	dir := t.TempDir()
	same := func(name, test, reference string) {
		t.Helper()
		got := rasterize(t, renderString(t, head+test).PDF, dir, name)
		want := rasterize(t, renderString(t, head+reference).PDF, dir, name+"-ref")
		if len(got) != len(want) {
			t.Errorf("%s: %d stron, wzorzec ma %d", name, len(got), len(want))
			return
		}
		for i := range got {
			if n := differentPixels(got[i], want[i]); n != 0 {
				t.Errorf("%s, strona %d: %d pikseli różni się od wzorca", name, i+1, n)
			}
		}
	}
	block := func(left, top, width, height int, color string) string {
		return fmt.Sprintf(`<div style="position:absolute;left:%dpx;top:%dpx;width:%dpx;height:%dpx;background:%s"></div>`,
			left, top, width, height, color)
	}

	// Pływający kontener kolumn stoi na samym dole pierwszej kolumny: przechodzi do drugiej.
	same("float-at-column-end", `<div style="columns:2;column-gap:0;column-fill:auto;width:200px;height:100px">`+
		`<div style="height:100px"></div><div style="float:left;columns:2;column-gap:0;width:100%">`+
		`<div style="height:200px;background:green"></div></div></div>`,
		block(100, 0, 100, 100, "green"))

	// Kontener o zerowej wysokości: blok z powielanymi obramowaniami zużywa 1px treści na kolumnę.
	same("zero-height-clone", `<div style="columns:3;column-gap:0;width:300px;height:0">`+
		`<div style="height:2px;border:5px solid green;box-decoration-break:clone"></div></div>`,
		`<div style="columns:3;column-gap:0;column-fill:auto;width:300px;height:30px"><div style="height:1px;border:5px solid green"></div>`+
			`<div style="break-before:column;height:1px;border:5px solid green"></div></div>`)

	// Pudełko pozycjonowane podzielone w kolumnach zagnieżdżonego kontenera ciągnie się w jego kolumnach
	// w następnej kolumnie zewnętrznej, choć treść w przepływie już się skończyła.
	same("nested-abspos-continuation", `<div style="columns:2;column-gap:0;column-fill:auto;width:100px;height:100px">`+
		`<div style="columns:2;column-gap:0;column-fill:auto;width:50px"><div style="position:relative">`+
		`<div style="position:absolute;width:25px;height:400px;background:green"></div></div></div></div>`,
		block(0, 0, 100, 100, "green"))

	// ::column jest poprawnym selektorem, ale tło do niego się nie stosuje.
	same("column-pseudo", `<style>@supports not selector(::column) { .m::before { content: "X" } } .m::column { background: red }</style>`+
		`<div class="m" style="columns:1;width:100px;height:100px"><div style="height:100px;background:green"></div></div>`,
		block(0, 0, 100, 100, "green"))
}

// Sekcja CDATA w treści dokumentu XHTML to tekst (XML 1.0 § 2.7), także poza <style>.
func TestCSS2MC8XHTMLCDATA(t *testing.T) {
	const ns = `<html xmlns="http://www.w3.org/1999/xhtml">`
	for _, c := range []struct{ name, in, want string }{
		{"text", ns + `<p><![CDATA[a < b & c]]></p>`, ns + `<p>a &lt; b &amp; c</p>`},
		{"style untouched", ns + `<style><![CDATA[p{}]]></style><p/>`, ns + `<style><![CDATA[p{}]]></style><p></p>`},
	} {
		if got := string(htmlinput.ExpandSelfClosing([]byte(c.in))); got != c.want {
			t.Errorf("%s:\n   jest %s\n ma być %s", c.name, got, c.want)
		}
	}
}
