package layout

import (
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
)

// Layout for flex containers && flex-items.

type indexedBox struct {
	box   Box
	index int
	// measured (gopdfgo): kopia elementu ułożona przy wyznaczaniu hipotetycznego rozmiaru poprzecznego
	// (CSS Flexbox 1 § 9.4 krok 7); z niej odczytujemy linie zagnieżdżonego kontenera flex (zob.
	// forcedBreakAtEdge). Może być nil.
	measured Box
}

type flexLine struct {
	line                     []indexedBox
	crossSize, lowerBaseline pr.Float
}

func (f flexLine) reverse() { slices.Reverse(f.line) }

func (f flexLine) sum() pr.Float {
	var sum pr.Float
	for _, child := range f.line {
		box := child.box.Box()
		sum += box.HypotheticalMainSize + box.MainOuterExtra
	}
	return sum
}

func (f flexLine) allFrozen() bool {
	for _, child := range f.line {
		if !child.box.Box().Frozen {
			return false
		}
	}
	return true
}

func (f flexLine) adjustements() pr.Float {
	var sum pr.Float
	for _, child := range f.line {
		sum += child.box.Box().Adjustment
	}
	return sum
}

func (f flexLine) flexItems() []*bo.BoxFields {
	var out []*bo.BoxFields
	for _, child := range f.line {
		if box := child.box.Box(); box.IsFlexItem {
			out = append(out, box)
		}
	}
	return out
}

func sumCross(f []flexLine) pr.Float {
	var sumCross pr.Float
	for _, line := range f {
		sumCross += line.crossSize
	}
	return sumCross
}

func getAttr(box *bo.BoxFields, axis pr.KnownProp, min string) pr.MaybeFloat {
	var boxAxis pr.MaybeFloat
	if axis == pr.PWidth {
		boxAxis = box.Width
		if min == "min" {
			boxAxis = box.MinWidth
		} else if min == "max" {
			boxAxis = box.MaxWidth
		}
	} else {
		boxAxis = box.Height
		if min == "min" {
			boxAxis = box.MinHeight
		} else if min == "max" {
			boxAxis = box.MaxHeight
		}
	}
	return boxAxis
}

// tableWithAutoSize (gopdfgo): kopia pudełka opakowującego tabelę, w której tabela ma rozmiar auto
// w podanej osi (do pomiaru minimum automatycznego elementu flex).
func tableWithAutoSize(wrapper Box, axis pr.KnownProp) Box {
	children := make([]Box, len(wrapper.Box().Children))
	for i, child := range wrapper.Box().Children {
		if bo.TableT.IsInstance(child) {
			table := bo.CopyWithChildren(child, child.Box().Children)
			table.Box().Style = child.Box().Style.Copy()
			if axis == pr.PWidth {
				table.Box().Style.SetWidth(pr.TagToV(pr.Auto))
				table.Box().Width = pr.AutoF
			} else {
				table.Box().Style.SetHeight(pr.TagToV(pr.Auto))
				table.Box().Height = pr.AutoF
			}
			child = table
		}
		children[i] = child
	}
	return bo.CopyWithChildren(wrapper, children)
}

// tableWithoutMinMaxHeight (gopdfgo): kopia pudełka opakowującego, w której tabela nie ma min-height
// ani max-height (do pomiaru rozmiaru bazowego elementu flex w kolumnie).
func tableWithoutMinMaxHeight(wrapper Box) Box {
	children := make([]Box, len(wrapper.Box().Children))
	for i, child := range wrapper.Box().Children {
		if bo.TableT.IsInstance(child) {
			table := bo.CopyWithChildren(child, child.Box().Children)
			table.Box().Style = child.Box().Style.Copy()
			table.Box().Style.SetMinHeight(pr.ZeroPixels.Tagged())
			table.Box().Style.SetMaxHeight(pr.FToPx(pr.Inf))
			child = table
		}
		children[i] = child
	}
	return bo.CopyWithChildren(wrapper, children)
}

// isStretchedInColumn (gopdfgo): czy element kontenera kolumnowego jest rozciągany na szerokość linii:
// align-self (albo align-items) stretch lub normal i żaden poziomy margines nie jest auto.
func isStretchedInColumn(child, container *bo.BoxFields) bool {
	alignSelf := child.Style.GetAlignSelf()
	if alignSelf.Has(pr.Auto) {
		alignSelf = container.Style.GetAlignItems()
	}
	return alignSelf.Intersects(pr.Normal, pr.Stretch) &&
		child.Style.GetMarginLeft().Tag != pr.Auto && child.Style.GetMarginRight().Tag != pr.Auto
}

// isLastBaseline (gopdfgo): czy element jest wyrównany do ostatniej linii bazowej (align-self albo
// align-items kontenera: last baseline).
func isLastBaseline(child, container *bo.BoxFields) bool {
	alignSelf := child.Style.GetAlignSelf()
	if alignSelf.Has(pr.Auto) {
		alignSelf = container.Style.GetAlignItems()
	}
	return alignSelf.Has(pr.Baseline) && alignSelf.Has(pr.Last)
}

// tableFrame (gopdfgo): obramowanie i odstępy tabeli opakowanej przez wrapper w podanej osi, gdy tabela
// ma box-sizing: content-box (przy border-box podane rozmiary już je obejmują).
func tableFrame(wrapper *bo.BoxFields, axis pr.KnownProp) pr.Float {
	table := wrapper.GetWrappedTable().Box()
	if table.Style.GetBoxSizing() == "border-box" {
		return 0
	}
	if axis == pr.PWidth {
		return table.PaddingLeft.V() + table.PaddingRight.V() + table.BorderLeftWidth.V() + table.BorderRightWidth.V()
	}
	return table.PaddingTop.V() + table.PaddingBottom.V() + table.BorderTopWidth.V() + table.BorderBottomWidth.V()
}

// marginIsAuto (gopdfgo): czy margines elementu jest auto według stylu.
func marginIsAuto(child *bo.BoxFields, side pr.KnownProp) bool {
	return getDimOrS(child, side).Tag == pr.Auto
}

// use child.Style
func getCrossMargins(child *bo.BoxFields, cross pr.KnownProp) (out bo.MaybePoint) {
	if cross == pr.PWidth {
		return bo.MaybePoint{child.Style.GetMarginLeft().ToMaybeFloat(), child.Style.GetMarginRight().ToMaybeFloat()}
	}
	return bo.MaybePoint{child.Style.GetMarginTop().ToMaybeFloat(), child.Style.GetMarginBottom().ToMaybeFloat()}
}

// use child.Style
func getDimOrS(box *bo.BoxFields, prop pr.KnownProp) pr.TaggedDim {
	out, _ := box.Style.Get(prop.Key()).(pr.TaggedDim)
	return out
}

const (
	directionX = true
	directionY = false
)

func setPos(box *bo.BoxFields, position bool, value pr.Float) {
	if position == directionX {
		box.PositionX = value
	} else {
		box.PositionY = value
	}
}

