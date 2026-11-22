package layout

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"golang.org/x/net/html"
)

// Layout for tables and internal table boxes.

// Layout for a table box.
func tableLayout(context *layoutContext, table_ bo.TableBoxITF, bottomSpace pr.Float, skipStack tree.ResumeStack,
	pageIsEmpty bool, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder,
) (bo.BlockLevelBoxITF, blockLayout) {
	context.independentDepth++ // gopdfgo: zob. layoutContext.canPushFloat
	defer func() { context.independentDepth-- }()
	table := table_.Table()

	// Remove top and bottom decorations for split tables.
	hasHeader := len(table.Children) != 0 && table.Children[0].Box().IsHeader
	hasFooter := len(table.Children) != 0 && table.Children[len(table.Children)-1].Box().IsFooter
	collapse := table.Style.GetBorderCollapse() == "collapse"
	// gopdfgo: obramowanie i odstęp wewnętrzny tabeli nie są powtarzane na kolejnych stronach także wtedy,
	// gdy tabela ma powtarzany nagłówek lub stopkę (box-decoration-break: slice; powtarza je dopiero clone).
	// Źródło przy nagłówku i stopce powtarzało dekorację na każdej stronie, nie rezerwując na nią miejsca,
	// więc dolne obramowanie wychodziło na margines strony.
	// W modelu obramowań zwiniętych "obramowanie" tabeli to połowa obramowania skrajnych komórek, więc
	// przy powtarzanym nagłówku lub stopce zostaje, jak w źródle.
	removeStartDecoration := skipStack != nil && !(collapse && hasHeader)
	table_.RemoveDecoration(&table.BoxFields, removeStartDecoration, false)
	// gopdfgo: przy box-decoration-break: clone każdy fragment ma dolny odstęp i obramowanie tabeli,
	// więc wiersze muszą zostawić na nie miejsce (tak samo jak w blockContainerLayout); źródło tego
	// nie rezerwowało i dolna dekoracja wychodziła na margines strony.
	outerBottomSpace := bottomSpace
	cloneDecoration := table.Style.GetBoxDecorationBreak() == "clone"
	if cloneDecoration {
		bottomSpace += table.PaddingBottom.V() + table.BorderBottomWidth.V()
	}

	// Set border spacings.
	var borderSpacingX, borderSpacingY pr.Float
	if !collapse {
		tmp := table.Style.GetBorderSpacing()
		borderSpacingX, borderSpacingY = tmp[0].Value, tmp[1].Value
	}

	// Define column positions.
	columnWidths := table.ColumnWidths
	// gopdfgo: kolumny z visibility: collapse znikają po wyznaczeniu szerokości kolumn: nie zajmują
	// miejsca ani odstępu między komórkami, tabela zwęża się o nie, a komórki rozpięte także na nich są
	// przycinane (CSS Tables 3 § 9.2 i 9.3, CSS 2.1 § 17.5.5; test WPT visibility-collapse-colspan-003).
	// W źródle visibility: collapse na kolumnach nie działało. Tylko dla kierunku ltr.
	var collapsedColumns map[int]bool
	var collapsedWidth pr.Float
	if table.Style.GetDirection() == pr.Ltr {
		for _, group := range table.ColumnGroups {
			for _, column := range group.Children {
				if x := column.Box().GridX; column.Box().Style.GetVisibility() == "collapse" && x < len(columnWidths) {
					if collapsedColumns == nil {
						collapsedColumns = map[int]bool{}
						columnWidths = slices.Clone(columnWidths)
					}
					if !collapsedColumns[x] {
						collapsedColumns[x] = true
						collapsedWidth += columnWidths[x] + borderSpacingX
						columnWidths[x] = 0
					}
				}
			}
		}
	}
	table.ColumnPositions = nil
	positionX := table.ContentBoxX()
	rowsLeftX := positionX + borderSpacingX
	var rowsWidth pr.Float
	if table.Style.GetDirection() == pr.Ltr {
		positionX := table.ContentBoxX()
		rowsX := positionX + borderSpacingX
		for x, width := range columnWidths {
			if !collapsedColumns[x] {
				positionX += borderSpacingX
			}
			table.ColumnPositions = append(table.ColumnPositions, positionX)
			positionX += width
		}
		rowsWidth = positionX - rowsX
	} else {
		positionX := table.ContentBoxX() + table.Width.V()
		rowsX := positionX - borderSpacingX
		for _, width := range columnWidths {
			positionX -= borderSpacingX
			positionX -= width
			table.ColumnPositions = append(table.ColumnPositions, positionX)
		}
		rowsWidth = rowsX - positionX
	}

	// Set border top width on tables with collapsed borders and split cells.
	var skippedRows int
	if collapse {
		table.SkipCellBorderTop = false
		table.SkipCellBorderBottom = false
		splitCells := false
		if skipStack != nil {
			skippedGroups, groupSkipStack := skipStack.Unpack()
			skippedRows = 0
			if groupSkipStack != nil {
				var cellSkipStack tree.ResumeStack
				skippedRows, cellSkipStack = groupSkipStack.Unpack()
				// gopdfgo: same dalsze ciągi komórek rozpiętych z poprzednich wierszy nie dzielą wiersza
				cellSkipStack, _ = splitRowResume(cellSkipStack)
				if cellSkipStack != nil {
					splitCells = true
				}
			}
			for _, group := range table.Children[:skippedGroups] {
				skippedRows += len(group.Box().Children)
			}
		}

		if !splitCells && !hasHeader {
			horizontalBorders := table.CollapsedBorderGrid.Horizontal
			if len(horizontalBorders) != 0 {
				var max pr.Fl
				for _, tmp := range horizontalBorders[skippedRows] {
					if tmp.Width > max {
						max = tmp.Width
					}
				}
				table.BorderTopWidth = pr.Float(max / 2)
			}
		}
	}

	// gopdfgo: wysokość powtarzanego nagłówka i stopki w bieżącej próbie układu treści (zob. allGroupsLayout).
	// Na następnej stronie zajmą tyle samo, więc wierszom zostaje tam co najwyżej fragmentator bez nich.
	var repeatedPartsHeight pr.Float
	// freshRowsHeight zwraca górne ograniczenie wysokości, jaką wiersz miałby do dyspozycji na nowej stronie
	// (albo w nowej kolumnie), albo +Inf, gdy treść nie jest dzielona na fragmenty. Wiersz wyższy od tej
	// wysokości nie zmieści się na żadnej stronie, więc jest dzielony tam, gdzie wypada (zob. tallRow).
	// Celowo ograniczenie górne: przy niedoszacowaniu wiersz, który zmieściłby się na następnej stronie,
	// byłby niepotrzebnie dzielony; przy przeszacowaniu najwyżej przechodzi dalej jak dotąd.
	freshRowsHeight := func(bottomSpace pr.Float) pr.Float {
		fragmentainerHeight := repeatFragmentainerHeight(context, bottomSpace)
		if fragmentainerHeight <= 0 {
			return pr.Inf
		}
		return fragmentainerHeight - repeatedPartsHeight
	}

	// naturalRowHeight mierzy wysokość, jaką wiersz miałby bez ograniczenia stroną: najwyższa z jego własnych
	// komórek nierozpiętych (z resztą treści według wznowienia i z podaną wysokością). Komórki muszą mieć już
	// ustalone położenie i szerokość z próby układu wiersza. Pomiar jest kosztowny, więc wołamy go tylko
	// wtedy, gdy od wysokości zależy, czy wiersz przechodzi na następną stronę w całości.
	naturalRowHeight := func(entries []cellEntry) pr.Float {
		var height pr.Float
		for _, entry := range entries {
			cell := entry.box.Box()
			if entry.own < 0 || cell.Rowspan > 1 {
				continue
			}
			originalStyle := cell.Style
			if cell.Style.GetHeight().Tag != pr.Auto {
				styleCopy := cell.Style.Copy()
				styleCopy.SetHeight(pr.TagToV(pr.Auto))
				cell.Style = styleCopy
			}
			cell.Height = pr.AutoF
			measured, _, _ := blockContainerLayout(context, entry.box, -pr.Inf, entry.skip, true,
				new([]*AbsolutePlaceholder), new([]*AbsolutePlaceholder), new([]pr.Float), false, -1)
			cell.Style = originalStyle
			if measured == nil {
				continue
			}
			box := measured.Box()
			content := box.Height.V()
			if computed, ok := cell.ComputedHeight.(pr.Float); ok {
				content = max(content, computed)
			}
			height = max(height, box.BorderHeight()-box.Height.V()+content)
		}
		return height
	}

	// Make this a sub-function so that many local variables like rowsX
	// don't need to be passed as parameters.
	groupLayout := func(group_ Box, positionY, bottomSpace pr.Float, pageIsEmpty bool, skipStack tree.ResumeStack) (Box, tree.ResumeStack, tree.PageBreak) {
		var resumeAt tree.ResumeStack
		nextPage := tree.PageBreak{Break: "any"}
		originalPageIsEmpty := pageIsEmpty
		forcedBreak := false // podział wymuszony między wierszami grupy (break-before/after)
		resolvePercentagesBox(group_, &table.BoxFields)
		group := group_.Box()
		group.PositionX = rowsLeftX
		group.PositionY = positionY
		group.Width = rowsWidth
		newGroupChildren := []Box{}
		// For each row, cells for which this is the last row (with rowspan)
		endingCellsByRow := make([][]Box, len(group.Children))
		// gopdfgo: ułożone komórki rozpięte na kilka wierszy i dalsze ciągi takich komórek (zob. spanState)
		spans := map[Box]spanState{}

		isGroupStart := skipStack == nil
		skip := 0
		if !isGroupStart {
			skip, skipStack = skipStack.Unpack()
		}

		// gopdfgo: komórka rozpięta na kilka wierszy należy do wiersza, w którym się zaczyna, więc
		// break-inside: avoid tego wiersza albo samej komórki obejmuje też granice między wierszami, przez
		// które komórka biegnie (CSS Fragmentation 3 § 3.2: podział wewnątrz potomka jest podziałem wewnątrz
		// pudełka; Chromium tak samo). W źródle wiersze pod taką komórką rozdzielano jak każde inne.
		for _, row_ := range group.Children {
			row_.Box().RowspanKeepsNext = false
		}
		for r, row_ := range group.Children {
			rowAvoid := avoidPageBreak(string(row_.Box().Style.GetBreakInside()), context)
			for _, cell_ := range row_.Box().Children {
				cell := cell_.Box()
				if cell.Rowspan <= 1 || !(rowAvoid || avoidPageBreak(string(cell.Style.GetBreakInside()), context)) {
					continue
				}
				for k := r; k < min(r+cell.Rowspan-1, len(group.Children)-1); k++ {
					group.Children[k].Box().RowspanKeepsNext = true
				}
			}
		}

		for i, row_ := range group.Children[skip:] {
			row := row_.Box()
			indexRow := i + skip
			row.Index = indexRow
			if len(newGroupChildren) != 0 {
				pageBreak := blockLevelPageBreak(newGroupChildren[len(newGroupChildren)-1], row_)
				if forcePageBreak(pageBreak, context) {
					nextPage.Break = pageBreak
					resumeAt = tree.ResumeStack{indexRow: nil}
					forcedBreak = true
					break
				}
			}

			resolvePercentagesBox(row_, &table.BoxFields)
			row.PositionX = rowsLeftX
			row.PositionY = positionY
			row.Width = rowsWidth
			// gopdfgo: dalszy ciąg wiersza o podanej wysokości, podzielonego między strony, ma resztę tej
			// wysokości (zob. niżej rowHeightSplit); skipStack to tu stos wznowienia tego wiersza.
			if remaining, ok := context.cellHeightRemaining[resumeKey(skipStack)]; ok && skipStack != nil {
				row.Height = remaining
			}
			specifiedRowHeight, rowHeightSpecified := row.Height.(pr.Float)
			// Place cells at the top of the row and layout their content.
			var newRowChildren []Box
			leftBehind := false  // komórka z treścią, z której nic nie zostało na tej stronie
			placedSome := false  // inna komórka, z której coś na tej stronie zostało
			heightSplit := false // komórka dzielona według podanej wysokości (zob. niżej)
			// gopdfgo: wiersz wyższy niż cała strona (zob. freshRowsHeight) nie zmieści się na żadnej stronie,
			// więc jest dzielony tam, gdzie wypada, zamiast najpierw przejść na następną stronę i zostawić
			// tę prawie pustą (CSS Fragmentation 3 § 4.4 i § 5.1: treść, która nie mieści się we
			// fragmentatorze, dzielimy w środku; decyzja użytkownika z 2026-11-20, uzupełnienie ADR 0020).
			// Wiersz niższy od strony nadal przechodzi w całości (CSS Tables 3 § 6.1). W źródle wiersz
			// o podanej wysokości był dzielony tylko na początku strony, a wiersz z komórką, z której nic
			// się nie zmieściło, przechodził dalej niezależnie od swojej wysokości.
			rowsHeightLimit := freshRowsHeight(bottomSpace)
			tallRow := false
			// gopdfgo: komórki układane w tym wierszu: własne komórki wiersza i dalsze ciągi komórek
			// rozpiętych (rowspan) z wierszy, które zostały na poprzedniej stronie (zob. rowCellEntries).
			entries := rowCellEntries(group, row_, indexRow, skipStack)
			for _, entry := range entries {
				cell_ := entry.box
				indexCell := entry.own
				if indexCell < 0 {
					indexCell = len(row.Children) // tylko do komunikatów
				}
				cell := cell_.Box()
				var spannedWidths []pr.Float
				if cell.GridX < len(columnWidths) {
					spannedWidths = columnWidths[cell.GridX:]
				}
				if cell.Colspan < len(spannedWidths) {
					spannedWidths = spannedWidths[:cell.Colspan]
				}
				// In the fixed layout the grid width is set by cells in
				// the first row and column elements.
				// This may be less than the previous value of cell.colspan
				// if that would bring the cell beyond the grid width.
				cell.Colspan = len(spannedWidths)
				if cell.Colspan == 0 {
					// The cell is entierly beyond the grid width, remove it
					// entierly. Subsequent cells in the same row have greater
					// gridX, so they are beyond too.
					ignoredCells := row.Children[min(indexCell, len(row.Children)):]
					logger.WarningLogger.Printf("This table row has more columns than the table, ignored %d cells: %v",
						len(ignoredCells), ignoredCells)
					break
				}
				resolvePercentagesBox(cell_, &table.BoxFields)
				if table.Style.GetDirection() == pr.Ltr {
					cell.PositionX = table.ColumnPositions[cell.GridX]
				} else {
					cell.PositionX = table.ColumnPositions[cell.GridX+cell.Colspan-1]
				}
				cell.PositionY = row.PositionY
				cell.MarginTop = pr.Float(0)
				cell.MarginLeft = pr.Float(0)
				cell.Width = pr.Float(0)
				bordersPlusPadding := cell.BorderWidth() // with width==0
				// TODO: we should remove the number of columns with no
				// originating cells to cell.colspan, see test_layout_table_auto_49.
				spanned := cell.Colspan
				if collapsedColumns != nil {
					spanned = 0
					for x := cell.GridX; x < cell.GridX+cell.Colspan; x++ {
						if !collapsedColumns[x] {
							spanned++
						}
					}
					if spanned != cell.Colspan && cell.Style.GetOverflow() == "visible" {
						cell.Style = cell.Style.Copy()
						cell.Style.SetOverflow("hidden")
						if spanned == 0 {
							cell.Style.SetVisibility("hidden")
						}
					}
				}
				width := borderSpacingX*pr.Float(max(spanned, 1)-1) - bordersPlusPadding
				for _, sw := range spannedWidths {
					width += sw
				}
				if collapsedColumns != nil {
					width = max(width, 0)
				}
				cell.Width = width
				cellSkipStack := entry.skip
				// gopdfgo: komórka rozpięta na dalsze wiersze tej grupy (albo dalszy ciąg takiej komórki)
				// nie dzieli wiersza, w którym się zaczyna: jej treść dzielona na końcu strony przechodzi do
				// dalszego ciągu komórki, a kolejne wiersze są układane dalej (CSS Tables 3 § 6, CSS
				// Fragmentation 3 § 4.4). W źródle podział treści takiej komórki dzielił jej pierwszy wiersz,
				// a komórka nie miała dalszego ciągu na następnej stronie.
				spanning := cell.Rowspan > 1 || entry.own < 0

				// Adapt cell and table collapsing borders when a row is split.
				if cellSkipStack != nil && collapse {
					if hasHeader {
						// We have a header, we have to adapt the position of
						// the split cell to match the header’s bottom border.
						headerRows := table.Children[0].Box().Children
						if L := len(headerRows); L != 0 && len(headerRows[L-1].Box().Children) != 0 {
							max := -pr.Inf
							for _, header := range headerRows[L-1].Box().Children {
								if w := header.Box().BorderBottomWidth; w > max {
									max = w
								}
							}
							cell.PositionY += max
						}
					} else if entry.own >= 0 {
						// We don’t have a header, we have to skip the
						// decoration at the top of the table when it’s drawn.
						// gopdfgo: tylko dla własnych komórek podzielonego wiersza; dalszy ciąg komórki
						// rozpiętej stoi w wierszu zaczętym na tej stronie, którego górne obramowanie zostaje.
						table.SkipCellBorderTop = true
					}
				}

				if traceMode {
					traceLogger.DumpTree(cell_, fmt.Sprintf("cell %d (before layout)", indexCell))
				}

				// First try to render content as if there was already something
				// on the page to avoid hitting block_level_layout’s TODO. Then
				// force to render something if the page is actually empty, or
				// just draw an empty cell otherwise. See
				// test_table_break_children_margin.
				// Pretend that height is not set, keeping computed height as a minimum.
				cell.ComputedHeight = cell.Height
				// gopdfgo: procentowa wysokość komórki nie jest minimum wiersza; wiersz z taką komórką
				// dostaje swój procent wysokości tabeli dopiero przy rozdziale nadmiaru wysokości (CSS Tables 3
				// § 3.10.3, jak Chromium; zob. distributeExtraTableHeight). Źródło liczyło procent od wysokości
				// tabeli jako minimum, więc wiersze rosły ponad podaną wysokość tabeli (testy WPT
				// table-as-item-cell-percentage-002, percent-height-replaced-in-percent-cell).
				if cell.Style.GetHeight().Unit == pr.Perc {
					cell.ComputedHeight = pr.AutoF
				}
				if remaining, ok := context.cellHeightRemaining[resumeKey(cellSkipStack)]; ok && cellSkipStack != nil {
					cell.ComputedHeight = remaining // dalszy ciąg komórki: zob. niżej
				}
				// gopdfgo: komórka o podanej wysokości jest podstawą wysokości procentowych swoich dzieci
				// (CSS Tables 3 § 3.10.3), choć sama jest układana jak pudełko o wysokości auto. W źródle
				// procenty w komórce liczyły się zawsze jak auto.
				cell.AnonymousBaseHeight = nil
				if specified, ok := cell.Height.(pr.Float); ok && cell.Style.GetHeight().Tag == 0 && cell.Style.GetHeight().Unit != pr.Perc {
					cell.AnonymousBaseHeight = specified
				}
				cell.Height = pr.AutoF
				// gopdfgo: max-height nie ogranicza komórki tabeli, której wysokość wyznacza treść i wiersz
				// (CSS 2.2 § 10.7 zostawia skutek max-height dla komórek nieokreślonym; przeglądarki go
				// pomijają, test WPT table-cell-multicol-nested-002). W źródle ostatni fragment komórki był
				// przycinany do max-height, a treść wystawała z wiersza.
				cell.MaxHeight = pr.Inf
				// gopdfgo: min-height też nie (ten sam § 10.7; Chromium pomija min-height komórki, jak
				// wiersza i grupy wierszy). Dokumenty z textutil (Word → HTML) dają każdej komórce
				// min-height z wysokości wiersza w Wordzie, a wiersze wychodziły prawie dwa razy wyższe.
				cell.MinHeight = pr.Float(0)
				originalStyle := cell.Style
				if cell.Style.GetHeight().Tag != pr.Auto {
					styleCopy := cell.Style.Copy()
					styleCopy.SetHeight(pr.TagToV(pr.Auto))
					cell.Style = styleCopy
				}
				newCell, tmp, _ := blockContainerLayout(context, cell_, bottomSpace, cellSkipStack, pageIsEmpty,
					absoluteBoxes, fixedBoxes, new([]pr.Float), false, -1)
				cellResumeAt := tmp.resumeAt
				cell.Style = originalStyle
				stalled := false // komórka rozpięta, z której treści nic nie zmieściło się na tej stronie
				if newCell == nil {
					if cellSkipStack == nil && len(cell.Children) != 0 && !spanning {
						leftBehind = true
					}
					cell_ = bo.CopyWithChildren(cell_, nil)
					cell_, _, _ = blockContainerLayout(context, cell_, bottomSpace, cellSkipStack, true,
						new([]*AbsolutePlaceholder), new([]*AbsolutePlaceholder), new([]pr.Float), false, -1)
					cellResumeAt = tree.ResumeStack{0: nil}
					// gopdfgo: dalszy ciąg komórki rozpiętej, z którego nic się tu nie zmieściło, wznawia
					// się w tym samym miejscu, a nie od początku treści (inaczej treść by się powtórzyła);
					// gdy cała treść już była, nie ma czego wznawiać. To samo dotyczy dalszego ciągu zwykłej
					// komórki podzielonego wiersza (np. pod powtórzonym nagłówkiem nie mieści się jej blok
					// niepodzielny, a inne komórki wiersza płyną dalej): źródło wznawiało ją od {0: nil},
					// więc treść z poprzednich stron powtarzała się (CSS Fragmentation 3 § 4.4).
					if cellSkipStack != nil {
						cellResumeAt = cellSkipStack
						if k, _ := cellSkipStack.Unpack(); len(cellSkipStack) == 1 && k >= len(cell.Children) {
							cellResumeAt = nil
						}
					}
					stalled = spanning && cellResumeAt != nil
				} else {
					cell_ = newCell
				}

				if traceMode {
					traceLogger.DumpTree(cell_, fmt.Sprintf("cell %d", indexCell))
				}

				cell = cell_.Box()
				cell_.RemoveDecoration(cell, cellSkipStack != nil, false)
				// gopdfgo: komórka o podanej wysokości większej niż miejsce do końca strony, której treść
				// zmieściła się w całości, w wierszu na początku strony (gdzie przeniesienie wiersza nic nie
				// da) jest dzielona: fragment sięga końca strony, a reszta podanej wysokości przechodzi do
				// dalszego ciągu bez treści (CSS Fragmentation 3 § 5.1, jak blok w block.go; zob. niżej
				// cellHeightRemaining). W źródle wiersz wystawał poza stronę, a za nim ginęło to, co poza nią.
				// Poza początkiem strony wiersz jak dotąd przechodzi na następną stronę.
				// gopdfgo: poza początkiem strony tak samo dzielimy komórkę, która razem z odstępami
				// i obramowaniem jest wyższa niż cała strona (zob. tallRow).
				if computed, ok := cell.ComputedHeight.(pr.Float); ok && !spanning && cellResumeAt == nil && newCell != nil {
					tall := computed+cell.BorderHeight()-cell.Height.V() > rowsHeightLimit
					if available := context.pageBottom - bottomSpace - cell.ContentBoxY(); (pageIsEmpty || tall) && available > 0 &&
						cell.Height.V() < available && computed > available {
						cellResumeAt = tree.ResumeStack{len(entry.box.Box().Children): nil}
						heightSplit = true
					}
					tallRow = tallRow || tall
				}
				if spanning {
					// gopdfgo: komórka dzielona między strony nie jest wyrównywana w pionie: jej treść
					// zaczyna się u góry każdego fragmentu (tak jak treść dzielonego bloku).
					spans[cell_] = spanState{row: entry.originRow, cell: entry.originCell, own: entry.own,
						resume: cellResumeAt, top: cellResumeAt != nil || entry.own < 0, stalled: stalled}
				} else if cellResumeAt != nil {
					if resumeAt == nil {
						resumeAt = tree.ResumeStack{indexRow: tree.ResumeStack{}}
					}
					resumeAt[indexRow][indexCell] = cellResumeAt
				}

				any := false
				for _, child := range cell.Children {
					if child.Box().IsFloated() || child.Box().IsInNormalFlow() {
						any = true
						break
					}
				}
				cell.Empty = !any
				if any && newCell != nil {
					placedSome = true
				}

				cell.ContentHeight = cell.Height.V()
				if computed, ok := cell.ComputedHeight.(pr.Float); ok && cellResumeAt != nil && newCell != nil {
					// gopdfgo: komórka o podanej wysokości dzielona między strony albo kolumny sięga
					// najwyżej do końca fragmentatora, a resztę podanej wysokości przenosi do dalszego
					// ciągu (CSS Fragmentation 3 § 5.1, jak blok w block.go). W źródle każdy fragment
					// dostawał całą wysokość: wiersz wystawał poza fragmentator i przechodził w całości
					// dalej (test WPT table-cell-multicol-nested-003).
					available := context.pageBottom - bottomSpace - cell.ContentBoxY()
					used := max(cell.Height.V(), min(computed, available))
					if remaining := computed - used; remaining > 0 {
						if context.cellHeightRemaining == nil {
							context.cellHeightRemaining = map[uintptr]pr.Float{}
						}
						context.cellHeightRemaining[context.keepResumeKey(cellResumeAt)] = remaining
					}
					cell.Height = used
				} else if cell.ComputedHeight != pr.AutoF {
					cell.Height = max(cell.Height.V(), cell.ComputedHeight.V())
				}
				newRowChildren = append(newRowChildren, cell_)
			}

			// gopdfgo: komórka rozpięta, która kończy się w tym wierszu, a jej treść nie zmieściła się do końca
			// strony, dzieli ten wiersz: wiersz sięga końca strony, a na następnej ma dalszy ciąg razem
			// z dalszym ciągiem tej komórki (CSS Fragmentation 3 § 4.4). Własne komórki wiersza dostają wtedy
			// jawne wznowienie (zwykle „cała treść już była”), żeby ich treść nie powtórzyła się.
			var pendingEnd []Box
			stalledEnd := false
			{
				for _, cell := range endingCellsByRow[0] {
					if state, ok := spans[cell]; ok && state.resume != nil {
						pendingEnd = append(pendingEnd, cell)
					}
				}
				for _, cell := range newRowChildren {
					if state, ok := spans[cell]; ok && state.own < 0 && state.resume != nil && cell.Box().Rowspan == 1 {
						pendingEnd = append(pendingEnd, cell)
					}
				}
				if len(pendingEnd) != 0 && resumeAt == nil {
					resumeAt = tree.ResumeStack{indexRow: tree.ResumeStack{}}
				}
				// Komórka, której treść nie zaczęła się na tej stronie, a kończy się w tym wierszu, przenosi
				// ten wiersz na następną stronę (jak komórka bez treści w leftBehind): podzielony wiersz
				// bez postępu w treści powtarzałby się w nieskończoność.
				for _, cell := range pendingEnd {
					if spans[cell].stalled && !pageIsEmpty {
						stalledEnd = true
					}
				}
				if resumeAt != nil {
					own := resumeAt[indexRow]
					if own == nil {
						own = tree.ResumeStack{}
						resumeAt[indexRow] = own
					}
					if len(pendingEnd) != 0 {
						for i, cell := range row.Children {
							if _, has := own[i]; !has {
								own[i] = tree.ResumeStack{len(cell.Box().Children): nil}
							}
						}
					}
					for _, cell := range newRowChildren {
						if state, ok := spans[cell]; ok && state.own >= 0 && state.resume != nil {
							own[state.own] = state.resume
						}
					}
				}
			}

			if resumeAt != nil && !pageIsEmpty {
				// Avoid break when "break-inside: avoid" is set on row or any
				// on its cells.
				avoidBreak := avoidPageBreak(string(row.Style.GetBreakInside()), context)
				// gopdfgo: wiersz, którego któraś komórka nie zmieściłaby na tej stronie żadnej treści
				// (np. trzy wiersze tekstu przy orphans i widows 2), a inna by zmieściła, przechodzi na
				// następną stronę cały, o ile nie jest pierwszym wierszem grupy na stronie: podział między wierszami
				// jest lepszy niż podział, który zostawia komórkę pustą (CSS Break 3 § 3.1, tak dzielą
				// przeglądarki). W źródle na stronie zostawały komórki jednowierszowe (numer, kwoty),
				// a nazwa pozycji lądowała na następnej.
				// gopdfgo: nie dotyczy wiersza wyższego niż cała strona (zob. tallRow): na następnej stronie też by
				// się nie zmieścił, więc dzielimy go tutaj, a treść komórki, z której nic się nie zmieściło
				// (np. blok niepodzielny), przechodzi do jej dalszego ciągu.
				if leftBehind && placedSome && len(newGroupChildren) != 0 && !tallRow &&
					!(rowHeightSpecified && specifiedRowHeight > rowsHeightLimit) &&
					!(rowsHeightLimit < pr.Inf && naturalRowHeight(entries) > rowsHeightLimit) {
					avoidBreak = true
				}
				if stalledEnd {
					avoidBreak = true
				}
				for _, cell := range row.Children {
					avoidBreak = avoidBreak || avoidPageBreak(string(cell.Box().Style.GetBreakInside()), context)
				}
				if avoidBreak {
					resumeAt = tree.ResumeStack{indexRow: {}}
					break
				}
			}

			if resumeAt != nil {
				// Remove bottom decoration if row is split.
				for _, cell := range newRowChildren {
					cell.RemoveDecoration(cell.Box(), false, true)
				}
			}

			row_ = bo.CopyWithChildren(row_, newRowChildren)
			row = row_.Box()

			// Table height algorithm
			// http://www.w3.org/TR/CSS21/tables.html#height-layout

			// Set row baseline with cells with vertical-align: baseline.
			var baselineCells []Box
			for _, cell_ := range row.Children {
				cell := cell_.Box()
				verticalAlign := cell.Style.GetVerticalAlign()
				if spans[cell_].top {
					cell.VerticalAlign = pr.Top // fragment komórki rozpiętej dzielonej między strony
				} else if verticalAlign.Tag == pr.Top || verticalAlign.Tag == pr.Middle || verticalAlign.Tag == pr.Bottom {
					cell.VerticalAlign = verticalAlign.Tag
				} else {
					// Assume "baseline" for any other value
					cell.VerticalAlign = pr.Baseline
					cell.Baseline = cellBaseline(cell_)
					// gopdfgo: komórka bez treści w przepływie nie uczestniczy w wyrównaniu do linii bazowej
					// wiersza, jak w Chromium (ADR 0006: przy rozbieżności z przeglądarkami domyślnie Chromium).
					// Dosłowne brzmienie CSS 2.1 § 17.5.3 włącza każdą komórkę wyrównaną do linii bazowej;
					// w źródle pusta komórka wyznaczała linię bazową wiersza dołem swojej treści (testy WPT
					// width-applies-to-006, min-height-applies-to-014, table-vertical-align-baseline-008).
					if cellHasInFlowContent(cell_) {
						baselineCells = append(baselineCells, cell_)
					}
				}
			}
			if len(baselineCells) != 0 {
				maxBaseline := -pr.Inf
				for _, cell := range baselineCells {
					if bs := cell.Box().Baseline.V(); bs > maxBaseline {
						maxBaseline = bs
					}
				}
				row.Baseline = maxBaseline
				for _, cell := range baselineCells {
					extra := row.Baseline.V() - cell.Box().Baseline.V()
					if cell.Box().Baseline != row.Baseline && extra != 0 {
						addTopPadding(cell.Box(), extra)
					}
				}
			}

			if traceMode {
				traceLogger.DumpTree(row_, fmt.Sprintf("row %d (before height)", indexRow))
			}

			// Set row height.
			for _, cell := range row.Children {
				endingCellsByRow[cell.Box().Rowspan-1] = append(endingCellsByRow[cell.Box().Rowspan-1], cell)
			}
			var (
				rowBottomY  pr.Float
				endingCells []Box
			)
			endingCells, endingCellsByRow = endingCellsByRow[0], endingCellsByRow[1:]
			if len(endingCells) != 0 { // in this row
				if row.Height == pr.AutoF {
					for _, cell := range endingCells {
						if v := cell.Box().PositionY + cell.Box().BorderHeight(); v > rowBottomY {
							rowBottomY = v
						}
					}
					row.Height = max(rowBottomY-row.PositionY, 0)
				} else {
					var m pr.Float
					for _, rowCell := range endingCells {
						if v := rowCell.Box().BorderHeight(); v > m {
							m = v
						}
					}
					row.Height = max(row.Height.V(), m)
					rowBottomY = row.PositionY + row.Height.V()
				}
			} else if height, ok := row.Height.(pr.Float); ok {
				// gopdfgo: wiersz bez komórek ma podaną wysokość (CSS 2.1 § 17.5.3: wysokość wiersza
				// to większa z podanej i wymaganej przez komórki); w źródle miał zawsze zero (test WPT
				// border-collapse-empty-row)
				rowBottomY = row.PositionY + height
			} else {
				rowBottomY = row.PositionY
				row.Height = pr.Float(0)
			}

			// gopdfgo: wiersz o podanej wysokości, który nie mieści się do końca strony, a jest na jej
			// początku albo jest wyższy niż cała strona (zob. tallRow), dzielimy jak blok o podanej wysokości
			// (CSS Fragmentation 3 § 5.1): fragment sięga końca strony, a reszta podanej wysokości przechodzi
			// do dalszego ciągu wiersza na następnej stronie (zapisana w cellHeightRemaining pod stosem
			// wznowienia wiersza). Komórki, które zmieściły się w całości, nie mają dalszego ciągu treści.
			// W źródle taki wiersz wystawał poza stronę, a jego część za końcem strony ginęła.
			rowHeightSplit := false
			if limit := context.pageBottom - bottomSpace; rowHeightSpecified && rowsHeightLimit < pr.Inf &&
				row.Style.GetVisibility() != "collapse" && overflows(limit, rowBottomY) && limit > row.PositionY &&
				(pageIsEmpty || specifiedRowHeight > rowsHeightLimit) {
				cellsBottom := row.PositionY
				for _, cell := range endingCells {
					cellsBottom = max(cellsBottom, cell.Box().PositionY+cell.Box().BorderHeight())
				}
				if !overflows(limit, cellsBottom) {
					if resumeAt == nil {
						resumeAt = tree.ResumeStack{indexRow: tree.ResumeStack{}}
						for _, cell := range row.Children {
							cell.RemoveDecoration(cell.Box(), false, true)
						}
					}
					own := resumeAt[indexRow]
					for i, cell := range group.Children[indexRow].Box().Children {
						if _, has := own[i]; !has {
							own[i] = tree.ResumeStack{len(cell.Box().Children): nil}
						}
					}
					if remaining := row.Height.V() - (limit - row.PositionY); remaining > 0 {
						if context.cellHeightRemaining == nil {
							context.cellHeightRemaining = map[uintptr]pr.Float{}
						}
						context.cellHeightRemaining[context.keepResumeKey(own)] = remaining
					}
					row.Height = limit - row.PositionY
					rowBottomY = limit
					rowHeightSplit = true
				}
			}

			if len(baselineCells) == 0 {
				// gopdfgo: bez komórek wyrównanych do linii bazowej linią bazową wiersza jest najniższa dolna
				// krawędź treści komórek (CSS 2.1 § 17.5.3), ustalana niżej, po rozciągnięciu komórek;
				// w źródle była to bezwzględna współrzędna dołu wiersza zapisana jako odległość od jego góry.
				row.Baseline = nil
			}

			// Add extra padding to make the cells the same height as the row
			// and honor vertical-align.
			for _, cell_ := range endingCells {
				cell := cell_.Box()
				cellBottomY := cell.PositionY + cell.BorderHeight()
				extra := rowBottomY - cellBottomY
				moved := pr.Float(0)
				if extra != 0 {
					if cell.VerticalAlign == pr.Bottom {
						moved += extra
						addTopPadding(cell, extra)
					} else if cell.VerticalAlign == pr.Middle {
						extra /= 2.
						moved += extra
						addTopPadding(cell, extra)
						cell.PaddingBottom = cell.PaddingBottom.V() + extra
					} else {
						cell.PaddingBottom = cell.PaddingBottom.V() + extra
					}
				}
				if cell.ComputedHeight != pr.AutoF {
					var verticalAlignShift pr.Float
					if cell.VerticalAlign == pr.Middle {
						verticalAlignShift = (cell.ComputedHeight.V() - cell.ContentHeight.V()) / 2
					} else if cell.VerticalAlign == pr.Bottom {
						verticalAlignShift = cell.ComputedHeight.V() - cell.ContentHeight.V()
					}
					if verticalAlignShift > 0 {
						moved += verticalAlignShift
						for _, child := range cell.Children {
							child.Translate(0, verticalAlignShift, false)
						}
					}
				}
				// gopdfgo: pudełkiem zawierającym dla potomków pozycjonowanych jest komórka w wysokości
				// wiersza (CSS 2.1 § 10.1, CSS Tables 3 § 3.10.3), a układ komórki zna tylko wysokość
				// treści. Po rozciągnięciu komórki pozycjonowanie powtarzamy; w źródle `bottom` i wysokości
				// procentowe liczyły się od wysokości treści.
				if pending, ok := context.cellPositioned[cell]; ok {
					delete(context.cellPositioned, cell)
					for _, positioned := range pending {
						if positioned.height == cell.PaddingHeight() && moved == 0 {
							continue
						}
						{
							positioned.original.Box().PositionY += moved
							positioned.placeholder.AliasBox = positioned.original
							positioned.placeholder.layoutDone = false
							absoluteLayout(context, positioned.placeholder, cell_, fixedBoxes, bottomSpace, nil)
						}
					}
				}
			}

			if traceMode {
				traceLogger.DumpTree(row_, fmt.Sprintf("row %d", indexRow))
			}

			nextPositionY := row.PositionY + row.Height.V()
			spacingAfter := pr.Float(0) // gopdfgo: zob. niżej, sprawdzenie przepełnienia
			if resumeAt == nil {
				nextPositionY += borderSpacingY
				spacingAfter = borderSpacingY
			}
			// gopdfgo: wiersz z visibility: collapse bierze udział w wyznaczaniu
			// szerokości kolumn, ale nie zajmuje wysokości i nie jest rysowany
			// (CSS 2.1 § 17.5.5); źródło traktowało go jak zwykły wiersz
			if row.Style.GetVisibility() == "collapse" && resumeAt == nil {
				row.Height = pr.Float(0)
				nextPositionY, spacingAfter = row.PositionY, 0
				// gopdfgo: komórka rozpięta także na zwiniętym wierszu traci jego wysokość, a jej treść jest
				// przycinana (CSS Tables 3 § 9.2, „visibility-collapse-cell-rendering”; test WPT
				// visibility-collapse-rowspan-005); w źródle wystawała pod komórkę.
				clip := func(cell Box) {
					if cell.Box().Rowspan > 1 && cell.Box().Style.GetOverflow() == "visible" {
						cell.Box().Style = cell.Box().Style.Copy()
						cell.Box().Style.SetOverflow("hidden")
					}
				}
				for _, cell_ := range endingCells {
					clip(cell_)
					// komórka kończy się teraz na dole poprzedniego wiersza
					cell := cell_.Box()
					if cell.Rowspan > 1 {
						delta := row.PositionY - borderSpacingY - (cell.PositionY + cell.BorderHeight())
						if delta < 0 {
							shrink := min(-delta, cell.PaddingBottom.V())
							cell.PaddingBottom = cell.PaddingBottom.V() - shrink
							cell.Height = max(0, cell.Height.V()+delta+shrink)
						}
					}
				}
				for _, cells := range endingCellsByRow {
					for _, cell := range cells {
						clip(cell)
					}
				}
			}

			// Break if one cell was broken.
			breakCell := false
			if resumeAt != nil {
				// gopdfgo: komórka rozpięta z wcześniejszego wiersza, której treść przechodzi dalej (pendingEnd),
				// też jest treścią tego wiersza, choć nie jest jego dzieckiem
				// gopdfgo: podobnie komórka dzielona według podanej wysokości: nie ma treści, ale wiersz z nią
				// robi postęp (inaczej wiersz byłby porzucany i układany od nowa w nieskończoność)
				// gopdfgo: tak samo wiersz dzielony według własnej podanej wysokości (rowHeightSplit)
				allEmpty := len(pendingEnd) == 0 && !heightSplit && !rowHeightSplit
				for _, child := range row.Children {
					if !child.Box().Empty {
						allEmpty = false
						break
					}
				}

				if allEmpty {
					// No cell was displayed, give up row.
					nextPositionY = pr.Inf
					pageIsEmpty = false
					resumeAt = nil
				} else {
					breakCell = true
				}
			}

			// Break if this row overflows the page, unless there is no
			// other content on the page.
			// gopdfgo: liczy się dół wiersza, nie odstęp border-spacing pod nim: podział za wierszem wypada
			// w tym odstępie, więc odstęp nie musi się mieścić na stronie (CSS 2.1 § 17.6.1 i CSS
			// Fragmentation 3 nie każą go zachować; tak drukuje Chromium). Za ostatnim wierszem grupy
			// musi się zmieścić dolny odstęp wewnętrzny i obramowanie tabeli (tabela może się na nim
			// skończyć; Chromium tak samo), a odstęp przed nimi jest obcinany na końcu tableLayout. W źródle
			// wiersz, który się mieścił, ale jego odstęp już nie, przechodził na następną stronę.
			rowEnd := nextPositionY - spacingAfter
			if indexRow == len(group.Children)-1 && spacingAfter > 0 && table.Style.GetBoxDecorationBreak() != "clone" {
				rowEnd += table.PaddingBottom.V() + table.BorderBottomWidth.V()
			}
			overflow := context.overflowsPage(bottomSpace, rowEnd)
			if !pageIsEmpty && overflow {
				removePlaceholders(context, row.Children, absoluteBoxes, fixedBoxes)
				if len(newGroupChildren) != 0 {
					previousRow := newGroupChildren[len(newGroupChildren)-1]
					pageBreak := blockLevelPageBreak(previousRow, row_)
					if avoidPageBreak(pageBreak, context) {
						newGroupChildrenTmp, resumeAtTmp := findEarlierPageBreak(context, newGroupChildren, absoluteBoxes, fixedBoxes)
						if newGroupChildrenTmp != nil || resumeAtTmp != nil {
							newGroupChildren, resumeAt = newGroupChildrenTmp, resumeAtTmp
							break
						}
					} else {
						resumeAt = tree.ResumeStack{indexRow: nil}
						break
					}
				}

				if originalPageIsEmpty {
					resumeAt = tree.ResumeStack{indexRow: nil}
				} else {
					return nil, nil, nextPage
				}

				break
			}

			newGroupChildren = append(newGroupChildren, row_)
			positionY = nextPositionY
			pageIsEmpty = false
			skipStack = nil

			if breakCell && collapse && !hasFooter {
				table.SkipCellBorderBottom = true
			}

			if breakCell || resumeAt != nil {
				break
			}
		}

		// Do not keep the row group if we made a page break
		// before any of its rows or with "avoid".
		// gopdfgo: podział wymuszony wewnątrz grupy z break-inside: avoid nie przenosi grupy na następną
		// stronę: avoid dotyczy tylko podziałów niewymuszonych (CSS Fragmentation 3 § 3.1, § 4.4); w źródle
		// grupa była przenoszona w całości także wtedy (WPT rowgroup-page-break-inside-avoid-8-print).
		abort := resumeAt != nil && !originalPageIsEmpty && ((avoidPageBreak(string(group.Style.GetBreakInside()), context) && !forcedBreak) || len(newGroupChildren) == 0)
		if abort {
			removePlaceholders(context, newGroupChildren, absoluteBoxes, fixedBoxes)
			return nil, nil, nextPage
		}
		if resumeAt != nil && len(newGroupChildren) != 0 {
			resumeAt = continueSpanningCells(context, group, newGroupChildren, spans, resumeAt)
		}

		group_ = bo.CopyWithChildren(group_, newGroupChildren)
		group = group_.Box()
		group_.RemoveDecoration(group, !isGroupStart, resumeAt != nil)
		// Set missing baselines in a second loop because of rowspan.
		for _, row_ := range group.Children {
			row := row_.Box()
			if row.Baseline == nil {
				if len(row.Children) != 0 {
					// Set baseline to lowest bottom content edge.
					// gopdfgo: komórka rozciągnięta do wysokości wiersza ma treść sięgającą jego dołu (dodatek
					// do wysokości jest tu dopisany do odstępu dolnego, więc liczymy od krawędzi obramowania
					// z odstępem dolnym z CSS, jak Chromium); w źródle brana była wysokość treści sprzed
					// rozciągnięcia.
					var max pr.Float
					for _, cell_ := range row.Children {
						cell := cell_.Box()
						paddingBottom := cell.PaddingBottom.V()
						if pb := cell.Style.GetPaddingBottom(); pb.Unit == pr.Px && pb.Tag != pr.Auto {
							paddingBottom = min(paddingBottom, pb.Value)
						}
						if v := cell.BorderBoxY() + cell.BorderHeight() - cell.BorderBottomWidth.V() - paddingBottom; v > max {
							max = v
						}
					}
					row.Baseline = max - row.PositionY
				} else {
					row.Baseline = pr.Float(0)
				}
			}

		}
		group.Height = positionY - group.PositionY
		if len(group.Children) != 0 {
			// The last border spacing is outside of the group.
			group.Height = group.Height.V() - borderSpacingY
		}

		return group_, resumeAt, nextPage
	}

	bodyGroupsLayout := func(skipStack tree.ResumeStack, positionY, bottomSpace pr.Float, pageIsEmpty bool) ([]Box, tree.ResumeStack, tree.PageBreak, pr.Float) {
		skip := 0
		if skipStack != nil {
			skip, skipStack = skipStack.Unpack()
		}
		newTableChildren := []Box{}
		var resumeAt tree.ResumeStack
		nextPage := tree.PageBreak{Break: "any"}

		for i, group_ := range table.Children[skip:] {
			group := group_.Box()
			indexGroup := i + skip
			group.Index = indexGroup

			if group.IsHeader || group.IsFooter {
				continue
			}

			if L := len(newTableChildren); L != 0 {
				pageBreak := blockLevelPageBreak(newTableChildren[L-1], group_)
				if forcePageBreak(pageBreak, context) {
					nextPage.Break = pageBreak
					resumeAt = tree.ResumeStack{indexGroup: nil}
					break
				}
			}
			var newGroup Box
			newGroup, resumeAt, nextPage = groupLayout(group_, positionY, bottomSpace, pageIsEmpty, skipStack)
			skipStack = nil

			if newGroup == nil {
				if L := len(newTableChildren); L != 0 {
					previousGroup := newTableChildren[L-1]
					pageBreak := blockLevelPageBreak(previousGroup, group_)
					if avoidPageBreak(pageBreak, context) {
						v1, v2 := findEarlierPageBreak(context, newTableChildren, absoluteBoxes, fixedBoxes)
						if v1 == nil && v2 == nil {
							removePlaceholders(context, newTableChildren, absoluteBoxes, fixedBoxes)
							return nil, nil, nextPage, positionY
						}
						newTableChildren, resumeAt = v1, v2
						break
					}
					resumeAt = tree.ResumeStack{indexGroup: nil}
				} else {
					return nil, nil, nextPage, positionY
				}
				break
			}

			newTableChildren = append(newTableChildren, newGroup)
			positionY += newGroup.Box().Height.V() + borderSpacingY
			pageIsEmpty = false

			if resumeAt != nil {
				resumeAt = tree.ResumeStack{indexGroup: resumeAt}
				break
			}
		}

		return newTableChildren, resumeAt, nextPage, positionY
	}

	// Layout row groups, rows and cells.
	positionY := table.ContentBoxY()
	// gopdfgo: tabela bez wierszy nie ma odstępów między komórkami, więc jej wysokość to zero
	// (test WPT css-tables/caption-relative-positioning); źródło dodawało jeden odstęp pionowy.
	if skipStack == nil && len(table.Children) != 0 {
		positionY += borderSpacingY
	}

	initialPositionY := positionY
	var tableRows []Box
	for _, rowGroup := range table.Children {
		if !rowGroup.Box().IsHeader && !rowGroup.Box().IsFooter {
			tableRows = append(tableRows, rowGroup)
		}
	}

	// gopdfgo: fragment, w którym z tabeli została tylko stopka (zob. placeDroppedFooter), nie ma już
	// wierszy treści: stopka stoi w nim jak w tabeli bez wierszy.
	lastIndex := len(table.Children) - 1
	onlyFooterLeft := false
	if skipStack != nil && hasFooter {
		skip, _ := skipStack.Unpack()
		onlyFooterLeft = skip == lastIndex
	}
	rowsLeft := len(tableRows) != 0 && !onlyFooterLeft

	// gopdfgo: zob. overflowUnderRepeatedParts
	overflowUnderParts := (hasHeader || hasFooter) && bottomSpace > -pr.Inf && context.pageBottom != pr.Inf &&
		!context.inColumn && !context.behavior.Continuous && context.clipDepth == 0
	var behindHeaderRemainders map[*html.Node]pr.Float
	allGroupsLayout := func() (Box, []Box, Box, pr.Float, tree.ResumeStack, tree.PageBreak) {
		var (
			header, footer             Box
			headerHeight, footerHeight pr.Float
			resumeAt                   tree.ResumeStack
			nextPage                   tree.PageBreak
		)

		// If the page is not empty, we try to render the header and the footer
		// on it. If the table does not fit on the page, we try to render it on
		// the next page.
		//
		// If the page is empty and the header and footer are too big, there
		// are not rendered. If no row can be rendered because of the header and
		// the footer, the header and/or the footer are not rendered.

		headerFooterMaxBottomSpace := -pr.Inf
		if pageIsEmpty {
			headerFooterMaxBottomSpace = bottomSpace
		}

		if hasHeader {
			header = table.Children[0]
			header, resumeAt, _ = groupLayout(header, positionY, headerFooterMaxBottomSpace, false, nil)
			if header != nil && resumeAt == nil {
				headerHeight = header.Box().Height.V() + borderSpacingY
			} else { // Header too big for the page
				header = nil
			}
		} // else header = nil

		if hasFooter {
			footer = table.Children[len(table.Children)-1]
			footer, resumeAt, _ = groupLayout(footer, positionY, headerFooterMaxBottomSpace, false, nil)
			if footer != nil && resumeAt == nil {
				footerHeight = footer.Box().Height.V() + borderSpacingY
			} else { // Footer too big for the page
				footer = nil
			}
		} // else footer = nil

		// gopdfgo: zanim zrezygnujemy z nagłówka lub stopki, bo żaden wiersz się z nimi nie zmieścił,
		// próbujemy jeszcze raz tak, jak na pustej stronie: zawartość komórek może wtedy zostać podzielona.
		// Wynik bierzemy tylko wtedy, gdy treść rzeczywiście mieści się nad stopką; inaczej zostaje
		// zachowanie źródła (nagłówek i stopka znikają, żeby zrobić miejsce na wiersz). W źródle wiersz
		// wyższy niż strona zawsze zabierał tabeli nagłówek i stopkę.
		forcedBodyLayout := func(skipStack tree.ResumeStack, positionY, bottomSpace pr.Float, children []Box, resumeAt tree.ResumeStack,
			nextPage tree.PageBreak, endPositionY pr.Float,
		) ([]Box, tree.ResumeStack, tree.PageBreak, pr.Float) {
			forcedChildren, forcedResumeAt, forcedNextPage, forcedEnd := bodyGroupsLayout(skipStack, positionY, bottomSpace, true)
			if len(forcedChildren) != 0 && !context.overflowsPage(bottomSpace, forcedEnd-borderSpacingY) {
				return forcedChildren, forcedResumeAt, forcedNextPage, forcedEnd
			}
			return children, resumeAt, nextPage, endPositionY
		}

		// gopdfgo: stopka, z której zrezygnowaliśmy, bo w tym fragmencie nie zmieściła się razem z treścią,
		// jest częścią tabeli i musi się pojawić tam, gdzie wypada w treści (CSS Tables 3 § 6.2): gdy treść
		// tabeli skończyła się w tym fragmencie, stawiamy stopkę pod nią, a jeśli i ona się nie mieści,
		// przechodzi do następnego fragmentu. W źródle taka stopka znikała z dokumentu.
		placeDroppedFooter := func(children []Box, resumeAt tree.ResumeStack, endPositionY pr.Float) (Box, tree.ResumeStack, pr.Float) {
			if !hasFooter || resumeAt != nil || len(children) == 0 {
				return nil, resumeAt, endPositionY
			}
			footer, footerResume, _ := groupLayout(table.Children[lastIndex], endPositionY, bottomSpace, false, nil)
			if footer != nil && footerResume == nil {
				return footer, nil, endPositionY + footer.Box().Height.V() + borderSpacingY
			}
			return nil, tree.ResumeStack{lastIndex: nil}, endPositionY
		}

		// gopdfgo: reszta treści niepodzielnej wznawianego wiersza stoi za powtarzanym nagłówkiem (zob.
		// overflowUnderRepeatedParts): wysokość nagłówka liczy się do niej, więc pod nagłówkiem zostaje
		// tylko to, co z niej wystaje.
		behindHeader := func(headerHeight pr.Float) tree.ResumeStack {
			behindHeaderRemainders = nil
			if !overflowUnderParts || skipStack == nil {
				return skipStack
			}
			return mapMonolithicRemainders(context, table_, skipStack, func(box Box, remaining float64) float64 {
				if behindHeaderRemainders == nil {
					behindHeaderRemainders = map[*html.Node]pr.Float{}
				}
				behindHeaderRemainders[box.Box().Element] = pr.Float(remaining)
				return remaining - float64(headerHeight)
			})
		}

		// Don't remove headers and footers if breaks are avoided in line groups
		skip := 0
		if skipStack != nil {
			skip, _ = skipStack.Unpack()
		}
		avoidBreaks := false
		for _, group_ := range table.Children[skip:] {
			group := group_.Box()
			if bi := group.Style.GetBreakInside(); !group.IsHeader && !group.IsFooter {
				avoidBreaks = avoidPageBreak(string(bi), context)
				break
			}
		}

		if header != nil && footer != nil {
			// Try with both the header and footer
			var (
				newTableChildren []Box
				endPositionY     pr.Float
			)
			bodySkip := behindHeader(headerHeight)
			repeatedPartsHeight = headerHeight + footerHeight
			newTableChildren, resumeAt, nextPage, endPositionY = bodyGroupsLayout(bodySkip, positionY+headerHeight,
				bottomSpace+footerHeight, avoidBreaks)
			if len(newTableChildren) == 0 && rowsLeft && pageIsEmpty {
				newTableChildren, resumeAt, nextPage, endPositionY = forcedBodyLayout(bodySkip, positionY+headerHeight, bottomSpace+footerHeight,
					newTableChildren, resumeAt, nextPage, endPositionY)
			}
			if len(newTableChildren) != 0 || !rowsLeft || !pageIsEmpty {
				if resumeAt != nil {
					// gopdfgo: po wierszu przerwanym na granicy strony odstęp jest przed powtarzaną stopką,
					// a nie za nią (fragment kończy się stopką); źródło dosuwało stopkę do przerwanego wiersza.
					// Liczymy od dołu ostatniego wiersza: po podziale między wierszami endPositionY ma już
					// odstęp za wierszem i stopka stawała o drugi odstęp niżej niż w Chromium.
					endPositionY = lastRowBottom(newTableChildren, endPositionY) + borderSpacingY
					footer.Translate(0, endPositionY-footer.Box().PositionY, false)
					endPositionY += footerHeight - borderSpacingY
				} else {
					footer.Translate(0, endPositionY-footer.Box().PositionY, false)
					endPositionY += footerHeight
				}
				return header, newTableChildren, footer, endPositionY, resumeAt, nextPage
			} else {
				// We could not fit any content, drop the footer
				footer = nil
			}
		}

		if header != nil && footer == nil {
			// Try with just the header
			var (
				newTableChildren []Box
				endPositionY     pr.Float
			)
			bodySkip := behindHeader(headerHeight)
			repeatedPartsHeight = headerHeight
			newTableChildren, resumeAt, nextPage, endPositionY = bodyGroupsLayout(bodySkip, positionY+headerHeight, bottomSpace, avoidBreaks)
			if len(newTableChildren) == 0 && rowsLeft && pageIsEmpty {
				newTableChildren, resumeAt, nextPage, endPositionY = forcedBodyLayout(bodySkip, positionY+headerHeight, bottomSpace,
					newTableChildren, resumeAt, nextPage, endPositionY)
			}
			if len(newTableChildren) != 0 || !rowsLeft || !pageIsEmpty {
				footer, resumeAt, endPositionY = placeDroppedFooter(newTableChildren, resumeAt, endPositionY)
				return header, newTableChildren, footer, endPositionY, resumeAt, nextPage
			} else {
				// We could not fit any content, drop the header
				header = nil
			}
		}

		if footer != nil && header == nil {
			// Try with just the footer
			var (
				newTableChildren []Box
				endPositionY     pr.Float
			)
			repeatedPartsHeight = footerHeight
			newTableChildren, resumeAt, nextPage, endPositionY = bodyGroupsLayout(skipStack, positionY, bottomSpace+footerHeight, avoidBreaks)
			if len(newTableChildren) == 0 && rowsLeft && pageIsEmpty {
				newTableChildren, resumeAt, nextPage, endPositionY = forcedBodyLayout(skipStack, positionY, bottomSpace+footerHeight,
					newTableChildren, resumeAt, nextPage, endPositionY)
			}
			if len(newTableChildren) != 0 || !rowsLeft || !pageIsEmpty {
				if resumeAt != nil {
					// gopdfgo: po wierszu przerwanym na granicy strony odstęp jest przed powtarzaną stopką,
					// a nie za nią (fragment kończy się stopką); źródło dosuwało stopkę do przerwanego wiersza.
					// Liczymy od dołu ostatniego wiersza: po podziale między wierszami endPositionY ma już
					// odstęp za wierszem i stopka stawała o drugi odstęp niżej niż w Chromium.
					endPositionY = lastRowBottom(newTableChildren, endPositionY) + borderSpacingY
					footer.Translate(0, endPositionY-footer.Box().PositionY, false)
					endPositionY += footerHeight - borderSpacingY
				} else {
					footer.Translate(0, endPositionY-footer.Box().PositionY, false)
					endPositionY += footerHeight
				}
				return header, newTableChildren, footer, endPositionY, resumeAt, nextPage
			} else {
				// We could not fit any content, drop the footer
				footer = nil
			}
		}

		if header != nil || footer != nil {
			panic(fmt.Sprintf("expected empty header and footer, got %v %v", header, footer))
		}
		repeatedPartsHeight = 0
		newTableChildren, resumeAt, nextPage, endPositionY := bodyGroupsLayout(skipStack, positionY, bottomSpace, pageIsEmpty)
		footer, resumeAt, endPositionY = placeDroppedFooter(newTableChildren, resumeAt, endPositionY)
		return header, newTableChildren, footer, endPositionY, resumeAt, nextPage
	}

	// Closure getting the column cells.
	getColumnCells := func(table *bo.TableBox, column *bo.BoxFields) func() []Box {
		return func() []Box {
			var out []Box
			for _, rowGroup := range table.Children {
				for _, row := range rowGroup.Box().Children {
					for _, cell := range row.Box().Children {
						if cell.Box().GridX == column.GridX {
							out = append(out, cell)
						}
					}
				}
			}
			return out
		}
	}

	// gopdfgo: nagłówek albo stopka, które nie mieszczą się w całości nawet na pustej stronie, przestają być
	// powtarzane i płyną jak zwykła grupa wierszy (z podziałem między strony). W źródle taka grupa była
	// po cichu pomijana i jej treść znikała z dokumentu.
	// gopdfgo: w kolumnach (próby równoważenia o dowolnie małej wysokości) i przy próbie bez miejsca
	// (bottomSpace = +Inf, zob. columnsLayout) zmieniamy kopię tabeli, nie pudełko źródłowe: inaczej
	// nagłówek tracił powtarzanie na stałe, a tabela dzieliła się zaraz za nim (fragment z samym
	// nagłówkiem, bez powtórzenia w następnej kolumnie; korpus 22-multicol-pagination).
	// gopdfgo: nagłówek i stopka są powtarzane tylko wtedy, gdy każda z tych grup mieści się w ćwierci
	// wysokości fragmentatora (strony albo kolumny), osobno dla nagłówka i osobno dla stopki (CSS Tables 3
	// § 6.2 „Repeating headers across pages”). Grupa wyższa płynie jak zwykła grupa wierszy, czyli
	// pojawia się tylko tam, gdzie wypada w treści; druga grupa jest nadal powtarzana. Decyzję podejmujemy
	// w każdym fragmencie na kopii tabeli (pudełko źródłowe zostaje), bo fragmentatory mogą mieć różną
	// wysokość, a w kolumnach próby równoważenia mają różne wysokości. W źródle powtarzana była każda
	// grupa, która zmieściła się na stronie (testy WPT CSS2/pagination/row*-page-break-inside-avoid-*).
	if fragmentainerHeight := repeatFragmentainerHeight(context, bottomSpace); fragmentainerHeight > 0 && (hasHeader || hasFooter) {
		tooTall := func(group_ Box) bool {
			group, _, _ := groupLayout(group_, positionY, -pr.Inf, false, nil)
			return group != nil && group.Box().Height.V() > fragmentainerHeight/4
		}
		var groups []Box
		demote := func(i int, header bool) {
			if groups == nil {
				groups = append([]Box(nil), table.Children...)
			}
			groups[i] = groups[i].Copy()
			if header {
				groups[i].Box().IsHeader = false
			} else {
				groups[i].Box().IsFooter = false
			}
		}
		if hasHeader && tooTall(table.Children[0]) {
			demote(0, true)
		}
		if hasFooter && tooTall(table.Children[len(table.Children)-1]) {
			demote(len(table.Children)-1, false)
		}
		if groups != nil {
			table_ = bo.CopyWithChildren(table_, groups).(bo.TableBoxITF)
			return tableLayout(context, table_, bottomSpace, skipStack, pageIsEmpty, absoluteBoxes, fixedBoxes)
		}
	}

	if pageIsEmpty && skipStack == nil && (hasHeader || hasFooter) {
		permanent := !context.inColumn && bottomSpace < pr.Inf
		groups := table.Children
		if !permanent {
			groups = append([]Box(nil), table.Children...)
		}
		demote := func(i int, header bool) {
			if !permanent {
				groups[i] = groups[i].Copy()
			}
			if header {
				groups[i].Box().IsHeader = false
			} else {
				groups[i].Box().IsFooter = false
			}
		}
		demoted := false
		if hasHeader {
			if group, groupResume, _ := groupLayout(table.Children[0], positionY, bottomSpace, false, nil); group == nil || groupResume != nil {
				demote(0, true)
				demoted = true
			}
		}
		if hasFooter {
			last := table.Children[len(table.Children)-1]
			if group, groupResume, _ := groupLayout(last, positionY, bottomSpace, false, nil); group == nil || groupResume != nil {
				demote(len(table.Children)-1, false)
				demoted = true
			}
		}
		if demoted {
			if !permanent {
				table_ = bo.CopyWithChildren(table_, groups).(bo.TableBoxITF)
			}
			return tableLayout(context, table_, bottomSpace, skipStack, pageIsEmpty, absoluteBoxes, fixedBoxes)
		}
	}

	header, newTableChildren, footer, positionY, resumeAt, nextPage := allGroupsLayout()
	overflowUnder := false
	if overflowUnderParts && len(newTableChildren) != 0 {
		if header == nil {
			behindHeaderRemainders = nil
		}
		var headerHeight pr.Float
		if header != nil {
			headerHeight = header.Box().Height.V() + borderSpacingY
		}
		resumeAt, overflowUnder = overflowUnderRepeatedParts(context, table_, newTableChildren, behindHeaderRemainders,
			headerHeight, footer != nil, resumeAt, context.pageBottom-outerBottomSpace)
	}
	if newTableChildren == nil {
		if resumeAt != nil {
			panic(fmt.Sprintf("resumeAt should be nil, got %v", resumeAt))
		}
		return nil, blockLayout{resumeAt: resumeAt, nextPage: nextPage, adjoiningMargins: nil, collapsingThrough: false}
	}

	var newChildren []Box
	if header != nil {
		newChildren = append(newChildren, header)
	}
	newChildren = append(newChildren, newTableChildren...)
	if footer != nil {
		newChildren = append(newChildren, footer)
	}
	table_ = bo.CopyWithChildren(table_, newChildren).(bo.TableBoxITF) // CopyWithChildren is type stable
	table = table_.Table()
	table.PartsOverOverflow = overflowUnder
	if collapsedWidth > 0 {
		table.Width = max(table.Width.V()-collapsedWidth, 0)
	}
	for i, v := range table.ColumnGroups {
		table.ColumnGroups[i] = v.Copy().(*bo.TableColumnGroupBox)
	}
	removeEndDecoration := resumeAt != nil && !(collapse && hasFooter)
	table_.RemoveDecoration(&table.BoxFields, removeStartDecoration, removeEndDecoration)
	if collapse {
		table.SkippedRows = skippedRows
	}

	// gopdfgo: gdy tabela ma podaną wysokość większą niż suma wierszy, nadmiar rozdzielamy między wiersze
	// (CSS Tables 3 § 3.10.5); w źródle zostawał jako puste miejsce pod ostatnim wierszem. Tabeli
	// podzielonej między strony nie rozciągamy.
	// gopdfgo: podana wysokość jest ograniczana przez max-height i min-height
	// (CSS 2.1 § 10.7); źródło ich dla tabel nie stosowało. Treść i tak nie
	// pozwala tabeli zejść poniżej sumy wierszy.
	var th pr.Float
	if table.Height != pr.AutoF {
		th = table.Height.V()
	}
	if maxHeight, ok := table.MaxHeight.(pr.Float); ok && maxHeight != pr.Inf {
		th = min(th, maxHeight)
	}
	if minHeight, ok := table.MinHeight.(pr.Float); ok {
		th = max(th, minHeight)
	}
	// gopdfgo: fragment tabeli podzielonej między strony ma wysokość swoich wierszy. Podana wysokość
	// dotyczy całej tabeli, a źródło stosowało ją do każdego fragmentu z osobna, więc fragment wystawał
	// poza stronę, a tabela rosła z każdą stroną (testy druku table-fragmentation-00x). Uproszczenie:
	// nadmiar podanej wysokości ponad wiersze nie jest przenoszony na kolejne strony.
	if resumeAt != nil || skipStack != nil {
		th = 0
	}
	if resumeAt == nil && th > 0 {
		if extra := th - (positionY - table.ContentBoxY()); extra > 0 {
			positionY += distributeExtraTableHeight(table, extra)
		}
	}

	// gopdfgo: odstęp border-spacing pod ostatnim wierszem fragmentu tabeli podzielonej między strony
	// przypada na podział i jest obcinany, gdy wychodzi poza stronę (zob. groupLayout); inaczej fragment
	// wystawał o ten odstęp i cała tabela przechodziła na następną stronę.
	// Tak samo odstęp nad dolnym obramowaniem tabeli, która kończy się przy dole strony.
	if end := table.PaddingBottom.V() + table.BorderBottomWidth.V(); resumeAt == nil && !cloneDecoration {
		if borderSpacingY > 0 && len(newTableChildren) != 0 && context.overflowsPage(outerBottomSpace, positionY+end) &&
			!context.overflowsPage(outerBottomSpace, positionY-borderSpacingY+end) {
			positionY -= borderSpacingY
		}
	} else if resumeAt != nil && borderSpacingY > 0 && len(newTableChildren) != 0 &&
		context.overflowsPage(outerBottomSpace, positionY) && !context.overflowsPage(outerBottomSpace, positionY-borderSpacingY) {
		positionY -= borderSpacingY
	}

	// If the height property has a bigger value, just add blank space
	// below the last row group.
	table.Height = max(th, positionY-table.ContentBoxY())

	// Layout for column groups and columns
	columnsHeight := positionY - initialPositionY
	if len(table.Children) != 0 {
		// The last border spacing is below the columns.
		columnsHeight -= borderSpacingY
	}
	for _, group := range table.ColumnGroups {
		for _, column_ := range group.Children {
			column := column_.Box()
			resolvePercentagesBox(column_, &table.BoxFields)
			if column.GridX < len(table.ColumnPositions) {
				column.PositionX = table.ColumnPositions[column.GridX]
				column.PositionY = initialPositionY
				column.Width = columnWidths[column.GridX]
				column.Height = columnsHeight
			} else {
				// Ignore extra empty columns
				column.PositionX = 0
				column.PositionY = 0
				column.Width = pr.Float(0)
				column.Height = pr.Float(0)
			}
			resolvePercentagesBox(group, &table.BoxFields)
			column.GetCells = getColumnCells(table, column)
		}
		first := group.Children[0].Box()
		last := group.Children[len(group.Children)-1].Box()
		group.PositionX = first.PositionX
		group.PositionY = initialPositionY
		group.Width = last.PositionX + last.Width.V() - first.PositionX
		group.Height = columnsHeight
	}

	// Invert columns for drawing.
	if table.Style.GetDirection() == pr.Rtl {
		slices.Reverse(columnWidths)
		slices.Reverse(table.ColumnPositions)
	}

	avoidBreak := avoidPageBreak(string(table.Style.GetBreakInside()), context)
	if resumeAt != nil && !pageIsEmpty && avoidBreak {
		table_ = nil
		resumeAt = nil
	}

	if traceMode {
		traceLogger.DumpTree(table_, "tableLayout-end")
		traceLogger.Dump(fmt.Sprintf("column positions: %v", table.ColumnPositions))
	}

	return table_, blockLayout{resumeAt: resumeAt, nextPage: nextPage, adjoiningMargins: nil, collapsingThrough: false}
}

