package document

import (
	"strings"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/layout"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	mt "github.com/polestorm/gopdfgo/internal/webrender/matrix"
	"github.com/polestorm/gopdfgo/internal/webrender/svg"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// gopdfgo: odnośniki elementów <a> wewnątrz obrazów SVG (ADR 0029). Plik własny gopdfgo.

// linkAreaImage to obraz z obszarami odnośników (images.SVGImage); obraz sam decyduje, czy jego
// sposób osadzenia jest interaktywny.
type linkAreaImage interface {
	LinkAreas(width, height pr.Fl) []svg.LinkArea
}

// gatherSVGLinks dopisuje do links odnośniki obrazów SVG z poddrzewa box_, w kolejności drzewa pudełek.
// Woła się ją po narysowaniu poddrzewa, bo ramki tekstu SVG pochodzą ze składu przy rysowaniu.
// Przekształcenia CSS składa się jak w gatherAnchors; obszar to prostokąt otaczający.
func gatherSVGLinks(box_ bo.Box, links *[]Link, matrix *mt.Transform) {
	if transform, hasTransform := getMatrix(box_); hasTransform {
		if matrix != nil {
			t := mt.Mul(*matrix, transform)
			matrix = &t
		} else {
			matrix = &transform
		}
	}
	if replaced, ok := box_.(bo.ReplacedBoxITF); ok {
		replacedSVGLinks(replaced, links, matrix)
	}
	for _, child := range box_.AllChildren() {
		gatherSVGLinks(child, links, matrix)
	}
}

// replacedSVGLinks: położenie i rozmiar obrazu jak w drawReplacedbox; obszary są przycinane do pola
// treści elementu (a we fragmencie pokrojonym między strony do części widocznej na tej stronie).
func replacedSVGLinks(box_ bo.ReplacedBoxITF, links *[]Link, matrix *mt.Transform) {
	box := box_.Replaced()
	image, ok := box.Replacement.(linkAreaImage)
	if !ok || box.Style.GetVisibility() != "visible" || !pr.Is(box.Width) || !pr.Is(box.Height) {
		return
	}
	visibleHeight := box.Height
	sliced := box.SliceFullHeight > 0
	if sliced {
		box.Height = box.SliceFullHeight
	}
	drawWidth, drawHeight, drawX, drawY := layout.LayoutReplacedBox(box_)
	box.Height = visibleHeight
	if drawWidth <= 0 || drawHeight <= 0 {
		return
	}
	if sliced {
		drawY -= box.SliceOffset
	}
	areas := image.LinkAreas(pr.Fl(drawWidth), pr.Fl(drawHeight))
	if len(areas) == 0 {
		return
	}
	clipX0, clipY0 := fl(box.ContentBoxX()), fl(box.ContentBoxY())
	clipX1, clipY1 := clipX0+fl(box.Width.V()), clipY0+fl(visibleHeight.V())
	for _, area := range areas {
		x0, y0 := max(clipX0, fl(drawX)+area.X), max(clipY0, fl(drawY)+area.Y)
		x1, y1 := min(clipX1, fl(drawX)+area.X+area.Width), min(clipY1, fl(drawY)+area.Y+area.Height)
		if x1 <= x0 || y1 <= y0 {
			continue
		}
		link := Link{Type: pr.External, Target: area.Href}
		if name, internal := strings.CutPrefix(area.Href, "#"); internal {
			link = Link{Type: pr.Internal, Target: name}
		}
		link.element, link.svg = box.Element, true // gopdfgo: element obrazu dla PDF z tagami (ADR 0033)
		link.svgIndex = area.Index
		if matrix != nil {
			link.Rectangle = rectangleAabb(*matrix, x0, y0, x1-x0, y1-y0)
		} else {
			link.Rectangle = [4]fl{x0, y0, x1, y1}
		}
		*links = append(*links, link)
	}
}

// validLinks zwraca odnośniki bez wewnętrznych prowadzących do nieistniejącej kotwicy (jak resolveLinks).
func validLinks(links []Link, anchors utils.Set) []Link {
	var out []Link
	for _, link := range links {
		if link.Type == pr.Internal && !anchors.Has(link.Target) {
			logger.WarningLogger.Printf("No anchor #%s for internal URI reference\n", link.Target)
			continue
		}
		out = append(out, link)
	}
	return out
}
