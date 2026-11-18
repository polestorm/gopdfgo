// This module takes care of steps 3 and 4 of “CSS 2.1 processing model”:
// Retrieve stylesheets associated with a document and annotate every Element
// with a value for every CSS property.
//
// http://www.w3.org/TR/CSS21/intro.html#processing-model
//
// This module does this in more than two steps. The
// `getAllComputedStyles` function does everything, but it is itsef
// based on other functions in this module.
//
// :copyright: Copyright 2011-2014 Simon Sapin and contributors, see AUTHORS.
// :license: BSD, see LICENSE for details.
package tree

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode"
	"unsafe"

	"github.com/polestorm/gopdfgo/internal/webrender/css/counters"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/text"

	"golang.org/x/net/html/atom"

	"github.com/polestorm/gopdfgo/internal/webrender/css/selector"

	pa "github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/css/validation"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
	"golang.org/x/net/html"
)

// Reject anything not in here
var pseudoElements = utils.NewSet("", "before", "after", "marker", "before::marker", "after::marker", "first-line", "first-letter", "footnote-call", "footnote-marker")

type Token = pa.Token

// StyleFor provides a convenience function `Get` to get the computed styles for an Element.
type StyleFor struct {
	cascadedStyles map[utils.ElementKey]cascadedStyle
	computedStyles map[utils.ElementKey]pr.ElementStyle
	textContext    text.TextLayoutContext
	sheets         []sheet

	// gopdfgo: obszar pierwszej strony dla jednostek vw/vh; wspólny dla kopii StyleFor
	viewport *viewport

	// gopdfgo: stosunek rozmiaru pisma monospace do zwykłego (ADR 0008), 1 wyłącza regułę
	monospaceScale pr.Float

	// gopdfgo: kaskady pseudoelementów, z których styl obliczony powstaje
	// dopiero na żądanie (zob. Get), oraz to, czego do tego potrzeba
	pseudoCascades  map[*utils.HTMLNode][]sharedCascade
	share           *shareContext
	layers          map[string]int32 // miejsce warstwy kaskady według nazwy
	emptyCascade    cascadedStyle    // wspólna kaskada elementów bez żadnej deklaracji
	root            *utils.HTMLNode
	baseUrl         string
	targetCollector *TargetCollector
}

// viewport liczy leniwie obszar pierwszej strony (rozmiar strony bez marginesów), do którego odnoszą się
// jednostki vw, vh, vmin i vmax. Przybliżenie: bierzemy stronę domyślną (pierwszą, prawą, bez nazwy),
// także wtedy, gdy dokument zaczyna się stroną nazwaną.
type viewport struct {
	styleFor      *StyleFor
	html          *HTML
	width, height pr.Float
	done, running bool
}

func (v *viewport) size() (pr.Float, pr.Float) {
	if v == nil || v.running {
		// brak kontekstu albo wartość potrzebna do policzenia samej strony: przyjmujemy zero
		return 0, 0
	}
	if v.done {
		return v.width, v.height
	}
	rootKey := utils.ElementKey{Element: v.html.Root, PseudoType: ""}
	if v.styleFor.computedStyles[rootKey] == nil {
		return 0, 0
	}
	v.running = true
	defer func() { v.running = false }()

	// gopdfgo: jednostki vw i vh odnoszą się do obszaru pierwszej strony, także nazwanej
	// (test WPT css-page/page-size-009); wcześniej zawsze do strony bez nazwy
	var groups []utils.PageGroup
	name := v.firstPageName()
	if name != "" {
		groups = []utils.PageGroup{{Name: name, Index: 0}}
	}
	pageType := utils.NewPageElement("right", name, 0, false, groups)
	v.styleFor.SetPageComputedStylesT(pageType, v.html)
	style := v.styleFor.Get(pageType, "")
	if style == nil {
		return 0, 0
	}
	size := style.GetSize()
	// gopdfgo: obszar strony to strona bez marginesów, obramowania i odstępu wewnętrznego; procenty liczą się
	// od szerokości strony dla boków i od wysokości dla góry i dołu (CSS Page 3 § 5.3). Wcześniej odejmowane
	// były tylko marginesy podane długością.
	side := func(margin, padding pr.TaggedDim, border pr.Float, reference pr.Float) pr.Float {
		resolve := func(d pr.TaggedDim) pr.Float {
			switch {
			case d.Tag != 0:
				return 0
			case d.Unit == pr.Perc:
				return d.Value*reference/100 + d.Offset
			case d.Unit == pr.Px || d.Unit == pr.Scalar:
				return d.Value
			}
			return 0
		}
		return resolve(margin) + resolve(padding) + border
	}
	v.width = size[0].Value -
		side(style.GetMarginLeft(), style.GetPaddingLeft(), style.GetBorderLeftWidth().Value, size[0].Value) -
		side(style.GetMarginRight(), style.GetPaddingRight(), style.GetBorderRightWidth().Value, size[0].Value)
	v.height = size[1].Value -
		side(style.GetMarginTop(), style.GetPaddingTop(), style.GetBorderTopWidth().Value, size[1].Value) -
		side(style.GetMarginBottom(), style.GetPaddingBottom(), style.GetBorderBottomWidth().Value, size[1].Value)
	// gopdfgo: width i height w @page podają wprost rozmiar obszaru strony (CSS Page 3 § 5.3);
	// wcześniej vw i vh liczyły się wtedy od domyślnego rozmiaru arkusza
	if width := style.GetWidth(); width.Tag == 0 && width.Unit == pr.Px && width.Calc == nil {
		v.width = width.Value
	}
	if height := style.GetHeight(); height.Tag == 0 && height.Unit == pr.Px && height.Calc == nil {
		v.height = height.Value
	}
	v.done = true
	return v.width, v.height
}

// firstPageName (gopdfgo) przybliża nazwę pierwszej strony, czyli wartość początkową page (CSS Page 3
// § 6.1), ze stylów już obliczonych, przed budową pudełek. Idzie w dół po pierwszych dzieciach, pomijając
// komentarze, białe znaki i elementy z display: none; ostatnia wartość różna od auto wygrywa, a tekst
// albo element bez obliczonego jeszcze stylu (style liczą się w kolejności drzewa) kończy zejście.
func (v *viewport) firstPageName() string {
	name := ""
	node := (*html.Node)(v.html.Root)
	for node != nil {
		style := v.styleFor.computedStyles[utils.ElementKey{Element: (*utils.HTMLNode)(node), PseudoType: ""}]
		if style == nil {
			return name
		}
		if page := style.GetPage(); page != "auto" && page != "" {
			name = string(page)
		}
		var next *html.Node
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.TextNode {
				if strings.TrimSpace(child.Data) == "" {
					continue
				}
				return name
			}
			if child.Type != html.ElementNode {
				continue
			}
			childStyle := v.styleFor.computedStyles[utils.ElementKey{Element: (*utils.HTMLNode)(child), PseudoType: ""}]
			if childStyle == nil {
				return name
			}
			if childStyle.GetDisplay().Outside == pr.None {
				continue
			}
			next = child
			break
		}
		node = next
	}
	return name
}

// PageArea (gopdfgo) podaje rozmiar obszaru strony (bez marginesów, obramowania i odstępu) wynikający
// z reguł @page dla zwykłej strony; zero, gdy nie da się go ustalić.
func (sf *StyleFor) PageArea() (width, height pr.Float) { return sf.viewport.size() }

func newStyleFor(html *HTML, sheets []sheet, presentationalHints bool,
	targetColllector *TargetCollector, textContext text.TextLayoutContext,
) *StyleFor {
	out := StyleFor{
		cascadedStyles: map[utils.ElementKey]cascadedStyle{},
		computedStyles: map[utils.ElementKey]pr.ElementStyle{},
		sheets:         sheets,
		textContext:    textContext,
		viewport:       &viewport{html: html},
		monospaceScale: monospaceScaleOf(html),

		pseudoCascades:  map[*utils.HTMLNode][]sharedCascade{},
		share:           newShareContext(),
		layers:          layerRanks(sheets),
		emptyCascade:    cascadedStyle{},
		root:            html.Root,
		baseUrl:         html.BaseUrl,
		targetCollector: targetColllector,
	}

	out.viewport.styleFor = &out
	logger.ProgressLogger.Printf("Step 3 - Applying CSS - %d sheet(s)\n", len(sheets))
	if textContext != nil && textContext.Fonts() != nil {
		textContext.Fonts().SetFontFeatureValues(fontFeatureValues(sheets, out.layers))
	}

	for _, styleAttr := range findStyleAttributes(html.Root, presentationalHints, html.BaseUrl) {
		// Element, declarations, BaseUrl = attributes
		style, ok := out.cascadedStyles[styleAttr.element.ToKey("")]
		if !ok {
			style = cascadedStyle{}
			out.cascadedStyles[styleAttr.element.ToKey("")] = style
		}
		for _, decl := range validation.PreprocessDeclarations(styleAttr.baseUrl, styleAttr.declaration) {
			// name, values, importance = decl
			precedence := declarationPrecedence("author", decl.Important)
			we := weight{precedence: precedence, specificity: styleAttr.specificity, styleAttribute: styleAttr.specificity != selector.Specificity{}}
			oldWeight := style[decl.Name].weight
			if oldWeight.isNone() || oldWeight.Less(we) {
				style[decl.Name] = weigthedValue{weight: we, value: decl.Value, shortand: decl.Shortand}
			}
		}
	}

	// First, add declarations and set computed styles for "real" elements *in
	// tree order*. Tree order is important so that parents have computed
	// styles before their children, for inheritance.

	// Iterate on all elements, even if there is no cascaded style for them.
	// gopdfgo: elementy dopasowane przez ten sam ciąg reguł (wiersze i komórki
	// dużej tabeli) mają identyczną kaskadę, więc dzielą jedną mapę zamiast
	// budować własną. Mapa po zbudowaniu jest tylko czytana. Elementów z
	// atrybutem style albo atrybutami prezentacyjnymi to nie dotyczy: ich
	// kaskada zawiera deklaracje własne.
	sharedCascades := map[string][]sharedCascade{}
	var (
		signature []byte
		matches   [][]matchResult
	)
	iter := html.Root.Iter()
	for iter.HasNext() {
		element := iter.Next()
		_, hasOwnDeclarations := out.cascadedStyles[element.ToKey("")]
		signature, matches = signature[:0], matches[:0]
		for i, sh := range sheets {
			matched := sh.sheet.matcher.match(element.AsHtmlNode())
			matches = append(matches, matched)
			for _, sel := range matched {
				signature = sel.appendSignature(signature, i)
			}
		}
		if !hasOwnDeclarations {
			if shared, ok := sharedCascades[string(signature)]; ok {
				out.finishElement(element, shared)
				continue
			}
		}
		var built []sharedCascade
		for i, sh := range sheets {
			// sheet, origin, sheetSpecificity
			// Add declarations for matched elements
			matchedSelectors := matches[i]

			for _, sel := range matchedSelectors {
				// specificity, order, pseudoType, declarations = selector
				specificity := sel.specificity
				if len(sh.specificity) == 3 {
					specificity = selector.Specificity{sh.specificity[0], sh.specificity[1], sh.specificity[2]}
				}
				key := element.ToKey(sel.pseudoType)
				style, in := out.cascadedStyles[key]
				if !in {
					style = cascadedStyle{}
					out.cascadedStyles[key] = style
					built = append(built, sharedCascade{pseudoType: sel.pseudoType, style: style})
				}

				for _, decl := range sel.payload {
					// name, values, importance = decl
					precedence := declarationPrecedence(sh.origin, decl.Important)
					we := weight{precedence: precedence, specificity: specificity, layer: out.layers[sel.layer]}
					oldWeight := style[decl.Name].weight
					if oldWeight.isNone() || oldWeight.Less(we) {
						style[decl.Name] = weigthedValue{weight: we, value: decl.Value, shortand: decl.Shortand}
					}
				}
			}
		}
		if !hasOwnDeclarations {
			sharedCascades[string(signature)] = built
		}
		for _, sc := range built {
			delete(out.cascadedStyles, element.ToKey(sc.pseudoType))
		}
		out.finishElement(element, built)
	}

	// gopdfgo: style pseudoelementów nie są tu obliczane. W źródle powstawały
	// wszystkie, a reguły arkusza domyślnego bez selektora elementu (::marker,
	// ::footnote-call, ::footnote-marker) pasują do każdego elementu, więc
	// każdy niósł trzy style, z których układ prawie nigdy nie korzysta.
	// Kaskada czeka w pseudoCascades, a styl oblicza Get przy pierwszym pytaniu.

	// The cascaded styles of elements are not needed anymore. Keep the
	// dictionary, it is used later for page margins.
	return &out
}