// Increase the top padding of a box. This also translates the children.
// distributeExtraTableHeight powiększa wiersze tabeli o łącznie extra pikseli i zwraca rozdzieloną wysokość.
// Nadmiar dostają wiersze bez podanej wysokości (ani własnej, ani komórek), proporcjonalnie do obecnej; gdy takich nie ma albo
// wszystkie mają zerową wysokość, wiersze dzielą go po równo. Komórki rosną razem ze swoim pierwszym
// wierszem, z zachowaniem vertical-align.
func distributeExtraTableHeight(table *bo.TableBox, extra pr.Float) pr.Float {
	var rows, candidates []Box
	for _, group := range table.Children {
		for _, row := range group.Box().Children {
			rows = append(rows, row)
			// gopdfgo: wiersz ma też podaną wysokość, gdy podaje ją któraś z jego komórek; taki wiersz nie
			// dostaje nadmiaru, jeśli są wiersze auto (jak w Chromium; test WPT border-image-repeat-round-003,
			// gdzie środkowy wiersz bez wysokości ma przejąć całą resztę)
			fixed := row.Box().Style.GetHeight().Tag != pr.Auto
			for _, cell := range row.Box().Children {
				if cell.Box().ComputedHeight != pr.AutoF { // styl komórki ma tu już height: auto
					fixed = true
				}
			}
			if !fixed {
				candidates = append(candidates, row)
			}
		}
	}
	if len(rows) == 0 {
		return 0
	}
	if len(candidates) == 0 {
		candidates = rows
	}
	// gopdfgo: najpierw wiersze z wysokością procentową (własną albo komórki o rowspan 1) rosną do
	// swojego procentu wysokości tabeli, w granicach nadmiaru (CSS Tables 3 § 3.10.3, jak Chromium)
	shares := make(map[Box]pr.Float, len(rows))
	if tableHeight := extra; true {
		for _, row := range rows {
			tableHeight += row.Box().Height.V()
		}
		for _, row := range rows {
			if extra <= 0 {
				break
			}
			if percent := rowHeightPercentage(row); percent > 0 {
				grow := min(max(percent/100*tableHeight-row.Box().Height.V(), 0), extra)
				shares[row] += grow
				extra -= grow
			}
		}
	}
	var auto []Box
	for _, row := range candidates {
		if rowHeightPercentage(row) == 0 {
			auto = append(auto, row)
		}
	}
	if len(auto) > 0 {
		candidates = auto
	}
	var totalHeight pr.Float
	for _, row := range candidates {
		totalHeight += row.Box().Height.V()
	}
	for _, row := range candidates {
		if extra <= 0 {
			break
		}
		if totalHeight > 0 {
			shares[row] += extra * row.Box().Height.V() / totalHeight
		} else {
			shares[row] += extra / pr.Float(len(candidates))
		}
	}

	var offset pr.Float
	for _, group_ := range table.Children {
		group := group_.Box()
		group.PositionY += offset
		for _, row_ := range group.Children {
			row := row_.Box()
			if offset != 0 {
				row_.Translate(0, offset, false)
			}
			share := shares[row_]
			if share == 0 {
				continue
			}
			row.Height = row.Height.V() + share
			// gopdfgo: wiersz bez komórek wyrównanych do linii bazowej ma ją na dolnej krawędzi treści
			// komórek, która schodzi razem z dołem wiersza (CSS 2.1 § 17.5.3)
			if baseline, ok := row.Baseline.(pr.Float); ok && !rowHasBaselineCells(row_) {
				row.Baseline = baseline + share
			}
			for _, cell_ := range row.Children {
				cell := cell_.Box()
				switch cell.VerticalAlign {
				case pr.Bottom:
					addTopPadding(cell, share)
				case pr.Middle:
					addTopPadding(cell, share/2)
					cell.PaddingBottom = cell.PaddingBottom.V() + share/2
				default:
					cell.PaddingBottom = cell.PaddingBottom.V() + share
				}
			}
			group.Height = group.Height.V() + share
			offset += share
		}
	}
	return offset
}

