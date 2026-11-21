package text

// layoutCache (gopdfgo) pamięta wyniki łamania pierwszego wiersza (WrapWordBreak) z ograniczonym
// rozmiarem. Źródło trzymało je w mapie bez limitu przez całe renderowanie. Kluczem jest cały pozostały
// tekst akapitu, więc długi akapit złamany na n wierszy kosztował około n²/2 znaków samych kluczy,
// a do tego glify każdego wiersza; przy książce 256 stron było to ok. 70 MB żywej pamięci, rosnące
// liniowo z liczbą stron, choć strony rysowane w trakcie układu (ADR 0027) do wyników nie wracają.
//
// Pamięć ma dwa pokolenia: wpisy trafiają do bieżącego, a gdy jego szacowany rozmiar przekroczy
// limit, bieżące staje się poprzednim, a dawne poprzednie jest porzucane. Trafienie w poprzednim
// przenosi wpis do bieżącego. Ponowne obliczenia (próby podziału strony, szerokości min/max-content)
// odwołują się do świeżych wpisów, więc trafiają dalej, a pamięć jest ograniczona do ok. dwóch
// limitów. Wynik nie zależy od trafień: WrapWordBreak jest funkcją swoich argumentów, a pamięć tylko
// oszczędza obliczeń. Mapy nie są nigdy przeglądane, więc kolejność iteracji nie ma znaczenia.
type layoutCache struct {
	current, previous map[textKey]FirstLine
	currentCost       int
}

// layoutCacheLimit to szacowany rozmiar jednego pokolenia w bajtach.
const layoutCacheLimit = 16 << 20

func newLayoutCache() layoutCache {
	return layoutCache{current: make(map[textKey]FirstLine)}
}

func (c *layoutCache) get(key textKey) (FirstLine, bool) {
	if l, ok := c.current[key]; ok {
		return l, true
	}
	if l, ok := c.previous[key]; ok {
		delete(c.previous, key)
		c.put(key, l)
		return l, true
	}
	return FirstLine{}, false
}

func (c *layoutCache) put(key textKey, l FirstLine) {
	c.current[key] = l
	c.currentCost += entryCost(key, l)
	if c.currentCost > layoutCacheLimit {
		c.previous = c.current
		c.current = make(map[textKey]FirstLine)
		c.currentCost = 0
	}
}

// entryCost szacuje pamięć wpisu: tekst klucza (bajty UTF-8), glify wiersza w zapisie zwartym
// (zob. compactGlyph) i stały narzut struktur.
func entryCost(key textKey, l FirstLine) int {
	cost := len(key.text) + 256
	if layout, ok := l.Layout.(*TextLayoutGotext); ok && layout != nil {
		cost += 48*layout.store.glyphTotal() + 4*len(layout.text)
	}
	return cost
}

// glyphTotal zwraca liczbę glifów wiersza.
func (s *lineStore) glyphTotal() int {
	if s.compact {
		return len(s.glyphs)
	}
	n := 0
	for _, run := range s.full {
		n += len(run.Glyphs)
	}
	return n
}
