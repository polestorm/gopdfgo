package contentstream

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"image/color"
	"math"

	"github.com/polestorm/gopdfgo/internal/pdflib/fonts"
	"github.com/polestorm/gopdfgo/internal/pdflib/model"
)

var (
	errNoFont     = errors.New("no font is currently selected")
	errUnbalanced = errors.New("unbalanced save/restore state operators")
)

type GraphicState struct {
	Font     fonts.BuiltFont // the current usable font
	FontSize Fl

	XTLM    Fl // The x position of the text line matrix.
	YTLM    Fl // The y position of the text line matrix.
	Leading Fl // The current text leading.
	Matrix  model.Matrix

	// current state to avoid unecessary instructions

	strokeColor            OpSetStrokeRGBColor
	fillColor              OpSetFillRGBColor
	strokeAlpha, fillAlpha Fl
}

// GraphicStream is a buffer of graphics operation,
// with a state. It provides convenient methods
// to ease the creation of a content stream.
// Once ready, it can be transformed to an XObjectForm,
// a Page object or a Tilling pattern.
type GraphicStream struct {
	resources model.ResourcesDict

	fillAlphaState, strokeAlphaState map[model.ObjFloat]*model.GraphicState

	ops []Operation

	// gopdfgo: flushed – operacje sprzed ops zapisane już jako tekst strumienia i skompresowane
	// (Flush), flushedOps – ich liczba; treść strumienia to flushed po rozpakowaniu, a za nią ops
	flushed    []byte
	flushedOps int

	stateList []GraphicState
	State     GraphicState

	BoundingBox model.Rectangle
}

// NewGraphicStream setup the BBox and initialize the
// resources dictionary.
func NewGraphicStream(bbox model.Rectangle) GraphicStream {
	return GraphicStream{
		BoundingBox: bbox,
		resources: model.ResourcesDict{
			Font:      make(map[model.ObjName]*model.FontDict),
			XObject:   make(map[model.ObjName]model.XObject),
			Shading:   make(map[model.ObjName]*model.ShadingDict),
			ExtGState: make(map[model.ObjName]*model.GraphicState),
			Pattern:   make(map[model.ObjName]model.Pattern),
		},
		State: GraphicState{
			Matrix:      model.Matrix{1, 0, 0, 1, 0, 0},
			strokeAlpha: 1,
			fillAlpha:   1,
		},
		fillAlphaState:   make(map[model.ObjFloat]*model.GraphicState),
		strokeAlphaState: make(map[model.ObjFloat]*model.GraphicState),
	}
}

// OpsLen (gopdfgo) zwraca liczbę operacji zapisanych dotąd w strumieniu; pozwala rozpoznać, że
// strumień się nie zmienił od ostatniego ToXFormObject.
func (ap GraphicStream) OpsLen() int { return ap.flushedOps + len(ap.ops) }

// content (gopdfgo) zwraca tekst strumienia: część zapisaną przez Flush i dalsze operacje, z tymi samymi
// odstępami, co WriteOperations dla wszystkich operacji naraz.
func (ap GraphicStream) content() []byte {
	if ap.flushedOps == 0 {
		return WriteOperations(ap.ops...)
	}
	reader, err := zlib.NewReader(bytes.NewReader(ap.flushed))
	if err != nil {
		panic(fmt.Sprintf("contentstream: flushed content: %v", err))
	}
	var out bytes.Buffer
	if _, err := out.ReadFrom(reader); err != nil {
		panic(fmt.Sprintf("contentstream: flushed content: %v", err))
	}
	writeOperationsAt(&out, ap.flushedOps, ap.ops)
	return out.Bytes()
}

