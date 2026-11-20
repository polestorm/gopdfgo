package document

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"text/template"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/matrix"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/text/hyphen"

	"github.com/polestorm/gopdfgo/internal/webrender/html/layout"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/images"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"

	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
)

// Take an "after layout" box tree and draw it onto a cairo context.

const (
	bottom pr.KnownProp = iota
	left
	right
	top
)

var sides = [4]pr.KnownProp{top, right, bottom, left}

const (
	pi = math.Pi

	headerSVG = `
	<svg height="{{ .Height }}" width="{{ .Width }}"
		 fill="transparent" stroke="black" stroke-width="1"
		 xmlns="http://www.w3.org/2000/svg"
		 xmlns:xlink="http://www.w3.org/1999/xlink">
  	`

	crop = `
  <!-- horizontal top left -->
  <path d="M0,{{ .Bleed.Top }} h{{ .HalfBleed.Left }}" />
  <!-- horizontal top right -->
  <path d="M0,{{ .Bleed.Top }} h{{ .HalfBleed.Right }}"
        transform="translate({{ .Width }},0) scale(-1,1)" />
  <!-- horizontal bottom right -->
  <path d="M0,{{ .Bleed.Bottom }} h{{ .HalfBleed.Right }}"
        transform="translate({{ .Width }},{{ .Height }}) scale(-1,-1)" />
  <!-- horizontal bottom left -->
  <path d="M0,{{ .Bleed.Bottom }} h{{ .HalfBleed.Left }}"
        transform="translate(0,{{ .Height }}) scale(1,-1)" />
  <!-- vertical top left -->
  <path d="M{{ .Bleed.Left }},0 v{{ .HalfBleed.Top }}" />
  <!-- vertical bottom right -->
  <path d="M{{ .Bleed.Right }},0 v{{ .HalfBleed.Bottom }}"
        transform="translate({{ .Width }},{{ .Height }}) scale(-1,-1)" />
  <!-- vertical bottom left -->
  <path d="M{{ .Bleed.Left }},0 v{{ .HalfBleed.Bottom }}"
        transform="translate(0,{{ .Height }}) scale(1,-1)" />
  <!-- vertical top right -->
  <path d="M{{ .Bleed.Right }},0 v{{ .HalfBleed.Top }}"
        transform="translate({{ .Width }},0) scale(-1,1)" />
`
	cross = `
  <!-- top -->
  <circle r="{{ .HalfBleed.Top }}"
          transform="scale(0.5)
                     translate({{ .Width }},{{ .HalfBleed.Top }}) scale(0.5)" />
  <path d="M-{{ .HalfBleed.Top }},{{ .HalfBleed.Top }} h{{ .Bleed.Top }}
           M0,0 v{{ .Bleed.Top }}"
        transform="scale(0.5) translate({{ .Width }},0)" />
  <!-- bottom -->
  <circle r="{{ .HalfBleed.Bottom }}"
          transform="translate(0,{{ .Height }}) scale(0.5)
                     translate({{ .Width }},-{{ .HalfBleed.Bottom }}) scale(0.5)" />
  <path d="M-{{ .HalfBleed.Bottom }},-{{ .HalfBleed.Bottom }} h{{ .Bleed.Bottom }}
           M0,0 v-{{ .Bleed.Bottom }}"
        transform="translate(0,{{ .Height }}) scale(0.5) translate({{ .Width }},0)" />
  <!-- left -->
  <circle r="{{ .HalfBleed.Left }}"
          transform="scale(0.5)
                     translate({{ .HalfBleed.Left }},{{ .Height }}) scale(0.5)" />
  <path d="M{{ .HalfBleed.Left }},-{{ .HalfBleed.Left }} v{{ .Bleed.Left }}
           M0,0 h{{ .Bleed.Left }}"
        transform="scale(0.5) translate(0,{{ .Height }})" />
  <!-- right -->
  <circle r="{{ .HalfBleed.Right }}"
          transform="translate({{ .Width }},0) scale(0.5)
                     translate(-{{ .HalfBleed.Right }},{{ .Height }}) scale(0.5)" />
  <path d="M-{{ .HalfBleed.Right }},-{{ .HalfBleed.Right }} v{{ .Bleed.Right }}
           M0,0 h-{{ .Bleed.Right }}"
        transform="translate({{ .Width }},0)
                   scale(0.5) translate(0,{{ .Height }})" />
`
)

type svgArgs struct {
	Width, Height    fl
	Bleed, HalfBleed bo.Bleed
}

// text layout is needed by SVG images
var _ text.TextLayoutContext = drawContext{}

type drawContext struct {
	dst   backend.Canvas
	fonts text.FontConfiguration

	hyphenCache       map[text.HyphenDictKey]hyphen.Hyphener
	strutLayoutsCache map[text.StrutLayoutKey][2]pr.Float

	// gopdfgo: borderMask – drawBorder rysuje kształt obramowania na biało, bez obrazu obramowania,
	// jako maskę dla background-clip: border-area
	borderMask bool

	// gopdfgo: filteredImages – obrazy rastrowe po przekształceniu barw filtra CSS (zob.
	// filterRasterImage), według obrazu i przekształcenia
	filteredImages map[string]*filteredImage

	// gopdfgo: context3D – kontekst renderowania 3D rodzica z transform-style: preserve-3d (nil poza
	// nim), zob. drawStackingContext
	context3D *context3D

	// gopdfgo: pageMark – wywoływane w kontekście nakładania strony po narysowaniu wszystkich jego
	// dzieci, przed zamknięciem stanów; tam rysowanie w trakcie układu dorysowuje pola marginesowe
	// (ADR 0027, zob. streamSink)
	pageMark func(ctx drawContext)
}

// context3D (gopdfgo): skumulowana macierz 3D elementu z transform-style: preserve-3d i macierz 2D
// nałożona na rysunek od rodzica korzenia kontekstu renderowania 3D (CSS Transforms 2 § 6).
type context3D struct {
	full    matrix.Matrix4
	applied matrix.Transform
}

func (ctx drawContext) Fonts() text.FontConfiguration { return ctx.fonts }

func (ctx drawContext) HyphenCache() map[text.HyphenDictKey]hyphen.Hyphener {
	return ctx.hyphenCache
}

func (ctx drawContext) StrutLayoutsCache() map[text.StrutLayoutKey][2]pr.Float {
	return ctx.strutLayoutsCache
}

// Draw the given PageBox.
func (ctx drawContext) drawPage(page *bo.PageBox) {
	// gopdfgo: dokument, którego element główny ma display: none, nie ma czego drukować: strona zostaje
	// pusta, bez tła, obramowania i pól marginesowych @page (norma tego nie rozstrzyga; tak drukuje
	// Chromium, test WPT css-page/root-element-display-none-print)
	for _, child := range page.Children {
		if child.Box().RootDisplayNone {
			return
		}
	}
	marks := page.Style.GetMarks()
	stackingContext := NewStackingContextFromPage(page)
	ctx.drawBackground(stackingContext.box.Box().Background, false, page.Bleed(), marks)
	// gopdfgo: pola marginesowe o ujemnym z-index leżą nad tłem strony, ale pod płótnem dokumentu
	// (CSS Page 3 § 3.1: „may only paint in front of the document content or behind the document
	// canvas”). W źródle rysowały się nad płótnem, razem z treścią.
	for _, behind := range stackingContext.negativeZContexts {
		ctx.drawStackingContext(behind)
	}
	stackingContext.negativeZContexts = nil
	ctx.setMaskBorder(page)
	ctx.drawBackground(page.CanvasBackground, false, bo.Bleed{}, pr.Marks{})
	ctx.drawBorder(page)
	ctx.drawStackingContext(stackingContext)
}

