package layout

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
)

// Layout for columns.

func isInFloats(v pr.Float, l []pr.Float) bool {
	for _, vl := range l {
		if vl == v {
			return true
		}
	}
	return false
}

// if box is nil, then represents a list
type boxOrList struct {
	box  bo.BlockLevelBoxITF
	list []Box
}

// Lay out a multi-column “box“.
func columnsLayout(context *layoutContext, box_ bo.BlockBoxITF, bottomSpace pr.Float, skipStack tree.ResumeStack, containingBlock *bo.BoxFields,
	pageIsEmpty bool, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder, adjoiningMargins []pr.Float,
) (bo.BlockLevelBoxITF, blockLayout) {
	style := box_.Box().Style
	width_ := style.GetColumnWidth()
	count_ := style.GetColumnCount()
	height_ := style.GetHeight()
	originalBottomSpace := bottomSpace

	wasInColumn := context.inColumn
	context.inColumn = true

	if style.GetPosition().String == "relative" {
		// New containing block, use a new absolute list
		absoluteBoxes = &[]*AbsolutePlaceholder{}
	}

	// gopdfgo: element rozpinający może stać głębiej niż dziecko kontenera (CSS Multicol 1 § 6.1);
	// wynosimy go na poziom dzieci, dzieląc przodków. Zmieniamy pudełko źródłowe, żeby indeksy
	// wznowienia na następnej stronie odnosiły się do tej samej listy; operacja jest idempotentna.
	if hoisted, changed := hoistSpanners(box_.Box().Children); changed {
		box_.Box().Children = hoisted
	}
	if skipStack == nil {
		resetSpannerSplitUsed(box_.Box().Children)
	}
	box_ = bo.CopyWithChildren(box_, box_.Box().Children).(bo.BlockBoxITF) // CopyWithChildren preserves the concrete type of box_
	box := box_.Box()
	// gopdfgo: własny górny margines kontenera kolumn zwija się z marginesami sąsiednimi (CSS 2 § 8.3.1);
	// w źródle nie był do nich doliczany, więc kontener z marginesem większym niż margines poprzednika
	// stał za wysoko.
	if marginTop, ok := box.MarginTop.(pr.Float); ok {
		box.PositionY += collapseMargin(append(append([]pr.Float(nil), adjoiningMargins...), marginTop)) - marginTop
	} else {
		box.PositionY += collapseMargin(adjoiningMargins)
	}

	// Set height if defined
	heightDefined := false
	// gopdfgo: kontener o podanej wysokości, którego treść przechodzi na następną stronę, zajmuje na tej
	// stronie miejsce do jej dołu, a na następnej resztę wysokości (CSS Fragmentation 3 § 5.1), tak jak
	// zwykły blok (zob. block.go, fixedHeightRemaining). W źródle każdy fragment miał całą wysokość, więc
	// kontener, który nie mieścił się w reszcie strony, był przenoszony w całości.
	// gopdfgo: wysokości procentowe dzieci liczą się od wysokości treści kontenera kolumn, o ile jest
	// określona (CSS Multicol 1 § 3: pudełka kolumn są anonimowe); w źródle od wysokości pudełka
	// kolumny, czyli zawsze od auto.
	var percentBase pr.MaybeFloat
	if base, ok := box.Height.(pr.Float); ok {
		percentBase = base
	}
	fixedHeight, hasFixedHeight := pr.Float(0), false
	if height_.Tag != pr.Auto && height_.Unit != pr.Perc {
		if height_.Unit != pr.Px {
			panic(fmt.Sprintf("expected Px got %v", height_))
		}
		fixedHeight, hasFixedHeight = height_.Value, true
		if remaining, ok := context.fixedHeightRemaining[resumeKey(skipStack)]; ok && skipStack != nil {
			fixedHeight = remaining
			box.Height = remaining
		}
		height_.Value = fixedHeight
		emptySpace := context.pageBottom - box.ContentBoxY() - height_.Value
		// gopdfgo: dodatkowe kolumny w kierunku wiersza powstają tylko wtedy, gdy treść ogranicza
		// wysokość kontenera; gdy wcześniej kończy się strona, dalsza treść przechodzi na następną
		// stronę (CSS Multicol 1 § 8.2). W źródle podana wysokość zawsze dawała dodatkowe kolumny.
		heightDefined = emptySpace >= bottomSpace
		bottomSpace = max(bottomSpace, emptySpace)
	}

	definedBottomSpace := bottomSpace

	blockLevelWidth(box_, nil, containingBlock)

	var gap pr.Float
	if gapV := style.GetColumnGap(); gapV.Tag == pr.Normal {
		// 1em because in column context
		gap = style.GetFontSize().Value
	} else {
		gap = pr.ResolvePercentage(gapV, box.Width.V()).V()
	}

	// Define the number of columns and their widths

	availableWidth := box.Width.V()
	var (
		width pr.Float
		count int
	)
	if width_.Tag == pr.Auto && count_.String != "auto" {
		count = count_.Int
		width = max(0, availableWidth-(pr.Float(count)-1)*gap) / pr.Float(count)
	} else if width_.Tag != pr.Auto && count_.String == "auto" {
		count = int(max(1, pr.Floor((availableWidth+gap)/(width_.Value+gap))))
		width = (availableWidth+gap)/pr.Float(count) - gap
	} else { // overconstrained, with width != 'auto' and count != 'auto'
		count = int(max(1, min(pr.Float(count_.Int), pr.Floor((availableWidth+gap)/(width_.Value+gap)))))
		width = (availableWidth+gap)/pr.Float(count) - gap
	}

	// Handle column-span property with the following structure:
	// columnsAndBlocks = [
	//     [columnChild1, columnChild2],
	//     spanningBlock,
	//     …
	// ]
	type ibl struct {
		index int
		bl    boxOrList
	}
	var (
		columnsAndBlocks []ibl
		columnChildren   []Box
	)
	skip := 0
	if skipStack != nil {
		skip, _ = skipStack.Unpack()
	}
	for i_, child := range box.Children[skip:] {
		index := i_ + skip
		if bo.IsColumnSpanner(child) {
			if len(columnChildren) != 0 {
				columnsAndBlocks = append(columnsAndBlocks, ibl{index - len(columnChildren), boxOrList{list: columnChildren}})
			}
			columnsAndBlocks = append(columnsAndBlocks, ibl{index, boxOrList{box: child.Copy().(bo.BlockLevelBoxITF)}})
			columnChildren = nil
			continue
		}
		columnChildren = append(columnChildren, child.Copy())
	}
	if len(columnChildren) != 0 {
		columnsAndBlocks = append(columnsAndBlocks, ibl{len(box.Children) - len(columnChildren), boxOrList{list: columnChildren}})
	}

	// gopdfgo: wznowienie elementu rozpinającego (column-span: all) jest jego własnym stosem i trafia do niego
	// bez zmian. W źródle było przekazywane jak wznowienie treści kolumn: indeks dziecka elementu
	// rozpinającego był brany za przesunięcie w liście dzieci kontenera.
	var spannerSkip, spannerResume tree.ResumeStack
	if skipStack != nil {
		if skip >= 0 && skip < len(box.Children) && bo.IsColumnSpanner(box.Children[skip]) {
			spannerSkip, skipStack = skipStack[skip], nil
		} else {
			skipStack = tree.ResumeStack{0: skipStack[skip]}
		}
	}

	var nextPage tree.PageBreak
	if len(box.Children) == 0 {
		nextPage = tree.PageBreak{Break: "any"}
		skipStack = nil
	}

	// Find height and balance.
	//
	// The current algorithm starts from the total available height, to check
	// whether the whole content can fit. If it doesn’t fit, we keep the partial
	// rendering. If it fits, we try to balance the columns starting from the
	// ideal height (the total height divided by the number of columns). We then
	// iterate until the last column is not the highest one. At the end of each
	// loop, we add the minimal height needed to make one direct child at the
	// top of one column go to the end of the previous column.
	//
	// We rely on a real rendering for each loop, and with a stupid algorithm
	// like this it can last minutes…

	adjoiningMargins = nil
	var (
		currentPositionY    = box.ContentBoxY()
		newChildren         []Box
		columnSkipStack     tree.ResumeStack
		lastLoop            = false
		breakPage           = false
		lastFootnotesHeight pr.Float
		footnoteAreaHeights = []pr.Float{0}
		index               int
	)
	if h := context.currentFootnoteArea.Height; h != pr.AutoF {
		footnoteAreaHeights = []pr.Float{context.currentFootnoteArea.MarginHeight()}
	}

	for _, pair := range columnsAndBlocks {
		index = pair.index
		columnChildrenOrBlock := pair.bl
		if block := columnChildrenOrBlock.box; block != nil {
			// We have a spanning block, we display it like other blocks.
			// gopdfgo: blokiem zawierającym element rozpinający jest kontener kolumn (CSS Multicol 1
			// § 6.1), nie blok zawierający kontenera; w źródle element miał szerokość bloku nadrzędnego.
			resolveSizesBox(context, block, box)
			block.Box().PositionX = box.ContentBoxX()
			block.Box().PositionY = currentPositionY
			newChild, tmp, _ := blockLevelLayout(context, block, originalBottomSpace, spannerSkip,
				box, pageIsEmpty, absoluteBoxes, fixedBoxes, &adjoiningMargins, false, -1)
			nextPage, adjoiningMargins = tmp.nextPage, tmp.adjoiningMargins
			skipStack, spannerSkip = nil, nil
			if newChild == nil {
				lastLoop = true
				breakPage = true
				break
			}
			newChildren = append(newChildren, newChild)
			currentPositionY = newChild.Box().BorderHeight() + newChild.Box().BorderBoxY()
			adjoiningMargins = append(adjoiningMargins, newChild.Box().MarginBottom.V())
			if tmp.resumeAt != nil {
				lastLoop = true
				breakPage = true
				spannerResume = tmp.resumeAt
				break
			}
			pageIsEmpty = false
			continue
		}

		// We have a list of children that we have to balance between columns.
		columnChildren := columnChildrenOrBlock.list

		// Find the total height available for the first run
		currentPositionY += collapseMargin(adjoiningMargins)
		adjoiningMargins = nil
		columnBox := createColumnBox(box_, containingBlock, columnChildren, width, currentPositionY)
		columnBox.Box().AnonymousBaseHeight = percentBase
		height := context.pageBottom - currentPositionY - originalBottomSpace
		// gopdfgo: kolumny nie są wyższe niż to, co zostało z podanej wysokości kontenera; w źródle
		// pierwsza próba i równoważenie liczyły do końca strony.
		if limited := context.pageBottom - currentPositionY - definedBottomSpace; heightDefined && limited > 0 {
			height = limited
		}
		maxHeight := height

		// Try to render columns until the content fits, increase the column
		// height step by step
		columnSkipStack = skipStack
		lostSpace := pr.Inf
		originalExcludedShapes := append([]*bo.BoxFields(nil), context.excludedShapes.list...) // copy
		originalPageIsEmpty := pageIsEmpty
		pageIsEmpty = false
		stopRendering, balancing := false, false
		balancedHeight := false
		for {
			// Remove extra excluded shapes introduced during the previous loop
			context.excludedShapes.list = (context.excludedShapes.list)[:len(originalExcludedShapes)]

			// Render the columns
			columnSkipStack = skipStack
			var (
				consumedHeights []pr.Float
				newBoxes        []Box
				trailingMargin  pr.Float
			)
			for i := 0; i < count; i += 1 {
				// Render one column
				newBox, tmp := blockBoxLayout(context, columnBox, context.pageBottom-currentPositionY-height,
					columnSkipStack, containingBlock, pageIsEmpty || !balancing, new([]*AbsolutePlaceholder), new([]*AbsolutePlaceholder), new([]pr.Float),
					false, -1)
				resumeAt := tmp.resumeAt
				nextPage = tmp.nextPage

				if traceMode {
					traceLogger.Dump(fmt.Sprintf("column %d -> %s %v", i, resumeAt, nextPage))
				}

				if newBox == nil {
					// We didn't render anything, retry
					columnSkipStack = tree.ResumeStack{0: nil}
					break
				}
				newBoxes = append(newBoxes, newBox)
				columnSkipStack = resumeAt

				// Calculate consumed height, empty space and next box height
				var lastInFlowChildren *bo.BoxFields
				var lastInFlowBox Box
				for _, child := range newBox.Box().Children {
					if ch := child.Box(); ch.IsInNormalFlow() {
						lastInFlowChildren, lastInFlowBox = ch, child
					}
				}
				// czy kolumna kończy się w środku pudełka (dalszy ciąg w następnej kolumnie)
				var continued tree.ResumeStack
				if resumeAt != nil {
					_, continued = resumeAt.Unpack()
				}

				var consumedHeight, emptySpace, nextBoxHeight pr.Float
				if lastInFlowChildren != nil {
					// Get the empty space at the bottom of the column box
					consumedHeight = lastInFlowChildren.MarginHeight() + lastInFlowChildren.PositionY - currentPositionY
					emptySpace = height - consumedHeight
					consumedHeight -= lastInFlowChildren.MarginBottom.V()
					trailingMargin += max(0, lastInFlowChildren.MarginBottom.V())
					// gopdfgo: pudełko podzielone w środku jest rozciągane do dołu kolumny, więc jego
					// wysokość nie mówi, ile treści się zmieściło; liczymy do końca ostatniego wiersza.
					// W źródle taka kolumna uchodziła za zapełnioną i zawyżała wysokość równoważenia.
					if continued != nil {
						consumedHeight = fragmentBottom(lastInFlowBox, continued) - currentPositionY
						emptySpace = height - consumedHeight
					}

					// Get the minimum size needed to render the next box
					if columnSkipStack != nil {
						// Gdy następnego pudełka nie wolno oddzielić od kolejnego, układamy resztę treści
						// bez ograniczenia wysokości, żeby poznać wysokość całej grupy.
						groupSpace := pr.Inf
						// Tylko gdy kolumna zaczyna się od całego pudełka: przy dalszym ciągu pudełka
						// podzielonego wcześniej zostaje krok źródła.
						if top, inner := columnSkipStack.Unpack(); inner == nil && top >= 0 && top+1 < len(columnChildren) &&
							columnChildren[top].Box().IsInNormalFlow() && columnChildren[top+1].Box().IsInNormalFlow() &&
							avoidPageBreak(blockLevelPageBreak(columnChildren[top], columnChildren[top+1]), context) {
							groupSpace = -pr.Inf
						}
						nextBox, _ := blockBoxLayout(context, columnBox, groupSpace,
							columnSkipStack, containingBlock, true, new([]*AbsolutePlaceholder), new([]*AbsolutePlaceholder), new([]pr.Float),
							false, -1)
						var nextChildren []Box
						if nextBox != nil { // gopdfgo: układ może nie zwrócić pudełka; w źródle panika
							nextChildren = nextBox.Box().Children
						}
						// gopdfgo: liczy się cała grupa pudełek, których nie wolno rozdzielić (break-before
						// albo break-after: avoid); w źródle tylko pierwsze pudełko, więc kolumny rosły
						// o zbyt mały krok i równoważenie kończyło się za wysoko.
						// Ostatnie pudełko grupy liczy się tylko do pierwszego miejsca, w którym samo może
						// się podzielić: krok za duży dałby kolumny wyższe, niż trzeba.
						var previous Box
						for _, child := range nextChildren {
							if !child.Box().IsInNormalFlow() {
								continue
							}
							if previous != nil {
								if !avoidPageBreak(blockLevelPageBreak(previous, child), context) {
									break
								}
								nextBoxHeight += previous.Box().MarginHeight()
							}
							previous = child
						}
						if previous != nil {
							if nextBoxHeight == 0 && continued == nil {
								nextBoxHeight = previous.Box().MarginHeight()
							} else {
								// także dalszy ciąg podzielonego pudełka potrzebuje tylko następnego wiersza
								nextBoxHeight += firstPieceHeight(previous)
							}
						}
						if nextBox != nil {
							removePlaceholders(context, []bo.Box{nextBox}, new([]*AbsolutePlaceholder), new([]*AbsolutePlaceholder))
						}
					}
				}
				// else
				// consumedHeight = 0
				// nextBoxHeight = 0
				// emptySpace = 0

				consumedHeights = append(consumedHeights, consumedHeight)

				// Append the size needed to render the next box in this
				// column.
				//
				// The next box size may be smaller than the empty space, for
				// example when the next box can't be separated from its own
				// next box. In this case we don't try to find the real value
				// and let the workaround below fix this for us.
				//
				// We also want to avoid very small values that may have been
				// introduced by rounding errors. As the workaround below at
				// least adds 1 pixel for each loop, we can ignore lost spaces
				// lower than 1px.
				if nextBoxHeight-emptySpace > 1 {
					lostSpace = min(lostSpace, nextBoxHeight-emptySpace)
				}

				// Stop if we already rendered the whole content
				if resumeAt == nil {
					break
				}
			}

			// Remove placeholders but keep the current footnote area height
			lastFootnotesHeight = 0
			if h := context.currentFootnoteArea.Height; h != pr.AutoF {
				lastFootnotesHeight = context.currentFootnoteArea.MarginHeight()
			}
			removePlaceholders(context, newBoxes, new([]*AbsolutePlaceholder), new([]*AbsolutePlaceholder))

			if lastLoop {
				break
			}

			if balancing {
				if columnSkipStack == nil {
					// We rendered the whole content, stop
					break
				}

				// Increase the column heights and render them again
				addHeight := lostSpace
				if lostSpace == pr.Inf {
					addHeight = 1
				}
				height += addHeight

				if height > maxHeight {
					// We reached max height, stop rendering
					height = maxHeight
					stopRendering = true
					break
				}
			} else {
				L := len(footnoteAreaHeights)
				if !isInFloats(lastFootnotesHeight, footnoteAreaHeights) {
					// Footnotes have been rendered, try to re-render with the
					// new footnote area height
					height -= lastFootnotesHeight - footnoteAreaHeights[L-1]
					footnoteAreaHeights = append(footnoteAreaHeights, lastFootnotesHeight)
					continue
				}
				everythingFits := (columnSkipStack == nil && pr.Maxs(consumedHeights...) <= maxHeight)
				if everythingFits {
					// Everything fits, start expanding columns at the average
					// of the column heights
					maxHeight -= lastFootnotesHeight
					if style.GetColumnFill() == "balance" || index < columnsAndBlocks[len(columnsAndBlocks)-1].index {
						balancing = true
						// gopdfgo: do wysokości wyjściowej liczy się też dolny margines ostatniego pudełka
						// (cała wysokość treści podzielona przez liczbę kolumn, jak w przeglądarkach);
						// w źródle był pomijany, a kontener i tak go potem doliczał.
						height = (sum(consumedHeights) + trailingMargin) / pr.Float(count)
						balancedHeight = true
					} else {
						break
					}
				} else {
					// Content overflows even at maximum height, stop now and
					// let the columns continue on the next page
					height += footnoteAreaHeights[L-1]
					if L > 2 {
						lastFootnotesHeight = min(lastFootnotesHeight, footnoteAreaHeights[L-1])
					}
					height -= lastFootnotesHeight
					stopRendering = true
					break
				}
			}
		}

		bottomSpace = max(bottomSpace, context.pageBottom-currentPositionY-height)

		// Replace the current box children with real columns
		i := 0
		var maxColumnHeight, maxContentHeight, forcedHeight pr.Float
		var columns []Box
		// wysokość treści kolumny bez dolnego marginesu ostatniego pudełka
		contentHeight := func(column Box, keepMargin bool) pr.Float {
			var last *bo.BoxFields
			for _, child := range column.Box().Children {
				if ch := child.Box(); ch.IsInNormalFlow() {
					last = ch
				}
			}
			if last == nil {
				return column.Box().MarginHeight()
			}
			withMargin := last.PositionY + last.MarginHeight() - currentPositionY
			if full := column.Box().MarginHeight(); full > withMargin+0.01 {
				// wysokość kolumny wyznaczają elementy pływające, nie ostatnie pudełko
				return full
			}
			if keepMargin {
				return withMargin
			}
			return withMargin - max(0, last.MarginBottom.V())
		}
		for {
			i_ := pr.Float(i)

			columnBox = createColumnBox(box_, containingBlock, columnChildren, width, currentPositionY)
			columnBox.Box().AnonymousBaseHeight = percentBase
			if style.GetDirection() == pr.Rtl {
				columnBox.Box().PositionX += box.Width.V() - (i_+1)*width - i_*gap
			} else {
				columnBox.Box().PositionX += i_ * (width + gap)
			}
			// gopdfgo: każda kolejna kolumna jest pustym fragmentem, więc treść, która się w niej nie
			// mieści (np. float wyższy niż kolumna), musi zostać w niej umieszczona; w źródle wszystkie
			// kolumny dziedziczyły stan pierwszej i taka treść była przenosiła dalej bez końca.
			newChild, tmp := blockBoxLayout(context, columnBox, bottomSpace, skipStack,
				containingBlock, originalPageIsEmpty || i > 0, absoluteBoxes, fixedBoxes, new([]pr.Float),
				false, -1)
			columnSkipStack = tmp.resumeAt
			columnNextPage := tmp.nextPage
			// Bezpiecznik: kolumna, która nie posunęła treści naprzód, kończy tworzenie kolumn.
			if newChild != nil && columnSkipStack != nil && reflect.DeepEqual(columnSkipStack, skipStack) {
				columns = append(columns, newChild)
				maxColumnHeight = max(maxColumnHeight, newChild.Box().MarginHeight())
				maxContentHeight = max(maxContentHeight, contentHeight(newChild, false))
				skipStack = nil
				bottomSpace = originalBottomSpace
				break
			}

			if traceMode {
				traceLogger.Dump(fmt.Sprintf("column %d -> %s %v", i, columnSkipStack, tmp.nextPage))
			}

			if newChild == nil {
				breakPage = true
				break
			}
			nextPage = columnNextPage
			skipStack = columnSkipStack
			columns = append(columns, newChild)
			maxColumnHeight = max(maxColumnHeight, newChild.Box().MarginHeight())
			// przed wymuszonym podziałem dolny margines zostaje (CSS Break 3 § 5.2)
			forced := columnSkipStack != nil && forcePageBreak(string(columnNextPage.Break), context)
			maxContentHeight = max(maxContentHeight, contentHeight(newChild, false))
			if forced {
				forcedHeight = max(forcedHeight, contentHeight(newChild, true))
			}
			if skipStack == nil {
				bottomSpace = originalBottomSpace
				break
			}
			i += 1
			if i == count && !heightDefined {
				// [If] a declaration that constrains the column height
				// (e.g., using height || max-height). In this case,
				// additional column boxes are created in the inline
				// direction.
				break
			}
		}

		// gopdfgo: kolumny zrównoważone mają wysokość z równoważenia; dolny margines ostatniego pudełka,
		// który się w niej nie mieści, jest obcinany jak przy podziale (CSS Break 3 § 5.2). W źródle
		// kontener rósł o ten margines.
		if balancedHeight && !stopRendering && !breakPage {
			maxColumnHeight = max(min(maxColumnHeight, max(height, maxContentHeight)), min(forcedHeight, maxHeight))
		}

		// wysokość zużyta przez fragmenty przodków podzielonych elementem rozpinającym
		for _, column := range columns {
			addSpannerSplitUsed(column.Box().Children)
		}

		// Update the current y position and set the columns’ height
		currentPositionY += min(maxHeight, maxColumnHeight)
		for _, column := range columns {
			column.Box().Height = maxColumnHeight
			newChildren = append(newChildren, column)
		}

		skipStack = nil
		pageIsEmpty = false

		if stopRendering {
			break
		}
	}

	// Report footnotes above the defined footnotes height
	reportFootnotes(context, lastFootnotesHeight)

	if len(box.Children) != 0 && len(newChildren) == 0 {
		// The box has children but none can be drawn, let's skip the whole box
		context.inColumn = wasInColumn

		if traceMode {
			traceLogger.Dump("columnLayout: exit early")
		}

		return nil, blockLayout{resumeAt: tree.ResumeStack{0: nil}, nextPage: tree.PageBreak{Break: "any"}}
	}

	// Set the height of box and the containing box
	box.Children = newChildren
	currentPositionY += collapseMargin(adjoiningMargins)
	height := currentPositionY - box.ContentBoxY()
	fragmentedFixed := false
	if hasFixedHeight && (spannerResume != nil || columnSkipStack != nil || breakPage) && originalBottomSpace > -pr.Inf {
		if available := context.pageBottom - originalBottomSpace - box.ContentBoxY(); available > 0 && available < fixedHeight {
			box.Height = available
			fragmentedFixed = true
		}
	}
	var heightDifference pr.Float
	if box.Height == pr.AutoF {
		box.Height = height
		heightDifference = 0
	} else {
		heightDifference = box.Height.V() - height
	}

	// Update the latest columns’ height to respect min-height
	if box.MinHeight != pr.AutoF && box.MinHeight.V() > box.Height.V() {
		heightDifference += box.MinHeight.V() - box.Height.V()
		box.Height = box.MinHeight
	}
	for _, child := range reversedBoxes(newChildren) {
		if child.Box().IsColumn {
			child.Box().Height = child.Box().Height.V() + heightDifference
		} else {
			break
		}
	}

	if style.GetPosition().String == "relative" {
		// New containing block, resolve the layout of the absolute descendants
		for _, absoluteBox := range *absoluteBoxes {
			absoluteLayout(context, absoluteBox, box_, fixedBoxes, bottomSpace, nil)
		}
	}

	// Calculate skip stack
	if spannerResume != nil {
		skipStack = tree.ResumeStack{index: spannerResume}
	} else if columnSkipStack != nil {
		skip, _ = columnSkipStack.Unpack()
		skipStack = tree.ResumeStack{index + skip: columnSkipStack[skip]}
	} else if breakPage {
		skipStack = tree.ResumeStack{index: nil}
	}
	if fragmentedFixed && skipStack != nil {
		if context.fixedHeightRemaining == nil {
			context.fixedHeightRemaining = map[uintptr]pr.Float{}
		}
		context.fixedHeightRemaining[resumeKey(skipStack)] = fixedHeight - box.Height.V()
	}

	// Update page bottom according to the new footnotes
	if context.currentFootnoteArea.Height != pr.AutoF {
		context.pageBottom += footnoteAreaHeights[0]
		context.pageBottom -= context.currentFootnoteArea.MarginHeight()
	}

	context.inColumn = wasInColumn

	if traceMode {
		traceLogger.Dump(fmt.Sprintf("columnsLayout -> %s, %v", skipStack, nextPage))
	}

	return box_, blockLayout{resumeAt: skipStack, nextPage: nextPage}
}

