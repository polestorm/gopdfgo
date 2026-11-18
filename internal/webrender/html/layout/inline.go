package layout

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
	"github.com/polestorm/gopdfgo/internal/webrender/utils/testutils/tracer"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
)

//     Line breaking and layout for inline-level boxes.

// IsLine returns wheter `box` is either
// an instance of LineBox or InlineBox.
func IsLine(box Box) bool {
	return bo.LineT.IsInstance(box) || bo.InlineT.IsInstance(box)
}

type lineBoxe struct {
	// laid-out LineBox with as much content as possible that
	// fits in the available width.
	line     *bo.LineBox
	resumeAt tree.ResumeStack
}

type lineBoxeIterator struct {
	firstLetterStyle pr.ElementStyle

	currentBox lineBoxe

	box                    *bo.LineBox
	containingBlock        Box
	fixedBoxes             *[]*AbsolutePlaceholder
	skipStack              tree.ResumeStack
	context                *layoutContext
	absoluteBoxes          *[]*AbsolutePlaceholder
	positionY, bottomSpace pr.Float

	// gopdfgo: text-indent z hanging i each-line (CSS Text 3 § 7.1). W źródle wcięcie dostawał
	// pierwszy wiersz, a potem było zerowane (resetTextIndent).
	textIndent        pr.Float // wartość użyta wcięcia
	indentNext        bool     // następny wiersz jest pierwszym wierszem albo wierszem po wymuszonym złamaniu
	hanging, eachLine bool
	done              bool
	firstLine         bool // gopdfgo: następny wiersz jest pierwszym wierszem pudełka (`::first-line`)

	// gopdfgo: text-wrap-style: balance (CSS Text 4 § 5.2): o tyle zwęża się dostępną szerokość
	// każdego wiersza bieżącego odcinka (do wymuszonego złamania); balanced mówi, że szerokość dla
	// odcinka już wybrano, forced, że ostatni wiersz skończył się wymuszonym złamaniem
	shrink   pr.Float
	balanced bool
	forced   bool
	emitted  int // liczba wydanych wierszy
	// maxLines: ile wierszy zostaje do obcięcia przez line-clamp (max-lines), -1 bez ograniczenia
	maxLines int
	// hyphenatedLines: liczba kolejnych wierszy zakończonych dzieleniem wyrazu (hyphenate-limit-lines)
	hyphenatedLines int
}

func (l *lineBoxeIterator) Has() bool {
	if !l.balanced {
		l.balanced = true
		l.balance()
	}
	if l.done {
		l.box.TextIndent = pr.Float(0)
		return false
	}
	if l.indentNext != l.hanging {
		l.box.TextIndent = l.textIndent
	} else {
		l.box.TextIndent = pr.Float(0)
	}
	// gopdfgo: hyphenate-limit-lines (CSS Text 4, #hyphenate-limit-lines): po tylu kolejnych wierszach zakończonych
	// dzieleniem wyrazu następny wiersz nie dzieli wyrazów automatycznie. Źródło właściwości nie znało.
	savedNoAutoHyphens := l.context.noAutoHyphens
	if limit := l.box.Style.GetHyphenateLimitLines(); limit.Tag != pr.None && l.hyphenatedLines >= limit.I {
		l.context.noAutoHyphens = true
	}
	line, resumeAt, forcedBreak := getNextLinebox(l.context, l.box, l.positionY, l.bottomSpace, l.skipStack, l.containingBlock,
		l.absoluteBoxes, l.fixedBoxes, l.firstLetterStyle, l.firstLine, l.shrink)
	l.context.noAutoHyphens = savedNoAutoHyphens
	if line != nil && resumeAt != nil && endsHyphenated(line) {
		l.hyphenatedLines++
	} else {
		l.hyphenatedLines = 0
	}
	if line != nil {
		l.firstLine = false
	}

	if traceMode {
		traceLogger.Dump(fmt.Sprintf("lineBoxeIterator.Has: %s", resumeAt))
	}

	if line != nil {
		handleLeader(l.context, line, l.containingBlock.Box())
		l.positionY = line.Box().PositionY + line.Box().Height.V()
		if forcedBreak { // gopdfgo: clear elementu <br> (clearAfterLine)
			l.positionY = clearAfterLine(l.context, line, l.positionY)
		}
	}
	if line == nil {
		l.done = true
		return false
	}
	l.currentBox = lineBoxe{line: line, resumeAt: resumeAt}
	if resumeAt == nil {
		l.done = true
	}
	l.skipStack = resumeAt
	// gopdfgo: wiersz z samym elementem pływającym nie jest wierszem sformatowanym (elementy pływające
	// nie należą do wierszy, CSS Text 3 § 7.1), więc wcięcie przechodzi na następny wiersz
	if forcedBreak || hasInFlowContent(line) {
		l.indentNext = l.eachLine && forcedBreak
	}
	l.firstLetterStyle = nil
	l.emitted++
	if l.forced = forcedBreak; forcedBreak {
		l.balanced = false // gopdfgo: każdy odcinek między wymuszonymi złamaniami wyrównujemy osobno
	}
	return true
}

func (l *lineBoxeIterator) Next() lineBoxe { return l.currentBox }

// maxBalancedLines: tyle wierszy najwyżej wyrównuje text-wrap-style: balance. Norma pozwala
// ograniczyć liczbę wierszy (CSS Text 4 § 5.2); przyjmujemy granicę Chromium (ADR 0006).
const maxBalancedLines = 6

// balance (gopdfgo) wybiera miejsca łamania dla text-wrap-style: balance (CSS Text 4 § 5.2):
// wiersze mają mieć możliwie równą szerokość przy tej samej liczbie wierszy. Szukamy połowieniem
// największego zwężenia dostępnej szerokości, które nie zwiększa liczby wierszy (jak Chromium);
// wyrównanie wierszy (text-align) liczy się dalej od pełnej szerokości. Jak w Chromium wyrównujemy
// osobno każdy odcinek zakończony wymuszonym złamaniem, a przy overflow-wrap zwężenie nie schodzi
// poniżej szerokości min-content, żeby nie łamać słów. Źródło nie znało text-wrap-style.
func (l *lineBoxeIterator) balance() {
	l.shrink = 0
	if l.box.Style.GetTextWrapStyle() != "balance" {
		return
	}
	if ws := l.box.Style.GetWhiteSpace(); ws == pr.Nowrap || ws == pr.Pre {
		return
	}
	if l.firstLetterStyle != nil && l.skipStack != nil {
		// litera ozdobna przebudowuje drzewo i stos wznowienia; próbny układ by to powtórzył
		return
	}
	width := l.containingBlock.Box().Width
	if width == nil || width.V() <= 0 || width.V() == pr.Inf {
		return
	}
	lines := l.trialLines(0)
	if lines < 2 || lines > maxBalancedLines {
		return
	}
	if l.maxLines >= 0 && lines > l.maxLines-l.emitted {
		// Norma każe najpierw obciąć wiersze (line-clamp), a potem wyrównać pozostałe z miejscem na
		// wielokropek; wielokropek bloku dokładamy dopiero przy rysowaniu, więc układ nie zna jego
		// szerokości. Obcinanego bloku nie wyrównujemy.
		return
	}
	low, high := pr.Float(0), width.V()
	if style := l.box.Style; style.GetOverflowWrap() != "normal" || style.GetWordBreak() == "break-word" {
		// zwężony wiersz nie może łamać słów, które mieszczą się w pełnej szerokości; bez
		// overflow-wrap słowo dłuższe od zwężonego wiersza tylko wystaje poza zwężenie, nie poza blok
		high -= inlineMinContentWidth(l.context, l.box, false, l.skipStack, false, true)
		if high <= 0 {
			return
		}
	}
	if l.trialLines(high) <= lines {
		l.shrink = high
		return
	}
	for i := 0; i < 20 && high-low > 1.0/64; i++ {
		middle := (low + high) / 2
		if l.trialLines(middle) <= lines {
			low = middle
		} else {
			high = middle
		}
	}
	l.shrink = low
}

// trialLines (gopdfgo) układa próbnie wiersze iteratora przy zwężeniu shrink i zwraca ich liczbę,
// najwyżej maxBalancedLines+1; stan iteratora i skutki uboczne układu (floaty, pudełka
// pozycjonowane, elementy biegnące) są potem przywracane.
func (l *lineBoxeIterator) trialLines(shrink pr.Float) int {
	saved := *l
	context := l.context
	// listy wykluczeń zapamiętujemy jako nagłówki: próbne floaty dopisują się za ich długością, a układ
	// floata przełącza context.excludedShapes na listę kontekstu formatowania, nie na kopię
	savedShapes := context.excludedShapes
	var savedShapesValue shapeList
	if savedShapes != nil {
		savedShapesValue = *savedShapes
	}
	savedLists := context.excludedShapesLists
	savedListsValues := slices.Clone(savedLists)
	savedBroken, savedSequence := maps.Clone(context.brokenOutOfFlow), context.brokenSequence
	savedRunning := make(map[string]map[int][]Box, len(context.runningElements))
	for name, pages := range context.runningElements {
		savedRunning[name] = maps.Clone(pages)
	}
	var absolutes, fixed []*AbsolutePlaceholder
	l.absoluteBoxes, l.fixedBoxes = &absolutes, &fixed
	l.shrink = shrink

	count := 0
	l.balanced = true
	for count <= maxBalancedLines && l.Has() {
		count++
		if l.forced {
			break
		}
	}

	*l = saved
	context.excludedShapesLists = savedLists
	copy(savedLists, savedListsValues)
	context.excludedShapes = savedShapes
	if savedShapes != nil {
		*savedShapes = savedShapesValue
	}
	context.brokenOutOfFlow, context.brokenSequence = savedBroken, savedSequence
	context.runningElements = savedRunning
	return count
}

func (l *lineBoxeIterator) consume() int {
	count := 0
	for l.Has() {
		count++
	}
	return count
}

// `box` is a non-laid-out `LineBox`
// positionY is the vertical top position of the line box on the page
// skipStack is “nil“ to start at the beginning of “linebox“,
// or a “resumeAt“ value to continue just after an
// already laid-out line.
func iterLineBoxes(context *layoutContext, box *bo.LineBox, positionY, bottomSpace pr.Float, skipStack tree.ResumeStack, containingBlock Box,
	absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder, firstLetterStyle pr.ElementStyle,
) *lineBoxeIterator {
	resolvePercentages(box, bo.MaybePoint{containingBlock.Box().Width, containingBlock.Box().Height})
	// gopdfgo: wcięcie dostaje pierwszy sformatowany wiersz kontenera blokowego, a z each-line także
	// każdy wiersz po wymuszonym złamaniu, w tym po pudełku blokowym; hanging odwraca wybór wierszy
	// (CSS Text 3 § 7.1). Źródło wcinało pierwszy wiersz każdego pudełka wiersza, także tego po bloku
	// (https://github.com/Kozea/WeasyPrint/issues/679). Po podziale strony nie wiemy, czy poprzedni
	// wiersz kończył się wymuszonym złamaniem, więc wiersz wznowiony liczy się jak zwykły.
	indent := box.Style.GetTextIndent()
	textIndent := resolveOnePercentage(indent.TaggedDim, pr.PTextIndent, containingBlock.Box().Width.V()).V()
	box.TextIndent = pr.Float(0)
	return &lineBoxeIterator{
		textIndent: textIndent, hanging: indent.Hanging, eachLine: indent.EachLine,
		indentNext: skipStack == nil && (!box.AfterBlock || indent.EachLine),
		box:        box, context: context, positionY: positionY, bottomSpace: bottomSpace, containingBlock: containingBlock,
		absoluteBoxes: absoluteBoxes, fixedBoxes: fixedBoxes, skipStack: skipStack, firstLetterStyle: firstLetterStyle,
		firstLine: skipStack == nil, maxLines: -1,
	}
}