// the returned box as same concrete type than box_
func flexLayout(context *layoutContext, box_ Box, bottomSpace pr.Float, skipStack tree.ResumeStack, containingBlock containingBlock,
	pageIsEmpty bool, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder, discard bool,
) (bo.Box, blockLayout) {
	if traceMode {
		traceLogger.DumpTree(box_, "starting flexLayout")
	}
	box := box_.Box()

	context.createFlexFormattingContext()
	brokenAtStart := context.brokenSequence // gopdfgo: zob. niżej, przed ostatecznym układem elementów

	var resumeAt tree.ResumeStack

	is_start := skipStack == nil
	box_.RemoveDecoration(box, !is_start, false)

	discard = discard || box.Style.GetContinue() == "discard"
	draw_bottom_decoration := discard || box.Style.GetBoxDecorationBreak() == "clone"

	rowGap, columnGap := box.Style.GetRowGap(), box.Style.GetColumnGap()

	if draw_bottom_decoration {
		bottomSpace += box.PaddingBottom.V() + box.BorderBottomWidth + box.MarginBottom.V()
	}

	// gopdfgo: kontener flex z transform jest blokiem zawierającym także dla potomków fixed
	// (CSS Transforms 1 § 3; zob. ownsFixedDescendants)
	ownsFixed := ownsFixedDescendants(box_)
	if box.Style.GetPosition().String == "relative" || ownsFixed {
		// New containing block, use a new absolute list
		absoluteBoxes = new([]*AbsolutePlaceholder)
	}
	if ownsFixed {
		fixedBoxes = new([]*AbsolutePlaceholder)
	}

	// References are to: https://www.w3.org/TR/css-flexbox-1/#layout-algorithm.

	// 1 Initial setup, done in formatting_structure.build.

	// 2 Determine the available main and cross space for the flex items.
	main, cross := pr.PHeight, pr.PWidth
	if strings.HasPrefix(string(box.Style.GetFlexDirection()), "row") {
		main, cross = pr.PWidth, pr.PHeight
	}

	var marginLeft pr.Float
	if box.MarginLeft != pr.AutoF {
		marginLeft = box.MarginLeft.V()
	}
	var marginRight pr.Float
	if box.MarginRight != pr.AutoF {
		marginRight = box.MarginRight.V()
	}

	// Define available main space.
	cbWidth, _ := containingBlock.ContainingBlock()
	var availableMainSpace pr.Float
	if boxMain := getAttr(box, main, ""); boxMain != pr.AutoF {
		availableMainSpace = boxMain.V()
	} else {
		// Otherwise, subtract the flex container’s margin, border, and padding…
		if main == pr.PWidth {
			availableMainSpace = cbWidth.V() - marginLeft - marginRight -
				box.PaddingLeft.V() - box.PaddingRight.V() - box.BorderLeftWidth.V() - box.BorderRightWidth.V()
		} else {
			availableMainSpace = pr.Inf
		}
	}

	// Same as above for available cross space.
	var availableCrossSpace pr.Float
	boxCross := getAttr(box, cross, "")
	if boxCross != pr.AutoF {
		availableCrossSpace = boxCross.V()
	} else {
		if cross == pr.PWidth {
			availableCrossSpace = cbWidth.V() - marginLeft - marginRight -
				box.PaddingLeft.V() - box.PaddingRight.V() - box.BorderLeftWidth.V() - box.BorderRightWidth.V()
		} else {
			availableCrossSpace = pr.Inf
		}
	}

	// gopdfgo: zob. BoxFields.IndefiniteHeight
	mainIsIndefinite := availableMainSpace == pr.Inf || (main == pr.PHeight && box.IndefiniteHeight)

	// 3 Determine the flex base size and hypothetical main size of each item.
	parentBox_ := box_.Copy()
	parentBox := parentBox_.Box()
	resolveSizesBox(context, parentBox_, containingBlock)
	if parentBox.MarginTopTruncated { // gopdfgo: zob. BoxFields.MarginTopTruncated
		parentBox.MarginTop = pr.Float(0)
	}
	// gopdfgo: wysokość nadana kontenerowi z zewnątrz (rozciągnięcie w osi
	// poprzecznej kontenera nadrzędnego) jest określona i służy za podstawę
	// procentów jego elementów (CSS Flexbox 1 § 9.4, krok 11). Źródło wracało
	// tu do wysokości ze stylu, czyli auto.
	if height, ok := box.Height.(pr.Float); ok && parentBox.Height == pr.AutoF && !box.IndefiniteHeight {
		parentBox.Height = height
	}
	blockLevelWidth(parentBox_, nil, containingBlock)
	// gopdfgo: szerokość nadana kontenerowi z zewnątrz (rozmiar główny elementu flex wiersza, rozciągnięcie
	// w kolumnie) jest jego szerokością także dla elementów: od niej zależy łamanie ich wierszy, a więc
	// i wysokość. Źródło liczyło ją tu na nowo ze stylu, więc kolumna flex będąca elementem wiersza
	// z flex-basis (albo width w procentach) mierzyła treść w szerokości bloku zawierającego, a układała
	// ją w węższej: elementy nachodziły na siebie.
	if width, ok := box.Width.(pr.Float); ok && width != pr.Inf && width >= 0 {
		parentBox.Width = width
	}
	// gopdfgo: kontener o wysokości auto z aspect-ratio i znaną szerokością ma określoną wysokość
	// z proporcji (CSS Sizing 4 § 5.1), ograniczoną min- i max-height; elementy rozciągają się do niej
	// (test WPT flex-aspect-ratio-cross-size-001). Źródło pomijało aspect-ratio kontenera flex.
	if ratio := pr.Float(math.Abs(float64(box.Style.GetAspectRatio()))); ratio > 0 && box.Height == pr.AutoF && parentBox.Height == pr.AutoF {
		if width, ok := parentBox.Width.(pr.Float); ok && width != pr.Inf {
			height := width / ratio
			if box.Style.GetBoxSizing() == "border-box" {
				outer := width + parentBox.PaddingLeft.V() + parentBox.PaddingRight.V() + parentBox.BorderLeftWidth.V() + parentBox.BorderRightWidth.V()
				height = outer/ratio - parentBox.PaddingTop.V() - parentBox.PaddingBottom.V() - parentBox.BorderTopWidth.V() - parentBox.BorderBottomWidth.V()
			}
			if maxHeight, ok := parentBox.MaxHeight.(pr.Float); ok {
				height = min(height, maxHeight)
			}
			if minHeight, ok := parentBox.MinHeight.(pr.Float); ok {
				height = max(height, minHeight)
			}
			height = max(height, 0)
			box.Height, parentBox.Height = height, height
			if cross == pr.PHeight {
				availableCrossSpace = height
			} else {
				availableMainSpace, mainIsIndefinite = height, false
			}
		}
	}
	children := append([]Box{}, box.Children...)
	// gopdfgo: sortowanie stabilne; w źródle sort.Slice, które przy równych wartościach order nie gwarantuje
	// zachowania kolejności dokumentu (dla więcej niż 12 elementów).
	// gopdfgo: dzieci pozycjonowane bezwzględnie nie są elementami flex, więc order ich nie przestawia
	// (CSS Flexbox 1 § 4.1, § 5.4); wszystkie mają ten sam klucz i zachowują kolejność dokumentu. W źródle
	// order zmieniał ich kolejność rysowania.
	orderOf := func(child Box) int {
		if child.Box().IsAbsolutelyPositioned() {
			return 0
		}
		return int(child.Box().Style.GetOrder())
	}
	sort.SliceStable(children, func(i, j int) bool { return orderOf(children[i]) < orderOf(children[j]) })

	var skip int
	if skipStack != nil {
		skip, skipStack = skipStack.Unpack()
		if strings.HasSuffix(string(box.Style.GetFlexDirection()), "-reverse") {
			children = children[:skip+1]
		} else {
			children = children[skip:]
		}
	} else {
		skipStack = nil
	}

	// gopdfgo: wznowienie linii, w której elementy dzieliły się między strony równolegle (zob. lineResume
	// niżej). Elementy, które skończyły się na poprzedniej stronie, zastępujemy pustymi, niewidocznymi
	// kopiami, żeby pozostałe zachowały swoje miejsce i szerokość.
	lineSkip := parallelLineState(skipStack)
	if lineSkip != nil {
		skipStack = nil
		for position, child := range children {
			resume, inLine := lineSkip[position+skip]
			if !inLine || !child.Box().IsFlexItem {
				continue
			}
			if resume == nil {
				children[position] = finishedFlexItem(child, isStretchedFlexItem(child, box))
			} else if len(resume) == 0 {
				// element przesunięty w całości z poprzedniej strony (zob. linePushed niżej)
				children[position] = pushedFlexItem(child, context.flexPushOffset[resumeKey(resume)])
			}
		}
	}
	pickSkip := func(index int, first tree.ResumeStack) tree.ResumeStack {
		if lineSkip != nil {
			if resume := lineSkip[index]; len(resume) != 0 {
				return resume
			}
			return nil
		}
		return first
	}
	// pushedBefore: element już raz przesunięty na tę stronę; drugi raz go nie przesuwamy
	pushedBefore := func(index int) bool {
		resume, inLine := lineSkip[index]
		return inLine && resume != nil && len(resume) == 0
	}
	pageEdge := context.pageBottom - bottomSpace
	pushItem := func(pushed map[int]tree.ResumeStack, index int, top pr.Float) {
		marker := tree.ResumeStack{}
		if context.flexPushOffset == nil {
			context.flexPushOffset = map[uintptr]pr.Float{}
		}
		context.flexPushOffset[resumeKey(marker)] = max(top-pageEdge, 0)
		pushed[index] = marker
	}

	childSkipStack := skipStack

	// resolve gaps and stores it into rowGap.Value
	if rowGap.Tag == pr.Normal {
		rowGap.Value = 0
	} else if rowGap.Unit == pr.Perc || rowGap.Calc != nil { // gopdfgo: także calc() z procentem
		// gopdfgo: przez ResolvePercentage, żeby calc() z procentem zachowało część w pikselach
		base := pr.Float(0)
		if box.Height != pr.AutoF && !box.IndefiniteHeight {
			base = box.Height.V()
		}
		rowGap.Value = pr.ResolvePercentage(rowGap, base).V()
	}
	if columnGap.Tag == pr.Normal {
		columnGap.Value = 0
	} else if columnGap.Unit == pr.Perc || columnGap.Calc != nil { // gopdfgo: także calc() z procentem
		base := pr.Float(0)
		if box.Width != pr.AutoF {
			base = box.Width.V()
		} else if horizontal := map[bool]pr.Float{true: availableMainSpace, false: availableCrossSpace}[main == pr.PWidth]; horizontal < pr.Inf {
			// gopdfgo: kontener blokowy o szerokości auto ma szerokość pudełka zawierającego bez marginesów,
			// obramowań i dopełnień (CSS 2 § 10.3.3); źródło liczyło wtedy procent od zera
			base = max(horizontal, 0)
		}
		columnGap.Value = pr.ResolvePercentage(columnGap, base).V()
	}

	var mainGap, crossGap pr.Float
	if main == pr.PWidth {
		mainGap, crossGap = columnGap.Value, rowGap.Value
	} else {
		mainGap, crossGap = rowGap.Value, columnGap.Value
	}

	// gopdfgo: elementy z visibility: collapse zamieniamy po pierwszym przebiegu na rozpórki i układ
	// zaczyna się od nowa od kroku 3 (CSS Flexbox 1 § 9.4 krok 10); w źródle krok 10 był pominięty
	// („TODO”), więc zwinięty element zajmował miejsce jak visibility: hidden.
	collapsedPass := false
	struts := map[*bo.BoxFields]bool{}
	// lineGap: odstęp osi głównej przed i-tym elementem linii. Odstępy przylegające do rozpórki
	// znikają (CSS Align 3 § 8.1, test WPT gap-collapse): odstęp stoi tylko między dwoma elementami,
	// które nie są rozpórkami.
	lineGap := func(items []indexedBox, i int) pr.Float {
		if struts[items[i].box.Box()] {
			return 0
		}
		for j := i - 1; j >= 0; j-- {
			if !struts[items[j].box.Box()] {
				return mainGap
			}
		}
		return 0
	}
	var boxBeforeLines bo.BoxFields
	var absoluteBefore, fixedBefore int
restartCollapsed:
	positionX := (parentBox.PositionX + parentBox.BorderLeftWidth + parentBox.PaddingLeft.V())
	if parentBox.MarginLeft != pr.AutoF {
		positionX += parentBox.MarginLeft.V()
	}
	positionY := (parentBox.PositionY + parentBox.BorderTopWidth + parentBox.PaddingTop.V())
	if parentBox.MarginTop != pr.AutoF {
		positionY += parentBox.MarginTop.V()
	}

	for index, child_ := range children {
		child := child_.Box()
		if !child.IsFlexItem && collapsedPass {
			continue // pozostałe dzieci obsłużył już pierwszy przebieg
		}
		if !child.IsFlexItem {
			// Absolute child layout: create placeholder.
			if child.IsAbsolutelyPositioned() {
				child.PositionX = positionX
				child.PositionY = positionY
				placeholder := NewAbsolutePlaceholder(child_)
				placeholder.Box().Index = index
				children[index] = placeholder
				if child.Style.GetPosition().String == "absolute" {
					*absoluteBoxes = append(*absoluteBoxes, placeholder)
				} else {
					*fixedBoxes = append(*fixedBoxes, placeholder)
				}
			} else if child.IsRunning() {
				runningName := child.Style.GetPosition().String
				page := context.currentPage
				context.runningElements[runningName][page] = append(context.runningElements[runningName][page], child_)
			}
			continue
		}

		// See https://www.W3.org/TR/css-flexbox-1/#min-size-auto
		var childContainingBlock bo.MaybePoint
		if main == pr.PWidth {
			childContainingBlock = bo.MaybePoint{availableMainSpace, parentBox.Height}
			if box.IndefiniteHeight {
				childContainingBlock[1] = pr.AutoF
			}
		} else if mainIsIndefinite {
			// gopdfgo: wysokość kontenera kolumnowego jest nieokreślona, więc
			// wysokości procentowe elementów liczą się jak auto (CSS Flexbox
			// § 9.8); źródło liczyło procent z nieskończoności, co dawało
			// NaN w zapisanym PDF
			childContainingBlock = bo.MaybePoint{parentBox.Width, pr.AutoF}
		} else {
			childContainingBlock = bo.MaybePoint{parentBox.Width, availableMainSpace}
		}
		resolveSizes(context, child_, childContainingBlock)
		if child.IsTableWrapper {
			tableWrapperWidth(context, child_, childContainingBlock)
		}
		child.PositionX = positionX
		child.PositionY = positionY
		// gopdfgo: automatyczne minimum dotyczy tylko osi głównej (CSS Flexbox 1 § 4.5); w osi poprzecznej
		// wynosi zero. Źródło liczyło je dla szerokości zawsze, a dla wysokości elementów zastępowanych także
		// w wierszu, przez co obraz rozciągany w poprzek był skalowany odwrotnie do proporcji.
		if minWidth := child.Style.GetMinWidth(); main == pr.PWidth && minWidth.Tag == pr.Auto && minWidth.Intrinsic == pr.IntrinsicNone {
			specifiedSize := child.Width
			newChild := child_.Copy()
			if bo.ParentT.IsInstance(child_) {
				newChild = bo.CopyWithChildren(child_, child.Children)
			}
			newChild.Box().Style = child.Style.Copy()
			newChild.Box().Style.SetWidth(pr.TagToV(pr.Auto))
			newChild.Box().Style.SetMinWidth(pr.ZeroPixels.Tagged())
			newChild.Box().Style.SetMaxWidth(pr.Dimension{Value: pr.Inf, Unit: pr.Px}.Tagged())
			if child.IsTableWrapper {
				// gopdfgo: podana szerokość tabeli nie jest dodatkowym minimum w algorytmie flex
				// (CSS Tables 3 § 3.9.1, test WPT table-as-item-specified-width): minimum automatyczne
				// liczymy z treści tabeli, a nie z jej width
				newChild = tableWithAutoSize(newChild, pr.PWidth)
				specifiedSize = pr.AutoF
			}
			contentSize := minContentWidth(context, newChild, false)
			var transferredSize pr.MaybeFloat
			if replaced, isReplaced := child_.(bo.ReplacedBoxITF); isReplaced {
				image := replaced.Replaced().Replacement
				_, intrinsicHeight, intrinsicRatio := image.GetIntrinsicSize(
					child.Style.GetImageResolution().Value, child.Style.GetFontSize().Value)
				// gopdfgo: gdy rozmiar poprzeczny jest podany, rozmiar przeniesiony przez proporcje
				// liczy się z niego, a nie z rozmiaru własnego obrazu (CSS Flexbox 1 § 4.5)
				if height, ok := child.Height.(pr.Float); ok && pr.Is(intrinsicRatio) {
					height = max(child.MinHeight.V(), min(child.MaxHeight.V(), height))
					transferredSize = height * intrinsicRatio.V()
					contentSize = transferredSize.V()
				} else if stretched, ok := ratioBaseSize(child_, box, parentBox, main, true); ok {
					// gopdfgo: rozciągnięty rozmiar poprzeczny jest określony (CSS Flexbox 1 § 9.8), więc
					// rozmiar min-content obrazu wynika z niego i z proporcji, a nie z rozmiaru własnego
					// (§ 4.5, test WPT flex-minimum-width-flex-items-013)
					transferredSize = stretched
					contentSize = stretched
				} else if pr.Is(intrinsicRatio) && pr.Is(intrinsicHeight) {
					// ...a bez niego rozmiar treści ograniczają min-height i max-height przeliczone proporcją
					ratio := intrinsicRatio.V()
					low, high := child.MinHeight.V()*ratio, child.MaxHeight.V()*ratio
					transferredSize = max(low, min(high, intrinsicHeight.V()*ratio))
					contentSize = max(child.MinWidth.V(), min(child.MaxWidth.V(), contentSize))
					contentSize = max(low, min(high, contentSize))
				}
			}
			if specifiedSize != pr.AutoF {
				child.MinWidth = min(specifiedSize.V(), contentSize)
			} else if transferredSize != nil {
				child.MinWidth = min(transferredSize.V(), contentSize)
			} else {
				child.MinWidth = contentSize
			}
			// gopdfgo: minimum automatyczne nie przekracza określonego maksimum (CSS Flexbox 1 § 4.5).
			child.MinWidth = min(child.MinWidth.V(), child.MaxWidth.V())
			// Minimum automatyczne dotyczy tylko elementów, które nie są kontenerami przewijania.
			if _, isReplaced := child_.(bo.ReplacedBoxITF); !isReplaced && isScrollContainer(child.Style) {
				child.MinWidth = pr.Float(0)
			}
		}
		// gopdfgo: min-width tabeli zostaje na pudełku tabeli, ale ogranicza rozmiar główny elementu flex,
		// którym jest pudełko opakowujące (CSS Tables 3 § 3.9.1); procent liczy się od kontenera flex.
		// Źródło je pomijało (test WPT table-item-flex-percentage-min-width).
		if main == pr.PWidth && child.IsTableWrapper {
			if tableMin, ok := child.GetWrappedTable().Box().MinWidth.(pr.Float); ok && tableMin > 0 {
				if child.GetWrappedTable().Box().Style.GetBoxSizing() != "border-box" {
					tableMin += tableFrame(child, pr.PWidth)
				}
				minWidth, _ := child.MinWidth.(pr.Float)
				child.MinWidth = max(minWidth, tableMin)
			}
		}
		// gopdfgo: automatyczna wysokość minimalna wymaga pełnego układu dziecka. W osi poprzecznej
		// (kontener wierszowy) według CSS Flexbox 1 § 4.5 wynosi zero, więc liczymy ją tam tylko dla
		// elementów zastępowanych, gdzie układ jest tani, a testy źródłowe na niej polegają. W źródle
		// liczono ją zawsze: trzeci pełny układ na poziom zagnieżdżenia, koszt 3^n zamiast 2^n.
		_, childIsReplaced := child_.(bo.ReplacedBoxITF)
		if main == pr.PHeight && child.Style.GetMinHeight().Tag == pr.Auto {
			specifiedSize := child.Height
			newChild := child_.Copy()
			newChild.Box().Style = child.Style.Copy()
			newChild.Box().Style.SetHeight(pr.TagToV(pr.Auto))
			newChild.Box().Style.SetMinHeight(pr.ZeroPixels.Tagged())
			newChild.Box().Style.SetMaxHeight(pr.Dimension{Value: pr.Inf, Unit: pr.Px}.Tagged())
			if child.IsTableWrapper {
				// gopdfgo: jak wyżej: podana wysokość tabeli nie jest minimum w algorytmie flex
				newChild = tableWithAutoSize(bo.CopyWithChildren(newChild, child.Children), pr.PHeight)
				specifiedSize = pr.AutoF
			}
			if cs := newChild.Box().Style; cs.GetWidth().Tag == pr.Auto {
				newChildWidth := maxContentWidth(context, newChild, true)
				cs.SetWidth(pr.FToPx(newChildWidth))
			}
			context.flexMeasuring++
			newChild, _, _ = blockLevelLayout(context, newChild.(bo.BlockLevelBoxITF),
				bottomSpace, pickSkip(index+skip, childSkipStack), parentBox, pageIsEmpty, nil, nil, nil, false, -1)
			context.flexMeasuring--
			var contentSize pr.Float
			if newChild != nil {
				contentSize = newChild.Box().Height.V()
			}
			var transferredSize pr.MaybeFloat
			if replaced, isReplaced := child_.(bo.ReplacedBoxITF); isReplaced {
				image := replaced.Replaced().Replacement
				intrinsicWidth, _, intrinsicRatio := image.GetIntrinsicSize(
					child.Style.GetImageResolution().Value, child.Style.GetFontSize().Value)
				if width, ok := child.Width.(pr.Float); ok && pr.Is(intrinsicRatio) && child.Style.GetWidth().Tag != pr.Auto {
					width = max(child.MinWidth.V(), min(child.MaxWidth.V(), width))
					transferredSize = width / intrinsicRatio.V()
					contentSize = transferredSize.V()
				} else if pr.Is(intrinsicRatio) && pr.Is(intrinsicWidth) {
					ratio := intrinsicRatio.V()
					low, high := pr.Float(0), pr.Inf
					if minWidth := child.Style.GetMinWidth(); minWidth.Tag != pr.Auto && minWidth.Unit == pr.Px {
						low = minWidth.Value / ratio
					}
					if maxWidth := child.Style.GetMaxWidth(); maxWidth.Tag != pr.Auto && maxWidth.Unit == pr.Px {
						high = maxWidth.Value / ratio
					}
					transferredSize = max(low, min(high, intrinsicWidth.V()/ratio))
					contentSize = max(child.MinHeight.V(), min(child.MaxHeight.V(), contentSize))
					contentSize = max(low, min(high, contentSize))
				} else if !pr.Is(intrinsicWidth) && pr.Is(intrinsicRatio) {
					// gopdfgo: bez rozmiaru i bez proporcji obraz ma rozmiar domyślny 300×150 (CSS Images 3
					// § 4.3) i wysokość z układu jest właściwa; zerujemy tylko przy samych proporcjach (test
					// WPT flex-svg-no-intrinsic-column-001)
					// TODO: wrongly set by block_level_layout, would be OK with
					// min_content_height.
					contentSize = 0
				}
			}
			if specifiedSize != pr.AutoF {
				child.MinHeight = min(specifiedSize.V(), contentSize)
			} else if transferredSize != nil {
				child.MinHeight = min(transferredSize.V(), contentSize)
			} else {
				child.MinHeight = contentSize
			}
			child.MinHeight = min(child.MinHeight.V(), child.MaxHeight.V())
			if !childIsReplaced && isScrollContainer(child.Style) {
				child.MinHeight = pr.Float(0)
			}
		}

		// gopdfgo: min-height i max-height podane jako min-content, max-content albo fit-content
		// w kontenerze kolumnowym to wysokość treści elementu (CSS Sizing 3 § 2.1; w osi blokowej wszystkie
		// trzy dają wysokość treści). Źródło traktowało je jak auto, czyli brak ograniczenia.
		if main == pr.PHeight && (child.Style.GetMaxHeight().Intrinsic != pr.IntrinsicNone || child.Style.GetMinHeight().Intrinsic != pr.IntrinsicNone) {
			probe := child_.Copy()
			probe.Box().Style = child.Style.Copy()
			probe.Box().Style.SetHeight(pr.TagToV(pr.Auto))
			probe.Box().Style.SetMinHeight(pr.ZeroPixels.Tagged())
			probe.Box().Style.SetMaxHeight(pr.Dimension{Value: pr.Inf, Unit: pr.Px}.Tagged())
			if cs := probe.Box().Style; cs.GetWidth().Tag == pr.Auto {
				if _, isReplaced := child_.(bo.ReplacedBoxITF); isReplaced && box.Style.GetFlexWrap() == "nowrap" && isStretchedInColumn(child, box) {
					// gopdfgo: rozciągnięta szerokość elementu jednowierszowego kontenera o określonej
					// szerokości jest określona (CSS Flexbox 1 § 9.8 p. 1), więc wysokość min-content obrazu
					// wynika z niej przez proporcje (test WPT flex-minimum-height-flex-items-023)
					inner := box.Width.V() - child.MarginLeft.V() - child.MarginRight.V() - child.PaddingLeft.V() -
						child.PaddingRight.V() - child.BorderLeftWidth.V() - child.BorderRightWidth.V()
					cs.SetWidth(pr.FToPx(max(child.MinWidth.V(), min(child.MaxWidth.V(), max(inner, 0)))))
				} else {
					cs.SetWidth(pr.FToPx(maxContentWidth(context, probe, true)))
				}
			}
			context.flexMeasuring++
			measured, _, _ := blockLevelLayout(context, probe.(bo.BlockLevelBoxITF),
				bottomSpace, pickSkip(index+skip, childSkipStack), parentBox, pageIsEmpty, nil, nil, nil, false, -1)
			context.flexMeasuring--
			if measured != nil {
				content := measured.Box().Height.V()
				if child.Style.GetMaxHeight().Intrinsic != pr.IntrinsicNone {
					child.MaxHeight = content
				}
				if child.Style.GetMinHeight().Intrinsic != pr.IntrinsicNone {
					child.MinHeight = content
				}
			}
		}

		var flexBasis pr.MaybeFloat // nil means "content"
		basisIntrinsic := pr.IntrinsicNone
		basisIsContent := child.Style.GetFlexBasis().Tag == pr.Content
		if styleFlexBasis := child.Style.GetFlexBasis(); styleFlexBasis.Tag == pr.Content {
			flexBasis = nil
		} else if styleFlexBasis.Unit == pr.Perc && mainIsIndefinite {
			// gopdfgo: procentowa podstawa przy nieokreślonym rozmiarze głównym
			// kontenera działa jak content (CSS Flexbox 1 § 7.2.3); źródło
			// liczyło procent z nieskończoności i zapisywało Inf w PDF
			flexBasis = nil
			basisIsContent = true
		} else {
			resolved := pr.ResolvePercentage(styleFlexBasis, availableMainSpace)
			if styleFlexBasis.Intrinsic != pr.IntrinsicNone {
				// gopdfgo: flex-basis: min-content, max-content i fit-content to rozmiar z treści (CSS Flexbox 1
				// § 7.2.3, CSS Sizing 3 § 3.1); źródło brało wtedy szerokość pudełka jak przy auto.
				flexBasis = nil
				basisIsContent = true
				basisIntrinsic = styleFlexBasis.Intrinsic
			} else if resolved == pr.AutoF {
				flexBasis = getAttr(child, main, "")
				// gopdfgo: tabela bez podanej szerokości ma podstawę z treści (flex-basis: auto przechodzi
				// w content, CSS Flexbox 1 § 7.2.3); szerokość pudełka opakowującego jest tu wynikiem
				// układu tabeli w całym dostępnym miejscu, nie rozmiarem podanym. Źródło brało ją jako
				// podstawę (test WPT table-as-item-percent-width-cell-001).
				if child.IsTableWrapper && main == pr.PWidth {
					if ts := child.GetWrappedTable().Box().Style.GetWidth(); ts.Tag == pr.Auto && ts.Intrinsic == pr.IntrinsicNone {
						flexBasis = nil
					}
				}
				if flexBasis == pr.AutoF {
					flexBasis = nil
				}
			} else {
				flexBasis = resolved
				// gopdfgo: flex-basis podaje rozmiar pudełka wskazanego przez box-sizing (CSS Flexbox 1
				// § 7.2.1); źródło traktowało go zawsze jak rozmiar treści, więc element z box-sizing:
				// border-box i flex-basis był szerszy o obramowanie i odstęp wewnętrzny.
				if basis, ok := resolved.(pr.Float); ok && child.Style.GetBoxSizing() == "border-box" {
					if main == pr.PWidth {
						basis -= child.PaddingLeft.V() + child.PaddingRight.V() + child.BorderLeftWidth.V() + child.BorderRightWidth.V()
					} else {
						basis -= child.PaddingTop.V() + child.PaddingBottom.V() + child.BorderTopWidth.V() + child.BorderBottomWidth.V()
					}
					flexBasis = max(basis, 0)
				}
				// gopdfgo: flex-basis tabeli opisuje pudełko wskazane przez box-sizing tabeli (CSS Tables 3
				// § 3.9.1), a rozmiar elementu flex to pudełko opakowujące, czyli pudełko obramowania tabeli;
				// przy content-box dodajemy obramowanie i odstępy tabeli. Źródło brało flex-basis wprost,
				// więc tabela z obramowaniem w kolumnie była o nie niższa (test WPT table-as-item-inflexible-in-column-1).
				if basis, ok := flexBasis.(pr.Float); ok && child.IsTableWrapper {
					flexBasis = basis + tableFrame(child, main)
					// gopdfgo: w kolumnie rozmiar pudełka opakowującego obejmuje też podpisy tabeli (CSS Tables 3
					// § 3.9.1: flex-basis dotyczy tabeli, a elementem flex jest pudełko opakowujące; test WPT
					// table-as-item-inflexible-in-column-2)
					if main == pr.PHeight {
						flexBasis = flexBasis.V() + captionsHeight(context, child_, bottomSpace, parentBox, pageIsEmpty)
					}
				}
			}
		}

		// 3.A If the item has a definite used flex basis…
		if flexBasis != nil {
			child.FlexBaseSize = flexBasis.V()
			if main == pr.PWidth {
				child.MainOuterExtra = (child.BorderLeftWidth + child.BorderRightWidth +
					child.PaddingLeft.V() + child.PaddingRight.V())
				if child.MarginLeft != pr.AutoF {
					child.MainOuterExtra += child.MarginLeft.V()
				}
				if child.MarginRight != pr.AutoF {
					child.MainOuterExtra += child.MarginRight.V()
				}
			} else {
				child.MainOuterExtra = (child.BorderTopWidth + child.BorderBottomWidth +
					child.PaddingTop.V() + child.PaddingBottom.V())
				if child.MarginTop != pr.AutoF {
					child.MainOuterExtra += child.MarginTop.V()
				}
				if child.MarginBottom != pr.AutoF {
					child.MainOuterExtra += child.MarginBottom.V()
				}
			}
		} else if base, ok := ratioBaseSize(child_, box, parentBox, main, basisIsContent); ok {
			// gopdfgo: 3.B element z proporcjami, podstawą content i określonym rozmiarem poprzecznym
			// (tu: rozciągnięty do określonego rozmiaru kontenera jednowierszowego) ma rozmiar bazowy
			// wyliczony z rozmiaru poprzecznego i proporcji (CSS Flexbox 1 § 9.2 krok 3B). W źródle ten
			// krok był pominięty, więc rozciągnięty obraz zachowywał szerokość wewnętrzną i był zniekształcany.
			child.FlexBaseSize = base
			if main == pr.PWidth {
				child.MainOuterExtra = child.BorderLeftWidth.V() + child.BorderRightWidth.V() + child.PaddingLeft.V() + child.PaddingRight.V()
				if child.MarginLeft != pr.AutoF {
					child.MainOuterExtra += child.MarginLeft.V()
				}
				if child.MarginRight != pr.AutoF {
					child.MainOuterExtra += child.MarginRight.V()
				}
				if child.Style.GetMinWidth().Tag == pr.Auto {
					child.MinWidth = min(child.MinWidth.V(), base) // minimum automatyczne też wynika z proporcji
				}
			} else {
				child.MainOuterExtra = child.BorderTopWidth.V() + child.BorderBottomWidth.V() + child.PaddingTop.V() + child.PaddingBottom.V()
				if child.MarginTop != pr.AutoF {
					child.MainOuterExtra += child.MarginTop.V()
				}
				if child.MarginBottom != pr.AutoF {
					child.MainOuterExtra += child.MarginBottom.V()
				}
				if child.Style.GetMinHeight().Tag == pr.Auto {
					child.MinHeight = min(child.MinHeight.V(), base)
				}
			}
		} else if false {
			// TODO: 3.C If the used flex basis is 'content'…
			// TODO: 3.D Otherwise, if the used flex basis is 'content'…
		} else {
			// 3.E Otherwise…
			newChild_ := child_.Copy()
			newChild := newChild_.Box()
			newChild.Style = child.Style.Copy()
			// gopdfgo: flex-basis: content mierzy treść, pomijając podany rozmiar w osi głównej
			// (CSS Flexbox 1 § 7.2.1); w źródle podana szerokość nadal obowiązywała.
			if basisIsContent {
				if main == pr.PWidth {
					newChild.Style.SetWidth(pr.TagToV(pr.Auto))
					newChild.Width = pr.AutoF
				} else {
					newChild.Style.SetHeight(pr.TagToV(pr.Auto))
					newChild.Height = pr.AutoF
				}
			}
			if main == pr.PWidth {
				// … the item’s min and max main sizes are ignored.
				newChild.Style.SetMinWidth(pr.ZeroPixels.Tagged())
				newChild.Style.SetMaxWidth(pr.FToPx(pr.Inf))

				child.FlexBaseSize = maxContentWidth(context, newChild_, false)
				switch basisIntrinsic {
				case pr.IntrinsicMinContent:
					child.FlexBaseSize = minContentWidth(context, newChild_, false)
				case pr.IntrinsicFitContent:
					child.FlexBaseSize = min(child.FlexBaseSize, max(minContentWidth(context, newChild_, false), availableMainSpace))
				}
				if fill, ok := ratioOnlyFillWidth(child_, availableMainSpace); ok {
					child.FlexBaseSize = fill
				}
				// gopdfgo: marginesy, obramowania i odstępy bierzemy z wartości rozwiązanych względem kontenera;
				// źródło liczyło je jako różnicę szerokości zewnętrznej i wewnętrznej pudełka z max-width, więc
				// przy treści szerszej niż max-width wychodziła wartość ujemna, a marginesy procentowe
				// (w szerokościach wewnętrznych równe zero) nie zmniejszały wolnego miejsca
				child.MainOuterExtra = child.BorderLeftWidth.V() + child.BorderRightWidth.V() + child.PaddingLeft.V() + child.PaddingRight.V()
				if child.MarginLeft != pr.AutoF {
					child.MainOuterExtra += child.MarginLeft.V()
				}
				if child.MarginRight != pr.AutoF {
					child.MainOuterExtra += child.MarginRight.V()
				}
			} else {
				// … the item’s min and max main sizes are ignored.
				newChild.Style.SetMinHeight(pr.ZeroPixels.Tagged())
				newChild.Style.SetMaxHeight(pr.FToPx(pr.Inf))
				if child.IsTableWrapper {
					// gopdfgo: min-height i max-height tabeli zostają na pudełku tabeli, nie na opakowującym;
					// też są pomijane przy rozmiarze bazowym (test WPT table-as-item-min-height-1). Źródło
					// mierzyło tabelę z jej min-height, więc rozmiar bazowy był za duży.
					newChild_ = tableWithoutMinMaxHeight(newChild_)
					newChild = newChild_.Box()
				}

				newChild.Width = pr.Inf
				// gopdfgo: element nierozciągany w osi poprzecznej ma szerokość fit-content (CSS Flexbox 1
				// § 9.4 krok 7) i z nią mierzymy jego wysokość; źródło mierzyło przy szerokości kontenera,
				// więc procentowe odstępy liczone od szerokości elementu były za duże (test WPT
				// fit-content-item-001)
				if !child.IsTableWrapper && newChild.Style.GetWidth().Tag == pr.Auto && newChild.Style.GetWidth().Intrinsic == pr.IntrinsicNone &&
					!isStretchedInColumn(child, box) {
					if available, ok := parentBox.Width.(pr.Float); ok {
						fit := min(maxContentWidth(context, newChild_, true), max(minContentWidth(context, newChild_, true), available))
						fit -= newChild.PaddingLeft.V() + newChild.PaddingRight.V() + newChild.BorderLeftWidth.V() + newChild.BorderRightWidth.V()
						if newChild.MarginLeft != pr.AutoF {
							fit -= newChild.MarginLeft.V()
						}
						if newChild.MarginRight != pr.AutoF {
							fit -= newChild.MarginRight.V()
						}
						if newChild.Style.GetBoxSizing() == "border-box" {
							fit += newChild.PaddingLeft.V() + newChild.PaddingRight.V() + newChild.BorderLeftWidth.V() + newChild.BorderRightWidth.V()
						}
						newChild.Style.SetWidth(pr.FToPx(max(fit, 0)))
					}
				}
				var tmp blockLayout
				context.flexMeasuring++
				newChild_, tmp, _ = blockLevelLayout(
					context, newChild_.(bo.BlockLevelBoxITF), bottomSpace, pickSkip(index+skip, childSkipStack), parentBox,
					pageIsEmpty, absoluteBoxes, fixedBoxes, nil, false, -1)
				context.flexMeasuring--
				if newChild_ != nil {
					// As flex items margins never collapse (with other flex items or
					// with the flex container), we can add the adjoining margins to the
					// child height.
					newChild = newChild_.Box()
					newChild.Height = newChild.Height.V() + collapseMargin(tmp.adjoiningMargins)
					child.FlexBaseSize = newChild.Height.V()
					child.MainOuterExtra = newChild.MarginHeight() - newChild.Height.V()
				} else {
					child.FlexBaseSize, child.MainOuterExtra = 0, 0
				}
			}
		}

		if main == pr.PWidth {
			positionX += child.FlexBaseSize + child.MainOuterExtra
			child.HypotheticalMainSize = max(child.MinWidth.V(), min(child.FlexBaseSize, child.MaxWidth.V()))
		} else {
			positionY += child.FlexBaseSize + child.MainOuterExtra
			child.HypotheticalMainSize = max(child.MinHeight.V(), min(child.FlexBaseSize, child.MaxHeight.V()))
		}

		// Skip stack is only for the first child
		childSkipStack = nil
	}

	// 4 Determine the main size of the flex container using the rules of the formatting
	// context in which it participates.
	if !collapsedPass {
		boxBeforeLines = *box
		absoluteBefore, fixedBefore = len(*absoluteBoxes), len(*fixedBoxes)
	}

	if main == pr.PWidth {
		blockLevelWidth(box_, nil, containingBlock)
	} else {
		if box.Height == pr.AutoF {
			box.Height = pr.Float(0)
			// gopdfgo: przy height: min-content, max-content i fit-content wysokość kontenera kolumnowego
			// to suma wkładów elementów (CSS Flexbox 1 § 9.9.1), a nie ich rozmiarów hipotetycznych
			// (test WPT flex-container-max-content-001)
			intrinsicHeight := box.Style.GetHeight().Intrinsic != pr.IntrinsicNone && box.Style.GetFlexWrap() == "nowrap"
			seen := false // gopdfgo: odstęp tylko między elementami (źródło liczyło od indeksu, także przed pierwszym elementem po dziecku pozycjonowanym)
			for _, child_ := range children {
				child := child_.Box()
				if !child.IsFlexItem {
					continue
				}
				if intrinsicHeight && !struts[child] {
					box.Height = box.Height.V() + columnItemContribution(context, child_, parentBox, pageIsEmpty) + child.MainOuterExtra
				} else {
					box.Height = box.Height.V() + child.HypotheticalMainSize + child.MainOuterExtra
				}
				if !struts[child] {
					if seen {
						box.Height = box.Height.V() + mainGap
					}
					seen = true
				}
			}
		}
		box.Height = max(box.MinHeight.V(), min(box.Height.V(), box.MaxHeight.V()))
	}

	// 5 If the flex container is single-line, collect all the flex items into a single
	// flex line.
	var flexLines []flexLine
	var line flexLine
	var lineSize pr.Float
	mainSize := getAttr(box, main, "")

	for i_, child_ := range children {
		i := i_ + skip
		child := child_.Box()
		if !child.IsFlexItem {
			continue
		}
		lineSize += child.HypotheticalMainSize + child.MainOuterExtra
		// gopdfgo: odstęp przed elementem tylko wtedy, gdy linia ma już element (zob. lineGap)
		if probe := append(line.line, indexedBox{index: i, box: child_}); lineGap(probe, len(probe)-1) != 0 {
			lineSize += mainGap
		}
		if box.Style.GetFlexWrap() != "nowrap" && lineSize > mainSize.V() {
			if len(line.line) != 0 {
				flexLines = append(flexLines, line)
				line = flexLine{line: []indexedBox{{index: i, box: child_}}}
				lineSize = child.HypotheticalMainSize + child.MainOuterExtra
			} else {
				line.line = append(line.line, indexedBox{index: i, box: child_})
				flexLines = append(flexLines, line)
				line.line = nil
				lineSize = 0
			}
		} else {
			line.line = append(line.line, indexedBox{index: i, box: child_})
		}
	}
	if len(line.line) != 0 {
		flexLines = append(flexLines, line)
	}
	// gopdfgo: kontener kolumnowy z zawijaniem i wysokością auto, łamany przez max-height, ma wysokość
	// najdłuższej linii (rozmiar max-content przy danym ograniczeniu, CSS Flexbox 1 § 9.9.1; tak Blink).
	// Źródło zostawiało sumę wszystkich elementów przyciętą do max-height, więc przy column-reverse
	// krótsza linia stała za nisko (test WPT flex-column-reverse-multiline-item-position).
	if main == pr.PHeight && len(flexLines) > 1 && boxBeforeLines.Height == pr.AutoF && !box.IndefiniteHeight {
		var longest pr.Float
		for _, line := range flexLines {
			var size pr.Float
			for i, v := range line.line {
				size += v.box.Box().HypotheticalMainSize + v.box.Box().MainOuterExtra + lineGap(line.line, i)
			}
			longest = max(longest, size)
		}
		box.Height = max(box.MinHeight.V(), min(longest, box.Height.V()))
	}

	// TODO: handle *-reverse using the terminology from the specification
	// gopdfgo: wrap-reverse zamienia początek i koniec osi poprzecznej (CSS Flexbox 1 § 5.2), więc
	// układamy linie jak dla wrap, a na końcu odbijamy pozycje elementów w osi poprzecznej. W źródle
	// odwracana była tylko kolejność linii, które nadal były pakowane od góry.
	if strings.HasSuffix(string(box.Style.GetFlexDirection()), "-reverse") {
		for _, line := range flexLines {
			line.reverse()
		}
	}

	// 6 Resolve the flexible lengths of all the flex items to find their used main size.
	availableMainSpace = getAttr(box, main, "").V()
	for _, line := range flexLines {
		// 9.7.1 Determine the used flex factor.
		hypotheticalMainSize := line.sum()
		flexFactorType := "shrink"
		if hypotheticalMainSize < availableMainSpace {
			flexFactorType = "grow"
		}

		// 9.7.3 Size inflexible items.
		for _, v := range line.line {
			child := v.box.Box()
			var flexCondition bool
			if flexFactorType == "grow" {
				child.FlexFactor = child.Style.GetFlexGrow()
				flexCondition = child.FlexBaseSize > child.HypotheticalMainSize
			} else {
				child.FlexFactor = child.Style.GetFlexShrink()
				flexCondition = child.FlexBaseSize < child.HypotheticalMainSize
			}
			if child.FlexFactor == 0 || flexCondition {
				child.TargetMainSize = child.HypotheticalMainSize
				child.Frozen = true
			} else {
				child.Frozen = false
			}
		}

		// 9.7.4 Calculate initial free space.
		initialFreeSpace := availableMainSpace
		for i, v := range line.line {
			child := v.box.Box()
			if child.Frozen {
				initialFreeSpace -= child.TargetMainSize + child.MainOuterExtra
			} else {
				initialFreeSpace -= child.FlexBaseSize + child.MainOuterExtra
			}
			initialFreeSpace -= lineGap(line.line, i)
		}

		// 9.7.5.a Check for flexible items.
		for !line.allFrozen() {
			var unfrozenFactorSum pr.Float
			remainingFreeSpace := availableMainSpace

			// 9.7.5.b Calculate the remaining free space.
			for i, v := range line.line {
				child := v.box.Box()
				if child.Frozen {
					remainingFreeSpace -= child.TargetMainSize + child.MainOuterExtra
				} else {
					remainingFreeSpace -= child.FlexBaseSize + child.MainOuterExtra
					unfrozenFactorSum += child.FlexFactor
				}
				remainingFreeSpace -= lineGap(line.line, i)
			}

			if initialFreeSpace == pr.Inf {
				initialFreeSpace = math.MaxInt32
			}
			if remainingFreeSpace == pr.Inf {
				remainingFreeSpace = math.MaxInt32
			}
			// gopdfgo: gdy suma współczynników jest mniejsza od 1, rozdzielany jest tylko ten ułamek
			// początkowego wolnego miejsca, o ile jest co do wartości bezwzględnej mniejszy od pozostałego
			// (CSS Flexbox 1 § 9.7 krok 4b). Źródło mnożyło początkowe wolne miejsce w każdym obrocie pętli
			// i porównywało rzędy wielkości zamiast wartości, tylko dla dodatnich; flex-shrink poniżej 1
			// kurczył więc element jak przy 1.
			if unfrozenFactorSum < 1 {
				if scaled := initialFreeSpace * unfrozenFactorSum; pr.Abs(scaled) < pr.Abs(remainingFreeSpace) {
					remainingFreeSpace = scaled
				}
			}

			// 9.7.5.c Distribute free space proportional to the flex factors.
			if remainingFreeSpace == 0 {
				// If the remaining free space is zero: "Do nothing", but we at least set
				// the flex_base_size as target_main_size for next step.
				for _, v := range line.line {
					child := v.box.Box()
					if !child.Frozen {
						child.TargetMainSize = child.FlexBaseSize
					}
				}
			} else {
				var scaledFlexShrinkFactorsSum, flexGrowFactorsSum pr.Float
				for _, v := range line.line {
					child := v.box.Box()
					if !child.Frozen {
						child.ScaledFlexShrinkFactor = child.FlexBaseSize * child.Style.GetFlexShrink()
						scaledFlexShrinkFactorsSum += child.ScaledFlexShrinkFactor
						flexGrowFactorsSum += child.Style.GetFlexGrow()
					}
				}
				for _, v := range line.line {
					child := v.box.Box()
					if !child.Frozen {
						if flexFactorType == "grow" {
							// If using the flex grow factor…
							ratio := child.Style.GetFlexGrow() / flexGrowFactorsSum
							child.TargetMainSize = child.FlexBaseSize + remainingFreeSpace*ratio
						} else if flexFactorType == "shrink" {
							// If using the flex shrink factor…
							if scaledFlexShrinkFactorsSum == 0 {
								child.TargetMainSize = child.FlexBaseSize
							} else {
								ratio := child.ScaledFlexShrinkFactor / scaledFlexShrinkFactorsSum
								child.TargetMainSize = child.FlexBaseSize + remainingFreeSpace*ratio
							}
						}
					}
				}
			}

			// 9.7.5.d Fix min/max violations.
			for _, v := range line.line {
				child := v.box.Box()
				child.Adjustment = 0
				if !child.Frozen {
					// gopdfgo: rozmiar docelowy przycinamy do minimum i maksimum, a różnicę zapisujemy jako
					// korektę ze znakiem (CSS Flexbox 1 § 9.7 krok 5d). W źródle rozmiar był przycinany już
					// przy podziale wolnego miejsca, a tutaj wykrywane było tylko naruszenie minimum, więc
					// miejsce odebrane elementowi z max-width nie wracało do pozostałych.
					minSize := getAttr(child, main, "min").V()
					maxSize := getAttr(child, main, "max").V()
					clamped := max(max(minSize, min(child.TargetMainSize, maxSize)), 0)
					if _, isReplaced := v.box.(bo.ReplacedBoxITF); isReplaced && main == pr.PWidth {
						// minimum i maksimum wysokości przeniesione przez proporcje obrazu
						clamped = minMax(v.box, clamped)
					}
					// Przy nieskończonym wolnym miejscu różnica dałaby NaN i pętla nie miałaby końca.
					if clamped != child.TargetMainSize {
						child.Adjustment = clamped - child.TargetMainSize
					}
					child.TargetMainSize = clamped
				}
			}

			// 9.7.5.e Freeze over-flexed items.
			adjustments := line.adjustements()
			for _, v := range line.line {
				child := v.box.Box()
				if adjustments == 0 {
					child.Frozen = true
				} else if adjustments > 0 && child.Adjustment > 0 {
					child.Frozen = true
				} else if adjustments < 0 && child.Adjustment < 0 {
					child.Frozen = true
				} else if adjustments != adjustments { // NaN: zamrażamy wszystko, żeby pętla się skończyła
					child.Frozen = true
				}
			}
		}
		// 9.7.6 Set each item’s used main size to its target main size.
		for _, v := range line.line {
			child := v.box.Box()
			if main == pr.PWidth {
				child.Width = child.TargetMainSize
			} else {
				child.Height = child.TargetMainSize
				// wysokość podana wprost pozostaje określona, o ile to ona wyznacza rozmiar główny, czyli
				// flex-basis to auto; przy flex-basis: content albo podanej długości rozmiar wynika z układu
				// flex i w kontenerze o nieokreślonej wysokości jest nieokreślony (CSS Flexbox 1 § 9.8)
				specified := child.Style.GetHeight()
				basis := child.Style.GetFlexBasis()
				// element całkowicie nieelastyczny z flex-basis podanym długością ma rozmiar określony
				inflexible := basis.Tag == 0 && basis.Unit != pr.Perc &&
					child.Style.GetFlexGrow() == 0 && child.Style.GetFlexShrink() == 0
				child.IndefiniteHeight = mainIsIndefinite && !inflexible &&
					(specified.Tag == pr.Auto || specified.Unit == pr.Perc || basis.Tag != pr.Auto)
			}
		}
	}

	noBaseline := map[*bo.BoxFields]bool{} // gopdfgo: elementy bez linii bazowej (bez wiersza tekstu)

	// 7 Determine the hypothetical cross size of each item.
	var newFlexLines []flexLine
	childSkipStack = skipStack
	for _, line := range flexLines {
		var newFlexLine flexLine
		for _, v := range line.line {
			child_ := v.box
			child := child_.Box()
			// TODO: Fix this value, see test_flex_item_auto_margin_cross.
			if child.MarginTop == pr.AutoF {
				child.MarginTop = pr.Float(0)
			}
			if child.MarginBottom == pr.AutoF {
				child.MarginBottom = pr.Float(0)
			}
			// TODO: Find another way than calling block_level_layout_switch.
			newChild_ := child_.Copy()
			newChild_, tmp, _ := blockLevelLayoutSwitch(context, newChild_.(bo.BlockLevelBoxITF), -pr.Inf, pickSkip(v.index, childSkipStack),
				parentBox, pageIsEmpty, absoluteBoxes, fixedBoxes, new([]pr.Float), discard, -1)
			adjoiningMargins := tmp.adjoiningMargins
			child.Baseline = pr.Float(0)
			// przy „last baseline” zostaje dotychczasowe szukanie w głębi (bez linii bazowej zagnieżdżonego kontenera flex);
			// gopdfgo: szukamy wtedy ostatniej linii bazowej (CSS Align 3 § 9.1), źródło brało zawsze pierwszą
			itemAlign := child.Style.GetAlignSelf()
			if itemAlign.Has(pr.Auto) {
				itemAlign = box.Style.GetAlignItems()
			}
			if bl := findBaseline(newChild_, itemAlign.Has(pr.Last), !itemAlign.Has(pr.Last)); bl != nil {
				child.Baseline = bl.V()
			} else {
				noBaseline[child] = true
			}
			if cross == pr.PHeight {
				child.Height = newChild_.Box().Height
				// As flex items margins never collapse (with other flex items or
				// with the flex container), we can add the adjoining margins to the
				// child height.
				child.Height = child.Height.V() + collapseMargin(adjoiningMargins)
			} else {
				if child.Width == pr.AutoF {
					minWidth := minContentWidth(context, child_, false)
					maxWidth := maxContentWidth(context, child_, false)
					child.Width = min(max(minWidth, newChild_.Box().Width.V()), maxWidth)
					// gopdfgo: rozmiar hipotetyczny respektuje min-width i max-width elementu (CSS Flexbox 1
					// § 9.4 krok 7: rozmiar z układu); szerokości wewnętrzne liczą procentowe min-width jako
					// zero, więc obraz z min-width: 20% dostawał szerokość własną (test WPT
					// flex-aspect-ratio-img-column-007)
					if minimum, ok := child.MinWidth.(pr.Float); ok && child.Width.V() < minimum {
						child.Width = minimum
					}
				} else {
					child.Width = newChild_.Box().Width
				}
			}

			newFlexLine.line = append(newFlexLine.line, indexedBox{index: v.index, box: child_, measured: newChild_})

			// Skip stack is only for the first child
			childSkipStack = nil
		}
		if len(newFlexLine.line) != 0 {
			newFlexLines = append(newFlexLines, newFlexLine)
		}
	}
	flexLines = newFlexLines

	// 8 Calculate the cross size of each flex line.
	crossSize := getAttr(box, cross, "")
	// gopdfgo: „jednowierszowy” to flex-wrap: nowrap (CSS Flexbox 1 § 6); kontener z zawijaniem, który
	// ma tylko jedną linię, nadal jest wielowierszowy: linia ma rozmiar elementów, a rozmiar kontenera
	// rozdziela align-content (test WPT flex-wrap-005). Źródło sprawdzało tylko liczbę linii.
	singleLine := box.Style.GetFlexWrap() == "nowrap"
	if height, ok := parentBox.Height.(pr.Float); ok && cross == pr.PHeight && crossSize != pr.AutoF {
		// gopdfgo: wysokość treści po uwzględnieniu box-sizing; źródło brało tu wysokość pudełka
		// obramowania, więc elementy kontenera z box-sizing: border-box były rozciągane za wysoko
		crossSize = height
	}
	if len(flexLines) == 1 && crossSize != pr.AutoF && singleLine {
		// If the flex container is single-line…
		flexLines[0].crossSize = crossSize.V()
	} else {
		// Otherwise, for each flex line…
		// 8.1 Collect all the flex items whose inline-axis is parallel to the main-axis…
		for index, line := range flexLines {
			var collectedItems, notCollectedItems []*bo.BoxFields
			for _, v := range line.line {
				child := v.box.Box()
				alignSelf := child.Style.GetAlignSelf()
				// gopdfgo: align-self: auto przyjmuje wartość align-items kontenera (CSS Align 3 § 6.1);
				// źródło sprawdzało tu samo align-self, więc elementy wyrównane do linii bazowej przez
				// align-items nie poszerzały linii o różnicę swoich linii bazowych
				if alignSelf.Has(pr.Auto) {
					alignSelf = box.Style.GetAlignItems()
				}
				collect := strings.HasPrefix(string(box.Style.GetFlexDirection()), "row") && alignSelf.Has(pr.Baseline) &&
					child.MarginTop != pr.AutoF && child.MarginBottom != pr.AutoF
				if collect {
					collectedItems = append(collectedItems, child)
				} else {
					notCollectedItems = append(notCollectedItems, child)
				}
			}
			// gopdfgo: odległości mogą być ujemne (linia bazowa poza elementem); ich suma to nadal rozmiar
			// linii (CSS Flexbox 1 § 9.4 krok 8.1, test WPT baseline-outside-flex-item). Źródło zaczynało od
			// zera, co przy jednym elemencie z linią bazową poza nim dawało za wysoką linię.
			// gopdfgo: elementy z „last baseline” tworzą osobną grupę wyrównania (CSS Align 3 § 9.3), która
			// też wyznacza rozmiar linii; źródło łączyło obie grupy w jedną.
			var collectedCrossSize pr.Float
			for _, last := range [2]bool{false, true} {
				crossStartDistance, crossEndDistance := -pr.Inf, -pr.Inf
				for _, child := range collectedItems {
					if isLastBaseline(child, box) != last {
						continue
					}
					baseline := child.Baseline.V() - child.PositionY
					crossStartDistance = max(crossStartDistance, baseline)
					crossEndDistance = max(crossEndDistance, child.MarginHeight()-baseline)
				}
				if crossStartDistance > -pr.Inf {
					collectedCrossSize = max(collectedCrossSize, crossStartDistance+crossEndDistance)
				}
			}
			var nonCollectedCrossSize pr.Float
			// 8.2 Find the largest outer hypothetical cross size.
			if len(notCollectedItems) != 0 {
				nonCollectedCrossSize = -pr.Inf
				for _, child := range notCollectedItems {
					var childCrossSize pr.Float
					if cross == pr.PHeight {
						childCrossSize = child.BorderHeight()
						if child.MarginTop != pr.AutoF {
							childCrossSize += child.MarginTop.V()
						}
						if child.MarginBottom != pr.AutoF {
							childCrossSize += child.MarginBottom.V()
						}
					} else {
						childCrossSize = child.BorderWidth()
						if child.MarginLeft != pr.AutoF {
							childCrossSize += child.MarginLeft.V()
						}
						if child.MarginRight != pr.AutoF {
							childCrossSize += child.MarginRight.V()
						}
					}
					nonCollectedCrossSize = max(childCrossSize, nonCollectedCrossSize)
				}
			}
			// 8.3 Set the used cross-size of the flex line.
			flexLines[index].crossSize = max(collectedCrossSize, nonCollectedCrossSize)
		}
	}

	// 8.3 If the flex container is single-line…
	if len(flexLines) == 1 && singleLine {
		line := &flexLines[0]
		minCrossSize := getAttr(box, cross, "min")
		if minCrossSize == pr.AutoF {
			minCrossSize = -pr.Inf
		}
		maxCrossSize := getAttr(box, cross, "max")
		if maxCrossSize == pr.AutoF {
			maxCrossSize = pr.Inf
		}
		line.crossSize = max(minCrossSize.V(), min(line.crossSize, maxCrossSize.V()))
	}

	// 9 Handle 'align-content: stretch'.
	alignContent := box.Style.GetAlignContent()
	if alignContent.Has(pr.Normal) {
		alignContent = pr.JustifyOrAlign{pr.Stretch}
	}
	if alignContent.Has(pr.Stretch) {
		var definiteCrossSize pr.MaybeFloat
		if he := box.Style.GetHeight(); cross == pr.PHeight && he.Tag != pr.Auto && (he.Unit != pr.Perc || box.Height != pr.AutoF) {
			// gopdfgo: wysokość procentowa przy nieokreślonej wysokości bloku zawierającego (rozwiązana do auto)
			// nie jest określona (CSS 2 § 10.5); źródło brało wtedy liczbę procentów jako piksele
			definiteCrossSize = he.Value
			// gopdfgo: wysokość treści po rozwiązaniu box-sizing i procentów; źródło brało surową wartość
			// ze stylu, więc w kontenerze z box-sizing: border-box linia była wyższa o obramowanie i odstępy
			if height, ok := parentBox.Height.(pr.Float); ok {
				definiteCrossSize = height
			}
		} else if height, ok := parentBox.Height.(pr.Float); ok && cross == pr.PHeight {
			// gopdfgo: wysokość nadana z zewnątrz (rozciągnięcie w kontenerze nadrzędnym, aspect-ratio) też jest
			// określona (CSS Flexbox 1 § 9.8; test WPT stretched-child-in-nested-flexbox-002)
			definiteCrossSize = height
		} else if cross == pr.PWidth {
			if bo.FlexT.IsInstance(box_) {
				if box.Style.GetWidth().Tag == pr.Auto {
					// gopdfgo: szerokość auto kontenera blokowego ograniczają jego min-width i max-width
					// (CSS 2 § 10.4); źródło rozciągało linię do całej dostępnej szerokości (test WPT
					// flexbox-single-line-clamp-2)
					available := availableCrossSpace
					if maxWidth, ok := parentBox.MaxWidth.(pr.Float); ok {
						available = min(available, maxWidth)
					}
					if minWidth, ok := parentBox.MinWidth.(pr.Float); ok {
						available = max(available, minWidth)
					}
					definiteCrossSize = available
				} else if width, ok := parentBox.Width.(pr.Float); ok {
					// gopdfgo: szerokość treści po rozwiązaniu procentów, calc() i box-sizing; źródło brało
					// liczbę ze stylu, więc kolumna flex z width: 50% miała linię (i rozciągnięte elementy)
					// szerokości 50px, a elementy zmierzone w pełnej szerokości nachodziły na siebie
					definiteCrossSize = width
				} else {
					definiteCrossSize = box.Style.GetWidth().Value
				}
			}
		}
		if definiteCrossSize != nil {
			extraCrossSize := definiteCrossSize.V()
			for _, line := range flexLines {
				extraCrossSize -= line.crossSize
			}
			extraCrossSize -= pr.Float(max(len(flexLines)-1, 0)) * crossGap

			// gopdfgo: w kontenerze wielowierszowym align-content: stretch tylko powiększa linie, gdy zostaje
			// wolne miejsce (CSS Flexbox 1 § 9.4 krok 9); przy przepełnieniu źródło je zmniejszało i linie
			// wychodziły ponad kontener (test WPT flexbox-overflow-horiz-004). W jednowierszowym linia ma
			// po prostu rozmiar kontenera (krok 8), także mniejszy od elementów.
			if extraCrossSize > 0 || (extraCrossSize != 0 && singleLine) {
				for i, line := range flexLines {
					line.crossSize += extraCrossSize / pr.Float(len(flexLines))
					flexLines[i] = line
				}
			}
		}
	}

	// 10 Collapse 'visibility: collapse' items.
	if !collapsedPass {
		collapsed := map[Box]pr.Float{}
		for _, line := range flexLines {
			for _, v := range line.line {
				if child := v.box.Box(); child.Style.GetVisibility() == "collapse" && !child.IsTableWrapper {
					collapsed[v.box] = line.crossSize
				}
			}
		}
		if len(collapsed) != 0 {
			for k, child := range children {
				if size, ok := collapsed[child]; ok {
					children[k] = collapsedStrut(child, size, main)
					struts[children[k].Box()] = true
				}
			}
			*box = boxBeforeLines
			*absoluteBoxes, *fixedBoxes = (*absoluteBoxes)[:absoluteBefore], (*fixedBoxes)[:fixedBefore]
			collapsedPass = true
			goto restartCollapsed
		}
	}

	// 11 Determine the used cross size of each flex item.

	alignItems := box.Style.GetAlignItems()
	if alignItems.Has(pr.Normal) {
		alignItems = pr.JustifyOrAlign{pr.Stretch}
	}
	for _, line := range flexLines {
		for _, v := range line.line {
			child := v.box.Box()
			alignSelf := child.Style.GetAlignSelf()
			if alignSelf.Has(pr.Normal) {
				alignSelf = pr.JustifyOrAlign{pr.Stretch}
			} else if alignSelf.Has(pr.Auto) {
				alignSelf = alignItems
			}
			crossStyle := child
			if child.IsTableWrapper {
				// gopdfgo: o tym, czy tabela ma rozmiar auto, decyduje styl tabeli, nie pudełka opakowującego
				crossStyle = child.GetWrappedTable().Box()
			}
			if crossMargins := getCrossMargins(child, cross); cross == pr.PHeight && getDimOrS(crossStyle, cross).Tag == pr.Auto &&
				(!alignSelf.Has(pr.Stretch) || crossMargins[0] == pr.AutoF || crossMargins[1] == pr.AutoF) {
				// gopdfgo: wysokość elementu nierozciąganego wynika z treści i nie jest określona, więc nie
				// jest podstawą procentów jego dzieci (CSS Flexbox 1 § 9.8; określony jest tylko rozmiar
				// elementu rozciągniętego, § 9.4 krok 11). Źródło liczyło procent od wysokości z kroku 7
				// (test WPT flexbox_align-items-stretch-4).
				child.IndefiniteHeight = true
			}
			// gopdfgo: width: max-content (i inne słowa rozmiaru wewnętrznego) ma w stylu znacznik auto, ale nie
			// jest rozmiarem auto, więc element nie jest rozciągany (CSS Flexbox 1 § 9.4 krok 11)
			if alignSelf.Has(pr.Stretch) && getDimOrS(crossStyle, cross).Tag == pr.Auto && getDimOrS(crossStyle, cross).Intrinsic == pr.IntrinsicNone {
				crossMargins := getCrossMargins(child, cross)
				if !(crossMargins[0] == pr.AutoF || crossMargins[1] == pr.AutoF) {
					crossSize := line.crossSize
					if cross == pr.PHeight {
						crossSize -= child.MarginTop.V() + child.MarginBottom.V() +
							child.PaddingTop.V() + child.PaddingBottom.V() + child.BorderTopWidth.V() + child.BorderBottomWidth.V()
					} else {
						crossSize -= child.MarginLeft.V() + child.MarginRight.V() +
							child.PaddingLeft.V() + child.PaddingRight.V() + child.BorderLeftWidth.V() + child.BorderRightWidth.V()
					}
					if cross == pr.PWidth {
						child.Width = crossSize
					} else {
						child.Height = crossSize
					}
				}
			} // else: Cross size has been set by step 7
		}
	}

	// 12 Distribute any remaining free space.
	// gopdfgo: o tym, czy margines jest auto, rozstrzyga styl, a nie pole pudełka: w kierunku kolumny
	// wcześniejszy układ elementu zdążył już zamienić marginesy auto na zero i wolne miejsce szło do
	// justify-content zamiast do marginesu (CSS Flexbox 1 § 8.1).
	originalPositionMain := box.ContentBoxY()
	if main == pr.PWidth {
		originalPositionMain = box.ContentBoxX()
	}
	justifyContent := box.Style.GetJustifyContent()
	if justifyContent.Has(pr.Normal) {
		justifyContent = pr.JustifyOrAlign{pr.FlexStart}
	}
	// gopdfgo: „safe”: gdy elementy nie mieszczą się w osi głównej, wyrównanie działa jak start
	// (CSS Align 3 § 4.4); źródło ignorowało to słowo. Zapamiętujemy je, bo zamiana flex-start
	// i flex-end niżej zostawia samą wartość położenia.
	safeJustify := justifyContent.Has(pr.Safe)
	if strings.HasSuffix(string(box.Style.GetFlexDirection()), "-reverse") {
		if justifyContent.Has(pr.FlexStart) {
			justifyContent = pr.JustifyOrAlign{pr.FlexEnd}
		} else if justifyContent.Has(pr.FlexEnd) {
			justifyContent = pr.JustifyOrAlign{pr.FlexStart}
		}
		// gopdfgo: start i end odnoszą się do kierunku pisma, nie do osi głównej (CSS Align 3 § 4.1),
		// więc przy kierunku odwróconym się nie zamieniają; źródło zamieniało je jak flex-start i flex-end.
	}

	for _, line := range flexLines {
		positionMain := originalPositionMain
		var freeSpace pr.Float
		if main == pr.PWidth {
			freeSpace = box.Width.V()
			for _, v := range line.line {
				child := v.box.Box()
				freeSpace -= child.BorderWidth()
				if !marginIsAuto(child, pr.PMarginLeft) {
					freeSpace -= child.MarginLeft.V()
				}
				if !marginIsAuto(child, pr.PMarginRight) {
					freeSpace -= child.MarginRight.V()
				}
			}
		} else {
			freeSpace = box.Height.V()
			for _, v := range line.line {
				child := v.box.Box()
				freeSpace -= child.BorderHeight()
				if !marginIsAuto(child, pr.PMarginTop) {
					freeSpace -= child.MarginTop.V()
				}
				if !marginIsAuto(child, pr.PMarginBottom) {
					freeSpace -= child.MarginBottom.V()
				}
			}
		}
		for i := range line.line {
			freeSpace -= lineGap(line.line, i)
		}

		// 12.1 If the remaining free space is positive…
		var margins pr.Float
		for _, v := range line.line {
			child := v.box.Box()
			if main == pr.PWidth {
				if marginIsAuto(child, pr.PMarginLeft) {
					margins += 1
				}
				if marginIsAuto(child, pr.PMarginRight) {
					margins += 1
				}
			} else {
				if marginIsAuto(child, pr.PMarginTop) {
					margins += 1
				}
				if marginIsAuto(child, pr.PMarginBottom) {
					margins += 1
				}
			}
		}
		if margins != 0 {
			// gopdfgo: marginesy auto dostają tylko dodatnie wolne miejsce; przy przepełnieniu są zerem
			// (CSS Flexbox 1 § 9.5 krok 12.1). W źródle dostawały wartość ujemną i elementy nachodziły na siebie.
			remaining := freeSpace
			freeSpace = max(freeSpace, 0)
			freeSpace /= margins
			for _, v := range line.line {
				child := v.box.Box()
				if main == pr.PWidth {
					if marginIsAuto(child, pr.PMarginLeft) {
						child.MarginLeft = freeSpace
					}
					if marginIsAuto(child, pr.PMarginRight) {
						child.MarginRight = freeSpace
					}
				} else {
					if marginIsAuto(child, pr.PMarginTop) {
						child.MarginTop = freeSpace
					}
					if marginIsAuto(child, pr.PMarginBottom) {
						child.MarginBottom = freeSpace
					}
				}
			}
			freeSpace = min(remaining, 0)
		}

		// gopdfgo: gdy wolnego miejsca brakuje albo w linii jest jeden element,
		// space-between działa jak flex-start, a space-around i space-evenly
		// jak „safe center” (CSS Box Alignment 3 § 4.3). Źródło
		// rozdzielało wtedy ujemne odstępy, a pojedynczy element przy
		// kierunku odwróconym zostawiało po złej stronie.
		justifyContent := justifyContent
		if freeSpace < 0 || len(line.line) == 1 {
			flexStart := pr.JustifyOrAlign{pr.FlexStart}
			if strings.HasSuffix(string(box.Style.GetFlexDirection()), "-reverse") {
				flexStart = pr.JustifyOrAlign{pr.FlexEnd}
			}
			if justifyContent.Has(pr.SpaceBetween) {
				justifyContent = flexStart
			} else if justifyContent.Intersects(pr.SpaceAround, pr.SpaceEvenly) {
				// wyrównanie zastępcze to „safe center”: środek, a gdy
				// treść się nie mieści, początek
				justifyContent = pr.JustifyOrAlign{pr.Center}
				if freeSpace < 0 {
					justifyContent = flexStart
				}
			}
		}

		if freeSpace < 0 && safeJustify {
			justifyContent = pr.JustifyOrAlign{pr.FlexStart}
		}

		if box.Style.GetDirection() == pr.Rtl && main == pr.PWidth {
			freeSpace = -freeSpace
		}

		// 12.2 Align the items along the main-axis per justify-content.
		// gopdfgo: left i right odnoszą się do osi poziomej; w kontenerze
		// kolumnowym działają jak start (CSS Box Alignment 3 § 4.1), a źródło
		// traktowało right jak koniec osi głównej
		towardsEnd := justifyContent.Intersects(pr.FlexEnd, pr.End) || (justifyContent.Has(pr.Right) && main == pr.PWidth)
		if towardsEnd {
			positionMain += freeSpace
		} else if justifyContent.Has(pr.Center) {
			positionMain += freeSpace / 2
		} else if justifyContent.Has(pr.SpaceAround) {
			positionMain += freeSpace / pr.Float(len(line.line)) / 2
		} else if justifyContent.Has(pr.SpaceEvenly) {
			positionMain += freeSpace / (pr.Float(len(line.line)) + 1)
		}

		var growths pr.Float
		for _, child := range children {
			growths += child.Box().Style.GetFlexGrow()
		}
		for i, v := range line.line {
			child := v.box.Box()
			positionMain += lineGap(line.line, i)
			if main == pr.PWidth {
				child.PositionX = positionMain
				if justifyContent.Has(pr.Stretch) && growths != 0 {
					child.Width = child.Width.V() + freeSpace*child.Style.GetFlexGrow()/growths
				}
			} else {
				child.PositionY = positionMain
			}

			var marginMain pr.Float
			if main == pr.PWidth {
				marginMain = child.MarginWidth()
			} else {
				marginMain = child.MarginHeight()
			}
			if box.Style.GetDirection() == pr.Rtl && main == pr.PWidth {
				marginMain *= -1
			}
			positionMain += marginMain

			if justifyContent.Has(pr.SpaceAround) {
				positionMain += freeSpace / pr.Float(len(line.line))
			} else if justifyContent.Has(pr.SpaceBetween) {
				if len(line.line) > 1 {
					positionMain += freeSpace / (pr.Float(len(line.line)) - 1)
				}
			} else if justifyContent.Has(pr.SpaceEvenly) {
				positionMain += freeSpace / (pr.Float(len(line.line)) + 1)
			}
		}
	}

	firstLineWithoutBaseline := false // gopdfgo: zob. krok 13

	// 13 Resolve cross-axis auto margins.
	if cross == pr.PWidth {
		// Make sure width/margins are no longer "auto", as we did not do it above in
		// step 4.
		blockLevelWidth(box_, context, containingBlock)
	}
	positionCross := box.ContentBoxX()
	if cross == pr.PHeight {
		positionCross = box.ContentBoxY()
	}
	// gopdfgo: grupa „last baseline” każdej linii: największe odległości linii bazowej od krawędzi
	// początkowej i końcowej; grupa stoi przy końcu linii (CSS Align 3 § 9.3, wyrównanie zastępcze
	// safe end)
	lastBelow, lastAbove := make([]pr.Float, len(flexLines)), make([]pr.Float, len(flexLines))
	for i := range flexLines {
		lastBelow[i], lastAbove[i] = -pr.Inf, -pr.Inf
	}
	for index, line := range flexLines {
		line.lowerBaseline = -pr.Inf
		// TODO: Don't duplicate this loop
		for _, v := range line.line {
			child := v.box.Box()
			alignSelf := child.Style.GetAlignSelf()
			if alignSelf.Has(pr.Auto) {
				alignSelf = box.Style.GetAlignItems()
			}
			if alignSelf.Has(pr.Baseline) && main == pr.PWidth {
				// TODO: handle vertical text
				child.Baseline = child.Baseline.V() - positionCross
				if alignSelf.Has(pr.Last) {
					lastBelow[index] = max(lastBelow[index], child.MarginHeight()-child.Baseline.V())
					lastAbove[index] = max(lastAbove[index], child.Baseline.V())
					continue
				}
				line.lowerBaseline = max(line.lowerBaseline, child.Baseline.V())
			}
		}
		if line.lowerBaseline == -pr.Inf && lastAbove[index] > -pr.Inf {
			line.lowerBaseline = lastAbove[index] // tylko grupa „last baseline”: jak dotąd
		}
		if line.lowerBaseline == -pr.Inf {
			if len(line.line) != 0 {
				line.lowerBaseline = line.line[0].box.Box().Baseline.V()
				// gopdfgo: element bez wiersza tekstu nie daje linii bazowej linii; kontener syntetyzuje
				// ją wtedy ze swojej krawędzi (CSS Flexbox 1 § 8.5, CSS Align 3 § 9.1). Źródło brało
				// zero, więc pusty element podnosił kontener inline-flex nad linią bazową wiersza.
				if noBaseline[line.line[0].box.Box()] && index == 0 {
					firstLineWithoutBaseline = true
				}
			} else {
				line.lowerBaseline = 0
			}
		}
		for _, v := range line.line {
			child := v.box.Box()
			crossMargins := getCrossMargins(child, cross)
			var autoMargins pr.Float
			if crossMargins[0] == pr.AutoF {
				autoMargins += 1
			}
			if crossMargins[1] == pr.AutoF {
				autoMargins += 1
			}
			// If a flex item has auto cross-axis margins…
			if autoMargins != 0 {
				// gopdfgo: element z marginesem auto też zaczyna się na początku swojej linii; w źródle
				// zostawał na pozycji sprzed układu linii i przesuwał całą linię przy align-content.
				if cross == pr.PHeight {
					child.PositionY = positionCross
				} else {
					child.PositionX = positionCross
				}
				extraCross := line.crossSize
				if cross == pr.PHeight {
					extraCross -= child.BorderHeight()
					if !marginIsAuto(child, pr.PMarginTop) {
						extraCross -= child.MarginTop.V()
					}
					if !marginIsAuto(child, pr.PMarginBottom) {
						extraCross -= child.MarginBottom.V()
					}
				} else {
					extraCross -= child.BorderWidth()
					if !marginIsAuto(child, pr.PMarginLeft) {
						extraCross -= child.MarginLeft.V()
					}
					if !marginIsAuto(child, pr.PMarginRight) {
						extraCross -= child.MarginRight.V()
					}
				}
				if extraCross > 0 {
					// If its outer cross size is less than the cross size…
					extraCross /= autoMargins
					if cross == pr.PHeight {
						if marginIsAuto(child, pr.PMarginTop) {
							child.MarginTop = extraCross
						}
						if marginIsAuto(child, pr.PMarginBottom) {
							child.MarginBottom = extraCross
						}
					} else {
						if marginIsAuto(child, pr.PMarginLeft) {
							child.MarginLeft = extraCross
						}
						if marginIsAuto(child, pr.PMarginRight) {
							child.MarginRight = extraCross
						}
					}
				} else {
					// Otherwise…
					if cross == pr.PHeight {
						if marginIsAuto(child, pr.PMarginTop) {
							child.MarginTop = pr.Float(0)
						}
						child.MarginBottom = extraCross
					} else {
						if marginIsAuto(child, pr.PMarginLeft) {
							child.MarginLeft = pr.Float(0)
						}
						child.MarginRight = extraCross
					}
				}
			} else {
				// 14 Align all flex items along the cross-axis.
				alignSelf := child.Style.GetAlignSelf()
				if alignSelf.Has(pr.Normal) {
					alignSelf = pr.JustifyOrAlign{pr.Stretch}
				} else if alignSelf.Has(pr.Auto) {
					alignSelf = alignItems
				}
				if cross == pr.PHeight {
					child.PositionY = positionCross
				} else {
					child.PositionX = positionCross
				}
				// gopdfgo: „safe”: element większy niż linia stoi przy jej początku (CSS Align 3 § 4.4);
				// źródło ignorowało to słowo w align-self i align-items
				outerCross := child.MarginWidth()
				if cross == pr.PHeight {
					outerCross = child.MarginHeight()
				}
				if alignSelf.Has(pr.Safe) && outerCross > line.crossSize {
					alignSelf = pr.JustifyOrAlign{pr.FlexStart}
				}
				if alignSelf.Intersects(pr.End, pr.SelfEnd, pr.FlexEnd) {
					if cross == pr.PHeight {
						child.PositionY += line.crossSize - child.MarginHeight()
					} else {
						child.PositionX += line.crossSize - child.MarginWidth()
					}
				} else if alignSelf.Has(pr.Center) {
					if cross == pr.PHeight {
						child.PositionY += (line.crossSize - child.MarginHeight()) / 2
					} else {
						child.PositionX += (line.crossSize - child.MarginWidth()) / 2
					}
				} else if alignSelf.Has(pr.Baseline) {
					if cross == pr.PHeight && alignSelf.Has(pr.Last) && main == pr.PWidth && lastBelow[index] > -pr.Inf {
						child.PositionY += line.crossSize - lastBelow[index] - child.Baseline.V()
					} else if cross == pr.PHeight {
						child.PositionY += line.lowerBaseline - child.Baseline.V()
					}
					// TODO: Handle vertical text.
				} else if alignSelf.Has(pr.Stretch) {
					if getDimOrS(child, cross).Tag == pr.Auto {
						var margins pr.Float
						if cross == pr.PHeight {
							margins = child.MarginTop.V() + child.MarginBottom.V()
						} else {
							margins = child.MarginLeft.V() + child.MarginRight.V()
						}
						if child.Style.GetBoxSizing() == "content-box" {
							if cross == pr.PHeight {
								margins += child.BorderTopWidth.V() + child.BorderBottomWidth.V() +
									child.PaddingTop.V() + child.PaddingBottom.V()
							} else {
								margins += child.BorderLeftWidth.V() + child.BorderRightWidth.V() +
									child.PaddingLeft.V() + child.PaddingRight.V()
							}
						}
					}
				}
			}
		}
		positionCross += line.crossSize
		flexLines[index] = line
	}

	// 15 Determine the flex container’s used cross size.
	// TODO: Use the updated algorithm.
	// gopdfgo: wysokość procentowa rozwiązana do auto (nieokreślony blok zawierający, CSS 2 § 10.5) też
	// wynika z linii; źródło zostawiało wtedy wysokość auto, czyli zero (test WPT
	// grid-item-overflow-auto-max-height-percentage)
	if getDimOrS(box, cross).Tag == pr.Auto || (cross == pr.PHeight && box.Height == pr.AutoF) {
		// Otherwise, use the sum of the flex lines' cross sizes…
		// TODO: Handle min-max.
		// TODO: What about align-content here?
		crossSize := sumCross(flexLines)
		crossSize += pr.Float(max(len(flexLines)-1, 0)) * crossGap
		// gopdfgo: rozmiar poprzeczny z linii jest ograniczany przez min- i max- kontenera
		// (CSS Flexbox 1 § 9.4 krok 15); w źródle "TODO: Handle min-max".
		clamp := func(size pr.Float, lower, upper pr.MaybeFloat) pr.Float {
			if v, ok := upper.(pr.Float); ok {
				size = min(size, v)
			}
			if v, ok := lower.(pr.Float); ok {
				size = max(size, v)
			}
			return size
		}
		if cross == pr.PHeight {
			box.Height = clamp(crossSize, box.MinHeight, box.MaxHeight)
		} else if _, known := box.Width.(pr.Float); !known || len(flexLines) < 2 {
			// gopdfgo: szerokość wieloliniowego kontenera kolumnowego wyznaczona już przed układem (blok,
			// shrink-to-fit elementu pływającego) zostaje; linie mogą z niego wystawać (CSS Flexbox 1
			// § 9.2 krok 2, § 9.9.2; test WPT multiline-shrink-to-fit). Źródło brało sumę linii.
			box.Width = clamp(crossSize, box.MinWidth, box.MaxWidth)
		}
	}
	// gopdfgo: przesunięcie każdej linii przez align-content, potrzebne do linii bazowej kontenera
	lineShift := make([]pr.Float, len(flexLines))
	if len(flexLines) > 1 {
		// 15 If the cross size property is a definite size, use that…
		extraCrossSize := getAttr(box, cross, "").V()
		extraCrossSize -= sumCross(flexLines)
		extraCrossSize -= pr.Float(max(len(flexLines)-1, 0)) * crossGap
		// gopdfgo: „safe” w align-content: przy przepełnieniu linie zaczynają się na początku (CSS Align 3
		// § 4.4); źródło przesuwało je przed kontener
		if extraCrossSize < 0 && alignContent.Has(pr.Safe) {
			extraCrossSize = 0
		}
		// 16 Align all flex lines per align-content.
		var crossTranslate pr.Float
		direction := directionX
		if cross == pr.PHeight {
			direction = directionY
		}
		for i, line := range flexLines {
			flexItems := line.flexItems()
			if i != 0 {
				crossTranslate += crossGap
			}
			for _, child := range flexItems {
				currentValue := child.PositionX
				if direction == directionY {
					currentValue = child.PositionY
				}
				setPos(child, direction, currentValue+crossTranslate)
			}
			lineShift[i] = crossTranslate
			if extraCrossSize == 0 {
				continue
			}
			switch {
			case alignContent.Intersects(pr.End, pr.FlexEnd):
				lineShift[i] += extraCrossSize
			case alignContent.Has(pr.Center):
				lineShift[i] += extraCrossSize / 2
			case alignContent.Has(pr.SpaceAround):
				lineShift[i] += extraCrossSize / pr.Float(len(flexLines)) / 2
			case alignContent.Has(pr.SpaceEvenly):
				lineShift[i] += extraCrossSize / (pr.Float(len(flexLines)) + 1)
			}
			for _, child := range flexItems {
				// gopdfgo: każdy element przesuwamy od jego własnej pozycji; źródło brało pozycję
				// ostatniego elementu linii dla wszystkich, gubiąc wyrównanie w osi poprzecznej
				currentValue := child.PositionX
				if direction == directionY {
					currentValue = child.PositionY
				}
				switch {
				case alignContent.Intersects(pr.End, pr.FlexEnd):
					setPos(child, direction, currentValue+extraCrossSize)
				case alignContent.Has(pr.Center):
					setPos(child, direction, currentValue+extraCrossSize/2)
				case alignContent.Has(pr.SpaceAround):
					setPos(child, direction, currentValue+extraCrossSize/pr.Float(len(flexLines))/2)
				case alignContent.Has(pr.SpaceEvenly):
					setPos(child, direction, currentValue+extraCrossSize/(pr.Float(len(flexLines))+1))
				}
			}
			switch {
			case alignContent.Has(pr.SpaceBetween):
				crossTranslate += extraCrossSize / (pr.Float(len(flexLines)) - 1)
			case alignContent.Has(pr.SpaceAround):
				crossTranslate += extraCrossSize / pr.Float(len(flexLines))
			case alignContent.Has(pr.SpaceEvenly):
				crossTranslate += extraCrossSize / (pr.Float(len(flexLines)) + 1)
			}
		}
	}

	// Now we are no longer in the flex algorithm.
	var newChildren []Box
	for _, child := range children {
		if child.Box().IsAbsolutelyPositioned() {
			newChildren = append(newChildren, child)
		}
	}
	box_ = bo.CopyWithChildren(box_, newChildren)
	box = box_.Box()
	// gopdfgo: elementy pływające i pozycjonowane podzielone między strony w układach próbnych elementów
	// (rozmiar hipotetyczny, wkład) zgłaszały swój dalszy ciąg; zgłoszenia te zastępuje ostateczny układ
	// poniżej. W źródle zostawały, więc dalszy ciąg elementu pływającego zagnieżdżonego w elemencie flex
	// był rysowany na następnych stronach podwójnie (testy WPT css-flexbox/break-nested-float-in-flex-item-*).
	if context.flexMeasuring == 0 {
		for key, broken := range context.brokenOutOfFlow {
			if broken.order > brokenAtStart {
				delete(context.brokenOutOfFlow, key)
			}
		}
	}
	childSkipStack = skipStack
	finalLines := make([][]Box, len(flexLines)) // pudełka po ostatecznym układzie, według linii
	// gopdfgo: w wierszu elementy jednej linii dzielą się między strony równolegle (CSS Flexbox 1 § 12):
	// gdy jeden przechodzi na następną stronę, pozostałe z tej linii nadal są układane na bieżącej.
	// W źródle układ linii kończył się na pierwszym podzielonym elemencie, a jego sąsiedzi trafiali
	// dopiero na ostatnią stronę.
	parallel := main == pr.PWidth && !strings.HasSuffix(string(box.Style.GetFlexDirection()), "-reverse") &&
		context.flexMeasuring == 0 && bottomSpace > -pr.Inf
	lineResume := map[int]tree.ResumeStack{}
	// gopdfgo: elementy linii, które nie mieszczą się na stronie i same się nie dzielą, przechodzą na
	// następną pojedynczo, gdy inne elementy tej linii zostają (tak dzielą wiersz flex przeglądarki;
	// CSS Flexbox 1 § 12 zostawia to implementacji). Wcześniej przechodziła cała linia.
	linePushed := map[int]tree.ResumeStack{}
	var lineOverflow []Box               // elementy bieżącej linii wystające poza stronę, które same się nie dzielą
	flexItemsBefore := len(box.Children) // dzieci dodane wcześniej (pozycjonowane bezwzględnie)
	// gopdfgo: w kontenerze kolumnowym element wznowiony z resztą podanej wysokości (podzielony blok
	// niepodzielny, zob. canSplitFixedHeight) został ułożony przez algorytm flex w pełnej wysokości;
	// o różnicę przesuwamy w górę następne elementy i skracamy kontener o wysokości auto. W przeciwnym
	// razie następny element zaczynał się za pełną wysokością i przechodził na kolejną stronę (testy WPT
	// css-page/monolithic-overflow-005, -007).
	var columnShift pr.Float
	for lineIndex, line := range flexLines {
		// gopdfgo: wymuszony podział strony między liniami kontenera wierszowego: break-before na elemencie
		// tej linii albo break-after na elemencie poprzedniej (CSS Flexbox 1 § 12, CSS Break 3 § 3.1).
		// Źródło ignorowało te właściwości na elementach flex.
		if lineIndex > 0 && len(box.Children) > flexItemsBefore && context.flexMeasuring == 0 && bottomSpace > -pr.Inf &&
			main == pr.PWidth && forcedBreakBetweenFlexLines(context, flexLines[lineIndex-1], line) {
			resumeAt = tree.ResumeStack{line.line[0].index: nil}
			break
		}
		lineStartsFragment := len(box.Children) == flexItemsBefore
		// gopdfgo: linia wiersza, która w całości zaczyna się poniżej strony, przechodzi na następną
		// cała, a odstęp row-gap przed nią jest obcinany na podziale (CSS Align 3 § 8.1). Bez tego jej
		// elementy przechodziły pojedynczo (pushItem) z przesunięciem o część odstępu wystającą za stronę.
		if parallel && !lineStartsFragment {
			lineTop, anyItem := pr.Inf, false
			for _, v := range line.line {
				if v.box.Box().IsFlexItem {
					lineTop, anyItem = min(lineTop, v.box.Box().PositionY), true
				}
			}
			if anyItem && lineTop >= pageEdge {
				resumeAt = tree.ResumeStack{line.line[0].index: nil}
				break
			}
		}
		for position, v := range line.line {
			i, child := v.index, v.box.Box()
			// gopdfgo: w kolumnie bez zawijania wymuszony podział strony wypada między elementami (jak wyżej
			// między liniami); w kolumnie z zawijaniem żądanie podziału dotyczy linii, nie strony
			if position > 0 && main == pr.PHeight && box.Style.GetFlexWrap() == "nowrap" && len(box.Children) > flexItemsBefore && context.flexMeasuring == 0 &&
				bottomSpace > -pr.Inf && (forcedBreakAtEdge(context, v.box, true) ||
				forcedBreakAtEdge(context, line.line[position-1].box, false)) {
				resumeAt = tree.ResumeStack{i: nil}
				break
			}
			if child.IsFlexItem {
				if columnShift != 0 {
					v.box.Translate(0, -columnShift, false)
				}
				itemSkip := pickSkip(i, childSkipStack)
				_, resumedRemainder := itemSkip.Remainder()
				plannedHeight := child.MarginHeight()
				// gopdfgo: strona jest „pusta” dla elementu tylko wtedy, gdy przed nim w tym fragmencie
				// kontenera nic nie stoi w osi blokowej: w wierszu żadna wcześniejsza linia, w kolumnie żaden
				// wcześniejszy element tej linii. W źródle każdy element dostawał pageIsEmpty kontenera,
				// więc element dalszej linii, którego margines sięgał za stronę, tracił górny margines
				// (zasada „pierwszy wiersz za wysoki na pustą stronę” w blockContainerLayout) i zostawał
				// na stronie zamiast przejść na następną. Obcinanie marginesów dotyczy tylko marginesów
				// przylegających do podziału (CSS Fragmentation 3 § 5.2), a margines elementu linii
				// zaczynającej się w środku fragmentu do podziału nie przylega (test WPT
				// css-break/flexbox/multi-line-row-flex-fragmentation-064-print).
				itemPageIsEmpty := pageIsEmpty
				if main == pr.PWidth {
					itemPageIsEmpty = pageIsEmpty && lineStartsFragment
				} else {
					itemPageIsEmpty = pageIsEmpty && len(finalLines[lineIndex]) == 0
				}
				// TODO: Don't use block_level_layout_switch.
				newChild, tmp, _ := blockLevelLayoutSwitch(context, v.box.(bo.BlockLevelBoxITF), bottomSpace, itemSkip, box,
					itemPageIsEmpty, absoluteBoxes, fixedBoxes, new([]pr.Float), discard, -1)
				if resumedRemainder && main == pr.PHeight && newChild != nil && lineSkip == nil {
					if shift := plannedHeight - newChild.Box().MarginHeight(); shift > 0 {
						columnShift += shift
					}
				}
				childResumeAt := tmp.resumeAt
				if clipsBlockOverflow(box) {
					childResumeAt = nil // dalszy ciąg byłby przycięty przez kontener
				}
				if newChild == nil && parallel && !pushedBefore(i) && len(box.Children) > flexItemsBefore {
					// gopdfgo: element zaczynający się poniżej strony przechodzi na następną sam, reszta
					// linii jest układana dalej
					pushItem(linePushed, i, child.PositionY+child.MarginTop.V())
				} else if newChild == nil {
					resumeIndex := 0
					if resumeAt != nil {
						resumeIndex, _ = resumeAt.Unpack()
						resumeIndex -= 1
					}
					resumeAt = tree.ResumeStack{resumeIndex + i: nil}
				} else {
					// gopdfgo: element, który nie mieści się na stronie, przechodzi na następną: w kolumnie
					// dzielimy między elementami, w wierszu między liniami (CSS Flexbox 1 § 12). W źródle
					// elementy były układane poniżej dolnej krawędzi strony i ginęły, bo kontener flex
					// nie sprawdzał, czy wychodzą poza stronę.
					bottom := newChild.Box().PositionY + newChild.Box().MarginHeight()
					overflowsPage := childResumeAt == nil && context.flexMeasuring == 0 && !clipsBlockOverflow(box) &&
						context.overflowsPage(bottomSpace, bottom)
					if overflowsPage && parallel {
						// w wierszu decyzja zapada po ułożeniu całej linii (niżej): element rozciągnięty
						// obok dzielonego sąsiada jest przycinany do strony, a nie przenoszony
						lineOverflow = append(lineOverflow, newChild)
					} else if overflowsPage {
						lineStart := len(box.Children) - len(finalLines[lineIndex])
						placedBefore := lineStart
						if main == pr.PHeight {
							placedBefore = len(box.Children)
						}
						if placedBefore == flexItemsBefore && !pageIsEmpty && skip == 0 {
							// nic z kontenera nie zmieściło się na tej stronie: przechodzi cały, żeby na
							// starej stronie nie zostało samo jego obramowanie
							removePlaceholders(context, append(box.Children, newChild), absoluteBoxes, fixedBoxes)
							context.finishFlexFormattingContext(box_)
							return nil, blockLayout{nextPage: tree.PageBreak{Break: "any"}}
						}
						if placedBefore > flexItemsBefore || !pageIsEmpty {
							resumeIndex := i
							if main != pr.PHeight {
								// cała linia przechodzi na następną stronę
								resumeIndex = line.line[0].index
								box.Children = box.Children[:lineStart]
								finalLines[lineIndex] = nil
							}
							removePlaceholders(context, []Box{newChild}, absoluteBoxes, fixedBoxes)
							resumeAt = tree.ResumeStack{resumeIndex: nil}
							break
						}
					}
					box.Children = append(box.Children, newChild)
					finalLines[lineIndex] = append(finalLines[lineIndex], newChild)
					if childResumeAt != nil {
						// fragment elementu nie może sięgać poniżej obszaru strony
						if over := newChild.Box().PositionY + newChild.Box().MarginHeight() - (context.pageBottom - bottomSpace); over > 0 &&
							bottomSpace > -pr.Inf && context.flexMeasuring == 0 && newChild.Box().Height != pr.AutoF {
							newChild.Box().Height = max(newChild.Box().Height.V()-over, 0)
						}
						if parallel {
							// reszta linii zostaje na tej stronie; stan wznowienia zbieramy dla całej linii
							lineResume[i] = childResumeAt
						} else {
							// gopdfgo: i jest już indeksem w pełnej liście dzieci; w źródle doliczany był
							// do niego jeszcze raz indeks wznowienia, co przy drugim podziale dawało
							// indeks spoza listy.
							resumeAt = tree.ResumeStack{i: childResumeAt}
						}
					}
				}
				if resumeAt != nil {
					break
				}
			}

			// Skip stack is only for the first child
			childSkipStack = nil
		}
		if resumeAt == nil && len(lineResume) == 0 && len(lineOverflow) != 0 && len(lineOverflow) < len(finalLines[lineIndex]) {
			// część elementów linii mieści się na stronie: wystające przechodzą na następną pojedynczo
			lineStart := len(box.Children) - len(finalLines[lineIndex])
			kept := box.Children[:lineStart:lineStart]
			var keptLine []Box
			positions := map[Box]int{}
			for position, placed := range finalLines[lineIndex] {
				positions[placed] = position
			}
			itemIndexes := make([]int, 0, len(line.line))
			for _, v := range line.line {
				if v.box.Box().IsFlexItem {
					if _, pushed := linePushed[v.index]; !pushed {
						itemIndexes = append(itemIndexes, v.index)
					}
				}
			}
			for _, placed := range finalLines[lineIndex] {
				index := itemIndexes[positions[placed]]
				if isInBoxes(placed, lineOverflow) && !pushedBefore(index) {
					removePlaceholders(context, []Box{placed}, absoluteBoxes, fixedBoxes)
					pushItem(linePushed, index, placed.Box().PositionY+placed.Box().MarginTop.V())
					continue
				}
				kept = append(kept, placed)
				keptLine = append(keptLine, placed)
			}
			box.Children, finalLines[lineIndex] = kept, keptLine
		} else if resumeAt == nil && len(lineResume) == 0 && len(lineOverflow) != 0 && len(linePushed) == 0 {
			// żaden element linii się nie dzieli, a linia wystaje poza stronę: przechodzi na następną
			lineStart := len(box.Children) - len(finalLines[lineIndex])
			if lineStart == flexItemsBefore && !pageIsEmpty && skip == 0 {
				// nic z kontenera nie zmieściło się na tej stronie: przechodzi cały
				removePlaceholders(context, box.Children, absoluteBoxes, fixedBoxes)
				context.finishFlexFormattingContext(box_)
				return nil, blockLayout{nextPage: tree.PageBreak{Break: "any"}}
			}
			if lineStart > flexItemsBefore || !pageIsEmpty {
				removePlaceholders(context, box.Children[lineStart:], absoluteBoxes, fixedBoxes)
				box.Children = box.Children[:lineStart]
				finalLines[lineIndex] = nil
				resumeAt = tree.ResumeStack{line.line[0].index: nil}
			}
		} else if len(lineResume) != 0 {
			for _, child := range lineOverflow {
				if over := child.Box().PositionY + child.Box().MarginHeight() - (context.pageBottom - bottomSpace); over > 0 && child.Box().Height != pr.AutoF {
					child.Box().Height = max(child.Box().Height.V()-over, 0)
					// element ciągnie się na następną stronę: bez dolnego obramowania i odstępu na tej
					bottomDecoration := child.Box().BorderBottomWidth.V() + child.Box().PaddingBottom.V() + child.Box().MarginBottom.V()
					child.RemoveDecoration(child.Box(), false, true)
					child.Box().Height = child.Box().Height.V() + bottomDecoration
				}
			}
		}
		lineOverflow = lineOverflow[:0]
		if resumeAt == nil && len(lineResume)+len(linePushed) != 0 {
			state := tree.ResumeStack{}
			for _, v := range line.line {
				if v.box.Box().IsFlexItem {
					state[-1-v.index] = lineResume[v.index] // nil: element skończył się na tej stronie
					if marker, pushed := linePushed[v.index]; pushed {
						state[-1-v.index] = marker
					}
				}
			}
			resumeAt = tree.ResumeStack{line.line[0].index: state}
		}
		if resumeAt != nil {
			break
		}
	}

	if columnShift > 0 && box.Style.GetHeight().Tag == pr.Auto {
		box.Height = max(box.Height.V()-columnShift, box.MinHeight.V())
	}
	// gopdfgo: fragment kontenera podzielonego między strony sięga dolnej krawędzi strony, jak fragment
	// zwykłego bloku (CSS Fragmentation 3 § 5.1); inaczej zachowałby wysokość całej treści i sam wystawał
	// poza stronę.
	if resumeAt != nil && box.Height != pr.AutoF && bottomSpace > -pr.Inf && context.flexMeasuring == 0 {
		box.Height = max(context.pageBottom-bottomSpace-box.ContentBoxY(), 0)
	}

	// gopdfgo: pozycja statyczna dziecka pozycjonowanego bezwzględnie to miejsce, które zająłby jedyny
	// element flex o jego rozmiarze: wyrównanie według justify-content w osi głównej i align-self
	// w poprzecznej (CSS Flexbox 1 § 4.1); źródło stawiało je w lewym górnym rogu treści kontenera.
	if width, ok := box.Width.(pr.Float); ok {
		height, _ := box.Height.(pr.Float)
		reverse := strings.HasSuffix(string(box.Style.GetFlexDirection()), "-reverse")
		wrapReverse := box.Style.GetFlexWrap() == "wrap-reverse"
		mainFraction := func() pr.Float {
			jc := box.Style.GetJustifyContent()
			switch {
			case jc.Intersects(pr.Center, pr.SpaceAround, pr.SpaceEvenly):
				return 0.5
			case jc.Has(pr.FlexEnd):
				if reverse {
					return 0
				}
				return 1
			case jc.Intersects(pr.End, pr.Right) && main == pr.PWidth:
				return 1
			case jc.Has(pr.End):
				return 1
			case jc.Intersects(pr.FlexStart, pr.Normal, pr.SpaceBetween, pr.Stretch):
				if reverse {
					return 1
				}
			}
			return 0
		}()
		for _, child_ := range box.Children {
			child := child_.Box()
			if !child.IsAbsolutelyPositioned() || child.StaticAlign != nil {
				continue
			}
			alignSelf := child.Style.GetAlignSelf()
			if alignSelf.Has(pr.Auto) || len(alignSelf) == 0 {
				alignSelf = box.Style.GetAlignItems()
			}
			var crossFraction pr.Float
			switch {
			case alignSelf.Has(pr.Center):
				crossFraction = 0.5
			case alignSelf.Intersects(pr.End, pr.SelfEnd, pr.FlexEnd):
				crossFraction = 1
			}
			if wrapReverse && !alignSelf.Has(pr.Center) {
				crossFraction = 1 - crossFraction
			}
			if alignSelf.Has(pr.Safe) {
				crossFraction += 10
			}
			mainFraction := mainFraction
			if box.Style.GetJustifyContent().Has(pr.Safe) {
				mainFraction += 10
			}
			static := [6]pr.Float{box.ContentBoxX(), box.ContentBoxY(), width, height, mainFraction, crossFraction}
			if main == pr.PHeight {
				static[4], static[5] = crossFraction, mainFraction
			}
			child.StaticAlign = &static
		}
	}

	if box.Style.GetPosition().String == "relative" || ownsFixed {
		// New containing block, resolve the layout of the absolute descendants.
		layoutOwnedPositioned(context, box_, absoluteBoxes, fixedBoxes, ownsFixed, bottomSpace)
	}

	// TODO: Use real algorithm, see https://www.w3.org/TR/css-flexbox-1/#flex-baselines.
	if !bo.InlineFlexT.IsInstance(box_) && main == pr.PWidth && len(flexLines) != 0 && flexLines[0].lowerBaseline > -pr.Inf &&
		box.Style.GetFlexWrap() != "wrap-reverse" {
		// gopdfgo: także kontener blokowy ma linię bazową z pierwszej linii elementów (CSS Flexbox 1
		// § 8.5); korzysta z niej findInFlowBaseline, gdy kontener leży w inline-block, komórce albo
		// innym kontenerze flex. Źródło szukało wtedy pierwszego wiersza tekstu w głębi.
		box.Baseline = flexLines[0].lowerBaseline + lineShift[0]
	}
	if bo.InlineFlexT.IsInstance(box_) {
		// gopdfgo: kontener bez elementu wyznaczającego linię bazową ma ją syntetyzowaną z dolnej krawędzi
		// marginesu (CSS Align 3 § 9.1, kontekst liniowy); źródło przyjmowało górę pudełka, więc pusty
		// inline-flex wisiał pod linią bazową wiersza.
		synthesized := pr.Float(0)
		if _, ok := box.Height.(pr.Float); ok {
			synthesized = box.MarginHeight()
		}
		if main == pr.PWidth { // and main text direction is horizontal
			if len(flexLines) != 0 && !firstLineWithoutBaseline {
				// gopdfgo: z przesunięciem pierwszej linii przez align-content; źródło je pomijało
				box.Baseline = flexLines[0].lowerBaseline + lineShift[0]
			} else {
				box.Baseline = synthesized
			}
		} else {
			box.Baseline = synthesized
			for _, child := range box.Children {
				if child.Box().IsInNormalFlow() {
					var val pr.MaybeFloat
					if len(box.Children) != 0 {
						val = findInFlowBaseline(child, false)
					}
					if val != nil {
						box.Baseline = val.V()
					} else {
						box.Baseline = synthesized
					}
					break
				}
			}
		}
	}

	if box.Style.GetFlexWrap() == "wrap-reverse" && resumeAt == nil {
		for lineIndex, line := range finalLines {
			// Elementy wyrównane do linii bazowej odbijamy razem, jako grupę, żeby zachować wspólną
			// linię bazową; pozostałe każdy z osobna.
			groupTop, groupBottom := pr.Inf, -pr.Inf
			isBaseline := make([]bool, len(line))
			for i, v := range line {
				child := v.Box()
				alignSelf := child.Style.GetAlignSelf()
				if alignSelf.Has(pr.Auto) {
					alignSelf = box.Style.GetAlignItems()
				}
				if alignSelf.Has(pr.Baseline) {
					isBaseline[i] = true
					if cross == pr.PHeight {
						groupTop = min(groupTop, child.PositionY)
						groupBottom = max(groupBottom, child.PositionY+child.MarginHeight())
					} else {
						// gopdfgo: w kolumnie grupa to zakres w osi poziomej (elementy zachowują wspólną krawędź)
						groupTop = min(groupTop, child.PositionX)
						groupBottom = max(groupBottom, child.PositionX+child.MarginWidth())
					}
				}
			}
			for i, v := range line {
				child := v.Box()
				var delta pr.Float
				if cross == pr.PHeight && box.Height != pr.AutoF {
					top := box.ContentBoxY()
					if isBaseline[i] {
						delta = top + box.Height.V() - (groupTop - top) - (groupBottom - groupTop) - groupTop
					} else {
						delta = top + box.Height.V() - (child.PositionY - top) - child.MarginHeight() - child.PositionY
					}
					// gopdfgo: „safe” w align-content: element wyższy niż kontener zostaje przy początku
					// (górze), zamiast wystawać ponad kontener po odbiciu (CSS Align 3 § 4.4)
					if alignContent.Has(pr.Safe) && len(finalLines) == 1 && child.MarginHeight() > box.Height.V() {
						delta = top - child.PositionY
					}
					v.Translate(0, delta, false)
					// gopdfgo: linię bazową kontenera wyznacza linia leżąca najwyżej, czyli po odwróceniu
					// ostatnia (CSS Align 3 § 9.1: pierwsza linia bazowa pochodzi z linii najbliższej
					// początkowi bloku). Źródło brało pierwszą linię flex, która po odwróceniu jest na dole.
					if last := len(finalLines) - 1; lineIndex == last && i == 0 && bo.InlineFlexT.IsInstance(box_) && last < len(flexLines) {
						lineStart := lineShift[last]
						for _, previous := range flexLines[:last] {
							lineStart += previous.crossSize
						}
						box.Baseline = lineStart + flexLines[last].lowerBaseline + delta
					}
				} else if cross == pr.PWidth && box.Width != pr.AutoF {
					left := box.ContentBoxX()
					mirrored := left + box.Width.V() - (child.PositionX - left) - child.MarginWidth()
					if isBaseline[i] {
						mirrored = child.PositionX + (left + box.Width.V() - (groupTop - left) - (groupBottom - groupTop) - groupTop)
					}
					v.Translate(mirrored-child.PositionX, 0, false)
				}
			}
		}
		// Kolejność pudełek jak kolejność linii po odwróceniu: od początku osi poprzecznej.
		sort.SliceStable(box.Children, func(i, j int) bool {
			a, b := box.Children[i].Box(), box.Children[j].Box()
			if !a.IsFlexItem || !b.IsFlexItem {
				return !a.IsFlexItem && b.IsFlexItem
			}
			if cross == pr.PHeight {
				return a.PositionY < b.PositionY
			}
			return a.PositionX < b.PositionX
		})
	}

	box_.RemoveDecoration(box, false, resumeAt != nil && !discard)

	// gopdfgo: element flex z position: relative jest przesuwany o swoje top, right, bottom, left
	// (CSS 2.1 § 9.4.3, CSS Flexbox 1 § 4); w źródle przesunięcie dostawały tylko dzieci bloków.
	if width, ok := box.Width.(pr.Float); ok {
		height, _ := box.Height.(pr.Float)
		for _, child := range box.Children {
			if child.Box().IsFlexItem {
				relativePositioning(child, bo.Point{width, height})
			}
		}
	}

	if traceMode {
		traceLogger.DumpTree(box_, "end flexLayout")
	}

	context.finishFlexFormattingContext(box_)

	// TODO: check these returned values
	return box_, blockLayout{
		resumeAt:          resumeAt,
		nextPage:          tree.PageBreak{Break: "any"},
		adjoiningMargins:  nil,
		collapsingThrough: false,
	}
}

