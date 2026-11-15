package gopdfgo

import (
	"math"
	"testing"

	"github.com/polestorm/gopdfgo/internal/webrender/css/parser"
)

// Funkcje barw z CSS Color 4 sprowadzane do sRGB. Wartości odniesienia pochodzą z przykładów normy
// (rozdz. 9 i 10): wszystkie zapisy poniżej oznaczają barwę green, czyli rgb(0, 128, 0).
func TestColorFunctionsConvertToSRGB(t *testing.T) {
	cases := []struct {
		css     string
		r, g, b float64
	}{
		{"hwb(120 0% 49.8039%)", 0, 128, 0},
		{"lab(46.2775% -47.5621 48.5837)", 0, 128, 0},
		{"lch(46.2775% 67.9892 134.3912)", 0, 128, 0},
		{"oklab(51.975% -0.1403 0.10768)", 0, 128, 0},
		{"oklch(51.975% 0.17686 142.495)", 0, 128, 0},
		{"oklch(0.51975 0.17686 142.495deg / 50%)", 0, 128, 0},
		{"lab(100% 0 0)", 255, 255, 255},
		{"oklab(0 0 0)", 0, 0, 0},
		{"hwb(0 100% 100%)", 127.5, 127.5, 127.5},
		{"rgb(0 128 0 / 0.5)", 0, 128, 0},
		{"hsl(120deg 100% 25.098%)", 0, 128, 0},
	}
	for _, c := range cases {
		tokens := parser.Tokenize([]byte(c.css), true)
		if len(tokens) != 1 {
			t.Fatalf("%s: %d tokenów", c.css, len(tokens))
		}
		color := parser.ParseColor(tokens[0])
		if color.Type != parser.ColorRGBA {
			t.Errorf("%s: nie rozpoznano barwy", c.css)
			continue
		}
		got := [3]float64{float64(color.RGBA.R) * 255, float64(color.RGBA.G) * 255, float64(color.RGBA.B) * 255}
		for i, want := range [3]float64{c.r, c.g, c.b} {
			if math.Abs(got[i]-want) > 0.6 {
				t.Errorf("%s: składowe %.2f, oczekiwane %v %v %v", c.css, got, c.r, c.g, c.b)
				break
			}
		}
	}
	for _, invalid := range []string{"lab(50%, 0, 0)", "oklch(0.5 0.1)", "lch(50% 10 20px)"} {
		tokens := parser.Tokenize([]byte(invalid), true)
		if color := parser.ParseColor(tokens[0]); color.Type == parser.ColorRGBA {
			t.Errorf("%s: przyjęto niepoprawny zapis", invalid)
		}
	}
}
