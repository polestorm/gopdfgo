# Postęp: co naprawiono, co zostało, co jest ważne

Stan na 2026-11-16 po drugiej paczce kolumn (podział między strony, wysokości). Dokument aktualizujemy po każdej paczce poprawek.
Szczegóły każdej rundy są w `docs/feature-audit.md`, lista zmian w przejętym kodzie w `THIRD_PARTY.md`.

Sposób pomiaru: sondaż ekranowy to reftesty W3C (WPT) z siedmiu obszarów, renderowane bez pomniejszania
treści (`-max-shrink 1`, od 16.11), rasteryzowane w 96 dpi
i porównywane według ADR 0003 (zaliczenie ścisłe albo z dokładnością do krawędzi). Reftesty druku to
340 testów stronicowania trzymanych w `testdata/wpt`. Korpus to 22 własne dokumenty
w `testdata/documents` porównywanych z wydrukami Chromium.

## Stan w liczbach

| Miara | Wynik | Uwagi |
|---|---|---|
| Sondaż ekranowy W3C | **7209 z 9110 (79,1%)** | ściśle 6815, krawędziowo 394 |
| Sondaż rozszerzony (od 16.11): tekst, tła, fonty | **1095 z 1768 (61,9%)** | ściśle 1057, krawędziowo 38; `css-text` 493 z 887, `css-backgrounds` 434 z 600, `css-fonts` 168 z 281; pierwszy pomiar 970 |
| Reftesty druku W3C | **243 z 340 (71,5%)** | wszystkie ściśle |
| Korpus: liczba stron jak w Chromium | 21 z 22 dokumentów | wyjątek: `06-margin-boxes` |
| Testy własne | 107 funkcji testowych i 2 pomiary, wszystkie przechodzą | w tym 77 w `regression_test.go` |
| Faktura KSeF z xml.pro, 4 warianty (do 140 pozycji, 7 stron) | strony i wszystkie słowa jak w Chromium (do 1 pt) | `docs/feature-audit.md`, 2026-11-16 |
| Testy przejętego kodu | 6 stałych niezaliczeń | te same od 2026-11-13 |
| Czas renderowania (korpus) | 32–101 ms na dokument, tabela 4000 wierszy 1,10 s | `docs/benchmark.md` § 9, 2026-11-16 |
| PDF/A-2b (`Options.Conformance`, ADR 0016) | 22 z 22 dokumentów korpusu zgodne według veraPDF 1.30, bez ostrzeżeń | 2026-11-16 |
| Rozmiar binarki | 23,35 MB (macOS arm64, `-s -w`) | 17 razy mniej niż Chromium z bibliotekami |
| Commity | 105 w 4 dni | 13.11: 11, 14.11: 66, 15.11: 17, 16.11: 11 |
| Zmiany w przejętym kodzie | 267 wpisów w `THIRD_PARTY.md` | każda z odwołaniem do normy |
| Decyzje (ADR) | 14 | brak otwartych pytań |

## Sondaż ekranowy w czasie

Jeden znak `█` to 2 punkty procentowe zaliczonych testów. Do 15.11 mianownik to 9135, potem 9110
(25 testów z pismami spoza fontów testowych wyłączono z pomiaru).

```
13.11  pierwszy przebieg            2490  █████████████▋                          27,3%
13.11  arkusze XHTML, paniki        3100  ████████████████▉                       33,9%
13.11  rasteryzacja 96 dpi (*)      4295  ███████████████████████▌                47,0%
13.11  metryki fontu w pikselach    4829  ██████████████████████████▍             52,9%
13.11  ::first-letter, grid, kolumny 5221 ████████████████████████████▌           57,2%
13.11  atrybuty prezentacyjne HTML  5853  ████████████████████████████████        64,1%
13.11  koniec dnia (z krawędziami)  6325  ██████████████████████████████████▌     69,2%
14.11  stronicowanie flex i grid    6468  ███████████████████████████████████▍    70,8%
14.11  składnia, selektory, kaskada 6537  ███████████████████████████████████▊    71,6%
14.11  tabele                       6576  ████████████████████████████████████    72,0%
14.11  flex                         6655  ████████████████████████████████████▍   72,9%
14.11  pływające, @layer, gap       6724  ████████████████████████████████████▊   73,6%
14.11  listy i liczniki             6762  █████████████████████████████████████   74,0%
15.11  tekst, CSS 2, tabele         6855  █████████████████████████████████████▌  75,0%
15.11  zmiana zakresu pomiaru       6848  █████████████████████████████████████▌  75,2%
15.11  strony, flex, aspect-ratio   6919  █████████████████████████████████████▉  75,9%
15.11  box-shadow, nowsza składnia  6976  ██████████████████████████████████████▎ 76,6%
16.11  fonty, interlinia, monospace 7002  ██████████████████████████████████████▍ 76,9%
16.11  obramowania, margines strony 7004  ██████████████████████████████████████▍ 76,9%
16.11  siatka                       7048  ██████████████████████████████████████▋ 77,4%
16.11  siatka: rozpięte, obrazy     7058  ██████████████████████████████████████▊ 77,5%
16.11  ::first-line, abspos w siatce 7094 ██████████████████████████████████████▉ 77,9%
16.11  siatka: wysokość z wiersza   7112  ███████████████████████████████████████  78,1%
16.11  runda na dokumentach         7119  ███████████████████████████████████████  78,1%
16.11  kolumny, contain             7189  ███████████████████████████████████████▍ 78,9%
16.11  kolumny: strony, wysokości   7209  ███████████████████████████████████████▌ 79,1%
```