// Draw a “stackingContext“ on “context“.
func (ctx drawContext) drawStackingContext(stackingContext StackingContext) {
	// See http://www.w3.org/TR/CSS2/zindex.html
	ctx.dst.OnNewStack(func() {
		box_ := stackingContext.box
		box := box_.Box()

		// gopdfgo: pas pudełka krojonego między strony jako całość (zob. BoxFields.SliceVisible): całość
		// przycięta do części widocznej na tej stronie
		if box.SliceVisible > 0 {
			ctx.dst.Rectangle(-1e5, fl(box.PositionY+box.SliceOffset), 2e5, fl(box.SliceVisible))
			ctx.dst.State().Clip(false)
		}

		// apply the viewport_overflow to the html box, see #35
		if box.IsForRootElement && (stackingContext.page.Style.GetOverflow() != "visible") {
			roundedBox(
				ctx.dst, stackingContext.page.RoundedPaddingBox())
			ctx.dst.State().Clip(false)
		}

		if clips := box.Style.GetClip(); box.IsAbsolutelyPositioned() && len(clips) != 0 {
			top, right, bottom, left := clips[0], clips[1], clips[2], clips[3]
			// gopdfgo: rect(top, right, bottom, left) podaje przesunięcia od górnej (top, bottom) i lewej
			// (right, left) krawędzi obramowania, a auto oznacza krawędź pudełka (CSS 2.2 § 11.1.2).
			// Źródło zamieniało right z left: prostokąt zaczynał się w x = right i miał szerokość left − right.
			if top.Tag == pr.Auto {
				top.Value = 0
			}
			if right.Tag == pr.Auto {
				right.Value = box.BorderWidth()
			}
			if bottom.Tag == pr.Auto {
				bottom.Value = box.BorderHeight()
			}
			if left.Tag == pr.Auto {
				left.Value = 0
			}
			// prostokąt o ujemnej szerokości albo wysokości jest pusty (CSS Masking 1 § 9)
			ctx.dst.Rectangle(
				fl(box.BorderBoxX()+left.Value),
				fl(box.BorderBoxY()+top.Value),
				fl(max(0, right.Value-left.Value)),
				fl(max(0, bottom.Value-top.Value)),
			)
			ctx.dst.State().Clip(false)
		}

		originalDst := ctx.dst
		opacity := fl(box.Style.GetOpacity())
		if opacity < 1 { // we draw all the following to a separate group
			// gopdfgo: grupa krycia nie przycina treści do pudełka obramowania: opacity dotyczy elementu
			// razem z potomkami wystającymi poza niego (CSS Color 4 § 15.1); źródło dawało grupie ramkę
			// pudełka obramowania, więc wystające potomki i cienie znikały
			ctx.dst = newFilterGroup(ctx.dst)
		}

		mat, hasMatrix, own3D := getMatrixAndFace(box_)
		// gopdfgo: kontekst renderowania 3D (CSS Transforms 2 § 6). Element, którego rodzic ma
		// transform-style: preserve-3d, ma skumulowaną macierz 3D: macierz rodzica razy własną. Bez
		// perspektywy obraz takiego elementu to rzut prostokątny tej macierzy, więc rysujemy go macierzą
		// względną: odwrotność macierzy już nałożonej w kontekście razy spłaszczona macierz skumulowana.
		// Dzięki temu np. dwa rotateX(45deg) w preserve-3d dają rotateX(90deg), a nie iloczyn dwóch
		// spłaszczeń. Nie odtwarzamy sortowania po głębokości ani przecinania płaszczyzn: kolejność
		// rysowania zostaje jak w drzewie. W źródle nieznane.
		accumulated := own3D
		applied := mat // macierz nałożona przez ten element, licząc od rodzica korzenia kontekstu 3D
		if !hasMatrix {
			applied = matrix.Identity()
		}
		in3D := stackingContext.parent3D && ctx.context3D != nil
		if in3D {
			accumulated = matrix.Mul4(ctx.context3D.full, own3D)
			applied = accumulated.Flatten()
			if !accumulated.Invertible() {
				applied = matrix.Transform{}
			}
			parent := ctx.context3D.applied
			if err := parent.Invert(); err == nil {
				mat, hasMatrix = matrix.Mul(parent, applied), true
			}
		}
		preserve3D := box.Style.GetTransformStyle() == "preserve-3d"
		// hideOwn: element nie rysuje własnej treści, ale rysuje uczestników swojego kontekstu 3D
		hideOwn := false
		// gopdfgo: backface-visibility: hidden ukrywa element, gdy widać jego tylną stronę według
		// skumulowanej macierzy 3D (CSS Transforms 2 § 11); w źródle nieznane. Element płaski znika z
		// potomkami (są spłaszczeni w jego płaszczyźnie), element preserve-3d tylko z własną treścią.
		if box.Style.GetBackfaceVisibility() == "hidden" && accumulated.BackfaceVisible() {
			if !preserve3D {
				return
			}
			hideOwn = true
		}
		// (wyznacznik bliski zera: cos(90deg) w liczbach zmiennoprzecinkowych nie daje dokładnie zera)
		if preserve3D && hasMatrix && math.Abs(float64(mat.Determinant())) < 1e-6 && accumulated.Invertible() {
			// płaszczyzna elementu widoczna z boku (np. rotateX(90deg)): jego treści nie widać, ale
			// uczestnicy kontekstu 3D mogą mieć macierz odwracalną; nie nakładamy macierzy osobliwej
			hideOwn, hasMatrix = true, false
			if ctx.context3D != nil && in3D {
				applied = ctx.context3D.applied
			} else {
				applied = matrix.Identity()
			}
		}
		ctx.context3D = nil
		if preserve3D {
			ctx.context3D = &context3D{full: accumulated, applied: applied}
		}
		if hasMatrix {
			if mat.Determinant() != 0 {
				ctx.dst.State().Transform(mat)
			} else {
				logger.WarningLogger.Printf("non invertible transformation matrix %v\n", mat)
				return
			}
		}
		// gopdfgo: clip-path przycina element razem z potomkami w jego układzie po przekształceniu
		// (CSS Masking 1 § 5.1)
		ctx.applyClipPath(box)

		// gopdfgo: treść elementu (punkty 2–10) rysujemy przez etapy właściwości filter (draw_filter.go),
		// w układzie elementu po przekształceniu; filtr działa przed przycięciem (clip, clip-path)
		// i kryciem (Filter Effects 1 § 5, CSS Masking 1 § 3: „filter, clipping, masking, opacity”).
		paint := func(ctx drawContext) {
			// gopdfgo: przy hideOwn rysujemy tylko uczestników kontekstu 3D elementu
			drawChild := func(child StackingContext) {
				if !hideOwn || child.parent3D {
					ctx.drawStackingContext(child)
				}
			}

			// Point 1 is done in drawPage

			// Point 2
			// gopdfgo: przy hideOwn (zob. wyżej) bez własnej treści elementu
			if !hideOwn && (bo.BlockT.IsInstance(box_) || bo.MarginT.IsInstance(box_) ||
				bo.InlineBlockT.IsInstance(box_) || bo.TableCellT.IsInstance(box_) ||
				bo.FlexContainerT.IsInstance(box_) || bo.GridContainerT.IsInstance(box_) || bo.ReplacedT.IsInstance(box_)) {
				ctx.setMaskBorder(box_)
				// The canvas background was removed by layout_backgrounds.
				ctx.drawBoxShadows(box_.Box(), false)
				ctx.drawBackgroundDefaut(box_.Box().Background)
				ctx.drawBoxShadows(box_.Box(), true)
				ctx.drawBorder(box_)
			}

			// gopdfgo: tabela, która sama tworzy kontekst nakładania (overflow, opacity, transform, z-index
			// na pudełku tabeli), rysuje tu swoje tła: własne, kolumn, wierszy i komórek. W źródle tabela
			// była rysowana tylko jako dziecko cudzego kontekstu, więc w tym przypadku tła komórek ginęły.
			rootTable, rootIsTable := box_.(bo.TableBoxITF)
			rootIsTable = rootIsTable && !hideOwn
			if rootIsTable {
				ctx.setMaskBorder(box_)
				ctx.drawTable(rootTable.Table())
			}

			ctx.dst.OnNewStack(func() {
				// dont clip the PageBox, see #35
				if bo.ClipsOverflow(box_) && !bo.PageT.IsInstance(box_) {
					// Only clip the content and the children:
					// - the background is already clipped
					// - the border must *not* be clipped
					roundedBox(ctx.dst, box.RoundedPaddingBox())
					ctx.dst.State().Clip(false)
				}

				// Point 3
				for _, childContext := range stackingContext.negativeZContexts {
					drawChild(childContext)
				}

				// Point 4
				// gopdfgo: obramowania zwinięte tabeli są rysowane po tłach i obramowaniach pudełek blokowych
				// leżących w jej komórkach, ale przed pudełkami następującymi po tabeli (CSS Tables 3 § 5.3.3,
				// testy WPT collapsed-borders-painting-order-*, collapsed-border-paint-phase-002). W źródle
				// były rysowane razem z tłem tabeli, więc blok albo tabela w komórce zasłaniały obramowanie.
				type openTable struct {
					box         Box
					descendants map[*bo.BoxFields]bool // według pól pudełka: nie każda wartość Box jest haszowalna
				}
				var openTables []openTable
				closeTables := func(next Box) {
					for len(openTables) != 0 {
						top := openTables[len(openTables)-1]
						if next != nil && top.descendants[next.Box()] {
							return
						}
						openTables = openTables[:len(openTables)-1]
						ctx.setMaskBorder(top.box)
						ctx.drawCollapsedBorders(top.box.(bo.TableBoxITF).Table())
					}
				}
				var paintRoot Box
				if rootIsTable {
					paintRoot = box_
				}
				blocks := orderTablePaint(stackingContext.blockLevelBoxes, paintRoot)
				if hideOwn {
					blocks = nil
				}
				for _, item := range blocks {
					block := item.box
					closeTables(block)
					if item.table != nil {
						// odłożony nagłówek lub stopka tabeli (zob. orderTablePaint)
						ctx.setMaskBorder(item.table)
						ctx.drawTableGroup(item.table.(bo.TableBoxITF).Table(), block)
						continue
					}
					ctx.setMaskBorder(block)
					if box_, ok := block.(bo.TableBoxITF); ok {
						ctx.drawTable(box_.Table())
						if table := box_.Table(); table.Style.GetBorderCollapse() == "collapse" && table.Style.GetVisibility() == "visible" {
							descendants := map[*bo.BoxFields]bool{}
							var collect func(Box)
							collect = func(parent Box) {
								for _, child := range parent.Box().Children {
									switch child.(type) {
									case StackingContext, *StackingContext:
										continue // osobny kontekst warstw: rysowany poza tą pętlą
									}
									descendants[child.Box()] = true
									collect(child)
								}
							}
							collect(block)
							openTables = append(openTables, openTable{block, descendants})
						}
					} else {
						ctx.drawBoxShadows(block.Box(), false)
						ctx.drawBackgroundDefaut(block.Box().Background)
						ctx.drawBoxShadows(block.Box(), true)
						ctx.drawBorder(block)
					}
				}
				closeTables(nil)
				if rootIsTable {
					if table := rootTable.Table(); table.Style.GetBorderCollapse() == "collapse" && table.Style.GetVisibility() == "visible" {
						ctx.drawCollapsedBorders(table)
					}
				}

				// Point 5
				for _, childContext := range stackingContext.floatContexts {
					drawChild(childContext)
				}

				// Point 6
				if bo.InlineT.IsInstance(box_) && !hideOwn {
					ctx.drawInlineLevel(stackingContext.page, box_, 0, "clip", pr.TaggedString{Tag: pr.None})
				}

				// Point 7
				if !hideOwn {
					root := newBoxTree()
					root.set(box_, stackingContext.blocksAndCells)
					ctx.drawBlockLevel(stackingContext.page, root)
				}

				// Point 8
				for _, childContext := range stackingContext.zeroZContexts {
					drawChild(childContext)
				}

				// Point 9
				for _, childContext := range stackingContext.positiveZContexts {
					drawChild(childContext)
				}

				// gopdfgo: miejsce na pola marginesowe dorysowywane po ułożeniu wszystkich stron (ADR 0027)
				if ctx.pageMark != nil && bo.PageT.IsInstance(box_) {
					ctx.pageMark(ctx)
				}
			})

			// Point 10
			if !hideOwn {
				ctx.drawOutline(box_)
			}
		}
		ctx.drawFiltered(filterStages(box.Style), paint)

		if opacity < 1 {
			group := ctx.dst
			ctx.dst = originalDst
			ctx.dst.OnNewStack(func() {
				ctx.dst.DrawWithOpacity(opacity, group)
			})
		}
	})
}

