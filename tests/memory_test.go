package tests

import (
	"runtime"
	"testing"
)

// TestRenderReleasesMemory: kolejne renderowania w jednym procesie nie zostawiają po sobie pamięci.
// Do 2026-11-26 obiekty fontów backendu trafiały do mapy globalnej w pakiecie text/draw, której nic nie
// czyściło; każde renderowanie zostawiało w niej swoje sparsowane fonty (ok. 5 MB na dokument z korpusu).
// Pakiet tests przekraczał przez to 7 GB pamięci, a maszyny CI z 7 GB zwalniały i przekraczały limity
// czasu w różnych testach.
func TestRenderReleasesMemory(t *testing.T) {
	file := "../testdata/documents/02-register-table.html"
	live := func() uint64 {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}
	if _, err := renderFile(file, "../testdata", ""); err != nil { // pamięci jednorazowe procesu
		t.Fatal(err)
	}
	before := live()
	const renders = 20
	for range renders {
		if _, err := renderFile(file, "../testdata", ""); err != nil {
			t.Fatal(err)
		}
	}
	// przed poprawką ok. 108 MB; dopuszczamy szum rzędu pojedynczych megabajtów
	if after := live(); after > before+16<<20 {
		t.Errorf("po %d renderowaniach żywa sterta urosła o %d MB", renders, (after-before)>>20)
	}
}
