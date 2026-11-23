package text

// gopdfgo: plik dodany (ADR 0045). Pamięć podręczna sparsowanych fontów współdzielona między renderowaniami.
//
// Biblioteka go-text parsuje font w całości (font.NewFont czyta wszystkie glify, nazwy glifów z tablicy
// post, tablice BASE i GSUB/GPOS), więc dokument z kilkoma fontami płacił za parsowanie przy każdym
// renderowaniu: na fakturze z 14 fontami w Options.Fonts i 6 regułami @font-face ok. 35% czasu procesora
// i ok. 65% przydziałów pamięci (pomiar 2026-11-23, docs/benchmark.md § 11). Sparsowany font
// (*font.Font) jest po parsowaniu tylko czytany, więc może służyć wielu renderowaniom; każde
// renderowanie dostaje własną płytką kopię struktury, bo mapy fontów i konfiguracji rozpoznają twarze po
// wskaźniku *font.Font, a reguła @font-face z unicode-range podmienia w kopii pole Cmap.
//
// Kluczem jest treść pliku (skrót maphash i porównanie bajtów), nie nazwa ani adres: ten sam plik
// podany pod różnymi nazwami albo przeczytany ponownie przez Resolver trafia w tę samą pozycję.
//
// Pozycja ma dwa stopnie (ADR 0047): streszczenie fontu (Summary: cmap, rodzina i odmiana, oś wght),
// czytane z tablic cmap, OS/2, head, maxp, name i fvar, wystarcza do rejestracji twarzy w fontmapie;
// pełne parsowanie (Parse, z glyf, CFF, GSUB, GPOS) następuje dopiero, gdy twarz jest naprawdę użyta
// (zob. lazy_fonts.go).

import (
	"bytes"
	"hash/maphash"
	"sync"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
)

// ParsedFont to jeden font z pliku (z kolekcji TTC kilka) w postaci gotowej do użycia.
type ParsedFont struct {
	Font *font.Font
	// WeightAxis: font zmienny z osią wght (zob. hasWeightAxis)
	WeightAxis bool
}

// FontCache przechowuje sparsowane fonty według treści pliku. Bezpieczny dla wielu wątków.
// Zero value nie nadaje się do użycia; zob. NewFontCache.
type FontCache struct {
	mu       sync.Mutex
	seed     maphash.Seed
	entries  map[uint64][]*fontCacheEntry
	size     int64 // suma długości plików w pamięci
	maxBytes int64
	clock    uint64
	borrow   bool // treść nie jest kopiowana, zob. NewTransientFontCache
}

// FontSummary to dane fontu potrzebne do rejestracji twarzy bez pełnego parsowania: cmap (pokrycie
// znaków), rodzina i odmiana (jak font.Font.Describe) i oś wght.
type FontSummary struct {
	Cmap        font.Cmap
	Description font.Description
	WeightAxis  bool
}

type fontCacheEntry struct {
	content   []byte // kopia treści do porównania
	summaries []FontSummary
	fonts     []ParsedFont // nil, dopóki nikt nie poprosił o pełne parsowanie
	supported int8         // wynik Supported: 0 nieznany, 1 nie, 2 tak
	err       error
	lastUsed  uint64
}

// NewFontCache tworzy pamięć podręczną mieszczącą pliki fontów o łącznej długości do maxBytes;
// po przekroczeniu usuwane są pozycje najdawniej użyte. maxBytes <= 0 oznacza brak limitu.
func NewFontCache(maxBytes int64) *FontCache {
	return &FontCache{seed: maphash.MakeSeed(), entries: map[uint64][]*fontCacheEntry{}, maxBytes: maxBytes}
}

// NewTransientFontCache tworzy pamięć podręczną na czas jednego renderowania, bez limitu: zapamiętuje
// treść plików bez kopiowania, więc podane bufory nie mogą się zmieniać, dopóki jest używana. Łączy
// ten sam plik podany w Options.Fonts i wskazany przez @font-face (parsowany wtedy raz, nie dwa razy).
func NewTransientFontCache() *FontCache {
	c := NewFontCache(0)
	c.borrow = true
	return c
}

// Summary zwraca streszczenia fontów z pliku content (jeden dla TTF/OTF, kilka dla TTC) bez pełnego
// parsowania. Błąd jest ten sam co z Parse: streszczenie sprawdza te same tablice wymagane (cmap, head,
// maxp) co font.NewFont, w tej samej kolejności.
func (c *FontCache) Summary(content []byte) ([]FontSummary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entry(content)
	return entry.summaries, entry.err
}

// Parse zwraca fonty z pliku content (jeden dla TTF/OTF, kilka dla TTC), parsując go tylko za
// pierwszym razem. Błąd parsowania też jest zapamiętywany. Zwracanych *font.Font nie wolno
// modyfikować; zob. Copy.
func (c *FontCache) Parse(content []byte) ([]ParsedFont, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entry(content)
	if entry.err == nil && entry.fonts == nil {
		if OnFontParsed != nil {
			OnFontParsed(content)
		}
		entry.fonts, entry.err = parseFontFile(content)
	}
	return entry.fonts, entry.err
}

