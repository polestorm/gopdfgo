package document

import (
	"math"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	"github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/text/draw"

	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
)

// (offsetX=0,textOverflow="clip")
func (ctx drawContext) drawText(textbox *bo.TextBox, offsetX fl, textOverflow string, blockEllipsis pr.TaggedString) {
	if textbox.Style.GetVisibility() != "visible" {
		return
	}

	// Draw text decoration

	decoration := textbox.Style.GetTextDecorationLine()
	color := tree.ResolveColor(textbox.Style, pr.PTextDecorationColor)

	var offsetY pr.Float

	metrics := textbox.TextLayout.Metrics()

	if decoration&pr.Overline != 0 {
		thickness := metrics.UnderlineThickness
		offsetY = textbox.Baseline.V() - pr.Float(metrics.Ascent) + pr.Float(thickness)/2
		ctx.drawTextDecoration(textbox, offsetX, pr.Fl(offsetY), thickness, color.RGBA)
	}
	if decoration&pr.Underline != 0 {
		thickness := metrics.UnderlineThickness
		offsetY = textbox.Baseline.V() - pr.Float(metrics.UnderlinePosition) + pr.Float(thickness)/2
		ctx.drawTextDecoration(textbox, offsetX, pr.Fl(offsetY), thickness, color.RGBA)
	}

	x, y := pr.Fl(textbox.PositionX), pr.Fl(textbox.PositionY+textbox.Baseline.V())
	textbox.TextLayout.ApplyJustification()

	// gopdfgo: text-shadow (CSS Text Decoration 3 § 4). Cienie leżą pod tekstem, pierwszy z listy na
	// wierzchu. PDF nie ma rozmycia, więc cień rozmyty przybliżamy kopiami tekstu na dwóch okręgach
	// wokół przesunięcia, każda o ułamku krycia; cień ostry to jedna kopia.
	shadows := textbox.Style.GetTextShadow()
	for index := len(shadows) - 1; index >= 0; index-- {
		shadow := shadows[index]
		// gopdfgo: także barwy względne zależne od currentColor (CSS Color 5)
		shadowColor := shadow.Color.ResolveCurrentColor(textbox.Style.GetColor()).RGBA
		if shadowColor.A == 0 {
			continue
		}
		sx, sy := x+pr.Fl(shadow.X.Value), y+pr.Fl(shadow.Y.Value)
		blur := pr.Fl(shadow.Blur.Value)
		if blur <= 0 {
			ctx.dst.State().SetColorRgba(shadowColor, false)
			ctx.drawFirstLine(textbox, textOverflow, blockEllipsis, sx, sy, shadowColor)
			continue
		}
		const copies = 8
		faint := shadowColor
		faint.A = shadowColor.A / 6
		ctx.dst.State().SetColorRgba(faint, false)
		ctx.drawFirstLine(textbox, textOverflow, blockEllipsis, sx, sy, faint)
		for _, radius := range [2]pr.Fl{blur * 0.25, blur * 0.5} {
			for k := 0; k < copies; k++ {
				angle := 2 * math.Pi * float64(k) / copies
				ctx.drawFirstLine(textbox, textOverflow, blockEllipsis,
					sx+radius*pr.Fl(math.Cos(angle)), sy+radius*pr.Fl(math.Sin(angle)), faint)
			}
		}
	}

	// gopdfgo: glify wypełnia -webkit-text-fill-color (Compatibility Standard § 5.2), domyślnie
	// currentcolor, czyli color
	fill := textbox.Style.GetWebkitTextFillColor()
	fillColor := fill.ResolveCurrentColor(textbox.Style.GetColor()).RGBA // gopdfgo: zob. ResolveCurrentColor
	ctx.dst.State().SetColorRgba(fillColor, false)
	ctx.drawFirstLine(textbox, textOverflow, blockEllipsis, x, y, fillColor)

	if decoration&pr.LineThrough != 0 {
		thickness := metrics.StrikethroughThickness
		offsetY = textbox.Baseline.V() - pr.Float(metrics.StrikethroughPosition)
		ctx.drawTextDecoration(textbox, offsetX, pr.Fl(offsetY), thickness, color.RGBA)
	}
}

// drawTextDecorationsInColor (gopdfgo) rysuje wszystkie linie dekoracji pudełka tekstu w podanym
// kolorze, w tych samych miejscach co drawText; służy masce background-clip: text, która obejmuje
// też dekoracje tekstu (CSS Backgrounds 4 § 3.6; test WPT clip-text-text-decorations).
func (ctx drawContext) drawTextDecorationsInColor(textbox *bo.TextBox, color Color) {
	decoration := textbox.Style.GetTextDecorationLine()
	if decoration == 0 {
		return
	}
	metrics := textbox.TextLayout.Metrics()
	if decoration&pr.Overline != 0 {
		thickness := metrics.UnderlineThickness
		offsetY := textbox.Baseline.V() - pr.Float(metrics.Ascent) + pr.Float(thickness)/2
		ctx.drawTextDecoration(textbox, 0, pr.Fl(offsetY), thickness, color)
	}
	if decoration&pr.Underline != 0 {
		thickness := metrics.UnderlineThickness
		offsetY := textbox.Baseline.V() - pr.Float(metrics.UnderlinePosition) + pr.Float(thickness)/2
		ctx.drawTextDecoration(textbox, 0, pr.Fl(offsetY), thickness, color)
	}
	if decoration&pr.LineThrough != 0 {
		thickness := metrics.StrikethroughThickness
		offsetY := textbox.Baseline.V() - pr.Float(metrics.StrikethroughPosition)
		ctx.drawTextDecoration(textbox, 0, pr.Fl(offsetY), thickness, color)
	}
}

// gopdfgo: color to kolor wypełnienia ustawiony przed wywołaniem; syntetyczne pogrubienie
// (backend.TextRun.BoldStroke) obrysowuje glify tym samym kolorem.
func (ctx drawContext) drawFirstLine(textbox *bo.TextBox, textOverflow string, blockEllipsis pr.TaggedString, x, y pr.Fl, color parser.RGBA) {
	// Don’t draw lines with only invisible characters
	if strings.TrimSpace(textbox.TextS()) == "" {
		return
	}

	fontSize := textbox.Style.GetFontSize().Value
	if fontSize < 1e-6 { // Default float precision used by pydyf
		return
	}

	textContext := draw.Context{Output: ctx.dst, Fonts: ctx.fonts}
	text := textContext.CreateFirstLine(textbox.TextLayout, textOverflow, blockEllipsis, 1, x, y, 0)
	if text.HasSyntheticBold() {
		ctx.dst.OnNewStack(func() {
			ctx.dst.State().SetColorRgba(color, true)
			ctx.dst.DrawText([]backend.TextDrawing{text})
		})
		return
	}
	ctx.dst.DrawText([]backend.TextDrawing{text})
}

// Draw text-decoration of “textbox“ to a “context“.
func (ctx drawContext) drawTextDecoration(textbox *bo.TextBox, offsetX, offsetY, thickness pr.Fl, color Color) {
	ctx.drawLine(fl(textbox.PositionX), fl(textbox.PositionY)+offsetY, fl(textbox.PositionX)+fl(textbox.Width.V()), fl(textbox.PositionY)+offsetY,
		thickness, textbox.Style.GetTextDecorationStyle(), [2]parser.RGBA{color}, offsetX)
}
