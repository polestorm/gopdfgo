// Polecenie gopdfgo zamienia plik HTML na PDF.
//
//	gopdfgo -in dokument.html -out dokument.pdf -fonts katalog-fontów [-root katalog-zasobów]
//
// Fonty bierze wyłącznie z katalogu -fonts i jego podkatalogów pierwszego poziomu (*.ttf, *.otf);
// plik families.json w tym katalogu wskazuje fonty dla rodzin generycznych (ADR 0005). Zasoby wskazane w dokumencie czyta tylko
// spod -root (domyślnie katalog pliku wejściowego); wszystko inne jest odrzucane z ostrzeżeniem.
// Adres bezwzględny w dokumencie (/style/a.css) liczy się od -root, jak na serwerze WWW.
//
// Flaga -system-fonts dopuszcza fonty zainstalowane w systemie jako fonty zastępcze (ADR 0039); działa
// tylko w binarce zbudowanej ze znacznikiem systemfonts (go build -tags systemfonts ./cmd/gopdfgo),
// a -fonts jest wtedy opcjonalne.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/polestorm/gopdfgo/htmlpdf"
)

func main() {
	in := flag.String("in", "", "HTML file")
	out := flag.String("out", "", "PDF file")
	fontDir := flag.String("fonts", "", "directory with fonts (*.ttf, *.otf)")
	root := flag.String("root", "", "directory resources may be read from (default: the directory of the input file)")
	timeout := flag.Duration("timeout", time.Minute, "limit czasu")
	userCSS := flag.String("css", "", "user stylesheet, e.g. \"@page { size: A4 }\"")
	quiet := flag.Bool("quiet", false, "do not print warnings")
	continuous := flag.Bool("continuous", false, "no pagination: one page as tall as the content")
	hyphenDicts := flag.String("hyphenation", "", "hyphenation dictionaries besides the built-in ones, e.g. \"cs=hyph-cs.pat.txt,sv=hyph-sv.pat.txt\"")
	uncompressed := flag.Bool("uncompressed", false, "uncompressed content, font and ToUnicode streams (for debugging)")
	pdfa := flag.String("pdfa", "", "output conformance: 2b, 3b (PDF/A-2b, PDF/A-3b), 2a, 3a (level A, tagged); default: plain PDF")
	tagged := flag.Bool("tagged", false, "tagged PDF: logical structure from the HTML elements, for screen readers (ADR 0033)")
	pdfua := flag.String("pdfua", "", "accessibility standard: 1 (PDF/UA-1, tagged; combines with -pdfa, e.g. -pdfa 2a -pdfua 1); default: none")
	flag.Func("attach", "document attachment: file[,relationship[,MIME type]], relationship: Source, Data, Alternative, Supplement, Unspecified (default); may be repeated", func(value string) error {
		parts := strings.Split(value, ",")
		data, err := os.ReadFile(parts[0])
		if err != nil {
			return err
		}
		attachment := htmlpdf.Attachment{Name: filepath.Base(parts[0]), Data: data}
		if len(parts) > 1 {
			attachment.Relationship = htmlpdf.Relationship(strings.TrimSpace(parts[1]))
		}
		if len(parts) > 2 {
			attachment.MediaType = strings.TrimSpace(parts[2])
		}
		attachments = append(attachments, attachment)
		return nil
	})
	facturXFile := flag.String("facturx", "", "Factur-X/ZUGFeRD invoice: CII XML file embedded as factur-x.xml (xrechnung.xml for XRECHNUNG); implies PDF/A-3 (ADR 0034)")
	facturXLevel := flag.String("facturx-level", "", "invoice profile: MINIMUM, BASICWL, BASIC, EN16931, EXTENDED, XRECHNUNG; default: read from the XML")
	marginsEscape := flag.Bool("margins-escape-height-limits", false,
		"a child's bottom margin escapes a box limited by min-height or max-height (CSS 2.2 § 10.7 literally)")
	exactLengths := flag.Bool("exact-lengths", false,
		"lengths exactly as in the stylesheet, without rounding (default as in Chromium: a 0.5pt border gives 1px, border-spacing 1mm gives 3px, padding 2.2px gives 2.1875px)")
	maxShrinkFlag := flag.Float64("max-shrink", 0,
		"largest shrink factor for content wider than the page; 0 means 1.5 as in Chromium, 1 turns it off, +Inf means no limit")
	monospaceSize := flag.Float64("monospace-size", 0,
		"font size (px) for font-family: monospace without a font-size; 0 means 13 as in browsers, 16 turns the rule off")
	systemFonts := flag.Bool("system-fonts", false,
		"use system fonts as a fallback for missing families and characters (only in a binary built with -tags systemfonts, ADR 0039)")
	generic := map[string][]string{}
	for name, usage := range map[string]string{"serif": "serif", "sans-serif": "sans-serif", "monospace": "monospace"} {
		flag.Func(name, "font families for the generic family "+usage+", comma-separated, e.g. \"Tinos\"", func(value string) error {
			for _, family := range strings.Split(value, ",") {
				if family = strings.TrimSpace(family); family != "" {
					generic[name] = append(generic[name], family)
				}
			}
			return nil
		})
	}
	flag.Parse()
	continuousMode = *continuous
	taggedMode = *tagged
	uncompressedMode = *uncompressed
	for _, item := range strings.Split(*hyphenDicts, ",") {
		if item = strings.TrimSpace(item); item == "" {
			continue
		}
		tag, path, ok := strings.Cut(item, "=")
		data, err := os.ReadFile(path)
		if !ok || err != nil {
			fmt.Fprintln(os.Stderr, "gopdfgo: -hyphenation: expected language=file:", item, err)
			os.Exit(2)
		}
		if hyphenation == nil {
			hyphenation = map[string][]byte{}
		}
		hyphenation[tag] = data
	}
	switch *pdfa {
	case "":
	case "2b", "2B":
		conformance = htmlpdf.PDFA2b
	case "3b", "3B":
		conformance = htmlpdf.PDFA3b
	case "2a", "2A":
		conformance = htmlpdf.PDFA2a
	case "3a", "3A":
		conformance = htmlpdf.PDFA3a
	default:
		fmt.Fprintln(os.Stderr, "gopdfgo: unknown -pdfa value (allowed: 2b, 3b, 2a, 3a)")
		os.Exit(2)
	}
	switch *pdfua {
	case "":
	case "1":
		accessibility = htmlpdf.PDFUA1
	default:
		fmt.Fprintln(os.Stderr, "gopdfgo: unknown -pdfua value (allowed: 1)")
		os.Exit(2)
	}
	if *facturXFile != "" {
		data, err := os.ReadFile(*facturXFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gopdfgo: -facturx:", err)
			os.Exit(2)
		}
		invoice = &htmlpdf.FacturX{XML: data}
		if *facturXLevel != "" {
			level, ok := facturXLevels[strings.NewReplacer(" ", "", "_", "", "-", "").Replace(strings.ToUpper(*facturXLevel))]
			if !ok {
				fmt.Fprintln(os.Stderr, "gopdfgo: unknown -facturx-level value (allowed: MINIMUM, BASICWL, BASIC, EN16931, EXTENDED, XRECHNUNG)")
				os.Exit(2)
			}
			invoice.Level = level
		}
	} else if *facturXLevel != "" {
		fmt.Fprintln(os.Stderr, "gopdfgo: -facturx-level requires -facturx")
		os.Exit(2)
	}
	systemFontsMode = *systemFonts
	if *in == "" || *out == "" || (*fontDir == "" && !*systemFonts) {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*in, *out, *fontDir, *root, *userCSS, generic, *marginsEscape, *exactLengths, *maxShrinkFlag, *monospaceSize, *timeout, *quiet); err != nil {
		fmt.Fprintln(os.Stderr, "gopdfgo:", err)
		os.Exit(1)
	}
}

