package tests

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo/internal/webrender/html/layout"
)

// Regresja z 2026-11-23 (ADR 0036, dopisek): element pływający przeniesiony na następną stronę (pushFloat)
// żyje do jej ułożenia w kontekście (brokenOutOfFlow), nie w stanie stron (pageMaker). Obieg paginacji,
// który układał od nowa tylko niektóre strony (tu strony z counter(pages) w nagłówku rozdziału), dawał
// stronie ułożonej od nowa zgłoszenia zostawione przez ostatnią stronę ułożoną w tym obiegu, często odległą:
// element pływający stawał drugi raz na górze dalszej strony. Drugi przebieg układa strony po kolei, więc
// kotwica wypadała gdzie indziej niż w pierwszym (checkAnchor) i dokument wracał do starej drogi: książka
// ze spisem treści z testdata/books zajmowała 0,55 GB zamiast 0,44 GB.
//
// Dokument ma ponad TwoPassMinPages stron, spis treści z target-counter, „strona X z Y” w nagłówkach,
// elementy pływające przenoszone na następną stronę, tabelę obok elementu pływającego dzieloną między
// strony i kontener kolumn obok elementu pływającego (zmiany z 438a2ef). Ma iść drogą dwuprzebiegową bez
// przeszkody i dać te same bajty co układ całego dokumentu przed rysowaniem.
func TestTwoPassPushedFloats(t *testing.T) {
	opts := streamOptions(t)
	image := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(
		`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="200"><rect width="120" height="200" fill="teal"/></svg>`))
	lorem := strings.Repeat("Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor. ", 2)
	const chapters = 14
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang=pl><meta charset=utf-8><style>
		@page { size: A6 }
		.toc a::after { content: leader('.') target-counter(attr(href), page) }
		nav { break-after: page }
		h2::after { content: " (strona " counter(page) " z " counter(pages) ")" }
		.fig { float: left; margin: 0 8px 4px 0 }
		.cols { columns: 2; column-gap: 8px }
		table { border-collapse: collapse } td { border: 1px solid; padding: 1px 3px }
		</style><body><nav><h1>Spis treści</h1>`)
	for i := range chapters {
		fmt.Fprintf(&b, `<p class="toc"><a href="#r%d">Rozdział %d</a></p>`, i, i+1)
	}
	b.WriteString(`</nav>`)
	for i := range chapters {
		fmt.Fprintf(&b, `<h2 id="r%d">Rozdział %d</h2>`, i, i+1)
		// element pływający coraz niżej na stronie nagłówka: w części rozdziałów nie mieści się i przechodzi
		// na następną stronę
		b.WriteString(paragraphs(1+i%7, lorem))
		fmt.Fprintf(&b, `<img class="fig" src="%s" alt="">`, image)
		b.WriteString(paragraphs(4, lorem))
		switch i % 3 {
		case 1: // tabela obok elementu pływającego, dzielona między strony
			fmt.Fprintf(&b, `<img class="fig" src="%s" alt=""><table>`, image)
			for r := range 30 {
				fmt.Fprintf(&b, `<tr><td>%d</td><td>pozycja %d</td></tr>`, r+1, r+1)
			}
			b.WriteString(`</table>`)
		case 2: // kontener kolumn obok elementu pływającego
			fmt.Fprintf(&b, `<img class="fig" src="%s" alt=""><div class="cols">%s</div>`, image, paragraphs(3, lorem))
		}
		b.WriteString(paragraphs(6, lorem))
	}
	html := []byte(b.String())

	classic := classicRender(t, html, opts)
	// tabela obok elementu pływającego wystaje poza stronę, więc dokument jest pomniejszany (ADR 0014): to
	// zostaje dla dwóch przebiegów; bez pomniejszania strony są odkładane (ADR 0036, dopisek trzeci)
	checkDeferred(t, html, opts, classic, "wider than the page")
	unscaled := opts
	unscaled.MaxShrink = 1
	checkDeferred(t, html, unscaled, classicRender(t, html, unscaled), "")
	result, fallback, mode := twoPassRender(t, html, opts, layout.TwoPassMinPages)
	if result.Pages <= layout.TwoPassMinPages {
		t.Fatalf("stron %d, dokument ma mieć więcej niż TwoPassMinPages (%d)", result.Pages, layout.TwoPassMinPages)
	}
	if mode != "stream" || fallback != "" {
		t.Errorf("drugi przebieg nie rysował w trakcie układu: tryb %q, przeszkoda %q", mode, fallback)
	}
	if !bytes.Equal(result.PDF, classic.PDF) {
		t.Errorf("wynik różni się od układu całego dokumentu (%d B wobec %d B)", len(result.PDF), len(classic.PDF))
	}
	if len(result.Warnings) != len(classic.Warnings) {
		t.Errorf("ostrzeżeń %d, bez rysowania w trakcie układu %d", len(result.Warnings), len(classic.Warnings))
	}
}
