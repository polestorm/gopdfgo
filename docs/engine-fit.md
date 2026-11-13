# Dopasowanie silników Go do zakresu (ocena statyczna)

Stan na 2026-11-12. **Metoda: czytanie kodu i dokumentacji, bez uruchamiania silników** (zasada z `CLAUDE.md`).
Płytkie klony: `carlos7ags/folio` `84e0913` (2026-10-29), `benoitkugler/webrender` `6e769b1` i `go-weasyprint`
`e4c9509` (2026-10-01), `go-webengine/engine` `d19afd4` (2026-11-10), `go-pdfkit/html2pdf` `4e9e894` (2026-11-01).
„Tak” znaczy: jest kod, który to obsługuje; nie znaczy, że działa poprawnie. To rozstrzygnie benchmark na korpusie
z `testdata/`. Linie kodu: `*.go` bez testów / testy, `wc -l`.

## 1. Pozycje „musi” ze `scope.md`

| Pozycja | folio | webrender (port WeasyPrint) | go-webengine + html2pdf | Dokument korpusu |
|---|---|---|---|---|
| Model układu | własny model dokumentu (`Paragraph`, `Div`, `Table`); HTML jest na niego tłumaczony | drzewo pudełek CSS 2.1 jak w WeasyPrint | drzewo pudełek CSS, wzorcem Chrome | wszystkie |
| Zapadanie marginesów | tylko sąsiednie rodzeństwo (`collapseMargins`) | pełne (`block.go`, 142 trafienia) | jest | 12 |
| `float`, `position: absolute` | „pragmatyczny” model float; absolute jest | tak | tak | 12 |
| `table-layout`, `caption-side`, `empty-cells` | **brak** (nie ma w rejestrze 139 właściwości) | tak | nie znaleziono | 02, 17 |
| Powtarzanie `thead`/`tfoot` | tak | tak | nie znaleziono w `paginate/` | 01, 02 |
| `var()` | tak | tak | tak | 14 |
| `calc()` | tak, także `min/max/clamp` | brak w źródle; **dodane w forku 2026-11-12** dla długości (ograniczenia w `benchmark.md` pkt 5) | tak | 14 |
| `quotes`, `overflow-wrap`, `tab-size` | **brak** | tak | tak | 09, 10 |
| 6 pól marginesowych góra/dół | tak | tak | tak (w eksporterze, nie w silniku) | wszystkie |
| pozostałe 10 pól (boczne, narożne) | **brak** | tak | **brak** (README) | 06 |
| `counter(page)`, `counter(pages)` | tak | tak | tak | wszystkie |
| `string-set` / `string()` | tak | tak | **brak** | 01, 04 |
| `@page :first/:left/:right` | tak | tak | **brak** (README) | 04, 06 |
| `@page :blank` | **brak** (jawnie odrzucany) | tak | brak | 06 |
| Strony nazwane (`page: x`) | **brak** | tak | **brak** (README) | 04, 15 |
| `break-*: left/right` | **brak** | tak | nie sprawdzono | 06 |
| `orphans`, `widows`, `break-inside: avoid` | tak | tak | tak | 15 |
| Flex | tak | tak (`flex.go`, 1345 linii, 17 TODO) | tak | 08 |
| SVG `clipPath`, `symbol`, style z CSS | **brak** (`svg/doc.go`) | tak | raster przez oksvg, wektor osobno | 03 |
| `@font-face` z plików, podzbiory, `ToUnicode` | tak (własny stos fontów) | tak (go-text + `benoitkugler/pdf`) | tak | 16 |
| Fonty tylko wskazane, nie systemowe | font awaryjny szukany w ścieżkach systemu, chyba że podany `FallbackFontPath` | `fontscan.FontMap` można zbudować bez `UseSystemFonts` | własne fonty wbudowane | 16 |
| Determinizm | zasada projektu; `/ID` losowe tylko na żądanie, `SetFileID` | brak `time.Now` i `rand` w webrender i go-weasyprint; zależność `benoitkugler/pdf` nie sprawdzona | nie sprawdzono | wszystkie |
| Raport nieobsługiwanego CSS (Y8) | **brak**, ignoruje po cichu | globalny `logger.WarningLogger`, tekst bez pozycji w źródle | odrzuca deklarację, bez raportu | 11 |
| Licencja całości | Apache-2.0, zależności tylko `golang.org/x` | BSD-3; LGPL w dwóch plikach, patrz niżej | BSD-3; zależności m.in. goja, esbuild, wazero, `go-macos/metal` | |