func formatSVG(svg string, data svgArgs) (string, error) {
	tmp, err := template.New("svg").Parse(svg)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := tmp.Execute(&b, data); err != nil {
		return "", fmt.Errorf("unexpected template error : %s", err)
	}
	return b.String(), nil
}

func reversed(in []bo.BackgroundLayer) []bo.BackgroundLayer {
	N := len(in)
	out := make([]bo.BackgroundLayer, N)
	for i, v := range in {
		out[N-1-i] = v
	}
	return out
}

func (ctx drawContext) drawBackgroundDefaut(bg *bo.Background) {
	ctx.drawBackground(bg, true, bo.Bleed{}, pr.Marks{})
}

// Draw the background color and image
// If “clipBox“ is set to “false“, the background is not clipped to the
// border box of the background, but only to the painting area
// clipBox=true bleed=nil marks=()
// zeroPaintingArea (gopdfgo): obszar malowania koloru tła ma zerową szerokość albo wysokość.
func zeroPaintingArea(bg *bo.Background) bool {
	if len(bg.Layers) == 0 {
		return false
	}
	_, _, width, height := bg.Layers[len(bg.Layers)-1].PaintingArea.Unpack()
	return width <= 0 || height <= 0
}

func (ctx drawContext) drawBackground(bg *bo.Background, clipBox bool, bleed bo.Bleed, marks pr.Marks) {
	if bg == nil {
		return
	}

	// gopdfgo: każda warstwa jest przycinana do swojego background-clip, a kolor tła do background-clip
	// ostatniej warstwy (CSS Backgrounds 3 § 3.7). Źródło przycinało całe tło do pudełka ostatniej
	// warstwy, więc np. przy background-clip: border-box, content-box pierwszy obraz nie wychodził
	// poza treść (test WPT clip-border-area-multiple-backgrounds).
	clipLayer := func(layer bo.BackgroundLayer) {
		if !clipBox {
			return
		}
		for _, box := range layer.ClippedBoxes {
			roundedBox(ctx.dst, box)
		}
		ctx.dst.State().Clip(false)
	}
	ctx.dst.OnNewStack(func() {
		if bg.TextClip != nil {
			ctx.setTextMask(bg.TextClip, false)
		}
		// gopdfgo: warstwy z background-blend-mode mieszają się tylko z tłem tego samego elementu
		// (CSS Compositing 1 § 3.2: tło elementu tworzy izolowaną grupę), więc całe tło rysujemy wtedy
		// do osobnej, izolowanej grupy przezroczystości
		if blendGroup := backgroundBlendGroup(bg); blendGroup != nil {
			outer := ctx.dst
			ctx.dst = outer.NewGroup(blendGroup.Unpack())
			defer func() {
				group := ctx.dst
				ctx.dst = outer
				outer.DrawWithOpacity(1, group)
			}()
		}

		// Background color
		// gopdfgo: obszar malowania o zerowej wysokości albo szerokości nie ma pola, więc tło nie jest
		// malowane (CSS Backgrounds 3 § 3.7, background-clip); w źródle trafiał do PDF prostokąt zerowej
		// wysokości, który przeglądarki PDF rysują jako linię włosową (test WPT
		// no-clearance-due-to-large-margin-after-left-right).
		if bg.Color.A > 0 && !zeroPaintingArea(bg) {
			ctx.dst.OnNewStack(func() {
				clipLayer(bg.Layers[len(bg.Layers)-1])
				if last := bg.Layers[len(bg.Layers)-1]; last.BorderArea && last.Text {
					ctx.setTextMask(bg.Owner, true)
				} else if last.BorderArea {
					ctx.setBorderAreaMask(bg.Owner)
				}
				ctx.dst.State().SetColorRgba(bg.Color, false)
				paintingArea := bg.Layers[len(bg.Layers)-1].PaintingArea
				ctx.dst.Rectangle(paintingArea.Unpack())
				ctx.dst.State().Clip(false)
				ctx.dst.Rectangle(paintingArea.Unpack())
				ctx.dst.Paint(backend.FillNonZero)
			})
		}

		if (bleed != bo.Bleed{}) && !marks.IsNone() {
			x, y, width, height := bg.Layers[len(bg.Layers)-1].PaintingArea.Unpack()
			svg := headerSVG
			if marks.Crop {
				svg += crop
			}
			if marks.Cross {
				svg += cross
			}
			svg += "</svg>"
			halfBleed := bo.Bleed{
				Top:    bleed.Top * 0.5,
				Bottom: bleed.Bottom * 0.5,
				Left:   bleed.Left * 0.5,
				Right:  bleed.Right * 0.5,
			}
			svg, err := formatSVG(svg, svgArgs{Width: width, Height: height, Bleed: bleed, HalfBleed: halfBleed})
			if err != nil {
				logger.WarningLogger.Println(err)
				return
			}
			image, err := images.NewSVGImage(strings.NewReader(svg), "", nil)
			if err != nil {
				logger.WarningLogger.Println(err)
				return
			}

			// Painting area is the PDF media box
			size := [2]pr.Float{pr.Float(width), pr.Float(height)}
			position := bo.Position{Point: bo.MaybePoint{pr.Float(x), pr.Float(y)}}
			repeat := bo.Repeat{Reps: [2]string{"no-repeat", "no-repeat"}}
			unbounded := true
			paintingArea := pr.Rectangle{pr.Float(x), pr.Float(y), pr.Float(width), pr.Float(height)}
			positioningArea := pr.Rectangle{0, 0, pr.Float(width), pr.Float(height)}
			layer := bo.BackgroundLayer{
				Image: image, Size: size, Position: position, Repeat: repeat, Unbounded: unbounded,
				PaintingArea: paintingArea, PositioningArea: positioningArea,
			}
			bg.Layers = append([]bo.BackgroundLayer{layer}, bg.Layers...)
		}
		// Paint in reversed order: first layer is "closest" to the viewer.
		for _, layer := range reversed(bg.Layers) {
			if layer.Image == nil {
				continue
			}
			ctx.dst.OnNewStack(func() {
				clipLayer(layer)
				if layer.BorderArea && layer.Text {
					ctx.setTextMask(bg.Owner, true)
				} else if layer.BorderArea {
					ctx.setBorderAreaMask(bg.Owner)
				}
				if layer.BlendMode != "" && layer.BlendMode != "normal" {
					ctx.dst.State().SetBlendingMode(layer.BlendMode)
				}
				ctx.drawBackgroundImage(layer, bg.ImageRendering)
			})
		}
	})
}

