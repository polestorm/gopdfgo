#!/usr/bin/env python3
"""wordalign.py <pdf dir> <reference dir> [name]: word positions of PDFs compared with reference PDFs.

For each PDF in <pdf dir> with a file of the same name in <reference dir> (Chromium's output), the words of
both files (`pdftotext -bbox`, Poppler) are matched by sequence alignment (difflib), so extra or missing
words do not shift the rest. For the matched words it reports how many are on a different page and, for
those on the same page, the median, 90th percentile and count above 3 pt of the vertical (dy) and
horizontal (dx) offset. With [name], only documents whose name contains it, plus details. Standard library
only; run with `python3 -I`. Used by compare.py (docs/benchmarks.md).
"""
import difflib
import glob
import json
import os
import re
import subprocess
import sys

WORD = re.compile(r'<word xMin="([\d.]+)" yMin="([\d.]+)" xMax="([\d.]+)" yMax="([\d.]+)">(.*)</word>')


def words(pdf):
    out = subprocess.run(['pdftotext', '-bbox', pdf, '-'], capture_output=True, text=True).stdout
    page, res = 0, []
    for line in out.splitlines():
        if '<page ' in line:
            page += 1
        m = WORD.search(line)
        if m:
            res.append((page, m[5], float(m[1]), float(m[2])))
    return res


def _stats(values):
    values = sorted(values) or [0]
    return values[len(values) // 2], values[len(values) * 9 // 10], sum(1 for v in values if v > 3)


def align(pdf, ref, details=False):
    """Słowa pdf dopasowane do słów ref; wynik jako słownik (pola opisane w docs/benchmarks.md)."""
    a, b = words(pdf), words(ref)
    sm = difflib.SequenceMatcher(None, [w[1] for w in a], [w[1] for w in b], autojunk=False)
    pairs, missing = [], []
    for tag, i1, i2, j1, j2 in sm.get_opcodes():
        if tag == 'equal':
            pairs += list(zip(a[i1:i2], b[j1:j2]))
        else:
            missing.append((' '.join(w[1] for w in a[i1:i2]), ' '.join(w[1] for w in b[j1:j2])))
    same = [(x, y) for x, y in pairs if x[0] == y[0]]
    dy = _stats(abs(x[3] - y[3]) for x, y in same)
    dx = _stats(abs(x[2] - y[2]) for x, y in same)
    res = dict(words=len(a), ref_words=len(b), common=len(pairs), other_page=len(pairs) - len(same),
               dy_med=round(dy[0], 2), dy_p90=round(dy[1], 2), dy_gt3=dy[2],
               dx_med=round(dx[0], 2), dx_p90=round(dx[1], 2), dx_gt3=dx[2])
    if details:
        res['differences'] = missing[:25]
        worst = sorted(same, key=lambda p: -(abs(p[0][3] - p[1][3]) + abs(p[0][2] - p[1][2])))[:15]
        res['worst'] = [dict(page=x[0], word=x[1], ours=(x[2], x[3]), ref=(y[2], y[3])) for x, y in worst]
        first = next(((x, y) for x, y in pairs if x[0] != y[0]), None)
        if first:
            res['first_other_page'] = dict(word=first[0][1], page=first[0][0], ref_page=first[1][0])
    return res


def main():
    only = sys.argv[3] if len(sys.argv) > 3 else None
    for f in sorted(glob.glob(os.path.join(sys.argv[1], '*.pdf'))):
        name = os.path.basename(f)[:-4]
        ref = os.path.join(sys.argv[2], name + '.pdf')
        if (only and only not in name) or not os.path.exists(ref):
            continue
        r = align(f, ref, details=bool(only))
        print('%-26s words %4d/%4d matched %4d other page %4d | dy med %.2f p90 %.2f >3pt %4d | dx med %.2f p90 %.2f >3pt %4d' % (
            name, r['words'], r['ref_words'], r['common'], r['other_page'], r['dy_med'], r['dy_p90'], r['dy_gt3'],
            r['dx_med'], r['dx_p90'], r['dx_gt3']))
        if only:
            print(json.dumps({k: r[k] for k in ('differences', 'worst', 'first_other_page') if k in r}, ensure_ascii=False, indent=1))


if __name__ == '__main__':
    main()
