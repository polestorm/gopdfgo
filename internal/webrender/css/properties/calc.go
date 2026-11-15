package properties

import (
	"errors"
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

// ErrCalcPercentComparison: min(), max() albo clamp() z procentem; poprawne, ale liczone dopiero w układzie.
var ErrCalcPercentComparison = errors.New("calc: percentages in min(), max() and clamp() are resolved at layout time")

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
			if value.IsNumber != values[0].IsNumber && i > 0 {
				return CalcResult{}, errCalcTypes
			}
			if value.Percent != 0 {
				return CalcResult{}, ErrCalcPercentComparison
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

// Freeze zwraca kopię wyrażenia z długościami sprowadzonymi do pikseli. Używane dla min(), max() i clamp()
// z procentami: takie wyrażenie da się policzyć dopiero w układzie, gdy znana jest podstawa procentu.
func (n *CalcNode) Freeze(toPixels func(Float, Unit) Float) *CalcNode {
	out := &CalcNode{Op: n.Op, Value: n.Value, Unit: n.Unit}
	if n.Op == CalcLeaf {
		if n.Unit != Scalar && n.Unit != Perc && n.Unit != Px {
			out.Value, out.Unit = toPixels(n.Value, n.Unit), Px
		}
		return out
	}
	for _, arg := range n.Args {
		out.Args = append(out.Args, arg.Freeze(toPixels))
	}
	return out
}

// Evaluate liczy wyrażenie zamrożone przez Freeze przy podanej podstawie procentu; wynik w pikselach
// (albo liczba dla wyrażeń liczbowych).
func (n *CalcNode) Evaluate(percentBase Float) Float {
	switch n.Op {
	case CalcLeaf:
		if n.Unit == Perc {
			return n.Value * percentBase / 100
		}
		return n.Value
	case CalcAdd, CalcSub, CalcMul, CalcDiv:
		if len(n.Args) != 2 {
			return 0
		}
		a, b := n.Args[0].Evaluate(percentBase), n.Args[1].Evaluate(percentBase)
		switch n.Op {
		case CalcAdd:
			return a + b
		case CalcSub:
			return a - b
		case CalcMul:
			return a * b
		default:
			if b == 0 {
				return 0
			}
			return a / b
		}
	case CalcMin, CalcMax, CalcClamp:
		if len(n.Args) == 0 {
			return 0
		}
		values := make([]Float, len(n.Args))
		for i, arg := range n.Args {
			values[i] = arg.Evaluate(percentBase)
		}
		out := values[0]
		switch {
		case n.Op == CalcMin:
			for _, v := range values[1:] {
				out = min(out, v)
			}
		case n.Op == CalcMax:
			for _, v := range values[1:] {
				out = max(out, v)
			}
		case len(values) == 3:
			out = max(values[0], min(values[1], values[2]))
		}
		return out
	}
	return 0
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
