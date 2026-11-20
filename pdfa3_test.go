package gopdfgo

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// invoiceXML to próbka danych strukturalnych e-faktury (w rodzaju Factur-X), osadzana jako plik stowarzyszony.
const invoiceXML = `<?xml version="1.0" encoding="UTF-8"?>
<Invoice xmlns="urn:example:invoice"><ID>FV/1/2026</ID><Total currency="PLN">123.00</Total></Invoice>
`

// attachmentDocument wskazuje załączniki także z HTML: <link rel=attachment> (cały dokument)
// i <a rel=attachment> (adnotacja pliku na stronie).
const attachmentDocument = `<!DOCTYPE html><html lang="pl"><head><meta charset="utf-8"><title>Faktura</title>
<link rel="attachment" href="https://example.test/zalaczniki/umowa.txt" title="Umowa ramowa">
<style>@page { size: 300px 200px; margin: 20px } body { margin: 0; font: 12px sans-serif }</style></head>
<body><p>Faktura FV/1/2026. <a rel="attachment" href="https://example.test/zalaczniki/notatka.txt">Notatka</a></p></body></html>`

// attachmentResolver dostarcza z pamięci pliki wskazane w attachmentDocument i liczy pobrania.
func attachmentResolver(fetched map[string]int) Resolver {
	files := map[string]string{
		"https://example.test/zalaczniki/umowa.txt":   "Treść umowy.\n",
		"https://example.test/zalaczniki/notatka.txt": "Treść notatki.\n",
	}
	return func(url string) (Resource, error) {
		fetched[url]++
		if data, ok := files[url]; ok {
			return Resource{Data: []byte(data), MediaType: "text/plain"}, nil
		}
		return DenyAll(url)
	}
}

func attachmentOptions(t *testing.T, conformance Conformance, fetched map[string]int) Options {
	return Options{Fonts: testFonts(t), Conformance: conformance, Resolver: attachmentResolver(fetched),
		Attachments: []Attachment{
			{Name: "factur-x.xml", Data: []byte(invoiceXML), MediaType: "text/xml; charset=utf-8",
				Description: "Faktura w postaci XML", Relationship: RelationshipAlternative,
				ModTime: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)},
			{Name: "dane.txt", Data: []byte("dane źródłowe\n")}, // bez typu i związku: wartości domyślne
		}}
}

