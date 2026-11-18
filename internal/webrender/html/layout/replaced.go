package layout

import (
	"fmt"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
)

// Layout for images and other replaced elements.
// https://drafts.csswg.org/csswg/css-images-3/#sizing

// Default sizing algorithm for the concrete object size.
// https://drafts.csswg.org/csswg/css-images-3/#default-sizing
func DefaultImageSizing(intrinsicWidth, intrinsicHeight, intrinsicRatio,
	specifiedWidth, specifiedHeight pr.MaybeFloat, defaultWidth, defaultHeight pr.Float,
) (concreteWidth, concreteHeight pr.Float) {
	if specifiedWidth == pr.AutoF {
		specifiedWidth = nil
	}
	if specifiedHeight == pr.AutoF {
		specifiedHeight = nil
	}

	if specifiedWidth != nil && specifiedHeight != nil {
		return specifiedWidth.V(), specifiedHeight.V()
	} else if specifiedWidth != nil {
		if intrinsicRatio != nil {
			concreteHeight = specifiedWidth.V() / intrinsicRatio.V()
		} else if intrinsicHeight != nil {
			concreteHeight = intrinsicHeight.V()
		} else {
			concreteHeight = defaultHeight
		}
		return specifiedWidth.V(), concreteHeight
	} else if specifiedHeight != nil {
		if intrinsicRatio != nil {
			concreteWidth = specifiedHeight.V() * intrinsicRatio.V()
		} else if intrinsicWidth != nil {
			concreteWidth = intrinsicWidth.V()
		} else {
			concreteWidth = defaultWidth
		}
		return concreteWidth, specifiedHeight.V()
	} else {
		if intrinsicWidth != nil || intrinsicHeight != nil {
			return DefaultImageSizing(intrinsicWidth, intrinsicHeight, intrinsicRatio,
				intrinsicWidth, intrinsicHeight, defaultWidth, defaultHeight)
		} else {
			return containConstraintImageSizing(defaultWidth, defaultHeight, intrinsicRatio)
		}
	}
}

// Cover constraint sizing algorithm for the concrete object size.
// https://drafts.csswg.org/csswg/css-images-3/#contain-constraint
func containConstraintImageSizing(constraintWidth, constraintHeight pr.Float, intrinsicRatio pr.MaybeFloat) (concreteWidth, concreteHeight pr.Float) {
	return constraintImageSizing(constraintWidth, constraintHeight, intrinsicRatio, false)
}

// Cover constraint sizing algorithm for the concrete object size.
// https://drafts.csswg.org/csswg/css-images-3/#cover-constraint
func coverConstraintImageSizing(constraintWidth, constraintHeight pr.Float, intrinsicRatio pr.MaybeFloat) (concreteWidth, concreteHeight pr.Float) {
	return constraintImageSizing(constraintWidth, constraintHeight, intrinsicRatio, true)
}

func constraintImageSizing(constraintWidth, constraintHeight pr.Float, intrinsicRatio pr.MaybeFloat, cover bool) (concreteWidth, concreteHeight pr.Float) {
	if intrinsicRatio == nil {
		return constraintWidth, constraintHeight
	} else if cover != (constraintWidth > constraintHeight*intrinsicRatio.V()) {
		return constraintHeight * intrinsicRatio.V(), constraintHeight
	} else {
		return constraintWidth, constraintWidth / intrinsicRatio.V()
	}
}

