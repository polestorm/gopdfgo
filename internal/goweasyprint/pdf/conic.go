package pdf

import (
	"math"
	"strconv"
	"strings"

	cs "github.com/polestorm/gopdfgo/internal/pdflib/contentstream"
	"github.com/polestorm/gopdfgo/internal/pdflib/model"
	"github.com/polestorm/gopdfgo/internal/webrender/backend"
)

// drawConicGradient (gopdfgo) rysuje gradient stożkowy (CSS Images 4 § 3.3). PDF nie ma takiego
// cieniowania, więc używamy cieniowania funkcyjnego (typ 1, ISO 32000-2 § 8.7.4.5.2) z funkcją
// kalkulatora PostScript (typ 4, § 7.10.5): funkcja liczy kąt punktu względem środka (operator atan)
// i barwę z przystanków odcinkami liniowo. Krycie przystanków trafia do maski miękkiej zbudowanej tak
// samo w skali szarości.
func (g *group) drawConicGradient(layout backend.GradientLayout, width, height fl) {
	needAlpha := false
	for _, c := range layout.Colors {
		needAlpha = needAlpha || c.A != 1
	}
	colors := make([][]fl, len(layout.Colors))
	alphas := make([][]fl, len(layout.Colors))
	for i, c := range layout.Colors {
		colors[i] = []fl{c.R, c.G, c.B}
		alphas[i] = []fl{c.A}
	}
	// barwa przystanku przezroczystego nie przenika do sąsiednich odcinków (interpolacja
	// z kryciem przemnożonym, CSS Images 4 § 3.5.1), jak w gradientach liniowych
	for i, c := range layout.Colors {
		if c.A != 0 {
			continue
		}
		switch {
		case i > 0 && layout.Colors[i-1].A != 0:
			colors[i] = colors[i-1]
		case i+1 < len(layout.Colors) && layout.Colors[i+1].A != 0:
			colors[i] = colors[i+1]
		}
	}

	domain := [4]fl{0, max(width, 1e-3), 0, max(height, 1e-3)}
	color := conicShading(layout, colors, domain, model.ColorSpaceRGB)
	if needAlpha {
		alphaStream := cs.NewGraphicStream(model.Rectangle{Llx: 0, Lly: 0, Urx: width, Ury: height})
		shName := alphaStream.AddShading(conicShading(layout, alphas, domain, model.ColorSpaceGray))
		alphaStream.Ops(cs.OpShFill{Shading: shName})
		g.drawMask(&alphaStream)
	}
	g.stream.Shading(color)
}

// conicShading buduje cieniowanie funkcyjne dla składowych components (po jednej liście na przystanek).
func conicShading(layout backend.GradientLayout, components [][]fl, domain [4]fl, space model.ColorSpaceName) *model.ShadingDict {
	n := len(components[0])
	ranges := make([]model.Range, n)
	for i := range ranges {
		ranges[i] = model.Range{0, 1}
	}
	code := conicFunctionCode(layout, components)
	return &model.ShadingDict{
		ColorSpace: space,
		ShadingType: model.ShadingFunctionBased{
			Domain: domain,
			Function: []model.FunctionDict{{
				FunctionType: model.FunctionPostScriptCalculator{Content: []byte(code)},
				Domain:       []model.Range{{domain[0], domain[1]}, {domain[2], domain[3]}},
				Range:        ranges,
			}},
		},
	}
}

func psNumber(v float64) string {
	if math.Abs(v) < 1e-9 {
		return "0"
	}
	return strconv.FormatFloat(v, 'f', 6, 64)
}

// conicFunctionCode zwraca program kalkulatora PostScript: (x y) -> składowe barwy.
func conicFunctionCode(layout backend.GradientLayout, components [][]fl) string {
	cx, cy := float64(layout.Coords[0]), float64(layout.Coords[1])
	fromTurn := float64(layout.Coords[2]) / (2 * math.Pi)
	positions := make([]float64, len(layout.Positions))
	for i, p := range layout.Positions {
		positions[i] = float64(p)
	}
	var b strings.Builder
	b.WriteString("{ ")
	// dx = x - cx, dy = y - cy; kąt mierzony od góry zgodnie z ruchem wskazówek zegara (oś y w dół)
	// to atan2(dx, -dy); atan z PostScript daje stopnie z przedziału [0, 360)
	b.WriteString(psNumber(cy) + " sub exch " + psNumber(cx) + " sub exch neg ")
	b.WriteString("2 copy abs exch abs add 0.000001 lt { pop pop 0 } { atan } ifelse ")
	// t: ułamek obrotu od kąta początkowego, w [0, 1)
	b.WriteString("360 div " + psNumber(fromTurn) + " sub dup floor sub ")
	first, last := positions[0], positions[len(positions)-1]
	if layout.Reapeating {
		period := last - first
		b.WriteString(psNumber(first) + " sub " + psNumber(period) + " div dup floor sub " +
			psNumber(period) + " mul " + psNumber(first) + " add ")
	}
	writeConicSegments(&b, positions, components, 0)
	b.WriteString("}")
	return b.String()
}

// writeConicSegments wypisuje zagnieżdżone ifelse wybierające odcinek przystanków dla t na stosie.
func writeConicSegments(b *strings.Builder, positions []float64, components [][]fl, i int) {
	pushConstant := func(c []fl) {
		b.WriteString("pop ")
		for _, v := range c {
			b.WriteString(psNumber(float64(v)) + " ")
		}
	}
	if i == 0 {
		b.WriteString("dup " + psNumber(positions[0]) + " le { ")
		pushConstant(components[0])
		b.WriteString("} { ")
		writeConicSegments(b, positions, components, 1)
		b.WriteString("} ifelse ")
		return
	}
	if i >= len(positions) {
		pushConstant(components[len(components)-1])
		return
	}
	b.WriteString("dup " + psNumber(positions[i]) + " le { ")
	length := positions[i] - positions[i-1]
	if length <= 1e-9 {
		pushConstant(components[i])
	} else {
		// u = (t - p[i-1]) / długość; składowa = c0 + u * (c1 - c0)
		b.WriteString(psNumber(positions[i-1]) + " sub " + psNumber(length) + " div ")
		c0, c1 := components[i-1], components[i]
		for k := range c0 {
			if k < len(c0)-1 {
				b.WriteString("dup ")
			}
			b.WriteString(psNumber(float64(c1[k]-c0[k])) + " mul " + psNumber(float64(c0[k])) + " add ")
			if k < len(c0)-1 {
				b.WriteString("exch ")
			}
		}
	}
	b.WriteString("} { ")
	writeConicSegments(b, positions, components, i+1)
	b.WriteString("} ifelse ")
}
