# ADR 0021: obszar strony zaokrąglany do całych pikseli jak w Chromium

Status: przyjęta 2026-11-19 (decyzja użytkownika; rozszerza listę zaokrągleń z ADR 0013).

## Kontekst

CSS Page 3 wyznacza obszar strony dokładnie: rozmiar strony minus marginesy. A4 z marginesami 15 mm ma
szerokość obszaru 680,31 px. Chrome 157 zaokrągla ten obszar do całych pikseli CSS. Pomiar z 2026-11-19
na własnych plikach:

- szerokość i wysokość obszaru treści rosną do najbliższej całości w górę (680,31 px daje 681 px);
- lewa krawędź obszaru jest zaokrąglana do najbliższego piksela, górna zostaje bez zmian.

Różnica jest ułamkowa, ale przesuwa łamanie wierszy, gdy słowo mieści się na granicy szerokości. Na tym
opierało się wiele różnic w korpusie: `19-flex-grid-pagination` miał 72 słowa przesunięte wobec Chromium.

Norma nie wymaga, żeby obszar treści miał wymiary ułamkowe. Mamy już przełącznik, który wybiera między
zaokrągleniami Chromium a wiernością arkuszowi (`ExactLengths`, ADR 0013).

## Decyzja

1. Gdy `ExactLengths` jest wyłączone (domyślnie), `makePage` (`internal/webrender/html/layout/pages.go`)
   zaokrągla szerokość i wysokość obszaru strony w górę do całych pikseli CSS, z tolerancją 0,001 px na błąd
   liczb zmiennoprzecinkowych. Lewy margines zaokrągla do najbliższego piksela. Prawy i dolny margines maleją
   o tyle, o ile urósł obszar, więc rozmiar strony się nie zmienia. Zaokrąglenie dotyczy obszaru treści:
   pola marginesów (`@top-right` itp.) układamy według dokładnych marginesów z arkusza
   (`exactPageGeometry`, `layout.go`). Dokładne wymiary obowiązują tylko na czas układu pól marginesów;
   potem strona wraca do zaokrąglonych. Próba z trwałym przywróceniem dokładnych wymiarów psuła korpus
   (`22-multicol-pagination`: 37 stron różnych), bo dalsze kroki czytają geometrię, na której ułożono treść.
2. `ExactLengths: true` zostawia obszar strony dokładnie taki, jak wynika z arkusza.
3. Zaokrąglenie dotyczy obszaru przed pomniejszaniem z ADR 0014; mnożnik skali działa na wartości już
   zaokrąglone.

## Skutki

- Korpus (2026-11-19, `corpuscmp.py` przeciw binarce sprzed zmiany): `19-flex-grid-pagination` 72 → 18 słów
  przesuniętych wobec Chromium; pozostałe dokumenty bez pogorszeń.
- Treść zaczyna się na lewej krawędzi zaokrąglonej do piksela i ma obszar o ułamek piksela większy niż
  w arkuszu. Pola marginesów mają wymiary dokładne; jak układa je Chrome, nie mierzyliśmy.
- Testy przejęte `TestMarginBoxesFixedDimension*` przechodzą bez zmian. `TestPageBreaksComplex1` porównuje
  prawy margines strony po zaokrągleniu do piksela (9,70 px zamiast 10 px na domyślnym A4).
- Sondaż W3C (tryb ciągły, 2026-11-19): 9498 z 10962, bez zysków i strat; reftesty druku bez zmian.
