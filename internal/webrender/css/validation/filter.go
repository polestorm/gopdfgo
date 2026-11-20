package validation

// Plik dodany w gopdfgo, nie pochodzi ze źródła webrender.

import (
	pa "github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// gopdfgo: “filter“ (Filter Effects 1 § 5, § 13.1): none albo lista funkcji filtra rozdzielonych
// odstępami, w kolejności stosowania. Funkcje barwne przyjmują liczbę albo procent (domyślnie 1, a dla
// hue-rotate() kąt, domyślnie 0); wartości ujemne są błędem składni, a grayscale(), invert(), opacity()
// i sepia() powyżej 100% obcinamy do 1 (§ 13.1: „UAs must clamp the values to 1”). blur() przyjmuje
// nieujemną długość (domyślnie 0), drop-shadow() kolor i dwie albo trzy długości jak box-shadow, bez
// rozszerzenia i inset. url() zapamiętujemy, ale rysowanie go pomija (zob. document/draw_filter.go).
// Źródło tej właściwości nie znało.
func filter(tokens []Token, baseUrl string) pr.CssProperty {
	tokens = pa.RemoveWhitespace(tokens)
	if getSingleKeyword(tokens) == "none" {
		return pr.Filters{}
	}
	if len(tokens) == 0 {
		return nil
	}
	var out pr.Filters
	for _, token := range tokens {
		if _, isURL := token.(pa.URL); isURL {
			url, _, err := getUrl(token, baseUrl)
			if err != nil {
				return nil
			}
			out = append(out, pr.FilterFunction{Name: "url", URL: url.S})
			continue
		}
		fn, ok := token.(pa.FunctionBlock)
		if !ok {
			return nil
		}
		name := utils.AsciiLower(fn.Name)
		if name == "url" {
			url, _, err := getUrl(token, baseUrl)
			if err != nil {
				return nil
			}
			out = append(out, pr.FilterFunction{Name: "url", URL: url.S})
			continue
		}
		f, ok := filterFunction(name, pa.RemoveWhitespace(fn.Arguments))
		if !ok {
			return nil
		}
		out = append(out, f)
	}
	return out
}

func filterFunction(name string, args []Token) (pr.FilterFunction, bool) {
	out := pr.FilterFunction{Name: name}
	switch name {
	case "brightness", "contrast", "grayscale", "invert", "opacity", "saturate", "sepia":
		out.Amount = 1
		if len(args) == 0 {
			return out, true
		}
		if len(args) != 1 {
			return out, false
		}
		var amount pr.Fl
		switch t := args[0].(type) {
		case pa.Number:
			amount = t.ValueF
		case pa.Percentage:
			amount = t.ValueF / 100
		default:
			return out, false
		}
		if amount < 0 {
			return out, false
		}
		switch name {
		case "grayscale", "invert", "opacity", "sepia":
			amount = min(amount, 1)
		}
		out.Amount = amount
		return out, true
	case "hue-rotate":
		if len(args) == 0 {
			return out, true
		}
		if len(args) != 1 {
			return out, false
		}
		// <angle> albo <zero>
		if n, ok := args[0].(pa.Number); ok && n.ValueF == 0 {
			return out, true
		}
		angle, ok := getAngle(args[0])
		if !ok {
			return out, false
		}
		out.Amount = angle
		return out, true
	case "blur":
		out.Length = pr.Dimension{Unit: pr.Px}
		if len(args) == 0 {
			return out, true
		}
		if len(args) != 1 {
			return out, false
		}
		length := getLength(args[0], false, false)
		if length.IsNone() {
			return out, false
		}
		out.Length = length
		return out, true
	case "drop-shadow":
		shadow := pr.BoxShadow{Color: pr.Color{Type: pa.ColorCurrentColor}}
		var lengths []pr.Dimension
		hasColor, lengthsDone := false, false
		for _, token := range args {
			if length := getLength(token, true, false); !length.IsNone() {
				if lengthsDone || len(lengths) == 3 {
					return out, false
				}
				lengths = append(lengths, length)
				continue
			}
			color := pa.ParseColor(token)
			if color.IsNone() || hasColor {
				return out, false
			}
			shadow.Color, hasColor = pr.Color(color), true
			lengthsDone = len(lengths) != 0
		}
		if len(lengths) < 2 {
			return out, false
		}
		shadow.X, shadow.Y = lengths[0], lengths[1]
		shadow.Blur = pr.Dimension{Unit: pr.Px}
		if len(lengths) == 3 {
			if lengths[2].Calc == nil && lengths[2].Value < 0 {
				return out, false
			}
			shadow.Blur = lengths[2]
		}
		out.Shadow = shadow
		return out, true
	}
	return out, false
}
