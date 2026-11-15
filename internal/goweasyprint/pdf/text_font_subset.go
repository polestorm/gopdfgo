package pdf

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
)

// basic font subset

var (
	maxpTag = ot.MustNewTag("maxp")
	locaTag = ot.MustNewTag("loca")
	glyfTag = ot.MustNewTag("glyf")
	postTag = ot.MustNewTag("post")

	fvarTag = ot.MustNewTag("fvar")
	avarTag = ot.MustNewTag("avar")
	mVARTag = ot.MustNewTag("MVAR")
	gvarTag = ot.MustNewTag("gvar")
	hVARTag = ot.MustNewTag("HVAR")
	vVARTag = ot.MustNewTag("VVAR")

	gSUBTag = ot.MustNewTag("GSUB")
	gPOSTag = ot.MustNewTag("GPOS")
)

// several tables are not useful in PDF
func ignoreTable(table ot.Tag) bool {
	switch table {
	case fvarTag, avarTag, mVARTag, gvarTag, hVARTag, vVARTag, gSUBTag, gPOSTag:
		return true
	default:
		return false
	}
}

type glyphSet map[ot.GID]struct{}

func (gs glyphSet) Add(g ot.GID) { gs[g] = struct{}{} }

// recursively fetch composite glyphs deps, adding it to the set
func handleComposite(glyphs glyphSet, glyf tables.Glyf) {
	queue := make([]ot.GID, 0, len(glyphs))
	// start with the given glyphs
	for g := range glyphs {
		queue = append(queue, g)
	}
	for len(queue) != 0 {
		// pop the last glyph
		g := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if int(g) >= len(glyf) { // invalid glyph
			continue
		}
		switch data := glyf[g].Data.(type) {
		case tables.SimpleGlyph:
			// simply add it to the final set
			glyphs.Add(g)
		case tables.CompositeGlyph:
			// fetch deps
			for _, part := range data.Glyphs {
				dep := ot.GID(part.GlyphIndex)
				// if not already seen, add it to the queue
				if _, ok := glyphs[dep]; !ok {
					glyphs.Add(dep)
					queue = append(queue, dep)
				}
				// else : already processed, continue
			}
		default:
			// nil : nothing to do
		}
	}
}

