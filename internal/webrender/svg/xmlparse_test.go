package svg

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// hasInsideSVG mówi, czy element tag leży wewnątrz elementu svg.
func hasInsideSVG(n *html.Node, tag string, inSVG bool) bool {
	if n.Type == html.ElementNode {
		if n.Data == tag && inSVG {
			return true
		}
		if n.Data == "svg" {
			inSVG = true
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if hasInsideSVG(c, tag, inSVG) {
			return true
		}
	}
	return false
}

func TestSVGDocumentIsXML(t *testing.T) {
	for name, source := range map[string]string{
		// element XHTML w metadanych: parser HTML5 kończył na nim treść SVG
		"xhtml-metadata": `<svg xmlns="http://www.w3.org/2000/svg"><metadata><link xmlns="http://www.w3.org/1999/xhtml" rel="author" href="x"/><meta xmlns="http://www.w3.org/1999/xhtml" name="assert" content="x"/></metadata><rect width="5" height="5"/></svg>`,
		// encja z DTD w stylu Adobe Illustrator
		"dtd-entity": `<?xml version="1.0"?><!DOCTYPE svg [<!ENTITY ns_svg "http://www.w3.org/2000/svg"><!ENTITY st "fill:red">]><svg xmlns="&ns_svg;"><rect style="&st;" width="5" height="5"/></svg>`,
		// niepoprawny XML (atrybut bez cudzysłowu): powrót do parsera HTML5
		"broken-xml": `<svg xmlns="http://www.w3.org/2000/svg"><rect width=5 height="5"/></svg>`,
	} {
		root, err := parseSVGDocument(strings.NewReader(source))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !hasInsideSVG(root, "rect", false) {
			t.Errorf("%s: brak <rect> wewnątrz <svg>", name)
		}
	}
	root, _ := parseSVGDocument(strings.NewReader(`<?xml version="1.0"?><!DOCTYPE svg [<!ENTITY st "fill:red">]><svg xmlns="http://www.w3.org/2000/svg"><rect style="&st;"/></svg>`))
	var style string
	var find func(*html.Node)
	find = func(n *html.Node) {
		for _, a := range n.Attr {
			if a.Key == "style" {
				style = a.Val
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(root)
	if style != "fill:red" {
		t.Errorf("encja z DTD: style = %q", style)
	}
	// xlink:href ma przestrzeń nazw "xlink", jak w drzewie z parsera HTML5
	root, _ = parseSVGDocument(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><use xlink:href="#a"/></svg>`))
	var ns string
	find = func(n *html.Node) {
		for _, a := range n.Attr {
			if a.Key == "href" {
				ns = a.Namespace
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(root)
	if ns != "xlink" {
		t.Errorf("xlink:href: przestrzeń nazw %q", ns)
	}
}