// rowHeightPercentage (gopdfgo) podaje procentową wysokość wiersza: większą z jego własnej i jego
// komórek o rowspan 1 (0, gdy żadnej nie ma).
func rowHeightPercentage(row Box) pr.Float {
	var percent pr.Float
	if h := row.Box().Style.GetHeight(); h.Unit == pr.Perc {
		percent = h.Value
	}
	for _, cell := range row.Box().Children {
		if h := cell.Box().Style.GetHeight(); h.Unit == pr.Perc && cell.Box().Rowspan == 1 {
			percent = max(percent, h.Value)
		}
	}
	return percent
}

func addTopPadding(box *bo.BoxFields, extraPadding pr.Float) {
	box.PaddingTop = box.PaddingTop.V() + extraPadding
	for _, child := range box.Children {
		child.Translate(0, extraPadding, false)
	}
}

// Run the fixed table layout and return a list of column widths
// http://www.w3.org/TR/CSS21/tables.html#fixed-table-layout
func fixedTableLayout(box *bo.BoxFields) {
	table := box.GetWrappedTable().Table()
	if table.Width == pr.AutoF {
		panic("table width can't be auto here")
	}
	var allColumns []Box
	for _, columnGroup := range table.ColumnGroups {
		allColumns = append(allColumns, columnGroup.Box().Children...)
	}

	var firstRowCells []Box
	if len(table.Children) != 0 && len(table.Children[0].Box().Children) != 0 {
		firstRowgroup := table.Children[0].Box()
		firstRowCells = firstRowgroup.Children[0].Box().Children
	}
	var sum int
	for _, cell := range firstRowCells {
		sum += cell.Box().Colspan
	}
	numColumns := max(len(allColumns), sum)
	// ``None`` means not know yet.
	columnWidths := make([]pr.MaybeFloat, numColumns)

	var borderSpacingX pr.Float
	if table.Style.GetBorderCollapse() == "separate" {
		borderSpacingX = table.Style.GetBorderSpacing()[0].Value
	}

	// `width` on column boxes
	// gopdfgo: procent szerokości kolumny odnosi się do szerokości tabeli bez
	// odstępów między komórkami (CSS Tables 3 § 3.9.3); źródło liczyło go od
	// całej szerokości, więc kolumny procentowe wychodziły za szerokie
	percentageBase := max(0, table.Width.V()-borderSpacingX*pr.Float(numColumns+1))
	for i, column_ := range allColumns {
		column := column_.Box()
		column.Width = resolveOnePercentage(column.Style.GetWidth(), pr.PWidth, percentageBase)
		if column.Width != pr.AutoF {
			columnWidths[i] = column.Width
		}
	}

	// `width` on cells of the first row.
	i := 0
	for _, cell_ := range firstRowCells {
		cell := cell_.Box()
		resolvePercentagesBox(cell_, &table.BoxFields)
		if cell.Width != pr.AutoF {
			width := cell.BorderWidth()
			width -= borderSpacingX * pr.Float(cell.Colspan-1)
			// In the general case, this width affects several columns (through
			// colspan) some of which already have a width. Subtract these
			// known widths and divide among remaining columns.
			var columnsWithoutWidth []int // and occupied by this cell
			for j := i; j < i+cell.Colspan; j++ {
				if columnWidths[j] == nil {
					columnsWithoutWidth = append(columnsWithoutWidth, j)
				} else {
					width -= columnWidths[j].V()
				}
			}
			if len(columnsWithoutWidth) != 0 {
				widthPerColumn := width / pr.Float(len(columnsWithoutWidth))
				for _, j := range columnsWithoutWidth {
					columnWidths[j] = widthPerColumn
				}
			}
		}
		i += cell.Colspan
	}

	// Distribute the remaining space equally on columns that do not have
	// a width yet.
	allBorderSpacing := borderSpacingX * pr.Float(numColumns+1)
	var columnsWithoutWidth []int
	minTableWidth := allBorderSpacing
	for i, w := range columnWidths {
		if w != nil {
			minTableWidth += w.V()
		} else {
			columnsWithoutWidth = append(columnsWithoutWidth, i)
		}
	}
	if len(columnsWithoutWidth) != 0 && table.Width.V() >= minTableWidth {
		remainingWidth := table.Width.V() - minTableWidth
		widthPerColumn := remainingWidth / pr.Float(len(columnsWithoutWidth))
		for _, i := range columnsWithoutWidth {
			columnWidths[i] = widthPerColumn
		}
	} else {
		// XXX this is bad, but we were given a broken table to work with...
		for _, i := range columnsWithoutWidth {
			columnWidths[i] = pr.Float(0)
		}
	}
	outCW := make([]pr.Float, len(columnWidths))
	var sumColumnWidths pr.Float
	for i, v := range columnWidths {
		sumColumnWidths += v.V()
		outCW[i] = v.V()
	}
	// If the sum is less than the table width,
	// distribute the remaining space equally
	extraWidth := table.Width.V() - sumColumnWidths - allBorderSpacing
	if extraWidth <= 0 {
		// substract a negative: widen the table
		table.Width = table.Width.V() - extraWidth
	} else if numColumns != 0 {
		// gopdfgo: nadwyżkę dostają kolumny proporcjonalnie do szerokości (CSS Tables 3 § 3.9.4
		// „Distributing excess width to columns”: w układzie stałym wszystkie kolumny mają szerokość,
		// więc rozdział proporcjonalny do niej; jak Chrome 157, pomiar 2026-11-21 na tabeli e-SF
		// z kolumnami 5em, 15em i 13ch). Źródło dzieliło po równo, co zostaje, gdy szerokości są zerowe.
		snap := snapsLayoutUnits(table.Style)
		if sumColumnWidths > 0 {
			var given pr.Float
			for i, w := range outCW {
				share := extraWidth * w / sumColumnWidths
				if snap {
					share = pr.LayoutUnit(share)
				}
				if i == len(outCW)-1 {
					share = extraWidth - given // reszta z obcięcia do ostatniej kolumny
				}
				given += share
				outCW[i] = w + share
			}
		} else {
			extraPerColumn := extraWidth / pr.Float(numColumns)
			for i, w := range outCW {
				outCW[i] = w + extraPerColumn
			}
		}
	}

	// Now we have table.Width == sum(columnWidths) + allBorderSpacing
	// with possible floating point rounding errors.
	// (unless there is zero column)
	table.ColumnWidths = outCW
}

