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
)

// Stream (gopdfgo) układa dokument i rysuje każdą stronę zaraz po tym, jak jej obszar jest ostateczny
// (ADR 0027), zamiast układać wszystkie strony przed rysowaniem (Render i Write). Pola marginesowe są
// dorysowywane w treści stron po ułożeniu wszystkich stron, bo mogą zależeć od ich liczby, a odnośniki,
// kotwice, zakładki i załączniki dochodzą na końcu, jak w Write. Wynik jest taki sam jak z Render
// i Write z ReleaseDrawnPages.
//
// newTarget jest wywoływane przed każdym przebiegiem rysowania; poprzedni cel jest wtedy nieważny.
// Gdy wynik różniłby się od Render i Write, Stream zwraca opis przeszkody, a wywołujący odrzuca cel
// i układa dokument przez Render.
func Stream(html *tree.HTML, stylesheets []tree.CSS, presentationalHints bool, fontConfig text.FontConfiguration,
	newTarget func() backend.Document, zoom pr.Fl, attachments []backend.Attachment,
) (pages int, fallback string) {
	var sink *streamSink
	pages, fallback = layout.LayoutStreaming(html, stylesheets, presentationalHints, fontConfig, func() layout.PageSink {
		sink = &streamSink{
			doc:    Document{urlFetcher: html.UrlFetcher, fontconfig: fontConfig, root: html.Root.AsHtmlNode()},
			target: newTarget(),
			scale:  zoom * 0.75, // 0.75 = 72 PDF point per inch / 96 CSS pixel per inch
		}
		return sink
	})
	if fallback == "" && sink.fallback != "" {
		fallback = sink.fallback
	}
	if fallback != "" {
		return 0, fallback
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
}

func (s *streamSink) Area(index int, pageBox *bo.PageBox) {
	if s.fallback != "" {
		return
	}
	// odpowiednik newPage: kotwice, odnośniki i zakładki obszaru strony; pola marginesowe dojdą w Margins
	page := Page{
		Width: fl(pageBox.MarginWidth()), Height: fl(pageBox.MarginHeight()),
		Bleed: pageBox.Bleed(), anchors: anchors{},
	}
	gatherAnchors(pageBox, page.anchors, &page.links, &page.bookmarks, nil)

	// jak w Write
	scale := s.scale
	pageWidth := scale * (page.Width + fl(page.Bleed.Left) + fl(page.Bleed.Right))
	pageHeight := scale * (page.Height + fl(page.Bleed.Top) + fl(page.Bleed.Bottom))
	left := -scale * fl(page.Bleed.Left)
	top := -scale * fl(page.Bleed.Top)
	right := left + pageWidth
	bottom := top + pageHeight

	outputPage := s.target.AddPage(left/scale, top/scale, (right-left)/scale, (bottom-top)/scale)
	outputPage.State().Transform(mt.New(1, 0, 0, -1, 0, page.Height*scale))
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
