# ADR 0007: cechy w zapytaniach `@media` oceniane względem stałego medium odniesienia

Status: przyjęta 2026-11-15 (decyzja robocza przy wdrożeniu; do rewizji, zob. pytanie 20 w `open-questions.md`).

## Kontekst

Przejęty kod znał w `@media` tylko typ medium. Zapytanie z cechą (`print and (min-width: 10cm)`,
`(max-width: 600px)`, `(prefers-color-scheme: dark)`) unieważniało całą regułę z ostrzeżeniem. Zwykłe
arkusze używają takich zapytań powszechnie, więc dokument tracił całe bloki stylu, a z nimi bywało,
że układ „dla szerokiego ekranu”, który autor oglądał w przeglądarce.

Media Queries 4 § 4.1 mówi, że w medium stronicowanym `width` to szerokość obszaru strony. Ten rozmiar
zależy od `@page`, a arkusze są przetwarzane (i reguły `@media` rozstrzygane) przed układem stron;
`@page` może też stać w późniejszym arkuszu albo wewnątrz samego `@media`.

## Decyzja

1. Zapytania są rozbierane według Media Queries 4: typ z `not`/`only`, warunki `and`/`or`/`not`
   z nawiasami, cechy z przedrostkami `min-`/`max-` i składnia zakresów (`width >= 600px`).
2. Cechy oceniamy względem stałego medium odniesienia, niezależnego od dokumentu:
   - `width` 210 mm i `height` 297 mm (pudełko strony A4 w pionie, czyli wartość początkowa `size`),
     `orientation: portrait`;
   - `resolution` 1dppx, `color` 8, `monochrome` 0, `color-gamut: srgb`;
   - `prefers-color-scheme: light`, `prefers-reduced-motion: no-preference`;
   - `hover`, `pointer`, `scripting`, `update`: `none`; `overflow-block: paged`.
3. Nieznana cecha albo błąd składni czyni fałszywym jedno zapytanie z listy, nie całą regułę (§ 3.2).
4. Ta sama ocena obowiązuje atrybut `media` elementów `<link>` i `<style>` oraz `@import`.

## Skutki

- Reguły `@media (min-width: 768px)` są stosowane, `(max-width: 600px)` nie: dokument drukuje się
  w układzie „szerokim”, jak w podglądzie na komputerze.
- Wynik nie zależy od `@page { size }`: dokument z `size: A3 landscape` nadal jest oceniany jak A4
  w pionie. To świadome uproszczenie, deterministyczne i tanie; poprawne rozwiązanie wymaga
  wyznaczenia rozmiaru pierwszej strony przed rozstrzygnięciem reguł.
- Chromium przy druku bierze szerokość obszaru treści strony (po odjęciu marginesów), więc dla progów
  między ok. 640 a 794 px wynik może się różnić od jego wydruku.
