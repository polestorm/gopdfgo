package gopdfgo

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// largeTableDocument buduje tabelę o podanej liczbie wierszy i siedmiu kolumnach:
// dokument, w którym liczba elementów, a nie złożoność układu, decyduje o czasie
// i pamięci.
func largeTableDocument(rows int) []byte {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><style>td{border:1px solid #888;padding:2px 4px} tr:nth-child(even){background:#eee} .n{text-align:right}</style>`)
	b.WriteString(`<table><thead><tr><th>Lp</th><th>Nazwa</th><th>Ilość</th><th>Cena</th><th>Wartość</th><th>VAT</th><th>Uwagi</th></tr></thead><tbody>`)
	for i := range rows {
		fmt.Fprintf(&b, `<tr><td class="n">%d</td><td>Pozycja numer %d</td><td class="n">%d</td><td class="n">12,34</td><td class="n">%d,00</td><td>23%%</td><td>uwaga</td></tr>`, i, i, i%7, i*3)
	}
	b.WriteString(`</tbody></table>`)
	return []byte(b.String())
}

// BenchmarkLargeTable mierzy czas i przydziały pamięci dla tabeli 4000 wierszy
// (ok. 32 tys. elementów). Wyniki z datą zapisujemy w docs/feature-audit.md.
func BenchmarkLargeTable(b *testing.B) {
	document := largeTableDocument(4000)
	options := Options{Fonts: testFonts(b)}
	b.ReportAllocs()
	for b.Loop() {
		result, err := Render(context.Background(), document, options)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(result.Pages), "stron")
	}
}
