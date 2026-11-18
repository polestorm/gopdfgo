package layout

import (
	"fmt"
	"math"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/utils/testutils/tracer"

	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
)

// ---------------------- Absolutely positioned boxes management. ----------------

type AliasBox = bo.Box

// AbsolutePlaceholder is left where an absolutely-positioned box was taken out of the flow.
type AbsolutePlaceholder struct {
	AliasBox
	layoutDone bool
}

func NewAbsolutePlaceholder(box Box) *AbsolutePlaceholder {
	out := AbsolutePlaceholder{AliasBox: box}
	return &out
}

func (AbsolutePlaceholder) Type() bo.BoxType { return bo.AbsolutePlaceholderT }

func (abs *AbsolutePlaceholder) setLaidOutBox(newBox Box) {
	abs.AliasBox = newBox
	abs.layoutDone = true
}

func (abs *AbsolutePlaceholder) Translate(dx, dy pr.Float, ignoreFloats bool) {
	if dx == 0 && dy == 0 {
		return
	}
	if abs.layoutDone {
		abs.AliasBox.Translate(dx, dy, ignoreFloats)
	} else {
		// Descendants do not have a position yet.
		abs.AliasBox.Box().PositionX += dx
		abs.AliasBox.Box().PositionY += dy
		// gopdfgo: prostokąt pozycji statycznej (dziecko kontenera flex albo siatki) przesuwa się
		// razem z pudełkiem, np. gdy kontener jest elementem pływającym przesuwanym po układzie
		if static := abs.AliasBox.Box().StaticAlign; static != nil {
			moved := *static
			moved[0] += dx
			moved[1] += dy
			abs.AliasBox.Box().StaticAlign = &moved
		}
	}
}

func (abs AbsolutePlaceholder) Copy() Box {
	out := abs
	out.AliasBox = abs.AliasBox.Copy()
	return &out
}

func (abs AbsolutePlaceholder) String() string {
	return fmt.Sprintf("<Placeholder %s (%s)>", abs.AliasBox.Type(), abs.AliasBox.Box().ElementTag())
}

var absoluteWidth = handleMinMaxWidth(_absoluteWidth)

