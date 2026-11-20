package svg

import (
	"bytes"
	"encoding/xml"
	"io"
	"regexp"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// gopdfgo: plik SVG (obraz, <use> z innego pliku) jest dokumentem XML (SVG 2 § 1.1), a źródło czytało go
// parserem HTML5. W HTML5 element XHTML wewnątrz SVG (np. <p> albo <meta> w <metadata> czy <desc>) kończy
// treść SVG (HTML § 13.2.6.5, wyjście z treści obcej), więc reszta rysunku przepadała; XML nie zna też
// encji HTML w rodzaju &nbsp;, za to pozwala definiować własne w DTD (Illustrator: xmlns="&ns_svg;").
// parseSVGDocument czyta plik jako XML i buduje to samo drzewo html.Node, którego oczekuje reszta pakietu:
// elementy SVG z przestrzenią nazw "svg", atrybuty xlink:href z przestrzenią "xlink". Plik, który nie jest
// poprawnym XML-em, jest czytany jak dotąd parserem HTML5.
func parseSVGDocument(r io.Reader) (*html.Node, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if root, err := parseXMLTree(data); err == nil {
		return root, nil
	}
	return html.Parse(bytes.NewReader(data))
}

const (
	svgNamespace   = "http://www.w3.org/2000/svg"
	xlinkNamespace = "http://www.w3.org/1999/xlink"
	xmlNamespace   = "http://www.w3.org/XML/1998/namespace"
	mathNamespace  = "http://www.w3.org/1998/Math/MathML"
)

var entityDeclaration = regexp.MustCompile(`<!ENTITY\s+([A-Za-z_][\w.-]*)\s+(?:"([^"]*)"|'([^']*)')`)

// parseXMLTree buduje drzewo html.Node z dokumentu XML; błąd, gdy dokument nie jest poprawny albo nie ma
// elementu svg.
func parseXMLTree(data []byte) (*html.Node, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	entities := make(map[string]string, len(xml.HTMLEntity)+4)
	for name, value := range xml.HTMLEntity { // tolerancja dla plików z encjami HTML, jak przeglądarki nie robią,
		entities[name] = value // ale lepiej narysować niż odrzucić cały plik
	}
	decoder.Entity = entities
	decoder.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }

	document := &html.Node{Type: html.DocumentNode}
	current := document
	foundSVG := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.Directive:
			// encje z wewnętrznego podzbioru DTD, np. <!ENTITY ns_svg "http://www.w3.org/2000/svg">
			for _, m := range entityDeclaration.FindAllSubmatch(t, -1) {
				entities[string(m[1])] = string(m[2]) + string(m[3])
			}
		case xml.StartElement:
			node := &html.Node{Type: html.ElementNode, Data: t.Name.Local, DataAtom: atom.Lookup([]byte(t.Name.Local))}
			switch t.Name.Space {
			case svgNamespace:
				node.Namespace = "svg"
				foundSVG = foundSVG || t.Name.Local == "svg"
			case mathNamespace:
				node.Namespace = "math"
			}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
					continue // deklaracje przestrzeni nazw rozwiązał już dekoder
				}
				attr := html.Attribute{Key: a.Name.Local, Val: a.Value}
				switch a.Name.Space {
				case xlinkNamespace:
					attr.Namespace = "xlink"
				case xmlNamespace:
					attr.Namespace = "xml"
				}
				node.Attr = append(node.Attr, attr)
			}
			current.AppendChild(node)
			current = node
		case xml.EndElement:
			if current.Parent != nil {
				current = current.Parent
			}
		case xml.CharData:
			if current != document {
				current.AppendChild(&html.Node{Type: html.TextNode, Data: string(t)})
			}
		}
	}
	if !foundSVG {
		return nil, errNoSVGRoot
	}
	return document, nil
}

type xmlError string

func (e xmlError) Error() string { return string(e) }

const errNoSVGRoot = xmlError("svg: no <svg> element in the SVG namespace")
