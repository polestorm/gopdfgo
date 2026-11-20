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
| software | browser: 460 MB on a Linux server (`du -sh /opt/google/chrome`) plus system libraries from packages; 2.2 GB on macOS | 21.2 MB command-line binary, linux/amd64, `CGO_ENABLED=0 -trimpath -ldflags "-s -w"` |
| fonts | system fonts | supplied by the caller (in xml.pro: 2.1 MB, five Noto files) |
| installation and updates | browser package, security updates, a writable home directory for the browser process | one static binary, nothing to install on the system |

## Speed and memory

Documents:
- four variants of a Polish KSeF e-invoice rendered by `goksefgo/ksefview`: 1, 3, 1 and 7 pages;
- five e-SF financial statements from the public Polish court register (KRS), labelled A–E: 6, 11, 12, 16
  and 4 pages.

Both engines got the same font files (Noto Sans, Noto Sans Mono). All nine documents have the same page
count in both engines.

Machine: Apple M4, 10 cores, 32 GB, system load average 6.7–7.9 during the run. Times are the shortest
of five runs of the whole process, including program start.

| Document | Pages | Chromium, `--headless --print-to-pdf` (browser starts for each document) | gopdfgo, command line |
|---|---|---|---|
| invoice, 2 items | 1 | 409 ms | 49 ms |
| invoice, 60 items | 3 | 426 ms | 73 ms |
| invoice, VAT rates and bank transfer | 1 | 407 ms | 53 ms |
| invoice, 140 items | 7 | 469 ms | 109 ms |
| e-SF A | 6 | 476 ms | 128 ms |
| e-SF B | 11 | 534 ms | 202 ms |
| e-SF C | 12 | 512 ms | 197 ms |
| e-SF D | 16 | 547 ms | 244 ms |
| e-SF E | 4 | 462 ms | 93 ms |

Chromium kept running and driven through DevTools (`Page.printToPDF` via chromedp, no wait before
printing) needed 0.42–0.46 s per document. That run used the same day and the same documents; see
`docs/chromium-comparison.md` (Polish). gopdfgo is 2 to 9 times faster than that.

Memory:
- gopdfgo, peak RSS of the process (`/usr/bin/time -l`): invoice 48 MB, 6-page e-SF 95 MB, 16-page e-SF
  156 MB;
- Chromium: peak RSS summed over the browser processes, 943–961 MB, with two browser instances running at
  once, as in xml.pro.

gopdfgo renders one document at a time per process; further calls wait in a queue. Chromium can print
several tabs in parallel.

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

gopdfgo's file size comes from:
- zlib compression at level 6 (ADR 0023);
- font subsets that keep only the glyphs used and short tables.

`Options.Uncompressed` writes an uncompressed file for reading in a text editor.

## Output compared with Chromium

- KSeF invoices: same pages; every word lies within 1.1 pt of its position in Chromium. Measured with
  `pdftotext -bbox`, words matched by sequence alignment.
- e-SF statements: same pages and column layout.
  - In statement E every word is within 2.8 pt of Chromium.
  - In A–D, 14–25% of words are more than 3 pt away, and 0–72 words land on a different page.
  - The causes are not fully investigated. One is known: in A and C the „Nota podatkowa” table is scaled
    down in Chrome by the document's script, and gopdfgo does not run scripts.
- W3C screen reftests across 11 CSS areas: 9502 of 10962 pass.
- W3C print reftests: 310 of 340 pass, 308 exactly and 2 within anti-aliasing edges (ADR 0003).
- Own corpus of 22 documents: page counts as in Chromium; PDF/A-2b valid in veraPDF for 22 of 22.

Where the specifications leave a choice, gopdfgo follows Chromium; each such case is recorded in
`docs/adr/`. `Options.ExactLengths` turns off the pixel rounding that imitates Chromium (ADR 0013).

## Not supported in this version

Out of scope by design:
- JavaScript and everything that depends on it; `video`, `audio`; `canvas` stays empty. Documents embedded
  with `iframe`, `object` and `embed` are printed at the size of the element, clipped, as Chromium prints them;
- system fonts: only the fonts supplied by the caller are used, so the output is reproducible;
- vertical writing (`writing-mode`) and bidirectional or right-to-left text (ADR 0010);
- state pseudo-classes (`:hover` and similar), `@container`, `@property`, `::selection`;
- tagged PDF (PDF/UA, PDF/A-2a, PDF/A-3a). PDF/A-2b, PDF/A-3b and file attachments are supported
  (ADR 0016, 0026); the XMP extension schema that Factur-X/ZUGFeRD require is not written;
