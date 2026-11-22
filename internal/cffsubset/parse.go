package cffsubset

import (
	"encoding/binary"
)

// fontDict to jeden słownik FD (dla fontu nie-CID: jedyny, zbudowany z Top DICT)
type fontDict struct {
	entries []dictEntry // operatory słownika Font DICT bez Private i FontName
	private []dictEntry // operatory Private DICT bez Subrs (dla CFF2 z rozwiązanym blend)
	subrs   [][]byte    // lokalne podprogramy
	matrix  [][]byte    // FontMatrix słownika FD (font CID-keyed), może być puste
}

// source to font CFF albo CFF2 rozłożony na części potrzebne do zapisu podzbioru
type source struct {
	cff2        bool
	cid         bool        // CFF 1 z ROS
	top         []dictEntry // Top DICT
	strings     [][]byte    // String INDEX (CFF 1)
	globalSubrs [][]byte
	charStrings [][]byte
	fds         []fontDict
	fdSelect    []uint8 // numer FD dla każdego glifu; nil, gdy jeden FD
	vsRegions   []int   // CFF2: liczba regionów dla każdego vsindex (ItemVariationData)
}

func (s *source) fdOf(gid int) int {
	if s.fdSelect == nil {
		return 0
	}
	return int(s.fdSelect[gid])
}

func (s *source) topEntry(op int) (dictEntry, bool) {
	for _, e := range s.top {
		if e.op == op {
			return e, true
		}
	}
	return dictEntry{}, false
}

// parseCFF czyta tabelę `CFF ` (wersja 1) albo `CFF2`
func parseCFF(src []byte, cff2 bool) (*source, error) {
	if len(src) < 4 {
		return nil, errMalformed
	}
	s := &source{cff2: cff2}
	hdrSize := int(src[2])
	if cff2 {
		if src[0] != 2 || len(src) < 5 {
			return nil, errorf("unsupported CFF2 version %d", src[0])
		}
		topSize := int(binary.BigEndian.Uint16(src[3:]))
		if hdrSize+topSize > len(src) {
			return nil, errMalformed
		}
		top, err := parseDict(src[hdrSize : hdrSize+topSize])
		if err != nil {
			return nil, err
		}
		s.top = top
		s.globalSubrs, _, err = readIndex(src, hdrSize+topSize, true)
		if err != nil {
			return nil, err
		}
	} else {
		if src[0] != 1 {
			return nil, errorf("unsupported CFF version %d", src[0])
		}
		_, off, err := readIndex(src, hdrSize, false) // Name INDEX
		if err != nil {
			return nil, err
		}
		tops, off, err := readIndex(src, off, false)
		if err != nil {
			return nil, err
		}
		if len(tops) != 1 {
			return nil, errorf("expected one font, got %d", len(tops))
		}
		if s.top, err = parseDict(tops[0]); err != nil {
			return nil, err
		}
		if s.strings, off, err = readIndex(src, off, false); err != nil {
			return nil, err
		}
		if s.globalSubrs, _, err = readIndex(src, off, false); err != nil {
			return nil, err
		}
		if e, ok := s.topEntry(opCharstringType); ok {
			if v, err := e.int(0); err != nil || v != 2 {
				return nil, errorf("unsupported charstring type")
			}
		}
		_, s.cid = s.topEntry(opROS)
	}

	e, ok := s.topEntry(opCharStrings)
	if !ok {
		return nil, errorf("missing CharStrings")
	}
	off, err := e.int(0)
	if err != nil {
		return nil, err
	}
	if s.charStrings, _, err = readIndex(src, off, cff2); err != nil {
		return nil, err
	}
	if len(s.charStrings) == 0 || len(s.charStrings) > 65535 {
		return nil, errorf("invalid glyph count %d", len(s.charStrings))
	}

	if cff2 {
		if e, ok := s.topEntry(opVstore); ok {
			off, err := e.int(0)
			if err != nil {
				return nil, err
			}
			if s.vsRegions, err = parseVariationStore(src, off); err != nil {
				return nil, err
			}
		}
	}

	if cff2 || s.cid {
		e, ok := s.topEntry(opFDArray)
		if !ok {
			return nil, errorf("missing FDArray")
		}
		off, err := e.int(0)
		if err != nil {
			return nil, err
		}
		fdDicts, _, err := readIndex(src, off, cff2)
		if err != nil {
			return nil, err
		}
		if len(fdDicts) == 0 || len(fdDicts) > 256 {
			return nil, errorf("invalid FDArray")
		}
		for _, raw := range fdDicts {
			entries, err := parseDict(raw)
			if err != nil {
				return nil, err
			}
			fd, err := s.readFontDict(src, entries)
			if err != nil {
				return nil, err
			}
			s.fds = append(s.fds, fd)
		}
		if e, ok := s.topEntry(opFDSelect); ok {
			off, err := e.int(0)
			if err != nil {
				return nil, err
			}
			if s.fdSelect, err = parseFDSelect(src, off, len(s.charStrings), len(s.fds)); err != nil {
				return nil, err
			}
		} else if len(s.fds) != 1 {
			return nil, errorf("missing FDSelect")
		}
	} else {
		fd, err := s.readFontDict(src, s.top)
		if err != nil {
			return nil, err
		}
		fd.entries = nil
		s.fds = []fontDict{fd}
	}
	return s, nil
}

