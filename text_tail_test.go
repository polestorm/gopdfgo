package gopdfgo

import "testing"

// TestTextTail sprawdza poprawki z końcówki testów CSS Text: odstęp między literami na końcu wiersza
// i przy znakach Cf, tabulatorze i wymuszonym złamaniu (CSS Text 3 § 7.2), white-space-trim i
// white-space-collapse (CSS Text 4 § 4.1, § 4.2), wiszące odstępy Zs przy wyrównaniu (CSS Text 3
// § 4.1.3), pozycję statyczną liniowego elementu absolutnego za text-indent (CSS 2 § 10.3.7) i
// dzielenie wyrazów rozciętych granicami elementów (CSS Text 3 § 5.4). Font Ahem, 20 px.
func TestTextTail(t *testing.T) {
	same := ahemComparer(t)
	box := func(left, top, width int) string {
		return `<div style="position:absolute;background:green;height:20px;left:` + itoa(left) + `px;top:` + itoa(top) + `px;width:` + itoa(width) + `px"></div>`
	}

	// letter-spacing: na końcu wiersza odstępu nie ma, więc „123” mieści się w 5em, a przy
	// wyrównaniu do prawej ostatnia litera dochodzi do krawędzi
	same("letter-spacing-line-end", `<div style="width:100px;letter-spacing:20px">123</div>
		<div style="width:100px;letter-spacing:20px;text-align:right">12</div>`,
		box(0, 0, 20)+box(40, 0, 20)+box(80, 0, 20)+box(40, 20, 20)+box(80, 20, 20))
	// przy zawijaniu: wiersz „1 2 3” i ostatni wiersz „4 5” nie liczą odstępu za ostatnią literą
	same("letter-spacing-wrap", `<div style="width:100px;letter-spacing:20px">123 45</div>`,
		box(0, 0, 20)+box(40, 0, 20)+box(80, 0, 20)+box(0, 20, 20)+box(40, 20, 20))
	// wymuszone złamanie przy pre-wrap: „4 5” przed znakiem nowego wiersza mieści się w 5em
	same("letter-spacing-forced-break", `<div style="width:100px;letter-spacing:20px;white-space:pre-wrap">4 5&#10;6</div>`,
		box(0, 0, 20)+box(80, 0, 20)+box(0, 20, 20))
	// spacja zerowej szerokości na początku nie daje odstępu przed pierwszą literą
	same("letter-spacing-cf", `<div style="letter-spacing:20px">&#x200b;a&#x200b;b&#x2060;</div>`,
		box(0, 0, 20)+box(40, 0, 20))
	// znak za tabulatorem stoi dokładnie na pozycji tabulacji (tab-size: 3 → 3 × (spacja + odstęp))
	same("letter-spacing-tab", `<div style="white-space:pre;tab-size:3;letter-spacing:20px">1&#9;1</div>`,
		box(0, 0, 20)+box(120, 0, 20))

	// ciąg elementów atomowych liczy się jak jeden znak: odstęp przed nim i za nim, nie między nimi
	same("letter-spacing-atomic-run", `<div style="letter-spacing:20px">a<span style="display:inline-block;vertical-align:top;width:20px;height:20px;background:green"></span><span
		style="display:inline-block;vertical-align:top;width:20px;height:20px;background:green"></span>b</div>`,
		box(0, 0, 20)+box(40, 0, 40)+box(100, 0, 20))

	// (font-weight: normal w <b>: Ahem nie ma kroju pogrubionego, a syntetyczne pogrubienie zmieniłoby
	// rysunek względem wzorca bez <b>)
	// white-space-trim: discard-before i discard-after usuwają zwijane białe znaki poza elementem,
	// także przez granice przodka; discard-inner usuwa je wewnątrz elementu
	same("white-space-trim-before-after", `<div>a <b style="font-weight:normal;white-space-trim:discard-before">b</b>c</div>
		<div><span><b style="white-space-trim:discard-after">a</b></span> b</div>
		<div>a<span style="white-space-trim:discard-inner"> b </span>c</div>`,
		`<div>abc</div><div>ab</div><div>abc</div>`)
	// zachowane białe znaki (pre) zostają
	same("white-space-trim-pre", `<div style="white-space:pre">a <b style="font-weight:normal;white-space-trim:discard-before">b</b></div>`,
		`<div style="white-space:pre">a b</div>`)
	// white-space jako skrót ustawia white-space-trim na none
	same("white-space-trim-shorthand", `<div>a <b style="font-weight:normal;white-space-trim:discard-before;white-space:normal">b</b></div>`,
		`<div>a b</div>`)
	// white-space-collapse: break-spaces zachowuje spacje, więc white-space-trim ich nie usuwa
	same("white-space-collapse", `<div style="white-space-collapse:break-spaces">a <b style="font-weight:normal;white-space-trim:discard-before">b</b></div>
		<div style="white-space-collapse:preserve-breaks">a   b&#10;c</div>`,
		`<div>a b</div><div>a b<br>c</div>`)

	// odstęp ideograficzny na końcu wiersza wisi także przy white-space: normal
	same("ideographic-space-hangs", `<div style="width:40px;text-align:right">x&#x3000;x</div>`,
		box(20, 0, 20)+box(20, 20, 20))

	// zachowane spacje (pre-wrap) przed odstępem ideograficznym przy white-space: normal wiszą
	// bezwarunkowo razem z nim
	same("hanging-sequence", `<div style="width:60px;text-align:right"><span style="white-space:pre-wrap">X  </span>&#x3000;<br>X</div>`,
		box(40, 0, 20)+box(40, 20, 20))

	// liniowy element absolutny przed pierwszym wierszem stoi za wcięciem
	same("abspos-static-text-indent", `<div style="text-indent:20px"><span style="position:absolute;width:20px;height:20px;background:green"></span>xx</div>`,
		box(20, 0, 20)+`<div style="margin-left:20px">xx</div>`)
}

// TestTextTailHyphenation: wyraz rozcięty granicą elementu (pustego albo z treścią) albo elementem
// spoza przepływu dzieli się tak samo jak w jednym pudełku tekstu (CSS Text 3 § 5.4).
func TestTextTailHyphenation(t *testing.T) {
	same := ahemComparer(t)
	ref := `<div lang=en style="width:120px">high&#x2010;<br>way</div>`
	for name, body := range map[string]string{
		"plain":        `highway`,
		"empty-span":   `high<span></span>way`,
		"span":         `<span>hi</span>ghway`,
		"out-of-flow":  `h<span style="position:absolute"></span>ighway`,
		"span-at-edge": `<span>highwa</span>y`,
	} {
		same("hyphens-"+name, `<div lang=en style="width:120px;hyphens:auto">`+body+`</div>`, ref)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	s := ""
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		s = string(rune('0'+v%10)) + s
		v /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
