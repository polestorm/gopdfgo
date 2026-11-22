# gopdfgo

HTML + CSS + SVG to PDF in pure Go: no browser, no JavaScript, no cgo. gopdfgo is a document engine for
the documents businesses print: invoices, reports, financial statements, tables that run over many pages.
It is not meant to print arbitrary web pages. It supports CSS Paged Media: page size and margins, margin
boxes with running headers and footers, page counters, named pages, page breaks with `orphans` and
`widows`, repeated table headers.

Status on 2026-11-22:
- W3C screen reftests across 11 CSS areas: 9499 of 10962 pass;
- W3C print reftests: 310 of 340 pass (308 exact, 2 within anti-aliasing edges);
- W3C SVG reftests: 194 of the 235 that can run without scripts pass;
- own corpus of 22 documents: same page count as Chromium; PDF/A-2b, PDF/A-2a and PDF/UA-1 valid in
  veraPDF for all 22;
- real documents (4 KSeF invoices, 5 e-SF financial statements): same pages and page breaks as Chromium,
  every word within 0.81 pt of its position in Chromium; 12 Markdown documents and 10 diagram pages: same
  page counts as Chromium, remaining differences explained in `docs/overview.md`;
- Factur-X output accepted by veraPDF and the Mustang validator.

Comparison with Chromium, the list of what is not supported, and requirements:
[`docs/overview.md`](docs/overview.md).

## What it does

- HTML5 parsing and the CSS cascade: selectors level 4 including `:is()`, `:where()`, `:not()`; custom
  properties; `calc()`, `min()`, `max()`, `clamp()`, `round()`, `mod()`, `rem()`; colour functions;
  `transform` with 2D and 3D functions (flattened to the page, as browsers do without perspective),
  `backface-visibility`, the individual `translate`, `rotate`, `scale` properties; `filter` (colour
  functions exact, `drop-shadow()` and `blur()` as vector approximations); relative colours
  (`rgb(from …)` …), `color()` with predefined colour spaces, `color-mix()` in every space.
- Layout:
  - block, inline, floats and positioning;
  - tables, including spanning cells split across pages;
  - flexbox, grid and multi-column, fragmented across pages;
  - lists and counters, generated content.
- Paged media: `@page` with sizes, margins and margin boxes, `counter(page)` and `counter(pages)`,
  running elements, named pages, footnotes, `leader()`, `target-counter()`.
- Text: OpenType shaping (go-text/HarfBuzz port), font fallback, `@font-face`, font features,
  `hyphens: auto`, `text-transform`, justification, `white-space`, `word-break`, `overflow-wrap`.
- SVG, inline and as files (details in `docs/overview.md`):
  - parsed as XML; inline SVG styled by the page's whole cascade; text shaped like HTML text, with
    `textPath` and `text-shadow`;
  - `foreignObject`, `switch`, external `<use>`, markers, masks, patterns, gradients (also inherited
    through `href`, as Inkscape and Illustrator write them), `non-scaling-stroke`;
  - drop shadow and blur filters drawn as vectors, without rasterization;
  - `<a href>` inside inline SVG becomes a clickable PDF link;
  - URL fragments select part of an image: `#xywh=`, `#svgView(…)`, `<view>`;
  - an SVG file used many times (a logo in every page header) is written into the PDF once;
  - an SVG file used as an image is static and loads no external resources.
- PNG, JPEG and GIF images; documents embedded with `iframe` (`srcdoc` or `src`), `object` and `embed`
  are printed at the size of the element.
