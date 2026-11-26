# gopdfgo

Prints HTML + CSS + SVG documents to PDF in pure Go: no browser, no JavaScript, no cgo. Made for the documents
businesses print (invoices, reports, financial statements, long tables, books) with CSS Paged Media: page
margins and headers, tables of contents with page numbers, footnotes, PDF/A, PDF/UA and Factur-X. The same
input always gives the same PDF, byte for byte.

```go
import "github.com/polestorm/gopdfgo/htmlpdf"

result, err := htmlpdf.Render(ctx, html, htmlpdf.Options{Fonts: fonts})
// result.PDF
```

## Why gopdfgo

Compared with headless Chromium and with [folio](https://github.com/carlos7ags/folio), another HTML to PDF
engine in Go (details in [Comparison](#comparison)):

- **Prints like a browser**: the same page count as Chromium on 33 of 35 test documents.
- **Does what browsers don't do for print**: tables of contents with page numbers (`target-counter()`), "page X
  of Y" in the text, footnotes, all 16 margin boxes, running headers, **hyphenation** for 24 languages built in.
- **Archival and accessible output**: PDF/A-2 and PDF/A-3 (levels b and a), PDF/UA-1 and Factur-X e-invoices,
  checked with veraPDF and Mustang; PAdES signatures in an optional package.
- **One small program**: a single Go binary of 20.5 MB instead of a 460 MB browser, 3–14 times faster than
  Chromium on business documents, with 49–113 MB of memory per document. Fonts and files come only from the caller;
  nothing is read from the disk or the network unless you allow it.
- **Standards first**: the layout follows the W3C specifications and is tested on their test suites: 9484 of
  10962 CSS reftests, 310 of 340 print reftests, 194 of 235 SVG reftests[^wpt].

## Contents

- [Why gopdfgo](#why-gopdfgo) · [Comparison](#comparison) · [Examples](#examples) · [Status](#status) ·
  [Documentation](#documentation) · [How the comparison was measured](#how-the-comparison-was-measured) ·
  [License](#license)
- [Usage](docs/usage.md): fonts, resources, PDF/A and PDF/UA, Factur-X, signatures, command line, what is
  and is not supported

## Comparison

Compared with headless Chromium, the usual way to print HTML from a server, and with folio, a younger Go library
with its own layout engine and an HTML converter.

| | gopdfgo | Chromium 157 | folio |
|---|:---:|:---:|:---:|
| Approach | WeasyPrint's layout model ported to Go, extended | a web browser printing a page | own layout engine in Go |
| Dependencies[^deps] | 7 Go modules | a browser process and system libraries | 3 Go modules 🥇 |
| Program size[^size] | 20.5 MB | about 460 MB on Linux | 11.7 MB 🥇 |
| Invoice, 3 pages[^bench] | 44 ms | 430 ms | 22 ms 🥇 |
| Book, 255 pages | 1.1 s | 2.1–2.6 s | 0.50 s 🥇 |
| Book with a table of contents | 1.4 s 🥇 | no page numbers | not supported |
| Memory, business documents | 49–113 MB | about 950 MB, two browser instances[^mem] | 25–56 MB 🥇 |
| Memory, book | 0.43 GB | 1.7 GB | 0.27 GB 🥇 |
| PDF size, invoice[^pdf] | 25–51 kB | 27–53 kB | 26–56 kB |
| PDF size, book | 26.3 MB 🥇 | 27.6 MB | 26.7 MB |
| Same page count as Chromium[^fidelity] | 33 of 35 🥇 | reference | 17 of 35 |
| Words on the same page as in Chromium | 92% 🥇 | reference | 35% |
| Median offset of words from Chromium | 0.2 pt 🥇 | reference | 55 pt |
| Deterministic output | yes, tested on 46 756 renders 🥇 | no (dates, identifiers) | yes, by design |
| Unsupported CSS | reported as warnings 🥇 | developer console | ignored silently |
| JavaScript | no | yes 🥇 | no |
| Flexbox, grid, multi-column | yes (no `subgrid`) | yes 🥇 | partly |
| `@page` margin boxes | 16 of 16 🥇 | 16 of 16 🥇 | 6 of 16 |
| Running headers (`string-set`, `running()`) | both 🥇 | neither | `string-set` |
| Table of contents with page numbers | yes 🥇 | no | no |
| "Page X of Y" in the text | yes 🥇 | margin boxes only | no |
| Footnotes (`float: footnote`) | yes 🥇 | no | no |
| Hyphenation | 24 languages built in 🥇 | system dictionaries | English |
| Line breaking by the Unicode rules (UAX #14) | yes 🥇 | yes 🥇 | at spaces and between CJK characters |
| W3C reftests passed (CSS, print, SVG)[^wpt] | 9484 of 10962, 310 of 340, 194 of 235 🥇 | reference | not published |
| SVG | yes | yes 🥇 | basic shapes |
| Right-to-left and vertical text | no | yes 🥇 | right-to-left |
| PDF/A | 2b, 2a, 3b, 3a 🥇 | no | yes |
| Tagged PDF, PDF/UA | PDF/UA-1, checked with veraPDF 🥇 | tagged | tagged |
| Factur-X / ZUGFeRD | yes, checked with Mustang 🥇 | no | yes |
| PAdES signatures | B-B, B-T, B-LT; fields placed in HTML (optional package) | no | B-B to B-LTA 🥇 |
| HTML form elements (`<input>`, `<select>`, `<textarea>`) | printed as they look | printed as they look | printed as they look |
| Fillable PDF form fields | signature fields placed in HTML | no | created from Go code at given coordinates; filling existing PDF forms 🥇 |
| Barcodes, editing PDF files | no | no | yes 🥇 |
| License | Apache 2.0 | BSD-3-Clause | Apache 2.0 |
| GitHub popularity[^pop] | new | Chromium itself | 884 stars, 61 forks |

[^deps]: Go modules the program needs (`go version -m`). gopdfgo: go-text (shaping), benoitkugler/pdf and
    pstokenizer (PDF writing), hhrutter/lzw and three `golang.org/x` modules; all pure Go.
[^size]: The command-line program as built by `go build` on macOS arm64 on 2026-11-24 (folio at commit
    `29bca95`). Chromium: the headless shell with its libraries on Linux; the macOS application
    is 2.2 GB.
[^bench]: Whole process, measured 2026-11-24 on one Apple M4 laptop with the same input and fonts for every
    engine; Chromium measured 2026-11-20 and 2026-11-21. Business documents: Polish e-invoices and financial
    statements of 1–16 pages. Over 36 documents rendered inside a running program, folio is a median 2.0 times
    faster than gopdfgo (from 0.7 to 5.1 times; 1.5 times when gopdfgo keeps its fonts parsed between calls with
    `FontCache`); gopdfgo is faster on the SVG report, the page-break document and the long register table. Chromium's
    time includes starting the browser; inside a running program through DevTools it still needs 0.42–0.46 s per
    document, so gopdfgo's whole process is 2.4–14 times faster than that. "3–14 times" compares whole processes on
    nine invoices and financial statements (Chromium 2026-11-20, gopdfgo 2026-11-23, [`docs/overview.md`](docs/overview.md));
    these documents are not in the repository yet, so this ratio cannot be repeated from outside until they are
    replaced by public ones (ADR 0055). Per-document numbers in [`docs/comparison.md`](docs/comparison.md).
[^pdf]: Untagged PDF, fonts embedded as subsets; Chromium through DevTools with the same font files
    (`docs/overview.md`). No medal: the files differ by less than 8%, and on 2026-11-20, with the fonts passed as
    `data:` URLs, Chromium's invoices were 17–26% smaller (`docs/chromium-comparison.md`, cause not
    checked). The engines will be measured again with one setting (ADR 0055).
[^mem]: Peak resident memory summed over all browser processes, with two browser instances running at once
    (one for invoices, one for financial statements); one instance alone was not measured.
[^wpt]: Web Platform Tests at the commit in `testdata/wpt/UPSTREAM`, compared as in ADR 0003: exactly equal,
    or equal except for one-pixel anti-aliased edges. CSS 9020 exact + 464 edge (2026-11-24), print 308 + 2,
    SVG 191 + 3. Tools and recorded results: [`tools/verify`](tools/verify).
[^fidelity]: 35 documents printed by all three: the test corpus, financial statements, office documents
    converted to HTML, Markdown and diagram pages, generated random documents; positions of words compared with Chromium's PDF. Chromium is the reference because
    documents are usually designed in a browser; where it departs from the specification, gopdfgo follows the
    specification.
[^pop]: Stars and forks on 2026-11-24.

## Examples

Pages printed by gopdfgo from the test corpus ([`testdata/documents`](testdata/documents)):

<table>
<tr>
<td width="25%"><img src="docs/gallery/invoice.png" alt="An invoice with a table that continues on the next pages"></td>
<td width="25%"><img src="docs/gallery/table-of-contents.png" alt="A table of contents with page numbers, a running header and a page number"></td>
<td width="25%"><img src="docs/gallery/svg-charts.png" alt="A report with SVG bar and line charts"></td>
<td width="25%"><img src="docs/gallery/columns.png" alt="Two columns continued across pages"></td>
</tr>
<tr>
<td>An invoice: the items table runs over three pages with a repeated header; "page 1 of 3" in the margin box.</td>
<td>A table of contents filled in with <code>target-counter()</code>, a running header from <code>running()</code>.</td>
<td>SVG charts with gradients, transparency, a blur and repeated symbols, drawn as vectors.</td>
<td>Multi-column text fragmented across pages; with <code>text-wrap: pretty</code> no line ends with a one-letter Polish word.</td>
</tr>
</table>

## Status

Early, but complete for its purpose: it prints the business documents it was built for, page for page like
Chromium, and passes the W3C tests listed above. Before 1.0 the API may still change. What is supported and what
is not: [`docs/usage.md`](docs/usage.md#what-is-supported); remaining differences from Chromium and their reasons:
[`docs/overview.md`](docs/overview.md).

## Documentation

- [`docs/usage.md`](docs/usage.md): how to use the library and the commands.
- [`docs/overview.md`](docs/overview.md): measurements against Chromium, unsupported features, requirements.
- [`docs/comparison.md`](docs/comparison.md): gopdfgo, folio and Chromium side by side, with sources for every
  number.
- In Polish: [`docs/provenance.md`](docs/provenance.md) (origin of the inherited code and every change made to
  it), [`docs/adr/`](docs/adr/) (decisions), [`docs/progress.md`](docs/progress.md) (progress in numbers),
  [`testdata/README.md`](testdata/README.md) (test corpus), [`docs/vision.md`](docs/vision.md) (goals).

## How the comparison was measured

All three engines printed the same HTML files with the same fonts, on one machine; folio was run only as a
program, from its own repository. Times are for the whole process unless stated, so they include starting the
program and, for Chromium, the browser. The book is the illustrated *Pride and Prejudice* from Project Gutenberg
(27 MB of images). Fidelity is measured on words: for each word of Chromium's PDF, the page and position of the
same word in the other PDF. W3C test counts are reftests from the Web Platform Tests, compared as described in
ADR 0003: exact, or equal except for anti-aliased edges. The full method is in
[`docs/benchmark.md`](docs/benchmark.md) (in Polish). The measuring tools are in [`tools/bench`](tools/bench)
(gopdfgo, folio and Chromium) and [`tools/verify`](tools/verify) (W3C reftests, byte reproducibility).

## License

Apache License 2.0, see `LICENSE` and `NOTICE`. The layout engine is derived from Benoit Kugler's Go port of
WeasyPrint (BSD-3-Clause); this and the other third-party code and data are listed, with their licence texts, in
[`docs/third-party-notices.txt`](docs/third-party-notices.txt). Include it, together with `LICENSE` and `NOTICE`,
when you distribute software built with gopdfgo.
