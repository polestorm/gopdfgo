package text

import (
	"strings"
	"unicode"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

// Autospace (gopdfgo) to rodzaje odstępów text-autospace (CSS Text 4 § 6.4) jako flagi.
type Autospace uint8

const (
	AutospaceIdeographAlpha   Autospace = 1 << iota // ideograph-alpha
	AutospaceIdeographNumeric                       // ideograph-numeric
)

// newAutospace (gopdfgo) zamienia wartość obliczoną text-autospace na flagi. normal i auto oznaczają
// ideograph-alpha ideograph-numeric (§ 6.4: normal; auto zostawia wybór, bierzemy to samo, co
// Chromium). punctuation, insert i replace nie zmieniają układu: odstępów przy interpunkcji nie
// dodajemy, a w tekście nie ma spacji, które trzeba by zastępować.
func newAutospace(value string) Autospace {
	switch value {
	case "", "normal", "auto":
		return AutospaceIdeographAlpha | AutospaceIdeographNumeric
	case "no-autospace":
		return 0
	}
	var out Autospace
	for _, keyword := range strings.Fields(value) {
		switch keyword {
		case "ideograph-alpha":
			out |= AutospaceIdeographAlpha
		case "ideograph-numeric":
			out |= AutospaceIdeographNumeric
		}
	}
	return out
}

// autospaceClass (gopdfgo) to klasy znaków z CSS Text 4 § 6.4.1.
type autospaceClass uint8

const (
	autospaceOther autospaceClass = iota
	autospaceIdeograph
	autospaceLetter
	autospaceNumeral
)

// isFullWidth (gopdfgo): znaki o szerokości wschodnioazjatyckiej F (UAX 11): formy pełnej szerokości
// i spacja ideograficzna.
func isFullWidth(r rune) bool {
	return r == 0x3000 || 0xFF01 <= r && r <= 0xFF60 || 0xFFE0 <= r && r <= 0xFFE6
}

// classifyAutospace (gopdfgo) przypisuje znak do klasy z CSS Text 4 § 6.4.1: ideogramy to pismo Han
// oraz hiragana i katakana (bez kropki środkowej U+30FB), litery to kategorie L i M, cyfry to
// kategoria Nd, w obu przypadkach poza ideogramami i znakami pełnej szerokości (tak jak Chromium).
func classifyAutospace(r rune) autospaceClass {
	if r < 0x80 {
		switch {
		case 'a' <= r && r <= 'z', 'A' <= r && r <= 'Z':
			return autospaceLetter
		case '0' <= r && r <= '9':
			return autospaceNumeral
		}
		return autospaceOther
	}
	if 0x3041 <= r && r <= 0x30FF {
		if r == 0x30FB || r == 0x30A0 || 0x3099 <= r && r <= 0x309C {
			return autospaceOther
		}
		return autospaceIdeograph
	}
	if 0x31F0 <= r && r <= 0x31FF || unicode.Is(unicode.Han, r) && unicode.IsLetter(r) {
		return autospaceIdeograph
	}
	if isFullWidth(r) {
		return autospaceOther
	}
	if unicode.IsLetter(r) || unicode.IsMark(r) {
		return autospaceLetter
	}
	if unicode.Is(unicode.Nd, r) {
		return autospaceNumeral
	}
	return autospaceOther
}

// autospaceBetween (gopdfgo) mówi, czy między znakami a i b (w kolejności logicznej) wstawia się
// odstęp text-autospace.
func autospaceBetween(a, b rune, flags Autospace) bool {
	ca, cb := classifyAutospace(a), classifyAutospace(b)
	if ca != autospaceIdeograph {
		ca, cb = cb, ca
	}
	if ca != autospaceIdeograph {
		return false
	}
	switch cb {
	case autospaceLetter:
		return flags&AutospaceIdeographAlpha != 0
	case autospaceNumeral:
		return flags&AutospaceIdeographNumeric != 0
	}
	return false
}

// autospaceBoundary (gopdfgo) mówi, czy przed znakiem text[i] stoi granica z odstępem
// text-autospace. Znaki łączące (M) należą do znaku bazowego, więc po stronie poprzedniego znaku je
// pomijamy, a znak łączący po prawej granicy nie tworzy.
func autospaceBoundary(text []rune, i int, flags Autospace) bool {
	if i <= 0 || i >= len(text) || unicode.IsMark(text[i]) {
		return false
	}
	j := i - 1
	for j > 0 && unicode.IsMark(text[j]) {
		j--
	}
	return autospaceBetween(text[j], text[i], flags)
}

// applyAutospace (gopdfgo) dodaje odstęp 1/8 ic (CSS Text 4 § 6.4) za ostatnim glifem znaku, po którym
// wypada granica ideogramu z literą albo cyfrą innego pisma. Tylko w tekście poziomym od lewej do
// prawej i tylko wewnątrz jednego pudełka tekstu; granic między elementami nie uwzględniamy.
func applyAutospace(runs shaping.Line, text []rune, space fixed.Int26_6, flags Autospace) {
	if flags == 0 || space == 0 {
		return
	}
	for r := range runs {
		run := &runs[r]
		if run.Direction != di.DirectionLTR {
			continue
		}
		changed := false
		for g := range run.Glyphs {
			glyph := run.Glyphs[g]
			// ostatni glif klastra: następny glif ma inny klaster albo go nie ma
			if g+1 < len(run.Glyphs) && run.Glyphs[g+1].ClusterIndex == glyph.ClusterIndex {
				continue
			}
			end := glyph.ClusterIndex + max(glyph.RuneCount, 1)
			if !autospaceBoundary(text, end, flags) {
				continue
			}
			run.Glyphs[g].Advance += space
			run.Glyphs[g].XAdvance += space
			changed = true
		}
		if changed {
			run.RecomputeAdvance()
		}
	}
}
