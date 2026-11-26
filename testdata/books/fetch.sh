#!/bin/sh
# fetch.sh: pobiera ilustrowane wydanie HTML „Pride and Prejudice” (Project Gutenberg, nr 1342, ilustracje
# Hugh Thomsona; domena publiczna) z lustra gutenberg.pglaf.org do testdata/books/pride-and-prejudice/,
# sprawdza sumy SHA-256 z pride-and-prejudice.sha256 i tworzy 1342-bench.htm z dołączonym bench.css.
# Pliki książki (27 MB) nie są w repozytorium (.gitignore); pomiar w docs/overview.md.
set -e
D=$(cd "$(dirname "$0")" && pwd)
OUT="$D/pride-and-prejudice"
B=https://gutenberg.pglaf.org/1/3/4/1342
mkdir -p "$OUT/images"
cut -c67- "$D/pride-and-prejudice.sha256" | while read -r f; do
	[ -f "$OUT/$f" ] && continue
	case "$f" in
	LICENSE.txt) url="$B/LICENSE.txt" ;;
	*) url="$B/1342-h/$f" ;;
	esac
	curl -sf -o "$OUT/$f" "$url"
done
(cd "$OUT" && shasum -a 256 -c --quiet "$D/pride-and-prejudice.sha256")
sed 's#</head>#<link rel="stylesheet" href="../bench.css"></head>#' "$OUT/1342-h.htm" > "$OUT/1342-bench.htm"
python3 -I "$D/variants.py"
echo "ok: $OUT/1342-bench.htm"
