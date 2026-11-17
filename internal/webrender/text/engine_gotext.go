package text

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/fontscan"
	"github.com/go-text/typesetting/language"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/css/validation"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/text/hyphen"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"

	"github.com/go-text/typesetting/segmenter"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	"unicode"
)

var (
	_ FontConfiguration = (*FontConfigurationGotext)(nil)
	_ EngineLayout      = (*TextLayoutGotext)(nil)
)

// FontConfigurationGotext is NOT safe for concurrent use
type FontConfigurationGotext struct {
	fm         *fontscan.FontMap
	shaper     shaping.HarfbuzzShaper
	unicodeSeg segmenter.Segmenter
	inputSeg   shaping.Segmenter

	lineWrapper shaping.LineWrapper
	// residuals (gopdfgo): reszty zaokrągleń szerokości znaków tekstu zawijanego w bieżącym wywołaniu
	// WrapWordBreak, zob. shapeWithResiduals
	residuals []int32

	fontsContent  map[string][]byte        // to be embedded in the target
	fontsFeatures map[*font.Font][]Feature // as requested by @font-face
	loadedFaces   map[string]bool          // gopdfgo: reguły @font-face już wczytane (zob. loadOneFont)
	// featureValues (gopdfgo): nazwy z reguł @font-feature-values, zob. SetFontFeatureValues
	featureValues validation.FontFeatureValues

	// ReuseLoadedFaces (gopdfgo): ta sama reguła @font-face przetwarzana ponownie nie wczytuje fontu
	// drugi raz. Włącza to gopdfgo.Render, gdzie konfiguracja fontów żyje przez jedno renderowanie;
	// konfiguracja współdzielona między dokumentami (testy przejęte) zachowuje zachowanie źródła.
	ReuseLoadedFaces bool

	textLayoutCache map[textKey]FirstLine

	// GenericFamilies (gopdfgo) przypisuje rodzinom generycznym CSS ("serif", "sans-serif", "monospace", ...)
	// rodziny fontów próbowane w podanej kolejności, zanim zadziała tablica podstawień biblioteki go-text.
	// Klucze małymi literami. Ustawiane przed pierwszym użyciem konfiguracji.
	GenericFamilies map[string][]string
}

func NewFontConfigurationGotext(fm *fontscan.FontMap) *FontConfigurationGotext {
	out := FontConfigurationGotext{
		fm:              fm,
		fontsContent:    make(map[string][]byte),
		fontsFeatures:   make(map[*font.Font][]Feature), // as loaded by loadOneFont
		textLayoutCache: make(map[textKey]FirstLine),
	}
	out.shaper.SetFontCacheSize(64)
	return &out
}

// SetFontFeatureValues (gopdfgo): zob. [FontConfiguration].
func (f *FontConfigurationGotext) SetFontFeatureValues(values validation.FontFeatureValues) {
	f.featureValues = values
}

func (f *FontConfigurationGotext) fontFeatureValues() validation.FontFeatureValues {
	return f.featureValues
}

// AddFontFace load a font file from an external source, using
// the given [urlFetcher], which must be valid.
//
// It returns the key of the loaded file.
func (f *FontConfigurationGotext) AddFontFace(ruleDescriptors validation.FontFaceDescriptors, urlFetcher utils.UrlFetcher) string {
	for _, url := range ruleDescriptors.Src {
		if url.S == "" {
			continue
		}
		if !(url.Tag == pr.External || url.Tag == pr.Local) {
			continue
		}

		filename, err := f.loadOneFont(url, ruleDescriptors, urlFetcher)
		if err != nil {
			logger.WarningLogger.Printf("AddFonfFace: %s\n", err)
			continue
		}

		return filename
	}

	logger.WarningLogger.Printf("Font face %s (src : %v) cannot be loaded", ruleDescriptors.FontFamily, ruleDescriptors.Src)
	return ""
}

// returns an error if the font is not found or has failed to be downloaded.
func (f *FontConfigurationGotext) loadOneFont(url pr.TaggedString, ruleDescriptors validation.FontFaceDescriptors, urlFetcher utils.UrlFetcher) (string, error) {
	if url.Tag == pr.Local {
		family := url.S
		// search through the system fonts, returning the filepath of the font, or an empty string
		// if no font matches the given family.
		location, ok := f.fm.FindSystemFont(family)
		if !ok {
			return "", fmt.Errorf("failed to load local font %s: not found", family)
		}

		// replace the family by an actual path
		var err error
		url.S, err = filepath.Abs(location.File)
		if err != nil {
			return "", fmt.Errorf("failed to load local font %s: %s", family, err)
		}
	}

	desc := font.Description{
		Family: string(ruleDescriptors.FontFamily),
		Aspect: newAspect(
			newFontStyle(ruleDescriptors.FontStyle),
			newFontWeight(ruleDescriptors.FontWeight),
			newFontStretch(ruleDescriptors.FontStretch),
		),
	}
	// add the face with a "unique" ID
	key := fmt.Sprintf("%s+%v", url.S, desc)

	// gopdfgo: ta sama reguła @font-face czytana drugi raz (arkusze są przetwarzane dwukrotnie, zob.
	// ADR 0012) nie pobiera ani nie parsuje fontu ponownie. W źródle każde przetworzenie arkusza
	// dodawało nową twarz; dla małych dokumentów była to ponad jedna piąta czasu renderowania.
	features := getFontFaceFeatures(ruleDescriptors)
	loadedKey := fmt.Sprintf("%s+%v", key, features)
	if f.ReuseLoadedFaces && f.loadedFaces[loadedKey] {
		return key, nil
	}

	result, err := urlFetcher(url.S)
	if err != nil {
		return "", fmt.Errorf("failed to load font at %s: %s", url.S, err)
	}

	content, err := io.ReadAll(result.Content)
	if err != nil {
		return "", fmt.Errorf("failed to load font at %s", url.S)
	}

	lds, err := opentype.NewLoaders(result.Content)
	if err != nil {
		return "", fmt.Errorf("failed to parse font at %s : %s", url.S, err)
	}
	if len(lds) != 1 {
		return "", fmt.Errorf("font collections are not supported (at %s)", url.S)
	}

	ft, err := font.NewFont(lds[0])
	if err != nil {
		return "", fmt.Errorf("failed to parse font at %s : %s", url.S, err)
	}

	f.fm.AddFace(font.NewFace(ft), fontscan.Location{File: key}, desc)

	if url.Tag == pr.External {
		f.fontsContent[key] = content
	}

	// track the font features to apply
	f.fontsFeatures[ft] = features
	if f.loadedFaces == nil {
		f.loadedFaces = map[string]bool{}
	}
	f.loadedFaces[loadedKey] = true

	return key, nil
}

func (f *FontConfigurationGotext) FontLocation(font *font.Font) fontscan.Location {
	// TODO: simplify FontContent impl.
	return f.fm.FontLocation(font)
}

// FontContent returns the content of the given face, which may be needed
// in the final output.
// RegisterFontContent zapamiętuje treść fontu dodanego do mapy fontów z pamięci (fontscan.FontMap.AddFont),
// żeby FontContent nie szukało go na dysku pod identyfikatorem.
//
// gopdfgo: dodane; w źródle treść miały tylko fonty z @font-face, a pozostałe czytano z pliku.
func (f *FontConfigurationGotext) RegisterFontContent(fileID string, content []byte) {
	f.fontsContent[fileID] = content
}

func (f *FontConfigurationGotext) FontContent(font FontOrigin) []byte {
	// either the font is registred at run time or is loaded from disk
	// if registred at run time, its content has already been written in fontsContent
	if content, has := f.fontsContent[font.File]; has {
		return content
	}

	b, err := os.ReadFile(font.File)
	if err != nil {
		logger.WarningLogger.Println("FontContent:", err)
	}
	// cache the result to avoid loading the same file over and over
	f.fontsContent[font.File] = b

	return b
}

type TextLayoutGotext struct {
	hangingSpaces int // gopdfgo: spacje na końcu wiersza wyłączone z justowania
	text          []rune
	Style         *TextStyle
	Line          shaping.Line

	MaxWidth pr.Float // input constraint, not always respected. Inf for no constraint

	justification pr.Float
	justified     bool // gopdfgo: odstęp justowania został już dodany do glifów
}

// privateLayout (gopdfgo) zwraca wynik z własnym obiektem układu. Wyniki z pamięci podręcznej były
// współdzielone: SetJustification i ApplyJustification zmieniały wspólny obiekt, więc wiersze o tym
// samym tekście nadpisywały sobie odstępy justowania, a wiersz rysowany drugi raz dostawał je podwójnie
// i wystawał poza prawy margines.
func privateLayout(line FirstLine) FirstLine {
	if shared, ok := line.Layout.(*TextLayoutGotext); ok && shared != nil {
		own := *shared
		own.justification, own.justified = 0, false
		line.Layout = &own
	}
	return line
}

// Text returns a readonly slice of the text in the layout
func (l TextLayoutGotext) Text() []rune { return l.text }