func getNextLinebox(context *layoutContext, linebox *bo.LineBox, positionY, bottomSpace pr.Float, skipStack tree.ResumeStack,
	containingBlock_ Box, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder,
	firstLetterStyle pr.ElementStyle, firstLine bool, shrink pr.Float,
) (line_ *bo.LineBox, resumeAt tree.ResumeStack, forcedBreak bool) {
	if traceMode {
		traceLogger.DumpTree(linebox, "getNextLinebox")
	}

	containingBlock := containingBlock_.Box()
	skipStack, cont := skipFirstWhitespace(linebox, skipStack)
	if cont {
		return
	}

	skipStack = firstLetterToBox(context, linebox, skipStack, firstLetterStyle)

	wrappedFirstLine := false
	if firstLine && linebox.FirstLineStyle != nil {
		// gopdfgo: pierwszy wiersz układamy z kopii, w której treść dziedziczy po `::first-line`;
		// budowa kopii jest taka sama, więc miejsce wznowienia pasuje do pudełka pierwotnego
		linebox = firstLineBox(linebox, linebox.FirstLineStyle)
		wrappedFirstLine = true
		if skipStack != nil {
			skipStack = tree.ResumeStack{0: skipStack}
		}
	}

	linebox.PositionY = positionY
	shapesAtStart := len(context.excludedShapes.list)
	shapesPrefix := slices.Clone(context.excludedShapes.list)

	if len(context.excludedShapes.list) != 0 {
		// Width and height must be calculated to avoid floats
		linebox.Width = inlineMinContentWidth(context, linebox, true, skipStack, true, false)
		linebox.Height, _ = text.StrutLayout(linebox.Style, context)
	} else {
		// No float, width and height will be set by the lines
		linebox.Width = pr.Float(0)
		linebox.Height = pr.Float(0)
	}
	positionX, positionY, availableWidth := avoidCollisions(context, linebox, containingBlock, false)

	candidateHeight := linebox.Height

	excludedShapes := shapeList{list: append([]*bo.BoxFields{}, context.excludedShapes.list...), isFrozen: context.excludedShapes.isFrozen}

	var (
		linePlaceholders, lineAbsolutes, lineFixed []*AbsolutePlaceholder
		waitingFloats                              []Box
	)
	for {
		linebox.PositionX, linebox.PositionY = positionX, positionY
		originalPositionX, originalPositionY := positionX, positionY
		originalWidth := linebox.Width.V()
		originalAvailable := availableWidth
		waitingFloats = waitingFloats[:0] // reset

		// gopdfgo: shrink zwęża wiersz przy text-wrap-style: balance (lineBoxeIterator.balance)
		maxX := positionX + availableWidth - shrink
		positionX += linebox.TextIndent.V()

		var (
			preservedLineBreak bool
			floatWidths        widths
		)

		savedStart, savedTabUnit, savedInLine := context.lineStartX, context.lineTabUnit, context.inLine
		context.lineStartX, context.lineTabUnit, context.inLine = linebox.PositionX, -1, true
		spi := splitInlineBox(context, linebox, positionX, maxX, bottomSpace, skipStack, containingBlock_,
			&lineAbsolutes, &lineFixed, &linePlaceholders, &waitingFloats, nil)
		context.lineStartX, context.lineTabUnit, context.inLine = savedStart, savedTabUnit, savedInLine
		resumeAt, preservedLineBreak, floatWidths = spi.resumeAt, spi.preservedLineBreak, spi.floatWidths
		forcedBreak = preservedLineBreak
		line_ = spi.newBox.(*bo.LineBox) // splitInlineBox preserve the concrete type

		if traceMode {
			traceLogger.DumpTree(line_, "line in getNextLinebox")
		}

		if resumeAt != nil && !preservedLineBreak {
			addSoftHyphenAtBoxEnd(context, line_)
		}
		line := line_.Box()
		linebox.Width, linebox.Height = line.Width, line.Height

		if isPhantomLinebox(line) && !preservedLineBreak {
			line.Height = pr.Float(0)
			break
		}

		removeLastWhitespace(context, line_)

		// gopdfgo: miejsce do wyrównania liczy się na całej wysokości wiersza znanej z poprzedniego
		// przebiegu (candidateHeight); wysokość z splitInlineBox bywa jeszcze wysokością rozpórki, więc
		// element pływający niżej w pasie wiersza nie był widziany i wiersz wyrównany do prawej
		// nachodził na niego (test WPT floats-wrap-top-below-inline-001r).
		splitHeight := linebox.Height
		if candidateHeight != nil && linebox.Height != nil && candidateHeight.V() > linebox.Height.V() {
			linebox.Height = candidateHeight
		}
		newPositionX, _, newAvailableWidth := avoidCollisions(context, linebox, containingBlock, false)
		linebox.Height = splitHeight
		newAvailableWidth -= floatWidths.right
		alignmentAvailableWidth := newAvailableWidth + newPositionX - linebox.PositionX
		// gopdfgo: przy white-space: pre-wrap spacje i tabulatory na końcu wiersza wiszą (CSS Text 3
		// § 4.1.3): zachowują szerokość i tło, ale nie liczą się do wyrównania ani justowania. W wierszu
		// zawiniętym wiszą wszystkie, w wierszu zakończonym wymuszonym podziałem albo ostatnim tylko
		// ta część, która nie mieści się w wierszu.
		hang := trailingPreservedWidth(line_)
		if resumeAt == nil || preservedLineBreak {
			hang = max(0, min(hang, line.Width.V()-alignmentAvailableWidth))
		}
		offsetX := textAlign(context, line_, alignmentAvailableWidth, resumeAt == nil || preservedLineBreak, hang)

		if traceMode {
			traceLogger.DumpTree(line_, "line in getNextLinebox after textAlign")
		}

		if containingBlock.Style.GetDirection() == pr.Rtl {
			offsetX = -offsetX
			offsetX -= line.Width.V()
		}

		bottom, top := lineBoxVerticality(context, line_)
		line.Baseline = -top
		line.PositionY = top
		line.Height = bottom - top
		offsetY := positionY - top
		line.MarginTop = pr.Float(0)
		line.MarginBottom = pr.Float(0)

		line_.Translate(offsetX, offsetY, false)
		// Avoid floating point errors, as positionY - top + top != positionY
		// Removing this line breaks the position == linebox.Position test below
		// See https://github.com/Kozea/WeasyPrint/issues/583
		line.PositionY = positionY

		if line.Height.V() <= candidateHeight.V() {
			break
		}
		candidateHeight = line.Height

		newExcludedShapes := context.excludedShapes
		context.excludedShapes = &excludedShapes
		positionX, positionY, availableWidth = avoidCollisions(context, line_, containingBlock, false)
		var condition bool
		if containingBlock.Style.GetDirection() == pr.Ltr {
			condition = positionX == originalPositionX && positionY == originalPositionY
		} else {
			condition = positionX+line.Width.V() == originalPositionX+originalWidth && positionY == originalPositionY
		}

		// gopdfgo: wiersz wyższy niż przyjęty zajmuje też pas niżej; gdy stoi tam szerszy element pływający
		// po stronie końca wiersza, pas jest węższy, choć początek wiersza się nie zmienia, i wiersz trzeba
		// ułożyć ponownie (CSS 2.1 § 9.5: wiersz obok elementu pływającego jest skracany na całej wysokości;
		// test WPT floats-wrap-top-below-inline-001r). W źródle porównywano tylko początek wiersza.
		if condition && availableWidth < originalAvailable-0.01 {
			condition = false
		}
		if condition {
			context.excludedShapes = newExcludedShapes
			break
		}
	}

	if traceMode {
		traceLogger.DumpTree(line_, "after loop in getNextLinebox")
	}

	if wrappedFirstLine && resumeAt != nil {
		// miejsce wznowienia liczone jest względem pudełka `::first-line`: zdejmujemy ten poziom
		if _, inner := resumeAt.Unpack(); inner != nil {
			resumeAt = inner
		} else {
			resumeAt = tree.ResumeStack{0: nil}
		}
	}

	*absoluteBoxes = append(*absoluteBoxes, lineAbsolutes...)
	*fixedBoxes = append(*fixedBoxes, lineFixed...)

	line := line_.Box()
	for _, placeholder := range linePlaceholders {
		if placeholder.Box().Style.Specified().Display.Has(pr.Inline) {
			// Inline-level static position :
			placeholder.Translate(0, positionY-placeholder.Box().PositionY.V(), false)
		} else {
			// Block-level static position: at the start of the next line
			// gopdfgo: bez treści przed blokiem wiersz przed nim miałby zerową wysokość,
			// więc położeniem statycznym jest góra wiersza (contentBeforeInLine).
			staticY := positionY + line.Height.V()
			if !contentBeforeInLine(line_, placeholder) {
				staticY = positionY
			}
			placeholder.Translate(line.PositionX-placeholder.Box().PositionX.V(),
				staticY-placeholder.Box().PositionY.V(), false)
		}
	}

	// gopdfgo: element pływający ułożony w części wiersza, która po złamaniu przeszła do następnego
	// wiersza, zostawał na liście wykluczeń, choć nie ma go w drzewie: przy ponownym układzie
	// w następnym wierszu stawał obok swojego widma (test WPT float-nowrap-4). Usuwamy z listy elementy
	// pływające dopisane w tym wierszu, których w wierszu nie ma.
	if len(context.excludedShapes.list) > shapesAtStart && slices.Equal(context.excludedShapes.list[:shapesAtStart], shapesPrefix) {
		placed := map[*bo.BoxFields]bool{}
		var walk func(Box)
		walk = func(b Box) {
			for _, child := range b.Box().Children {
				if child.Box().IsFloated() {
					placed[child.Box()] = true
				}
				walk(child)
			}
		}
		walk(line_)
		kept := context.excludedShapes.list[:shapesAtStart]
		for _, shape := range context.excludedShapes.list[shapesAtStart:] {
			if placed[shape] {
				kept = append(kept, shape)
			}
		}
		context.excludedShapes.list = kept
	}

	var floatChildren []Box
	waitingFloatsY := line.PositionY + line.Height.V()
	for _, waitingFloat_ := range waitingFloats {
		waitingFloat := waitingFloat_.Box()
		waitingFloat.PositionY = waitingFloatsY
		newWaitingFloat, waitingFloatResumeAt := floatLayout(context, waitingFloat_, containingBlock, absoluteBoxes, fixedBoxes, bottomSpace, nil)

		if traceMode {
			traceLogger.DumpTree(newWaitingFloat, "after float layout")
		}

		floatChildren = append(floatChildren, newWaitingFloat)
		if waitingFloatResumeAt != nil {
			context.setBrokenOutOfFlow(newWaitingFloat, brokenBox{box: waitingFloat_, containingBlock: containingBlock_, resumeAt: waitingFloatResumeAt})
		}
	}
	line.Children = append(line.Children, floatChildren...)

	return line_, resumeAt, forcedBreak
}

// firstLineBox (gopdfgo) zwraca kopię pudełka wiersza ze stylami przeliczonymi tak, jakby treść
// wiersza była objęta pseudoelementem `::first-line` (CSS Pseudo-Elements 4 § 2.1): pudełka
// liniowe i tekst dziedziczą po nim, a pudełka atomowe i wyjęte z przepływu zostają bez zmian.
func firstLineBox(linebox *bo.LineBox, style pr.ElementStyle) *bo.LineBox {
	out := *linebox
	out.Style = tree.Reparent(linebox.Style, style)
	parent := tree.FirstLineParent(style, linebox.Style.ParentStyle())
	// treść wiersza obejmuje pudełko liniowe pseudoelementu: rysuje jego tło i niesie `vertical-align`
	wrapper := bo.NewInlineBox(style, linebox.Element, "first-line", firstLineChildren(linebox.Children, parent))
	out.Children = []Box{wrapper}
	return &out
}

func firstLineChildren(children []Box, parent pr.ElementStyle) []Box {
	out := make([]Box, len(children))
	for i, child := range children {
		out[i] = child
		if !child.Box().IsInNormalFlow() {
			continue
		}
		switch child.(type) {
		case *bo.TextBox, *bo.InlineBox:
			copied := child.Copy()
			copied.Box().Style = tree.Reparent(child.Box().Style, parent)
			copied.Box().Children = firstLineChildren(child.Box().Children, copied.Box().Style)
			out[i] = copied
		}
	}
	return out
}

// firstLineTarget (gopdfgo) szuka pudełka pierwszego sformatowanego wiersza bloku: pierwszy wiersz
// samego bloku albo jego pierwszego dziecka blokowego w przepływie (CSS Pseudo-Elements 4 § 2.1.2).
// Drugi wynik mówi, że szukanie jest zakończone (także bez znalezienia wiersza).
func firstLineTarget(box Box) (*bo.LineBox, bool) {
	for _, child := range box.Box().Children {
		if !child.Box().IsInNormalFlow() {
			continue
		}
		if line, ok := child.(*bo.LineBox); ok {
			return line, true
		}
		if _, ok := child.(*bo.BlockBox); !ok {
			return nil, true
		}
		if line, done := firstLineTarget(child); done {
			return line, true
		}
	}
	return nil, false
}

// Return the “skipStack“ to start just after the removed spaces
// at the beginning of the line.
// See https://www.w3.org/TR/CSS21/text.html#white-space-model
func skipFirstWhitespace(box Box, skipStack tree.ResumeStack) (tree.ResumeStack, bool) {
	var (
		index         int
		nextSkipStack tree.ResumeStack
	)
	if skipStack != nil {
		index, nextSkipStack = skipStack.Unpack()
	}
	if textBox, ok := box.(*bo.TextBox); ok {
		if nextSkipStack != nil {
			panic(fmt.Sprintf("expected nil nextSkipStack, got %v", nextSkipStack))
		}
		whiteSpace := textBox.Style.GetWhiteSpace()
		text := textBox.Text
		length := len(text)
		if index == length {
			// Starting a the end of the TextBox, no text to see: Continue
			return nil, true
		}
		if whiteSpace == pr.Normal || whiteSpace == pr.Nowrap || whiteSpace == pr.PreLine {
			for index < length && text[index] == ' ' {
				index += 1
			}
		}
		if index != 0 {
			return tree.ResumeStack{index: nil}, false
		}
		return nil, false
	}

	if IsLine(box) {
		children := box.Box().Children
		if index == 0 && len(children) == 0 {
			// gopdfgo: puste pudełko liniowe bez marginesów, dopełnienia i obramowań w poziomie nie
			// zatrzymuje usuwania spacji z początku wiersza (CSS Text 3 § 4.1.2); tak wygląda np.
			// reszta pudełka po wydzieleniu z niego bloku. Źródło kończyło tu usuwanie spacji.
			return nil, isBareEmptyInline(box)
		}
		result, cont := skipFirstWhitespace(children[index], nextSkipStack)
		for cont {
			index += 1
			if index >= len(children) {
				return nil, true
			}
			result, cont = skipFirstWhitespace(children[index], nil)
		}
		if index != 0 || result != nil {
			return tree.ResumeStack{index: result}, false
		}
		return nil, false
	}

	if skipStack != nil {
		panic(fmt.Sprintf("unexpected skip inside %s", box.Type()))
	}

	return nil, false
}

