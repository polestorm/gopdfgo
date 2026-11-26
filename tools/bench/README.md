# tools/bench: comparison with folio and Chromium

The programs behind the comparison table in the README (ADR 0055). Run everything from the repository root.
Requirements: Go, Python 3 (standard library only), Poppler (`pdfinfo`, `pdftotext`); Chromium only to remake
the reference PDFs. Working output goes to `bench/out/` (ignored by git).

| File | What it does |
|---|---|
| `compare.py` | builds both harnesses, renders every document with folio, gopdfgo, gopdfgo with `FontCache` and Chromium (one browser per document), and writes `results.json`: environment (date, commit, Go, OS, CPU, memory, power source), exact commands, SHA-256 of the harness binaries, raw times, peak memory, PDF size, pages, word positions against Chromium |
| `summary.py` | the numbers of the README table from a `results.json` (public documents only) |
| `gopdfgo-harness/` | renders one file with `htmlpdf.Render`; `-n N` times N warm renders in one process |
| `folio-harness/` | the same with folio; a separate Go module, folio pinned in its `go.mod` (commit `29bca95`, 2026-11-18) |
| `wordalign.py` | words of two PDFs matched by sequence alignment: page and position offsets |
| `gendocs/` | fictional invoices and financial statements of `testdata/comparison` (`go run ./tools/bench/gendocs testdata/comparison`) |
| `genrand.py` | random documents `rand-NN` (deterministic seeds) |
| `scramble.py` | replaces the text of a private HTML document at random, keeping markup and styles (made the `office-*` files) |
| `sizes.sh` | size of the gopdfgo and folio command-line programs, with and without the symbol table |
| `chromium.sh` | Chromium's reference PDFs, with the browser version and checksums in `chromium.json` |

```
python3 -I tools/bench/compare.py -out bench/out/compare     
python3 -I tools/bench/summary.py bench/out/compare/results.json
sh tools/bench/chromium.sh bench/out/chromium                    # optional: remake the references
sh tools/bench/sizes.sh bench/out/sizes.json
```

Documents: `testdata/documents/*.html` (test corpus) and `testdata/comparison/*.html` (business documents with
fictional content, see its README), each with Chromium's PDFs in `reference/chromium`, and the illustrated book from `testdata/books/fetch.sh` (downloaded from Project Gutenberg, checked with SHA-256).
Both engines get the same font files from `testdata/fonts` through the documents' `@font-face` rules. Close
other programs and use mains power: times are wall-clock times on a shared machine.
