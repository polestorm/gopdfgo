# gopdfgo, folio and Chromium compared

A side-by-side view of three ways to turn HTML into PDF from a Go program, written to show where gopdfgo is
strong and where it is not. Numbers are approximate and dated; each comes from a measurement described in
`docs/overview.md` (Chromium), `docs/benchmark.md` § 11 (folio) and the ADRs it cites. Measurements were
made on one machine (Apple M4, macOS) with the same input and the same fonts for every engine. folio was run
at commit `29bca95` (2026-11-18), Chromium 157 through `--headless --print-to-pdf`, gopdfgo at the commit
named in each source.

## The projects

| | Chromium (headless) | folio | gopdfgo |
|---|---|---|---|
| What it is | a web browser that can print a page to PDF | a PDF library for Go with its own layout engine and an HTML converter | an HTML + CSS + SVG to PDF engine in Go, built for paginated documents |
| Started | 2008 (headless mode 2017) | March 2026 | November 2026; the layout core derives from WeasyPrint (2011) through a Go port |
| Maintainers, popularity | Google and a large community; the most used rendering engine | 17 contributors, about 880 GitHub stars, release v0.11 (October 2026) | one maintainer; not public yet |
| Licence | BSD-3-Clause (Chromium) | Apache-2.0 | Apache-2.0 |
| Maturity | very high | young, fast-moving (`MIGRATING.md` lists breaking changes) | young; validated mainly on W3C tests and real business documents |

## Size and dependencies

| | Chromium | folio | gopdfgo |
|---|---|---|---|
| Installed size | about 460 MB on Linux plus system libraries (2.2 GB as a macOS app) | about 9 MB command-line binary | 19 MB static binary (linux/amd64, stripped) |
| Dependencies | system libraries (fonts, NSS, graphics), a writable home directory, a browser process to manage | 3 Go modules (`golang.org/x/image`, `x/net`, `x/text`) | 7 Go modules, all pure Go (go-text, benoitkugler/pdf, `golang.org/x/*`) |
| cgo / external processes | separate browser process | no / no | no / no |
| Use from Go | through DevTools (e.g. chromedp) or a subprocess | library call | library call or command line |

## Speed and memory

| Document | Chromium | folio | gopdfgo |
|---|---|---|---|
| invoice, 3 pages, whole process | about 0.43 s (browser started per document) | 22 ms | 66 ms |
| invoice, 3 pages, inside a running program | 0.42–0.46 s per document through DevTools | 16 ms | 51 ms; 38 ms with a font cache shared between calls |
| illustrated book, about 255 pages | 2.1–2.6 s | 0.48 s | 1.2 s |
| the same book four times, about 1020 pages | 4.5–4.7 s | not measured | 4.6–4.7 s |
| peak memory, business documents | about 950 MB (browser processes) | 22–80 MB | 48–117 MB |
| peak memory, book | 1.7 GB | 0.29 GB | 0.42 GB |

folio is the fastest and the lightest: 2–3 times faster than gopdfgo on typical documents and 5–10 times on
documents with thousands of inline styles. Chromium pays for starting a browser on small documents and is
as fast as gopdfgo at a thousand pages. With a table of contents of page numbers and "page X of Y" in the text,
gopdfgo needs a second layout pass (the 1020-page book takes 8.8 s instead of 4.7 s); neither of the others
fills these in at all (see Features).

## Output

| | Chromium | folio | gopdfgo |
|---|---|---|---|
| Page count equal to Chromium (35 test documents) | reference | 17 of 35 | 33 of 35 |
| Words on the same page as in Chromium | reference | 35% | 92% |
| Median vertical offset of words | reference | 55 pt | 0.2 pt |
| PDF size, invoice (untagged) | 27–53 kB (through DevTools) | 26–56 kB | 25–51 kB |
| PDF size, invoice, as written by the command line | 43–383 kB (tagged) | — | 25–51 kB (untagged by default) |
| PDF size, illustrated book | 27.6 MB | 26.7 MB | 26.3 MB |
| Same input gives the same bytes | no (dates, identifiers) | yes, by design | yes, checked on 46 756 renders |
| Unsupported CSS | developer tools console | ignored silently | reported as warnings |

## Features

✓ supported, ◐ partly, ✗ not supported. Entries marked “not documented” were not found in the project’s documentation and were not tested.

| | Chromium | folio | gopdfgo |
|---|---|---|---|
| JavaScript | ✓ | ✗ | ✗ |
| CSS layout: flexbox, grid, multi-column | ✓ | ◐ | ✓ (no `subgrid`) |
| W3C CSS reftests passed (11 areas) | reference | not published | 9484 of 10962 |
| `@page` margin boxes | ✓ | ◐ 6 of 16 | ✓ 16 of 16 |
| Named pages, `:first`/`:left`/`:right`/`:blank` | ◐ (no `:blank`) | ◐ (`:first`/`:left`/`:right` only) | ✓ |
| Running headers (`string-set`, `position: running()`) | ✗ | ◐ `string-set` | ✓ |
| Table of contents with page numbers (`target-counter()`) | ✗ | ✗ | ✓ |
| "Page X of Y" in the page content (`counter(pages)`) | ✗ (margin boxes only) | ✗ | ✓ |
| Footnotes (`float: footnote`) | ✗ | ✗ (not documented) | ✓ |
| `widows` / `orphans` as the specification defines them | ◐ | ✓ | ✓ |
| SVG | ✓ | ◐ (basic shapes; text, clipping and filters limited) | ✓ (194 of 235 runnable W3C SVG reftests) |
| Right-to-left and bidirectional text | ✓ | ✓ | ✗ |
| Vertical writing | ✓ | ✗ (not documented) | ✗ |
| Hyphenation | ✓ (system dictionaries) | ◐ English | ✓ 24 languages built in |
| System fonts | ✓ | ◐ fallback lookup | ◐ only in a separate build (`-tags systemfonts`) |
| PDF/A | ✗ | ✓ | ✓ 2b, 2a, 3b, 3a |
| Tagged PDF / PDF/UA | ◐ tagged by default, PDF/UA not claimed | ◐ tagged | ✓ PDF/UA-1 (veraPDF on all corpus documents) |
| Factur-X / ZUGFeRD e-invoices | ✗ | ✓ | ✓ (veraPDF and Mustang) |
| Interactive forms, digital signatures, barcodes | ✗ | ✓ | ✗ |
| Reading, merging, redacting, encrypting PDF files | ✗ | ✓ | ✗ |
| Other platforms | any OS with Chromium | WASM, C library, Java SDK | Go only |

## Summary

**gopdfgo is strong at** printing HTML documents the way a browser lays them out, and at what browsers do not
do for print: running headers, tables of contents with page numbers, "page X of Y" in text, footnotes, all 16
margin boxes, built-in hyphenation for 24 languages, PDF/A, PDF/UA and Factur-X, all in one dependency-free
binary that gives the same bytes for the same input.

**gopdfgo is weaker at** speed and memory compared with folio, at writing systems (no right-to-left or
vertical text), at everything that is not printing HTML (forms, signatures, barcodes, editing existing PDF
files), and at running scripts, which only a browser does. It is young and not yet public, while Chromium is
the reference engine and folio already has users.

**Choose Chromium** for arbitrary web pages, pages that need JavaScript, or right-to-left and vertical text,
when installing and running a browser is acceptable. **Choose folio** to build PDF files from Go code, fill or
sign forms, or process existing PDF files, when the HTML is simple. **Choose gopdfgo** for business documents
written in HTML and CSS (invoices, reports, statements, long paginated documents) that must look like they
do in a browser, meet archival and accessibility standards, and come out the same every time.