func sum(l []pr.Float) pr.Float {
	var out pr.Float
	for _, v := range l {
		out += v
	}
	return out
}

// Run the auto table layout and return a list of column widths.
// http://www.w3.org/TR/CSS21/tables.html#auto-table-layout
func autoTableLayout(context *layoutContext, box_ Box, containingBlock bo.Point) {
	box := box_.Box()
	table_ := box.GetWrappedTable()
	table := table_.Table()
	tmp := tableAndColumnsPreferredWidths(context, box_, false)
	var margins pr.Float
	if box.MarginLeft != pr.AutoF {
		margins += box.MarginLeft.V()
	}
	if box.MarginRight != pr.AutoF {
		margins += box.MarginRight.V()
	}
	paddings := table.PaddingLeft.V() + table.PaddingRight.V()
	borders := table.BorderLeftWidth.V() + table.BorderRightWidth.V()

	cbWidth := containingBlock[0]
	availableWidth := cbWidth - margins - paddings - borders

	if intrinsic := table.Style.GetWidth().Intrinsic; table.Width == pr.AutoF && intrinsic != pr.IntrinsicNone {
		// gopdfgo: width: min-content, max-content i fit-content tabeli (CSS Sizing 3 § 3.1); źródło
		// traktowało je jak auto, więc tabela z komórkami width: 100% wypełniała blok zawierający.
		// Przy max-content wkład kolumn procentowych się nie liczy (zob. tableAndColumnsPreferredWidths).
		switch intrinsic {
		case pr.IntrinsicMinContent:
			table.Width = tmp.tableMinContentWidth
		case pr.IntrinsicMaxContent:
			table.Width = tmp.tableMaxContentWidth
		default:
			table.Width = max(tmp.tableMinContentWidth, min(availableWidth, tmp.layoutMaxContentWidth))
		}
	} else if table.Width == pr.AutoF {
		if availableWidth <= tmp.tableMinContentWidth {
			table.Width = tmp.tableMinContentWidth
		} else if availableWidth < tmp.layoutMaxContentWidth {
			table.Width = availableWidth
		} else {
			// gopdfgo: przy układzie wkład kolumn procentowych liczy się zawsze (zob. layoutMaxContentWidth)
			table.Width = tmp.layoutMaxContentWidth
		}
	} else {
		// gopdfgo: dolną granicą jest szerokość minimalna treści (i min-width), a nie podana szerokość
		// (CSS Tables 3 § 3.9.1); ma to znaczenie, gdy szerokość tabeli narzuca układ flex. Źródło
		// porównywało z wartością zawierającą width, więc tabela z width: 500px i flex-basis: 100px
		// miała 500 px.
		lower := tmp.tableMinContentWidth
		if table.IsFlexItem || box.IsFlexItem {
			lower = tmp.gridMinWidth
			if minWidth, ok := table.MinWidth.(pr.Float); ok {
				lower = max(lower, minWidth)
			}
		}
		if table.Width.V() < lower {
			table.Width = lower
		}
	}

	if len(tmp.grid) == 0 {
		table.ColumnWidths = nil
		return
	}

	assignableWidth := table.Width.V() - tmp.totalHorizontalBorderSpacing
	minContentGuess := append([]pr.Float{}, tmp.columnMinContentWidths...)
	minContentPercentageGuess := append([]pr.Float{}, tmp.columnMinContentWidths...)
	minContentSpecifiedGuess := append([]pr.Float{}, tmp.columnMinContentWidths...)
	maxContentGuess := append([]pr.Float{}, tmp.columnMaxContentWidths...)
	L := 4
	guesses := [4]*[]pr.Float{
		&minContentGuess, &minContentPercentageGuess,
		&minContentSpecifiedGuess, &maxContentGuess,
	}
	// https://www.w3.org/TR/css-tables-3/#width-distribution-algorithm
	for i := range tmp.grid {
		if tmp.columnIntrinsicPercentages[i] != 0 {
			minContentPercentageGuess[i] = max(
				tmp.columnIntrinsicPercentages[i]/100*assignableWidth,
				tmp.columnMinContentWidths[i])
			minContentSpecifiedGuess[i] = minContentPercentageGuess[i]
			maxContentGuess[i] = minContentPercentageGuess[i]
		} else if tmp.constrainedness[i] {
			// any other column that is constrained is assigned
			// its max-content width
			minContentSpecifiedGuess[i] = tmp.columnMaxContentWidths[i]
		}
	}

	if assignableWidth < sum(maxContentGuess) {
		// Default values shouldn't be used, but we never know.
		// See https://github.com/Kozea/WeasyPrint/issues/770
		lowerGuess := guesses[0]
		upperGuess := guesses[L-1]

		// We have to work around floating point rounding errors here.
		// The 1e-9 value comes from PEP 485.
		for _, guess := range guesses {
			if sum(*guess) <= assignableWidth*(1+1e-9) {
				lowerGuess = guess
			} else {
				break
			}
		}
		for i := range guesses {
			guess := guesses[L-1-i]
			if sum(*guess) >= assignableWidth*(1-1e-9) {
				upperGuess = guess
			} else {
				break
			}
		}
		if upperGuess == lowerGuess {
			table.ColumnWidths = *upperGuess
		} else {
			addedWidths := make([]pr.Float, len(tmp.grid))
			var sl, saw, availableRatio pr.Float
			for i := range tmp.grid {
				addedWidths[i] = (*upperGuess)[i] - (*lowerGuess)[i]
				sl += (*lowerGuess)[i]
				saw += addedWidths[i]
			}
			if saw != 0 {
				availableRatio = (assignableWidth - sl) / saw
			}
			cw := make([]pr.Float, len(tmp.grid))
			for i := range tmp.grid {
				cw[i] = (*lowerGuess)[i] + addedWidths[i]*availableRatio
			}
			table.ColumnWidths = cw
		}
	} else {
		table.ColumnWidths = maxContentGuess
		excessWidth := assignableWidth - sum(maxContentGuess)
		distributeExcessWidth(tmp.grid, excessWidth, table.ColumnWidths, tmp.constrainedness,
			tmp.columnIntrinsicPercentages, tmp.columnMaxContentWidths, [2]int{0, len(tmp.grid)})
	}
}

