package pdf

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
	"github.com/polestorm/gopdfgo/internal/cffsubset"
	"github.com/polestorm/gopdfgo/internal/pdflib/contentstream"
	pdfFonts "github.com/polestorm/gopdfgo/internal/pdflib/fonts"
	"github.com/polestorm/gopdfgo/internal/pdflib/model"
	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/matrix"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	drawText "github.com/polestorm/gopdfgo/internal/webrender/text/draw"
)

type pdfFont struct {
	*backend.FontChars
	*model.FontDict
}

func (g *group) SetTextPaint(op backend.PaintOp) {
	doFill := op&(backend.FillEvenOdd|backend.FillNonZero) != 0
	doStroke := op&(backend.Stroke) != 0

	var tr uint8
	if doFill && doStroke {
		tr = 2
	} else if doFill {
		tr = 0
	} else if doStroke {
		tr = 1
	} else {
		tr = 3
	}
	g.stream.Ops(contentstream.OpSetTextRender{Render: tr})
}

// DrawText draws the given text using the current fill color.
func (g *group) DrawText(texts []backend.TextDrawing) {
	// gopdfgo: tekst w całości poza widocznym wycinkiem nie trafia do pliku (ADR 0041, cull.go)
	g.texts += len(texts)
	if texts = g.culledTexts(texts); len(texts) == 0 {
		return
	}
	// gopdfgo: treść oznaczona wokół obiektu tekstowego; w nim q i Q (emoji) nie zamykają ciągu
	if m := g.marking; m != nil {
		g.beginContent()
		m.hold++
		defer func() { m.hold-- }()
	}
	g.stream.BeginText()
	defer g.stream.EndText()

	for _, text := range texts {
		mat := text.Matrix()
		g.stream.SetTextMatrix(mat.A, mat.B, mat.C, mat.D, mat.E, mat.F)
		var rise fl // gopdfgo: bieżące wzniesienie tekstu, w tysięcznych firetu
		// gopdfgo: syntetyczne pochylenie i pogrubienie (zob. backend.TextRun.Skew): pochylenie to macierz
		// tekstu ze ścięciem ustawiana na początku przebiegu, więc pozycję pióra w przestrzeni tekstu
		// liczymy tak, jak przesuwa je operator TJ; pogrubienie to tryb wypełnienia z obrysem (Tr 2,
		// ISO 32000-1 § 9.3.6)
		var (
			pen            fl
			skewed, bolded bool
		)

		for _, run := range text.Runs {
			pf := g.fonts[run.Font]
			// do not use bitmap fonts
			useFont := g.cache.fontFiles[run.Font.Origin()].isSupported

			size := text.FontSize
			if run.SizeScale != 0 { // gopdfgo: zob. backend.TextRun.SizeScale
				size *= run.SizeScale
			}
			if run.Skew != 0 || skewed {
				m := text.Matrix()
				m.RightMultBy(matrix.New(1, 0, run.Skew, 1, pen, 0))
				g.stream.SetTextMatrix(m.A, m.B, m.C, m.D, m.E, m.F)
				skewed = run.Skew != 0
			}
			if run.BoldStroke != 0 {
				g.stream.Ops(contentstream.OpSetLineWidth{W: run.BoldStroke}, contentstream.OpSetTextRender{Render: 2})
				bolded = true
			} else if bolded {
				g.stream.Ops(contentstream.OpSetTextRender{Render: 0})
				bolded = false
			}
			if useFont {
				g.stream.SetFontAndSize(pdfFonts.BuiltFont{Meta: pf.FontDict}, size)
			}
			if rise != 0 { // wzniesienie jest w jednostkach rozmiaru przebiegu: ustawiane od nowa
				rise = fl(math.NaN())
			}

			var out []contentstream.SpacedGlyph
			for _, posGlyph := range run.Glyphs {
				// gopdfgo: przesunięcie pionowe glifu (YOffset z kształtowania: znaki diakrytyczne
				// pozycjonowane przez GPOS, syntetyczny indeks górny i dolny) jako wzniesienie tekstu
				// (ISO 32000-1 § 9.3.7). Źródło je pomijało i wszystkie glify stały na linii bazowej.
				if posGlyph.Rise != rise && useFont {
					if len(out) != 0 {
						g.stream.Ops(contentstream.OpShowSpaceGlyph{Glyphs: out})
						out = out[:0:0]
					}
					rise = posGlyph.Rise
					g.stream.Ops(contentstream.OpSetTextRise{Rise: rise * size / 1000})
				}
				out = append(out, contentstream.SpacedGlyph{
					SpaceSubtractedBefore: -int(posGlyph.Offset),
					GID:                   posGlyph.Glyph,
					SpaceSubtractedAfter:  posGlyph.Kerning,
				})
				width, has := pf.Extents[posGlyph.Glyph]
				if !has {
					width.Width = 1000 // szerokość domyślna DW (ISO 32000-1 § 9.7.4.3)
				}
				pen += (fl(int(posGlyph.Offset)) + fl(width.Width) - fl(posGlyph.Kerning)) * size / 1000

				// PDF readers don't support colored bitmap glyphs
				// so we have to add them as an image
				drawText.DrawEmoji(run.Font, posGlyph.Glyph, pf.Extents[posGlyph.Glyph],
					text.FontSize, text.X, text.Y, posGlyph.XAdvance, g)
			}

			if useFont {
				g.stream.Ops(contentstream.OpShowSpaceGlyph{Glyphs: out})
			}
		}
		if rise != 0 {
			g.stream.Ops(contentstream.OpSetTextRise{Rise: 0})
		}
		if bolded {
			g.stream.Ops(contentstream.OpSetTextRender{Render: 0})
		}
	}
}