// Metrics may return nil when [TextDecorationLine] is empty
func (l TextLayoutGotext) Metrics() LineMetrics {
	if l.Style.TextDecorationLine == 0 {
		return LineMetrics{}
	}

	face := l.Line[0].Face
	factor := pr.Fl(l.Style.FontDescription.Size) / pr.Fl(face.Upem())

	extents, _ := face.FontHExtents()
	return LineMetrics{
		Ascent:                 pr.Fl(extents.Ascender) * factor,
		UnderlinePosition:      face.LineMetric(font.UnderlinePosition) * factor,
		UnderlineThickness:     face.LineMetric(font.UnderlineThickness) * factor,
		StrikethroughPosition:  face.LineMetric(font.StrikethroughPosition) * factor,
		StrikethroughThickness: face.LineMetric(font.StrikethroughThickness) * factor,
	}
}

// Justification returns the current justification
func (l TextLayoutGotext) Justification() pr.Float { return l.justification }

// SetJustification add an additional spacing between words
// to justify text. Depending on the implementation, it
// may be ignored until [ApplyJustification] is called.
func (l *TextLayoutGotext) SetJustification(spacing pr.Float) {
	l.justification = spacing
}

func (l *TextLayoutGotext) SetHangingSpaces(count int) { l.hangingSpaces = count }
func (l *TextLayoutGotext) HangingSpaces() int         { return l.hangingSpaces }

func (l *TextLayoutGotext) TrailingSpacesWidth() pr.Float {
	if len(l.Line) == 0 || l.Style == nil || l.Style.Direction == pr.Rtl {
		return 0
	}
	// gopdfgo: wiszą też odstępy kategorii Zs inne niż spacja (CSS Text 3 § 4.1.3), a ciąg bywa
	// złożony kilkoma fontami, więc liczymy go przez wszystkie przebiegi od końca
	var width fixed.Int26_6
runs:
	for r := len(l.Line) - 1; r >= 0; r-- {
		glyphs := l.Line[r].Glyphs
		for i := len(glyphs) - 1; i >= 0; i-- {
			index := glyphs[i].ClusterIndex
			if index < 0 || index >= len(l.text) || !isHangingWhiteSpace(l.text[index]) {
				break runs
			}
			width += glyphs[i].Advance
		}
	}
	return fixedToFloat(width)
}

func (l *TextLayoutGotext) ApplyJustification() {
	if l.justified || l.justification == 0 {
		return // gopdfgo: pudełko rysowane ponownie nie dostaje odstępu drugi raz
	}
	// gopdfgo: glify są wspólne z pamięcią podręczną układów, więc zmieniamy ich kopię
	line := make(shaping.Line, len(l.Line))
	for i, run := range l.Line {
		run.Glyphs = append([]shaping.Glyph(nil), run.Glyphs...)
		line[i] = run
	}
	l.Line = line
	// gopdfgo: numer skupienia glifu może wychodzić poza tekst wiersza (obcięta spacja na końcu, tekst
	// od prawej do lewej), a go-text indeksuje nim tekst bez sprawdzenia; w źródle kończyło się to
	// paniką. Brakujące pozycje dopełniamy znakiem, który nie jest odstępem.
	text := l.text
	for _, run := range l.Line {
		for _, glyph := range run.Glyphs {
			if glyph.ClusterIndex >= len(text) {
				text = append(append([]rune(nil), text...), make([]rune, glyph.ClusterIndex+1-len(text))...)
			}
		}
	}
	// wiszące spacje na końcu wiersza zachowują swoją szerokość
	var kept []fixed.Int26_6
	if n := l.hangingSpaces; n > 0 && len(l.Line) != 0 {
		if glyphs := l.Line[len(l.Line)-1].Glyphs; len(glyphs) >= n {
			for _, glyph := range glyphs[len(glyphs)-n:] {
				kept = append(kept, glyph.Advance)
			}
		}
	}
	shaping.AddSpacing(l.Line, text, floatToFixed(pr.Fl(l.justification)), 0)
	if len(kept) != 0 {
		last := &l.Line[len(l.Line)-1]
		for i, advance := range kept {
			last.Glyphs[len(last.Glyphs)-len(kept)+i].Advance = advance
		}
		last.RecomputeAdvance()
	}
	l.justified = true
}

func newAspect(style FontStyle, weight uint16, stretch FontStretch) font.Aspect {
	aspect := font.Aspect{
		Style:  font.StyleNormal,
		Weight: font.Weight(weight),
	}
	if style == FSty_Italic || style == FSty_Oblique {
		aspect.Style = font.StyleItalic
	}
	switch stretch {
	case FStr_UltraCondensed:
		aspect.Stretch = font.StretchUltraCondensed
	case FStr_ExtraCondensed:
		aspect.Stretch = font.StretchExtraCondensed
	case FStr_Condensed:
		aspect.Stretch = font.StretchCondensed
	case FStr_SemiCondensed:
		aspect.Stretch = font.StretchSemiCondensed
	case FStr_Normal:
		aspect.Stretch = font.StretchNormal
	case FStr_SemiExpanded:
		aspect.Stretch = font.StretchSemiExpanded
	case FStr_Expanded:
		aspect.Stretch = font.StretchExpanded
	case FStr_ExtraExpanded:
		aspect.Stretch = font.StretchExtraExpanded
	case FStr_UltraExpanded:
		aspect.Stretch = font.StretchUltraExpanded
	}
	return aspect
}

func (fc *FontConfigurationGotext) newQuery(fd FontDescription) fontscan.Query {
	families := fd.Family
	if len(fc.GenericFamilies) != 0 {
		// gopdfgo: rodziny wskazane dla rodziny generycznej wchodzą na listę tuż przed nią
		families = make([]string, 0, len(fd.Family)+2)
		for _, family := range fd.Family {
			families = append(families, fc.GenericFamilies[strings.ToLower(family)]...)
			families = append(families, family)
		}
	}
	return fontscan.Query{
		Families: families,
		Aspect:   newAspect(fd.Style, fd.Weight, fd.Stretch),
	}
}

func newFeatures(features []Feature) []shaping.FontFeature {
	fts := make([]shaping.FontFeature, len(features))
	for i, f := range features {
		fts[i] = shaping.FontFeature{
			Tag:   opentype.NewTag(f.Tag[0], f.Tag[1], f.Tag[2], f.Tag[3]),
			Value: f.Value,
		}
	}
	return fts
}

func (fc *FontConfigurationGotext) resolveFace(r rune, font FontDescription) *font.Face {
	query := fc.newQuery(font)
	fc.fm.SetQuery(query)
	return fc.fm.ResolveFace(r)
}

// uses sizeFactor * font.Size and returns sizeFactor
func (fc *FontConfigurationGotext) shapeRune(r rune, desc FontDescription, features []Feature) ([]shaping.Glyph, shaping.Bounds, pr.Float) {
	face := fc.resolveFace(r, desc)
	if face == nil { // fontmap is broken
		return nil, shaping.Bounds{}, 1
	}

	// sizeFactor is used to get a better precision
	sizeFactor := fixed.Int26_6(1000)
	if desc.Size >= 400 { // avoid overflowing go-text Int26_6 upper limit
		sizeFactor = 1
	} else if desc.Size >= 200 {
		sizeFactor = 5
	} else if desc.Size >= 100 {
		sizeFactor = 10
	}

	out := fc.shaper.Shape(shaping.Input{
		Text:     []rune{r},
		RunStart: 0, RunEnd: 1,
		Direction:    di.DirectionLTR,
		FontFeatures: newFeatures(features),
		Script:       language.Latin,
		Language:     language.NewLanguage("en"),
		Face:         face,
		// float to fixed, the size factor is to get a better precision
		Size: floatToFixed(desc.Size) * sizeFactor,
	})
	return out.Glyphs, out.LineBounds, pr.Float(sizeFactor)
}

func (fc *FontConfigurationGotext) heightx(style *TextStyle) pr.Fl {
	glyphs, _, sizeFactor := fc.shapeRune('x', style.FontDescription, style.FontFeatures)

	if len(glyphs) == 0 { // fontmap is broken, return a 'reasonnable' value
		return style.FontDescription.Size
	}

	return pr.Fl(fixedToFloat(glyphs[0].YBearing) / sizeFactor) // fixed to float
}

func (fc *FontConfigurationGotext) width0(style *TextStyle) pr.Fl {
	glyphs, _, sizeFactor := fc.shapeRune('0', style.FontDescription, style.FontFeatures)

	if len(glyphs) == 0 { // fontmap is broken, return a 'reasonnable' value
		return style.FontDescription.Size
	}
	return pr.Fl(fixedToFloat(glyphs[0].Advance) / sizeFactor) // fixed to float
}

func (fc *FontConfigurationGotext) spaceHeight(style *TextStyle) (height, baseline pr.Float) {
	_, bounds, sizeFactor := fc.shapeRune(' ', style.FontDescription, style.FontFeatures)

	baseline = roundMetric(fixedToFloat(bounds.Ascent) / sizeFactor)
	height = baseline - roundMetric(fixedToFloat(bounds.Descent)/sizeFactor)

	return height, baseline
}

// lineGap (gopdfgo) zwraca odstęp międzywierszowy pierwszego dostępnego fontu, zaokrąglony do całych
// pikseli tak jak wydłużenia (zob. roundMetric).
func (fc *FontConfigurationGotext) lineGap(style *TextStyle) pr.Float {
	face := fc.resolveFace(' ', style.FontDescription)
	if face == nil || face.Upem() == 0 {
		return 0
	}
	extents, _ := face.FontHExtents()
	return max(0, roundMetric(pr.Float(extents.LineGap)*pr.Float(style.FontDescription.Size)/pr.Float(face.Upem())))
}

