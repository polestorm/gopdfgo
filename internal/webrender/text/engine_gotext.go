package text

import (
	"encoding/hex"
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

	fontsContent  map[string][]byte          // to be embedded in the target
	fontsFeatures map[*font.Font][]Feature   // as requested by @font-face
	loadedFaces   map[string]bool            // gopdfgo: reguły @font-face już wczytane (zob. loadOneFont)
	fontsMetrics  map[*font.Font]faceMetrics // gopdfgo: deskryptory metryk z @font-face, zob. faceMetrics
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

	// familyIndex (gopdfgo): nazwy rodzin fontów dopasowywane według CSS, zob. family_names.go
	familyIndex familyIndex

	// familyKnown (gopdfgo): pamięć wyników familyAvailable
	familyKnown map[string]bool
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
	if len(ruleDescriptors.UnicodeRange) != 0 {
		// gopdfgo: twarze z różnymi unicode-range to różne twarze (zob. rangeCmap)
		key += fmt.Sprintf("+%v", ruleDescriptors.UnicodeRange)
	}
	metrics := newFaceMetrics(ruleDescriptors)
	if metrics != (faceMetrics{sizeAdjust: 1}) {
		// gopdfgo: tak samo twarze z różnymi deskryptorami metryk
		key += fmt.Sprintf("+%v", metrics)
	}

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

	if len(ruleDescriptors.UnicodeRange) != 0 {
		ft.Cmap = rangeCmap{ft.Cmap, ruleDescriptors.UnicodeRange}
	}
	f.fm.AddFace(font.NewFace(ft), fontscan.Location{File: key}, desc)
	// gopdfgo: style i grubości twarzy dla dopasowania i syntezy, zob. family_names.go
	synthesisWeight := uint16(desc.Aspect.Weight)
	if r := ruleDescriptors.FontWeightRange; r[1] != 0 {
		synthesisWeight = uint16(r[1])
	} else if synthesisWeight == 0 {
		synthesisWeight = uint16(ft.Describe().Aspect.Weight)
	}
	var fixed uint8
	if ruleDescriptors.FontStyle != "" {
		fixed |= axisSlant
	}
	if desc.Aspect.Weight != 0 {
		fixed |= axisWeight
	}
	f.familyIndex.addFace(desc.Family, newFontStyle(ruleDescriptors.FontStyle), synthesisWeight, fixed, ft)
	f.familyKnown = nil // gopdfgo: nowa rodzina z @font-face może zmienić wynik familyAvailable
	if metrics != (faceMetrics{sizeAdjust: 1}) {
		if f.fontsMetrics == nil {
			f.fontsMetrics = map[*font.Font]faceMetrics{}
		}
		f.fontsMetrics[ft] = metrics
	}

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
	f.familyIndex.fontIDs = append(f.familyIndex.fontIDs, fileID) // gopdfgo: zob. family_names.go
	f.familyIndex.built = false
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
	justifyStart  int  // gopdfgo: spacje przed tym znakiem nie dostają odstępu justowania
	// gopdfgo: odstęp justowania między jednostkami typograficznymi (text-justify: inter-character)
	charJustification pr.Float

	// gopdfgo: odstęp między literami za ostatnim znakiem tekstu, który znika, gdy wiersz kończy się
	// na tym pudełku (CSS Text 3 § 7.2), zob. TrimTrailingLetterSpacing
	trailingSpacing fixed.Int26_6
}

// TrimTrailingLetterSpacing (gopdfgo) zdejmuje odstęp między literami za ostatnim znakiem tekstu,
// gdy wiersz kończy się na tym pudełku: na końcu wiersza odstępu nie ma (CSS Text 3 § 7.2). Zwraca
// zdjętą szerokość.
func (l *TextLayoutGotext) TrimTrailingLetterSpacing() pr.Float {
	ls := l.trailingSpacing
	if ls == 0 || len(l.Line) == 0 {
		return 0
	}
	l.Line = slices.Clone(l.Line)
	addToLastGlyph(l.Line, l.text, -ls, false)
	l.trailingSpacing = 0
	return fixedToFloat(ls)
}

// TrailingLetterSpacing (gopdfgo) zwraca odstęp między literami za ostatnim znakiem tekstu (zob.
// TrimTrailingLetterSpacing).
func (l *TextLayoutGotext) TrailingLetterSpacing() pr.Float { return fixedToFloat(l.trailingSpacing) }

// addToLastGlyph (gopdfgo) dodaje delta do szerokości ostatniego glifu wiersza, na kopii glifów
// (są wspólne z pamięcią podręczną układów). Z skipIgnorable pomija końcowe znaki kategorii Cf,
// którym odstęp zdjął joinersWithoutSpacing. Zwraca delta, albo zero, gdy glifu nie ma.
func addToLastGlyph(line shaping.Line, text []rune, delta fixed.Int26_6, skipIgnorable bool) fixed.Int26_6 {
	for r := len(line) - 1; r >= 0; r-- {
		run := &line[r]
		for i := len(run.Glyphs) - 1; i >= 0; i-- {
			if index := run.Glyphs[i].ClusterIndex; skipIgnorable && index >= 0 && index < len(text) && run.Glyphs[i].GlyphCount == 1 && isIgnorableForSpacing(text[index]) {
				continue
			}
			run.Glyphs = slices.Clone(run.Glyphs)
			run.Glyphs[i].Advance += delta
			run.Glyphs[i].XAdvance += delta
			run.RecomputeAdvance()
			return delta
		}
	}
	return 0
}

// isIgnorableForSpacing (gopdfgo): niewidoczne znaki formatujące o zerowej szerokości (kategoria Cf),
// które odstęp między literami pomija (CSS Text 3 § 7.2)
func isIgnorableForSpacing(r rune) bool { return unicode.Is(unicode.Cf, r) }

