package selector

import (
	"golang.org/x/net/html"
)

// gopdfgo: klucze do indeksu selektorów arkusza. Selektor może pasować do elementu tylko wtedy,
// gdy element ma identyfikator, klasę albo nazwę wymaganą przez najbardziej prawy selektor złożony
// (Selectors 4 § 4: wszystkie selektory proste selektora złożonego muszą pasować do tego samego
// elementu). Kaskada sprawdza więc tylko selektory z kubełków elementu i selektory bez klucza,
// zamiast wszystkich selektorów arkusza; dopasowanie rozstrzyga dalej Match.

// KeyKind to rodzaj klucza indeksu.
type KeyKind uint8

const (
	KeyNone  KeyKind = iota // selektor bez klucza: sprawdzany przy każdym elemencie
	KeyID                   // atrybut id równy kluczowi
	KeyClass                // klasa na liście atrybutu class
	KeyTag                  // nazwa elementu
)

// IndexKey zwraca klucz najbardziej prawego selektora złożonego w [s]: identyfikator, jeśli
// selektor go wymaga, inaczej klasę, inaczej nazwę elementu. Element bez tego klucza nie pasuje
// do [s].
func IndexKey(s Sel) (KeyKind, string) {
	for {
		combined, ok := s.(combinedSelector)
		if !ok {
			break
		}
		if combined.combinator == 0 {
			s = combined.first
		} else {
			s = combined.second
		}
	}
	var parts []Sel
	switch s := s.(type) {
	case compoundSelector:
		parts = s.selectors
	case idSelector, classSelector, tagSelector:
		parts = []Sel{s}
	default:
		return KeyNone, ""
	}
	kind, key := KeyNone, ""
	for _, part := range parts {
		switch part := part.(type) {
		case idSelector:
			return KeyID, part.id
		case classSelector:
			if kind != KeyClass {
				kind, key = KeyClass, part.class
			}
		case tagSelector:
			if kind == KeyNone {
				kind, key = KeyTag, part.name()
			}
		}
	}
	return kind, key
}

// name zwraca nazwę, pod którą tagSelector trafia do indeksu; Match przepuszcza tylko element,
// którego n.Data albo nazwa atomu jest równa tej nazwie (zob. ElementKeys).
func (t tagSelector) name() string {
	if t.tag != 0 {
		return t.tag.String()
	}
	return t.tagS
}

// ElementKeys woła [yield] dla każdego klucza indeksu, pod którym może leżeć selektor pasujący do
// [n]: wartości atrybutów id, klas z atrybutów class (podzielonych jak w dopasowaniu klasy) i nazw
// elementu (n.Data oraz nazwy atomu). Klucze mogą się powtarzać.
func ElementKeys(n *html.Node, yield func(KeyKind, string)) {
	for _, a := range n.Attr {
		switch a.Key {
		case "id":
			yield(KeyID, a.Val)
		case "class":
			s := a.Val
			for s != "" {
				i := spaceAsciiSet.index(s)
				if i == -1 {
					yield(KeyClass, s)
					break
				}
				if i > 0 {
					yield(KeyClass, s[:i])
				}
				s = s[i+1:]
			}
		}
	}
	yield(KeyTag, n.Data)
	if n.DataAtom != 0 {
		if name := n.DataAtom.String(); name != n.Data {
			yield(KeyTag, name)
		}
	}
}
