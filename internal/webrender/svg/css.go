package svg

import (
	"strings"

	pa "github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	"github.com/polestorm/gopdfgo/internal/webrender/css/selector"
	"github.com/polestorm/gopdfgo/internal/webrender/css/validation"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Apply CSS to SVG documents.

// http://www.w3.org/TR/SVG/styling.html#StyleElement
// n has tag style
func handleStyleElement(n *utils.HTMLNode) []byte {
	if n.DataAtom != atom.Style {
		return nil
	}
	for _, v := range n.Attr {
		if v.Key == "type" && v.Val != "text/css" {
			return nil
		}
	}

	// extract the css
	return n.GetChildrenText()
}

// gopdfgo: arkusz z @import pobiera ten sam fetcher co reszta zasobów obrazu SVG (w gopdfgo: Options.Resolver);
// źródło wołało tu zawsze DefaultUrlFetcher, czyli plik z dysku albo sieć z pominięciem fetchera dokumentu.
func fetchURL(url, baseURL string, fetcher utils.UrlFetcher) ([]byte, string, error) {
	if fetcher == nil {
		fetcher = utils.DefaultUrlFetcher
	}
	joinedUrl, err := utils.SafeUrljoin(baseURL, url, true)
	if err != nil {
		return nil, "", err
	}
	cssUrl, err := parseURL(joinedUrl)
	if err != nil {
		return nil, "", err
	}
	resolvedURL := cssUrl.String()
	content, err := utils.FetchSource(utils.InputUrl(resolvedURL), baseURL, fetcher, false)
	if err != nil {
		return nil, "", err
	}
	return content.Content, resolvedURL, nil
}

// Find rules among stylesheet rules and imports.
func findStylesheetsRules(rules []pa.Compound, baseUrl string, fetcher utils.UrlFetcher) (out []pa.QualifiedRule) {
	for _, rule := range rules {
		switch rule := rule.(type) {
		case pa.AtRule:
			if utils.AsciiLower(rule.AtKeyword) == "import" && rule.Content == nil {
				urlToken := pa.ParseOneComponentValue(rule.Prelude)
				var url string
				switch urlToken := urlToken.(type) {
				case pa.String:
					url = urlToken.Value
				case pa.URL:
					url = urlToken.Value
				default:
					continue
				}
				cssContent, resolvedURL, err := fetchURL(url, baseUrl, fetcher)
				if err != nil {
					logger.WarningLogger.Printf("failed to load stylesheet: %s", err)
					continue
				}

				stylesheet := pa.ParseStylesheetBytes(cssContent, true, true)
				out = append(out, findStylesheetsRules(stylesheet, resolvedURL, fetcher)...)
			}
			// if rule.AtKeyword.Lower() == "media":
		case pa.QualifiedRule:
			out = append(out, rule)
			// elif rule.type == "error":
		}
	}
	return out
}

type declaration struct {
	property string
	value    string
}

// Parse declarations in a given rule content.
func parseDeclarations(input []pa.Token) (normalDeclarations, importantDeclarations []declaration) {
	for _, decl := range pa.ParseBlocksContents(input, false) {
		if decl, ok := decl.(pa.Declaration); ok {
			// gopdfgo: właściwości niestandardowe („--x”) zostają, zachowując wielkość liter, dla var()
			// (zob. substituteAttributeVars); pomijane są tylko rozszerzenia producentów („-webkit-”)
			name := utils.AsciiLower(decl.Name)
			if strings.HasPrefix(decl.Name, "--") {
				name = decl.Name
			} else if strings.HasPrefix(decl.Name, "-") {
				continue
			}
			if decl.Important {
				importantDeclarations = append(importantDeclarations, declaration{name, pa.Serialize(decl.Value)})
			} else {
				normalDeclarations = append(normalDeclarations, declaration{name, pa.Serialize(decl.Value)})
			}
		}
	}
	return normalDeclarations, importantDeclarations
}

type match struct {
	selector     selector.SelectorGroup
	declarations []declaration
}

type matcher []match

// Find stylesheets and return rule matchers.
func parseStylesheets(stylesheets [][]byte, url string, fetcher utils.UrlFetcher) (matcher, matcher) {
	var normalMatcher, importantMatcher matcher
	// Parse rules and fill matchers
	for _, css := range stylesheets {
		stylesheet := pa.ParseStylesheetBytes(css, true, true)
		for _, rule := range findStylesheetsRules(stylesheet, url, fetcher) {
			normalDeclarations, importantDeclarations := parseDeclarations(rule.Content)
			prelude := pa.Serialize(rule.Prelude)
			selector, err := selector.ParseGroup(prelude)
			if err != nil {
				logger.WarningLogger.Printf("Invalid or unsupported selector '%s', %s \n", prelude, err)
				continue
			}
			if len(normalDeclarations) != 0 {
				normalMatcher = append(normalMatcher, match{selector: selector, declarations: normalDeclarations})
			}
			if len(importantDeclarations) != 0 {
				importantMatcher = append(importantMatcher, match{selector: selector, declarations: importantDeclarations})
			}
		}
	}
	return normalMatcher, importantMatcher
}

// returns (property, value) pairs
func (m matcher) match(element *html.Node) (out []declaration) {
	for _, mat := range m {
		for _, sel := range mat.selector {
			if sel.Match(element) {
				out = append(out, mat.declarations...)
			}
		}
	}
	return
}

// replace `d` with the (potential) expanded properties
func expandProperty(d declaration) []declaration {
	if d.property != "font" {
		return []declaration{d}
	}

	tokens := pa.RemoveWhitespace(pa.Tokenize([]byte(d.value), true))
	expanded, err := validation.ExpandFont(tokens)
	if err != nil {
		logger.WarningLogger.Printf("ignoring %s property: %s", d.property, err)
		return nil
	}

	out := make([]declaration, len(expanded))
	for i, p := range expanded {
		out[i] = declaration{p[0], p[1]}
	}
	return out
}

func (attrs nodeAttributes) applyStyle(_ string, node *html.Node, normal, important matcher) {
	var normalAttr, importantAttr []declaration
	if styleAttr := attrs["style"]; styleAttr != "" {
		normalAttr, importantAttr = parseDeclarations(pa.Tokenize([]byte(styleAttr), false))
	}
	delete(attrs, "style") // not useful anymore

	var allProps []declaration
	allProps = append(allProps, normal.match(node)...)
	allProps = append(allProps, normalAttr...)
	allProps = append(allProps, important.match(node)...)
	allProps = append(allProps, importantAttr...)
	for _, d := range allProps {
		expanded := expandProperty(d)
		for _, exp := range expanded {
			attrs[exp.property] = strings.TrimSpace(exp.value)
		}
	}
}

// rootBoxProperties (gopdfgo): zob. applyDocumentStyle
var rootBoxProperties = utils.NewSet("width", "height", "x", "y", "transform", "translate", "rotate", "scale",
	"transform-origin", "transform-box", "opacity", "filter", "clip-path", "mix-blend-mode")

// rootBoxEffects (gopdfgo): właściwości efektów spośród rootBoxProperties, które pudełko CSS stosuje
// także z atrybutu style (zob. applyDocumentStyle)
var rootBoxEffects = utils.NewSet("opacity", "filter", "clip-path", "mix-blend-mode")

// appliedByRootBox (gopdfgo) mówi, czy deklarację na elemencie głównym stosuje pudełko CSS w układzie
// HTML. clip-path z odwołaniem url() do <clipPath> i filter złożony z samego url() stosuje rysowanie SVG,
// bo układ HTML takich odwołań nie zna (tak samo mask, której układ HTML nie obsługuje).
func appliedByRootBox(names utils.Set, name, value string) bool {
	if !names.Has(name) {
		return false
	}
	v := strings.ToLower(strings.TrimSpace(value))
	if name == "clip-path" && strings.Contains(v, "url(") {
		return false
	}
	// sam url(): lista filtrów z funkcjami CSS stosuje pudełko (bez url(), które pomija)
	if name == "filter" && strings.HasPrefix(v, "url(") && strings.Count(v, "(") == 1 {
		return false
	}
	return true
}

// applyDocumentStyle (gopdfgo) stosuje kaskadę dokumentu HTML do elementu SVG osadzonego w HTML
// (zob. DocumentStyle). decls są w rosnącej kolejności kaskady; presentation to nazwy atrybutów
// elementu. Atrybuty prezentacyjne liczą się jak reguły autora o specyficzności 0 stojące przed
// wszystkimi arkuszami autora (SVG 2 § 6.2), więc wygrywają z regułami zwykłymi użytkownika i przegrywają
// z każdą regułą autora; atrybut style stoi nad zwykłymi regułami autora, a jego !important nad
// !important autora i pod !important użytkownika (CSS Cascade 4 § 6.1).
//
// Na elemencie głównym pomijamy deklaracje arkuszy dla właściwości, które stosuje już pudełko CSS
// elementu <svg> w układzie HTML (rozmiar i położenie, przekształcenia, opacity, filter, clip-path,
// mix-blend-mode): np. „figure svg { width: 100%; height: auto }” ustala rozmiar pudełka, a nie
// rozmiar wewnętrzny obrazu, a opacity zastosowana drugi raz wewnątrz obrazu by się podwoiła. Z atrybutu
// style pomijamy właściwości efektów (rootBoxEffects; wcześniej opacity z atrybutu style działała
// podwójnie). mask zostaje dla rysowania SVG, bo układ HTML jej nie obsługuje (test WPT outer-svg-mask).
func (attrs nodeAttributes) applyDocumentStyle(decls []validation.RawDeclaration, presentation nodeAttributes, root bool) {
	if root {
		kept := decls[:0:0]
		for _, d := range decls {
			if !appliedByRootBox(rootBoxProperties, d.Name, d.Value) {
				kept = append(kept, d)
			}
		}
		decls = kept
	}
	var normalAttr, importantAttr []declaration
	if styleAttr := attrs["style"]; styleAttr != "" {
		normalAttr, importantAttr = parseDeclarations(pa.Tokenize([]byte(styleAttr), false))
	}
	delete(attrs, "style")
	if root {
		dropEffects := func(ds []declaration) []declaration {
			kept := ds[:0:0]
			for _, d := range ds {
				if !appliedByRootBox(rootBoxEffects, d.property, d.value) {
					kept = append(kept, d)
				}
			}
			return kept
		}
		normalAttr, importantAttr = dropEffects(normalAttr), dropEffects(importantAttr)
	}

	set := func(d declaration) {
		for _, exp := range expandProperty(d) {
			attrs[exp.property] = strings.TrimSpace(exp.value)
		}
	}
	i := 0
	for ; i < len(decls) && !decls[i].Important && decls[i].User; i++ {
		d := declaration{decls[i].Name, decls[i].Value}
		for _, exp := range expandProperty(d) {
			if _, isAttr := presentation[exp.property]; !isAttr {
				attrs[exp.property] = strings.TrimSpace(exp.value)
			}
		}
	}
	for ; i < len(decls) && !decls[i].Important; i++ {
		set(declaration{decls[i].Name, decls[i].Value})
	}
	for _, d := range normalAttr {
		set(d)
	}
	for ; i < len(decls) && !decls[i].User; i++ {
		set(declaration{decls[i].Name, decls[i].Value})
	}
	for _, d := range importantAttr {
		set(d)
	}
	for ; i < len(decls); i++ {
		set(declaration{decls[i].Name, decls[i].Value})
	}
}
