#!/usr/bin/env python3
"""scramble.py <in.html> <out.html> [seed]: a public stand-in for a private HTML document.

Keeps the markup, the stylesheet and the length of every word, and replaces the text: each letter becomes a
random letter of the same kind (lower or upper case, vowel or consonant, Polish letter with a diacritic or
without), each digit a random digit; spaces, punctuation and character references stay. The text of <title>
is replaced too; <meta> elements with dates are dropped, and comments are removed. The layout problems of
the document (tables, styles, line breaking) stay, its content does not. Used once to make the office-*
documents of testdata/comparison from documents that cannot be published (ADR 0055); the originals are not
in the repository, so this step cannot be repeated from outside, but its output is fixed in the repository.
"""
import random
import re
import sys

VOWELS, CONSONANTS = 'aeiouy', 'bcdfghjklmnprstwz'
POLISH = 'ąćęłńóśźż'


def scramble_text(text, rnd):
    out = []
    i = 0
    while i < len(text):
        c = text[i]
        if c == '&':  # odwołanie znakowe zostaje bez zmian
            m = re.match(r'&(#\d+|#x[0-9a-fA-F]+|[a-zA-Z]+);', text[i:])
            if m:
                out.append(m[0])
                i += len(m[0])
                continue
        low = c.lower()
        if low in VOWELS:
            r = rnd.choice(VOWELS)
        elif low in CONSONANTS or (low.isascii() and low.isalpha()):
            r = rnd.choice(CONSONANTS)
        elif low in POLISH:
            r = rnd.choice(POLISH)
        elif c.isdigit():
            r = str(rnd.randrange(10))
        else:
            r = c
        out.append(r.upper() if c.isupper() else r)
        i += 1
    return ''.join(out)


def scramble(html, seed):
    rnd = random.Random(seed)
    html = re.sub(r'(?s)<!--.*?-->', '', html)
    html = re.sub(r'(?i)\s*<meta name="(CreationTime|ModificationTime|Author|LastAuthor|Company)"[^>]*>', '', html)
    parts = re.split(r'(?s)(<style.*?</style>|<script.*?</script>|<[^>]*>)', html)
    return ''.join(p if p.startswith('<') else scramble_text(p, rnd) for p in parts)


if __name__ == '__main__':
    src = open(sys.argv[1], encoding='utf-8').read()
    seed = int(sys.argv[3]) if len(sys.argv) > 3 else 1
    open(sys.argv[2], 'w', encoding='utf-8').write(scramble(src, seed))
