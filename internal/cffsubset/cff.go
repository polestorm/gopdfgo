// Package cffsubset tworzy podzbiory fontów o konturach CFF (tabela `CFF ` albo `CFF2` fontu OpenType)
// do osadzenia w PDF jako /FontFile3 z /Subtype /CIDFontType0C (ISO 32000-1 § 9.9, tab. 126).
//
// Wynik jest zawsze fontem CFF w wersji 1 w postaci CID-keyed (Adobe Technical Note #5176, § 18), z ROS
// Adobe-Identity-0, a identyfikator CID każdego glifu to jego numer w foncie źródłowym. PDF z kodowaniem
// Identity-H adresuje więc glify tymi samymi liczbami co dla fontów TrueType (CIDFontType2 z
// /CIDToGIDMap /Identity), a przeliczenie CID na glif w podzbiorze robi czytnik przez charset fontu CFF
// (ISO 32000-1 § 9.7.4.2). Decyzja i uzasadnienie: docs/adr/0040-cff-subsets.md.
package cffsubset

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// operatory DICT (TN #5176, tab. 9, 10, 23); operatory dwubajtowe jako 1200 + drugi bajt
const (
	opVersion            = 0
	opNotice             = 1
	opFullName           = 2
	opFamilyName         = 3
	opWeight             = 4
	opCharset            = 15
	opEncoding           = 16
	opCharStrings        = 17
	opPrivate            = 18
	opSubrs              = 19
	opVsindex            = 22 // CFF2
	opBlend              = 23 // CFF2
	opVstore             = 24 // CFF2
	opCopyright          = 1200
	opIsFixedPitch       = 1201
	opItalicAngle        = 1202
	opUnderlinePosition  = 1203
	opUnderlineThickness = 1204
	opPaintType          = 1205
	opCharstringType     = 1206
	opFontMatrix         = 1207
	opStrokeWidth        = 1208
	opROS                = 1230
	opCIDFontVersion     = 1231
	opCIDFontRevision    = 1232
	opCIDCount           = 1234
	opFDArray            = 1236
	opFDSelect           = 1237
	opFontName           = 1238
	opFontBBox           = 5
)

// liczba standardowych napisów CFF (TN #5176, dodatek A); SID od tej wartości wskazuje String INDEX
const stdStringCount = 391

var errMalformed = errors.New("cff: malformed font")

// dictEntry to jeden operator DICT z surowymi bajtami argumentów (każdy argument osobno)
type dictEntry struct {
	op   int
	args [][]byte
}

func (e dictEntry) int(i int) (int, error) {
	if i >= len(e.args) {
		return 0, errMalformed
	}
	v, ok := decodeDictNumber(e.args[i])
	if !ok || v != math.Trunc(v) {
		return 0, errMalformed
	}
	return int(v), nil
}

// readIndex czyta INDEX zaczynający się w src[off:]; cff2 oznacza 32-bitowy licznik (CFF2 § 7).
// Zwraca elementy i pozycję za INDEX-em.
func readIndex(src []byte, off int, cff2 bool) ([][]byte, int, error) {
	countSize := 2
	if cff2 {
		countSize = 4
	}
	if off < 0 || off+countSize > len(src) {
		return nil, 0, errMalformed
	}
	var count int
	if cff2 {
		count = int(binary.BigEndian.Uint32(src[off:]))
	} else {
		count = int(binary.BigEndian.Uint16(src[off:]))
	}
	off += countSize
	if count == 0 {
		return nil, off, nil
	}
	if off >= len(src) {
		return nil, 0, errMalformed
	}
	offSize := int(src[off])
	off++
	if offSize < 1 || offSize > 4 || count > (len(src)-off)/offSize {
		return nil, 0, errMalformed
	}
	readOff := func(i int) int {
		v := 0
		for _, b := range src[off+i*offSize : off+(i+1)*offSize] {
			v = v<<8 | int(b)
		}
		return v
	}
	if off+(count+1)*offSize > len(src) {
		return nil, 0, errMalformed
	}
	base := off + (count+1)*offSize - 1 // przesunięcia liczone od 1
	out := make([][]byte, count)
	prev := readOff(0)
	for i := 0; i < count; i++ {
		next := readOff(i + 1)
		if prev < 1 || next < prev || base+next > len(src) {
			return nil, 0, errMalformed
		}
		out[i] = src[base+prev : base+next]
		prev = next
	}
	return out, base + prev, nil
}

// writeIndex zapisuje INDEX w formacie CFF 1 (16-bitowy licznik) z najmniejszym możliwym offSize
func writeIndex(items [][]byte) []byte {
	if len(items) == 0 {
		return []byte{0, 0}
	}
	total := 1
	for _, it := range items {
		total += len(it)
	}
	offSize := 1
	for total >= 1<<(8*offSize) {
		offSize++
	}
	out := make([]byte, 3, 3+(len(items)+1)*offSize+total)
	binary.BigEndian.PutUint16(out, uint16(len(items)))
	out[2] = byte(offSize)
	writeOff := func(v int) {
		for i := offSize - 1; i >= 0; i-- {
			out = append(out, byte(v>>(8*i)))
		}
	}
	pos := 1
	writeOff(pos)
	for _, it := range items {
		pos += len(it)
		writeOff(pos)
	}
	for _, it := range items {
		out = append(out, it...)
	}
	return out
}

