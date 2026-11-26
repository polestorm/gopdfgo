#!/usr/bin/env python3
"""variants.py: the book variants used in the comparison, made from pride-and-prejudice/1342-bench.htm
(created by fetch.sh) and written next to it:

    x4.htm          the body repeated four times (about 1020 pages)
    1342-toc.htm    a table of contents with target-counter() page numbers and "Strona X z Y" under every
                    chapter heading (counter(pages) in the page content), which needs further layout passes
    x4-toc.htm      both

Deterministic; run with `python3 -I testdata/books/variants.py` after fetch.sh.
"""
import os
import re

D = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'pride-and-prejudice')
STYLE = ('<style>\nnav.toc { break-after: page; }\nnav.toc li { list-style: none; }\n'
         'nav.toc a::after { content: " \\2014  strona " target-counter(attr(href), page); }\n'
         'h2::after { content: "Strona " counter(page) " z " counter(pages); display: block; font-size: 10pt; }\n'
         '</style>\n')
LINK = '<link rel="stylesheet" href="../bench.css">'


def times4(doc):
    start = doc.index('<body>') + len('<body>')
    end = doc.index('</body>')
    return doc[:end] + doc[start:end] * 3 + doc[end:]


def with_toc(doc, src):
    ids = re.findall(r'id="(Chapter_I|CHAPTER_[IVXLC]+)"', src)
    nav = ('<nav class="toc"><h1>Spis treści</h1><ul>\n' +
           ''.join('<li><a href="#%s">%s</a></li>\n' % (i, i.replace('_', ' ').title()) for i in ids) + '</ul></nav>\n\n')
    doc = doc.replace(LINK + '</head>', LINK + STYLE + '</head>', 1)
    return doc.replace('<body>\n', '<body>\n' + nav, 1)


def main():
    src = open(os.path.join(D, '1342-bench.htm'), encoding='utf-8').read()
    out = {'x4.htm': times4(src), '1342-toc.htm': with_toc(src, src), 'x4-toc.htm': with_toc(times4(src), src)}
    for name, doc in out.items():
        with open(os.path.join(D, name), 'w', encoding='utf-8') as f:
            f.write(doc)
        print(name, len(doc))


if __name__ == '__main__':
    main()
