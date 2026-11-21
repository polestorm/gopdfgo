// Transform a "before layout" box tree into an "after layout" tree,
// by breaking boxes across lines and pages; and determining the position and dimension
// of each box fragment.
//
// Boxes in the new tree have `used values` in their PositionX,
// PositionY, Width and Height attributes, amongst others.
// (see https://www.w3.org/TR/CSS21/cascade.html#used-value)
//
// The laid out pages are ready to be printed or display on screen,
// which is done by the higher level `document` package.
package layout

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/polestorm/gopdfgo/internal/webrender/css/counters"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
	"github.com/polestorm/gopdfgo/internal/webrender/images"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/text/hyphen"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
	"github.com/polestorm/gopdfgo/internal/webrender/utils/testutils/tracer"
	"golang.org/x/net/html"
)

// if true, save a structured trace in an external file
// this is a costly operation, and should only be used for debugging purpose
const traceMode = false

var traceLogger tracer.Tracer // used only when traceMode is true

func init() {
	if traceMode {
		traceLogger = tracer.NewTracer(filepath.Join(os.TempDir(), "trace_go.txt"))
	}
}

type Box = bo.Box

// Layout lay out the whole document, returning one box per pages.
//
// This includes line breaks, page breaks, absolute size and position for all
// boxes.
func Layout(html *tree.HTML, stylesheets []tree.CSS, presentationalHints bool, fontConfig text.FontConfiguration) []*bo.PageBox {
	pages := layoutScaled(html, stylesheets, presentationalHints, fontConfig, 1)
	// gopdfgo: treść szersza niż obszar strony jest pomniejszana, żeby się zmieściła (ADR 0014): dokument
	// układamy drugi raz na obszarze strony powiększonym tyle razy, ile wynosi pomniejszenie, a rysujemy
	// w odpowiedniej skali. Dokument, w którym nic nie wystaje, jest układany raz.
	if limit := pr.Float(html.Behavior.MaxShrink); limit > 1 {
		if scale := min(contentOverflowScale(pages), limit); scale > 1.001 {
			pages = layoutScaled(html, stylesheets, presentationalHints, fontConfig, scale)
		}
	}
	return pages
}

func layoutScaled(html *tree.HTML, stylesheets []tree.CSS, presentationalHints bool, fontConfig text.FontConfiguration,
	contentScale pr.Float,
) []*bo.PageBox {
	context := newLayoutContext(html, stylesheets, presentationalHints, fontConfig, make(counters.CounterStyle))
	counterStyle := context.counterStyle
	context.contentScale = contentScale

	logger.ProgressLogger.Println("Step 4 - Creating formatting structure")

	rootBox := bo.BuildFormattingStructure(html.Root, context.styleFor, context.resolver,
		html.BaseUrl, &context.TargetCollector, counterStyle, &context.footnotes)

	return layoutDocument(html, rootBox, context, -1)
}

// Initialize “context.pageMaker“.
// Collect the pagination's states required for page based counters.
func initializePageMaker(context *layoutContext, rootBox bo.BoxFields) {
	context.pageMaker = nil

	// Special case the root box
	pageBreak := rootBox.Style.GetBreakBefore()

	// TODO: take care of text direction and writing mode
	// https://www.w3.org/TR/css3-page/#progression
	var rightPage bool
	switch pageBreak {
	case "right":
		rightPage = true
	case "left":
		rightPage = false
	case "recto":
		rightPage = rootBox.Style.GetDirection() == pr.Ltr
	case "verso":
		rightPage = rootBox.Style.GetDirection() == pr.Rtl
	default:
		rightPage = rootBox.Style.GetDirection() == pr.Ltr
	}
	pv, _ := rootBox.PageValues()
	nextPage := tree.PageBreak{Break: "any", Page: pv, HasPage: true}

	// pageState is prerequisite for filling in missing page based counters
	// although neither a variable quoteDepth nor counterScopes are needed
	// in page-boxes -- reusing
	// `formattingStructure.bo.updateCounters()` to avoid redundant
	// code requires a full `state`.
	// The value of **pages**, of course, is unknown until we return and
	// might change when "contentChanged" triggers re-pagination...
	// So we start with an empty state
	pageState := tree.PageState{
		// Shared mutable objects:
		QuoteDepth:    []int{0}, // quoteDepth: single integer
		CounterValues: tree.CounterValues{"pages": []int{0}},
		CounterScopes: []utils.Set{utils.NewSet("pages")}, // counterScopes
	}

	// gopdfgo: liczniki założone na elemencie głównym są widoczne w kontekście strony, więc
	// counter-increment w @page zwiększa licznik dokumentu, a nie zakłada własnego od zera
	// (testy druku css-page/margin-boxes/content-010 i 011). W źródle kontekst strony zaczynał bez nich.
	for _, reset := range rootBox.Style.GetCounterReset().Values {
		if reset.String == "pages" || reset.String == "page" || reset.Reversed {
			continue
		}
		pageState.CounterValues[reset.String] = []int{reset.Int}
		pageState.CounterScopes[0].Add(reset.String)
	}

	// Initial values
	remakeState := tree.RemakeState{}
	context.pageMaker = append(context.pageMaker, tree.PageMaker{
		ResumeAt: nil, NextPage: nextPage, RightPage: rightPage,
		PageState: pageState, RemakeState: remakeState,
	})
}

