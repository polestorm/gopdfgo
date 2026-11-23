package properties

import (
	"fmt"
	"iter"
	"math"
	"math/bits"

	"github.com/polestorm/gopdfgo/internal/webrender/css/parser"
)

var Inf = Float(math.Inf(+1))

// --------------- Values  -----------------------------------------------

func (d Dimension) ToPixels() Dimension {
	if c, ok := LengthsToPixels[d.Unit]; ok {
		return Dimension{Unit: Px, Value: d.Value * c}
	}
	return d
}

func (p Point) ToPixels() Point {
	return Point{p[0].ToPixels(), p[1].ToPixels()}
}

// FToPx returns `f` as pixels.
func FToPx(f Float) TaggedDim      { return TaggedDim{Dimension: Dimension{Value: f, Unit: Px}} }
func FToD(f Fl) Dimension          { return Dimension{Value: Float(f), Unit: Scalar} }
func PercToD(f Fl) Dimension       { return Dimension{Value: Float(f), Unit: Perc} }
func PercToV(f Fl) TaggedDim       { return TaggedDim{Dimension: PercToD(f)} }
func FToV(f Fl) TaggedDim          { return TaggedDim{Dimension: FToD(f)} }
func (f Float) ToValue() TaggedDim { return FToV(Fl(f)) }
func TagToV(t Keyword) TaggedDim   { return TaggedDim{Tag: t} }

func (v TaggedDim) ToMaybeFloat() MaybeFloat {
	if v.Tag == Auto {
		return AutoF
	}
	return v.Value
}

// HasTablePrefix returns true for all keywords starting
// by "table-"
func (kw Keyword) HasTablePrefix() bool {
	return TableCaption <= kw && kw <= TableRowGroup
}

func NewColor(r, g, b, a Fl) Color {
	return Color{RGBA: parser.RGBA{R: r, G: g, B: b, A: a}, Type: parser.ColorRGBA}
}

// ResolveCurrentColor (gopdfgo) zwraca kolor z currentColor zastąpionym kolorem elementu [current];
// liczy też zależne od currentColor barwy względne i color-mix() (CSS Color 5 § 3, § 4).
func (c Color) ResolveCurrentColor(current Color) Color {
	if c.Type != parser.ColorCurrentColor {
		return c
	}
	return Color(parser.Color(c).Resolve(current.RGBA))
}

// Has returns `true` is v is one of the three elements.
func (d Display) Has(v Keyword) bool {
	return d.Inside == v || d.Outside == v || d.ListItem == v
}

const (
	True  = Bool(true)
	False = Bool(false)
)

type Bool bool

func (Bool) isMaybeBool() {}

// MaybeBool stores a tree state boolean : true, false or nil
type MaybeBool interface {
	isMaybeBool()
}

// ----------------- misc  ---------------------------------------

func (p Point) ToSlice() []Dimension {
	return []Dimension{p[0], p[1]}
}

func (s GradientSize) IsExplicit() bool {
	return s.Keyword == ""
}

// -------------- Images ------------------------

func (i NoneImage) isImage()      {}
func (i UrlImage) isImage()       {}
func (i LinearGradient) isImage() {}
func (i RadialGradient) isImage() {}
func (i ConicGradient) isImage()  {}

// -------------------------- Content Property --------------------------
// func (i NoneImage) copyAsInnerContent() InnerContent      { return i }
// func (i UrlImage) copyAsInnerContent() InnerContent       { return i }
// func (i LinearGradient) copyAsInnerContent() InnerContent { return i.copy() }
// func (i RadialGradient) copyAsInnerContent() InnerContent { return i.copy() }

// contents,
func (s String) isInnerContent()  {}
func (s Strings) isInnerContent() {}

func (s Counters) isInnerContent() {}

// target
func (s SContentProps) isInnerContent() {}

// url
func (s TaggedString) isInnerContent() {}

func (s Dimension) isInnerContent() {}
func (s Float) isInnerContent()     {}
func (s Int) isInnerContent()       {}
func (s Color) isInnerContent()     {}
func (s Quote) isInnerContent()     {}

// ContentImage (gopdfgo) to obraz inny niż url() w content (gradient, image-set();
// CSS Content 3 § 1: <image>), element listy typu "image".
type ContentImage struct{ Image Image }

func (s ContentImage) isInnerContent() {}
func (s AttrData) isInnerContent()     {}
func (s VarData) isInnerContent()      {}

func (c ContentProperty) AsString() (value string) {
	return string(c.Content.(String))
}

func (c ContentProperty) AsLeader() string {
	return c.Content.(Strings)[1]
}

func (c ContentProperty) AsCounter() (counterName string, counterStyle CounterStyleID) {
	value := c.Content.(Counters)
	return value.Name, value.Style
}

func (c ContentProperty) AsCounters() (counterName, separator string, counterStyle CounterStyleID) {
	value := c.Content.(Counters)
	return value.Name, value.Separator, value.Style
}

func (c ContentProperty) AsStrings() []string {
	value, ok := c.Content.(Strings)
	if !ok {
		panic(fmt.Sprintf("invalid content (expected []string): %v", c.Content))
	}
	return value
}

