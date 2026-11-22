package cffsubset

import (
	"bytes"
	"encoding/binary"
	"slices"

	ot "github.com/go-text/typesetting/font/opentype"
)

var (
	tagCFF  = ot.MustNewTag("CFF ")
	tagCFF2 = ot.MustNewTag("CFF2")
	tagGlyf = ot.MustNewTag("glyf")
	tagHead = ot.MustNewTag("head")
	tagHhea = ot.MustNewTag("hhea")
	tagHmtx = ot.MustNewTag("hmtx")
)

// Kind mówi, jakie kontury ma font OpenType
type Kind int

const (
	NotCFF Kind = iota // kontury TrueType albo brak konturów
	CFF                // tabela `CFF ` (CFF 1)
	CFF2               // tabela `CFF2` (font zmienny albo statyczny)
)

// FontKind rozpoznaje fonty o konturach CFF; font z tabelą glyf jest traktowany jak TrueType
func FontKind(font []byte) Kind {
	ld, err := ot.NewLoader(bytes.NewReader(font))
	if err != nil || ld.HasTable(tagGlyf) {
		return NotCFF
	}
	switch {
	case ld.HasTable(tagCFF):
		return CFF
	case ld.HasTable(tagCFF2):
		return CFF2
	}
	return NotCFF
}

// Subset zwraca podzbiór fontu OpenType o konturach CFF albo CFF2 jako goły font CFF w postaci CID-keyed
// (do /FontFile3 z /Subtype /CIDFontType0C). Podzbiór ma glif .notdef i glify gids (numery glifów fontu
// źródłowego), a CID każdego z nich jest równy jego numerowi w foncie źródłowym; name to nazwa fontu
// w Name INDEX (zwykle /BaseFont z prefiksem podzbioru). Wynik jest deterministyczny.
//
// Podprogramy: charstringi wybranych glifów są rozwijane w miejscu (bez podprogramów); gdy wynik
// z podprogramami ograniczonymi do użytych jest mniejszy, zostaje ten drugi. Konstrukcje, których
// interpreter nie rozwija (zob. errUnsupportedCharstring), zostawiają podprogramy w całości. Font CFF2 jest
// przepisywany na CFF 1 w domyślnej instancji (bez zmienności); wymaga to rozwinięcia, więc dla takiego
// fontu nieobsługiwana konstrukcja jest błędem.
func Subset(font []byte, gids []uint16, name string) ([]byte, error) {
	ld, err := ot.NewLoader(bytes.NewReader(font))
	if err != nil {
		return nil, err
	}
	var raw []byte
	cff2 := false
	if ld.HasTable(tagCFF) {
		raw, err = ld.RawTable(tagCFF)
	} else {
		cff2 = true
		raw, err = ld.RawTable(tagCFF2)
	}
	if err != nil {
		return nil, err
	}
	src, err := parseCFF(raw, cff2)
	if err != nil {
		return nil, err
	}

	numGlyphs := len(src.charStrings)
	keep := []int{0}
	seen := map[int]bool{0: true}
	for _, g := range gids {
		if int(g) < numGlyphs && !seen[int(g)] {
			seen[int(g)] = true
			keep = append(keep, int(g))
		}
	}
	slices.Sort(keep)

	var advances []int
	if cff2 {
		if advances, err = readAdvances(ld, numGlyphs); err != nil {
			return nil, err
		}
	}

	// rozwinięcie charstringów i zbiór użytych podprogramów
	usedGlobal := map[int]bool{}
	usedLocal := make([]map[int]bool, len(src.fds))
	for i := range usedLocal {
		usedLocal[i] = map[int]bool{}
	}
	flat := make([][]byte, len(keep))
	walkOK := true
	for i, g := range keep {
		fd := src.fdOf(g)
		w := walker{
			src: src, local: src.fds[fd].subrs, usedGlobal: usedGlobal, usedLocal: usedLocal[fd],
			flatten: true,
		}
		if cff2 {
			w.out = encodeInt(advances[g]) // nominalWidthX = 0
			w.pendingWidth = true
		}
		if err := w.run(src.charStrings[g], 0); err != nil {
			if cff2 || err != errUnsupportedCharstring {
				return nil, err
			}
			walkOK = false
			break
		}
		flat[i] = w.out
	}

	if !walkOK {
		return writeSubset(src, keep, keep, nil, nil, nil, name, ld), nil
	}
	flatOut := writeSubset(src, keep, keep, flat, nil, nil, name, ld)
	if cff2 {
		return flatOut, nil
	}
	prunedOut := writeSubset(src, keep, keep, nil, usedGlobal, usedLocal, name, ld)
	if len(prunedOut) < len(flatOut) {
		return prunedOut, nil
	}
	return flatOut, nil
}