// isScrollContainer mówi, czy pudełko jest kontenerem przewijania: overflow inne niż visible i clip
// (CSS Overflow 3 § 3.1). Tylko dla takich automatyczne minimum elementu flex wynosi zero.
func isScrollContainer(style pr.ElementStyle) bool {
	overflow := style.GetOverflow()
	return overflow != "visible" && overflow != "clip"
}

// parallelLineState rozpoznaje stan wznowienia linii flex dzielonej równolegle: klucze -(indeks+1) dla
// wszystkich elementów linii, wartość nil dla elementów zakończonych. Zwraca mapę indeks -> wznowienie
// albo nil, gdy stos jest zwykłym wznowieniem pierwszego dziecka.
func parallelLineState(stack tree.ResumeStack) map[int]tree.ResumeStack {
	if len(stack) == 0 {
		return nil
	}
	anyUnfinished := false
	for key, value := range stack {
		if key >= 0 {
			return nil
		}
		anyUnfinished = anyUnfinished || value != nil
	}
	if !anyUnfinished {
		return nil
	}
	out := make(map[int]tree.ResumeStack, len(stack))
	for key, value := range stack {
		out[-1-key] = value
	}
	return out
}

// finishedFlexItem zwraca pustą kopię elementu flex, którego treść skończyła się na poprzedniej stronie.
// forcedBreakBetweenFlexLines podaje, czy między dwiema kolejnymi liniami kontenera wierszowego jest
// wymuszony podział strony.
func forcedBreakBetweenFlexLines(context *layoutContext, previous, next flexLine) bool {
	for _, v := range previous.line {
		if forcedBreakAtEdge(context, v.box, false) || forcedBreakAtFlexLineEdge(context, v.measured, false) {
			return true
		}
	}
	for _, v := range next.line {
		if forcedBreakAtEdge(context, v.box, true) || forcedBreakAtFlexLineEdge(context, v.measured, true) {
			return true
		}
	}
	return false
}

