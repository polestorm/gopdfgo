package tree

import (
	"fmt"
	"strconv"
	"strings"
)

// svgTransformToCSS (gopdfgo) przepisuje atrybut transform elementu SVG (składnia SVG 1.1 § 7.6:
// liczby bez jednostek, kąty w stopniach, rotate z opcjonalnym środkiem) na wartość właściwości CSS
// transform. SVG 2 § 8.5 i CSS Transforms 1 § 12 czynią z atrybutu atrybut prezentacyjny tej
// właściwości, więc na zewnętrznym <svg> osadzonym w HTML działa on jak transform pola CSS.
// Zwraca false dla wartości niepoprawnej (atrybut jest wtedy pomijany).
func svgTransformToCSS(attr string) (string, bool) {
	s := strings.TrimSpace(attr)
	if s == "" {
		return "", false
	}
	num := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	var out []string
	for s != "" {
		open := strings.IndexByte(s, '(')
		if open < 0 {
			return "", false
		}
		name := strings.TrimSpace(s[:open])
		closing := strings.IndexByte(s[open:], ')')
		if closing < 0 {
			return "", false
		}
		closing += open
		fields := strings.FieldsFunc(s[open+1:closing], func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		})
		args := make([]float64, len(fields))
		for i, f := range fields {
			v, err := strconv.ParseFloat(f, 64)
			if err != nil {
				return "", false
			}
			args[i] = v
		}
		switch {
		case name == "matrix" && len(args) == 6:
			out = append(out, fmt.Sprintf("matrix(%s,%s,%s,%s,%s,%s)",
				num(args[0]), num(args[1]), num(args[2]), num(args[3]), num(args[4]), num(args[5])))
		case name == "translate" && len(args) == 1:
			out = append(out, fmt.Sprintf("translate(%spx)", num(args[0])))
		case name == "translate" && len(args) == 2:
			out = append(out, fmt.Sprintf("translate(%spx,%spx)", num(args[0]), num(args[1])))
		case name == "scale" && len(args) == 1:
			out = append(out, fmt.Sprintf("scale(%s)", num(args[0])))
		case name == "scale" && len(args) == 2:
			out = append(out, fmt.Sprintf("scale(%s,%s)", num(args[0]), num(args[1])))
		case name == "rotate" && len(args) == 1:
			out = append(out, fmt.Sprintf("rotate(%sdeg)", num(args[0])))
		case name == "rotate" && len(args) == 3:
			out = append(out, fmt.Sprintf("translate(%spx,%spx) rotate(%sdeg) translate(%spx,%spx)",
				num(args[1]), num(args[2]), num(args[0]), num(-args[1]), num(-args[2])))
		case (name == "skewX" || name == "skewY") && len(args) == 1:
			out = append(out, fmt.Sprintf("%s(%sdeg)", name, num(args[0])))
		default:
			return "", false
		}
		s = strings.TrimLeft(s[closing+1:], " ,\t\r\n")
	}
	return strings.Join(out, " "), true
}