// finishElement oblicza styl elementu z jego kaskady (wpis bez pseudoelementu
// w cascades albo deklaracje własne zebrane wcześniej w cascadedStyles)
// i odkłada kaskady pseudoelementów na później.
func (sf *StyleFor) finishElement(element *utils.HTMLNode, cascades []sharedCascade) {
	key := element.ToKey("")
	hasPseudo := false
	for _, sc := range cascades {
		if sc.pseudoType == "" {
			sf.cascadedStyles[key] = sc.style
		} else {
			hasPseudo = true
		}
	}
	sf.setComputedStyles(element, (*utils.HTMLNode)(element.Parent), sf.root, "", sf.baseUrl, sf.targetCollector)
	delete(sf.cascadedStyles, key)
	if hasPseudo {
		sf.pseudoCascades[element] = cascades
	}
}

// pseudoStyle oblicza na żądanie styl pseudoelementu, jeśli ma on kaskadę.
// The pseudo-Element inherits from the Element.
func (sf *StyleFor) pseudoStyle(element *utils.HTMLNode, pseudoType string) pr.ElementStyle {
	if originating, ok := strings.CutSuffix(pseudoType, "::marker"); ok {
		return sf.nestedMarkerStyle(element, originating, pseudoType)
	}
	for _, sc := range sf.pseudoCascades[element] {
		if sc.pseudoType != pseudoType {
			continue
		}
		key := element.ToKey(pseudoType)
		sf.cascadedStyles[key] = sc.style
		sf.setComputedStyles(element, element, sf.root, pseudoType, sf.baseUrl, sf.targetCollector)
		delete(sf.cascadedStyles, key)
		return sf.computedStyles[key]
	}
	return nil
}

// nestedMarkerStyle oblicza styl ::before::marker albo ::after::marker. Znacznik zagnieżdżony
// dziedziczy po swoim pseudoelemencie, a pasują do niego tylko selektory z podpseudoelementem
// (CSS Pseudo 4 § 4.2: ::marker samo nie wybiera znaczników ::before i ::after); bez reguł
// dostaje kaskadę pustą. gopdfgo: w źródle znacznik pseudoelementu brał styl ::marker elementu.
func (sf *StyleFor) nestedMarkerStyle(element *utils.HTMLNode, originating, pseudoType string) pr.ElementStyle {
	parentStyle := sf.Get(element, originating)
	if parentStyle == nil {
		return nil
	}
	cascaded := sf.emptyCascade
	for _, sc := range sf.pseudoCascades[element] {
		if sc.pseudoType == pseudoType {
			cascaded = sc.style
		}
	}
	rootStyle := sf.computedStyles[utils.ElementKey{Element: sf.root, PseudoType: ""}]
	style := computedFromCascaded(element, cascaded, parentStyle, rootStyle, pseudoType, sf.baseUrl,
		sf.targetCollector, sf.textContext, sf.share)
	if computed, ok := style.(*ComputedStyle); ok {
		computed.viewport = sf.viewport
		computed.monospaceScale = sf.monospaceScale
	}
	sf.computedStyles[element.ToKey(pseudoType)] = style
	return style
}

// layerRanks ustala kolejność warstw kaskady ze wszystkich arkuszy: warstwy
// w kolejności pierwszego ogłoszenia, a warstwa zagnieżdżona przed regułami
// stojącymi bezpośrednio w warstwie nadrzędnej. Zwraca wartości ujemne;
// brak wpisu (0) oznacza reguły poza warstwami.
func layerRanks(sheets []sheet) map[string]int32 {
	first := map[string]int{} // pierwsze wystąpienie każdej warstwy i każdego jej przodka
	var names []string
	for _, sh := range sheets {
		for _, m := range sh.sheet.matcher {
			if m.layer == "" {
				continue
			}
			if _, seen := first[m.layer]; !seen {
				names = append(names, m.layer)
			}
			for end := 0; end <= len(m.layer); end++ {
				if end == len(m.layer) || m.layer[end] == '.' {
					if _, seen := first[m.layer[:end]]; !seen {
						first[m.layer[:end]] = len(first)
					}
				}
			}
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.SliceStable(names, func(i, j int) bool {
		a, b := strings.Split(names[i], "."), strings.Split(names[j], ".")
		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				return first[strings.Join(a[:k+1], ".")] < first[strings.Join(b[:k+1], ".")]
			}
		}
		return len(a) > len(b) // warstwa zagnieżdżona przed własnymi regułami rodzica
	})
	ranks := make(map[string]int32, len(names))
	for i, name := range names {
		ranks[name] = int32(i - len(names))
	}
	return ranks
}

// sharedCascade to kaskada jednego (pseudo)elementu dzielona przez wszystkie
// elementy o tym samym ciągu dopasowanych reguł.
type sharedCascade struct {
	pseudoType string
	style      cascadedStyle
}

// appendSignature dopisuje do podpisu to, od czego zależy wkład dopasowania
// w kaskadę: arkusz, blok deklaracji, wagę selektora i pseudoelement.
func (m matchResult) appendSignature(signature []byte, sheet int) []byte {
	signature = binary.LittleEndian.AppendUint32(signature, uint32(sheet))
	signature = binary.LittleEndian.AppendUint64(signature, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(m.payload)))))
	signature = binary.LittleEndian.AppendUint32(signature, uint32(len(m.payload)))
	for _, part := range m.specificity {
		signature = binary.LittleEndian.AppendUint32(signature, uint32(part))
	}
	signature = append(signature, m.pseudoType...)
	return append(signature, 0)
}

// Set the computed values of styles to “Element“.
//
// Take the properties left by “applyStyleRule“ on an Element or
// pseudo-Element and assign computed values with respect to the cascade,
// declaration priority (ie. “!important“) and selector specificity.
func (sf *StyleFor) setComputedStyles(element, parent Element,
	root *utils.HTMLNode, pseudoType, baseUrl string,
	targetCollector *TargetCollector,
) {
	var (
		parentStyle pr.ElementStyle
		rootStyle_  pr.StyleAccessor
	)
	if element == root && pseudoType == "" {
		if node, ok := parent.(*utils.HTMLNode); parent != nil && (ok && node != nil) {
			panic("parent should be nil here")
		}
		rootStyle_ = pr.Properties{
			// When specified on the font-size property of the Root Element, the
			// rem units refer to the property’s initial value.
			pr.PFontSize: pr.InitialValues.GetFontSize(),
		}
	} else {
		if parent == nil {
			panic("parent shouldn't be nil here")
		}
		parentStyle = sf.computedStyles[parent.ToKey("")]
		rootStyle_ = sf.computedStyles[utils.ElementKey{Element: root, PseudoType: ""}]
	}
	key := element.ToKey(pseudoType)
	cascaded, in := sf.cascadedStyles[key]
	if !in {
		cascaded = sf.emptyCascade
	}
	style := computedFromCascaded(element, cascaded, parentStyle,
		rootStyle_, pseudoType, baseUrl, targetCollector, sf.textContext, sf.share)
	if computed, ok := style.(*ComputedStyle); ok {
		computed.viewport = sf.viewport
		computed.monospaceScale = sf.monospaceScale
	}
	sf.computedStyles[key] = style
}

// PseudoStyleWithParent (gopdfgo) oblicza styl pseudoelementu z jego kaskady, dziedziczący po
// podanym stylu zamiast po elemencie; zwraca nil, gdy pseudoelement nie ma kaskady. Służy
// ::first-letter, którego litera leży w ::before: pseudoelement dziedziczy wtedy po ::before
// (CSS Pseudo 4 § 2.5, „inherits from the innermost element containing the letter”).
func (s StyleFor) PseudoStyleWithParent(element *utils.HTMLNode, pseudoType string, parentStyle pr.ElementStyle) pr.ElementStyle {
	for _, sc := range s.pseudoCascades[element] {
		if sc.pseudoType != pseudoType {
			continue
		}
		rootStyle := s.computedStyles[utils.ElementKey{Element: s.root, PseudoType: ""}]
		style := computedFromCascaded(element, sc.style, parentStyle, rootStyle, pseudoType, s.baseUrl,
			s.targetCollector, s.textContext, s.share)
		if computed, ok := style.(*ComputedStyle); ok {
			computed.viewport = s.viewport
			computed.monospaceScale = s.monospaceScale
		}
		return style
	}
	return nil
}

func (s StyleFor) Get(element Element, pseudoType string) pr.ElementStyle {
	style := s.computedStyles[element.ToKey(pseudoType)]
	if style == nil && pseudoType != "" {
		if node, ok := element.(*utils.HTMLNode); ok && node != nil {
			style = s.pseudoStyle(node, pseudoType)
		}
	}
	if style != nil {
		display := style.GetDisplay()
		if display.Has(pr.Table) && style.GetBorderCollapse() == "collapse" {
			// Padding do not apply
			style.SetPaddingTop(pr.ZeroPixels.Tagged())
			style.SetPaddingBottom(pr.ZeroPixels.Tagged())
			style.SetPaddingLeft(pr.ZeroPixels.Tagged())
			style.SetPaddingRight(pr.ZeroPixels.Tagged())
		}

		if d := display.Outside; display.Inside == 0 && display.ListItem == 0 && d.HasTablePrefix() && d != pr.TableCaption {
			// Margins do not apply
			style.SetMarginTop(pr.ZeroPixels.Tagged())
			style.SetMarginBottom(pr.ZeroPixels.Tagged())
			style.SetMarginLeft(pr.ZeroPixels.Tagged())
			style.SetMarginRight(pr.ZeroPixels.Tagged())
		}
	}
	return style
}

func (s StyleFor) addPageDeclarations(page_T utils.PageElement) {
	// gopdfgo: najlepszy rozmiar arkusza spoza arkuszy autora, do którego odnoszą się size: auto,
	// portrait i landscape (pr.PageOrientation)
	sizeKey := pr.PSize.Key()
	var defaultSize weigthedValue
	defer func() {
		style, in := s.cascadedStyles[page_T.ToKey("")]
		if !in {
			return
		}
		orientation, ok := style[sizeKey].value.(pr.PageOrientation)
		if !ok {
			return
		}
		base := pr.A4
		if point, ok := defaultSize.value.(pr.Point); ok {
			base = point
		} else if other, ok := defaultSize.value.(pr.PageOrientation); ok {
			base = other.Orient(pr.A4)
		}
		resolved := style[sizeKey]
		resolved.value = orientation.Orient(base)
		style[sizeKey] = resolved
	}()
	for _, sh := range s.sheets {
		// Add declarations for page elements
		for _, pageR := range sh.sheet.pageRules {
			// Rule, selectorList, declarations
			for _, sel := range pageR.selectors {
				// specificity, pseudoType, selector_page_type = selector
				if pageTypeMatch(sel.pageType, page_T) {
					specificity := sel.specificity
					if len(sh.specificity) == 3 {
						specificity = selector.Specificity{sh.specificity[0], sh.specificity[1], sh.specificity[2]}
					}
					style, in := s.cascadedStyles[page_T.ToKey(sel.pseudoType)]
					if !in {
						style = cascadedStyle{}
						s.cascadedStyles[page_T.ToKey(sel.pseudoType)] = style
					}

					for _, decl := range pageR.declarations {
						// name, values, importance
						precedence := declarationPrecedence(sh.origin, decl.Important)
						we := weight{precedence: precedence, specificity: specificity, layer: s.layers[pageR.layer]}
						if decl.Name == sizeKey && sh.origin != "author" && sel.pseudoType == "" &&
							(defaultSize.weight.isNone() || defaultSize.weight.Less(we)) {
							defaultSize = weigthedValue{weight: we, value: decl.Value}
						}
						oldWeight := style[decl.Name].weight
						if oldWeight.isNone() || oldWeight.Less(we) {
							style[decl.Name] = weigthedValue{weight: we, value: decl.Value, shortand: decl.Shortand}
						}
					}
				}
			}
		}
	}
}

