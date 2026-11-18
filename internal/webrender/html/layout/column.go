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
	// gopdfgo: przy box-decoration-break: clone każdy fragment kontenera ma własne dolne obramowanie
	// i dopełnienie (CSS Fragmentation 3 § 5.4), więc treść kolumn kończy się nad nimi; w źródle miejsce
	// na nie rezerwował tylko ostatni fragment (test WPT multicol-breaking-004).
	cloneDecoration := style.GetBoxDecorationBreak() == "clone"
	if cloneDecoration {
		bottomSpace += box_.Box().PaddingBottom.V() + box_.Box().BorderBottomWidth.V()
	}
	originalBottomSpace := bottomSpace

	wasInColumn := context.inColumn
	outerColumnsLeft := context.columnsLeft
	outerColumnX := context.columnX
	defer func() { context.columnX = outerColumnX }()
	defer func() { context.columnsLeft = outerColumnsLeft }()
	// gopdfgo: kolumna po pierwszej zaczyna się za podziałem kolumny, więc przylegające do niego marginesy
	// są obcinane (CSS Fragmentation 3 § 5.2); w źródle tylko na stronach po pierwszej. Pierwsza kolumna
	// dziedziczy stan fragmentu, w którym stoi kontener.
	outerColumnBreak := context.columnBreak
	defer func() { context.columnBreak = outerColumnBreak }()
	columnBreakAfter := func(previous tree.PageBreak) columnBreakKind {
		if forcePageBreak(string(previous.Break), context) {
			return columnBreakForced
		}
		return columnBreakUnforced
	}
	// gopdfgo: tryb ciągły bez zewnętrznego kontenera kolumn: treść nie ma dokąd przejść, więc nadmiar
	// zostaje w tym kontenerze. Kontener zagnieżdżony w kolumnie dzieli się między kolumny zewnętrzne.
	unpaged := context.behavior.Continuous && !wasInColumn
	context.inColumn = true

	// gopdfgo: kontener kolumn z transform jest blokiem zawierającym także dla potomków fixed
	// (CSS Transforms 1 § 3; zob. ownsFixedDescendants)
	ownsFixed := ownsFixedDescendants(box_)
	if style.GetPosition().String == "relative" || ownsFixed {
		// New containing block, use a new absolute list
		absoluteBoxes = &[]*AbsolutePlaceholder{}
	}
	if ownsFixed {
		fixedBoxes = &[]*AbsolutePlaceholder{}
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
	// gopdfgo: dalszy ciąg kontenera podzielonego między strony albo kolumny zewnętrzne nie ma górnego
	// marginesu, obramowania ani dopełnienia (CSS Fragmentation 3 § 5.4, box-decoration-break: slice);
	// w źródle każdy fragment kontenera kolumn je zachowywał.
	if skipStack != nil {
		box_.RemoveDecoration(box, true, false)
		// gopdfgo: min-height dotyczy sumy fragmentów (CSS Fragmentation 3 § 5.1): dalszy ciąg dostaje
		// to, czego nie zużyły poprzednie fragmenty. W źródle każdy fragment miał całe min-height.
		if remaining, ok := context.minHeightRemaining[resumeKey(skipStack)]; ok {
			box.MinHeight = remaining
		}
	}
	// gopdfgo: dalszy ciąg kontenera o podanej wysokości, którego treść skończyła się we wcześniejszym
	// fragmencie: zostaje sama reszta wysokości, dzielona dalej, jeśli się nie mieści (CSS Fragmentation 3
	// § 5.1; testy WPT multicol-breaking-*). W źródle takiego wznowienia nie było, zob. niżej.
	if remaining, ok := skipStack.Remainder(); ok {
		box.Children = nil
		if !box.IsAbsolutelyPositioned() {
			blockLevelWidth(box_, nil, containingBlock)
		}
		height := pr.Float(remaining)
		var resume tree.ResumeStack
		if available := context.pageBottom - originalBottomSpace - box.ContentBoxY(); originalBottomSpace > -pr.Inf &&
			available > 0 && height > available+0.5 {
			resume = tree.RemainderResume(float64(height - available))
			height = available
			box_.RemoveDecoration(box, false, true)
		}
		box.Height = height
		context.inColumn = wasInColumn
		return box_, blockLayout{resumeAt: resume, nextPage: tree.PageBreak{Break: "any"}}
	}
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
	// gopdfgo: wysokość nadana z zewnątrz (rozciągnięcie elementu flex albo siatki) ogranicza kolumny tak
	// samo jak podana wysokość (CSS Multicol 1 § 8.2); w źródle liczyła się tylko właściwość height.
	if forced, ok := box.Height.(pr.Float); ok && height_.Tag == pr.Auto && skipStack == nil && (box.IsFlexItem || box.IsGridItem) {
		height_ = pr.TaggedDim{Dimension: pr.Dimension{Value: forced, Unit: pr.Px}}
	}
	if height_.Tag != pr.Auto && height_.Unit != pr.Perc {
		if height_.Unit != pr.Px {
			panic(fmt.Sprintf("expected Px got %v", height_))
		}
		fixedHeight, hasFixedHeight = height_.Value, true
		// gopdfgo: przy box-sizing: border-box wysokość treści jest mniejsza od wartości height; bierzemy
		// wysokość treści wyliczoną już dla pudełka (CSS Box Sizing 3 § 4). W źródle wartość height.
		if contentHeight, ok := box.Height.(pr.Float); ok && skipStack == nil {
			fixedHeight = contentHeight
		}
		// gopdfgo: wysokością kolumn jest wysokość używana, ograniczona przez min-height i max-height
		// (CSS 2 § 10.7); w źródle wartość height bez ograniczeń.
		if maxHeight, ok := box.MaxHeight.(pr.Float); ok {
			fixedHeight = min(fixedHeight, maxHeight)
		}
		if minHeight, ok := box.MinHeight.(pr.Float); ok {
			fixedHeight = max(fixedHeight, minHeight)
		}
		if skipStack == nil {
			box.Height = fixedHeight
		}
		if remaining, ok := context.fixedHeightRemaining[resumeKey(skipStack)]; ok && skipStack != nil {
			fixedHeight = remaining
			box.Height = remaining
		}
		height_.Value = fixedHeight
		// gopdfgo: kolumna o zerowej wysokości musi przesuwać treść naprzód (CSS Break 3 § 4.4), więc jak
		// w przeglądarkach ma co najmniej 1px; w źródle przy height: 0 treść stała w jednej kolumnie
		// (testy WPT multicol-zero-height-*).
		emptySpace := context.pageBottom - box.ContentBoxY() - max(height_.Value, 1)
		// gopdfgo: dodatkowe kolumny w kierunku wiersza powstają tylko wtedy, gdy treść ogranicza
		// wysokość kontenera; gdy wcześniej kończy się strona, dalsza treść przechodzi na następną
		// stronę (CSS Multicol 1 § 8.2). W źródle podana wysokość zawsze dawała dodatkowe kolumny.
		heightDefined = emptySpace >= bottomSpace
		bottomSpace = max(bottomSpace, emptySpace)
	} else if maxHeight, ok := box.MaxHeight.(pr.Float); ok && maxHeight != pr.Inf && skipStack == nil {
		// gopdfgo: max-height też ogranicza wysokość kolumn (CSS Multicol 1 § 8.2: „height || max-height”),
		// a nadmiar treści tworzy dodatkowe kolumny w kierunku wiersza; w źródle max-height był pomijany.
		if minHeight, ok := box.MinHeight.(pr.Float); ok {
			maxHeight = max(maxHeight, minHeight)
		}
		emptySpace := context.pageBottom - box.ContentBoxY() - maxHeight
		heightDefined = emptySpace >= bottomSpace
		bottomSpace = max(bottomSpace, emptySpace)
	}

	definedBottomSpace := bottomSpace

	// gopdfgo: szerokość pudełka pozycjonowanego bezwzględnie wyznaczył już jego układ (CSS 2 § 10.3.7)
	if !box.IsAbsolutelyPositioned() {
		blockLevelWidth(box_, nil, containingBlock)
	}

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
	// gopdfgo: column-width: 0 jest poprawne (CSS Multicol 1 § 3.1), ale wzór z § 3.4 dzieliłby wtedy
	// przez zero przy zerowym odstępie; jak w przeglądarkach przyjmujemy szerokość co najmniej 1px.
	// W źródle liczba kolumn wychodziła nieskończona i treść znikała.
	if width_.Tag != pr.Auto && width_.Value < 1 {
		width_.Value = 1
	}
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
			spanner := child.Copy().(bo.BlockLevelBoxITF)
			spanner.Box().IsSpanner = true
			columnsAndBlocks = append(columnsAndBlocks, ibl{index, boxOrList{box: spanner}})
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
	var spannerSkip, spannerResume, afterSpannerResume tree.ResumeStack
	forcedOuterBreak := func(value string) bool {
		if value == "column" {
			return wasInColumn
		}
		return !context.behavior.Continuous && forcePageBreak(value, context)
	}
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
		context.columnBreak = outerColumnBreak
		index = pair.index
		columnChildrenOrBlock := pair.bl
		if block := columnChildrenOrBlock.box; block != nil {
			// We have a spanning block, we display it like other blocks.
			// gopdfgo: blokiem zawierającym element rozpinający jest kontener kolumn (CSS Multicol 1
			// § 6.1), nie blok zawierający kontenera; w źródle element miał szerokość bloku nadrzędnego.
			resolveSizesBox(context, block, box)
			block.Box().PositionX = box.ContentBoxX()
			block.Box().PositionY = currentPositionY
			// gopdfgo: wymuszony podział przed elementem rozpinającym albo za nim dzieli fragmentator
			// zewnętrzny: kontener kolumn kończy się tu i ciągnie dalej w następnej kolumnie zewnętrznej
			// albo na następnej stronie (CSS Multicol 1 § 6.1, CSS Break 3 § 3.1; testy WPT
			// spanner-fragmentation-010, -012). W źródle te podziały były pomijane.
			if before := string(block.Box().Style.GetBreakBefore()); len(newChildren) != 0 && spannerSkip == nil && forcedOuterBreak(before) {
				nextPage = tree.PageBreak{Break: before}
				lastLoop, breakPage = true, true
				break
			}
			// marginesy przodków przylegające do marginesów elementu (zob. hoistSpanners)
			ancestorMargin := func(styles []pr.ElementStyle, top bool) (out []pr.Float) {
				for _, s := range styles {
					value := s.GetMarginBottom()
					if top {
						value = s.GetMarginTop()
					}
					if margin, ok := pr.ResolvePercentage(value, box.Width.V()).(pr.Float); ok {
						out = append(out, margin)
					}
				}
				return out
			}
			if spannerSkip == nil {
				adjoiningMargins = append(adjoiningMargins, ancestorMargin(block.Box().SpannerMarginsBefore, true)...)
			}
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
			if tmp.resumeAt == nil {
				adjoiningMargins = append(adjoiningMargins, ancestorMargin(block.Box().SpannerMarginsAfter, false)...)
			}
			if tmp.resumeAt != nil {
				lastLoop = true
				breakPage = true
				spannerResume = tmp.resumeAt
				break
			}
			if after := string(block.Box().Style.GetBreakAfter()); index+1 < len(box.Children) && forcedOuterBreak(after) {
				nextPage = tree.PageBreak{Break: after}
				lastLoop = true
				afterSpannerResume = tree.ResumeStack{index + 1: nil}
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
				// gopdfgo: element pływający z break-inside: avoid podzielony między kolumny przy
				// równoważeniu (zob. niżej)
				brokenAvoid bool
			)
			floatMark := context.brokenSequence
			for i := 0; i < count; i += 1 {
				// Render one column
				context.columnBreak = outerColumnBreak
				if i > 0 {
					context.columnBreak = columnBreakAfter(nextPage)
					takeColumnFloats(context, floatMark, columnBox.Box().PositionX)
				}
				floatMark = context.brokenSequence
				context.columnX = columnBox.Box().PositionX
				context.columnsLeft = count - 1 - i
				newBox, tmp := blockBoxLayout(context, columnBox, context.pageBottom-currentPositionY-height,
					columnSkipStack, containingBlock, pageIsEmpty || !balancing, new([]*AbsolutePlaceholder), new([]*AbsolutePlaceholder), new([]pr.Float),
					false, -1)
				context.columnFloats = nil
				resumeAt := tmp.resumeAt
				nextPage = tmp.nextPage
				if resumeAt == nil && hasColumnFloats(context, floatMark) {
					resumeAt = floatsOnlyResume(columnChildren)
				}

				if traceMode {
					traceLogger.Dump(fmt.Sprintf("column %d -> %s %v", i, resumeAt, nextPage))
				}

				if newBox == nil && i > 0 && balancing {
					// gopdfgo: pusta kolumna nie pomieściła nawet pierwszego pudełka (np. niepodzielnego),
					// więc kolumny muszą urosnąć co najmniej do jego wysokości (CSS Multicol 1 § 7.1,
					// CSS Break 3 § 4.1). W źródle krok liczono tylko z poprzedniej kolumny i wychodził
					// za duży (test WPT multicol-overflow-clip-auto-sized).
					if first, _ := blockBoxLayout(context, columnBox, pr.Inf, columnSkipStack, containingBlock, true,
						new([]*AbsolutePlaceholder), new([]*AbsolutePlaceholder), new([]pr.Float), false, -1); first != nil {
						for _, child := range first.Box().Children {
							if ch := child.Box(); ch.IsInNormalFlow() {
								if need := ch.PositionY + ch.MarginHeight() - currentPositionY - height; need > 0 {
									lostSpace = min(lostSpace, need)
								}
								break
							}
						}
						removePlaceholders(context, []bo.Box{first}, new([]*AbsolutePlaceholder), new([]*AbsolutePlaceholder))
					}
					context.columnFloats = nil
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
					if ch := child.Box(); ch.IsInNormalFlow() && !isEmptyColumnLine(child) {
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
						context.columnBreak = columnBreakAfter(nextPage)
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

				// gopdfgo: kolumna jest korzeniem kontekstu formatowania, więc jej treść obejmuje też elementy
				// pływające (CSS 2 § 10.6.7); w źródle równoważenie liczyło tylko pudełka w przepływie.
				for _, child := range newBox.Box().Children {
					if ch := child.Box(); ch.IsFloated() {
						consumedHeight = max(consumedHeight, ch.PositionY+ch.MarginHeight()-currentPositionY)
					}
				}
				consumedHeights = append(consumedHeights, consumedHeight)

				// gopdfgo: równoważenie nie może kończyć się podziałem elementu pływającego z break-inside:
				// avoid (CSS Multicol 1 § 7.1, CSS Break 3 § 4.1: najpierw unikamy podziałów, których
				// unikać należy); kolumny rosną co najmniej do jego pełnej wysokości (test WPT
				// multicol-fill-balance-032). W źródle taki element dzielono, a równoważenie uznawało, że
				// cała treść się zmieściła.
				if balancing {
					for _, v := range context.brokenOutOfFlow {
						if v.order <= floatMark || v.resumeAt == nil || !v.box.Box().IsFloated() ||
							!avoidPageBreak(string(v.box.Box().Style.GetBreakInside()), context) {
							continue
						}
						shapes := len(context.excludedShapes.list)
						full, _ := floatLayout(context, v.box, v.containingBlock.Box(), new([]*AbsolutePlaceholder),
							new([]*AbsolutePlaceholder), -pr.Inf, nil)
						context.excludedShapes.list = context.excludedShapes.list[:shapes]
						if full == nil {
							continue
						}
						if need := full.Box().MarginHeight() - height; need > 0 {
							lostSpace = min(lostSpace, need)
							brokenAvoid = true
						}
					}
				}

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
				if columnSkipStack == nil && !brokenAvoid {
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
					// gopdfgo: w medium ciągłym column-fill: auto działa tylko przy ograniczonej wysokości
					// kolumn; inaczej kolumny są równoważone (CSS Multicol 1 § 7.1)
					unconstrained := unpaged && !hasFixedHeight && (style.GetMaxHeight().Tag == pr.Auto || style.GetMaxHeight().Value == pr.Inf)
					if style.GetColumnFill() == "balance" || unconstrained || index < columnsAndBlocks[len(columnsAndBlocks)-1].index {
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
				if ch := child.Box(); ch.IsInNormalFlow() && !isEmptyColumnLine(child) {
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
		floatMark := context.brokenSequence
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
			// W trybie ciągłym nie ma następnej strony, więc treść zostaje także w pierwszej kolumnie.
			// gopdfgo: kontener zagnieżdżony w kolumnie, który nie zaczyna się u góry kolumny zewnętrznej,
			// ma dalsze miejsce w następnej kolumnie zewnętrznej: treść, która nie mieści się w kolejnej
			// kolumnie wewnętrznej, przechodzi tam, a reszta kolumn tego rzędu zostaje pusta (CSS Multicol 1
			// § 8.1, CSS Fragmentation 3 § 4.4). W źródle każda kolumna po pierwszej była zawsze pusta.
			deferrable := wasInColumn && !originalPageIsEmpty && !unpaged
			context.columnBreak = outerColumnBreak
			tookFloats := false
			if i > 0 {
				context.columnBreak = columnBreakAfter(nextPage)
				tookFloats = takeColumnFloats(context, floatMark, columnBox.Box().PositionX)
			}
			floatMark = context.brokenSequence
			context.columnX = columnBox.Box().PositionX
			context.columnsLeft = count - 1 - i
			newChild, tmp := blockBoxLayout(context, columnBox, bottomSpace, skipStack,
				containingBlock, originalPageIsEmpty || (i > 0 && !deferrable) || unpaged, absoluteBoxes, fixedBoxes, new([]pr.Float),
				false, -1)
			context.columnFloats = nil
			columnSkipStack = tmp.resumeAt
			if columnSkipStack == nil && newChild != nil && hasColumnFloats(context, floatMark) {
				columnSkipStack = floatsOnlyResume(columnChildren)
			}
			columnNextPage := tmp.nextPage
			if deferrable && i > 0 && !tookFloats && (newChild == nil || (columnSkipStack != nil && reflect.DeepEqual(columnSkipStack, skipStack))) {
				// żadna treść nie zmieściła się w tej kolumnie: dalszy ciąg w następnej kolumnie zewnętrznej
				columnSkipStack = skipStack
				break
			}
			// Bezpiecznik: kolumna, która nie posunęła treści naprzód, kończy tworzenie kolumn.
			if newChild != nil && columnSkipStack != nil && !tookFloats && reflect.DeepEqual(columnSkipStack, skipStack) {
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
			// gopdfgo: w trybie ciągłym nie ma następnej strony, więc także przy wysokości auto nadmiarowe
			// kolumny (po wymuszonych podziałach) powstają w osi wiersza (CSS Multicol 1 § 8.2)
			if i == count && !heightDefined && !unpaged {
				// [If] a declaration that constrains the column height
				// (e.g., using height || max-height). In this case,
				// additional column boxes are created in the inline
				// direction.
				break
			}
		}

		// dalszy ciąg elementów pływających, który nie zmieścił się w kolumnach, przechodzi na następną
		// stronę (pages.go)
		if isFloatsOnlyResume(columnSkipStack, columnChildren) {
			columnSkipStack = nil
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

	alignColumnsMarker(newChildren)

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
	// gopdfgo: kontener o podanej wysokości zagnieżdżony w kolumnach, którego treść już się skończyła, a który
	// sam nie mieści się w kolumnie zewnętrznej, dzieli się: reszta wysokości przechodzi do następnej
	// kolumny zewnętrznej (CSS Fragmentation 3 § 5.1). W źródle kontener wystawał z kolumny.
	var remainderResume tree.ResumeStack
	// Dzielimy tylko wtedy, gdy w rzędzie kolumn zewnętrznych zostaje następna kolumna: sama reszta
	// wysokości nie tworzy kolumn nadmiarowych (jak w przeglądarkach).
	if hasFixedHeight && wasInColumn && outerColumnsLeft > 0 && !unpaged && !fragmentedFixed && spannerResume == nil && columnSkipStack == nil &&
		!breakPage && originalBottomSpace > -pr.Inf {
		if available := context.pageBottom - originalBottomSpace - box.ContentBoxY(); available > 0 && fixedHeight > available+0.5 &&
			height <= available {
			remainderResume = tree.RemainderResume(float64(fixedHeight - available))
			box.Height = available
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
	fragmented := spannerResume != nil || columnSkipStack != nil || breakPage || afterSpannerResume != nil
	var minHeight pr.Float
	if box.MinHeight != pr.AutoF {
		minHeight = box.MinHeight.V()
	}
	usedMinHeight := minHeight
	// gopdfgo: fragment, po którym kontener ciągnie się dalej, nie wystaje z powodu min-height poza
	// fragmentator; reszta min-height przechodzi na dalszy ciąg (CSS Fragmentation 3 § 5.1)
	if fragmented && originalBottomSpace > -pr.Inf {
		if available := context.pageBottom - originalBottomSpace - box.ContentBoxY(); available < usedMinHeight {
			usedMinHeight = max(available, box.Height.V())
		}
	}
	if usedMinHeight > box.Height.V() {
		heightDifference += usedMinHeight - box.Height.V()
		box.Height = usedMinHeight
	}
	for _, child := range reversedBoxes(newChildren) {
		if child.Box().IsColumn {
			child.Box().Height = child.Box().Height.V() + heightDifference
		} else {
			break
		}
	}

	if style.GetPosition().String == "relative" || ownsFixed {
		// New containing block, resolve the layout of the absolute descendants
		layoutOwnedPositioned(context, box_, absoluteBoxes, fixedBoxes, ownsFixed, bottomSpace)
	}

	// Calculate skip stack
	if spannerResume != nil {
		skipStack = tree.ResumeStack{index: spannerResume}
	} else if columnSkipStack != nil {
		skip, _ = columnSkipStack.Unpack()
		skipStack = tree.ResumeStack{index + skip: columnSkipStack[skip]}
	} else if breakPage {
		skipStack = tree.ResumeStack{index: nil}
	} else if remainderResume != nil {
		skipStack = remainderResume
	} else if afterSpannerResume != nil {
		skipStack = afterSpannerResume
	}
	// gopdfgo: fragment, po którym kontener ciągnie się dalej, nie ma dolnego marginesu, obramowania ani
	// dopełnienia (CSS Fragmentation 3 § 5.4); w źródle je zachowywał.
	if skipStack != nil {
		box_.RemoveDecoration(box, false, true)
	}
	if fragmented && skipStack != nil && minHeight > 0 {
		if context.minHeightRemaining == nil {
			context.minHeightRemaining = map[uintptr]pr.Float{}
		}
		context.minHeightRemaining[resumeKey(skipStack)] = max(0, minHeight-box.Height.V())
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
		var lastSpanner Box
		var pendingBefore []pr.ElementStyle
		flush := func(last bool) {
			part := bo.CopyWithChildren(child, run)
			part.Box().SpannerSplitUsed = used
			part.Box().SpannerSplitBefore = !first
			part.Box().SpannerSplitAfter = !last
			// gopdfgo: margines przodka przylega w drzewie do marginesu elementu rozpinającego, gdy między
			// nimi nie ma treści, obramowania ani dopełnienia, i zwija się z nim (CSS 2 § 8.3.1); pusty
			// fragment przodka go wtedy nie ma (test WPT multicol-span-all-margin-nested-002). W źródle
			// pusty fragment zachowywał margines, który dodawał się do marginesu elementu.
			style := child.Box().Style
			if len(run) == 0 && first && !last && style.GetBorderTopWidth().Value == 0 && style.GetPaddingTop().Value == 0 {
				part.Box().Style = style.Copy()
				part.Box().Style.SetMarginTop(pr.ZeroPixels.Tagged())
				pendingBefore = append(pendingBefore, style)
			}
			if len(run) == 0 && !first && last && lastSpanner != nil && style.GetBorderBottomWidth().Value == 0 &&
				style.GetPaddingBottom().Value == 0 {
				part.Box().Style = style.Copy()
				part.Box().Style.SetMarginBottom(pr.ZeroPixels.Tagged())
				lastSpanner.Box().SpannerMarginsAfter = append(slices.Clip(lastSpanner.Box().SpannerMarginsAfter), style)
			}
			out = append(out, part)
			run, first = nil, false
		}
		for _, c := range inner {
			if bo.IsColumnSpanner(c) {
				// między sąsiednimi elementami rozpinającymi nie ma fragmentu przodka
				if first || len(run) != 0 {
					flush(false)
				}
				c = c.Copy()
				if len(pendingBefore) != 0 {
					c.Box().SpannerMarginsBefore = append(append([]pr.ElementStyle(nil), pendingBefore...), c.Box().SpannerMarginsBefore...)
					pendingBefore = nil
				}
				lastSpanner = c
				// gopdfgo: element rozpinający nadal należy do przodka, więc przezroczystość przodka
				// dotyczy także jego (CSS Color 4 § 13: opacity działa na całą grupę potomków). Grupa
				// przodka zawiera tu tylko element, więc wystarczy pomnożyć krycie (test WPT
				// spanner-in-opacity); bez tego wyniesiony element tracił krycie przodka.
				if opacity := child.Box().Style.GetOpacity(); opacity < 1 {
					c.Box().Style = c.Box().Style.Copy()
					c.Box().Style.SetOpacity(c.Box().Style.GetOpacity() * opacity)
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

// columnFloat to element pływający podzielony w kolumnie, którego dalszy ciąg trafia na górę następnej
// kolumny; x to położenie dalszego ciągu: położenie poprzedniego fragmentu przesunięte o odległość
// między kolumnami.
type columnFloat struct {
	brokenBox
	x pr.Float
}

// takeColumnFloats przenosi elementy pływające (i pudełka pozycjonowane bezwzględnie) podzielone po
// znaczniku after (zob. layoutContext.brokenSequence) do kontynuacji w następnej kolumnie, położonej
// w columnX, i mówi, czy jakieś były. Dalszy ciąg zachowuje odległość od lewej krawędzi kolumny, w której
// pudełko zostało podzielone; to może być kolumna zagnieżdżonego kontenera kolumn.
func takeColumnFloats(context *layoutContext, after int, columnX pr.Float) bool {
	context.columnFloats = nil
	for key, v := range context.brokenOutOfFlow {
		if v.order > after && continuesInColumn(v) {
			context.columnFloats = append(context.columnFloats, columnFloat{brokenBox: v, x: key.Box().PositionX - v.columnX + columnX})
			delete(context.brokenOutOfFlow, key)
		}
	}
	slices.SortFunc(context.columnFloats, func(a, b columnFloat) int { return a.order - b.order })
	return len(context.columnFloats) != 0
}

// hasColumnFloats mówi, czy po znaczniku after podzielono jakiś element pływający.
func hasColumnFloats(context *layoutContext, after int) bool {
	for _, v := range context.brokenOutOfFlow {
		if v.order > after && continuesInColumn(v) {
			return true
		}
	}
	return false
}

// continuesInColumn mówi, czy dalszy ciąg pudełka poza przepływem trafia do następnej kolumny: element
// pływający albo pudełko pozycjonowane bezwzględnie podzielone w kolumnie (gopdfgo: w źródle tylko na
// stronach).
func continuesInColumn(v brokenBox) bool {
	return v.deferredPages == 0 && (v.resumeAt != nil || v.nextColumn) && (v.box.Box().IsFloated() || v.box.Box().IsAbsolutelyPositioned())
}

// floatsOnlyResume to wznowienie kolumny, w której zostaje już tylko dalszy ciąg elementów pływających:
// wskazuje za ostatnie dziecko, więc treść w przepływie nie jest układana ponownie.
func floatsOnlyResume(children []Box) tree.ResumeStack {
	return tree.ResumeStack{len(children): nil}
}

func isFloatsOnlyResume(resume tree.ResumeStack, children []Box) bool {
	if len(resume) != 1 {
		return false
	}
	inner, ok := resume[len(children)]
	return ok && inner == nil
}

// isEmptyColumnLine mówi, czy dziecko kolumny jest pustym wierszem (np. po końcowym <br>), który nie
// zajmuje miejsca (CSS 2 § 9.4.2).
//
// gopdfgo: w źródle taki wiersz był brany za ostatnie pudełko kolumny, a jego marginesy zawyżały
// wysokość treści (test WPT multicol-breaking-000).
func isEmptyColumnLine(child Box) bool {
	line, ok := child.(*bo.LineBox)
	return ok && line.Height == pr.Float(0) && isEmptyLinebox(line)
}