// skipsOnlyLeadingSpaces podaje, czy “skipStack“ omija wyłącznie zwijane spacje
// na samym początku pudełka (wynik skipFirstWhitespace), a nie treść z poprzedniego wiersza.
func skipsOnlyLeadingSpaces(box Box, skipStack tree.ResumeStack) bool {
	for skipStack != nil {
		index, next := skipStack.Unpack()
		if textBox, ok := box.(*bo.TextBox); ok {
			ws := textBox.Style.GetWhiteSpace()
			if ws != pr.Normal && ws != pr.Nowrap && ws != pr.PreLine {
				return false
			}
			if index > len(textBox.Text) {
				return false
			}
			for _, c := range textBox.Text[:index] {
				if c != ' ' {
					return false
				}
			}
			return true
		}
		children := box.Box().Children
		if index != 0 || !IsLine(box) || len(children) == 0 {
			return false
		}
		box, skipStack = children[0], next
	}
	return true
}

// Remove in place space characters at the end of a line.
// This also reduces the width of the inline parents of the modified text.
func removeLastWhitespace(context *layoutContext, line *bo.LineBox) {
	var ancestors []Box
	box := Box(line)
	for IsLine(box) {
		ch := box.Box().Children
		if len(ch) == 0 {
			if box == Box(line) {
				return
			}
			break // gopdfgo: puste pudełko liniowe na końcu, zob. niżej
		}
		ancestors = append(ancestors, box)
		box = ch[len(ch)-1]
	}
	// gopdfgo: za końcową spacją bywają puste pudełka liniowe (z samym obramowaniem albo
	// dopełnieniem, bez treści); spacja jest wtedy nadal na końcu wiersza i znika (CSS Text 3
	// § 4.1.2, granice pudełek liniowych się nie liczą; test WPT line-edge-white-space-collapse-001).
	// Źródło sprawdzało tylko ostatni liść. Pudełka za spacją przesuwamy o jej szerokość.
	var following []Box
	// Tylko gdy wiersz kończy się pustym pudełkiem liniowym albo pustym tekstem; element pływający
	// albo atomowy na końcu wiersza zostawia spację jak w źródle (test TestFloatTableAbortedRow).
	if textBox, ok := box.(*bo.TextBox); line.Style.GetDirection() == pr.Ltr && (ok && len(textBox.Text) == 0 || !ok && IsLine(box)) {
		path, after, found := lastTextBefore(line, nil)
		if !found || len(path) < 2 {
			return
		}
		ancestors, box, following = path[:len(path)-1], path[len(path)-1], after
	}
	textBox, ok := box.(*bo.TextBox)
	if ws := box.Box().Style.GetWhiteSpace(); !(ok && (ws == pr.Normal || ws == pr.Nowrap || ws == pr.PreLine)) {
		return
	}
	newText := text.TrimSuffix(textBox.Text, ' ')
	var spaceWidth pr.Float
	if L := len(newText); L != 0 {
		if L == len(textBox.Text) {
			return
		}
		textBox.Text = newText
		newBox, resume, _ := splitTextBox(context, textBox, nil, 0, true)
		if newBox == nil || resume != -1 {
			panic(fmt.Sprintf("expected newBox and no resume, got %v, %v", newBox, resume))
		}
		spaceWidth = textBox.Width.V() - newBox.Box().Width.V()
		textBox.Width = newBox.Box().Width
	} else {
		spaceWidth = textBox.Width.V()
		textBox.Width = pr.Float(0)
		textBox.Text = nil
	}

	// RTL line, the trailing space is at the left of the box. We have to translate the
	// box to align the stripped text with the right edge of the box.
	if textBox.FirstLineIsRTL {
		for _, child := range line.Children {
			child.Translate(-spaceWidth, 0, true)
		}
	}

	for _, ancestor := range ancestors {
		ancestor.Box().Width = ancestor.Box().Width.V() - spaceWidth
	}
	for _, box := range following {
		box.Translate(-spaceWidth, 0, true)
	}

	// TODO: All tabs (U+0009) are rendered as a horizontal shift that
	// lines up the start edge of the next glyph with the next tab stop.
	// Tab stops occur at points that are multiples of 8 times the width
	// of a space (U+0020) rendered in the block"s font from the block"s
	// starting content edge.

	// TODO: If spaces (U+0020) or tabs (U+0009) at the end of a line have
	// "white-space" set to "pre-wrap", UAs may visually collapse them.
}

// lastTextBefore (gopdfgo) szuka w box ostatniego niepustego pudełka tekstowego, pomijając puste
// pudełka liniowe i puste teksty za nim. Zwraca ścieżkę od box do tego tekstu oraz pominięte
// pudełka leżące za nim; found jest fałszywe, gdy za ostatnim tekstem jest inna treść (np. element
// atomowy) albo tekstu nie ma.
func lastTextBefore(box Box, after []Box) (path, following []Box, found bool) {
	if _, ok := box.(*bo.TextBox); ok {
		return []Box{box}, after, true // pusty tekst pomija wywołujący (isEmptyInline)
	}
	if !IsLine(box) {
		return nil, after, false
	}
	children := box.Box().Children
	for i := len(children) - 1; i >= 0; i-- {
		child := children[i]
		if !child.Box().IsInNormalFlow() {
			continue
		}
		if isEmptyInline(child) {
			after = append(after, child)
			continue
		}
		if _, isText := child.(*bo.TextBox); !isText && !IsLine(child) {
			return nil, after, false
		}
		subPath, subAfter, found := lastTextBefore(child, after)
		if found {
			return append([]Box{box}, subPath...), subAfter, true
		}
		return nil, after, false
	}
	return nil, after, false
}

// isEmptyInline (gopdfgo): pudełko liniowe bez treści (bez tekstu i elementów atomowych w przepływie).
func isEmptyInline(box Box) bool {
	if textBox, ok := box.(*bo.TextBox); ok {
		return len(textBox.Text) == 0
	}
	if !IsLine(box) {
		return false
	}
	for _, child := range box.Box().Children {
		if child.Box().IsInNormalFlow() && !isEmptyInline(child) {
			return false
		}
	}
	return true
}

// Create a box for the ::first-letter selector.
func firstLetterToBox(context *layoutContext, box Box, skipStack tree.ResumeStack, firstLetterStyle pr.ElementStyle) tree.ResumeStack {
	if firstLetterStyle == nil || len(box.Box().Children) == 0 {
		return skipStack
	}

	// Some properties must be ignored :in first-letter boxes.
	// https://drafts.csswg.org/selectors-3/#application-in-css
	// At least, position is ignored to avoid layout troubles.
	firstLetterStyle.SetPosition(pr.BoolString{String: "static"})

	firstLetter := ""
	child := box.Box().Children[0]
	var childSkipStack tree.ResumeStack
	if textBox, ok := child.(*bo.TextBox); ok {
		letterStyle := tree.ComputedFromCascaded(nil, nil, firstLetterStyle, context)
		if strings.HasSuffix(textBox.ElementTag(), "::first-letter") {
			letterBox := bo.NewInlineBox(letterStyle, textBox.Element, "first-letter", []Box{child})
			box.Box().Children[0] = letterBox
		} else if len(textBox.Text) != 0 {
			text := textBox.Text
			characterFound := false
			if skipStack != nil {
				_, childSkipStack = skipStack.Unpack()
				if childSkipStack != nil {
					index, _ := childSkipStack.Unpack()
					text = text[index:]
					skipStack = nil
				}
			}
			// gopdfgo: tekst zaczynający się od nierozdzielającej spacji (separator Zs inny niż zwykła spacja,
			// która może jeszcze zostać zwinięta) nie ma ::first-letter, jak w Chromium; spacje nie są
			// typograficzną jednostką litery (CSS Pseudo 4 § 2.2, test WPT first-letter-space-not-selected).
			// Źródło brało taką spację za pierwszą literę.
			leadingSpace := len(text) != 0 && text[0] != ' ' && unicode.Is(unicode.Zs, text[0])
			for len(text) != 0 && !leadingSpace {
				nextLetter := text[0]
				isPunc := unicode.In(nextLetter, bo.TableFirstLetter...)
				if !isPunc {
					if characterFound {
						break
					}
					characterFound = true
				}
				firstLetter += string(nextLetter)
				text = text[1:]
			}
			textBox.Text = text
			if strings.TrimLeft(firstLetter, "\n") != "" {
				// "This type of initial letter is similar to an
				// inline-level element if its "float" property is "none",
				// otherwise it is similar to a floated element."
				if firstLetterStyle.GetFloat() == "none" {
					letterBox := bo.NewInlineBox(firstLetterStyle, textBox.Element, "first-letter", nil)
					textBox = bo.NewTextBox(letterStyle, textBox.Element, "first-letter", []rune(firstLetter))
					letterBox.Children = []Box{textBox}
					// gopdfgo: pudełko litery trafia przed dzieci pudełka nadrzędnego (w WeasyPrint:
					// box.children = (letter_box,) + box.children). W źródle dopisywano je do nowego
					// pudełka tekstu, więc pierwsza litera znikała z dokumentu.
					box.Box().Children = append([]Box{letterBox}, box.Box().Children...)
				} else {
					letterBox := bo.NewBlockBox(firstLetterStyle, textBox.Element, "first-letter", nil)
					letterBox.FirstLetterStyle = nil
					// gopdfgo: wiersz wewnątrz pudełka litery dostaje styl anonimowy (jak w WeasyPrint), nie styl
					// ::first-letter; z float na wierszu układ kończył się paniką.
					lineBox := bo.NewLineBox(letterStyle, textBox.Element, "first-letter", nil)
					letterBox.Children = []Box{&lineBox}
					textBox = bo.NewTextBox(letterStyle, textBox.Element, "first-letter", []rune(firstLetter))
					lineBox.Children = []Box{textBox}
					// gopdfgo: pudełko litery trafia przed dzieci pudełka nadrzędnego (w WeasyPrint:
					// box.children = (letter_box,) + box.children). W źródle dopisywano je do nowego
					// pudełka tekstu, więc pierwsza litera znikała z dokumentu.
					box.Box().Children = append([]Box{letterBox}, box.Box().Children...)
				}
				bo.ProcessTextTransform(textBox)
				if skipStack != nil && childSkipStack != nil {
					index, _ := skipStack.Unpack()
					childIndex, grandChildSkipStack := childSkipStack.Unpack()
					skipStack = tree.ResumeStack{index: tree.ResumeStack{childIndex + 1: grandChildSkipStack}}
				}
			}
		}
	} else if bo.ParentT.IsInstance(child) {
		if skipStack != nil {
			_, childSkipStack = skipStack.Unpack()
		} else {
			childSkipStack = nil
		}
		childSkipStack = firstLetterToBox(context, child, childSkipStack, firstLetterStyle)
		if skipStack != nil {
			resumeIndex, _ := skipStack.Unpack()
			skipStack = tree.ResumeStack{resumeIndex: childSkipStack}
		}
	}
	return skipStack
}

func resolveMarginAuto(box *bo.BoxFields) {
	if box.MarginTop == pr.AutoF {
		box.MarginTop = pr.Float(0)
	}
	if box.MarginRight == pr.AutoF {
		box.MarginRight = pr.Float(0)
	}
	if box.MarginBottom == pr.AutoF {
		box.MarginBottom = pr.Float(0)
	}
	if box.MarginLeft == pr.AutoF {
		box.MarginLeft = pr.Float(0)
	}
}

// Compute the width and the height of the atomic “box“.
func atomicBox(context *layoutContext, box_ Box, positionX pr.Float, skipStack tree.ResumeStack, containingBlock *bo.BoxFields,
	absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder,
) Box {
	if _, ok := box_.(bo.ReplacedBoxITF); ok {
		box_ = box_.Copy()
		inlineReplacedBoxLayout(box_, containingBlock)
		box_.Box().Baseline = box_.Box().MarginHeight()
	} else if bo.InlineBlockT.IsInstance(box_) {
		var width, minWidth, maxWidth pr.MaybeFloat
		if box := box_.Box(); box.IsTableWrapper {
			tableWrapperWidth(context, box_, bo.MaybePoint{containingBlock.Width, containingBlock.Height})
			width, minWidth, maxWidth = box.Width, box.MinWidth, box.MaxWidth
		}
		box_ = inlineBlockBoxLayout(context, box_, positionX, skipStack, containingBlock,
			absoluteBoxes, fixedBoxes)
		if box := box_.Box(); box.IsTableWrapper {
			box.Width, box.MinWidth, box.MaxWidth = width, minWidth, maxWidth
		}
	} else {
		panic(fmt.Sprintf("Layout for %s not handled yet", box_))
	}
	return box_
}

func inlineBlockBoxLayout(context *layoutContext, box_ Box, positionX pr.Float, skipStack tree.ResumeStack,
	containingBlock *bo.BoxFields, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder,
) Box {
	resolveSizesBox(context, box_, containingBlock)
	box := box_.Box()
	// https://www.w3.org/TR/CSS21/visudet.html#inlineblock-width
	if box.MarginLeft == pr.AutoF {
		box.MarginLeft = pr.Float(0)
	}
	if box.MarginRight == pr.AutoF {
		box.MarginRight = pr.Float(0)
	}
	// https://www.w3.org/TR/CSS21/visudet.html#block-root-margin
	if box.MarginTop == pr.AutoF {
		box.MarginTop = pr.Float(0)
	}
	if box.MarginBottom == pr.AutoF {
		box.MarginBottom = pr.Float(0)
	}

	inlineBlockWidth(box_, context, containingBlock)

	box.PositionX = positionX
	box.PositionY = 0
	box_, _, _ = blockContainerLayout(context, box_, -pr.Inf, skipStack,
		true, absoluteBoxes, fixedBoxes, new([]pr.Float), false, -1)
	box_.Box().Baseline = inlineBlockBaseline(box_)
	return box_
}

