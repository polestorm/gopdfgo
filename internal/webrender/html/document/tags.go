package document

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
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
	// contents: treść oznaczona według właściciela: elementu HTML (*html.Node) albo znacznika elementu
	// listy (listLabel)
	contents   map[any]*backend.MarkedContent
	artifact   *backend.MarkedContent
	pagination *backend.MarkedContent
	warned     map[*html.Node]bool // obrazy bez tekstu alternatywnego już zgłoszone
	// svgLinks: elementy struktury Link odnośników SVG według elementu obrazu, w kolejności adnotacji
	svgLinks map[*html.Node][]*backend.StructureNode
	// svgLinkNodes: element struktury Link według elementu obrazu i numeru elementu <a> w obrazie
	svgLinkNodes map[svgLinkKey]*backend.StructureNode
	// repeated: grupy nagłówka i stopki tabeli (powtarzane na kolejnych stronach) według numeru strony,
	// na której narysowano je pierwszy raz; późniejsze kopie są artefaktami paginacji
	repeated map[*html.Node]int
	// listNumbering: atrybut ListNumbering listy (ul, ol) według list-style-type jej znaczników
	listNumbering map[*html.Node]string
	// grids: siatka tabeli (tableGrid) według komórki, wypełniana przy budowie drzewa struktury
	grids map[*html.Node]map[*html.Node]tableCell
}

// svgLinkKey to klucz elementu <a> obrazu SVG: element obrazu i numer elementu <a> (zob. Link.svgIndex).
type svgLinkKey struct {
	image *html.Node
	index int
}

// listLabel to klucz treści znacznika (::marker) elementu listy: element Lbl w LI (ISO 32000-1
// § 14.8.4.3.3, ISO 14289-1 § 7.6); reszta treści elementu listy należy do LBody.
type listLabel struct{ item *html.Node }