// pr.ElementStyle provides on demand access of computed properties
// for a box.

var (
	_ pr.ElementStyle = (*ComputedStyle)(nil)
	_ pr.ElementStyle = (*AnonymousStyle)(nil)
)

// gopdfgo: wartości obliczone trzymamy w dwóch warstwach. Wspólna tablica
// (sharedValues) należy do wszystkich stylów o tej samej kaskadzie i tak samo
// wyliczonym rodzicu; 28 tys. komórek tabeli ma zwykle kilka takich tablic,
// a w źródle każdy element miał własną, ok. 3 kB. Nakładka (own) trzyma to,
// co dotyczy tylko jednego stylu: wartości ustawione z zewnątrz (Set*) oraz
// obliczone z udziałem atrybutów elementu albo cudzej nakładki.
type ownValue struct {
	prop  pr.KnownProp
	value pr.CssProperty
}

type propsCache struct {
	own    []ownValue
	vars   map[string]pr.CssProperty
	shared *sharedValues // nil w stylach anonimowych
	share  *shareContext // nil w stylach anonimowych
}

// sharedValues to wartości wynikające wyłącznie z kaskady i z przodków.
type sharedValues struct {
	known     []pr.CssProperty
	variables map[string]pr.RawTokens
}

type sharedKey struct {
	cascade unsafe.Pointer // tożsamość mapy kaskady
	parent  *sharedValues
	pseudo  string
}

// shareContext jest wspólny dla stylów jednego dokumentu.
type shareContext struct {
	values map[sharedKey]*sharedValues
	// tainted: w trakcie obliczania wartości sięgnięto po coś, co nie wynika
	// z samej kaskady (atrybut elementu, czyjaś nakładka), więc wyniku nie
	// wolno zapisać we wspólnej tablicy
	tainted bool
}

func newShareContext() *shareContext {
	return &shareContext{values: map[sharedKey]*sharedValues{}}
}

func (c *propsCache) get(key pr.PropKey) (out pr.CssProperty, ok bool) {
	if k := key.KnownProp; k != 0 {
		for i := range c.own {
			if c.own[i].prop == k {
				if c.share != nil {
					c.share.tainted = true
				}
				return c.own[i].value, true
			}
		}
		if c.shared != nil && int(k) < len(c.shared.known) {
			out = c.shared.known[k]
			ok = out != nil
		}
	} else {
		out, ok = c.vars[key.Var]
		if ok && c.share != nil {
			c.share.tainted = true
		}
	}
	return
}

// setKnown zapisuje wartość w nakładce tego stylu.
func (c *propsCache) setKnown(k pr.KnownProp, value pr.CssProperty) {
	for i := range c.own {
		if c.own[i].prop == k {
			c.own[i].value = value
			return
		}
	}
	c.own = append(c.own, ownValue{prop: k, value: value})
}

func (c *propsCache) Set(key pr.PropKey, value pr.CssProperty) {
	if key.KnownProp != 0 {
		c.setKnown(key.KnownProp, value)
	} else {
		if c.vars == nil {
			c.vars = make(map[string]pr.CssProperty)
		}
		c.vars[key.Var] = value
	}
}

// setShared zapisuje wartość we wspólnej tablicy.
func (s *sharedValues) set(k_ pr.KnownProp, value pr.CssProperty) {
	k := int(k_)
	if L := len(s.known); k >= L {
		s.known = append(s.known, make([]pr.CssProperty, k+1-L)...)
	}
	s.known[k] = value
}

func (c *propsCache) updateWith(other propsCache) {
	for _, v := range other.own {
		c.setKnown(v.prop, v.value)
	}
	for k, v := range other.vars {
		c.Set(pr.PropKey{Var: k}, v)
	}
}

// ComputedStyle provides on demand access of computed properties
type ComputedStyle struct {
	propsCache

	textContext text.TextLayoutContext
	parentStyle pr.ElementStyle
	element     Element

	cache pr.TextRatioCache

	// gopdfgo: wspólne dla stylów o tej samej kaskadzie i rodzicu, tylko do odczytu
	variables map[string]pr.RawTokens

	// subset of properties of the root element
	rootStyle pr.StyleAccessor

	cascaded   cascadedStyle
	pseudoType string

	// inProgress: właściwości tego stylu w trakcie obliczania (zob. Get)
	inProgress []pr.KnownProp
	baseUrl    string
	specified  pr.SpecifiedAttributes

	viewport *viewport // gopdfgo: dla jednostek vw/vh

	monospaceScale  pr.Float // gopdfgo: zob. StyleFor.monospaceScale; 0 jak 1
	keywordFontSize int8     // gopdfgo: pamięć fontSizeFromKeyword: 0 nieznane, 1 tak, -1 nie
}

func newComputedStyle(parentStyle pr.ElementStyle, cascaded cascadedStyle,
	element Element, pseudoType string, rootStyle pr.StyleAccessor, baseUrl string, textContext text.TextLayoutContext,
	share *shareContext,
) *ComputedStyle {
	out := &ComputedStyle{
		propsCache: propsCache{share: share},

		textContext: textContext,
		parentStyle: parentStyle,
		cascaded:    cascaded,
		element:     element,
		pseudoType:  pseudoType,
		rootStyle:   rootStyle,
		baseUrl:     baseUrl,
	}

	// gopdfgo: wspólną tablicę wartości dostają style o tej samej kaskadzie,
	// tym samym pseudoelemencie i rodzicu z tą samą wspólną tablicą. Rodzic
	// innego rodzaju niż ComputedStyle (albo jego brak poza korzeniem) nie
	// daje takiej gwarancji, więc styl dostaje tablicę własną.
	key := sharedKey{cascade: reflect.ValueOf(cascaded).UnsafePointer(), pseudo: pseudoType}
	sharable := parentStyle == nil
	if parent, ok := parentStyle.(*ComputedStyle); ok && parent != nil && parent.share == share {
		key.parent = parent.shared
		sharable = true
	}
	if sharable {
		out.shared = share.values[key]
	}
	if out.shared == nil {
		out.shared = &sharedValues{variables: make(map[string]pr.RawTokens)}
		// inherit the variables
		if parentStyle != nil {
			for k, v := range parentStyle.Variables() {
				out.shared.variables[k] = v
			}
		}
		for k, v := range cascaded {
			if k.Var != "" {
				out.shared.variables[k.Var] = v.value.(pr.RawTokens)
			}
		}
		if sharable {
			share.values[key] = out.shared
		}
	}
	out.variables = out.shared.variables
	// inherit the cache
	if parentStyle != nil {
		out.cache = parentStyle.Cache()
	} else {
		out.cache = pr.NewTextRatioCache()
	}

	// Set specified value needed for computed value
	position, _ := out.cascadeValue(pr.PPosition.Key())
	display, _ := out.cascadeValue(pr.PDisplay.Key())
	float, _ := out.cascadeValue(pr.PFloat.Key())

	out.specified.Position, _ = position.(pr.BoolString)
	out.specified.Display, _ = display.(pr.Display)
	out.specified.Float, _ = float.(pr.String)

	return out
}

func (c *ComputedStyle) isRootElement() bool { return c.parentStyle == nil }

// snapsLengths (gopdfgo): czy długości zaokrąglane przez Chromium (szerokości obramowań, konturów i linii
// między kolumnami, border-spacing) są zaokrąglane do całych pikseli (ADR 0009, 0013). Styl liczony poza
// dokumentem (bez viewport) zostaje dokładny.
func (c *ComputedStyle) snapsLengths() bool {
	return c.viewport != nil && c.viewport.html != nil && !c.viewport.html.Behavior.ExactLengths
}

func (c *ComputedStyle) Copy() pr.ElementStyle {
	out := newComputedStyle(c.parentStyle, c.cascaded, c.element, c.pseudoType, c.rootStyle, c.baseUrl, c.textContext, c.share)
	out.viewport = c.viewport
	out.monospaceScale = c.monospaceScale
	out.propsCache.updateWith(c.propsCache)
	return out
}

func (c *ComputedStyle) ParentStyle() pr.ElementStyle       { return c.parentStyle }
func (c *ComputedStyle) Variables() map[string]pr.RawTokens { return c.variables }
func (c *ComputedStyle) Cache() pr.TextRatioCache           { return c.cache }
func (c *ComputedStyle) Specified() pr.SpecifiedAttributes  { return c.specified }

// the returned boolean is true if the value must be saved
func (c *ComputedStyle) cascadeValue(key pr.PropKey) (value pr.DeclaredValue, save bool) {
	var shortand pr.Shortand
	if casc, in := c.cascaded[key]; in { // Property defined in cascaded properties.
		value = casc.value
		shortand = casc.shortand
	} else {
		// Property not defined in cascaded properties, defined as inherited
		// or initial value.
		if pr.Inherited.Has(key.KnownProp) || key.Var != "" {
			value = pr.Inherit
		} else {
			value = pr.Initial
		}
	}

	if value == pr.Inherit && c.isRootElement() {
		// On the root element, "inherit" from initial values
		value = pr.Initial
	}

	parent_style := c.parentStyle
	if rawTokens, isPending := value.(pr.RawTokens); isPending { // Property with pending values, validate them.
		var solvedTokens []Token
		for _, token := range rawTokens {
			tokens := resolveVar(c.variables, token, utils.Set{})
			if tokens == nil {
				solvedTokens = append(solvedTokens, token)
			} else {
				solvedTokens = append(solvedTokens, tokens...)
			}
		}
		var err error
		if len(solvedTokens) == 0 {
			err = errors.New("no value")
		} else if shortand != 0 {
			// the tokens must be expanded (shortand are never variable)
			value, err = validation.ExpandValidatePending(key.KnownProp, shortand, solvedTokens)
		} else {
			value, err = validation.Validate(key, solvedTokens)
		}
		if err != nil {
			logger.WarningLogger.Printf("Ignored `%s: %s`, %s",
				key, pa.Serialize(solvedTokens), err)

			if pr.Inherited.Has(key.KnownProp) {
				// Values in parent_style are already computed.
				save = true
				value = parent_style.Get(key)
			} else {
				value = pr.InitialValues[key.KnownProp]
				if !pr.InitialNotComputed.Has(key.KnownProp) {
					// The value is the same as when computed.
					save = true
				}
			}
		}
	}

	if value == pr.Initial {
		value = pr.InitialValues[key.KnownProp]
		if !pr.InitialNotComputed.Has(key.KnownProp) {
			// The value is the same as when computed.
			save = true
		}
	} else if value == pr.Inherit {
		// Values in parent_style are already computed.
		value = c.parentStyle.Get(key)
		save = true
	}

	_ = value.(pr.CssProperty) // TODO: can we ensure this behavior ?

	return value, save
}

