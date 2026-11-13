package draw

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"sync"

	"github.com/go-text/typesetting/font"
	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/matrix"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
	"golang.org/x/image/math/fixed"
)

var _ backend.Font = (*gotextFont)(nil)

type gotextFont struct {
	face *font.Face
	id   font.FontID
	size int
}

// gopdfgo: jeden obiekt fontu na parę (krój, rozmiar). Backend rozpoznaje fonty po
// wartości interfejsu, więc nowy wskaźnik dla każdego ciągu tekstu osadzał font wielokrotnie.
type fontKey struct {
	face *font.Face
	size int
}

var (
	fontCacheLock sync.Mutex
	fontCache     = map[fontKey]*gotextFont{}
)

func sharedFont(face *font.Face, id font.FontID, size int) *gotextFont {
	fontCacheLock.Lock()
	defer fontCacheLock.Unlock()
	key := fontKey{face, size}
	if f, ok := fontCache[key]; ok {
		return f
	}
	f := &gotextFont{face, id, size}
	fontCache[key] = f
	return f
}

func (f *gotextFont) Origin() text.FontOrigin { return text.FontOrigin(f.id) }

func (f *gotextFont) Description() backend.FontDescription {
	extents, _ := f.face.FontHExtents()
	meta := f.face.Describe()

	out := backend.FontDescription{
		Family: meta.Family,
		Size:   f.size,
		Style:  fontStyle(meta.Aspect.Style),
		Weight: int(meta.Aspect.Weight),
	}
	if f.size != 0 {
		out.Ascent = extents.Ascender * 1000 / utils.Fl(f.size)
		out.Descent = extents.Descender * 1000 / utils.Fl(f.size)
	}

	out.IsOpentype = true
	// out.IsOpentypeOpentype = face.Type == truetype.TypeOpenType // FIXME : requires recent go-text

	return out
}

func fontStyle(s font.Style) text.FontStyle {
	switch s {
	case font.StyleNormal:
		return text.FSty_Normal
	case font.StyleItalic:
		return text.FSty_Italic
	default:
		return text.FSty_Normal
	}
}

const notFound font.GID = 0xFFFFFFFF

