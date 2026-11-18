package layout

import (
	"fmt"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"math"
	"reflect"
	"slices"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
)

// Page breaking and layout for block-level and block-container boxes.

type blockLayout struct {
	resumeAt          tree.ResumeStack
	adjoiningMargins  []pr.Float
	nextPage          tree.PageBreak
	collapsingThrough bool
}

// Lay out the block-level [box].
//
// [maxPositionY] is the absolute vertical position (as in
// 'someBox.PositionY') of the bottom of the
// content box of the current page area.
//
// [absoluteBoxes], [fixedBoxes] and [adjoiningMargins] may be nil
// [discard] default to false, [maxLines] default to -1
func blockLevelLayout(context *layoutContext, box bo.BlockLevelBoxITF, bottomSpace pr.Float, skipStack tree.ResumeStack,
	containingBlock *bo.BoxFields, pageIsEmpty bool,
	absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder, adjoiningMargins *[]pr.Float,
	discard bool, maxLines int,
) (bo.BlockLevelBoxITF, blockLayout, int) {
	if traceMode {
		traceLogger.DumpTree(box, fmt.Sprintf("entering blockLevelLayout skipStack %s", skipStack))
	}

	if absoluteBoxes == nil {
		absoluteBoxes = new([]*AbsolutePlaceholder)
	}
	if fixedBoxes == nil {
		fixedBoxes = new([]*AbsolutePlaceholder)
	}
	if adjoiningMargins == nil {
		adjoiningMargins = new([]pr.Float)
	}

	box_ := box.Box()
	prepareFieldsetLegend(context, box)
	if !bo.TableT.IsInstance(box) {
		resolveSizesBox(context, box, containingBlock)

		if box_.MarginTop == pr.AutoF {
			box_.MarginTop = pr.Float(0)
		}
		if box_.MarginBottom == pr.AutoF {
			box_.MarginBottom = pr.Float(0)
		}
		if box_.MarginTopTruncated { // gopdfgo: zob. BoxFields.MarginTopTruncated
			box_.MarginTop = pr.Float(0)
		}

		// gopdfgo: także na początku kolejnej kolumny (CSS Fragmentation 3 § 5.2); w źródle tylko na
		// początku strony innej niż pierwsza.
		if atBreak, forcedBreak := context.breakAtFragmentTop(); atBreak && pageIsEmpty {
			// When an unforced break occurs before or after a block-level box,
			// any margins adjoining the break are truncated to zero.
			if collapseWithPage := containingBlock.IsForRootElement || len(*adjoiningMargins) != 0; collapseWithPage {
				if box_.Style.GetMarginBreak() == "discard" {
					box_.MarginTop = pr.Float(0)
				} else if box_.Style.GetMarginBreak() == "auto" {
					if !forcedBreak {
						box_.MarginTop = pr.Float(0)
					}
				}
			}
		}

		collapsedMargin := collapseMargin(append(*adjoiningMargins, box_.MarginTop.V()))
		bl := box.BlockLevel()
		bl.Clearance = getClearance(context, box_, collapsedMargin)
		// gopdfgo: clearance wymuszone przez przylegające elementy pływające (zob. inFlowLayout)
		forced := context.forcedClearance[box_] && bl.Clearance != pr.MaybeFloat(pr.Inf)
		delete(context.forcedClearance, box_)
		if forced {
			if c := forcedClearanceFor(context, box_, collapsedMargin); c != nil {
				bl.Clearance = c
			} else {
				forced = false
			}
		}
		if context.separatedFromFloats[box_] {
			// gopdfgo: margines oddzielony od przylegających elementów pływających (zob. inFlowLayout):
			// krawędź obramowania staje na końcu łańcucha marginesów bez własnego marginesu, a niżej
			// pudełko omija elementy pływające (avoidCollisions)
			delete(context.separatedFromFloats, box_)
			if bl.Clearance == nil {
				bl.Clearance = collapseMargin(*adjoiningMargins) - collapsedMargin
			}
		}
		if box_.IsFlexItem || box_.IsGridItem {
			// gopdfgo: clear nie działa na elementach flex i grid (CSS Flexbox 1 § 3, CSS Grid 1 § 5.1)
			bl.Clearance = nil
		}
		// gopdfgo: pozycję hipotetyczną liczy się po zwinięciu marginesów, także z marginesami pierwszych
		// dzieci, które zwijają się przez górną krawędź pudełka (CSS 2.1 § 9.5.2 i § 8.3.1). Gdy już ona
		// leży poniżej elementów pływających, clearance nie powstaje; gdy powstaje, górna krawędź
		// obramowania (wspólna z tymi dziećmi) staje równo z dołem elementu pływającego. Źródło brało
		// tylko margines samego pudełka, więc margines dziecka dochodził jeszcze pod clearance.
		own := box_.MarginTop.V()
		if inner := leadingChildMargins(context, box, box_.PositionY, append(append([]pr.Float(nil), *adjoiningMargins...), own)); len(inner) != 0 &&
			(bl.Clearance != nil || (box_.Style.GetClear() != "none" && !box_.IsFlexItem && !box_.IsGridItem)) {
			// gopdfgo: także gdy sam margines pudełka mija elementy pływające, a ujemny margines dziecka
			// cofa pozycję hipotetyczną nad nie (test WPT adjoining-float-nested-forced-clearance-003)
			withChildren := collapseMargin(append(append(append([]pr.Float(nil), *adjoiningMargins...), own), inner...))
			if forced {
				bl.Clearance = forcedClearanceFor(context, box_, withChildren)
			} else {
				bl.Clearance = getClearance(context, box_, withChildren)
			}
			if bl.Clearance != nil {
				topBorderEdge := box_.PositionY + withChildren + bl.Clearance.V()
				box_.PositionY = topBorderEdge - collapseMargin(append([]pr.Float{own}, inner...))
				adjoiningMargins = new([]pr.Float)
			}
		} else if bl.Clearance != nil {
			topBorderEdge := box_.PositionY + collapsedMargin + bl.Clearance.V()
			box_.PositionY = topBorderEdge - box_.MarginTop.V()
			adjoiningMargins = new([]pr.Float)
		}
	}
	box, layout, v := blockLevelLayoutSwitch(context, box, bottomSpace, skipStack, containingBlock,
		pageIsEmpty, absoluteBoxes, fixedBoxes, adjoiningMargins, discard, maxLines)
	if box != nil && skipStack == nil {
		markLegendGap(box)
	}

	if traceMode {
		traceLogger.DumpTree(box, "after blockLevelLayout")
	}

	return box, layout, v
}

// Call the layout function corresponding to the “box“ type.
func blockLevelLayoutSwitch(context *layoutContext, box_ bo.BlockLevelBoxITF, bottomSpace pr.Float, skipStack tree.ResumeStack,
	containingBlock *bo.BoxFields, pageIsEmpty bool, absoluteBoxes,
	fixedBoxes *[]*AbsolutePlaceholder, adjoiningMargins *[]pr.Float, discard bool,
	maxLines int,
) (bo.BlockLevelBoxITF, blockLayout, int) {
	if traceMode {
		traceLogger.DumpTree(box_, fmt.Sprintf("at blockLevelLayoutSwitch with skipStack %s", skipStack))
	}

	blockBox, isBlockBox := box_.(bo.BlockBoxITF)
	replacedBox, isReplacedBox := box_.(bo.ReplacedBoxITF)
	if table, ok := box_.(bo.TableBoxITF); ok {
		out1, out2 := tableLayout(context, table, bottomSpace, skipStack, pageIsEmpty, absoluteBoxes, fixedBoxes)
		return out1, out2, -1
	} else if isBlockBox {
		out1, out2 := blockBoxLayout(context, blockBox, bottomSpace, skipStack, containingBlock,
			pageIsEmpty, absoluteBoxes, fixedBoxes, adjoiningMargins, discard, maxLines)
		return out1, out2, -1
	} else if isReplacedBox && bo.BlockReplacedT.IsInstance(box_) {
		b, v := blockReplacedBoxLayout(context, replacedBox, containingBlock)
		v = sliceTallReplaced(context, b, v, bottomSpace, skipStack)
		return b.(bo.BlockLevelBoxITF), v, -1 // blockReplacedBoxLayout is type stable
	} else if bo.FlexT.IsInstance(box_) {
		original := box_
		box_, layout := flexLayout(context, box_, bottomSpace, skipStack, containingBlock,
			pageIsEmpty, absoluteBoxes, fixedBoxes, discard)
		if box_ == nil { // kontener w całości przechodzi na następną stronę
			return nil, layout, -1
		}
		if narrowed, ok := narrowedBesideFloats(context, original, box_, containingBlock); ok {
			box_, layout = flexLayout(context, narrowed, bottomSpace, skipStack, containingBlock,
				pageIsEmpty, absoluteBoxes, fixedBoxes, discard)
			if box_ == nil {
				return nil, layout, -1
			}
		}
		avoidFloats(context, box_, containingBlock)
		return box_.(bo.BlockLevelBoxITF), layout, -1 // flexLayout is type stable
	} else if bo.GridT.IsInstance(box_) {
		box_, layout := gridLayout(context, box_, bottomSpace, skipStack, containingBlock,
			pageIsEmpty, absoluteBoxes, fixedBoxes)
		if box_ == nil { // gopdfgo: siatka przeniesiona w całości na następną stronę
			return nil, layout, -1
		}
		avoidFloats(context, box_, containingBlock)
		return box_.(bo.BlockLevelBoxITF), layout, -1 // gridLayout is type stable
	} else {
		panic(fmt.Sprintf("Layout for %s not handled yet", box_))
	}
}

// Lay out the block “box“.
func blockBoxLayout(context *layoutContext, box_ bo.BlockBoxITF, bottomSpace pr.Float, skipStack tree.ResumeStack,
	containingBlock *bo.BoxFields, pageIsEmpty bool, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder, adjoiningMargins *[]pr.Float,
	discard bool, maxLines int,
) (bo.BlockLevelBoxITF, blockLayout) {
	box := box_.Box()
	if box.Style.GetColumnWidth().Tag != pr.Auto || box.Style.GetColumnCount().String != "auto" {
		newBox_, result := columnsLayout(context, box_, bottomSpace, skipStack, containingBlock,
			pageIsEmpty, absoluteBoxes, fixedBoxes, *adjoiningMargins)
		resumeAt := result.resumeAt
		// przy box-decoration-break: clone miejsce na dolne odstępy zarezerwował już układ kolumn
		if resumeAt == nil && box.Style.GetBoxDecorationBreak() != "clone" {
			newBox := newBox_.Box()
			columnsBottomSpace := newBox.MarginBottom.V() + newBox.PaddingBottom.V() + newBox.BorderBottomWidth.V()
			if columnsBottomSpace != 0 {
				removePlaceholders(context, []Box{newBox_}, absoluteBoxes, fixedBoxes)
				bottomSpace += columnsBottomSpace
				newBox_, result = columnsLayout(context, box_, bottomSpace, skipStack,
					containingBlock, pageIsEmpty, absoluteBoxes, fixedBoxes, *adjoiningMargins)
			}
		}
		return newBox_, result
	} else if box.IsTableWrapper {
		// gopdfgo: tabela będąca elementem flex dostaje szerokość wyznaczoną przez układ flex (rozmiar
		// docelowy w wierszu, rozciągnięcie w kolumnie); źródło liczyło ją od nowa z samej tabeli, więc
		// flex-basis, flex-grow i rozciąganie tabeli nie działały.
		forced, _ := box.Width.(pr.Float)
		if !box.IsFlexItem || box.Width == pr.AutoF {
			forced = -1
		}
		tableWrapperWidthForced(context, box_, bo.MaybePoint{containingBlock.Width, containingBlock.Height}, forced)
		// gopdfgo: wysokość nadana przez układ flex zastępuje podaną wysokość tabeli (CSS Tables 3
		// § 3.10, test WPT table-as-item-specified-height); tabela jest układana do treści i niżej
		// rośnie do wysokości z układu flex. Treść i tak nie pozwala jej zejść poniżej sumy wierszy.
		if _, forcedHeight := box.Height.(pr.Float); forcedHeight && box.IsFlexItem && skipStack == nil &&
			box.Style.GetFlexBasis().Tag == 0 { // tylko przy flex-basis podanym długością
			box.GetWrappedTable().Box().Height = pr.AutoF
		}
	}
	blockLevelWidth(box_, nil, containingBlock)
	narrowBesideFloats(context, box_, containingBlock, *adjoiningMargins)

	savedPositionY, savedMargins := box.PositionY, append([]pr.Float(nil), *adjoiningMargins...)
	newBox__, result, _ := blockContainerLayout(context, box_, bottomSpace, skipStack, pageIsEmpty,
		absoluteBoxes, fixedBoxes, adjoiningMargins, discard, maxLines)
	// gopdfgo: pudełko z własnym kontekstem i tabela o szerokości auto, które obok elementów pływających
	// zajęłyby pas węższy niż ich szerokość na całej wysokości, są układane ponownie w węższym pasie
	// albo niżej, w następnym miejscu (CSS 2.1 § 9.5); zob. relayoutBesideFloats.
	if newBox__ != nil && skipStack == nil && result.resumeAt == nil {
		relayout := func(width pr.Float) (Box, blockLayout) {
			removePlaceholders(context, []Box{newBox__}, absoluteBoxes, fixedBoxes)
			box.PositionY = savedPositionY
			*adjoiningMargins = append((*adjoiningMargins)[:0], savedMargins...)
			if box.IsTableWrapper {
				table := box.GetWrappedTable().Box()
				table.Width = pr.AutoF
				tableWrapperWidthForced(context, box_, bo.MaybePoint{width, containingBlock.Height}, -1)
			} else {
				box.Width = width - (box.PaddingLeft.V() + box.PaddingRight.V() + box.BorderLeftWidth.V() + box.BorderRightWidth.V())
			}
			laidOut, layout, _ := blockContainerLayout(context, box_, bottomSpace, skipStack, pageIsEmpty,
				absoluteBoxes, fixedBoxes, adjoiningMargins, discard, maxLines)
			newBox__ = laidOut
			return laidOut, layout
		}
		if relaid, layout, ok := relayoutBesideFloats(context, box_, newBox__, containingBlock, relayout); ok {
			newBox__, result = relaid, layout
		}
	}
	// gopdfgo: tabela będąca elementem flex o wysokości wyznaczonej przez układ flex (rozmiar docelowy
	// w kolumnie, rozciągnięcie w wierszu) rośnie do tej wysokości: brakującą różnicę dostaje sama tabela,
	// podpisy zostają przy swojej. W źródle rosło tylko pudełko opakowujące, a tabela zostawała niska.
	if forcedHeight, ok := box.Height.(pr.Float); ok && box.IsTableWrapper && box.IsFlexItem &&
		newBox__ != nil && result.resumeAt == nil && skipStack == nil {
		var used pr.Float
		var laidOutTable *bo.BoxFields
		for _, child := range newBox__.Box().Children {
			if child.Box().IsInNormalFlow() {
				used += child.Box().MarginHeight()
			}
			if bo.TableT.IsInstance(child) {
				laidOutTable = child.Box()
			}
		}
		if deficit := forcedHeight - used; deficit > 0.5 && laidOutTable != nil {
			removePlaceholders(context, []Box{newBox__}, absoluteBoxes, fixedBoxes)
			table := box.GetWrappedTable().Box()
			table.Height = laidOutTable.Height.V() + deficit
			newBox__, result, _ = blockContainerLayout(context, box_, bottomSpace, skipStack, pageIsEmpty,
				absoluteBoxes, fixedBoxes, new([]pr.Float), discard, maxLines)
		} else if deficit < -0.5 && laidOutTable != nil {
			// gopdfgo: tabela nie schodzi poniżej wysokości treści (CSS Tables 3 § 3.10), więc pudełko
			// opakowujące rośnie razem z nią, zamiast zostawać przy wysokości z układu flex; inaczej
			// potomkowie pozycjonowani względem niego dostawali za niski blok zawierający (test WPT
			// table-as-item-large-intrinsic-size).
			removePlaceholders(context, []Box{newBox__}, absoluteBoxes, fixedBoxes)
			box.Height = used
			newBox__, result, _ = blockContainerLayout(context, box_, bottomSpace, skipStack, pageIsEmpty,
				absoluteBoxes, fixedBoxes, new([]pr.Float), discard, maxLines)
		}
	}
	newBox, _ := newBox__.(bo.BlockBoxITF) // blockContainerLayout is type stable
	// TODO: columns and flex items shouldn't be block boxes, this condition
	// would then be useless when this is fixed.
	if newBox == nil || newBox.Box().IsColumn || newBox.Box().IsFlexItem {
		return newBox, result
	}
	if newBox.Box().IsTableWrapper || bo.EstablishesFormattingContext(newBox) {
		// Don't collide with floats
		// https://www.w3.org/TR/CSS21/visuren.html#floats
		nb := newBox.Box()
		autoLeft, autoRight := nb.Style.GetMarginLeft().Tag == pr.Auto, nb.Style.GetMarginRight().Tag == pr.Auto
		if (autoLeft || autoRight) && len(context.excludedShapes.list) != 0 && containingBlock.Style.GetDirection() == pr.Ltr {
			// gopdfgo: marginesy auto pudełka omijającego elementy pływające dzielą miejsce pasa obok nich,
			// nie całego bloku zawierającego (CSS 2.1 § 9.5 pozwala postawić pudełko obok elementów
			// pływających; tak robią przeglądarki, test WPT floats-wrap-top-below-bfc-001r). W źródle
			// margines auto liczony od całego bloku zabierał miejsce i spychał pudełko pod elementy pływające.
			oldContentX := nb.ContentBoxX()
			if autoLeft {
				nb.MarginLeft = pr.Float(0)
			}
			if autoRight {
				nb.MarginRight = pr.Float(0)
			}
			positionX, positionY, available := avoidCollisions(context, newBox, containingBlock, false)
			if free := available - nb.BorderWidth(); free > 0 {
				switch {
				case autoLeft && autoRight:
					nb.MarginLeft, nb.MarginRight = free/2, free/2
				case autoLeft:
					nb.MarginLeft = free
				default:
					nb.MarginRight = free
				}
			}
			newBox.Translate(positionX+nb.MarginLeft.V()-(oldContentX-nb.BorderLeftWidth.V()-nb.PaddingLeft.V()), positionY-nb.PositionY, false)
			nb.PositionX = positionX
		} else {
			positionX, positionY, _ := avoidCollisions(context, newBox, containingBlock, false)
			newBox.Translate(positionX-newBox.Box().PositionX, positionY-newBox.Box().PositionY, false)
		}
	}

	return newBox, result
}

