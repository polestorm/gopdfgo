package layout

import (
	"fmt"
	"sort"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
)

// Layout for floating boxes.

var floatWidth = handleMinMaxWidth(floatWidth_)

// @handleMinMaxWidth
// containingBlock must be block
func floatWidth_(box Box, context *layoutContext, containingBlock containingBlock) (bool, pr.Float) {
	// Check that box.width is auto even if the caller does it too, because
	// the handleMinMaxWidth decorator can change the value
	if w := box.Box().Width; w == pr.AutoF {
		box.Box().Width = shrinkToFit(context, box, containingBlock.(block).Width)
	}
	return false, 0
}

// Set the width and position of floating “box“.
func floatLayout(context *layoutContext, box_ Box, containingBlock *bo.BoxFields, absoluteBoxes,
	fixedBoxes *[]*AbsolutePlaceholder, bottomSpace pr.Float, skipStack tree.ResumeStack,
) (Box, tree.ResumeStack) {
	cbWidth, cbHeight := containingBlock.Width, containingBlock.Height
	resolveSizes(context, box_, bo.MaybePoint{cbWidth, cbHeight})

	// TODO: This is only handled later in blocks.blockContainerLayout
	// https://www.w3.org/TR/CSS21/visudet.html#normal-block
	if cbHeight == pr.AutoF {
		cbHeight = containingBlock.PositionY - containingBlock.ContentBoxY()
	}

	box := box_.Box()
	resolvePositionPercentages(box, bo.Point{cbWidth.V(), cbHeight.V()})

	if box.MarginLeft == pr.AutoF {
		box.MarginLeft = pr.Float(0)
	}
	if box.MarginRight == pr.AutoF {
		box.MarginRight = pr.Float(0)
	}
	if box.MarginTop == pr.AutoF {
		box.MarginTop = pr.Float(0)
	}
	if box.MarginBottom == pr.AutoF {
		box.MarginBottom = pr.Float(0)
	}

	clearance := getClearance(context, box, 0)
	if clearance != nil {
		box.PositionY += clearance.V()
	}

	if bo.BlockReplacedT.IsInstance(box_) {
		inlineReplacedBoxWidthHeight(box_, containingBlock)
	} else if box.Width == pr.AutoF || !box.IsTableWrapper {
		// gopdfgo: min-width i max-width dotyczą też elementu pływającego o podanej szerokości (CSS 2.1
		// § 10.4); w źródle dekorator ograniczający szerokość był wołany tylko dla width: auto (test WPT
		// max-width-applies-to-018).
		floatWidth(box_, context, block{Width: containingBlock.Width.V()})
	}

	if box.IsTableWrapper {
		tableWrapperWidth(context, box_, bo.MaybePoint{cbWidth, cbHeight})
	}

	var (
		resumeAt tree.ResumeStack
		tmp      blockLayout
	)
	laidOutAsColumns := false
	if multicol, ok := box_.(bo.BlockBoxITF); ok && bo.BlockT.IsInstance(box_) &&
		(box.Style.GetColumnWidth().Tag != pr.Auto || box.Style.GetColumnCount().String != "auto") {
		// gopdfgo: element pływający może być kontenerem kolumn (CSS Multicol 1 § 2); w źródle jego
		// treść była układana jak w zwykłym bloku, bez kolumn. Gdy w kolumnach nie mieści się nic,
		// zostaje układ źródła.
		context.createBlockFormattingContext()
		laidOut, columnsResult := columnsLayout(context, multicol, bottomSpace, skipStack, containingBlock,
			true, absoluteBoxes, fixedBoxes, nil)
		if laidOut != nil {
			box_, resumeAt, laidOutAsColumns = laidOut, columnsResult.resumeAt, true
		}
		context.finishBlockFormattingContext(box_)
	}
	if laidOutAsColumns {
		// ułożony wyżej
	} else if bo.BlockContainerT.IsInstance(box_) {
		context.createBlockFormattingContext()
		box_, tmp, _ = blockContainerLayout(context, box_, bottomSpace,
			skipStack, true, absoluteBoxes, fixedBoxes, new([]pr.Float), false, -1)
		resumeAt = tmp.resumeAt
		context.finishBlockFormattingContext(box_)
	} else if bo.FlexContainerT.IsInstance(box_) {
		box_, tmp = flexLayout(context, box_, bottomSpace, skipStack, containingBlock,
			true, absoluteBoxes, fixedBoxes, false)
		resumeAt = tmp.resumeAt
	} else if bo.GridContainerT.IsInstance(box_) {
		box_, tmp = gridLayout(
			context, box_, bottomSpace, skipStack, containingBlock,
			true, absoluteBoxes, fixedBoxes)
		resumeAt = tmp.resumeAt
	} else if !bo.BlockReplacedT.IsInstance(box_) {
		panic(fmt.Sprintf("expected BlockReplaced , got %v", box))
	}

	box_ = findFloatPosition(context, box_, containingBlock)

	context.excludedShapes.append(box_.Box())

	return box_, resumeAt
}

