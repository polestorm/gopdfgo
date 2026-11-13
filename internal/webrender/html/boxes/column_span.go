package boxes

// gopdfgo: w źródle sprawdzany był tylko styl dziecka kontenera kolumn, więc tabela z column-span: all
// nie rozpinała kolumn.

// IsColumnSpanner mówi, czy dziecko kontenera kolumn rozpina wszystkie kolumny. Dla tabeli właściwość
// column-span stoi na pudełku tabeli, a dzieckiem kontenera jest jej anonimowe opakowanie.
func IsColumnSpanner(child Box) bool {
	// column-span dotyczy tylko pudełek blokowych w normalnym przepływie (CSS Multicol 1 § 6.1)
	if !child.Box().IsInNormalFlow() {
		return false
	}
	if child.Box().Style.GetColumnSpan() == "all" {
		return true
	}
	if child.Box().IsTableWrapper {
		for _, inner := range child.Box().Children {
			if TableT.IsInstance(inner) && inner.Box().Style.GetColumnSpan() == "all" {
				return true
			}
		}
	}
	return false
}