// narrowedBesideFloats (gopdfgo): kontener flex o szerokości auto, obok którego stoją elementy pływające,
// dostaje szerokość pasa wolnego od nich (CSS 2.1 § 9.5: pudełko z własnym kontekstem formatowania może
// zostać zwężone, żeby zmieścić się obok elementu pływającego; tak robią przeglądarki). Zwraca kopię
// pudełka wyjściowego z nadaną szerokością do ponownego układu.
func narrowedBesideFloats(context *layoutContext, original, laidOut Box, containingBlock *bo.BoxFields) (Box, bool) {
	box := laidOut.Box()
	if box.IsFlexItem || box.IsGridItem || box.IsColumn || !box.IsInNormalFlow() || len(context.excludedShapes.list) == 0 ||
		box.Style.GetWidth().Tag != pr.Auto || box.Style.GetWidth().Intrinsic != pr.IntrinsicNone {
		return nil, false
	}
	// pas wolny od elementów pływających na wysokości pudełka: pytamy pudełkiem o zerowej szerokości,
	// bo szerokie zostałoby od razu przesunięte pod elementy pływające
	probe := laidOut.Copy()
	probe.Box().Width = pr.Float(0)
	_, _, available := avoidCollisions(context, probe, containingBlock, false)
	if available >= box.BorderWidth()-0.01 || available <= 0 {
		return nil, false
	}
	narrowed := original.Copy()
	nb := narrowed.Box()
	nb.Style = nb.Style.Copy()
	width := available - (box.BorderWidth() - box.Width.V())
	if width <= 0 {
		return nil, false
	}
	nb.Style.SetWidth(pr.FToPx(width))
	if nb.Style.GetBoxSizing() == "border-box" {
		nb.Style.SetWidth(pr.FToPx(available))
	}
	nb.Width = width
	nb.NarrowedBesideFloats = true
	return narrowed, true
}

// leadingChildMargins zwraca górne marginesy pierwszych dzieci blokowych, które zwijają się z górnym
// marginesem pudełka: pudełko nie ma górnego obramowania ani odstępu i nie zakłada kontekstu formatowania.
// base i outer (marginesy do marginesu pudełka włącznie) wyznaczają pozycję hipotetyczną, zob. leadingChildChain.
func leadingChildMargins(context *layoutContext, box_ Box, base pr.Float, outer []pr.Float) (margins []pr.Float) {
	margins, _ = leadingChildChain(context, box_, base, outer)
	return margins
}

// leadingChildChain (gopdfgo) działa jak leadingChildMargins, ale podaje też najgłębsze pudełko, którego
// margines trafił na listę (nil, gdy lista jest pusta). Łańcuch kończy się przed dzieckiem z clear,
// które na pozycji hipotetycznej (base i wszystkie marginesy łańcucha zwinięte) nie minęłoby elementów
// pływających, które ma oczyścić: dostanie clearance, więc jego margines nie zwija się z resztą (CSS 2.1
// § 9.5.2). Dziecko z clear, które je mija, zostaje w łańcuchu (test WPT
// no-clearance-due-to-large-margin-after-left-right).
func leadingChildChain(context *layoutContext, box_ Box, base pr.Float, outer []pr.Float) (margins []pr.Float, last Box) {
	margins, boxes := leadingChildBoxes(box_)
	if len(margins) == 0 {
		return nil, nil
	}
	hypothetical := base + collapseMargin(append(append([]pr.Float(nil), outer...), margins...))
	for k, b := range boxes {
		clear := b.Box().Style.GetClear()
		if clear == "none" {
			continue
		}
		for _, shape := range context.excludedShapes.list {
			if (clear == "both" || clear == shape.Style.GetFloat()) && shape.PositionY+shape.MarginHeight() > hypothetical {
				margins, boxes = margins[:k], boxes[:k]
				break
			}
		}
		if len(boxes) == k {
			break
		}
	}
	if len(boxes) != 0 {
		last = boxes[len(boxes)-1]
	}
	return margins, last
}

// leadingChildBoxes zbiera łańcuch pierwszych dzieci i ich górnych marginesów bez sprawdzania clear.
func leadingChildBoxes(box_ Box) (margins []pr.Float, boxes []Box) {
	for depth := 0; depth < 16; depth++ {
		box := box_.Box()
		if !bo.BlockT.IsInstance(box_) || bo.EstablishesFormattingContext(box_) || box.IsTableWrapper {
			return margins, boxes
		}
		style := box.Style
		if border := style.GetBorderTopWidth(); border.Value != 0 && style.GetBorderTopStyle() != "none" && style.GetBorderTopStyle() != "hidden" {
			return margins, boxes
		}
		if padding := style.GetPaddingTop(); padding.Value != 0 {
			return margins, boxes
		}
		var first Box
		for _, child := range box.Children {
			if child.Box().IsInNormalFlow() {
				first = child
				break
			}
		}
		if first == nil || !bo.BlockLevelT.IsInstance(first) {
			if first == nil && len(boxes) != 0 && style.GetHeight().Tag == pr.Auto && slices.ContainsFunc(margins, func(m pr.Float) bool { return m < 0 }) {
				// łańcuch kończy się pustym pudełkiem, przez które marginesy przechodzą dalej: przy
				// ujemnych marginesach zostaje dotychczasowe liczenie (test WPT
				// adjoining-float-nested-forced-clearance-002)
				return nil, nil
			}
			return margins, boxes
		}
		margin := first.Box().Style.GetMarginTop()
		if margin.Tag == pr.Auto || margin.Unit != pr.Px || margin.Calc != nil {
			return margins, boxes
		}
		margins = append(margins, margin.Value)
		boxes = append(boxes, first)
		box_ = first
	}
	return margins, boxes
}

// avoidFloats (gopdfgo): kontener flex albo grid w przepływie blokowym zakłada własny kontekst
// formatowania, więc nie nachodzi na elementy pływające, tylko staje obok nich albo pod nimi
// (CSS 2.1 § 9.5, CSS Flexbox 1 § 3, CSS Grid 1 § 5.1). Źródło robiło to tylko dla bloków i tabel.
// Dotyczy kontenera o podanej szerokości; kontener o szerokości auto zostaje na swoim miejscu.
func avoidFloats(context *layoutContext, box_ Box, containingBlock *bo.BoxFields) {
	box := box_.Box()
	if box.IsFlexItem || box.IsGridItem || box.IsColumn || !box.IsInNormalFlow() || len(context.excludedShapes.list) == 0 ||
		(box.Style.GetWidth().Tag == pr.Auto && !box.NarrowedBesideFloats) {
		return
	}
	positionX, positionY, _ := avoidCollisions(context, box_, containingBlock, false)
	if positionX != box.PositionX || positionY != box.PositionY {
		box_.Translate(positionX-box.PositionX, positionY-box.PositionY, false)
	}
}

var blockReplacedWidth = handleMinMaxWidth(blockReplacedWidth_)

// @handleMinMaxWidth
func blockReplacedWidth_(box Box, _ *layoutContext, containingBlock containingBlock) (bool, pr.Float) {
	// https://www.w3.org/TR/CSS21/visudet.html#block-replaced-width
	replacedBoxWidth_(box, nil, containingBlock)
	blockLevelWidth_(box, nil, containingBlock)
	return false, 0
}

var blockLevelWidth = handleMinMaxWidth(blockLevelWidth_)

// @handleMinMaxWidth
// Set the “box“ width.
// containingBlock must be bo.BoxFields
func blockLevelWidth_(box_ Box, _ *layoutContext, containingBlock_ containingBlock) (bool, pr.Float) {
	box := box_.Box()
	// "cb" stands for "containing block"
	var (
		cbWidth   pr.Float
		direction pr.Keyword
	)
	switch cb := containingBlock_.(type) {
	case *bo.BoxFields:
		direction = cb.Style.GetDirection()
		cbWidth = cb.Width.V()
	case block:
		cbWidth = cb.Width
		direction = pr.Ltr
	}
	// https://www.w3.org/TR/CSS21/visudet.html#blockwidth

	// These names are waaay too long
	marginL := box.MarginLeft
	marginR := box.MarginRight
	width := box.Width
	paddingL := box.PaddingLeft.V()
	paddingR := box.PaddingRight.V()
	borderL := box.BorderLeftWidth.V()
	borderR := box.BorderRightWidth.V()

	// Only margin-left, margin-right and width can be "auto".
	// We want:  width of containing block ==
	//               margin-left + border-left-width + padding-left + width
	//               + padding-right + border-right-width + margin-right

	paddingsPlusBorders := paddingL + paddingR + borderL + borderR
	if width != pr.AutoF {
		total := paddingsPlusBorders + width.V()
		if marginL != pr.AutoF {
			total += marginL.V()
		}
		if marginR != pr.AutoF {
			total += marginR.V()
		}
		if total > cbWidth {
			if marginL == pr.AutoF {
				marginL = pr.Float(0)
				box.MarginLeft = pr.Float(0)
			}
			if marginR == pr.AutoF {
				marginR = pr.Float(0)
				box.MarginRight = pr.Float(0)
			}
		}
	}
	if width != pr.AutoF && marginL != pr.AutoF && marginR != pr.AutoF {
		// The equation is over-constrained.
		if direction == pr.Rtl && !box.IsColumn {
			box.PositionX += cbWidth - paddingsPlusBorders - width.V() - marginR.V() - marginL.V()
		} // Do nothing in ltr.
	}
	if width == pr.AutoF {
		if marginL == pr.AutoF {
			marginL = pr.Float(0)
			box.MarginLeft = pr.Float(0)
		}
		if marginR == pr.AutoF {
			marginR = pr.Float(0)
			box.MarginRight = pr.Float(0)
		}
		width = cbWidth - (paddingsPlusBorders + marginL.V() + marginR.V())
		box.Width = width
	}
	marginSum := cbWidth - paddingsPlusBorders - width.V()
	if marginL == pr.AutoF && marginR == pr.AutoF {
		box.MarginLeft = marginSum / 2.
		box.MarginRight = marginSum / 2.
	} else if marginL == pr.AutoF && marginR != pr.AutoF {
		box.MarginLeft = marginSum - marginR.V()
	} else if marginL != pr.AutoF && marginR == pr.AutoF {
		box.MarginRight = marginSum - marginL.V()
	}
	return false, 0
}

