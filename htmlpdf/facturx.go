package htmlpdf

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// FacturXLevel is the data profile of a Factur-X / ZUGFeRD invoice; the value is written verbatim to
// fx:ConformanceLevel in the XMP metadata (Factur-X 1.0.07 § 6.3.1, code list HybridConformanceType).
type FacturXLevel string

const (
	FacturXMinimum   FacturXLevel = "MINIMUM"
	FacturXBasicWL   FacturXLevel = "BASIC WL"
	FacturXBasic     FacturXLevel = "BASIC"
	FacturXEN16931   FacturXLevel = "EN 16931"
	FacturXExtended  FacturXLevel = "EXTENDED"
	FacturXXRechnung FacturXLevel = "XRECHNUNG"
)

// FacturX describes a Factur-X 1.0 / ZUGFeRD 2.x hybrid invoice (ADR 0034): the invoice XML in UN/CEFACT
// CII syntax embedded in a PDF/A-3 file with XMP metadata that mark the file as an invoice.
type FacturX struct {
	// XML is the content of the invoice: a UTF-8 encoded XML document with the root rsm:CrossIndustryInvoice
	// (namespace urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100); required.
	XML []byte
	// Level is the invoice profile. Empty means the profile read from the specification identifier (BT-24,
	// ram:GuidelineSpecifiedDocumentContextParameter/ram:ID); when it cannot be recognised, Render fails
	// with ErrInvalidFacturX. A profile that is given but differs from the XML gives a warning (BR-HYBRID-15).
	Level FacturXLevel
	// Relationship is the relationship of the XML to the document (/AFRelationship). Empty means the value from
	// the table in Factur-X § 6.2.2: Data for MINIMUM and BASIC WL, Alternative for the other profiles. Data,
	// Source and Alternative are allowed; a value outside the table for the profile gives a warning (BR-HYBRID-11).
	Relationship Relationship
	// Description is the description of the file (/Desc); empty means "Factur-X invoice (<profile>)".
	Description string
	// Version is fx:Version; empty means "1.0" (BR-HYBRID-10). The allowed values are the code list
	// HybridDocumentVersion: 1.0, 1p0, 2p0, 2p1, 2p2.
	Version string
	// ModTime is the modification date of the XML file (/Params /ModDate), recommended by § 6.2. Zero value:
	// the entry is omitted, so that the output is reproducible (ADR 0026).
	ModTime time.Time
}

// ErrInvalidFacturX: Options.FacturX with XML that is not a CII invoice, with an unknown profile or
// relationship, or combined with a standard other than PDF/A-3.
var ErrInvalidFacturX = errors.New("gopdfgo: invalid Factur-X invoice")

const (
	ciiNamespace = "urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100"
	ramNamespace = "urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100"
	// facturXNamespace to przestrzeń schematu rozszerzenia XMP; znak # na końcu jest wymagany (§ 6.3.1).
	facturXNamespace = "urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#"
)

// facturXLevelByGuideline wiąże identyfikator specyfikacji BT-24 z profilem (Factur-X 1.0.07,
// załącznik 1, BT-24; XRechnung: identyfikator KoSIT z numerem wersji, porównywany po przedrostku).
var facturXLevelByGuideline = map[string]FacturXLevel{
	"urn:factur-x.eu:1p0:minimum":                                     FacturXMinimum,
	"urn:factur-x.eu:1p0:basicwl":                                     FacturXBasicWL,
	"urn:cen.eu:en16931:2017#compliant#urn:factur-x.eu:1p0:basic":     FacturXBasic,
	"urn:cen.eu:en16931:2017":                                         FacturXEN16931,
	"urn:cen.eu:en16931:2017#conformant#urn:factur-x.eu:1p0:extended": FacturXExtended,
}

const xrechnungGuidelinePrefix = "urn:cen.eu:en16931:2017#compliant#urn:xeinkauf.de:kosit:xrechnung_"

// levelFromGuideline zwraca profil dla identyfikatora BT-24 albo "".
func levelFromGuideline(id string) FacturXLevel {
	if level, ok := facturXLevelByGuideline[id]; ok {
		return level
	}
	if strings.HasPrefix(id, xrechnungGuidelinePrefix) {
		return FacturXXRechnung
	}
	return ""
}

// fileName to nazwa osadzonego pliku: factur-x.xml, a dla profilu referencyjnego XRECHNUNG xrechnung.xml
// (§ 6.2, § 7.7, BR-HYBRID-13).
func (l FacturXLevel) fileName() string {
	if l == FacturXXRechnung {
		return "xrechnung.xml"
	}
	return "factur-x.xml"
}

