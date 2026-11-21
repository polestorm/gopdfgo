package tests

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo"
	"github.com/polestorm/gopdfgo/internal/webrender/html/document"
	"github.com/polestorm/gopdfgo/internal/webrender/html/layout"
)

// Układ w dwóch przebiegach (ADR 0036): dokument, który wymaga ponownej paginacji (spis treści
// z target-counter, counter(pages) w treści, string-set i bookmark-label z licznikami stron albo celu),
// najpierw jest paginowany bez zachowywania stron, a potem układany i rysowany strona po stronie. Wynik ma
// być bajt w bajt taki sam jak z układu całego dokumentu przed rysowaniem, a przypadki, których drugi
// przebieg nie obsługuje, mają wracać do tej drogi.

// twoPassRender renderuje dokument z rysowaniem w trakcie układu; minPages ustawia layout.TwoPassMinPages
// (0: drugi przebieg także dla krótkiego dokumentu). Zwraca wynik, opis przeszkody i tryb dwóch przebiegów
// ("" nieużyty, "whole" krótki dokument ułożony w całości, "stream" drugi przebieg rysujący).
func twoPassRender(t *testing.T, html []byte, opts gopdfgo.Options, minPages int) (result gopdfgo.Result, fallback, mode string) {
	t.Helper()
	saved := layout.TwoPassMinPages
	layout.TwoPassMinPages = minPages
	fallback = "not reported"
	document.OnStream = func(reason string) { fallback = reason }
	document.OnTwoPass = func(whole bool) { mode = map[bool]string{true: "whole", false: "stream"}[whole] }
	defer func() { document.OnStream, document.OnTwoPass, layout.TwoPassMinPages = nil, nil, saved }()
	result, err := gopdfgo.Render(context.Background(), html, opts)
	if err != nil {
		t.Fatal(err)
	}
	return result, fallback, mode
}

