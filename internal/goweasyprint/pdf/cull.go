package pdf

import (
	"math"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	"github.com/polestorm/gopdfgo/internal/webrender/matrix"
)

// gopdfgo: pomijanie tekstu leżącego w całości poza widocznym wycinkiem (ADR 0041). Plik własny gopdfgo.

var _ backend.TextCuller = (*group)(nil)

// TextCullingDisabled wyłącza pomijanie tekstu; służy testom, które porównują wygląd z rysowaniem
// całego tekstu.
var TextCullingDisabled bool

// textCull to stan pomijania tekstu, zapamiętywany i przywracany razem ze stanem graficznym (OnNewStack).
type textCull struct {
	on   bool
	rect [4]fl // x0, y0, x1, y1 w układzie bazowym grupy (przed wszystkimi cm)
	// lineWidth: ostatnia szerokość linii (obrys tekstu poszerza obrys glifów o połowę; liczymy całą)
	lineWidth fl
}

// CullTextOutside (backend.TextCuller) zawęża obszar, poza którym tekst nie jest rysowany, do prostokąta
// (x, y, width, height) w bieżącym układzie współrzędnych. Prostokąt jest zapisywany jako prostokąt
// otaczający w układzie bazowym grupy, więc przy obrocie bywa większy: pomijanie jest ostrożne.
func (g *group) CullTextOutside(x, y, width, height fl) {
	if TextCullingDisabled {
		return
	}
	rect := aabb(g.GetTransform(), x, y, x+width, y+height)
	if g.cull.on {
		rect = [4]fl{max(rect[0], g.cull.rect[0]), max(rect[1], g.cull.rect[1]), min(rect[2], g.cull.rect[2]), min(rect[3], g.cull.rect[3])}
	}
	g.cull.on, g.cull.rect = true, rect
}

// aabb zwraca prostokąt otaczający obraz prostokąta (x0, y0)–(x1, y1) przez m.
func aabb(m matrix.Transform, x0, y0, x1, y1 fl) [4]fl {
	out := [4]fl{fl(math.Inf(1)), fl(math.Inf(1)), fl(math.Inf(-1)), fl(math.Inf(-1))}
	for _, p := range [4][2]fl{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}} {
		px, py := m.Apply(p[0], p[1])
		out[0], out[1] = min(out[0], px), min(out[1], py)
		out[2], out[3] = max(out[2], px), max(out[3], py)
	}
	return out
}

// cullMargin to zapas w układzie bazowym grupy (na stronie: punkty PDF, 1/72 cala).
const cullMargin = 1

// culledTexts zwraca teksty, których obrys przecina obszar pomijania (wszystkie, gdy pomijanie jest
// wyłączone). Obrys to prostokąt otaczający obrysy glifów (z tabeli Extents fontu; dla glifu bez wpisu
// i w poziomie: prostokąt fontu), poszerzony o pochylenie, pogrubienie i obrys, a po przekształceniu
// o cullMargin: piksel na krawędzi przycięcia bywa częściowo pokryty także przez treść tuż za nią
// (wygładzanie krawędzi przycięcia mnoży pokrycia), więc tekst bliżej krawędzi zostaje.
func (g *group) culledTexts(texts []backend.TextDrawing) []backend.TextDrawing {
	if !g.cull.on {
		return texts
	}
	ctm := g.GetTransform()
	var kept []backend.TextDrawing
	for _, text := range texts {
		x0, y0, x1, y1, ok := g.textBounds(text)
		var box [4]fl
		if ok {
			m := matrix.Mul(ctm, text.Matrix())
			box = aabb(m, x0, y0, x1, y1)
		}
		r := g.cull.rect
		if !ok || (box[2]+cullMargin > r[0] && box[0]-cullMargin < r[2] && box[3]+cullMargin > r[1] && box[1]-cullMargin < r[3]) {
			kept = append(kept, text)
		} else {
			g.culled++
		}
	}
	return kept
}

// textBounds zwraca obrys tekstu w przestrzeni tekstu (przed macierzą text.Matrix(), oś y w górę),
// liczony tak, jak DrawText przesuwa pióro; ok = false, gdy obrysu nie da się ustalić (tekst zostaje).
func (g *group) textBounds(text backend.TextDrawing) (x0, y0, x1, y1 fl, ok bool) {
	x0, y0 = fl(math.Inf(1)), fl(math.Inf(1))
	x1, y1 = fl(math.Inf(-1)), fl(math.Inf(-1))
	var pen fl
	for _, run := range text.Runs {
		pf, has := g.fonts[run.Font]
		if !has || pf.FontChars == nil {
			return 0, 0, 0, 0, false
		}
		size := text.FontSize
		if run.SizeScale != 0 {
			size *= run.SizeScale
		}
		bbox := pf.Bbox
		margin := max(g.cull.lineWidth, run.BoldStroke)
		skew := fl(math.Abs(float64(run.Skew)))
		for _, glyph := range run.Glyphs {
			extents, has := pf.Extents[glyph.Glyph]
			width := fl(1000)
			top, bottom := fl(bbox[3]), fl(bbox[1])
			if has {
				width = fl(extents.Width)
				top, bottom = fl(extents.Y), fl(extents.Y+extents.Height)
				if bottom > top {
					top, bottom = bottom, top
				}
			}
			origin := pen + glyph.Offset*size/1000
			rise := glyph.Rise
			gy0, gy1 := (bottom+rise)*size/1000, (top+rise)*size/1000
			// pochylenie przesuwa glif w poziomie o skew·y
			slant := skew * max(fl(math.Abs(float64(gy0))), fl(math.Abs(float64(gy1))))
			gx0 := origin + min(fl(bbox[0]), 0)*size/1000 - slant
			gx1 := origin + max(fl(bbox[2]), width)*size/1000 + slant
			x0, x1 = min(x0, gx0-margin), max(x1, gx1+margin)
			y0, y1 = min(y0, gy0-margin), max(y1, gy1+margin)
			pen += (glyph.Offset + width - fl(glyph.Kerning)) * size / 1000
		}
	}
	if x0 > x1 || y0 > y1 {
		return 0, 0, 0, 0, false
	}
	for _, v := range [4]fl{x0, y0, x1, y1} {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return 0, 0, 0, 0, false
		}
	}
	return x0, y0, x1, y1, true
}