(*) Skoki z 13.11 to w dużej części poprawki metody pomiaru, nie silnika: w 40 dpi kształty o tej samej
geometrii różniły się wygładzaniem krawędzi.

Reftesty druku (z 340):

```
14.11  179  ██████████████████████████▎             52,6%
14.11  192  ████████████████████████████▏           56,5%
15.11  204  ██████████████████████████████          60,0%
15.11  210  ██████████████████████████████▉         61,8%
15.11  225  █████████████████████████████████       66,2%
16.11  228  █████████████████████████████████▌      67,1%
16.11  229  █████████████████████████████████▋      67,4%
16.11  230  █████████████████████████████████▊      67,6%
16.11  232  ██████████████████████████████████      68,2%
16.11  240  ███████████████████████████████████▎    70,6%
16.11  242  ███████████████████████████████████▌    71,2%
```

## Poprawki według obszarów

Liczba wpisów w `THIRD_PARTY.md` (zmiany w przejętym kodzie od 13.11). Przydział do obszaru jest
przybliżony: wpis dotykający dwóch obszarów liczony jest raz.

```
strony i podział między strony        39  ███████████████████▌
style, kaskada, selektory, składnia   39  ███████████████████▌
tekst i fonty                         36  ██████████████████
tabele                                29  ██████████████▌
siatka (grid)                         28  ██████████████
flex                                  27  █████████████▌
pływające i pozycjonowanie            12  ██████
listy, liczniki, treść generowana     11  █████▌
rysowanie i zapis PDF                  5  ██▌
bloki i marginesy                      4  ██
inne                                   9  ████▌
```

Razem 239 wpisów w chwili liczenia; dwie późniejsze paczki dodały 13 (siatka 4, strony 3, style 2, tekst 2,
pływające i pozycjonowanie 2). Trzy obszary zebrały prawie połowę poprawek: stronicowanie (bo to cel silnika,
a przejęty kod gubił treść na granicach stron), style i składnia (dużo małych braków) oraz tekst.

## Stan obszarów sondażu

Jeden znak to 5 punktów procentowych.

```
obszar                          zaliczone        brakuje
CSS 2 (5837)                    ██████████████████▊  94,0%    351
css-tables (141)                ███████████████▋     78,7%     30
css-lists (150)                 ███████████████▍     77,3%     34
css-flexbox (952)               ██████████████▏      70,7%    279
css-grid bez lanes i subgrid    █████████████▋       68,6%    201
css-multicol (370)              █████████████        65,1%    129
css-content (37)                ███████████▎         56,8%     16
css-grid: subgrid (88)          ████▊                23,9%     67
css-grid: grid-lanes (865)      ██▎                  11,3%    767
css-multicol: column-height (30) ██                  10,0%     27
```

`grid-lanes` (układ murarski) i `column-height` to funkcje eksperymentalne, których przeglądarki
jeszcze powszechnie nie mają. Bez nich i bez `subgrid` wynik to **7087 z 8127 (87,2%)**.

## Co zostało do naprawienia

Ważność to moja ocena z punktu widzenia celu projektu (wydruki typowych dokumentów), nie liczby testów.

### Wysoka: widać w zwykłych dokumentach

| Temat | Skąd wiadomo | Rozmiar |
|---|---|---|
| Linia bazowa w siatce i flex (`last baseline`, oś kolumn) | 30 testów `css-grid/alignment`, część `css-flexbox` | średni |
| Elementy siatki: procenty i `minmax` z obrazem, tabela jako element, ponowne liczenie kolumn po wierszach | ok. 30 testów `grid-items` | średni |
| Pola marginesów: pozostałe wymiary (część wymaga pisma pionowego) | 8 testów `margin-boxes/dimensions` | średni |
| Podział kontenera flex między strony: żądanie podziału w środku linii zagnieżdżonej, wysoki element pływający w elemencie flex | 5 reftestów druku flex | średni |
| Pozostałe reftesty druku: tabele (podział wierszy), pola marginesów, strony nazwane | 97 z 340 niezaliczonych | duży, po kawałku |
| `::first-line`: `vertical-align`, `text-transform`, elementy pływające w pierwszym wierszu | 4 testy; podstawa działa od 16.11 | mały |
| Tabela obok elementów pływających: zwężanie, żeby ominąć kolejny | `table-among-floats-001` (strata z 16.11) | mały |

### Średnia: rzadziej, ale spotykane

