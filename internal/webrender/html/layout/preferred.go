package layout

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/text"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
)

// Preferred and minimum preferred width, aka. max-content and min-content
// width, aka. the shrink-to-fit algorithm.

// Terms used (max-content width, min-content width) are defined in David
// Baron's unofficial draft (https://dbaron.org/css/intrinsic/).

// Return the shrink-to-fit width of “box“.
// *Warning:* both availableOuterWidth and the return value are
// for width of the *content area*, not margin area.
// https://www.w3.org/TR/CSS21/visudet.html#float-width
func shrinkToFit(context *layoutContext, box Box, availableContentWidth pr.Float) pr.Float {
	// gopdfgo: pudełko o znanej wysokości jest blokiem zawierającym o określonej wysokości, więc
	// procentowe wysokości jego dzieci (np. obrazu z proporcją) są znane już przy liczeniu szerokości
	// dopasowanej do treści (CSS 2.1 § 10.5, CSS Sizing 3 § 5.2.1); w źródle liczyły się jak auto
	// (test WPT intrinsic-size-with-anonymous-block).
	if height, ok := box.Box().Height.(pr.Float); ok && bo.BlockContainerT.IsInstance(box) && !box.Box().IsTableWrapper {
		setChildrenDefiniteHeight(box.Box().Children, height)
	}
	out := min(
		max(minContentWidth(context, box, false), availableContentWidth),
		maxContentWidth(context, box, false),
	)
	return out
}

// Return the min-content width for “box“.
// This is the width by breaking at every line-break opportunity.
// [outer] defaults to true
func minContentWidth(context *layoutContext, box Box, outer bool) pr.Float {
	if width, ok := ratioContentWidth(box); ok {
		return adjust(box, outer, ratioFloor(context, box, width), true, true)
	}
	return contentMinContentWidth(context, box, outer)
}

// ratioFloor (gopdfgo): szerokość wynikająca z proporcji nie schodzi poniżej szerokości min-content
// treści, o ile pudełko nie przewija (minimum automatyczne, CSS Sizing 4 § 5.1 „aspect-ratio-minimum”;
// test WPT aspect-ratio-nonreplaced-max-content-contribution-001). Źródło brało samą proporcję.
func ratioFloor(context *layoutContext, box Box, width pr.Float) pr.Float {
	style := box.Box().Style
	// Uproszczenie: min-width nie jest sprawdzane, bo kopie pomiarowe (flex, intrinsic.go) mają min-width: 0.
	if style.GetOverflow() != "visible" {
		return width
	}
	return max(width, contentMinContentWidth(context, box, false))
}

// contentMinContentWidth to minContentWidth bez szerokości wynikającej z aspect-ratio.
func contentMinContentWidth(context *layoutContext, box Box, outer bool) pr.Float {
	if box.Box().IsTableWrapper {
		return tableAndColumnsPreferredWidths(context, box, outer).tableMinContentWidth
	} else if bo.TableCellT.IsInstance(box) {
		return tableCellMinContentWidth(context, box, outer)
	} else if bo.BlockContainerT.IsInstance(box) || bo.TableColumnT.IsInstance(box) {
		return blockMinContentWidth(context, box, outer)
	} else if bo.TableColumnGroupT.IsInstance(box) {
		return columnGroupContentWidth(box)
	} else if IsLine(box) {
		return inlineMinContentWidth(context, box, outer, nil, false, true)
	} else if rep, isReplaced := box.(bo.ReplacedBoxITF); isReplaced {
		return replacedMinContentWidth(rep.Replaced(), outer)
	} else if bo.FlexContainerT.IsInstance(box) {
		return flexMinContentWidth(context, box, outer)
	} else if bo.GridContainerT.IsInstance(box) {
		return gridContentWidth(context, box, outer, true)
	} else {
		panic(fmt.Sprintf("min-content width for %T not handled yet", box))
	}
}

// ratioContentWidth (gopdfgo): szerokość treści pudełka niezastępowanego z aspect-ratio, szerokością auto
// i wysokością podaną długością wynika z proporcji (CSS Sizing 4 § 5.1); dotyczy pudełek dopasowujących się
// do treści: elementów flex i grid, pływających, pozycjonowanych, inline-block.
func ratioContentWidth(box Box) (pr.Float, bool) {
	// gopdfgo: także kontenery flex i siatki (CSS Sizing 4 § 5.1 dotyczy wszystkich pudełek
	// niezastępowanych; test WPT aspect-ratio-nonreplaced-max-content-contribution-001)
	container := bo.BlockContainerT.IsInstance(box) || bo.FlexContainerT.IsInstance(box) || bo.GridContainerT.IsInstance(box)
	if _, isReplaced := box.(bo.ReplacedBoxITF); isReplaced || !container || box.Box().IsTableWrapper {
		return 0, false
	}
	style := box.Box().Style
	ratio := pr.Float(math.Abs(float64(style.GetAspectRatio())))
	height := style.GetHeight()
	if ratio <= 0 || style.GetWidth().Tag != pr.Auto {
		return 0, false
	}
	if definite, ok := box.Box().DefiniteHeight.(pr.Float); ok {
		return definite * ratio, true
	}
	if height.Tag == pr.Auto && len(box.Box().Children) == 0 {
		// gopdfgo: pusty blok z aspect-ratio i min-height: szerokość wewnętrzna to min-height przeniesione
		// przez proporcje (CSS Sizing 4 § 5.1; dla box-sizing: border-box proporcja dotyczy pudełka
		// obramowania). Testy WPT flex-item-transferred-sizes-padding-*.
		if minHeight := style.GetMinHeight(); minHeight.Tag == 0 && minHeight.Unit == pr.Px && minHeight.Calc == nil && minHeight.Value > 0 {
			width := minHeight.Value * ratio
			if style.GetBoxSizing() == "border-box" {
				for _, d := range [...]pr.TaggedDim{style.GetPaddingLeft(), style.GetPaddingRight(), style.GetBorderLeftWidth(), style.GetBorderRightWidth()} {
					if d.Tag == 0 && d.Unit == pr.Px && d.Calc == nil {
						width -= d.Value
					}
				}
			}
			return max(width, 0), true
		}
	}
	if height.Tag != 0 || height.Unit != pr.Px || height.Calc != nil {
		return 0, false
	}
	return height.Value * ratio, true
}

// Return the max-content width for “box“.
// This is the width by only breaking at forced line breaks.
// outer=true
func maxContentWidth(context *layoutContext, box Box, outer bool) pr.Float {
	if width, ok := ratioContentWidth(box); ok {
		return adjust(box, outer, ratioFloor(context, box, width), true, true)
	}
	rep, isReplaced := box.(bo.ReplacedBoxITF)
	if box.Box().IsTableWrapper {
		// gopdfgo: tabela o układzie stałym i szerokości procentowej nie ma skończonej szerokości
		// max-content: zajmuje tyle, ile da jej blok zawierający (tak Gecko i Blink; CSS Tables 3 § 3.9.3
		// tej sytuacji nie rozstrzyga, test WPT fixed-table-layout-with-percentage-width-in-flex-item).
		// „Nieskończoność” zastępujemy dużą skończoną liczbą, żeby rachunek flex się nie rozjechał.
		if style := box.Box().GetWrappedTable().Box().Style; style.GetTableLayout() == "fixed" && style.GetWidth().Unit == pr.Perc && style.GetWidth().Tag == 0 {
			return fixedPercentTableMaxContent
		}
		return tableAndColumnsPreferredWidths(context, box, outer).tableMaxContentWidth
	} else if bo.TableCellT.IsInstance(box) {
		return tableCellMaxContentWidth(context, box, outer)
	} else if bo.BlockContainerT.IsInstance(box) || bo.TableColumnT.IsInstance(box) {
		return blockMaxContentWidth(context, box, outer)
	} else if bo.TableColumnGroupT.IsInstance(box) {
		return columnGroupContentWidth(box)
	} else if IsLine(box) {
		return inlineMaxContentWidth(context, box, outer, true)
	} else if isReplaced {
		return replacedMaxContentWidth(rep.Replaced(), outer)
	} else if bo.FlexContainerT.IsInstance(box) {
		return flexMaxContentWidth(context, box, outer)
	} else if bo.GridContainerT.IsInstance(box) {
		return gridContentWidth(context, box, outer, false)
	} else {
		logger.WarningLogger.Printf("max-content width for %T not handled yet", box)
		return 0
	}
}

// fixedPercentTableMaxContent (gopdfgo): umowna szerokość max-content tabeli o układzie stałym
// i szerokości procentowej (zob. maxContentWidth).
const fixedPercentTableMaxContent pr.Float = 1e5

type fnBlock = func(*layoutContext, Box, bool) pr.Float

// Helper to create “block_*_ContentWidth.“
func blockContentWidth(context *layoutContext, box Box, function fnBlock, outer, sumFloats bool) pr.Float {
	width := box.Box().Style.GetWidth()
	var widthValue pr.Float
	if width.Tag == pr.Auto || width.Unit == pr.Perc {
		// "percentages on the following properties are treated instead as
		// though they were the following: width: auto"
		// https://dbaron.org/css/intrinsic/#outer-intrinsic
		var max, floats pr.Float
		children := box.Box().Children
		multicol := bo.BlockContainerT.IsInstance(box) && !box.Box().IsTableWrapper && bo.IsMulticol(box.Box().Style)
		var spannersWidth pr.Float
		if multicol {
			// elementy rozpinające (także zagnieżdżone, zob. hoistSpanners) mają szerokość kontenera,
			// nie kolumny, więc nie mnożymy ich przez liczbę kolumn
			hoisted, _ := hoistSpanners(children)
			children = nil
			for _, child := range hoisted {
				if bo.IsColumnSpanner(child) {
					spannersWidth = pr.Maxs(spannersWidth, function(context, child, true))
				} else {
					children = append(children, child)
				}
			}
		}
		for _, child_ := range children {
			child := child_.Box()
			if child.IsAbsolutelyPositioned() {
				continue
			}
			v := function(context, child_, true)
			if !sumFloats {
				if v > max {
					max = v
				}
				continue
			}
			// gopdfgo: w szerokości max-content kolejne elementy pływające stoją obok siebie, więc ich
			// szerokości się sumują (CSS Sizing 3 § 5.1: szerokość bez żadnego łamania); źródło brało
			// największy z nich, przez co kontener z dwoma elementami pływającymi dostawał szerokość
			// jednego i drugi spadał niżej. `clear` zaczyna nowy rząd, a blok w przepływie go kończy;
			// blok, który omija elementy pływające (własny kontekst formatowania), staje obok nich.
			if child.Style.GetClear() != "none" {
				if floats > max {
					max = floats
				}
				floats = 0
			}
			if child.IsFloated() {
				floats += v
				continue
			}
			if bo.EstablishesFormattingContext(child_) || bo.ReplacedT.IsInstance(child_) || child.IsTableWrapper ||
				bo.FlexContainerT.IsInstance(child_) || bo.GridContainerT.IsInstance(child_) {
				v += floats
			} else if len(child.Children) != 0 && bo.LineT.IsInstance(child.Children[0]) {
				// gopdfgo: wiersze bloku z treścią liniową opływają wcześniejsze elementy pływające, więc
				// przy szerokości max-content stoją obok nich (test WPT intrinsic-size-float-and-line);
				// w źródle szerokości się nie sumowały i wiersz spadał pod element pływający.
				v += floats
			}
			if floats > max {
				max = floats
			}
			if v > max {
				max = v
			}
			floats = 0
		}
		if floats > max {
			max = floats
		}
		widthValue = max
		if multicol {
			widthValue = pr.Maxs(multicolContentWidth(box.Box().Style, widthValue), spannersWidth)
		}
	} else {
		if width.Unit != pr.Px {
			panic(fmt.Sprintf("expected Px got %d", width.Unit))
		}
		widthValue = width.Value
		// gopdfgo: przy box-sizing: border-box podana szerokość obejmuje obramowanie i odstęp wewnętrzny
		// (przy padding-box sam odstęp), a adjust dodaje je jeszcze raz; szerokość treści to więc width
		// minus te pola (CSS Box Sizing 3 § 3, CSS Sizing 3 § 5.2: udział to rozmiar pudełka marginesu).
		// Źródło brało width wprost, więc np. inline-block albo kontener flex z dzieckiem o stałej
		// szerokości i box-sizing: border-box wychodził szerszy o obramowanie i odstęp dziecka (test WPT
		// css-page/margin-boxes/auto-margins-002, wzorzec).
		widthValue = max(widthValue-boxSizingFrame(box, true), 0)
	}

	return adjust(box, outer, widthValue, true, true)
}