// backgroundBlendGroup (gopdfgo) zwraca obszar grupy dla tła z warstwą mieszaną trybem innym niż
// normal (suma obszarów malowania warstw) albo nil, gdy żadna warstwa się nie miesza.
func backgroundBlendGroup(bg *bo.Background) *pr.Rectangle {
	blends := false
	for _, layer := range bg.Layers {
		if layer.Image != nil && layer.BlendMode != "" && layer.BlendMode != "normal" {
			blends = true
		}
	}
	if !blends {
		return nil
	}
	area := bg.Layers[0].PaintingArea
	for _, layer := range bg.Layers[1:] {
		x0, y0 := min(area[0], layer.PaintingArea[0]), min(area[1], layer.PaintingArea[1])
		x1 := max(area[0]+area[2], layer.PaintingArea[0]+layer.PaintingArea[2])
		y1 := max(area[1]+area[3], layer.PaintingArea[1]+layer.PaintingArea[3])
		area = pr.Rectangle{x0, y0, x1 - x0, y1 - y0}
	}
	return &area
}

// setTextMask (gopdfgo) ustawia maskę przezroczystości z tekstu pudełka: wszystko, co zostanie potem
// namalowane w bieżącym stanie graficznym, jest widoczne tylko w kształcie liter (background-clip: text).
// Tekst jest rysowany do osobnej grupy na biało, niezależnie od własnego koloru, który zwykle
// jest przezroczysty.
// withBorder dokłada do maski kształt obramowania (background-clip: border-area text: suma obszarów).
func (ctx *drawContext) setTextMask(box_ Box, withBorder bool) {
	rbb := box_.Box().RoundedBorderBox()
	current := ctx.dst
	ctx.dst = ctx.dst.NewGroup(fl(rbb.X), fl(rbb.Y), fl(rbb.Width), fl(rbb.Height))
	// maska jest liczona z jasności: biały tekst odsłania, czarne tło grupy zasłania (tło malujemy
	// wprost, bo przeglądarki PDF różnie traktują puste miejsce w grupie maski)
	ctx.dst.State().SetColorRgba(Color{R: 0, G: 0, B: 0, A: 1}, false)
	ctx.dst.Rectangle(fl(rbb.X), fl(rbb.Y), fl(rbb.Width), fl(rbb.Height))
	ctx.dst.Paint(backend.FillNonZero)
	ctx.dst.State().SetColorRgba(Color{R: 1, G: 1, B: 1, A: 1}, false)
	// gopdfgo: text-overflow i block-ellipsis wiersza obowiązują też w masce, żeby kształt tekstu
	// zgadzał się z tekstem rysowanym zwyczajnie (test WPT clip-text-ellipsis); źródło dawało "clip".
	var walk func(Box)
	textOverflow, blockEllipsis := "clip", pr.TaggedString{Tag: pr.None}
	walk = func(b Box) {
		if lineBox, ok := b.(*bo.LineBox); ok {
			textOverflow, blockEllipsis = lineBox.TextOverflow, lineBox.BlockEllipsis
		}
		if textbox, ok := b.(*bo.TextBox); ok {
			if textbox.Style.GetVisibility() == "visible" {
				textbox.TextLayout.ApplyJustification()
				ctx.drawFirstLine(textbox, textOverflow, blockEllipsis,
					pr.Fl(textbox.PositionX), pr.Fl(textbox.PositionY+textbox.Baseline.V()), Color{R: 1, G: 1, B: 1, A: 1})
				// gopdfgo: maska obejmuje też dekoracje tekstu (drawTextDecorationsInColor)
				ctx.drawTextDecorationsInColor(textbox, Color{R: 1, G: 1, B: 1, A: 1})
				ctx.dst.State().SetColorRgba(Color{R: 1, G: 1, B: 1, A: 1}, false)
			}
			return
		}
		for _, child := range b.Box().Children {
			walk(child)
		}
	}
	walk(box_)
	if withBorder {
		mask := *ctx
		mask.borderMask = true
		mask.drawBorder(box_)
	}
	current.State().SetAlphaMask(ctx.dst)
	ctx.dst = current
}