// Translate the “box“ if it is relatively positioned.
// resolveRelativeOffsets (gopdfgo) rozwiązuje top, right, bottom i left pudełka przesuniętego względnie;
// ujemna wysokość bloku zawierającego oznacza wysokość zależną od treści, przy której procentowe top
// i bottom liczą się jak auto.
func resolveRelativeOffsets(box *bo.BoxFields, containingBlock bo.Point) {
	resolvePositionPercentages(box, containingBlock)
	if containingBlock[1] < 0 {
		if top := box.Style.GetTop(); top.Tag != pr.Auto && top.Unit == pr.Perc {
			box.Top = pr.AutoF
		}
		if bottom := box.Style.GetBottom(); bottom.Tag != pr.Auto && bottom.Unit == pr.Perc {
			box.Bottom = pr.AutoF
		}
	}
}

func relativePositioning(box_ Box, containingBlock bo.Point) {
	box := box_.Box()
	if box.Style.GetPosition().String == "relative" {
		resolveRelativeOffsets(box, containingBlock)
		var translateX, translateY pr.Float
		if box.Left != pr.AutoF && box.Right != pr.AutoF {
			if box.Style.GetDirection() == pr.Ltr {
				translateX = box.Left.V()
			} else {
				translateX = -box.Right.V()
			}
		} else if box.Left != pr.AutoF {
			translateX = box.Left.V()
		} else if box.Right != pr.AutoF {
			translateX = -box.Right.V()
		} else {
			translateX = 0
		}

		if box.Top != pr.AutoF {
			translateY = box.Top.V()
		} else if box.Bottom != pr.AutoF {
			translateY = -box.Bottom.V()
		} else {
			translateY = 0
		}

		box_.Translate(translateX, translateY, false)
	}
	// gopdfgo: przesunięcie względne pudełek liniowych, z których wydzielono ten blok (CSS 2.1 § 9.2.1.1)
	for _, ancestor_ := range box.RelativeInlineAncestors {
		ancestor := ancestor_.Box()
		resolveRelativeOffsets(ancestor, containingBlock)
		var translateX, translateY pr.Float
		if ancestor.Left != pr.AutoF && (ancestor.Right == pr.AutoF || ancestor.Style.GetDirection() == pr.Ltr) {
			translateX = ancestor.Left.V()
		} else if ancestor.Right != pr.AutoF {
			translateX = -ancestor.Right.V()
		}
		if ancestor.Top != pr.AutoF {
			translateY = ancestor.Top.V()
		} else if ancestor.Bottom != pr.AutoF {
			translateY = -ancestor.Bottom.V()
		}
		box_.Translate(translateX, translateY, false)
	}
	if IsLine(box_) {
		for _, child := range box.Children {
			relativePositioning(child, containingBlock)
		}
	}
}

func reversedBoxes(in []Box) []Box {
	N := len(in)
	out := make([]Box, N)
	for i, v := range in {
		out[N-1-i] = v
	}
	return out
}