// Flush (gopdfgo) zapisuje pierwsze n operacji (wszystkie, gdy n < 0) jako tekst strumienia, skompresowany
// szybkim stopniem zlib, i zwalnia je. Treść strumienia (ToXFormObject) się nie zmienia; operacje
// zajmują w pamięci kilkanaście razy więcej niż ich skompresowany zapis. Służy rysowaniu w trakcie
// układu (ADR 0027): strona narysowana czeka w tej postaci na koniec dokumentu.
func (ap *GraphicStream) Flush(n int) {
	if n < 0 || n > len(ap.ops) {
		n = len(ap.ops)
	}
	if n == 0 {
		return
	}
	var plain bytes.Buffer
	if ap.flushedOps != 0 {
		reader, err := zlib.NewReader(bytes.NewReader(ap.flushed))
		if err != nil {
			panic(fmt.Sprintf("contentstream: flushed content: %v", err))
		}
		if _, err := plain.ReadFrom(reader); err != nil {
			panic(fmt.Sprintf("contentstream: flushed content: %v", err))
		}
	}
	writeOperationsAt(&plain, ap.flushedOps, ap.ops[:n])
	var packed bytes.Buffer
	w, _ := zlib.NewWriterLevel(&packed, zlib.BestSpeed)
	w.Write(plain.Bytes())
	w.Close()
	ap.flushed = packed.Bytes()
	ap.flushedOps += n
	ap.ops = append([]Operation(nil), ap.ops[n:]...)
}

// StreamMark (gopdfgo) to zapamiętane miejsce w strumieniu: liczba operacji, stan graficzny i stos
// stanów w chwili Mark (zob. ResumeAt).
type StreamMark struct {
	ops   int
	state GraphicState
	stack []GraphicState
}

// Offset (gopdfgo) zwraca numer operacji, przed którą leży miejsce m.
func (m StreamMark) Offset() int { return m.ops }

// Flushed (gopdfgo) zwraca liczbę operacji zapisanych przez Flush.
func (ap GraphicStream) Flushed() int { return ap.flushedOps }

// Mark (gopdfgo) zapamiętuje bieżące miejsce w strumieniu, żeby można było później dopisać w nim
// operacje (ResumeAt). Służy rysowaniu pól marginesowych strony po ułożeniu wszystkich stron (ADR 0027).
func (ap *GraphicStream) Mark() StreamMark {
	return StreamMark{ops: ap.flushedOps + len(ap.ops), state: ap.State, stack: append([]GraphicState(nil), ap.stateList...)}
}

// ResumeAt (gopdfgo) wraca do miejsca m: operacje dopisane po nim zdejmuje, a stan graficzny i stos
// przywraca. Działa tylko wtedy, gdy po m strumień już tylko zamykał zapisane stany (same Q) i stos
// jest pusty; zwraca wtedy liczbę zamknięć, które trzeba odtworzyć przez RestoreState po dopisaniu
// operacji. W przeciwnym razie nic nie zmienia i zwraca ok = false.
func (ap *GraphicStream) ResumeAt(m StreamMark) (restores int, ok bool) {
	at := m.ops - ap.flushedOps // miejsce w ops (część wcześniejsza mogła zostać zapisana przez Flush)
	if at < 0 || at > len(ap.ops) || len(ap.stateList) != 0 || len(ap.ops)-at != len(m.stack) {
		return 0, false
	}
	for _, op := range ap.ops[at:] {
		if _, isRestore := op.(OpRestore); !isRestore {
			return 0, false
		}
	}
	restores = len(ap.ops) - at
	ap.ops = ap.ops[:at]
	ap.State = m.state
	ap.stateList = append(ap.stateList[:0], m.stack...)
	return restores, true
}

// ToXFormObject write the appearance to a new object,
// and associate it the resources, which are shallow copied.
// The content is optionaly compressed with the Flater filter.
func (ap GraphicStream) ToXFormObject(compress bool) *model.XObjectForm {
	out := new(model.XObjectForm)
	out.BBox = ap.BoundingBox
	out.Resources = ap.resources.ShallowCopy()
	out.Content = ap.content()
	if compress {
		out.Stream = model.NewCompressedStream(out.Content)
	}
	return out
}

