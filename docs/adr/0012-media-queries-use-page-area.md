# ADR 0012: cechy rozmiaru w `@media` oceniane względem obszaru strony z `@page`

Status: zastąpiona 2026-11-18 przez ADR 0019 (Chrome nie bierze rozmiaru z `@page`; CSS Page 3 § 7.1). Wcześniej: przyjęta 2026-11-16 (decyzja użytkownika; zamyka pytanie 20 i zastępuje punkt 2 ADR 0007
w części dotyczącej rozmiaru).

## Kontekst

ADR 0007 oceniał `width`, `height`, `aspect-ratio` i `orientation` względem stałej strony A4 w pionie
(794 na 1123 px), niezależnie od dokumentu. Media Queries 4 § 4.1 mówi, że w medium stronicowanym
`width` to szerokość obszaru strony, i tak robi Chromium przy druku. Skutki starej reguły: dokument
z `@page { size: A3 landscape }` albo wąskim wydrukiem był oceniany jak A4, a dla progów między
szerokością obszaru treści a 794 px (np. popularne 768 px) wybierana była inna gałąź niż w Chromium.

Trudność: wynik `@media` jest wyliczany przy czytaniu arkusza, a rozmiar strony wynika z `@page`, które
może stać później, w innym arkuszu albo wewnątrz `@media`.

## Decyzja

1. Rozmiarem medium jest obszar strony: rozmiar z `@page` (dla zwykłej strony, bez `:first` i stron
   nazwanych) pomniejszony o marginesy, obramowanie i odstęp wewnętrzny strony.
2. Arkusze czytane są najpierw z rozmiarem domyślnym (A4 w pionie). Jeśli żadne zapytanie nie zależy
   od rozmiaru, na tym koniec. W przeciwnym razie z reguł `@page` wyznaczany jest obszar strony
   i arkusze (autora i użytkownika) czytane są drugi raz, już z tym rozmiarem.
3. `orientation` wynika z tego samego rozmiaru. Pozostałe cechy zostają stałe jak w ADR 0007.
4. Nie ma parametru w `Options` do podania rozmiaru medium ręcznie (propozycja z pytania 20 nie została
   przyjęta).

## Skutki

- Dokument A4 z domyślnymi marginesami ma medium o szerokości ok. 644 px, więc `(min-width: 768px)`
  jest teraz fałszywe, jak w Chromium; wcześniej było prawdziwe. Układ „szeroki” wymaga mniejszych
  marginesów albo większego arkusza.
- `@page` zapisane wewnątrz `@media` zależnego od rozmiaru: rozmiar medium pochodzi z pierwszego
  czytania (z rozmiarem domyślnym) i nie jest uzgadniany dalej. Przypadek brzegowy, świadomie pominięty.
- Koszt: dokument z zapytaniem zależnym od rozmiaru ma arkusze czytane i kaskadę liczoną dwa razy.
  Dokumenty bez takich zapytań nie płacą nic.
- Rozmiar medium jest stanem globalnym pakietu `tree` ustawianym na czas jednego renderowania;
  renderowanie jest szeregowane, więc wywołania sobie nie przeszkadzają.
