# ADR 0043: Szerokość tekstu z dokładnych szerokości znaków, w górę do LayoutUnit jak w Chromium

Status: przyjęta 2026-11-22.

## Kontekst

Rozbiór dokumentów biurowych i losowych (`docs/feature-audit.md`, 2026-11-22) zostawił trzy dokumenty
(`office-quote`, `office-wmbus-protocol`, `rand-29` w `bench/realdocs`), w których wiersz kończący się tuż
przy krawędzi mieścił się u nas, a w Chromium nie. Przypuszczenie z rozbioru: szerokości znaków są
zaokrąglane do 1/64 px każda osobno, więc suma wiersza bywa o setne części piksela mniejsza niż w Chromium.
Rozbiór na minimalnych przypadkach (Chrome 157, pliki w `bench/lw/`, poza gitem) pokazał cztery źródła.

**1. Szerokości znaków zaokrąglane osobno.** go-text składa tekst w `fixed.Int26_6` (1/64 jednostki
rozmiaru). Kształtujemy go w rozmiarze 64 razy większym (`shapePrecisely`, `shapingPrecision`), więc
HarfBuzz daje pozycje z dokładnością 1/4096 px, a `shapeAtSize` zaokrąglało każdą szerokość do najbliższej
1/64 px. Błąd nie jest przypadkowy: przy fontach o 2048 jednostkach na em i małym piśmie wiele liter ma
szerokość tuż powyżej połowy jednostki 1/64 px. Tinos 8 px: wiersz z `office-quote` ma dokładnie
627,1016 px (tyle samo w Chromium), a suma zaokrąglonych szerokości 626,7969 px; w Noto Sans 14 px
(`md-docs-0033`) odwrotnie, zaokrąglone 503,13 px wobec dokładnych 503,01 px. Reszty zaokrągleń były
liczone już wcześniej, ale `LineWrap` przyjmował bez sprawdzenia każdy wiersz, którego suma zaokrąglona
mieściła się w dostępnej szerokości, a szerokość pudełka tekstu i min-/max-content były sumą zaokrągloną.

**2. Jak liczy Chromium** (pomiar 2026-11-22, `bench/lw/m1.html`, `m5.html`): szerokość kawałka tekstu
(część węzła tekstowego w jednym wierszu) to suma dokładnych pozycji HarfBuzz zamieniona na LayoutUnit
w górę. 100 znaków „a” Tinos 13 px, każdy 5,77001953 px: `getBoundingClientRect` i `width: max-content`
dają 577,015625 px (577,00195 px w górę do 1/64 px). Wiersz mieści się, gdy ta szerokość nie przekracza
dostępnej powiększonej o 1/64 px (ADR 0013): „a×100 a” (586,02197 px) mieści się w 586,015625 px,
w 586 px już nie; tak samo w siedmiu innych zestawach fontu, rozmiaru i liczby znaków. Kerning obejmuje
też pary ze spacją (Arimo „a T”: 19,984375 px w układzie); pomiar przez `canvas.measureText` kerningu
na spacjach nie ma, więc do porównań się nie nadaje.

**3. Rozmiar pisma obcinany do 0,01 px w podwójnej precyzji.** `chromiumFontSize` liczyło
`floor(size·100 + 1e-6)` w `float64`; 0.88em przy 14 px to w `float32` 12,3199997, razy 100 w podwójnej
precyzji 1231,99997, więc wychodziło 12,31 px. Blink mnoży w `float`, co daje 1232, czyli 12,32 px
(`bench/lw/m9.html`: 100 znaków Noto Sans Mono `.88em` i `12.32px` ma 739,203125 px, `12.3199px`
738,609375 px). Wstawki `code` w dokumentacji Markdown (`font-size: .88em`) były przez to o 0,08% za wąskie.

**4. Szerokość układu przy pomniejszaniu** (ADR 0014; `rand-29`, `rand-26`, `rand-28`): układamy dokument na
obszarze strony powiększonym dokładnie tyle razy, ile treść jest szersza. Chromium układa go na szerokości
całkowitej (`bench/lw/m6.py`, `m10.py`: przy elemencie szerokim na 450,1, 450,4, 450,6 i 450,9 px na
obszarze A5 409 px w wierszu mieści się 450 px plus 1/64 px; 451,5 px daje 451, 480,5 px daje 480),
a wysokość strony i skalę rysowania liczy z szerokości dokładnej (`m7.py`, `m8.py`: strona mieści 707 px
treści przy 450,9 px, a odcinek 400 px rysuje się jako 362,8374 px, czyli w skali 409 : 450,9). Na A4
(obszar 643 px) reguła nie jest czystym obcięciem: 700,3 i 700,5 px daje 700, a 700,6 px już 701,
912,6 px daje 912, a 912,7 i 912,86 px 913 (`m10.py`); próg ułamka rośnie z szerokością, a formuły nie
ustaliliśmy.

## Decyzja