// Report footnotes above the defined footnotes height.
func reportFootnotes(context *layoutContext, footnotesHeight pr.Float) {
	if len(context.currentPageFootnotes) == 0 {
		return
	}
	// Report and count footnotes
	reportedFootnotes := 0
	for context.currentFootnoteArea.MarginHeight() > footnotesHeight {
		context.reportFootnote(context.currentPageFootnotes[len(context.currentPageFootnotes)-1])
		reportedFootnotes += 1
	}
	// Revert reported footnotes, as they’ve been reported starting from the
	// last one
	if reportedFootnotes >= 2 {
		slices.Reverse(context.reportedFootnotes[len(context.reportedFootnotes)-reportedFootnotes:])
	}
}

// Create a column box including given children.
func createColumnBox(box_ Box, containingBlock containingBlock, children []Box, width, positionY pr.Float) bo.BlockBoxITF {
	columnBox := box_.Type().AnonymousFrom(box_, children).(bo.BlockBoxITF) // AnonymousFrom preserves concrete types
	resolvePercentagesBox(columnBox, containingBlock)
	columnBox.Box().IsColumn = true
	columnBox.Box().Width = width
	columnBox.Box().PositionX = box_.Box().ContentBoxX()
	columnBox.Box().PositionY = positionY
	return columnBox
}

