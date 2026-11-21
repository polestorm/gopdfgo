package document

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
)

// gopdfgo: PDF z tagami (ADR 0033). Plik własny gopdfgo.
//
// Rysowanie mówi płótnu strony, do którego elementu należy treść (markElement) albo że jest artefaktem
// (markArtifact); płótno otacza nią operatory rysujące (backend.MarkingCanvas). Kluczem treści jest
// węzeł elementu HTML: żyje do końca renderowania, podczas gdy pudełka stron są zwalniane po narysowaniu
// (ADR 0027). Drzewo struktury powstaje na końcu z drzewa elementów (structureTree), a cel wiąże je
// z treścią stron.

// tagger to stan oznaczania treści wspólny dla wszystkich stron dokumentu.
type tagger struct {
	contents   map[*html.Node]*backend.MarkedContent
	artifact   *backend.MarkedContent
	pagination *backend.MarkedContent
	warned     map[*html.Node]bool // obrazy bez tekstu alternatywnego już zgłoszone
	// svgLinks: elementy struktury Link odnośników SVG według elementu obrazu, w kolejności adnotacji
	svgLinks map[*html.Node][]*backend.StructureNode
	// repeated: grupy nagłówka i stopki tabeli (powtarzane na kolejnych stronach) według numeru strony,
	// na której narysowano je pierwszy raz; późniejsze kopie są artefaktami paginacji
	repeated map[*html.Node]int
}

func newTagger() *tagger {
	return &tagger{
		contents:   map[*html.Node]*backend.MarkedContent{},
		artifact:   &backend.MarkedContent{},
		pagination: &backend.MarkedContent{Pagination: true},
		warned:     map[*html.Node]bool{},
		svgLinks:   map[*html.Node][]*backend.StructureNode{},
		repeated:   map[*html.Node]int{},
	}
}

func (ctx drawContext) setMarked(m *backend.MarkedContent) {
	if marking, ok := ctx.dst.(backend.MarkingCanvas); ok {
		marking.SetMarkedContent(m)
	}
}

// markArtifact: treść rysowana od teraz to artefakt (tła, obramowania, cienie); w polu marginesu
// artefakt paginacji.
func (ctx drawContext) markArtifact() {
	if ctx.tags == nil {
		return
	}
	if ctx.pagination {
		ctx.setMarked(ctx.tags.pagination)
		return
	}
	ctx.setMarked(ctx.tags.artifact)
}

// markElement: treść rysowana od teraz należy do elementu node (tekst, obraz, linia dekoracji).
// Element bez odpowiednika w strukturze (body) przekazuje treść najbliższemu przodkowi, który go ma.
func (ctx drawContext) markElement(node *html.Node) {
	if ctx.tags == nil {
		return
	}
	owner := contentOwner(node)
	if ctx.pagination || owner == nil {
		ctx.markArtifact()
		return
	}
	if ctx.repeatedCopy(node) {
		ctx.setMarked(ctx.tags.pagination)
		return
	}
	m := ctx.tags.contents[owner]
	if m == nil {
		m = &backend.MarkedContent{Tag: structureType(owner), Owner: owner}
		ctx.tags.contents[owner] = m
	}
	ctx.setMarked(m)
}

// markBox: treść grupy (krycie, mix-blend-mode, filtr) rysowanej na stronie jako jedna forma należy do
// elementu pudełka tworzącego kontekst nakładania.
func (ctx drawContext) markBox(box Box) {
	if ctx.tags == nil {
		return
	}
	if bo.MarginT.IsInstance(box) || bo.PageT.IsInstance(box) {
		ctx.markArtifact()
		return
	}
	ctx.markElement(box.Box().Element)
}

