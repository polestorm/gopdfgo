package svg

import (
	"math"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	"github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/matrix"
)

// gopdfgo: wektorowe rysowanie warstw filtra (zob. filters.go)

// Rozmycie sylwetki. Kopie o wagach g_k (Σ g_k = 1) nakładane zwykłym składaniem z kryciem
// w_k = 1 − exp(−U·g_k) dają w masce alfę L = 1 − exp(−Σ d_k·c_k), gdzie d_k = −ln(1 − w_k), a c_k ∈ {0, 1}
// mówi, czy kopia k pokrywa punkt. Funkcja przejścia maski f(L) = −ln(1 − L)/D, D = Σ d_k, odtwarza
// z tego sumę Σ d_k·c_k / D, czyli splot sylwetki z dyskretnym jądrem Gaussa, a środek dużego kształtu
// dostaje pełne krycie. Czytniki składają maski w 8 bitach i krycie kopii obcinają do k/255, więc
// krycia są od razu kwantowane do pełnych kroków (k + 1/4)/255, a D liczone z tych kroków; kopie
// o kryciu poniżej 3/255 traciłyby zbyt wiele, dlatego zewnętrzne pierścienie mają mniej kopii.
// U = 1,5 trzyma L ≤ 0,78 (stromość funkcji przejścia przy 8-bitowej masce).
const blurDensity = 1.5

// invertTransfer: f(L) = 1 − L (wycięcie kształtu elementu)
var invertTransfer = []Fl{1, 0}

type kernelTap struct{ x, y, w Fl }

// blurKernel to punkty jądra z kryciem kopii i funkcją przejścia maski.
type blurKernel struct {
	taps     []kernelTap
	alphas   []Fl // krycie kopii
	transfer []Fl // próbki f(L) = min(1, −ln(1 − L)/D)
}

// newBlurKernel rozkłada dwuwymiarowy rozkład Gaussa o σ = 1 na punkty: środek i pierścienie co `step`
// (w jednostkach σ) do promienia około 2,5σ; waga pierścienia to masa rozkładu w jego paśmie
// (z dystrybuanty radialnej 1 − exp(−r²/2)), a ogon za ostatnim pasmem dokładamy do ostatniego
// pierścienia. Promień pierścienia to średni promień w paśmie, liczba kopii rośnie z obwodem (odstęp
// około 1,5·step), ograniczona tak, żeby krycie kopii nie spadło poniżej 3/255, a kolejne pierścienie
// są obrócone o pół kroku, żeby krawędzie kopii nie układały się w promienie.
func newBlurKernel(step Fl) blurKernel {
	cdf := func(r float64) float64 { return 1 - math.Exp(-r*r/2) }
	meanRadius := func(a, b float64) float64 {
		const n = 64
		var num, den float64
		for i := 0; i < n; i++ {
			r := a + (b-a)*(float64(i)+0.5)/n
			d := r * math.Exp(-r*r/2)
			num += r * d
			den += d
		}
		return num / den
	}
	minDensity := -math.Log(1-3.0/255) / blurDensity // najmniejsza waga kopii
	var out blurKernel
	var total float64
	add := func(x, y, mass float64) {
		k := math.Max(1, math.Round(255*(1-math.Exp(-blurDensity*mass))))
		out.taps = append(out.taps, kernelTap{Fl(x), Fl(y), Fl(mass)})
		out.alphas = append(out.alphas, Fl((k+0.25)/255))
		total += -math.Log(1 - k/255)
	}
	s := float64(step)
	rings := max(1, int(math.Round(2.5/s)))
	add(0, 0, cdf(s/2))
	for j := 1; j <= rings; j++ {
		a, b := (float64(j)-0.5)*s, (float64(j)+0.5)*s
		mass := cdf(b) - cdf(a)
		if j == rings {
			mass = 1 - cdf(a)
			b = a + 4
		}
		r := meanRadius(a, b)
		n := int(math.Ceil(2 * math.Pi * r / (1.5 * s)))
		n = max(4, min(n, int(mass/minDensity)))
		for k := 0; k < n; k++ {
			angle := 2 * math.Pi * (float64(k) + 0.5*float64(j%2)) / float64(n)
			add(r*math.Cos(angle), r*math.Sin(angle), mass/float64(n))
		}
	}
	// funkcja przejścia; między próbkami czytnik interpoluje liniowo, a przy 64 próbkach błąd
	// interpolacji jest poniżej 0,001
	out.transfer = make([]Fl, 64)
	for i := range out.transfer {
		l := float64(i) / float64(len(out.transfer)-1)
		v := 1.0
		if l < 1 {
			v = min(1, -math.Log(1-l)/total)
		}
		out.transfer[i] = Fl(v)
	}
	return out
}

