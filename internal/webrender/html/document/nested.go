package document

import (
	"fmt"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/layout"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
	"github.com/polestorm/gopdfgo/internal/webrender/images"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/svg"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/text/hyphen"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// gopdfgo: dokumenty osadzone w <iframe>, <object> i <embed> (zob. boxes.NestedDocument) oraz treść
// <foreignObject> w SVG (zob. svg.ForeignDocument).

// maxNestedDepth ogranicza zagnieżdżanie dokumentów; ramka wskazująca na własny dokument kończy się
// pustym prostokątem zamiast nieskończonej rekursji.
const maxNestedDepth = 3

// nestedDepth to bieżąca głębokość zagnieżdżenia; renderowania idą po kolei (gopdfgo.Render), a rysowanie
// dokumentu osadzonego odbywa się w trakcie rysowania strony rodzica.
var nestedDepth int

func init() {
	bo.NestedDocument = nestedDocument
	svg.ForeignDocument = drawNestedDocument
}

// nestedDocument zwraca obraz o rozmiarze wewnętrznym 300 na 150 (HTML § 15.4.1, rozmiar domyślny
// ramki), który przy rysowaniu układa source jako dokument o szerokości i wysokości ramki i rysuje
// jego pierwszą stronę przyciętą do ramki. Tak drukuje Chromium: treść ramki, która się nie mieści,
// jest ucinana, a nie przenoszona na kolejne strony.
func nestedDocument(source []byte, baseURL string, fetch utils.UrlFetcher) images.Image {
	return images.Drawn{Width: 300, Height: 150, Paint: func(dst backend.Canvas, fonts text.FontConfiguration, width, height pr.Fl) {
		drawNestedDocument(dst, fonts, source, baseURL, fetch, width, height, true)
	}}
}

// drawNestedDocument układa source jako dokument o stronie width na height i rysuje jego pierwszą
// stronę w prostokącie (0, 0, width, height) płótna dst, przyciętą do niego, gdy clip. Służy też
// treści <foreignObject> w SVG (svg.ForeignDocument).
func drawNestedDocument(dst backend.Canvas, fonts text.FontConfiguration, source []byte, baseURL string, fetch utils.UrlFetcher, width, height pr.Fl, clip bool) {
	if nestedDepth >= maxNestedDepth || width <= 0 || height <= 0 {
		return
	}
	nestedDepth++
	defer func() { nestedDepth-- }()

	html, err := tree.NewHTML(utils.InputString(string(source)), baseURL, fetch, "print")
	if err != nil {
		logger.WarningLogger.Printf("embedded document %q: %s", baseURL, err)
		return
	}
	// strona o rozmiarze ramki, bez marginesów; wysokość rośnie z treścią (tryb ciągły), a nadmiar
	// ucina przycięcie poniżej; bez pomniejszania treści szerszej niż ramka
	html.Behavior = tree.Behavior{Continuous: true}
	page, err := tree.NewCSSExt(utils.InputString(fmt.Sprintf("@page { size: %.4fpx %.4fpx; margin: 0 }", width, height)), baseURL, fonts)
	if err != nil {
		return
	}
	pages := layout.Layout(html, []tree.CSS{page}, true, fonts)
	if len(pages) == 0 {
		return
	}
	dst.OnNewStack(func() {
		if clip {
			dst.Rectangle(0, 0, width, height)
			dst.State().Clip(false)
		}
		ctx := drawContext{
			dst:               dst,
			fonts:             fonts,
			hyphenCache:       make(map[text.HyphenDictKey]hyphen.Hyphener),
			strutLayoutsCache: make(map[text.StrutLayoutKey][2]pr.Float),
		}
		ctx.drawPage(pages[0])
	})
}
