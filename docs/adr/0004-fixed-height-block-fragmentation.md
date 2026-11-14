# ADR 0004: blok o podanej wysokości na granicy stron

Status: przyjęta 2026-11-14 (decyzja użytkownika). Rozstrzyga pytanie 16 z `open-questions.md`.

## Kontekst

Blok o podanej wysokości może nie mieścić się w reszcie strony. CSS Fragmentation 3 opisuje, jak rozłożyć
wysokość podzielonego pudełka (§ 5.1), ale nie rozstrzyga jednoznacznie, czy pusty blok wolno przeciąć
w miejscu końca strony: punkt podziału klasy C jest zdefiniowany przez odstęp od treści dzieci, a pusty blok
dzieci nie ma. Przeglądarki taki blok przecinają, o ile nie ma `break-inside: avoid`. WeasyPrint przenosił go
w całości na następną stronę, a blok wyższy niż strona zostawiał wystający poza stronę (treść ginęła).

## Decyzja

1. Blok, który zmieściłby się na pustej stronie, jest przenoszony w całości na następną stronę.
2. Blok wyższy niż obszar strony jest dzielony między strony; `break-inside: avoid` przenosi go najpierw na
   początek strony, a tam i tak jest dzielony.
3. Zachowanie przeglądarek (przecinanie także bloku, który zmieściłby się na pustej stronie) może w przyszłości
   zostać dodane jako opcja renderowania; domyślne pozostaje przenoszenie.

Kod: `internal/webrender/html/layout/block.go` (`canSplitFixedHeight`, warunek `splittable`
w `blockContainerLayout`), test `TestTallFixedHeightBlockIsSplit`.

## Uzasadnienie

W dokumentach do druku (faktury, formularze) ramki, karty i pola podpisu o stałej wysokości nie powinny być
przecinane bez wyraźnej woli autora; puste miejsce na dole strony jest mniejszym złem niż przecięta ramka.
Przewidywalność jest ważniejsza niż zgodność z przeglądarką w punkcie, którego norma nie rozstrzyga.

## Konsekwencje

- Liczba stron może różnić się od wydruku z przeglądarki dla dokumentów z wysokimi blokami o stałej wysokości.
- Ograniczenia obecnej wersji podziału: nie są dzielone elementy flex i grid, treść kolumn, bloki z `overflow`
  innym niż `visible` ani bloki, których dzieci same zostały podzielone między strony.
