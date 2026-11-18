package boxes

import (
	"strings"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
)

// gopdfgo: white-space-trim (CSS Text 4 § 4.2). Źródło właściwości nie znało. Przycinanie odbywa
// się przed fazą I przetwarzania białych znaków, na całej treści liniowej bloku: discard-before
// i discard-after sięgają poza granice elementu, także przez granice jego przodków i puste pudełka
// liniowe, a zatrzymują się na treści innej niż biały znak, na elemencie atomowym i na białych
// znakach, które nie są zwijalne.

type trimEventKind uint8

const (
	trimText   trimEventKind = iota
	trimStart                // początek pudełka (liniowego albo atomowego, któremu właściwość przysługuje)
	trimEnd                  // koniec takiego pudełka
	trimAtomic               // element atomowy albo blokowy w treści liniowej
)

type trimEvent struct {
	kind trimEventKind
	box  Box
}

// whiteSpaceTrimOf zwraca słowa kluczowe white-space-trim pudełka (pusty napis dla none)
func whiteSpaceTrimOf(box Box) string {
	if v := string(box.Box().Style.GetWhiteSpaceTrim()); v != "none" {
		return v
	}
	return ""
}

// ApplyWhiteSpaceTrim przycina białe znaki w treści liniowej kontenera blokowego box według
// white-space-trim jego potomków liniowych i jego samego (discard-inner kontenera blokowego).
func ApplyWhiteSpaceTrim(box Box) {
	var events []trimEvent
	found := false
	var walk func(parent Box)
	walk = func(parent Box) {
		for _, child := range parent.Box().Children {
			if !child.Box().IsInNormalFlow() {
				continue
			}
			switch c := child.(type) {
			case *TextBox:
				events = append(events, trimEvent{trimText, c})
			case *InlineBox:
				found = found || whiteSpaceTrimOf(c) != ""
				events = append(events, trimEvent{trimStart, c})
				walk(c)
				events = append(events, trimEvent{trimEnd, c})
			default:
				// white-space-trim przysługuje tylko kontenerom blokowym (np. inline-block), nie
				// inline-flex ani elementom zastępowanym
				if BlockContainerT.IsInstance(child) && !ReplacedT.IsInstance(child) && whiteSpaceTrimOf(child) != "" {
					found = true
					events = append(events, trimEvent{trimStart, child}, trimEvent{trimAtomic, child}, trimEvent{trimEnd, child})
				} else {
					events = append(events, trimEvent{trimAtomic, child})
				}
			}
		}
	}
	walk(box)
	own := whiteSpaceTrimOf(box)
	if !found && !strings.Contains(own, "discard-inner") {
		return
	}
	for i, event := range events {
		if event.kind == trimAtomic || event.kind == trimText {
			continue
		}
		value := whiteSpaceTrimOf(event.box)
		if value == "" {
			continue
		}
		if event.kind == trimStart && strings.Contains(value, "discard-before") {
			trimAround(events, i-1, -1, true)
		}
		if event.kind == trimEnd && strings.Contains(value, "discard-after") {
			trimAround(events, i+1, 1, true)
		}
		if _, inline := event.box.(*InlineBox); inline && strings.Contains(value, "discard-inner") {
			if event.kind == trimStart {
				trimAround(events, i+1, 1, false)
			} else {
				trimAround(events, i-1, -1, false)
			}
		}
	}
	if strings.Contains(own, "discard-inner") {
		trimBlockInner(events)
	}
}

// isDocumentWhiteSpace: spacja, tabulator i złamanie segmentu (CSS Text 3 § 4)
func isDocumentWhiteSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }

// isCollapsibleWhiteSpace: biały znak zwijalny przy danym white-space
func isCollapsibleWhiteSpace(r rune, ws pr.Keyword) bool {
	switch ws {
	case pr.Normal, pr.Nowrap:
		return isDocumentWhiteSpace(r)
	case pr.PreLine:
		return r == ' ' || r == '\t'
	}
	return false
}

// trimAround przycina białe znaki w tekstach od zdarzenia from w kierunku step (-1: końce tekstów
// przed, 1: początki tekstów za), aż do pierwszej treści innej niż biały znak albo elementu
// atomowego. Z onlyCollapsible zatrzymuje się na białych znakach niezwijalnych; bez niego usuwa
// każdy biały znak (discard-inner pudełka liniowego), nie wychodząc poza to pudełko.
func trimAround(events []trimEvent, from, step int, onlyCollapsible bool) {
	depth := 0
	for i := from; i >= 0 && i < len(events); i += step {
		event := events[i]
		switch event.kind {
		case trimAtomic:
			return
		case trimStart, trimEnd:
			if !onlyCollapsible {
				// wejście w zagnieżdżone pudełko albo wyjście z własnego
				if (step > 0) == (event.kind == trimStart) {
					depth++
				} else if depth--; depth < 0 {
					return
				}
			}
			continue
		}
		textBox := event.box.(*TextBox)
		ws := textBox.Style.GetWhiteSpace()
		text := textBox.Text
		trim := func(r rune) bool {
			if onlyCollapsible {
				return isCollapsibleWhiteSpace(r, ws)
			}
			return isDocumentWhiteSpace(r)
		}
		if step < 0 {
			n := len(text)
			for n > 0 && trim(text[n-1]) {
				n--
			}
			textBox.Text = text[:n]
		} else {
			n := 0
			for n < len(text) && trim(text[n]) {
				n++
			}
			textBox.Text = text[n:]
		}
		if len(textBox.Text) != 0 {
			return
		}
	}
}

// trimBlockInner: discard-inner kontenera blokowego usuwa białe znaki na początku aż do ostatniego
// złamania segmentu przed pierwszym znakiem innym niż biały (włącznie) i na końcu od pierwszego
// złamania segmentu za ostatnim takim znakiem (CSS Text 4 § 4.2)
func trimBlockInner(events []trimEvent) {
	var texts []*TextBox
	for _, event := range events {
		if event.kind == trimAtomic {
			break
		}
		if event.kind == trimText {
			texts = append(texts, event.box.(*TextBox))
		}
	}
	lastText, lastAt := -1, -1
leading:
	for t, textBox := range texts {
		for i, r := range textBox.Text {
			if !isDocumentWhiteSpace(r) {
				break leading
			}
			if r == '\n' {
				lastText, lastAt = t, i
			}
		}
	}
	for t := 0; t <= lastText; t++ {
		if t == lastText {
			texts[t].Text = texts[t].Text[lastAt+1:]
		} else {
			texts[t].Text = nil
		}
	}

	texts = texts[:0]
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].kind == trimAtomic {
			break
		}
		if events[i].kind == trimText {
			texts = append(texts, events[i].box.(*TextBox))
		}
	}
	firstText, firstAt := -1, -1
trailing:
	for t, textBox := range texts {
		for i := len(textBox.Text) - 1; i >= 0; i-- {
			r := textBox.Text[i]
			if !isDocumentWhiteSpace(r) {
				break trailing
			}
			if r == '\n' {
				firstText, firstAt = t, i
			}
		}
	}
	for t := 0; t <= firstText; t++ {
		if t == firstText {
			texts[t].Text = texts[t].Text[:firstAt]
		} else {
			texts[t].Text = nil
		}
	}
}
