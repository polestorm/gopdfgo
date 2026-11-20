package layout

import (
	"fmt"

	"github.com/polestorm/gopdfgo/internal/webrender/css/counters"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"golang.org/x/net/html"
)

// Rysowanie w trakcie układu (gopdfgo, ADR 0027).
//
// Layout układa wszystkie strony, a dopiero potem dokument je rysuje; przy dużym dokumencie w pamięci są
// naraz drzewa pudełek wszystkich stron. LayoutStreaming oddaje każdą stronę do narysowania zaraz po tym,
// jak jej obszar jest ostateczny, i zaraz potem zwalnia jej drzewo. Wynik ma być bajt w bajt taki sam jak
// z Layout, więc strumień obsługuje tylko dokumenty, w których strona po ułożeniu już się nie zmienia;
// w pozostałych przerywa i każe wywołującemu ułożyć dokument zwykłą drogą.

// pageFinisher (gopdfgo) wykonuje obróbkę stron po zakończeniu paginacji, stronę po stronie. Źródło
// robiło to w jednej pętli w layoutDocument; rozdzielenie na kroki pozwala wykonać je także w trakcie
// układu, zaraz po powstaniu strony.
type pageFinisher struct {
	context *layoutContext
	// Prevent repetition of bookmarks (see #1145).
	watchElements, watchElementsBefore, watchElementsAfter map[*html.Node]bool

	firstRoot    *bo.BoxFields // element główny z pierwszej strony: początek tła płótna
	canvasOffset pr.Float      // łączna wysokość obszarów poprzednich stron
}

func newPageFinisher(context *layoutContext) *pageFinisher {
	return &pageFinisher{
		context:       context,
		watchElements: map[*html.Node]bool{}, watchElementsBefore: map[*html.Node]bool{},
		watchElementsAfter: map[*html.Node]bool{},
	}
}

// collect: zakładki bez powtórzeń, liczniki stron w string-set i bookmark-label, wartości string-set
// strony i. Zwraca true, gdy któraś z przeliczonych wartości zależy od liczby stron albo od liczników
// celu (target-counter), czyli od stron, których w trakcie układu może jeszcze nie być.
//
// Calculate string-sets and bookmark-label containing page based counters
// when pagination is finished. No need to do that (maybe multiple times) in
// makePage because they dont create boxes, only appear in MarginBoxes and
// in the final PDF.
func (f *pageFinisher) collect(i int, page *bo.PageBox) (needsLaterPages bool) {
	context := f.context
	// We need the updated pageCounterValues
	pageCounterValues := context.pageMaker[i+1].PageState.CounterValues

	for _, child := range bo.Descendants(page) {
		childBox := child.Box()
		// Only one bookmark per original box
		if childBox.BookmarkLabel != "" {
			var checklist map[*html.Node]bool
			if childBox.PseudoType == "before" {
				checklist = f.watchElementsBefore
			} else if childBox.PseudoType == "after" {
				checklist = f.watchElementsAfter
			} else {
				checklist = f.watchElements
			}

			if checklist[childBox.Element] {
				childBox.BookmarkLabel = ""
			} else {
				checklist[childBox.Element] = true
			}
		}

		if mLink := child.MissingLink(); mLink != nil {
			for key, item := range context.TargetCollector.CounterLookupItems {
				box, cssToken := key.SourceBox, key.CssToken
				if mLink == box && cssToken != "content" {
					if cssToken == "bookmark-label" && childBox.BookmarkLabel == "" {
						// don't refill it!
						continue
					}

					if item.MissingCounters.Has("pages") || len(item.MissingTargetCounters) != 0 {
						needsLaterPages = true
					}
					item.ParseAgain(pageCounterValues)

					if cssToken == "bookmark-label" {
						childBox.BookmarkLabel = box.GetBookmarkLabel()
					}
				}
			}
		}
		// Collect the stringSets in the LayoutContext
		stringSets := childBox.StringSet
		for _, stringSet := range stringSets {
			stringName, text := stringSet.Type, string(stringSet.Content.(pr.String))
			dict := context.stringSet[stringName]
			if dict == nil {
				dict = make(map[int][]string)
			}
			dict[i+1] = append(dict[i+1], text)
			context.stringSet[stringName] = dict
		}
	}
	return needsLaterPages
}

