package model

import (
	"crypto/md5"
	"encoding/hex"
	"strings"
	"time"
)

// FileSpec is a File Specification Dictionary.
// In PDF, it may be found as a simple File Specification String
// which should be stored in the `UF` field.
type FileSpec struct {
	UF   string // optional
	EF   *EmbeddedFileStream
	Desc string // optional

	// gopdfgo: AFRelationship to związek pliku z dokumentem (Source, Data, Alternative, Supplement,
	// Unspecified; ISO 32000-2 § 7.11.3 tabela 43, wymagany przez ISO 19005-3 § 6.8). Źródło go nie miało.
	AFRelationship Name // optional
}

// returns the dictionnay, with a nil content `pdf` is used
// to create the EmbeddedFileStream object.
func (f *FileSpec) pdfContent(pdf pdfWriter, ref Reference) (StreamHeader, string, []byte) {
	b := newBuffer()
	b.fmt("<</Type/Filespec")
	if f.UF != "" {
		b.fmt("/UF %s /F %s", pdf.EncodeString(f.UF, TextString, ref),
			pdf.EncodeString(f.UF, ByteString, ref))
	}
	if f.EF != nil {
		s, _, by := f.EF.pdfContent(pdf, ref)
		ref := pdf.addStream(s, by)
		b.fmt("/EF <</F %s>>", ref)
	}
	if f.Desc != "" {
		b.fmt("/Desc %s", pdf.EncodeString(f.Desc, TextString, ref))
	}
	if f.AFRelationship != "" {
		b.fmt("/AFRelationship %s", f.AFRelationship)
	}
	b.fmt(">>")
	return StreamHeader{}, b.String(), nil
}

func (f *FileSpec) clone(cache cloneCache) Referenceable {
	if f == nil {
		return f
	}
	out := *f
	out.EF = cache.checkOrClone(f.EF).(*EmbeddedFileStream)
	return &out
}

type EmbeddedFileParams struct {
	CreationDate time.Time // optional
	ModDate      time.Time // optional
	Size         int       // optional
	CheckSum     string    // optional, must be hex16 encoded
}

// SetChecksumAndSize compute the size and the checksum of the `content`,
// which must be the original (not encoded) data.
func (f *EmbeddedFileParams) SetChecksumAndSize(content []byte) {
	f.Size = len(content)
	tmp := md5.Sum(content)
	f.CheckSum = hex.EncodeToString(tmp[:])
}

func (params EmbeddedFileParams) pdfString(pdf pdfWriter, ref Reference) string {
	b := newBuffer()
	b.WriteString("<<")
	if params.Size != 0 {
		b.fmt("/Size %d", params.Size)
	}
	if !params.CreationDate.IsZero() {
		b.fmt("/CreationDate %s", pdf.dateString(params.CreationDate, ref))
	}
	if !params.ModDate.IsZero() {
		b.fmt("/ModDate %s", pdf.dateString(params.ModDate, ref))
	}
	if params.CheckSum != "" {
		b.fmt("/CheckSum <%s>", params.CheckSum)
	}
	b.fmt(">>")
	return b.String()
}

type EmbeddedFileStream struct {
	Stream
	Params EmbeddedFileParams // optional

	// gopdfgo: Subtype to typ MIME pliku, np. "text/xml" (ISO 32000-1 § 7.11.4 tabela 45; wymagany
	// przez ISO 19005-3 § 6.8). Zapisywany jako nazwa, z ukośnikiem kodowanym jako #2F (§ 7.3.5).
	// Źródło tego wpisu nie zapisywało.
	Subtype string // optional
}

// EncodeName (gopdfgo) zapisuje dowolny napis jako obiekt nazwy PDF: bajty spoza zakresu ! do ~ oraz
// ograniczniki i znak # są kodowane jako #XX (ISO 32000-1 § 7.3.5).
func EncodeName(s string) string {
	b := newBuffer()
	b.WriteString("/")
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '!' || c > '~' || strings.IndexByte("#()<>[]{}/%", c) >= 0 {
			b.fmt("#%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func (emb *EmbeddedFileStream) pdfContent(pdf pdfWriter, ref Reference) (StreamHeader, string, []byte) {
	args := emb.PDFCommonFields(true)
	args.Fields["Type"] = "/EmbeddedFile"
	if emb.Subtype != "" {
		args.Fields["Subtype"] = EncodeName(emb.Subtype)
	}
	args.Fields["Params"] = emb.Params.pdfString(pdf, ref)
	return args, "", emb.Content
}

// clone returns a deep copy, with concrete type `*EmbeddedFileStream`
func (emb *EmbeddedFileStream) clone(cloneCache) Referenceable {
	if emb == nil {
		return emb
	}
	out := *emb // shallow copy
	out.Stream = emb.Stream.Clone()
	return &out
}
