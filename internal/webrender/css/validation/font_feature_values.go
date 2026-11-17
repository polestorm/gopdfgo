package validation

// gopdfgo: reguła @font-feature-values i funkcyjne wartości font-variant-alternates
// (CSS Fonts 4 § 6.8 i § 6.9). Źródło znało tylko „normal” i „historical-forms”,
// a regułę @font-feature-values pomijało jako nieznaną.

import (
	"strings"

	pa "github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// alternatesFunctions podaje funkcje font-variant-alternates w kolejności
// gramatyki i to, czy przyjmują listę nazw (CSS Fonts 4 § 6.8). Te same
// nazwy (bez nawiasów) są blokami w @font-feature-values.
var alternatesFunctions = [...]struct {
	name string
	list bool
}{
	{"stylistic", false},
	{"styleset", true},
	{"character-variant", true},
	{"swash", false},
	{"ornaments", false},
	{"annotation", false},
}

func alternatesFunction(name string) (index int, list bool) {
	for i, f := range alternatesFunctions {
		if f.name == name {
			return i, f.list
		}
	}
	return -1, false
}

// fontVariantAlternatesValue sprawdza wartość inną niż „normal”:
// [ stylistic(<name>) || historical-forms || styleset(<name>#) ||
// character-variant(<name>#) || swash(<name>) || ornaments(<name>) ||
// annotation(<name>) ]. Wynik to postać kanoniczna w kolejności gramatyki,
// np. „historical-forms styleset(a,b) swash(c)”, rozwijana przy składaniu
// tekstu przez ParseFontVariantAlternates. Nazwy zachowują wielkość liter
// (<custom-ident>), słowa kluczowe nie.
func fontVariantAlternatesValue(tokens []Token) pr.CssProperty {
	var (
		historical bool
		functions  [len(alternatesFunctions)][]string
	)
	tokens = pa.RemoveWhitespace(tokens)
	if len(tokens) == 0 {
		return nil
	}
	for _, token := range tokens {
		switch token := token.(type) {
		case pa.Ident:
			if utils.AsciiLower(token.Value) != "historical-forms" || historical {
				return nil
			}
			historical = true
		case pa.FunctionBlock:
			index, list := alternatesFunction(utils.AsciiLower(token.Name))
			if index < 0 || functions[index] != nil {
				return nil
			}
			var names []string
			for _, part := range pa.SplitOnComma(token.Arguments) {
				part = pa.RemoveWhitespace(part)
				if len(part) != 1 {
					return nil
				}
				ident, ok := part[0].(pa.Ident)
				if !ok || !isFeatureValueName(ident.Value) {
					return nil
				}
				names = append(names, ident.Value)
			}
			if len(names) == 0 || (!list && len(names) != 1) {
				return nil
			}
			functions[index] = names
		default:
			return nil
		}
	}
	var parts []string
	if historical {
		parts = append(parts, "historical-forms")
	}
	for i, names := range functions {
		if names != nil {
			parts = append(parts, alternatesFunctions[i].name+"("+strings.Join(names, ",")+")")
		}
	}
	return pr.String(strings.Join(parts, " "))
}

// isFeatureValueName: <feature-value-name> to <custom-ident> (CSS Values 4 § 4.2),
// a więc nie słowo ogólnoCSS-owe ani „default”; znaki rozdzielające postać
// kanoniczną wykluczamy, bo nie da się ich wtedy odczytać.
func isFeatureValueName(name string) bool {
	switch utils.AsciiLower(name) {
	case "", "initial", "inherit", "unset", "revert", "revert-layer", "default":
		return false
	}
	return !strings.ContainsAny(name, " (),")
}

// AlternatesValue to jedna funkcja z wartości font-variant-alternates.
type AlternatesValue struct {
	Function string   // „stylistic”, „styleset”, …
	Names    []string // nazwy z @font-feature-values
}

// ParseFontVariantAlternates rozkłada postać kanoniczną z fontVariantAlternatesValue.
func ParseFontVariantAlternates(value string) (historical bool, functions []AlternatesValue) {
	for _, part := range strings.Fields(value) {
		open := strings.IndexByte(part, '(')
		if open < 0 {
			historical = historical || part == "historical-forms"
			continue
		}
		names := strings.Split(strings.TrimSuffix(part[open+1:], ")"), ",")
		functions = append(functions, AlternatesValue{Function: part[:open], Names: names})
	}
	return historical, functions
}

// FontFeatureValuesRule to jedna reguła @font-feature-values: rodziny z prelude
// (małymi literami) i wartości z bloków @stylistic, @styleset, …
type FontFeatureValuesRule struct {
	Families []string
	Values   map[string]map[string][]int // blok („styleset”) → nazwa → liczby
}

// FontFeatureValues to tablica rodzina (małymi literami) → blok → nazwa → liczby,
// złożona ze wszystkich reguł @font-feature-values dokumentu.
type FontFeatureValues map[string]map[string]map[string][]int

// Add dokłada regułę; nazwa zdefiniowana ponownie zastępuje wcześniejszą
// (CSS Fonts 4 § 6.9.1: reguły tej samej rodziny łączą się, późniejsze wygrywają).
func (t FontFeatureValues) Add(rule FontFeatureValuesRule) {
	for _, family := range rule.Families {
		blocks := t[family]
		if blocks == nil {
			blocks = map[string]map[string][]int{}
			t[family] = blocks
		}
		for block, names := range rule.Values {
			if blocks[block] == nil {
				blocks[block] = map[string][]int{}
			}
			for name, values := range names {
				blocks[block][name] = values
			}
		}
	}
}

// Lookup szuka nazwy w bloku dla kolejnych rodzin z listy font-family;
// pierwsza rodzina, która ją definiuje, wygrywa.
func (t FontFeatureValues) Lookup(families []string, block, name string) ([]int, bool) {
	for _, family := range families {
		if values, ok := t[strings.ToLower(family)][block][name]; ok {
			return values, true
		}
	}
	return nil, false
}

// ParseFontFeatureValues sprawdza regułę @font-feature-values (CSS Fonts 4 § 6.9).
// Prelude to <family-name>#; treść to bloki @stylistic { nazwa: <integer [0,∞]>+; }
// i podobne. Błędne deklaracje i nieznane bloki są pomijane, nie unieważniają reguły.
// ok = false, gdy prelude jest błędne.
func ParseFontFeatureValues(prelude, content []Token) (rule FontFeatureValuesRule, ok bool) {
	for _, part := range pa.SplitOnComma(prelude) {
		family := _fontFamilyDesc(pa.RemoveWhitespace(part), false)
		if family == "" {
			return rule, false
		}
		rule.Families = append(rule.Families, strings.ToLower(family))
	}
	if len(rule.Families) == 0 {
		return rule, false
	}
	rule.Values = map[string]map[string][]int{}
	for _, item := range pa.ParseBlocksContents(content, false) {
		block, isBlock := item.(pa.AtRule)
		if !isBlock || block.Content == nil {
			continue // font-display i inne deklaracje opisowe nie dotyczą cech
		}
		name := utils.AsciiLower(block.AtKeyword)
		if index, _ := alternatesFunction(name); index < 0 {
			continue
		}
		for _, item := range pa.ParseBlocksContents(block.Content, false) {
			decl, isDecl := item.(pa.Declaration)
			if !isDecl || !isFeatureValueName(decl.Name) {
				continue
			}
			values, valid := featureValueNumbers(name, pa.RemoveWhitespace(decl.Value))
			if !valid {
				continue
			}
			if rule.Values[name] == nil {
				rule.Values[name] = map[string][]int{}
			}
			rule.Values[name][decl.Name] = values
		}
	}
	return rule, true
}

// featureValueNumbers: liczby całkowite nieujemne; @styleset przyjmuje dowolnie
// wiele, @character-variant jedną albo dwie, pozostałe bloki dokładnie jedną.
func featureValueNumbers(block string, tokens []Token) ([]int, bool) {
	var values []int
	for _, token := range tokens {
		number, ok := token.(pa.Number)
		if !ok || !number.IsInt() || number.ValueF < 0 {
			return nil, false
		}
		values = append(values, number.Int())
	}
	switch block {
	case "styleset":
		return values, len(values) >= 1
	case "character-variant":
		return values, len(values) == 1 || len(values) == 2
	default:
		return values, len(values) == 1
	}
}
