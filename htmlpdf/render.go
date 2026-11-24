package htmlpdf

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"github.com/polestorm/gopdfgo/internal/htmlinput"
	"io"
	"log"
	"math"
	"mime"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/go-text/typesetting/fontscan"
	"github.com/polestorm/gopdfgo/internal/goweasyprint/pdf"
	pdfmodel "github.com/polestorm/gopdfgo/internal/pdflib/model"
	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	"github.com/polestorm/gopdfgo/internal/webrender/html/document"
	"github.com/polestorm/gopdfgo/internal/webrender/html/layout"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/text/hyphen"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// EngineName and EngineVersion identify the engine in the result; the recipient stores them with the copy of
// the document. The version is raised with every change that alters the output bytes on the corpus.
const (
	EngineName    = "gopdfgo/webrender"
	EngineVersion = "0.1.0"
)

// Errors returned by Render; check them with errors.Is.
var (
	// ErrNoFonts: Options.Fonts contains no font and system fonts are turned off (or turned on, but the
	// system directories contain no font that can be embedded).
	ErrNoFonts = errors.New("gopdfgo: no fonts provided")
	// ErrSystemFontsNotBuilt: Options.SystemFonts in a binary built without the systemfonts build tag
	// (ADR 0039). The default variant does not contain the code that scans the system font directories.
	ErrSystemFontsNotBuilt = errors.New("gopdfgo: system fonts not compiled in (build with -tags systemfonts)")
	// ErrEngine: the engine ended with a panic; the message contains the stack trace.
	ErrEngine = errors.New("gopdfgo: engine failure")
	// ErrResourceDenied: returned by DenyAll for every resource other than data:.
	ErrResourceDenied = errors.New("gopdfgo: resource denied")
)

// Font is a single font file (TTF or OTF) available to the document.
type Font struct {
	Name string // name used in messages, e.g. the file name
	Data []byte
}

// Resource is a resource returned by a Resolver.
type Resource struct {
	Data      []byte
	MediaType string // e.g. "text/css"; empty means detection by the extension of the address
}

// Resolver supplies the resources the document refers to through url(...), src and href. It receives an absolute
// address, resolved against Options.BaseURL. The engine does not pass data: addresses; it handles them itself.
type Resolver func(url string) (Resource, error)

// DenyAll refuses every resource; it is the default Resolver.
func DenyAll(url string) (Resource, error) {
	return Resource{}, fmt.Errorf("%w: %s", ErrResourceDenied, url)
}

