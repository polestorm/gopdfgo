#!/usr/bin/env python3
"""compare.py: gopdfgo and folio on the same documents, with Chromium's PDFs as the reference.

    python3 -I tools/bench/compare.py [-out DIR] [-pattern GLOB] [-engines "folio gopdfgo gopdfgo-cache"]
                                      [-extra DIR] [-process-runs 5]

Run from the repository root. The script builds both harnesses (tools/bench/gopdfgo-harness and the separate
module tools/bench/folio-harness, folio pinned in its go.mod), then for every document and engine records:

- in-process time: median of N warm renders inside one process (N = 20, 10 for -extra, 3 for the book);
- whole process: median wall-clock time of -process-runs runs of the harness rendering once
  (start, reading fonts, rendering, writing the PDF);
- peak memory: maximum resident set size of one whole-process run (/usr/bin/time);
- PDF size and page count (pdfinfo), warnings;
- word positions against Chromium's PDF of the same document (wordalign.py).

Documents: testdata/documents/*.html with references in testdata/documents/reference/chromium, and the book
from testdata/books (fetch.sh) when it is there. -extra DIR adds DIR/html/*.html with references
DIR/chrome/*.pdf; such results are marked "public": false and are not used for the published comparison.

Output: DIR/results.json (environment, commands, checksums of the harness binaries, raw results) and the PDFs;
a summary line per document on stdout. summary.py turns results.json into the numbers in the README.
Method and meaning of every number: docs/benchmarks.md. Needs Go, Poppler (pdfinfo, pdftotext) and Python 3.
"""
import argparse
import datetime
import fnmatch
import glob
import hashlib
import importlib.util
import json
import os
import platform
import re
import statistics
import subprocess
import sys
import time

REPO = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..'))
TESTDATA = os.path.join(REPO, 'testdata')
FONTS = os.path.join(TESTDATA, 'fonts')

_spec = importlib.util.spec_from_file_location('wordalign', os.path.join(REPO, 'tools', 'bench', 'wordalign.py'))
wordalign = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(wordalign)


def rel(path):
    return os.path.relpath(path, REPO) if path and path.startswith(REPO) else path


def sh(cmd, **kw):
    return subprocess.run(cmd, capture_output=True, text=True, **kw).stdout.strip()


def sha256(path):
    with open(path, 'rb') as f:
        return hashlib.sha256(f.read()).hexdigest()


def environment():
    """Sprzęt, system i wersje narzędzi, zapisywane razem z wynikiem."""
    env = dict(date=datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='seconds'),
               gopdfgo_commit=sh(['git', '-C', REPO, 'rev-parse', 'HEAD']),
               gopdfgo_dirty=bool(sh(['git', '-C', REPO, 'status', '--porcelain', '--untracked-files=no'])),
               go=sh(['go', 'version']), python=platform.python_version(),
               os=' '.join((platform.system(), platform.release(), platform.machine())),
               poppler=(subprocess.run(['pdfinfo', '-v'], capture_output=True, text=True).stderr.splitlines() or [''])[0])
    if sys.platform == 'darwin':
        env['cpu'] = sh(['sysctl', '-n', 'machdep.cpu.brand_string'])
        env['memory_gb'] = round(int(sh(['sysctl', '-n', 'hw.memsize']) or 0) / 2**30)
        env['power'] = (sh(['pmset', '-g', 'batt']).splitlines() or [''])[0]
    else:
        try:
            cpuinfo = open('/proc/cpuinfo').read()
            env['cpu'] = re.search(r'model name\s*:\s*(.*)', cpuinfo)[1]
            env['memory_gb'] = round(int(re.search(r'MemTotal:\s*(\d+)', open('/proc/meminfo').read())[1]) / 2**20)
        except (OSError, TypeError):
            pass
    env['cpus'] = os.cpu_count()
    with open(os.path.join(REPO, 'tools', 'bench', 'folio-harness', 'go.mod')) as f:
        env['folio'] = re.search(r'github.com/carlos7ags/folio (\S+)', f.read())[1]
    return env


def build(bindir):
    """Buduje oba programy pomiarowe; zwraca ścieżki i sumy SHA-256."""
    os.makedirs(bindir, exist_ok=True)
    exes = {'gopdfgo': os.path.join(bindir, 'gopdfgo-harness'), 'folio': os.path.join(bindir, 'folio-harness')}
    subprocess.run(['go', 'build', '-o', exes['gopdfgo'], './tools/bench/gopdfgo-harness'], cwd=REPO, check=True)
    subprocess.run(['go', 'build', '-o', exes['folio'], '.'], cwd=os.path.join(REPO, 'tools', 'bench', 'folio-harness'), check=True)
    return exes, {k: sha256(v) for k, v in exes.items()}


