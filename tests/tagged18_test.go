package tests

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
	"github.com/polestorm/gopdfgo/internal/webrender/html/document"
)

// PDF z tagami, runda 1 (ADR 0033): drzewo struktury z elementów HTML w kolejności dokumentu, treść
// stron oznaczona numerami MCID, tła, obramowania i pola marginesów jako artefakty, odnośniki jako
// elementy Link z odwołaniem OBJR do adnotacji, poziom dokumentu (MarkInfo, Lang, DisplayDocTitle,
// StructParents, Tabs). Bez opcji plik się nie zmienia; z opcją obie drogi rysowania dają te same bajty.

// pdfObjects dzieli nieskompresowany plik PDF na obiekty według numeru.
var pdfObjectRe = regexp.MustCompile(`(?s)(?:^|\n)(\d+) 0 obj\n(.*?)\nendobj`)

func pdfObjects(t *testing.T, pdf []byte) map[int]string {
	t.Helper()
	out := map[int]string{}
	for _, m := range pdfObjectRe.FindAllSubmatch(pdf, -1) {
		n, _ := strconv.Atoi(string(m[1]))
		out[n] = string(m[2])
	}
	if len(out) == 0 {
		t.Fatal("plik bez obiektów (oczekiwany zapis nieskompresowany)")
	}
	return out
}

var (
	refRe       = regexp.MustCompile(`(\d+) 0 R`)
	kidsRe      = regexp.MustCompile(`(?s)/K \[(.*?)\]\n`)
	kidTokenRe  = regexp.MustCompile(`<</Type/MCR/MCID (\d+)[^>]*>>|<</Type/OBJR/Obj (\d+) 0 R[^>]*>>|(\d+) 0 R|(\d+)`)
	typeRe      = regexp.MustCompile(`^<</S/(\w+)`)
	altRe       = regexp.MustCompile(`/Alt \(((?:\\.|[^\\)])*)\)`)
	langRe      = regexp.MustCompile(`/Lang \(((?:\\.|[^\\)])*)\)`)
	structRootR = regexp.MustCompile(`/StructTreeRoot (\d+) 0 R`)
)

// structureDump zapisuje drzewo struktury jako tekst: Typ{Alt}[Lang](dzieci), gdzie # to odcinek
// treści oznaczonej, @ odwołanie do adnotacji.
func structureDump(t *testing.T, pdf []byte) string {
	t.Helper()
	objects := pdfObjects(t, pdf)
	m := structRootR.FindSubmatch(pdf)
	if m == nil {
		t.Fatal("brak /StructTreeRoot")
	}
	root, _ := strconv.Atoi(string(m[1]))
	var dump func(n int) string
	dump = func(n int) string {
		obj := objects[n]
		var b strings.Builder
		if tm := typeRe.FindStringSubmatch(obj); tm != nil {
			b.WriteString(tm[1])
		}
		if am := altRe.FindStringSubmatch(obj); am != nil {
			b.WriteString("{" + am[1] + "}")
		}
		if lm := langRe.FindStringSubmatch(obj); lm != nil {
			b.WriteString("[" + lm[1] + "]")
		}
		km := kidsRe.FindStringSubmatch(obj)
		if km == nil {
			return b.String()
		}
		var kids []string
		for _, tok := range kidTokenRe.FindAllStringSubmatch(km[1], -1) {
			switch {
			case tok[1] != "" || tok[4] != "":
				kids = append(kids, "#")
			case tok[2] != "":
				kids = append(kids, "@")
			case tok[3] != "":
				ref, _ := strconv.Atoi(tok[3])
				kids = append(kids, dump(ref))
			}
		}
		if typeRe.MatchString(obj) {
			return b.String() + "(" + strings.Join(kids, " ") + ")"
		}
		return strings.Join(kids, " ") // korzeń drzewa
	}
	return dump(root)
}

// contentTokens dzieli strumień treści na słowa; nawiasy tablic i słowników są osobnymi słowami.
func contentTokens(content string) []string {
	content = strings.NewReplacer("[", " [ ", "]", " ] ", "<<", " << ", ">>", " >> ").Replace(content)
	return strings.Fields(content)
}