// sizeAdjustFactor (gopdfgo) zwraca mnożnik, przez który font-size-adjust przelicza obliczony rozmiar
// fontu na rozmiar używany (CSS Fonts 5 § 2.7): podana liczba podzielona przez stosunek wybranej
// metryki pierwszego dostępnego fontu do jego firetu. Zwraca 1 dla none, dla from-font (liczba jest
// wtedy z definicji równa stosunkowi fontu podstawowego) i wtedy, gdy font metryki nie podaje.
// Fonty zastępcze dostają ten sam mnożnik co podstawowy; norma każe liczyć go osobno dla każdego fontu.
func (fc *FontConfigurationGotext) sizeAdjustFactor(style *TextStyle) pr.Fl {
	adjust := style.SizeAdjust
	if adjust.String == "" || adjust.Bool {
		return 1
	}
	face := fc.resolveFace(' ', style.FontDescription)
	if face == nil || face.Upem() == 0 {
		return 1
	}
	advance := func(r rune, vertical bool) float32 {
		gid, ok := face.NominalGlyph(r)
		if r == '水' && (!ok || vertical && !face.Font.HasVerticalMetrics()) {
			// font bez znaku U+6C34 albo bez metryk pionowych: miarą ic jest 1em (CSS Values 4 § 6.1.1)
			return float32(face.Upem())
		}
		if !ok {
			return 0
		}
		if vertical {
			return face.VerticalAdvance(gid)
		}
		return face.HorizontalAdvance(gid)
	}
	var metric float32
	switch adjust.String {
	case "ex-height":
		metric = face.LineMetric(font.XHeight)
	case "cap-height":
		metric = face.LineMetric(font.CapHeight)
	case "ch-width":
		metric = advance('0', false)
	case "ic-width":
		metric = advance('水', false)
	case "ic-height":
		metric = advance('水', true)
	}
	if metric < 0 {
		metric = -metric
	}
	if metric == 0 {
		return 1
	}
	return adjust.Float * pr.Fl(face.Upem()) / pr.Fl(metric)
}

// tabUnit: szerokość spacji powiększona o letter-spacing i word-spacing; tab-size podany liczbą jest
// jej wielokrotnością (CSS Text 3 § 4.2).
func (fc *FontConfigurationGotext) tabUnit(style *TextStyle) pr.Fl {
	return fc.widthSpace(style) + style.LetterSpacing + fc.wordSpacing(style)
}

// wordSpacing (gopdfgo) zwraca word-spacing w pikselach; procent liczy się od szerokości znaku
// U+0020 w foncie elementu (CSS Text 4 § 10.1).
func (fc *FontConfigurationGotext) wordSpacing(style *TextStyle) pr.Fl {
	if style.WordSpacingCalc != nil {
		return pr.Fl(style.WordSpacingCalc.Evaluate(pr.Float(fc.widthSpace(style))))
	}
	if style.WordSpacingPercent == 0 {
		return style.WordSpacing
	}
	return style.WordSpacing + style.WordSpacingPercent/100*fc.widthSpace(style)
}

func (fc *FontConfigurationGotext) widthSpace(style *TextStyle) pr.Fl {
	glyphs, _, sizeFactor := fc.shapeRune(' ', style.FontDescription, style.FontFeatures)

	if len(glyphs) == 0 { // fontmap is broken, return a 'reasonnable' value
		return style.FontDescription.Size
	}

	return pr.Fl(fixedToFloat(glyphs[0].Advance) / sizeFactor) // fixed to float
}

func (fc *FontConfigurationGotext) CanBreakText(t []rune) pr.MaybeBool {
	if len(t) < 2 {
		return nil
	}
	fc.unicodeSeg.Init(t)
	iter := fc.unicodeSeg.LineIterator()
	if iter.Next() {
		line := iter.Line()
		end := line.Offset + len(line.Text)
		if end < len(t) {
			return pr.True
		}
	}
	return pr.False
}

// returns nil or a slice [wordStart:wordEnd]
func (fc *FontConfigurationGotext) wordBoundaries(t []rune) *[2]int {
	if len(t) < 2 {
		return nil
	}
	fc.unicodeSeg.Init(t)
	iter := fc.unicodeSeg.WordIterator()
	if iter.Next() {
		word := iter.Word()
		return &[2]int{word.Offset, word.Offset + len(word.Text)}
	}
	return nil
}

// return -1 if not found
func (fc *FontConfigurationGotext) getNextBreakPoint(text []rune) int {
	fc.unicodeSeg.Init(text)
	iter := fc.unicodeSeg.LineIterator()
	if iter.Next() {
		line := iter.Line()
		end := line.Offset + len(line.Text)
		return end
	}
	return -1
}

// GetLastWordEnd returns the index in `t` of the end of the before-last word,
// or -1
func (fc *FontConfigurationGotext) GetLastWordEnd(t []rune) int {
	if len(t) < 2 {
		return -1
	}
	fc.unicodeSeg.Init(t)
	iter := fc.unicodeSeg.WordIterator()
	var word1, word2 segmenter.Word
	for iter.Next() {
		word1 = word2
		word2 = iter.Word()
	}
	return word1.Offset + len(word1.Text)
}

// returns the first occurence of c, or -1 if not found
func index(text []rune, c rune) int {
	for i, r := range text {
		if r == c {
			return i
		}
	}
	return -1
}

// returns the last occurence of c, or -1 if not found
func lastIndex(text []rune, c rune) int {
	for i := len(text) - 1; i >= 0; i-- {
		if text[i] == c {
			return i
		}
	}
	return -1
}

func hasSuffix(text []rune, c rune) bool {
	return len(text) != 0 && text[len(text)-1] == c
}

// equivalent of python rstrip(' ')
func trimTrailingSpaces(text []rune) []rune {
	i := len(text) - 1
	for ; i >= 0; i-- {
		if text[i] != ' ' {
			break
		}
	}
	return text[:i+1]
}

// secondLineIndex is -1 if the whole [text] fits into the first line
// pass pr.Inf to remove width constraint
func (fc *FontConfigurationGotext) wrap(text []rune, style *TextStyle, maxWidth pr.Float) FirstLine {
	return fc.WrapWordBreak(text, style, maxWidth, false)
}

func floatToFixed(v pr.Fl) fixed.Int26_6    { return fixed.Int26_6(v * 64) }
func fixedToFloat(v fixed.Int26_6) pr.Float { return pr.Float(v) / 64 }

type textKey struct {
	text           string
	style          styleKey
	maxWidth       pr.Float
	allowWordBreak bool
}

