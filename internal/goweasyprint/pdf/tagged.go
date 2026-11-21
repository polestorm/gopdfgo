package pdf

import (
	"fmt"

	cs "github.com/polestorm/gopdfgo/internal/pdflib/contentstream"
	"github.com/polestorm/gopdfgo/internal/pdflib/model"
	"github.com/polestorm/gopdfgo/internal/webrender/backend"
)

// gopdfgo: treść oznaczona i drzewo struktury (ADR 0033). Plik własny gopdfgo.
//
// Strona oznacza treść leniwie: rysowanie ustawia rodzaj treści (SetMarkedContent), a operator BDC
// trafia do strumienia dopiero przed pierwszym operatorem, który coś maluje (tekst, wypełnienie lub
// obrys ścieżki, obraz, gradient, forma). Kolejne malowanie tej samej treści należy do tego samego
// ciągu. Ciąg jest zamykany przed każdym q i Q z OnNewStack, więc ciągi treści oznaczonej i stany
// graficzne zagnieżdżają się poprawnie; po zamknięciu następne malowanie otwiera nowy ciąg z nowym
// numerem MCID. Ścieżka, która tylko przycina (W n), nie otwiera ciągu: początek ciągu jest wstawiany
// przed konstrukcję ścieżki dopiero wtedy, gdy ścieżka zostaje wypełniona albo obrysowana.

var (
	_ backend.MarkingCanvas  = (*group)(nil)
	_ backend.TaggedPage     = (*outputPage)(nil)
	_ backend.TaggedDocument = (*Output)(nil)
)

// markState to stan oznaczania treści strony; nil w grupach, wzorcach, maskach i bez tagów.
type markState struct {
	desired, open *backend.MarkedContent
	owners        []any          // właściciel według numeru MCID
	tags          map[any]string // znacznik BDC według właściciela

	inPath    bool // trwa konstrukcja ścieżki
	pathStart int  // numer pierwszej operacji ścieżki (OpsLen)
	// hold > 0: wewnątrz obiektu tekstowego albo cudzego ciągu BMC/BDC (WithoutText, kopie filtrów);
	// tam q i Q nie zamykają ciągu, żeby nie przeciąć zagnieżdżenia
	hold int

	annotationOwner    any
	annotationContents string
	annotations        []taggedAnnotation
}

type taggedAnnotation struct {
	annotation *model.AnnotationDict
	owner      any
}

// SetMarkedContent (backend.MarkingCanvas) ustawia rodzaj treści rysowanej od teraz.
func (g *group) SetMarkedContent(m *backend.MarkedContent) {
	if g.marking != nil {
		g.marking.desired = m
	}
}

// beginContent otwiera, jeśli trzeba, ciąg treści oznaczonej przed operatorem malującym dopisywanym
// na końcu strumienia.
func (g *group) beginContent() {
	if g.marking != nil {
		g.beginContentAt(g.stream.OpsLen())
	}
}

// beginContentAt otwiera ciąg przed operacją numer at (zob. GraphicStream.InsertOps).
func (g *group) beginContentAt(at int) {
	m := g.marking
	if m == nil || m.desired == m.open {
		return
	}
	var ops []cs.Operation
	if m.open != nil {
		ops = append(ops, cs.OpEndMarkedContent{})
	}
	switch d := m.desired; {
	case d == nil:
	case d.Owner != nil:
		mcid := len(m.owners)
		m.owners = append(m.owners, d.Owner)
		if _, has := m.tags[d.Owner]; !has {
			m.tags[d.Owner] = d.Tag
		}
		ops = append(ops, cs.OpBeginMarkedContent{Tag: model.ObjName(d.Tag), Properties: cs.PropertyListRaw(fmt.Sprintf("<</MCID %d>>", mcid))})
	case d.Pagination:
		ops = append(ops, cs.OpBeginMarkedContent{Tag: "Artifact", Properties: cs.PropertyListRaw("<</Type/Pagination>>")})
	default:
		ops = append(ops, cs.OpBeginMarkedContent{Tag: "Artifact"})
	}
	m.open = m.desired
	g.stream.InsertOps(at, ops...)
}

// closeMarked zamyka otwarty ciąg treści oznaczonej.
func (g *group) closeMarked() {
	if m := g.marking; m != nil && m.open != nil {
		g.stream.Ops(cs.OpEndMarkedContent{})
		m.open = nil
	}
}

// closeAtStack zamyka ciąg przed q albo Q (zob. markState.hold).
func (g *group) closeAtStack() {
	if m := g.marking; m != nil && m.hold == 0 {
		g.closeMarked()
	}
}

// startPath zapamiętuje początek konstrukcji ścieżki.
func (g *group) startPath() {
	if m := g.marking; m != nil && !m.inPath {
		m.inPath, m.pathStart = true, g.stream.OpsLen()
	}
}

