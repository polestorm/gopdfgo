package cffsubset

import (
	"encoding/binary"
	"errors"
)

// Interpretacja charstringów Type 2 (Adobe Technical Note #5177) i CFF2 (OpenType 1.9, „CFF2 Charstring
// Format”) w zakresie potrzebnym do podzbioru: rozwinięcie wywołań podprogramów w miejscu (callsubr,
// callgsubr), zapamiętanie użytych podprogramów, a dla CFF2 usunięcie blend i vsindex (zostają wartości
// domyślnej instancji) i dopisanie szerokości oraz endchar, których CFF2 nie ma. Kontury nie są liczone:
// bajty liczb i operatorów są przepisywane bez zmian.

// errUnsupportedCharstring oznacza konstrukcję, której interpreter nie rozwija (operatory arytmetyczne
// i przechowywania, seac, numer podprogramu wyliczony zamiast podanego wprost); podzbiór zostaje wtedy przy
// podprogramach w całości i charstringach bez zmian
var errUnsupportedCharstring = errors.New("cff: unsupported charstring construct")

// maksymalna głębokość wywołań podprogramów (TN #5177, dodatek B)
const maxSubrDepth = 10

// maxType2Stack to granica stosu argumentów Type 2 (TN #5177, dodatek B); CFF2 dopuszcza 513
const maxType2Stack = 48

type walker struct {
	src   *source
	local [][]byte // podprogramy lokalne FD bieżącego glifu

	usedGlobal map[int]bool
	usedLocal  map[int]bool

	flatten bool   // czy budować charstring bez wywołań
	out     []byte // charstring bez wywołań
	stack   []int  // początki (w out) liczb leżących na stosie
	vals    []float64
	nStems  int
	vsindex int
	ended   bool

	// pendingWidth: dla CFF2 szerokość dopisana na początku out zajmuje miejsce na stosie do pierwszego
	// operatora czyszczącego stos
	pendingWidth bool
}

// run przechodzi charstring glifu; zwraca błąd, gdy nie da się go rozwinąć
func (w *walker) run(cs []byte, depth int) error {
	for i := 0; i < len(cs); {
		b0 := cs[i]
		if b0 >= 32 || b0 == 28 {
			n, v := charstringNumber(cs[i:])
			if n == 0 {
				return errMalformed
			}
			w.stack = append(w.stack, len(w.out))
			w.vals = append(w.vals, v)
			if w.flatten {
				w.out = append(w.out, cs[i:i+n]...)
			}
			i += n
			continue
		}
		op, n := int(b0), 1
		if b0 == 12 {
			if i+1 >= len(cs) {
				return errMalformed
			}
			op, n = 1200+int(cs[i+1]), 2
		}
		switch op {
		case 10, 29: // callsubr, callgsubr
			if len(w.stack) == 0 {
				return errMalformed
			}
			v := w.vals[len(w.vals)-1]
			if v != float64(int(v)) {
				return errUnsupportedCharstring
			}
			if w.flatten {
				w.out = w.out[:w.stack[len(w.stack)-1]]
			}
			w.stack, w.vals = w.stack[:len(w.stack)-1], w.vals[:len(w.vals)-1]
			subrs, used := w.local, w.usedLocal
			if op == 29 {
				subrs, used = w.src.globalSubrs, w.usedGlobal
			}
			idx := int(v) + subrBias(len(subrs))
			if idx < 0 || idx >= len(subrs) || depth >= maxSubrDepth {
				return errMalformed
			}
			used[idx] = true
			if err := w.run(subrs[idx], depth+1); err != nil {
				return err
			}
			if w.ended {
				return nil
			}
			i += n
			continue
		case 11: // return
			if w.src.cff2 || depth == 0 {
				return errMalformed
			}
			return nil
		case 14: // endchar
			if w.src.cff2 {
				return errMalformed
			}
			if len(w.stack) >= 4 { // seac (TN #5177, dodatek C): akcent z glifów kodowania standardowego
				return errUnsupportedCharstring
			}
			w.emitOp(cs[i : i+n])
			w.ended = true
			return nil
		case 1, 3, 18, 23: // hstem, vstem, hstemhm, vstemhm
			w.nStems += len(w.stack) / 2
			if err := w.emitClearing(cs[i : i+n]); err != nil {
				return err
			}
		case 19, 20: // hintmask, cntrmask
			w.nStems += len(w.stack) / 2
			mask := (w.nStems + 7) / 8
			if i+n+mask > len(cs) {
				return errMalformed
			}
			if err := w.emitClearing(cs[i : i+n+mask]); err != nil {
				return err
			}
			i += mask
		case 15: // vsindex (CFF2)
			if !w.src.cff2 || len(w.stack) != 1 {
				return errMalformed
			}
			w.vsindex = int(w.vals[0])
			w.dropStack(0)
		case 16: // blend (CFF2)
			if !w.src.cff2 || len(w.stack) == 0 {
				return errMalformed
			}
			count := int(w.vals[len(w.vals)-1])
			if w.vsindex < 0 || w.vsindex >= len(w.src.vsRegions) {
				return errMalformed
			}
			k := w.src.vsRegions[w.vsindex]
			first := len(w.stack) - 1 - count*(k+1)
			if count < 0 || first < 0 {
				return errMalformed
			}
			w.dropStack(first + count) // zostają wartości domyślne
		case 21, 22, 4, 5, 6, 7, 8, 24, 25, 26, 27, 30, 31, // ruchy, linie i krzywe
			1234, 1235, 1236, 1237: // hflex, flex, hflex1, flex1
			if err := w.emitClearing(cs[i : i+n]); err != nil {
				return err
			}
		default: // arytmetyka, put/get, random i operatory zarezerwowane
			return errUnsupportedCharstring
		}
		i += n
	}
	if depth == 0 {
		if !w.src.cff2 && !w.ended {
			return errMalformed // charstring Type 2 kończy endchar
		}
		w.emitOp([]byte{14})
		w.ended = true
	}
	return nil // koniec podprogramu CFF2 działa jak return
}

