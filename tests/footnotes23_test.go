package tests

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo/internal/webrender/html/layout"
)

// Przypisy (CSS GCPM: float: footnote, ::footnote-call, ::footnote-marker, @footnote) w ponownej
// paginacji (spis treści z target-counter, counter(pages) w treści) i w drodze dwuprzebiegowej (ADR 0036,
// dopisek z 2026-11-23). Błędy z 2026-11-23:
//   - przypisy przeniesione na następną stronę (reportedFootnotes) żyły w kontekście, nie w stanie stron
//     (pageMaker): obieg paginacji, który układał od nowa tylko niektóre strony, dawał stronie ułożonej od
//     nowa przypisy przeniesione przez ostatnią stronę ułożoną w tym obiegu (przypis drugi raz, liczba
//     stron rosła z obiegu na obieg, aż makeAllPages kończył się paniką „index out of range”);
//   - drugi i dalsze przypisy z wiersza, w którym pierwszy przeszedł na następną stronę, ginęły;
//   - z kilku przypisów przeniesionych razem, z których drugi już się nie mieścił, ginęły wszystkie
//     poza pierwszym i drugim (albo panika);
//   - kilka długich przypisów przeniesionych razem wychodziło nad górną krawędź strony.
//
// Sprawdzamy: te same bajty z układu całego dokumentu i z drogi dwuprzebiegowej (bez przeszkody), każdy
// przypis dokładnie raz, w kolejności, z wywołaniem i znacznikiem o tym samym numerze, nie przed stroną
// wywołania, oraz tekst stron równy dokumentowi bez ponownej paginacji (wartości spisu treści
// i counter(pages) wpisane wprost, układ w jednym obiegu).