// Set the “box“ height.
func blockContainerLayout(context *layoutContext, box_ Box, bottomSpace pr.Float, skipStack tree.ResumeStack,
	pageIsEmpty bool, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder, adjoiningMargins *[]pr.Float, discard bool, maxLines int,
) (Box, blockLayout, int) {
	box := box_.Box()
	if !bo.BlockContainerT.IsInstance(box_) {
		panic(fmt.Sprintf("expected BlockContainer, got %T", box_))
	}

	if traceMode {
		traceLogger.DumpTree(box_, "at blockContainerLayout start")
	}

	// gopdfgo: w pudełku, które przycina treść (podana wysokość i overflow inne niż visible), nic nie jest
	// krojone ani dzielone między strony: to, co się nie mieści, jest po prostu niewidoczne.
	// W kolumnach to samo dotyczy każdego pudełka niepodzielnego (CSS Break 3 § 4.1).
	if clipsBlockOverflow(box) || (context.inColumn && !box.IsColumn && bo.BlockT.IsInstance(box_) && bo.IsMonolithic(box_)) {
		context.clipDepth++
		defer func() { context.clipDepth-- }()
	}
	// gopdfgo: w kolumnach kontener przewijania (overflow: hidden, scroll, auto; CSS Overflow 3 § 3) jest
	// niepodzielny (CSS Break 3 § 4.1), także przy wysokości auto: jego treść układamy bez ograniczenia,
	// a o przeniesieniu całości do następnej kolumny decyduje rodzic. W źródle treść dzieliła się między
	// kolumny (test WPT overflow-unsplittable-003). Na stronach zostaje zachowanie źródła: na papierze
	// nie da się przewinąć, więc treść dzielimy jak przeglądarki przy druku.
	if context.inColumn && !box.IsColumn && bo.BlockT.IsInstance(box_) && skipStack == nil && box.IsInNormalFlow() {
		if overflow := box.Style.GetOverflow(); overflow == "hidden" || overflow == "scroll" || overflow == "auto" {
			bottomSpace = -pr.Inf
		}
	}

	// See https://www.w3.org/TR/CSS21/visuren.html#block-formatting
	// gopdfgo: pudełko opakowujące tabelę zakłada kontekst formatowania blokowego (CSS 2.1 § 17.4),
	// więc elementy pływające spoza niego nie działają na podpis tabeli (clear, wiersze); w źródle podpis
	// z clear schodził pod element pływający stojący obok tabeli (test WPT clear-applies-to-015).
	wrapperContext := box.IsTableWrapper && !bo.EstablishesFormattingContext(box_)
	if bo.EstablishesFormattingContext(box_) || wrapperContext {
		context.createBlockFormattingContext()
	}

	isStart := skipStack == nil
	box_.RemoveDecoration(box, !isStart || box.SpannerSplitBefore, false)
	// gopdfgo: przodek o podanej wysokości podzielony elementem rozpinającym kolumny: fragmenty przed
	// elementem mają wysokość treści, ostatni dostaje to, co z podanej wysokości zostało (testy WPT
	// multicol-span-all-children-height-004a, -005).
	if box.SpannerSplitUsed != nil && box.Height != pr.AutoF {
		if box.SpannerSplitAfter {
			box.Height = pr.AutoF
		} else if box.SpannerSplitBefore && isStart {
			box.Height = max(0, box.Height.V()-*box.SpannerSplitUsed)
		}
	}

	discard = discard || box.Style.GetContinue() == "discard"
	drawBottomDecoration := discard || box.Style.GetBoxDecorationBreak() == "clone"

	if drawBottomDecoration {
		bottomSpace += box.PaddingBottom.V() + box.BorderBottomWidth.V() + box.MarginBottom.V()
	}

	*adjoiningMargins = append(*adjoiningMargins, box.MarginTop.V())
	thisBoxAdjoiningMargins := adjoiningMargins

	collapsingWithChildren := !(pr.Is(box.BorderTopWidth) || pr.Is(box.PaddingTop) || box.IsFlexItem ||
		box.IsGridItem || bo.EstablishesFormattingContext(box_) || box.IsForRootElement)
	var positionY pr.Float
	if collapsingWithChildren {
		// Not counting margins in adjoining_margins, if any
		// (there are not padding or borders, see above)
		positionY = box.PositionY
	} else {
		box.PositionY += collapseMargin(*adjoiningMargins) - box.MarginTop.V()
		adjoiningMargins = new([]pr.Float)
		positionY = box.ContentBoxY()
	}

	positionX := box.ContentBoxX()

	if box.Style.GetPosition().String == "relative" {
		// New containing block, use a new absolute list
		absoluteBoxes = new([]*AbsolutePlaceholder)
	}

	var (
		newChildren, brokenOutOfFlow []Box
		floatRemaining               pr.Float // reszta wysokości elementów pływających kontynuowanych dalej
		nextPage                     = tree.PageBreak{Break: "any"}
		resumeAt                     tree.ResumeStack
		lastInFlowChild              Box
	)
	// gopdfgo: elementy pływające podzielone w poprzedniej kolumnie ciągną się od góry tej kolumny
	// (CSS Break 3 § 4.4: fragmenty pudełka trafiają do kolejnych fragmentatorów), tak jak na stronach
	// robi to pages.go. W źródle ich dalszy ciąg trafiał dopiero na następną stronę.
	if box.IsColumn && len(context.columnFloats) != 0 {
		pending := context.columnFloats
		context.columnFloats = nil
		for _, v := range pending {
			v.box.Box().PositionY = box.ContentBoxY()
			placed, placedResumeAt := floatLayout(context, v.box, v.containingBlock.Box(), absoluteBoxes, fixedBoxes, bottomSpace, v.resumeAt)
			placed.Translate(v.x-placed.Box().PositionX, 0, false)
			newChildren = append(newChildren, placed)
			if placedResumeAt != nil {
				context.setBrokenOutOfFlow(placed, brokenBox{box: v.box, containingBlock: v.containingBlock, resumeAt: placedResumeAt})
				brokenOutOfFlow = append(brokenOutOfFlow, placed)
			}
		}
	}

	if ml := box.Style.GetMaxLines(); ml.Tag != pr.None {
		if maxLines <= 0 || ml.I <= maxLines {
			maxLines = ml.I
		}
	}

	// gopdfgo: podana wysokość jest od razu ograniczana przez min-height i max-height, bo to od wysokości
	// używanej liczą się procentowe wysokości dzieci (CSS 2 §10.5, §10.7); w źródle ograniczenie następowało
	// dopiero po ułożeniu dzieci.
	if isStart && box.Height != pr.AutoF && box.MinHeight != nil && box.MaxHeight != nil &&
		box.MinHeight != pr.AutoF && box.MaxHeight != pr.AutoF {
		box.Height = max(min(box.Height.V(), box.MaxHeight.V()), box.MinHeight.V())
	}

	// gopdfgo: aspect-ratio (CSS Sizing 4 § 5.1): pudełko o wysokości auto i znanej szerokości dostaje
	// wysokość z proporcji (dla pudełka wskazanego przez box-sizing), ograniczoną przez min-height
	// i max-height. Treść wyższa niż ta wysokość powiększa pudełko, o ile overflow to visible (minimum
	// automatyczne, § 5.1.1); zob. koniec funkcji.
	heightFromRatio := false
	if ratio := pr.Float(math.Abs(float64(box.Style.GetAspectRatio()))); isStart && ratio > 0 && box.Height == pr.AutoF && !box.IsTableWrapper && !bo.TableT.IsInstance(box_) {
		if width, ok := box.Width.(pr.Float); ok && width != pr.Inf {
			height := width / ratio
			if box.Style.GetBoxSizing() == "border-box" {
				borderWidth := width + box.PaddingLeft.V() + box.PaddingRight.V() + box.BorderLeftWidth.V() + box.BorderRightWidth.V()
				height = borderWidth/ratio - box.PaddingTop.V() - box.PaddingBottom.V() - box.BorderTopWidth.V() - box.BorderBottomWidth.V()
			}
			height = max(height, 0)
			if minHeight, ok := box.MinHeight.(pr.Float); ok {
				height = max(height, minHeight)
			}
			if maxHeight, ok := box.MaxHeight.(pr.Float); ok {
				height = min(height, maxHeight)
			}
			box.Height, heightFromRatio = height, true
		}
	}

	skip := 0
	firstLetterStyle := box.FirstLetterStyle
	if isStart && box.FirstLineStyle != nil {
		// gopdfgo: styl `::first-line` trafia na pudełko pierwszego sformatowanego wiersza; blok
		// zagnieżdżony układany jest później, więc jego własny styl ma pierwszeństwo
		if line, _ := firstLineTarget(box_); line != nil {
			line.FirstLineStyle = tree.Reparent(box.FirstLineStyle, line.Style)
		}
	}
	// gopdfgo: treść wylewająca się z bloku o podanej wysokości jest kontynuowana na następnej stronie,
	// choć sam blok skończył się wcześniej (CSS Fragmentation 3 § 5.1). W źródle taka treść była
	// porzucana ("forget overflowing children").
	resumedRemainder, overflowContinuation, finishedWithOverflow := false, false, false
	resumedFixed := false
	if !isStart {
		if inner, isOverflow := skipStack.Overflow(); isOverflow {
			// sam blok skończył się na poprzedniej stronie: zostaje po nim tylko wylewająca się treść
			overflowContinuation = true
			box.Height = pr.Float(0)
			box.MinHeight, box.MaxHeight = pr.Float(0), pr.Inf
			box_.RemoveDecoration(box, true, true)
			skip, skipStack = 0, nil
			if inner != nil {
				skip, skipStack = inner.Unpack()
			}
		} else if remaining, isRemainder := skipStack.Remainder(); isRemainder {
			// dalszy ciąg bloku o podanej wysokości: dzieci są już ułożone, zostaje reszta wysokości
			resumedRemainder = true
			skip, skipStack = len(box.Children), nil
			if remaining > 0 {
				box.Height = pr.Float(remaining)
			}
		} else {
			if remaining, ok := context.fixedHeightRemaining[resumeKey(skipStack)]; ok {
				// dalszy ciąg bloku o podanej wysokości, którego dzieci podzieliły się między strony:
				// zostaje mu reszta wysokości (zob. niżej)
				resumedFixed = true
				box.Height = remaining
				box.MinHeight, box.MaxHeight = pr.Float(0), pr.Inf
			}
			skip, skipStack = skipStack.Unpack()
		}
		firstLetterStyle = nil
	}
	var (
		i      int
		child_ Box
	)
	// gopdfgo: w Pythonie children[skip:] poza zakresem daje pustą listę; w Go to panika. Zdarza się,
	// gdy komórka tabeli jest układana ponownie bez dzieci, ale z pozycją wznowienia (tables.go).
	skip = min(skip, len(box.Children))
	for i, child_ = range box.Children[skip:] {
		index := i + skip
		child := child_.Box()
		child.PositionX = positionX
		child.PositionY = positionY // does not count margins in adjoiningMargins
		var newFootnotes []Box

		var abort, stop bool
		if !child.IsInNormalFlow() {
			abort = false
			var (
				outOfFlowResumeAt tree.ResumeStack
				newChild          Box
			)
			stop, resumeAt, newChild, outOfFlowResumeAt = outOfFlowLayout(context, box_, index, child_,
				&newChildren, pageIsEmpty, absoluteBoxes, fixedBoxes, *adjoiningMargins, bottomSpace)
			if outOfFlowResumeAt != nil {
				context.setBrokenOutOfFlow(newChild, brokenBox{box: child_, containingBlock: box_, resumeAt: outOfFlowResumeAt})
				brokenOutOfFlow = append(brokenOutOfFlow, newChild)
				if remaining, ok := outOfFlowResumeAt.Remainder(); ok && child.IsFloated() {
					floatRemaining = max(floatRemaining, pr.Float(remaining))
				}
			}
			if child.IsOutsideMarker {
				newChild.Box().PositionX = box.BorderBoxX()
				if child.Style.GetDirection() == pr.Rtl {
					newChild.Box().PositionX += box.Width.V() + box.PaddingRight.V()
				}
			}
		} else if childLineBox, ok := child_.(*bo.LineBox); ok { // LineBox is a final type
			// gopdfgo: wiersz bez treści (np. pusty <span> między blokami) ma zerową wysokość i dla
			// zwijania marginesów nie istnieje (CSS 2.1 § 9.4.2); źródło kończyło na nim łańcuch
			// sąsiadujących marginesów, więc marginesy bloków przed nim i po nim się sumowały.
			lineMargins := *adjoiningMargins
			emptyLine := skipStack == nil && isEmptyLinebox(childLineBox)
			if emptyLine {
				lineMargins = nil
			}
			abort, stop, resumeAt, positionY, newChildren, newFootnotes, maxLines = lineBoxLayout(context, box_, index, childLineBox,
				newChildren, pageIsEmpty, absoluteBoxes, fixedBoxes, lineMargins,
				bottomSpace, positionY, skipStack, firstLetterStyle, maxLines)
			drawBottomDecoration = drawBottomDecoration || resumeAt == nil
			if !emptyLine {
				adjoiningMargins = new([]pr.Float)
			}
		} else {
			var (
				adjoiningMarginsV []pr.Float
				newMaxLines       int
			)
			abort, stop, resumeAt, positionY, adjoiningMarginsV, nextPage, newChildren, newMaxLines = inFlowLayout(context, box_, index, child_,
				newChildren, pageIsEmpty, absoluteBoxes, fixedBoxes, adjoiningMargins,
				bottomSpace, positionY, skipStack, firstLetterStyle, collapsingWithChildren, discard, maxLines)
			skipStack = nil
			adjoiningMargins = &adjoiningMarginsV
			if collapsingWithChildren && !stop && !abort && marginChainOpen(newChildren) {
				// gopdfgo: dziecko było puste i marginesy przeszły przez nie, więc górny margines tego
				// pudełka (i jego przodków, które dzielą z nim listę marginesów) zwija się także
				// z marginesami następnego dziecka (CSS 2.1 § 8.3.1). W źródle lista była zastępowana
				// nową i pudełko ustawiało się bez marginesów dzieci stojących za pustym.
				*thisBoxAdjoiningMargins = adjoiningMarginsV
				adjoiningMargins = thisBoxAdjoiningMargins
				// gopdfgo: dolny margines pustego dziecka też należy do górnego marginesu tego pudełka, więc
				// dotychczasowe dzieci (i elementy pływające w nich) przesuwają się razem z górną krawędzią
				// pudełka (CSS 2.1 § 8.3.1). W źródle przesunięcie uwzględniało tylko górne marginesy dzieci,
				// a gdy łańcuch kończył się na pudełku z min-height, element pływający z pustego dziecka
				// zostawał nad pudełkiem (test WPT margin-collapse-min-height-001).
				if n := len(adjoiningMarginsV); n > 0 {
					if delta := collapseMargin(adjoiningMarginsV) - collapseMargin(adjoiningMarginsV[:n-1]); delta != 0 {
						for _, previousNewChild := range newChildren {
							previousNewChild.Translate(0, delta, false)
						}
					}
				}
			}

			if newMaxLines != -1 && maxLines != -1 {
				maxLines = newMaxLines
				if maxLines <= 0 {
					stop = true
					lastChild := (child_ == box.Children[len(box.Children)-1])
					if !lastChild {
						children := newChildren
						for len(children) != 0 {
							lastChild := children[len(children)-1]
							if lastChildF, ok := lastChild.(*bo.LineBox); ok {
								lastChildF.BlockEllipsis = box.Style.GetBlockEllipsis()
							} else if bo.ParentT.IsInstance(lastChild) {
								children = lastChild.Box().Children
								continue
							}
							break
						}
					}
				}
			}

		}

		if traceMode {
			origin := "inFlow"
			if !child.IsInNormalFlow() {
				origin = "outOfFlow"
			} else if _, ok := child_.(*bo.LineBox); ok {
				origin = "lineBox"
			}
			bilan := "continue"
			if abort {
				bilan = "abort"
			} else if stop {
				bilan = "stop"
			}
			traceLogger.Dump(fmt.Sprintf("in blockContainerLayout child %d (%s) resumeAt %s; %s", index, origin, resumeAt, bilan))
		}

		if abort {
			page_, _ := child.PageValues()
			removePlaceholders(context, box.Children[skip:], absoluteBoxes, fixedBoxes)
			for _, footnote := range newFootnotes {
				context.unlayoutFootnote(footnote)
			}

			if wrapperContext { // gopdfgo: zamknięcie kontekstu pudełka opakowującego tabelę
				context.discardBlockFormattingContext()
			}
			return nil, blockLayout{nextPage: tree.PageBreak{Break: "any", Page: page_, HasPage: true}}, maxLines
		} else if stop {
			if box.Height != pr.AutoF {
				if overflows(box.PositionY+box.BorderHeight(), positionY) {
					// Box height is fixed and it doesn’t overflow page: the box
					// ends here, its overflowing children continue on the next page,
					// unless the overflow is clipped.
					if box.Style.GetOverflow() == "visible" && !box.IsColumn && !context.viewportClips {
						finishedWithOverflow = resumeAt != nil
					} else {
						resumeAt = nil
					}
				}
			}
			adjoiningMargins = new([]pr.Float)
			i = -1 // hasBroken
			break
		}
	}

	if i != -1 {
		resumeAt = nil
	}

	// gopdfgo: blok o podanej wysokości, którego dzieci się zmieściły, ale który sam wystaje poza stronę,
	// jest dzielony: na tej stronie zostaje część do dolnej krawędzi, reszta wysokości przechodzi na
	// następną (CSS Fragmentation 3 § 5.1). W źródle blok wystawał poza stronę i jego dalsza część ginęła.
	// Dzielimy tylko bloki wyższe niż obszar strony, czyli takie, które nie zmieszczą się na żadnej;
	// blok, który mieści się na stronie, jest przenoszony w całości na następną (ADR 0004).
	splitHeight := pr.Float(-1)
	// Gdy dzieci bloku były dzielone między strony, nie wiemy, ile wysokości zużyły strony poprzednie;
	// takiego bloku nie dzielimy (zachowanie źródła).
	// Dzielimy też blok, który jest pierwszą treścią na stronie: nie da się go przenieść, więc inaczej
	// wystawałby poza stronę (przed nim są tylko obramowania i odstępy przodków).
	// W kolumnach blok dzielimy zawsze: równoważenie kolumn tego wymaga, a wysokość kolumny (nie strony)
	// jest tam miarą tego, co się mieści.
	splittable := resumedRemainder || (isStart && (box.BorderHeight() > context.pageHeight || (pageIsEmpty && box.IsInNormalFlow()) || context.inColumn))
	// break-inside: avoid nie da się spełnić dla bloku wyższego niż strona; taki blok przenosimy najpierw
	// na początek strony i dopiero tam dzielimy.
	if splittable && !resumedRemainder && !pageIsEmpty && avoidPageBreak(string(box.Style.GetBreakInside()), context) {
		splittable = false
	}
	// Bloku, który w kolumnach jest blokiem zawierającym elementy pozycjonowane, nie dzielimy:
	// tych elementów nie umiemy podzielić razem z nim.
	if splittable && context.inColumn && box.Style.GetPosition().String == "relative" && len(*absoluteBoxes) != 0 {
		splittable = false
	}
	if resumeAt == nil && box.Height != pr.AutoF && splittable && canSplitFixedHeight(context, box_, bottomSpace) {
		// Dzielimy wysokość używaną, czyli po uwzględnieniu min-height i max-height; dla dalszego ciągu
		// jest nią reszta z poprzedniej strony.
		usedHeight := box.Height.V()
		if !resumedRemainder {
			usedHeight = max(min(usedHeight, box.MaxHeight.V()), box.MinHeight.V())
		}
		available := context.pageBottom - bottomSpace - box.ContentBoxY()
		if collapsingWithChildren {
			// pudełko bez górnego obramowania i odstępu dostaje ostateczne położenie dopiero niżej,
			// po zwinięciu marginesów; miejsce liczymy od tego położenia
			available -= collapseMargin(*thisBoxAdjoiningMargins) - box.MarginTop.V()
		}
		// gopdfgo: w kolumnach fragment zużywa co najmniej tyle wysokości, ile zajęła treść, która musiała
		// się w nim zmieścić (np. niepodzielny wiersz wyższy niż kolumna; CSS Break 3 § 4.4); w źródle
		// reszta wysokości dawała puste fragmenty w następnych kolumnach (test WPT multicol-zero-height-002).
		if used := positionY - box.ContentBoxY(); context.inColumn && available > 0 && used > available {
			available = used
		}
		if remaining := usedHeight - available; available > 0 && remaining > 0.5 {
			splitHeight = available
			resumeAt = tree.RemainderResume(float64(remaining))
		}
	}

	if overflowContinuation && resumeAt != nil {
		finishedWithOverflow = true
	}
	// wznowienie pudełka kolumny i kontenera kolumn czyta układ kolumn, który zna tylko indeksy dzieci
	if box.IsColumn || box.Style.GetColumnWidth().Tag != pr.Auto || box.Style.GetColumnCount().String != "auto" {
		finishedWithOverflow = false
	}
	boxIsFragmented := (resumeAt != nil && !finishedWithOverflow) || box.ForceFragmentation
	if box.Style.GetContinue() == "discard" {
		resumeAt = nil
	}

	if bi := string(box.Style.GetBreakInside()); boxIsFragmented && avoidPageBreak(bi, context) && !pageIsEmpty {
		removePlaceholders(context, append(newChildren, box.Children[skip:]...), absoluteBoxes, fixedBoxes)
		for _, child := range brokenOutOfFlow {
			delete(context.brokenOutOfFlow, child)
		}

		if wrapperContext { // gopdfgo: zamknięcie kontekstu pudełka opakowującego tabelę
			context.discardBlockFormattingContext()
		}
		return nil, blockLayout{nextPage: tree.PageBreak{Break: "any"}}, maxLines
	}

	if collapsingWithChildren {
		box.PositionY += collapseMargin(*thisBoxAdjoiningMargins) - box.MarginTop.V()
	}

	lastInFlowChild = nil
	for _, previousChild := range reversedBoxes(newChildren) {
		if line, isLine := previousChild.(*bo.LineBox); isLine && line.Height == pr.Float(0) && isEmptyLinebox(line) {
			continue // gopdfgo: pusty wiersz nie przerywa zwijania marginesów przez pudełko
		}
		if previousChild.Box().IsInNormalFlow() {
			lastInFlowChild = previousChild
			break
		}
	}
	collapsingThrough := false
	if lastInFlowChild == nil {
		collapsedMargin := collapseMargin(*adjoiningMargins)
		// top && bottom margin of this box
		// gopdfgo: marginesy pudełka zakładającego własny kontekst formatowania nie przechodzą przez nie
		// (CSS 2.1 § 8.3.1); w źródle puste pudełko z overflow: hidden przepuszczało marginesy.
		if (box.Height == pr.AutoF || box.Height == pr.Float(0)) && !bo.EstablishesFormattingContext(box_) &&
			getClearance(context, box, collapsedMargin) == nil &&
			box.MinHeight == pr.Float(0) && box.BorderTopWidth == pr.Float(0) && box.PaddingTop == pr.Float(0) &&
			box.BorderBottomWidth == pr.Float(0) && box.PaddingBottom == pr.Float(0) {
			collapsingThrough = true
		} else {
			positionY += collapsedMargin
			adjoiningMargins = new([]pr.Float)
		}
	} else {
		// bottom margin of the last child && bottom margin of this box ...
		if box.Height != pr.AutoF {
			// not adjoining. (positionY is not used afterwards.)
			adjoiningMargins = new([]pr.Float)
		} else if box.MinHeight == pr.Float(0) && box.BorderTopWidth == pr.Float(0) && box.PaddingTop == pr.Float(0) &&
			box.BorderBottomWidth == pr.Float(0) && box.PaddingBottom == pr.Float(0) &&
			!bo.EstablishesFormattingContext(box_) && allCollapsedThrough(newChildren) &&
			getClearance(context, box, collapseMargin(*adjoiningMargins)) == nil {
			// gopdfgo: marginesy pudełka zwijają się przez nie także wtedy, gdy ma dzieci, o ile marginesy
			// wszystkich dzieci w przepływie też się zwijają (CSS 2 § 8.3.1); źródło uznawało to tylko
			// dla pudełek bez dzieci, więc pusty blok w pustym bloku rozdzielał marginesy.
			collapsingThrough = true
		}
	}

	// gopdfgo: margines zwinięty z marginesami pustego pudełka z clearance nie zwija się z dolnym
	// marginesem rodzica (CSS 2 §8.3.1); w źródle wychodził poza rodzica.
	// gopdfgo: element flex i element siatki zakładają własny kontekst formatowania (CSS Flexbox 1 § 3,
	// CSS Grid 1 § 6.1), więc dolny margines ostatniego dziecka zostaje w środku, tak jak górny; w źródle
	// górny był zachowany, a dolny wychodził poza element i ginął.
	if pr.Is(box.BorderBottomWidth) || pr.Is(box.PaddingBottom) || endsWithClearedEmptyBox(newChildren) ||
		bo.EstablishesFormattingContext(box_) || box.IsForRootElement || box.IsTableWrapper || box.IsFlexItem || box.IsGridItem {
		positionY += collapseMargin(*adjoiningMargins)
		adjoiningMargins = new([]pr.Float)
	}

	newBox_ := bo.CopyWithChildren(box_, newChildren)
	newBox := newBox_.Box()
	newBox.CollapsedThrough = collapsingThrough
	newBox_.RemoveDecoration(newBox, !isStart || box.SpannerSplitBefore, (boxIsFragmented && !discard) || (box.SpannerSplitAfter && resumeAt == nil))

	if newBox.Height == pr.AutoF {
		if len(context.excludedShapes.list) != 0 && newBox.Style.GetOverflow() != "visible" {
			maxFloatPositionY := -pr.Inf
			for _, floatBox := range context.excludedShapes.list {
				v := floatBox.PositionY + floatBox.MarginHeight()
				if v > maxFloatPositionY {
					maxFloatPositionY = v
				}
			}
			positionY = max(maxFloatPositionY, positionY)
		}
		if positionY == newBox.ContentBoxY() && positionY == pr.Inf {
			newBox.Height = pr.Float(0)
		} else if collapsingThrough {
			// gopdfgo: pudełko, przez które przechodzą marginesy, nie ma treści w przepływie, więc ma
			// wysokość zero (CSS 2.1 § 10.6.3); w źródle różnica między pozycją treści a pozycją pudełka
			// przesuniętego o ujemny zwinięty margines dawała mu dodatnią wysokość.
			newBox.Height = pr.Float(0)
		} else {
			newBox.Height = positionY - newBox.ContentBoxY()
		}
	} else if heightFromRatio && newBox.Style.GetOverflow() == "visible" && resumeAt == nil && positionY < pr.Inf {
		// treść wyższa niż wysokość z proporcji
		if content := positionY - newBox.ContentBoxY(); content > newBox.Height.V() {
			newBox.Height = min(content, newBox.MaxHeight.V())
		}
	}

	// gopdfgo: przy zawieraniu rozmiaru wysokość auto liczy się jak dla pudełka bez treści (CSS
	// Containment 1 § 3.1)
	if box.Height == pr.AutoF && !heightFromRatio && bo.HasContainment(box_, "size") {
		newBox.Height = pr.Float(0)
	}

	if newBox.Style.GetPosition().String == "relative" {
		if bo.TableCellT.IsInstance(newBox_) && len(*absoluteBoxes) != 0 {
			// gopdfgo: komórka tabeli dostanie ostateczną wysokość dopiero z wiersza; zapamiętujemy
			// pudełka pierwotne, żeby układ tabeli mógł powtórzyć pozycjonowanie (zob. tables.go)
			pending := make([]cellPositioned, len(*absoluteBoxes))
			for i, absoluteBox := range *absoluteBoxes {
				pending[i] = cellPositioned{placeholder: absoluteBox, original: absoluteBox.AliasBox, height: newBox.PaddingHeight()}
			}
			if context.cellPositioned == nil {
				context.cellPositioned = map[*bo.BoxFields][]cellPositioned{}
			}
			context.cellPositioned[newBox] = pending
		}
		// New containing block, resolve the layout of the absolute descendants
		for _, absoluteBox := range *absoluteBoxes {
			absoluteLayout(context, absoluteBox, newBox_, fixedBoxes, bottomSpace, nil)
		}
	}

	// gopdfgo: przy wysokości bloku zawierającego zależnej od treści procentowe top i bottom liczą się jak
	// auto (CSS 2.1 § 10.5 i errata do § 9.3.2, tak jak w Chromium); ujemna wysokość to sygnał dla
	// relativePositioning. W źródle procent liczył się od wysokości ułożonego bloku (test WPT top-115).
	cbHeight := newBox.Height.V()
	if box.Height == pr.AutoF && !bo.TableCellT.IsInstance(newBox_) {
		cbHeight = -1
	}
	for _, child := range newBox.Children {
		relativePositioning(child, bo.Point{newBox.Width.V(), cbHeight})
	}

	if bo.EstablishesFormattingContext(newBox_) || wrapperContext {
		context.finishBlockFormattingContext(newBox_)
	}

	// gopdfgo: pudełko, którego wysokość wyznaczają elementy pływające kontynuowane na następnej stronie
	// (np. display: flow-root z wysokim elementem pływającym), też jest dzielone, żeby jego tło
	// i obramowanie sięgały tak daleko jak te elementy. W źródle pudełko kończyło się na pierwszej stronie,
	// a dalszy ciąg elementu pływającego zostawał bez tła.
	if splitHeight < 0 && resumeAt == nil && isStart && floatRemaining > 0.5 && floatRemaining < maxSliceableHeight && box.Height == pr.AutoF &&
		bo.EstablishesFormattingContext(newBox_) && canSplitFixedHeight(context, box_, bottomSpace) {
		if available := context.pageBottom - bottomSpace - newBox.ContentBoxY(); available > 0 {
			splitHeight = available
			resumeAt = tree.RemainderResume(float64(floatRemaining))
			newBox_.RemoveDecoration(newBox, false, true)
		}
	}

	if splitHeight >= 0 {
		newBox.Height = splitHeight
	} else if resumedRemainder {
		// ostatni fragment podzielonego bloku: reszta wysokości, już po uwzględnieniu min i max
	} else if discard || !boxIsFragmented {
		// After finishBlockFormattingContext which may increment
		// newBox.Height
		clamped := max(min(newBox.Height.V(), newBox.MaxHeight.V()), newBox.MinHeight.V())
		// gopdfgo: gdy min-height albo max-height zmienia wysokość automatyczną, dolny margines ostatniego
		// dziecka nie wychodzi poza pudełko (testy WPT margin-collapse-min-height-*, *-height-separates-margin).
		// W źródle margines przechodził na zewnątrz i odsuwał następne pudełko; to zachowanie, zgodne
		// z dosłownym tekstem CSS 2.2 § 10.7, włącza Behavior.MarginsEscapeHeightLimits (ADR 0006).
		if box.Height == pr.AutoF && clamped != newBox.Height.V() && !collapsingThrough &&
			!context.behavior.MarginsEscapeHeightLimits {
			adjoiningMargins = new([]pr.Float)
		}
		newBox.Height = clamped
	} else if bottomSpace > -pr.Inf && !newBox.IsColumn {
		// gopdfgo: blok o podanej wysokości, którego dzieci dzielą się między strony, zajmuje na tej
		// stronie miejsce do jej dolnej krawędzi, a resztę wysokości przenosi na następną (CSS
		// Fragmentation 3 § 5.1). W źródle każdy fragment dostawał całą podaną wysokość: blok wystawał
		// poza stronę, więc był przenoszony na następną, a jego ostatni fragment wypełniał całą stronę.
		fixedFragment := false
		if box.Height != pr.AutoF && resumeAt != nil && !finishedWithOverflow && canSplitFixedHeight(context, box_, bottomSpace) {
			used := box.Height.V()
			if !resumedFixed {
				used = max(min(used, box.MaxHeight.V()), box.MinHeight.V())
			}
			if available := context.pageBottom - bottomSpace - newBox.ContentBoxY(); available > 0 && used-available > 0.5 {
				newBox.Height = available
				if context.fixedHeightRemaining == nil {
					context.fixedHeightRemaining = map[uintptr]pr.Float{}
				}
				context.fixedHeightRemaining[resumeKey(resumeAt)] = used - available
				fixedFragment = true
			}
		}
		// Make the box fill the blank space at the bottom of the page
		// https://www.w3.org/TR/css-break-3/#box-splitting
		newBoxHeight := context.pageBottom - bottomSpace - newBox.PositionY - (newBox.MarginHeight() - newBox.Height.V())
		if !fixedFragment && newBoxHeight > newBox.Height.V() {
			newBox.Height = newBoxHeight
			if drawBottomDecoration {
				newBox.Height = newBox.Height.V() + box.PaddingBottom.V() + box.BorderBottomWidth.V() + box.MarginBottom.V()
			}
		}
	}

	if !nextPage.HasPage {
		_, nextPage.Page = newBox.PageValues()
		nextPage.HasPage = true
	}

	if overflowContinuation {
		newBox.Height = pr.Float(0)
	}
	if finishedWithOverflow && resumeAt != nil {
		resumeAt = tree.OverflowResume(resumeAt)
	}
	return newBox_, blockLayout{
		resumeAt: resumeAt, nextPage: nextPage,
		adjoiningMargins: *adjoiningMargins, collapsingThrough: collapsingThrough,
	}, maxLines
}

