package svg

import (
	"strings"

	pa "github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
	"golang.org/x/net/html"
)

// Podstawianie var() w atrybutach i właściwościach SVG (gopdfgo).

// geometryProperties to właściwości geometrii SVG 2 (§ 9.2 i in.), których wartość podstawiona przez
// var() musi być poprawną długością CSS (zob. substituteAttributeVars)
var geometryProperties = utils.NewSet("x", "y", "width", "height", "r", "rx", "ry", "cx", "cy")

// substituteAttributeVars podstawia var() w wartościach atrybutów i właściwości elementu (CSS Variables
// 1 § 3; atrybuty prezentacyjne są parsowane gramatyką właściwości CSS, SVG 2 § 6.6), np.
// r="var(--radii)". Zmienne bierze z kaskady dokumentu HTML (doc), a w samodzielnym dokumencie SVG
// z odziedziczonych właściwości niestandardowych („--x”) w attrs. Wartość, której nie da się podstawić,
// jest nieważna w chwili obliczania: właściwość dziedziczona bierze wartość rodzica, pozostałe wartość
// początkową. Wynik podstawienia jest parsowany jak CSS, więc dla właściwości geometrii liczba bez
// jednostki (inna niż 0) jest nieważna poza trybem zgodności wstecznej dokumentu HTML (Quirks Mode § 3.2;
// testy WPT css-var-on-length-attributes-*). Źródło nie znało var() w SVG.
func (attrs nodeAttributes) substituteAttributeVars(node *html.Node, parent nodeAttributes, doc *DocumentStyle) {
	lookup := func(name string) (string, bool) {
		if doc != nil && doc.Variable != nil {
			return doc.Variable(node, name)
		}
		v, ok := attrs[name]
		return v, ok && strings.TrimSpace(v) != ""
	}
	for key, value := range attrs {
		if strings.HasPrefix(key, "--") || !strings.Contains(strings.ToLower(value), "var(") {
			continue
		}
		tokens, ok := substituteVarTokens(pa.Tokenize([]byte(value), true), lookup, 0)
		resolved := strings.TrimSpace(pa.Serialize(tokens))
		if ok && geometryProperties.Has(key) && (doc == nil || !doc.Quirks) {
			if n, isNumber := singleNumber(tokens); isNumber && n != 0 {
				ok = false
			}
		}
		if ok && resolved != "" {
			attrs[key] = resolved
			continue
		}
		delete(attrs, key)
		if pv, has := parent[key]; has && !notInheritedAttributes.Has(key) {
			attrs[key] = pv
		}
	}
}

// singleNumber mówi, czy tokeny (bez odstępów) to jedna liczba bez jednostki.
func singleNumber(tokens []pa.Token) (float64, bool) {
	tokens = pa.RemoveWhitespace(tokens)
	if len(tokens) != 1 {
		return 0, false
	}
	if n, ok := tokens[0].(pa.Number); ok {
		return float64(n.ValueF), true
	}
	return 0, false
}

// substituteVarTokens zastępuje funkcje var() w tokenach wartością zmiennej albo wartością zastępczą;
// false, gdy zmiennej nie ma i nie podano wartości zastępczej (albo przy cyklu).
func substituteVarTokens(tokens []pa.Token, lookup func(string) (string, bool), depth int) ([]pa.Token, bool) {
	if depth > 32 {
		return nil, false
	}
	out := make([]pa.Token, 0, len(tokens))
	for _, t := range tokens {
		fn, isFn := t.(pa.FunctionBlock)
		if !isFn {
			out = append(out, t)
			continue
		}
		if !strings.EqualFold(fn.Name, "var") {
			args, ok := substituteVarTokens(fn.Arguments, lookup, depth)
			if !ok {
				return nil, false
			}
			out = append(out, pa.NewFunctionBlock(fn.Pos(), fn.Name, args))
			continue
		}
		var name string
		var fallback []pa.Token
		hasFallback := false
		for i, a := range fn.Arguments {
			if lit, ok := a.(pa.Literal); ok && lit.Value == "," {
				fallback, hasFallback = fn.Arguments[i+1:], true
				break
			}
			if id, ok := a.(pa.Ident); ok && name == "" {
				name = id.Value
			}
		}
		if !strings.HasPrefix(name, "--") {
			return nil, false
		}
		var sub []pa.Token
		if v, ok := lookup(name); ok {
			sub = pa.Tokenize([]byte(v), true)
		} else if hasFallback {
			sub = fallback
		} else {
			return nil, false
		}
		resolved, ok := substituteVarTokens(sub, lookup, depth+1)
		if !ok {
			return nil, false
		}
		out = append(out, resolved...)
	}
	return out, true
}
