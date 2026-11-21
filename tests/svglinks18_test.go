package tests

import (
	"bytes"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Odnośniki <a> wewnątrz SVG (SVG 2 § 16.1, ADR 0029): SVG osadzone w HTML daje adnotacje /Link
// z prostokątem otaczającym narysowaną treść elementu po przekształceniach; SVG w <img> jest statyczne
// (bez odnośników), a w <object> daje tylko odnośniki zewnętrzne. Obie drogi rysowania (w trakcie układu
// i po ułożeniu całego dokumentu) dają ten sam plik.

// pdfLink: adnotacja odnośnika; prostokąt w pikselach CSS od lewego górnego rogu strony o wysokości 300px.
type pdfLink struct {
	target         string // adres URI albo "#" + nazwa celu
	x0, y0, x1, y1 float64
}

var linkAnnotRe = regexp.MustCompile(`/Rect \[([^\]]*)\][^>]*?/Subtype/Link\s*(?:/A <</S/URI/URI \(([^)]*)\)|/Dest \(([^)]*)\)|/Dest /([^ />]*))`)

func pdfLinks(t *testing.T, pdf []byte) []pdfLink {
	t.Helper()
	var out []pdfLink
	for _, m := range linkAnnotRe.FindAllSubmatch(pdf, -1) {
		fields := strings.Fields(string(m[1]))
		if len(fields) != 4 {
			t.Fatalf("niepoprawny /Rect %q", m[1])
		}
		var v [4]float64
		for i, f := range fields {
			n, err := strconv.ParseFloat(f, 64)
			if err != nil {
				t.Fatal(err)
			}
			v[i] = n / 0.75
		}
		link := pdfLink{x0: min(v[0], v[2]), x1: max(v[0], v[2]), y0: 300 - max(v[1], v[3]), y1: 300 - min(v[1], v[3])}
		switch {
		case m[2] != nil:
			link.target = string(m[2])
		case m[3] != nil:
			link.target = "#" + string(m[3])
		default:
			link.target = "#" + string(m[4])
		}
		out = append(out, link)
	}
	return out
}

const svgLinkPage = `<style>@page { size: 400px 300px; margin: 0 } body { margin: 0 } svg, img, object { display: block }</style>`

func renderSVGLinks(t *testing.T, html string) []pdfLink {
	t.Helper()
	streamed, classic, fallback := streamPair(t, []byte(svgLinkPage+html), streamOptions(t))
	if fallback != "" {
		t.Errorf("dokument rysowany po ułożeniu: %s", fallback)
	}
	if !bytes.Equal(streamed, classic) {
		t.Errorf("wynik różni się od rysowania po ułożeniu całego dokumentu")
	}
	links := pdfLinks(t, streamed)
	if got := pdfLinks(t, classic); len(got) != len(links) {
		t.Errorf("bez rysowania w trakcie układu %d odnośników, w trakcie %d", len(got), len(links))
	}
	return links
}

func checkLinks(t *testing.T, got []pdfLink, want []pdfLink) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("odnośników %d, oczekiwano %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.target != w.target {
			t.Errorf("odnośnik %d: cel %q, oczekiwano %q", i, g.target, w.target)
		}
		const eps = 0.05
		if math.Abs(g.x0-w.x0) > eps || math.Abs(g.y0-w.y0) > eps || math.Abs(g.x1-w.x1) > eps || math.Abs(g.y1-w.y1) > eps {
			t.Errorf("odnośnik %d (%s): prostokąt %.2f %.2f %.2f %.2f, oczekiwano %.2f %.2f %.2f %.2f",
				i, w.target, g.x0, g.y0, g.x1, g.y1, w.x0, w.y0, w.x1, w.y1)
		}
	}
}

// Odnośnik zewnętrzny przy viewBox (skala 2) i wewnętrzny do kotwicy HTML przy obrocie (prostokąt
// otaczający); href ma pierwszeństwo przed xlink:href.
func TestSVGLinksInline(t *testing.T) {
	links := renderSVGLinks(t, `
<svg width="200" height="100" viewBox="0 0 100 50" xmlns:xlink="http://www.w3.org/1999/xlink">
  <a href="https://example.com/x" xlink:href="https://wrong.example/"><rect x="10" y="10" width="20" height="10"/></a>
  <g transform="translate(50 0)"><a xlink:href="#target"><rect transform="rotate(45)" width="10" height="10"/></a></g>
</svg>
<p id="target" style="margin: 0">cel</p>`)
	d := 10 * math.Sqrt2 / 2
	checkLinks(t, links, []pdfLink{
		{"https://example.com/x", 20, 20, 60, 40},
		{"#target", 2 * (50 - d), 0, 2 * (50 + d), 2 * 2 * d},
	})
}