// Lay out and yield the fixed boxes of “pages“.
//
// gopdfgo: pudełko fixed jest powtarzane na stronie raz (CSS 2.1 § 9.6.1). Element w powtarzanej stopce albo
// nagłówku tabeli (oraz w układach próbnych, które potem odrzucono) rejestruje się na wielu stronach,
// także kilka razy na jednej; źródło dokładało każdą rejestrację, więc pudełko było rysowane 7–8 razy
// (test WPT css-break/table/repeated-section/fixedpos-in-footer-forced-break). Pomijamy elementy, które
// strona zawierająca ma już sama, i powtórzenia tego samego elementu; seen jest wspólne dla obu wywołań
// dla jednej strony.
func layoutFixedBoxes(context *layoutContext, pages []*bo.PageBox, containingPage *bo.PageBox, seen map[fixedKey]bool) []Box {
	var out []Box
	for _, page := range pages {
		for _, box := range page.FixedBoxes {
			if key := fixedKeyOf(box); seen[key] {
				continue
			} else {
				seen[key] = true
			}
			// As replaced boxes are never copied during layout, ensure that we
			// have different boxes (with a possibly different layout) for
			// each pages.
			if bo.ReplacedT.IsInstance(box) {
				box = box.Copy()
			}
			// Absolute boxes in fixed boxes are rendered as fixed boxes'
			// children, even when they are fixed themselves.
			var absoluteBoxes []*AbsolutePlaceholder
			b, _ := absoluteBoxLayout(context, box, containingPage, &absoluteBoxes, -pr.Inf, nil)
			out = append(out, b)
			for len(absoluteBoxes) != 0 {
				var newAbsoluteBoxes []*AbsolutePlaceholder
				for _, absBox := range absoluteBoxes {
					absoluteLayout(context, absBox, containingPage, &newAbsoluteBoxes, -pr.Inf, nil)
				}
				absoluteBoxes = newAbsoluteBoxes
			}
		}
	}
	return out
}

func layoutDocument(doc *tree.HTML, rootBox bo.BlockLevelBoxITF, context *layoutContext, maxLoops int) []*bo.PageBox {
	initializePageMaker(context, *rootBox.Box())
	markTablesWithoutColumnPercentages(rootBox, false) // gopdfgo: zob. TableBox.NoColumnPercentages
	if maxLoops == -1 {
		maxLoops = 8 // default value
	}
	var (
		pages             []*bo.PageBox
		originalFootnotes = append([]Box(nil), context.footnotes...) // copy
	)
	actualTotalPages := 0

	for loop := 0; loop < maxLoops; loop += 1 {
		if loop > 0 {
			logger.ProgressLogger.Printf("Step 5 - Creating layout - Repagination #%d \n", loop)
			context.footnotes = append([]Box(nil), originalFootnotes...)
		}

		initialTotalPages := actualTotalPages
		pages = context.makeAllPages(rootBox, doc, pages)
		actualTotalPages = len(pages)

		// Check whether another round is required
		reloopContent := false
		reloopPages := false
		for _, pageData := range context.pageMaker {
			// Update pages
			pageCounterValues := pageData.PageState.CounterValues
			pageCounterValues["pages"] = []int{actualTotalPages}
			if pageData.RemakeState.ContentChanged {
				reloopContent = true
			}
			if pageData.RemakeState.PagesWanted {
				reloopPages = initialTotalPages != actualTotalPages
			}
		}

		// No need for another loop, stop here
		if !reloopContent && !reloopPages {
			break
		}
	}

	// gopdfgo: dalsza obróbka stron (zakładki, liczniki stron w string-set i bookmark-label, pudełka
	// fixed, pola marginesowe, tła) jest w pageFinisher (stream.go), wspólnym z rysowaniem w trakcie
	// układu (ADR 0027); kolejność kroków jest taka jak w źródle.
	finisher := newPageFinisher(context)
	for i, page := range pages {
		finisher.collect(i, page)
	}

	attachLateBoxes(context, pages)

	out := make([]*bo.PageBox, len(pages))
	// Add margin boxes
	for i, page := range pages {
		finisher.placeFixedBoxes(pages[:i], pages[i+1:], page)
		scaledHeight := finisher.prepare(i, page)
		page.Children = append(page.Children, finisher.marginBoxes(i, page)...)
		finisher.backgrounds(i, page, scaledHeight)
		out[i] = page

		if traceMode {
			traceLogger.DumpTree(page, fmt.Sprintf("Final page %d", i))
		}
	}
	return out
}

var _ text.TextLayoutContext = (*layoutContext)(nil)

type brokenBox struct {
	box             Box
	containingBlock Box
	resumeAt        tree.ResumeStack
	order           int // kolejność zgłoszenia, zob. setBrokenOutOfFlow
	// deferredPages > 0: pudełko pozycjonowane leży poniżej strony, na której zostało ułożone; liczba mówi,
	// o ile stron dalej jesteśmy teraz względem tamtej strony
	deferredPages int
	// fromPage (gopdfgo): numer strony, w której układzie współrzędnych pudełko zostało ułożone
	// (layoutContext.geometryPage); przesunięcie odłożonego pudełka liczymy z geometrii stron od tamtej
	fromPage int
	// icb (gopdfgo): blokiem zawierającym jest blok początkowy (strona), liczony od pierwszej strony
	// (zob. icbFor); dalszy ciąg i odłożone pudełko układamy względem bloku początkowego bieżącej strony
	icb bool
	// columnX (gopdfgo): położenie kolumny, w której pudełko zostało podzielone (zob. layoutContext.columnX)
	columnX pr.Float
	// pushed (gopdfgo): element pływający przeniesiony w całości na następną stronę (zob. pushFloat);
	// pushedFrom to element jego rodzica (zob. dropStalePushedFloats), nil dla elementu przeniesionego
	// dalej z początku strony
	pushed     bool
	pushedFrom *html.Node
	// nextColumn (gopdfgo): pudełko pozycjonowane, które w kolumnie nie zmieściło żadnej treści i w całości
	// zaczyna się w następnej kolumnie (zob. absoluteLayout)
	nextColumn bool
	// owner (gopdfgo): element kontenera kolumn, w którego kolumnie pudełko zostało podzielone (zob.
	// layoutContext.columnOwner); dalszy ciąg bierze tylko ten kontener, także gdy jest zagnieżdżony
	owner *html.Node
}

// gopdfgo: pudełka poza przepływem podzielone między strony są kontynuowane w kolejności zgłoszenia.
// W źródle kolejność wynikała z iteracji po mapie, czyli była losowa: przy dwóch takich pudełkach
// wynik (które rysuje się na wierzchu) zmieniał się między uruchomieniami.

