# ADR 0038: Łamanie wiersza po każdej spacji i po ukośniku na początku słowa

Status: przyjęta 2026-11-22.

## Kontekst

Porównanie dokumentacji Markdown z Chromium (`bench/realdocs`, dokumenty `md-*` przepuszczone przez gomdgo)
pokazało, że w komórkach tabel wstawki kodu w rodzaju „`@page :first/:left/:right`” albo „`float, position:
absolute`” były u nas nierozerwalne, a w Chromium łamią się po spacji. Szerokość minimalna kolumny rosła,
tabela wychodziła poza obszar strony, a pomniejszanie treści szerszej niż strona (ADR 0014) zmniejszało cały
dokument o 7%: inne łamanie każdego akapitu i inne podziały stron (`engine-fit`: 511 słów przesuniętych
o ponad 3 pt, 191 na innej stronie; `overview`: 1545 i 36).

UAX #14 (rewizja 57) zabrania łamania po spacji przed znakami zamykającymi, wykrzyknikiem, dwukropkiem,
przecinkiem, kropką i ukośnikiem (LB13, LB15d: „× CL”, „× IS” także po spacjach) oraz po nawiasie
otwierającym i cudzysłowie, za którymi stoją spacje (LB14–LB17). CSS Text 3 § 5 opisuje UAX #14 jako punkt
wyjścia, który „ma być dalej dostosowany”, a § 5.2 wymienia odstępstwa pożądane dla zgodności
z istniejącymi implementacjami.

Pomiar w Chrome 157 (2026-11-22, `width: min-content`): po spacji U+0020 Chromium łamie przed każdym znakiem
(„@page |:first”, „ab |) x”, „ab |( |cd”, „mot |!”, „ab |„ cd”); po U+00A0, U+3000 i U+2003 nie. Po ukośniku
na początku słowa nie łamie („xx |/Lang”, „xx |/opt/google/chrome”, „(/Lang)” także gdy `/Lang` jest wstawką
kodu); CSS Text 3 § 5.5 podaje ten sam przykład: „check /etc” nigdy nie łamie się między „/” a „e”.

## Decyzja

1. Po ostatniej spacji U+0020 ciągu spacji stojącego przed znakiem niebiałym jest zawsze okazja do złamania
   wiersza (`text/engine_gotext.go` `breakAfterSpaces`: spacja w tekście do wyznaczania podziałów zamieniona
   na U+200B, po którym UAX #14 łamie zawsze, LB8). Znaki klejące (GL, WJ, ZWJ, U+00A0) i inne odstępy niż
   U+0020 zostają przy regułach UAX #14. Przy `white-space: break-spaces` reguła nie działa. Dotyczy łamania
   wierszy i szerokości min-content.
2. Ukośnik na początku tekstu albo po białym znaku podlega regule ADR 0035 tak jak ukośnik w środku słowa:
   traci okazję do złamania po sobie, gdy następna okazja jest bliżej niż 20 znaków.
3. Dywiz na początku słowa zostaje przy UAX #14 LB20a („-update” się nie łamie). Chromium łamie po nim
   („-|update”, „-|safe”), ale reguła LB20a jest w normie od Unicode 15.1 właśnie po to, by nie odrywać
   dywizu od opcji i przedrostków; pytanie 28 w `open-questions.md`.

## Konsekwencje

- `engine-fit`: słowa przesunięte o ponad 3 pt 511 → 0 (191 → 0 na innej stronie), `overview` 1545 → 10
  (36 → 0), `progress` 422 → 27, `landscape` 544 → 177 (→ 4 razem z poprawką przepełnienia o ułamek piksela,
  `docs/feature-audit.md`, 2026-11-22); faktury i e-SF bez zmian.
- Tekst „mot !” albo „« mot »” z wpisaną zwykłą spacją może się złamać przed znakiem przestankowym, tak jak
  w Chromium; kto chce temu zapobiec, wpisuje spację nierozdzielającą.
- Sondaż CSS: trzy testy `hanging-punctuation-last*` przestają przechodzić. Zaliczały się przypadkiem
  (nawias po spacji zostawał w wierszu przez LB13, jak gdyby wisiał); `hanging-punctuation` nie jest
  obsługiwane, a Chromium, który go też nie obsługuje, tych testów nie zalicza.
