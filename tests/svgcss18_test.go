package tests

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/polestorm/gopdfgo"
)

// chartPage to wykres słupkowy w stylu D3: klasy na <g>, <rect>, <text> i ścieżce osi, wygląd
// nadawany wyłącznie arkuszem strony.
const chartPage = `<!doctype html><html><head><style>
@page { size: 320px 220px; margin: 0 } body { margin: 0; font-family: sans-serif; color: #224 }
.report .chart { display: block }
.chart .bar { fill: steelblue }
.chart .bar:nth-child(2n) { fill: #e07b39 }
.chart .bar.highlight { fill: crimson; stroke: black; stroke-width: 2px }
.axis path, .axis line { fill: none; stroke: currentColor; shape-rendering: crispEdges }
.axis text { font-size: 10px; fill: currentColor; text-anchor: middle }
svg .title { font-size: 1.4em; font-weight: bold }
</style></head><body><div class="report">
<svg class="chart" width="320" height="220">
 <text class="title" x="10" y="20">Sprzedaż</text>
 <g class="bars" transform="translate(30,30)">
  <rect class="bar" x="0" y="80" width="40" height="70"/>
  <rect class="bar" x="50" y="40" width="40" height="110"/>
  <rect class="bar highlight" x="100" y="20" width="40" height="130"/>
  <rect class="bar" x="150" y="60" width="40" height="90"/>
 </g>
 <g class="axis" transform="translate(30,180)">
  <path d="M0,0H250"/><line x1="20" y1="0" x2="20" y2="5"/><line x1="70" y1="0" x2="70" y2="5"/>
  <text x="20" y="18">I</text><text x="70" y="18">II</text><text x="120" y="18">III</text><text x="170" y="18">IV</text>
 </g>
</svg></div></body></html>`

// chartAttributes to ten sam wykres zapisany atrybutami prezentacyjnymi, bez arkusza dla SVG.
const chartAttributes = `<!doctype html><html><head><style>
@page { size: 320px 220px; margin: 0 } body { margin: 0; font-family: sans-serif }
svg { display: block }
</style></head><body><div>
<svg width="320" height="220" font-family="sans-serif">
 <text x="10" y="20" font-size="22.4px" font-weight="bold" fill="black">Sprzedaż</text>
 <g transform="translate(30,30)">
  <rect fill="steelblue" x="0" y="80" width="40" height="70"/>
  <rect fill="#e07b39" x="50" y="40" width="40" height="110"/>
  <rect fill="crimson" stroke="black" stroke-width="2" x="100" y="20" width="40" height="130"/>
  <rect fill="#e07b39" x="150" y="60" width="40" height="90"/>
 </g>
 <g transform="translate(30,180)" fill="none" stroke="#224" shape-rendering="crispEdges">
  <path d="M0,0H250"/><line x1="20" y1="0" x2="20" y2="5"/><line x1="70" y1="0" x2="70" y2="5"/>
  <g stroke="none" fill="#224" font-size="10px" text-anchor="middle">
  <text x="20" y="18">I</text><text x="70" y="18">II</text><text x="120" y="18">III</text><text x="170" y="18">IV</text>
  </g>
 </g>
</svg></div></body></html>`