1. `shapeAtSize` zaokrągla szerokości znaków z przenoszeniem błędu: zaokrąglana jest pozycja końca znaku
   od początku przebiegu, szerokość znaku to różnica sąsiednich pozycji. Suma dowolnego ciągu znaków różni
   się od dokładnej najwyżej o 1/64 px, więc także położenia glifów w PDF są bliżej dokładnych. Reszty
   (`residuals`, teraz `float64` w 1/4096 px) liczymy zawsze, a rozmiar `fine` przekazujemy bez
   zaokrąglenia do siatki 1/4096 px.
2. Wiersz mieści się, gdy jego szerokość dokładna (zaokrąglona plus reszty, bez końcowych odstępów)
   nie przekracza dostępnej (`LineWrap`); gdy się nie mieści, próbujemy krótszego, a gdy krótszego nie
   ma, zostaje wiersz z jednym słowem.
3. Szerokość kawałka tekstu (`FirstLine.Width`, `preciseLineWidth`) to szerokość dokładna; przy
   `ExactLengths: false` zaokrąglona w górę do 1/64 px (`TextStyle.LayoutUnits`), przy `true` bez
   zaokrąglenia. Ta sama funkcja daje szerokości min-/max-content (`preferred.go` woła ten sam podział
   wiersza), więc pudełko o szerokości max-content nie łamie własnego tekstu. Wiersz, którego treść
   dokładnie się mieści, a szerokość w górę do 1/64 px przekracza dostępną o mniej niż 1/64 px, zgłasza
   szerokość dostępną (jak dotąd wiersz przyjęty z tolerancją); zapas 1/16 px na zaokrąglenia
   wcześniejszych pudełek w wierszu jest zbędny i znika.
4. `chromiumFontSize` mnoży przez 100 w pojedynczej precyzji.
5. Przy `ExactLengths: false` szerokość powiększonego obszaru strony obcinamy w dół do całych pikseli;
   wysokość i skala rysowania zostają dokładne. To przybliżenie: zgadza się z każdym pomiarem na A5 i z
   połową pomiarów na A4 (tam Chromium czasem zaokrągla w górę). Z trzech wariantów sprawdzonych na
   `bench/realdocs` (w dół, do najbliższego, bez zaokrąglenia) obcięcie daje najmniej słów przesuniętych:
   `rand-26` 433 → 0, `rand-29` 1147 → 711, a zaokrąglenie do najbliższego psuje `rand-26` z powrotem
   (433). Dokładna reguła Chromium zostaje do ustalenia (pytanie 29 w `open-questions.md`).

## Skutki

Pomiar (2026-11-22, `bench/realdocs`, `driftalign.py` wobec Chrome 157; inne strony / mediana dy w pt /
słowa odchylone w pionie o ponad 3 pt / w poziomie o ponad 3 pt), dokumenty, w których coś się zmieniło:

| Dokument | Przed | Po |
|---|---|---|
| `md-docs-0033-tagged-pdf` | 0 / 0,19 / 27 / 18 | 0 / 0,19 / 26 / 0 |
| `md-gomdgo-differences` | 16 / 21,87 / 296 / 191 | 16 / 21,94 / 296 / 191 |
| `md-gomdgo-testing` | 63 / 0,44 / 757 / 331 | 63 / 0,42 / 756 / 329 |
| `md-gopdfgo-engine-fit` | 0 / 0,20 / 0 / 0 | 0 / 0,21 / 0 / 0 |
| `office-db-model` | 199 / 69,42 / 2230 / 13 | 199 / 69,42 / 2231 / 13 |
| `office-quote` | 0 / 0,00 / 1 / 8 | 0 / 0,00 / 0 / 0 |
| `office-terminology` | 141 / 45,45 / 1853 / 6 | 141 / 45,45 / 1852 / 2 |
| `office-wmbus-protocol` | 0 / 0,00 / 2 / 26 | 0 / 0,00 / 0 / 0 |
| `rand-01` | 2508 / 5,90 / 1056 / 9 | 2508 / 5,94 / 1058 / 32 |
| `rand-02` | 211 / 108,46 / 1442 / 4 | 146 / 54,13 / 1238 / 40 |
| `rand-03` | 54 / 15,92 / 967 / 24 | 54 / 16,06 / 966 / 3 |
| `rand-08` | 0 / 0,24 / 0 / 0 | 0 / 0,23 / 1 / 5 |
| `rand-09` | 104 / 0,36 / 1048 / 1 | 119 / 0,36 / 1046 / 0 |
| `rand-10` | 0 / 0,21 / 0 / 0 | 0 / 0,20 / 0 / 0 |
| `rand-11` | 102 / 79,81 / 2521 / 27 | 102 / 79,81 / 2519 / 31 |
| `rand-12` | 0 / 0,19 / 282 / 1 | 0 / 0,19 / 179 / 0 |
| `rand-25` | 207 / 0,25 / 777 / 32 | 185 / 0,25 / 781 / 32 |
| `rand-26` | 0 / 0,30 / 433 / 60 | 0 / 0,20 / 0 / 0 |
| `rand-27` | 850 / 344,67 / 827 / 1 | 812 / 344,67 / 827 / 1 |
| `rand-28` | 8 / 0,25 / 41 / 1 | 8 / 0,15 / 28 / 5 |
| `rand-29` | 140 / 0,34 / 1147 / 70 | 135 / 0,24 / 711 / 0 |
| `rand-30` | 169 / 134,75 / 657 / 87 | 169 / 122,99 / 680 / 104 |