// setBrokenOutOfFlow zapamiętuje dalszy ciąg pudełka poza przepływem wraz z kolejnością zgłoszenia.
func (l *layoutContext) setBrokenOutOfFlow(key Box, broken brokenBox) {
	l.brokenSequence++
	broken.order = l.brokenSequence
	broken.columnX = l.columnX
	broken.owner = l.columnOwner
	broken.fromPage = l.geometryPage
	l.brokenOutOfFlow[key] = broken
}

// Elementy pływające przenoszone na następną stronę (gopdfgo). Element pływający, który nie mieści się
// w reszcie strony i nie może się podzielić (treść niepodzielna, np. obraz, albo break-inside: avoid),
// przechodzi na następną stronę sam, a treść w przepływie, która stoi za nim w źródle, zostaje na tej
// stronie i zajmuje całą szerokość. CSS 2.1 § 9.5.1 (reguły 5 i 6) ogranicza położenie elementu
// pływającego tylko przez treść wcześniejszą w źródle, więc późniejsze wiersze mogą stać wyżej; tak drukują
// Chromium i Gecko (wzorzec WPT CSS2/pagination/float-page-break-inside-avoid-8; pomiar w Chrome 157
// 2026-11-21, docs/feature-audit.md). W źródle element zatrzymywał rodzica: cały akapit przechodził razem
// z nim i zostawiał pustą resztę strony, a element pływający w środku wiersza był ucinany na dole strony.
//
// Przeniesiony element pływający jest zgłaszany jak dalszy ciąg elementu podzielonego (brokenOutOfFlow)
// bez miejsca wznowienia, więc na następnej stronie jest układany od początku przed treścią w przepływie
// (makePage). Kluczem jest pudełko ze źródła: kolejne układy próbne tego samego miejsca nadpisują jedno
// zgłoszenie. Gdy rodzic i tak przechodzi na następną stronę, przepływ dochodzi tam do tego samego elementu
// jeszcze raz i go pomija (pushedFloatsPlaced).

// canPushFloat mówi, czy element pływający może przejść na następną stronę bez treści, która stoi za nim.
// Tylko w zwykłym przepływie stron: nie w kolumnach, tabelach, flex, siatce, pudełkach przycinających
// ani w treści innych pudełek poza przepływem, gdzie dalszy ciąg na następnej stronie stawałby względem
// strony, a nie swojego kontenera. Także tylko w kontekście formatowania blokowego strony: pudełko, które
// zakłada własny kontekst (flow-root, overflow inny niż visible), musiałoby wtedy ciągnąć się na następną
// stronę, żeby objąć przeniesiony element (CSS 2.1 § 10.6.7; wzorce WPT css-page/page-size-007/008), a tego
// mechanizm nie robi; tam zostaje zachowanie źródła.
func (l *layoutContext) canPushFloat(floatBox *bo.BoxFields) bool {
	return !l.inColumn && l.flexMeasuring == 0 && l.clipDepth == 0 && l.outOfFlowDepth == 0 && l.independentDepth == 0 &&
		len(l.excludedShapesLists) == 1 &&
		l.pageBottom != pr.Inf && !l.behavior.Continuous &&
		(floatBox.PositionY > l.pageBottom-l.pageHeight+0.5 || math.IsNaN(float64(floatBox.PositionY)))
}

// removeExcludedShape usuwa z listy wykluczeń element pływający, który jednak nie stanie na tej stronie.
func (l *layoutContext) removeExcludedShape(box *bo.BoxFields) {
	if l.excludedShapes == nil || l.excludedShapes.isFrozen {
		return
	}
	l.excludedShapes.list = slices.DeleteFunc(l.excludedShapes.list, func(b *bo.BoxFields) bool { return b == box })
}

// pushFloat zgłasza element pływający box do ułożenia od początku na następnej stronie.
func (l *layoutContext) pushFloat(box Box, containingBlock Box) {
	l.setBrokenOutOfFlow(box, brokenBox{box: box, containingBlock: containingBlock, pushed: true,
		pushedFrom: containingBlock.Box().Element})
}

// dropStalePushedFloats usuwa zgłoszenia przeniesienia z rodziców, których na ułożonej stronie nie ma, bo
// w całości przeszli na następną (np. blok o podanej wysokości, ADR 0004, albo break-inside: avoid).
// Rodzic ułożony od nowa na następnej stronie sam dojdzie do elementu pływającego; zgłoszenie stawiałoby go
// tam przed rodzicem, który mógłby wtedy dostać pod nim clearance.
func (l *layoutContext) dropStalePushedFloats(root Box) {
	stale := false
	for _, v := range l.brokenOutOfFlow {
		stale = stale || v.pushed
	}
	if !stale {
		return
	}
	present := map[*html.Node]bool{}
	var walk func(Box)
	walk = func(b Box) {
		present[b.Box().Element] = true
		for _, child := range b.Box().Children {
			walk(child)
		}
	}
	walk(root)
	for key, v := range l.brokenOutOfFlow {
		if v.pushed && v.pushedFrom != nil && !present[v.pushedFrom] {
			delete(l.brokenOutOfFlow, key)
		}
	}
}

// hasPushedFloat mówi, czy na tej stronie jakiś element pływający przeszedł już na następną; późniejsze
// elementy pływające nie mogą stanąć wyżej niż on (CSS 2.1 § 9.5.1, reguła 5), więc przechodzą za nim.
func (l *layoutContext) hasPushedFloat() bool {
	for _, v := range l.brokenOutOfFlow {
		if v.pushed {
			return true
		}
	}
	return false
}

// floatPushedAway mówi, że element pływający nie stoi w układanym właśnie wierszu, bo przeszedł na
// następną stronę albo już stoi na tej, przeniesiony z poprzedniej.
func (l *layoutContext) floatPushedAway(box Box) bool {
	v, ok := l.brokenOutOfFlow[box]
	return (ok && v.pushed) || l.pushedFloatPlaced(box)
}

// pushedFloatPlaced mówi, że element pływający przeniesiony z poprzedniej strony stoi już na tej stronie.
func (l *layoutContext) pushedFloatPlaced(box Box) bool {
	page, ok := l.pushedFloatsPlaced[box]
	return ok && page == l.currentPage
}