| Temat | Skąd wiadomo | Rozmiar |
|---|---|---|
| Kolumny: kolumny zagnieżdżone (ok. 45 testów), podział elementu rozpinającego między kolumny zewnętrzne, elementy pozycjonowane po elemencie rozpinającym, kolumny w komórce tabeli | 129 testów `css-multicol`; zwykłe kolumny na wielu stronach zgadzają się z Chromium (`22-multicol-pagination`) | duży, po kawałku |
| Elementy pozycjonowane w siatce | 62 testy `css-grid/abspos`, większość z pismem pionowym, `rtl` albo `last baseline` | średni |
| Elementy pływające: przypadki brzegowe z `clear` i marginesami | 58 testów CSS 2 | średni |
| Nieobsługiwane właściwości z ostrzeżeniem: `conic-gradient`, `clip-path`, `translate/rotate/scale`, `text-wrap`, `rgb(from …)`, `round()` | lista z audytu | mały każda |

### Niska: eksperymentalne albo poza zakresem

| Temat | Skąd wiadomo | Uwagi |
|---|---|---|
| Pismo pionowe (`writing-mode`) | 272 niezaliczone testy sondażu, 17 druku | ADR 0010: poza projektem; poddrzewo obrócone dopiero na potrzebę dokumentu |
| `grid-lanes` (układ murarski) | 766 testów | brak w większości przeglądarek |
| `subgrid` | 67 testów | do rozważenia po uporządkowaniu siatki |
| `flex-wrap: balance` | 28 testów `css-flexbox/balance` | eksperymentalne |
| `column-height` | 29 testów | eksperymentalne |
| Tekst dwukierunkowy (`bidi`) | 48 testów CSS 2 | poza zakresem (`docs/scope.md`) |
| `@container`, `@property`, `::selection` | lista z audytu | nie dotyczą druku albo rzadkie |

## Odchylenie od wydruków Chromium

Cały korpus, 2026-11-16 po drugiej paczce kolumn; słowa dopasowane przez wyrównanie ciągów. Szczegóły
i przyczyny pozostałych różnic są w `docs/feature-audit.md` (sekcja „Runda na dokumentach”).

| Dokument | Słowa wspólne | Mediana w pionie (pt) | Ponad 3 pt w pionie | Ponad 3 pt w poziomie | Na innej stronie |
|---|---|---|---|---|---|
| `01-record-copy` | 812 | 0,56 | 1 | 2 | 0 |
| `02-register-table` | 2844 | 0,34 | 0 | 0 | 0 |
| `03-report-svg-chart` | 140 | 0,58 | 0 | 2 | 0 |
| `04-qms-procedure` | 2605 | 0,81 | 20 | 0 | 36 |
| `05-hyphenation` | 99 | 0,18 | 0 | 2 | 0 |
| `06-margin-boxes` | 73 | 0,27 | 0 | 0 | 37 |
| `07-images` | 109 | 0,31 | 0 | 0 | 0 |
| `08-flex` | 189 | 0,24 | 0 | 0 | 0 |
| `09-lists-counters` | 170 | 0,21 | 0 | 0 | 0 |
| `10-preformatted` | 142 | 0,12 | 0 | 0 | 0 |
| `11-edge-cases` | 128 | 0,13 | 9 | 0 | 6 |
| `12-floats-positioning` | 230 | 0,22 | 0 | 1 | 0 |
| `13-forms-html-elements` | 131 | 2,72 | 52 | 1 | 0 |
| `14-cascade-selectors` | 179 | 0,57 | 0 | 0 | 0 |
| `15-page-breaks` | 413 | 0,22 | 0 | 0 | 0 |
| `16-typography-fonts` | 217 | 0,50 | 0 | 0 | 0 |
| `17-tables` | 175 | 0,33 | 0 | 0 | 0 |
| `18-toc-running-elements` | 910 | 0,32 | 25 | 0 | 0 |
| `19-flex-grid-pagination` | 2321 | 0,21 | 25 | 54 | 0 |
| `20-invoice` | 826 | 0,68 | 0 | 0 | 0 |
| `21-mixed-report` | 846 | 0,22 | 4 | 26 | 0 |
| `22-multicol-pagination` | 2345 | 0,20 | 0 | 0 | 0 |

W 16 z 22 dokumentów żadne słowo nie odbiega o więcej niż 3 pt w pionie. Kolumny w `21-mixed-report`
zgadzają się od 16.11 (było 193 słów ponad 3 pt: kontener kolumn był o 3 pt za wysoki i akapit po nim
spadał na następną stronę). Dla porównania, rano 16.11: `02-register-table` 5,57 pt (2015 słów ponad
3 pt), `07-images` 43,3 pt, `10-preformatted` 17,2 pt, `13-forms-html-elements` 21,5 pt.

## Sposób pracy od 2026-11-16

Poprawki robimy w 2–3 obszarach naraz i dopiero wtedy puszczamy pełne testy (decyzja użytkownika).
Pełny sondaż trwa ok. 10 minut, poprawka zwykle krócej, a obszary rzadko na siebie wpływają. Koszt:
gdy sondaż pokaże stratę, trzeba ustalić, która z 2–3 poprawek ją spowodowała.
