#!/usr/bin/env python3
"""Wybiera z klonu web-platform-tests testy nadające się dla silnika bez JavaScriptu.

Użycie:
    python3 -I testdata/wpt/select.py <katalog-klonu-wpt> testdata/wpt

Skrypt tylko czyta pliki klonu (niczego z niego nie uruchamia) i zapisuje:
    manifest.tsv   jeden wiersz na test: warstwa, temat, ścieżki, flagi
    UPSTREAM       commit i data klonu
    css/, fonts/   skopiowane testy warstw `paged` i `paged-manual` wraz z zależnościami

Warstwy:
    paged          statyczne reftesty druku (para test + wzorzec), kopiowane do repozytorium
    paged-manual   testy druku bez wzorca, z opisem oczekiwań w treści, kopiowane
    layout         statyczne reftesty ekranowe (CSS 2.1, tabele, listy, flex, fragmentacja
                   w kolumnach); tylko w manifeście, pliki zostają w klonie
"""
import os
import re
import shutil
import subprocess
import sys

TEST_EXTENSIONS = ('.html', '.htm', '.xht', '.xhtml')
SKIPPED_DIRS = {'support', 'reference', 'references', 'resources', 'crashtests', 'tools'}

# Katalogi przeglądane w poszukiwaniu testów.
PAGED_DIRS = ['css/css-page', 'css/CSS2/pagination']
MANUAL_DIRS = ['css/css-gcpm', 'css/CSS2/page-box', 'css/CSS2/pagination']
LAYOUT_DIRS = ['css/CSS2', 'css/css-tables', 'css/css-lists', 'css/css-content',
               'css/css-flexbox', 'css/css-break']

# Temat testu druku: pierwsze dopasowanie wygrywa. Tematy odpowiadają pozycjom ze scope.md.
PAGED_TOPICS = [
    (r'/margin-boxes/|margin-rules|/css-gcpm/using-strings', 'margin-boxes'),
    (r'string-set|/css-gcpm/', 'named-strings'),
    (r'page-name|named-page', 'named-pages'),
    (r'pseudo-first|page-left-right|first-page|left-right', 'page-selectors'),
    (r'page-size|dimensions|page-orientation|size-|subpixel-page', 'page-size'),
    (r'page-margin|auto-margins|/margin-|page-box|safe-printable', 'page-margins'),
    (r'page-background|page-context|page-rule|paint-order|layers|root-element', 'page-context'),
    (r'orphans|widows', 'orphans-widows'),
    (r'table|row|thead|tfoot', 'table-fragmentation'),
    (r'flex|single-line-column', 'flex-fragmentation'),
    (r'monolithic|tall-inline|overflow', 'monolithic-overflow'),
    (r'fixedpos|fixed|abspos', 'positioned'),
    (r'float', 'float-fragmentation'),
    (r'break|pagination', 'breaks'),
    (r'counter|content', 'page-counters'),
]

# Cechy spoza zakresu („nie będzie” w scope.md); trafiają do kolumny flags.
OUT_OF_SCOPE = [
    (r'<iframe', 'iframe'),
    (r'<canvas', 'canvas'),
    (r'<video|<audio|<object|<embed', 'embedded'),
    (r'writing-mode\s*:\s*(vertical|sideways|tb)|-wm-v[lr]', 'vertical-writing'),
    (r'display\s*:\s*(inline-)?grid', 'grid'),
    (r'position\s*:\s*sticky', 'sticky'),
    (r'@container|@layer', 'container-layer'),
    (r'\bfilter\s*:|clip-path\s*:|mask(-image)?\s*:', 'filter-mask'),
    (r':hover|:focus|:active', 'state-pseudo'),
]

LINK_RE = re.compile(r'<link\b[^>]*>', re.I | re.S)
ATTR_RE = re.compile(r'\b(href|src|data|poster)\s*=\s*(?:"([^"]*)"|\'([^\']*)\'|([^\s>]+))', re.I)
URL_RE = re.compile(r'url\(\s*(?:"([^"]*)"|\'([^\']*)\'|([^)\s]*))\s*\)', re.I)
IMPORT_RE = re.compile(r'@import\s+(?:"([^"]*)"|\'([^\']*)\')', re.I)
META_RE = re.compile(r'<meta\b[^>]*>', re.I | re.S)