func (ctx Context) createFirstLineGotext(layout *text.TextLayoutGotext,
	textOverflow string, blockEllipsis pr.TaggedString, scaleX, x, y, angle pr.Fl,
) backend.TextDrawing {
	fts := ctx.Fonts.(*text.FontConfigurationGotext)
	style := layout.Style
	textRunes := layout.Text()

	if text.IsWhitespace(textRunes) {
		return backend.TextDrawing{}
	}

	var (
		visualLine = layout.Line
		index      int
		ellipsis   []rune
	)
	if textOverflow == "ellipsis" {
		_, wrapped, _ := fts.LineWrap(textRunes, style, layout.Line, layout.MaxWidth, true)
		visualLine = wrapped.Line
		index = wrapped.NextLine
		if index == len(textRunes) {
			index = -1
		}
	} else if blockEllipsis.Tag != pr.None {
		if blockEllipsis.Tag == pr.Auto {
			ellipsis = []rune("…")
		} else {
			ellipsis = []rune(blockEllipsis.S)
		}
		textRunes = slices.Concat(textRunes, ellipsis)
		line := fts.WrapWordBreak(textRunes, style, layout.MaxWidth, true)
		visualLine = line.Layout.(*text.TextLayoutGotext).Line
		index = line.ResumeAt
		// Remove last word if hyphenated
		// TODO
		// newText := layout.Text()
		// if hyph := style.HyphenateCharacter; strings.HasSuffix(string(newText), hyph) {
		// 	lastWordEnd := fts.GetLastWordEnd(newText[:len(newText)-len([]rune(hyph))])
		// 	if lastWordEnd != -1 && lastWordEnd != 0 {
		// 		newText = newText[:lastWordEnd]
		// 	}
		// }
		// layout.SetText(string(newText) + ellipsis)
	}

	if blockEllipsis.Tag != pr.None {
		for index != 0 && index != -1 {
			lastWordEnd := fts.GetLastWordEnd(textRunes[:len(textRunes)-len(ellipsis)])
			if lastWordEnd == -1 {
				break
			}
			textRunes = slices.Concat(textRunes[:lastWordEnd], ellipsis)
			line := fts.WrapWordBreak(textRunes, style, layout.MaxWidth, true)
			index = line.ResumeAt
			visualLine = line.Layout.(*text.TextLayoutGotext).Line
		}
	}

	var (
		output        backend.TextDrawing
		lastFont      *font.Font
		lastFontChars *backend.FontChars
		xAdvance      pr.Fl
	)

	fontSize := style.FontDescription.Size

	output.FontSize = fontSize
	output.ScaleX = scaleX
	output.X, output.Y = x, y
	output.Angle = angle
	output.Text = textRunes

	for _, run := range visualLine {
		// Font content
		face := run.Face
		outFont := lastFontChars
		if lastFont != face.Font { // add a new "run"
			location := fts.FontLocation(face.Font)
			content := fts.FontContent(location)
			backendFont := sharedFont(face, location, run.Size.Round())
			outFont = ctx.Output.AddFont(backendFont, content)

			lastFont = face.Font
			lastFontChars = outFont

			outRun := backend.TextRun{Font: backendFont}
			output.Runs = append(output.Runs, outRun)
		} // else use the last one

		runDst := &output.Runs[len(output.Runs)-1]

		currentL := len(runDst.Glyphs)
		nextL := currentL + len(run.Glyphs)
		runDst.Glyphs = slices.Grow(runDst.Glyphs, len(run.Glyphs))[0:nextL]
		for i, glyphInfo := range run.Glyphs {
			outGlyph := &runDst.Glyphs[currentL+i]
			gAdvance := fixedToFloat(glyphInfo.Advance)
			glyph := glyphInfo.GlyphID
			if glyph == 0 {
				glyph = notFound
			}

			outGlyph.Offset = fixedToFloat(glyphInfo.XOffset) * 1000 / fontSize
			outGlyph.Rise = fixedToFloat(glyphInfo.YOffset) * 1000 / fontSize
			outGlyph.Glyph = backend.GID(glyph)

			if glyph != notFound {
				// Ink bounding box and logical widths in font
				if _, in := outFont.Extents[outGlyph.Glyph]; !in {
					extents, _ := face.GlyphExtents(glyph)
					x1, y1, x2, y2 := extents.XBearing, -extents.YBearing-extents.Height,
						extents.XBearing+extents.Width, -extents.YBearing
					if int(x1) < outFont.Bbox[0] {
						outFont.Bbox[0] = int(x1 * 1000 / fontSize)
					}
					if int(y1) < outFont.Bbox[1] {
						outFont.Bbox[1] = int(y1 * 1000 / fontSize)
					}
					if int(x2) > outFont.Bbox[2] {
						outFont.Bbox[2] = int(x2 * 1000 / fontSize)
					}
					if int(y2) > outFont.Bbox[3] {
						outFont.Bbox[3] = int(y2 * 1000 / fontSize)
					}
					// gopdfgo: Width to szerokość logiczna (posunięcie), nie szerokość obrysu, w tysięcznych
					// firetu wg jednostek fontu. Trafia do tablicy /W w PDF; z szerokością obrysu każdy glif
					// dostawał korektę w TJ i tekst nie dawał się wyodrębnić.
					upem := pr.Fl(face.Upem())
					outFont.Extents[outGlyph.Glyph] = backend.GlyphExtents{
						Width:  int(math.Round(float64(face.HorizontalAdvance(glyph) / upem * 1000))),
						Y:      int(extents.YBearing / upem * 1000),
						Height: int(extents.Height / upem * 1000),
					}
				}

				// Mapping between glyphs and characters
				outGlyph.TextOffset, outGlyph.TextLength = glyphInfo.TextIndex(), glyphInfo.RunesCount()
				if _, in := outFont.Cmap[outGlyph.Glyph]; !in {
					outFont.Cmap[outGlyph.Glyph] = textRunes[outGlyph.TextOffset : outGlyph.TextOffset+outGlyph.TextLength]
				}
			}

			// Kerning, word spacing, letter spacing
			outGlyph.Kerning = int(pr.Fl(outFont.Extents[outGlyph.Glyph].Width) - gAdvance*1000/fontSize + outGlyph.Offset)
			// advance
			outGlyph.XAdvance = xAdvance
			xAdvance += gAdvance*1000/fontSize + outGlyph.Offset - pr.Fl(outGlyph.Kerning)
		}
	}

	return output
}

func drawEmojiGotext(font_ *gotextFont, glyph backend.GID, extents backend.GlyphExtents,
	fontSize, x, y, xAdvance utils.Fl, dst backend.Canvas,
) {
	face := font_.face
	data := face.GlyphData(font.GID(glyph))

	switch data := data.(type) {
	// TODO: more formats
	case font.GlyphBitmap:
		if data.Format == font.PNG {
			img := backend.RasterImage{
				Content:   bytes.NewReader(data.Data),
				MimeType:  "image/png",
				Rendering: "",
				ID:        utils.Hash(fmt.Sprintf("%p-%d", face, glyph)),
			}

			d := utils.Fl(extents.Width) / 1000
			a := utils.Fl(data.Width) / utils.Fl(data.Height) * d
			f := utils.Fl(-extents.Y-extents.Height)/1000 - fontSize
			f = y + f
			e := xAdvance / 1000
			e = x + e*fontSize

			dst.OnNewStack(func() {
				dst.State().Transform(matrix.New(a, 0, 0, d, e, f))
				dst.DrawRasterImage(img, fontSize, fontSize)
			})
		}
	}
}

func floatToFixed(v pr.Fl) fixed.Int26_6 { return fixed.Int26_6(v * 64) }
func fixedToFloat(v fixed.Int26_6) pr.Fl { return pr.Fl(v) / 64 }