// defaultRelationship to związek z tabeli § 6.2.2 wspólny dla Francji i Niemiec.
func (l FacturXLevel) defaultRelationship() Relationship {
	if l == FacturXMinimum || l == FacturXBasicWL {
		return RelationshipData
	}
	return RelationshipAlternative
}

// facturXPlan to sprawdzona faktura gotowa do osadzenia.
type facturXPlan struct {
	attachment Attachment
	level      FacturXLevel
	version    string
	warnings   []Warning
}

// planFacturX sprawdza Options.FacturX i ustala plik, profil i ostrzeżenia.
func planFacturX(invoice *FacturX) (facturXPlan, error) {
	guideline, err := parseCII(invoice.XML)
	if err != nil {
		return facturXPlan{}, err
	}
	var plan facturXPlan
	detected := levelFromGuideline(guideline)
	plan.level = invoice.Level
	switch plan.level {
	case "":
		if detected == "" {
			return facturXPlan{}, fmt.Errorf("%w: profile not given and not recognised from GuidelineSpecifiedDocumentContextParameter %q", ErrInvalidFacturX, guideline)
		}
		plan.level = detected
	case FacturXMinimum, FacturXBasicWL, FacturXBasic, FacturXEN16931, FacturXExtended, FacturXXRechnung:
		switch {
		case guideline == "":
			plan.warnings = append(plan.warnings, Warning{Message: "Factur-X: XML has no GuidelineSpecifiedDocumentContextParameter (BT-24)"})
		case detected == "":
			plan.warnings = append(plan.warnings, Warning{Message: fmt.Sprintf(
				"Factur-X: unknown specification identifier %q in XML, profile %s taken from options", guideline, plan.level)})
		case detected != plan.level:
			plan.warnings = append(plan.warnings, Warning{Message: fmt.Sprintf(
				"Factur-X: profile %s does not match the XML specification identifier %q (profile %s; BR-HYBRID-15)", plan.level, guideline, detected)})
		}
	default:
		return facturXPlan{}, fmt.Errorf("%w: unknown profile %q", ErrInvalidFacturX, invoice.Level)
	}

	relationship := invoice.Relationship
	switch relationship {
	case "":
		relationship = plan.level.defaultRelationship()
	case RelationshipData, RelationshipSource, RelationshipAlternative:
		allowed := relationship == plan.level.defaultRelationship() ||
			// Francja dopuszcza dla BASIC, EN 16931 i EXTENDED także Data i Source (§ 6.2.2, tabela)
			(plan.level == FacturXBasic || plan.level == FacturXEN16931 || plan.level == FacturXExtended)
		if !allowed {
			plan.warnings = append(plan.warnings, Warning{Message: fmt.Sprintf(
				"Factur-X: relationship %s is not the one specified for profile %s (%s; BR-HYBRID-11)", relationship, plan.level, plan.level.defaultRelationship())})
		}
	default:
		return facturXPlan{}, fmt.Errorf("%w: relationship %q (allowed: Data, Source, Alternative)", ErrInvalidFacturX, invoice.Relationship)
	}

	plan.version = invoice.Version
	switch plan.version {
	case "":
		plan.version = "1.0"
	case "1.0", "1p0", "2p0", "2p1", "2p2":
	default:
		return facturXPlan{}, fmt.Errorf("%w: version %q (allowed: 1.0, 1p0, 2p0, 2p1, 2p2)", ErrInvalidFacturX, invoice.Version)
	}

	description := invoice.Description
	if description == "" {
		description = "Factur-X invoice (" + string(plan.level) + ")"
	}
	plan.attachment = Attachment{
		Name:         plan.level.fileName(),
		Data:         invoice.XML,
		MediaType:    "text/xml", // § 6.2
		Description:  description,
		Relationship: relationship,
		ModTime:      invoice.ModTime,
	}
	return plan, nil
}