// LayoutReplacedBox computes the dimension of the content of a replaced box.
func LayoutReplacedBox(box_ bo.ReplacedBoxITF) (drawWidth, drawHeight, positionX, positionY pr.Float) {
	box := box_.Replaced()
	// TODO: respect box-sizing ?
	objectFit := box.Style.GetObjectFit()
	position := box.Style.GetObjectPosition() // see validation

	image := box.Replacement
	intrinsicWidth, intrinsicHeight, ratio := image.GetIntrinsicSize(box.Style.GetImageResolution().Value, box.Style.GetFontSize().Value)
	if intrinsicWidth == nil || intrinsicHeight == nil {
		intrinsicWidth, intrinsicHeight = containConstraintImageSizing(box.Width.V(), box.Height.V(), ratio)
	}

	if objectFit == "fill" {
		drawWidth, drawHeight = box.Width.V(), box.Height.V()
	} else {
		if objectFit == "contain" || objectFit == "scale-down" {
			drawWidth, drawHeight = containConstraintImageSizing(box.Width.V(), box.Height.V(), ratio)
		} else if objectFit == "cover" {
			drawWidth, drawHeight = coverConstraintImageSizing(box.Width.V(), box.Height.V(), ratio)
		} else if objectFit == "none" {
			drawWidth, drawHeight = intrinsicWidth.V(), intrinsicHeight.V()
		} else {
			panic(fmt.Sprintf("unexpected objectFit %s", objectFit))
		}

		if objectFit == "scale-down" {
			drawWidth = min(drawWidth, intrinsicWidth.V())
			drawHeight = min(drawHeight, intrinsicHeight.V())
		}
	}

	originX, positionX_, originY, positionY_ := position.OriginX, position.Pos[0], position.OriginY, position.Pos[1]
	refX := box.Width.V() - drawWidth
	refY := box.Height.V() - drawHeight

	positionX = pr.ResolvePercentage(positionX_.Tagged(), refX).V()
	positionY = pr.ResolvePercentage(positionY_.Tagged(), refY).V()
	if originX == pr.Right {
		positionX = refX - positionX
	}
	if originY == pr.Bottom {
		positionY = refY - positionY
	}

	positionX += box.ContentBoxX()
	positionY += box.ContentBoxY()

	return drawWidth, drawHeight, positionX, positionY
}

var replacedBoxWidth = handleMinMaxWidth(replacedBoxWidth_)

// @handleMinMaxWidth
// Compute and set the used width for replaced boxes (inline- or block-level)
// containingBlock must be block
func replacedBoxWidth_(box_ Box, _ *layoutContext, containingBlock containingBlock) (bool, pr.Float) {
	box__, ok := box_.(bo.ReplacedBoxITF)
	if !ok {
		panic(fmt.Sprintf("expected ReplacedBox instance, got %s", box_))
	}
	box := box__.Replaced()
	intrinsicWidth, intrinsicHeight, ratio := box.Replacement.GetIntrinsicSize(box.Style.GetImageResolution().Value, box.Style.GetFontSize().Value)
	intrinsicWidth, intrinsicHeight, ratio = applyPreferredRatio(box.Style, intrinsicWidth, intrinsicHeight, ratio)

	// This algorithm simply follows the different points of the specification
	// https://www.w3.org/TR/CSS21/visudet.html#inline-replaced-width
	if box.Height == pr.AutoF && box.Width == pr.AutoF {
		if intrinsicWidth != nil {
			// Point #1
			box.Width = intrinsicWidth
		} else if ratio != nil {
			if intrinsicHeight != nil {
				// Point #2 first part
				box.Width = intrinsicHeight.V() * ratio.V()
			} else {
				// Point #3
				blockLevelWidth(box, nil, containingBlock)
			}
		}
	}
	if box.Width == pr.AutoF {
		if ratio != nil {
			// Point #2 second part
			box.Width = box.Height.V() * ratio.V()
		} else if intrinsicWidth != nil {
			// Point #4
			box.Width = intrinsicWidth
		} else {
			// Point #5
			// It's pretty useless to rely on device size to set width.
			box.Width = pr.Float(300)
		}
	}

	return false, 0
}

var replacedBoxHeight = handleMinMaxHeight(replacedBoxHeight_)

