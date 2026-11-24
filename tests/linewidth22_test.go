package tests

import (
	"os"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
)

// Dokładna szerokość wiersza jak w Chromium (ADR 0044; pomiary Chrome 157 z 2026-11-22): szerokość
// kawałka tekstu to suma dokładnych szerokości znaków, w trybie domyślnym w górę do 1/64 px, i tak samo
// liczona przy min-/max-content; rozmiar pisma obcinany do 0,01 px w pojedynczej precyzji; szerokość
// obszaru strony powiększonego przy pomniejszaniu treści w dół do całych pikseli.
func TestPreciseLineWidth(t *testing.T) {
	fonts := testFonts(t)
	for _, name := range []string{"tinos/Tinos-Regular.ttf", "tinos/Tinos-Bold.ttf", "arimo/Arimo-Regular.ttf", "NotoSansMono-Regular.ttf"} {
		data, err := os.ReadFile("../testdata/fonts/" + name)
		if err != nil {
			t.Fatal(err)
		}
		fonts = append(fonts, htmlpdf.Font{Name: name, Data: data})
	}
	tops := func(source string, exact bool) map[string]float64 {
		t.Helper()
		result, err := htmlpdf.Render(t.Context(), []byte(`<!DOCTYPE html><meta charset="utf-8">`+source),
			htmlpdf.Options{Fonts: fonts, ExactLengths: exact})
		if err != nil {
			t.Fatal(err)
		}
		return wordTops(t, result.PDF)
	}
	sameLine := func(name, source, first, second string, exact, want bool) {
		t.Helper()
		got := tops(source, exact)
		a, okA := got[first]
		b, okB := got[second]
		if !okA || !okB {
			t.Fatalf("%s: brak słów %q, %q w %v", name, first, second, got)
		}
		if same := a == b; same != want {
			t.Errorf("%s (ExactLengths %v): %q i %q w jednym wierszu: %v, oczekiwano %v (%.2f, %.2f pt)",
				name, exact, first, second, same, want, a, b)
		}
	}

	// Tinos 8 px: wiersz kończący się na „układzie” ma dokładnie 627,1016 px. Szerokości znaków
	// zaokrąglane osobno do 1/64 px dawały 626,80 px i wiersz mieścił się w 627 px (bench/realdocs
	// office-quote); w Chromium się nie mieści.
	paragraph := func(width string) string {
		return `<p style="margin:0;font:8px Tinos;width:` + width + `">Powyższe etapy (modelowania) są najważniejsze ` +
			`z punktu późniejszej pracy z bazą, gdyż od ich jakości zależy niezawodność i możliwość dalszej ` +
			`rozbudowy systemu ale ich położenie w układzie projektu uniezależnia je całkowicie.</p>`
	}
	for _, exact := range []bool{false, true} {
		sameLine("Tinos 8px w 627px", paragraph("627px"), "Powyższe", "układzie", exact, false)
		sameLine("Tinos 8px w 627.11px", paragraph("627.11px"), "Powyższe", "układzie", exact, true)
	}

	// Pudełko o szerokości max-content (i element pływający, i komórka tabeli) nie łamie własnego
	// tekstu: szerokość min-/max-content liczy się tak samo jak szerokość wiersza.
	for _, box := range []string{`div style="width:max-content`, `div style="float:left`, `span style="display:inline-block`} {
		tag := strings.Fields(box)[0]
		source := `<` + box + `;font:10.7px Tinos">aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa <b>bbbbbbbbbbbbbbbb</b> ` +
			`<span style="font:9.3px Arimo">cccccccccccccccccc</span> ddddddddd</` + tag + `>`
		for _, exact := range []bool{false, true} {
			sameLine(box, source, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "ddddddddd", exact, true)
		}
	}

	// Rozmiar .88em przy 14 px to 12,32 px (nie 12,31 px): 100 znaków Noto Sans Mono ma 739,2 px.
	mono := func(width string) string {
		return `<div style="font:14px 'Noto Sans Mono';width:` + width + `"><span style="font-size:.88em">` +
			strings.Repeat("x", 50) + ` ` + strings.Repeat("y", 49) + `</span></div>`
	}
	sameLine("Noto Sans Mono .88em w 739.17px", mono("739.17px"), strings.Repeat("x", 50), strings.Repeat("y", 49), false, false)
	sameLine("Noto Sans Mono .88em w 739.21px", mono("739.21px"), strings.Repeat("x", 50), strings.Repeat("y", 49), false, true)

	// Pomniejszanie (ADR 0014): przy treści szerokiej na 450,9 px dokument jest układany na szerokości
	// 450 px, więc 450 px i 0,2 px w jednym wierszu się nie mieszczą (Chromium); dokładnie: 450,9 px.
	shrink := `<style>@page { size: A5; margin: 2cm } body { margin: 0; font: 10px Arimo }
		.ib { display: inline-block; height: 12px }</style><div style="width:450.9px;height:2px"></div>
		<div><span class=ib style="width:450px">A</span><span class=ib style="width:0.2px">B</span></div>`
	sameLine("pomniejszanie", shrink, "A", "B", false, false)
	sameLine("pomniejszanie", shrink, "A", "B", true, true)
}