// ToGroupXFormObject (gopdfgo) działa jak ToXFormObject dla formy, która będzie grupą przezroczystości
// (także grupą maski miękkiej). Gdy treść używa wzorców, zamyka ją w zagnieżdżonej zwykłej formie
// (bez /Group), a forma zewnętrzna zawiera tylko `/XI0 Do`.
//
// Macierz wzorca odwzorowuje przestrzeń wzorca na domyślny układ strumienia, w którym go użyto
// (ISO 32000-1 § 8.7.3.1), czyli na układ formy. Apple PDFKit (Podgląd, Quick Look, Safari) dla
// wzorca z zasobów grupy przezroczystości bierze zamiast tego domyślny układ strony, pomijając CTM
// przy Do i /Matrix formy; wzorzec z zasobów zwykłej formy, także zagnieżdżonej w grupie, odczytuje
// poprawnie (sprawdzone na ręcznie zbudowanych plikach, 2026-11-20). Forma pośrednia ma ten sam
// układ co grupa, więc wynik według normy się nie zmienia, a ta sama grupa może być rysowana w wielu
// miejscach (kopie rozmycia filtra).
func (ap GraphicStream) ToGroupXFormObject(compress bool) *model.XObjectForm {
	if len(ap.resources.Pattern) == 0 {
		return ap.ToXFormObject(compress)
	}
	inner := ap.ToXFormObject(compress)
	out := new(model.XObjectForm)
	out.BBox = ap.BoundingBox
	out.Resources = model.ResourcesDict{XObject: map[model.ObjName]model.XObject{"XI0": inner}}
	out.Content = WriteOperations(OpXObject{XObject: "XI0"})
	if compress {
		out.Stream = model.NewCompressedStream(out.Content)
	}
	return out
}

// ApplyToPageObject update the given page with a single Content,
// build from the appearance.
// The content is optionaly compressed with the Flater filter.
func (ap GraphicStream) ApplyToPageObject(page *model.PageObject, compress bool) {
	fo := ap.ToXFormObject(compress)
	page.Contents = []model.ContentStream{fo.ContentStream}
	page.MediaBox = &fo.BBox
	page.Resources = &fo.Resources
}

// ApplyToTilling update the fields BBox, ContentStream and Resources
// of the given pattern.
func (ap GraphicStream) ApplyToTilling(pattern *model.PatternTiling) {
	pattern.BBox = ap.BoundingBox
	pattern.Resources = ap.resources.ShallowCopy()
	pattern.ContentStream.Content = ap.content()
}

// Ops adds one or more graphic command. Some commands usually need
// to also update the state: see the other methods.
func (ap *GraphicStream) Ops(op ...Operation) {
	ap.ops = append(ap.ops, op...)
}

// ForgetColors (gopdfgo) oznacza kolory i stałe alfa jako nieznane, tak że następne ustawienie zawsze
// trafia do strumienia. Potrzebne w formach XObject (grupach): dziedziczą stan graficzny z miejsca
// użycia, więc domyślna czerń i alfa 1 nie są w nich pewne (PDF 32000-1 § 8.10.1, § 11.6.5.2).
// Bez tego czarne tło maski miękkiej przejmowało kolor wypełnienia z miejsca jej ustawienia.
func (app *GraphicStream) ForgetColors() {
	app.State.fillColor = OpSetFillRGBColor{R: -1}
	app.State.strokeColor = OpSetStrokeRGBColor{R: -1}
	app.State.fillAlpha, app.State.strokeAlpha = -1, -1
}

func (app *GraphicStream) SetColorFill(c color.Color) {
	r, g, b := colorRGB(c)
	op := OpSetFillRGBColor{R: r, G: g, B: b}
	if app.State.fillColor == op {
		return
	}
	app.State.fillColor = op
	app.Ops(op)
}

func (app *GraphicStream) SetColorStroke(c color.Color) {
	r, g, b := colorRGB(c)
	op := OpSetStrokeRGBColor{R: r, G: g, B: b}
	if app.State.strokeColor == op {
		return
	}
	app.State.strokeColor = op
	app.Ops(op)
}

func (app *GraphicStream) SetFillAlpha(alpha Fl) {
	if app.State.fillAlpha == alpha {
		return
	}
	app.State.fillAlpha = alpha

	state, has := app.fillAlphaState[model.ObjFloat(alpha)]
	if !has {
		state = &model.GraphicState{Ca: model.ObjFloat(alpha)}
		app.fillAlphaState[model.ObjFloat(alpha)] = state
	}
	app.SetGraphicState(state)
}

func (app *GraphicStream) SetStrokeAlpha(alpha Fl) {
	if app.State.strokeAlpha == alpha {
		return
	}
	app.State.strokeAlpha = alpha

	state, has := app.strokeAlphaState[model.ObjFloat(alpha)]
	if !has {
		state = &model.GraphicState{CA: model.ObjFloat(alpha)}
		app.strokeAlphaState[model.ObjFloat(alpha)] = state
	}
	app.SetGraphicState(state)
}

