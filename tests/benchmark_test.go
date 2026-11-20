package tests

import (
	"context"
	"fmt"
	"github.com/polestorm/gopdfgo"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
	options := gopdfgo.Options{Fonts: testFonts(b)}
	b.ReportAllocs()
	for b.Loop() {
		result, err := gopdfgo.Render(context.Background(), document, options)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(result.Pages), "stron")
	}
}

// BenchmarkCorpus mierzy czas renderowania każdego dokumentu z testdata/documents (samo renderowanie,
// razem z czytaniem fontów dokumentu, bez startu procesu). Wyniki z datą zapisujemy w docs/benchmark.md.
func BenchmarkCorpus(b *testing.B) {
	documents, _ := filepath.Glob("../testdata/documents/*.html")
	if len(documents) == 0 {
		b.Fatal("brak dokumentów w testdata/documents")
	}
	for _, path := range documents {
		b.Run(strings.TrimSuffix(filepath.Base(path), ".html"), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				result, err := renderFileResult(path, "../testdata", "")
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(result.Pages), "stron")
			}
		})
	}
}

// peakLiveHeap renderuje dokument i zwraca największy przyrost żywej sterty
// zaobserwowany w trakcie (odśmiecanie wymuszane co 25 ms, więc czas przebiegu
// nie jest miarodajny).
func peakLiveHeap(t testing.TB, document []byte) (pages int, peak uint64) {
	options := gopdfgo.Options{Fonts: testFonts(t)}
	var stats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&stats)
	before := stats.HeapAlloc
	stop := make(chan struct{})
	done := make(chan uint64)
	go func() {
		var highest uint64
		var stats runtime.MemStats
		tick := time.NewTicker(25 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				done <- highest
				return
			case <-tick.C:
				runtime.GC()
				runtime.ReadMemStats(&stats)
				highest = max(highest, stats.HeapAlloc)
			}
		}
	}()
	result, err := gopdfgo.Render(context.Background(), document, options)
	close(stop)
	highest := <-done
	if err != nil {
		t.Fatal(err)
	}
	if highest < before {
		return result.Pages, 0
	}
	return result.Pages, highest - before
}

// Pamięć dużej tabeli ma rosnąć z liczbą wierszy łagodnie. Przejęty kod trzymał
// dla 2000 wierszy ok. 360 MB żywej sterty (cztery pełne style na element);
// po współdzieleniu wartości obliczonych jest to ok. 160 MB. Próg zostawia
// zapas na różnice między platformami, a łapie powrót do stylu na element.
func TestLargeTablePeakMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("pomiar pamięci pomijany w trybie -short")
	}
	const rows, limit = 2000, 240 << 20
	pages, peak := peakLiveHeap(t, largeTableDocument(rows))
	t.Logf("%d wierszy, %d stron, szczyt żywej sterty %d MB", rows, pages, peak>>20)
	if peak > limit {
		t.Errorf("szczyt żywej sterty %d MB przekracza próg %d MB", peak>>20, limit>>20)
	}
}
