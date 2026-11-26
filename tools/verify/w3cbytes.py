#!/usr/bin/env python3
"""w3cbytes.py <gopdfgo A> <gopdfgo B> [work dir]: byte-for-byte comparison of the PDFs of two binaries.

Renders every W3C test of the CSS survey (baselines/css.tsv, files in the WPT checkout) and every file of
testdata/wpt, each in paged mode and in survey mode (-max-shrink 1 -continuous), with both binaries:
11 689 documents, 46 756 renders. Given the same binary twice it checks determinism (the same input gives
the same bytes); given two versions it checks that a change meant to keep the output does keep it. Prints
the number of documents, renders and differences, and progress every 250 documents on standard error. WPT as in
css.sh. Run from the repository root.
"""
import concurrent.futures as cf
import os
import subprocess
import sys
import threading
import time

REPO = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..'))
first, second = sys.argv[1:3]
out = sys.argv[3] if len(sys.argv) > 3 else os.path.join(REPO, 'bench', 'out', 'verify', 'w3cbytes')
os.makedirs(out, exist_ok=True)
wpt = os.environ.get('WPT', os.path.join(REPO, 'bench', 'wpt'))
jobs = []
for line in open(os.path.join(REPO, 'tools', 'verify', 'baselines', 'css.tsv')):
    t = line.rstrip('\n').split('\t')[2]
    if t != 'test':
        jobs.append((os.path.join(wpt, t), wpt))
for root, _, files in os.walk(os.path.join(REPO, 'testdata', 'wpt')):
    for f in sorted(files):
        if f.endswith(('.html', '.htm', '.xht', '.xhtml')):
            jobs.append((os.path.join(root, f), os.path.join(REPO, 'testdata')))
modes = {'paged': [], 'survey': ['-max-shrink', '1', '-continuous']}
fonts = os.path.join(REPO, 'testdata', 'fonts')


def run(job):
    path, rootdir = job
    res = []
    for mode, extra in modes.items():
        outs = []
        for i, b in enumerate((first, second)):
            o = os.path.join(out, '%d-%d.pdf' % (threading.get_ident(), i))
            try:
                os.remove(o)
            except FileNotFoundError:
                pass
            try:
                code = subprocess.run([b, '-quiet', '-timeout', '30s', '-fonts', fonts, '-root', rootdir, '-in', path,
                                       '-out', o] + extra, capture_output=True, timeout=90).returncode
            except subprocess.TimeoutExpired:
                code = 'timeout'
            outs.append((code, open(o, 'rb').read() if os.path.exists(o) else b''))
        if outs[0] != outs[1]:
            res.append('%s\t%s\t%s/%s\t%d/%d' % (mode, os.path.relpath(path, REPO), outs[0][0], outs[1][0],
                                                 len(outs[0][1]), len(outs[1][1])))
    return res


diff = []
start = time.time()
with cf.ThreadPoolExecutor(int(os.environ.get('WORKERS', '8'))) as ex:
    for i, r in enumerate(ex.map(run, jobs), 1):
        diff += r
        if i % 250 == 0 or i == len(jobs):
            elapsed = time.time() - start
            print('progress %d/%d documents, %d differences, %.0f s elapsed, about %.0f s left' % (
                i, len(jobs), len(diff), elapsed, elapsed / i * (len(jobs) - i)), file=sys.stderr, flush=True)
print('documents: %d, renders: %d, differences: %d' % (len(jobs), len(jobs) * 4, len(diff)))
for d in diff[:40]:
    print(d)