// setBorderAreaMask (gopdfgo) ustawia maskę przezroczystości w kształcie obramowania pudełka: to, co
// zostanie potem namalowane w bieżącym stanie graficznym, jest widoczne tylko tam, gdzie obramowanie
// maluje, z uwzględnieniem szerokości i stylu, ale bez przezroczystości koloru i bez border-image
// (background-clip: border-area, CSS Backgrounds 4 § 3.6).
// Obramowanie pełne (solid, inset, outset, groove, ridge) albo podwójne na wszystkich bokach daje
// ścieżkę przycinania z pierścieni zamiast maski: krawędzie wypadają wtedy dokładnie tam, gdzie krawędzie
// samego obramowania, bez wygładzania maski.
func (ctx *drawContext) setBorderAreaMask(box_ Box) {
	box := box_.Box()
	if box.Style.GetVisibility() != "visible" {
		ctx.dst.Rectangle(0, 0, 0, 0)
		ctx.dst.State().Clip(false)
		return
	}
	full, double := true, true
	widths := [4]pr.Float{box.BorderTopWidth.V(), box.BorderRightWidth.V(), box.BorderBottomWidth.V(), box.BorderLeftWidth.V()}
	for i, side := range sides {
		style := box.Style.Get((pr.PBorderBottomStyle + side*5).Key()).(pr.String)
		if widths[i] == 0 {
			continue
		}
		switch style {
		case "solid", "inset", "outset", "groove", "ridge":
			double = false
		case "double":
			full = false
		case "none", "hidden":
		default:
			full, double = false, false
		}
	}
	if full || double {
		roundedBox(ctx.dst, box.RoundedPaddingBox())
		if !full {
			roundedBox(ctx.dst, box.RoundedBoxRatio(1./3))
			roundedBox(ctx.dst, box.RoundedBoxRatio(2./3))
		}
		roundedBox(ctx.dst, box.RoundedBorderBox())
		ctx.dst.State().Clip(true)
		return
	}
	rbb := box.RoundedBorderBox()
	current := ctx.dst
	mask := *ctx
	mask.dst = ctx.dst.NewGroup(fl(rbb.X), fl(rbb.Y), fl(rbb.Width), fl(rbb.Height))
	mask.borderMask = true
	mask.dst.State().SetColorRgba(Color{R: 0, G: 0, B: 0, A: 1}, false)
	mask.dst.Rectangle(fl(rbb.X), fl(rbb.Y), fl(rbb.Width), fl(rbb.Height))
	mask.dst.Paint(backend.FillNonZero)
	mask.drawBorder(box_)
	current.State().SetAlphaMask(mask.dst)
}

func (ctx drawContext) drawBackgroundImage(layer bo.BackgroundLayer, imageRendering pr.String) {
	if layer.Image == nil || layer.Size[0] == 0 || layer.Size[1] == 0 {
		return
	}

	paintingX, paintingY, paintingWidth, paintingHeight := layer.PaintingArea.Unpack()
	positioningX, positioningY, positioningWidth, positioningHeight := layer.PositioningArea.Unpack()
	positionX, positionY := layer.Position.Point[0], layer.Position.Point[1]
	repeatX, repeatY := layer.Repeat.Reps[0], layer.Repeat.Reps[1]
	imageWidth, imageHeight := pr.Fl(layer.Size[0]), pr.Fl(layer.Size[1])
	var repeatWidth, repeatHeight pr.Fl
	var singleX, singleY bool // obraz rysowany raz w danej osi (zob. niżej)
	switch repeatX {
	case "no-repeat":
		// We want at least the whole image_width drawn on sub_surface, but we
		// want to be sure it will not be repeated on the painting_width. We
		// double the painting width to ensure viewers don't incorrectly bleed
		// the edge of the pattern into the painting area. (See #1539.)
		repeatWidth, singleX = utils.Maxs(imageWidth, 2*paintingWidth), true
	case "repeat", "round":
		// We repeat the image each imageWidth.
		repeatWidth = imageWidth
	case "space":
		nRepeats := pr.Fl(math.Floor(float64(positioningWidth / imageWidth)))
		if nRepeats >= 2 {
			// The repeat width is the whole positioning width with one image
			// removed, divided by (the number of repeated images - 1). This
			// way, we get the width of one image + one space. We ignore
			// background-position for this dimension.
			repeatWidth = (positioningWidth - imageWidth) / (nRepeats - 1)
			positionX = pr.Float(0)
		} else {
			// gopdfgo: gdy nie mieszczą się dwie kopie, rysujemy jeden obraz w miejscu z background-position
			// (CSS Backgrounds 3 § 3.4, space). Źródło powtarzało go co szerokość obszaru pozycjonowania.
			repeatWidth, singleX = utils.Maxs(imageWidth, 2*paintingWidth), true
		}
	default:
		panic(fmt.Sprintf("unexpected repeatX %s", repeatX))
	}

	// Comments above apply here too.
	switch repeatY {
	case "no-repeat":
		repeatHeight, singleY = utils.Maxs(imageHeight, 2*paintingHeight), true
	case "repeat", "round":
		repeatHeight = imageHeight
	case "space":
		nRepeats := fl(math.Floor(float64(positioningHeight / imageHeight)))
		if nRepeats >= 2 {
			repeatHeight = (positioningHeight - imageHeight) / (nRepeats - 1)
			positionY = pr.Float(0)
		} else {
			repeatHeight, singleY = utils.Maxs(imageHeight, 2*paintingHeight), true // jak wyżej
		}
	default:
		panic(fmt.Sprintf("unexpected repeatY %s", repeatY))
	}

	X := pr.Fl(positionX.V()) + positioningX
	Y := pr.Fl(positionY.V()) + positioningY

	// gopdfgo: obraz bez powtarzania leżący daleko od obszaru malowania (tło płótna na dalszych stronach
	// ma początek na pierwszej stronie, kilka wysokości strony wyżej) powtarzał się na obszarze, bo kafel
	// miał tylko dwie wysokości obszaru. Kafel musi sięgać od obrazu przez cały obszar malowania w obie
	// strony (CSS Backgrounds 3 § 3.4, no-repeat: obraz jest rysowany raz).
	if singleX {
		repeatWidth = utils.Maxs(repeatWidth, paintingWidth+pr.Fl(math.Abs(float64(X-paintingX)))+imageWidth)
	}
	if singleY {
		repeatHeight = utils.Maxs(repeatHeight, paintingHeight+pr.Fl(math.Abs(float64(Y-paintingY)))+imageHeight)
	}

	// gopdfgo: gdy w obszarze malowania widać najwyżej jedną kopię obrazu w każdej osi (najczęstszy
	// przypadek: gradient albo obraz na całe pudełko), rysujemy ją wprost, przycinając do obszaru
	// malowania i do pola obrazu, bez wzorca kafelkowego. Wynik jest ten sam (CSS Backgrounds 3
	// § 3.4), plik mniejszy, a obraz nie zależy od tego, jak przeglądarka PDF odczytuje macierz
	// wzorca w grupie przezroczystości (zob. internal/goweasyprint/pdf/placement.go). Źródło
	// zawsze używało wzorca.
	areaX, areaY, areaW, areaH := paintingX, paintingY, paintingWidth, paintingHeight
	if layer.Unbounded {
		x1, y1, x2, y2 := ctx.dst.GetBoundingBox()
		areaX, areaY, areaW, areaH = x1, y1, x2-x1, y2-y1
	}
	kx, nx := visibleTiles(X, repeatWidth, imageWidth, areaX, areaW)
	ky, ny := visibleTiles(Y, repeatHeight, imageHeight, areaY, areaH)
	if nx == 0 || ny == 0 {
		return
	}
	if nx == 1 && ny == 1 {
		x0, y0 := X+kx*repeatWidth, Y+ky*repeatHeight
		// przycięcie do części wspólnej obszaru malowania i pola obrazu, liczonej tutaj: pole obrazu
		// bywa ogromne (cover dla skrajnych proporcji), a takich współrzędnych nie piszemy do PDF
		clipX0, clipY0 := max(areaX, x0), max(areaY, y0)
		clipX1, clipY1 := min(areaX+areaW, x0+imageWidth), min(areaY+areaH, y0+imageHeight)
		if clipX1 <= clipX0 || clipY1 <= clipY0 {
			return
		}
		ctx.dst.OnNewStack(func() {
			ctx.dst.Rectangle(clipX0, clipY0, clipX1-clipX0, clipY1-clipY0)
			ctx.dst.State().Clip(false)
			ctx.dst.State().Transform(matrix.New(1, 0, 0, 1, x0, y0))
			// obraz tła ma własną przezroczystość, jak w SetColorPattern
			ctx.dst.State().SetAlpha(1, false)
			layer.Image.Draw(ctx.dst, ctx, imageWidth, imageHeight, string(imageRendering))
		})
		return
	}

	// draw the image on a pattern
	patttern := ctx.dst.NewGroup(0, 0, repeatWidth, repeatHeight)
	layer.Image.Draw(patttern, ctx, imageWidth, imageHeight, string(imageRendering))

	ctx.dst.OnNewStack(func() {
		mat := matrix.New(1, 0, 0, 1, X, Y) // translate
		ctx.dst.State().SetColorPattern(patttern, imageWidth, imageHeight, mat, false)
		if layer.Unbounded {
			x1, y1, x2, y2 := ctx.dst.GetBoundingBox()
			ctx.dst.Rectangle(x1, y1, x2-x1, y2-y1)
		} else {
			ctx.dst.Rectangle(paintingX, paintingY, paintingWidth, paintingHeight)
		}
		ctx.dst.Paint(backend.FillNonZero)
	})
}

