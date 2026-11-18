package layout

import (
	"fmt"
	"math"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
	"github.com/polestorm/gopdfgo/internal/webrender/images"
)

func boxRectangle(box bo.BoxFields, whichRectangle string) [4]pr.Float {
	switch whichRectangle {
	case "border-box":
		return [4]pr.Float{
			box.BorderBoxX(),
			box.BorderBoxY(),
			box.BorderWidth(),
			box.BorderHeight(),
		}
	case "padding-box":
		return [4]pr.Float{
			box.PaddingBoxX(),
			box.PaddingBoxY(),
			box.PaddingWidth(),
			box.PaddingHeight(),
		}
	case "content-box":
		return [4]pr.Float{
			box.ContentBoxX(),
			box.ContentBoxY(),
			box.Width.V(),
			box.Height.V(),
		}
	default:
		panic(fmt.Sprintf("unexpected whichRectangle : %s", whichRectangle))
	}
}

// emulate Python itertools.cycle
// i is the current iteration index, N the length of the target slice.
func cycle(i, N int) int { return i % N }

// gopdfgo: currentColor to wartość color, którą przystanki gradientu z currentcolor przyjmują przy
// rysowaniu (CSS Images 3 § 3.4); dla pudełek w ::first-line jest to kolor z tego pseudoelementu.
func resolveImage(image pr.Image, orientation pr.SBoolFloat, getImageFromUri bo.ImageFetcher, currentColor pr.Color) images.Image {
	resolveStops := func(stops []pr.ColorStop) []pr.ColorStop {
		out := append([]pr.ColorStop(nil), stops...)
		for i, stop := range out {
			if stop.Color.Type == parser.ColorCurrentColor {
				out[i].Color = currentColor
			}
		}
		return out
	}
	switch img := image.(type) {
	case nil, pr.NoneImage:
		return nil
	case pr.UrlImage:
		return getImageFromUri(string(img), "", orientation)
	case pr.RadialGradient:
		img.ColorStops = resolveStops(img.ColorStops)
		return images.NewRadialGradient(img)
	case pr.LinearGradient:
		img.ColorStops = resolveStops(img.ColorStops)
		return images.NewLinearGradient(img)
	case pr.ConicGradient: // gopdfgo: gradient stożkowy (CSS Images 4 § 3.3)
		img.ColorStops = resolveStops(img.ColorStops)
		return images.NewConicGradient(img)
	default:
		panic(fmt.Sprintf("unexpected type for image: %T %v", image, image))
	}
}

// scrollContentExtent (gopdfgo) zwraca prawą i dolną krawędź treści pudełka: największe krawędzie pudełek
// marginesowych potomków w przepływie (wiersze, bloki, tekst), bez potomków pozycjonowanych.
func scrollContentExtent(box_ Box) (right, bottom pr.Float) {
	box := box_.Box()
	right, bottom = box.ContentBoxX()+box.Width.V(), box.ContentBoxY()+box.Height.V()
	var walk func(b Box)
	walk = func(b Box) {
		for _, child := range b.Box().Children {
			fields := child.Box()
			if fields.IsAbsolutelyPositioned() {
				continue
			}
			if _, ok := fields.Width.(pr.Float); ok {
				if _, ok := fields.Height.(pr.Float); ok {
					right = max(right, fields.PositionX+fields.MarginWidth())
					bottom = max(bottom, fields.PositionY+fields.MarginHeight())
				}
			}
			walk(child)
		}
	}
	walk(box_)
	return right, bottom
}

// Fetch and position background images.
// gopdfgo: positioningBox, jeśli podane, wyznacza obszar pozycjonowania obrazów tła zamiast box_. Służy tłu
// płótna, które według CSS Backgrounds 3 § 2.11.1 jest pozycjonowane względem elementu głównego; w źródle
// było pozycjonowane względem pudełka strony.
func layoutBoxBackgrounds(page *bo.PageBox, box_ Box, getImageFromUri bo.ImageFetcher, layoutChildren bool, style pr.ElementStyle, positioningBox *bo.BoxFields) {
	layoutBoxBackgroundsIn(page, box_, getImageFromUri, layoutChildren, style, positioningBox, false)
}