// Get the right position of the float “box“.
func findFloatPosition(context *layoutContext, box_ Box, containingBlock *bo.BoxFields) Box {
	box := box_.Box()
	// See https://www.w3.org/TR/CSS2/visuren.html#float-position

	// Point 4 is already handled as box.positionY is set according to the
	// containing box top position, with collapsing margins handled

	// Points 5 and 6, box.positionY is set to the highest positionY possible
	if L := len(context.excludedShapes.list); L != 0 {
		highestY := (context.excludedShapes.list)[L-1].PositionY
		if box.PositionY < highestY {
			box_.Translate(0, highestY-box.PositionY, false)
		}
	}

	// Points 1 and 2
	positionX, positionY, availableWidth := avoidCollisions(context, box_, containingBlock, true)
	// Point 9
	// positionY is set now, let's define positionX
	// for float: left elements, it's already done!
	if box.Style.GetFloat() == "right" {
		positionX += availableWidth - box.MarginWidth()
	}

	box_.Translate(positionX-box.PositionX, positionY-box.PositionY, false)

	return box_
}

// Return nil if there is no clearance, otherwise the clearance value (as Float)
// [collapsedMargin] defaults to 0
func getClearance(context *layoutContext, box *bo.BoxFields, collapsedMargin pr.Float) (clearance pr.MaybeFloat) {
	// Box should be after shape that’s broken on this page.
	for _, brokenShape := range context.brokenOutOfFlow {
		if brokenShape.box.Box().IsFloated() {
			if clear := box.Style.GetClear(); clear == brokenShape.box.Box().Style.GetFloat() || clear == "both" {
				return pr.Inf
			}
		}
	}
	// Hypothetical position is the position of the top border edge
	hypotheticalPosition := box.PositionY + collapsedMargin
	for _, excludedShape := range context.excludedShapes.list {
		if clear := box.Style.GetClear(); clear == excludedShape.Style.GetFloat() || clear == "both" {
			y, h := excludedShape.PositionY, excludedShape.MarginHeight()
			if hypotheticalPosition < y+h {
				var safeClearance pr.Float
				if clearance != nil {
					safeClearance = clearance.V()
				}
				clearance = max(safeClearance, y+h-hypotheticalPosition)
			}
		}
	}
	return clearance
}

// forcedClearanceFor (gopdfgo) liczy clearance pudełka, które bez niego pociągnęłoby za sobą przylegający
// element pływający (zob. inFlowLayout): górna krawędź obramowania staje równo z dołem najniższego
// oczyszczanego elementu pływającego, niezależnie od wielkości marginesu (CSS 2.1 § 9.5.2; pozycja
// hipotetyczna przesuwa w takim przypadku i sam element pływający, więc nigdy nie leży poniżej niego).
// Zwraca nil, gdy nie ma czego oczyszczać.
func forcedClearanceFor(context *layoutContext, box *bo.BoxFields, collapsedMargin pr.Float) pr.MaybeFloat {
	clear := box.Style.GetClear()
	bottom, found := pr.Float(0), false
	for _, shape := range context.excludedShapes.list {
		if clear == shape.Style.GetFloat() || clear == "both" {
			if b := shape.PositionY + shape.MarginHeight(); !found || b > bottom {
				bottom, found = b, true
			}
		}
	}
	if !found {
		return nil
	}
	return bottom - (box.PositionY + collapsedMargin)
}

