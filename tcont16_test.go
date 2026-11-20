package gopdfgo

import (
	"fmt"
	"strings"
	"testing"
)

// tcontSame porównuje strony dokumentu test ze stronami wzorca reference piksel po pikselu.
func tcontSame(t *testing.T, dir, name, test, reference string) {
	t.Helper()
	got := rasterize(t, renderString(t, test).PDF, dir, name)
	want := rasterize(t, renderString(t, reference).PDF, dir, name+"-ref")
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

// tcontBox rysuje prostokąt ustawiony bezwzględnie.
func tcontBox(left, top, width, height int, color string) string {
	return fmt.Sprintf(`<div style="position:absolute;left:%dpx;top:%dpx;width:%dpx;height:%dpx;background:%s"></div>`,
		left, top, width, height, color)
}

// tcontPages składa strony wzorca z prostokątów, każda strona od nowej strony.
func tcontPages(page string, fragments ...string) string {
	var b strings.Builder
	b.WriteString(page)
	for i, f := range fragments {
		breakBefore := ""
		if i > 0 {
			breakBefore = "break-before:page;"
		}
		fmt.Fprintf(&b, `<div style="%sposition:relative;height:10px">%s</div>`, breakBefore, f)
	}
	return b.String()
}

// Dalszy ciąg komórki rozpiętej (rowspan) na następnej stronie ma tło wiersza, w którym się zaczyna,
// a nie wiersza, w którym stoi (CSS 2.1 § 17.5.1: tło wiersza leży pod komórkami, które się w nim
// zaczynają). Przy pasach zebry (tr:nth-child(even)) kolor dalszego ciągu był kolorem pierwszego wiersza
// następnej strony.
func TestRowspanContinuationRowBackground(t *testing.T) {
	dir := t.TempDir()
	page := `<style>@page { size: 200px 100px; margin: 0 } body { margin: 0 }
		table { border-spacing: 0 } td { padding: 0; width: 50px; height: 40px }
		tr { background: yellow } tr:nth-child(even) { background: blue }</style>`
	tcontSame(t, dir, "rowspan-zebra",
		page+`<table><tr><td rowspan="4"></td><td></td></tr><tr><td></td></tr><tr><td></td></tr><tr><td></td></tr></table>`,
		tcontPages(page,
			tcontBox(0, 0, 50, 80, "yellow")+tcontBox(50, 0, 50, 40, "yellow")+tcontBox(50, 40, 50, 40, "blue"),
			tcontBox(0, 0, 50, 80, "yellow")+tcontBox(50, 0, 50, 40, "yellow")+tcontBox(50, 40, 50, 40, "blue")))
	// Komórka rozpięta zaczyna się w wierszu parzystym (niebieskim), a następna strona zaczyna się od
	// wiersza nieparzystego (żółtego).
	tcontSame(t, dir, "rowspan-zebra-even",
		page+`<table><tr><td></td><td></td></tr><tr><td rowspan="3"></td><td></td></tr><tr><td></td></tr><tr><td></td></tr></table>`,
		tcontPages(page,
			tcontBox(0, 0, 100, 40, "yellow")+tcontBox(0, 40, 100, 40, "blue"),
			tcontBox(0, 0, 50, 80, "blue")+tcontBox(50, 0, 50, 40, "yellow")+tcontBox(50, 40, 50, 40, "blue")))
}