// same as wrap, but may allows break inside words
func (fc *FontConfigurationGotext) WrapWordBreak(text []rune, style *TextStyle, maxWidth pr.Float, allowWordBreak bool) FirstLine {
	if len(text) == 0 {
		return FirstLine{
			Layout:   &TextLayoutGotext{Style: style, MaxWidth: maxWidth},
			Length:   0,
			ResumeAt: -1,
			Width:    0, Height: 0, Baseline: 0,
			FirstLineRTL: false,
		}
	}

	key := textKey{string(text), style.key(), maxWidth, allowWordBreak}
	if l, ok := fc.textLayoutCache[key]; ok {
		return privateLayout(l)
	}

	var lang language.Language
	if flo := style.FontLanguageOverride; (flo != fontLanguageOverride{}) {
		lang = language.NewLanguage(lstToISO[flo])
	} else if lg := style.Lang; lg != "" {
		lang = language.NewLanguage(lg)
	} else {
		lang = language.DefaultLanguage()
	}

	// select the proper fonts
	fc.fm.SetQuery(fc.newQuery(style.FontDescription))

	// segment the input text, with proper lang and size
	dir := di.DirectionLTR
	if style.Direction == pr.Rtl {
		dir = di.DirectionRTL
	}
	inputs := fc.inputSeg.Split(shaping.Input{
		Text:      text,
		RunEnd:    len(text),
		Language:  lang,
		Size:      floatToFixed(style.FontDescription.Size),
		Direction: dir, // default, will be overriden
	}, fc.fm)
	inputs = fc.spacesInOwnFace(inputs)

	// gopdfgo: reszty zaokrągleń szerokości znaków, potrzebne tylko przy zawijaniu (zob. LineWrap)
	var residuals []int32
	if style.textWrap() && maxWidth != pr.Inf {
		residuals = make([]int32, len(text))
	}
	// TODO: lazy iterator
	outputs := make(shaping.Line, len(inputs))
	for i, input := range inputs {
		// the features are comming either from the style,
		// or registred via CSS @font-face rule
		defaults := newFeatureSet(fc.fontsFeatures[input.Face.Font])
		defaults.merge(style.FontFeatures)
		input.FontFeatures = newFeatures(defaults.list())

		// shape !
		outputs[i] = shapeWithResiduals(&fc.shaper, input, residuals)
	}
	fc.residuals = residuals

	if wordSpacing := fc.wordSpacing(style); style.LetterSpacing != 0 || wordSpacing != 0 {
		ws, ls := floatToFixed(wordSpacing), floatToFixed(style.LetterSpacing)
		shaping.AddSpacing(outputs, text, ws, ls)
		// add letter spacing at the end, like other browers do
		lastRun := &outputs[len(outputs)-1]
		lastRun.Glyphs[len(lastRun.Glyphs)-1].Advance += ls
		lastRun.RecomputeAdvance()
	}

	// do we have some tabs ?
	// gopdfgo: po odstępach między literami, bo tabulator ich nie dostaje: kończy się na pozycji tabulacji
	if indexRune(text, '\t') != -1 {
		tabWidth := floatToFixed(style.TabSize.Width)
		if style.TabSize.IsMultiple {
			unit := fc.tabUnit(style)
			if style.HasTabUnit {
				unit = style.TabUnit
			}
			// jednostka zaokrąglona tak jak szerokości glifów, żeby tabulator równał się z rzędem spacji
			tabWidth = fixed.Int26_6(math.Round(math.Round(float64(unit)*64) * float64(style.TabSize.Width)))
		}
		AlignTabs(outputs, text, tabWidth, floatToFixed(style.TabOffset), floatToFixed(fc.width0(style)/2))
	}

	// now we can wrap the runs
	textWrap, spaceCollapse := style.textWrap(), style.spaceCollapse()
	// resolve the wraping width, ignoring the constraint when not wraping
	wrapingMaxWidth := maxWidth
	if !textWrap {
		wrapingMaxWidth = pr.Inf
	}
	mw, wLine, fitsOnFirstLine := fc.LineWrap(text, style, outputs, wrapingMaxWidth, allowWordBreak)
	line := wLine.Line
	// gopdfgo: biblioteka pozwala wystawać poza wiersz każdemu końcowemu odstępowi, także spacji
	// nierozdzielającej. Ta nie jest zwijalna i nie wisi, więc wiersz, który przez nią się nie mieści,
	// łamiemy wcześniej (o ile jest gdzie).
	if textWrap && len(line) != 0 && dir.Progression() == di.FromTopLeft {
		var total fixed.Int26_6
		for _, run := range line {
			total += run.Advance
		}
		if total > mw {
			glyphs := line[len(line)-1].Glyphs
			var trailing fixed.Int26_6
			count := 0
			for i := len(glyphs) - 1; i >= 0; i-- {
				index := glyphs[i].ClusterIndex
				if index < 0 || index >= len(text) || glyphs[i].Width != 0 || text[index] == ' ' || IsOtherSpaceSeparator(text[index]) || !unicode.IsSpace(text[index]) {
					break
				}
				trailing += glyphs[i].Advance
				count++
			}
			if lastRun := &line[len(line)-1]; trailing != 0 && total-trailing <= mw && (allowWordBreak || style.WordBreak == WBAnywhere) &&
				(len(line) > 1 || len(glyphs) > count) && lastRun.Runes.Count >= count {
				// łamanie w dowolnym miejscu: odstęp przechodzi na początek następnego wiersza
				lastRun.Glyphs = slices.Clone(glyphs[:len(glyphs)-count])
				lastRun.Runes.Count -= count
				lastRun.RecalculateAll()
				wLine.NextLine -= count
				fitsOnFirstLine = false
			} else if narrower := wrapingMaxWidth - pr.Float(trailing)/64; trailing != 0 && total-trailing <= mw && narrower > 0 {
				end := wLine.NextLine
				if _, shorter, _ := fc.LineWrap(text, style, outputs, narrower, allowWordBreak); len(shorter.Line) != 0 && shorter.NextLine < end {
					wLine, line, fitsOnFirstLine = shorter, shorter.Line, false
				} else {
					mw, wLine, fitsOnFirstLine = fc.LineWrap(text, style, outputs, wrapingMaxWidth, allowWordBreak)
					line = wLine.Line
				}
			}
		}
	}

	// gopdfgo: Hang liczymy tylko dla wierszy z odstępem innym niż spacja (np. ideograficznym U+3000),
	// który na końcu wiersza wisi, zachowując szerokość (CSS Text 3 § 4.1.3)
	hasIdeographic := spaceCollapse && slices.ContainsFunc(text, IsOtherSpaceSeparator)

	if len(line) == 0 {
		return FirstLine{
			Layout:   &TextLayoutGotext{Style: style, MaxWidth: maxWidth},
			Length:   0,
			ResumeAt: -1,
			Width:    0, Height: 0, Baseline: 0,
			FirstLineRTL: false,
		}
	}

	// gopdfgo: white-space: break-spaces. Zachowane spacje zajmują miejsce i nie wiszą, a wiersz wolno
	// złamać po każdej z nich, nigdy przed pierwszą (CSS Text 3 § 3). Biblioteka pozwala spacjom na końcu
	// wiersza wystawać poza jego szerokość, więc poprawiamy wynik: gdy słowo z jedną spacją się nie
	// mieści, łamiemy wcześniej; spacje ponad szerokość przechodzą do następnego wiersza. Tylko dla
	// pisma od lewej do prawej.
	if style.breakSpaces() && textWrap && dir.Progression() == di.FromTopLeft {
		for attempt := 0; attempt < 2; attempt++ {
			end := wLine.NextLine
			// gopdfgo: także tabulatory: przy break-spaces wiersz wolno złamać po każdym zachowanym
			// białym znaku, tabulator nie wisi (CSS Text 3 § 5.1, § 4.1.3; testy WPT break-spaces-tab-*)
			if end <= 0 || end > len(text) || !isBreakSpace(text[end-1]) {
				break
			}
			spaces := 0
			for end-spaces-1 >= 0 && isBreakSpace(text[end-spaces-1]) {
				spaces++
			}
			lastRun := &line[len(line)-1]
			if len(lastRun.Glyphs) < spaces {
				break
			}
			var total fixed.Int26_6
			for _, run := range line {
				total += run.Advance
			}
			if total <= mw {
				break
			}
			// szerokości spacji w kolejności tekstu (zwykła spacja i ideograficzna mają różne)
			trailing := lastRun.Glyphs[len(lastRun.Glyphs)-spaces:]
			spaceAdvance := trailing[0].Advance
			var spacesWidth fixed.Int26_6
			for _, glyph := range trailing {
				spacesWidth += glyph.Advance
			}
			if spaceAdvance <= 0 {
				break
			}
			content := total - spacesWidth
			// Przy overflow-wrap: anywhere albo break-word wolno złamać także przed pierwszą spacją,
			// i to jest lepsze niż podział w środku słowa. word-break: break-all tego nie daje: tam
			// łamie się słowo (testy WPT break-spaces-before-first-char-004, overflow-wrap-break-word-006).
			// line-break: anywhere pozwala łamać przy każdym znaku, także przed zachowaną spacją.
			minKeep := 1
			if allowWordBreak && content > 0 && (style.WordBreak == WBAnywhere || style.OverflowWrap != ONormal && style.WordBreak == WBNormal) {
				minKeep = 0
			}
			if content > 0 && content+spaceAdvance > mw && attempt == 0 && minKeep == 1 {
				// słowo razem z pierwszą spacją się nie mieści: szukamy wcześniejszego miejsca podziału
				narrower := wrapingMaxWidth - pr.Float(spaceAdvance)/64
				if _, shorter, _ := fc.LineWrap(text, style, outputs, narrower, allowWordBreak); len(shorter.Line) != 0 && shorter.NextLine < end {
					wLine, line, fitsOnFirstLine = shorter, shorter.Line, false
					continue
				}
				mw, wLine, fitsOnFirstLine = fc.LineWrap(text, style, outputs, wrapingMaxWidth, allowWordBreak)
				line = wLine.Line
				lastRun = &line[len(line)-1]
			}
			keep := spaces
			if content+spacesWidth > mw {
				keep = 0
				for used := content; keep < spaces && used+trailing[keep].Advance <= mw; keep++ {
					used += trailing[keep].Advance
				}
				keep = max(minKeep, keep)
			}
			if drop := spaces - keep; drop > 0 {
				lastRun.Glyphs = slices.Clone(lastRun.Glyphs[:len(lastRun.Glyphs)-drop])
				lastRun.Runes.Count -= drop
				lastRun.RecalculateAll()
				wLine.NextLine -= drop
				fitsOnFirstLine = false
			}
			break
		}
	}

	resumeAt := wLine.NextLine
	firstLineLength := resumeAt
	if resumeAt == len(text) {
		resumeAt = -1
	}

	// handle mandatory break properly : go-text keeps it on the first line,
	// while weasyprint skips it
	// See https://unicode.org/reports/tr14/#BK
	// 000B LINE TABULATION (VT)
	// 000C FORM FEED (FF)
	// 2028 LINE SEPARATOR
	// 2029 PARAGRAPH SEPARATOR
	mandatoryBreak := false
	if r := text[wLine.NextLine-1]; r == '\n' || r == '\u000B' || r == '\u000C' || r == '\u2028' || r == '\u2029' {
		mandatoryBreak = true
		resumeAt = wLine.NextLine
		firstLineLength = wLine.NextLine - 1
		lastRun := &line[len(line)-1]
		lastRun.Glyphs = lastRun.Glyphs[:len(lastRun.Glyphs)-1]
		lastRun.RecalculateAll()
		wLine.TrimmedTrailingWhitespace = 0 // not to interfere with latter space handling
	}

	// sort the line by visual order
	sort.Slice(line, func(i, j int) bool { return line[i].VisualIndex < line[j].VisualIndex })

	if !fitsOnFirstLine && spaceCollapse {
		// remove the space runes...
		text = trimTrailingSpaces(text[:firstLineLength])
		trimmed := firstLineLength - len(text)
		firstLineLength = len(text)
		// and the matching glyphs
		lastRun := &line[len(line)-1]
		lastRun.Runes.Count -= trimmed // we assume all spaces are on the same run
		i := len(lastRun.Glyphs) - 1
		for ; i >= 0; i-- {
			if lastRun.Glyphs[i].Width != 0 || lastRun.Glyphs[i].ClusterIndex < len(text) {
				break
			}
		}
		lastRun.Glyphs = lastRun.Glyphs[:i+1]
		lastRun.RecalculateAll()
	}

	// copy the line, owned by lineWrapper
	outLine := slices.Clone(line)

	var (
		width fixed.Int26_6
		// directly fetch line bounds, to avoid rounding errors
		height, top pr.Float
	)
	for _, run := range outLine {
		width += run.Advance

		extents, _ := run.Face.FontHExtents()
		ascent := roundMetric(pr.Float(extents.Ascender) * pr.Float(style.FontDescription.Size) / pr.Float(run.Face.Upem()))
		descent := roundMetric(pr.Float(extents.Descender) * pr.Float(style.FontDescription.Size) / pr.Float(run.Face.Upem()))

		bottom := top - height

		if ascent > top {
			top = ascent
		}
		if descent < bottom {
			bottom = descent
		}
		height = top - bottom
	}

	if fitsOnFirstLine && spaceCollapse {
		// weasyprint puts the collapsed space on the line...
		// gopdfgo: porównanie z szerokością bez tolerancji zaokrągleń, żeby zwrócona szerokość wiersza
		// nie przekraczała dostępnej
		if fixedToFloat(width+wLine.TrimmedTrailingWhitespace) <= wrapingMaxWidth {
			width += wLine.TrimmedTrailingWhitespace
			// in RTL direction, the collapsed char must be expanded again
			// Note that the line is already sorted in visual order
			if dir.Progression() == di.TowardTopLeft {
				glyphs := outLine[0].Glyphs
				if len(glyphs) != 0 {
					glyphs[0].Advance += wLine.TrimmedTrailingWhitespace
				}
			}
		}
	}

	var hang fixed.Int26_6
	if !fitsOnFirstLine && !mandatoryBreak && !spaceCollapse && textWrap && !style.breakSpaces() && dir.Progression() == di.FromTopLeft {
		// gopdfgo: przy white-space: pre-wrap spacje i tabulatory, po których wiersz został zawinięty,
		// wiszą: zachowują szerokość, ale nie liczą się do przepełnienia ani wyrównania wiersza
		// (CSS Text 3 § 4.1.3).
		// gopdfgo: także odstępy kategorii Zs inne niż spacja, przez wszystkie przebiegi (zob. TrailingSpacesWidth)
	runs:
		for r := len(outLine) - 1; r >= 0; r-- {
			glyphs := outLine[r].Glyphs
			for i := len(glyphs) - 1; i >= 0; i-- {
				index := glyphs[i].ClusterIndex
				if index < 0 || index >= len(text) || !isHangingWhiteSpace(text[index]) {
					break runs
				}
				hang += glyphs[i].Advance
			}
		}
	} else if hasIdeographic && dir.Progression() == di.FromTopLeft {
		// gopdfgo: ciąg odstępów bywa złożony kilkoma fontami (odstępu brak w foncie podstawowym),
		// więc liczymy go przez wszystkie przebiegi od końca wiersza, nie tylko w ostatnim
		glyphs := outLine[len(outLine)-1].Glyphs
		if n := len(glyphs); n != 0 && glyphs[n-1].ClusterIndex < len(text) && IsOtherSpaceSeparator(text[glyphs[n-1].ClusterIndex]) {
		otherRuns:
			for r := len(outLine) - 1; r >= 0; r-- {
				glyphs := outLine[r].Glyphs
				for i := len(glyphs) - 1; i >= 0; i-- {
					if index := glyphs[i].ClusterIndex; index < 0 || index >= len(text) || (text[index] != ' ' && !IsOtherSpaceSeparator(text[index])) {
						break otherRuns
					}
					hang += glyphs[i].Advance
				}
			}
		}
	}

	// gopdfgo: wiersz przyjęty dzięki tolerancji zaokrągleń (zob. LineWrap) zgłasza szerokość nie
	// większą niż dostępna; inaczej układ wiersza uznałby go za przepełniony i złamał wcześniej.
	lineWidth := fixedToFloat(width)
	if used := fixedToFloat(width - hang); textWrap && used > wrapingMaxWidth && used <= wrapingMaxWidth+0.5+1e-6 {
		lineWidth = wrapingMaxWidth + fixedToFloat(hang)
	}

	out := FirstLine{
		Hang:         fixedToFloat(hang),
		Layout:       &TextLayoutGotext{text: text[:firstLineLength], Style: style, Line: outLine, MaxWidth: maxWidth},
		Length:       firstLineLength,
		ResumeAt:     resumeAt,
		FirstLineRTL: style.Direction == pr.Rtl,
		Width:        lineWidth,
		Height:       height,
		Baseline:     top,
	}

	fc.textLayoutCache[key] = out

	return privateLayout(out)
}