// paintPath: ścieżka zostaje wypełniona albo obrysowana (paints) albo tylko zakończona.
func (g *group) paintPath(paints bool) {
	m := g.marking
	if m == nil {
		return
	}
	if paints {
		at := g.stream.OpsLen()
		if m.inPath {
			at = m.pathStart
		}
		g.beginContentAt(at)
	}
	m.inPath = false
}

// holdMarked oznacza treść przed task i wykonuje task bez zamykania ciągu przy q i Q.
func (g *group) holdMarked(task func()) {
	m := g.marking
	if m == nil {
		task()
		return
	}
	g.beginContent()
	m.hold++
	defer func() { m.hold-- }()
	task()
}

// Tagged (backend.TaggedPage) mówi, czy strona oznacza treść.
func (cp *outputPage) Tagged() bool { return cp.marking != nil }

// CloseMarkedContent (backend.TaggedPage) zamyka otwarty ciąg treści oznaczonej.
func (cp *outputPage) CloseMarkedContent() { cp.closeMarked() }

// SetAnnotationOwner (backend.TaggedPage) wskazuje element i opis następnej adnotacji odnośnika.
func (cp *outputPage) SetAnnotationOwner(owner any, contents string) {
	if cp.marking != nil {
		cp.marking.annotationOwner, cp.marking.annotationContents = owner, contents
	}
}

// addAnnotation dopisuje adnotację do strony i zapamiętuje jej element.
func (cp *outputPage) addAnnotation(an *model.AnnotationDict) {
	cp.page.Annots = append(cp.page.Annots, an)
	if m := cp.marking; m != nil {
		// opis adnotacji dla technologii wspomagających (ISO 14289-1 § 7.18.1, § 7.18.5)
		an.Contents = m.annotationContents
		m.annotations = append(m.annotations, taggedAnnotation{annotation: an, owner: m.annotationOwner})
		m.annotationOwner, m.annotationContents = nil, ""
	}
}

// SetTagged włącza PDF z tagami: strony dodane potem oznaczają treść (ADR 0033).
func (c *Output) SetTagged() { c.tagged = true }

// SetStructure (backend.TaggedDocument) przyjmuje drzewo struktury dokumentu.
func (c *Output) SetStructure(root *backend.StructureNode, lang string) {
	c.structure, c.lang = root, lang
}

// contentItem to odcinek treści oznaczonej (annotation == nil) albo adnotacja elementu.
type contentItem struct {
	seq        int // kolejność rysowania w dokumencie
	page       *model.PageObject
	mcid       int
	annotation *model.AnnotationDict
}