// @handleMinMaxWidth
// containingBlock must be block
func _absoluteWidth(box_ Box, context *layoutContext, containingBlock containingBlock) (bool, pr.Float) {
	// https://www.w3.org/TR/CSS2/visudet.html#abs-replaced-width
	box := box_.Box()

	ltr := box.Style.ParentStyle() == nil || box.Style.ParentStyle().GetDirection() == pr.Ltr
	paddingsBorders := box.PaddingLeft.V() + box.PaddingRight.V() + box.BorderLeftWidth.V() + box.BorderRightWidth.V()

	marginL := box.MarginLeft
	marginR := box.MarginRight
	width := box.Width
	left := box.Left
	right := box.Right

	cb_ := containingBlock.(block)
	cbX, cbWidth := cb_.X, cb_.Width

	var translateX pr.Float = 0
	translateBoxWidth := false
	defaultTranslateX := cbX - box.PositionX
	if left == pr.AutoF && right == pr.AutoF && width == pr.AutoF {
		if marginL == pr.AutoF {
			box.MarginLeft = pr.Float(0)
		}
		if marginR == pr.AutoF {
			box.MarginRight = pr.Float(0)
		}
		availableWidth := cbWidth - (paddingsBorders + box.MarginLeft.V() + box.MarginRight.V())
		fitWidth := availableWidth
		if ltr && !box.IsOutsideMarker {
			// gopdfgo: `left` przyjmuje pozycję statyczną, więc na szerokość zostaje miejsce od niej do
			// końca pudełka zawierającego (CSS 2.1 § 10.3.7, reguła 3); źródło dawało całą szerokość
			fitWidth += defaultTranslateX
		}
		box.Width = shrinkToFit(context, box_, fitWidth)
		if box.IsOutsideMarker {
			translateBoxWidth = ltr
		} else if !ltr {
			translateBoxWidth = true
			translateX = defaultTranslateX + availableWidth
		}
	} else if left != pr.AutoF && right != pr.AutoF && width != pr.AutoF {
		widthForMargins := cbWidth - (right.V() + left.V() + width.V() + paddingsBorders)
		if marginL == pr.AutoF && marginR == pr.AutoF {
			if width.V()+paddingsBorders+right.V()+left.V() <= cbWidth {
				box.MarginLeft = widthForMargins / 2
				box.MarginRight = box.MarginLeft
			} else {
				if ltr {
					box.MarginLeft = pr.Float(0)
					box.MarginRight = widthForMargins
				} else {
					box.MarginLeft = widthForMargins
					box.MarginRight = pr.Float(0)
				}
			}
		} else if marginL == pr.AutoF {
			// gopdfgo: jak wyżej, CSS 2.1 § 10.3.7
			box.MarginLeft = widthForMargins - marginR.V()
		} else if marginR == pr.AutoF {
			box.MarginRight = widthForMargins - marginL.V()
		} else if ltr {
			box.MarginRight = widthForMargins - marginL.V()
		} else {
			box.MarginLeft = widthForMargins - marginR.V()
		}
		translateX = left.V() + defaultTranslateX
	} else {
		if marginL == pr.AutoF {
			box.MarginLeft = pr.Float(0)
		}
		if marginR == pr.AutoF {
			box.MarginRight = pr.Float(0)
		}
		spacing := paddingsBorders + box.MarginLeft.V() + box.MarginRight.V()
		if left == pr.AutoF && width == pr.AutoF {
			box.Width = shrinkToFit(context, box_, cbWidth-spacing-right.V())
			translateX = cbWidth - right.V() - spacing + defaultTranslateX
			translateBoxWidth = true
		} else if left == pr.AutoF && right == pr.AutoF {
			if !ltr {
				availableWidth := cbWidth - (paddingsBorders + box.MarginLeft.V() + box.MarginRight.V())
				translateBoxWidth = true
				translateX = defaultTranslateX + availableWidth
			}
		} else if width == pr.AutoF && right == pr.AutoF {
			box.Width = shrinkToFit(context, box_, cbWidth-spacing-left.V())
			translateX = left.V() + defaultTranslateX
		} else if left == pr.AutoF {
			translateX = cbWidth + defaultTranslateX - right.V() - spacing - width.V()
		} else if width == pr.AutoF {
			box.Width = cbWidth.V() - right.V() - left.V() - spacing
			translateX = left.V() + defaultTranslateX
		} else if right == pr.AutoF {
			translateX = left.V() + defaultTranslateX
		}
	}
	return translateBoxWidth, translateX
}

