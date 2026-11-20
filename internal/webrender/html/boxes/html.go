package boxes

import (
	"fmt"
	"strconv"
	"strings"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/css/validation"
	"github.com/polestorm/gopdfgo/internal/webrender/images"
	"github.com/polestorm/gopdfgo/internal/webrender/svg"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"

	"github.com/polestorm/gopdfgo/internal/webrender/utils"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type handlerFunction = func(element *utils.HTMLNode, box Box, resolver URLResolver, baseUrl string) []Box

// htmlHandlers map a tag name to a callback creating the boxes needed.
var htmlHandlers = map[string]handlerFunction{
	"img":      handleImg,
	"embed":    handleEmbed,
	"object":   handleObject,
	"colgroup": handleColgroup,
	"col":      handleCol,
	"svg":      handleSVG,
	"canvas":   handleCanvas,
	"iframe":   handleIframe,
}

// HandleElement handle HTML elements that need special care.
func handleElement(element *utils.HTMLNode, box Box, resolver URLResolver, baseUrl string) []Box {
	handler, in := htmlHandlers[box.Box().ElementTag()]
	if in {
		ls := handler(element, box, resolver, baseUrl)
		return ls
	}
	return []Box{box}
}

// handleCanvas (gopdfgo): <canvas> bez skryptów to przezroczysty element zastępowany o rozmiarze z atrybutów
// width i height, domyślnie 300 na 150 (HTML § 4.12.5); tło, obramowanie i rozmiary z CSS działają jak dla
// obrazu. W źródle był zwykłym pustym pudełkiem liniowym, więc nie zajmował miejsca.
func handleCanvas(element *utils.HTMLNode, box Box, resolver URLResolver, _ string) []Box {
	dimension := func(name string, fallback int) int {
		if value, err := strconv.Atoi(strings.TrimSpace(element.Get(name))); err == nil && value >= 0 {
			return value
		}
		return fallback
	}
	source := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d"></svg>`,
		dimension("width", 300), dimension("height", 150))
	img, err := images.NewSVGImage(strings.NewReader(source), "", resolver.Fetch)
	if err != nil {
		return []Box{box}
	}
	return []Box{makeReplacedBox(element, box, img)}
}

// handleIframe (gopdfgo): <iframe> to element zastępowany o rozmiarze domyślnym 300 na 150 i bez
// proporcji własnych (CSS 2.1 § 10.3.2, HTML § 4.8.5); zagnieżdżonego dokumentu nie rysujemy, ale
// ramka zajmuje swoje miejsce razem z tłem i obramowaniem. W źródle był pustym pudełkiem liniowym.
func handleIframe(element *utils.HTMLNode, box Box, resolver URLResolver, baseUrl string) []Box {
	// gopdfgo: treść ramki jest drukowana (HTML § 4.8.5): najpierw srcdoc, potem dokument spod src,
	// ułożony w rozmiarze ramki i przycięty do niej, jak drukuje Chromium. Źródło zostawiało pustą ramkę.
	if NestedDocument != nil {
		if srcdoc := element.Get("srcdoc"); srcdoc != "" {
			return []Box{makeReplacedBox(element, box, NestedDocument([]byte(srcdoc), baseUrl, resolver.Fetch))}
		}
		if src := element.GetUrlAttribute("src", baseUrl, false); src != "" {
			if data, ok := fetchHTMLDocument(resolver, src, "", true); ok {
				return []Box{makeReplacedBox(element, box, NestedDocument(data, src, resolver.Fetch))}
			}
			if image := resolver.FetchImage(src, "", box.Box().Style.GetImageOrientation()); image != nil {
				return []Box{makeReplacedBox(element, box, image)}
			}
		}
	}
	return []Box{makeReplacedBox(element, box, images.Blank{Width: 300, Height: 150})}
}

// asDocument (gopdfgo): SVG w <object> i <embed> jest osobnym dokumentem, a nie obrazem: plik bez viewBox
// nie skaluje się do rozmiaru elementu (zob. images.SVGImage.AsDocument).
func asDocument(image images.Image) images.Image {
	if svgImage, ok := image.(images.SVGImage); ok {
		return svgImage.AsDocument()
	}
	return image
}

