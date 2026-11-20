package tree

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	pa "github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/css/validation"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
	"golang.org/x/net/html"
)

// Kaskada SVG osadzonego w HTML (gopdfgo).
//
// Elementy <svg> leżące w dokumencie HTML są częścią tego dokumentu, więc dotyczą ich wszystkie
// arkusze dokumentu (CSS Cascade 4 § 6, SVG 2 § 6.1 i § 6.3), a selektory mogą przekraczać granicę
// HTML/SVG („div.report svg rect”). Źródło stosowało do nich tylko elementy <style> leżące wewnątrz
// <svg>, bez arkuszy dokumentu, arkusza użytkownika i bez kolejności według specyficzności.
// Właściwości SVG (fill, stroke…) są nieznane walidacji HTML, dlatego kaskada HTML zapamiętuje
// dla elementów SVG dopasowane reguły z deklaracjami w postaci surowej (match.raw), a pakiet svg
// stosuje je do swoich atrybutów tekstowych, wplatając atrybuty prezentacyjne i atrybut style
// we właściwe miejsca (SVG 2 § 6.2).

// svgMatch to reguła arkusza autora albo użytkownika dopasowana do elementu SVG.
type svgMatch struct {
	user   bool
	result matchResult
}

// svgSheetUsable mówi, czy reguły arkusza dotyczą elementów SVG: arkusz domyślny przeglądarki
// (HTML, bez przestrzeni nazw) i arkusz wskazówek prezentacyjnych HTML ich nie dotyczą.
func svgSheetUsable(sh sheet) bool {
	return (sh.origin == "author" || sh.origin == "user") && sh.specificity == nil
}

// recordSVGMatches zapamiętuje reguły dopasowane do elementu w przestrzeni nazw SVG.
func (sf *StyleFor) recordSVGMatches(element *utils.HTMLNode, matches [][]matchResult) {
	var out []svgMatch
	for i, sh := range sf.sheets {
		if !svgSheetUsable(sh) {
			continue
		}
		for _, m := range matches[i] {
			if m.pseudoType == "" && len(m.raw) != 0 {
				out = append(out, svgMatch{user: sh.origin == "user", result: m})
			}
		}
	}
	if len(out) == 0 {
		return
	}
	if sf.svgMatches == nil {
		sf.svgMatches = map[*utils.HTMLNode][]svgMatch{}
	}
	sf.svgMatches[element] = out
}

// matchSVGRules dopasowuje reguły arkuszy autora i użytkownika do dowolnego elementu (przodków
// HTML elementu <svg>, dla których kaskada nie zapamiętała reguł).
func (s StyleFor) matchSVGRules(element *utils.HTMLNode) []svgMatch {
	if element.Namespace == "svg" {
		return s.svgMatches[element]
	}
	var out []svgMatch
	for _, sh := range s.sheets {
		if !svgSheetUsable(sh) {
			continue
		}
		for _, m := range sh.sheet.matcher.match(element.AsHtmlNode()) {
			if m.pseudoType == "" && len(m.raw) != 0 {
				out = append(out, svgMatch{user: sh.origin == "user", result: m})
			}
		}
	}
	return out
}

// sortedRawDeclarations zwraca deklaracje dopasowanych reguł w rosnącej kolejności kaskady
// (pochodzenie i ważność, warstwa, specyficzność, kolejność w arkuszach), z rozwiązanymi var().
func (s StyleFor) sortedRawDeclarations(element *utils.HTMLNode, matches []svgMatch) []validation.RawDeclaration {
	if len(matches) == 0 {
		return nil
	}
	type item struct {
		w weight
		d validation.RawDeclaration
	}
	var variables map[string]pr.RawTokens
	if style := s.computedStyles[element.ToKey("")]; style != nil {
		variables = style.Variables()
	}
	var items []item
	for _, m := range matches {
		origin := "author"
		if m.user {
			origin = "user"
		}
		for _, decl := range m.result.raw {
			name := utils.AsciiLower(decl.Name)
			if strings.HasPrefix(name, "-") {
				continue
			}
			value := strings.TrimSpace(serializeResolvingVars(decl.Value, variables))
			if value == "" {
				continue
			}
			w := weight{precedence: declarationPrecedence(origin, decl.Important), specificity: m.result.specificity, layer: s.layers[m.result.layer]}
			items = append(items, item{w: w, d: validation.RawDeclaration{Name: name, Value: value, Important: decl.Important, User: m.user}})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return !items[j].w.Less(items[i].w) })
	out := make([]validation.RawDeclaration, len(items))
	for i, it := range items {
		out[i] = it.d
	}
	return out
}

// serializeResolvingVars zapisuje wartość deklaracji, zastępując var() wartościami zmiennych
// elementu (CSS Variables 1 § 3).
func serializeResolvingVars(tokens []pa.Token, variables map[string]pr.RawTokens) string {
	hasVar := false
	for _, token := range tokens {
		if validation.HasVar(token) {
			hasVar = true
			break
		}
	}
	if !hasVar {
		return pa.Serialize(tokens)
	}
	var solved []pa.Token
	for _, token := range tokens {
		if resolved := resolveVar(variables, token, utils.Set{}); resolved != nil {
			solved = append(solved, resolved...)
		} else if validation.HasVar(token) {
			return "" // zmienna bez wartości: deklaracja nieważna w chwili obliczania
		} else {
			solved = append(solved, token)
		}
	}
	return pa.Serialize(solved)
}

