package svg

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	"github.com/polestorm/gopdfgo/internal/webrender/matrix"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// gopdfgo: <foreignObject> (SVG 2 § 23.2) i <switch> z atrybutami warunkowymi (SVG 2 § 5.8). Źródło nie
// znało foreignObject: jego dzieci (elementy HTML) trafiały do drzewa SVG jako puste grupy, więc treść
// przepadała; <switch> był rysowany jak <g>, czyli wszystkie dzieci naraz (draw.io: etykieta HTML i jej
// tekstowy zamiennik jedna na drugiej).

// ForeignDocument (gopdfgo) układa dokument HTML source w prostokącie (0, 0, width, height) płótna dst
// i rysuje go tam; clip mówi, czy przyciąć treść do prostokąta. Ustawia go pakiet html/document (svg
// nie może importować pakietów HTML, cykl importów), tak jak boxes.NestedDocument.
var ForeignDocument func(dst backend.Canvas, fonts text.FontConfiguration, source []byte, baseURL string, fetch utils.UrlFetcher, width, height Fl, clip bool)

// foreignObject to treść XHTML elementu <foreignObject>, przygotowana jako osobny dokument HTML.
// Element foreignObject odpowiada w nim elementowi <body>: dostaje styl i klasę elementu, a układ
// treści odbywa się w stronie o rozmiarze (width, height) elementu (SVG 2 § 23.2: foreignObject
// ustanawia nowy blok zawierający i kontekst nakładania).
type foreignObject struct {
	head        string // arkusze stylów dokumentu (bez reguł @page)
	bodyAttrs   string // atrybut class elementu foreignObject
	bodyStyle   string // właściwości dziedziczone z kontekstu SVG i styl własny elementu
	content     string // dzieci foreignObject zserializowane jako HTML
	hasFontSize bool   // font-size ustawione w kontekście SVG; wartość liczona przy rysowaniu
	clip        bool   // overflow inny niż visible
	blendMode   string // mix-blend-mode inne niż normal (Compositing 1 § 3.2)
	baseURL     string
	fetch       utils.UrlFetcher
}

// inheritedForeignProperties to dziedziczone właściwości CSS przekazywane z kontekstu SVG do treści
// foreignObject (CSS Cascade 4 § 3.3: treść foreignObject dziedziczy po nim jak każdy element). Kaskada
// tego pakietu przechowuje wartości jako surowy tekst, więc przenosimy tylko te, które w HTML znaczą to
// samo; font-size liczymy osobno, bo względne wartości (em, %) trzeba odnieść do przodków w SVG.
var inheritedForeignProperties = []string{
	"color", "font-family", "font-style", "font-weight", "font-variant", "font-stretch",
	"line-height", "letter-spacing", "word-spacing", "text-align", "white-space", "visibility",
	"text-transform", "direction",
}

func newForeignObject(node *cascadedNode, tree *svgContext) (drawable, error) {
	out := &foreignObject{baseURL: tree.baseURL, fetch: tree.urlFetcher}
	out.head = tree.documentStyles()
	var style strings.Builder
	// zerujemy reguły dokumentu dla html/body, które nie dotyczą pudełka foreignObject (marginesy,
	// tło); dziedziczone właściwości ciała dokumentu (font, color) zostają, jak w przeglądarce, gdzie
	// treść dziedziczy przez <svg> po <body>
	style.WriteString("margin:0;padding:0;border:0;background:none;width:auto;height:auto;")
	for _, name := range inheritedForeignProperties {
		if v, ok := node.attrs[name]; ok && v != "" && v != "inherit" {
			fmt.Fprintf(&style, "%s:%s;", name, v)
		}
	}
	_, out.hasFontSize = node.attrs["font-size"]
	if node.foreign != nil {
		for _, a := range node.foreign.Attr {
			switch a.Key {
			case "style":
				style.WriteString(a.Val)
			case "class":
				out.bodyAttrs = fmt.Sprintf(` class="%s"`, html.EscapeString(a.Val))
			}
		}
		var content bytes.Buffer
		for c := node.foreign.FirstChild; c != nil; c = c.NextSibling {
			_ = html.Render(&content, c)
		}
		out.content = content.String()
	}
	// efekty elementu SVG (przezroczystość, maska, przycięcie, przekształcenie, mieszanie) nakłada
	// rysowanie SVG; w dokumencie zastępczym nie mogą zadziałać drugi raz na <body>
	style.WriteString(";opacity:1;mask:none;clip-path:none;filter:none;transform:none;mix-blend-mode:normal")
	out.bodyStyle = style.String()
	if mode := strings.TrimSpace(node.attrs["mix-blend-mode"]); blendModes.Has(mode) {
		out.blendMode = mode
	}
	// SVG 2 § 13.7.4 (UA: foreignObject { overflow: hidden }); auto przycina jak w CSS Overflow 3
	// (zob. overflowClips)
	out.clip = overflowClips(node.attrs["overflow"], true)
	return out, nil
}

