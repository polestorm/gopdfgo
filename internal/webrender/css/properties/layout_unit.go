package properties

import "math"

// LayoutUnit (gopdfgo) obcina długość do 1/64 piksela CSS w stronę zera, jak Blink przy przejściu
// z liczby zmiennoprzecinkowej na LayoutUnit, swoją jednostkę długości układu (ADR 0013, przełącznik
// ExactLengths). Wartości nieskończone zostają bez zmian. Margines 1e-3 jednostki chroni wartości,
// które w arytmetyce float32 wypadły tuż pod wielokrotnością 1/64 (12 jako 11,9999995).
func LayoutUnit(v Float) Float {
	if math.IsInf(float64(v), 0) || math.IsNaN(float64(v)) {
		return v
	}
	x := float64(v) * 64
	if x >= 0 {
		return Float(math.Floor(x+1e-3) / 64)
	}
	return Float(-math.Floor(-x+1e-3) / 64)
}

// MaybeLayoutUnit (gopdfgo) to [LayoutUnit] dla wartości, która może być auto.
func MaybeLayoutUnit(v MaybeFloat) MaybeFloat {
	if f, ok := v.(Float); ok {
		return LayoutUnit(f)
	}
	return v
}