// markReplaced: obraz elementu zastępowanego to Figure; obraz z pustym alt (HTML: obraz ozdobny),
// obraz treści generowanej i obraz w polu marginesu to artefakty. Obraz bez tekstu alternatywnego
// daje ostrzeżenie (raz na element).
func (ctx drawContext) markReplaced(box *bo.BoxFields) {
	if ctx.tags == nil {
		return
	}
	node := box.Element
	if node == nil || box.PseudoType != "" || ctx.pagination {
		ctx.markArtifact()
		return
	}
	if node.DataAtom == atom.Img {
		if alt, has := attribute(node, "alt"); has && strings.TrimSpace(alt) == "" {
			ctx.markArtifact()
			return
		}
	}
	if structureType(node) == "Figure" && alternativeText(node) == "" && !ctx.tags.warned[node] {
		ctx.tags.warned[node] = true
		logger.WarningLogger.Printf("Tagged PDF: image without alternative text (alt, aria-label or title): <%s%s>\n", node.Data, describe(node))
	}
	ctx.markElement(node)
}

// noteRepeatedGroup zapamiętuje stronę, na której pierwszy raz narysowano grupę nagłówka albo stopki
// tabeli (thead, tfoot i display: table-header-group, table-footer-group).
func (ctx drawContext) noteRepeatedGroup(group Box) {
	if ctx.tags == nil {
		return
	}
	box := group.Box()
	if (!box.IsHeader && !box.IsFooter) || box.Element == nil {
		return
	}
	if _, seen := ctx.tags.repeated[box.Element]; !seen {
		ctx.tags.repeated[box.Element] = ctx.pageIndex
	}
}

// repeatedCopy: node leży w nagłówku albo stopce tabeli powtórzonej z wcześniejszej strony; czytnik
// czyta je raz, w miejscu pierwszego wystąpienia (ISO 32000-1 § 14.8.2.2: artefakty paginacji).
func (ctx drawContext) repeatedCopy(node *html.Node) bool {
	if len(ctx.tags.repeated) == 0 {
		return false
	}
	for ; node != nil; node = node.Parent {
		if first, has := ctx.tags.repeated[node]; has {
			return first < ctx.pageIndex
		}
	}
	return false
}

// closeMarked zamyka otwarty ciąg treści oznaczonej strony (przed miejscem na pola marginesowe,
// żeby obie drogi rysowania dały te same bajty, ADR 0027).
func (ctx drawContext) closeMarked() {
	if ctx.tags == nil {
		return
	}
	if page, ok := ctx.dst.(backend.TaggedPage); ok {
		page.CloseMarkedContent()
	}
}

func describe(node *html.Node) string {
	for _, key := range []string{"src", "data", "id"} {
		if v, has := attribute(node, key); has && v != "" {
			if len(v) > 80 {
				v = v[:77] + "..."
			}
			return " " + key + "=\"" + v + "\""
		}
	}
	return ""
}

func attribute(node *html.Node, key string) (string, bool) {
	for _, a := range node.Attr {
		if a.Key == key && a.Namespace == "" {
			return a.Val, true
		}
	}
	return "", false
}

// contentOwner zwraca element, do którego należy treść elementu node: sam node albo najbliższy przodek
// z typem struktury; nil dla węzła spoza drzewa elementów.
func contentOwner(node *html.Node) *html.Node {
	for node != nil && node.Type != html.ElementNode {
		node = node.Parent
	}
	for node != nil {
		if node.Type == html.ElementNode && structureType(node) != "" {
			return node
		}
		node = node.Parent
	}
	return nil
}

// isLink: element <a> albo <area> z adresem.
func isLink(node *html.Node) bool {
	if node.DataAtom != atom.A && node.DataAtom != atom.Area {
		return false
	}
	_, has := attribute(node, "href")
	return has
}

