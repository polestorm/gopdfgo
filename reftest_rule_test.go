package gopdfgo

import (
	"fmt"
	"testing"
)

// syntheticPage buduje białą stronę PPM z czarnymi pikselami w podanych miejscach.
func syntheticPage(width, height int, black [][2]int) []byte {
	page := []byte(fmt.Sprintf("P6\n%d %d\n255\n", width, height))
	data := make([]byte, width*height*3)
	for i := range data {
		data[i] = 255
	}
	for _, p := range black {
		for c := 0; c < 3; c++ {
			data[(p[1]*width+p[0])*3+c] = 0
		}
	}
	return append(page, data...)
}

// Reguła z ADR 0003: linia grubości jednego piksela to różnica krawędziowa, blok 2x2 już nie.
func TestOnlyEdgeDifferences(t *testing.T) {
	white := syntheticPage(40, 40, nil)
	var line, block, twoLines [][2]int
	for x := 5; x < 35; x++ {
		line = append(line, [2]int{x, 10})
		twoLines = append(twoLines, [2]int{x, 10}, [2]int{x, 12})
		block = append(block, [2]int{x, 10}, [2]int{x, 11})
	}
	cases := []struct {
		name  string
		black [][2]int
		edge  bool
	}{
		{"identyczne", nil, true},
		{"linia 1 px", line, true},
		{"dwie linie 1 px z odstępem", twoLines, true},
		{"pas 2 px", block, false},
		{"blok 2x2", [][2]int{{3, 3}, {4, 3}, {3, 4}, {4, 4}}, false},
		{"para na końcu i początku wiersza", [][2]int{{39, 3}, {0, 4}, {39, 4}, {0, 5}}, true},
	}
	for _, c := range cases {
		if got := onlyEdgeDifferences(white, syntheticPage(40, 40, c.black)); got != c.edge {
			t.Errorf("%s: onlyEdgeDifferences = %v, oczekiwane %v", c.name, got, c.edge)
		}
	}
	if onlyEdgeDifferences(white, syntheticPage(41, 40, nil)) {
		t.Error("strony o różnych rozmiarach nie mogą być zgodne")
	}
}