// parseCII sprawdza, że data to poprawny składniowo dokument XML z korzeniem rsm:CrossIndustryInvoice,
// i zwraca identyfikator specyfikacji BT-24 (pusty, gdy go nie ma).
func parseCII(data []byte) (string, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return "", fmt.Errorf("%w: empty XML", ErrInvalidFacturX)
	}
	// znacznik kolejności bajtów UTF-8 jest dozwolony przed deklaracją XML (XML 1.0 § 4.3.3); osadzamy
	// plik bez zmian, pomijamy go tylko przy czytaniu
	decoder := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	var (
		stack     []xml.Name
		guideline string
		inID      bool
		rootSeen  bool
	)
	// ścieżka BT-24 pod korzeniem
	path := []xml.Name{
		{Space: ciiNamespace, Local: "CrossIndustryInvoice"},
		{Space: ciiNamespace, Local: "ExchangedDocumentContext"},
		{Space: ramNamespace, Local: "GuidelineSpecifiedDocumentContextParameter"},
		{Space: ramNamespace, Local: "ID"},
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("%w: XML: %v", ErrInvalidFacturX, err)
		}
		switch t := token.(type) {
		case xml.StartElement:
			if len(stack) == 0 {
				if rootSeen {
					return "", fmt.Errorf("%w: XML has more than one root element", ErrInvalidFacturX)
				}
				rootSeen = true
				if t.Name != path[0] {
					return "", fmt.Errorf("%w: root element is {%s}%s, expected rsm:CrossIndustryInvoice in %s",
						ErrInvalidFacturX, t.Name.Space, t.Name.Local, ciiNamespace)
				}
			}
			stack = append(stack, t.Name)
			inID = guideline == "" && len(stack) == len(path) && namesEqual(stack, path)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
			inID = false
		case xml.CharData:
			if inID {
				guideline += string(t)
			}
		}
	}
	if !rootSeen {
		return "", fmt.Errorf("%w: XML has no root element", ErrInvalidFacturX)
	}
	return strings.TrimSpace(guideline), nil
}

func namesEqual(a, b []xml.Name) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// facturXConformance ustala normę PDF/A-3 dla faktury: Factur-X wymaga PDF/A-3 (§ 6, BR-HYBRID-02),
// poziom zgodności jest dowolny, a 3a zalecany. Bez podanej normy: 3a, gdy dokument i tak ma tagi,
// w przeciwnym razie 3b.
func facturXConformance(conformance Conformance, tagged bool) (Conformance, error) {
	switch conformance {
	case ConformanceNone:
		if tagged {
			return PDFA3a, nil
		}
		return PDFA3b, nil
	case PDFA3b, PDFA3a:
		return conformance, nil
	default:
		return "", fmt.Errorf("%w: requires PDF/A-3, not %s", ErrInvalidFacturX, conformance)
	}
}

// facturXDescription to opis właściwości fx w metadanych XMP (§ 6.3.1).
type facturXDescription struct {
	fileName, level, version string
}

// facturXExtensionSchema opisuje schemat fx w schemacie rozszerzeń PDF/A (ISO 19005-3 § 6.6.2.3,
// Factur-X § 6.3; nazwy, typy i opisy według przykładu XMP z pakietu specyfikacji).
const facturXExtensionSchema = `<rdf:li rdf:parseType="Resource">
<pdfaSchema:schema>Factur-X PDFA Extension Schema</pdfaSchema:schema>
<pdfaSchema:namespaceURI>urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#</pdfaSchema:namespaceURI>
<pdfaSchema:prefix>fx</pdfaSchema:prefix>
<pdfaSchema:property><rdf:Seq><rdf:li rdf:parseType="Resource">
<pdfaProperty:name>DocumentFileName</pdfaProperty:name>
<pdfaProperty:valueType>Text</pdfaProperty:valueType>
<pdfaProperty:category>external</pdfaProperty:category>
<pdfaProperty:description>name of the embedded XML invoice file</pdfaProperty:description>
</rdf:li><rdf:li rdf:parseType="Resource">
<pdfaProperty:name>DocumentType</pdfaProperty:name>
<pdfaProperty:valueType>Text</pdfaProperty:valueType>
<pdfaProperty:category>external</pdfaProperty:category>
<pdfaProperty:description>INVOICE</pdfaProperty:description>
</rdf:li><rdf:li rdf:parseType="Resource">
<pdfaProperty:name>Version</pdfaProperty:name>
<pdfaProperty:valueType>Text</pdfaProperty:valueType>
<pdfaProperty:category>external</pdfaProperty:category>
<pdfaProperty:description>The actual version of the Factur-X XML schema</pdfaProperty:description>
</rdf:li><rdf:li rdf:parseType="Resource">
<pdfaProperty:name>ConformanceLevel</pdfaProperty:name>
<pdfaProperty:valueType>Text</pdfaProperty:valueType>
<pdfaProperty:category>external</pdfaProperty:category>
<pdfaProperty:description>The conformance level of the embedded Factur-X data</pdfaProperty:description>
</rdf:li></rdf:Seq></pdfaSchema:property>
</rdf:li>`
