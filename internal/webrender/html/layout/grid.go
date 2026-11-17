package layout

import (
	"fmt"
	"sort"

	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// Layout for grid containers and grid-items.

func isLength(sizing pr.TaggedDim) bool { return sizing.Unit != 0 && sizing.Unit != pr.Fr }

func isFr(sizing pr.TaggedDim) bool { return sizing.Unit == pr.Fr }

func intersect(position1, size1, position2, size2 int) bool {
	return position1 < position2+size2 && position2 < position1+size1
}

func intersectWithChildren(x, y, width, height int, positions map[Box]rect) bool {
	for _, rect := range positions {
		fullX, fullY, fullWidth, fullHeight := rect.unpack()
		xIntersect := intersect(x, width, fullX, fullWidth)
		yIntersect := intersect(y, height, fullY, fullHeight)
		if xIntersect && yIntersect {
			return true
		}
	}
	return false
}

// definiteTrackSize zwraca rozmiar ścieżki znany przed układem: funkcję maksymalną, jeśli jest
// długością albo procentem, w przeciwnym razie minimalną; ok=false, gdy żadna nie jest określona.
func definiteTrackSize(track pr.GridSpec, available pr.Float) (pr.Float, bool) {
	dims, isDims := track.(pr.GridDims)
	if !isDims {
		return 0, false
	}
	functions := dims.SizingFunctions()
	resolve := func(f pr.TaggedDim) (pr.Float, bool) {
		switch {
		case f.Tag != 0:
		case f.Unit == pr.Px:
			return f.Value + f.Offset, true
		case f.Unit == pr.Perc:
			return available*f.Value/100 + f.Offset, true
		}
		return 0, false
	}
	minimum, hasMin := resolve(functions[0])
	if maximum, ok := resolve(functions[1]); ok {
		// gopdfgo: gdy obie funkcje są określone, maksimum nie schodzi poniżej minimum
		// (CSS Grid 1 § 7.2.3.2): minmax(50px, 25px) liczy się jak 50px
		if hasMin {
			maximum = max(maximum, minimum)
		}
		return maximum, true
	}
	return minimum, hasMin
}

// autoRepeatCount liczy powtórzenia dla repeat(auto-fill | auto-fit, ...): największą liczbę, przy której
// ścieżki z odstępami mieszczą się w kontenerze (CSS Grid 1 § 7.2.3.2); jedno powtórzenie, gdy rozmiar
// kontenera albo którejś powtarzanej ścieżki nie jest określony.
func autoRepeatCount(tracks pr.GridTemplate, repeat pr.GridRepeat, available pr.MaybeFloat, gap pr.Float) int {
	if available == nil || available == pr.AutoF {
		return 1
	}
	var fixed, unit pr.Float
	others, perRepeat := 0, 0
	for j, track := range repeat.Names {
		if j%2 == 0 {
			continue
		}
		size, ok := definiteTrackSize(track, available.V())
		if !ok {
			return 1
		}
		unit += size
		perRepeat++
	}
	for i, track := range tracks.Names {
		if i%2 == 0 {
			continue
		}
		if r, isRepeat := track.(pr.GridRepeat); isRepeat {
			if r.Repeat > 0 {
				for j, inner := range r.Names {
					if j%2 != 0 {
						size, _ := definiteTrackSize(inner, available.V())
						fixed += size * pr.Float(r.Repeat)
						others += r.Repeat
					}
				}
			}
			continue
		}
		size, _ := definiteTrackSize(track, available.V())
		fixed += size
		others++
	}
	if perRepeat == 0 || unit+gap*pr.Float(perRepeat) <= 0 {
		return 1
	}
	// fixed + n*unit + (others + n*perRepeat - 1)*gap <= available
	n := int((available.V() - fixed - pr.Float(others-1)*gap + 1e-6) / (unit + pr.Float(perRepeat)*gap))
	return max(n, 1)
}

// gopdfgo: available i gap służą tylko do repeat(auto-fill | auto-fit); w źródle oba dawały jedno
// powtórzenie z ostrzeżeniem. Puste ścieżki auto-fit zwija potem collapseEmptyAutoFit.
func getTemplateTracks(tracks pr.GridTemplate, available pr.MaybeFloat, gap pr.Float) []pr.GridSpec {
	if tracks.Tag == pr.None {
		tracks.Names = []pr.GridSpec{pr.GridNames{}}
	}
	if tracks.Tag == pr.Subgrid {
		// TODO: Support subgrids.
		logger.WarningLogger.Println("Subgrids are unsupported")
		return []pr.GridSpec{pr.GridNames{}}
	}
	var tracksList []pr.GridSpec
	for i, track := range tracks.Names {
		if i%2 != 0 {
			// Track size.
			if trackR, isRepeat := track.(pr.GridRepeat); isRepeat {
				repeatNumber := trackR.Repeat
				if repeatNumber == pr.RepeatAutoFill || repeatNumber == pr.RepeatAutoFit {
					repeatNumber = autoRepeatCount(tracks, trackR, available, gap)
				}
				for _c := 0; _c < repeatNumber; _c++ {
					for j, repeatTrack := range trackR.Names {
						if j%2 != 0 {
							// Track size in repeat.
							tracksList = append(tracksList, repeatTrack)
						} else {
							// Line names in repeat.
							if len(tracksList)%2 != 0 {
								tracksList[len(tracksList)-1] = append(tracksList[len(tracksList)-1].(pr.GridNames), repeatTrack.(pr.GridNames)...)
							} else {
								tracksList = append(tracksList, repeatTrack)
							}
						}
					}
				}
			} else {
				tracksList = append(tracksList, track)
			}
		} else {
			// Line names.
			if len(tracksList)%2 != 0 {
				tracksList[len(tracksList)-1] = append(tracksList[len(tracksList)-1].(pr.GridNames), track.(pr.GridNames)...)
			} else {
				tracksList = append(tracksList, track)
			}
		}
	}
	return tracksList
}

// autoFitRange zwraca położenie ścieżek pochodzących z repeat(auto-fit, …):
// numer pierwszej (od zera, w siatce jawnej) i ich liczbę; (0, 0), gdy
// szablon nie ma takiego powtórzenia.
func autoFitRange(tracks pr.GridTemplate, available pr.MaybeFloat, gap pr.Float) (first, count int) {
	if tracks.Tag == pr.None || tracks.Tag == pr.Subgrid {
		return 0, 0
	}
	before := 0
	for i, track := range tracks.Names {
		if i%2 == 0 {
			continue
		}
		repeat, isRepeat := track.(pr.GridRepeat)
		if !isRepeat {
			before++
			continue
		}
		perRepeat := len(repeat.Names) / 2
		switch repeat.Repeat {
		case pr.RepeatAutoFit:
			return before, perRepeat * autoRepeatCount(tracks, repeat, available, gap)
		case pr.RepeatAutoFill:
			before += perRepeat * autoRepeatCount(tracks, repeat, available, gap)
		default:
			before += perRepeat * repeat.Repeat
		}
	}
	return 0, 0
}

// collapseEmptyAutoFit usuwa z listy ścieżek te ścieżki auto-fit, w których
// nie leży żaden element, i przesuwa współrzędne elementów za nimi.
//
// gopdfgo: CSS Grid 2 § 7.2.3.2 każe pustą ścieżkę auto-fit zwinąć: dostaje
// rozmiar zero, a odstępy po jej obu stronach zlewają się w jeden. Wynik
// jest taki sam jak po usunięciu ścieżki, bo żaden element jej nie obejmuje.
// Źródło układało auto-fit jak auto-fill, więc elementy nie rozkładały się
// na całą szerokość kontenera.
// removed: numery usuniętych ścieżek (w numeracji sprzed zwijania), potrzebne do przeliczenia linii
// podanych przez elementy pozycjonowane.
func collapseEmptyAutoFit(specs []pr.GridSpec, first, count, implicitBefore int, positions map[Box]rect, axis int) (_ []pr.GridSpec, removed []int) {
	if count == 0 {
		return specs, nil
	}
	occupied := map[int]bool{}
	for _, r := range positions {
		for k := 0; k < r[axis+2]; k++ {
			occupied[r[axis]+implicitBefore+k] = true
		}
	}
	for k := implicitBefore + first + count - 1; k >= implicitBefore+first; k-- {
		if occupied[k] || 2*k+2 >= len(specs) {
			continue
		}
		specs = append(specs[:2*k+1:2*k+1], specs[2*k+3:]...)
		removed = append(removed, k)
		for child, r := range positions {
			if r[axis]+implicitBefore > k {
				r[axis]--
				positions[child] = r
			}
		}
	}
	return specs, removed
}

type maybeInt struct {
	valid bool
	i     int
}

func getLine(line pr.GridLine, lines []pr.GridNames, side string) (isSpan bool, _ int, _ string, coord maybeInt) {
	isSpan, number, ident := line.IsSpan(), line.Val, line.Ident
	if ident != "" && line.IsCustomIdent() {
		coord.valid = true
		hasBroken := false
		var (
			line pr.GridNames
			tag  = fmt.Sprintf("%s-%s", ident, side)
		)
		for coord.i, line = range lines {
			if utils.IsIn(line, tag) {
				hasBroken = true
				break
			}
		}
		if !hasBroken {
			number = 1
		}
	}
	if number != 0 && !isSpan {
		coord.valid = true
		if ident == "" {
			// gopdfgo: liczba ujemna liczy linie od końca siatki jawnej (CSS Grid 1 § 8.3): -1 to
			// ostatnia linia. W źródle każda liczba dawała number - 1, czyli -1 stawało się linią -2.
			if number > 0 {
				coord.i = number - 1
			} else {
				coord.i = len(lines) + number
			}
		} else {
			step := -1
			if number > 0 {
				step = 1
			}
			L := len(lines) / step
			hasBroken := false
			for coord.i = 0; coord.i < L; coord.i++ {
				line := lines[coord.i*step]
				if utils.IsIn(line, ident) {
					number -= step
					hasBroken = true
					break
				}
				if number == 0 {
					hasBroken = true
					break
				}
			}
			if !hasBroken {
				coord.i += utils.Abs(number)
			}

			if step == -1 {
				coord.i = len(lines) - 1 - coord.i
			}
		}
	}
	if isSpan {
		coord.valid = false
	}
	return isSpan, number, ident, coord
}

type placement [2]int // coord, size

func (pl placement) isNotNone() bool { return pl[1] != 0 }

func (pl placement) unpack() (coord, size int) { return pl[0], pl[1] }

// Input coordinates are 1-indexed, returned coordinates are 0-indexed.
func getPlacement(start, end pr.GridLine, lines []pr.GridNames) placement {
	if start.IsAuto() || start.Tag == pr.Span {
		if end.IsAuto() || end.Tag == pr.Span {
			return placement{}
		}
	}
	var (
		coord            maybeInt
		size             int
		isSpan           bool
		number           int
		spanIdent, ident string
	)
	if start.Tag != pr.Auto {
		isSpan, number, ident, coord = getLine(start, lines, "start")
		if isSpan {
			size = 1
			if number != 0 {
				size = number
			}
			spanIdent = ident
		}
	} else {
		size = 1
		spanIdent = ""
	}
	if end.Tag != pr.Auto {
		var coordEnd maybeInt
		isSpan, number, ident, coordEnd = getLine(end, lines, "end")
		if isSpan {
			size = 1
			if number != 0 {
				size = number
			}
			spanNumber := size
			spanIdent = ident
			if spanIdent != "" {
				hasBroken := false
				for index, line := range pySlice(lines, coord.i+1, len(lines)) {
					size = index + 1
					if utils.IsIn(line, spanIdent) {
						spanNumber -= 1
					}
					if spanNumber == 0 {
						hasBroken = true
						break
					}
				}
				if !hasBroken {
					size += spanNumber
				}
			}
		} else if coord.valid {
			size = coordEnd.i - coord.i
		}
		if !coord.valid {
			if spanIdent == "" {
				coord = maybeInt{valid: true, i: coordEnd.i - size}
			} else {
				if number == 0 {
					number = 1
				}
				if coordEnd.i > 0 {
					slice := pySlice(lines, coordEnd.i-1, len(lines))
					hasBroken := false
					for coord.i = range slice {
						line := slice[len(slice)-1-coord.i] // reverse
						if utils.IsIn(line, spanIdent) {
							number -= 1
						}
						if number == 0 {
							coord.i = coordEnd.i - 1 - coord.i
							hasBroken = true
							break
						}
					}
					if !hasBroken {
						coord.i = -number
					}
				} else {
					coord = maybeInt{valid: true, i: -number}
				}
			}
			size = coordEnd.i - coord.i
		}
	} else {
		size = 1
	}

	if size < 0 {
		size = -size
		coord.i -= size
	}
	if size == 0 {
		size = 1
	}
	return placement{coord.i, size}
}

func getSpan(place pr.GridLine) int {
	// TODO: Handle lines.
	span := 1
	if place.IsSpan() && place.Val != 0 {
		span = place.Val
	}
	return span
}

func getColumnPlacement(rowPlacement [2]int, columnStart, columnEnd pr.GridLine,
	columns []pr.GridNames, childrenPositions map[Box]rect, dense bool,
) placement {
	occupiedColumns := map[int]bool{}
	for _, rect := range childrenPositions {
		x, y, width, height := rect.unpack()
		// Test whether cells overlap.
		if intersect(y, height, rowPlacement[0], rowPlacement[1]) {
			for xv := x; xv < x+width; xv++ {
				occupiedColumns[xv] = true
			}
		}
	}
	if dense {
		for x := 0; ; x++ {
			if occupiedColumns[x] {
				continue
			}
			var pl placement
			if columnStart.IsAuto() {
				pl = getPlacement(pr.GridLine{Val: x + 1}, columnEnd, columns)
			} else {
				if columnStart.Tag != pr.Span { // gopdfgo: warunek odwrócony w źródle, jak niżej
					panic("expected span")
				}
				// If the placement contains two spans, remove the one
				// contributed by the end grid-placement property.
				// https://drafts.csswg.org/css-grid/#grid-placement-errors
				span := getSpan(columnStart)
				pl = getPlacement(columnStart, pr.GridLine{Val: x + 1 + span}, columns)
			}
			hasIntersection := false
			for col := pl[0]; col < pl[0]+pl[1]; col++ {
				if occupiedColumns[col] {
					hasIntersection = true
					break
				}
			}
			if !hasIntersection {
				return pl
			}
		}
	} else {
		// gopdfgo: w wierszu bez zajętych komórek element trafia do pierwszej kolumny (CSS Grid 1 § 8.5,
		// krok 2); źródło zaczynało od zera i dodawało jeden, więc pusty wiersz dawał kolumnę drugą
		y := -1
		for k := range occupiedColumns {
			if k > y {
				y = k
			}
		}
		y += 1
		if columnStart.IsAuto() {
			return getPlacement(pr.GridLine{Val: y + 1}, columnEnd, columns)
		} else {
			// gopdfgo: w źródle warunek był odwrócony względem asercji z WeasyPrint
			// (assert column_start[0] == 'span') i panika padała właśnie dla poprawnego przypadku.
			if columnStart.Tag != pr.Span {
				panic("expected span")
			}
			// If the placement contains two spans, remove the one contributed
			// by the end grid-placement property.
			// https://drafts.csswg.org/css-grid/#grid-placement-errors
			for endY := y + 1; ; endY++ {
				placement := getPlacement(columnStart, pr.GridLine{Val: endY + 1}, columns)
				if placement[0] >= y {
					return placement
				}
			}
		}
	}
}

const (
	sizeMin byte = iota
	sizeMax
)

// affectedSizes : sizeMin or sizeMax ("min", "max")
// affectedTracksTypes : i, c, m ("intrinsic", "content-based", "max-content")
// sizeContribution : m, c, C ("minimum", "min-content", "max-content")
// direction : x, y
func distributeExtraSpace(context *layoutContext, affectedSizes, affectedTracksTypes, sizeContribution byte, tracksChildren [][]Box,
	sizingFunctions [][2]pr.TaggedDim, tracksSizes [][2]pr.MaybeFloat, span int, direction byte, containingBlock *bo.BoxFields,
) {
	// 1. Maintain separately for each affected track a planned increase.
	plannedIncreases := make([]pr.Float, len(tracksSizes))

	// 2. Distribute space.
	affectedTracks := make([]bool, len(sizingFunctions))
	for i, functions := range sizingFunctions {
		function := functions[affectedSizes]
		isAffected := false
		switch affectedTracksTypes {
		case 'i':
			isAffected = function.Tag == pr.MinContent || function.Tag == pr.MaxContent || function.Tag == pr.Auto
		case 'c':
			isAffected = function.Tag == pr.MinContent || function.Tag == pr.MaxContent
		case 'm':
			isAffected = function.Tag == pr.MaxContent || function.Tag == pr.Auto
		}
		affectedTracks[i] = isAffected
	}
	for i, children := range tracksChildren {
		if len(children) == 0 {
			continue
		}
		for _, item := range children {
			// 2.1 Find the space distribution.
			// TODO: Differenciate minimum && min-content values.
			// TODO: Find a better way to get height.
			var space pr.Float
			if direction == 'x' {
				// gopdfgo: wkład minimalny elementu z podanym min-width to ten min-width z obramowaniem,
				// odstępem i marginesami, a nie szerokość min-content (CSS Grid 1 § 6.6, § 11.5)
				minimum := func() pr.Float {
					if minWidth := item.Box().Style.GetMinWidth(); minWidth.Tag != pr.Auto && minWidth.Unit == pr.Px &&
						item.Box().Style.GetWidth().Tag == pr.Auto {
						return marginWidth(item.Box(), minWidth.Value, true, true)
					}
					return minContentWidth(context, item, true)
				}
				switch sizeContribution {
				case 'm':
					space = minimum()
				case 'c':
					space = minContentWidth(context, item, true)
				default:
					space = maxContentWidth(context, item, true)
					// gopdfgo: wkład max-content ograniczony sumą stałych funkcji maksimum ścieżek,
					// ale nie mniejszy niż wkład minimalny (§ 11.5, „limited max-content contribution”)
					if affectedSizes == sizeMin {
						limit, fixed := pr.Float(0), true
						for j := i; j < i+span && j < len(sizingFunctions); j++ {
							if f := sizingFunctions[j][1]; isLength(f) && f.Unit == pr.Px {
								limit += f.Value
							} else {
								fixed = false
							}
						}
						if fixed {
							space = max(minimum(), min(space, limit))
						}
					}
				}
			} else {
				item = bo.Deepcopy(item)
				item.Box().PositionX = 0
				item.Box().PositionY = 0
				item, _, _ = blockLevelLayout(context, item.(bo.BlockLevelBoxITF), -pr.Inf, nil,
					containingBlock, true, nil, nil, nil, false, -1)
				space = item.Box().MarginHeight()
			}
			for _, sizes := range tracksSizes[min(i, len(tracksSizes)):min(i+span, len(tracksSizes))] {
				space -= sizes[affectedSizes].V()
			}
			space = max(0, space)
			// 2.2 Distribute space up to limits.
			var affectedTracksNumbers, unaffectedTracksNumbers []int
			for j := i; j < i+span && j < len(affectedTracks); j++ {
				if affectedTracks[j] {
					affectedTracksNumbers = append(affectedTracksNumbers, j)
				} else {
					unaffectedTracksNumbers = append(unaffectedTracksNumbers, j)
				}
			}
			// tracksNumbers = list(enumerate(affectedTracks[i:i+span], i))
			// affectedTracksNumbers = [                j for j, affected := range tracksNumbers if affected]
			itemIncurredIncreases := make([]pr.Float, len(sizingFunctions))
			// gopdfgo: rozdział po równo z zamrażaniem ścieżek, które doszły do granicy wzrostu,
			// i dalszym rozdawaniem reszty niezamrożonym (CSS Grid 1 § 11.5.1, krok 2.2). Źródło robiło
			// jedną rundę, a przy rozmiarze już ponad granicą dawało przyrost ujemny i „odzyskane”
			// miejsce oddawało innym ścieżkom.
			distribute := func(tracks []int) {
				active := append([]int(nil), tracks...)
				for space > 0.0001 && len(active) != 0 {
					share := space / pr.Float(len(active))
					var next []int
					for _, trackNumber := range active {
						room := tracksSizes[trackNumber][1].V() - tracksSizes[trackNumber][affectedSizes].V() -
							itemIncurredIncreases[trackNumber]
						if room <= share {
							room = max(room, 0)
							itemIncurredIncreases[trackNumber] += room
							space -= room
						} else {
							itemIncurredIncreases[trackNumber] += share
							space -= share
							next = append(next, trackNumber)
						}
					}
					if len(next) == len(active) {
						break
					}
					active = next
				}
			}
			distribute(affectedTracksNumbers)
			// 2.3 Distribute space to non-affected tracks.
			if len(affectedTracksNumbers) != 0 {
				distribute(unaffectedTracksNumbers)
			}
			// 2.4 Distribute space beyond limits.
			// gopdfgo: miejsce, które zostało po dojściu ścieżek do granic wzrostu, dostają ścieżki
			// objęte rozkładem o funkcji maksimum zależnej od treści, a gdy takich nie ma, wszystkie
			// objęte (CSS Grid 1 § 11.5.1, krok 2.4). Źródło miało tu TODO, więc element rozpięty na
			// kilka ścieżek mógł się w nich nie mieścić.
			if space > 0.001 && affectedSizes == sizeMin && len(affectedTracksNumbers) != 0 {
				var beyond []int
				for _, trackNumber := range affectedTracksNumbers {
					switch sizingFunctions[trackNumber][1].Tag {
					case pr.MinContent, pr.MaxContent, pr.Auto:
						beyond = append(beyond, trackNumber)
					}
				}
				if len(beyond) == 0 {
					beyond = affectedTracksNumbers
				}
				share := space / pr.Float(len(beyond))
				for _, trackNumber := range beyond {
					itemIncurredIncreases[trackNumber] += share
				}
			}
			// 2.5. Set the track’s planned increase.
			for k, extra := range itemIncurredIncreases {
				if extra > plannedIncreases[k] {
					plannedIncreases[k] = extra
				}
			}
		}
	}
	// 3. Update the tracks’ affected size.
	for i, increase := range plannedIncreases {
		if affectedSizes == sizeMax && tracksSizes[i][1] == pr.Inf {
			tracksSizes[i][1] = tracksSizes[i][0].V() + increase
		} else {
			tracksSizes[i][affectedSizes] = tracksSizes[i][affectedSizes].V() + increase
		}
	}
}

// direction : 'x' or 'y'
func resolveTracksSizes(context *layoutContext, sizingFunctions [][2]pr.TaggedDim, boxSize pr.MaybeFloat, childrenPositions map[Box]rect,
	implicitStart int, direction byte, gap pr.Float,
	containingBlock bo.Box, orthogonalSizes [][2]pr.Float,
) [][2]pr.Float {
	percentBoxSize := pr.Float(0)
	if boxSize != pr.AutoF {
		percentBoxSize = boxSize.V()
	} else {
		// gopdfgo: przy nieokreślonym rozmiarze kontenera ścieżka procentowa liczy się jak auto
		// (CSS Grid 1 § 7.2.1); źródło dawało jej zero
		withAuto := make([][2]pr.TaggedDim, len(sizingFunctions))
		for i, funcs := range sizingFunctions {
			for k, f := range funcs {
				if isLength(f) && f.Unit == pr.Perc {
					f = pr.TaggedDim{Tag: pr.Auto}
				}
				withAuto[i][k] = f
			}
		}
		sizingFunctions = withAuto
	}

	// 1.1 Initialize track sizes.
	tracksSizes := make([][2]pr.MaybeFloat, len(sizingFunctions))
	for i, funcs := range sizingFunctions {
		minFunction, maxFunction := funcs[0], funcs[1]
		var baseSize pr.MaybeFloat
		if isLength(minFunction) {
			baseSize = pr.ResolvePercentage(minFunction, percentBoxSize)
		} else if minFunction.Tag == pr.MinContent || minFunction.Tag == pr.MaxContent || minFunction.Tag == pr.Auto {
			baseSize = pr.Float(0)
		}
		var growthLimit pr.MaybeFloat
		if isLength(maxFunction) {
			growthLimit = pr.ResolvePercentage(maxFunction, percentBoxSize)
		} else if (maxFunction.Tag == pr.MinContent || maxFunction.Tag == pr.MaxContent || maxFunction.Tag == pr.Auto) ||
			isFr(maxFunction) {
			growthLimit = pr.Inf
		}
		if baseSize != nil && growthLimit != nil {
			growthLimit = max(baseSize.V(), growthLimit.V())
		}
		tracksSizes[i] = [2]pr.MaybeFloat{baseSize, growthLimit}
	}

	if traceMode {
		traceLogger.Dump(fmt.Sprintf("resolveTracksSizes(1): %v", tracksSizes))
	}

	// 1.2 Resolve intrinsic track sizes.
	// 1.2.1 Shim baseline-aligned items.
	// TODO: Shim items.
	// 1.2.2 Size tracks to fit non-spanning items.
	// gopdfgo: wysokość treści elementów leżących w jednym wierszu elastycznym; przy nieokreślonej
	// wysokości kontenera wyznacza ułamek fr (CSS Grid 1 § 11.7: wkład max-content elementu podzielony
	// przez sumę współczynników, nie mniejszą niż 1). W źródle "TODO: Respect grid items max-content
	// contribution", więc wiersz minmax(0, Nfr) z treścią miał wysokość zero.
	frContent := make([]pr.Float, len(tracksSizes))
	tracksChildren := make([][]Box, len(tracksSizes))
	for child, rect := range childrenPositions {
		x, y, width, height := rect.unpack()
		coord, size := y, height
		if direction == 'x' {
			coord, size = x, width
		}
		if size != 1 {
			continue
		}
		if i := coord - implicitStart; i >= 0 && i < len(tracksChildren) { // gopdfgo: zakres sprawdzany
			tracksChildren[i] = append(tracksChildren[i], child)
		}
	}

	for i, children := range tracksChildren {
		minFunction, maxFunction := sizingFunctions[i][0], sizingFunctions[i][1]
		sizes := &tracksSizes[i]
		if len(children) == 0 {
			continue
		}
		if direction == 'y' {
			// TODO: Find a better way to get height.
			height := pr.Float(0)
			for _, child := range children {
				pos := childrenPositions[child]
				x, _, width, _ := pos.unpack()
				widthF := sum0(pySlice(orthogonalSizes, x, x+width))
				child = bo.Deepcopy(child)
				child.Box().PositionX = 0
				child.Box().PositionY = 0
				parent := bo.BlockT.AnonymousFrom(containingBlock, nil)
				cbW, cbH := containingBlock.Box().ContainingBlock()
				resolvePercentages(parent, bo.MaybePoint{cbW, cbH})
				parent.Box().PositionX = child.Box().PositionX
				parent.Box().PositionY = child.Box().PositionY
				parent.Box().Width = widthF
				// gopdfgo: przy wyznaczaniu wkładu elementu w wiersz wysokość obszaru siatki nie jest
				// jeszcze znana, więc wysokości procentowe liczą się jak auto (CSS Grid 1 § 6.5, CSS Sizing 3
				// § 5.2.1); źródło podstawiało największą dotąd zmierzoną wysokość, na początku zero, więc
				// obraz z height: 100% miał wysokość zero
				parent.Box().Height = pr.AutoF
				bottomSpace := -pr.Inf
				child, _, _ = blockLevelLayout(context, child.(bo.BlockLevelBoxITF), bottomSpace, nil,
					parent.Box(), true, nil, nil, nil, false, -1)
				height = max(height, child.Box().MarginHeight())
			}
			if isFr(maxFunction) {
				frContent[i] = height
			}
			if minFunction.Tag == pr.MinContent || minFunction.Tag == pr.MaxContent || minFunction.Tag == pr.Auto {
				sizes[0] = height
			}
			if maxFunction.Tag == pr.MinContent || maxFunction.Tag == pr.MaxContent {
				sizes[1] = height
			}
			if sizes[0] != nil && sizes[1] != nil {
				sizes[1] = max(sizes[0].V(), sizes[1].V())
			}
			continue
		}
		if minFunction.Tag == pr.MinContent {
			ma := pr.Float(0)
			for _, child := range children {
				if v := minContentWidth(context, child, true); v > ma {
					ma = v
				}
			}
			sizes[0] = ma
		} else if minFunction.Tag == pr.MaxContent {
			ma := pr.Float(0)
			for _, child := range children {
				if v := maxContentWidth(context, child, true); v > ma {
					ma = v
				}
			}
			sizes[0] = ma
		} else if minFunction.Tag == pr.Auto {
			// TODO: Handle min-/max-content constrained parents.
			// TODO: Use real "minimum contributions".
			ma := pr.Float(0)
			for _, child := range children {
				if v := minContentWidth(context, child, true); v > ma {
					ma = v
				}
			}
			sizes[0] = ma
		}
		if maxFunction.Tag == pr.MinContent {
			ma := -pr.Inf
			for _, child := range children {
				if v := minContentWidth(context, child, true); v > ma {
					ma = v
				}
			}
			sizes[1] = ma
		} else if maxFunction.Tag == pr.Auto || maxFunction.Tag == pr.MaxContent {
			ma := -pr.Inf
			for _, child := range children {
				if v := maxContentWidth(context, child, true); v > ma {
					ma = v
				}
			}
			sizes[1] = ma
			if sizes[0] != nil && sizes[1] != nil {
				sizes[1] = max(sizes[0].V(), sizes[1].V())
			}
		}
	}

	if traceMode {
		traceLogger.Dump(fmt.Sprintf("resolveTracksSizes(2): %v", tracksSizes))
	}

	// 1.2.3 Increase sizes to accommodate items spanning content-sized tracks.
	var spans []int
	for _, rect := range childrenPositions {
		v := rect[2] // width
		if direction == 'y' {
			v = rect[3] // height
		}
		if v >= 2 {
			spans = append(spans, v)
		}
	}
	sort.Ints(spans)

	for _, span := range spans {
		tracksChildren := make([][]Box, len(sizingFunctions))
		i := -1
		for child, rect := range childrenPositions {
			i++
			x, y, width, height := rect.unpack()
			coord, size := x, width
			if direction == 'y' {
				coord, size = y, height
			}
			if size != span {
				continue
			}
			hasFr := false
			// gopdfgo: ścieżki, które element zajmuje; źródło brało zakres od numeru kolejnego elementu
			// w (losowej) kolejności mapy, więc o pominięciu elementu decydował przypadek
			first := min(max(coord-implicitStart, 0), len(sizingFunctions))
			for _, functions := range sizingFunctions[first:min(len(sizingFunctions), first+span)] {
				if isFr(functions[1]) {
					hasFr = true
					break
				}
			}
			if !hasFr {
				if i := coord - implicitStart; i >= 0 && i < len(tracksChildren) { // gopdfgo: zakres sprawdzany
					tracksChildren[i] = append(tracksChildren[i], child)
				}
			}
		}
		// 1.2.3.1 For intrinsic minimums.
		// TODO: Respect min-/max-content constraint.
		distributeExtraSpace(context, sizeMin, 'i', 'm', tracksChildren, sizingFunctions, tracksSizes, span, direction, containingBlock.Box())
		// 1.2.3.2 For content-based minimums.
		distributeExtraSpace(context, sizeMin, 'c', 'c', tracksChildren, sizingFunctions, tracksSizes, span, direction, containingBlock.Box())
		// 1.2.3.3 For max-content minimums.
		// TODO: Respect max-content constraint.
		distributeExtraSpace(context, sizeMin, 'm', 'C', tracksChildren, sizingFunctions, tracksSizes, span, direction, containingBlock.Box())
		// 1.2.3.4 Increase growth limit.
		for j, sizes := range tracksSizes {
			if sizes[0] != nil && sizes[1] != nil {
				tracksSizes[j][1] = max(sizes[0].V(), sizes[1].V())
			}
		}
		i = -1
		for child, rect := range childrenPositions {
			i++
			x, y, width, height := rect.unpack()
			coord, size := x, width
			if direction == 'y' {
				coord, size = y, height
			}
			if size != span {
				continue
			}

			hasFr := false
			// gopdfgo: ścieżki, które element zajmuje; źródło brało zakres od numeru kolejnego elementu
			// w (losowej) kolejności mapy, więc o pominięciu elementu decydował przypadek
			first := min(max(coord-implicitStart, 0), len(sizingFunctions))
			for _, functions := range sizingFunctions[first:min(len(sizingFunctions), first+span)] {
				if isFr(functions[1]) {
					hasFr = true
					break
				}
			}
			if !hasFr {
				if i := coord - implicitStart; i >= 0 && i < len(tracksChildren) { // gopdfgo: zakres sprawdzany
					tracksChildren[i] = append(tracksChildren[i], child)
				}
			}
		}
		// 1.2.3.5 For intrinsic maximums.
		distributeExtraSpace(context, sizeMax, 'i', 'c', tracksChildren, sizingFunctions, tracksSizes, span, direction, containingBlock.Box())
		// 1.2.3.6 For max-content maximums.
		distributeExtraSpace(context, sizeMax, 'm', 'C', tracksChildren, sizingFunctions, tracksSizes, span, direction, containingBlock.Box())
	}
	// 1.2.4 Increase sizes to accommodate items spanning flexible tracks.
	// TODO: Support spans for flexible tracks.
	// 1.2.5 Fix infinite growth limits.
	for i, sizes := range tracksSizes {
		if sizes[1] == pr.Inf {
			tracksSizes[i][1] = sizes[0]
		}
	}
	// 1.3 Maximize tracks.
	var freeSpace pr.MaybeFloat
	if boxSize != pr.AutoF {
		sum := pr.Float(0)
		for _, size := range tracksSizes {
			sum += size[0].V()
		}

		freeSpaceF := boxSize.V() - sum - pr.Float(len(tracksSizes)-1)*gap
		// gopdfgo: wolne miejsce dzielimy po równo między ścieżki, które mogą jeszcze rosnąć, a ścieżka,
		// która osiągnęła swój limit, wypada z podziału i reszta jest dzielona ponownie (CSS Grid 1
		// § 12.6). W źródle był jeden podział między wszystkie ścieżki, więc miejsce przypadające na
		// ścieżki o stałym rozmiarze przepadało.
		frozen := make([]bool, len(tracksSizes))
		for freeSpaceF > 1e-6 {
			growing := 0
			for i := range tracksSizes {
				if !frozen[i] && tracksSizes[i][0].V() < tracksSizes[i][1].V() {
					growing++
				} else {
					frozen[i] = true
				}
			}
			if growing == 0 {
				break
			}
			distributedFreeSpace := freeSpaceF / pr.Float(growing)
			for i := range tracksSizes {
				if frozen[i] {
					continue
				}
				sizes := &tracksSizes[i]
				baseSize, growthLimit := sizes[0].V(), sizes[1].V()
				if baseSize+distributedFreeSpace >= growthLimit {
					sizes[0] = growthLimit
					freeSpaceF -= growthLimit - baseSize
					frozen[i] = true
				} else {
					sizes[0] = baseSize + distributedFreeSpace
					freeSpaceF -= distributedFreeSpace
				}
			}
		}
		freeSpace = freeSpaceF
	}
	// TODO: Respect max-width/-height.
	// 1.4 Expand flexible tracks.
	var flexFraction pr.Float
	if freeSpace != nil && freeSpace.V() <= 0 {
		// TODO: Respect min-content constraint.
		flexFraction = 0
	} else if freeSpace != nil {
		stop := false
		inflexibleTracks := make([]bool, len(tracksSizes))
		var hypotheticalFrSize pr.Float
		for !stop {
			leftoverSpace := freeSpace.V()
			flexFactorSum := pr.Float(0)
			for i, sizes := range tracksSizes {
				maxFunction := sizingFunctions[i][1]
				if isFr(maxFunction) {
					leftoverSpace += sizes[0].V()
					if !inflexibleTracks[i] {
						flexFactorSum += maxFunction.Value
					}
				}
			}
			flexFactorSum = max(1, flexFactorSum)
			hypotheticalFrSize = leftoverSpace / flexFactorSum
			stop = true
			// iterable = enumerate(zip(tracksSizes, sizingFunctions))
			for i, sizes := range tracksSizes {
				maxFunction := sizingFunctions[i][1]
				if !inflexibleTracks[i] && isFr(maxFunction) {
					if hypotheticalFrSize*maxFunction.Value < sizes[0].V() {
						inflexibleTracks[i] = true
						stop = false
					}
				}
			}
		}
		flexFraction = hypotheticalFrSize
	} else {
		flexFraction = 0
		for i, sizes := range tracksSizes {
			maxFunction := sizingFunctions[i][1]
			if isFr(maxFunction) {
				if maxFunction.Value > 1 {
					flexFraction = max(flexFraction, maxFunction.Value*sizes[0].V())
				} else {
					flexFraction = max(flexFraction, sizes[0].V())
				}
			}
		}
		for i, content := range frContent {
			if maxFunction := sizingFunctions[i][1]; isFr(maxFunction) && content > 0 {
				flexFraction = max(flexFraction, content/max(maxFunction.Value, 1))
			}
		}
		// TODO: Respect min-* constraint.
	}
	for i, sizes := range tracksSizes {
		maxFunction := sizingFunctions[i][1]
		if isFr(maxFunction) {
			if flexFraction*maxFunction.Value > sizes[0].V() {
				if freeSpace != nil {
					freeSpace = freeSpace.V() - flexFraction*maxFunction.Value
				}
				tracksSizes[i][0] = flexFraction * maxFunction.Value
			}
		}
	}
	// 1.5 Expand stretched auto tracks.
	justifyContent := containingBlock.Box().Style.GetJustifyContent()
	alignContent := containingBlock.Box().Style.GetAlignContent()
	xStretch := direction == 'x' && justifyContent.Intersects(pr.Normal, pr.Stretch)
	yStretch := direction == 'y' && alignContent.Intersects(pr.Normal, pr.Stretch)
	if (xStretch || yStretch) && freeSpace != nil && freeSpace.V() > 0 {
		var autoTracksSizes []*[2]pr.MaybeFloat
		for i := range tracksSizes {
			// gopdfgo: rozciągane są ścieżki o funkcji maksimum auto (CSS Grid 1 § 11.8); źródło
			// patrzyło na funkcję minimum, więc minmax(25px, auto) zostawało bez rozciągnięcia
			if sizingFunctions[i][1].Tag == pr.Auto {
				autoTracksSizes = append(autoTracksSizes, &tracksSizes[i])
			}
		}
		if len(autoTracksSizes) != 0 {
			distributedFreeSpace := freeSpace.V() / pr.Float(len(autoTracksSizes))
			for _, ptr := range autoTracksSizes {
				ptr[0] = ptr[0].V() + distributedFreeSpace
			}
		}
	}

	out := make([][2]pr.Float, len(tracksSizes))
	for i, m := range tracksSizes {
		out[i] = [2]pr.Float{m[0].V(), m[1].V()}
	}
	return out
}

// return the equivalent of Python l[::2]
func extractNames(rows []pr.GridSpec) []pr.GridNames {
	var names []pr.GridNames
	for i, row := range rows {
		if i%2 != 0 {
			continue
		}
		names = append(names, row.(pr.GridNames))
	}
	return names
}

// return the equivalent of Python l[1::2]
func extractDims(rows []pr.GridSpec) [][2]pr.TaggedDim {
	var dims [][2]pr.TaggedDim
	for i, row := range rows {
		if i%2 != 1 {
			continue
		}
		dims = append(dims, row.(pr.GridDims).SizingFunctions())
	}
	return dims
}

func sum0(l [][2]pr.Float) pr.Float {
	su := pr.Float(0)
	for _, size := range l {
		su += size[0]
	}
	return su
}

type rect [4]int

func (ir rect) unpack() (x, y, width, height int) {
	return ir[0], ir[1], ir[2], ir[3]
}

func findBoxIndex(l []Box, v Box) int {
	for i, b := range l {
		if b == v {
			return i
		}
	}
	return -1
}

func gridLayout(context *layoutContext, box_ Box, bottomSpace pr.Float, skipStack tree.ResumeStack, containingBlock containingBlock,
	pageIsEmpty bool, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder,
) (Box, blockLayout) {
	// gopdfgo: wyrównanie do linii bazowej powiększa wiersz, gdy elementy po przesunięciu przestają się
	// w nim mieścić (CSS Grid 1 § 11.5 i CSS Align 3 § 9.3: do wkładu elementu w rozmiar ścieżki
	// dolicza się odsunięcie od wspólnej linii bazowej). Linie bazowe znamy dopiero po ułożeniu
	// elementów, więc pierwszy przebieg mierzy, o ile wiersze są za niskie, a drugi układa siatkę
	// z powiększonymi wierszami. Pudełka pozycjonowane zebrane w pierwszym przebiegu są odrzucane.
	absoluteCount, fixedCount := len(*absoluteBoxes), len(*fixedBoxes)
	// pierwszy przebieg wpisuje do pudełka wyliczoną wysokość; drugi ma znów zacząć od wysokości auto,
	// inaczej powiększone wiersze nie powiększyłyby kontenera
	originalHeight := box_.Box().Height
	out, layout, rowExtra := gridLayoutPass(context, box_, bottomSpace, skipStack, containingBlock, pageIsEmpty,
		absoluteBoxes, fixedBoxes, nil)
	if len(rowExtra) == 0 {
		return out, layout
	}
	*absoluteBoxes, *fixedBoxes = (*absoluteBoxes)[:absoluteCount], (*fixedBoxes)[:fixedCount]
	box_.Box().Height = originalHeight
	out, layout, _ = gridLayoutPass(context, box_, bottomSpace, skipStack, containingBlock, pageIsEmpty,
		absoluteBoxes, fixedBoxes, rowExtra)
	return out, layout
}

// gridLayoutPass układa siatkę; rowExtra to dodatki do wysokości wierszy z poprzedniego przebiegu
// (nil w pierwszym). Trzeci wynik to dodatki, których wymaga wyrównanie do linii bazowej.
func gridLayoutPass(context *layoutContext, box_ Box, bottomSpace pr.Float, skipStack tree.ResumeStack, containingBlock containingBlock,
	pageIsEmpty bool, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder, rowExtra map[int]pr.Float,
) (Box, blockLayout, map[int]pr.Float) {
	context.createBlockFormattingContext()
	// gopdfgo: pomiar szerokości wewnętrznej siatki, zob. gridContentWidth
	measure := context.gridMeasure
	if measure != nil && measure.target != box_.Box() {
		measure = nil
	}
	if measure != nil {
		box_.Box().Width = measure.constraint
	}

	// gopdfgo: dzieci pozycjonowane bezwzględnie nie są elementami siatki (CSS Grid 1 § 9): nie biorą udziału
	// w rozmieszczaniu ani w rozmiarach ścieżek. Dostają zastępnik, a gdy siatka jest ich pudełkiem
	// zawierającym, tym pudełkiem jest obszar wskazany przez grid-row i grid-column (linie auto to krawędzie
	// odstępu wewnętrznego kontenera). W źródle były układane jak zwykłe elementy siatki.
	var positionedChildren []Box
	for _, child := range box_.Box().Children {
		if child.Box().IsAbsolutelyPositioned() {
			positionedChildren = append(positionedChildren, child)
		}
	}
	if len(positionedChildren) != 0 {
		inFlow := make([]Box, 0, len(box_.Box().Children))
		for _, child := range box_.Box().Children {
			if !child.Box().IsAbsolutelyPositioned() {
				inFlow = append(inFlow, child)
			}
		}
		box_ = bo.CopyWithChildren(box_, inFlow)
	}
	// gopdfgo: siatka z position: relative jest pudełkiem zawierającym dla pozycjonowanych potomków;
	// źródło oddawało ich ułożenie dalszemu przodkowi.
	ownsPositioned := box_.Box().Style.GetPosition().String == "relative"
	if ownsPositioned {
		absoluteBoxes = &[]*AbsolutePlaceholder{}
	}

	// Define explicit grid
	box, style := box_.Box(), box_.Box().Style
	gridAreas := style.GetGridTemplateAreas()
	flow := style.GetGridAutoFlow()
	autoRows := style.GetGridAutoRows().Cycle()
	autoColumns := style.GetGridAutoColumns().Cycle()
	autoRowsBack := style.GetGridAutoRows().Reverse().Cycle()
	autoColumnsBack := style.GetGridAutoColumns().Reverse().Cycle()
	// gopdfgo: percentRowGap: row-gap z procentem przy wysokości auto. Do rozmiaru kontenera liczy się
	// jak zero, a przy rozkładaniu wierszy odnosi się do wysokości wynikłej z samych wierszy
	// (CSS Box Alignment 3 § 8.3), więc wiersze wychodzą poza kontener.
	percentRowGap := false
	var rowGap, columnGap pr.Float
	// gopdfgo: odstęp procentowy odnosi się do rozmiaru treści kontenera
	// w danej osi, a przy rozmiarze nieokreślonym wynosi zero (CSS Box
	// Alignment 3 § 8.3); źródło brało liczbę procentów jako piksele.
	// Szerokość bloku nie jest tu jeszcze wyznaczona, więc liczymy ją na kopii.
	if v := style.GetColumnGap(); v.Tag != pr.Normal {
		columnGap = v.Value
		if v.Unit == pr.Perc {
			columnGap = 0
			probe := box_.Copy()
			if measure != nil {
				probe.Box().Width = pr.AutoF // do rozmiaru wewnętrznego procent liczy się jak zero
			} else if _, known := box.Width.(pr.Float); !known {
				if bo.GridT.IsInstance(box_) {
					blockLevelWidth(probe, context, containingBlock)
				} else {
					inlineBlockWidth(probe, context, containingBlock)
				}
			}
			// calc() z procentem: część w pikselach liczy się także przy nieokreślonej podstawie
			width, _ := probe.Box().Width.(pr.Float)
			columnGap = pr.ResolvePercentage(v, width).V()
		}
	}
	if v := style.GetRowGap(); v.Tag != pr.Normal {
		rowGap = v.Value
		if v.Unit == pr.Perc {
			rowGap = 0
			height, known := box.Height.(pr.Float)
			rowGap = pr.ResolvePercentage(v, height).V()
			percentRowGap = !known
		}
	}

	if gridAreas.IsNone() {
		gridAreas = pr.GridTemplateAreas{{""}}
	}

	rows := getTemplateTracks(style.GetGridTemplateRows(), box.Height, rowGap)
	columns := getTemplateTracks(style.GetGridTemplateColumns(), box.Width, columnGap)
	rowsFitFirst, rowsFitCount := autoFitRange(style.GetGridTemplateRows(), box.Height, rowGap)
	columnsFitFirst, columnsFitCount := autoFitRange(style.GetGridTemplateColumns(), box.Width, columnGap)

	// Adjust rows number
	gridAreasColumns := 0
	if len(gridAreas) != 0 {
		gridAreasColumns = len(gridAreas[0])
	}
	rowsDiff := (len(rows)-1)/2 - len(gridAreas)
	if rowsDiff > 0 {
		for c := 0; c < rowsDiff; c++ {
			gridAreas = append(gridAreas, make([]string, gridAreasColumns))
		}
	} else if rowsDiff < 0 {
		for c := 0; c < -rowsDiff; c++ {
			rows = append(rows, autoRows.Next(), pr.GridNames{})
		}
	}

	// Adjust columns number
	columnsDiff := (len(columns)-1)/2 - gridAreasColumns
	if columnsDiff > 0 {
		for i := range gridAreas {
			gridAreas[i] = append(gridAreas[i], make([]string, columnsDiff)...)
		}
	} else if columnsDiff < 0 {
		for c := 0; c < -columnsDiff; c++ {
			columns = append(columns, autoColumns.Next(), pr.GridNames{})
		}
	}

	// Add implicit line names
	// gopdfgo: każdy obszar nazwany daje liniom na swoich krawędziach nazwy <obszar>-start i <obszar>-end
	// (CSS Grid 1 § 7.3.2), także wtedy, gdy taka nazwa jest już nadana jawnie innej linii. Źródło w takim
	// razie nazwy nie dodawało, a linie końcowe liczyło od końca szablonu, więc przy szablonie dłuższym
	// niż grid-template-areas trafiały na złe linie.
	addName := func(tracks []pr.GridSpec, index int, name string) {
		if index < 0 || index >= len(tracks) {
			return
		}
		if names, ok := tracks[index].(pr.GridNames); ok && !utils.IsIn(names, name) {
			tracks[index] = append(names, name)
		}
	}
	type areaBounds struct{ top, left, bottom, right int }
	bounds := map[string]*areaBounds{}
	var areaOrder []string
	for y, row := range gridAreas {
		for x, areaName := range row {
			if areaName == "" {
				continue
			}
			b := bounds[areaName]
			if b == nil {
				b = &areaBounds{y, x, y + 1, x + 1}
				bounds[areaName] = b
				areaOrder = append(areaOrder, areaName)
			}
			b.top, b.left = min(b.top, y), min(b.left, x)
			b.bottom, b.right = max(b.bottom, y+1), max(b.right, x+1)
		}
	}
	for _, areaName := range areaOrder {
		b := bounds[areaName]
		addName(rows, 2*b.top, areaName+"-start")
		addName(columns, 2*b.left, areaName+"-start")
		addName(rows, 2*b.bottom, areaName+"-end")
		addName(columns, 2*b.right, areaName+"-end")
	}

	explicitRowNames, explicitColumnNames := extractNames(rows), extractNames(columns) // dla elementów pozycjonowanych

	// gopdfgo: grid-auto-flow: column. Algorytm rozmieszczania poniżej jest napisany dla przepływu
	// wierszowego; dla kolumnowego zamieniamy osie przed nim i przywracamy po nim (CSS Grid 1 § 8.5
	// opisuje oba przepływy symetrycznie). W źródle wartość column była ignorowana z ostrzeżeniem.
	columnFlow := utils.IsIn(flow, "column")
	transposeAreas := func(areas pr.GridTemplateAreas) pr.GridTemplateAreas {
		if len(areas) == 0 {
			return areas
		}
		out := make(pr.GridTemplateAreas, len(areas[0]))
		for x := range out {
			out[x] = make([]string, len(areas))
			for y := range areas {
				out[x][y] = areas[y][x]
			}
		}
		return out
	}
	gridLineOf := func(child Box, column, start bool) pr.GridLine {
		style := child.Box().Style
		if columnFlow {
			column = !column
		}
		switch {
		case column && start:
			return style.GetGridColumnStart()
		case column:
			return style.GetGridColumnEnd()
		case start:
			return style.GetGridRowStart()
		default:
			return style.GetGridRowEnd()
		}
	}
	if columnFlow {
		rows, columns = columns, rows
		rowsFitFirst, columnsFitFirst = columnsFitFirst, rowsFitFirst
		rowsFitCount, columnsFitCount = columnsFitCount, rowsFitCount
		autoRows, autoColumns = autoColumns, autoRows
		autoRowsBack, autoColumnsBack = autoColumnsBack, autoRowsBack
		gridAreas = transposeAreas(gridAreas)
	}

	// 1. Run the grid placement algorithm.

	// 1.1 Position anything that’s not auto-positioned.
	childrenPositions := map[Box]rect{}
	for _, child := range box.Children {
		columnStart := gridLineOf(child, true, true)
		columnEnd := gridLineOf(child, true, false)
		rowStart := gridLineOf(child, false, true)
		rowEnd := gridLineOf(child, false, false)

		columnPlacement := getPlacement(columnStart, columnEnd, extractNames(columns))
		rowPlacement := getPlacement(rowStart, rowEnd, extractNames(rows))
		if columnPlacement.isNotNone() && rowPlacement.isNotNone() {
			x, width := columnPlacement.unpack()
			y, height := rowPlacement.unpack()
			childrenPositions[child] = rect{x, y, width, height}
		}
	}

	// 1.2 Process the items locked to a given row.
	children := make([]Box, len(box.Children))
	copy(children, box.Children)
	sort.Slice(children, func(i, j int) bool { return children[i].Box().Style.GetOrder() < children[j].Box().Style.GetOrder() })
	for _, child := range children {
		if _, has := childrenPositions[child]; has {
			continue
		}
		rowStart := gridLineOf(child, false, true)
		rowEnd := gridLineOf(child, false, false)
		rowPlacement := getPlacement(rowStart, rowEnd, extractNames(rows))
		if !rowPlacement.isNotNone() {
			continue
		}
		y, height := rowPlacement[0], rowPlacement[1]
		columnStart := gridLineOf(child, true, true)
		columnEnd := gridLineOf(child, true, false)
		x, width := getColumnPlacement(rowPlacement, columnStart, columnEnd, extractNames(columns),
			childrenPositions, utils.IsIn(flow, "dense")).unpack()
		childrenPositions[child] = [4]int{x, y, width, height}
	}

	// 1.3 Determine the columns in range the implicit grid.
	// 1.3.1 Start with the columns from the explicit grid.
	implicitX1 := 0
	implicitX2 := 0
	if len(gridAreas) != 0 {
		implicitX2 = len(gridAreas[0])
	}
	// 1.3.2 Add columns to the beginning and end of the implicit grid.
	var remainingGridItems []Box
	for _, child := range children {
		var x, width int
		if r, has := childrenPositions[child]; has {
			x, _, width, _ = r.unpack()
		} else {
			columnStart := gridLineOf(child, true, true)
			columnEnd := gridLineOf(child, true, false)
			columnPlacement := getPlacement(columnStart, columnEnd, extractNames(columns))
			remainingGridItems = append(remainingGridItems, child)
			if columnPlacement.isNotNone() {
				x, width = columnPlacement.unpack()
			} else {
				continue
			}
		}
		implicitX1 = min(x, implicitX1)
		implicitX2 = max(x+width, implicitX2)
	}
	// 1.3.3 Add columns to accommodate max column span.
	for _, child := range remainingGridItems {
		columnStart := gridLineOf(child, true, true)
		columnEnd := gridLineOf(child, true, false)
		span := 1
		if columnStart.IsSpan() {
			span = columnStart.Val
		} else if columnEnd.IsSpan() {
			span = columnEnd.Val
		}
		if span == 0 {
			span = 1
		}
		implicitX2 = max(implicitX1+span, implicitX2)
	}

	// 1.4 Position the remaining grid items.
	implicitY1 := 0
	implicitY2 := len(gridAreas)
	for _, position := range childrenPositions {
		_, y, _, height := position.unpack()
		implicitY1 = min(y, implicitY1)
		implicitY2 = max(y+height, implicitY2)
	}
	// gopdfgo: wiersze niejawne dopisuje wyłącznie growRows, tak żeby rows miało zawsze implicitY2
	// wierszy za jawnym początkiem. Źródło dopisywało je w kilku miejscach i myliło się o jeden przy
	// przejściu kursora do nowego wiersza, więc siatka o n ≥ 3 wierszach dostawała 3n − 4 ścieżki;
	// puste nie miały wysokości, ale każda dokładała row-gap.
	rowsEnd := len(gridAreas)
	growRows := func(end int) {
		for ; rowsEnd < end; rowsEnd++ {
			rows = append(rows, autoRows.Next(), pr.GridNames{})
		}
		implicitY2 = max(implicitY2, end)
	}
	growRows(implicitY2)
	cursorX, cursorY := implicitX1, implicitY1
	if utils.IsIn(flow, "dense") {
		for _, child := range remainingGridItems {
			columnStart := gridLineOf(child, true, true)
			columnEnd := gridLineOf(child, true, false)
			columnPlacement := getPlacement(columnStart, columnEnd, extractNames(columns))
			if columnPlacement.isNotNone() {
				// 1. Set the row position of the cursor.
				cursorY = implicitY1
				x, width := columnPlacement[0], columnPlacement[1]
				cursorX = x
				// 2. Increment the cursor’s row position.
				rowStart := gridLineOf(child, false, true)
				rowEnd := gridLineOf(child, false, false)
				var y, height int
				for y = cursorY; ; y++ {
					if rowStart.IsAuto() {
						y, height = getPlacement(pr.GridLine{Val: y + 1}, rowEnd, extractNames(rows)).unpack()
					} else {
						// assert rowStart[0] == "span"
						// assert rowStart.IsAuto() || rowStart[0] == "span"
						span := getSpan(rowStart)
						y, height = getPlacement(rowStart, pr.GridLine{Val: y + 1 + span}, extractNames(rows)).unpack()
					}
					if y < cursorY {
						continue
					}
					hasBroken := false
					for row := y; row < y+height; row++ {
						intersect := intersectWithChildren(x, y, width, height, childrenPositions)
						if intersect {
							// Child intersects with a positioned child on
							// current row.
							hasBroken = true
							break
						}
					}
					if !hasBroken {
						// Child doesn’t intersect with any positioned child on
						// any row.
						break
					}
				}
				growRows(y + height)
				// 3. Set the item’s row-start line.
				childrenPositions[child] = rect{x, y, width, height}
			} else {
				// 1. Set the cursor’s row && column positions.
				cursorX, cursorY = implicitX1, implicitY1
				for {
					// 2. Increment the column position of the cursor.
					y := cursorY
					rowStart := gridLineOf(child, false, true)
					rowEnd := gridLineOf(child, false, false)
					columnStart = gridLineOf(child, true, true)
					columnEnd = gridLineOf(child, true, false)
					hasBroken := false
					for x := cursorX; x < implicitX2; x++ {
						var width, height int
						if rowStart.IsAuto() {
							y, height = getPlacement(pr.GridLine{Val: y + 1}, rowEnd, extractNames(rows)).unpack()
						} else {
							span := getSpan(rowStart)
							y, height = getPlacement(rowStart, pr.GridLine{Val: y + 1 + span}, extractNames(rows)).unpack()
						}
						if columnStart.IsAuto() {
							x, width = getPlacement(pr.GridLine{Val: x + 1}, columnEnd, extractNames(columns)).unpack()
						} else {
							span := getSpan(columnStart)
							x, width = getPlacement(columnStart, pr.GridLine{Val: x + 1 + span}, extractNames(columns)).unpack()
						}
						intersect := intersectWithChildren(x, y, width, height, childrenPositions)
						if intersect {
							// Child intersects with a positioned child.
							continue
						} else {
							// Free place found.
							// 3. Set the item’s row-/column-start lines.
							childrenPositions[child] = [4]int{x, y, width, height}
							growRows(y + height)
							hasBroken = true
							break
						}
					}
					if !hasBroken {
						// No room found.
						// 2. Return to the previous step.
						cursorY += 1
						growRows(cursorY + 1)
						cursorX = implicitX1
						continue
					}
					break
				}
			}
		}
	} else {
		for _, child := range remainingGridItems {
			columnStart := gridLineOf(child, true, true)
			columnEnd := gridLineOf(child, true, false)
			columnPlacement := getPlacement(columnStart, columnEnd, extractNames(columns))
			if columnPlacement.isNotNone() {
				// 1. Set the column position of the cursor.
				x, width := columnPlacement.unpack()
				if x < cursorX {
					cursorY += 1
				}
				cursorX = x
				// 2. Increment the cursor’s row position.
				rowStart := gridLineOf(child, false, true)
				rowEnd := gridLineOf(child, false, false)
				var y, height int
				for ; ; cursorY++ {
					if rowStart.IsAuto() {
						y, height = getPlacement(pr.GridLine{Val: cursorY + 1}, rowEnd, extractNames(rows)).unpack()
					} else {
						// assert rowStart[0] == "span"
						// assert rowStart.IsAuto() || rowStart[0] == "span"
						span := getSpan(rowStart)
						y, height = getPlacement(rowStart, pr.GridLine{Val: cursorY + 1 + span}, extractNames(rows)).unpack()
					}
					if y < cursorY {
						continue
					}
					hasBroken := false
					for row := y; row < y+height; row++ {
						intersect := intersectWithChildren(x, y, width, height, childrenPositions)
						if intersect {
							// Child intersects with a positioned child on
							// current row.
							hasBroken = true
							break
						}
					}
					if !hasBroken {
						// Child doesn’t intersect with any positioned child on
						// any row.
						break
					}
				}
				growRows(y + height) // 3. Set the item’s row-start line.
				childrenPositions[child] = [4]int{x, y, width, height}
			} else {
				for {
					// 1. Increment the column position of the cursor.
					y := cursorY
					rowStart := gridLineOf(child, false, true)
					rowEnd := gridLineOf(child, false, false)
					columnStart = gridLineOf(child, true, true)
					columnEnd = gridLineOf(child, true, false)
					hasBroken := false
					for x := cursorX; x < implicitX2; x++ {
						var width, height int
						if rowStart.IsAuto() {
							y, height = getPlacement(pr.GridLine{Val: y + 1}, rowEnd, extractNames(rows)).unpack()
						} else {
							span := getSpan(rowStart)
							y, height = getPlacement(rowStart, pr.GridLine{Val: y + 1 + span},
								extractNames(rows)).unpack()
						}
						if columnStart.IsAuto() {
							x, width = getPlacement(pr.GridLine{Val: x + 1}, columnEnd, extractNames(columns)).unpack()
						} else {
							span := getSpan(columnStart)
							x, width = getPlacement(columnStart, pr.GridLine{Val: x + 1 + span},
								extractNames(columns)).unpack()
						}
						intersect := intersectWithChildren(x, y, width, height, childrenPositions)
						if intersect {
							// Child intersects with a positioned child.
							continue
						} else {
							// Free place found.
							// 2. Set the item’s row-/column-start lines.
							childrenPositions[child] = [4]int{x, y, width, height}
							growRows(y + height)
							hasBroken = true
							break
						}
					}
					if !hasBroken {
						// No room found.
						// 2. Return to the previous step.
						cursorY += 1
						growRows(cursorY + 1)
						cursorX = implicitX1
						continue
					}
					break
				}
			}
		}
	}

	if traceMode {
		traceLogger.Dump(fmt.Sprintf("gridLayout: childrenPositions=%v", childrenPositions))
	}

	for c := 0; c < -implicitX1; c++ {
		columns = append([]pr.GridSpec{pr.GridNames{}, autoColumnsBack.Next()}, columns...)
	}
	start := 0
	if len(gridAreas) != 0 {
		start = len(gridAreas[0])
	}
	for c := start; c < implicitX2; c++ {
		columns = append(columns, autoColumns.Next(), pr.GridNames{})
	}
	for c := 0; c < -implicitY1; c++ {
		rows = append([]pr.GridSpec{pr.GridNames{}, autoRowsBack.Next()}, rows...)
	}

	columns, collapsedColumns := collapseEmptyAutoFit(columns, columnsFitFirst, columnsFitCount, -implicitX1, childrenPositions, 0)
	rows, collapsedRows := collapseEmptyAutoFit(rows, rowsFitFirst, rowsFitCount, -implicitY1, childrenPositions, 1)

	if columnFlow {
		rows, columns = columns, rows
		gridAreas = transposeAreas(gridAreas)
		implicitX1, implicitY1 = implicitY1, implicitX1
		implicitX2, implicitY2 = implicitY2, implicitX2
		for child, r := range childrenPositions {
			childrenPositions[child] = rect{r[1], r[0], r[3], r[2]}
		}
	}

	// 2. Find the size of the grid container.

	if measure != nil {
		// szerokość zostaje ograniczeniem pomiaru
	} else if bo.GridT.IsInstance(box_) {
		blockLevelWidth(box_, context, containingBlock)
	} else {
		// assert isinstance(box, boxes.InlineGridBox)
		// from .inline import inlineBlockWidth
		inlineBlockWidth(box_, context, containingBlock)
	}
	if box.Width == pr.AutoF && measure == nil {
		// TODO: Calculate max-width.
		box.Width, _ = containingBlock.ContainingBlock()
	}

	// 3. Run the grid sizing algorithm.

	// 3.0 List min/max sizing functions.
	rowSizingFunctions := extractDims(rows)
	columnSizingFunctions := extractDims(columns)

	// gopdfgo: element w wierszach o stałym rozmiarze zna swoją wysokość już przy liczeniu kolumn, więc
	// wysokość procentowa i proporcje wchodzą do jego szerokości z treści (CSS Grid 2 § 12.1, CSS Sizing 4
	// § 5.1); źródło liczyło wtedy procenty jak auto i kolumna wychodziła za wąska albo za szeroka.
	for child, position := range childrenPositions {
		setDefiniteHeight(child, fixedRowsHeight(rowSizingFunctions, position[1]-implicitY1, position[3], rowGap, box.Height),
			stretchesInBlockAxis(child, box))
	}

	// 3.1 Resolve the sizes of the grid columns.
	columnsSizes := resolveTracksSizes(context, columnSizingFunctions, box.Width, childrenPositions, implicitX1,
		'x', columnGap, box_, nil)
	if measure != nil {
		measure.result = pr.Float(max(len(columnsSizes)-1, 0)) * columnGap
		for _, size := range columnsSizes {
			// max-content: ścieżka rośnie do granicy wzrostu (CSS Grid 1 § 11.6), o ile jest skończona
			if limit := size[1]; measure.constraint == pr.AutoF && limit > size[0] && limit < pr.Inf {
				measure.result += limit
			} else {
				measure.result += size[0]
			}
		}
		measure.done = true
		context.finishBlockFormattingContext(box_)
		return nil, blockLayout{}, nil
	}

	// 3.2 Resolve the sizes of the grid rows.
	rowsSizes := resolveTracksSizes(context, rowSizingFunctions, box.Height, childrenPositions, implicitY1,
		'y', rowGap, box_, columnsSizes)

	// gopdfgo: min-height kontenera o wysokości auto działa jak określona wysokość, gdy treść jest
	// niższa: wiersze są rozwiązywane ponownie w tej wysokości, więc ścieżki auto się rozciągają
	// (CSS Grid 1 § 11.1 i § 11.8). Źródło stosowało min-height dopiero po układzie elementów.
	if minHeight, ok := box.MinHeight.(pr.Float); ok && box.Height == pr.AutoF && minHeight > 0 && measure == nil && skipStack == nil {
		// tylko gdy jest ścieżka auto, która może się rozciągnąć; procenty w fit-content() zostają nieokreślone
		hasAuto := false
		for i, row := range rows {
			if dims, ok := row.(pr.GridDims); ok && i%2 == 1 {
				if _, fit := dims.IsFitcontent(); !fit && dims.SizingFunctions()[1].Tag == pr.Auto {
					hasAuto = true
				}
			}
		}
		if hasAuto && sum0(rowsSizes)+pr.Float(max(len(rowsSizes)-1, 0))*rowGap < minHeight {
			box.Height = minHeight
			rowsSizes = resolveTracksSizes(context, rowSizingFunctions, box.Height, childrenPositions, implicitY1,
				'y', rowGap, box_, columnsSizes)
		}
	}

	contentHeight := pr.Float(-1)
	// gopdfgo: gdy suma współczynników fr wierszy jest mniejsza od 1, wysokość kontenera wynika
	// z pierwszego przebiegu, a wiersze są rozwiązywane drugi raz w tej już określonej wysokości
	// (CSS Grid 1 § 11.1 krok 2 i § 11.7.1); źródło robiło jeden przebieg.
	if box.Height == pr.AutoF && measure == nil && skipStack == nil && rowExtra == nil {
		var factors pr.Float
		for _, function := range rowSizingFunctions {
			if isFr(function[1]) {
				factors += function[1].Value
			}
		}
		if factors > 0 && factors < 1 {
			contentHeight = sum0(rowsSizes) + pr.Float(max(len(rowsSizes)-1, 0))*rowGap
			rowsSizes = resolveTracksSizes(context, rowSizingFunctions, contentHeight, childrenPositions, implicitY1,
				'y', rowGap, box_, columnsSizes)
		}
	}
	if percentRowGap && skipStack == nil {
		contentHeight = sum0(rowsSizes) + pr.Float(max(len(rowsSizes)-1, 0))*rowGap
		rowGap = pr.ResolvePercentage(style.GetRowGap(), contentHeight).V()
	}
	for row, extra := range rowExtra {
		if row < len(rowsSizes) {
			rowsSizes[row][0] += extra
			rowsSizes[row][1] = max(rowsSizes[row][1], rowsSizes[row][0])
		}
	}

	// 3.3 Re-resolve the sizes of the grid columns with min-/max-content.
	// TODO: Re-resolve.

	// 3.4 Re-re-resolve the sizes of the grid columns with min-/max-content.
	// TODO: Re-re-resolve.

	// 3.5 Align the tracks within the grid container.
	// TODO: Support safe/unsafe.
	justifyContent := box.Style.GetJustifyContent()
	x := box.ContentBoxX()

	// gopdfgo: odstępy między kolumnami nie są wolnym miejscem do rozdania
	// (tak jak w osi wierszy niżej); źródło je pomijało, więc przy
	// justify-content innym niż start kolumny rozjeżdżały się o sumę odstępów
	freeWidth := max(0, box.Width.V()-sum0(columnsSizes)-pr.Float(max(len(columnsSizes)-1, 0))*columnGap)
	columnsNumber := pr.Float(len(columnsSizes))
	columnsPositions := make([]pr.Float, len(columnsSizes))
	if justifyContent.Has(pr.Center) {
		x += freeWidth / 2
		for i, size := range columnsSizes {
			columnsPositions[i] = x
			x += size[0] + columnGap
		}
	} else if justifyContent.Intersects(pr.Right, pr.End, pr.FlexEnd) {
		x += freeWidth
		for i, size := range columnsSizes {
			columnsPositions[i] = x
			x += size[0] + columnGap
		}
	} else if justifyContent.Has(pr.SpaceAround) {
		x += freeWidth / 2 / columnsNumber
		for i, size := range columnsSizes {
			columnsPositions[i] = x
			x += size[0] + freeWidth/columnsNumber + columnGap
		}
	} else if justifyContent.Has(pr.SpaceBetween) {
		for i, size := range columnsSizes {
			columnsPositions[i] = x
			if columnsNumber >= 2 {
				x += size[0] + freeWidth/(columnsNumber-1) + columnGap
			}
		}
	} else if justifyContent.Has(pr.SpaceEvenly) {
		x += freeWidth / (columnsNumber + 1)
		for i, size := range columnsSizes {
			columnsPositions[i] = x
			x += size[0] + freeWidth/(columnsNumber+1) + columnGap
		}
	} else {
		for i, size := range columnsSizes {
			columnsPositions[i] = x
			x += size[0] + columnGap
		}
	}

	alignContent := box.Style.GetAlignContent()
	y := box.ContentBoxY()
	freeHeight := pr.Float(0)
	if h := box.Height; h != pr.AutoF {
		freeHeight = h.V() - sum0(rowsSizes) - pr.Float(len(rowsSizes)-1)*rowGap
		freeHeight = max(0, freeHeight)
	}
	rowsNumber := pr.Float(len(rowsSizes))
	rowsPositions := make([]pr.Float, len(rowsSizes))
	if alignContent.Has(pr.Center) {
		y += freeHeight / 2
		for i, size := range rowsSizes {
			rowsPositions[i] = y
			y += size[0] + rowGap
		}
	} else if alignContent.Intersects(pr.Right, pr.End, pr.FlexEnd) {
		y += freeHeight
		for i, size := range rowsSizes {
			rowsPositions[i] = y
			y += size[0] + rowGap
		}
	} else if alignContent.Has(pr.SpaceAround) {
		y += freeHeight / 2 / rowsNumber
		for i, size := range rowsSizes {
			rowsPositions[i] = y
			y += size[0] + freeHeight/rowsNumber + rowGap
		}
	} else if alignContent.Has(pr.SpaceBetween) {
		for i, size := range rowsSizes {
			rowsPositions[i] = y
			if rowsNumber >= 2 {
				y += size[0] + freeHeight/(rowsNumber-1) + rowGap
			}
		}
	} else if alignContent.Has(pr.SpaceEvenly) {
		y += freeHeight / (rowsNumber + 1)
		for i, size := range rowsSizes {
			rowsPositions[i] = y
			y += size[0] + freeHeight/(rowsNumber+1) + rowGap
		}
	} else {
		if alignContent.Has(pr.Baseline) {
			// TODO: Support baseline value.
			logger.WarningLogger.Println("Baseline alignment is not supported for grid layout")
		}
		for i, size := range rowsSizes {
			rowsPositions[i] = y
			y += size[0] + rowGap
		}
	}

	// 4. Lay out the grid items into their respective containing blocks.
	// Find resumeAt row.
	var (
		thisPageChildren    []Box
		resumeRow           = -1
		skipRow, skipHeight = 0, pr.Float(0)
		resumeAt            tree.ResumeStack
	)
	// gopdfgo: podział elementów siatki między strony. Gdy element nie mieści się na stronie, stos wznowienia
	// niesie {wiersz: {indeks elementu: wznowienie, …}}; na następnej stronie układamy tylko dalszy ciąg
	// tych elementów, a kolejne wiersze zaczynają się od nowej strony. W źródle ("TODO: Support
	// fragmentation in grid items") wznowienie elementu było porzucane i jego dalsza część ginęła.
	var rowSkip tree.ResumeStack // dalszy ciąg elementów wiersza skipRow
	if skipStack != nil {
		skipRow, rowSkip = skipStack.Unpack()
		// gopdfgo: przed wierszem skipRow jest skipRow odstępów; źródło liczyło o jeden mniej, więc
		// dalszy ciąg siatki zaczynał stronę niżej o row-gap
		skipHeight = sum0(rowsSizes[:skipRow]) + pr.Float(skipRow)*rowGap
	}
	rowResume := tree.ResumeStack{} // elementy podzielone na tej stronie, według indeksu
	resumedRow := -1
	hasBroken := false
	for i := skipRow; i < len(rowsPositions); i++ {
		rowY := rowsPositions[i]
		// TODO: Check that page is not empty.
		if context.overflowsPage(bottomSpace, rowY-skipHeight) {
			if i == 0 {
				// gopdfgo: całą siatkę przenosi tylko układ blokowy; dla siatki pływającej albo w wierszu
				// wynik nil kończył się paniką w układzie elementów pływających
				if box.IsInNormalFlow() && bo.GridT.IsInstance(box_) {
					return nil, blockLayout{nil, nil, tree.PageBreak{Break: "any"}, false}, nil
				}
				continue
			}
			// gopdfgo: wiersz, od którego zaczynamy tę stronę, nie mieści się na pustej stronie. Zostawiamy
			// go tutaj (wystaje), bo wznowienie od niego samego dawało nieskończenie wiele stron
			// (TODO z oryginału: "Check that page is not empty").
			if i-1 == skipRow && pageIsEmpty {
				continue
			}
			resumeRow = i - 1
			resumeAt = tree.ResumeStack{i - 1: nil}
			for _, child := range children {
				_, y, _, _ := childrenPositions[child].unpack()
				if skipRow <= y && y <= i-2 {
					thisPageChildren = append(thisPageChildren, child)
				}
			}
			hasBroken = true
			break
		}
	}
	// gopdfgo: pętla wyżej poznaje, że wiersz się nie mieści, po położeniu następnego, więc ostatniego
	// wiersza nie sprawdzała wcale (działało to tylko przypadkiem, dzięki nadmiarowym wierszom niejawnym).
	// Ostatni wiersz, który się nie mieści, przechodzi na następną stronę w całości, tak jak wiersze
	// środkowe. Wyjątek: wiersz, od którego zaczyna się ten fragment siatki, z elementem o wysokości
	// auto, dzieli w treści układ elementu; gdy jego elementy mają podaną wysokość, też przechodzi
	// w całości, jak blok w ADR 0004 (w źródle zostawał i wystawał poza stronę).
	if last := len(rowsPositions) - 1; !hasBroken && rowSkip == nil && last >= 0 && bottomSpace > -pr.Inf &&
		context.flexMeasuring == 0 && !(last == skipRow && pageIsEmpty) &&
		context.overflowsPage(bottomSpace, rowsPositions[last]+rowsSizes[last][0]-skipHeight) {
		fixed, any := true, false
		for _, child := range children {
			_, y, _, height := childrenPositions[child].unpack()
			if y-implicitY1 <= last && last < y-implicitY1+height {
				any = true
				if child.Box().Style.GetHeight().Tag == pr.Auto {
					fixed = false
				}
			}
		}
		if any && (fixed || last > skipRow) {
			if last == 0 {
				// całą siatkę przenosi tylko układ blokowy; pływająca i pozycjonowana zostają na miejscu
				if box.IsInNormalFlow() && bo.GridT.IsInstance(box_) {
					return nil, blockLayout{nil, nil, tree.PageBreak{Break: "any"}, false}, nil
				}
			} else {
				resumeRow, resumeAt, hasBroken = last, tree.ResumeStack{last: nil}, true
			}
			for _, child := range children {
				_, y, _, _ := childrenPositions[child].unpack()
				if skipRow <= y && y < last {
					thisPageChildren = append(thisPageChildren, child)
				}
			}
		}
	}
	// gopdfgo: wymuszony podział strony między wierszami siatki: break-before na elemencie wiersza albo
	// break-after na elemencie poprzedniego (CSS Grid 2 § 11, CSS Break 3 § 3.1). Źródło ignorowało te
	// właściwości na elementach siatki.
	if rowSkip == nil && bottomSpace > -pr.Inf && context.flexMeasuring == 0 {
		forcedRow := -1
		for _, child := range children {
			_, y, _, height := childrenPositions[child].unpack()
			if y > skipRow && forcePageBreak(string(child.Box().Style.GetBreakBefore()), context) && (forcedRow == -1 || y < forcedRow) {
				forcedRow = y
			}
			if after := y + height; after > skipRow && after < len(rowsSizes) &&
				forcePageBreak(string(child.Box().Style.GetBreakAfter()), context) && (forcedRow == -1 || after < forcedRow) {
				forcedRow = after
			}
		}
		if forcedRow != -1 && (!hasBroken || forcedRow < resumeRow) {
			resumeRow, resumeAt, hasBroken = forcedRow, tree.ResumeStack{forcedRow: nil}, true
			thisPageChildren = thisPageChildren[:0]
			for _, child := range children {
				_, y, _, _ := childrenPositions[child].unpack()
				if skipRow <= y && y < forcedRow {
					thisPageChildren = append(thisPageChildren, child)
				}
			}
		}
	}
	if !hasBroken {
		for _, child := range children {
			_, y, _, _ := childrenPositions[child].unpack()
			if skipRow <= y {
				thisPageChildren = append(thisPageChildren, child)
			}
		}
	}
	if rowSkip != nil {
		// dalszy ciąg wiersza: tylko elementy, które zostały podzielone; reszta siatki od następnej strony
		continued := thisPageChildren[:0:0]
		for _, child := range children {
			_, y, _, _ := childrenPositions[child].unpack()
			if _, pending := rowSkip[findBoxIndex(box.Children, child)]; pending && y-implicitY1 == skipRow {
				continued = append(continued, child)
			}
		}
		thisPageChildren = continued
		resumeRow, resumeAt = -1, nil
		if skipRow+1 < len(rowsPositions) {
			resumeRow, resumeAt = skipRow+1, tree.ResumeStack{skipRow + 1: nil}
		}
	}
	if box.Height == pr.AutoF {
		slice := resumeRow
		if resumeRow == -1 {
			slice = len(rowsSizes)
		}
		rows := pySlice(rowsSizes, skipRow, slice)
		box.Height = sum0(rows) + pr.Float(max(len(rows)-1, 0))*rowGap
		if contentHeight >= 0 && resumeRow == -1 {
			box.Height = contentHeight
		}
		// gopdfgo: wysokość z wierszy jest ograniczana przez max-height i min-height kontenera
		// (CSS 2.1 § 10.7); źródło ich tu nie stosowało, więc siatka z wierszami wyższymi niż
		// max-height miała pełną wysokość wierszy. Tylko dla siatki niedzielonej między strony.
		if skipStack == nil && resumeRow == -1 {
			if maxHeight, ok := box.MaxHeight.(pr.Float); ok && maxHeight < box.Height.V() {
				box.Height = maxHeight
			}
			if minHeight, ok := box.MinHeight.(pr.Float); ok && minHeight > box.Height.V() {
				box.Height = minHeight
			}
		}
	}
	// Lay out grid items.
	justifyItems := box.Style.GetJustifyItems()
	alignItems := box.Style.GetAlignItems()
	type baselineItem struct {
		box Box
		row int
	}
	var (
		newChildren   []Box
		baseline      pr.MaybeFloat
		baselineItems []baselineItem
	)
	for _, child := range thisPageChildren {
		x, y, width, height := childrenPositions[child].unpack()
		index := findBoxIndex(box.Children, child)
		var childSkipStack tree.ResumeStack
		if rowSkip != nil && y-implicitY1 == skipRow {
			childSkipStack = rowSkip[index]
		}
		child = bo.Deepcopy(child)
		childB := child.Box()
		// gopdfgo: szerokość i wysokość rozciąganego elementu są niżej wpisywane do stylu; styl jest
		// współdzielony z pudełkiem źródłowym, więc bez kopii wartości z tej strony obowiązywałyby też na
		// następnych (dalszy ciąg elementu dostawał wysokość całego wiersza).
		childB.Style = childB.Style.Copy()
		// gopdfgo: tablice ścieżek zaczynają się od pierwszej ścieżki niejawnej, więc współrzędną siatki
		// (ujemną dla ścieżek przed siatką jawną) trzeba przesunąć; w źródle indeks ujemny kończył się
		// paniką, a w Pythonie po cichu brał ścieżkę od końca.
		xi := min(max(x-implicitX1, 0), len(columnsPositions)-1)
		yi := min(max(y-implicitY1, 0), len(rowsPositions)-1)
		childB.PositionX = columnsPositions[xi]
		childB.PositionY = rowsPositions[yi] - skipHeight
		cbW, cbH := box.ContainingBlock()
		resolveSizes(context, child, bo.MaybePoint{cbW, cbH})
		widthF := (sum0(pySlice(columnsSizes, xi, xi+width)) + pr.Float(width-1)*columnGap)
		heightF := (sum0(pySlice(rowsSizes, yi, yi+height)) + pr.Float(height-1)*rowGap)
		childWidth := widthF - (childB.MarginLeft.V() + childB.BorderLeftWidth + childB.PaddingLeft.V() +
			childB.MarginRight.V() + childB.BorderRightWidth + childB.PaddingRight.V())
		childHeight := heightF - (childB.MarginTop.V() + childB.BorderTopWidth + childB.PaddingTop.V() +
			childB.MarginBottom.V() + childB.BorderBottomWidth + childB.PaddingBottom.V())

		// rozmiar auto zapamiętany przed rozciąganiem, które niżej wpisuje do stylu rozmiar obszaru
		// gopdfgo: min-content, max-content i fit-content to nie auto: taki element nie jest rozciągany
		// (CSS Align 3 § 6.1); źródło rozróżniało tylko długość i resztę
		itemWidth, itemHeight := childB.Style.GetWidth(), childB.Style.GetHeight()
		widthAuto := itemWidth.Tag == pr.Auto && itemWidth.Intrinsic == pr.IntrinsicNone
		heightAuto := itemHeight.Tag == pr.Auto && itemHeight.Intrinsic == pr.IntrinsicNone
		justifySelf := childB.Style.GetJustifySelf()
		if justifySelf.Has(pr.Auto) {
			justifySelf = justifyItems
		}
		alignSelf := childB.Style.GetAlignSelf()
		if alignSelf.Has(pr.Auto) {
			alignSelf = alignItems
		}
		// gopdfgo: dla elementu zastępowanego (obraz, svg) wyrównanie normal działa jak start, nie jak
		// stretch (CSS Grid 1 § 6.2): zachowuje rozmiar wynikający z proporcji. Źródło rozciągało obraz
		// do całego obszaru siatki.
		replaced := bo.ReplacedT.IsInstance(child)
		stretchInline := justifySelf.Intersects(pr.Normal, pr.Stretch) && !(replaced && !justifySelf.Has(pr.Stretch))
		stretchBlock := alignSelf.Intersects(pr.Normal, pr.Stretch) && !(replaced && !alignSelf.Has(pr.Stretch))
		// gopdfgo: marginesy auto elementu siatki pochłaniają wolne miejsce w swojej osi przed
		// wyrównaniem i wyłączają rozciąganie (CSS Grid 1 § 6.4). W źródle: "TODO: Apply auto margins".
		marginLeftAuto := childB.Style.GetMarginLeft().Tag == pr.Auto
		marginRightAuto := childB.Style.GetMarginRight().Tag == pr.Auto
		marginTopAuto := childB.Style.GetMarginTop().Tag == pr.Auto
		marginBottomAuto := childB.Style.GetMarginBottom().Tag == pr.Auto
		inlineAuto, blockAuto := marginLeftAuto || marginRightAuto, marginTopAuto || marginBottomAuto
		if !inlineAuto && stretchInline {
			if widthAuto {
				childB.Style.SetWidth(pr.FToPx(childWidth))
			}
		}
		if !blockAuto && stretchBlock && childSkipStack == nil {
			if heightAuto {
				childB.Style.SetHeight(pr.FToPx(childHeight))
			}
		}

		// TODO: Find a better solution for the layout.
		parent := bo.BlockT.AnonymousFrom(box_, nil)
		cbW, cbH = containingBlock.ContainingBlock()
		resolvePercentages(parent, bo.MaybePoint{cbW, cbH})
		parent.Box().PositionX = childB.PositionX
		parent.Box().PositionY = childB.PositionY
		parent.Box().Width = widthF
		parent.Box().Height = heightF
		newChild, itemLayout, _ := blockLevelLayout(context, child.(bo.BlockLevelBoxITF), bottomSpace, childSkipStack, parent.Box(),
			pageIsEmpty, absoluteBoxes, fixedBoxes, nil, false, -1)
		if newChild != nil {
			if itemLayout.resumeAt != nil && bottomSpace > -pr.Inf && !clipsBlockOverflow(box) {
				if resumedRow == -1 || yi < resumedRow {
					resumedRow, rowResume = yi, tree.ResumeStack{}
				}
				if yi == resumedRow {
					rowResume[index] = itemLayout.resumeAt
				}
			}
		} else {
			// TODO: Support fragmentation in grid rows.
			continue
		}

		if inlineAuto {
			// Przy podanej szerokości marginesy auto w poziomie rozkłada już układ blokowy;
			// tutaj zostaje tylko element o szerokości auto, który kurczy się do treści.
			if widthAuto {
				newChild.Box().Width = fitContentItemWidth(context, newChild, childWidth)
				// marginesy auto pochłaniają tylko dodatnie wolne miejsce
				diff := max(childWidth-newChild.Box().Width.V(), 0)
				switch {
				case marginLeftAuto && marginRightAuto:
					newChild.Translate(diff/2, 0, false)
				case marginLeftAuto:
					newChild.Translate(diff, 0, false)
				}
			}
		} else if stretchInline {
			// gopdfgo: rozciągany jest tylko element o rozmiarze auto (CSS Grid 1 § 6.2, CSS Align 3 § 6.1);
			// źródło rozciągało też element z podaną szerokością albo wysokością do rozmiaru obszaru
			if widthAuto {
				newChild.Box().Width = max(childWidth, newChild.Box().Width.V())
			}
		} else {
			// gopdfgo: do treści kurczy się tylko element o szerokości auto; w źródle podana szerokość
			// była nadpisywana szerokością treści.
			if widthAuto && !replaced {
				newChild.Box().Width = fitContentItemWidth(context, newChild, childWidth)
			}
			diff := childWidth - newChild.Box().Width.V()
			if justifySelf.Has(pr.Center) {
				newChild.Translate(diff/2, 0, false)
			} else if justifySelf.Intersects(pr.Right, pr.End, pr.FlexEnd, pr.SelfEnd) {
				newChild.Translate(diff, 0, false)
			}
		}

		if blockAuto {
			diff := max(childHeight-newChild.Box().Height.V(), 0)
			switch {
			case marginTopAuto && marginBottomAuto:
				newChild.Translate(0, diff/2, false)
			case marginTopAuto:
				newChild.Translate(0, diff, false)
			}
		} else if itemLayout.resumeAt != nil || childSkipStack != nil {
			// fragment elementu podzielonego między strony: nie rozciągamy go do wysokości całego wiersza
			// i nie pozwalamy mu sięgać poniżej obszaru strony
			if over := newChild.Box().PositionY + newChild.Box().MarginHeight() - (context.pageBottom - bottomSpace); over > 0 &&
				bottomSpace > -pr.Inf && newChild.Box().Height != pr.AutoF {
				newChild.Box().Height = max(newChild.Box().Height.V()-over, 0)
			}
		} else if stretchBlock {
			if heightAuto {
				newChild.Box().Height = max(childHeight, newChild.Box().Height.V())
			}
		} else {
			diff := childHeight - newChild.Box().Height.V()
			if alignSelf.Has(pr.Center) {
				newChild.Translate(0, diff/2, false)
			} else if alignSelf.Intersects(pr.End, pr.FlexEnd, pr.SelfEnd) {
				newChild.Translate(0, diff, false)
			}
		}

		// TODO: Take care of page fragmentation.
		newChildren = append(newChildren, newChild)
		if alignSelf.Has(pr.Baseline) && !alignSelf.Has(pr.Last) && height == 1 && !blockAuto && childSkipStack == nil {
			baselineItems = append(baselineItems, baselineItem{newChild, yi})
		}
		if baseline == nil && y == implicitY1 {
			baseline = findInFlowBaseline(newChild, false)
			if baseline == nil {
				// gopdfgo: element bez wiersza tekstu ma linię bazową syntetyzowaną z dolnej krawędzi
				// obramowania (CSS Align 3 § 9.1); źródło przyjmowało wtedy górę kontenera, więc
				// inline-grid z pustymi elementami wisiał pod linią bazową wiersza.
				baseline = newChild.Box().BorderBoxY() + newChild.Box().BorderHeight()
			}
		}
	}

	// gopdfgo: wyrównanie do linii bazowej w wierszu siatki (CSS Grid 1 § 10.6, CSS Align 3 § 9.3): elementy
	// jednego wiersza z align-self: baseline są przesuwane tak, żeby ich pierwsze linie bazowe leżały na
	// wspólnej linii; element bez wiersza tekstu ma linię bazową na dolnej krawędzi marginesu. Rozmiary
	// ścieżek nie są przy tym powiększane. Źródło zgłaszało tylko ostrzeżenie o braku obsługi.
	itemBaseline := func(item Box) pr.Float {
		if baseline := findInFlowBaseline(item, false); baseline != nil {
			return baseline.V() - item.Box().PositionY
		}
		return item.Box().MarginHeight()
	}
	rowBaseline := map[int]pr.Float{}
	rowBelow := map[int]pr.Float{}
	for _, item := range baselineItems {
		above := itemBaseline(item.box)
		rowBaseline[item.row] = max(rowBaseline[item.row], above)
		rowBelow[item.row] = max(rowBelow[item.row], item.box.Box().MarginHeight()-above)
	}
	// o ile wiersz o rozmiarze wyznaczanym z treści jest za niski dla wyrównanych elementów
	var neededExtra map[int]pr.Float
	if rowExtra == nil && skipStack == nil && resumedRow == -1 {
		for row, above := range rowBaseline {
			if row >= len(rowsSizes) || row >= len(rowSizingFunctions) {
				continue
			}
			switch rowSizingFunctions[row][1].Tag {
			case pr.Auto, pr.MinContent, pr.MaxContent:
			default:
				continue
			}
			if extra := above + rowBelow[row] - rowsSizes[row][0]; extra > 0.01 {
				if neededExtra == nil {
					neededExtra = map[int]pr.Float{}
				}
				neededExtra[row] = extra
			}
		}
	}
	for _, item := range baselineItems {
		if shift := rowBaseline[item.row] - itemBaseline(item.box); shift > 0 {
			item.box.Translate(0, shift, false)
		}
	}

	if resumedRow != -1 {
		resumeAt = tree.ResumeStack{resumedRow: rowResume}
		// fragment kontenera sięga dolnej krawędzi strony
		box.Height = max(context.pageBottom-bottomSpace-box.ContentBoxY(), 0)
	} else if rowSkip != nil {
		// ostatni fragment wiersza: kontener kończy się za najniższym elementem
		bottom := box.ContentBoxY()
		for _, child := range newChildren {
			bottom = max(bottom, child.Box().PositionY+child.Box().MarginHeight())
		}
		box.Height = bottom - box.ContentBoxY()
	}

	// Zastępniki dzieci pozycjonowanych, z obszarem siatki jako pudełkiem zawierającym.
	for _, child_ := range append(positionedChildren[:len(positionedChildren):len(positionedChildren)], nil) {
		edges := func(start, end pr.GridLine, names []pr.GridNames, positions []pr.Float, sizes [][2]pr.Float,
			implicitBefore int, paddingStart, paddingSize pr.Float, collapsed []int,
		) (pr.Float, pr.Float, bool) {
			from, to, placed := paddingStart, paddingStart+paddingSize, false
			resolve := func(line pr.GridLine, side string) (int, bool) {
				if line.IsAuto() || line.Tag == pr.Span {
					return 0, false
				}
				_, _, _, coord := getLine(line, names, side)
				index := coord.i + implicitBefore
				// gopdfgo: linie zwiniętych ścieżek auto-fit zlewają się z sąsiednią (CSS Grid 2 § 7.2.3.2);
				// numer linii sprzed zwijania przeliczamy na pozycję wśród pozostałych ścieżek
				shift := 0
				for _, track := range collapsed {
					if track < index {
						shift++
					}
				}
				index -= shift
				if !coord.valid || index < 0 || index > len(positions) || len(positions) == 0 {
					return 0, false
				}
				return index, true
			}
			if index, ok := resolve(start, "start"); ok {
				placed = true
				if index < len(positions) {
					from = positions[index]
				} else {
					from = positions[index-1] + sizes[index-1][0]
				}
			}
			if index, ok := resolve(end, "end"); ok {
				placed = true
				if index > 0 {
					to = positions[index-1] + sizes[index-1][0]
				} else {
					to = positions[0]
				}
			}
			return from, max(to-from, 0), placed
		}
		if child_ == nil {
			// gopdfgo: dalsi potomkowie, dla których kontener siatki jest pudełkiem zawierającym, też
			// wskazują obszar właściwościami grid-column i grid-row (CSS Grid 1 § 9: reguła dotyczy
			// każdego pudełka pozycjonowanego, którego pudełko zawierające tworzy kontener siatki);
			// ich pozycja statyczna zostaje tam, gdzie wypadła w układzie
			if ownsPositioned && skipStack == nil {
				for _, placeholder := range *absoluteBoxes {
					descendant := placeholder.Box()
					if placeholder.layoutDone || isInBoxes(placeholder.AliasBox, positionedChildren) {
						continue
					}
					style := descendant.Style
					x, width, placedX := edges(style.GetGridColumnStart(), style.GetGridColumnEnd(), explicitColumnNames,
						columnsPositions, columnsSizes, -implicitX1, box.PaddingBoxX(), box.PaddingWidth(), collapsedColumns)
					y, height, placedY := edges(style.GetGridRowStart(), style.GetGridRowEnd(), explicitRowNames,
						rowsPositions, rowsSizes, -implicitY1, box.PaddingBoxY(), box.PaddingHeight(), collapsedRows)
					if placedX || placedY {
						descendant.GridArea = &[4]pr.Float{x, y, width, height}
					}
				}
			}
			break
		}
		child := child_.Box()
		style := child.Style
		x, width, placedX := edges(style.GetGridColumnStart(), style.GetGridColumnEnd(), explicitColumnNames,
			columnsPositions, columnsSizes, -implicitX1, box.PaddingBoxX(), box.PaddingWidth(), collapsedColumns)
		y, height, placedY := edges(style.GetGridRowStart(), style.GetGridRowEnd(), explicitRowNames,
			rowsPositions, rowsSizes, -implicitY1, box.PaddingBoxY(), box.PaddingHeight(), collapsedRows)
		child.PositionX, child.PositionY = box.ContentBoxX(), box.ContentBoxY()
		child.GridArea = nil
		// pozycja statyczna: jak jedyny element w obszarze o krawędziach treści kontenera albo wskazanych linii
		static := [6]pr.Float{box.ContentBoxX(), box.ContentBoxY(), box.Width.V(), box.Height.V(), 0, 0}
		if placedX || placedY {
			// gopdfgo: prostokąt pozycji statycznej to cały obszar siatki (CSS Box Alignment 3 § 4.2),
			// a w osi bez wskazanych linii obszar sięga krawędzi odstępu wewnętrznego kontenera
			static[0], static[1], static[2], static[3] = x, y, width, height
		}
		fraction := func(self, items pr.JustifyOrAlign) pr.Float {
			if self.Has(pr.Auto) || len(self) == 0 {
				self = items
			}
			switch {
			case self.Has(pr.Center):
				return 0.5
			case self.Intersects(pr.End, pr.FlexEnd, pr.SelfEnd, pr.Right):
				return 1
			}
			return 0
		}
		static[4] = fraction(style.GetJustifySelf(), box.Style.GetJustifyItems())
		static[5] = fraction(style.GetAlignSelf(), box.Style.GetAlignItems())
		child.StaticAlign = &static
		if ownsPositioned && skipStack == nil {
			if placedX || placedY {
				child.GridArea = &[4]pr.Float{x, y, width, height}
				// pozycja statyczna: początek obszaru
				child.PositionX, child.PositionY = x, y
			}
		}
		if skipStack != nil {
			continue // pozycjonowane dzieci należą do pierwszego fragmentu siatki
		}
		placeholder := NewAbsolutePlaceholder(child_)
		newChildren = append(newChildren, placeholder)
		if child.Style.GetPosition().String == "absolute" {
			*absoluteBoxes = append(*absoluteBoxes, placeholder)
		} else {
			*fixedBoxes = append(*fixedBoxes, placeholder)
		}
	}

	box_ = bo.CopyWithChildren(box_, newChildren)
	if ownsPositioned {
		for _, absoluteBox := range *absoluteBoxes {
			absoluteLayout(context, absoluteBox, box_, fixedBoxes, bottomSpace, nil)
		}
	}
	if bo.InlineGridT.IsInstance(box_) {
		// TODO: Synthetize a real baseline value.
		logger.WarningLogger.Println("Inline grids are not supported")
		if !pr.Is(baseline) {
			baseline = pr.Float(0)
		}
		box_.Box().Baseline = baseline
	}

	context.finishBlockFormattingContext(box_)

	// gopdfgo: element siatki z position: relative jest przesuwany o swoje top, right, bottom, left
	// (CSS 2.1 § 9.4.3); procenty liczą się od obszaru siatki elementu, tu w przybliżeniu od pudełka
	// kontenera. W źródle przesunięcie dostawały tylko dzieci bloków.
	if width, ok := box_.Box().Width.(pr.Float); ok && neededExtra == nil {
		height, _ := box_.Box().Height.(pr.Float)
		for _, child := range box_.Box().Children {
			if child.Box().IsGridItem {
				relativePositioning(child, bo.Point{width, height})
			}
		}
	}

	if traceMode {
		traceLogger.DumpTree(box_, "after gridLayout")
	}

	return box_, blockLayout{resumeAt, nil, tree.PageBreak{Break: "any"}, false}, neededExtra
}

// pySlice zwraca s[start:end] według reguł wycinków Pythona: indeks ujemny liczy się od końca,
// a zakres poza tablicą jest przycinany zamiast kończyć się paniką.
//
// gopdfgo: dodane; układ siatki przeniesiony z WeasyPrint polega na tych regułach przy liniach
// i ścieżkach niejawnych (indeksy ujemne i wychodzące poza siatkę jawną).
func pySlice[T any](s []T, start, end int) []T {
	n := len(s)
	if start < 0 {
		start = max(start+n, 0)
	}
	if end < 0 {
		end = max(end+n, 0)
	}
	start, end = min(start, n), min(end, n)
	if start >= end {
		return nil
	}
	return s[start:end]
}

// gridMeasure (gopdfgo) to zlecenie pomiaru szerokości wewnętrznej siatki: gridLayoutPass dla pudełka
// target liczy kolumny przy szerokości constraint i kończy przed układaniem elementów.
type gridMeasure struct {
	target     *bo.BoxFields
	constraint pr.MaybeFloat
	result     pr.Float
	done       bool
}

// gridContentWidth (gopdfgo) liczy szerokość min-content albo max-content kontenera siatki jako sumę
// kolumn i odstępów (CSS Grid 1 § 11.1 i § 5.2): max-content przy nieokreślonej szerokości dostępnej,
// min-content przy zerowej. Źródło liczyło siatkę jak blok, czyli brało najszerszy element, więc
// inline-grid, siatka pływająca albo w komórce tabeli miała szerokość jednej kolumny albo zero.
func gridContentWidth(context *layoutContext, box Box, outer, minimum bool) pr.Float {
	if width := box.Box().Style.GetWidth(); width.Tag != pr.Auto && width.Unit == pr.Px && width.Calc == nil {
		return adjust(box, outer, width.Value, true, true)
	}
	probe := box.Copy()
	request := &gridMeasure{target: probe.Box(), constraint: pr.AutoF}
	if minimum {
		request.constraint = pr.Float(0)
	}
	previous := context.gridMeasure
	context.gridMeasure = request
	var absoluteBoxes, fixedBoxes []*AbsolutePlaceholder
	measureGridPass(context, probe, -pr.Inf, nil, indefiniteBlock{}, false, &absoluteBoxes, &fixedBoxes, nil)
	context.gridMeasure = previous
	if !request.done {
		if minimum {
			return blockMinContentWidth(context, box, outer)
		}
		return blockMaxContentWidth(context, box, outer)
	}
	return adjust(box, outer, request.result, true, true)
}

// fitContentItemWidth (gopdfgo): szerokość treści elementu siatki, który nie jest rozciągany: szerokość
// max-content, ale nie więcej niż obszar siatki (o ile treść da się w nim zmieścić). Źródło wpisywało
// tu szerokość zewnętrzną, razem z marginesami, obramowaniem i odstępami, więc element z justify-self
// innym niż stretch był szerszy o ich sumę.
func fitContentItemWidth(context *layoutContext, item Box, available pr.Float) pr.Float {
	width := maxContentWidth(context, item, false)
	if width > available {
		width = max(available, minContentWidth(context, item, false))
	}
	return width
}

// indefiniteBlock to pudełko zawierające o nieokreślonych rozmiarach, używane przy pomiarze siatki.
type indefiniteBlock struct{}

func (indefiniteBlock) ContainingBlock() (width, height pr.MaybeFloat) { return pr.AutoF, pr.AutoF }

// measureGridPass wskazuje gridLayoutPass; przypisanie w init przerywa cykl inicjalizacji pakietu
// (absoluteWidth → shrinkToFit → minContentWidth → gridContentWidth → gridLayoutPass → absoluteLayout).
var measureGridPass func(context *layoutContext, box_ Box, bottomSpace pr.Float, skipStack tree.ResumeStack,
	containingBlock containingBlock, pageIsEmpty bool, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder,
	rowExtra map[int]pr.Float) (Box, blockLayout, map[int]pr.Float)

func init() { measureGridPass = gridLayoutPass }

// fixedRowsHeight podaje łączną wysokość wierszy first..first+span-1, jeśli wszystkie mają stały rozmiar
// (długość albo procent określonej wysokości kontenera); inaczej nil.
func fixedRowsHeight(sizingFunctions [][2]pr.TaggedDim, first, span int, gap pr.Float, containerHeight pr.MaybeFloat) pr.MaybeFloat {
	if first < 0 || span <= 0 || first+span > len(sizingFunctions) {
		return nil
	}
	total := pr.Float(span-1) * gap
	for _, functions := range sizingFunctions[first : first+span] {
		size := functions[1]
		if functions[0] != size || size.Tag != 0 || size.Calc != nil {
			return nil
		}
		switch size.Unit {
		case pr.Px:
			total += size.Value
		case pr.Perc:
			height, definite := containerHeight.(pr.Float)
			if !definite {
				return nil
			}
			total += size.Value * height / 100
		default:
			return nil
		}
	}
	return total
}

// stretchesInBlockAxis mówi, czy element siatki o wysokości auto wypełnia swój obszar w pionie
// (CSS Grid 1 § 6.2): `stretch`, a dla pudełek bez proporcji i rozmiaru własnego także `normal`.
func stretchesInBlockAxis(item Box, container *bo.BoxFields) bool {
	style := item.Box().Style
	if style.GetMarginTop().Tag == pr.Auto || style.GetMarginBottom().Tag == pr.Auto {
		return false
	}
	alignSelf := style.GetAlignSelf()
	if alignSelf.Has(pr.Auto) {
		alignSelf = container.Style.GetAlignItems()
	}
	if alignSelf.Has(pr.Stretch) {
		return true
	}
	return alignSelf.Has(pr.Normal) && !bo.ReplacedT.IsInstance(item) && style.GetAspectRatio() == 0
}

// setDefiniteHeight zapisuje na pudełku i jego potomkach wysokość treści wynikającą z wysokości bloku
// zawierającego (base), o ile da się ją ustalić bez układu.
func setDefiniteHeight(box_ Box, base pr.MaybeFloat, stretch bool) {
	box := box_.Box()
	box.DefiniteHeight = nil
	pixels := func(d pr.TaggedDim) pr.Float {
		if d.Tag == 0 && d.Calc == nil && d.Unit == pr.Px {
			return d.Value
		}
		return 0
	}
	var own pr.MaybeFloat
	height := box.Style.GetHeight()
	baseValue, known := base.(pr.Float)
	switch {
	case height.Tag == 0 && height.Calc == nil && height.Unit == pr.Px:
		own = height.Value
	case height.Tag == 0 && height.Calc == nil && height.Unit == pr.Perc && known:
		own = baseValue * height.Value / 100
		box.DefiniteHeight = own
	case height.Tag == pr.Auto && stretch && known:
		style := box.Style
		own = max(baseValue-pixels(style.GetMarginTop())-pixels(style.GetMarginBottom())-
			pixels(style.GetPaddingTop())-pixels(style.GetPaddingBottom())-
			style.GetBorderTopWidth().Value-style.GetBorderBottomWidth().Value, 0)
		box.DefiniteHeight = own
	}
	setChildrenDefiniteHeight(box.Children, own)
}

func setChildrenDefiniteHeight(children []Box, base pr.MaybeFloat) {
	for _, child := range children {
		switch child.(type) {
		case *bo.LineBox, *bo.InlineBox:
			setChildrenDefiniteHeight(child.Box().Children, base)
		case *bo.TextBox:
		default:
			if base == nil && child.Box().DefiniteHeight == nil {
				continue // głębiej wartość nie mogła powstać: łańcuch wysokości jest ciągły
			}
			setDefiniteHeight(child, base, false)
		}
	}
}
