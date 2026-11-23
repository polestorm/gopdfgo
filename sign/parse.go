package sign

// Minimalny czytnik struktury PDF (ISO 32000-1 § 7.2–7.5) na potrzeby aktualizacji przyrostowej:
// odsyłacze (tablice i strumienie, także pliki hybrydowe), trailer i obiekty pośrednie, również
// w strumieniach obiektów. Wczytuje tylko obiekty, o które poprosi wywołujący. Liczby zachowuje
// w postaci źródłowej, a słowniki w kolejności kluczy z pliku, żeby przepisany obiekt (katalog,
// strona) miał dokładnie te same wartości co oryginał.

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Wartości obiektów PDF.
type (
	pdfObject = any
	pdfName   string // nazwa bez ukośnika, po rozkodowaniu #xx
	pdfNumber string // liczba w postaci źródłowej, np. "595.2756"
	pdfString []byte // ciąg po rozkodowaniu (dosłowny albo szesnastkowy)
	pdfBool   bool
	pdfNull   struct{}
	pdfArray  []pdfObject
	pdfRef    struct{ num, gen int }
	pdfRaw    string // gotowy zapis obiektu (tylko przy zapisie aktualizacji)
)

// pdfDict zachowuje kolejność kluczy z pliku.
type pdfDict struct {
	keys   []pdfName
	values map[pdfName]pdfObject
}

func newDict() *pdfDict { return &pdfDict{values: map[pdfName]pdfObject{}} }

func (d *pdfDict) get(key pdfName) pdfObject { return d.values[key] }

func (d *pdfDict) set(key pdfName, value pdfObject) {
	if _, ok := d.values[key]; !ok {
		d.keys = append(d.keys, key)
	}
	d.values[key] = value
}

// pdfStream to strumień: słownik i bajty w postaci zapisanej w pliku (przed dekodowaniem filtrów).
type pdfStream struct {
	dict *pdfDict
	data []byte
}

// xrefEntry to wpis odsyłacza: obiekt pod przesunięciem (inStream == false) albo obiekt numer index
// w strumieniu obiektów stream.
type xrefEntry struct {
	offset   int64
	gen      int
	inStream bool
	stream   int
	index    int
	free     bool
}

// pdfFile to wczytana struktura pliku wejściowego.
type pdfFile struct {
	data       []byte
	xref       map[int]xrefEntry
	trailer    *pdfDict // trailer najnowszej sekcji
	startxref  int64    // przesunięcie najnowszej sekcji odsyłaczy (/Prev aktualizacji)
	xrefStream bool     // najnowsza sekcja jest strumieniem odsyłaczy
	size       int      // pierwszy wolny numer obiektu
	cache      map[int]pdfObject
	objStreams map[int][]pdfObject
}

var errMalformed = errors.New("sign: uszkodzony plik PDF")

func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errMalformed, fmt.Sprintf(format, args...))
}

// readPDF wczytuje odsyłacze i trailer pliku.
func readPDF(data []byte) (*pdfFile, error) {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, malformed("brak nagłówka %%PDF-")
	}
	f := &pdfFile{data: data, xref: map[int]xrefEntry{}, cache: map[int]pdfObject{}, objStreams: map[int][]pdfObject{}}
	tail := data[max(0, len(data)-2048):]
	i := bytes.LastIndex(tail, []byte("startxref"))
	if i < 0 {
		return nil, malformed("brak startxref")
	}
	lx := &lexer{data: tail, pos: i + len("startxref")}
	tok, err := lx.next()
	if err != nil {
		return nil, err
	}
	start, err := strconv.ParseInt(tok.text, 10, 64)
	if tok.kind != tokNumber || err != nil {
		return nil, malformed("niepoprawne startxref")
	}
	f.startxref = start
	seen := map[int64]bool{}
	offset := start
	for first := true; ; first = false {
		if offset <= 0 || offset >= int64(len(data)) || seen[offset] {
			if first {
				return nil, malformed("startxref poza plikiem")
			}
			break
		}
		seen[offset] = true
		trailer, isStream, err := f.readSection(offset)
		if err != nil {
			return nil, err
		}
		if first {
			f.trailer, f.xrefStream = trailer, isStream
		}
		// plik hybrydowy (§ 7.5.8.4): strumień /XRefStm uzupełnia tablicę tej samej sekcji
		if stm, ok := trailer.get("XRefStm").(pdfNumber); ok && !isStream {
			if at, err := strconv.ParseInt(string(stm), 10, 64); err == nil && !seen[at] {
				seen[at] = true
				if _, _, err := f.readSection(at); err != nil {
					return nil, err
				}
			}
		}
		prev, ok := trailer.get("Prev").(pdfNumber)
		if !ok {
			break
		}
		if offset, err = strconv.ParseInt(string(prev), 10, 64); err != nil {
			return nil, malformed("niepoprawne /Prev")
		}
	}
	if size, ok := f.trailer.get("Size").(pdfNumber); ok {
		f.size, _ = strconv.Atoi(string(size))
	}
	for num := range f.xref {
		f.size = max(f.size, num+1)
	}
	return f, nil
}

