#!/usr/bin/env python3
"""genrand.py <output dir> [count] [seed]: random HTML documents rand-NN.html for the comparison with Chromium.

Deterministic: document i has the seed seed*1000 + i, so the same number always gives the same file (Python 3
random.Random). Contents: tables (colspan, rowspan, thead repeated on pages, tfoot), floats, lists and counters
(ol start/reversed, nested, counter-reset/counter-increment in ::before), paragraphs with various widows and
orphans, text-align: justify, break-before/after/inside, columns (column-count, column-span), page margin
boxes (@top-center, @bottom-right with counter(page)), nested bordered blocks. Fonts only from
testdata/fonts through @font-face (Noto Sans, Tinos, Arimo, Noto Sans Mono). testdata/comparison holds
rand-01 and rand-12 made with the defaults (seed 1); docs/benchmarks.md.
"""
import os, random, sys

WORDS = ('dokument strona tabela wiersz kolumna akapit liczba wartość suma razem pozycja opis kod data '
         'faktura sprawozdanie bilans aktywa pasywa przychody koszty zysk strata kapitał rezerwy należności '
         'zobowiązania środki pieniężne inwestycje okres rok miesiąc kwartał zmiana wzrost spadek udział '
         'procent stawka podatek netto brutto zaliczka termin płatności numer konto bank przelew adres '
         'spółka zarząd rada nadzorcza wspólnik uchwała umowa aneks załącznik protokół raport analiza '
         'model proces etap wynik test próba pomiar norma zgodność jakość ryzyko kontrola audyt '
         'a i o w z na do od po przy dla oraz lub także jednak który która które jest są był była').split()
FAMILIES = ['"Noto Sans"', 'Tinos', 'Arimo']


def faces():
    out = ['@font-face{font-family:"Noto Sans";src:url("../fonts/NotoSans-Regular.ttf")}',
           '@font-face{font-family:"Noto Sans";font-weight:bold;src:url("../fonts/NotoSans-Bold.ttf")}',
           '@font-face{font-family:"Noto Sans";font-style:italic;src:url("../fonts/NotoSans-Italic.ttf")}',
           '@font-face{font-family:"Noto Sans";font-weight:bold;font-style:italic;src:url("../fonts/NotoSans-BoldItalic.ttf")}',
           '@font-face{font-family:"Noto Sans Mono";src:url("../fonts/NotoSansMono-Regular.ttf")}']
    for fam in ('Tinos', 'Arimo'):
        for w, s, suf in (('normal', 'normal', 'Regular'), ('bold', 'normal', 'Bold'),
                          ('normal', 'italic', 'Italic'), ('bold', 'italic', 'BoldItalic')):
            out.append('@font-face{font-family:%s;font-weight:%s;font-style:%s;src:url("../fonts/%s/%s-%s.ttf")}'
                       % (fam, w, s, fam.lower(), fam, suf))
    return '\n'.join(out)