// mayHoldSpanner mówi, czy potomek kontenera kolumn należy do tego samego kontekstu formatowania
// blokowego, więc element z column-span: all w jego wnętrzu rozpina kolumny kontenera.
func mayHoldSpanner(child Box) bool {
	box := child.Box()
	if !bo.BlockT.IsInstance(child) || !box.IsInNormalFlow() || box.IsTableWrapper || box.IsColumn {
		return false
	}
	// przekształcenie też zatrzymuje element rozpinający (jak w przeglądarkach; test WPT
	// multicol-span-all-010)
	if bo.EstablishesFormattingContext(child) || len(box.Style.GetTransform()) != 0 {
		return false
	}
	return box.Style.GetColumnWidth().Tag == pr.Auto && box.Style.GetColumnCount().String == "auto"
}

// hoistSpanners wynosi elementy rozpinające kolumny z potomków na poziom podanej listy dzieci.
// Przodek zostaje podzielony na fragmenty przed elementem i po nim, bez obramowań w miejscu podziału.
func hoistSpanners(children []Box) ([]Box, bool) {
	var out []Box
	changed := false
	for i, child := range children {
		if bo.IsColumnSpanner(child) || !mayHoldSpanner(child) {
			if changed {
				out = append(out, child)
			}
			continue
		}
		inner, _ := hoistSpanners(child.Box().Children)
		hasSpanner := false
		for _, c := range inner {
			if bo.IsColumnSpanner(c) {
				hasSpanner = true
				break
			}
		}
		if !hasSpanner {
			if changed {
				out = append(out, child)
			}
			continue
		}
		if !changed {
			changed = true
			out = append(out, children[:i]...)
		}
		var run []Box
		first := true
		used := new(pr.Float)
		flush := func(last bool) {
			part := bo.CopyWithChildren(child, run)
			part.Box().SpannerSplitUsed = used
			part.Box().SpannerSplitBefore = !first
			part.Box().SpannerSplitAfter = !last
			out = append(out, part)
			run, first = nil, false
		}
		for _, c := range inner {
			if bo.IsColumnSpanner(c) {
				// między sąsiednimi elementami rozpinającymi nie ma fragmentu przodka
				if first || len(run) != 0 {
					flush(false)
				}
				out = append(out, c)
				continue
			}
			run = append(run, c)
		}
		flush(true)
	}
	if !changed {
		return children, false
	}
	return out, true
}