func (fc *FontConfigurationGotext) LineWrap(text []rune, style *TextStyle, runs shaping.Line, maxWidth pr.Float, allowWordBreak bool) (usedMaxWidth fixed.Int26_6, _ shaping.WrappedLine, _ bool) {
	dir := di.DirectionLTR
	if style.Direction == pr.Rtl {
		dir = di.DirectionRTL
	}

	// convert to fixed; properly handling large overflow
	const maxFixed = math.MaxInt32 >> 6 // max value for fixed.Int26_6
	mw := fixed.I(maxFixed)
	if maxWidth <= maxFixed {
		mw = floatToFixed(pr.Fl(maxWidth))
	}
	exact := mw

	spaceCollapse := style.spaceCollapse()
	config := shaping.WrapConfig{
		Direction:                     dir,           // overall direction of the text
		BreakPolicy:                   shaping.Never, // mimic the default pango behavior
		DisableTrailingWhitespaceTrim: true,          // gopdfgo: przycinamy sami, niżej
	}
	if allowWordBreak {
		config.BreakPolicy = shaping.Always
	}

	// gopdfgo: UAX #14 pozwala łamać wewnątrz ciągu białych znaków: między spacją a następującym po
	// niej tabulatorem albo odstępem innym niż spacja (LB18), przed spacją ideograficzną zawsze. Ciąg
	// białych znaków na końcu wiersza wisi jednak w całości, a wiersz łamie się dopiero po nim (CSS
	// Text 3 § 4.1.3, § 5.1; tak robi Chromium, testy WPT pre-wrap-tab-*,
	// trailing-other-space-separators-*). Wyznaczaniu miejsc podziału podajemy więc tekst, w którym
	// każdy ciąg co najmniej dwóch białych znaków (spacje, odstępy kategorii Zs, przy pre-wrap także
	// tabulatory) jest zamieniony na spacje; kształty i szerokości zostają bez zmian.
	breakText := text
	if !style.breakSpaces() {
		breakText = unifyWhiteSpaceRuns(text, !spaceCollapse)
	}
	wrap := func(limit fixed.Int26_6) (shaping.WrappedLine, bool) {
		fc.lineWrapper.Prepare(config, breakText, shaping.NewSliceIterator(runs))
		line, done := fc.lineWrapper.WrapNextLineF(limit)
		if spaceCollapse {
			trimTrailingSpace(&line, text, dir)
		}
		return line, done
	}
	// gopdfgo: szerokości znaków są zaokrąglane do 1/64 piksela każda z osobna, więc suma wiersza
	// odbiega od dokładnej: dla fontu o stałej szerokości systematycznie, do ćwierć piksela na 80
	// znakach. N znaków w pudełku o szerokości N ch bywało przez to „za szerokie” i ostatni znak spadał
	// do następnego wiersza. Zawijamy więc z zapasem pół piksela, a wiersz, który z zapasu skorzystał,
	// przyjmujemy tylko wtedy, gdy jego dokładna szerokość (zaokrąglona plus reszty zaokrągleń)
	// mieści się w dostępnej; inaczej próbujemy krótszego.
	const slack = 32
	if fc.residuals == nil || len(fc.residuals) != len(text) || maxWidth > maxFixed {
		line, done := wrap(exact)
		return exact, line, done
	}
	// gopdfgo: go-text pozwala wystawać poza wiersz tylko ostatniemu glifowi bez obrysu, a cały ciąg
	// białych znaków na końcu wiersza wisi (CSS Text 3 § 4.1.3): przy pre-wrap spacje i tabulatory,
	// zawsze odstępy kategorii Zs inne niż spacja. Gdy w tekście jest ciąg co najmniej dwóch takich
	// znaków, zawijamy z zapasem jego szerokości i przyjmujemy wiersz, którego treść bez końcowych
	// odstępów się mieści; inaczej próbujemy krótszego (testy WPT pre-wrap-002,
	// trailing-other-space-separators-*).
	if extra := hangingExtra(runs, text, style, dir); extra > 0 {
		limit, previous := exact+slack+extra, -1
		for attempt := 0; attempt < 8; attempt++ {
			line, done := wrap(limit)
			rounded, precise := fc.contentWidth(line.Line, text)
			if rounded <= exact {
				return exact, line, done
			}
			if rounded <= exact+slack && float64(precise) <= float64(maxWidth)*64*shapingPrecision+1 {
				return rounded, line, done
			}
			if line.NextLine == previous {
				break // krótszego wiersza nie ma
			}
			previous, limit = line.NextLine, rounded-1
		}
		line, done := wrap(exact)
		return exact, line, done
	}
	limit := exact + slack
	for attempt := 0; attempt < 4; attempt++ {
		line, done := wrap(limit)
		rounded, precise := fc.contentWidth(line.Line, text)
		if rounded <= exact {
			return exact, line, done
		}
		if rounded <= exact+slack && float64(precise) <= float64(maxWidth)*64*shapingPrecision+1 {
			return rounded, line, done
		}
		if rounded > exact+slack {
			break // jedno słowo szersze niż wiersz: tolerancja nic nie zmienia
		}
		limit = rounded - 1
	}
	line, done := wrap(exact)
	return exact, line, done
}