// Variables font are not supported : the variable tables will be dropped
// TODO: For now, [subset] only supports the 'glyf' and 'post' tables
func subset(input ot.Resource, glyphs glyphSet) ([]byte, error) {
	ld, err := ot.NewLoader(input)
	if err != nil {
		return nil, fmt.Errorf("subsetting failed: %s", err)
	}

	headT, _, err := font.LoadHeadTable(ld, nil)
	if err != nil {
		return nil, fmt.Errorf("subsetting failed: %s", err)
	}
	maxp, err := ld.RawTable(maxpTag)
	if err != nil {
		return nil, fmt.Errorf("subsetting failed: %s", err)
	}
	maxpT, _, err := tables.ParseMaxp(maxp)
	if err != nil {
		return nil, fmt.Errorf("subsetting failed: %s", err)
	}

	var glyfNew, locaNew, postNew []byte
	maxGlyphs := 0 // liczba glifów po ucięciu tabeli loca
	if ld.HasTable(locaTag) && ld.HasTable(glyfTag) {
		// load 'locaT' and 'glyf' tables
		locaT, err := ld.RawTable(locaTag)
		if err != nil {
			return nil, fmt.Errorf("subsetting failed: %s", err)
		}

		isLong := headT.IndexToLocFormat == 1

		loca, err := tables.ParseLoca(locaT, int(maxpT.NumGlyphs), isLong)
		if err != nil {
			return nil, fmt.Errorf("subsetting failed: %s", err)
		}
		glyfRaw, err := ld.RawTable(glyfTag)
		if err != nil {
			return nil, fmt.Errorf("subsetting failed: %s", err)
		}

		glyf, err := tables.ParseGlyf(glyfRaw, loca)
		if err != nil {
			return nil, fmt.Errorf("subsetting failed: %s", err)
		}
		// handle composite glyph
		handleComposite(glyphs, glyf)

		loca, glyfNew = subsetGlyf(loca, glyfRaw, glyphs)
		locaNew = writeLoca(loca, isLong)
		maxGlyphs = len(loca) - 1
	}
	if ld.HasTable(postTag) {
		raw, err := ld.RawTable(postTag)
		if err != nil {
			return nil, fmt.Errorf("subsetting failed: %s", err)
		}
		post, _, err := tables.ParsePost(raw)
		if err != nil {
			return nil, fmt.Errorf("subsetting failed: %s", err)
		}
		names := post.Names
		if v20, isV20 := names.(tables.PostNames20); isV20 {
			names = subsetPost20(v20, glyphs)
		}
		postNew = writePost(raw, names)
	}

	// gopdfgo: przy konturach TrueType font w PDF jest adresowany wyłącznie numerami glifów
	// (CIDFontType2 z Identity-H), więc zostają tylko tabele potrzebne do rysowania (ISO 32000-1
	// § 9.9, tab. 126), a tabele indeksowane numerem glifu są ucinane za ostatnim użytym glifem.
	// Źródło kopiowało w całości hmtx, cmap, GDEF i resztę: podzbiór Noto Sans z kilkudziesięcioma
	// glifami miał 37-44 kB, z czego 28 kB to hmtx i cmap.
	var trimmed map[ot.Tag][]byte
	if locaNew != nil {
		// font o nietypowych tabelach zostaje przy samym podzbiorze konturów
		trimmed, _ = trimGlyphIndexedTables(ld, maxp, maxGlyphs)
		if trimmed != nil {
			trimmed[ot.MustNewTag("cmap")] = buildCmap(keptChars(ld, glyphs))
		}
	}

	origTablesTags := ld.Tables()
	tables := make([]ot.Table, 0, len(origTablesTags))
	for _, tag := range origTablesTags {
		if ignoreTable(tag) || (trimmed != nil && !keptInTrueTypeSubset[tag]) {
			continue
		}

		table := ot.Table{Tag: tag}

		if content, ok := trimmed[tag]; ok {
			table.Content = content
		} else if tag == glyfTag && glyfNew != nil {
			table.Content = glyfNew
		} else if tag == locaTag && locaNew != nil {
			table.Content = locaNew
		} else if tag == postTag && postNew != nil {
			table.Content = postNew
		} else {
			table.Content, err = ld.RawTable(tag)
			if err != nil {
				return nil, fmt.Errorf("subsetting failed: %s", err)
			}
		}

		tables = append(tables, table)
	}

	return ot.WriteOpentype(tables, ld.Type), nil
}

// mutate and returns [loca] and [glyph]
func subsetGlyf(loca []uint32, glyf []byte, glyphs glyphSet) ([]uint32, []byte) {
	// trim the unused glyph data, and adjust the offset to have start == end
	currentStart := loca[0]
	nbGlyphs := len(loca) - 1
	maxGlyphUsed := ot.GID(0)
	for gid := ot.GID(0); gid < ot.GID(nbGlyphs); gid++ {
		origStart, origEnd := loca[gid], loca[gid+1]
		// start and end offsets will change, but not the length
		glyphLength := origEnd - origStart

		newStart := currentStart
		var newEnd uint32
		if _, has := glyphs[gid]; has {
			// keep the data
			newEnd = newStart + glyphLength

			if gid > maxGlyphUsed {
				maxGlyphUsed = gid
			}
		} else {
			// set the length to 0
			newEnd = newStart
		}

		// avoid useless copy that will happen until we skip a glyph
		if newStart != origStart {
			copy(glyf[newStart:], glyf[origStart:origEnd])
		}

		loca[gid] = newStart

		// update the offset
		currentStart = newEnd
	}
	// update the final offset
	loca[nbGlyphs] = currentStart
	glyf = glyf[:currentStart]

	// trim the local table
	loca = loca[:maxGlyphUsed+2]

	return loca, glyf
}

