package layout

import (
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
)

// alignOutsideMarker (gopdfgo) ustawia znacznik elementu listy umieszczony na zewnątrz
// (list-style-position: outside) obok pierwszego wiersza elementu (CSS Lists 3 § 3.1, CSS Pseudo 4 § 3.2:
// znacznik przylega do pierwszego wiersza; tak też Chromium). Gdy fragment elementu nie ma jeszcze
// żadnego wiersza, a element ciągnie się w następnej kolumnie albo na następnej stronie, znacznik przechodzi
// do fragmentu z pierwszym wierszem (test WPT multicol-list-item-008). W źródle znacznik stał zawsze na
// górze elementu, więc np. przy pierwszym dziecku z górnym marginesem był nad tekstem.
func alignOutsideMarker(context *layoutContext, box_ Box, children []Box, isStart, fragmented bool,
	absoluteBoxes *[]*AbsolutePlaceholder,
) []Box {
	box := box_.Box()
	style := box.Style
	// pudełka kolumn są tworzone od nowa dla każdej kolumny; znacznik kontenera kolumn ustawia
	// alignColumnsMarker
	if box.IsColumn {
		return children
	}
	if isStart {
		if context.pendingMarkers != nil {
			delete(context.pendingMarkers, style)
		}
		index := -1
		for i, child := range children {
			if placeholder, ok := child.(*AbsolutePlaceholder); ok && !placeholder.layoutDone && placeholder.AliasBox.Box().IsOutsideMarker {
				index = i
				break
			}
		}
		if index < 0 {
			return children
		}
		marker := children[index]
		if line := markerLine(children); line != nil {
			marker.Translate(0, line.Box().PositionY-marker.Box().PositionY, false)
			return children
		}
		if fragmented {
			removePlaceholders(context, []Box{marker}, absoluteBoxes, new([]*AbsolutePlaceholder))
			if context.pendingMarkers == nil {
				context.pendingMarkers = map[pr.ElementStyle]Box{}
			}
			context.pendingMarkers[style] = marker.(*AbsolutePlaceholder).AliasBox
			return append(children[:index:index], children[index+1:]...)
		}
		return children
	}
	marker, ok := context.pendingMarkers[style]
	if !ok {
		return children
	}
	line := markerLine(children)
	if line == nil {
		if !fragmented {
			delete(context.pendingMarkers, style)
		}
		return children
	}
	delete(context.pendingMarkers, style)
	placeholder := NewAbsolutePlaceholder(marker.Copy())
	placeholder.Box().PositionX = box.BorderBoxX()
	if marker.Box().Style.GetDirection() == pr.Rtl {
		placeholder.Box().PositionX += box.Width.V() + box.PaddingRight.V()
	}
	placeholder.Box().PositionY = line.Box().PositionY
	*absoluteBoxes = append(*absoluteBoxes, placeholder)
	return append([]Box{placeholder}, children...)
}

// markerLine podaje pierwszy niepusty wiersz w przepływie wśród dzieci i ich potomków blokowych albo nil,
// gdy przed nim stoi pudełko innego rodzaju (np. tabela albo blok z własnym kontekstem formatowania).
func markerLine(children []Box) Box {
	for _, child := range children {
		if !child.Box().IsInNormalFlow() {
			continue
		}
		if line, ok := child.(*bo.LineBox); ok {
			if line.Height == pr.Float(0) && isEmptyLinebox(line) {
				continue
			}
			return child
		}
		if !bo.BlockT.IsInstance(child) || bo.EstablishesFormattingContext(child) {
			return nil
		}
		if line := markerLine(child.Box().Children); line != nil {
			return line
		}
	}
	return nil
}

// alignColumnsMarker (gopdfgo) robi to samo co alignOutsideMarker dla elementu listy, który jest kontenerem
// kolumn: znacznik z pierwszej kolumny staje obok pierwszego wiersza, choćby ten leżał w dalszej kolumnie
// albo za elementem rozpinającym (test WPT multicol-list-item-003).
func alignColumnsMarker(children []Box) {
	var marker Box
	for _, child := range children {
		if !child.Box().IsColumn {
			continue
		}
		for _, c := range child.Box().Children {
			if placeholder, ok := c.(*AbsolutePlaceholder); ok && !placeholder.layoutDone && placeholder.AliasBox.Box().IsOutsideMarker {
				marker = c
			}
		}
		break
	}
	if marker == nil {
		return
	}
	for _, child := range children {
		// także element rozpinający, choć zakłada własny kontekst formatowania (test WPT
		// multicol-list-item-005)
		line := markerLine(child.Box().Children)
		if line != nil {
			marker.Translate(0, line.Box().PositionY-marker.Box().PositionY, false)
			return
		}
	}
}
