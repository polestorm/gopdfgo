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

## Uzupełnienie 2026-11-20: wiersz wyższy niż cała strona

Decyzja użytkownika: wiersz wyższy niż cała strona dostępna dla wierszy (wysokość strony albo kolumny minus
powtarzany nagłówek i stopka tabeli) jest dzielony tam, gdzie wypada, a nie najpierw przenoszony na następną
stronę. Przeniesienie nic nie daje, bo na następnej stronie wiersz też się nie zmieści, a zostawia stronę prawie
pustą (np. stronę z samą resztą 1–3 px poprzedniego wiersza). CSS Fragmentation 3 § 4.4 i § 5.1: treść, która
nie mieści się we fragmentatorze, dzielimy w środku.

1. Pkt 1 decyzji obowiązuje bez zmian: wiersz niższy od strony przechodzi w całości (CSS Tables 3 § 6.1).
2. Wysokość wiersza porównujemy z górnym ograniczeniem miejsca na nowej stronie (`freshRowsHeight`: fragmentator
   minus powtarzany nagłówek i stopka). Przy przeszacowaniu wiersz najwyżej przechodzi dalej jak dotąd; nigdy nie
   jest niepotrzebnie dzielony wiersz, który zmieściłby się na następnej stronie.
3. Dotyczy wierszy wysokich przez podaną wysokość (komórki albo `tr`) i przez treść komórek. Treść niepodzielna,
   która nie mieści się na stronie, nie jest dzielona: przechodzi do dalszego ciągu swojej komórki na następnej
   stronie, a tam podlega dotychczasowym zasadom krojenia.
4. `break-inside: avoid` na wierszu albo komórce nadal przenosi także wysoki wiersz na następną stronę (pkt 2
   decyzji).
5. Wiersz, z którego w miejscu podziału nie zmieściłoby się nic (żadna treść ani wysokość), przechodzi dalej, bo
   pusty fragment nie jest postępem.

Skutek: tabele z wierszami wyższymi niż strona zajmują mniej stron niż dotąd, a powtarzany nagłówek zostaje na
każdej stronie. Wiersz z podaną wysokością `tr` wyższą niż strona wcześniej nie był dzielony wcale i jego część
za końcem strony ginęła; teraz jest dzielony. Zgodności liczby stron z Chrome nie mierzyliśmy.