func (f pdfFont) newFontDescriptor(font backend.Font, content *model.FontFile) model.FontDescriptor {
	desc := font.Description()

	hash_ := md5.Sum(fmt.Append(nil, desc.Family, desc.Style, desc.Weight, desc.Size))
	// gopdfgo: znacznik podzbioru to sześć wielkich liter (ISO 32000-1 § 9.6.4); źródło wstawiało
	// 32 znaki szesnastkowe, przez co czytniki nie rozpoznawały fontu jako podzbioru
	var tag [6]byte
	for i := range tag {
		tag[i] = 'A' + hash_[i]%26
	}
	hash := string(tag[:])

	flags := model.Symbolic // since we use a custom char set
	if desc.Style != text.FSty_Normal {
		flags |= model.Italic
	}
	if strings.Contains(desc.Family, "Serif") {
		flags |= model.Serif
	}
	if f.FontChars.IsFixedPitch() {
		flags |= model.FixedPitch
	}

	bbox := f.FontChars.Bbox
	return model.FontDescriptor{
		FontName:    model.ObjName(hash + "+" + strings.ReplaceAll(desc.Family, " ", "")),
		FontFamily:  desc.Family,
		Flags:       flags,
		FontBBox:    model.Rectangle{Llx: fl(bbox[0]), Lly: fl(bbox[1]), Urx: fl(bbox[2]), Ury: fl(bbox[3])},
		ItalicAngle: 0,
		Ascent:      desc.Ascent,
		Descent:     desc.Descent,
		CapHeight:   fl(bbox[3]),
		StemV:       80,
		StemH:       80,
		FontFile:    content,
	}
}

// AddFont register a new font to be used in the output and return
// an object used to store associated metadata.
// This method will be called several times with the same `face` argument,
// so caching is advised.
func (g *group) AddFont(font backend.Font, content []byte) *backend.FontChars {
	// check the cache
	if ft, has := g.fonts[font]; has {
		return ft.FontChars
	}
	out := &backend.FontChars{
		Cmap:    make(map[backend.GID][]rune),
		Extents: make(map[backend.GID]backend.GlyphExtents),
	}
	// we only initialize the FontDict pointer,
	// which will be filled later in `writeFonts`
	g.fonts[font] = pdfFont{
		FontChars: out,
		FontDict:  &model.FontDict{},
	}

	origin := font.Origin()
	// until then, we store the content
	if _, ok := g.fontFiles[origin]; !ok {
		g.fontFiles[origin] = fontContent{
			content:     content,
			isSupported: isSupportedFont(content),
			cff:         cffsubset.FontKind(content),
		}
	}

	return out
}

