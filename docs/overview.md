# gopdfgo at a glance: comparison with Chromium, unsupported features, requirements

gopdfgo turns HTML, CSS and SVG into PDF in pure Go. It needs no browser and runs no JavaScript. It
supports CSS Paged Media: pages, margins, margin boxes with headers and footers, page counters and breaks.
It is built for ordinary business documents (invoices, reports, financial statements, multi-page tables),
not for printing arbitrary web pages.

All numbers below were measured on 2026-11-20:
- gopdfgo: commit `e3f59aa` (code as of this page's first version), engine `gopdfgo/webrender 0.1.0-dev`;
- Chromium: Google Chrome 157.0.8103.79 on macOS;
- browser size on a server: Chrome 158.0.8127.36 on Ubuntu.

They need to be measured again after engine changes.

## Size

| | Chromium | gopdfgo |
|---|---|---|
| software | browser: 460 MB on a Linux server (`du -sh /opt/google/chrome`) plus system libraries from packages; 2.2 GB on macOS | 19.1 MB command-line binary, linux/amd64, `CGO_ENABLED=0 -trimpath -ldflags "-s -w"` (2026-11-22; 22.2 MB before ADR 0039 removed `text/template`) |
| fonts | system fonts | supplied by the caller (in xml.pro: 2.1 MB, five Noto files); system fonts only in a separate build (`-tags systemfonts`, ADR 0039) |
| installation and updates | browser package, security updates, a writable home directory for the browser process | one static binary, nothing to install on the system |

## Speed and memory

Documents:
- four variants of a Polish KSeF e-invoice rendered by `goksefgo/ksefview`: 1, 3, 1 and 7 pages;
- five e-SF financial statements from the public Polish court register (KRS), labelled A–E: 6, 11, 12, 16
  and 4 pages.

Both engines got the same font files (Noto Sans, Noto Sans Mono). All nine documents have the same page
count in both engines.

Machine: Apple M4, 10 cores, 32 GB. Times are the shortest of five runs of the whole process, including
program start. Chromium was measured on 2026-11-20 (system load average 6.7–7.9); gopdfgo again on
2026-11-23 at commit `de62591` (load average about 2), after the speed-ups of ADR 0037 and ADR 0045, with the
same five Noto files xml.pro passes and the e-SF statements in the variant xml.pro gives gopdfgo
(`bench/realdocs`, without scripts). The gopdfgo
times of 2026-11-20 are in brackets.

| Document | Pages | Chromium, `--headless --print-to-pdf` (browser starts for each document) | gopdfgo, command line |
|---|---|---|---|
| invoice, 2 items | 1 | 409 ms | 30 ms (49 ms) |
| invoice, 60 items | 3 | 426 ms | 51 ms (73 ms) |
| invoice, VAT rates and bank transfer | 1 | 407 ms | 32 ms (53 ms) |
| invoice, 140 items | 7 | 469 ms | 82 ms (109 ms) |
| e-SF A | 6 | 476 ms | 94 ms (128 ms) |
| e-SF B | 11 | 534 ms | 160 ms (202 ms) |
| e-SF C | 12 | 512 ms | 152 ms (197 ms) |
| e-SF D | 16 | 547 ms | 190 ms (244 ms) |
| e-SF E | 4 | 462 ms | 67 ms (93 ms) |

Peak memory of the gopdfgo process on 2026-11-23: invoices 48–72 MB, e-SF 67–117 MB. A program that renders
many documents can keep parsed fonts between calls with `Options.FontCache` (ADR 0045), which saves a further
10–15 ms per document; the command line renders one document and does not use it.

Chromium kept running and driven through DevTools (`Page.printToPDF` via chromedp, no wait before
printing) needed 0.42–0.46 s per document. That run used the same day and the same documents; see
`docs/chromium-comparison.md` (Polish). gopdfgo is now 2 to 15 times faster than that (2026-11-23).

Memory:
- gopdfgo, peak RSS of the process (`/usr/bin/time -l`): invoice 48 MB, 6-page e-SF 95 MB, 16-page e-SF
  156 MB;
- Chromium: peak RSS summed over the browser processes, 943–961 MB, with two browser instances running at
  once, as in xml.pro.

gopdfgo renders one document at a time per process; further calls wait in a queue. Chromium can print
several tabs in parallel.

### A large document: an illustrated book (256 pages)

The illustrated HTML edition of *Pride and Prejudice* from Project Gutenberg (book 1342, 164 illustrations
by Hugh Thomson, 0.8 MB of HTML and 27 MB of JPEG/PNG images; public domain). The files are not in the
repository; `testdata/books/fetch.sh` downloads them from the Gutenberg mirror and checks their SHA-256
sums, so the measurement can be repeated on the same input. For the measurement both engines get
`testdata/books/bench.css`: the same font (Tinos, with Times metrics, from `testdata/fonts`) and A4 pages
with 20 mm margins.

Measured 2026-11-20 on the same machine; gopdfgo at commit `0eb174f`, Chrome 157.0.8103.79. Three runs each,
whole process including program or browser start.

| | Chromium, `--headless --print-to-pdf` | gopdfgo, command line |
|---|---|---|
| pages | 253 | 256 |
| time | 2.13–2.47 s | 1.60 s (1.54–1.58 s before the memory optimisations below) |
| peak memory | 1672–1701 MB (all browser processes) | 423–456 MB (after the optimisations below) |
| PDF size | 27.58 MB (tagged PDF) | 26.25 MB |
| of which images | 25.21 MB (171 image objects) | 24.76 MB (164 image objects) |
| of which text, pages, fonts and structure | 2.37 MB | 1.49 MB |

- Both engines embed the illustrations almost unchanged, so most of the file is images. Without them,
  gopdfgo's file is 37% smaller; part of that is the structure tree Chrome's command line adds for screen
  readers (tagged PDF). Chrome without tags (through DevTools) was not measured for this book.
- The split was measured by summing the stream lengths of image objects, font files and everything else in
  the PDF.

**Page count.** The first measurement gave 265 pages. The cause was a bug: an `@font-face` rule without a
`font-weight` descriptor got weight 0 instead of the font's own weight (`auto`, CSS Fonts 4 § 4.5), so
normal text matched the bold face of the same family and the whole book was set in bold. After the fix the
text of every page matches Chrome's up to page 30, where the first such paragraph occurs. The remaining 3 pages come from widows and orphans: Chrome splits a
three-line paragraph 2 + 1 at a page end, although the initial `widows: 2` forbids a single line at the top
of the next page (CSS Fragmentation 3 § 3.3); with four or more lines Chrome follows the rule. gopdfgo
follows the specification and moves such a paragraph whole. With `widows: 1; orphans: 1` gopdfgo gives
253 pages, the same as Chrome.

**Memory.** The illustrations are not the cause: they are embedded without decoding, and the raw files take
27 MB of the heap. All pages are laid out before any is drawn (the design inherited from WeasyPrint), so
memory grows with the number of pages. Live heap after a forced collection, measured at each stage:

| Stage | book, 256 pages | book ×4, 1021 pages |
|---|---|---|
| HTML parsed, stylesheets | 40 MB | 47 MB |
| computed styles | 57 MB | 85 MB |
| box tree (the whole input) | 103 MB | 191 MB |
| all pages laid out | 289 MB | 527 MB |
| all pages drawn | 390 MB | 859 MB |

The Go garbage collector lets the heap grow to about twice the live size before collecting (`GOGC=100`),
which gives the peak of about 1 GB for the book. With `GOGC=50` the peak was 740 MB and with `GOGC=25`
660 MB; wall time stays about the same, CPU time grows by 25% and 70%.

The table was measured before five optimisations, which give byte-identical output:

- laid-out lines keep their glyphs in a compact form (44 B per glyph instead of the 80 B record of the
  go-text shaper);
- justification stores only the changed glyphs instead of copying each justified line (190 MB of copies
  for the 1021-page book);
- each page's boxes are released right after the page is drawn;
- each page is drawn as soon as its layout is final; margin boxes are added to its content after all
  pages are laid out, so "page X of Y" needs no second pass (ADR 0027);
- the cache of broken lines has a size limit (two generations of about 16 MB each) instead of growing
  for the whole document. Its key is the remaining text of the paragraph, so it took about 400 MB for
  1021 pages.

Peak memory after the first four: book 0.92–1.07 → 0.56–0.63 GB, book ×4 (1021 pages, 4.8 s, Chromium
2.44 GB) 2.25–2.31 → 0.90–0.96 GB. After the fifth (measured 2026-11-21, three runs): book 0.51–0.58 →
0.42–0.46 GB, same time; book ×4 0.83–0.85 → 0.73–0.79 GB, but 4.8 → 6.4 s, because the ×4 file repeats the
same text four times and the unlimited cache reused whole paragraphs from earlier copies. On a 1021-page
variant with different text in each copy (one vowel replaced per copy) time is the same, 6.5 s, and memory
drops from 1.30–1.54 to 0.73 GB.

Memory is now bounded mainly by the input (styles, box tree, images) rather than by the laid-out
document; the peak is reached during layout.

Documents that need a second pagination pass, such as a table of contents with `target-counter()` or
"page X of Y" with `counter(pages)` in the page content (not in margin boxes), are laid out twice
(ADR 0036): the first pass settles page numbers and the page count and releases each page right after
laying it out; the second lays out every page again with those values and draws it at once. Output is
byte-identical. Documents of up to 32 pages are laid out whole, as before, because the second pass costs
time and would save little memory there. Measured 2026-11-21 (three runs) on a variant of the book with a
table of contents of 61 chapter links at the front and "page X of Y" under every chapter heading:

| | book, no TOC | book with TOC, before | book with TOC, after | book ×4 with TOC, before | book ×4 with TOC, after |
|---|---|---|---|---|---|
| pages | 256 | 259 | 259 | 1027 | 1027 |
| peak memory | 440–483 MB | 598–613 MB | 472–479 MB | 1450–1531 MB | 802–839 MB |
| time | 1.60 s | 2.24 s | 3.00–3.02 s | 8.93–8.94 s | 12.27–12.34 s |

The extra layout pass costs 35–38% of time. A few cases still lay out the whole document before
drawing: a positioned box continued on an earlier page, a `position: fixed` box that first appears on a
later page, and a margin box with a negative `z-index`.

Since 2026-11-23 (ADR 0036, third addendum) most such documents skip the second pass. The old pagination
loop produces a mix of pages from its passes, and most pages of the final result come from the first one:
99.2–99.8% with only a table of contents at the front, 75% with "page X of Y" under every chapter heading
(the heading pages are laid out again). The first pass now draws those pages at once and defers only pages
that may still change (marked for another pass, or with `target-counter()` content), runs the further
pagination passes over the deferred pages only, and draws them at the end into their reserved places in the
PDF. Output is byte-identical (W3C corpus, document corpus, the books); the order of drawing matters only for
the characters a glyph gets in ToUnicode, which is checked. Measured on a loaded machine (12 runs each,
alternating): the book ×4 with a table of contents takes a median 1.17 times the time of the book ×4 without
one, against 1.9 times before. Measured again at commit `5b5fbe8` (three runs, load average about 5): book ×4
with a table of contents 5.18–5.38 s instead of 8.6–8.8 s, book with a table of contents 1.38–1.47 s instead of
2.23–2.31 s; peak memory is unchanged. Documents with `string-set` or
`bookmark-label` that use page counts or `target-counter()`, `position: fixed` boxes, content wider than the
page, tagged PDF, or references on nearly every page still take the two-pass path.

The inherited pagination loop marked a page with a reference to an anchor for another layout pass every
time the anchor was laid out, even when its page number had not changed; the book with a table of
contents ran 8 pagination passes (the limit) instead of 3. Pages are now marked only when the value
changes (output is byte-identical). `target-counter(…, pages)` now gives the page count of the document,
as CSS Generated Content 3 requires, instead of the page count of the pass in which the anchor was laid
out (often 0, "3/0"); such documents now take the two-pass path too. Measured 2026-11-21 (three runs):

| | passes | peak memory, before | peak memory, after | time, before | time, after |
|---|---|---|---|---|---|
| book, no TOC | — | 431–452 MB | 439–469 MB | 1.61–1.63 s | 1.62–1.63 s |
| book with TOC (259 pages) | 8 → 3 | 467–478 MB | 461–493 MB | 3.04–3.06 s | 2.72–2.76 s |
| book ×4 with TOC (1027 pages) | 8 → 3 | 804–889 MB | 834–931 MB | 12.40–12.44 s | 10.88–10.92 s |

### Current state compared with Chromium (2026-11-21)

Measured 2026-11-21 on the same machine, Chrome 157 and gopdfgo after the speed-ups of ADR 0037 (the
earlier gopdfgo times, commit `a655fcd`, are in that ADR); three runs each, whole process including
program or browser start; memory is the peak resident size, for Chromium summed over all
its processes. The table-of-contents variants (61 chapter links with `target-counter()` at the front,
"page X of Y" with `counter(pages)` under every chapter heading) are made by a script from the book.

| Document | Pages, Chromium / gopdfgo | Chromium: time | gopdfgo: time | Chromium: memory | gopdfgo: memory |
|---|---|---|---|---|---|
| book | 253 / 256 | 2.10–2.56 s | 1.17–1.22 s | 1.67–1.73 GB | 0.42–0.44 GB |
| book with table of contents | 256 / 259 | 2.09–2.12 s | 1.38–1.47 s ¹ | 1.42–1.45 GB | 0.43–0.45 GB |
| book ×4 | 1009 / 1021 | 4.54–4.69 s | 4.57–4.69 s | 2.20–2.26 GB | 0.73–0.77 GB |
| book ×4 with table of contents | 1015 / 1027 | 4.55–4.68 s | 5.18–5.38 s ¹ | 2.11–2.14 GB | 0.77–0.82 GB |

¹ After deferred pages, commit `5b5fbe8` (2026-11-23, see above); before them 2.23–2.31 s and 8.6–8.8 s.

- Chromium does not support `target-counter()` or `counter(pages)` outside margin boxes: its table of
  contents has no page numbers and "page X of Y" reads "Strona 0 z 0". gopdfgo writes "Chapter I —
  strona 20" and "Strona 11 z 259", which costs further pagination passes (ADR 0036). The times of the
  table-of-contents rows therefore do not compare the same work.
- On the plain book gopdfgo takes about half of Chromium's time, partly because Chromium starts a
  browser for each document; at 1021 pages the two take the same time. gopdfgo's time grows linearly
  with the number of pages (1.2 s × 4 ≈ 4.6 s), Chromium's more slowly, so on still longer documents
  Chromium would lead. gopdfgo uses 2.7–3.8 times less memory in every case.
- The extra pages come from `widows` (see "Page count" above): gopdfgo moves a three-line paragraph whole
  where Chromium splits it 2 + 1.

### Compared with folio (2026-11-23)

[folio](https://github.com/carlos7ags/folio) (Apache-2.0, commit `29bca95`) is the other pure-Go HTML-to-PDF
engine. Both engines rendered the same 36 documents (the 22 of `testdata/documents`, 13 real documents:
invoices, e-SF statements, Markdown pages, Word documents, random documents, and the 254-page book) with
the same Noto fonts through `@font-face`; Apple M4, in-process median of repeated renders; measured again on
2026-11-24 at commit `5961ab8` on an idle machine. Details and
the full table: `docs/benchmark.md` § 11 (Polish).

| | folio | gopdfgo | gopdfgo with `FontCache` |
|---|---|---|---|
| in-process time, median ratio to folio | 1 | 2.8× (1.0–5.4×) | about 1.9× (2026-11-23) |
| invoice `20-invoice` (3 pages), in process | 15.6 ms | 48.7 ms | about 36 ms |
| book (254 pages), in process | 0.46 s | 1.10 s | |
| peak memory (whole process) | 22–281 MB | 73–469 MB (book: 0.28 against 0.47 GB) | |
| page count as in Chromium | 17 of 35 | 33 of 35 | |
| reference words on the same page as in Chromium | 35% | 92% | |
| median vertical offset of words | 55 pt | 0.2 pt | |

folio is faster on every document, but it lays out a different document: e-SF statements come out on
twice as many pages, floats and multi-column text go missing, SVG text loses Polish letters, and it gives
no warnings for CSS it ignores. About half of folio's lead on small documents came from fonts: go-text
parses a whole font up front, and gopdfgo did it on every call, twice for a file given both in
`Options.Fonts` and in `@font-face`. ADR 0045 adds `Options.FontCache` (parsed fonts kept between calls)
and parses such a file once per render; documents with thousands of `style` attributes (Word exports) now
parse each distinct attribute once. All of this leaves the output byte for byte the same (46,756 W3C
renders, 77 real documents): the invoice went from 57.8 to 51.4 ms (37.9 ms with a warm cache), a 13-page
Word export from 0.92 to 0.56 s.

## PDF file size

| Document | Chromium via DevTools (untagged PDF) | Chromium `--print-to-pdf` (tagged PDF) | gopdfgo (untagged PDF) |
|---|---|---|---|
| invoice, 2 items | 26.8 kB | 42.7 kB | 24.9 kB |
| invoice, 60 items | 37.5 kB | 182.9 kB | 35.9 kB |
| invoice, VAT rates and bank transfer | 28.2 kB | 63.3 kB | 26.6 kB |
| invoice, 140 items | 52.9 kB | 383.0 kB | 51.0 kB |
| e-SF A | — | 315.4 kB | 60.3 kB |
| e-SF B | — | 621.9 kB | 97.4 kB |
| e-SF C | — | 541.9 kB | 94.0 kB |
| e-SF D | — | 734.5 kB | 116.3 kB |
| e-SF E | — | 181.9 kB | 40.4 kB |

Chrome's command line writes a tagged PDF: it adds a structure tree for screen readers, and `pdfinfo`
shows „Tagged: yes”. Through DevTools, with default settings, Chrome writes an untagged PDF, like
gopdfgo. Those two are comparable, and gopdfgo's files are 4–7% smaller. The e-SF statements were not
measured through DevTools with the same fonts.

gopdfgo writes a tagged PDF with `Options.Tagged` (ADR 0033); its objects then go into compressed object
streams. Measured 2026-11-21 (median of 5 runs, whole process, macOS arm64): corpus invoice
(`20-invoice`) 28.5 → 34.7 kB, register table (`02-register-table`, 1466 structure elements) 81.1 →
106.0 kB, mixed report (`21-mixed-report`) 37.7 → 35.1 kB; rendering 1–4% longer.

gopdfgo's file size comes from:
- zlib compression at level 6 (ADR 0023);
- font subsets that keep only the glyphs used and short tables, for TrueType and for CFF outlines (`.otf`,
  embedded as `CIDFontType0C`; CFF2 variable fonts at their default instance; ADR 0040). Two Japanese
  characters in Hiragino Sans took 5.6 MB before CFF subsets and take 2.7 kB now.

`Options.Uncompressed` writes an uncompressed file for reading in a text editor.

## Output compared with Chromium

Real documents, measured 2026-11-21 with `pdftotext -bbox`, words matched by sequence alignment; both
engines get the same Noto fonts through `@font-face`; Chrome 157.

- KSeF invoices (4): same pages; every word lies within 0.81 pt of its position in Chromium, median 0.14–0.23 pt.
- e-SF statements from xml.pro (5, variant without scripts): same pages and page breaks; every word lies
  within 0.43 pt of Chromium (3 words in one statement are matched to a neighbouring row because Chrome
  writes that row in a different order). Before 2026-11-21 two of them had 1342–1491 words more than 3 pt
  away: Chromium computes layout in 1/64 px units (LayoutUnit), and gopdfgo now does the same by default
  (ADR 0013).
- Markdown documentation (12 files rendered through gomdgo, measured 2026-11-22): all have Chromium's page
  count; in 9 every word is on the same page and at most 27 words are more than 3 pt away. The rest come
  from three Chromium behaviours that the standards do not support, where gopdfgo keeps the standard: a break after a word-initial hyphen
  (`-update`; UAX #14 LB20a forbids it), splitting a 3-line block 2 + 1 against `widows: 2` although a break
  before the block is allowed (CSS Fragmentation 3), and, in documents with font fallback, system fonts
  that gopdfgo does not use. Fixed on the way: a break after every space (`@page |:first`, as in Chromium),
  no break after a word-initial slash, a sub-pixel overflow before a comma, and document selectors reaching
  into `<foreignObject>` (ADR 0038).
- Mermaid diagram pages (10, inline SVG with `foreignObject` labels): 8 match Chromium to hundredths of a
  point; one differs by its label font (Chromium finds a system font), one by Chromium leaving page 1 empty
  before a diagram taller than the page. A diagram taller than the page is sliced across pages, and each
  page carries only the text that its slice shows; a label crossing the slice edge is on both pages, as in
  Chromium (ADR 0041; before 2026-11-22 an inline `<svg>` repeated its first slice on every page).
- Office documents (15 Word and OpenOffice files converted to HTML with macOS `textutil`, measured
  2026-11-22): all have Chromium's page count; in 8 at most 2 words are more than 3 pt away. Fixed on the way: a crash
  on a letter-spaced space in a table cell, `min-height` on table cells (ignored, as in Chromium; Word
  exports give every cell one), list items holding only empty inline boxes, and subscript/superscript
  offsets (now 1/5 and 1/3 of the parent font size, the CSS Inline 3 defaults; Chromium adds 1 px, ADR 0042).
  The rest: Chromium's system fonts for ✔ and ★, `widows` in short blocks, Chromium's rounding of the
  half-leading, and lines ending within hundredths of a pixel of the edge.
- Random documents (30 from a seeded generator: tables with spans and repeated headers, floats, lists,
  counters, columns, breaks, margin boxes; measured 2026-11-23): 28 have Chromium's page count. The generator
  found a crash in column layout after a float, multi-column containers overlapping floats, a table cell
  broken at a page end that made later content forget earlier floats, split tables not placed beside floats,
  rows pushed to the next page because the `border-spacing` below them did not fit (ADR 0043), and rows under
  a `rowspan` cell of a `break-inside: avoid` row being separated; all fixed. No hangs. The rest: Chromium
  splitting 3-line blocks 2 + 1 against `widows` (also when balancing columns), Chromium widening the body
  rows of a table continued below a float while keeping the repeated header's columns, line widths, and one
  document whose first page is laid out differently in the second layout pass (open).
- The remaining median of about 0.2 pt comes from Chrome snapping text baselines to whole pixels when it
  draws; gopdfgo keeps the exact baseline.
- W3C screen reftests across 11 CSS areas: 9484 of 10962 pass (2026-11-22; three `hanging-punctuation` tests
  passed only by accident before ADR 0038, see `docs/feature-audit.md`). Before ADR 0044 it was 9499: text
  widths are now exact and rounded up to 1/64 px per text piece as in Chromium, which shifts glyphs by a
  fraction of a pixel where a test and its reference split the same text into different boxes (open
  question 29).
- Lines ending right at the edge now break as in Chromium: line widths are the sum of exact glyph advances
  (before, each advance was rounded to 1/64 px and a line could be off by 0.3 px), the same for min- and
  max-content, and a shrunk page is laid out on a whole-pixel width (ADR 0044).
- W3C print reftests: 310 of 340 pass, 308 exactly and 2 within anti-aliasing edges (ADR 0003).
- Own corpus of 22 documents: page counts as in Chromium; PDF/A-2b and, with tags, PDF/A-2a valid in
  veraPDF for 22 of 22; PDF/UA-1 valid for 22 of 22 (five documents had a skipped heading level or SVG
  images without alternative text; fixed in the source on 2026-11-21 without changing their pages).

Where the specifications leave a choice, gopdfgo follows Chromium; each such case is recorded in
`docs/adr/`. `Options.ExactLengths` turns off the pixel and 1/64 px rounding that imitates Chromium (ADR 0013).

## Not supported in this version

System fonts are a build option, not part of the default binary (ADR 0039). Built with
`go build -tags systemfonts`, the engine accepts `Options.SystemFonts` (`-system-fonts`): fonts installed on
the machine fill in families and characters missing from `Options.Fonts` (CSS Fonts 4 § 5 font fallback),
the caller's fonts and `GenericFamilies` keep priority, every system font used is reported in a warning with
its family and file, and fonts whose OS/2 `fsType` forbids embedding a subset are skipped with a warning.
Directories are scanned once per process (31–65 ms for 449 font files on macOS, 2026-11-22) and nothing is
written to disk. The output then depends on the machine and is not reproducible across machines; the
default build keeps using only the caller's fonts, contains no system font scanning code (checked with
`go tool nm` in `tests/systemfonts_default_test.go`), and returns `ErrSystemFontsNotBuilt` for
`Options.SystemFonts`. `go version -m <binary>` shows `build -tags=systemfonts` for the system-fonts variant.

Out of scope by design:
- JavaScript and everything that depends on it; `video`, `audio`; `canvas` stays empty. Documents embedded
  with `iframe`, `object` and `embed` are printed at the size of the element, clipped, as Chromium prints them;
- vertical writing (`writing-mode`) and bidirectional or right-to-left text (ADR 0010);
- state pseudo-classes (`:hover` and similar), `@container`, `@property`, `::selection`;
- structure inside SVG images, notes and references (`Note`, `Reference`) and alternative text for
  generated content in tagged PDF. Tagged PDF works since 2026-11-21, off by default (`Options.Tagged`,
  ADR 0033): a structure tree from the HTML elements in document order (`Document`, `Div`/`Sect`/`Art`, `P`,
  `H1`–`H6`, `L`/`LI` with `Lbl` for the `::marker` and `LBody`, `Table`/`THead`/`TBody`/`TFoot`/`TR`/`TH`/
  `TD` with `Scope`, `RowSpan` and `ColSpan` and the same grid as in HTML, `Link` with a reference to its
  annotation and the link text as the annotation's `/Contents`, `Figure` with `Alt` from `alt`,
  `aria-label`, the SVG `<title>` or the `figcaption` of a `figure` holding one image, `figure` as `Div`
  with `Caption`, `Span` for inline elements), marked page content, backgrounds, borders and shadows as
  artifacts, margin boxes and repeated table headers as pagination artifacts, `/Lang` from `lang`, the
  document title shown by the viewer. PDF/A-2a and PDF/A-3a turn it on; veraPDF accepts all 22 corpus
  documents as PDF/A-2a. PDF/UA-1 (`Options.Accessibility`, `-pdfua 1`, also together with PDF/A-2a) adds
  `pdfuaid` to the XMP metadata; veraPDF accepts 22 of 22 corpus documents. Defects gopdfgo does not repair (no title, no language, an image without
  alternative text, a skipped heading level, table rows with different numbers of columns) give warnings.
  PDF/A-2b, PDF/A-3b and file attachments are supported (ADR 0016, 0026), and so are Factur-X 1.0 /
  ZUGFeRD 2.x hybrid invoices (`Options.FacturX`, ADR 0034: the invoice XML, the `fx` XMP properties with
  their extension schema, PDF/A-3b or 3a). When rendering, gopdfgo does not validate the invoice XML against
  the profile's XSD or Schematron; the caller should do that. The output was checked with the Mustang
  validator 2.26.0 on our own MINIMUM-profile invoice (ADR 0034);
- PNG or SVG output: the output is PDF only;
- PDF forms; digital signatures only at the PAdES B-B level (optional package `sign`, ADR 0050):
  `sign.SignPDF` signs any unencrypted PDF with an invisible signature as an incremental update (CMS,
  SHA-256, RSA or ECDSA, `signing-certificate-v2`); earlier signatures stay valid and veraPDF still accepts
  signed PDF/A-2b, PDF/A-3b, Factur-X and PDF/UA-1 files. Not yet: timestamps (B-T), validation data
  (B-LT, B-LTA), visible signatures, encrypted input.

Experimental features that browsers do not generally support yet: `grid-lanes` (masonry),
`column-height`, `flex-wrap: balance`.

Not done yet (status and priority in `docs/progress.md`, Polish):
- `subgrid`;
- baseline alignment in grid and flex (`last baseline`, column axis);
- some cases of positioned grid items and nested multi-column layout;
- CSS `filter`: the colour functions are exact on colours and on raster image pixels; `drop-shadow()` and
  `blur()` are vector approximations (soft-mask copies); `url()` filters on HTML elements, `backdrop-filter`
  and the `filter()` image function are not supported;
- perspective projection: `perspective()`, the `perspective` property and the perspective components of
  `matrix3d()` are accepted with a warning and ignored, so the element is drawn as an orthographic
  projection; `preserve-3d` has no depth sorting or plane intersection. 3D transforms without perspective
  (`rotateX()`, `rotate3d()`, `translate3d()`, `matrix3d()` …) and `backface-visibility` are exact;
- `<angle>` values inside `calc()` in transform functions;
- colours outside the sRGB gamut (`color(display-p3 …)`, `oklch()` …) are clipped per channel, as Chromium
  prints them, not gamut-mapped as CSS Color 4 § 13 describes;
- the colour of `-webkit-text-stroke`;
- CSS `zoom`; `mask` and `mask-image` on HTML elements (masks inside SVG work); animations and transitions
  are not evaluated, so print shows the static style;
- a box with `overflow: hidden` and a fixed height, or with `contain: size`, taller than a page is clipped
  at the page edge;
- the frozen-item rule for column flex containers works only without wrapping;
- hyphenation dictionaries (`hyphens: auto`) are built in for 24 languages: af, be, bg, da, de, el, en, es,
  et, fr, gl, it, lt, mn, nb, nl, nn, pl, pt, sk, sl, sq, te, uk. Other languages need a dictionary passed in
  `Options.Hyphenation` (ADR 0024).

## SVG

Checked 2026-11-20 and 2026-11-21 in the code (`internal/webrender/svg`), on the W3C `svg/` reftests, and on a corpus of
23 real-world SVG files (`testdata/svg`: icons, flags and coats of arms from Wikimedia Commons, charts,
a draw.io-style diagram) compared with Chrome 157.

Supported:
- shapes: `rect`, `circle`, `ellipse`, `line`, `polyline`, `polygon`, `path`;
- structure:
  - `g`, nested `svg`, `defs`, `a`;
  - `use` and `symbol`, also from another file (`<use href="icons.svg#id">`); the copied content inherits
    from the `<use>` element, as in a shadow tree;
  - `switch` with conditional attributes;
- `image` (raster and SVG), `foreignObject` (HTML content laid out in the given box);
- coordinates: `viewBox` (a zero width or height disables rendering), `preserveAspectRatio`, `transform`,
  also on the outermost `<svg>`; `overflow` other than `visible` (including `auto`) clips;
- painting:
  - fill and stroke with opacity, dash arrays, line caps and joins, `vector-effect: non-scaling-stroke`;
  - paint servers: `linearGradient`, `radialGradient`, `pattern`, also taking stops or content from another
    one through `href`, as files from Inkscape and Illustrator do;
  - `clipPath`, `mask` (also with `objectBoundingBox` content units), `marker` with `orient="auto"`,
    `context-fill`/`context-stroke`;
- text: `text`, `tspan`, `textPath` (on a path or a `path` attribute, with `startOffset`), shaped like HTML
  text (kerning, ligatures, OpenType features), `text-anchor` per text chunk, `textLength`/`lengthAdjust`,
  `dominant-baseline`, `letter-spacing`, `text-shadow`; text is written as runs, so it can be selected and searched in the PDF;
- styling: presentation attributes, `style` attributes, `<style>` inside the SVG and, for inline `<svg>` in
  HTML, the page's whole cascade (`<style>` in `<head>`, `<link rel=stylesheet>`, the user stylesheet,
  `@media`, `var()`), with inheritance from the HTML ancestors; `var()` also works in presentation
  attributes (`r="var(--r)"`); on the outermost `<svg>`, `mask` is applied by the SVG renderer and
  `opacity`, `filter`, `clip-path` and `mix-blend-mode` by the CSS box;
- inline `<svg>` in HTML, and SVG files used with `img`, `object`, `embed` and CSS `background-image`:
  - SVG files are parsed as XML;
  - an SVG file used as an image (`img`, CSS images, `<image>`) is static and loads no external resources,
    only `data:` URLs (ADR 0031); in `object`/`embed` it loads them. Its `@font-face` rules and the root
    element's `background-image` work, scoped to that file;
  - a file with `width` and `height` but no `viewBox` stretches to the image box in `img` and backgrounds,
    and stays unscaled in `object`/`embed`, as in Chrome;
  - URL fragments select part of an image (ADR 0030): `#xywh=x,y,w,h` (also `pixel:` and `percent:`,
    Media Fragments URI 1.0) crops raster and SVG images, and `#svgView(viewBox(…);preserveAspectRatio(…);
    transform(…))` or the name of a `<view>` element picks the view of an SVG file; the file is loaded once
    for all its fragments;
  - an SVG file used many times (a logo in every page header, icons in table rows, a background) is
    written once as a PDF form XObject and referenced from each use, like a raster image (ADR 0028):
    a 100 kB coat of arms on 41 pages gives a 68 kB PDF instead of 1.5 MB (measured 2026-11-21);
- links: `<a href>` (or `xlink:href`) becomes a PDF link annotation covering the bounding box of its drawn
  content after all transforms (ADR 0029). In inline `<svg>`, `#id` links go to anchors in the HTML document
  and other links are external; in `object`/`embed` only external links are kept; SVG in `img` and CSS
  images is static, so it has no links. Rotated content gets its bounding rectangle, not `QuadPoints`.

Not supported or incomplete:
- filters: drop shadow and blur (`feGaussianBlur`, `feOffset`, `feFlood`, `feComposite`, `feMerge`,
  `feDropShadow`, `feBlend`, `feColorMatrix`/`feComponentTransfer` on shadows) are drawn without
  rasterization. A blur is many soft-mask copies, so it is an approximation, within a few brightness levels
  of Chrome, and adds about 2 kB per filtered element. Colour matrices on the element itself and `feImage`,
  `feTile`, `feMorphology`, `feTurbulence`, `feConvolveMatrix`, lighting and `feDisplacementMap` skip the
  filter as a whole;
- `currentColor` passed through a context paint, and `objectBoundingBox` paint servers on a context;
- CSS `zoom`;
- structure inside SVG in tagged PDF: an SVG image is one `Figure` with alternative text;
- animation (SMIL), scripting and CSS Linked Parameters (a proposal) are out of scope.

W3C reftests in `svg/` (687 in total): 235 can run here. The rest need scripts (44) or missing files (46),
or have no reference. Of those 235, 194 pass (2026-11-21); on the morning of 2026-11-20 it was 83.

Each stand-alone `.svg` test was measured as a document drawn through `<img>`.

Remaining failures (41), the largest groups: CSS Linked Parameters (23, a proposal), CSS `zoom` (6),
the animated state in print and `use` in print (6), and single cases (`mask` on HTML elements, path bearing commands from an
SVG 2 draft, `:visited`). Two `text-shadow` tests differ from their references within the tolerance the
tests declare (`fuzzy`), which the survey does not apply.

Real-world corpus: every file is within 2.3% of Chrome's pixels; the remaining differences are font
substitution, the missing blur shadow, and anti-aliasing.

## Requirements

- Go 1.26 or newer (`go.mod`: `go 1.26.0`); built and tested with go1.26.6.
- No cgo: builds with `CGO_ENABLED=0` for any platform Go supports (checked: darwin/arm64, linux/amd64).
- TrueType or OpenType fonts from the caller (`Options.Fonts`). The fonts for generic families
  (`sans-serif`, `serif`, `monospace`) are set in `Options.GenericFamilies` (ADR 0005).
- External resources (stylesheets, images, `@font-face` fonts) come from the caller through
  `Options.Resolver`. By default the engine reads nothing from the network or the disk.
- Module dependencies, all pure Go:

| Module | Licence | Used for |
|---|---|---|
| `github.com/go-text/typesetting` | BSD-3-Clause or Unlicense | text shaping, fonts, segmentation |
| `github.com/benoitkugler/pdf` | MIT | PDF data structures |
| `github.com/benoitkugler/pstokenizer` | MIT | PostScript tokenizer (through `pdf`) |
| `github.com/hhrutter/lzw` | BSD-3-Clause | LZW filter (through `pdf`) |
| `golang.org/x/image` | BSD-3-Clause | image decoding |
| `golang.org/x/net` | BSD-3-Clause | HTML5 parser |
| `golang.org/x/text` | BSD-3-Clause | character encodings |
| `github.com/benoitkugler/textlayout` | MIT | tests only |

- Tools needed only for tests:
  - `pdftotext` and `pdftoppm` (Poppler);
  - Ghostscript, for the pixel tests of `internal/goweasyprint/pdf`;
  - veraPDF, optional, for PDF/A validation;
  - `pdfsig` (Poppler) and `openssl`, optional, for checking signatures made by the package `sign`.

## Licence

Apache License 2.0, Copyright 2026 Michał Głębowski (`LICENSE`, `NOTICE`). Code and data by other authors,
with their licences, are listed in `THIRD_PARTY_NOTICES`. Include that file when you distribute software
built with gopdfgo (ADR 0025).

## How it was measured

- Time: shortest of five runs of the whole process.
  - Chromium: `"Google Chrome" --headless --disable-gpu --no-pdf-header-footer --print-to-pdf=…`, a new
    process for each document.
  - gopdfgo: `gopdfgo -fonts … -in … -out …`.
- Memory: `/usr/bin/time -l`, „maximum resident set size”.
- File size: `stat`. Tagging: `pdfinfo`.
- Program size: `ls` of the binary built with the command in the table above. Browser size: `du -sh`.
- The source documents and the outputs are kept outside the repository.