// multicolContentWidth zamienia szerokość wewnętrzną treści jednej kolumny na szerokość kontenera
// kolumn: kolumna ma co najmniej column-width, a kontener mieści column-count kolumn z odstępami
// (przy column-count: auto jedną).
//
// gopdfgo: źródło liczyło szerokość wewnętrzną kontenera kolumn jak zwykłego bloku, czyli jednej
// kolumny, więc komórka tabeli albo element pływający z kolumnami o szerokości auto dostawał szerokość
// jednej kolumny dzieloną na wszystkie. CSS Multicol 1 nie definiuje tych rozmiarów wprost; robimy
// to samo co Blink i Gecko. Odstęp procentowy liczymy jako 0 (CSS Sizing 3, procent cykliczny
// w rozmiarze wewnętrznym).
func multicolContentWidth(style pr.ElementStyle, columnWidth pr.Float) pr.Float {
	if width := style.GetColumnWidth(); width.Tag != pr.Auto {
		columnWidth = max(columnWidth, width.Value)
	}
	count := pr.Float(1)
	if c := style.GetColumnCount(); c.String != "auto" && c.Int > 1 {
		count = pr.Float(c.Int)
	}
	var gap pr.Float
	if gapV := style.GetColumnGap(); gapV.Tag == pr.Normal {
		gap = style.GetFontSize().Value
	} else if gapV.Unit != pr.Perc {
		gap = gapV.Value
	}
	return count*columnWidth + (count-1)*gap
}

// boxSizingFrame (gopdfgo) podaje sumę odstępów wewnętrznych i obramowań w osi poziomej (horizontal)
// albo pionowej, którą przy box-sizing: border-box (przy padding-box same odstępy) obejmują width,
// min-width i max-width (w pionie height, min-height i max-height); rozmiar treści to podana wartość
// minus ta suma (CSS Box Sizing 3 § 3). Procentowe odstępy liczą się w rozmiarach wewnętrznych jako
// zero (CSS Sizing 3 § 5.2.1). Tabela i komórka w modelu zwiniętym mają za obramowanie użyte połówki
// obramowań (CSS 2.1 § 17.6.2), a tabela w tym modelu nie ma odstępów wewnętrznych.
func boxSizingFrame(box_ Box, horizontal bool) pr.Float {
	box := box_.Box()
	style := box.Style
	boxSizing := style.GetBoxSizing()
	if boxSizing != "border-box" && boxSizing != "padding-box" {
		return 0
	}
	pixels := func(d pr.TaggedDim) pr.Float {
		if d.Tag == 0 && d.Unit == pr.Px && d.Calc == nil {
			return d.Value
		}
		return 0
	}
	collapse := style.GetBorderCollapse() == "collapse" && (bo.TableT.IsInstance(box_) || bo.TableCellT.IsInstance(box_))
	var padding, border pr.Float
	if horizontal {
		padding = pixels(style.GetPaddingLeft()) + pixels(style.GetPaddingRight())
		border = style.GetBorderLeftWidth().Value + style.GetBorderRightWidth().Value
		if collapse && (box.BorderLeftWidth != 0 || box.BorderRightWidth != 0) {
			border = box.BorderLeftWidth + box.BorderRightWidth
		}
	} else {
		padding = pixels(style.GetPaddingTop()) + pixels(style.GetPaddingBottom())
		border = style.GetBorderTopWidth().Value + style.GetBorderBottomWidth().Value
		if collapse && (box.BorderTopWidth != 0 || box.BorderBottomWidth != 0) {
			border = box.BorderTopWidth + box.BorderBottomWidth
		}
	}
	if collapse && bo.TableT.IsInstance(box_) {
		padding = 0
	}
	if boxSizing == "padding-box" {
		return padding
	}
	return padding + border
}

// Get box width from given width and box min- and max-widths.
func minMax(box Box, width pr.Float) pr.Float {
	minWidth := box.Box().Style.GetMinWidth()
	maxWidth := box.Box().Style.GetMaxWidth()
	// gopdfgo: przy box-sizing: border-box (padding-box) min-width i max-width opisują pudełko
	// obramowania (odstępu), a width tu jest szerokością treści; ograniczenia porównujemy więc po odjęciu
	// obramowania i odstępu (CSS Box Sizing 3 § 3, CSS Sizing 3 § 5.2). W źródle porównanie szło wprost,
	// więc pudełko dopasowujące się do treści (inline-block, element pływający, absolute, ścieżka siatki,
	// komórka tabeli) z dzieckiem o min-width i box-sizing: border-box wychodziło szersze o obramowanie
	// i odstęp dziecka, a z max-width nie przycinało się do niego. To samo dla min-height i max-height
	// przeniesionych przez proporcje elementu zastępowanego.
	frame := boxSizingFrame(box, true)
	var resMin, resMax pr.Float
	if minWidth.Tag == pr.Auto || minWidth.Unit == pr.Perc {
		resMin = 0
	} else {
		resMin = max(minWidth.Value-frame, 0)
	}
	if maxWidth.Tag == pr.Auto || maxWidth.Unit == pr.Perc {
		resMax = pr.Inf
	} else {
		resMax = max(maxWidth.Value-frame, 0)
	}

	if replaced, ok := box.(bo.ReplacedBoxITF); ok {
		_, _, ratio := replaced.Replaced().Replacement.GetIntrinsicSize(1, box.Box().Style.GetFontSize().Value)
		if ratio != nil {
			frameV := boxSizingFrame(box, false)
			minHeight := box.Box().Style.GetMinHeight()
			if minHeight.Tag != pr.Auto && minHeight.Unit != pr.Perc {
				resMin = max(resMin, max(minHeight.Value-frameV, 0)*ratio.V())
			}
			maxHeight := box.Box().Style.GetMaxHeight()
			if maxHeight.Tag != pr.Auto && maxHeight.Unit != pr.Perc {
				resMax = min(resMax, max(maxHeight.Value-frameV, 0)*ratio.V())
			}
		}
	}

	return max(resMin, min(width, resMax))
}

// marginWidthWithoutPercentages (gopdfgo): jak marginWidth, ale procentowe marginesy i odstępy
// wewnętrzne liczą się jako zero.
func marginWidthWithoutPercentages(box *bo.BoxFields, width pr.Float, left, right bool) pr.Float {
	return marginWidthPercentages(box, width, left, right, false)
}

// Add box paddings, borders and margins to “width“.
// left=true, right=true
func marginWidth(box *bo.BoxFields, width pr.Float, left, right bool) pr.Float {
	return marginWidthPercentages(box, width, left, right, true)
}

func marginWidthPercentages(box *bo.BoxFields, width pr.Float, left, right, usePercentages bool) pr.Float {
	// See https://drafts.csswg.org/css-tables-3/#cell-intrinsic-offsets
	// It is a set of computed values for border-left-width, padding-left,
	// padding-right, and border-right-width (along with zero values for
	// margin-left and margin-right)

	var percentages pr.Float
	var cases []pr.KnownProp
	if left {
		cases = append(cases, pr.PMarginLeft, pr.PPaddingLeft)
	}
	if right {
		cases = append(cases, pr.PMarginRight, pr.PPaddingRight)
	}
	for _, value := range cases {
		styleValue := box.Style.Get(value.Key()).(pr.TaggedDim)
		if styleValue.Tag != pr.Auto {
			switch styleValue.Unit {
			case pr.Px:
				width += styleValue.Value
			case pr.Perc:
				percentages += styleValue.Value
			default:
				panic(fmt.Sprintf("expected Px or Percentage, got %d", styleValue.Unit))
			}
		}
	}

	collapse := box.Style.GetBorderCollapse() == "collapse"
	if left {
		if collapse && box.BorderLeftWidth != 0 {
			// In collapsed-borders mode: the computed horizontal padding of the
			// cell and, for border values, the used border-width values of the
			// cell (half the winning border-width)
			width += box.BorderLeftWidth
		} else {
			// In separated-borders mode: the computed horizontal padding and
			// border of the table-cell
			width += box.Style.GetBorderLeftWidth().Value
		}
	}
	if right {
		if collapse && box.BorderRightWidth != 0 {
			// [...] the used border-width values of the cell
			width += box.BorderRightWidth
		} else {
			// [...] the computed border of the table-cell
			width += box.Style.GetBorderRightWidth().Value
		}
	}

	if !usePercentages {
		return width
	}
	if percentages < 100 {
		return width / (1 - percentages/100.)
	} else {
		// Pathological case, ignore
		return 0
	}
}

// Respect min/max and adjust [width] depending on “outer“.
// If “outer“ is set to “true“, return margin width, else return content
// width.
// left=true, right=true
func adjust(box Box, outer bool, width pr.Float, left, right bool) pr.Float {
	fixed := minMax(box, width)
	if outer {
		if !bo.TableCellT.IsInstance(box) && !box.Box().IsTableWrapper {
			// gopdfgo: procentowe marginesy i odstępy wewnętrzne liczą się w udziale do rozmiaru wewnętrznego
			// jako zero (CSS Sizing 3 § 5.2.1, tak jak w Chromium). Źródło dzieliło szerokość przez
			// (1 - procent), jak dla komórek tabeli (CSS Tables 3), więc margines -100% zmniejszał udział
			// o połowę (test WPT intrinsic-size-with-negative-margins).
			return marginWidthWithoutPercentages(box.Box(), fixed, left, right)
		}
		return marginWidth(box.Box(), fixed, left, right)
	} else {
		return fixed
	}
}

// Return the min-content width for a “BlockBox“.
// outer=true
func blockMinContentWidth(context *layoutContext, box Box, outer bool) pr.Float {
	// gopdfgo: width: max-content daje tę samą szerokość w obu pomiarach
	if box.Box().Style.GetWidth().Intrinsic == pr.IntrinsicMaxContent {
		return blockContentWidth(context, box, maxContentWidth, outer, true)
	}
	// gopdfgo: min-width: max-content podnosi udział min-content do max-content (CSS Sizing 3 § 5.2:
	// udział to rozmiar ograniczony przez min-width); minMax niżej zna tylko długości (test WPT
	// flex-item-content-is-min-width-max-content)
	if style := box.Box().Style; style.GetMinWidth().Intrinsic == pr.IntrinsicMaxContent && style.GetWidth().Tag == pr.Auto {
		return blockContentWidth(context, box, maxContentWidth, outer, true)
	}
	v := blockContentWidth(context, box, minContentWidth, outer, false)
	return v
}