func TestAttachmentsPDFA3b(t *testing.T) {
	fetched := map[string]int{}
	first, err := Render(context.Background(), []byte(attachmentDocument), attachmentOptions(t, PDFA3b, fetched))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(context.Background(), []byte(attachmentDocument), attachmentOptions(t, PDFA3b, fetched))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.PDF, second.PDF) {
		t.Error("dwa przebiegi z załącznikami dały różne bajty")
	}
	for _, url := range []string{"https://example.test/zalaczniki/umowa.txt", "https://example.test/zalaczniki/notatka.txt"} {
		if fetched[url] == 0 {
			t.Errorf("załącznik %s nie został pobrany przez Resolver", url)
		}
	}

	file := first.PDF
	for _, needle := range []string{
		"<pdfaid:part>3</pdfaid:part>", "<pdfaid:conformance>B</pdfaid:conformance>",
		"/Subtype /text#2Fxml", "/Subtype /application#2Foctet-stream", "/Subtype /text#2Fplain",
		"/UF (factur-x.xml) /F (factur-x.xml)", "/AFRelationship /Alternative", "/ModDate (D:20261120120000+00'00')",
		"/UF (umowa.txt) /F (umowa.txt)", "/UF (notatka.txt) /F (notatka.txt)", "/Subtype/FileAttachment",
	} {
		if !bytes.Contains(file, []byte(needle)) {
			t.Errorf("w pliku brakuje %q", needle)
		}
	}
	if n := bytes.Count(file, []byte("/AFRelationship /Unspecified")); n != 3 {
		t.Errorf("/AFRelationship /Unspecified: %d razy, oczekiwane 3 (dane.txt, umowa.txt, notatka.txt)", n)
	}
	// drzewo nazw: klucze to nazwy plików, posortowane; korzeń bez /Limits
	names := regexp.MustCompile(`<</Names \[\(([^)]*)\) \d+ 0 R \(([^)]*)\) \d+ 0 R \(([^)]*)\) \d+ 0 R\]>>`).FindSubmatch(file)
	if names == nil {
		t.Fatal("brak drzewa /EmbeddedFiles z trzema plikami")
	}
	if got := string(names[1]) + "," + string(names[2]) + "," + string(names[3]); got != "dane.txt,factur-x.xml,umowa.txt" {
		t.Errorf("klucze drzewa /EmbeddedFiles: %s", got)
	}
	// /AF katalogu: trzy pliki dokumentu i plik adnotacji
	if af := regexp.MustCompile(`/AF \[(\d+ 0 R ?){4}\]`).Find(file); af == nil {
		t.Error("katalog nie ma tablicy /AF z czterema plikami")
	}

	// zwykły PDF: te same pliki, ale bez /AF i bez metadanych PDF/A
	plain, err := Render(context.Background(), []byte(attachmentDocument), attachmentOptions(t, ConformanceNone, fetched))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(plain.PDF, []byte("/AF [")) || bytes.Contains(plain.PDF, []byte("pdfaid")) {
		t.Error("zwykły PDF zawiera wpisy PDF/A-3")
	}
	if !bytes.Contains(plain.PDF, []byte("/EmbeddedFiles")) || !bytes.Contains(plain.PDF, []byte("/AFRelationship /Alternative")) {
		t.Error("zwykły PDF nie ma załączników")
	}

	verapdf(t, file, "3b")
}

func TestMissingAttachmentIsSkipped(t *testing.T) {
	result, err := Render(context.Background(), []byte(attachmentDocument), Options{Fonts: testFonts(t), Conformance: PDFA3b})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(result.PDF, []byte("/FileAttachment")) || bytes.Contains(result.PDF, []byte("/EmbeddedFiles")) {
		t.Error("niepobrany załącznik zostawił adnotację albo wpis w drzewie nazw")
	}
	if len(result.Warnings) == 0 {
		t.Error("brak ostrzeżenia o niepobranym załączniku")
	}
}

