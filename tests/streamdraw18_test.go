package tests

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
	"github.com/polestorm/gopdfgo/internal/webrender/html/document"
)

// Rysowanie w trakcie układu (ADR 0027): wynik ma być bajt w bajt taki sam jak z rysowania po ułożeniu
// całego dokumentu, a dokumenty, w których strona po ułożeniu jeszcze się zmienia, mają wracać do
// zwykłej drogi.

// streamPair renderuje dokument raz z rysowaniem w trakcie układu i raz bez niego; zwraca oba wyniki
// i opis przeszkody z pierwszego renderowania ("" = rysowany w trakcie układu).
func streamPair(t *testing.T, html []byte, opts htmlpdf.Options) (streamed, classic []byte, fallback string) {
	t.Helper()
	fallback = "not reported"
	document.OnStream = func(reason string) { fallback = reason }
	defer func() { document.OnStream, document.StreamingDisabled = nil, false }()
	first, err := htmlpdf.Render(context.Background(), html, opts)
	if err != nil {
		t.Fatal(err)
	}
	document.OnStream, document.StreamingDisabled = nil, true
	second, err := htmlpdf.Render(context.Background(), html, opts)
	if err != nil {
		t.Fatal(err)
	}
	if first.Pages != second.Pages {
		t.Errorf("liczba stron %d, bez rysowania w trakcie układu %d", first.Pages, second.Pages)
	}
	if len(first.Warnings) != len(second.Warnings) {
		t.Errorf("ostrzeżeń %d, bez rysowania w trakcie układu %d", len(first.Warnings), len(second.Warnings))
	}
	return first.PDF, second.PDF, fallback
}

func streamOptions(t *testing.T) htmlpdf.Options {
	t.Helper()
	fonts, families, err := corpusFonts()
	if err != nil {
		t.Fatal(err)
	}
	return htmlpdf.Options{Fonts: fonts, GenericFamilies: families}
}

func pngDataURL(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "wpt", "css", "css-page", "support", "cat.png"))
	if err != nil {
		return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(
			`<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><rect width="20" height="20" fill="green"/></svg>`))
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
}

func paragraphs(n int, text string) string {
	var b strings.Builder
	for i := range n {
		b.WriteString("<p id=\"p")
		b.WriteString(strings.Repeat("x", i%3))
		b.WriteString(strconv.Itoa(i))
		b.WriteString("\">")
		b.WriteString(text)
		b.WriteString("</p>\n")
	}
	return b.String()
}

