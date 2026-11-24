package tests

import (
	"bytes"
	"os"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
)

// Szerokość min-content słowo po słowie bez kształtowania reszty tekstu (ADR 0048): wynik ma być bajt w
// bajt ten sam co z kształtowaniem całej reszty przy każdym słowie. Dokumenty z korpusu z tabelami i
// tekst z przypadkami brzegowymi: kerning ze spacją (Arimo „a T”), ligatury, dzielenie wyrazów
// (automatyczne i miękkie), łamanie słów, letter-spacing i word-spacing (także ujemne), spacja
// nierozdzielająca, odstęp ideograficzny, pismo od prawej do lewej, pre-line, pre-wrap, znaki łączone.
func TestZeroWidthShortcutSameAsFullShaping(t *testing.T) {
	fonts := testFonts(t)
	for _, name := range []string{"tinos/Tinos-Regular.ttf", "arimo/Arimo-Regular.ttf", "NotoSansMono-Regular.ttf", "NotoSerif-Regular.ttf"} {
		data, err := os.ReadFile("../testdata/fonts/" + name)
		if err != nil {
			t.Fatal(err)
		}
		fonts = append(fonts, htmlpdf.Font{Name: name, Data: data})
	}
	cells := []string{
		`a T a T aT Ta. To Ty Va Wo, „Tak” — T. a`,
		`fi fl ffi office affine flow fifty`,
		`Dokumentacja przedsiębiorstwa nieruchomości zarządzającego wodomierzami`,
		`ab&shy;cd&shy;ef gh&shy;ij kl`,
		`bardzozbytdlugiewyrazbezspacjiktorytrzebazlamac a b`,
		`1&nbsp;234,56 zł 7&nbsp;890 PLN`,
		`a　b　c ideograficzny`,
		`שלום עולם abc`,
		`e&#x301; a&#x308; x&#x200D; y&#x200C; z`,
		`a/b/c http://example.com/path?x=1 2024-11-23 10-20`,
		`  spacje   na   początku i końcu  `,
	}
	styles := []string{
		``,
		`font-family: Arimo`,
		`font-family: Tinos; font-size: 11px`,
		`font-family: 'Noto Sans Mono'`,
		`font-family: 'Noto Serif'; font-variant-ligatures: common-ligatures`,
		`hyphens: auto`,
		`hyphens: manual`,
		`overflow-wrap: anywhere`,
		`word-break: break-all`,
		`word-break: keep-all`,
		`letter-spacing: 1px`,
		`letter-spacing: -3px`,
		`word-spacing: -4px`,
		`word-spacing: 20%`,
		`white-space: pre-line`,
		`white-space: pre-wrap`,
		`direction: rtl`,
		`text-transform: uppercase`,
		`font-variant: small-caps`,
		`font-size: 0.4px`,
	}
	var source bytes.Buffer
	source.WriteString(`<!DOCTYPE html><meta charset="utf-8"><html lang="pl"><style>td{border:1px solid}</style>`)
	for _, style := range styles {
		source.WriteString(`<table><tr>`)
		for _, cell := range cells {
			source.WriteString(`<td style="` + style + `">` + cell + `</td>`)
		}
		source.WriteString(`</tr></table><div style="float:left;` + style + `">` + cells[0] + ` ` + cells[2] + `</div>`)
		source.WriteString(`<div style="display:inline-block;width:min-content;` + style + `">` + cells[1] + ` <b>pogrubione słowa</b> i dalej</div>`)
	}
	documents := map[string][]byte{"cases": source.Bytes()}
	for _, name := range []string{"20-invoice", "17-tables", "02-register-table"} {
		data, err := os.ReadFile("../testdata/documents/" + name + ".html")
		if err != nil {
			t.Fatal(err)
		}
		documents[name] = data
	}
	render := func(name string, data []byte, shortcut bool) []byte {
		t.Helper()
		text.ZeroWidthShortcut = shortcut
		defer func() { text.ZeroWidthShortcut = true }()
		result, err := htmlpdf.Render(t.Context(), data, htmlpdf.Options{Fonts: fonts})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return result.PDF
	}
	for name, data := range documents {
		if !bytes.Equal(render(name, data, true), render(name, data, false)) {
			t.Errorf("%s: PDF różni się z drogą skróconą i bez niej", name)
		}
	}
}