Pozostałe dokumenty (faktury, e-SF, mermaid, reszta `md-*`, `office-*` i `rand-*`) bez zmian liczb; liczba
stron zgodna z Chromium tam, gdzie była. Problem z rozbioru znika w `office-quote` i `office-wmbus-protocol`;
w `rand-29` źródłem była szerokość układu przy pomniejszaniu (punkt 4), po poprawce 711 słów zamiast 1147
(reszta to inne różnice tego dokumentu). Mediana dy rośnie w czterech dokumentach:

- `md-gopdfgo-engine-fit` (0,20 → 0,21 pt): dokument jest pomniejszany przez tabelę o szerokości
  min-content większej niż strona; przy dokładnych szerokościach słów tabela ma 678,44 px (wcześniej 678,17,
  w Chromium 678,67 px przy układzie na 673 px), więc zmienia się skala, która wcześniej przypadkiem
  wyrównywała inne różnice w pionie (wiersze w Chromium są o ok. 0,5 pt na stronę niższe);
- `md-gomdgo-differences`, `rand-01`, `rand-03` (o 0,04–0,14 pt przy medianie 6–22 pt): dokumenty z dużymi
  rozbieżnościami z innych przyczyn (pytanie 28, elementy pływające, kolumny); przestawia się kilka złamań.

Pogarszają się też, przy tych samych przyczynach, liczby słów w dokumentach losowych `rand-09` (104 → 119
słów na innej stronie), `rand-30` (657 → 680) i `rand-01` (9 → 32 w poziomie) oraz `rand-08` (1 słowo
i 5 w poziomie: Chromium układa ten dokument na 913 px, my na 912 px; pytanie 29).

**Sondaż CSS** (tryb ciągły, `bench/verify/css.sh`): 9484 z 10962 wobec 9499, 2 zyski i 20 strat, w tym
3 znane `hanging-punctuation-last*` (ADR 0038). Pozostałe straty to reftesty, w których test i wzorzec
dzielą ten sam tekst na inne pudełka (`<span>` w środku zdania, treść generowana, `text-transform`,
liczniki, tabulator): po zmianie położenia glifów zależą od podziału na pudełka o ułamek piksela (do
1/64 px przez zaokrąglenie w górę każdego kawałka, do 1/128 px przez przenoszenie błędu w przebiegu),
a porównanie rastrów jest dokładne co do piksela. Przedtem każdy znak miał tę samą zaokrągloną szerokość
niezależnie od położenia, więc wynik nie zależał od podziału, za to szerokości odbiegały od dokładnych
(i od Chromium) o dziesiąte części piksela na wiersz. Chromium ma to samo przesunięcie o 1/64 px na
granicy pudełek, ale rysuje glify na siatce 1/4 px i różnicy w rastrze zwykle nie widać. Wariant bez
zaokrąglenia w górę (szerokość dokładna) zostawia 10 z tych strat, ale w `bench/realdocs` przywraca dwa
z naprawionych złamań (`md-docs-0033`: wstawka kodu i przecinek u końca wiersza; `rand-06` 257 → 306 słów),
bo Chromium sprawdza szerokość wiersza jako sumę kawałków zaokrąglonych w górę. Trzy straty
(`hyphens-auto-004`, `trailing-other-space-separators-002`, `white-space-pre-wrap-trailing-spaces-021`)
to wiersze z tekstem o szerokości dokładnie równej szerokości pudełka w `ch` i wiszącymi odstępami innymi
niż spacja: zaliczały się, bo zaokrąglona szerokość była o tysięczne części piksela większa od dokładnej;
do rozebrania osobno. Decyzja o tym, czy straty sondażu przyjąć, należy do użytkownika (pytanie 29).

Sondaż SVG 194 z 235 bez zmian. Korpus własny: liczby stron bez zmian, rastry różne (położenia glifów),
słowa przesunięte względem Chromium bez zmian (`corpuscmp.py`).

Lista zaokrągleń przy `ExactLengths: false` w ADR 0013 i w komentarzu `Options.ExactLengths` dostaje dwie
pozycje (szerokość kawałka tekstu, szerokość obszaru przy pomniejszaniu). Odstępy `letter-spacing`
i `word-spacing` są dalej obcinane do 1/64 px przed dodaniem do szerokości znaków (`floatToFixed`); Chromium
dodaje je w liczbach zmiennoprzecinkowych, czego nie mierzyliśmy.

Test: `TestPreciseLineWidth` w `tests/linewidth22_test.go`.