// provide on demand computation
func (c *ComputedStyle) Get(key pr.PropKey) pr.CssProperty {
	// check the cache
	if v, has := c.propsCache.get(key); has {
		return v
	}
	// gopdfgo: wartość, której obliczenie pyta o nią samą (np. tab-size: 0.4ch: jednostka ch wymaga
	// stylu tekstu, a ten czyta tab-size), dostaje na czas tego obliczenia wartość początkową.
	// W źródle kończyło się to nieskończoną rekurencją i przepełnieniem stosu, którego nie da się
	// przechwycić: ginął cały proces.
	if key.Var == "" {
		for _, busy := range c.inProgress {
			if busy == key.KnownProp {
				return pr.InitialValues[key.KnownProp]
			}
		}
		c.inProgress = append(c.inProgress, key.KnownProp)
		defer func() { c.inProgress = c.inProgress[:len(c.inProgress)-1] }()
	}
	if key.KnownProp == pr.PFontSize && key.Var == "" {
		// zależy od rodziny fontu i rozmiaru rodzica; wynik zostaje przy tym stylu
		out := c.monospaceFontSize(c.compute(key))
		c.propsCache.Set(key, out)
		return out
	}

	// gopdfgo: wynik trafia do wspólnej tablicy tylko wtedy, gdy w trakcie
	// obliczania nie sięgnięto po nic właściwego jednemu elementowi
	share := c.share
	outer := share.tainted
	share.tainted = false
	out := c.compute(key)
	tainted := share.tainted
	share.tainted = outer || tainted

	if tainted || key.KnownProp == 0 {
		c.propsCache.Set(key, out)
	} else {
		c.shared.set(key.KnownProp, out)
	}
	return out
}

// ownElement zwraca element stylu i odnotowuje, że obliczana wartość od
// niego zależy (zob. Get).
func (c *ComputedStyle) ownElement() Element {
	c.share.tainted = true
	return c.element
}

func (c *ComputedStyle) compute(key pr.PropKey) pr.CssProperty {
	if out, ok := c.inheritedBorderWidth(key); ok {
		return out
	}
	value, save := c.cascadeValue(key)

	if css, ok := value.(pr.CssProperty); ok && key.KnownProp.IsTextDecoration() && c.parentStyle != nil {
		// Text decorations are not inherited but propagated. See
		// https://www.w3.org/TR/css-text-decor-3/#line-decoration.
		_, isCascaded := c.cascaded[key]
		value = textDecoration(key.KnownProp, css, c.parentStyle.Get(key), isCascaded)
	} else if key.KnownProp == pr.PPage && value == pr.Page("auto") {
		// The page property does not inherit. However, if the page value on
		// an element is auto, then its used value is the value specified on
		// its nearest ancestor with a non-auto value. When specified on the
		// Root Element, the used value for auto is the empty string. See
		// https://www.w3.org/TR/css-page-3/#using-named-pages.
		value = pr.Page("")
		if c.parentStyle != nil {
			value = c.parentStyle.GetPage()
		}
	} else if ws, ok := value.(pr.Keyword); ok && key.KnownProp == pr.PWhiteSpace && key.Var == "" {
		// gopdfgo: text-wrap-mode nakłada się na white-space także wtedy, gdy white-space jest
		// odziedziczone, więc reszta kodu czyta jedną wartość
		return effectiveWhiteSpace(ws, c.GetTextWrapMode())
	} else if save {
		// Value already computed.
		return value.(pr.CssProperty)
	}

	out := value.(pr.CssProperty)
	if fn := computerFunctions[key.KnownProp]; fn != nil {
		// Value not computed yet: compute.
		out = fn(c, key.KnownProp, out)
	}
	return out
}

// borderWidthProps (gopdfgo) to szerokości obramowań i obrysów; ich właściwość stylu leży tuż przed nimi.
var borderWidthProps = map[pr.KnownProp]bool{
	pr.PBorderTopWidth: true, pr.PBorderRightWidth: true, pr.PBorderBottomWidth: true, pr.PBorderLeftWidth: true,
	pr.POutlineWidth: true, pr.PColumnRuleWidth: true,
}

// inheritedBorderWidth (gopdfgo) obsługuje jawne inherit szerokości obramowania. Wartość obliczona
// szerokości nie zależy od stylu obramowania; zero przy stylu none albo hidden to wartość używana
// (CSS Backgrounds 3 § 3.2 i § 3.3 w obecnym brzmieniu, jak w przeglądarkach; testy WPT
// border-width-011, outline-width-096). Rodzic bez obramowania przekazuje więc swoją szerokość.
// Pozostała część silnika czyta szerokość ze stylu jako wartość używaną, dlatego ta ze stylem none
// albo hidden zostaje w stylu zerem, a niezerowaną wartość odtwarza unzeroedBorderWidth.
// Źródło dziedziczyło zero.
func (c *ComputedStyle) inheritedBorderWidth(key pr.PropKey) (pr.CssProperty, bool) {
	if key.Var != "" || !borderWidthProps[key.KnownProp] {
		return nil, false
	}
	casc, in := c.cascaded[key]
	if !in || casc.value != pr.Inherit || c.isRootElement() {
		return nil, false
	}
	parent, ok := c.parentStyle.(*ComputedStyle)
	if !ok {
		return nil, false
	}
	if style := c.Get(pr.PropKey{KnownProp: key.KnownProp - 1}).(pr.String); style == "none" || style == "hidden" {
		return pr.FToV(0), true
	}
	return parent.unzeroedBorderWidth(key), true
}

// unzeroedBorderWidth (gopdfgo) zwraca wartość obliczoną szerokości obramowania bez zerowania przy
// stylu none albo hidden (zob. inheritedBorderWidth).
func (c *ComputedStyle) unzeroedBorderWidth(key pr.PropKey) pr.CssProperty {
	casc, in := c.cascaded[key]
	if !in || casc.value == pr.Initial {
		return borderWidthValue(c, pr.InitialValues[key.KnownProp].(pr.TaggedDim))
	}
	if casc.value == pr.Inherit && !c.isRootElement() {
		if parent, ok := c.parentStyle.(*ComputedStyle); ok {
			return parent.unzeroedBorderWidth(key)
		}
	}
	if value, ok := casc.value.(pr.TaggedDim); ok {
		return borderWidthValue(c, value)
	}
	return c.Get(key)
}

// effectiveWhiteSpace (gopdfgo) składa white-space z text-wrap-mode (CSS Text 4 § 3 i § 5.1) w jedno
// słowo white-space, z którego korzysta układ: nowrap odbiera zawijanie (normal → nowrap, pre-wrap
// i break-spaces → pre), wrap je przywraca (nowrap → normal, pre → pre-wrap). Deklaracja white-space
// ustawia obie właściwości, więc bez osobnego text-wrap-mode wynik jest wartością white-space.
// Dla pre-line nie ma słowa bez zawijania: zostaje pre-line.
func effectiveWhiteSpace(ws pr.Keyword, mode pr.String) pr.Keyword {
	if mode == "nowrap" {
		switch ws {
		case pr.Normal:
			return pr.Nowrap
		case pr.PreWrap, pr.BreakSpaces:
			return pr.Pre
		}
		return ws
	}
	switch ws {
	case pr.Nowrap:
		return pr.Normal
	case pr.Pre:
		return pr.PreWrap
	}
	return ws
}

// Rozmiar pisma monospace (gopdfgo, ADR 0008). W przeglądarkach font-size: medium znaczy 13 px, gdy
// font-family to samo słowo monospace, i 16 px w pozostałych przypadkach; dotyczy to każdego rozmiaru
// wywiedzionego ze słowa kluczowego, także odziedziczonego albo podanego względem rodzica (em, %).
// Żadna norma tego nie opisuje, ale <pre> i <code> mają przez to w każdej przeglądarce pismo 13 px,
// a dokumenty są pod to układane. Źródło dawało 16 px.

func monospaceScaleOf(html *HTML) pr.Float {
	size := html.Behavior.MonospaceFontSize
	if size <= 0 {
		size = 13
	}
	return pr.Float(size) / pr.InitialValues.GetFontSize().Value
}

func isMonospaceOnly(family pr.Strings) bool {
	return len(family) == 1 && utils.AsciiLower(family[0]) == "monospace"
}

// fontSizeFromKeyword mówi, czy rozmiar pisma elementu wywodzi się ze słowa kluczowego (medium, large…),
// a nie z podanej długości. Wartość początkowa to medium, więc dokument bez font-size spełnia warunek.
func (c *ComputedStyle) fontSizeFromKeyword() bool {
	if c.keywordFontSize != 0 {
		return c.keywordFontSize > 0
	}
	parent := func() bool {
		switch p := c.parentStyle.(type) {
		case nil:
			return true
		case *ComputedStyle:
			return p.fontSizeFromKeyword()
		case *AnonymousStyle:
			if grand, ok := p.parentStyle.(*ComputedStyle); ok {
				return grand.fontSizeFromKeyword()
			}
		}
		return false
	}
	result := false
	casc, has := c.cascaded[pr.PFontSize.Key()]
	switch value := casc.value.(type) {
	case nil:
		result = parent()
	case pr.TaggedDim:
		switch {
		case pr.XxSmall <= value.Tag && value.Tag <= pr.XxxLarge:
			result = true
		case value.Tag == pr.Larger || value.Tag == pr.Smaller,
			value.Calc == nil && (value.Unit == pr.Perc || value.Unit == pr.Em):
			result = parent()
		}
	default:
		if !has || casc.value == pr.Inherit {
			result = parent()
		} else if casc.value == pr.Initial {
			result = true
		}
	}
	c.keywordFontSize = -1
	if result {
		c.keywordFontSize = 1
	}
	return result
}

// monospaceFontSize przelicza obliczony rozmiar pisma przy przejściu między rodziną monospace
// a każdą inną.
func (c *ComputedStyle) monospaceFontSize(size pr.CssProperty) pr.CssProperty {
	scale := c.monospaceScale
	dim, ok := size.(pr.TaggedDim)
	if scale == 0 || scale == 1 || !ok || !c.fontSizeFromKeyword() {
		return size
	}
	own := isMonospaceOnly(c.GetFontFamily())
	inherited := false
	if c.parentStyle != nil {
		inherited = isMonospaceOnly(c.parentStyle.GetFontFamily())
	}
	if casc, has := c.cascaded[pr.PFontSize.Key()]; has {
		if value, isDim := casc.value.(pr.TaggedDim); (isDim && pr.XxSmall <= value.Tag && value.Tag <= pr.XxxLarge) || casc.value == pr.Initial {
			// słowo kluczowe podane wprost: rozmiar nie zależy od rodzica
			inherited = false
		}
	}
	switch {
	case own && !inherited:
		dim.Value *= scale
	case !own && inherited:
		dim.Value /= scale
	}
	return dim
}

// AnonymousStyle provides on demand access of computed properties,
// optimized for anonymous boxes
type AnonymousStyle struct {
	propsCache

	parentStyle pr.ElementStyle
	cache       pr.TextRatioCache
	variables   map[string]pr.RawTokens

	specified pr.SpecifiedAttributes
}

func newAnonymousStyle(parentStyle pr.ElementStyle) *AnonymousStyle {
	out := &AnonymousStyle{
		parentStyle: parentStyle,
	}
	// inherit the variables (gopdfgo: mapa rodzica jest tylko czytana, więc
	// wystarczy odwołanie zamiast kopii na każde pudełko anonimowe)
	if parentStyle != nil {
		out.variables = parentStyle.Variables()
	}
	// inherit the cache
	if parentStyle != nil {
		out.cache = parentStyle.Cache()
	} else {
		out.cache = pr.NewTextRatioCache()
	}

	out.specified.Display = out.GetDisplay()
	out.specified.Float = out.GetFloat()
	out.specified.Position = out.GetPosition()
	return out
}

func (c *AnonymousStyle) Copy() pr.ElementStyle {
	out := newAnonymousStyle(c.parentStyle)
	out.propsCache.updateWith(c.propsCache)
	return out
}

func (c *AnonymousStyle) ParentStyle() pr.ElementStyle       { return c.parentStyle }
func (c *AnonymousStyle) Variables() map[string]pr.RawTokens { return c.variables }
func (c *AnonymousStyle) Cache() pr.TextRatioCache           { return c.cache }
func (c *AnonymousStyle) Specified() pr.SpecifiedAttributes  { return c.specified }

