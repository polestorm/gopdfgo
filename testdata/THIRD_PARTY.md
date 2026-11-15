# Rejestr pochodzenia plików obcych

Stan na 2026-11-12. Wszystko poza `documents/` (własne) pochodzi z poniższych źródeł, bez zmian w treści plików.

| Katalog | Źródło | Commit / wersja | Licencja | Uwagi |
|---|---|---|---|---|
| `wpt/css/`, `wpt/fonts/`, `wpt/images/`, `wpt/common/` | https://github.com/web-platform-tests/wpt | `ab97c0179bd24410d26704863acc83e85c05e432` (2026-11-12) | BSD-3-Clause, tekst w `wpt/LICENSE.md` | wybór skryptem `wpt/select.py`; nagłówki autorów w plikach zachowane |
| `wpt/fonts/Ahem.ttf` | jw. | jw. | domena publiczna, a gdzie prawo jej nie uznaje CC0 1.0 (wg tabeli `name` w pliku fontu, wersja 1.50) | font testowy ładowany przez `@font-face`, tylko do testów i pomiarów (ADR 0003) |
| `samples/weasyprint/{invoice,report,letter,ticket}` | https://github.com/CourtBouillon/weasyprint-samples | `6dc675b` (2026-03-24) | domena publiczna wg `README.upstream.rst` („Everything else from the samples is in the public domain”); fonty na własnych licencjach | pominięto `book` (tekst własnością Madcats), `poster`, `report/thumbnail.png` |
| fonty w `samples/weasyprint/` | dołączone do próbek | | Source Sans 3, Pacifico, Fira Sans, Barlow Condensed, Libre Barcode 128: OFL; Coming Soon: Apache-2.0 (wg katalogu Google Fonts, z pamięci; pliki licencji nie są dołączone do próbek) | do potwierdzenia przed publikacją repozytorium |
| `fonts/Noto*.ttf` | https://github.com/notofonts/notofonts.github.io (`fonts/*/unhinted/ttf/`) | pobrane 2026-11-12 z gałęzi `main` | OFL-1.1, tekst w `fonts/OFL.txt` | |
| `fonts/tinos/Tinos-*.ttf` | https://github.com/google/fonts (`ofl/tinos/`), źródło fontu: https://github.com/googlefonts/tinos | commit `ba95515f1333efe9342c2ad988b9c2f6bef6dbad` (2026-04-27), wersja 1.340; pobrane 2026-11-15 | OFL-1.1, tekst w `fonts/tinos/OFL.txt` (z repozytorium źródłowego, gałąź `main`) | metryki Times New Roman; wersje do 1.23 były na Apache-2.0, od 1.340 OFL. Font dla rodziny `serif` w testach i pomiarach, przypisany w `fonts/families.json` (ADR 0005) |
| `fonts/arimo/Arimo-*.ttf` | https://github.com/googlefonts/Arimo (`fonts/ttf/`) | commit `22e482e2ce0ebb2e6f88ba49d029d60f558d5d67` (2026-11-05), odmiany statyczne; pobrane 2026-11-15 | OFL-1.1, tekst w `fonts/arimo/OFL.txt` (z tego samego commita) | metryki Arial. Font dla rodziny `sans-serif` w testach i pomiarach, przypisany w `fonts/families.json` |
| `regression/endless-line-slicing.html` | https://github.com/web-platform-tests/wpt (`css/css-grid/grid-lanes/item-placement/grid-lanes-item-placement-006-ref.html`) | `ab97c0179bd24410d26704863acc83e85c05e432` | domena publiczna (CC0, nagłówek pliku) | przypadek regresji: wiersz o nieskończonej wysokości |

`fonts/coverage.txt` i `fonts/families.json` są własne: pierwszy generuje `go run ./tools/fontcoverage testdata/fonts testdata/wpt/fonts/Ahem.ttf`,
drugi przypisuje fonty rodzinom generycznym (ADR 0005).

Nie skopiowano (brak licencji albo ograniczenia): `zopyx/print-css-rocks`, próbka `book` z weasyprint-samples.