// writeLoca performs the reverse operation implemented by [ParseLoca]
func writeLoca(offsets []uint32, isLong bool) []byte {
	if isLong {
		out := make([]byte, 4*len(offsets))
		for i, off := range offsets {
			binary.BigEndian.PutUint32(out[4*i:], off)
		}
		return out
	} else {
		out := make([]byte, 2*len(offsets))
		for i, off := range offsets {
			binary.BigEndian.PutUint16(out[2*i:], uint16(off>>1))
		}
		return out
	}
}

func subsetPost20(names tables.PostNames20, glyphs glyphSet) tables.PostNames20 {
	const numBuiltInPostNames = 258

	out := tables.PostNames20{
		GlyphNameIndexes: make([]uint16, 0, len(glyphs)),
		Strings:          make([]string, 0, len(glyphs)),
	}
	for gid, index := range names.GlyphNameIndexes {
		if _, has := glyphs[ot.GID(gid)]; !has {
			continue // drop this glyph
		}
		// offsets are shifted to handle predefined glyphs
		if index < numBuiltInPostNames { // just copy the builtin index
			out.GlyphNameIndexes = append(out.GlyphNameIndexes, index)
		} else {
			// fetch and copy the name
			index = index - numBuiltInPostNames
			out.Strings = append(out.Strings, names.Strings[index])
			// compute the new index
			newIndex := uint16(len(out.Strings) - 1 + numBuiltInPostNames)
			out.GlyphNameIndexes = append(out.GlyphNameIndexes, newIndex)
		}
	}
	return out
}

func writePost(src []byte, names tables.PostNames) []byte {
	// simple copy the header
	out := make([]byte, 32)
	copy(out, src)
	switch names := names.(type) {
	case tables.PostNames10, tables.PostNames30: // empty
	case tables.PostNames20:
		out = append(out, writePostNames20(names)...)
	}
	return out
}

func writePostNames20(names tables.PostNames20) []byte {
	numG := len(names.GlyphNameIndexes)
	out := make([]byte, 2+2*numG)
	binary.BigEndian.PutUint16(out, uint16(numG))
	for i, index := range names.GlyphNameIndexes {
		binary.BigEndian.PutUint16(out[2+2*i:], index)
	}
	// strings as Pascal encoded
	for _, name := range names.Strings {
		out = append(out, byte(len(name)))
		out = append(out, name...)
	}
	return out
}

// Tabele zostające w podzbiorze fontu o konturach TrueType.
var keptInTrueTypeSubset = map[ot.Tag]bool{
	ot.MustNewTag("head"): true, ot.MustNewTag("hhea"): true, maxpTag: true, ot.MustNewTag("hmtx"): true,
	locaTag: true, glyfTag: true, postTag: true, ot.MustNewTag("name"): true, ot.MustNewTag("OS/2"): true,
	ot.MustNewTag("cvt "): true, ot.MustNewTag("fpgm"): true, ot.MustNewTag("prep"): true,
	ot.MustNewTag("gasp"): true, ot.MustNewTag("cmap"): true,
}

// keptChars zwraca odwzorowanie znak → glif z tabeli cmap fontu, ograniczone do zostających glifów.
func keptChars(ld *ot.Loader, glyphs glyphSet) map[rune]ot.GID {
	out := map[rune]ot.GID{}
	raw, err := ld.RawTable(ot.MustNewTag("cmap"))
	if err != nil {
		return out
	}
	table, _, err := tables.ParseCmap(raw)
	if err != nil {
		return out
	}
	cmap, _, err := font.ProcessCmap(table, 0)
	if err != nil {
		return out
	}
	for it := cmap.Iter(); it.Next(); {
		r, gid := it.Char()
		if _, kept := glyphs[gid]; kept {
			out[r] = gid
		}
	}
	return out
}

