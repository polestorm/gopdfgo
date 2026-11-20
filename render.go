package gopdfgo

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
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
	"github.com/polestorm/gopdfgo/internal/webrender/html/document"
	"github.com/polestorm/gopdfgo/internal/webrender/html/layout"
	"github.com/polestorm/gopdfgo/internal/webrender/html/tree"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/text/hyphen"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// EngineName i EngineVersion identyfikują silnik w wyniku; odbiorca zapisuje je przy kopii dokumentu.
// Wersję podnosimy przy każdej zmianie, która zmienia bajty wyniku na korpusie.
const (
	EngineName    = "gopdfgo/webrender"
	EngineVersion = "0.1.0-dev"
)

// Błędy zwracane przez Render; sprawdzać przez errors.Is.
var (
	// ErrNoFonts: w Options.Fonts nie ma żadnego fontu. Silnik nie sięga po fonty systemowe.
	ErrNoFonts = errors.New("gopdfgo: no fonts provided")
	// ErrEngine: silnik zakończył pracę paniką; komunikat zawiera ślad stosu.
	ErrEngine = errors.New("gopdfgo: engine failure")
	// ErrResourceDenied: zwracany przez DenyAll dla każdego zasobu poza data:.
	ErrResourceDenied = errors.New("gopdfgo: resource denied")
)

// Font to jeden plik fontu (TTF lub OTF) dostępny dla dokumentu.
type Font struct {
	Name string // nazwa do komunikatów, np. nazwa pliku
	Data []byte
}

// Resource to zasób zwrócony przez Resolver.
type Resource struct {
	Data      []byte
	MediaType string // np. "text/css"; pusty oznacza rozpoznanie po rozszerzeniu adresu
}

// Resolver dostarcza zasoby wskazane w dokumencie przez url(...), src i href. Dostaje adres bezwzględny,
// rozwiązany względem Options.BaseURL. Adresów data: silnik nie przekazuje, obsługuje je sam.
type Resolver func(url string) (Resource, error)

// DenyAll odmawia każdego zasobu; to domyślny Resolver.
func DenyAll(url string) (Resource, error) {
	return Resource{}, fmt.Errorf("%w: %s", ErrResourceDenied, url)
}