// forcedBreakAtFlexLineEdge (gopdfgo) mówi, czy ułożony kontener flex wierszowy z zawijaniem żąda podziału
// strony przed sobą (before) albo po sobie przez elementy swojej pierwszej albo ostatniej linii: w kontenerze
// wierszowym break-before i break-after elementów przechodzą na linię flex, a break-before pierwszej linii
// i break-after ostatniej na sam kontener (CSS Flexbox 1 § 12). forcedBreakAtEdge schodzi tylko do
// pierwszego albo ostatniego dziecka, więc podział żądany przez drugi element pierwszej linii ginął (testy
// WPT css-break/flexbox/multi-line-row-flex-fragmentation-082b, -082c). Linie odtwarzamy z położeń ułożonych
// elementów: w kierunku row nowa linia zaczyna się elementem, który nie stoi na prawo od poprzedniego.
func forcedBreakAtFlexLineEdge(context *layoutContext, measured Box, before bool) bool {
	if measured == nil || !bo.FlexContainerT.IsInstance(measured) {
		return false
	}
	box := measured.Box()
	if box.Style.GetFlexDirection() != "row" || box.Style.GetFlexWrap() != "wrap" {
		return false
	}
	var items []Box
	for _, child := range box.Children {
		if child.Box().IsFlexItem && child.Box().IsInNormalFlow() {
			items = append(items, child)
		}
	}
	if len(items) < 2 {
		return false
	}
	newLine := func(prev, next Box) bool {
		p, n := prev.Box(), next.Box()
		return n.PositionX < p.PositionX+p.MarginWidth()-0.5
	}
	var edge []Box
	if before {
		edge = items[:1]
		for i := 1; i < len(items) && !newLine(items[i-1], items[i]); i++ {
			edge = items[:i+1]
		}
	} else {
		start := len(items) - 1
		for start > 0 && !newLine(items[start-1], items[start]) {
			start--
		}
		edge = items[start:]
	}
	for _, item := range edge {
		if forcedBreakAtEdge(context, item, before) {
			return true
		}
	}
	return false
}