// conformance to zgodność wybrana flagą -pdfa, continuousMode to flaga -continuous,
// uncompressedMode to flaga -uncompressed.
var (
	conformance      htmlpdf.Conformance
	accessibility    htmlpdf.Accessibility // flaga -pdfua
	continuousMode   bool
	uncompressedMode bool
	taggedMode       bool                 // flaga -tagged
	hyphenation      map[string][]byte    // flaga -hyphenation
	attachments      []htmlpdf.Attachment // flaga -attach
	invoice          *htmlpdf.FacturX     // flagi -facturx i -facturx-level
	systemFontsMode  bool                 // flaga -system-fonts
)

// facturXLevels to wartości -facturx-level po usunięciu spacji, podkreśleń i łączników.
var facturXLevels = map[string]htmlpdf.FacturXLevel{
	"MINIMUM": htmlpdf.FacturXMinimum, "BASICWL": htmlpdf.FacturXBasicWL, "BASIC": htmlpdf.FacturXBasic,
	"EN16931": htmlpdf.FacturXEN16931, "EXTENDED": htmlpdf.FacturXExtended, "XRECHNUNG": htmlpdf.FacturXXRechnung,
}

func run(in, out, fontDir, root, userCSS string, generic map[string][]string, marginsEscape, exactLengths bool, maxShrink float64, monospaceSize float64, timeout time.Duration, quiet bool) error {
	html, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(in)
	if err != nil {
		return err
	}
	if root == "" {
		root = filepath.Dir(absolute)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}

	var fonts []htmlpdf.Font
	patterns := []string{"*.ttf", "*.otf", "*/*.ttf", "*/*.otf"}
	if fontDir == "" {
		patterns = nil // tylko fonty systemowe (-system-fonts)
	}
	for _, pattern := range patterns {
		files, _ := filepath.Glob(filepath.Join(fontDir, pattern))
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			fonts = append(fonts, htmlpdf.Font{Name: filepath.Base(file), Data: data})
		}
	}
	// Plik families.json w katalogu fontów przypisuje rodziny fontów rodzinom generycznym CSS,
	// np. {"serif": ["Tinos"]}; flagi -serif, -sans-serif i -monospace mają pierwszeństwo.
	if data, err := os.ReadFile(filepath.Join(fontDir, "families.json")); fontDir != "" && err == nil {
		fromFile := map[string][]string{}
		if err := json.Unmarshal(data, &fromFile); err != nil {
			return fmt.Errorf("families.json: %w", err)
		}
		for name, families := range fromFile {
			if _, set := generic[name]; !set {
				generic[name] = families
			}
		}
	}

	resolver := func(target string) (htmlpdf.Resource, error) {
		u, err := url.Parse(target)
		if err != nil || u.Scheme != "file" {
			return htmlpdf.DenyAll(target)
		}
		file := filepath.Clean(u.Path)
		if !strings.HasPrefix(file, root+string(filepath.Separator)) {
			file = filepath.Join(root, file) // adres bezwzględny liczony od -root
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return htmlpdf.Resource{}, err
		}
		return htmlpdf.Resource{Data: data}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := htmlpdf.Render(ctx, html, htmlpdf.Options{Fonts: fonts, Resolver: resolver, BaseURL: "file://" + absolute, UserCSS: userCSS, GenericFamilies: generic,
		MarginsEscapeHeightLimits: marginsEscape, MonospaceFontSize: monospaceSize, ExactLengths: exactLengths, MaxShrink: maxShrink,
		Conformance: conformance, Continuous: continuousMode, Uncompressed: uncompressedMode, Hyphenation: hyphenation,
		Attachments: attachments, Tagged: taggedMode, Accessibility: accessibility, FacturX: invoice,
		SystemFonts: systemFontsMode})
	if err != nil {
		return err
	}
	if !quiet {
		for _, warning := range result.Warnings {
			fmt.Fprintln(os.Stderr, "warning:", warning.Message)
		}
	}
	fmt.Fprintf(os.Stderr, "%d pages, %d B, %s %s\n", result.Pages, len(result.PDF), result.EngineName, result.EngineVersion)
	return os.WriteFile(out, result.PDF, 0o644)
}