## 2. Pozycje „powinien”

| Pozycja | folio | webrender | go-webengine + html2pdf |
|---|---|---|---|
| Dzielenie wyrazów po polsku | wzorce tylko en-US; `NewHyphenator(patterns)` istnieje, ale konwerter HTML używa domyślnego | 42 słowniki, w tym `hyph_pl_PL` | brak |
| `position: running()`, `target-counter()` | brak (zgłoszenie #222) | tak | `target-counter` tak, `running` brak |
| Zakładki PDF, linki | tak | tak | linki tak |
| PDF/A | tak (`document/pdfa.go`) | brak | brak |
| Tagged PDF | tak | brak | tak |
| `font-feature-settings` | brak | tak | brak |
| Grid, kolumny | tak | tak (grid: 37 TODO) | tak |

## 3. Co z tego wynika

1. **webrender ma komplet Paged Media i poprawny model CSS, folio ma komplet PDF.** Luki folio w „musi” dotyczą
   stronicowania (strony nazwane, `:blank`, 10 z 16 pól, podziały lewa/prawa) i tabel, a jego model układu nie jest
   drzewem pudełek CSS, więc tych luk nie zamyka się pojedynczymi łatkami. Luki webrender to `calc()` i warstwa
   wyjściowa (PDF/A, tagowanie), czyli rzeczy dopisywalne bez zmiany architektury.
2. **Blokada LGPL w webrender jest mniejsza, niż zakładało `landscape.md`.** Domyślny silnik tekstu to już
   go-text (`text/engine_gotext.go`, 971 linii); `textprocessing` importują tylko `text/engine_pango.go`,
   `text/draw/draw_pango.go` (razem 1215 linii) i narzędzia testowe. Ostatni commit: „prepare tests for pango
   removal”. W forku usunięcie tych plików powinno zdjąć LGPL z binarki; do potwierdzenia kompilacją i `go mod why`.
3. **go-webengine odpada jako podstawa.** Pola marginesowe i `target-counter` są dopisane w eksporterze przez
   drugi przebieg układu, bez `string-set` i stron nazwanych; zależności obejmują silnik JS, sieć i GPU; wymaga
   Go 1.27. Zostaje jako uczestnik benchmarku.
4. **Mieszanka ma naturalny szew.** `webrender/backend` to interfejs wyjścia (`Document`, `Page`, `Canvas`,
   ok. 45 metod: ścieżki, glify z identyfikatorami, obrazy, gradienty, grupy z przezroczystością, linki, zakładki).
   Nasza lista wyświetlania (ADR 0001) może go implementować jako rejestrator, a backend PDF czytać listę.
   Backendem na start jest istniejący `go-weasyprint/pdf` (1461 linii na `benoitkugler/pdf`, MIT); folio jako
   backend (PDF/A, tagowanie, podpisy) wymaga sprawdzenia, czy jego pakiety `document`/`content`/`font` przyjmą
   glify po identyfikatorach z cudzego kształtowania.

## 4. Próba forka (2026-11-12)

Wynik w ADR 0002: fork bez dwóch plików Pango kompiluje się, LGPL znika z listy zależności, binarka 26,8 MB.
Pakiety PDF folio mają interfejs po identyfikatorach glifów (`EncodeGIDs`, `Subset`), więc folio jako backend PDF
pod interfejsem `webrender/backend` jest wykonalne na poziomie interfejsów; nie zostało jeszcze napisane.

## 5. Czego ta ocena nie mówi

- Jakości implementacji: ile z 340 reftestów druku i 18 dokumentów faktycznie przechodzi.
- Czasu, pamięci, rozmiaru binarki po usunięciu Pango, determinizmu bajtów.
- Zgodności wyglądu z Chromium tam, gdzie specyfikacja zostawia swobodę.

To zakres etapu 2. Wymaga uruchomienia obcego kodu, czego `CLAUDE.md` dziś zabrania; patrz `adr/open-questions.md`
pytanie 11.
