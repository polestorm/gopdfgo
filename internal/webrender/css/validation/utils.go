package validation

import (
	"fmt"
	"math"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/css/counters"
	pa "github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// Default fallback values used in attr() functions
var attrFallbacks = map[string]pr.CssProperty{
	"string":  pr.String(""),
	"color":   pr.String("currentcolor"),
	"url":     pr.String("about:invalid"),
	"integer": pr.Dimension{Unit: pr.Scalar}.Tagged(),
	"number":  pr.Dimension{Unit: pr.Scalar}.Tagged(),
	"%":       pr.Dimension{Unit: pr.Scalar}.Tagged(),
}

func init() {
	for unitString, unit := range LENGTHUNITS {
		attrFallbacks[unitString] = pr.Dimension{Unit: unit}.Tagged()
	}
	for unitString, unit := range AngleUnits {
		attrFallbacks[unitString] = pr.Dimension{Unit: unit}.Tagged()
	}
}

// Split a list of tokens on optional commas, ie “LiteralToken(",")“.
func splitOnOptionalComma(tokens []Token) (parts []Token) {
	for _, splitPart := range pa.SplitOnComma(tokens) {
		if len(splitPart) == 0 {
			// Happens when there"s a comma at the beginning, at the end, or
			// when two commas are next to each other.
			return
		}
		parts = append(parts, splitPart...)
	}
	return parts
}

// If “token“ is a keyword, return its name. Otherwise return empty string.
func getCustomIdent(token Token) string {
	if ident, ok := token.(pa.Ident); ok {
		return string(ident.Value)
	}
	return ""
}

// Parse an <image> token.
func getImage(_token Token, baseUrl string) (pr.Image, error) {
	parsed, _, err := getUrl(_token, baseUrl)
	if err != nil {
		return nil, err
	}
	if parsed.Tag == pr.External {
		return pr.UrlImage(parsed.S), nil
	}

	token, ok := _token.(pa.FunctionBlock)
	if !ok {
		return nil, nil
	}
	arguments := pa.SplitOnComma(pa.RemoveWhitespace(token.Arguments))
	name := utils.AsciiLower(token.Name)
	arguments = withoutInterpolationSpace(arguments)
	if len(arguments) == 0 {
		return nil, nil
	}
	switch name {
	case "linear-gradient", "repeating-linear-gradient":
		direction, colorStops := parseLinearGradientParameters(arguments)
		if len(colorStops) > 0 {
			parsedColorsStop, err := parseColorStops(colorStops)
			if err != nil {
				return nil, err
			}
			return pr.LinearGradient{
				Direction:  direction,
				Repeating:  name == "repeating-linear-gradient",
				ColorStops: parsedColorsStop,
			}, nil
		}
	case "radial-gradient", "repeating-radial-gradient":
		result := parseRadialGradientParameters(arguments)
		if result.IsNone() {
			result.shape = "ellipse"
			result.size = pr.GradientSize{Keyword: "farthest-corner"}
			result.position = pr.CenterPos{OriginX: pr.Left, OriginY: pr.Top, Pos: pr.Point{fiftyPercent, fiftyPercent}}
			result.colorStops = arguments
		}
		if len(result.colorStops) > 0 {
			parsedColorsStop, err := parseColorStops(result.colorStops)
			if err != nil {
				return nil, err
			}
			return pr.RadialGradient{
				ColorStops: parsedColorsStop,
				Shape:      result.shape,
				Size:       result.size,
				Center:     result.position,
				Repeating:  name == "repeating-radial-gradient",
			}, nil
		}
	}
	return nil, nil
}

func parseLinearGradientParameters(arguments [][]Token) (pr.DirectionType, [][]Token) {
	firstArg := arguments[0]
	if len(firstArg) == 1 {
		angle, isNotNone := getAngle(firstArg[0])
		if isNotNone {
			return pr.DirectionType{Angle: angle}, arguments[1:]
		}
	} else {
		var mapped [3]string
		for index, token := range firstArg {
			if index < 3 {
				mapped[index] = getKeyword(token)
			}
		}
		result, isNotNone := directionKeywords[mapped]
		if isNotNone {
			return result, arguments[1:]
		}
	}
	return pr.DirectionType{Angle: math.Pi}, arguments // Default direction is "to bottom"
}

func reverse(a []Token) []Token {
	n := len(a)
	out := make([]Token, n)
	for i := range a {
		out[n-1-i] = a[i]
	}
	return out
}

type radialGradientParameters struct {
	shape      string
	colorStops [][]Token
	position   pr.CenterPos
	size       pr.GradientSize
}

func (r radialGradientParameters) IsNone() bool {
	return r.shape == "" && r.size.IsNone() && r.position.IsNone() && r.colorStops == nil
}

func parseRadialGradientParameters(arguments [][]Token) radialGradientParameters {
	var shape, sizeShape string
	var position pr.CenterPos
	var size pr.GradientSize
	stack := reverse(arguments[0])
	for len(stack) > 0 {
		token := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		keyword := getKeyword(token)
		if keyword == "at" {
			position = parsePosition(reverse(stack))
			if position.IsNone() {
				return radialGradientParameters{}
			}
			break
		} else if (keyword == "circle" || keyword == "ellipse") && shape == "" {
			shape = keyword
		} else if (keyword == "closest-corner" || keyword == "farthest-corner" || keyword == "closest-side" || keyword == "farthest-side") && size.IsNone() {
			size = pr.GradientSize{Keyword: keyword}
		} else {
			if len(stack) > 0 && size.IsNone() {
				length1 := getLength(token, true, true)
				length2 := getLength(stack[len(stack)-1], true, true)
				if !length1.IsNone() && !length2.IsNone() {
					size = pr.GradientSize{Explicit: [2]pr.Dimension{length1, length2}}
					sizeShape = "ellipse"
					i := max(len(stack)-1, 0)
					stack = stack[:i]
				}
			}
			if size.IsNone() {
				length1 := getLength(token, true, false)
				if !length1.IsNone() {
					size = pr.GradientSize{Explicit: [2]pr.Dimension{length1, length1}}
					sizeShape = "circle"
				}
			}
			if size.IsNone() {
				return radialGradientParameters{}
			}
		}
	}
	if shape == "circle" && sizeShape == "ellipse" {
		return radialGradientParameters{}
	}
	out := radialGradientParameters{
		shape:      shape,
		size:       size,
		position:   position,
		colorStops: arguments[1:],
	}
	if shape == "" {
		if sizeShape != "" {
			out.shape = sizeShape
		} else {
			out.shape = "ellipse"
		}
	}
	if size.IsNone() {
		out.size = pr.GradientSize{Keyword: "farthest-corner"}
	}
	if position.IsNone() {
		out.position = pr.CenterPos{
			OriginX: pr.Left,
			OriginY: pr.Top,
			Pos:     pr.Point{fiftyPercent, fiftyPercent},
		}
	}
	return out
}

// gopdfgo: przestrzeń interpolacji z CSS Images 4 („in oklch”, „in hsl longer hue”) w pierwszym
// argumencie gradientu jest przyjmowana i pomijana: barwy interpolujemy zawsze w sRGB. Źródło odrzucało
// cały gradient.
func withoutInterpolationSpace(arguments [][]Token) [][]Token {
	if len(arguments) == 0 {
		return arguments
	}
	first := arguments[0]
	for index, token := range first {
		if getKeyword(token) != "in" || index+1 >= len(first) || getKeyword(first[index+1]) == "" {
			continue
		}
		end := index + 2
		if end+1 < len(first) && getKeyword(first[end+1]) == "hue" {
			end += 2
		}
		kept := append(append([]Token{}, first[:index]...), first[end:]...)
		if len(kept) == 0 {
			return arguments[1:]
		}
		return append([][]Token{kept}, arguments[1:]...)
	}
	return arguments
}

// gopdfgo: lista przystanków gradientu z CSS Images 4: przystanek o dwóch pozycjach („red 0 50%”)
// to dwa przystanki tej samej barwy, a sama pozycja między przystankami to wskazówka przejścia.
// Wskazówkę przybliżamy przystankiem o barwie pośredniej w podanym miejscu (norma każe wygiąć
// krzywą przejścia wykładniczo; środek przejścia wypada tam, gdzie trzeba, kształt jest odcinkami
// liniowy). Źródło odrzucało gradient z którymkolwiek z tych zapisów.
func parseColorStops(stops [][]Token) ([]pr.ColorStop, error) {
	out := make([]pr.ColorStop, 0, len(stops))
	var hints []int // indeksy w out przystanków wstawionych za wskazówki
	for index, stop := range stops {
		switch len(stop) {
		case 1:
			if position := getLength(stop[0], true, true); !position.IsNone() {
				// wskazówka: nie może stać na początku, na końcu ani obok innej wskazówki
				if index == 0 || index == len(stops)-1 || len(stops[index+1]) == 1 && !getLength(stops[index+1][0], true, true).IsNone() {
					return nil, ErrInvalidValue
				}
				hints = append(hints, len(out))
				out = append(out, pr.ColorStop{Position: position})
				continue
			}
		case 3:
			first, err := parseColorStop(stop[:2])
			if err != nil {
				return nil, err
			}
			second := getLength(stop[2], true, true)
			if second.IsNone() {
				return nil, ErrInvalidValue
			}
			out = append(out, first, pr.ColorStop{Color: first.Color, Position: second})
			continue
		}
		parsed, err := parseColorStop(stop)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	for _, index := range hints {
		if out[index-1].Color.Type == pa.ColorCurrentColor && out[index+1].Color.Type == pa.ColorCurrentColor {
			out[index].Color = out[index-1].Color // przejście między currentcolor a currentcolor
			continue
		}
		// uproszczenie: currentcolor obok wskazówki liczy się tu jak czerń
		before, after := out[index-1].Color.RGBA, out[index+1].Color.RGBA
		if out[index-1].Color.Type == pa.ColorCurrentColor {
			before = pa.RGBA{A: 1}
		}
		if out[index+1].Color.Type == pa.ColorCurrentColor {
			after = pa.RGBA{A: 1}
		}
		// średnia na składowych przemnożonych przez krycie, jak w interpolacji gradientu
		alpha := (before.A + after.A) / 2
		mixed := pa.RGBA{A: alpha}
		if alpha != 0 {
			mixed.R = (before.R*before.A + after.R*after.A) / (2 * alpha)
			mixed.G = (before.G*before.A + after.G*after.A) / (2 * alpha)
			mixed.B = (before.B*before.A + after.B*after.A) / (2 * alpha)
		}
		out[index].Color = pr.Color{Type: pa.ColorRGBA, RGBA: mixed}
	}
	return out, nil
}

func parseColorStop(tokens []Token) (pr.ColorStop, error) {
	switch len(tokens) {
	case 1:
		// gopdfgo: currentcolor zostaje do chwili rysowania, gdzie przyjmuje wartość color pudełka
		// (CSS Color 4 § 6.4, CSS Images 3 § 3.4; test WPT linear-gradient-currentcolor-first-line).
		// Źródło zamieniało je na czerń.
		color := pa.ParseColor(tokens[0])
		if !color.IsNone() {
			return pr.ColorStop{Color: pr.Color(color)}, nil
		}
	case 2:
		color := pa.ParseColor(tokens[0])
		position := getLength(tokens[1], true, true)
		if !color.IsNone() && !position.IsNone() {
			return pr.ColorStop{Color: pr.Color(color), Position: position}, nil
		}
	}
	return pr.ColorStop{}, ErrInvalidValue
}

func parseURLToken(value, baseURL string) (url pr.TaggedString, attr pr.AttrData, err error) {
	if strings.HasPrefix(value, "#") {
		return pr.TaggedString{Tag: pr.Internal, S: utils.Unquote(value[1:])}, attr, nil
	} else {
		var joined string
		joined, err = utils.SafeUrljoin(baseURL, value, false)
		if err != nil {
			return
		}
		return pr.TaggedString{Tag: pr.External, S: joined}, attr, nil
	}
}

func getUrl(_token Token, baseUrl string) (url pr.TaggedString, attr pr.AttrData, err error) {
	switch token := _token.(type) {
	case pa.URL:
		return parseURLToken(token.Value, baseUrl)
	case pa.FunctionBlock:
		// gopdfgo: nazwy funkcji nie rozróżniają wielkości liter (CSS Syntax 3
		// § 4.3.4); źródło porównywało je dosłownie
		if name := utils.AsciiLower(token.Name); name == "attr" {
			attr = checkAttrFunction(token, "url")
			return
		} else if L := len(token.Arguments); name == "url" && (L == 1 || L == 2) {
			val, _ := (token.Arguments)[0].(pa.String)
			return parseURLToken(val.Value, baseUrl)
		}
	}
	return
}

func checkStringOrElementFunction(stringOrElement string, token Token) (out pr.ContentProperty) {
	name, args := pa.ParseFunction(token)
	if name == "" {
		return
	}
	if name == stringOrElement && (len(args) == 1 || len(args) == 2) {
		customIdent_, ok := args[0].(pa.Ident)
		args = args[1:]
		if !ok {
			return
		}
		customIdent := customIdent_.Value

		var ident string
		if len(args) > 0 {
			ident_ := args[0]
			identToken, ok := ident_.(pa.Ident)
			val := utils.AsciiLower(identToken.Value)
			if !ok || (val != "first" && val != "start" && val != "last" && val != "first-except") {
				return
			}
			ident = val
		} else {
			ident = "first"
		}
		return pr.ContentProperty{Type: stringOrElement + "()", Content: pr.Strings{string(customIdent), ident}}
	}
	return
}

// HasVar returns true if [token] is a var(...),
// or is a function with any var()
func HasVar(token Token) bool {
	name, args := pa.ParseFunction(token)
	if name == "" {
		return false
	}
	if name == "var" && len(args) != 0 {
		// TODO: we should check authorized tokens
		// https://drafts.csswg.org/css-syntax-3/#typedef-declaration-value
		ident, ok := args[0].(pa.Ident)
		return ok && strings.HasPrefix(ident.Value, "--")
	}

	// recurse
	for _, arg := range args {
		if HasVar(arg) {
			return true
		}
	}
	return false
}

func checkAttrFunction(token pa.FunctionBlock, allowedType string) (out pr.AttrData) {
	name, args := pa.ParseFunction(token)
	if name == "" {
		return
	}
	la := len(args)
	if name == "attr" && (la == 1 || la == 2 || la == 3) {
		ident, ok := args[0].(pa.Ident)
		if !ok {
			return
		}
		attrName := ident.Value
		var (
			typeOrUnit string
			fallback   pr.CssProperty
		)
		if la == 1 {
			typeOrUnit = "string"
		} else {
			ident2, ok := args[1].(pa.Ident)
			if !ok {
				return
			}
			typeOrUnit = string(ident2.Value)
			fb, isIN := attrFallbacks[typeOrUnit]
			if !isIN {
				return
			}
			if la < 3 {
				fallback = fb
			} else {
				switch fbValue := args[2].(type) {
				case pa.String:
					fallback = pr.String(fbValue.Value)
				default:
					// TODO: handle other fallback types
					return
				}
			}
		}
		if allowedType == "" || allowedType == typeOrUnit {
			return pr.AttrData{Name: string(attrName), TypeOrUnit: typeOrUnit, Fallback: fallback}
		}
	}
	return
}

// Parse background-position and object-position.
//
// See http://drafts.csswg.org/csswg/css-backgrounds-3/#the-background-position
// https://drafts.csswg.org/css-images-3/#propdef-object-position
func parsePosition(tokens []Token) pr.CenterPos {
	center := parse2dPosition(tokens)
	if !center.IsNone() {
		return pr.CenterPos{
			OriginX: pr.Left,
			OriginY: pr.Top,
			Pos:     center,
		}
	}

	if len(tokens) == 4 {
		keyword1 := getKeywordT(tokens[0])
		keyword2 := getKeywordT(tokens[2])
		length1 := getLength(tokens[1], true, true)
		length2 := getLength(tokens[3], true, true)
		if !length1.IsNone() && !length2.IsNone() {
			if (keyword1 == pr.Left || keyword1 == pr.Right) && (keyword2 == pr.Top || keyword2 == pr.Bottom) {
				return pr.CenterPos{
					OriginX: keyword1,
					OriginY: keyword2,
					Pos:     pr.Point{length1, length2},
				}
			}
			if (keyword2 == pr.Left || keyword2 == pr.Right) && (keyword1 == pr.Top || keyword1 == pr.Bottom) {
				return pr.CenterPos{
					OriginX: keyword2,
					OriginY: keyword1,
					Pos:     pr.Point{length2, length1},
				}
			}
		}
	}

	if len(tokens) == 3 {
		length := getLength(tokens[2], true, true)
		var keyword, otherKeyword pr.Keyword
		if !length.IsNone() {
			keyword = getKeywordT(tokens[1])
			otherKeyword = getKeywordT(tokens[0])
		} else {
			length = getLength(tokens[1], true, true)
			otherKeyword = getKeywordT(tokens[2])
			keyword = getKeywordT(tokens[0])
		}

		if !length.IsNone() {
			switch otherKeyword {
			case pr.Center:
				switch keyword {
				case pr.Top, pr.Bottom:
					return pr.CenterPos{OriginX: pr.Left, OriginY: keyword, Pos: pr.Point{fiftyPercent, length}}
				case pr.Left, pr.Right:
					return pr.CenterPos{OriginX: keyword, OriginY: pr.Top, Pos: pr.Point{length, fiftyPercent}}
				}
			case pr.Top, pr.Bottom:
				if keyword == pr.Left || keyword == pr.Right {
					return pr.CenterPos{OriginX: keyword, OriginY: otherKeyword, Pos: pr.Point{length, zeroPercent}}
				}
			case pr.Left, pr.Right:
				if keyword == pr.Top || keyword == pr.Bottom {
					return pr.CenterPos{OriginX: otherKeyword, OriginY: keyword, Pos: pr.Point{zeroPercent, length}}
				}
			}
		}
	}
	return pr.CenterPos{}
}

// Parse a <string> token.
func getString(_token Token) (out pr.ContentProperty) {
	switch token := _token.(type) {
	case pa.String:
		return pr.ContentProperty{Type: "string", Content: pr.String(token.Value)}
	case pa.FunctionBlock:
		switch token.Name {
		case "attr":
			attr := checkAttrFunction(token, "string")
			if attr.IsNone() {
				return
			}
			return pr.ContentProperty{Type: "attr()", Content: attr}
		case "counter", "counters":
			return checkCounterFunction(token)
		case "content":
			return checkContentFunction(token)
		case "string":
			return checkStringOrElementFunction("string", token)
		}
	}
	return
}

func checkCounterFunction(token Token) (prop pr.ContentProperty) {
	name, args := pa.ParseFunction(token)
	if name == "" {
		return
	}
	var out pr.Counters
	LA := len(args)
	if (name == "counter" && (LA == 1 || LA == 2)) || (name == "counters" && (LA == 2 || LA == 3)) {
		ident, ok := args[0].(pa.Ident)
		args = args[1:]
		if !ok {
			return
		}
		out.Name = string(ident.Value)
		if isReservedCounterName(out.Name) {
			return
		}

		if name == "counters" {
			str, ok := args[0].(pa.String)
			args = args[1:]
			if !ok {
				return
			}
			out.Separator = str.Value
		}

		if len(args) > 0 {
			// gopdfgo: styl w counter() i counters() to nazwa stylu albo symbols(), nie łańcuch znaków
			// (CSS Lists 3 § 4.7); łańcuch jest poprawny tylko w list-style-type. Źródło go przyjmowało.
			if _, isString := args[0].(pa.String); isString {
				return
			}
			counterStyle, ok := listStyleType_(args[0:1])
			if !ok {
				return
			}
			out.Style = counterStyle
		} else {
			out.Style.Name = "decimal"
		}

		return pr.ContentProperty{Type: fmt.Sprintf("%s()", name), Content: out}
	}
	return
}

func checkContentFunction(token Token) (out pr.ContentProperty) {
	name, args := pa.ParseFunction(token)
	if name == "" {
		return
	}
	if name == "content" {
		if len(args) == 0 {
			return pr.ContentProperty{Type: "content()", Content: pr.String("text")}
		} else if len(args) == 1 {
			ident, ok := args[0].(pa.Ident)
			v := utils.AsciiLower(ident.Value)
			if ok && (v == "text" || v == "before" || v == "after" || v == "first-letter" || v == "marker") {
				return pr.ContentProperty{Type: "content()", Content: pr.String(v)}
			}
		}
	}
	return
}

// Parse a <quote> token.
func getQuote(token Token) (pr.Quote, bool) {
	keyword := getKeyword(token)
	out, ok := contentQuoteKeywords[keyword]
	return out, ok
}

// Parse a <target> token.
func getTarget(token Token, baseUrl string) (out pr.ContentProperty, err error) {
	name, args := pa.ParseFunction(token)
	if name == "" {
		return
	}
	args = splitOnOptionalComma(args)
	la := len(args)
	if la == 0 {
		return
	}
	switch name {
	case "target-counter":
		if la != 2 && la != 3 {
			return
		}
	case "target-counters":
		if la != 3 && la != 4 {
			return
		}
	case "target-text":
		if la != 1 && la != 2 {
			return
		}
	default:
		return
	}

	var (
		values pr.SContentProps
		value  pr.SContentProp
	)

	link := args[0]
	args = args[1:]
	stringLink := getString(link)
	if stringLink.IsNone() {
		ur, attr, err := getUrl(link, baseUrl)
		if err != nil {
			return out, err
		}
		if !ur.IsNone() {
			value.ContentProperty = pr.ContentProperty{Type: "url", Content: ur}
		} else if !attr.IsNone() {
			value.ContentProperty = pr.ContentProperty{Type: "attr()", Content: attr}
		} else {
			return out, nil
		}
		values = append(values, value)
	} else {
		values = append(values, pr.SContentProp{ContentProperty: stringLink})
	}

	if strings.HasPrefix(name, "target-counter") {
		if len(args) == 0 {
			return
		}

		ident_ := args[0]
		args = args[1:]
		ident, ok := ident_.(pa.Ident)
		if !ok {
			return
		}
		values = append(values, pr.SContentProp{String: string(ident.Value)})

		if name == "target-counters" {
			string_ := getString(args[0])
			args = args[1:]
			if string_.IsNone() {
				return
			}
			values = append(values, pr.SContentProp{ContentProperty: string_})
		}

		var counterStyle string
		if len(args) > 0 {
			counterStyle = getKeyword(args[0])
		} else {
			counterStyle = "decimal"
		}
		values = append(values, pr.SContentProp{String: counterStyle})
	} else {
		var content string
		if len(args) > 0 {
			content = getKeyword(args[0])
			if content != "content" && content != "before" && content != "after" && content != "first-letter" {
				return
			}
		} else {
			content = "content"
		}
		values = append(values, pr.SContentProp{String: content})
	}
	return pr.ContentProperty{Type: fmt.Sprintf("%s()", name), Content: values}, nil
}

// Parse <content-list> tokens.
func getContentList(tokens []Token, baseUrl string) (out pr.ContentProperties, err error) {
	// See https://www.w3.org/TR/css-content-3/#typedef-content-list
	parsedTokens := make([]pr.ContentProperty, len(tokens))
	for i, token := range tokens {
		parsedTokens[i], err = getContentListToken(token, baseUrl)
		if err != nil {
			return nil, err
		}
		if parsedTokens[i].IsNone() {
			return nil, nil
		}
	}
	return parsedTokens, nil
}

// Parse one of the <content-list> tokens.
func getContentListToken(token Token, baseUrl string) (pr.ContentProperty, error) {
	// See https://www.w3.org/TR/css-content-3/#typedef-content-list

	// <string>
	string_ := getString(token)
	if !string_.IsNone() {
		return string_, nil
	}

	// contents
	if getKeyword(token) == "contents" {
		return pr.ContentProperty{Type: "content()", Content: pr.String("text")}, nil
	}

	// <uri>
	url, attr, err := getUrl(token, baseUrl)
	if err != nil {
		return pr.ContentProperty{}, err
	}
	if !url.IsNone() {
		return pr.ContentProperty{Type: "url", Content: url}, nil
	} else if !attr.IsNone() {
		return pr.ContentProperty{Type: "attr()", Content: attr}, nil
	}

	// <quote>
	quote, ok := getQuote(token)
	if ok {
		return pr.ContentProperty{Type: "quote", Content: quote}, nil
	}

	// <target>
	target, err := getTarget(token, baseUrl)
	if err != nil || !target.IsNone() {
		return target, err
	}

	// <leader>
	name, args := pa.ParseFunction(token)
	if name == "" {
		return pr.ContentProperty{}, nil
	}
	if name == "leader" {
		if len(args) != 1 {
			return pr.ContentProperty{}, nil
		}
		arg_ := args[0]
		var str string
		switch arg := arg_.(type) {
		case pa.Ident:
			switch arg.Value {
			case "dotted":
				str = "."
			case "solid":
				str = "_"
			case "space":
				str = " "
			default:
				return pr.ContentProperty{}, nil
			}
		case pa.String:
			str = arg.Value
		}
		return pr.ContentProperty{Type: "leader()", Content: pr.Strings{"string", str}}, nil
	} else if name == "element" { // <element>
		return checkStringOrElementFunction("element", token), nil
	}
	return pr.ContentProperty{}, nil
}

func ParseCounterStyleName(tokens []pa.Token, cs counters.CounterStyle) string {
	tokens = pa.RemoveWhitespace(tokens)
	if len(tokens) != 1 {
		return ""
	}

	token := tokens[0]
	if ident, ok := token.(pa.Ident); ok {
		if v := utils.AsciiLower(ident.Value); v == "decimal" || v == "disc" {
			if _, ok := cs[v]; !ok {
				return ident.Value
			}
		} else if utils.AsciiLower(ident.Value) != "none" {
			return ident.Value
		}
	}

	return ""
}
