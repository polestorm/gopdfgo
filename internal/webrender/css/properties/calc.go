package properties

import (
	"errors"
	"fmt"
)

// Plik dodany w gopdfgo, nie pochodzi ze źródła webrender.

// CalcOp to rodzaj węzła wyrażenia calc().
type CalcOp uint8

const (
	CalcLeaf  CalcOp = iota // liczba, długość albo procent
	CalcAdd                 // a + b
	CalcSub                 // a - b
	CalcMul                 // a * b, jeden z argumentów jest liczbą
	CalcDiv                 // a / b, b jest liczbą różną od zera
	CalcMin                 // min(a, b, ...)
	CalcMax                 // max(a, b, ...)
	CalcClamp               // clamp(min, wartość, max)
)

// CalcNode to drzewo wyrażenia calc(), min(), max() albo clamp() zapamiętane przy walidacji.
// Wartość poznajemy dopiero przy liczeniu wartości wyliczonych (jednostki zależne od fontu),
// a część procentową dopiero w układzie.
type CalcNode struct {
	Op    CalcOp
	Value Float // dla CalcLeaf
	Unit  Unit  // dla CalcLeaf: Scalar, Perc albo jednostka długości
	Args  []*CalcNode
}

// CalcResult to wynik wyrażenia: liczba albo suma pikseli i procentów.
type CalcResult struct {
	Pixels   Float // piksele; dla liczby jej wartość
	Percent  Float
	IsNumber bool
}

var errCalcTypes = errors.New("calc: mismatched types")

// Resolve liczy wyrażenie; toPixels zamienia długość w podanej jednostce na piksele.
// Błąd oznacza wyrażenie niepoprawne typami (długość + liczba, dzielenie przez długość)
// albo min/max/clamp z procentami, których nie da się sprowadzić do sumy pikseli i procentów.
func (n *CalcNode) Resolve(toPixels func(Float, Unit) Float) (CalcResult, error) {
	switch n.Op {
	case CalcLeaf:
		switch n.Unit {
		case Scalar:
			return CalcResult{Pixels: n.Value, IsNumber: true}, nil
		case Perc:
			return CalcResult{Percent: n.Value}, nil
		default:
			return CalcResult{Pixels: toPixels(n.Value, n.Unit)}, nil
		}
	case CalcAdd, CalcSub, CalcMul, CalcDiv:
		if len(n.Args) != 2 {
			return CalcResult{}, errCalcTypes
		}
		a, err := n.Args[0].Resolve(toPixels)
		if err != nil {
			return CalcResult{}, err
		}
		b, err := n.Args[1].Resolve(toPixels)
		if err != nil {
			return CalcResult{}, err
		}
		switch n.Op {
		case CalcAdd, CalcSub:
			if a.IsNumber != b.IsNumber {
				return CalcResult{}, errCalcTypes
			}
			if n.Op == CalcSub {
				b.Pixels, b.Percent = -b.Pixels, -b.Percent
			}
			return CalcResult{Pixels: a.Pixels + b.Pixels, Percent: a.Percent + b.Percent, IsNumber: a.IsNumber}, nil
		case CalcMul:
			if !a.IsNumber && !b.IsNumber {
				return CalcResult{}, errCalcTypes
			}
			if !a.IsNumber {
				a, b = b, a // a jest liczbą
			}
			return CalcResult{Pixels: a.Pixels * b.Pixels, Percent: a.Pixels * b.Percent, IsNumber: b.IsNumber}, nil
		default: // CalcDiv
			if !b.IsNumber || b.Pixels == 0 {
				return CalcResult{}, errCalcTypes
			}
			return CalcResult{Pixels: a.Pixels / b.Pixels, Percent: a.Percent / b.Pixels, IsNumber: a.IsNumber}, nil
		}
	case CalcMin, CalcMax, CalcClamp:
		if len(n.Args) == 0 || (n.Op == CalcClamp && len(n.Args) != 3) {
			return CalcResult{}, errCalcTypes
		}
		values := make([]CalcResult, len(n.Args))
		for i, arg := range n.Args {
			value, err := arg.Resolve(toPixels)
			if err != nil {
				return CalcResult{}, err
			}
			if value.Percent != 0 || value.IsNumber != values[0].IsNumber && i > 0 {
				return CalcResult{}, fmt.Errorf("calc: percentages are not supported in min(), max() and clamp()")
			}
			values[i] = value
		}
		out := values[0]
		switch n.Op {
		case CalcMin:
			for _, v := range values[1:] {
				out.Pixels = min(out.Pixels, v.Pixels)
			}
		case CalcMax:
			for _, v := range values[1:] {
				out.Pixels = max(out.Pixels, v.Pixels)
			}
		default: // clamp(min, wartość, max) = max(min, min(wartość, max))
			out.Pixels = max(values[0].Pixels, min(values[1].Pixels, values[2].Pixels))
		}
		return out, nil
	}
	return CalcResult{}, errCalcTypes
}

// HasPercentage mówi, czy w wyrażeniu występuje procent.
func (n *CalcNode) HasPercentage() bool {
	if n.Op == CalcLeaf {
		return n.Unit == Perc
	}
	for _, arg := range n.Args {
		if arg.HasPercentage() {
			return true
		}
	}
	return false
}