func classicRender(t *testing.T, html []byte, opts gopdfgo.Options) gopdfgo.Result {
	t.Helper()
	document.StreamingDisabled = true
	defer func() { document.StreamingDisabled = false }()
	result, err := gopdfgo.Render(context.Background(), html, opts)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// twoPassCases: want = "" znaczy, że drugi przebieg ma rysować strony w trakcie układu, inaczej podciąg
// spodziewanej przeszkody.
func twoPassCases() []struct{ name, html, want string } {
	lorem := strings.Repeat("Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor. ", 6)
	toc := func(n int) string {
		var b strings.Builder
		b.WriteString(`<nav><h1>Spis treści</h1>`)
		for i := range n {
			fmt.Fprintf(&b, `<p class="toc"><a href="#r%d">Rozdział %d</a></p>`, i, i+1)
		}
		b.WriteString(`</nav>`)
		return b.String()
	}
	chapters := func(n, length int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, `<h2 id="r%d">Rozdział %d</h2>`, i, i+1)
			b.WriteString(paragraphs(length, lorem))
		}
		return b.String()
	}
	tocStyle := `@page { size: A6 } .toc a::after { content: leader('.') target-counter(attr(href), page) }`
	return []struct{ name, html, want string }{
		{"toc", `<style>` + tocStyle + ` nav { break-after: page }</style>` + toc(12) + chapters(12, 3), ""},
		{"toc-page-x-of-y", `<style>` + tocStyle + ` h2::after { content: " (strona " counter(page) " z " counter(pages) ")" }</style>` +
			toc(12) + chapters(12, 3), ""},
		{"counter-pages-content", `<style>@page { size: A6 } h2::after { content: " / " counter(pages) }</style>` + chapters(10, 3), ""},
		{"counter-page-content", `<style>` + tocStyle + ` h2::before { content: counter(page) ". " }</style>` + toc(8) + chapters(8, 3), ""},
		// odwołania wstecz i do kotwicy na tej samej stronie
		{"toc-backward-self", `<style>` + tocStyle + ` .back::after { content: " s. " target-counter(attr(href), page) }</style>` +
			`<p><a class="back" href="#self" id="self">tu</a></p>` + toc(6) + chapters(6, 3) +
			`<p><a class="back" href="#r0">wstecz do 1</a> <a class="back" href="#r5">wstecz do 6</a></p>`, ""},
		// target-counter(…, pages): liczba stron dokumentu, ta sama dla każdej kotwicy (zob. TestTargetCounterPages)
		{"target-counter-pages", `<style>@page { size: A6 } .toc a::after { content: target-counter(attr(href), page) "/" target-counter(attr(href), pages) }</style>` +
			toc(6) + chapters(6, 3), ""},
		{"target-counter-pages-backward", `<style>@page { size: A6 } .back::after { content: " s. " target-counter(attr(href), page) "/" target-counter(attr(href), pages) }</style>` +
			chapters(6, 3) + `<p><a class="back" href="#r0">wstecz do 1</a> <a class="back" href="#r4">wstecz do 5</a></p>`, ""},
		{"target-text", `<style>@page { size: A6 } .toc a::after { content: " — " target-text(attr(href)) " s. " target-counter(attr(href), page) }</style>` +
			toc(6) + chapters(6, 3), ""},
		{"string-set-pages", `<style>@page { size: A6; @top-center { content: string(head) } }
			h2 { string-set: head content() " (" counter(page) "/" counter(pages) ")" }</style>` + chapters(8, 3), ""},
		{"string-set-target", `<style>@page { size: A6; @top-center { content: string(ref) } }
			.toc a { string-set: ref "dalej s. " target-counter(attr(href), page) }</style>` + toc(1) + chapters(1, 4) +
			`<p class="toc"><a href="#end">koniec</a></p>` + paragraphs(10, lorem) + `<h2 id="end">Koniec</h2>`, ""},
		{"bookmark-label-target", `<style>` + tocStyle + ` .toc a { bookmark-level: 1; bookmark-label: content() " s. " target-counter(attr(href), page) }</style>` +
			toc(8) + chapters(8, 3), ""},
		{"toc-footnotes", `<style>` + tocStyle + ` .fn { float: footnote }</style>` + toc(6) +
			strings.ReplaceAll(chapters(6, 3), "</p>", `<span class="fn">Przypis.</span></p>`), ""},
		{"toc-running-margins", `<style>` + tocStyle + ` @page { @top-center { content: element(head) } @bottom-center { content: counter(page) " / " counter(pages) } }
			.head { position: running(head) }</style><div class="head">Raport <a href="#r3">r. 4</a></div>` + toc(8) + chapters(8, 3), ""},
		{"toc-max-shrink", `<style>` + tocStyle + `</style>` + toc(8) + `<div style="width: 600px; border: 1px solid">szeroka treść</div>` + chapters(8, 3), ""},
		{"toc-fixed-later-page", `<style>` + tocStyle + ` .f { position: fixed; bottom: 0; right: 0 }</style>` + toc(6) + chapters(6, 3) +
			`<div class="f">fixed</div>`, "fixed box"},
		{"toc-negative-z-margin", `<style>` + tocStyle + ` @page { @top-center { content: "pod"; z-index: -1 } }</style>` + toc(6) + chapters(6, 3), "negative z-index"},
	}
}

func TestTwoPassByteIdentity(t *testing.T) {
	opts := streamOptions(t)
	for _, c := range twoPassCases() {
		t.Run(c.name, func(t *testing.T) {
			html := []byte("<!doctype html><html lang=pl><meta charset=utf-8><body>" + c.html)
			classic := classicRender(t, html, opts)
			for _, minPages := range []int{0, layout.TwoPassMinPages} {
				result, fallback, mode := twoPassRender(t, html, opts, minPages)
				if !bytes.Equal(result.PDF, classic.PDF) {
					t.Errorf("TwoPassMinPages %d: wynik różni się od układu całego dokumentu (%d B wobec %d B; przeszkoda %q)",
						minPages, len(result.PDF), len(classic.PDF), fallback)
				}
				if len(result.Warnings) != len(classic.Warnings) {
					t.Errorf("TwoPassMinPages %d: ostrzeżeń %d, bez rysowania w trakcie układu %d", minPages, len(result.Warnings), len(classic.Warnings))
				}
				if mode == "" {
					t.Errorf("TwoPassMinPages %d: dokument nie był układany w dwóch przebiegach (przeszkoda %q)", minPages, fallback)
				}
				if minPages > 0 {
					if result.Pages > minPages {
						t.Fatalf("dokument ma %d stron, więcej niż TwoPassMinPages", result.Pages)
					}
					if mode != "whole" || fallback != "" {
						t.Errorf("krótki dokument: tryb %q, przeszkoda %q; oczekiwany układ w całości", mode, fallback)
					}
					continue
				}
				if c.want == "" && (fallback != "" || mode != "stream") {
					t.Errorf("drugi przebieg nie rysował w trakcie układu: tryb %q, przeszkoda %q", mode, fallback)
				}
				if c.want != "" && !strings.Contains(fallback, c.want) {
					t.Errorf("przeszkoda %q, oczekiwana zawierająca %q", fallback, c.want)
				}
			}
		})
	}
}

