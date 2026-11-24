package htmlpdf

import (
	"errors"
	"fmt"
	"github.com/polestorm/gopdfgo/internal/pdfa"
	"time"

	"github.com/polestorm/gopdfgo/internal/goweasyprint/pdf"
	"github.com/polestorm/gopdfgo/internal/webrender/backend"
)

// Relationship is the relationship of an attached file to the document, written as /AFRelationship in the
// file specification dictionary (ISO 32000-2 § 7.11.3, table 43; ISO 19005-3 § 6.8).
type Relationship string

const (
	// RelationshipSource: the source file the document was made from (e.g. the spreadsheet a table was printed from).
	RelationshipSource Relationship = "Source"
	// RelationshipData: the data the content of the document was made from (e.g. the data of a chart).
	RelationshipData Relationship = "Data"
	// RelationshipAlternative: another form of the same content (e.g. the XML of a Factur-X/ZUGFeRD invoice).
	RelationshipAlternative Relationship = "Alternative"
	// RelationshipSupplement: a supplement to the content of the document.
	RelationshipSupplement Relationship = "Supplement"
	// RelationshipUnspecified: a relationship that is unknown or other than the above; the default value.
	RelationshipUnspecified Relationship = "Unspecified"
)

// Attachment is a file embedded in the document at the level of the whole document (the catalog's
// /EmbeddedFiles name tree and, in PDF/A-3 mode, also the catalog's /AF array).
type Attachment struct {
	// Name is the file name (/F and /UF of the file specification, the key in the name tree); required and unique.
	Name string
	// Data is the content of the file.
	Data []byte
	// MediaType is the MIME type, e.g. "text/xml", written as the /Subtype of the file stream. Empty means
	// "application/octet-stream" (ISO 19005-3 § 6.8). Parameters (";charset=...") are dropped.
	MediaType string
	// Description is the description of the file (/Desc); it may be empty.
	Description string
	// Relationship is the relationship to the document; empty means RelationshipUnspecified.
	Relationship Relationship
	// ModTime is the modification date of the file (/Params /ModDate). Zero value: the entry is omitted, because
	// ISO 32000-1 table 46 makes it optional and ISO 19005-3 § 6.8 does not require it; the engine does not
	// insert the current time, so that the output is reproducible.
	ModTime time.Time
}

var (
	// ErrInvalidAttachment: an attachment from Options.Attachments without a name, with a repeated name or
	// with an unknown Relationship value.
	ErrInvalidAttachment = errors.New("gopdfgo: invalid attachment")
	// ErrAttachmentNotPDFA: in PDF/A-2b mode the document has an attachment that is not a PDF/A-1 or
	// PDF/A-2 file, which ISO 19005-2 § 6.8 forbids. PDF/A-3b allows attachments of any type.
	ErrAttachmentNotPDFA = errors.New("gopdfgo: PDF/A-2 allows only PDF/A-1 or PDF/A-2 files as attachments")
)

// backendAttachments sprawdza Options.Attachments i zamienia je na postać silnika.
func backendAttachments(list []Attachment) ([]backend.Attachment, error) {
	out := make([]backend.Attachment, 0, len(list))
	seen := map[string]bool{}
	for _, a := range list {
		if a.Name == "" {
			return nil, fmt.Errorf("%w: empty name", ErrInvalidAttachment)
		}
		if seen[a.Name] {
			return nil, fmt.Errorf("%w: duplicate name %q", ErrInvalidAttachment, a.Name)
		}
		seen[a.Name] = true
		relationship := a.Relationship
		switch relationship {
		case "":
			relationship = RelationshipUnspecified
		case RelationshipSource, RelationshipData, RelationshipAlternative, RelationshipSupplement, RelationshipUnspecified:
		default:
			return nil, fmt.Errorf("%w: %q: unknown relationship %q", ErrInvalidAttachment, a.Name, a.Relationship)
		}
		out = append(out, backend.Attachment{
			Title:        a.Name,
			Description:  a.Description,
			Content:      a.Data,
			MediaType:    mediaType(a.MediaType, ""),
			Relationship: string(relationship),
			ModTime:      a.ModTime,
		})
	}
	return out, nil
}

// checkAttachments zwraca błąd, gdy wybrana norma nie dopuszcza któregoś z osadzonych plików.
func checkAttachments(files []pdf.EmbeddedFile, conformance Conformance) error {
	if conformance != PDFA2b && conformance != PDFA2a {
		return nil
	}
	for _, file := range files {
		if !pdfa.DeclaresPDFA12(file.Data) {
			return fmt.Errorf("%w: %q", ErrAttachmentNotPDFA, file.Name)
		}
	}
	return nil
}
