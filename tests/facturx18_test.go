package tests

import (
	"bytes"
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/polestorm/gopdfgo"
)

// Faktura hybrydowa Factur-X 1.0 / ZUGFeRD 2.x (ADR 0034): XML CII osadzony w PDF/A-3 ze związkiem
// według profilu, metadane XMP fx z opisem schematu rozszerzenia, sprawdzenie wejścia.

const facturXDocument = "../testdata/documents/20-invoice.html"

func facturXMinimumXML(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../testdata/facturx/20-invoice-minimum.xml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// facturXWithGuideline zamienia identyfikator specyfikacji BT-24 w fakturze MINIMUM.
func facturXWithGuideline(t *testing.T, guideline string) []byte {
	t.Helper()
	return bytes.Replace(facturXMinimumXML(t), []byte("urn:factur-x.eu:1p0:minimum"), []byte(guideline), 1)
}

// renderFacturX renderuje fakturę z korpusu z zapisem nieskompresowanym (słowniki specyfikacji plików
// czytelne także w strumieniach obiektów pliku z tagami).
func renderFacturX(t *testing.T, invoice gopdfgo.FacturX, adjust func(*gopdfgo.Options)) gopdfgo.Result {
	t.Helper()
	result, err := renderFileOptions(facturXDocument, "../testdata", "", func(o *gopdfgo.Options) {
		o.Uncompressed = true
		o.FacturX = &invoice
		if adjust != nil {
			adjust(o)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// xmpOf zwraca pakiet XMP z pliku (strumień metadanych PDF/A nie jest filtrowany).
func xmpOf(t *testing.T, pdf []byte) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<\?xpacket begin=.*?<\?xpacket end="r"\?>`).Find(pdf)
	if m == nil {
		t.Fatal("brak pakietu XMP")
	}
	return string(m)
}

func requireAll(t *testing.T, where string, text string, needles ...string) {
	t.Helper()
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			t.Errorf("%s: brak %q", where, needle)
		}
	}
}

func facturXWarnings(result gopdfgo.Result) []string {
	var out []string
	for _, w := range result.Warnings {
		if strings.HasPrefix(w.Message, "Factur-X") {
			out = append(out, w.Message)
		}
	}
	return out
}

func TestFacturXMinimum(t *testing.T) {
	invoice := gopdfgo.FacturX{XML: facturXMinimumXML(t), ModTime: time.Date(2026, 11, 14, 9, 30, 0, 0, time.UTC)}
	first := renderFacturX(t, invoice, nil)
	second := renderFacturX(t, invoice, nil)
	if !bytes.Equal(first.PDF, second.PDF) {
		t.Error("dwa przebiegi z fakturą dały różne bajty")
	}
	if w := facturXWarnings(first); len(w) != 0 {
		t.Errorf("nieoczekiwane ostrzeżenia: %q", w)
	}
	xmp := xmpOf(t, first.PDF)
	requireAll(t, "XMP", xmp,
		"<pdfaid:part>3</pdfaid:part>", "<pdfaid:conformance>B</pdfaid:conformance>",
		`<rdf:Description rdf:about="" xmlns:fx="urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#">`,
		"<fx:DocumentType>INVOICE</fx:DocumentType>", "<fx:DocumentFileName>factur-x.xml</fx:DocumentFileName>",
		"<fx:Version>1.0</fx:Version>", "<fx:ConformanceLevel>MINIMUM</fx:ConformanceLevel>",
		// opis schematu rozszerzenia (Factur-X § 6.3, ISO 19005-3 § 6.6.2.3)
		"<pdfaSchema:schema>Factur-X PDFA Extension Schema</pdfaSchema:schema>",
		"<pdfaSchema:namespaceURI>urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#</pdfaSchema:namespaceURI>",
		"<pdfaSchema:prefix>fx</pdfaSchema:prefix>",
		"<pdfaProperty:name>DocumentFileName</pdfaProperty:name>", "<pdfaProperty:name>DocumentType</pdfaProperty:name>",
		"<pdfaProperty:name>Version</pdfaProperty:name>", "<pdfaProperty:name>ConformanceLevel</pdfaProperty:name>")
	if n := strings.Count(xmp, "<pdfaProperty:valueType>Text</pdfaProperty:valueType>\n<pdfaProperty:category>external</pdfaProperty:category>"); n != 4 {
		t.Errorf("właściwości fx typu Text, external: %d, oczekiwane 4", n)
	}
	if strings.Contains(xmp, "pdfuaid") {
		t.Error("XMP faktury bez PDF/UA zawiera pdfuaid")
	}

	file := string(first.PDF)
	requireAll(t, "plik", file,
		"/UF (factur-x.xml) /F (factur-x.xml)", "/AFRelationship /Data", "/Subtype /text#2Fxml",
		"/Desc (Factur-X invoice \\(MINIMUM\\))", "/ModDate (D:20261114093000+00'00')")
	// /AF katalogu wskazuje specyfikację pliku faktury
	spec := regexp.MustCompile(`(?s)(\d+) 0 obj\n<<[^\n]*/UF \(factur-x\.xml\)`).FindStringSubmatch(file)
	if spec == nil {
		t.Fatal("brak specyfikacji pliku factur-x.xml jako osobnego obiektu")
	}
	if !regexp.MustCompile(`/AF \[` + spec[1] + ` 0 R\]`).MatchString(file) {
		t.Errorf("/AF katalogu nie wskazuje specyfikacji pliku %s 0 R", spec[1])
	}
	// treść XML osadzona bez zmian (strumień nieskompresowany)
	if !strings.Contains(file, string(facturXMinimumXML(t))) {
		t.Error("treść XML faktury nie została osadzona bez zmian")
	}

	verapdf(t, first.PDF, "3b")
}

func TestFacturXLevelsFromXML(t *testing.T) {
	for _, c := range []struct {
		guideline    string
		level        string
		fileName     string
		relationship string
	}{
		{"urn:factur-x.eu:1p0:minimum", "MINIMUM", "factur-x.xml", "Data"},
		{"urn:factur-x.eu:1p0:basicwl", "BASIC WL", "factur-x.xml", "Data"},
		{"urn:cen.eu:en16931:2017#compliant#urn:factur-x.eu:1p0:basic", "BASIC", "factur-x.xml", "Alternative"},
		{"urn:cen.eu:en16931:2017", "EN 16931", "factur-x.xml", "Alternative"},
		{"urn:cen.eu:en16931:2017#conformant#urn:factur-x.eu:1p0:extended", "EXTENDED", "factur-x.xml", "Alternative"},
		{"urn:cen.eu:en16931:2017#compliant#urn:xeinkauf.de:kosit:xrechnung_3.0", "XRECHNUNG", "xrechnung.xml", "Alternative"},
	} {
		result, err := gopdfgo.Render(context.Background(), []byte("<p>Faktura</p>"), gopdfgo.Options{Fonts: testFonts(t),
			Uncompressed: true, FacturX: &gopdfgo.FacturX{XML: facturXWithGuideline(t, "  "+c.guideline+"\n")}})
		if err != nil {
			t.Errorf("%s: %v", c.level, err)
			continue
		}
		if w := facturXWarnings(result); len(w) != 0 {
			t.Errorf("%s: nieoczekiwane ostrzeżenia: %q", c.level, w)
		}
		requireAll(t, c.level, xmpOf(t, result.PDF), "<fx:ConformanceLevel>"+c.level+"</fx:ConformanceLevel>",
			"<fx:DocumentFileName>"+c.fileName+"</fx:DocumentFileName>")
		requireAll(t, c.level, string(result.PDF), "/UF ("+c.fileName+") /F ("+c.fileName+")", "/AFRelationship /"+c.relationship)
	}
}

func TestFacturXLevelMismatchWarns(t *testing.T) {
	result := renderFacturX(t, gopdfgo.FacturX{XML: facturXMinimumXML(t), Level: gopdfgo.FacturXEN16931}, nil)
	warnings := facturXWarnings(result)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "BR-HYBRID-15") {
		t.Errorf("ostrzeżenia: %q, oczekiwane jedno o niezgodnym profilu (BR-HYBRID-15)", warnings)
	}
	// podany profil wygrywa: metadane i związek według EN 16931
	requireAll(t, "XMP", xmpOf(t, result.PDF), "<fx:ConformanceLevel>EN 16931</fx:ConformanceLevel>")
	requireAll(t, "plik", string(result.PDF), "/AFRelationship /Alternative")

	// związek spoza tabeli § 6.2.2 dla profilu: ostrzeżenie, nie błąd
	result = renderFacturX(t, gopdfgo.FacturX{XML: facturXMinimumXML(t), Relationship: gopdfgo.RelationshipAlternative}, nil)
	warnings = facturXWarnings(result)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "BR-HYBRID-11") {
		t.Errorf("ostrzeżenia: %q, oczekiwane jedno o związku (BR-HYBRID-11)", warnings)
	}
	// Francja dopuszcza Source dla EN 16931: bez ostrzeżenia
	result = renderFacturX(t, gopdfgo.FacturX{XML: facturXWithGuideline(t, "urn:cen.eu:en16931:2017"), Relationship: gopdfgo.RelationshipSource}, nil)
	if w := facturXWarnings(result); len(w) != 0 {
		t.Errorf("EN 16931 ze związkiem Source: ostrzeżenia %q", w)
	}
}

func TestFacturXTaggedAndUA(t *testing.T) {
	invoice := gopdfgo.FacturX{XML: facturXWithGuideline(t, "urn:cen.eu:en16931:2017")}
	// PDF z tagami bez podanej normy: PDF/A-3a
	tagged := renderFacturX(t, invoice, func(o *gopdfgo.Options) { o.Tagged = true })
	requireAll(t, "XMP 3a", xmpOf(t, tagged.PDF), "<pdfaid:part>3</pdfaid:part>", "<pdfaid:conformance>A</pdfaid:conformance>",
		"<fx:ConformanceLevel>EN 16931</fx:ConformanceLevel>")

	// PDF/A-3a z PDF/UA-1: oba schematy w jednym bloku rozszerzeń
	ua := renderFacturX(t, invoice, func(o *gopdfgo.Options) { o.Accessibility = gopdfgo.PDFUA1 })
	xmp := xmpOf(t, ua.PDF)
	requireAll(t, "XMP 3a+UA", xmp, "<pdfaid:conformance>A</pdfaid:conformance>", "<pdfuaid:part>1</pdfuaid:part>",
		"<pdfaSchema:prefix>pdfuaid</pdfaSchema:prefix>", "<pdfaSchema:prefix>fx</pdfaSchema:prefix>")
	if n := strings.Count(xmp, "<pdfaExtension:schemas>"); n != 1 {
		t.Errorf("bloków pdfaExtension:schemas: %d, oczekiwany 1", n)
	}
	requireAll(t, "plik 3a+UA", string(ua.PDF), "/AFRelationship /Alternative", "/UF (factur-x.xml) /F (factur-x.xml)")

	verapdf(t, tagged.PDF, "3a")
	verapdf(t, ua.PDF, "3a")
	verapdf(t, ua.PDF, "ua1")
}

func TestFacturXRejectsInvalidInput(t *testing.T) {
	minimum := facturXMinimumXML(t)
	for name, c := range map[string]struct {
		invoice gopdfgo.FacturX
		adjust  func(*gopdfgo.Options)
	}{
		"pusty XML":        {invoice: gopdfgo.FacturX{XML: []byte(" \n")}},
		"nie XML":          {invoice: gopdfgo.FacturX{XML: []byte("faktura FV/1")}},
		"XML niedomknięty": {invoice: gopdfgo.FacturX{XML: minimum[:len(minimum)-30]}},
		"korzeń UBL":       {invoice: gopdfgo.FacturX{XML: []byte(`<Invoice xmlns="urn:oasis:names:specification:ubl:schema:xsd:Invoice-2"><ID>1</ID></Invoice>`)}},
		"CrossIndustryInvoice spoza przestrzeni CII": {invoice: gopdfgo.FacturX{XML: []byte(`<CrossIndustryInvoice xmlns="urn:ferd:CrossIndustryDocument:invoice:1p0"/>`)}},
		"profil nierozpoznany":                       {invoice: gopdfgo.FacturX{XML: facturXWithGuideline(t, "urn:example:profile")}},
		"nieznany profil":                            {invoice: gopdfgo.FacturX{XML: minimum, Level: "COMFORT"}},
		"związek Supplement":                         {invoice: gopdfgo.FacturX{XML: minimum, Relationship: gopdfgo.RelationshipSupplement}},
		"kodowanie ISO-8859-2":                       {invoice: gopdfgo.FacturX{XML: bytes.Replace(minimum, []byte(`encoding="UTF-8"`), []byte(`encoding="ISO-8859-2"`), 1)}},
		"nieznana wersja":                            {invoice: gopdfgo.FacturX{XML: minimum, Version: "2.3"}},
		"PDF/A-2b":                                   {invoice: gopdfgo.FacturX{XML: minimum}, adjust: func(o *gopdfgo.Options) { o.Conformance = gopdfgo.PDFA2b }},
		"PDF/A-2a":                                   {invoice: gopdfgo.FacturX{XML: minimum}, adjust: func(o *gopdfgo.Options) { o.Conformance = gopdfgo.PDFA2a }},
	} {
		invoice := c.invoice
		opts := gopdfgo.Options{Fonts: testFonts(t), FacturX: &invoice}
		if c.adjust != nil {
			c.adjust(&opts)
		}
		_, err := gopdfgo.Render(context.Background(), []byte("<p>x</p>"), opts)
		if !errors.Is(err, gopdfgo.ErrInvalidFacturX) {
			t.Errorf("%s: błąd %v, oczekiwany ErrInvalidFacturX", name, err)
		}
	}

	// załącznik o nazwie pliku faktury
	_, err := gopdfgo.Render(context.Background(), []byte("<p>x</p>"), gopdfgo.Options{Fonts: testFonts(t),
		FacturX:     &gopdfgo.FacturX{XML: minimum},
		Attachments: []gopdfgo.Attachment{{Name: "factur-x.xml", Data: minimum}}})
	if !errors.Is(err, gopdfgo.ErrInvalidAttachment) {
		t.Errorf("załącznik factur-x.xml obok faktury: błąd %v, oczekiwany ErrInvalidAttachment", err)
	}
	// plik o nazwie faktury wskazany w HTML
	html := `<html><head><link rel="attachment" href="https://example.test/factur-x.xml"></head><body><p>x</p></body></html>`
	_, err = gopdfgo.Render(context.Background(), []byte(html), gopdfgo.Options{Fonts: testFonts(t),
		FacturX: &gopdfgo.FacturX{XML: minimum},
		Resolver: func(string) (gopdfgo.Resource, error) {
			return gopdfgo.Resource{Data: []byte("<x/>"), MediaType: "text/xml"}, nil
		}})
	if !errors.Is(err, gopdfgo.ErrInvalidFacturX) {
		t.Errorf("<link rel=attachment> o nazwie factur-x.xml: błąd %v, oczekiwany ErrInvalidFacturX", err)
	}
}

func TestFacturXAcceptsByteOrderMark(t *testing.T) {
	data := append([]byte("\xef\xbb\xbf"), facturXMinimumXML(t)...)
	result, err := gopdfgo.Render(context.Background(), []byte("<p>x</p>"), gopdfgo.Options{Fonts: testFonts(t),
		FacturX: &gopdfgo.FacturX{XML: data}})
	if err != nil {
		t.Fatal(err)
	}
	requireAll(t, "XMP", xmpOf(t, result.PDF), "<fx:ConformanceLevel>MINIMUM</fx:ConformanceLevel>")
}

// Opcja nie zmienia wywołującemu Options.Attachments ani pliku bez faktury.
func TestFacturXKeepsCallerAttachments(t *testing.T) {
	attachments := []gopdfgo.Attachment{{Name: "dane.txt", Data: []byte("x"), Relationship: gopdfgo.RelationshipSupplement}}
	opts := gopdfgo.Options{Fonts: testFonts(t), Uncompressed: true, Attachments: attachments,
		FacturX: &gopdfgo.FacturX{XML: facturXMinimumXML(t)}}
	result, err := gopdfgo.Render(context.Background(), []byte("<p>x</p>"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.Attachments) != 1 || opts.Conformance != gopdfgo.ConformanceNone {
		t.Error("Render zmienił opcje wywołującego")
	}
	if !regexp.MustCompile(`/AF \[\d+ 0 R \d+ 0 R\]`).Match(result.PDF) {
		t.Error("/AF katalogu bez dwóch plików (faktura i dane.txt)")
	}
	verapdf(t, result.PDF, "3b")
}
