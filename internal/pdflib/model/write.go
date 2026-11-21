package model

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/unicode"
)

// Reference is the object number of a PDF object.
// It is only needed to write a document.
// It differs from the ObjReference type because
// it is never part of a PDF object, but is only created and used
// during the write phase. Instead, indirection is represented
// in the model by standard Go pointers.
type Reference uint32

// String return a string to be used when writing a PDF
func (r Reference) String() string {
	return fmt.Sprintf("%d 0 R", r)
}

// output implements the logic needed to write object
// and keep track of the correct byte offsets
type output struct {
	dst     io.Writer
	err     error     // internal error, to defer error checking
	written int       // total number of bytes written to dst
	digest  hash.Hash // gopdfgo: skrót zapisanych bajtów, podstawa Trailer.ContentID

	// encode the object numbers as index (starting from 1)
	// and the byte offsets of objects (starts at 1, [0] is unused)
	objOffsets []int

	// gopdfgo: packed – obiekty czekające na zapis w strumieniu obiektów (Trailer.ObjectStreams); nil:
	// zapis zwykły
	packed *objectStreams
}

// objectStreams (gopdfgo) zbiera obiekty niebędące strumieniami w strumienie obiektów (ISO 32000-1
// § 7.5.7) po objectsPerStream; entries to położenie obiektu: numer strumienia i indeks w nim.
type objectStreams struct {
	refs    []Reference
	offsets []int
	body    bytes.Buffer
	entries map[Reference][2]int
}

const objectsPerStream = 200

// writePacked (gopdfgo) dopisuje obiekt do bieżącego strumienia obiektów.
func (w *output) writePacked(content string, ref Reference) {
	p := w.packed
	if p.body.Len() != 0 {
		p.body.WriteByte('\n')
	}
	p.refs = append(p.refs, ref)
	p.offsets = append(p.offsets, p.body.Len())
	p.body.WriteString(content)
	if len(p.refs) == objectsPerStream {
		w.flushPacked()
	}
}

// flushPacked (gopdfgo) zapisuje zebrane obiekty jako strumień obiektów, skompresowany jak inne strumienie.
func (w *output) flushPacked() {
	p := w.packed
	if len(p.refs) == 0 {
		return
	}
	var index bytes.Buffer
	for i, ref := range p.refs {
		fmt.Fprintf(&index, "%d %d ", ref, p.offsets[i])
	}
	index.WriteByte('\n')
	stream := w.CreateObject()
	for i, ref := range p.refs {
		p.entries[ref] = [2]int{int(stream), i}
	}
	data := NewCompressedStream(append(index.Bytes(), p.body.Bytes()...))
	w.objOffsets[stream] = w.written
	w.bytes([]byte(fmt.Sprintf("%d 0 obj\n<</Type/ObjStm/N %d/First %d/Filter/FlateDecode/Length %d>>\nstream\n",
		stream, len(p.refs), index.Len(), len(data.Content))))
	w.bytes(data.Content)
	w.bytes([]byte("\nendstream\nendobj\n"))
	p.refs, p.offsets = p.refs[:0], p.offsets[:0]
	p.body.Reset()
}

// writeXRefStream (gopdfgo) kończy plik strumieniem odsyłaczy (ISO 32000-1 § 7.5.8) zamiast tabeli
// odsyłaczy i słownika trailer; identyfikator pliku jak w writeFooter.
func (w *output) writeXRefStream(trailer Trailer, root, info Reference) {
	w.flushPacked()
	xref := w.CreateObject()
	offset := w.written
	w.objOffsets[xref] = offset
	size := len(w.objOffsets)
	rows := make([]byte, 0, 7*size)
	for j := 0; j < size; j++ {
		var kind byte
		var field2, field3 int
		switch entry, packed := w.packed.entries[Reference(j)]; {
		case j == 0:
			kind, field3 = 0, 65535
		case packed:
			kind, field2, field3 = 2, entry[0], entry[1]
		default:
			kind, field2 = 1, w.objOffsets[j]
		}
		rows = append(rows, kind, byte(field2>>24), byte(field2>>16), byte(field2>>8), byte(field2), byte(field3>>8), byte(field3))
	}
	data := NewCompressedStream(rows)
	var id string
	if trailer.ContentID && w.digest != nil {
		sum := hex.EncodeToString(w.digest.Sum(nil)[:16])
		id = fmt.Sprintf("/ID [<%s> <%s>]", sum, sum)
	}
	w.bytes([]byte(fmt.Sprintf("%d 0 obj\n<</Type/XRef/Size %d/W [1 4 2]/Root %d 0 R/Info %d 0 R%s/Filter/FlateDecode/Length %d>>\nstream\n",
		xref, size, root, info, id, len(data.Content))))
	w.bytes(data.Content)
	w.bytes([]byte(fmt.Sprintf("\nendstream\nendobj\nstartxref\n%d\n%%%%EOF", offset)))
}