// buildCmap składa tabelę cmap z jedną podtabelą formatu 12 (platforma 3, kodowanie 10) dla podanych
// znaków. Glify wybiera się w PDF numerami, więc czytnik zgodny z normą tej tabeli nie potrzebuje, ale
// CoreGraphics (podgląd macOS) rysuje tekst minimalnie inaczej, gdy font jej nie ma albo jest pusta,
// a część czytników odrzuca font TrueType bez cmap. Kolejność grup jest rosnąca po kodzie znaku.
func buildCmap(chars map[rune]ot.GID) []byte {
	codes := make([]rune, 0, len(chars))
	for r := range chars {
		codes = append(codes, r)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	type group struct{ start, end, gid uint32 }
	var groups []group
	for _, r := range codes {
		code, gid := uint32(r), uint32(chars[r])
		if n := len(groups); n != 0 && groups[n-1].end+1 == code && groups[n-1].gid+(code-groups[n-1].start) == gid {
			groups[n-1].end = code
			continue
		}
		groups = append(groups, group{code, code, gid})
	}
	out := make([]byte, 12+16+12*len(groups))
	binary.BigEndian.PutUint16(out[2:], 1)   // jedna podtabela
	binary.BigEndian.PutUint16(out[4:], 3)   // platforma Windows
	binary.BigEndian.PutUint16(out[6:], 10)  // pełny Unicode
	binary.BigEndian.PutUint32(out[8:], 12)  // przesunięcie podtabeli
	binary.BigEndian.PutUint16(out[12:], 12) // format 12
	binary.BigEndian.PutUint32(out[16:], uint32(16+12*len(groups)))
	binary.BigEndian.PutUint32(out[24:], uint32(len(groups)))
	for i, g := range groups {
		at := 28 + 12*i
		binary.BigEndian.PutUint32(out[at:], g.start)
		binary.BigEndian.PutUint32(out[at+4:], g.end)
		binary.BigEndian.PutUint32(out[at+8:], g.gid)
	}
	return out
}

// trimGlyphIndexedTables zwraca nowe maxp, hhea i hmtx dla fontu uciętego do numGlyphs glifów.
func trimGlyphIndexedTables(ld *ot.Loader, maxp []byte, numGlyphs int) (map[ot.Tag][]byte, error) {
	hheaTag, hmtxTag := ot.MustNewTag("hhea"), ot.MustNewTag("hmtx")
	hhea, err := ld.RawTable(hheaTag)
	if err != nil {
		return nil, err
	}
	hmtx, err := ld.RawTable(hmtxTag)
	if err != nil {
		return nil, err
	}
	if len(maxp) < 6 || len(hhea) < 36 || numGlyphs <= 0 || numGlyphs > 0xFFFF {
		return nil, fmt.Errorf("invalid maxp or hhea table")
	}
	oldGlyphs := int(binary.BigEndian.Uint16(maxp[4:]))
	oldMetrics := int(binary.BigEndian.Uint16(hhea[34:]))
	if numGlyphs > oldGlyphs || oldMetrics > oldGlyphs || len(hmtx) < 4*oldMetrics+2*(oldGlyphs-oldMetrics) {
		return nil, fmt.Errorf("inconsistent hmtx table")
	}
	newMetrics := min(oldMetrics, numGlyphs)
	// pełne wpisy (szerokość i odsadka), potem same odsadki glifów o wspólnej szerokości
	newHmtx := append([]byte{}, hmtx[:4*newMetrics]...)
	if numGlyphs > newMetrics {
		newHmtx = append(newHmtx, hmtx[4*oldMetrics:4*oldMetrics+2*(numGlyphs-newMetrics)]...)
	}
	newMaxp := append([]byte{}, maxp...)
	binary.BigEndian.PutUint16(newMaxp[4:], uint16(numGlyphs))
	newHhea := append([]byte{}, hhea...)
	binary.BigEndian.PutUint16(newHhea[34:], uint16(newMetrics))
	return map[ot.Tag][]byte{
		maxpTag: newMaxp, hheaTag: newHhea, hmtxTag: newHmtx,
	}, nil
}
