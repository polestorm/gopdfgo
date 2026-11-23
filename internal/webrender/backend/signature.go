package backend

// gopdfgo: puste pola podpisu (ADR 0051). Plik własny gopdfgo.

// SignatureFieldPage to strona celu, który zapisuje pola formularza podpisu. Pole nie ma wartości
// (/V): podpis dopisuje później program do podpisu. Prostokąt w jednostkach strony, jak w AddExternalLink;
// name to nazwa pola (/T), description jego opis (/TU). W PDF z tagami widżet należy do elementu
// wskazanego wcześniej przez TaggedPage.SetAnnotationOwner.
type SignatureFieldPage interface {
	AddSignatureField(xMin, yMin, xMax, yMax Fl, name, description string)
}
