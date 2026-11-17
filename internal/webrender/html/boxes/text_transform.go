package boxes

import (
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

// greekUpper (gopdfgo) zamienia tekst grecki na wielkie litery według reguł języka greckiego, tak
// jak ICU (używane przez Chromium) w trybie "el":
//   - wielkie litery tracą akcenty (tonos, varia, perispomeni) i przydechy, a zachowują dialytikę;
//   - samogłoska z akcentem przed ι lub υ bez znaków diakrytycznych (dyftong rozdzielony akcentem)
//     przekazuje następnej literze dialytikę: Νεράιδα -> ΝΕΡΑΪΔΑ;
//   - samodzielne ή (spójnik „lub”) zachowuje tonos: ή -> Ή.
//
// CSS Text 3 § 2.1 odsyła do odwzorowań zależnych od języka. Znaki spoza alfabetu greckiego
// przechodzą przez zwykłe odwzorowanie Unicode.
func greekUpper(text string) string {
	runes := []rune(norm.NFD.String(text))
	isMark := func(r rune) bool { return unicode.Is(unicode.Mn, r) }
	type cluster struct {
		base  rune
		marks []rune
	}
	var clusters []cluster
	for i := 0; i < len(runes); {
		c := cluster{base: runes[i]}
		i++
		for i < len(runes) && isMark(runes[i]) {
			c.marks = append(c.marks, runes[i])
			i++
		}
		clusters = append(clusters, c)
	}
	isLetter := func(i int) bool {
		return i >= 0 && i < len(clusters) && unicode.IsLetter(clusters[i].base)
	}
	upper := cases.Upper(language.Und)
	var out strings.Builder
	addDialytika := false
	for i, c := range clusters {
		if !unicode.Is(unicode.Greek, c.base) || !unicode.IsLetter(c.base) {
			out.WriteString(upper.String(string(c.base) + string(c.marks)))
			addDialytika = false
			continue
		}
		var accent, dialytika, iotaSubscript bool
		var other []rune
		for _, m := range c.marks {
			switch m {
			case '́', '̀', '͂': // tonos (oxia), varia, perispomeni
				accent = true
			case '̈':
				dialytika = true
			case 'ͅ':
				iotaSubscript = true
			case '̓', '̔', '̆', '̄': // psili, dasia, vrachy, macron
			default:
				other = append(other, m)
			}
		}
		lower := unicode.ToLower(c.base)
		out.WriteRune(unicode.ToUpper(c.base))
		if dialytika || (addDialytika && len(c.marks) == 0) {
			out.WriteRune('̈')
		}
		if lower == 'η' && accent && !dialytika && !isLetter(i-1) && !isLetter(i+1) {
			out.WriteRune('́') // samodzielne ή
		}
		out.WriteString(string(other))
		if iotaSubscript {
			out.WriteRune('Ι')
		}
		// samogłoska z akcentem przed ι albo υ: dyftong rozdzielony, następna litera dostaje dialytikę
		addDialytika = false
		if accent && !dialytika && i+1 < len(clusters) && len(clusters[i+1].marks) == 0 {
			next := clusters[i+1].base
			switch lower {
			case 'α', 'ε', 'η', 'ο', 'υ':
				addDialytika = next == 'ι' || (next == 'υ' && lower != 'υ')
			}
		}
	}
	return norm.NFC.String(out.String())
}

// mathItalic (gopdfgo) zwraca znak z bloku Mathematical Alphanumeric Symbols odpowiadający literze
// w kursywie matematycznej (MathML Core, #italic-mappings, tabela italic); inne znaki zwraca bez zmian.
func mathItalic(r rune) rune {
	switch {
	case r == 'h':
		return 0x210E
	case r >= 'A' && r <= 'Z':
		return 0x1D434 + r - 'A'
	case r >= 'a' && r <= 'z':
		return 0x1D44E + r - 'a'
	case r == 0x131:
		return 0x1D6A4
	case r == 0x237:
		return 0x1D6A5
	case r >= 0x391 && r <= 0x3A1:
		return 0x1D6E2 + r - 0x391
	case r == 0x3F4:
		return 0x1D6F3
	case r >= 0x3A3 && r <= 0x3A9:
		return 0x1D6F4 + r - 0x3A3
	case r == 0x2207:
		return 0x1D6FB
	case r >= 0x3B1 && r <= 0x3C9:
		return 0x1D6FC + r - 0x3B1
	}
	switch r {
	case 0x2202:
		return 0x1D715
	case 0x3F5:
		return 0x1D716
	case 0x3D1:
		return 0x1D717
	case 0x3F0:
		return 0x1D718
	case 0x3D5:
		return 0x1D719
	case 0x3F1:
		return 0x1D71A
	case 0x3D6:
		return 0x1D71B
	}
	return r
}