func absoluteHeight(box_ Box, containingBlock block) (bool, pr.Float) {
	box := box_.Box()

	paddingsBorders := box.PaddingTop.V() + box.PaddingBottom.V() + box.BorderTopWidth.V() + box.BorderBottomWidth.V()

	marginT := box.MarginTop
	marginB := box.MarginBottom
	height := box.Height
	top := box.Top
	bottom := box.Bottom

	cbY, cbHeight := containingBlock.Y, containingBlock.Height

	// https://www.w3.org/TR/CSS2/visudet.html#abs-non-replaced-height

	var translateY pr.Float = 0
	translateBoxHeight := false
	defaultTranslateY := cbY - box.PositionY
	if top == pr.AutoF && bottom == pr.AutoF && height == pr.AutoF {
		// Keep the static position
		if marginT == pr.AutoF {
			box.MarginTop = pr.Float(0)
		}
		if marginB == pr.AutoF {
			box.MarginBottom = pr.Float(0)
		}
	} else if top != pr.AutoF && bottom != pr.AutoF && height != pr.AutoF {
		heightForMargins := cbHeight - (top.V() + bottom.V() + height.V() + paddingsBorders)
		if marginT == pr.AutoF && marginB == pr.AutoF {
			box.MarginTop = heightForMargins / 2
			box.MarginBottom = box.MarginTop
		} else if marginT == pr.AutoF {
			// gopdfgo: jeden margines automatyczny dostaje resztę po odjęciu
			// drugiego (CSS 2.1 § 10.6.4); źródło pomijało drugi margines
			box.MarginTop = heightForMargins - marginB.V()
		} else if marginB == pr.AutoF {
			box.MarginBottom = heightForMargins - marginT.V()
		} else {
			box.MarginBottom = heightForMargins - marginT.V()
		}
		translateY = top.V() + defaultTranslateY
	} else {
		if marginT == pr.AutoF {
			box.MarginTop = pr.Float(0)
		}
		if marginB == pr.AutoF {
			box.MarginBottom = pr.Float(0)
		}
		spacing := paddingsBorders + box.MarginTop.V() + box.MarginBottom.V()
		if top == pr.AutoF && height == pr.AutoF {
			translateY = cbHeight.V() - bottom.V() - spacing + defaultTranslateY
			translateBoxHeight = true
		} else if top == pr.AutoF && bottom == pr.AutoF {
			// Keep the static position
		} else if height == pr.AutoF && bottom == pr.AutoF {
			translateY = top.V() + defaultTranslateY
		} else if top == pr.AutoF {
			translateY = cbHeight.V() + defaultTranslateY - bottom.V() - spacing - height.V()
		} else if height == pr.AutoF {
			// gopdfgo: przy align-self innym niż normal i stretch wysokość auto to fit-content, a pudełko
			// jest wyrównywane w bloku zawierającym pomniejszonym o top i bottom (CSS Align 3 § 6.1,
			// CSS Position 3 § 4.1); przesunięcie dolicza absoluteBlock po układzie. Tylko dla dzieci
			// siatki. Źródło zawsze rozciągało pudełko między top i bottom.
			if _, aligned := absoluteBlockAlignment(box_); !aligned {
				box.Height = cbHeight.V() - bottom.V() - top.V() - spacing
			}
			translateY = top.V() + defaultTranslateY
		} else if bottom == pr.AutoF {
			translateY = top.V() + defaultTranslateY
		}
	}
	return translateBoxHeight, translateY
}

// absoluteHeightMinMax (gopdfgo) stosuje min-height i max-height według
// CSS 2.1 § 10.7: gdy wysokość wstępna wykracza poza ograniczenie, reguły
// z § 10.6.4 liczy się ponownie z wysokością ograniczoną, więc marginesy
// automatyczne (wyśrodkowanie) i przesunięcie wynikają z wysokości ostatecznej.
// Źródło ograniczało wysokość dopiero po wyznaczeniu marginesów.
func absoluteHeightMinMax(box_ Box, containingBlock block) (bool, pr.Float) {
	box := box_.Box()
	marginTop, marginBottom := box.MarginTop, box.MarginBottom
	translateBoxHeight, translateY := absoluteHeight(box_, containingBlock)
	height, ok := box.Height.(pr.Float)
	if !ok {
		return translateBoxHeight, translateY
	}
	clamped := height
	if maxHeight, ok := box.MaxHeight.(pr.Float); ok && maxHeight != pr.Inf {
		clamped = min(clamped, maxHeight)
	}
	if minHeight, ok := box.MinHeight.(pr.Float); ok {
		clamped = max(clamped, minHeight)
	}
	if clamped == height {
		return translateBoxHeight, translateY
	}
	box.MarginTop, box.MarginBottom, box.Height = marginTop, marginBottom, clamped
	return absoluteHeight(box_, containingBlock)
}

