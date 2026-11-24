package htmlpdf

import (
	"errors"
	"fmt"
	"github.com/polestorm/gopdfgo/internal/pdfa"
	"time"

	"github.com/polestorm/gopdfgo/internal/goweasyprint/pdf"
	"github.com/polestorm/gopdfgo/internal/webrender/backend"
)

// Relationship to związek pliku dołączonego z dokumentem, zapisywany jako /AFRelationship w słowniku
// specyfikacji pliku (ISO 32000-2 § 7.11.3, tabela 43; ISO 19005-3 § 6.8).
type Relationship string

const (
	// RelationshipSource: plik źródłowy, z którego powstał dokument (np. arkusz, z którego wydrukowano tabelę).
	RelationshipSource Relationship = "Source"
	// RelationshipData: dane, z których powstała treść dokumentu (np. dane wykresu).
	RelationshipData Relationship = "Data"
	// RelationshipAlternative: inna postać tej samej treści (np. XML faktury Factur-X/ZUGFeRD).
	RelationshipAlternative Relationship = "Alternative"
	// RelationshipSupplement: uzupełnienie treści dokumentu.
	RelationshipSupplement Relationship = "Supplement"
	// RelationshipUnspecified: związek nieznany albo inny niż powyższe; wartość domyślna.
	RelationshipUnspecified Relationship = "Unspecified"
)

// Attachment to plik osadzany w dokumencie na poziomie całego dokumentu (drzewo nazw /EmbeddedFiles
// katalogu, a w trybie PDF/A-3 także tablica /AF katalogu).
type Attachment struct {
	// Name to nazwa pliku (/F i /UF specyfikacji pliku, klucz w drzewie nazw); wymagana i niepowtarzalna.
	Name string
	// Data to treść pliku.
	Data []byte
	// MediaType to typ MIME, np. "text/xml", zapisywany jako /Subtype strumienia pliku. Pusty oznacza
	// "application/octet-stream" (ISO 19005-3 § 6.8). Parametry (";charset=...") są pomijane.
	MediaType string
	// Description to opis pliku (/Desc); może być pusty.
	Description string
	// Relationship to związek z dokumentem; pusty oznacza RelationshipUnspecified.
	Relationship Relationship
	// ModTime to data modyfikacji pliku (/Params /ModDate). Wartość zerowa: wpis jest pomijany, bo
	// ISO 32000-1 tabela 46 czyni go opcjonalnym, a ISO 19005-3 § 6.8 go nie wymaga; silnik nie wstawia
	// bieżącego czasu, żeby wynik był powtarzalny.
	ModTime time.Time
}

var (
	// ErrInvalidAttachment: załącznik z Options.Attachments bez nazwy, z nazwą powtórzoną albo z nieznaną
	// wartością Relationship.
	ErrInvalidAttachment = errors.New("gopdfgo: invalid attachment")
	// ErrAttachmentNotPDFA: w trybie PDF/A-2b dokument ma załącznik, który nie jest plikiem PDF/A-1 ani
	// PDF/A-2, a tego zabrania ISO 19005-2 § 6.8. Załączniki dowolnego typu dopuszcza PDF/A-3b.
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