func (ctx drawContext) drawTable(table *bo.TableBox) {
	// Draw the background color and image of the table children.
	// gopdfgo: cień samej tabeli (CSS Backgrounds 3 § 7.1); źródło go nie rysowało
	// (test WPT box-shadow-table-border-collapse-001)
	ctx.drawBoxShadows(&table.BoxFields, false)
	ctx.drawBackgroundDefaut(table.Background)
	ctx.drawBoxShadows(&table.BoxFields, true)
	for _, columnGroup := range table.ColumnGroups {
		ctx.drawBackgroundDefaut(columnGroup.Background)
		for _, column := range columnGroup.Children {
			ctx.drawBackgroundDefaut(column.Box().Background)
		}
	}
	for _, rowGroup := range table.Children {
		if !deferredTableGroup(table, rowGroup) {
			ctx.drawTableGroupBackgrounds(table, rowGroup)
		}
	}

	// Draw borders
	if table.Style.GetBorderCollapse() == "collapse" {
		// obramowania zwinięte rysuje drawStackingContext po wszystkich pudełkach blokowych
		return
	}

	ctx.drawBorder(table)
	for _, rowGroup := range table.Children {
		if !deferredTableGroup(table, rowGroup) {
			ctx.drawTableGroupBorders(rowGroup)
		}
	}
}

// deferredTableGroup (gopdfgo): nagłówek i stopka fragmentu tabeli, pod które wylewa się treść
// niepodzielna wierszy (bo.TableBox.PartsOverOverflow), są malowane osobno, po tej treści (zob.
// orderTablePaint).
func deferredTableGroup(table *bo.TableBox, group Box) bool {
	return table.PartsOverOverflow && (group.Box().IsHeader || group.Box().IsFooter)
}

// drawTableGroup rysuje tła i obramowania (rozdzielone) grupy wierszy group tabeli table.
func (ctx drawContext) drawTableGroup(table *bo.TableBox, group Box) {
	ctx.drawTableGroupBackgrounds(table, group)
	if table.Style.GetBorderCollapse() != "collapse" {
		ctx.drawTableGroupBorders(group)
	}
}

func (ctx drawContext) drawTableGroupBackgrounds(table *bo.TableBox, rowGroup Box) {
	ctx.drawBackgroundDefaut(rowGroup.Box().Background)
	for _, row := range rowGroup.Box().Children {
		// gopdfgo: cień wiersza tabeli (box-shadow dotyczy wszystkich elementów, CSS Backgrounds 3
		// § 7.1; test WPT box-shadow-table-row-display); źródło rysowało cienie tylko komórek
		ctx.drawBoxShadows(row.Box(), false)
		ctx.drawBackgroundDefaut(row.Box().Background)
		ctx.drawBoxShadows(row.Box(), true)
		for _, cell := range row.Box().Children {
			cell := cell.Box()
			if table.Style.GetBorderCollapse() == "collapse" ||
				cell.Style.GetEmptyCells() == "show" || !cell.Empty {
				// gopdfgo: pod dalszym ciągiem komórki rozpiętej tło wiersza, w którym się zaczyna
				// (zob. bo.BoxFields.SpanOriginRow)
				ctx.drawBackgroundDefaut(cell.SpanRowBackground)
				ctx.drawBoxShadows(cell, false)
				ctx.drawBackgroundDefaut(cell.Background)
				ctx.drawBoxShadows(cell, true)
			}
		}
	}
}

func (ctx drawContext) drawTableGroupBorders(rowGroup Box) {
	for _, row := range rowGroup.Box().Children {
		for _, cell := range row.Box().Children {
			if cell.Box().Style.GetEmptyCells() == "show" || !cell.Box().Empty {
				ctx.drawBorder(cell)
			}
		}
	}
}

// tablePaintItem to pozycja kroku 4 malowania (zob. orderTablePaint): pudełko blokowe albo, gdy table nie
// jest nil, odłożona grupa wierszy box tej tabeli.
type tablePaintItem struct {
	box, table Box
}

// orderTablePaint (gopdfgo) układa pudełka blokowe kroku 4 (CSS 2.1 dodatek E.2) do malowania. Zwykle
// zostaje porządek drzewa, a tabela maluje tła wszystkich swoich części naraz (dodatek E.2, krok 4, za
// CSS 2.1 § 17.5.1). Wyjątkiem jest fragment tabeli, w którym treść niepodzielna wierszy wylewa się pod
// powtarzany nagłówek lub stopkę (bo.TableBox.PartsOverOverflow): nagłówek, a za nim stopka, są wtedy
// malowane po pudełkach blokowych pozostałych grup, każde razem z pudełkami blokowymi w swoich komórkach,
// tak jak w Chromium, które maluje grupy wierszy po kolei, każdą w całości. Specyfikacje nie mówią, jak
// powtarzane części tabeli mają się do przepełnienia z fragmentacji (CSS Break 3 § 4.4, CSS Tables 3
// § 6.2); test WPT css-page/monolithic-overflow-017. root to tabela tworząca sama kontekst nakładania,
// której potomkami są wszystkie bloki (nil, gdy jej nie ma).
func orderTablePaint(blocks []Box, root Box) []tablePaintItem {
	if root != nil && root.(bo.TableBoxITF).Table().PartsOverOverflow {
		return orderTableParts(root, blocks)
	}
	out := make([]tablePaintItem, 0, len(blocks))
	for i := 0; i < len(blocks); i++ {
		block := blocks[i]
		out = append(out, tablePaintItem{box: block})
		table, ok := block.(bo.TableBoxITF)
		if !ok || !table.Table().PartsOverOverflow {
			continue
		}
		descendants := tableDescendants(block)
		end := i + 1
		for end < len(blocks) && descendants[blocks[end].Box()] {
			end++
		}
		out = append(out, orderTableParts(block, blocks[i+1:end])...)
		i = end - 1
	}
	return out
}