// readAdvances czyta posunięcia glifów z hmtx (szerokości dla charstringów przepisanych z CFF2)
func readAdvances(ld *ot.Loader, numGlyphs int) ([]int, error) {
	hhea, err := ld.RawTable(tagHhea)
	if err != nil || len(hhea) < 36 {
		return nil, errorf("missing hhea")
	}
	hmtx, err := ld.RawTable(tagHmtx)
	if err != nil {
		return nil, errorf("missing hmtx")
	}
	nMetrics := int(binary.BigEndian.Uint16(hhea[34:]))
	if nMetrics == 0 || 4*nMetrics > len(hmtx) {
		return nil, errorf("invalid hmtx")
	}
	out := make([]int, numGlyphs)
	for g := range out {
		m := min(g, nMetrics-1)
		out[g] = int(binary.BigEndian.Uint16(hmtx[4*m:]))
	}
	return out, nil
}

// fontBBox zwraca xMin, yMin, xMax, yMax z tabeli head jako argumenty DICT albo nil
func fontBBox(ld *ot.Loader) [][]byte {
	head, err := ld.RawTable(tagHead)
	if err != nil || len(head) < 44 {
		return nil
	}
	out := make([][]byte, 4)
	for i := range out {
		out[i] = encodeInt(int(int16(binary.BigEndian.Uint16(head[36+2*i:]))))
	}
	return out
}

func unitsPerEm(ld *ot.Loader) int {
	head, err := ld.RawTable(tagHead)
	if err != nil || len(head) < 20 {
		return 1000
	}
	if upm := int(binary.BigEndian.Uint16(head[18:])); upm > 0 {
		return upm
	}
	return 1000
}