// orderedBroken zwraca wpisy mapy w kolejności zgłoszenia.
func orderedBroken(broken map[Box]brokenBox) []brokenBox {
	out := make([]brokenBox, 0, len(broken))
	for _, v := range broken {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].order < out[j].order })
	return out
}

type shapeList struct {
	list     []*bo.BoxFields
	isFrozen bool
}

func (sl *shapeList) append(box *bo.BoxFields) {
	if sl.isFrozen {
		return
	}
	sl.list = append(sl.list, box)
}

// layoutContext stores the global context needed during layout,
// such as various caches.
type layoutContext struct {
	// gopdfgo: początek układanego właśnie wiersza (krawędź bloku), od którego liczą się pozycje
	// tabulacji; inLine mówi, czy wartość jest ustawiona
	forceWordBreak bool // gopdfgo: zob. splitInlineLevel
	// gopdfgo: wyczerpany limit hyphenate-limit-lines: następny wiersz nie dzieli wyrazów automatycznie
	noAutoHyphens bool
	// gopdfgo: w kontenerze line-clamp za układanym pudełkiem jest jeszcze treść (zob.
	// lineBoxeIterator.clampContentAfter)
	clampContentAfter bool
	lineStartX        pr.Float
	lineTabUnit       pr.Float
	inLine            bool
	// gopdfgo: pudełka pozycjonowane bezwzględnie, których blokiem zawierającym jest pudełko liniowe
	// układanego wiersza; układamy je po wyrównaniu wiersza w pionie (zob. getNextLinebox)
	deferredInlineAbsolutes *[]inlineAbsolute

	// caches
	stringSet       map[string]map[int][]string
	runningElements map[string]map[int][]Box
	// gopdfgo: startChains[n]: startChain strony n zapamiętany przed zwolnieniem jej drzewa (ADR 0027)
	startChains  map[int][]pr.StringSet
	strutLayouts map[text.StrutLayoutKey][2]pr.Float
	tables       map[*bo.TableBox]map[bool]tableContentWidths

	resolver            bo.URLResolver
	fontConfig          text.FontConfiguration
	behavior            tree.Behavior // gopdfgo: przełączniki zachowań (tree.HTML.Behavior)
	TargetCollector     tree.TargetCollector
	counterStyle        counters.CounterStyle
	dictionaries        map[text.HyphenDictKey]hyphen.Hyphener
	styleFor            *tree.StyleFor
	pageMaker           []tree.PageMaker
	excludedShapes      *shapeList
	excludedShapesLists []shapeList
	brokenOutOfFlow     map[Box]brokenBox
	brokenSequence      int
	// gopdfgo: obszary stron ułożonych w bieżącym przebiegu (indeks = numer strony - 1), zob. pageArea
	pageAreas []pageArea
	// gopdfgo: numer strony, której obszar odpowiada współrzędnym bieżącego układu; zwykle bieżąca
	// strona, przy układzie fragmentów na wcześniejszych stronach (layoutEarlierFragments) tamta strona
	geometryPage int
	// gopdfgo: fragmenty pudełek pozycjonowanych dołożone do wcześniejszych, już ułożonych stron;
	// klucz to numer strony, której układ je wytworzył (wpisy kasujemy, gdy ta strona jest układana od
	// nowa), a dokładamy je do stron po zakończeniu paginacji (layoutDocument)
	lateBoxes map[int][]lateBox
	// gopdfgo: ostatnio układana strona umieściła dalszy ciąg pudełka poza przepływem albo czeka na
	// pudełko odłożone (zob. makePage)
	outOfFlowProgress bool
	// gopdfgo: położenie pierwszego fragmentu pudełka pozycjonowanego w osi fragmentacji oraz kopie
	// dalszych fragmentów przesunięte do tego położenia jako bloki zawierające potomków (zob. absoluteBlock)
	fragmentStarts map[Box]pr.Float
	shiftedBlocks  map[Box]bool
	// gopdfgo: elementy pływające podzielone w poprzedniej kolumnie, do kontynuacji na górze następnej
	// (zob. columnsLayout, takeColumnFloats)
	columnFloats []columnFloat
	// columnX (gopdfgo): położenie najgłębszej układanej właśnie kolumny; dalszy ciąg pudełka poza
	// przepływem zachowuje w następnej kolumnie odległość od jej lewej krawędzi
	columnX pr.Float
	// columnOwner (gopdfgo): element najgłębszego układanego właśnie kontenera kolumn (nil poza kolumnami)
	columnOwner *html.Node
	// gopdfgo: ile kolumn zostaje w bieżącym rzędzie kontenera kolumn za kolumną układaną teraz
	columnsLeft int
	// flexMeasuring > 0 oznacza układ próbny elementu flex (pomiar rozmiaru); kontenery flex nie dzielą się
	// wtedy między strony, żeby pomiar dał pełną wysokość treści.
	flexMeasuring int
	gridMeasure   *gridMeasure // gopdfgo: trwający pomiar szerokości wewnętrznej siatki
	// gopdfgo: odsunięcia elementów wyrównanych do linii bazowej doliczane do wkładu w wysokość wierszy
	// rozwiązywanej właśnie siatki (CSS Grid 1 § 11.5 krok 1), klucz to pudełko elementu
	gridShims map[Box]pr.Float

	// gopdfgo: pudełka pozycjonowane względem komórki tabeli, do ponownego ułożenia po ustaleniu
	// wysokości wiersza; klucz to ułożona komórka
	cellPositioned map[*bo.BoxFields][]cellPositioned
	// gopdfgo: pudełka absolute zakotwiczone dołem, czekające na ostatni fragment bloku zawierającego
	// (klucz: pudełko bloku sprzed układu; zob. deferBottomAnchoredAbsolutes)
	pendingBottomAbsolutes map[*bo.BoxFields][]Box
	// gopdfgo: outOfFlowDepth > 0 w trakcie układu treści pudełka pozycjonowanego albo pływającego; zmiana
	// nazwy strony łamie stronę tylko między pudełkami w przepływie kontekstu fragmentacji (CSS Page 3
	// § 6.1 i CSS Break 3 § 4.1 — podziały klasy A), nie wewnątrz takich pudełek
	outOfFlowDepth int

	// gopdfgo: contentScale > 1: obszar strony jest powiększony tyle razy na czas układu treści
	// (pomniejszanie treści szerszej niż strona, ADR 0014)
	contentScale pr.Float

	// gopdfgo: dokładne wymiary obszaru strony i marginesy z arkusza sprzed zaokrąglenia do pikseli
	// (ADR 0021), według numeru strony; pola marginesów układamy według nich
	exactPageGeometry map[int][5]pr.Float

	// gopdfgo: pageDone, gdy nie jest nil, makeAllPages wywołuje po ułożeniu każdej strony; false
	// przerywa układ (rysowanie w trakcie układu, ADR 0027, zob. streamer.pageDone)
	pageDone func(index int, pages []*bo.PageBox) bool

	// gopdfgo: reszta wysokości bloku o podanej wysokości podzielonego razem z dziećmi; klucz to stos
	// wznowienia zwrócony przez układ fragmentu (resumeKey)
	fixedHeightRemaining map[uintptr]pr.Float
	// gopdfgo: dalsze ciągi treści wylewającej się z bloku, który sam skończył się na wcześniejszej
	// stronie (overflowContinuation w blockContainerLayout). Taki fragment ma wysokość zero i biegnie
	// równolegle z rodzeństwem, więc nie zajmuje miejsca na stronie (zob. hasPlacedChild).
	overflowContinuationBoxes map[*bo.BoxFields]bool
	// gopdfgo: reszta min-height kontenera kolumn podzielonego między fragmentatory; klucz jak wyżej
	minHeightRemaining map[uintptr]pr.Float
	// pendingMarkers (gopdfgo): znaczniki list (list-style-position: outside) czekające na pierwszy wiersz
	// elementu listy w dalszym fragmencie, według stylu elementu (zob. alignOutsideMarker)
	pendingMarkers map[pr.ElementStyle]Box
	// gopdfgo: reszta podanej wysokości komórki tabeli podzielonej między fragmentatory; klucz to stos
	// wznowienia komórki (resumeKey), zob. tables.go
	cellHeightRemaining map[uintptr]pr.Float

	// gopdfgo: odległość od góry następnej strony dla elementu wiersza flex przesuniętego w całości;
	// klucz to znacznik w stanie wznowienia linii (resumeKey)
	flexPushOffset map[uintptr]pr.Float
	// gopdfgo: stosy wznowienia, pod których adresami są wpisy w mapach powyżej (zob. keepResumeKey)
	keptResumeStacks []tree.ResumeStack
	// gopdfgo: pudełka, które dostają clearance, bo bez niego pociągnęłyby za sobą przylegające elementy
	// pływające (zob. inFlowLayout); ich obramowanie staje pod najniższym z oczyszczanych elementów
	forcedClearance map[*bo.BoxFields]bool
	// gopdfgo: pudełka omijające elementy pływające, których margines oddziela się od przylegających
	// elementów pływających (zob. inFlowLayout); górna krawędź obramowania staje bez ich marginesu
	separatedFromFloats map[*bo.BoxFields]bool
	// gopdfgo: w pustym wierszu umieszczono element pływający szerszy niż wiersz; wiersz kończy się
	// zaraz za nim (zob. inlineOutOfFlowLayout)
	floatOnlyLine bool
	// clipDepth > 0 oznacza układ wewnątrz pudełka przycinającego treść
	clipDepth int
	// gopdfgo: independentDepth > 0 w trakcie układu tabeli, kontenera flex albo siatki; elementy pływające
	// nie są tam przenoszone osobno na następną stronę (zob. canPushFloat)
	independentDepth int
	// gopdfgo: pushedFloatsPlaced: element pływający przeniesiony na następną stronę (zob. pushFloat) →
	// numer strony, na której stanął; przepływ, który na tej stronie znów do niego dojdzie, go pomija
	pushedFloatsPlaced map[Box]int
	// viewportClips: overflow inne niż visible przeniesione z elementu głównego na obszar strony
	viewportClips bool

	footnotes            []Box
	currentPageFootnotes []Box
	reportedFootnotes    []Box
	currentFootnoteArea  *bo.FootnoteAreaBox

	currentPage int
	pageBottom  pr.Float
	pageHeight  pr.Float // wysokość obszaru bieżącej strony

	marginClearance bool
	forcedBreak     bool
	inColumn        bool
	// gopdfgo: wysokość bieżącej kolumny (fragmentatora w kontenerze kolumn), zob. blockContainerLayout;
	// 0, gdy nie jest znana
	columnHeight pr.Float
	// gopdfgo: rodzaj podziału przed bieżącą kolumną (columnBreakNone: obowiązuje podział strony);
	// zob. breakAtFragmentTop
	columnBreak columnBreakKind
}

