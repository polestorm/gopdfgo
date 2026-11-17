package text

import (
	"math"
	"strings"
	"unicode"

	"github.com/go-text/typesetting/language"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/text/hyphen"
)

type TextLayoutContext interface {
	Fonts() FontConfiguration
	HyphenCache() map[HyphenDictKey]hyphen.Hyphener
	StrutLayoutsCache() map[StrutLayoutKey][2]pr.Float
}

// EngineLayout stores the text engine dependant version of the laid out
// text.
//
// Is is only meant to be consumed by the text/draw package.
type EngineLayout interface {
	// Text returns a readonly slice of the text in the layout
	Text() []rune

	// Metrics may return zero when [TextDecorationLine] is empty
	Metrics() LineMetrics

	// Justification returns the current justification
	Justification() pr.Float
	// SetJustification add an additional spacing between words
	// to justify text. Depending on the implementation, it
	// may be ignored until [ApplyJustification] is called.
	SetJustification(spacing pr.Float)

	ApplyJustification()

	// TrailingSpacesWidth (gopdfgo) zwraca łączną szerokość spacji i tabulatorów na końcu wiersza:
	// przy white-space: pre-wrap takie znaki wiszą, czyli nie liczą się do wyrównania wiersza.
	TrailingSpacesWidth() pr.Float
	// SetHangingSpaces (gopdfgo) wyłącza z justowania podaną liczbę spacji na końcu wiersza.
	SetHangingSpaces(count int)
	HangingSpaces() int
}

// FirstLine exposes the result of laying out
// one line of text
type FirstLine struct {
	// Output layout containing (at least) the first line
	Layout EngineLayout

	// Length in runes of the first line
	Length int

	// ResumeAt is the number of runes to skip for the next line.
	// May be -1 if the whole text fits in one line.
	// This may be greater than [Length] in case of preserved
	// newline characters or white space collapse.
	ResumeAt int

	// Width is the width in pixels of the first line
	Width pr.Float

	// Hang (gopdfgo) to część Width zajęta przez wiszące spacje ideograficzne na końcu wiersza:
	// są rysowane, ale nie liczą się do szerokości wewnętrznych ani do przepełnienia.
	Hang pr.Float

	// Height is the height in pixels of the first line,
	// computed by merging the font extents (y position and height)
	// of each run in the line.
	Height pr.Float

	// Baseline is the baseline in pixels of the first line
	Baseline pr.Float

	FirstLineRTL bool // true is the first line direction is RTL
}

// split word on each hyphen occurence, starting by the end
func hyphenDictionaryIterations(word []rune, hyphen rune) (out []string) {
	for i := len(word) - 1; i >= 0; i-- {
		if word[i] == hyphen {
			out = append(out, string(word[:i+1]))
		}
	}
	return out
}

type HyphenDictKey struct {
	lang  language.Language
	limit pr.Limits
}

// returns a prefix of text
func shortTextHint(text []rune, maxWidth, fontSize pr.Float) []rune {
	// Try to use a small amount of text to avoid the whole layout. We need
	// at least one line, and one possible line break point on the second line.
	cut := len(text)

	const ratio = 4 // number that almost always respects char_height / char_width > ratio
	if fontSize*ratio > maxWidth {
		// Trying to find minimum or very small size, let's naively split on
		// spaces and keep one word + one letter.
		if spaceIndex := indexRune(text, ' '); spaceIndex != -1 {
			cut = spaceIndex + 2 // index + space + one letter
		}
	} else {
		// Use the magic ratio and hope that we’ll get the right amount of text.
		cut = int(maxWidth / fontSize * ratio)
	}

	if cut > len(text) {
		cut = len(text)
	}

	return text[:cut]
}

// SplitFirstLine fit as much text from [text] as possible in the available width given by [maxWidth].
// minimum should default to [false]
func SplitFirstLine(text []rune, style_ pr.StyleAccessor, context TextLayoutContext,
	maxWidth pr.MaybeFloat, minimum, isLineStart bool,
) FirstLine {
	return SplitFirstLineAt(text, style_, context, maxWidth, minimum, isLineStart, 0, -1)
}

