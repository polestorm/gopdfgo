package gopdfgo

import (
	"fmt"
	"github.com/polestorm/gopdfgo/internal/pdfa"
	"strings"
	"time"

	"github.com/polestorm/gopdfgo/internal/goweasyprint/pdf"
	pdfmodel "github.com/polestorm/gopdfgo/internal/pdflib/model"
)

// Conformance wybiera normę, z którą ma być zgodny plik wynikowy.
type Conformance string

const (
	// ConformanceNone: zwykły PDF 1.7 (domyślnie).
	ConformanceNone Conformance = ""
	// PDFA2b: PDF/A-2b (ISO 19005-2, poziom zgodności B), plik do długotrwałego przechowywania:
	// metadane XMP z identyfikatorem PDF/A, profil sRGB jako zamierzone wyjście, identyfikator pliku,
	// bez interpolacji obrazów (ADR 0016).
	PDFA2b Conformance = "PDF/A-2b"
	// PDFA3b: PDF/A-3b (ISO 19005-3, poziom B): to samo co PDFA2b, a ponadto załączniki dowolnego typu
	// jako pliki stowarzyszone z dokumentem (/AF katalogu, /AFRelationship; § 6.8), np. XML e-faktury
	// (ADR 0026).
	PDFA3b Conformance = "PDF/A-3b"
	// PDFA2a: PDF/A-2a (ISO 19005-2, poziom A): to samo co PDFA2b, a ponadto struktura logiczna (PDF
	// z tagami, ISO 19005-2 § 6.8, ADR 0033); włącza Options.Tagged.
	PDFA2a Conformance = "PDF/A-2a"
	// PDFA3a: PDF/A-3a (ISO 19005-3, poziom A): PDFA3b z tagami, jak PDFA2a.
	PDFA3a Conformance = "PDF/A-3a"
)

// tagged mówi, czy norma wymaga PDF z tagami.
func (c Conformance) tagged() bool { return c == PDFA2a || c == PDFA3a }

// Accessibility wybiera normę dostępności pliku wynikowego (ADR 0033). Jest niezależna od Conformance:
// PDF/UA i PDF/A to osobne normy, które jeden plik może spełniać naraz (np. PDF/A-2a i PDF/UA-1).
type Accessibility string

const (
	// AccessibilityNone: bez deklaracji dostępności (domyślnie).
	AccessibilityNone Accessibility = ""
	// PDFUA1: PDF/UA-1 (ISO 14289-1): PDF z tagami (włącza Options.Tagged), identyfikator pdfuaid:part 1
	// w metadanych XMP z tytułem dokumentu, a przy PDF/A także opis schematu pdfuaid w XMP (ISO 19005-2
	// § 6.6.2.3). Wady dokumentu, których silnik nie naprawia (brak tytułu, języka, tekstu
	// alternatywnego obrazu, przeskok poziomu nagłówka), dają ostrzeżenia.
	PDFUA1 Accessibility = "PDF/UA-1"
)

// tagged mówi, czy norma wymaga PDF z tagami.
func (a Accessibility) tagged() bool { return a == PDFUA1 }

// applyConformance uzupełnia dokument o to, czego wymaga wybrana norma PDF/A i norma dostępności;
// files to wszystkie pliki osadzone w dokumencie (pdf.Output.EmbeddedFiles).
func applyConformance(file *pdfmodel.Document, conformance Conformance, accessibility Accessibility, files []pdf.EmbeddedFile) error {
	ua := 0
	switch accessibility {
	case AccessibilityNone:
	case PDFUA1:
		ua = 1
	default:
		return fmt.Errorf("gopdfgo: nieznana norma dostępności %q", string(accessibility))
	}
	part, level := 2, "B"
	if conformance.tagged() {
		level = "A"
	}
	switch conformance {
	case ConformanceNone:
		if ua != 0 {
			// PDF/UA bez PDF/A: metadane XMP z identyfikatorem PDF/UA i tytułem (ISO 14289-1 § 5, § 7.1)
			file.Catalog.Metadata = xmpPacket(file.Trailer.Info, 0, "", ua)
		}
		return nil
	case PDFA3b, PDFA3a:
		part = 3
		// ISO 19005-3 § 6.8: każdy osadzony plik ma być stowarzyszony z dokumentem albo jego częścią
		// przez klucz AF. Pliki z <link rel=attachment> i Options.Attachments dotyczą całego dokumentu;
		// pliki adnotacji (<a rel=attachment>) też stowarzyszamy z dokumentem, bo PDF/A-3 nie wymaga
		// wskazania węższej części, a jedna tablica w katalogu wystarcza czytnikom e-faktur.
		file.Catalog.AF = nil
		for _, f := range files {
			file.Catalog.AF = append(file.Catalog.AF, f.Spec)
		}
		fallthrough
	case PDFA2b, PDFA2a:
		file.Trailer.ContentID = true
		file.Catalog.Metadata = xmpPacket(file.Trailer.Info, part, level, ua)
		file.Catalog.OutputIntents = []pdfmodel.OutputIntent{{
			S:                         "GTS_PDFA1",
			OutputConditionIdentifier: "sRGB IEC61966-2.1",
			Info:                      "sRGB IEC61966-2.1",
			DestOutputProfile:         pdfa.SRGBProfile(),
			N:                         3,
		}}
		return nil
	default:
		return fmt.Errorf("gopdfgo: nieznana zgodność %q", string(conformance))
	}
}