// TestInlineSVGDocumentCascade sprawdza, że do SVG osadzonego w HTML stosują się arkusze dokumentu
// i arkusz użytkownika (SVG 2 § 6.1, § 6.3) z kolejnością kaskady (CSS Cascade 4 § 6), atrybuty
// prezentacyjne jako reguły autora o specyficzności 0 (SVG 2 § 6.2) i dziedziczenie po rodzicu HTML.
// Każdy przypadek porównuje obraz z zapisem samymi atrybutami prezentacyjnymi.
func TestInlineSVGDocumentCascade(t *testing.T) {
	dir := t.TempDir()
	render := func(name, doc, user string) []byte {
		t.Helper()
		result, err := gopdfgo.Render(context.Background(), []byte(doc), gopdfgo.Options{Fonts: testFonts(t), UserCSS: user})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	head := `<!doctype html><style>@page { size: 200px 100px; margin: 0 } body { margin: 0 } svg { display: block }`
	same := func(name, style, body, user, reference string) {
		t.Helper()
		got := render(name, head+style+"</style>"+body, user)
		want := render(name+"-ref", head+"</style>"+reference, "")
		if n := differentPixels(got, want); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	svg := func(content string) string { return `<svg width="200" height="100">` + content + `</svg>` }

	// wykres stylowany arkuszem strony
	if n := differentPixels(render("chart", chartPage, ""), render("chart-ref", chartAttributes, "")); n != 0 {
		t.Errorf("chart: %d pikseli różni się od wzorca", n)
	}

	// selektor przekraczający granicę HTML/SVG i specyficzność: późniejsza reguła o mniejszej
	// specyficzności przegrywa
	same("cross-boundary", `div.r svg rect#a { fill: green } rect { fill: red }`,
		`<div class=r>`+svg(`<rect id=a width=50 height=50 />`)+`</div>`, "",
		svg(`<rect width=50 height=50 fill=green />`))
	// atrybut prezentacyjny przegrywa z każdą regułą autora, wygrywa z regułą użytkownika
	same("presentation-vs-author", `rect { fill: green }`, svg(`<rect width=50 height=50 fill=red />`), "",
		svg(`<rect width=50 height=50 fill=green />`))
	same("presentation-vs-user", ``, svg(`<rect width=50 height=50 fill=green />`), `rect { fill: red }`,
		svg(`<rect width=50 height=50 fill=green />`))
	same("user-without-attribute", ``, svg(`<rect width=50 height=50 />`), `rect { fill: green }`,
		svg(`<rect width=50 height=50 fill=green />`))
	// atrybut style nad zwykłą regułą autora, !important autora nad atrybutem style,
	// !important użytkownika nad wszystkim
	same("style-attribute", `#a { fill: red }`, svg(`<rect id=a width=50 height=50 style="fill: green"/>`), "",
		svg(`<rect width=50 height=50 fill=green />`))
	same("author-important", `#a { fill: green !important }`, svg(`<rect id=a width=50 height=50 style="fill: red"/>`), "",
		svg(`<rect width=50 height=50 fill=green />`))
	same("user-important", `#a { fill: red !important }`, svg(`<rect id=a width=50 height=50 style="fill: red !important"/>`),
		`rect { fill: green !important }`, svg(`<rect width=50 height=50 fill=green />`))
	// dziedziczenie po przodkach HTML: color dla currentColor, fill z reguły dla <body>
	same("inherit-color", `body { color: green } svg { fill: currentColor }`, svg(`<rect width=50 height=50 />`), "",
		svg(`<rect width=50 height=50 fill=green />`))
	same("inherit-fill", `body { fill: green; stroke: blue; stroke-width: 4px }`, `<div>`+svg(`<rect x=10 y=10 width=50 height=50 />`)+`</div>`, "",
		svg(`<rect x=10 y=10 width=50 height=50 fill=green stroke=blue stroke-width=4 />`))
	// zmienne CSS i rozmiar pisma: em względem rodzica HTML, bez mnożenia na każdym poziomie
	same("variables", `:root { --accent: green } .bar { fill: var(--accent) }`, svg(`<rect class=bar width=50 height=50 />`), "",
		svg(`<rect width=50 height=50 fill=green />`))
	same("font-size-em", `body { font-size: 10px }`, svg(`<g font-size="2em"><g><g><rect width="1em" height="1em" fill=green /></g></g></g>`), "",
		svg(`<rect width=20 height=20 fill=green />`))
	// <style> wewnątrz <svg> to także arkusz dokumentu: dotyczy elementów HTML i ustępuje
	// bardziej specyficznym regułom z <head>
	same("svg-style-element", `.chart rect { fill: green }`, `<svg class=chart width=200 height=100><style>rect { fill: red } p.x { height: 0 }</style><rect width=50 height=50 /></svg><p class=x>`, "",
		svg(`<rect width=50 height=50 fill=green />`))
	// szerokość i wysokość z CSS ustala pudełko <svg>, nie rozmiar obrazu: viewBox nadal skaluje treść
	same("root-box-size", `svg.s { width: 100px; height: auto }`, `<svg class=s viewBox="0 0 20 10"><rect width=10 height=10 fill=green /></svg>`, "",
		`<svg width=100 height=50 viewBox="0 0 20 10"><rect width=10 height=10 fill=green /></svg>`)

	// SVG jako obraz to osobny dokument: arkusz strony go nie dotyczy
	image := base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="50" height="50"><rect width="50" height="50" fill="green"/></svg>`))
	same("image-isolated", `rect { fill: red } body { fill: red }`, `<img src="data:image/svg+xml;base64,`+image+`" style="display:block">`, "",
		svg(`<rect width=50 height=50 fill=green />`))
}