var pageContentsRe = regexp.MustCompile(`/Type/Page\n(?s:.*?)/Contents \[(\d+) 0 R\]`)

// pageContents zwraca treść strumieni stron w kolejności stron.
func pageContents(t *testing.T, pdf []byte) []string {
	t.Helper()
	objects := pdfObjects(t, pdf)
	var out []string
	for _, m := range pageContentsRe.FindAllSubmatch(pdf, -1) {
		n, _ := strconv.Atoi(string(m[1]))
		obj := objects[n]
		start := strings.Index(obj, "stream\n")
		end := strings.LastIndex(obj, "endstream")
		if start < 0 || end < start {
			t.Fatalf("obiekt %d nie jest strumieniem", n)
		}
		out = append(out, obj[start+7:end])
	}
	return out
}

// markingStats sprawdza zagnieżdżenie treści oznaczonej w strumieniu strony: BDC/EMC i q/Q zagnieżdżają
// się poprawnie, ciąg z MCID nie leży w innym ciągu z MCID ani w artefakcie, każdy operator malujący
// leży w treści oznaczonej. Zwraca liczbę ciągów z MCID, artefaktów i artefaktów paginacji z tekstem.
func markingStats(t *testing.T, content string) (mcids, artifacts, paginationText int) {
	t.Helper()
	type frame struct {
		kind string // q, mcid, artifact, pagination, other
	}
	var stack []frame
	inside := func(kinds ...string) bool {
		for _, f := range stack {
			for _, k := range kinds {
				if f.kind == k {
					return true
				}
			}
		}
		return false
	}
	nextMCID := 0
	tokens := contentTokens(content)
	for i, tok := range tokens {
		prev := func(k int) string {
			if i-k < 0 {
				return ""
			}
			return tokens[i-k]
		}
		switch tok {
		case "BDC", "BMC":
			switch {
			case tok == "BDC" && prev(4) == "<<" && prev(3) == "/MCID":
				if inside("mcid", "artifact", "pagination") {
					t.Errorf("ciąg %s MCID %s zagnieżdżony w innej treści oznaczonej", prev(5), prev(2))
				}
				if n, _ := strconv.Atoi(prev(2)); n != nextMCID {
					t.Errorf("MCID %d, oczekiwany kolejny %d", n, nextMCID)
				}
				nextMCID++
				mcids++
				stack = append(stack, frame{"mcid"})
			case tok == "BDC" && prev(4) == "/Artifact" && prev(2) == "/Type/Pagination":
				artifacts++
				stack = append(stack, frame{"pagination"})
			case tok == "BMC" && prev(1) == "/Artifact":
				artifacts++
				stack = append(stack, frame{"artifact"})
			default:
				stack = append(stack, frame{"other"})
			}
		case "q":
			stack = append(stack, frame{"q"})
		case "EMC", "Q":
			if len(stack) == 0 {
				t.Fatalf("%s bez otwarcia", tok)
			}
			top := stack[len(stack)-1]
			if (tok == "Q") != (top.kind == "q") {
				t.Fatalf("%s zamyka %s: treść oznaczona i stany graficzne nie zagnieżdżają się poprawnie", tok, top.kind)
			}
			stack = stack[:len(stack)-1]
		case "TJ", "Tj", "Do", "f", "f*", "S", "s", "B", "B*", "b", "b*", "sh": // operatory malujące
			if !inside("mcid", "artifact", "pagination") {
				t.Errorf("operator %s poza treścią oznaczoną", tok)
			}
			if (tok == "TJ" || tok == "Tj") && inside("pagination") {
				paginationText++
			}
		}
	}
	if len(stack) != 0 {
		t.Errorf("strumień kończy się z %d niezamkniętymi poziomami", len(stack))
	}
	return
}