// Return the max-content width for a “BlockBox“.
// outer=true
func blockMaxContentWidth(context *layoutContext, box Box, outer bool) pr.Float {
	if box.Box().Style.GetWidth().Intrinsic == pr.IntrinsicMinContent {
		return blockContentWidth(context, box, minContentWidth, outer, false)
	}
	return blockContentWidth(context, box, maxContentWidth, outer, true)
}

// Return the min-content width for an “InlineBox“.
//
// The width is calculated from the lines from “skipStack“. If
// “firstLine“ is “true“, only the first line minimum width is
// calculated.
// outer=true, skipStack=None, firstLine=false, isLineStart=false
func inlineMinContentWidth(context *layoutContext, box_ Box, outer bool, skipStack tree.ResumeStack,
	firstLine, isLineStart bool,
) pr.Float {
	widths := inlineLineWidths(context, box_, outer, isLineStart, true, skipStack, firstLine)

	if firstLine {
		widths = widths[0:1]
	}

	return adjust(box_, outer, pr.Maxs(widths...), true, true)
}

// Return the max-content width for an “InlineBox“.
// outer=true, isLineStart=false
func inlineMaxContentWidth(context *layoutContext, box_ Box, outer, isLineStart bool) pr.Float {
	widths := inlineLineWidths(context, box_, outer, isLineStart, false, nil, false)
	// Remove trailing space, as split_first_line keeps trailing spaces when
	// max_width is not set.
	widths[len(widths)-1] -= trailingWhitespaceSize(context, box_, false)
	out := adjust(box_, outer, pr.Maxs(widths...), true, true)
	// gopdfgo: przy ujemnym wcięciu (text-indent) wiersz max-content bywa węższy niż najszerszy
	// wiersz min-content (wcięcie skraca tylko pierwszy wiersz); rozmiar max-content nie jest jednak
	// mniejszy od min-content (CSS Sizing 3 § 2.1: max-content to najmniejszy rozmiar bez
	// przepełnienia, gdy łamanie nie jest wymuszone, więc mieści też każdy wiersz min-content; tak
	// Chromium). W źródle szerokość dopasowana do treści wychodziła zero, a treść wystawała (testy
	// WPT CSS2/text/text-indent-intrinsic-003, -004).
	if bo.LineT.IsInstance(box_) {
		if indent := box_.Box().Style.GetTextIndent(); indent.Unit != pr.Perc && indent.Value < 0 {
			out = max(out, inlineMinContentWidth(context, box_, outer, nil, false, isLineStart))
		}
	}
	return out
}

// Return the *-content width for a “TableColumnGroupBox“.
func columnGroupContentWidth(box Box) pr.Float {
	width := box.Box().Style.GetWidth()
	var width_ pr.Float
	if width.Tag == pr.Auto || width.Unit == pr.Perc {
		width_ = 0
	} else if width.Unit == pr.Px {
		width_ = width.Value
	} else {
		panic(fmt.Sprintf("expected Px got %d", width.Unit))
	}

	return adjust(box, false, width_, true, true)
}

// Return the min-content width for a “TableCellBox“.
func tableCellMinContentWidth(context *layoutContext, box_ Box, outer bool) pr.Float {
	// See https://www.w3.org/TR/css-tables-3/#outer-min-content
	// The outer min-content width of a table-cell is
	// max(min-width, min-content width) adjusted by
	// the cell intrinsic offsets.
	box := box_.Box()
	var maxChildrenWidths pr.Float
	for _, child := range box.Children {
		if !child.Box().IsAbsolutelyPositioned() {
			v := minContentWidth(context, child, true)
			if v > maxChildrenWidths {
				maxChildrenWidths = v
			}
		}
	}
	childrenMinWidth := adjust(box_, outer, maxChildrenWidths, true, true)
	return childrenMinWidth
}

// Return the max-content width for a “TableCellBox“.
func tableCellMaxContentWidth(context *layoutContext, box Box, outer bool) pr.Float {
	return max(tableCellMinContentWidth(context, box, outer), blockMaxContentWidth(context, box, outer))
}

func orNil(v pr.Float) pr.MaybeFloat {
	if v == 0 {
		return nil
	}
	return v
}

func orZero(v pr.MaybeFloat) pr.Float {
	if v == nil {
		return 0
	}
	return v.V()
}

// Yield line width for each line.
// firstLine=false
//
// gopdfgo: wiszące odstępy inne niż spacja (np. U+3000, U+2000–U+200A) na końcu ostatniego wiersza
// tekstu nie są odejmowane od razu, bo za tekstem może być dalsza treść tego samego wiersza (choćby
// w następnym pudełku); wiszą tylko na końcu wiersza (CSS Text 3 § 4.1.3). Odejmujemy je przy
// złamaniu wiersza i na końcu treści.
func inlineLineWidths(context *layoutContext, box_ Box, outer, isLineStart,
	minimum bool, skipStack tree.ResumeStack, firstLine bool,
) []pr.Float {
	out, hang, _ := inlineLineWidthsHang(context, box_, outer, isLineStart, minimum, skipStack, firstLine)
	if last := len(out) - 1; hang != 0 && !(firstLine && last > 0) {
		out[last] = max(0, out[last]-hang)
	}
	return out
}