// forcedBreakAtEdge mówi, czy element żąda podziału strony przed sobą (before) albo po sobie. Żądanie
// podziału na pierwszym albo ostatnim dziecku w przepływie przechodzi na rodzica (CSS Break 3 § 3.1),
// więc `break-before` na pierwszym elemencie zagnieżdżonego kontenera flex dzieli strony przed tym
// kontenerem; źródło (i wcześniejszy kod gopdfgo) patrzyło tylko na sam element.
func forcedBreakAtEdge(context *layoutContext, box_ Box, before bool) bool {
	for depth := 0; box_ != nil && depth < 32; depth++ {
		box := box_.Box()
		value := box.Style.GetBreakAfter()
		if before {
			value = box.Style.GetBreakBefore()
		}
		if forcePageBreak(string(value), context) {
			return true
		}
		if !bo.BlockContainerT.IsInstance(box_) && !bo.FlexContainerT.IsInstance(box_) && !bo.GridContainerT.IsInstance(box_) {
			return false
		}
		var next Box
		for i := range box.Children {
			child := box.Children[i]
			if !before {
				child = box.Children[len(box.Children)-1-i]
			}
			if _, isLine := child.(*bo.LineBox); isLine {
				return false
			}
			if child.Box().IsInNormalFlow() {
				next = child
				break
			}
		}
		box_ = next
	}
	return false
}

