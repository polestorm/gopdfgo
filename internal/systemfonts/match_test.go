//go:build systemfonts

package systemfonts

import (
	"strings"
	"testing"

	"github.com/go-text/typesetting/font"
)

func TestEmbeddingRestriction(t *testing.T) {
	for _, c := range []struct {
		fsType  uint16
		blocked string // fragment powodu albo "" dla fontu, który wolno osadzić
	}{
		{0x0000, ""},                         // installable
		{0x0002, "restricted license"},       // restricted license embedding
		{0x0004, ""},                         // preview & print
		{0x0008, ""},                         // editable
		{0x0006, ""},                         // stare fonty: kilka bitów, obowiązuje najmniej restrykcyjny
		{0x000a, ""},                         // jw.
		{0x0104, "subsetting not permitted"}, // bez podzbiorów
		{0x0200, "only bitmap"},              // tylko mapy bitowe
		{0x0202, "restricted license"},
	} {
		got := embeddingRestriction(c.fsType)
		if (c.blocked == "") != (got == "") || !strings.Contains(got, c.blocked) {
			t.Errorf("fsType 0x%04x: %q, oczekiwany fragment %q", c.fsType, got, c.blocked)
		}
	}
}

// Wybór fontu w rodzinie według CSS Fonts 4 § 5.2: szerokość, styl, grubość.
func TestBestMatch(t *testing.T) {
	aspect := func(style font.Style, weight font.Weight) font.Aspect {
		return font.Aspect{Style: style, Weight: weight, Stretch: font.StretchNormal}
	}
	entries := []entry{
		{Aspect: aspect(font.StyleNormal, 300)},
		{Aspect: aspect(font.StyleNormal, 400)},
		{Aspect: aspect(font.StyleNormal, 700)},
		{Aspect: aspect(font.StyleItalic, 400)},
		{Aspect: font.Aspect{Style: font.StyleNormal, Weight: 400, Stretch: font.StretchCondensed}},
	}
	all := []int{0, 1, 2, 3, 4}
	for _, c := range []struct {
		want font.Aspect
		best int
	}{
		{aspect(font.StyleNormal, 400), 1},
		{aspect(font.StyleNormal, 500), 1}, // 400-500: najpierw do 500, potem cieńsze
		{aspect(font.StyleNormal, 600), 2}, // powyżej 500: najpierw grubsze
		{aspect(font.StyleNormal, 200), 0}, // poniżej 400: najpierw cieńsze, potem grubsze
		{aspect(font.StyleItalic, 700), 3}, // styl przed grubością
		{font.Aspect{Style: font.StyleNormal, Weight: 700, Stretch: font.StretchCondensed}, 4}, // szerokość przed resztą
	} {
		if got := bestMatch(entries, all, c.want); got != c.best {
			t.Errorf("%v: font %d, oczekiwany %d", c.want, got, c.best)
		}
	}
}