func newTagger() *tagger {
	return &tagger{
		contents:   map[any]*backend.MarkedContent{},
		artifact:   &backend.MarkedContent{},
		pagination: &backend.MarkedContent{Pagination: true},
		warned:     map[*html.Node]bool{},
		svgLinks:   map[*html.Node][]*backend.StructureNode{},

		svgLinkNodes: map[svgLinkKey]*backend.StructureNode{},
		repeated:     map[*html.Node]int{},

		listNumbering: map[*html.Node]string{},
		grids:         map[*html.Node]map[*html.Node]tableCell{},
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
	ctx.setMarked(ctx.tags.content(owner, contentTag(owner)))
}

// content zwraca rodzaj treści właściciela owner (tworzy go przy pierwszym użyciu).
func (t *tagger) content(owner any, tag string) *backend.MarkedContent {
	m := t.contents[owner]
	if m == nil {
		m = &backend.MarkedContent{Tag: tag, Owner: owner}
		t.contents[owner] = m
	}
	return m
}

// markText: tekst pudełka należy do jego elementu, a tekst znacznika (::marker) elementu listy li do
// elementu Lbl tego elementu listy.
func (ctx drawContext) markText(textbox *bo.TextBox) {
	if ctx.tags == nil {
		return
	}
	item := textbox.Element
	switch textbox.PseudoType {
	case "marker", "before::marker", "after::marker":
	default:
		item = nil
	}
	if item == nil || item.DataAtom != atom.Li || item.Namespace != "" || ctx.pagination || ctx.repeatedCopy(item) {
		ctx.markElement(textbox.Element)
		return
	}
	if list := item.Parent; list != nil && structureType(list) == "L" {
		if _, has := ctx.tags.listNumbering[list]; !has {
			ctx.tags.listNumbering[list] = listNumbering(textbox.Style.GetListStyleType().Name)
		}
	}
	ctx.setMarked(ctx.tags.content(listLabel{item}, "Lbl"))
}

// listNumbering odwzorowuje list-style-type na wartość atrybutu ListNumbering (ISO 32000-1 tabela
// 347); "" dla None (wartość domyślna, nie zapisywana): typy bez odpowiednika i znaczniki z content.
func listNumbering(styleType string) string {
	switch styleType {
	case "disc":
		return "Disc"
	case "circle":
		return "Circle"
	case "square":
		return "Square"
	case "decimal", "decimal-leading-zero":
		return "Decimal"
	case "upper-roman":
		return "UpperRoman"
	case "lower-roman":
		return "LowerRoman"
	case "upper-alpha", "upper-latin":
		return "UpperAlpha"
	case "lower-alpha", "lower-latin":
		return "LowerAlpha"
	}
	return ""
}

// contentTag: znacznik BDC treści elementu; treść elementu listy (poza znacznikiem) należy do LBody.
func contentTag(owner *html.Node) string {
	if kind := structureType(owner); kind != "LI" {
		return kind
	}
	return "LBody"
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
	if structureType(node) == "Figure" && figureAlt(node) == "" && !ctx.tags.warned[node] {
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
	case atom.Img, atom.Object, atom.Embed, atom.Video, atom.Audio, atom.Canvas, atom.Iframe:
		return "Figure"
	case atom.Figure:
		// figure grupuje treść z podpisem (HTML § 4.4.12: ilustracja, tabela, kod, cytat); Figure
		// w PDF to treść graficzna zastępowana przez Alt (ISO 32000-1 § 14.8.4.5, ISO 14289-1 § 7.3),
		// więc figure to Div, podpis w nim to Caption, a obraz w środku osobny Figure (ADR 0033)
		return "Div"
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

// figureAlt: tekst alternatywny obrazu (alternativeText), a bez niego podpis figcaption, gdy obraz jest
// jedyną treścią osadzoną elementu figure: HTML pozwala wtedy pominąć alt, bo opis niesie podpis
// (HTML § 4.8.4.4.11, „images whose contents are not known”; HTML-AAM: nazwą figure jest figcaption).
func figureAlt(node *html.Node) string {
	if alt := alternativeText(node); alt != "" {
		return alt
	}
	var figure *html.Node
	for n := node.Parent; n != nil; n = n.Parent {
		if n.Type == html.ElementNode && n.DataAtom == atom.Figure && n.Namespace == "" {
			figure = n
			break
		}
	}
	if figure == nil {
		return ""
	}
	var caption *html.Node
	for child := figure.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.DataAtom == atom.Figcaption {
			caption = child
			break
		}
	}
	if caption == nil {
		return ""
	}
	images := 0
	var count func(n *html.Node)
	count = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if structureType(c) == "Figure" {
				images++
				continue
			}
			if !opaqueContent(c) {
				count(c)
			}
		}
	}
	count(figure)
	if images != 1 {
		return ""
	}
	return strings.Join(strings.Fields(textContent(caption)), " ")
}

// tableCell to położenie komórki w siatce tabeli HTML.
type tableCell struct {
	x, y, width, height int
	header              bool
}