// layoutBoxBackgroundsIn: inTransform (gopdfgo) mówi, że któryś przodek ma transform. Tło
// z background-attachment: fixed zachowuje się wtedy jak scroll (CSS Transforms 1 § 3: element
// przekształcony tworzy blok zawierający dla potomków, a tło stałe przewija się z nim; test WPT
// background-attachment-fixed-inside-transform-1). Źródło pozycjonowało je względem strony.
func layoutBoxBackgroundsIn(page *bo.PageBox, box_ Box, getImageFromUri bo.ImageFetcher, layoutChildren bool, style pr.ElementStyle, positioningBox *bo.BoxFields, inTransform bool) {
	// Resolve percentages in border-radius properties
	box := box_.Box()
	resolveRadiiPercentages(box)

	if layoutChildren {
		childInTransform := inTransform || len(box_.Box().Style.GetTransform()) != 0
		for _, child := range box_.AllChildren() {
			layoutBoxBackgroundsIn(page, child, getImageFromUri, true, nil, nil, childInTransform)
		}
	}

	if style == nil {
		style = box.Style
	}

	// This is for the border image, not the background, but this is a
	// convenient place to get the image.
	box.BorderImage = resolveImage(style.GetBorderImageSource(), pr.SBoolFloat{}, getImageFromUri, style.GetColor())
	box.MaskBorderImage = resolveImage(style.GetMaskBorderSource(), pr.SBoolFloat{}, getImageFromUri, style.GetColor())

	var (
		color     parser.RGBA // transparent
		images_   []images.Image
		anyImages = false
	)
	// gopdfgo: visibility: collapse ukrywa tło tak samo jak hidden
	if style.GetVisibility() == "visible" {
		orientation := style.GetImageOrientation()
		bs := style.GetBackgroundImage()
		images_ = make([]images.Image, len(bs))
		for i, v := range bs {
			images_[i] = resolveImage(v, orientation, getImageFromUri, style.GetColor())
			if images_[i] != nil {
				anyImages = true
			}
		}
		color = tree.ResolveColor(style, pr.PBackgroundColor).RGBA
	}

	if color.A == 0 && !anyImages {
		if page != box_ { // Pages need a background for bleed box
			box.Background = nil
			return
		}
	}

	sizes := style.GetBackgroundSize()
	sizesN := len(sizes)
	clips := style.GetBackgroundClip()
	clipsN := len(clips)
	repeats := style.GetBackgroundRepeat()
	repeatsN := len(repeats)
	origins := style.GetBackgroundOrigin()
	originsN := len(origins)
	positions := style.GetBackgroundPosition()
	positionsN := len(positions)
	attachments := style.GetBackgroundAttachment()
	attachmentsN := len(attachments)
	blendModes := style.GetBackgroundBlendMode()
	blendModesN := len(blendModes)

	ir := style.GetImageResolution()
	layers := make([]bo.BackgroundLayer, len(images_))
	for i, img := range images_ {
		attachment := attachments[cycle(i, attachmentsN)]
		if attachment == "fixed" && (inTransform || (len(box.Style.GetTransform()) != 0 && !box.IsForRootElement)) {
			attachment = "scroll"
		}
		layers[i] = layoutBackgroundLayer(box_, page, positioningBox, ir, img,
			sizes[cycle(i, sizesN)],
			clips[cycle(i, clipsN)],
			repeats[cycle(i, repeatsN)],
			origins[cycle(i, originsN)],
			positions[cycle(i, positionsN)],
			attachment,
		)
		// gopdfgo: background-clip: border-area (CSS Backgrounds 4 § 3.6); tło strony maluje się w obszarze
		// spadu niezależnie od background-clip
		clip := clips[cycle(i, clipsN)]
		layers[i].BorderArea = (clip == "border-area" || clip == "border-area text") && box_ != page
		layers[i].Text = layers[i].BorderArea && clip == "border-area text"
		// gopdfgo: background-blend-mode, lista powtarzana jak pozostałe właściwości warstw (CSS
		// Compositing 1 § 3.2); źródło jej nie znało
		if blendModesN != 0 {
			layers[i].BlendMode = blendModes[cycle(i, blendModesN)]
		}
	}

	if traceMode {
		traceLogger.Dump(fmt.Sprintf("background: %v", layers))
	}

	box.Background = &bo.Background{Color: color, ImageRendering: style.GetImageRendering(), Layers: layers, Owner: box_}
	// gopdfgo: background-clip: text na którejkolwiek warstwie przycina całe tło do tekstu elementu
	// (uproszczenie: norma pozwala podać to osobno dla każdej warstwy).
	// Tło elementu głównego przechodzi na płótno strony i tam kształt tekstu nie obowiązuje.
	for _, clip := range clips {
		if clip == "text" && box_ != page && !box.IsForRootElement {
			box.Background.TextClip = box_
			break
		}
	}
}

