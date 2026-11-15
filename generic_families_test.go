package gopdfgo

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Options.GenericFamilies (ADR 0005): rodzina wskazana dla rodziny generycznej zastępuje wybór silnika,
// także dla fontu domyślnego dokumentu; rodziny nazwane wprost i wpisy o nieznanych rodzinach nic nie zmieniają.
func TestGenericFamilies(t *testing.T) {
	var fonts []Font
	for _, name := range []string{"NotoSans-Regular.ttf", "NotoSerif-Regular.ttf", "tinos/Tinos-Regular.ttf"} {
		data, err := os.ReadFile(filepath.Join("testdata/fonts", name))
		if err != nil {
			t.Fatal(err)
		}
		fonts = append(fonts, Font{Name: filepath.Base(name), Data: data})
	}
	render := func(html string, generic map[string][]string) []byte {
		t.Helper()
		result, err := Render(t.Context(), []byte(html), Options{Fonts: fonts, GenericFamilies: generic})
		if err != nil {
			t.Fatal(err)
		}
		return result.PDF
	}
	tinos := map[string][]string{"serif": {"Tinos"}}
	cases := []struct {
		name     string
		html     string
		generic  map[string][]string
		sameHTML string
		same     map[string][]string
	}{
		{"font domyślny dokumentu", `<p>Zażółć gęślą jaźń</p>`, tinos, `<p style="font-family: Tinos">Zażółć gęślą jaźń</p>`, nil},
		{"serif podany wprost", `<p style="font-family: serif">Zażółć</p>`, tinos, `<p style="font-family: Tinos">Zażółć</p>`, nil},
		{"klucz bez rozróżniania wielkości liter", `<p>Zażółć</p>`, map[string][]string{" Serif ": {"Tinos"}}, `<p>Zażółć</p>`, tinos},
		{"nieznana rodzina jest pomijana", `<p>Zażółć</p>`, map[string][]string{"serif": {"Nie Ma Takiego", "Tinos"}}, `<p>Zażółć</p>`, tinos},
		{"sama nieznana rodzina nic nie zmienia", `<p>Zażółć</p>`, map[string][]string{"serif": {"Nie Ma Takiego"}}, `<p>Zażółć</p>`, nil},
		{"rodzina nazwana wprost wygrywa", `<p style="font-family: 'Noto Serif', serif">Zażółć</p>`, tinos, `<p style="font-family: 'Noto Serif', serif">Zażółć</p>`, nil},
		{"inna rodzina generyczna bez zmian", `<p style="font-family: sans-serif">Zażółć</p>`, tinos, `<p style="font-family: sans-serif">Zażółć</p>`, nil},
		{"sans-serif", `<p style="font-family: sans-serif">Zażółć</p>`, map[string][]string{"sans-serif": {"Tinos"}}, `<p style="font-family: Tinos">Zażółć</p>`, nil},
	}
	for _, c := range cases {
		if !bytes.Equal(render(c.html, c.generic), render(c.sameHTML, c.same)) {
			t.Errorf("%s: wynik inny niż oczekiwany", c.name)
		}
	}
	if bytes.Equal(render(`<p>Zażółć</p>`, tinos), render(`<p>Zażółć</p>`, nil)) {
		t.Error("wskazanie Tinos dla serif nie zmieniło fontu domyślnego")
	}
}