// writeSubset zapisuje font CID-keyed z glifami keep (numery źródła, rosnąco) i ich CID (podzbiór: cids = keep).
// flat != nil: charstringi bez podprogramów (indeksowane jak keep);
// w przeciwnym razie charstringi źródła i podprogramy: wszystkie (usedGlobal == nil) albo tylko użyte
// (pozostałe zastąpione samym return, więc numery się nie zmieniają)
func writeSubset(src *source, keep, cids []int, flat [][]byte, usedGlobal map[int]bool, usedLocal []map[int]bool,
	name string, ld *ot.Loader,
) []byte {
	// słowniki FD użyte przez zachowane glify, w kolejności źródła
	fdUsed := make([]bool, len(src.fds))
	for _, g := range keep {
		fdUsed[src.fdOf(g)] = true
	}
	fdMap := make([]int, len(src.fds))
	var fdOrder []int
	for i, u := range fdUsed {
		if u {
			fdMap[i] = len(fdOrder)
			fdOrder = append(fdOrder, i)
		}
	}

	// String INDEX: ROS, potem przepisywane napisy Top DICT
	strs := [][]byte{[]byte("Adobe"), []byte("Identity")}
	sid := func(old int) int {
		if old < stdStringCount {
			return old
		}
		if old-stdStringCount >= len(src.strings) {
			return -1
		}
		strs = append(strs, src.strings[old-stdStringCount])
		return stdStringCount + len(strs) - 1
	}

	// macierz fontu: w Top DICT, a słowniki FD fontu nie-CID dostają jednostkową, żeby iloczyn macierzy
	// (TN #5176 § 18, czytniki mnożą macierz Top DICT przez macierz FD) dał macierz źródła. Font CFF bez
	// FontMatrix w pliku OpenType ma jednostki z unitsPerEm tabeli head, nie domyślne 1/1000 (tak czyta go
	// FreeType i przeglądarki, a go-text liczy w tych jednostkach posunięcia); goły CFF w PDF nie ma tabeli
	// head, więc taka macierz musi trafić do Top DICT jawnie
	upmMatrix := func() [][]byte {
		upm := unitsPerEm(ld)
		if upm == 1000 {
			return nil
		}
		s := encodeReal(1 / float64(upm))
		return [][]byte{s, encodeInt(0), encodeInt(0), s, encodeInt(0), encodeInt(0)}
	}
	var topMatrix [][]byte
	identityFD := false
	switch {
	case src.cid:
		if e, ok := src.topEntry(opFontMatrix); ok {
			topMatrix = e.args
		} else if !slices.ContainsFunc(src.fds, func(fd fontDict) bool { return fd.matrix != nil }) {
			topMatrix = upmMatrix()
			identityFD = topMatrix != nil
		}
	default: // CFF nie-CID (macierz z Top DICT jest w fds[0].matrix) i CFF2
		m := src.fds[0].matrix
		if src.cff2 {
			m = nil
			if e, ok := src.topEntry(opFontMatrix); ok {
				m = e.args
			}
		}
		switch {
		case m == nil:
			topMatrix = upmMatrix()
		case !isDefaultFontMatrix(m):
			topMatrix = m
		}
		identityFD = topMatrix != nil
	}

	// FontBBox z tabeli head: w pliku OpenType prostokąt fontu podaje head, a FontBBox w CFF bywa
	// nieaktualny (w fontach testowych WPT [0 -1 0 0]); goły CFF w PDF nie ma head, a poppler wymiaruje
	// według FontBBox bufor glifów, przez co przy małych rozmiarach rysował inaczej niż z całym plikiem
	// OpenType. CFF2 FontBBox nie ma wcale.
	headBBox := fontBBox(ld)

	var topRest []byte
	if !src.cff2 {
		for _, e := range src.top {
			switch e.op {
			case opVersion, opNotice, opFullName, opFamilyName, opWeight, opCopyright:
				v, err := e.int(0)
				if err != nil || len(e.args) != 1 {
					continue
				}
				if s := sid(v); s >= 0 {
					topRest = appendEntry(topRest, e.op, encodeInt(s))
				}
			case opIsFixedPitch, opItalicAngle, opUnderlinePosition, opUnderlineThickness, opPaintType,
				opStrokeWidth, opCIDFontVersion, opCIDFontRevision:
				topRest = appendEntry(topRest, e.op, e.args...)
			case opFontBBox:
				if headBBox == nil {
					topRest = appendEntry(topRest, e.op, e.args...)
				}
			}
		}
	}
	if headBBox != nil {
		topRest = appendEntry(topRest, opFontBBox, headBBox...)
	}
	if topMatrix != nil {
		topRest = appendEntry(topRest, opFontMatrix, topMatrix...)
	}
	cidCount := len(src.charStrings)
	for _, c := range cids {
		cidCount = max(cidCount, c+1)
	}
	topRest = appendEntry(topRest, opCIDCount, encodeInt(cidCount))

	// charset: CID (numer glifu źródła) dla glifów 1..n-1, format 0 albo 2, krótszy
	cids = cids[1:]
	charset0 := []byte{0}
	for _, c := range cids {
		charset0 = binary.BigEndian.AppendUint16(charset0, uint16(c))
	}
	charset2 := []byte{2}
	for i := 0; i < len(cids); {
		j := i
		for j+1 < len(cids) && cids[j+1] == cids[j]+1 && j+1-i < 0xffff {
			j++
		}
		charset2 = binary.BigEndian.AppendUint16(charset2, uint16(cids[i]))
		charset2 = binary.BigEndian.AppendUint16(charset2, uint16(j-i))
		i = j + 1
	}
	charset := charset0
	if len(charset2) < len(charset0) {
		charset = charset2
	}

	// FDSelect: format 0 albo 3, krótszy
	fdSel0 := []byte{0}
	for _, g := range keep {
		fdSel0 = append(fdSel0, byte(fdMap[src.fdOf(g)]))
	}
	fdSel3 := []byte{3, 0, 0}
	nRanges := 0
	for i := range keep {
		fd := fdMap[src.fdOf(keep[i])]
		if i == 0 || fd != fdMap[src.fdOf(keep[i-1])] {
			fdSel3 = binary.BigEndian.AppendUint16(fdSel3, uint16(i))
			fdSel3 = append(fdSel3, byte(fd))
			nRanges++
		}
	}
	binary.BigEndian.PutUint16(fdSel3[1:], uint16(nRanges))
	fdSel3 = binary.BigEndian.AppendUint16(fdSel3, uint16(len(keep)))
	fdSelect := fdSel0
	if len(fdSel3) < len(fdSel0) {
		fdSelect = fdSel3
	}

	// charstringi i podprogramy
	charStrings := flat
	if charStrings == nil {
		charStrings = make([][]byte, len(keep))
		for i, g := range keep {
			charStrings[i] = src.charStrings[g]
		}
	}
	prune := func(subrs [][]byte, used map[int]bool) [][]byte {
		if used == nil {
			return subrs
		}
		out := make([][]byte, len(subrs))
		for i, s := range subrs {
			if used[i] {
				out[i] = s
			} else {
				out[i] = []byte{11} // return
			}
		}
		return out
	}
	var globalSubrs [][]byte
	if flat == nil {
		globalSubrs = prune(src.globalSubrs, usedGlobal)
	}

	// Private DICT i lokalne podprogramy każdego FD
	type fdOut struct {
		dict    []byte // Font DICT bez Private
		private []byte // Private DICT bez Subrs
		subrs   []byte // INDEX albo nil
	}
	fds := make([]fdOut, len(fdOrder))
	for i, old := range fdOrder {
		fd := src.fds[old]
		var o fdOut
		for _, e := range fd.entries {
			o.dict = appendEntry(o.dict, e.op, e.args...)
		}
		switch {
		case identityFD:
			o.dict = appendEntry(o.dict, opFontMatrix,
				encodeInt(1), encodeInt(0), encodeInt(0), encodeInt(1), encodeInt(0), encodeInt(0))
		case src.cid && fd.matrix != nil:
			o.dict = appendEntry(o.dict, opFontMatrix, fd.matrix...)
		}
		for _, e := range fd.private {
			o.private = appendEntry(o.private, e.op, e.args...)
		}
		if flat == nil && len(fd.subrs) != 0 {
			var used map[int]bool
			if usedLocal != nil {
				used = usedLocal[old]
			}
			o.subrs = writeIndex(prune(fd.subrs, used))
		}
		fds[i] = o
	}

	// układ: nagłówek, Name, Top DICT, String, Global Subrs, charset, FDSelect, CharStrings, FDArray,
	// Private DICT z lokalnymi podprogramami; przesunięcia w słownikach mają stałe 5 bajtów
	header := []byte{1, 0, 4, 4}
	nameIndex := writeIndex([][]byte{[]byte(name)})
	stringIndex := writeIndex(strs)
	globalIndex := writeIndex(globalSubrs)
	charStringsIndex := writeIndex(charStrings)

	buildTop := func(charsetOff, fdSelectOff, charStringsOff, fdArrayOff int) []byte {
		top := appendEntry(nil, opROS, encodeInt(stdStringCount), encodeInt(stdStringCount+1), encodeInt(0))
		top = append(top, topRest...)
		top = appendEntry(top, opCharset, encodeInt32(charsetOff))
		top = appendEntry(top, opFDSelect, encodeInt32(fdSelectOff))
		top = appendEntry(top, opCharStrings, encodeInt32(charStringsOff))
		top = appendEntry(top, opFDArray, encodeInt32(fdArrayOff))
		return top
	}
	topIndexLen := len(writeIndex([][]byte{buildTop(0, 0, 0, 0)}))
	charsetOff := len(header) + len(nameIndex) + topIndexLen + len(stringIndex) + len(globalIndex)
	fdSelectOff := charsetOff + len(charset)
	charStringsOff := fdSelectOff + len(fdSelect)
	fdArrayOff := charStringsOff + len(charStringsIndex)

	buildFDs := func(privOffs []int) [][]byte {
		out := make([][]byte, len(fds))
		for i, o := range fds {
			d := append([]byte(nil), o.dict...)
			out[i] = appendEntry(d, opPrivate, encodeInt32(len(o.private)+privSubrsLen(o.subrs)), encodeInt32(privOffs[i]))
		}
		return out
	}
	privOffs := make([]int, len(fds))
	pos := fdArrayOff + len(writeIndex(buildFDs(privOffs)))
	for i, o := range fds {
		privOffs[i] = pos
		pos += len(o.private) + privSubrsLen(o.subrs) + len(o.subrs)
	}

	out := make([]byte, 0, pos)
	out = append(out, header...)
	out = append(out, nameIndex...)
	out = append(out, writeIndex([][]byte{buildTop(charsetOff, fdSelectOff, charStringsOff, fdArrayOff)})...)
	out = append(out, stringIndex...)
	out = append(out, globalIndex...)
	out = append(out, charset...)
	out = append(out, fdSelect...)
	out = append(out, charStringsIndex...)
	out = append(out, writeIndex(buildFDs(privOffs))...)
	for _, o := range fds {
		out = append(out, o.private...)
		if o.subrs != nil {
			// Subrs: przesunięcie względem początku Private DICT, podprogramy tuż za nim
			out = appendEntry(out, opSubrs, encodeInt32(len(o.private)+privSubrsLen(o.subrs)))
			out = append(out, o.subrs...)
		}
	}
	return out
}

// privSubrsLen to długość wpisu Subrs w Private DICT (5 bajtów liczby i operator)
func privSubrsLen(subrs []byte) int {
	if subrs == nil {
		return 0
	}
	return 6
}
