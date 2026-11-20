package svg

import "math"

// gopdfgo: wierzchołki znaczników i ich kierunki według SVG 2 § 13.7.3 (rendering markers) oraz
// § 9.5.1 (path directionality). Źródło wyznaczało kąt jako kierunek odcinka wchodzącego (z
// odwróconymi argumentami atan2) i nigdy go nie stosowało, łuki dawały wierzchołek na każdym kawałku
// krzywej Béziera, a wielokąt nie miał wierzchołka zamykającego.

// markSegment to jeden odcinek ścieżki (łuk przybliżony kilkoma krzywymi to nadal jeden odcinek).
type markSegment struct {
	end                point
	startDir, endDir   point // wektory kierunku na początku i na końcu odcinka
	zeroStart, zeroEnd bool  // kierunek nieokreślony (odcinek zerowej długości)
}

// markSubpath to podścieżka: punkt początkowy, odcinki i informacja o zamknięciu.
type markSubpath struct {
	start  point
	segs   []markSegment
	closed bool
	// implicit: podścieżka niejawna po Z — jej początek pokrywa się z wierzchołkiem Z i nie jest
	// osobnym wierzchołkiem
	implicit bool
}

func lineSegment(from, to point) markSegment {
	d := point{to.x - from.x, to.y - from.y}
	zero := d.x == 0 && d.y == 0
	return markSegment{end: to, startDir: d, endDir: d, zeroStart: zero, zeroEnd: zero}
}

func cubicSegment(p0, p1, p2, p3 point) markSegment {
	sub := func(a, b point) point { return point{a.x - b.x, a.y - b.y} }
	nonZero := func(ps ...point) (point, bool) {
		for _, p := range ps {
			if p.x != 0 || p.y != 0 {
				return p, false
			}
		}
		return point{}, true
	}
	out := markSegment{end: p3}
	out.startDir, out.zeroStart = nonZero(sub(p1, p0), sub(p2, p0), sub(p3, p0))
	out.endDir, out.zeroEnd = nonZero(sub(p3, p2), sub(p3, p1), sub(p3, p0))
	return out
}

// fixZeroDirections nadaje odcinkom zerowej długości kierunek sąsiadów (SVG 2 § 9.5.1):
// kierunek końca poprzedniego odcinka, a gdy go brak — początku następnego.
func (sp *markSubpath) fixZeroDirections() {
	var (
		last    point
		hasLast bool
	)
	for i := range sp.segs {
		s := &sp.segs[i]
		if s.zeroStart && hasLast {
			s.startDir, s.zeroStart = last, false
		}
		if s.zeroEnd && !s.zeroStart {
			s.endDir, s.zeroEnd = s.startDir, false
		}
		if !s.zeroEnd {
			last, hasLast = s.endDir, true
		}
	}
	var (
		next    point
		hasNext bool
	)
	for i := len(sp.segs) - 1; i >= 0; i-- {
		s := &sp.segs[i]
		if s.zeroEnd && hasNext {
			s.endDir, s.zeroEnd = next, false
		}
		if s.zeroStart && !s.zeroEnd {
			s.startDir, s.zeroStart = s.endDir, false
		}
		if !s.zeroStart {
			next, hasNext = s.startDir, true
		}
	}
}

func dirAngle(d point) Fl { return Fl(math.Atan2(float64(d.y), float64(d.x))) }

// bisectAngle zwraca kąt dwusiecznej kierunków wejścia i wyjścia. Przy zawróceniu o 180°
// dwusieczna jest prostopadła: kąt wejścia minus 90° (jak Chromium; test WPT markers-orient-002).
func bisectAngle(in, out point) Fl {
	a1, a2 := float64(dirAngle(in)), float64(dirAngle(out))
	sx, sy := math.Cos(a1)+math.Cos(a2), math.Sin(a1)+math.Sin(a2)
	if math.Hypot(sx, sy) < 1e-5 {
		return Fl(a1 - math.Pi/2)
	}
	return Fl(math.Atan2(sy, sx))
}

// markVertices zwraca wierzchołki ścieżki złożonej z podanych podścieżek, z kątem dla orient="auto".
func markVertices(subpaths []markSubpath) []vertex {
	var out []vertex
	for _, sp := range subpaths {
		sp.fixZeroDirections()
		n := len(sp.segs)
		// wierzchołek początkowy
		angle := Fl(0)
		if n != 0 {
			first := sp.segs[0]
			angle = dirAngle(first.startDir)
			if sp.closed {
				angle = bisectAngle(sp.segs[n-1].endDir, first.startDir)
			}
		}
		if !sp.implicit {
			out = append(out, vertex{sp.start.x, sp.start.y, angle})
		}
		for i, s := range sp.segs {
			in := s.endDir
			switch {
			case i+1 < n:
				angle = bisectAngle(in, sp.segs[i+1].startDir)
			case sp.closed:
				angle = bisectAngle(in, sp.segs[0].startDir)
			default:
				angle = dirAngle(in)
			}
			out = append(out, vertex{s.end.x, s.end.y, angle})
		}
	}
	return out
}

// close zamyka podścieżkę odcinkiem do punktu początkowego. Gdy bieżący punkt już jest punktem
// początkowym, zamknięcie nie tworzy osobnego wierzchołka (tak rysują wzorce WPT marker-path-022/023
// i Chromium), chyba że podścieżka nie ma jeszcze żadnego odcinka.
func (sp *markSubpath) close(current point) {
	if current != sp.start || len(sp.segs) == 0 {
		sp.segs = append(sp.segs, lineSegment(current, sp.start))
	}
	sp.closed = true
}

// pathSubpaths dzieli elementy ścieżki na podścieżki do wyznaczenia wierzchołków.
func pathSubpaths(items []pathItem) []markSubpath {
	var (
		out     []markSubpath
		current point
		cur     = -1 // indeks bieżącej podścieżki
	)
	ensure := func() {
		if cur == -1 {
			// podścieżka niejawna: po Z albo bez M (SVG 2 § 9.3.4, test WPT marker-implicit-subpaths)
			sp := markSubpath{start: current}
			if len(out) != 0 {
				sp.start, sp.implicit = out[len(out)-1].start, true
			}
			out = append(out, sp)
			cur = len(out) - 1
		}
	}
	for _, item := range items {
		switch item.op {
		case moveTo:
			out = append(out, markSubpath{start: item.args[0]})
			cur = len(out) - 1
			current = item.args[0]
		case lineTo:
			ensure()
			out[cur].segs = append(out[cur].segs, lineSegment(current, item.args[0]))
			current = item.args[0]
		case cubicTo:
			ensure()
			seg := cubicSegment(current, item.args[0], item.args[1], item.args[2])
			if segs := out[cur].segs; item.joined && len(segs) != 0 {
				// dalszy kawałek tego samego łuku: przedłuża poprzedni odcinek
				last := &segs[len(segs)-1]
				last.end, last.endDir, last.zeroEnd = seg.end, seg.endDir, seg.zeroEnd
			} else {
				out[cur].segs = append(out[cur].segs, seg)
			}
			current = item.args[2]
		case close:
			ensure()
			out[cur].close(current)
			out[cur].closed = true
			current = out[cur].start
			cur = -1
		}
	}
	return out
}