def read_text(path):
    with open(path, encoding='utf-8', errors='replace') as handle:
        return handle.read()


def attribute(tag, name):
    match = re.search(r'\b%s\s*=\s*(?:"([^"]*)"|\'([^\']*)\'|([^\s>]+))' % name, tag, re.I)
    if not match:
        return None
    return next(group for group in match.groups() if group is not None)


def is_local(reference):
    if not reference or reference.startswith('#'):
        return False
    return not re.match(r'^[a-z][a-z0-9+.-]*:', reference, re.I) and not reference.startswith('//')


def resolve(root, source, reference):
    """Zamienia odwołanie z pliku `source` na ścieżkę względem korzenia WPT."""
    reference = reference.split('#')[0].split('?')[0]
    if not reference:
        return None
    if reference.startswith('/'):
        target = reference.lstrip('/')
    else:
        target = os.path.normpath(os.path.join(os.path.dirname(source), reference))
    if target.startswith('..'):
        return None
    return target


def references(root, path):
    """Zwraca (wzorce, zależności) pliku: wzorce to pary (match|mismatch, ścieżka)."""
    text = read_text(os.path.join(root, path))
    matches, dependencies = [], set()
    markup = path.endswith(TEST_EXTENSIONS) or path.endswith('.svg')
    if markup:
        link_targets = set()
        for tag in LINK_RE.findall(text):
            rel = (attribute(tag, 'rel') or '').lower()
            href = attribute(tag, 'href')
            if href is None:
                continue
            link_targets.add(href)
            if not is_local(href):
                continue
            target = resolve(root, path, href)
            if target is None:
                continue
            if rel in ('match', 'mismatch'):
                matches.append((rel, target))
            elif 'stylesheet' in rel:
                dependencies.add(target)
        for name, *values in ATTR_RE.findall(text):
            value = next(v for v in values if v)  if any(values) else ''
            if name.lower() == 'href':
                continue  # href poza <link> to odnośnik, nie zasób
            if is_local(value):
                target = resolve(root, path, value)
                if target:
                    dependencies.add(target)
    for groups in URL_RE.findall(text) + IMPORT_RE.findall(text):
        value = next((g for g in groups if g), '')
        if is_local(value):
            target = resolve(root, path, value)
            if target:
                dependencies.add(target)
    return matches, dependencies


def closure(root, path, cache):
    """Wszystkie pliki potrzebne do wyrenderowania `path`; None w zbiorze oznacza brak pliku."""
    needed, missing, queue = set(), set(), [path]
    while queue:
        current = queue.pop()
        if current in needed:
            continue
        if not os.path.isfile(os.path.join(root, current)):
            missing.add(current)
            continue
        needed.add(current)
        if current.endswith(TEST_EXTENSIONS + ('.css', '.svg')):
            if current not in cache:
                cache[current] = references(root, current)
            matches, dependencies = cache[current]
            queue.extend(dependencies)
            queue.extend(target for _, target in matches)
    return needed, missing


def has_script(root, files):
    for path in files:
        if path.endswith(TEST_EXTENSIONS + ('.svg',)) and '<script' in read_text(os.path.join(root, path)).lower():
            return True
    return any(path.endswith('.js') for path in files)


def is_print_test(path):
    return '-print.' in os.path.basename(path) or '/print/' in path


def is_reference_name(path):
    return re.search(r'-(not)?ref(-?\d+)?[a-z]?\.|/reference/|/references/|-ref-', path) is not None


def topic_for(tier, path):
    if tier == 'layout':
        parts = path.split('/')
        return parts[2] if parts[1] == 'CSS2' and len(parts) > 3 else parts[1]
    for pattern, topic in PAGED_TOPICS:
        if re.search(pattern, path):
            return topic
    return 'other'