// columnBreakKind opisuje podział, po którym zaczyna się bieżąca kolumna.
type columnBreakKind uint8

const (
	columnBreakNone     columnBreakKind = iota // pierwsza kolumna: obowiązuje podział strony
	columnBreakForced                          // kolumna po wymuszonym podziale (break-before: column)
	columnBreakUnforced                        // kolumna po zwykłym podziale
)

// breakAtFragmentTop mówi, czy początek bieżącego fragmentu (strony albo kolumny) leży za podziałem
// i czy był to podział wymuszony. Marginesy przylegające do zwykłego podziału są obcinane do zera
// (CSS Fragmentation 3 § 5.2).
func (l *layoutContext) breakAtFragmentTop() (atBreak, forced bool) {
	switch l.columnBreak {
	case columnBreakForced:
		return true, true
	case columnBreakUnforced:
		return true, false
	}
	return l.currentPage > 1, l.forcedBreak
}

// presentationalHints=false,
func newLayoutContext(html *tree.HTML, stylesheets []tree.CSS,
	presentationalHints bool, fontConfig text.FontConfiguration, counterStyle counters.CounterStyle,
) *layoutContext {
	var (
		pageRules       []tree.PageRule
		userStylesheets = stylesheets
	)

	cache := images.NewCache()
	getImageFromUri := func(url, forcedMimeType string, orientation pr.SBoolFloat) images.Image {
		return images.GetImageFromUri(cache, html.UrlFetcher, false, url, forcedMimeType, orientation)
	}

	self := layoutContext{}
	self.resolver = bo.URLResolver{Fetch: html.UrlFetcher, FetchImage: getImageFromUri}
	self.fontConfig = fontConfig
	self.behavior = html.Behavior
	self.TargetCollector = tree.NewTargetCollector()
	self.counterStyle = counterStyle
	self.runningElements = make(map[string]map[int][]Box)
	self.brokenOutOfFlow = make(map[Box]brokenBox)
	self.pushedFloatsPlaced = make(map[Box]int)
	self.lateBoxes = make(map[int][]lateBox)
	self.fragmentStarts = make(map[Box]pr.Float)
	self.shiftedBlocks = make(map[Box]bool)

	// Cache
	self.stringSet = make(map[string]map[int][]string)
	self.dictionaries = make(map[text.HyphenDictKey]hyphen.Hyphener)
	self.strutLayouts = make(map[text.StrutLayoutKey][2]pr.Float)
	self.tables = map[*bo.TableBox]map[bool]tableContentWidths{}

	self.styleFor = tree.GetAllComputedStyles(html, userStylesheets, presentationalHints, fontConfig,
		counterStyle, &pageRules, &self.TargetCollector, false, &self)
	return &self
}

