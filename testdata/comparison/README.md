# testdata/comparison: business documents for the comparison with folio and Chromium

Public stand-ins for the documents that the comparison in the README measured outside the repository until
2026-11-26 (ADR 0055). Each `NAME.html` has Chromium's PDF in `reference/chromium/NAME.pdf`, made with
`tools/bench/chromium.sh` (browser version, command and checksums in `reference/chromium/chromium.json`).
Fonts come from `../fonts` (`testdata/fonts`) through `@font-face`, so every engine gets the same files.

| Files | Content | Source |
|---|---|---|
| `invoice-*` (4) | Polish e-invoices: 2, 60, 5 (several VAT rates, bank transfer) and 140 items | `go run ./tools/bench/gendocs testdata/comparison`; fictional parties and amounts |
| `esf-*` (5) | Polish financial statements: balance sheet, profit and loss account, cash flow, notes; 5–16 pages | the same generator; line item labels from the annex to the Polish Accounting Act, all values fictional |
| `office-*` (4) | Word documents converted to HTML by macOS `textutil`: an agreement, a database model with 43 tables, a terminology table, a protocol description | `tools/bench/scramble.py`: markup and styles of real documents kept, every letter and digit replaced at random; the originals are private and not in the repository |
| `md-*` (2) | Markdown pages converted by [gomdgo](https://github.com/polestorm/gomdgo) with a print stylesheet | `md-gopdfgo-overview`: an earlier version of `docs/overview.md`; `md-gomdgo-testing`: `docs/testing.md` of gomdgo |
| `mmd-p6` | three flowcharts as inline SVG | rendered by [gomermaidgo](https://github.com/polestorm/gomermaidgo) from its test corpus (`testdata/corpus/flowchart`); the embedded Recursive font is under the SIL Open Font License 1.1 (`OFL-Recursive.txt`) |
| `rand-01`, `rand-12` | random documents: tables, floats, lists, counters, columns, margin boxes, page breaks | `python3 -I tools/bench/genrand.py <dir> 12` (seed 1) |

The invoices and statements are not copies of real documents: they reproduce the kind of layout (long tables
with repeated headers, page counters in margin boxes, grids, `details`, SVG icons, a QR-like code) with made-up
content. The `office-*` text is unreadable on purpose; line breaking, tables and styles are what is measured.
