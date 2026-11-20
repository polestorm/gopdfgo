package boxes

import (
	"fmt"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"math"
	"regexp"
	"strings"
	"unicode"

	"github.com/polestorm/gopdfgo/internal/webrender/images"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/text"

	"github.com/polestorm/gopdfgo/internal/webrender/css/parser"

	"github.com/polestorm/gopdfgo/internal/webrender/css/counters"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Turn an element tree with associated CSS style (computed values)
// into a "before layout" formatting structure / box tree.
// This includes creating anonymous boxes and processing whitespace
// as necessary.

var (
	TableFirstLetter = []*unicode.RangeTable{unicode.Ps, unicode.Pe, unicode.Pi, unicode.Pf, unicode.Po}

	styleScores = map[pr.String]float64{}
	styleMap    = map[pr.String]pr.String{
		"inset":  "ridge",
		"outset": "groove",
	}

	transparent = pr.Color(parser.ParseColorString("transparent"))
)

func init() {
	styles := []pr.String{
		"hidden", "double", "solid", "dashed", "dotted", "ridge",
		"outset", "groove", "inset", "none",
	}
	N := len(styles) - 1
	for i, v := range styles {
		styleScores[v] = float64(N - i)
	}
}

type Context interface {
	// RunningElements() map[string]map[int]Box
	CurrentPage() int
	GetStringSetFor(page Box, name, keyword string) string
	GetRunningElementFor(page Box, name, keyword string) Box
}

type URLResolver struct {
	Fetch      utils.UrlFetcher
	FetchImage ImageFetcher
}

type ImageFetcher = func(url, forcedMimeType string, orientation pr.SBoolFloat) images.Image

type styleForI interface {
	Get(element tree.Element, pseudoType string) pr.ElementStyle
}

type rootStyleFor struct {
	elementTree *utils.HTMLNode
	tree.StyleFor
}

func (r rootStyleFor) Get(element tree.Element, pseudoType string) pr.ElementStyle {
	style := r.StyleFor.Get(element, pseudoType)
	if style != nil {
		if element == r.elementTree {
			style.SetDisplay(pr.Display{Outside: pr.Block, Inside: pr.Flow})
			// gopdfgo: element główny z display: none nie tworzy pudełka, więc jego tło nie przechodzi
			// na płótno (CSS Backgrounds 3 § 2.11.1, test WPT background-color-root-propagation-001);
			// zastępcze pudełko bloku nie ma tła.
			style.SetBackgroundColor(pr.InitialValues.GetBackgroundColor())
			style.SetBackgroundImage(pr.InitialValues.GetBackgroundImage())
		} else {
			style.SetDisplay(pr.Display{Outside: pr.None})
		}
	}
	return style
}

var (
	tabRe   = regexp.MustCompile(`[\t ]*\n[\t ]*`)
	spaceRe = regexp.MustCompile(`[\t ]+`)
)

// CreateAnonymousBox create anonymous boxes in box descendants according to layout rules.
func CreateAnonymousBox(box Box) Box {
	box = AnonymousTableBoxes(box)
	box = FlexBoxes(box)
	box = GridBoxes(box)
	box = MulticolWrappers(box) // gopdfgo: komórki i pudełka liniowo-blokowe z kolumnami
	box = InlineInBlock(box)
	box = BlockInInline(box)
	return box
}

// Build a formatting structure (box tree) from an element tree.
func BuildFormattingStructure(elementTree *utils.HTMLNode, styleFor *tree.StyleFor, resolver URLResolver,
	baseUrl string, targetCollector *tree.TargetCollector, cs counters.CounterStyle, footnotes *[]Box,
) BlockLevelBoxITF {
	boxList := elementToBox(elementTree, styleFor, resolver, baseUrl, targetCollector, cs, nil, footnotes)

	var box Box
	if len(boxList) > 0 {
		box = boxList[0]
	} else { //  No root element
		rsf := rootStyleFor{elementTree: elementTree, StyleFor: *styleFor}
		box = elementToBox(elementTree, rsf, resolver, baseUrl, targetCollector, cs, nil, footnotes)[0]
		box.Box().RootDisplayNone = true
	}

	targetCollector.CheckPendingTargets()

	box.Box().IsForRootElement = true
	// If this is changed, maybe update layout.pages.makeMarginBoxes()
	box = CreateAnonymousBox(box)
	box = setViewportOverflow(box)
	return box.(BlockLevelBoxITF)
}

// Maps values of the “display“ CSS property to box types.
func makeBox(style pr.ElementStyle, content []Box, element *utils.HTMLNode, pseudoType string) (b Box, _ error) {
	tmp := style.GetDisplay()
	display := [2]pr.Keyword{tmp.Outside, tmp.Inside}
	switch display {
	case [2]pr.Keyword{pr.Block, pr.Flow}:
		b = NewBlockBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.Inline, pr.Flow}:
		b = NewInlineBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.Block, pr.FlowRoot}:
		b = NewBlockBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.Inline, pr.FlowRoot}:
		b = NewInlineBlockBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.Block, pr.Table}:
		b = NewTableBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.Inline, pr.Table}:
		b = NewInlineTableBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.Block, pr.Flex}:
		b = NewFlexBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.Inline, pr.Flex}:
		b = NewInlineFlexBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.Block, pr.Grid}:
		b = NewGridBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.Inline, pr.Grid}:
		b = NewInlineGridBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.TableRow}:
		b = NewTableRowBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.TableRowGroup}:
		b = NewTableRowGroupBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.TableHeaderGroup}:
		b = NewTableRowGroupBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.TableFooterGroup}:
		b = NewTableRowGroupBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.TableColumn}:
		b = NewTableColumnBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.TableColumnGroup}:
		b = NewTableColumnGroupBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.TableCell}:
		b = NewTableCellBox(style, (*html.Node)(element), pseudoType, content)
	case [2]pr.Keyword{pr.TableCaption}:
		b = NewTableCaptionBox(style, (*html.Node)(element), pseudoType, content)
	default:
		return nil, fmt.Errorf("ignored box %s: display property %v not supported", element.Data, tmp)
	}
	return b, nil
}

// Convert an element and its children into a box with children.
//
// Return a list of boxes. Most of the time the list will have one item but
// may have zero or more than one.
//
//	Eg.::
//
//	    <p>Some <em>emphasised</em> text.</p>
//
//	gives (not actual syntax)::
//
//	    BlockBox[
//	        TextBox["Some "],
//	        InlineBox[
//	            TextBox["emphasised"],
//	        ],
//	        TextBox[" text."],
//	    ]
//
//	``TextBox``es are anonymous inline boxes:
//	See https://www.w3.org/TR/CSS21/visuren.html#anonymous
func elementToBox(element *utils.HTMLNode, styleFor styleForI,
	resolver URLResolver, baseUrl string, targetCollector *tree.TargetCollector,
	cs counters.CounterStyle, state *tree.PageState, footnotes *[]Box,
) []Box {
	if element.Type != html.TextNode && element.Type != html.ElementNode && element.Type != html.DocumentNode {
		// Here we ignore comments and XML processing instructions.
		return nil
	}

	style := styleFor.Get(element, "")

	display := style.GetDisplay()
	if display == (pr.Display{Outside: pr.None}) {
		return nil
	}

	if style.GetFloat() == "footnote" {
		if style.GetFootnoteDisplay() == "block" {
			style.SetDisplay(pr.Display{Outside: pr.Block, Inside: pr.Flow})
		} else {
			style.SetDisplay(pr.Display{Outside: pr.Inline, Inside: pr.Flow})
		}
	}

	box, err := makeBox(style, nil, element, "")
	if err != nil {
		logger.WarningLogger.Println(err)
		return nil
	}

	if state == nil {
		// use a list to have a shared mutable object
		state = &tree.PageState{
			// Shared mutable objects:
			QuoteDepth: []int{0}, // single integer
			CounterValues: tree.CounterValues{
				"footnote": []int{0},
			}, // name -> stacked/scoped values
			CounterScopes: []utils.Set{ //  element tree depths -> counter names
				utils.NewSet("footnote"),
			},
		}
	}

	counterValues := state.CounterValues

	// gopdfgo: element bez pudełka (tu display: contents) nie zakłada, nie ustawia i nie zwiększa
	// liczników (CSS Lists 3 § 4.5); źródło stosowało do niego counter-*.
	var nestedCounters []string
	if !generatesNoBox(element, display) {
		nestedCounters = updateCounters(state, style, func(name string, withSiblings bool) int {
			return reversedCounterStart(element, styleFor, name, withSiblings)
		}, false)
	}
	if !display.Contents && element.Type == html.ElementNode {
		if image := replacementImage(style, resolver); image != nil {
			PopNestedCounters(state, nestedCounters)
			return []Box{makeReplacedBox(element, box, image)}
		}
	}

	// If this element’s direct children create new scopes, the counter
	// names will be in this new list
	state.CounterScopes = append(state.CounterScopes, utils.Set{})

	box.Box().FirstLetterStyle = styleFor.Get(element, "first-letter")
	box.Box().FirstLineStyle = tree.FirstLineStyle(styleFor.Get(element, "first-line"))

	var children, markerBoxes []Box
	if display.Has(pr.ListItem) {
		mb := markerToBox(element, "marker", state, style, styleFor, resolver, targetCollector, cs)
		if mb != nil {
			markerBoxes = []Box{mb}
		}
		children = append(children, markerBoxes...)
	}

	vs := beforeAfterToBox(element, "before", state, styleFor, resolver, targetCollector, cs)
	children = append(children, vs...)
	// gopdfgo: gdy pierwsza litera leży w ::before, ::first-letter dziedziczy po ::before (CSS Pseudo 4
	// § 2.5; test WPT before-first-letter-selector-001); źródło zawsze dziedziczyło po elemencie.
	if box.Box().FirstLetterStyle != nil && len(vs) == 1 && hasVisibleText(vs[0]) {
		if restyler, ok := styleFor.(interface {
			PseudoStyleWithParent(*utils.HTMLNode, string, pr.ElementStyle) pr.ElementStyle
		}); ok {
			if style := restyler.PseudoStyleWithParent(element, "first-letter", vs[0].Box().Style); style != nil {
				box.Box().FirstLetterStyle = style
			}
		}
	}

	// collect anchor's counter_values, maybe it's a target.
	// to get the spec-conform counter_values we must do it here,
	// after the ::before is parsed and before the ::after is
	if anchor := string(style.GetAnchor()); anchor != "" {
		targetCollector.StoreTarget(anchor, counterValues, box)
	}

	if text := element.Data; element.Type == html.TextNode && text != "" {
		children = append(children, TextBoxAnonymousFrom(box, text))
	}

	// gopdfgo: tekst doklejamy tylko do tekstu tego samego elementu. Ostatnim dzieckiem bywa tekst
	// elementu z display: contents (jego dzieci przechodzą do rodzica) o innym stylu, np. innym
	// white-space; źródło doklejało do niego tekst rodzica (test WPT white-space-applies-to-text-001).
	var lastOwnText *TextBox
	if len(children) > 0 {
		lastOwnText, _ = children[len(children)-1].(*TextBox)
	}
	for _, childElement := range element.NodeChildren(false) {
		// utils.HTMLNode as no notion of tail. Instead, text are converted in text nodes
		if ok, text := childElement.IsText(); ok && text != "" {
			textBox := TextBoxAnonymousFrom(box, text)
			if len(children) > 0 {
				// TextBox is a leaf in inheritance tree, so we can type assert against the concrete type
				// instead of using interfaces.
				if ct, ok := children[len(children)-1].(*TextBox); ok && ct == lastOwnText {
					ct.Text = append(ct.Text, textBox.Text...)
				} else {
					children = append(children, textBox)
					lastOwnText = textBox
				}
			} else {
				children = append(children, textBox)
				lastOwnText = textBox
			}
		} else {
			childBoxes := elementToBox(childElement, styleFor, resolver, baseUrl, targetCollector, cs, state, footnotes)
			if len(childBoxes) != 0 && childBoxes[0].Box().Style.GetFloat() == "footnote" {
				footnote := childBoxes[0]
				footnote.Box().Style.SetFloat("none")
				(*footnotes) = append(*footnotes, footnote)
				callStyle := styleFor.Get((*utils.HTMLNode)(footnote.Box().Element), "footnote-call")
				footnoteCall, err := makeBox(callStyle, nil, (*utils.HTMLNode)(footnote.Box().Element), "footnote-call")
				if err != nil {
					logger.WarningLogger.Println(err)
					return nil
				}
				footnoteCall.Box().Children = ContentToBoxes(
					callStyle, footnoteCall, state.QuoteDepth, state.CounterValues,
					resolver, targetCollector, cs, nil, nil)
				footnoteCall.Box().Footnote = footnote
				childBoxes = []Box{footnoteCall}
			}
			children = append(children, childBoxes...)
		}
	}
	children = append(children, beforeAfterToBox(element, "after", state, styleFor, resolver, targetCollector, cs)...)

	// Scopes created by this element’s children stop here.
	counterScope := state.CounterScopes[len(state.CounterScopes)-1]
	state.CounterScopes = state.CounterScopes[:len(state.CounterScopes)-1]
	for name := range counterScope {
		counterValues[name] = counterValues[name][:len(counterValues[name])-1]
		if len(counterValues[name]) == 0 {
			delete(counterValues, name)
		}
	}
	PopNestedCounters(state, nestedCounters)

	box.Box().Children = children
	if style.GetAppearance() != "none" {
		box.Box().Children = nil
	}

	// gopdfgo: white-space-trim przed fazą I przetwarzania białych znaków (CSS Text 4 § 4.2)
	if BlockContainerT.IsInstance(box) {
		ApplyWhiteSpaceTrim(box)
	}
	ProcessWhitespace(box, false)
	setContentLists(element, box, style, counterValues, targetCollector, cs)
	ProcessTextTransform(box)
	// gopdfgo: dzielenie wyrazów rozciętych granicami elementów, zob. HyphenateAcrossBoxes
	if BlockContainerT.IsInstance(box) {
		HyphenateAcrossBoxes(box)
	}

	if len(markerBoxes) > 0 && len(box.Box().Children) == 1 {
		// See https://www.w3.org/TR/css-lists-3/#list-style-position-outside
		//
		// "The size or contents of the marker box may affect the height of the
		//  principal block box and/or the height of its first line box, and in
		//  some cases may cause the creation of a new line box; this
		//  interaction is also not defined."
		//
		// We decide here to add a zero-width space to have a minimum
		// height. Adding text boxes is not the best idea, but it's not a good
		// moment to add an empty line box, and the specification lets us do
		// almost what we want, so…
		if style.GetListStylePosition() == "outside" {
			box.Box().Children = append(box.Box().Children, TextBoxAnonymousFrom(box, "\u200b"))
		}
	}
	if len(markerBoxes) > 0 && markerBoxes[0].Box().IsOutsideMarker {
		addOutsideImageMarkerStrut(box)
	}

	if style.GetFloat() == "footnote" {
		state.CounterValues["footnote"][len(state.CounterValues["footnote"])-1] += 1
		markerStyle := styleFor.Get(element, "footnote-marker")
		marker, err := makeBox(markerStyle, nil, element, "footnote-marker")
		if err != nil {
			logger.WarningLogger.Println(err)
			return nil
		}
		marker.Box().Children = ContentToBoxes(
			markerStyle, marker, state.QuoteDepth, state.CounterValues, resolver,
			targetCollector, cs, nil, nil)
		box.Box().Children = append([]Box{marker}, box.Box().Children...)
	}
	// gopdfgo: display: contents (CSS Display 3 § 2.5): element nie tworzy pudełka, a jego dzieci
	// i pseudoelementy przechodzą do rodzica. Pudełko liniowe zbudowane wyżej służyło tylko do tego,
	// żeby teksty odziedziczyły styl elementu. Elementy zastępowane i formularzowe zostają pudełkami.
	if display.Contents && !keepsBoxWithDisplayContents(element) {
		return box.Box().Children
	}
	// Specific handling for the element. (eg. replaced element)
	return handleElement(element, box, resolver, baseUrl)
}

