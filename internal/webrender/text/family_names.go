package text

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"unicode/utf16"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
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

	// style krojów z reguł @font-face (zob. matchingStyle, Synthesis): zbiór stylów rodziny
	// (bity styleBit) i styl CSS każdej twarzy, bo go-text nie odróżnia italic od oblique
	faceStyles map[string]uint8
	fontStyles map[*font.Font]FontStyle
	// fontWeights: grubość twarzy z reguły @font-face dla syntezy pogrubienia: górna granica zakresu
	// z deskryptora albo, dla auto i deskryptora pominiętego lub nieważnego (wartość początkowa auto,
	// CSS Fonts 4 § 4.4), grubość zapisana w foncie
	fontWeights map[*font.Font]uint16
	// fontAxes: osie zmienności fontu ważne dla syntezy (bity axisWeight, axisSlant)
	fontAxes map[*font.Font]uint8
	// fixedAxes: osie przypięte jedną wartością deskryptora reguły @font-face
	fixedAxes map[*font.Font]uint8
}

const (
	axisWeight uint8 = 1 << iota // 'wght'
	axisSlant                    // 'slnt' albo 'ital'
)

// variationAxes czyta znaczniki osi z tablicy fvar fontu (OpenType, tablica fvar).
func (fc *FontConfigurationGotext) variationAxes(ft *font.Font) uint8 {
	fi := &fc.familyIndex
	if axes, has := fi.fontAxes[ft]; has {
		return axes
	}
	var axes uint8
	if content := fc.FontContent(fc.FontLocation(ft)); len(content) != 0 {
		if ld, err := opentype.NewLoader(bytes.NewReader(content)); err == nil {
			if raw, err := ld.RawTable(opentype.MustNewTag("fvar")); err == nil && len(raw) >= 16 {
				offset := int(binary.BigEndian.Uint16(raw[4:]))
				count := int(binary.BigEndian.Uint16(raw[8:]))
				size := int(binary.BigEndian.Uint16(raw[10:]))
				for i := 0; i < count && offset+i*size+4 <= len(raw); i++ {
					switch string(raw[offset+i*size : offset+i*size+4]) {
					case "wght":
						axes |= axisWeight
					case "slnt", "ital":
						axes |= axisSlant
					}
				}
			}
		}
	}
	if fi.fontAxes == nil {
		fi.fontAxes = map[*font.Font]uint8{}
	}
	fi.fontAxes[ft] = axes
	return axes
}

func styleBit(style FontStyle) uint8 { return 1 << style }

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

