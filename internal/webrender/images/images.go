// Fetch and decode images in range various formats.
package images

import (
	"fmt"
	"image"
	"io"
	"math"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	"github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/svg"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
	"golang.org/x/net/html"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

type Color = parser.RGBA

// Image is the common interface for supported image formats,
// such as gradients, SVG, or JPEG, PNG, etc...
type Image interface {
	backend.Image

	isImage()
}

var (
	_ Image = rasterImage{}
	_ Image = SVGImage{}
	_ Image = LinearGradient{}
	_ Image = ConicGradient{}
	_ Image = RadialGradient{}
)

// An error occured when loading an image.
// The image data is probably corrupted or in an invalid format.
func imageLoadingError(err error) error {
	return fmt.Errorf("error loading image : %s", err)
}

// Cache stores the result of fetching an image.
type Cache map[string]Image

func NewCache() Cache { return make(Cache) }

// Gets an image from an image URI.
// In case of an error, a log is printed and nil is returned
func GetImageFromUri(cache Cache, fetcher utils.UrlFetcher, optimizeSize bool, url, forcedMimeType string, orientation pr.SBoolFloat) Image {
	res, in := cache[url]
	if in {
		return res
	}

	img, err := getImageFromUri(fetcher, optimizeSize, url, forcedMimeType, orientation)

	cache[url] = img

	if err != nil {
		logger.WarningLogger.Println(err)
	}

	return img
}

// without caching
func getImageFromUri(fetcher utils.UrlFetcher, optimizeSize bool, url, forcedMimeType string, orientation pr.SBoolFloat) (Image, error) {
	var (
		img     Image
		err     error
		content utils.RemoteRessource
	)

	content, err = fetcher(url)
	if err != nil {
		err = fmt.Errorf(`failed to load image at "%s" (%s)`, url, err)
		return nil, err
	}

	mimeType := forcedMimeType
	if mimeType == "" {
		mimeType = content.MimeType
	}

	var errSvg error
	// Try to rely on given mimetype for SVG
	if mimeType == "image/svg+xml" {
		var svgIm SVGImage
		svgIm, errSvg = NewSVGImage(content.Content, url, fetcher)
		if errSvg == nil {
			img = svgIm
		}
	}

	// Look for raster images, or for failing SVG
	if img == nil {
		content.Content.Seek(0, io.SeekStart)
		imageConfig, imageFormat, errRaster := image.DecodeConfig(content.Content)
		if errRaster != nil {
			if errSvg != nil { // Tried SVGImage then raster for a SVG, abort
				err = fmt.Errorf(`failed to load image at "%s" (%s)`, url, errSvg)
				return nil, err
			}

			// Last chance, try SVG in case mime type is incorrect
			content.Content.Seek(0, io.SeekStart)
			img, errSvg = NewSVGImage(content.Content, url, fetcher)
			if errSvg != nil {
				err = fmt.Errorf(`failed to load image at "%s" (%s)`, url, errRaster)
				return nil, err
			}
		} else {
			content.Content.Seek(0, io.SeekStart)
			img = newRasterImage(imageConfig, content.Content, "image/"+imageFormat, utils.Hash(url), optimizeSize, orientation)
		}
	}

	return img, err
}

type rasterImage struct {
	image backend.RasterImage

	intrinsicRatio  pr.Float
	intrinsicWidth  pr.Float
	intrinsicHeight pr.Float
	optimizeSize    bool
}

func newRasterImage(imageConfig image.Config, content io.Reader, mimeType string, id int, optimizeSize bool, _ pr.SBoolFloat) rasterImage {
	self := rasterImage{}
	self.optimizeSize = optimizeSize
	self.image.Content = content
	self.image.MimeType = mimeType
	self.image.ID = id
	self.intrinsicWidth = pr.Float(imageConfig.Width)
	self.intrinsicHeight = pr.Float(imageConfig.Height)
	self.intrinsicRatio = pr.Inf
	if self.intrinsicHeight != 0 {
		self.intrinsicRatio = self.intrinsicWidth / self.intrinsicHeight
	}
	return self
}

func (r rasterImage) isImage() {}

func (r rasterImage) GetIntrinsicSize(imageResolution, _ pr.Float) (width, height, ratio pr.MaybeFloat) {
	// Raster images are affected by the "image-resolution" property.
	return r.intrinsicWidth / imageResolution, r.intrinsicHeight / imageResolution, r.intrinsicRatio
}

func (r rasterImage) Draw(context backend.Canvas, _ text.TextLayoutContext, concreteWidth, concreteHeight pr.Fl, imageRendering string) {
	hasSize := concreteWidth > 0 && concreteHeight > 0 && r.intrinsicWidth > 0 && r.intrinsicHeight > 0
	if !hasSize {
		return
	}

	r.image.Rendering = string(imageRendering)
	context.DrawRasterImage(r.image, concreteWidth, concreteHeight)
}

type SVGImage struct {
	icon *svg.SVGImage
	// asDocument (gopdfgo): obraz z <object> albo <embed>, rysowany jak osobny dokument (zob. AsDocument)
	asDocument bool
	// inline (gopdfgo): element <svg> osadzony w HTML (NewSVGImageFromNode); jego treść zależy od kaskady
	// i kontekstu dokumentu, więc nie jest współdzielona między wystąpieniami (zob. Draw)
	inline bool
}

// AsDocument (gopdfgo) zwraca kopię obrazu rysowaną jak osobny dokument SVG (<object>, <embed>), bez
// skalowania pliku bez viewBox; obraz w pamięci podręcznej, wspólny z <img>, zostaje bez zmian.
func (img SVGImage) AsDocument() SVGImage {
	img.asDocument = true
	return img
}

func (SVGImage) isImage() {}

func NewSVGImage(svgData io.Reader, baseURL string, urlFetcher utils.UrlFetcher) (SVGImage, error) {
	// don’t pass data URIs: they are useless for relative URIs anyway.
	if strings.HasPrefix(strings.ToLower(baseURL), "data:") {
		baseURL = ""
	}

	imageLoader := func(url string) (backend.Image, error) {
		return getImageFromUri(urlFetcher, false, url, "", pr.SBoolFloat{})
	}

	var err error
	icon, err := svg.Parse(svgData, baseURL, imageLoader, urlFetcher)
	if err != nil {
		return SVGImage{}, imageLoadingError(err)
	}
	return SVGImage{icon: icon}, nil
}

// doc (gopdfgo) to kaskada dokumentu HTML, w którym leży element (zob. svg.DocumentStyle); może być nil.
func NewSVGImageFromNode(node *html.Node, baseURL string, urlFetcher utils.UrlFetcher, doc *svg.DocumentStyle) (SVGImage, error) {
	// don’t pass data URIs: they are useless for relative URIs anyway.
	if strings.HasPrefix(strings.ToLower(baseURL), "data:") {
		baseURL = ""
	}

	imageLoader := func(url string) (backend.Image, error) {
		return getImageFromUri(urlFetcher, false, url, "", pr.SBoolFloat{})
	}

	var err error
	icon, err := svg.ParseInlineNode(node, baseURL, imageLoader, urlFetcher, doc)
	if err != nil {
		return SVGImage{}, imageLoadingError(err)
	}
	return SVGImage{icon: icon, inline: true}, nil
}

// LinkAreas (gopdfgo) zwraca obszary odnośników <a> obrazu narysowanego w rozmiarze width na height
// (zob. svg.SVGImage.LinkAreas; woła się po Draw), według sposobu osadzenia (ADR 0029):
//   - SVG osadzone w HTML należy do dokumentu: odnośniki zewnętrzne i wewnętrzne (#nazwa, kotwice HTML);
//   - SVG w <object> i <embed> to osobny, interaktywny dokument (HTML § 4.8.6, 4.8.7; SVG Integration,
//     tryb „dynamic interactive”): tylko odnośniki zewnętrzne, bo fragment wskazuje miejsce w samym SVG;
//   - SVG w <img> i jako obraz CSS jest wyświetlane w trybie statycznym bez interakcji (SVG Integration,
//     „secure static mode”; HTML § 4.8.4.4): bez odnośników.
//
// Ramki tekstu pochodzą ze składu przy ostatnim rysowaniu, dlatego <object> i <embed> z odnośnikami nie
// korzystają z formy współdzielonej (zob. Draw): każde wystąpienie składa tekst w swoim rozmiarze.
func (img SVGImage) LinkAreas(width, height pr.Fl) []svg.LinkArea {
	if !img.inline && !img.asDocument {
		return nil
	}
	areas := img.icon.LinkAreas(width, height, img.asDocument)
	if img.inline {
		return areas
	}
	external := areas[:0]
	for _, area := range areas {
		if !strings.HasPrefix(area.Href, "#") {
			external = append(external, area)
		}
	}
	return external
}

func (s SVGImage) GetIntrinsicSize(_, fontSize pr.Float) (pr.MaybeFloat, pr.MaybeFloat, pr.MaybeFloat) {
	width, height := s.icon.DisplayedSize()

	var intrinsicWidth, intrinsicHeight, ratio pr.MaybeFloat
	if width.U != svg.Perc {
		intrinsicWidth = pr.Float(width.Resolve(pr.Fl(fontSize), 0))
	}
	if height.U != svg.Perc {
		intrinsicHeight = pr.Float(height.Resolve(pr.Fl(fontSize), 0))
	}

	// gopdfgo: ujemna szerokość lub wysokość jest błędna i działa jak auto (SVG 2 § 8.2, atrybuty width
	// i height elementu <svg>), a proporcje z width i height zdegenerowane przez zero ustępują
	// proporcjom z viewBox (SVG 2 „Sizing SVG in CSS”, CSS Values 4 § 7.3, proporcje zdegenerowane;
	// testy WPT outer-svg-intrinsic-size-003…005). Źródło brało ujemne wymiary dosłownie, a przy
	// width="0" zwracało proporcje 0.
	// (pr.Is traktuje zero jak brak wartości, stąd porównania z nil)
	if intrinsicWidth != nil && intrinsicWidth.V() < 0 {
		intrinsicWidth = nil
	}
	if intrinsicHeight != nil && intrinsicHeight.V() < 0 {
		intrinsicHeight = nil
	}
	if intrinsicWidth != nil && intrinsicHeight != nil && (intrinsicWidth.V() == 0 || intrinsicHeight.V() == 0) {
		if viewbox := s.icon.ViewBox(); viewbox != nil && viewbox.Width > 0 && viewbox.Height > 0 {
			ratio = pr.Float(viewbox.Width / viewbox.Height)
		}
		return intrinsicWidth, intrinsicHeight, ratio
	}

	if intrinsicWidth == nil || intrinsicHeight == nil {
		viewbox := s.icon.ViewBox()
		if viewbox != nil && viewbox.Width != 0 && viewbox.Height != 0 {
			ratio = pr.Float(viewbox.Width / viewbox.Height)
			if pr.Is(intrinsicWidth) {
				intrinsicHeight = intrinsicWidth.V() / ratio.V()
			} else if pr.Is(intrinsicHeight) {
				intrinsicWidth = intrinsicHeight.V() * ratio.V()
			}
		}
	} else if pr.Is(intrinsicWidth) && pr.Is(intrinsicHeight) {
		ratio = intrinsicWidth.V() / intrinsicHeight.V()
	}

	return intrinsicWidth, intrinsicHeight, ratio
}

// SVG (gopdfgo) udostępnia przetworzony obraz SVG rysującemu go elementowi <image> innego SVG
// (zob. svg.svgImageHolder).
func (img SVGImage) SVG() *svg.SVGImage { return img.icon }

// sharedSVGKey (gopdfgo): klucz formy współdzielonej obrazu SVG (zob. Draw). Wskaźnik jest tu tylko
// tożsamością obrazu w mapie wyszukiwania; mapa trzyma go przy życiu i nigdy nie jest przeglądana.
type sharedSVGKey struct {
	icon          *svg.SVGImage
	asDocument    bool
	width, height pr.Fl
}

// SVGSharingDisabled (gopdfgo) wyłącza współdzielenie treści obrazów SVG (zob. Draw); służy testom, które
// porównują wynik z rysowaniem każdego wystąpienia osobno.
var SVGSharingDisabled bool

// Draw rysuje obraz w rozmiarze konkretnym (concreteWidth, concreteHeight).
//
// gopdfgo: obraz SVG z pliku (img, object, embed, background-image, border-image, content: url()) jest
// dokumentem odrębnym od strony: nie dziedziczy jej kaskady ani currentColor (SVG Integration § 2.2,
// tryb „secure animated”), a image-rendering nie wpływa na grafikę wektorową. Wynik zależy więc tylko od
// obrazu, rozmiaru konkretnego i trybu dokumentu (object, embed), a układ gospodarza (hostScale) jest
// względny wobec miejsca rysowania. Taki obraz rysujemy raz do treści współdzielonej (backend.SharedDrawer,
// w PDF jedna forma XObject) przyciętej do obszaru widoku (0, 0, szerokość, wysokość), jak w przeglądarkach,
// które obraz SVG malują w granicach jego obszaru widoku; kolejne wystąpienia, także na innych stronach
// i w polach marginesów, tylko się do niej odwołują. Źródło rysowało i zapisywało obraz przy każdym
// wystąpieniu: herb (100 kB SVG) na 41 stronach dawał 1,5 MB PDF. SVG osadzony w HTML (inline) jest
// rysowany jak dotąd.
func (img SVGImage) Draw(dst backend.Canvas, textContext text.TextLayoutContext, concreteWidth, concreteHeight pr.Fl, imageRendering string) {
	if shared, ok := dst.(backend.SharedDrawer); ok && !SVGSharingDisabled && !img.inline && img.icon != nil &&
		!(img.asDocument && img.icon.HasLinks()) && // gopdfgo: zob. LinkAreas
		concreteWidth > 0 && concreteHeight > 0 && !math.IsInf(float64(concreteWidth), 0) && !math.IsInf(float64(concreteHeight), 0) {
		key := sharedSVGKey{icon: img.icon, asDocument: img.asDocument, width: concreteWidth, height: concreteHeight}
		shared.DrawShared(key, concreteWidth, concreteHeight, func(form backend.Canvas) {
			img.draw(form, textContext, concreteWidth, concreteHeight)
		})
		return
	}
	img.draw(dst, textContext, concreteWidth, concreteHeight)
}

// draw (gopdfgo) rysuje obraz wprost na dst.
func (img SVGImage) draw(dst backend.Canvas, textContext text.TextLayoutContext, concreteWidth, concreteHeight pr.Fl) {
	if img.asDocument {
		img.icon.DrawAsDocument(dst, concreteWidth, concreteHeight, textContext)
		return
	}
	img.icon.Draw(dst, concreteWidth, concreteHeight, textContext)
}
