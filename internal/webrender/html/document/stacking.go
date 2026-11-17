package document

import (
	"fmt"
	"slices"
	"sort"

	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/layout"
)

type aliasForStackingContext = bo.Box

var _ bo.Box = StackingContext{}

// BoxTree przypisuje pudełkom ich pudełka wewnętrzne z zachowaniem kolejności wstawiania.
//
// gopdfgo: w źródle było to map[bo.Box]BoxTree (odpowiednik słownika Pythona, który pamięta kolejność).
// Mapa Go jej nie pamięta, więc kolejność rysowania bloków, a z nią bajty wyniku, zmieniała się
// między uruchomieniami.
type BoxTree = *boxTree

type boxTree struct {
	boxes []bo.Box
	inner []BoxTree
	index map[bo.Box]int
}

func newBoxTree() BoxTree { return &boxTree{index: map[bo.Box]int{}} }

// set dopisuje pudełko albo, jak przypisanie w mapie, podmienia jego poddrzewo.
func (t *boxTree) set(box bo.Box, inner BoxTree) {
	if i, ok := t.index[box]; ok {
		t.inner[i] = inner
		return
	}
	t.index[box] = len(t.boxes)
	t.boxes = append(t.boxes, box)
	t.inner = append(t.inner, inner)
}

// Stacking contexts define the paint order of all pieces of a document.
// http://www.w3.org/TR/CSS21/visuren.html#x43
// http://www.w3.org/TR/CSS21/zindex.html
type StackingContext struct {
	// StackingContext needs to implement Box (see below)
	aliasForStackingContext
	box               Box
	page              *bo.PageBox
	blockLevelBoxes   []bo.Box
	floatContexts     []StackingContext
	negativeZContexts []StackingContext
	zeroZContexts     []StackingContext
	positiveZContexts []StackingContext
	blocksAndCells    BoxTree
	zIndex            int
}

func NewStackingContext(box Box, childContexts []StackingContext, blocks []bo.Box, floats []StackingContext, blocksAndCells BoxTree,
	page *bo.PageBox,
) StackingContext {
	self := StackingContext{}
	self.box = box
	self.page = page
	self.blockLevelBoxes = blocks        // 4: In flow, non positioned
	self.floatContexts = floats          // 5: Non positioned
	self.negativeZContexts = nil         // 3: Child contexts, z-index < 0
	self.zeroZContexts = nil             // 8: Child contexts, z-index = 0
	self.positiveZContexts = nil         // 9: Child contexts, z-index > 0
	self.blocksAndCells = blocksAndCells // 7: Non positioned

	for _, context := range childContexts {
		if context.zIndex < 0 {
			self.negativeZContexts = append(self.negativeZContexts, context)
		} else if context.zIndex == 0 {
			self.zeroZContexts = append(self.zeroZContexts, context)
		} else { // context.zIndex > 0
			self.positiveZContexts = append(self.positiveZContexts, context)
		}
	}
	sort.SliceStable(self.negativeZContexts, func(i, j int) bool {
		return self.negativeZContexts[i].zIndex < self.negativeZContexts[j].zIndex
	})
	sort.SliceStable(self.positiveZContexts, func(i, j int) bool {
		return self.positiveZContexts[i].zIndex < self.positiveZContexts[j].zIndex
	})
	// sort() is stable, so the lists are now storted
	// by z-index, then tree order.

	zIndex := box.Box().Style.GetZIndex()
	if zIndex.String == "auto" {
		self.zIndex = 0
	} else {
		self.zIndex = zIndex.Int
	}
	return self
}

func NewStackingContextFromPage(page *bo.PageBox) StackingContext {
	// Page children (the box for the root element and margin boxes)
	// as well as the page box itself are unconditionally stacking contexts.
	childContexts := make([]StackingContext, len(page.Children))
	for i, child := range page.Children {
		childContexts[i] = NewStackingContextFromBox(child, page, nil)
	}
	// Children are sub-contexts, remove them from the "normal" tree.
	page = bo.CopyWithChildren(page, nil).(*bo.PageBox)
	return NewStackingContext(page, childContexts, nil, nil, nil, page)
}

func insertStackingContext(a *[]StackingContext, i int, item StackingContext) {
	*a = slices.Insert(*a, i, item)
}