// performs either "blockContainer", "flex" or "grid" layout on box_
func absoluteBlock(context *layoutContext, box_ Box, containingBlock block, fixedBoxes *[]*AbsolutePlaceholder,
	bottomSpace pr.Float, skipStack tree.ResumeStack,
) (Box, tree.ResumeStack) {
	box := box_.Box()
	cbWidth, cbHeight := containingBlock.Width, containingBlock.Height

	// gopdfgo: w kontenerze flex wysokość wyznaczona przez top i bottom jest znana przed szerokością
	// i wpływa na nią (rozciągnięty element z proporcjami, CSS Flexbox 1 § 9.9.1, test WPT
	// flex-abspos-inset-cross-size-001), więc liczymy ją najpierw; nie zależy od szerokości.
	heightFirst := bo.FlexContainerT.IsInstance(box_) && len(skipStack) == 0
	translateBoxHeight, translateY := false, pr.Float(0)
	if heightFirst {
		translateBoxHeight, translateY = absoluteHeightMinMax(box_, containingBlock)
	}
	translateBoxWidth, translateX := absoluteWidth(box_, context, containingBlock)
	if len(skipStack) == 0 && !heightFirst {
		translateBoxHeight, translateY = absoluteHeightMinMax(box_, containingBlock)
	}

	if traceMode {
		traceLogger.Dump(fmt.Sprintf("after absolute{Width, Height}: %s %s", tracer.FormatMaybeFloat(box.Width), tracer.FormatMaybeFloat(box.Height)))
	}

	if translateBoxHeight {
		bottomSpace -= box.PositionY
	} else {
		bottomSpace += translateY
	}

	// This box is the containing block for absolute descendants.
	var absoluteBoxes []*AbsolutePlaceholder

	if box.IsTableWrapper {
		// gopdfgo: tabela pozycjonowana bezwzględnie ma do dyspozycji bryłę
		// zawierającą pomniejszoną o left i right (CSS 2.1 § 10.3.7); źródło
		// dawało jej całą szerokość, więc tabela z left: 1px; right: 1px
		// wystawała o dwa piksele
		available := cbWidth
		if left, ok := box.Left.(pr.Float); ok {
			available -= left
		}
		if right, ok := box.Right.(pr.Float); ok {
			available -= right
		}
		tableWrapperWidth(context, box_, bo.MaybePoint{available, cbHeight})
	}

	var (
		newBox Box
		bl     blockLayout
	)
	laidOutAsColumns := false
	if multicol, ok := box_.(bo.BlockBoxITF); ok && bo.BlockT.IsInstance(box_) &&
		(box.Style.GetColumnWidth().Tag != pr.Auto || box.Style.GetColumnCount().String != "auto") {
		// gopdfgo: pudełko pozycjonowane bezwzględnie może być kontenerem kolumn (CSS Multicol 1 § 2);
		// w źródle jego treść była układana jak w zwykłym bloku (test WPT column-count-used-001).
		// Szerokość jest już wyznaczona przez absoluteWidth.
		cb := &bo.BoxFields{Style: box.Style, Width: cbWidth, Height: cbHeight}
		context.createBlockFormattingContext()
		laidOut, columnsResult := columnsLayout(context, multicol, bottomSpace, skipStack, cb,
			true, &absoluteBoxes, fixedBoxes, nil)
		if laidOut != nil {
			newBox, bl, laidOutAsColumns = laidOut, blockLayout{resumeAt: columnsResult.resumeAt}, true
			context.finishBlockFormattingContext(laidOut)
		} else {
			context.finishBlockFormattingContext(box_)
		}
	}
	if laidOutAsColumns {
		// ułożony wyżej
	} else if bo.BlockT.IsInstance(box_) {
		newBox, bl, _ = blockContainerLayout(context, box_, bottomSpace, skipStack, true,
			&absoluteBoxes, fixedBoxes, new([]pr.Float), false, -1)
	} else if bo.FlexContainerT.IsInstance(box_) {
		newBox, bl = flexLayout(context, box_, bottomSpace, skipStack, containingBlock, true, &absoluteBoxes, fixedBoxes, false)
	} else if bo.GridContainerT.IsInstance(box_) {
		newBox, bl = gridLayout(context, box_, bottomSpace, skipStack, containingBlock, true, &absoluteBoxes, fixedBoxes)
	}

	for _, childPlaceholder := range absoluteBoxes {
		absoluteLayout(context, childPlaceholder, newBox, fixedBoxes, bottomSpace, skipStack)
	}

	if translateBoxWidth {
		translateX -= newBox.Box().Width.V()
	}
	if translateBoxHeight {
		translateY -= newBox.Box().Height.V()
	}
	// gopdfgo: wyrównanie pudełka o wysokości fit-content między top i bottom, zob. absoluteHeight
	if fraction, aligned := absoluteBlockAlignment(box_); aligned && len(skipStack) == 0 && box.Style.GetHeight().Tag == pr.Auto {
		if top, ok := box.Top.(pr.Float); ok {
			if bottom, ok := box.Bottom.(pr.Float); ok {
				free := cbHeight.V() - top - bottom - newBox.Box().MarginHeight()
				translateY += max(free, 0) * fraction
			}
		}
	}

	newBox.Translate(translateX, translateY, false)

	return newBox, bl.resumeAt
}

