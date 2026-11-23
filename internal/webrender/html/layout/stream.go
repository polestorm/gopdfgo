package layout

import (
	"fmt"
	"io"
	"slices"

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
	// gopdfgo: wartość to numer strony, która ma zakładkę elementu (zob. outOfOrder)
	watchElements, watchElementsBefore, watchElementsAfter map[*html.Node]int
	// outOfOrder (gopdfgo): strona obrabiana po stronie dalszej (strona odłożona, ADR 0036) ma zakładkę
	// elementu, którą wzięła już strona dalsza; po kolei wzięłaby ją ta strona
	outOfOrder bool

	firstRoot    *bo.BoxFields // element główny z pierwszej strony: początek tła płótna
	canvasOffset pr.Float      // łączna wysokość obszarów poprzednich stron
}

func newPageFinisher(context *layoutContext) *pageFinisher {
	return &pageFinisher{
		context:       context,
		watchElements: map[*html.Node]int{}, watchElementsBefore: map[*html.Node]int{},
		watchElementsAfter: map[*html.Node]int{},
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
			var checklist map[*html.Node]int
			if childBox.PseudoType == "before" {
				checklist = f.watchElementsBefore
			} else if childBox.PseudoType == "after" {
				checklist = f.watchElementsAfter
			} else {
				checklist = f.watchElements
			}

			if owner := checklist[childBox.Element]; owner != 0 {
				childBox.BookmarkLabel = ""
				if owner > i+1 {
					f.outOfOrder = true
				}
			} else {
				checklist[childBox.Element] = i + 1
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
		rounded := [6]pr.Float{page.Width.V(), page.Height.V(), page.MarginLeft.V(), page.MarginRight.V(), page.MarginBottom.V(), page.MarginTop.V()}
		page.Width, page.MarginLeft, page.MarginRight, page.MarginTop = g[0], g[2], g[3], g[5]
		if !context.behavior.Continuous {
			page.Height, page.MarginBottom = g[1], g[4]
		}
		boxes := makeMarginBoxes(context, page, state)
		page.Width, page.Height, page.MarginLeft, page.MarginRight, page.MarginBottom, page.MarginTop = rounded[0], rounded[1], rounded[2], rounded[3], rounded[4], rounded[5]
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

// DeferringSink (gopdfgo) to PageSink, który pozwala odłożyć rysowanie strony na koniec paginacji
// (ADR 0036, dopisek trzeci): strony dalsze są rysowane wcześniej, a w celu stoją w kolejności stron.
type DeferringSink interface {
	PageSink
	// Reserve zajmuje w celu miejsce strony index; jej obszar przyjdzie później przez Area. page to strona
	// z pierwszego obiegu paginacji: Area dostanie stronę o tych samych wymiarach albo zgłosi przeszkodę.
	// false: cel nie pozwala rysować stron poza kolejnością (np. PDF z tagami).
	Reserve(index int, page *bo.PageBox) bool
	// GlyphConflicts zwraca, ile razy rysowany glif miał już w foncie celu przypisane inne znaki (zob.
	// backend.FontChars.Conflicts). Pierwsze przypisanie wygrywa, więc wzrost licznika przy rysowaniu stron
	// odłożonych znaczy, że po kolei wynik mógłby być inny.
	GlyphConflicts() int
}

// LayoutStreaming (gopdfgo) układa dokument jak Layout, ale każdą stronę oddaje do sink zaraz po
// ułożeniu (ADR 0027). newSink jest wywoływane przed każdym przebiegiem: przy pomniejszaniu treści
// szerszej niż strona (ADR 0014) pierwszy przebieg tylko mierzy, a strony rysuje drugi, w skali.
//
// Dokument, który wymaga ponownej paginacji (spis treści z target-counter, counter(pages) w treści),
// jest układany w dwóch przebiegach (ADR 0036): pierwszy ustala stan paginacji bez zachowywania stron,
// drugi układa i rysuje strony od nowa. restart, gdy nie jest nil, jest wywoływane przed pierwszym
// z nich, bo wszystko, co sink dostał wcześniej, idzie do kosza. Dokument, który ma najwyżej
// TwoPassMinPages stron, pierwszy przebieg układa w całości, tak jak Layout, i zwraca strony w laidOut;
// wywołujący rysuje je wtedy po ułożeniu, jak wynik Layout.
//
// Zanim dojdzie do dwóch przebiegów, pierwszy przebieg próbuje odłożyć strony, które mogą się jeszcze
// zmienić (ADR 0036, dopisek trzeci): rysuje pozostałe, kończy obiegi paginacji i na końcu rysuje strony
// odłożone; deferred to ich liczba. Dwa przebiegi zostają dla dokumentów, w których to się nie udaje.
//
// Zwraca liczbę stron i twoPass, gdy dokument był układany w dwóch przebiegach, albo, gdy wynik różniłby
// się od Layout, opis przeszkody; wywołujący odrzuca wtedy wszystko, co dostał sink, i układa dokument
// przez Layout.
func LayoutStreaming(html *tree.HTML, stylesheets []tree.CSS, presentationalHints bool, fontConfig text.FontConfiguration,
	newSink func() PageSink, restart func(),
) (pages int, laidOut []*bo.PageBox, twoPass bool, deferred int, fallback string) {
	limit := pr.Float(html.Behavior.MaxShrink)
	r := streamScaled(html, stylesheets, presentationalHints, fontConfig, 1, newSink(), limit > 1, false)
	if r.fallback == "" {
		deferred = r.deferred
	}
	if r.repaginate && OnRepaginate != nil {
		OnRepaginate(r.fallback)
	}
	if r.repaginate && !TwoPassDisabled {
		if restart != nil {
			restart()
		}
		twoPass = true
		r = streamScaled(html, stylesheets, presentationalHints, fontConfig, 1, newSink(), limit > 1, true)
		if r.laidOut != nil {
			// jak w Layout: pomniejszanie po ułożeniu całego dokumentu w skali 1
			if limit > 1 {
				if scale := min(contentOverflowScale(r.laidOut), limit); scale > 1.001 {
					r.laidOut = layoutScaled(html, stylesheets, presentationalHints, fontConfig, scale)
				}
			}
			return len(r.laidOut), r.laidOut, true, 0, ""
		}
	}
	if r.fallback != "" {
		return 0, nil, twoPass, 0, r.fallback
	}
	if scale := min(r.overflow, limit); limit > 1 && scale > 1.001 {
		r = streamScaled(html, stylesheets, presentationalHints, fontConfig, scale, newSink(), false, twoPass)
		if r.laidOut != nil {
			return len(r.laidOut), r.laidOut, twoPass, 0, ""
		}
		deferred = 0
	}
	return r.pages, nil, twoPass, deferred, r.fallback
}

// TwoPassDisabled (gopdfgo) wyłącza układ w dwóch przebiegach (ADR 0036): dokument wymagający ponownej
// paginacji wraca wtedy do układu całego dokumentu przed rysowaniem, jak przed tą decyzją. Służy testom
// i pomiarom.
var TwoPassDisabled bool

// TwoPassMinPages (gopdfgo): dokument wymagający ponownej paginacji, który ma najwyżej tyle stron, jest
// układany w całości przed rysowaniem, bo drugi przebieg układu kosztuje czas, a przy kilkudziesięciu
// stronach oszczędza mało pamięci (ADR 0036). Zmienna dla testów i pomiarów.
var TwoPassMinPages = 32

// DeferDisabled (gopdfgo) wyłącza odkładanie stron (ADR 0036, dopisek trzeci): dokument wymagający
// ponownej paginacji idzie wtedy od razu drogą dwuprzebiegową. Służy testom i pomiarom.
var DeferDisabled bool

// OnRepaginate (gopdfgo), gdy nie jest nil, dostaje opis przeszkody, przez którą pierwszy przebieg
// LayoutStreaming skończył się potrzebą ponownej paginacji (dwa przebiegi albo, gdy są wyłączone, Layout).
// Służy testom i pomiarom.
var OnRepaginate func(reason string)

// streamResult: wynik jednego przebiegu LayoutStreaming.
type streamResult struct {
	pages    int
	overflow pr.Float // ile razy treść jest szersza niż strona (gdy przebieg to mierzył)
	fallback string
	// repaginate: przebieg przerwała potrzeba ponownej paginacji, którą rozwiązuje układ w dwóch przebiegach
	repaginate bool
	// laidOut: krótki dokument ułożony w całości przez pierwszy z dwóch przebiegów (zob. LayoutStreaming)
	laidOut []*bo.PageBox
	// deferred: liczba stron odłożonych i narysowanych po zakończeniu paginacji
	deferred int
}

// streamScaled prowadzi jeden przebieg LayoutStreaming w skali contentScale. twoPass: najpierw ustalić
// stan paginacji (streamer.paginate), potem układać i rysować strony z tym stanem.
func streamScaled(html *tree.HTML, stylesheets []tree.CSS, presentationalHints bool, fontConfig text.FontConfiguration,
	contentScale pr.Float, sink PageSink, measureOverflow, twoPass bool,
) streamResult {
	context := newLayoutContext(html, stylesheets, presentationalHints, fontConfig, make(counters.CounterStyle))
	counterStyle := context.counterStyle
	context.contentScale = contentScale

	logger.ProgressLogger.Println("Step 4 - Creating formatting structure")

	rootBox := bo.BuildFormattingStructure(html.Root, context.styleFor, context.resolver,
		html.BaseUrl, &context.TargetCollector, counterStyle, &context.footnotes)

	s := &streamer{context: context, finisher: newPageFinisher(context), sink: sink,
		measureOverflow: measureOverflow, overflow: 1}
	// odkładanie stron tylko w przebiegu w skali 1, bez dwóch przebiegów (zob. deferPage)
	if deferSink, ok := sink.(DeferringSink); ok && !twoPass && contentScale == 1 && !DeferDisabled {
		s.deferring, s.deferSink, s.deferred = true, deferSink, map[int]*deferredPage{}
	}
	// jak w layoutDocument
	initializePageMaker(context, *rootBox.Box())
	markTablesWithoutColumnPercentages(rootBox, false)
	if twoPass {
		laidOut, reason := s.paginate(html, rootBox)
		if reason != "" || laidOut != nil {
			return streamResult{fallback: reason, laidOut: laidOut}
		}
	}
	pages, overflow, fallback := s.run(html, rootBox)
	return streamResult{pages: pages, overflow: overflow, fallback: fallback, repaginate: s.repaginate, deferred: len(s.deferred)}
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
	// repaginate: przeszkodą jest ponowna paginacja (zob. LayoutStreaming)
	repaginate bool
	// verifying: drugi przebieg układu w dwóch przebiegach (ADR 0036); totalPages: liczba stron
	// z pierwszego, originalFootnotes: przypisy sprzed paginacji
	verifying         bool
	totalPages        int
	originalFootnotes []Box
	// warnings: miejsce docelowe ostrzeżeń, wyciszonych na czas układu stron w przebiegu drugim
	warnings io.Writer

	// deferring: strony, które mogą się jeszcze zmienić, są odkładane, a dokument kończy obiegi paginacji
	// w tym samym przebiegu (ADR 0036, dopisek trzeci); deferSink: sink jako DeferringSink
	deferring bool
	deferSink DeferringSink
	// deferred: strony odłożone według indeksu; loop: numer obiegu paginacji
	deferred map[int]*deferredPage
	loop     int
	held     int // liczba zachowanych drzew stron odłożonych (zob. maxHeldPages)
}

// deferredPage: strona odłożona do końca paginacji.
type deferredPage struct {
	// page: ostatnio ułożona wersja strony (nil, gdy wiadomo, że będzie ułożona od nowa)
	page *bo.PageBox
	// height i offset: wysokość strony z pierwszego obiegu i łączna wysokość stron poprzednich (tło płótna,
	// zob. pageFinisher.backgrounds)
	height, offset pr.Float
}

func (s *streamer) fail(reason string) bool {
	s.fallback = reason
	return false
}

func (s *streamer) failRepaginate(reason string) bool {
	s.repaginate = true
	return s.fail(reason)
}

// paginate: pierwszy przebieg układu w dwóch przebiegach (ADR 0036). Wykonuje obiegi paginacji
// z layoutDocument (layoutContext.paginate), ale drzewa stron zwalnia zaraz po ułożeniu: zostają stan
// paginacji (pageMaker), numery stron i liczniki kotwic (TargetCollector), liczba stron i treść pudełek
// przeliczona przez ParseAgain. Strona, której layoutDocument nie układa od nowa w ostatnim obiegu, jest
// tam brana z obiegu wcześniejszego; drugi przebieg układa ją jeszcze raz z tym samym stanem, a każdą
// różnicę stanu zgłasza jako przeszkodę (layoutContext.drift).
//
// Dopóki dokument ma najwyżej TwoPassMinPages stron, strony zostają; gdy tak jest do końca paginacji,
// paginate kończy układ jak layoutDocument i zwraca strony w laidOut.
func (s *streamer) paginate(doc *tree.HTML, rootBox bo.BlockLevelBoxITF) (laidOut []*bo.PageBox, fallback string) {
	context := s.context
	released := false
	context.pageDone = func(i int, pages []*bo.PageBox) bool {
		if !released && len(pages) <= TwoPassMinPages {
			return true
		}
		// fragmenty pudełek pozycjonowanych dokładane do stron po paginacji (attachLateBoxes)
		for _, late := range context.lateBoxes {
			if len(late) != 0 {
				return s.fail("positioned box continued on an earlier page")
			}
		}
		if !released {
			released = true
			for _, page := range pages[:i] {
				page.Children = nil
			}
		}
		pages[i].Children = nil
		return true
	}
	pages, originalFootnotes := context.paginate(doc, rootBox, -1)
	context.pageDone = nil
	if s.fallback != "" {
		return nil, s.fallback
	}
	if !released {
		return finishPages(context, pages), ""
	}
	s.verifying, s.totalPages, s.originalFootnotes = true, len(pages), originalFootnotes
	return nil, ""
}

// run odpowiada pierwszemu obiegowi pętli w layoutDocument; obiegu drugiego nie ma: gdy byłby potrzebny,
// przebieg kończy się przeszkodą, chyba że strony, które się zmienią, zostały odłożone (finishDeferred).
func (s *streamer) run(doc *tree.HTML, rootBox bo.BlockLevelBoxITF) (int, pr.Float, string) {
	context := s.context
	if s.deferring {
		s.originalFootnotes = append([]Box(nil), context.footnotes...) // jak w paginate
	}
	if s.verifying {
		// jak kolejny obieg w layoutDocument, ale z układaniem wszystkich stron od nowa; ostrzeżenia z układu
		// stron zgłosił już pierwszy przebieg, więc tu są wyciszone (zob. pageDone)
		context.footnotes = append([]Box(nil), s.originalFootnotes...)
		context.verifying = true
		s.warnings = logger.WarningLogger.Writer()
		logger.WarningLogger.SetOutput(io.Discard)
	}

	context.pageDone = s.pageDone
	defer func() { context.pageDone = nil }()
	pages := context.makeAllPages(rootBox, doc, nil)
	if s.verifying {
		logger.WarningLogger.SetOutput(s.warnings)
		context.verifying = false
	}
	if s.fallback != "" {
		return 0, 0, s.fallback
	}

	actualTotalPages := len(pages)
	if s.verifying {
		if context.drift != "" {
			return 0, 0, "two-pass layout: " + context.drift
		}
		if actualTotalPages != s.totalPages {
			return 0, 0, "two-pass layout: page count changed"
		}
	}
	context.TargetCollector.UpdateTargetPages(actualTotalPages, context.pageMaker) // gopdfgo: zob. paginate
	for _, pageData := range context.pageMaker {
		pageData.PageState.CounterValues["pages"] = []int{actualTotalPages}
		if s.verifying || s.deferring {
			// znaczniki ponownego układu: w przebiegu sprawdzającym stan paginacji jest już ustalony (ADR 0036),
			// a przy odkładaniu stron sprawdza je finishDeferred
			continue
		}
		if pageData.RemakeState.ContentChanged {
			s.repaginate = true
			return 0, 0, "content changed after its page was laid out (repagination)"
		}
		if pageData.RemakeState.PagesWanted {
			s.repaginate = true
			return 0, 0, "counter(pages) in page content (repagination)"
		}
	}
	if s.deferring {
		var reason string
		if pages, reason = s.finishDeferred(doc, rootBox, pages); reason != "" {
			return 0, 0, reason
		}
		actualTotalPages = len(pages)
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
	if s.deferring {
		return s.deferPage(i, pages)
	}
	if !s.verifying {
		return s.finishPage(i, pages)
	}
	// obróbka i rysowanie strony zgłaszają ostrzeżenia, układ stron nie (zob. run)
	if s.context.drift != "" {
		return s.fail("two-pass layout: " + s.context.drift)
	}
	logger.WarningLogger.SetOutput(s.warnings)
	ok := s.finishPage(i, pages)
	logger.WarningLogger.SetOutput(io.Discard)
	return ok
}

func (s *streamer) finishPage(i int, pages []*bo.PageBox) bool {
	context := s.context
	page := pages[i]

	// Drugi obieg pętli w layoutDocument jest już przesądzony: znacznik na stronie ułożonej nie jest
	// zerowany do końca obiegu. W przebiegu sprawdzającym znaczniki nie znaczą nic: stan paginacji
	// jest ustalony, a zmianę wartości wykrywa layoutContext.drift. Przy odkładaniu stron znaczniki
	// sprawdza deferPage.
	for k := 0; !s.verifying && !s.deferring && k <= i && k < len(context.pageMaker); k++ {
		if remake := context.pageMaker[k].RemakeState; remake.ContentChanged {
			return s.failRepaginate(fmt.Sprintf("content of page %d changed after layout (target-counter, target-text or counter(pages))", k+1))
		} else if remake.PagesWanted {
			return s.failRepaginate("counter(pages) in page content (repagination)")
		}
	}
	// fragmenty pudełek pozycjonowanych dokładane do stron po paginacji (attachLateBoxes)
	for _, late := range context.lateBoxes {
		if len(late) != 0 {
			return s.fail("positioned box continued on an earlier page")
		}
	}

	if s.finisher.collect(i, page) && !s.verifying {
		return s.failRepaginate("string-set or bookmark-label with counter(pages) or target-counter")
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
	// gopdfgo: według indeksu, bo strona odłożona jest obrabiana po dalszych (zob. deferPage)
	for len(s.fixedSeen) <= i {
		s.fixedSeen = append(s.fixedSeen, nil)
	}
	s.fixedSeen[i] = seen

	scaledHeight := s.finisher.prepare(i, page)
	s.finisher.backgrounds(i, page, scaledHeight)

	if s.measureOverflow {
		s.overflow = max(s.overflow, contentOverflowScale([]*bo.PageBox{page}))
		if limit := pr.Float(context.behavior.MaxShrink); min(s.overflow, limit) > 1.001 {
			s.measuring = true
			// strony narysowane i odłożone idą do kosza; dwa przebiegi mierzą i rysują w skali, jak dotąd
			if s.deferring && len(s.deferred) != 0 {
				return s.failRepaginate("content wider than the page with deferred pages")
			}
			s.deferring = false
		}
	}
	if !s.measuring {
		s.sink.Area(i, page)
	}
	// słowo kluczowe start w string() i element() czyta drzewo strony przy układaniu pól marginesowych
	if context.startChains == nil {
		context.startChains = map[int][]pr.StringSet{}
	}
	context.startChains[i+1] = startChain(page)
	page.Children = nil
	return true
}

// Odkładanie stron (ADR 0036, dopisek trzeci).
//
// Dokument z ponowną paginacją układany starą drogą (layoutDocument) to mieszanka stron z różnych obiegów
// paginacji: strona, której żaden dalszy obieg nie układa od nowa, zostaje z obiegu pierwszego. W spisie
// treści na początku i w „strona X z Y” w treści takich stron jest większość (pomiar w ADR). Pierwszy obieg
// rysuje je więc od razu, jak w ADR 0027, a odkłada tylko strony, które mogą się jeszcze zmienić; po
// ostatnim obiegu rysuje strony odłożone z ich ostatniego układu. Wynik to ta sama mieszanka co w starej
// drodze, bez drugiego układu całego dokumentu. Strona narysowana, którą dalszy obieg układa od nowa, kończy
// przebieg przeszkodą z ponowną paginacją (dwa przebiegi, jak dotąd).

// deferPage jest pageDone przy odkładaniu stron.
func (s *streamer) deferPage(i int, pages []*bo.PageBox) bool {
	context := s.context
	page := pages[i]
	// pudełka fixed są powtarzane na stronach wcześniejszych i dalszych (placeFixedBoxes), co zależy od
	// kolejności obróbki stron
	if len(page.FixedBoxes) != 0 {
		if len(s.deferred) != 0 || s.loop > 0 {
			return s.failRepaginate("fixed box with deferred pages")
		}
		s.deferring = false
		return s.finishPage(i, pages)
	}
	for _, late := range context.lateBoxes {
		if len(late) != 0 {
			return s.fail("positioned box continued on an earlier page")
		}
	}
	if s.loop > 0 {
		// dalszy obieg: od nowa wolno układać tylko strony odłożone
		if !context.pageRemade {
			return true
		}
		d := s.deferred[i]
		if d == nil {
			return s.failRepaginate(fmt.Sprintf("page %d drawn during layout laid out again (repagination)", i+1))
		}
		return s.hold(d, page, len(context.pageMaker)-1)
	}

	// obieg pierwszy: strona narysowana, którą coś oznaczyło do ponownego układu, nie może się już zmienić
	for k := 0; k < i && k < len(context.pageMaker); k++ {
		if remake := context.pageMaker[k].RemakeState; (remake.ContentChanged || remake.PagesWanted) && s.deferred[k] == nil {
			return s.failRepaginate(fmt.Sprintf("content of page %d changed after it was drawn (target-counter, target-text or counter(pages))", k+1))
		}
	}
	remake, pending := s.pageMayChange(i)
	if !remake && !pending {
		return s.finishPage(i, pages)
	}
	if !s.deferSink.Reserve(i, page) {
		return s.failRepaginate("drawing target cannot reserve pages")
	}
	d := &deferredPage{height: page.Height.V(), offset: s.finisher.canvasOffset}
	s.deferred[i] = d
	s.finisher.canvasOffset += d.height
	if s.finisher.firstRoot == nil {
		copied := *page.Children[0].Box()
		s.finisher.firstRoot = &copied
	}
	if remake {
		// strona oznaczona do ponownego układu będzie ułożona w następnym obiegu
		page.Children = nil
		return true
	}
	return s.hold(d, page, i+1)
}

// maxHeldPages: ile drzew stron odłożonych może naraz czekać na narysowanie, gdy ułożono pages stron.
// Drzewo strony to kilkaset KB (pomiar w ADR 0036, dopisek trzeci); dokument, w którym strony z odwołaniami
// do liczników stron są wszędzie, idzie dwoma przebiegami, które drzew nie trzymają.
func maxHeldPages(pages int) int { return 64 + pages/3 }

// hold zachowuje page jako ostatni układ strony odłożonej d.
func (s *streamer) hold(d *deferredPage, page *bo.PageBox, pages int) bool {
	if d.page == nil {
		s.held++
	}
	d.page = page
	if s.held > maxHeldPages(pages) {
		return s.failRepaginate("too many deferred pages")
	}
	return true
}

// release zwalnia zachowane drzewo strony odłożonej d.
func (s *streamer) release(d *deferredPage) {
	if d.page != nil {
		d.page.Children = nil
		d.page = nil
		s.held--
	}
}

// pageMayChange: strona i jest oznaczona do ponownego układu (remake) albo ma odwołania do liczników
// innych stron, które mogą ją jeszcze oznaczyć (pending: target-counter do kotwicy dalszej albo jeszcze
// nieułożonej).
func (s *streamer) pageMayChange(i int) (remake, pending bool) {
	state := s.context.pageMaker[i].RemakeState
	if state.ContentChanged || state.PagesWanted {
		return true, false
	}
	for _, item := range state.ContentLookups {
		if len(item.MissingTargetCounters) != 0 {
			return false, true
		}
	}
	return false, false
}

// finishDeferred kończy obiegi paginacji po obiegu pierwszym (jak layoutContext.paginate), układając od
// nowa tylko strony odłożone, i rysuje strony odłożone w kolejności stron. Zwraca strony dokumentu albo
// opis przeszkody.
func (s *streamer) finishDeferred(doc *tree.HTML, rootBox bo.BlockLevelBoxITF, pages []*bo.PageBox) ([]*bo.PageBox, string) {
	context := s.context
	total := len(pages)
	initialTotal := 0  // jak w paginate: obieg pierwszy zaczyna od 0 stron
	const maxLoops = 8 // jak w paginate
	for loop := 1; ; loop++ {
		reloopContent, reloopPages := false, false
		for _, pageData := range context.pageMaker {
			if pageData.RemakeState.ContentChanged {
				reloopContent = true
			}
			if pageData.RemakeState.PagesWanted {
				reloopPages = initialTotal != total
			}
		}
		if (!reloopContent && !reloopPages) || loop >= maxLoops {
			break
		}
		// następny obieg układa od nowa strony oznaczone: muszą być odłożone, a ich dotychczasowe drzewa
		// nie są już potrzebne
		for k := 0; k < total; k++ {
			remake := context.pageMaker[k].RemakeState
			if !remake.ContentChanged && !remake.PagesWanted {
				continue
			}
			d := s.deferred[k]
			if d == nil {
				return nil, s.failRepaginateReason(fmt.Sprintf("content of page %d changed after it was drawn (target-counter, target-text or counter(pages))", k+1))
			}
			s.release(d)
		}
		logger.ProgressLogger.Printf("Step 5 - Creating layout - Repagination #%d \n", loop)
		context.footnotes = append([]Box(nil), s.originalFootnotes...)
		initialTotal = total
		s.loop = loop
		pages = context.makeAllPages(rootBox, doc, pages)
		if s.fallback != "" {
			return nil, s.fallback
		}
		if len(pages) != total {
			return nil, s.failRepaginateReason("page count changed after pages were drawn")
		}
		context.TargetCollector.UpdateTargetPages(total, context.pageMaker)
		for _, pageData := range context.pageMaker {
			pageData.PageState.CounterValues["pages"] = []int{total}
		}
	}

	// strony odłożone w kolejności stron, każda z przesunięciem tła płótna ze swojego miejsca
	indexes := make([]int, 0, len(s.deferred))
	for i := range s.deferred {
		indexes = append(indexes, i)
	}
	slices.Sort(indexes)
	conflicts := s.deferSink.GlyphConflicts()
	offset := s.finisher.canvasOffset
	for _, i := range indexes {
		d := s.deferred[i]
		page := d.page
		if page == nil || page != pages[i] {
			return nil, s.failRepaginateReason(fmt.Sprintf("deferred page %d was not laid out again", i+1))
		}
		if page.Height.V() != d.height {
			return nil, s.failRepaginateReason(fmt.Sprintf("height of deferred page %d changed", i+1))
		}
		if i == 0 && !sameCanvasGeometry(s.finisher.firstRoot, page.Children[0].Box()) {
			return nil, s.failRepaginateReason("root box of the first page changed")
		}
		s.finisher.canvasOffset = d.offset
		if !s.finishPage(i, pages) {
			return nil, s.fallback
		}
		d.page = nil // drzewo zwolnione przez finishPage
		s.held--
		if s.finisher.outOfOrder {
			return nil, s.failRepaginateReason("bookmark of a deferred page taken by a later page")
		}
	}
	s.finisher.canvasOffset = offset
	if s.deferSink.GlyphConflicts() != conflicts {
		return nil, s.failRepaginateReason("glyph mapping of a deferred page depends on drawing order")
	}
	return pages, ""
}

func (s *streamer) failRepaginateReason(reason string) string {
	s.failRepaginate(reason)
	return reason
}

// sameCanvasGeometry: pudełka elementu głównego dają tło płótna w tym samym miejscu (zob. layoutBackgrounds,
// gdzie pudełko z pierwszej strony służy za początek tła płótna wszystkich stron).
func sameCanvasGeometry(a, b *bo.BoxFields) bool {
	for _, rect := range []string{"border-box", "padding-box", "content-box"} {
		if boxRectangle(*a, rect) != boxRectangle(*b, rect) {
			return false
		}
	}
	return true
}

// noteDrift zapamiętuje pierwszą różnicę przebiegu sprawdzającego względem stanu paginacji (ADR 0036).
func (context *layoutContext) noteDrift(reason string) {
	if context.drift == "" {
		context.drift = reason
	}
}

// checkAnchor: przebieg sprawdzający układa strony z wartościami z pierwszego przebiegu (ADR 0036). Kotwica
// na innej stronie albo z innymi licznikami stron znaczy, że narysowane odwołania do niej są nieaktualne.
// Liczba stron w licznikach kotwic jest po każdym obiegu liczbą stron dokumentu (UpdateTargetPages), więc
// porównujemy wszystkie liczniki, także pages.
func (context *layoutContext) checkAnchor(anchor string, pageIndex int, values tree.CounterValues) {
	item := context.TargetCollector.TargetLookupItems[anchor]
	if item == nil || !item.IsUpToDate() {
		return
	}
	if item.PageMakerIndex != pageIndex {
		context.noteDrift(fmt.Sprintf("anchor %q moved to page %d", anchor, pageIndex+1))
		return
	}
	if !item.CachedPageCounterValues.Equal(values) {
		context.noteDrift(fmt.Sprintf("page counters of anchor %q changed", anchor))
	}
}
