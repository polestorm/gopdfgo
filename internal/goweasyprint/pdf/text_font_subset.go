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
	headTag = ot.MustNewTag("head")
	nameTag = ot.MustNewTag("name")

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

	var glyfNew, locaNew, postNew, headNew, nameNew []byte
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
		// gopdfgo: podzbiór mieści się zwykle w krótkim formacie loca (2 bajty na glif zamiast 4)
		if isLong {
			if shortLoca, shortGlyf, ok := toShortLoca(loca, glyfNew); ok {
				if head, err := ld.RawTable(headTag); err == nil && len(head) >= 54 {
					loca, glyfNew, isLong = shortLoca, shortGlyf, false
					headNew = append([]byte{}, head...)
					binary.BigEndian.PutUint16(headNew[50:], 0) // indexToLocFormat: krótki
				}
			}
		}
		locaNew = writeLoca(loca, isLong)
		maxGlyphs = len(loca) - 1
	}
	if ld.HasTable(nameTag) {
		if raw, err := ld.RawTable(nameTag); err == nil {
			nameNew = minimalName(raw)
		}
	}
	if ld.HasTable(postTag) {
		raw, err := ld.RawTable(postTag)
		if err != nil {
			return nil, fmt.Errorf("subsetting failed: %s", err)
		}
		postNew = writePost(raw)
	}

	// gopdfgo: przy konturach TrueType font w PDF jest adresowany wyłącznie numerami glifów
	// (CIDFontType2 z Identity-H), więc zostają tylko tabele potrzebne do rysowania (ISO 32000-1
	// § 9.9, tab. 126), a tabele indeksowane numerem glifu są ucinane za ostatnim użytym glifem.
	// Źródło kopiowało w całości hmtx, cmap, GDEF i resztę: podzbiór Noto Sans z kilkudziesięcioma
	// glifami miał 37-44 kB, z czego 28 kB to hmtx i cmap.
	var trimmed map[ot.Tag][]byte
	if locaNew != nil {
		// font o nietypowych tabelach zostaje przy samym podzbiorze konturów
		trimmed, _ = trimGlyphIndexedTables(ld, maxp, maxGlyphs, glyphs)
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
		} else if tag == headTag && headNew != nil {
			table.Content = headNew
		} else if tag == nameTag && nameNew != nil {
			table.Content = nameNew
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

// toShortLoca (gopdfgo) przepisuje glify tak, żeby każdy zaczynał się pod parzystym przesunięciem, i zwraca
// je z przesunięciami do krótkiego formatu loca (przesunięcie/2 w 16 bitach); ok jest false, gdy podzbiór
// jest na to za duży (OpenType, tabela loca). Źródło zostawiało format fontu, a Noto Sans ma format długi:
// w fakturze tabela loca miała 6,6 kB zamiast 3,3 kB.
func toShortLoca(loca []uint32, glyf []byte) ([]uint32, []byte, bool) {
	newLoca := make([]uint32, len(loca))
	out := make([]byte, 0, len(glyf)+len(loca))
	for i := 0; i+1 < len(loca); i++ {
		newLoca[i] = uint32(len(out))
		out = append(out, glyf[loca[i]:loca[i+1]]...)
		if len(out)%2 == 1 {
			out = append(out, 0)
		}
	}
	newLoca[len(loca)-1] = uint32(len(out))
	if len(out) > 0x1FFFE {
		return nil, nil, false
	}
	return newLoca, out, true
}

// minimalName (gopdfgo) zostawia w tabeli name tylko rekordy o numerach 0-6 (prawa autorskie, rodzina,
// odmiana, identyfikator, pełna nazwa, wersja, nazwa PostScript), tak jak podzbiory Chromium. PDF nie
// czyta z niej nic (ISO 32000-1 § 9.9 nie wymaga tej tabeli), a pełna tabela Noto Sans zajmowała w każdym
// foncie ok. 1,5 kB. Zwraca nil, gdy tabela jest nieczytelna (wtedy zostaje oryginał).
func minimalName(raw []byte) []byte {
	if len(raw) < 6 {
		return nil
	}
	count, storage := int(binary.BigEndian.Uint16(raw[2:])), int(binary.BigEndian.Uint16(raw[4:]))
	if 6+12*count > len(raw) || storage > len(raw) {
		return nil
	}
	type record struct {
		head [8]byte // platformID, encodingID, languageID, nameID
		text []byte
	}
	var kept []record
	for i := 0; i < count; i++ {
		r := raw[6+12*i : 18+12*i]
		if binary.BigEndian.Uint16(r[6:]) > 6 {
			continue
		}
		length, offset := int(binary.BigEndian.Uint16(r[8:])), int(binary.BigEndian.Uint16(r[10:]))
		if storage+offset+length > len(raw) {
			return nil
		}
		var rec record
		copy(rec.head[:], r[:8])
		rec.text = raw[storage+offset : storage+offset+length]
		kept = append(kept, rec)
	}
	start := 6 + 12*len(kept)
	out := make([]byte, start)
	binary.BigEndian.PutUint16(out[2:], uint16(len(kept)))
	binary.BigEndian.PutUint16(out[4:], uint16(start))
	for i, rec := range kept {
		r := out[6+12*i:]
		copy(r, rec.head[:])
		binary.BigEndian.PutUint16(r[8:], uint16(len(rec.text)))
		binary.BigEndian.PutUint16(r[10:], uint16(len(out)-start))
		out = append(out, rec.text...)
	}
	return out
}

func writePost(src []byte) []byte {
	// simple copy the header
	out := make([]byte, 32)
	copy(out, src)
	// gopdfgo: tabela post w wersji 3.0, czyli bez nazw glifów. W PDF font jest adresowany numerami
	// glifów, więc nazwy nie są używane, a lista nazw z wersji 2.0 po odrzuceniu glifów miała inną
	// liczbę wpisów niż maxp (walidator veraPDF: "numberOfGlyphs field of 'post' table does not match").
	binary.BigEndian.PutUint32(out, 0x00030000)
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
// gopdfgo: metryki glifów spoza podzbioru (poza glifem 0) są zerowane, jak w podzbiorach Chromium; PDF
// bierze szerokości z /W, a ciąg zer kompresuje się prawie do niczego (w fakturze 1655 wpisów, z czego
// używanych 83).
func trimGlyphIndexedTables(ld *ot.Loader, maxp []byte, numGlyphs int, used glyphSet) (map[ot.Tag][]byte, error) {
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
	for gid := 1; gid < numGlyphs; gid++ {
		if _, ok := used[ot.GID(gid)]; ok {
			continue
		}
		if gid < newMetrics {
			clear(newHmtx[4*gid : 4*gid+4])
		} else {
			at := 4*newMetrics + 2*(gid-newMetrics)
			clear(newHmtx[at : at+2])
		}
	}
	newMaxp := append([]byte{}, maxp...)
	binary.BigEndian.PutUint16(newMaxp[4:], uint16(numGlyphs))
	newHhea := append([]byte{}, hhea...)
	binary.BigEndian.PutUint16(newHhea[34:], uint16(newMetrics))
	return map[ot.Tag][]byte{
		maxpTag: newMaxp, hheaTag: newHhea, hmtxTag: newHmtx,
	}, nil
}