// Find the width of each column and derive the wrapper width.
func tableWrapperWidth(context *layoutContext, wrapper_ Box, containingBlock bo.MaybePoint) {
	tableWrapperWidthForced(context, wrapper_, containingBlock, -1)
}

// tableWrapperWidthForced: forced >= 0 to szerokość pudełka obramowania tabeli narzucona z zewnątrz
// (element flex); tabela i tak nie zejdzie poniżej szerokości minimalnej treści.
func tableWrapperWidthForced(context *layoutContext, wrapper_ Box, containingBlock bo.MaybePoint, forced pr.Float) {
	wrapper := wrapper_.Box()
	table := wrapper.GetWrappedTable()
	resolvePercentages(table, containingBlock)
	if forced >= 0 {
		// gopdfgo: rozmiar z układu flex to szerokość pudełka opakowującego, czyli pudełka obramowania
		// tabeli; podstawa flex-basis tabeli z box-sizing: content-box jest do niego przeliczana w układzie
		// flex (zob. tableFrame). Wcześniej odejmowaliśmy tu obramowanie tylko przy border-box, więc tabela
		// w kolumnie flex rosła przy każdym układzie o swoje obramowanie.
		tb := table.Box()
		forced -= tb.PaddingLeft.V() + tb.PaddingRight.V() + tb.BorderLeftWidth.V() + tb.BorderRightWidth.V()
		tb.Width = max(forced, 0)
	}

	// gopdfgo: min-width i max-width tabeli (CSS 2.1 § 10.4, CSS Tables 3
	// § 3.9.1). Źródło je pomijało: tabela z width: 3in i max-width: 1in
	// miała trzy cale. Podana szerokość jest ograniczana przed układem;
	// szerokość automatyczna po nim, z ponownym układem w razie przekroczenia
	// (tabela i tak nie zejdzie poniżej szerokości minimalnej treści).
	tableBox := table.Box()
	clamp := func(width pr.Float) pr.Float {
		if maxWidth, ok := tableBox.MaxWidth.(pr.Float); ok && maxWidth != pr.Inf {
			width = min(width, maxWidth)
		}
		if minWidth, ok := tableBox.MinWidth.(pr.Float); ok {
			width = max(width, minWidth)
		}
		return width
	}
	layout := func() {
		if tableBox.Style.GetTableLayout() == "fixed" && tableBox.Width != pr.AutoF {
			fixedTableLayout(wrapper)
		} else {
			autoTableLayout(context, wrapper_, containingBlock.V())
		}
	}
	if width, ok := tableBox.Width.(pr.Float); ok {
		tableBox.Width = clamp(width)
		layout()
	} else {
		layout()
		if width, ok := tableBox.Width.(pr.Float); ok && clamp(width) != width {
			// układ automatyczny sam pilnuje szerokości minimalnej treści
			tableBox.Width = clamp(width)
			layout()
		}
	}
	wrapper.Width = tableBox.BorderWidth()
}