// generatesNoBox mówi, czy element z display: contents nie tworzy własnego pudełka.
func generatesNoBox(element *utils.HTMLNode, display pr.Display) bool {
	return display.Contents && !keepsBoxWithDisplayContents(element)
}

// keepsBoxWithDisplayContents: elementy, dla których display: contents nie znaczy "tylko dzieci"
// (CSS Display 3, dodatek B wymienia je jako traktowane osobno); tu zachowują zwykłe pudełko.
func keepsBoxWithDisplayContents(element *utils.HTMLNode) bool {
	switch element.DataAtom {
	case atom.Img, atom.Svg, atom.Br, atom.Wbr, atom.Input, atom.Select, atom.Textarea, atom.Button,
		atom.Canvas, atom.Video, atom.Audio, atom.Object, atom.Embed, atom.Iframe, atom.Meter, atom.Progress:
		return true
	}
	return false
}

// Yield the box for ::before or ::after pseudo-element.
func beforeAfterToBox(element *utils.HTMLNode, pseudoType string, state *tree.PageState, styleFor styleForI,
	resolver URLResolver, targetCollector *tree.TargetCollector, cs counters.CounterStyle,
) []Box {
	style := styleFor.Get(element, pseudoType)
	if pseudoType != "" && style == nil {
		// Pseudo-elements with no style at all do not get a style dict.
		// Their initial content property computes to "none".
		return nil
	}

	display := style.GetDisplay()
	content := style.GetContent()
	if display == (pr.Display{Outside: pr.None}) || content.String == "none" || content.String == "normal" || content.String == "inhibit" {
		return nil
	}

	box, err := makeBox(style, nil, element, pseudoType)
	if err != nil {
		logger.WarningLogger.Println(err)
		return nil
	}

	nestedCounters := UpdateCounters(state, style)
	defer PopNestedCounters(state, nestedCounters)

	var children []Box
	if display.Has(pr.ListItem) {
		mb := markerToBox(element, pseudoType+"::marker", state, style, styleFor, resolver, targetCollector, cs)
		if mb != nil {
			children = append(children, mb)
		}
	}
	children = append(children, ContentToBoxes(
		style, box, state.QuoteDepth, state.CounterValues, resolver, targetCollector, cs, nil, nil)...)

	// gopdfgo: pojedynczy <image> w content (CSS Content 3 § 1, <content-replacement>) ma rozmiar
	// pseudoelementu: obraz dostaje jego width i height. Norma czyni wtedy zastępowanym sam
	// pseudoelement; przyjmujemy model Chromium (pseudoelement zwykły, obraz z jego wymiarami,
	// test WPT pseudo-element-inline-box), który daje ten sam rozmiar obrazu. W źródle gradient
	// bez wymiarów naturalnych dostawał 300×150 px.
	if len(children) == 1 && replacementImage(style, resolver) != nil {
		if image, ok := children[0].(*InlineReplacedBox); ok {
			image.Style.SetWidth(style.GetWidth())
			image.Style.SetHeight(style.GetHeight())
		}
	}

	box.Box().Children = children

	// calculate the bookmark-label
	if style.GetBookmarkLevel().Tag != pr.None {
		computeBookmarkLabel(element, box, style.GetBookmarkLabel(), state.CounterValues, targetCollector, cs)
	}

	return []Box{box}
}

// Yield the box for ::marker pseudo-element if there is one.
// https://drafts.csswg.org/css-lists-3/#marker-pseudo
//
// gopdfgo: markerType to "marker" albo, dla ::before i ::after z display: list-item,
// "before::marker" i "after::marker" (CSS Pseudo 4 § 4.2); źródło brało zawsze ::marker elementu.
func markerToBox(element *utils.HTMLNode, markerType string, state *tree.PageState, parentStyle pr.ElementStyle, styleFor styleForI,
	resolver URLResolver, targetCollector *tree.TargetCollector, cs counters.CounterStyle,
) Box {
	style := styleFor.Get(element, markerType)
	if style == nil {
		return nil
	}

	box, err := makeBox(style, nil, element, markerType)
	if err != nil {
		logger.WarningLogger.Println(err)
		return nil
	}

	children := &box.Box().Children
	symbolMarker := false // znacznik domyślny z list-style-type: disc, circle, square albo disclosure-*
	symbolWidth := 1.36   // szerokość znacznika-symbolu wewnątrz elementu, w em

	if style.GetDisplay() == (pr.Display{Outside: pr.None}) || style.GetContent().String == "none" {
		return nil
	}

	// gopdfgo: counter-* w ::marker działają jak w ::before (CSS Lists 3 § 3, § 4: „all elements”);
	// źródło je pomijało. Licznik założony przez znacznik obejmuje resztę elementu listy, także
	// ::after (test WPT marker-counter), więc trafia do zasięgu rodzeństwa znacznika.
	updateCounters(state, style, nil, true)

	image := style.GetListStyleImage()

	if content := style.GetContent().String; content != "normal" && content != "inhibit" {
		*children = append(*children, ContentToBoxes(style, box, state.QuoteDepth, state.CounterValues,
			resolver, targetCollector, cs, nil, nil)...)
	} else {
		// gopdfgo: znacznikiem może być dowolny <image>, także gradient (CSS Lists 3 § 3.1); źródło
		// obsługiwało tylko url().
		if _, none := image.(pr.NoneImage); image != nil && !none {
			// image may be None here too, in case the image is not available.
			image_ := contentImage(pr.ContentProperty{Type: "image", Content: pr.ContentImage{Image: image}}, style, resolver)
			if image_ != nil {
				markerBox := InlineReplacedBoxAnonymousFrom(box, images.MarkerImage(image_))
				*children = append(*children, markerBox)
			}
		}
		if len(*children) == 0 && style.GetListStyleType().Name != "none" {
			counterValue_, has := state.CounterValues["list-item"]
			if !has {
				counterValue_ = []int{0}
			}
			counterValue := counterValue_[len(counterValue_)-1]
			switch style.GetListStyleType().Name {
			case "disc", "circle", "square":
				symbolMarker = true
			case "disclosure-open", "disclosure-closed":
				symbolMarker = true
				symbolWidth = 1.06
			}
			if markerText := cs.RenderMarker(style.GetListStyleType(), counterValue); markerText != "" {
				markerBox := TextBoxAnonymousFrom(box, markerText)
				markerBox.Box().Style.SetWhiteSpace(pr.PreWrap)
				*children = append(*children, markerBox)
			}
		}
	}

	if len(*children) == 0 {
		return nil
	}
	var markerBox Box
	// gopdfgo: dla elementu listy będącego pudełkiem liniowym (display: inline list-item) outside
	// działa jak inside (CSS Lists 3 § 3.3); źródło stawiało znacznik zewnętrzny
	if parentStyle.GetListStylePosition() == "outside" && !(parentStyle.GetDisplay().Has(pr.Inline) && !parentStyle.GetDisplay().Has(pr.FlowRoot)) {
		markerBox = BlockBoxAnonymousFrom(box, *children)
		// We can safely edit everything that can't be changed by user style
		// See https://drafts.csswg.org/css-pseudo-4/#marker-pseudo
		markerBox.Box().Style.SetPosition(pr.BoolString{String: "absolute"})
		markerBox.Box().IsOutsideMarker = true
		// gopdfgo: obraz znacznika zewnętrznego stoi 7 px przed treścią, jak w Chromium (stała
		// kCMarkerPaddingPx); tekst znacznika ma własną spację, więc go to nie dotyczy
		if len(*children) == 1 && InlineReplacedT.IsInstance((*children)[0]) && parentStyle.GetDirection() == pr.Ltr {
			markerBox.Box().Style.SetMarginLeft(pr.FToPx(-7))
		}
		// translateX := pr.Dimension{Value: 100, Unit: pr.Perc}
		// if parentStyle.GetDirection() == "ltr" {
		// 	translateX = pr.Dimension{Value: -100, Unit: pr.Perc}
		// }
		// translateY := pr.ZeroPixels
		// markerBox.Box().Style.SetTransform(pr.Transforms{{String: "translate", Dimensions: pr.Dimensions{translateX, translateY}}})
	} else if symbolMarker {
		// gopdfgo: znacznik-symbol (disc, circle, square) wewnątrz elementu zajmuje stałą szerokość,
		// niezależną od glifu. Norma zostawia ją implementacji (CSS Lists 3 § 3.1); przyjmujemy pomiar
		// z Chromium z 2026-11-16 (tekst zaczyna się 14,67, 26,67 i 54,67 px od krawędzi przy piśmie 10,
		// 20 i 40 px, czyli ok. 1,36 em). W źródle szerokością był glif z odstępem (ok. 0,6 em), więc
		// tekst elementu łamał się w innych miejscach niż w przeglądarce.
		inlineBlock := InlineBlockBoxAnonymousFrom(box, *children)
		// Trójkąt disclosure-open i disclosure-closed (znacznik summary) zajmuje w Chromium 1,06 em
		// (pomiar z 2026-11-18: tekst summary 106 px od krawędzi przy piśmie 100 px, 10,6 px przy 10 px);
		// wcześniej szerokością był glif z fontu awaryjnego ze spacją (ok. 0,57 em).
		inlineBlock.Box().Style.SetWidth(pr.FToPx(pr.Float(symbolWidth) * style.GetFontSize().Value))
		markerBox = inlineBlock
	} else {
		markerBox = InlineBoxAnonymousFrom(box, *children)
	}
	return markerBox
}

// Collect missing counters.
func collectMissingCounter(counterName string, counterValues tree.CounterValues, missingCounters utils.Set) {
	for s := range counterValues {
		if s == counterName {
			return
		}
	}
	missingCounters.Add(counterName)
}

// Collect missing target counters.
//
// The corresponding TargetLookupItem caches the target"s page based
// counter values during pagination.
func collectMissingTargetCounter(counterName string, lookupCounterValues tree.CounterValues,
	anchorName string, missingTargetCounters map[string]utils.Set,
) {
	if _, in := lookupCounterValues[counterName]; !in {
		missingCounters := missingTargetCounters[anchorName]
		if missingCounters == nil {
			missingCounters = make(utils.Set)
		}
		for s := range missingCounters {
			if counterName == s {
				return
			}
		}
		missingCounters.Add(counterName)
		missingTargetCounters[anchorName] = missingCounters
	}
}