// buildStructure tworzy drzewo struktury PDF (ISO 32000-1 § 14.7) z drzewa elementów i treści
// oznaczonej stron: element dostaje swoje odcinki treści i adnotacje, potomków w kolejności dokumentu,
// a odcinki treści między potomkami według kolejności rysowania. Wywoływane w Finalize.
func (c *Output) buildStructure() {
	items := map[any][]contentItem{}
	var owners []any // właściciele w kolejności pierwszego odcinka, dla treści spoza drzewa
	tags := map[any]string{}
	seq := 0
	for _, p := range c.pages {
		for mcid, owner := range p.marking.owners {
			if _, has := items[owner]; !has {
				owners = append(owners, owner)
			}
			items[owner] = append(items[owner], contentItem{seq: seq, page: &p.page, mcid: mcid})
			seq++
		}
		for owner, tag := range p.marking.tags {
			tags[owner] = tag
		}
	}
	// adnotacje za treścią elementu; element bez treści na stronach ustawiają według końca treści strony
	seq = 0
	for _, p := range c.pages {
		seq += len(p.marking.owners)
		for _, a := range p.marking.annotations {
			if a.owner == nil {
				continue
			}
			if _, has := items[a.owner]; !has {
				owners = append(owners, a.owner)
			}
			items[a.owner] = append(items[a.owner], contentItem{seq: seq, page: &p.page, annotation: a.annotation})
		}
	}

	elementOf := map[any]*model.StructureElement{}
	// build zwraca element węzła, numer jego pierwszego odcinka w kolejności rysowania (-1: brak)
	// i to, czy element ma treść (własną albo potomków); element bez treści wraca tylko z Keep.
	var build func(node *backend.StructureNode, parent *model.StructureElement) (*model.StructureElement, int, bool)
	build = func(node *backend.StructureNode, parent *model.StructureElement) (*model.StructureElement, int, bool) {
		element := &model.StructureElement{S: model.ObjName(node.Type), P: parent, Alt: node.Alt, Lang: node.Lang,
			A: structureAttributes(node)}
		var own []contentItem
		if node.Owner != nil && elementOf[node.Owner] == nil {
			own = items[node.Owner]
			if len(own) != 0 {
				elementOf[node.Owner] = element
			}
		}
		type kid struct {
			element *model.StructureElement
			seq     int
		}
		var kids []kid
		content := len(own) != 0
		for _, child := range node.Children {
			e, s, has := build(child, element)
			if e == nil {
				continue
			}
			if s < 0 && len(kids) != 0 {
				s = kids[len(kids)-1].seq // pusty potomek stoi za poprzednim rodzeństwem
			}
			kids = append(kids, kid{e, s})
			content = content || has
		}
		if !content {
			if !node.Keep {
				return nil, -1, false
			}
			// pusty element zachowany (Keep): bez treści, z potomkami, które też mają Keep
			for _, k := range kids {
				element.K = append(element.K, k.element)
			}
			return element, -1, false
		}
		minSeq := -1
		var marked, annotations []contentItem
		for _, it := range own {
			if it.annotation == nil {
				marked = append(marked, it)
			} else {
				annotations = append(annotations, it)
			}
		}
		if len(marked) != 0 {
			minSeq = marked[0].seq
			element.Pg = marked[0].page
		} else if len(annotations) != 0 {
			minSeq = annotations[0].seq
			element.Pg = annotations[0].page
		}
		for _, k := range kids { // pierwszy potomek z treścią
			if k.seq >= 0 {
				if minSeq < 0 || k.seq < minSeq {
					minSeq = k.seq
				}
				break
			}
		}
		for i, j := 0, 0; i < len(marked) || j < len(kids); {
			if i < len(marked) && (j == len(kids) || marked[i].seq < kids[j].seq) {
				element.K = append(element.K, model.ContentItemMarkedReference{MCID: marked[i].mcid, Container: marked[i].page})
				i++
			} else {
				element.K = append(element.K, kids[j].element)
				j++
			}
		}
		for _, it := range annotations {
			element.K = append(element.K, model.ContentItemObjectReference{Pg: it.page, Obj: it.annotation})
		}
		return element, minSeq, true
	}
	var root *model.StructureElement
	if c.structure != nil {
		root, _, _ = build(c.structure, nil)
	}
	if root == nil {
		root = &model.StructureElement{S: "Document"}
	}
	// treść, której element nie leży w drzewie (np. poza drzewem dokumentu), trafia na koniec korzenia
	for _, owner := range owners {
		if elementOf[owner] != nil {
			continue
		}
		tag := tags[owner]
		if tag == "" {
			tag = "Span"
		}
		orphan, _, _ := build(&backend.StructureNode{Type: tag, Owner: owner}, root)
		if orphan != nil {
			root.K = append(root.K, orphan)
		}
	}

	parents := map[int]model.NumToParent{}
	key := 0
	for _, p := range c.pages {
		if owners := p.marking.owners; len(owners) != 0 {
			elements := make([]*model.StructureElement, len(owners))
			for mcid, owner := range owners {
				elements[mcid] = elementOf[owner]
			}
			p.page.StructParents = model.ObjInt(key)
			parents[key] = model.NumToParent{Parents: elements}
			key++
		}
	}
	for _, p := range c.pages {
		for _, a := range p.marking.annotations {
			element := elementOf[a.owner]
			if element == nil {
				// adnotacja bez elementu: własny element w korzeniu (każda adnotacja należy do struktury)
				element = &model.StructureElement{S: "Link", P: root, Pg: &p.page,
					K: []model.ContentItem{model.ContentItemObjectReference{Pg: &p.page, Obj: a.annotation}}}
				if _, isFile := a.annotation.Subtype.(model.AnnotationFileAttachment); isFile {
					element.S = "Annot"
				}
				root.K = append(root.K, element)
			}
			a.annotation.StructParent = model.ObjInt(key)
			parents[key] = model.NumToParent{Parent: element}
			key++
		}
		if len(p.page.Annots) != 0 {
			p.page.Tabs = "S" // kolejność tabulacji według struktury (ISO 32000-1 § 12.5, tabela 30)
		}
	}

	catalog := &c.document.Catalog
	catalog.StructTreeRoot = &model.StructureTree{K: []*model.StructureElement{root}, ParentTree: model.NewParentTree(parents)}
	catalog.MarkInfo = &model.MarkDict{Marked: true}
	catalog.Lang = c.lang
	catalog.ViewerPreferences = &model.ViewerPreferences{DisplayDocTitle: true}
}

// structureAttributes zapisuje atrybuty węzła jako obiekty atrybutów właścicieli /Table i /List
// (ISO 32000-1 § 14.8.5.7).
func structureAttributes(node *backend.StructureNode) []model.AttributeObject {
	var out []model.AttributeObject
	table := map[model.Name]model.Object{}
	if node.Scope != "" {
		table["Scope"] = model.ObjName(node.Scope)
	}
	if node.RowSpan > 1 {
		table["RowSpan"] = model.ObjInt(node.RowSpan)
	}
	if node.ColSpan > 1 {
		table["ColSpan"] = model.ObjInt(node.ColSpan)
	}
	if len(table) != 0 {
		out = append(out, model.AttributeObject{O: "Table", Attributes: table})
	}
	if node.ListNumbering != "" {
		out = append(out, model.AttributeObject{O: "List", Attributes: map[model.Name]model.Object{"ListNumbering": model.ObjName(node.ListNumbering)}})
	}
	return out
}
