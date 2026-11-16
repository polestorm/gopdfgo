# gopdfgo

Silnik dokumentowy w czystym Go: statyczny HTML + CSS + SVG → lista wyświetlania → PDF (oraz SVG i PNG na stronę).
Bez JavaScriptu, bez Chromium, bez cgo. Silnik ogólnego użytku dla typowych dokumentów (faktury, dokumenty tekstowe
z obrazami, raporty z tabelami, grafika wektorowa), nie dla „dowolnej strony z internetu”. Obsługuje CSS Paged Media: rozmiar strony, pola marginesowe
z numerem strony, nagłówki bieżące, łamanie stron z `orphans`/`widows`.

Stan na 2026-11-14: sondaż 9135 reftestów ekranowych W3C: 6468 zaliczonych; reftesty druku: 183 z 340; binarka 21,5 MB (`docs/feature-audit.md`). Wcześniej: **działa `gopdfgo.Render` (HTML → PDF) na forku webrender w `internal/` (ADR 0002); korpus testowy pilnuje liczby stron, wyszukiwalności tekstu, determinizmu i braku panik. Brakuje m.in. `calc()`, PDF/A, listy wyświetlania i wyjść SVG/PNG.** Rozpoznanie wykazało, że w Go istnieją już trzy
silniki układu HTML/CSS (port WeasyPrint, Folio, go-webengine), więc pierwsza decyzja brzmi: pisać własny, adoptować,
czy sforkować. Szczegóły w `docs/landscape.md` i `docs/adr/open-questions.md`.

| Plik | Zawartość | Czytaj, gdy |
|---|---|---|
| `docs/progress.md` | Postęp w liczbach i wykresach: wyniki testów W3C w czasie, poprawki według obszarów, co zostało i jak ważne | pytasz „gdzie jesteśmy” |
| `docs/vision.md` | Cztery cele projektu, co poświęcamy na rzecz rozmiaru i przewidywalności, czym silnik nie jest | chcesz zrozumieć kierunek |
| `docs/requirements.md` | Wymagania wobec renderera: wejście, układ, wyjście, działanie | oceniasz silnik albo projektujesz interfejs |
| `docs/scope.md` | Zakres HTML, CSS, Paged Media i SVG: musi, powinien, nie będzie | pytasz „czy to obsłużymy” |
| `docs/architecture.md` | Potok, lista wyświetlania, backendy, interfejs, determinizm | zaczynasz kodować albo piszesz adapter |
| `docs/landscape.md` | Przegląd istniejących silników (przeglądarki, dokumentowe, Go) z pomiarami | wybierasz strategię |
| `docs/building-blocks.md` | Inwentarz bibliotek Go do użycia, gdyby silnik powstawał od zera | planujesz własną implementację |
| `docs/estimates.md` | Rozmiar binarki, pamięć, pracochłonność dla trzech strategii; ocena wariantu „tylko PDF” | szacujesz koszt |
| `docs/engine-fit.md` | Które pozycje „musi” obsługuje każdy z trzech silników Go (ocena z czytania kodu) | wybierasz strategię po korpusie |
| `testdata/README.md` | Korpus: 21 dokumentów własnych, próbki WeasyPrint, testy druku z WPT; format oczekiwań | uruchamiasz albo rozszerzasz testy |
| `docs/benchmark.md` | Wyniki forka webrender na korpusie: dokumenty, próbki, reftesty WPT, wady warstwy PDF | oceniasz, co trzeba naprawić |
| `docs/feature-audit.md` | Stan funkcji: rozmiar w binarce, sondaż 9135 reftestów, paniki, ocena grid; poprawić, zostawić czy wyłączyć | decydujesz, czym zająć się dalej |
| `cmd/gopdfgo` | Polecenie: plik HTML → PDF | chcesz wyrenderować dokument z wiersza poleceń |
| `docs/roadmap.md` | Etapy: korpus testowy, benchmark, decyzja, wdrożenie | planujesz pracę |
| `tools/maprange` | Skaner pętli po mapach (źródło niedeterminizmu) | szukasz, czemu dwa przebiegi dają różne bajty |
| `THIRD_PARTY.md` | Skąd pochodzi kod w `internal/`, co w nim zmieniono, stan testów po przejęciu | dotykasz przejętego kodu |
| `docs/adr/` | Rejestr decyzji (0001 lista wyświetlania, 0002 fork webrender + folio, 0003 porównanie obrazów w reftestach, 0004 blok o stałej wysokości na granicy stron) i pytania otwarte | zastanawiasz się „dlaczego tak” |

## Najkrótsze streszczenie

1. **Po co**: zastąpić Chromium (400 MB, osobny proces, wynik niedeterministyczny) silnikiem co najmniej 10 razy
   mniejszym, nad którego kodem mamy pełną kontrolę i który renderuje deterministycznie. Pokrycie w wybranych ramach,
   bez konkurowania z przeglądarką (`docs/vision.md`).
2. **Co**: silnik dokumentowy klasy WeasyPrint/Prince, nie przeglądarka. Wejście to samowystarczalny HTML z naszych
   szablonów. Lista wyświetlania oddziela układ od formatu wyjścia, więc PDF, SVG i PNG powstają z jednego układu.
3. **Czy Go**: tak. Parser HTML5, selektory CSS, kształtowanie tekstu, dzielenie wyrazów, flex, zapis PDF z podzbiorami
   fontów istnieją w czystym Go. Od zera trzeba by napisać układ i fragmentację (ok. 40–60 tys. linii, 2–4 osobolata).
4. **Ale**: od zera pisać nie trzeba. Folio (Apache-2.0, 100 tys. linii, binarka 9 MB) i port WeasyPrint (BSD-3,
   52 tys. linii, binarka 30 MB) już to robią w różnym zakresie. Korpus jest gotowy; ocena statyczna wskazuje fork
   portu WeasyPrint jako układ z wymiennym backendem PDF. Etap 2 to próba forka i benchmark, etap 3 to decyzja.
5. **Rozmiar**: zmierzone 9 MB (Folio) i 30 MB (port WeasyPrint) wobec 150–200 MB `chrome-headless-shell`
   i ok. 400 MB Chromium. Własny silnik: szacunkowo 20–25 MB.
