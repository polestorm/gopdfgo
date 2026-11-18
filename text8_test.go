package gopdfgo

import "testing"

// TestText8 sprawdza poprawki ósmej paczki tekstu: text-autospace (CSS Text 4 § 6.4), łącznik
// nierozdzielający U+2011 w foncie z U+2010 (jak HarfBuzz), letter-spacing przy klastrze kilku znaków Cf
// (CSS Text 3 § 7.2) i syntezę kapitalików (CSS Fonts 4 § 7.4). Font Ahem, 20 px.
func TestText8(t *testing.T) {
	same := ahemComparer(t)

	// ideograph-alpha i ideograph-numeric: 1/8 ic (Ahem nie ma znaku 水, więc ic = 1em) między
	// literą lub cyfrą a ideogramem, w obie strony
	same("autospace-alpha", `<div style="text-autospace:ideograph-alpha">A水水</div>`,
		`<div style="text-autospace:no-autospace">A<span style="margin-left:2.5px">水水</span></div>`)
	same("autospace-numeric", `<div style="text-autospace:ideograph-numeric">水4</div>`,
		`<div style="text-autospace:no-autospace">水<span style="margin-left:2.5px">4</span></div>`)
	// wartość początkowa normal obejmuje oba rodzaje; ideograph-numeric nie dodaje odstępu przy literze
	same("autospace-normal", `<div>A水</div><div style="text-autospace:ideograph-numeric">A水</div>`,
		`<div style="text-autospace:no-autospace">A<span style="margin-left:2.5px">水</span></div><div style="text-autospace:no-autospace">A水</div>`)
	// tekst preformatowany bez odstępów (arkusz domyślny, jak w Chromium)
	same("autospace-pre", `<pre style="font:20px/20px Ahem;margin:0">A水</pre><div><code style="font:20px Ahem">A水</code></div>`,
		`<pre style="font:20px/20px Ahem;margin:0;text-autospace:no-autospace">A水</pre><div style="text-autospace:no-autospace">A水</div>`)
	// na granicy, na której łamie się wiersz, odstępu nie ma
	same("autospace-line-end", `<div style="width:40px;text-align:right">XX水</div>`,
		`<div style="width:40px;text-align:right;text-autospace:no-autospace">XX<br>水</div>`)

	// Ahem nie ma U+2011, ale ma U+2010: łącznik nierozdzielający jest jego glifem, nie glifem
	// fontu zastępczego
	same("non-breaking-hyphen", `<div>X&#x2011;X</div>`, `<div>X&#x2010;X</div>`)

	// ZWNJ i ZWJ tworzą jeden klaster (dwa glify); odstęp między literami pomija go w całości
	same("letter-spacing-cf-cluster", `<div style="letter-spacing:20px">x&#x200C;&#x200D;x</div>`,
		`<div style="letter-spacing:20px">xx</div>`)

	// synteza kapitalików: Ahem nie ma cechy smcp, więc małe litery są wielkimi w rozmiarze 0,7;
	// ß rozwija się w SS z odstępem między literami także wewnątrz rozwinięcia
	same("small-caps-sharp-s", `<div style="font-variant:small-caps;letter-spacing:10px">Straße</div>`,
		`<div style="font-variant:small-caps;letter-spacing:10px">Strasse</div>`)
	same("small-caps-scale", `<div style="font-variant:small-caps">Ab</div>`,
		`<div>A<span style="font-size:14px">B</span></div>`)
	same("all-small-caps", `<div style="font-variant:all-small-caps">Ab</div>`,
		`<div><span style="font-size:14px">AB</span></div>`)
	// font-synthesis-small-caps: none wyłącza syntezę
	same("small-caps-no-synthesis", `<div style="font-variant:small-caps;font-synthesis-small-caps:none">Ab</div>`,
		`<div>Ab</div>`)
}