// TabUnit (gopdfgo) zwraca jednostkę tabulacji bloku o podanym stylu: szerokość spacji powiększoną o
// letter-spacing i word-spacing. tab-size podany liczbą jest jej wielokrotnością, także w elemencie
// liniowym o innym foncie (CSS Text 3 § 4.2).
func TabUnit(style_ pr.StyleAccessor, context TextLayoutContext) pr.Float {
	return pr.Float(context.Fonts().tabUnit(NewTextStyle(style_, false)))
}

// SplitFirstLineAt (gopdfgo) działa jak [SplitFirstLine] dla tekstu, który zaczyna się [tabOffset]
// pikseli za początkiem wiersza; od tej odległości zależą szerokości tabulatorów. [tabWidth] to
// odstęp tabulacji bloku ([TabUnit]) albo liczba ujemna, gdy ma wynikać ze stylu samego tekstu.
func SplitFirstLineAt(text []rune, style_ pr.StyleAccessor, context TextLayoutContext,
	maxWidth pr.MaybeFloat, minimum, isLineStart bool, tabOffset, tabUnit pr.Float,
) FirstLine {
	style := NewTextStyle(style_, false)
	style.TabOffset = pr.Fl(tabOffset)
	if tabUnit >= 0 {
		style.TabUnit, style.HasTabUnit = pr.Fl(tabUnit), true
	}
	// fmt.Printf("SplitFirstLine in: %q %v %v %v %v\n", string(text), text, maxWidth, style_.GetDirection(), style.Family)
	out := context.Fonts().splitFirstLine(context.HyphenCache(), text, style, maxWidth, minimum, isLineStart)
	// fmt.Println("SplitFirstLine out:", out.Length, out.ResumeAt, out.Width, out.Height, out.FirstLineRTL)
	// fmt.Println()
	return out
}

type StrutLayoutKey struct {
	lang                 string
	fontFamily           string // joined
	lineHeight           pr.TaggedDim
	fontWeight           uint16
	fontSize             pr.Fl
	fontLanguageOverride fontLanguageOverride
	fontStretch          FontStretch
	fontStyle            FontStyle
	contentArea          bool // gopdfgo: wysokość obszaru treści, bez odstępu międzywierszowego
}

// StrutLayout returns a tuple of the used value of `line-height` and the baseline.
// The baseline is given from the top edge of line height.
// [context] is mandatory for the text layout.
func StrutLayout(style_ pr.StyleAccessor, context TextLayoutContext) (lineHeight, baseline pr.Float) {
	return strutLayout(style_, context, style_.GetLineHeight(), false)
}

// ContentAreaHeight zwraca wysokość obszaru treści pudełka liniowego: sumę wydłużenia górnego i dolnego
// pierwszego dostępnego fontu (CSS Inline 3 § 4.1), niezależną od line-height.
//
// gopdfgo: w źródle pudełko liniowe miało wysokość równą rozmiarowi fontu, więc tło i obramowanie
// elementów liniowych były niższe niż w przeglądarkach.
func ContentAreaHeight(style_ pr.StyleAccessor, context TextLayoutContext) pr.Float {
	height, _ := strutLayout(style_, context, pr.TaggedDim{Tag: pr.Normal}, true)
	return height
}

// SplitLeading (gopdfgo) dzieli interlinię na część nad i pod obszarem treści (CSS Inline 3 § 4.3:
// połowa z każdej strony, bez określenia dokładności).
//
// Przy podanym line-height podział jest dokładnie po połowie, jak w źródle: pomiar na korpusie
// dokumentów (2026-11-16, ramki słów względem wydruku Chromium) pokazał, że zaokrąglanie oddala
// tekst od Chromium o ok. 0,3 pt. Przy line-height: normal interlinią jest odstęp międzywierszowy
// fontu, liczony w całych pikselach; tam górna część jest zaokrąglana w dół, żeby linia bazowa
// została na siatce pikseli także przy nieparzystym odstępie.
func SplitLeading(leading pr.Float, normal bool) (top, bottom pr.Float) {
	if !normal {
		return leading / 2, leading / 2
	}
	top = pr.Float(math.Floor(float64(leading) / 2))
	return top, leading - top
}