// inlineLineWidthsHang (gopdfgo) działa jak inlineLineWidths, ale wiszących odstępów na końcu
// ostatniego wiersza nie odejmuje, tylko zwraca ich szerokość. Zwraca też tekst, którego zwijalna
// spacja kończy ostatni wiersz (przy max-content), bo i ona znika, gdy dalej jest wymuszone złamanie.
func inlineLineWidthsHang(context *layoutContext, box_ Box, outer, isLineStart,
	minimum bool, skipStack tree.ResumeStack, firstLine bool,
) (_ []pr.Float, trailingHang pr.Float, trailingSpace *bo.TextBox) {
	var (
		pendingHang             pr.Float    // gopdfgo: wiszące odstępy na końcu bieżącego wiersza
		pendingSpace            *bo.TextBox // gopdfgo: tekst ze zwijalną spacją na końcu bieżącego wiersza
		textIndent, currentLine pr.Float
		lineEnded               bool     // gopdfgo: przy firstLine pierwszy wiersz skończył się wewnątrz
		restIndent              pr.Float // gopdfgo: wcięcie wierszy po pierwszym
		skip                    int
		out                     []pr.Float
		box                     = box_.Box()
	)
	// Set text indent.
	if bo.LineT.IsInstance(box_) && box.Style.GetTextIndent().Unit != pr.Perc {
		// gopdfgo: hanging wcina wiersze po pierwszym, each-line wiersze po wymuszonym złamaniu
		// (CSS Text 3 § 7.1). Tu nie odróżniamy złamań wymuszonych od miękkich: przy szerokości
		// max-content wszystkie są wymuszone, przy min-content przyjmujemy miękkie.
		indent := box.Style.GetTextIndent()
		textIndent = indent.Value
		switch {
		case indent.Hanging:
			if !indent.EachLine || minimum {
				restIndent = textIndent
			}
			textIndent = 0
		case indent.EachLine && !minimum:
			restIndent = textIndent
		}
	}

	// Yield widths for each line.
	currentLine = 0
	if skipStack != nil {
		skip, skipStack = skipStack.Unpack()
	}
	afterAtomic := false // gopdfgo: poprzednie dziecko w przepływie było elementem atomowym
	for _, child := range box.Children[skip:] {
		// Skip absolutely positioned elements.
		if child.Box().IsAbsolutelyPositioned() {
			continue
		}
		textBox, isTextBox := child.(*bo.TextBox)
		// gopdfgo: ciąg elementów atomowych liczy się do odstępu między literami jak jeden znak, więc
		// między nim a następującym tekstem jest odstęp (CSS Text 3 § 7.2; test WPT letter-spacing-204)
		if child.Box().IsInNormalFlow() {
			atomic := !isTextBox && !bo.InlineT.IsInstance(child)
			if afterAtomic && !atomic && !minimum && (!isTextBox || len(textBox.Text) != 0 && textBox.Text[0] != ' ') {
				currentLine += atomicRunSpacing(box.Style)
			}
			if atomic || !isTextBox || len(textBox.Text) != 0 {
				afterAtomic = atomic
			}
		}
		firstLineEnds := false
		// nil is used in "lines" to track line breaks, transformed to 0 when yielded.
		var lines []pr.MaybeFloat
		var childHang pr.Float     // gopdfgo: wiszące odstępy na końcu ostatniego wiersza dziecka
		var childSpace *bo.TextBox // gopdfgo: tekst ze zwijalną spacją na końcu ostatniego wiersza dziecka
		if bo.InlineT.IsInstance(child) {
			// Inline box, call function recursively.
			lines_, nestedHang, nestedSpace := inlineLineWidthsHang(context, child, outer, isLineStart, minimum, skipStack, firstLine)
			childHang, childSpace = nestedHang, nestedSpace
			// gopdfgo: przy pierwszym wierszu złamanie wewnątrz pudełka (np. <br>) kończy wiersz, więc dalsze
			// dzieci się nie liczą; w źródle szerokość następnego słowa doliczała się do pierwszego wiersza,
			// a wiersz z samym <br> obok elementu pływającego spadał pod niego (test WPT
			// float-no-content-beside-001)
			firstLineEnds = firstLine && len(lines_) > 1
			if firstLine {
				lines_ = lines_[0:1]
			}
			lines = make([]pr.MaybeFloat, len(lines_))
			for i, v := range lines_ {
				lines[i] = orNil(v)
			}
			if len(lines) == 1 {
				lines[0] = adjust(child, outer, orZero(lines[0]), true, true)
			} else {
				lines[0] = orNil(adjust(child, outer, orZero(lines[0]), true, false))
				lines[len(lines)-1] = orNil(adjust(child, outer, orZero(lines[len(lines)-1]), false, true))
			}
		} else if isTextBox {
			// Text box, split into lines.
			whiteSpace := textBox.Style.GetWhiteSpace()
			spaceCollapse := whiteSpace == pr.Normal || whiteSpace == pr.Nowrap || whiteSpace == pr.PreLine
			textWrap := whiteSpace == pr.Normal || whiteSpace == pr.PreWrap || whiteSpace == pr.PreLine || whiteSpace == pr.BreakSpaces
			if skipStack == nil {
				skip = 0
			} else {
				skip, skipStack = skipStack.Unpack()
				if skipStack != nil {
					panic(fmt.Sprintf("expected empty SkipStack, got %v", skipStack))
				}
			}
			childText := textBox.Text[skip:]
			if isLineStart && spaceCollapse {
				childText = text.TrimPrefix(childText, ' ')
			}
			var maxWidth pr.MaybeFloat
			if minimum {
				maxWidth = pr.Float(0)
			}
			resumeIndex := 0
			newResumeIndex := 0
			textRunes := childText
			for newResumeIndex != -1 {
				resumeIndex += newResumeIndex
				tmp := text.SplitFirstLine(textRunes[resumeIndex:], textBox.Style,
					context, maxWidth, true, isLineStart)
				newResumeIndex = tmp.ResumeAt
				lineWidth := tmp.Width - tmp.Hang
				childHang = 0
				if newResumeIndex == -1 {
					// gopdfgo: ostatni wiersz tekstu: odstępy wiszą tylko, gdy wiersz tu się kończy
					lineWidth, childHang = tmp.Width, tmp.Hang
				}
				if minimum && whiteSpace == pr.PreWrap && newResumeIndex != 0 {
					// gopdfgo: zachowane spacje na końcu złamanego wiersza wiszą poza nim i nie liczą się
					// do szerokości min-content (CSS Text 3 § 4.1.3); źródło je doliczało. Także na końcu
					// ostatniego wiersza tekstu: przy min-content po spacji wiersz się kończy (miejsce
					// podziału albo koniec bloku, gdzie spacje wiszą warunkowo); testy WPT
					// white-space-intrinsic-size-003, -013.
					end := len(textRunes)
					if newResumeIndex > 0 {
						end = resumeIndex + newResumeIndex
					}
					line := textRunes[resumeIndex:end]
					// wiersz zakończony wymuszonym złamaniem: spacje przed nim wiszą warunkowo
					if n := len(line); n > 0 && line[n-1] == '\n' {
						line = line[:n-1]
					}
					trimmed := len(line)
					// gopdfgo: także odstępy inne niż spacja (np. U+3000), które przy pre-wrap wiszą
					// tak samo (CSS Text 3 § 4.1.3; test WPT text-transform-fullwidth-009)
					for trimmed > 0 && (line[trimmed-1] == ' ' || line[trimmed-1] == '\t' || text.IsOtherSpaceSeparator(line[trimmed-1])) {
						trimmed--
					}
					if trimmed == 0 {
						lineWidth = 0
					} else if trimmed < len(line) {
						lineWidth = text.SplitFirstLine(line[:trimmed], textBox.Style, context, nil, false, isLineStart).Width
					}
				} else if minimum && firstLine && whiteSpace == pr.Pre && newResumeIndex != 0 {
					// gopdfgo: przy white-space: pre spacje na końcu wiersza nie liczą się do jego
					// szerokości, gdy wiersz szuka miejsca obok elementów pływających (jak w Chromium; test
					// WPT pre-float-001). Na końcu tekstu odejmujemy je dopiero, gdy wiersz tu się kończy.
					end := len(textRunes)
					if newResumeIndex > 0 {
						end = resumeIndex + newResumeIndex
					}
					line := textRunes[resumeIndex:end]
					forced := false
					if n := len(line); n > 0 && line[n-1] == '\n' {
						line, forced = line[:n-1], true
					}
					trimmed := len(line)
					for trimmed > 0 && line[trimmed-1] == ' ' {
						trimmed--
					}
					if trimmed < len(line) {
						var contentWidth pr.Float
						if trimmed > 0 {
							contentWidth = text.SplitFirstLine(line[:trimmed], textBox.Style, context, nil, false, isLineStart).Width
						}
						if forced {
							lineWidth = contentWidth
						} else {
							childHang = max(0, lineWidth-contentWidth)
						}
					}
				}
				lines = append(lines, orNil(lineWidth))
				if firstLine {
					break
				}
			}
			if firstLine && newResumeIndex != -1 && newResumeIndex != 0 {
				// We only need the first line, break early.
				currentLine += orZero(lines[0])
				lineEnded = true
				break
			}
			// gopdfgo: zachowany znak nowego wiersza na końcu tekstu (np. treść <br>) też łamie wiersz:
			// dalsza treść zaczyna następny (CSS Text 3 § 4.1.1); w źródle złamanie widać było tylko
			// wtedy, gdy po znaku nowego wiersza był jeszcze tekst w tym samym pudełku.
			if newResumeIndex == -1 && len(childText) != 0 && childText[len(childText)-1] == '\n' &&
				(whiteSpace == pr.Pre || whiteSpace == pr.PreWrap || whiteSpace == pr.PreLine || whiteSpace == pr.BreakSpaces) {
				lines = append(lines, nil)
				firstLineEnds = firstLine
			}
			// TODO: use the real next character instead of 'a' to detect line breaks.
			sample := []rune{'a'}
			if len(childText) != 0 {
				sample = []rune{childText[len(childText)-1], 'a'}
			}
			canBreak := context.Fonts().CanBreakText(sample)
			if minimum && textWrap && len(childText) != 0 && breaksAnywhere(textBox.Style, true) {
				// gopdfgo: przy łamaniu w dowolnym miejscu granica fragmentu tekstu też jest miejscem
				// podziału dla szerokości min-content (CSS Text 3 § 5.5)
				canBreak = pr.True
			}
			if minimum && textWrap && canBreak == pr.True {
				// Add all possible line breaks for minimal width.
				lines = append(lines, nil)
			}
			if !minimum && spaceCollapse && len(childText) != 0 && childText[len(childText)-1] == ' ' {
				childSpace = textBox
			}
			if len(lines) > 1 && childHang != 0 && lines[len(lines)-1] == nil {
				// gopdfgo: po tekście jest złamanie wiersza, więc jego końcowe odstępy wiszą
				if i := len(lines) - 2; lines[i] != nil {
					lines[i] = max(0, lines[i].V()-childHang)
				}
				childHang = 0
			}
		} else {
			// Replaced elements, inline blocks…
			// https://www.w3.org/TR/css3-text/#line-break-details
			// "The line breaking behavior of a replaced element
			//  or other atomic inline is equivalent to that
			//  of the Object Replacement Character (U+FFFC)."
			// https://www.unicode.org/reports/tr14/#DescriptionOfProperties
			// "By default, there is a break opportunity
			//  both before and after any inline object."
			if minimum && currentLine == 0 {
				// gopdfgo: na początku wiersza nie ma czego łamać przed
				// obiektem, więc wcięcie pierwszego wiersza (text-indent)
				// liczy się razem z nim; źródło zostawiało wcięcie w pustym
				// wierszu i szerokość minimalna wychodziła za mała
				lines = []pr.MaybeFloat{minContentWidth(context, child, true), nil}
			} else if minimum {
				lines = []pr.MaybeFloat{nil, minContentWidth(context, child, true), nil}
			} else {
				lines = []pr.MaybeFloat{maxContentWidth(context, child, true)}
			}
		}
		// The first text line goes on the current line
		currentLine += orZero(lines[0])
		if orZero(lines[0]) != 0 {
			pendingHang, pendingSpace = 0, nil
		}
		if firstLineEnds {
			// gopdfgo: wiersz kończy się tu wymuszonym złamaniem, więc odstępy z jego końca wiszą
			currentLine -= pendingHang
			if pendingSpace != nil {
				currentLine -= trailingWhitespaceSize(context, pendingSpace, false)
			}
			pendingHang, pendingSpace = 0, nil
			lineEnded = true
			break
		}
		if len(lines) > 1 {
			// Forced line break(s).
			// gopdfgo: ujemne wcięcie nie daje ujemnej szerokości wiersza
			// gopdfgo: przed wymuszonym złamaniem zwijalna spacja z końca wiersza znika (CSS Text 3
			// § 4.1.2), a wiszące odstępy się nie liczą; test WPT white-space-intrinsic-size-024
			if pendingSpace != nil {
				currentLine -= trailingWhitespaceSize(context, pendingSpace, false)
			}
			out = append(out, max(0, currentLine-pendingHang+textIndent))
			pendingHang, pendingSpace = 0, nil
			textIndent = restIndent
			if len(lines) > 2 {
				for _, v := range lines[1 : len(lines)-1] {
					out = append(out, max(0, orZero(v)+textIndent))
				}
			}
			currentLine = orZero(lines[len(lines)-1])
		}
		if childHang != 0 {
			pendingHang = childHang
		}
		if childSpace != nil {
			pendingSpace = childSpace
		}
		isLineStart = lines[len(lines)-1] == nil
		skipStack = nil
	}
	out = append(out, max(0, currentLine+textIndent))
	if lineEnded {
		// gopdfgo: znak dla wywołującego, że pierwszy wiersz skończył się w tym pudełku
		out = append(out, 0)
		pendingHang, pendingSpace = 0, nil
	}
	return out, pendingHang, pendingSpace
}

// Return the percentage contribution of a cell, column or column group.
// https://dbaron.org/css/intrinsic/#pct-contrib
func percentageContribution(box bo.BoxFields) pr.Float {
	var minWidth, width pr.Float
	maxWidth := pr.Inf
	miw, maw, w := box.Style.GetMinWidth(), box.Style.GetMaxWidth(), box.Style.GetWidth()
	if miw.Tag != pr.Auto && miw.Unit == pr.Perc && miw.Offset == 0 {
		minWidth = miw.Value
	}
	if maw.Tag != pr.Auto && maw.Unit == pr.Perc && maw.Offset == 0 {
		maxWidth = maw.Value
	}
	// gopdfgo: szerokość mieszana, calc() z procentem i długością, liczy się w tabeli jak auto
	// (CSS Tables 3 § 3.8.3); źródło brało z niej sam procent.
	if w.Tag != pr.Auto && w.Unit == pr.Perc && w.Offset == 0 {
		width = w.Value
	}
	return max(minWidth, min(width, maxWidth))
}

// definiteColumnWidth podaje szerokość kolumny wynikającą z samych długości w width, min-width
// i max-width elementu <col> (procenty i calc() z procentem liczą się jak brak wartości); zero, gdy
// kolumna nie ma takiej szerokości.
func definiteColumnWidth(style pr.ElementStyle) pr.Float {
	length := func(d pr.TaggedDim) (pr.Float, bool) {
		if d.Tag != 0 || d.Unit == pr.Perc || d.Calc != nil {
			return 0, false
		}
		return d.Value, true
	}
	width, _ := length(style.GetWidth())
	if maxWidth, ok := length(style.GetMaxWidth()); ok {
		width = min(width, maxWidth)
	}
	if minWidth, ok := length(style.GetMinWidth()); ok {
		width = max(width, minWidth)
	}
	return width
}

// captionsMinContentWidth (gopdfgo) podaje największą zewnętrzną szerokość minimalną treści podpisów
// tabeli w pudełku opakowującym.
func captionsMinContentWidth(context *layoutContext, wrapper Box) (pr.Float, bool) {
	var width pr.Float
	found := false
	for _, child := range wrapper.Box().Children {
		if bo.TableCaptionT.IsInstance(child) {
			width = max(width, minContentWidth(context, child, true))
			found = true
		}
	}
	return width, found
}

type tableContentWidths struct {
	grid                       [][]Box
	columnMinContentWidths     []pr.Float
	columnMaxContentWidths     []pr.Float
	columnIntrinsicPercentages []pr.Float
	constrainedness            []bool
	tableMinContentWidth       pr.Float
	// gridMinWidth (gopdfgo): szerokość minimalna treści tabeli (GRIDMIN z odstępami), bez jej
	// podanej szerokości; to dolna granica szerokości użytej (CSS Tables 3 § 3.9.1)
	gridMinWidth                 pr.Float
	totalHorizontalBorderSpacing pr.Float
	tableMaxContentWidth         pr.Float
	// layoutMaxContentWidth (gopdfgo): szerokość maksymalna treści używana przy układzie samej tabeli,
	// zawsze z wkładem kolumn procentowych (zob. intrinsicMaxContentWidth niżej)
	layoutMaxContentWidth pr.Float
}

// tableMaxInlineSize (gopdfgo) to „nieskończenie duża liczba” z dbaron.org/css/intrinsic dla wkładu
// kolumn procentowych sumujących się do 100 %; 1e6 jak kTableMaxInlineSize w Chromium. Źródło
// używało math.MaxInt32, który w float32 traci precyzję przy odejmowaniu (kurczenie elementów flex).
const tableMaxInlineSize = 1e6