// Zagnieżdżone <svg> z viewBox i przycięciem, <use> oraz zagnieżdżone <a>: treść należy do najbliższego
// <a>, a wewnętrzny leży w PDF nad zewnętrznym (później w /Annots). Element niewidoczny nie ma obszaru.
func TestSVGLinksNested(t *testing.T) {
	links := renderSVGLinks(t, `
<svg width="300" height="100">
  <defs><rect id="r" width="10" height="10"/></defs>
  <a href="https://outer.example/">
    <rect x="0" y="0" width="20" height="20"/>
    <a href="https://inner.example/"><rect x="40" y="0" width="20" height="20"/></a>
  </a>
  <svg x="100" y="0" width="50" height="50" viewBox="0 0 10 10">
    <a href="https://clipped.example/"><rect x="5" y="5" width="100" height="100"/></a>
  </svg>
  <a href="https://use.example/"><use href="#r" x="200" y="10"/></a>
  <a href="https://hidden.example/"><rect x="0" y="50" width="10" height="10" visibility="hidden"/></a>
  <a href="https://none.example/" display="none"><rect x="0" y="50" width="10" height="10"/></a>
</svg>`)
	checkLinks(t, links, []pdfLink{
		{"https://outer.example/", 0, 0, 20, 20},
		{"https://inner.example/", 40, 0, 60, 20},
		{"https://clipped.example/", 125, 25, 150, 50},
		{"https://use.example/", 200, 10, 210, 20},
	})
}

// Tekst w odnośniku: obszar to ramka składu tekstu, także dla <a> wewnątrz <text>.
func TestSVGLinksText(t *testing.T) {
	links := renderSVGLinks(t, `
<svg width="300" height="100">
  <a href="https://text.example/"><text x="10" y="40" font-size="20">Kliknij</text></a>
  <text x="10" y="80" font-size="20">abc <a href="https://span.example/">def</a></text>
</svg>`)
	if len(links) != 2 {
		t.Fatalf("odnośników %d, oczekiwano 2: %+v", len(links), links)
	}
	first, second := links[0], links[1]
	if first.target != "https://text.example/" || first.x0 < 9 || first.x0 > 12 || first.x1 < 60 || first.y0 > 25 || first.y1 < 40 || first.y1 > 50 {
		t.Errorf("odnośnik tekstu: %+v", first)
	}
	if second.target != "https://span.example/" || second.x0 < 40 || second.x1 < second.x0+20 || second.y1 < 80 || second.y0 < 55 {
		t.Errorf("odnośnik fragmentu tekstu: %+v", second)
	}
}

// SVG w <img> jest wyświetlane w trybie statycznym, bez odnośników; w <object> to osobny dokument:
// odnośnik zewnętrzny działa, fragment wskazuje miejsce w samym SVG i nie trafia do PDF.
func TestSVGLinksImageAndObject(t *testing.T) {
	image := `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="50">` +
		`<a href="https://ext.example/"><rect width="40" height="20"/></a>` +
		`<a href="#target"><rect x="50" width="40" height="20"/></a></svg>`
	data := "data:image/svg+xml," + url.PathEscape(image)
	links := renderSVGLinks(t, `<img src="`+data+`"><p id="target">cel</p>`)
	checkLinks(t, links, nil)
	links = renderSVGLinks(t, `<div style="height: 60px"></div><object type="image/svg+xml" data="`+data+`" width="100" height="50"></object><p id="target">cel</p>`)
	checkLinks(t, links, []pdfLink{{"https://ext.example/", 0, 60, 40, 80}})
	// ten sam plik dwa razy: każde wystąpienie ma swój odnośnik (bez formy współdzielonej, zob. ADR 0029)
	object := `<object type="image/svg+xml" data="` + data + `" width="100" height="50"></object>`
	links = renderSVGLinks(t, object+object)
	checkLinks(t, links, []pdfLink{{"https://ext.example/", 0, 0, 40, 20}, {"https://ext.example/", 0, 50, 40, 70}})
}