// Set the width of absolute positioned “box“.
func absoluteLayout(context *layoutContext, placeholder *AbsolutePlaceholder, containingBlock Box,
	fixedBoxes *[]*AbsolutePlaceholder, bottomSpace pr.Float, skipStack tree.ResumeStack,
) {
	if placeholder.layoutDone {
		panic("placeholder can't have its layout done.")
	}
	box := placeholder.AliasBox
	newBox, resumeAt := absoluteBoxLayout(context, box, containingBlock, fixedBoxes, bottomSpace, skipStack)
	if static := box.Box().StaticAlign; static != nil && newBox != nil {
		// gopdfgo: wyrównanie pozycji statycznej dziecka siatki w osi bez podanych przesunięć
		style, laidOut := newBox.Box().Style, newBox.Box()
		// ułamek ≥ 10 oznacza wyrównanie „safe”: gdy wyrównane pudełko wychodzi poza pudełko zawierające,
		// wraca do początku (CSS Align 3 § 4.4). Przepełnienie sprawdzamy wobec pudełka zawierającego,
		// nie prostokąta pozycji statycznej, jak Chromium (zagadnienie CSSWG 11934, testy WPT
		// flex-abspos-align-self-safe-outer-cb-*)
		cb := containingBlock.Box()
		resolve := func(start, free, size, fraction, cbStart, cbSize pr.Float) pr.Float {
			if fraction >= 10 {
				fraction -= 10
				position := start + free*fraction
				if position < cbStart-0.01 || position+size > cbStart+cbSize+0.01 {
					return start
				}
			}
			return start + free*fraction
		}
		var dx, dy pr.Float
		if style.GetLeft().Tag == pr.Auto && style.GetRight().Tag == pr.Auto {
			dx = resolve(static[0], static[2]-laidOut.MarginWidth(), laidOut.MarginWidth(), static[4],
				cb.PaddingBoxX(), cb.PaddingWidth()) - laidOut.PositionX
		}
		if style.GetTop().Tag == pr.Auto && style.GetBottom().Tag == pr.Auto {
			dy = resolve(static[1], static[3]-laidOut.MarginHeight(), laidOut.MarginHeight(), static[5],
				cb.PaddingBoxY(), cb.PaddingHeight()) - laidOut.PositionY
		}
		if dx != 0 || dy != 0 {
			newBox.Translate(dx, dy, false)
		}
	}
	placeholder.setLaidOutBox(newBox)
	// gopdfgo: dalszy ciąg pudełka przyciętego przez blok zawierający nie jest widoczny, więc nie tworzy stron
	if _, isPage := containingBlock.(*bo.PageBox); !isPage && containingBlock.Box().Style.GetOverflow() != "visible" {
		resumeAt = nil
	}
	if resumeAt != nil {
		context.setBrokenOutOfFlow(placeholder, brokenBox{
			box:             box,
			containingBlock: containingBlock,
			resumeAt:        resumeAt,
		})
		return
	}
	// gopdfgo: pudełko pozycjonowane w całości poniżej obszaru strony trafia na tę stronę, na którą wypada
	// jego pozycja (treść dokumentu płynie przez strony w jednym układzie współrzędnych). W źródle zostawało
	// poniżej dolnej krawędzi strony i nie było widoczne.
	if bottomSpace > -pr.Inf && context.pageBottom != pr.Inf && context.pageHeight > 0 && context.clipDepth == 0 &&
		newBox.Box().Style.GetPosition().String == "absolute" &&
		(newBox.Box().Style.GetTop().Tag != pr.Auto || newBox.Box().Style.GetBottom().Tag != pr.Auto) {
		// tylko pudełka z podanym top albo bottom: pozycja statyczna podąża za miejscem w treści
		if top := newBox.Box().PositionY; top >= context.pageBottom && top < maxSliceableHeight {
			newBox.Box().DeferredToLaterPage = true
			context.setBrokenOutOfFlow(placeholder, brokenBox{box: box, containingBlock: containingBlock, deferredPages: 1})
		}
	}
}

