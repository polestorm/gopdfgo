#!/usr/bin/env python3
"""survey.py <gopdfgo> <wpt> <repo> <out.tsv> <work dir> [areas…]: W3C reftests (Web Platform Tests) with gopdfgo.

Each test and each of its references is rendered by a separate gopdfgo process (so a hang affects one test),
rasterized with pdftoppm at 96 dpi and compared pixel by pixel (ADR 0003): "pass" = equal (at most 20 pixels
differ by more than 40 in a channel), "edge" = differences only on one-pixel edges (anti-aliasing), else
"fail"; "mismatch" references must differ. Tests are chosen by testdata/wpt/select.py: static reftests
without scripts whose letters the test fonts cover. Output: one row per test (result, area, test, detail).
Usually run through css.sh or svg.sh; method in docs/benchmarks.md. Standard library only.
"""
import threading, importlib.util, os, re, subprocess, sys, collections, concurrent.futures, hashlib, shutil

cli, wpt, repo, out_tsv, work = sys.argv[1:6]
tops = sys.argv[6:]
spec = importlib.util.spec_from_file_location('select_wpt', os.path.join(repo, 'testdata/wpt/select.py'))
sel = importlib.util.module_from_spec(spec); spec.loader.exec_module(sel)
PAGE = '@page { size: 800px 600px; margin: 0 }'
# SURVEY_CSS: dodatkowy arkusz użytkownika, np. font domyślny o metrykach Times: 'html { font-family: Tinos }'
PAGE += ' ' + os.environ.get('SURVEY_CSS', '')
FONTS = os.path.join(repo, 'testdata/fonts')
# SURVEY_TINOS=1: do fontów dochodzi testdata/fonts/tinos (katalog roboczy z dowiązaniami do obu zestawów)
if os.environ.get('SURVEY_TINOS'):
    import glob
    mixed = os.path.join(work, 'fonts-with-tinos'); os.makedirs(mixed, exist_ok=True)
    for f in glob.glob(os.path.join(FONTS, '*.ttf')) + glob.glob(os.path.join(FONTS, 'tinos', '*.ttf')):
        link = os.path.join(mixed, os.path.basename(f))
        if not os.path.exists(link): os.symlink(f, link)
    FONTS = mixed
os.makedirs(work, exist_ok=True)
# SURVEY_ARGS: dodatkowe argumenty CLI, np. '-serif Tinos' (razem z SURVEY_TINOS=1)
EXTRA = os.environ.get('SURVEY_ARGS', '').split()

# SURVEY_CROP=600: z każdej strony zostaje tylko górne N wierszy pikseli. W trybie ciągłym (-continuous)
# odpowiada to zrzutowi okna 800x600, jaki porównuje WPT: treść poniżej okna nie bierze udziału.
CROP = int(os.environ.get('SURVEY_CROP', '0'))
def crop(ppm):
    if not CROP: return ppm
    m = re.match(rb'P6\s+(\d+)\s+(\d+)\s+(\d+)\s', ppm)
    if not m: return ppm
    w, h = int(m.group(1)), int(m.group(2))
    if h <= CROP: return ppm
    return b'P6\n%d %d\n%s\n' % (w, CROP, m.group(3)) + ppm[m.end():m.end() + w * CROP * 3]

def render(path):
    key = hashlib.md5(path.encode()).hexdigest() + '-' + str(os.getpid())
    pdf = os.path.join(work, key + '.pdf')
    try:
        done = subprocess.run([cli, '-in', os.path.join(wpt, path), '-out', pdf, '-fonts', FONTS, '-root', wpt, '-css', PAGE, *EXTRA,
                               '-quiet', '-timeout', '15s'], capture_output=True, text=True, timeout=25)
    except subprocess.TimeoutExpired:
        return 'timeout', []
    if done.returncode != 0:
        return ('panic' if 'engine failure' in done.stderr else 'timeout' if 'deadline' in done.stderr else 'error'), []
    subprocess.run(['pdftoppm', '-r', '96', pdf, os.path.join(work, key)], capture_output=True)
    pages = sorted((f for f in os.listdir(work) if f.startswith(key + '-') and f.endswith('.ppm')), key=lambda f: int(f[len(key)+1:-4]))
    data = []
    for f in pages:
        data.append(crop(open(os.path.join(work, f), 'rb').read())); os.remove(os.path.join(work, f))
    os.remove(pdf)
    return 'ok', data