// contentImage zamienia element "url" albo "image" listy content na obraz; nil, gdy obrazu nie ma.
// Przystanki gradientu z currentcolor biorą kolor pudełka (CSS Images 3 § 3.4).
func contentImage(content pr.ContentProperty, style pr.ElementStyle, resolver URLResolver) images.Image {
	resolveStops := func(stops pr.ColorsStops) pr.ColorsStops {
		out := append(pr.ColorsStops(nil), stops...)
		for i, stop := range out {
			// gopdfgo: także barwy względne zależne od currentColor (CSS Color 5)
			out[i].Color = stop.Color.ResolveCurrentColor(style.GetColor())
		}
		return out
	}
	switch content.Type {
	case "url":
		value, ok := content.Content.(pr.TaggedString)
		if !ok || value.Tag != pr.External || resolver.FetchImage == nil {
			return nil
		}
		if image := resolver.FetchImage(value.S, "", style.GetImageOrientation()); image != nil {
			return image
		}
	case "image":
		switch image := content.Content.(pr.ContentImage).Image.(type) {
		case pr.UrlImage:
			if resolver.FetchImage != nil {
				if image := resolver.FetchImage(string(image), "", style.GetImageOrientation()); image != nil {
					return image
				}
			}
		case pr.LinearGradient:
			image.ColorStops = resolveStops(image.ColorStops)
			return images.NewLinearGradient(image)
		case pr.RadialGradient:
			image.ColorStops = resolveStops(image.ColorStops)
			return images.NewRadialGradient(image)
		}
	}
	return nil
}

// replacementImage podaje obraz, którym content zastępuje zawartość elementu (CSS Content 3 § 1:
// pojedynczy <image> na elemencie czyni go elementem zastępowanym, także <img>); nil, gdy content
// tego nie robi albo obrazu nie da się wczytać.
// gopdfgo: źródło stosowało content tylko do pseudoelementów.
func replacementImage(style pr.ElementStyle, resolver URLResolver) images.Image {
	contents := style.GetContent().Contents
	if len(contents) != 1 || (contents[0].Type != "url" && contents[0].Type != "image") {
		return nil
	}
	return contentImage(contents[0], style, resolver)
}

// Compute and return the boxes corresponding to the “content_list“.
//
// “parseAgain“ is called to compute the “content_list“ again when
// “target_collector.lookup_target()“ detected a pending target.
//
// “build_formatting_structure“ calls
// “target_collector.check_pending_targets()“ after the first pass to do
// required reparsing.
func computeContentList(contentList pr.ContentProperties, parentBox Box, counterValues tree.CounterValues,
	cssToken string, parseAgain tree.ParseFunc, targetCollector *tree.TargetCollector, cs counters.CounterStyle,
	resolver URLResolver, quoteDepth []int, quoteStyle pr.Quotes, lang pr.TaggedString, context Context, page Box,
) []Box {
	contentBoxes := []Box{}

	missingCounters := utils.Set{}
	missingTargetCounters := map[string]utils.Set{}
	inPageContext := context != nil && page != nil

	// Collect missing counters during build_formatting_structure.
	// Pointless to collect missing target counters in MarginBoxes.
	needCollectMissing := targetCollector.IsCollecting() && !inPageContext

	if parentBox.Box().cachedCounterValues == nil {
		// Store the counter_values in the parent_box to make them accessible
		// in @page context. Obsoletes the parse_again function's deepcopy.
		parentBox.Box().cachedCounterValues = counterValues.Copy()
	}

	var hasText bool
	addText := func(text string) {
		hasText = true
		if text == "" {
			return
		}
		if L := len(contentBoxes); L != 0 {
			if textBox, ok := contentBoxes[L-1].(*TextBox); ok {
				textBox.Text = append(textBox.Text, []rune(text)...)
				return
			}
		}
		contentBoxes = append(contentBoxes, TextBoxAnonymousFrom(parentBox, text))
	}

outerLoop:
	for _, content := range contentList {
		switch content.Type {
		case "string":
			addText(content.AsString())
		case "url":
			if resolver.FetchImage == nil {
				continue
			}
			value := content.Content.(pr.TaggedString)
			if value.Tag != pr.External {
				// Embedding internal references is impossible
				continue
			}
			image := resolver.FetchImage(value.S, "", parentBox.Box().Style.GetImageOrientation())
			if image != nil {
				contentBoxes = append(contentBoxes, InlineReplacedBoxAnonymousFrom(parentBox, image))
			}
		case "image": // gopdfgo: gradienty i image-set() w content (CSS Content 3 § 1)
			if image := contentImage(content, parentBox.Box().Style, resolver); image != nil {
				contentBoxes = append(contentBoxes, InlineReplacedBoxAnonymousFrom(parentBox, image))
			}
		case "content()":
			addedText := extractText(content.AsString(), parentBox)
			// Simulate the step of white space processing
			// (normally done during the layout)
			addedText = strings.TrimSpace(addedText)
			addText(addedText)
		case "string()":
			value := content.AsStrings()
			if !inPageContext {
				// string() is currently only valid in @page context
				// See https://github.com/Kozea/WeasyPrint/issues/723
				logger.WarningLogger.Printf("'string(%s)' is only allowed in page margins", strings.Join(value, " "))
				continue
			}
			if len(value) == 1 {
				value = append(value, "first")
			}
			addText(context.GetStringSetFor(page, value[0], value[1]))
		case "counter()":
			counterName, counterStyle := content.AsCounter()
			if needCollectMissing {
				collectMissingCounter(counterName, counterValues, missingCounters)
			}
			if counterStyle.Name == "none" {
				continue
			}
			cv, has := counterValues[counterName]
			if !has {
				cv = []int{0}
			}
			counterValue := cv[len(cv)-1]
			addText(cs.RenderValueStyle(counterValue, counterStyle))
		case "counters()":
			counterName, separator, counterStyle := content.AsCounters()
			if needCollectMissing {
				collectMissingCounter(counterName, counterValues, missingCounters)
			}
			if counterStyle.Name == "none" {
				continue
			}
			vs, has := counterValues[counterName]
			if !has {
				vs = []int{0}
			}
			styles := make([]string, len(vs))
			for i, counterValue := range vs {
				styles[i] = cs.RenderValueStyle(counterValue, counterStyle)
			}
			addText(strings.Join(styles, separator))
		case "target-counter()":
			anchorToken, counterName, counterStyle := content.AsTargetCounter()
			lookupTarget := targetCollector.LookupTarget(anchorToken, parentBox, cssToken, parseAgain)
			if lookupTarget.IsUpToDate() {
				targetValues := lookupTarget.TargetBox.CachedCounterValues()
				if needCollectMissing {
					collectMissingTargetCounter(counterName, targetValues,
						tree.AnchorNameFromToken(anchorToken),
						missingTargetCounters)
				}
				// Mixin target"s cached page counters.
				// cachedPageCounterValues are empty during layout.
				localCounters := lookupTarget.CachedPageCounterValues.Copy()
				localCounters.Update(targetValues)
				vs, has := localCounters[counterName]
				if !has {
					vs = []int{0}
				}
				counterValue := vs[len(vs)-1]
				addText(cs.RenderValue(counterValue, counterStyle))
			} else {
				break outerLoop
			}
		case "target-counters()":
			anchorToken, counterName, separator, counterStyle := content.AsTargetCounters()
			lookupTarget := targetCollector.LookupTarget(
				anchorToken, parentBox, cssToken, parseAgain)
			if lookupTarget.IsUpToDate() {
				if separator.Type != "string" {
					break outerLoop
				}
				separatorString := separator.AsString()
				targetValues := lookupTarget.TargetBox.CachedCounterValues()
				if needCollectMissing {
					collectMissingTargetCounter(
						counterName, targetValues,
						tree.AnchorNameFromToken(anchorToken),
						missingTargetCounters)
				}
				// Mixin target"s cached page counters.
				// cachedPageCounterValues are empty during layout.
				localCounters := lookupTarget.CachedPageCounterValues.Copy()
				localCounters.Update(targetValues)
				vs, has := localCounters[counterName]
				if !has {
					vs = []int{0}
				}
				tmps := make([]string, len(vs))
				for j, counterValue := range vs {
					tmps[j] = cs.RenderValue(counterValue, counterStyle)
				}
				addText(strings.Join(tmps, separatorString))
			} else {
				break outerLoop
			}
		case "target-text()":
			anchorToken, textStyle := content.AsTargetText()
			lookupTarget := targetCollector.LookupTarget(
				anchorToken, parentBox, cssToken, parseAgain)
			if lookupTarget.IsUpToDate() {
				targetBox := lookupTarget.TargetBox
				text := extractText(textStyle, targetBox.(Box))
				// Simulate the step of white space processing
				// (normally done during the layout)
				addText(strings.TrimSpace(text))
			} else {
				break outerLoop
			}
		case "quote":
			if quoteDepth != nil && !quoteStyle.IsNone() {
				value := content.AsQuote()
				isOpen := value.Open
				insert := value.Insert && quoteStyle.Tag != pr.None
				if !isOpen {
					quoteDepth[0] = max(0, quoteDepth[0]-1)
				}
				if insert {
					openQuotes, closeQuotes := quoteStyle.Open, quoteStyle.Close
					if quoteStyle.Tag == pr.Auto {
						openQuotes, closeQuotes = text.GetLangQuotes(lang.S)
					}
					quotes := closeQuotes
					if isOpen {
						quotes = openQuotes
					}
					addText(quotes[min(quoteDepth[0], len(quotes)-1)])
				}
				if isOpen {
					quoteDepth[0] += 1
				}
			}
		case "element()":
			value := content.AsStrings()
			if !inPageContext {
				logger.WarningLogger.Printf("element(%s) is only allowed in page margins", strings.Join(value, " "))
				continue
			}
			if len(value) == 1 {
				value = append(value, "first")
			}
			newBox := context.GetRunningElementFor(page, value[0], value[1])
			if newBox == nil {
				continue
			}

			newBox = Deepcopy(newBox)

			newBox.Box().Style.SetPosition(pr.BoolString{String: "static"})

			if ParentT.IsInstance(newBox) {
				for _, child := range Descendants(newBox) {
					if content := child.Box().Style.GetContent(); content.String == "normal" || content.String == "none" {
						continue
					}
					child.Box().Children = ContentToBoxes(
						child.Box().Style, child, quoteDepth, counterValues,
						resolver, targetCollector, cs, context, page)
				}
			}
			contentBoxes = append(contentBoxes, newBox)
		case "leader()":
			ld := content.AsLeader()
			if ld == "" {
				continue
			}

			textBox := TextBoxAnonymousFrom(parentBox, ld)
			leaderBox := InlineBoxAnonymousFrom(parentBox, []Box{textBox})
			// Avoid breaks inside the leader box
			leaderBox.Style.SetWhiteSpace(pr.Pre)
			// Prevent whitespaces from being removed from the text box
			textBox.Style.SetWhiteSpace(pr.Pre)
			leaderBox.IsLeader = true
			contentBoxes = append(contentBoxes, leaderBox)
		}
	}

	if hasText || len(contentBoxes) > 0 {
		// Only add CounterLookupItem if the content_list actually produced text
		targetCollector.CollectMissingCounters(parentBox, cssToken, parseAgain, missingCounters, missingTargetCounters)
		return contentBoxes
	}
	return nil
}

// Takes the value of a “content“ property and yield boxes.
func ContentToBoxes(style pr.ElementStyle, parentBox Box, quoteDepth []int, counterValues tree.CounterValues,
	resolver URLResolver, targetCollector *tree.TargetCollector, cs counters.CounterStyle, context Context, page Box,
) []Box {
	origQuoteDepth := make([]int, len(quoteDepth))

	// Closure to parse the ``parentBoxes`` children all again.
	parseAgain := func(mixinPagebasedCounters tree.CounterValues) {
		// Neither alters the mixed-in nor the cached counter values, no
		// need to deepcopy here
		localCounters := mixinPagebasedCounters.Copy()
		for k, v := range parentBox.Box().cachedCounterValues {
			localCounters[k] = v
		}

		var localChildren []Box
		localChildren = append(localChildren, ContentToBoxes(
			style, parentBox, origQuoteDepth, localCounters,
			resolver, targetCollector, cs, nil, nil)...)

		parentChildren := parentBox.Box().Children
		if len(parentChildren) == 1 && LineT.IsInstance(parentChildren[0]) {
			parentChildren[0].Box().Children = localChildren
		} else {
			parentBox.Box().Children = localChildren
		}
	}

	if style.GetContent().String == "inhibit" {
		return nil
	}

	copy(origQuoteDepth, quoteDepth)

	cssToken := "content"
	boxList := computeContentList(
		style.GetContent().Contents, parentBox, counterValues, cssToken, parseAgain,
		targetCollector, cs, resolver, quoteDepth, style.GetQuotes(), quoteLanguage(parentBox, style.GetLang()),
		context, page)
	return boxList
}

// quoteLanguage (gopdfgo): przy quotes: auto znaki cudzysłowu dobiera się według języka rodzica
// elementu, nie samego elementu (CSS Content 3 § 4.1): <q lang="fr"> w tekście angielskim dostaje
// cudzysłowy angielskie. Źródło brało język elementu. Gdy element nie ma własnego atrybutu lang,
// jego język jest językiem rodzica i zostaje wartość ze stylu.
func quoteLanguage(pseudo Box, own pr.TaggedString) pr.TaggedString {
	element := pseudo.Box().Element
	if element == nil || pseudo.Box().PseudoType == "" {
		return own
	}
	langOf := func(node *html.Node) (string, bool) {
		for _, attr := range node.Attr {
			if attr.Key == "lang" {
				return attr.Val, true
			}
		}
		return "", false
	}
	if _, has := langOf(element); !has {
		return own
	}
	for node := element.Parent; node != nil; node = node.Parent {
		if lang, has := langOf(node); has {
			return pr.TaggedString{Tag: own.Tag, S: lang}
		}
	}
	return pr.TaggedString{}
}