// NestedDocument (gopdfgo) zamienia dokument HTML na obraz, który przy rysowaniu układa go w rozmiarze
// elementu i rysuje przycięty do niego (<iframe>, a także <object> i <embed> z dokumentem HTML).
// Ustawia ją pakiet document, który zna układ i rysowanie; nil oznacza brak obsługi.
var NestedDocument func(source []byte, baseURL string, fetch utils.UrlFetcher) images.Image

// isHTMLType (gopdfgo): typ MIME dokumentu HTML albo XHTML.
func isHTMLType(mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(strings.SplitN(mime, ";", 2)[0]))
	return mime == "text/html" || mime == "application/xhtml+xml"
}

// fetchHTMLDocument (gopdfgo) pobiera url, gdy wskazuje dokument HTML: według podanego typu, a przy
// braku typu według rozszerzenia adresu (.html, .htm, .xhtml) albo, gdy anyType, według typu zasobu.
func fetchHTMLDocument(resolver URLResolver, url, declaredType string, anyType bool) ([]byte, bool) {
	if NestedDocument == nil || resolver.Fetch == nil {
		return nil, false
	}
	path := strings.ToLower(strings.SplitN(strings.SplitN(url, "#", 2)[0], "?", 2)[0])
	byName := strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".htm") || strings.HasSuffix(path, ".xhtml")
	if declaredType != "" && !isHTMLType(declaredType) {
		return nil, false
	}
	if declaredType == "" && !byName && !anyType {
		return nil, false
	}
	resource, err := resolver.Fetch(url)
	if err != nil || resource.Content == nil {
		return nil, false
	}
	if declaredType == "" && !byName && !isHTMLType(resource.MimeType) {
		return nil, false
	}
	data := make([]byte, resource.Content.Len())
	_, _ = resource.Content.Read(data)
	return data, true
}

// Wrap an image in a replaced box.
//
// That box is either block-level or inline-level, depending on what the
// element should be.
func makeReplacedBox(element *utils.HTMLNode, box Box, image images.Image) Box {
	var newBox Box
	if box.Box().Style.GetDisplay().Has(pr.Block) {
		b := NewBlockReplacedBox(box.Box().Style, (*html.Node)(element), "", image)
		newBox = &b
	} else {
		b := NewInlineReplacedBox(box.Box().Style, (*html.Node)(element), "", image)
		newBox = &b
	}
	newBox.Box().StringSet = box.Box().StringSet
	newBox.Box().BookmarkLabel = box.Box().BookmarkLabel
	return newBox
}

// Handle “<img>“ elements, return either an image or the alt-text.
// See: http://www.w3.org/TR/html5/embedded-content-1.html#the-img-element
func handleImg(element *utils.HTMLNode, box Box, resolver URLResolver, baseUrl string) []Box {
	src := element.GetUrlAttribute("src", baseUrl, false)
	alt := element.Get("alt")
	if src != "" {
		image := resolver.FetchImage(src, "", box.Box().Style.GetImageOrientation())
		if image != nil {
			return []Box{makeReplacedBox(element, box, image)}
		}
		// Invalid image, use the alt-text.
		if alt != "" {
			box.Box().Children = []Box{TextBoxAnonymousFrom(box, alt)}
			return []Box{box}
		}
	} else {
		if alt != "" {
			box.Box().Children = []Box{TextBoxAnonymousFrom(box, alt)}
			return []Box{box}
		}
	}
	// The element represents nothing
	return nil
}

// Handle “<embed>“ elements, return either an image or nothing.
// See: https://www.w3.org/TR/html5/embedded-content-0.html#the-embed-element
func handleEmbed(element *utils.HTMLNode, box Box, resolver URLResolver, baseUrl string) []Box {
	src := element.GetUrlAttribute("src", baseUrl, false)
	type_ := strings.TrimSpace(element.Get("type"))
	if src != "" {
		// gopdfgo: <embed> z dokumentem HTML drukuje jego treść, jak <iframe> (HTML § 4.8.6)
		if data, ok := fetchHTMLDocument(resolver, src, type_, false); ok {
			return []Box{makeReplacedBox(element, box, NestedDocument(data, src, resolver.Fetch))}
		}
		image := resolver.FetchImage(src, type_, box.Box().Style.GetImageOrientation())
		if image != nil {
			return []Box{makeReplacedBox(element, box, asDocument(image))}
		}
	}
	// No fallback.
	return nil
}