func cidWidths(dict map[backend.GID]backend.GlyphExtents) []model.CIDWidth {
	var (
		widths       []model.CIDWidth
		keys         = make([]backend.GID, 0, len(dict))
		currentBlock model.CIDWidthArray
	)

	for gid := range dict {
		keys = append(keys, gid)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	for _, gid := range keys {
		if _, has := dict[gid-1]; !has {
			// end the current block
			if len(currentBlock.W) != 0 {
				widths = append(widths, currentBlock)
			}
			currentBlock = model.CIDWidthArray{Start: model.CID(gid)}
		}
		currentBlock.W = append(currentBlock.W, model.Fl(dict[gid].Width))
	}

	if len(currentBlock.W) != 0 {
		widths = append(widths, currentBlock)
	}

	return widths
}

// returns true if a valid 'glyf' , 'cff ' 'or'  tables is present
func isSupportedFont(content []byte) bool {
	ld, err := ot.NewLoader(bytes.NewReader(content))
	if err != nil {
		return false
	}

	headT, _, err := font.LoadHeadTable(ld, nil)
	if err != nil {
		return false
	}
	maxp, err := ld.RawTable(maxpTag)
	if err != nil {
		return false
	}
	maxpT, _, err := tables.ParseMaxp(maxp)
	if err != nil {
		return false
	}

	var hasValidGlyf bool
	if ld.HasTable(locaTag) && ld.HasTable(glyfTag) {
		// load 'locaT' and 'glyf' tables
		locaT, _ := ld.RawTable(locaTag)

		isLong := headT.IndexToLocFormat == 1
		loca, _ := tables.ParseLoca(locaT, int(maxpT.NumGlyphs), isLong)
		glyfRaw, _ := ld.RawTable(glyfTag)
		glyf, _ := tables.ParseGlyf(glyfRaw, loca)
		hasValidGlyf = len(glyf) > 0
	}
	hasCff := ld.HasTable(ot.MustNewTag("CFF "))
	// gopdfgo: kontury CFF2 (fonty zmienne OTF) są przepisywane na CFF (ADR 0040); źródło pomijało taki
	// font, a tekst nim złożony znikał z PDF bez ostrzeżenia
	hasCff = hasCff || ld.HasTable(ot.MustNewTag("CFF2"))

	hasBitmap := ld.HasTable(ot.MustNewTag("EBDT")) && ld.HasTable(ot.MustNewTag("EBLC"))

	return hasValidGlyf || hasCff || hasBitmap
}

func newFontFile(fontDesc backend.FontDescription, font pdfFont, content []byte) *model.FontFile {
	fs := &model.FontFile{}
	if fontDesc.IsOpentype {
		// subset the font
		set := glyphSet{}
		for gid := range font.Cmap {
			set.Add(ot.GID(gid))
		}
		contentS, err := subset(bytes.NewReader(content), set)
		if err != nil {
			log.Printf("font subsetting failed: %s", err)
		} else {
			content = contentS
		}

		if fontDesc.IsOpentypeOpentype {
			fs.Subtype = "OpenType"
		} else {
			fs.Length1 = len(content)
		}
	}
	fs.Stream = newStream(content)
	return fs
}

// TODO: see https://github.com/polestorm/gopdfgo/internal/webrender/issues/3
// // https://docs.microsoft.com/typography/opentype/spec/ebdt
// func buildBitmapFontDictionary( pdf, font, widths,
//                                   compress_pdf, subset) (font_dictionary model.FontType3) {
//     font_dictionary.FontBBox = model.Rectangle{0, 0, 1, 1}
//     font_dictionary.FontMatrix = model.Matrix{1, 0, 0, 1, 0, 0}
//     if subset{
//         chars = tuple(sorted(font.cmap))
//    } else{
//         chars = tuple([]int{256})}
//     first, last = chars[0], chars[-1]
//     font_dictionary.FirstChar = first
//     // font_dictionary.LastChar = last
//     var differences []int
//     // for index, index_widths in zip(widths[::2], widths[1::2]):
//     //     differences.append(index)
//     //     for i in range(len(index_widths)):
//     //         if i + index in chars:
//     //             differences.append(f'/{i + index}')
//     // font_dictionary.Encoding = pydyf.Dictionary({        'Type': '/Encoding',        'Differences': pydyf.Array(differences),    })
//     // char_procs = pydyf.Dictionary({})
//     font_glyphs = font.ttfont["EBDT"].strikeData[0]
//     widths := make([]int,   (last - first + 1))
//     glyphs_info = map[int]int{}
//     for key, glyph := range font_glyphs {
//         glyph_format = glyph.getFormat()
//         glyph_id = font.ttfont.getGlyphID(key)

//         // Get and store glyph metrics
//         if glyph_format == 5{
//             data = glyph.data
//             subtables = font.ttfont["EBLC"].strikes[0].indexSubTables
//             for subtable in subtables:
//                 first_index = subtable.firstGlyphIndex
//                 last_index = subtable.lastGlyphIndex
//                 if first_index <= glyph_id <= last_index:
//                     height = subtable.metrics.height
//                     advance = width = subtable.metrics.width
//                     bearing_x = subtable.metrics.horiBearingX
//                     bearing_y = subtable.metrics.horiBearingY
//                     break
//             else:
//                 LOGGER.warning(f"Unknown bitmap metrics for glyph: {glyph_id}")
//                 continue
//         }else{
//             data_start = 5 if glyph_format in (1, 2, 8) else 8
//             data = glyph.data[data_start:]
//             height, width = glyph.data[0:2]
//             bearing_x = int.from_bytes(glyph.data[2:3], "big", signed=True)
//             bearing_y = int.from_bytes(glyph.data[3:4], "big", signed=True)
//             advance = glyph.data[4]}
//         position_y = bearing_y - height
//         if glyph_id in chars{
//             widths[glyph_id - first] = advance}
//         stride = ceil(width / 8)
//         glyph_info = glyphs_info[glyph_id] = {
//             "width": width,
//             "height": height,
//             "x": bearing_x,
//             "y": position_y,
//             "stride": stride,
//             "bitmap": None,
//             "subglyphs": None,
//         }

//         // Decode bitmaps
//         if 0 in (width, height) or not data{
//             glyph_info["bitmap"] = b""}
//         elif glyph_format in (1, 6):
//             glyph_info["bitmap"] = data
//         elif glyph_format in (2, 5, 7):
//             padding = (8 - (width % 8)) % 8
//             bits = bin(int(data.hex(), 16))[2:]
//             bits = bits.zfill(8 * len(data))
//             bitmap_bits = ''.join(
//                 bits[i * width:(i + 1) * width] + padding * '0'
//                 for i in range(height))
//             glyph_info['bitmap'] = int(bitmap_bits, 2).to_bytes(
//                 height * stride, 'big')
//         elif glyph_format in (8, 9):
//             subglyphs = glyph_info['subglyphs'] = []
//             i = 0 if glyph_format == 9 else 1
//             number_of_components = int.from_bytes(data[i:i+2], 'big')
//             for j in range(number_of_components):
//                 index = (i + 2) + (j * 4)
//                 subglyph_id = int.from_bytes(data[index:index+2], 'big')
//                 x = int.from_bytes(data[index+2:index+3], 'big', signed=True)
//                 y = int.from_bytes(data[index+3:index+4], 'big', signed=True)
//                 subglyphs.append({'id': subglyph_id, 'x': x, 'y': y})
//         else:  // pragma: no cover
//             LOGGER.warning(f'Unsupported bitmap glyph format: {glyph_format}')
//             glyph_info['bitmap'] = bytes(height * stride)
// 			}
//     for glyph_id, glyph_info in glyphs_info.items():
//         // Don’t store glyph not in cmap
//         if glyph_id not in chars:
//             continue

//         // Draw glyph
//         stride = glyph_info['stride']
//         width = glyph_info['width']
//         height = glyph_info['height']
//         x = glyph_info['x']
//         y = glyph_info['y']
//         if glyph_info['bitmap'] is None:
//             length = height * stride
//             bitmap_int = int.from_bytes(bytes(length), 'big')
//             for subglyph in glyph_info['subglyphs']:
//                 sub_x = subglyph['x']
//                 sub_y = subglyph['y']
//                 sub_id = subglyph['id']
//                 if sub_id not in glyphs_info:
//                     LOGGER.warning(f'Unknown subglyph: {sub_id}')
//                     continue
//                 subglyph = glyphs_info[sub_id]
//                 if subglyph['bitmap'] is None:
//                     // TODO: support subglyph in subglyph
//                     LOGGER.warning(
//                         f'Unsupported subglyph in subglyph: {sub_id}')
//                     continue
//                 for row_y in range(subglyph['height']):
//                     row_slice = slice(
//                         row_y * subglyph['stride'],
//                         (row_y + 1) * subglyph['stride'])
//                     row = subglyph['bitmap'][row_slice]
//                     row_int = int.from_bytes(row, 'big')
//                     shift = stride * 8 * (height - sub_y - row_y - 1)
//                     stride_difference = stride - subglyph['stride']
//                     if stride_difference > 0:
//                         row_int <<= stride_difference * 8
//                     elif stride_difference < 0:
//                         row_int >>= -stride_difference * 8
//                     if sub_x > 0:
//                         row_int >>= sub_x
//                     elif sub_x < 0:
//                         row_int <<= -sub_x
//                     row_int %= 1 << stride * 8
//                     row_int <<= shift
//                     bitmap_int |= row_int
//             bitmap = bitmap_int.to_bytes(length, 'big')
//         else:
//             bitmap = glyph_info['bitmap']
//         bitmap_stream = pydyf.Stream([
//             b'0 0 d0',
//             f'{width} 0 0 {height} {x} {y} cm'.encode(),
//             b'BI',
//             b'/IM true',
//             b'/W', width,
//             b'/H', height,
//             b'/BPC 1',
//             b'/D [1 0]',
//             b'ID', bitmap, b'EI'
//         ], compress=compress_pdf)
//         pdf.add_object(bitmap_stream)
//         char_procs[glyph_id] = bitmap_stream.reference

//     pdf.add_object(char_procs)
//     font_dictionary['Widths'] = pydyf.Array(widths)
//     font_dictionary['CharProcs'] = char_procs.reference
// 		}

// post-process the font used
// GlyphConflicts (gopdfgo) zwraca łączną liczbę glifów rysowanych ze znakami innymi niż te, które glif ma
// już w ToUnicode (backend.FontChars.Conflicts).
func (c *Output) GlyphConflicts() int {
	n := 0
	for _, font := range c.cache.fonts {
		n += font.Conflicts
	}
	return n
}

func (c *Output) writeFonts() {
	for bFont, font := range c.cache.fonts {
		// gopdfgo: font bez żadnego rozpoznanego glifu (sam znak zastępczy, np. litera spoza podanych
		// fontów) też musi dostać słownik; w źródle był pomijany, a strona nadal się do niego
		// odwoływała, co kończyło zapis PDF paniką.

		content := c.cache.fontFiles[bFont.Origin()]
		// PDF readers do not support bitmap fonts
		if !content.isSupported {
			continue
		}

		// gopdfgo: font o konturach CFF to CIDFontType0 z podzbiorem CFF (ADR 0040); źródło zapisywało
		// go jako CIDFontType2 z całym fontem OpenType, czego czytniki nie uznają (pdffonts: „Mismatch
		// between font type and embedded font file”)
		if content.cff != cffsubset.NotCFF {
			font.writeCFFFont(bFont, content.content)
			continue
		}

		fs := newFontFile(bFont.Description(), font, content.content)

		desc := font.newFontDescriptor(bFont, fs)
		widths := cidWidths(font.Extents)

		font.FontDict.Subtype = model.FontType0{
			BaseFont: desc.FontName,
			Encoding: model.CMapEncodingPredefined("Identity-H"),
			DescendantFonts: model.CIDFontDictionary{
				Subtype:  "CIDFontType2",
				BaseFont: desc.FontName,
				CIDSystemInfo: model.CIDSystemInfo{
					Registry:   "Adobe",
					Ordering:   "Identity",
					Supplement: 0,
				},
				W:              widths,
				FontDescriptor: desc,
				// gopdfgo: identyfikatory znaków są numerami glifów; wpis jest wymagany dla osadzonych
				// fontów CID typu 2 (ISO 32000-1 § 9.7.4, tablica 117), źródło go pomijało.
				CIDToGIDMap: model.CIDToGIDMapIdentity{},
			},
		}
		font.FontDict.ToUnicode = &model.UnicodeCMap{Stream: newStream(writeToUnicodeCMap(font.Cmap))}
	}
}

// writeCFFFont (gopdfgo) zapisuje font o konturach CFF albo CFF2 jako font złożony Type 0 z potomkiem
// CIDFontType0 i programem /FontFile3 /Subtype /CIDFontType0C: podzbiór CFF w postaci CID-keyed, w którym
// CID glifu to jego numer w foncie źródłowym (ISO 32000-1 § 9.7.4, § 9.9; ADR 0040). Kodowanie Identity-H
// i szerokości /W są takie jak dla fontów TrueType; /CIDToGIDMap dotyczy tylko CIDFontType2, a CID na glif
// przelicza charset fontu CFF. Gdy podzbioru nie da się zbudować (uszkodzona tabela CFF albo konstrukcja
// CFF2, której interpreter nie przepisuje), osadzany jest cały font OpenType (/Subtype /OpenType).
func (font pdfFont) writeCFFFont(bFont backend.Font, content []byte) {
	desc := font.newFontDescriptor(bFont, nil)
	gids := make([]uint16, 0, len(font.Cmap))
	for gid := range font.Cmap {
		gids = append(gids, uint16(gid))
	}
	sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
	fs := &model.FontFile{Subtype: "CIDFontType0C"}
	data, err := cffsubset.Subset(content, gids, cffFontName(string(desc.FontName)))
	if err != nil {
		logger.WarningLogger.Printf("CFF font subsetting failed for %s: %s; embedding the whole font", bFont.Description().Family, err)
		fs.Subtype, data = "OpenType", content
	}
	fs.Stream = newStream(data)
	desc.FontFile = fs
	font.FontDict.Subtype = model.FontType0{
		BaseFont: desc.FontName,
		Encoding: model.CMapEncodingPredefined("Identity-H"),
		DescendantFonts: model.CIDFontDictionary{
			Subtype:  "CIDFontType0",
			BaseFont: desc.FontName,
			CIDSystemInfo: model.CIDSystemInfo{
				Registry:   "Adobe",
				Ordering:   "Identity",
				Supplement: 0,
			},
			W:              cidWidths(font.Extents),
			FontDescriptor: desc,
		},
	}
	font.FontDict.ToUnicode = &model.UnicodeCMap{Stream: newStream(writeToUnicodeCMap(font.Cmap))}
}

// cffFontName (gopdfgo) zostawia w nazwie fontu dla Name INDEX tylko drukowalne znaki ASCII bez znaków
// specjalnych PostScriptu (TN #5176 § 7), najwyżej 127 bajtów
func cffFontName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r > 32 && r < 127 && !strings.ContainsRune("[](){}<>/%", r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
		if b.Len() >= 127 {
			break
		}
	}
	return b.String()
}
