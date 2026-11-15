package validation

import (
	"errors"
	"strings"

	pa "github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
)

// Plik dodany w gopdfgo, nie pochodzi ze źródła webrender.

// parseCalcFunction zamienia calc(), min(), max() albo clamp() na drzewo wyrażenia.
// Zwraca nil dla innej funkcji, błędu składni albo niezgodności typów.
func parseCalcFunction(token pa.FunctionBlock) *pr.CalcNode {
	node := calcFunction(token)
	if node == nil {
		return nil
	}
	// sprawdzenie typów na sucho: każda długość liczona jako jej wartość
	if _, err := node.Resolve(func(v pr.Float, _ pr.Unit) pr.Float { return v }); err != nil && !errors.Is(err, pr.ErrCalcPercentComparison) {
		return nil
	}
	return node
}

func calcFunction(token pa.FunctionBlock) *pr.CalcNode {
	name := strings.ToLower(token.Name)
	tokens := withoutWhitespace(token.Arguments)
	switch name {
	case "calc":
		return calcSum(tokens)
	case "min", "max", "clamp":
		var args []*pr.CalcNode
		for _, part := range splitOnComma(tokens) {
			arg := calcSum(part)
			if arg == nil {
				return nil
			}
			args = append(args, arg)
		}
		op := map[string]pr.CalcOp{"min": pr.CalcMin, "max": pr.CalcMax, "clamp": pr.CalcClamp}[name]
		return &pr.CalcNode{Op: op, Args: args}
	}
	return nil
}

func withoutWhitespace(tokens []pa.Token) []pa.Token {
	out := make([]pa.Token, 0, len(tokens))
	for _, token := range tokens {
		switch token.(type) {
		case pa.Whitespace, pa.Comment:
		default:
			out = append(out, token)
		}
	}
	return out
}

func literal(token pa.Token) string {
	if lit, ok := token.(pa.Literal); ok {
		return lit.Value
	}
	return ""
}

func splitOnComma(tokens []pa.Token) [][]pa.Token {
	var out [][]pa.Token
	start := 0
	for i, token := range tokens {
		if literal(token) == "," {
			out = append(out, tokens[start:i])
			start = i + 1
		}
	}
	return append(out, tokens[start:])
}

// calcSum: iloczyn (("+" | "-") iloczyn)*
func calcSum(tokens []pa.Token) *pr.CalcNode {
	var (
		result *pr.CalcNode
		op     = pr.CalcAdd
		start  = 0
	)
	flush := func(end int) bool {
		term := calcProduct(tokens[start:end])
		if term == nil {
			return false
		}
		if result == nil {
			result = term
		} else {
			result = &pr.CalcNode{Op: op, Args: []*pr.CalcNode{result, term}}
		}
		return true
	}
	for i, token := range tokens {
		if sign := literal(token); sign == "+" || sign == "-" {
			if !flush(i) {
				return nil
			}
			op = pr.CalcAdd
			if sign == "-" {
				op = pr.CalcSub
			}
			start = i + 1
		}
	}
	if !flush(len(tokens)) {
		return nil
	}
	return result
}

// calcProduct: wartość (("*" | "/") wartość)*
func calcProduct(tokens []pa.Token) *pr.CalcNode {
	if len(tokens) == 0 || len(tokens)%2 == 0 {
		return nil
	}
	result := calcValue(tokens[0])
	for i := 1; result != nil && i < len(tokens); i += 2 {
		operand := calcValue(tokens[i+1])
		if operand == nil {
			return nil
		}
		switch literal(tokens[i]) {
		case "*":
			result = &pr.CalcNode{Op: pr.CalcMul, Args: []*pr.CalcNode{result, operand}}
		case "/":
			result = &pr.CalcNode{Op: pr.CalcDiv, Args: []*pr.CalcNode{result, operand}}
		default:
			return nil
		}
	}
	return result
}

func calcValue(token pa.Token) *pr.CalcNode {
	switch token := token.(type) {
	case pa.Number:
		return &pr.CalcNode{Value: pr.Float(token.ValueF), Unit: pr.Scalar}
	case pa.Percentage:
		return &pr.CalcNode{Value: pr.Float(token.ValueF), Unit: pr.Perc}
	case pa.Dimension:
		if unit, ok := LENGTHUNITS[strings.ToLower(string(token.Unit))]; ok {
			return &pr.CalcNode{Value: pr.Float(token.ValueF), Unit: unit}
		}
	case pa.ParenthesesBlock:
		return calcSum(withoutWhitespace(token.Arguments))
	case pa.FunctionBlock:
		return calcFunction(token)
	}
	return nil
}