// avoidsFloats (gopdfgo): pudełko blokowe, którego obramowanie nie może nachodzić na elementy pływające
// (CSS 2.1 § 9.5): tabela, element zastępowany i pudełko zakładające własny kontekst formatowania.
func avoidsFloats(box_ Box) bool {
	box := box_.Box()
	if box.IsFloated() || !box.IsInNormalFlow() || box.IsFlexItem || box.IsGridItem {
		return false
	}
	return box.IsTableWrapper || bo.BlockReplacedT.IsInstance(box_) || bo.EstablishesFormattingContext(box_)
}

// fitsBesideFloats (gopdfgo) sprawdza, czy pudełko omijające elementy pływające zmieści się obok nich,
// gdy górna krawędź jego obramowania leży na wysokości y w bloku zawierającym cb. Szerokość i wysokość
// są brane ze stylu; gdy szerokość nie jest podana w px albo procentach, przyjmujemy, że się zmieści
// (pudełko o szerokości auto jest zwężane).
func fitsBesideFloats(context *layoutContext, box_ Box, y pr.Float, cb *bo.BoxFields) bool {
	style := box_.Box().Style
	cbWidth, ok := cb.Width.(pr.Float)
	if !ok {
		return true
	}
	length := func(v pr.TaggedDim) (pr.Float, bool) {
		if v.Calc != nil || v.Tag == pr.Auto {
			return 0, false
		}
		switch v.Unit {
		case pr.Px:
			return v.Value, true
		case pr.Perc:
			return v.Value * cbWidth / 100, true
		}
		return 0, v.Value == 0
	}
	width, ok := length(style.GetWidth())
	if !ok || style.GetWidth().Intrinsic != pr.IntrinsicNone {
		return true
	}
	if style.GetBoxSizing() != "border-box" {
		for _, side := range []pr.TaggedDim{style.GetPaddingLeft(), style.GetPaddingRight()} {
			v, ok := length(side)
			if !ok {
				return true
			}
			width += v
		}
		if style.GetBorderLeftStyle() != "none" && style.GetBorderLeftStyle() != "hidden" {
			width += style.GetBorderLeftWidth().Value
		}
		if style.GetBorderRightStyle() != "none" && style.GetBorderRightStyle() != "hidden" {
			width += style.GetBorderRightWidth().Value
		}
	}
	var height pr.Float
	if h := style.GetHeight(); h.Unit == pr.Px && h.Calc == nil && h.Tag != pr.Auto {
		height = h.Value
	}
	height = max(height, 0.01)
	left, right := cb.ContentBoxX(), cb.ContentBoxX()+cbWidth
	for _, shape := range context.excludedShapes.list {
		if shape.PositionY >= y+height || shape.PositionY+shape.MarginHeight() <= y || shape.MarginHeight() <= 0 {
			continue
		}
		switch shape.Style.GetFloat() {
		case "left":
			left = max(left, shape.PositionX+shape.MarginWidth())
		case "right":
			right = min(right, shape.PositionX)
		}
	}
	return width <= right-left+0.01
}