// readSection czyta sekcję odsyłaczy spod offset; wpisy nowsze (wczytane wcześniej) mają pierwszeństwo.
func (f *pdfFile) readSection(offset int64) (*pdfDict, bool, error) {
	lx := &lexer{data: f.data, pos: int(offset)}
	lx.skipSpace()
	if bytes.HasPrefix(f.data[lx.pos:], []byte("xref")) {
		lx.pos += 4
		trailer, err := f.readXrefTable(lx)
		return trailer, false, err
	}
	_, obj, err := f.parseIndirect(lx)
	if err != nil {
		return nil, false, err
	}
	stream, ok := obj.(*pdfStream)
	if !ok || stream.dict.get("Type") != pdfName("XRef") {
		return nil, false, malformed("pod startxref nie ma odsyłaczy")
	}
	return stream.dict, true, f.readXrefStream(stream)
}

func (f *pdfFile) readXrefTable(lx *lexer) (*pdfDict, error) {
	for {
		tok, err := lx.next()
		if err != nil {
			return nil, err
		}
		if tok.kind == tokKeyword && tok.text == "trailer" {
			obj, err := lx.object()
			if err != nil {
				return nil, err
			}
			dict, ok := obj.(*pdfDict)
			if !ok {
				return nil, malformed("trailer nie jest słownikiem")
			}
			return dict, nil
		}
		first, err1 := strconv.Atoi(tok.text)
		countTok, err := lx.next()
		if err != nil {
			return nil, err
		}
		count, err2 := strconv.Atoi(countTok.text)
		if tok.kind != tokNumber || err1 != nil || err2 != nil || count < 0 {
			return nil, malformed("niepoprawny podział tablicy odsyłaczy")
		}
		for n := first; n < first+count; n++ {
			offTok, err := lx.next()
			if err != nil {
				return nil, err
			}
			genTok, err := lx.next()
			if err != nil {
				return nil, err
			}
			kindTok, err := lx.next()
			if err != nil {
				return nil, err
			}
			off, _ := strconv.ParseInt(offTok.text, 10, 64)
			gen, _ := strconv.Atoi(genTok.text)
			if _, known := f.xref[n]; known {
				continue
			}
			switch kindTok.text {
			case "n":
				f.xref[n] = xrefEntry{offset: off, gen: gen}
			case "f":
				f.xref[n] = xrefEntry{gen: gen, free: true}
			default:
				return nil, malformed("niepoprawny wpis tablicy odsyłaczy")
			}
		}
	}
}