// Dokumenty korpusu z drugim przebiegiem także dla krótkich dokumentów: wynik taki sam jak z układu
// całego dokumentu przed rysowaniem.
func TestTwoPassCorpus(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "testdata", "documents", "*.html"))
	if len(files) == 0 {
		t.Skip("brak korpusu")
	}
	saved := layout.TwoPassMinPages
	layout.TwoPassMinPages = 0
	defer func() { layout.TwoPassMinPages = saved }()
	for _, file := range files {
		mode := ""
		document.OnTwoPass = func(whole bool) { mode = "two-pass" }
		streamed, err := renderFile(file, "../testdata", "")
		document.OnTwoPass = nil
		if err != nil {
			t.Fatal(err)
		}
		if mode == "" {
			continue
		}
		document.StreamingDisabled = true
		classic, err := renderFile(file, "../testdata", "")
		document.StreamingDisabled = false
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(streamed, classic) {
			t.Errorf("%s: wynik różni się od układu całego dokumentu", filepath.Base(file))
		}
		t.Logf("%s: dwa przebiegi", filepath.Base(file))
	}
}

// target-counter(…, pages) podaje liczbę stron dokumentu (CSS Generated Content 3: wartość licznika pages
// w miejscu celu; CSS Paged Media 3: pages ma tę samą wartość na każdej stronie), w obu drogach układu
// i dla odwołań w przód i wstecz. Przejęty kod dawał liczbę stron z obiegu, w którym ułożono kotwicę
// (zwykle 0).
func TestTargetCounterPages(t *testing.T) {
	opts := streamOptions(t)
	lorem := strings.Repeat("Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor. ", 6)
	html := `<!doctype html><html lang=pl><meta charset=utf-8><style>@page { size: A6 }
		a::after { content: " [" target-counter(attr(href), page) "/" target-counter(attr(href), pages) "]" }
		h2 { break-before: page }</style>
		<p><a href="#c">do C</a> <a href="#b">do B</a></p>
		<h2 id="a">A</h2>` + paragraphs(2, lorem) + `<h2 id="b">B</h2>` + paragraphs(2, lorem) +
		`<h2 id="c">C</h2><p><a href="#a">do A</a></p>`
	for _, streaming := range []bool{true, false} {
		document.StreamingDisabled = !streaming
		result, err := gopdfgo.Render(context.Background(), []byte(html), opts)
		document.StreamingDisabled = false
		if err != nil {
			t.Fatal(err)
		}
		text := string(extractText(t, result.PDF))
		n := result.Pages
		if n < 4 {
			t.Fatalf("stron %d, oczekiwane co najmniej 4", n)
		}
		for _, want := range []string{fmt.Sprintf("do C [%d/%d]", n, n), fmt.Sprintf("do A [2/%d]", n)} {
			if !strings.Contains(text, want) {
				t.Errorf("rysowanie w trakcie układu %v: brak %q w tekście %q", streaming, want, text)
			}
		}
		if got := strings.Count(text, fmt.Sprintf("/%d]", n)); got != 3 {
			t.Errorf("rysowanie w trakcie układu %v: liczba stron %d w %d odwołaniach z 3: %q", streaming, n, got, text)
		}
	}
}
