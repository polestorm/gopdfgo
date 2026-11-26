# Benchmarks: how the comparison with Chromium and folio is measured

Every number of the comparison table in the [README](../README.md) and in [`comparison.md`](comparison.md) comes
from the run described here, and every step can be repeated with the files of this repository (ADR 0055).

- [What is compared](#what-is-compared)
- [Input](#input)
- [Engines and their settings](#engines-and-their-settings)
- [What each number means](#what-each-number-means)
- [Machine and noise](#machine-and-noise)
- [Repeating the measurement](#repeating-the-measurement)
- [W3C reftests and reproducibility](#w3c-reftests-and-reproducibility)
- [Recorded runs](#recorded-runs)

## What is compared

Three ways to turn the same HTML file into PDF:

| Engine | Version | Installed by |
|---|---|---|
| gopdfgo | the commit named in the run | this repository |
| folio | `github.com/carlos7ags/folio` at commit `29bca95` (2026-11-18), pinned in `tools/bench/folio-harness/go.mod` | the Go module proxy (`go build` in that directory) |
| Chromium | Google Chrome 158.0.8127.88 for macOS, headless | the browser's installer; another Chromium build can be named with `CHROME=` |

## Input

| Set | Files | Chromium reference |
|---|---|---|
| test corpus | `testdata/documents/*.html`, 22 documents written for the scope of the project | `testdata/documents/reference/chromium` |
| business documents | `testdata/comparison/*.html`, 18 documents: invoices and financial statements with fictional content, office documents with scrambled text, Markdown and diagram pages, random documents ([its README](../testdata/comparison/README.md)) | `testdata/comparison/reference/chromium` |
| book | *Pride and Prejudice*, illustrated HTML edition of Project Gutenberg (no. 1342, public domain, 27 MB of images): `testdata/books/fetch.sh` downloads it and checks SHA-256 sums; `testdata/books/variants.py` adds the body repeated four times and the variants with a table of contents (`target-counter()`) and "Strona X z Y" under each chapter heading (`counter(pages)`) | none |

All engines get the same font files (`testdata/fonts`: Noto Sans, Noto Sans Mono, Tinos, Arimo; SIL Open Font
License) through the `@font-face` rules of the documents, and for the book the same stylesheet
`testdata/books/bench.css` (A4, 20 mm margins, Tinos). The reference PDFs are made with
`tools/bench/chromium.sh`, which records the browser version and the checksum of every file in `chromium.json`.

## Engines and their settings

- **gopdfgo**: `tools/bench/gopdfgo-harness` calls `htmlpdf.Render` with the fonts of `testdata/fonts`,
  `tinos/` and `arimo/` in `Options.Fonts`, the generic families of `testdata/fonts/families.json`, and a
  resolver that reads only the document's directory and the font directory. Default options otherwise
  (untagged PDF, Chromium-compatible pixel rounding). `gopdfgo-cache` is the same with `Options.FontCache`
  (fonts parsed once per process, ADR 0045).
- **folio**: `tools/bench/folio-harness` calls `document.NewDocument(PageSizeA4)`, `AddHTML` with a file system
  limited to the same two directories, Noto Sans as fallback font, and `WriteTo`. Network access is off.
- **Chromium**: one browser process per document, as when printing from the command line:
  `chrome --headless --disable-gpu --no-pdf-header-footer --user-data-dir=<run>/chromium-profile
  --print-to-pdf=<out>.pdf file://<document>`. Chrome writes a tagged PDF by default; for the PDF size it is
  run once more with `--disable-pdf-tagging`, because the other two write untagged PDF by default.
- Both harnesses rewrite `url(../fonts/…)` to their font directory and, with `-css`, put a stylesheet file into
  the document in place of the `<link>` to it, since files outside the document's directory are not readable.

## What each number means

| Number | How it is measured |
|---|---|
| Whole process | wall-clock time of one complete run: process start, reading the fonts, layout, writing the PDF. Median of 5 runs. For Chromium each run starts a browser; one untimed run before them warms the profile and the disk cache. |
| In process | gopdfgo and folio only: median of N renders inside one running process, after one untimed render (N = 20 for the test corpus, 10 for the business documents, 3 for the book, 1 for the four-times book). |
| Peak memory | gopdfgo and folio: maximum resident set size of one whole-process run (`/usr/bin/time -l`). Chromium: the largest sum of the resident sizes of the browser process and all its helper processes, sampled every 20 ms with `ps`. Pages shared between Chromium's processes are counted in each, so this is an upper bound. |
| PDF size | file size in bytes; Chromium untagged (see above), its default tagged size is recorded as well. |
| Pages | `pdfinfo`. "Same page count as Chromium" compares with the reference PDF. |
| Words on the same page, offset | `tools/bench/wordalign.py`: the words of both PDFs (`pdftotext -bbox`) are matched by sequence alignment; the share of Chromium's words whose match lies on the same page, and for those the median of the vertical offset per document. Computed over all 40 documents with a reference. |
| Ratios | per document, then the range (and the median) over the documents named in the table row. |

## Machine and noise

The recorded run used one Apple M4 laptop (10 cores, 32 GB, macOS 27) on mains power, with no other heavy
work. Times are wall-clock times on a desktop system: expect a few percent of noise between runs, more for
the first documents after the machine was idle. Medians of 5 runs reduce, but do not remove, that noise. The
memory sampling of Chromium runs in a separate, untimed run.

## Repeating the measurement

Requirements: Go 1.26, Python 3.9 or later (standard library only), Poppler (`pdfinfo`, `pdftotext`,
`pdftoppm`), Google Chrome or Chromium.

```
git clone https://github.com/polestorm/gopdfgo && cd gopdfgo
sh testdata/books/fetch.sh                                 # the book, checked with SHA-256
sh tools/bench/chromium.sh testdata/documents/reference/chromium          # optional: references
sh tools/bench/chromium.sh testdata/comparison/reference/chromium testdata/comparison/*.html
python3 -I tools/bench/compare.py -out bench/out/compare   # all engines, all documents
python3 -I tools/bench/summary.py bench/out/compare/results.json
```

`compare.py` writes `results.json` with the environment (date, commit, Go, operating system, processor,
memory, power source, Chrome version and checksum), the exact command of every measurement, the checksums
of the harness binaries and every raw time. `summary.py` prints the per-document table and the aggregates
used in the README. `-pattern 'invoice-*'` limits the documents, `-engines` the engines.

## W3C reftests and reproducibility

`tools/verify` holds the tools and the recorded results (see [its README](../tools/verify/README.md)). The
Web Platform Tests are checked out at the commit in `testdata/wpt/UPSTREAM` by `tools/verify/fetch-wpt.sh`.
Passing follows ADR 0003: exactly equal rasters, or equal except for one-pixel anti-aliased edges; both
counts are given.

## Recorded runs

### 2026-11-26, commit `c228df4`

- Machine: Apple M4, 10 cores, 32 GB, macOS 27 (Darwin 27.0.0, arm64), mains power, load average about 2 before
  the run; Go 1.26.6, Python 3.9.6, Poppler 26.10.0, Google Chrome 158.0.8127.88.
- Command: `python3 -I tools/bench/compare.py -out bench/out/compare-2026-11-26` (all engines, all documents,
  5 whole-process runs); duration about 11 minutes.
- Raw result: [`benchmarks/2026-11-26-compare.json`](benchmarks/2026-11-26-compare.json) (paths relative to
  the repository); the output of `summary.py` for it, with the per-document table:
  [`benchmarks/2026-11-26-summary.md`](benchmarks/2026-11-26-summary.md).
- Program sizes: `sh tools/bench/sizes.sh`, [`benchmarks/2026-11-26-sizes.json`](benchmarks/2026-11-26-sizes.json):
  gopdfgo 20.5 MB (15.1 MB with `-ldflags "-s -w"`), folio 11.7 MB (8.1 MB).
- SVG reftests at the same engine state: `sh tools/verify/svg.sh`,
  [`benchmarks/2026-11-26-svg.tsv`](benchmarks/2026-11-26-svg.tsv): 194 of 235 (191 exact, 3 edge).
- Reproducibility: the `gopdfgo` and `gopdfgo-cache` runs wrote the same bytes for all 44 documents in separate
  processes; Chromium's PDFs differed from its own reference PDFs of the same day in all 40 cases.

Numbers used in the README, as printed by `summary.py`:

| Row | gopdfgo | Chromium | folio |
|---|---|---|---|
| invoice, 3 pages (`invoice-b-60rows`), whole process | 42 ms | 563 ms | 24 ms |
| book (`book-1342`), whole process | 1.13 s, 254 pages | 2.23 s, 253 pages | 0.49 s, 281 pages |
| book with table of contents (`book-1342-toc`) | 1.36 s | 2.19 s (no page numbers) | not run |
| four-times book (`book-x4`), with contents (`book-x4-toc`) | 4.44 s, 4.99 s | 4.69 s, 4.75 s | 1.90 s, not run |
| business documents (`invoice-*`, `esf-*`), whole process | 25–158 ms | 531–640 ms | 16–71 ms |
| business documents, Chromium time / gopdfgo time | 4.0–21 (median 8.6) | | |
| business documents, peak memory | 44–104 MB | 718–822 MB | 25–49 MB |
| book, peak memory | 0.40 GB | 1.45 GB | 0.28 GB |
| invoices, PDF size (untagged) | 21–46 kB | 21–45 kB (tagged 37–377 kB) | 21–49 kB |
| book, PDF size | 26.3 MB | 26.6 MB untagged | 26.7 MB |
| page count equal to Chromium, 40 documents | 38 | reference | 24 |
| Chromium's words on the same page | 91.6% | reference | 50.2% |
| median of the per-document median vertical offsets | 0.19 pt | reference | 54 pt |
| in-process time gopdfgo / folio, 42 documents | median 2.03 (0.67–4.96); 1.58 with `FontCache` | | |

### W3C reftests, 2026-11-24

CSS reftests, 11 areas: 9484 of 10962 (9020 exact, 464 edge), `tools/verify/baselines/css.tsv`, measured on the
code committed as `c0a9f7d`. The engine changes after it (ADR 0053: no HTTP client; ADR 0054: `text-wrap:
pretty`, off by default) do not touch these tests; the SVG run of 2026-11-26 above gave the same result as the
baseline. Byte reproducibility, `tools/verify/w3cbytes.py` with the builds before and after a change meant to keep the
output: 0 differences
over 11 689 documents and 46 756 renders (2026-11-24; the log of that run was not kept). Both are long runs and
are repeated at night, then recorded here.