// hangingExtra (gopdfgo) zwraca szerokość najdłuższego w tekście ciągu co najmniej dwóch wiszących
// białych znaków (zob. isHangingWhiteSpace) albo ciągu zakończonego znakiem nowego wiersza, albo
// zero. Spacje wiszą tylko przy pre-wrap; przy break-spaces nic nie wisi, a w zapisie od prawej do
// lewej zostawiamy zachowanie biblioteki.
func hangingExtra(runs shaping.Line, text []rune, style *TextStyle, dir di.Direction) fixed.Int26_6 {
	if style.breakSpaces() || dir.Progression() != di.FromTopLeft {
		return 0
	}
	preserved := !style.spaceCollapse()
	var best, current fixed.Int26_6
	count, newline := 0, false
	for _, run := range runs {
		for _, glyph := range run.Glyphs {
			index := glyph.ClusterIndex
			if index < 0 || index >= len(text) {
				current, count, newline = 0, 0, false
				continue
			}
			// znak nowego wiersza kończy ciąg; jego glif (często .notdef z obrysem) też nie może
			// zmuszać do złamania przed nim
			if r := text[index]; r == '\n' || isHangingWhiteSpace(r) && (preserved || r != ' ') {
				current += glyph.Advance
				count++
				newline = newline || r == '\n'
				if (count >= 2 || newline) && current > best {
					best = current
				}
				continue
			}
			current, count, newline = 0, 0, false
		}
	}
	return best
}

// contentWidth zwraca szerokość treści wiersza bez końcowych odstępów: zaokrągloną (1/64 px) oraz
// dokładną, z resztami zaokrągleń (1/4096 px).
func (fc *FontConfigurationGotext) contentWidth(line shaping.Line, text []rune) (rounded fixed.Int26_6, precise int64) {
	trailing := true
	for r := len(line) - 1; r >= 0; r-- {
		glyphs := line[r].Glyphs
		for g := len(glyphs) - 1; g >= 0; g-- {
			index := glyphs[g].ClusterIndex
			inText := index >= 0 && index < len(text)
			if trailing && inText && unicode.IsSpace(text[index]) {
				continue
			}
			trailing = false
			rounded += glyphs[g].Advance
			precise += int64(glyphs[g].Advance) * shapingPrecision
			if inText && glyphs[g].Advance != 0 && text[index] != '\t' && index < len(fc.residuals) && (g == 0 || glyphs[g-1].ClusterIndex != index) {
				precise += int64(fc.residuals[index])
			}
		}
	}
	return rounded, precise
}

// trimTrailingSpace odbiera szerokość spacji (U+0020) kończącej wiersz i zapisuje ją w
// TrimmedTrailingWhitespace.
//
// gopdfgo: biblioteka go-text robi to samo dla każdego ostatniego glifu bez obrysu, czyli także dla
// spacji nierozdzielającej, ideograficznej i innych odstępów, które nie są zwijalnymi białymi znakami
// i na końcu wiersza zachowują szerokość (CSS Text 3 § 4.1.3). Dlatego jej przycinanie jest wyłączone.
func trimTrailingSpace(wrapped *shaping.WrappedLine, text []rune, dir di.Direction) {
	line := wrapped.Line
	if len(line) == 0 {
		return
	}
	rtl := dir.Progression() == di.TowardTopLeft
	goal := int32(len(line) - 1)
	if rtl {
		goal = 0
	}
	for i := range line {
		run := &line[i]
		if run.VisualIndex != goal || len(run.Glyphs) == 0 || run.Direction.IsVertical() {
			continue
		}
		glyph := &run.Glyphs[len(run.Glyphs)-1]
		if rtl {
			glyph = &run.Glyphs[0]
		}
		if index := glyph.ClusterIndex; glyph.Width == 0 && index >= 0 && index < len(text) && text[index] == ' ' {
			// kopia: glify są wspólne z wejściem zawijania, które bywa używane ponownie
			run.Glyphs = slices.Clone(run.Glyphs)
			glyph = &run.Glyphs[len(run.Glyphs)-1]
			if rtl {
				glyph = &run.Glyphs[0]
			}
			before := run.Advance
			glyph.XAdvance, glyph.Advance = 0, 0
			run.RecomputeAdvance()
			wrapped.TrimmedTrailingWhitespace = before - run.Advance
		}
		return
	}
}

