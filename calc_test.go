package gopdfgo

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// calcCases: deklaracja z calc() i równoważna deklaracja bez niego. Oba dokumenty muszą dać
// identyczne bajty, co przy deterministycznym zapisie jest najprostszym sprawdzeniem wyniku.
// Kontener ma 400px szerokości i font 20px, więc 1em = 20px, a 50% szerokości = 200px.
var calcCases = []struct{ name, withCalc, plain string }{
	{"width px", "width: calc(100px + 50px)", "width: 150px"},
	{"width mixed units", "width: calc(1in + 4px)", "width: 100px"},
	{"width em", "width: calc(2em + 10px)", "width: 50px"},
	{"width percent minus px", "width: calc(50% - 20px)", "width: 180px"},
	{"width percent plus em", "width: calc(25% + 1em)", "width: 120px"},
	{"width product", "width: calc(3 * 40px)", "width: 120px"},
	{"width division", "width: calc(300px / 4)", "width: 75px"},
	{"width nested parentheses", "width: calc((10px + 20px) * 2 - 10px)", "width: 50px"},
	{"width nested calc", "width: calc(calc(10px + 20px) + 30px)", "width: 60px"},
	{"width precedence", "width: calc(10px + 20px * 3)", "width: 70px"},
	{"width min", "width: min(100px, 5em)", "width: 100px"},
	{"width max", "width: max(100px, 6em)", "width: 120px"},
	{"width clamp low", "width: clamp(60px, 1em, 200px)", "width: 60px"},
	{"width clamp high", "width: clamp(60px, 300px, 200px)", "width: 200px"},
	{"width min inside calc", "width: calc(min(100px, 300px) + 10px)", "width: 110px"},
	{"min z procentem", "width: min(50%, 100px)", "width: 100px"},
	{"clamp z procentem", "width: clamp(10px, 50%, 120px)", "width: 120px"},
	{"max z procentem", "max-width: max(10%, 80px); width: 500px", "width: 80px"},
	{"height", "height: calc(10px + 1em)", "height: 30px"},
	{"min-width", "min-width: calc(50% + 10px); width: 10px", "min-width: 210px; width: 10px"},
	{"max-width", "max-width: calc(50% - 10px)", "max-width: 190px"},
	{"margin-left", "margin-left: calc(10px + 5px)", "margin-left: 15px"},
	{"margin shorthand", "margin: calc(2px * 3) calc(1em / 2)", "margin: 6px 10px"},
	{"margin-left percent", "margin-left: calc(10% + 5px)", "margin-left: 45px"},
	{"padding", "padding: calc(1px + 2px)", "padding: 3px"},
	{"padding-left percent", "padding-left: calc(5% - 5px)", "padding-left: 15px"},
	{"border-width", "border-width: calc(1px + 2px)", "border-width: 3px"},
	{"border-radius", "border-radius: calc(2px * 4)", "border-radius: 8px"},
	{"font-size", "font-size: calc(10px + 6px)", "font-size: 16px"},
	{"line-height", "line-height: calc(20px + 10px)", "line-height: 30px"},
	{"letter-spacing", "letter-spacing: calc(1px + 1px)", "letter-spacing: 2px"},
	{"word-spacing", "word-spacing: calc(2px * 2)", "word-spacing: 4px"},
	{"text-indent", "text-indent: calc(1em + 5px)", "text-indent: 25px"},
	{"left", "position: relative; left: calc(10px + 1em)", "position: relative; left: 30px"},
	{"top percent", "position: absolute; top: calc(10px + 5px); left: calc(50% - 10px)", "position: absolute; top: 15px; left: 190px"},
	{"flex-basis", "display: flex; flex: 0 0 calc(40px + 10px)", "display: flex; flex: 0 0 50px"},
	{"border-spacing", "display: table; border-collapse: separate; border-spacing: calc(1px + 1px)", "display: table; border-collapse: separate; border-spacing: 2px"},
	{"background-position", "background: linear-gradient(red, blue) no-repeat; background-size: 10px 10px; background-position: calc(5px + 5px) calc(50% - 5px)", "background: linear-gradient(red, blue) no-repeat; background-size: 10px 10px; background-position: 10px calc(50% - 5px)"},
	{"background-size", "background: linear-gradient(red, blue) no-repeat; background-size: calc(5px * 2) calc(2px + 3px)", "background: linear-gradient(red, blue) no-repeat; background-size: 10px 5px"},
	{"outline-width", "outline: solid red; outline-width: calc(1px + 1px)", "outline: solid red; outline-width: 2px"},
	// obszar strony w calcDocument to 560 x 360 px (600 x 400 minus marginesy 20)
	{"vw", "width: 50vw", "width: 280px"},
	{"vh", "height: 10vh", "height: 36px"},
	{"vmin", "width: 10vmin", "width: 36px"},
	{"vmax", "width: 10vmax", "width: 56px"},
	{"vw in calc", "width: calc(50vw - 10px)", "width: 270px"},
	{"vh margin", "margin-top: 5vh", "margin-top: 18px"},
	// właściwości logiczne (pismo poziome, od lewej)
	{"inline-size", "inline-size: 120px", "width: 120px"},
	{"block-size", "block-size: 60px", "height: 60px"},
	{"min-inline-size", "min-inline-size: 300px; width: 10px", "min-width: 300px; width: 10px"},
	{"max-block-size", "max-block-size: 10px", "max-height: 10px"},
	{"margin-inline-start", "margin-inline-start: 30px", "margin-left: 30px"},
	{"margin-block-end", "margin-block-end: 30px", "margin-bottom: 30px"},
	{"margin-block pair", "margin-block: 5px 15px", "margin-top: 5px; margin-bottom: 15px"},
	{"margin-inline single", "margin-inline: 25px", "margin-left: 25px; margin-right: 25px"},
	{"padding-inline pair", "padding-inline: 5px 15px", "padding-left: 5px; padding-right: 15px"},
	{"padding-block-start", "padding-block-start: calc(2px + 3px)", "padding-top: 5px"},
	{"border-block-start", "border-block-start: 3px solid red", "border-top: 3px solid red"},
	{"border-inline-end-width", "border-style: solid; border-inline-end-width: 4px", "border-style: solid; border-right-width: 4px"},
	{"inset-inline-start", "position: relative; inset-inline-start: 12px", "position: relative; left: 12px"},
	{"inset-block pair", "position: absolute; inset-block: 5px 10px", "position: absolute; top: 5px; bottom: 10px"},
	{"column-gap", "column-count: 2; column-gap: calc(5px + 5px)", "column-count: 2; column-gap: 10px"},
}