// structureType odwzorowuje element HTML na typ standardowy ISO 32000-1 § 14.8.4 (ADR 0033 pkt 2);
// "" oznacza element przezroczysty, którego treść i potomkowie należą do rodzica.
func structureType(node *html.Node) string {
	if node.Namespace == "svg" {
		if node.DataAtom == atom.Svg {
			return "Figure"
		}
		return ""
	}
	if node.Namespace == "math" {
		if node.DataAtom == atom.Math {
			return "Formula"
		}
		return ""
	}
	switch node.DataAtom {
	case atom.Html:
		return "Document"
	case atom.Body, atom.Picture, atom.Head, atom.Script, atom.Style, atom.Template, atom.Title,
		atom.Meta, atom.Link, atom.Base, atom.Noscript:
		return ""
	case atom.P, atom.Pre, atom.Address:
		return "P"
	case atom.H1:
		return "H1"
	case atom.H2:
		return "H2"
	case atom.H3:
		return "H3"
	case atom.H4:
		return "H4"
	case atom.H5:
		return "H5"
	case atom.H6:
		return "H6"
	case atom.Ul, atom.Ol, atom.Menu:
		return "L"
	case atom.Li:
		return "LI"
	case atom.Table:
		return "Table"
	case atom.Thead:
		return "THead"
	case atom.Tbody:
		return "TBody"
	case atom.Tfoot:
		return "TFoot"
	case atom.Tr:
		return "TR"
	case atom.Th:
		return "TH"
	case atom.Td:
		return "TD"
	case atom.Caption, atom.Figcaption:
		return "Caption"
	case atom.Img, atom.Object, atom.Embed, atom.Video, atom.Audio, atom.Canvas, atom.Iframe, atom.Figure:
		return "Figure"
	case atom.Blockquote:
		return "BlockQuote"
	case atom.Q:
		return "Quote"
	case atom.Code:
		return "Code"
	case atom.Section:
		return "Sect"
	case atom.Article:
		return "Art"
	case atom.Ruby:
		return "Ruby"
	case atom.Rt:
		return "RT"
	case atom.Rp:
		return "RP"
	case atom.Rb:
		return "RB"
	case atom.A, atom.Area:
		if isLink(node) {
			return "Link"
		}
		return "Span"
	case atom.Span, atom.Em, atom.Strong, atom.B, atom.I, atom.U, atom.S, atom.Small, atom.Mark, atom.Abbr,
		atom.Cite, atom.Dfn, atom.Kbd, atom.Samp, atom.Var, atom.Sub, atom.Sup, atom.Time, atom.Data,
		atom.Label, atom.Bdi, atom.Bdo, atom.Del, atom.Ins, atom.Font, atom.Big, atom.Tt, atom.Strike,
		atom.Nobr, atom.Wbr, atom.Br, atom.Output, atom.Button, atom.Input, atom.Select, atom.Textarea,
		atom.Meter, atom.Progress, atom.Acronym, atom.Center:
		return "Span"
	}
	return "Div"
}

// opaqueContent: element, którego potomkowie w drzewie DOM nie są rysowani jako elementy dokumentu
// (obraz, SVG, MathML, ramka); drzewo struktury do nich nie schodzi.
func opaqueContent(node *html.Node) bool {
	if node.Namespace == "svg" || node.Namespace == "math" {
		return true
	}
	switch node.DataAtom {
	case atom.Img, atom.Object, atom.Embed, atom.Video, atom.Audio, atom.Canvas, atom.Iframe,
		atom.Head, atom.Script, atom.Style, atom.Template, atom.Select, atom.Textarea:
		return true
	}
	return false
}

// alternativeText: tekst alternatywny elementu: alt (img, area, input), aria-label, <title> SVG,
// atrybut title.
func alternativeText(node *html.Node) string {
	if node.DataAtom == atom.Img || node.DataAtom == atom.Area || node.DataAtom == atom.Input {
		if alt, _ := attribute(node, "alt"); strings.TrimSpace(alt) != "" {
			return strings.TrimSpace(alt)
		}
	}
	if label, _ := attribute(node, "aria-label"); strings.TrimSpace(label) != "" {
		return strings.TrimSpace(label)
	}
	if node.Namespace == "svg" {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && child.Data == "title" {
				if text := strings.Join(strings.Fields(textContent(child)), " "); text != "" {
					return text
				}
			}
		}
	}
	if title, _ := attribute(node, "title"); strings.TrimSpace(title) != "" {
		return strings.TrimSpace(title)
	}
	return ""
}

