package images

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/matrix"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
)

// gopdfgo: fragment adresu obrazu. Dokument obrazu jest pobierany i przetwarzany raz dla adresu bez
// fragmentu (Cache), a fragment wyznacza obraz pochodny:
//   - obraz SVG z widokiem (svgView(…) albo nazwa elementu <view>, zob. svg.View) ma własną tożsamość
//     formy współdzielonej i własne proporcje;
//   - wymiar przestrzenny Media Fragments URI 1.0 (§ 4.2.2, xywh=…) wycina prostokąt obrazu
//     rastrowego albo SVG (croppedImage).
// Źródło pomijało fragmenty, a obraz z fragmentem pobierało i przetwarzało osobno.

// splitFragment dzieli adres na dokument i zdekodowany fragment (bez #). W adresie data: fragmentem
// jest tylko końcówka po ostatnim #, gdy wygląda jak fragment (nazwa, svgView(…), para nazwa=wartość):
// treść SVG wpisana wprost w data: często zawiera niezakodowane kolory #rrggbb, które według normy URL
// też zaczynałyby fragment, a przeglądarki obraz takiego adresu i tak rysują po swojemu.
func splitFragment(target string) (document, fragment string) {
	var i int
	if strings.HasPrefix(strings.ToLower(target), "data:") {
		i = strings.LastIndexByte(target, '#')
		if i < 0 || !dataFragmentRe.MatchString(target[i+1:]) {
			return target, ""
		}
	} else {
		i = strings.IndexByte(target, '#')
		if i < 0 {
			return target, ""
		}
	}
	raw := target[i+1:]
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		decoded = raw
	}
	return target[:i], decoded
}

var dataFragmentRe = regexp.MustCompile(`^([A-Za-z_][\w.:-]*|svgView\(.*\)(&t=[^&]*)?|[\w%]+=[^<>"']*)$`)

// withFragment zwraca obraz base widziany przez fragment albo base, gdy fragment go nie dotyczy.
func withFragment(base Image, fragment string) Image {
	if base == nil || fragment == "" {
		return base
	}
	if s, ok := base.(SVGImage); ok && s.icon != nil {
		if view, ok := s.icon.ResolveView(fragment); ok {
			s.view = view
			return s
		}
	}
	if strings.HasPrefix(strings.TrimSpace(fragment), "svgView(") {
		return base // niepoprawny widok SVG nie jest fragmentem mediów (test WPT svgview-and-xywh)
	}
	if region, ok := parseSpatialFragment(fragment); ok {
		switch base.(type) {
		case SVGImage, rasterImage:
			return newCroppedImage(base, region)
		}
	}
	return base
}

// spatialFragment to wymiar xywh: w pikselach obrazu albo w procentach jego rozmiaru.
type spatialFragment struct {
	x, y, w, h float64
	percent    bool
}

var (
	pixelRe   = regexp.MustCompile(`^(?:pixel:)?(\d+),(\d+),(\d+),(\d+)$`)
	percentRe = regexp.MustCompile(`^percent:(\d+(?:\.\d+)?),(\d+(?:\.\d+)?),(\d+(?:\.\d+)?),(\d+(?:\.\d+)?)$`)
)

// parseSpatialFragment czyta wymiar xywh (Media Fragments URI 1.0 § 4.2.2): xywh=[pixel:|percent:]x,y,w,h,
// piksele jako liczby całkowite bez znaku, procenty także z częścią ułamkową; szerokość i wysokość muszą
// być dodatnie. Pary nazwa=wartość rozdziela &, a z kilku poprawnych wystąpień obowiązuje ostatnie
// (§ 5.1.1). Wartość niepoprawna jest pomijana (§ 7.1), a fragment bez poprawnego xywh nie wycina niczego.
func parseSpatialFragment(fragment string) (out spatialFragment, ok bool) {
	for _, pair := range strings.Split(fragment, "&") {
		name, value, found := strings.Cut(pair, "=")
		if !found || name != "xywh" {
			continue
		}
		var m []string
		percent := false
		if m = pixelRe.FindStringSubmatch(value); m == nil {
			if m = percentRe.FindStringSubmatch(value); m == nil {
				continue
			}
			percent = true
		}
		var v [4]float64
		for i := range v {
			v[i], _ = strconv.ParseFloat(m[i+1], 64)
		}
		if v[2] <= 0 || v[3] <= 0 {
			continue
		}
		out, ok = spatialFragment{x: v[0], y: v[1], w: v[2], h: v[3], percent: percent}, true
	}
	return out, ok
}

