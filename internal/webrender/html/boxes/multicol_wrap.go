package boxes

import (
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
)

// gopdfgo: właściwości kontenera kolumn, które przechodzą z komórki tabeli albo pudełka
// liniowo-blokowego na anonimowy blok kolumn (zob. MulticolWrappers).
var multicolContainerProperties = [...]pr.KnownProp{
	pr.PColumnCount, pr.PColumnWidth, pr.PColumnGap, pr.PColumnFill,
	pr.PColumnRuleColor, pr.PColumnRuleStyle, pr.PColumnRuleWidth,
}

// IsMulticol mówi, czy styl czyni pudełko kontenerem kolumn (CSS Multicol 1 § 2: column-width
// albo column-count różne od auto).
func IsMulticol(style pr.ElementStyle) bool {
	return style.GetColumnWidth().Tag != pr.Auto || style.GetColumnCount().String != "auto"
}

// MulticolWrappers przenosi treść komórki tabeli i pudełka liniowo-blokowego, które są kontenerami
// kolumn, do anonimowego bloku z właściwościami kolumn; sama komórka (pudełko) dostaje dla nich
// wartości początkowe.
//
// gopdfgo: kolumny dotyczą każdego kontenera blokowego poza pudełkiem opakowującym tabelę (CSS
// Multicol 1 § 2), a źródło układało w kolumnach tylko zwykłe bloki; treść komórki z column-count
// stała w jednej kolumnie. Anonimowy blok pozwala użyć układu kolumn bez zmian: komórka nadal
// ustala wysokość wiersza i vertical-align z wysokości treści, a ta jest wysokością zrównoważonych
// kolumn. Blok jest układany jak każdy kontener kolumn, także w kolumnach zewnętrznych.
func MulticolWrappers(box Box) Box {
	if !ParentT.IsInstance(box) || box.Box().IsRunning() {
		return box
	}
	for i, child := range box.Box().Children {
		box.Box().Children[i] = MulticolWrappers(child)
	}
	b := box.Box()
	if b.IsTableWrapper || len(b.Children) == 0 || !IsMulticol(b.Style) ||
		!(TableCellT.IsInstance(box) || InlineBlockT.IsInstance(box)) {
		return box
	}
	wrapper := BlockBoxAnonymousFrom(box, b.Children)
	wrapper.Style = wrapper.Style.Copy()
	b.Style = b.Style.Copy()
	for _, name := range multicolContainerProperties {
		key := name.Key()
		wrapper.Style.Set(key, b.Style.Get(key))
		b.Style.Set(key, pr.InitialValues[name])
	}
	b.Children = []Box{wrapper}
	return box
}
