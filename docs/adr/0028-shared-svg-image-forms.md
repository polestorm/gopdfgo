# ADR 0028: Obraz SVG powtórzony w dokumencie zapisany w PDF raz

Status: przyjęta 2026-11-21.

## Kontekst

Obraz rastrowy użyty wiele razy jest w PDF jednym obiektem XObject (pamięć podręczna obrazów w
`goweasyprint/pdf`, klucz `RasterImage.ID`), a każde wystąpienie to operator `Do`. Obraz SVG był rysowany
od nowa przy każdym wystąpieniu: jego ścieżki, gradienty i tekst trafiały do treści każdej strony.
Pomiar 2026-11-21 (`gopdfgo` z `cmd/gopdfgo`, macOS arm64): herb `testdata/svg/commons-Herb_Polski.svg`
(100 kB) jako `<img width=40>` na każdej z 41 stron daje 1,54 MB PDF (jedna strona: 43 kB), ten sam PNG
na 41 stronach 130 kB. Typowe przypadki: logo w polach marginesów (`@top-left`, `position: running`),
ikony w wierszach tabeli, tło SVG.

## Decyzja

1. Opcjonalne rozszerzenie płótna `backend.SharedDrawer` z jedną metodą
   `DrawShared(key, width, height, draw)`: przy pierwszym wywołaniu z danym kluczem `draw` maluje treść
   na nowym płótnie o prostokącie (0, 0, width, height), kolejne wywołania z tym kluczem tylko wskazują
   zapamiętaną treść.
2. W PDF (`goweasyprint/pdf/filters.go`) treść jest zwykłą formą XObject, bez grupy przezroczystości,
   więc wynik jest taki sam jak przy rysowaniu wprost (ISO 32000-1 § 8.10): forma dziedziczy stan
   graficzny z miejsca użycia, a `Do` przywraca go po narysowaniu. Formy leżą we wspólnej pamięci
   dokumentu (`cache.forms`), więc ta sama forma służy wszystkim stronom, polom marginesów dorysowanym
   później (ADR 0027), kafelkom wzorców tła i maskom. Klucz w PDF obejmuje też granicę rozmiaru form
   masek miękkich bieżącej strony (`maskLimit`), od której zależy treść.
3. `images.SVGImage.Draw` używa tego rozszerzenia dla obrazów SVG z pliku: `img`, `object`, `embed`,
   `background-image`, `border-image`, `content: url()`. Klucz: obiekt obrazu (wspólny dla wszystkich
   wystąpień tego samego adresu, `images.Cache`), tryb dokumentu (`object`/`embed`) i rozmiar konkretny.
   Obraz SVG jest dokumentem odrębnym od strony: nie dziedziczy jej kaskady ani `currentColor`
   (SVG Integration § 2.2), a `image-rendering` nie dotyczy grafiki wektorowej; układ gospodarza
   (`hostScale`, `vector-effect: non-scaling-stroke`) jest względny wobec miejsca rysowania, więc jest
   ten sam na stronie i w formie.
4. Forma jest przycinana do obszaru widoku obrazu (`/BBox`), jak w przeglądarkach, które malują obraz
   SVG w granicach jego obszaru widoku. Wcześniej treść wystająca poza obszar widoku elementu głównego
   była widoczna; w korpusie i w testach W3C nie ma takiego przypadku (piksele bez zmian). Przejęty
   test `TestImageOverflow` (`goweasyprint/pdf/draw_image_test.go`) oczekiwał treści wystającej aż do
   krawędzi strony; wzorzec poprawiony na obraz przycięty do 4×4.
5. Nie są współdzielone: `<svg>` osadzony w HTML (dziedziczy kaskadę i kontekst strony) oraz obraz SVG
   wstawiony elementem `<image>` wewnątrz innego SVG (rysowany w układzie gospodarza zewnętrznego
   dokumentu, zob. `svg.svgImageHolder`). Klucz nie opiera się na adresach w postaci liczbowej: mapa
   trzyma wskaźnik do obrazu (więc obraz żyje do końca zapisu) i służy tylko do wyszukiwania, nigdy nie
   jest przeglądana; kolejność obiektów w pliku zależy wyłącznie od kolejności rysowania.

## Skutki

- Pomiar 2026-11-21 (jak wyżej, trzy przebiegi): herb na 41 stronach 1 544 078 B → 67 767 B,
  0,19 s → 0,04 s; dokument A5 z herbem w `@top-left`, w elemencie biegnącym, w tle pola marginesu,
  oraz 120 wierszy tabeli z `<img>`, tłem kafelkowanym i `<object>` (7 stron): 14,0 MB → 297 kB,
  1,47 s → 0,11 s. Rasteryzacja (pdftoppm, 96 dpi) bez zmian.
- Obraz użyty raz kosztuje dodatkowy obiekt formy (kilkaset bajtów); mała ikona narysowana w dwóch
  rozmiarach to dwie formy, więc plik z kilkoma małymi ikonami bywa większy o 1–3 kB. Plik z dużym
  obrazem narysowanym raz bywa mniejszy (osobny strumień kompresuje się lepiej): flaga Meksyku
  280 kB → 188 kB.
- `images.SVGSharingDisabled` wyłącza współdzielenie (testy porównujące z rysowaniem osobno,
  `tests/svgshare18_test.go`).