// PageArea (gopdfgo) podaje obszar zwykłej strony wynikający z arkuszy przeglądarki i podanych arkuszy
// (bez marginesów, obramowania i odstępu); zero, gdy nie da się go ustalić. Służy do wyznaczenia
// rozmiaru medium z arkusza użytkownika na pustym dokumencie (ADR 0019).
// gopdfgo: podaje też rozmiar samej strony (arkusza), do którego odnoszą się jednostki vw i vh w @page
// (tree.SetDefaultPageBox).
func PageArea(html *tree.HTML, stylesheets []tree.CSS, fontConfig text.FontConfiguration) (width, height, boxWidth, boxHeight pr.Float) {
	context := newLayoutContext(html, stylesheets, false, fontConfig, make(counters.CounterStyle))
	width, height = context.styleFor.PageArea()
	boxWidth, boxHeight = context.styleFor.PageBox()
	return width, height, boxWidth, boxHeight
}

func (l *layoutContext) CurrentPage() int { return l.currentPage }

func (l *layoutContext) Fonts() text.FontConfiguration { return l.fontConfig }

func (l *layoutContext) HyphenCache() map[text.HyphenDictKey]hyphen.Hyphener {
	return l.dictionaries
}

func (l *layoutContext) StrutLayoutsCache() map[text.StrutLayoutKey][2]pr.Float {
	return l.strutLayouts
}

func (l *layoutContext) overflowsPage(bottomSpace, positionY pr.Float) bool {
	return overflows(l.pageBottom-bottomSpace, positionY)
}

func overflows(bottomSpace, positionY pr.Float) bool {
	// Use a small fudge factor to avoid floating numbers errors.
	// The 1e-9 value comes from PEP 485.
	return positionY > bottomSpace*(1+1e-9)
}

func (l *layoutContext) createBlockFormattingContext() {
	l.excludedShapesLists = append(l.excludedShapesLists, shapeList{})
	l.excludedShapes = &l.excludedShapesLists[len(l.excludedShapesLists)-1]
}

func (l *layoutContext) finishBlockFormattingContext(rootBox_ Box) {
	// See https://www.w3.org/TR/CSS2/visudet.html#root-height
	rootBox := rootBox_.Box()
	if rootBox.Style.GetHeight().Tag == pr.Auto && len(l.excludedShapes.list) != 0 {
		boxBottom := rootBox.ContentBoxY() + rootBox.Height.V()
		maxShapeBottom := boxBottom
		for _, shape := range l.excludedShapes.list {
			v := shape.PositionY + shape.MarginHeight()
			if v > maxShapeBottom {
				maxShapeBottom = v
			}
		}
		rootBox.Height = rootBox.Height.V() + maxShapeBottom - boxBottom
	}
	l.excludedShapesLists = l.excludedShapesLists[:len(l.excludedShapesLists)-1]
	if L := len(l.excludedShapesLists); L != 0 {
		l.excludedShapes = &l.excludedShapesLists[L-1]
	} else {
		l.excludedShapes = nil
	}
}

// discardBlockFormattingContext (gopdfgo) zamyka kontekst bez dopasowania wysokości pudełka
// (układ przerwany, pudełka nie ma).
func (l *layoutContext) discardBlockFormattingContext() {
	l.excludedShapesLists = l.excludedShapesLists[:len(l.excludedShapesLists)-1]
	if L := len(l.excludedShapesLists); L != 0 {
		l.excludedShapes = &l.excludedShapesLists[L-1]
	} else {
		l.excludedShapes = nil
	}
}

func (l *layoutContext) createFlexFormattingContext() {
	l.excludedShapesLists = append(l.excludedShapesLists, shapeList{isFrozen: true})
	l.excludedShapes = &l.excludedShapesLists[len(l.excludedShapesLists)-1]
}

func (l *layoutContext) finishFlexFormattingContext(_ Box) {
	l.excludedShapesLists = l.excludedShapesLists[:len(l.excludedShapesLists)-1]
	if L := len(l.excludedShapesLists); L != 0 {
		l.excludedShapes = &l.excludedShapesLists[L-1]
	} else {
		l.excludedShapes = nil
	}
}

func resolveKeyword(keyword, name string, startChain []pr.StringSet) string {
	switch keyword {
	case "first":
		return "first"
	case "start":
		for _, stringSet := range startChain {
			if stringSet.String != "none" {
				for _, v := range stringSet.Contents {
					if v.String == name {
						return "first"
					}
				}
			}
		}
	case "last":
		return "last"
	case "first-except":
		return "return"
	}
	return ""
}

