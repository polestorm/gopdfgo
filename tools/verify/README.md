# tools/verify: W3C reftests, the test corpus and byte reproducibility

Run from the repository root with a `gopdfgo` binary (`go build -o bench/out/bin/gopdfgo ./cmd/gopdfgo`).
Requirements: Python 3 (standard library only) and Poppler (`pdftoppm`). The W3C tests come from
`fetch-wpt.sh`, which checks out the Web Platform Tests at the commit in `testdata/wpt/UPSTREAM` into
`bench/wpt` (`WPT=dir` uses another checkout). Results go to `bench/out/verify/`.

| Command | What it measures | Recorded result |
|---|---|---|
| `sh tools/verify/fetch-wpt.sh` | partial, sparse clone of WPT at the pinned commit (about 200 MB) | — |
| `sh tools/verify/css.sh <gopdfgo> <name>` | CSS reftests, 11 areas, 10962 tests, continuous mode cropped to 800x600 | `baselines/css.tsv`, 2026-11-24: 9484 (9020 exact, 464 edge) |
| `sh tools/verify/svg.sh <gopdfgo> <name>` | SVG reftests, 235 files printed through `<img>` | `baselines/svg.tsv`: 194 (191 exact, 3 edge) |
| `python3 -I tools/verify/w3cbytes.py <A> <B>` | same PDF bytes from two binaries on 11 689 documents in two modes (46 756 renders); the same binary twice checks determinism | 0 differences |
| `sh tools/verify/corpus.sh <A> <B>` | rasters of the 22 corpus documents and 23 SVG files | `fail=0` |

The print reftests of `testdata/wpt` (310 of 340) run with `go test ./tests/`. Passing follows ADR 0003: exact,
or equal except for one-pixel anti-aliased edges. The CSS survey and `w3cbytes.py` are long runs
(`WORKERS`/`SURVEY_WORKERS` set the parallelism, default 8); do not time anything else on the machine meanwhile.
