package htmlpdf

import (
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