func layoutBackgroundLayer(box_ Box, page *bo.PageBox, positioningBox *bo.BoxFields, resolution pr.TaggedDim, image images.Image, size pr.Size, clip string, repeat [2]string,
	origin string, position pr.CenterPos, attachment string,
) bo.BackgroundLayer {
	var (
		clippedBoxes []bo.RoundedBox
		paintingArea pr.Rectangle
	)
	box := box_.Box()
	if clip == "text" || clip == "border-area" || clip == "border-area text" {
		// kształt tekstu (Background.TextClip) albo obramowania (BackgroundLayer.BorderArea) nakłada
		// rysowanie; obszar malowania jak dla border-box
		clip = "border-box"
	}
	// gopdfgo: przy background-attachment: local w pudełku przewijanym (overflow inne niż visible) tło
	// należy do przewijanej treści, więc nie wchodzi pod obramowanie: obszar malowania kończy się na
	// krawędzi odstępu (CSS Backgrounds 3 § 3.5; test WPT background-attachment-local-hidden)
	if attachment == "local" && clip == "border-box" && box_ != page {
		if overflow := box.Style.GetOverflow(); overflow != "visible" && overflow != "clip" {
			clip = "padding-box"
		}
	}
	if box_ == page {
		// [The page’s] background painting area is the bleed area […]
		// regardless of background-clip.
		// https://drafts.csswg.org/css-page-3/#painting
		paintingArea = page.BleedArea()
	} else if bo.TableRowGroupT.IsInstance(box_) {
		clippedBoxes = nil
		var totalHeight pr.Float
		for _, row_ := range box.Children {
			row := row_.Box()
			if len(row.Children) != 0 {
				var maxVal pr.Float
				for _, cell := range row.Children {
					clippedBoxes = append(clippedBoxes, cell.Box().RoundedBorderBox())
					if v := cell.Box().BorderHeight(); v > maxVal {
						maxVal = v
					}
				}
				totalHeight = max(totalHeight, maxVal)
			}
		}
		// gopdfgo: tło grupy wierszy obejmuje całą grupę (CSS 2.1 § 17.5.1);
		// źródło brało wysokość najwyższej komórki, więc tło kończyło się
		// po pierwszym wierszu
		if height, ok := box.Height.(pr.Float); ok {
			totalHeight = max(totalHeight, height)
		}
		paintingArea = [4]pr.Float{
			box.BorderBoxX(), box.BorderBoxY(),
			box.BorderWidth(), totalHeight,
		}
	} else if bo.TableRowT.IsInstance(box_) {
		if len(box.Children) != 0 {
			clippedBoxes = nil
			var max pr.Float
			for _, cell := range box.Children {
				clippedBoxes = append(clippedBoxes, cell.Box().RoundedBorderBox())
				if v := cell.Box().BorderHeight(); v > max {
					max = v
				}
			}
			height := max
			paintingArea = [4]pr.Float{
				box.BorderBoxX(), box.BorderBoxY(),
				box.BorderWidth(), height,
			}
		}
	} else if bo.TableColumnGroupT.IsInstance(box_) || bo.TableColumnT.IsInstance(box_) {
		cells := box.GetCells()
		if len(cells) != 0 {
			clippedBoxes = nil
			maxX, minX := -pr.Inf, pr.Inf
			for _, cell := range cells {
				clippedBoxes = append(clippedBoxes, cell.Box().RoundedBorderBox())
				if v := cell.Box().BorderBoxX() + cell.Box().BorderWidth(); v > maxX {
					maxX = v
				}
				if v := cell.Box().BorderBoxX(); v < minX {
					minX = v
				}
			}
			paintingArea = [4]pr.Float{
				minX, box.BorderBoxY(),
				maxX - minX, box.BorderHeight(),
			}
		}
	} else {
		paintingArea = boxRectangle(*box, clip)
		switch clip {
		case "border-box":
			clippedBoxes = []bo.RoundedBox{box.RoundedBorderBox()}
		case "padding-box":
			clippedBoxes = []bo.RoundedBox{box.RoundedPaddingBox()}
		case "content-box":
			clippedBoxes = []bo.RoundedBox{box.RoundedContentBox()}
		default:
			// see validation
			panic(fmt.Sprintf("unexpected clip : %s", clip))
		}
	}

	var intrinsicWidth, intrinsicHeight, ratio pr.MaybeFloat
	if image != nil {
		intrinsicWidth, intrinsicHeight, ratio = image.GetIntrinsicSize(resolution.Value, box.Style.GetFontSize().Value)
	}
	if image == nil || (intrinsicWidth == pr.Float(0) || intrinsicHeight == pr.Float(0)) {
		return bo.BackgroundLayer{
			Image: nil, Unbounded: box_ == page, PaintingArea: paintingArea,
			Size: [2]pr.Float{}, Position: bo.Position{String: "unused"}, Repeat: bo.Repeat{String: "unused"},
			ClippedBoxes: clippedBoxes,
		}
	}

	var positioningArea [4]pr.Float
	if attachment == "fixed" {
		// Initial containing block
		if bo.PageT.IsInstance(box_) {
			// […] if background-attachment is fixed then the image is
			// positioned relative to the page box including its margins […].
			// https://drafts.csswg.org/css-page/#painting
			positioningArea = [4]pr.Float{0, 0, box.MarginWidth(), box.MarginHeight()}
		} else {
			positioningArea = boxRectangle(page.BoxFields, "content-box")
		}
	} else if positioningBox != nil {
		positioningArea = boxRectangle(*positioningBox, origin)
	} else {
		positioningArea = boxRectangle(*box, origin)
		// gopdfgo: przy background-attachment: local w pudełku przewijanym obszarem pozycjonowania jest
		// cały przewijany obszar treści, nie tylko widoczna część (CSS Backgrounds 3 § 3.5; test WPT
		// background-attachment-350). Wydruk pokazuje stan bez przewinięcia, więc obszar rośnie w prawo
		// i w dół o treść wystającą poza pudełko (wraz z dopełnieniem po stronie końca).
		if overflow := box.Style.GetOverflow(); attachment == "local" && overflow != "visible" && overflow != "clip" && box_ != page {
			right, bottom := scrollContentExtent(box_)
			right += box.PaddingRight.V()
			bottom += box.PaddingBottom.V()
			positioningArea[2] = max(positioningArea[2], right-positioningArea[0])
			positioningArea[3] = max(positioningArea[3], bottom-positioningArea[1])
		}
	}

	_, _, positioningWidth, positioningHeight := positioningArea[0], positioningArea[1], positioningArea[2], positioningArea[3]
	var imageWidth, imageHeight pr.Float
	if size.Tag == pr.Cover {
		imageWidth, imageHeight = coverConstraintImageSizing(positioningWidth, positioningHeight, ratio)
	} else if size.Tag == pr.Contain {
		imageWidth, imageHeight = containConstraintImageSizing(positioningWidth, positioningHeight, ratio)
	} else {
		sizeWidth, sizeHeight := size.Width, size.Height
		iwidth, iheight, iratio := image.GetIntrinsicSize(resolution.Value, box.Style.GetFontSize().Value)
		imageWidth, imageHeight = DefaultImageSizing(iwidth, iheight, iratio,
			pr.ResolvePercentage(sizeWidth, positioningWidth), pr.ResolvePercentage(sizeHeight, positioningHeight), positioningWidth, positioningHeight)
	}

	repeatX, repeatY := repeat[0], repeat[1]

	// gopdfgo: round najpierw skaluje obraz, a potem background-position działa zwyczajnie, z procentami
	// liczonymi od nowego rozmiaru (CSS Backgrounds 3 § 3.4 i 3.9; test WPT background-repeat-round-4).
	// Źródło pomijało background-position w wymiarze z round.
	if repeatX == "round" {
		nRepeats := max(1, int(math.Round(float64(positioningWidth/imageWidth))))
		newWidth := positioningWidth / pr.Float(nRepeats)
		if repeatY != "round" && size.Height.Tag == pr.Auto {
			imageHeight *= newWidth / imageWidth
		}
		imageWidth = newWidth
	}
	if repeatY == "round" {
		nRepeats := max(1, int(math.Round(float64(positioningHeight/imageHeight))))
		newHeight := positioningHeight / pr.Float(nRepeats)
		if repeatX != "round" && size.Width.Tag == pr.Auto {
			imageWidth *= newHeight / imageHeight
		}
		imageHeight = newHeight
	}

	originX, positionX_, originY, positionY_ := position.OriginX, position.Pos[0], position.OriginY, position.Pos[1]
	refX := positioningWidth - imageWidth
	refY := positioningHeight - imageHeight
	positionX := pr.ResolvePercentage(positionX_.Tagged(), refX)
	positionY := pr.ResolvePercentage(positionY_.Tagged(), refY)
	if originX == pr.Right {
		positionX = refX - positionX.V()
	}
	if originY == pr.Bottom {
		positionY = refY - positionY.V()
	}

	return bo.BackgroundLayer{
		Image:           image,
		Size:            [2]pr.Float{imageWidth, imageHeight},
		Position:        bo.Position{Point: bo.MaybePoint{positionX, positionY}},
		Repeat:          bo.Repeat{Reps: repeat},
		Unbounded:       false,
		PaintingArea:    paintingArea,
		PositioningArea: positioningArea,
		ClippedBoxes:    clippedBoxes,
	}
}

