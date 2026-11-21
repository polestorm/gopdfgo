package svg

import (
	"math"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	pa "github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/css/validation"
	"github.com/polestorm/gopdfgo/internal/webrender/matrix"
)

// text-shadow tekstu SVG (gopdfgo; CSS Text Decoration 3 § 4, stosowana do tekstu SVG jak
// w przeglądarkach; testy WPT svg/painting/reftests/text-shadow-*). Źródło tę właściwość pomijało.

// textShadow to jeden cień tekstu w jednostkach użytkownika.
type textShadow struct {
	dx, dy, blur Fl
	color        pa.RGBA
}

// parseTextShadows odczytuje listę cieni z wartości text-shadow; currentColor (także brak koloru)
// to kolor elementu.
func parseTextShadows(value, currentColor string, fontSize Fl) []textShadow {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "none") {
		return nil
	}
	declared, err := validation.ValidateKnown(pr.PTextShadow, pa.Tokenize([]byte(value), true), "")
	if err != nil {
		return nil
	}
	shadows, ok := declared.(pr.BoxShadows)
	if !ok {
		return nil
	}
	current := pa.ParseColorString(currentColor)
	if current.IsNone() {
		current = pa.ParseColorString("black")
	}
	length := func(d pr.Dimension) Fl {
		switch d.Unit {
		case pr.Em:
			return Fl(d.Value) * fontSize
		case pr.Pt:
			return Fl(d.Value) * 4 / 3
		}
		return Fl(d.Value)
	}
	out := make([]textShadow, 0, len(shadows))
	for _, s := range shadows {
		c := s.Color.ResolveCurrentColor(pr.Color(current)).RGBA
		out = append(out, textShadow{dx: length(s.X), dy: length(s.Y), blur: length(s.Blur), color: c})
	}
	return out
}

// drawTextShadows rysuje cienie pod tekstem: ostatni z listy najniżej. Jak w HTML (document/draw_text.go)
// cień rozmyty przybliżamy kopiami tekstu na dwóch okręgach wokół przesunięcia, każda o szóstej części
// krycia, a cień ostry to jedna kopia. Kopie to wypełnione glify i obrys tekstu w kolorze cienia.
func drawTextShadows(dst backend.Canvas, shadows []textShadow, texts []backend.TextDrawing, doStroke bool) {
	for i := len(shadows) - 1; i >= 0; i-- {
		s := shadows[i]
		if s.color.A == 0 {
			continue
		}
		draw := func(dx, dy Fl, c pa.RGBA) {
			dst.OnNewStack(func() {
				dst.State().Transform(matrix.Translation(dx, dy))
				dst.State().SetColorRgba(c, false)
				dst.State().SetColorRgba(c, true)
				// cień ma kształt glifów (wypełnionych) razem z obrysem, także dla tekstu bez wypełnienia:
				// tak wzorcem testów WPT text-shadow-02/03 jest cień tekstu HTML
				dst.State().SetTextPaint(newPaintOp(true, doStroke, false))
				dst.DrawText(texts)
			})
		}
		if s.blur <= 0 {
			draw(s.dx, s.dy, s.color)
			continue
		}
		const copies = 8
		faint := s.color
		faint.A = s.color.A / 6
		draw(s.dx, s.dy, faint)
		for _, radius := range [2]Fl{s.blur * 0.25, s.blur * 0.5} {
			for k := 0; k < copies; k++ {
				angle := 2 * math.Pi * float64(k) / copies
				draw(s.dx+radius*Fl(math.Cos(angle)), s.dy+radius*Fl(math.Sin(angle)), faint)
			}
		}
	}
}
