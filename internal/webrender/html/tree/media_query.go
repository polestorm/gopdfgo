package tree

import (
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// gopdfgo: zapytania o medium z cechami (Media Queries 4). Źródło znało tylko typy medium i odrzucało
// całą regułę @media, gdy zapytanie miało cokolwiek poza jednym identyfikatorem.
//
// Rozmiar medium to obszar strony arkusza domyślnego (ADR 0019, CSS Page 3 § 7.1: zapytania o medium
// nie uwzględniają size, lecz zakładają papier wybrany przy braku reguł @page). Arkusz domyślny to A4
// w pionie albo rozmiar z arkusza użytkownika (pełni rolę okna druku), pomniejszony o marginesy @page
// z arkusza przeglądarki (75 px, html5_ua.css) i arkusza użytkownika; @page autora nie ma wpływu, więc
// arkusze czyta się raz. Rozmiar ustala wywołujący przez SetMediaReference. Pozostałe cechy są stałe:
// 96 pikseli na cal, kolor, bez wskaźnika, bez skryptów.
const (
	defaultMediaWidth  = 210*96/25.4 - 2*75 // px: A4 bez domyślnych marginesów
	defaultMediaHeight = 297*96/25.4 - 2*75 // px
)

// Stan globalny: renderowanie jest szeregowane (jedno naraz), a wynik zapytań jest wyliczany już przy
// czytaniu arkusza.
var (
	mediaReferenceWidth, mediaReferenceHeight float64 = defaultMediaWidth, defaultMediaHeight
	// mediaSizeSeen: ostatnio przetworzona lista zapytań zawierała cechę zależną od rozmiaru medium
	mediaSizeSeen bool
	// sizeMediaDepth: liczba otwartych reguł @media i @import, których zapytanie zależy od rozmiaru
	// medium; size w @page wewnątrz nich jest pomijane (CSS Page 3 § 7.1)
	sizeMediaDepth int
)

// SetMediaReference ustawia rozmiar medium dla cech width, height, aspect-ratio i orientation.
// Wartości niedodatnie przywracają rozmiar domyślny.
func SetMediaReference(width, height float64) {
	mediaReferenceWidth, mediaReferenceHeight = defaultMediaWidth, defaultMediaHeight
	if width > 0 && height > 0 {
		mediaReferenceWidth, mediaReferenceHeight = width, height
	}
	mediaSizeSeen = false
	sizeMediaDepth = 0
}

// MediaReference podaje bieżący rozmiar medium.
func MediaReference() (width, height float64) { return mediaReferenceWidth, mediaReferenceHeight }

// parseMediaQueryForSize parsuje listę zapytań i mówi też, czy któreś zależy od rozmiaru medium.
func parseMediaQueryForSize(tokens []Token) (media []string, sizeDependent bool) {
	saved := mediaSizeSeen
	mediaSizeSeen = false
	media = parseMediaQuery(tokens)
	sizeDependent = mediaSizeSeen
	mediaSizeSeen = saved || sizeDependent
	return media, sizeDependent
}

// Wynik zapytania zależy jeszcze tylko od typu medium, więc zapisujemy go jako napis:
// typ ("all", "print", …), "!typ" (zaprzeczenie) albo mediaNever (zawsze fałsz).
const mediaNever = "\x00never"

// Return the boolean evaluation of `queryList` for the given
// `deviceMediaType`.
func evaluateMediaQuery(queryList []string, deviceMediaType string) bool {
	for _, query := range queryList {
		if query == mediaNever {
			continue
		}
		if negated, isNegated := strings.CutPrefix(query, "!"); isNegated {
			if negated != "all" && negated != deviceMediaType {
				return true
			}
			continue
		}
		if query == "all" || query == deviceMediaType {
			return true
		}
	}
	return false
}

// parseMediaQuery zwraca listę zapytań z ocenionymi już cechami; nil oznacza błąd składni
// w zapytaniu złożonym z samego typu (zachowanie źródła). Zapytanie z nieznaną cechą albo błędną
// składnią warunku jest fałszywe, ale nie unieważnia pozostałych zapytań z listy (MQ 4 § 3.2).
func parseMediaQuery(tokens []Token) []string {
	tokens = parser.RemoveWhitespace(tokens)
	if len(tokens) == 0 {
		return []string{"all"}
	}
	var media []string
	for _, part := range parser.SplitOnComma(tokens) {
		part = parser.RemoveWhitespace(part)
		if len(part) == 0 {
			logger.WarningLogger.Printf("Expected a media type, got %s", parser.Serialize(part))
			return nil
		}
		media = append(media, parseSingleMediaQuery(part))
	}
	return media
}

func mediaKeyword(token Token) string {
	if ident, ok := token.(parser.Ident); ok {
		return utils.AsciiLower(ident.Value)
	}
	return ""
}

func parseSingleMediaQuery(part []Token) string {
	negated := false
	mediaType := "all"
	rest := part
	// [ not | only ]? <typ> [ and <warunek bez or> ]?  albo sam warunek
	switch first := mediaKeyword(rest[0]); {
	case first == "not" && len(rest) > 1 && mediaKeyword(rest[1]) != "":
		negated, rest = true, rest[1:]
	case first == "only":
		rest = rest[1:]
		if len(rest) == 0 || mediaKeyword(rest[0]) == "" {
			return mediaNever
		}
	}
	if kw := mediaKeyword(rest[0]); kw != "" && kw != "not" {
		mediaType, rest = kw, rest[1:]
		if len(rest) != 0 {
			if mediaKeyword(rest[0]) != "and" || len(rest) < 2 {
				return mediaNever
			}
			rest = rest[1:]
			// po typie medium dozwolone są tylko warunki łączone przez and
			for i := 1; i < len(rest); i += 2 {
				if mediaKeyword(rest[i]) != "and" {
					return mediaNever
				}
			}
		}
	}
	features, valid := true, true
	if len(rest) != 0 {
		features, valid = evaluateMediaCondition(rest)
	}
	switch {
	case !valid:
		return mediaNever
	case negated && !features:
		return "all"
	case negated:
		return "!" + mediaType
	case !features:
		return mediaNever
	}
	return mediaType
}

// evaluateMediaCondition ocenia warunek: not <w> | <w> [and <w>]* | <w> [or <w>]*,
// gdzie <w> to nawias z cechą albo z zagnieżdżonym warunkiem.
func evaluateMediaCondition(tokens []Token) (result, valid bool) {
	tokens = parser.RemoveWhitespace(tokens)
	if len(tokens) == 0 {
		return false, false
	}
	single := func(token Token) (bool, bool) {
		block, ok := token.(parser.ParenthesesBlock)
		if !ok {
			return false, false
		}
		inner := parser.RemoveWhitespace(block.Arguments)
		if len(inner) == 0 {
			return false, false
		}
		if _, nested := inner[0].(parser.ParenthesesBlock); nested || mediaKeyword(inner[0]) == "not" {
			return evaluateMediaCondition(inner)
		}
		return evaluateMediaFeature(inner), true
	}
	if mediaKeyword(tokens[0]) == "not" {
		if len(tokens) != 2 {
			return false, false
		}
		value, ok := single(tokens[1])
		return !value, ok
	}
	result, valid = single(tokens[0])
	if !valid || len(tokens)%2 == 0 {
		return false, false
	}
	operator := ""
	for i := 1; i < len(tokens); i += 2 {
		op := mediaKeyword(tokens[i])
		if (op != "and" && op != "or") || (operator != "" && op != operator) {
			return false, false
		}
		operator = op
		value, ok := single(tokens[i+1])
		if !ok {
			return false, false
		}
		if op == "and" {
			result = result && value
		} else {
			result = result || value
		}
	}
	return result, true
}

// mediaLength zwraca długość w pikselach; em i rem liczone od początkowego rozmiaru fontu (16px).
func mediaLength(token Token) (float64, bool) {
	switch v := token.(type) {
	case parser.Number:
		if v.ValueF == 0 {
			return 0, true
		}
	case parser.Dimension:
		value := float64(v.ValueF)
		switch utils.AsciiLower(v.Unit) {
		case "px":
			return value, true
		case "in":
			return value * 96, true
		case "cm":
			return value * 96 / 2.54, true
		case "mm":
			return value * 96 / 25.4, true
		case "q":
			return value * 96 / 101.6, true
		case "pt":
			return value * 96 / 72, true
		case "pc":
			return value * 16, true
		case "em", "rem":
			return value * 16, true
		}
	}
	return 0, false
}

// mediaResolution zwraca rozdzielczość w dppx.
func mediaResolution(token Token) (float64, bool) {
	if v, ok := token.(parser.Dimension); ok {
		switch utils.AsciiLower(v.Unit) {
		case "dppx", "x":
			return float64(v.ValueF), true
		case "dpi":
			return float64(v.ValueF) / 96, true
		case "dpcm":
			return float64(v.ValueF) * 2.54 / 96, true
		}
	}
	return 0, false
}

// Cechy dyskretne medium odniesienia; pusta wartość cechy (kontekst logiczny) jest prawdziwa,
// gdy wartość nie jest "none".
var mediaDiscreteFeatures = map[string]string{
	"orientation":            "portrait",
	"prefers-color-scheme":   "light",
	"prefers-reduced-motion": "no-preference",
	"prefers-contrast":       "no-preference",
	"forced-colors":          "none",
	"hover":                  "none",
	"any-hover":              "none",
	"pointer":                "none",
	"any-pointer":            "none",
	"scripting":              "none",
	"update":                 "none",
	"overflow-block":         "paged",
	"overflow-inline":        "none",
	"color-gamut":            "srgb",
	"display-mode":           "browser",
}

// evaluateMediaFeature ocenia zawartość jednego nawiasu: (cecha), (cecha: wartość) albo zakres
// (width >= 600px), (400px <= width <= 700px). Nieznana cecha jest fałszywa.
func evaluateMediaFeature(tokens []Token) bool {
	// zakres: operatory porównania zamiast dwukropka
	var operands [][]Token
	var operators []string
	current := []Token{}
	isRange := false
	for i := 0; i < len(tokens); i++ {
		lit, ok := tokens[i].(parser.Literal)
		if ok && (lit.Value == "<" || lit.Value == ">" || lit.Value == "=" || lit.Value == "<=" || lit.Value == ">=") {
			op := lit.Value
			if next, ok := tokenLiteral(tokens, i+1); ok && next == "=" && len(op) == 1 && op != "=" {
				op += "="
				i++
			}
			operands, operators, current = append(operands, current), append(operators, op), []Token{}
			isRange = true
			continue
		}
		current = append(current, tokens[i])
	}
	if isRange {
		operands = append(operands, current)
		return evaluateMediaRange(operands, operators)
	}

	name := mediaKeyword(tokens[0])
	if name == "" {
		return false
	}
	if len(tokens) == 1 {
		// kontekst logiczny
		switch name {
		case "width", "height", "color", "aspect-ratio", "resolution", "device-width", "device-height":
			return true
		case "monochrome", "grid", "color-index":
			return false
		}
		value, known := mediaDiscreteFeatures[name]
		return known && value != "none"
	}
	if len(tokens) < 3 || !parser.IsLiteral(tokens[1], ":") {
		return false
	}
	value := tokens[2:]
	prefix := ""
	for _, p := range [...]string{"min-", "max-"} {
		if strings.HasPrefix(name, p) {
			prefix, name = p, name[len(p):]
		}
	}
	compare := func(actual, wanted float64) bool {
		switch prefix {
		case "min-":
			return actual >= wanted
		case "max-":
			return actual <= wanted
		}
		return actual == wanted
	}
	switch name {
	case "width", "device-width", "height", "device-height":
		mediaSizeSeen = true
		wanted, ok := mediaLength(value[0])
		if !ok || len(value) != 1 {
			return false
		}
		if strings.HasSuffix(name, "width") {
			return compare(mediaReferenceWidth, wanted)
		}
		return compare(mediaReferenceHeight, wanted)
	case "aspect-ratio", "device-aspect-ratio":
		mediaSizeSeen = true
		wanted, ok := mediaRatio(value)
		return ok && compare(mediaReferenceWidth/mediaReferenceHeight, wanted)
	case "resolution":
		wanted, ok := mediaResolution(value[0])
		return ok && len(value) == 1 && compare(1, wanted)
	case "color":
		number, ok := value[0].(parser.Number)
		return ok && len(value) == 1 && compare(8, float64(number.ValueF))
	case "monochrome", "color-index", "grid":
		number, ok := value[0].(parser.Number)
		return ok && len(value) == 1 && compare(0, float64(number.ValueF))
	}
	if prefix != "" || len(value) != 1 {
		return false
	}
	actual, known := mediaDiscreteFeatures[name]
	if name == "orientation" {
		mediaSizeSeen = true
		actual = "portrait"
		if mediaReferenceWidth > mediaReferenceHeight {
			actual = "landscape"
		}
	}
	return known && mediaKeyword(value[0]) == actual
}

func tokenLiteral(tokens []Token, index int) (string, bool) {
	if index >= len(tokens) {
		return "", false
	}
	lit, ok := tokens[index].(parser.Literal)
	return lit.Value, ok
}

func mediaRatio(tokens []Token) (float64, bool) {
	var numbers []float64
	for _, token := range tokens {
		if parser.IsLiteral(token, "/") {
			continue
		}
		number, ok := token.(parser.Number)
		if !ok {
			return 0, false
		}
		numbers = append(numbers, float64(number.ValueF))
	}
	switch {
	case len(numbers) == 1:
		return numbers[0], true
	case len(numbers) == 2 && numbers[1] != 0:
		return numbers[0] / numbers[1], true
	}
	return 0, false
}

// evaluateMediaRange ocenia zakres o dwóch albo trzech operandach; jednym z nich jest nazwa cechy.
func evaluateMediaRange(operands [][]Token, operators []string) bool {
	if len(operands) < 2 || len(operands) > 3 {
		return false
	}
	values := make([]float64, len(operands))
	feature := ""
	for index, operand := range operands {
		if len(operand) == 1 {
			if name := mediaKeyword(operand[0]); name != "" {
				if feature != "" {
					return false
				}
				feature = name
				values[index] = 0
				continue
			}
		}
		operands[index] = operand
	}
	var actual float64
	var convert func([]Token) (float64, bool)
	one := func(f func(Token) (float64, bool)) func([]Token) (float64, bool) {
		return func(tokens []Token) (float64, bool) {
			if len(tokens) != 1 {
				return 0, false
			}
			return f(tokens[0])
		}
	}
	if feature != "resolution" {
		mediaSizeSeen = true
	}
	switch feature {
	case "width", "device-width":
		actual, convert = mediaReferenceWidth, one(mediaLength)
	case "height", "device-height":
		actual, convert = mediaReferenceHeight, one(mediaLength)
	case "aspect-ratio":
		actual, convert = mediaReferenceWidth/mediaReferenceHeight, mediaRatio
	case "resolution":
		actual, convert = 1, one(mediaResolution)
	default:
		return false
	}
	for index, operand := range operands {
		if len(operand) == 1 && mediaKeyword(operand[0]) == feature {
			values[index] = actual
			continue
		}
		value, ok := convert(operand)
		if !ok {
			return false
		}
		values[index] = value
	}
	if len(operands) == 3 {
		// cecha musi stać w środku, a oba operatory muszą mieć ten sam kierunek
		if len(operands[1]) != 1 || mediaKeyword(operands[1][0]) != feature ||
			operators[0][0] != operators[1][0] || operators[0][0] == '=' {
			return false
		}
	}
	for index, op := range operators {
		left, right := values[index], values[index+1]
		var ok bool
		switch op {
		case "<":
			ok = left < right
		case "<=":
			ok = left <= right
		case ">":
			ok = left > right
		case ">=":
			ok = left >= right
		case "=":
			ok = left == right
		}
		if !ok {
			return false
		}
	}
	return true
}
