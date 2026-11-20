package gopdfgo

import (
	"encoding/binary"
	"fmt"
	"math"
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
)

// applyConformance uzupełnia dokument o to, czego wymaga wybrana norma; files to wszystkie pliki
// osadzone w dokumencie (pdf.Output.EmbeddedFiles).
func applyConformance(file *pdfmodel.Document, conformance Conformance, files []pdf.EmbeddedFile) error {
	part := 2
	switch conformance {
	case ConformanceNone:
		return nil
	case PDFA3b:
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
	case PDFA2b:
		file.Trailer.ContentID = true
		file.Catalog.Metadata = xmpPacket(file.Trailer.Info, part, "B")
		file.Catalog.OutputIntents = []pdfmodel.OutputIntent{{
			S:                         "GTS_PDFA1",
			OutputConditionIdentifier: "sRGB IEC61966-2.1",
			Info:                      "sRGB IEC61966-2.1",
			DestOutputProfile:         srgbProfile(),
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

// srgbProfile buduje minimalny profil ICC w wersji 2.1 dla przestrzeni sRGB (IEC 61966-2-1): profil
// monitora typu macierz i krzywa, z barwami podstawowymi przeliczonymi do punktu bieli D50 i jedną
// krzywą przenoszenia wspólną dla trzech kanałów. Profil powstaje z liczb z normy, więc nie niesie
// żadnej obcej licencji; wynik jest zawsze ten sam.
func srgbProfile() []byte {
	fixed := func(v float64) []byte { // s15Fixed16Number
		out := make([]byte, 4)
		binary.BigEndian.PutUint32(out, uint32(int32(math.Round(v*65536))))
		return out
	}
	xyz := func(x, y, z float64) []byte {
		out := append([]byte("XYZ \x00\x00\x00\x00"), fixed(x)...)
		return append(append(out, fixed(y)...), fixed(z)...)
	}
	// krzywa sRGB próbkowana w 1024 punktach
	const samples = 1024
	curve := append([]byte("curv\x00\x00\x00\x00"), 0, 0, samples>>8, samples&0xff)
	for i := range samples {
		v := float64(i) / (samples - 1)
		linear := v / 12.92
		if v > 0.04045 {
			linear = math.Pow((v+0.055)/1.055, 2.4)
		}
		value := uint16(math.Round(linear * 65535))
		curve = append(curve, byte(value>>8), byte(value))
	}
	description := "sRGB IEC61966-2.1"
	desc := append([]byte("desc\x00\x00\x00\x00"), 0, 0, 0, byte(len(description)+1))
	desc = append(desc, description...)
	desc = append(desc, make([]byte, 1+4+4+2+1+67)...) // koniec napisu, puste opisy Unicode i ScriptCode
	text := append([]byte("text\x00\x00\x00\x00"), "No copyright, use freely\x00"...)

	type tag struct {
		signature string
		data      []byte
	}
	tags := []tag{
		{"desc", desc},
		{"cprt", text},
		{"wtpt", xyz(0.9642, 1.0, 0.8249)},
		{"rXYZ", xyz(0.4360747, 0.2225045, 0.0139322)},
		{"gXYZ", xyz(0.3850649, 0.7168786, 0.0971045)},
		{"bXYZ", xyz(0.1430804, 0.0606169, 0.7141733)},
		{"rTRC", curve},
		{"gTRC", nil}, // ta sama krzywa co rTRC
		{"bTRC", nil},
	}

	const headerSize = 128
	offset := headerSize + 4 + 12*len(tags)
	table := make([]byte, 4, 4+12*len(tags))
	binary.BigEndian.PutUint32(table, uint32(len(tags)))
	var body []byte
	var curveOffset, curveSize int
	for _, t := range tags {
		entry := make([]byte, 12)
		copy(entry, t.signature)
		if t.data == nil {
			binary.BigEndian.PutUint32(entry[4:], uint32(curveOffset))
			binary.BigEndian.PutUint32(entry[8:], uint32(curveSize))
		} else {
			if t.signature == "rTRC" {
				curveOffset, curveSize = offset, len(t.data)
			}
			binary.BigEndian.PutUint32(entry[4:], uint32(offset))
			binary.BigEndian.PutUint32(entry[8:], uint32(len(t.data)))
			padded := append([]byte(nil), t.data...)
			for len(padded)%4 != 0 {
				padded = append(padded, 0)
			}
			body = append(body, padded...)
			offset += len(padded)
		}
		table = append(table, entry...)
	}

	header := make([]byte, headerSize)
	binary.BigEndian.PutUint32(header[0:], uint32(offset)) // rozmiar profilu
	binary.BigEndian.PutUint32(header[8:], 0x02100000)     // wersja 2.1
	copy(header[12:], "mntr")                              // klasa: monitor
	copy(header[16:], "RGB ")                              // przestrzeń danych
	copy(header[20:], "XYZ ")                              // przestrzeń połączenia profili
	// data utworzenia: stała, żeby profil był zawsze ten sam (2026-01-01 00:00:00)
	binary.BigEndian.PutUint16(header[24:], 2026)
	binary.BigEndian.PutUint16(header[26:], 1)
	binary.BigEndian.PutUint16(header[28:], 1)
	copy(header[36:], "acsp")
	copy(header[68:], fixed(0.9642)) // oświetlenie przestrzeni połączenia: D50
	copy(header[72:], fixed(1.0))
	copy(header[76:], fixed(0.8249))
	return append(append(header, table...), body...)
}
