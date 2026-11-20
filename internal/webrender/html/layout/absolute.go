package layout

import (
	"fmt"
	"math"
	"slices"

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

// gopdfgo: pudełko z transform innym niż none jest blokiem zawierającym dla wszystkich potomków
// pozycjonowanych, także position: fixed (CSS Transforms 1 § 3); tak samo pudełko z zawieraniem
// layout lub paint (CSS Containment 2 § 3.2, § 3.3). Źródło uznawało za blok zawierający potomków
// absolute tylko pudełka pozycjonowane, a potomków fixed zawsze układało względem strony.
// Pudełko pozycjonowane bezwzględnie załatwia to samo w absoluteBlock, więc tu jest pomijane;
// pudełek liniowych niezastępowanych transform nie dotyczy.
func ownsFixedDescendants(box_ Box) bool {
	box := box_.Box()
	if box.IsAbsolutelyPositioned() || bo.InlineT.IsInstance(box_) {
		return false
	}
	return establishesFixedContainingBlock(box_)
}

func establishesFixedContainingBlock(box_ Box) bool {
	box := box_.Box()
	if box.IsForRootElement {
		return false
	}
	// gopdfgo: także translate, rotate i scale (CSS Transforms 2 § 5.1), perspective (§ 7) i
	// transform-style: preserve-3d (§ 5); źródło patrzyło tylko na transform
	if pr.EstablishesTransformContext(box.Style) {
		return true
	}
	return bo.HasContainment(box_, "layout") || bo.HasContainment(box_, "paint") ||
		bo.HasContainment(box_, "strict") || bo.HasContainment(box_, "content")
}

// ownsAbsoluteDescendants mówi, czy pudełko w przepływie jest blokiem zawierającym dla potomków
// position: absolute (pozycjonowane względnie albo jak w ownsFixedDescendants).
func ownsAbsoluteDescendants(box_ Box) bool {
	return box_.Box().Style.GetPosition().String == "relative" || ownsFixedDescendants(box_)
}

// layoutOwnedPositioned układa potomków pozycjonowanych, dla których containingBlock jest blokiem
// zawierającym; przy ownsFixed fixedBoxes jest własną listą tego pudełka. Lista fixed może rosnąć
// w trakcie (fixed wewnątrz absolute), stąd pętla po indeksie.
func layoutOwnedPositioned(context *layoutContext, containingBlock Box, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder,
	ownsFixed bool, bottomSpace pr.Float,
) {
	for _, absoluteBox := range *absoluteBoxes {
		absoluteLayout(context, absoluteBox, containingBlock, fixedBoxes, bottomSpace, nil)
	}
	if ownsFixed {
		for i := 0; i < len(*fixedBoxes); i++ {
			absoluteLayout(context, (*fixedBoxes)[i], containingBlock, fixedBoxes, bottomSpace, nil)
		}
	}
}

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
	if box.IsOutsideMarker {
		// gopdfgo: znacznik zewnętrzny stoi na początku elementu listy według kierunku elementu,
		// nie samego ::marker (CSS Lists 3 § 3.4); pudełko anonimowe dziedziczy po ::marker, a ten
		// po elemencie listy. Źródło brało kierunek ::marker (test WPT marker-direction-does-not-move-box).
		if marker := box.Style.ParentStyle(); marker != nil && marker.ParentStyle() != nil {
			ltr = marker.ParentStyle().GetDirection() == pr.Ltr
		}
	}
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
	// gopdfgo: tabela nie jest rozciągana między left i right: szerokość pudełka opakowującego to szerokość
	// tabeli (CSS 2.1 § 17.4, CSS Tables 3 § 3.9.3), a marginesy automatyczne ją wyśrodkowują jak przy
	// bloku o podanej szerokości (test WPT absolute-tables-016). W źródle opakowanie dostawało całą
	// szerokość między left i right, a tabela stała przy lewej krawędzi.
	if box.IsTableWrapper && width == pr.AutoF && left != pr.AutoF && right != pr.AutoF {
		available := cbWidth - left.V() - right.V()
		for _, margin := range [2]pr.MaybeFloat{marginL, marginR} {
			if margin != pr.AutoF {
				available -= margin.V()
			}
		}
		// gopdfgo: dostępna szerokość tabeli nie przekracza szerokości bloku zawierającego (jak Chromium,
		// testy WPT absolute-tables-008/009/011.tentative)
		tableWrapperWidth(context, box_, bo.MaybePoint{max(min(available, cbWidth), 0), cb_.Height})
		width = box.Width
	}
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
		// gopdfgo: dla tabeli dostępna szerokość nie przekracza bloku zawierającego (zob. wyżej)
		fit := func(available pr.Float) pr.Float {
			if box.IsTableWrapper {
				available = min(available, cbWidth-spacing)
			}
			return available
		}
		if left == pr.AutoF && width == pr.AutoF {
			box.Width = shrinkToFit(context, box_, fit(cbWidth-spacing-right.V()))
			translateX = cbWidth - right.V() - spacing + defaultTranslateX
			translateBoxWidth = true
		} else if left == pr.AutoF && right == pr.AutoF {
			if !ltr {
				availableWidth := cbWidth - (paddingsBorders + box.MarginLeft.V() + box.MarginRight.V())
				translateBoxWidth = true
				translateX = defaultTranslateX + availableWidth
			}
		} else if width == pr.AutoF && right == pr.AutoF {
			box.Width = shrinkToFit(context, box_, fit(cbWidth-spacing-left.V()))
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
	// gopdfgo: tak samo w pionie: tabela o podanej wysokości (bez podpisu) nie jest rozciągana między top
	// i bottom, więc marginesy automatyczne ją wyśrodkowują (test WPT absolute-tables-016)
	if box.IsTableWrapper && height == pr.AutoF && top != pr.AutoF && bottom != pr.AutoF && len(box.Children) == 1 {
		if tableHeight := box.Children[0].Box().Style.GetHeight(); tableHeight.Tag != pr.Auto && tableHeight.Unit == pr.Px {
			height = tableHeight.Value
			box.Height = height
		}
	}
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
	// gopdfgo: z transform (albo zawieraniem) także dla potomków fixed (CSS Transforms 1 § 3)
	ownsFixed := establishesFixedContainingBlock(box_)
	if ownsFixed {
		fixedBoxes = new([]*AbsolutePlaceholder)
	}

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
		// gopdfgo: i nie przekracza szerokości bryły zawierającej (jak Chromium, testy WPT
		// absolute-tables-008/011.tentative: ujemne left albo right nie poszerzają tabeli). Przy obu
		// podanych szerokość ustaliło już absoluteWidth, a margines nadmiarowy odejmuje autoTableLayout.
		if box.Left == pr.AutoF || box.Right == pr.AutoF {
			available = min(available, cbWidth)
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
	// gopdfgo: zob. layoutContext.outOfFlowDepth; pudełko w pozycji statycznej w osi pionowej (top
	// i bottom auto) płynie z treścią i nazwy stron w nim łamią strony, jak w Chromium (testy WPT
	// css-page/page-name-003 i page-name-abspos-002)
	staticBlock := box.Style.GetTop().Tag == pr.Auto && box.Style.GetBottom().Tag == pr.Auto
	if !staticBlock {
		context.outOfFlowDepth++
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
	if !staticBlock {
		context.outOfFlowDepth--
	}

	// gopdfgo: dalszy ciąg pudełka podzielonego między strony jest blokiem zawierającym swoich potomków
	// pozycjonowanych tak, jakby nie był podzielony (CSS Break 3 § 2: treść układa się w jednym ciągłym
	// przepływie fragmentacji, jak w Chrome, test WPT css-page/fixedpos-008): przesuwamy go w górę
	// o wysokość fragmentów na poprzednich stronach. Źródło liczyło top potomka od góry bieżącego fragmentu.
	childCB := newBox
	if paged := len(context.pageAreas) > 0 && !context.behavior.Continuous && newBox != nil; paged {
		g := context.geometryPage
		f := newBox.Box().PositionY - context.area(g).y + context.pageOffset(g)
		if len(skipStack) == 0 {
			defer func() {
				context.fragmentStarts[box_] = newBox.Box().PositionY - context.area(g).y + context.pageOffset(g)
			}()
		} else if start, ok := context.fragmentStarts[box_]; ok && f-start > 0.01 {
			shifted := newBox.Copy()
			shifted.Box().PositionY -= f - start
			context.shiftedBlocks[shifted] = true
			childCB = shifted
		}
	}
	for _, childPlaceholder := range absoluteBoxes {
		absoluteLayout(context, childPlaceholder, childCB, fixedBoxes, bottomSpace, skipStack)
	}
	if ownsFixed {
		for i := 0; i < len(*fixedBoxes); i++ {
			absoluteLayout(context, (*fixedBoxes)[i], newBox, fixedBoxes, bottomSpace, nil)
		}
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
	if len(skipStack) == 0 && len(context.lateBoxes) > 0 {
		// gopdfgo: układ od początku zastępuje fragmenty z poprzedniego układu (zob. forgetLateFragments)
		context.forgetLateFragments(box)
	}
	// gopdfgo: pudełko fixed, którego blokiem zawierającym jest strona, powtarza się na każdej stronie
	// (CSS 2.1 § 9.6.1, CSS Position 3 § 2.1). Strona zapamiętuje je przed układem pudełek pozycjonowanych,
	// więc pudełka fixed zagnieżdżone w pudełkach absolute (dopisywane do listy w trakcie tego układu) nie
	// były powtarzane (testy WPT css-page/fixedpos-002, -004).
	if page, isPage := containingBlock.(*bo.PageBox); isPage && bottomSpace > -pr.Inf && box.Box().Style.GetPosition().String == "fixed" &&
		!slices.Contains(page.FixedBoxes, box) {
		page.FixedBoxes = append(page.FixedBoxes, box)
	}
	// gopdfgo: blok początkowy liczony od pierwszej strony (zob. icbFor); układamy względem niego zamiast
	// względem bieżącej strony
	layoutCB, icb := containingBlock, context.usesInitialContainingBlock(box, containingBlock, bottomSpace, skipStack)
	if icb {
		layoutCB = context.icbFor(containingBlock.(*bo.PageBox), context.geometryPage)
	}
	newBox, resumeAt := absoluteBoxLayout(context, box, layoutCB, fixedBoxes, bottomSpace, skipStack)
	shiftedCB := !icb && context.shiftedBlocks[containingBlock] && len(skipStack) == 0 && bottomSpace > -pr.Inf &&
		box.Box().Style.GetPosition().String == "absolute" &&
		(box.Box().Style.GetTop().Tag != pr.Auto || box.Box().Style.GetBottom().Tag != pr.Auto)
	if (icb || shiftedCB) && newBox != nil {
		// pudełko zaczyna się na wcześniejszej stronie: tam układamy jego fragmenty, a na tej stronie
		// zostaje dalszy ciąg albo pusta, niewidoczna kopia
		cur := context.geometryPage
		if top := newBox.Box().PositionY; top < context.area(cur).y-0.01 {
			f := top - (context.area(cur).y - context.pageOffset(cur))
			if first := context.pageAt(f, cur); first < cur {
				removePlaceholders(context, []Box{newBox}, new([]*AbsolutePlaceholder), fixedBoxes)
				cbFor := func(j int) Box { return context.shiftedBlockFor(containingBlock, cur, j) }
				if icb {
					page := containingBlock.(*bo.PageBox)
					cbFor = func(j int) Box { return context.icbFor(page, j) }
				}
				fragment, fragmentResumeAt, reached := context.layoutEarlierFragments(box, cbFor, first, cur, fixedBoxes)
				if !reached {
					placeholder.setLaidOutBox(hiddenEmpty(newBox))
					return
				}
				newBox, resumeAt = fragment, fragmentResumeAt
			}
		}
	}
	if static := box.Box().StaticAlign; static != nil && newBox != nil {
		// gopdfgo: wyrównanie pozycji statycznej dziecka siatki w osi bez podanych przesunięć
		style, laidOut := newBox.Box().Style, newBox.Box()
		// ułamek ≥ 10 oznacza wyrównanie „safe”: gdy wyrównane pudełko wychodzi poza pudełko zawierające,
		// wraca do początku (CSS Align 3 § 4.4). Przepełnienie sprawdzamy wobec pudełka zawierającego,
		// nie prostokąta pozycji statycznej, jak Chromium (zagadnienie CSSWG 11934, testy WPT
		// flex-abspos-align-self-safe-outer-cb-*)
		cb := containingBlock.Box()
		resolve := func(start, free, size, fraction, cbStart, cbSize, clampStart pr.Float) pr.Float {
			if fraction >= 10 {
				fraction -= 10
				position := start + free*fraction
				if position < cbStart-0.01 || position+size > cbStart+cbSize+0.01 {
					// gopdfgo: pudełko wychodzące przed początek pudełka zawierającego jest dosuwane do
					// krawędzi treści tego pudełka (CSS Position 3 § 4.4; krawędź jak w Chromium, testy WPT
					// grid-abspos-staticpos-align-self-safe-*); wcześniej wracało do początku prostokąta
					// pozycji statycznej
					return max(min(position, start), clampStart)
				}
			}
			return start + free*fraction
		}
		var dx, dy pr.Float
		if style.GetLeft().Tag == pr.Auto && style.GetRight().Tag == pr.Auto {
			dx = resolve(static[0], static[2]-laidOut.MarginWidth(), laidOut.MarginWidth(), static[4],
				cb.PaddingBoxX(), cb.PaddingWidth(), cb.ContentBoxX()) - laidOut.PositionX
		}
		if style.GetTop().Tag == pr.Auto && style.GetBottom().Tag == pr.Auto {
			dy = resolve(static[1], static[3]-laidOut.MarginHeight(), laidOut.MarginHeight(), static[5],
				cb.PaddingBoxY(), cb.PaddingHeight(), cb.ContentBoxY()) - laidOut.PositionY
		}
		if dx != 0 || dy != 0 {
			newBox.Translate(dx, dy, false)
		}
	}
	// gopdfgo: pudełko pozycjonowane w kolumnie, którego górna krawędź leży na dole kolumny albo niżej, nie
	// ma w niej miejsca na żadną treść: pierwszy fragment jest pusty, a całe pudełko zaczyna się u góry
	// następnej kolumny (CSS Position 3 § 4.2, CSS Break 3 § 4.4; test WPT
	// fixedpos-static-pos-with-viewport-cb-001). W źródle wystawało z kolumny w całości.
	if context.inColumn && newBox != nil && resumeAt == nil && len(skipStack) == 0 && bottomSpace > -pr.Inf && context.pageBottom != pr.Inf &&
		context.clipDepth == 0 && newBox.Box().PositionY >= context.pageBottom-bottomSpace-0.01 && newBox.Box().MarginHeight() > 0 &&
		box.Box().Style.GetTop().Tag == pr.Auto && box.Box().Style.GetBottom().Tag == pr.Auto {
		if _, isPage := containingBlock.(*bo.PageBox); !isPage && containingBlock.Box().Style.GetOverflow() == "visible" {
			removePlaceholders(context, []Box{newBox}, new([]*AbsolutePlaceholder), fixedBoxes)
			empty := bo.CopyWithChildren(newBox, nil)
			empty.Box().Height = pr.Float(0)
			empty.Box().Style = empty.Box().Style.Copy()
			empty.Box().Style.SetVisibility("hidden")
			placeholder.setLaidOutBox(empty)
			context.setBrokenOutOfFlow(placeholder, brokenBox{box: box, containingBlock: containingBlock, nextColumn: true})
			return
		}
	}
	// gopdfgo: to samo na stronie: pudełko w pozycji statycznej w pionie, którego górna krawędź leży na
	// dole strony albo niżej (zastępnik na samym dole strony), ma tu pusty pierwszy fragment, a całe
	// zaczyna się u góry następnej strony (CSS Break 3 § 4.4, test WPT css-page/page-margin-007). W źródle
	// pierwsze dziecko było wymuszane na tę stronę i wystawało na jej dolny margines. Tylko zastępnik
	// dokładnie na dolnej krawędzi: zastępnik poniżej niej zostaje po treści przeniesionej na następną
	// stronę, gdzie pudełko jest układane ponownie (test goweasyprint TestAbsoluteInInline).
	if !context.inColumn && !context.behavior.Continuous && newBox != nil && len(skipStack) == 0 && bottomSpace > -pr.Inf &&
		context.pageBottom != pr.Inf && context.clipDepth == 0 && len(context.pageAreas) > 0 &&
		math.Abs(float64(newBox.Box().PositionY-(context.pageBottom-bottomSpace))) < 0.01 &&
		newBox.Box().PositionY > context.area(context.geometryPage).y+0.01 && newBox.Box().MarginHeight() > 0 &&
		box.Box().Style.GetPosition().String == "absolute" &&
		box.Box().Style.GetTop().Tag == pr.Auto && box.Box().Style.GetBottom().Tag == pr.Auto {
		if _, isPage := containingBlock.(*bo.PageBox); isPage || containingBlock.Box().Style.GetOverflow() == "visible" {
			removePlaceholders(context, []Box{newBox}, new([]*AbsolutePlaceholder), fixedBoxes)
			placeholder.setLaidOutBox(hiddenEmpty(newBox))
			context.setBrokenOutOfFlow(placeholder, brokenBox{box: box, containingBlock: containingBlock})
			return
		}
	}
	placeholder.setLaidOutBox(newBox)
	// gopdfgo: dalszy ciąg pudełka przyciętego przez blok zawierający nie jest widoczny, więc nie tworzy stron
	if _, isPage := containingBlock.(*bo.PageBox); !isPage && containingBlock.Box().Style.GetOverflow() != "visible" {
		resumeAt = nil
	}
	// gopdfgo: pudełko pozycjonowane w całości poniżej obszaru strony trafia na tę stronę, na którą wypada
	// jego pozycja (treść dokumentu płynie przez strony w jednym układzie współrzędnych). W źródle zostawało
	// poniżej dolnej krawędzi strony i nie było widoczne. Dotyczy to także pudełka, które samo by się
	// dzieliło (np. z treścią wylewającą się poza podaną wysokość): wcześniej jego pierwszy fragment
	// zostawał na tej stronie i był rysowany na jej dolnym marginesie (test WPT
	// css-page/monolithic-overflow-029).
	if bottomSpace > -pr.Inf && context.pageBottom != pr.Inf && context.pageHeight > 0 && context.clipDepth == 0 &&
		newBox.Box().Style.GetPosition().String == "absolute" &&
		(newBox.Box().Style.GetTop().Tag != pr.Auto || newBox.Box().Style.GetBottom().Tag != pr.Auto) {
		// tylko pudełka z podanym top albo bottom: pozycja statyczna podąża za miejscem w treści
		if top := newBox.Box().PositionY; top >= context.pageBottom && top < maxSliceableHeight {
			// Na tej stronie zostaje pusta, niewidoczna kopia: pudełko leżące tuż pod obszarem strony było
			// inaczej rysowane na jej dolnym marginesie (test WPT css-page/monolithic-overflow-029).
			removePlaceholders(context, []Box{newBox}, new([]*AbsolutePlaceholder), fixedBoxes)
			empty := bo.CopyWithChildren(newBox, nil)
			empty.Box().Height = pr.Float(0)
			empty.Box().Style = empty.Box().Style.Copy()
			empty.Box().Style.SetVisibility("hidden")
			empty.Box().DeferredToLaterPage = true
			placeholder.setLaidOutBox(empty)
			context.setBrokenOutOfFlow(placeholder, brokenBox{box: box, containingBlock: containingBlock, deferredPages: 1, icb: icb})
			return
		}
	}
	if resumeAt != nil {
		context.setBrokenOutOfFlow(placeholder, brokenBox{
			box:             box,
			containingBlock: containingBlock,
			resumeAt:        resumeAt,
			icb:             icb,
		})
		return
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
		startY := box.Box().PositionY // dalszy ciąg: góra strony (zob. makePage)
		newBox = absoluteReplaced(box, containingBlock)
		newBox, resumeAt = sliceAbsoluteReplaced(context, newBox, bottomSpace, skipStack, startY)
	} else if sliced, sliceResume, ok := sliceAbsoluteClipped(context, box, containingBlock, fixedBoxes, bottomSpace, skipStack); ok {
		newBox, resumeAt = sliced, sliceResume
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

// inlineAbsolute (gopdfgo): pudełko pozycjonowane bezwzględnie czekające na układ względem
// pudełka liniowego, które jest jego blokiem zawierającym (zob. getNextLinebox).
type inlineAbsolute struct {
	placeholder     *AbsolutePlaceholder
	containingBlock Box
	fixedBoxes      *[]*AbsolutePlaceholder
	bottomSpace     pr.Float
}

// isBareEmptyInline (gopdfgo) podaje, czy pudełko jest pustą resztą pudełka liniowego po
// wydzieleniu z niego bloku (SplitBefore), bez marginesów, dopełnienia i obramowań po bokach, czyli
// bez żadnego śladu w wierszu. Puste pudełka z dokumentu (np. kotwice <a id>) zostają w wierszu.
func isBareEmptyInline(box Box) bool {
	if !bo.InlineT.IsInstance(box) || len(box.Box().Children) != 0 || !box.Box().SplitBefore {
		return false
	}
	style := box.Box().Style
	for _, v := range []pr.TaggedDim{style.GetMarginLeft(), style.GetMarginRight(), style.GetPaddingLeft(), style.GetPaddingRight()} {
		if v.Value != 0 {
			return false
		}
	}
	return style.GetBorderLeftWidth().Value == 0 && style.GetBorderRightWidth().Value == 0
}

// contentBeforeInLine (gopdfgo) podaje, czy w wierszu przed zastępnikiem pudełka
// pozycjonowanego stoi treść, która nadałaby wierszowi wysokość: tekst inny niż
// spacje, element atomowy albo pudełko liniowe z niezerowym marginesem, dopełnieniem
// lub obramowaniem po stronie początku (CSS 2.1 § 9.4.2). Blok pozycjonowany
// bezwzględnie wewnątrz pudełka liniowego rozdzieliłby je (§ 9.2.1.1); gdy przed nim
// nie ma takiej treści, wiersz przed nim miałby zerową wysokość, a jego położenie
// statyczne to góra wiersza, nie jego dół (§ 10.6.4).
func contentBeforeInLine(line Box, placeholder *AbsolutePlaceholder) bool {
	found, content := false, false
	var walk func(b Box)
	walk = func(b Box) {
		for _, child := range b.Box().Children {
			if found || content {
				return
			}
			if child == Box(placeholder) || child.Box() == placeholder.Box() {
				found = true
				return
			}
			switch {
			case child.Box().IsAbsolutelyPositioned() || child.Box().IsFloated():
			case bo.TextT.IsInstance(child):
				for _, r := range child.(*bo.TextBox).Text {
					if r != ' ' {
						content = true
						return
					}
				}
			case bo.InlineT.IsInstance(child):
				fields := child.Box()
				if fields.MarginLeft.V() != 0 || fields.PaddingLeft.V() != 0 || fields.BorderLeftWidth.V() != 0 {
					content = true
					return
				}
				walk(child)
			case bo.ParentT.IsInstance(child) && !bo.AtomicInlineLevelT.IsInstance(child):
				walk(child)
			default:
				content = true
			}
		}
	}
	walk(line)
	return content
}

// gopdfgo: blok początkowy w druku. Pudełko pozycjonowane bezwzględnie bez pozycjonowanego przodka ma za
// blok zawierający blok początkowy (CSS 2.1 § 10.1 pkt 4), czyli obszar pierwszej strony; w układzie
// stronicowanym ciągnie się on w dół przez kolejne strony, a pudełko jest dzielone między nie (CSS Break 3
// § 2, jak Chrome: `top: 100vh` to zawsze druga strona dokumentu). Źródło liczyło przesunięcia od strony,
// na którą trafił zastępnik pudełka w przepływie, więc pudełko lądowało o tyle stron dalej.

// pageArea to położenie i rozmiar obszaru strony (pudełka treści strony) we współrzędnych tej strony.
type pageArea struct{ x, y, width, height pr.Float }

// lateBox to fragment pudełka pozycjonowanego dla wcześniejszej strony page (numer od 1); source to
// pudełko z drzewa, z którego fragment ułożono.
type lateBox struct {
	page   int
	box    Box
	source Box
}

// forgetLateFragments (gopdfgo) usuwa z lateBoxes fragmenty pudełka source ułożone wcześniej. Pudełko
// układane od nowa (zastępnik przeniesiony z niepełnej strony na następną, próbny układ wiersza tabeli)
// wytwarza swoje fragmenty jeszcze raz; bez tego fragmenty z poprzedniego układu zostawały pod numerem
// strony, której nikt już nie układał, i pudełko było rysowane dwa razy (CSS Break 3 § 2: każdy fragment
// pudełka jest jeden; wzorzec WPT css-break/table/repeated-section/fixedpos-in-footer-forced-break-print).
func (l *layoutContext) forgetLateFragments(source Box) {
	for origin, lates := range l.lateBoxes {
		kept := lates[:0:0]
		for _, late := range lates {
			if late.source != source {
				kept = append(kept, late)
			}
		}
		if len(kept) != len(lates) {
			l.lateBoxes[origin] = kept
		}
	}
}

// pageOffset podaje odległość początku obszaru strony n od początku obszaru pierwszej strony w ciągłej
// osi fragmentacji: suma wysokości obszarów stron poprzednich.
func (l *layoutContext) pageOffset(n int) pr.Float {
	var offset pr.Float
	for i := 0; i < n-1 && i < len(l.pageAreas); i++ {
		offset += l.pageAreas[i].height
	}
	return offset
}

// pageAt podaje numer strony (od 1, najwyżej last), na którą w osi fragmentacji wypada położenie f.
func (l *layoutContext) pageAt(f pr.Float, last int) int {
	var offset pr.Float
	for j := 1; j < last && j <= len(l.pageAreas); j++ {
		offset += l.pageAreas[j-1].height
		if f < offset {
			return j
		}
	}
	return last
}

// area podaje obszar strony n albo, gdy go nie znamy, obszar ostatniej znanej strony.
func (l *layoutContext) area(n int) pageArea {
	if n >= 1 && n <= len(l.pageAreas) {
		return l.pageAreas[n-1]
	}
	return l.pageAreas[len(l.pageAreas)-1]
}

// icbFor zwraca blok początkowy widziany ze strony j: rozmiar obszaru pierwszej strony, początek
// przesunięty w górę o wysokość stron poprzednich, poziomo przy lewej krawędzi obszaru strony j.
// page jest wzorcem (pudełko strony bieżącej); zwracamy jego płytką kopię.
func (l *layoutContext) icbFor(page *bo.PageBox, j int) *bo.PageBox {
	first, target := l.area(1), l.area(j)
	icb := page.Copy().(*bo.PageBox)
	icb.PositionX += target.x - page.ContentBoxX()
	icb.PositionY += target.y - l.pageOffset(j) - page.ContentBoxY()
	icb.Width, icb.Height = first.width, first.height
	return icb
}

// shiftedBlockFor zwraca płytką kopię bloku zawierającego ułożonego we współrzędnych strony from,
// przeniesioną do współrzędnych strony j (to samo miejsce w osi fragmentacji).
func (l *layoutContext) shiftedBlockFor(cb Box, from, j int) Box {
	if from == j || from < 1 {
		return cb
	}
	a, b := l.area(from), l.area(j)
	shifted := cb.Copy()
	shifted.Box().PositionX += b.x - a.x
	shifted.Box().PositionY += (b.y - l.pageOffset(j)) - (a.y - l.pageOffset(from))
	return shifted
}

// onPage wykonuje f z dołem strony i numerem strony geometrii ustawionymi na stronę j.
func (l *layoutContext) onPage(j int, f func()) {
	savedBottom, savedHeight, savedPage := l.pageBottom, l.pageHeight, l.geometryPage
	a := l.area(j)
	l.pageBottom, l.pageHeight, l.geometryPage = a.y+a.height, a.height, j
	f()
	l.pageBottom, l.pageHeight, l.geometryPage = savedBottom, savedHeight, savedPage
}

// usesInitialContainingBlock mówi, czy pudełko pozycjonowane w bloku zawierającym cb jest liczone od
// bloku początkowego w osi stron (zob. wyżej). Pudełko w pozycji statycznej w pionie (top i bottom auto)
// podąża za zastępnikiem; tryb ciągły nie ma stron.
func (l *layoutContext) usesInitialContainingBlock(box Box, cb Box, bottomSpace pr.Float, skipStack tree.ResumeStack) bool {
	_, isPage := cb.(*bo.PageBox)
	style := box.Box().Style
	return isPage && len(skipStack) == 0 && bottomSpace > -pr.Inf && !l.behavior.Continuous &&
		l.pageBottom != pr.Inf && l.pageHeight > 0 && l.clipDepth == 0 && !l.inColumn &&
		l.geometryPage >= 1 && l.geometryPage <= len(l.pageAreas) &&
		style.GetPosition().String == "absolute" && (style.GetTop().Tag != pr.Auto || style.GetBottom().Tag != pr.Auto)
}

// layoutEarlierFragments układa pudełko box od strony first (wcześniejszej niż bieżąca strona cur) aż
// do strony cur: fragmenty dla stron wcześniejszych trafiają do lateBoxes, fragment dla strony cur
// zwracamy (reached == true) razem z miejscem wznowienia. cbFor podaje blok zawierający we współrzędnych
// danej strony. Ograniczenie: pudełka zagnieżdżone, które dzielą się na wcześniejszej stronie, są
// kontynuowane dopiero na stronie po cur.
func (l *layoutContext) layoutEarlierFragments(box Box, cbFor func(j int) Box, first, cur int,
	fixedBoxes *[]*AbsolutePlaceholder,
) (fragment Box, resumeAt tree.ResumeStack, reached bool) {
	l.forgetLateFragments(box)
	for j := first; j <= cur; j++ {
		l.onPage(j, func() {
			if j == first {
				fragment, resumeAt = absoluteBoxLayout(l, box, cbFor(j), fixedBoxes, 0, nil)
				return
			}
			box.Box().PositionX += l.area(j).x - l.area(j-1).x
			box.Box().PositionY = l.area(j).y
			fragment, resumeAt = absoluteBoxLayout(l, box, cbFor(j), fixedBoxes, 0, resumeAt)
		})
		if j == cur {
			return fragment, resumeAt, true
		}
		l.lateBoxes[cur] = append(l.lateBoxes[cur], lateBox{page: j, box: fragment, source: box})
		if resumeAt == nil {
			return nil, nil, false
		}
	}
	return nil, nil, false
}

// hiddenEmpty zwraca pustą, niewidoczną kopię pudełka o zerowej wysokości: zastępnik pudełka, którego
// treść jest na innej stronie.
func hiddenEmpty(box Box) Box {
	empty := bo.CopyWithChildren(box, nil)
	empty.Box().Height = pr.Float(0)
	empty.Box().Style = empty.Box().Style.Copy()
	empty.Box().Style.SetVisibility("hidden")
	return empty
}

// takeFixedPlaceholders usuwa z drzewa box zastępniki pudełek fixed i je zwraca.
func takeFixedPlaceholders(box Box) []Box {
	var taken []Box
	fields := box.Box()
	kept := fields.Children[:0:0]
	for _, child := range fields.Children {
		if placeholder, ok := child.(*AbsolutePlaceholder); ok && placeholder.Box().Style.GetPosition().String == "fixed" {
			taken = append(taken, child)
			continue
		}
		taken = append(taken, takeFixedPlaceholders(child)...)
		kept = append(kept, child)
	}
	if len(kept) != len(fields.Children) {
		fields.Children = kept
	}
	return taken
}

// attachLateBoxes dokłada fragmenty z lateBoxes do stron po zakończeniu paginacji, w kolejności stron,
// które je wytworzyły.
func attachLateBoxes(context *layoutContext, pages []*bo.PageBox) {
	origins := make([]int, 0, len(context.lateBoxes))
	for origin := range context.lateBoxes {
		origins = append(origins, origin)
	}
	slices.Sort(origins)
	for _, origin := range origins {
		if origin > len(pages) {
			continue
		}
		originRoot := pages[origin-1].Box().Children[0].Box()
		for _, late := range context.lateBoxes[origin] {
			if late.page < 1 || late.page > len(pages) {
				continue
			}
			// pudełka fixed zagnieżdżone we fragmencie należą do strony, która je ułożyła (na pozostałe
			// powtarza je layoutFixedBoxes); we fragmencie byłyby narysowane drugi raz
			originRoot.Children = append(originRoot.Children, takeFixedPlaceholders(late.box)...)
			root := pages[late.page-1].Box().Children[0].Box()
			root.Children = append(root.Children, late.box)
		}
	}
}

// sliceAbsoluteReplaced (gopdfgo) kroi element zastępowany pozycjonowany bezwzględnie, wyższy niż strona,
// między strony, jak element zastępowany w przepływie (zob. sliceTallReplaced; treść monolityczna wyższa
// niż strona jest dzielona, CSS Break 3 § 4.4, jak w Chrome: wzorce WPT css-page/page-background-002/003).
// Źródło zostawiało go w całości na stronie, na której się zaczynał. Fragment jest kopią pudełka; dalszy
// ciąg (skipStack z resztą wysokości) zaczyna się w startY, u góry strony.
func sliceAbsoluteReplaced(context *layoutContext, box Box, bottomSpace pr.Float, skipStack tree.ResumeStack,
	startY pr.Float,
) (Box, tree.ResumeStack) {
	fields := box.Box()
	if bottomSpace == -pr.Inf || context.pageBottom == pr.Inf || context.inColumn || context.clipDepth > 0 ||
		context.behavior.Continuous || !pr.Is(fields.Height) || avoidPageBreak(string(fields.Style.GetBreakInside()), context) {
		return box, nil
	}
	fullHeight := fields.Height.V()
	if fullHeight >= maxSliceableHeight {
		return box, nil
	}
	var consumed pr.Float
	if remaining, isRemainder := skipStack.Remainder(); isRemainder {
		consumed = max(fullHeight-pr.Float(remaining), 0)
	} else if fields.BorderHeight() <= context.pageHeight {
		return box, nil
	}
	fragment := box.Copy()
	out := fragment.Box()
	if consumed > 0 {
		fragment.RemoveDecoration(out, true, false)
		out.PositionY = startY
	}
	available := context.pageBottom - bottomSpace - out.ContentBoxY()
	remaining := fullHeight - consumed
	if consumed == 0 && (available <= 0 || remaining <= available) {
		return box, nil
	}
	visible := max(min(remaining, available), 0)
	out.SliceFullHeight, out.SliceOffset, out.Height = fullHeight, consumed, visible
	if left := remaining - visible; left > 0.5 {
		fragment.RemoveDecoration(out, false, true)
		return fragment, tree.RemainderResume(float64(left))
	}
	return fragment, nil
}

// sliceAbsoluteClipped (gopdfgo) kroi między strony pudełko blokowe pozycjonowane bezwzględnie, które przycina
// swoją treść (overflow inne niż visible przy podanej wysokości) i jest wyższe niż strona. Takie pudełko jest
// układane w całości, bez podziału treści (wewnątrz pudełka przycinającego nic nie jest dzielone, zob.
// layoutContext.clipDepth), a każda strona pokazuje kolejny pas całości, jak treść niepodzielna wyższa niż
// strona (CSS Break 3 § 4.4; tak drukuje Chromium: testy WPT css-page/monolithic-overflow-028, wzorzec
// css-page/page-background-003). Wcześniej pudełko zostawało na pierwszej stronie, a to, co było niżej,
// ginęło. Dalszy ciąg (stos z [tree.SliceResume]: wysokość pokazana wcześniej) jest układany od nowa
// i przesuwany tak, żeby pokazana już część leżała nad górą strony (startY).
func sliceAbsoluteClipped(context *layoutContext, box Box, containingBlock block, fixedBoxes *[]*AbsolutePlaceholder,
	bottomSpace pr.Float, skipStack tree.ResumeStack,
) (Box, tree.ResumeStack, bool) {
	fields := box.Box()
	if !bo.BlockT.IsInstance(box) || fields.IsTableWrapper || !clipsBlockOverflow(fields) || bottomSpace == -pr.Inf ||
		context.pageBottom == pr.Inf || context.inColumn || context.clipDepth > 0 || context.behavior.Continuous ||
		context.flexMeasuring > 0 || len(context.pageAreas) == 0 ||
		fields.Style.GetColumnWidth().Tag != pr.Auto || fields.Style.GetColumnCount().String != "auto" {
		return nil, nil, false
	}
	consumed, _, resumed := skipStack.Slice()
	if !resumed && len(skipStack) != 0 {
		return nil, nil, false
	}
	startY := fields.PositionY // dalszy ciąg: góra strony (zob. makePage)
	whole, _ := absoluteBlock(context, box, containingBlock, fixedBoxes, -pr.Inf, nil)
	if whole == nil {
		return nil, nil, false
	}
	out := whole.Box()
	full := out.MarginHeight()
	if !resumed && (out.BorderHeight() <= context.pageHeight || full >= maxSliceableHeight) {
		return whole, nil, true
	}
	if resumed {
		whole.Translate(0, startY-pr.Float(consumed)-out.PositionY, false)
	}
	visibleTop := out.PositionY + pr.Float(consumed)
	available := context.pageBottom - bottomSpace - visibleTop
	remaining := full - pr.Float(consumed)
	if !resumed && (available <= 0 || remaining <= available) {
		return whole, nil, true
	}
	visible := max(min(remaining, available), 0)
	out.SliceOffset, out.SliceVisible = pr.Float(consumed), visible
	if left := remaining - visible; left > 0.5 && visible > 0 {
		return whole, tree.SliceResume(consumed+float64(visible), nil), true
	}
	return whole, nil, true
}
