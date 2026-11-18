package document

import (
	"math"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
)

// gopdfgo: clip-path z kształtem podstawowym (CSS Masking 1 § 5.1, CSS Shapes 1 § 3.1): wszystko, co
// maluje element i jego potomkowie, jest przycinane do kształtu położonego w pudełku odniesienia
// (domyślnie border-box). Źródło tej właściwości nie znało.

// clipPathReference zwraca pudełko odniesienia jako prostokąt.
func clipPathReference(box *bo.BoxFields, name string) (x, y, width, height pr.Float) {
	switch name {
	case "content-box":
		return box.ContentBoxX(), box.ContentBoxY(), box.Width.V(), box.Height.V()
	case "padding-box":
		return box.PaddingBoxX(), box.PaddingBoxY(), box.PaddingWidth(), box.PaddingHeight()
	case "margin-box":
		return box.PositionX, box.PositionY, box.MarginWidth(), box.MarginHeight()
	}
	return box.BorderBoxX(), box.BorderBoxY(), box.BorderWidth(), box.BorderHeight()
}

// applyClipPath przycina bieżący stan graficzny do clip-path pudełka; zwraca false, gdy kształt jest
// pusty i nic nie powinno być malowane.
func (ctx drawContext) applyClipPath(box *bo.BoxFields) bool {
	clip := box.Style.GetClipPath()
	if clip.Shape == "" {
		return true
	}
	x, y, width, height := clipPathReference(box, clip.Box)
	resolve := func(d pr.Dimension, reference pr.Float) pr.Float {
		return pr.ResolvePercentage(d.Tagged(), reference).V()
	}
	dst := ctx.dst
	evenOdd := false
	switch clip.Shape {
	case "box":
		var rounded bo.RoundedBox
		switch clip.Box {
		case "content-box":
			rounded = box.RoundedContentBox()
		case "padding-box":
			rounded = box.RoundedPaddingBox()
		case "margin-box":
			// uproszczenie: promienie jak dla obramowania (CSS Shapes 1 § 4.1 powiększa je o margines)
			rounded = box.RoundedBorderBox()
			rounded.X, rounded.Y, rounded.Width, rounded.Height = x, y, width, height
		default:
			rounded = box.RoundedBorderBox()
		}
		roundedBox(dst, rounded)
	case "inset":
		a := clip.Args
		top, right, bottom, left := resolve(a[0], height), resolve(a[1], width), resolve(a[2], height), resolve(a[3], width)
		// przesunięcia większe niż pudełko zmniejszamy proporcjonalnie (CSS Shapes 1 § 3.1.1)
		if sum := left + right; sum > width && sum > 0 {
			left, right = left*width/sum, right*width/sum
		}
		if sum := top + bottom; sum > height && sum > 0 {
			top, bottom = top*height/sum, bottom*height/sum
		}
		w, h := max(0, width-left-right), max(0, height-top-bottom)
		var radii [4]bo.Point
		for i := range radii {
			radii[i] = bo.Point{resolve(a[4+2*i], width), resolve(a[5+2*i], height)}
		}
		// promienie za duże na prostokąt zmniejszamy jak border-radius (CSS Backgrounds 3 § 5.5)
		ratio := pr.Float(1)
		for _, pair := range [...][3]pr.Float{
			{radii[0][0] + radii[1][0], w}, {radii[3][0] + radii[2][0], w},
			{radii[0][1] + radii[3][1], h}, {radii[1][1] + radii[2][1], h},
		} {
			if pair[0] > 0 && pair[1]/pair[0] < ratio {
				ratio = pair[1] / pair[0]
			}
		}
		for i := range radii {
			radii[i][0] *= ratio
			radii[i][1] *= ratio
		}
		roundedBox(dst, bo.RoundedBox{
			X: x + left, Y: y + top, Width: w, Height: h,
			TopLeft: radii[0], TopRight: radii[1], BottomRight: radii[2], BottomLeft: radii[3],
		})
	case "circle", "ellipse":
		radii := clip.Args[:len(clip.Args)-2]
		cx, cy := resolve(clip.Args[len(clip.Args)-2], width), resolve(clip.Args[len(clip.Args)-1], height)
		if clip.FromRight {
			cx = width - cx
		}
		if clip.FromBottom {
			cy = height - cy
		}
		side := func(d pr.Dimension, center, size pr.Float) (pr.Float, bool) {
			if d.Unit == pr.Scalar && d.Value < 0 {
				near, far := min(abs(center), abs(size-center)), max(abs(center), abs(size-center))
				if d.Value == -1 {
					return near, true
				}
				return far, true
			}
			return 0, false
		}
		var rx, ry pr.Float
		if clip.Shape == "circle" {
			r := radii[0]
			if r.Unit == pr.Scalar && r.Value < 0 {
				nearX, _ := side(r, cx, width)
				nearY, _ := side(r, cy, height)
				if r.Value == -1 {
					rx = min(nearX, nearY)
				} else {
					farX, _ := side(pr.Dimension{Value: -2, Unit: pr.Scalar}, cx, width)
					farY, _ := side(pr.Dimension{Value: -2, Unit: pr.Scalar}, cy, height)
					rx = max(farX, farY)
				}
			} else {
				// procent promienia koła liczy się od przekątnej podzielonej przez √2 (CSS Shapes 1 § 3.1.2)
				diagonal := pr.Float(math.Hypot(float64(width), float64(height)) / math.Sqrt2)
				rx = resolve(r, diagonal)
			}
			ry = rx
		} else {
			var keyword bool
			if rx, keyword = side(radii[0], cx, width); !keyword {
				rx = resolve(radii[0], width)
			}
			if ry, keyword = side(radii[1], cy, height); !keyword {
				ry = resolve(radii[1], height)
			}
		}
		ellipsePath(ctx, x+cx, y+cy, rx, ry)
	case "polygon":
		for i := 0; i+1 < len(clip.Args); i += 2 {
			px, py := pr.Fl(x+resolve(clip.Args[i], width)), pr.Fl(y+resolve(clip.Args[i+1], height))
			if i == 0 {
				dst.MoveTo(px, py)
			} else {
				dst.LineTo(px, py)
			}
		}
		dst.ClosePath()
		evenOdd = clip.EvenOdd
	default:
		return true
	}
	dst.State().Clip(evenOdd)
	return true
}

func abs(v pr.Float) pr.Float {
	if v < 0 {
		return -v
	}
	return v
}

// ellipsePath dodaje do ścieżki elipsę z czterech łuków Béziera.
func ellipsePath(ctx drawContext, cx, cy, rx, ry pr.Float) {
	const k = 0.5522847498
	dst := ctx.dst
	x, y, a, b := pr.Fl(cx), pr.Fl(cy), pr.Fl(max(0, rx)), pr.Fl(max(0, ry))
	dst.MoveTo(x+a, y)
	dst.CubicTo(x+a, y+b*k, x+a*k, y+b, x, y+b)
	dst.CubicTo(x-a*k, y+b, x-a, y+b*k, x-a, y)
	dst.CubicTo(x-a, y-b*k, x-a*k, y-b, x, y-b)
	dst.CubicTo(x+a*k, y-b, x+a, y-b*k, x+a, y)
	dst.ClosePath()
}
