package boxes

import (
	"unicode"

	"github.com/go-text/typesetting/language"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/text/hyphen"
)

// gopdfgo: hyphens: auto dla wyrazów rozciętych granicami elementów liniowych. Silnik tekstu dzieli
// wyrazy w obrębie jednego pudełka tekstu, więc „<span>high</span>way” dzielił jak dwa wyrazy albo
// wcale, a element spoza przepływu w środku wyrazu (np. pozycjonowany absolutnie) rozcinał go tak
// samo. Tymczasem elementy liniowe bez zmiany formatowania i elementy spoza przepływu nie mają wpływu
// na dzielenie wyrazów (CSS Text 3 § 5.4; testy WPT hyphens-span-002, hyphens-out-of-flow-002).
// Dla pudełek tekstu z takim wyrazem miejsca podziału z słownika wstawiamy jako miękkie łączniki
// (U+00AD) do wszystkich ich wyrazów, a pudełka dzielą dalej tylko według miękkich łączników
// (TextBox.ManualHyphens). Źródło tego nie robiło.

const softHyphen = '­'

type hyphenSegment struct {
	box   *TextBox
	start int // indeks pierwszego znaku pudełka w połączonym tekście
}

// HyphenateAcrossBoxes przetwarza treść liniową kontenera blokowego box.
func HyphenateAcrossBoxes(box Box) {
	var runs [][]hyphenSegment // ciągi pudełek tekstu bez elementów atomowych między nimi
	var current []hyphenSegment
	found := false
	var walk func(parent Box)
	walk = func(parent Box) {
		for _, child := range parent.Box().Children {
			if !child.Box().IsInNormalFlow() {
				continue
			}
			switch c := child.(type) {
			case *TextBox:
				if len(c.Text) != 0 {
					current = append(current, hyphenSegment{box: c})
					found = found || c.Style.GetHyphens() == "auto"
				}
			case *InlineBox:
				walk(c)
			default:
				if len(current) != 0 {
					runs = append(runs, current)
				}
				current = nil
			}
		}
	}
	walk(box)
	if len(current) != 0 {
		runs = append(runs, current)
	}
	if !found {
		return
	}
	hypheners := map[[3]int]map[language.Language]hyphen.Hyphener{}
	for _, run := range runs {
		if len(run) > 1 {
			hyphenateRun(run, hypheners)
		}
	}
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.Is(unicode.M, r) }

func hyphenateRun(run []hyphenSegment, hypheners map[[3]int]map[language.Language]hyphen.Hyphener) {
	var joined []rune
	owner := []int{} // indeks pudełka dla każdego znaku
	for i := range run {
		run[i].start = len(joined)
		joined = append(joined, run[i].box.Text...)
		for range run[i].box.Text {
			owner = append(owner, i)
		}
	}
	// wyrazy przekraczające granicę pudełek
	affected := map[int]bool{}
	for start := 0; start < len(joined); {
		if !isWordRune(joined[start]) {
			start++
			continue
		}
		end := start
		for end < len(joined) && (isWordRune(joined[end]) || joined[end] == softHyphen) {
			end++
		}
		if owner[start] != owner[end-1] {
			for i := owner[start]; i <= owner[end-1]; i++ {
				affected[i] = true
			}
		}
		start = end
	}
	if len(affected) == 0 {
		return
	}
	// pudełka, których wyrazy dzielimy: dotknięte i te, które dzielą z nimi wyraz (przechodnio,
	// bo wyraz z dotkniętego pudełka może sięgać dalej)
	insertAt := map[int]bool{} // indeksy w joined, przed którymi wstawiamy miękki łącznik
	for start := 0; start < len(joined); {
		if !isWordRune(joined[start]) {
			start++
			continue
		}
		end := start
		for end < len(joined) && (isWordRune(joined[end]) || joined[end] == softHyphen) {
			end++
		}
		word := joined[start:end]
		touched := false
		sameStyle := true
		first := run[owner[start]].box.Style
		for i := owner[start]; i <= owner[end-1]; i++ {
			touched = touched || affected[i]
			style := run[i].box.Style
			if style.GetHyphens() != "auto" || style.GetLang().S != first.GetLang().S {
				sameStyle = false
			}
		}
		hasManual := false
		for _, r := range word {
			if r == softHyphen {
				hasManual = true
			}
		}
		if touched && sameStyle && !hasManual {
			for _, point := range wordPoints(word, first, hypheners) {
				insertAt[start+point] = true
			}
		}
		start = end
	}
	for i := range run {
		if !affected[i] {
			continue
		}
		tb := run[i].box
		var text []rune
		for j, r := range tb.Text {
			if j > 0 && insertAt[run[i].start+j] {
				text = append(text, softHyphen)
			}
			text = append(text, r)
		}
		// podział na samej granicy z następnym pudełkiem: łącznik na końcu tego pudełka
		if next := run[i].start + len(tb.Text); i+1 < len(run) && insertAt[next] {
			text = append(text, softHyphen)
		}
		tb.Text = text
		tb.ManualHyphens = true
	}
}

// wordPoints zwraca miejsca podziału wyrazu według słownika języka stylu, z ograniczeniami
// hyphenate-limit-chars
func wordPoints(word []rune, style pr.ElementStyle, hypheners map[[3]int]map[language.Language]hyphen.Hyphener) []int {
	lang := language.NewLanguage(style.GetLang().S)
	if lang == "" {
		return nil
	}
	lang = hyphen.LanguageFallback(lang)
	if lang == "" {
		return nil
	}
	limits := style.GetHyphenateLimitChars()
	if len(word) < limits.Total {
		return nil
	}
	key := [3]int{limits.Total, limits.Left, limits.Right}
	if hypheners[key] == nil {
		hypheners[key] = map[language.Language]hyphen.Hyphener{}
	}
	h, ok := hypheners[key][lang]
	if !ok {
		h = hyphen.NewHyphener(lang, limits.Left, limits.Right)
		hypheners[key][lang] = h
	}
	return h.Points(word)
}