// marginChainOpen (gopdfgo) podaje, czy górny margines rodzica nadal przylega do górnego marginesu
// następnego dziecka: wszystkie dotychczasowe dzieci w przepływie są puste, marginesy przez nie
// przechodzą i żadne nie dostało clearance (CSS 2.1 § 8.3.1). Puste wiersze się nie liczą.
func marginChainOpen(children []Box) bool {
	for _, child := range children {
		if line, isLine := child.(*bo.LineBox); isLine && line.Height == pr.Float(0) && isEmptyLinebox(line) {
			continue
		}
		if !child.Box().IsInNormalFlow() {
			continue
		}
		if !child.Box().CollapsedThrough {
			return false
		}
		if bl, ok := child.(bo.BlockLevelBoxITF); ok && bl.BlockLevel().Clearance != nil {
			return false
		}
	}
	return true
}

// allCollapsedThrough podaje, czy marginesy wszystkich dzieci w przepływie zwijają się przez nie
// i żadne z nich nie dostało clearance.
func allCollapsedThrough(children []Box) bool {
	for _, child := range children {
		box := child.Box()
		if !box.IsInNormalFlow() {
			continue
		}
		if !box.CollapsedThrough {
			return false
		}
		// clearance oddziela górny margines dziecka od marginesów rodzica
		if bl, ok := child.(bo.BlockLevelBoxITF); ok && bl.BlockLevel().Clearance != nil {
			return false
		}
	}
	return true
}

// endsWithClearedEmptyBox podaje, czy końcowe marginesy przyległe obejmują marginesy pudełka
// z clearance, przez które marginesy się zwijają.
func endsWithClearedEmptyBox(children []Box) bool {
	for _, child := range reversedBoxes(children) {
		box := child.Box()
		if !box.IsInNormalFlow() {
			continue
		}
		if !box.CollapsedThrough {
			return false
		}
		if bl, ok := child.(bo.BlockLevelBoxITF); ok && bl.BlockLevel().Clearance != nil {
			return true
		}
	}
	return false
}

func findLastInFlowChild(children []Box) Box {
	for _, previousChild := range reversedBoxes(children) {
		if previousChild.Box().IsInNormalFlow() {
			return previousChild
		}
	}
	return nil
}

// stop : whether we should stop parent rendering after this layout
// resumeAt : where to resume in-flow rendering
// newChild : child rendered by this layout
// outOfFlowLayoutResumeAt : where to resume out-of-flow rendering
func outOfFlowLayout(context *layoutContext, box bo.Box, index int, child_ Box, newChildren *[]Box,
	pageIsEmpty bool, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder, adjoiningMargins []pr.Float, bottomSpace pr.Float,
) (stop bool, resumeAt tree.ResumeStack, newChild Box, outOfFlowLayoutResumeAt tree.ResumeStack) {
	child := child_.Box()

	// Add the parent’s collapsing margins to shift the child’s position. Don’t
	// include the out-of-flow child’s top margin because it doesn’t collapse
	// with its parent.
	child.PositionY += collapseMargin(adjoiningMargins)

	if traceMode {
		traceLogger.Dump(fmt.Sprintf("out of flow: %d %v", index, child.IsAbsolutelyPositioned()))
	}

	// Absolute child layout: create placeholder.
	if child.IsAbsolutelyPositioned() {
		placeholder := NewAbsolutePlaceholder(child_)
		placeholder.Box().Index = index
		newChild = placeholder
		*newChildren = append(*newChildren, placeholder)
		if child.Style.GetPosition().String == "absolute" {
			*absoluteBoxes = append(*absoluteBoxes, placeholder)
		} else {
			*fixedBoxes = append(*fixedBoxes, placeholder)
		}
	} else if child.IsFloated() { // Float child layout.
		newChild, outOfFlowLayoutResumeAt = floatLayout(context, child_, box.Box(), absoluteBoxes, fixedBoxes, bottomSpace, nil)
		// Check that child doesn’t overflow page.
		pageOverflow := context.overflowsPage(bottomSpace, newChild.Box().PositionY+newChild.Box().Height.V())
		addChild := (pageIsEmpty && len(*newChildren) == 0) || !pageOverflow || bo.IsMonolithic(box)
		if addChild {
			// Child fits or has to fit, add it.
			newChild.Box().Index = index
			*newChildren = append(*newChildren, newChild)
		} else {
			// Child doesn’t fit and we can break, find where to break and stop
			// parent rendering.
			lastInFlowChild := findLastInFlowChild(*newChildren)
			pageBreak := blockLevelPageBreak(lastInFlowChild, child_)
			resumeAt = tree.ResumeStack{index: nil}
			stop = true
			if len(*newChildren) != 0 && avoidPageBreak(pageBreak, context) {
				// Can’t break inside float, find an earlier page break.
				r1, r2 := findEarlierPageBreak(context, *newChildren, absoluteBoxes, fixedBoxes)
				if r1 != nil || r2 != nil {
					// Earlier page break found, drop whole child rendering.
					*newChildren, resumeAt = r1, r2
					newChild = nil
					outOfFlowLayoutResumeAt = nil
				}
			}
		}
	} else if child.IsRunning() { // Running element layout.
		context.addRunning(child_)
	}
	return stop, resumeAt, newChild, outOfFlowLayoutResumeAt
}