// orderTableParts porządkuje bloki run leżące w tabeli table (zob. orderTablePaint).
func orderTableParts(table Box, run []Box) []tablePaintItem {
	groups := table.Box().Children
	var header, footer Box
	var headerSet, footerSet map[*bo.BoxFields]bool
	if len(groups) != 0 && groups[0].Box().IsHeader {
		header, headerSet = groups[0], tableDescendants(groups[0])
	}
	if L := len(groups); L != 0 && groups[L-1].Box().IsFooter {
		footer, footerSet = groups[L-1], tableDescendants(groups[L-1])
	}
	var body, inHeader, inFooter []Box
	for _, block := range run {
		switch {
		case headerSet[block.Box()]:
			inHeader = append(inHeader, block)
		case footerSet[block.Box()]:
			inFooter = append(inFooter, block)
		default:
			body = append(body, block)
		}
	}
	out := orderTablePaint(body, nil)
	if header != nil {
		out = append(out, tablePaintItem{box: header, table: table})
		out = append(out, orderTablePaint(inHeader, nil)...)
	}
	if footer != nil {
		out = append(out, tablePaintItem{box: footer, table: table})
		out = append(out, orderTablePaint(inFooter, nil)...)
	}
	return out
}

// tableDescendants zbiera pola pudełek potomnych parent poza osobnymi kontekstami nakładania.
func tableDescendants(parent Box) map[*bo.BoxFields]bool {
	descendants := map[*bo.BoxFields]bool{}
	var collect func(Box)
	collect = func(parent Box) {
		for _, child := range parent.Box().Children {
			switch child.(type) {
			case StackingContext, *StackingContext:
				continue
			}
			descendants[child.Box()] = true
			collect(child)
		}
	}
	collect(parent)
	return descendants
}

