#!/bin/sh
# chromium.sh <output dir> [html files…]: Chromium's reference PDFs for compare.py and wordalign.py.
#
# Prints every file (default: testdata/documents/*.html) with headless Chromium, the way the references in
# testdata/documents/reference/chromium were made:
#   chrome --headless --disable-gpu --no-pdf-header-footer --print-to-pdf=<out>.pdf file://<html>
# Fonts come from the documents' @font-face rules (testdata/fonts), so all engines get the same files.
# CHROME selects the browser binary (default: Google Chrome on macOS, else chromium on PATH). The browser
# version, the command and the SHA-256 of each PDF are written to <output dir>/chromium.json.
# Run from the repository root; method in docs/benchmarks.md.
set -eu
OUT=${1:?usage: chromium.sh <output dir> [html files…]}; shift
if [ $# -eq 0 ]; then set -- testdata/documents/*.html; fi
if [ -z "${CHROME:-}" ]; then
	CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	[ -x "$CHROME" ] || CHROME=$(command -v chromium || command -v chromium-browser || command -v google-chrome)
fi
mkdir -p "$OUT"
version=$("$CHROME" --version 2>/dev/null || echo unknown)
{
	printf '{\n "chrome": "%s",\n "command": "--headless --disable-gpu --no-pdf-header-footer --print-to-pdf=<out>.pdf file://<html>",\n "date": "%s",\n "files": {' \
		"$version" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	sep=""
	for f in "$@"; do
		n=$(basename "$f"); n=${n%.*}
		abs=$(cd "$(dirname "$f")" && pwd)/$(basename "$f")
		"$CHROME" --headless --disable-gpu --no-pdf-header-footer --print-to-pdf="$OUT/$n.pdf" "file://$abs" >/dev/null 2>&1
		printf '%s\n  "%s": "%s"' "$sep" "$n" "$(shasum -a 256 "$OUT/$n.pdf" | cut -d' ' -f1)"
		sep=","
	done
	printf '\n }\n}\n'
} > "$OUT/chromium.json"
echo "$version: $# files in $OUT"