// stretchedRatioContribution (gopdfgo): wkład elementu z proporcjami do szerokości wewnętrznej
// jednowierszowego kontenera wierszowego o podanej wysokości, gdy element jest rozciągany w osi
// poprzecznej: szerokość wynika z tej wysokości i proporcji (CSS Flexbox 1 § 9.9.1, rozmiar poprzeczny
// rozciągniętego elementu jest określony). Zwraca szerokość zewnętrzną elementu.
func stretchedRatioContribution(container, item Box) (pr.Float, bool) {
	style := container.Box().Style
	if !strings.HasPrefix(string(style.GetFlexDirection()), "row") || style.GetFlexWrap() != "nowrap" {
		return 0, false
	}
	pixels := func(d pr.TaggedDim) (pr.Float, bool) {
		if d.Tag != 0 || d.Unit != pr.Px || d.Calc != nil {
			return 0, false
		}
		return d.Value, true
	}
	height, ok := pixels(style.GetHeight())
	if cb := container.Box(); !ok && style.GetHeight().Tag == pr.Auto && cb.IsAbsolutelyPositioned() &&
		style.GetTop().Tag != pr.Auto && style.GetBottom().Tag != pr.Auto {
		// gopdfgo: wysokość kontenera pozycjonowanego bezwzględnie wynika z top i bottom; jest już
		// wyliczona (zob. absoluteBlock) i jest wysokością treści
		if h, known := cb.Height.(pr.Float); known {
			return stretchedRatioWidth(style, item, h)
		}
	}
	if !ok {
		return 0, false
	}
	if style.GetBoxSizing() == "border-box" {
		for _, d := range [...]pr.TaggedDim{style.GetPaddingTop(), style.GetPaddingBottom(), style.GetBorderTopWidth(), style.GetBorderBottomWidth()} {
			v, _ := pixels(d)
			height -= v
		}
	}
	return stretchedRatioWidth(style, item, height)
}