// Return content widths for the auto layout table and its columns.
// https://dbaron.org/css/intrinsic/
// outer = true
func tableAndColumnsPreferredWidths(context *layoutContext, box_ Box, outer bool) tableContentWidths {
	box := box_.Box()
	table_ := box.GetWrappedTable()
	table := table_.Table()

	if result := context.tables[table]; result != nil {
		return result[outer]
	}

	// Create the grid
	var gridWidth, gridHeight int
	rowNumber := 0
	for _, rowGroup := range table.Children {
		for _, row := range rowGroup.Box().Children {
			for _, cell := range row.Box().Children {
				gridWidth = max(cell.Box().GridX+cell.Box().Colspan, gridWidth)
				gridHeight = max(rowNumber+cell.Box().Rowspan, gridHeight)
			}
			rowNumber += 1
		}
	}
	// gopdfgo: kolumna zadeklarowana elementem <col> o szerokości podanej długością istnieje także wtedy,
	// gdy żaden wiersz nie ma w niej komórki (CSS 2 § 17.2.1: liczba kolumn to większa z liczb kolumn
	// zadeklarowanych i wynikających z komórek; test WPT css-tables/col-definite-size-001). W źródle
	// liczbę kolumn wyznaczały same komórki.
	cellGridWidth := gridWidth
	if gridHeight > 0 {
		columnNumber := 0
		for _, columnGroup := range table.ColumnGroups {
			for _, column := range columnGroup.Children {
				columnNumber++
				if columnNumber > gridWidth && definiteColumnWidth(column.Box().Style) > 0 {
					gridWidth = columnNumber
				}
			}
		}
	}
	// zippedGrid = list(zip(*grid)), which is the transpose of grid
	grid, zippedGrid := make([][]Box, gridHeight), make([][]Box, gridWidth)
	for i := range grid {
		grid[i] = make([]Box, gridWidth)
	}
	for j := range zippedGrid {
		zippedGrid[j] = make([]Box, gridHeight)
	}
	rowNumber = 0
	for _, rowGroup := range table.Children {
		for _, row := range rowGroup.Box().Children {
			for _, cell := range row.Box().Children {
				grid[rowNumber][cell.Box().GridX] = cell
				zippedGrid[cell.Box().GridX][rowNumber] = cell
			}
			rowNumber += 1
		}
	}

	// Define the total horizontal border spacing
	var totalHorizontalBorderSpacing pr.Float
	if table.Style.GetBorderCollapse() == "separate" && gridWidth > 0 {
		var tot pr.Float = 1
		for i, column := range zippedGrid {
			any := i >= cellGridWidth // kolumna istniejąca tylko dzięki <col>
			for _, b := range column {
				if b != nil {
					any = true
					break
				}
			}
			if any {
				tot += 1
			}
		}
		totalHorizontalBorderSpacing = table.Style.GetBorderSpacing()[0].Value * tot
	}

	// gopdfgo: szerokość minimalna treści podpisów (pudełko obramowania tabeli nie jest węższe niż
	// podpis: CSS Tables 3 § 3.9.1, „captions' min-content”, tak jak w Chromium). CSS 2.1 § 17.4 tego
	// nie określa; w źródle podpis nie wpływał na szerokość tabeli i jego treść wystawała poza niego
	// (testy WPT margin-right-applies-to-015, padding-right-applies-to-015).
	captionMinWidth, hasCaption := captionsMinContentWidth(context, box_)
	captionContentWidth := captionMinWidth - marginWidth(&table.BoxFields, 0, true, true)

	if gridWidth == 0 || gridHeight == 0 {
		table.Children = nil
		minWidth := blockMinContentWidth(context, table_, false)
		maxWidth := blockMaxContentWidth(context, table_, false)
		outerMinWidth := adjust(box_, true, blockMinContentWidth(context, table_, true), true, true)
		outerMaxWidth := adjust(box_, true, blockMaxContentWidth(context, table_, true), true, true)
		if hasCaption {
			minWidth, maxWidth = max(minWidth, captionContentWidth), max(maxWidth, captionContentWidth)
			outerMinWidth = max(outerMinWidth, adjust(box_, true, captionMinWidth, true, true))
			outerMaxWidth = max(outerMaxWidth, adjust(box_, true, captionMinWidth, true, true))
		}
		context.tables[table] = map[bool]tableContentWidths{
			false: {
				tableMinContentWidth:         minWidth,
				tableMaxContentWidth:         maxWidth,
				layoutMaxContentWidth:        maxWidth,
				totalHorizontalBorderSpacing: totalHorizontalBorderSpacing,
			},
			true: {
				tableMinContentWidth:         outerMinWidth,
				tableMaxContentWidth:         outerMaxWidth,
				totalHorizontalBorderSpacing: totalHorizontalBorderSpacing,
			},
		}
		return context.tables[table][outer]
	}

	columnGroups := make([]Box, gridWidth)
	columns := make([]Box, gridWidth)
	columnNumber := 0
outerLoop:
	for _, columnGroup := range table.ColumnGroups {
		for _, column := range columnGroup.Children {
			columnGroups[columnNumber] = columnGroup
			columns[columnNumber] = column
			columnNumber += 1
			if columnNumber == gridWidth {
				break outerLoop
			}
		}
	}

	var colspanCells []Box

	// Define the intermediate content widths
	minContentWidths := make([]pr.Float, gridWidth)
	maxContentWidths := make([]pr.Float, gridWidth)
	intrinsicPercentages := make([]pr.Float, gridWidth)

	groupss := [2]*[]Box{&columnGroups, &columns}

	// Intermediate content widths for span 1
	for i := range minContentWidths {
		for _, groups := range groupss {
			if b := (*groups)[i]; b != nil {
				// gopdfgo: marginesy i odstępy wewnętrzne nie dotyczą kolumn ani grup kolumn (CSS 2.1 § 17.3),
				// więc ich wkładem jest sama szerokość; w źródle liczona była szerokość zewnętrzna, a padding
				// kolumny poszerzał tabelę (test WPT padding-right-applies-to-006).
				minContentWidths[i] = max(minContentWidths[i], minContentWidth(context, b, false))
				maxContentWidths[i] = max(maxContentWidths[i], maxContentWidth(context, b, false))
				intrinsicPercentages[i] = max(intrinsicPercentages[i], percentageContribution(*b.Box()))
			}
		}
		for _, cell := range zippedGrid[i] {
			if cell != nil {
				if cell.Box().Colspan == 1 {
					minContentWidths[i] = max(minContentWidths[i], minContentWidth(context, cell, true))
					maxContentWidths[i] = max(maxContentWidths[i], maxContentWidth(context, cell, true))
					intrinsicPercentages[i] = max(intrinsicPercentages[i], percentageContribution(*cell.Box()))
				} else {
					colspanCells = append(colspanCells, cell)
				}
			}
		}
	}

	// Intermediate content widths for span > 1 is wrong in the 4.1 section, as
	// explained in its third issue. Min- and max-content widths are handled by
	// the excess width distribution method, and percentages do not distribute
	// widths to columns that have originating cells.

	// Intermediate intrinsic percentage widths for span > 1
	for span := 1; span < gridWidth; span += 1 {
		var percentageContributions []pr.Float
		for i, percentageContribution_ := range intrinsicPercentages {
			for j := range zippedGrid[i] {
				var indexes []int
				for k := 0; k < i+1; k += 1 {
					if grid[j][k] != nil {
						indexes = append(indexes, k)
					}
				}
				if len(indexes) == 0 {
					continue
				}
				origin := indexes[len(indexes)-1] // = max
				originCell := grid[j][origin]
				ocColspan := originCell.Box().Colspan
				if ocColspan-1 != span {
					continue
				}
				var baselinePercentage pr.Float
				for u := origin; u < origin+ocColspan; u += 1 {
					baselinePercentage += intrinsicPercentages[u]
				}

				// Cell contribution to intrinsic percentage width
				if intrinsicPercentages[i] == 0 {
					diff := max(0, percentageContribution(*originCell.Box())-baselinePercentage)
					var (
						otherColumnsContributions    []pr.Float
						otherColumnsContributionsSum pr.Float
					)
					for s := origin; s < origin+ocColspan; s += 1 {
						if intrinsicPercentages[s] == 0 {
							otherColumnsContributions = append(otherColumnsContributions, maxContentWidths[s])
							otherColumnsContributionsSum += maxContentWidths[s]
						}
					}
					var ratio pr.Float
					if otherColumnsContributionsSum == 0 {
						if len(otherColumnsContributions) > 0 {
							ratio = 1. / pr.Float(len(otherColumnsContributions))
						} else {
							ratio = 1
						}
					} else {
						ratio = maxContentWidths[i] / otherColumnsContributionsSum
					}
					percentageContribution_ = max(percentageContribution_, diff*ratio)
				}
			}
			percentageContributions = append(percentageContributions, percentageContribution_)
		}
		intrinsicPercentages = percentageContributions
	}

	// Define constrainedness
	constrainedness := make([]bool, gridWidth)
	for i := range constrainedness {
		if columnGroups[i] != nil {
			if wid := columnGroups[i].Box().Style.GetWidth(); wid.Tag != pr.Auto && wid.Unit != pr.Perc {
				constrainedness[i] = true
				continue
			}
		}
		if columns[i] != nil {
			if wid := columns[i].Box().Style.GetWidth(); wid.Tag != pr.Auto && wid.Unit != pr.Perc {
				constrainedness[i] = true
				continue
			}
		}
		for _, cell := range zippedGrid[i] {
			if cell != nil {
				if wid := cell.Box().Style.GetWidth(); cell.Box().Colspan == 1 && wid.Tag != pr.Auto && wid.Unit != pr.Perc {
					constrainedness[i] = true
					break
				}
			}
		}
	}
	var cumsum pr.Float
	for i, percentage := range intrinsicPercentages {
		u := min(percentage, 100-cumsum)
		cumsum += percentage
		intrinsicPercentages[i] = u
	}

	// Max- and min-content widths for span > 1
	for _, cell_ := range colspanCells {
		cell := cell_.Box()
		minContent := minContentWidth(context, cell_, true)
		maxContent := maxContentWidth(context, cell_, true)
		columnSlice := [2]int{cell.GridX, cell.GridX + cell.Colspan}
		var spacing pr.Float
		if table.Style.GetBorderCollapse() == "separate" {
			spacing = pr.Float(cell.Colspan-1) * table.Style.GetBorderSpacing()[0].Value
		}

		// gopdfgo: kolumna procentowa bierze z szerokości komórki rozpinającej
		// swój udział procentowy, zanim reszta trafi do pozostałych kolumn.
		// Źródło oddawało całość kolumnom bez procentu (CSS Tables 3 § 3.8.3
		// zostawia tu swobodę), więc kolumna „90%” obok pustej kolumny auto
		// kurczyła się prawie do zera, gdy tabela miała wiersz z colspan;
		// przeglądarki zachowują proporcję.
		var spannedPercentage pr.Float
		for s := columnSlice[0]; s < columnSlice[1]; s += 1 {
			spannedPercentage += intrinsicPercentages[s]
		}
		if spannedPercentage > 0 {
			scale := pr.Float(1)
			if spannedPercentage > 100 {
				scale = 100 / spannedPercentage
			}
			for s := columnSlice[0]; s < columnSlice[1]; s += 1 {
				if share := intrinsicPercentages[s] * scale / 100; share > 0 {
					minContentWidths[s] = max(minContentWidths[s], (minContent-spacing)*share)
					maxContentWidths[s] = max(maxContentWidths[s], (maxContent-spacing)*share)
				}
			}
		}

		var columnsMinContent, columnsMaxContent pr.Float
		for s := columnSlice[0]; s < columnSlice[1]; s += 1 {
			columnsMinContent += minContentWidths[s]
			columnsMaxContent += maxContentWidths[s]
		}

		if minContent > columnsMinContent+spacing {
			excessWidth := minContent - (columnsMinContent + spacing)
			distributeExcessWidth(zippedGrid, excessWidth, minContentWidths,
				constrainedness, intrinsicPercentages, maxContentWidths, columnSlice)
		}

		if maxContent > columnsMaxContent+spacing {
			excessWidth := maxContent - (columnsMaxContent + spacing)
			distributeExcessWidth(zippedGrid, excessWidth, maxContentWidths,
				constrainedness, intrinsicPercentages, maxContentWidths, columnSlice)
		}
	}

	// Calculate the max- and min-content widths of table and columns
	var (
		smallpercentageContributions               []pr.Float
		largepercentageContributionNumerator, sum_ pr.Float
	)
	for i, v := range intrinsicPercentages {
		sum_ += v
		if v != 0 {
			smallpercentageContributions = append(smallpercentageContributions, maxContentWidths[i]/(v/100.))
		} else {
			largepercentageContributionNumerator += maxContentWidths[i]
		}
	}
	largepercentageContributionDenominator := (100. - sum_) / 100.
	var largepercentageContribution pr.Float
	if largepercentageContributionDenominator == 0 {
		if largepercentageContributionNumerator == 0 {
			largepercentageContribution = 0
		} else {
			// "the large percentage contribution of the table [is] an
			// infinitely large number if the numerator is nonzero [and] the
			// denominator of that ratio is 0."
			//
			// https://dbaron.org/css/intrinsic/#autotableintrinsic
			//
			// Please note that "an infinitely large number" is not "infinite",
			// and that"s probably not a coincindence: putting "inf" here breaks
			// some cases (see #305).
			largepercentageContribution = tableMaxInlineSize
		}
	} else {
		largepercentageContribution = largepercentageContributionNumerator / largepercentageContributionDenominator
	}

	var sumMin, sumMax pr.Float
	for i := range minContentWidths {
		sumMin += minContentWidths[i]
		sumMax += maxContentWidths[i]
	}
	tableMinContentWidth := totalHorizontalBorderSpacing + sumMin
	tableMaxContentWidth := totalHorizontalBorderSpacing + max(
		max(sumMax, largepercentageContribution),
		pr.Maxs(smallpercentageContributions...))

	gridMinWidth := tableMinContentWidth
	tableMinWidth := tableMinContentWidth
	tableMaxWidth := tableMaxContentWidth
	fixedWidth := false
	if wid := table.Style.GetWidth(); wid.Tag != pr.Auto && wid.Unit == pr.Px {
		fixedWidth = true
		// "percentages on the following properties are treated instead as
		// though they were the following: width: auto"
		// https://dbaron.org/css/intrinsic/#outer-intrinsic
		tableMinWidth = wid.Value
		tableMaxWidth = wid.Value
		// gopdfgo: przy box-sizing: border-box (domyślnym dla tabel w HTML) podana szerokość obejmuje
		// obramowanie i odstęp wewnętrzny tabeli; w źródle była brana jako szerokość treści, więc tabela
		// wychodziła za szeroka.
		if table.Style.GetBoxSizing() != "content-box" {
			delta := table.Style.GetBorderLeftWidth().Value + table.Style.GetBorderRightWidth().Value
			if table.Style.GetBoxSizing() == "border-box" && table.Style.GetBorderCollapse() == "collapse" {
				// gopdfgo: w modelu zwiniętym obramowaniem tabeli jest połowa obramowań skrajnych komórek
				// (CSS 2.1 § 17.6.2), już wpisana w BorderLeftWidth i BorderRightWidth; źródło przyjmowało
				// tu zero, więc tabela z width: 100px była o tę połowę z każdej strony za szeroka (test WPT
				// table-cell-width-0)
				delta = table.BorderLeftWidth.V() + table.BorderRightWidth.V()
			}
			for _, padding := range [...]pr.TaggedDim{table.Style.GetPaddingLeft(), table.Style.GetPaddingRight()} {
				if padding.Unit == pr.Px && table.Style.GetBorderCollapse() != "collapse" {
					delta += padding.Value
				}
			}
			if table.Style.GetBoxSizing() == "padding-box" {
				delta -= table.Style.GetBorderLeftWidth().Value + table.Style.GetBorderRightWidth().Value
			}
			tableMinWidth = max(tableMinWidth-delta, 0)
			tableMaxWidth = max(tableMaxWidth-delta, 0)
		}
	}

	tableMinContentWidth = max(tableMinContentWidth, adjust(table, false, tableMinWidth, true, true))
	tableMaxContentWidth = max(tableMaxContentWidth, adjust(table, false, tableMaxWidth, true, true))
	if hasCaption {
		tableMinContentWidth = max(tableMinContentWidth, captionContentWidth)
		tableMaxContentWidth = max(tableMaxContentWidth, captionContentWidth)
	}
	// gopdfgo: max-width tabeli ogranicza także jej szerokość preferowaną (nie
	// poniżej szerokości minimalnej treści), żeby kontener kurczący się do
	// treści miał tę samą szerokość co ułożona tabela. Przy box-sizing: border-box (domyślnym dla tabel
	// w HTML) max-width obejmuje obramowanie i odstęp wewnętrzny tabeli, a porównujemy go z szerokością
	// treści, więc najpierw je odejmujemy (CSS Box Sizing 3 § 3, jak adjustBoxSizing w układzie tabeli).
	tableMaxWidthLimit := pr.Inf
	if maxWidth := table.Style.GetMaxWidth(); maxWidth.Tag != pr.Auto && maxWidth.Unit == pr.Px && maxWidth.Value != pr.Inf {
		tableMaxWidthLimit = max(maxWidth.Value-boxSizingFrame(table, true), 0)
		tableMaxContentWidth = max(tableMinContentWidth, min(tableMaxContentWidth, tableMaxWidthLimit))
	}
	// gopdfgo: wkład procentowych kolumn („large/small percentage contribution”) liczymy do szerokości
	// preferowanej tabeli tylko wtedy, gdy żaden przodek nie jest komórką tabeli, kontenerem flex ani
	// siatki i tabela nie ma width: max-content; przy samym układzie tabeli wkład liczy się zawsze (jak
	// TableNode::AllowColumnPercentages w Chromium; CSS Tables 3 § 3.9.1 tego nie rozstrzyga). Źródło
	// liczyło wkład zawsze, więc tabela z komórką width: 100% obok innej komórki rozpychała element flex
	// i ścieżkę siatki do „nieskończenie dużej” szerokości (testy WPT
	// item-with-table-with-infinite-max-intrinsic-width, table-as-item-percent-width-cell-001).
	intrinsicMaxContentWidth := tableMaxContentWidth
	if noPercentages := totalHorizontalBorderSpacing + sumMax; (table.NoColumnPercentages ||
		table.Style.GetWidth().Intrinsic == pr.IntrinsicMaxContent) && noPercentages < tableMaxContentWidth {
		limit := noPercentages
		if fixedWidth {
			limit = tableMaxWidth
		}
		intrinsicMaxContentWidth = max(noPercentages, adjust(table, false, limit, true, true))
		if hasCaption {
			intrinsicMaxContentWidth = max(intrinsicMaxContentWidth, captionContentWidth)
		}
		intrinsicMaxContentWidth = max(tableMinContentWidth, min(intrinsicMaxContentWidth, tableMaxWidthLimit))
	}
	tableOuterMinContentWidth := marginWidth(&table.BoxFields, marginWidth(box, tableMinContentWidth, true, true), true, true)
	tableOuterMaxContentWidth := marginWidth(&table.BoxFields, marginWidth(box, intrinsicMaxContentWidth, true, true), true, true)

	result := tableContentWidths{
		columnMinContentWidths:       minContentWidths,
		columnMaxContentWidths:       maxContentWidths,
		columnIntrinsicPercentages:   intrinsicPercentages,
		constrainedness:              constrainedness,
		totalHorizontalBorderSpacing: totalHorizontalBorderSpacing,
		grid:                         zippedGrid,
		gridMinWidth:                 gridMinWidth,
	}
	resultFalse := result
	resultFalse.tableMinContentWidth = tableMinContentWidth
	resultFalse.tableMaxContentWidth = intrinsicMaxContentWidth
	resultFalse.layoutMaxContentWidth = tableMaxContentWidth
	resultTrue := result
	resultTrue.tableMinContentWidth = tableOuterMinContentWidth
	resultTrue.tableMaxContentWidth = tableOuterMaxContentWidth

	context.tables[table] = map[bool]tableContentWidths{
		false: resultFalse,
		true:  resultTrue,
	}
	return context.tables[table][outer]
}