// rowHasBaselineCells (gopdfgo) podaje, czy w wierszu jest komórka wyrównana do linii bazowej.
func rowHasBaselineCells(row Box) bool {
	for _, cell := range row.Box().Children {
		if cell.Box().VerticalAlign == pr.Baseline && cellHasInFlowContent(cell) {
			return true
		}
	}
	return false
}

// cellHasInFlowContent (gopdfgo) podaje, czy komórka ma treść w przepływie (inną niż pusty wiersz).
func cellHasInFlowContent(cell Box) bool {
	for _, child := range cell.Box().Children {
		if !child.Box().IsInNormalFlow() {
			continue
		}
		if line, isLine := child.(*bo.LineBox); isLine && isEmptyLinebox(line) {
			continue
		}
		return true
	}
	return false
}

// Return the y position of a cell’s baseline from the top of its border box.
// See http://www.w3.org/TR/CSS21/tables.html#height-layout
func cellBaseline(cell Box) pr.Float {
	result := findInFlowBaseline(cell, false, bo.LineT, bo.TableRowT)
	if result != nil {
		return result.V() - cell.Box().PositionY
	} else {
		// Default to the bottom of the content area.
		return cell.Box().BorderTopWidth.V() + cell.Box().PaddingTop.V() + cell.Box().Height.V()
	}
}