// Parse the content-list value of “stringName“ for “string-set“.
func computeStringSet(element *utils.HTMLNode, box Box, stringName string, contentList pr.ContentProperties,
	counterValues tree.CounterValues, targetCollector *tree.TargetCollector, cs counters.CounterStyle,
) {
	// Closure to parse the string-set string value all again.
	parseAgain := func(mixinPagebasedCounters tree.CounterValues) {
		// Neither alters the mixed-in nor the cached counter values, no
		// need to deepcopy here
		localCounters := mixinPagebasedCounters.Copy()
		for k, v := range box.Box().cachedCounterValues {
			localCounters[k] = v
		}

		computeStringSet(element, box, stringName, contentList, localCounters, targetCollector, cs)
	}

	cssToken := "string-set::" + stringName
	boxList := computeContentList(contentList, box, counterValues, cssToken, parseAgain,
		targetCollector, cs, URLResolver{}, nil, pr.Quotes{}, pr.TaggedString{}, nil, nil)
	if boxList != nil {
		var builder []rune
		for _, box1 := range boxList {
			if textBox, ok := box1.(*TextBox); ok {
				builder = append(builder, textBox.Text...)
			}
		}
		string_ := string(builder)
		// Avoid duplicates, care for parseAgain and missing counters, don't
		// change the pointer
		newStringSet := make(pr.ContentProperties, 0, len(box.Box().StringSet))
		for i, stringSet := range box.Box().StringSet {
			if stringSet.Type == stringName {
				newStringSet = append(newStringSet, box.Box().StringSet[i+1:]...)
				break
			}
			newStringSet = append(newStringSet, stringSet)
		}
		newStringSet = append(newStringSet, pr.ContentProperty{Type: stringName, Content: pr.String(string_)})
		box.Box().StringSet = newStringSet
	}
}

// Parses the content-list value for “bookmark-label“.
func computeBookmarkLabel(element *utils.HTMLNode, box Box, contentList pr.ContentProperties, counterValues tree.CounterValues,
	targetCollector *tree.TargetCollector, cs counters.CounterStyle,
) {
	// Closure to parse the bookmark-label all again..
	parseAgain := func(mixinPagebasedCounters tree.CounterValues) {
		// Neither alters the mixed-in nor the cached counter values, no
		// need to deepcopy here
		localCounters := mixinPagebasedCounters.Copy()
		for k, v := range box.Box().cachedCounterValues {
			localCounters[k] = v
		}
		computeBookmarkLabel(element, box, contentList, localCounters, targetCollector, cs)
	}

	cssToken := "bookmark-label"
	boxList := computeContentList(contentList, box, counterValues, cssToken, parseAgain, targetCollector, cs,
		URLResolver{}, nil, pr.Quotes{}, pr.TaggedString{}, nil, nil)

	var builder []rune
	for _, box := range boxList {
		if textBox, ok := box.(*TextBox); ok {
			builder = append(builder, textBox.Text...)
		}
	}
	box.Box().BookmarkLabel = string(builder)
}

// Set the content-lists values.
// These content-lists are used in GCPM properties like “string-set“ and
// “bookmark-label“.
func setContentLists(element *utils.HTMLNode, box Box, style pr.ElementStyle, counterValues tree.CounterValues,
	targetCollector *tree.TargetCollector, cs counters.CounterStyle,
) {
	if sss := style.GetStringSet(); sss.String != "none" {
		for _, c := range sss.Contents {
			stringName, stringValues := c.String, c.Contents
			computeStringSet(element, box, stringName, stringValues, counterValues, targetCollector, cs)
		}
	}
	if style.GetBookmarkLevel().Tag != pr.None {
		computeBookmarkLabel(element, box, style.GetBookmarkLabel(), counterValues, targetCollector, cs)
	}
}

// Handle the “counter-*“ properties.
// UpdateCounters stosuje counter-reset, counter-increment i counter-set elementu, w tej kolejności
// (CSS Lists 3 § 4.5). Zwraca nazwy liczników zagnieżdżonych, które trzeba zdjąć wywołaniem
// [PopNestedCounters], gdy element się kończy.
//
// gopdfgo: w źródle counter-set działał przed counter-increment, a licznik utworzony przez element,
// którego przodek ma już licznik o tej nazwie, był widoczny także dla następnego rodzeństwa. Według
// CSS Lists 3 § 4.3 rodzeństwo dziedziczy od poprzednika tylko liczniki, których nie ma u rodzica.
func UpdateCounters(state *tree.PageState, style pr.ElementStyle) (nested []string) {
	return updateCounters(state, style, nil, false)
}

// updateCounters: reversedStart podaje wartość początkową licznika odwróconego
// bez podanej wartości (counter-reset: reversed(nazwa)); nil oznacza zero. Przy siblingScoped
// licznik zagnieżdżony w liczniku przodka jest widoczny dla następnego rodzeństwa (::marker).
func updateCounters(state *tree.PageState, style pr.ElementStyle, reversedStart func(name string, withSiblings bool) int,
	siblingScoped bool,
) (nested []string) {
	counterValues, counterScopes := state.CounterValues, state.CounterScopes
	siblingScopes := counterScopes[len(counterScopes)-1]

	for _, nv := range style.GetCounterReset().Values {
		slice := counterValues[nv.String]
		if nv.AutoStart && reversedStart != nil {
			// licznik zagnieżdżony w liczniku przodka nie obejmuje rodzeństwa
			nv.Int = reversedStart(nv.String, siblingScopes.Has(nv.String) || len(slice) == 0)
		}
		switch {
		case siblingScopes.Has(nv.String): // licznik utworzony przez rodzeństwo: zastępujemy go
			slice = slice[:len(slice)-1]
		case len(slice) == 0 || siblingScoped: // nowy licznik, widoczny dla następnego rodzeństwa
			siblingScopes.Add(nv.String)
		default: // licznik przodka: nowy, zagnieżdżony, tylko dla tego elementu i jego potomków
			nested = append(nested, nv.String)
		}
		counterValues[nv.String] = append(slice, clampCounter(nv.Int))
		state.SetCounterReversed(nv.String, len(slice), nv.Reversed)
	}

	counterIncrement := style.GetCounterIncrement()
	if counterIncrement.K == pr.Auto {
		// "auto" is the initial value but is not valid in stylesheet:
		// there was no counter-increment declaration for this element.
		// (Or the winning value was "initial".)
		counterIncrement = pr.SIntStrings{}
	}
	// https://drafts.csswg.org/css-lists-3/#declaring-a-list-item
	// gopdfgo: element listy zwiększa licznik list-item zawsze, gdy counter-increment go nie wymienia
	// (także przy "none" i przy innych licznikach), a w liczniku odwróconym przyrost domyślny to -1
	// (CSS Lists 3 § 4.6); źródło dodawało 1 tylko przy braku deklaracji.
	if implicitListItemIncrement(style) {
		step := 1
		if state.IsCounterReversed("list-item") {
			step = -1
		}
		values := append(pr.IntStrings{}, counterIncrement.Values...)
		counterIncrement = pr.SIntStrings{Values: append(values, pr.IntString{String: "list-item", Int: step})}
	}
	for _, ci := range counterIncrement.Values {
		values := counterValues[ci.String]
		if len(values) == 0 {
			siblingScopes.Add(ci.String)
			values = append(values, 0)
			state.SetCounterReversed(ci.String, 0, false)
		}
		// zwiększenie, które wyszłoby poza zakres, jest pomijane (CSS Lists 3 § 4.2)
		if sum := int64(values[len(values)-1]) + int64(ci.Int); sum >= math.MinInt32 && sum <= math.MaxInt32 {
			values[len(values)-1] = int(sum)
		}
		counterValues[ci.String] = values
	}

	for _, nv := range style.GetCounterSet().Values {
		values := counterValues[nv.String]
		if len(values) == 0 {
			siblingScopes.Add(nv.String)
			values = append(values, 0)
		}
		values[len(values)-1] = clampCounter(nv.Int)
		counterValues[nv.String] = values
	}
	return nested
}

// implicitListItemIncrement podaje, czy element listy zwiększa licznik list-item domyślnie,
// czyli czy jego counter-increment tego licznika nie wymienia (CSS Lists 3 § 4.6).
func implicitListItemIncrement(style pr.ElementStyle) bool {
	if !style.GetDisplay().Has(pr.ListItem) {
		return false
	}
	for _, ci := range style.GetCounterIncrement().Values {
		if ci.String == "list-item" {
			return false
		}
	}
	return true
}

// reversedCounterStart wyznacza wartość początkową licznika odwróconego bez
// podanej wartości według CSS Lists 3 § 4.4.2 („Instantiating counters”):
// przegląda elementy i pseudoelementy ::before i ::after w zasięgu licznika
// (element, jego potomków, a gdy licznik nie jest zagnieżdżony w liczniku
// przodka, także następne rodzeństwo z potomkami) i sumuje przyrosty
// z przeciwnym znakiem; na pierwszym counter-set dolicza ustawianą wartość
// i kończy, a na końcu dolicza ostatni niezerowy przyrost z przeciwnym znakiem.
//
// gopdfgo: wcześniej pseudoelementy były pomijane, przy counter-set ostatni
// przyrost nie był doliczany, a przyrost elementu z counter-set był sumowany;
// licznik zagnieżdżony obejmował też następne rodzeństwo (testy WPT
// counter-reset-reversed-pseudo-*, -nested, li-value-reversed-007…009).
func reversedCounterStart(origin *utils.HTMLNode, styleFor styleForI, name string, withSiblings bool) int {
	num, lastNegated, done := 0, 0, false
	// count obsługuje jeden element albo pseudoelement el (kroki 3.1–3.4)
	count := func(style pr.ElementStyle, isElement bool) {
		increment, counts := 0, false
		if isElement && name == "list-item" && implicitListItemIncrement(style) {
			increment, counts = -1, true // przyrost domyślny w liczniku odwróconym
		} else {
			for _, ci := range style.GetCounterIncrement().Values {
				if ci.String == name {
					increment, counts = increment+ci.Int, true
				}
			}
		}
		set, hasSet := 0, false
		for _, cs := range style.GetCounterSet().Values {
			if cs.String == name {
				set, hasSet = cs.Int, true
			}
		}
		if !counts && !hasSet {
			return
		}
		if increment != 0 {
			lastNegated = -increment
		}
		if hasSet {
			num, done = num+set, true
			return
		}
		num -= increment
	}
	pseudo := func(element *utils.HTMLNode, pseudoType string) {
		style := styleFor.Get(element, pseudoType)
		if style == nil || style.GetDisplay() == (pr.Display{Outside: pr.None}) {
			return
		}
		if c := style.GetContent().String; c == "none" || c == "normal" || c == "inhibit" {
			return
		}
		count(style, false)
	}
	var visit func(element *utils.HTMLNode, isOrigin bool) (resets bool)
	visit = func(element *utils.HTMLNode, isOrigin bool) bool {
		style := styleFor.Get(element, "")
		if style == nil || style.GetDisplay() == (pr.Display{Outside: pr.None}) {
			return false
		}
		noBox := generatesNoBox(element, style.GetDisplay())
		if !isOrigin && !noBox {
			for _, reset := range style.GetCounterReset().Values {
				if reset.String == name {
					return true // nowy licznik: element i jego potomkowie są poza zasięgiem
				}
			}
		}
		if !noBox {
			count(style, true)
		}
		if !done {
			pseudo(element, "before")
		}
		for child := element.FirstChild; child != nil && !done; child = child.NextSibling {
			if child.Type == html.ElementNode {
				visit((*utils.HTMLNode)(child), false)
			}
		}
		if !done {
			pseudo(element, "after")
		}
		return false
	}
	visit(origin, true)
	for sibling := origin.NextSibling; withSiblings && sibling != nil && !done; sibling = sibling.NextSibling {
		if sibling.Type != html.ElementNode {
			continue
		}
		if visit((*utils.HTMLNode)(sibling), false) {
			break // rodzeństwo zakłada licznik od nowa: koniec zasięgu
		}
	}
	return num + lastNegated
}

// clampCounter ogranicza wartość licznika do zakresu 32 bitów ze znakiem, na którym działają style
// liczników; wartość spoza zakresu wpędzała je w nieskończoną rekurencję zastępczych stylów.
func clampCounter(value int) int {
	return min(max(value, math.MinInt32), math.MaxInt32)
}

// PopNestedCounters zdejmuje liczniki zagnieżdżone zwrócone przez [UpdateCounters].
func PopNestedCounters(state *tree.PageState, nested []string) {
	for _, name := range nested {
		if values := state.CounterValues[name]; len(values) > 1 {
			state.CounterValues[name] = values[:len(values)-1]
		}
	}
}

var reHasNonWhitespace = regexp.MustCompile(`\S`)

func hasNonWhitespaceDefault(text string) bool { return reHasNonWhitespace.MatchString(text) }

// Return true if “box“ is a TextBox with only whitespace.
func isWhitespace(box Box, hasNonWhitespace func(string) bool) bool {
	if hasNonWhitespace == nil {
		hasNonWhitespace = hasNonWhitespaceDefault
	}
	textBox, is := box.(*TextBox)
	return is && !hasNonWhitespace(textBox.TextS())
}

type wrapImproperIterator struct {
	box      Box
	children boxIterator

	currentBox Box // box to return
	stackBox   Box // may store an additional Box to yield

	test           func(Box) bool
	improper       []Box
	wrapperBoxType BoxType
}

