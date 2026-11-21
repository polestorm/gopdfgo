package tests

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo"
)

// PDF z tagami, runda 2 (ADR 0033): PDF/UA-1 (ISO 14289-1). Elementy listy jako Lbl i LBody, komórki
// tabeli z Scope, RowSpan i ColSpan, siatka tabeli zachowana (puste komórki, TBody przy THead), figure
// jako Div z Caption, opis adnotacji odnośników, identyfikator pdfuaid w XMP, ostrzeżenia o wadach
// dokumentu.

func renderUA(t *testing.T, html string, adjust func(*gopdfgo.Options)) gopdfgo.Result {
	t.Helper()
	return renderTagged(t, html, func(o *gopdfgo.Options) {
		o.Tagged = false // tagi włącza sama norma
		o.Accessibility = gopdfgo.PDFUA1
		if adjust != nil {
			adjust(o)
		}
	})
}

// objectsWith zwraca obiekty elementów struktury typu kind w kolejności numerów obiektów.
func structureElements(t *testing.T, pdf []byte, kind string) []string {
	t.Helper()
	objects := pdfObjects(t, pdf)
	var numbers []int
	for n, obj := range objects {
		if strings.HasPrefix(obj, "<</S/"+kind+"/") || strings.HasPrefix(obj, "<</S/"+kind+"\n") {
			numbers = append(numbers, n)
		}
	}
	sort.Ints(numbers)
	out := make([]string, len(numbers))
	for i, n := range numbers {
		out[i] = objects[n]
	}
	return out
}

var tableAttrRe = regexp.MustCompile(`<</O/Table([^>]*)>>`)

// tableAttributes zwraca atrybuty /Table elementów typu kind, np. "/ColSpan 2/Scope /Column".
func tableAttributes(t *testing.T, pdf []byte, kind string) []string {
	t.Helper()
	var out []string
	for _, obj := range structureElements(t, pdf, kind) {
		attr := ""
		if m := tableAttrRe.FindStringSubmatch(obj); m != nil {
			attr = strings.ReplaceAll(m[1], " /", "/")
		}
		out = append(out, attr)
	}
	return out
}

func TestPDFUAListsAndTables(t *testing.T) {
	html := taggedPage + `<h1>Tytul</h1>
<ol><li>jeden</li><li>dwa<ul style="list-style-type: square"><li>zagniezdzony</li></ul></li></ol>
<ul style="list-style: none"><li>bez znacznika</li></ul>
<table><thead><tr><th>A</th><th colspan="2">B</th></tr></thead>
<tbody><tr><th>w1</th><td rowspan="2">x</td><td></td></tr><tr><th scope="row">w2</th><td>y</td></tr></tbody></table>
<table><tr><th>K</th><td>1</td></tr></table>
<table><thead><tr><th>Sam naglowek</th></tr></thead></table></body></html>`
	result := renderUA(t, html, nil)
	got := structureDump(t, result.PDF)
	want := "Document(H1(#) " +
		"L(LI(Lbl(#) LBody(#)) LI(Lbl(#) LBody(# L(LI(Lbl(#) LBody(#)))))) L(LI(LBody(#))) " +
		"Table(THead(TR(TH(#) TH(#))) TBody(TR(TH(#) TD(#) TD()) TR(TH(#) TD(#)))) " +
		"Table(TBody(TR(TH(#) TD(#)))) " +
		"Table(THead(TR(TH(#))) TBody()))"
	if got != want {
		t.Errorf("drzewo struktury:\n%s\noczekiwane:\n%s", got, want)
	}
	// w kolejności obiektów: tabela 1 (A, B, w1, w2), tabela 2 (K), tabela 3
	if got, want := strings.Join(tableAttributes(t, result.PDF, "TH"), " | "),
		"/Scope/Column | /ColSpan 2/Scope/Column | /Scope/Row | /Scope/Row | /Scope/Row | /Scope/Column"; got != want {
		t.Errorf("atrybuty TH: %s, oczekiwane %s", got, want)
	}
	if got, want := strings.Join(tableAttributes(t, result.PDF, "TD"), " | "), "/RowSpan 2 |  |  | "; got != want {
		t.Errorf("atrybuty TD: %q, oczekiwane %q", got, want)
	}
	var numbering []string
	for _, obj := range structureElements(t, result.PDF, "L") {
		m := regexp.MustCompile(`/ListNumbering\s*/(\w+)`).FindStringSubmatch(obj)
		if m == nil {
			numbering = append(numbering, "-")
		} else {
			numbering = append(numbering, m[1])
		}
	}
	if got, want := strings.Join(numbering, " "), "Decimal Square -"; got != want {
		t.Errorf("ListNumbering: %s, oczekiwane %s", got, want)
	}
	// znacznik ma w treści strony znacznik Lbl, reszta elementu listy LBody
	content := strings.Join(pageContents(t, result.PDF), "\n")
	for _, tag := range []string{"/Lbl <</MCID", "/LBody <</MCID"} {
		if !strings.Contains(content, tag) {
			t.Errorf("w treści strony brakuje %q", tag)
		}
	}
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "Tagged PDF") {
			t.Errorf("nieoczekiwane ostrzeżenie: %s", w.Message)
		}
	}
}

