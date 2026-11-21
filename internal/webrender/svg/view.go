package svg

import (
	"regexp"
	"strings"
)

// gopdfgo: widok dokumentu SVG wskazany fragmentem adresu (SVG 2, rozdział „Linking”, „Linking into
// SVG content: URL fragments and SVG views”). Fragment obrazu SVG (img, tło CSS, object, …) może:
//   - podawać widok wprost: #svgView(viewBox(…);preserveAspectRatio(…);transform(…));
//   - nazywać element <view>: jego viewBox i preserveAspectRatio zastępują atrybuty elementu głównego.
//
// Wartość niepoprawna jednego atrybutu (viewBox z trzema liczbami albo ujemnym wymiarem) jest pomijana
// i zostaje wartość elementu głównego; viewBox o zerowej szerokości albo wysokości wyłącza rysowanie
// (SVG 2, atrybut viewBox). Źródło fragmenty adresów obrazów pomijało.

// View to widok dokumentu SVG z fragmentu adresu; nil oznacza widok domyślny.
type View struct {
	viewBox       *Rectangle           // nil: viewBox elementu głównego
	disabled      bool                 // viewBox o zerowym wymiarze: obraz nie jest rysowany
	preserveRatio *preserveAspectRatio // nil: preserveAspectRatio elementu głównego
	transforms    []transform          // svgView(transform(…)), w układzie użytkownika po viewBox
}

// viewElement to atrybuty elementu <view> zapamiętane przy przetwarzaniu dokumentu.
type viewElement struct {
	viewBox, preserveAspectRatio string
	hasViewBox, hasRatio         bool
}

// collectViews zbiera elementy <view> z identyfikatorem.
func collectViews(tree *svgContext) map[string]viewElement {
	var out map[string]viewElement
	for id, node := range tree.defs {
		if node.tag != "view" {
			continue
		}
		if out == nil {
			out = make(map[string]viewElement)
		}
		var v viewElement
		v.viewBox, v.hasViewBox = node.attrs["viewBox"]
		v.preserveAspectRatio, v.hasRatio = node.attrs["preserveAspectRatio"]
		out[id] = v
	}
	return out
}

// ResolveView zwraca widok wskazany fragmentem adresu (bez znaku #, po zdekodowaniu procentów) albo
// ok == false, gdy fragment nie jest widokiem: nie jest poprawnym svgView(…) ani nazwą elementu <view>
// (fragment wskazujący inny element pokazuje dokument w widoku domyślnym).
func (img *SVGImage) ResolveView(fragment string) (view *View, ok bool) {
	fragment = strings.TrimSpace(fragment)
	if strings.HasPrefix(fragment, "svgView(") {
		return parseSVGViewSpec(fragment)
	}
	// BareName ( "&" timesegment )?
	name := fragment
	if i := strings.IndexByte(fragment, '&'); i >= 0 {
		if !isTimeSegment(fragment[i+1:]) {
			return nil, false
		}
		name = fragment[:i]
	}
	element, found := img.views[name]
	if !found {
		return nil, false
	}
	view = &View{}
	if element.hasViewBox {
		view.setViewBox(element.viewBox)
	}
	if element.hasRatio {
		if ratio, valid := parseRatioStrict(element.preserveAspectRatio); valid {
			view.preserveRatio = &ratio
		}
	}
	return view, true
}

// setViewBox ustawia viewBox widoku; wartość błędna (inna liczba liczb, ujemny wymiar) jest pomijana.
func (v *View) setViewBox(s string) {
	rect, err := parseViewbox(s)
	if err != nil || rect.Width < 0 || rect.Height < 0 {
		return
	}
	if rect.Width == 0 || rect.Height == 0 {
		v.disabled = true
		return
	}
	v.viewBox = &rect
}

// isTimeSegment: wymiar czasowy Media Fragments URI 1.0 (§ 4.2.1, „t=…”). Obraz jest statyczny, więc
// wartość nie ma wpływu na wynik; sprawdzamy tylko, że to ten wymiar.
func isTimeSegment(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "t=") && !strings.Contains(s, "&")
}