// startChain (gopdfgo) zbiera wartości string-set pudełek na ścieżce od strony przez kolejne pierwsze
// dzieci, czyli to, co czyta słowo kluczowe start. Źródło przechodziło tę ścieżkę w resolveKeyword, przy
// układaniu pól marginesowych; przy rysowaniu w trakcie układu (ADR 0027) drzewo strony jest już wtedy
// zwolnione, więc ścieżkę zapamiętujemy wcześniej (pageFinisher.rememberStart).
func startChain(page Box) []pr.StringSet {
	var chain []pr.StringSet
	for element := page; element != nil; {
		chain = append(chain, element.Box().Style.GetStringSet())
		if len(element.Box().Children) == 0 {
			break
		}
		element = element.Box().Children[0]
	}
	return chain
}

// pageStartChain zwraca ścieżkę start strony bieżącej: zapamiętaną, jeśli drzewo strony zwolniono,
// a w przeciwnym razie liczoną z drzewa.
func (lc *layoutContext) pageStartChain(page Box) []pr.StringSet {
	if chain, ok := lc.startChains[lc.currentPage]; ok {
		return chain
	}
	return startChain(page)
}

// Resolve value of string function (as set by string set).
// We'll have something like this that represents all assignments on a
// given page:
//
// {1: [u"First Header"], 3: [u"Second Header"],
//
//	4: [u"Third Header", u"3.5th Header"]}
//
// Value depends on current page.
// https://drafts.csswg.org/csswg/css-gcpm/#funcdef-string
//
// `keyword` indicates which value of the named string to use.
// Default is the first assignment on the current page
// else the most recent assignment (entry value)
// keyword="first"
func (lc *layoutContext) GetStringSetFor(page Box, name, keyword string) string {
	if currentS, in := lc.stringSet[name][lc.currentPage]; in {
		// A value was assigned on this page
		switch resolveKeyword(keyword, name, lc.pageStartChain(page)) {
		case "first":
			return currentS[0]
		case "last":
			return currentS[len(currentS)-1]
		case "return":
			return ""
		}
	}
	// Search backwards through previous pages
	for previousPage := lc.currentPage - 1; previousPage > 0; previousPage -= 1 {
		if currentS, in := lc.stringSet[name][previousPage]; in {
			return currentS[len(currentS)-1]
		}
	}
	return ""
}

func (lc *layoutContext) GetRunningElementFor(page Box, name, keyword string) Box {
	if currentS, in := lc.runningElements[name][lc.currentPage]; in {
		// A value was assigned on this page
		switch resolveKeyword(keyword, name, lc.pageStartChain(page)) {
		case "first":
			return currentS[0]
		case "last":
			return currentS[len(currentS)-1]
		case "return":
			return nil
		}
	}
	// Search backwards through previous pages
	for previousPage := lc.currentPage - 1; previousPage > 0; previousPage -= 1 {
		if currentS, in := lc.runningElements[name][previousPage]; in {
			return currentS[len(currentS)-1]
		}
	}
	return nil
}

// Add a footnote to the layout for this page.
func (l *layoutContext) layoutFootnote(footnote Box) bool {
	removeFromBoxes(&l.footnotes, footnote)
	l.currentPageFootnotes = append(l.currentPageFootnotes, footnote)
	return l.updateFootnoteArea()
}

// Remove a footnote from the layout and return it to the waitlist.
func (l *layoutContext) unlayoutFootnote(footnote Box) {
	if !isInBoxes(footnote, l.footnotes) {
		l.footnotes = append(l.footnotes, footnote)
		if isInBoxes(footnote, l.currentPageFootnotes) {
			removeFromBoxes(&l.currentPageFootnotes, footnote)
		} else if isInBoxes(footnote, l.reportedFootnotes) {
			removeFromBoxes(&l.reportedFootnotes, footnote)
		}
		l.updateFootnoteArea()
	}
}

// Mark a footnote as being moved to the next page.
func (l *layoutContext) reportFootnote(footnote Box) {
	removeFromBoxes(&l.currentPageFootnotes, footnote)
	l.reportedFootnotes = append(l.reportedFootnotes, footnote)
	l.updateFootnoteArea()
}

// Update the page bottom size and our footnote area height.
func (l *layoutContext) updateFootnoteArea() bool {
	if l.currentFootnoteArea.Height != pr.AutoF && !l.inColumn {
		l.pageBottom += l.currentFootnoteArea.MarginHeight()
	}
	l.currentFootnoteArea.Children = l.currentPageFootnotes
	if len(l.currentFootnoteArea.Children) != 0 {
		footnoteArea := bo.CreateAnonymousBox(bo.Deepcopy(l.currentFootnoteArea)).(bo.BlockLevelBoxITF)
		footnoteArea, _, _ = blockLevelLayout(
			l, footnoteArea, -pr.Inf, nil,
			&l.currentFootnoteArea.Page.BoxFields, true, nil, nil, nil, false, -1)
		l.currentFootnoteArea.Height = footnoteArea.Box().Height
		if !l.inColumn {
			l.pageBottom -= footnoteArea.Box().MarginHeight()
		}
		lastChild := footnoteArea.Box().Children[len(footnoteArea.Box().Children)-1]
		lastChildBottom := lastChild.Box().PositionY + lastChild.Box().MarginHeight() -
			lastChild.Box().MarginBottom.V()
		footnoteAreaBottom := footnoteArea.Box().PositionY + footnoteArea.Box().MarginHeight() -
			footnoteArea.Box().MarginBottom.V()
		overflow := lastChildBottom > footnoteAreaBottom
		return overflow
	} else {
		l.currentFootnoteArea.Height = pr.Float(0)
		return false
	}
}