// figure to Div z Caption; obraz bez alt jako jedyna treść osadzona figure z podpisem dostaje Alt
// z podpisu (HTML pozwala wtedy pominąć alt), więc nie ma ostrzeżenia.
func TestPDFUAFigure(t *testing.T) {
	const img = `<img width="10" height="10" src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='10'%3E%3Crect width='10' height='10'/%3E%3C/svg%3E"`
	html := taggedPage + `<h1>Tytul</h1>
<figure>` + img + ` alt="Kwadrat"><figcaption>Podpis pierwszy</figcaption></figure>
<figure>` + img + `><figcaption>Podpis  drugi</figcaption></figure></body></html>`
	result := renderUA(t, html, nil)
	got := structureDump(t, result.PDF)
	want := "Document(H1(#) Div(Figure{Kwadrat}(#) Caption(#)) Div(Figure{Podpis drugi}(#) Caption(#)))"
	if got != want {
		t.Errorf("drzewo struktury:\n%s\noczekiwane:\n%s", got, want)
	}
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "Tagged PDF") {
			t.Errorf("nieoczekiwane ostrzeżenie: %s", w.Message)
		}
	}
	// dwa obrazy w jednym figure: podpis opisuje grupę, nie obraz; obrazy bez alt dają ostrzeżenia
	result = renderUA(t, taggedPage+`<h1>T</h1><figure>`+img+`>`+img+`><figcaption>Grupa</figcaption></figure></body></html>`, nil)
	count := 0
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "image without alternative text") {
			count++
		}
	}
	if count != 2 {
		t.Errorf("ostrzeżeń o obrazach bez alt: %d, oczekiwane 2: %v", count, result.Warnings)
	}
}

// Adnotacja odnośnika ma /Contents: aria-label, tekst odnośnika z alt obrazów, title, adres celu.
func TestPDFUALinkContents(t *testing.T) {
	html := taggedPage + `<h1 id="top">Tytul</h1>
<p><a href="https://example.com/a">Tekst <b>odnosnika</b></a></p>
<p><a href="https://example.com/b" aria-label="Etykieta">x</a></p>
<p><a href="https://example.com/c"><img alt="Logo" width="10" height="10" src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='10'%3E%3Crect width='10' height='10'/%3E%3C/svg%3E"></a></p>
<p><a href="#top" title="Na gore" style="display: inline-block; width: 20px; height: 10px"></a></p></body></html>`
	result := renderUA(t, html, nil)
	var got []string
	for _, m := range regexp.MustCompile(`/Contents \(([^)]*)\)`).FindAllSubmatch(result.PDF, -1) {
		// pudełko liniowe wewnątrz odnośnika ma własną adnotację z tym samym opisem
		if len(got) == 0 || got[len(got)-1] != string(m[1]) {
			got = append(got, string(m[1]))
		}
	}
	if want := "Tekst odnosnika|Etykieta|Logo|Na gore"; strings.Join(got, "|") != want {
		t.Errorf("/Contents adnotacji: %q, oczekiwane %q", strings.Join(got, "|"), want)
	}
	// bez tagów adnotacje bez /Contents (plik bez zmian)
	opts := streamOptions(t)
	opts.Uncompressed = true
	plain, err := gopdfgo.Render(context.Background(), []byte(html), opts)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(plain.PDF, []byte("/Contents (")) {
		t.Error("plik bez tagów z /Contents adnotacji")
	}
}

// Wady dokumentu, których silnik nie naprawia, dają ostrzeżenia; plik powstaje.
func TestPDFUAWarnings(t *testing.T) {
	html := `<!doctype html><html><head><title>T</title></head><body><h2>Pierwszy</h2><h4>Przeskok</h4><h3>Dobrze</h3>
<table><tr><td>1</td><td>2</td></tr><tr><td>3</td></tr></table></body></html>`
	result := renderUA(t, html, nil)
	var messages []string
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "Tagged PDF") {
			messages = append(messages, w.Message)
		}
	}
	joined := strings.Join(messages, "\n")
	for _, needle := range []string{"document without a language", `the first heading is <h2> "Pierwszy"`,
		`heading level skipped: <h4> after <h2>, "Przeskok"`, "table rows with different numbers of columns"} {
		if !strings.Contains(joined, needle) {
			t.Errorf("brak ostrzeżenia %q:\n%s", needle, joined)
		}
	}
	if len(messages) != 4 {
		t.Errorf("ostrzeżeń: %d, oczekiwane 4:\n%s", len(messages), joined)
	}
	// struktura nagłówków zostaje jak w dokumencie (ADR 0033)
	if got := structureDump(t, result.PDF); !strings.HasPrefix(got, "Document(H2(#) H4(#) H3(#) ") {
		t.Errorf("drzewo struktury: %s", got)
	}
}