var ratioRe = regexp.MustCompile(`^(none|x(Min|Mid|Max)Y(Min|Mid|Max))(\s+(meet|slice))?$`)

// parseRatioStrict czyta wartość preserveAspectRatio; niepoprawna daje ok == false.
func parseRatioStrict(s string) (preserveAspectRatio, bool) {
	s = strings.Join(strings.Fields(s), " ")
	if !ratioRe.MatchString(s) {
		return preserveAspectRatio{}, false
	}
	return parsePreserveAspectRatio(s), true
}

// parseSVGViewSpec czyta SVGViewSpec ( "&" timesegment )? (SVG 2, „SVGFragmentIdentifier”):
//
//	svgView( atrybut ; atrybut … )   atrybut: viewBox(…) | preserveAspectRatio(…) | transform(…) | zoomAndPan(…)
//
// Odstępy są dozwolone wokół elementów składni. Inna nazwa atrybutu albo niedomknięty nawias unieważnia
// cały fragment; to samo inny ciąg po nawiasie zamykającym niż „&t=…” (np. „&xywh=…”, test WPT
// svgview-and-xywh).
func parseSVGViewSpec(s string) (*View, bool) {
	body := s[len("svgView("):]
	depth, end := 1, -1
	for i, c := range body {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, false
	}
	if rest := strings.TrimSpace(body[end+1:]); rest != "" {
		if !strings.HasPrefix(rest, "&") || !isTimeSegment(rest[1:]) {
			return nil, false
		}
	}
	view := &View{}
	for _, attr := range splitTopLevel(body[:end], ';') {
		attr = strings.TrimSpace(attr)
		open := strings.IndexByte(attr, '(')
		if open < 0 || !strings.HasSuffix(attr, ")") {
			return nil, false
		}
		name, value := strings.TrimSpace(attr[:open]), attr[open+1:len(attr)-1]
		switch name {
		case "viewBox":
			view.viewBox, view.disabled = nil, false
			view.setViewBox(value)
		case "preserveAspectRatio":
			if ratio, valid := parseRatioStrict(value); valid {
				view.preserveRatio = &ratio
			}
		case "transform":
			if transforms, err := parseTransform(value); err == nil {
				view.transforms = transforms
			}
		case "zoomAndPan":
			// bez znaczenia dla obrazu statycznego
		default:
			return nil, false
		}
	}
	return view, true
}

// splitTopLevel dzieli s znakiem sep leżącym poza nawiasami.
func splitTopLevel(s string, sep rune) []string {
	var out []string
	depth, start := 0, 0
	for i, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// ApplyView ustawia widok view na czas rysowania albo wyznaczania rozmiaru obrazu i zwraca funkcję
// przywracającą stan; disabled mówi, że widok wyłącza rysowanie. view == nil niczego nie zmienia.
func (img *SVGImage) ApplyView(view *View) (restore func(), disabled bool) {
	if view == nil || img == nil || img.root == nil {
		return func() {}, false
	}
	root := img.root
	savedViewBox, savedContent, savedSynthesized := root.viewbox, root.graphicContent, img.synthesizedViewBox
	if view.viewBox != nil {
		vb := *view.viewBox
		root.viewbox = &vb
		img.synthesizedViewBox = false // viewBox widoku obowiązuje także w <object> (zob. DrawAsDocument)
	}
	if s, ok := root.graphicContent.(svg); ok {
		if view.preserveRatio != nil {
			s.preserveRatio = *view.preserveRatio
		} else if view.viewBox != nil && savedSynthesized {
			s.preserveRatio.none = false // viewBox dopisany rozciągał obraz; widok ma własny viewBox
		}
		s.viewTransforms = view.transforms
		root.graphicContent = s
	}
	return func() {
		root.viewbox, root.graphicContent, img.synthesizedViewBox = savedViewBox, savedContent, savedSynthesized
	}, view.disabled
}
