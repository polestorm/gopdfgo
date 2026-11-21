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

// applyConformance uzupełnia dokument o to, czego wymaga wybrana norma; files to wszystkie pliki
// osadzone w dokumencie (pdf.Output.EmbeddedFiles).
func applyConformance(file *pdfmodel.Document, conformance Conformance, files []pdf.EmbeddedFile) error {
	part, level := 2, "B"
	if conformance.tagged() {
		level = "A"
	}
	switch conformance {
	case ConformanceNone:
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
		file.Catalog.Metadata = xmpPacket(file.Trailer.Info, part, level)
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

// xmpPacket buduje pakiet XMP z identyfikatorem PDF/A i tymi samymi danymi, które niesie słownik Info
// (ISO 19005-2 § 6.6.2.1, § 6.6.4; ISO 16684-1). Pola puste są pomijane.
func xmpPacket(info pdfmodel.Info, part int, level string) []byte {
	var b strings.Builder
	b.WriteString("<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n")
	b.WriteString(`<x:xmpmeta xmlns:x="adobe:ns:meta/">` + "\n")
	b.WriteString(`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` + "\n")
	b.WriteString(`<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:pdf="http://ns.adobe.com/pdf/1.3/" xmlns:xmp="http://ns.adobe.com/xap/1.0/">` + "\n")
	fmt.Fprintf(&b, "<pdfaid:part>%d</pdfaid:part>\n<pdfaid:conformance>%s</pdfaid:conformance>\n", part, level)
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