// Options ustawia renderowanie. Wartość zerowa nie wystarcza: wymagane są fonty.
type Options struct {
	// Fonts to jedyne fonty, jakich silnik użyje, także dla rodzin generycznych i znaków zastępczych.
	Fonts []Font
	// Resolver dostarcza zasoby zewnętrzne; nil oznacza DenyAll.
	Resolver Resolver
	// BaseURL to adres, względem którego rozwiązywane są adresy względne w dokumencie.
	BaseURL string
	// UserCSS to arkusz użytkownika (niższy priorytet niż arkusze dokumentu), np. domyślny @page.
	UserCSS string
	// GenericFamilies wskazuje, których rodzin z Fonts użyć dla rodzin generycznych CSS. Klucz to nazwa
	// rodziny generycznej ("serif", "sans-serif", "monospace", "cursive", "fantasy", "system-ui"), wartość to
	// nazwy rodzin fontów w kolejności prób, np. {"serif": {"Tinos"}}. Dotyczy też fontu domyślnego dokumentu,
	// bo wartością początkową font-family jest serif. Rodzina generyczna bez wpisu, albo z wpisem, którego
	// rodzin nie ma w Fonts, jest rozstrzygana jak dotąd (ADR 0005).
	GenericFamilies map[string][]string
	// MarginsEscapeHeightLimits przełącza zwijanie marginesów przy min-height i max-height. Domyślnie
	// (false), jak w Chromium, dolny margines ostatniego dziecka zostaje w pudełku, którego wysokość
	// zmieniło ograniczenie. Wartość true daje zachowanie z dosłownego tekstu CSS 2.2 § 10.7: margines
	// zwija się z marginesem rodzica i odsuwa następne pudełko (ADR 0006).
	MarginsEscapeHeightLimits bool
	// MonospaceFontSize to rozmiar w pikselach, jaki ma font-size: medium, gdy font-family jest samym
	// słowem monospace (<pre>, <code>, <kbd>, <samp>, <tt>). Zero oznacza 13, jak w przeglądarkach;
	// 16 wyłącza tę regułę i daje pismo tej samej wielkości co reszta tekstu (ADR 0008).
	MonospaceFontSize float64
	// ExactLengths wybiera między wydrukiem jak w Chromium a wiernością arkuszowi (ADR 0013).
	// Domyślnie (false) długości, które Chromium zaokrągla do całych pikseli CSS, są zaokrąglane tak samo:
	//   - border-width, outline-width i column-rule-width: wartości między 0 a 1 px rosną do 1 px,
	//     większe są obcinane w dół (0.5pt daje 0,75 pt, 1pt daje 0,75 pt, 1.5pt daje 1,5 pt; ADR 0009);
	//   - border-spacing: obcinane w dół do całych pikseli (1mm daje 3 px);
	//   - obszar strony: szerokość i wysokość w górę do całych pikseli, lewy margines do najbliższego
	//     piksela (A4 z marginesami 15 mm: 681 px zamiast 680,31; ADR 0021).
	//   - rozmiar pisma, którym składany jest tekst: obcinany do 0,01 px (9,625 px daje 9,62 px; em
	//     liczy się dalej od pełnego font-size).
	// Wartość true układa i rysuje wszystkie długości dokładnie takie, jak podano, z dokładnością,
	// na jaką pozwala PDF; wynik różni się wtedy od wydruku Chromium.
	ExactLengths bool
	// MaxShrink ogranicza pomniejszanie treści szerszej niż obszar strony (ADR 0014). Gdy coś wystaje
	// poza prawą krawędź obszaru strony (sztywna szerokość, szeroka tabela, długi wiersz bez łamania),
	// cała treść jest pomniejszana tak, żeby się zmieściła, najwyżej MaxShrink razy; pola marginesów
	// zostają w skali 1. Zero oznacza 1,5, jak w Chromium; 1 wyłącza pomniejszanie (treść wystaje
	// i jest ucinana na krawędzi kartki); math.Inf(1) pomniejsza bez limitu.
	MaxShrink float64
	// Continuous wyłącza podział na strony: powstaje jedna strona o szerokości z @page, która rośnie
	// w dół do wysokości treści i nie jest niższa niż strona z arkusza. Wysokość z @page pozostaje
	// podstawą procentów, jednostek vh i elementów pozycjonowanych, jak okno przeglądarki; właściwości
	// podziału (break-*, orphans, widows) i powtarzanie nagłówków tabel nie mają wtedy zastosowania
	// (ADR 0017).
	Continuous bool
	// Conformance wybiera normę pliku wynikowego; wartość pusta to zwykły PDF 1.7, PDFA2b to PDF/A-2b
	// (ADR 0016), PDFA3b to PDF/A-3b z plikami stowarzyszonymi (ADR 0026). W trybie PDF/A plik dostaje metadane XMP, profil sRGB i identyfikator wyliczony
	// z treści; układ i wygląd stron się nie zmieniają.
	Conformance Conformance
	// Uncompressed zapisuje strumienie treści stron, fontów i map ToUnicode bez kompresji, żeby dało się je
	// czytać w edytorze tekstu (diagnostyka). Obrazy zostają skompresowane bezstratnie stopniem 0, bo ich
	// filtr jest częścią formatu. Domyślnie (false) wszystko jest kompresowane zlib w stopniu 6.
	Uncompressed bool
	// Hyphenation to słowniki dzielenia wyrazów (hyphens: auto) podane przez wywołującego: klucz to znacznik
	// języka BCP 47 ("cs", "sv-SE"), wartość to wzorce Lianga w UTF-8, jeden w wierszu (plik .pat.txt
	// z hyph-utf8 albo słownik .dic Hunspella). Mają pierwszeństwo przed wbudowanymi. Wbudowane są tylko
	// słowniki na licencjach MIT, BSD, Unlicense i FSF all-permissive (ADR 0024): af, be, bg, da, de, el,
	// en, es, et, fr, gl, it, lt, mn, nb, nl, nn, pl, pt, sk, sl, sq, te, uk. Za licencję słownika
	// podanego tutaj odpowiada wywołujący.
	Hyphenation map[string][]byte
	// Attachments to pliki osadzane w dokumencie obok tych, które wskazuje sam HTML (<link rel=attachment>,
	// <a rel=attachment>, pobierane przez Resolver). Trafiają do drzewa /EmbeddedFiles katalogu, a w trybie
	// PDFA3b także do tablicy /AF katalogu (pliki stowarzyszone, ISO 19005-3 § 6.8), np. XML faktury
	// Factur-X/ZUGFeRD jako RelationshipAlternative. PDFA2b dopuszcza tylko załączniki będące plikami
	// PDF/A-1 albo PDF/A-2; inne kończą Render błędem ErrAttachmentNotPDFA (ADR 0026).
	Attachments []Attachment
}