func (a *AnonymousStyle) Get(key pr.PropKey) pr.CssProperty {
	// check the cache
	if v, has := a.propsCache.get(key); has {
		return v
	}

	// gopdfgo: wartości odziedziczonych i początkowych nie zapisujemy. Ich
	// wyznaczenie to jedno odwołanie, a tablica na wszystkie właściwości
	// zajmuje ok. 3 kB na każde pudełko anonimowe (samych pudełek liniowych
	// jest w dużej tabeli tyle, ile komórek). Tablica
	// powstaje dopiero przy jawnym ustawieniu wartości.
	switch key.KnownProp {
	case pr.PBorderTopWidth, pr.PBorderBottomWidth, pr.PBorderLeftWidth, pr.PBorderRightWidth, pr.POutlineWidth:
		// border-*-style is none, so border-width computes to zero.
		// Other than that, properties that would need computing are
		// border-*-color, but they do not apply.
		return pr.TaggedDim{}
	}
	if pr.Inherited.Has(key.KnownProp) || key.Var != "" {
		return a.parentStyle.Get(key)
	} else if key.KnownProp == pr.PPage {
		// page is not inherited but taken from the ancestor if 'auto'
		return a.parentStyle.Get(key)
	} else if key.KnownProp.IsTextDecoration() {
		return textDecoration(key.KnownProp, pr.InitialValues[key.KnownProp], a.parentStyle.Get(key), false)
	}
	return pr.InitialValues[key.KnownProp]
}

// ResolveColor return the color for `key`, replacing
// `currentColor` with p["color"]
// It panics if the key has not concrete type `Color`.
func ResolveColor(style pr.ElementStyle, key pr.KnownProp) pr.Color {
	// replace Python getColor function
	value := style.Get(key.Key()).(pr.Color)
	if value.Type == pa.ColorCurrentColor {
		return style.GetColor()
	}
	return value
}

func pageTypeMatch(pageSelectorType pageSelector, pageType utils.PageElement) bool {
	if pageSelectorType.Side != "" && pageSelectorType.Side != pageType.Side {
		return false
	}
	if pageSelectorType.Blank && pageSelectorType.Blank != pageType.Blank {
		return false
	}
	if pageSelectorType.First && pageSelectorType.First != pageType.First() {
		return false
	}
	if pageSelectorType.Name != "" && pageSelectorType.Name != pageType.Name {
		return false
	}
	if index := pageSelectorType.Index; !index.IsNone() {
		a, b, name := index.A, index.B, index.Group
		if name == "" {
			offset := pageType.Index + 1 - b
			if a == 0 {
				return offset == 0
			}
			return offset/a >= 0 && offset%a == 0
		}
		if name != pageType.Name {
			return false
		}
		for _, group := range pageType.Groups() {
			if name != group.Name {
				continue
			}
			offset := group.Index + 1 - b
			condition := offset == 0
			if a != 0 {
				condition = offset/a >= 0 && offset%a == 0
			}
			if condition {
				return true
			}
			return false
		}
	}
	return true
}

func textDecoration(key pr.KnownProp, value, parentValue pr.CssProperty, cascaded bool) pr.CssProperty {
	// The text-decoration-* properties are not inherited but propagated
	// using specific rules.
	// See https://drafts.csswg.org/css-text-decor-3/#line-decoration
	// TODO: these rules don’t follow the specification.
	switch key {
	case pr.PTextDecorationColor, pr.PTextDecorationStyle, pr.PTextDecorationThickness:
		if !cascaded {
			value = parentValue
		}
	case pr.PTextDecorationLine:
		pv := parentValue.(pr.Decorations)
		v := value.(pr.Decorations)
		value = v.Union(pv)
	}
	return value
}

// Yield the stylesheets in “elementTree“.
// The output order is the same as the source order.
func findStylesheets(wrapperElement *utils.HTMLNode, deviceMediaType string, urlFetcher utils.UrlFetcher, baseUrl string,
	fontConfig text.FontConfiguration, counterStyle counters.CounterStyle, pageRules *[]PageRule,
) (out []CSS) {
	sel := selector.MustCompile("style, link")
	for _, _element := range selector.MatchAll((*html.Node)(wrapperElement), sel) {
		element := (*utils.HTMLNode)(_element)
		mimeType := element.Get("type")
		if mimeType == "" {
			mimeType = "text/css"
		}
		mimeType = strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])
		// Only keep "type/subtype" from "type/subtype ; param1; param2".
		if mimeType != "text/css" {
			continue
		}
		mediaAttr := strings.TrimSpace(element.Get("media"))
		if mediaAttr == "" {
			mediaAttr = "all"
		}
		// gopdfgo: atrybut media to pełna lista zapytań, nie tylko typy medium
		media, sizeDependent := parseMediaQueryForSize(pa.Tokenize([]byte(mediaAttr), true))
		if !evaluateMediaQuery(media, deviceMediaType) {
			continue
		}
		// gopdfgo: size w @page arkusza z zapytaniem zależnym od rozmiaru medium jest pomijane (CSS Page 3 § 7.1)
		if sizeDependent {
			sizeMediaDepth++
		}
		switch element.DataAtom {
		case atom.Style:
			// Content is text that is directly in the <style> Element, not its
			// descendants
			content := element.GetChildrenText()
			// gopdfgo: dokumenty XHTML (np. z XSLT) opakowują arkusz w sekcję CDATA; parser HTML oddaje
			// jej znaczniki jako tekst i bez ich zdjęcia pierwsza reguła psuła cały arkusz.
			if trimmed := bytes.TrimSpace(content); bytes.HasPrefix(trimmed, []byte("<![CDATA[")) {
				content = bytes.TrimSuffix(bytes.TrimPrefix(trimmed, []byte("<![CDATA[")), []byte("]]>"))
			}
			// ElementTree should give us either unicode or  ASCII-only
			// bytestrings, so we don"t need `encoding` here.
			css, err := newCSS(utils.InputString(content), baseUrl, urlFetcher, false, deviceMediaType,
				fontConfig, nil, pageRules, counterStyle)
			if err != nil {
				logger.WarningLogger.Printf("Invalid style %s : %s \n", content, err)
			} else {
				out = append(out, css)
			}
		case atom.Link:
			if element.Get("href") != "" && element.HasLinkType("stylesheet") && !element.HasLinkType("alternate") {
				href := element.GetUrlAttribute("href", baseUrl, false)
				if href != "" {
					// gopdfgo: atrybut charset podaje kodowanie środowiska arkusza (CSS Syntax 3 § 3.2,
					// krok 3; HTML uznaje atrybut za przestarzały, ale Chromium go stosuje; testy WPT
					// at-charset-010, charset-attr-001). Źródło go pomijało.
					var input utils.ContentInput = utils.InputUrl(href)
					if label := strings.TrimSpace(element.Get("charset")); label != "" {
						input = utils.InputStylesheetUrl{Url: href, EnvironmentEncoding: label}
					}
					css, err := newCSS(input, "", urlFetcher, true, deviceMediaType,
						fontConfig, nil, pageRules, counterStyle)
					if err != nil {
						logger.WarningLogger.Printf("Failed to load stylesheet at %s : %s \n", href, err)
					} else {
						out = append(out, css)
					}
				}
			}
		}
		if sizeDependent {
			sizeMediaDepth--
		}
	}
	return out
}

