//go:build systemfonts

package systemfonts

import "github.com/go-text/typesetting/font"

// bestMatch wybiera z fontów jednej rodziny font najlepiej pasujący do aspektu według CSS Fonts 4
// § 5.2, krok 4: najpierw szerokość (font-stretch), potem styl (font-style), potem grubość
// (font-weight). Przy remisie wygrywa font wcześniejszy w kolejności ścieżek. candidates niepuste.
func bestMatch(entries []entry, candidates []int, aspect font.Aspect) int {
	candidates = keepBest(candidates, func(i int) float64 { return stretchRank(entries[i].Aspect.Stretch, aspect.Stretch) })
	candidates = keepBest(candidates, func(i int) float64 { return styleRank(entries[i].Aspect.Style, aspect.Style) })
	candidates = keepBest(candidates, func(i int) float64 { return weightRank(entries[i].Aspect.Weight, aspect.Weight) })
	return candidates[0]
}

// keepBest zostawia kandydatów o najmniejszej randze (zachowując kolejność).
func keepBest(candidates []int, rank func(int) float64) []int {
	best := rank(candidates[0])
	for _, i := range candidates[1:] {
		if r := rank(i); r < best {
			best = r
		}
	}
	out := candidates[:0:0]
	for _, i := range candidates {
		if rank(i) == best {
			out = append(out, i)
		}
	}
	return out
}

// stretchRank: dla szerokości do normalnej najpierw węższe (od najbliższej), potem szersze; dla
// szerszej od normalnej odwrotnie.
func stretchRank(have, want font.Stretch) float64 {
	if want == 0 {
		want = font.StretchNormal
	}
	if have == 0 {
		have = font.StretchNormal
	}
	return directionalRank(float64(have), float64(want), want <= font.StretchNormal, 1000)
}

// styleRank: italic → italic, potem normal; normal → normal, potem italic (go-text łączy oblique
// z italic).
func styleRank(have, want font.Style) float64 {
	if want == 0 {
		want = font.StyleNormal
	}
	if have == want {
		return 0
	}
	return 1
}

// weightRank: grubość od 400 do 500 najpierw grubsze do 500, potem cieńsze, potem grubsze od 500;
// poniżej 400 najpierw cieńsze, potem grubsze; powyżej 500 najpierw grubsze, potem cieńsze.
func weightRank(have, want font.Weight) float64 {
	if want == 0 {
		want = font.WeightNormal
	}
	if have == 0 {
		have = font.WeightNormal
	}
	h, w := float64(have), float64(want)
	if w >= 400 && w <= 500 {
		switch {
		case h >= w && h <= 500:
			return h - w
		case h < w:
			return 1000 + (w - h)
		default:
			return 2000 + (h - 500)
		}
	}
	return directionalRank(h, w, w < 400, 1000)
}

// directionalRank daje randze wartości have: najpierw wartości po preferowanej stronie want
// (lower: mniejsze lub równe), od najbliższej, potem po drugiej stronie, przesunięte o offset.
func directionalRank(have, want float64, lower bool, offset float64) float64 {
	if lower {
		if have <= want {
			return want - have
		}
		return offset + (have - want)
	}
	if have >= want {
		return have - want
	}
	return offset + (want - have)
}