func (c ContentProperty) AsTargetCounter() (anchorToken ContentProperty, counterName, counterStyle string) {
	value, _ := c.Content.(SContentProps)
	if len(value) != 3 {
		panic(fmt.Sprintf("invalid content (expected 3-list of String or ContentProperty): %v", c.Content))
	}
	return value[0].ContentProperty, value[1].String, value[2].String
}

func (c ContentProperty) AsTargetCounters() (anchorToken ContentProperty, counterName string, separator ContentProperty, counterStyle string) {
	value, _ := c.Content.(SContentProps)
	if len(value) != 4 {
		panic(fmt.Sprintf("invalid content (expected 4-list of String or ContentProperty): %v", c.Content))
	}
	return value[0].ContentProperty, value[1].String, value[2].ContentProperty, value[3].String
}

func (c ContentProperty) AsTargetText() (anchorToken ContentProperty, textStyle string) {
	value, _ := c.Content.(SContentProps)
	if len(value) != 2 {
		panic(fmt.Sprintf("invalid content (expected 2-list of String or ContentProperty): %v", c.Content))
	}
	return value[0].ContentProperty, value[1].String
}

func (c ContentProperty) AsQuote() Quote {
	return c.Content.(Quote)
}

// ------------------------- Usefull for test ---------------------------

func (bs Values) Repeat(n int) Values {
	var out Values
	for i := 0; i < n; i++ {
		out = append(out, bs...)
	}
	return out
}

func (bs Images) Repeat(n int) CssProperty {
	var out Images
	for i := 0; i < n; i++ {
		out = append(out, bs...)
	}
	return out
}

func (bs Centers) Repeat(n int) CssProperty {
	var out Centers
	for i := 0; i < n; i++ {
		out = append(out, bs...)
	}
	return out
}

func (bs Sizes) Repeat(n int) CssProperty {
	var out Sizes
	for i := 0; i < n; i++ {
		out = append(out, bs...)
	}
	return out
}

func (bs Repeats) Repeat(n int) CssProperty {
	var out Repeats
	for i := 0; i < n; i++ {
		out = append(out, bs...)
	}
	return out
}

func (bs Strings) Repeat(n int) CssProperty {
	var out Strings
	for i := 0; i < n; i++ {
		out = append(out, bs...)
	}
	return out
}

type (
	SContents  []SContent
	Dimensions []Dimension
)

// --------------- Geometry -------------------------

type Rectangle [4]Float

func (r Rectangle) ToFloat() [4]Fl {
	return [4]Fl{Fl(r[0]), Fl(r[1]), Fl(r[2]), Fl(r[3])}
}

func (r Rectangle) Unpack() (x, y, w, h Fl) {
	o := r.ToFloat()
	return o[0], o[1], o[2], o[3]
}

func (r Rectangle) Unpack2() (x, y, w, h Float) {
	return r[0], r[1], r[2], r[3]
}

func (r Rectangle) IsNone() bool {
	return r == Rectangle{}
}

// SetK to zbiór właściwości znanych.
//
// gopdfgo: mapa zastąpiona tablicą bitów (KnownProp to uint8). Has wołane przy każdym odczycie
// wartości stylu (Inherited, InitialNotComputed) było ok. 7% czasu dokumentu losowego. Zbiór
// przechodzimy (All) w kolejności rosnącej, a nie losowej jak mapę.
type SetK struct {
	bits *[4]uint64
}

func (s SetK) Add(key KnownProp) {
	s.bits[key>>6] |= 1 << (key & 63)
}

func (s SetK) Extend(keys []KnownProp) {
	for _, key := range keys {
		s.Add(key)
	}
}

func (s SetK) Has(key KnownProp) bool {
	return s.bits != nil && s.bits[key>>6]&(1<<(key&63)) != 0
}

// Len zwraca liczbę właściwości w zbiorze.
func (s SetK) Len() int {
	if s.bits == nil {
		return 0
	}
	n := 0
	for _, w := range s.bits {
		n += bits.OnesCount64(w)
	}
	return n
}

// All przechodzi właściwości zbioru w kolejności rosnącej.
func (s SetK) All() iter.Seq[KnownProp] {
	return func(yield func(KnownProp) bool) {
		if s.bits == nil {
			return
		}
		for i := 0; i < 256; i++ {
			if s.bits[i>>6]&(1<<(i&63)) != 0 && !yield(KnownProp(i)) {
				return
			}
		}
	}
}

// Copy returns a deepcopy.
func (s SetK) Copy() SetK {
	if s.bits == nil {
		return SetK{}
	}
	b := *s.bits
	return SetK{bits: &b}
}

func (s SetK) IsNone() bool { return s.bits == nil }

func (s SetK) Equal(other SetK) bool {
	if s.bits == nil || other.bits == nil {
		return s.Len() == other.Len()
	}
	return *s.bits == *other.bits
}

func NewSetK(values ...KnownProp) SetK {
	s := SetK{bits: new([4]uint64)}
	for _, v := range values {
		s.Add(v)
	}
	return s
}
