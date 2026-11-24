package tests

import (
	"bytes"
	"context"
	"github.com/polestorm/gopdfgo/htmlpdf"
	"strings"
	"testing"
)

// Funkcje skokowe round(), mod() i rem() (CSS Values 4 § 10.3, § 10.4): każda deklaracja musi dać
// te same bajty co równoważna wartość wpisana wprost. calcDocument: kontener 400px, font 20px.
var steppedCases = []struct{ name, withFunction, plain string }{
	{"round nearest", "width: round(33.3px, 10px)", "width: 30px"},
	{"round nearest tie up", "width: round(35px, 10px)", "width: 40px"},
	{"round up", "width: round(up, 31px, 10px)", "width: 40px"},
	{"round down", "width: round(down, 39px, 10px)", "width: 30px"},
	{"round to-zero", "margin-left: round(to-zero, -39px, 10px)", "margin-left: -30px"},
	{"round nearest negative", "margin-left: round(-36px, 10px)", "margin-left: -40px"},
	{"round negative step", "width: round(33px, -10px)", "width: 30px"},
	{"round exact multiple", "width: round(up, 40px, 10px)", "width: 40px"},
	{"round em step", "width: round(up, 41px, 1em)", "width: 60px"},
	{"round in calc", "width: calc(round(14px, 10px) * 3)", "width: 30px"},
	{"round number default step", "width: calc(round(2.6) * 10px)", "width: 30px"},
	{"round step zero is NaN, NaN is 0", "width: round(10px, 0px)", "width: 0px"},
	{"round infinite step", "width: calc(100px + round(3px, infinity * 1px))", "width: 100px"},
	{"mod negative", "margin-left: mod(-7px, 3px)", "margin-left: 2px"},
	{"mod negative step", "margin-left: mod(7px, -3px)", "margin-left: -2px"},
	{"rem negative", "margin-left: rem(-7px, 3px)", "margin-left: -1px"},
	{"rem positive", "width: rem(107px, 10px)", "width: 7px"},
	{"round percent", "width: round(up, 33%, 50px)", "width: 150px"},
	{"mod percent", "width: mod(50%, 60px)", "width: 20px"},
	{"rem percent step", "width: rem(150px, 25%)", "width: 50px"},
	{"round inside min", "width: min(round(up, 101px, 50px), 500px)", "width: 150px"},
}

func TestSteppedValueFunctions(t *testing.T) {
	fonts := testFonts(t)
	for _, c := range steppedCases {
		t.Run(c.name, func(t *testing.T) {
			withFunction, err := htmlpdf.Render(context.Background(), calcDocument(c.withFunction), htmlpdf.Options{Fonts: fonts})
			if err != nil {
				t.Fatal(err)
			}
			plain, err := htmlpdf.Render(context.Background(), calcDocument(c.plain), htmlpdf.Options{Fonts: fonts})
			if err != nil {
				t.Fatal(err)
			}
			for _, warning := range withFunction.Warnings {
				if strings.Contains(warning.Message, "Ignored") {
					t.Errorf("deklaracja odrzucona: %s", warning.Message)
				}
			}
			if !bytes.Equal(withFunction.PDF, plain.PDF) {
				t.Errorf("%q daje inny wynik niż %q", c.withFunction, c.plain)
			}
		})
	}
}

// Niezgodne typy i zła liczba argumentów muszą być odrzucone.
func TestSteppedValueFunctionsRejectInvalid(t *testing.T) {
	fonts := testFonts(t)
	for _, declaration := range []string{
		"width: round(10px)",            // brak kroku dla długości
		"width: round(10px, 2)",         // długość i liczba
		"width: mod(10px)",              // jeden argument
		"width: rem(10px, 2px, 3px)",    // trzy argumenty
		"width: round(left, 10px, 2px)", // nieznana strategia
		"width: mod(10px, 5%, 1px)",     // trzy argumenty z procentem
		"width: mod(10%, 2)",            // procent i liczba
	} {
		result, err := htmlpdf.Render(context.Background(), calcDocument(declaration), htmlpdf.Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, warning := range result.Warnings {
			found = found || strings.Contains(warning.Message, "width")
		}
		if !found {
			t.Errorf("%q przyjęte bez ostrzeżenia", declaration)
		}
	}
}

// Właściwości translate, rotate i scale (CSS Transforms 2 § 5.1) muszą malować to samo co równoważna
// lista funkcji transform, a złożone razem iść w kolejności translate, rotate, scale, transform (§ 6).
func TestIndividualTransformProperties(t *testing.T) {
	same := fontComparer(t, testFonts(t), `div.b { position: absolute; left: 80px; top: 60px; width: 60px; height: 30px;
		background: #36c; border-top: 6px solid #c33; transform-origin: 20% 30% }`)
	box := func(style string) string { return `<div class="b" style="` + style + `"></div>` }
	for _, c := range []struct{ name, individual, transform string }{
		{"translate", "translate: 20px 10px", "transform: translate(20px, 10px)"},
		{"translate one value", "translate: 25px", "transform: translate(25px)"},
		{"translate percent", "translate: 50% -50%", "transform: translate(50%, -50%)"},
		{"translate z ignored", "translate: 20px 10px 30px", "transform: translate(20px, 10px)"},
		{"rotate", "rotate: 30deg", "transform: rotate(30deg)"},
		{"rotate z axis", "rotate: z 30deg", "transform: rotate(30deg)"},
		{"rotate negative z vector", "rotate: 0 0 -2 30deg", "transform: rotate(-30deg)"},
		{"rotate angle first", "rotate: 30deg z", "transform: rotate(30deg)"},
		{"rotate x axis flattened", "rotate: x 60deg", "transform: scale(1, 0.5)"},
		{"rotate y axis flattened", "rotate: y 60deg", "transform: scale(0.5, 1)"},
		{"scale", "scale: 2", "transform: scale(2)"},
		{"scale two values", "scale: 1.5 50%", "transform: scale(1.5, 0.5)"},
		{"scale z zero hides", "scale: 1 1 0", "visibility: hidden"},
		{"scale calc", "scale: calc(1 + 0.5)", "transform: scale(1.5)"},
		{"order", "transform: skewX(10deg); scale: 2 1; rotate: 30deg; translate: 10px 5px",
			"transform: translate(10px, 5px) rotate(30deg) scale(2, 1) skewX(10deg)"},
		{"none", "translate: none; rotate: none; scale: none", ""},
	} {
		same(c.name, box(c.individual), box(c.transform))
	}
}

// translate, rotate albo scale inne niż none tworzą kontekst nakładania i blok zawierający dla
// potomków fixed, tak jak transform (CSS Transforms 2 § 5.1).
func TestIndividualTransformSideEffects(t *testing.T) {
	same := fontComparer(t, testFonts(t), `.p { margin: 40px; width: 100px; height: 60px; background: #9c9 }
		.c { width: 50px; height: 30px; background: #c39 }`)
	for _, property := range []string{"translate: 0px", "rotate: 0deg", "scale: 1"} {
		same("fixed "+property,
			`<div class="p" style="`+property+`"><div class="c" style="position: fixed; left: 10px; top: 10px"></div></div>`,
			`<div class="p" style="transform: translate(0)"><div class="c" style="position: fixed; left: 10px; top: 10px"></div></div>`)
		same("stacking "+property,
			`<div class="p" style="`+property+`"><div class="c" style="position: relative; z-index: -1"></div></div>`,
			`<div class="p" style="transform: translate(0)"><div class="c" style="position: relative; z-index: -1"></div></div>`)
	}
}