- PDF output:
  - font subsets, TrueType and CFF (`.otf`, including CFF2 variable fonts at their default instance; ADR 0040);
  - internal and external links, bookmarks from headings;
  - PDF/A-2b and PDF/A-3b on request (`Options.Conformance`);
  - tagged PDF on request (`Options.Tagged`, `-tagged`; ADR 0033): a structure tree from the HTML elements
    in document order (headings, paragraphs, lists, tables, links, images with alternative text), page
    content marked as belonging to elements, backgrounds, borders and margin boxes marked as artifacts;
    PDF/A-2a and PDF/A-3a (`Options.Conformance`, `-pdfa 2a`) turn it on and pass veraPDF on all 22
    documents of the corpus;
  - PDF/UA-1 on request (`Options.Accessibility = PDFUA1`, `-pdfua 1`, combinable with `-pdfa 2a`): tags
    with list labels and bodies, table header scopes and spans, link descriptions, `pdfuaid` in XMP.
    veraPDF accepts 22 of 22 corpus documents; defects of a source document (a skipped heading level,
    an image without alternative text) are reported as warnings;
  - file attachments (`Options.Attachments`, `<link rel="attachment">`), with MIME type and
    `AFRelationship`, e.g. the XML of an e-invoice inside a PDF/A-3 file;
  - Factur-X 1.0 / ZUGFeRD 2.x hybrid invoices (`Options.FacturX`, `-facturx invoice.xml`; ADR 0034): the
    CII invoice XML embedded as `factur-x.xml` (`xrechnung.xml` for XRECHNUNG) with the relationship the
    specification gives for the profile, the `fx` XMP properties with their PDF/A extension schema, PDF/A-3b
    (PDF/A-3a with tags, also with PDF/UA-1); veraPDF accepts the result, and the Mustang validator 2.26.0
    accepts both the PDF and the invoice XML (MINIMUM profile, measured 2026-11-21).
- Output is deterministic: the same input gives the same bytes.

Where the specifications leave a choice, gopdfgo prints like Chromium, so documents designed in a browser
keep their layout. One switch, `Options.ExactLengths`, turns off Chromium's pixel rounding for output that
follows the stylesheet exactly. Every such decision has an ADR in `docs/adr/`.

## Compared with Chromium

Business documents, measured 2026-11-20 on KSeF invoices and e-SF financial statements, with the same
fonts in both engines and the same page counts:

| | Chromium 157 | gopdfgo |
|---|---|---|
| software size | about 460 MB browser plus system libraries | 19 MB static binary (linux/amd64) |
| time per document (whole process) | 0.41–0.55 s | 0.05–0.24 s |
| memory | about 950 MB (two browser instances) | 48–156 MB |
| PDF size, invoices (untagged PDF) | 26.8–52.9 kB | 24.9–51.0 kB |

Large documents, measured 2026-11-21: the illustrated *Pride and Prejudice* (256 pages, 27 MB of images),
the same book four times over (1021 pages), and both with a table of contents of `target-counter()` links
and "page X of Y" in the page content. Three runs, whole process, same font; memory is the peak of all
Chromium processes.

| | Chromium 157: time | gopdfgo: time | Chromium 157: memory | gopdfgo: memory |
|---|---|---|---|---|
| book, 256 pages | 2.10–2.56 s | 1.17–1.22 s | 1.67–1.73 GB | 0.42–0.44 GB |
| book with table of contents | 2.09–2.12 s ¹ | 2.23–2.31 s | 1.42–1.45 GB | 0.44–0.46 GB |
| book ×4, 1021 pages | 4.54–4.69 s | 4.57–4.69 s | 2.20–2.26 GB | 0.73–0.77 GB |
| book ×4 with table of contents | 4.55–4.68 s ¹ | 8.6–8.8 s | 2.11–2.14 GB | 0.78–0.80 GB |

¹ Chromium does not support `target-counter()` or `counter(pages)` outside margin boxes: its table of
contents has no page numbers and "page X of Y" reads "0 of 0". gopdfgo fills them in with a second
layout pass (ADR 0036). Details: `docs/overview.md`.

## Usage

```go
regular, _ := os.ReadFile("NotoSans-Regular.ttf")
bold, _ := os.ReadFile("NotoSans-Bold.ttf")
result, err := gopdfgo.Render(ctx, html, gopdfgo.Options{
	Fonts: []gopdfgo.Font{
		{Name: "NotoSans-Regular.ttf", Data: regular},
		{Name: "NotoSans-Bold.ttf", Data: bold},
	},
	GenericFamilies: map[string][]string{"sans-serif": {"Noto Sans"}},
})
// result.PDF, result.Pages, result.Warnings
```

