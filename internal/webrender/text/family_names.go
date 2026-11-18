package text

import (
	"bytes"
	"encoding/binary"
	"strings"
	"unicode/utf16"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
)

// gopdfgo: dopasowanie nazw rodzin według CSS Fonts 4 § 5.1 (font-family) i § 5.2 (font matching).
//
// Biblioteka go-text porównuje rodziny po usunięciu wszystkich spacji i zmianie liter na małe
// (font.NormalizeFamily) i zna tylko jedną nazwę rodziny każdego fontu. CSS porównuje nazwy bez
// rozróżniania wielkości liter ASCII, ale spacje są znaczące ("CSSTest   FamilyName" w cudzysłowie
// nie pasuje do fontu "CSSTest FamilyName"), a font pasuje pod każdą swoją nazwą rodziny z tablicy
// name, także zlokalizowaną (font-family-name-007, -008, -010). Ten plik buduje indeks nazw fontów
// podanych do renderowania (RegisterFontContent) i rodzin z reguł @font-face, a newQuery przez
// exactFamilies podmienia nazwę na tę, którą zna go-text, albo pomija nazwę, która pasowałaby
// tylko dzięki normalizacji biblioteki. Gdy indeks jest pusty (fonty systemowe w testach
// przejętych), lista rodzin przechodzi bez zmian.

// familyIndex odwzorowuje nazwę rodziny w postaci cssFamilyKey na nazwę rozpoznawaną przez go-text.
type familyIndex struct {
	built     bool
	installed map[string]string   // nazwy z tablic name fontów podanych do renderowania
	faces     map[string]string   // rodziny z reguł @font-face
	known     map[string]struct{} // nazwy znormalizowane przez go-text (font.NormalizeFamily)
	cache     map[string]string   // wynik resolve dla nazwy z CSS; "" = pominąć
	fontIDs   []string            // identyfikatory fontów podanych do renderowania
}

// cssFamilyKey zamienia wielkie litery ASCII na małe i nic więcej (CSS Fonts 4 § 5.1).
func cssFamilyKey(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isGenericFamilyKeyword(key string) bool {
	switch key {
	case "serif", "sans-serif", "monospace", "cursive", "fantasy", "math", "emoji", "system-ui",
		"ui-serif", "ui-sans-serif", "ui-monospace", "ui-rounded", "fangsong":
		return true
	}
	return false
}

func (fi *familyIndex) addFace(family string) {
	if fi.faces == nil {
		fi.faces = map[string]string{}
		fi.known = map[string]struct{}{}
	}
	fi.faces[cssFamilyKey(family)] = family
	if fi.known == nil {
		fi.known = map[string]struct{}{}
	}
	fi.known[font.NormalizeFamily(family)] = struct{}{}
	fi.cache = nil
}

func (fi *familyIndex) build(contents map[string][]byte) {
	fi.built = true
	if len(fi.fontIDs) == 0 {
		return
	}
	fi.installed = map[string]string{}
	if fi.known == nil {
		fi.known = map[string]struct{}{}
	}
	var buffer []byte
	for _, id := range fi.fontIDs {
		lds, err := opentype.NewLoaders(bytes.NewReader(contents[id]))
		if err != nil {
			continue
		}
		for _, ld := range lds {
			var desc font.Description
			desc, buffer = font.Describe(ld, buffer)
			if desc.Family == "" {
				continue
			}
			fi.known[font.NormalizeFamily(desc.Family)] = struct{}{}
			if _, has := fi.installed[cssFamilyKey(desc.Family)]; !has {
				fi.installed[cssFamilyKey(desc.Family)] = desc.Family
			}
			raw, err := ld.RawTable(opentype.MustNewTag("name"))
			if err != nil {
				continue
			}
			for _, name := range familyNameRecords(raw) {
				if _, has := fi.installed[cssFamilyKey(name)]; !has {
					fi.installed[cssFamilyKey(name)] = desc.Family
				}
			}
		}
	}
}

// resolve zwraca nazwę do przekazania go-text, albo "", gdy nazwa pasowałaby tylko po normalizacji
// biblioteki.
func (fi *familyIndex) resolve(family string) string {
	if out, has := fi.cache[family]; has {
		return out
	}
	key := cssFamilyKey(family)
	out := family
	if isGenericFamilyKeyword(key) {
		// rodziny generyczne bez zmian
	} else if name, has := fi.faces[key]; has {
		out = name
	} else if name, has := fi.installed[key]; has {
		out = name
	} else if _, has := fi.known[font.NormalizeFamily(family)]; has {
		out = ""
	}
	if fi.cache == nil {
		fi.cache = map[string]string{}
	}
	fi.cache[family] = out
	return out
}

// exactFamilies stosuje resolve do listy rodzin; zwraca tę samą listę, gdy nic się nie zmienia.
func (fc *FontConfigurationGotext) exactFamilies(families []string) []string {
	fi := &fc.familyIndex
	if !fi.built {
		fi.build(fc.fontsContent)
	}
	if len(fi.installed) == 0 && len(fi.faces) == 0 {
		return families
	}
	for i, family := range families {
		if fi.resolve(family) == family {
			continue
		}
		out := append([]string(nil), families[:i]...)
		for _, family := range families[i:] {
			if name := fi.resolve(family); name != "" {
				out = append(out, name)
			}
		}
		return out
	}
	return families
}

// familyNameRecords zwraca wszystkie nazwy rodziny (identyfikatory 1, 16 i 21, wszystkie platformy
// i języki) z surowej tablicy name.
func familyNameRecords(table []byte) []string {
	if len(table) < 6 {
		return nil
	}
	count := int(binary.BigEndian.Uint16(table[2:]))
	storage := int(binary.BigEndian.Uint16(table[4:]))
	var out []string
	for i := 0; i < count; i++ {
		rec := 6 + 12*i
		if rec+12 > len(table) {
			break
		}
		platform := binary.BigEndian.Uint16(table[rec:])
		nameID := binary.BigEndian.Uint16(table[rec+6:])
		length := int(binary.BigEndian.Uint16(table[rec+8:]))
		offset := int(binary.BigEndian.Uint16(table[rec+10:]))
		if nameID != 1 && nameID != 16 && nameID != 21 {
			continue
		}
		start := storage + offset
		if start+length > len(table) {
			continue
		}
		value := table[start : start+length]
		switch platform {
		case 0, 3: // UTF-16BE
			units := make([]uint16, len(value)/2)
			for j := range units {
				units[j] = binary.BigEndian.Uint16(value[2*j:])
			}
			out = append(out, string(utf16.Decode(units)))
		case 1: // Mac Roman: tylko nazwy ASCII
			ascii := true
			for _, c := range value {
				if c >= 0x80 {
					ascii = false
					break
				}
			}
			if ascii {
				out = append(out, string(value))
			}
		}
	}
	return out
}