// splitFirstLineGotext fit as much text from [text_] as possible in the available width given by [maxWidth].
// minimum should defaults to false
func (fc *FontConfigurationGotext) splitFirstLine(hyphenCache map[HyphenDictKey]hyphen.Hyphener, text []rune, style *TextStyle,
	maxWidth pr.MaybeFloat, minimum, isLineStart bool,
) FirstLine {
	// See https://www.w3.org/TR/css-text-3/#white-space-property
	var (
		textWrap         = style.textWrap()
		originalMaxWidth = maxWidth
		fontSize         = pr.Float(style.Size)
		firstLine        FirstLine
	)
	if !textWrap {
		maxWidth = nil
	}
	// Step #1: Get a draft layout with the first line
	if maxWidth, ok := maxWidth.(pr.Float); ok && maxWidth != pr.Inf && fontSize != 0 {
		// Try to use a small amount of text instead of the whole text
		shortText := shortTextHint(text, maxWidth, fontSize)

		firstLine = fc.wrap(shortText, style, maxWidth)
		if firstLine.ResumeAt == -1 && len(shortText) != len(text) {
			// The small amount of text fits in one line, give up and use the whole text
			firstLine = fc.wrap(text, style, maxWidth)
		} else {
			// If the second line of the short text can break, we have the next
			// line break point required for step #3 in it, drop the end of the text.
			if firstLine.ResumeAt != -1 && firstLine.ResumeAt != len(shortText) {
				firstLineText := shortText[:firstLine.ResumeAt]
				start := len(firstLineText) + 1
				if fc.getNextBreakPoint(shortText[start:]) != -1 {
					text = shortText
				}
			}
		}
	} else {
		originalMaxW := pr.Inf
		if originalMaxWidth != nil {
			originalMaxW = originalMaxWidth.V()
		}
		firstLine = fc.wrap(text, style, originalMaxW)
	}

	// Step #2: Don't split lines when it's not needed
	if maxWidth == nil || len(text) == 0 {
		// The first line can take all the place needed
		return firstLine
	}
	maxWidthV := maxWidth.V()

	if firstLine.ResumeAt == -1 && firstLine.Width-firstLine.Hang <= maxWidthV {
		// The first line really fits in the available width
		return firstLine
	}

	// Step #3: Try to put the first word of the second line on the first line
	// https://mail.gnome.org/archives/gtk-i18n-list/2013-September/msg00006
	// is a good thread related to this problem.
	var firstLineText, secondLineText []rune
	if firstLine.Width-firstLine.Hang <= maxWidthV {
		// the first line fits, but,
		//	since we wrap without using work breaks, the first word of the second line
		// 	could, after hyphenation, fit (partially) on the first line
		if firstLine.ResumeAt != -1 {
			firstLineText, secondLineText = text[:firstLine.ResumeAt], text[firstLine.ResumeAt:]
		}
	} else {
		// the first line is too long (only possible with one word)
		firstLineText = []rune{}
		secondLineText = text
	}

	breakPoint := len(secondLineText)
	if len(secondLineText) != 0 {
		// Find the second line’s first break point.
		breakPoint = fc.getNextBreakPoint(secondLineText)
		if breakPoint == -1 {
			breakPoint = len(secondLineText)
		}
	}

	nextWord := trimTrailingSpaces(secondLineText[:breakPoint])
	if len(nextWord) != 0 {
		if style.spaceCollapse() && hasSuffix(secondLineText[:breakPoint], ' ') {
			// Next word might fit without a space afterwards; only try when
			// space collapsing is allowed.
			newFirstLineText := slices.Concat(firstLineText, nextWord)
			firstLine = fc.wrap(newFirstLineText, style, maxWidthV)
			if firstLine.ResumeAt == -1 {
				if len(firstLineText) != 0 {
					// The next word fits in the first line, keep the layout.
					return firstLine
				} else {
					// Second line is None.
					firstLine.ResumeAt = firstLine.Length + 1
					if firstLine.ResumeAt >= len(text) {
						firstLine.ResumeAt = -1
					}
				}
			}
		}
	} else if len(firstLineText) != 0 {
		// We found something on the first line but we did not find a word on
		// the next line, no need to hyphenate, we can keep the current layout.
		return firstLine
	}

	// Step #4: Try to hyphenate
	hyphens := style.Hyphens
	lang := language.NewLanguage(style.Lang)
	if lang != "" {
		lang = hyphen.LanguageFallback(lang)
	}
	hyphenLimit := style.HyphenateLimitChars
	hyphenateCharacter := []rune(style.HyphenateCharacter)
	hyphenated := false
	const softHyphen = '\u00ad'

	autoHyphenation, manualHyphenation := false, false
	if hyphens != HNone {
		manualHyphenation = index(firstLineText, softHyphen) != -1 || index(nextWord, softHyphen) != -1
	}

	var startWord, stopWord, nextTextIndex int
	if hyphens == HAuto && lang != "" {
		// Get text until next line break opportunity.
		nextText := secondLineText
		if nextBreakPoint := fc.getNextBreakPoint(secondLineText); nextBreakPoint != -1 {
			nextText = nextText[:nextBreakPoint]
		}

		// Try all words included in this text.
		for len(nextText) != 0 {
			nextWordBoundaries := fc.wordBoundaries(nextText)
			if nextWordBoundaries != nil {
				// We have a word to hyphenate
				startWord, stopWord = nextWordBoundaries[0], nextWordBoundaries[1]
				nextWord = nextText[startWord:stopWord]
				if stopWord-startWord >= hyphenLimit.Total {
					// This word is long enough
					space := pr.Fl(maxWidthV - firstLine.Width + firstLine.Hang)
					zone := style.HyphenateLimitZone
					limitZone := zone.Limit
					if zone.IsPercentage {
						limitZone = (pr.Fl(maxWidthV) * zone.Limit / 100.)
					}
					if space > limitZone || space < 0 {
						// Available space is worth the try, or the line is even too
						// long to fit: try to hyphenate
						autoHyphenation = true
						nextTextIndex += startWord
						break
					}
				}

				// This word doesn’t work, try next one.
				nextText = nextText[stopWord:]
				nextTextIndex += stopWord
			} else {
				break
			}
		}
	}

	// Automatic hyphenation opportunities within a word must be ignored if the
	// word contains a conditional hyphen, in favor of the conditional
	// hyphen(s).
	// See https://drafts.csswg.org/css-text-3/#valdef-hyphens-auto
	var dictionaryIterations []string
	if manualHyphenation {
		// Manual hyphenation: check that the line ends with a soft
		// hyphen and add the missing hyphen
		if hasSuffix(firstLineText, softHyphen) {
			// The first line has been split on a soft hyphen
			if i := lastIndex(firstLineText, ' '); i != -1 {
				firstLineText, nextWord = firstLineText[:i], firstLineText[i:] // next word start with a space
				firstLine = fc.wrap(firstLineText, style, maxWidthV)
				firstLine.ResumeAt = len(firstLineText) + 1 // track the space we have remove
			} else {
				firstLineText, nextWord = nil, firstLineText
			}
		}
		dictionaryIterations = hyphenDictionaryIterations(nextWord, softHyphen)
	} else if autoHyphenation {
		dictionaryKey := HyphenDictKey{lang, hyphenLimit}
		dictionary, ok := hyphenCache[dictionaryKey]
		if !ok {
			dictionary = hyphen.NewHyphener(lang, hyphenLimit.Left, hyphenLimit.Right)
			hyphenCache[dictionaryKey] = dictionary
		}
		previousWords := secondLineText[:nextTextIndex]
		dictionaryIterations = dictionary.IterateRunes(nextWord, string(previousWords))
	}

	var hyphenatedFirstLineText []rune
	if len(dictionaryIterations) != 0 {
		var newFirstLineText []rune
		for _, firstWordPart := range dictionaryIterations {
			newFirstLineText = slices.Concat(firstLineText, []rune(firstWordPart))
			hyphenatedFirstLineText = append(newFirstLineText, hyphenateCharacter...)
			newFirstLine := fc.wrap(hyphenatedFirstLineText, style, maxWidthV)
			newSpace := maxWidthV - newFirstLine.Width + newFirstLine.Hang
			hyphenated = newFirstLine.ResumeAt == -1 && (newSpace >= 0 || firstWordPart == dictionaryIterations[len(dictionaryIterations)-1])
			if hyphenated {
				firstLine = newFirstLine
				firstLine.ResumeAt = len(newFirstLineText)
				break
			}
		}

		if !hyphenated && len(firstLineText) == 0 {
			// Recreate the layout with no maxWidth to be sure that
			// we don't break before or inside the hyphenate character
			hyphenated = true
			firstLine = fc.wrap(hyphenatedFirstLineText, style, pr.Inf)
			firstLine.ResumeAt = len(newFirstLineText)
			if text[firstLine.ResumeAt] == softHyphen {
				firstLine.ResumeAt += 1
			}
		}
	}

	if !hyphenated && hasSuffix(firstLineText, softHyphen) {
		// Recreate the layout with no maxWidth to be sure that
		// we don't break inside the hyphenate-character string
		hyphenated = true
		hyphenatedFirstLineText = slices.Concat(firstLineText, hyphenateCharacter)
		firstLine = fc.wrap(hyphenatedFirstLineText, style, pr.Inf)
		firstLine.ResumeAt = len(firstLineText)
	}

	// Step #5: Try to break word if it's too long for the line
	overflowWrap, wordBreak := style.OverflowWrap, style.WordBreak
	space := maxWidthV - firstLine.Width + firstLine.Hang
	// If we can break words and the first line is too long
	canBreak := wordBreak != WBNormal ||
		(isLineStart && (overflowWrap == OAnywhere || (overflowWrap == OBreakWord && !minimum)))
	// gopdfgo: przy word-break: break-all każdy znak jest miejscem łamania (CSS Text 3 § 5.2), więc wiersz
	// wypełniamy do końca także wtedy, gdy wcześniej jest granica wyrazu; w źródle słowo było łamane dopiero
	// wtedy, gdy samo nie mieściło się w wierszu.
	breakAll := wordBreak != WBNormal && firstLine.ResumeAt != -1 && maxWidthV != pr.Inf
	if (space < 0 && canBreak) || breakAll {
		// Is it really OK to remove hyphenation for word-break ?
		hyphenated = false
		firstLine = fc.WrapWordBreak(text, style, maxWidthV, true)
		// gopdfgo: word-break: break-all pozwala łamać między literami i cyframi (traktowanymi jak
		// znaki ideograficzne), ale reguły dla znaków przestankowych zostają (CSS Text 3 § 5.2):
		// wiersz nie zaczyna się od kropki ani od spacji nierozdzielającej. Biblioteka łamie w dowolnym
		// miejscu, więc cofamy podział do najbliższego dozwolonego.
		if wordBreak == WBBreakAll && firstLine.ResumeAt > 0 && firstLine.ResumeAt < len(text) {
			at := firstLine.ResumeAt
			for at > 0 && !fc.breakAllAllowed(text[at-1], text[at]) {
				at--
			}
			if at > 0 && at < firstLine.ResumeAt {
				firstLine = fc.wrap(text[:at], style, pr.Inf)
				firstLine.ResumeAt = at
			}
		}
	}

	if hyphenated {
		firstLine.Length -= len(hyphenateCharacter)
	}

	return firstLine
}

// tabs support

func applyTabs(out *shaping.Output, text []rune, columnWidth, runStart, threshold fixed.Int26_6) {
	columnWidthF := float64(columnWidth) / 64
	var advance fixed.Int26_6
	for i, g := range out.Glyphs {
		gAdvance := g.Advance
		isTab := g.RunesCount() == 1 && g.GlyphsCount() == 1 && text[g.TextIndex()] == '\t'
		if !isTab {
			advance += gAdvance
			continue
		}

		var updatedTabAdvance fixed.Int26_6
		if columnWidth == 0 {
			// simply trim the advance, nothing else to do
		} else {
			// update the advance of the glyph so that the next glyph is "tab-aligned" :
			// we want the "end" of the tab to be a multiple of columnWidth, that is :
			// (runStart + advance + updatedTabAdvance) % columnWith == 0
			glyphStartF := float64(runStart+advance) / 64
			remainder := math.Mod(glyphStartF, columnWidthF)
			toNextStop := columnWidthF - remainder
			// gopdfgo: gdy do pozycji tabulatora brakuje mniej niż pół znaku, obowiązuje następna
			// (CSS Text 3 § 4.2). W źródle tekst kończący się dokładnie na pozycji tabulatora, z błędem
			// zaokrąglenia tuż przed nią, dostawał tabulator o szerokości bliskiej zera i kolumny się zlewały.
			// Pół znaku to połowa szerokości cyfry zero (jednostka ch); odejmujemy 1/64 piksela na
			// błąd zaokrąglenia szerokości.
			if toNextStop < float64(threshold-1)/64 {
				toNextStop += columnWidthF
			}
			updatedTabAdvance = fixed.Int26_6(toNextStop * 64)
		}

		out.Glyphs[i].Advance = updatedTabAdvance

		advance += updatedTabAdvance
	}

	// no need to call RecomputeAdvance
	out.Advance = advance
}