// Return the absolute Y position for the first (or last) in-flow baseline
// if any or nil. Can't return "auto".
// [last] defaults to false, baselinesT to nothing
func findInFlowBaseline(box Box, last bool, baselinesT ...bo.BoxType) pr.MaybeFloat {
	return findBaseline(box, last, true, baselinesT...)
}

// findBaseline: z flexBaseline blokowy kontener flex w wierszu zwraca własną linię bazową.
func findBaseline(box Box, last, flexBaseline bool, baselinesT ...bo.BoxType) pr.MaybeFloat {
	if len(baselinesT) == 0 {
		baselinesT = []bo.BoxType{bo.LineT}
	}
	// TODO: synthetize baseline when needed
	// See https://www.w3.org/TR/css-align-3/#synthesize-baseline
	for _, type_ := range baselinesT { // if isinstance(box, baselinesT)
		if type_.IsInstance(box) {
			return box.Box().PositionY + box.Box().Baseline.V()
		}
	}
	if bo.TableCaptionT.IsInstance(box) {
		return nil
	}
	// gopdfgo: kontener siatki ma własne linie bazowe wyznaczane z elementów pierwszego (ostatniego)
	// wiersza w porządku siatki (CSS Grid 1 § 10.7), zapisane przez gridLayout. Źródło schodziło do
	// dzieci w kolejności dokumentu, więc brało linię pierwszego (ostatniego) dziecka, a nie elementu
	// z pierwszego (ostatniego) wiersza ani wspólnej linii elementów wyrównanych do linii bazowej.
	if bo.GridT.IsInstance(box) || bo.InlineGridT.IsInstance(box) {
		offset := box.Box().GridFirstBaseline
		if last {
			offset = box.Box().GridLastBaseline
		}
		if offset, ok := offset.(pr.Float); ok {
			return box.Box().PositionY + offset
		}
		return nil
	}
	// gopdfgo: blokowy kontener flex w wierszu ma własną linię bazową (zob. flexLayout)
	if baseline, ok := box.Box().Baseline.(pr.Float); ok && !last && flexBaseline && bo.FlexT.IsInstance(box) &&
		strings.HasPrefix(string(box.Box().Style.GetFlexDirection()), "row") {
		return box.Box().ContentBoxY() + baseline
	}

	children := box.Box().Children
	if last {
		children = reversedBoxes(children)
	}
	for _, child := range children {
		// gopdfgo: pusty wiersz (np. reszta pudełka liniowego za wydzielonym z niego blokiem) nie ma linii
		// bazowej (CSS 2.1 § 9.4.2: taki wiersz traktuje się jak nieistniejący; tak robi Chromium); w źródle
		// inline-block stawał na linii bazowej takiego wiersza (test WPT block-in-inline-baseline-001).
		if line, isLine := child.(*bo.LineBox); isLine && isEmptyLinebox(line) {
			continue
		}
		if child.Box().IsInNormalFlow() {
			result := findBaseline(child, last, flexBaseline, baselinesT...)
			if result != nil {
				return result
			}
			// gopdfgo: blok z overflow innym niż visible bez wierszy w środku ma przy szukaniu ostatniej
			// linii bazowej linię bazową na dolnej krawędzi marginesu (jak samo inline-block z overflow,
			// CSS 2.1 § 10.8.1; tak robi Chromium, test WPT baseline-block-with-overflow-001); w źródle
			// szukanie szło dalej, do wcześniejszych pudełek.
			// Nie dotyczy elementu siatki ani flex: ich linię bazową syntetyzuje kontener z krawędzi
			// obramowania (CSS Align 3 § 9.1, test WPT grid-baseline-003).
			if last && bo.BlockT.IsInstance(child) && child.Box().Style.GetOverflow() != "visible" &&
				!child.Box().IsGridItem && !child.Box().IsFlexItem {
				return child.Box().PositionY + child.Box().MarginHeight()
			}
		}
	}
	// gopdfgo: kontener flex w wierszu, którego elementy nie mają linii bazowej, ma ją syntetyzowaną
	// z dolnej krawędzi obramowania pierwszego (ostatniego) elementu (CSS Flexbox 1 § 8.5); źródło
	// zwracało wtedy brak linii bazowej, więc np. inline-block z takim kontenerem stał na dolnej
	// krawędzi swojego marginesu.
	if flexBaseline && (bo.FlexT.IsInstance(box) || bo.InlineFlexT.IsInstance(box)) &&
		strings.HasPrefix(string(box.Box().Style.GetFlexDirection()), "row") {
		for _, child := range children {
			if child.Box().IsInNormalFlow() {
				return child.Box().BorderBoxY() + child.Box().BorderHeight()
			}
		}
	}
	return nil
}

// Distribute available width to columns.
//
// See https://www.w3.org/TR/css-tables-3/#distributing-width-to-columns
func distributeExcessWidth(grid [][]bo.Box, excessWidth pr.Float, columnWidths []pr.Float,
	constrainedness []bool, columnIntrinsicPercentages, columnMaxContentWidths []pr.Float, columnSlice [2]int,
) {
	// First group
	var columns []int
	for i_ := range grid[columnSlice[0]:columnSlice[1]] {
		i := i_ + columnSlice[0]
		if !constrainedness[i] && columnIntrinsicPercentages[i] == 0 && columnMaxContentWidths[i] > 0 {
			columns = append(columns, i)
		}
	}
	if len(columns) != 0 {
		sumMaxContentWidths := pr.Float(0)
		for _, i := range columns {
			sumMaxContentWidths += columnMaxContentWidths[i]
		}
		ratio := excessWidth / sumMaxContentWidths
		for _, i := range columns {
			columnWidths[i] += columnMaxContentWidths[i] * ratio
		}
		return
	}

	// Second group
	columns = columns[:0]
	for i_ := range grid[columnSlice[0]:columnSlice[1]] {
		i := i_ + columnSlice[0]
		if !constrainedness[i] && columnIntrinsicPercentages[i] == 0 {
			columns = append(columns, i)
		}
	}
	if l := pr.Float(len(columns)); l != 0 {
		for _, i := range columns {
			columnWidths[i] += excessWidth / l
		}
		return
	}

	// Third group
	columns = columns[:0]
	for i_ := range grid[columnSlice[0]:columnSlice[1]] {
		i := i_ + columnSlice[0]
		if constrainedness[i] && columnIntrinsicPercentages[i] == 0 && columnMaxContentWidths[i] > 0 {
			columns = append(columns, i)
		}
	}
	if len(columns) != 0 {
		sumMaxContentWidths := pr.Float(0)
		for _, i := range columns {
			sumMaxContentWidths += columnMaxContentWidths[i]
		}
		ratio := excessWidth / sumMaxContentWidths
		for _, i := range columns {
			columnWidths[i] += columnMaxContentWidths[i] * ratio
		}
		return
	}

	// Fourth group
	columns = columns[:0]
	for i_ := range grid[columnSlice[0]:columnSlice[1]] {
		i := i_ + columnSlice[0]
		if columnIntrinsicPercentages[i] > 0 && columnMaxContentWidths[i] > 0 {
			columns = append(columns, i)
		}
	}
	if len(columns) != 0 {
		sumIntrinsicPercentages := pr.Float(0)
		for _, i := range columns {
			sumIntrinsicPercentages += columnIntrinsicPercentages[i]
		}
		ratio := excessWidth / sumIntrinsicPercentages
		for _, i := range columns {
			columnWidths[i] += columnIntrinsicPercentages[i] * ratio
		}
		return
	}

	// Fifth group
	columns = columns[:0]
	for i_, column := range grid[columnSlice[0]:columnSlice[1]] {
		i := i_ + columnSlice[0]
		if len(column) != 0 {
			columns = append(columns, i)
		}
	}
	if L := pr.Float(len(columns)); L != 0 {
		for _, i := range columns {
			columnWidths[i] += excessWidth / L
		}
		return
	}

	// Sixth group
	columns = columns[:0]
	for i_ := range grid[columnSlice[0]:columnSlice[1]] {
		i := i_ + columnSlice[0]
		columns = append(columns, i)
	}
	for _, i := range columns {
		columnWidths[i] += excessWidth / pr.Float(len(columns))
	}
}

// markTablesWithoutColumnPercentages (gopdfgo) ustawia TableBox.NoColumnPercentages tabelom, których
// przodkiem (w łańcuchu bloków zawierających, uproszczonym do rodziców, z position: fixed jako
// początkiem łańcucha) jest komórka tabeli, kontener flex albo siatki; tak działa
// TableNode::AllowColumnPercentages w Chromium.
func markTablesWithoutColumnPercentages(box Box, inside bool) {
	if box.Box().Style != nil && box.Box().Style.GetPosition().String == "fixed" {
		inside = false
	}
	if table, ok := box.(bo.TableBoxITF); ok {
		table.Table().NoColumnPercentages = inside
	}
	switch box.(type) {
	case bo.FlexContainerBoxITF, bo.GridContainerBoxITF:
		inside = true
	}
	if bo.TableCellT.IsInstance(box) {
		inside = true
	}
	if parent, ok := box.(bo.ParentBoxITF); ok {
		for _, child := range parent.Box().Children {
			markTablesWithoutColumnPercentages(child, inside)
		}
	}
}