class Gen:
    def __init__(self, seed):
        self.r = random.Random(seed)
        self.counter = 0

    def words(self, lo, hi):
        r = self.r
        ws = [r.choice(WORDS) for _ in range(r.randint(lo, hi))]
        out = []
        for i, w in enumerate(ws):
            x = r.random()
            if x < 0.04:
                w = '<b>%s</b>' % w
            elif x < 0.07:
                w = '<i>%s</i>' % w
            elif x < 0.09:
                w = '<code>%s</code>' % w
            elif x < 0.11:
                w = '%s,' % w
            elif x < 0.12:
                w = '%d,%02d' % (r.randint(1, 99999), r.randint(0, 99))
            out.append(w)
        s = ' '.join(out)
        return s[0].upper() + s[1:] if s and s[0].isalpha() else s

    def sentence(self):
        return self.words(6, 18) + '.'

    def para(self, cls=''):
        r = self.r
        style = []
        if r.random() < 0.3:
            style.append('widows:%d;orphans:%d' % (r.randint(1, 4), r.randint(1, 4)))
        if r.random() < 0.2:
            style.append('text-align:justify')
        if r.random() < 0.1:
            style.append('text-indent:%dem' % r.randint(1, 3))
        if r.random() < 0.08:
            style.append('break-inside:avoid')
        st = ' style="%s"' % ';'.join(style) if style else ''
        return '<p%s%s>%s</p>' % (cls, st, ' '.join(self.sentence() for _ in range(r.randint(1, 6))))

    def heading(self, level):
        r = self.r
        st = ''
        if r.random() < 0.15:
            st = ' style="break-before:page"'
        return '<h%d%s>%s</h%d>' % (level, st, self.words(2, 6), level)

    def cell(self, tag='td'):
        r = self.r
        x = r.random()
        if x < 0.5:
            return self.words(1, 4)
        if x < 0.8:
            return '%d %03d,%02d' % (r.randint(0, 999), r.randint(0, 999), r.randint(0, 99))
        return self.words(4, 14)

    def table(self):
        r = self.r
        cols = r.randint(2, 7)
        rows = r.choice([3, 5, 8, 15, 30, 60])
        style = []
        if r.random() < 0.7:
            style.append('border-collapse:collapse')
        else:
            style.append('border-spacing:%dpx' % r.randint(0, 4))
        if r.random() < 0.6:
            style.append('width:100%')
        if r.random() < 0.2:
            style.append('table-layout:fixed')
        out = ['<table style="%s">' % ';'.join(style)]
        if r.random() < 0.3:
            out.append('<caption>%s</caption>' % self.words(2, 6))
        if r.random() < 0.75:
            out.append('<thead><tr>%s</tr></thead>' % ''.join('<th>%s</th>' % self.words(1, 3) for _ in range(cols)))
        if r.random() < 0.25:
            out.append('<tfoot><tr><td colspan="%d">%s</td></tr></tfoot>' % (cols, self.words(3, 8)))
        out.append('<tbody>')
        pending = [0] * cols  # rowspan zajmujące kolumny
        for i in range(rows):
            tr = ' style="break-inside:avoid"' if r.random() < 0.1 else ''
            cells, c = [], 0
            while c < cols:
                if pending[c] > 0:
                    pending[c] -= 1
                    c += 1
                    continue
                span = 1
                attrs = ''
                if r.random() < 0.12 and c < cols - 1:
                    span = r.randint(2, cols - c)
                    # colspan nie może wchodzić na kolumny zajęte przez rowspan
                    while span > 1 and any(pending[k] > 0 for k in range(c, c + span)):
                        span -= 1
                    if span > 1:
                        attrs += ' colspan="%d"' % span
                if r.random() < 0.08 and i < rows - 1:
                    rs = r.randint(2, min(4, rows - i))
                    attrs += ' rowspan="%d"' % rs
                    for k in range(c, c + span):
                        pending[k] = rs - 1
                if r.random() < 0.1:
                    attrs += ' style="text-align:right;vertical-align:%s"' % r.choice(['top', 'middle', 'bottom'])
                cells.append('<td%s>%s</td>' % (attrs, self.cell()))
                c += span
            out.append('<tr%s>%s</tr>' % (tr, ''.join(cells)))
        out.append('</tbody></table>')
        return '\n'.join(out)

    def lst(self, depth=0):
        r = self.r
        ordered = r.random() < 0.6
        tag = 'ol' if ordered else 'ul'
        attrs = ''
        if ordered and r.random() < 0.3:
            attrs += ' start="%d"' % r.randint(0, 20)
        if ordered and r.random() < 0.1:
            attrs += ' reversed'
        st = []
        if r.random() < 0.3:
            st.append('list-style-type:%s' % r.choice(['lower-alpha', 'upper-roman', 'decimal-leading-zero', 'square', 'circle', 'lower-greek']))
        if r.random() < 0.15:
            st.append('list-style-position:inside')
        if st:
            attrs += ' style="%s"' % ';'.join(st)
        items = []
        for _ in range(r.randint(2, 8)):
            body = self.words(3, 25)
            if depth < 2 and r.random() < 0.2:
                body += self.lst(depth + 1)
            elif r.random() < 0.1:
                body += self.para()
            items.append('<li>%s</li>' % body)
        return '<%s%s>%s</%s>' % (tag, attrs, ''.join(items), tag)

    def counters(self):
        r = self.r
        self.counter += 1
        name = 'c%d' % self.counter
        items = ''.join('<div class="item">%s</div>' % self.words(4, 20) for _ in range(r.randint(3, 8)))
        return ('<style>.%s{counter-reset:%s %d}.%s .item{counter-increment:%s %d;margin:.2em 0}'
                '.%s .item::before{content:counter(%s, %s) ". ";font-weight:bold}</style><div class="%s">%s</div>') % (
            name, name, r.randint(0, 5), name, name, r.choice([1, 1, 2, -1]), name, name,
            r.choice(['decimal', 'lower-roman', 'upper-alpha']), name, items)

    def floatbox(self):
        r = self.r
        side = r.choice(['left', 'right'])
        w = r.randint(20, 45)
        inner = self.words(5, 30) if r.random() < 0.6 else self.table()
        return ('<div style="float:%s;width:%d%%;margin:%s;padding:4px;border:1px solid #888;background:#f4f4f4">%s</div>'
                % (side, w, '0 0 6px 8px' if side == 'right' else '0 8px 6px 0', inner))

    def columns(self):
        r = self.r
        n = r.randint(2, 3)
        body = ''.join(self.para() for _ in range(r.randint(2, 6)))
        if r.random() < 0.3:
            body += '<h3 style="column-span:all">%s</h3>' % self.words(2, 5) + ''.join(self.para() for _ in range(r.randint(1, 4)))
        gap = r.choice(['1em', '2em', '12px'])
        rule = '; column-rule:1px solid #999' if r.random() < 0.5 else ''
        return '<div style="column-count:%d; column-gap:%s%s">%s</div>' % (n, gap, rule, body)

    def boxed(self, depth=0):
        r = self.r
        inner = ''.join(self.block(depth + 1) for _ in range(r.randint(1, 3)))
        st = 'border:%dpx solid #666;padding:%dpx;margin:%dpx 0' % (r.randint(0, 3), r.randint(0, 12), r.randint(0, 16))
        if r.random() < 0.2:
            st += ';break-inside:avoid'
        if r.random() < 0.15:
            st += ';break-after:page'
        tag = r.choice(['div', 'section', 'blockquote'])
        return '<%s style="%s">%s</%s>' % (tag, st, inner, tag)

    def block(self, depth=0):
        r = self.r
        kinds = ['para'] * 6 + ['table'] * 2 + ['list'] * 2 + ['counters', 'float', 'columns', 'pre']
        if depth < 2:
            kinds += ['boxed'] * 2
        k = r.choice(kinds)
        if k == 'para':
            return self.para()
        if k == 'table':
            return self.table()
        if k == 'list':
            return self.lst()
        if k == 'counters':
            return self.counters()
        if k == 'float':
            return self.floatbox() + self.para() + self.para()
        if k == 'columns':
            return self.columns()
        if k == 'pre':
            return '<pre>%s</pre>' % '\n'.join(self.words(2, 10) for _ in range(r.randint(2, 8))).replace('<', '').replace('>', '').replace('/', '')
        return self.boxed(depth)

    def document(self):
        r = self.r
        fam = r.choice(FAMILIES)
        size = r.choice([9, 10, 10.5, 11, 12])
        lh = r.choice(['normal', '1.2', '1.35', '1.5'])
        margin = r.choice(['20mm', '15mm 18mm', '25mm 20mm 30mm', '2cm'])
        page = r.choice(['A4', 'A4', 'A5', 'letter', 'A4 landscape'])
        boxes = ''
        if r.random() < 0.6:
            boxes += '@top-center{content:"%s";font:8pt %s;color:#555}' % (self.words(2, 4).replace('<', '').replace('>', '').replace('/', ''), fam)
        if r.random() < 0.7:
            boxes += '@bottom-right{content:"Strona " counter(page) " z " counter(pages);font:8pt %s}' % fam
        if r.random() < 0.2:
            boxes += '@bottom-left{content:"%s";font:8pt %s}' % (self.words(1, 3).replace('<', '').replace('>', '').replace('/', ''), fam)
        css = '\n'.join([faces(),
                         '@page{size:%s;margin:%s;%s}' % (page, margin, boxes),
                         'html{font:%spt/%s %s;color:#111}' % (size, lh, fam),
                         'body{margin:0}',
                         'h1,h2,h3{break-after:avoid;margin:.9em 0 .4em}',
                         'p{margin:0 0 .6em}',
                         'td,th{border:1px solid #999;padding:2px 4px;vertical-align:top}',
                         'th{background:#eee}',
                         'thead{display:table-header-group}',
                         'code,pre{font-family:"Noto Sans Mono";font-size:.9em}',
                         'pre{background:#f6f6f6;padding:4px;white-space:pre-wrap}'])
        body = ['<h1>%s</h1>' % self.words(3, 7)]
        for _ in range(r.randint(2, 5)):
            body.append(self.heading(2))
            for _ in range(r.randint(2, 7)):
                body.append(self.block())
        return '<!DOCTYPE html><html lang="pl"><head><meta charset="utf-8"><title>rand</title><style>\n%s\n</style></head><body>\n%s\n</body></html>\n' % (
            css, '\n'.join(body))


def main():
    out = sys.argv[1]
    n = int(sys.argv[2]) if len(sys.argv) > 2 else 30
    seed = int(sys.argv[3]) if len(sys.argv) > 3 else 1
    for i in range(1, n + 1):
        doc = Gen(seed * 1000 + i).document()
        path = os.path.join(out, 'rand-%02d.html' % i)
        open(path, 'w', encoding='utf-8').write(doc)
    print('generated', n)


if __name__ == '__main__':
    main()