// croppedImage to prostokąt obrazu base wskazany wymiarem xywh. Obraz źródłowy jest rysowany
// w rozmiarze odniesienia (refWidth, refHeight) i przycinany do prostokąta, który staje się całym
// obrazem: rozmiar naturalny wycinka to rozmiar prostokąta (Media Fragments URI 1.0 § 7.1, obraz jest
// wyświetlany jako wycinek). Prostokąt wychodzący poza obraz jest obcinany do jego granic, a pusty po
// obcięciu daje obraz bez treści.
//
// Rozmiarem odniesienia jest rozmiar naturalny obrazu; SVG bez rozmiaru naturalnego bierze rozmiar
// viewBox, a bez niego 300 na 150 (domyślny rozmiar obiektu, CSS Images 3 § 4.1), bo piksele obrazu
// wektorowego bez rozmiaru nie są inaczej określone. Wycinek takiego obrazu też nie ma rozmiaru
// naturalnego, tylko proporcje prostokąta.
type croppedImage struct {
	base                Image
	refWidth, refHeight pr.Fl
	x, y, width, height pr.Fl
	natural             bool // obraz źródłowy ma rozmiar naturalny
}

func (croppedImage) isImage() {}

func newCroppedImage(base Image, region spatialFragment) croppedImage {
	out := croppedImage{base: base}
	w, h, _ := base.GetIntrinsicSize(1, 16)
	var refW, refH float64 = 300, 150
	if w != nil && h != nil {
		refW, refH = float64(w.V()), float64(h.V())
		out.natural = true
	} else if s, ok := base.(SVGImage); ok {
		restore, _ := s.icon.ApplyView(s.view)
		if vb := s.icon.ViewBox(); vb != nil && vb.Width > 0 && vb.Height > 0 {
			refW, refH = float64(vb.Width), float64(vb.Height)
		}
		restore()
	}
	out.refWidth, out.refHeight = pr.Fl(refW), pr.Fl(refH)
	x, y, rw, rh := region.x, region.y, region.w, region.h
	if region.percent {
		x, y, rw, rh = x*refW/100, y*refH/100, rw*refW/100, rh*refH/100
	}
	x0, y0 := min(max(x, 0), refW), min(max(y, 0), refH)
	x1, y1 := min(x+rw, refW), min(y+rh, refH)
	out.x, out.y = pr.Fl(x0), pr.Fl(y0)
	out.width, out.height = pr.Fl(max(x1-x0, 0)), pr.Fl(max(y1-y0, 0))
	return out
}

func (c croppedImage) GetIntrinsicSize(imageResolution, fontSize pr.Float) (width, height, ratio pr.MaybeFloat) {
	if c.width <= 0 || c.height <= 0 {
		return pr.Float(0), pr.Float(0), nil
	}
	if !c.natural {
		return nil, nil, pr.Float(c.width / c.height)
	}
	// skala rozmiaru naturalnego (image-resolution obrazu rastrowego)
	scale := pr.Float(1)
	if w, _, _ := c.base.GetIntrinsicSize(imageResolution, fontSize); w != nil && c.refWidth > 0 {
		if bw, _, _ := c.base.GetIntrinsicSize(1, fontSize); bw != nil && bw.V() > 0 {
			scale = w.V() / bw.V()
		}
	}
	return pr.Float(c.width) * scale, pr.Float(c.height) * scale, pr.Float(c.width / c.height)
}

func (c croppedImage) Draw(dst backend.Canvas, textContext text.TextLayoutContext, concreteWidth, concreteHeight pr.Fl, imageRendering string) {
	if c.width <= 0 || c.height <= 0 || concreteWidth <= 0 || concreteHeight <= 0 {
		return
	}
	sx, sy := concreteWidth/c.width, concreteHeight/c.height
	dst.OnNewStack(func() {
		dst.Rectangle(0, 0, concreteWidth, concreteHeight)
		dst.State().Clip(false)
		dst.State().Transform(matrix.New(sx, 0, 0, sy, -c.x*sx, -c.y*sy))
		c.base.Draw(dst, textContext, c.refWidth, c.refHeight, imageRendering)
	})
}