// outer=true
func avoidCollisions(context *layoutContext, box_ Box, containingBlock *bo.BoxFields, outer bool) (pr.Float, pr.Float, pr.Float) {
	excludedShapes := context.excludedShapes
	box := box_.Box()
	positionY := box.BorderBoxY()
	boxWidth := box.BorderWidth()
	boxHeight := box.BorderHeight()
	if outer {
		positionY = box.PositionY
		boxWidth = box.MarginWidth()
		boxHeight = box.MarginHeight()
	}

	// gopdfgo: element pływający o zerowej wysokości jest umieszczany według tych samych reguł co inne
	// (CSS 2.1 § 9.5.1) i jako wykluczenie skraca wiersze, których pas obejmuje jego położenie (test WPT
	// floats-zero-height-wrap-002). Źródło zwracało dla niego pozycję (0, 0) strony.
	var maxLeftBound, maxRightBound pr.Float
	for {
		var collidingShapes []*bo.BoxFields
		for _, shape := range excludedShapes.list {
			// gopdfgo: element pływający o zerowej szerokości nie ma pola, na które można by nachodzić
			// (CSS 2.1 § 9.5), więc nie zwęża pudełek z własnym kontekstem formatowania ani wierszy;
			// w źródle wyznaczał granicę, choć pudełko z ujemnym marginesem mogło ją przekroczyć
			// (test WPT zero-width-floats).
			if !outer && shape.MarginWidth() <= 0 {
				continue
			}
			// Assign locals to avoid slow attribute lookups.
			shapePositionY := shape.PositionY
			shapeMarginHeight := shape.MarginHeight()
			if shapeMarginHeight <= 0 {
				// gopdfgo: element pływający o zerowej wysokości wyklucza tylko pasy, które obejmują jego
				// położenie w środku; pudełko zaczynające się na tej wysokości go nie widzi (testy WPT
				// floats-zero-height-wrap-002, float-006, floats-placement-001)
				if positionY < shapePositionY && shapePositionY < positionY+boxHeight {
					collidingShapes = append(collidingShapes, shape)
				}
				continue
			}
			if (shapePositionY < positionY && positionY < shapePositionY+shapeMarginHeight) ||
				(shapePositionY < positionY+boxHeight && positionY+boxHeight < shapePositionY+shapeMarginHeight) ||
				(shapePositionY >= positionY && shapePositionY+shapeMarginHeight <= positionY+boxHeight) {
				collidingShapes = append(collidingShapes, shape)
			}
		}
		var leftBounds, rightBounds []pr.Float
		for _, shape := range collidingShapes {
			if shape.Style.GetFloat() == "left" {
				leftBounds = append(leftBounds, shape.PositionX+shape.MarginWidth())
			}
			if shape.Style.GetFloat() == "right" {
				rightBounds = append(rightBounds, shape.PositionX)
			}

		}
		// Set the default maximum bounds
		maxLeftBound = containingBlock.ContentBoxX()
		maxRightBound = containingBlock.ContentBoxX() + containingBlock.Width.V()

		if !outer {
			maxLeftBound += box.MarginLeft.V()
			maxRightBound -= box.MarginRight.V()
		}

		// Set the real maximum bounds according to sibling float elements
		if len(leftBounds) != 0 || len(rightBounds) != 0 {
			if len(leftBounds) != 0 {
				maxLeftBound = max(pr.Maxs(leftBounds...), maxLeftBound)
			}
			if len(rightBounds) != 0 {
				maxRightBound = min(pr.Mins(rightBounds...), maxRightBound)
			}

			// gopdfgo: margines po stronie końca wiersza nie odbiera miejsca pudełku o podanej szerokości:
			// przy nadokreśleniu jest liczony od nowa (CSS 2.1 § 10.3.3), więc nie może zepchnąć
			// pudełka pod element pływający. W źródle pudełko 50 px z marginesem 1 px nie mieściło się
			// w 50 px obok elementu pływającego.
			var endMargin pr.Float
			if !outer && box.Style.GetWidth().Tag != pr.Auto {
				if containingBlock.Style.GetDirection() == pr.Rtl {
					endMargin = max(box.MarginLeft.V(), 0)
				} else {
					endMargin = max(box.MarginRight.V(), 0)
				}
			}

			// gopdfgo: dla elementu pływającego krawędź bloku zawierającego ogranicza go tylko wtedy, gdy
			// po tej samej stronie stoi inny element pływający (CSS 2.1 § 9.5.1, reguła 7); bez niego element
			// szerszy niż blok zawierający mieści się, dopóki nie sięga elementu pływającego z przeciwnej
			// strony (reguła 3). W źródle zawsze porównywano z krawędzią bloku, więc taki element spadał
			// pod element pływający przeciwnej strony (testy WPT floats-rule3-outside-*).
			fitLeft, fitRight := maxLeftBound, maxRightBound
			if outer {
				switch box.Style.GetFloat() {
				case "left":
					if len(leftBounds) == 0 {
						fitRight = pr.Inf
						if len(rightBounds) != 0 {
							fitRight = pr.Mins(rightBounds...)
						}
					}
				case "right":
					if len(rightBounds) == 0 {
						fitLeft = -pr.Inf
						if len(leftBounds) != 0 {
							fitLeft = pr.Maxs(leftBounds...)
						}
					}
				}
			}

			// Points 3, 7 && 8
			if boxWidth > fitRight-fitLeft+endMargin {
				// The box does not fit here
				min := pr.Inf
				for _, shape := range collidingShapes {
					if v := shape.PositionY + shape.MarginHeight(); v < min {
						min = v
					}
				}
				newPositonY := min

				if newPositonY > positionY {
					// We can find a solution with a higher positionY
					positionY = newPositonY
					continue
				} // No solution, we must put the box here
			}
		}
		break
	}
	positionX := maxLeftBound

	// See https://www.w3.org/TR/CSS21/visuren.html#floats
	// Boxes that can’t collide with floats are:
	// - floats
	// - line boxes
	// - table wrappers
	// - block-level replaced box
	// - element establishing new formatting contexts
	if traceMode {
		if fl := box.Style.GetFloat(); !(fl == "right" || fl == "left" || bo.LineT.IsInstance(box_) ||
			box.IsTableWrapper || bo.BlockReplacedT.IsInstance(box_) || bo.EstablishesFormattingContext(box_)) {
			panic("assertion failed")
		}
	}

	// The x-position of the box depends on its type.
	positionX = maxLeftBound
	if box.Style.GetFloat() == "none" {
		if containingBlock.Style.GetDirection() == pr.Rtl {
			if bo.LineT.IsInstance(box_) {
				// The position of the line is the position of the cursor, at
				// the right bound.
				positionX = maxRightBound
			} else if box.IsTableWrapper {
				// The position of the right border of the table is at the right
				// bound.
				positionX = maxRightBound - boxWidth
			} else {
				// The position of the right border of the replaced box or
				// formatting context is at the right bound.
				if !(bo.BlockReplacedT.IsInstance(box_) || bo.EstablishesFormattingContext(box_)) {
					panic(fmt.Sprintf("expected BlockContainer, got %T", box_))
				}
				positionX = maxRightBound - boxWidth
			}
		}
	}

	availableWidth := maxRightBound - maxLeftBound
	if !outer {
		positionX -= box.MarginLeft.V()
		positionY -= box.MarginTop.V()
	}

	return positionX, positionY, availableWidth
}

