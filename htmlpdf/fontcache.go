package htmlpdf

import (
	"github.com/polestorm/gopdfgo/internal/webrender/text"
)

// FontCache stores parsed fonts between Render calls (ADR 0045). A program that renders many
// documents with the same fonts creates one cache and passes it in Options.FontCache; fonts
// from Options.Fonts and from @font-face rules (fetched through Resolver) are then parsed once, on
// first use. The rendering output is the same as without the cache.
//
// Entries are identified by the content of the file, not by its name. The cache is safe for concurrent
// use and may be shared by any number of Render calls.
type FontCache struct {
	cache *text.FontCache
}

// NewFontCache creates a font cache. maxBytes limits the total length of the font files kept
// (a parsed font usually takes several times more memory than its file); when the limit is exceeded,
// the least recently used fonts are removed. maxBytes <= 0 means no limit.
func NewFontCache(maxBytes int64) *FontCache {
	return &FontCache{cache: text.NewFontCache(maxBytes)}
}