// Compute and set the used height for replaced boxes (inline- or block-level)
// box_ must be a ReplacedBoxITF
func replacedBoxHeight_(box_ Box, _ *layoutContext, _ containingBlock) (bool, pr.Float) {
	box__, ok := box_.(bo.ReplacedBoxITF)
	if !ok {
		panic(fmt.Sprintf("expected ReplacedBox instance, got %s", box_))
	}
	box := box__.Replaced()
	// https://www.w3.org/TR/CSS21/visudet.html#inline-replaced-height
	_, intrinsicHeight, ratio := box.Replacement.GetIntrinsicSize(
		box.Style.GetImageResolution().Value, box.Style.GetFontSize().Value)
	_, intrinsicHeight, ratio = applyPreferredRatio(box.Style, nil, intrinsicHeight, ratio)

	// Test pr.Auto on the computed width, not the used width
	// gopdfgo: bez wysokości własnej (np. obraz, którego nie udało się wczytać) wysokość zostaje auto
	// i rozstrzygają ją reguły niżej; źródło wpisywało nil, co kończyło się paniką w min/max-height
	if box.Height == pr.AutoF && box.Width == pr.AutoF && intrinsicHeight != nil {
		box.Height = intrinsicHeight
	} else if box.Height == pr.AutoF && pr.Is(ratio) {
		box.Height = box.Width.V() / ratio.V()
	}

	if box.Height == pr.AutoF && box.Width == pr.AutoF && intrinsicHeight != nil {
		box.Height = intrinsicHeight
	} else if ratio != nil && box.Height == pr.AutoF {
		box.Height = box.Width.V() / ratio.V()
	} else if box.Height == pr.AutoF && intrinsicHeight != nil {
		box.Height = intrinsicHeight
	} else if box.Height == pr.AutoF {
		// It"s pretty useless to rely on device size to set width.
		box.Height = pr.Float(150)
	}

	return false, 0
}

// applyPreferredRatio (gopdfgo): aspect-ratio na elemencie zastępowanym (CSS Sizing 4 § 5.1). Proporcja
// podana bez auto zastępuje proporcję obrazu; "auto <proporcja>" działa tylko wtedy, gdy obraz własnej
// nie ma. Rozmiar wewnętrzny w jednej osi jest wtedy uzupełniany z drugiej według nowej proporcji.
func applyPreferredRatio(style pr.ElementStyle, width, height, ratio pr.MaybeFloat) (pr.MaybeFloat, pr.MaybeFloat, pr.MaybeFloat) {
	preferred := style.GetAspectRatio()
	if preferred == 0 || (preferred < 0 && pr.Is(ratio)) {
		return width, height, ratio
	}
	if preferred < 0 {
		preferred = -preferred
	}
	if style.GetWidth().Tag != pr.Auto || style.GetHeight().Tag != pr.Auto {
		// rozmiar wewnętrzny nie uczestniczy: brakujący wymiar wynika z podanego i z proporcji
		return nil, nil, preferred
	}
	if pr.Is(width) {
		height = width.V() / preferred
	} else if pr.Is(height) {
		width = height.V() * preferred
	}
	return width, height, preferred
}

// minMaxAutoReplacedBox (gopdfgo): tabela ograniczeń z CSS 2.1 § 10.4 dotyczy tylko elementów
// z proporcją wewnętrzną; bez niej szerokość i wysokość ograniczamy niezależnie (test WPT
// replaced-elements-max-height-20). Źródło stosowało tabelę zawsze, więc np. obraz 300×150 bez
// proporcji zachowywał proporcję 2:1.
func minMaxAutoReplacedBox(box Box) {
	if replaced, ok := box.(bo.ReplacedBoxITF); ok && !hasIntrinsicRatio(replaced.Replaced()) {
		fields := box.Box()
		fields.Width = max(fields.MinWidth.V(), min(fields.Width.V(), fields.MaxWidth.V()))
		fields.Height = max(fields.MinHeight.V(), min(fields.Height.V(), fields.MaxHeight.V()))
		return
	}
	minMaxAutoReplaced(box.Box())
}