// Return the y position of the baseline for an inline block
// from the top of its margin box.
// https://www.w3.org/TR/CSS21/visudet.html#propdef-vertical-align
func inlineBlockBaseline(box_ Box) pr.Float {
	box := box_.Box()
	if box.IsTableWrapper {
		// Inline table's baseline is its first row's baseline
		for _, child := range box.Children {
			if bo.TableT.IsInstance(child) {
				if cc := child.Box().Children; len(cc) != 0 && len(cc[0].Box().Children) != 0 {
					firstRow := cc[0].Box().Children[0]
					// gopdfgo: linia bazowa wiersza jest liczona od jego górnej krawędzi, a wynik ma być
					// liczony od górnej krawędzi marginesu tabeli (pudełko leży tu w y = 0); w źródle
					// pomijane były obramowanie tabeli, odstęp między komórkami i podpis nad tabelą.
					return firstRow.Box().PositionY - box.PositionY + firstRow.Box().Baseline.V()
				}
			}
		}
	} else if box.Style.GetOverflow() == "visible" {
		result := findInFlowBaseline(box_, true)
		if pr.Is(result) {
			return result.V()
		}
	} else if result := findInFlowBaseline(box_, true); pr.Is(result) && result.V() <= box.BorderBoxY()+box.BorderHeight() &&
		box.ElementTag() != "textarea" {
		// gopdfgo: kontener przewijania bierze linię bazową z ostatniego wiersza, o ile ten wiersz
		// leży w pudełku; gdy treść jest przycięta i ostatni wiersz wypada poniżej, zostaje dolna
		// krawędź marginesu z CSS 2.1 § 10.8.1 (tak robi Chromium; testy WPT inline-block-baseline-003
		// i vertical-align-baseline-005a). Źródło zawsze brało krawędź marginesu. Wyjątek: textarea,
		// które w Chromium stoi dolną krawędzią na linii bazowej (pomiar na dokumencie
		// 13-forms-html-elements korpusu).
		return result.V()
	}
	return box.PositionY + box.MarginHeight()
}

var inlineBlockWidth = handleMinMaxWidth(inlineBlockWidth_)

// @handleMinMaxWidth
func inlineBlockWidth_(box_ Box, context *layoutContext, containingBlock containingBlock) (bool, pr.Float) {
	if box := box_.Box(); box.Width == pr.AutoF {
		cbWidth, _ := containingBlock.ContainingBlock()
		availableContentWidth := cbWidth.V() - (box.MarginLeft.V() + box.MarginRight.V() +
			box.BorderLeftWidth.V() + box.BorderRightWidth.V() +
			box.PaddingLeft.V() + box.PaddingRight.V())
		box.Width = shrinkToFit(context, box_, availableContentWidth)
	}
	return false, 0
}

type widths struct {
	left, right pr.Float
}

func (w *widths) add(key pr.String, value pr.Float) {
	switch key {
	case "left":
		w.left += value
	case "right":
		w.right += value
	default:
		panic("unexpected key " + key)
	}
}

type splitedInline struct {
	newBox                  Box
	resumeAt                tree.ResumeStack
	preservedLineBreak      bool
	firstLetter, lastLetter rune // 0 for none
	floatWidths             widths
}

// Fit as much content as possible from an inline-level box in a width.
//
// Return “(newBox, resumeAt, preservedLineBreak, firstLetter,
// lastLetter)“. “resumeAt“ is “nil“ if all of the content
// fits. Otherwise it can be passed as a “skipStack“ parameter to resume
// where we left off.
//
// “newBox“ is non-empty (unless the box is empty) and as big as possible
// while being narrower than “availableWidth“, if possible (may overflow
// is no split is possible.)
func splitInlineLevel(context *layoutContext, box_ Box, positionX, maxX, bottomSpace pr.Float, skipStack tree.ResumeStack,
	containingBlock Box, absoluteBoxes, fixedBoxes,
	linePlaceholders *[]*AbsolutePlaceholder, waitingFloats *[]Box, lineChildren []indexedBox,
) splitedInline {
	if traceMode {
		traceLogger.Indent()
		defer traceLogger.DeIndent()
		traceLogger.DumpTree(box_, "splitInlineLevel")
	}

	box := box_.Box()
	resolvePercentagesBox(box_, containingBlock.Box())
	floatWidths := widths{}
	var (
		newBox                  Box
		preservedLineBreak      bool
		resumeAt                tree.ResumeStack
		firstLetter, lastLetter rune
	)

	if textBox, ok := box_.(*bo.TextBox); ok {
		textBox.PositionX = positionX
		skip := 0
		if skipStack != nil {
			skip, skipStack = skipStack.Unpack()
			if skipStack != nil {
				panic(fmt.Sprintf("expected empty skipStack, got %v", skipStack))
			}
		}
		var newTextBox *bo.TextBox
		// gopdfgo: forceWordBreak: w wierszu nie ma miejsca podziału, więc słowo wolno złamać tak,
		// jakby zaczynało wiersz (overflow-wrap)
		isLineStart := len(lineChildren) == 0 || context.forceWordBreak
		tabOffset, tabUnit := pr.Float(0), pr.Float(-1)
		if context.inLine && slices.Contains(textBox.Text[skip:], '\t') {
			// odstęp tabulacji wynika ze stylu bloku zawierającego wiersz, nie z elementu liniowego
			if context.lineTabUnit < 0 {
				context.lineTabUnit = text.TabUnit(containingBlock.Box().Style, context)
			}
			tabOffset, tabUnit = positionX-context.lineStartX, context.lineTabUnit
		}
		newTextBox, skip, preservedLineBreak = splitTextBoxAt(context, textBox, maxX-positionX, skip, isLineStart, tabOffset, tabUnit)
		if skip != -1 {
			resumeAt = tree.ResumeStack{skip: nil}
		}
		if newTextBox != nil { // we dont want a non nil interface value with a nil pointer
			newBox = newTextBox
		}
		if text := textBox.Text; len(text) != 0 {
			firstLetter = text[0]
			if skip == -1 {
				lastLetter = text[len(text)-1]
			} else {
				lastLetter = text[skip-1]
			}
		}
	} else if bo.InlineT.IsInstance(box_) {
		if box.MarginLeft == pr.AutoF {
			box.MarginLeft = pr.Float(0)
		}
		if box.MarginRight == pr.AutoF {
			box.MarginRight = pr.Float(0)
		}
		tmp := splitInlineBox(context, box_, positionX, maxX, bottomSpace, skipStack, containingBlock,
			absoluteBoxes, fixedBoxes, linePlaceholders, waitingFloats, lineChildren)
		newBox, resumeAt, preservedLineBreak, firstLetter, lastLetter, floatWidths = tmp.newBox, tmp.resumeAt, tmp.preservedLineBreak, tmp.firstLetter, tmp.lastLetter, tmp.floatWidths
	} else if bo.AtomicInlineLevelT.IsInstance(box_) {
		newBox = atomicBox(context, box_, positionX, skipStack, containingBlock.Box(), absoluteBoxes, fixedBoxes)
		newBox.Box().PositionX = positionX
		resumeAt = nil
		preservedLineBreak = false
		// See https://www.w3.org/TR/css-text-3/#line-breaking
		// Atomic inlines behave like ideographic characters.
		firstLetter = '\u2e80'
		lastLetter = '\u2e80'
	} else if bo.InlineFlexT.IsInstance(box_) {
		box.PositionX = positionX
		box.PositionY = 0
		resolveMarginAuto(box)
		// gopdfgo: inline-flex o szerokości auto dopasowuje się do treści jak inline-block
		// (CSS Flexbox 1 § 3); w źródle zajmował całą szerokość wiersza
		if box.Width == pr.AutoF && box.Style.GetWidth().Intrinsic == pr.IntrinsicNone {
			inlineBlockWidth(box_, context, containingBlock.Box())
		}
		var v blockLayout
		newBox, v = flexLayout(context, box_, -pr.Inf, skipStack,
			containingBlock.Box(), false, absoluteBoxes, fixedBoxes, false)
		resumeAt = v.resumeAt
		preservedLineBreak = false
		firstLetter = '\u2e80'
		lastLetter = '\u2e80'
	} else if bo.InlineGridT.IsInstance(box_) {
		box.PositionX = positionX
		box.PositionY = 0
		resolveMarginAuto(box)
		var v blockLayout
		newBox, v = gridLayout(context, box_, -pr.Inf, skipStack, containingBlock.Box(), false, absoluteBoxes, fixedBoxes)
		resumeAt = v.resumeAt
		preservedLineBreak = false
		firstLetter = '\u2e80'
		lastLetter = '\u2e80'
	} else { // pragma: no cover
		logger.WarningLogger.Printf("Layout for %v not handled yet", box)
		return splitedInline{}
	}

	if traceMode {
		traceLogger.DumpTree(newBox, fmt.Sprintf("end splitInlineLevel %s", resumeAt))
	}

	return splitedInline{
		newBox:             newBox,
		resumeAt:           resumeAt,
		preservedLineBreak: preservedLineBreak,
		firstLetter:        firstLetter,
		lastLetter:         lastLetter,
		floatWidths:        floatWidths,
	}
}

const (
	letterTrue  rune = -1
	letterFalse rune = -2
)

func isInBoxes(b Box, boxes []Box) bool {
	for _, v := range boxes {
		if v == b {
			return true
		}
	}
	return false
}

func breakWaitingChildren(context *layoutContext, box Box, bottomSpace pr.Float, initialSkipStack tree.ResumeStack,
	absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder,
	linePlaceholders *[]*AbsolutePlaceholder, waitingFloats *[]Box, lineChildren []indexedBox,
	children *[]indexedBoxC, waitingChildren []indexedBoxC,
) tree.ResumeStack {
	if len(waitingChildren) != 0 {
		// Too wide, let's try to cut inside waiting children,
		// starting from the end.
		// TODO: we should take care of children added into
		// absoluteBoxes, fixedBoxes and other lists.
		waitingChildrenCopy := append([]indexedBoxC{}, waitingChildren...)
		for len(waitingChildrenCopy) != 0 {
			var tmp indexedBoxC
			tmp, waitingChildrenCopy = waitingChildrenCopy[len(waitingChildrenCopy)-1], waitingChildrenCopy[:len(waitingChildrenCopy)-1]
			childIndex, child, originalChild := tmp.index, tmp.box, tmp.child
			if !child.Box().IsInNormalFlow() || canBreakInside(context, child) != pr.True {
				continue
			}

			childSkipStack := initialSkipStack[childIndex]

			// We break the waiting child at its last possible
			// breaking point.
			// TODO: The dirty solution chosen here is to
			// decrease the actual size by 1 and render the
			// waiting child again with this constraint. We may
			// find a better way.
			maxX := child.Box().PositionX + child.Box().MarginWidth() - 1
			var (
				hasBroken     bool
				newChild      Box
				childResumeAt tree.ResumeStack
			)
			for maxX > child.Box().PositionX {
				tmp := splitInlineLevel(context, originalChild, child.Box().PositionX, maxX, bottomSpace,
					childSkipStack, box, absoluteBoxes, fixedBoxes, linePlaceholders, waitingFloats, lineChildren)
				newChild, childResumeAt = tmp.newBox, tmp.resumeAt

				if childResumeAt != nil {
					hasBroken = true
					break
				}
				maxX -= 1
			}
			if !hasBroken { // else
				// No line break found
				continue
			}

			*children = append(*children, waitingChildrenCopy...)
			if newChild == nil {
				// May be nil where we have an empty TextBox.
				if !bo.TextT.IsInstance(child) {
					panic(fmt.Sprintf("only text box may yield empty child, got %s", child))
				}
			} else {
				*children = append(*children, indexedBoxC{index: childIndex, box: newChild, child: child})
			}
			return tree.ResumeStack{childIndex: childResumeAt}
		}
	}

	if l := len(*children); l != 0 {
		// Too wide, can't break waiting children and the inline is
		// non-empty: put child entirely on the next line.
		return tree.ResumeStack{(*children)[l-1].index + 1: nil}
	}

	return nil
}

