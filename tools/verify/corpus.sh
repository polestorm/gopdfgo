#!/bin/sh
# corpus.sh <gopdfgo A> <gopdfgo B>: rasters (48 dpi, pdftoppm) of the test corpus with two binaries: the 22 documents
# of testdata/documents and the 23 files of testdata/svg, in <img> and inline; prints pages that differ (DIFF)
# and different page counts (PAGES); "fail=0" means equal. Run from the repository root.
R=$(cd "$(dirname "$0")/../.." && pwd)
M=$R/bench/out/verify; BASE=$1; NEW=$2
W=$M/corpus
rm -rf "$W"; mkdir -p "$W/out"
cp -R "$R/testdata/svg" "$W/svg"
for f in "$R"/testdata/svg/*.svg; do
  n=$(basename "$f" .svg)
  printf '<!DOCTYPE html><style>img{display:block;max-width:100%%}</style><img src="svg/%s.svg">' "$n" > "$W/img_$n.html"
  python3 -I -c 'import re,sys; s=open(sys.argv[1],encoding="utf-8").read(); s=re.sub(r"(?s)<\?xml.*?\?>|<!DOCTYPE[^>]*>","",s); open(sys.argv[2],"w").write("<!DOCTYPE html><body>"+s)' "$f" "$W/inl_$n.html"
done
fail=0
render() { # plik root
  f=$1; root=$2; n=$(basename "$f" .html)
  for b in base new; do
    bin=$NEW; [ $b = base ] && bin=$BASE
    "$bin" -in "$f" -out "$W/out/$n.$b.pdf" -fonts "$R/testdata/fonts" -root "$root" -quiet 2>/dev/null
    pdftoppm -r 48 "$W/out/$n.$b.pdf" "$W/out/$n.$b" 2>/dev/null
  done
  for p in "$W/out/$n".base-*.ppm; do
    q=$(echo "$p" | sed 's/\.base-/.new-/')
    if ! cmp -s "$p" "$q"; then echo "DIFF $n $(basename "$p")"; fail=1; fi
  done
  nb=$(ls "$W/out/$n".base-*.ppm 2>/dev/null | wc -l); nn=$(ls "$W/out/$n".new-*.ppm 2>/dev/null | wc -l)
  [ "$nb" = "$nn" ] || { echo "PAGES $n $nb $nn"; fail=1; }
  rm -f "$W/out/$n".*.ppm
}
for f in "$R"/testdata/documents/*.html; do render "$f" "$R/testdata"; done
for f in "$W"/img_*.html "$W"/inl_*.html; do render "$f" "$W"; done
echo "pdf: $(ls "$W"/out/*.base.pdf | wc -l) fail=$fail"
