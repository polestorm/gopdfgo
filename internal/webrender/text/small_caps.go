package text

import (
	"math"
	"unicode"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// smallCapsScale (gopdfgo): rozmiar syntetycznych kapitalików względem rozmiaru pisma, jak w Chromium
// (kSmallCapsFontSizeMultiplier).
const smallCapsScale = 0.7

// smallCapsFeatures (gopdfgo) zwraca cechy OpenType, których brak w foncie wymaga syntezy dla
// font-variant-caps, i czy wielkie litery też są pomniejszane (all-small-caps, all-petite-caps).
func smallCapsFeatures(caps FontVariantCaps) (tags []opentype.Tag, all bool) {
	switch caps {
	case FVC_SmallCaps:
		return []opentype.Tag{opentype.MustNewTag("smcp")}, false
	case FVC_AllSmallCaps:
		return []opentype.Tag{opentype.MustNewTag("smcp"), opentype.MustNewTag("c2sc")}, true
	case FVC_PetiteCaps:
		return []opentype.Tag{opentype.MustNewTag("pcap")}, false
	case FVC_AllPetiteCaps:
		return []opentype.Tag{opentype.MustNewTag("pcap"), opentype.MustNewTag("c2pc")}, true
	}
	return nil, false
}

// faceHasFeatures (gopdfgo) mówi, czy tabela GSUB fontu ma wszystkie podane cechy.
func faceHasFeatures(face *font.Face, tags []opentype.Tag) bool {
	if face == nil || face.Font == nil {
		return false
	}
	for _, tag := range tags {
		found := false
		for _, feature := range face.Font.GSUB.Features {
			if feature.Tag == tag {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// synthesizeSmallCaps (gopdfgo) syntetyzuje kapitaliki (CSS Fonts 4 § 6.3, § 7.4: font-synthesis-
// small-caps), gdy font przebiegu nie ma cechy 'smcp' (albo 'c2sc', 'pcap', 'c2pc'): małe litery są
// zamieniane na wielkie z pełnym odwzorowaniem Unicode (ß -> SS) i składane pismem o rozmiarze 0,7,
// jak w Chromium; przy all-small-caps także wielkie litery. Przebieg dzielimy na kawałki o jednym
// rozmiarze; glify kawałka zamienionego wskazują znaki tekstu źródłowego, a znak rozwinięty w kilka
// liter (ß) tworzy jeden klaster z kilkoma glifami. expanded dostaje indeksy takich znaków (zob.
// spaceExpandedClusters). Zwraca false, gdy synteza nie jest potrzebna. Tylko pismo poziome od lewej do
// prawej. Źródło syntezy nie znało: bez cechy w foncie tekst zostawał małymi literami.
func (fc *FontConfigurationGotext) synthesizeSmallCaps(input shaping.Input, style *TextStyle, residuals []int32, expanded *[]int) ([]shaping.Output, bool) {
	tags, all := smallCapsFeatures(style.FontDescription.VariantCaps)
	if len(tags) == 0 || style.FontDescription.Synthesis&SynthesisSmallCaps == 0 || input.Direction != di.DirectionLTR ||
		faceHasFeatures(input.Face, tags) {
		return nil, false
	}
	lang := language.Und
	if style.Lang != "" {
		if tag, err := language.Parse(style.Lang); err == nil {
			lang = tag
		}
	}
	upper := cases.Upper(lang)
	text := input.Text
	// klasa znaku: pomniejszany (zamieniany na wielką literę) albo nie; znaki łączące należą do
	// poprzedniego znaku
	scaled := func(i int) bool {
		r := text[i]
		if unicode.IsLower(r) {
			return true
		}
		if all && (unicode.IsUpper(r) || unicode.IsTitle(r)) {
			return true
		}
		return upper.String(string(r)) != string(r) && unicode.IsLetter(r)
	}
	classes := make([]bool, input.RunEnd-input.RunStart)
	anyScaled := false
	for i := input.RunStart; i < input.RunEnd; i++ {
		c := scaled(i)
		if unicode.IsMark(text[i]) && i > input.RunStart {
			c = classes[i-1-input.RunStart]
		}
		classes[i-input.RunStart] = c
		anyScaled = anyScaled || c
	}
	if !anyScaled {
		return nil, false
	}
	plain := input
	plain.FontFeatures = nil
	for _, feature := range input.FontFeatures {
		if !isCapsFeature(feature.Tag) {
			plain.FontFeatures = append(plain.FontFeatures, feature)
		}
	}
	smallSize := fixed.Int26_6(math.Round(float64(input.Size) * smallCapsScale))
	var outs []shaping.Output
	for start := input.RunStart; start < input.RunEnd; {
		end := start + 1
		for end < input.RunEnd && classes[end-input.RunStart] == classes[start-input.RunStart] {
			end++
		}
		piece := plain
		piece.RunStart, piece.RunEnd = start, end
		if !classes[start-input.RunStart] {
			outs = append(outs, shapeWithResiduals(&fc.shaper, piece, residuals))
			start = end
			continue
		}
		// tekst zamieniony na wielkie litery i indeks znaku źródłowego dla każdego jego znaku
		var mapped []rune
		var origin []int
		for i := start; i < end; i++ {
			u := []rune(upper.String(string(text[i])))
			if len(u) > 1 {
				*expanded = append(*expanded, i)
			}
			for _, r := range u {
				mapped = append(mapped, r)
				origin = append(origin, i)
			}
		}
		piece.Text, piece.RunStart, piece.RunEnd, piece.Size = mapped, 0, len(mapped), smallSize
		var pieceResiduals []int32
		if residuals != nil {
			pieceResiduals = make([]int32, len(mapped))
		}
		out := shapeWithResiduals(&fc.shaper, piece, pieceResiduals)
		for i, v := range pieceResiduals {
			residuals[origin[i]] += v
		}
		for g := range out.Glyphs {
			if ci := out.Glyphs[g].ClusterIndex; ci >= 0 && ci < len(origin) {
				out.Glyphs[g].ClusterIndex = origin[ci]
			}
		}
		// klastry od nowa: kolejne glify tego samego znaku źródłowego tworzą jeden klaster
		for g := 0; g < len(out.Glyphs); {
			ci := out.Glyphs[g].ClusterIndex
			h := g + 1
			for h < len(out.Glyphs) && out.Glyphs[h].ClusterIndex == ci {
				h++
			}
			next := end
			if h < len(out.Glyphs) {
				next = out.Glyphs[h].ClusterIndex
			}
			for k := g; k < h; k++ {
				out.Glyphs[k].GlyphCount = h - g
				out.Glyphs[k].RuneCount = max(next-ci, 1)
			}
			g = h
		}
		out.Runes = shaping.Range{Offset: start, Count: end - start}
		outs = append(outs, out)
		start = end
	}
	return outs, true
}

// isCapsFeature (gopdfgo): cechy wielkości liter z font-variant-caps.
func isCapsFeature(tag opentype.Tag) bool {
	switch tag {
	case opentype.MustNewTag("smcp"), opentype.MustNewTag("c2sc"), opentype.MustNewTag("pcap"),
		opentype.MustNewTag("c2pc"), opentype.MustNewTag("unic"), opentype.MustNewTag("titl"):
		return true
	}
	return false
}

// SmallCapsGlyphChars (gopdfgo) zwraca znaki dla /ToUnicode glifu z syntetycznych kapitalików: glif
// wielkiej litery zastępuje tam małą (albo ß), więc mapowanie na znak źródłowy przypisałoby glifowi
// „S” znak „s” w całym dokumencie (mapa jest wspólna dla fontu). Gdy glif jest glifem nominalnym
// którejś litery z wielkoliterowej postaci chars, zwraca tę literę; inaczej chars bez zmian.
func SmallCapsGlyphChars(face *font.Face, glyph font.GID, chars []rune, caps FontVariantCaps) []rune {
	if tags, _ := smallCapsFeatures(caps); len(tags) == 0 || face == nil || len(chars) == 0 {
		return chars
	}
	if len(chars) == 1 {
		if g, ok := face.NominalGlyph(chars[0]); ok && g == glyph {
			return chars
		}
	}
	for _, r := range cases.Upper(language.Und).String(string(chars)) {
		if g, ok := face.NominalGlyph(r); ok && g == glyph {
			return []rune{r}
		}
	}
	return chars
}

// spaceExpandedClusters (gopdfgo): znak rozwinięty przez syntezę kapitalików w kilka liter (ß -> SS)
// jest jednym klastrem, a go-text dodaje odstęp między literami tylko między klastrami; litery
// rozwinięcia dostają go między sobą tak, jakby były osobnymi znakami (test WPT
// small-caps-letter-spacing-001: „Straße” jak „Strasse”).
func spaceExpandedClusters(runs shaping.Line, expanded []int, ls fixed.Int26_6) {
	if len(expanded) == 0 || ls == 0 {
		return
	}
	isExpanded := make(map[int]bool, len(expanded))
	for _, i := range expanded {
		isExpanded[i] = true
	}
	for r := range runs {
		run := &runs[r]
		changed := false
		for g := 0; g+1 < len(run.Glyphs); g++ {
			if ci := run.Glyphs[g].ClusterIndex; isExpanded[ci] && run.Glyphs[g+1].ClusterIndex == ci {
				run.Glyphs[g].Advance += ls
				run.Glyphs[g].XAdvance += ls
				changed = true
			}
		}
		if changed {
			run.RecomputeAdvance()
		}
	}
}