// document składa dokument HTML; fontSize to obliczony rozmiar czcionki foreignObject w jednostkach
// użytkownika.
func (fo *foreignObject) document(fontSize Fl) []byte {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html style="margin:0;padding:0;border:0;background:none"><head><meta charset="utf-8">`)
	b.WriteString(fo.head)
	b.WriteString(`</head><body`)
	b.WriteString(fo.bodyAttrs)
	b.WriteString(` style="`)
	if fo.hasFontSize {
		fmt.Fprintf(&b, "font-size:%.4fpx;", fontSize)
	}
	b.WriteString(html.EscapeString(fo.bodyStyle))
	b.WriteString(`">`)
	b.WriteString(fo.content)
	b.WriteString(`</body></html>`)
	return []byte(b.String())
}

func (fo *foreignObject) draw(dst backend.Canvas, attrs *attributes, svg *SVGImage, dims drawingDims) []vertex {
	x, y := dims.point(attrs.x, attrs.y)
	width, height := dims.point(attrs.width, attrs.height)
	// SVG 2 § 23.2: width i height auto (brak atrybutów) liczą się jako 0, a element bez rozmiaru
	// nie jest rysowany (test WPT foreign-object-size)
	if width <= 0 || height <= 0 || ForeignDocument == nil || svg.textContext == nil {
		return nil
	}
	source := fo.document(dims.fontSize)
	paint := func(dst backend.Canvas) {
		dst.OnNewStack(func() {
			dst.State().Transform(matrix.Translation(x, y))
			ForeignDocument(dst, svg.textContext.Fonts(), source, fo.baseURL, fo.fetch, width, height, fo.clip)
		})
	}
	if fo.blendMode == "" {
		paint(dst)
		return nil
	}
	// mix-blend-mode: treść rysowana w grupie, a grupa mieszana z tłem (Compositing 1 § 3.2)
	group := dst.NewGroup(x, y, width, height)
	paint(group)
	dst.OnNewStack(func() {
		dst.State().SetBlendingMode(fo.blendMode)
		dst.DrawWithOpacity(1, group)
	})
	return nil
}

var blendModes = utils.NewSet("multiply", "screen", "overlay", "darken", "lighten", "color-dodge", "color-burn",
	"hard-light", "soft-light", "difference", "exclusion", "hue", "saturation", "color", "luminosity")

func (fo *foreignObject) boundingBox(attrs *attributes, dims drawingDims) (Rectangle, bool) {
	x, y := dims.point(attrs.x, attrs.y)
	width, height := dims.point(attrs.width, attrs.height)
	return Rectangle{x, y, width, height}, true
}

