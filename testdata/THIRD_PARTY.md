# Rejestr pochodzenia plików obcych

Stan na 2026-11-12. Wszystko poza `documents/` (własne) pochodzi z poniższych źródeł, bez zmian w treści plików.

| Katalog | Źródło | Commit / wersja | Licencja | Uwagi |
|---|---|---|---|---|
| `wpt/css/`, `wpt/fonts/`, `wpt/images/`, `wpt/common/` | https://github.com/web-platform-tests/wpt | `ab97c0179bd24410d26704863acc83e85c05e432` (2026-11-12) | BSD-3-Clause, tekst w `wpt/LICENSE.md` | wybór skryptem `wpt/select.py`; nagłówki autorów w plikach zachowane |
| `wpt/fonts/Ahem.ttf` | jw. | jw. | `fonts/README.md` w WPT nie podaje osobnej licencji fontu; przyjmujemy licencję repozytorium (BSD-3-Clause), do potwierdzenia przed publikacją | font testowy, glify-kwadraty |
| `samples/weasyprint/{invoice,report,letter,ticket}` | https://github.com/CourtBouillon/weasyprint-samples | `6dc675b` (2026-03-24) | domena publiczna wg `README.upstream.rst` („Everything else from the samples is in the public domain”); fonty na własnych licencjach | pominięto `book` (tekst własnością Madcats), `poster`, `report/thumbnail.png` |
| fonty w `samples/weasyprint/` | dołączone do próbek | | Source Sans 3, Pacifico, Fira Sans, Barlow Condensed, Libre Barcode 128: OFL; Coming Soon: Apache-2.0 (wg katalogu Google Fonts, z pamięci; pliki licencji nie są dołączone do próbek) | do potwierdzenia przed publikacją repozytorium |
| `fonts/Noto*.ttf` | https://github.com/notofonts/notofonts.github.io (`fonts/*/unhinted/ttf/`) | pobrane 2026-11-12 z gałęzi `main` | OFL-1.1, tekst w `fonts/OFL.txt` | |

Nie skopiowano (brak licencji albo ograniczenia): `zopyx/print-css-rocks`, próbka `book` z weasyprint-samples.