// Handle “<object>“ elements, return either an image or the fallback
// content.
// See: https://www.w3.org/TR/html5/embedded-content-0.html#the-object-element
func handleObject(element *utils.HTMLNode, box Box, resolver URLResolver, baseUrl string) []Box {
	data := element.GetUrlAttribute("data", baseUrl, false)
	type_ := strings.TrimSpace(element.Get("type"))
	if data != "" {
		// gopdfgo: <object> z dokumentem HTML drukuje jego treść zamiast treści zastępczej (HTML § 4.8.7)
		if doc, ok := fetchHTMLDocument(resolver, data, type_, false); ok {
			return []Box{makeReplacedBox(element, box, NestedDocument(doc, data, resolver.Fetch))}
		}
		image := resolver.FetchImage(data, type_, box.Box().Style.GetImageOrientation())
		if image != nil {
			return []Box{makeReplacedBox(element, box, asDocument(image))}
		}
	}
	// The element’s children are the fallback.
	return []Box{box}
}

// Handle the “span“ attribute.
func handleColgroup(element *utils.HTMLNode, box Box, _ URLResolver, _ string) []Box {
	if box, ok := box.(*TableColumnGroupBox); ok { // leaf
		hasCol := false
		for _, child := range element.NodeChildren(true) {
			if child.DataAtom == atom.Col {
				hasCol = true
			}
		}
		if !hasCol {
			children := make([]Box, box.span())
			for i := range children {
				children[i] = TableColumnBoxAnonymousFrom(box, nil)
			}
			box.Box().Children = children
		}
	}
	return []Box{box}
}

// Handle the “span“ attribute.
func handleCol(_ *utils.HTMLNode, box Box, _ URLResolver, _ string) []Box {
	if box, ok := box.(*TableColumnBox); ok { // leaf
		if span := box.span(); span > 1 {
			// Generate multiple boxes
			// http://lists.w3.org/Archives/Public/www-style/2011Nov/0293.html
			out := make([]Box, span)
			for i := range out {
				out[i] = box.Copy()
			}
			return out
		}
	}
	return []Box{box}
}

// handle the inline <svg> elements
// Return either an image or the fallback content.
func handleSVG(element *utils.HTMLNode, box Box, resolver URLResolver, baseUrl string) []Box {
	return handleInlineSVG(element, box, resolver, baseUrl, nil)
}

// svgCascade (gopdfgo) to kaskada dokumentu dla SVG osadzonego w HTML (tree.StyleFor).
type svgCascade interface {
	SVGDeclarations(element *html.Node) []validation.RawDeclaration
	SVGInherited(svgElement *html.Node) (map[string]string, float64)
}

// handleInlineSVG (gopdfgo) to handleSVG z kaskadą dokumentu: arkusze strony i arkusz użytkownika
// dotyczą elementów osadzonego <svg>, a jego element główny dziedziczy po rodzicu HTML (SVG 2 § 6).
// Źródło stylowało osadzone SVG tylko jego własnymi elementami <style>.
func handleInlineSVG(element *utils.HTMLNode, box Box, resolver URLResolver, baseUrl string, cascade svgCascade) []Box {
	var doc *svg.DocumentStyle
	if cascade != nil {
		inherited, fontSize := cascade.SVGInherited((*html.Node)(element))
		doc = &svg.DocumentStyle{Declarations: cascade.SVGDeclarations, Inherited: inherited, FontSize: svg.Fl(fontSize)}
	}
	img, err := images.NewSVGImageFromNode((*html.Node)(element), baseUrl, resolver.Fetch, doc)
	if err != nil {
		logger.WarningLogger.Printf("Failed to load inline SVG: %s", err)
		return nil
	}
	return []Box{makeReplacedBox(element, box, img)}
}