// AlignTabs updates the advance of glyphs mapped to '\t' runes,
// so that tabs are aligned on columns defined by [columnWidth].
//
// [lineOffset] may be non zero if the line starts after the first column.
//
// As a special case, if [columnWidth] is zero,
// tabs are trimmed (their advance is set to 0).
func AlignTabs(l shaping.Line, text []rune, columnWidth, lineOffset, threshold fixed.Int26_6) {
	runsAdvance := lineOffset // the position of the start of the current run
	for i := range l {
		applyTabs(&l[i], text, columnWidth, runsAdvance, threshold)
		runsAdvance += l[i].Advance
	}
}

// shapingPrecision to mnożnik rozmiaru fontu na czas kształtowania tekstu.
const shapingPrecision = 64

// shapePrecisely kształtuje tekst w rozmiarze podanym z dokładnością do 1/64 piksela.
//
// gopdfgo: biblioteka go-text zaokrągla rozmiar fontu w górę do całych pikseli (Size.Ceil()), więc tekst
// 13,33 px (10 pt) był składany z szerokościami znaków jak dla 14 px, o 5% za szeroko. Kształtujemy więc
// w rozmiarze 64 razy większym i dzielimy wynik; błąd zaokrąglenia spada poniżej 0,1%. W źródle ten sam
// zabieg był stosowany tylko przy pomiarze pojedynczych znaków (shapeRune).
func shapePrecisely(shaper *shaping.HarfbuzzShaper, input shaping.Input) shaping.Output {
	return shapeWithResiduals(shaper, input, nil)
}

// shapeWithResiduals działa jak shapePrecisely i dodatkowo dopisuje do residuals (indeks: numer
// znaku w tekście) to, co zaokrąglenie odjęło od szerokości znaku, w jednostkach 1/4096 piksela.
// Suma szerokości zaokrąglonych i reszt daje dokładną szerokość wiersza (zob. LineWrap).
func shapeWithResiduals(shaper *shaping.HarfbuzzShaper, input shaping.Input, residuals []int32) shaping.Output {
	size := input.Size
	input.Size = size * shapingPrecision
	out := shaper.Shape(input)
	scale := func(v fixed.Int26_6) fixed.Int26_6 {
		if v < 0 {
			return -((-v + shapingPrecision/2) / shapingPrecision)
		}
		return (v + shapingPrecision/2) / shapingPrecision
	}
	for i := range out.Glyphs {
		g := &out.Glyphs[i]
		g.Width, g.Height = scale(g.Width), scale(g.Height)
		g.XBearing, g.YBearing = scale(g.XBearing), scale(g.YBearing)
		if residuals != nil && g.ClusterIndex >= 0 && g.ClusterIndex < len(residuals) {
			residuals[g.ClusterIndex] += int32(g.Advance - scale(g.Advance)*shapingPrecision)
		}
		g.Advance, g.XAdvance, g.YAdvance = scale(g.Advance), scale(g.XAdvance), scale(g.YAdvance)
		g.XOffset, g.YOffset = scale(g.XOffset), scale(g.YOffset)
	}
	out.Size = size
	out.LineBounds.Ascent = scale(out.LineBounds.Ascent)
	out.LineBounds.Descent = scale(out.LineBounds.Descent)
	out.LineBounds.Gap = scale(out.LineBounds.Gap)
	out.RecalculateAll()
	return out
}

// roundMetric zaokrągla wysokość nad i pod linią bazową do całych pikseli CSS.
//
// gopdfgo: dodane. Specyfikacja nie określa dokładności metryk fontu, a przeglądarki zaokrąglają je
// do pikseli, dzięki czemu wiersze i wszystko pod nimi leżą na siatce pikseli. Z metrykami ułamkowymi
// każdy blok pod akapitem tekstu zaczynał się w ułamku piksela.
func roundMetric(v pr.Float) pr.Float {
	return pr.Float(math.Round(float64(v)))
}

// spacesInOwnFace (gopdfgo) wydziela spacje z przebiegów złożonych fontem
// zastępczym. Segmentacja go-text dokleja odstępy do bieżącego przebiegu, więc
// spacja po znaku spoza fontu podstawowego („◦ ”, „• ”, znak CJK) dostawała
// szerokość z fontu zastępczego. Według CSS Fonts 4 § 5.3 font dobiera się dla
// każdego znaku osobno, zaczynając od początku listy rodzin, czyli spacja
// należy do pierwszego fontu, który ją ma.
func (fc *FontConfigurationGotext) spacesInOwnFace(inputs []shaping.Input) []shaping.Input {
	var spaceFace *font.Face
	needsSplit := false
	for _, input := range inputs {
		for _, r := range input.Text[input.RunStart:input.RunEnd] {
			if r != ' ' && r != '\u00a0' {
				continue
			}
			if spaceFace == nil {
				spaceFace = fc.fm.ResolveFace(' ')
			}
			if spaceFace != nil && input.Face != spaceFace {
				needsSplit = true
			}
			break
		}
	}
	if !needsSplit {
		return inputs
	}
	out := make([]shaping.Input, 0, len(inputs)+2)
	for _, input := range inputs {
		if input.Face == spaceFace {
			out = append(out, input)
			continue
		}
		start := input.RunStart
		isSpace := func(i int) bool { r := input.Text[i]; return r == ' ' || r == '\u00a0' }
		for i := input.RunStart + 1; i <= input.RunEnd; i++ {
			if i < input.RunEnd && isSpace(i) == isSpace(start) {
				continue
			}
			piece := input
			piece.RunStart, piece.RunEnd = start, i
			if isSpace(start) {
				piece.Face = spaceFace
			}
			out = append(out, piece)
			start = i
		}
	}
	return out
}

// breakAllAllowed mówi, czy przy word-break: break-all wolno złamać wiersz między znakami a i b:
// litery i cyfry liczą się jak znaki ideograficzne, reszta według UAX 14.
func (fc *FontConfigurationGotext) breakAllAllowed(a, b rune) bool {
	ideograph := func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return '\u4e00'
		}
		return r
	}
	return fc.CanBreakText([]rune{ideograph(a), ideograph(b)}) == pr.True
}

// IsOtherSpaceSeparator (gopdfgo): odstępy kategorii Zs inne niż spacja i spacja nierozdzielająca
// (U+1680, U+2000–U+200A, U+202F, U+205F, U+3000). Nie są zwijalne, ale na końcu wiersza wiszą
// (CSS Text 3 § 4.1.3).
func IsOtherSpaceSeparator(r rune) bool {
	return r != ' ' && r != '\u00a0' && unicode.Is(unicode.Zs, r)
}

// isPreservedSpace: spacja zwykła i ideograficzna (U+3000), które na końcu wiersza wiszą albo, przy
// break-spaces, przechodzą do następnego wiersza (CSS Text 3 § 4.1.3).
func isPreservedSpace(r rune) bool { return r == ' ' || r == '\u3000' }

// unifyWhiteSpaceRuns (gopdfgo) zwraca tekst, w którym każdy ciąg co najmniej dwóch białych znaków
// (spacja, odstępy kategorii Zs poza spacją nierozdzielającą, z tabs także tabulator), który nie
// składa się wyłącznie ze spacji i zawiera znak, po którym wolno łamać, jest zamieniony na spacje.
// Ciągi samych odstępów nierozdzielających (U+2007, U+202F) zostają. Gdy nie ma czego zmieniać,
// zwraca text bez kopiowania.
func unifyWhiteSpaceRuns(text []rune, tabs bool) []rune {
	isWhite := func(r rune) bool { return r == ' ' || IsOtherSpaceSeparator(r) || tabs && r == '\t' }
	out := text
	for i := 0; i < len(text); {
		if !isWhite(text[i]) {
			i++
			continue
		}
		j, other, breaking := i, false, false
		for j < len(text) && isWhite(text[j]) {
			other = other || text[j] != ' '
			breaking = breaking || text[j] == ' ' || text[j] == '\t' || isBreakingOtherSpace(text[j])
			j++
		}
		if j-i >= 2 && other && breaking {
			if &out[0] == &text[0] {
				out = slices.Clone(text)
			}
			for k := i; k < j; k++ {
				out[k] = ' '
			}
		}
		i = j
	}
	return out
}

// isHangingWhiteSpace (gopdfgo): znaki, które przy pre-wrap na końcu wiersza wiszą: spacja,
// tabulator i odstępy kategorii Zs inne niż spacja (CSS Text 3 § 4.1.3).
func isHangingWhiteSpace(r rune) bool { return r == ' ' || r == '\t' || IsOtherSpaceSeparator(r) }

// isBreakingOtherSpace (gopdfgo): odstępy inne niż spacja, po których UAX #14 pozwala łamać wiersz
// (klasy BA i ID); bez nierozdzielających U+2007 i U+202F.
func isBreakingOtherSpace(r rune) bool {
	return IsOtherSpaceSeparator(r) && r != '\u2007' && r != '\u202F'
}

// isBreakSpace (gopdfgo): zachowany biały znak, po którym przy white-space: break-spaces wolno złamać
// wiersz: spacja, spacja ideograficzna i tabulator.
func isBreakSpace(r rune) bool { return isPreservedSpace(r) || r == '\t' }
