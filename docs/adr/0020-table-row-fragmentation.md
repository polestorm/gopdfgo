# ADR 0020: dzielenie wierszy tabeli między strony według CSS Tables 3 § 6.1

Status: przyjęta 2026-11-18 (decyzja użytkownika: „zostawmy jak jest, bo jest dobrze”).

## Kontekst

CSS Tables 3 § 6.1 (szkic): przeglądarki „must attempt to preserve the table rows unfragmented”, gdy komórki
wiersza nie są rozpięte na następne wiersze i wiersz jest co najmniej dwa razy niższy od fragmentatora
(strony albo kolumny). Wiersz wyższy albo z `rowspan` jest „freely fragmentable”: dostaje resztę wysokości
fragmentatora, a komórki ciągną się dalej. CSS Fragmentation 3 § 4.1 nie zalicza wierszy do treści
niepodzielnej.

Pomiar 2026-11-18, Chrome 157 (`--headless --print-to-pdf`), strona 1000 px: wiersz 40 px Chrome dzieli między
strony, gopdfgo przenosi w całości; wiersz na 1,5 strony oba dzielą tak samo (221 słów na stronie 1);
`tr { break-inside: avoid }` w obu przenosi wysoki wiersz na następną stronę.

## Decyzja

1. Zostaje zachowanie zgodne z normą: niski wiersz (bez `rowspan`, niższy niż pół strony) przechodzi w całości,
   wysoki się dzieli. Odstępstwo Chrome świadomie pomijamy: zasada „domyślnie jak Chromium” (ADR 0006)
   dotyczy tylko miejsc, w których norma zostawia wybór, a tu jest „must attempt”.
2. Bez przełącznika w `Options`: autor steruje przez CSS (`break-inside: avoid` na wierszu przenosi także
   wysoki wiersz, `break-before`/`break-after` wymuszają podział między wierszami).

## Skutki

- Tabela z niskimi wierszami może mieć więcej stron niż w Chrome (np. przy wysokim nagłówku: 4 zamiast 3).
- Wydruk zależy tylko od dokumentu, nie od ustawień programu.