// hasIntrinsicRatio (gopdfgo) podaje, czy element zastępowany ma proporcję wewnętrzną (także z aspect-ratio).
func hasIntrinsicRatio(box *bo.ReplacedBox) bool {
	width, height, ratio := box.Replacement.GetIntrinsicSize(box.Style.GetImageResolution().Value, box.Style.GetFontSize().Value)
	_, _, ratio = applyPreferredRatio(box.Style, width, height, ratio)
	return pr.Is(ratio)
}

// Resolve min/max constraints on replaced elements
// that have "auto" width or heights.
func minMaxAutoReplaced(box *bo.BoxFields) {
	width := box.Width.V()
	height := box.Height.V()
	minWidth := box.MinWidth.V()
	minHeight := box.MinHeight.V()
	maxWidth := max(minWidth, box.MaxWidth.V())
	maxHeight := max(minHeight, box.MaxHeight.V())

	// (violationWidth, violationHeight)
	var violationWidth, violationHeight string
	if width < minWidth {
		violationWidth = "min"
	} else if width > maxWidth {
		violationWidth = "max"
	}
	if height < minHeight {
		violationHeight = "min"
	} else if height > maxHeight {
		violationHeight = "max"
	}

	// Work around divisions by zero. These are pathological cases anyway.
	// TODO: is there a cleaner way?
	if width == 0 {
		width = 1e-6
	}
	if height == 0 {
		height = 1e-6
	}

	switch [2]string{violationWidth, violationHeight} {
	// ("", ""): nothing to do
	case [2]string{"max", ""}:
		box.Width = maxWidth
		box.Height = max(maxWidth*height/width, minHeight)
	case [2]string{"min", ""}:
		box.Width = minWidth
		box.Height = min(minWidth*height/width, maxHeight)
	case [2]string{"", "max"}:
		box.Width = max(maxHeight*width/height, minWidth)
		box.Height = maxHeight
	case [2]string{"", "min"}:
		box.Width = min(minHeight*width/height, maxWidth)
		box.Height = minHeight
	case [2]string{"max", "max"}:
		if maxWidth/width <= maxHeight/height {
			box.Width = maxWidth
			box.Height = max(minHeight, maxWidth*height/width)
		} else {
			box.Width = max(minWidth, maxHeight*width/height)
			box.Height = maxHeight
		}
	case [2]string{"min", "min"}:
		if minWidth/width <= minHeight/height {
			box.Width = min(maxWidth, minHeight*width/height)
			box.Height = minHeight
		} else {
			box.Width = minWidth
			box.Height = min(maxHeight, minWidth*height/width)
		}
	case [2]string{"min", "max"}:
		box.Width = minWidth
		box.Height = maxHeight
	case [2]string{"max", "min"}:
		box.Width = maxWidth
		box.Height = minHeight
	}
}

// Lay out an inline :class:`boxes.ReplacedBox` “box“.
func inlineReplacedBoxLayout(box_ Box, containingBlock *bo.BoxFields) {
	resolveMarginAuto(box_.Box())
	inlineReplacedBoxWidthHeight(box_, containingBlock)
}

func inlineReplacedBoxWidthHeight(box Box, containingBlock containingBlock) {
	if style := box.Box().Style; style.GetWidth().Tag == pr.Auto && style.GetHeight().Tag == pr.Auto {
		replacedBoxWidth_(box, nil, containingBlock)
		replacedBoxHeight_(box, nil, nil)
		minMaxAutoReplacedBox(box)
	} else {
		// gopdfgo: przy szerokości automatycznej i podanej wysokości najpierw wysokość z min/max-height,
		// żeby szerokość wynikała z wysokości już ograniczonej (CSS 2 §10.7); w źródle szerokość liczono
		// z wysokości sprzed ograniczenia.
		if style.GetWidth().Tag == pr.Auto {
			replacedBoxHeight(box, nil, nil)
		}
		replacedBoxWidth(box, nil, containingBlock)
		replacedBoxHeight(box, nil, nil)
	}
}