// placeFixedBoxes powtarza na stronie pudełka fixed zarejestrowane na stronach before i after (zob.
// layoutFixedBoxes) i zwraca klucze pudełek fixed, które strona ma po tym kroku.
func (f *pageFinisher) placeFixedBoxes(before, after []*bo.PageBox, page *bo.PageBox) map[fixedKey]bool {
	var rootChildren []Box
	root := page.Box().Children[0]
	// gopdfgo: za widziane uznajemy pudełka fixed obecne w drzewie tej strony; page.FixedBoxes zawiera też
	// rejestracje z odrzuconych układów próbnych (np. wiersze przeniesione przez orphans), których na stronie
	// nie ma (przejęty TestFixedInInline)
	seen := map[fixedKey]bool{}
	collectFixedKeys(root, seen)
	rootChildren = append(rootChildren, layoutFixedBoxes(f.context, before, page, seen)...)
	rootChildren = append(rootChildren, root.Box().Children...)
	rootChildren = append(rootChildren, layoutFixedBoxes(f.context, after, page, seen)...)
	root.Box().Children = rootChildren
	return seen
}

// prepare ustawia bieżącą stronę i skalę treści (ADR 0014) i zostawia w dzieciach strony element główny
// oraz obszar przypisów, jeśli nie jest pusty. Zwraca wysokość strony sprzed przeskalowania.
func (f *pageFinisher) prepare(i int, page *bo.PageBox) (scaledHeight pr.Float) {
	context := f.context
	root, footnoteArea := page.Box().Children[0], page.Box().Children[1]
	context.currentPage = i + 1 // pageNumber starts at 1

	scaledHeight = page.Height.V()
	if scale := context.contentScale; scale > 1 {
		// treść strony jest ułożona na powiększonym obszarze; sama strona, jej marginesy i pola
		// marginesów wracają do rozmiaru z arkusza, a treść będzie rysowana w skali 1/scale
		page.Width, page.Height = page.Width.V()/scale, page.Height.V()/scale
		for _, content := range []Box{root, footnoteArea} {
			content.Box().PageScale = scale
			content.Box().PageScaleOriginX, content.Box().PageScaleOriginY = page.ContentBoxX(), page.ContentBoxY()
		}
	}
	page.Children = []Box{root}
	if len(footnoteArea.Box().Children) != 0 {
		page.Children = append(page.Children, footnoteArea)
	}
	return scaledHeight
}

// marginBoxes układa pola marginesowe strony i (context.currentPage musi wskazywać tę stronę).
func (f *pageFinisher) marginBoxes(i int, page *bo.PageBox) []Box {
	context := f.context
	// pageMaker's pageState is ready for the MarginBoxes
	state := context.pageMaker[i+1].PageState
	if g, ok := context.exactPageGeometry[i+1]; ok {
		// gopdfgo: zaokrąglenie do pikseli (ADR 0021) dotyczy obszaru treści; pola marginesów układamy
		// według marginesów dokładnie takich, jak w arkuszu, a potem strona wraca do zaokrąglonych (dalsze
		// kroki, m.in. rysowanie i powtórny układ, czytają geometrię, na której ułożono treść). W trybie
		// ciągłym (ADR 0017) wysokość strony urosła już do wysokości treści i zostaje.
		rounded := [5]pr.Float{page.Width.V(), page.Height.V(), page.MarginLeft.V(), page.MarginRight.V(), page.MarginBottom.V()}
		page.Width, page.MarginLeft, page.MarginRight = g[0], g[2], g[3]
		if !context.behavior.Continuous {
			page.Height, page.MarginBottom = g[1], g[4]
		}
		boxes := makeMarginBoxes(context, page, state)
		page.Width, page.Height, page.MarginLeft, page.MarginRight, page.MarginBottom = rounded[0], rounded[1], rounded[2], rounded[3], rounded[4]
		return boxes
	}
	return makeMarginBoxes(context, page, state)
}

