package gopdfgo

import (
	"bytes"
	"regexp"
)

// Dokumenty XHTML (np. wynik XSLT) zapisują puste elementy jako <div/>. Parser HTML5 traktuje taki
// znacznik jak otwierający, więc wszystko, co następuje, staje się jego zawartością i drzewo dokumentu
// wychodzi zagnieżdżone zamiast płaskie. Przed parsowaniem rozwijamy więc <div/> do <div></div>.

// voidElements to elementy HTML bez zawartości; ich zapis <br/> jest poprawny także w HTML.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "param": true, "source": true, "track": true, "wbr": true,
}

// foreignRoots to elementy, wewnątrz których obowiązuje składnia XML także w HTML (<path/> w SVG).
var foreignRoots = [][]byte{[]byte("<svg"), []byte("<math")}

var (
	xhtmlNamespace  = []byte(`xmlns="http://www.w3.org/1999/xhtml"`)
	selfClosingTag  = regexp.MustCompile(`<([A-Za-z][A-Za-z0-9]*)((?:\s+[^<>"']*(?:"[^"]*"|'[^']*'))*)\s*/>`)
	foreignElements = regexp.MustCompile(`(?is)<(svg|math)\b.*?</(svg|math)\s*>`)
)

// isXHTML rozpoznaje dokument XHTML po przestrzeni nazw na elemencie html w początku pliku.
func isXHTML(document []byte) bool {
	return bytes.Contains(document[:min(len(document), 2048)], xhtmlNamespace)
}

// expandSelfClosing zamienia <x .../> na <x ...></x> dla elementów, które w HTML nie są puste.
// Fragmenty SVG i MathML zostają bez zmian. Dokumentów bez przestrzeni nazw XHTML nie dotyka.
func expandSelfClosing(document []byte) []byte {
	if !isXHTML(document) {
		return document
	}
	document = stripNamespacePrefixes(document)
	var out bytes.Buffer
	rest := document
	for len(rest) > 0 {
		// fragment do najbliższego elementu obcego przetwarzamy, sam element przepisujemy bez zmian
		loc := foreignElements.FindIndex(rest)
		plain := rest
		if loc != nil {
			plain = rest[:loc[0]]
		}
		out.Write(selfClosingTag.ReplaceAllFunc(plain, func(tag []byte) []byte {
			parts := selfClosingTag.FindSubmatch(tag)
			name := bytes.ToLower(parts[1])
			if voidElements[string(name)] {
				return tag
			}
			expanded := make([]byte, 0, len(tag)+len(name)+3)
			expanded = append(expanded, '<')
			expanded = append(expanded, parts[1]...)
			expanded = append(expanded, parts[2]...)
			expanded = append(expanded, "></"...)
			expanded = append(expanded, parts[1]...)
			return append(expanded, '>')
		}))
		if loc == nil {
			break
		}
		out.Write(rest[loc[0]:loc[1]])
		rest = rest[loc[1]:]
	}
	return out.Bytes()
}

// prefixedNamespace znajduje deklaracje xmlns:p="…" dla przestrzeni nazw, których elementy parser HTML5
// rozpoznaje po samej nazwie: XHTML, SVG i MathML.
var prefixedNamespace = regexp.MustCompile(`xmlns:([A-Za-z][A-Za-z0-9_.-]*)\s*=\s*["']http://www\.w3\.org/(?:1999/xhtml|2000/svg|1998/Math/MathML)["']`)

// stripNamespacePrefixes zamienia w dokumencie XHTML znaczniki <p:nazwa> na <nazwa> dla przedrostków
// związanych z XHTML, SVG lub MathML (Namespaces in XML 1.0: przedrostek jest tylko skrótem przestrzeni
// nazw). Parser HTML5 nie zna przedrostków, więc <svg:svg> był dla niego nieznanym elementem i osadzona
// grafika znikała.
func stripNamespacePrefixes(document []byte) []byte {
	for _, match := range prefixedNamespace.FindAllSubmatch(document, -1) {
		prefix := match[1]
		document = bytes.ReplaceAll(document, append(append([]byte("<"), prefix...), ':'), []byte("<"))
		document = bytes.ReplaceAll(document, append(append([]byte("</"), prefix...), ':'), []byte("</"))
	}
	return document
}