// Supported zwraca zapamiętany wynik compute(content) (sprawdzenie zapisu PDF, czy font ma kontury do
// osadzenia) dla pliku z pamięci; wylicza go przy pierwszym wywołaniu. Dla treści spoza pamięci (np. fontów
// systemowych) wylicza go za każdym razem, bez dodawania pozycji.
func (c *FontCache) Supported(content []byte, compute func([]byte) bool) bool {
	key := maphash.Bytes(c.seed, content)
	c.mu.Lock()
	var found *fontCacheEntry
	for _, entry := range c.entries[key] {
		if bytes.Equal(entry.content, content) {
			found = entry
			break
		}
	}
	if found == nil {
		c.mu.Unlock()
		return compute(content)
	}
	defer c.mu.Unlock()
	if found.supported == 0 {
		found.supported = 1
		if compute(content) {
			found.supported = 2
		}
	}
	return found.supported == 2
}

// entry zwraca pozycję dla treści content, tworząc ją ze streszczeniem przy pierwszym wywołaniu.
// Wywoływane pod c.mu.
func (c *FontCache) entry(content []byte) *fontCacheEntry {
	key := maphash.Bytes(c.seed, content)
	c.clock++
	for _, entry := range c.entries[key] {
		if bytes.Equal(entry.content, content) {
			entry.lastUsed = c.clock
			return entry
		}
	}
	entry := &fontCacheEntry{content: content, lastUsed: c.clock}
	if !c.borrow {
		entry.content = bytes.Clone(content)
	}
	entry.summaries, entry.err = summarizeFontFile(entry.content)
	c.entries[key] = append(c.entries[key], entry)
	c.size += int64(len(content))
	c.evict(entry)
	return entry
}

// evict usuwa pozycje najdawniej użyte, dopóki suma długości przekracza limit; keep zostaje zawsze.
func (c *FontCache) evict(keep *fontCacheEntry) {
	for c.maxBytes > 0 && c.size > c.maxBytes {
		var (
			oldKey   uint64
			oldIndex = -1
			oldest   uint64
		)
		for key, list := range c.entries {
			for i, entry := range list {
				if entry != keep && (oldIndex < 0 || entry.lastUsed < oldest) {
					oldKey, oldIndex, oldest = key, i, entry.lastUsed
				}
			}
		}
		if oldIndex < 0 {
			return
		}
		list := c.entries[oldKey]
		c.size -= int64(len(list[oldIndex].content))
		list = append(list[:oldIndex], list[oldIndex+1:]...)
		if len(list) == 0 {
			delete(c.entries, oldKey)
		} else {
			c.entries[oldKey] = list
		}
	}
}

// parseFontFile parsuje plik tak jak fontscan.FontMap.AddFont (font.ParseTTC) i loadOneFont
// (opentype.NewLoaders, font.NewFont): błąd któregokolwiek fontu kolekcji to błąd całego pliku.
func parseFontFile(content []byte) ([]ParsedFont, error) {
	loaders, err := opentype.NewLoaders(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	out := make([]ParsedFont, len(loaders))
	for i, ld := range loaders {
		ft, err := font.NewFont(ld)
		if err != nil {
			return nil, err
		}
		out[i] = ParsedFont{Font: ft, WeightAxis: hasWeightAxis(ld)}
	}
	return out, nil
}

// summarizeFontFile czyta streszczenia fontów pliku. Tablice wymagane i ich kolejność jak w
// font.NewFont (OS/2 dla strony kodowej cmap, cmap, head, maxp), więc plik odrzucany przez NewFont jest
// odrzucany tutaj z tym samym błędem, a plik przyjęty tutaj NewFont przyjmie; cmap powstaje tym samym
// wywołaniem font.ProcessCmap co w NewFont.
func summarizeFontFile(content []byte) ([]FontSummary, error) {
	loaders, err := opentype.NewLoaders(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	out := make([]FontSummary, len(loaders))
	var buffer []byte
	for i, ld := range loaders {
		raw, _ := ld.RawTable(opentype.MustNewTag("OS/2"))
		os2, _, _ := tables.ParseOs2(raw)
		raw, err := ld.RawTable(opentype.MustNewTag("cmap"))
		if err != nil {
			return nil, err
		}
		cmapTable, _, err := tables.ParseCmap(raw)
		if err != nil {
			return nil, err
		}
		cmap, _, err := font.ProcessCmap(cmapTable, os2.FontPage())
		if err != nil {
			return nil, err
		}
		if _, _, err := font.LoadHeadTable(ld, nil); err != nil {
			return nil, err
		}
		raw, err = ld.RawTable(opentype.MustNewTag("maxp"))
		if err != nil {
			return nil, err
		}
		if _, _, err := tables.ParseMaxp(raw); err != nil {
			return nil, err
		}
		out[i].Cmap = cmap
		out[i].Description, buffer = font.Describe(ld, buffer)
		out[i].WeightAxis = hasWeightAxis(ld)
	}
	return out, nil
}

// Copy zwraca płytką kopię fontu: własny wskaźnik dla map rozpoznających twarze po *font.Font,
// te same (tylko czytane) tablice.
func (p ParsedFont) Copy() *font.Font {
	copied := *p.Font
	return &copied
}