// backgrounds układa tła strony, płótna i pudełek strony (page.Children[0] to element główny).
func (f *pageFinisher) backgrounds(i int, page *bo.PageBox, scaledHeight pr.Float) {
	root := page.Children[0]
	if f.firstRoot == nil {
		copied := *root.Box()
		f.firstRoot = &copied
	}
	canvasOrigin := *f.firstRoot
	canvasOrigin.PositionY -= f.canvasOffset
	layoutBackgrounds(page, f.context.resolver.FetchImage, &canvasOrigin)
	f.canvasOffset += scaledHeight
	warnContentBelowPage(page, root, i+1)
}

// PageSink (gopdfgo) odbiera strony układane przez LayoutStreaming.
type PageSink interface {
	// Area dostaje stronę index bez pól marginesowych, gdy jej obszar jest ostateczny: dzieci strony to
	// element główny i, jeśli nie jest pusty, obszar przypisów. Po powrocie układ zwalnia dzieci strony.
	Area(index int, page *bo.PageBox)
	// Margins dostaje pola marginesowe strony index, gdy wszystkie strony są już ułożone, w kolejności
	// stron. Zwraca opis przeszkody, gdy pól nie da się dorysować bez zmiany wyniku, albo "".
	Margins(index int, page *bo.PageBox, boxes []Box) (fallback string)
}

// LayoutStreaming (gopdfgo) układa dokument jak Layout, ale każdą stronę oddaje do sink zaraz po
// ułożeniu (ADR 0027). newSink jest wywoływane przed każdym przebiegiem: przy pomniejszaniu treści
// szerszej niż strona (ADR 0014) pierwszy przebieg tylko mierzy, a strony rysuje drugi, w skali.
// Zwraca liczbę stron albo, gdy wynik różniłby się od Layout, opis przeszkody; wywołujący odrzuca wtedy
// wszystko, co dostał sink, i układa dokument przez Layout.
func LayoutStreaming(html *tree.HTML, stylesheets []tree.CSS, presentationalHints bool, fontConfig text.FontConfiguration,
	newSink func() PageSink,
) (pages int, fallback string) {
	limit := pr.Float(html.Behavior.MaxShrink)
	pages, scale, fallback := streamScaled(html, stylesheets, presentationalHints, fontConfig, 1, newSink(), limit > 1)
	if fallback != "" {
		return 0, fallback
	}
	if scale = min(scale, limit); limit > 1 && scale > 1.001 {
		pages, _, fallback = streamScaled(html, stylesheets, presentationalHints, fontConfig, scale, newSink(), false)
	}
	return pages, fallback
}

func streamScaled(html *tree.HTML, stylesheets []tree.CSS, presentationalHints bool, fontConfig text.FontConfiguration,
	contentScale pr.Float, sink PageSink, measureOverflow bool,
) (pages int, overflow pr.Float, fallback string) {
	context := newLayoutContext(html, stylesheets, presentationalHints, fontConfig, make(counters.CounterStyle))
	counterStyle := context.counterStyle
	context.contentScale = contentScale

	logger.ProgressLogger.Println("Step 4 - Creating formatting structure")

	rootBox := bo.BuildFormattingStructure(html.Root, context.styleFor, context.resolver,
		html.BaseUrl, &context.TargetCollector, counterStyle, &context.footnotes)

	s := &streamer{context: context, finisher: newPageFinisher(context), sink: sink,
		measureOverflow: measureOverflow, overflow: 1}
	return s.run(html, rootBox)
}

// streamer prowadzi jeden przebieg LayoutStreaming.
type streamer struct {
	context  *layoutContext
	finisher *pageFinisher
	sink     PageSink

	// measureOverflow: liczyć, ile razy treść jest szersza niż strona (contentOverflowScale); measuring:
	// treść wystaje, więc ten przebieg już tylko mierzy, a strony narysuje przebieg w skali
	measureOverflow, measuring bool
	overflow                   pr.Float

	// fixedSeen[i]: klucze pudełek fixed, które ma strona i (zob. placeFixedBoxes)
	fixedSeen []map[fixedKey]bool

	fallback string
}

func (s *streamer) fail(reason string) bool {
	s.fallback = reason
	return false
}