func (iter *wrapImproperIterator) Next() bool {
	if iter.stackBox != nil {
		iter.currentBox = iter.stackBox
		iter.stackBox = nil
		iter.improper = iter.improper[:0]
		return true
	}

	for iter.children.Next() { // process the next input box
		child := iter.children.Box()
		if iter.test(child) {
			if len(iter.improper) > 0 {
				wrapper := iter.wrapperBoxType.AnonymousFrom(iter.box, nil)
				// Apply the rules again on the new wrapper
				iter.currentBox = tableBoxesChildren(wrapper, iter.improper)
				iter.stackBox = child
			} else {
				iter.currentBox = child
			}
			return true
		} else {
			iter.improper = append(iter.improper, child)
		}
	}

	if len(iter.improper) > 0 {
		wrapper := iter.wrapperBoxType.AnonymousFrom(iter.box, nil)
		// Apply the rules again on the new wrapper
		iter.currentBox = tableBoxesChildren(wrapper, iter.improper)
		iter.improper = iter.improper[:0]
		return true
	}

	return false
}

func (iter *wrapImproperIterator) Box() Box { return iter.currentBox }

//	Wrap consecutive children that do not pass ``test`` in a box of type
//
// “test“ defaults to children being of the same type as “wrapperType“.
func wrapImproper(box Box, children boxIterator, wrapperBoxType BoxType, test func(Box) bool) *wrapImproperIterator {
	if test == nil {
		test = wrapperBoxType.IsInstance
	}
	return &wrapImproperIterator{box: box, children: children, wrapperBoxType: wrapperBoxType, test: test}
}

// Remove and add boxes according to the table model.
//
// Take and return a “Box“ object.
//
// See https://www.w3.org/TR/CSS21/tables.html#anonymous-boxes
func AnonymousTableBoxes(box Box) Box {
	if !ParentT.IsInstance(box) || box.Box().IsRunning() {
		return box
	}

	// Do recursion.
	boxChildren := box.Box().Children
	children := make([]Box, len(boxChildren))
	for index, child := range boxChildren {
		children[index] = AnonymousTableBoxes(child)
	}
	return tableBoxesChildren(box, children)
}

// Internal implementation of AnonymousTableBoxes().box
func tableBoxesChildren(box Box, children []Box) Box {
	if TableColumnT.IsInstance(box) { // rule 1.1
		// Remove all children.
		children = nil
	} else if tableBox, ok := box.(*TableColumnGroupBox); ok { // rule 1.2
		// Remove children other than table-column.
		newChildren := make([]Box, 0, len(children))
		for _, child := range children {
			if TableColumnT.IsInstance(child) {
				newChildren = append(newChildren, child)
			}
		}
		children = newChildren

		// Rule XXX (not in the spec): column groups have at least
		// one column child.
		if len(children) == 0 {
			span := tableBox.span()
			if span < 1 {
				span = 1
			}

			for i := 0; i < span; i++ {
				children = append(children, TableColumnBoxAnonymousFrom(box, nil))
			}
		}
	}

	// rule 1.3
	if box.Box().tabularContainer && len(children) >= 2 {
		// Last child
		internal, text := children[len(children)-2], children[len(children)-1]

		if internal.Box().internalTableOrCaption && isWhitespace(text, nil) {
			children = children[:len(children)-1]
		}
		// First child
		if len(children) >= 2 {
			text, internal = children[0], children[1]
			if internal.Box().internalTableOrCaption && isWhitespace(text, nil) {
				children = children[1:]
			}
		}
		// Children other than first and last that would be removed by
		// rule 1.3 are also removed by rule 1.4 below.
	}

	newChildren, maxIndex := make([]Box, 0, len(children)), len(children)-1
	for index, child := range children {
		// Ignore some whitespace: rule 1.4
		var prevChild, nextChild Box
		if index != 0 {
			prevChild = children[index-1]
		}
		if index != maxIndex {
			nextChild = children[index+1]
		}
		if !(prevChild != nil && prevChild.Box().internalTableOrCaption && nextChild != nil && nextChild.Box().internalTableOrCaption && isWhitespace(child, nil)) {
			newChildren = append(newChildren, child)
		}
	}
	children = newChildren

	var childrenIter boxIterator = newBoxIter(children)
	if TableT.IsInstance(box) {
		// Rule 2.1
		childrenIter = wrapImproper(box, childrenIter, TableRowT,
			func(child Box) bool { return child.Box().properTableChild })
	} else if TableRowGroupT.IsInstance(box) {
		// Rule 2.2
		childrenIter = wrapImproper(box, childrenIter, TableRowT, nil)
	}

	if TableRowT.IsInstance(box) {
		// Rule 2.3
		childrenIter = wrapImproper(box, childrenIter, TableCellT, nil)
	} else {
		// Rule 3.1
		childrenIter = wrapImproper(box, childrenIter, TableRowT, func(child Box) bool {
			return !TableCellT.IsInstance(child)
		})
	}
	// Rule 3.2
	if InlineT.IsInstance(box) {
		childrenIter = wrapImproper(box, childrenIter, InlineTableT,
			func(child Box) bool {
				return !child.Box().properTableChild
			})
	} else {
		parentType := box.Type()
		childrenIter = wrapImproper(box, childrenIter, TableT,
			func(child Box) bool {
				return !child.Box().properTableChild || parentType.IsInProperParents(child.Type())
			})
	}

	if tableBox, ok := box.(TableBoxITF); ok {
		return wrapTable(tableBox, childrenIter)
	}
	box.Box().Children = collectBoxes(childrenIter)
	return box
}

// Take a table box and return it in its table wrapper box.
// Also re-order children and assign grid positions to each column and cell.
// Because of colspan/rowspan works, gridY is implicitly the index of a row,
// but gridX is an explicit attribute on cells, columns and column group.
// https://www.w3.org/TR/CSS21/tables.html#model
// https://www.w3.org/TR/CSS21/tables.html#table-layout
//
// wrapTable will panic if box's children are not table boxes
func wrapTable(box TableBoxITF, children boxIterator) Box {
	// Group table children by type
	var columns, rows, allCaptions []Box
	byType := map[BoxType]*[]Box{
		TableColumnT:      &columns,
		TableColumnGroupT: &columns,
		TableRowT:         &rows,
		TableRowGroupT:    &rows,
		TableCaptionT:     &allCaptions,
	}

	for children.Next() {
		child := children.Box()
		*byType[child.Type()] = append(*byType[child.Type()], child)
	}

	// Split top and bottom captions
	var captionTop, captionBottom []Box
	for _, caption := range allCaptions {
		switch caption.Box().Style.GetCaptionSide() {
		case "top":
			captionTop = append(captionTop, caption)
		case "bottom":
			captionBottom = append(captionBottom, caption)
		}
	}
	// Assign X positions on the grid to column boxes
	columnGroups := collectTableColumnGroupBoxs(wrapImproper(box, newBoxIter(columns), TableColumnGroupT, nil))
	gridX := 0
	for _, group := range columnGroups {
		group.GridX = gridX
		if len(group.Children) > 0 {
			for _, column := range group.Children {
				// There's no need to take care of group's span, as "span=x"
				// already generates x TableColumnBox children
				column.Box().GridX = gridX
				gridX += 1
			}
		} else {
			gridX += group.span()
		}
	}
	gridWidth := gridX

	rowGroups := collectBoxes(wrapImproper(box, newBoxIter(rows), TableRowGroupT, nil))
	// Extract the optional header and footer groups.
	var (
		bodyRowGroups []Box
		header        Box
		footer        Box
	)
	for _, _group := range rowGroups {
		group := _group.Box()
		display := group.Style.GetDisplay()
		if display == (pr.Display{Outside: pr.TableHeaderGroup}) && header == nil {
			group.IsHeader = true
			header = _group
		} else if display == (pr.Display{Outside: pr.TableFooterGroup}) && footer == nil {
			group.IsFooter = true
			footer = _group
		} else {
			bodyRowGroups = append(bodyRowGroups, _group)
		}
	}

	rowGroups = nil
	if header != nil {
		rowGroups = []Box{header}
	}
	rowGroups = append(rowGroups, bodyRowGroups...)
	if footer != nil {
		rowGroups = append(rowGroups, footer)
	}

	// Assign a (x,y) position in the grid to each cell.
	// rowspan can not extend beyond a row group, so each row group
	// is independent.
	// https://www.w3.org/TR/CSS21/tables.html#table-layout
	// Column 0 is on the left if direction is ltr, right if rtl.
	// This algorithm does not change.
	gridHeight := 0
	for _, group := range rowGroups {
		// Indexes: row number in the group.
		// Values: set of cells already occupied by row-spanning cells.
		groupChildren := group.Box().Children
		occupiedCellsByRow := make([]map[int]bool, len(groupChildren))
		// init the maps
		for i := range occupiedCellsByRow {
			occupiedCellsByRow[i] = make(map[int]bool)
		}
		for _, row := range groupChildren {
			occupiedCellsInThisRow := occupiedCellsByRow[0]
			occupiedCellsByRow = occupiedCellsByRow[1:]
			// The list is now about rows after this one.
			gridX = 0
			for _, _cell := range row.Box().Children {
				cell := _cell.Box()
				// Make sure that the first grid cell is free.
				for occupiedCellsInThisRow[gridX] {
					gridX += 1
				}
				cell.GridX = gridX
				newGridX := gridX + cell.Colspan
				// https://www.w3.org/TR/html401/struct/tables.html#adef-rowspan
				if cell.Rowspan != 1 {
					maxRowspan := len(occupiedCellsByRow) + 1
					var spannedRows []map[int]bool
					if cell.Rowspan == 0 {
						// All rows until the end of the group
						spannedRows = occupiedCellsByRow
						cell.Rowspan = maxRowspan
					} else {
						cell.Rowspan = min(cell.Rowspan, maxRowspan)
						spannedRows = occupiedCellsByRow[:cell.Rowspan-1]
					}
					for _, occupiedCells := range spannedRows {
						for i := gridX; i < newGridX; i++ {
							occupiedCells[i] = true
						}
					}
				}
				gridX = newGridX
				gridWidth = max(gridWidth, gridX)
			}
		}
		gridHeight += len(groupChildren)
	}
	table := CopyWithChildren(box, rowGroups).(TableBoxITF)
	table.Box().Style = table.Box().Style.Copy()
	tableBox := table.Table()
	tableBox.ColumnGroups = columnGroups
	if tableBox.Style.GetBorderCollapse() == "collapse" {
		tableBox.CollapsedBorderGrid = collapseTableBorders(table, gridWidth, gridHeight)
	}
	var wrapperAFT func(Box, []Box) Box
	if InlineTableT.IsInstance(box) {
		wrapperAFT = InlineBlockT.AnonymousFrom
	} else {
		wrapperAFT = BlockT.AnonymousFrom
	}
	wrapper := wrapperAFT(box, append(append(captionTop, table), captionBottom...))
	wrapperBox := wrapper.Box()
	wrapperBox.Style = wrapperBox.Style.Copy()
	wrapperBox.IsTableWrapper = true
	// Non-inherited properties of the table element apply to one
	// of the wrapper and the table. The other get the initial value.
	wbStyle, tbStyle := wrapperBox.Style, table.Box().Style
	for name := range pr.TableWrapperBoxProperties {
		key := name.Key()
		wbStyle.Set(key, tbStyle.Get(key))
		tbStyle.Set(key, pr.InitialValues[name])
	}

	return wrapper
}

// Turn an inline box into a block box.
// isLayoutFlex : flex or grid
func blockify(box Box, isLayoutFlex bool) Box {
	// See https://drafts.csswg.org/css-display-4/#blockify.
	var anonymous Box
	if InlineBlockT.IsInstance(box) {
		anonymous = BlockBoxAnonymousFrom(box, box.Box().Children)
	} else if replaced, ok := box.(InlineReplacedBoxITF); ok {
		replacement := replaced.Replaced().Replacement
		anonymous = BlockReplacedBoxAnonymousFrom(box, replacement)
	} else if InlineT.IsInstance(box) && !box.Box().IsRunning() {
		// gopdfgo: pudełko liniowe będące elementem flex lub grid samo staje się blokiem (CSS Display 3
		// § 2.7): jego dzieci przechodzą do pudełka blokowego o tym samym stylu. Źródło opakowywało je
		// w blok o tym samym stylu, więc odstępy, obramowanie i tło były stosowane dwukrotnie.
		anonymous = BlockBoxAnonymousFrom(box, box.Box().Children)
	} else if InlineLevelT.IsInstance(box) {
		anonymous = BlockBoxAnonymousFrom(box, []Box{box})
		if isLayoutFlex {
			box.Box().IsFlexItem = false
		} else {
			box.Box().IsGridItem = false
		}
	} else {
		return box
	}
	anonymous.Box().Style = box.Box().Style
	if isLayoutFlex {
		anonymous.Box().IsFlexItem = true
	} else {
		anonymous.Box().IsGridItem = true
	}
	return anonymous
}

type Score [3]float64

func (s Score) Lower(other Score) bool {
	return s[0] < other[0] || (s[0] == other[0] && (s[1] < other[1] || (s[1] == other[1] && s[2] < other[2])))
}

type Border struct {
	Style pr.String
	Score Score
	Width utils.Fl
	Color pr.Color
}

type BorderGrids struct {
	Vertical, Horizontal [][]Border
}

