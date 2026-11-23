package pdf

import (
	"github.com/polestorm/gopdfgo/internal/pdflib/model"
	"github.com/polestorm/gopdfgo/internal/webrender/backend"
)

// gopdfgo: puste pola podpisu (ADR 0051). Plik własny gopdfgo.

var _ backend.SignatureFieldPage = (*outputPage)(nil)

// AddSignatureField (backend.SignatureFieldPage) dodaje pole podpisu bez wartości: pole i widżet
// w jednym słowniku (ISO 32000-1 § 12.7.3.1), /FT /Sig, /T i /TU, flaga Print (ISO 19005-2 § 6.3.2)
// i wygląd /AP /N jako pusta forma (§ 6.3.3: widżet o niezerowym prostokącie ma mieć wygląd; treść
// elementu jest już narysowana na stronie, więc forma nic nie dorysowuje). /SigFlags zostaje
// nieustawione: pole bez podpisu nie ustawia SignaturesExist (tabela 219); ustawia je podpis.
func (cp *outputPage) AddSignatureField(xMin, yMin, xMax, yMax fl, name, description string) {
	rect := model.Rectangle{Llx: min(xMin, xMax), Lly: min(yMin, yMax), Urx: max(xMin, xMax), Ury: max(yMin, yMax)}
	widget := &model.AnnotationDict{
		BaseAnnotation: model.BaseAnnotation{
			Rect: rect,
			F:    model.APrint,
			AP: &model.AppearanceDict{
				N: model.AppearanceEntry{"": &model.XObjectForm{
					ContentStream: model.ContentStream{Stream: model.Stream{Content: []byte{}}},
					BBox:          model.Rectangle{Urx: rect.Urx - rect.Llx, Ury: rect.Ury - rect.Lly},
				}},
			},
		},
		Subtype: model.AnnotationWidget{},
	}
	cp.addAnnotation(widget) // z elementem Form dla PDF z tagami (tagged.go)
	cp.signatureFields = append(cp.signatureFields, &model.FormFieldDict{
		FormFieldInheritable: model.FormFieldInheritable{FT: model.FormFieldSignature{}},
		T:                    name,
		TU:                   description,
		Widgets:              []model.FormFieldWidget{{AnnotationDict: widget}},
	})
}