// Return the min-content width for an “InlineReplacedBox“.
// outer=true
func replacedMinContentWidth(box *bo.ReplacedBox, outer bool) pr.Float {
	width := box.Style.GetWidth()
	var (
		h pr.MaybeFloat
		w pr.Float
	)
	if width.Tag == pr.Auto {
		height := box.Style.GetHeight()
		if height.Tag == pr.Auto || height.Unit == pr.Perc {
			h = pr.AutoF
			if definite, ok := box.DefiniteHeight.(pr.Float); ok {
				h = definite // gopdfgo: wysokość znana z wiersza siatki o stałym rozmiarze
			}
		} else if height.Unit == pr.Px {
			// gopdfgo: przy box-sizing: border-box/padding-box podana długość obejmuje obramowanie i odstęp, a adjust
			// dodaje je jeszcze raz (CSS Box Sizing 3 § 3); źródło brało ją za rozmiar treści
			h = max(height.Value-boxSizingFrame(box, false), 0)
		} else {
			panic(fmt.Sprintf("expected Px got %d", height.Unit))
		}
		if mw := box.Style.GetMaxWidth(); mw.Tag != pr.Auto && mw.Unit == pr.Perc {
			// See https://drafts.csswg.org/css-sizing/#intrinsic-contribution
			w = pr.Float(0)
		} else {
			image := box.Replacement
			iwidth, iheight, iratio := image.GetIntrinsicSize(box.Style.GetImageResolution().Value, box.Style.GetFontSize().Value)
			iwidth, iheight, iratio = applyPreferredRatio(box.Style, iwidth, iheight, iratio) // gopdfgo: aspect-ratio
			if pr.Is(iratio) && !pr.Is(iwidth) && !pr.Is(iheight) {
				w = 0
			} else {
				w, _ = DefaultImageSizing(iwidth, iheight, iratio, pr.AutoF, h, 300, 150)
				w = clampByHeightLimits(box, w, iratio)
			}
		}
	} else if width.Unit == pr.Perc {
		// See https://drafts.csswg.org/css-sizing/#intrinsic-contribution
		w = 0
	} else if width.Unit == pr.Px {
		// gopdfgo: przy box-sizing: border-box/padding-box podana długość obejmuje obramowanie i odstęp, a adjust
		// dodaje je jeszcze raz (CSS Box Sizing 3 § 3); źródło brało ją za rozmiar treści
		w = max(width.Value-boxSizingFrame(box, true), 0)
	} else {
		panic(fmt.Sprintf("expected Px got %d", width.Unit))
	}
	return adjust(box, outer, w, true, true)
}

