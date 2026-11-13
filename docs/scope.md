# Zakres: HTML, CSS, Paged Media, SVG

Stan na 2026-11-12. Trzy poziomy: **musi** (bez tego typowe dokumenty nie wychodzą poprawnie), **powinien** (potrzebne w rozsądnym
czasie), **nie będzie** (poza celem; szablony tego nie używają). Zakres obowiązuje niezależnie od tego, czy silnik
powstaje od zera, czy jest adoptowany: w drugim przypadku jest listą do sprawdzenia na korpusie.

## HTML

- **Musi**: elementy blokowe i wierszowe HTML5, `table` z `thead/tbody/tfoot`, `colgroup`, `colspan/rowspan`,
  listy, `img`, `svg` inline, `figure`, `details` (rozwinięte), `pre`, `code`, `br`, `hr`, encje, atrybuty
  `lang`, `dir`, `title`, `alt`. Formularze renderowane statycznie (wartość pola jako tekst, checkbox jako znak).
- **Powinien**: `ruby`, `bdi`, `wbr`, `picture` z wyborem pierwszego źródła.
- **Nie będzie**: `script`, `iframe`, `video`, `audio`, `canvas`, `object`, `embed`, Web Components, `contenteditable`.

## CSS: selektory i kaskada

- **Musi**: selektory poziomu 3 (typ, klasa, id, atrybut z operatorami, kombinatory, `:first/last/nth-child`,
  `:nth-of-type`, `:not`, `:root`, `:empty`, `::before`, `::after`), specyficzność, `!important`, dziedziczenie,
  `inherit/initial/unset`, zmienne `var()` z wartością domyślną, `calc()`, jednostki px/pt/mm/cm/in/em/rem/%/ch.
- **Powinien**: `:is`, `:where`, `min()/max()/clamp()`, `::first-letter`, `::first-line`, `:lang()`.
- **Nie będzie**: `:hover` i inne pseudoklasy stanu, `:has`, `@container`, `@layer`, `@supports` (traktowane jako prawda
  dla znanych właściwości), `@keyframes`, `transition`.

## CSS: model wizualny

- **Musi**: `display` block/inline/inline-block/none/list-item/table-*/flex, model pudełkowy z `box-sizing`,
  marginesy (z zapadaniem), `float`/`clear`, `position` static/relative/absolute, `z-index`, `overflow` visible/hidden,
  `width/height/min/max` w tym `auto` i procenty, `vertical-align`, `line-height`, `white-space` (wszystkie wartości),
  `word-break`, `overflow-wrap`, `text-align` z `justify`, `text-indent`, `text-transform`, `letter-spacing`,
  `text-decoration`, `color`, `background-color`, `background-image` (data:), `border-*`, `border-radius`,
  `border-collapse`, `border-spacing`, `table-layout`, `empty-cells`, `caption-side`, `list-style-*`,
  `counter-reset/increment`, `content` z `counter()`, `attr()`, łańcuchami, `quotes`, `visibility`, `opacity`.
- **Powinien**: flex w pełnym zakresie (`flex-wrap`, `align-*`, `order`, `gap`), `column-*`, `transform` 2D,
  `box-shadow`, `text-shadow`, gradienty liniowe i radialne, `object-fit`, `aspect-ratio`, `hyphens: auto`
  ze wzorcami, `font-feature-settings` (tabular numbers), `tab-size`.
- **Nie będzie**: Grid (na start; wraca, jeśli adoptowany silnik go ma), `position: sticky/fixed` poza
  `fixed` jako elementem na każdej stronie, `filter`, `mask`, `clip-path` w CSS, `writing-mode` pionowy,
  `backdrop-filter`, `mix-blend-mode`, przestrzenie barw inne niż sRGB.

## CSS: fonty

- **Musi**: `@font-face` z `src: url(data:...)` TTF/OTF, `font-family` z listą zastępczą, `font-weight` numeryczny,
  `font-style`, generyczne `serif/sans-serif/monospace` mapowane na fonty osadzone w binarce, dobór glifu zastępczego
  z fontu awaryjnego, podzbiory przy osadzaniu w PDF.
- **Powinien**: WOFF2, `unicode-range`, fonty zmienne (wybór instancji).
- **Nie będzie**: fonty systemowe (determinizm), `font-display`, fonty kolorowe.

## CSS Paged Media (poziom 3) i GCPM

- **Musi**: `@page` z `size` (nazwy, wymiary, `landscape`), `margin`, `:first`, `:left`, `:right`, `:blank`, strony
  nazwane (`page: name`), 16 pól marginesowych z `content`, `counter(page)`, `counter(pages)`, `string()`,
  `string-set`, `break-before/after/inside` (`page`, `avoid`, `left`, `right`, `avoid-page`) i aliasy `page-break-*`,
  `orphans`, `widows`, powtarzanie `thead`/`tfoot`, zakaz cięcia wiersza tabeli, cięcie bloków z marginesami i obramowaniem
  zgodnie z CSS Fragmentation (`box-decoration-break: slice`).
- **Powinien**: `position: running()` i `element()`, `target-counter()`, `target-text()`, `bookmark-level/label`,
  `box-decoration-break: clone`, `page-break` w tabelach wielowierszowych z `rowspan`.
- **Nie będzie**: przypisy (`float: footnote`) na start, `@page` z `bleed/marks`, `leader()`.

## SVG

- **Musi**: `svg`, `g`, `path`, `rect`, `circle`, `ellipse`, `line`, `polyline`, `polygon`, `text`/`tspan`
  (z fontami z CSS), `use`, `defs`, `symbol`, `clipPath`, `viewBox` z `preserveAspectRatio`, `transform`, wypełnienie
  i obrys z `opacity`, `stroke-dasharray`, `linearGradient`, `radialGradient`, style przez atrybuty i CSS.
- **Powinien**: `image` (data:), `marker`, `pattern`, `textPath`, `mask`.
- **Nie będzie**: filtry SVG, animacje SMIL, skrypty, `foreignObject`.

## Kryterium „musi”

Korpus z `roadmap.md` etap 1 zawiera dokument dla każdej pozycji „musi”. Silnik (własny lub adoptowany) jest zgodny
z zakresem, gdy korpus przechodzi, a każda pozycja „nie będzie” trafia do raportu renderowania jako ostrzeżenie.
