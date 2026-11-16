package pdf

import (
	"bytes"
	_ "embed"
	"os"
	"reflect"
	"testing"

	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
)

func gs(gids ...ot.GID) glyphSet {
	out := make(glyphSet)
	for _, gid := range gids {
		out.Add(gid)
	}
	return out
}

func comp(deps ...ot.GID) tables.Glyph {
	var parts []tables.CompositeGlyphPart
	for _, dep := range deps {
		parts = append(parts, tables.CompositeGlyphPart{GlyphIndex: uint16(dep)})
	}
	return tables.Glyph{
		Data: tables.CompositeGlyph{
			Glyphs: parts,
		},
	}
}

func Test_handleComposite(t *testing.T) {
	sg := tables.Glyph{Data: tables.SimpleGlyph{}}
	tests := []struct {
		glyphs glyphSet
		glyf   tables.Glyf
		want   glyphSet
	}{
		{glyphSet{}, nil, glyphSet{}},
		{ // only simple glyphs
			gs(0, 1, 2, 3, 4),
			tables.Glyf{tables.Glyph{}, sg, sg, sg},
			gs(0, 1, 2, 3, 4),
		},
		{
			gs(0, 1),
			tables.Glyf{comp(2, 3), sg, sg, sg},
			gs(0, 1, 2, 3),
		},
		{
			gs(0),
			tables.Glyf{comp(1), comp(2, 3), sg, sg},
			gs(0, 1, 2, 3),
		},
		{ // with cycle
			gs(0),
			tables.Glyf{comp(0), sg, sg, sg},
			gs(0),
		},
	}
	for _, tt := range tests {
		handleComposite(tt.glyphs, tt.glyf)
		if !reflect.DeepEqual(tt.glyphs, tt.want) {
			t.Fatalf("expected %v, got %v", tt.want, tt.glyphs)
		}
	}
}

// extracted from /usr/share/fonts/truetype/dejavu/DejaVuSans.ttf
//
//go:embed test/table_post_20.bin
var post20 []byte

// gopdfgo: podzbiór zapisuje tabelę post w wersji 3.0, bez nazw glifów (zob. writePost); źródło
// sprawdzało tu przepisanie nazw z wersji 2.0, którego już nie ma.
func TestSubsetPost20(t *testing.T) {
	table, _, err := tables.ParsePost(post20)
	if err != nil {
		t.Fatal(err)
	}
	if numG := len(table.Names.(tables.PostNames20).GlyphNameIndexes); numG != 6253 {
		t.Fatal()
	}
	final := writePost(post20)
	if len(final) != 32 || !bytes.Equal(final[4:], post20[4:32]) {
		t.Fatal("nagłówek tabeli post nie został zachowany")
	}
	table, _, err = tables.ParsePost(final)
	if err != nil {
		t.Fatal(err)
	}
	if _, isV30 := table.Names.(tables.PostNames30); !isV30 {
		t.Fatalf("oczekiwana tabela post 3.0, jest %T", table.Names)
	}
}

func TestSubsetWeasyprintFont(t *testing.T) {
	f, err := os.Open("../resources_test/weasyprint.otf")
	if err != nil {
		t.Fatal(err)
	}
	content, err := subset(f, glyphSet{1: struct{}{}, 207: struct{}{}, 208: struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	font, err := ot.NewLoader(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if font.Type != ot.OpenType {
		t.Fatal("unexpected subset flavor")
	}
}
