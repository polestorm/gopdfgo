package images

import pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"

// markerImage (gopdfgo) to obraz znacznika listy. Jego konkretny rozmiar wyznacza domyślny algorytm
// rozmiaru z domyślnym rozmiarem obiektu 1em × 1em (CSS Lists 3 § 3.1, CSS Images 3 § 5.2), więc
// obraz bez własnego rozmiaru (gradient, SVG bez width i height) ma bok 1em, a nie 300 × 150 px
// jak zwykły element zastępowany (test WPT list-style-image-gradients).
type markerImage struct {
	Image
}

// MarkerImage opakowuje obraz użyty jako znacznik listy.
func MarkerImage(image Image) Image { return markerImage{image} }

func (m markerImage) GetIntrinsicSize(imageResolution, fontSize pr.Float) (width, height, ratio pr.MaybeFloat) {
	width, height, ratio = m.Image.GetIntrinsicSize(imageResolution, fontSize)
	switch {
	case width != nil && height != nil:
	case width != nil:
		if ratio != nil && ratio.V() != 0 {
			height = width.V() / ratio.V()
		} else {
			height = fontSize
		}
	case height != nil:
		if ratio != nil {
			width = height.V() * ratio.V()
		} else {
			width = fontSize
		}
	case ratio != nil && ratio.V() > 0:
		// wpisanie proporcji w kwadrat 1em (ograniczenie „contain”)
		if ratio.V() >= 1 {
			width, height = fontSize, fontSize/ratio.V()
		} else {
			width, height = fontSize*ratio.V(), fontSize
		}
	default:
		width, height = fontSize, fontSize
	}
	if ratio == nil && height.V() != 0 {
		ratio = width.V() / height.V()
	}
	return width, height, ratio
}