func (fi *familyIndex) addFace(family string, style FontStyle, weight uint16, fixed uint8, ft *font.Font) {
	if fi.fixedAxes == nil {
		fi.fixedAxes = map[*font.Font]uint8{}
	}
	fi.fixedAxes[ft] = fixed
	if fi.faces == nil {
		fi.faces = map[string]string{}
		fi.faceStyles = map[string]uint8{}
		fi.fontStyles = map[*font.Font]FontStyle{}
		fi.fontWeights = map[*font.Font]uint16{}
	}
	fi.fontWeights[ft] = weight
	fi.faces[cssFamilyKey(family)] = family
	fi.faceStyles[cssFamilyKey(family)] |= styleBit(style)
	fi.fontStyles[ft] = style
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

// matchingStyle (gopdfgo) wybiera styl zapytania do go-text według CSS Fonts 4 § 5.2, krok 4b:
// dla italic kolejno twarze italic, oblique, normal; dla oblique kolejno oblique, normal (twarze
// italic dopiero na końcu). go-text łączy italic i oblique w jeden styl, więc gdy rodzina z reguł
// @font-face nie ma twarzy, które CSS wybrałby przed normalnymi, zapytanie prosi o styl normalny.
// font-synthesis-style: oblique-only wyłącza zastępowanie italic przez oblique (CSS Fonts 5 § 7.3).
// Decyduje pierwsza rodzina z listy zdefiniowana regułami @font-face.
func (fc *FontConfigurationGotext) matchingStyle(fd FontDescription) FontStyle {
	if fd.Style == FSty_Normal || len(fc.familyIndex.faceStyles) == 0 {
		return fd.Style
	}
	for _, family := range fd.Family {
		set, has := fc.familyIndex.faceStyles[cssFamilyKey(family)]
		if !has {
			continue
		}
		normal := set&styleBit(FSty_Normal) != 0
		italic := set&styleBit(FSty_Italic) != 0
		oblique := set&styleBit(FSty_Oblique) != 0
		switch fd.Style {
		case FSty_Italic:
			if !italic && (!oblique || fd.Synthesis&SynthesisObliqueOnly != 0) && normal {
				return FSty_Normal
			}
		case FSty_Oblique:
			if !oblique && normal {
				return FSty_Normal
			}
		}
		return fd.Style
	}
	return fd.Style
}

// Synthesis (gopdfgo) mówi, czy tekst złożony krojem face trzeba pochylić albo pogrubić
// syntetycznie (CSS Fonts 4 § 7.2, 7.3): pochylenie, gdy żądano italic albo oblique, a krój jest
// prosty; pogrubienie, gdy żądano grubości co najmniej 600, a krój ma mniejszą (jak Chromium).
//
// skew to przesunięcie poziome na jednostkę wysokości, tangens kąta pochylenia (FontDescription.ObliqueAngle).
func (fc *FontConfigurationGotext) Synthesis(face *font.Face, fd FontDescription) (skew pr.Fl, bold bool) {
	if face == nil {
		return 0, false
	}
	var oblique bool
	_, aspect := fc.fm.FontMetadata(face.Font)
	faceStyle, has := fc.familyIndex.fontStyles[face.Font]
	if !has {
		faceStyle = FSty_Normal
		if aspect.Style == font.StyleItalic {
			faceStyle = FSty_Italic
		}
	}
	if faceStyle == FSty_Normal {
		switch fd.Style {
		case FSty_Italic:
			oblique = fd.Synthesis&SynthesisStyle != 0
		case FSty_Oblique:
			oblique = fd.Synthesis&(SynthesisStyle|SynthesisObliqueOnly) != 0
		}
	}
	faceWeight, has := fc.familyIndex.fontWeights[face.Font]
	if !has {
		faceWeight = uint16(aspect.Weight)
	}
	bold = fd.Synthesis&SynthesisWeight != 0 && fd.Weight >= 600 && faceWeight != 0 && faceWeight < 600
	// font zmienny z osią grubości albo pochylenia nie jest syntetyzowany w tej osi, także poza jej
	// zakresem (jak Chromium; testy WPT synthetic-bold/oblique-out-of-capabilities-range)
	// Deskryptor @font-face z jedną wartością stylu albo grubości przypina oś, więc synteza działa
	// (testy WPT variations/font-slant-3, font-weight-3).
	if oblique || bold {
		axes := fc.variationAxes(face.Font) &^ fc.familyIndex.fixedAxes[face.Font]
		oblique = oblique && axes&axisSlant == 0
		bold = bold && axes&axisWeight == 0
	}
	if oblique {
		skew = pr.Fl(math.Tan(float64(fd.ObliqueAngle) * math.Pi / 180))
	}
	return skew, bold
}

// FallbackLineExtents (gopdfgo) zwraca, ile nad linią bazową i pod nią sięgają w ułożonym tekście
// fonty inne niż pierwszy dostępny, każdy z połową własnego odstępu międzywierszowego. Przy
// line-height: normal wysokość wiersza obejmuje metryki wszystkich użytych fontów (CSS Inline 3
// § 4.2: „normal”, tak robi Chromium; test WPT font-face-unicode-range-2). ok jest fałszywe, gdy
// tekst złożono tylko pierwszym dostępnym fontem.
func FallbackLineExtents(context TextLayoutContext, layout EngineLayout) (above, below pr.Float, ok bool) {
	fc, isGotext := context.Fonts().(*FontConfigurationGotext)
	l, isLayout := layout.(*TextLayoutGotext)
	if !isGotext || !isLayout || len(l.Line) == 0 {
		return 0, 0, false
	}
	primary := fc.resolveFace(' ', l.Style.FontDescription)
	for _, run := range l.Line {
		if run.Face == nil || run.Face == primary || len(run.Glyphs) == 0 {
			continue
		}
		ascent, descent := fc.faceExtents(l.Style, run.Face)
		top, bottom := SplitLeading(fc.faceLineGap(l.Style, run.Face), true)
		above, below, ok = max(above, ascent+top), max(below, -descent+bottom), true
	}
	return above, below, ok
}

// faceLineGap (gopdfgo): odstęp międzywierszowy podanej twarzy w rozmiarze stylu, jak lineGap.
func (fc *FontConfigurationGotext) faceLineGap(style *TextStyle, face *font.Face) pr.Float {
	if face.Upem() == 0 {
		return 0
	}
	extents, _ := face.FontHExtents()
	size := pr.Float(style.FontDescription.Size) * pr.Float(fc.FaceSizeScale(style, face))
	if metrics, ok := fc.fontsMetrics[face.Font]; ok {
		if v, ok := metrics.lineGap.(pr.Float); ok {
			return roundMetric(v * size)
		}
	}
	return max(0, roundMetric(pr.Float(extents.LineGap)*size/pr.Float(face.Upem())))
}

// hasWeightAxis (gopdfgo) mówi, czy font ma oś wariantów wght (OpenType, tablica fvar).
func hasWeightAxis(ld *opentype.Loader) bool {
	raw, err := ld.RawTable(opentype.MustNewTag("fvar"))
	if err != nil || len(raw) < 16 {
		return false
	}
	offset := int(binary.BigEndian.Uint16(raw[4:]))
	count := int(binary.BigEndian.Uint16(raw[8:]))
	size := int(binary.BigEndian.Uint16(raw[10:]))
	for i := 0; i < count && offset+i*size+4 <= len(raw); i++ {
		if string(raw[offset+i*size:offset+i*size+4]) == "wght" {
			return true
		}
	}
	return false
}
