# gopdfgo

HTML + CSS + SVG to PDF in pure Go: no browser, no JavaScript, no cgo. gopdfgo is a document engine for
the documents businesses print: invoices, reports, financial statements, tables that run over many pages.
It is not meant to print arbitrary web pages. It supports CSS Paged Media: page size and margins, margin
boxes with running headers and footers, page counters, named pages, page breaks with `orphans` and
`widows`, repeated table headers.

Status on 2026-11-20:
- W3C screen reftests across 11 CSS areas: 9502 of 10962 pass;
- W3C print reftests: 310 of 340 pass (308 exact, 2 within anti-aliasing edges);
- own corpus of 22 documents: same page count as Chromium, PDF/A-2b valid in veraPDF for all 22.

Comparison with Chromium, the list of what is not supported, and requirements:
[`docs/overview.md`](docs/overview.md).

## What it does

- HTML5 parsing and the CSS cascade: selectors level 4 including `:is()`, `:where()`, `:not()`; custom
  properties; `calc()`, `min()`, `max()`, `clamp()`; colour functions.
- Layout:
  - block, inline, floats and positioning;
  - tables, including spanning cells split across pages;
  - flexbox, grid and multi-column, fragmented across pages;
  - lists and counters, generated content.
- Paged media: `@page` with sizes, margins and margin boxes, `counter(page)` and `counter(pages)`,
  running elements, named pages, footnotes, `leader()`, `target-counter()`.
- Text: OpenType shaping (go-text/HarfBuzz port), font fallback, `@font-face`, font features,
  `hyphens: auto`, `text-transform`, justification, `white-space`, `word-break`, `overflow-wrap`.
- SVG, PNG, JPEG and GIF images.
- PDF output:
  - font subsets;
  - internal and external links, bookmarks from headings;
  - PDF/A-2b on request (`Options.Conformance`).
- Output is deterministic: the same input gives the same bytes.

Where the specifications leave a choice, gopdfgo prints like Chromium, so documents designed in a browser
keep their layout. One switch, `Options.ExactLengths`, turns off Chromium's pixel rounding for output that
follows the stylesheet exactly. Every such decision has an ADR in `docs/adr/`.

## Compared with Chromium

Measured 2026-11-20 on KSeF invoices and e-SF financial statements, with the same fonts in both engines
and the same page counts. Details: `docs/overview.md`.

| | Chromium 157 | gopdfgo |
|---|---|---|
| software size | about 460 MB browser plus system libraries | 21 MB static binary |
| time per document (whole process) | 0.41–0.55 s | 0.05–0.24 s |
| memory | about 950 MB (two browser instances) | 48–156 MB |
| PDF size, invoices (untagged PDF) | 26.8–52.9 kB | 24.9–51.0 kB |

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
- `Options.Fonts` lists the fonts; there is no system font lookup;
- `Options.Resolver` loads stylesheets, images and other external resources; by default nothing is read
  from the network or the disk.

Command line:

```
go run ./cmd/gopdfgo -fonts fonts/ -in invoice.html -out invoice.pdf
```

Requirements: Go 1.26 or newer, no cgo. Dependencies are pure Go under BSD-3-Clause, MIT or Unlicense
licences (list in `docs/overview.md`).

## Not supported

- JavaScript, `iframe`, `video`, `audio`, `canvas`, `object`, `embed`.
- System fonts.
- Vertical writing and right-to-left/bidirectional text.
- Tagged PDF (PDF/UA), PDF/A-3, file attachments.
- `subgrid`, `grid-lanes`, `column-height`.
- Individual transform properties (`translate`, `rotate`, `scale`), relative colours (`rgb(from …)`) and
  `round()`. These are skipped with a warning.

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
