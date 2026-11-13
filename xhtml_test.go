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
