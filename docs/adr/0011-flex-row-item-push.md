# ADR 0011: element wiersza flex, który nie mieści się na stronie, przechodzi na następną sam

Status: przyjęta 2026-11-16 (decyzja robocza przy wdrożeniu, według zasady z ADR 0006: tam, gdzie norma
zostawia wybór, zachowanie jak w przeglądarkach).

## Kontekst

CSS Flexbox 1 § 12 nie określa dokładnie podziału kontenera flex między strony; podaje tylko przykładowy
algorytm, w którym na następną stronę przechodzi cała linia. Tak działał gopdfgo od 2026-11-14.
Przeglądarki dzielą inaczej: każdy element linii jest rozpatrywany osobno. Elementy, które mieszczą się
na stronie, zostają; element niepodzielny, który wystaje, przechodzi na następną stronę sam, a kontener
rośnie. Tego zachowania oczekują reftesty druku W3C (`single-line-row-flex-fragmentation-042`,
`multi-line-row-flex-fragmentation-075`, `-076`).

## Decyzja

W kontenerze wierszowym element, który nie mieści się na stronie i sam się nie dzieli, przechodzi na
następną stronę pojedynczo, o ile inny element tej linii zostaje na bieżącej. Na następnej stronie:

- jego górny margines jest obcinany (CSS Fragmentation 3 § 5.2), a element staje tak daleko od góry
  strony, jak daleko poniżej podziału wypadał w układzie bez stron;
- elementy linii zakończone na poprzedniej stronie zachowują swoje miejsce w osi głównej.

Gdy nie mieści się żaden element linii, na następną stronę przechodzi cała linia (bez zmian).

## Skutki

- Rząd kart o różnej wysokości na końcu strony może się „rozjechać”: niższe karty zostają, wyższe
  przechodzą dalej. Autor, który chce trzymać rząd razem, używa `break-inside: avoid` na kontenerze.
- Zachowanie zgodne z wydrukiem Chromium i Firefoksa; reftesty druku flex: 3 zyskane.
- Nie ma parametru przywracającego przenoszenie całej linii; do dodania, jeśli okaże się potrzebny.