// Same behavior as splitInlineLevel.
// the returned newBox has same concrete type has box_
func splitInlineBox(context *layoutContext, box_ Box, positionX, maxX, bottomSpace pr.Float, skipStack tree.ResumeStack,
	containingBlock Box, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder,
	linePlaceholders *[]*AbsolutePlaceholder, waitingFloats *[]Box, lineChildren []indexedBox,
) splitedInline {
	if !IsLine(box_) {
		panic(fmt.Sprintf("expected Line or Inline Box, got %T", box_))
	}
	box := box_.Box()

	initialSkipStack := skipStack
	// gopdfgo: pominięcie samych zwijanych spacji z początku wiersza nie czyni fragmentu kontynuacją;
	// w źródle pudełko liniowe zaczynające się od spacji traciło lewy margines, obramowanie i odstęp.
	isStart := (skipStack == nil || skipsOnlyLeadingSpaces(box_, skipStack)) && !box.SplitBefore
	var skip int
	if skipStack != nil {
		skip, skipStack = skipStack.Unpack()
	}

	if traceMode {
		traceLogger.DumpTree(box_, fmt.Sprintf("splitInlineBox %d %v", skip, tracer.FormatMaybeFloat(maxX)))
	}

	// In some cases (shrink-to-fit result being the preferred width)
	// maxX is coming from Pango itself,
	// but floating point errors have accumulated:
	//   width2 = (width + X) - X   // in some cases, width2 < width
	// Increase the value a bit to compensate and not introduce
	// an unexpected line break. The 1e-6 comes from tests.
	maxX *= 1 + 1e-6

	initialPositionX := positionX

	leftSpacing := box.PaddingLeft.V() + box.MarginLeft.V() + box.BorderLeftWidth.V()
	rightSpacing := box.PaddingRight.V() + box.MarginRight.V() + box.BorderRightWidth.V()
	if clone := box.Style.GetBoxDecorationBreak() == "clone"; !clone {
		// strona ucięta przez blok w środku pudełka (ustawiane w boxes.innerBlockInInline)
		ltr := box.Style.GetDirection() == pr.Ltr
		if (box.SplitBefore && ltr) || (box.SplitAfter && !ltr) {
			leftSpacing = 0
		}
		if (box.SplitAfter && ltr) || (box.SplitBefore && !ltr) {
			rightSpacing = 0
		}
	}
	contentBoxLeft := positionX
	// gopdfgo: dzieci są układane od positionX i dopiero na końcu przesuwane o lewy margines, obramowanie
	// i odstęp pudełka, więc miejsce w wierszu trzeba o nie zmniejszyć; w źródle pierwszy wiersz
	// pudełka liniowego z lewym odstępem wystawał o ten odstęp poza blok.
	if !bo.LineT.IsInstance(box_) && box.Style.GetDirection() == pr.Ltr &&
		(isStart || box.Style.GetBoxDecorationBreak() == "clone") && maxX < pr.Inf {
		maxX -= leftSpacing
	}

	if box.Style.GetPosition().String == "relative" {
		absoluteBoxes = &[]*AbsolutePlaceholder{}
	}

	var (
		i, floatResumeAt          int
		L                         = len(box.Children[skip:])
		resumeAt                  tree.ResumeStack
		children, waitingChildren []indexedBoxC
		firstLetter, lastLetter   rune
		lastStyle                 pr.ElementStyle // gopdfgo: styl poprzedniego dziecka w przepływie
		preservedLineBreak        = false
		floatWidths               widths
		// gopdfgo: położenie, od którego wiersz kończy się samymi wiszącymi odstępami (-1: nie kończy się
		// nimi); pozwala uznać za wiszący ciąg odstępów rozłożony na kilka pudełek
		hangStart pr.Float = -1
	)

	for ; i < L; i++ {
		index := i + skip
		child_ := box.Children[index]
		child := child_.Box()
		child.PositionY = box.PositionY
		if !child.IsInNormalFlow() {
			positionX = inlineOutOfFlowLayout(context, box_, containingBlock, index, child_, children, lineChildren,
				&waitingChildren, waitingFloats, absoluteBoxes, fixedBoxes, linePlaceholders, &floatWidths, maxX, positionX, bottomSpace)
			if child.IsFloated() {
				floatResumeAt = index + 1
				// gopdfgo: float lewy w wierszu przesunął już pozycję startową; odejmowanie jego
				// szerokości także od prawej krawędzi zwężało wiersz dwukrotnie.
				leftInLine := child.Style.GetFloat() == "left" && bo.LineT.IsInstance(box_)
				if !isInBoxes(child_, *waitingFloats) && !leftInLine {
					maxX -= child.MarginWidth()
				}
			}
			if context.floatOnlyLine {
				// gopdfgo: element pływający szerszy niż pusty wiersz zajmuje ten wiersz sam; dalsza treść
				// zaczyna następny wiersz, który schodzi pod element pływający, jeśli obok się nie mieści
				// (zob. inlineOutOfFlowLayout)
				context.floatOnlyLine = false
				if index+1 < len(box.Children) {
					children = append(children, waitingChildren...)
					waitingChildren = nil
					resumeAt = tree.ResumeStack{index + 1: nil}
					break
				}
			}
			continue
		}

		lastChild := index == len(box.Children)-1
		availableWidth := maxX
		var childWaitingFloats []Box
		childSkipStack := skipStack
		shapesBefore := len(context.excludedShapes.list)
		absoluteBefore, fixedBefore, placeholdersBefore := len(*absoluteBoxes), len(*fixedBoxes), len(*linePlaceholders)
		v := splitInlineLevel(context, child_, positionX, availableWidth, bottomSpace, skipStack,
			containingBlock, absoluteBoxes, fixedBoxes, linePlaceholders, &childWaitingFloats, lineChildren)
		resumeAt = v.resumeAt
		newChild, preserved, first, last, newFloatWidths := v.newBox, v.preservedLineBreak, v.firstLetter, v.lastLetter, v.floatWidths

		var endSpacing pr.Float
		if box.Style.GetDirection() == pr.Rtl {
			endSpacing = leftSpacing
			maxX -= newFloatWidths.left
		} else {
			endSpacing = rightSpacing
			maxX -= newFloatWidths.right
		}

		if lastChild && rightSpacing != 0 && resumeAt == nil {
			// TODO: we should take care of children added into absoluteBoxes,
			// fixedBoxes and other lists.
			availableWidth -= endSpacing
			// gopdfgo: elementy pływające umieszczone przy pierwszym układzie dziecka są układane jeszcze
			// raz, więc znikają z listy wykluczeń i oczekujących; w źródle zostawały i drugi egzemplarz
			// stawał obok pierwszego (test WPT float-in-inline-002)
			if len(context.excludedShapes.list) > shapesBefore {
				context.excludedShapes.list = context.excludedShapes.list[:shapesBefore]
			}
			childWaitingFloats = nil
			v := splitInlineLevel(context, child_, positionX, availableWidth, bottomSpace, skipStack,
				containingBlock, absoluteBoxes, fixedBoxes, linePlaceholders, &childWaitingFloats, lineChildren)
			newChild, resumeAt, preserved, first, last, newFloatWidths = v.newBox, v.resumeAt, v.preservedLineBreak, v.firstLetter, v.lastLetter, v.floatWidths
		}

		skipStack = nil
		if preserved {
			preservedLineBreak = true
		}

		var canBreak pr.MaybeBool
		if lastLetter == letterTrue {
			lastLetter = ' '
		} else if lastLetter == letterFalse {
			lastLetter = ' ' // no-break space
		} else if box.Style.GetWhiteSpace() == pr.Pre || box.Style.GetWhiteSpace() == pr.Nowrap {
			canBreak = pr.False
		}
		if canBreak == nil {
			if lastLetter == 0 || first == 0 {
				canBreak = pr.False
			} else if first == letterTrue {
				canBreak = pr.True
			} else if first == letterFalse {
				canBreak = pr.False
			} else if (lastLetter == '\u2e80' || first == '\u2e80') && !keepsUAXBreaking(lastLetter+first-'\u2e80') {
				// (odst\u0119py, znaki steruj\u0105ce i spacja zerowej szeroko\u015bci zostaj\u0105 przy regu\u0142ach UAX 14:
				// nie \u0142amiemy przed wymuszonym podzia\u0142em ani przed spacj\u0105)
				// gopdfgo: przed elementem atomowym (obraz, inline-block) i po nim wiersz wolno złamać
				// zawsze, także obok znaku, który zwykle zabrania podziału (np. dwukropka) i obok spacji
				// nierozdzielającej U+00A0; sklejają tylko pozostałe znaki klas GL, WJ i ZWJ (CSS Text 3
				// § 5.1). W źródle granica była sprawdzana według UAX 14 z elementem jako znakiem ID.
				canBreak = pr.True
				if other := lastLetter + first - '\u2e80'; other != '\u2e80' && isGlueClass(other) {
					canBreak = pr.False
				}
			} else {
				// gopdfgo: przy word-break: break-all litery i cyfry liczą się jak znaki ideograficzne
				// (CSS Text 3 § 5.2) także na granicy elementów: liczy się styl strony, z której pochodzi
				// znak. W źródle granica elementów zawsze sprawdzała same znaki.
				a, b := lastLetter, first
				if lastStyle != nil && lastStyle.GetWordBreak() == "break-all" && (unicode.IsLetter(a) || unicode.IsDigit(a)) {
					a = '\u4e00'
				}
				if child.Style.GetWordBreak() == "break-all" && (unicode.IsLetter(b) || unicode.IsDigit(b)) {
					b = '\u4e00'
				}
				canBreak = context.Fonts().CanBreakText([]rune{a, b})
			}
		}

		if canBreak == pr.True {
			children = append(children, waitingChildren...)
			waitingChildren = nil
		}

		// gopdfgo: pudełko z wrap-inside: avoid, które musiało się złamać w środku, przechodzi w całości
		// do następnego wiersza, jeśli tuż przed nim wolno złamać wiersz (CSS Text 4, #wrap-inside: podział
		// wewnątrz tylko w ostateczności). Pudełko zewnętrzne sprawdza to przed wewnętrznym, więc ma
		// pierwszeństwo. Źródło właściwości nie znało.
		if resumeAt != nil && canBreak == pr.True && !preserved && childSkipStack == nil &&
			child.Style.GetWrapInside() == "avoid" && bo.InlineT.IsInstance(child_) && hasInFlowBox(children) {
			*absoluteBoxes, *fixedBoxes, *linePlaceholders = (*absoluteBoxes)[:absoluteBefore], (*fixedBoxes)[:fixedBefore], (*linePlaceholders)[:placeholdersBefore]
			context.excludedShapes.list = context.excludedShapes.list[:shapesBefore]
			resumeAt = tree.ResumeStack{index: nil}
			break
		}

		if firstLetter == 0 {
			firstLetter = first
		}
		if child.TrailingCollapsibleSpace {
			lastLetter = letterTrue
		} else {
			lastLetter = last
		}
		previousStyle := lastStyle
		lastStyle = child.Style

		if asTextBox, ok := newChild.(*bo.TextBox); newChild == nil || (ok && asTextBox == nil) {
			// May be nil where we have an empty TextBox.
		} else {
			// Store lines to get previous break points.
			if bo.LineT.IsInstance(box_) {
				lineChildren = append(lineChildren, indexedBox{index: index, box: newChild})
			}
			// Check that text doesn’t overflow.
			newPositionX := newChild.Box().PositionX + newChild.Box().MarginWidth()
			// gopdfgo: pudełko o zerowej szerokości (np. <wbr>, pusty element) nie może przepełnić wiersza;
			// gdy wiersz wystaje już przed nim, to przez wiszące odstępy poprzedniego tekstu, przyjęte
			// wcześniej (CSS Text 3 § 4.1.3; test WPT trailing-other-space-separators-002). Źródło
			// sprawdzało je jak każde inne i łamało wiersz przed tym tekstem.
			// gopdfgo: pudełko złożone z samych wiszących odstępów przedłuża ciąg odstępów poprzedniego
			// pudełka, więc treść wiersza kończy się tam, gdzie ten ciąg się zaczyna (CSS Text 3 § 4.1.3)
			width, trailing := newChild.Box().MarginWidth(), trailingWhitespaceSize(context, newChild, true)+trailingPreservedWidth(newChild)
			contentEnd := newPositionX - trailing
			if width != 0 && trailing >= width {
				contentEnd = newChild.Box().PositionX
				if hangStart >= 0 {
					contentEnd = hangStart
				}
			}
			if width != 0 {
				hangStart = -1
				if trailing > 0 {
					hangStart = contentEnd
				}
			}
			if width != 0 && contentEnd > maxX {
				// Text overflows, find previous break point.
				previousResumeAt := breakWaitingChildren(context, containingBlock, bottomSpace, initialSkipStack, absoluteBoxes, fixedBoxes,
					linePlaceholders, waitingFloats, lineChildren, &children, waitingChildren)
				if previousResumeAt != nil {
					resumeAt = previousResumeAt
					break
				}
				// gopdfgo: gdy w wierszu nie ma żadnego miejsca podziału, a styl pozwala łamać
				// w dowolnym miejscu (overflow-wrap: anywhere lub break-word, word-break: break-all,
				// line-break: anywhere), wiersz łamie się na granicy elementów liniowych, przed
				// pudełkiem, które się nie mieści (CSS Text 3 § 5.5). W źródle taki podział był
				// możliwy tylko wewnątrz jednego fragmentu tekstu.
				// Tylko na poziomie wiersza: dopiero tu wiadomo, że wcześniej w wierszu nie było
				// miejsca podziału (słowo, które mieści się w następnym wierszu, przechodzi do niego całe).
				// gopdfgo: granica elementów jest awaryjnym miejscem podziału także wtedy, gdy łamanie
				// w dowolnym miejscu pozwala styl tekstu przed nią (overflow-wrap: anywhere na elemencie,
				// który kończy się przed pudełkiem niemieszczącym się w wierszu)
				if bo.LineT.IsInstance(box_) && !breaksAnywhere(child.Style, false) && previousStyle != nil &&
					breaksAnywhere(previousStyle, false) && !breakAllOnly(previousStyle) && len(waitingChildren) != 0 {
					children = append(children, waitingChildren...)
					waitingChildren = nil
					resumeAt = tree.ResumeStack{index: nil}
					break
				}
				if bo.LineT.IsInstance(box_) && breaksAnywhere(child.Style, false) {
					// najpierw próba złamania samego pudełka tak, żeby jego początek dopełnił wiersz
					savedForce := context.forceWordBreak
					context.forceWordBreak = true
					retry := splitInlineLevel(context, child_, positionX, availableWidth, bottomSpace, childSkipStack,
						containingBlock, absoluteBoxes, fixedBoxes, linePlaceholders, &childWaitingFloats, lineChildren)
					context.forceWordBreak = savedForce
					if part := retry.newBox; part != nil && retry.resumeAt != nil && !isNilTextBox(part) &&
						part.Box().PositionX+part.Box().MarginWidth()-trailingWhitespaceSize(context, part, true)-trailingPreservedWidth(part) <= maxX {
						children = append(children, waitingChildren...)
						children = append(children, indexedBoxC{index: index, box: part, child: child_})
						waitingChildren = nil
						resumeAt = tree.ResumeStack{index: retry.resumeAt}
						break
					}
					if len(waitingChildren) != 0 && (!breakAllOnly(child.Style) || unicode.IsLetter(first) || unicode.IsDigit(first)) {
						children = append(children, waitingChildren...)
						waitingChildren = nil
						resumeAt = tree.ResumeStack{index: nil}
						break
					}
				}
			}

			positionX = newPositionX
			waitingChildren = append(waitingChildren, indexedBoxC{index: index, box: newChild, child: child_})
		}
		*waitingFloats = append(*waitingFloats, childWaitingFloats...)
		if resumeAt != nil {
			children = append(children, waitingChildren...)
			resumeAt = tree.ResumeStack{index: resumeAt}
			break
		}
	}
	if i == L {
		children = append(children, waitingChildren...)
		resumeAt = nil
	}

	// Reorder inline blocks when direction is rtl
	if box.Style.GetDirection() == pr.Rtl && len(children) > 1 {
		var inFlowChildren []Box
		for _, child := range children {
			if child.box.Box().IsInNormalFlow() {
				inFlowChildren = append(inFlowChildren, child.box)
			}
		}
		posX := inFlowChildren[0].Box().PositionX
		for _, child := range reversedBoxes(inFlowChildren) {
			child.Translate((posX - child.Box().PositionX), 0, true)
			posX += child.Box().MarginWidth()
		}
	}

	isEnd := resumeAt == nil && !box.SplitAfter
	toCopy := make([]Box, len(children))
	for i, boxChild := range children {
		toCopy[i] = boxChild.box
	}

	newBox_ := bo.CopyWithChildren(box_, toCopy)
	newBox := newBox_.Box()
	newBox_.RemoveDecoration(newBox, !isStart, !isEnd)

	if bo.LineT.IsInstance(box_) {
		// We must reset line box width according to its new children
		newBox.Width = pr.Float(0)
		children := newBox.Children
		if newBox.Style.GetDirection() == pr.Ltr {
			children = reversedBoxes(children)
		}
		for _, boxChild := range children {
			if boxChild.Box().IsInNormalFlow() {
				newBox.Width = boxChild.Box().PositionX + boxChild.Box().MarginWidth() - newBox.PositionX
				break
			}
		}
	} else {
		newBox.PositionX = initialPositionX
		var translationNeeded bool
		if box.Style.GetBoxDecorationBreak() == "clone" {
			translationNeeded = true
		} else if box.Style.GetDirection() == pr.Ltr {
			translationNeeded = isStart
		} else {
			translationNeeded = isEnd
		}
		if translationNeeded {
			for _, child := range newBox.Children {
				// gopdfgo: element pływający leży względem bloku
				// zawierającego, nie pudełka liniowego (CSS 2.1 § 9.5.1),
				// więc margines, obramowanie i odstęp pudełka go nie
				// przesuwają; źródło przesuwało go razem z tekstem
				if child.Box().IsFloated() {
					continue
				}
				child.Translate(leftSpacing, 0, true)
			}
		}
		newBox.Width = positionX - contentBoxLeft
		newBox_.Translate(floatWidths.left, 0, true)
	}
	lineHeight, baseline := text.StrutLayout(box.Style, context)
	newBox.Baseline = baseline
	newBox.Height = text.ContentAreaHeight(box.Style, context)
	leadingTop, leadingBottom := text.SplitLeading(lineHeight-newBox.Height.V(), box.Style.GetLineHeight().Tag == pr.Normal)
	// Set margins to the half leading but also compensate for borders and
	// paddings. We want marginHeight() == lineHeight
	newBox.MarginTop = leadingTop - newBox.BorderTopWidth.V() - newBox.PaddingTop.V()
	newBox.MarginBottom = leadingBottom - newBox.BorderBottomWidth.V() - newBox.PaddingBottom.V()

	if newBox.Style.GetPosition().String == "relative" {
		for _, absoluteBox := range *absoluteBoxes {
			absoluteLayout(context, absoluteBox, newBox_, fixedBoxes, bottomSpace, nil)
		}
	}

	if resumeAt != nil {
		resumeIndex, _ := resumeAt.Unpack()
		if resumeIndex < floatResumeAt {
			resumeAt = tree.ResumeStack{floatResumeAt: nil}
		}
	}

	if box.IsLeader {
		firstLetter = letterTrue
		lastLetter = letterFalse
	}

	return splitedInline{
		newBox:             newBox_,
		resumeAt:           resumeAt,
		preservedLineBreak: preservedLineBreak,
		firstLetter:        firstLetter,
		lastLetter:         lastLetter,
		floatWidths:        floatWidths,
	}
}