// clampByHeightLimits (gopdfgo): szerokość z treści elementu zastępowanego z proporcjami i szerokością
// auto uwzględnia min-height i max-height przeniesione przez proporcje (CSS 2.1 § 10.4, tabela ograniczeń;
// CSS Sizing 4 § 5.2). Procenty liczą się od HeightBase, gdy jest znana; inaczej nie ograniczają. Źródło
// pomijało oba ograniczenia (testy WPT image-nested-within-definite-column-flexbox, percentage-max-height-003).
func clampByHeightLimits(box *bo.ReplacedBox, w pr.Float, ratio pr.MaybeFloat) pr.Float {
	if !pr.Is(ratio) || ratio.V() <= 0 {
		return w
	}
	resolve := func(d pr.TaggedDim) (pr.Float, bool) {
		if d.Tag != 0 || d.Calc != nil {
			return 0, false
		}
		switch d.Unit {
		case pr.Px:
			return d.Value, true
		case pr.Perc:
			if base, ok := box.HeightBase.(pr.Float); ok {
				return base * d.Value / 100, true
			}
		}
		return 0, false
	}
	frame := box.Style.GetBoxSizing() == "border-box"
	limit := func(d pr.TaggedDim) (pr.Float, bool) {
		v, ok := resolve(d)
		if ok && frame {
			for _, p := range [...]pr.TaggedDim{box.Style.GetPaddingTop(), box.Style.GetPaddingBottom(), box.Style.GetBorderTopWidth(), box.Style.GetBorderBottomWidth()} {
				if p.Tag == 0 && p.Unit == pr.Px && p.Calc == nil {
					v -= p.Value
				}
			}
			v = max(v, 0)
		}
		return v, ok
	}
	h := w / ratio.V()
	if maxHeight, ok := limit(box.Style.GetMaxHeight()); ok && h > maxHeight {
		h = maxHeight
	}
	if minHeight, ok := limit(box.Style.GetMinHeight()); ok && h < minHeight {
		h = minHeight
	}
	return h * ratio.V()
}

// Return the max-content width for an “InlineReplacedBox“.
func replacedMaxContentWidth(box *bo.ReplacedBox, outer bool) pr.Float {
	width := box.Style.GetWidth()
	var (
		h pr.MaybeFloat
		w pr.Float
	)
	// gopdfgo: szerokość procentowa liczy się w szerokości max-content jak auto (CSS Sizing 3 § 5.2.1:
	// zerowany jest tylko udział min-content); źródło zerowało oba, więc obraz o szerokości 100% w elemencie
	// dopasowującym się do treści (element flex, float, inline-block) miał szerokość zero.
	if width.Tag == pr.Auto || width.Unit == pr.Perc {
		height := box.Style.GetHeight()
		if height.Tag == pr.Auto || height.Unit == pr.Perc {
			h = pr.AutoF
			if definite, ok := box.DefiniteHeight.(pr.Float); ok {
				h = definite // gopdfgo: wysokość znana z wiersza siatki o stałym rozmiarze
			}
		} else if height.Unit == pr.Px {
			// gopdfgo: przy box-sizing: border-box/padding-box podana długość obejmuje obramowanie i odstęp, a adjust
			// dodaje je jeszcze raz (CSS Box Sizing 3 § 3); źródło brało ją za rozmiar treści
			h = max(height.Value-boxSizingFrame(box, false), 0)
		} else {
			panic(fmt.Sprintf("expected Px got %d", height.Unit))
		}

		image := box.Replacement
		iwidth, iheight, ratio := image.GetIntrinsicSize(box.Style.GetImageResolution().Value, box.Style.GetFontSize().Value)
		iwidth, iheight, ratio = applyPreferredRatio(box.Style, iwidth, iheight, ratio) // gopdfgo: aspect-ratio
		w, _ = DefaultImageSizing(iwidth, iheight, ratio, pr.AutoF, h, 300, 150)
		w = clampByHeightLimits(box, w, ratio)

	} else if width.Unit == pr.Perc {
		// See https://drafts.csswg.org/css-sizing/#intrinsic-contribution
		w = 0
	} else if width.Unit == pr.Px {
		// gopdfgo: przy box-sizing: border-box/padding-box podana długość obejmuje obramowanie i odstęp, a adjust
		// dodaje je jeszcze raz (CSS Box Sizing 3 § 3); źródło brało ją za rozmiar treści
		w = max(width.Value-boxSizingFrame(box, true), 0)
	} else {
		panic(fmt.Sprintf("expected Px got %d", width.Unit))
	}
	return adjust(box, outer, w, true, true)
}

// Return the min-content width for an “FlexContainerBox“.
// outer=true
func flexMinContentWidth(context *layoutContext, box Box, outer bool) pr.Float {
	// TODO: use real values, see
	// https://www.w3.org/TR/css-flexbox-1/#intrinsic-sizes
	// gopdfgo: kontener o szerokości podanej długością ma tę szerokość także jako wewnętrzną
	// (CSS Sizing 3 § 5.1), tak jak blok i siatka; źródło liczyło ją zawsze z elementów.
	if width := box.Box().Style.GetWidth(); width.Tag != pr.Auto && width.Unit == pr.Px && width.Calc == nil {
		// gopdfgo: przy box-sizing: border-box/padding-box podana długość obejmuje obramowanie i odstęp, a adjust
		// dodaje je jeszcze raz (CSS Box Sizing 3 § 3); źródło brało ją za rozmiar treści
		return adjust(box, outer, max(width.Value-boxSizingFrame(box, true), 0), true, true)
	}
	primeFlexDefiniteHeights(box)
	var sum, max pr.Float
	for _, child := range box.Box().Children {
		if collapsedInRow(box, child) {
			continue
		}
		if child.Box().IsFlexItem {
			v := minContentWidth(context, child, true)
			if content, ok := flexibleItemContentWidth(context, box, child); ok && content > v {
				v = content
			}
			if ratioWidth, ok := stretchedRatioContribution(box, child); ok {
				v = ratioWidth
			}
			v = clampByBasis(context, box, child, v)
			sum += v
			if v > max {
				max = v
			}
		}
	}
	if strings.HasPrefix(string(box.Box().Style.GetFlexDirection()), "row") && box.Box().Style.GetFlexWrap() == "nowrap" {
		return adjust(box, outer, sum+flexMainGaps(box), true, true)
	} else {
		// gopdfgo: szerokość min-content wieloliniowego kontenera w kolumnie to największy wkład min-content
		// elementu, a nie suma kolumn (CSS Flexbox 1 § 9.9.2; suma dotyczy tylko max-content). Wcześniej
		// liczyliśmy tu columnWrapContentWidth, więc kontener nie zwężał się do dostępnego miejsca (test WPT
		// multiline-shrink-to-fit).
		return adjust(box, outer, max, true, true)
	}
}

// flexibleItemContentWidth (gopdfgo): wkład min-content elementu flex w kontenerze wierszowym to większa
// z wartości: rozmiar min-content i podana szerokość (CSS Flexbox 1 § 9.9.1), ograniczona przez max-width.
// Źródło brało szerokość wewnętrzną pudełka z podaną szerokością, czyli samą podaną szerokość, więc
// element węższy niż jego treść zwężał kontener. Zwraca szerokość zewnętrzną treści; false, gdy
// element nie ma podanej szerokości albo nie może ani rosnąć, ani się kurczyć.
func flexibleItemContentWidth(context *layoutContext, container, child Box) (pr.Float, bool) {
	return flexibleItemContribution(context, container, child, minContentWidth)
}

// flexibleItemContribution: jak flexibleItemContentWidth, z rozmiarem treści liczonym przez measure
// (min-content albo max-content). gopdfgo: dla max-content też obowiązuje większa z wartości: rozmiar
// max-content treści i podana szerokość (CSS Flexbox 1 § 9.9.1); źródło brało samą podaną szerokość.
func flexibleItemContribution(context *layoutContext, container, child Box, measure func(*layoutContext, Box, bool) pr.Float) (pr.Float, bool) {
	style := child.Box().Style
	if !strings.HasPrefix(string(container.Box().Style.GetFlexDirection()), "row") || style.GetWidth().Tag == pr.Auto ||
		(style.GetFlexGrow() == 0 && style.GetFlexShrink() == 0) {
		return 0, false
	}
	if _, isReplaced := child.(bo.ReplacedBoxITF); isReplaced {
		return 0, false
	}
	probe := child.Copy()
	if bo.ParentT.IsInstance(child) {
		probe = bo.CopyWithChildren(child, child.Box().Children)
	}
	probe.Box().Style = style.Copy()
	probe.Box().Style.SetWidth(pr.TagToV(pr.Auto))
	content := measure(context, probe, true)
	if maxWidth := style.GetMaxWidth(); maxWidth.Unit == pr.Px && maxWidth.Calc == nil && maxWidth.Tag == 0 {
		content = min(content, adjust(probe, true, maxWidth.Value, true, true))
	}
	return content, true
}

// inflexibleBasis (gopdfgo): zewnętrzna szerokość z flex-basis podanego długością, którą clampByBasis
// ogranicza wkład elementu do szerokości wewnętrznej kontenera wierszowego (CSS Flexbox 1 § 9.9.1).
// Tylko w kontenerze wierszowym (w kolumnie flex-basis jest wysokością); automatyczne minimum
// elementu (§ 4.5) nie pozwala zejść poniżej szerokości min-content jego treści.
func inflexibleBasis(context *layoutContext, container, child Box, floor bool) (pr.Float, bool) {
	style := child.Box().Style
	basis := style.GetFlexBasis()
	if basis.Tag == pr.Auto && style.GetWidth().Tag == 0 && style.GetWidth().Unit == pr.Px && style.GetWidth().Calc == nil {
		// gopdfgo: flex-basis: auto bierze rozmiar główny elementu (CSS Flexbox 1 § 7.2.3)
		basis = style.GetWidth()
	}
	// element, który nie może się kurczyć, nie zejdzie poniżej flex-basis; gdy może rosnąć, szerokość
	// treści nadal może dać więcej
	if !strings.HasPrefix(string(container.Box().Style.GetFlexDirection()), "row") ||
		basis.Tag != 0 || basis.Unit != pr.Px || basis.Calc != nil {
		return 0, false
	}
	pixels := func(d pr.TaggedDim) pr.Float {
		if d.Tag != 0 || d.Unit != pr.Px || d.Calc != nil {
			return 0
		}
		return d.Value
	}
	var width pr.Float
	if style.GetBoxSizing() == "border-box" {
		// flex-basis, min-width i max-width opisują pudełko obramowania (§ 7.2.1)
		frame := pixels(style.GetPaddingLeft()) + pixels(style.GetPaddingRight()) +
			pixels(style.GetBorderLeftWidth()) + pixels(style.GetBorderRightWidth())
		border := max(basis.Value, frame)
		if maxWidth := style.GetMaxWidth(); maxWidth.Tag == 0 && maxWidth.Unit == pr.Px && maxWidth.Value != pr.Inf {
			border = min(border, max(maxWidth.Value, frame))
		}
		if minWidth := style.GetMinWidth(); minWidth.Tag == 0 && minWidth.Unit == pr.Px {
			border = max(border, minWidth.Value)
		}
		width = border + pixels(style.GetMarginLeft()) + pixels(style.GetMarginRight())
	} else {
		width = adjust(child, true, basis.Value, true, true)
	}
	if minWidth := style.GetMinWidth(); floor && minWidth.Tag == pr.Auto && minWidth.Intrinsic == pr.IntrinsicNone && style.GetOverflow() == "visible" {
		width = max(width, minContentWidth(context, child, true))
	}
	return width, true
}

// primeFlexDefiniteHeights (gopdfgo): kontener flex o znanej wysokości treści jest podstawą procentowych
// wysokości swoich elementów, a w jednowierszowym kontenerze wierszowym element rozciągany ma wysokość
// określoną (CSS Flexbox 1 § 9.8 p. 1); zapisujemy to przed liczeniem szerokości z treści, jak shrinkToFit
// dla bloku. Źródło liczyło szerokości elementów tak, jakby wysokość kontenera była nieznana.
func primeFlexDefiniteHeights(box Box) {
	fields := box.Box()
	height, ok := fields.Height.(pr.Float)
	if !ok {
		height, ok = fields.DefiniteHeight.(pr.Float)
	}
	if !ok || fields.IndefiniteHeight {
		return
	}
	row := strings.HasPrefix(string(fields.Style.GetFlexDirection()), "row")
	for _, child := range fields.Children {
		if !child.Box().IsFlexItem {
			continue
		}
		stretch := row && fields.Style.GetFlexWrap() == "nowrap" && isStretchedFlexItem(child, fields) &&
			child.Box().Style.GetMarginTop().Tag != pr.Auto && child.Box().Style.GetMarginBottom().Tag != pr.Auto
		setDefiniteHeight(child, height, stretch)
	}
}

