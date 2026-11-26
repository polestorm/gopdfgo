#!/usr/bin/env python3
"""survey-diff.py <baseline.tsv> <new.tsv>: passed tests (pass + edge, ADR 0003) and the gains and losses of a
survey result against the baseline. Exact and edge passes are counted separately."""
import sys


def load(path):
    d = {}
    for line in open(path):
        p = line.rstrip('\n').split('\t')
        if len(p) >= 3 and p[0] != 'result':
            d[p[2]] = p[0]
    return d


a, b = load(sys.argv[1]), load(sys.argv[2])
ok = lambda r: r in ('pass', 'edge')
gains = [t for t in b if t in a and ok(b[t]) and not ok(a[t])]
losses = [t for t in b if t in a and not ok(b[t]) and ok(a[t])]
print('passed %d of %d (exact %d, edge %d) | baseline %d | gains %d, losses %d' % (
    sum(ok(r) for r in b.values()), len(b), sum(r == 'pass' for r in b.values()), sum(r == 'edge' for r in b.values()),
    sum(ok(r) for r in a.values()), len(gains), len(losses)))
for t in losses:
    print('  LOSS', a[t], '->', b[t], t)
for t in gains:
    print('  gain', t)
