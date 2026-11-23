package document

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	mt "github.com/polestorm/gopdfgo/internal/webrender/matrix"
)

// gopdfgo: puste pola podpisu wskazane w HTML atrybutem data-pdf-signature (ADR 0051). Plik własny gopdfgo.
//
// Element z atrybutem dostaje w PDF pole podpisu (/FT /Sig) bez wartości, z widżetem na prostokącie
// obramowania swojego pierwszego pudełka w dokumencie (pierwsza strona, pierwszy fragment). Treść
// elementu jest rysowana na stronie jak zwykle; pole to tylko miejsce, które podpis wypełni później
// (sign.SignPDF z Options.FieldName albo dowolny program do podpisu). Kod kryptograficzny tu nie trafia.

// SignatureAttribute to atrybut HTML, którego wartość jest nazwą pola podpisu (/T).
const SignatureAttribute = "data-pdf-signature"

// signatureField to pole podpisu zebrane z pudełka strony.
type signatureField struct {
	name        string
	description string // /TU: aria-label, title, tekst elementu albo nazwa
	rectangle   [4]fl  // [x_min, y_min, x_max, y_max] w pikselach CSS od lewego górnego rogu strony
	element     *html.Node
}

// gatherSignatureField dopisuje pole podpisu, gdy pudełko jest pudełkiem elementu z atrybutem
// SignatureAttribute (bez pseudoelementów, tekstu i wierszy, które dziedziczą element po rodzicu).
func gatherSignatureField(box_ bo.Box, fields *[]signatureField, matrix *mt.Transform) {
	box := box_.Box()
	if box.Element == nil || box.PseudoType != "" || bo.TextT.IsInstance(box_) || bo.LineT.IsInstance(box_) {
		return
	}
	name, has := attribute(box.Element, SignatureAttribute)
	if !has {
		return
	}
	posX, posY, width, height := bo.HitArea(box_).Unpack()
	field := signatureField{name: strings.TrimSpace(name), element: box.Element}
	if matrix != nil {
		field.rectangle = rectangleAabb(*matrix, posX, posY, width, height)
	} else {
		field.rectangle = [4]fl{posX, posY, posX + width, posY + height}
	}
	*fields = append(*fields, field)
}

// resolveSignatureFields wybiera pola stron: element ma jedno pole, na pierwszej stronie i w pierwszym
// fragmencie (kolejność pudełek w drzewie); nazwa pusta, z kropką (ISO 32000-1 § 12.7.3.2: nazwa
// częściowa nie zawiera kropki) albo powtórzona daje ostrzeżenie i pole jest pomijane.
func resolveSignatureFields(pages []Page) [][]signatureField {
	out := make([][]signatureField, len(pages))
	elements := map[*html.Node]bool{}
	names := map[string]bool{}
	for i, page := range pages {
		for _, field := range page.signatureFields {
			if elements[field.element] {
				continue
			}
			elements[field.element] = true
			switch {
			case field.name == "":
				logger.WarningLogger.Printf("Signature field without a name (%s attribute is empty), skipped\n", SignatureAttribute)
				continue
			case strings.Contains(field.name, "."):
				logger.WarningLogger.Printf("Signature field name %q contains a period, skipped\n", field.name)
				continue
			case names[field.name]:
				logger.WarningLogger.Printf("Duplicate signature field name %q, only the first one is kept\n", field.name)
				continue
			}
			names[field.name] = true
			field.description = signatureDescription(field.element, field.name)
			out[i] = append(out[i], field)
		}
	}
	return out
}

// signatureDescription: opis pola (/TU, ISO 32000-1 § 12.7.3.1; PDF/UA-1 § 7.18.4 wymaga /TU albo
// /Alt): aria-label, title, tekst elementu, a bez nich nazwa pola.
func signatureDescription(node *html.Node, name string) string {
	for _, key := range []string{"aria-label", "title"} {
		if v, _ := attribute(node, key); strings.TrimSpace(v) != "" {
			return strings.Join(strings.Fields(v), " ")
		}
	}
	if text := strings.Join(strings.Fields(textContent(node)), " "); text != "" {
		return text
	}
	return name
}

// addSignatureFields dodaje pola podpisu strony za jej odnośnikami.
func (d Document) addSignatureFields(fields []signatureField, context backend.Page, scale mt.Transform) {
	if len(fields) == 0 {
		return
	}
	target, ok := context.(backend.SignatureFieldPage)
	if !ok {
		return
	}
	tagged, _ := context.(backend.TaggedPage)
	for _, field := range fields {
		// widżet należy do elementu Form w elemencie struktury pola (ADR 0051, PDF/UA-1 § 7.18.4)
		if d.tags != nil && tagged != nil && tagged.Tagged() {
			tagged.SetAnnotationOwner(contentOwner(field.element), "")
		}
		xMin, yMin := scale.Apply(field.rectangle[0], field.rectangle[1])
		xMax, yMax := scale.Apply(field.rectangle[2], field.rectangle[3])
		target.AddSignatureField(xMin, yMin, xMax, yMax, field.name, field.description)
	}
}