func (w *walker) emitOp(op []byte) {
	if w.flatten {
		w.out = append(w.out, op...)
	}
	w.stack, w.vals = w.stack[:0], w.vals[:0]
}

// emitClearing zapisuje operator czyszczący stos; argumenty ponad granicę stosu Type 2 (możliwe po
// przepisaniu z CFF2) dzielą się na kilka operatorów, gdy pozwala na to operator (rlineto, rrcurveto)
func (w *walker) emitClearing(op []byte) error {
	limit := maxType2Stack
	if w.pendingWidth {
		limit--
		w.pendingWidth = false
	}
	if n := len(w.stack); n > limit && w.flatten {
		group := 0
		switch op[0] {
		case 5: // rlineto
			group = 2
		case 8: // rrcurveto
			group = 6
		}
		if group == 0 || n%group != 0 {
			return errUnsupportedCharstring
		}
		start := w.stack[0]
		tail := append([]byte(nil), w.out[start:]...)
		w.out = w.out[:start]
		for j := 0; j < n; {
			end := min(j+(limit/group)*group, n)
			from, to := w.stack[j]-start, len(tail)
			if end < n {
				to = w.stack[end] - start
			}
			w.out = append(w.out, tail[from:to]...)
			w.out = append(w.out, op...)
			j, limit = end, maxType2Stack
		}
		w.stack, w.vals = w.stack[:0], w.vals[:0]
		return nil
	}
	w.emitOp(op)
	return nil
}

// dropStack usuwa ze stosu (i z out) liczby od pozycji from
func (w *walker) dropStack(from int) {
	if w.flatten {
		end := len(w.out)
		if from < len(w.stack) {
			end = w.stack[from]
		}
		w.out = w.out[:end]
	}
	w.stack, w.vals = w.stack[:from], w.vals[:from]
}

// charstringNumber zwraca długość i wartość liczby charstringu Type 2 (TN #5177 § 3.2)
func charstringNumber(b []byte) (int, float64) {
	b0 := b[0]
	switch {
	case b0 == 28:
		if len(b) < 3 {
			return 0, 0
		}
		return 3, float64(int16(binary.BigEndian.Uint16(b[1:])))
	case b0 >= 32 && b0 <= 246:
		return 1, float64(int(b0) - 139)
	case b0 >= 247 && b0 <= 250:
		if len(b) < 2 {
			return 0, 0
		}
		return 2, float64((int(b0)-247)*256 + int(b[1]) + 108)
	case b0 >= 251 && b0 <= 254:
		if len(b) < 2 {
			return 0, 0
		}
		return 2, float64(-(int(b0)-251)*256 - int(b[1]) - 108)
	case b0 == 255:
		if len(b) < 5 {
			return 0, 0
		}
		return 5, float64(int32(binary.BigEndian.Uint32(b[1:]))) / 65536
	}
	return 0, 0
}
