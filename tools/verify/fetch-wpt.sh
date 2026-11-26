#!/bin/sh
# fetch-wpt.sh [dir]: the Web Platform Tests at the commit in testdata/wpt/UPSTREAM, for css.sh, svg.sh and
# w3cbytes.py (default dir: bench/wpt, outside git). A partial, sparse clone of the directories the surveys
# read (about 200 MB); nothing from the checkout is executed. Run from the repository root.
set -eu
R=$(cd "$(dirname "$0")/../.." && pwd)
D=${1:-$R/bench/wpt}
COMMIT=$(sed -n 's/^commit: //p' "$R/testdata/wpt/UPSTREAM")
if [ ! -d "$D/.git" ]; then
	git clone --filter=blob:none --no-checkout https://github.com/web-platform-tests/wpt.git "$D"
fi
git -C "$D" sparse-checkout set --no-cone /LICENSE.md /fonts/ /images/ /common/blank.html /css/support/ \
	/css/reference/ /css/fonts/ /css/CSS2/ /css/css-content/ /css/css-flexbox/ /css/css-grid/ /css/css-lists/ \
	/css/css-multicol/ /css/css-tables/ /css/css-text/ /css/css-backgrounds/ /css/css-fonts/ /css/css-page/ \
	/css/css-break/ /css/css-gcpm/ /svg/
git -C "$D" fetch --quiet origin "$COMMIT"
git -C "$D" -c advice.detachedHead=false checkout --quiet "$COMMIT"
echo "wpt $(git -C "$D" rev-parse HEAD) in $D"
