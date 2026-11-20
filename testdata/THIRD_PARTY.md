# Rejestr pochodzenia plików obcych

Stan na 2026-11-12. Wszystko poza `documents/` (własne) pochodzi z poniższych źródeł, bez zmian w treści plików.

| Katalog | Źródło | Commit / wersja | Licencja | Uwagi |
|---|---|---|---|---|
| `wpt/css/`, `wpt/fonts/`, `wpt/images/`, `wpt/common/` | https://github.com/web-platform-tests/wpt | `ab97c0179bd24410d26704863acc83e85c05e432` (2026-11-12) | BSD-3-Clause, tekst w `wpt/LICENSE.md` | wybór skryptem `wpt/select.py`; nagłówki autorów w plikach zachowane |
| `wpt/fonts/Ahem.ttf` | jw. | jw. | domena publiczna, a gdzie prawo jej nie uznaje CC0 1.0 (wg tabeli `name` w pliku fontu, wersja 1.50) | font testowy ładowany przez `@font-face`, tylko do testów i pomiarów (ADR 0003) |
| `samples/weasyprint/{invoice,report,letter,ticket}` | https://github.com/CourtBouillon/weasyprint-samples | `6dc675b` (2026-03-24) | domena publiczna wg `README.upstream.rst` („Everything else from the samples is in the public domain”); fonty na własnych licencjach | pominięto `book` (tekst własnością Madcats), `poster`, `report/thumbnail.png` |
| fonty w `samples/weasyprint/` | dołączone do próbek | | Source Sans 3, Pacifico, Fira Sans, Barlow Condensed, Libre Barcode 128: OFL; Coming Soon: Apache-2.0 (wg katalogu Google Fonts, z pamięci; pliki licencji nie są dołączone do próbek) | do potwierdzenia przed publikacją repozytorium |
| `fonts/csstest/*.ttf` | https://github.com/web-platform-tests/wpt (`fonts/CSSTest/`) | `ab97c0179bd24410d26704863acc83e85c05e432` (2026-11-12), skopiowane 2026-11-18 | SIL OFL 1.1 (przeróbka Gentium Basic, SIL International), tekst w `fonts/csstest/OFL.txt` | tylko do testów i pomiarów (ADR 0018) |
| `fonts/Noto*.ttf` | https://github.com/notofonts/notofonts.github.io (`fonts/*/unhinted/ttf/`) | pobrane 2026-11-12 z gałęzi `main` | OFL-1.1, tekst w `fonts/OFL.txt` | |
| `svg/bootstrap-*.svg` | https://github.com/twbs/icons (`icons/`) | `829d2bc` (2026-10-29), pobrane 2026-11-20 | MIT, tekst w `svg/licenses/bootstrap-icons-LICENSE.txt` | korpus SVG (porównanie z Chromium) |
| `svg/material-*.svg` | https://github.com/google/material-design-icons (`src/*/*/materialicons/24px.svg`) | `10ecc14` (2026-10-23), pobrane 2026-11-20 | Apache-2.0, tekst w `svg/licenses/material-design-icons-LICENSE.txt` | jw. |
| `svg/simpleicons-*.svg` | https://github.com/simple-icons/simple-icons (`icons/`) | `32e0300` (2026-11-19), pobrane 2026-11-20 | CC0 1.0, tekst w `svg/licenses/simple-icons-LICENSE.md`; znaki towarowe należą do właścicieli marek | jw.; tylko do testów |
| `svg/commons-*.svg` | Wikimedia Commons (`Special:FilePath/<nazwa>.svg`) | pobrane 2026-11-20 | domena publiczna (licencja z API Commons, pole `LicenseShortName`: „Public domain”) | jw.; pliki bez `viewBox`, z Inkscape |
| `svg/own-*.svg` | własne (wzorowane na wynikach D3, draw.io, Inkscape) | | jak projekt | jw. |
| `books/pride-and-prejudice/` (poza repozytorium, pobierane przez `books/fetch.sh`) | Project Gutenberg, książka nr 1342 (wydanie HTML z ilustracjami Hugh Thomsona), lustro https://gutenberg.pglaf.org/1/3/4/1342/ | pobrane 2026-11-20; sumy SHA-256 w `books/pride-and-prejudice.sha256` | domena publiczna (tekst z 1813, ilustracje z 1894); znak Project Gutenberg i warunki w `LICENSE.txt` dołączonym do plików | pomiar dużego dokumentu w `docs/overview.md`; 0,8 MB HTML, 164 obrazy, 27 MB |
| `fonts/tinos/Tinos-*.ttf` | https://github.com/google/fonts (`ofl/tinos/`), źródło fontu: https://github.com/googlefonts/tinos | commit `ba95515f1333efe9342c2ad988b9c2f6bef6dbad` (2026-04-27), wersja 1.340; pobrane 2026-11-15 | OFL-1.1, tekst w `fonts/tinos/OFL.txt` (z repozytorium źródłowego, gałąź `main`) | metryki Times New Roman; wersje do 1.23 były na Apache-2.0, od 1.340 OFL. Font dla rodziny `serif` w testach i pomiarach, przypisany w `fonts/families.json` (ADR 0005) |
| `fonts/arimo/Arimo-*.ttf` | https://github.com/googlefonts/Arimo (`fonts/ttf/`) | commit `22e482e2ce0ebb2e6f88ba49d029d60f558d5d67` (2026-11-05), odmiany statyczne; pobrane 2026-11-15 | OFL-1.1, tekst w `fonts/arimo/OFL.txt` (z tego samego commita) | metryki Arial. Font dla rodziny `sans-serif` w testach i pomiarach, przypisany w `fonts/families.json` |
| `regression/endless-line-slicing.html` | https://github.com/web-platform-tests/wpt (`css/css-grid/grid-lanes/item-placement/grid-lanes-item-placement-006-ref.html`) | `ab97c0179bd24410d26704863acc83e85c05e432` | domena publiczna (CC0, nagłówek pliku) | przypadek regresji: wiersz o nieskończonej wysokości |

`fonts/coverage.txt` i `fonts/families.json` są własne: pierwszy generuje `go run ./tools/fontcoverage testdata/fonts testdata/wpt/fonts/Ahem.ttf`,
drugi przypisuje fonty rodzinom generycznym (ADR 0005).

Nie skopiowano (brak licencji albo ograniczenia): `zopyx/print-css-rocks`, próbka `book` z weasyprint-samples.
