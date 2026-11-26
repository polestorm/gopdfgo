#!/usr/bin/env python3
"""summary.py <results.json> [-all]: the numbers of the comparison table, derived from a compare.py run.

Prints a Markdown table per document and the aggregates used in the README: median and range of the
in-process time ratio gopdfgo / folio (also with FontCache), page counts equal to Chromium, share of
Chromium's words found on the same page, median vertical offset. Only documents marked "public" count,
unless -all is given. Run with `python3 -I`; method in docs/benchmarks.md.
"""
import json
import statistics
import sys


def fmt(x, nd=0):
    return '—' if x is None else ('%.' + str(nd) + 'f') % x


def main():
    report = json.load(open(sys.argv[1]))
    rows = [r for r in report['results'] if r.get('public') or '-all' in sys.argv]
    env = report['environment']
    print('Run %s, gopdfgo %s%s, folio %s, %s, %s' % (env['date'], env['gopdfgo_commit'][:7],
          ' (dirty)' if env['gopdfgo_dirty'] else '', env['folio'], env.get('cpu', '?'), env['os']))
    print()
    by = {}
    for r in rows:
        by.setdefault(r['doc'], {})[r['engine']] = r
    print('| Document | Pages Chromium / folio / gopdfgo | In-process ms folio / gopdfgo / gopdfgo+cache | '
          'Process ms folio / gopdfgo | Peak RSS MB folio / gopdfgo | PDF kB folio / gopdfgo | '
          'Words on the same page folio / gopdfgo | dy median pt folio / gopdfgo |')
    print('|---|---|---|---|---|---|---|---|')

    def words(r):
        d = r.get('drift')
        return '—' if not d else '%d%%' % round(100 * (d['common'] - d['other_page']) / max(d['ref_words'], 1))

    ratios, ratios_cache = [], []
    for doc, e in by.items():
        fo, g, gc = e.get('folio', {}), e.get('gopdfgo', {}), e.get('gopdfgo-cache', {})
        if fo.get('median_ms') and g.get('median_ms'):
            ratios.append(g['median_ms'] / fo['median_ms'])
            if gc.get('median_ms'):
                ratios_cache.append(gc['median_ms'] / fo['median_ms'])
        ms = lambda r: r['process_s'] * 1000 if r.get('process_s') else None
        kb = lambda r: r['size'] / 1000 if r.get('size') else None
        dy = lambda r: fmt(r['drift']['dy_med'], 1) if r.get('drift') else '—'
        print('| `%s` | %s / %s / %s | %s / %s / %s | %s / %s | %s / %s | %s / %s | %s / %s | %s / %s |' % (
            doc, g.get('ref_pages') or fo.get('ref_pages') or '—', fo.get('pages', fo.get('error', '—')), g.get('pages', '—'),
            fmt(fo.get('median_ms'), 1), fmt(g.get('median_ms'), 1), fmt(gc.get('median_ms'), 1),
            fmt(ms(fo)), fmt(ms(g)), fmt(fo.get('rss_mb')), fmt(g.get('rss_mb')),
            fmt(kb(fo), 1), fmt(kb(g), 1), words(fo), words(g), dy(fo), dy(g)))
    print()
    if ratios:
        print('in-process time gopdfgo / folio: median %.2f (with FontCache %s), range %.2f–%.2f over %d documents' % (
            statistics.median(ratios), '%.2f' % statistics.median(ratios_cache) if ratios_cache else '—',
            min(ratios), max(ratios), len(ratios)))
    for engine in ('folio', 'gopdfgo'):
        with_ref = [d[engine] for d in by.values() if d.get(engine, {}).get('ref_pages')]
        same = sum(1 for r in with_ref if r.get('pages') == r['ref_pages'])
        drifts = [r['drift'] for r in with_ref if r.get('drift')]
        total = sum(v['ref_words'] for v in drifts)
        ok = sum(v['common'] - v['other_page'] for v in drifts)
        if with_ref and total:
            print('%s: page count equal to Chromium %d of %d; Chromium words on the same page %d of %d (%.1f%%); '
                  'median of per-document dy medians %.2f pt' % (engine, same, len(with_ref), ok, total, 100 * ok / total,
                                                                statistics.median(v['dy_med'] for v in drifts)))


if __name__ == '__main__':
    main()
