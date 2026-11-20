package pdf

import (
	"math"

	cs "github.com/polestorm/gopdfgo/internal/pdflib/contentstream"

	"github.com/polestorm/gopdfgo/internal/pdflib/model"
	"github.com/polestorm/gopdfgo/internal/webrender/backend"
)

// gopdfgo: rozszerzenie backend.FilterCanvas dla wektorowych filtrów SVG (cień, rozmycie)

var _ backend.FilterCanvas = (*group)(nil)

// DrawSharedGroup rysuje grupę jako izolowaną grupę przezroczystości; obiekt XObject powstaje przy
// pierwszym wywołaniu i jest wskazywany przez wszystkie następne, więc wiele kopii tej samej treści
// (sylwetka rozmytego cienia) kosztuje w pliku tylko operatory Do.
func (g *group) DrawSharedGroup(opacity fl, gr backend.Canvas) {
	other := gr.(*group)
	if other.shared == nil {
		content := other.stream.ToGroupXFormObject(compressStreams) // gopdfgo: wzorce w formie pośredniej (zob. ToGroupXFormObject)
		other.shared = &model.XObjectTransparencyGroup{
			XObjectForm: *content,
			Group:       model.TransparencyGroup{CS: model.ColorSpaceRGB, I: true},
		}
	}
	// grupę rysowaną przez Do składa się ze stałą alfa wypełnienia ca (PDF 32000-1 § 11.6.4.4), więc
	// CA pomijamy: kopie rozmycia mają wiele różnych krycia i każde to osobny słownik ExtGState
	g.stream.SetFillAlpha(opacity)
	g.stream.AddXObject(other.shared)
}

// SetSoftMask ustawia maskę miękką /Alpha albo /Luminosity z funkcją przejścia /TR zapisaną jako
// funkcja próbkowana typu 0 (8 bitów na próbkę, PDF 32000-1 § 7.10.2, § 11.6.5.2).
func (g *group) SetSoftMask(mask backend.Canvas, alpha bool, transfer []fl) {
	form := mask.(*group).stream.ToGroupXFormObject(compressStreams) // gopdfgo: jw.
	kind := "Luminosity"
	if alpha {
		kind = "Alpha"
	}
	state := model.GraphicState{
		SMask: model.SoftMaskDict{
			S: model.ObjName(kind),
			G: &model.XObjectTransparencyGroup{
				XObjectForm: *form,
				Group:       model.TransparencyGroup{CS: model.ColorSpaceRGB},
			},
			TR: g.transferFunction(transfer),
		},
		AIS: false,
	}
	g.stream.SetGraphicState(&state)
}

// transferFunction zamienia próbki na funkcję próbkowaną; ta sama tablica próbek daje ten sam obiekt.
func (g *group) transferFunction(samples []fl) *model.FunctionDict {
	if len(samples) < 2 {
		return nil
	}
	content := make([]byte, len(samples))
	for i, v := range samples {
		content[i] = byte(math.Round(float64(max(0, min(1, v)) * 255)))
	}
	key := string(content)
	if fn, has := g.cache.transfers[key]; has {
		return fn
	}
	fn := &model.FunctionDict{
		FunctionType: model.FunctionSampled{
			Stream:        model.Stream{Content: content},
			Size:          []int{len(samples)},
			BitsPerSample: 8,
		},
		Domain: []model.Range{{0, 1}},
		Range:  []model.Range{{0, 1}},
	}
	if g.cache.transfers != nil {
		g.cache.transfers[key] = fn
	}
	return fn
}

// WithoutText obejmuje task sekcją treści oznaczonej z pustym tekstem zastępczym
// (/Span <</ActualText ()>> BDC … EMC, ISO 32000-1 § 14.9.4): czytniki i narzędzia wyodrębniające
// tekst (pdftotext, kopiowanie) biorą wtedy pusty tekst zamiast glifów z kopii.
func (g *group) WithoutText(task func()) {
	g.stream.Ops(cs.OpBeginMarkedContent{Tag: "Span", Properties: cs.PropertyListDict{"ActualText": model.ObjStringLiteral("")}})
	task()
	g.stream.Ops(cs.OpEndMarkedContent{})
}