// readFontDict czyta Private DICT i lokalne podprogramy wskazane przez słownik (Top DICT albo FD)
func (s *source) readFontDict(src []byte, entries []dictEntry) (fontDict, error) {
	var fd fontDict
	for _, e := range entries {
		switch e.op {
		case opPrivate:
			size, err := e.int(0)
			if err != nil {
				return fd, err
			}
			off, err := e.int(1)
			if err != nil {
				return fd, err
			}
			if off < 0 || size < 0 || off+size > len(src) {
				return fd, errMalformed
			}
			priv, err := parseDict(src[off : off+size])
			if err != nil {
				return fd, err
			}
			if s.cff2 {
				if priv, err = s.resolveBlend(priv); err != nil {
					return fd, err
				}
			}
			for _, pe := range priv {
				if pe.op == opSubrs {
					rel, err := pe.int(0)
					if err != nil {
						return fd, err
					}
					if fd.subrs, _, err = readIndex(src, off+rel, s.cff2); err != nil {
						return fd, err
					}
					continue
				}
				fd.private = append(fd.private, pe)
			}
		case opFontName:
		case opFontMatrix:
			fd.matrix = e.args
		default:
			fd.entries = append(fd.entries, e)
		}
	}
	return fd, nil
}

// resolveBlend zostawia w Private DICT fontu CFF2 wartości domyślnej instancji: blend z n*(k+1)
// argumentów i licznika n daje n pierwszych, vsindex znika (CFF2 § 9, § 13)
func (s *source) resolveBlend(priv []dictEntry) ([]dictEntry, error) {
	vsindex := 0
	var out []dictEntry
	var pending [][]byte // argumenty przed operatorem, z wynikami blend
	for _, e := range priv {
		args := append(pending, e.args...)
		pending = nil
		switch e.op {
		case opVsindex:
			v, err := (dictEntry{args: args}).int(0)
			if err != nil {
				return nil, err
			}
			vsindex = v
		case opBlend:
			if len(args) == 0 {
				return nil, errMalformed
			}
			n, err := (dictEntry{args: args}).int(len(args) - 1)
			if err != nil {
				return nil, err
			}
			if vsindex >= len(s.vsRegions) {
				return nil, errMalformed
			}
			k := s.vsRegions[vsindex]
			start := len(args) - 1 - n*(k+1)
			if n < 0 || start < 0 {
				return nil, errMalformed
			}
			pending = append(append([][]byte(nil), args[:start]...), args[start:start+n]...)
		default:
			out = append(out, dictEntry{op: e.op, args: args})
		}
	}
	return out, nil
}

func parseFDSelect(src []byte, off, numGlyphs, numFDs int) ([]uint8, error) {
	if off < 0 || off >= len(src) {
		return nil, errMalformed
	}
	out := make([]uint8, numGlyphs)
	switch src[off] {
	case 0:
		if off+1+numGlyphs > len(src) {
			return nil, errMalformed
		}
		copy(out, src[off+1:])
	case 3, 4:
		format4 := src[off] == 4
		p := off + 1
		var nRanges int
		if format4 {
			if p+4 > len(src) {
				return nil, errMalformed
			}
			nRanges = int(binary.BigEndian.Uint32(src[p:]))
			p += 4
		} else {
			if p+2 > len(src) {
				return nil, errMalformed
			}
			nRanges = int(binary.BigEndian.Uint16(src[p:]))
			p += 2
		}
		rangeSize := 3
		if format4 {
			rangeSize = 6
		}
		if nRanges > (len(src)-p)/rangeSize {
			return nil, errMalformed
		}
		readRange := func(i int) (first, fd int) {
			r := src[p+i*rangeSize:]
			if format4 {
				return int(binary.BigEndian.Uint32(r)), int(binary.BigEndian.Uint16(r[4:]))
			}
			return int(binary.BigEndian.Uint16(r)), int(r[2])
		}
		sentinelAt := p + nRanges*rangeSize
		var sentinel int
		if format4 {
			if sentinelAt+4 > len(src) {
				return nil, errMalformed
			}
			sentinel = int(binary.BigEndian.Uint32(src[sentinelAt:]))
		} else {
			if sentinelAt+2 > len(src) {
				return nil, errMalformed
			}
			sentinel = int(binary.BigEndian.Uint16(src[sentinelAt:]))
		}
		for i := 0; i < nRanges; i++ {
			first, fd := readRange(i)
			end := sentinel
			if i+1 < nRanges {
				end, _ = readRange(i + 1)
			}
			if first > end || end > numGlyphs || fd > 255 {
				return nil, errMalformed
			}
			for g := first; g < end; g++ {
				out[g] = uint8(fd)
			}
		}
	default:
		return nil, errorf("unsupported FDSelect format %d", src[off])
	}
	for _, fd := range out {
		if int(fd) >= numFDs {
			return nil, errMalformed
		}
	}
	return out, nil
}

// parseVariationStore zwraca liczbę regionów każdego ItemVariationData (CFF2 § 11, OpenType
// „Item Variation Store”); od niej zależy liczba argumentów operatora blend
func parseVariationStore(src []byte, off int) ([]int, error) {
	// uint16 długość, potem ItemVariationStore
	base := off + 2
	if off < 0 || base+8 > len(src) {
		return nil, errMalformed
	}
	count := int(binary.BigEndian.Uint16(src[base+6:]))
	if base+8+4*count > len(src) {
		return nil, errMalformed
	}
	out := make([]int, count)
	for i := range out {
		o := base + int(binary.BigEndian.Uint32(src[base+8+4*i:]))
		if o+6 > len(src) {
			return nil, errMalformed
		}
		out[i] = int(binary.BigEndian.Uint16(src[o+4:]))
	}
	return out, nil
}
