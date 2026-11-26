#!/usr/bin/env python3
"""summary.py <results.json> [-all]: the numbers of the README comparison table, derived from a compare.py run.

Prints a Markdown table per document and then every aggregate the README uses, each with the documents it
is computed from:

- business documents: testdata/comparison invoice-* and esf-* (4 invoices, 5 financial statements);
- invoice: invoice-b-60rows (3 pages);
- book: book-1342 (about 255 pages), book-1342-toc (with a table of contents), book-x4, book-x4-toc;
- fidelity: every document with a Chromium reference (testdata/documents and testdata/comparison).

Times are whole-process medians unless marked "in process"; memory is peak resident memory. Only documents
marked "public" count, unless -all is given. Run with `python3 -I`; method in docs/benchmarks.md.
"""
import fnmatch
import json
import statistics
import sys

ENGINES = ('folio', 'gopdfgo', 'gopdfgo-cache', 'chromium')


def fmt(x, nd=0):
    return '—' if x is None else ('%.' + str(nd) + 'f') % x


def span(values, nd=0, unit=''):
    values = [v for v in values if v is not None]
    if not values:
        return '—'
    lo, hi = min(values), max(values)
    return (fmt(lo, nd) if fmt(lo, nd) == fmt(hi, nd) else '%s–%s' % (fmt(lo, nd), fmt(hi, nd))) + unit


def main():
    report = json.load(open(sys.argv[1]))
    rows = [r for r in report['results'] if r.get('public') or '-all' in sys.argv]
    env = report['environment']
    print('Run %s, gopdfgo %s%s, folio %s, %s, %s, %s, %s GB, %s' % (
        env['date'], env['gopdfgo_commit'][:7], ' (dirty)' if env['gopdfgo_dirty'] else '', env['folio'],
        env.get('chromium', 'no Chromium'), env.get('cpu', '?'), env['os'], env.get('memory_gb', '?'), env.get('power', '')))
    print()
    by = {}
    for r in rows:
        by.setdefault(r['doc'], {})[r['engine']] = r

    def words(r):
        d = r.get('drift')
        return '—' if not d else '%d%%' % round(100 * (d['common'] - d['other_page']) / max(d['ref_words'], 1))

    ms = lambda r: r['process_s'] * 1000 if r and r.get('process_s') else None
    print('| Document | Pages Chromium / folio / gopdfgo | Process ms Chromium / folio / gopdfgo | '
          'In-process ms folio / gopdfgo / gopdfgo+cache | Peak memory MB Chromium / folio / gopdfgo | '
          'PDF kB Chromium untagged / folio / gopdfgo | Words on the same page folio / gopdfgo | dy median pt folio / gopdfgo |')
    print('|---|---|---|---|---|---|---|---|')
    for doc, e in by.items():
        ch, fo, g, gc = (e.get(x, {}) for x in ENGINES)
        kb = lambda v: v / 1000 if v else None
        dy = lambda r: fmt(r['drift']['dy_med'], 1) if r.get('drift') else '—'
        print('| `%s` | %s / %s / %s | %s / %s / %s | %s / %s / %s | %s / %s / %s | %s / %s / %s | %s / %s | %s / %s |' % (
            doc, ch.get('pages', '—'), fo.get('pages', fo.get('error', '—')), g.get('pages', '—'),
            fmt(ms(ch)), fmt(ms(fo)), fmt(ms(g)),
            fmt(fo.get('median_ms'), 1), fmt(g.get('median_ms'), 1), fmt(gc.get('median_ms'), 1),
            fmt(ch.get('rss_mb')), fmt(fo.get('rss_mb')), fmt(g.get('rss_mb')),
            fmt(kb(ch.get('size_untagged')), 1), fmt(kb(fo.get('size')), 1), fmt(kb(g.get('size')), 1),
            words(fo), words(g), dy(fo), dy(g)))
    print()

    def pick(pattern, engine):
        return [e[engine] for d, e in by.items() if fnmatch.fnmatch(d, pattern) and engine in e]

    business = lambda engine: pick('invoice-*', engine) + pick('esf-*', engine)
    print('## Aggregates')
    print()
    print('business documents (invoice-*, esf-*), whole process ms: ' + ', '.join(
        '%s %s' % (e, span([ms(r) for r in business(e)])) for e in ENGINES))
    print('business documents, peak memory MB: ' + ', '.join('%s %s' % (e, span([r.get('rss_mb') for r in business(e)])) for e in ENGINES))
    ratios = [ms(c) / ms(g) for c, g in zip(business('chromium'), business('gopdfgo')) if ms(c) and ms(g)]
    if ratios:
        print('business documents, Chromium / gopdfgo whole process: %.1f–%.1f times (median %.1f)' % (
            min(ratios), max(ratios), statistics.median(ratios)))
    inv = by.get('invoice-b-60rows', {})
    print('invoice-b-60rows: ' + ', '.join('%s process %s ms, in process %s ms, %s pages' % (
        e, fmt(ms(inv.get(e))), fmt((inv.get(e) or {}).get('median_ms'), 1), (inv.get(e) or {}).get('pages')) for e in ENGINES if e in inv))
    print('invoice PDF size kB (invoice-*; Chromium untagged / tagged): chromium %s / %s, folio %s, gopdfgo %s' % (
        span([r.get('size_untagged', 0) / 1000 for r in pick('invoice-*', 'chromium')]),
        span([r.get('size', 0) / 1000 for r in pick('invoice-*', 'chromium')]),
        span([r.get('size', 0) / 1000 for r in pick('invoice-*', 'folio')]),
        span([r.get('size', 0) / 1000 for r in pick('invoice-*', 'gopdfgo')])))
    for book in ('book-1342', 'book-1342-toc', 'book-x4', 'book-x4-toc'):
        e = by.get(book)
        if not e:
            continue
        print('%s: ' % book + ', '.join('%s %s pages, process %s s, in process %s s, memory %s GB, PDF %s MB%s' % (
            name, r.get('pages'), fmt(r.get('process_s'), 2), fmt((r.get('median_ms') or 0) / 1000 or None, 2),
            fmt((r.get('rss_mb') or 0) / 1000, 2), fmt((r.get('size_untagged') or r.get('size') or 0) / 1e6, 1),
            ' (untagged)' if r.get('size_untagged') else '') for name, r in e.items()))
    fo_ratios = [g['median_ms'] / f['median_ms'] for d, e in by.items()
                 for f, g in [(e.get('folio', {}), e.get('gopdfgo', {}))] if f.get('median_ms') and g.get('median_ms')]
    fc_ratios = [g['median_ms'] / f['median_ms'] for d, e in by.items()
                 for f, g in [(e.get('folio', {}), e.get('gopdfgo-cache', {}))] if f.get('median_ms') and g.get('median_ms')]
    if fo_ratios:
        print('in-process time gopdfgo / folio: median %.2f (with FontCache %.2f), range %.2f–%.2f over %d documents' % (
            statistics.median(fo_ratios), statistics.median(fc_ratios) if fc_ratios else 0,
            min(fo_ratios), max(fo_ratios), len(fo_ratios)))
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