func absoluteBoxLayout(context *layoutContext, box Box, cb_ Box, fixedBoxes *[]*AbsolutePlaceholder,
	bottomSpace pr.Float, skipStack tree.ResumeStack,
) (Box, tree.ResumeStack) {
	// https://www.w3.org/TR/CSS2/visudet.html1#containing-block-details

	if traceMode {
		traceLogger.DumpTree(box, "absoluteBoxLayout")
	}

	var containingBlock block
	cb := cb_.Box()
	if _, isPageBox := cb_.(*bo.PageBox); isPageBox {
		containingBlock.X = cb.ContentBoxX()
		containingBlock.Y = cb.ContentBoxY()
		containingBlock.Width = cb.Width.V()
		containingBlock.Height = cb.Height.V()
	} else {
		containingBlock.X = cb.PaddingBoxX()
		containingBlock.Y = cb.PaddingBoxY()
		containingBlock.Width = cb.PaddingWidth()
		containingBlock.Height = cb.PaddingHeight()
	}
	if area := box.Box().GridArea; area != nil {
		// gopdfgo: dziecko siatki pozycjonowane bezwzględnie: pudełkiem zawierającym jest obszar siatki
		containingBlock.X, containingBlock.Y, containingBlock.Width, containingBlock.Height = area[0], area[1], area[2], area[3]
	}

	resolveSizes(context, box, bo.MaybePoint{containingBlock.Width, containingBlock.Height})
	resolvePositionPercentages(box.Box(), bo.Point{containingBlock.Width, containingBlock.Height})

	context.createBlockFormattingContext()
	var (
		newBox   Box
		resumeAt tree.ResumeStack
	)
	if bo.BlockReplacedT.IsInstance(box) {
		newBox = absoluteReplaced(box, containingBlock)
	} else {
		// Absolute tables are wrapped into block boxes
		newBox, resumeAt = absoluteBlock(context, box, containingBlock, fixedBoxes, bottomSpace, skipStack)
	}
	context.finishBlockFormattingContext(newBox)
	return newBox, resumeAt
}

func intDiv(a pr.Float, b int) pr.Float {
	return pr.Float(int(math.Floor(float64(a))) / b)
}