func (f *pdfFile) readXrefStream(stream *pdfStream) error {
	data, err := f.decodeStream(stream)
	if err != nil {
		return err
	}
	widths, ok := stream.dict.get("W").(pdfArray)
	if !ok || len(widths) != 3 {
		return malformed("strumień odsyłaczy bez /W")
	}
	var w [3]int
	for i := range w {
		w[i] = f.intValue(widths[i])
		if w[i] < 0 || w[i] > 8 {
			return malformed("niepoprawne /W")
		}
	}
	size := f.intValue(stream.dict.get("Size"))
	index := []int{0, size}
	if arr, ok := stream.dict.get("Index").(pdfArray); ok {
		index = index[:0]
		for _, v := range arr {
			index = append(index, f.intValue(v))
		}
	}
	rowLen := w[0] + w[1] + w[2]
	if rowLen == 0 {
		return malformed("niepoprawne /W")
	}
	field := func(row []byte, from, width int, def int64) int64 {
		if width == 0 {
			return def
		}
		var v int64
		for _, b := range row[from : from+width] {
			v = v<<8 | int64(b)
		}
		return v
	}
	pos := 0
	for i := 0; i+1 < len(index); i += 2 {
		for n := index[i]; n < index[i]+index[i+1]; n++ {
			if pos+rowLen > len(data) {
				return malformed("strumień odsyłaczy za krótki")
			}
			row := data[pos : pos+rowLen]
			pos += rowLen
			if _, known := f.xref[n]; known {
				continue
			}
			kind := field(row, 0, w[0], 1)
			a := field(row, w[0], w[1], 0)
			b := field(row, w[0]+w[1], w[2], 0)
			switch kind {
			case 0:
				f.xref[n] = xrefEntry{free: true, gen: int(b)}
			case 1:
				f.xref[n] = xrefEntry{offset: a, gen: int(b)}
			case 2:
				f.xref[n] = xrefEntry{inStream: true, stream: int(a), index: int(b)}
			}
		}
	}
	return nil
}

// resolve zwraca obiekt wskazywany przez odwołanie albo sam obiekt bezpośredni; odwołanie
// do obiektu nieistniejącego daje null (§ 7.3.10).
func (f *pdfFile) resolve(obj pdfObject) (pdfObject, error) {
	ref, ok := obj.(pdfRef)
	if !ok {
		return obj, nil
	}
	if cached, ok := f.cache[ref.num]; ok {
		return cached, nil
	}
	entry, ok := f.xref[ref.num]
	if !ok || entry.free {
		return pdfNull{}, nil
	}
	f.cache[ref.num] = pdfNull{} // pętla odwołań kończy się na null
	var result pdfObject
	if entry.inStream {
		objects, err := f.objectStream(entry.stream)
		if err != nil {
			return nil, err
		}
		if entry.index >= len(objects) {
			return nil, malformed("obiekt %d poza strumieniem obiektów", ref.num)
		}
		result = objects[entry.index]
	} else {
		if entry.offset <= 0 || entry.offset >= int64(len(f.data)) {
			return nil, malformed("obiekt %d poza plikiem", ref.num)
		}
		num, obj, err := f.parseIndirect(&lexer{data: f.data, pos: int(entry.offset)})
		if err != nil {
			return nil, err
		}
		if num != ref.num {
			return nil, malformed("pod przesunięciem obiektu %d jest obiekt %d", ref.num, num)
		}
		result = obj
	}
	f.cache[ref.num] = result
	return result, nil
}

// objectStream rozkodowuje strumień obiektów (§ 7.5.7).
func (f *pdfFile) objectStream(num int) ([]pdfObject, error) {
	if objects, ok := f.objStreams[num]; ok {
		return objects, nil
	}
	obj, err := f.resolve(pdfRef{num: num})
	if err != nil {
		return nil, err
	}
	stream, ok := obj.(*pdfStream)
	if !ok {
		return nil, malformed("obiekt %d nie jest strumieniem obiektów", num)
	}
	data, err := f.decodeStream(stream)
	if err != nil {
		return nil, err
	}
	n, first := f.intValue(stream.dict.get("N")), f.intValue(stream.dict.get("First"))
	if n < 0 || first < 0 || first > len(data) {
		return nil, malformed("niepoprawny strumień obiektów %d", num)
	}
	header := &lexer{data: data[:first]}
	objects := make([]pdfObject, n)
	for i := range n {
		if _, err := header.next(); err != nil {
			return nil, err
		}
		offTok, err := header.next()
		if err != nil {
			return nil, err
		}
		off, err := strconv.Atoi(offTok.text)
		if err != nil || first+off > len(data) {
			return nil, malformed("niepoprawny nagłówek strumienia obiektów %d", num)
		}
		if objects[i], err = (&lexer{data: data, pos: first + off}).object(); err != nil {
			return nil, err
		}
	}
	f.objStreams[num] = objects
	return objects, nil
}