// tableGrid ustala siatkę tabeli według modelu tabel HTML (HTML § 4.9.12.1, „forming a table”):
// wiersze w kolejności dokumentu, komórki na pierwszych wolnych miejscach wiersza, colspan 1–1000,
// rowspan 0–65534 (0: do końca grupy wierszy). Komórka sięgająca za grupę wierszy jest przycinana do
// jej końca, jak przy rysowaniu (CSS 2 § 17.5). Zwraca komórki i liczbę zajętych kolumn każdego wiersza.
func tableGrid(table *html.Node) (map[*html.Node]tableCell, []int) {
	cells := map[*html.Node]tableCell{}
	var widths []int
	var groups [][]*html.Node
	var loose []*html.Node // wiersze wprost w table, bez sekcji
	for child := table.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || child.Namespace != "" {
			continue
		}
		switch child.DataAtom {
		case atom.Tr:
			loose = append(loose, child)
			continue
		case atom.Thead, atom.Tbody, atom.Tfoot:
			if len(loose) != 0 {
				groups, loose = append(groups, loose), nil
			}
			var rows []*html.Node
			for row := child.FirstChild; row != nil; row = row.NextSibling {
				if row.Type == html.ElementNode && row.DataAtom == atom.Tr && row.Namespace == "" {
					rows = append(rows, row)
				}
			}
			groups = append(groups, rows)
		}
	}
	if len(loose) != 0 {
		groups = append(groups, loose)
	}
	y := 0
	for _, rows := range groups {
		occupied := map[[2]int]bool{}
		for i, row := range rows {
			x := 0
			for cell := row.FirstChild; cell != nil; cell = cell.NextSibling {
				if cell.Type != html.ElementNode || cell.Namespace != "" || (cell.DataAtom != atom.Td && cell.DataAtom != atom.Th) {
					continue
				}
				for occupied[[2]int{x, i}] {
					x++
				}
				width := spanAttribute(cell, "colspan", 1, 1000)
				height := spanAttribute(cell, "rowspan", 0, 65534)
				if height == 0 || i+height > len(rows) {
					height = len(rows) - i
				}
				for dy := range height {
					for dx := range width {
						occupied[[2]int{x + dx, i + dy}] = true
					}
				}
				cells[cell] = tableCell{x: x, y: y + i, width: width, height: height, header: cell.DataAtom == atom.Th}
				x += width
			}
		}
		for i := range rows {
			width := 0
			for slot := range occupied {
				if slot[1] == i {
					width++
				}
			}
			widths = append(widths, width)
		}
		y += len(rows)
	}
	return cells, widths
}

// spanAttribute czyta colspan albo rowspan jako nieujemną liczbę całkowitą HTML (wartość błędna:
// 1), ograniczoną do [low, high].
func spanAttribute(node *html.Node, key string, low, high int) int {
	value, has := attribute(node, key)
	if !has {
		return 1
	}
	value = strings.TrimLeft(value, " \t\n\f\r")
	n, digits := 0, 0
	for _, r := range value {
		if r < '0' || r > '9' {
			break
		}
		if n <= high {
			n = n*10 + int(r-'0')
		}
		digits++
	}
	if digits == 0 {
		return 1
	}
	return min(max(n, low), high)
}

// headerScope: zasięg komórki nagłówka (atrybut Scope, ISO 32000-1 tabela 349). Atrybut scope HTML
// (row, col, rowgroup, colgroup) wprost; w stanie auto według HTML § 4.9.12.1 („column header”,
// „row header”): nagłówek kolumny, gdy w jego wierszach nie ma komórek danych, nagłówek wiersza, gdy
// nie ma ich w jego kolumnach, a w pozostałych przypadkach nagłówek obu kierunków.
func headerScope(cell *html.Node, at tableCell, cells map[*html.Node]tableCell) string {
	switch scope, _ := attribute(cell, "scope"); strings.ToLower(strings.TrimSpace(scope)) {
	case "row", "rowgroup":
		return "Row"
	case "col", "colgroup":
		return "Column"
	}
	dataInRows, dataInColumns := false, false
	for _, other := range cells {
		if other.header {
			continue
		}
		if other.y < at.y+at.height && at.y < other.y+other.height {
			dataInRows = true
		}
		if other.x < at.x+at.width && at.x < other.x+other.width {
			dataInColumns = true
		}
	}
	switch {
	case !dataInRows:
		return "Column"
	case !dataInColumns:
		return "Row"
	}
	return "Both"
}

