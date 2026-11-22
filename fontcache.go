package gopdfgo

import (
	"fmt"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
)

// FontCache przechowuje sparsowane fonty między wywołaniami Render (ADR 0045). Program renderujący
// wiele dokumentów tymi samymi fontami tworzy jedną pamięć podręczną i podaje ją w Options.FontCache;
// fonty z Options.Fonts i z reguł @font-face (pobrane przez Resolver) są wtedy parsowane raz, przy
// pierwszym użyciu. Wynik renderowania jest taki sam jak bez pamięci podręcznej.
//
// Pozycje rozpoznawane są po treści pliku, nie po nazwie. Pamięć podręczna jest bezpieczna dla wielu
// wątków i może być współdzielona przez dowolnie wiele wywołań Render.
type FontCache struct {
	cache *text.FontCache
}

// NewFontCache tworzy pamięć podręczną fontów. maxBytes ogranicza łączną długość zapamiętanych plików
// fontów (sparsowany font zajmuje w pamięci zwykle kilka razy więcej niż plik); po przekroczeniu
// usuwane są fonty najdawniej użyte. maxBytes <= 0 oznacza brak limitu.
func NewFontCache(maxBytes int64) *FontCache {
	return &FontCache{cache: text.NewFontCache(maxBytes)}
}

// addCachedFont dodaje do mapy fontów font z pamięci podręcznej, tak jak fontscan.FontMap.AddFont
// dodaje font parsowany od nowa: każda twarz pliku (kilka dla kolekcji TTC) z położeniem id i
// numerem w kolekcji, z rodziną i odmianą odczytanymi z fontu.
func addCachedFont(fontMap *fontscan.FontMap, cache *FontCache, data []byte, id string) error {
	parsed, err := cache.cache.Parse(data)
	if err != nil {
		return fmt.Errorf("unsupported font resource: %s", err)
	}
	for i, p := range parsed {
		ft := p.Copy()
		fontMap.AddFace(font.NewFace(ft), fontscan.Location{File: id, Index: uint16(i)}, ft.Describe())
	}
	return nil
}