func calcDocument(declarations string) []byte {
	return []byte(fmt.Sprintf(`<html><head><style>
@page { size: 600px 400px; margin: 20px }
body { margin: 0; font-size: 20px; width: 400px; position: relative }
.box { background: #9cf; border: 0 solid #036; height: 40px; %s }
</style></head><body><div class="box">tekst próbny <span>w pudełku</span></div><p>akapit po pudełku</p></body></html>`, declarations))
}

func TestCalcMatchesPlainValue(t *testing.T) {
	fonts := testFonts(t)
	for _, c := range calcCases {
		t.Run(c.name, func(t *testing.T) {
			withCalc, err := Render(context.Background(), calcDocument(c.withCalc), Options{Fonts: fonts})
			if err != nil {
				t.Fatal(err)
			}
			plain, err := Render(context.Background(), calcDocument(c.plain), Options{Fonts: fonts})
			if err != nil {
				t.Fatal(err)
			}
			for _, warning := range withCalc.Warnings {
				if strings.Contains(warning.Message, "Ignored") {
					t.Errorf("deklaracja odrzucona: %s", warning.Message)
				}
			}
			if !bytes.Equal(withCalc.PDF, plain.PDF) {
				t.Errorf("%q daje inny wynik niż %q", c.withCalc, c.plain)
			}
		})
	}
}

// Wyrażenia niepoprawne muszą być odrzucone z ostrzeżeniem, a nie policzone byle jak.
func TestCalcRejectsInvalid(t *testing.T) {
	fonts := testFonts(t)
	for _, declaration := range []string{
		"width: calc(10px + 5)",           // długość + liczba
		"width: calc(10px * 5px)",         // długość * długość
		"width: calc(10px / 2px)",         // dzielenie przez długość
		"width: calc(10px / 0)",           // dzielenie przez zero
		"width: calc(10px +)",             // brak argumentu
		"width: calc()",                   // puste
		"letter-spacing: calc(10% + 1px)", // procent tam, gdzie właściwość go nie przyjmuje
	} {
		result, err := Render(context.Background(), calcDocument(declaration), Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, warning := range result.Warnings {
			found = found || strings.Contains(warning.Message, strings.SplitN(declaration, ":", 2)[0])
		}
		if !found {
			t.Errorf("%q przyjęte bez ostrzeżenia", declaration)
		}
	}
}