func (context *layoutContext) addRunning(child_ bo.Box) {
	runningName := child_.Box().Style.GetPosition().String
	currentRE, has := context.runningElements[runningName]
	if !has {
		currentRE = map[int][]Box{}
		context.runningElements[runningName] = currentRE
	}
	currentRE[context.currentPage] = append(currentRE[context.currentPage], child_)
}

type indexedBoxC struct {
	index int
	box   Box
	child Box
}

// gopdfgo: zwraca bieżącą pozycję x po uwzględnieniu floata lewego umieszczonego w wierszu. W źródle
// funkcja nic nie zwracała, a przesunięcie pozycji (w WeasyPrint zmienna pętli) ginęło, więc tekst
// pierwszego wiersza nachodził na float, np. na pływającą literę ::first-letter.
func inlineOutOfFlowLayout(context *layoutContext, box Box, containingBlock Box, index int, child_ Box, children []indexedBoxC,
	lineChildren []indexedBox, waitingChildren *[]indexedBoxC, waitingFloats *[]Box,
	absoluteBoxes, fixedBoxes, linePlaceholders *[]*AbsolutePlaceholder, floatWidths *widths,
	maxX, positionX, bottomSpace pr.Float,
) pr.Float {
	if traceMode {
		traceLogger.DumpTree(box, "inlineOutOfFlowLayout")
		traceLogger.Dump(fmt.Sprintf("is absolute: %v", child_.Box().IsAbsolutelyPositioned()))
	}

	child := child_.Box()
	if child.IsAbsolutelyPositioned() {
		child.PositionX = positionX
		placeholder := NewAbsolutePlaceholder(child_)
		*linePlaceholders = append(*linePlaceholders, placeholder)
		*waitingChildren = append(*waitingChildren, indexedBoxC{index: index, box: placeholder, child: child_})
		if child.Style.GetPosition().String == "absolute" {
			*absoluteBoxes = append(*absoluteBoxes, placeholder)
		} else {
			*fixedBoxes = append(*fixedBoxes, placeholder)
		}
	} else if child.IsFloated() {
		child.PositionX = positionX
		floatWidth := shrinkToFit(context, child_, containingBlock.Box().Width.V())

		// To retrieve the real available space for floats, we must remove
		// the trailing whitespaces from the line
		var nonFloatingChildren []Box
		for _, v := range append(children, *waitingChildren...) {
			if !v.box.Box().IsFloated() {
				nonFloatingChildren = append(nonFloatingChildren, v.box)
			}
		}
		if Lf := len(nonFloatingChildren); Lf != 0 {
			floatWidth -= trailingWhitespaceSize(context, nonFloatingChildren[Lf-1], true)
		}

		// gopdfgo: w pustym jeszcze wierszu element pływający szerszy niż wiersz i tak nigdzie się nie
		// zmieści, więc staje na wysokości wiersza, a wiersz schodzi pod niego (CSS 2.1 § 9.5.1, reguła 8:
		// jak najwyżej; test WPT float-wider-than-line-inside-inline-with-block). W źródle czekał na koniec
		// wiersza i stawał pod nim.
		emptyLine := len(lineChildren) == 0 && len(children) == 0 && len(*waitingChildren) == 0
		if floatWidth > maxX-positionX && emptyLine && len(*waitingFloats) == 0 {
			context.floatOnlyLine = true
		}
		if (floatWidth > maxX-positionX && !emptyLine) || len(*waitingFloats) != 0 {
			// TODO: the absolute and fixed boxes in the floats must be
			// added here, and not in iterLineBoxes
			*waitingFloats = append(*waitingFloats, child_)
		} else {
			newChild, floatResumeAt := floatLayout(context, child_, containingBlock.Box(), absoluteBoxes, fixedBoxes,
				bottomSpace, nil)

			if floatResumeAt != nil {
				context.setBrokenOutOfFlow(child_, brokenBox{box: child_, containingBlock: containingBlock, resumeAt: floatResumeAt})
			}
			*waitingChildren = append(*waitingChildren, indexedBoxC{index: index, box: newChild, child: child_})
			child_ = newChild
			child = child_.Box()

			// Translate previous line children
			dx := max(child.MarginWidth(), 0)
			floatWidths.add(child.Style.GetFloat(), dx)
			if child.Style.GetFloat() == "left" {
				if bo.LineT.IsInstance(box) {
					// The parent is the line, update the current position
					// for the next child. When the parent is not the line
					// (it is an inline block), the current position of the
					// line is updated by the box itself (see next
					// splitInlineLevel call).
					positionX += dx
				}
			} else if child.Style.GetFloat() == "right" {
				// Update the maximum x position for the next children
				maxX -= dx
			}
			for _, oldChild := range lineChildren {
				if !oldChild.box.Box().IsInNormalFlow() {
					continue
				}
				if (child.Style.GetFloat() == "left" && box.Box().Style.GetDirection() == pr.Ltr) ||
					(child.Style.GetFloat() == "right" && box.Box().Style.GetDirection() == pr.Rtl) {
					oldChild.box.Translate(dx, 0, true)
				}
			}
		}
	} else if child.IsRunning() {
		context.addRunning(child_)
	}
	return positionX
}

// See https://unicode.org/reports/tr14/
// \r is already handled by processWhitespace
var lineBreaks = utils.NewSet("\n", "\t", "\f", "\u0085", "\u2028", "\u2029")

// Keep as much text as possible from a TextBox in a limited width.
//
// Try not to overflow but always have some text in the returned [TextBox]
//
// Also returns 'skip', the number of
// runes to skip form the start of the TextBox for the next line, or
// -1 if all of the text fits.
//
// Also break on preserved line breaks.
func splitTextBox(context *layoutContext, box *bo.TextBox, availableWidth pr.MaybeFloat,
	skip int, isLineStart bool,
) (_ *bo.TextBox, _ int, _ bool) {
	return splitTextBoxAt(context, box, availableWidth, skip, isLineStart, 0, -1)
}

// splitTextBoxAt (gopdfgo): jak splitTextBox, dla tekstu zaczynającego się tabOffset za początkiem wiersza.
func splitTextBoxAt(context *layoutContext, box *bo.TextBox, availableWidth pr.MaybeFloat,
	skip int, isLineStart bool, tabOffset, tabUnit pr.Float,
) (_ *bo.TextBox, _ int, _ bool) {
	fontSize := box.Style.GetFontSize()
	text_ := box.Text[skip:]
	if fontSize == pr.FToV(0) && len(text_) != 0 {
		// gopdfgo: tekst o zerowym rozmiarze fontu nie zajmuje miejsca, ale zachowany znak nowego
		// wiersza nadal łamie wiersz (CSS Text 3 § 4.1.1; test WPT white-space-zero-fontsize-001).
		// Źródło pomijało cały tekst. Po zwinięciu białych znaków znak nowego wiersza zostaje tylko
		// tam, gdzie white-space go zachowuje.
		if i := slices.Index(text_, '\n'); i >= 0 {
			return nil, skip + i + 1, true
		}
	}
	// gopdfgo: tekst o zerowym rozmiarze fontu jest układany jak każdy inny (ma zerową szerokość i
	// wysokość), bo wiersz z nim nie jest pusty: marginesy nie zwijają się przez jego blok (CSS 2 § 8.3.1,
	// test WPT font-size-zero-1). Źródło taki tekst pomijało; pomijamy nadal sam zwijalny odstęp, który
	// wiersza i tak nie wypełnia (zob. isPhantomLinebox).
	if len(text_) == 0 || (fontSize == pr.FToV(0) && isCollapsibleSpace(box.CopyWithText(text_))) {
		return nil, -1, false
	}
	var style pr.StyleAccessor = box.Style
	if context.noAutoHyphens && box.Style.GetHyphens() == "auto" {
		style = manualHyphensStyle{box.Style}
	}
	v := text.SplitFirstLineAt(text_, style, context, availableWidth, false, isLineStart, tabOffset, tabUnit)
	layout, length, resumeIndex, width, height := v.Layout, v.Length, v.ResumeAt, v.Width, v.Height
	if resumeIndex == 0 {
		panic("resumeAt should not be 0 here")
	}

	if traceMode {
		traceLogger.Dump(fmt.Sprintf("splitTextBox (availableWidth %v) <%q> (len: %d) resumeIndex: %d", tracer.FormatMaybeFloat(availableWidth), string(text_), len(text_), resumeIndex))
	}

	if newText := layout.Text(); length > 0 {
		box = box.CopyWithText(newText)
		// gopdfgo: układ wiersza dzielonego zawiera znak dzielenia, którego nie ma w tekście
		box.Hyphenated = resumeIndex != -1 && len(newText) > length
		box.SplitInside = resumeIndex != -1
		box.Width = width
		box.TextLayout = layout
		box.FirstLineIsRTL = v.FirstLineRTL
		// "The height of the content area should be based on the font,
		//  but this specification does not specify how."
		// https://www.w3.org/TR/CSS21/visudet.html#inline-non-replaced
		// We trust Pango and use the height of the LayoutLine.
		box.Height = height
		// "only the "line-height" is used when calculating the height
		//  of the line box."
		// Set margins so that marginHeight() == lineHeight
		lineHeight, strutBaseline := text.StrutLayout(box.Style, context)
		leadingTop, leadingBottom := text.SplitLeading(lineHeight-height, box.Style.GetLineHeight().Tag == pr.Normal)
		if box.Style.GetLineHeight().Tag != pr.Normal {
			// gopdfgo: przy podanym line-height pudełko układa się według metryk pierwszego dostępnego
			// fontu, także gdy część glifów pochodzi z fontu zastępczego (CSS Inline 3 § 4.2). Linia
			// bazowa stoi tam, gdzie w rozpórce wiersza; w źródle liczyły się metryki fontu zastępczego,
			// więc jeden znak spoza fontu (symbol, łącznik nierozdzielający) powiększał wiersz.
			leadingTop = strutBaseline - v.Baseline
			leadingBottom = lineHeight - height - leadingTop
		}
		box.MarginTop = leadingTop
		box.MarginBottom = leadingBottom
		// form the top of the content box
		box.Baseline = v.Baseline + box.MarginTop.V() // form the top of the margin box
	} else {
		box = nil
	}

	preservedLineBreak := false
	if resumeIndex != -1 {
		if resumeIndex > len(text_) {
			resumeIndex = len(text_)
		}
		if length > resumeIndex {
			length = resumeIndex
		}
		between := string(text_[length:resumeIndex])
		preservedLineBreak = len(strings.Trim(between, " ")) != 0
		// gopdfgo: znaki między wierszami, których nie ma na liście znanych podziałów (np. U+000B,
		// U+000C: klasa BK w UAX 14), też liczą się jako zachowany podział wiersza. W źródle silnik
		// zgłaszał tu panikę na poprawnym dokumencie.
		resumeIndex += skip
	}

	return box, resumeIndex, preservedLineBreak
}

