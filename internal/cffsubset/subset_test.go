package cffsubset

import (
	"bytes"
	"encoding/binary"
	"os"
	"reflect"
	"testing"

	"github.com/go-text/typesetting/font/cff"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
)

const (
	notoOTF   = "../../testdata/fonts/cff/noto/NotoSans-Regular.otf"
	cff2OTF   = "../../testdata/fonts/cff/adobe/SourceSans3VF-Italic.otf"
	notoCIDOT = "../../testdata/fonts/cff/noto/NotoSansCID-Regular.otf"
)

func readFont(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func rawTable(t *testing.T, font []byte, tag string) []byte {
	t.Helper()
	ld, err := ot.NewLoader(bytes.NewReader(font))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := ld.RawTable(ot.MustNewTag(tag))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// charsetOf odczytuje charset podzbioru: CID dla każdego glifu
func charsetOf(t *testing.T, data []byte) []int {
	t.Helper()
	src, err := parseCFF(data, false)
	if err != nil {
		t.Fatal(err)
	}
	if !src.cid {
		t.Fatal("subset is not CID-keyed")
	}
	e, _ := src.topEntry(opCharset)
	off, err := e.int(0)
	if err != nil {
		t.Fatal(err)
	}
	n := len(src.charStrings)
	out := []int{0}
	switch data[off] {
	case 0:
		for i := 1; i < n; i++ {
			out = append(out, int(binary.BigEndian.Uint16(data[off+1+2*(i-1):])))
		}
	case 2:
		p := off + 1
		for len(out) < n {
			first, left := int(binary.BigEndian.Uint16(data[p:])), int(binary.BigEndian.Uint16(data[p+2:]))
			for c := first; c <= first+left; c++ {
				out = append(out, c)
			}
			p += 4
		}
	default:
		t.Fatalf("charset format %d", data[off])
	}
	return out
}

type outlineFunc func(gid int) ([]ot.Segment, error)

func cffOutlines(t *testing.T, raw []byte) outlineFunc {
	t.Helper()
	f, err := cff.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return func(gid int) ([]ot.Segment, error) {
		segs, _, err := f.LoadGlyph(tables.GlyphID(gid))
		return segs, err
	}
}

func cff2Outlines(t *testing.T, raw []byte) outlineFunc {
	t.Helper()
	f, err := cff.ParseCFF2(raw)
	if err != nil {
		t.Fatal(err)
	}
	return func(gid int) ([]ot.Segment, error) {
		segs, _, err := f.LoadGlyph(tables.GlyphID(gid), nil)
		return segs, err
	}
}

// checkSubset sprawdza podzbiór: postać CID-keyed, CID = numer glifu źródła, kontury jak w źródle
func checkSubset(t *testing.T, font []byte, original outlineFunc, gids []uint16) []byte {
	t.Helper()
	out, err := Subset(font, gids, "ABCDEF+Test")
	if err != nil {
		t.Fatal(err)
	}
	charset := charsetOf(t, out)
	want := map[int]bool{0: true}
	for _, g := range gids {
		want[int(g)] = true
	}
	if len(charset) != len(want) {
		t.Fatalf("subset has %d glyphs, want %d", len(charset), len(want))
	}
	subset := cffOutlines(t, out)
	for newGID, cid := range charset {
		if !want[cid] {
			t.Fatalf("unexpected CID %d", cid)
		}
		got, err := subset(newGID)
		if err != nil {
			t.Fatalf("glyph %d: %s", cid, err)
		}
		exp, err := original(cid)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, exp) {
			t.Fatalf("glyph %d: outline differs\n got %v\nwant %v", cid, got, exp)
		}
	}
	return out
}

func allGlyphs(n int) []uint16 {
	out := make([]uint16, n)
	for i := range out {
		out[i] = uint16(i)
	}
	return out
}

func TestSubsetCFF(t *testing.T) {
	font := readFont(t, notoOTF)
	raw := rawTable(t, font, "CFF ")
	orig := cffOutlines(t, raw)
	src, err := parseCFF(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	small := checkSubset(t, font, orig, []uint16{36, 37, 38, 72, 3, 1000})
	if len(small) > 4000 {
		t.Errorf("subset of 6 glyphs has %d bytes", len(small))
	}
	checkSubset(t, font, orig, allGlyphs(len(src.charStrings)))
}

func TestSubsetCFF2(t *testing.T) {
	font := readFont(t, cff2OTF)
	raw := rawTable(t, font, "CFF2")
	orig := cff2Outlines(t, raw)
	src, err := parseCFF(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	checkSubset(t, font, orig, []uint16{1, 2, 3, 50, 100})
	checkSubset(t, font, orig, allGlyphs(len(src.charStrings)))
}

func TestSubsetDeterministic(t *testing.T) {
	font := readFont(t, notoOTF)
	a, err := Subset(font, []uint16{40, 5, 300, 5}, "ABCDEF+Test")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Subset(font, []uint16{300, 40, 5}, "ABCDEF+Test")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("subset depends on glyph order")
	}
}

// Po przepisaniu z CFF2 operator z więcej niż 48 argumentami (z szerokością) dzieli się na kilka;
// blend zostawia wartości domyślne, vsindex znika.
func TestCFF2Charstring(t *testing.T) {
	src := &source{cff2: true, vsRegions: []int{1, 2}}
	var cs []byte
	cs = append(cs, encodeInt(1)...)
	cs = append(cs, 15) // vsindex 1: dwa regiony
	// 10 20 (delty 1 2, 3 4) 2 blend rmoveto
	for _, v := range []int{10, 20, 1, 2, 3, 4, 2} {
		cs = append(cs, encodeInt(v)...)
	}
	cs = append(cs, 16, 21)
	for i := 0; i < 60; i++ {
		cs = append(cs, encodeInt(i%7)...)
	}
	cs = append(cs, 5) // rlineto z 60 argumentami
	w := walker{src: src, flatten: true, out: encodeInt(500), pendingWidth: true,
		usedGlobal: map[int]bool{}, usedLocal: map[int]bool{}}
	if err := w.run(cs, 0); err != nil {
		t.Fatal(err)
	}
	var want []byte
	for _, v := range []int{500, 10, 20} {
		want = append(want, encodeInt(v)...)
	}
	want = append(want, 21)
	for i := 0; i < 48; i++ {
		want = append(want, encodeInt(i%7)...)
	}
	want = append(want, 5)
	for i := 48; i < 60; i++ {
		want = append(want, encodeInt(i%7)...)
	}
	want = append(want, 5, 14)
	if !bytes.Equal(w.out, want) {
		t.Fatalf("got  %v\nwant %v", w.out, want)
	}

	// szerokość zajmuje miejsce na stosie pierwszego operatora: 48 argumentów rlineto + szerokość
	cs = nil
	for i := 0; i < 48; i++ {
		cs = append(cs, encodeInt(1)...)
	}
	cs = append(cs, 5)
	w = walker{src: src, flatten: true, out: encodeInt(500), pendingWidth: true,
		usedGlobal: map[int]bool{}, usedLocal: map[int]bool{}}
	if err := w.run(cs, 0); err != nil {
		t.Fatal(err)
	}
	if ops := bytes.Count(w.out, []byte{5}); ops != 2 {
		t.Fatalf("rlineto: %d operators, want 2 (46 + 2 arguments)", ops)
	}
}

// Font CFF bez FontMatrix ma jednostki z unitsPerEm tabeli head: podzbiór (goły CFF, bez head) dostaje
// jawną macierz 1/unitsPerEm w Top DICT i jednostkową w FD.
func TestSubsetUnitsPerEm(t *testing.T) {
	font := readFont(t, notoOTF)
	ld, err := ot.NewLoader(bytes.NewReader(font))
	if err != nil {
		t.Fatal(err)
	}
	var tabs []ot.Table
	for _, tag := range ld.Tables() {
		content, _ := ld.RawTable(tag)
		if tag == tagHead {
			content = append([]byte(nil), content...)
			binary.BigEndian.PutUint16(content[18:], 2048)
		}
		tabs = append(tabs, ot.Table{Tag: tag, Content: content})
	}
	for _, tc := range []struct {
		font []byte
		want float64
	}{{font, 0.001}, {ot.WriteOpentype(tabs, ld.Type), 1.0 / 2048}} {
		out, err := Subset(tc.font, []uint16{36}, "ABCDEF+Test")
		if err != nil {
			t.Fatal(err)
		}
		src, err := parseCFF(out, false)
		if err != nil {
			t.Fatal(err)
		}
		got := 0.001
		if e, ok := src.topEntry(opFontMatrix); ok {
			got, _ = decodeDictNumber(e.args[0])
			if fd := src.fds[0].matrix; len(fd) != 6 {
				t.Errorf("FD matrix %v, want identity", fd)
			} else if v, _ := decodeDictNumber(fd[0]); v != 1 {
				t.Errorf("FD matrix scale %v, want 1", v)
			}
		}
		if got != tc.want {
			t.Errorf("FontMatrix scale %v, want %v", got, tc.want)
		}
	}
}

// checkOutlines porównuje kontury podzbioru out z konturami źródła
func checkOutlines(t *testing.T, out []byte, original outlineFunc) {
	t.Helper()
	subset := cffOutlines(t, out)
	for newGID, cid := range charsetOf(t, out) {
		got, err := subset(newGID)
		if err != nil {
			t.Fatalf("glyph %d: %s", cid, err)
		}
		exp, _ := original(cid)
		if !reflect.DeepEqual(got, exp) {
			t.Fatalf("glyph %d: outline differs", cid)
		}
	}
}

// TestSubsetModes sprawdza trzy postacie zapisu: bez podprogramów, z podprogramami ograniczonymi do
// użytych i z podprogramami w całości (gdy interpreter nie rozwija charstringów)
func TestSubsetModes(t *testing.T) {
	for _, path := range []string{notoOTF, notoCIDOT} {
		font := readFont(t, path)
		raw := rawTable(t, font, "CFF ")
		orig := cffOutlines(t, raw)
		src, err := parseCFF(raw, false)
		if err != nil {
			t.Fatal(err)
		}
		all := make([]int, len(src.charStrings))
		for i := range all {
			all[i] = i
		}
		for _, keep := range [][]int{{0, 3, 36, 37, 72, 500}, all} {
			usedGlobal := map[int]bool{}
			usedLocal := make([]map[int]bool, len(src.fds))
			for i := range usedLocal {
				usedLocal[i] = map[int]bool{}
			}
			flat := make([][]byte, len(keep))
			for i, g := range keep {
				fd := src.fdOf(g)
				w := walker{src: src, local: src.fds[fd].subrs, usedGlobal: usedGlobal, usedLocal: usedLocal[fd],
					flatten: true}
				if err := w.run(src.charStrings[g], 0); err != nil {
					t.Fatal(err)
				}
				flat[i] = w.out
			}
			ld, _ := ot.NewLoader(bytes.NewReader(font))
			flatOut := writeSubset(src, keep, keep, flat, nil, nil, "ABCDEF+Test", ld)
			pruned := writeSubset(src, keep, keep, nil, usedGlobal, usedLocal, "ABCDEF+Test", ld)
			whole := writeSubset(src, keep, keep, nil, nil, nil, "ABCDEF+Test", ld)
			t.Logf("%s, %d glyphs: source %d B, flat %d B, pruned %d B, all subrs %d B",
				path, len(keep), len(raw), len(flatOut), len(pruned), len(whole))
			for _, out := range [][]byte{flatOut, pruned, whole} {
				checkOutlines(t, out, orig)
			}
		}
	}
}
