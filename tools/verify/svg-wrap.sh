#!/bin/sh
# svg-wrap.sh: stands in for gopdfgo in the SVG survey (svg.sh): a .svg argument is replaced by an HTML file
# showing it through <img>, without <metadata> (the HTML parser leaves SVG at XHTML elements inside it);
# an outer svg without width or height gets 800 x 600. SVG_CLI is the real gopdfgo binary.
CLI="${SVG_CLI:?}"
svg=""; for a in "$@"; do case "$a" in *.svg) svg="$a";; esac; done
if [ -z "$svg" ]; then exec "$CLI" "$@"; fi
dir=$(dirname "$svg"); name=$(basename "$svg")
copy="$dir/__wrap_$$_$name"; wrap="$copy.html"
python3 -I -c 'import re,sys; s=open(sys.argv[1],encoding="utf-8",errors="replace").read(); open(sys.argv[2],"w",encoding="utf-8").write((s if __import__("os").environ.get("SVG_STRIP")=="0" else re.sub(r"(?s)<metadata>.*?</metadata>","",s)))' "$svg" "$copy"
head=$(tr '\n' ' ' < "$copy" | grep -o '<svg[^>]*>' | head -1)
w=""; h=""
echo "$head" | grep -q ' width=' || w="width:800px;"
echo "$head" | grep -q ' height=' || h="height:600px;"
printf '<!DOCTYPE html><style>html,body{margin:0}img{display:block;%s%s}</style><img src="%s">' "$w" "$h" "$(basename "$copy")" > "$wrap"
first=1
for a in "$@"; do
  if [ $first = 1 ]; then set --; first=0; fi
  if [ "$a" = "$svg" ]; then a="$wrap"; fi
  set -- "$@" "$a"
done
"$CLI" "$@"; rc=$?; rm -f "$wrap" "$copy"; exit $rc