func renderTagged(t *testing.T, html string, adjust func(*htmlpdf.Options)) htmlpdf.Result {
	t.Helper()
	opts := streamOptions(t)
	opts.Tagged, opts.Uncompressed = true, true
	if adjust != nil {
		adjust(&opts)
	}
	result, err := htmlpdf.Render(context.Background(), []byte(html), opts)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

const taggedPage = `<!doctype html><html lang="pl"><head><meta charset="utf-8"><title>Dokument</title>
<style>@page { size: 300px 300px; margin: 40px; @top-center { content: "Naglowek " counter(page) } }
body { margin: 0; font: 12px sans-serif } .box { background: #eee; border: 1px solid #999; padding: 2px }
.half { opacity: 0.5 }</style></head><body>`

func TestTaggedStructure(t *testing.T) {
	html := taggedPage + `<h1>Tytul</h1>
<p>Akapit z <em>wyroznieniem</em> i <a href="https://example.com/">odnosnikiem</a>.</p>
<div class="box"><p>W ramce</p></div>
<ul><li>jeden</li><li>dwa</li></ul>
<table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2</td></tr></table>
<img alt="Kwadrat" width="10" height="10" src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='10'%3E%3Crect width='10' height='10'/%3E%3C/svg%3E">
<img alt="" width="10" height="10" src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='10'%3E%3Crect width='10' height='10'/%3E%3C/svg%3E">
<svg aria-label="Kolo" width="20" height="20"><circle cx="10" cy="10" r="8"/></svg>
<p class="half" lang="en">Translucent</p></body></html>`
	result := renderTagged(t, html, nil)
	got := structureDump(t, result.PDF)
	want := "Document(H1(#) P(# Span(#) # Link(# # @) #) Div(P(#)) L(LI(Lbl(#) LBody(#)) LI(Lbl(#) LBody(#))) " +
		"Table(TBody(TR(TH(#) TH(#)) TR(TD(#) TD(#)))) Figure{Kwadrat}(#) Figure{Kolo}(# #) P[en](#))"
	if got != want {
		t.Errorf("drzewo struktury:\n%s\noczekiwane:\n%s", got, want)
	}
	for _, needle := range []string{"/MarkInfo <</Marked true", "/Lang (pl)", "/DisplayDocTitle true",
		"/StructParents 0", "/Tabs /S", "/Type/StructTreeRoot"} {
		if !bytes.Contains(result.PDF, []byte(needle)) {
			t.Errorf("w pliku brakuje %q", needle)
		}
	}
	if !regexp.MustCompile(`/StructParent \d+ /Subtype/Link`).Match(result.PDF) {
		t.Error("adnotacja odnośnika bez /StructParent")
	}
	var plainArtifact bool
	for i, content := range pageContents(t, result.PDF) {
		mcids, artifacts, paginationText := markingStats(t, content)
		if mcids == 0 || artifacts == 0 {
			t.Errorf("strona %d: ciągów z MCID: %d, artefaktów: %d", i+1, mcids, artifacts)
		}
		if paginationText == 0 {
			t.Errorf("strona %d: tekst pola marginesu nie jest artefaktem paginacji", i+1)
		}
		plainArtifact = plainArtifact || strings.Contains(content, "/Artifact BMC")
	}
	// tło i obramowanie ramki to artefakt bez właściwości
	if !plainArtifact {
		t.Error("tło i obramowanie nie są artefaktami")
	}
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "Tagged PDF") {
			t.Errorf("nieoczekiwane ostrzeżenie: %s", w.Message)
		}
	}
	// plik skompresowany (strumienie obiektów) czyta pdfinfo z tym samym drzewem
	if path, err := exec.LookPath("pdfinfo"); err == nil {
		compressed := renderTagged(t, html, func(o *htmlpdf.Options) { o.Uncompressed = false })
		file := filepath.Join(t.TempDir(), "tagged.pdf")
		if err := os.WriteFile(file, compressed.PDF, 0o644); err != nil {
			t.Fatal(err)
		}
		out, _ := exec.Command(path, file).CombinedOutput()
		if !regexp.MustCompile(`Tagged:\s+yes`).Match(out) {
			t.Errorf("pdfinfo nie widzi tagów:\n%s", out)
		}
		out, _ = exec.Command(path, "-struct", file).CombinedOutput()
		var types []string
		for _, line := range strings.Split(string(out), "\n") {
			if fields := strings.Fields(line); len(fields) != 0 && !strings.HasPrefix(fields[0], "/") {
				types = append(types, strings.TrimSuffix(fields[0], ":"))
			}
		}
		if got, want := strings.Join(types, " "), "Document H1 P Span Link Object Div P L LI Lbl LBody LI Lbl LBody Table TBody TR TH TH TR TD TD Figure Figure P"; got != want {
			t.Errorf("pdfinfo -struct: %s, oczekiwane %s", got, want)
		}
	}
}

