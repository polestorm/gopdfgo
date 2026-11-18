package text

import (
	"github.com/go-text/typesetting/font"
	pa "github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/css/validation"
)

// faceMetrics (gopdfgo) to deskryptory metryk reguły @font-face (CSS Fonts 5 § 4.10–4.12):
// sizeAdjust mnoży rozmiar, w którym twarz jest składana i rysowana, wraz z jej metrykami; ascent,
// descent i lineGap (pr.Float, ułamek rozmiaru) zastępują wartości z tablic fontu, nil zostawia je.
// Źródło tych deskryptorów nie znało.
type faceMetrics struct {
	sizeAdjust               pr.Fl
	ascent, descent, lineGap pr.MaybeFloat
}

func newFaceMetrics(descriptors validation.FontFaceDescriptors) faceMetrics {
	out := faceMetrics{sizeAdjust: 1, ascent: descriptors.AscentOverride,
		descent: descriptors.DescentOverride, lineGap: descriptors.LineGapOverride}
	if v, ok := descriptors.SizeAdjust.(pr.Float); ok {
		out.sizeAdjust = pr.Fl(v)
	}
	return out
}

// FaceSizeAdjust (gopdfgo) zwraca mnożnik rozmiaru z deskryptora size-adjust twarzy (1 bez niego).
func (fc *FontConfigurationGotext) FaceSizeAdjust(ft *font.Font) pr.Fl {
	if metrics, ok := fc.fontsMetrics[ft]; ok {
		return metrics.sizeAdjust
	}
	return 1
}

// rangeCmap (gopdfgo) ogranicza tablicę cmap fontu do zakresów z deskryptora unicode-range reguły
// @font-face (CSS Fonts 4 § 4.5): znak spoza zakresów nie jest pokrywany przez tę twarz, więc wybór
// fontu przechodzi do następnej rodziny, a twarz nie może być pierwszym dostępnym fontem, jeśli
// zakresy nie obejmują U+0020. Źródło deskryptor czytało, ale go nie stosowało.
type rangeCmap struct {
	cmap   font.Cmap
	ranges []pa.UnicodeRange
}

func (c rangeCmap) contains(r rune) bool {
	for _, rg := range c.ranges {
		if uint32(r) >= rg.Start && uint32(r) <= rg.End {
			return true
		}
	}
	return false
}

func (c rangeCmap) Lookup(r rune) (font.GID, bool) {
	if !c.contains(r) {
		return 0, false
	}
	return c.cmap.Lookup(r)
}

func (c rangeCmap) Iter() font.CmapIter { return &rangeCmapIter{iter: c.cmap.Iter(), owner: c} }

// rangeCmapIter: Char iteratorów go-text przesuwa pozycję, więc bieżący znak trzymamy u siebie
type rangeCmapIter struct {
	iter  font.CmapIter
	owner rangeCmap
	r     rune
	gid   font.GID
}

func (it *rangeCmapIter) Next() bool {
	for it.iter.Next() {
		if it.r, it.gid = it.iter.Char(); it.owner.contains(it.r) {
			return true
		}
	}
	return false
}

func (it *rangeCmapIter) Char() (rune, font.GID) { return it.r, it.gid }