// parseIndirect czyta „N G obj … endobj” spod bieżącej pozycji; strumień czyta z /Length.
func (f *pdfFile) parseIndirect(lx *lexer) (int, pdfObject, error) {
	numTok, err := lx.next()
	if err != nil {
		return 0, nil, err
	}
	if _, err := lx.next(); err != nil {
		return 0, nil, err
	}
	objTok, err := lx.next()
	if err != nil {
		return 0, nil, err
	}
	num, err := strconv.Atoi(numTok.text)
	if err != nil || objTok.text != "obj" {
		return 0, nil, malformed("oczekiwano definicji obiektu pod przesunięciem %d", lx.pos)
	}
	obj, err := lx.object()
	if err != nil {
		return 0, nil, err
	}
	dict, ok := obj.(*pdfDict)
	if !ok {
		return num, obj, nil
	}
	save := lx.pos
	tok, err := lx.next()
	if err != nil || tok.kind != tokKeyword || tok.text != "stream" {
		lx.pos = save
		return num, obj, nil
	}
	// po słowie stream: CRLF albo LF (§ 7.3.8.1)
	if lx.pos < len(lx.data) && lx.data[lx.pos] == '\r' {
		lx.pos++
	}
	if lx.pos < len(lx.data) && lx.data[lx.pos] == '\n' {
		lx.pos++
	}
	start := lx.pos
	length := -1
	switch l := dict.get("Length").(type) {
	case pdfNumber:
		length, _ = strconv.Atoi(string(l))
	case pdfRef:
		resolved, err := f.resolve(l)
		if err != nil {
			return 0, nil, err
		}
		length = f.intValue(resolved)
	}
	if length < 0 || start+length > len(lx.data) {
		// /Length niepoprawne: koniec strumienia według słowa endstream
		end := bytes.Index(lx.data[start:], []byte("endstream"))
		if end < 0 {
			return 0, nil, malformed("strumień obiektu %d bez endstream", num)
		}
		length = end
	}
	return num, &pdfStream{dict: dict, data: lx.data[start : start+length]}, nil
}

func (f *pdfFile) intValue(obj pdfObject) int {
	resolved, err := f.resolve(obj)
	if err != nil {
		return -1
	}
	if n, ok := resolved.(pdfNumber); ok {
		if v, err := strconv.Atoi(string(n)); err == nil {
			return v
		}
	}
	return -1
}