func breakLine(context *layoutContext, box *bo.BoxFields, line *bo.LineBox, newChildren *[]Box, nextLines int,
	pageIsEmpty bool, index int, skipStack, resumeAt tree.ResumeStack,
	absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder,
) (bool, bool, tree.ResumeStack) {
	overOrphans := len(*newChildren) - int(box.Style.GetOrphans())
	if overOrphans < 0 && !pageIsEmpty {
		// Reached the bottom of the page before we had
		// enough lines for orphans, cancel the whole box.
		return true, false, resumeAt
	}
	// How many lines we need on the next page to satisfy widows
	// -1 for the current line.
	needed := max(int(box.Style.GetWidows())-1-nextLines, 0)

	if needed > overOrphans && !pageIsEmpty {
		// Total number of lines < orphans + widows
		return true, false, resumeAt
	}
	if needed != 0 && needed <= overOrphans {
		// Remove lines to keep them for the next page
		cut := len(*newChildren) - needed
		for _, child := range (*newChildren)[cut:] {
			removePlaceholders(context, child.Box().Children, absoluteBoxes, fixedBoxes)
		}
		(*newChildren) = (*newChildren)[:cut]
	}
	// Page break here, resume before this line
	removePlaceholders(context, line.Children, absoluteBoxes, fixedBoxes)
	return false, true, tree.ResumeStack{index: skipStack}
}

// newChildren and the returned newChildren are actually *LineBox
func lineBoxLayout(context *layoutContext, box_ Box, index int, child_ *bo.LineBox, newChildren []Box,
	pageIsEmpty bool, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder, adjoiningMargins []pr.Float, bottomSpace, positionY pr.Float,
	skipStack tree.ResumeStack, firstLetterStyle pr.ElementStyle, maxLines int) (
	abort, stop bool, resumeAt tree.ResumeStack, _ pr.Float, _ []Box, newFootnotes []Box, _ int,
) {
	box := box_.Box()
	if len(box.Children) != 1 {
		panic("line box with siblings before layout")
	}
	if len(adjoiningMargins) != 0 {
		positionY += collapseMargin(adjoiningMargins)
		adjoiningMargins = nil
	}

	if traceMode {
		traceLogger.DumpTree(child_, "at lineBoxLayout")
	}

	// gopdfgo: wiersz wyższy niż strona (obraz albo inline-block w wierszu tekstu) jest krojony między
	// strony: każda strona pokazuje kolejny wycinek tego samego wiersza. W źródle taki wiersz wystawał
	// poza stronę i jego dalsza część ginęła.
	sliceConsumed, lineStart, sliced := skipStack.Slice()
	if sliced {
		skipStack = lineStart
	}
	canSlice := bottomSpace > -pr.Inf && context.pageBottom != pr.Inf && !context.inColumn && context.flexMeasuring == 0 &&
		context.clipDepth == 0

	linesIterator := iterLineBoxes(context, child_, positionY, bottomSpace, skipStack,
		box_, absoluteBoxes, fixedBoxes, firstLetterStyle)
	linesIterator.maxLines = maxLines // gopdfgo: text-wrap-style: balance nie wyrównuje bloku obcinanego przez line-clamp
	for i := 0; linesIterator.Has(); i++ {
		tmp := linesIterator.Next()
		resumeAt = tmp.resumeAt
		line_, line := tmp.line, tmp.line.Box()

		// wysokość nieskończona (błąd układu pudełka w wierszu) dawałaby nieskończenie wiele wycinków
		if fullHeight := line.Height.V(); canSlice && i == 0 && fullHeight < maxSliceableHeight && (sliced || (pageIsEmpty && len(newChildren) == 0 && fullHeight > context.pageHeight && hasTallAtomicInline(line_, context.pageHeight))) {
			available := context.pageBottom - bottomSpace - line.PositionY
			if remaining := fullHeight - pr.Float(sliceConsumed); available > 0 && remaining > 0 {
				visible := min(remaining, available)
				line.SliceFullHeight = fullHeight
				line.SliceOffset = pr.Float(sliceConsumed)
				line.Height = visible
				if rest := remaining - visible; rest > 0.5 {
					line_.ResumeAt = tree.SliceResume(sliceConsumed+float64(visible), skipStack)
					newChildren = append(newChildren, line_)
					positionY = line.PositionY + visible
					stop = true
					break
				}
			}
		}

		// Break box if we reached max-lines
		if maxLines != -1 {
			if maxLines == 0 {
				newChildren[len(newChildren)-1].(*bo.LineBox).BlockEllipsis = box.Style.GetBlockEllipsis()

				if traceMode {
					traceLogger.Dump("lineBoxLayout -> maxLines == 0")
				}
				break
			}
			maxLines -= 1
		}
		// Update line resume_at and position_y
		line_.ResumeAt = resumeAt
		newPositionY := line.PositionY + line.Height.V()
		// Add bottom padding and border to the bottom position of the box if needed
		var offsetY pr.Float
		if resumeAt == nil || box.Style.GetBoxDecorationBreak() == "clone" {
			offsetY = box.BorderBottomWidth.V() + box.PaddingBottom.V()
		}

		// Allow overflow if the first line of the page is higher than the page itself so
		// that we put *something* on this page and can advance in the context.
		overflow := (len(newChildren) != 0 || !pageIsEmpty) && context.overflowsPage(bottomSpace, newPositionY+offsetY)
		if overflow {
			// If we couldn’t break the line before but can break now, first try to
			// report footnotes and see if we don’t overflow.
			couldBreakBefore, canBreakNow := true, true
			nextLines := linesIterator.consume()
			if len(newChildren)+1 < int(box.Style.GetOrphans()) {
				canBreakNow = false
			} else if nextLines < int(box.Style.GetWidows()) {
				canBreakNow = false
			}
			if len(newChildren) < int(box.Style.GetOrphans()) {
				couldBreakBefore = false
			} else if nextLines+1 < int(box.Style.GetWidows()) {
				couldBreakBefore = false
			}
			report := !context.inColumn && canBreakNow && !couldBreakBefore
			reportedFootnotes := 0
			hasBroken := false
			for report && len(context.currentPageFootnotes) != 0 {
				context.reportFootnote(context.currentPageFootnotes[len(context.currentPageFootnotes)-1])
				reportedFootnotes += 1
				if !context.overflowsPage(bottomSpace, newPositionY+offsetY) {
					newChildren = append(newChildren, line_)
					stop = true
					hasBroken = true
					break
				}
			}
			if !hasBroken {
				abort, stop, resumeAt = breakLine(context, box, line_, &newChildren, nextLines,
					pageIsEmpty, index, skipStack, resumeAt, absoluteBoxes, fixedBoxes)
			}

			if traceMode {
				traceLogger.Dump("lineBoxLayout -> overflow")
			}

			// Revert reported footnotes, as they’ve been reported starting from the last
			// one.
			if reportedFootnotes >= 2 {
				slices.Reverse(context.reportedFootnotes[len(context.reportedFootnotes)-reportedFootnotes:])
			}

			break
			// See https://drafts.csswg.org/css-page-3/#allowed-pg-brk
			// "When an unforced page break occurs here, both the adjoining
			//  ‘margin-top’ and ‘margin-bottom’ are set to zero."
			// See https://github.com/Kozea/WeasyPrint/issues/115
		} else if pageIsEmpty && context.overflowsPage(bottomSpace, newPositionY) {
			// Remove the top border when a page is empty && the box is
			// too high to be drawn := range one page
			newPositionY -= box.MarginTop.V()
			line_.Translate(0, -box.MarginTop.V(), false)
			box.MarginTop = pr.Float(0)
		}

		if len(context.footnotes) != 0 {
			breakLinebox := false
			var footnotes []Box
			for _, descendant := range bo.Descendants(line_) {
				if ftn := descendant.Box().Footnote; isInBoxes(ftn, context.footnotes) {
					footnotes = append(footnotes, ftn)
				}
			}
			for _, footnote := range footnotes {
				overflow := context.layoutFootnote(footnote)
				newFootnotes = append(newFootnotes, footnote)
				overflow = overflow || len(context.reportedFootnotes) != 0 || context.overflowsPage(bottomSpace, newPositionY+offsetY)
				if overflow {
					context.reportFootnote(footnote)
					// If we've put other content on this page, then we may want
					// to push this line or block to the next page. Otherwise,
					// we can't (and would loop forever if we tried), so don't
					// even try.
					if len(newChildren) != 0 || !pageIsEmpty {
						if footnote.Box().Style.GetFootnotePolicy() == "line" {
							nextLines := linesIterator.consume()
							abort, stop, resumeAt = breakLine(
								context, box, line_, &newChildren, nextLines, pageIsEmpty,
								index, skipStack, resumeAt, absoluteBoxes, fixedBoxes)
							breakLinebox = true
						} else if footnote.Box().Style.GetFootnotePolicy() == "block" {
							abort, breakLinebox = true, true
						}
						break
					}
				}
			}
			if breakLinebox {

				if traceMode {
					traceLogger.Dump("lineBoxLayout -> break for footnotes")
				}

				break
			}
		}

		newChildren = append(newChildren, line_)
		positionY = newPositionY
		skipStack = resumeAt

		if traceMode {
			traceLogger.DumpTree(line_, fmt.Sprintf("lineBoxLayout at line %d -> %s", i, skipStack))
		}
	}

	if len(newChildren) != 0 {
		resumeAt = tree.ResumeStack{index: newChildren[len(newChildren)-1].(*bo.LineBox).ResumeAt}
	}

	return abort, stop, resumeAt, positionY, newChildren, newFootnotes, maxLines
}