func absoluteReplaced(box_ Box, containingBlock block) Box {
	inlineReplacedBoxWidthHeight(box_, containingBlock)
	box := box_.Box()
	cbX, cbY, cbWidth, cbHeight := containingBlock.X, containingBlock.Y, containingBlock.Width, containingBlock.Height

	ltr := box.Style.ParentStyle() == nil || box.Style.ParentStyle().GetDirection() == pr.Ltr

	// https://www.w3.org/TR/CSS21/visudet.html#abs-replaced-width
	if box.Left == pr.AutoF && box.Right == pr.AutoF {
		// static position:
		if ltr {
			box.Left = box.PositionX - cbX
		} else {
			box.Right = cbX + cbWidth - box.PositionX
		}
	}
	if box.Left == pr.AutoF || box.Right == pr.AutoF {
		if box.MarginLeft == pr.AutoF {
			box.MarginLeft = pr.Float(0)
		}
		if box.MarginRight == pr.AutoF {
			box.MarginRight = pr.Float(0)
		}
		remaining := cbWidth - box.MarginWidth()
		if box.Left == pr.AutoF {
			box.Left = remaining - box.Right.V()
		}
		if box.Right == pr.AutoF {
			box.Right = remaining - box.Left.V()
		}
	} else if pr.AutoF == box.MarginLeft || pr.AutoF == box.MarginRight {
		remaining := cbWidth - (box.BorderWidth() + box.Left.V() + box.Right.V())
		if box.MarginLeft == pr.AutoF && box.MarginRight == pr.AutoF {
			if remaining >= 0 {
				box.MarginLeft = intDiv(remaining, 2)
				box.MarginRight = box.MarginLeft
			} else if ltr {
				box.MarginLeft = pr.Float(0)
				box.MarginRight = remaining
			} else {
				box.MarginLeft = remaining
				box.MarginRight = pr.Float(0)
			}
		} else if box.MarginLeft == pr.AutoF {
			box.MarginLeft = remaining
		} else {
			box.MarginRight = remaining
		}
	} else {
		// Over-constrained
		if ltr {
			box.Right = cbWidth - (box.MarginWidth() + box.Left.V())
		} else {
			box.Left = cbWidth - (box.MarginWidth() + box.Right.V())
		}
	}

	// https://www.w3.org/TR/CSS21/visudet.html#abs-replaced-height
	if box.Top == pr.AutoF && box.Bottom == pr.AutoF {
		box.Top = box.PositionY - cbY
	}
	if box.Top == pr.AutoF || box.Bottom == pr.AutoF {
		if box.MarginTop == pr.AutoF {
			box.MarginTop = pr.Float(0)
		}
		if box.MarginBottom == pr.AutoF {
			box.MarginBottom = pr.Float(0)
		}
		remaining := cbHeight - box.MarginHeight()
		if box.Top == pr.AutoF {
			box.Top = remaining - box.Bottom.V()
		}
		if box.Bottom == pr.AutoF {
			box.Bottom = remaining - box.Top.V()
		}
	} else if box.MarginTop == pr.AutoF || box.MarginBottom == pr.AutoF {
		remaining := cbHeight - (box.BorderHeight() + box.Top.V() + box.Bottom.V())
		if box.MarginTop == pr.AutoF && box.MarginBottom == pr.AutoF {
			box.MarginTop = intDiv(remaining, 2)
			box.MarginBottom = box.MarginTop
		} else if box.MarginTop == pr.AutoF {
			box.MarginTop = remaining
		} else {
			box.MarginBottom = remaining
		}
	} else {
		// Over-constrained
		box.Bottom = cbHeight - (box.MarginHeight() + box.Top.V())
	}

	// No children for replaced boxes, no need to .translate()
	box.PositionX = cbX + box.Left.V()
	box.PositionY = cbY + box.Top.V()
	return box_
}

// absoluteBlockAlignment (gopdfgo) zwraca dla dziecka siatki pozycjonowanego bezwzględnie z podanym
// align-self innym niż auto, normal i stretch położenie w wolnym miejscu w pionie (0 początek,
// 0.5 środek, 1 koniec); baseline wyrównuje się jak start, last baseline jak end (CSS Align 3 § 4.3).
func absoluteBlockAlignment(box_ Box) (pr.Float, bool) {
	box := box_.Box()
	if box.StaticAlign == nil {
		return 0, false
	}
	alignSelf := box.Style.GetAlignSelf()
	if alignSelf.Intersects(pr.Auto, pr.Normal, pr.Stretch) {
		return 0, false
	}
	switch {
	case alignSelf.Has(pr.Center):
		return 0.5, true
	case alignSelf.Intersects(pr.End, pr.FlexEnd, pr.SelfEnd, pr.Last):
		return 1, true
	}
	return 0, true
}