// blurStep dobiera odstęp pierścieni (w σ) do wielkości rozmycia w pikselach gospodarza: co pół σ,
// ale nie gęściej niż co piksel; małe rozmycia dostają mniej kopii.
func blurStep(sigmaHost Fl) Fl {
	if sigmaHost <= 0 {
		return 1
	}
	return min(1.25, max(0.5, 1/sigmaHost))
}

// drawFiltered rysuje element przez filtr; false, gdy filtra nie da się narysować (element zostaje
// wtedy narysowany bez filtra przez wołającego). content rysuje sam element bez filtra.
func (svg *SVGImage) drawFiltered(dst backend.Canvas, f *svgFilter, node *svgNode, dims drawingDims, content func(backend.Canvas)) bool {
	if _, ok := dst.(backend.FilterCanvas); !ok {
		return false
	}
	bbox, hasBBox := svg.objectBoundingBox(dst, node, dims)
	region, ok := f.resolveRegion(bbox, hasBBox, dims)
	if !ok {
		// ramka pusta przy objectBoundingBox: obszar filtra jest pusty, element nie jest rysowany
		// (Filter Effects 1 § 7.1)
		return true
	}
	if region.Width <= 0 || region.Height <= 0 {
		return true
	}
	if len(f.primitives) == 0 {
		// filtr bez prymitywów daje przezroczystą czerń (Filter Effects 1 § 7.1)
		return true
	}
	ev := filterEval{filter: f, dims: dims, bbox: bbox, region: region}
	layers, ok := ev.run()
	if !ok {
		if !f.warned {
			logger.WarningLogger.Printf("SVG: unsupported filter (%s): filter skipped", ev.reason)
			f.warned = true
		}
		return false
	}
	if len(layers) == 0 {
		return true
	}

	// element bez filtra w grupie o rozmiarze obszaru filtra; grupa jest potem wskazywana przez
	// wszystkie warstwy (kopie rozmycia, sylwetki, sam element)
	savedHostBase := svg.hostBaseScale
	hostScale := svg.hostScale(dst)
	svg.hostBaseScale *= transformScale(dst.State().GetTransform())
	source := dst.NewGroup(region.X, region.Y, region.Width, region.Height)
	content(source)
	svg.hostBaseScale = savedHostBase

	needsGroup := false
	for _, l := range layers {
		if l.blend != "" {
			needsGroup = true
		}
	}
	dst.OnNewStack(func() {
		dst.Rectangle(region.X, region.Y, region.Width, region.Height)
		dst.State().Clip(false)
		target := dst
		if needsGroup {
			// tryby feBlend mieszają warstwę tylko z warstwami wyniku pod nią, nie z tłem strony
			target = dst.NewGroup(region.X, region.Y, region.Width, region.Height)
		}
		for _, l := range layers {
			drawFilterLayer(target, l, source, region, hostScale)
		}
		if needsGroup {
			dst.DrawWithOpacity(1, target)
		}
	})
	return true
}