func TestAttachmentWithoutModTimeHasNoModDate(t *testing.T) {
	result, err := Render(context.Background(), []byte("<p>x</p>"), Options{Fonts: testFonts(t), Conformance: PDFA3b,
		Attachments: []Attachment{{Name: "a.xml", Data: []byte(invoiceXML), MediaType: "text/xml", Relationship: RelationshipData}}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(result.PDF, []byte("/ModDate")) {
		t.Error("załącznik bez ModTime dostał /ModDate")
	}
	if !bytes.Contains(result.PDF, []byte("/AFRelationship /Data")) {
		t.Error("brak /AFRelationship /Data")
	}
}

func TestPDFA2bRejectsNonPDFAAttachments(t *testing.T) {
	// załącznik z Options.Attachments
	_, err := Render(context.Background(), []byte("<p>x</p>"), Options{Fonts: testFonts(t), Conformance: PDFA2b,
		Attachments: []Attachment{{Name: "factur-x.xml", Data: []byte(invoiceXML), MediaType: "text/xml"}}})
	if !errors.Is(err, ErrAttachmentNotPDFA) {
		t.Errorf("PDF/A-2b z załącznikiem XML: błąd %v, oczekiwany ErrAttachmentNotPDFA", err)
	}
	// załącznik wskazany w HTML
	_, err = Render(context.Background(), []byte(attachmentDocument), Options{Fonts: testFonts(t), Conformance: PDFA2b,
		Resolver: attachmentResolver(map[string]int{})})
	if !errors.Is(err, ErrAttachmentNotPDFA) {
		t.Errorf("PDF/A-2b z <link rel=attachment>: błąd %v, oczekiwany ErrAttachmentNotPDFA", err)
	}
	// plik PDF/A-2b jako załącznik jest dozwolony
	inner, err := Render(context.Background(), []byte("<p>wewnętrzny</p>"), Options{Fonts: testFonts(t), Conformance: PDFA2b})
	if err != nil {
		t.Fatal(err)
	}
	outer, err := Render(context.Background(), []byte("<p>zewnętrzny</p>"), Options{Fonts: testFonts(t), Conformance: PDFA2b,
		Attachments: []Attachment{{Name: "wewnetrzny.pdf", Data: inner.PDF, MediaType: "application/pdf", Relationship: RelationshipSource}}})
	if err != nil {
		t.Fatalf("PDF/A-2b z załącznikiem PDF/A-2b: %v", err)
	}
	// zwykły PDF (bez deklaracji PDF/A) nie wystarcza
	plain, err := Render(context.Background(), []byte("<p>zwykły</p>"), Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Render(context.Background(), []byte("<p>x</p>"), Options{Fonts: testFonts(t), Conformance: PDFA2b,
		Attachments: []Attachment{{Name: "zwykly.pdf", Data: plain.PDF, MediaType: "application/pdf"}}})
	if !errors.Is(err, ErrAttachmentNotPDFA) {
		t.Errorf("PDF/A-2b z załącznikiem zwykły PDF: błąd %v, oczekiwany ErrAttachmentNotPDFA", err)
	}
	verapdf(t, outer.PDF, "2b")
}

func TestInvalidAttachments(t *testing.T) {
	for name, list := range map[string][]Attachment{
		"bez nazwy":        {{Data: []byte("x")}},
		"nazwa powtórzona": {{Name: "a.txt", Data: []byte("x")}, {Name: "a.txt", Data: []byte("y")}},
		"nieznany związek": {{Name: "a.txt", Data: []byte("x"), Relationship: "Parent"}},
	} {
		_, err := Render(context.Background(), []byte("<p>x</p>"), Options{Fonts: testFonts(t), Attachments: list})
		if !errors.Is(err, ErrInvalidAttachment) {
			t.Errorf("%s: błąd %v, oczekiwany ErrInvalidAttachment", name, err)
		}
	}
}

func TestDeclaresPDFA12(t *testing.T) {
	for data, want := range map[string]bool{
		"%PDF-1.7 <pdfaid:part>2</pdfaid:part>": true,
		`%PDF-1.4 pdfaid:part="1"`:              true,
		"%PDF-1.7 <pdfaid:part>3</pdfaid:part>": false,
		"%PDF-1.7 bez metadanych":               false,
		"<pdfaid:part>2</pdfaid:part>":          false,
	} {
		if got := declaresPDFA12([]byte(data)); got != want {
			t.Errorf("declaresPDFA12(%q) = %v, oczekiwane %v", data, got, want)
		}
	}
}

// verapdf sprawdza plik walidatorem veraPDF w podanym profilu; bez walidatora test jest pomijany.
func verapdf(t *testing.T, file []byte, flavour string) {
	t.Helper()
	if _, err := exec.LookPath("verapdf"); err != nil {
		t.Skipf("brak verapdf: zgodność z PDF/A-%s nie została sprawdzona walidatorem", flavour)
	}
	path := filepath.Join(t.TempDir(), "file.pdf")
	if err := os.WriteFile(path, file, 0o644); err != nil {
		t.Fatal(err)
	}
	report, _ := exec.Command("verapdf", "-f", flavour, "--format", "text", path).CombinedOutput()
	if !strings.Contains(string(report), "\nPASS ") && !strings.HasPrefix(string(report), "PASS ") {
		t.Errorf("veraPDF odrzuca plik jako PDF/A-%s:\n%s", flavour, report)
	}
	if strings.Contains(string(report), "WARNING") {
		t.Errorf("veraPDF zgłasza ostrzeżenia:\n%s", report)
	}
}