func NewStackingContextFromBox(box Box, page *bo.PageBox, childContexts *[]StackingContext) StackingContext {
	return newStackingContextFromBox(box, page, childContexts, nil)
}

// escape (gopdfgo): lista kontekstu leżącego na zewnątrz najbliższego przodka
// z overflow innym niż visible, który sam nie jest pozycjonowany. Trafiają do
// niej pudełka pozycjonowane bezwzględnie, których bryła zawierająca leży poza
// tym przodkiem: przycięcie obejmuje tylko potomków, dla których element jest
// (pośrednio) bryłą zawierającą (CSS 2.1 § 11.1.1). Źródło przycinało wszystkich
// potomków, więc takie pudełko znikało, gdy leżało poza przodkiem. nil oznacza,
// że nie ma skąd uciekać.
func newStackingContextFromBox(box Box, page *bo.PageBox, childContexts, escape *[]StackingContext) StackingContext {
	var children []StackingContext // What will be passed to this box
	if childContexts == nil {
		childContexts = &children
	}
	// childContexts: where to put sub-contexts that we find here.
	// May not be the same as children for :
	//   "treat the element as if it created a new stacking context,
	//    but any positioned descendants && descendants which actually
	//    create a new stacking context should be considered part of the
	//    parent stacking context, not this new one."
	var (
		blocks         []Box
		floats         []StackingContext
		blocksAndCells = newBoxTree()
	)

	box = dispatchChildren(box, page, childContexts, escape, &blocks, &floats, blocksAndCells)
	return NewStackingContext(box, children, blocks, floats, blocksAndCells, page)
}

