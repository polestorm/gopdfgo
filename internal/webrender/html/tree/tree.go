package tree

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"

	"github.com/polestorm/gopdfgo/internal/webrender/css/counters"
	"github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	"github.com/polestorm/gopdfgo/internal/webrender/css/selector"
	"github.com/polestorm/gopdfgo/internal/webrender/css/validation"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/text"

	"github.com/polestorm/gopdfgo/internal/webrender/utils"
	"golang.org/x/net/html"
)

// Represents an HTML document parsed by net/html.
type HTML struct {
	Root       *utils.HTMLNode
	mediaType  string
	UrlFetcher utils.UrlFetcher
	BaseUrl    string

	UAStyleSheet   CSS
	FormStyleSheet CSS
	PHStyleSheet   CSS

	// Behavior (gopdfgo): przełączniki zachowań układu ustawiane przez wywołującego.
	Behavior Behavior
}

// Behavior zbiera przełączniki zachowań, w których norma i przeglądarki się różnią.
// Wartość zerowa to zachowanie domyślne silnika.
type Behavior struct {
	// MarginsEscapeHeightLimits: dolny margines ostatniego dziecka zwija się z marginesem rodzica także
	// wtedy, gdy min-height albo max-height zmieniło wysokość rodzica (dosłownie CSS 2.2 § 10.7).
	// Domyślnie, jak w Chromium, taki margines zostaje w rodzicu.
	MarginsEscapeHeightLimits bool
	// MonospaceFontSize: rozmiar w pikselach, jaki ma font-size: medium, gdy font-family to samo
	// monospace. Zero oznacza 13 px, jak w przeglądarkach; 16 wyłącza tę regułę (ADR 0008).
	MonospaceFontSize float64
	// ExactLengths: długości zostają dokładne. Domyślnie szerokości obramowań, konturów i linii między
	// kolumnami oraz border-spacing są zaokrąglane do całych pikseli CSS jak w Chromium (ADR 0009, 0013).
	ExactLengths bool
	// MaxShrink: największe pomniejszenie treści szerszej niż obszar strony (ADR 0014). Wartości do 1
	// (także zero) wyłączają pomniejszanie.
	MaxShrink float64
}

// `baseUrl` is the base used to resolve relative URLs
// (e.g. in “<img src="../foo.png">“). If not provided, is is infered from
// the input filename or the URL
//
// `urlFetcher` is a function called to fetch external resources such as stylesheets and images.
// and defaults to utils.DefaultUrlFetcher
//
// `mediaType` is the media type to use for “@media“, and defaults to "print".
func NewHTML(htmlContent utils.ContentInput, baseUrl string, urlFetcher utils.UrlFetcher, mediaType string) (*HTML, error) {
	logger.ProgressLogger.Println("Step 1 - Fetching and parsing HTML")
	if urlFetcher == nil {
		urlFetcher = utils.DefaultUrlFetcher
	}
	if mediaType == "" {
		mediaType = "print"
	}
	result, err := utils.FetchSource(htmlContent, baseUrl, urlFetcher, false)
	if err != nil {
		return nil, fmt.Errorf("can't fetch html input : %s", err)
	}

	root, err := html.ParseWithOptions(bytes.NewReader(result.Content), html.ParseOptionEnableScripting(false))
	if err != nil || root.FirstChild == nil {
		return nil, fmt.Errorf("invalid html input : %s", err)
	}

	var out HTML
	// html.Parse wraps the <html> tag
	// gopdfgo: korzeniem jest pierwszy element dokumentu. W źródle pomijano tylko DOCTYPE, więc
	// komentarz przed <html> stawał się korzeniem i budowa drzewa pudełek kończyła się paniką.
	rootElement := root.FirstChild
	for rootElement != nil && rootElement.Type != html.ElementNode {
		rootElement = rootElement.NextSibling
	}
	if rootElement == nil {
		return nil, fmt.Errorf("invalid html input : no root element")
	}
	out.Root = (*utils.HTMLNode)(rootElement)
	out.Root.Parent = nil
	out.BaseUrl = utils.FindBaseUrl(root, result.BaseUrl)
	out.UrlFetcher = urlFetcher
	out.mediaType = mediaType
	out.UAStyleSheet = Html5UAStylesheet
	out.PHStyleSheet = Html5PHStylesheet
	return &out, nil
}

func newHtml(htmlContent utils.ContentInput) (*HTML, error) {
	return NewHTML(htmlContent, "", nil, "")
}

func (h HTML) GetMetadata() utils.DocumentMetadata {
	return utils.GetHtmlMetadata(h.Root, h.BaseUrl)
}

var (
	// Html5UAStylesheet is the user agent style sheet
	Html5UAStylesheet CSS

	// Html5UAFormsStylesheet is the user agent style sheet used when forms are enabled.
	Html5UAFormsStylesheet CSS

	// Html5PHStylesheet is the presentational hints style sheet
	Html5PHStylesheet CSS

	// The counters defined in the user agent style sheet
	UACounterStyle = make(counters.CounterStyle)

	//go:embed html5_ua.css
	html5UACSS string

	//go:embed html5_ua_forms.css
	html5UAFormsCSS string

	//go:embed html5_ph.css
	html5PHCSS string
)

