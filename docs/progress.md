# Postęp: co naprawiono, co zostało, co jest ważne

Stan na 2026-11-16 po paczce: pomniejszanie treści szerszej niż strona (ADR 0014). Dokument aktualizujemy po każdej paczce poprawek.
Szczegóły każdej rundy są w `docs/feature-audit.md`, lista zmian w przejętym kodzie w `THIRD_PARTY.md`.

Sposób pomiaru: sondaż ekranowy to reftesty W3C (WPT) z siedmiu obszarów, renderowane bez pomniejszania
treści (`-max-shrink 1`, od 16.11), rasteryzowane w 96 dpi
i porównywane według ADR 0003 (zaliczenie ścisłe albo z dokładnością do krawędzi). Reftesty druku to
340 testów stronicowania trzymanych w `testdata/wpt`. Korpus to 21 własnych dokumentów
w `testdata/documents` porównywanych z wydrukami Chromium.

## Stan w liczbach

| Miara | Wynik | Uwagi |
|---|---|---|
| Sondaż ekranowy W3C | **7112 z 9110 (78,1%)** | ściśle 6724, krawędziowo 388 |
| Reftesty druku W3C | **242 z 340 (71,2%)** | wszystkie ściśle |
| Korpus: liczba stron jak w Chromium | 20 z 21 dokumentów | wyjątek: `06-margin-boxes` |
| Testy własne | 98 funkcji testowych, wszystkie przechodzą | w tym 73 w `regression_test.go` |
| Testy przejętego kodu | 6 stałych niezaliczeń | te same od 2026-11-13 |
| Commity | 103 w 4 dni | 13.11: 11, 14.11: 66, 15.11: 17, 16.11: 9 |
| Zmiany w przejętym kodzie | 261 wpisów w `THIRD_PARTY.md` | każda z odwołaniem do normy |
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
CSS 2 (5837)                    ██████████████████▊  93,8%    359
css-tables (141)                ███████████████▋     78,7%     30
css-lists (150)                 ███████████████▍     77,3%     34
css-flexbox (952)               ██████████████▏      70,8%    278
css-grid bez lanes i subgrid    █████████████▋       68,4%    202
css-content (37)                ███████████▎         56,8%     16
css-multicol (370)              ████████▎            41,4%    217
css-grid: subgrid (88)          ████▊                23,9%     67
css-grid: grid-lanes (865)      ██▎                  11,4%    766
css-multicol: column-height (30) ▋                    3,3%     29
```

`grid-lanes` (układ murarski) i `column-height` to funkcje eksperymentalne, których przeglądarki
jeszcze powszechnie nie mają. Bez nich i bez `subgrid` wynik to **6991 z 8127 (86,0%)**.

## Co zostało do naprawienia

Ważność to moja ocena z punktu widzenia celu projektu (wydruki typowych dokumentów), nie liczby testów.

### Wysoka: widać w zwykłych dokumentach

| Temat | Skąd wiadomo | Rozmiar |
|---|---|---|
| Linia bazowa w siatce i flex (`last baseline`, oś kolumn) | 30 testów `css-grid/alignment`, część `css-flexbox` | średni |
| Elementy siatki: procenty i `minmax` z obrazem, tabela jako element, ponowne liczenie kolumn po wierszach | ok. 30 testów `grid-items` | średni |
| Pola marginesów: pozostałe wymiary (część wymaga pisma pionowego) | 8 testów `margin-boxes/dimensions` | średni |
| Podział kontenera flex między strony: żądanie podziału w środku linii zagnieżdżonej, wysoki element pływający w elemencie flex | 5 reftestów druku flex | średni |
| Pozostałe reftesty druku: tabele (podział wierszy), pola marginesów, strony nazwane | 98 z 340 niezaliczonych | duży, po kawałku |
| `::first-line`: `vertical-align`, `text-transform`, elementy pływające w pierwszym wierszu | 4 testy; podstawa działa od 16.11 | mały |
| Tabela obok elementów pływających: zwężanie, żeby ominąć kolejny | `table-among-floats-001` (strata z 16.11) | mały |

### Średnia: rzadziej, ale spotykane

| Temat | Skąd wiadomo | Rozmiar |
|---|---|---|
| Kolumny: element rozpinający w zagnieżdżeniu, równoważenie, podział | 217 testów `css-multicol` | duży |
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

Mediana odchylenia położenia słów w pionie (pt) i liczba słów odchylonych o ponad 3 pt, dla dokumentów,
w których słowa dają się dopasować po kolei:

| Dokument | 16.11 rano | 16.11 po poprawkach |
|---|---|---|
| `02-register-table` | 5,57 pt, 2015 z 2844 słów | 0,34 pt, 0 słów |
| `08-flex` | 3,61 pt, 121 z 189 | 0,24 pt, 0 |
| `20-invoice` | 2,52 pt, 350 z 826 | 0,68 pt, 0 |
| `19-flex-grid-pagination` | 310 z 2313 | 0,21 pt, 25 z 2313 |
| `17-tables` | 1,32 pt, 13 z 175 | 1,84 pt, 0 |

Pozostałe 25 słów w `19-flex-grid-pagination` to karty o stałej wysokości: Chromium tnie je na
granicy strony, gopdfgo przenosi w całości (ADR 0004). `07-images` i `10-preformatted` Chromium
drukuje w skali 0,667; od ADR 0014 gopdfgo robi to samo (mediana odchylenia 0,3 pt i 0,1 pt).

## Sposób pracy od 2026-11-16

Poprawki robimy w 2–3 obszarach naraz i dopiero wtedy puszczamy pełne testy (decyzja użytkownika).
Pełny sondaż trwa ok. 10 minut, poprawka zwykle krócej, a obszary rzadko na siebie wpływają. Koszt:
gdy sondaż pokaże stratę, trzeba ustalić, która z 2–3 poprawek ją spowodowała.
