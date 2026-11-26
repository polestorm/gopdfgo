#!/bin/sh
# sizes.sh <output.json>: size of the command-line programs of gopdfgo (./cmd/gopdfgo) and folio (cmd/folio at the
# commit pinned in tools/bench/folio-harness/go.mod), built with `go build` and with `go build -ldflags "-s -w"`
# (no symbol table). Run from the repository root; docs/benchmarks.md.
set -eu
OUT=${1:?usage: sizes.sh <output.json>}
R=$(pwd); T=$R/bench/out/sizes; mkdir -p "$T"
go build -o "$T/gopdfgo" ./cmd/gopdfgo
go build -ldflags "-s -w" -o "$T/gopdfgo-sw" ./cmd/gopdfgo
(cd tools/bench/folio-harness && go build -o "$T/folio" github.com/carlos7ags/folio/cmd/folio && \
	go build -ldflags "-s -w" -o "$T/folio-sw" github.com/carlos7ags/folio/cmd/folio)
size() { wc -c < "$1" | tr -d ' '; }
printf '{\n "date": "%s",\n "gopdfgo_commit": "%s",\n "go": "%s",\n "folio_version": "%s",\n "gopdfgo": %s,\n "gopdfgo_stripped": %s,\n "folio": %s,\n "folio_stripped": %s\n}\n' \
	"$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$(git rev-parse HEAD)" "$(go version)" \
	"$(sed -n 's/^require github.com\/carlos7ags\/folio //p' tools/bench/folio-harness/go.mod)" \
	"$(size "$T/gopdfgo")" "$(size "$T/gopdfgo-sw")" "$(size "$T/folio")" "$(size "$T/folio-sw")" > "$OUT"
cat "$OUT"