func init() {
	logger.ProgressLogger.SetOutput(io.Discard)
	logger.WarningLogger.SetOutput(io.Discard)
	defer func() {
		logger.ProgressLogger.SetOutput(os.Stdout)
		logger.WarningLogger.SetOutput(os.Stdout)
	}()

	var err error
	Html5UAStylesheet, err = newCSS(utils.InputString(html5UACSS), "", nil, false, "", nil, nil, nil, UACounterStyle)
	if err != nil {
		panic(fmt.Sprintf("invalid embedded stylesheet: %s", err))
	}
	Html5UAFormsStylesheet, err = newCSS(utils.InputString(html5UAFormsCSS), "", nil, false, "", nil, nil, nil, UACounterStyle)
	if err != nil {
		panic(fmt.Sprintf("invalid embedded stylesheet: %s", err))
	}
	Html5PHStylesheet, err = NewCSSDefault(utils.InputString(html5PHCSS))
	if err != nil {
		panic(fmt.Sprintf("invalid embedded stylesheet: %s", err))
	}
}

// CSS represents a parsed CSS stylesheet.
type CSS struct {
	matcher   matcher
	pageRules []PageRule
	baseUrl   string
}

// newCSS creates an instance, in the same way as [HTML], except that
// the “tree“ argument is not available. All other arguments are the same.
// An additional argument called [fontConfig] must be provided to handle
// “@font-config“ rules. The same “fonts.FontConfiguration“ object must be
// used for different “CSS“ objects applied to the same document.
//
// [checkMimeType] should default to false
func newCSS(input utils.ContentInput, baseUrl string,
	urlFetcher utils.UrlFetcher, checkMimeType bool,
	mediaType string, fontConfig text.FontConfiguration, matcher *matcher,
	pageRules *[]PageRule, counterStyle counters.CounterStyle,
) (CSS, error) {
	logger.ProgressLogger.Printf("Step 2 - Fetching and parsing CSS - %s", input)

	if urlFetcher == nil {
		urlFetcher = utils.DefaultUrlFetcher
	}
	if mediaType == "" {
		mediaType = "print"
	}

	ressource, err := utils.FetchStylesheet(input, baseUrl, urlFetcher, checkMimeType)
	if err != nil {
		return CSS{}, fmt.Errorf("error fetching css input : %s", err)
	}

	stylesheet := parser.ParseStylesheetBytes(ressource.Content, false, false)

	if matcher == nil {
		matcher = newMatcher()
	}
	if pageRules == nil {
		pageRules = &[]PageRule{}
	}
	if counterStyle == nil {
		counterStyle = make(counters.CounterStyle)
	}

	out := CSS{baseUrl: ressource.BaseUrl}
	preprocessStylesheet(mediaType, ressource.BaseUrl, stylesheet, urlFetcher, matcher,
		pageRules, fontConfig, counterStyle, false)
	out.matcher = *matcher
	out.pageRules = *pageRules
	return out, nil
}

// NewCSSDefault processes a CSS input.
func NewCSSDefault(input utils.ContentInput) (CSS, error) {
	return NewCSSExt(input, "", nil)
}

// NewCSSDefault processes a CSS input.
func NewCSSExt(input utils.ContentInput, baseURL string, fonts text.FontConfiguration) (CSS, error) {
	return newCSS(input, baseURL, nil, false, "", fonts, nil, nil, nil)
}

func (c CSS) IsNone() bool {
	return c.baseUrl == "" && c.matcher == nil && c.pageRules == nil
}

type match struct {
	selector     selector.SelectorGroup
	declarations []validation.Declaration
	// gopdfgo: pełna nazwa warstwy kaskady (@layer), w której stoi reguła;
	// pusta poza warstwami. Wpis bez selektora tylko ogłasza warstwę.
	layer string
}

type matcher []match

func newMatcher() *matcher {
	return &matcher{}
}

type matchResult struct {
	pseudoType  string
	payload     []validation.Declaration
	specificity selector.Specificity
	layer       string
}

func (m matcher) match(element *html.Node) (out []matchResult) {
	for _, mat := range m {
		for _, sel := range mat.selector {
			if sel.Match(element) {
				out = append(out, matchResult{specificity: sel.Specificity(), pseudoType: sel.PseudoElement(), payload: mat.declarations, layer: mat.layer})
			}
		}
	}
	return
}

type pageIndex struct {
	Group string
	A, B  int
}

func (p pageIndex) IsNone() bool {
	return p.A == 0 && p.B == 0 && p.Group == ""
}

type pageSelector struct {
	Side        string // "left", "right" or ""
	Name        string // page name string or ""
	Index       pageIndex
	Specificity selector.Specificity
	Blank       bool
	First       bool
}