// privateLayout (gopdfgo) zwraca wynik z własnym obiektem układu. Wyniki z pamięci podręcznej były
// współdzielone: SetJustification i ApplyJustification zmieniały wspólny obiekt, więc wiersze o tym
// samym tekście nadpisywały sobie odstępy justowania, a wiersz rysowany drugi raz dostawał je podwójnie
// i wystawał poza prawy margines.
func privateLayout(line FirstLine) FirstLine {
	if shared, ok := line.Layout.(*TextLayoutGotext); ok && shared != nil {
		own := *shared
		own.justification, own.justified, own.justifyStart, own.charJustification = 0, false, 0, 0
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

func (l *TextLayoutGotext) SetHangingSpaces(count int)      { l.hangingSpaces = count }
func (l *TextLayoutGotext) SetJustificationStart(index int) { l.justifyStart = index }
func (l *TextLayoutGotext) JustificationStart() int         { return l.justifyStart }
func (l *TextLayoutGotext) HangingSpaces() int              { return l.hangingSpaces }

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

// SetCharJustification (gopdfgo), zob. [EngineLayout.SetCharJustification].
func (l *TextLayoutGotext) SetCharJustification(spacing pr.Float) { l.charJustification = spacing }

// justifiableClusterEnds (gopdfgo) zwraca dla każdej jednostki typograficznej liczonej przez
// JustifiableClusters położenie jej ostatniego glifu (przebieg, glif), w kolejności wizualnej.
func (l *TextLayoutGotext) justifiableClusterEnds() [][2]int {
	var ends [][2]int
	lastCluster := -1
	for r, run := range l.Line {
		for i, glyph := range run.Glyphs {
			index := glyph.ClusterIndex
			if index < l.justifyStart || index < 0 {
				continue
			}
			if index < len(l.text) && isIgnorableForSpacing(l.text[index]) && glyph.GlyphCount == 1 {
				continue
			}
			if index == lastCluster && len(ends) != 0 {
				ends[len(ends)-1] = [2]int{r, i}
				continue
			}
			lastCluster = index
			ends = append(ends, [2]int{r, i})
		}
	}
	// wiszące spacje na końcu wiersza zachowują szerokość i nie są jednostkami justowania
	if n := l.hangingSpaces; n > 0 {
		ends = ends[:max(0, len(ends)-n)]
	}
	return ends
}

// JustifiableClusters (gopdfgo), zob. [EngineLayout.JustifiableClusters].
func (l *TextLayoutGotext) JustifiableClusters() int {
	if l.Style != nil && l.Style.Direction == pr.Rtl {
		return 0 // pismo od prawej do lewej: bez rozsuwania znaków
	}
	return len(l.justifiableClusterEnds())
}

func (l *TextLayoutGotext) ApplyJustification() {
	if l.justified || (l.justification == 0 && l.charJustification == 0) {
		return // gopdfgo: pudełko rysowane ponownie nie dostaje odstępu drugi raz
	}
	if l.charJustification != 0 {
		// gopdfgo: text-justify: inter-character (CSS Text 3 § 6.4): odstęp za każdą jednostką
		// typograficzną oprócz ostatniej w pudełku (odstęp przed następnym pudełkiem dodaje układ wiersza)
		ends := l.justifiableClusterEnds()
		line := make(shaping.Line, len(l.Line))
		for i, run := range l.Line {
			run.Glyphs = append([]shaping.Glyph(nil), run.Glyphs...)
			line[i] = run
		}
		l.Line = line
		// odstęp zaokrąglony do 1/64 z przeniesieniem reszty, żeby suma zgadzała się z szerokością z układu
		var given fixed.Int26_6
		for j, end := range ends[:max(0, len(ends)-1)] {
			spacing := fixed.Int26_6(math.Round(float64(l.charJustification)*64*float64(j+1))) - given
			given += spacing
			glyph := &l.Line[end[0]].Glyphs[end[1]]
			glyph.Advance += spacing
			glyph.XAdvance += spacing
		}
		for i := range l.Line {
			l.Line[i].RecomputeAdvance()
		}
		l.justified = true
		return
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
	// gopdfgo: tekst przed początkiem justowania (część wiersza przed ostatnim tabulatorem) zasłaniamy
	// znakiem, który nie jest separatorem słów, żeby AddSpacing go nie poszerzał
	if l.justifyStart > 0 {
		text = append([]rune(nil), text...)
		for i := 0; i < l.justifyStart && i < len(text); i++ {
			text[i] = 'x'
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
	// gopdfgo: nazwy rodzin porównywane według CSS Fonts 4 § 5.1, zob. family_names.go
	exact := fc.exactFamilies(fd.Family)
	families := exact
	if len(fc.GenericFamilies) != 0 {
		// gopdfgo: rodziny wskazane dla rodziny generycznej wchodzą na listę tuż przed nią
		expanded := make([]string, 0, len(families)+2)
		for _, family := range families {
			expanded = append(expanded, fc.GenericFamilies[strings.ToLower(family)]...)
			expanded = append(expanded, family)
		}
		families = expanded
		// gopdfgo: gdy żadna z podanych rodzin nie jest dostępna (ani wprost, ani przez podstawienie
		// zgodnej metrycznie rodziny), używamy fontu domyślnego, czyli rodziny generycznej serif, jak
		// Chromium (CSS Fonts 4 § 5.1 zostawia wybór agentowi; testy WPT font-family-rule-001,
		// content-076). go-text dopisywało wtedy sans-serif (reguła fontconfig 49-sansserif).
		// Rodzinę zastępczą dopisujemy tylko przy liście bez dostępnej rodziny, bo w pierwszym
		// przebiegu go-text dokładna nazwa (np. Tinos) wygrałaby z podstawieniem Arial → Arimo.
		// Dostępność sprawdzamy na liście po dopasowaniu nazw według CSS (exactFamilies): nazwa,
		// która pasowałaby tylko po normalizacji go-text, nie jest dostępna.
		if len(fd.Family) != 0 && (len(exact) == 0 || fc.needsDefaultFamily(exact)) {
			families = append(families, fc.GenericFamilies["serif"]...)
			families = append(families, "serif")
		}
	}
	return fontscan.Query{
		Families: families,
		// gopdfgo: styl według kolejności CSS dla italic i oblique, zob. matchingStyle
		Aspect: newAspect(fc.matchingStyle(fd), fd.Weight, fd.Stretch),
	}
}

// cssGenericFamilies to nazwy rodzin generycznych CSS (CSS Fonts 4 § 4.2) małymi literami.
var cssGenericFamilies = map[string]bool{
	"serif": true, "sans-serif": true, "monospace": true, "cursive": true, "fantasy": true,
	"system-ui": true, "ui-serif": true, "ui-sans-serif": true, "ui-monospace": true, "ui-rounded": true,
	"math": true, "emoji": true, "fangsong": true,
}

// needsDefaultFamily (gopdfgo) mówi, czy lista rodzin nie zawiera ani rodziny generycznej, ani żadnej
// rodziny dostępnej w bazie fontów.
func (fc *FontConfigurationGotext) needsDefaultFamily(families []string) bool {
	if len(families) == 0 {
		return false
	}
	for _, family := range families {
		lower := strings.ToLower(strings.TrimSpace(family))
		if cssGenericFamilies[lower] || len(fc.GenericFamilies[lower]) != 0 || fc.familyAvailable(lower) {
			return false
		}
	}
	return true
}

// familyAvailable (gopdfgo) sprawdza, czy rodzina prowadzi do fontu z bazy wprost albo przez tablicę
// podstawień go-text. Dla rodziny nieznanej go-text dopisuje rodzinę sans-serif (reguła fontconfig
// 49-sansserif), więc rodzina jest niedostępna, gdy daje ten sam font co sama sans-serif, a font nie
// nosi jej nazwy. Wynik jest zapamiętywany; zapytanie fontmapy ustawia potem wywołujący (zob. newQuery).
func (fc *FontConfigurationGotext) familyAvailable(family string) bool {
	if known, ok := fc.familyKnown[family]; ok {
		return known
	}
	probe := func(families ...string) *font.Face {
		fc.fm.SetQuery(fontscan.Query{Families: families, Aspect: font.Aspect{Style: font.StyleNormal, Weight: font.WeightNormal, Stretch: font.StretchNormal}})
		return fc.fm.ResolveFace('a')
	}
	known := true
	if own, fallback := probe(family), probe("sans-serif"); own != nil && fallback != nil &&
		fc.fm.FontLocation(own.Font) == fc.fm.FontLocation(fallback.Font) {
		name, _ := fc.fm.FontMetadata(own.Font)
		known = font.NormalizeFamily(name) == font.NormalizeFamily(family)
	}
	if fc.familyKnown == nil {
		fc.familyKnown = map[string]bool{}
	}
	fc.familyKnown[family] = known
	return known
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
	desc.Size *= fc.FaceSizeAdjust(face.Font) // gopdfgo: deskryptor size-adjust, zob. faceMetrics

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
	// gopdfgo: jednostka ex to wysokość x pierwszego dostępnego fontu (CSS Values 4 § 6.1.1). Gdy
	// 'x' pochodzi z innego fontu (unicode-range pierwszego jej nie obejmuje), bierzemy sxHeight
	// z tablicy OS/2 pierwszego dostępnego, ze size-adjust (test WPT size-adjust.tentative); źródło
	// mierzyło glif 'x' fontu zastępczego.
	if primary := fc.resolveFace(' ', style.FontDescription); primary != nil && primary.Upem() != 0 &&
		fc.resolveFace('x', style.FontDescription) != primary {
		if xHeight := primary.LineMetric(font.XHeight); xHeight > 0 {
			return pr.Fl(xHeight) / pr.Fl(primary.Upem()) * style.FontDescription.Size * fc.FaceSizeAdjust(primary.Font)
		}
	}
	glyphs, _, sizeFactor := fc.shapeRune('x', style.FontDescription, style.FontFeatures)

	if len(glyphs) == 0 { // fontmap is broken, return a 'reasonnable' value
		return style.FontDescription.Size
	}

	return pr.Fl(fixedToFloat(glyphs[0].YBearing) / sizeFactor) // fixed to float
}

// heightCap (gopdfgo): wysokość obrysu znaku 'H', przybliżenie wysokości wersalików (jednostka cap,
// CSS Values 4 § 6.1.1), liczona tak jak wysokość 'x' dla jednostki ex.
func (fc *FontConfigurationGotext) heightCap(style *TextStyle) pr.Fl {
	glyphs, _, sizeFactor := fc.shapeRune('H', style.FontDescription, style.FontFeatures)
	if len(glyphs) == 0 || glyphs[0].GlyphID == 0 {
		return 0
	}
	return pr.Fl(fixedToFloat(glyphs[0].YBearing) / sizeFactor)
}

// autospaceWidth (gopdfgo) zwraca odstęp text-autospace (1/8 ic, CSS Text 4 § 6.4) albo zero, gdy
// styl go wyłącza albo w tekście nie ma ideogramów. Przy niezerowym letter-spacing odstępu nie
// dodajemy: znaki są już rozsunięte, a test WPT letter-spacing-200 (ideogramy z letter-spacing: 1em)
// oczekuje samego odstępu między literami; norma tego przypadku nie rozstrzyga.
func (fc *FontConfigurationGotext) autospaceWidth(style *TextStyle, text []rune) fixed.Int26_6 {
	if style.Autospace == 0 || style.LetterSpacing != 0 || !slices.ContainsFunc(text, func(r rune) bool { return classifyAutospace(r) == autospaceIdeograph }) {
		return 0
	}
	return floatToFixed(fc.widthIdeographic(style) / 8)
}

// widthIdeographic (gopdfgo): posunięcie znaku '水' (jednostka ic, CSS Values 4 § 6.1.1); 1em, gdy
// żaden font go nie ma.
func (fc *FontConfigurationGotext) widthIdeographic(style *TextStyle) pr.Fl {
	glyphs, _, sizeFactor := fc.shapeRune('水', style.FontDescription, style.FontFeatures)
	if len(glyphs) == 0 || glyphs[0].GlyphID == 0 {
		return style.FontDescription.Size
	}
	return pr.Fl(fixedToFloat(glyphs[0].Advance) / sizeFactor)
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
	descent := roundMetric(-fixedToFloat(bounds.Descent) / sizeFactor)
	// gopdfgo: ascent-override i descent-override (CSS Fonts 5 § 4.10) zastępują wydłużenia pierwszego
	// dostępnego fontu procentem rozmiaru, przeskalowanym jeszcze przez size-adjust
	if face := fc.resolveFace(' ', style.FontDescription); face != nil {
		if metrics, ok := fc.fontsMetrics[face.Font]; ok {
			size := pr.Float(style.FontDescription.Size) * pr.Float(metrics.sizeAdjust)
			if v, ok := metrics.ascent.(pr.Float); ok {
				baseline = roundMetric(v * size)
			}
			if v, ok := metrics.descent.(pr.Float); ok {
				descent = roundMetric(v * size)
			}
		}
	}
	height = baseline + descent

	return height, baseline
}

// faceExtents (gopdfgo) zwraca wydłużenie górne i dolne (ujemne) twarzy w rozmiarze stylu,
// zaokrąglone do pikseli, z uwzględnieniem deskryptorów ascent-override, descent-override i
// size-adjust (CSS Fonts 5 § 4.10, 4.12) oraz font-size-adjust dla fontu zastępczego.
func (fc *FontConfigurationGotext) faceExtents(style *TextStyle, face *font.Face) (ascent, descent pr.Float) {
	extents, _ := face.FontHExtents()
	size := pr.Float(style.FontDescription.Size) * pr.Float(fc.FaceSizeScale(style, face))
	ascent = roundMetric(pr.Float(extents.Ascender) * size / pr.Float(face.Upem()))
	descent = roundMetric(pr.Float(extents.Descender) * size / pr.Float(face.Upem()))
	if metrics, ok := fc.fontsMetrics[face.Font]; ok {
		if v, ok := metrics.ascent.(pr.Float); ok {
			ascent = roundMetric(v * size)
		}
		if v, ok := metrics.descent.(pr.Float); ok {
			descent = -roundMetric(v * size)
		}
	}
	return ascent, descent
}

// lineGap (gopdfgo) zwraca odstęp międzywierszowy pierwszego dostępnego fontu, zaokrąglony do całych
// pikseli tak jak wydłużenia (zob. roundMetric).
func (fc *FontConfigurationGotext) lineGap(style *TextStyle) pr.Float {
	face := fc.resolveFace(' ', style.FontDescription)
	if face == nil || face.Upem() == 0 {
		return 0
	}
	extents, _ := face.FontHExtents()
	// gopdfgo: line-gap-override i size-adjust (CSS Fonts 5 § 4.11, 4.12)
	if metrics, ok := fc.fontsMetrics[face.Font]; ok {
		size := pr.Float(style.FontDescription.Size) * pr.Float(metrics.sizeAdjust)
		if v, ok := metrics.lineGap.(pr.Float); ok {
			return roundMetric(v * size)
		}
		return max(0, roundMetric(pr.Float(extents.LineGap)*size/pr.Float(face.Upem())))
	}
	return max(0, roundMetric(pr.Float(extents.LineGap)*pr.Float(style.FontDescription.Size)/pr.Float(face.Upem())))
}

// sizeAdjustFactor (gopdfgo) zwraca mnożnik, przez który font-size-adjust przelicza obliczony rozmiar
// fontu na rozmiar używany (CSS Fonts 5 § 2.7): podana liczba podzielona przez stosunek wybranej
// metryki pierwszego dostępnego fontu do jego firetu. Zwraca 1 dla none, dla from-font (liczba jest
// wtedy z definicji równa stosunkowi fontu podstawowego) i wtedy, gdy font metryki nie podaje.
// Fonty zastępcze dopasowuje osobno FaceSizeScale.
func (fc *FontConfigurationGotext) sizeAdjustFactor(style *TextStyle) pr.Fl {
	adjust := style.SizeAdjust
	if adjust.String == "" || adjust.Bool {
		return 1
	}
	return fc.faceSizeAdjustFactor(style, fc.resolveFace(' ', style.FontDescription))
}

// faceSizeAdjustFactor (gopdfgo) liczy mnożnik font-size-adjust dla podanej twarzy. Metryka twarzy
// obejmuje deskryptor size-adjust z @font-face (CSS Fonts 5 § 4.12), więc font-size-adjust go znosi.
// Dla from-font docelową liczbą jest stosunek pierwszego dostępnego fontu elementu.
func (fc *FontConfigurationGotext) faceSizeAdjustFactor(style *TextStyle, face *font.Face) pr.Fl {
	adjust := style.SizeAdjust
	if adjust.String == "" {
		return 1
	}
	ratio := fc.faceAspectRatio(adjust.String, face)
	if ratio == 0 {
		return 1
	}
	target := adjust.Float
	if adjust.Bool {
		if target = fc.faceAspectRatio(adjust.String, fc.resolveFace(' ', style.FontDescription)); target == 0 {
			return 1
		}
	}
	return target / ratio
}

// faceAspectRatio (gopdfgo) zwraca stosunek metryki font-size-adjust ([metricName]) twarzy do jej
// rozmiaru, z deskryptorem size-adjust; 0, gdy twarz metryki nie ma.
func (fc *FontConfigurationGotext) faceAspectRatio(metricName string, face *font.Face) pr.Fl {
	if face == nil || face.Upem() == 0 {
		return 0
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
	// font bez metryki w tablicy OS/2 dostaje wysokość obrysu znaku (x albo H), jak przy jednostce ex
	// (CSS Values 4 § 6.1.1)
	glyphHeight := func(r rune) float32 {
		if gid, ok := face.NominalGlyph(r); ok {
			if extents, ok := face.GlyphExtents(gid); ok {
				return extents.YBearing
			}
		}
		return 0
	}
	var metric float32
	switch metricName {
	case "ex-height":
		if metric = face.LineMetric(font.XHeight); metric == 0 {
			metric = glyphHeight('x')
		}
	case "cap-height":
		if metric = face.LineMetric(font.CapHeight); metric == 0 {
			metric = glyphHeight('H')
		}
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
	return pr.Fl(metric) / pr.Fl(face.Upem()) * fc.FaceSizeAdjust(face.Font)
}

// FaceSizeScale (gopdfgo) zwraca mnożnik rozmiaru stylu [style] (już przeliczonego przez
// font-size-adjust dla pierwszego dostępnego fontu, zob. adjustedTextStyle) dla tekstu składanego
// twarzą [face]: deskryptor size-adjust twarzy, a przy font-size-adjust stosunek mnożnika tej twarzy
// do mnożnika pierwszego dostępnego fontu, bo norma każe dopasować każdy użyty font (CSS Fonts 5 § 2.7).
func (fc *FontConfigurationGotext) FaceSizeScale(style *TextStyle, face *font.Face) pr.Fl {
	scale := fc.FaceSizeAdjust(face.Font)
	if style.SizeAdjust.String != "" {
		if first := fc.sizeAdjustFactor(style); first != 0 {
			scale *= fc.faceSizeAdjustFactor(style, face) / first
		}
	}
	return scale
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
		// gopdfgo: znacznik systemu językowego OpenType przekazany wprost (prywatny podznacznik
		// "x-hbot" HarfBuzz), obok języka treści (CSS Fonts 4 § 6.13). Źródło zamieniało znacznik na
		// kod ISO 639-3, a ten dla języków z kodem dwuliterowym (TRK → tur) nie dawał znacznika
		// (test WPT font-language-override-01).
		// znacznik w zapisie szesnastkowym, bo zapis literowy HarfBuzz zamienia na wielkie litery
		var tag [4]byte
		copy(tag[:], strings.TrimRight(string(flo[:]), "\x00"))
		for i := range tag {
			if tag[i] == 0 {
				tag[i] = ' '
			}
		}
		private := "x-hbot-" + hex.EncodeToString(tag[:])
		if lg := style.Lang; lg != "" {
			lang = language.NewLanguage(lg + "-" + private)
		} else {
			lang = language.NewLanguage(private)
		}
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
	if (style.FontLanguageOverride != fontLanguageOverride{}) {
		// gopdfgo: segmentacja go-text zastępuje język przebiegów językiem zgodnym z pismem i gubi
		// prywatny podznacznik z font-language-override, więc przywracamy go
		for i := range inputs {
			inputs[i].Language = lang
		}
	}
	inputs = fc.spacesInOwnFace(inputs)
	inputs = fc.controlsInSpaceFace(inputs)
	inputs = nonBreakingHyphenFallback(inputs)

	// gopdfgo: reszty zaokrągleń szerokości znaków, potrzebne tylko przy zawijaniu (zob. LineWrap)
	var residuals []int32
	if style.textWrap() && maxWidth != pr.Inf {
		residuals = make([]int32, len(text))
	}
	// TODO: lazy iterator
	outputs := make(shaping.Line, 0, len(inputs))
	var expandedCaps []int // gopdfgo: zob. synthesizeSmallCaps
	for _, input := range inputs {
		// the features are comming either from the style,
		// or registred via CSS @font-face rule
		defaults := newFeatureSet(fc.fontsFeatures[input.Face.Font])
		defaults.merge(style.FontFeatures)
		input.FontFeatures = newFeatures(defaults.list())
		// gopdfgo: deskryptor size-adjust i font-size-adjust dla twarzy innej niż pierwsza dostępna
		if scale := fc.FaceSizeScale(style, input.Face); scale != 1 {
			input.Size = fixed.Int26_6(math.Round(float64(style.FontDescription.Size) * float64(scale) * 64))
		}

		// shape !
		if style.SynthesizedPosition != "" {
			if out, ok := fc.synthesizePosition(input, style.SynthesizedPosition, residuals); ok {
				outputs = append(outputs, out)
				continue
			}
		}
		if outs, ok := fc.synthesizeSmallCaps(input, style, residuals, &expandedCaps); ok {
			outputs = append(outputs, outs...)
			continue
		}
		fine := fixed.Int26_6(math.Round(float64(style.FontDescription.Size) * float64(fc.FaceSizeScale(style, input.Face)) * 64 * shapingPrecision))
		outputs = append(outputs, shapeAtSize(&fc.shaper, input, residuals, fine))
	}
	fc.residuals = residuals

	if wordSpacing := fc.wordSpacing(style); style.LetterSpacing != 0 || wordSpacing != 0 {
		ws, ls := floatToFixed(wordSpacing), floatToFixed(style.LetterSpacing)
		shaping.AddSpacing(outputs, text, ws, ls)
		oddLetterSpacingRemainder(outputs, ls)
		spaceExpandedClusters(outputs, expandedCaps, ls)
		if ls != 0 {
			joinersWithoutSpacing(outputs, text, ls)
			if dir.Progression() == di.FromTopLeft {
				forcedBreaksWithoutSpacing(outputs, text, ls)
				afterTabsWithoutSpacing(outputs, text, ls)
			}
		}
		// gopdfgo: odstęp za ostatnim znakiem tekstu (jak w przeglądarkach: odstęp należy do znaku
		// przed nim) dodajemy dopiero po zawinięciu, i tylko gdy wiersz sięga końca tekstu, zob.
		// trailingLetterSpacing niżej. Źródło dodawało go tutaj, więc liczył się przy sprawdzaniu, czy
		// wiersz się mieści, choć na końcu wiersza odstępu nie ma (CSS Text 3 § 7.2; test WPT
		// letter-spacing-200). Dla pisma od prawej do lewej bez zmian.
		if dir.Progression() != di.FromTopLeft {
			lastRun := &outputs[len(outputs)-1]
			lastRun.Glyphs[len(lastRun.Glyphs)-1].Advance += ls
			lastRun.RecomputeAdvance()
		}
	}

	// gopdfgo: text-autospace (CSS Text 4 § 6.4): 1/8 ic między ideogramem a literą lub cyfrą
	autospace := fc.autospaceWidth(style, text)
	if autospace != 0 && dir.Progression() == di.FromTopLeft {
		applyAutospace(outputs, text, autospace, style.Autospace)
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
		// gopdfgo: wiersz, który mieści się po uwzględnieniu reszt zaokrągleń, zostaje (test WPT
		// white-space-pre-034: 16 znaków ze spacjami nierozdzielającymi w pudełku o szerokości 16ch)
		if total > mw && !fc.fitsPrecisely(line, text, total, wrapingMaxWidth) {
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
			// gopdfgo: szerokość każdego glifu jest zaokrąglona do 1/64 piksela z błędem najwyżej pół
			// jednostki, więc suma może przekroczyć dostępną szerokość, choć dokładna się mieści (zob.
			// LineWrap); dopuszczamy taki błąd (test WPT white-space-pre-034)
			limit := mw
			if wrapingMaxWidth < pr.Float(math.MaxInt32>>6) {
				glyphCount := 0
				for _, run := range line {
					glyphCount += len(run.Glyphs)
				}
				limit = max(mw, floatToFixed(pr.Fl(wrapingMaxWidth))+fixed.Int26_6(glyphCount+1)/2)
			}
			if content+spacesWidth > limit {
				keep = 0
				for used := content; keep < spaces && used+trailing[keep].Advance <= limit; keep++ {
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
		// gopdfgo: znak przed wymuszonym złamaniem kończy wiersz, więc odstępu między literami za nim
		// nie ma (CSS Text 3 § 7.2); połowę odstępu za nim zdejmuje trailingLetterSpacing niżej.
		// Źródło (i wcześniejsza wersja gopdfgo) dawało mu pełny odstęp.
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

	// gopdfgo: odstęp text-autospace na granicy, na której łamie się wiersz, znika (CSS Text 4 § 6.4:
	// odstęp wstawia się tylko między znakami w jednym wierszu)
	if autospace != 0 && !mandatoryBreak && wLine.NextLine < len(text) && autospaceBoundary(text, wLine.NextLine, style.Autospace) &&
		dir.Progression() == di.FromTopLeft {
		addToLastGlyph(line, text, -autospace, false)
	}

	// gopdfgo: odstęp między literami na końcu wiersza (CSS Text 3 § 7.2), zob. trailingLetterSpacing
	var trailingSpacing fixed.Int26_6
	if ls := floatToFixed(style.LetterSpacing); ls != 0 && len(line) != 0 {
		if dir.Progression() != di.FromTopLeft {
			// pismo od prawej do lewej: bez zmian względem źródła, zob. wyżej
		} else if wLine.NextLine >= len(text) && !mandatoryBreak {
			// tabulator kończy się na pozycji tabulacji, odstęp za nim nie przysługuje (zob.
			// afterTabsWithoutSpacing; test WPT tab-size-spacing-001)
			if text[len(text)-1] != '\t' {
				trailingSpacing = addToLastGlyph(line, text, ls, false)
			}
		} else if !mandatoryBreak { // przy wymuszonym złamaniu zob. forcedBreaksWithoutSpacing
			addToLastGlyph(line, text, -ls/2, true)
		}
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

		ascent, descent := fc.faceExtents(style, run.Face)

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

	// gopdfgo: odstęp za ostatnim znakiem tekstu wisi jak końcowe odstępy: zostaje, gdy w tym samym
	// wierszu jest dalsza treść, znika, gdy wiersz tu się kończy (CSS Text 3 § 7.2)
	hang += trailingSpacing

	// gopdfgo: wiersz przyjęty dzięki tolerancji zaokrągleń (zob. LineWrap) zgłasza szerokość nie
	// większą niż dostępna; inaczej układ wiersza uznałby go za przepełniony i złamał wcześniej.
	lineWidth := fixedToFloat(width)
	if used := fixedToFloat(width - hang); textWrap && used > wrapingMaxWidth && used <= wrapingMaxWidth+0.5+1e-6 {
		lineWidth = wrapingMaxWidth + fixedToFloat(hang)
	}

	out := FirstLine{
		Hang:         fixedToFloat(hang),
		Layout:       &TextLayoutGotext{text: text[:firstLineLength], Style: style, Line: outLine, MaxWidth: maxWidth, trailingSpacing: trailingSpacing},
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

// joinersAfterSpaces (gopdfgo) zastępuje w tekście do wyznaczania miejsc podziału łącznik (ZWJ, ZWNJ)
// stojący tuż po spacji spacją. UAX 14 LB9 nie dokleja znaków łączących do spacji, więc po LB10 łącznik
// liczy się jak litera (AL) i przed nim wolno złamać wiersz (LB18). Kształtowanie łączy jednak spację
// z łącznikiem w jedno skupienie, a go-text łamie tylko między skupieniami i doklejał łącznik do spacji
// (LB8a: po ZWJ nie łamać), więc miejsce podziału za spacją znikało (test WPT
// white-space-vs-joiners-002). Ze spacją w miejscu łącznika podział wypada zaraz za skupieniem.
// Kształty i szerokości zostają bez zmian.
func joinersAfterSpaces(text []rune) []rune {
	out := text
	for i := 1; i < len(text); i++ {
		if (text[i] == '\u200d' || text[i] == '\u200c') && (text[i-1] == ' ' || text[i-1] == '\t' || text[i-1] == '\u3000') {
			if &out[0] == &text[0] {
				out = slices.Clone(text)
			}
			out[i] = ' '
		}
	}
	return out
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
	breakText = joinersAfterSpaces(breakText)
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
// zero. Spacje wiszą tylko przy pre-wrap; przy break-spaces liczy się tylko znak nowego wiersza, a w
// zapisie od prawej do lewej zostawiamy zachowanie biblioteki.
func hangingExtra(runs shaping.Line, text []rune, style *TextStyle, dir di.Direction) fixed.Int26_6 {
	if dir.Progression() != di.FromTopLeft {
		return 0
	}
	preserved, breakSpaces := !style.spaceCollapse(), style.breakSpaces()
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
			// przy break-spaces odstępy nie wiszą, tylko znak nowego wiersza nie zajmuje miejsca
			if r := text[index]; r == '\n' || !breakSpaces && isHangingWhiteSpace(r) && (preserved || r != ' ') {
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
	return fc.lineWidth(line, text, true)
}

// fitsPrecisely (gopdfgo) mówi, czy cały wiersz (z końcowymi odstępami) o zaokrąglonej szerokości
// total mieści się w maxWidth po uwzględnieniu reszt zaokrągleń (zob. LineWrap).
func (fc *FontConfigurationGotext) fitsPrecisely(line shaping.Line, text []rune, total fixed.Int26_6, maxWidth pr.Float) bool {
	if fc.residuals == nil || len(fc.residuals) != len(text) || maxWidth > math.MaxInt32>>6 || float64(total) > float64(maxWidth)*64+32 {
		return false
	}
	_, precise := fc.lineWidth(line, text, false)
	return float64(precise) <= float64(maxWidth)*64*shapingPrecision+1
}

// lineWidth zwraca szerokość wiersza, z skipTrailing bez końcowych odstępów (zob. contentWidth).
func (fc *FontConfigurationGotext) lineWidth(line shaping.Line, text []rune, skipTrailing bool) (rounded fixed.Int26_6, precise int64) {
	trailing := skipTrailing
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
	firstLine := fc.splitFirstLineRaw(hyphenCache, text, style, maxWidth, minimum, isLineStart)
	// gopdfgo: wiersz złamany wewnątrz tekstu kończy się tutaj, więc odstęp między literami za jego
	// ostatnim znakiem znika (CSS Text 3 § 7.2). Tu trafiają wiersze ułożone z części tekstu (np. przy
	// dzieleniu wyrazów), które silnik zakończył jak koniec tekstu.
	if layout, ok := firstLine.Layout.(*TextLayoutGotext); ok && firstLine.ResumeAt != -1 && layout.trailingSpacing != 0 {
		trimmed := layout.TrimTrailingLetterSpacing()
		firstLine.Width -= trimmed
		firstLine.Hang -= trimmed
	}
	return firstLine
}

func (fc *FontConfigurationGotext) splitFirstLineRaw(hyphenCache map[HyphenDictKey]hyphen.Hyphener, text []rune, style *TextStyle,
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
		// gopdfgo: krótki tekst kształtujemy w całości. Początek tekstu ukształtowany osobno może się
		// różnić od całości: ligatura obejmująca spację (font z ikonami-ligaturami, test WPT
		// font-features-across-space-1) rozpadała się na słowa i wiersz łamał się przy spacji, choć cała
		// ligatura mieściła się w szerokości liczonej z pełnego tekstu (max-content). Skrót z
		// shortTextHint zostaje dla długich tekstów, gdzie oszczędza kształtowanie całości przy każdym
		// wierszu.
		if len(text) <= 128 {
			shortText = text
		}

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
			for at > 0 && !fc.breakAllAllowed(text[at-1], text[at], style.LooseBreak) {
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
			if remainder < 0 {
				// gopdfgo: przed krawędzią bloku (ujemne text-indent) następną pozycją tabulatora może
				// być sama krawędź, bo pozycje są wielokrotnościami tab-size od niej (CSS Text 3 § 4.2);
				// math.Mod zwraca tu resztę ujemną, a źródło pomijało tę pozycję
				remainder += columnWidthF
			}
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
// synthesizePosition (gopdfgo) syntetyzuje indeks dolny albo górny (CSS Fonts 4 § 6.5): gdy font nie ma
// wariantu 'subs' lub 'sups' dla każdego znaku przebiegu, cały przebieg jest składany bez tej cechy,
// mniejszym pismem i przesunięty w pionie. Współczynniki są jak w Firefoksie (Chromium syntezy nie
// ma): rozmiar 0,82 dla pisma do 20 px, 0,667 od 45 px, liniowo pomiędzy; podniesienie 0,34 em,
// obniżenie 0,2 em. Źródło syntezy nie znało.
func (fc *FontConfigurationGotext) synthesizePosition(input shaping.Input, position string, residuals []int32) (shaping.Output, bool) {
	tag := opentype.MustNewTag("sups")
	if position == "sub" {
		tag = opentype.MustNewTag("subs")
	}
	plain := input
	plain.FontFeatures = nil
	for _, feature := range input.FontFeatures {
		if feature.Tag != tag {
			plain.FontFeatures = append(plain.FontFeatures, feature)
		}
	}
	with, without := shapePrecisely(&fc.shaper, input), shapePrecisely(&fc.shaper, plain)
	if len(with.Glyphs) != len(without.Glyphs) {
		return shaping.Output{}, false // cecha zmieniła liczbę glifów, więc font ją ma
	}
	covered := true
	for i := range with.Glyphs {
		if with.Glyphs[i].GlyphID == without.Glyphs[i].GlyphID {
			covered = false
			break
		}
	}
	if covered {
		return shaping.Output{}, false
	}
	size := float64(fixedToFloat(input.Size))
	ratio := 0.82 + (0.667-0.82)*min(1, max(0, (size-20)/(45-20)))
	plain.Size = fixed.Int26_6(math.Round(size * ratio * 64))
	out := shapeWithResiduals(&fc.shaper, plain, residuals)
	shift := fixed.Int26_6(math.Round(size * 0.34 * 64))
	if position == "sub" {
		shift = -fixed.Int26_6(math.Round(size * 0.2 * 64))
	}
	for i := range out.Glyphs {
		out.Glyphs[i].YOffset += shift
	}
	return out, true
}

// oddLetterSpacingRemainder (gopdfgo): go-text dzieli odstęp między literami na dwie połowy obcięte
// do 1/64 piksela (ls/2), więc przy nieparzystej liczbie jednostek każdy odstęp jest o 1/64 piksela za
// krótki i rząd znaków z odstępem rozjeżdża się z tabulacją liczoną z pełnego odstępu (test WPT
// tab-size-spacing-002). Brakującą jednostkę dokładamy do połowy za klastrem, tam gdzie go-text ją dodał
// (wszędzie poza ostatnim klastrem ostatniego przebiegu, zob. shaping.Output.AddLetterSpacing).
func oddLetterSpacingRemainder(runs shaping.Line, ls fixed.Int26_6) {
	rem := ls - 2*(ls/2)
	if rem == 0 {
		return
	}
	for r := range runs {
		run := &runs[r]
		changed := false
		for start := 0; start < len(run.Glyphs); {
			end := start + max(run.Glyphs[start].GlyphCount, 1) - 1
			if end >= len(run.Glyphs) {
				break
			}
			if end+1 < len(run.Glyphs) || r < len(runs)-1 {
				run.Glyphs[end].Advance += rem
				if run.Direction.IsVertical() {
					run.Glyphs[end].YAdvance += rem
				} else {
					run.Glyphs[end].XAdvance += rem
				}
				changed = true
			}
			start = end + 1
		}
		if changed {
			run.RecomputeAdvance()
		}
	}
}

// clusterIgnorableForSpacing (gopdfgo) mówi, czy klaster glifu g składa się wyłącznie ze znaków Cf albo
// znaków wymuszonego złamania, które nie dostają odstępu między literami (zob. joinersWithoutSpacing).
func clusterIgnorableForSpacing(g shaping.Glyph, text []rune) bool {
	if g.ClusterIndex < 0 || g.ClusterIndex >= len(text) {
		return false
	}
	end := min(g.ClusterIndex+max(g.RuneCount, 1), len(text))
	for _, c := range text[g.ClusterIndex:end] {
		if !isIgnorableForSpacing(c) && !isMandatoryBreak(c) {
			return false
		}
	}
	return true
}

// joinersWithoutSpacing (gopdfgo) zdejmuje letter-spacing dodany przez go-text do znaków ZWNJ i ZWJ
// stojących we własnym klastrze. Należą one do klastra grafemów poprzedniego znaku (Grapheme_Extend,
// UAX 29), a odstęp między literami dodaje się między klastrami grafemów (CSS Text 3 § 8.2), więc
// „f‌i” ma jeden odstęp, jak „fi” (test WPT font-feature-resolution-001). Źródło dodawało dwa.
// gopdfgo: tak samo każdy inny znak kategorii Cf (U+200B, U+2060, U+FEFF, znaki sterujące
// kierunkiem): odstęp dodaje się tak, jakby ich nie było (CSS Text 3 § 7.2; testy WPT
// letter-spacing-202, letter-spacing-control-chars-001).
func joinersWithoutSpacing(runs shaping.Line, text []rune, ls fixed.Int26_6) {
	half := ls / 2
	// gopdfgo: klaster może obejmować kilka takich znaków (HarfBuzz dołącza ZWJ do klastra
	// poprzedzającego ZWNJ: dwa glify, dwa znaki), więc sprawdzamy wszystkie znaki klastra, a odstęp
	// zdejmujemy tam, gdzie go-text go dodał: połowę przed pierwszym glifem klastra i połowę za ostatnim
	for r := range runs {
		run := &runs[r]
		changed := false
		for start := 0; start < len(run.Glyphs); {
			g := run.Glyphs[start]
			end := start + max(g.GlyphCount, 1) - 1
			next := end + 1
			if end >= len(run.Glyphs) || !clusterIgnorableForSpacing(g, text) {
				start = next
				continue
			}
			if start > 0 || r > 0 { // połowa przed klastrem (zob. shaping.Output.AddLetterSpacing)
				first := &run.Glyphs[start]
				first.Advance -= half
				first.XAdvance -= half
				first.XOffset -= half
			}
			if next < len(run.Glyphs) || r < len(runs)-1 { // połowa za klastrem
				last := &run.Glyphs[end]
				last.Advance -= half
				last.XAdvance -= half
			}
			changed = true
			start = next
		}
		if changed {
			run.RecomputeAdvance()
		}
	}
	// gopdfgo: znaki Cf na początku albo na końcu tekstu: pierwszy (ostatni) widoczny znak nie ma
	// sąsiada po tej stronie, więc nie dostaje połowy odstępu przed sobą (za sobą), jak gdyby
	// znaków Cf nie było (test WPT letter-spacing-control-chars-001)
	ignorable := func(g shaping.Glyph) bool {
		return clusterIgnorableForSpacing(g, text) && !isMandatoryBreak(text[g.ClusterIndex])
	}
	type glyphRef struct{ r, i int }
	var first, last *glyphRef
	leading, trailing := false, false
	for r := range runs {
		for i, g := range runs[r].Glyphs {
			if ignorable(g) {
				if first == nil {
					leading = true
				}
				trailing = last != nil
				continue
			}
			if first == nil {
				first = &glyphRef{r, i}
			}
			last, trailing = &glyphRef{r, i}, false
		}
	}
	if first != nil && leading {
		addToGlyph(runs, first.r, first.i, -half, true)
	}
	if last != nil && trailing {
		addToGlyph(runs, last.r, last.i, -half, false)
	}
}

// forcedBreaksWithoutSpacing (gopdfgo) zdejmuje odstęp między literami przy zachowanym znaku nowego
// wiersza (i innych znakach klasy BK z UAX 14): znak ten kończy wiersz, a na końcu wiersza odstępu nie
// ma (CSS Text 3 § 7.2). Sam znak nie dostaje odstępu (zob. joinersWithoutSpacing), a znak przed nim
// traci połowę odstępu za sobą.
// Robimy to przed zawijaniem, żeby odstęp nie liczył się do szerokości wiersza (test WPT
// letter-spacing-200: „4 5” w pudełku o szerokości 5em). Tylko pismo od lewej do prawej.
func forcedBreaksWithoutSpacing(runs shaping.Line, text []rune, ls fixed.Int26_6) {
	half := ls / 2
	for r := range runs {
		for i, g := range runs[r].Glyphs {
			if g.GlyphCount != 1 || g.ClusterIndex < 0 || g.ClusterIndex >= len(text) || !isMandatoryBreak(text[g.ClusterIndex]) {
				continue
			}
			// poprzedni glif (pomijając znaki Cf bez odstępu) traci połowę odstępu za sobą
			pr_, pi := r, i
			for {
				if pi--; pi < 0 {
					if pr_--; pr_ < 0 {
						break
					}
					pi = len(runs[pr_].Glyphs) - 1
					if pi < 0 {
						continue
					}
				}
				pg := runs[pr_].Glyphs[pi]
				if pg.GlyphCount == 1 && pg.ClusterIndex >= 0 && pg.ClusterIndex < len(text) &&
					(isIgnorableForSpacing(text[pg.ClusterIndex]) || isMandatoryBreak(text[pg.ClusterIndex])) {
					if isMandatoryBreak(text[pg.ClusterIndex]) {
						break // przed nim był już początek wiersza
					}
					continue
				}
				addToGlyph(runs, pr_, pi, -half, false)
				break
			}
		}
	}
}

// afterTabsWithoutSpacing (gopdfgo): znak za tabulatorem zaczyna się dokładnie na pozycji tabulacji
// (CSS Text 3 § 4.2), więc nie dostaje połowy odstępu między literami przed sobą; tabulator kończy się
// na tej pozycji (zob. AlignTabs). Źródło przesuwało go o pół odstępu (test WPT tab-size-spacing-002).
func afterTabsWithoutSpacing(runs shaping.Line, text []rune, ls fixed.Int26_6) {
	afterTab := false
	for r := range runs {
		for i, g := range runs[r].Glyphs {
			isTab := g.GlyphCount == 1 && g.ClusterIndex >= 0 && g.ClusterIndex < len(text) && text[g.ClusterIndex] == '\t'
			if afterTab && !isTab {
				addToGlyph(runs, r, i, -ls/2, true)
			}
			afterTab = isTab
		}
	}
}

// addToGlyph (gopdfgo) dodaje delta do szerokości glifu i przelicza szerokość przebiegu; z offset
// przesuwa też sam glif (połowa odstępu przed nim)
func addToGlyph(runs shaping.Line, r, i int, delta fixed.Int26_6, offset bool) {
	g := &runs[r].Glyphs[i]
	g.Advance += delta
	g.XAdvance += delta
	if offset {
		g.XOffset += delta
	}
	runs[r].RecomputeAdvance()
}

func isMandatoryBreak(r rune) bool {
	return r == '\n' || r == '\u000B' || r == '\u000C' || r == '\u2028' || r == '\u2029'
}

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
	return shapeAtSize(shaper, input, residuals, input.Size*shapingPrecision)
}

// shapeAtSize (gopdfgo) działa jak shapeWithResiduals, ale kształtuje w rozmiarze fine podanym
// w 1/4096 piksela, a nie w input.Size (1/64 piksela). Rozmiar obcięty do 0,01 px jak w Chromium
// (chromiumFontSize, np. 8,12 px) nie mieści się w siatce 1/64 px: zaokrąglony do niej różniłby się
// od Chromium o więcej niż samo obcięcie.
func shapeAtSize(shaper *shaping.HarfbuzzShaper, input shaping.Input, residuals []int32, fine fixed.Int26_6) shaping.Output {
	size := input.Size
	input.Size = size * shapingPrecision
	out := shaper.Shape(input)
	// go-text zaokrągla rozmiar w górę do całych jednostek, więc kształtujemy w rozmiarze z siatki
	// 1/64 px, a szerokości i przesunięcia przeliczamy liniowo na rozmiar fine (bez hintingu zależą
	// od rozmiaru liniowo).
	if fine != input.Size && fine > 0 && input.Size > 0 {
		r := float64(fine) / float64(input.Size)
		mul := func(v fixed.Int26_6) fixed.Int26_6 { return fixed.Int26_6(math.Round(float64(v) * r)) }
		for i := range out.Glyphs {
			g := &out.Glyphs[i]
			g.Width, g.Height, g.XBearing, g.YBearing = mul(g.Width), mul(g.Height), mul(g.XBearing), mul(g.YBearing)
			g.Advance, g.XAdvance, g.YAdvance = mul(g.Advance), mul(g.XAdvance), mul(g.YAdvance)
			g.XOffset, g.YOffset = mul(g.XOffset), mul(g.YOffset)
		}
		out.LineBounds.Ascent, out.LineBounds.Descent, out.LineBounds.Gap = mul(out.LineBounds.Ascent), mul(out.LineBounds.Descent), mul(out.LineBounds.Gap)
	}
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
	// gopdfgo: spacja niełamiąca dostaje własny font (pierwszy, który ją ma), a nie font spacji:
	// różnią się, gdy unicode-range reguły @font-face obejmuje tylko jedną z nich (test WPT
	// font-face-unicode-range-nbsp)
	var spaceFace, nbspFace *font.Face
	faceOf := func(r rune) *font.Face {
		if r == ' ' {
			if spaceFace == nil {
				spaceFace = fc.fm.ResolveFace(' ')
			}
			return spaceFace
		}
		if nbspFace == nil {
			nbspFace = fc.fm.ResolveFace('\u00a0')
		}
		return nbspFace
	}
	isSpaceRune := func(r rune) bool { return r == ' ' || r == '\u00a0' }
	needsSplit := false
	for _, input := range inputs {
		for _, r := range input.Text[input.RunStart:input.RunEnd] {
			if !isSpaceRune(r) {
				continue
			}
			if face := faceOf(r); face != nil && input.Face != face {
				needsSplit = true
				break
			}
		}
	}
	if !needsSplit {
		return inputs
	}
	out := make([]shaping.Input, 0, len(inputs)+2)
	for _, input := range inputs {
		start, first := input.RunStart, len(out)
		// klasa znaku: 0 zwykły, 1 spacja, 2 spacja niełamiąca
		class := func(i int) int {
			switch input.Text[i] {
			case ' ':
				return 1
			case '\u00a0':
				return 2
			}
			return 0
		}
		for i := input.RunStart + 1; i <= input.RunEnd; i++ {
			if i < input.RunEnd && class(i) == class(start) {
				continue
			}
			piece := input
			piece.RunStart, piece.RunEnd = start, i
			if class(start) != 0 {
				if face := faceOf(input.Text[start]); face != nil {
					piece.Face = face
				}
			}
			// sąsiednie kawałki tego samego przebiegu z tym samym fontem łączymy z powrotem
			if n := len(out); n > first && out[n-1].Face == piece.Face {
				out[n-1].RunEnd = piece.RunEnd
			} else {
				out = append(out, piece)
			}
			start = i
		}
	}
	return out
}

// nonBreakingHyphenFallback (gopdfgo): HarfBuzz rysuje łącznik nierozdzielający U+2011 glifem
// łącznika U+2010, gdy font nie ma własnego (hb-ot-shape-normalize; go-text robi to samo), więc
// w przeglądarkach font sąsiedniego tekstu, który ma U+2010, obsługuje też U+2011. Segmentacja go-text
// patrzy tylko na mapę znaków i dawała takiemu znakowi font zastępczy (test WPT
// line-break-anywhere-overrides-uax-behavior-013: Ahem nie ma U+2011). Przebieg złożony z samych
// U+2011 dostaje więc font przebiegu poprzedniego (albo następnego), jeśli ten ma U+2010, i łączy się
// z nim; także gdy jego font zastępczy ma własny U+2011, bo font sąsiedniego tekstu jest zwykle
// wcześniej na liście rodzin.
func nonBreakingHyphenFallback(inputs []shaping.Input) []shaping.Input {
	onlyNBH := func(in shaping.Input) bool {
		if in.RunEnd <= in.RunStart {
			return false
		}
		for _, r := range in.Text[in.RunStart:in.RunEnd] {
			if r != 0x2011 {
				return false
			}
		}
		return true
	}
	hasHyphen := func(face *font.Face) bool {
		if face == nil {
			return false
		}
		_, ok := face.NominalGlyph(0x2010)
		return ok
	}
	changed := false
	for i := range inputs {
		if !onlyNBH(inputs[i]) {
			continue
		}
		switch {
		case i > 0 && hasHyphen(inputs[i-1].Face):
			inputs[i].Face = inputs[i-1].Face
			changed = true
		case i+1 < len(inputs) && hasHyphen(inputs[i+1].Face):
			inputs[i].Face = inputs[i+1].Face
			changed = true
		}
	}
	if !changed {
		return inputs
	}
	out := inputs[:1]
	for _, in := range inputs[1:] {
		last := &out[len(out)-1]
		if last.Face == in.Face && last.RunEnd == in.RunStart && last.Direction == in.Direction &&
			last.Script == in.Script && last.Language == in.Language && last.Size == in.Size {
			last.RunEnd = in.RunEnd
			continue
		}
		out = append(out, in)
	}
	return out
}

// controlsInSpaceFace (gopdfgo) daje przebiegowi złożonemu wyłącznie ze znaków sterujących (np. sam
// tabulator w osobnym elemencie) font spacji, czyli pierwszy font z listy rodzin. go-text wybierał dla
// takiego przebiegu pierwszy font, który ma glif znaku sterującego, zwykle font zastępczy, i wiersz
// dostawał jego wysokość. Tabulator i znak nowego wiersza nie mają glifu, tylko odstęp albo podział
// (CSS Text 3 § 4.2, § 5.1), a pozostałe znaki sterujące zamieniamy wcześniej na U+FFFD, więc znak
// sterujący nie powinien wybierać fontu; tak robi Chromium.
func (fc *FontConfigurationGotext) controlsInSpaceFace(inputs []shaping.Input) []shaping.Input {
	var spaceFace *font.Face
	for i, input := range inputs {
		controls := input.RunEnd > input.RunStart
		for _, r := range input.Text[input.RunStart:input.RunEnd] {
			if !unicode.IsControl(r) {
				controls = false
				break
			}
		}
		if !controls {
			continue
		}
		if spaceFace == nil {
			if spaceFace = fc.fm.ResolveFace(' '); spaceFace == nil {
				return inputs
			}
		}
		inputs[i].Face = spaceFace
	}
	return inputs
}

// breakAllAllowed mówi, czy przy word-break: break-all wolno złamać wiersz między znakami a i b:
// litery i cyfry liczą się jak znaki ideograficzne, reszta według UAX 14.
func (fc *FontConfigurationGotext) breakAllAllowed(a, b rune, loose bool) bool {
	ideograph := func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return '\u4e00'
		}
		return r
	}
	// gopdfgo: przy line-break: loose wolno złamać przed łącznikiem U+2010 i półpauzą U+2013, gdy
	// poprzedni znak jest ideograficzny, także przez word-break: break-all (CSS Text 3 § 5.3)
	if loose && (b == '\u2010' || b == '\u2013') && ideograph(a) == '\u4e00' {
		return true
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