// stretchedRatioWidth: dalsza część stretchedRatioContribution przy znanej wysokości treści kontenera.
func stretchedRatioWidth(style pr.ElementStyle, item Box, height pr.Float) (pr.Float, bool) {
	pixels := func(d pr.TaggedDim) (pr.Float, bool) {
		if d.Tag != 0 || d.Unit != pr.Px || d.Calc != nil {
			return 0, false
		}
		return d.Value, true
	}
	cs := item.Box().Style
	if cs.GetHeight().Tag != pr.Auto || cs.GetWidth().Tag != pr.Auto || cs.GetMarginTop().Tag == pr.Auto || cs.GetMarginBottom().Tag == pr.Auto {
		return 0, false
	}
	alignSelf := cs.GetAlignSelf()
	if alignSelf.Has(pr.Auto) {
		alignSelf = style.GetAlignItems()
	}
	if !(alignSelf.Has(pr.Stretch) || alignSelf.Has(pr.Normal)) {
		return 0, false
	}
	ratio, ok := itemRatio(item)
	if !ok {
		return 0, false
	}
	sum := func(ds ...pr.TaggedDim) (total pr.Float) {
		for _, d := range ds {
			v, _ := pixels(d)
			total += v
		}
		return total
	}
	frameY := sum(cs.GetPaddingTop(), cs.GetPaddingBottom(), cs.GetBorderTopWidth(), cs.GetBorderBottomWidth())
	frameX := sum(cs.GetPaddingLeft(), cs.GetPaddingRight(), cs.GetBorderLeftWidth(), cs.GetBorderRightWidth())
	marginsX := sum(cs.GetMarginLeft(), cs.GetMarginRight())
	cross := max(height-sum(cs.GetMarginTop(), cs.GetMarginBottom())-frameY, 0)
	width := cross * ratio
	if _, isReplaced := item.(bo.ReplacedBoxITF); !isReplaced && cs.GetBoxSizing() == "border-box" {
		width = max((cross+frameY)*ratio-frameX, 0)
	}
	return width + frameX + marginsX, true
}