// addSoftHyphenAtBoxEnd (gopdfgo) pokazuje znak dzielenia, gdy wiersz złamał się na granicy
// elementów tuż po miękkim łączniku (U+00AD) kończącym pudełko tekstu: miękki łącznik jest miejscem
// podziału, a przy podziale w nim widać znak z hyphenate-character (CSS Text 3 § 5.4). Źródło
// dodawało znak tylko przy podziale wewnątrz jednego pudełka tekstu.
func addSoftHyphenAtBoxEnd(context *layoutContext, line Box) {
	var chain []Box
	box := line
	for IsLine(box) {
		chain = append(chain, box)
		children := box.Box().Children
		box = nil
		for i := len(children) - 1; i >= 0; i-- {
			if children[i].Box().IsInNormalFlow() && !isNilTextBox(children[i]) {
				box = children[i]
				break
			}
		}
	}
	textBox, ok := box.(*bo.TextBox)
	if !ok || textBox.Hyphenated || textBox.SplitInside || len(textBox.Text) == 0 || textBox.Text[len(textBox.Text)-1] != '\u00ad' ||
		textBox.Style.GetHyphens() == "none" || textBox.TextLayout == nil {
		return
	}
	hyphenated := append(slices.Clone(textBox.Text), []rune(string(textBox.Style.GetHyphenateCharacter()))...)
	v := text.SplitFirstLine(hyphenated, textBox.Style, context, pr.Inf, false, false)
	if v.Layout == nil {
		return
	}
	delta := v.Width - textBox.Width.V()
	textBox.Text, textBox.TextLayout, textBox.Hyphenated = v.Layout.Text(), v.Layout, true
	textBox.Width = v.Width
	for _, parent := range chain {
		parent.Box().Width = parent.Box().Width.V() + delta
	}
}

// manualHyphensStyle (gopdfgo) wyłącza automatyczne dzielenie wyrazów (hyphens: auto działa jak
// manual) po wyczerpaniu limitu hyphenate-limit-lines (CSS Text 4, #hyphenate-limit-lines).
type manualHyphensStyle struct{ pr.StyleAccessor }

func (manualHyphensStyle) GetHyphens() pr.String { return "manual" }

// endsHyphenated mówi, czy wiersz kończy się dzieleniem wyrazu.
func endsHyphenated(box Box) bool {
	if textBox, ok := box.(*bo.TextBox); ok {
		return textBox.Hyphenated
	}
	if !IsLine(box) {
		return false
	}
	children := box.Box().Children
	for i := len(children) - 1; i >= 0; i-- {
		if children[i].Box().IsInNormalFlow() && !isNilTextBox(children[i]) {
			return endsHyphenated(children[i])
		}
	}
	return false
}

type boxMinMax struct {
	box      Box
	max, min pr.MaybeFloat
}

// Handle “vertical-align“ within an `LineBox` (or of a
// non-align sub-tree).
// Place all boxes vertically assuming that the baseline of “box“
// is at `y = 0`.
// Return “(maxY, minY)“, the maximum and minimum vertical position
// of margin boxes.
func lineBoxVerticality(context *layoutContext, box Box) (pr.Float, pr.Float) {
	var topBottomSubtrees []Box
	maxY, minY := alignedSubtreeVerticality(context, box, &topBottomSubtrees, 0)
	subtreesWithMinMax := make([]boxMinMax, 0, len(topBottomSubtrees))
	for i := 0; i < len(topBottomSubtrees); i++ { // note that topBottomSubtrees may grow over this loop
		subtree := topBottomSubtrees[i]
		var subMaxY, subMinY pr.MaybeFloat
		if !subtree.Box().IsFloated() {
			subMaxY, subMinY = alignedSubtreeVerticality(context, subtree, &topBottomSubtrees, 0)
		}
		subtreesWithMinMax = append(subtreesWithMinMax, boxMinMax{box: subtree, max: subMaxY, min: subMinY})
	}

	// gopdfgo: pudełko z vertical-align: bottom wyższe niż wiersz powiększa go w górę, a pudełko z top
	// w dół (CSS 2.1 § 10.8.1 tego nie rozstrzyga; tak robi Chromium, test WPT vertical-align-121).
	// W źródle wiersz zawsze rósł w dół, więc przy bottom treść wyrównana do linii bazowej stała u góry.
	for _, subtree := range subtreesWithMinMax {
		if subtree.box.Box().IsFloated() {
			continue
		}
		height := subtree.max.V() - subtree.min.V()
		if subtree.box.Box().Style.GetVerticalAlign().Tag == pr.Bottom {
			minY = min(minY, maxY-height)
		} else {
			maxY = max(maxY, minY+height)
		}
	}

	for _, v := range subtreesWithMinMax {
		va := v.box.Box().Style.GetVerticalAlign()
		var dy pr.Float
		if v.box.Box().IsFloated() {
			dy = minY - v.box.Box().PositionY
		} else if va.Tag == pr.Top {
			dy = minY - v.min.V()
		} else if va.Tag == pr.Bottom {
			dy = maxY - v.max.V()
		} else {
			panic(fmt.Sprintf("expected top or bottom, got %v", va))
		}
		translateSubtree(v.box, dy)
	}
	return maxY, minY
}

func translateSubtree(box Box, dy pr.Float) {
	if bo.InlineT.IsInstance(box) {
		box.Box().PositionY += dy
		if va := box.Box().Style.GetVerticalAlign().Tag; va == pr.Top || va == pr.Bottom {
			for _, child := range box.Box().Children {
				// gopdfgo: zagnieżdżone poddrzewo z top/bottom jest przesuwane osobno (lineBoxVerticality);
				// w źródle przesuwało się dwa razy (test WPT vertical-align-nested-top-001)
				if cva := child.Box().Style.GetVerticalAlign().Tag; child.Box().IsInNormalFlow() && (cva == pr.Top || cva == pr.Bottom) {
					continue
				}
				translateSubtree(child, dy)
			}
		}
	} else {
		// Text or atomic boxes
		box.Translate(0, dy, true)
	}
}

func alignedSubtreeVerticality(context *layoutContext, box Box, topBottomSubtrees *[]Box, baselineY pr.Float) (pr.Float, pr.Float) {
	maxY, minY := inlineBoxVerticality(context, box, topBottomSubtrees, baselineY)
	// Account for the line box itself :
	top := baselineY - box.Box().Baseline.V()
	bottom := top + box.Box().MarginHeight()
	if minY == nil || top < minY.V() {
		minY = top
	}
	if maxY == nil || bottom > maxY.V() {
		maxY = bottom
	}

	return maxY.V(), minY.V()
}

// Handle “vertical-align“ within an `InlineBox`.
//
//	Place all boxes vertically assuming that the baseline of ``box``
//	is at `y = baselineY`.
//	Return ``(maxY, minY)``, the maximum and minimum vertical position
//	of margin boxes.
func inlineBoxVerticality(context *layoutContext, box_ Box, topBottomSubtrees *[]Box, baselineY pr.Float) (maxY, minY pr.MaybeFloat) {
	if !IsLine(box_) {
		return maxY, minY
	}
	box := box_.Box()
	for _, child_ := range box_.Box().Children {
		child := child_.Box()
		if !child.IsInNormalFlow() {
			if child.IsFloated() {
				*topBottomSubtrees = append(*topBottomSubtrees, child_)
			}
			continue
		}
		var childBaselineY pr.Float
		verticalAlign := child.Style.GetVerticalAlign()
		switch verticalAlign.Tag {
		case pr.Baseline:
			childBaselineY = baselineY
		case pr.Middle:
			oneEx := box.Style.GetFontSize().Value * text.CharacterRatio(box.Style, box.Style.Cache(), false, context.fontConfig)
			top := baselineY - (oneEx+child.MarginHeight())/2.
			childBaselineY = top + child.Baseline.V()
		case pr.TextTop:
			// align top with the top of the parent’s content area
			top := baselineY - box.Baseline.V() + box.MarginTop.V() +
				box.BorderTopWidth.V() + box.PaddingTop.V()
			childBaselineY = top + child.Baseline.V()
		case pr.TextBottom:
			// align bottom with the bottom of the parent’s content area
			bottom := baselineY - box.Baseline.V() + box.MarginTop.V() +
				box.BorderTopWidth.V() + box.PaddingTop.V() + box.Height.V()
			childBaselineY = bottom - child.MarginHeight() + child.Baseline.V()
		case pr.Top, pr.Bottom:
			// TODO: actually implement vertical-align: top and bottom
			// Later, we will assume for this subtree that its baseline
			// is at y=0.
			childBaselineY = 0
		default:
			// Numeric value: The child’s baseline is `verticalAlign` above
			// (lower y) the parent’s baseline.
			childBaselineY = baselineY - verticalAlign.Value
		}

		// the child’s `top` is `child.Baseline` above (lower y) its baseline.
		top := childBaselineY - child.Baseline.V()
		if bo.InlineBlockT.IsInstance(child_) || bo.InlineFlexT.IsInstance(child_) || bo.InlineGridT.IsInstance(child_) {
			// This also includes table wrappers for inline tables.
			child_.Translate(0, top-child.PositionY, false)
		} else {
			child.PositionY = top
			// grand-children for inline boxes are handled below
		}

		if verticalAlign.Tag == pr.Top || verticalAlign.Tag == pr.Bottom {
			// top or bottom are special, they need to be handled in
			// a later pass.
			*topBottomSubtrees = append(*topBottomSubtrees, child_)
			continue
		}

		bottom := top + child.MarginHeight()
		if minY == nil || top < minY.V() {
			minY = top
		}
		if maxY == nil || bottom > maxY.V() {
			maxY = bottom
		}
		if bo.InlineT.IsInstance(child_) {
			childrenMaxY, childrenMinY := inlineBoxVerticality(context, child_, topBottomSubtrees, childBaselineY)
			if childrenMinY != nil && childrenMinY.V() < minY.V() {
				minY = childrenMinY
			}
			if childrenMaxY != nil && childrenMaxY.V() > maxY.V() {
				maxY = childrenMaxY
			}
		}
	}
	return maxY, minY
}

// Return how much the line should be moved horizontally according to
// the `text-align` property.
func textAlign(context *layoutContext, line *bo.LineBox, availableWidth pr.Float, last bool, hang pr.Float) pr.Float {
	// "When the total width of the inline-level boxes on a line is less than
	// the width of the line box containing them, their horizontal distribution
	// within the line box is determined by the "text-align" property."
	if line.Width.V()-hang >= availableWidth {
		return 0
	}
	align := line.Style.GetTextAlignAll()
	if last {
		alignLast := line.Style.GetTextAlignLast()
		if alignLast != "auto" {
			align = alignLast
		} else if align == "justify" {
			// gopdfgo: text-align-last: auto przy text-align-all: justify wyrównuje ostatni wiersz do
			// początku (CSS Text 3 § 6.3); w źródle robił to skrót text-align, który ustawiał start
			align = "start"
		}
	}
	ws := line.Style.GetWhiteSpace()
	spaceCollapse := ws == pr.Normal || ws == pr.Nowrap || ws == pr.PreLine

	if align == "left" || align == "right" {
		if (align == "left") != (line.Style.GetDirection() == pr.Rtl) { // xor
			align = "start"
		} else {
			align = "end"
		}
	}

	if align == "start" {
		return 0
	}

	offset := availableWidth - (line.Width.V() - hang)
	switch align {
	case "justify":
		// gopdfgo: tekst z zachowanymi spacjami, który się zawija (pre-wrap, break-spaces), też jest
		// justowany. CSS Text 3 § 6.4 zostawia to do wyboru; tak robi Chromium. W źródle justowany
		// był tylko tekst ze zwijanymi spacjami.
		if spaceCollapse || ws == pr.PreWrap || ws == pr.BreakSpaces {
			justifyLine(context, line, offset, hang != 0)
		}
		return 0
	case "center":
		return offset / 2
	case "end":
		return offset
	}
	panic(fmt.Sprintf("align should be 'justify', 'center' or 'end', got %s", align))
}

