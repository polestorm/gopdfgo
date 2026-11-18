package gopdfgo

import (
	"bytes"
	"html"
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
	document = keepPreLeadingNewline(decodeStyleEntities(stripNamespacePrefixes(document)))
	var out bytes.Buffer
	rest := document
	for len(rest) > 0 {
		// fragment do najbliższego elementu obcego przetwarzamy, sam element przepisujemy bez zmian
		loc := foreignElements.FindIndex(rest)
		plain := rest
		if loc != nil {
			plain = rest[:loc[0]]
		}
		plain = unwrapCDATA(plain)
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

var preStartWithNewline = regexp.MustCompile(`(?i)(<(?:pre|listing|textarea)\b[^<>]*>)(\r?\n)`)

// keepPreLeadingNewline zachowuje w dokumencie XHTML znak nowego wiersza tuż po znaczniku <pre>.
// Parser HTML5 go pomija (HTML § 13.2.6.4.7), parser XML nie, więc dokument XHTML ma w <pre> o jeden
// pusty wiersz więcej (test WPT c548-ln-ht-000). Dopisujemy jeden znak, który parser HTML zje.
func keepPreLeadingNewline(document []byte) []byte {
	return preStartWithNewline.ReplaceAll(document, []byte("$1\n$2"))
}

var xmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

var styleElement = regexp.MustCompile(`(?is)(<style\b[^>]*>)(.*?)(</style\s*>)`)

// decodeStyleEntities rozwija odwołania znakowe w treści <style> dokumentu XHTML. W XML treść tego
// elementu to zwykłe dane znakowe, więc „.a &gt; .b” oznacza „.a > .b”; parser HTML5 czyta ją dosłownie
// i selektor z encją unieważniał regułę. Sekcji CDATA nie dotykamy: w niej odwołań się nie rozwija.
func decodeStyleEntities(document []byte) []byte {
	return styleElement.ReplaceAllFunc(document, func(element []byte) []byte {
		parts := styleElement.FindSubmatch(element)
		if bytes.Contains(parts[2], []byte("<![CDATA[")) {
			return element
		}
		// Komentarz XML w <style> nie należy do danych znakowych elementu; parser HTML5 zostawia go w
		// arkuszu, gdzie tekst między <!-- a --> psuje następną regułę (wzorzec WPT
		// flexbox-margin-auto-horiz-001-ref).
		content := xmlComment.ReplaceAll(parts[2], nil)
		if !bytes.Contains(content, []byte("&")) {
			if len(content) == len(parts[2]) {
				return element
			}
			return append(append(append([]byte{}, parts[1]...), content...), parts[3]...)
		}
		decoded := html.UnescapeString(string(content))
		if bytes.Contains(bytes.ToLower([]byte(decoded)), []byte("</style")) {
			return element
		}
		out := make([]byte, 0, len(element))
		out = append(out, parts[1]...)
		out = append(out, decoded...)
		return append(out, parts[3]...)
	})
}

var (
	cdataSection = regexp.MustCompile(`(?s)<!\[CDATA\[(.*?)\]\]>`)
	rawTextBlock = regexp.MustCompile(`(?is)<(style|script)\b.*?</(?:style|script)\s*>`)
)

// unwrapCDATA zamienia w treści dokumentu XHTML sekcje CDATA na zwykły tekst z encjami. W XML sekcja CDATA
// to dane znakowe (XML 1.0 § 2.7), a parser HTML5 poza SVG i MathML czyta ją jako komentarz i tekst ginie
// (test WPT first-letter-selector-000). Treść <style> i <script> zostaje bez zmian (sekcje CDATA w arkuszu
// obsługuje tree/style.go).
func unwrapCDATA(document []byte) []byte {
	if !bytes.Contains(document, []byte("<![CDATA[")) {
		return document
	}
	var out bytes.Buffer
	rest := document
	for len(rest) > 0 {
		loc := rawTextBlock.FindIndex(rest)
		plain := rest
		if loc != nil {
			plain = rest[:loc[0]]
		}
		out.Write(cdataSection.ReplaceAllFunc(plain, func(section []byte) []byte {
			return []byte(html.EscapeString(string(cdataSection.FindSubmatch(section)[1])))
		}))
		if loc == nil {
			break
		}
		out.Write(rest[loc[0]:loc[1]])
		rest = rest[loc[1]:]
	}
	return out.Bytes()
}
