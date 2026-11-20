package svg

import (
	"bytes"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// convert from html nodes to an intermediate svg tree

// svgContext is an intermediated representation of an SVG file,
// where CSS has been applied, and text has been processed
type svgContext struct {
	root *cascadedNode // with tag svg

	baseURL     string
	imageLoader ImageLoader
	urlFetcher  utils.UrlFetcher

	// to handle use tags
	defs map[string]*cascadedNode
	// ID of the current <use> target being resolved,
	// to prevent infinite recursion
	inUseIDs utils.Set

	// externalUses (gopdfgo): adresy (z fragmentem) celów <use> z innych plików, które są właśnie
	// rozwijane; wspólne dla wszystkich dokumentów jednego obrazu, chroni przed cyklem między plikami
	externalUses utils.Set

	// cache
	pathParser pathParser

	// document (gopdfgo) to korzeń całego dokumentu, w którym leży SVG (dla SVG w HTML: dokument
	// HTML); foreignHead to zapamiętane arkusze stylów dla treści foreignObject (zob. documentStyles)
	document    *html.Node
	foreignHead *string
}

// newSVGContext converts from the html representation to an internal,
// simplified form, suitable for post-processing.
//
// The stylesheets are processed and applied, the values
// of the CSS properties begin stored as attributes.
//
// Inheritable attributes are cascaded and 'inherit' special values are resolved.
func newSVGContext(root *html.Node, baseURL string, urlFetcher utils.UrlFetcher) (*svgContext, error) {
	// extract the root svg node, which is not
	// always the first one
	iter := utils.NewHtmlIterator(root, atom.Svg)
	if !iter.HasNext() {
		return nil, errors.New("missing <svg> element")
	}
	return buildSVGContext((*html.Node)(iter.Next()), baseURL, urlFetcher), nil
}

// buildSVGContext (gopdfgo) to dalsza część newSVGContext dla podanego elementu głównego, który nie
// musi być elementem <svg> (dokument wskazany przez <use> z innego pliku, zob. fetchExternalUse).
func buildSVGContext(svgRoot *html.Node, baseURL string, urlFetcher utils.UrlFetcher) *svgContext {
	stylesheets, trefs := fetchStyleAndTextRefs((*utils.HTMLNode)(svgRoot))
	normalMatcher, importantMatcher := parseStylesheets(stylesheets, baseURL)

	// build the SVG tree and apply style attribute
	var out svgContext
	out.baseURL = baseURL
	out.urlFetcher = urlFetcher
	out.defs = make(map[string]*cascadedNode)
	out.inUseIDs = make(utils.Set)
	out.document = svgRoot
	for out.document.Parent != nil {
		out.document = out.document.Parent
	}
	out.externalUses = make(utils.Set)

	// may return nil to discard the node
	var buildTree func(node *html.Node, parentAttrs, parentRaw nodeAttributes) *cascadedNode

	// gopdfgo: parentRaw przechowuje wartości właściwości kolorów rodzica sprzed zamiany currentColor;
	// źródło dziedziczyło kolor już rozwiązany względem rodzica.
	buildTree = func(node *html.Node, parentAttrs, parentRaw nodeAttributes) *cascadedNode {
		// text is handled by the parent
		// style elements are no longer useful
		if node.Type != html.ElementNode || node.DataAtom == atom.Style {
			return nil
		}

		childAttrs := newNodeAttributes(node.Attr)
		// gopdfgo: skrót 'marker' nie jest atrybutem prezentacji (SVG 2 § 13.7.2), działa tylko z CSS
		delete(childAttrs, "marker")
		// Cascade attributes
		for key, value := range parentAttrs {
			if _, isNotInherited := notInheritedAttributes[key]; !isNotInherited {
				if _, isSet := childAttrs[key]; !isSet {
					if raw, ok := parentRaw[key]; ok {
						value = raw
					}
					childAttrs[key] = value
				}
			}
		}

		// Apply style
		childAttrs.applyStyle(baseURL, (*html.Node)(node), normalMatcher, importantMatcher)

		// gopdfgo: wartością obliczoną currentColor jest słowo kluczowe (CSS Color 4 § 6.4), więc
		// dziedziczy się ono jako słowo i rozwiązuje względem 'color' elementu, który go używa
		// (np. treść <pattern> z fill="inherit", zastępczy kolor po url(...)). Źródło zamieniało tylko
		// dokładny zapis "currentColor" i dziedziczyło już rozwiązany kolor rodzica.
		for key, raw := range parentRaw {
			if childAttrs[key] == "inherit" {
				childAttrs[key] = raw
			}
		}
		if c := childAttrs["color"]; c == "inherit" || strings.EqualFold(c, "currentColor") {
			if pc, has := parentAttrs["color"]; has {
				childAttrs["color"] = pc
			} else {
				delete(childAttrs, "color")
			}
		}
		childRaw := nodeAttributes{}
		for key := range colorAttributes {
			v := childAttrs[key]
			if !currentColorRe.MatchString(v) {
				continue
			}
			childRaw[key] = v
			c, has := childAttrs["color"]
			if !has {
				c = "black"
			}
			childAttrs[key] = currentColorRe.ReplaceAllLiteralString(v, c)
		}

		// Handle 'inherit' values
		for key, value := range childAttrs {
			if value == "inherit" {
				childAttrs[key] = parentAttrs[key]
			}
		}

		nodeSVG := &cascadedNode{
			tag:   node.Data,
			text:  (*utils.HTMLNode)(node).GetChildrenText(),
			attrs: childAttrs,
		}

		// gopdfgo: dzieci <foreignObject> to treść HTML rysowana jako osobny dokument (zob. foreign.go),
		// a nie elementy SVG
		if strings.EqualFold(node.Data, "foreignObject") {
			nodeSVG.tag = "foreignObject"
			nodeSVG.foreign = node
		}

		// recurse
		for child := node.FirstChild; child != nil && nodeSVG.foreign == nil; child = child.NextSibling {
			if childSVG := buildTree(child, childAttrs, childRaw); childSVG != nil {
				nodeSVG.children = append(nodeSVG.children, childSVG)
			}
		}

		// Fix text in text tags
		if node.Data == "text" || node.Data == "textPath" || node.Data == "a" {
			handleText(nodeSVG, true, true, trefs)
		}

		if ID := childAttrs["id"]; ID != "" {
			out.defs[ID] = nodeSVG
		}

		return nodeSVG
	}

	out.root = buildTree((*html.Node)(svgRoot), nil, nil)

	out.inheritDefs()

	return &out
}

// Handle inheritance of different defined elements lists.
func (tree *svgContext) inheritDefs() {
	for _, element := range tree.defs {
		if t := element.tag; t == "linearGradient" || t == "radialGradient" || t == "pattern" {
			tree.inheritElement(element)
		}
	}
}

// Recursively handle inheritance of defined element.
func (tree *svgContext) inheritElement(node *cascadedNode) {
	href := node.attrs["href"]
	url, err := utils.SafeUrljoin(tree.baseURL, href, true)
	if err != nil {
		return
	}
	delete(node.attrs, "href")

	parent := tree.defs[parseURLFragment(url)]
	if parent == nil {
		return
	}
	tree.inheritElement(parent)
	for key, value := range parent.attrs {
		if _, in := node.attrs[key]; !in {
			node.attrs[key] = value
		}
	}
}

// cascadedNode is a node in an SVG document.
// we use this intermediate representation to
// ease the cascading of the properties and the text handling
type cascadedNode struct {
	tag      string
	text     []byte
	attrs    nodeAttributes
	children []*cascadedNode

	// foreign (gopdfgo) to element <foreignObject> z drzewa wejściowego; jego dzieci nie są
	// przetwarzane jako SVG
	foreign *html.Node
}

// returns a copy
// attrs is deepcopied, but the children and text are shallow copied
func (c *cascadedNode) copy() cascadedNode {
	out := *c
	out.attrs = make(nodeAttributes, len(c.attrs))
	for k, v := range c.attrs {
		out.attrs[k] = v
	}
	return out
}

// nodeAttributes stores the raw attributes value of a node
//
// attributes will be updated in the post processing
// step due to the cascade
type nodeAttributes map[string]string

func newNodeAttributes(attrs []html.Attribute) nodeAttributes {
	out := make(nodeAttributes, len(attrs))
	for _, attr := range attrs {
		out[attr.Key] = attr.Val
	}
	return out
}

func (na nodeAttributes) viewBox() (*Rectangle, error) {
	if attrValue := na["viewBox"]; attrValue != "" {
		v, err := parseViewbox(attrValue)
		return &v, err
	}
	return nil, nil
}

func (na nodeAttributes) fontSize() (Value, error) {
	attrValue, has := na["font-size"]
	if !has {
		attrValue = "1em"
	}
	return parseValue(attrValue)
}

func (na nodeAttributes) strokeWidth() (Value, error) {
	attrValue, has := na["stroke-width"]
	if !has {
		attrValue = "1px"
	}
	return parseValue(attrValue)
}

func (na nodeAttributes) aspectRatio() preserveAspectRatio {
	preserveAspectRatio := "xMidYMid"
	if s, has := na["preserveAspectRatio"]; has {
		preserveAspectRatio = s
	}
	return parsePreserveAspectRatio(preserveAspectRatio)
}

// default to black
func (na nodeAttributes) fill() (painter, error) {
	attrValue, has := na["fill"]
	if !has {
		attrValue = "black"
	}
	return newPainter(attrValue)
}

func (na nodeAttributes) lineCap() backend.StrokeCapMode {
	switch na["stroke-linecap"] {
	case "round":
		return backend.RoundCap
	case "square":
		return backend.SquareCap
	default:
		return backend.ButtCap
	}
}

func (na nodeAttributes) lineJoin() backend.StrokeJoinMode {
	switch na["stroke-linejoin"] {
	case "round":
		return backend.Round
	case "bevel":
		return backend.Bevel
	default:
		return backend.Miter
	}
}

func (na nodeAttributes) miterLimit() (Fl, error) {
	attrValue, has := na["stroke-miterlimit"]
	if !has {
		attrValue = "4"
	}
	v, err := strconv.ParseFloat(attrValue, 32)
	if v < 0 {
		v = 4
	}
	return Fl(v), err
}

func (na nodeAttributes) display() bool {
	attrValue := na["display"]
	return attrValue != "none"
}

func (na nodeAttributes) visible() bool {
	attrValue := na["visibility"]
	visible := attrValue != "hidden"
	return na.display() && visible
}

func (na nodeAttributes) spacePreserve() bool {
	return na["space"] == "preserve"
}

// walk the tree to extract content needed to build the SVG tree
func fetchStyleAndTextRefs(root *utils.HTMLNode) ([][]byte, map[string][]byte) {
	var (
		stylesheets [][]byte
		trefs       = make(map[string][]byte)
	)
	iter := root.Iter()
	for iter.HasNext() {
		node := iter.Next()
		if css := handleStyleElement(node); len(css) != 0 {
			stylesheets = append(stylesheets, css)
			continue
		}

		// register text refs
		if id := node.Get("id"); id != "" {
			trefs[id] = node.GetChildrenText()
		}
	}
	return stylesheets, trefs
}

var (
	replacerPreserve   = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ")
	replacerNoPreserve = strings.NewReplacer("\n", "", "\r", "", "\t", " ")
)

// replace newlines by spaces, and merge spaces if not preserved.
func processWhitespace(text []byte, preserveSpace bool) []byte {
	if preserveSpace {
		return []byte(replacerPreserve.Replace(string(text)))
	}
	return []byte(replacerNoPreserve.Replace(string(text)))
}

// handle text node by fixing whitespaces and flattening tails,
// updating node 'children' and 'text'
func handleText(node *cascadedNode, trailingSpace, textRoot bool, trefs map[string][]byte) bool {
	preserve := node.attrs.spacePreserve()
	node.text = processWhitespace(node.text, preserve)
	if trailingSpace && !preserve {
		node.text = bytes.TrimLeft(node.text, " ")
	}

	if len(node.text) != 0 {
		trailingSpace = bytes.HasSuffix(node.text, []byte{' '})
	}

	var newChildren []*cascadedNode
	for _, child := range node.children {
		if child.tag == "tref" {
			// Retrieve the referenced node and get its flattened text
			// and remove the node children.
			id := parseURLFragment(child.attrs["href"])
			node.text = append(node.text, trefs[id]...)
			continue
		}

		trailingSpace = handleText(child, trailingSpace, false, trefs)

		newChildren = append(newChildren, child)
	}

	if textRoot && len(newChildren) == 0 && !preserve {
		node.text = bytes.TrimRight(node.text, " ")
	}

	node.children = newChildren

	return trailingSpace
}

// these attributes are not cascaded
var notInheritedAttributes = utils.NewSet(
	"clip",
	"clip-path",
	"filter",
	"height",
	"id",
	"mask",
	"opacity",
	"overflow",
	"rotate",
	"stop-color",
	"stop-opacity",
	"style",
	"transform",
	"transform-origin",
	"translate", // gopdfgo: CSS Transforms 2 § 5.1, niedziedziczone
	"scale",
	"viewBox",
	"width",
	"x",
	"y",
	"dx",
	"dy",
	"href",
	// gopdfgo: atrybuty warunkowe (SVG 2 § 5.8.1) dotyczą tylko swojego elementu
	"requiredExtensions",
	"requiredFeatures",
	"systemLanguage",
	"vector-effect", // gopdfgo: SVG 2 § 13.2, niedziedziczona
	// gopdfgo: display nie jest dziedziczona (CSS Display 3 § 2); poddrzewo z display: none i tak nie
	// jest rysowane (drawNode nie schodzi do dzieci), a źródło dziedzicząc ją ukrywało też treść
	// <marker>, <pattern> i celów <use> leżących w takim poddrzewie (test WPT marker-007)
	"display",
)

// currentColorRe znajduje słowo kluczowe currentColor (bez względu na wielkość liter).
var currentColorRe = regexp.MustCompile(`(?i)\bcurrentcolor\b`)

var colorAttributes = utils.NewSet(
	"fill",
	"flood-color",
	"lighting-color",
	"stop-color",
	"stroke",
)