func inFlowLayout(context *layoutContext, box_ bo.Box, index int, child_ Box, newChildren []Box,
	pageIsEmpty bool, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder, adjoiningMargins *[]pr.Float, bottomSpace, positionY pr.Float,
	skipStack tree.ResumeStack, firstLetterStyle pr.ElementStyle, collapsingWithChildren, discard bool,
	maxLines int) (
	abort, stop bool, resumeAt tree.ResumeStack, _ pr.Float, _ []pr.Float, nextPage tree.PageBreak, _ []Box, _ int,
) {
	box := box_.Box()
	lastInFlowChild := findLastInFlowChild(newChildren)
	child := child_.Box()
	pageBreak := "auto"
	if lastInFlowChild != nil {
		// Between in-flow siblings
		pageBreak = blockLevelPageBreak(lastInFlowChild, child_)
		if (!context.behavior.Continuous && pageNameChanges(lastInFlowChild, child_)) || forcePageBreak(pageBreak, context) {
			pageName, _ := child.PageValues()
			nextPage = tree.PageBreak{Break: pageBreak, Page: pageName, HasPage: true}
			resumeAt = tree.ResumeStack{index: nil}
			stop = true
			return abort, stop, resumeAt, positionY, *adjoiningMargins, nextPage, newChildren, maxLines
		}
	}

	newContainingBlock := box
	if _, anonymous := child.Style.(*tree.AnonymousStyle); anonymous && bo.BlockT.IsInstance(child_) && child.Style.GetHeight().Tag == pr.Auto {
		// gopdfgo: zob. BoxFields.AnonymousBaseHeight; w źródle procentowa wysokość obrazu obok bloku
		// liczyła się od anonimowego bloku o wysokości auto, czyli jak auto
		_, child.AnonymousBaseHeight = box.ContainingBlock()
		if child.AnonymousBaseHeight == pr.AutoF {
			child.AnonymousBaseHeight = nil
		}
	}

	if !newContainingBlock.IsTableWrapper {
		resolveSizesBox(context, child_, newContainingBlock)
		// gopdfgo: górny margines rodzica zwija się nie tylko z marginesem pierwszego dziecka, ale też
		// z marginesami kolejnych, dopóki poprzednie dzieci są puste i marginesy przez nie przechodzą
		// (CSS 2.1 § 8.3.1). Elementy pływające z tych pustych dzieci i stojące przed nimi przylegają
		// do górnej krawędzi rodzica, więc przesuwają się razem z nią (§ 8.3.1: górna krawędź pudełka,
		// którego margines zwija się z marginesem rodzica, jest krawędzią rodzica). W źródle działo się
		// to tylko dla pierwszego dziecka w przepływie i bez marginesów jego pierwszych dzieci.
		if collapsingWithChildren && marginChainOpen(newChildren) {
			oldCollapsedMargin := collapseMargin(*adjoiningMargins)
			childMarginTop := child.MarginTop
			if childMarginTop == pr.AutoF {
				childMarginTop = pr.Float(0)
			} else if atBreak, forcedBreak := context.breakAtFragmentTop(); atBreak && pageIsEmpty {
				if mb := box.Style.GetMarginBreak(); mb == "discard" || (mb == "auto" && !forcedBreak) {
					childMarginTop = pr.Float(0)
				}
			} else if box.MarginTopTruncated {
				// gopdfgo: margines pierwszego dziecka zwija się z obciętym marginesem rodzica, więc też
				// przylega do podziału kolumny (CSS Fragmentation 3 § 5.2)
				childMarginTop = pr.Float(0)
			}
			chain := append(append([]pr.Float(nil), *adjoiningMargins...), childMarginTop.V())
			var avoider Box // pudełko omijające elementy pływające, którego margines należy do łańcucha
			if avoidsFloats(child_) {
				avoider = child_
			}
			if childMarginTop == child.MarginTop && !box.MarginTopTruncated {
				// gopdfgo: z marginesami pierwszych dzieci, które zwijają się przez górną krawędź pudełka
				inner, last := leadingChildChain(context, child_, child.PositionY, chain)
				if last != nil && avoidsFloats(last) {
					avoider = last
				}
				chain = append(chain, inner...)
			}
			newCollapsedMargin := collapseMargin(chain)
			collapsedMarginDifference := newCollapsedMargin - oldCollapsedMargin
			shapesBefore := make([]pr.Float, len(context.excludedShapes.list))
			for i, shape := range context.excludedShapes.list {
				shapesBefore[i] = shape.PositionY
			}
			for _, previousNewChild := range newChildren {
				previousNewChild.Translate(0, collapsedMarginDifference, false)
			}
			movedShapes := false
			for i, shape := range context.excludedShapes.list {
				if i < len(shapesBefore) && shape.PositionY != shapesBefore[i] {
					movedShapes = true
				}
			}

			// gopdfgo: pudełko z własnym kontekstem formatowania (także tabela i element zastępowany), które
			// na pozycji hipotetycznej nie mieści się obok przylegających elementów pływających, oddziela
			// swój margines od nich jak przy clearance: elementy pływające zostają na miejscu, a pudełko staje
			// pod nimi bez względu na margines (CSS 2.1 § 9.5: takie pudełko implementacja ma „oczyścić”;
			// testy WPT new-fc-separates-from-float-*). W źródle margines pociągał elementy pływające w dół.
			if avoider != nil && movedShapes &&
				!fitsBesideFloats(context, avoider, child.PositionY+newCollapsedMargin, box) {
				for _, previousNewChild := range newChildren {
					previousNewChild.Translate(0, -collapsedMarginDifference, false)
				}
				if context.separatedFromFloats == nil {
					context.separatedFromFloats = map[*bo.BoxFields]bool{}
				}
				context.separatedFromFloats[avoider.Box()] = true
				if avoider == child_ {
					chain = chain[:len(*adjoiningMargins)]
				} else {
					chain = chain[:len(chain)-1]
				}
				newCollapsedMargin = collapseMargin(chain)
				collapsedMarginDifference = newCollapsedMargin - oldCollapsedMargin
				for _, previousNewChild := range newChildren {
					previousNewChild.Translate(0, collapsedMarginDifference, false)
				}
				if avoider == child_ {
					// łańcuch marginesów rodzica kończy się przed tym pudełkiem
					shift := oldCollapsedMargin - box.MarginTop.V()
					box.PositionY += shift
					positionY = box.ContentBoxY()
					box.PositionY -= shift
					child.PositionY = positionY
					adjoiningMargins = new([]pr.Float)
				}
			} else if clearance := getClearance(context, child, newCollapsedMargin); clearance != nil {
				// gopdfgo: gdy bez clearance pudełko pociągnęłoby za sobą przylegający element pływający,
				// który ma oczyścić, clearance jest konieczne bez względu na wielkość marginesu, a krawędź
				// pudełka staje pod tym elementem (CSS 2.1 § 9.5.2, test WPT adjoining-float-before-clearance).
				// W źródle po cofnięciu elementów pływających pozycja hipotetyczna liczyła się od nowa i duży
				// margines znosił clearance.
				clear := child.Style.GetClear()
				for i, shape := range context.excludedShapes.list {
					if i < len(shapesBefore) && shape.PositionY != shapesBefore[i] &&
						(clear == "both" || clear == shape.Style.GetFloat()) {
						if context.forcedClearance == nil {
							context.forcedClearance = map[*bo.BoxFields]bool{}
						}
						context.forcedClearance[child] = true
						break
					}
				}
				for _, previousNewChild := range newChildren {
					previousNewChild.Translate(0, -collapsedMarginDifference, false)
				}

				collapsedMargin := collapseMargin(*adjoiningMargins)
				// Count box.MarginTop as we emptied adjoiningMargins
				// gopdfgo: położenie treści liczone jest już z marginesem zwiniętym, ale samo pudełko
				// zostaje nieprzesunięte, bo przesuwa je koniec układu pudełka (thisBoxAdjoiningMargins,
				// wspólne z przodkami); w źródle pudełko było przesuwane tu i drugi raz na końcu.
				shift := collapsedMargin - box.MarginTop.V()
				box.PositionY += shift
				positionY = box.ContentBoxY()
				box.PositionY -= shift
				adjoiningMargins = new([]pr.Float)
			}
		}
	}
	child.MarginTopTruncated = false
	// gopdfgo: pierwsze pudełko kolumny stojącej za podziałem kolumn przylega do tego podziału jak
	// pierwsze pudełko strony, choć kolumna nie zwija marginesów z dziećmi, a przy równoważeniu kolumny
	// nie są układane jako puste (CSS Fragmentation 3 § 5.2). W źródle margines zostawał.
	if box.IsColumn && lastInFlowChild == nil && context.columnBreak != columnBreakNone && child.IsInNormalFlow() {
		if mb := child.Style.GetMarginBreak(); mb == "discard" || (mb == "auto" && context.columnBreak == columnBreakUnforced) {
			child.MarginTopTruncated = true
		}
	}
	if len(*adjoiningMargins) != 0 {
		if box.IsTableWrapper { // should not be a special case
			collapsedMargin := collapseMargin(*adjoiningMargins)
			child.PositionY += collapsedMargin
			adjoiningMargins = new([]pr.Float)
		} else if !bo.BlockT.IsInstance(child_) { // blocks handle that themselves
			var marginTop pr.Float
			if mt := child.Style.GetMarginTop(); mt.Tag != pr.Auto {
				marginTop = pr.ResolvePercentage(mt, box.Width.V()).V()
			}
			// gopdfgo: margines przylegający do zwykłego podziału strony jest obcinany do zera także
			// dla kontenerów flex i siatki oraz elementów zastępowanych (CSS Fragmentation 3 § 5.2).
			// W źródle robiły to tylko bloki, bo tylko one dostają adjoiningMargins.
			if atBreak, forcedBreak := context.breakAtFragmentTop(); atBreak && pageIsEmpty && !hasPlacedChild(newChildren) {
				if mb := child.Style.GetMarginBreak(); mb == "discard" || (mb == "auto" && !forcedBreak) {
					marginTop = 0
					child.MarginTopTruncated = true
				}
			}
			*adjoiningMargins = append(*adjoiningMargins, marginTop)
			offsetY := collapseMargin(*adjoiningMargins) - marginTop
			child.PositionY += offsetY
			adjoiningMargins = new([]pr.Float)
		}
	}

	pageIsEmptyWithNoChildren := pageIsEmpty && !hasPlacedChild(newChildren)

	if child.FirstLetterStyle == nil {
		child.FirstLetterStyle = firstLetterStyle
	}

	newChild_, tmp, maxLines := blockLevelLayout(context, child_.(bo.BlockLevelBoxITF), bottomSpace, skipStack,
		newContainingBlock, pageIsEmptyWithNoChildren, absoluteBoxes, fixedBoxes, adjoiningMargins, discard, maxLines)
	resumeAt, nextPage = tmp.resumeAt, tmp.nextPage
	nextAdjoiningMargins, collapsingThrough := tmp.adjoiningMargins, tmp.collapsingThrough

	if traceMode {
		traceLogger.Dump(fmt.Sprintf("in inFlowLayout: blockLevelLayout -> %s", resumeAt))
	}

	if newChild_ != nil {
		newChild := newChild_.Box()
		if !collapsingThrough {
			newContentPositionY := newChild.ContentBoxY() + newChild.Height.V()
			newPositionY := newChild.BorderBoxY() + newChild.BorderHeight()
			contentPageOverflow := context.overflowsPage(bottomSpace, newContentPositionY)
			borderPageOverflow := context.overflowsPage(bottomSpace, newPositionY)
			canBreak := !(pageIsEmptyWithNoChildren || bo.IsMonolithic(box_))
			if canBreak && contentPageOverflow {
				// The child content overflows the page area, display it on the
				// next page.
				removePlaceholders(context, []Box{newChild_}, absoluteBoxes, fixedBoxes)
				newChild_ = nil
			} else if canBreak && borderPageOverflow {
				// The child border/padding overflows the page area, do the
				// layout again with a higher bottomSpace value.
				removePlaceholders(context, []Box{newChild_}, absoluteBoxes, fixedBoxes)
				bottomSpace += newChild.PaddingBottom.V() + newChild.BorderBottomWidth.V()

				newChild_, tmp, maxLines = blockLevelLayout(context, child_.(bo.BlockLevelBoxITF), bottomSpace, skipStack,
					newContainingBlock, pageIsEmptyWithNoChildren, absoluteBoxes, fixedBoxes, adjoiningMargins, discard, maxLines)
				resumeAt, nextPage = tmp.resumeAt, tmp.nextPage
				nextAdjoiningMargins = tmp.adjoiningMargins
				if newChild_ != nil {
					newChild = newChild_.Box()
					positionY = (newChild.BorderBoxY() + newChild.BorderHeight())
				}
			} else {
				positionY = newPositionY
			}
		}

		adjoiningMargins = &nextAdjoiningMargins
		if newChild_ != nil {
			*adjoiningMargins = append(*adjoiningMargins, newChild.MarginBottom.V())
		}

		if newChild_ != nil && newChild_.BlockLevel().Clearance != nil {
			positionY = newChild.BorderBoxY() + newChild.BorderHeight()
			if collapsingThrough {
				// gopdfgo: marginesy pustego pudełka z clearance zwijają się ze sobą i z marginesami
				// następnych pudełek; zwinięty margines liczy się od górnej krawędzi marginesu,
				// nie od krawędzi obramowania (CSS 2 §8.3.1). W źródle górny margines liczył się dwukrotnie.
				positionY = newChild.PositionY
			}
		}
	}

	skipStack = nil

	if newChild_ == nil {
		// Nothing fits in the remaining space of this page: break
		if avoidPageBreak(pageBreak, context) {
			r1, r2 := findEarlierPageBreak(context, newChildren, absoluteBoxes, fixedBoxes)
			if r1 != nil || r2 != nil {
				newChildren, resumeAt = r1, r2
				stop = true
				return abort, stop, resumeAt, positionY, *adjoiningMargins, nextPage, newChildren, maxLines
			} else {
				// We did not find any page break opportunity
				if !pageIsEmpty {
					// The page has content *before* this block:
					// cancel the block and try to find a break
					// in the parent.
					abort = true
					return abort, stop, resumeAt, positionY, *adjoiningMargins, nextPage, newChildren, maxLines
				}
				// else : ignore this "avoid" and break anyway.
			}
		}
		allAbsPos := true
		for _, child := range newChildren {
			if !child.Box().IsAbsolutelyPositioned() {
				allAbsPos = false
				break
			}
		}
		if allAbsPos {
			// This box has only rendered absolute children, keep them
			// for the next page. This is for example useful for list
			// markers.
			removePlaceholders(context, newChildren, absoluteBoxes, fixedBoxes)
			newChildren = nil
		}
		if len(newChildren) != 0 {
			resumeAt = tree.ResumeStack{index: nil}
			stop = true
		} else {
			// This was the first child of this box, cancel the box
			// completly
			abort = true
		}
		return abort, stop, resumeAt, positionY, *adjoiningMargins, nextPage, newChildren, maxLines
	}

	// index in its non-laid-out parent, not in future new parent
	// May be used in findEarlierPageBreak()
	newChild_.Box().Index = index
	newChildren = append(newChildren, newChild_)
	if resumeAt != nil {
		resumeAt = tree.ResumeStack{index: resumeAt}
		stop = true
	}

	return abort, stop, resumeAt, positionY, *adjoiningMargins, nextPage, newChildren, maxLines
}

// Return the amount of collapsed margin for a list of adjoining margins.
func collapseMargin(adjoiningMargins []pr.Float) pr.Float {
	var maxPos, minNeg pr.Float
	for _, m := range adjoiningMargins {
		if m > maxPos {
			maxPos = m
		} else if m < minNeg {
			minNeg = m
		}
	}
	return maxPos + minNeg
}

// https://drafts.csswg.org/css-break-3/#possible-breaks
func isParallel(box Box) bool {
	return bo.BlockLevelT.IsInstance(box) || bo.TableRowGroupT.IsInstance(box) || bo.TableRowT.IsInstance(box)
}

// Return the value of “page-break-before“ or “page-break-after“
// that "wins" for boxes that meet at the margin between two sibling boxes.
// For boxes before the margin, the "page-break-after" value is considered;
// for boxes after the margin the "page-break-before" value is considered.
// * "avoid" takes priority over "auto"
// * "page" takes priority over "avoid" or "auto"
// * "left" or "right" take priority over "always", "avoid" or "auto"
// * Among "left" && "right", later values in the tree take priority.
//
// See https://drafts.csswg.org/css-page-3/#allowed-pg-brk
func blockLevelPageBreak(siblingBefore, siblingAfter Box) string {
	var values []pr.String

	box_ := siblingBefore
	for isParallel(box_) {
		box := box_.Box()
		values = append(values, box.Style.GetBreakAfter())
		if len(box.Children) == 0 {
			break
		}
		box_ = box.Children[len(box.Children)-1]
	}
	slices.Reverse(values) // Have them in tree order

	box_ = siblingAfter
	for isParallel(box_) {
		box := box_.Box()
		values = append(values, box.Style.GetBreakBefore())
		if len(box.Children) == 0 {
			break
		}
		box_ = box.Children[0]
	}
	choices := map[[2]pr.String]bool{
		{"page", "auto"}:           true,
		{"page", "avoid"}:          true,
		{"page", "avoid-page"}:     true,
		{"page", "avoid-column"}:   true,
		{"column", "auto"}:         true,
		{"column", "avoid"}:        true,
		{"column", "avoid-page"}:   true,
		{"column", "avoid-column"}: true,
		{"avoid", "auto"}:          true,
		{"avoid-page", "auto"}:     true,
		{"avoid-column", "auto"}:   true,
	}
	var result pr.String = "auto"
	for _, value := range values {
		tmp := [2]pr.String{value, result}
		if value == "left" || value == "right" || value == "recto" || value == "verso" || choices[tmp] {
			result = value
		}

	}

	return string(result)
}

// pageNameChanges mówi, czy między rodzeństwem zmienia się nazwa strony, co wymusza podział.
//
// gopdfgo: w źródle funkcja zwracała nazwę następnej strony, a pusty napis znaczył "bez zmiany".
// Strona domyślna też ma pustą nazwę, więc powrót ze strony nazwanej na domyślną nie łamał strony.
func pageNameChanges(siblingBefore, siblingAfter Box) bool {
	_, beforePage := siblingBefore.PageValues()
	afterPage, _ := siblingAfter.PageValues()
	return beforePage != afterPage
}