// Lay out the block :class:`boxes.ReplacedBox` “box“.
func blockReplacedBoxLayout(context *layoutContext, box_ bo.ReplacedBoxITF, containingBlock *bo.BoxFields) (bo.ReplacedBoxITF, blockLayout) {
	box_ = box_.Copy().(bo.ReplacedBoxITF) // Copy is type stable
	box := box_.Box()
	if box.Style.GetWidth().Tag == pr.Auto && box.Style.GetHeight().Tag == pr.Auto {
		computedMarginsL, computedMarginsR := box.MarginLeft, box.MarginRight
		blockReplacedWidth_(box_, nil, containingBlock)
		replacedBoxHeight_(box_, nil, nil)
		minMaxAutoReplacedBox(box_)
		box.MarginLeft, box.MarginRight = computedMarginsL, computedMarginsR
		blockLevelWidth_(box_, nil, containingBlock)
	} else {
		if box.Style.GetWidth().Tag == pr.Auto { // jak w inlineReplacedBoxWidthHeight
			replacedBoxHeight(box_, nil, nil)
		}
		blockReplacedWidth(box_, nil, containingBlock)
		replacedBoxHeight(box_, nil, nil)
	}

	// Don't collide with floats
	// https://www.w3.org/TR/CSS21/visuren.html#floats
	// gopdfgo: element flex albo grid stoi tam, gdzie postawił go układ kontenera; omijanie elementów
	// pływających ustawiało go z powrotem przy lewej krawędzi, więc dwa elementy zastępowane obok
	// siebie (np. dwa canvas albo iframe) nakładały się.
	if !box.IsFlexItem && !box.IsGridItem {
		box.PositionX, box.PositionY, _ = avoidCollisions(context, box_, containingBlock, false)
	}
	nextPage := tree.PageBreak{Break: "any"}

	if traceMode {
		traceLogger.DumpTree(box_, "blockReplacedBoxLayout-end")
	}

	return box_, blockLayout{resumeAt: nil, nextPage: nextPage, adjoiningMargins: nil, collapsingThrough: false}
}

// sliceTallReplaced kroi między strony element zastępowany w przepływie, który jest wyższy niż obszar
// strony: na tej stronie zostaje część do dolnej krawędzi, reszta przechodzi na następne. Element, który
// mieści się na stronie, zostaje nietknięty (zostanie przeniesiony w całości, ADR 0004).
//
// gopdfgo: w źródle taki obraz wystawał poza stronę, a jego dalsza część i wysokość ginęły.
func sliceTallReplaced(context *layoutContext, box_ bo.ReplacedBoxITF, layout blockLayout, bottomSpace pr.Float,
	skipStack tree.ResumeStack,
) blockLayout {
	box := box_.Box()
	if bottomSpace == -pr.Inf || context.pageBottom == pr.Inf || context.inColumn || context.clipDepth > 0 || !box.IsInNormalFlow() ||
		box.IsFlexItem || box.IsGridItem || box.Height == pr.AutoF {
		return layout
	}
	// break-inside: avoid na obrazie wyłącza krojenie: obraz zostaje w całości i wystaje poza stronę
	if avoidPageBreak(string(box.Style.GetBreakInside()), context) {
		return layout
	}
	fullHeight := box.Height.V()
	if fullHeight >= maxSliceableHeight {
		return layout
	}
	var consumed pr.Float
	if remaining, isRemainder := skipStack.Remainder(); isRemainder {
		consumed = max(fullHeight-pr.Float(remaining), 0)
	} else if box.BorderHeight() <= context.pageHeight {
		return layout
	}
	if consumed > 0 {
		box_.RemoveDecoration(box, true, false)
	}
	available := context.pageBottom - bottomSpace - box.ContentBoxY()
	remaining := fullHeight - consumed
	if available <= 0 || (consumed == 0 && remaining <= available) {
		return layout
	}
	visible := min(remaining, available)
	box.SliceFullHeight = fullHeight
	box.SliceOffset = consumed
	box.Height = visible
	if left := remaining - visible; left > 0.5 {
		box_.RemoveDecoration(box, false, true)
		layout.resumeAt = tree.RemainderResume(float64(left))
	}
	return layout
}
