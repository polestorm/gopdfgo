package document

import (
	"sort"

	mt "github.com/polestorm/gopdfgo/internal/webrender/matrix"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/text/hyphen"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/layout"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// StreamingDisabled (gopdfgo) wyłącza rysowanie w trakcie układu w gopdfgo.Render (testy i pomiary
// porównawcze); OnStream, gdy nie jest nil, dostaje po każdym renderowaniu opis przeszkody, przez którą
// dokument był rysowany po ułożeniu wszystkich stron, albo "", gdy był rysowany w trakcie układu.
var (
	StreamingDisabled bool
	OnStream          func(fallback string)
	// OnTwoPass, gdy nie jest nil, jest wywoływane, gdy dokument wymagał ponownej paginacji i był układany
	// w dwóch przebiegach (ADR 0036); whole: krótki dokument, ułożony w całości przed rysowaniem.
	OnTwoPass func(whole bool)
	// OnDeferred, gdy nie jest nil, jest wywoływane, gdy dokument wymagał ponownej paginacji, a rysowanie
	// pages stron zostało odłożone na koniec paginacji (ADR 0036, dopisek trzeci)
	OnDeferred func(pages int)
)

// Stream (gopdfgo) układa dokument i rysuje każdą stronę zaraz po tym, jak jej obszar jest ostateczny
// (ADR 0027), zamiast układać wszystkie strony przed rysowaniem (Render i Write). Pola marginesowe są
// dorysowywane w treści stron po ułożeniu wszystkich stron, bo mogą zależeć od ich liczby, a odnośniki,
// kotwice, zakładki i załączniki dochodzą na końcu, jak w Write. Wynik jest taki sam jak z Render
// i Write z ReleaseDrawnPages.
//
// newTarget jest wywoływane przed każdym przebiegiem rysowania; poprzedni cel jest wtedy nieważny.
// restart jest wywoływane, gdy dokument wymaga ponownej paginacji i jest układany od nowa w dwóch
// przebiegach (ADR 0036): wszystko, co powstało wcześniej, łącznie z ostrzeżeniami, idzie do kosza.
// Gdy wynik różniłby się od Render i Write, Stream zwraca opis przeszkody, a wywołujący odrzuca cel
// i układa dokument przez Render.
func Stream(html *tree.HTML, stylesheets []tree.CSS, presentationalHints bool, fontConfig text.FontConfiguration,
	newTarget func() backend.Document, zoom pr.Fl, attachments []backend.Attachment, restart func(),
) (pages int, fallback string) {
	var (
		sink     *streamSink
		laidOut  []*bo.PageBox
		twoPass  bool
		deferred int
	)
	pages, laidOut, twoPass, deferred, fallback = layout.LayoutStreaming(html, stylesheets, presentationalHints, fontConfig, func() layout.PageSink {
		sink = &streamSink{
			doc:    Document{urlFetcher: html.UrlFetcher, fontconfig: fontConfig, root: html.Root.AsHtmlNode()},
			target: newTarget(),
			scale:  zoom * 0.75, // 0.75 = 72 PDF point per inch / 96 CSS pixel per inch
		}
		return sink
	}, restart)
	if OnTwoPass != nil && twoPass {
		OnTwoPass(laidOut != nil)
	}
	if OnDeferred != nil && deferred != 0 {
		OnDeferred(deferred)
	}
	if fallback == "" && sink.fallback != "" {
		fallback = sink.fallback
	}
	if fallback != "" {
		return 0, fallback
	}
	if laidOut != nil {
		// krótki dokument z ponowną paginacją, ułożony w całości (ADR 0036): jak Render i Write
		d := Document{Metadata: html.GetMetadata(), urlFetcher: html.UrlFetcher, fontconfig: fontConfig,
			root: html.Root.AsHtmlNode(), ReleaseDrawnPages: true}
		d.Pages = make([]Page, len(laidOut))
		for i, pageBox := range laidOut {
			d.Pages[i] = newPage(pageBox)
		}
		d.Write(newTarget(), zoom, attachments)
		return pages, ""
	}
	sink.doc.Metadata = html.GetMetadata()
	sink.finish(attachments)
	return pages, ""
}

// streamSink rysuje strony oddawane przez layout.LayoutStreaming.
type streamSink struct {
	doc    Document // strony bez drzew pudełek: wymiary, kotwice, odnośniki, zakładki
	target backend.Document
	scale  fl
	pages  []streamedPage
	// lastBox: granice ostatnio dodanej strony; od nich zależy granica form masek miękkich
	// (pdf.Output.AddPage), więc strona odłożona musi mieć te same
	lastBox [4]fl

	fallback string
}

// streamedPage: strona narysowana bez pól marginesowych.
type streamedPage struct {
	output backend.Page
	// mark: miejsce w treści strony na pola marginesowe; ctx: kontekst rysowania w tym miejscu
	mark   any
	ctx    drawContext
	marked bool
	// empty: element główny ma display: none, strona nie ma treści ani pól marginesowych (drawPage)
	empty bool
	// reserved (gopdfgo): miejsce strony odłożonej (Reserve), której obszar jeszcze nie przyszedł; box to
	// granice strony ze spadem (zob. pageHeader)
	reserved bool
	box      [4]fl
}

var _ layout.DeferringSink = (*streamSink)(nil)

// glyphConflicts: cel, który liczy niezgodne przypisania znaków do glifów (pdf.Output.GlyphConflicts)
type glyphConflicts interface{ GlyphConflicts() int }

// pageHeader: odpowiednik newPage bez rysowania: wymiary i spad strony oraz jej granice ze spadem w punktach
// (left, top, right, bottom), jak w Write
func (s *streamSink) pageHeader(pageBox *bo.PageBox) (Page, [4]fl) {
	page := Page{
		Width: fl(pageBox.MarginWidth()), Height: fl(pageBox.MarginHeight()),
		Bleed: pageBox.Bleed(), anchors: anchors{},
	}
	scale := s.scale
	pageWidth := scale * (page.Width + fl(page.Bleed.Left) + fl(page.Bleed.Right))
	pageHeight := scale * (page.Height + fl(page.Bleed.Top) + fl(page.Bleed.Bottom))
	left := -scale * fl(page.Bleed.Left)
	top := -scale * fl(page.Bleed.Top)
	right := left + pageWidth
	bottom := top + pageHeight
	return page, [4]fl{left, top, right, bottom}
}

// addPage dodaje stronę o granicach box (zob. pageHeader) do celu, jak w Write.
func (s *streamSink) addPage(page Page, box [4]fl) backend.Page {
	scale := s.scale
	left, top, right, bottom := box[0], box[1], box[2], box[3]
	outputPage := s.target.AddPage(left/scale, top/scale, (right-left)/scale, (bottom-top)/scale)
	outputPage.State().Transform(mt.New(1, 0, 0, -1, 0, page.Height*scale))
	s.lastBox = box
	return outputPage
}

// Reserve (layout.DeferringSink) dodaje do celu stronę index, którą Area narysuje później. Tylko cel, który
// liczy niezgodne przypisania glifów, i bez tagów: drzewo struktury zależy od kolejności rysowania.
func (s *streamSink) Reserve(index int, pageBox *bo.PageBox) bool {
	if s.fallback != "" || index != len(s.pages) {
		return false
	}
	if _, ok := s.target.(glyphConflicts); !ok {
		return false
	}
	page, box := s.pageHeader(pageBox)
	outputPage := s.addPage(page, box)
	if tagged, ok := outputPage.(backend.TaggedPage); ok && tagged.Tagged() {
		return false
	}
	s.doc.Pages = append(s.doc.Pages, page)
	s.pages = append(s.pages, streamedPage{output: outputPage, reserved: true, box: box})
	return true
}

// GlyphConflicts (layout.DeferringSink): zob. pdf.Output.GlyphConflicts.
func (s *streamSink) GlyphConflicts() int {
	if target, ok := s.target.(glyphConflicts); ok {
		return target.GlyphConflicts()
	}
	return 0
}

func (s *streamSink) Area(index int, pageBox *bo.PageBox) {
	if s.fallback != "" {
		return
	}
	// odpowiednik newPage: kotwice, odnośniki i zakładki obszaru strony; pola marginesowe dojdą w Margins
	page, box := s.pageHeader(pageBox)
	gatherAnchors(pageBox, page.anchors, &page.links, &page.bookmarks, nil)
	scale := s.scale
	left, top, right, bottom := box[0], box[1], box[2], box[3]

	var outputPage backend.Page
	reserved := index < len(s.pages) && s.pages[index].reserved
	switch {
	case reserved:
		// strona odłożona (Reserve): ta sama wielkość co przy rezerwacji i co ostatnia dodana strona
		reservedPage := s.doc.Pages[index]
		if box != s.pages[index].box || box != s.lastBox || page.Width != reservedPage.Width ||
			page.Height != reservedPage.Height || page.Bleed != reservedPage.Bleed {
			s.fallback = "deferred page has a different size"
			return
		}
		outputPage = s.pages[index].output
	case index == len(s.pages):
		outputPage = s.addPage(page, box)
	default:
		s.fallback = "pages drawn out of order"
		return
	}
	streamed := streamedPage{output: outputPage}
	for _, child := range pageBox.Children {
		if child.Box().RootDisplayNone {
			streamed.empty = true
		}
	}
	resumable, canResume := outputPage.(backend.ResumablePage)
	if !canResume {
		s.fallback = "drawing target cannot resume page content"
		return
	}
	// odpowiednik Page.Paint(outputPage, fontconfig, 0, 0, scale, false)
	outputPage.OnNewStack(func() {
		outputPage.State().Transform(mt.New(scale, 0, 0, scale, 0, 0))
		ctx := drawContext{
			dst:               outputPage,
			fonts:             s.doc.fontconfig,
			hyphenCache:       make(map[text.HyphenDictKey]hyphen.Hyphener),
			strutLayoutsCache: make(map[text.StrutLayoutKey][2]pr.Float),
			filteredImages:    make(map[string]*filteredImage),
			tags:              s.doc.pageTagger(outputPage), // gopdfgo: zob. tags.go
			pageIndex:         index,
		}
		ctx.pageMark = func(at drawContext) {
			if streamed.marked || any(at.dst) != any(outputPage) {
				streamed.marked = false
				streamed.mark = nil
				return
			}
			at.pageMark = nil
			streamed.ctx, streamed.mark, streamed.marked = at, resumable.MarkContent(), true
		}
		ctx.drawPage(pageBox)
	})
	gatherSVGLinks(pageBox, &page.svgLinks, nil) // gopdfgo: po narysowaniu, zob. gatherSVGLinks
	if !streamed.empty && !streamed.marked {
		s.fallback = "page content cannot be resumed for margin boxes"
		return
	}
	// treść narysowana czeka na pola marginesowe w postaci skompresowanej
	resumable.FlushContent(streamed.mark)

	setMediaBoxes(page.Bleed, [4]fl{left, top, right, bottom}, outputPage)
	// gopdfgo: page-orientation obraca stronę po ułożeniu, przy prezentacji (CSS Page 3 § 7.2)
	if rotatable, ok := outputPage.(backend.RotatablePage); ok {
		switch pageBox.Style.GetPageOrientation() {
		case "rotate-right":
			rotatable.SetRotation(90)
		case "rotate-left":
			rotatable.SetRotation(270)
		}
	}
	if reserved {
		s.doc.Pages[index], s.pages[index] = page, streamed
		return
	}
	s.doc.Pages = append(s.doc.Pages, page)
	s.pages = append(s.pages, streamed)
}

func (s *streamSink) Margins(index int, pageBox *bo.PageBox, boxes []Box) string {
	if s.fallback != "" {
		return s.fallback
	}
	page := &s.doc.Pages[index]
	for _, box := range boxes {
		gatherAnchors(box, page.anchors, &page.links, &page.bookmarks, nil)
	}
	streamed := &s.pages[index]
	if streamed.empty || len(boxes) == 0 {
		streamed.ctx = drawContext{}
		return ""
	}
	// kolejność z NewStackingContextFromPage: pola marginesowe leżą po elemencie głównym i obszarze
	// przypisów; z-index ujemny rysuje się przed płótnem, czyli przed miejscem zapamiętanym w treści
	var zero, positive []StackingContext
	for _, box := range boxes {
		context := NewStackingContextFromBox(box, pageBox, nil)
		switch {
		case context.zIndex < 0:
			return "margin box with negative z-index"
		case context.zIndex == 0:
			zero = append(zero, context)
		default:
			positive = append(positive, context)
		}
	}
	sort.SliceStable(positive, func(i, j int) bool { return positive[i].zIndex < positive[j].zIndex })

	finish, ok := streamed.output.(backend.ResumablePage).ResumeContent(streamed.mark)
	if !ok {
		return "page content cannot be resumed for margin boxes"
	}
	ctx := streamed.ctx
	for _, context := range zero {
		ctx.drawStackingContext(context)
	}
	for _, context := range positive {
		ctx.drawStackingContext(context)
	}
	finish()
	for _, box := range boxes {
		gatherSVGLinks(box, &page.svgLinks, nil) // gopdfgo: po narysowaniu, zob. gatherSVGLinks
	}
	streamed.ctx, streamed.mark = drawContext{}, nil
	return ""
}

// finish dodaje odnośniki, kotwice, załączniki, zakładki i metadane, jak Write po narysowaniu stron.
func (s *streamSink) finish(attachments []backend.Attachment) {
	d, target := &s.doc, s.target
	pagedLinks, pagedAnchors, _ := d.resolveLinks()
	// files must be embedded before being used on the pages
	d.embedFileAnnotations(pagedLinks, target)
	for i, page := range d.Pages {
		// Draw from the top-left corner
		matrix := mt.New(s.scale, 0, 0, -s.scale, 0, page.Height*s.scale)
		d.addHyperlinks(pagedLinks[i], s.pages[i].output, matrix)
		d.scaleAnchors(pagedAnchors[i], matrix)
	}
	target.CreateAnchors(pagedAnchors)

	logger.ProgressLogger.Println("Step 7 - Adding PDF metadata")
	as := attachments
	for _, a := range d.Metadata.Attachments {
		t := d.fetchAttachment(a.URL)
		t.Description = a.Title // gopdfgo: atrybut title elementu <link> jako opis pliku, jak w WeasyPrint
		if len(t.Content) != 0 {
			as = append(as, t)
		}
	}
	target.SetAttachments(as)
	target.SetBookmarks(d.makeBookmarkTree())
	target.SetMetadata(utils.VersionString, d.Metadata)
	d.setStructure(target) // gopdfgo: zob. tags.go
}
