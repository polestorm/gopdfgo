package model

import (
	"math"
	"testing"
)

// Wartości, których PDF nie umie zapisać, stają się zerem i są zliczane.
func TestFmtFloatNonFinite(t *testing.T) {
	NonFiniteNumbers.Store(0)
	for _, value := range []Fl{Fl(math.NaN()), Fl(math.Inf(1)), Fl(math.Inf(-1))} {
		if got := FmtFloat(value); got != "0" {
			t.Errorf("FmtFloat(%v) = %q, oczekiwane \"0\"", value, got)
		}
	}
	if got := FmtFloat(1.5); got != "1.5" {
		t.Errorf("FmtFloat(1.5) = %q", got)
	}
	if count := NonFiniteNumbers.Load(); count != 3 {
		t.Errorf("licznik = %d, oczekiwane 3", count)
	}
	NonFiniteNumbers.Store(0)
}
