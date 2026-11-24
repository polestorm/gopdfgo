# Postęp: co naprawiono, co zostało, co jest ważne

Stan na 2026-11-22 (tabela „Stan w liczbach”; dalsze sekcje opisują stan z dat podanych przy nich). Dokument aktualizujemy po każdej paczce poprawek.
Szczegóły każdej rundy są w `docs/feature-audit.md`, lista zmian w przejętym kodzie w `docs/provenance.md`.

Sposób pomiaru: sondaż ekranowy to reftesty W3C (WPT) z siedmiu obszarów (od 16.11 osobno liczone trzy
dalsze: tekst, tła, fonty), renderowane bez pomniejszania treści i bez podziału na strony
(`-max-shrink 1 -continuous`, od 16.11), rasteryzowane w 96 dpi, przycinane do okna 800 x 600 px
i porównywane według ADR 0003 (zaliczenie ścisłe albo z dokładnością do krawędzi). Do 16.11 sondaż był
stronicowy; ostatni wynik tą metodą to 7209 z 9110 i 1095 z 1768.

Od 2026-11-17 klon testów W3C leży w `bench/survey/wpt` (ten sam commit `ab97c017`) i zawiera całe katalogi
`/fonts/` i `/images/`. Dzięki temu do pomiaru weszły 84 testy, które wcześniej odpadały przez brakujące
zasoby (41 z nich przechodzi). Pozostałe 10 878 testów daje na tej samej binarce wynik identyczny
z poprzednim klonem, więc szereg jest ciągły poza tym dodatkiem. Reftesty druku to
340 testów stronicowania trzymanych w `testdata/wpt`. Korpus to 22 własne dokumenty
w `testdata/documents` porównywanych z wydrukami Chromium.

Od 2026-11-17 test z kilkoma odnośnikami `rel="match"` jest zaliczony, gdy zgadza się z którymkolwiek z nich
(tak definiuje to WPT: odnośniki są alternatywami). Wcześniej sondaż wymagał zgodności ze wszystkimi.
Dotyczy 61 testów (47 w `css-text`, 12 w CSS 2, 2 w `css-fonts`); sama zmiana miary dała 13 zaliczeń
(9 w tekście, 4 w CSS 2).

Od 2026-11-17 skrypt sondażu porównuje obrazy w ośmiu procesach zamiast wątków (porównanie pikseli to czysty
Python, wątki wykonywały je po kolei) i pamięta ostatnio renderowane wzorce (11 061 użyć przy 5066 różnych).
Pełny przebieg dziesięciu obszarów trwa 6 min 50 s zamiast ok. 17 min; wynik jest identyczny co do testu
(sprawdzone na 2837 testach czterech obszarów).

## Stan w liczbach