// parseDict dzieli DICT na operatory z surowymi argumentami (TN #5176 § 4)
func parseDict(b []byte) ([]dictEntry, error) {
	var (
		out  []dictEntry
		args [][]byte
	)
	for i := 0; i < len(b); {
		b0 := b[i]
		switch {
		case b0 <= 21 || (b0 >= 22 && b0 <= 27):
			op := int(b0)
			n := 1
			if b0 == 12 {
				if i+1 >= len(b) {
					return nil, errMalformed
				}
				op = 1200 + int(b[i+1])
				n = 2
			}
			out = append(out, dictEntry{op: op, args: args})
			args = nil
			i += n
		default:
			n := dictNumberLength(b[i:])
			if n == 0 {
				return nil, errMalformed
			}
			args = append(args, b[i:i+n])
			i += n
		}
	}
	return out, nil
}

// dictNumberLength zwraca długość liczby DICT na początku b albo 0
func dictNumberLength(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	b0 := b[0]
	n := 0
	switch {
	case b0 == 28:
		n = 3
	case b0 == 29:
		n = 5
	case b0 == 30:
		for i := 1; i < len(b); i++ {
			if b[i]&0x0f == 0x0f || b[i]>>4 == 0x0f {
				return i + 1
			}
		}
		return 0
	case b0 >= 32 && b0 <= 246:
		n = 1
	case b0 >= 247 && b0 <= 254:
		n = 2
	default:
		return 0
	}
	if n > len(b) {
		return 0
	}
	return n
}

func decodeDictNumber(b []byte) (float64, bool) {
	if len(b) == 0 {
		return 0, false
	}
	b0 := b[0]
	switch {
	case b0 == 28 && len(b) >= 3:
		return float64(int16(binary.BigEndian.Uint16(b[1:]))), true
	case b0 == 29 && len(b) >= 5:
		return float64(int32(binary.BigEndian.Uint32(b[1:]))), true
	case b0 == 30:
		var s strings.Builder
		for _, c := range b[1:] {
			for _, nib := range [2]byte{c >> 4, c & 0x0f} {
				switch {
				case nib <= 9:
					s.WriteByte('0' + nib)
				case nib == 0xa:
					s.WriteByte('.')
				case nib == 0xb:
					s.WriteByte('E')
				case nib == 0xc:
					s.WriteString("E-")
				case nib == 0xe:
					s.WriteByte('-')
				case nib == 0xf:
					v, err := strconv.ParseFloat(s.String(), 64)
					return v, err == nil
				default:
					return 0, false
				}
			}
		}
		return 0, false
	case b0 >= 32 && b0 <= 246:
		return float64(int(b0) - 139), true
	case b0 >= 247 && b0 <= 250 && len(b) >= 2:
		return float64((int(b0)-247)*256 + int(b[1]) + 108), true
	case b0 >= 251 && b0 <= 254 && len(b) >= 2:
		return float64(-(int(b0)-251)*256 - int(b[1]) - 108), true
	}
	return 0, false
}

// encodeInt koduje liczbę całkowitą najkrócej (DICT i charstringi Type 2 kodują je tak samo,
// poza 5-bajtową postacią 29, której charstringi nie mają)
func encodeInt(v int) []byte {
	switch {
	case v >= -107 && v <= 107:
		return []byte{byte(v + 139)}
	case v >= 108 && v <= 1131:
		v -= 108
		return []byte{byte(v>>8 + 247), byte(v)}
	case v >= -1131 && v <= -108:
		v = -v - 108
		return []byte{byte(v>>8 + 251), byte(v)}
	case v >= -32768 && v <= 32767:
		return []byte{28, byte(v >> 8), byte(v)}
	}
	return encodeInt32(v)
}

// encodeInt32 koduje liczbę na stałych 5 bajtach (tylko DICT); przesunięcia mają przez to znaną długość
func encodeInt32(v int) []byte {
	return []byte{29, byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

// encodeReal koduje liczbę rzeczywistą DICT (operator 30, TN #5176 tab. 5)
func encodeReal(v float64) []byte {
	s := strings.ToUpper(strconv.FormatFloat(v, 'g', -1, 64))
	var nibs []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			nibs = append(nibs, c-'0')
		case c == '.':
			nibs = append(nibs, 0xa)
		case c == '-':
			nibs = append(nibs, 0xe)
		case c == 'E':
			if i+1 < len(s) && s[i+1] == '-' {
				nibs = append(nibs, 0xc)
				i++
			} else {
				if i+1 < len(s) && s[i+1] == '+' {
					i++
				}
				nibs = append(nibs, 0xb)
			}
		}
	}
	nibs = append(nibs, 0xf)
	if len(nibs)%2 == 1 {
		nibs = append(nibs, 0xf)
	}
	out := []byte{30}
	for i := 0; i < len(nibs); i += 2 {
		out = append(out, nibs[i]<<4|nibs[i+1])
	}
	return out
}

func encodeOp(op int) []byte {
	if op >= 1200 {
		return []byte{12, byte(op - 1200)}
	}
	return []byte{byte(op)}
}

// appendEntry dopisuje operator z argumentami do DICT
func appendEntry(dst []byte, op int, args ...[]byte) []byte {
	for _, a := range args {
		dst = append(dst, a...)
	}
	return append(dst, encodeOp(op)...)
}

func subrBias(count int) int {
	switch {
	case count < 1240:
		return 107
	case count < 33900:
		return 1131
	}
	return 32768
}

func isDefaultFontMatrix(args [][]byte) bool {
	if len(args) != 6 {
		return false
	}
	want := [6]float64{0.001, 0, 0, 0.001, 0, 0}
	for i, a := range args {
		v, ok := decodeDictNumber(a)
		if !ok || math.Abs(v-want[i]) > 1e-12 {
			return false
		}
	}
	return true
}

func errorf(format string, args ...any) error {
	return fmt.Errorf("cff: "+format, args...)
}
