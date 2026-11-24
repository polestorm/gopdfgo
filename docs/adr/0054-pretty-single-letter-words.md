# ADR 0054: `text-wrap: pretty` i jednoliterowe wyrazy na końcu wiersza

Status: przyjęta 2026-11-24 (decyzja użytkownika).

## Kontekst

Polska, czeska i słowacka typografia nie zostawia jednoliterowych spójników i przyimków („w”, „z”, „i”,
„a”, „o”, „u”; po czesku i słowacku także „k”, „s”, „v”) na końcu wiersza (sierotki, zawieszki). CSS nie ma na
to osobnej właściwości, UAX #14 pozwala łamać po każdej spacji, a Chromium tak łamie: przykład w README
(dokument korpusu 22) miał na pierwszej stronie 5 takich wierszy, w tych samych miejscach co wzorzec Chromium.
To nie są sieroty i wdowy z CSS (`orphans`, `widows` dotyczą wierszy przy podziale strony).

CSS Text 4 § 5.2 (`text-wrap-style: pretty`) pozwala przeglądarce wybierać miejsca łamania według własnych
reguł typograficznych, kosztem szybkości. Walidacja tę wartość już przyjmowała, bez działania.

## Decyzja

1. Przy `text-wrap-style: pretty` (także skrót `text-wrap: pretty`) w języku `pl`, `cs` albo `sk` (podstawowy
   znacznik `lang`) spacja po jednoliterowym wyrazie z listy języka nie jest miejscem łamania: wyraz przechodzi
   do następnego wiersza razem z wyrazem za nim. Wyraz jednoliterowy to litera na początku tekstu albo po białym
   znaku lub znaku otwierającym (nawias, cudzysłów, myślnik).
2. Ta sama reguła obowiązuje we wszystkich miejscach, gdzie silnik szuka podziału w tekście (`LineWrap`,
   `getNextBreakPoint`) i na granicy pudełek (tekst kończący się takim wyrazem i spacją przed elementem
   liniowym). Bez zgodności obu dróg układ wiersza ciął tekst w innym miejscu niż zawijanie i gubił resztę.
3. Domyślnie (`auto`) i w innych językach łamanie bez zmian, jak w UAX #14 i Chromium.

## Konsekwencje

- Dokument korpusu 22 z `html { text-wrap: pretty }`: 17 wierszy zakończonych jednoliterowym wyrazem → 0, te
  same strony i ten sam tekst. Przykład w README pokazuje ten wariant.
- Ograniczenie: wyraz jednoliterowy będący w całości osobnym elementem (`<b>w</b> archiwum`) nadal może zostać
  na końcu wiersza; obejście: `&nbsp;` w źródle.
- Testy: `tests/pretty24_test.go`.