| Miara | Wynik | Uwagi |
|---|---|---|
| Sondaż ekranowy W3C, 11 obszarów CSS | **9499 z 10962 (86,7%)** | 22.11: 3 testy `hanging-punctuation-last*` przechodziły przypadkiem i przestały po poprawce łamania (ADR 0038); `bench/verify/css.sh` |
| Reftesty druku W3C | **310 z 340 (91,2%)** | ściśle 308, krawędziowo 2; rozbiór 30 niezaliczonych w `docs/feature-audit.md` (21.11) |
| Reftesty W3C `svg/` | **194 z 235** | rano 20.11: 83; pozostałe głównie CSS Linked Parameters (propozycja), `zoom`, animacje |
| Korpus: liczba stron jak w Chromium | 22 z 22 dokumentów | odchylenie słów: `docs/overview.md` |
| Prawdziwe dokumenty wobec Chromium (`bench/realdocs`) | 4 faktury KSeF i 5 e-SF: strony i łamanie jak w Chromium, każde słowo w granicach 0,81 pt; 12 dokumentów Markdown i 10 stron z diagramami: liczby stron jak w Chromium | reszta różnic opisana (UAX #14 LB20a, `widows`, fonty systemowe Chromium); pytanie 28 |
| PDF/A-2b, PDF/A-2a, PDF/UA-1 | 22 z 22 dokumentów korpusu zgodne według veraPDF 1.30 | ADR 0016, 0033 |
| Factur-X / ZUGFeRD | faktura MINIMUM: PDF/A-3b oraz 3a + UA-1; veraPDF i Mustang 2.26.0: poprawne | ADR 0034 |
| Testy własne | 274 funkcje testowe, wszystkie przechodzą | plus `go test -tags systemfonts` dla wariantu z czcionkami systemowymi |
| Testy przejętego kodu | 6 stałych niezaliczeń | te same od 2026-11-13 |
| Czas i pamięć: książka 256 stron | 1,17–1,22 s, 0,42–0,44 GB (Chromium 2,10–2,56 s, 1,67–1,73 GB) | 1021 stron: 4,57–4,69 s (Chromium 4,54–4,69 s); ADR 0037 |
| Rozmiar binarki | 19,1 MB (linux/amd64, `-s -w`) | 22.11: −13,7% po usunięciu `text/template` (ADR 0039) |
| Commity | 554 | 13–22.11 |
| Zmiany w przejętym kodzie | 278 wpisów w `docs/provenance.md` | każda z odwołaniem do normy |
| Decyzje (ADR) | 40 | otwarte pytanie 28 (dywiz na początku słowa, `widows` w krótkich blokach) |

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
16.11  tryb ciągły (*)              7229  ███████████████████████████████████████▋ 79,4%
17.11  większy zestaw (9123)        7240  ███████████████████████████████████████▋ 79,4%
17.11  tekst, flex                  7273  ███████████████████████████████████████▊ 79,7%
17.11  siatka, flex                 7304  ████████████████████████████████████████  80,1%
17.11  kolumny w trybie ciągłym     7319  ████████████████████████████████████████  80,2%
17.11  CSS 2: ogon grup             7367  ████████████████████████████████████████▍ 80,8%
17.11  praca równoległa (5 obszarów) 7389 ████████████████████████████████████████▌ 81,0%
17.11  balance, alternates, flex     7415 ████████████████████████████████████████▋ 81,3%
17.11  kolumny zagnieżdżone         7427 ████████████████████████████████████████▋ 81,4%
17.11  elementy pływające, tabele    7517 █████████████████████████████████████████▏ 82,4%
18.11  ogon CSS 2 i tekst            7569 █████████████████████████████████████████▌ 83,0%
18.11  flex, siatka, tła, fonty, kolumny 7677 ██████████████████████████████████████████ 84,1%
18.11  listy, tekst, ogony obszarów  7771 ██████████████████████████████████████████▌ 85,2%
18.11  reszta pięciu obszarów       7817 ██████████████████████████████████████████▊ 85,7%
18.11  tekst; CSS 2 i kolumny       7830 ██████████████████████████████████████████▉ 85,8%
19.11  druk: tabele, flex, kolumny   7835 ██████████████████████████████████████████▉ 85,9%
```

(*) Skoki z 13.11 i przejście na tryb ciągły 16.11 to poprawki metody pomiaru, nie silnika; przy trybie
ciągłym 20 testów w tych siedmiu obszarach i 53 w tłach okazało się artefaktem podziału na strony.
Z 13.11: w 40 dpi kształty o tej samej
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
18.11  244  ███████████████████████████████████▉    71,8%
18.11  245  ████████████████████████████████████    72,1%
18.11  279  █████████████████████████████████████████82,1%
18.11  282  █████████████████████████████████████████▍82,9%
19.11  287  ██████████████████████████████████████████▏84,4%
19.11  296  ███████████████████████████████████████████▌87,1%
19.11  300  ████████████████████████████████████████████ 88,2%
20.11  310  █████████████████████████████████████████████▌91,2%
```

## Poprawki według obszarów

Liczba wpisów w `docs/provenance.md` (zmiany w przejętym kodzie od 13.11). Przydział do obszaru jest
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
pływające i pozycjonowanie 2). Piąta, szósta i siódma paczka równoległa (18.11) dodały po 5 wpisów zbiorczych, po jednym na obszar. Trzy obszary zebrały prawie połowę poprawek: stronicowanie (bo to cel silnika,
a przejęty kod gubił treść na granicach stron), style i składnia (dużo małych braków) oraz tekst.

## Stan obszarów sondażu

Jeden znak to 5 punktów procentowych.

```
obszar                          zaliczone        brakuje
CSS 2 (5846)                    ███████████████████▍ 97,3%    156
css-backgrounds (603)           ██████████████████▉  94,9%     31
css-tables (141)                ██████████████████▎  91,5%     12
css-lists (151)                 ██████████████████▊  94,0%      9
css-flexbox (952)               █████████████████▍   87,0%    124
css-grid bez lanes i subgrid    ████████████████▊    84,2%    101
css-multicol (370)              ██████████████████   90,0%     37
css-text (909)                  █████████████████▊   89,2%     98
css-fonts (327)                 █████████████████▍   87,2%     42
css-content (40)                ████████████████▌    82,5%      7
css-grid: subgrid (88)          ██████▎              31,8%     60
css-grid: grid-lanes (865)      ██▍                  11,9%    762
css-multicol: column-height (30) ██                  10,0%     27
```