// Return True if all characters in S are digits and there is at least one character in S, False otherwise.
func isDigit(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// Yield “specificity, (Element, declaration, BaseUrl)“ rules.
// Rules from "style" attribute are returned with specificity
// “(1, 0, 0)“.
// If “presentationalHints“ is “true“, rules from presentational hints
// are returned with specificity “(0, 0, 0)“.
// presentationalHints=false
func findStyleAttributes(tree *utils.HTMLNode, presentationalHints bool, baseUrl string) (out []styleAttrSpec) {
	checkStyleAttribute := func(element *utils.HTMLNode, styleAttribute string) styleAttr {
		declarations := pa.ParseBlocksContentsString(styleAttribute)
		return styleAttr{element: element, declaration: declarations, baseUrl: baseUrl}
	}

	iter := tree.Iter()
	for iter.HasNext() {
		element := iter.Next()
		specificity := selector.Specificity{1, 0, 0}
		styleAttribute := element.Get("style")
		if styleAttribute != "" {
			out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
		}
		if !presentationalHints {
			continue
		}
		specificity = selector.Specificity{0, 0, 0}
		switch element.DataAtom {
		case atom.Body:
			// TODO: we should check the container frame Element
			for _, pp := range [4][2]string{{"height", "top"}, {"height", "bottom"}, {"width", "left"}, {"width", "right"}} {
				part, position := pp[0], pp[1]
				styleAttribute = ""
				for _, prop := range [2]string{"margin" + part, position + "margin"} {
					s := element.Get(prop)
					if s != "" {
						styleAttribute = fmt.Sprintf("margin-%s:%spx", position, s)
						break
					}
				}
				if styleAttribute != "" {
					out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
				}
			}
			if element.Get("background") != "" {
				styleAttribute = fmt.Sprintf("background-image:url(%s)", element.Get("background"))
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
			}
			if element.Get("bgcolor") != "" {
				styleAttribute = fmt.Sprintf("background-color:%s", element.Get("bgcolor"))
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
			}
			if element.Get("text") != "" {
				styleAttribute = fmt.Sprintf("color:%s", element.Get("text"))
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
			}
		// TODO: we should support link, vlink, alink
		case atom.Center:
			out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, "text-align:center")})
		case atom.Div:
			align := strings.ToLower(element.Get("align"))
			switch align {
			case "middle":
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, "text-align:center")})
			case "center", "left", "right", "justify":
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, fmt.Sprintf("text-align:%s", align))})
			}
		case atom.Font:
			if element.Get("color") != "" {
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, fmt.Sprintf("color:%s", element.Get("color")))})
			}
			if element.Get("face") != "" {
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, fmt.Sprintf("font-family:%s", element.Get("face")))})
			}
			if element.Get("size") != "" {
				size := strings.TrimSpace(element.Get("size"))
				relativePlus := strings.HasPrefix(size, "+")
				relativeMinus := strings.HasPrefix(size, "-")
				if relativePlus || relativeMinus {
					size = strings.TrimSpace(size[1:])
				}
				sizeI, err := strconv.Atoi(size)
				if err != nil {
					logger.WarningLogger.Printf("Invalid value for size: %s \n", size)
				} else {
					fontSizes := map[int]string{
						1: "x-small",
						2: "small",
						3: "medium",
						4: "large",
						5: "x-large",
						6: "xx-large",
						7: "48px", // 1.5 * xx-large
					}
					if relativePlus {
						sizeI += 3
					} else if relativeMinus {
						sizeI -= 3
					}
					sizeI = max(1, min(7, sizeI))
					out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, fmt.Sprintf("font-size:%s", fontSizes[sizeI]))})
				}
			}
		case atom.Table:
			// TODO: we should support cellpadding
			if element.Get("cellspacing") != "" {
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, fmt.Sprintf("border-spacing:%spx", element.Get("cellspacing")))})
			}
			if element.Get("cellpadding") != "" {
				cellpadding := element.Get("cellpadding")
				if isDigit(cellpadding) {
					cellpadding += "px"
				}
				// TODO: don't match subtables cells
				iterElement := element.Iter()
				for iterElement.HasNext() {
					subelement := iterElement.Next()
					if subelement.DataAtom == atom.Td || subelement.DataAtom == atom.Th {
						out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(subelement,
							fmt.Sprintf("padding-left:%s;padding-right:%s;padding-top:%s;padding-bottom:%s;", cellpadding, cellpadding, cellpadding, cellpadding))})
					}
				}
			}
			if element.Get("hspace") != "" {
				hspace := element.Get("hspace")
				if isDigit(hspace) {
					hspace += "px"
				}
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element,
					fmt.Sprintf("margin-left:%s;margin-right:%s", hspace, hspace))})
			}
			if element.Get("vspace") != "" {
				vspace := element.Get("vspace")
				if isDigit(vspace) {
					vspace += "px"
				}
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element,
					fmt.Sprintf("margin-top:%s;margin-bottom:%s", vspace, vspace))})
			}
			if element.Get("width") != "" {
				styleAttribute = fmt.Sprintf("width:%s", element.Get("width"))
				if isDigit(element.Get("width")) {
					styleAttribute += "px"
				}
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
			}
			if element.Get("height") != "" {
				styleAttribute = fmt.Sprintf("height:%s", element.Get("height"))
				if isDigit(element.Get("height")) {
					styleAttribute += "px"
				}
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
			}
			if element.Get("background") != "" {
				styleAttribute = fmt.Sprintf("background-image:url(%s)", element.Get("background"))
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
			}
			if element.Get("bgcolor") != "" {
				styleAttribute = fmt.Sprintf("background-color:%s", element.Get("bgcolor"))
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
			}
			if element.Get("bordercolor") != "" {
				styleAttribute = fmt.Sprintf("border-color:%s", element.Get("bordercolor"))
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
			}
			if element.Get("border") != "" {
				styleAttribute = fmt.Sprintf("border-width:%spx", element.Get("border"))
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
			}
		case atom.Tr, atom.Td, atom.Th, atom.Thead, atom.Tbody, atom.Tfoot:
			align := strings.ToLower(element.Get("align"))
			if align == "left" || align == "right" || align == "justify" {
				// TODO: we should align descendants too
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, fmt.Sprintf("text-align:%s", align))})
			}
			if element.Get("background") != "" {
				styleAttribute = fmt.Sprintf("background-image:url(%s)", element.Get("background"))
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
			}
			if element.Get("bgcolor") != "" {
				styleAttribute = fmt.Sprintf("background-color:%s", element.Get("bgcolor"))
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
			}
			if element.DataAtom == atom.Tr || element.DataAtom == atom.Td || element.DataAtom == atom.Th {
				if element.Get("height") != "" {
					styleAttribute = fmt.Sprintf("height:%s", element.Get("height"))
					if isDigit(element.Get("height")) {
						styleAttribute += "px"
					}
					out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
				}
				if element.DataAtom == atom.Td || element.DataAtom == atom.Th {
					if element.Get("width") != "" {
						styleAttribute = fmt.Sprintf("width:%s", element.Get("width"))
						if isDigit(element.Get("width")) {
							styleAttribute += "px"
						}
						out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
					}
				}
			}
		case atom.Caption:
			align := strings.ToLower(element.Get("align"))
			// TODO: we should align descendants too
			if align == "left" || align == "right" || align == "justify" {
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, fmt.Sprintf("text-align:%s", align))})
			}
		case atom.Col:
			if element.Get("width") != "" {
				styleAttribute = fmt.Sprintf("width:%s", element.Get("width"))
				if isDigit(element.Get("width")) {
					styleAttribute += "px"
				}
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
			}
		case atom.Textarea:
			// gopdfgo: atrybuty cols i rows wyznaczają rozmiar pola (WHATWG HTML § 15.5.14); źródło je
			// pomijało i każde pole miało rozmiar z arkusza domyślnego. Szerokość znaku 0,55 em i wiersz
			// 1,36 em to przybliżenie zgodne z wydrukiem Chromium dla pisma bezszeryfowego.
			if cols, err := strconv.Atoi(strings.TrimSpace(element.Get("cols"))); err == nil && cols > 0 {
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, fmt.Sprintf("width:%.2fem", float64(cols)*0.55))})
			}
			if rows, err := strconv.Atoi(strings.TrimSpace(element.Get("rows"))); err == nil && rows > 0 {
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, fmt.Sprintf("height:%.2fem", float64(rows)*1.36))})
			}
		case atom.Hr:
			size := 0
			if element.Get("size") != "" {
				var err error
				size, err = strconv.Atoi(element.Get("size"))
				if err != nil {
					logger.WarningLogger.Printf("Invalid value for size: %s \n", element.Get("size"))
				}
			}
			if element.HasAttr("color") || element.HasAttr("noshade") {
				if size >= 1 {
					out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, fmt.Sprintf("border-width:%dpx", size/2))})
				}
			} else if size == 1 {
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, "border-bottom-width:0")})
			} else if size > 1 {
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, fmt.Sprintf("height:%dpx", size-2))})
			}

			if element.Get("width") != "" {
				styleAttribute = fmt.Sprintf("width:%s", element.Get("width"))
				if isDigit(element.Get("width")) {
					styleAttribute += "px"
				}
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
			}
			if element.Get("color") != "" {
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, fmt.Sprintf("color:%s", element.Get("color")))})
			}
		case atom.Iframe, atom.Applet, atom.Embed, atom.Img, atom.Input, atom.Object:
			if element.DataAtom != atom.Input || strings.ToLower(element.Get("type")) == "image" {
				align := strings.ToLower(element.Get("align"))
				if align == "middle" || align == "center" {
					// TODO: middle && center values are wrong
					out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, "vertical-align:middle")})
				}
				if element.Get("hspace") != "" {
					hspace := element.Get("hspace")
					if isDigit(hspace) {
						hspace += "px"
					}
					out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element,
						fmt.Sprintf("margin-left:%s;margin-right:%s", hspace, hspace))})
				}
				if element.Get("vspace") != "" {
					vspace := element.Get("vspace")
					if isDigit(vspace) {
						vspace += "px"
					}
					out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element,
						fmt.Sprintf("margin-top:%s;margin-bottom:%s", vspace, vspace))})
				}
				// TODO: img seems to be excluded for width && height, but a
				// lot of W3C tests rely on this attribute being applied to img
				if element.Get("width") != "" {
					styleAttribute = fmt.Sprintf("width:%s", element.Get("width"))
					if isDigit(element.Get("width")) {
						styleAttribute += "px"
					}
					out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
				}
				if element.Get("height") != "" {
					styleAttribute = fmt.Sprintf("height:%s", element.Get("height"))
					if isDigit(element.Get("height")) {
						styleAttribute += "px"
					}
					out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, styleAttribute)})
				}
				if element.DataAtom == atom.Img || element.DataAtom == atom.Object || element.DataAtom == atom.Input {
					if element.Get("border") != "" {
						out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element,
							fmt.Sprintf("border-width:%spx;border-style:solid", element.Get("border")))})
					}
				}
			}
		case atom.Svg:
			// gopdfgo: width i height elementu <svg> osadzonego w HTML są atrybutami prezentacyjnymi
			// (SVG 2 § 8.3), więc działają jak właściwości CSS: element z height="100" nie jest
			// rozciągany jak element o wysokości auto. W źródle wyznaczały tylko rozmiar wewnętrzny obrazu.
			for _, name := range [...]string{"width", "height"} {
				value := strings.TrimSpace(element.Get(name))
				if value == "" {
					continue
				}
				if _, err := strconv.ParseFloat(value, 64); err == nil {
					value += "px"
				}
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, name+":"+value)})
			}
		case atom.Ol:
			// gopdfgo: atrybuty list według HTML (§ 4.4.5, § 4.4.8) i CSS Lists 3. W źródle `start`
			// obniżał licznik przez counter-increment na samej liście, `reversed` nie był obsługiwany,
			// a `value` sprawdzany był na `ul` zamiast na `li`.
			start, hasStart := parseListInteger(element.Get("start"))
			if element.HasAttr("reversed") {
				// bez atrybutu start wartość początkową wyznacza zasięg licznika
				// (boxes.reversedCounterStart)
				declaration := "counter-reset:reversed(list-item)"
				if hasStart {
					declaration = fmt.Sprintf("counter-reset:reversed(list-item) %d", start+1)
				}
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element, declaration)})
			} else if hasStart {
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element,
					fmt.Sprintf("counter-reset:list-item %d", start-1))})
			}
		case atom.Li:
			// przyrost -1 w liście odwróconej wynika z samego licznika (boxes.updateCounters), nie z atrybutu:
			// gdy autor zastąpi counter-reset listy zwykłym licznikiem, elementy liczą w górę
			var declarations []string
			if value, ok := parseListInteger(element.Get("value")); ok {
				// licznik ustawiany po zwiększeniu, więc element dostaje dokładnie tę wartość
				declarations = append(declarations, fmt.Sprintf("counter-set:list-item %d", value))
			}
			if len(declarations) != 0 {
				out = append(out, styleAttrSpec{specificity: specificity, styleAttr: checkStyleAttribute(element,
					strings.Join(declarations, ";"))})
			}
		}
	}
	return out
}

// evaluateSupports rozstrzyga warunek reguły @supports (CSS Conditional 3 § 6.1): deklaracja w nawiasach jest
// obsługiwana, gdy przechodzi walidację silnika; selector(...) gdy selektor daje się sparsować; warunki łączą
// not, and i or. Wszystko, czego nie rozpoznajemy, jest fałszem.
func evaluateSupports(prelude []pa.Token, baseUrl string) bool {
	var tokens []pa.Token
	for _, token := range prelude {
		switch token.(type) {
		case pa.Whitespace, pa.Comment:
		default:
			tokens = append(tokens, token)
		}
	}
	keyword := func(token pa.Token) string {
		if ident, ok := token.(pa.Ident); ok {
			return utils.AsciiLower(string(ident.Value))
		}
		return ""
	}
	single := func(token pa.Token) bool {
		switch block := token.(type) {
		case pa.ParenthesesBlock:
			inner := block.Arguments
			for _, t := range inner {
				if lit, ok := t.(pa.Literal); ok && lit.Value == ":" {
					// deklaracja: sprawdzamy ją walidacją, bez zgłaszania ostrzeżeń
					previous := logger.WarningLogger.Writer()
					logger.WarningLogger.SetOutput(io.Discard)
					defer logger.WarningLogger.SetOutput(previous)
					return len(validation.PreprocessDeclarations(baseUrl, pa.ParseBlocksContents(inner, false))) != 0
				}
			}
			return evaluateSupports(inner, baseUrl)
		case pa.FunctionBlock:
			if utils.AsciiLower(block.Name) == "selector" {
				_, err := selector.ParseGroup(pa.Serialize(block.Arguments))
				return err == nil
			}
		}
		return false
	}
	if len(tokens) == 0 {
		return false
	}
	if keyword(tokens[0]) == "not" {
		return len(tokens) == 2 && !single(tokens[1])
	}
	result := single(tokens[0])
	operator := ""
	for i := 1; i+1 < len(tokens); i += 2 {
		op := keyword(tokens[i])
		if (op != "and" && op != "or") || (operator != "" && op != operator) {
			return false
		}
		operator = op
		next := single(tokens[i+1])
		if op == "and" {
			result = result && next
		} else {
			result = result || next
		}
	}
	return len(tokens)%2 == 1 && result
}

// Return the precedence for a declaration.
// Precedence values have no meaning unless compared to each other.
// Acceptable values for “origin“ are the strings “"author"“, “"user"“
// and “"user agent"“.
func declarationPrecedence(origin string, importance bool) uint8 {
	// See http://www.w3.org/TR/CSS21/cascade.html#cascading-order
	if origin == "user agent" {
		return 1
	} else if origin == "user" && !importance {
		return 2
	} else if origin == "author" && !importance {
		return 3
	} else if origin == "author" { // && importance
		return 4
	} else {
		if origin != "user" {
			logger.WarningLogger.Printf("origin should be 'user' got %s", origin)
		}
		return 5
	}
}

// Get a dict of computed style mixed from parent and cascaded styles.
func ComputedFromCascaded(element Element, cascaded cascadedStyle, parentStyle pr.ElementStyle, textContext text.TextLayoutContext,
) pr.ElementStyle {
	return computedFromCascaded(element, cascaded, parentStyle, pr.Properties{}, "", "", nil, textContext, newShareContext())
}

func computedFromCascaded(element Element, cascaded cascadedStyle, parentStyle pr.ElementStyle, rootStyle_ pr.StyleAccessor, pseudoType, baseUrl string,
	targetCollector *TargetCollector, textContext text.TextLayoutContext, share *shareContext,
) pr.ElementStyle {
	if cascaded == nil && parentStyle != nil {
		return newAnonymousStyle(parentStyle)
	}

	style := newComputedStyle(parentStyle, cascaded, element, pseudoType, rootStyle_, baseUrl, textContext, share)
	if anchor := string(style.GetAnchor()); targetCollector != nil && anchor != "" {
		targetCollector.collectAnchor(anchor)
	}
	return style
}

// either a html node or a page type
type Element interface {
	ToKey(pseudoType string) utils.ElementKey
}