// SetGraphicState register the given state and write it on the stream
func (app *GraphicStream) SetGraphicState(state *model.GraphicState) {
	name := app.AddExtGState(state)
	app.Ops(OpSetExtGState{Dict: name})
}

// Shading register the given shading and apply it on the stream.
// It is a shortcut for `AddShading` followed by `Ops(OpShFill)`.
func (app *GraphicStream) Shading(sh *model.ShadingDict) {
	name := app.AddShading(sh)
	app.Ops(OpShFill{Shading: name})
}

// check is the font is in the resources map or generate a new name and add the font
func (ap GraphicStream) addFont(newFont *model.FontDict) model.ObjName {
	for name, f := range ap.resources.Font {
		if f == newFont {
			return name
		}
	}
	// this convention must be respected so that the names are distincts
	name := model.ObjName(fmt.Sprintf("FT%d", len(ap.resources.Font)))
	ap.resources.Font[name] = newFont
	return name
}

// SetFontAndSize sets the font and the size (in points) for the subsequent text writing.
func (ap *GraphicStream) SetFontAndSize(font fonts.BuiltFont, size Fl) {
	if ap.State.Font.Meta == font.Meta && ap.State.FontSize == size {
		return
	}

	ap.State.Font = font
	ap.State.FontSize = size
	name := ap.addFont(font.Meta) // store and add the PDF name
	ap.Ops(OpSetFont{Font: name, Size: size})
}

// SetLeading sets the text leading parameter, which is measured in text space units.
// It specifies the vertical distance
// between the baselines of adjacent lines of text.
func (ap *GraphicStream) SetLeading(leading Fl) {
	ap.State.Leading = leading
	ap.Ops(OpSetTextLeading{L: leading})
}

// BeginVariableText starts a MarkedContent sequence of text
func (ap *GraphicStream) BeginVariableText() {
	ap.Ops(OpBeginMarkedContent{Tag: "Tx"})
}

// EndVariableText end a MarkedContent sequence of text
func (ap *GraphicStream) EndVariableText() {
	ap.Ops(OpEndMarkedContent{})
}

// BeginText starts the writing of text.
func (ap *GraphicStream) BeginText() {
	ap.State.XTLM = 0
	ap.State.YTLM = 0
	ap.Ops(OpBeginText{})
}

// EndText ends the writing of text
func (ap *GraphicStream) EndText() {
	ap.State.XTLM = 0
	ap.State.YTLM = 0
	ap.Ops(OpEndText{})
}

// MoveText moves to the start of the next line, offset from the start of the current line.
func (ap *GraphicStream) MoveText(x, y Fl) {
	ap.State.XTLM += x
	ap.State.YTLM += y
	ap.Ops(OpTextMove{X: x, Y: y})
}

// ShowText shows the `text`, after encoding it
// according to the current font.
// And error is returned (only) if a font has not been setup.
// A typical text drawing should apply the following methods ;
//   - BeginText
//   - SetFontAndSize
//   - ShowText
//   - EndText
func (ap *GraphicStream) ShowText(text string) error {
	if ap.State.Font.Font == nil {
		return errNoFont
	}
	s := string(ap.State.Font.Encode([]rune(text)))
	ap.Ops(OpShowText{Text: s})
	return nil
}

// NewlineShowText moves to the next line and shows text.
func (ap *GraphicStream) NewlineShowText(text string) error {
	if ap.State.Font.Font == nil {
		return errNoFont
	}
	ap.State.YTLM -= ap.State.Leading
	s := string(ap.State.Font.Encode([]rune(text)))
	ap.Ops(OpMoveShowText{Text: s})
	return nil
}

// Transform changes the current matrix, by applying a Concat Op with the given `mat` and
// updating the current state.
func (ap *GraphicStream) Transform(mat model.Matrix) {
	ap.Ops(OpConcat{Matrix: mat})
	ap.State.Matrix = mat.Multiply(ap.State.Matrix)
}

// SetTextMatrix changes the text matrix.
// This operation also initializes the current point position.
func (ap *GraphicStream) SetTextMatrix(a, b, c, d, x, y Fl) {
	ap.State.XTLM = x
	ap.State.YTLM = y
	ap.Ops(OpSetTextMatrix{Matrix: model.Matrix{a, b, c, d, x, y}})
}