func (w *output) bytes(b []byte) {
	if w.err != nil { // write is now a no-op
		return
	}
	n, err := w.dst.Write(b)
	if err != nil {
		w.err = err
		return
	}
	w.written += n
	if w.digest != nil {
		w.digest.Write(b[:n])
	}
}

// CreateObject return a new reference
// and grow the `objOffsets` accordingly.
// This is needed to write objects that must reference their "parent".
func (w *output) CreateObject() Reference {
	ref := Reference(len(w.objOffsets)) // last object is at len(objOffsets) - 1
	w.objOffsets = append(w.objOffsets, 0)
	return ref
}

func (w *output) writeHeader() {
	w.bytes([]byte("%PDF-1.7\n"))
	// If a PDF file contains binary data, as most do (see 7.2, "Lexical Conventions"), the header line shall be
	// immediately followed by a comment line containing at least four binary characters—that is, characters whose
	// codes are 128 or greater.
	w.bytes([]byte("%"))
	w.bytes([]byte{200, 200, 200, 200})
	w.bytes([]byte("\n"))
}

func (w *output) writeFooter(trailer Trailer, root, info, encrypt Reference) {
	var b bytes.Buffer
	// Cross-ref
	o, n := w.written, len(w.objOffsets)-1
	b.WriteString("xref\n")
	b.WriteString(fmt.Sprintf("0 %d\n", n+1))
	b.WriteString("0000000000 65535 f \n")
	for j := 1; j <= n; j++ {
		b.WriteString(fmt.Sprintf("%010d 00000 n \n", w.objOffsets[j]))
	}
	// Trailer
	b.WriteString("trailer\n")
	b.WriteString("<<\n")
	b.WriteString(fmt.Sprintf("/Size %d\n", n+1))
	b.WriteString(fmt.Sprintf("/Root %d 0 R\n", root))
	b.WriteString(fmt.Sprintf("/Info %d 0 R\n", info))
	if encrypt > 0 {
		b.WriteString(fmt.Sprintf("/Encrypt %s\n", encrypt))
		b.WriteString(fmt.Sprintf("/ID [%s %s]\n",
			EscapeByteString([]byte(trailer.ID[0])), EscapeByteString([]byte(trailer.ID[1]))))
	} else if trailer.ContentID && w.digest != nil {
		id := hex.EncodeToString(w.digest.Sum(nil)[:16])
		b.WriteString(fmt.Sprintf("/ID [<%s> <%s>]\n", id, id))
	}
	b.WriteString(">>\n")
	b.WriteString("startxref\n")
	b.WriteString(fmt.Sprintf("%d\n", o))
	b.WriteString("%%EOF")
	w.bytes(b.Bytes())
}

// pdfWriter uses an output and an internal cache
// to write a Document.
// The internal cache avoids duplication of indirect object,
// by associating an object number to a pointer
type pdfWriter struct {
	*output

	cache     map[Referenceable]Reference
	pages     map[PageNode]Reference
	outlines  map[*OutlineItem]Reference
	fields    map[*FormFieldDict]Reference
	structure map[*StructureElement]Reference

	// needed by annotations and accroform,
	// setup early
	catalog           Reference
	mergedAccroFields map[*AnnotationDict]*FormFieldDict

	encrypt *Encrypt
}

func newWriter(dest io.Writer, encrypt *Encrypt) pdfWriter {
	return pdfWriter{
		output:            &output{dst: dest, objOffsets: []int{0}},
		cache:             make(map[Referenceable]Reference),
		pages:             make(map[PageNode]Reference),
		outlines:          make(map[*OutlineItem]Reference),
		fields:            make(map[*FormFieldDict]Reference),
		structure:         make(map[*StructureElement]Reference), // gopdfgo: zob. StructureTree.pdfString
		mergedAccroFields: make(map[*AnnotationDict]*FormFieldDict),
		encrypt:           encrypt,
	}
}