`grid-lanes` (układ murarski) i `column-height` to funkcje eksperymentalne, których przeglądarki
jeszcze powszechnie nie mają. Bez nich i bez `subgrid` wynik siedmiu dotychczasowych obszarów to
**7699 z 8140 (94,6%)**; `css-text`, `css-backgrounds` i `css-fonts` liczymy osobno (tabela wyżej).

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
| Nieobsługiwane właściwości z ostrzeżeniem: `translate`, `rotate`, `scale`, `rgb(from …)`, `round()` (`conic-gradient` i `clip-path` działają; sprawdzone 2026-11-20) | lista z audytu | mały każda |

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

Cały korpus, 2026-11-21 po obcięciu długości układu do 1/64 px (LayoutUnit Blink, ADR 0013, uzupełnienie);
słowa dopasowane przez wyrównanie ciągów (`driftalign.py`), wzorce `testdata/documents/reference/chromium`.
Mediana w pionie przed → po zmianie; pozostałe kolumny po zmianie (przed były takie same). Szczegóły
i przyczyny pozostałych różnic są w `docs/feature-audit.md` (sekcja „Runda na dokumentach”).

| Dokument | Słowa wspólne | Mediana w pionie (pt) | Ponad 3 pt w pionie | Ponad 3 pt w poziomie | Na innej stronie |
|---|---|---|---|---|---|
| `01-record-copy` | 811 | 0,56 → 0,18 | 0 | 0 | 0 |
| `02-register-table` | 2844 | 0,28 → 0,16 | 0 | 0 | 0 |
| `03-report-svg-chart` | 141 | 0,11 → 0,22 | 0 | 0 | 0 |
| `04-qms-procedure` | 2605 | 0,73 → 0,19 | 20 | 0 | 36 |
| `05-hyphenation` | 101 | 0,16 → 0,15 | 0 | 2 | 0 |
| `06-margin-boxes` | 73 | 0,27 → 0,23 | 0 | 0 | 37 |
| `07-images` | 109 | 0,20 → 0,18 | 0 | 0 | 0 |
| `08-flex` | 189 | 0,17 → 0,22 | 0 | 0 | 0 |
| `09-lists-counters` | 170 | 0,15 → 0,21 | 0 | 0 | 0 |
| `10-preformatted` | 142 | 0,14 → 0,11 | 0 | 0 | 0 |
| `11-edge-cases` | 132 | 0,11 → 0,11 | 0 | 0 | 0 |
| `12-floats-positioning` | 230 | 0,23 → 0,22 | 0 | 0 | 0 |
| `13-forms-html-elements` | 132 | 0,84 → 0,38 | 2 | 1 | 0 |
| `14-cascade-selectors` | 179 | 0,51 → 0,22 | 0 | 0 | 0 |
| `15-page-breaks` | 413 | 0,22 → 0,18 | 0 | 0 | 0 |
| `16-typography-fonts` | 223 | 0,22 → 0,20 | 2 | 0 | 0 |
| `17-tables` | 175 | 0,26 → 0,22 | 0 | 0 | 0 |
| `18-toc-running-elements` | 910 | 0,27 → 0,19 | 25 | 0 | 0 |
| `19-flex-grid-pagination` | 2321 | 0,19 → 0,19 | 18 | 0 | 0 |
| `20-invoice` | 826 | 0,67 → 0,19 | 0 | 0 | 0 |
| `21-mixed-report` | 846 | 0,19 → 0,19 | 0 | 0 | 0 |
| `22-multicol-pagination` | 2345 | 0,18 → 0,18 | 0 | 0 | 0 |

Liczba stron bez zmian. Chrome przyciąga linię bazową tekstu do całych pikseli dopiero przy rysowaniu; tego
nie naśladujemy, więc mediana ok. 0,2 pt (do pół piksela) jest dolną granicą tej miary. Trzy dokumenty,
których mediana wzrosła (0,11–0,17 → 0,21–0,22), miały ją niższą przypadkiem: dotąd tekst stał w połowie
piksela między położeniem w Chrome a jego przyciągnięciem; p90 spadło we wszystkich trzech.

Prawdziwe dokumenty spoza korpusu (`bench/realdocs`: 4 faktury KSeF, 5 sprawozdań e-SF, wydruk Chrome 157):
mediana 0,33–0,65 → 0,14–0,23 pt, słowa ponad 3 pt w e-SF 2 i e-SF 4 z 1491 i 1342 do 3 i 0, w e-SF 2 już
żadne słowo nie trafia na inną stronę (było 39); tabela w ADR 0013.

## Sposób pracy od 2026-11-16

Poprawki robimy w 2–3 obszarach naraz i dopiero wtedy puszczamy pełne testy (decyzja użytkownika).
Pełny sondaż trwa ok. 10 minut, poprawka zwykle krócej, a obszary rzadko na siebie wpływają. Koszt:
gdy sondaż pokaże stratę, trzeba ustalić, która z 2–3 poprawek ją spowodowała.