// clampByBasis (gopdfgo): wkład elementu flex do szerokości wewnętrznej kontenera wierszowego jest
// ograniczony przez flex-basis podane długością: od góry, gdy element nie może rosnąć, i od dołu, gdy
// nie może się kurczyć (CSS Flexbox 1 § 9.9.1). Wcześniej ograniczano tylko elementy bez kurczenia,
// więc element z flex: 0 1 100px i szeroką treścią poszerzał kontener do szerokości treści.
func clampByBasis(context *layoutContext, container, child Box, v pr.Float) pr.Float {
	style := child.Box().Style
	// element, który może się kurczyć, nie dostaje tu minimum automatycznego: Chromium go nie stosuje,
	// a wzorzec testu WPT flex-container-max-content-001 (przypadki 13-15) oczekuje samego flex-basis
	basis, ok := inflexibleBasis(context, container, child, style.GetFlexShrink() == 0)
	if !ok {
		return v
	}
	if style.GetFlexShrink() == 0 && v < basis {
		v = basis
	}
	if style.GetFlexGrow() == 0 && v > basis {
		v = basis
	}
	return v
}

// collapsedInRow (gopdfgo): element z visibility: collapse w kontenerze wierszowym staje się rozpórką
// o zerowym rozmiarze głównym (CSS Flexbox 1 § 4.4), więc nie wnosi nic do szerokości wewnętrznej.
func collapsedInRow(container, child Box) bool {
	return child.Box().IsFlexItem && child.Box().Style.GetVisibility() == "collapse" && !child.Box().IsTableWrapper &&
		strings.HasPrefix(string(container.Box().Style.GetFlexDirection()), "row")
}

// flexMainGaps (gopdfgo): suma odstępów column-gap między elementami kontenera flex w wierszu; liczy
// się do jego szerokości wewnętrznej (CSS Align 3 § 8). Odstęp w procentach daje tu zero.
func flexMainGaps(box Box) pr.Float {
	gap := box.Box().Style.GetColumnGap()
	if gap.Tag == pr.Normal || gap.Unit != pr.Px || gap.Calc != nil {
		return 0
	}
	items := 0
	for _, child := range box.Box().Children {
		if child.Box().IsFlexItem && !collapsedInRow(box, child) { // gopdfgo: przy rozpórce odstęp znika (CSS Align 3 § 8.1)
			items++
		}
	}
	if items < 2 {
		return 0
	}
	return gap.Value * pr.Float(items-1)
}

// columnWrapContentWidth (gopdfgo): szerokość wewnętrzna kontenera flex w kolumnie z zawijaniem
// i o podanej wysokości to suma szerokości jego linii (kolumn) wraz z odstępami, a nie szerokość
// najszerszego elementu (CSS Flexbox 1 § 9.9.1: rozmiar wyznacza układ przy danym ograniczeniu).
// Elementy dzielimy na linie według ich rozmiaru w osi głównej: flex-basis, potem height, z ograniczeniem
// min-height i max-height. Gdy któryś element nie ma podanej wysokości, wynik jest nieokreślony
// (ok == false) i wołający zostaje przy najszerszym elemencie.
func columnWrapContentWidth(context *layoutContext, box Box, itemWidth func(*layoutContext, Box, bool) pr.Float) (pr.Float, bool) {
	style := box.Box().Style
	if !strings.HasPrefix(string(style.GetFlexDirection()), "column") || style.GetFlexWrap() == "nowrap" {
		return 0, false
	}
	pixels := func(d pr.TaggedDim) (pr.Float, bool) {
		if d.Tag != 0 || d.Unit != pr.Px || d.Calc != nil {
			return 0, false
		}
		return d.Value, true
	}
	limit, ok := pixels(style.GetHeight())
	if !ok {
		// przy wysokości auto linie łamie max-height
		if limit, ok = pixels(style.GetMaxHeight()); !ok {
			return 0, false
		}
	}
	if style.GetBoxSizing() == "border-box" {
		for _, d := range [...]pr.TaggedDim{style.GetPaddingTop(), style.GetPaddingBottom(), style.GetBorderTopWidth(), style.GetBorderBottomWidth()} {
			v, _ := pixels(d)
			limit -= v
		}
	}
	rowGap, _ := pixels(style.GetRowGap())
	columnGap, _ := pixels(style.GetColumnGap())

	var total, lineWidth, lineHeight pr.Float
	lines, inLine := 0, 0
	// kolejność elementów według order, stabilnie
	var items []Box
	for _, child := range box.Box().Children {
		if child.Box().IsFlexItem {
			items = append(items, child)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Box().Style.GetOrder() < items[j].Box().Style.GetOrder()
	})
	for _, child := range items {
		cs := child.Box().Style
		var frame pr.Float
		for _, d := range [...]pr.TaggedDim{cs.GetPaddingTop(), cs.GetPaddingBottom(), cs.GetBorderTopWidth(), cs.GetBorderBottomWidth()} {
			v, _ := pixels(d)
			frame += v
		}
		size, known := pixels(cs.GetFlexBasis())
		if !known {
			size, known = pixels(cs.GetHeight())
		}
		if !known {
			return 0, false
		}
		borderBox := cs.GetBoxSizing() == "border-box"
		if v, has := pixels(cs.GetMaxHeight()); has && size > v {
			size = v
		}
		if v, has := pixels(cs.GetMinHeight()); has && size < v {
			size = v
		}
		if borderBox {
			size = max(size, frame)
		} else {
			size += frame
		}
		for _, d := range [...]pr.TaggedDim{cs.GetMarginTop(), cs.GetMarginBottom()} {
			v, _ := pixels(d)
			size += v
		}
		width := itemWidth(context, child, true)
		if inLine > 0 && lineHeight+rowGap+size > limit {
			total += lineWidth
			lines++
			lineWidth, lineHeight, inLine = 0, 0, 0
		}
		if inLine > 0 {
			lineHeight += rowGap
		}
		lineHeight += size
		lineWidth = max(lineWidth, width)
		inLine++
	}
	if inLine > 0 {
		total += lineWidth
		lines++
	}
	if lines == 0 {
		return 0, false
	}
	return total + pr.Float(lines-1)*columnGap, true
}

// Return the max-content width for an “FlexContainerBox“.
func flexMaxContentWidth(context *layoutContext, box Box, outer bool) pr.Float {
	// TODO: use real values, see
	// https://www.w3.org/TR/css-flexbox-1/#intrinsic-sizes
	// gopdfgo: kontener o szerokości podanej długością ma tę szerokość także jako wewnętrzną
	// (CSS Sizing 3 § 5.1), tak jak blok i siatka; źródło liczyło ją zawsze z elementów.
	if width := box.Box().Style.GetWidth(); width.Tag != pr.Auto && width.Unit == pr.Px && width.Calc == nil {
		// gopdfgo: przy box-sizing: border-box/padding-box podana długość obejmuje obramowanie i odstęp, a adjust
		// dodaje je jeszcze raz (CSS Box Sizing 3 § 3); źródło brało ją za rozmiar treści
		return adjust(box, outer, max(width.Value-boxSizingFrame(box, true), 0), true, true)
	}
	primeFlexDefiniteHeights(box)
	var sum, max pr.Float
	for _, child := range box.Box().Children {
		if collapsedInRow(box, child) {
			continue
		}
		if child.Box().IsFlexItem {
			v := maxContentWidth(context, child, true)
			if content, ok := flexibleItemContribution(context, box, child, maxContentWidth); ok && content > v {
				v = content
			}
			if ratioWidth, ok := stretchedRatioContribution(box, child); ok {
				v = ratioWidth
			}
			v = clampByBasis(context, box, child, v)
			sum += v
			if v > max {
				max = v
			}
		}
	}
	if strings.HasPrefix(string(box.Box().Style.GetFlexDirection()), "row") {
		return adjust(box, outer, sum+flexMainGaps(box), true, true)
	} else {
		if wrapped, ok := columnWrapContentWidth(context, box, maxContentWidth); ok {
			return adjust(box, outer, wrapped, true, true)
		}
		return adjust(box, outer, max, true, true)
	}
}

// Return the size of the trailing whitespace of “box“.
// gopdfgo: z ideographic liczą się też wiszące odstępy inne niż spacja (np. ideograficzny U+3000); szerokości wewnętrzne
// odejmują ją osobno (text.FirstLine.Hang), więc tam parametr jest wyłączony.
func trailingWhitespaceSize(context *layoutContext, box Box, ideographic bool) pr.Float {
	// Find last box child, keep last parent to remove nested trailing spaces.
	var lastParent Box
	for IsLine(box) {
		ch := box.Box().Children
		if len(ch) == 0 {
			return 0
		}
		lastParent, box = box, ch[len(ch)-1]
	}
	// Return early if possible.
	textBox, ok := box.(*bo.TextBox)
	if !ok || len(textBox.Text) == 0 {
		// There’s no text in last child.
		return 0
	} else if ws := box.Box().Style.GetWhiteSpace(); !(ws == pr.Normal || ws == pr.Nowrap || ws == pr.PreLine) {
		// Spaces don’t collapse.
		return 0
	} else if box.Box().Style.GetFontSize() == pr.FToV(0) {
		// Trailing spaces take no space.
		return 0
	}
	// gopdfgo: odstępy inne niż spacja (np. ideograficzny) na końcu wiersza wiszą (CSS Text 3 § 4.1.3)
	strippedText := textBox.Text
	for n := len(strippedText); n > 0 && (strippedText[n-1] == ' ' || ideographic && text.IsOtherSpaceSeparator(strippedText[n-1])); n = len(strippedText) {
		strippedText = strippedText[:n-1]
	}
	if len(strippedText) == len(textBox.Text) {
		// No trailing space.
		return 0
	}

	// Strip text.
	if len(strippedText) != 0 {
		// Stripped text is not empty, calculate width difference.
		resume, oldResume := 0, -1
		var oldBox *bo.TextBox
		for resume != -1 {
			oldResume = resume
			oldBox, resume, _ = splitTextBox(context, textBox, nil, resume, true)
		}
		if oldBox == nil {
			panic("oldBox can't be nil")
		}
		strippedBox := textBox.CopyWithText(strippedText)
		strippedBox, resume, _ = splitTextBox(context, strippedBox, nil, oldResume, true)
		if strippedBox == nil {
			// Old box is split just before the trailing spaces.
			return oldBox.Box().Width.V()
		} else {
			if resume != -1 {
				panic("invalid resume at")
			}
			return oldBox.Box().Width.V() - strippedBox.Box().Width.V()
		}
	} else {
		// Stripped text is empty, render spaces to get width.
		spli := text.SplitFirstLine(textBox.Text, textBox.Style, context, nil, false, true)
		width := spli.Width
		// Remove possible trailing spaces from previous child.
		if lastParent != nil && len(lastParent.Box().Children) >= 2 {
			children := lastParent.Box().Children
			width += trailingWhitespaceSize(context, children[len(children)-2], ideographic)
		}
		return width
	}
}