// xmpPacket buduje pakiet XMP z identyfikatorem PDF/A (part > 0) i PDF/UA (ua > 0) oraz tymi samymi
// danymi, które niesie słownik Info (ISO 19005-2 § 6.6.2.1, § 6.6.4; ISO 14289-1 § 5; ISO 16684-1).
// Pola puste są pomijane. Schemat pdfuaid nie należy do schematów predefiniowanych PDF/A, więc w pliku
// PDF/A ma opis w schemacie rozszerzeń pdfaExtension (ISO 19005-2 § 6.6.2.3.1).
func xmpPacket(info pdfmodel.Info, part int, level string, ua int) []byte {
	var b strings.Builder
	b.WriteString("<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n")
	b.WriteString(`<x:xmpmeta xmlns:x="adobe:ns:meta/">` + "\n")
	b.WriteString(`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` + "\n")
	if part > 0 && ua > 0 {
		b.WriteString(pdfuaExtensionSchema)
	}
	b.WriteString(`<rdf:Description rdf:about=""`)
	if part > 0 {
		b.WriteString(` xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"`)
	}
	if ua > 0 {
		b.WriteString(` xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/"`)
	}
	b.WriteString(` xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:pdf="http://ns.adobe.com/pdf/1.3/" xmlns:xmp="http://ns.adobe.com/xap/1.0/">` + "\n")
	if part > 0 {
		fmt.Fprintf(&b, "<pdfaid:part>%d</pdfaid:part>\n<pdfaid:conformance>%s</pdfaid:conformance>\n", part, level)
	}
	if ua > 0 {
		fmt.Fprintf(&b, "<pdfuaid:part>%d</pdfuaid:part>\n", ua)
	}
	if info.Title != "" {
		fmt.Fprintf(&b, "<dc:title><rdf:Alt><rdf:li xml:lang=\"x-default\">%s</rdf:li></rdf:Alt></dc:title>\n", xmlText(info.Title))
	}
	if info.Author != "" {
		fmt.Fprintf(&b, "<dc:creator><rdf:Seq><rdf:li>%s</rdf:li></rdf:Seq></dc:creator>\n", xmlText(info.Author))
	}
	if info.Subject != "" {
		fmt.Fprintf(&b, "<dc:description><rdf:Alt><rdf:li xml:lang=\"x-default\">%s</rdf:li></rdf:Alt></dc:description>\n", xmlText(info.Subject))
	}
	if info.Keywords != "" {
		fmt.Fprintf(&b, "<pdf:Keywords>%s</pdf:Keywords>\n", xmlText(info.Keywords))
	}
	if info.Producer != "" {
		fmt.Fprintf(&b, "<pdf:Producer>%s</pdf:Producer>\n", xmlText(info.Producer))
	}
	if info.Creator != "" {
		fmt.Fprintf(&b, "<xmp:CreatorTool>%s</xmp:CreatorTool>\n", xmlText(info.Creator))
	}
	if !info.CreationDate.IsZero() {
		fmt.Fprintf(&b, "<xmp:CreateDate>%s</xmp:CreateDate>\n", info.CreationDate.Format(time.RFC3339))
	}
	if !info.ModDate.IsZero() {
		fmt.Fprintf(&b, "<xmp:ModifyDate>%s</xmp:ModifyDate>\n", info.ModDate.Format(time.RFC3339))
	}
	b.WriteString("</rdf:Description>\n</rdf:RDF>\n</x:xmpmeta>\n<?xpacket end=\"r\"?>")
	return []byte(b.String())
}

func xmlText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// pdfuaExtensionSchema opisuje schemat pdfuaid dla PDF/A (ISO 19005-2 § 6.6.2.3; opis według
// ISO 14289-1 § 5 i przykładu PDF Association „PDF/UA Identification Schema”).
const pdfuaExtensionSchema = `<rdf:Description rdf:about="" xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/" xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#" xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#">
<pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">
<pdfaSchema:schema>PDF/UA Universal Accessibility Schema</pdfaSchema:schema>
<pdfaSchema:namespaceURI>http://www.aiim.org/pdfua/ns/id/</pdfaSchema:namespaceURI>
<pdfaSchema:prefix>pdfuaid</pdfaSchema:prefix>
<pdfaSchema:property><rdf:Seq><rdf:li rdf:parseType="Resource">
<pdfaProperty:name>part</pdfaProperty:name>
<pdfaProperty:valueType>Integer</pdfaProperty:valueType>
<pdfaProperty:category>internal</pdfaProperty:category>
<pdfaProperty:description>Indicates, which part of ISO 14289 standard is followed</pdfaProperty:description>
</rdf:li></rdf:Seq></pdfaSchema:property>
</rdf:li></rdf:Bag></pdfaExtension:schemas>
</rdf:Description>
`
