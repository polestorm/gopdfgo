package parser

import "testing"

// gopdfgo: matchNumber ma dawać to samo dopasowanie co numberRe na wszystkich napisach z liter
// istotnych dla wzorca, do długości 7.
func TestMatchNumberAgreesWithRegexp(t *testing.T) {
	alphabet := []byte("09.+-eEa")
	buf := make([]byte, 0, 7)
	var walk func()
	count := 0
	walk = func() {
		count++
		want := -1
		if m := numberRe.FindIndex(buf); m != nil {
			want = m[1]
		}
		if got := matchNumber(buf); got != want {
			t.Fatalf("%q: matchNumber %d, numberRe %d", buf, got, want)
		}
		if len(buf) == cap(buf) {
			return
		}
		for _, c := range alphabet {
			buf = append(buf, c)
			walk()
			buf = buf[:len(buf)-1]
		}
	}
	walk()
	if count < 1000000 {
		t.Fatalf("za mało przypadków: %d", count)
	}
}
