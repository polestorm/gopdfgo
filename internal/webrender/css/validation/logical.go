package validation

import (
	pa "github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// Plik dodany w gopdfgo, nie pochodzi ze źródła webrender.

// Właściwości logiczne (CSS Logical Properties) zamieniamy na fizyczne przed walidacją.
// Przybliżenie: zakładamy pismo poziome od lewej do prawej, czyli block = góra/dół, inline = lewo/prawo;
// przy direction: rtl strony inline wyjdą odwrócone.

var logicalSides = map[string]string{
	"block-start": "top", "block-end": "bottom", "inline-start": "left", "inline-end": "right",
}

// logicalLonghands: nazwy bez odpowiednika wyliczanego wzorcem.
var logicalLonghands = map[string]string{
	"border-start-start-radius": "border-top-left-radius", "border-start-end-radius": "border-top-right-radius",
	"border-end-start-radius": "border-bottom-left-radius", "border-end-end-radius": "border-bottom-right-radius",
	"inline-size": "width", "block-size": "height",
	"min-inline-size": "min-width", "min-block-size": "min-height",
	"max-inline-size": "max-width", "max-block-size": "max-height",
	"inset-block-start": "top", "inset-block-end": "bottom", "inset-inline-start": "left", "inset-inline-end": "right",
}

// logicalPairs: skróty dwuwartościowe i ich właściwości fizyczne (początek, koniec).
var logicalPairs = map[string][2]string{
	"margin-block": {"margin-top", "margin-bottom"}, "margin-inline": {"margin-left", "margin-right"},
	"padding-block": {"padding-top", "padding-bottom"}, "padding-inline": {"padding-left", "padding-right"},
	"inset-block": {"top", "bottom"}, "inset-inline": {"left", "right"},
	"border-block-width": {"border-top-width", "border-bottom-width"}, "border-inline-width": {"border-left-width", "border-right-width"},
	"border-block-style": {"border-top-style", "border-bottom-style"}, "border-inline-style": {"border-left-style", "border-right-style"},
	"border-block-color": {"border-top-color", "border-bottom-color"}, "border-inline-color": {"border-left-color", "border-right-color"},
}

// placeShorthands: skróty wyrównania w obu osiach.
var placeShorthands = map[string][2]string{
	"place-items":   {"align-items", "justify-items"},
	"place-content": {"align-content", "justify-content"},
	"place-self":    {"align-self", "justify-self"},
}

// logicalBorders: skróty obramowania obu stron osi; cała wartość trafia do obu skrótów fizycznych.
var logicalBorders = map[string][2]string{
	"border-block": {"border-top", "border-bottom"}, "border-inline": {"border-left", "border-right"},
}

// physicalName zwraca fizyczny odpowiednik właściwości logicznej albo pusty napis.
func physicalName(name string) string {
	return physicalNames[name]
}

// physicalNames: wszystkie nazwy rozpoznawane przez physicalName, wyliczone raz; wcześniej każde
// wywołanie składało napisy dla każdej strony logicznej (zauważalne przy tysiącach atrybutów style).
var physicalNames = func() map[string]string {
	out := map[string]string{}
	for logical, physical := range logicalLonghands {
		out[logical] = physical
	}
	for logical, side := range logicalSides {
		// margin-block-start, padding-inline-end
		for _, prefix := range [...]string{"margin-", "padding-"} {
			out[prefix+logical] = prefix + side
		}
		// border-block-start, border-inline-end-width, border-block-start-color, ...
		for _, rest := range [...]string{"", "-width", "-style", "-color"} {
			out["border-"+logical+rest] = "border-" + side + rest
		}
	}
	return out
}()

// expandLogical zamienia deklaracje właściwości logicznych na fizyczne. Pomija też deklarację
// print-color-adjust: exact, bo silnik zawsze drukuje tła i kolory dokładnie.
func expandLogical(declarations []pa.Compound) []pa.Compound {
	out := make([]pa.Compound, 0, len(declarations))
	for _, compound := range declarations {
		declaration, ok := compound.(pa.Declaration)
		if !ok {
			out = append(out, compound)
			continue
		}
		name := utils.AsciiLower(declaration.Name)
		switch {
		case name == "print-color-adjust" || name == "-webkit-print-color-adjust":
			if values := withoutWhitespace(declaration.Value); len(values) == 1 && getKeyword(values[0]) == "exact" {
				continue
			}
			out = append(out, compound)
		case name == "-webkit-text-fill-color":
			// gopdfgo: nazwa właściwości z przedrostkiem (Compatibility Standard § 5.2); stała
			// PWebkitTextFillColor ma nazwę bez początkowego łącznika
			declaration.Name = "webkit-text-fill-color"
			out = append(out, declaration)
		case physicalName(name) != "":
			declaration.Name = physicalName(name)
			out = append(out, declaration)
		case logicalPairs[name] != [2]string{}:
			values := withoutWhitespace(declaration.Value)
			if len(values) != 1 && len(values) != 2 {
				out = append(out, compound) // zostanie odrzucona z ostrzeżeniem jako nieznana
				continue
			}
			for i, physical := range logicalPairs[name] {
				side := declaration
				side.Name = physical
				side.Value = []pa.Token{values[min(i, len(values)-1)]}
				out = append(out, side)
			}
		case name == "inset":
			// skrót inset: 1 do 4 wartości jak w margin (góra, prawo, dół, lewo)
			values := withoutWhitespace(declaration.Value)
			if len(values) < 1 || len(values) > 4 {
				out = append(out, compound)
				continue
			}
			order := [4][4]int{{0, 0, 0, 0}, {0, 1, 0, 1}, {0, 1, 2, 1}, {0, 1, 2, 3}}[len(values)-1]
			for i, physical := range [...]string{"top", "right", "bottom", "left"} {
				side := declaration
				side.Name = physical
				side.Value = []pa.Token{values[order[i]]}
				out = append(out, side)
			}
		case placeShorthands[name] != [2]string{}:
			// skróty place-*: pierwsza wartość dla osi blokowej (align-*), druga dla liniowej (justify-*);
			// jedna wartość obowiązuje w obu osiach.
			// gopdfgo: wcześniej skrót dzielono na pojedyncze tokeny, więc "safe end" czy "first baseline"
			// traciły pierwszy wyraz; teraz wartości są dzielone według gramatyki (CSS Box Alignment 3
			// § 5.2, § 6.2, § 6.3), a w place-content wartość <baseline-position> bez drugiej daje
			// justify-content: start.
			alignPart, justifyPart, ok := splitPlaceValue(withoutWhitespace(declaration.Value))
			if !ok {
				out = append(out, compound)
				continue
			}
			if justifyPart == nil {
				justifyPart = alignPart
				if name == "place-content" && getKeyword(alignPart[len(alignPart)-1]) == "baseline" {
					justifyPart = []pa.Token{pa.NewIdent("start", alignPart[0].Pos())}
				}
			}
			for i, physical := range placeShorthands[name] {
				side := declaration
				side.Name = physical
				side.Value = [][]pa.Token{alignPart, justifyPart}[i]
				out = append(out, side)
			}
		case name == "overflow-x" || name == "overflow-y":
			// Silnik ma jedną właściwość overflow dla obu osi, więc wartość inną niż visible w jednej osi
			// przenosimy na obie (CSS Overflow 3 § 3.1: przy przycinaniu w jednej osi druga i tak nie
			// zostaje visible). auto i scroll działają jak w skrócie overflow, czyli przycinają; visible
			// jest pomijane.
			values := withoutWhitespace(declaration.Value)
			if len(values) != 1 {
				out = append(out, compound)
				continue
			}
			switch getKeyword(values[0]) {
			case "hidden", "clip", "auto", "scroll":
				declaration.Name = "overflow"
				out = append(out, declaration)
			case "visible":
			default:
				out = append(out, compound)
			}
		case logicalBorders[name] != [2]string{}:
			for _, physical := range logicalBorders[name] {
				side := declaration
				side.Name = physical
				out = append(out, side)
			}
		default:
			out = append(out, compound)
		}
	}
	return out
}

// splitPlaceValue dzieli wartość skrótu place-* na część align-* i (opcjonalną) część justify-*.
// Pierwsza część ma dwa wyrazy, gdy zaczyna się od first/last (przed baseline) albo od safe/unsafe;
// reszta (jeden lub dwa wyrazy, np. "legacy left") należy do justify-*.
func splitPlaceValue(values []pa.Token) (alignPart, justifyPart []pa.Token, ok bool) {
	if len(values) == 0 {
		return nil, nil, false
	}
	n := 1
	switch getKeyword(values[0]) {
	case "first", "last", "safe", "unsafe":
		n = 2
	}
	if len(values) < n || len(values) > n+2 {
		return nil, nil, false
	}
	if len(values) == n {
		return values, nil, true
	}
	return values[:n], values[n:], true
}
