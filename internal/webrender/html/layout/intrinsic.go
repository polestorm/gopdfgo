package layout

import (
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
)

// gopdfgo: rozmiary wewnętrzne podane słowami kluczowymi (CSS Sizing 3 § 2.1). W źródle słowa
// min-content, max-content i fit-content nie były rozpoznawane w width, min-width ani max-width.

// resolveSizes wylicza wartości używane pudełka: procenty względem bloku zawierającego, a potem
// szerokości podane słowem kluczowym rozmiaru wewnętrznego.
func resolveSizes(context *layoutContext, box Box, containingBlock bo.MaybePoint) {
	resolvePercentages(box, containingBlock)
	resolveIntrinsicSizes(context, box, containingBlock[0])
}

func resolveSizesBox(context *layoutContext, box Box, containingBlock containingBlock) {
	w, h := containingBlock.ContainingBlock()
	resolveSizes(context, box, bo.MaybePoint{w, h})
}

// resolveIntrinsicSizes podstawia szerokość treści za width, min-width i max-width podane jako
// min-content, max-content albo fit-content. Wysokości o tych wartościach zachowują się jak auto
// (wysokość treści), co robi już resolvePercentages.
func resolveIntrinsicSizes(context *layoutContext, box_ Box, available pr.MaybeFloat) {
	box := box_.Box()
	width := box.Style.GetWidth().Intrinsic
	minWidth := box.Style.GetMinWidth().Intrinsic
	maxWidth := box.Style.GetMaxWidth().Intrinsic
	if width == pr.IntrinsicNone && minWidth == pr.IntrinsicNone && maxWidth == pr.IntrinsicNone {
		return
	}
	if context == nil || bo.TableT.IsInstance(box_) || bo.InlineT.IsInstance(box_) {
		return
	}

	// Treść mierzymy na kopii bez własnych ograniczeń szerokości: min-width: max-content obowiązuje
	// niezależnie od podanego width.
	measured := box_.Copy()
	if bo.ParentT.IsInstance(box_) {
		measured = bo.CopyWithChildren(box_, box.Children)
	}
	measured.Box().Style = box.Style.Copy()
	measured.Box().Style.SetWidth(pr.TagToV(pr.Auto))
	measured.Box().Style.SetMinWidth(pr.ZeroPixels.Tagged())
	measured.Box().Style.SetMaxWidth(pr.Dimension{Value: pr.Inf, Unit: pr.Px}.Tagged())
	// gopdfgo: jak w shrinkToFit: pudełko o znanej wysokości daje dzieciom określoną bazę wysokości
	// procentowych już przy liczeniu szerokości treści (CSS Sizing 3 § 5.2.1, test WPT percentage-heights-022)
	if height, ok := box.Height.(pr.Float); ok && bo.BlockContainerT.IsInstance(box_) && !box.IsTableWrapper {
		setChildrenDefiniteHeight(box.Children, height)
	}

	size := func(kind pr.IntrinsicSize) pr.Float {
		switch kind {
		case pr.IntrinsicMinContent:
			return minContentWidth(context, measured, false)
		case pr.IntrinsicMaxContent:
			return maxContentWidth(context, measured, false)
		}
		// fit-content: min(max-content, max(min-content, dostępne miejsce))
		availableContent := pr.Inf
		if available != nil && available != pr.AutoF {
			availableContent = available.V() - definiteOrZero(box.MarginLeft) - definiteOrZero(box.MarginRight) -
				definiteOrZero(box.PaddingLeft) - definiteOrZero(box.PaddingRight) -
				box.BorderLeftWidth.V() - box.BorderRightWidth.V()
		}
		return shrinkToFit(context, measured, availableContent)
	}

	if width != pr.IntrinsicNone {
		box.Width = size(width)
	}
	if minWidth != pr.IntrinsicNone {
		box.MinWidth = size(minWidth)
	}
	if maxWidth != pr.IntrinsicNone {
		box.MaxWidth = size(maxWidth)
	}
}

func definiteOrZero(value pr.MaybeFloat) pr.Float {
	if value == nil || value == pr.AutoF {
		return 0
	}
	return value.V()
}