// Find the last possible page break in “children“
// Because of a `page-break-before: avoid` or a `page-break-after: avoid`
// we need to find an earlier page break opportunity inside `children`.
// Absolute or fixed placeholders removed from children should also be
// removed from `absoluteBoxes` or `fixedBoxes`.
func findEarlierPageBreak(context *layoutContext, children []Box, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder) (newChildren []Box, resumeAt tree.ResumeStack) {
	if len(children) != 0 && bo.LineT.IsInstance(children[0]) {
		// Normally `orphans` and `widows` apply to the block container, but
		// line boxes inherit them.
		orphans := int(children[0].Box().Style.GetOrphans())
		widows := int(children[0].Box().Style.GetWidows())
		index := len(children) - widows // how many lines we keep
		if index < orphans {
			return nil, nil
		}
		newChildren = children[:index]
		resumeAt = tree.ResumeStack{0: newChildren[len(newChildren)-1].(*bo.LineBox).ResumeAt}
		removePlaceholders(context, children[index:], absoluteBoxes, fixedBoxes)
		return newChildren, resumeAt
	}

	var (
		previousInFlow Box
		index          int
		i_, L          = 0, len(children)
	)
	for i_ = 0; i_ < L; i_++ { // reversed(list(enumerate(children)))
		index = L - i_ - 1
		child_ := children[index]
		child := child_.Box()

		if bo.TableRowGroupT.IsInstance(child_) && (child.IsHeader || child.IsFooter) {
			// We don’t want to break pages before table headers or footers.
			continue
		} else if child.IsColumn {
			// We don’t want to break pages between columns.
			continue
		}

		if child.IsInNormalFlow() {
			pageBreak := blockLevelPageBreak(child_, previousInFlow)
			if previousInFlow != nil && !avoidPageBreak(pageBreak, context) {
				index += 1 // break after child
				newChildren = children[:index]
				// Get the index in the original parent
				resumeAt = tree.ResumeStack{children[index].Box().Index: nil}
				break
			}
			previousInFlow = child_
		}
		if bi := string(child.Style.GetBreakInside()); child.IsInNormalFlow() && !avoidPageBreak(bi, context) {
			if bo.BlockT.IsInstance(child_) || bo.TableT.IsInstance(child_) || bo.TableRowGroupT.IsInstance(child_) {
				newGrandChildren, resumeAtTmp := findEarlierPageBreak(context, child.Children, absoluteBoxes, fixedBoxes)
				if newGrandChildren != nil || resumeAtTmp != nil {
					resumeAt = resumeAtTmp
					newChild := bo.CopyWithChildren(child_, newGrandChildren)
					newChildren = append(children[:index], newChild)

					// Re-add footer at the end of split table
					if bo.TableRowGroupT.IsInstance(child_) {
						for _, nextChild := range children[index:] {
							if nextChild.Box().IsFooter {
								newChildren = append(newChildren, nextChild)
							}
						}
					}

					// Index in the original parent
					resumeAt = tree.ResumeStack{newChild.Box().Index: resumeAt}
					index += 1 // Remove placeholders after child
					break
				}
			}
		}
	}
	if i_ == L {
		return nil, nil
	}

	removePlaceholders(context, children[index:], absoluteBoxes, fixedBoxes)
	return newChildren, resumeAt
}

func removeFromAbsolutes(list *[]*AbsolutePlaceholder, box Box) {
	b := (*list)[:0]
	for _, x := range *list {
		if x != box {
			b = append(b, x)
		}
	}
	*list = b
}

// update boxes in place
func removeFromBoxes(list *[]Box, box Box) {
	b := (*list)[:0]
	for _, x := range *list {
		if x != box {
			b = append(b, x)
		}
	}
	*list = b
}

// For boxes that have been removed in findEarlierPageBreak(),
// also remove the matching placeholders in absoluteBoxes and fixedBoxes.
//
// Also takes care of removed footnotes and floats.
func removePlaceholders(context *layoutContext, boxList []Box, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder) {
	for _, box_ := range boxList {
		box := box_.Box()
		if bo.ParentT.IsInstance(box_) {
			removePlaceholders(context, box.Children, absoluteBoxes, fixedBoxes)
		}
		if box.Style.GetPosition().String == "absolute" {
			removeFromAbsolutes(absoluteBoxes, box_)
		} else if box.Style.GetPosition().String == "fixed" {
			removeFromAbsolutes(fixedBoxes, box_)
		}

		if box.Footnote != nil {
			context.unlayoutFootnote(box.Footnote)
		}
		delete(context.brokenOutOfFlow, box_)
	}
}

// Test whether we should avoid breaks.
func avoidPageBreak(pageBreak string, context *layoutContext) bool {
	if context.inColumn {
		return pageBreak == "avoid" || pageBreak == "avoid-page" || pageBreak == "avoid-column"
	}
	return pageBreak == "avoid" || pageBreak == "avoid-page"
}

// Test whether we should force breaks.
func forcePageBreak(pageBreak string, context *layoutContext) bool {
	if context.behavior.Continuous {
		// tryb ciągły nie ma stron: zostaje tylko wymuszony podział kolumny
		return context.inColumn && pageBreak == "column"
	}
	if context.inColumn {
		return pageBreak == "page" || pageBreak == "left" || pageBreak == "right" || pageBreak == "recto" || pageBreak == "verso" || pageBreak == "column"
	}
	return pageBreak == "page" || pageBreak == "left" || pageBreak == "right" || pageBreak == "recto" || pageBreak == "verso"
}

// canSplitFixedHeight mówi, czy blok o podanej wysokości wolno podzielić między strony w miejscu, gdzie
// kończy się strona: tylko zwykłe bloki w normalnym przepływie, bez kolumn, w układzie stronicowanym
// i bez przycinania treści.
func canSplitFixedHeight(context *layoutContext, box_ Box, bottomSpace pr.Float) bool {
	box := box_.Box()
	if !bo.BlockT.IsInstance(box_) || !(box.IsInNormalFlow() || box.IsAbsolutelyPositioned() || box.IsFloated()) || box.IsColumn || box.IsForRootElement {
		return false
	}
	// Elementy flex i grid mają własne algorytmy podziału; tam bloków nie tniemy.
	if box.IsFlexItem || box.IsGridItem {
		return false
	}
	if bottomSpace == -pr.Inf || context.pageBottom == pr.Inf || context.clipDepth > 0 {
		return false
	}
	// wysokość nieskończona albo absurdalnie duża dawałaby (prawie) nieskończenie wiele stron
	if box.Height != pr.AutoF && box.Height != nil && box.Height.V() >= maxSliceableHeight {
		return false
	}
	// Pudełko niepodzielne przez zawieranie rozmiaru (contain: size) tniemy między strony tak jak
	// zwykły blok o podanej wysokości: wyższe niż strona inaczej by z niej wystawało (CSS Break 3 § 4.1
	// pozwala wtedy ciąć; testy WPT css-page/monolithic-overflow-*). W kolumnach zostaje w całości.
	if box.Style.GetOverflow() != "visible" || (bo.IsMonolithic(box_) && (context.inColumn || !bo.HasContainment(box_, "size"))) {
		return false
	}
	if box.Style.GetColumnWidth().Tag != pr.Auto || box.Style.GetColumnCount().String != "auto" {
		return false
	}
	return true
}

// narrowBesideFloats zwęża pudełko tworzące nowy kontekst formatowania o szerokości auto do miejsca, które
// zostaje obok elementów pływających na jego wysokości, o ile mieści się tam jego min-width.
//
// gopdfgo: CSS 2.1 § 9.5 pozwala takie pudełko zwęzić albo przenieść pod element pływający; przeglądarki
// zwężają. W źródle pudełko dostawało pełną szerokość bloku zawierającego i, nie mieszcząc się obok,
// lądowało pod elementem pływającym.
func narrowBesideFloats(context *layoutContext, box_ Box, containingBlock *bo.BoxFields, adjoiningMargins []pr.Float) {
	box := box_.Box()
	width := box.Style.GetWidth()
	if len(context.excludedShapes.list) == 0 || box.IsTableWrapper || box.IsFlexItem || box.IsGridItem ||
		!box.IsInNormalFlow() || width.Tag != pr.Auto || width.Intrinsic != pr.IntrinsicNone ||
		!bo.EstablishesFormattingContext(box_) || box.Width == pr.AutoF {
		return
	}
	// Liczy się min-width, nie szerokość treści: treść szersza niż miejsce obok elementu pływającego
	// wystaje z pudełka (albo jest przycinana), a pudełko zostaje obok.
	minWidth := box.MinWidth.V()
	probe := box_.Copy()
	probe.Box().Width = minWidth
	probe.Box().Height = pr.Float(0.01)
	// Marginesy czekające na złożenie (np. margines body nad pierwszym blokiem)
	// nie są jeszcze wliczone w pozycję pudełka; bez nich sonda leżała nad
	// elementem pływającym, nie widziała go i pudełko nie było zwężane.
	if marginTop, ok := box.MarginTop.(pr.Float); ok && len(adjoiningMargins) != 0 {
		pending := append(append([]pr.Float{}, adjoiningMargins...), marginTop)
		probe.Box().PositionY += collapseMargin(pending) - marginTop
	}
	_, _, available := avoidCollisions(context, probe, containingBlock, false)
	available -= box.PaddingLeft.V() + box.PaddingRight.V() + box.BorderLeftWidth.V() + box.BorderRightWidth.V()
	if available < box.Width.V() && available > 0 && available >= minWidth {
		box.Width = available
	}
}

// hasTallAtomicInline mówi, czy wiersz zawiera pudełko niepodzielne (obraz, inline-block, inline-flex,
// inline-grid) wyższe niż limit. Tylko takie wiersze kroimy między strony; wiersz samego tekstu wyższy niż
// strona zostaje w całości, jak w źródle.
func hasTallAtomicInline(box Box, limit pr.Float) bool {
	for _, child := range box.Box().Children {
		if bo.AtomicInlineLevelT.IsInstance(child) || bo.InlineFlexT.IsInstance(child) || bo.InlineGridT.IsInstance(child) {
			if height := child.Box().Height; height != nil && height != pr.AutoF && child.Box().MarginHeight() > limit {
				return true
			}
		} else if bo.InlineT.IsInstance(child) && hasTallAtomicInline(child, limit) {
			return true
		}
	}
	return false
}

// maxSliceableHeight to górna granica wysokości wiersza, bloku albo obrazu dzielonego między strony
// (ok. 2600 stron A4); wyższe, w tym nieskończone, zostają niepodzielone.
const maxSliceableHeight pr.Float = 3_000_000

// gopdfgo: legenda ramki (HTML § 15.3.13 "The fieldset and legend elements"). Pierwszy element legend
// ramki fieldset leży na jej górnym obramowaniu, wyśrodkowany w pionie na linii obramowania, a obramowanie
// jest pod nim przerwane. W źródle legenda była zwykłym blokiem wewnątrz ramki.

// renderedLegend zwraca pierwsze dziecko ramki w normalnym przepływie, jeśli jest elementem legend.
func renderedLegend(fieldset *bo.BoxFields) Box {
	for _, child := range fieldset.Children {
		if !child.Box().IsInNormalFlow() {
			continue
		}
		if strings.EqualFold(child.Box().ElementTag(), "legend") && !bo.LineT.IsInstance(child) {
			return child
		}
		return nil
	}
	return nil
}

// prepareFieldsetLegend dostosowuje (raz) style ramki i jej legendy: legenda dostaje ujemny margines górny,
// który wynosi ją na środek górnego obramowania, a ramka większy margines górny, żeby legenda nie nachodziła
// na treść powyżej. Wysokość legendy przyjmujemy jak dla jednego wiersza tekstu.
func prepareFieldsetLegend(context *layoutContext, box_ Box) {
	box := box_.Box()
	if box.LegendPrepared || !strings.EqualFold(box.ElementTag(), "fieldset") {
		return
	}
	box.LegendPrepared = true
	legend := renderedLegend(box)
	if legend == nil {
		return
	}
	px := func(v pr.TaggedDim) pr.Float {
		if v.Tag == pr.Auto || v.Unit != pr.Px {
			return 0
		}
		return v.Value
	}
	legendStyle := legend.Box().Style.Copy()
	lineHeight, _ := text.StrutLayout(legendStyle, context)
	legendHeight := lineHeight + px(legendStyle.GetPaddingTop()) + px(legendStyle.GetPaddingBottom()) +
		legendStyle.GetBorderTopWidth().Value + legendStyle.GetBorderBottomWidth().Value
	borderTop := box.Style.GetBorderTopWidth().Value
	paddingTop := px(box.Style.GetPaddingTop())

	legendStyle.SetMarginTop(pr.FToPx(-(paddingTop + borderTop/2 + legendHeight/2)))
	// gopdfgo: treść ramki zaczyna się pod legendą, w odległości górnego odstępu ramki (WHATWG HTML
	// § 15.5.20: odstęp ramki dotyczy anonimowego pudełka treści, które stoi pod legendą); wcześniej
	// pierwsza linia treści wypadała o ten odstęp za wysoko
	// (odstęp wewnętrzny legendy, nie margines: margines zwinąłby się z marginesem pierwszego dziecka)
	legendStyle.SetPaddingBottom(pr.FToPx(px(legendStyle.GetPaddingBottom()) + paddingTop))
	legend.Box().Style = legendStyle

	fieldsetStyle := box.Style.Copy()
	fieldsetStyle.SetMarginTop(pr.FToPx(px(fieldsetStyle.GetMarginTop()) + max(legendHeight/2-borderTop/2, 0)))
	box.Style = fieldsetStyle
}

// markLegendGap zapisuje w ułożonej ramce odcinek górnego obramowania zasłonięty przez legendę.
func markLegendGap(fieldset Box) {
	box := fieldset.Box()
	if !strings.EqualFold(box.ElementTag(), "fieldset") {
		return
	}
	if legend := renderedLegend(box); legend != nil {
		box.BorderGapX = legend.Box().BorderBoxX()
		box.BorderGapWidth = legend.Box().BorderWidth()
	}
}

// hasPlacedChild (gopdfgo) podaje, czy wśród ułożonych dzieci jest coś poza zastępnikami pudełek
// pozycjonowanych bezwzględnie.
func hasPlacedChild(children []Box) bool {
	for _, child := range children {
		if _, isPlaceholder := child.(*AbsolutePlaceholder); !isPlaceholder {
			return true
		}
	}
	return false
}

// resumeKey (gopdfgo) identyfikuje stos wznowienia: ten sam obiekt wraca do układu na następnej stronie.
func resumeKey(stack tree.ResumeStack) uintptr {
	if stack == nil {
		return 0
	}
	return reflect.ValueOf(stack).Pointer()
}

// isEmptyLinebox (gopdfgo): wiersz bez treści i bez pudełek wyjętych z przepływu. Wiersz z samym elementem
// pływającym albo pozycjonowanym też ma zerową wysokość, ale wyznacza położenie tego elementu, więc
// marginesy przed nim muszą być już rozliczone.
func isEmptyLinebox(line *bo.LineBox) bool {
	if !isPhantomLinebox(line.Box()) {
		return false
	}
	for _, descendant := range bo.DescendantsPlaceholders(line, true) {
		if !descendant.Box().IsInNormalFlow() {
			return false
		}
		if _, isPlaceholder := descendant.(*AbsolutePlaceholder); isPlaceholder {
			return false
		}
	}
	return true
}
