// This package implements the high level parts of
// the document generation, but is still backend independant.
// It is meant to be used together with a `backend.Drawer`.
package document

import (
	"fmt"
	"net/url"
	"path"

	"golang.org/x/net/html"

	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	mt "github.com/polestorm/gopdfgo/internal/webrender/matrix"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/text/hyphen"

	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	"github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	bo "github.com/polestorm/gopdfgo/internal/webrender/html/boxes"
	"github.com/polestorm/gopdfgo/internal/webrender/html/layout"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

type (
	Color = parser.RGBA
	Box   = bo.Box
)

type fl = utils.Fl

func toF(v pr.Dimension) fl { return fl(v.Value) }

// Return the matrix for the CSS transform properties on this box (possibly nil).
func getMatrix(box_ Box) (mt.Transform, bool) {
	matrix, ok, _ := getMatrixAndFace(box_)
	return matrix, ok
}

// getMatrixAndFace (gopdfgo) zwraca macierz przekształcenia jak getMatrix i dodatkowo pełną macierz
// 4×4 (jednostkową, gdy przekształcenia nie ma), z której rysowanie sprawdza, czy widać tylną stronę
// elementu (CSS Transforms 2 § 11). Lista z funkcjami 3D (CSS Transforms 2 § 12) jest
// składana w macierz 4×4 i spłaszczana raz, na końcu: rzut prostokątny na płaszczyznę rodzica, czyli
// część 2D macierzy, jak przy transform-style: flat bez perspektywy (§ 6). Spłaszczanie każdej
// funkcji z osobna nie dałoby tego samego, np. dla rotateX() rotateY(). Macierz nieodwracalna w 3D
// (np. scaleZ(0)) daje macierz zerową, więc element nie jest rysowany. Źródło znało tylko funkcje 2D.
func getMatrixAndFace(box_ Box) (_ mt.Transform, _ bool, full mt.Matrix4) {
	// "Transforms apply to block-level and atomic inline-level elements,
	//  but do not apply to elements which may be split into
	//  multiple inline-level boxes."
	// http://www.w3.org/TR/css3-2d-transforms/#introduction
	box := box_.Box()
	// gopdfgo: translate, rotate i scale przed transform, w kolejności z CSS Transforms 2 § 6;
	// źródło znało tylko transform
	trans := pr.EffectiveTransforms(box.Style)
	if box.PageScale > 1 {
		// gopdfgo: treść strony pomniejszona, żeby zmieściła się na szerokość (ADR 0014); własne
		// przekształcenie elementu głównego jest wtedy pomijane
		originX, originY, factor := fl(box.PageScaleOriginX), fl(box.PageScaleOriginY), fl(1/box.PageScale)
		matrix := mt.New(1, 0, 0, 1, originX, originY)
		matrix.RightMultBy(mt.New(factor, 0, 0, factor, 0, 0))
		matrix.Translate(-originX, -originY)
		return matrix, true, mt.FromTransform(matrix)
	}
	if len(trans) == 0 || bo.InlineT.IsInstance(box_) {
		return mt.Transform{}, false, mt.Identity4()
	}

	borderWidth := box.BorderWidth()
	borderHeight := box.BorderHeight()
	or := box.Style.GetTransformOrigin()
	offsetX := pr.ResolvePercentage(or.Point[0].Tagged(), borderWidth).V()
	offsetY := pr.ResolvePercentage(or.Point[1].Tagged(), borderHeight).V()
	originX := fl(box.BorderBoxX() + offsetX)
	originY := fl(box.BorderBoxY() + offsetY)

	if is3D(trans) {
		originZ := float64(or.Z.Value)
		matrix := mt.Translation3D(float64(originX), float64(originY), originZ)
		for _, t := range trans {
			args := t.Dimensions
			var right mt.Matrix4
			switch t.Kind {
			case pr.Translate:
				tz := 0.
				if len(args) == 3 {
					tz = float64(args[2].Value)
				}
				right = mt.Translation3D(
					float64(pr.ResolvePercentage(args[0].Tagged(), borderWidth).V()),
					float64(pr.ResolvePercentage(args[1].Tagged(), borderHeight).V()), tz)
			case pr.Scale:
				sz := 1.
				if len(args) == 3 {
					sz = float64(args[2].Value)
				}
				right = mt.Scaling3D(float64(args[0].Value), float64(args[1].Value), sz)
			case pr.Rotate3D:
				right = mt.Rotation3D(float64(args[0].Value), float64(args[1].Value), float64(args[2].Value), float64(args[3].Value))
			case pr.Matrix3D:
				var v [16]float64
				for i := range v {
					v[i] = float64(args[i].Value)
				}
				right = mt.FromColumns(v)
				// składowe perspektywy (m14, m24, m34) pomijamy jak perspective(); m44 zostaje i dzieli
				// współrzędne przy spłaszczaniu
				right[3][0], right[3][1], right[3][2] = 0, 0, 0
			case pr.Perspective:
				// perspektywa pominięta (ostrzeżenie przy odczycie stylu): macierz jednostkowa
				right = mt.Identity4()
			default:
				right = mt.FromTransform(transform2D(t, borderWidth, borderHeight))
			}
			matrix = mt.Mul4(matrix, right)
		}
		matrix = mt.Mul4(matrix, mt.Translation3D(-float64(originX), -float64(originY), -originZ))
		if !matrix.Invertible() {
			return mt.Transform{}, true, matrix
		}
		return matrix.Flatten(), true, matrix
	}

	matrix := mt.New(1, 0, 0, 1, originX, originY)
	for _, t := range trans {
		matrix.RightMultBy(transform2D(t, borderWidth, borderHeight)) // same as matrix = mt.Mul(matrix, rightMat)
	}
	matrix.Translate(-originX, -originY) // same as matrix = mt.Mul(matrix, mt.New(1, 0, 0, 1, -originX, -originY))
	// gopdfgo: przekształcenie 2D nie pokazuje tylnej strony (macierz 4×4 do sprawdzenia w rysowaniu)
	return matrix, true, mt.FromTransform(matrix)
}

// is3D (gopdfgo) mówi, czy lista ma funkcję 3D (CSS Transforms 2 § 12).
func is3D(trans pr.Transforms) bool {
	for _, t := range trans {
		switch t.Kind {
		case pr.Matrix3D, pr.Rotate3D, pr.Perspective:
			return true
		case pr.Translate, pr.Scale:
			if len(t.Dimensions) == 3 {
				return true
			}
		}
	}
	return false
}

// transform2D zwraca macierz funkcji przekształcenia 2D.
func transform2D(t pr.Transform, borderWidth, borderHeight pr.Float) mt.Transform {
	name, args := t.Kind, t.Dimensions
	// The length of args depends on `name`, see package validation for details.
	rightMat := mt.Identity()
	switch name {
	case pr.Scale:
		sx, sy := toF(args[0]), toF(args[1])
		rightMat.Scale(sx, sy)
	case pr.Rotate:
		angle := toF(args[0])
		rightMat.Rotate(angle)
	case pr.Translate:
		translateX, translateY := args[0], args[1]
		rightMat.Translate(
			fl(pr.ResolvePercentage(translateX.Tagged(), borderWidth).V()),
			fl(pr.ResolvePercentage(translateY.Tagged(), borderHeight).V()),
		)
	case pr.Skew:
		rightMat.Skew(toF(args[0]), toF(args[1]))
	case pr.Matrix:
		rightMat = mt.New(toF(args[0]), toF(args[1]), toF(args[2]),
			toF(args[3]), toF(args[4]), toF(args[5]))
	default:
		panic(fmt.Sprintf("unexpected name for CSS transform property : %d", name))
	}
	return rightMat
}

// Apply a transformation matrix to an axis-aligned rectangle
// and return its axis-aligned bounding box as “(x_min, y_min, x_max, y_max)“
func rectangleAabb(matrix mt.Transform, posX, posY, width, height fl) [4]fl {
	x1, y1 := matrix.Apply(posX, posY)
	x2, y2 := matrix.Apply(posX+width, posY)
	x3, y3 := matrix.Apply(posX, posY+height)
	x4, y4 := matrix.Apply(posX+width, posY+height)
	boxX1 := utils.Mins(x1, x2, x3, x4)
	boxY1 := utils.Mins(y1, y2, y3, y4)
	boxX2 := utils.Maxs(x1, x2, x3, x4)
	boxY2 := utils.Maxs(y1, y2, y3, y4)
	return [4]fl{boxX1, boxY1, boxX2, boxY2}
}

// Link is a positionned link in a page.
type Link struct {
	// Type is one of three strings :
	// - "external": `target` is an absolute URL
	// - "internal": `target` is an anchor name
	//   The anchor might be defined in another page,
	//   in multiple pages (in which case the first occurence is used),
	//   or not at all.
	// - "attachment": `target` is an absolute URL and points
	//   to a resource to attach to the document.
	Type pr.Keyword

	Target string

	// [x_min, y_min, x_max, y_max] in CSS
	// pixels from the top-left of the page.
	Rectangle [4]fl

	// gopdfgo: element, z którego pochodzi odnośnik (dla PDF z tagami, ADR 0033); svg – odnośnik
	// elementu <a> wewnątrz obrazu SVG, element to wtedy element obrazu
	element *html.Node
	svg     bool
	// svgIndex: numer elementu <a> w obrazie SVG (svg.LinkArea.Index); odnośniki tego samego elementu
	// na kolejnych stronach (wycinki obrazu krojonego) należą do jednego elementu struktury Link
	svgIndex int
}

type bookmarkData struct {
	label    string
	open     bool
	position [2]fl
	level    int
}

// gopdfgo: fields – pola podpisu (signature_fields.go)
func gatherAnchors(box_ bo.Box, anchors anchors, links *[]Link, bookmarks *[]bookmarkData, fields *[]signatureField, matrix *mt.Transform) {
	if transform, hasTransform := getMatrix(box_); hasTransform {
		if matrix != nil {
			t := mt.Mul(*matrix, transform)
			matrix = &t
		} else {
			matrix = &transform
		}
	}
	box := box_.Box()
	bookmarkLabel := box.BookmarkLabel
	bookmarkLevel := 0
	if lvl := box.Style.GetBookmarkLevel(); lvl.Tag != pr.None {
		bookmarkLevel = lvl.I
	}
	state := box.Style.GetBookmarkState()
	link := box.Style.GetLink()
	anchorName := string(box.Style.GetAnchor())
	hasBookmark := bookmarkLabel != "" && bookmarkLevel != 0
	// "link" is inherited but redundant on text boxes
	hasLink := !link.IsNone() && !(bo.TextT.IsInstance(box_) || bo.LineT.IsInstance(box_))
	// In case of duplicate IDs, only the first is an anchor.
	_, inAnchors := anchors[anchorName]
	hasAnchor := anchorName != "" && !inAnchors
	isAttachment := box.IsAttachment()
	gatherSignatureField(box_, fields, matrix) // gopdfgo: zob. signature_fields.go

	if hasBookmark || hasLink || hasAnchor {
		posX, posY, width, height := bo.HitArea(box_).Unpack()
		if hasLink {
			linkType, target := link.Tag, link.S
			if linkType == pr.External && isAttachment {
				linkType = pr.Attachment
			}
			linkS := Link{Type: linkType, Target: target, element: box.Element}
			if matrix != nil {
				linkS.Rectangle = rectangleAabb(*matrix, posX, posY, width, height)
			} else {
				linkS.Rectangle = [4]fl{posX, posY, posX + width, posY + height}
			}
			*links = append(*links, linkS)
		}
		if hasBookmark {
			if matrix != nil {
				posX, posY = matrix.Apply(posX, posY)
			}
			*bookmarks = append(*bookmarks, bookmarkData{
				level: bookmarkLevel, label: bookmarkLabel,
				position: [2]fl{posX, posY}, open: state == "open",
			})
		}
		if hasAnchor {
			posX1, posY1, posX2, posY2 := posX, posY, posX+width, posY+height
			if matrix != nil {
				posX1, posY1 = matrix.Apply(posX1, posY1)
				posX2, posY2 = matrix.Apply(posX2, posY2)
			}
			anchors[anchorName] = [4]fl{posX1, posY1, posX2, posY2}
		}
	}

	for _, child := range box_.AllChildren() {
		gatherAnchors(child, anchors, links, bookmarks, fields, matrix)
	}
}

type anchors = map[string][4]fl

// Page represents a single rendered page.
type Page struct {
	pageBox *bo.PageBox

	// The `dict` mapping each anchor name to its target, an
	// `(x, y)` point in CSS pixels from the top-left of the page.
	anchors anchors

	// `bookmarkLevel` and `bookmarkLabel` are based on
	// the CSS properties of the same names. `target` is an `(x, y)`
	// point in CSS pixels from the top-left of the page.
	bookmarks []bookmarkData

	links []Link
	// svgLinks (gopdfgo): odnośniki elementów <a> obrazów SVG (gatherSVGLinks), zbierane po narysowaniu
	// strony; w PDF leżą za odnośnikami HTML, więc nad nimi
	svgLinks []Link
	// signatureFields (gopdfgo): pola podpisu elementów z atrybutem data-pdf-signature (signature_fields.go)
	signatureFields []signatureField

	// The page bleed widths with values in CSS pixels.
	Bleed bo.Bleed

	// The page width, including margins, in CSS pixels.
	Width fl

	// The page height, including margins, in CSS pixels.
	Height fl

	// tags (gopdfgo): oznaczanie treści w PDF z tagami (ADR 0033); nil bez tagów; index – numer strony
	tags  *tagger
	index int
}

// newPage post-process a laid out `PageBox`.
func newPage(pageBox *bo.PageBox) Page {
	d := Page{}
	d.Width = fl(pageBox.MarginWidth())
	d.Height = fl(pageBox.MarginHeight())

	d.Bleed = pageBox.Bleed()
	d.anchors = anchors{}

	gatherAnchors(pageBox, d.anchors, &d.links, &d.bookmarks, &d.signatureFields, nil)
	d.pageBox = pageBox
	return d
}

// Paint the page on `dst`.
// leftX is the X coordinate of the left of the page, in user units.
// topY is the Y coordinate of the top of the page, in user units.
// scale is the Zoom scale in user units per CSS pixel.
// clip : whether to clip/cut content outside the page. If false, content can overflow.
// (leftX=0, topY=0, scale=1, clip=false)
func (d Page) Paint(dst backend.Page, fc text.FontConfiguration, leftX, topY, scale fl, clip bool) {
	dst.OnNewStack(func() {
		// Make (0, 0) the top-left corner and make user units CSS pixels
		dst.State().Transform(mt.New(scale, 0, 0, scale, leftX, topY))
		if clip {
			dst.Rectangle(0, 0, d.Width, d.Height)
			dst.State().Clip(false)
		}
		ctx := drawContext{
			dst:               dst,
			fonts:             fc,
			hyphenCache:       make(map[text.HyphenDictKey]hyphen.Hyphener),
			strutLayoutsCache: make(map[text.StrutLayoutKey][2]pr.Float),
			filteredImages:    make(map[string]*filteredImage), // gopdfgo: zob. filterRasterImage
			tags:              d.tags,                          // gopdfgo: zob. tags.go
			pageIndex:         d.index,
		}
		ctx.drawPage(d.pageBox)
	})
}

// Document is a rendered document ready to be painted on a drawing target.
//
// It is obtained by calling the `Render()` function.
type Document struct {
	// A list of `Page` objects.
	Pages []Page

	// A function called to fetch external resources such
	// as stylesheets and images.
	urlFetcher utils.UrlFetcher

	fontconfig text.FontConfiguration

	// A `DocumentMetadata` object.
	// Contains information that does not belong to a specific page
	// but to the whole document.
	Metadata utils.DocumentMetadata

	// ReleaseDrawnPages (gopdfgo): Write zwalnia drzewo pudełek strony zaraz po jej narysowaniu.
	// Złożone strony żyją inaczej do końca zapisu, a po narysowaniu nic ich nie czyta (odnośniki,
	// kotwice i zakładki są zebrane wcześniej w newPage i resolveLinks). Przy 1021 stronach z
	// ilustracjami szczyt pamięci spada o około jedną trzecią. Dokumentu nie da się potem narysować
	// drugi raz, dlatego to opcja, a nie zachowanie domyślne.
	ReleaseDrawnPages bool

	// gopdfgo: root – element główny dokumentu, z którego powstaje drzewo struktury PDF z tagami;
	// tags – stan oznaczania treści, tworzony przy pierwszej stronie celu z tagami (ADR 0033)
	root *html.Node
	tags *tagger
}

// Render performs the layout of the whole document and returns a document
// ready to be painted.
//
// fontConfig is mandatory
// presentationalHints should default to `false`
func Render(html *tree.HTML, stylesheets []tree.CSS, presentationalHints bool, fontConfig text.FontConfiguration) Document {
	pageBoxes := layout.Layout(html, stylesheets, presentationalHints, fontConfig)
	pages := make([]Page, len(pageBoxes))
	for i, pageBox := range pageBoxes {
		pages[i] = newPage(pageBox)
	}
	return Document{Pages: pages, Metadata: html.GetMetadata(), urlFetcher: html.UrlFetcher, fontconfig: fontConfig,
		root: html.Root.AsHtmlNode()}
}

// Take a subset of the pages.
//
// Examples:
// Write two PDF files for odd-numbered and even-numbered pages:
//     document.Copy(document.pages[::2]).writePdf("oddPages.pdf")
//     document.Copy(document.pages[1::2]).writePdf("evenPages.pdf")
// Combine multiple documents into one PDF file, using metadata from the first:
//		var allPages []Page
// 		for _, doc := range documents {
//		 	for _, p := range doc.pages {
//		 		allPages = append(allPages, p)
//		 	}
//		 }
//		documents[0].Copy(allPages).writePdf("combined.pdf")
// func (d Document) Copy(pages []Page, all bool) Document {
// 	if all {
// 		pages = d.Pages
// 	}
// 	return Document{Pages: pages, Metadata: d.Metadata, urlFetcher: d.urlFetcher}
// }

// Resolve internal hyperlinks.
// Links to a missing anchor are removed with a warning.
// If multiple anchors have the same name, the first one is used.
// Returns lists (one per page) like :attr:`Page.links`,
// except that “target“ for internal hyperlinks is
// “(pageNumber, x, y)“ instead of an anchor name.
// The page number is a 0-based index into the :attr:`pages` list,
// and “x, y“ have been scaled (origin is at the top-left of the page).
// gopdfgo: zwraca też zbiór nazw kotwic (dla odnośników SVG zbieranych po narysowaniu strony w Write),
// a odnośniki strony to links i za nimi svgLinks.
func (d *Document) resolveLinks() ([][]Link, [][]backend.Anchor, utils.Set) {
	anchors := utils.NewSet()
	pagedAnchors := make([][]backend.Anchor, len(d.Pages))
	for i, page := range d.Pages {
		var current []backend.Anchor
		for anchorName, pos := range page.anchors {
			if !anchors.Has(anchorName) {
				current = append(current, backend.Anchor{Name: anchorName, X: pos[0], Y: pos[1]})
				anchors.Add(anchorName)
			}
		}
		pagedAnchors[i] = current
	}
	pagedLinks := make([][]Link, len(d.Pages))
	for i, page := range d.Pages {
		// gopdfgo: filtr wspólny z odnośnikami SVG, zob. validLinks
		pagedLinks[i] = append(validLinks(page.links, anchors), validLinks(page.svgLinks, anchors)...)
	}
	return pagedLinks, pagedAnchors, anchors
}

// Make a tree of all bookmarks in the document.
func (d Document) makeBookmarkTree() []backend.BookmarkNode {
	// At one point in the document, for each "output" depth, how much
	// to add to get the source level (CSS values of bookmark-level).
	// E.g. with <h1> then <h3>, levelShifts == [0, 1]
	// 1 means that <h3> has depth 3 - 1 = 2 in the output.
	var (
		skippedLevels []int
		root          []backend.BookmarkNode
	)
	lastByDepth := []*[]backend.BookmarkNode{&root} // initialise with the root
	previousLevel := 0
	for pageNumber, page := range d.Pages {
		for _, bk := range page.bookmarks {
			level, label, pos, open := bk.level, bk.label, bk.position, bk.open
			if level > previousLevel {
				// Example: if the previous bookmark is a <h2>, the next
				// depth "should" be for <h3>. If now we get a <h6> we’re
				// skipping two levels: append 6 - 3 - 1 = 2
				skippedLevels = append(skippedLevels, level-previousLevel-1)
			} else {
				temp := level
				for temp < previousLevel {
					pop := skippedLevels[len(skippedLevels)-1]
					skippedLevels = skippedLevels[:len(skippedLevels)-1]
					temp += 1 + pop
				}
				if temp > previousLevel {
					// We remove too many "skips", add some back:
					skippedLevels = append(skippedLevels, temp-previousLevel-1)
				}
			}
			sum := 0
			for _, l := range skippedLevels {
				sum += l
			}
			previousLevel = level
			depth := level - sum
			if depth != len(skippedLevels) || depth < 1 {
				panic(fmt.Sprintf("expected depth >= 1 and depth == len(skippedLevels) got %d", depth))
			}
			subtree := backend.BookmarkNode{Label: label, PageIndex: pageNumber, X: pos[0], Y: pos[1], Open: open}
			(*lastByDepth[depth-1]) = append((*lastByDepth[depth-1]), subtree)
			lastByDepth = lastByDepth[:depth]
			tmp := *lastByDepth[depth-1]
			lastByDepth = append(lastByDepth, &tmp[len(tmp)-1].Children)
		}
	}
	return root
}

// Include hyperlinks in current PDF page.
func (d Document) addHyperlinks(links []Link, context backend.Page, scale mt.Transform) {
	tagged, _ := context.(backend.TaggedPage)
	for _, link := range links {
		// gopdfgo: adnotacja należy do elementu odnośnika w drzewie struktury (ADR 0033)
		if d.tags != nil && tagged != nil && tagged.Tagged() {
			tagged.SetAnnotationOwner(d.tags.linkOwner(link), linkContents(link))
		}
		linkType, linkTarget, rectangle := link.Type, link.Target, link.Rectangle
		xMin, yMin := scale.Apply(rectangle[0], rectangle[1])
		xMax, yMax := scale.Apply(rectangle[2], rectangle[3])
		if linkType == pr.External {
			context.AddExternalLink(xMin, yMin, xMax, yMax, linkTarget)
		} else if linkType == pr.Internal {
			context.AddInternalLink(xMin, yMin, xMax, yMax, linkTarget)
		} else if linkType == pr.Attachment {
			// actual embedding has be done previously
			context.AddFileAnnotation(xMin, yMin, xMax, yMax, linkTarget)
		}
	}
}

func (d *Document) scaleAnchors(anchors []backend.Anchor, matrix mt.Transform) {
	for i, a := range anchors {
		anchors[i].X, anchors[i].Y = matrix.Apply(a.X, a.Y)
	}
}

func (d *Document) fetchAttachment(attachmentUrl string) backend.Attachment {
	// Attachments from document links like <link> or <a> can only be URLs.
	// gopdfgo: zasób pobierany wprost przez urlFetcher (u nas Options.Resolver), bez dekodowania tekstu,
	// bo załącznik to bajty pliku; typ MIME z odpowiedzi trafia do /Subtype (ISO 19005-3 § 6.8). Związek
	// z dokumentem jest nieznany, więc /AFRelationship to Unspecified (ISO 32000-2 § 7.11.3, tabela 43:
	// wartość dla związku nieznanego). Źródło czytało zasób przez FetchSource i gubiło typ MIME.
	result, err := d.urlFetcher(attachmentUrl)
	if err != nil {
		logger.WarningLogger.Printf("Failed to load attachment at url %s: %s\n", attachmentUrl, err)
		return backend.Attachment{}
	}
	var source []byte
	if result.Content != nil {
		source = make([]byte, result.Content.Len())
		_, _ = result.Content.Read(source)
	}
	location := result.RedirectedUrl
	if location == "" {
		location = attachmentUrl
	}
	filename := getFilenameFromResult(location)
	return backend.Attachment{Content: source, Title: filename, MediaType: result.MimeType, Relationship: "Unspecified"}
}

// Derive a filename from a fetched resource.
// This is either the filename returned by the URL fetcher, the last URL path
// component or a synthetic name if the URL has no path.
func getFilenameFromResult(rawurl string) string {
	var filename string

	// The URL path likely contains a filename, which is a good second guess
	if rawurl != "" {
		u, err := url.Parse(rawurl)
		if err == nil {
			if u.Scheme != "data" {
				filename = path.Base(u.Path)
			}
		}
	}

	if filename == "" {
		// The URL lacks a path altogether. Use a synthetic name.

		// Using guessExtension is a great idea, but sadly the extension is
		// probably random, depending on the alignment of the stars, which car
		// you're driving and which software has been installed on your machine.
		extension := ".bin"
		filename = "attachment" + extension
	} else {
		filename = utils.Unquote(filename)
	}

	return filename
}

// Write paints the pages in the given `target`, with meta-data.
//
// The zoom factor is in PDF units per CSS units, and should default to 1.
// Warning : all CSS units are affected, including physical units like
// `cm` and named sizes like `A4`.  For values other than
// 1, the physical CSS units will thus be "wrong".
//
// `attachments` is an optional list of additional file attachments for the
// generated PDF document, added to those collected from the metadata.
func (d *Document) Write(target backend.Document, zoom pr.Fl, attachments []backend.Attachment) {
	// 0.75 = 72 PDF point per inch / 96 CSS pixel per inch
	scale := zoom * 0.75

	// Links and anchors
	pagedLinks, pagedAnchors, anchorNames := d.resolveLinks()
	pagedFields := resolveSignatureFields(d.Pages) // gopdfgo: zob. signature_fields.go

	// files must be embedded before being used on the pages
	d.embedFileAnnotations(pagedLinks, target)

	logger.ProgressLogger.Println("Step 6 - Drawing pages")

	for i, page := range d.Pages {
		pageWidth := scale * (page.Width + fl(page.Bleed.Left) + fl(page.Bleed.Right))
		pageHeight := scale * (page.Height + fl(page.Bleed.Top) + fl(page.Bleed.Bottom))
		left := -scale * fl(page.Bleed.Left)
		top := -scale * fl(page.Bleed.Top)
		right := left + pageWidth
		bottom := top + pageHeight

		outputPage := target.AddPage(left/scale, top/scale, (right-left)/scale, (bottom-top)/scale)
		outputPage.State().Transform(mt.New(1, 0, 0, -1, 0, page.Height*scale))
		page.tags, page.index = d.pageTagger(outputPage), i // gopdfgo: zob. tags.go
		page.Paint(outputPage, d.fontconfig, 0, 0, scale, false)
		// gopdfgo: odnośniki SVG po narysowaniu strony (zob. gatherSVGLinks), za odnośnikami HTML jak w Stream
		var svgLinks []Link
		gatherSVGLinks(page.pageBox, &svgLinks, nil)
		pagedLinks[i] = append(pagedLinks[i], validLinks(svgLinks, anchorNames)...)

		// Draw from the top-left corner
		matrix := mt.New(scale, 0, 0, -scale, 0, page.Height*scale)

		d.addHyperlinks(pagedLinks[i], outputPage, matrix)
		d.addSignatureFields(pagedFields[i], outputPage, matrix) // gopdfgo: zob. signature_fields.go
		d.scaleAnchors(pagedAnchors[i], matrix)
		setMediaBoxes(page.Bleed, [4]fl{left, top, right, bottom}, outputPage)
		// gopdfgo: page-orientation obraca stronę po ułożeniu, przy prezentacji (CSS Page 3 § 7.2);
		// w PDF to wpis /Rotate strony, w stopniach zgodnie z ruchem wskazówek zegara
		if rotatable, ok := outputPage.(backend.RotatablePage); ok {
			switch page.pageBox.Style.GetPageOrientation() {
			case "rotate-right":
				rotatable.SetRotation(90)
			case "rotate-left":
				rotatable.SetRotation(270)
			}
		}
		if d.ReleaseDrawnPages {
			d.Pages[i].pageBox = nil // gopdfgo: zob. ReleaseDrawnPages
		}
	}

	target.CreateAnchors(pagedAnchors)

	logger.ProgressLogger.Println("Step 7 - Adding PDF metadata")

	// embedded files
	as := attachments
	for _, a := range d.Metadata.Attachments {
		t := d.fetchAttachment(a.URL)
		t.Description = a.Title // gopdfgo: atrybut title elementu <link> jako opis pliku, jak w WeasyPrint
		if len(t.Content) != 0 {
			as = append(as, t)
		}
	}
	target.SetAttachments(as)

	// Set bookmarks
	target.SetBookmarks(d.makeBookmarkTree())

	// Set document information
	target.SetMetadata(utils.VersionString, d.Metadata)
	d.setStructure(target) // gopdfgo: zob. tags.go
}

func (d *Document) embedFileAnnotations(pagedLinks [][]Link, context backend.Document) {
	// A single link can be split in multiple regions.
	for _, rl := range pagedLinks {
		for _, link := range rl {
			if link.Type == pr.Attachment {
				a := d.fetchAttachment(link.Target)
				if len(a.Content) != 0 {
					context.EmbedFile(link.Target, a)
				}
			}
		}
	}
}

func setMediaBoxes(bleed bo.Bleed, mediaBox [4]fl, target backend.Page) {
	bleed.Top *= 0.75
	bleed.Bottom *= 0.75
	bleed.Left *= 0.75
	bleed.Right *= 0.75

	// Add bleed box
	left, top, right, bottom := mediaBox[0], mediaBox[1], mediaBox[2], mediaBox[3]

	trimLeft := left + fl(bleed.Left)
	trimTop := top + fl(bleed.Top)
	trimRight := right - fl(bleed.Right)
	trimBottom := bottom - fl(bleed.Bottom)

	// Arbitrarly set PDF BleedBox between CSS bleed box (PDF MediaBox) and
	// CSS page box (PDF TrimBox), at most 10 px from the TrimBox.
	bleedLeft := trimLeft - min(10, fl(bleed.Left))
	bleedTop := trimTop - min(10, fl(bleed.Top))
	bleedRight := trimRight + min(10, fl(bleed.Right))
	bleedBottom := trimBottom + min(10, fl(bleed.Bottom))

	target.SetMediaBox(left, top, right, bottom)
	target.SetTrimBox(trimLeft, trimTop, trimRight, trimBottom)
	target.SetBleedBox(bleedLeft, bleedTop, bleedRight, bleedBottom)
}