def flags_for(root, path):
    text = read_text(os.path.join(root, path)).lower()
    found = [flag for pattern, flag in OUT_OF_SCOPE if re.search(pattern, text) or re.search(pattern, path)]
    for tag in META_RE.findall(text):
        if (attribute(tag, 'name') or '') == 'fuzzy':
            found.append('fuzzy')
    if 'ahem' in text:
        found.append('ahem')
    return ','.join(sorted(set(found))) or '-'


def walk_tests(root, top):
    for directory, subdirs, files in os.walk(os.path.join(root, top)):
        subdirs[:] = sorted(d for d in subdirs if d not in SKIPPED_DIRS)
        for name in sorted(files):
            if name.endswith(TEST_EXTENSIONS):
                yield os.path.relpath(os.path.join(directory, name), root)


def main():
    root, out = sys.argv[1], sys.argv[2]
    cache, rows, copied, skipped = {}, [], set(), {'script': 0, 'missing': 0}
    seen = set()

    def consider(path, tier):
        if path in seen or is_reference_name(path):
            return
        if path not in cache:
            cache[path] = references(root, path)
        matches, _ = cache[path]
        if tier == 'paged-manual':
            if matches:
                return
        elif not matches:
            return
        needed, missing = closure(root, path, cache)
        if has_script(root, needed):
            skipped['script'] += 1
            return
        if missing:
            skipped['missing'] += 1
            return
        seen.add(path)
        kind = ';'.join(sorted({rel for rel, _ in matches})) or 'manual'
        refs = ';'.join(target for _, target in matches) or '-'
        rows.append((tier, topic_for(tier, path), path, kind, refs, flags_for(root, path)))
        if tier != 'layout':
            copied.update(needed)

    for top in PAGED_DIRS:
        for path in walk_tests(root, top):
            if top == 'css/css-page' or is_print_test(path):
                consider(path, 'paged')
    for top in LAYOUT_DIRS:
        for path in walk_tests(root, top):
            if is_print_test(path):
                consider(path, 'paged')
    for top in MANUAL_DIRS:
        for path in walk_tests(root, top):
            consider(path, 'paged-manual')
    for top in LAYOUT_DIRS:
        for path in walk_tests(root, top):
            if not path.startswith(tuple(d + '/' for d in PAGED_DIRS + MANUAL_DIRS)):
                consider(path, 'layout')

    for stale in ('css', 'fonts', 'images'):
        shutil.rmtree(os.path.join(out, stale), ignore_errors=True)
    for path in sorted(copied):
        target = os.path.join(out, path)
        os.makedirs(os.path.dirname(target), exist_ok=True)
        shutil.copyfile(os.path.join(root, path), target)
    shutil.copyfile(os.path.join(root, 'LICENSE.md'), os.path.join(out, 'LICENSE.md'))

    rows.sort()
    with open(os.path.join(out, 'manifest.tsv'), 'w', encoding='utf-8') as handle:
        handle.write('tier\ttopic\ttest\tkind\treferences\tflags\n')
        for row in rows:
            handle.write('\t'.join(row) + '\n')

    def git(*arguments):
        return subprocess.run(['git', '-C', root, *arguments], capture_output=True, text=True).stdout.strip()

    with open(os.path.join(out, 'UPSTREAM'), 'w', encoding='utf-8') as handle:
        handle.write('repository: https://github.com/web-platform-tests/wpt\n')
        handle.write('commit: %s\n' % git('rev-parse', 'HEAD'))
        handle.write('commit-date: %s\n' % git('log', '-1', '--format=%cI'))

    counts = {}
    for tier, topic, *_ in rows:
        counts[(tier, topic)] = counts.get((tier, topic), 0) + 1
    for (tier, topic), count in sorted(counts.items()):
        print('%-13s %-24s %5d' % (tier, topic, count))
    for tier in ('paged', 'paged-manual', 'layout'):
        print('%-13s %-24s %5d' % (tier, 'RAZEM', sum(c for (t, _), c in counts.items() if t == tier)))
    print('skopiowane pliki: %d; pominięte: %d ze skryptem, %d z brakującą zależnością'
          % (len(copied), skipped['script'], skipped['missing']))


if __name__ == '__main__':
    main()
