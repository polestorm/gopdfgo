package model

import (
	"cmp"
	"slices"
)

// Plik dodany w gopdfgo, nie pochodzi ze źródła benoitkugler/pdf.

// SortedKeys zwraca klucze mapy w porządku rosnącym. Zapis PDF nadaje numery obiektom w kolejności
// odwiedzania i wypisuje wpisy słowników w kolejności iteracji, więc iteracja po mapie wprost dawała
// inne bajty przy każdym uruchomieniu. Wszystkie pętle po mapach w tym pakiecie przechodzą przez tę funkcję.
func SortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