type weight struct {
	precedence uint8
	// gopdfgo: deklaracja z atrybutu style wygrywa z każdym selektorem tego
	// samego pochodzenia i ważności (CSS 2.1 § 6.4.3: a=1). Źródło dawało
	// jej wagę (1,0,0), równą jednemu selektorowi identyfikatora, więc
	// reguła „#id { … }” ją przesłaniała.
	styleAttribute bool
	// gopdfgo: miejsce warstwy kaskady; 0 poza warstwami, wartości ujemne
	// dla warstw, tym mniejsze, im wcześniej warstwa została ogłoszona
	layer       int32
	specificity selector.Specificity
}

func (w weight) isNone() bool {
	return w == weight{}
}

// Less return `true` if w <= other
func (w weight) Less(other weight) bool {
	if w.precedence != other.precedence {
		return w.precedence < other.precedence
	}
	if w.styleAttribute != other.styleAttribute {
		return other.styleAttribute
	}
	if w.layer != other.layer {
		// Deklaracje zwykłe: późniejsza warstwa wygrywa, a reguły poza
		// warstwami wygrywają ze wszystkimi; dla !important odwrotnie
		// (CSS Cascade 5 § 6.4.3). Ważność 4 i 5 to deklaracje !important.
		if w.precedence >= 4 {
			return w.layer > other.layer
		}
		return w.layer < other.layer
	}
	return w.specificity.Less(other.specificity) || w.specificity == other.specificity
}

type weigthedValue struct {
	value    pr.DeclaredValue
	shortand pr.Shortand
	weight   weight
}

type cascadedStyle = map[pr.PropKey]weigthedValue

// Parse a page selector rule.
//
// Return a list of page data if the rule is correctly parsed.
// Return nil if something went wrong while parsing the rule.
func parsePageSelectors(rule pa.QualifiedRule) (out []pageSelector) {
	// See https://drafts.csswg.org/css-page-3/#syntax-page-selector

	tokens := pa.RemoveWhitespace(rule.Prelude)

	// TODO: Specificity is probably wrong, should clean and test that.
	if len(tokens) == 0 {
		out = append(out, pageSelector{})
		return out
	}

	for len(tokens) > 0 {
		var types_ pageSelector

		if ident, ok := tokens[0].(pa.Ident); ok {
			tokens = tokens[1:]
			types_.Name = string(ident.Value)
			types_.Specificity[0] = 1
		}

		if len(tokens) == 1 {
			return nil
		} else if len(tokens) == 0 {
			out = append(out, types_)
			return out
		}

		for len(tokens) > 0 {
			token_ := tokens[0]
			tokens = tokens[1:]
			literal, ok := token_.(pa.Literal)
			if !ok {
				return nil
			}

			if literal.Value == ":" {
				if len(tokens) == 0 {
					return nil
				}
				switch firstToken := tokens[0].(type) {
				case pa.Ident:
					tokens = tokens[1:]
					pseudoClass := utils.AsciiLower(firstToken.Value)
					switch pseudoClass {
					case "left", "right":
						if types_.Side != "" && types_.Side != pseudoClass {
							return nil
						}
						types_.Side = pseudoClass
						types_.Specificity[2] += 1
						continue
					case "blank":
						types_.Blank = true
						types_.Specificity[1] += 1
						continue
					case "first":
						types_.First = true
						types_.Specificity[1] += 1
						continue
					}
				case pa.FunctionBlock:
					tokens = tokens[1:]
					if firstToken.Name != "nth" {
						return nil
					}
					var group []pa.Token
					nth := firstToken.Arguments
					for i, argument := range firstToken.Arguments {
						if ident, ok := argument.(pa.Ident); ok && ident.Value == "of" {
							nth = (firstToken.Arguments)[:(i - 1)]
							group = (firstToken.Arguments)[i+1:]
						}
					}
					nthValues := pa.ParseNth(nth)
					if nthValues == nil {
						return nil
					}
					types_.Index = pageIndex{
						A: nthValues[0],
						B: nthValues[1],
					}
					if group != nil {
						var group_ []pa.Token
						for _, token := range group {
							if ty := token.Kind(); ty != pa.KComment && ty != pa.KWhitespace {
								group_ = append(group_, token)
							}
						}
						if len(group_) != 1 {
							return nil
						}
						groupIdent, ok := group_[0].(pa.Ident)
						if !ok {
							return nil
						}
						types_.Index.Group = groupIdent.Value
					}
					// TODO: specificity is not specified yet
					// https://github.com/w3c/csswg-drafts/issues/3524
					types_.Specificity[1] += 1
					if types_.Index.Group != "" {
						types_.Specificity[0] += 1
					}
					continue
				}
				return nil

			} else if literal.Value == "," {
				if len(tokens) > 0 && (types_.Specificity != selector.Specificity{}) {
					break
				} else {
					return nil
				}
			}
		}
		out = append(out, types_)
	}
	return out
}

// fontFeatureValues (gopdfgo) składa tablicę nazw ze wszystkich reguł
// @font-feature-values (CSS Fonts 4 § 6.9.1). Przy powtórzonej nazwie wygrywa
// reguła z późniejszej warstwy kaskady, a w tej samej warstwie późniejsza
// w dokumencie; reguły poza warstwami idą po wszystkich warstwach (CSS Cascade 5 § 6.4).
func fontFeatureValues(sheets []sheet, layers map[string]int32) validation.FontFeatureValues {
	type ranked struct {
		layer int32
		rule  *validation.FontFeatureValuesRule
	}
	var rules []ranked
	for _, sh := range sheets {
		for _, m := range sh.sheet.matcher {
			if m.featureValues != nil {
				rules = append(rules, ranked{layers[m.layer], m.featureValues})
			}
		}
	}
	if len(rules) == 0 {
		return nil
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].layer < rules[j].layer })
	out := validation.FontFeatureValues{}
	for _, r := range rules {
		out.Add(*r.rule)
	}
	return out
}

func _isContentNone(rule pa.Compound) bool {
	switch token := rule.(type) {
	case pa.QualifiedRule:
		return token.Content == nil
	case pa.AtRule:
		return token.Content == nil
	default:
		return true
	}
}

type selectorPageRule struct {
	pseudoType  string
	pageType    pageSelector
	specificity selector.Specificity
}

type PageRule struct {
	rule         pa.AtRule
	selectors    []selectorPageRule
	declarations []validation.Declaration
	layer        string // gopdfgo: jak match.layer
}

// Do the work that can be done early on stylesheet, before they are
// in a document.
// ignoreImports = false
func preprocessStylesheet(deviceMediaType, baseUrl string, stylesheetRules []pa.Compound,
	urlFetcher utils.UrlFetcher, matcher *matcher, pageRules *[]PageRule,
	fontConfig text.FontConfiguration, counterStyle counters.CounterStyle, ignoreImports bool,
) {
	preprocessLayer(deviceMediaType, baseUrl, stylesheetRules, urlFetcher, matcher, pageRules,
		fontConfig, counterStyle, ignoreImports, "")
}

// anonymousLayers numeruje warstwy bez nazwy; każda jest osobną warstwą.
var anonymousLayers atomic.Int64

// declareLayer odnotowuje pierwsze wystąpienie warstwy: o kolejności warstw
// decyduje kolejność ogłoszenia, nie kolejność reguł (CSS Cascade 5 § 6.4.3).
func declareLayer(matcher *matcher, pageRules *[]PageRule, name string) {
	*matcher = append(*matcher, match{layer: name})
	if pageRules != nil {
		*pageRules = append(*pageRules, PageRule{layer: name})
	}
}

