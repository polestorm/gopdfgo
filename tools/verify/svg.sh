#!/bin/sh
# svg.sh <gopdfgo binary> <name>: the SVG reftests of the Web Platform Tests (235 tests in svg-list.txt), each
# .svg file printed as a standalone image through <img> (svg-wrap.sh); result in bench/out/verify/<name>-svg.tsv,
# compared with tools/verify/baselines/svg.tsv. WPT as in css.sh. Run from the repository root.
set -eu
R=$(cd "$(dirname "$0")/../.." && pwd); V=$R/tools/verify; W=$R/bench/out/verify; mkdir -p "$W"
WPT=${WPT:-$R/bench/wpt}; B=${1:?usage: svg.sh <gopdfgo binary> <name>}; n=${2:?name}
case "$B" in /*) ;; *) B=$PWD/$B ;; esac
SVG_CLI="$B" SURVEY_LIST=$V/svg-list.txt SURVEY_CROP=600 SURVEY_ARGS='-max-shrink 1 -continuous' \
	python3 -I "$V/survey.py" "$V/svg-wrap.sh" "$WPT" "$R" "$W/$n-svg.tsv" "$W/work-$n-svg" > "$W/$n-svg.log" 2>&1
python3 -I "$V/survey-diff.py" "$V/baselines/svg.tsv" "$W/$n-svg.tsv"