// Resolve border conflicts for a table in the collapsing border model.
//
// Set appropriate border widths on the table,
// column group, column, row group, row and cell boxes; and return
// a data structure for the resolved collapsed border grid.
func collapseTableBorders(table TableBoxITF, gridWidth, gridHeight int) BorderGrids {
	if gridWidth == 0 || gridHeight == 0 {
		// Don’t bother with empty tables
		return BorderGrids{}
	}

	weakNullBorder := Border{Score: Score{0, 0, styleScores["none"]}, Style: "none", Width: 0, Color: transparent}

	// Borders are always stored left to right, top to bottom.
	// verticalBorders has size gridHeigth x gridWidth+1
	// horizontalBorders has size gridHeight+1 x gridWidth
	verticalBorders, horizontalBorders := make([][]Border, gridHeight), make([][]Border, gridHeight+1)
	for y := range horizontalBorders {
		l1, l2 := make([]Border, gridWidth+1), make([]Border, gridWidth)
		for x := range l2 {
			l1[x] = weakNullBorder
			l2[x] = weakNullBorder
		}
		l1[gridWidth] = weakNullBorder
		if y < gridHeight {
			verticalBorders[y] = l1
		}
		horizontalBorders[y] = l2
	}

	// side follows the [bottom, left, right, top] convention
	const (
		bottom pr.KnownProp = iota
		left
		right
		top
	)
	setOneBorder := func(borderGrid [][]Border, boxStyle pr.ElementStyle, side pr.KnownProp, gridX, gridY int) {
		style := boxStyle.Get((pr.PBorderBottomStyle + side*5).Key()).(pr.String)
		width := boxStyle.Get((pr.PBorderBottomWidth + side*5).Key()).(pr.TaggedDim)
		color := tree.ResolveColor(boxStyle, pr.PBorderBottomColor+side*5)

		// https://www.w3.org/TR/CSS21/tables.html#border-conflict-resolution
		score := Score{0, float64(width.Value), styleScores[style]}
		if style == "hidden" {
			score[0] = 1
		}

		_style, in := styleMap[style]
		if in {
			style = _style
		}

		previousScore := borderGrid[gridY][gridX].Score
		// Strict < so that the earlier call wins in case of a tie.
		if previousScore.Lower(score) {
			borderGrid[gridY][gridX] = Border{Score: score, Style: style, Width: utils.Fl(width.Value), Color: color}
		}
	}

	direction := table.Box().Style.GetDirection()

	setBorders := func(box Box, x, y, w, h int) {
		style := box.Box().Style

		// x and y are logical (possibly rtl), but borders are graphical (always ltr).
		if direction == pr.Ltr {
			for yy := y; yy < y+h; yy++ {
				setOneBorder(verticalBorders, style, left, x, yy)
				setOneBorder(verticalBorders, style, right, x+w, yy)
			}
			for xx := x; xx < x+w; xx++ {
				setOneBorder(horizontalBorders, style, top, xx, y)
				setOneBorder(horizontalBorders, style, bottom, xx, y+h)
			}
		} else {
			for yy := y; yy < y+h; yy++ {
				setOneBorder(verticalBorders, style, left, (gridWidth+1)-1-w-x, yy)
				setOneBorder(verticalBorders, style, right, (gridWidth+1)-1-x, yy)
			}
			for xx := -1 - x; xx > -1-x-w; xx-- {
				setOneBorder(horizontalBorders, style, top, (gridWidth)+xx, y)
				setOneBorder(horizontalBorders, style, bottom, (gridWidth)+xx, y+h)
			}
		}
	}

	// Set cell borders. The order is important here:
	// "A style set on a cell wins over one on a row, which wins over a
	//  row group, column, column group and, lastly, table"
	// See https://www.w3.org/TR/CSS21/tables.html#border-conflict-resolution
	strongNullBorder := Border{Score: Score{1, 0, styleScores["hidden"]}, Style: "hidden", Width: 0, Color: transparent}

	gridY := 0
	for _, rowGroup := range table.Box().Children {
		for _, row := range rowGroup.Box().Children {
			for _, _cell := range row.Box().Children {
				cell := _cell.Box()
				// Force null border inside of a cell with rowspan or colspan.
				gridX, colspan, rowspan := cell.GridX, cell.Colspan, cell.Rowspan
				var verticalXRange, horizontalXRange [3]int
				if direction == pr.Ltr {
					verticalXRange = [3]int{gridX + 1, gridX + colspan, 1}
					horizontalXRange = [3]int{gridX, gridX + colspan, 1}
				} else {
					verticalXRange = [3]int{(gridWidth + 1) - 2 - gridX, (gridWidth + 1) - 1 - gridX - colspan, -1}
					horizontalXRange = [3]int{(gridWidth) - 1 - gridX, (gridWidth) - 1 - gridX - colspan, -1}
				}
				for xx := verticalXRange[0]; xx != verticalXRange[1]; xx += verticalXRange[2] {
					for yy := gridY; yy < gridY+rowspan; yy++ {
						verticalBorders[yy][xx] = strongNullBorder
					}
				}
				for xx := horizontalXRange[0]; xx != horizontalXRange[1]; xx += horizontalXRange[2] {
					for yy := gridY + 1; yy < gridY+rowspan; yy++ {
						horizontalBorders[yy][xx] = strongNullBorder
					}
				}
				// Set cell border.
				setBorders(_cell, gridX, gridY, colspan, rowspan)
			}
			gridY += 1
		}
	}

	// Set row borders.
	gridY = 0
	for _, rowGroup := range table.Box().Children {
		for _, row := range rowGroup.Box().Children {
			setBorders(row, 0, gridY, gridWidth, 1)
			gridY += 1
		}
	}

	// Set row group borders.
	gridY = 0
	for _, rowGroup := range table.Box().Children {
		rowspan := len(rowGroup.Box().Children)
		setBorders(rowGroup, 0, gridY, gridWidth, rowspan)
		gridY += rowspan
	}

	// Set column borders.
	for _, columnGroup := range table.Table().ColumnGroups {
		for _, column := range columnGroup.Children {
			setBorders(column, column.Box().GridX, 0, 1, gridHeight)
		}
	}

	// Set column group group borders.
	for _, columnGroup := range table.Table().ColumnGroups {
		setBorders(columnGroup, columnGroup.GridX, 0, columnGroup.span(), gridHeight)
	}

	// Set table borders.
	setBorders(table, 0, 0, gridWidth, gridHeight)

	// Now that all conflicts are resolved, set transparent borders of the
	// correct widths on each box. The actual border grid will be painted
	// separately.
	setBorderUsedWidth := func(box Box, side pr.KnownProp, twiceWidth utils.Fl) {
		box_, value := box.Box(), pr.Float(twiceWidth/2)
		switch side {
		case top:
			box_.BorderTopWidth = value
		case right:
			box_.BorderRightWidth = value
		case bottom:
			box_.BorderBottomWidth = value
		case left:
			box_.BorderLeftWidth = value
		}
	}

	removeBorders := func(box Box) {
		setBorderUsedWidth(box, top, 0)
		setBorderUsedWidth(box, right, 0)
		setBorderUsedWidth(box, bottom, 0)
		setBorderUsedWidth(box, left, 0)
	}

	maxVerticalWidth := func(x, y1, y2 int) utils.Fl {
		if x < 0 {
			x = gridWidth + 1 + x
		}
		var max utils.Fl
		for _, gridRow := range verticalBorders[y1:y2] {
			width := gridRow[x].Width
			if width > max {
				max = width
			}
		}
		return max
	}

	// x2 = -1 means "None"
	maxHorizontalWidth := func(x1, y, x2 int) utils.Fl {
		list := horizontalBorders[y]
		if x1 < 0 {
			x1 = len(list) + x1
		}
		if x2 < 0 {
			x2 = len(list) + x2
		}
		var max utils.Fl
		for _, _s := range list[x1:x2] {
			width := _s.Width
			if width > max {
				max = width
			}
		}
		return max
	}

	gridY = 0
	for _, rowGroup := range table.Box().Children {
		removeBorders(rowGroup)
		for _, row := range rowGroup.Box().Children {
			removeBorders(row)
			for _, _cell := range row.Box().Children {
				cell := _cell.Box()
				x, y := cell.GridX, gridY
				colspan, rowspan := cell.Colspan, cell.Rowspan
				var topW, bottomW, leftW, rightW pr.Fl
				if direction == pr.Ltr {
					topW = maxHorizontalWidth(x, y, x+colspan)
					bottomW = maxHorizontalWidth(x, y+rowspan, x+colspan)
					leftW = maxVerticalWidth(x, y, y+rowspan)
					rightW = maxVerticalWidth(x+colspan, y, y+rowspan)
				} else {
					y2 := -x // -x or None
					if y2 == 0 {
						y2 = gridWidth
					}
					topW = maxHorizontalWidth(-colspan-x, y, y2)
					bottomW = maxHorizontalWidth(-colspan-x, y+rowspan, y2)
					leftW = maxVerticalWidth(-1-colspan-x, y, y+rowspan)
					rightW = maxVerticalWidth(-1-x, y, y+rowspan)
				}
				setBorderUsedWidth(_cell, top, topW)
				setBorderUsedWidth(_cell, bottom, bottomW)
				setBorderUsedWidth(_cell, left, leftW)
				setBorderUsedWidth(_cell, right, rightW)
			}
			gridY += 1
		}
	}

	for _, columnGroup := range table.Table().ColumnGroups {
		removeBorders(columnGroup)
		for _, column := range columnGroup.Children {
			removeBorders(column)
		}
	}

	setBorderUsedWidth(table, top, maxHorizontalWidth(0, 0, gridWidth))
	setBorderUsedWidth(table, bottom, maxHorizontalWidth(0, gridHeight, gridWidth))
	// "UAs must compute an initial left && right border width for the table
	// by examining the first && last cells in the first row of the table."
	// https://www.w3.org/TR/CSS21/tables.html#collapsing-borders
	// ... so h=1, not gridHeight :
	setBorderUsedWidth(table, left, maxVerticalWidth(0, 0, 1))
	setBorderUsedWidth(table, right, maxVerticalWidth(gridWidth, 0, 1))

	return BorderGrids{Vertical: verticalBorders, Horizontal: horizontalBorders}
}

// Remove and add boxes according to the flex model.
// See https://www.w3.org/TR/css-flexbox-1/#flex-items
func FlexBoxes(box Box) Box {
	if !ParentT.IsInstance(box) || box.Box().IsRunning() {
		return box
	}

	// Do recursion.
	children := make([]Box, len(box.Box().Children))
	for i, child := range box.Box().Children {
		children[i] = FlexBoxes(child)
	}
	box.Box().Children = flexChildren(box, children)
	return box
}

func flexChildren(box Box, children []Box) []Box {
	if _, isFlexCont := box.(FlexContainerBoxITF); isFlexCont {
		var flexChildren []Box
		for _, child := range children {
			child.Box().forceNoFloated = true
			if child.Box().IsInNormalFlow() {
				child.Box().IsFlexItem = true
			}

			if textBox, ok := child.(*TextBox); ok {
				// https://www.w3.org/TR/css-flexbox-1/#flex-items
				// gopdfgo: pomijamy tekst złożony wyłącznie z białych znaków dokumentu (spacja, tabulator,
				// znak nowego wiersza), niezależnie od white-space (CSS Flexbox 1 § 4); źródło sprawdzało tylko
				// spacje, więc przy white-space: pre powstawały anonimowe elementy z tabulatorem i wierszami
				if strings.Trim(textBox.TextS(), " \t\n\r\f") == "" {
					continue
				}
			}

			flexChildren = append(flexChildren, blockify(child, true))
		}
		return flexChildren
	}
	return children
}

// Remove and add boxes according to the grid model.
// See https://drafts.csswg.org/css-grid-2/#grid-item
func GridBoxes(box Box) Box {
	if !ParentT.IsInstance(box) || box.Box().IsRunning() {
		return box
	}
	// Do recursion.
	children := make([]Box, len(box.Box().Children))
	for i, child := range box.Box().Children {
		children[i] = GridBoxes(child)
	}
	box.Box().Children = gridChildren(box, children)
	return box
}

func gridChildren(box Box, children []Box) []Box {
	if GridContainerT.IsInstance(box) {
		var gridChildren []Box
		for _, child := range children {
			if child.Box().IsInNormalFlow() {
				child.Box().IsGridItem = true
			}
			if text, ok := child.(*TextBox); ok && strings.Trim(text.TextS(), " \t\n\r\f") == "" {
				// gopdfgo: jak we flex: tekst z samych białych znaków dokumentu nie tworzy elementu siatki
				// (CSS Grid 1 § 6); źródło sprawdzało tylko spacje
				continue
			}

			gridChildren = append(gridChildren, blockify(child, false))
		}
		return gridChildren
	}
	return children
}

// ProcessWhitespace executes the first part of "The 'white-space' processing model".
// See https://www.w3.org/TR/CSS21/text.html#white-space-model
// and https://drafts.csswg.org/css-text-3/#white-space-rules
// The default value of followingCollapsibleSpace shoud be `false`.
func ProcessWhitespace(box Box, followingCollapsibleSpace bool) bool {
	state := whitespaceState{followingCollapsibleSpace: followingCollapsibleSpace}
	processWhitespace(box, &state)
	return state.followingCollapsibleSpace && !box.Box().IsRunning()
}

// whitespaceState (gopdfgo): stan przetwarzania białych znaków przenoszony między kolejnymi
// pudełkami treści liniowej.
type whitespaceState struct {
	followingCollapsibleSpace bool
	// afterZWSP: dotychczasowa treść kończy się spacją zerowej szerokości (U+200B)
	afterZWSP bool
}