def documents(extra):
    out = []
    for f in sorted(glob.glob(os.path.join(TESTDATA, 'documents', '*.html'))):
        n = os.path.basename(f)[:-5]
        out.append(dict(name=n, html=f, root=os.path.join(TESTDATA, 'documents'), n=20, serif=False, public=True,
                        ref=os.path.join(TESTDATA, 'documents', 'reference', 'chromium', n + '.pdf')))
    book = os.path.join(TESTDATA, 'books', 'pride-and-prejudice', '1342-bench.htm')
    if os.path.exists(book):
        out.append(dict(name='book-1342', html=book, root=os.path.dirname(book), ref=None, n=3, serif=True, public=True))
    if extra:
        for f in sorted(glob.glob(os.path.join(extra, 'html', '*.html'))):
            n = os.path.basename(f)[:-5]
            out.append(dict(name=n, html=f, root=os.path.dirname(f), n=10, serif=False, public=False,
                            ref=os.path.join(extra, 'chrome', n + '.pdf')))
    return out


def pages(pdf):
    m = re.search(r'Pages:\s+(\d+)', sh(['pdfinfo', pdf]))
    return int(m[1]) if m else None


def peak_rss_mb(cmd):
    """Szczyt RSS jednego uruchomienia: /usr/bin/time -l (macOS, bajty) albo -v (Linux, kilobajty)."""
    flag = '-l' if sys.platform == 'darwin' else '-v'
    err = subprocess.run(['/usr/bin/time', flag] + cmd, capture_output=True, text=True, timeout=900).stderr
    m = re.search(r'(\d+)\s+maximum resident set size', err)
    if m:
        return int(m[1]) / 1e6
    m = re.search(r'Maximum resident set size \(kbytes\):\s*(\d+)', err)
    return int(m[1]) / 1e3 if m else None


def run(exe, engine, d, outdir, process_runs):
    pdf = os.path.join(outdir, engine, d['name'] + '.pdf')
    base = [exe, '-in', d['html'], '-root', d['root'], '-fonts', FONTS]
    if d['serif']:
        base.append('-serif-tinos')
    if engine == 'gopdfgo-cache':
        base.append('-cache')
    warm = base + ['-out', pdf, '-n', str(d['n'])]
    res = dict(engine=engine, doc=d['name'], public=d['public'],
               commands=dict(warm=[rel(a) for a in warm], process=[rel(a) for a in base]))
    try:
        r = subprocess.run(warm, capture_output=True, text=True, timeout=900)
    except subprocess.TimeoutExpired:
        res['error'] = 'timeout'
        return res
    if r.returncode != 0:
        res['error'] = (r.stderr.strip().splitlines() or ['exit %d' % r.returncode])[-1][:300]
        return res
    res.update(json.loads(r.stdout.strip().splitlines()[-1]))
    res['warm_runs'] = d['n']
    real = []
    for _ in range(process_runs):
        t = time.perf_counter()
        subprocess.run(base, capture_output=True, timeout=900)
        real.append(time.perf_counter() - t)
    res['process_s_runs'] = [round(x, 4) for x in real]
    res['process_s'] = statistics.median(real) if real else None
    res['rss_mb'] = peak_rss_mb(base)
    res['pages'] = pages(pdf)
    res['size'] = os.path.getsize(pdf) if os.path.exists(pdf) else None
    if d['ref'] and os.path.exists(d['ref']):
        res['ref_pages'] = pages(d['ref'])
        res['drift'] = wordalign.align(pdf, d['ref'])
    return res


def main():
    p = argparse.ArgumentParser(description=__doc__.split('\n\n')[0])
    p.add_argument('-out', default=os.path.join(REPO, 'bench', 'out', 'compare-' + datetime.date.today().isoformat()))
    p.add_argument('-pattern', default='*')
    p.add_argument('-engines', default='folio gopdfgo gopdfgo-cache')
    p.add_argument('-extra', default='')
    p.add_argument('-process-runs', type=int, default=5)
    a = p.parse_args()
    outdir = os.path.abspath(a.out)
    engines = a.engines.split()
    for e in engines:
        os.makedirs(os.path.join(outdir, e), exist_ok=True)
    exes, sums = build(os.path.join(outdir, 'bin'))
    report = dict(tool='tools/bench/compare.py', arguments=sys.argv[1:], environment=environment(),
                  harness_sha256=sums, process_runs=a.process_runs, results=[])
    for d in documents(a.extra and os.path.abspath(a.extra)):
        if not fnmatch.fnmatch(d['name'], a.pattern):
            continue
        for e in engines:
            r = run(exes['folio' if e == 'folio' else 'gopdfgo'], e, d, outdir, a.process_runs)
            report['results'].append(r)
            dr = r.get('drift') or {}
            print('%-26s %-13s warm %7.1f ms  process %6.3f s  rss %5s MB  pages %4s/%4s  size %8s  matched %5s/%5s other page %4s dy %s %s' % (
                d['name'], e, r.get('median_ms', -1), r.get('process_s') or -1, round(r['rss_mb']) if r.get('rss_mb') else None,
                r.get('pages'), r.get('ref_pages'), r.get('size'), dr.get('common'), dr.get('ref_words'),
                dr.get('other_page'), dr.get('dy_med'), r.get('error', '')), flush=True)
    with open(os.path.join(outdir, 'results.json'), 'w') as f:
        json.dump(report, f, indent=1, ensure_ascii=False)
    print('results:', rel(os.path.join(outdir, 'results.json')))


if __name__ == '__main__':
    main()
