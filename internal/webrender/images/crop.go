package images

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"io"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
)

// Crop (gopdfgo) zwraca wycinek obrazu rastrowego jako osobny obraz. Służy do rysowania wycinków
// border-image: wycinek rysowany jako cały obraz przycięty maską miesza przy wygładzaniu swoją krawędź
// z sąsiednim obszarem obrazu, a wycięty obraz ma własne krawędzie (CSS Backgrounds 3 § 6.2: każdy
// wycinek jest skalowany osobno). ok == false, gdy obraz nie jest rastrowy albo nie da się go
// zdekodować; wołający rysuje wtedy po staremu.
func Crop(img Image, x, y, width, height int) (Image, bool) {
	raster, isRaster := img.(rasterImage)
	if !isRaster || width <= 0 || height <= 0 {
		return nil, false
	}
	source, seekable := raster.image.Content.(io.ReadSeeker)
	if !seekable {
		return nil, false
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, false
	}
	decoded, _, err := image.Decode(source)
	source.Seek(0, io.SeekStart) // treść czyta potem zapis PDF
	if err != nil {
		return nil, false
	}
	bounds := decoded.Bounds()
	rect := image.Rect(x, y, x+width, y+height).Add(bounds.Min).Intersect(bounds)
	if rect.Empty() {
		return nil, false
	}
	part := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(part, part.Bounds(), decoded, rect.Min, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, part); err != nil {
		return nil, false
	}
	out := rasterImage{optimizeSize: raster.optimizeSize}
	out.image.Content = bytes.NewReader(encoded.Bytes())
	out.image.MimeType = "image/png"
	// identyfikator wyznaczony przez obraz źródłowy i prostokąt: ten sam wycinek trafia do PDF raz
	out.image.ID = raster.image.ID*31 + ((x*4099+y)*4099+width)*4099 + height + 1
	out.intrinsicWidth, out.intrinsicHeight = pr.Float(rect.Dx()), pr.Float(rect.Dy())
	out.intrinsicRatio = out.intrinsicWidth / out.intrinsicHeight
	return out, true
}

// Blank (gopdfgo) to pusty obraz o podanym rozmiarze i bez proporcji własnych: treść elementu
// zastępowanego, którego zawartości silnik nie rysuje (iframe).
type Blank struct{ Width, Height pr.Float }

func (Blank) isImage() {}

func (b Blank) GetIntrinsicSize(_, _ pr.Float) (width, height, ratio pr.MaybeFloat) {
	return b.Width, b.Height, nil
}

func (Blank) Draw(backend.Canvas, text.TextLayoutContext, pr.Fl, pr.Fl, string) {}