// tableSections porządkuje dzieci Table według ISO 32000-1 tabela 337 (Table: same TR albo THead,
// jedna lub więcej TBody i TFoot; ISO 14289-1 § 7.5): przy sekcjach wiersze leżące wprost w table
// trafiają do TBody (w HTML tbody jest niejawne, HTML § 4.9.5), a tabela z THead bez TBody dostaje
// pusty TBody za THead.
func tableSections(children []*backend.StructureNode) []*backend.StructureNode {
	sections, body := false, false
	for _, c := range children {
		switch c.Type {
		case "THead", "TFoot":
			sections = true
		case "TBody":
			sections, body = true, true
		}
	}
	if !sections {
		return children
	}
	var out []*backend.StructureNode
	var loose *backend.StructureNode
	for _, c := range children {
		if c.Type == "TR" {
			if loose == nil {
				loose = &backend.StructureNode{Type: "TBody", Keep: true}
				out = append(out, loose)
				body = true
			}
			loose.Children = append(loose.Children, c)
			continue
		}
		loose = nil
		out = append(out, c)
	}
	if !body {
		for i, c := range out {
			if c.Type == "THead" {
				out = append(out[:i+1], append([]*backend.StructureNode{{Type: "TBody", Keep: true}}, out[i+1:]...)...)
				break
			}
		}
	}
	return out
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
		table := structureType(node) == "Table"
		if table {
			cells, _ := tableGrid(node)
			for cell := range cells {
				t.grids[cell] = cells
			}
		}
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
				switch kind {
				case "Figure":
					n.Alt = figureAlt(child)
				case "L":
					n.ListNumbering = t.listNumbering[child]
				case "LI":
					// LI zawiera tylko Lbl i LBody (ISO 14289-1 § 7.6, ISO 32000-1 § 14.8.4.3.3)
					n.Owner = nil
					n.Children = []*backend.StructureNode{
						{Type: "Lbl", Owner: listLabel{child}},
						{Type: "LBody", Owner: child, Children: n.Children},
					}
				case "THead", "TBody", "TFoot":
					n.Keep = true
				case "TR":
					// wiersz z komórkami zostaje razem z tabelą, nawet pusty, żeby siatka miała te same
					// wiersze co w HTML; wiersz bez komórek nie ma odpowiednika w siatce
					n.Keep = len(n.Children) != 0
				case "TH", "TD":
					n.Keep = true
					if grid := t.grids[child]; grid != nil {
						cell := grid[child]
						n.RowSpan, n.ColSpan = cell.height, cell.width
						if kind == "TH" {
							n.Scope = headerScope(child, cell, grid)
						}
					}
				}
				children = append(children, n)
			}
		}
		if table {
			children = tableSections(children)
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
		// gopdfgo: ten sam element <a> na kolejnych wycinkach obrazu krojonego między strony to jeden
		// element Link z kilkoma adnotacjami (ADR 0041)
		key := svgLinkKey{link.element, link.svgIndex}
		if node, has := t.svgLinkNodes[key]; has {
			return node
		}
		node := &backend.StructureNode{Type: "Link"}
		node.Owner = node
		t.svgLinks[link.element] = append(t.svgLinks[link.element], node)
		t.svgLinkNodes[key] = node
		return node
	}
	for node := link.element; node != nil; node = node.Parent {
		if node.Type == html.ElementNode && structureType(node) == "Link" {
			return node
		}
	}
	return contentOwner(link.element)
}

// linkContents: opis adnotacji odnośnika (/Contents, ISO 14289-1 § 7.18.5): etykieta aria-label,
// tekst elementu z tekstem alternatywnym obrazów, atrybut title, a bez nich adres celu. Odnośnik SVG
// dostaje adres celu.
func linkContents(link Link) string {
	target := link.Target
	if link.Type == pr.Internal {
		target = "#" + target
	}
	if link.svg {
		return target
	}
	node := link.element
	for n := node; n != nil; n = n.Parent {
		if n.Type == html.ElementNode && isLink(n) {
			node = n
			break
		}
	}
	if node == nil || node.Type != html.ElementNode {
		return target
	}
	if label, _ := attribute(node, "aria-label"); strings.TrimSpace(label) != "" {
		return strings.Join(strings.Fields(label), " ")
	}
	if node.DataAtom == atom.Area {
		if alt := alternativeText(node); alt != "" {
			return alt
		}
	}
	if text := strings.Join(strings.Fields(accessibleText(node)), " "); text != "" {
		return text
	}
	if title, _ := attribute(node, "title"); strings.TrimSpace(title) != "" {
		return strings.Join(strings.Fields(title), " ")
	}
	return target
}

