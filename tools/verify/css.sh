#!/bin/sh
# css.sh <gopdfgo binary> <name>: the CSS reftest survey (11 areas of the Web Platform Tests, 10962 tests) in
# continuous mode with a 800x600 crop, the way WPT compares screenshots; result in bench/out/verify/<name>.tsv,
# compared with tools/verify/baselines/css.tsv (the run behind the numbers in the README).
# WPT: the checkout made by fetch-wpt.sh (WPT=dir overrides; default bench/wpt). Run from the repository root.
set -eu
R=$(cd "$(dirname "$0")/../.." && pwd); V=$R/tools/verify; W=$R/bench/out/verify; mkdir -p "$W"
WPT=${WPT:-$R/bench/wpt}; B=${1:?usage: css.sh <gopdfgo binary> <name>}; n=${2:?name}
SURVEY_CROP=600 SURVEY_ARGS='-max-shrink 1 -continuous' python3 -I "$V/survey.py" "$B" "$WPT" "$R" "$W/$n.tsv" "$W/work-$n" \
	css/CSS2 css/css-content css/css-flexbox css/css-grid css/css-lists css/css-multicol css/css-tables css/css-text \
	css/css-backgrounds css/css-fonts > "$W/$n.log" 2>&1
tail -3 "$W/$n.log"
python3 -I "$V/survey-diff.py" "$V/baselines/css.tsv" "$W/$n.tsv"