// Options configures rendering. The zero value is not enough: fonts are required.
type Options struct {
	// Fonts are the only fonts the engine will use, also for generic families and fallback characters,
	// unless SystemFonts is turned on.
	Fonts []Font
	// SystemFonts allows fonts installed in the system to be used as fallback fonts (ADR 0039). It works only
	// in a binary built with the systemfonts build tag (go build -tags systemfonts); in the default variant
	// Render fails with ErrSystemFontsNotBuilt. Fonts from Fonts and families from GenericFamilies take
	// precedence: a system font is used only for a family that is not in Fonts (not even through
	// a metric-compatible substitution) and for a character missing from the font chosen from Fonts (CSS Fonts 4
	// § 5). Fonts may then be empty. Every system font used gives a warning with the family name and the file
	// path: adding that file to Fonts makes the output independent of the machine. Fonts whose licence forbids
	// embedding (OS/2 fsType) are skipped with a warning. The output depends on the fonts installed
	// on the machine, so it is not reproducible across machines.
	// The system font directories are scanned on the first rendering with SystemFonts in the process,
	// and their index stays in memory until the process ends; the engine writes nothing to disk.
	SystemFonts bool
	// Resolver supplies external resources; nil means DenyAll.
	Resolver Resolver
	// BaseURL is the address against which relative addresses in the document are resolved.
	BaseURL string
	// UserCSS is the user stylesheet (lower priority than the document's stylesheets), e.g. a default @page.
	UserCSS string
	// GenericFamilies tells which families from Fonts to use for the CSS generic families. The key is the name
	// of a generic family ("serif", "sans-serif", "monospace", "cursive", "fantasy", "system-ui"), the value is
	// the font family names in the order they are tried, e.g. {"serif": {"Tinos"}}. It also applies to the
	// document's default font, because the initial value of font-family is serif. A generic family without an
	// entry, or with an entry whose families are not in Fonts, is resolved as before (ADR 0005).
	GenericFamilies map[string][]string
	// MarginsEscapeHeightLimits switches margin collapsing at min-height and max-height. By default
	// (false), as in Chromium, the bottom margin of the last child stays inside the box whose height
	// was changed by the limit. The value true gives the behaviour of the literal text of CSS 2.2 § 10.7:
	// the margin collapses with the parent's margin and pushes the next box away (ADR 0006).
	MarginsEscapeHeightLimits bool
	// MonospaceFontSize is the size in pixels of font-size: medium when font-family is the single
	// keyword monospace (<pre>, <code>, <kbd>, <samp>, <tt>). Zero means 13, as in browsers;
	// 16 turns this rule off and gives type of the same size as the rest of the text (ADR 0008).
	MonospaceFontSize float64
	// ExactLengths chooses between a printout like Chromium's and fidelity to the stylesheet (ADR 0013).
	// By default (false) the lengths that Chromium rounds to whole CSS pixels are rounded the same way:
	//   - border-width, outline-width and column-rule-width: values between 0 and 1 px grow to 1 px,
	//     larger ones are truncated down (0.5pt gives 0.75 pt, 1pt gives 0.75 pt, 1.5pt gives 1.5 pt; ADR 0009);
	//   - border-spacing: truncated down to whole pixels (1mm gives 3 px);
	//   - page area: width and height up to whole pixels, left and top margin to the nearest
	//     pixel (A4 with 15 mm margins: 681 px instead of 680.31; ADR 0021, 0013);
	//   - the font size the text is set in: truncated to 0.01 px (9.625 px gives 9.62 px; em
	//     is still computed from the full font-size);
	//   - box lengths (margins, paddings, width, height, their limits, top/left…), the line height
	//     from line-height and the shares of the excess in a fixed-layout table: truncated to 1/64 px like
	//     LayoutUnit in Blink (padding 2.2px gives 2.1875 px), and a line fits content wider than itself
	//     by less than 1/64 px; the leading above the text down to a whole pixel (ADR 0013, addendum
	//     of 2026-11-21);
	//   - width of a text fragment (the part of a text box within a line, also for min-/max-content): the exact
	//     sum of the character widths up to 1/64 px (ADR 0044);
	//   - width of the page area enlarged when the content is shrunk (MaxShrink): down to whole
	//     pixels (ADR 0044).
	// The value true lays out and draws all lengths exactly as given, with the precision
	// that PDF allows; the output then differs from Chromium's printout.
	ExactLengths bool
	// MaxShrink limits the shrinking of content wider than the page area (ADR 0014). When something sticks
	// out past the right edge of the page area (a fixed width, a wide table, a long line with no break),
	// all the content is scaled down so that it fits, by a factor of at most MaxShrink; the margin boxes
	// stay at scale 1. Zero means 1.5, as in Chromium; 1 turns shrinking off (the content sticks out
	// and is cut off at the edge of the sheet); math.Inf(1) shrinks without a limit.
	MaxShrink float64
	// Continuous turns pagination off: the result is a single page with the width from @page, which grows
	// downwards to the height of the content and is not shorter than the page from the stylesheet. The height
	// from @page remains the basis for percentages, vh units and positioned elements, like a browser window;
	// the fragmentation properties (break-*, orphans, widows) and the repetition of table headers do not
	// apply then (ADR 0017).
	Continuous bool
	// Conformance selects the standard of the output file; the empty value is plain PDF 1.7, PDFA2b is PDF/A-2b
	// (ADR 0016), PDFA3b is PDF/A-3b with associated files (ADR 0026), PDFA2a and PDFA3a are the same
	// standards at level A, with tags (ADR 0033). In PDF/A mode the file gets XMP metadata, an sRGB profile
	// and an identifier computed from the content; the layout and appearance of the pages do not change.
	Conformance Conformance
	// Tagged produces a tagged PDF (ADR 0033): a logical structure tree built from the HTML elements in
	// document order (headings, paragraphs, lists, tables, links, images with alternative text), page
	// content marked as belonging to elements, backgrounds, borders and margin boxes marked as artifacts,
	// the language from the lang attribute and the title from <title> displayed by the reader. Screen readers
	// and text export rely on the order and meaning of the elements. The file is larger and takes a little
	// longer to produce; the appearance of the pages does not change. Conformance PDFA2a and PDFA3a turn
	// tags on by themselves. An image without alternative text and a document without a title give warnings.
	Tagged bool
	// Accessibility selects the accessibility standard: PDFUA1 is PDF/UA-1 (ISO 14289-1, ADR 0033), with tags
	// and with the PDF/UA identifier in the XMP metadata; it combines with any value of Conformance (usually
	// PDFA2a). Defects of the document that the engine does not repair (a missing title, language or image
	// alternative text, a skipped heading level) give warnings.
	Accessibility Accessibility
	// Uncompressed writes the streams of page content, fonts and ToUnicode maps without compression, so that
	// they can be read in a text editor (diagnostics). Images stay losslessly compressed at level 0, because
	// their filter is part of the format. By default (false) everything is compressed with zlib at level 6.
	Uncompressed bool
	// Hyphenation holds hyphenation dictionaries (hyphens: auto) supplied by the caller: the key is a BCP 47
	// language tag ("cs", "sv-SE"), the value is Liang patterns in UTF-8, one per line (a .pat.txt file
	// from hyph-utf8 or a Hunspell .dic dictionary). They take precedence over the built-in ones. Only
	// dictionaries under the MIT, BSD, Unlicense and FSF all-permissive licences are built in (ADR 0024): af,
	// be, bg, da, de, el, en, es, et, fr, gl, it, lt, mn, nb, nl, nn, pl, pt, sk, sl, sq, te, uk. The caller
	// is responsible for the licence of a dictionary supplied here.
	Hyphenation map[string][]byte
	// Attachments are files embedded in the document in addition to those the HTML itself points to (<link
	// rel=attachment>, <a rel=attachment>, fetched through Resolver). They go into the catalog's /EmbeddedFiles
	// tree and, in PDFA3b mode, also into the catalog's /AF array (associated files, ISO 19005-3 § 6.8), e.g.
	// a Factur-X/ZUGFeRD invoice XML as RelationshipAlternative. PDFA2b allows only attachments that are
	// PDF/A-1 or PDF/A-2 files; others make Render fail with ErrAttachmentNotPDFA (ADR 0026).
	Attachments []Attachment
	// FacturX produces a Factur-X 1.0 / ZUGFeRD 2.x hybrid invoice (ADR 0034): the CII invoice XML embedded
	// as factur-x.xml (xrechnung.xml for the XRECHNUNG profile) with the Data or Alternative relationship
	// according to the profile, fx XMP metadata with the description of the extension schema, and the PDF/A-3
	// standard: an empty Conformance gives PDFA3b, and with tags (Tagged, Accessibility) PDFA3a; PDFA2b
	// and PDFA2a make Render fail with ErrInvalidFacturX. XML that is not a CII invoice also makes Render fail
	// with that error; a profile that disagrees with the specification identifier in the XML gives a warning.
	FacturX *FacturX
	// FontCache keeps parsed fonts (from Fonts and from @font-face rules) for subsequent Render calls
	// (ADR 0045); nil means that fonts are parsed on every call. The output is the same.
	FontCache *FontCache
}