func justifyLine(context *layoutContext, line *bo.LineBox, extraWidth pr.Float, hanging bool) {
	// TODO: We should use a better alorithm here, see
	// https://www.w3.org/TR/css-text-3/#justify-algos
	// gopdfgo: spacje wiszące na końcu wiersza (pre-wrap) nie są miejscami justowania
	// gopdfgo: w wierszu z tabulatorem justujemy tylko część po ostatnim tabulatorze, a tekst przed nim
	// zostaje na pozycjach tabulacji (CSS Text 3 § 6.4: tabulator zachowany wyznacza granicę, za którą
	// justowanie nie sięga; tak robi Chromium). Źródło justowało cały wiersz.
	if line.Style.GetDirection() != pr.Rtl {
		limitJustificationToLastTab(line)
	} else if lineHasTab(line) {
		return
	}
	nbSpaces := countExpandableSpaces(line)
	if hanging {
		if count, _, last := trailingPreservedSpaces(line); last != nil && last.TextLayout != nil {
			last.TextLayout.SetHangingSpaces(count)
			if last.Style.GetTextJustify() != "none" { // gopdfgo: te spacje nie były policzone
				nbSpaces -= count
			}
		}
	}
	if nbSpaces > 0 {
		addWordSpacing(context, line, extraWidth/pr.Float(nbSpaces), 0)
	}
}

// trailingPreservedWidth zwraca szerokość zachowanych spacji i tabulatorów na samym końcu wiersza
// (white-space: pre-wrap), czyli tę część wiersza, która może wisieć.
func trailingPreservedWidth(box Box) pr.Float {
	_, _, last := trailingPreservedSpaces(box)
	if last == nil || last.TextLayout == nil || last.FirstLineIsRTL {
		return 0
	}
	return last.TextLayout.TrailingSpacesWidth()
}

// trailingPreservedSpaces szuka ostatniego tekstu w wierszu i liczy jego końcowe zachowane spacje
// (white-space: pre-wrap). Puste pudełka liniowe na końcu wiersza są pomijane.
func trailingPreservedSpaces(box Box) (count int, found bool, last *bo.TextBox) {
	if textBox, ok := box.(*bo.TextBox); ok {
		if len(textBox.Text) == 0 {
			return 0, false, nil
		}
		if textBox.Style.GetWhiteSpace() != pr.PreWrap {
			return 0, true, nil
		}
		for i := len(textBox.Text) - 1; i >= 0 && textBox.Text[i] == ' '; i-- {
			count++
		}
		return count, true, textBox
	}
	if !IsLine(box) {
		return 0, true, nil
	}
	children := box.Box().Children
	for i := len(children) - 1; i >= 0; i-- {
		if !children[i].Box().IsInNormalFlow() {
			continue
		}
		if count, found, last = trailingPreservedSpaces(children[i]); found {
			return count, true, last
		}
	}
	return 0, false, nil
}

// breaksAnywhere mówi, czy styl pozwala złamać wiersz między dowolnymi znakami. Z intrinsic liczą
// się tylko wartości, które zmniejszają też szerokość min-content (overflow-wrap: break-word nie).
func breaksAnywhere(style pr.ElementStyle, intrinsic bool) bool {
	if ws := style.GetWhiteSpace(); ws == pr.Nowrap || ws == pr.Pre {
		return false
	}
	switch style.GetOverflowWrap() {
	case "anywhere":
		return true
	case "break-word":
		if !intrinsic {
			return true
		}
	}
	if style.GetWordBreak() == "break-word" || style.GetLineBreak() == "anywhere" {
		return true
	}
	// word-break: break-all łamie tylko między literami i cyframi; na granicy elementów sprawdza to
	// wołający (breakAllOnly), a przy szerokości min-content granicy nie liczymy
	return !intrinsic && style.GetWordBreak() == "break-all"
}

// breakAllOnly: łamanie w dowolnym miejscu wynika wyłącznie z word-break: break-all, które zachowuje
// reguły dla znaków przestankowych (wiersz nie zaczyna się od kropki).
func breakAllOnly(style pr.ElementStyle) bool {
	return style.GetWordBreak() == "break-all" && style.GetOverflowWrap() == "normal" && style.GetLineBreak() != "anywhere"
}

// keepsUAXBreaking: obok tych znaków (spacja, znaki sterujące i wymuszone podziały, spacja zerowej
// szerokości) granica elementu atomowego zostaje przy regułach UAX 14.
func keepsUAXBreaking(r rune) bool {
	return r == ' ' || unicode.IsControl(r) || r == '\u200b' || r == '\u2028' || r == '\u2029'
}

// isGlueClass mówi, czy znak należy do klasy GL, WJ albo ZWJ z UAX 14 (bez U+00A0): obok takiego
// znaku nie wolno złamać wiersza także przy elemencie atomowym (CSS Text 3 § 5.1).
func isGlueClass(r rune) bool {
	switch {
	case r == '\u034f', r >= '\u035c' && r <= '\u0362', r == '\u0f08', r == '\u0f0c', r == '\u0f12',
		r == '\u0fd9', r == '\u0fda', r == '\u180e', r == '\u2007', r == '\u2011', r == '\u202f', // GL
		r == '\u2060', r == '\ufeff', // WJ
		r == '\u200d': // ZWJ
		return true
	}
	return false
}

// hasInFlowContent mówi, czy w pudełku liniowym jest treść w przepływie (tekst albo pudełko atomowe);
// puste pudełka liniowe się nie liczą.
func hasInFlowContent(box Box) bool {
	for _, child := range box.Box().Children {
		if !child.Box().IsInNormalFlow() || isNilTextBox(child) {
			continue
		}
		if textBox, ok := child.(*bo.TextBox); ok && len(textBox.Text) == 0 {
			continue
		}
		if !IsLine(child) || hasInFlowContent(child) {
			return true
		}
	}
	return false
}

// hasInFlowBox mówi, czy wśród pudełek jest choć jedno w normalnym przepływie.
func hasInFlowBox(boxes []indexedBoxC) bool {
	for _, b := range boxes {
		if b.box.Box().IsInNormalFlow() && !isNilTextBox(b.box) {
			return true
		}
	}
	return false
}

func isNilTextBox(box Box) bool {
	textBox, ok := box.(*bo.TextBox)
	return ok && textBox == nil
}

// limitJustificationToLastTab wyłącza z justowania spacje przed ostatnim tabulatorem wiersza
// (wiersz od lewej do prawej).
func limitJustificationToLastTab(line Box) {
	var texts []*bo.TextBox
	var collect func(box Box)
	collect = func(box Box) {
		if textBox, ok := box.(*bo.TextBox); ok {
			texts = append(texts, textBox)
		} else if IsLine(box) {
			for _, child := range box.Box().Children {
				if child.Box().IsInNormalFlow() {
					collect(child)
				}
			}
		}
	}
	collect(line)
	last := -1
	for i := len(texts) - 1; i >= 0 && last < 0; i-- {
		if tab := lastIndexRune(texts[i].Text, '\t'); tab >= 0 {
			last = i
			if texts[i].TextLayout != nil {
				texts[i].TextLayout.SetJustificationStart(tab + 1)
			}
		}
	}
	for _, textBox := range texts[:max(last, 0)] {
		if textBox.TextLayout != nil {
			textBox.TextLayout.SetJustificationStart(len(textBox.Text))
		}
	}
}

func lastIndexRune(text []rune, r rune) int {
	for i := len(text) - 1; i >= 0; i-- {
		if text[i] == r {
			return i
		}
	}
	return -1
}

func lineHasTab(box Box) bool {
	if textBox, isTextBox := box.(*bo.TextBox); isTextBox {
		return slices.Contains(textBox.Text, '\t')
	}
	if IsLine(box) {
		for _, child := range box.Box().Children {
			if lineHasTab(child) {
				return true
			}
		}
	}
	return false
}

// Count expandable spaces (space and nbsp) for justification.
func countExpandableSpaces(box Box) int {
	if textBox, isTextBox := box.(*bo.TextBox); isTextBox {
		// gopdfgo: tekst z text-justify: none nie ma miejsc justowania (CSS Text 3 § 6.4); właściwość
		// działa na pudełkach liniowych, więc liczy się styl tekstu, a nie wiersza. Źródło jej nie znało.
		if textBox.Style.GetTextJustify() == "none" {
			return 0
		}
		// TODO: remove trailing spaces correctly
		text := textBox.Text
		if textBox.TextLayout != nil { // gopdfgo: bez spacji przed ostatnim tabulatorem
			text = text[min(textBox.TextLayout.JustificationStart(), len(text)):]
		}
		var count int
		for _, r := range text {
			if isWordSeparator(r) {
				count++
			}
		}
		return count
	} else if IsLine(box) {
		var sum int
		for _, child := range box.Box().Children {
			sum += countExpandableSpaces(child)
		}
		return sum
	} else {
		return 0
	}
}

// isWordSeparator (gopdfgo) mówi, czy znak jest separatorem słów, czyli miejscem justowania
// (CSS Text 3 § 6.4 i 4.3: spacja, spacja nierozdzielająca i separatory słów innych pism). Lista
// zgadza się z tą, którą go-text poszerza przy justowaniu (shaping.AddWordSpacing); w źródle liczyły
// się tylko spacja i spacja nierozdzielająca, więc odstęp wychodził za szeroki.
func isWordSeparator(r rune) bool {
	switch r {
	case ' ', '\u00a0', '\u1361', '\U00010100', '\U00010101', '\U0001039F', '\U0001091F':
		return true
	}
	return false
}

func addWordSpacing(context *layoutContext, box_ Box, justificationSpacing, xAdvance pr.Float) pr.Float {
	if textBox, isTextBox := box_.(*bo.TextBox); isTextBox {
		// textBox.JustificationSpacing = justificationSpacing
		textBox.PositionX += xAdvance
		nbSpaces := pr.Float(countExpandableSpaces(box_) - textBox.TextLayout.HangingSpaces())
		if nbSpaces > 0 {
			textBox.TextLayout.SetJustification(justificationSpacing)
			extraSpace := justificationSpacing * nbSpaces
			xAdvance += extraSpace
			textBox.Width = textBox.Width.V() + extraSpace
		}
	} else if IsLine(box_) {
		box := box_.Box()
		box.PositionX += xAdvance
		previousXAdvance := xAdvance
		children := slices.Clone(box.Children)
		if box.Style.GetDirection() == pr.Rtl {
			slices.Reverse(children)
		}
		for _, child := range children {
			if child.Box().IsInNormalFlow() {
				xAdvance = addWordSpacing(context, child, justificationSpacing, xAdvance)
			}
		}
		box.Width = box.Width.V() + xAdvance - previousXAdvance
	} else {
		// Atomic inline-level box
		box_.Translate(xAdvance, 0, false)
	}
	return xAdvance
}

// Shttps://www.w3.org/TR/CSS21/visuren.html#phantom-line-box
func isPhantomLinebox(linebox *bo.BoxFields) bool {
	for _, child_ := range linebox.Children {
		child := child_.Box()
		if bo.InlineT.IsInstance(child_) {
			if !isPhantomLinebox(child) {
				return false
			}
			// gopdfgo: liczą się tylko marginesy, obramowania i odstępy w osi wiersza (CSS Inline 3 § 2.1,
			// "non-zero inline-axis margins, padding, or borders"), i tylko po stronach, których fragment
			// nie stracił przez blok w środku. W źródle wiersz przestawał być pusty także przez
			// obramowanie górne lub dolne.
			ltr := child.Style.GetDirection() == pr.Ltr
			skipLeft := (child.SplitBefore && ltr) || (child.SplitAfter && !ltr)
			skipRight := (child.SplitAfter && ltr) || (child.SplitBefore && !ltr)
			if child.Style.GetBoxDecorationBreak() == "clone" {
				skipLeft, skipRight = false, false
			}
			if !skipLeft && (getDimOrS(child, pr.PMarginLeft).Value != 0 ||
				getDimOrS(child, pr.PBorderLeftWidth).Value != 0 || getDimOrS(child, pr.PPaddingLeft).Value != 0) {
				return false
			}
			if !skipRight && (getDimOrS(child, pr.PMarginRight).Value != 0 ||
				getDimOrS(child, pr.PBorderRightWidth).Value != 0 || getDimOrS(child, pr.PPaddingRight).Value != 0) {
				return false
			}
		} else if isCollapsibleSpace(child_) {
			// gopdfgo: wiersz z samymi zwijalnymi odstępami nie ma tekstu (odstępy na początku i końcu
			// wiersza znikają, CSS Text 3 § 4.1.2), więc też ma zerową wysokość (CSS 2.1 § 9.4.2).
			// W źródle taki wiersz, np. między dwoma blokami wewnątrz elementów liniowych, zajmował
			// pełną wysokość.
			continue
		} else if child.IsInNormalFlow() {
			return false
		}
	}
	return true
}

// isCollapsibleSpace mówi, czy pudełko jest tekstem złożonym wyłącznie ze zwijalnych odstępów.
func isCollapsibleSpace(box Box) bool {
	textBox, ok := box.(*bo.TextBox)
	if !ok {
		return false
	}
	switch textBox.Style.GetWhiteSpace() {
	case pr.Normal, pr.Nowrap, pr.PreLine:
	default:
		return false
	}
	for _, r := range textBox.TextS() {
		if r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}

func canBreakInside(ctx *layoutContext, box Box) pr.MaybeBool {
	// See https://www.w3.org/TR/css-text-3/#white-space-property
	ws := box.Box().Style.GetWhiteSpace()
	textWrap := ws == pr.Normal || ws == pr.PreWrap || ws == pr.PreLine || ws == pr.BreakSpaces
	textBox, isTextBox := box.(*bo.TextBox)
	if bo.AtomicInlineLevelT.IsInstance(box) {
		return pr.False
	} else if textWrap && isTextBox {
		return ctx.Fonts().CanBreakText(textBox.Text)
	} else if textWrap && bo.ParentT.IsInstance(box) {
		for _, child := range box.Box().Children {
			if canBreakInside(ctx, child) == pr.True {
				return pr.True
			}
		}
		return pr.False
	}
	return pr.False
}
