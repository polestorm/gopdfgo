# Inwentarz bibliotek Go

Stan na 2026-11-12. Klocki do użycia, gdyby silnik powstawał od zera (strategia A w `estimates.md`) albo do łatania
luk w silniku adoptowanym. Gwiazdki i daty z GitHub API z dnia 2026-11-12.

| Warstwa | Biblioteka | Licencja | Stan | Uwagi |
|---|---|---|---|---|
| parser HTML5 | `golang.org/x/net/html` | BSD-3 | stabilna, w x/ | pełny algorytm WHATWG, tokenizer i budowa drzewa |
| tokenizer CSS | `github.com/tdewolff/parse/v2/css` | MIT | stabilna | tokeny i gramatyka; walidacja wartości do napisania |
| selektory CSS | `github.com/andybalholm/cascadia` | BSD-2 | 760★, push 2026-11 | poziom 3 i część 4 nad węzłami `x/net/html` |
| kształtowanie tekstu | `github.com/go-text/typesetting` | UNLICENSE/BSD (sprawdzić plik) | 162★, push 2026-11 | port HarfBuzz, parser OpenType, segmentacja UAX 14/29, `fontscan`; używany przez Gio i Fyne, współautor Benoit Kugler |
| bidi | `golang.org/x/text/unicode/bidi` | BSD-3 | stabilna | algorytm UAX 9 |
| dzielenie wyrazów | `github.com/speedata/hyphenation` | CC0 | 21★, push 2026-11 | wzorce TeX, jest `hyph-pl` |
| flexbox | `github.com/kjk/flex` | BSD-3 (sprawdzić) | port Yogi z 2017, przechodzi testy Yogi z tamtego czasu | API bliskie C; alternatywa: własna implementacja wg CSS Flexbox L1 |
| rysowanie wielowyjściowe | `github.com/tdewolff/canvas` | MIT | 1847★, push 2026-09 | jeden cel rysowania, wyjście PDF, SVG, PNG, EPS, Canvas HTML; parser fontów TTF/OTF/WOFF/WOFF2; podzbiory fontów do PDF; tekst przez go-text |
| zapis PDF (alternatywy) | `github.com/benoitkugler/pdf` (MIT, 31★), `github.com/go-pdfkit/pdfkit` (BSD-3, 3★, nowy, 100% pokrycia wg autora), `github.com/pdfcpu/pdfcpu` (Apache-2.0; przetwarzanie, nie układ) | | | pdfcpu do pieczętowania i scalania po stronie odbiorcy, niezależnie od silnika |
| SVG | `github.com/srwiley/oksvg` + `rasterx` | BSD-3 | 185★, ostatni push 2023 | parser SVG 2 częściowy i rasteryzacja; do PDF trzeba przekierować na listę wyświetlania |
| raster | `golang.org/x/image/vector` | BSD-3 | stabilna | rasteryzacja ścieżek z antyaliasingiem; backend PNG |
| obrazy | stdlib `image/png`, `image/jpeg`, `image/gif`; `x/image/webp`, `tiff`, `bmp` | BSD-3 | stabilne | |
| fonty do osadzenia | Noto Sans / Noto Serif / Noto Sans Mono (OFL) albo Inter (OFL) | OFL | | polski zestaw znaków, tabular numbers; `github.com/go-opentype/fonts` pakuje Inter, Atkinson, Lora, JetBrains Mono |
| składanie TeX-owe | `github.com/boxesandglue/boxesandglue` | BSD-3 | 263★, push 2026-11 | model pudełek i kleju, łamanie akapitów Knutha-Plassa; źródło algorytmu łamania wierszy, nie silnik CSS |

## Czego w Go nie ma gotowego

- **Kaskada i computed values** dla pełnego CSS 2.1 z `var()`, `calc()`, dziedziczeniem i wartościami względnymi.
  Obce silniki (webrender, folio, go-webengine) mają własne, nieprzenośne.
- **Układ block/inline/tabele/float** i **fragmentacja**. To jest 60% pracy własnego silnika.
- **Paged Media**: pola marginesowe, nagłówki bieżące, strony nazwane. Ma je webrender (port z WeasyPrint) i folio.
- **PDF/A** jako zamknięta funkcja: folio deklaruje; webrender nie; pdfcpu potrafi walidować (częściowo) i uzupełniać XMP.

## Co jest w Rust, gdyby to miało znaczenie

Dla porównania, nie do użycia bez FFI: Stylo (silnik CSS Servo/Firefox jako crate), Taffy (flex i grid), Parley
(układ tekstu), Blitz (HTML + CSS bez JS na tych trzech, 4,4 tys.★, eksperymentalny, bez Paged Media), Typst
(składacz z własnym językiem, deterministyczny PDF). Rust ma więcej gotowych klocków układu, Go ma lepszą
integrację z naszymi binarkami i brak FFI.