// Warning is a report from the engine that does not interrupt rendering: an unsupported property or value,
// a resource that could not be loaded, an unknown selector.
type Warning struct {
	Message string
}

// Result is the outcome of rendering.
type Result struct {
	PDF           []byte
	Pages         int
	Warnings      []Warning
	EngineName    string
	EngineVersion string
}

// engineSlot dopuszcza jedno renderowanie naraz: przejęty silnik zgłasza ostrzeżenia przez zmienną
// globalną (logger.WarningLogger) i nie jest sprawdzony pod kątem współbieżności.
var engineSlot = make(chan struct{}, 1)

// Render converts an HTML document to PDF.
//
// Renderings are executed one after another. The engine has no cancellation points, so when ctx expires during
// the work, Render returns ctx.Err() while the work continues in the background until it finishes, and until
// then subsequent calls wait (or return with their own ctx.Err()).
func Render(ctx context.Context, html []byte, opts Options) (Result, error) {
	if opts.SystemFonts && !systemFontsBuilt {
		return Result{}, ErrSystemFontsNotBuilt
	}
	if len(opts.Fonts) == 0 && !opts.SystemFonts {
		return Result{}, ErrNoFonts
	}
	checked := opts // sprawdzenie przed kolejką; render przygotowuje opcje sam
	if _, _, err := prepareFacturX(&checked); err != nil {
		return Result{}, err
	}
	if _, err := backendAttachments(checked.Attachments); err != nil {
		return Result{}, err
	}
	select {
	case engineSlot <- struct{}{}:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}

	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		defer func() { <-engineSlot }()
		result, err := render(html, opts)
		done <- outcome{result, err}
	}()

	select {
	case out := <-done:
		return out.result, out.err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

func render(html []byte, opts Options) (result Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v\n%s", ErrEngine, r, debug.Stack())
		}
	}()

	// Stopień 6 (domyślny zlib): pomiar 2026-11-20 na fakturze i dwóch sprawozdaniach e-SF dał pliki
	// o 9-32% mniejsze niż stopień 1 kosztem ok. 3% czasu; stopień 9 zmniejszał je o dalsze 0-2%,
	// ale wydłużał renderowanie o kolejne 8-13%.
	level := 6
	if opts.Uncompressed {
		level = zlib.NoCompression
	}
	pdfmodel.SetCompressionLevel(level)
	fx, invoiceWarnings, err := prepareFacturX(&opts)
	if err != nil {
		return Result{}, err
	}
	attachments, err := backendAttachments(opts.Attachments)
	if err != nil {
		return Result{}, err
	}
	if err := hyphen.SetUserDictionaries(opts.Hyphenation); err != nil {
		return Result{}, fmt.Errorf("gopdfgo: %w", err)
	}
	defer hyphen.SetUserDictionaries(nil)

	var warnings bytes.Buffer
	previousWarnings, previousProgress := logger.WarningLogger.Writer(), logger.ProgressLogger.Writer()
	logger.WarningLogger.SetOutput(&warnings)
	logger.ProgressLogger.SetOutput(io.Discard)
	defer func() {
		logger.WarningLogger.SetOutput(previousWarnings)
		logger.ProgressLogger.SetOutput(previousProgress)
	}()

	// Fonty parsujemy przez pamięć podręczną (ADR 0045): podaną w opcjach, współdzieloną między
	// renderowaniami, albo tylko na to renderowanie, żeby ten sam plik w Options.Fonts i w @font-face
	// nie był parsowany dwa razy.
	fontCache := opts.FontCache
	if fontCache == nil {
		fontCache = &FontCache{cache: text.NewTransientFontCache()}
	}
	fontMap := fontscan.NewFontMap(log.New(io.Discard, "", 0))
	fontConfig := text.NewFontConfigurationGotext(fontMap)
	fontConfig.ReuseLoadedFaces = true
	fontConfig.Cache = fontCache.cache
	// identyfikatory w kolejności Options.Fonts: przy dwóch fontach o tej samej nazwie rodziny (także
	// zlokalizowanej) wygrywa podany wcześniej; rejestracja w kolejności iteracji po mapie losowała
	// zwycięzcę przy każdym uruchomieniu (test WPT css-fonts/font-family-name-010). Fonty są
	// rejestrowane ze streszczenia, a parsowane w całości przy pierwszym użyciu (ADR 0047).
	for i, font := range opts.Fonts {
		id := fmt.Sprintf("gopdfgo-font-%d:%s", i, font.Name)
		if err := fontConfig.AddFontFile(id, font.Data); err != nil {
			return Result{}, fmt.Errorf("gopdfgo: font %q: %w", font.Name, err)
		}
	}
	if len(opts.GenericFamilies) != 0 {
		fontConfig.GenericFamilies = map[string][]string{}
		for generic, families := range opts.GenericFamilies {
			fontConfig.GenericFamilies[strings.ToLower(strings.TrimSpace(generic))] = families
		}
	}
	var system systemFontSource
	if opts.SystemFonts {
		if system, err = openSystemFonts(); err != nil {
			return Result{}, err
		}
		if len(opts.Fonts) == 0 && system.Empty() {
			return Result{}, ErrNoFonts
		}
		fontConfig.System = system
	}

	resolver := opts.Resolver
	if resolver == nil {
		resolver = DenyAll
	}
	fetch := func(target string) (utils.RemoteRessource, error) {
		if strings.HasPrefix(strings.ToLower(target), "data:") {
			return utils.DefaultUrlFetcher(target)
		}
		resource, err := resolver(target)
		if err != nil {
			return utils.RemoteRessource{}, err
		}
		return utils.RemoteRessource{
			Content:       bytes.NewReader(resource.Data),
			MimeType:      mediaType(resource.MediaType, target),
			RedirectedUrl: target, // adres bazowy dla url(...) wewnątrz arkusza
		}, nil
	}

	parsed, err := tree.NewHTML(utils.InputString(htmlinput.ExpandSelfClosing(htmlinput.DecodeByteOrderMark(html))), opts.BaseURL, fetch, "print")
	if err != nil {
		return Result{}, fmt.Errorf("gopdfgo: %w", err)
	}
	parsed.Behavior = tree.Behavior{MarginsEscapeHeightLimits: opts.MarginsEscapeHeightLimits,
		MonospaceFontSize: opts.MonospaceFontSize, ExactLengths: opts.ExactLengths, MaxShrink: maxShrink(opts.MaxShrink), Continuous: opts.Continuous}
	// Cechy width, height, aspect-ratio i orientation w @media oceniamy względem obszaru strony arkusza
	// domyślnego, bez wpływu @page autora (ADR 0019, CSS Page 3 § 7.1). Arkusz domyślny to A4 w pionie
	// z marginesami arkusza przeglądarki, chyba że arkusz użytkownika (odpowiednik okna druku) podaje
	// w @page rozmiar albo marginesy: wtedy obszar strony liczymy na pustym dokumencie z samym arkuszem
	// użytkownika i, jeśli się różni, czytamy ten arkusz drugi raz. Arkusze autora czytane są raz.
	userSheets := func() ([]tree.CSS, error) {
		if opts.UserCSS == "" {
			return nil, nil
		}
		sheet, err := tree.NewCSSExt(utils.InputString(opts.UserCSS), opts.BaseURL, fontConfig)
		if err != nil {
			return nil, fmt.Errorf("gopdfgo: user stylesheet: %w", err)
		}
		return []tree.CSS{sheet}, nil
	}
	tree.SetMediaReference(0, 0)
	defer tree.SetMediaReference(0, 0)
	sheets, err := userSheets()
	if err != nil {
		return Result{}, err
	}
	if len(sheets) > 0 && strings.Contains(strings.ToLower(opts.UserCSS), "@page") {
		empty, err := tree.NewHTML(utils.InputString("<html></html>"), opts.BaseURL, fetch, "print")
		if err != nil {
			return Result{}, fmt.Errorf("gopdfgo: %w", err)
		}
		width, height, boxWidth, boxHeight := layout.PageArea(empty, sheets, fontConfig)
		// gopdfgo: arkusz domyślny dla jednostek vw i vh w @page (tree.SetDefaultPageBox)
		tree.SetDefaultPageBox(float64(boxWidth), float64(boxHeight))
		if defaultWidth, defaultHeight := tree.MediaReference(); width > 0 && height > 0 &&
			(math.Abs(float64(width)-defaultWidth) > 0.01 || math.Abs(float64(height)-defaultHeight) > 0.01) {
			tree.SetMediaReference(float64(width), float64(height))
			if sheets, err = userSheets(); err != nil {
				return Result{}, err
			}
		}
	}

	// Atrybuty prezentacyjne HTML (width i height na img, bgcolor, align, border na table itd.) są
	// stosowane: standard HTML definiuje je jako wskazówki prezentacyjne, które renderer ma uwzględniać
	// (WHATWG HTML, sekcja Rendering). W silniku źródłowym to opcja domyślnie wyłączona.
	//
	// Strony są rysowane zaraz po ułożeniu (ADR 0027); gdy wynik różniłby się od rysowania po ułożeniu
	// całego dokumentu, wszystko, co narysowano, idzie do kosza, a dokument jest układany jeszcze raz
	// i rysowany po ułożeniu wszystkich stron. Ostrzeżenia z odrzuconego przebiegu też odrzucamy.
	var (
		output *pdf.Output
		pages  int
	)
	tagged := opts.Tagged || opts.Conformance.tagged() || opts.Accessibility.tagged()
	fallback := "streaming disabled"
	if !document.StreamingDisabled {
		warningsBefore := warnings.Len()
		pages, fallback = document.Stream(parsed, sheets, true, fontConfig, func() backend.Document {
			pdfmodel.NonFiniteNumbers.Store(0)
			output = newOutput(tagged, fontCache)
			return output
		}, 1, attachments, func() { warnings.Truncate(warningsBefore) })
		if fallback != "" {
			warnings.Truncate(warningsBefore)
		}
	}
	if fallback != "" {
		laidOut := document.Render(parsed, sheets, true, fontConfig)
		pdfmodel.NonFiniteNumbers.Store(0)
		output = newOutput(tagged, fontCache)
		laidOut.ReleaseDrawnPages = true // strony zwalniane zaraz po narysowaniu, zob. document.Document
		laidOut.Write(output, 1, attachments)
		pages = len(laidOut.Pages)
	}
	if document.OnStream != nil {
		document.OnStream(fallback)
	}
	if opts.Conformance != ConformanceNone {
		output.DisableImageInterpolation() // ISO 19005-2 § 6.2.8
	}
	file := output.Finalize()
	file.Trailer.Info.Producer = EngineName + " " + EngineVersion
	// drzewo struktury to tysiące małych obiektów: w strumieniach obiektów (ADR 0033)
	file.Trailer.ObjectStreams = tagged
	embedded := output.EmbeddedFiles()
	if err := checkAttachments(embedded, opts.Conformance); err != nil {
		return Result{}, err
	}
	if fx != nil {
		// nazwa pliku faktury jednoznacznie wskazuje XML (§ 6.2): drugi plik o tej nazwie z HTML to błąd
		// (klucz w drzewie nazw dostaje wtedy przyrostek, ale /F i /UF zostają te same)
		count := 0
		for _, f := range embedded {
			if f.Spec != nil && f.Spec.UF == fx.fileName {
				count++
			}
		}
		if count != 1 {
			return Result{}, fmt.Errorf("%w: %d embedded files named %q", ErrInvalidFacturX, count, fx.fileName)
		}
	}
	if err := applyConformance(&file, opts.Conformance, opts.Accessibility, embedded, fx); err != nil {
		return Result{}, err
	}
	var out bytes.Buffer
	if err := file.Write(&out, nil); err != nil {
		return Result{}, fmt.Errorf("gopdfgo: %w", err)
	}

	result = Result{PDF: out.Bytes(), Pages: pages, EngineName: EngineName, EngineVersion: EngineVersion}
	result.Warnings = append(result.Warnings, invoiceWarnings...)
	if system != nil {
		for _, message := range system.Warnings() {
			result.Warnings = append(result.Warnings, Warning{Message: message})
		}
	}
	// PDF z tagami bez tytułu: czytnik pokazuje wtedy nazwę pliku, a PDF/UA wymaga tytułu (ISO 14289-1 § 7.1)
	if tagged && strings.TrimSpace(file.Trailer.Info.Title) == "" {
		result.Warnings = append(result.Warnings, Warning{Message: "Tagged PDF: document without a title (<title>)"})
	}
	// Wartość nieskończona albo NaN w rysunku oznacza błąd układu: zapisujemy ją jako 0, żeby plik
	// pozostał poprawny składniowo, i zgłaszamy, bo fragment strony może leżeć nie tam, gdzie powinien.
	if count := pdfmodel.NonFiniteNumbers.Load(); count > 0 {
		result.Warnings = append(result.Warnings, Warning{Message: fmt.Sprintf(
			"infinite or undefined values in a drawing: %d (written as 0)", count)})
	}
	scanner := bufio.NewScanner(&warnings)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		message := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "webrender.warning: "))
		if message != "" {
			result.Warnings = append(result.Warnings, Warning{Message: message})
		}
	}
	return result, nil
}