type segment struct {
	side pr.KnownProp
	bo.Border
	borderBox pr.Rectangle
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Draw borders of table cells when they collapse.
func (ctx drawContext) drawCollapsedBorders(table *bo.TableBox) {
	var rowHeights, rowPositions []pr.Float
	for _, rowGroup := range table.Children {
		for _, row := range rowGroup.Box().Children {
			rowHeights = append(rowHeights, row.Box().Height.V())
			rowPositions = append(rowPositions, row.Box().PositionY)
		}
	}
	columnWidths := table.ColumnWidths
	if len(rowHeights) == 0 || len(columnWidths) == 0 {
		// One of the list is empty: don’t bother with empty tables
		return
	}
	columnPositions := table.ColumnPositions // shallow copy
	gridHeight := len(rowHeights)
	gridWidth := len(columnWidths)

	if gridWidth != len(columnPositions) {
		panic(fmt.Sprintf("expected same gridWidth and columnPositions length, got %d, %d", gridWidth, len(columnPositions)))
	}

	verticalBorders, horizontalBorders := table.CollapsedBorderGrid.Vertical, table.CollapsedBorderGrid.Horizontal
	// Add the end of the last column.
	columnPositions = append(columnPositions, columnPositions[len(columnPositions)-1]+columnWidths[len(columnWidths)-1])
	// Add the end of the last row.
	rowPositions = append(rowPositions, rowPositions[len(rowPositions)-1]+rowHeights[len(rowHeights)-1])

	headerRows := 0
	if table.Children[0].Box().IsHeader {
		headerRows = len(table.Children[0].Box().Children)
	}

	footerRows := 0
	if L := len(table.Children); table.Children[L-1].Box().IsFooter {
		footerRows = len(table.Children[L-1].Box().Children)
	}

	skippedRows := table.SkippedRows
	bodyRowsOffset := 0
	if skippedRows != 0 {
		bodyRowsOffset = skippedRows - headerRows
	}

	originalGridHeight := len(verticalBorders)
	footerRowsOffset := originalGridHeight - gridHeight

	rowNumber := func(y int, horizontal bool) int {
		// Examples in comments for 2 headers rows, 5 body rows, 3 footer rows
		if headerRows != 0 && y < (headerRows+boolToInt(horizontal)) {
			// Row in header: y < 2 for vertical, y < 3 for horizontal
			return y
		} else if footerRows != 0 && y >= (gridHeight-footerRows-boolToInt(horizontal)) {
			// Row in footer: y >= 7 for vertical, y >= 6 for horizontal
			return y + footerRowsOffset
		} else {
			// Row in body: 2 >= y > 7 for vertical, 3 >= y > 6 for horizontal
			return y + bodyRowsOffset
		}
	}

	var segments []segment

	// vertical=true
	halfMaxWidth := func(borderList [][]bo.Border, yxPairs [2][2]int, vertical bool) pr.Float {
		var result pr.Float
		for _, tmp := range yxPairs {
			y, x := tmp[0], tmp[1]
			cond := 0 <= y && y <= gridHeight && 0 <= x && x < gridWidth
			if vertical {
				cond = 0 <= y && y < gridHeight && 0 <= x && x <= gridWidth
			}
			if cond {
				yy := rowNumber(y, !vertical)
				width := pr.Float(borderList[yy][x].Width)
				result = max(result, width)
			}
		}
		return result / 2
	}

	addVertical := func(x, y int) {
		yy := rowNumber(y, false)
		border := verticalBorders[yy][x]
		if border.Width == 0 || border.Color.RGBA.A == 0 {
			return
		}
		posX := columnPositions[x]
		posY1 := rowPositions[y]
		if y != 0 || !table.SkipCellBorderTop {
			posY1 -= halfMaxWidth(horizontalBorders, [2][2]int{{y, x - 1}, {y, x}}, false)
		}
		posY2 := rowPositions[y+1]
		if y != gridHeight-1 || !table.SkipCellBorderBottom {
			posY2 += halfMaxWidth(horizontalBorders, [2][2]int{{y + 1, x - 1}, {y + 1, x}}, false)
		}
		segments = append(segments, segment{
			Border: border, side: left,
			borderBox: pr.Rectangle{posX, posY1, 0, posY2 - posY1},
		})
	}

	addHorizontal := func(x, y int) {
		if y == 0 && table.SkipCellBorderTop {
			return
		}
		if y == gridHeight && table.SkipCellBorderBottom {
			return
		}

		yy := rowNumber(y, true)
		border := horizontalBorders[yy][x]
		if border.Width == 0 || border.Color.RGBA.A == 0 {
			return
		}
		posY := rowPositions[y]
		shiftBefore := halfMaxWidth(verticalBorders, [2][2]int{{y - 1, x}, {y, x}}, true)
		shiftAfter := halfMaxWidth(verticalBorders, [2][2]int{{y - 1, x + 1}, {y, x + 1}}, true)
		posX1 := columnPositions[x] - shiftBefore
		posX2 := columnPositions[x+1] + shiftAfter
		segments = append(segments, segment{
			Border: border, side: top,
			borderBox: pr.Rectangle{posX1, posY, posX2 - posX1, 0},
		})
	}

	for x := range gridWidth {
		addHorizontal(x, 0)
	}
	for y := range gridHeight {
		addVertical(0, y)
		for x := range gridWidth {
			addVertical(x+1, y)
			addHorizontal(x, y+1)
		}
	}
	// gopdfgo: przy równej sile obramowań narożnik należy do odcinków komórki leżącej wyżej i bardziej
	// na lewo (tak jak rozstrzyga się sam konflikt kolorów w CSS 2.1 § 17.6.2.1 i jak rysuje Chromium;
	// testy WPT border-conflict-element-001d, -001e), więc te odcinki rysujemy na końcu. W źródle
	// rysowane były w kolejności dodawania i dolne narożniki przykrywał odcinek komórki po prawej.
	slices.Reverse(segments)

	// Sort bigger scores last (painted later, on top)
	sort.SliceStable(segments, func(i, j int) bool {
		return segments[i].Border.Score.Lower(segments[j].Border.Score)
	})

	for _, segment := range segments {
		ctx.dst.OnNewStack(func() {
			bx, by, bw, bh := segment.borderBox.Unpack()
			ctx.drawLine(bx, by, bx+bw, by+bh, segment.Width, segment.Style,
				styledColor(segment.Style, segment.Color.RGBA, segment.side), 0)
		})
	}
}

// Draw the given `bo.ReplacedBox`
func (ctx drawContext) drawReplacedbox(box_ bo.ReplacedBoxITF) {
	box := box_.Replaced()
	if box.Style.GetVisibility() != "visible" || !pr.Is(box.Width) || !pr.Is(box.Height) {
		return
	}

	// gopdfgo: fragment obrazu pokrojonego między strony rysujemy jako cały obraz przesunięty w górę
	// o część pokazaną wcześniej i przycięty do pudełka fragmentu.
	visibleHeight := box.Height
	sliced := box.SliceFullHeight > 0
	if sliced {
		box.Height = box.SliceFullHeight
	}
	drawWidth, drawHeight, drawX, drawY := layout.LayoutReplacedBox(box_)
	box.Height = visibleHeight
	if drawWidth <= 0 || drawHeight <= 0 {
		return
	}

	ctx.dst.OnNewStack(func() {
		if sliced {
			ctx.dst.Rectangle(fl(box.ContentBoxX()), fl(box.ContentBoxY()), fl(box.Width.V()), fl(visibleHeight.V()))
			ctx.dst.State().Clip(false)
			drawY -= box.SliceOffset
		}
		// gopdfgo: treść elementu zastępowanego jest zawsze przycinana do zaokrąglonej krawędzi treści
		// (CSS Backgrounds 3 § 5.3; test WPT border-radius-clip-001); źródło tego nie robiło
		if box.BorderTopLeftRadius != (bo.Point{}) || box.BorderTopRightRadius != (bo.Point{}) ||
			box.BorderBottomRightRadius != (bo.Point{}) || box.BorderBottomLeftRadius != (bo.Point{}) {
			roundedBox(ctx.dst, box.RoundedContentBox())
			ctx.dst.State().Clip(false)
		}
		ctx.dst.State().SetAlpha(1, false)
		ctx.dst.State().Transform(matrix.Translation(fl(drawX), fl(drawY)))
		ctx.dst.OnNewStack(func() {
			box.Replacement.Draw(ctx.dst, ctx, pr.Fl(drawWidth), pr.Fl(drawHeight), string(box.Style.GetImageRendering()))
		})
	})
}

// offsetX=0, textOverflow="clip"
func (ctx drawContext) drawInlineLevel(page *bo.PageBox, box_ Box, offsetX fl, textOverflow string, blockEllipsis pr.TaggedString) {
	if stackingContext, ok := box_.(StackingContext); ok {
		if !(bo.InlineBlockT.IsInstance(stackingContext.box) || bo.InlineFlexT.IsInstance(stackingContext.box) || bo.InlineGridT.IsInstance(stackingContext.box)) {
			panic(fmt.Sprintf("expected InlineBlock or InlineFlex, got %v", stackingContext.box))
		}
		ctx.drawStackingContext(stackingContext)
	} else if lineBox, ok := box_.(*bo.LineBox); ok && lineBox.SliceFullHeight > 0 {
		// gopdfgo: wycinek wiersza krojonego między strony: cały wiersz przesunięty w górę o część
		// pokazaną wcześniej i przycięty do wysokości wycinka.
		whole := *lineBox
		whole.SliceFullHeight = 0
		ctx.dst.OnNewStack(func() {
			ctx.dst.Rectangle(-1e5, fl(lineBox.PositionY), 2e5, fl(lineBox.Height.V()))
			ctx.dst.State().Clip(false)
			ctx.dst.State().Transform(matrix.Translation(0, -fl(lineBox.SliceOffset)))
			ctx.drawInlineLevel(page, &whole, offsetX, textOverflow, blockEllipsis)
		})
	} else {
		ctx.setMaskBorder(box_)
		box := box_.Box()
		ctx.drawBoxShadows(box, false)
		ctx.drawBackgroundDefaut(box.Background)
		ctx.drawBoxShadows(box, true)
		ctx.drawBorder(box_)
		textBox, isTextBox := box_.(*bo.TextBox)
		replacedBox, isReplacedBox := box_.(bo.ReplacedBoxITF)
		if layout.IsLine(box_) {
			if lineBox, ok := box_.(*bo.LineBox); ok {
				textOverflow = lineBox.TextOverflow
				blockEllipsis = lineBox.BlockEllipsis
			}
			for _, child := range box.Children {
				childOffsetX := offsetX
				if _, ok := child.(StackingContext); !ok {
					childOffsetX = offsetX + fl(child.Box().PositionX) - fl(box.PositionX)
				}
				if childT, ok := child.(*bo.TextBox); ok {
					ctx.drawText(childT, childOffsetX, textOverflow, blockEllipsis)
				} else {
					ctx.drawInlineLevel(page, child, childOffsetX, textOverflow, blockEllipsis)
				}
			}
		} else if isReplacedBox {
			ctx.drawReplacedbox(replacedBox)
		} else if isTextBox {
			// Should only happen for list markers
			ctx.drawText(textBox, offsetX, textOverflow, blockEllipsis)
		} else {
			panic(fmt.Sprintf("unexpected box %s", box_.Type()))
		}
	}
}

func (ctx *drawContext) drawBlockLevel(page *bo.PageBox, blocksAndCells BoxTree) {
	if blocksAndCells == nil {
		return
	}
	for i, block := range blocksAndCells.boxes {
		innerBlocksAndCells := blocksAndCells.inner[i]
		if blockRep, ok := block.(bo.ReplacedBoxITF); ok {
			ctx.drawReplacedbox(blockRep)
		} else if children := block.Box().Children; len(children) != 0 {
			if bo.LineT.IsInstance(children[len(children)-1]) {
				for _, child := range children {
					ctx.drawInlineLevel(page, child, 0, "clip", pr.TaggedString{Tag: pr.None})
				}
			}
		}
		ctx.drawBlockLevel(page, innerBlocksAndCells)
	}
}

// visibleTiles (gopdfgo) zwraca numer pierwszej kopii obrazu (kopia k zaczyna się w origin+k·step
// i ma długość size) przecinającej odcinek [start, start+length] dodatnią długością oraz liczbę
// takich kopii (2 oznacza „co najmniej dwie”).
func visibleTiles(origin, step, size, start, length pr.Fl) (first pr.Fl, count int) {
	const eps = 1e-3
	if step <= 0 || size <= 0 || length <= 0 {
		return 0, 0
	}
	// tolerancja w jednostkach długości (nie w ułamku kroku: krok bywa ogromny)
	o, st, sz, a, b := float64(origin), float64(step), float64(size), float64(start)+eps, float64(start+length)-eps
	kMin := math.Ceil((a - o - sz) / st)
	kMax := math.Floor((b - o) / st)
	// kopia kMin może kończyć się przed początkiem odcinka, gdy size < step (odstępy przy space)
	for kMin <= kMax && o+kMin*st+sz <= a {
		kMin++
	}
	for kMin <= kMax && o+kMax*st >= b {
		kMax--
	}
	switch {
	case kMax < kMin:
		return 0, 0
	case kMax == kMin:
		return pr.Fl(kMin), 1
	default:
		return pr.Fl(kMin), 2
	}
}
