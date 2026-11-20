// Package pdf implements the backend needed to draw a document, using github.com/benoitkugler/pdf.
package pdf

import (
	"fmt"
	"sort"
	"strings"

	"github.com/polestorm/gopdfgo/internal/pdflib/model"
	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

var (
	_ backend.Document = (*Output)(nil)
	_ backend.Page     = (*outputPage)(nil)
)

// may be set to false when debugging
// gopdfgo: w źródle false (wygoda przy debugowaniu); strumienie stron, fontów i CMap kompresujemy.
const compressStreams = true

type fontContent struct {
	content []byte
	// bitmap fonts are not supported by PDF readers
	isSupported bool
}

type cache struct {
	// global shared cache for image content
	images map[int]*model.XObjectImage

	// global shared cache for fonts
	fonts map[backend.Font]pdfFont

	// global shared cache for font files.
	// The same face may be used at different sizes
	// and we don't want to duplicate the font file
	fontFiles map[text.FontOrigin]fontContent

	// transfers (gopdfgo): funkcje przejścia masek miękkich według próbek (zob. SetSoftMask), żeby
	// ta sama funkcja była zapisana w pliku raz
	transfers map[string]*model.FunctionDict
}

func newCache() cache {
	return cache{
		images:    make(map[int]*model.XObjectImage),
		fonts:     make(map[backend.Font]pdfFont),
		fontFiles: make(map[text.FontOrigin]fontContent),
		transfers: make(map[string]*model.FunctionDict),
	}
}

// Output implements backend.Output
type Output struct {
	// global map for files embedded in the PDF
	// and used in file annotations
	embeddedFiles map[string]*model.FileSpec

	// gopdfgo: osadzone pliki w kolejności zapisu (EmbeddedFiles)
	documentFiles, annotationFiles []EmbeddedFile

	cache cache

	document model.Document

	// temporary content, will be copied in the document (see `finalize`)
	pages []*outputPage
}

func NewOutput() *Output {
	out := Output{
		embeddedFiles: make(map[string]*model.FileSpec),
		cache:         newCache(),
	}
	return &out
}

func (c *Output) AddPage(left, top, right, bottom fl) backend.Page {
	out := newContextPage(left, top, right, bottom, c.embeddedFiles, c.cache)
	c.pages = append(c.pages, out)
	return out
}

func (s *Output) SetMetadata(producer string, meta utils.DocumentMetadata) {
	s.document.Trailer.Info.Title = meta.Title
	s.document.Trailer.Info.Subject = meta.Description
	s.document.Trailer.Info.Creator = meta.Generator
	s.document.Trailer.Info.Author = strings.Join(meta.Authors, ", ")
	s.document.Trailer.Info.Keywords = strings.Join(meta.Keywords, ", ")
	s.document.Trailer.Info.Producer = producer
	s.document.Trailer.Info.CreationDate = meta.Created
	s.document.Trailer.Info.ModDate = meta.Modified
}

func (c *Output) CreateAnchors(anchors [][]backend.Anchor) {
	// pages have been processed, meaning that len(anchors) == len(c.pages)

	var names []model.NameToDest
	for i, l := range anchors {
		page := c.pages[i]
		for _, anchor := range l {
			names = append(names, model.NameToDest{
				Name: model.DestinationString(anchor.Name),
				Destination: model.DestinationExplicitIntern{
					Page: &page.page,
					Location: model.DestinationLocationXYZ{
						Left: model.ObjFloat(anchor.X),
						Top:  model.ObjFloat(anchor.Y),
					},
				},
			})
		}
	}

	sort.Slice(names, func(i, j int) bool { return names[i].Name < names[j].Name })

	c.document.Catalog.Names.Dests.Names = names
}

// embedded files

// EmbeddedFile (gopdfgo) to plik osadzony w dokumencie: specyfikacja pliku zapisywana w PDF i treść
// przed kompresją, potrzebna do sprawdzenia zgodności z PDF/A.
type EmbeddedFile struct {
	Name string
	Spec *model.FileSpec
	Data []byte
}

// gopdfgo: typ MIME trafia do /Subtype strumienia pliku, a gdy go nie znamy, zapisujemy
// application/octet-stream, jak każe ISO 19005-3 § 6.8; związek z dokumentem do /AFRelationship,
// data modyfikacji do /Params /ModDate (pomijana, gdy zerowa). Źródło zapisywało tylko /UF, /Desc
// i sumę kontrolną.
func newFileSpec(a backend.Attachment) *model.FileSpec {
	stream := model.NewCompressedStream(a.Content)
	subtype := a.MediaType
	if subtype == "" {
		subtype = "application/octet-stream"
	}
	fs := &model.FileSpec{
		UF:             a.Title,
		Desc:           a.Description,
		AFRelationship: model.Name(a.Relationship),
		EF: &model.EmbeddedFileStream{
			Stream:  stream,
			Subtype: subtype,
		},
	}
	fs.EF.Params.SetChecksumAndSize(a.Content)
	fs.EF.Params.ModDate = a.ModTime
	return fs
}

// Add global attachments to the file, which are compressed using FlateDecode filter
//
// gopdfgo: kluczem w drzewie /EmbeddedFiles jest nazwa pliku (powtórzona dostaje przyrostek " (2)",
// " (3)"...), a plik bez nazwy dostaje nazwę attachment.bin; drzewo jest posortowane według kluczy
// (ISO 32000-1 § 7.9.6). Źródło używało kluczy attachement_0, attachement_1...
func (c *Output) SetAttachments(as []backend.Attachment) {
	var files model.EmbeddedFileTree
	used := map[string]bool{}
	for _, a := range as {
		if a.Title == "" {
			a.Title = "attachment.bin"
		}
		name := a.Title
		for i := 2; used[name]; i++ {
			name = fmt.Sprintf("%s (%d)", a.Title, i)
		}
		used[name] = true
		fs := newFileSpec(a)
		files = append(files, model.NameToFile{
			Name:     name,
			FileSpec: fs,
		})
		c.documentFiles = append(c.documentFiles, EmbeddedFile{Name: name, Spec: fs, Data: a.Content})
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	sort.SliceStable(c.documentFiles, func(i, j int) bool { return c.documentFiles[i].Name < c.documentFiles[j].Name })

	c.document.Catalog.Names.EmbeddedFiles = files
}

// Embed a file. Calling this method twice with the same id
// won't embed the content twice.
func (c *Output) EmbedFile(fileID string, a backend.Attachment) {
	ptr := c.embeddedFiles[fileID] // cache the attachment by id
	if ptr != nil {
		return
	}

	c.embeddedFiles[fileID] = newFileSpec(a)
	c.annotationFiles = append(c.annotationFiles, EmbeddedFile{Name: a.Title, Spec: c.embeddedFiles[fileID], Data: a.Content})
}

// EmbeddedFiles (gopdfgo) zwraca wszystkie osadzone pliki: najpierw pliki dokumentu w kolejności
// drzewa nazw, potem pliki adnotacji w kolejności osadzania.
func (c *Output) EmbeddedFiles() []EmbeddedFile {
	return append(append([]EmbeddedFile(nil), c.documentFiles...), c.annotationFiles...)
}

func bookmarksToOutline(root []backend.BookmarkNode, pages []*outputPage) *model.Outline {
	var nodeToItem func(node backend.BookmarkNode, parent model.OutlineNode) *model.OutlineItem

	nodesToItem := func(nodes []backend.BookmarkNode, parent model.OutlineNode) *model.OutlineItem {
		var first, lastChild *model.OutlineItem
		for i, node := range nodes {
			item := nodeToItem(node, parent)
			if i == 0 {
				first = item
			}
			if lastChild != nil {
				lastChild.Next = item
			}
			lastChild = item
		}
		return first
	}

	nodeToItem = func(node backend.BookmarkNode, parent model.OutlineNode) *model.OutlineItem {
		out := &model.OutlineItem{
			Parent: parent,
			Title:  node.Label,
			Open:   node.Open,
			Dest: model.DestinationExplicitIntern{
				Page: &pages[node.PageIndex].page,
				Location: model.DestinationLocationXYZ{
					Left: model.ObjFloat(node.X),
					Top:  model.ObjFloat(node.Y),
				},
			},
		}
		out.First = nodesToItem(node.Children, out)
		return out
	}
	var outline model.Outline
	outline.First = nodesToItem(root, &outline)
	return &outline
}

func (c *Output) SetBookmarks(root []backend.BookmarkNode) {
	c.document.Catalog.Outlines = bookmarksToOutline(root, c.pages)
}

// Finalize setup and returns the final document
// DisableImageInterpolation (gopdfgo) wyłącza wygładzanie obrazów przy powiększaniu we wszystkich
// obrazach dokumentu; PDF/A zabrania wpisu Interpolate o wartości true (ISO 19005-2 § 6.2.8).
// Wywoływać po narysowaniu stron, przed Finalize.
func (c *Output) DisableImageInterpolation() {
	for _, image := range c.cache.images {
		image.Interpolate = false
	}
}

func (c *Output) Finalize() model.Document {
	pages := make([]model.PageNode, len(c.pages))
	for i, p := range c.pages {
		p.finalize()
		pages[i] = &p.page
	}
	c.document.Catalog.Pages = model.PageTree{
		Kids: pages,
	}

	// fonts
	c.writeFonts()

	return c.document
}