Fonts and external resources come only from the caller:
- `Options.Fonts` lists the fonts; the default build has no system font lookup;
- `Options.Resolver` loads stylesheets, images and other external resources; by default nothing is read
  from the network or the disk.

System fonts are a build option (ADR 0039). A binary built with `go build -tags systemfonts ./cmd/gopdfgo`
accepts `Options.SystemFonts` (`-system-fonts` on the command line): installed fonts then fill in font
families and characters that the caller's fonts lack, while the caller's fonts and `GenericFamilies` keep
priority. Each system font used produces a warning with its family and file, so the file can be added to
`Options.Fonts` to freeze the output; fonts whose licence forbids embedding (OS/2 `fsType`) are skipped. The
output then depends on the fonts installed on the machine and is not reproducible across machines. The
default build contains no code that scans system font directories, and `Options.SystemFonts` there returns
`ErrSystemFontsNotBuilt`; `go version -m <binary>` shows `-tags=systemfonts` for the other variant.

Command line:

```
go run ./cmd/gopdfgo -fonts fonts/ -in invoice.html -out invoice.pdf
```

Requirements: Go 1.26 or newer, no cgo. Dependencies are pure Go under BSD-3-Clause, MIT or Unlicense
licences (list in `docs/overview.md`).

## Not supported

- JavaScript, `video`, `audio`; `canvas` stays empty without scripts.
- Vertical writing and right-to-left/bidirectional text.
- Structure inside SVG images and alternative text for generated content in tagged PDF.
- Digital signatures (PAdES).
- CSS `zoom`, `mask` and `mask-image` on HTML elements, animations (CSS and SVG SMIL), CSS Linked
  Parameters in SVG.
- Validation of invoice content: when rendering, gopdfgo checks only that the Factur-X invoice XML is a CII
  document and reads its profile identifier; it does not validate the XML against the XSD or Schematron of
  the profile, so an invalid invoice is embedded as given. Validate the XML before rendering (for example
  with Mustang, which we use in tests to check gopdfgo's own output).
- `subgrid`, `grid-lanes`, `column-height`.
- Filters that need rasterization: SVG `feImage`, `feTile`, `feMorphology`, `feTurbulence`, convolution,
  lighting and displacement (such a filter is skipped and the element drawn without it), `url()` filters on
  HTML elements, `backdrop-filter`. Blur and drop shadows (SVG and CSS `filter`) are vector approximations.
- Perspective: `perspective()` and the `perspective` property are accepted with a warning and drawn as
  an orthographic projection; `preserve-3d` has no depth sorting. 3D transforms without perspective are
  drawn exactly.

The full list with its status is in `docs/overview.md` and `docs/progress.md`.

## Documentation

| File | Contents |
|---|---|
| `docs/overview.md` | comparison with Chromium, unsupported features, requirements |
| `docs/progress.md` | progress in numbers, what is left and how important it is (Polish) |
| `docs/vision.md` | goals and trade-offs (Polish) |
| `docs/architecture.md` | pipeline and interfaces (Polish) |
| `docs/feature-audit.md` | feature status, measurements, work log by batch (Polish) |
| `docs/chromium-comparison.md` | measurements on KSeF invoices and e-SF statements in xml.pro (version `4eca5e5`) (Polish) |
| `docs/adr/` | architecture decision records (Polish) |
| `testdata/README.md` | test corpus: own documents, WeasyPrint samples, W3C print tests (Polish) |
| `THIRD_PARTY.md` | origin of the code in `internal/` and every change made to it (Polish) |

## Licence

Apache License 2.0 (`LICENSE`), Copyright 2026 Michał Głębowski (`NOTICE`). gopdfgo includes code and data
by other authors under BSD-3-Clause, MIT, Unlicense and FSF all-permissive licences. The layout engine
is derived from Benoit Kugler's Go port of WeasyPrint (webrender, go-weasyprint). Full licence texts are in
`THIRD_PARTY_NOTICES`. Include them, together with `LICENSE` and `NOTICE`, when you distribute software
built with gopdfgo.