// accessibleText: tekst elementu z tekstem alternatywnym obrazów (alt, aria-label, <title> SVG), bez
// treści skryptów, stylów i szablonów; przybliżenie nazwy dostępnej z treści (W3C Accessible Name
// and Description Computation 1.2 § 4.3.2, krok 2F).
func accessibleText(node *html.Node) string {
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch {
			case c.Type == html.TextNode:
				b.WriteString(c.Data)
			case c.Type != html.ElementNode:
			case structureType(c) == "Figure":
				b.WriteString(" " + alternativeText(c) + " ")
			case c.DataAtom == atom.Br:
				b.WriteString(" ")
			case !opaqueContent(c):
				// elementy liniowe sklejają się z sąsiednim tekstem, pozostałe są od niego oddzielone
				switch structureType(c) {
				case "Span", "Link", "Code", "Quote", "Ruby", "RB", "RT", "RP", "":
					walk(c)
				default:
					b.WriteString(" ")
					walk(c)
					b.WriteString(" ")
				}
			}
		}
	}
	walk(node)
	return b.String()
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
		d.tags.warnStructure(d.root, lang)
		tagged.SetStructure(root, lang)
	}
}

// warnStructure zgłasza wady dokumentu, przez które PDF z tagami nie spełni PDF/UA-1: brak języka
// (ISO 14289-1 § 7.2), pierwszy nagłówek inny niż h1 albo pominięty poziom nagłówka (§ 7.4.2;
// struktury nie poprawiamy, ADR 0033), wiersze tabeli o różnej liczbie kolumn (§ 7.5, ISO 32000-1
// § 14.8.4.3.4). Liczą się tylko elementy z treścią na stronach.
func (t *tagger) warnStructure(root *html.Node, lang string) {
	if lang == "" {
		logger.WarningLogger.Println("Tagged PDF: document without a language (lang on <html>)")
	}
	drawn := map[*html.Node]bool{}
	for owner := range t.contents {
		node, _ := owner.(*html.Node)
		if label, ok := owner.(listLabel); ok {
			node = label.item
		}
		for ; node != nil && !drawn[node]; node = node.Parent {
			drawn[node] = true
		}
	}
	previous := 0
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode || !drawn[c] {
				continue
			}
			kind := structureType(c)
			if level := headingLevel(kind); level != 0 {
				text := strings.Join(strings.Fields(textContent(c)), " ")
				if r := []rune(text); len(r) > 60 {
					text = string(r[:57]) + "..."
				}
				switch {
				case previous == 0 && level != 1:
					logger.WarningLogger.Printf("Tagged PDF: the first heading is <h%d> %q, not <h1> (PDF/UA-1, ISO 14289-1 § 7.4.2)\n", level, text)
				case previous != 0 && level > previous+1:
					logger.WarningLogger.Printf("Tagged PDF: heading level skipped: <h%d> after <h%d>, %q (PDF/UA-1, ISO 14289-1 § 7.4.2)\n", level, previous, text)
				}
				previous = level
			}
			if kind == "Table" {
				if _, widths := tableGrid(c); len(widths) > 1 {
					first := 0
					for _, w := range widths { // wiersz bez komórek nie trafia do struktury
						if first == 0 {
							first = w
						}
						if w != 0 && w != first {
							logger.WarningLogger.Printf("Tagged PDF: table rows with different numbers of columns: <table%s> (PDF/UA-1, ISO 14289-1 § 7.5)\n", describe(c))
							break
						}
					}
				}
			}
			if !opaqueContent(c) {
				walk(c)
			}
		}
	}
	walk(root)
}

// headingLevel: poziom nagłówka H1–H6, 0 dla innych typów.
func headingLevel(kind string) int {
	if len(kind) == 2 && kind[0] == 'H' && kind[1] >= '1' && kind[1] <= '6' {
		return int(kind[1] - '0')
	}
	return 0
}
