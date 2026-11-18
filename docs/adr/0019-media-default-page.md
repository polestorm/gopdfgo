# ADR 0019: cechy rozmiaru w `@media` oceniane względem domyślnego arkusza, bez wpływu `@page` autora

Status: przyjęta 2026-11-18 (decyzja użytkownika po sprawdzeniu w Chrome; zastępuje ADR 0012).

## Kontekst

ADR 0012 oceniał `width`, `height`, `aspect-ratio` i `orientation` względem obszaru strony wyliczonego
z reguł `@page` autora, z powołaniem na Media Queries 4 i Chromium. Dwa źródła mówią inaczej:

- CSS Page 3 § 7.1 (normatywnie): „Media queries do not honor size: they assume the paper size that would
  be chosen if no @page rules were specified.” Deklaracja `size` w `@page` objętym zapytaniem o szerokość,
  wysokość, proporcje albo orientację jest ignorowana (zapobiega to zależności cyklicznej).
- Media Queries 4: w medium stronicowanym `width`/`height` to wymiary „page box”. Wszystkie znane
  implementacje biorą jednak obszar strony (arkusz minus marginesy); sprawa jest otwarta w CSSWG (issue
  #5437), co opisuje test W3C `css-page/media-queries-001-print`.

Pomiar 2026-11-18, Chrome 157.0.8103.79 (`--headless --print-to-pdf --no-pdf-header-footer`), dokument
z progami `min-width`/`min-height` co 10 px:

| `@page` w dokumencie | arkusz w PDF | `@media` w Chrome | `@media` w gopdfgo (ADR 0012) |
|---|---|---|---|
| brak | Letter | szer. ≥ 740, wys. ≥ 980, pion | ≥ 640, ≥ 970, pion |
| `size: A3 landscape` | A3 poziomo | ≥ 740, ≥ 980, pion | ≥ 1430, ≥ 970, poziom |
| `size: 10in; margin: 2in` | 10 × 10 in | ≥ 740, ≥ 980, pion | ≥ 570, ≥ 570, pion |
| `size: A5; margin: 0` | A5 | ≥ 740, ≥ 980, pion | ≥ 550, ≥ 790, pion |

Chrome zmienia arkusz według `@page`, ale `@media` widzi zawsze obszar domyślnego papieru z ustawień
druku (Letter minus marginesy 1 cm). Założenie ADR 0012 o Chromium było błędne.

## Decyzja

1. Rozmiarem medium dla `width`, `height`, `aspect-ratio` i `orientation` jest obszar strony domyślnego
   arkusza: A4 w pionie albo `size` z arkusza użytkownika (`Options`, CLI), pomniejszony o domyślne
   marginesy strony (arkusz domyślny, ewentualnie marginesy z arkusza użytkownika). Reguły `@page`
   autora nie mają wpływu. Arkusz użytkownika pełni u nas rolę ustawień okna druku.
2. Deklaracja `size` w `@page` wewnątrz `@media` zależnego od rozmiaru jest ignorowana (CSS Page 3 § 7.1).
3. Arkusze są czytane raz; drugie czytanie z ADR 0012 odpada.
4. Pozostałe cechy bez zmian (ADR 0007).

## Skutki

- Dokument z `@page { size: A3 landscape }` i zapytaniem o szerokość dostaje gałąź dla A4, tak jak
  w przeglądarce z domyślnym papierem A4; żeby dostać gałąź „szeroką”, trzeba podać rozmiar w arkuszu
  użytkownika (odpowiednik wyboru papieru w oknie druku).
- Różnica wobec Chrome zostaje tylko w domyślnym papierze i marginesach (u nas A4 i marginesy arkusza
  domyślnego, w Chrome papier z ustawień regionalnych, np. Letter, i 1 cm).
- Dokumenty z zapytaniami o rozmiar nie płacą już za podwójne czytanie arkuszy.
