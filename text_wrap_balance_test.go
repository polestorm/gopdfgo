package gopdfgo

import (
	"strings"
	"testing"
)

// text-wrap-style: balance (CSS Text 4 § 5.2) i wartości stylu w skrócie text-wrap (§ 5.4).
// Ahem 20px: znak ma 20px, więc blok o szerokości 200px mieści 10 znaków.
func TestTextWrapBalance(t *testing.T) {
	same := ahemComparer(t)
	const text = `AAA BB CC DDD`
	greedy := `<div style="width:200px">` + text + `</div>`
	balanced := `<div style="width:200px">AAA BB<br>CC DDD</div>`

	same("shorthand", `<div style="width:200px;text-wrap:balance">`+text+`</div>`, balanced)
	same("longhand", `<div style="width:200px;text-wrap-style:balance">`+text+`</div>`, balanced)
	same("inherited", `<section style="text-wrap-style:balance"><div style="width:200px">`+text+`</div></section>`, balanced)
	// tryb i styl w jednym skrócie, w dowolnej kolejności
	same("shorthand-both", `<div style="width:200px;text-wrap:balance wrap">`+text+`</div>`, balanced)
	same("shorthand-nowrap", `<div style="width:200px;text-wrap:nowrap balance">`+text+`</div>`,
		`<div style="width:200px;white-space:nowrap">`+text+`</div>`)
	// stable i pretty łamią jak auto
	same("stable", `<div style="width:200px;text-wrap:stable">`+text+`</div>`, greedy)
	same("pretty", `<div style="width:200px;text-wrap-style:pretty">`+text+`</div>`, greedy)
	// późniejszy skrót bez stylu przywraca auto
	same("reset", `<div style="width:200px;text-wrap-style:balance;text-wrap:wrap">`+text+`</div>`, greedy)
	// wyrównanie liczy się od pełnej szerokości bloku, nie od zwężonej
	same("center", `<div style="width:200px;text-wrap:balance;text-align:center">`+text+`</div>`,
		`<div style="width:200px;text-align:center">AAA BB<br>CC DDD</div>`)
	same("right", `<div style="width:200px;text-wrap:balance;text-align:right">`+text+`</div>`,
		`<div style="width:200px;text-align:right">AAA BB<br>CC DDD</div>`)
	// jeden wiersz zostaje bez zmian
	same("single", `<div style="width:200px;text-wrap:balance">AAA BB</div>`, `<div style="width:200px">AAA BB</div>`)
	// wyrównujemy najwyżej 6 wierszy (jak Chromium); przy 7 łamanie jest zwykłe
	six := strings.Repeat(text+`<br>`, 2) + text
	same("six-lines", `<div style="width:200px;text-wrap:balance">`+six+`</div>`,
		`<div style="width:200px">`+strings.Repeat(`AAA BB<br>CC DDD<br>`, 2)+`AAA BB<br>CC DDD</div>`)
	same("seven-lines", `<div style="width:200px;text-wrap:balance">`+six+`<br>X</div>`,
		`<div style="width:200px">`+six+`<br>X</div>`)
	// float w wierszu: próbne układy nie zostawiają floata na liście wykluczeń
	floated := `<span style="float:left;width:40px;height:40px;background:blue"></span>`
	same("float", `<div style="width:200px;text-wrap:balance">AAA `+floated+`BB CC DDD</div>`,
		`<div style="width:200px">AAA `+floated+`BB<br>CC DDD</div>`)
	// blok obcinany przez line-clamp nie jest wyrównywany
	same("line-clamp", `<div style="width:200px;line-clamp:1;text-wrap:balance">`+text+`</div>`,
		`<div style="width:200px;line-clamp:1">`+text+`</div>`)
}