def thick_difference(a, b):
    """True, gdy różnice tworzą gdzieś blok 2x2 piksele (obszar grubszy niż jednopikselowa krawędź)."""
    ha = re.match(rb'P6\s+(\d+)\s+(\d+)\s+\d+\s', a); hb = re.match(rb'P6\s+(\d+)\s+(\d+)\s+\d+\s', b)
    if not ha or not hb or ha.group(0) != hb.group(0): return True
    w = int(ha.group(1)); a = a[ha.end():]; b = b[hb.end():]
    diff = set()
    for i in range(0, len(a) - 2, 3):
        if a[i] != b[i] or a[i+1] != b[i+1] or a[i+2] != b[i+2]:
            if abs(a[i]-b[i]) > 40 or abs(a[i+1]-b[i+1]) > 40 or abs(a[i+2]-b[i+2]) > 40:
                diff.add(i // 3)
                if len(diff) > 20000: return True
    return any((p + 1) in diff and (p + w) in diff and (p + w + 1) in diff and (p % w) != w - 1 for p in diff)

def different(a, b):
    if a == b: return 0
    ha = re.match(rb'P6\s+\d+\s+\d+\s+\d+\s', a); hb = re.match(rb'P6\s+\d+\s+\d+\s+\d+\s', b)
    if not ha or not hb or ha.group(0) != hb.group(0): return -1
    a, b = a[ha.end():], b[hb.end():]
    return sum(1 for i in range(0, len(a) - 2, 3) if abs(a[i]-b[i]) > 40 or abs(a[i+1]-b[i+1]) > 40 or abs(a[i+2]-b[i+2]) > 40)

import functools, multiprocessing
# Wzorce powtarzają się (ok. 11 tys. użyć, 5 tys. różnych; najczęstszy 735 razy), więc każdy proces
# roboczy pamięta ostatnio renderowane.
@functools.lru_cache(maxsize=48)
def render_ref(path):
    return render(path)

def one(test):
    matches, _ = sel.references(wpt, test)
    status, pages = render(test)
    if status != 'ok': return test, status, ''
    # Kilka odnośników "match" to alternatywy: wystarczy zgodność z jednym (konwencja WPT);
    # odnośniki "mismatch" muszą różnić się wszystkie.
    best = None
    for kind, ref in matches:
        rstatus, rpages = render_ref(ref)
        if rstatus != 'ok':
            if kind == 'match': best = best or ('ref-' + rstatus, ref); continue
            return test, 'ref-' + rstatus, ref
        equal = len(pages) == len(rpages) and all(0 <= different(x, y) <= 20 for x, y in zip(pages, rpages))
        if kind == 'mismatch':
            if equal: return test, 'fail', ''
            continue
        if equal: best = ('pass', ''); break
        if len(pages) == len(rpages) and not any(thick_difference(x, y) for x, y in zip(pages, rpages)):
            best = ('edge', '')
        elif best is None or best[0].startswith('ref-'):
            best = ('fail', '')
    if best is None: best = ('pass', '')
    return test, best[0], best[1]

tests = []
cache = {}
# SURVEY_LIST=plik: lista ścieżek testów (po jednej w wierszu) zamiast przeglądania obszarów (używa chk.sh)
LIST = os.environ.get('SURVEY_LIST')
if LIST:
    tops = []
    tests = [l.strip() for l in open(LIST) if l.strip()]
# testy z literami, których nie ma żaden font testowy, są poza pomiarem (testdata/fonts/coverage.txt)
COVERED = sel.load_coverage(os.path.join(repo, 'testdata/fonts/coverage.txt'))
uncovered = []
for top in tops:
    for path in sel.walk_tests(wpt, top):
        if sel.is_reference_name(path) or sel.is_print_test(path): continue
        cache[path] = sel.references(wpt, path)
        if not cache[path][0]: continue
        needed, missing = sel.closure(wpt, path, cache)
        if missing or sel.has_script(wpt, needed): continue
        if sel.uncovered_letters(wpt, needed, COVERED): uncovered.append(path); continue
        tests.append(path)
# Próbka: SURVEY_PART=k/n bierze testy, których skrót ścieżki daje resztę k z dzielenia przez n. Przydział
# zależy tylko od ścieżki, więc ćwiartka jest ta sama bez względu na to, które obszary są uruchamiane.
part = os.environ.get('SURVEY_PART')
if part:
    k, n = map(int, part.split('/'))
    tests = [t for t in sorted(tests) if int(hashlib.md5(t.encode()).hexdigest(), 16) % n == k]
print(len(tests), 'testów;', len(uncovered), 'pominiętych (litery spoza fontów testowych)', flush=True)
if os.environ.get('SURVEY_LIST_ONLY'):
    print('\n'.join(uncovered)); sys.exit()
results = []
# Procesy zamiast wątków: porównanie pikseli to czysty Python, a wątki blokują się na GIL.
with concurrent.futures.ProcessPoolExecutor(max_workers=int(os.environ.get('SURVEY_WORKERS', '8')), mp_context=multiprocessing.get_context('fork')) as pool:
    for i, r in enumerate(pool.map(one, tests, chunksize=8)):
        results.append(r)
        if i % 500 == 0: print(i, flush=True)
with open(out_tsv, 'w') as f:
    f.write('result\ttopic\ttest\tdetail\n')
    for test, status, detail in results:
        parts = test.split('/'); topic = parts[1] + ('/' + parts[2] if parts[1] == 'CSS2' and len(parts) > 3 else '')
        f.write('%s\t%s\t%s\t%s\n' % (status, topic, test, detail))
tot = collections.defaultdict(collections.Counter)
for test, status, _ in results:
    parts = test.split('/'); tot[parts[1] + ('/' + parts[2] if parts[1] == 'CSS2' and len(parts) > 3 else '')][status] += 1
for k in sorted(tot):
    c = tot[k]; n = sum(c.values())
    print('%-28s %4d/%4d %3d%%  %s' % (k, c['pass'], n, 100 * c['pass'] // n, ' '.join('%s=%d' % x for x in sorted(c.items()) if x[0] not in ('pass', 'fail'))))
shutil.rmtree(work, ignore_errors=True)