// decodeStream rozkodowuje strumień odsyłaczy albo obiektów: FlateDecode z predyktorami PNG
// (§ 7.4.4) albo brak filtra. Innych filtrów te strumienie w praktyce nie mają.
func (f *pdfFile) decodeStream(stream *pdfStream) ([]byte, error) {
	filter := stream.dict.get("Filter")
	params, _ := f.resolve(stream.dict.get("DecodeParms"))
	if arr, ok := filter.(pdfArray); ok {
		if len(arr) > 1 {
			return nil, malformed("nieobsługiwany łańcuch filtrów strumienia struktury")
		}
		filter = nil
		if len(arr) == 1 {
			filter = arr[0]
		}
		if p, ok := params.(pdfArray); ok && len(p) == 1 {
			params, _ = f.resolve(p[0])
		}
	}
	switch filter {
	case nil:
		return stream.data, nil
	case pdfName("FlateDecode"):
	default:
		return nil, malformed("nieobsługiwany filtr %v strumienia struktury", filter)
	}
	r, err := zlib.NewReader(bytes.NewReader(stream.data))
	if err != nil {
		return nil, malformed("FlateDecode: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil && len(data) == 0 {
		return nil, malformed("FlateDecode: %v", err)
	}
	dict, ok := params.(*pdfDict)
	if !ok {
		return data, nil
	}
	predictor := f.intValue(dict.get("Predictor"))
	if predictor < 10 {
		if predictor > 1 {
			return nil, malformed("nieobsługiwany predyktor %d", predictor)
		}
		return data, nil
	}
	columns := f.intValue(dict.get("Columns"))
	if columns <= 0 {
		columns = 1
	}
	colors, bits := f.intValue(dict.get("Colors")), f.intValue(dict.get("BitsPerComponent"))
	if colors <= 0 {
		colors = 1
	}
	if bits <= 0 {
		bits = 8
	}
	return unpredictPNG(data, columns, (colors*bits+7)/8)
}

// unpredictPNG odwraca predyktory PNG (RFC 2083 § 6), każdy wiersz z własnym bajtem typu.
func unpredictPNG(data []byte, columns, bpp int) ([]byte, error) {
	rowLen := columns * bpp
	out := make([]byte, 0, len(data))
	prev := make([]byte, rowLen)
	for pos := 0; pos+1+rowLen <= len(data); pos += 1 + rowLen {
		kind, row := data[pos], append([]byte(nil), data[pos+1:pos+1+rowLen]...)
		for i := range row {
			var left, up, upLeft byte
			if i >= bpp {
				left, upLeft = row[i-bpp], prev[i-bpp]
			}
			up = prev[i]
			switch kind {
			case 0:
			case 1:
				row[i] += left
			case 2:
				row[i] += up
			case 3:
				row[i] += byte((int(left) + int(up)) / 2)
			case 4:
				p := int(left) + int(up) - int(upLeft)
				pa, pb, pc := abs(p-int(left)), abs(p-int(up)), abs(p-int(upLeft))
				switch {
				case pa <= pb && pa <= pc:
					row[i] += left
				case pb <= pc:
					row[i] += up
				default:
					row[i] += upLeft
				}
			default:
				return nil, malformed("niepoprawny predyktor PNG %d", kind)
			}
		}
		out = append(out, row...)
		prev = row
	}
	return out, nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Leksyka PDF (§ 7.2, 7.3).

type tokenKind int

const (
	tokNumber tokenKind = iota
	tokName
	tokString
	tokKeyword // true, false, null, R, obj, stream, trailer…
	tokDelim   // [ ] << >>
)

type token struct {
	kind tokenKind
	text string // liczba, nazwa (rozkodowana), słowo kluczowe albo ogranicznik
	str  []byte // ciąg po rozkodowaniu
}

type lexer struct {
	data []byte
	pos  int
}

func isSpace(c byte) bool {
	return c == 0 || c == '\t' || c == '\n' || c == '\f' || c == '\r' || c == ' '
}

func isDelim(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

func (lx *lexer) skipSpace() {
	for lx.pos < len(lx.data) {
		c := lx.data[lx.pos]
		if isSpace(c) {
			lx.pos++
		} else if c == '%' {
			for lx.pos < len(lx.data) && lx.data[lx.pos] != '\n' && lx.data[lx.pos] != '\r' {
				lx.pos++
			}
		} else {
			return
		}
	}
}

func (lx *lexer) next() (token, error) {
	lx.skipSpace()
	if lx.pos >= len(lx.data) {
		return token{}, malformed("nieoczekiwany koniec danych")
	}
	c := lx.data[lx.pos]
	switch {
	case c == '[' || c == ']':
		lx.pos++
		return token{kind: tokDelim, text: string(c)}, nil
	case c == '<' && lx.pos+1 < len(lx.data) && lx.data[lx.pos+1] == '<':
		lx.pos += 2
		return token{kind: tokDelim, text: "<<"}, nil
	case c == '>' && lx.pos+1 < len(lx.data) && lx.data[lx.pos+1] == '>':
		lx.pos += 2
		return token{kind: tokDelim, text: ">>"}, nil
	case c == '<':
		return lx.hexString()
	case c == '(':
		return lx.literalString()
	case c == '/':
		lx.pos++
		var name []byte
		for lx.pos < len(lx.data) && !isSpace(lx.data[lx.pos]) && !isDelim(lx.data[lx.pos]) {
			b := lx.data[lx.pos]
			if b == '#' && lx.pos+2 < len(lx.data) {
				if v, err := strconv.ParseUint(string(lx.data[lx.pos+1:lx.pos+3]), 16, 8); err == nil {
					name = append(name, byte(v))
					lx.pos += 3
					continue
				}
			}
			name = append(name, b)
			lx.pos++
		}
		return token{kind: tokName, text: string(name)}, nil
	case isDelim(c):
		return token{}, malformed("nieoczekiwany znak %q", c)
	}
	start := lx.pos
	for lx.pos < len(lx.data) && !isSpace(lx.data[lx.pos]) && !isDelim(lx.data[lx.pos]) {
		lx.pos++
	}
	text := string(lx.data[start:lx.pos])
	if c == '+' || c == '-' || c == '.' || (c >= '0' && c <= '9') {
		return token{kind: tokNumber, text: text}, nil
	}
	return token{kind: tokKeyword, text: text}, nil
}

func (lx *lexer) hexString() (token, error) {
	lx.pos++
	var out []byte
	high, half := byte(0), false
	for lx.pos < len(lx.data) {
		c := lx.data[lx.pos]
		lx.pos++
		if c == '>' {
			if half {
				out = append(out, high<<4)
			}
			return token{kind: tokString, str: out}, nil
		}
		var v byte
		switch {
		case c >= '0' && c <= '9':
			v = c - '0'
		case c >= 'a' && c <= 'f':
			v = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			v = c - 'A' + 10
		case isSpace(c):
			continue
		default:
			return token{}, malformed("niepoprawny ciąg szesnastkowy")
		}
		if half {
			out = append(out, high<<4|v)
		} else {
			high = v
		}
		half = !half
	}
	return token{}, malformed("niezamknięty ciąg szesnastkowy")
}

func (lx *lexer) literalString() (token, error) {
	lx.pos++
	var out []byte
	depth := 1
	for lx.pos < len(lx.data) {
		c := lx.data[lx.pos]
		lx.pos++
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return token{kind: tokString, str: out}, nil
			}
		case '\r':
			// koniec wiersza w ciągu to zawsze \n (§ 7.3.4.2)
			if lx.pos < len(lx.data) && lx.data[lx.pos] == '\n' {
				lx.pos++
			}
			c = '\n'
		case '\\':
			if lx.pos >= len(lx.data) {
				break
			}
			e := lx.data[lx.pos]
			lx.pos++
			switch e {
			case 'n':
				c = '\n'
			case 'r':
				c = '\r'
			case 't':
				c = '\t'
			case 'b':
				c = '\b'
			case 'f':
				c = '\f'
			case '\r':
				if lx.pos < len(lx.data) && lx.data[lx.pos] == '\n' {
					lx.pos++
				}
				continue
			case '\n':
				continue
			default:
				if e >= '0' && e <= '7' {
					v := int(e - '0')
					for k := 0; k < 2 && lx.pos < len(lx.data) && lx.data[lx.pos] >= '0' && lx.data[lx.pos] <= '7'; k++ {
						v = v*8 + int(lx.data[lx.pos]-'0')
						lx.pos++
					}
					c = byte(v)
				} else {
					c = e
				}
			}
		}
		out = append(out, c)
	}
	return token{}, malformed("niezamknięty ciąg")
}

// object czyta jeden obiekt bezpośredni; „N G R” daje odwołanie.
func (lx *lexer) object() (pdfObject, error) {
	tok, err := lx.next()
	if err != nil {
		return nil, err
	}
	return lx.objectFrom(tok)
}

func (lx *lexer) objectFrom(tok token) (pdfObject, error) {
	switch tok.kind {
	case tokName:
		return pdfName(tok.text), nil
	case tokString:
		return pdfString(tok.str), nil
	case tokNumber:
		// odwołanie „N G R”: dwie liczby całkowite i R
		save := lx.pos
		if gen, err := lx.next(); err == nil && gen.kind == tokNumber {
			if r, err := lx.next(); err == nil && r.kind == tokKeyword && r.text == "R" {
				num, err1 := strconv.Atoi(tok.text)
				g, err2 := strconv.Atoi(gen.text)
				if err1 == nil && err2 == nil {
					return pdfRef{num: num, gen: g}, nil
				}
			}
		}
		lx.pos = save
		return pdfNumber(tok.text), nil
	case tokKeyword:
		switch tok.text {
		case "true":
			return pdfBool(true), nil
		case "false":
			return pdfBool(false), nil
		case "null":
			return pdfNull{}, nil
		}
		return nil, malformed("nieoczekiwane słowo %q", tok.text)
	}
	switch tok.text {
	case "[":
		arr := pdfArray{}
		for {
			t, err := lx.next()
			if err != nil {
				return nil, err
			}
			if t.kind == tokDelim && t.text == "]" {
				return arr, nil
			}
			v, err := lx.objectFrom(t)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
	case "<<":
		dict := newDict()
		for {
			t, err := lx.next()
			if err != nil {
				return nil, err
			}
			if t.kind == tokDelim && t.text == ">>" {
				return dict, nil
			}
			if t.kind != tokName {
				return nil, malformed("klucz słownika nie jest nazwą")
			}
			v, err := lx.object()
			if err != nil {
				return nil, err
			}
			dict.set(pdfName(t.text), v)
		}
	}
	return nil, malformed("nieoczekiwany ogranicznik %q", tok.text)
}
