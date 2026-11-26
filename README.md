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

- **Prints like a browser**: the same page count as Chromium on 38 of 40 test documents.
- **Does what browsers don't do for print**: tables of contents with page numbers (`target-counter()`), "page X
  of Y" in the text, footnotes, all 16 margin boxes, running headers, **hyphenation** for 24 languages built in.
- **Archival and accessible output**: PDF/A-2 and PDF/A-3 (levels b and a), PDF/UA-1 and Factur-X e-invoices,
  checked with veraPDF and Mustang; PAdES signatures in an optional package.
- **One small program**: a single Go binary of 20.5 MB instead of a 460 MB browser, 4–21 times faster than
  Chromium on business documents, with 44–104 MB of memory per document. Fonts and files come only from the caller;
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

| | gopdfgo | Chromium 158 | folio |
|---|:---:|:---:|:---:|
| Approach | WeasyPrint's layout model ported to Go, extended | a web browser printing a page | own layout engine in Go |
| Dependencies[^deps] | 7 Go modules | a browser process and system libraries | 3 Go modules 🥇 |
| Program size[^size] | 20.5 MB | about 460 MB on Linux | 11.7 MB 🥇 |
| Invoice, 3 pages[^bench] | 42 ms | 563 ms | 24 ms 🥇 |
| Book, 255 pages | 1.1 s | 2.2 s | 0.49 s 🥇 |
| Book with a table of contents | 1.4 s 🥇 | 2.2 s, no page numbers | not supported |
| Memory, business documents | 44–104 MB | 718–822 MB[^mem] | 25–49 MB 🥇 |
| Memory, book | 0.40 GB | 1.45 GB | 0.28 GB 🥇 |
| PDF size, invoice[^pdf] | 21–46 kB | 21–45 kB | 21–49 kB |
| PDF size, book | 26.3 MB 🥇 | 26.6 MB | 26.7 MB |
| Same page count as Chromium[^fidelity] | 38 of 40 🥇 | reference | 24 of 40 |
| Words on the same page as in Chromium | 92% 🥇 | reference | 50% |
| Median offset of words from Chromium | 0.2 pt 🥇 | reference | 54 pt |
| Deterministic output[^det] | yes 🥇 | no (dates, identifiers) | yes, by design |
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
[^size]: The command-line program as built by `go build` on macOS arm64 on 2026-11-26 (15.1 and 8.1 MB without
    the symbol table; `tools/bench/sizes.sh`, folio at commit `29bca95`). Chromium: the browser directory on a
    Linux server (`du -sh /opt/google/chrome`, 2026-11-20; not repeatable from this repository); the macOS
    application is 2.2 GB.
[^bench]: Whole process, median of 5 runs, measured 2026-11-26 at commit `c228df4` on one Apple M4 laptop, with
    the same input, fonts and stylesheets for every engine; Chromium's time includes starting the browser for
    each document. Invoice: `invoice-b-60rows`; business documents: the 4 invoices and 5 financial statements of
    [`testdata/comparison`](testdata/comparison) (1–16 pages, fictional content), on which gopdfgo is 4.0–21
    times faster than Chromium (median 8.6). Inside a running program folio is a median 2.0 times faster than
    gopdfgo over 42 documents (from 0.67 to 5.0 times; 1.6 times when gopdfgo keeps its fonts parsed between
    calls with `FontCache`). Method, commands and raw results: [`docs/benchmarks.md`](docs/benchmarks.md).
[^pdf]: Untagged PDF with font subsets; Chromium with `--disable-pdf-tagging` (its default tagged PDF is
    37–377 kB). No medal: the three are within 10% of each other.
[^mem]: Peak of the resident memory summed over the browser's processes (8–10 per document); pages shared between
    them count in each, so this is an upper bound.
[^det]: In the measured run gopdfgo wrote the same bytes in separate processes for all 44 documents, Chromium
    for none of 40 (it writes dates and identifiers into the file); earlier, `tools/verify/w3cbytes.py` found no
    difference over 46 756 renders of W3C tests (2026-11-24).
[^wpt]: Web Platform Tests at the commit in `testdata/wpt/UPSTREAM`, compared as in ADR 0003: exactly equal,
    or equal except for one-pixel anti-aliased edges. CSS 9020 exact + 464 edge (2026-11-24), print 308 + 2,
    SVG 191 + 3. Tools and recorded results: [`tools/verify`](tools/verify).
[^fidelity]: 40 documents printed by all three: the test corpus ([`testdata/documents`](testdata/documents), 22) and
    [`testdata/comparison`](testdata/comparison) (18: invoices, financial statements, office documents, Markdown
    and diagram pages, random documents); positions of words compared with Chromium's PDF. Chromium is the
    reference because documents are usually designed in a browser; where it departs from the specification,
    gopdfgo follows the specification.
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
- [`docs/comparison.md`](docs/comparison.md): gopdfgo, folio and Chromium side by side.
- [`docs/benchmarks.md`](docs/benchmarks.md): how the comparison is measured, with the raw results; tools in
  [`tools/bench`](tools/bench) and [`tools/verify`](tools/verify).
- In Polish: [`docs/provenance.md`](docs/provenance.md) (origin of the inherited code and every change made to
  it), [`docs/adr/`](docs/adr/) (decisions), [`docs/progress.md`](docs/progress.md) (progress in numbers),
  [`testdata/README.md`](testdata/README.md) (test corpus), [`docs/vision.md`](docs/vision.md) (goals).

## How the comparison was measured

All three engines printed the same HTML files with the same fonts on one machine, in one run on one commit:
gopdfgo and folio through small programs in [`tools/bench`](tools/bench) (folio pinned to a commit in their
`go.mod`), Chromium from the command line, one browser per document. Times are for the whole process unless
stated, so they include starting the program and, for Chromium, the browser. The book is the illustrated *Pride
and Prejudice* from Project Gutenberg (27 MB of images), downloaded and checked by `testdata/books/fetch.sh`.
Fidelity is measured on words: for each word of Chromium's PDF, the page and position of the same word in the
other PDF. W3C test counts are reftests from the Web Platform Tests, compared as described in ADR 0003: exact,
or equal except for anti-aliased edges ([`tools/verify`](tools/verify)). Method, versions, commands, the
meaning of every number and the raw results: [`docs/benchmarks.md`](docs/benchmarks.md).

## License

Apache License 2.0, see `LICENSE` and `NOTICE`. The layout engine is derived from Benoit Kugler's Go port of
WeasyPrint (BSD-3-Clause); this and the other third-party code and data are listed, with their licence texts, in
[`docs/third-party-notices.txt`](docs/third-party-notices.txt). Include it, together with `LICENSE` and `NOTICE`,
when you distribute software built with gopdfgo.