// warnContentBelowPage zgłasza ostrzeżenie, gdy tekst albo obraz leży w całości poniżej dolnej krawędzi
// strony, czyli nie będzie widoczny w wyniku. Treść przycięta przez przodka (overflow inne niż visible przy podanej
// wysokości) nie jest zgłaszana: autor sam ją ukrył.
//
// gopdfgo: dodane. Silnik nie umie podzielić każdego pudełka między strony (np. inline-block wyższego niż
// strona); bez ostrzeżenia taka strata treści była cicha.
func warnContentBelowPage(page *bo.PageBox, root Box, pageNumber int) {
	pageBottom := page.MarginHeight() + 1
	var find func(box Box) Box
	find = func(box_ Box) Box {
		box := box_.Box()
		if box.Style.GetVisibility() != "visible" && len(box.Children) == 0 {
			return nil
		}
		_, isText := box_.(*bo.TextBox)
		_, isReplaced := box_.(bo.ReplacedBoxITF)
		if isText || isReplaced {
			// tylko treść w całości poniżej strony: wiersz albo obraz, który wystaje częściowo, jest widoczny
			if box.Height != pr.AutoF && box.Height != nil && box.PositionY > pageBottom {
				return box_
			}
			return nil
		}
		if box.Style.GetOverflow() != "visible" && box.Style.GetHeight().Tag != pr.Auto {
			return nil
		}
		if box.SliceFullHeight > 0 {
			return nil // wycinek wiersza krojonego między strony: reszta jest na następnych stronach
		}
		if box.DeferredToLaterPage {
			return nil // pudełko pozycjonowane poniżej strony: zostanie narysowane na późniejszej
		}
		for _, child := range box.Children {
			if found := find(child); found != nil {
				return found
			}
		}
		return nil
	}
	if found := find(root); found != nil {
		logger.WarningLogger.Printf("strona %d: treść elementu <%s> leży poniżej dolnej krawędzi strony i nie jest widoczna",
			pageNumber, found.Box().ElementTag())
	}
}

// cellPositioned (gopdfgo) łączy zastępnik pudełka pozycjonowanego z jego pudełkiem sprzed układu.
type cellPositioned struct {
	placeholder *AbsolutePlaceholder
	original    Box
	height      pr.Float // wysokość komórki (z odstępem wewnętrznym) w chwili pierwszego układu
}

// contentOverflowScale (gopdfgo) podaje, ile razy treść jest szersza niż obszar strony: największy iloraz
// po stronach; 1, gdy nic nie wystaje. Liczą się pudełka treści (bez pól marginesów), także pozycjonowane;
// zawartość pudełek przycinających (overflow inny niż visible) jest pomijana.
func contentOverflowScale(pages []*bo.PageBox) pr.Float {
	scale := pr.Float(1)
	for _, page := range pages {
		left, width := page.ContentBoxX(), page.Width.V()
		if width <= 0 {
			continue
		}
		right := left + width
		for _, child := range page.Children {
			if _, isMargin := child.(*bo.MarginBox); isMargin {
				continue
			}
			right = max(right, contentRightEdge(child))
		}
		scale = max(scale, (right-left)/width)
	}
	return scale
}

func contentRightEdge(box_ Box) pr.Float {
	if placeholder, ok := box_.(*AbsolutePlaceholder); ok {
		box_ = placeholder.AliasBox
	}
	box := box_.Box()
	edge := -pr.Inf
	// gopdfgo: pudełko fixed, którego blokiem zawierającym jest strona, nie należy do przewijalnej treści
	// dokumentu (CSSOM View 1 § 3.3, tak jak w Chromium), więc nie pomniejsza treści, nawet gdy wystaje
	// poza stronę (test WPT css-page/fixedpos-010).
	if box.Style.GetPosition().String == "fixed" && box.IsAbsolutelyPositioned() {
		return edge
	}
	switch box_.(type) {
	case *bo.LineBox:
		// szerokość wiersza nie jest szerokością treści; liczą się jego dzieci
	case *bo.TextBox:
		if width, ok := box.Width.(pr.Float); ok {
			edge = box.PositionX + width
		}
	default:
		if _, ok := box.Width.(pr.Float); ok && box.Style.GetVisibility() == "visible" {
			edge = box.BorderBoxX() + box.BorderWidth()
		}
	}
	if !bo.LineT.IsInstance(box_) && box.Style.GetOverflow() != "visible" {
		return edge
	}
	for _, child := range box.Children {
		edge = max(edge, contentRightEdge(child))
	}
	return edge
}

// continuousPageHeight to umowna wysokość strony w trybie ciągłym: na tyle duża, że żadna treść nie
// sięga jej dołu, i na tyle mała, że położenia zachowują dokładność.
const continuousPageHeight pr.Float = 1 << 22

// contentBottomEdge podaje dolną krawędź treści pudełka i jego potomków (krawędzie obramowań, tekst,
// elementy pozycjonowane); treść przyciętą przez overflow pomija.
func contentBottomEdge(box_ Box) pr.Float {
	if placeholder, ok := box_.(*AbsolutePlaceholder); ok {
		box_ = placeholder.AliasBox
	}
	box := box_.Box()
	edge := -pr.Inf
	if height, ok := box.Height.(pr.Float); ok && box.Style.GetVisibility() == "visible" {
		switch box_.(type) {
		case *bo.TextBox, *bo.LineBox:
			edge = box.PositionY + height
		default:
			edge = box.BorderBoxY() + box.BorderHeight()
		}
	}
	if !bo.LineT.IsInstance(box_) && box.Style.GetOverflow() != "visible" && !box.IsForRootElement {
		return edge
	}
	for _, child := range box.Children {
		edge = max(edge, contentBottomEdge(child))
	}
	return edge
}

// fixedKey (gopdfgo) wskazuje element źródłowy pudełka fixed (zob. layoutFixedBoxes); pudełko anonimowe
// rozpoznajemy po samym pudełku.
type fixedKey struct {
	element *html.Node
	pseudo  string
	box     Box
}

func fixedKeyOf(box Box) fixedKey {
	if fields := box.Box(); fields.Element != nil {
		return fixedKey{element: fields.Element, pseudo: fields.PseudoType}
	}
	return fixedKey{box: box}
}

// collectFixedKeys (gopdfgo) zapisuje w seen klucze pudełek fixed z drzewa box, bez schodzenia w ich wnętrze.
func collectFixedKeys(box Box, seen map[fixedKey]bool) {
	for _, child := range box.Box().Children {
		if f := child.Box(); f.Style != nil && f.Style.GetPosition().String == "fixed" && f.IsAbsolutelyPositioned() {
			seen[fixedKeyOf(child)] = true
			continue
		}
		collectFixedKeys(child, seen)
	}
}
