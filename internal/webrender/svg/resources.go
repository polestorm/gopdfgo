package svg

import (
	"regexp"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/matrix"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// svgElement to typ elementu <svg> (nazwa svg jest zajęta przez odbiorniki metod)
type svgElement = svg

// Zasoby zewnętrzne samodzielnego dokumentu SVG (gopdfgo).
//
// Dokument SVG wyświetlany jako obraz (<img>, obraz CSS) działa w trybie „secure static”
// (SVG Integration § 4.2; HTML § 4.8.4.4): nie wczytuje zasobów zewnętrznych, czyli innych niż
// adresy data:. Ten sam dokument w <object> albo <embed> jest zwykłym dokumentem (tryb „dynamic
// interactive”) i wczytuje je (test WPT external-resource-inline-sheet). Obraz w pamięci podręcznej
// jest wspólny dla obu sposobów osadzenia, więc o użyciu zasobu decydujemy przy rysowaniu
// (documentMode), a nie przy wczytywaniu. Źródło wczytywało zasoby zawsze.

// externalResource mówi, czy adres wskazuje zasób zewnętrzny (inny niż data:).
func externalResource(url string) bool {
	return !strings.HasPrefix(strings.ToLower(strings.TrimSpace(url)), "data:")
}

// loadsExternal mówi, czy obraz rysowany teraz może użyć zasobów zewnętrznych: SVG osadzone w HTML
// i dokument z <object>/<embed> tak, obraz w trybie statycznym nie.
func (svg *SVGImage) loadsExternal() bool {
	return !svg.standalone || svg.documentMode
}

var cssURLRe = regexp.MustCompile(`(?i)\burl\(\s*(?:"([^"]*)"|'([^']*)'|([^)\s]*))\s*\)`)

// rootBackgroundImage odczytuje adres pierwszej warstwy background-image elementu głównego (także
// z background) i rozwiązuje go względem baseURL.
func rootBackgroundImage(root *cascadedNode, baseURL string) string {
	if root == nil {
		return ""
	}
	for _, name := range []string{"background-image", "background"} {
		m := cssURLRe.FindStringSubmatch(root.attrs[name])
		if m == nil {
			continue
		}
		url := m[1] + m[2] + m[3]
		if url == "" {
			return ""
		}
		joined, err := utils.SafeUrljoin(baseURL, url, true)
		if err != nil {
			return ""
		}
		return joined
	}
	return ""
}

// drawBackgroundImage rysuje obraz tła elementu głównego na płótnie dokumentu (CSS Backgrounds 3
// § 2.11.2): w rozmiarze własnym, powtarzany od lewego górnego rogu (wartości początkowe
// background-repeat, -position i -size; inne wartości nie są obsługiwane).
func (svg *SVGImage) drawBackgroundImage(dst backend.Canvas, width, height Fl) {
	if svg.backgroundImageURL == "" || svg.imageLoader == nil {
		return
	}
	if externalResource(svg.backgroundImageURL) && !svg.loadsExternal() {
		return
	}
	if !svg.backgroundLoaded {
		svg.backgroundLoaded = true
		img, err := svg.imageLoader(svg.backgroundImageURL)
		if err != nil {
			logger.WarningLogger.Printf("SVG background image: %s", err)
		}
		svg.backgroundImage = img
	}
	if svg.backgroundImage == nil {
		return
	}
	iw, ih, ratio := svg.backgroundImage.GetIntrinsicSize(1, pr.Float(defaultFontSize))
	var w, h Fl
	switch {
	case iw != nil && ih != nil:
		w, h = Fl(iw.V()), Fl(ih.V())
	case iw != nil && ratio != nil && ratio.V() > 0:
		w = Fl(iw.V())
		h = w / Fl(ratio.V())
	case ih != nil && ratio != nil:
		h = Fl(ih.V())
		w = h * Fl(ratio.V())
	default:
		w, h = width, height
	}
	if !(w > 0 && h > 0) {
		return
	}
	dst.OnNewStack(func() {
		dst.Rectangle(0, 0, width, height)
		dst.State().Clip(false)
		for y := Fl(0); y < height; y += h {
			for x := Fl(0); x < width; x += w {
				dst.OnNewStack(func() {
					dst.State().Transform(matrix.Translation(x, y))
					svg.backgroundImage.Draw(dst, svg.textContext, w, h, "auto")
				})
			}
		}
	})
}

// drawIgnoringRootAspectRatio rysuje dokument SVG wstawiony elementem <image>: preserveAspectRatio
// elementu głównego wstawianego dokumentu jest pomijany, jakby miał wartość none, bo dopasowanie
// do prostokąta robi już preserveAspectRatio elementu <image> (SVG 2 § 12.3; test WPT
// image-preserveaspectratio-ignores-svg-root-viewbox). Źródło stosowało oba.
func (svg *SVGImage) drawIgnoringRootAspectRatio(dst backend.Canvas, width, height Fl, textContext text.TextLayoutContext, hostBaseScale Fl) {
	if svg.root != nil {
		if s, ok := svg.root.graphicContent.(svgElement); ok && !s.preserveRatio.none {
			s.preserveRatio.none = true
			svg.root.graphicContent = s
			defer func() {
				s.preserveRatio.none = false
				svg.root.graphicContent = s
			}()
		}
	}
	svg.draw(dst, width, height, textContext, hostBaseScale)
}
