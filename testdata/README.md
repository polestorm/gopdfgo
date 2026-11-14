# Korpus testowy

Stan na 2026-11-12. Trzy warstwy: dokumenty całościowe (własne i obce) oraz testy zgodności z web-platform-tests.
Pochodzenie i licencje obcych plików: `THIRD_PARTY.md`.

| Katalog | Zawartość | Liczba | Wzorzec |
|---|---|---|---|
| `documents/` | dokumenty syntetyczne pisane pod pozycje „musi” ze `docs/scope.md` | 19 (17 „musi”, 2 „powinien”) | opis w `*.expect.json`, PDF z Chromium w `reference/chromium/` |
| `samples/weasyprint/` | próbki WeasyPrint: faktura, raport, list, bilet | 4 | PDF z WeasyPrint dołączony przez autorów |
| `wpt/` | testy druku z web-platform-tests, bez JavaScriptu | 340 reftestów + 115 ręcznych | plik wzorcowy HTML (reftest) albo opis w treści testu |
| `wpt/manifest.tsv`, warstwa `layout` | reftesty ekranowe CSS 2.1, tabele, listy, flex, fragmentacja w kolumnach | 8242, tylko lista | pliki w klonie WPT o commicie z `wpt/UPSTREAM` |
| `fonts/` | Noto Sans (4 odmiany), Noto Sans Mono, Noto Serif | 6 | używane przez `documents/` |

Rozmiar: 15 MB, 833 pliki (pomiar `du -sh`, `find -type f`, 2026-11-12).

## `documents/`

Każdy dokument to jeden samowystarczalny plik HTML (CSS w `<style>`, obrazy jako `data:`); jedyne zasoby zewnętrzne to
fonty z `../fonts/` przez `@font-face`. Pliki HTML są źródłem prawdy i można je edytować ręcznie.

`NN-nazwa.expect.json`:

- `level`: `must` albo `should` (poziom ze `scope.md`); pojedyncze przypadki „powinien” w dokumencie „musi” są
  oznaczone w treści dokumentu i w `checks`;
- `covers`: numery wymagań z `docs/requirements.md` i pozycje zakresu;
- `pages`: liczba stron wynikająca z konstrukcji dokumentu (wymuszone podziały); `null`, gdy zależy od metryk tekstu;
- `checks`: normatywna lista tego, co ma być widać; to ona jest wzorcem, nie Chromium;
- `chromium`: pomiar z Chrome 157.0.8103.51 (macOS arm64, `--headless --print-to-pdf --no-pdf-header-footer`):
  liczba stron, pierwszy wiersz treści każdej strony, znane odchylenia Chromium od specyfikacji.

Chromium nie jest wzorcem tam, gdzie nie obsługuje funkcji: `string-set`, `running()`, `target-counter()`,
`bookmark-*`, `break-before: right` z pustą stroną. Te przypadki są wypisane w `chromium.deviations`.

Dokumenty 14 (selektory i kaskada) i 15 (podziały stron) są samosprawdzające: w 14 żaden pasek nie może być
czerwony, w 15 etykieta „SN” musi leżeć na stronie N.

Odtworzenie wzorców (PNG powstaje z PDF, więc w repozytorium trzymamy tylko PDF):

```sh
chrome --headless --disable-gpu --no-pdf-header-footer --print-to-pdf=out.pdf file://$PWD/documents/01-record-copy.html
pdftoppm -r 96 -png out.pdf page
```

## `wpt/`

Struktura katalogów jak w korzeniu WPT (`css/…`, `fonts/…`, `images/…`), więc ścieżki bezwzględne w testach
(`/fonts/ahem.css`) działają, gdy korzeniem jest `testdata/wpt/`. `manifest.tsv` ma kolumny:

- `tier`: `paged` (reftest druku, skopiowany), `paged-manual` (test z opisem oczekiwań, skopiowany), `layout`
  (reftest ekranowy, niekopiowany);
- `topic`: pozycja zakresu (`margin-boxes`, `named-pages`, `table-fragmentation`, …) albo rozdział CSS 2.1;
- `test`, `kind` (`match`, `mismatch`, `manual`), `references`;
- `flags`: `ahem` (wymaga fontu Ahem), `fuzzy` (test dopuszcza różnice pikseli), cechy spoza zakresu
  (`grid`, `vertical-writing`, `iframe`, `canvas`, …).

Reftest przechodzi, gdy test i wzorzec wyrenderowane tym samym silnikiem dają identyczne strony. Reftesty druku
zakładają stronę 5 × 3 cale z marginesem 0,5 cala, o ile test nie ustawia `@page`
(https://web-platform-tests.org/writing-tests/print-reftests.html).

Wybór odtwarza `select.py`; pomija testy ze skryptem (516) i z zależnością spoza pobranych katalogów (34):

```sh
git clone --depth 1 --filter=blob:none --sparse https://github.com/web-platform-tests/wpt.git
git -C wpt sparse-checkout set --no-cone /LICENSE.md /fonts/Ahem.ttf /fonts/ahem.css /images/green.png \
  /common/blank.html /css/css-page/ /css/css-break/ /css/css-gcpm/ /css/CSS2/ /css/support/ /css/reference/ \
  /css/fonts/ /css/css-tables/ /css/css-lists/ /css/css-content/ /css/css-flexbox/
python3 -I testdata/wpt/select.py wpt testdata/wpt
```

Warstwa `layout` nie jest kopiowana (ok. 60 MB); benchmark czyta ją z klonu o commicie zapisanym w `wpt/UPSTREAM`.

## Czego w korpusie nie ma

- print-css.rocks (lekcje PrintCSS): repozytorium nie ma pliku licencji, więc nie kopiujemy; można uruchamiać z klonu.
- Wizualizacji faktur XRechnung (KoSIT, Apache-2.0): arkusz HTML dołącza JavaScript, a transformacja wymaga
  procesora XSLT 2.0; do rozważenia dla xml.pro.
- Próbki „book” z WeasyPrint: tekst jest własnością Madcats.
- PDF/A, podpisów, zakładek jako testów automatycznych: sprawdzane narzędziami (veraPDF) w etapie 2.
