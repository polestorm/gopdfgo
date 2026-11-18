package layout

import (
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
)

// gopdfgo: wielokropek bloku (block-ellipsis, CSS Overflow 4 § 4.4) wstawiany w układzie. W źródle
// wiersz dostawał tylko znacznik LineBox.BlockEllipsis, a wielokropek dopisywało rysowanie do
// każdego pudełka tekstu wiersza osobno, w stylu tego tekstu; norma każe go umieścić w anonimowym
// pudełku liniowym, którego rodzicem jest główne pudełko liniowe kontenera (styl bloku), na końcu
// wiersza, a treść, która się z nim nie mieści, usunąć.

// ellipsisPseudoType oznacza pudełko wielokropka bloku wstawione przez układ.
const ellipsisPseudoType = "-gopdfgo-block-ellipsis"

// ellipsisText zwraca tekst wielokropka bloku albo "" przy block-ellipsis: none.
func ellipsisText(ellipsis pr.TaggedString) string {
	switch ellipsis.Tag {
	case pr.None:
		return ""
	case pr.Auto:
		return "…"
	}
	return ellipsis.S
}

// newEllipsisBox układa pudełko wielokropka w stylu wiersza (kontenera blokowego); nil, gdy
// wielokropka nie ma.
func newEllipsisBox(context *layoutContext, line *bo.LineBox, ellipsis pr.TaggedString) *bo.TextBox {
	s := ellipsisText(ellipsis)
	if s == "" {
		return nil
	}
	box, _, _ := splitTextBox(context, bo.NewTextBox(line.Style, line.Element, ellipsisPseudoType, []rune(s)), pr.Inf, 0, false)
	if box != nil {
		zero := pr.Float(0)
		box.PaddingLeft, box.PaddingRight, box.PaddingTop, box.PaddingBottom = zero, zero, zero, zero
		box.BorderLeftWidth, box.BorderRightWidth, box.BorderTopWidth, box.BorderBottomWidth = zero, zero, zero, zero
		box.MarginLeft, box.MarginRight = zero, zero
	}
	return box
}

// ellipsisWidth zwraca szerokość wielokropka bloku w stylu wiersza.
func ellipsisWidth(context *layoutContext, line *bo.LineBox, ellipsis pr.TaggedString) pr.Float {
	if box := newEllipsisBox(context, line, ellipsis); box != nil {
		return box.Width.V()
	}
	return 0
}

// insertBlockEllipsis kończy ułożony wiersz wielokropkiem bloku: usuwa z końca wiersza treść
// (całe słowa, pudełka atomowe), aż wielokropek zmieści się przed limit, i dopisuje go za
// pozostałą treścią. Wiersz od prawej do lewej zostaje przy rysowaniu wielokropka jak w źródle.
func insertBlockEllipsis(context *layoutContext, line *bo.LineBox, ellipsis pr.TaggedString, limit pr.Float) {
	if ellipsisText(ellipsis) == "" {
		return
	}
	if n := len(line.Children); n != 0 && line.Children[n-1].Box().PseudoType == ellipsisPseudoType {
		return // wielokropek wstawiony już przez kontener zagnieżdżony
	}
	if line.Style.GetDirection() == pr.Rtl {
		line.BlockEllipsis = ellipsis
		return
	}
	box := newEllipsisBox(context, line, ellipsis)
	if box == nil {
		return
	}
	trimInlineTo(context, line, limit-box.Width.V())
	removeLastWhitespace(context, line)
	end := line.PositionX + line.Width.V()
	box.PositionX = end
	if line.Baseline != nil {
		box.PositionY = line.PositionY + line.Baseline.V() - box.Baseline.V()
	} else {
		box.PositionY = line.PositionY
	}
	line.Children = append(line.Children, box)
	line.Width = end + box.Width.V() - line.PositionX
}

// trimInlineTo usuwa z końca pudełka wiersza albo pudełka liniowego treść, która sięga poza limit,
// i zwraca prawą krawędź marginesu tego, co zostało. Tekst jest skracany do ostatniego miejsca
// podziału, które się mieści; tekst bez takiego miejsca i pudełka atomowe znikają w całości.
func trimInlineTo(context *layoutContext, box_ Box, limit pr.Float) pr.Float {
	box := box_.Box()
	_, isLine := box_.(*bo.LineBox)
	contentX := box.PositionX
	var rightSpacing pr.Float
	if !isLine {
		contentX = box.ContentBoxX()
		rightSpacing = box.PaddingRight.V() + box.BorderRightWidth.V() + box.MarginRight.V()
	}
	innerLimit := limit - rightSpacing
	end := contentX
	children := box.Children
	i := len(children) - 1
	for ; i >= 0; i-- {
		child := children[i].Box()
		if !child.IsInNormalFlow() {
			continue
		}
		childEnd := child.PositionX + child.MarginWidth()
		if childEnd <= innerLimit {
			end = childEnd
			break
		}
		if text, ok := children[i].(*bo.TextBox); ok && len(text.Text) != 0 {
			if available := innerLimit - child.PositionX; available > 0 {
				part, resume, _ := splitTextBox(context, text, available, 0, false)
				if part != nil && resume != -1 && part.PositionX+part.MarginWidth() <= innerLimit {
					children[i] = part
					end = part.PositionX + part.MarginWidth()
					break
				}
			}
		} else if IsLine(children[i]) && child.PositionX < innerLimit {
			end = trimInlineTo(context, children[i], innerLimit)
			break
		}
		// usuwamy dziecko: pudełka spoza przepływu za nim zostają
		children = append(children[:i:i], children[i+1:]...)
	}
	box.Children = children
	if isLine {
		box.Width = end - box.PositionX
		return end
	}
	box.Width = max(0, end-contentX)
	return end + rightSpacing
}