// Saves the graphic state. SaveState and RestoreState must be balanced.
func (ap *GraphicStream) SaveState() {
	ap.Ops(OpSave{})
	ap.stateList = append(ap.stateList, ap.State)
}

// RestoreState restores the graphic state.
// An error is returned (only) if the calls of SaveState and RestoreState are not balanced.
func (ap *GraphicStream) RestoreState() error {
	idx := len(ap.stateList) - 1
	if idx < 0 {
		return errUnbalanced
	}
	ap.Ops(OpRestore{})
	ap.State = ap.stateList[idx]
	ap.stateList = ap.stateList[:idx]
	return nil
}

// check if the image or content is in the resources map or generate a new name and add the object
func (ap *GraphicStream) addXobject(xobj model.XObject) model.Name {
	for name, obj := range ap.resources.XObject {
		if obj == xobj {
			return name
		}
	}
	// this convention must be respected so that the names are distincts
	name := model.ObjName(fmt.Sprintf("XO%d", len(ap.resources.XObject)))
	ap.resources.XObject[name] = xobj
	return name
}

// AddXObjectDims puts an image or an XObjectForm in the current page, at the given position,
// with the given dimentions.
// See `RenderingDims` for several ways of specifying image dimentions.
func (ap *GraphicStream) AddXObjectDims(obj model.XObject, x, y, width, height Fl) {
	xObjectName := ap.addXobject(obj)
	ap.Ops(
		OpSave{},
		OpConcat{Matrix: model.Matrix{width, 0, 0, height, x, y}},
		OpXObject{XObject: xObjectName},
		OpRestore{},
	)
}

// AddXObject is the same as AddXObjectDims, but do not change the CTM.
func (ap *GraphicStream) AddXObject(obj model.XObject) {
	// since we don't change the CTM we dont need to save and restore the state
	// ap.AddXObjectDims(obj, 0, 0, 1, 1)
	xObjectName := ap.addXobject(obj)
	ap.Ops(OpXObject{XObject: xObjectName})
}

// approximate arc for border radius
const myArc = (4.0 / 3.0) * (math.Sqrt2 - 1.0)

// RoundedRectPath returns a rectangle path with rounded corners.
// The rectangle is of width `w` and height `h`. Its upper left corner is positioned at point (`x`, `y`).
// The radius for each corner are given by `rTL` (top-left), `rTR` (top-right)
// `rBR` (bottom-right), `rBL` (bottom-left) (0 means square corners)
func RoundedRectPath(x, y, w, h, rTL, rTR, rBR, rBL model.Fl) []Operation {
	out := make([]Operation, 0, 4)

	out = append(out, OpMoveTo{X: x + rTL, Y: y})
	xc := x + w - rTR
	yc := y + rTR
	out = append(out, OpLineTo{X: xc, Y: y})
	if rTR > 0 {
		out = append(out, OpCubicTo{X1: xc + rTR*myArc, Y1: yc - rTR, X2: xc + rTR, Y2: yc - rTR*myArc, X3: xc + rTR, Y3: yc})
	}

	xc = x + w - rBR
	yc = y + h - rBR
	out = append(out, OpLineTo{X: x + w, Y: yc})
	if rBR > 0 {
		out = append(out, OpCubicTo{X1: xc + rBR, Y1: yc + rBR*myArc, X2: xc + rBR*myArc, Y2: yc + rBR, X3: xc, Y3: yc + rBR})
	}

	xc = x + rBL
	yc = y + h - rBL
	out = append(out, OpLineTo{X: xc, Y: y + h})
	if rBL != 0 {
		out = append(out, OpCubicTo{X1: xc - rBL*myArc, Y1: yc + rBL, X2: xc - rBL, Y2: yc + rBL*myArc, X3: xc - rBL, Y3: yc})
	}

	xc = x + rTL
	yc = y + rTL
	out = append(out, OpLineTo{X: x, Y: yc})
	if rTL != 0 {
		out = append(out, OpCubicTo{X1: xc - rTL, Y1: yc - rTL*myArc, X2: xc - rTL*myArc, Y2: yc - rTL, X3: xc, Y3: yc - rTL})
	}
	return out
}