// itemRatio (gopdfgo): proporcje elementu flex: własne elementu zastępowanego albo z aspect-ratio
// (CSS Sizing 4 § 5); dla elementu niezastępowanego tylko przy pustej treści, bo treść może ją zmienić.
func itemRatio(item Box) (pr.Float, bool) {
	child := item.Box()
	if replaced, isReplaced := item.(bo.ReplacedBoxITF); isReplaced {
		_, _, ratio := replaced.Replaced().Replacement.GetIntrinsicSize(
			child.Style.GetImageResolution().Value, child.Style.GetFontSize().Value)
		if pr.Is(ratio) && ratio.V() > 0 {
			return ratio.V(), true
		}
		return 0, false
	}
	ratio := pr.Float(math.Abs(float64(child.Style.GetAspectRatio())))
	if ratio <= 0 || len(child.Children) != 0 {
		return 0, false
	}
	return ratio, true
}

// ratioBaseSize podaje rozmiar bazowy elementu zastępowanego z proporcjami, który zostanie rozciągnięty
// w osi poprzecznej do określonego rozmiaru kontenera jednowierszowego (CSS Flexbox 1 § 9.2 krok 3B
// i § 9.9.2: taki rozmiar poprzeczny jest określony).
// basisIsContent: flex-basis: content pomija rozmiar podany w osi głównej.
func ratioBaseSize(item Box, container, parentBox *bo.BoxFields, main pr.KnownProp, basisIsContent bool) (pr.Float, bool) {
	if container.Style.GetFlexWrap() != "nowrap" {
		return 0, false
	}
	child := item.Box()
	ratio, ok := itemRatio(item)
	if !ok {
		return 0, false
	}
	alignSelf := child.Style.GetAlignSelf()
	if alignSelf.Has(pr.Auto) {
		alignSelf = container.Style.GetAlignItems()
	}
	if !(alignSelf.Has(pr.Stretch) || alignSelf.Has(pr.Normal)) {
		return 0, false
	}
	if main == pr.PWidth {
		// wysokość po rozwiązaniu box-sizing i nadana z zewnątrz (parentBox), nie surowa ze stylu
		height, definite := parentBox.Height.(pr.Float)
		if !definite || container.IndefiniteHeight || child.Style.GetHeight().Tag != pr.Auto || (child.Style.GetWidth().Tag != pr.Auto && !basisIsContent) ||
			child.MarginTop == pr.AutoF || child.MarginBottom == pr.AutoF {
			return 0, false
		}
		cross := height - child.MarginTop.V() - child.MarginBottom.V() - child.PaddingTop.V() - child.PaddingBottom.V() -
			child.BorderTopWidth.V() - child.BorderBottomWidth.V()
		cross = max(child.MinHeight.V(), min(cross, child.MaxHeight.V()))
		if _, isReplaced := item.(bo.ReplacedBoxITF); !isReplaced && child.Style.GetBoxSizing() == "border-box" {
			// aspect-ratio z box-sizing: border-box dotyczy pudełka obramowania (CSS Sizing 4 § 5.1)
			frameY := child.PaddingTop.V() + child.PaddingBottom.V() + child.BorderTopWidth.V() + child.BorderBottomWidth.V()
			frameX := child.PaddingLeft.V() + child.PaddingRight.V() + child.BorderLeftWidth.V() + child.BorderRightWidth.V()
			return max((max(cross, 0)+frameY)*ratio-frameX, 0), true
		}
		return max(cross, 0) * ratio, true
	}
	width, definite := container.Width.(pr.Float)
	if !definite || child.Style.GetWidth().Tag != pr.Auto || (child.Style.GetHeight().Tag != pr.Auto && !basisIsContent) ||
		child.MarginLeft == pr.AutoF || child.MarginRight == pr.AutoF {
		return 0, false
	}
	cross := width - child.MarginLeft.V() - child.MarginRight.V() - child.PaddingLeft.V() - child.PaddingRight.V() -
		child.BorderLeftWidth.V() - child.BorderRightWidth.V()
	cross = max(child.MinWidth.V(), min(cross, child.MaxWidth.V()))
	return max(cross, 0) / ratio, true
}

// Element rozciągany do wysokości linii (stretched) zostaje widoczny, bez górnego obramowania i odstępu:
// jego tło i boczne obramowanie ciągną się obok sąsiada, który nadal się dzieli. Pozostałe są niewidoczne
// i tylko trzymają miejsce.
func finishedFlexItem(item Box, stretched bool) Box {
	empty := item.Copy()
	if bo.ParentT.IsInstance(item) {
		empty = bo.CopyWithChildren(item, nil)
	}
	style := item.Box().Style.Copy()
	style.SetHeight(pr.TagToV(pr.Auto))
	style.SetMinHeight(pr.ZeroPixels.Tagged())
	if stretched {
		style.SetBorderTopWidth(pr.ZeroPixels.Tagged())
		style.SetPaddingTop(pr.ZeroPixels.Tagged())
		style.SetMarginTop(pr.ZeroPixels.Tagged())
	} else {
		style.SetVisibility("hidden")
		// margines elementu zostaje na stronie, na której element się zaczął
		style.SetMarginTop(pr.ZeroPixels.Tagged())
	}
	empty.Box().Style = style
	return empty
}

// pushedFlexItem zwraca kopię elementu wiersza flex przesuniętego w całości na następną stronę: górny
// margines jest obcinany na podziale (CSS Fragmentation 3 § 5.2), a element staje tak daleko od góry
// strony, jak daleko poniżej podziału wypadał w układzie bez stron (offset).
func pushedFlexItem(item Box, offset pr.Float) Box {
	pushed := item.Copy()
	style := item.Box().Style.Copy()
	style.SetMarginTop(pr.FToPx(offset))
	style.SetAlignSelf(pr.JustifyOrAlign{pr.FlexStart})
	pushed.Box().Style = style
	return pushed
}

// isStretchedFlexItem mówi, czy element wiersza flex jest rozciągany do wysokości linii: align-self (albo
// align-items kontenera) to stretch lub normal, a wysokość elementu jest auto.
func isStretchedFlexItem(item Box, container *bo.BoxFields) bool {
	alignSelf := item.Box().Style.GetAlignSelf()
	if alignSelf.Has(pr.Auto) {
		alignSelf = container.Style.GetAlignItems()
	}
	return alignSelf.Intersects(pr.Normal, pr.Stretch) && item.Box().Style.GetHeight().Tag == pr.Auto
}

// clipsBlockOverflow mówi, czy kontener o podanej wysokości przycina treść: to, co się w nim nie mieści,
// nie jest widoczne, więc nie przechodzi na następną stronę.
func clipsBlockOverflow(box *bo.BoxFields) bool {
	return box.Style.GetOverflow() != "visible" && box.Style.GetHeight().Tag != pr.Auto
}

// collapsedStrut zamienia element flex z visibility: collapse na rozpórkę (CSS Flexbox 1 § 4.4): pudełko
// bez treści, o zerowym rozmiarze głównym i rozmiarze poprzecznym równym rozmiarowi poprzecznemu linii,
// w której element stał przy pierwszym przebiegu. Rozpórka nie ma marginesów, obramowań ani odstępów
// i nie jest elastyczna.
func collapsedStrut(item Box, crossSize pr.Float, main pr.KnownProp) Box {
	strut := bo.CopyWithChildren(item, nil)
	style := item.Box().Style.Copy()
	zero := pr.ZeroPixels.Tagged()
	for _, set := range []func(pr.TaggedDim){
		style.SetMarginTop, style.SetMarginRight, style.SetMarginBottom, style.SetMarginLeft,
		style.SetPaddingTop, style.SetPaddingRight, style.SetPaddingBottom, style.SetPaddingLeft,
		style.SetBorderTopWidth, style.SetBorderRightWidth, style.SetBorderBottomWidth, style.SetBorderLeftWidth,
		style.SetMinWidth, style.SetMinHeight,
	} {
		set(zero)
	}
	style.SetMaxWidth(pr.FToPx(pr.Inf))
	style.SetMaxHeight(pr.FToPx(pr.Inf))
	style.SetBoxSizing("content-box")
	style.SetFlexGrow(0)
	style.SetFlexShrink(0)
	style.SetFlexBasis(pr.TagToV(pr.Auto))
	style.SetAlignSelf(pr.JustifyOrAlign{pr.FlexStart})
	if main == pr.PWidth {
		style.SetWidth(zero)
		style.SetHeight(pr.FToPx(crossSize))
	} else {
		style.SetHeight(zero)
		style.SetWidth(pr.FToPx(crossSize))
	}
	strut.Box().Style = style
	return strut
}

// ratioOnlyFillWidth (gopdfgo): element zastępowany z samymi proporcjami (bez rozmiaru własnego, np. SVG
// z viewBox), z width i height auto, ma szerokość z równania dla bloku w normalnym przepływie, czyli
// dostępną szerokość pomniejszoną o marginesy, obramowania i odstępy (CSS 2.1 § 10.3.2, zalecenie dla
// takiego przypadku; tak robi Chromium). Źródło brało rozmiar domyślny 300×150 wpisany w proporcje
// (test WPT svg-root-as-flex-item-003).
func ratioOnlyFillWidth(item Box, available pr.Float) (pr.Float, bool) {
	replaced, isReplaced := item.(bo.ReplacedBoxITF)
	if !isReplaced || available == pr.Inf {
		return 0, false
	}
	child := item.Box()
	if child.Style.GetWidth().Tag != pr.Auto || child.Style.GetWidth().Intrinsic != pr.IntrinsicNone || child.Style.GetHeight().Tag != pr.Auto {
		return 0, false
	}
	iwidth, iheight, ratio := replaced.Replaced().Replacement.GetIntrinsicSize(
		child.Style.GetImageResolution().Value, child.Style.GetFontSize().Value)
	iwidth, iheight, ratio = applyPreferredRatio(child.Style, iwidth, iheight, ratio)
	if pr.Is(iwidth) || pr.Is(iheight) || !pr.Is(ratio) || ratio.V() <= 0 {
		return 0, false
	}
	fill := available - child.PaddingLeft.V() - child.PaddingRight.V() - child.BorderLeftWidth.V() - child.BorderRightWidth.V()
	if child.MarginLeft != pr.AutoF {
		fill -= child.MarginLeft.V()
	}
	if child.MarginRight != pr.AutoF {
		fill -= child.MarginRight.V()
	}
	return max(fill, 0), true
}

// captionsHeight (gopdfgo): łączna wysokość zewnętrzna podpisów tabeli w pudełku opakowującym; zero,
// gdy podpisów nie ma. Mierzy kopię pudełka z tabelą o wysokości zero i bez min-height i max-height.
func captionsHeight(context *layoutContext, wrapper Box, bottomSpace pr.Float, containingBlock *bo.BoxFields, pageIsEmpty bool) pr.Float {
	hasCaption := false
	for _, child := range wrapper.Box().Children {
		if bo.TableCaptionT.IsInstance(child) {
			hasCaption = true
		}
	}
	if !hasCaption {
		return 0
	}
	probe := tableWithoutMinMaxHeight(wrapper)
	for _, child := range probe.Box().Children {
		if bo.TableT.IsInstance(child) {
			child.Box().Style.SetHeight(pr.FToPx(0))
			child.Box().Height = pr.Float(0)
		}
	}
	probe.Box().Style = probe.Box().Style.Copy()
	probe.Box().Style.SetHeight(pr.TagToV(pr.Auto))
	probe.Box().Height = pr.AutoF
	context.flexMeasuring++
	measured, _, _ := blockLevelLayout(context, probe.(bo.BlockLevelBoxITF), bottomSpace, nil, containingBlock,
		pageIsEmpty, nil, nil, nil, false, -1)
	context.flexMeasuring--
	if measured == nil {
		return 0
	}
	var total pr.Float
	for _, child := range measured.Box().Children {
		if bo.TableCaptionT.IsInstance(child) {
			total += child.Box().MarginHeight()
		}
	}
	return total
}

// columnItemContribution (gopdfgo): wkład elementu kontenera kolumnowego do jego wysokości wewnętrznej
// (CSS Flexbox 1 § 9.9.1): większa z wartości: wysokość treści i podana wysokość, ograniczona przez
// rozmiar bazowy od góry (element nie rośnie) i od dołu (element się nie kurczy), potem przez min-height
// i max-height. Wysokość treści (bez obramowania i odstępów), jak FlexBaseSize.
func columnItemContribution(context *layoutContext, item Box, parentBox *bo.BoxFields, pageIsEmpty bool) pr.Float {
	child := item.Box()
	probe := item.Copy()
	if bo.ParentT.IsInstance(item) {
		probe = bo.CopyWithChildren(item, child.Children)
	}
	probe.Box().Style = child.Style.Copy()
	probe.Box().Style.SetHeight(pr.TagToV(pr.Auto))
	probe.Box().Style.SetMinHeight(pr.ZeroPixels.Tagged())
	probe.Box().Style.SetMaxHeight(pr.FToPx(pr.Inf))
	if width, ok := child.Width.(pr.Float); ok {
		probe.Box().Style.SetWidth(pr.FToPx(width))
		if child.Style.GetBoxSizing() == "border-box" {
			probe.Box().Style.SetBoxSizing("content-box")
		}
	}
	context.flexMeasuring++
	measured, layout, _ := blockLevelLayout(context, probe.(bo.BlockLevelBoxITF), -pr.Inf, nil, parentBox, pageIsEmpty, nil, nil, nil, false, -1)
	context.flexMeasuring--
	v := child.FlexBaseSize
	if measured != nil {
		v = measured.Box().Height.V() + collapseMargin(layout.adjoiningMargins)
	}
	if height := child.Style.GetHeight(); height.Tag == 0 && height.Unit == pr.Px && height.Calc == nil {
		specified := height.Value
		if child.Style.GetBoxSizing() == "border-box" {
			specified = max(specified-child.PaddingTop.V()-child.PaddingBottom.V()-child.BorderTopWidth.V()-child.BorderBottomWidth.V(), 0)
		}
		v = max(v, specified)
	}
	if child.Style.GetFlexGrow() == 0 {
		v = min(v, child.FlexBaseSize)
	}
	if child.Style.GetFlexShrink() == 0 {
		v = max(v, child.FlexBaseSize)
	}
	// minimum automatyczne pomijamy, jak w wierszu (zob. clampByBasis)
	if child.Style.GetMinHeight().Tag != pr.Auto {
		v = max(child.MinHeight.V(), v)
	}
	return min(v, child.MaxHeight.V())
}