// streamCases: dokumenty z cechami, które dotykają obróbki stron po paginacji; want = "" znaczy, że dokument
// ma być rysowany w trakcie układu, inaczej podciąg spodziewanej przeszkody.
func streamCases(t *testing.T) []struct{ name, html, want string } {
	lorem := strings.Repeat("Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor. ", 6)
	img := pngDataURL(t)
	cases := []struct{ name, html, want string }{
		{"counter-pages-margin", `<style>@page { size: A6; @bottom-center { content: "Strona " counter(page) " z " counter(pages) }
			@top-right { content: "Nagłówek"; opacity: .5 } }</style>` + paragraphs(30, lorem), ""},
		{"running-string-set", `<style>@page { size: A6; @top-center { content: element(head) } @bottom-left { content: string(chapter) } }
			.head { position: running(head) } h1 { string-set: chapter content() }</style>
			<div class="head">Raport <a href="#c2">do rozdziału 2</a></div>
			<h1>Rozdział 1</h1>` + paragraphs(8, lorem) + `<h1 id="c2">Rozdział 2</h1>` + paragraphs(8, lorem), ""},
		{"footnotes", `<style>@page { size: A6 } .fn { float: footnote }</style>` +
			strings.Repeat(`<p>`+lorem+`<span class="fn">Przypis do akapitu.</span></p>`, 12), ""},
		{"table-header-repeat", `<style>@page { size: A6 } td, th { border: 1px solid }</style><table><thead><tr><th>Kol 1</th><th>Kol 2</th></tr></thead><tbody>` +
			strings.Repeat(`<tr><td>wiersz</td><td>wartość</td></tr>`, 80) + `</tbody></table>`, ""},
		{"links-bookmarks", `<style>@page { size: A6 }</style><p><a href="#end">na koniec</a> <a href="https://example.org/">zewnętrzny</a> <a href="#brak">brak</a></p>
			<h1>Pierwszy</h1>` + paragraphs(10, lorem) + `<h2>Drugi</h2>` + paragraphs(10, lorem) + `<h1 id="end">Koniec</h1>`, ""},
		{"page-orientation", `<style>@page { size: A6 } @page wide { size: A6 landscape; page-orientation: rotate-left }
			.w { page: wide }</style>` + paragraphs(4, lorem) + `<div class="w">` + paragraphs(4, lorem) + `</div>` + paragraphs(3, lorem), ""},
		{"max-shrink", `<style>@page { size: A6 }</style><div style="width: 600px; border: 1px solid">szeroka treść</div>` + paragraphs(12, lorem), ""},
		{"max-shrink-late-page", `<style>@page { size: A6 }</style>` + paragraphs(12, lorem) + `<div style="width: 700px">szeroka na końcu</div>`, ""},
		{"fixed-first-page", `<style>@page { size: A6 } .f { position: fixed; top: 0; right: 0; color: red }</style><div class="f">fixed</div>` + paragraphs(12, lorem), ""},
		{"fixed-later-page", `<style>@page { size: A6 } .f { position: fixed; bottom: 0; right: 0; color: red }</style>` + paragraphs(12, lorem) + `<div class="f">fixed</div>`, "fixed box"},
		{"images-backgrounds", `<style>@page { size: A6; background: url(` + img + `) no-repeat; @top-left { content: url(` + img + `) } }
			html { background: #eef } p { background: url(` + img + `) }</style>` + paragraphs(10, lorem), ""},
		{"mixed-page-sizes-opacity", `<style>@page { size: A6; @top-center { content: "półprzezroczysty"; opacity: .4 } } @page big { size: A5 }
			.b { page: big; opacity: .6 }</style>` + paragraphs(4, lorem) + `<div class="b">` + paragraphs(6, lorem) + `</div>`, ""},
		{"root-display-none", `<style>html { display: none } @page { @top-center { content: "x" } }</style><p>niewidoczne</p>`, ""},
		{"target-counter-toc", `<style>@page { size: A6 } a::after { content: leader('.') target-counter(attr(href), page) }</style>
			<p><a href="#a">A</a></p><p><a href="#b">B</a></p>` + paragraphs(10, lorem) + `<h2 id="a">A</h2>` + paragraphs(10, lorem) + `<h2 id="b">B</h2>`, ""},
		// ponowna paginacja: dwa przebiegi (ADR 0036), krótki dokument ułożony w całości; zob. twopass21_test.go
		{"counter-pages-content", `<style>@page { size: A6 } .n::after { content: counter(pages) }</style><p class="n">Stron: </p>` + paragraphs(10, lorem), ""},
		{"negative-z-margin", `<style>@page { size: A6; @top-center { content: "pod"; z-index: -1 } }</style>` + paragraphs(5, lorem), "negative z-index"},
		{"attachments", `<link rel="attachment" href="data:text/plain,dokument" title="opis"><style>@page { size: A6 }</style>
			<p><a rel="attachment" href="data:text/plain,za%C5%82%C4%85cznik">załącznik</a></p>` + paragraphs(8, lorem) +
			`<p><a rel="attachment" href="data:text/plain,drugi">drugi</a></p>`, ""},
		{"tall-absolute", `<style>@page { size: A6 } .a { position: absolute; top: 50px; left: 10px; width: 50px; height: 1500px; background: teal }</style>
			<div class="a"></div>` + paragraphs(10, lorem), ""},
		{"continuous", `<p>jedna strona</p>` + paragraphs(5, lorem), ""},
	}
	return append(cases, pageKeywordCases(lorem)...)
}

// pageKeywordCases: string() i element() ze słowami kluczowymi start, first, last i first-except przy
// przypisaniu na początku strony, w środku strony i na stronie bez przypisania. Słowo start czyta drzewo
// strony (pierwsze dzieci), które przy rysowaniu w trakcie układu jest zwalniane przed polami marginesów.
func pageKeywordCases(lorem string) []struct{ name, html, want string } {
	var out []struct{ name, html, want string }
	for _, keyword := range []string{"start", "first", "last", "first-except"} {
		// strona 1: przypisanie jako pierwsze pudełko strony i drugie w środku; strona 2 (i dalsze) bez
		// przypisania; potem przypisanie na początku strony po wymuszonym podziale i w środku następnej
		body := func(heading func(attrs, text string) string) string {
			return heading("", "Sekcja 1") + paragraphs(1, lorem) + heading("", "Sekcja 1b") + paragraphs(6, lorem) +
				heading(` style="break-before: page"`, "Sekcja 2") + paragraphs(2, lorem) + heading("", "Sekcja 3") + paragraphs(3, lorem)
		}
		out = append(out, struct{ name, html, want string }{"string-" + keyword,
			`<style>@page { size: A6; @top-center { content: string(section, ` + keyword + `) } }
			h2 { string-set: section content() }</style>` + body(func(attrs, text string) string {
				return "<h2" + attrs + ">" + text + "</h2>"
			}), ""})
		// element() ze słowem start też sprawdza string-set o tej nazwie na ścieżce pierwszych dzieci
		// strony, więc nagłówek w przepływie nosi string-set head, a element biegnący jest w nim
		out = append(out, struct{ name, html, want string }{"running-" + keyword,
			`<style>@page { size: A6; @top-center { content: element(head, ` + keyword + `) } }
			h2 { string-set: head content() } .head { position: running(head) }</style>` + body(func(attrs, text string) string {
				return "<h2" + attrs + `><span class="head">Biegnący: ` + text + "</span>" + text + "</h2>"
			}), ""})
	}
	return out
}