func processWhitespace(box Box, state *whitespaceState) {
	if box_, isTextBox := box.(*TextBox); isTextBox {
		text := box_.TextS()
		if len(text) == 0 {
			return
		}

		// gopdfgo: źródło zamieniało CR i CRLF na znak nowego wiersza. Końce wierszy normalizuje już
		// parser HTML (i XML), więc CR, który tu dociera, pochodzi z odwołania znakowego (&#x0D;); taki
		// znak traktujemy dokładnie jak spację (CSS Text 3 § 4.1; test WPT control-chars-00D).
		text = strings.ReplaceAll(text, "\r", " ")
		// gopdfgo: znaki sterujące (kategoria Cc) inne niż tabulator i znak nowego wiersza mają być
		// widoczne (CSS Text 3 § 2.1); rysujemy w ich miejscu znak zastępczy U+FFFD. W źródle znikały.
		text = visibleControls(text)

		styleWhiteSpace := box_.Style.GetWhiteSpace()
		newLineCollapse := styleWhiteSpace == pr.Normal || styleWhiteSpace == pr.Nowrap
		spaceCollapse := styleWhiteSpace == pr.Normal || styleWhiteSpace == pr.Nowrap || styleWhiteSpace == pr.PreLine

		if spaceCollapse {
			// \r characters were removed/converted earlier
			text = tabRe.ReplaceAllString(text, "\n")
		}

		if newLineCollapse {
			// gopdfgo: złamanie segmentu tuż przed spacją zerowej szerokości (U+200B) albo tuż po niej
			// znika, także gdy spacja jest w poprzednim pudełku liniowym (CSS Text 3 § 4.1.3;
			// granice pudełek liniowych i elementy spoza przepływu się nie liczą, testy WPT
			// seg-break-transformation-018, -019). W źródle zamieniało się w spację.
			text = strings.ReplaceAll(text, "\u200b\n", "\u200b")
			text = strings.ReplaceAll(text, "\n\u200b", "\u200b")
			if state.afterZWSP && strings.HasPrefix(text, "\n") {
				text = text[1:]
			}
			// Could also replace with a zero width space character (U+200B),
			// or no character
			// CSS3: https://www.w3.org/TR/css3-text/#line-break-transform
			text = strings.ReplaceAll(text, "\n", " ")
		}
		// gopdfgo: word-space-transform (CSS Text 4, #word-space-transform) zamienia spację zerowej szerokości (także
		// z <wbr>) na spację albo spację ideograficzną między fazą 1 a 2 przetwarzania odstępów, więc
		// wstawione spacje zwijają się z sąsiednimi jak zwykłe. Źródło właściwości nie znało.
		if wst := string(box_.Style.GetWordSpaceTransform()); wst != "none" && strings.ContainsRune(text, '​') {
			replacement := " "
			if strings.HasPrefix(wst, "ideographic-space") {
				replacement = "　"
			}
			text = strings.ReplaceAll(text, "​", replacement)
		}
		if text != "" {
			state.afterZWSP = strings.HasSuffix(text, "\u200b")
		}
		if spaceCollapse {
			text = spaceRe.ReplaceAllString(text, " ")
			previousText := text
			if state.followingCollapsibleSpace && strings.HasPrefix(text, " ") {
				text = text[1:]
				box_.LeadingCollapsibleSpace = true
			}
			if previousText != "" {
				state.followingCollapsibleSpace = strings.HasSuffix(previousText, " ")
			}
		} else {
			state.followingCollapsibleSpace = false
		}
		box_.Text = []rune(text)
	} else {
		for _, child := range box.Box().Children {
			switch child.(type) {
			case *TextBox, *InlineBox: // leaf
				childState := *state
				processWhitespace(child, &childState)
				if box.Box().IsInNormalFlow() && child.Box().IsInNormalFlow() {
					childState.followingCollapsibleSpace = childState.followingCollapsibleSpace && !child.Box().IsRunning()
					*state = childState
				}
			default:
				if child.Box().IsInNormalFlow() {
					state.followingCollapsibleSpace = false
					state.afterZWSP = false
				}
			}
		}
	}
}

// hasVisibleText (gopdfgo) podaje, czy w poddrzewie pudełka jest tekst inny niż białe znaki.
func hasVisibleText(box Box) bool {
	if text, ok := box.(*TextBox); ok {
		return strings.TrimSpace(string(text.Text)) != ""
	}
	for _, child := range box.Box().Children {
		if hasVisibleText(child) {
			return true
		}
	}
	return false
}

// inheritInlineStacking (gopdfgo): blok wydzielony z pudełka liniowego, które tworzy kontekst
// nakładania (opacity mniejsze od 1 albo pozycjonowanie z z-index), należy do tego kontekstu
// (CSS 2.1 § 9.2.1.1: blok pozostaje potomkiem pudełka liniowego; testy WPT
// opacity-affects-block-in-inline, zindex-affects-block-in-inline). Rysowanie zna konteksty tylko
// na pudełkach, więc blok przejmuje krycie (mnożone przez własne) i z-index pudełka liniowego.
// Źródło gubiło je przy wydzielaniu bloku.
func inheritInlineStacking(block, inline Box) {
	inlineStyle := inline.Box().Style
	opacity := inlineStyle.GetOpacity()
	zIndex := inlineStyle.GetZIndex()
	positioned := inlineStyle.GetPosition().String != "static"
	stacksZ := positioned && zIndex.String != "auto"
	if opacity >= 1 && !stacksZ {
		return
	}
	style := block.Box().Style.Copy()
	if opacity < 1 {
		style.SetOpacity(style.GetOpacity() * opacity)
	}
	if stacksZ && style.GetZIndex().String == "auto" {
		if style.GetPosition().String == "static" {
			style.SetPosition(pr.BoolString{String: "relative"})
		}
		style.SetZIndex(zIndex)
	}
	block.Box().Style = style
}

// Capitalize words according to CSS’s "text-transform: capitalize".
func capitalize(text string) string {
	letterFound := false
	return capitalizeLang(text, language.Und, &letterFound)
}

// capitalizeLang (gopdfgo) zamienia pierwszą literę każdego słowa na wielką literę tytułową
// (titlecase, CSS Text 3 § 2.1.1), z odwzorowaniem pełnym (ligatura ﬁ -> Fi) i z regułami języka
// (niderlandzkie ij -> IJ). letterFound przechodzi między pudełkami tekstu, bo słowo może być
// podzielone na kilka elementów. W źródle: unicode.ToUpper na pojedynczym znaku, osobno w każdym
// pudełku.
func capitalizeLang(text string, lang language.Tag, letterFound *bool) string {
	title := cases.Title(lang, cases.NoLower)
	dutch := false
	if base, _ := lang.Base(); base.String() == "nl" {
		dutch = true
	}
	runes := []rune(text)
	var out strings.Builder
	for i := 0; i < len(runes); i++ {
		letter := runes[i]
		isLN := unicode.Is(unicode.L, letter) || unicode.Is(unicode.N, letter)
		if !*letterFound && isLN {
			*letterFound = true
			if dutch && (letter == 'i' || letter == 'I') && i+1 < len(runes) && (runes[i+1] == 'j' || runes[i+1] == 'J') {
				out.WriteString("IJ")
				i++
				continue
			}
			out.WriteString(title.String(string(letter)))
			continue
		} else if isWordSeparator(letter) {
			*letterFound = false
		}
		out.WriteRune(letter)
	}
	return out.String()
}

// isWordSeparator (gopdfgo) mówi, czy znak kończy słowo dla text-transform: capitalize. CSS Text 3
// § 2.1.1 zostawia granice słów przeglądarce, więc bierzemy regułę Chromium (pomiar Chrome 157,
// 2026-11-19; test WPT text-transform-capitalize-035): słowo kończy każdy znak niebędący literą, cyfrą
// ani znakiem łączącym, poza „_”, apostrofami ' i ’ oraz kropką środkową ·. Stąd „text-transform” daje
// „Text-Transform”. Źródło uznawało tylko separatory Unicode (Z); wcześniej dodaliśmy znaki białe
// (text-transform-cap-003).
func isWordSeparator(r rune) bool {
	switch r {
	case '_', '\'', '\u2019', '\u00b7':
		return false
	}
	return !unicode.In(r, unicode.L, unicode.N, unicode.M)
}

// textTransformLanguage zwraca język treści dla reguł zmiany wielkości liter (CSS Text 3 § 2.1:
// odwzorowanie zależy od języka elementu).
func textTransformLanguage(style pr.ElementStyle) language.Tag {
	if lang := style.GetLang().S; lang != "" {
		if tag, err := language.Parse(lang); err == nil {
			return tag
		}
	}
	return language.Und
}

func ProcessTextTransform(box Box) {
	// gopdfgo: capitalize zależy od tekstu przed pudełkiem, więc pudełka liniowe przetwarzamy dopiero
	// z najbliższym przodkiem blokowym, który przechodzi cały swój tekst po kolei
	_, inline := box.(*InlineBox)
	letterFound := false
	processTextTransform(box, !inline, &letterFound)
}

func processTextTransform(box Box, withCapitalize bool, letterFound *bool) {
	if tb, ok := box.(*TextBox); ok {
		text, style := tb.TextS(), tb.Style
		textTransform := tb.Style.GetTextTransform()
		if textTransform != "none" {
			lang := textTransformLanguage(style)
			switch textTransform {
			// gopdfgo: pełne odwzorowanie wielkości liter z Unicode (CSS Text 3 § 2.1), np. ß -> SS,
			// z regułami języka elementu (np. greckie wielkie litery bez tonosu); w źródle odwzorowanie
			// proste, znak po znaku.
			case "uppercase":
				if base, _ := lang.Base(); base.String() == "el" {
					text = greekUpper(text)
				} else {
					text = cases.Upper(lang).String(text)
				}
			case "lowercase":
				text = cases.Lower(lang).String(text)
			// Python’s unicode.captitalize is not the same.
			case "capitalize":
				if !withCapitalize {
					tb.Text = []rune(text)
					return // gopdfgo: zrobi to przodek blokowy
				}
				text = capitalizeLang(text, lang, letterFound)
			case "math-auto":
				// gopdfgo: pojedynczy znak dostaje odpowiednik z kursywy matematycznej (CSS Text 4
				// § 2.1.1, odwzorowanie z MathML Core, #italic-mappings); dłuższy tekst zostaje bez zmian
				if runes := []rune(text); len(runes) == 1 {
					text = string(mathItalic(runes[0]))
				}
			case "full-width":
				text = strings.Map(func(u rune) rune {
					rep, in := asciiToWide[u]
					if !in {
						return u
					}
					return rep
				}, text)
			}
		}
		if textTransform != "capitalize" && withCapitalize {
			// stan słowa dla następnego pudełka z capitalize
			for _, r := range text {
				if unicode.Is(unicode.L, r) || unicode.Is(unicode.N, r) {
					*letterFound = true
				} else if isWordSeparator(r) {
					*letterFound = false
				}
			}
		}
		if style.GetHyphens() == "none" {
			text = strings.ReplaceAll(text, "\u00AD", "") //  U+00AD SOFT HYPHEN (SHY)
		}
		tb.Text = []rune(text)
	} else if !box.Box().IsRunning() {
		// recursion
		for _, child := range box.Box().Children {
			if TextT.IsInstance(child) || InlineT.IsInstance(child) {
				processTextTransform(child, withCapitalize, letterFound)
			}
		}
	}
}