// Layout backgrounds on the page box and on its children.
//
// This function takes care of the canvas background, taken from the root
// elememt or a <body> child of the root element.
//
// See https://www.w3.org/TR/CSS21/colors.html#background
//
// gopdfgo: canvasOrigin to pudełko elementu głównego z pierwszej strony przesunięte w górę o wysokość
// poprzednich stron. Tło płótna jest pozycjonowane względem niego, więc biegnie ciągle przez strony:
// obraz bez powtarzania jest tylko na pierwszej, a powtarzany nie zaczyna się od nowa na każdej. W źródle
// tło płótna było pozycjonowane osobno na każdej stronie.
func layoutBackgrounds(page *bo.PageBox, getImageFromUri bo.ImageFetcher, canvasOrigin *bo.BoxFields) {
	layoutBoxBackgrounds(page, page, getImageFromUri, true, nil, nil)

	rootBox_ := page.Children[0]
	rootBox := rootBox_.Box()
	if bo.MarginT.IsInstance(rootBox_) {
		panic("unexpected margin box as first child of page")
	}
	// gopdfgo: gdy element główny jest tabelą, jego tło leży na pudełku tabeli wewnątrz pudełka
	// opakowującego; to ono przechodzi na płótno (CSS 2.1 § 14.2). Źródło patrzyło tylko na pudełko
	// opakowujące, które tła nie ma.
	if rootBox.IsTableWrapper && rootBox.Background == nil {
		if table := rootBox.GetWrappedTable(); table != nil && table.Box().Background != nil {
			rootBox_, rootBox = table, table.Box()
		}
	}
	chosenBox_ := rootBox_
	if strings.ToLower(rootBox.ElementTag()) == "html" && rootBox.Background == nil {
		// gopdfgo: body bywa głębiej niż bezpośrednie dziecko, gdy element główny jest tabelą (pudełka
		// anonimowe wiersza i komórki) albo gdy body jest pozycjonowane
		// gopdfgo: tło body nie przechodzi na płótno, gdy element główny albo body ma zawieranie
		// malowania (CSS Contain 2 § 3.3; testy WPT background-color-body-propagation-008, -009)
		if body := findBodyBox(rootBox_, 6); body != nil && !hasPaintContainment(rootBox_) && !hasPaintContainment(body) {
			chosenBox_ = body
		}
	}
	chosenBox := chosenBox_.Box()
	if chosenBox.Background != nil {
		paintingArea := boxRectangle(page.BoxFields, "border-box")
		originalBackground := page.Background
		if canvasOrigin == nil {
			canvasOrigin = rootBox
		}
		layoutBoxBackgrounds(page, page, getImageFromUri, false, chosenBox.Style, canvasOrigin)
		canvasBg := *page.Background
		for i, l := range canvasBg.Layers {
			l.PaintingArea = paintingArea
			canvasBg.Layers[i] = l
		}
		page.CanvasBackground = &canvasBg
		page.Background = originalBackground
		chosenBox.Background = nil
	} else {
		page.CanvasBackground = nil
	}
}

// hasPaintContainment (gopdfgo) mówi, czy pudełko ma zawieranie malowania: paint wprost albo przez
// strict lub content (CSS Contain 2 § 2).
func hasPaintContainment(box Box) bool {
	return bo.HasContainment(box, "paint") || bo.HasContainment(box, "strict") || bo.HasContainment(box, "content")
}

// findBodyBox szuka pudełka elementu body wśród potomków elementu głównego, do podanej głębokości.
func findBodyBox(box Box, depth int) Box {
	if placeholder, ok := box.(*AbsolutePlaceholder); ok {
		box = placeholder.AliasBox
	}
	for _, child := range box.Box().Children {
		if strings.ToLower(child.Box().ElementTag()) == "body" {
			if placeholder, ok := child.(*AbsolutePlaceholder); ok {
				return placeholder.AliasBox
			}
			return child
		}
	}
	if depth > 0 {
		for _, child := range box.Box().Children {
			if tag := strings.ToLower(child.Box().ElementTag()); tag != "html" && tag != "" {
				continue // tylko pudełka anonimowe elementu głównego
			}
			if found := findBodyBox(child, depth-1); found != nil {
				return found
			}
		}
	}
	return nil
}