// run odpowiada pierwszemu obiegowi pętli w layoutDocument; obiegu drugiego nie ma: gdy byłby potrzebny,
// przebieg kończy się przeszkodą.
func (s *streamer) run(doc *tree.HTML, rootBox bo.BlockLevelBoxITF) (int, pr.Float, string) {
	context := s.context
	initializePageMaker(context, *rootBox.Box())
	markTablesWithoutColumnPercentages(rootBox, false)

	context.pageDone = s.pageDone
	pages := context.makeAllPages(rootBox, doc, nil)
	context.pageDone = nil
	if s.fallback != "" {
		return 0, 0, s.fallback
	}

	actualTotalPages := len(pages)
	for _, pageData := range context.pageMaker {
		pageData.PageState.CounterValues["pages"] = []int{actualTotalPages}
		if pageData.RemakeState.ContentChanged {
			return 0, 0, "content changed after its page was laid out (repagination)"
		}
		if pageData.RemakeState.PagesWanted {
			return 0, 0, "counter(pages) in page content (repagination)"
		}
	}
	if s.measuring {
		return actualTotalPages, s.overflow, ""
	}

	// pola marginesowe: liczba stron jest już znana (counter(pages)), podobnie wartości string-set
	// i elementy biegnące ze wszystkich stron
	for i, page := range pages {
		context.currentPage = i + 1
		boxes := s.finisher.marginBoxes(i, page)
		inTransform := pr.HasTransform(page.Style)
		for _, box := range boxes {
			layoutBoxBackgroundsIn(page, box, context.resolver.FetchImage, true, nil, nil, inTransform)
		}
		if reason := s.sink.Margins(i, page, boxes); reason != "" {
			return 0, 0, reason
		}
	}
	return actualTotalPages, s.overflow, ""
}

// pageDone jest wywoływane przez makeAllPages po ułożeniu strony i (pages[i]). Wykonuje dla niej obróbkę
// z layoutDocument (oprócz pól marginesowych), oddaje ją do sink i zwalnia jej drzewo. Zwraca false, gdy
// strona albo strony wcześniejsze mogą się jeszcze zmienić.
func (s *streamer) pageDone(i int, pages []*bo.PageBox) bool {
	context := s.context
	page := pages[i]

	// Drugi obieg pętli w layoutDocument jest już przesądzony: znacznik na stronie ułożonej nie jest
	// zerowany do końca obiegu.
	for k := 0; k <= i && k < len(context.pageMaker); k++ {
		if remake := context.pageMaker[k].RemakeState; remake.ContentChanged {
			return s.fail(fmt.Sprintf("content of page %d changed after layout (target-counter, target-text or counter(pages))", k+1))
		} else if remake.PagesWanted {
			return s.fail("counter(pages) in page content (repagination)")
		}
	}
	// fragmenty pudełek pozycjonowanych dokładane do stron po paginacji (attachLateBoxes)
	for _, late := range context.lateBoxes {
		if len(late) != 0 {
			return s.fail("positioned box continued on an earlier page")
		}
	}

	if s.finisher.collect(i, page) {
		return s.fail("string-set or bookmark-label with counter(pages) or target-counter")
	}

	if i > 0 {
		context.currentPage = i // jak w layoutDocument: wartość z poprzedniej strony
	}
	seen := s.finisher.placeFixedBoxes(pages[:i], nil, page)
	// Pudełko fixed jest powtarzane także na stronach wcześniejszych niż ta, na której się pojawiło
	// (layoutFixedBoxes z pages[i+1:]); strony już narysowane by go nie miały.
	for _, box := range page.FixedBoxes {
		key := fixedKeyOf(box)
		for k := range i {
			if !s.fixedSeen[k][key] {
				return s.fail("fixed box repeated on earlier pages")
			}
		}
	}
	s.fixedSeen = append(s.fixedSeen, seen)

	scaledHeight := s.finisher.prepare(i, page)
	s.finisher.backgrounds(i, page, scaledHeight)

	if s.measureOverflow {
		s.overflow = max(s.overflow, contentOverflowScale([]*bo.PageBox{page}))
		if limit := pr.Float(context.behavior.MaxShrink); min(s.overflow, limit) > 1.001 {
			s.measuring = true
		}
	}
	if !s.measuring {
		s.sink.Area(i, page)
	}
	page.Children = nil
	return true
}