// Warning to zgłoszenie silnika, które nie przerywa renderowania: nieobsługiwana właściwość albo wartość,
// zasób, którego nie udało się wczytać, nieznany selektor.
type Warning struct {
	Message string
}

// Result to wynik renderowania.
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

// Render zamienia dokument HTML na PDF.
//
// Renderowania są wykonywane po kolei. Silnik nie ma punktów przerwania, więc gdy ctx wygaśnie w trakcie
// pracy, Render wraca z ctx.Err(), a praca trwa w tle do końca i do tego czasu kolejne wywołania czekają
// (albo wracają z własnym ctx.Err()).
func Render(ctx context.Context, html []byte, opts Options) (Result, error) {
	if len(opts.Fonts) == 0 {
		return Result{}, ErrNoFonts
	}
	if _, err := backendAttachments(opts.Attachments); err != nil {
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

	fontMap := fontscan.NewFontMap(log.New(io.Discard, "", 0))
	contents := map[string][]byte{}
	for i, font := range opts.Fonts {
		id := fmt.Sprintf("gopdfgo-font-%d:%s", i, font.Name)
		if err := fontMap.AddFont(bytes.NewReader(font.Data), id, ""); err != nil {
			return Result{}, fmt.Errorf("gopdfgo: font %q: %w", font.Name, err)
		}
		contents[id] = font.Data
	}
	fontConfig := text.NewFontConfigurationGotext(fontMap)
	fontConfig.ReuseLoadedFaces = true
	if len(opts.GenericFamilies) != 0 {
		fontConfig.GenericFamilies = map[string][]string{}
		for generic, families := range opts.GenericFamilies {
			fontConfig.GenericFamilies[strings.ToLower(strings.TrimSpace(generic))] = families
		}
	}
	for id, data := range contents {
		fontConfig.RegisterFontContent(id, data)
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

	parsed, err := tree.NewHTML(utils.InputString(expandSelfClosing(decodeByteOrderMark(html))), opts.BaseURL, fetch, "print")
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
	laidOut := document.Render(parsed, sheets, true, fontConfig)
	pdfmodel.NonFiniteNumbers.Store(0)
	output := pdf.NewOutput()
	laidOut.Write(output, 1, attachments)
	if opts.Conformance != ConformanceNone {
		output.DisableImageInterpolation() // ISO 19005-2 § 6.2.8
	}
	file := output.Finalize()
	file.Trailer.Info.Producer = EngineName + " " + EngineVersion
	embedded := output.EmbeddedFiles()
	if err := checkAttachments(embedded, opts.Conformance); err != nil {
		return Result{}, err
	}
	if err := applyConformance(&file, opts.Conformance, embedded); err != nil {
		return Result{}, err
	}
	var out bytes.Buffer
	if err := file.Write(&out, nil); err != nil {
		return Result{}, fmt.Errorf("gopdfgo: %w", err)
	}

	result = Result{PDF: out.Bytes(), Pages: len(laidOut.Pages), EngineName: EngineName, EngineVersion: EngineVersion}
	// Wartość nieskończona albo NaN w rysunku oznacza błąd układu: zapisujemy ją jako 0, żeby plik
	// pozostał poprawny składniowo, i zgłaszamy, bo fragment strony może leżeć nie tam, gdzie powinien.
	if count := pdfmodel.NonFiniteNumbers.Load(); count > 0 {
		result.Warnings = append(result.Warnings, Warning{Message: fmt.Sprintf(
			"wartości nieskończone lub nieokreślone w rysunku: %d (zapisane jako 0)", count)})
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