// preprocessLayer (gopdfgo) to dawne preprocessStylesheet z nazwą warstwy
// kaskady, w której stoją przetwarzane reguły. Źródło pomijało @layer razem
// z całą zawartością.
func preprocessLayer(deviceMediaType, baseUrl string, stylesheetRules []pa.Compound,
	urlFetcher utils.UrlFetcher, matcher *matcher, pageRules *[]PageRule,
	fontConfig text.FontConfiguration, counterStyle counters.CounterStyle, ignoreImports bool, layer string,
) {
	for _, rule := range stylesheetRules {
		atRule, isAtRule := rule.(pa.AtRule)
		if _isContentNone(rule) {
			if err, isErr := rule.(pa.ParseError); isErr {
				logger.WarningLogger.Printf("Parse error at %d:%d: %s\n", err.Pos().Line, err.Pos().Column, err.Message)
			}
			if !isAtRule {
				// gopdfgo: także pusta reguła („foo{}”) jest regułą, po której
				// @import jest już nieważny (CSS 2.1 § 4.1.5)
				if _, isRule := rule.(pa.QualifiedRule); isRule {
					ignoreImports = true
				}
				continue
			}
			if keyword := utils.AsciiLower(atRule.AtKeyword); keyword == "layer" {
				// „@layer a, b;” ogłasza warstwy i ich kolejność
				for _, name := range strings.Split(pa.Serialize(atRule.Prelude), ",") {
					if name = strings.TrimSpace(name); name != "" {
						if layer != "" {
							name = layer + "." + name
						}
						declareLayer(matcher, pageRules, name)
					}
				}
				continue
			} else if keyword != "import" {
				logger.WarningLogger.Printf("Unknown empty rule %s at %d:%d\n", pa.Serialize(atRule.Content), atRule.Pos().Line, atRule.Pos().Column)
				continue
			}
		}

		switch rule := rule.(type) {
		case pa.QualifiedRule:
			allDeclarations, err := validation.PreprocessDeclarationsPrelude(baseUrl,
				pa.ParseBlocksContents(rule.Content, false), rule.Prelude)
			if err != nil {
				logger.WarningLogger.Printf("Invalid or unsupported selector '%s', %s \n", pa.Serialize(rule.Prelude), err)
				continue
			}

			if len(allDeclarations) > 0 {
				for _, item := range allDeclarations {
					for _, sel := range item.Selector {
						if _, in := pseudoElements[sel.PseudoElement()]; !in {
							err = fmt.Errorf("unsupported pseudo-element : %s", sel.PseudoElement())
							break
						}
					}
					if err != nil {
						logger.WarningLogger.Println(err)
						continue
					}
					*matcher = append(*matcher, match{selector: item.Selector, declarations: item.Declarations, layer: layer})
					ignoreImports = true
				}
			} else {
				ignoreImports = true
			}
		case pa.AtRule:
			switch utils.AsciiLower(rule.AtKeyword) {
			case "import":
				if ignoreImports {
					logger.WarningLogger.Printf("@import rule '%s' not at the beginning of the whole rule was ignored. \n",
						pa.Serialize(rule.Prelude))
					continue
				}

				tokens := pa.RemoveWhitespace(rule.Prelude)
				// gopdfgo: znacznik końca arkusza w niezamkniętym url( nie jest częścią zapytania o media
				// (CSS Syntax 3 § 4.3.6); w źródle unieważniał regułę
				if n := len(tokens); n > 0 && pa.IsEOFError(tokens[n-1]) {
					tokens = tokens[:n-1]
				}
				var url string
				if len(tokens) > 0 {
					switch str := tokens[0].(type) {
					case pa.URL:
						url = str.Value
					case pa.String:
						url = str.Value
					case pa.FunctionBlock:
						// gopdfgo: url("…") z adresem w cudzysłowie jest dla
						// analizatora funkcją, nie żetonem URL; źródło
						// obsługiwało tylko url(bez cudzysłowu) i "napis",
						// więc najczęstsza postać @import była pomijana
						if args := pa.RemoveWhitespace(str.Arguments); utils.AsciiLower(str.Name) == "url" && len(args) == 1 {
							if quoted, ok := args[0].(pa.String); ok {
								url = quoted.Value
							}
						}
					}
					if url == "" {
						logger.WarningLogger.Printf("Invalid @import rule '%s' was ignored. \n", pa.Serialize(rule.Prelude))
						continue
					}
				} else {
					continue
				}
				media, sizeDependent := parseMediaQueryForSize(tokens[1:])
				if media == nil {
					logger.WarningLogger.Printf("Invalid media type '%s' the whole @import rule was ignored. \n",
						pa.Serialize(rule.Prelude))
					continue
				}
				if !evaluateMediaQuery(media, deviceMediaType) {
					continue
				}
				url = utils.UrlJoin(baseUrl, url, false, "@import")
				if url != "" {
					// gopdfgo: size w @page wewnątrz @import z zapytaniem zależnym od rozmiaru medium jest
					// pomijane (CSS Page 3 § 7.1)
					if sizeDependent {
						sizeMediaDepth++
					}
					_, err := newCSS(utils.InputUrl(url), "", urlFetcher, false,
						deviceMediaType, fontConfig, matcher, pageRules, counterStyle)
					if sizeDependent {
						sizeMediaDepth--
					}
					if err != nil {
						logger.WarningLogger.Printf("Failed to load stylesheet at %s : %s \n", url, err)
					}
				}
			case "media":
				media, sizeDependent := parseMediaQueryForSize(rule.Prelude)
				if media == nil {
					logger.WarningLogger.Printf("Invalid media type '%s' the whole @media rule was ignored. \n",
						pa.Serialize(rule.Prelude))
					continue
				}
				ignoreImports = true
				if !evaluateMediaQuery(media, deviceMediaType) {
					continue
				}
				// gopdfgo: size w @page wewnątrz @media z cechą width, height, aspect-ratio albo
				// orientation jest pomijane (CSS Page 3 § 7.1), bo rozmiar strony nie może zależeć od
				// zapytania o rozmiar medium; źródło je stosowało
				if sizeDependent {
					sizeMediaDepth++
				}
				contentRules := pa.ParseRuleList(rule.Content, false, false)
				preprocessLayer(
					deviceMediaType, baseUrl, contentRules, urlFetcher,
					matcher, pageRules, fontConfig, counterStyle, true, layer)
				if sizeDependent {
					sizeMediaDepth--
				}
			case "supports":
				// gopdfgo: @supports (CSS Conditional 3 § 6); źródło pomijało całą regułę jako nieznaną
				ignoreImports = true
				if rule.Content == nil || !evaluateSupports(rule.Prelude, baseUrl) {
					continue
				}
				preprocessLayer(
					deviceMediaType, baseUrl, pa.ParseRuleList(rule.Content, false, false), urlFetcher,
					matcher, pageRules, fontConfig, counterStyle, true, layer)
			case "layer":
				name := strings.TrimSpace(pa.Serialize(rule.Prelude))
				if name == "" {
					name = fmt.Sprintf("\x00%d", anonymousLayers.Add(1))
				}
				if layer != "" {
					name = layer + "." + name
				}
				declareLayer(matcher, pageRules, name)
				ignoreImports = true
				preprocessLayer(
					deviceMediaType, baseUrl, pa.ParseRuleList(rule.Content, false, false), urlFetcher,
					matcher, pageRules, fontConfig, counterStyle, true, name)
			case "page":
				data := parsePageSelectors(rule.QualifiedRule)
				if data == nil {
					logger.WarningLogger.Printf("Unsupported @page selector '%s', the whole @page rule was ignored. \n",
						pa.Serialize(rule.Prelude))
					continue
				}
				ignoreImports = true
				for _, pageType := range data {
					specificity := pageType.Specificity
					pageType.Specificity = selector.Specificity{}
					content := pa.ParseBlocksContents(rule.Content, false)
					declarations := validation.PreprocessDeclarations(baseUrl, content)
					if sizeMediaDepth > 0 {
						declarations = withoutPageSize(declarations)
					}

					var selectors []selectorPageRule
					if len(declarations) > 0 {
						selectors = []selectorPageRule{{specificity: specificity, pseudoType: "", pageType: pageType}}
						*pageRules = append(*pageRules, PageRule{rule: rule, selectors: selectors, declarations: declarations, layer: layer})
					}

					for _, marginRule := range content {
						atRule, ok := marginRule.(pa.AtRule)
						if !ok || atRule.Content == nil {
							continue
						}
						declarations = validation.PreprocessDeclarations(
							baseUrl, pa.ParseBlocksContents(atRule.Content, false))
						if len(declarations) > 0 {
							selectors = []selectorPageRule{{
								specificity: specificity, pseudoType: "@" + utils.AsciiLower(atRule.AtKeyword),
								pageType: pageType,
							}}
							*pageRules = append(*pageRules, PageRule{rule: atRule, selectors: selectors, declarations: declarations, layer: layer})
						}
					}
				}
			case "font-face":
				ignoreImports = true
				content := pa.ParseBlocksContents(rule.Content, false)
				ruleDescriptors := validation.PreprocessFontFaceDescriptors(baseUrl, content)
				if ruleDescriptors.Src == nil {
					logger.WarningLogger.Printf(`Missing src descriptor in "@font-face" rule at %d:%d`+"\n",
						rule.Pos().Line, rule.Pos().Column)
					break
				}
				if ruleDescriptors.FontFamily == "" {
					logger.WarningLogger.Printf(`Missing font-family descriptor in "@font-face" rule at %d:%d`+"\n",
						rule.Pos().Line, rule.Pos().Column)
					break
				}
				if ruleDescriptors.Src != nil && ruleDescriptors.FontFamily != "" && fontConfig != nil {
					fontConfig.AddFontFace(ruleDescriptors, urlFetcher)
				}

			case "font-feature-values":
				// gopdfgo: CSS Fonts 4 § 6.9; źródło pomijało regułę jako nieznaną
				ignoreImports = true
				if rule.Content == nil {
					continue
				}
				featureValues, ok := validation.ParseFontFeatureValues(rule.Prelude, rule.Content)
				if !ok {
					logger.WarningLogger.Printf("Invalid @font-feature-values prelude '%s' at %d:%d\n",
						pa.Serialize(rule.Prelude), rule.Pos().Line, rule.Pos().Column)
					continue
				}
				*matcher = append(*matcher, match{layer: layer, featureValues: &featureValues})

			case "counter-style":
				name := validation.ParseCounterStyleName(rule.Prelude, counterStyle)
				if name == "" {
					logger.WarningLogger.Printf(`Invalid counter style name %s, the whole @counter-style rule was ignored at %d:%d.`,
						pa.Serialize(rule.Prelude), rule.Pos().Line, rule.Pos().Column)
					continue
				}

				ignoreImports = true
				content := pa.ParseBlocksContents(rule.Content, false)
				ruleDescriptors := validation.PreprocessCounterStyleDescriptors(baseUrl, content)

				if err := ruleDescriptors.Validate(); err != nil {
					logger.WarningLogger.Printf("In counter style %s at %d:%d, %s", name, rule.Pos().Line, rule.Pos().Column, err)
					continue
				}

				counterStyle[name] = ruleDescriptors
			default:
				logger.WarningLogger.Printf("Unknown rule %s at %d:%d\n", pa.Serialize(atRule.Content), atRule.Pos().Line, atRule.Pos().Column)
			}
		default:
			logger.WarningLogger.Printf("Unknown rule %s at %d:%d\n", pa.Serialize(atRule.Content), atRule.Pos().Line, atRule.Pos().Column)
		}
	}
}

type sheet struct {
	sheet       CSS
	origin      string
	specificity []int
}

type styleAttr struct {
	element     *utils.HTMLNode
	baseUrl     string
	declaration []pa.Compound
}

type styleAttrSpec struct {
	styleAttr
	specificity selector.Specificity
}

// Compute all the computed styles of all elements in `html` document.
// Do everything from finding author stylesheets to parsing and applying them.
//
// Return a `StyleFor` function like object that takes an Element and an optional
// pseudo-Element type, and return a style dict object.
// presentationalHints=false
func GetAllComputedStyles(html *HTML, userStylesheets []CSS,
	presentationalHints bool, fontConfig text.FontConfiguration,
	counterStyle counters.CounterStyle, pageRules *[]PageRule,
	targetCollector *TargetCollector, forms bool,
	textContext text.TextLayoutContext,
) *StyleFor {
	if counterStyle == nil {
		counterStyle = make(counters.CounterStyle)
	}
	// add the UA counters
	for k, v := range UACounterStyle {
		counterStyle[k] = v
	}

	// List stylesheets. Order here is not important ("origin" is).
	sheets := []sheet{
		{sheet: html.UAStyleSheet, origin: "user agent", specificity: nil},
	}
	if forms {
		sheets = append(sheets, sheet{sheet: html.FormStyleSheet, origin: "user agent", specificity: nil})
	}
	if presentationalHints {
		sheets = append(sheets, sheet{sheet: html.PHStyleSheet, origin: "author", specificity: []int{0, 0, 0}})
	}
	authorShts := findStylesheets(html.Root, html.mediaType, html.UrlFetcher,
		html.BaseUrl, fontConfig, counterStyle, pageRules)
	for _, sht := range authorShts {
		sheets = append(sheets, sheet{sheet: sht, origin: "author", specificity: nil})
	}
	for _, sht := range userStylesheets {
		sheets = append(sheets, sheet{sheet: sht, origin: "user", specificity: nil})
	}
	return newStyleFor(html, sheets, presentationalHints, targetCollector, textContext)
}

// Set style for page types and pseudo-types matching “pageType“.
func (styleFor StyleFor) SetPageComputedStylesT(pageType utils.PageElement, html *HTML) {
	styleFor.addPageDeclarations(pageType)

	// Apply style for page
	// @page inherits from the Root Element :
	// http://lists.w3.org/Archives/Public/www-style/2012Jan/1164.html
	styleFor.setComputedStyles(pageType, html.Root, html.Root, "", html.BaseUrl, nil)

	// Apply style for page pseudo-elements (margin boxes)
	for key := range styleFor.cascadedStyles {
		// Element, pseudoType = key
		if key.PseudoType != "" && key.PageType == pageType {
			// The pseudo-Element inherits from the Element.
			styleFor.setComputedStyles(key.PageType, key.PageType, html.Root, key.PseudoType, html.BaseUrl, nil)
		}
	}
}

// Return tokens with resolved CSS variables.
func resolveVar(computed map[string]pr.RawTokens, token Token, knownVariables utils.Set) []Token {
	if !validation.HasVar(token) {
		return nil
	}

	if knownVariables == nil {
		knownVariables = make(utils.Set)
	}

	fn := token.(pa.FunctionBlock)
	if utils.AsciiLower(fn.Name) != "var" {
		arguments := []Token{}
		for _, argument := range fn.Arguments {
			if _, isFunction := argument.(pa.FunctionBlock); isFunction {
				arguments = append(arguments, resolveVar(computed, argument, knownVariables)...)
			} else {
				arguments = append(arguments, argument)
			}
		}
		token = pa.NewFunctionBlock(token.Pos(), fn.Name, arguments)
		if resolved := resolveVar(computed, token, knownVariables); len(resolved) != 0 {
			return resolved
		}
		return []Token{token}
	}

	_, args := pa.ParseFunction(token)
	// first arg is name, next args are default value
	varNameToken, default_ := args[0], args[1:]
	variableName := varNameToken.(pa.Ident).Value
	if knownVariables.Has(variableName) {
		return nil // endless recursion, returned value is nothing
	} else {
		knownVariables.Add(variableName)
	}

	source := default_
	if l := computed[variableName]; len(l) != 0 {
		source = l
	}
	computedValue := []Token{}
	for _, value := range source {
		if resolved := resolveVar(computed, value, knownVariables); resolved != nil {
			computedValue = append(computedValue, resolved...)
		} else {
			computedValue = append(computedValue, value)
		}
	}
	return computedValue
}

// parseListInteger czyta atrybut całkowitoliczbowy listy (start, value); pusty lub błędny daje false.
func parseListInteger(attribute string) (int, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(attribute))
	return value, err == nil
}

// withoutPageSize (gopdfgo) usuwa deklaracje size z reguły @page stojącej w @media zależnym od rozmiaru
// medium (CSS Page 3 § 7.1).
func withoutPageSize(declarations []validation.Declaration) []validation.Declaration {
	out := declarations[:0:0]
	for _, declaration := range declarations {
		if declaration.Name.KnownProp != pr.PSize {
			out = append(out, declaration)
		}
	}
	return out
}