// systemFontSource to fonty systemowe jednego renderowania (ADR 0039); implementacja jest w pliku
// systemfonts_on.go, kompilowanym tylko ze znacznikiem systemfonts.
type systemFontSource interface {
	text.SystemFonts
	Warnings() []string
	Empty() bool
}

// prepareFacturX sprawdza Options.FacturX i wprowadza fakturę do opts: plik XML na początek kopii
// Attachments i normę PDF/A-3 w Conformance. Bez faktury zwraca nil i niczego nie zmienia.
func prepareFacturX(opts *Options) (*facturXDescription, []Warning, error) {
	if opts.FacturX == nil {
		return nil, nil, nil
	}
	plan, err := planFacturX(opts.FacturX)
	if err != nil {
		return nil, nil, err
	}
	conformance, err := facturXConformance(opts.Conformance, opts.Tagged || opts.Accessibility.tagged())
	if err != nil {
		return nil, nil, err
	}
	opts.Conformance = conformance
	opts.Attachments = append([]Attachment{plan.attachment}, opts.Attachments...)
	return &facturXDescription{fileName: plan.attachment.Name, level: string(plan.level), version: plan.version}, plan.warnings, nil
}

// newOutput tworzy cel rysowania; tagged włącza oznaczanie treści i drzewo struktury (ADR 0033).
// Sprawdzenie konturów fontów przy zapisie jest zapamiętywane w pamięci fontów (ADR 0047).
func newOutput(tagged bool, fonts *FontCache) *pdf.Output {
	output := pdf.NewOutput()
	output.SetFontSupportCache(fonts.cache.Supported)
	if tagged {
		output.SetTagged()
	}
	return output
}

// mediaType zwraca typ bez parametrów (silnik porównuje go dosłownie, "text/css; charset=utf-8" odrzuca),
// a gdy Resolver go nie podał, zgaduje po rozszerzeniu adresu.
func mediaType(declared, target string) string {
	if declared == "" {
		path := target
		if i := strings.IndexAny(path, "?#"); i >= 0 {
			path = path[:i]
		}
		declared = mime.TypeByExtension(filepath.Ext(path))
	}
	if i := strings.IndexByte(declared, ';'); i >= 0 {
		declared = declared[:i]
	}
	return strings.TrimSpace(declared)
}

// maxShrink zamienia Options.MaxShrink na wartość dla silnika: zero to domyślne 1,5.
func maxShrink(option float64) float64 {
	if option == 0 {
		return 1.5
	}
	return option
}