// SVGDeclarations zwraca deklaracje arkuszy autora i użytkownika dokumentu, które dotyczą elementu
// SVG osadzonego w HTML, w rosnącej kolejności kaskady (bez atrybutu style, który stosuje pakiet svg).
func (s StyleFor) SVGDeclarations(element *html.Node) []validation.RawDeclaration {
	node := (*utils.HTMLNode)(element)
	return s.sortedRawDeclarations(node, s.svgMatches[node])
}

// svgOnlyInherited to dziedziczone właściwości SVG nieznane kaskadzie HTML (SVG 2, indeks
// właściwości; CSS Inline 3 dla dominant-baseline); ich wartości z przodków HTML elementu <svg>
// trzeba wyznaczyć z deklaracji surowych.
var svgOnlyInherited = utils.NewSet("fill", "fill-opacity", "fill-rule", "stroke", "stroke-dasharray",
	"stroke-dashoffset", "stroke-linecap", "stroke-linejoin", "stroke-miterlimit", "stroke-opacity",
	"stroke-width", "marker-start", "marker-mid", "marker-end", "paint-order", "text-anchor", "clip-rule",
	"color-interpolation", "color-interpolation-filters", "color-rendering", "shape-rendering",
	"text-rendering", "image-rendering", "dominant-baseline")

// SVGInherited zwraca wartości właściwości dziedziczonych, które element główny <svg> osadzony
// w HTML dostaje od swojego rodzica HTML (CSS Cascade 4 § 7.2): właściwości znane kaskadzie HTML
// z jego stylu obliczonego (kolor, pismo, odstępy, visibility), właściwości SVG z deklaracji
// przodków (np. „body { fill: navy }”). fontSize to obliczony rozmiar pisma rodzica w px (0 bez rodzica).
func (s StyleFor) SVGInherited(svgElement *html.Node) (values map[string]string, fontSize float64) {
	parent := svgElement.Parent
	for parent != nil && parent.Type != html.ElementNode {
		parent = parent.Parent
	}
	if parent == nil {
		return nil, 0
	}
	values = map[string]string{}
	// właściwości SVG: od korzenia w dół, później przodek wygrywa
	var chain []*utils.HTMLNode
	for n := parent; n != nil; n = n.Parent {
		if n.Type == html.ElementNode {
			chain = append(chain, (*utils.HTMLNode)(n))
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		node := chain[i]
		own := map[string]string{}
		var important []validation.RawDeclaration
		for _, d := range s.sortedRawDeclarations(node, s.matchSVGRules(node)) {
			if svgOnlyInherited.Has(d.Name) {
				if d.Important {
					important = append(important, d)
				} else {
					own[d.Name] = d.Value
				}
			}
		}
		if style := node.Get("style"); style != "" {
			for _, decl := range pa.ParseBlocksContents(pa.Tokenize([]byte(style), false), false) {
				if decl, ok := decl.(pa.Declaration); ok && !decl.Important {
					if name := utils.AsciiLower(decl.Name); svgOnlyInherited.Has(name) {
						own[name] = strings.TrimSpace(pa.Serialize(decl.Value))
					}
				}
			}
		}
		for _, d := range important {
			own[d.Name] = d.Value
		}
		for name, value := range own {
			switch strings.ToLower(value) {
			case "inherit", "unset":
				continue
			case "initial":
				delete(values, name)
				continue
			}
			values[name] = value
		}
	}

	style := s.computedStyles[(*utils.HTMLNode)(parent).ToKey("")]
	if style == nil {
		return values, 0
	}
	num := func(v pr.Float) string { return strconv.FormatFloat(float64(v), 'g', 6, 64) }
	c := style.GetColor().RGBA
	values["color"] = fmt.Sprintf("rgba(%s,%s,%s,%s)", num(pr.Float(c.R*255)), num(pr.Float(c.G*255)), num(pr.Float(c.B*255)), num(pr.Float(c.A)))
	if size := style.GetFontSize(); size.Unit == pr.Px || size.Unit == pr.Scalar {
		fontSize = float64(size.Value)
		values["font-size"] = num(size.Value) + "px"
	}
	if families := style.GetFontFamily(); len(families) != 0 {
		quoted := make([]string, len(families))
		for i, f := range families {
			if strings.ContainsAny(f, " \"'") {
				quoted[i] = strconv.Quote(f)
			} else {
				quoted[i] = f
			}
		}
		values["font-family"] = strings.Join(quoted, ", ")
	}
	if fs := style.GetFontStyle(); fs != "" {
		values["font-style"] = string(fs)
	}
	if fw := style.GetFontWeight(); fw.String == "" && fw.Int > 0 {
		values["font-weight"] = strconv.Itoa(fw.Int)
	}
	if ls := style.GetLetterSpacing(); ls.Tag == 0 && (ls.Unit == pr.Px || ls.Unit == pr.Scalar) && ls.Value != 0 {
		values["letter-spacing"] = num(ls.Value) + "px"
	}
	if ws := style.GetWordSpacing(); ws.Tag == 0 && (ws.Unit == pr.Px || ws.Unit == pr.Scalar) && ws.Value != 0 {
		values["word-spacing"] = num(ws.Value) + "px"
	}
	if v := style.GetVisibility(); v != "" && v != "visible" {
		values["visibility"] = string(v)
	}
	return values, fontSize
}