// footnoteDoc buduje dokument z rozdziałami, spisem treści i przypisami: krótkimi, długimi (dwa nie
// mieszczą się razem na stronie) i po trzy długie w jednym wierszu. Gdy toc != nil, numery stron spisu
// treści i liczba stron są wpisane wprost (dokument bez ponownej paginacji).
func footnoteDoc(chapters int, toc []string, total string) []byte {
	lorem := strings.Repeat("Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor. ", 2)
	long := strings.Repeat("Długi przypis ciągnie się przez wiele wierszy i zajmuje dużą część strony. ", 6)
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang=pl><meta charset=utf-8><style>
		@page { size: A6; @footnote { border-top: 1px solid; padding-top: 2px } }
		nav { break-after: page }
		.fn { float: footnote }
		.fn::footnote-call { content: "[" counter(footnote) "]" }
		.fn::footnote-marker { content: "<" counter(footnote) "> " }
		`)
	if toc == nil {
		b.WriteString(`.toc a::after { content: leader('.') target-counter(attr(href), page) }
		h2::after { content: " (strona " counter(page) " z " counter(pages) ")" }`)
	} else {
		for i, p := range toc {
			fmt.Fprintf(&b, ".toc a#t%d::after { content: leader('.') %q }\n", i, p)
		}
		fmt.Fprintf(&b, `h2::after { content: " (strona " counter(page) " z %s)" }`, total)
	}
	b.WriteString(`</style><body><nav><h1>Spis treści</h1>`)
	for i := range chapters {
		fmt.Fprintf(&b, `<p class="toc"><a id="t%d" href="#r%d">Rozdział %d</a></p>`, i, i, i+1)
	}
	b.WriteString(`</nav>`)
	n := 0
	fn := func(text string) string {
		n++
		return fmt.Sprintf(`<span class="fn">FA%d %sFZ%d</span>`, n, text, n)
	}
	for i := range chapters {
		fmt.Fprintf(&b, `<h2 id="r%d">Rozdział %d</h2>`, i, i+1)
		for p := range 3 + i%4 {
			switch (i + p) % 6 {
			case 0:
				fmt.Fprintf(&b, "<p>%s%s %s</p>", lorem, fn("krótki."), lorem)
			case 1:
				fmt.Fprintf(&b, "<p>%s%s%s %s</p>", lorem, fn(long), fn(long), lorem)
			case 2:
				fmt.Fprintf(&b, "<p>%s%s%s%s</p>", lorem, fn("a."), fn(long), fn("b."))
			case 3:
				fmt.Fprintf(&b, "<p>%s%s%s%s %s</p>", lorem, fn(long), fn(long), fn(long), lorem)
			default:
				fmt.Fprintf(&b, "<p>%s</p>", lorem)
			}
		}
	}
	return []byte(b.String())
}

// pdfPageTexts zwraca tekst kolejnych stron (pdftotext).
func pdfPageTexts(t *testing.T, pdf []byte) []string {
	t.Helper()
	cmd := exec.Command("pdftotext", "-", "-")
	cmd.Stdin = bytes.NewReader(pdf)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	pages := strings.Split(string(out), "\f")
	if len(pages) > 0 && strings.TrimSpace(pages[len(pages)-1]) == "" {
		pages = pages[:len(pages)-1]
	}
	return pages
}

var (
	footnoteStartRe  = regexp.MustCompile(`<(\d+)> FA(\d+) `)
	footnoteTokenRe  = regexp.MustCompile(`F([AZ])(\d+)`)
	footnoteCallRe   = regexp.MustCompile(`\[(\d+)\]`)
	tocEntryRe       = regexp.MustCompile(`Rozdział (\d+) \.+\s*(\d+)`)
	chapterHeadingRe = regexp.MustCompile(`Rozdział \d+ \(strona \d+ z\s+(\d+)\)`)
)

// checkFootnotes sprawdza, że każdy z want przypisów jest dokładnie raz, w kolejności, na jednej stronie,
// ze znacznikiem o swoim numerze, a jego wywołanie jest dokładnie raz, nie na późniejszej stronie.
func checkFootnotes(t *testing.T, pages []string, want int) {
	t.Helper()
	start, end, call := map[int][]int{}, map[int][]int{}, map[int][]int{}
	var order []int
	for i, text := range pages {
		for _, m := range footnoteTokenRe.FindAllStringSubmatch(text, -1) {
			n, _ := strconv.Atoi(m[2])
			if m[1] == "A" {
				start[n] = append(start[n], i+1)
				order = append(order, n)
			} else {
				end[n] = append(end[n], i+1)
			}
		}
		for _, m := range footnoteStartRe.FindAllStringSubmatch(text, -1) {
			if m[1] != m[2] {
				t.Errorf("strona %d: znacznik <%s> przy przypisie %s", i+1, m[1], m[2])
			}
		}
		for _, m := range footnoteCallRe.FindAllStringSubmatch(text, -1) {
			n, _ := strconv.Atoi(m[1])
			call[n] = append(call[n], i+1)
		}
	}
	for n := 1; n <= want; n++ {
		switch {
		case len(start[n]) != 1 || len(end[n]) != 1 || len(call[n]) != 1:
			t.Errorf("przypis %d: początek na stronach %v, koniec %v, wywołanie %v; ma być dokładnie raz",
				n, start[n], end[n], call[n])
		case start[n][0] != end[n][0]:
			t.Errorf("przypis %d zaczyna się na stronie %d, a kończy na %d", n, start[n][0], end[n][0])
		case start[n][0] < call[n][0]:
			t.Errorf("przypis %d na stronie %d, przed wywołaniem na stronie %d", n, start[n][0], call[n][0])
		}
	}
	if len(start) != want {
		t.Errorf("przypisów %d, w dokumencie %d", len(start), want)
	}
	for i := 1; i < len(order); i++ {
		if order[i] < order[i-1] {
			t.Errorf("przypis %d (wywołanie na stronie %v) na stronie %v po przypisie %d (wywołanie na stronie %v)",
				order[i], call[order[i]], start[order[i]], order[i-1], call[order[i-1]])
			break
		}
	}
}

func TestFootnotesRepagination(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("brak pdftotext")
	}
	opts := streamOptions(t)
	for _, c := range []struct {
		name     string
		chapters int
		long     bool // ponad TwoPassMinPages stron: droga dwuprzebiegowa rysuje w trakcie układu
	}{{"short", 4, false}, {"long", 14, true}} {
		t.Run(c.name, func(t *testing.T) {
			html := footnoteDoc(c.chapters, nil, "")
			want := bytes.Count(html, []byte(`class="fn"`))
			classic := classicRender(t, html, opts)
			if c.long != (classic.Pages > layout.TwoPassMinPages) {
				t.Fatalf("stron %d, próg TwoPassMinPages %d", classic.Pages, layout.TwoPassMinPages)
			}
			checkDeferred(t, html, opts, classic, "")
			for _, minPages := range []int{0, layout.TwoPassMinPages} {
				result, fallback, mode := twoPassRender(t, html, opts, minPages)
				if !bytes.Equal(result.PDF, classic.PDF) {
					t.Errorf("TwoPassMinPages %d: wynik różni się od układu całego dokumentu (%d B wobec %d B; przeszkoda %q)",
						minPages, len(result.PDF), len(classic.PDF), fallback)
				}
				if (c.long || minPages == 0) && (mode != "stream" || fallback != "") {
					t.Errorf("TwoPassMinPages %d: drugi przebieg nie rysował w trakcie układu: tryb %q, przeszkoda %q",
						minPages, mode, fallback)
				}
			}

			pages := pdfPageTexts(t, classic.PDF)
			checkFootnotes(t, pages, want)

			// ten sam dokument bez ponownej paginacji: wartości spisu treści i counter(pages) wpisane wprost
			body := strings.Join(pages, "\f")
			heading := chapterHeadingRe.FindStringSubmatch(body)
			tocText := body[:strings.Index(body, "Rozdział 1 (strona")]
			entries := tocEntryRe.FindAllStringSubmatch(tocText, -1)
			if heading == nil || len(entries) != c.chapters {
				t.Fatalf("nie odczytano spisu treści (%d pozycji) albo liczby stron (%q)", len(entries), heading)
			}
			if heading[1] != strconv.Itoa(classic.Pages) {
				t.Errorf("counter(pages) %s, stron %d", heading[1], classic.Pages)
			}
			toc := make([]string, len(entries))
			for i, e := range entries {
				toc[i] = e[2]
			}
			static := classicRender(t, footnoteDoc(c.chapters, toc, heading[1]), opts)
			staticPages := pdfPageTexts(t, static.PDF)
			if len(staticPages) != len(pages) {
				t.Fatalf("stron %d, bez ponownej paginacji %d", len(pages), len(staticPages))
			}
			for i := range pages {
				if pages[i] != staticPages[i] {
					t.Errorf("strona %d różni się od układu bez ponownej paginacji:\n%q\nwobec\n%q", i+1, pages[i], staticPages[i])
				}
			}
		})
	}
}