- PNG or SVG output: the output is PDF only.

Experimental features that browsers do not generally support yet: `grid-lanes` (masonry),
`column-height`, `flex-wrap: balance`.

Not done yet (status and priority in `docs/progress.md`, Polish):
- `subgrid`;
- baseline alignment in grid and flex (`last baseline`, column axis);
- some cases of positioned grid items and nested multi-column layout;
- perspective projection: `perspective()`, the `perspective` property and the perspective components of
  `matrix3d()` are accepted with a warning and ignored, so the element is drawn as an orthographic
  projection; `preserve-3d` has no depth sorting or plane intersection. 3D transforms without perspective
  (`rotateX()`, `rotate3d()`, `translate3d()`, `matrix3d()` …) and `backface-visibility` are exact;
- `<angle>` values inside `calc()` in transform functions;
- colours outside the sRGB gamut (`color(display-p3 …)`, `oklch()` …) are clipped per channel, as Chromium
  prints them, not gamut-mapped as CSS Color 4 § 13 describes;
- the colour of `-webkit-text-stroke`;
- a box with `overflow: hidden` and a fixed height, or with `contain: size`, taller than a page is clipped
  at the page edge;
- the frozen-item rule for column flex containers works only without wrapping;
- hyphenation dictionaries (`hyphens: auto`) are built in for 24 languages: af, be, bg, da, de, el, en, es,
  et, fr, gl, it, lt, mn, nb, nl, nn, pl, pt, sk, sl, sq, te, uk. Other languages need a dictionary passed in
  `Options.Hyphenation` (ADR 0024).

## SVG

Checked 2026-11-20 in the code (`internal/webrender/svg`), on the W3C `svg/` reftests, and on a corpus of
23 real-world SVG files (`testdata/svg`: icons, flags and coats of arms from Wikimedia Commons, charts,
a draw.io-style diagram) compared with Chrome 157.

Supported:
- shapes: `rect`, `circle`, `ellipse`, `line`, `polyline`, `polygon`, `path`;
- structure:
  - `g`, nested `svg`, `defs`, `a`;
  - `use` and `symbol`, also from another file (`<use href="icons.svg#id">`);
  - `switch` with conditional attributes;
- `image` (raster and SVG), `foreignObject` (HTML content laid out in the given box);
- coordinates: `viewBox`, `preserveAspectRatio`, `transform`, also on the outermost `<svg>`;
- painting:
  - fill and stroke with opacity, dash arrays, line caps and joins, `vector-effect: non-scaling-stroke`;
  - paint servers: `linearGradient`, `radialGradient`, `pattern`;
  - `clipPath`, `mask` (also with `objectBoundingBox` content units), `marker` with `orient="auto"`,
    `context-fill`/`context-stroke`;
- text: `text`, `tspan`, `textPath`, `text-anchor`, `dominant-baseline`, `letter-spacing`;
- `<style>` and `style` attributes, CSS selectors inside SVG;
- inline `<svg>` in HTML, and SVG files used with `img`, `object`, `embed` and CSS `background-image`:
  - SVG files are parsed as XML;
  - a file with `width` and `height` but no `viewBox` stretches to the image box in `img` and backgrounds,
    and stays unscaled in `object`/`embed`, as in Chrome.

Not supported or incomplete:
- filters: only `feOffset` and `feBlend`. A filter with any other primitive (`feGaussianBlur`,
  `feColorMatrix`, `feFlood`, `feComposite` …) is skipped as a whole, so the element is drawn without it;
  a drop shadow is simply missing;
- spatial media fragments in image URLs (`image.svg#xywh=…`);
- `currentColor` passed through a context paint, and `objectBoundingBox` paint servers on a context;
- page stylesheets do not reach elements inside inline `<svg>`; only `<style>` inside the `<svg>` applies;
- CSS `zoom`;
- animation (SMIL), scripting and CSS Linked Parameters (a proposal) are out of scope.

W3C reftests in `svg/` (687 in total): 235 can run here. The rest need scripts (44) or missing files (46),
or have no reference. Of those 235, 136 pass; on the morning of 2026-11-20 it was 83.

Each stand-alone `.svg` test was measured as a document drawn through `<img>`.

Remaining failures (99), the largest groups: CSS Linked Parameters (23, a proposal), media fragments (12),
SVG text with blur shadows or page stylesheets, CSS `zoom`, and single cases.

Real-world corpus: every file is within 2.5% of Chrome's pixels; the remaining differences are font
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
  - veraPDF, optional, for PDF/A validation.

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
