package layout

import (
	"fmt"
	"slices"
	"strings"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// Layout for pages and CSS3 margin boxes.

type contentSizer interface {
	minContentSize() pr.Float
	maxContentSize() pr.Float
}

type orientedBoxITF interface {
	baseBox() *orientedBox
	restoreBoxAttributes()
}

type orientedBox struct {
	// abstract, must be implemented by subclasses
	contentSizer

	context                 *layoutContext
	box                     Box // either *bo.PageBox or *bo.MarginBox
	marginA, marginB, inner pr.MaybeFloat
	paddingPlusBorder       pr.Float
}

func (o *orientedBox) baseBox() *orientedBox {
	return o
}

func (o orientedBox) sugar() pr.Float {
	return o.paddingPlusBorder + o.marginA.V() + o.marginB.V()
}

func (o orientedBox) outer() pr.Float {
	return o.sugar() + o.inner.V()
}

// gopdfgo: pole dostaje dokładnie wyliczony rozmiar zewnętrzny. W źródle (jak w WeasyPrint) rozmiar
// wewnętrzny był przycinany do przedziału od min-content do max-content, więc pole o szerokości auto
// kurczyło się do własnej treści; CSS Page 3 § 5.3 (Page-Margin Box Layout) takiego przycięcia nie zna.
func (o *orientedBox) setOuter(newOuterWidth pr.Float) {
	o.inner = max(newOuterWidth-o.sugar(), 0)
}

func (o orientedBox) outerMinContentSize() pr.Float {
	if o.inner == pr.AutoF {
		return o.sugar() + o.minContentSize()
	}
	return o.sugar() + o.inner.V()
}

func (o orientedBox) outerMaxContentSize() pr.Float {
	if o.inner == pr.AutoF {
		return o.sugar() + o.maxContentSize()
	}
	return o.sugar() + o.inner.V()
}

type verticalBox struct {
	content pr.MaybeFloat // gopdfgo: wysokość treści, liczona raz
	orientedBox
}

func newVerticalBox(context *layoutContext, box Box) *verticalBox {
	self := new(verticalBox)
	self.context = context
	self.box = box
	// Inner dimension: that of the content area, as opposed to the
	// outer dimension: that of the margin area.
	box_ := box.Box()
	self.inner = box_.Height
	self.marginA = box_.MarginTop
	self.marginB = box_.MarginBottom
	self.paddingPlusBorder = box_.PaddingTop.V() + box_.PaddingBottom.V() +
		box_.BorderTopWidth.V() + box_.BorderBottomWidth.V()
	self.orientedBox.contentSizer = self
	// gopdfgo: height: min-content, max-content i fit-content pola marginesowego to wysokość jego treści
	// (CSS Sizing 3 § 2.1); źródło czytało je jak auto, więc pole rozciągało się na cały margines strony.
	if marginBox, ok := box.(*bo.MarginBox); ok && marginBox.IsGenerated && box_.Width != pr.AutoF &&
		box_.Style.GetHeight().Intrinsic != pr.IntrinsicNone {
		self.inner = self.contentSize()
	}
	return self
}

func (vb *verticalBox) restoreBoxAttributes() {
	box := vb.box.Box()
	box.Height = vb.inner
	box.MarginTop = vb.marginA
	box.MarginBottom = vb.marginB
}

// gopdfgo: wysokość min-content i max-content pola marginesu to wysokość jego treści ułożonej
// w ustalonej już szerokości (CSS Page 3 § 5.3.2.4 stosuje do wysokości te same reguły co do
// szerokości). Źródło zwracało 0 i 1e6, więc pola z lewej i z prawej strony dzieliły krawędź po
// równo, niezależnie od treści.
func (vb *verticalBox) contentSize() pr.Float {
	if vb.content != nil {
		return vb.content.V()
	}
	vb.content = pr.Float(0)
	if marginBox, ok := vb.box.(*bo.MarginBox); ok && marginBox.IsGenerated && marginBox.Width != pr.AutoF {
		probe := bo.Deepcopy(vb.box)
		fields := probe.Box()
		fields.Height = pr.AutoF
		fields.MarginTop, fields.MarginBottom = pr.Float(0), pr.Float(0)
		if fields.MarginLeft == pr.AutoF {
			fields.MarginLeft = pr.Float(0)
		}
		if fields.MarginRight == pr.AutoF {
			fields.MarginRight = pr.Float(0)
		}
		var positioned []*AbsolutePlaceholder
		laidOut, _, _ := blockContainerLayout(vb.context, probe, -pr.Inf, nil, true,
			&positioned, &positioned, new([]pr.Float), false, -1)
		if height, ok := laidOut.Box().Height.(pr.Float); ok {
			vb.content = height
		}
	}
	return vb.content.V()
}

func (vb *verticalBox) minContentSize() pr.Float { return vb.contentSize() }

func (vb *verticalBox) maxContentSize() pr.Float { return vb.contentSize() }

type horizontalBox struct {
	_minContentSize, _maxContentSize pr.MaybeFloat
	orientedBox
}

func newHorizontalBox(context *layoutContext, box Box) *horizontalBox {
	self := new(horizontalBox)
	self.context = context
	self.box = box
	box_ := box.Box()
	self.inner = box_.Width
	self.marginA = box_.MarginLeft
	self.marginB = box_.MarginRight
	self.paddingPlusBorder = box_.PaddingLeft.V() + box_.PaddingRight.V() +
		box_.BorderLeftWidth.V() + box_.BorderRightWidth.V()
	self.orientedBox.contentSizer = self
	return self
}

func (hb *horizontalBox) restoreBoxAttributes() {
	box := hb.box.Box()
	box.Width = hb.inner
	box.MarginLeft = hb.marginA
	box.MarginRight = hb.marginB
}

func (hb *horizontalBox) minContentSize() pr.Float {
	if hb._minContentSize == nil {
		hb._minContentSize = minContentWidth(hb.context, hb.box, false)
	}
	return hb._minContentSize.V()
}

func (hb *horizontalBox) maxContentSize() pr.Float {
	if hb._maxContentSize == nil {
		hb._maxContentSize = maxContentWidth(hb.context, hb.box, false)
	}
	return hb._maxContentSize.V()
}

func countAuto(v1, v2, v3 pr.MaybeFloat) int {
	out := 0
	if v1 == pr.AutoF {
		out++
	}
	if v2 == pr.AutoF {
		out++
	}
	if v3 == pr.AutoF {
		out++
	}
	return out
}

// Compute and set a margin box fixed dimension on “box“.
//
// Described in: https://drafts.csswg.org/css-page-3/#margin-constraints
//
//   - box: The margin box to work on
//   - outer: The target outer dimension (value of a page margin)
//   - vertical: true to set height, margin-top and margin-bottom; false for width,
//     margin-left and margin-right
//   - topOrLeft: true if the margin box in if the top half (for vertical==true) or
//     left half (for vertical==false) of the page.
//     This determines which margin should be "auto" if the values are
//     over-constrained. (Rule 3 of the algorithm.)
func computeFixedDimension(context *layoutContext, box_ *bo.MarginBox, outer pr.Float, vertical, topOrLeft bool) {
	var boxOriented orientedBoxITF
	if vertical {
		boxOriented = newVerticalBox(context, box_)
	} else {
		boxOriented = newHorizontalBox(context, box_)
	}
	box := boxOriented.baseBox()

	// Rule 2
	total := box.paddingPlusBorder
	for _, value := range [3]pr.MaybeFloat{box.marginA, box.marginB, box.inner} {
		if value != pr.AutoF {
			total += value.V()
		}
	}
	if total > outer {
		if box.marginA == pr.AutoF {
			box.marginA = pr.Float(0)
		}
		if box.marginB == pr.AutoF {
			box.marginB = pr.Float(0)
		}
		if box.inner == pr.AutoF {
			// XXX this is not in the spec, but without it box.inner
			// would end up with a negative value.
			// Instead, this will trigger rule 3 below.
			// https://lists.w3.org/Archives/Public/www-style/2012Jul/0006.html
			box.inner = pr.Float(0)
		}
	}
	// Rule 3
	if countAuto(box.marginA, box.marginB, box.inner) == 0 {
		// Over-constrained
		if topOrLeft {
			box.marginA = pr.AutoF
		} else {
			box.marginB = pr.AutoF
		}
	}
	// Rule 4
	if countAuto(box.marginA, box.marginB, box.inner) == 1 {
		if box.inner == pr.AutoF {
			box.inner = outer - box.paddingPlusBorder - box.marginA.V() - box.marginB.V()
		} else if box.marginA == pr.AutoF {
			box.marginA = outer - box.paddingPlusBorder - box.marginB.V() - box.inner.V()
		} else if box.marginB == pr.AutoF {
			box.marginB = outer - box.paddingPlusBorder - box.marginA.V() - box.inner.V()
		}
	}
	// Rule 5
	if box.inner == pr.AutoF {
		if box.marginA == pr.AutoF {
			box.marginA = pr.Float(0)
		}
		if box.marginB == pr.AutoF {
			box.marginB = pr.Float(0)
		}
		box.inner = outer - box.paddingPlusBorder - box.marginA.V() - box.marginB.V()
	}
	// Rule 6
	if box.marginA == pr.AutoF && box.marginB == pr.AutoF {
		v := (outer - box.paddingPlusBorder - box.inner.V()) / 2
		box.marginA = v
		box.marginB = v
	}

	if countAuto(box.marginA, box.marginB, box.inner) > 0 {
		panic(fmt.Sprintf("unexpected auto value in %v", box))
	}

	boxOriented.restoreBoxAttributes()
}

// Compute and set a margin box fixed dimension on “box“
//
// Described in: https://drafts.csswg.org/css-page-3/#margin-dimension
//
//   - sideBoxes: Three boxes on a same side (as opposed to a corner.)
//     A list of:
//
//     -- A @*-left or @*-top margin box
//     -- A @*-center or @*-middle margin box
//     -- A @*-right or @*-bottom margin box
//
//   - vertical:
//     true to set height, margin-top and margin-bottom; false for width,
//     margin-left and margin-right
//
//   - availableSize:
//     The distance between the page box’s left right border edges
func computeVariableDimension(context *layoutContext, sideBoxes_ [3]*bo.MarginBox, vertical bool, availableSize pr.Float) {
	var sideBoxes [3]orientedBoxITF
	for i, box_ := range sideBoxes_ {
		if vertical {
			sideBoxes[i] = newVerticalBox(context, box_)
		} else {
			sideBoxes[i] = newHorizontalBox(context, box_)
		}
	}
	boxA, boxB, boxC := sideBoxes[0].baseBox(), sideBoxes[1].baseBox(), sideBoxes[2].baseBox()
	// gopdfgo: rozdział miejsca (flex-fit, CSS Page 3 § 5.3.2.3) działa na rozmiarach zewnętrznych:
	// pudełko dostaje swój zewnętrzny rozmiar max-content albo min-content i udział w wolnym miejscu.
	// Źródło podawało do setOuter rozmiar samej treści, więc pole z marginesem, obramowaniem albo
	// odstępem wewnętrznym wychodziło za wąskie, a suma pól nie wypełniała krawędzi strony.

	for _, box_ := range sideBoxes {
		box := box_.baseBox()
		if box.marginA == pr.AutoF {
			box.marginA = pr.Float(0)
		}
		if box.marginB == pr.AutoF {
			box.marginB = pr.Float(0)
		}
	}

	if !boxB.box.(*bo.MarginBox).IsGenerated {
		// Non-generated boxes get zero for every box-model property
		if boxB.inner.V() != 0 {
			panic(fmt.Sprintf("expected boxB.inner == 0, got %v", boxB.inner))
		}
		if boxA.inner == pr.AutoF && boxC.inner == pr.AutoF {
			// A and C both have 'width: auto'
			outerA, outerC := flexFitPair(availableSize,
				boxA.outerMinContentSize(), boxA.outerMaxContentSize(),
				boxC.outerMinContentSize(), boxC.outerMaxContentSize(), 1, 1)
			boxA.setOuter(outerA)
			boxC.setOuter(outerC)
		} else {
			// only one box has 'width: auto'
			if boxA.inner == pr.AutoF {
				boxA.setOuter(availableSize - boxC.outer())
			} else if boxC.inner == pr.AutoF {
				boxC.setOuter(availableSize - boxA.outer())
			}
		}
	} else {
		if boxB.inner == pr.AutoF {
			// gopdfgo: szerokość B rozwiązujemy osobno względem podwojonego A i podwojonego C i bierzemy
			// mniejszą, czyli tę, przy której podwojone pole boczne wychodzi większe (CSS Page 3 § 5.3.2.2:
			// „AC is double the maximum of A and C”; tak samo Chromium i testy WPT margin-boxes/dimensions-*).
			// Źródło składało AC z największego min-content i największego max-content obu pól osobno,
			// a pole boczne o ustalonej szerokości liczyło jak pole auto, więc B wychodziło za wąskie albo
			// za szerokie. Gdy wszystkie współczynniki są zerowe, AC (podwojone pole) dostaje współczynnik 2,
			// a B współczynnik 1: trzy puste pola dzielą krawędź po równo (dimensions-010).
			outerB := pr.Inf
			for _, side := range [2]*orientedBox{boxA, boxC} {
				var candidate pr.Float
				if side.inner == pr.AutoF {
					candidate, _ = flexFitPair(availableSize,
						boxB.outerMinContentSize(), boxB.outerMaxContentSize(),
						2*side.outerMinContentSize(), 2*side.outerMaxContentSize(), 1, 2)
				} else {
					candidate = availableSize - 2*side.outer()
				}
				outerB = min(outerB, candidate)
			}
			boxB.setOuter(outerB)
		}
		if boxA.inner == pr.AutoF {
			boxA.setOuter((availableSize - boxB.outer()) / 2)
		}
		if boxC.inner == pr.AutoF {
			boxC.setOuter((availableSize - boxB.outer()) / 2)
		}
	}

	// And, we’re done!
	if countAuto(boxA.inner, boxB.inner, boxC.inner) > 0 {
		panic("unexpected auto value")
	}

	// Set the actual attributes back.
	for _, box := range sideBoxes {
		box.restoreBoxAttributes()
	}
}

// flexFitPair dzieli dostępne miejsce między dwa pola o szerokości auto według CSS Page 3 § 5.3.2.2
// i zwraca ich rozmiary zewnętrzne. fallbackA i fallbackB to współczynniki używane, gdy suma
// współczynników wynosi zero (norma: „both flex factors are assumed to be 1”).
//
// gopdfgo: źródło przy zerowej sumie dzieliło przez 1 i zostawiało współczynniki zerowe, więc puste
// pola nie dostawały wolnego miejsca i krawędź strony zostawała niewypełniona.
func flexFitPair(available, minA, maxA, minB, maxB, fallbackA, fallbackB pr.Float) (pr.Float, pr.Float) {
	baseA, baseB := minA, minB
	var factorA, factorB pr.Float
	switch {
	case available > maxA+maxB:
		baseA, baseB = maxA, maxB
		factorA, factorB = maxA, maxB
	case available > minA+minB:
		factorA, factorB = maxA-minA, maxB-minB
	default:
		factorA, factorB = minA, minB
	}
	if factorA+factorB == 0 {
		factorA, factorB = fallbackA, fallbackB
	}
	flexSpace := available - baseA - baseB
	sum := factorA + factorB
	return baseA + flexSpace*factorA/sum, baseB + flexSpace*factorB/sum
}

// Drop "pages" counter from style in @page and @margin context.
// Ensure `counter-increment: page` for @page context if not otherwise
// manipulated by the style.
func standardizePageBasedCounters(style pr.ElementStyle, pseudoType string) {
	pageCounterTouched := false
	for _, propname := range [...]pr.KnownProp{pr.PCounterSet, pr.PCounterReset, pr.PCounterIncrement} {
		key := pr.PropKey{KnownProp: propname}
		prop := style.Get(key).(pr.SIntStrings)
		if prop.K == pr.Auto {
			style.Set(key, pr.SIntStrings{Values: pr.IntStrings{}})
			continue
		}
		var justifiedValues pr.IntStrings
		for _, v := range prop.Values {
			if v.String == "page" {
				pageCounterTouched = true
			}
			if v.String != "pages" {
				justifiedValues = append(justifiedValues, v)
			}
		}
		style.Set(key, pr.SIntStrings{Values: justifiedValues})
	}

	if pseudoType == "" && !pageCounterTouched {
		current := style.GetCounterIncrement()
		newInc := append(pr.IntStrings{{String: "page", Int: 1}}, current.Values...)
		style.SetCounterIncrement(pr.SIntStrings{Values: newInc})
	}
}

// marginBoxPaintOrder to domyślna kolejność rysowania pól marginesowych (CSS Page 3 § 3.1).
var marginBoxPaintOrder = [16]string{
	"@top-left-corner", "@top-left", "@top-center", "@top-right",
	"@top-right-corner", "@right-top", "@right-middle", "@right-bottom",
	"@bottom-right-corner", "@bottom-right", "@bottom-center", "@bottom-left",
	"@bottom-left-corner", "@left-bottom", "@left-middle", "@left-top",
}

// Yield laid-out margin boxes for this page.
// “state“ is the actual, up-to-date page-state from
// “context.pageMaker[context.currentPage]“.
func makeMarginBoxes(context *layoutContext, page *bo.PageBox, state tree.PageState) []Box {
	// This is a closure only to make calls shorter

	// Return a margin box with resolved percentages.
	// The margin box may still have "auto" values.
	// Return ``None`` if this margin box should not be generated.
	// :param atKeyword: which margin box to return, eg. "@top-left"
	// :param containingBlock: as expected by :func:`resolvePercentages`.
	makeBox := func(atKeyword string, containingBlock bo.MaybePoint) *bo.MarginBox {
		style := context.styleFor.Get(page.PageType, atKeyword)
		if style == nil {
			// doesn't affect counters
			style = tree.ComputedFromCascaded(nil, nil, page.Style, context)
		}
		standardizePageBasedCounters(style, atKeyword)
		box := bo.NewMarginBox(atKeyword, style)
		// Empty boxes should not be generated, but they may be needed for
		// the layout of their neighbors.
		// TODO: should be the computed value.
		ct := style.GetContent().String
		box.IsGenerated = !(ct == "normal" || ct == "inhibit" || ct == "none")
		// TODO: get actual counter values at the time of the last page break
		if box.IsGenerated {
			// @margins mustn't manipulate page-context counters
			marginState := state.Copy()
			// quoteDepth, counterValues, counterScopes = marginState
			// TODO: check this, probably useless
			// gopdfgo: counter-reset w polu marginesowym zastępuje licznik strony o tej samej nazwie,
			// zamiast zakładać licznik zagnieżdżony (counters() daje wtedy jedną wartość)
			replaced := utils.NewSet()
			for _, reset := range box.Style.GetCounterReset().Values {
				if len(marginState.CounterValues[reset.String]) != 0 {
					replaced.Add(reset.String)
				}
			}
			marginState.CounterScopes = append(marginState.CounterScopes, replaced)
			bo.UpdateCounters(&marginState, box.Style)
			box.Children = bo.ContentToBoxes(
				box.Style, box, marginState.QuoteDepth, marginState.CounterValues,
				context.resolver, &context.TargetCollector, context.counterStyle, context,
				page)
			bo.ProcessWhitespace(box, false)
			bo.ProcessTextTransform(box)
			box = bo.CreateAnonymousBox(box).(*bo.MarginBox) // type stable
		}
		resolvePercentages(box, containingBlock)
		boxF := box.Box()
		// gopdfgo: procenty w marginesach i odstępach górnych i dolnych pola marginesowego odnoszą się
		// do wysokości bloku zawierającego (CSS Page 3 § 6: „for top and bottom values, percentages
		// are relative to the height of the containing block”). Źródło liczyło je jak w CSS 2 od szerokości.
		if cbHeight, ok := containingBlock[1].(pr.Float); ok {
			boxF.MarginTop = resolveOnePercentage(style.GetMarginTop(), pr.PMarginTop, cbHeight)
			boxF.MarginBottom = resolveOnePercentage(style.GetMarginBottom(), pr.PMarginBottom, cbHeight)
			boxF.PaddingTop = resolveOnePercentage(style.GetPaddingTop(), pr.PPaddingTop, cbHeight)
			boxF.PaddingBottom = resolveOnePercentage(style.GetPaddingBottom(), pr.PPaddingBottom, cbHeight)
		}
		// gopdfgo: width: min-content, max-content i fit-content dają szerokość treści (CSS Sizing 3 § 2.1;
		// CSS Page 3 § 5.3 opiera rozmiary pól na min-content i max-content); źródło czytało je jak auto.
		if box.IsGenerated {
			resolveIntrinsicSizes(context, box, containingBlock[0])
		}
		if !box.IsGenerated {
			boxF.Width = pr.Float(0)
			boxF.Height = pr.Float(0)
			for _, side := range [4]bo.Side{bo.STop, bo.SRight, bo.SBottom, bo.SLeft} {
				boxF.ResetSpacing(side)
			}
		}
		return box
	}

	marginTop := page.MarginTop.V()
	marginBottom := page.MarginBottom
	marginLeft := page.MarginLeft.V()
	marginRight := page.MarginRight
	maxBoxWidth := page.BorderWidth()
	maxBoxHeight := page.BorderHeight()

	// bottom right corner of the border box
	pageEndX := marginLeft + maxBoxWidth
	pageEndY := marginTop + maxBoxHeight

	// Margin box dimensions, described in
	// https://drafts.csswg.org/csswg/css3-page/#margin-box-dimensions
	var generatedBoxes []*bo.MarginBox
	prefixs := [4]string{"top", "bottom", "left", "right"}
	verticals := [4]bool{false, false, true, true}
	containingBlocks := [4]bo.MaybePoint{
		{maxBoxWidth, marginTop},
		{maxBoxWidth, marginBottom},
		{marginLeft, maxBoxHeight},
		{marginRight, maxBoxHeight},
	}
	positionXs := [4]pr.Float{marginLeft, marginLeft, 0, pageEndX}
	positionYs := [4]pr.Float{0, pageEndY, marginTop, marginTop}

	for i := range prefixs {
		prefix, vertical, containingBlock, positionX, positionY := prefixs[i], verticals[i], containingBlocks[i], positionXs[i], positionYs[i]

		suffixes := [3]string{"left", "center", "right"}
		variableOuter, fixedOuter := containingBlock[0], containingBlock[1]
		if vertical {
			suffixes = [3]string{"top", "middle", "bottom"}
			fixedOuter, variableOuter = containingBlock[0], containingBlock[1]
		}
		var sideBoxes [3]*bo.MarginBox
		anyIsGenerated := false
		for i, suffix := range suffixes {
			sideBoxes[i] = makeBox(fmt.Sprintf("@%s-%s", prefix, suffix), containingBlock)
			if sideBoxes[i].IsGenerated {
				anyIsGenerated = true
			}
		}

		if !anyIsGenerated {
			continue
		}
		if vertical {
			// gopdfgo: szerokość pól z lewej i z prawej strony ustalamy przed wysokością, bo wysokość
			// treści zależy od szerokości
			for _, box := range sideBoxes {
				if box.IsGenerated {
					computeFixedDimension(context, box, fixedOuter.V(), false, prefix == "left")
				}
			}
		}
		// We need the three boxes together for the variable dimension:
		computeVariableDimension(context, sideBoxes, vertical, variableOuter.V())
		offsets := [...]pr.Float{0, 0.5, 1}
		for i := range sideBoxes {
			box, offset := sideBoxes[i], offsets[i]
			if !box.IsGenerated {
				continue
			}
			box.PositionY = positionY
			box.PositionX = positionX
			if vertical {
				box.PositionY += offset * (variableOuter.V() - box.MarginHeight())
			} else {
				box.PositionX += offset * (variableOuter.V() - box.MarginWidth())
			}
			if !vertical {
				computeFixedDimension(context, box, fixedOuter.V(), true, prefix == "top")
			}
			generatedBoxes = append(generatedBoxes, box)
		}
	}

	atKeywords := [4]string{"@top-left-corner", "@top-right-corner", "@bottom-left-corner", "@bottom-right-corner"}
	cbWidths := [4]pr.MaybeFloat{marginLeft, marginRight, marginLeft, marginRight}
	cbHeights := [4]pr.MaybeFloat{marginTop, marginTop, marginBottom, marginBottom}
	positionXs = [4]pr.Float{0, pageEndX, 0, pageEndX}
	positionYs = [4]pr.Float{0, 0, pageEndY, pageEndY}
	// Corner boxes
	for i := range atKeywords {
		atKeyword, cbWidth, cbHeight, positionX, positionY := atKeywords[i], cbWidths[i], cbHeights[i], positionXs[i], positionYs[i]
		box := makeBox(atKeyword, bo.MaybePoint{cbWidth, cbHeight})
		if !box.IsGenerated {
			continue
		}
		box.PositionX = positionX
		box.PositionY = positionY
		// gopdfgo: najpierw szerokość, bo wysokość treści (height: fit-content itp.) zależy od szerokości
		computeFixedDimension(context, box, cbWidth.V(), false, strings.Contains(atKeyword, "left"))
		computeFixedDimension(context, box, cbHeight.V(), true, strings.Contains(atKeyword, "top"))
		generatedBoxes = append(generatedBoxes, box)
	}

	// gopdfgo: domyślna kolejność rysowania pól zaczyna się od @top-left-corner i idzie zgodnie z ruchem
	// wskazówek zegara (CSS Page 3 § 3.1); źródło dawało kolejno krawędzie górną, dolną, lewą, prawą
	// i na końcu narożniki.
	slices.SortStableFunc(generatedBoxes, func(a, b *bo.MarginBox) int {
		return slices.Index(marginBoxPaintOrder[:], a.AtKeyword) - slices.Index(marginBoxPaintOrder[:], b.AtKeyword)
	})
	out := make([]Box, len(generatedBoxes))
	for i, box := range generatedBoxes {
		out[i] = marginBoxContentLayout(context, box)
	}
	return out
}

// Layout a margin box’s content once the box has dimensions.
func marginBoxContentLayout(context *layoutContext, mBox *bo.MarginBox) Box {
	var positionedBoxes []*AbsolutePlaceholder
	newBox_, tmp, _ := blockContainerLayout(context, mBox, -pr.Inf, nil, true,
		&positionedBoxes, &positionedBoxes, new([]pr.Float), false, -1)

	if tmp.resumeAt != nil {
		panic(fmt.Sprintf("resumeAt should be nil, got %v", tmp.resumeAt))
	}

	for _, absBox := range positionedBoxes {
		absoluteLayout(context, absBox, mBox, &positionedBoxes, 0, nil)
	}

	box := newBox_.Box()
	verticalAlign := box.Style.GetVerticalAlign()
	// Every other value is read as "top", ie. no change.
	if L := len(box.Children); (verticalAlign.Tag == pr.Middle || verticalAlign.Tag == pr.Bottom) && L != 0 {
		firstChild := box.Children[0]
		lastChild := box.Children[L-1].Box()
		top := firstChild.Box().PositionY
		// Not always exact because floating point errors
		// assert top == box.contentBoxY()
		bottom := lastChild.PositionY + lastChild.MarginHeight()
		contentHeight := bottom - top
		offset := box.Height.V() - contentHeight
		if verticalAlign.Tag == pr.Middle {
			offset /= 2
		}
		for _, child := range box.Children {
			child.Translate(0, offset, false)
		}
	}
	return newBox_
}

// Take a :class:`OrientedBox` object and set either width, margin-left
// and margin-right; or height, margin-top and margin-bottom.
// "The width and horizontal margins of the page box are then calculated
//
//	exactly as for a non-replaced block element := range normal flow. The height
//	and vertical margins of the page box are calculated analogously (instead
//	of using the block height formulas). In both cases if the values are
//	over-constrained, instead of ignoring any margins, the containing block
//	is resized to coincide with the margin edges of the page box."
//
// https://drafts.csswg.org/csswg/css3-page/#page-box-page-rule
// https://www.w3.org/TR/CSS21/visudet.html#blockwidth
func pageWidthOrHeight(box_ orientedBoxITF, containingBlockSize pr.Float) {
	box := box_.baseBox()
	remaining := containingBlockSize - box.paddingPlusBorder
	if box.inner == pr.AutoF {
		if box.marginA == pr.AutoF {
			box.marginA = pr.Float(0)
		}
		if box.marginB == pr.AutoF {
			box.marginB = pr.Float(0)
		}
		box.inner = remaining - box.marginA.V() - box.marginB.V()
	} else if box.marginA == pr.AutoF && box.marginB == pr.AutoF {
		box.marginA = (remaining - box.inner.V()) / 2
		box.marginB = box.marginA
	} else if box.marginA == pr.AutoF {
		box.marginA = remaining - box.inner.V() - box.marginB.V()
	} else if box.marginB == pr.AutoF {
		box.marginB = remaining - box.inner.V() - box.marginA.V()
	}
	box_.restoreBoxAttributes()
}

var (
	pageWidth  = handleMinMaxWidth(pageWidth_)
	pageHeight = handleMinMaxHeight(pageHeight_)
)

// @handleMinMaxWidth
// containingBlock must be block
func pageWidth_(box Box, context *layoutContext, containingBlock containingBlock) (bool, pr.Float) {
	pageWidthOrHeight(newHorizontalBox(context, box), containingBlock.(block).Width)
	return false, 0
}

// @handleMinMaxHeight
// containingBlock must be block
func pageHeight_(box Box, context *layoutContext, containingBlock containingBlock) (bool, pr.Float) {
	pageWidthOrHeight(newVerticalBox(context, box), containingBlock.(block).Height)
	return false, 0
}

// Take just enough content from the beginning to fill one page.
//
// Return “(page, finished)“. “page“ is a laid out PageBox object
// and “resumeAt“ indicates where in the document to start the next page,
// or is “None“ if this was the last page.
//
// pageNumber: integer, start at 1 for the first page
// resumeAt: as returned by “makePage()“ for the previous page,
//
//	or ``None`` for the first page.
func (context *layoutContext) makePage(rootBox bo.BlockLevelBoxITF, pageType utils.PageElement, resumeAt tree.ResumeStack,
	pageNumber int, pageState *tree.PageState,
) (*bo.PageBox, tree.ResumeStack, tree.PageBreak) {
	style := context.styleFor.Get(pageType, "")

	// Propagated from the root or <body>.
	style.SetOverflow(pr.String(rootBox.Box().ViewportOverflow))
	// overflow elementu głównego przeniesione na obszar strony: treść poza nim jest przycinana
	context.viewportClips = rootBox.Box().ViewportOverflow != "" && rootBox.Box().ViewportOverflow != "visible"
	page := bo.NewPageBox(pageType, style)

	deviceSize_ := page.Style.GetSize()
	cbWidth, cbHeight := deviceSize_[0].Value, deviceSize_[1].Value
	resolvePercentages(page, bo.MaybePoint{cbWidth, cbHeight})

	page.PositionX = 0
	page.PositionY = 0
	pageWidth(page, context, block{Width: cbWidth})
	pageHeight(page, context, block{Height: cbHeight})
	if scale := context.contentScale; scale > 1 {
		// gopdfgo: treść układamy na powiększonym obszarze strony (ADR 0014); rozmiar z arkusza wraca
		// przed układem pól marginesów
		page.Width, page.Height = page.Width.V()*scale, page.Height.V()*scale
	}

	rootBox.Box().PositionX = page.ContentBoxX()
	rootBox.Box().PositionY = page.ContentBoxY()
	context.pageBottom = rootBox.Box().PositionY + page.Height.V()
	context.pageHeight = page.Height.V()
	if context.behavior.Continuous {
		// gopdfgo: tryb ciągły (ADR 0017). Dół strony odsuwamy tak daleko, że nic się nie dzieli;
		// wysokość obszaru strony z arkusza zostaje podstawą procentów, jednostek vh i elementów
		// pozycjonowanych, a po układzie strona rośnie do dołu treści (zob. niżej).
		context.pageBottom = rootBox.Box().PositionY + continuousPageHeight
		context.pageHeight = continuousPageHeight
	}
	initialContainingBlock := page

	footnoteAreaStyle := context.styleFor.Get(pageType, "@footnote")
	footnoteArea := bo.NewFootnoteAreaBox(page, footnoteAreaStyle)
	resolvePercentages(footnoteArea, bo.MaybePoint{page.Width, page.Height})
	footnoteArea.PositionX = page.ContentBoxX()
	footnoteArea.PositionY = context.pageBottom

	var previousResumeAt tree.ResumeStack
	if pageType.Blank {
		previousResumeAt = resumeAt
		rootBox = bo.CopyWithChildren(rootBox, nil).(bo.BlockLevelBoxITF) // CopyWithChildren is type stable
	}

	// https://www.w3.org/TR/css-display-4/#root
	if !(bo.BlockLevelT.IsInstance(rootBox)) {
		panic(fmt.Sprintf("expected BlockLevel, got %s", rootBox))
	}
	context.createBlockFormattingContext()
	context.currentPage = pageNumber
	context.currentPageFootnotes = nil
	context.currentFootnoteArea = footnoteArea

	reportedFootnotes := context.reportedFootnotes
	context.reportedFootnotes = nil

	for i, reportedFootnote := range reportedFootnotes {
		context.footnotes = append(context.footnotes, reportedFootnote)
		overflow := context.layoutFootnote(reportedFootnote)
		if overflow && i != 0 {
			context.reportFootnote(reportedFootnote)
			context.reportedFootnotes = context.reportedFootnotes[i:]
			break
		}
	}

	// Display out-of-flow boxes broken on the previous page.
	// TODO: we shouldn’t separate broken in-flow and out-of-flow layout.
	var (
		pageIsEmpty      = true
		adjoiningMargins []pr.Float
		positionedBoxes  []*AbsolutePlaceholder // Mixed absolute and fixed
		outOfFlowBoxes   []Box
		contextOutOfFlow = context.brokenOutOfFlow
	)
	context.brokenOutOfFlow = make(map[Box]brokenBox) // new map
	for _, v := range orderedBroken(contextOutOfFlow) {
		box, containingBlock := v.box, v.containingBlock
		if v.deferredPages > 0 {
			// pudełko pozycjonowane poniżej wcześniejszej strony: układamy je od nowa i przesuwamy
			// o wysokość stron, które minęły; jeśli nadal leży poniżej tej strony, czeka dalej
			shift := pr.Float(v.deferredPages) * context.pageHeight
			placed, placedResumeAt := absoluteBoxLayout(context, box, containingBlock, &positionedBoxes, -shift, nil)
			if placed.Box().PositionY-shift >= context.pageBottom {
				context.setBrokenOutOfFlow(placed, brokenBox{box: box, containingBlock: containingBlock, deferredPages: v.deferredPages + 1})
				continue
			}
			placed.Translate(0, -shift, false)
			outOfFlowBoxes = append(outOfFlowBoxes, placed)
			pageIsEmpty = false
			if placedResumeAt != nil {
				context.setBrokenOutOfFlow(placed, brokenBox{box: box, containingBlock: containingBlock, resumeAt: placedResumeAt})
			}
			continue
		}
		box.Box().PositionY = rootBox.Box().ContentBoxY()

		var (
			outOfFlowBox      Box
			outOfFlowResumeAt tree.ResumeStack
		)
		if box.Box().IsFloated() {
			outOfFlowBox, outOfFlowResumeAt = floatLayout(context, box, containingBlock.Box(),
				&positionedBoxes, &positionedBoxes, 0, v.resumeAt)
		} else {
			if !box.Box().IsAbsolutelyPositioned() {
				panic("internal error: box should be absolutely positioned")
			}
			outOfFlowBox, outOfFlowResumeAt = absoluteBoxLayout(context, box, containingBlock,
				&positionedBoxes, 0, v.resumeAt)
		}
		outOfFlowBoxes = append(outOfFlowBoxes, outOfFlowBox)
		pageIsEmpty = false
		if outOfFlowResumeAt != nil {
			context.setBrokenOutOfFlow(outOfFlowBox, brokenBox{box: box, containingBlock: containingBlock, resumeAt: outOfFlowResumeAt})
		}
	}

	// Display in-flow content.
	initialRootBox := rootBox
	initialResumeAt := resumeAt
	var tmp blockLayout
	if position := rootBox.Box().Style.GetPosition().String; (position == "absolute" || position == "fixed") && resumeAt == nil &&
		!rootBox.Box().IsFloated() {
		// gopdfgo: element główny pozycjonowany bezwzględnie jest układany jak każde pudełko pozycjonowane,
		// względem początkowego bloku zawierającego (CSS 2.1 § 10.1 pkt 4: gdy nie ma pozycjonowanego
		// przodka, blokiem zawierającym jest blok początkowy). Źródło ignorowało position na elemencie
		// głównym i rozciągało go na całą stronę.
		tmp.nextPage = tree.PageBreak{Break: "any"}
		placeholder := NewAbsolutePlaceholder(rootBox)
		absoluteLayout(context, placeholder, page, &positionedBoxes, 0, nil)
		if laidOut, ok := placeholder.AliasBox.(bo.BlockLevelBoxITF); ok {
			rootBox = laidOut
		}
	} else if rootBox.Box().IsFloated() && resumeAt == nil {
		// gopdfgo: element główny pływający jest układany jak element pływający w bloku początkowym:
		// szerokość dopasowana do treści i dosunięcie do wskazanej krawędzi (CSS 2.1 § 9.5, § 10.3.5;
		// test WPT float-root). Źródło rozciągało go na całą szerokość strony. Na kolejnych stronach
		// (wznowienie) zostaje układ blokowy.
		tmp.nextPage = tree.PageBreak{Break: "any"}
		var laidOut Box
		laidOut, tmp.resumeAt = floatLayout(context, rootBox, &initialContainingBlock.BoxFields, &positionedBoxes, &positionedBoxes, 0, nil)
		if laidOut, ok := laidOut.(bo.BlockLevelBoxITF); ok {
			rootBox = laidOut
		}
	} else {
		rootBox, tmp, _ = blockLevelLayout(context, rootBox, 0, resumeAt,
			&initialContainingBlock.BoxFields, true, &positionedBoxes, &positionedBoxes, &adjoiningMargins, false, -1)
		// gopdfgo: element główny z position: relative przesuwamy jak każde pudełko pozycjonowane
		// względnie (CSS 2.1 § 9.4.3), z procentami liczonymi od bloku początkowego. Źródło
		// pomijało przesunięcie elementu głównego.
		if rootBox != nil {
			relativePositioning(rootBox, bo.Point{initialContainingBlock.Width.V(), initialContainingBlock.Height.V()})
		}
	}
	resumeAt = tmp.resumeAt
	if rootBox == nil {
		// In-flow page rendering didn’t progress, only out-of-flow did. Render empty box
		// at skip_stack and force fragmentation to make the root box and its descendants
		// cover the whole page height.
		if pageIsEmpty {
			panic("expected empty page")
		}
		initialRootBox = bo.Deepcopy(initialRootBox).(bo.BlockLevelBoxITF)
		box, parent := Box(initialRootBox), Box(initialRootBox)
		skipStack := initialResumeAt
		for len(skipStack) == 1 {
			if _, isRemainder := skipStack.Remainder(); isRemainder {
				break
			}
			var skip int
			skip, skipStack = skipStack.Unpack()
			// kontenery flex i grid zapisują w stosie numer linii lub wiersza, nie indeks dziecka
			if skip < 0 || skip >= len(box.Box().Children) || bo.FlexContainerT.IsInstance(box) || bo.GridContainerT.IsInstance(box) {
				break
			}
			box, parent = box.Box().Children[skip], box
		}
		parent.Box().Children = nil
		parent.Box().ForceFragmentation = true
		rootBox, _, _ = blockLevelLayout(
			context, initialRootBox, 0, initialResumeAt, &initialContainingBlock.BoxFields,
			pageIsEmpty, &positionedBoxes, &positionedBoxes, &adjoiningMargins, false, -1)
		resumeAt = initialResumeAt
	}

	rootBox.Box().Children = append(outOfFlowBoxes, rootBox.Box().Children...)

	footnoteArea = bo.CreateAnonymousBox(bo.Deepcopy(footnoteArea)).(*bo.FootnoteAreaBox)
	tmpBox, _, _ := blockLevelLayout(
		context, footnoteArea, -pr.Inf, nil, &footnoteArea.Page.BoxFields,
		true, &positionedBoxes, &positionedBoxes, nil, false, -1)
	footnoteArea = tmpBox.(*bo.FootnoteAreaBox)
	footnoteArea.Translate(0, -footnoteArea.MarginHeight(), false)

	for _, placeholder := range positionedBoxes {
		if placeholder.Box().Style.GetPosition().String == "fixed" {
			page.FixedBoxes = append(page.FixedBoxes, placeholder.AliasBox) // page.FixedBox is empty before this loop
		}
	}
	for i := 0; i < len(positionedBoxes); i++ { // note that positionedBoxes may grow over the loop
		absoluteLayout(context, positionedBoxes[i], page, &positionedBoxes, 0, nil)
	}

	context.finishBlockFormattingContext(rootBox)

	if context.behavior.Continuous {
		// strona kończy się tam, gdzie treść, ale nie wyżej niż obszar strony z arkusza; gdy przewijanie
		// okna jest wyłączone (overflow elementu głównego albo body inne niż visible), strona nie rośnie
		bottom := page.ContentBoxY() + page.Height.V()
		if page.Style.GetOverflow() == "visible" {
			bottom = max(bottom, contentBottomEdge(rootBox))
		}
		footnoteArea.Translate(0, bottom-context.pageBottom, false)
		page.Height = bottom - page.ContentBoxY()
		context.pageBottom = bottom
	}

	page.Children = []Box{rootBox, footnoteArea}

	// Update page counter values
	standardizePageBasedCounters(style, "")
	bo.UpdateCounters(pageState, style)
	pageCounterValues := pageState.CounterValues
	// pageCounterValues will be cached in the pageMaker

	targetCollector := context.TargetCollector
	pageMaker := context.pageMaker

	// remakeState tells the makeAllPages-loop in layoutDocument()
	// whether and what to re-make.
	remakeState := &pageMaker[pageNumber-1].RemakeState

	// Evaluate and cache page values only once (for the first LineBox)
	// otherwise we suffer endless loops when the target/pseudo-element
	// spans across multiple pages
	cachedAnchors := utils.NewSet()
	cachedLookups := map[*tree.CounterLookupItem]bool{}

	for _, v := range pageMaker[:pageNumber-1] {
		cachedAnchors.Extend(v.RemakeState.Anchors)
		for _, u := range v.RemakeState.ContentLookups {
			cachedLookups[u] = true
		}
	}

	for _, child := range bo.DescendantsPlaceholders(page, true) {
		// Cache target's page counters
		anchor := string(child.Box().Style.GetAnchor())
		if anchor != "" && !cachedAnchors.Has(anchor) {
			remakeState.Anchors = append(remakeState.Anchors, anchor)
			cachedAnchors.Add(anchor)
			// Re-make of affected targeting boxes is inclusive
			targetCollector.CacheTargetPageCounters(anchor, pageCounterValues, pageNumber-1, pageMaker)
		}

		// string-set and bookmark-labels don't create boxes, only `content`
		// requires another call to makePage. There is maximum one "content"
		// item per box.
		var counterLookup *tree.CounterLookupItem
		if missingLink := child.MissingLink(); missingLink != nil {
			// A CounterLookupItem exists for the css-token "content"
			key := tree.NewFunctionKey(missingLink, "content")
			counterLookup = targetCollector.CounterLookupItems[key]
		}

		// Resolve missing (page based) counters
		if counterLookup != nil {
			callParseAgain := false

			// Prevent endless loops
			refreshMissingCounters := !cachedLookups[counterLookup]
			if refreshMissingCounters {
				remakeState.ContentLookups = append(remakeState.ContentLookups, counterLookup)
				cachedLookups[counterLookup] = true
				counterLookup.PageMakerIndex = tree.NewOptionnalInt(pageNumber - 1)
			}

			// Step 1: page based back-references
			// Marked as pending by targetCollector.cacheTargetPageCounters
			if counterLookup.Pending {
				if !pageCounterValues.Equal(counterLookup.CachedPageCounterValues) {
					counterLookup.CachedPageCounterValues = pageCounterValues.Copy()
				}
				counterLookup.Pending = false
				callParseAgain = true
			}

			// Step 2: local counters
			// If the box mixed-in page counters changed, update the content
			// and cache the new values.
			missingCounters := counterLookup.MissingCounters
			if len(missingCounters) != 0 {
				if missingCounters.Has("pages") {
					remakeState.PagesWanted = true
				}
				if refreshMissingCounters && !pageCounterValues.Equal(counterLookup.CachedPageCounterValues) {
					counterLookup.CachedPageCounterValues = pageCounterValues.Copy()
					for counterName := range missingCounters {
						counterValue := pageCounterValues[counterName]
						if counterValue != nil {
							callParseAgain = true
							// no need to loop them all
							break
						}
					}
				}
			}

			// Step 3: targeted counters
			targetMissing := counterLookup.MissingTargetCounters
			for anchorName, missedCounters := range targetMissing {
				if !missedCounters.Has("pages") {
					continue
				}
				// Adjust "pagesWanted"
				item := targetCollector.TargetLookupItems[anchorName]
				pageMakerIndex := item.PageMakerIndex
				if pageMakerIndex >= 0 && cachedAnchors.Has(anchorName) {
					pageMaker[pageMakerIndex].RemakeState.PagesWanted = true
				}
				// "contentChanged" is triggered in
				// targets.cacheTargetPageCounters()
			}

			if callParseAgain {
				remakeState.ContentChanged = true
				counterLookup.ParseAgain(pageCounterValues)
			}
		}
	}

	if pageType.Blank {
		resumeAt = previousResumeAt
		tmp.nextPage = pageMaker[pageNumber-1].NextPage
	}

	// gopdfgo: treść w przepływie się skończyła, ale zostały podzielone pudełka poza przepływem (np. blok
	// pozycjonowany bezwzględnie wyższy niż strona). Zwracamy wznowienie „po ostatnim dziecku”, żeby
	// powstała następna strona z ich dalszym ciągiem; w źródle dokument kończył się tutaj i reszta ginęła.
	if resumeAt == nil && len(context.brokenOutOfFlow) != 0 {
		resumeAt = tree.RemainderResume(0)
		if (tmp.nextPage == tree.PageBreak{}) {
			tmp.nextPage = tree.PageBreak{Break: "any"}
		}
	}

	if traceMode {
		traceLogger.DumpTree(page, "end makePage")
		traceLogger.Dump(fmt.Sprintf("end makePage: resume at %s, nextPage %v", resumeAt, tmp.nextPage))
	}

	return page, resumeAt, tmp.nextPage
}

func includesResumeAt(resumeAt, pageGroupResumeAt tree.ResumeStack) bool {
	if len(pageGroupResumeAt) == 0 {
		return true
	}
	pageChildIndex, pageChildResumeAt := pageGroupResumeAt.Unpack()
	if _, in := resumeAt[pageChildIndex]; resumeAt == nil || !in {
		return false
	}
	if pageChildResumeAt == nil {
		return true
	}
	return includesResumeAt(resumeAt[pageChildIndex], pageChildResumeAt)
}

type pageGroup struct {
	name     pr.Page
	index    int
	resumeAt tree.ResumeStack
}

func updatePageGroups(pageGroups *[]pageGroup, resumeAt tree.ResumeStack, nextPage tree.PageBreak, rootBox Box, blank bool) pr.Page {
	// https://www.w3.org/TR/css-gcpm-3/#document-sequence-selectors
	// Remove or increment page groups.
	for i, pageGroup := range slices.Clone(*pageGroups) {
		if includesResumeAt(resumeAt, pageGroup.resumeAt) {
			(*pageGroups)[i].index += 1
		} else {
			*pageGroups = slices.Delete(*pageGroups, i, i+1)
		}
	}

	// Add page groups.
	if !blank {
		if (len(resumeAt) != 0 && nextPage.Break == "any") || nextPage.Page == "" {
			// We don’t have a forced page break |or a named page.
			return nextPage.Page
		}
		if L := len(*pageGroups); L != 0 && (*pageGroups)[L-1].name == nextPage.Page {
			// We’re already in an element whose page name is the next page name.
			return nextPage.Page
		}
	}

	// Find the box that has the named page. It is a first in-flow child of the
	// element corresponding to resumeAt.

	// Find element corrensponding to resumeAt.
	pageGroupResumeAt := resumeAt.Copy()
	if len(pageGroupResumeAt) == 0 {
		pageGroupResumeAt = tree.ResumeStack{0: nil}
	}
	var (
		currentResumeAt = pageGroupResumeAt
		currentElement  = rootBox
		parentElement   Box
		childIndex      int
	)
	for {
		var childResumeAt tree.ResumeStack
		if _, isRemainder := currentResumeAt.Remainder(); isRemainder || len(currentResumeAt) != 1 {
			break // stan wewnętrzny układu (reszta bloku, linia flex), nie indeks dziecka
		}
		childIndex, childResumeAt = currentResumeAt.Unpack()
		if childIndex < 0 || childIndex >= len(currentElement.Box().Children) || bo.GridContainerT.IsInstance(currentElement) {
			break // grid zapisuje w stosie numer wiersza, nie indeks dziecka
		}
		parentElement = currentElement
		currentElement = currentElement.Box().Children[childIndex]
		if childResumeAt == nil {
			break
		}
		currentResumeAt = childResumeAt
	}

	if blank {
		// Page is blank, don’t create a new page group and return parent’s page name.
		return parentElement.Box().Style.GetPage()
	}

	// Find the descendant with named page.
	for {
		if currentElement.Box().Style.GetPage() == nextPage.Page {
			*pageGroups = append(*pageGroups, pageGroup{nextPage.Page, 0, pageGroupResumeAt})
			return nextPage.Page
		}
		if !bo.ParentT.IsInstance(currentElement) {
			// Shouldn’t happen.
			return nextPage.Page
		}
		var hasBroken bool
		for i, child := range currentElement.Box().Children {
			if !child.Box().IsInNormalFlow() {
				continue
			}
			currentResumeAt[childIndex] = tree.ResumeStack{i: nil}
			currentResumeAt = currentResumeAt[childIndex]
			childIndex = i
			currentElement = child
			hasBroken = true
			break
		}
		if !hasBroken {
			// Shouldn’t happen.
			return nextPage.Page
		}
	}
}

// Return one laid out page without margin boxes.
// Start with the initial values from “context.pageMaker[index]“.
// The resulting values / initial values for the next page are stored in
// the “pageMaker“.
// As the function"s name suggests: the plan is not to make all pages
// repeatedly when a missing counter was resolved, but rather re-make the
// single page where the “contentChanged“ happened.
func (context *layoutContext) remakePage(index int, pageGroups *[]pageGroup, rootBox bo.BlockLevelBoxITF, html *tree.HTML) (*bo.PageBox, tree.ResumeStack) {
	tmp := context.pageMaker[index]

	// PageType for current page, values for pageMaker[index + 1].
	// Don't modify actual pageMaker[index] values!
	pageState := tmp.PageState.Copy()
	var nextPageSide string
	switch tmp.NextPage.Break {
	case "left", "right":
		nextPageSide = tmp.NextPage.Break
	case "recto", "verso":
		directionLtr := rootBox.Box().Style.GetDirection() == pr.Ltr
		breakVerso := tmp.NextPage.Break == "verso"
		nextPageSide = "left"
		if directionLtr != breakVerso {
			nextPageSide = "right"
		}
	}
	blank := (nextPageSide == "left" && tmp.RightPage) || (nextPageSide == "right" && !tmp.RightPage) ||
		(len(context.reportedFootnotes) != 0 && tmp.ResumeAt == nil)

	side := "left"
	if tmp.RightPage {
		side = "right"
	}
	name := updatePageGroups(pageGroups, tmp.ResumeAt, tmp.NextPage, rootBox, blank)
	groups := make([]utils.PageGroup, len(*pageGroups))
	for i, g := range *pageGroups {
		groups[i] = utils.PageGroup{Name: string(g.name), Index: g.index}
	}
	pageType := utils.NewPageElement(side, string(name), index, blank, groups)
	context.styleFor.SetPageComputedStylesT(pageType, html)
	context.forcedBreak = tmp.NextPage.Break != "any" || tmp.NextPage.Page != ""
	context.marginClearance = false

	// makePage wants a pageNumber of index + 1
	pageNumber := index + 1
	page, resumeAt, nextPage := context.makePage(rootBox, pageType, tmp.ResumeAt,
		pageNumber, &pageState)

	if (nextPage == tree.PageBreak{}) {
		panic("expected nextPage")
	}

	tmp.RightPage = !tmp.RightPage

	// Check whether we need to append or update the next pageMaker item
	var pageMakerNextChanged bool
	if index+1 >= len(context.pageMaker) {
		// New page
		pageMakerNextChanged = true
	} else {
		// Check whether something changed
		next := context.pageMaker[index+1]
		// (nextResumeAt, nextNextPage, nextRightPage,nextPageState, )
		pageMakerNextChanged = !next.ResumeAt.Equals(resumeAt) ||
			next.NextPage != nextPage ||
			next.RightPage != tmp.RightPage ||
			!next.PageState.Equal(pageState)
	}

	if pageMakerNextChanged {
		// Reset remakeState
		remakeState := tree.RemakeState{}
		// Setting contentChanged to true ensures remake.
		// If resumeAt  == nil  (last page) it must be false to prevent endless
		// loops and list index out of range (see #794).
		remakeState.ContentChanged = resumeAt != nil
		item := tree.PageMaker{
			ResumeAt: resumeAt, NextPage: nextPage, RightPage: tmp.RightPage,
			PageState: pageState, RemakeState: remakeState,
		}
		if index+1 >= len(context.pageMaker) {
			context.pageMaker = append(context.pageMaker, item)
		} else {
			context.pageMaker[index+1] = item
		}
	}

	return page, resumeAt
}

// Return a list of laid out pages without margin boxes.
// Re-make pages only if necessary.
func (context *layoutContext) makeAllPages(rootBox bo.BlockLevelBoxITF, html *tree.HTML, pages []*bo.PageBox) []*bo.PageBox {
	var (
		out               []*bo.PageBox
		i                 = 0
		reportedFootnotes []Box
		pageGroups        []pageGroup
	)

	stalledPages := 0 // kolejne strony bez postępu
	for {
		remakeState := context.pageMaker[i].RemakeState
		var (
			resumeAt tree.ResumeStack
			page     *bo.PageBox
		)
		if len(pages) == 0 || remakeState.ContentChanged || remakeState.PagesWanted {
			logger.ProgressLogger.Printf("Step 5 - Creating layout - Page %d", i+1)
			// Reset remakeState
			context.pageMaker[i].RemakeState = tree.RemakeState{}
			page, resumeAt = context.remakePage(i, &pageGroups, rootBox, html)
			reportedFootnotes = context.reportedFootnotes
			out = append(out, page)
		} else {
			logger.ProgressLogger.Printf("Step 5 - Creating layout - Page %d (up-to-date)", i+1)
			resumeAt = context.pageMaker[i+1].ResumeAt
			reportedFootnotes = nil
			out = append(out, pages[i])
		}

		i += 1
		// gopdfgo: bezpiecznik. Strona, która kończy się dokładnie tam, gdzie się zaczęła, nie zrobiła
		// postępu; jedna taka strona bywa uprawniona (pusta strona przed break-before: right, strona
		// z samym dalszym ciągiem pudełek poza przepływem), ale trzy z rzędu oznaczają pętlę. Kończymy
		// wtedy dokument z ostrzeżeniem, zamiast tworzyć strony w nieskończoność.
		if resumeAt != nil && resumeAt.Equals(context.pageMaker[i-1].ResumeAt) {
			stalledPages++
			if stalledPages >= 3 {
				logger.WarningLogger.Printf("układ nie robi postępu od strony %d; pozostała treść została pominięta", i-2)
				resumeAt, reportedFootnotes = nil, nil
			}
		} else {
			stalledPages = 0
		}
		if resumeAt == nil && len(reportedFootnotes) == 0 {
			// Throw away obsolete pages and content
			context.pageMaker = context.pageMaker[:i+1]
			for k := range context.brokenOutOfFlow {
				delete(context.brokenOutOfFlow, k)
			}
			context.reportedFootnotes = context.reportedFootnotes[:0]
			return out
		}
	}
}