var pdfuaidRe = regexp.MustCompile(`<pdfuaid:part>1</pdfuaid:part>`)

// Metadane: PDF/UA-1 sam pisze XMP z pdfuaid i tytułem, bez pdfaid i schematu rozszerzeń; z PDF/A
// identyfikatory obu norm i opis schematu pdfuaid. veraPDF (narzędzie testowe), gdy jest.
func TestPDFUAMetadata(t *testing.T) {
	html := taggedPage + `<h1>Tytul</h1><p>Akapit z <a href="https://example.com/">odnosnikiem</a>.</p>
<ul><li>punkt</li></ul><table><thead><tr><th>A</th></tr></thead><tbody><tr><td>1</td></tr></tbody></table></body></html>`
	verapdf, _ := exec.LookPath("verapdf")
	for _, c := range []struct {
		name        string
		conformance gopdfgo.Conformance
		profiles    []string
	}{{"ua1", gopdfgo.ConformanceNone, []string{"ua1"}}, {"2a-ua1", gopdfgo.PDFA2a, []string{"ua1", "2a"}},
		{"3a-ua1", gopdfgo.PDFA3a, []string{"ua1", "3a"}}} {
		readable := renderUA(t, html, func(o *gopdfgo.Options) { o.Conformance = c.conformance })
		xmp := string(readable.PDF)
		if !pdfuaidRe.MatchString(xmp) || !strings.Contains(xmp, "<dc:title>") || !bytes.Contains(readable.PDF, []byte("/StructTreeRoot")) {
			t.Errorf("%s: brak pdfuaid, dc:title albo drzewa struktury", c.name)
		}
		withA := c.conformance != gopdfgo.ConformanceNone
		if strings.Contains(xmp, "<pdfaid:part>") != withA || strings.Contains(xmp, "pdfaExtension:schemas") != withA {
			t.Errorf("%s: pdfaid i schemat rozszerzeń mają być tylko przy PDF/A", c.name)
		}
		if verapdf == "" {
			continue
		}
		result := renderUA(t, html, func(o *gopdfgo.Options) { o.Conformance, o.Uncompressed = c.conformance, false })
		file := filepath.Join(t.TempDir(), c.name+".pdf")
		if err := os.WriteFile(file, result.PDF, 0o644); err != nil {
			t.Fatal(err)
		}
		for _, profile := range c.profiles {
			report, _ := exec.Command(verapdf, "-f", profile, "--format", "text", file).CombinedOutput()
			if !strings.HasPrefix(string(report), "PASS ") {
				t.Errorf("%s: veraPDF odrzuca plik w profilu %s:\n%s", c.name, profile, report)
			}
		}
	}
	plain := renderTagged(t, html, nil)
	if pdfuaidRe.Match(plain.PDF) {
		t.Error("PDF z tagami bez PDF/UA zawiera pdfuaid")
	}
}

// Korpus z PDF/UA-1 i PDF/A-2a w veraPDF: przyjęte wszystkie dokumenty poza tymi, w których wada leży
// w źródle (przeskok h1 → h3, obrazy SVG bez tekstu alternatywnego); PDF/A-2a wszystkie.
func TestPDFUACorpus(t *testing.T) {
	verapdf, err := exec.LookPath("verapdf")
	if err != nil {
		t.Skip("brak veraPDF")
	}
	files, _ := filepath.Glob(filepath.Join("..", "testdata", "documents", "*.html"))
	if len(files) == 0 {
		t.Skip("brak korpusu")
	}
	dir := t.TempDir()
	var outputs []string
	for _, file := range files {
		result, err := renderFileOptions(file, "../testdata", "", func(o *gopdfgo.Options) {
			o.Conformance, o.Accessibility = gopdfgo.PDFA2a, gopdfgo.PDFUA1
		})
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, strings.TrimSuffix(filepath.Base(file), ".html")+".pdf")
		if err := os.WriteFile(out, result.PDF, 0o644); err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, out)
	}
	sourceDefects := map[string]bool{"09-lists-counters": true, "11-edge-cases": true, "12-floats-positioning": true,
		"20-invoice": true, "21-mixed-report": true}
	for _, profile := range []string{"ua1", "2a"} {
		report, _ := exec.Command(verapdf, append([]string{"-f", profile, "--format", "text"}, outputs...)...).CombinedOutput()
		checked := 0
		defer func() {
			if checked != len(outputs) {
				t.Errorf("veraPDF %s: wyników %d, plików %d:\n%s", profile, checked, len(outputs), report)
			}
		}()
		for _, line := range strings.Split(string(report), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 || (fields[0] != "PASS" && fields[0] != "FAIL") {
				continue
			}
			checked++
			name := strings.TrimSuffix(filepath.Base(fields[1]), ".pdf")
			if want := profile == "2a" || !sourceDefects[name]; (fields[0] == "PASS") != want {
				t.Errorf("veraPDF %s: %s %s, oczekiwane PASS=%v", profile, fields[0], name, want)
			}
		}
	}
}
