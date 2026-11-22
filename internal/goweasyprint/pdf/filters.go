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
	g.beginContent() // gopdfgo: zob. tagged.go
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
			S:  model.ObjName(kind),
			TR: g.transferFunction(transfer),
		},
		AIS: false,
	}
	// gopdfgo: maska w układzie znormalizowanym (zob. contentstream.SetSoftMaskState)
	g.stream.SetSoftMaskState(&state, form, form.BBox, model.TransparencyGroup{CS: model.ColorSpaceRGB}, g.maskLimit())
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
	g.holdMarked(func() { // gopdfgo: ciąg /Span wewnątrz treści oznaczonej (zob. tagged.go)
		g.stream.Ops(cs.OpBeginMarkedContent{Tag: "Span", Properties: cs.PropertyListDict{"ActualText": model.ObjStringLiteral("")}})
		task()
		g.stream.Ops(cs.OpEndMarkedContent{})
	})
}

// sharedFormKey (gopdfgo): klucz formy treści powtarzanej: klucz podany przez rysującego, rozmiar formy
// i granica rozmiaru form masek miękkich bieżącej strony (zob. maskLimit), od której zależy treść.
type sharedFormKey struct {
	key           any
	width, height fl
	maskLimit     [2]fl
}

var _ backend.SharedDrawer = (*group)(nil)

// DrawShared (gopdfgo, backend.SharedDrawer) rysuje treść draw jako zwykłą formę XObject (bez grupy
// przezroczystości, więc wynik jest taki sam jak przy rysowaniu wprost, ISO 32000-1 § 8.10) o prostokącie
// /BBox (0, 0, width, height). Forma powstaje przy pierwszym wywołaniu z danym kluczem; kolejne, na tej
// samej albo innej stronie, dopisują tylko operator Do. Forma dziedziczy stan graficzny z miejsca użycia
// (ISO 32000-1 § 8.10.1), więc jej strumień nie zakłada znanych kolorów ani alfy (ForgetColors), a Do
// przywraca stan po narysowaniu formy, więc stan śledzony w g.stream zostaje aktualny.
func (g *group) DrawShared(key any, width, height fl, draw func(dst backend.Canvas)) {
	k := sharedFormKey{key: key, width: width, height: height, maskLimit: g.maskLimit()}
	form, has := g.cache.forms[k]
	// gopdfgo: przy pomijaniu tekstu (wycinek obrazu krojonego między strony, ADR 0041) forma z tekstem
	// leżącym poza wycinkiem jest rysowana osobno, bez tego tekstu; wspólna forma służy dalej, gdy obraz
	// nie ma tekstu albo cały jego tekst przecina wycinek
	if rect, cull := g.formCull(width, height); cull && (!has || g.cache.formTexts[form]) {
		content := newGroup(g.cache, 0, 0, width, height)
		content.stream.ForgetColors()
		content.cull = textCull{on: true, rect: rect}
		draw(&content)
		if content.culled > 0 {
			g.beginContent()
			g.stream.AddXObject(content.stream.ToXFormObject(compressStreams))
			return
		}
		if !has {
			form, has = g.storeForm(k, &content), true
		}
	}
	if !has {
		content := newGroup(g.cache, 0, 0, width, height)
		content.stream.ForgetColors()
		draw(&content)
		form = g.storeForm(k, &content)
	}
	g.beginContent() // gopdfgo: zob. tagged.go
	g.stream.AddXObject(form)
}

// storeForm (gopdfgo) zapisuje treść content jako formę wspólną pod kluczem k.
func (g *group) storeForm(k sharedFormKey, content *group) *model.XObjectForm {
	form := content.stream.ToXFormObject(compressStreams)
	if g.cache.forms != nil {
		g.cache.forms[k] = form
		g.cache.formTexts[form] = content.texts > 0
	}
	return form
}

// formCull (gopdfgo) zwraca obszar pomijania tekstu w układzie formy (0, 0, width, height) rysowanej
// w bieżącym miejscu; cull = false, gdy pomijanie jest wyłączone albo obszar obejmuje całą formę.
func (g *group) formCull(width, height fl) (rect [4]fl, cull bool) {
	if !g.cull.on {
		return rect, false
	}
	inverse := g.GetTransform()
	if inverse.Invert() != nil {
		return rect, false
	}
	rect = aabb(inverse, g.cull.rect[0], g.cull.rect[1], g.cull.rect[2], g.cull.rect[3])
	if rect[0] <= 0 && rect[1] <= 0 && rect[2] >= width && rect[3] >= height {
		return rect, false
	}
	return rect, true
}