type PDFStringEncoding uint8

const (
	ByteString PDFStringEncoding = iota // no special treatment, except escaping
	// ASCIIString                          // ASCII encoding and escaping
	HexString  // hex form
	TextString // one of the PDF encoding: PDFDocEncoding or UTF16-BE
)

var (
	replacer = strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)", "\r", "\\r")
	utf16Enc = unicode.UTF16(unicode.BigEndian, unicode.UseBOM)
)

// WrittenObject represents a PDF object to write on a file.
// This intermediate representation makes to possible to
// modify object just before writting them, as needed for instance
// for the Length attribute of encrypted streams.
type StreamHeader struct {
	Fields      map[Name]string
	BypassCrypt bool
}

func (w StreamHeader) PDFContent() []byte {
	// sort for deterministic output
	keys := make([]Name, 0, len(w.Fields))
	for _, k := range SortedKeys(w.Fields) {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	var out bytes.Buffer
	out.WriteString("<<")
	for _, k := range keys {
		out.WriteString(k.String() + " " + w.Fields[k] + " ")
	}
	out.WriteString(">>")
	return out.Bytes()
}

func (w *StreamHeader) updateWith(other map[Name]string) {
	for _, k := range SortedKeys(other) {
		v := other[k]
		w.Fields[k] = v
	}
}

// PDFWritter abstracts away the complexity of writting PDF files.
// It used internally by the package, but is also exposed
// to ease the use of custom types implementing `Object`.
// It will handle strings formatting and encryption, as well
// as creating indirect objects.
type PDFWritter interface {
	// EncodeString transforms an UTF-8 string `s` to satisfy the PDF
	// format required by `mode`.
	// It will also encrypt `s`, if needed, using
	// `context`, which is the object number of the containing object.
	// EncodeString may panic if `mode` is not one of `ByteString`, `HexString`, `TextString`
	EncodeString(s string, mode PDFStringEncoding, context Reference) string

	// Allocate a new object (used then by `WriteObject`)
	CreateObject() Reference

	// WriteObject add the objects content to the output, under the
	// `ref` object number.
	// This method should be called at most once for each reference.
	WriteObject(content string, ref Reference)

	// WriteStream write the content of the object `ref`, and update the offsets.
	// This method will be called at most once for each reference.
	// Stream content will be encrypted if needed and the Length field adjusted.
	WriteStream(header StreamHeader, stream []byte, ref Reference)
}

// EscapeByteString return a pdf compatible litteral string, by
// escaping special characters and adding parenthesis.
//
// PDFStringEncoder.EncodeString provides a more general
// approach, and should be used when implementing custom types.
func EscapeByteString(sb []byte) string {
	s := replacer.Replace(string(sb))
	return "(" + s + ")"
}

// EspaceHexString return a pdf compatible hex string, by
// hex encoding it and adding brackets.
//
// PDFStringEncoder.EncodeString provides a more general
// approach, and should be used when implementing custom types.
func EspaceHexString(sb []byte) string {
	return "<" + hex.EncodeToString(sb) + ">"
}

func (p pdfWriter) EncodeString(s string, mode PDFStringEncoding, context Reference) string {
	if p.err != nil {
		return ""
	}
	sb := []byte(s)
	var err error
	if mode == TextString {
		// we try PDFEncoding to produce simpler PDF
		var ok bool
		s1, ok := stringToPDFDocEncoding(s)
		if ok {
			sb = s1
		} else {
			sb, err = utf16Enc.NewEncoder().Bytes(sb)
			if err != nil {
				p.err = fmt.Errorf("invalid text string %s: %w", s, err)
				return ""
			}
		}
	}

	if p.encrypt != nil && p.encrypt.EncryptionHandler != nil {
		sb, err = p.encrypt.EncryptionHandler.crypt(context, sb)
		if err != nil {
			p.err = fmt.Errorf("failed to encrypt content: %w", err)
			return ""
		}
	}

	switch mode {
	case ByteString, TextString:
		return EscapeByteString(sb) // string litteral
	case HexString:
		return EspaceHexString(sb) // hex string
	default:
		panic("invalid encoding mode")
	}
}

// WriteObject write the content of the object `ref`, and update the offsets.
// This method will be called at most once for each reference.
// For stream object, `content` will contain the dictionary,
// and `stream` the inner stream bytes. For other objects, `stream` will be nil.
// Stream content will be encrypted if needed.
func (w pdfWriter) WriteObject(content string, ref Reference) {
	if w.packed != nil { // gopdfgo: zob. Trailer.ObjectStreams
		w.writePacked(content, ref)
		return
	}
	w.objOffsets[ref] = w.written
	w.bytes([]byte(fmt.Sprintf("%d 0 obj\n", ref)))
	w.bytes([]byte(content))
	w.bytes([]byte("\nendobj\n"))
}

// WriteStream write the content of the object `ref`, and update the offsets.
// This method will be called at most once for each reference.
// Stream content will be encrypted if needed and the Length field adjusted.
func (w pdfWriter) WriteStream(content StreamHeader, stream []byte, ref Reference) {
	w.objOffsets[ref] = w.written
	w.bytes([]byte(fmt.Sprintf("%d 0 obj\n", ref)))
	// we first need to adjust the Length
	if w.encrypt != nil && w.encrypt.EncryptionHandler != nil && !content.BypassCrypt {
		// we must ensure we dont modify the original stream
		// which may be a Stream.Content slice
		stream = append([]byte(nil), stream...)
		w.encrypt.EncryptionHandler.crypt(ref, stream)
		content.Fields["Length"] = strconv.Itoa(len(stream))
	}
	w.bytes(content.PDFContent())
	if stream != nil {
		w.bytes([]byte("\nstream\n"))
		w.bytes(stream)
		// There should be an end-of-line marker after the data and before endstream
		w.bytes([]byte("\nendstream"))
	}
	w.bytes([]byte("\nendobj\n"))
}

// addObject is a convenience shortcut to write `content` into a new object
// and return the created reference
func (p pdfWriter) addObject(content string) Reference {
	ref := p.CreateObject()
	p.WriteObject(content, ref)
	return ref
}

// addStream is a convenience shortcut to write `content` and `stream` into a new stream
// and return the created reference
func (p pdfWriter) addStream(content StreamHeader, stream []byte) Reference {
	ref := p.CreateObject()
	p.WriteStream(content, stream, ref)
	return ref
}

// writerCache

// Referenceable is a private interface implemented
// by the structures idenfied by pointers.
// For such a structure, two usage of the same pointer
// in a `Document` will be written in the PDF file using the same
// object number, avoiding unnecessary duplications.
type Referenceable interface {
	IsReferenceable()

	// clone returns a deep copy, preserving the concrete type
	// it will use the `cache` for child items which are themselves
	// `Referenceable`
	// see cloneCache.checkOrClone to avoid unwanted allocations
	clone(cache cloneCache) Referenceable

	// pdfContent returns the content to write to the PDF file
	// Stream object will return a non nil `header` and `content` will be ignored
	// Regular object will return a nil `header` and `stream` will be ignored
	pdfContent(pdf pdfWriter, objectRef Reference) (header StreamHeader, content string, stream []byte)
}

func (*FontDict) IsReferenceable()                 {}
func (*GraphicState) IsReferenceable()             {}
func (*SimpleEncodingDict) IsReferenceable()       {}
func (*AnnotationDict) IsReferenceable()           {}
func (*FileSpec) IsReferenceable()                 {}
func (*EmbeddedFileStream) IsReferenceable()       {}
func (*ShadingDict) IsReferenceable()              {}
func (*FunctionDict) IsReferenceable()             {}
func (*PatternTiling) IsReferenceable()            {}
func (*PatternShading) IsReferenceable()           {}
func (*ColorSpaceICCBased) IsReferenceable()       {}
func (*ColorTableStream) IsReferenceable()         {}
func (*XObjectForm) IsReferenceable()              {}
func (*XObjectTransparencyGroup) IsReferenceable() {}
func (*XObjectImage) IsReferenceable()             {}
func (*ImageSMask) IsReferenceable()               {}
func (*FontFile) IsReferenceable()                 {}

// check the cache and write a new item if not found
func (pdf pdfWriter) addItem(item Referenceable) Reference {
	if ref, has := pdf.cache[item]; has {
		return ref
	}
	ref := pdf.CreateObject()
	pdf.cache[item] = ref
	header, obj, s := item.pdfContent(pdf, ref)
	if header.Fields != nil {
		pdf.WriteStream(header, s, ref)
	} else {
		pdf.WriteObject(obj, ref)
	}
	return ref
}
