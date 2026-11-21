package backend

// gopdfgo: PDF z tagami (ADR 0033). Plik własny gopdfgo.
//
// Rysowanie (pakiet document) mówi celowi, do czego należy treść rysowana od tej chwili: do elementu
// dokumentu (treść oznaczona z numerem MCID) albo do artefaktu (tło, obramowanie, pole marginesu).
// Cel otacza nią operatory rysujące i zapamiętuje, który numer MCID należy do którego elementu. Na końcu
// rysowanie oddaje drzewo struktury zbudowane z elementów HTML, a cel wiąże je z treścią oznaczoną
// i adnotacjami.

// MarkedContent to rodzaj treści rysowanej od chwili SetMarkedContent. Porównywany jest wskaźnik:
// ta sama wartość oznacza ten sam ciąg treści oznaczonej, jeśli nic go nie przerwało.
type MarkedContent struct {
	// Tag to znacznik operatora BDC: typ struktury elementu (P, Span, Figure); dla artefaktu pomijany.
	Tag string
	// Owner to klucz elementu, do którego należy treść; nil oznacza artefakt.
	Owner any
	// Pagination: artefakt paginacji (pola marginesów, elementy biegnące; ISO 32000-1 § 14.8.2.2.2).
	Pagination bool
}

// MarkingCanvas to płótno, które oznacza treść. Płótna pośrednie (grupy, wzorce, maski) ignorują
// wywołania: ich treść jest oznaczana jako całość tam, gdzie forma jest rysowana na stronie.
type MarkingCanvas interface {
	SetMarkedContent(m *MarkedContent)
}

// TaggedPage to strona celu, który tworzy PDF z tagami.
type TaggedPage interface {
	// Tagged mówi, czy strona oznacza treść; false: rysowanie nie wywołuje pozostałych metod.
	Tagged() bool
	// CloseMarkedContent zamyka otwarty ciąg treści oznaczonej (EMC) w bieżącym miejscu treści.
	CloseMarkedContent()
	// SetAnnotationOwner wskazuje element, do którego należy następna adnotacja odnośnika strony,
	// i jej opis (/Contents, ISO 14289-1 § 7.18.1 i § 7.18.5: tekst odnośnika albo jego etykieta).
	SetAnnotationOwner(owner any, contents string)
}

// StructureNode to węzeł drzewa struktury w kolejności dokumentu. Węzeł bez treści oznaczonej,
// bez adnotacji i bez potomków z treścią nie trafia do pliku (poza korzeniem).
type StructureNode struct {
	Type     string // typ standardowy ISO 32000-1 § 14.8.4
	Owner    any    // klucz treści oznaczonej i adnotacji (MarkedContent.Owner); nil: tylko grupowanie
	Alt      string // tekst alternatywny (/Alt)
	Lang     string // język elementu (/Lang), gdy różni się od języka rodzica
	Children []*StructureNode

	// Keep: węzeł bez treści trafia do pliku, gdy trafia tam jego rodzic (komórki, wiersze i sekcje
	// tabeli: siatka tabeli w strukturze ma te same wiersze i kolumny co w HTML, ISO 14289-1 § 7.5),
	// ale sam rodzica nie zachowuje.
	Keep bool
	// atrybuty właściciela /Table i /List (ISO 32000-1 § 14.8.5.7, tabele 347 i 349); puste i zera
	// pomijane: Scope (Row, Column, Both) komórki TH, RowSpan i ColSpan większe niż 1, ListNumbering L
	Scope            string
	RowSpan, ColSpan int
	ListNumbering    string
}

// TaggedDocument to cel, który przyjmuje drzewo struktury. Wywoływane po narysowaniu wszystkich stron
// i dodaniu odnośników; lang to język dokumentu (/Lang katalogu).
type TaggedDocument interface {
	SetStructure(root *StructureNode, lang string)
}
