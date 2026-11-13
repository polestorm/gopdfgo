// Package draw use a backend and a layout object to
// draw glyphs on the ouput.
package draw

import (
	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

type Context struct {
	Output backend.Canvas         // where to draw the text
	Fonts  text.FontConfiguration // used to find fonts
}

// CreateFirstLine create the text for the first line of [layout], starting at position `(x,y)`.
// It also register the fonts used with [backend.Canvas.AddFont].
func (ctx Context) CreateFirstLine(layout text.EngineLayout, textOverflow string, blockEllipsis pr.TaggedString, scaleX, x, y, angle pr.Fl,
) backend.TextDrawing {
	if layoutG, ok := layout.(*text.TextLayoutGotext); ok {
		return ctx.createFirstLineGotext(layoutG, textOverflow, blockEllipsis, scaleX, x, y, angle)
	}
	return backend.TextDrawing{}
}

// DrawEmoji loads and draws `glyph` onto `dst`.
// It may be used by backend implementations to render emojis.
func DrawEmoji(font backend.Font, glyph backend.GID, extents backend.GlyphExtents,
	fontSize, x, y, xAdvance utils.Fl, dst backend.Canvas,
) {
	if gFont, ok := font.(*gotextFont); ok {
		drawEmojiGotext(gFont, glyph, extents, fontSize, x, y, xAdvance, dst)
	}
}
