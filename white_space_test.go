package gopdfgo

import (
	"context"
	"os"
	"testing"
)

// Testy przetwarzania białych znaków i łamania wierszy z CSS Text 3 § 4 i § 5: wiszące odstępy,
// tabulatory przy pre-wrap i break-spaces, znak CR, spacja zerowej szerokości przy złamaniu
// segmentu, szerokości wewnętrzne. Każdy przypadek porównuje stronę z wzorcem ułożonym bez badanego
// zjawiska (font Ahem, div ma font 20px/20px).

func TestWhiteSpaceTabs(t *testing.T) {
	same := ahemComparer(t)
	// break-spaces: po każdym tabulatorze wolno złamać wiersz, tabulator nie wisi (break-spaces-tab-003)
	same("break-spaces", `<div style="white-space:break-spaces;width:20px">X&#9;&#9;&#9;X</div>`,
		`<div>X<br><br><br>X</div>`)
	// pre-wrap: ciąg spacji i tabulatorów jest jednym odstępem, wisi w całości (pre-wrap-tab-004);
	// tab-size 2, żeby wiszące odstępy nie wychodziły poza stronę (strona zmniejszyłaby treść)
	same("pre-wrap", `<div style="white-space:pre-wrap;width:40px;tab-size:2">XX &#9; &#9;XX</div>`,
		`<div>XX<br>XX</div>`)
}

func TestWhiteSpaceCarriageReturn(t *testing.T) {
	same := ahemComparer(t)
	// CR z odwołania znakowego jest spacją (control-chars-00D)
	same("pre", `<div style="white-space:pre">X&#13;&#13;X</div>`, `<div style="white-space:pre">X  X</div>`)
	same("normal", `<div>X&#13;&#13;X</div>`, `<div>X X</div>`)
}

func TestWhiteSpaceHanging(t *testing.T) {
	same := ahemComparer(t)
	// ciąg odstępów innych niż spacja wisi w całości, także rozłożony na kilka pudełek
	// (trailing-other-space-separators-002)
	same("other separators", `<div style="white-space:pre-wrap;width:60px">X X<span>&#x2000;</span>&#x2001; &#x2002;X X</div>`,
		`<div>X X<br>X X</div>`)
	// odstępy przed wymuszonym złamaniem nie zmuszają do złamania wcześniej (pre-wrap-011,
	// pre-wrap-leading-spaces-014)
	same("before newline", "<div style=\"white-space:pre-wrap;width:100px\">XXX X \nX</div>", `<div>XXX X<br>X</div>`)
	// zwijalna spacja przed pustym pudełkiem z obramowaniem znika na końcu wiersza
	// (line-edge-white-space-collapse-001)
	same("empty inline", `<div><span>X <span style="border-left:20px solid green"></span></span></div>`,
		`<div>X<span style="border-left:20px solid green"></span></div>`)
}

func TestWhiteSpaceIntrinsicSizes(t *testing.T) {
	same := ahemComparer(t)
	// min-content przy pre-wrap: spacje na końcu tekstu się nie liczą (white-space-intrinsic-size-003)
	same("min-content", `<div style="width:0"><div style="float:left;white-space:pre-wrap;background:green">X   </div></div>`,
		`<div style="width:0"><div style="float:left;background:green">X</div></div>`)
	// max-content: zwijalna spacja przed wymuszonym złamaniem w innym pudełku (white-space-intrinsic-size-024)
	same("max-content", "<div style=\"float:left;background:green\">X <span style=\"white-space:pre-line\">\nX</span></div>",
		`<div style="float:left;background:green">X<br>X</div>`)
	// odstęp za ostatnim znakiem wiersza przed wymuszonym złamaniem (white-space-letter-spacing-001)
	same("letter-spacing", "<div style=\"float:left;white-space:pre;font:16px monospace;letter-spacing:8px;background:green\">Two lines\nof text</div>",
		`<div style="float:left;font:16px monospace;letter-spacing:8px;background:green">Two lines<br>of text</div>`)
	// pre: spacje na końcu wiersza nie liczą się obok elementu pływającego (pre-float-001)
	same("pre beside float", `<div style="float:left;width:60px;height:40px;background:green"></div><div style="white-space:pre;width:200px">XXXXXXX  <br>X</div>`,
		`<div style="float:left;width:60px;height:40px;background:green"></div><div style="white-space:pre;width:200px">XXXXXXX<br>X</div>`)
}

func TestWhiteSpaceSegmentBreaks(t *testing.T) {
	same := ahemComparer(t)
	// złamanie segmentu po spacji zerowej szerokości znika, także przez granicę pudełek
	// (seg-break-transformation-018)
	same("zwsp", "<div>XX<span>&#x200b;</span>\nXX</div>", `<div>XXXX</div>`)
	// zachowany znak nowego wiersza łamie wiersz także przy zerowym rozmiarze fontu
	// (white-space-zero-fontsize-001)
	same("zero font size", "<div style=\"white-space:pre;font-size:0\"><span style=\"font-size:20px\">X</span>\n<span style=\"font-size:20px\">X</span></div>",
		`<div>X<br>X</div>`)
	// tekst rodzica nie dokleja się do tekstu dziecka z display: contents (white-space-applies-to-text-001)
	same("display contents", "<div><span style=\"display:contents;white-space:pre\">X</span>\n  X</div>", `<div>X X</div>`)
}

func TestWhiteSpaceRounding(t *testing.T) {
	mono, err := os.ReadFile("testdata/fonts/NotoSansMono-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	fonts := append([]Font{{Name: "NotoSansMono-Regular.ttf", Data: mono}}, testFonts(t)...)
	const head = `<style>@page { size: 300px 200px; margin: 0 } body { margin: 0 }</style>`
	dir := t.TempDir()
	page := func(name, body string) []byte {
		t.Helper()
		result, err := Render(context.Background(), []byte(head+body), Options{Fonts: fonts})
		if err != nil {
			t.Fatal(err)
		}
		pages := rasterize(t, result.PDF, dir, name)
		if len(pages) != 1 {
			t.Fatalf("%s: %d stron", name, len(pages))
		}
		return pages[0]
	}
	same := func(name, test, reference string) {
		t.Helper()
		if n := differentPixels(page(name, test), page(name+"-ref", reference)); n != 0 {
			t.Errorf("%s: %d pikseli różni się od wzorca", name, n)
		}
	}
	// szerokość znaku Noto Sans Mono 32 px (19,2 px) nie jest wielokrotnością 1/64 piksela: suma
	// zaokrąglonych szerokości glifów przekracza szerokość wiersza, choć dokładna się mieści
	// (white-space-pre-034)
	const font = "font:32px Noto Sans Mono;"
	same("nbsp", `<div style="`+font+`width:12ch">Lorem ips&nbsp;&nbsp;&nbsp;<br>X</div>`,
		`<div style="`+font+`white-space:pre">Lorem ips   <br>X</div>`)
	same("break-spaces", `<div style="`+font+`width:12ch;white-space:pre">Lorem <span style="white-space:break-spaces">ips      um</span></div>`,
		`<div style="`+font+`white-space:pre">Lorem ips   <br>   um</div>`)
}