// strutLayout: contentArea pomija odstęp międzywierszowy fontu, który należy do wysokości wiersza,
// a nie do obszaru treści.
func strutLayout(style_ pr.StyleAccessor, context TextLayoutContext, lineHeightV pr.TaggedDim, contentArea bool) (lineHeight, baseline pr.Float) {
	style := NewTextStyle(style_, false)

	fontSize := style.Size
	if fontSize == 0 {
		return 0, 0
	}

	key := StrutLayoutKey{
		lang:                 style.Lang,
		fontFamily:           strings.Join(style.Family, ""),
		lineHeight:           lineHeightV,
		fontWeight:           style.Weight,
		fontSize:             fontSize,
		fontLanguageOverride: style.FontLanguageOverride,
		fontStretch:          style.Stretch,
		fontStyle:            style.Style,
		contentArea:          contentArea,
	}

	cache := context.StrutLayoutsCache()
	if v, ok := cache[key]; ok {
		return v[0], v[1]
	}

	height, baseline := context.Fonts().spaceHeight(style)

	if lineHeightV.Tag == pr.Normal {
		lineHeight = height
		if !contentArea {
			// gopdfgo: line-height: normal obejmuje odstęp międzywierszowy fontu (CSS Inline 3 § 4.2:
			// „zaleca się użycie metryki line gap”), rozłożony nad i pod wierszem tak jak interlinia
			// przy podanym line-height (zob. SplitLeading). Źródło go pomijało, więc wiersze pisane fontem
			// z niezerowym odstępem (Tinos, Arimo) były niższe niż w przeglądarkach.
			gap := context.Fonts().lineGap(style)
			lineHeight += gap
			top, _ := SplitLeading(gap, true)
			baseline += top
		}
	} else {
		lineHeight = lineHeightV.Value
		if lineHeightV.Unit == pr.Scalar {
			lineHeight *= pr.Float(fontSize)
		}
		top, _ := SplitLeading(lineHeight-height, false)
		baseline = baseline + top
	}

	cache[key] = [2]pr.Float{lineHeight, baseline}
	return lineHeight, baseline
}

// CharacterRatio returns the ratio 1ex/font_size or 1ch/font_size, according to given style.
// It should be used with a valid text context to get accurate result.
// Otherwise, if context is `nil`, it returns 1 as a default value.
// It does not query WordSpacing or LetterSpacing from the style.
func CharacterRatio(style_ pr.ElementStyle, cache pr.TextRatioCache, isCh bool, fonts FontConfiguration) pr.Float {
	if fonts == nil {
		return 1
	}

	style := NewTextStyle(style_, true) // avoid recursion for letter-spacing and word-spacing properties
	key := style.cacheKey()
	if f, ok := cache.Get(key, isCh); ok {
		return f
	}

	// Random big value
	const fontSize pr.Fl = 100
	style.FontDescription.Size = fontSize

	var measure pr.Fl
	if isCh {
		measure = fonts.width0(style)
	} else {
		measure = fonts.heightx(style)
	}

	// Zero means some kind of failure, fallback is 0.5.
	// We round to try keeping exact values that were altered by the engine.
	v := pr.Float(math.Round(float64(measure/fontSize)*100000) / 100000)
	if v == 0 {
		v = 0.5
	}
	cache.Set(key, isCh, v)
	return v
}

func (style *TextStyle) cacheKey() string {
	return string(append(style.FontDescription.binary(nil, false), featuresBinary(style.FontFeatures)...))
}

func indexRune(text []rune, s rune) int {
	for i, v := range text {
		if v == s {
			return i
		}
	}
	return -1
}

func IsWhitespace(s []rune) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func HasPrefix(s []rune, r rune) bool {
	return len(s) != 0 && s[0] == r
}

func HasSuffix(s []rune, r rune) bool {
	return len(s) != 0 && s[len(s)-1] == r
}

func TrimSuffix(s []rune, r rune) []rune {
	for len(s) > 0 && s[len(s)-1] == r {
		s = s[:len(s)-1]
	}
	return s
}

func TrimPrefix(s []rune, r rune) []rune {
	for len(s) > 0 && s[0] == r {
		s = s[1:]
	}
	return s
}
