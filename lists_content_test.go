package gopdfgo

import (
	"context"
	"os"
	"testing"
)

// TestListsAndGeneratedContent sprawdza liczniki, znaczniki list i treść generowaną (CSS Lists 3,
// CSS Content 3, CSS Pseudo 4): każdy przypadek porównuje render z wzorcem bez badanej funkcji.
func TestListsAndGeneratedContent(t *testing.T) {
	ahem, err := os.ReadFile("testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Skip("brak fontu Ahem")
	}
	fonts := append([]Font{{Name: "Ahem.ttf", Data: ahem}}, testFonts(t)...)
	head := `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0; font: 20px/1 "Noto Sans" }
		.g { background: green } .d::before { content: attr(data-val) } .ahem { font: 20px/1 Ahem }</style>`
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

	// licznik odwrócony: wartość początkowa liczy przyrosty w ::before i ::after, a ostatni
	// niezerowy przyrost dwa razy (CSS Lists 3 § 4.4.2): 1+2+1+2 = 6, plus 2, więc start 8
	same("reversed-pseudo", `<style>.list { counter-reset: reversed(foo) }
		.item::before { counter-increment: foo -1; content: "B" counter(foo) }
		.item::after { counter-increment: foo -2; content: "A" counter(foo) }</style>
		<div class=list><div class=item></div><div class=item></div></div>`,
		`<div class=d data-val="B7A5"></div><div class=d data-val="B4A2"></div>`)
	// counter-set kończy sumowanie, a ostatni przyrost i tak się dolicza: 1 + 10 + 1 = 12;
	// licznik zagnieżdżony w liczniku przodka nie obejmuje rodzeństwa
	same("reversed-counter-set", `<style>ol { counter-reset: reversed(foo); margin: 0; padding: 0 }
		li { display: block } li::before { counter-increment: foo -1; content: counter(foo) ". " }
		.set { counter-set: foo 10 }</style>
		<ol><li>a</li><li>b<ol><li>c</li><li class=set>d</li><li>e</li></ol></li><li>f</li></ol>`,
		`<style>.d::after { content: " " }</style><div><span class=d data-val="3."></span>a</div>
		<div><span class=d data-val="2."></span>b</div><div><span class=d data-val="11."></span>c</div>
		<div><span class=d data-val="9."></span>d</div><div><span class=d data-val="8."></span>e</div>
		<div><span class=d data-val="1."></span>f</div>`)
	// ::marker { content: none } nie tworzy znacznika (CSS Lists 3 § 3.1)
	same("marker-content-none", `<style>li::marker { content: none }</style><ul><li>a</li></ul>`,
		`<ul style="list-style: none"><li>a</li></ul>`)
	// element z display: contents nie zmienia liczników (CSS Lists 3 § 4.5)
	same("display-contents-counters", `<style>.inc { counter-increment: x } .r { counter-reset: x 6 }
		.c { display: contents; counter-reset: x 666; counter-set: x 666 } .res::before { content: counter(x) }</style>
		<div><span class=r></span><span class="c inc"></span><span class="inc res"></span></div>`,
		`<div class=d data-val="7"></div>`)
	// counter-* w ::marker; licznik założony przez znacznik widzi też ::after elementu listy
	same("marker-counters", `<style>ol { counter-reset: c; list-style-position: inside; margin: 0; padding: 0 }
		::marker { counter-increment: c; content: "[" counters(c, ":") "]" }
		.reset::marker { counter-reset: c } .reset::after { counter-increment: c; content: "(" counters(c, ":") ")" }</style>
		<ol><li>a</li><li class=reset>b</li><li>c</li></ol>`,
		`<style>.d { display: block }</style><div><span class=d data-val="[1]" style="display:inline"></span>a</div>
		<div><span class=d data-val="[1:1]" style="display:inline"></span>b(1:2)</div>
		<div><span class=d data-val="[2]" style="display:inline"></span>c</div>`)
	// ::before::marker wybiera znacznik pseudoelementu, a samo ::marker go nie wybiera (CSS Pseudo 4 § 4.2)
	same("nested-marker", `<style>::marker { color: red } div::before { content: "X"; display: list-item;
		list-style-position: inside } div::before::marker { content: "MM"; color: green }</style><div class=ahem></div>`,
		`<div class=ahem><span style="color: green">MM</span>X</div>`)
	// pojedynczy <image> w content zastępuje zawartość elementu (CSS Content 3 § 1), także gradient
	same("element-replacement-gradient", `<div style="width: 100px; height: 100px; content: linear-gradient(green, green)">tekst</div>`,
		`<div class=g style="width: 100px; height: 100px"></div>`)
	// gradient w ::before ma rozmiar pseudoelementu; image-set() wybiera wariant 1x (CSS Images 4 § 2.2)
	same("pseudo-image-set", `<style>div::before { content: image-set(linear-gradient(red, red) 2x, linear-gradient(green, green) 1x);
		display: block; width: 100px; height: 100px }</style><div></div>`,
		`<div class=g style="width: 100px; height: 100px"></div>`)
	// quotes: match-parent bierze cudzysłowy rodzica dobrane według jego języka (CSS Content 3 § 4.1)
	same("quotes-match-parent", `<style>q { quotes: auto } q q { quotes: match-parent }</style>
		<div lang="fr"><q lang="de">a <q lang="sv">b</q></q></div>`,
		`<div lang="fr"><q>a <q>b</q></q></div>`)
	// kierunek ::marker nie przenosi znacznika zewnętrznego na drugą stronę elementu listy
	same("marker-direction", `<style>li { list-style-type: "X" } li::marker { direction: rtl }</style>
		<ol class=ahem><li>a</li></ol>`,
		`<ol class=ahem><li style="list-style-type: 'X'">a</li></ol>`)
	// znacznik zewnętrzny idzie za pierwszym wierszem, który zaczyna się obok elementu pływającego
	same("marker-next-to-float", `<div style="float: left; width: 96px; height: 96px"></div>
		<div class=ahem style="display: list-item; list-style: 'X' outside; margin-left: 20px"></div>`,
		`<div class=ahem style="display: list-item; list-style: 'X' outside; margin-left: 96px">&nbsp;</div>`)
}