func textContent(node *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(node)
	return b.String()
}

// documentElement zwraca element <html> (root może być węzłem dokumentu).
func documentElement(root *html.Node) *html.Node {
	for n := root; n != nil; {
		if n.Type == html.ElementNode {
			return n
		}
		next := n.FirstChild
		for next != nil && next.Type != html.ElementNode {
			next = next.NextSibling
		}
		n = next
	}
	return nil
}

func language(node *html.Node) (string, bool) {
	if lang, has := attribute(node, "lang"); has {
		return strings.TrimSpace(lang), true
	}
	for _, a := range node.Attr {
		if a.Key == "lang" && a.Namespace == "xml" {
			return strings.TrimSpace(a.Val), true
		}
	}
	return "", false
}

// structureTree buduje drzewo struktury z drzewa elementów w kolejności dokumentu i zwraca je razem
// z językiem dokumentu. Elementy bez treści na stronach odrzuca dopiero cel (nie wie o nich rysowanie).
func (t *tagger) structureTree(root *html.Node) (*backend.StructureNode, string) {
	element := documentElement(root)
	if element == nil {
		return &backend.StructureNode{Type: "Document"}, ""
	}
	lang, _ := language(element)
	var walk func(node *html.Node, inherited string) []*backend.StructureNode
	walk = func(node *html.Node, inherited string) []*backend.StructureNode {
		var children []*backend.StructureNode
		if !opaqueContent(node) {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type != html.ElementNode {
					continue
				}
				childLang := inherited
				own, has := language(child)
				if has {
					childLang = own
				}
				kind := structureType(child)
				if kind == "" {
					children = append(children, walk(child, childLang)...)
					continue
				}
				n := &backend.StructureNode{Type: kind, Owner: child, Children: walk(child, childLang)}
				if has && !strings.EqualFold(own, inherited) {
					n.Lang = own
				}
				if kind == "Figure" {
					n.Alt = alternativeText(child)
				}
				children = append(children, n)
			}
		}
		// odnośniki obrazu SVG: elementy Link wewnątrz jego Figure
		children = append(children, t.svgLinks[node]...)
		return children
	}
	return &backend.StructureNode{Type: "Document", Owner: element, Children: walk(element, lang)}, lang
}

// linkOwner zwraca klucz elementu adnotacji odnośnika: element <a> (albo inny z typem Link) dla
// odnośnika HTML, własny węzeł Link wewnątrz Figure dla odnośnika SVG.
func (t *tagger) linkOwner(link Link) any {
	if link.svg {
		if link.element == nil {
			return nil
		}
		node := &backend.StructureNode{Type: "Link"}
		node.Owner = node
		t.svgLinks[link.element] = append(t.svgLinks[link.element], node)
		return node
	}
	for node := link.element; node != nil; node = node.Parent {
		if node.Type == html.ElementNode && structureType(node) == "Link" {
			return node
		}
	}
	return contentOwner(link.element)
}

// pageTagger zwraca stan oznaczania treści dla strony celu (nil bez tagów), tworząc go przy pierwszej
// stronie z tagami.
func (d *Document) pageTagger(page backend.Page) *tagger {
	if tagged, ok := page.(backend.TaggedPage); !ok || !tagged.Tagged() {
		return nil
	}
	if d.tags == nil {
		d.tags = newTagger()
	}
	return d.tags
}

// setStructure oddaje celowi drzewo struktury, gdy dokument był rysowany z tagami.
func (d *Document) setStructure(target backend.Document) {
	if d.tags == nil {
		return
	}
	if tagged, ok := target.(backend.TaggedDocument); ok {
		root, lang := d.tags.structureTree(d.root)
		tagged.SetStructure(root, lang)
	}
}