func dispatch(box Box, page *bo.PageBox, childContexts, escape *[]StackingContext,
	blocks *[]Box, floats *[]StackingContext, blocksAndCells BoxTree,
) Box {
	if absPlac, ok := box.(*layout.AbsolutePlaceholder); ok {
		box = absPlac.AliasBox
	} else if stack, ok := box.(StackingContext); ok {
		box = stack.box
	}
	style := box.Box().Style
	position := style.GetPosition().String
	positioned := position != "static"
	hasTransform := len(style.GetTransform()) != 0 || box.Box().PageScale > 1 // gopdfgo: zob. BoxFields.PageScale
	if escape != nil && (position == "absolute" || position == "fixed") {
		// bryła zawierająca tego pudełka leży poza przycinającym przodkiem
		childContexts, escape = escape, nil
	}
	// dla potomków pudełka pozycjonowanego lub przekształconego bryłą
	// zawierającą jest ono samo, więc nie mają dokąd uciekać
	innerEscape := escape
	if positioned || hasTransform {
		innerEscape = nil
	}
	// gopdfgo: element flex lub grid z z-index innym niż auto tworzy kontekst
	// nakładania także bez pozycjonowania (CSS Flexbox 1 § 5.4, CSS Grid 2
	// § 6.4); źródło wymagało position innego niż static
	zIndexApplies := positioned || box.Box().IsFlexItem || box.Box().IsGridItem
	absoluteAndZIndex := zIndexApplies && style.GetZIndex().String != "auto"
	// gopdfgo: pudełko z position: fixed (i sticky) zawsze tworzy kontekst nakładania (CSS Positioned
	// Layout 3 § 3.4, tak jak w przeglądarkach); w źródle tylko z z-index innym niż auto (test WPT
	// fixed-pos-stacking-001)
	if position == "fixed" || position == "sticky" {
		absoluteAndZIndex = true
	}
	if absoluteAndZIndex || style.GetOpacity() < 1 ||
		// "transform: none" gives a "falsy" empty list here
		hasTransform || bo.ClipsOverflow(box) {

		// Pudełka uciekające spod przycięcia rysujemy po kontekście, z którego
		// uciekły, tak jak leżałyby w nim: nad jego treścią.
		var escaped []StackingContext
		if bo.ClipsOverflow(box) && !positioned && !hasTransform && innerEscape == nil {
			innerEscape = &escaped
		}
		// gopdfgo: element pływający, który tylko przycina treść (overflow inne niż visible), nie tworzy
		// kontekstu nakładania (CSS 2.1 § 9.9.1, Appendix E), więc jest rysowany na warstwie elementów
		// pływających, pod pudełkami pozycjonowanymi; w źródle trafiał do kontekstów o z-index 0 i zasłaniał
		// wcześniejsze pudełka pozycjonowane (test WPT max-width-106). Pozycjonowanych potomków rysujemy
		// razem z nim, żeby objęło ich przycięcie.
		// To samo dotyczy bloku w przepływie: rysujemy go w całości (przycięcie obejmuje całą jego treść)
		// na tej samej warstwie, czyli nad tłami bloków i pod pudełkami pozycjonowanymi (test WPT
		// abspos-overflow-011); w CSS jego potomkowie należą do zwykłych warstw rodzica, czego tu nie
		// odtwarzamy (test overflow-scroll-paint-order nadal nie przechodzi).
		if (box.Box().IsFloated() || (bo.BlockLevelT.IsInstance(box) && box.Box().IsInNormalFlow())) &&
			!absoluteAndZIndex && style.GetOpacity() >= 1 && !hasTransform && !positioned {
			*floats = append(*floats, newStackingContextFromBox(box, page, nil, innerEscape))
			*childContexts = append(*childContexts, escaped...)
			return nil
		}
		// This box defines a new stacking context, remove it
		// from the "normal" children list.
		*childContexts = append(*childContexts, newStackingContextFromBox(box, page, nil, innerEscape))
		*childContexts = append(*childContexts, escaped...)
	} else {
		if positioned {
			if style.GetZIndex().String != "auto" {
				panic(fmt.Sprintf("expected auto z-index, got %v", style.GetZIndex()))
			}
			// "Fake" context: sub-contexts will go := range this
			// `childContexts` list.
			// Insert at the position before creating the sub-context.
			index := len(*childContexts)
			insertStackingContext(childContexts, index, newStackingContextFromBox(box, page, childContexts, nil))
		} else if box.Box().IsFloated() || box.Box().IsFlexItem || box.Box().IsGridItem {
			// gopdfgo: elementy flex i grid są rysowane jak pudełka inline-block, czyli każdy w całości,
			// w kolejności dokumentu (CSS Flexbox 1 § 5.4, CSS Grid 1 § 9.5). W źródle były rysowane jak
			// zwykłe bloki (najpierw tła wszystkich, potem treść), więc tekst wcześniejszego elementu
			// przebijał przez tło późniejszego, który na niego zachodził. Trafiają na tę samą warstwę
			// co elementy pływające: nad tła bloków, pod treść liniową.
			*floats = append(*floats, newStackingContextFromBox(box, page, childContexts, innerEscape))
		} else if bo.InlineBlockT.IsInstance(box) || bo.InlineFlexT.IsInstance(box) || bo.InlineGridT.IsInstance(box) {
			// gopdfgo: dodane InlineGridT; rysowanie już je przewidywało, a bez tego kończyło się paniką
			// Have this fake stacking context be part of the "normal"
			// box tree, because we need its position in the middle
			// of a tree of inline boxes.
			return newStackingContextFromBox(box, page, childContexts, innerEscape)
		} else {
			if bo.BlockLevelT.IsInstance(box) {
				blocksIndex := len(*blocks)
				boxBlocksAndCells := newBoxTree()
				box = dispatchChildren(box, page, childContexts, innerEscape, blocks, floats, boxBlocksAndCells)
				*blocks = slices.Insert(*blocks, blocksIndex, box)
				blocksAndCells.set(box, boxBlocksAndCells)
			} else if bo.TableCellT.IsInstance(box) {
				boxBlocksAndCells := newBoxTree()
				box = dispatchChildren(box, page, childContexts, innerEscape, blocks, floats, boxBlocksAndCells)
				blocksAndCells.set(box, boxBlocksAndCells)
			} else {
				box = dispatchChildren(box, page, childContexts, innerEscape, blocks, floats, blocksAndCells)
			}

			return box
		}
	}
	return nil
}

func dispatchChildren(box Box, page *bo.PageBox, childContexts, escape *[]StackingContext,
	blocks *[]Box, floats *[]StackingContext, blocksAndCells BoxTree,
) Box {
	if !bo.ParentT.IsInstance(box) {
		return box
	}

	var newChildren []Box
	for _, child := range box.Box().Children {
		result := dispatch(child, page, childContexts, escape, blocks, floats, blocksAndCells)
		if result != nil {
			newChildren = append(newChildren, result)
		}
	}
	return bo.CopyWithChildren(box, newChildren)
}

func (StackingContext) Type() bo.BoxType { return bo.StackingContextT }