// repeatFragmentainerHeight zwraca wysokość fragmentatora (kolumny albo obszaru strony), względem której
// tableLayout ocenia powtarzanie nagłówka i stopki (CSS Tables 3 § 6.2), albo 0, gdy treść nie jest
// dzielona na fragmenty (tryb ciągły, pomiary bez ograniczenia wysokości).
func repeatFragmentainerHeight(context *layoutContext, bottomSpace pr.Float) pr.Float {
	if bottomSpace == -pr.Inf || bottomSpace == pr.Inf || context.pageBottom == pr.Inf || context.flexMeasuring > 0 {
		return 0
	}
	if context.inColumn {
		return context.columnHeight
	}
	return context.pageHeight
}

// overflowUnderRepeatedParts (gopdfgo): treść niepodzielna (zob. canSplitFixedHeight, bo.IsMonolithic) wyższa
// niż strona, krojona w komórce tabeli z powtarzanym nagłówkiem lub stopką, wylewa się pod nie tak jak
// w Chromium: wycinek na stronie, na której treść się urywa, sięga do dołu strony pod stopką, a na
// stronie z dalszym ciągiem zaczyna się u góry fragmentu tabeli, pod nagłówkiem. Nagłówek i stopka są
// wtedy malowane na wycinku (TableBox.PartsOverOverflow, zob. document.drawTable). CSS Break 3 § 4.4
// zostawia miejsce przepełnienia treści niepodzielnej przeglądarce, a CSS Tables 3 § 6.2 nie mówi, jak
// powtarzany nagłówek i stopka mają się do niej mieć (testy WPT css-page/monolithic-overflow-016, -017,
// -023). Wcześniej wycinek kończył się nad stopką i zaczynał pod nagłówkiem, więc treść zajmowała o ich
// wysokość więcej stron, a reszta krótsza niż nagłówek odsuwała następne wiersze.
//
// Część pod nagłówkiem odjął już od reszty przed układem behindHeader (remainders: reszty przed odjęciem,
// według elementu); tu wycinki są przesuwane w górę o headerHeight, a te ucięte na końcu strony
// wydłużane do pageEnd, o co zmniejsza się reszta w stosie wznowienia resumeAt. Zwraca nowy stos i to, czy
// coś leży pod nagłówkiem lub stopką.
func overflowUnderRepeatedParts(context *layoutContext, table_ bo.TableBoxITF, groups []Box,
	remainders map[*html.Node]pr.Float, headerHeight pr.Float, hasFooter bool, resumeAt tree.ResumeStack, pageEnd pr.Float,
) (tree.ResumeStack, bool) {
	under := false
	for element, remaining := range remainders {
		fragment := findMonolithicFragment(groups, element)
		if fragment == nil || headerHeight <= 0 {
			continue
		}
		box := fragment.Box()
		height := box.Height.V()
		fragment.Translate(0, -headerHeight, false)
		if remaining > headerHeight {
			box.Height = height + headerHeight
		} else {
			// reszta mieści się w całości pod nagłówkiem; w układzie została jej najmniejsza możliwa część
			box.Height = remaining
		}
		under = true
	}
	if hasFooter && resumeAt != nil {
		resumeAt = mapMonolithicRemainders(context, table_, resumeAt, func(source Box, remaining float64) float64 {
			fragment := findMonolithicFragment(groups, source.Box().Element)
			if fragment == nil {
				return remaining
			}
			box := fragment.Box()
			extension := pageEnd - (box.BorderBoxY() + box.BorderHeight())
			if extension <= 0 {
				return remaining
			}
			box.Height = box.Height.V() + extension
			under = true
			return remaining - float64(extension)
		})
	}
	return resumeAt, under
}

// findMonolithicFragment szuka w poddrzewach boxes fragmentu pudełka niepodzielnego elementu element.
func findMonolithicFragment(boxes []Box, element *html.Node) Box {
	if element == nil {
		return nil
	}
	for _, box := range boxes {
		if box.Box().Element == element && bo.IsMonolithic(box) && bo.BlockT.IsInstance(box) {
			return box
		}
		if found := findMonolithicFragment(box.Box().Children, element); found != nil {
			return found
		}
	}
	return nil
}

// mapMonolithicRemainders (gopdfgo) zwraca stos wznowienia stack pudełka box, w którym reszta wysokości
// (tree.RemainderResume) każdego pudełka niepodzielnego jest zastąpiona wynikiem f, nie mniejszym niż
// najmniejsza zapisywalna reszta (reszta zero oznaczałaby całą wysokość). Mapy na drodze do zmienionych
// reszt są kopiowane razem z wpisami kontekstu kluczowanymi tożsamością stosu (zob. resumeKey); reszta
// stosu jest wspólna z oryginałem, który pozostaje bez zmian.
func mapMonolithicRemainders(context *layoutContext, box Box, stack tree.ResumeStack, f func(Box, float64) float64) tree.ResumeStack {
	if len(stack) == 0 {
		return stack
	}
	if remaining, ok := stack.Remainder(); ok {
		if !bo.IsMonolithic(box) || !bo.BlockT.IsInstance(box) {
			return stack
		}
		mapped := f(box, remaining)
		if mapped == remaining {
			return stack
		}
		out := tree.RemainderResume(max(mapped, 1.0/64))
		copyResumeKeys(context, stack, out)
		return out
	}
	if _, _, ok := stack.Slice(); ok {
		return stack
	}
	if inner, ok := stack.Overflow(); ok {
		mapped := mapMonolithicRemainders(context, box, inner, f)
		if resumeKey(mapped) == resumeKey(inner) {
			return stack
		}
		out := tree.OverflowResume(mapped)
		copyResumeKeys(context, stack, out)
		return out
	}
	children := box.Box().Children
	var out tree.ResumeStack
	for k, sub := range stack {
		if k < 0 || k >= len(children) || sub == nil {
			continue
		}
		mapped := mapMonolithicRemainders(context, children[k], sub, f)
		if resumeKey(mapped) == resumeKey(sub) {
			continue
		}
		if out == nil {
			out = maps.Clone(stack)
			copyResumeKeys(context, stack, out)
		}
		out[k] = mapped
	}
	if out == nil {
		return stack
	}
	return out
}

// copyResumeKeys przenosi na kopię to stosu from wpisy kontekstu kluczowane tożsamością stosu.
func copyResumeKeys(context *layoutContext, from, to tree.ResumeStack) {
	old, key := resumeKey(from), context.keepResumeKey(to)
	for _, m := range []map[uintptr]pr.Float{context.fixedHeightRemaining, context.minHeightRemaining,
		context.cellHeightRemaining, context.flexPushOffset} {
		if v, ok := m[old]; ok {
			m[key] = v
		}
	}
}

// spanResumeKey (gopdfgo) to klucz w stosie wznowienia wiersza, pod którym są dalsze ciągi komórek
// rozpiętych (rowspan) z wcześniejszych wierszy grupy: wartością jest stos {wiersz: {komórka: wznowienie
// treści komórki}} z indeksami wiersza, w którym komórka się zaczyna, i komórki w tym wierszu. Indeksy
// własnych komórek wiersza są nieujemne, a ujemne klucze stosów bloków (tree.RemainderResume, SliceResume)
// nie występują na poziomie wiersza.
const spanResumeKey = math.MinInt32 + 1

// spanState (gopdfgo) opisuje ułożoną komórkę rozpiętą na kilka wierszy albo dalszy ciąg takiej komórki.
type spanState struct {
	row, cell int              // wiersz, w którym komórka się zaczyna, i jej indeks w tym wierszu
	own       int              // indeks komórki w bieżącym wierszu albo -1 dla dalszego ciągu
	resume    tree.ResumeStack // wznowienie treści, gdy nie zmieściła się do końca strony
	top       bool             // fragment komórki dzielonej: bez wyrównania w pionie
	stalled   bool             // z treści nic nie zmieściło się na tej stronie
}

// cellEntry (gopdfgo) to komórka układana w wierszu: własna (own ≥ 0) albo dalszy ciąg komórki rozpiętej
// z wcześniejszego wiersza (own = -1).
type cellEntry struct {
	box                   Box
	own                   int
	originRow, originCell int
	skip                  tree.ResumeStack
}

// splitRowResume (gopdfgo) rozdziela stos wznowienia wiersza na wznowienia własnych komórek (nil, gdy
// wiersz nie jest dzielony) i dalsze ciągi komórek rozpiętych z wcześniejszych wierszy (zob. spanResumeKey).
func splitRowResume(stack tree.ResumeStack) (own, spanning tree.ResumeStack) {
	spanning, has := stack[spanResumeKey]
	if !has {
		return stack, nil
	}
	for k, v := range stack {
		if k != spanResumeKey {
			if own == nil {
				own = tree.ResumeStack{}
			}
			own[k] = v
		}
	}
	return own, spanning
}

// rowCellEntries (gopdfgo) zwraca komórki do ułożenia w wierszu row_ o indeksie indexRow: jego własne
// komórki ze wznowieniem z rowStack oraz, na początku strony, dalsze ciągi komórek rozpiętych z wierszy
// ułożonych na poprzednich stronach. Dalszy ciąg to kopia komórki źródłowej z rowspan zmniejszonym do
// wierszy, które zostały; komórki są uporządkowane według kolumn.
func rowCellEntries(group *bo.BoxFields, row_ Box, indexRow int, rowStack tree.ResumeStack) []cellEntry {
	own, spanning := splitRowResume(rowStack)
	row := row_.Box()
	entries := make([]cellEntry, 0, len(row.Children))
	for i, cell := range row.Children {
		entry := cellEntry{box: cell, own: i, originRow: indexRow, originCell: i}
		if len(own) != 0 {
			if rs, has := own[i]; has {
				entry.skip = rs
			} else {
				entry.skip = tree.ResumeStack{len(cell.Box().Children): nil}
			}
		}
		entries = append(entries, entry)
	}
	if len(spanning) == 0 {
		return entries
	}
	for _, p := range slices.Sorted(maps.Keys(spanning)) {
		if p < 0 || p >= indexRow || p >= len(group.Children) {
			continue
		}
		origin := group.Children[p].Box()
		for _, c := range slices.Sorted(maps.Keys(spanning[p])) {
			if c < 0 || c >= len(origin.Children) {
				continue
			}
			source := origin.Children[c]
			remaining := p + source.Box().Rowspan - indexRow
			if remaining < 1 {
				continue
			}
			continuation := source.Copy()
			continuation.Box().Rowspan = remaining
			continuation.Box().SpanOriginRow = group.Children[p]
			skip := spanning[p][c]
			if skip == nil {
				skip = tree.ResumeStack{len(source.Box().Children): nil}
			}
			entries = append(entries, cellEntry{box: continuation, own: -1, originRow: p, originCell: c, skip: skip})
		}
	}
	slices.SortStableFunc(entries, func(a, b cellEntry) int { return a.box.Box().GridX - b.box.Box().GridX })
	return entries
}

// continueSpanningCells (gopdfgo): komórka rozpięta na wiersze, z których część przechodzi na następną
// stronę, jest dzielona razem z nimi (CSS Tables 3 § 6, CSS Fragmentation 3 § 5.4): jej fragment na tej
// stronie sięga dołu ostatniego ułożonego wiersza, bez dolnego obramowania i odstępu (box-decoration-break:
// slice), a do stosu wznowienia wiersza, od którego zaczyna się następna strona, trafia jej dalszy ciąg
// z wznowieniem treści (zob. spanResumeKey). W źródle komórka miała tylko wysokość swojej treści, a na
// następnej stronie znikała, razem z treścią, która nie zmieściła się na tej.
func continueSpanningCells(context *layoutContext, group *bo.BoxFields, rows []Box, spans map[Box]spanState, resumeAt tree.ResumeStack) tree.ResumeStack {
	if len(spans) == 0 || len(resumeAt) != 1 {
		return resumeAt
	}
	nextRow, rowStack := resumeAt.Unpack()
	last := rows[len(rows)-1].Box()
	bottom := last.PositionY + last.Height.V()
	for _, row_ := range rows {
		for _, cell_ := range row_.Box().Children {
			state, ok := spans[cell_]
			cell := cell_.Box()
			if !ok || row_.Box().Index+cell.Rowspan-1 < nextRow {
				continue
			}
			if state.row < nextRow {
				resume := state.resume
				if resume == nil {
					resume = tree.ResumeStack{len(group.Children[state.row].Box().Children[state.cell].Box().Children): nil}
					// komórka o podanej wysokości: fragment kończy się z wierszami, reszta wysokości przechodzi dalej
					if computed, ok := cell.ComputedHeight.(pr.Float); ok && cell.Height.V() > cell.ContentHeight {
						if used := max(cell.ContentHeight, bottom-cell.ContentBoxY()); used < cell.Height.V() {
							cell.Height = used
							if remaining := computed - used; remaining > 0 {
								if context.cellHeightRemaining == nil {
									context.cellHeightRemaining = map[uintptr]pr.Float{}
								}
								context.cellHeightRemaining[context.keepResumeKey(resume)] = remaining
							}
						}
					}
				}
				if rowStack == nil {
					rowStack = tree.ResumeStack{}
					resumeAt[nextRow] = rowStack
				}
				spanning := rowStack[spanResumeKey]
				if spanning == nil {
					spanning = tree.ResumeStack{}
					rowStack[spanResumeKey] = spanning
				}
				if spanning[state.row] == nil {
					spanning[state.row] = tree.ResumeStack{}
				}
				spanning[state.row][state.cell] = resume
			}
			if !cell.RemoveDecorationSides[bo.SBottom] {
				cell_.RemoveDecoration(cell, false, true)
			}
			if extra := bottom - (cell.PositionY + cell.BorderHeight()); extra > 0 {
				cell.PaddingBottom = cell.PaddingBottom.V() + extra
			}
		}
	}
	return resumeAt
}

// lastRowBottom (gopdfgo) podaje dół ostatniego ułożonego wiersza we fragmencie tabeli (grupy wierszy
// w kolejności), a bez wierszy wartość zastępczą.
func lastRowBottom(groups []Box, fallback pr.Float) pr.Float {
	for i := len(groups) - 1; i >= 0; i-- {
		if rows := groups[i].Box().Children; len(rows) != 0 {
			row := rows[len(rows)-1].Box()
			return row.PositionY + row.Height.V()
		}
	}
	return fallback
}