// firstPieceHeight podaje wysokość najmniejszego początkowego kawałka pudełka, po którym wolno je
// podzielić: pierwszy wiersz albo pierwsze pudełko bez dzieci, razem z górnymi odstępami przodków.
func firstPieceHeight(box_ Box) pr.Float {
	box := box_.Box()
	if bo.LineT.IsInstance(box_) {
		return box.Height.V()
	}
	for _, child := range box.Children {
		if child.Box().IsInNormalFlow() && (bo.LineT.IsInstance(child) || bo.BlockLevelT.IsInstance(child)) {
			return box.MarginTop.V() + box.BorderTopWidth.V() + box.PaddingTop.V() + firstPieceHeight(child)
		}
	}
	return box.MarginHeight()
}

// fragmentBottom podaje dolną krawędź treści pudełka podzielonego w miejscu opisanym przez resume:
// koniec ostatniego wiersza albo ostatniego całego pudełka, bez rozciągnięcia do dołu kolumny.
func fragmentBottom(box_ Box, resume tree.ResumeStack) pr.Float {
	box := box_.Box()
	full := box.PositionY + box.MarginHeight() - box.MarginBottom.V()
	if resume == nil || bo.LineT.IsInstance(box_) {
		return full
	}
	if _, ok := resume.Remainder(); ok {
		return full
	}
	if _, ok := resume.Overflow(); ok {
		return full
	}
	if _, _, ok := resume.Slice(); ok {
		return full
	}
	var last Box
	for _, child := range box.Children {
		if child.Box().IsInNormalFlow() && (bo.LineT.IsInstance(child) || bo.BlockLevelT.IsInstance(child)) {
			last = child
		}
	}
	if last == nil {
		return full
	}
	_, inner := resume.Unpack()
	return min(full, fragmentBottom(last, inner))
}

// resetSpannerSplitUsed zeruje liczniki wysokości fragmentów przodków (zob. BoxFields.SpannerSplitUsed)
// przed układem kontenera od początku.
func resetSpannerSplitUsed(children []Box) {
	for _, child := range children {
		if used := child.Box().SpannerSplitUsed; used != nil {
			*used = 0
			resetSpannerSplitUsed(child.Box().Children)
		}
	}
}

// addSpannerSplitUsed dolicza wysokości ułożonych fragmentów, po których stoi element rozpinający.
func addSpannerSplitUsed(children []Box) {
	for _, child := range children {
		box := child.Box()
		if box.SpannerSplitUsed == nil {
			continue
		}
		if height, ok := box.Height.(pr.Float); ok && box.SpannerSplitAfter {
			*box.SpannerSplitUsed += height
		}
		addSpannerSplitUsed(box.Children)
	}
}