// Odnośnik SVG to Link wewnątrz Figure obrazu, z odwołaniem do własnej adnotacji.
func TestTaggedSVGLink(t *testing.T) {
	html := taggedPage + `<svg aria-label="Wykres" width="60" height="30"><a href="https://example.com/s"><rect width="30" height="30"/></a></svg></body></html>`
	got := structureDump(t, renderTagged(t, html, nil).PDF)
	if want := "Document(Figure{Wykres}(# # Link(@)))"; got != want {
		t.Errorf("drzewo struktury %s, oczekiwane %s", got, want)
	}
}

// Obraz bez tekstu alternatywnego i dokument bez tytułu dają ostrzeżenia, a plik powstaje.
func TestTaggedWarnings(t *testing.T) {
	html := `<!doctype html><html lang="pl"><body><img width="10" height="10" src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='10'%3E%3C/svg%3E"></body></html>`
	result := renderTagged(t, html, nil)
	var image, title bool
	for _, w := range result.Warnings {
		image = image || strings.Contains(w.Message, "image without alternative text")
		title = title || strings.Contains(w.Message, "document without a title")
	}
	if !image || !title {
		t.Errorf("ostrzeżenia: obraz %v, tytuł %v: %v", image, title, result.Warnings)
	}
	if got := structureDump(t, result.PDF); got != "Document(Figure(#))" {
		t.Errorf("drzewo struktury %s", got)
	}
	plain, err := htmlpdf.Render(context.Background(), []byte(html), streamOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range plain.Warnings {
		if strings.Contains(w.Message, "Tagged PDF") {
			t.Errorf("ostrzeżenie bez tagów: %s", w.Message)
		}
	}
}

// Nagłówek tabeli powtórzony na kolejnej stronie jest artefaktem paginacji: czytnik czyta go raz.
func TestTaggedRepeatedTableHeader(t *testing.T) {
	var rows strings.Builder
	for i := range 40 {
		rows.WriteString("<tr><td>" + strconv.Itoa(i) + "</td></tr>")
	}
	html := taggedPage + `<table><thead><tr><th>Naglowek</th></tr></thead><tbody>` + rows.String() + `</tbody></table></body></html>`
	result := renderTagged(t, html, nil)
	if result.Pages < 2 {
		t.Fatalf("stron: %d, oczekiwane co najmniej 2", result.Pages)
	}
	got := structureDump(t, result.PDF)
	if n := strings.Count(got, "TH("); n != 1 {
		t.Errorf("komórek nagłówka w strukturze: %d, oczekiwana 1: %s", n, got)
	}
	if !strings.HasPrefix(got, "Document(Table(THead(TR(TH(#))) TBody(") {
		t.Errorf("drzewo struktury: %s", got)
	}
	for i, content := range pageContents(t, result.PDF) {
		_, _, paginationText := markingStats(t, content)
		// pole marginesu na każdej stronie, a od drugiej strony także powtórzony nagłówek
		want := 1
		if i > 0 {
			want = 2
		}
		if paginationText < want {
			t.Errorf("strona %d: tekstu w artefaktach paginacji %d, oczekiwane %d", i+1, paginationText, want)
		}
	}
}

// Bez opcji plik nie ma tagów; z opcją jest powtarzalny, a obie drogi rysowania dają te same bajty.
func TestTaggedDrawingPathsAndDeterminism(t *testing.T) {
	opts := streamOptions(t)
	opts.Tagged = true
	for _, c := range streamCases(t) {
		t.Run(c.name, func(t *testing.T) {
			html := []byte("<!doctype html><html lang=pl><meta charset=utf-8><body>" + c.html)
			o := opts
			o.Continuous = c.name == "continuous"
			streamed, classic, fallback := streamPair(t, html, o)
			if !bytes.Equal(streamed, classic) {
				t.Errorf("z tagami wynik różni się od rysowania po ułożeniu całego dokumentu (%d B wobec %d B; przeszkoda %q)",
					len(streamed), len(classic), fallback)
			}
			if c.want == "" && fallback != "" {
				t.Errorf("dokument nie był rysowany w trakcie układu: %s", fallback)
			}
			again, err := htmlpdf.Render(context.Background(), html, o)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(again.PDF, streamed) {
				t.Error("dwa renderowania z tagami dały różne bajty")
			}
			// obiekty, w tym drzewo struktury, leżą w skompresowanych strumieniach obiektów
			if !bytes.Contains(streamed, []byte("/Type/ObjStm")) || !bytes.Contains(streamed, []byte("/Type/XRef")) {
				t.Error("plik z tagami bez strumieni obiektów i strumienia odsyłaczy")
			}
			o.Tagged = false
			plain, err := htmlpdf.Render(context.Background(), html, o)
			if err != nil {
				t.Fatal(err)
			}
			for _, needle := range []string{"/StructTreeRoot", "/MarkInfo", "/StructParent", "/DisplayDocTitle", "/Type/ObjStm"} {
				if bytes.Contains(plain.PDF, []byte(needle)) {
					t.Errorf("plik bez opcji zawiera %s", needle)
				}
			}
		})
	}
}

// Dokumenty korpusu z tagami: obie drogi rysowania dają te same bajty, a treść stron jest oznaczona.
func TestTaggedCorpus(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "testdata", "documents", "*.html"))
	if len(files) == 0 {
		t.Skip("brak korpusu")
	}
	tagged := func(o *htmlpdf.Options) { o.Tagged, o.Uncompressed = true, true }
	for _, file := range files {
		streamed, err := renderFileOptions(file, "../testdata", "", tagged)
		if err != nil {
			t.Fatal(err)
		}
		document.StreamingDisabled = true
		classic, err := renderFileOptions(file, "../testdata", "", tagged)
		document.StreamingDisabled = false
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(streamed.PDF, classic.PDF) {
			t.Errorf("%s: z tagami wynik różni się od rysowania po ułożeniu całego dokumentu", filepath.Base(file))
		}
		for _, content := range pageContents(t, streamed.PDF) {
			markingStats(t, content) // zgłasza malowanie poza treścią oznaczoną i złe zagnieżdżenie
		}
	}
}

