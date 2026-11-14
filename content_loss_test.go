package gopdfgo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Treść dłuższa niż strona nie może ginąć: dla każdego rodzaju kontenera osiem elementów i akapit końcowy
// muszą leżeć w obrębie którejś strony. Przed 2026-11-14 kontenery flex i grid układały dalszą treść
// poniżej dolnej krawędzi strony, a wiersz z pudełkiem inline-block wyższym niż strona wystawał poza nią.
// Gdy silnik mimo wszystko zostawi treść poza stroną, zgłasza ostrzeżenie (TestWarningForContentBelowPage).

var wordBox = regexp.MustCompile(`yMin="([\d.-]+)" xMax="[\d.-]+" yMax="([\d.-]+)">(.*)</word>`)

// visibleWords zwraca słowa, których pole leży w całości w obrębie strony o wysokości pageHeight punktów.
func visibleWords(t *testing.T, pdf []byte, pageHeight float64) map[string]bool {
	t.Helper()
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("brak pdftotext")
	}
	file := filepath.Join(t.TempDir(), "out.pdf")
	if err := os.WriteFile(file, pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("pdftotext", "-bbox", file, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	visible := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if m := wordBox.FindStringSubmatch(line); m != nil {
			top, _ := strconv.ParseFloat(m[1], 64)
			bottom, _ := strconv.ParseFloat(m[2], 64)
			if top >= 0 && bottom <= pageHeight+0.5 {
				visible[m[3]] = true
			}
		}
	}
	return visible
}

func TestNoContentLostAcrossPages(t *testing.T) {
	const style = "<style>@page { size: 300px 200px; margin: 20px } body { margin: 0; font-size: 12px } " +
		".i { height: 50px; box-sizing: border-box; border: 1px solid #000 }</style>"
	const end = "<p>koniec</p>"
	var items, listItems string
	for i := 1; i <= 8; i++ {
		items += fmt.Sprintf(`<div class="i">e%d</div>`, i)
		listItems += fmt.Sprintf(`<li class="i">e%d</li>`, i)
	}
	cases := []struct{ name, html string }{
		{"blok", `<div>` + items + `</div>` + end},
		{"element pływający", `<div style="float: left; width: 80%">` + items + `</div><div style="clear: both"></div>` + end},
		{"pozycjonowany bezwzględnie", `<div style="position: absolute; top: 0; left: 0; width: 80%">` + items + end + `</div>`},
		{"overflow: hidden", `<div style="overflow: hidden">` + items + `</div>` + end},
		{"stała wysokość większa niż treść", `<div style="height: 500px">` + items + `</div>` + end},
		{"min-height", `<div style="min-height: 600px">` + items + `</div>` + end},
		{"stała wysokość mniejsza niż treść", `<div style="height: 100px">` + items + `</div>` + end},
		{"komórka tabeli", `<table><tr><td>` + items + `</td></tr></table>` + end},
		{"display: table", `<div style="display: table"><div style="display: table-cell">` + items + `</div></div>` + end},
		{"kolumny", `<div style="columns: 2">` + items + `</div>` + end},
		{"position: relative", `<div style="position: relative">` + items + `</div>` + end},
		{"transform", `<div style="transform: translate(0, 0)">` + items + `</div>` + end},
		{"fieldset", `<fieldset>` + items + `</fieldset>` + end},
		{"lista", `<ul>` + listItems + `</ul>` + end},
		{"flex w kolumnie", `<div style="display: flex; flex-direction: column">` + items + `</div>` + end},
		{"flex w wierszu z zawijaniem", `<div style="display: flex; flex-wrap: wrap">` + strings.ReplaceAll(items, `class="i"`, `class="i" style="width: 100%"`) + `</div>` + end},
		{"blok w elemencie flex", `<div style="display: flex"><div>` + items + `</div></div>` + end},
		{"zagnieżdżony flex", `<div style="display: flex; flex-direction: column"><div style="display: flex; flex-direction: column">` + items + `</div></div>` + end},
		{"grid", `<div style="display: grid">` + items + `</div>` + end},
		{"blok w elemencie grid", `<div style="display: grid"><div>` + items + `</div></div>` + end},
		{"inline-block", `<div style="display: inline-block; width: 80%">` + items + `</div>` + end},
		{"inline-flex", `<div style="display: inline-flex; flex-direction: column">` + items + `</div>` + end},
		{"html i body o wysokości 100%", `<style>html, body { height: 100% }</style>` + items + end},
		{"body flex z min-height", `<style>body { display: flex; flex-direction: column; min-height: 100vh }</style><div style="flex: 1">` + items + `</div>` + end},
	}
	for _, c := range cases {
		visible := visibleWords(t, renderString(t, style+c.html).PDF, 150)
		var lost []string
		for i := 1; i <= 8; i++ {
			if word := fmt.Sprintf("e%d", i); !visible[word] {
				lost = append(lost, word)
			}
		}
		if !visible["koniec"] {
			lost = append(lost, "koniec")
		}
		if len(lost) != 0 {
			t.Errorf("%s: poza stroną albo brak: %s", c.name, strings.Join(lost, " "))
		}
	}
}

// Treść, której silnik nie umie przenieść na następną stronę, nie znika po cichu: wynik zawiera ostrzeżenie
// z numerem strony. Tu obraz z break-inside: avoid, wyższy niż strona, zostaje w całości i wystaje.
func TestWarningForContentBelowPage(t *testing.T) {
	data, err := os.ReadFile("testdata/wpt/css/css-page/support/cat.png")
	if err != nil {
		t.Fatal(err)
	}
	render := func(html string) Result {
		result, err := Render(t.Context(), []byte("<style>@page { size: 300px 200px; margin: 20px } body { margin: 0 }</style>"+html), Options{
			Fonts:    testFonts(t),
			BaseURL:  "file:///doc/index.html",
			Resolver: func(string) (Resource, error) { return Resource{Data: data, MediaType: "image/png"}, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	warned := func(result Result) bool {
		for _, warning := range result.Warnings {
			if strings.Contains(warning.Message, "leży poniżej dolnej krawędzi strony") {
				return true
			}
		}
		return false
	}
	// pudełko pozycjonowane w całości poniżej strony nie trafia na następną (znane ograniczenie)
	if result := render(`<p>tekst</p><div style="position: absolute; top: 500px">niewidoczne</div>`); !warned(result) {
		t.Errorf("brak ostrzeżenia o treści poza stroną; ostrzeżenia: %v", result.Warnings)
	}
	if result := render(`<img src="cat.png" style="display: block; width: 100px; height: 400px">`); warned(result) {
		t.Errorf("ostrzeżenie mimo pokrojenia obrazu między strony: %v", result.Warnings)
	}
}
