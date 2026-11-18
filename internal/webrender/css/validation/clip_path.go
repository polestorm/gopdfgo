package validation

import (
	pa "github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// gopdfgo: “background-blend-mode“ (CSS Compositing 1 § 3.2): lista trybów mieszania warstw tła po
// przecinku. Źródło tej właściwości nie znało.
func backgroundBlendMode(tokens []Token, _ string) pr.CssProperty {
	var out pr.Strings
	for _, part := range pa.SplitOnComma(tokens) {
		keyword := getSingleKeyword(pa.RemoveWhitespace(part))
		switch keyword {
		case "normal", "multiply", "screen", "overlay", "darken", "lighten", "color-dodge", "color-burn",
			"hard-light", "soft-light", "difference", "exclusion", "hue", "saturation", "color", "luminosity":
			out = append(out, keyword)
		default:
			return nil
		}
	}
	return out
}

// gopdfgo: “clip-path“ (CSS Masking 1 § 5.1): none, kształt podstawowy (CSS Shapes 1 § 3.1: inset(),
// circle(), ellipse(), polygon()) z opcjonalnym pudełkiem odniesienia albo samo pudełko odniesienia.
// Odwołania url() do elementu <clipPath> i funkcje path(), rect(), xywh() nie są obsługiwane, więc
// deklaracja z nimi jest odrzucana. Źródło tej właściwości nie znało.
func clipPath(tokens []Token, _ string) pr.CssProperty {
	tokens = pa.RemoveWhitespace(tokens)
	if getSingleKeyword(tokens) == "none" {
		return pr.ClipPath{}
	}
	var out pr.ClipPath
	hasShape := false
	for _, token := range tokens {
		switch keyword := getKeyword(token); keyword {
		case "border-box", "padding-box", "content-box", "margin-box", "fill-box", "stroke-box", "view-box":
			if out.Box != "" {
				return nil
			}
			// pudełka SVG dla elementów HTML oznaczają border-box (CSS Masking 1 § 5.1)
			if keyword == "fill-box" || keyword == "stroke-box" || keyword == "view-box" {
				keyword = "border-box"
			}
			out.Box = keyword
			continue
		}
		fn, ok := token.(pa.FunctionBlock)
		if !ok || hasShape {
			return nil
		}
		shape, ok := basicShape(fn)
		if !ok {
			return nil
		}
		shape.Box = out.Box
		out, hasShape = shape, true
	}
	if !hasShape {
		if out.Box == "" {
			return nil
		}
		// samo pudełko odniesienia: kształtem jest to pudełko z zaokrągleniem z border-radius
		out.Shape = "box"
	}
	return out
}

func basicShape(fn pa.FunctionBlock) (pr.ClipPath, bool) {
	args := pa.RemoveWhitespace(fn.Arguments)
	switch name := utils.AsciiLower(fn.Name); name {
	case "inset":
		return insetShape(args)
	case "circle", "ellipse":
		return ellipseShape(name, args)
	case "polygon":
		return polygonShape(args)
	}
	return pr.ClipPath{}, false
}

// insetShape: inset( <length-percentage>{1,4} [ round <border-radius> ]? ). Args: cztery przesunięcia,
// potem osiem promieni (poziomy, pionowy) narożników od lewego górnego zgodnie z ruchem wskazówek zegara.
func insetShape(args []Token) (pr.ClipPath, bool) {
	offsets, radii := args, []Token(nil)
	for i, token := range args {
		if getKeyword(token) == "round" {
			offsets, radii = args[:i], args[i+1:]
			if len(radii) == 0 {
				return pr.ClipPath{}, false
			}
			break
		}
	}
	if len(offsets) < 1 || len(offsets) > 4 {
		return pr.ClipPath{}, false
	}
	var sides []pr.Dimension
	for _, token := range offsets {
		length := getLength(token, true, true)
		if length.IsNone() {
			return pr.ClipPath{}, false
		}
		sides = append(sides, length)
	}
	out := pr.ClipPath{Shape: "inset", Args: expandFour(sides)}
	corners := make([]pr.Dimension, 8)
	for i := range corners {
		corners[i] = pr.NewDim(0, pr.Px)
	}
	if radii != nil {
		var horizontal, vertical []pr.Dimension
		slash := false
		for _, token := range radii {
			if pa.IsLiteral(token, "/") {
				if slash {
					return pr.ClipPath{}, false
				}
				slash = true
				continue
			}
			length := getLength(token, false, true)
			if length.IsNone() {
				return pr.ClipPath{}, false
			}
			if slash {
				vertical = append(vertical, length)
			} else {
				horizontal = append(horizontal, length)
			}
		}
		if len(horizontal) < 1 || len(horizontal) > 4 || len(vertical) > 4 || (slash && len(vertical) == 0) {
			return pr.ClipPath{}, false
		}
		if len(vertical) == 0 {
			vertical = horizontal
		}
		h, v := expandFour(horizontal), expandFour(vertical)
		for i := 0; i < 4; i++ {
			corners[2*i], corners[2*i+1] = h[i], v[i]
		}
	}
	out.Args = append(out.Args, corners...)
	return out, true
}

// expandFour rozwija 1–4 wartości na cztery strony jak margin.
func expandFour(values []pr.Dimension) []pr.Dimension {
	switch len(values) {
	case 1:
		return []pr.Dimension{values[0], values[0], values[0], values[0]}
	case 2:
		return []pr.Dimension{values[0], values[1], values[0], values[1]}
	case 3:
		return []pr.Dimension{values[0], values[1], values[2], values[1]}
	}
	return values[:4]
}

// ellipseShape: circle( <shape-radius>? [ at <position> ]? ) i
// ellipse( [ <shape-radius>{2} ]? [ at <position> ]? ).
func ellipseShape(name string, args []Token) (pr.ClipPath, bool) {
	radiusTokens, positionTokens := args, []Token(nil)
	for i, token := range args {
		if getKeyword(token) == "at" {
			radiusTokens, positionTokens = args[:i], args[i+1:]
			if len(positionTokens) == 0 {
				return pr.ClipPath{}, false
			}
			break
		}
	}
	count := 1
	if name == "ellipse" {
		count = 2
	}
	var radii []pr.Dimension
	for _, token := range radiusTokens {
		switch getKeyword(token) {
		case "closest-side":
			radii = append(radii, pr.NewDim(-1, pr.Scalar))
		case "farthest-side":
			radii = append(radii, pr.NewDim(-2, pr.Scalar))
		default:
			length := getLength(token, false, true)
			if length.IsNone() {
				return pr.ClipPath{}, false
			}
			radii = append(radii, length)
		}
	}
	if len(radii) == 0 {
		for range count {
			radii = append(radii, pr.NewDim(-1, pr.Scalar))
		}
	}
	if len(radii) != count {
		return pr.ClipPath{}, false
	}
	out := pr.ClipPath{Shape: name, Args: radii}
	center := pr.CenterPos{OriginX: pr.Left, OriginY: pr.Top, Pos: pr.Point{fiftyPercent, fiftyPercent}}
	if positionTokens != nil {
		center = parsePosition(positionTokens)
		if center.OriginX == 0 {
			return pr.ClipPath{}, false
		}
	}
	out.FromRight, out.FromBottom = center.OriginX == pr.Right, center.OriginY == pr.Bottom
	out.Args = append(out.Args, center.Pos[0], center.Pos[1])
	return out, true
}

// polygonShape: polygon( <fill-rule>? , [ <length-percentage> <length-percentage> ]# )
func polygonShape(args []Token) (pr.ClipPath, bool) {
	out := pr.ClipPath{Shape: "polygon"}
	parts := pa.SplitOnComma(args)
	if len(parts) > 0 {
		if first := pa.RemoveWhitespace(parts[0]); len(first) == 1 {
			switch getKeyword(first[0]) {
			case "evenodd":
				out.EvenOdd, parts = true, parts[1:]
			case "nonzero":
				parts = parts[1:]
			}
		}
	}
	if len(parts) == 0 {
		return pr.ClipPath{}, false
	}
	for _, part := range parts {
		part = pa.RemoveWhitespace(part)
		if len(part) != 2 {
			return pr.ClipPath{}, false
		}
		for _, token := range part {
			length := getLength(token, true, true)
			if length.IsNone() {
				return pr.ClipPath{}, false
			}
			out.Args = append(out.Args, length)
		}
	}
	return out, true
}