// PDF/A-2a: tagi włączone przez Conformance, XMP z poziomem A; veraPDF (narzędzie testowe), gdy jest.
func TestTaggedPDFA2a(t *testing.T) {
	html := taggedPage + `<h1>Tytul</h1><p>Akapit z <a href="https://example.com/">odnosnikiem</a>.</p></body></html>`
	for _, c := range []struct {
		conformance htmlpdf.Conformance
		flag, part  string
	}{{htmlpdf.PDFA2a, "2a", "2"}, {htmlpdf.PDFA3a, "3a", "3"}} {
		opts := streamOptions(t)
		opts.Conformance, opts.Uncompressed = c.conformance, true
		readable, err := htmlpdf.Render(context.Background(), []byte(html), opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range []string{"<pdfaid:part>" + c.part + "</pdfaid:part>", "<pdfaid:conformance>A</pdfaid:conformance>",
			"/StructTreeRoot", "/MarkInfo"} {
			if !bytes.Contains(readable.PDF, []byte(needle)) {
				t.Errorf("%s: w pliku brakuje %q", c.flag, needle)
			}
		}
		opts.Uncompressed = false
		result, err := htmlpdf.Render(context.Background(), []byte(html), opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := exec.LookPath("verapdf"); err != nil {
			continue
		}
		file := filepath.Join(t.TempDir(), c.flag+".pdf")
		if err := os.WriteFile(file, result.PDF, 0o644); err != nil {
			t.Fatal(err)
		}
		report, _ := exec.Command("verapdf", "-f", c.flag, "--format", "text", file).CombinedOutput()
		if !strings.Contains(string(report), "\nPASS ") && !strings.HasPrefix(string(report), "PASS ") {
			t.Errorf("veraPDF odrzuca plik jako PDF/A-%s:\n%s", c.flag, report)
		}
	}
}