func TestStreamDrawingByteIdentity(t *testing.T) {
	opts := streamOptions(t)
	dump := os.Getenv("GOPDFGO_STREAMDRAW_DUMP")
	for _, c := range streamCases(t) {
		t.Run(c.name, func(t *testing.T) {
			html := []byte("<!doctype html><html lang=pl><meta charset=utf-8><body>" + c.html)
			if dump != "" {
				_ = os.WriteFile(filepath.Join(dump, c.name+".html"), html, 0o644)
			}
			o := opts
			o.Continuous = c.name == "continuous"
			streamed, classic, fallback := streamPair(t, html, o)
			if !bytes.Equal(streamed, classic) {
				t.Errorf("wynik różni się od rysowania po ułożeniu całego dokumentu (%d B wobec %d B; przeszkoda %q)",
					len(streamed), len(classic), fallback)
			}
			if c.want == "" && fallback != "" {
				t.Errorf("dokument nie był rysowany w trakcie układu: %s", fallback)
			}
			if c.want != "" && !strings.Contains(fallback, c.want) {
				t.Errorf("przeszkoda %q, oczekiwana zawierająca %q", fallback, c.want)
			}
		})
	}
}

// Dokumenty korpusu: wynik taki sam z rysowaniem w trakcie układu i bez niego.
func TestStreamDrawingCorpus(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "testdata", "documents", "*.html"))
	if len(files) == 0 {
		t.Skip("brak korpusu")
	}
	for _, file := range files {
		var fallback string
		document.OnStream = func(reason string) { fallback = reason }
		streamed, err := renderFile(file, "../testdata", "")
		document.OnStream = nil
		if err != nil {
			t.Fatal(err)
		}
		document.StreamingDisabled = true
		classic, err := renderFile(file, "../testdata", "")
		document.StreamingDisabled = false
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(streamed, classic) {
			t.Errorf("%s: wynik różni się od rysowania po ułożeniu całego dokumentu", filepath.Base(file))
		}
		t.Logf("%s: %s", filepath.Base(file), map[bool]string{true: "w trakcie układu", false: "po ułożeniu: " + fallback}[fallback == ""])
	}
}

// Diagnostyka: GOPDFGO_STREAMDRAW_FILE=plik.html [GOPDFGO_STREAMDRAW_ROOT=katalog zasobów]
// [GOPDFGO_STREAMDRAW_COMPARE=1] go test ./tests/ -run TestStreamDrawingFile -v podaje, czy plik był
// rysowany w trakcie układu, a z COMPARE porównuje wynik z rysowaniem po ułożeniu całego dokumentu.
func TestStreamDrawingFile(t *testing.T) {
	file := os.Getenv("GOPDFGO_STREAMDRAW_FILE")
	if file == "" {
		t.Skip("GOPDFGO_STREAMDRAW_FILE nie ustawione")
	}
	root := os.Getenv("GOPDFGO_STREAMDRAW_ROOT")
	if root == "" {
		root = filepath.Dir(file)
	}
	html, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	absolute, _ := filepath.Abs(file)
	root, _ = filepath.Abs(root)
	opts := streamOptions(t)
	opts.BaseURL = "file://" + absolute
	opts.Resolver = func(target string) (htmlpdf.Resource, error) {
		path := strings.TrimPrefix(target, "file://")
		if !strings.HasPrefix(target, "file://") || !strings.HasPrefix(filepath.Clean(path), root+string(filepath.Separator)) {
			return htmlpdf.DenyAll(target)
		}
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return htmlpdf.DenyAll(target)
		}
		return htmlpdf.Resource{Data: data}, nil
	}
	if os.Getenv("GOPDFGO_STREAMDRAW_COMPARE") == "" {
		fallback := "not reported"
		document.OnStream = func(reason string) { fallback = reason }
		defer func() { document.OnStream = nil }()
		result, err := htmlpdf.Render(context.Background(), html, opts)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%d stron, przeszkoda: %q", result.Pages, fallback)
		return
	}
	streamed, classic, fallback := streamPair(t, html, opts)
	t.Logf("przeszkoda: %q", fallback)
	if !bytes.Equal(streamed, classic) {
		t.Error("wynik różni się od rysowania po ułożeniu całego dokumentu")
	}
}