// documentStyles zwraca arkusze stylów całego dokumentu, w którym leży SVG (elementy <style> i <link
// rel=stylesheet>, także spoza <svg>, gdy SVG jest osadzone w HTML), do wstawienia w <head> dokumentu
// foreignObject: reguły dokumentu dotyczą też treści foreignObject. Reguły @page są usuwane, bo
// rozmiar strony dokumentu zastępczego to rozmiar foreignObject.
func (tree *svgContext) documentStyles() string {
	if tree.foreignHead != nil {
		return *tree.foreignHead
	}
	var b strings.Builder
	if tree.document != nil {
		var walk func(n *html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode {
				switch {
				case n.DataAtom == atom.Style || n.Data == "style":
					css := (*utils.HTMLNode)(n).GetChildrenText()
					b.WriteString("<style>")
					b.WriteString(strings.ReplaceAll(stripPageRules(string(css)), "</", `<\/`))
					b.WriteString("</style>")
					return
				case n.Data == "link":
					rel, href := "", ""
					for _, a := range n.Attr {
						switch a.Key {
						case "rel":
							rel = a.Val
						case "href":
							href = a.Val
						}
					}
					if href != "" && strings.Contains(" "+strings.ToLower(rel)+" ", " stylesheet ") {
						fmt.Fprintf(&b, `<link rel="stylesheet" href="%s">`, html.EscapeString(href))
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(tree.document)
	}
	s := b.String()
	tree.foreignHead = &s
	return s
}

// stripPageRules usuwa z arkusza reguły @page (z ich blokami, także zagnieżdżonymi regułami pól
// marginesu), pomijając komentarze i napisy.
func stripPageRules(css string) string {
	if !strings.Contains(strings.ToLower(css), "@page") {
		return css
	}
	var out strings.Builder
	for i := 0; i < len(css); {
		switch c := css[i]; {
		case c == '/' && i+1 < len(css) && css[i+1] == '*':
			end := strings.Index(css[i+2:], "*/")
			if end < 0 {
				end = len(css) - i - 4
			}
			out.WriteString(css[i : i+end+4])
			i += end + 4
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(css) && css[j] != c {
				if css[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(css))
			out.WriteString(css[i:j])
			i = j
		case c == '@' && len(css)-i >= 5 && strings.EqualFold(css[i:i+5], "@page"):
			j := strings.IndexByte(css[i:], '{')
			if j < 0 {
				return out.String()
			}
			depth := 0
			for j += i; j < len(css); j++ {
				if css[j] == '{' {
					depth++
				} else if css[j] == '}' {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			i = j + 1
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

// Atrybuty warunkowe (SVG 2 § 5.8.1). requiredFeatures usunięto w SVG 2, więc nie wpływa na wynik
// (jak w Chromium; draw.io oznacza nim foreignObject i oczekuje, że będzie rysowany).

// userLanguage to język użytkownika dla systemLanguage: silnik nie ma preferencji użytkownika,
// przyjmujemy en-US, jak domyślny Chromium.
const userLanguage = "en-us"

// supportedExtensions to rozszerzenia znane requiredExtensions: przestrzeń nazw XHTML, bo silnik
// rysuje treść foreignObject (Chromium uznaje też MathML).
var supportedExtensions = utils.NewSet("http://www.w3.org/1999/xhtml")

// conditionsPass mówi, czy atrybuty warunkowe elementu są spełnione.
func conditionsPass(node *cascadedNode) bool {
	if v, ok := node.attrs["requiredExtensions"]; ok {
		list := strings.Fields(v)
		if len(list) == 0 {
			return false
		}
		for _, ext := range list {
			if !supportedExtensions.Has(ext) {
				return false
			}
		}
	}
	if v, ok := node.attrs["systemLanguage"]; ok {
		match := false
		for _, lang := range strings.Split(v, ",") {
			lang = strings.ToLower(strings.TrimSpace(lang))
			if lang != "" && (langPrefix(lang, userLanguage) || langPrefix(userLanguage, lang)) {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	return true
}

// langPrefix mówi, czy prefix jest równy tag albo jest jego przedrostkiem zakończonym przed '-'
// (SVG 2 § 5.8.5).
func langPrefix(prefix, tag string) bool {
	return tag == prefix || (strings.HasPrefix(tag, prefix) && tag[len(prefix)] == '-')
}

// isDescriptive mówi, czy element jest opisowy (SVG 2 § 5.9) i nie kandyduje w <switch>.
func isDescriptive(tag string) bool {
	switch tag {
	case "desc", "title", "metadata":
		return true
	}
	return false
}