func drawFilterLayer(dst backend.Canvas, l filterLayer, source backend.Canvas, region Rectangle, hostScale Fl) {
	if l.knockout {
		inner := dst.NewGroup(region.X, region.Y, region.Width, region.Height)
		l.knockout = false
		drawFilterLayer(inner, l, source, region, hostScale)
		dst.OnNewStack(func() {
			mask := dst.NewGroup(region.X, region.Y, region.Width, region.Height)
			mfc := mask.(backend.FilterCanvas)
			mfc.WithoutText(func() { mfc.DrawSharedGroup(1, source) })
			dst.(backend.FilterCanvas).SetSoftMask(mask, true, invertTransfer)
			dst.DrawWithOpacity(1, inner)
		})
		return
	}
	fc := dst.(backend.FilterCanvas)
	dst.OnNewStack(func() {
		if l.blend != "" {
			dst.State().SetBlendingMode(l.blend)
		}
		if l.clip != nil {
			dst.Rectangle(l.clip.X, l.clip.Y, l.clip.Width, l.clip.Height)
			dst.State().Clip(false)
		}
		if l.kind == layerFlood {
			dst.Rectangle(region.X, region.Y, region.Width, region.Height)
			dst.State().SetColorRgba(l.color, false)
			dst.Paint(backend.FillNonZero)
			return
		}
		dst.State().Transform(matrix.Translation(l.dx, l.dy))
		// obszar filtra w układzie przesuniętej warstwy
		area := Rectangle{region.X - l.dx, region.Y - l.dy, region.Width, region.Height}

		// copies rysuje kopie grupy src w punktach jądra (w jednostkach σ = sx, sy) z kryciem alpha(i)
		copies := func(target, src backend.Canvas, taps []kernelTap, sx, sy Fl, alpha func(int) Fl) {
			tfc := target.(backend.FilterCanvas)
			tfc.WithoutText(func() {
				for i, tap := range taps {
					target.OnNewStack(func() {
						target.State().Transform(matrix.Translation(tap.x*sx, tap.y*sy))
						tfc.DrawSharedGroup(alpha(i), src)
					})
				}
			})
		}
		// blurMask buduje maskę alfa rozmytej sylwetki (z funkcją przejścia). Przy dużym rozmyciu
		// (σ powyżej 5 px gospodarza) kopie ostrych sylwetek co pół σ dawałyby widoczne schodki, więc
		// rozmycie jest dwustopniowe: najpierw sylwetka rozmyta z σ₁ = σ/√5 (grupa z własną maską), potem
		// kopie tej miękkiej sylwetki z σ₂ = 2σ/√5 (σ₁² + σ₂² = σ², a krok drugiego jądra jest równy σ₁,
		// więc schodki się wygładzają). Kopie o ułamkowym pokryciu składają się prawie addytywnie, bo
		// krycie każdej jest małe.
		blurMask := func() (backend.Canvas, []Fl) {
			src, sx, sy := source, l.sx, l.sy
			if max(sx, sy)*hostScale > 5 {
				const k1, k2 = 0.4472136, 0.8944272 // 1/√5, 2/√5
				s1x, s1y := sx*k1, sy*k1
				sx, sy = sx*k2, sy*k2
				pad := 3 * max(sx, sy)
				ext := Rectangle{area.X - pad, area.Y - pad, area.Width + 2*pad, area.Height + 2*pad}
				kernel := newBlurKernel(blurStep(max(s1x, s1y) * hostScale))
				inner := dst.NewGroup(ext.X, ext.Y, ext.Width, ext.Height)
				copies(inner, source, kernel.taps, s1x, s1y, func(i int) Fl { return kernel.alphas[i] })
				soft := dst.NewGroup(ext.X, ext.Y, ext.Width, ext.Height)
				soft.(backend.FilterCanvas).SetSoftMask(inner, true, kernel.transfer)
				soft.Rectangle(ext.X, ext.Y, ext.Width, ext.Height)
				soft.State().SetColorRgba(parser.RGBA{A: 1}, false)
				soft.Paint(backend.FillNonZero)
				src = soft
			}
			kernel := newBlurKernel(blurStep(max(sx, sy) * hostScale))
			mask := dst.NewGroup(area.X, area.Y, area.Width, area.Height)
			copies(mask, src, kernel.taps, sx, sy, func(i int) Fl { return kernel.alphas[i] })
			return mask, kernel.transfer
		}

		switch {
		case l.kind == layerSource && !l.blurred():
			fc.DrawSharedGroup(l.color.A, source)
		case l.kind == layerSource:
			// kolor: kopie nieprzezroczyste od zewnętrznego pierścienia do środka (na wierzchu kopie
			// najbliższe położenia elementu), krycie: rozmyta maska
			colors := dst.NewGroup(area.X, area.Y, area.Width, area.Height)
			taps := newBlurKernel(blurStep(max(l.sx, l.sy) * hostScale)).taps
			reversed := make([]kernelTap, len(taps))
			for i, t := range taps {
				reversed[len(taps)-1-i] = t
			}
			copies(colors, source, reversed, l.sx, l.sy, func(int) Fl { return 1 })
			mask, transfer := blurMask()
			fc.SetSoftMask(mask, true, transfer)
			dst.DrawWithOpacity(l.color.A, colors)
		default: // sylwetka
			if l.blurred() {
				mask, transfer := blurMask()
				fc.SetSoftMask(mask, true, transfer)
			} else {
				mask := dst.NewGroup(area.X, area.Y, area.Width, area.Height)
				mfc := mask.(backend.FilterCanvas)
				mfc.WithoutText(func() { mfc.DrawSharedGroup(1, source) })
				fc.SetSoftMask(mask, true, nil)
			}
			dst.Rectangle(area.X, area.Y, area.Width, area.Height)
			dst.State().SetColorRgba(l.color, false)
			dst.Paint(backend.FillNonZero)
		}
	})
}
