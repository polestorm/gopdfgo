package gopdfgo

import "testing"

func TestExpandSelfClosing(t *testing.T) {
	const ns = `<html xmlns="http://www.w3.org/1999/xhtml">`
	for _, c := range []struct{ name, in, want string }{
		{"non-void element", ns + `<div class="a"/><p>x</p>`, ns + `<div class="a"></div><p>x</p>`},
		{"void element kept", ns + `<br/><img src="a.png"/><hr />`, ns + `<br/><img src="a.png"/><hr />`},
		{"attribute with slash and quote", ns + `<a href="x/y" title='a "b"'/>`, ns + `<a href="x/y" title='a "b"'></a>`},
		{"table cell", ns + `<td/><td>1</td>`, ns + `<td></td><td>1</td>`},
		{"svg untouched", ns + `<svg viewBox="0 0 1 1"><path d="M0 0"/><g/></svg><span/>`, ns + `<svg viewBox="0 0 1 1"><path d="M0 0"/><g/></svg><span></span>`},
		{"plain html untouched", `<html><div class="a"/><p>x</p>`, `<html><div class="a"/><p>x</p>`},
	} {
		if got := string(expandSelfClosing([]byte(c.in))); got != c.want {
			t.Errorf("%s:\n   jest %s\n ma być %s", c.name, got, c.want)
		}
	}
}

func TestStripNamespacePrefixes(t *testing.T) {
	const document = `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:svg="http://www.w3.org/2000/svg" xmlns:x="urn:other">` +
		`<body><svg:svg height="50"><svg:rect width="10" height="10"/></svg:svg><x:data>a</x:data></body></html>`
	const want = `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:svg="http://www.w3.org/2000/svg" xmlns:x="urn:other">` +
		`<body><svg height="50"><rect width="10" height="10"/></svg><x:data>a</x:data></body></html>`
	if got := string(expandSelfClosing([]byte(document))); got != want {
		t.Errorf("wynik:\n%s\noczekiwany:\n%s", got, want)
	}
}

func TestDecodeStyleEntities(t *testing.T) {
	cases := map[string]string{
		`<style>.a &gt; .b { color: red }</style>`:               `<style>.a > .b { color: red }</style>`,
		`<style type="text/css">p:lang(&#x70;l) {}</style>`:      `<style type="text/css">p:lang(pl) {}</style>`,
		`<style><![CDATA[ .a &gt; .b {} ]]></style>`:             `<style><![CDATA[ .a &gt; .b {} ]]></style>`,
		`<style>p::before { content: "&lt;/style&gt;" }</style>`: `<style>p::before { content: "&lt;/style&gt;" }</style>`,
		`<p>a &gt; b</p>`: `<p>a &gt; b</p>`,
	}
	for input, want := range cases {
		if got := string(decodeStyleEntities([]byte(input))); got != want {
			t.Errorf("decodeStyleEntities(%q) = %q, oczekiwane %q", input, got, want)
		}
	}
}
