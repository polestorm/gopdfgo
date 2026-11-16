# ADR 0010: pismo pionowe tylko jako poddrzewo obrócone, i dopiero na potrzebę dokumentu

Status: przyjęta 2026-11-16 (decyzja użytkownika; zamyka pytanie 22).

## Kontekst

Silnik nie parsuje `writing-mode` ani `text-orientation`, a układ (ok. 33 tys. linii w `html/layout`,
pomiar 2026-11-16) liczy wszystko w szerokości i wysokości, bez osi „w wierszu” i „w bloku”.
`docs/scope.md` zalicza pismo pionowe do rzeczy poza zakresem.

Pomiar z 2026-11-16 (sondaż ekranowy 9110 reftestów WPT; testy, których źródło ustawia pionowy
`writing-mode` albo `sideways-*`): 362 testy, 272 niezaliczone (flex 58, siatka 84, `grid-lanes`
i `subgrid` 121, kolumny 6, tabele 3). Żaden nie wymaga glifów stojących. W reftestach druku 17
niezaliczonych plików ustawia `writing-mode`. W korpusie własnych dokumentów nie używa go żaden.

Tekst „na boku” da się dziś uzyskać przez `transform: rotate(-90deg)`: obrót trafia do PDF jako macierz,
tekst zostaje tekstem. Obrót nie zmienia jednak układu: autor sam rezerwuje miejsce i ustawia element.

Rozważane zakresy:

1. poddrzewo obrócone: element z pionowym `writing-mode` w poziomym otoczeniu układany jak osobny
   poziomy kontekst z zamienionymi wymiarami i rysowany z obrotem (szacunek: 2–3 paczki);
2. osie logiczne w całym układzie (szacunek: kilka tygodni, duże ryzyko regresji);
3. pełne pismo pionowe: dokument pionowy od korzenia, glify stojące, metryki pionowe fontu.

## Decyzja

- Zakresy 2 i 3 zostają poza projektem.
- Zakres 1 wdrażamy dopiero wtedy, gdy pojawi się dokument, który go potrzebuje. Do tego czasu
  `writing-mode` pozostaje nieobsługiwane.

## Skutki

- Testy W3C używające pisma pionowego jako drugiej osi (272 w sondażu) zostają niezaliczone; nie
  traktujemy ich jako braków do nadrobienia. Zakres 1 zaliczyłby z nich niewiele, bo sprawdzają
  osie logiczne w flex i siatce.
- Obrócone nagłówki tabel i etykiety robi się przez `transform: rotate()`.
- Gdy zakres 1 będzie wdrażany: poddrzewo nie dzieli się między strony, a zapis PDF nie wymaga zmian.