// Build the structure of lines inside blocks and return a new box tree.
//
// Consecutive inline-level boxes in a block container box are wrapped into a
// line box, itself wrapped into an anonymous block box.
//
// This line box will be broken into multiple lines later.
//
// This is the first case in
// https://www.w3.org/TR/CSS21/visuren.html#anonymous-block-level
//
// Example:
//
//	BlockBox[
//	    TextBox["Some "],
//	    InlineBox[TextBox["text"]],
//	    BlockBox[
//	        TextBox["More text"],
//	    ]
//	]
//
// is turned into::
//
//	BlockBox[
//	    AnonymousBlockBox[
//	        LineBox[
//	            TextBox["Some "],
//	            InlineBox[TextBox["text"]],
//	        ]
//	    ]
//	    BlockBox[
//	        LineBox[
//	            TextBox["More text"],
//	        ]
//	    ]
//	]
func InlineInBlock(box Box) Box {
	if len(box.Box().Children) == 0 || box.Box().IsRunning() {
		return box
	}
	baseBox := box.Box()
	boxChildren := baseBox.Children

	if len(boxChildren) > 0 && !baseBox.LeadingCollapsibleSpace {
		baseBox.LeadingCollapsibleSpace = boxChildren[0].Box().LeadingCollapsibleSpace
	}

	var children []Box
	trailingCollapsibleSpace := false
	for _, child := range boxChildren {
		// Keep track of removed collapsing spaces for wrap opportunities, and
		// remove empty text boxes.
		// (They may have been emptied by ProcessWhitespace().)

		if trailingCollapsibleSpace {
			child.Box().LeadingCollapsibleSpace = true
		}

		if textBox, isTextBox := child.(*TextBox); isTextBox && len(textBox.Text) == 0 {
			trailingCollapsibleSpace = child.Box().LeadingCollapsibleSpace
		} else {
			trailingCollapsibleSpace = false
			children = append(children, InlineInBlock(child))
		}
	}
	if !baseBox.TrailingCollapsibleSpace {
		baseBox.TrailingCollapsibleSpace = trailingCollapsibleSpace
	}

	if !BlockContainerT.IsInstance(box) {
		baseBox.Children = children
		return box
	}

	var newLineChildren, newChildren []Box
	for _, childBox := range children {
		if LineT.IsInstance(childBox) {
			panic("childBox can't be a LineBox")
		}
		if len(newLineChildren) > 0 && childBox.Box().IsAbsolutelyPositioned() {
			newLineChildren = append(newLineChildren, childBox)
		} else if InlineLevelT.IsInstance(childBox) || (len(newLineChildren) > 0 && !childBox.Box().IsInNormalFlow()) {
			// Do not append white space at the start of a line :
			// it would be removed during layout.
			childTextBox, isTextBox := childBox.(*TextBox)
			st := childBox.Box().Style.GetWhiteSpace()
			// Sequence of white-space was collapsed to a single space by ProcessWhitespace().
			if len(newLineChildren) > 0 || !(isTextBox && childTextBox.TextS() == " " && (st == pr.Normal || st == pr.Nowrap || st == pr.PreLine)) {
				newLineChildren = append(newLineChildren, childBox)
			}
		} else {
			if len(newLineChildren) > 0 {
				// Inlines are consecutive no more: add this line box
				// and create a new one.
				lineBox := LineBoxAnonymousFrom(box, newLineChildren)
				markAfterBlock(lineBox, newChildren)
				anonymous := BlockBoxAnonymousFrom(box, []Box{lineBox})
				newChildren = append(newChildren, anonymous)
				newLineChildren = nil
			}
			newChildren = append(newChildren, childBox)
		}
	}
	if len(newLineChildren) > 0 {
		// There were inlines at the end
		lineBox := LineBoxAnonymousFrom(box, newLineChildren)
		markAfterBlock(lineBox, newChildren)
		if len(newChildren) > 0 {
			anonymous := BlockBoxAnonymousFrom(box, []Box{lineBox})
			newChildren = append(newChildren, anonymous)
		} else {
			// Only inline-level children: one line box
			newChildren = append(newChildren, lineBox)
		}
	}

	baseBox.Children = newChildren
	return box
}

// Build the structure of blocks inside lines.
//
// Inline boxes containing block-level boxes will be broken in two
// boxes on each side on consecutive block-level boxes, each side wrapped
// in an anonymous block-level box.
//
// This is the second case in
// https://www.w3.org/TR/CSS21/visuren.html#anonymous-block-level
//
//	Eg. if this is given::
//
//	    BlockBox[
//	        LineBox[
//	            InlineBox[
//	                TextBox["Hello."],
//	            ],
//	            InlineBox[
//	                TextBox["Some "],
//	                InlineBox[
//	                    TextBox["text"]
//	                    BlockBox[LineBox[TextBox["More text"]]],
//	                    BlockBox[LineBox[TextBox["More text again"]]],
//	                ],
//	                BlockBox[LineBox[TextBox["And again."]]],
//	            ]
//	        ]
//	    ]
//
//	this is returned::
//
//	    BlockBox[
//	        AnonymousBlockBox[
//	            LineBox[
//	                InlineBox[
//	                    TextBox["Hello."],
//	                ],
//	                InlineBox[
//	                    TextBox["Some "],
//	                    InlineBox[TextBox["text"]],
//	                ]
//	            ]
//	        ],
//	        BlockBox[LineBox[TextBox["More text"]]],
//	        BlockBox[LineBox[TextBox["More text again"]]],
//	        AnonymousBlockBox[
//	            LineBox[
//	                InlineBox[
//	                ]
//	            ]
//	        ],
//	        BlockBox[LineBox[TextBox["And again."]]],
//	        AnonymousBlockBox[
//	            LineBox[
//	                InlineBox[
//	                ]
//	            ]
//	        ],
//	    ]
func BlockInInline(box Box) Box {
	if len(box.Box().Children) == 0 || box.Box().IsRunning() {
		return box
	}

	var newChildren []Box
	changed := false

	for _, child := range box.Box().Children {
		var newChild Box
		if LineT.IsInstance(child) {
			if len(box.Box().Children) != 1 {
				panic(fmt.Sprintf("Line boxes should have no siblings at this stage, got %v.", box.Box().Children))
			}

			var (
				stack          tree.ResumeStack
				newLine, block Box
			)
			for {
				newLine, block, stack = innerBlockInInline(child, stack)
				if block == nil {
					break
				}
				markAfterBlock(newLine, newChildren)
				anon := BlockBoxAnonymousFrom(box, []Box{newLine})
				newChildren = append(newChildren, anon)
				newChildren = append(newChildren, BlockInInline(block))
				// Loop with the same child and the new stack.
			}

			if len(newChildren) > 0 {
				// Some children were already added, this became a block
				// context.
				markAfterBlock(newLine, newChildren)
				newChild = BlockBoxAnonymousFrom(box, []Box{newLine})
			} else {
				// Keep the single line box as-is, without anonymous blocks.
				newChild = newLine
			}
		} else {
			// Not in an inline formatting context.
			newChild = BlockInInline(child)
		}

		if newChild != child {
			changed = true
		}
		newChildren = append(newChildren, newChild)
	}
	if changed {
		box.Box().Children = newChildren
	}
	return box
}

// markAfterBlock (gopdfgo) zaznacza pudełko wiersza, przed którym kontener blokowy ma już treść
// w normalnym przepływie: text-indent wcina tylko pierwszy sformatowany wiersz kontenera
// (CSS Text 3 § 7.1), a źródło wcinało pierwszy wiersz każdego anonimowego bloku.
func markAfterBlock(line Box, previous []Box) {
	lineBox, ok := line.(*LineBox)
	if !ok {
		return
	}
	for _, sibling := range previous {
		if sibling.Box().IsInNormalFlow() {
			lineBox.AfterBlock = true
			return
		}
	}
}

// Find a block-level box in an inline formatting context.
// If one is found, return “(newBox, blockLevelBox, resumeAt)“.
// “newBox“ contains all of “box“ content before the block-level box.
// “resumeAt“ can be passed as “skipStack“ in a new call to
// this function to resume the search just after the block-level box.
// If no block-level box is found after the position marked by
// “skipStack“, return “(newBox, None, None)“
func innerBlockInInline(box Box, skipStack tree.ResumeStack) (Box, Box, tree.ResumeStack) {
	var newChildren []Box
	var blockLevelBox Box
	var resumeAt tree.ResumeStack
	changed := false

	isStart := skipStack == nil
	var skip int
	if isStart {
		skip = 0
	} else {
		skip, skipStack = skipStack.Unpack()
	}

	hasBroken := false
	for i, child := range box.Box().Children[skip:] {
		index := i + skip
		if BlockLevelT.IsInstance(child) && child.Box().IsInNormalFlow() {
			if skipStack != nil {
				panic("Should not skip here")
			}
			blockLevelBox = child
			index += 1 // Resume *after* the block
		} else {
			var newChild Box
			if InlineT.IsInstance(child) {
				newChild, blockLevelBox, resumeAt = innerBlockInInline(child, skipStack)
				skipStack = nil
				// gopdfgo: blok wydzielony z pudełka liniowego przesuniętego względnie przesuwa się razem z nim
				// (CSS 2.1 § 9.2.1.1); w źródle przesunięcie gubiło się przy wydzielaniu (testy WPT
				// block-in-inline-relpos-001, -002).
				if blockLevelBox != nil && child.Box().Style.GetPosition().String == "relative" {
					blockLevelBox.Box().RelativeInlineAncestors = append(blockLevelBox.Box().RelativeInlineAncestors, child)
				}
				if blockLevelBox != nil {
					inheritInlineStacking(blockLevelBox, child)
				}
			} else {
				if skipStack != nil {
					panic("Should not skip here")
				}
				newChild = BlockInInline(child)
				// blockLevelBox is still None.
			}

			if newChild != child {
				changed = true
			}
			newChildren = append(newChildren, newChild)
		}

		if blockLevelBox != nil {
			resumeAt = tree.ResumeStack{index: resumeAt}
			box = CopyWithChildren(box, newChildren)
			// gopdfgo: fragmenty pudełka liniowego rozdzielonego przez blok pamiętają, z której strony
			// zostały ucięte; w źródle każdy fragment rysował pełne obramowanie i odstępy.
			if InlineT.IsInstance(box) {
				box.Box().SplitBefore = !isStart
				box.Box().SplitAfter = true
			}
			hasBroken = true
			break
		}
	}
	if !hasBroken {
		if changed || skip > 0 || !isStart {
			box = CopyWithChildren(box, newChildren)
			if InlineT.IsInstance(box) && !isStart {
				box.Box().SplitBefore = true
			}
		}
	}

	return box, blockLevelBox, resumeAt
}

// Set a “ViewportOverflow“ attribute on the box for the root element.
//
//	Like backgrounds, ``overflow`` on the root element must be propagated
//	to the viewport.
//
//	See https://www.w3.org/TR/CSS21/visufx.html#overflow
func setViewportOverflow(rootBox Box) Box {
	chosenBox := rootBox
	if strings.ToLower(rootBox.Box().ElementTag()) == "html" &&
		rootBox.Box().Style.GetOverflow() == "visible" {

		for _, child := range rootBox.Box().Children {
			if strings.ToLower(child.Box().ElementTag()) == "body" {
				chosenBox = child
				break
			}
		}
	}
	rootBox.Box().ViewportOverflow = string(chosenBox.Box().Style.GetOverflow())
	chosenBox.Box().Style.SetOverflow("visible")
	return rootBox
}

func boxText(box Box) string {
	if tBox, is := box.(*TextBox); is {
		return tBox.TextS()
	}
	var builder []rune
	if ParentT.IsInstance(box) {
		for _, child := range Descendants(box) {
			pt := child.Box().PseudoType
			if child, ok := child.(*TextBox); ok && pt != "before" && pt != "after" && pt != "marker" &&
				!strings.HasSuffix(pt, "::marker") { // gopdfgo: także znaczniki ::before::marker
				builder = append(builder, child.Text...)
			}
		}
	}
	return string(builder)
}

func extractText(textPart string, box Box) string {
	switch textPart {
	case "text", "content":
		return boxText(box)
	case "before", "after":
		var builder strings.Builder
		if ParentT.IsInstance(box) {
			for _, child := range Descendants(box) {
				et := child.Box().PseudoType
				if et == textPart && !ParentT.IsInstance(child) {
					builder.WriteString(boxText(child))
				}
			}
		}
		return builder.String()
	case "first-letter":
		characterFound := false
		firstLetter := ""
		for _, letter := range boxText(box) {
			isPunc := unicode.In(letter, TableFirstLetter...)
			if !isPunc {
				if characterFound {
					break
				}
				characterFound = true
			}
			firstLetter += string(letter)
		}
		return firstLetter
	default:
		panic("exhaustive switch")
	}
}

// visibleControls zamienia znaki sterujące, które nie mają znaczenia dla układu, na U+FFFD.
func visibleControls(text string) string {
	if strings.IndexFunc(text, isRenderedControl) < 0 {
		return text
	}
	var out strings.Builder
	for _, r := range text {
		switch {
		case r == '\v' || r == '\f' || r == '\u0085':
			// gopdfgo: znaki klas BK i NL z UAX 14 są widoczne i wymuszają podział wiersza (CSS Text 3
			// § 5.1); po znaku zastępczym wstawiamy separator wiersza U+2028 (klasa BK)
			out.WriteString("\uFFFD\u2028")
		case isRenderedControl(r):
			out.WriteRune('\uFFFD')
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

func isRenderedControl(r rune) bool {
	return unicode.Is(unicode.Cc, r) && r != '\t' && r != '\n'
}

// addOutsideImageMarkerStrut (gopdfgo): zewnętrzny znacznik z obrazem (list-style-image, url() w
// content ::marker) wpływa na wysokość pierwszego wiersza elementu listy, tak jakby stał w nim
// jako treść liniowa (CSS Lists 3 § 3.3 zostawia to implementacji; tak robi Chromium, test WPT
// outside-marker-taller-than-content). Na początek wiersza wstawiamy niewidoczny blok liniowy
// zerowej szerokości z kopią treści znacznika. Tylko gdy pozostałe dzieci są liniowe: przy dziecku
// blokowym wstawka utworzyłaby osobny wiersz. Znaczniki tekstowe zostają jak w źródle.
func addOutsideImageMarkerStrut(box Box) {
	children := box.Box().Children
	if len(children) == 0 {
		return
	}
	marker := children[0]
	var images []*InlineReplacedBox
	for _, child := range marker.Box().Children {
		if image, ok := child.(*InlineReplacedBox); ok {
			images = append(images, image)
		}
	}
	if len(images) == 0 {
		return
	}
	for _, child := range children[1:] {
		if !InlineLevelT.IsInstance(child) && !TextT.IsInstance(child) {
			return
		}
	}
	strut := InlineBlockBoxAnonymousFrom(box, nil)
	strut.Box().Style.SetWidth(pr.FToPx(0))
	strut.Box().Style.SetVisibility("hidden")
	strut.Box().Style.SetWhiteSpace(pr.Nowrap)
	for _, image := range images {
		strut.Box().Children = append(strut.Box().Children, InlineReplacedBoxAnonymousFrom(strut, image.Replacement))
	}
	box.Box().Children = append([]Box{marker, strut}, children[1:]...)
}
