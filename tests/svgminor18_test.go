package tests

import (
	"context"
	"os"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
)

// TestSVGMinorFeatures sprawdza mniejsze cechy SVG 2: rx/ry = auto (§ 10.2, § 10.4), currentColor
// dziedziczone jako słowo kluczowe, znaczniki (kierunki wierzchołków § 13.7.3, podścieżki niejawne,
// context-fill/context-stroke § 13.4.1, markerUnits przy non-scaling-stroke), maski (domyślne
// objectBoundingBox, maskContentUnits, maska na <g>) i tekst (text-transform, znaki łączące na
// listach pozycji, ukryty tekst przesuwający pozycję).
func TestSVGMinorFeatures(t *testing.T) {
	head := `<style>@page { size: 200px 120px; margin: 0 } body { margin: 0 } svg { display: block }</style>`
	dir := t.TempDir()
	fonts := testFonts(t)
	ahem, err := os.ReadFile("../testdata/wpt/fonts/Ahem.ttf")
	if err != nil {
		t.Fatal(err)
	}
	fonts = append([]htmlpdf.Font{{Name: "Ahem.ttf", Data: ahem}}, fonts...)
	page := func(name, body string) []byte {
		t.Helper()
		result, err := htmlpdf.Render(context.Background(), []byte(head+body),
			htmlpdf.Options{Fonts: fonts, Resolver: htmlpdf.DenyAll})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	// limit: liczba pikseli różniących się wygładzeniem krawędzi
	within := func(name, test, reference string, limit int) {
		t.Helper()
		if n := differentPixels(page(name, test), page(name+"-ref", reference)); n > limit {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	same := func(name, test, reference string) {
		t.Helper()
		within(name, test, reference, 0)
	}
	green := `<div style="width:100px;height:100px;background:green"></div>`

	// rx="auto" przyjmuje bezwzględną wartość ry (40% z 100 = 40), także w clipPath
	same("ellipse-auto-rx", `<svg width="200" height="100"><clipPath id="c"><ellipse cx="50" cy="50" rx="auto" ry="40%"/></clipPath>
		<rect width="200" height="100" fill="green" clip-path="url(#c)"/></svg>`,
		`<svg width="200" height="100"><circle cx="50" cy="50" r="40" fill="green"/></svg>`)
	// ry pominięte w <ellipse> to auto; <rect> z ry="0" nie ma zaokrągleń mimo rx (źródło brało ry = rx),
	// a rx="auto" przyjmuje ry
	same("ellipse-ry-omitted", `<svg width="200" height="100"><ellipse cx="50" cy="50" rx="30" fill="green"/>
		<rect x="100" width="80" height="60" rx="30" ry="0" fill="green"/></svg>`,
		`<svg width="200" height="100"><circle cx="50" cy="50" r="30" fill="green"/>
		<rect x="100" width="80" height="60" fill="green"/></svg>`)
	same("rect-auto-rx", `<svg width="100" height="100"><rect width="100" height="100" rx="auto" ry="20" fill="green"/></svg>`,
		`<svg width="100" height="100"><rect width="100" height="100" rx="20" ry="20" fill="green"/></svg>`)
	// currentColor w <pattern> dziedziczy się jako słowo i rozwiązuje względem 'color' treści wzorca
	same("currentcolor-pattern", `<svg width="100" height="100"><pattern id="p" width="100" height="100" patternUnits="userSpaceOnUse"
		color="red" fill="currentcolor"><rect width="100" height="100" color="green" fill="inherit"/></pattern>
		<rect width="100" height="100" fill="url(#p)"/></svg>`, green)
	// zastępczy kolor po url(...) też dziedziczy currentColor jako słowo
	same("currentcolor-fallback", `<svg width="100" height="100"><g fill="url(#none) currentColor" color="red">
		<rect width="100" height="100" color="green"/></g></svg>`, green)
	// zawrócenie o 180° w wierzchołku: znacznik prostopadły (kąt wejścia minus 90°)
	within("marker-orient-180", `<svg width="100" height="100"><marker id="m" markerWidth="100" markerHeight="50"
		markerUnits="userSpaceOnUse" orient="auto" refX="50" refY="50"><path d="M50,-5L105,50h-110z" fill="green"/></marker>
		<g marker-start="url(#m)"><path d="M50,0v50z"/><path d="M100,50h-50z"/><path d="M50,100v-50z"/><path d="M0,50h50z"/></g></svg>`,
		green, 20)
	// Z po Z tworzy niejawną podścieżkę z własnym wierzchołkiem (marker-mid)
	same("marker-implicit-subpath", `<svg width="100" height="100"><marker id="m" markerUnits="userSpaceOnUse" overflow="visible">
		<rect width="100" height="100" fill="green"/></marker><rect width="100" height="100" fill="red"/>
		<path d="M0,0ZZZ" marker-mid="url(#m)"/></svg>`, green)
	// orient z jednostką kąta i obrót znacznika na końcu odcinka
	same("marker-orient-turn", `<svg width="100" height="100"><marker id="m" markerWidth="100" markerHeight="100"
		markerUnits="userSpaceOnUse" orient="0.25turn"><rect width="100" height="100" fill="green"/></marker>
		<path d="M100,0h-1" marker-start="url(#m)"/></svg>`, green)
	// łuk to jeden odcinek: marker-mid tylko w wierzchołku między łukami
	same("marker-arc-vertices", `<svg width="100" height="100"><marker id="m" markerWidth="20" markerHeight="20"
		markerUnits="userSpaceOnUse" refX="10" refY="10"><rect width="20" height="20" fill="green"/></marker>
		<path d="M10,50A40,40 0 0 1 90,50A40,40 0 0 1 10,50" fill="none" marker-mid="url(#m)"/></svg>`,
		`<svg width="100" height="100"><rect x="80" y="40" width="20" height="20" fill="green"/></svg>`)
	// context-fill w znaczniku i w treści <use>
	same("context-fill", `<svg width="100" height="100"><marker id="m" markerWidth="50" markerHeight="100" markerUnits="userSpaceOnUse">
		<rect width="50" height="100" fill="context-stroke"/></marker><symbol id="s"><rect width="50" height="100" fill="context-fill"/></symbol>
		<path d="M50,0h1" stroke="green" stroke-width="0.001" marker-start="url(#m)"/><use href="#s" fill="green"/></svg>`, green)
	// markerUnits="strokeWidth" przy non-scaling-stroke: grubość w układzie użytkownika
	same("marker-non-scaling", `<svg width="200" height="120"><marker id="m" orient="0" refY="5" markerWidth="10" markerHeight="10">
		<rect width="10" height="10" fill="green"/></marker><line x2="10" y1="5" y2="5" vector-effect="non-scaling-stroke"
		stroke="red" stroke-width="10" marker-start="url(#m)" transform="scale(10)"/></svg>`, green)
	// maska z maskContentUnits="objectBoundingBox" i domyślnym obszarem objectBoundingBox
	same("mask-content-bbox", `<svg width="200" height="100"><mask id="m" maskContentUnits="objectBoundingBox">
		<rect width="0.5" height="1" fill="white"/></mask><rect x="100" width="200" height="100" fill="green" mask="url(#m)"/></svg>`,
		`<svg width="200" height="100"><rect x="100" width="100" height="100" fill="green"/></svg>`)
	// maska na <g> obejmuje potomków
	same("mask-group", `<svg width="100" height="100"><mask id="m" maskUnits="userSpaceOnUse"><rect width="100" height="50" fill="white"/></mask>
		<rect y="50" width="100" height="50" fill="green"/><g mask="url(#m)"><rect width="100" height="50" fill="green"/>
		<rect y="50" width="100" height="50" fill="red"/></g></svg>`, green)
	// text-transform w tekście SVG
	same("text-transform", `<svg width="200" height="100"><text y="50" font-size="20" style="text-transform: uppercase">Ab ç</text>
		<text y="80" font-size="20" style="text-transform: capitalize">ab, çd</text></svg>`,
		`<svg width="200" height="100"><text y="50" font-size="20">AB Ç</text><text y="80" font-size="20">Ab, Çd</text></svg>`)
	// znak łączący nie dostaje własnej wartości z listy x, a lista jest indeksowana znakami
	same("text-combining-x", `<svg width="200" height="100"><text x="10 60 110" y="50" style="font: 20px Ahem">a&#x301;b</text></svg>`,
		`<svg width="200" height="100"><text x="10" y="50" style="font: 20px Ahem">a&#x301;</text><text x="110" y="50" style="font: 20px Ahem">b</text></svg>`)
	// ukryty tekst przesuwa pozycję dla widocznego tspan
	same("text-hidden-advance", `<svg width="200" height="100"><text x="10" y="50" visibility="hidden" style="font: 20px Ahem" fill="green">ab<tspan visibility="visible">c</tspan></text></svg>`,
		`<svg width="200" height="100"><text x="50" y="50" style="font: 20px Ahem" fill="green">c</text></svg>`)
}
