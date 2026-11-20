package text

import (
	"math"
	"slices"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

// Zwarta postać złożonego wiersza (gopdfgo).
//
// Wiersz po złożeniu żyje do narysowania strony, a rekord glifu z kształtowania (shaping.Glyph) ma 80 B,
// z czego po złożeniu potrzebne są: numer glifu, posunięcia, przesunięcia, wymiary obrysu (czyta je
// zawijanie go-text, gdy wiersz jest zawijany ponownie dla text-overflow: ellipsis) i powiązanie ze
// znakami źródłowymi. compactGlyph trzyma tylko je (44 B). Maski nic po złożeniu nie czyta. Pola odstępu między
// literami (startLetterSpacing, endLetterSpacing) są nieeksportowane i czyta je zawijanie go-text,
// więc wiersz z odstępem między literami zostaje w pełnej postaci (zob. newLineStore).
//
// Zmiany po złożeniu (justowanie, przycięcie odstępu za ostatnią literą) nie kopiują wiersza:
// zapisujemy tylko zmienione glify (poprawki), a pełny wiersz z poprawkami odtwarza Line.

// compactGlyph to pola shaping.Glyph używane po złożeniu wiersza.
type compactGlyph struct {
	width, height, xBearing, yBearing fixed.Int26_6
	advance, xAdvance, yAdvance       fixed.Int26_6
	xOffset, yOffset                  fixed.Int26_6
	cluster                           int32
	id                                font.GID
	runes, glyphs                     uint16
}

func compactOf(g *shaping.Glyph) compactGlyph {
	return compactGlyph{
		width: g.Width, height: g.Height, xBearing: g.XBearing, yBearing: g.YBearing,
		advance: g.Advance, xAdvance: g.XAdvance, yAdvance: g.YAdvance,
		xOffset: g.XOffset, yOffset: g.YOffset,
		cluster: int32(g.ClusterIndex), id: g.GlyphID,
		runes: uint16(g.RuneCount), glyphs: uint16(g.GlyphCount),
	}
}

func (c compactGlyph) expand() shaping.Glyph {
	return shaping.Glyph{
		Width: c.width, Height: c.height, XBearing: c.xBearing, YBearing: c.yBearing,
		Advance: c.advance, XAdvance: c.xAdvance, YAdvance: c.yAdvance,
		XOffset: c.xOffset, YOffset: c.yOffset,
		ClusterIndex: int(c.cluster), GlyphID: c.id,
		RuneCount: int(c.runes), GlyphCount: int(c.glyphs),
	}
}

// fitsCompact mówi, czy glif da się zapisać zwięźle bez straty.
func fitsCompact(g *shaping.Glyph) bool {
	return g.ClusterIndex >= math.MinInt32 && g.ClusterIndex <= math.MaxInt32 &&
		g.RuneCount >= 0 && g.RuneCount <= math.MaxUint16 && g.GlyphCount >= 0 && g.GlyphCount <= math.MaxUint16
}

// glyphPatch to glif zmieniony po złożeniu: płaski indeks w wierszu i nowa wartość.
type glyphPatch struct {
	index int32
	glyph compactGlyph
}

// lineStore przechowuje złożony wiersz: zwięźle (runs, counts, glyphs, patches) albo w pełnej postaci
// (full), gdy zapis zwięzły byłby stratny.
type lineStore struct {
	full    shaping.Line
	runs    []shaping.Output // nagłówki przebiegów bez glifów
	counts  []int32          // liczba glifów każdego przebiegu
	glyphs  []compactGlyph
	patches []glyphPatch // posortowane według indeksu
	compact bool
}

// newLineStore zapisuje wiersz; keepFull wymusza pełną postać (odstęp między literami).
func newLineStore(line shaping.Line, keepFull bool) lineStore {
	if keepFull {
		return lineStore{full: line}
	}
	total := 0
	for i := range line {
		for g := range line[i].Glyphs {
			if !fitsCompact(&line[i].Glyphs[g]) {
				return lineStore{full: line}
			}
		}
		total += len(line[i].Glyphs)
	}
	s := lineStore{
		compact: true,
		runs:    make([]shaping.Output, len(line)),
		counts:  make([]int32, len(line)),
		glyphs:  make([]compactGlyph, 0, total),
	}
	for i := range line {
		s.runs[i] = line[i]
		s.runs[i].Glyphs = nil
		s.counts[i] = int32(len(line[i].Glyphs))
		for g := range line[i].Glyphs {
			s.glyphs = append(s.glyphs, compactOf(&line[i].Glyphs[g]))
		}
	}
	return s
}

// len zwraca liczbę przebiegów.
func (s *lineStore) len() int {
	if s.compact {
		return len(s.runs)
	}
	return len(s.full)
}

// face zwraca twarz przebiegu r.
func (s *lineStore) face(r int) *font.Face {
	if s.compact {
		return s.runs[r].Face
	}
	return s.full[r].Face
}

// line odtwarza pełny wiersz z poprawkami. W postaci zwięzłej to zawsze nowa kopia, którą wolno
// zmieniać; w pełnej postaci to wiersz przechowywany (wspólny), jak w źródle.
func (s *lineStore) line() shaping.Line {
	if !s.compact {
		return s.full
	}
	glyphs := make([]shaping.Glyph, len(s.glyphs))
	for i := range s.glyphs {
		glyphs[i] = s.glyphs[i].expand()
	}
	for _, p := range s.patches {
		glyphs[p.index] = p.glyph.expand()
	}
	out := make(shaping.Line, len(s.runs))
	start := 0
	for i := range s.runs {
		out[i] = s.runs[i]
		end := start + int(s.counts[i])
		out[i].Glyphs = glyphs[start:end:end]
		start = end
	}
	return out
}

// edit zmienia wiersz funkcją fn, która dostaje kopię wolną do zmian (liczby przebiegów i glifów
// muszą zostać te same). W postaci zwięzłej zapisuje tylko zmienione glify i nagłówki przebiegów.
func (s *lineStore) edit(fn func(shaping.Line)) {
	if !s.compact {
		line := slices.Clone(s.full)
		for i := range line {
			line[i].Glyphs = slices.Clone(line[i].Glyphs)
		}
		fn(line)
		s.full = line
		return
	}
	line := s.line()
	fn(line)
	if len(line) != len(s.runs) {
		*s = lineStore{full: line}
		return
	}
	var patches []glyphPatch
	k := 0
	for i := range line {
		if len(line[i].Glyphs) != int(s.counts[i]) {
			*s = lineStore{full: line}
			return
		}
		for g := range line[i].Glyphs {
			if !fitsCompact(&line[i].Glyphs[g]) {
				*s = lineStore{full: line}
				return
			}
			if c := compactOf(&line[i].Glyphs[g]); c != s.glyphs[k] {
				patches = append(patches, glyphPatch{index: int32(k), glyph: c})
			}
			k++
		}
	}
	runs := make([]shaping.Output, len(line))
	for i := range line {
		runs[i] = line[i]
		runs[i].Glyphs = nil
	}
	s.runs, s.patches = runs, patches
}

// glyphView to glif widziany przez metody układu: indeks przebiegu, indeks w przebiegu i pola.
type glyphView struct {
	run, index int
	glyph      compactGlyph
}

// each przechodzi glify wiersza w kolejności (od końca, gdy reverse), z poprawkami; fn zwraca false,
// żeby przerwać.
func (s *lineStore) each(reverse bool, fn func(glyphView) bool) {
	if !s.compact {
		for r := range s.full {
			if reverse {
				r = len(s.full) - 1 - r
			}
			glyphs := s.full[r].Glyphs
			for i := range glyphs {
				if reverse {
					i = len(glyphs) - 1 - i
				}
				if !fn(glyphView{r, i, compactOf(&glyphs[i])}) {
					return
				}
			}
		}
		return
	}
	starts := make([]int, len(s.counts))
	for i := 1; i < len(starts); i++ {
		starts[i] = starts[i-1] + int(s.counts[i-1])
	}
	for r := range s.runs {
		if reverse {
			r = len(s.runs) - 1 - r
		}
		n := int(s.counts[r])
		for i := 0; i < n; i++ {
			if reverse {
				i = n - 1 - i
			}
			k := starts[r] + i
			glyph := s.glyphs[k]
			if len(s.patches) != 0 {
				if j, found := slices.BinarySearchFunc(s.patches, int32(k), func(p glyphPatch, t int32) int { return int(p.index - t) }); found {
					glyph = s.patches[j].glyph
				}
			}
			if !fn(glyphView{r, i, glyph}) {
				return
			}
			if reverse {
				i = n - 1 - i
			}
		}
	}
}

// glyphCount zwraca liczbę glifów przebiegu r.
func (s *lineStore) glyphCount(r int) int {
	if s.compact {
		return int(s.counts[r])
	}
	return len(s.full[r].Glyphs)
}