// bandAvailable (gopdfgo) podaje szerokość pasa dla pudełka obramowania box między elementami
// pływającymi na wysokościach [y, y+height), z marginesami poziomymi pudełka, jak avoidCollisions
// z outer=false, ale bez przesuwania w dół.
func bandAvailable(context *layoutContext, box_ Box, containingBlock *bo.BoxFields, y, height pr.Float) pr.Float {
	box := box_.Box()
	left := containingBlock.ContentBoxX() + box.MarginLeft.V()
	right := containingBlock.ContentBoxX() + containingBlock.Width.V() - box.MarginRight.V()
	height = max(height, 0.01)
	for _, shape := range context.excludedShapes.list {
		if shape.MarginWidth() <= 0 || shape.PositionY >= y+height || shape.PositionY+shape.MarginHeight() <= y {
			continue
		}
		switch shape.Style.GetFloat() {
		case "left":
			left = max(left, shape.PositionX+shape.MarginWidth())
		case "right":
			right = min(right, shape.PositionX)
		}
	}
	return right - left
}

// relayoutBesideFloats (gopdfgo): pudełko z własnym kontekstem formatowania albo tabela o szerokości
// auto, ułożone na pozycji hipotetycznej w pasie obok elementów pływających, okazało się na całej
// wysokości szersze niż pas (bo niżej stoją elementy pływające szersze niż na górze), więc avoidCollisions
// zepchnęłoby je pod elementy pływające. Zgodnie z CSS 2.1 § 9.5 takie pudełko może zostać zwężone,
// żeby zmieścić się obok elementów pływających; próbujemy kolejno: tę samą wysokość z pasem zwężonym
// do najwęższego miejsca na wysokości pudełka, potem następne wysokości (dolne krawędzie elementów
// pływających). Tak robią przeglądarki (testy WPT floats-bfc-003, table-among-floats-001,
// table-sizing-with-adjacent-floats, floats-wrap-bfc-002-*). relayout układa pudełko ponownie
// w pasie o podanej szerokości (dla tabeli: szerokość dostępna). Gdy nic się nie mieści, zostaje
// układ pierwotny (pod elementami pływającymi).
func relayoutBesideFloats(context *layoutContext, original, laidOut Box, containingBlock *bo.BoxFields,
	relayout func(width pr.Float) (Box, blockLayout),
) (Box, blockLayout, bool) {
	box := original.Box()
	if len(context.excludedShapes.list) == 0 || box.IsFlexItem || box.IsGridItem || box.IsColumn || !box.IsInNormalFlow() ||
		!(box.IsTableWrapper || bo.EstablishesFormattingContext(original)) {
		return nil, blockLayout{}, false
	}
	if _, ok := containingBlock.Width.(pr.Float); !ok {
		return nil, blockLayout{}, false
	}
	if box.IsTableWrapper {
		if width := box.GetWrappedTable().Box().Style.GetWidth(); width.Tag != pr.Auto || width.Intrinsic != pr.IntrinsicNone {
			return nil, blockLayout{}, false
		}
	} else if width := box.Style.GetWidth(); width.Tag != pr.Auto || width.Intrinsic != pr.IntrinsicNone {
		return nil, blockLayout{}, false
	}
	laid := laidOut.Box()
	y0 := laid.BorderBoxY()
	_, marginY, _ := avoidCollisions(context, laidOut, containingBlock, false)
	finalY := marginY + laid.MarginTop.V()
	if finalY <= y0+0.01 {
		return nil, blockLayout{}, false
	}
	candidates := []pr.Float{y0}
	for _, shape := range context.excludedShapes.list {
		if bottom := shape.PositionY + shape.MarginHeight(); bottom > y0+0.01 && bottom < finalY-0.01 {
			candidates = append(candidates, bottom)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	originalWidth := laid.BorderWidth()
	if box.IsTableWrapper {
		originalWidth = containingBlock.Width.V()
	}
	for i, y := range candidates {
		if i > 0 && y-candidates[i-1] < 0.01 {
			continue
		}
		width := bandAvailable(context, laidOut, containingBlock, y, 0)
		for try := 0; try < 3 && width > 0; try++ {
			relaid, layout := relayout(width)
			if relaid == nil {
				return relaid, layout, true
			}
			rb := relaid.Box()
			fit := bandAvailable(context, relaid, containingBlock, y, rb.BorderHeight())
			if rb.BorderWidth() <= fit+0.01 {
				relaid.Translate(0, y-rb.BorderBoxY(), false)
				return relaid, layout, true
			}
			if fit >= width-0.01 {
				break
			}
			width = fit
		}
	}
	relaid, layout := relayout(originalWidth)
	return relaid, layout, true
}

// clearAfterLine (gopdfgo) obsługuje właściwość clear elementu <br>: następny wiersz zaczyna się
// poniżej elementów pływających po wskazanej stronie (HTML § 15.3.3 odwzorowuje atrybut clear
// elementu br na tę właściwość; Chromium stosuje ją do wiersza po złamaniu). Zwraca nowy początek
// następnego wiersza. Źródło ignorowało clear na elemencie liniowym.
func clearAfterLine(context *layoutContext, line Box, y pr.Float) pr.Float {
	clear := ""
	var walk func(b Box)
	walk = func(b Box) {
		for _, child := range b.Box().Children {
			if child.Box().Element != nil && child.Box().Element.Data == "br" && child.Box().PseudoType == "" {
				if c := child.Box().Style.GetClear(); c != "none" {
					clear = string(c)
				}
			}
			walk(child)
		}
	}
	walk(line)
	if clear == "" || context.excludedShapes == nil {
		return y
	}
	for _, shape := range context.excludedShapes.list {
		if float := string(shape.Style.GetFloat()); clear == "both" || clear == float {
			y = max(y, shape.PositionY+shape.MarginHeight())
		}
	}
	return y
}
