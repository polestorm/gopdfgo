# Przegląd stanu funkcji: poprawić, zostawić czy wyłączyć

Stan na 2026-11-13. Zasada (decyzja użytkownika): z żadnej funkcji nie rezygnujemy na zapas; dla każdej, która
działa źle, oceniamy przydatność i koszt dokończenia, a potem wybieramy: poprawić, zostawić albo wyłączyć do czasu
poprawy. Ten dokument zbiera pomiary do takich decyzji. Częstości użycia funkcji w dokumentach nie udało się
zmierzyć (wyszukiwanie statystyk nic wiarygodnego nie dało); oceny przydatności są opinią, oznaczoną jako opinia.

## 1. Rozmiar: co ile waży

Binarka polecenia `cmd/gopdfgo`: 22,2 MB (macOS arm64, go1.26.6, `CGO_ENABLED=0 -trimpath -ldflags='-s -w'`).
Sekcje wg `size -m`:

| Składnik | Rozmiar | Uwagi |
|---|---|---|
| kod maszynowy wszystkich pakietów | 6,5 MB | cały układ (`html/layout`) to ok. 0,3 MB wg `go tool nm` |
| dane tylko do odczytu | 6,2 MB | z czego słowniki dzielenia wyrazów 42 języków: 5,8 MB plików źródłowych |
| tablice wierszy i funkcji (`gopclntab`) | 5,4 MB | proporcjonalne do kodu |
| stałe i typy | 1,9 MB | |
| dane inicjowane | 1,4 MB | |

Wniosek: **usuwanie funkcji CSS prawie nie zmniejsza binarki**. Układ grid to dziesiątki kilobajtów. Rozmiar
siedzi w danych i w zależnościach: słowniki dzielenia wyrazów (ok. jednej czwartej binarki), tablice kodowań
znaków CJK z `golang.org/x/text` i stos sieciowy (`net/http`, `crypto/tls`) wciągany przez domyślne pobieranie
zasobów, którego `gopdfgo.Render` nie używa. Te trzy pozycje warto zmierzyć osobno i uczynić wybieralnymi.

## 2. Sondaż reftestów ekranowych WPT

9135 statycznych reftestów (bez JavaScriptu) z katalogów `css-grid`, `css-flexbox`, `css-multicol`, `css-tables`,
`css-lists`, `css-content`, `CSS2` klonu WPT `ab97c017`. Każdy plik renderowany osobnym procesem `gopdfgo`
(limit 15 s), strona 800 × 600 px bez marginesów, `pdftoppm -r 96`, tolerancja jak w `reftest_test.go`. Skrypt
i wyniki poza repozytorium (katalog roboczy sesji). Stan po poprawkach z pkt 6, pomiar 2026-11-13.

Ogółem: **4295 zaliczonych (47%)**, 4821 niezaliczonych, 11 przekroczeń czasu, 2 paniki, 6 innych błędów.

| Obszar | Zaliczone | Inne |
|---|---|---|
| CSS 2.1: lists | 148 / 153 (97%) |  |
| CSS 2.1: visufx | 47 / 49 (96%) |  |
| CSS 2.1: sec5 | 22 / 23 (96%) |  |
| CSS 2.1: margin-padding-clear | 589 / 683 (86%) |  |
| CSS 2.1: zindex | 25 / 29 (86%) |  |
| CSS 2.1: generated-content | 167 / 202 (83%) |  |
| CSS 2.1: fonts | 127 / 159 (80%) | 1 błąd |
| CSS 2.1: borders | 364 / 504 (72%) |  |
| CSS 2.1: text | 273 / 382 (71%) |  |
| CSS 2.1: visudet | 20 / 28 (71%) |  |
| CSS 2.1: tables | 177 / 249 (71%) |  |
| CSS 2.1: syntax | 163 / 264 (62%) |  |
| CSS 2.1: cascade | 19 / 31 (61%) |  |
| flexbox | 517 / 952 (54%) |  |
| tabele (CSS 3) | 76 / 141 (54%) |  |
| CSS 2.1: normal-flow | 369 / 697 (53%) |  |
| CSS 2.1: floats | 55 / 113 (49%) |  |
| CSS 2.1: abspos | 12 / 25 (48%) |  |
| CSS 2.1: positioning | 219 / 513 (43%) |  |
| CSS 2.1: visuren | 11 / 26 (42%) |  |
| CSS 2.1: css1 | 68 / 164 (41%) | 1 błąd |
| listy (CSS 3) | 61 / 155 (39%) | 1 błąd |
| CSS 2.1: box-display | 32 / 86 (37%) |  |
| CSS 2.1: ui | 19 / 52 (37%) |  |
| CSS 2.1: values | 9 / 25 (36%) | 1 błąd |
| CSS 2.1: floats-clear | 64 / 211 (30%) | 8 przekroczeń czasu |
| treść generowana (CSS 3) | 15 / 51 (29%) |  |
| CSS 2.1: backgrounds | 95 / 336 (28%) | 2 błąd |
| kolumny | 87 / 400 (22%) | 1 panika, 1 panika we wzorcu, 3 przekroczeń czasu |
| CSS 2.1: bidi-text | 21 / 105 (20%) |  |
| grid | 296 / 1593 (19%) |  |
| CSS 2.1: selectors | 78 / 468 (17%) |  |
| CSS 2.1: linebox | 20 / 191 (10%) |  |

Rozdziały z mniej niż 20 testami pominięte w tabeli.

**Historia pomiaru, czyli ile ważyły wspólne przyczyny.** Ta sama miara dawała kolejno:

| Stan | Zaliczone z 9135 |
|---|---|
| pierwszy przebieg | 2490 |
| po zdejmowaniu `<![CDATA[` z arkuszy XHTML | 3042 |
| po usunięciu panik i rozwijaniu `<div/>` w XHTML | 3100 |
| po zmianie rasteryzacji z 40 na 96 dpi | 4295 |
| po zaokrągleniu metryk fontu do całych pikseli | 4829 |
| po naprawie `::first-letter`, poprawkach grid i kolumn | 5221 |
| po włączeniu atrybutów prezentacyjnych HTML (`width`/`height` na `img` i inne) | 5853 |

Ostatni skok to błąd metody, nie silnika: w 40 dpi piksel CSS zajmuje niecałe pół piksela obrazu, więc kształty
o tej samej geometrii różniły się wygładzaniem krawędzi (marginesy: 9% → 86%, `z-index`: 0% → 86%). Wniosek na
przyszłość: zanim niski wynik obszaru uznamy za brak w silniku, obejrzeć kilka niezaliczonych testów.

Zastrzeżenie: przy zmianie rozdzielczości tolerancja (20 różnych pikseli na stronę) została ta sama, choć obraz
ma prawie sześć razy więcej pikseli. Kilka obszarów straciło na tym zaliczenia (treść generowana CSS 3: 31 → 15,
listy CSS 3: 78 → 61). Liczby są więc porównywalne między obszarami tylko w przybliżeniu; tolerancję trzeba
jeszcze dobrać świadomie.

Pomiar pomocniczy z 2026-11-13 (ten sam sondaż, dodatkowa klasa „krawędź”: różnice wyłącznie w pikselach
leżących na krawędziach kształtów): 5853 zaliczone ściśle, 366 kolejnych różni się tylko na krawędziach, 2910
niezaliczonych, 6 błędów renderowania. Najwięcej różnic krawędziowych: tło (72), tekst CSS 2 (52), marginesy
i `clear` (40), pozycjonowanie (39), obramowania (36). To górna granica tego, co da dobranie tolerancji:
6219 z 9135 (68%); reszta to rzeczywiste różnice układu.

Flexbox po rundzie z 2026-11-13 (min/max w kroku 9.7.5d, `wrap-reverse`, `flex-basis: content`): 559 z 952
(wcześniej 553). Pięć testów przestało przechodzić i zostaje do poprawy: linia bazowa kontenera i elementów przy
`wrap-reverse` (`align-baseline`, `flexbox-align-self-baseline-horiz-005`, `flexbox-baseline-multi-line-horiz-002`)
oraz słowo kluczowe `safe` w `align-content` (`flexbox-safe-overflow-position-002` i `-005`), które przedtem
przechodziły przypadkiem, bo linie nie były odbijane.

Słowa kluczowe rozmiaru wewnętrznego (`min-content`, `max-content`, `fit-content` w `width`, `min-width`,
`max-width`; CSS Sizing 3) dodane 2026-11-13. Przed zmianą używało ich 261 testów sondażu, z czego 218 nie
przechodziło. Pełny sondaż po zmianie (razem z poprawką automatycznego minimum flex): 5864 z 9135 (wcześniej
5853); 31 testów zyskanych (flexbox 21, grid 8, listy 2), 20 straconych (grid 12, flexbox 7, kolumny 1).
Stracone testy grid to `subgrid` i `standalone-axis-size`; `subgrid` nie jest obsługiwany, więc wcześniejsze
zaliczenia były najpewniej przypadkowe (nie sprawdzone pojedynczo). Zysk w grid jest mały, bo większość ze 168
niezaliczonych testów grid z tymi słowami dotyczy `subgrid`, układu `grid-lanes` (propozycja, nie norma)
i pionowego trybu pisma, których silnik nie obsługuje; 12 straconych testów to też `subgrid` z pionowym tekstem.

Kolumny po poprawce `column-span: all` z 2026-11-13: 143 z 400 (wcześniej 139), bez regresji. Podział
niezaliczonych: `column-height` i `column-wrap` (31, propozycja CSS Multicol 2), kolumny zagnieżdżone (35),
`column-span` (ok. 95), z czego 14 to wysokości procentowe dzieci liczone od wysokości kontenera kolumn
(nieobsługiwane: pudełko kolumny ma wysokość `auto`). Wiele testów zakłada medium ciągłe, w którym nadmiarowe
kolumny wychodzą w bok; w medium stronicowanym przechodzą na następną stronę, więc tych zaliczeń nie będzie.

**Co zostaje po odjęciu artefaktów pomiaru (2026-11-13).** Z 3271 testów niezaliczonych po odjęciu różnic
krawędziowych, testów z fontem Ahem (błędnie uznanych za pozbawione fontu, zob. niżej), testów wymagających nagłówków HTTP (`@charset`)
oraz funkcji nieobsługiwanych (`subgrid`, `grid-lanes`, pionowy tryb pisma, `column-height`, `column-wrap`)
zostaje 1490: flexbox 299, grid 248, kolumny 168, listy CSS 3 92 (głównie liczniki `reversed`), przepływ
normalny 74, pozycjonowanie 62, tło 57, tabele CSS 2 54, tabele CSS 3 53, elementy pływające 50 i 44.
Trzy sprawdzone grupy z tej reszty też okazały się artefaktami: `background-position` (obwódka z interpolacji
obrazu w rasteryzatorze), `table-anonymous-objects` (czerwony tekst pod zielonym, test sam zastrzega różnice
wygładzania), `absolute-replaced-width` (SVG z przedrostkiem przestrzeni nazw `svg:` w XHTML). Wniosek: sondaż
ekranowy w obecnej postaci przestaje wskazywać błędy silnika; żeby dalej był użyteczny, potrzebna jest decyzja
o foncie Ahem (pytanie 12) i o regule porównania obrazów.

Sprostowanie z 2026-11-14: testy z fontem Ahem nie były pozbawione fontu, bo ładują go przez `@font-face`
z `testdata/wpt/fonts`. Odjęcie ich powyżej było błędem: 610 niezaliczonych testów z Ahem należy do reszty do
przejrzenia, więc zostaje ok. 2100, nie 1490. Reguła porównania została rozstrzygnięta w ADR 0003.

**Stan według reguły z ADR 0003 (pomiar 2026-11-14, sondaż ekranowy, 9135 testów):** zaliczone 6231 (68%), w tym
5868 ściśle i 363 z dokładnością do krawędzi; niezaliczone 2898; błędy renderowania 6. Najwięcej zaliczeń
krawędziowych: tło 72 (205 ściśle z 336), tekst CSS 2 52 (309 z 382), marginesy i `clear` 40 (619 z 683),
pozycjonowanie 39 (405 z 513), obramowania 36 (461 z 504), `css1` 18, treść generowana CSS 3 15. Reftesty druku
(`testdata/wpt`, 340): 163 ściśle, 0 krawędziowych; tam reguła niczego nie zmienia.

Runda z 2026-11-14 (liczniki według CSS Lists 3, atrybuty `ol start`, `ol reversed`, `li value`, znacznik BOM
w HTML): 6325 z 9135 (5959 ściśle, 366 krawędziowo), wcześniej 6231. Zyskane 99 testów: interfejs CSS 2 (27)
i składnia CSS 2 (25) głównie dzięki BOM, flexbox 21, listy 18, fonty 6. Stracone 5: dwa testy wymagające
zasięgu liczników z CSS 2.1 (sprzeczne z nowszymi testami, zob. `THIRD_PARTY.md`) i trzy z funkcją CSS
`reversed()` w `counter-reset`, które przechodziły tylko dlatego, że wzorzec z `ol reversed` był renderowany
tak samo błędnie. Funkcja `reversed()` w `counter-reset` nie jest obsługiwana (ok. 30 testów).

Od 2026-11-14 kontrole w trakcie rundy robimy na ćwiartce testów (`SURVEY_PART=k/4`, co czwarty plik
posortowanej listy); pełny sondaż tylko jako pomiar zamykający etap.

**Jak to czytać.** Reftest przechodzi tylko wtedy, gdy silnik poprawnie renderuje i test, i wzorzec, więc jeden
błąd we wspólnym wzorcu odbiera wiele zaliczeń. Nadal podejrzanie niskie są `linebox` (10%), selektory (16%,
większość testów dotyczy `::first-letter` i `::first-line` ze znakami spoza podanych fontów) i grid (18%).

**Uwaga do tabeli obszarów:** pochodzi sprzed zaokrąglenia metryk fontu. Po nim największe zmiany to: przepływ
normalny 52% → 78%, obramowania 72% → 83%, `linebox` 10% → 30%, `css1` 41% → 57%, tło 28% → 38%, pozycjonowanie
42% → 49%, flexbox 54% → 58%, `clear` 30% → 38%, grid 18% → 20%; 12 testów przestało przechodzić (nieprzejrzane).

Stan obszarów po wszystkich poprawkach do 2026-11-13 (5853 z 9135, 64%; bez panik i zawieszeń, 6 błędów
renderowania): pozycjonowanie 78%, `linebox` 87%, tło 61%, `clear` 74%, przepływ normalny 86%, obramowania 91%,
marginesy 90%, selektory 92%, tekst 80%, tekst dwukierunkowy 57%, flexbox 58%, kolumny 35%, grid właściwy 41%.
Po ostatniej zmianie 18 testów przestało przechodzić (nieprzejrzane).

### Ranking konstrukcji w testach niezaliczonych

Dla 9475 reftestów (sondaż i testy druku) policzono, które konstrukcje CSS w teście albo we wzorcu występują
w niezaliczonych częściej, niż wynika ze średniej (53% niezaliczonych). Największe nadwyżki w chwili pomiaru:
wzorce z elementem `img` (1240 z 1345 niezaliczonych), grid (ok. 700 testów, 91–96%), `::first-letter`
(362 z 364). Pierwsza okazała się skutkiem ułamkowych metryk fontu (poprawione), trzecia błędem przeniesienia
z Pythona (pierwsza litera znikała z dokumentu; poprawione). Grid pozostaje.

### Grid po rozdzieleniu testów (2026-11-13)

Z 1593 testów katalogu `css-grid` 865 dotyczy `grid-lanes` (układ murarski, osobna eksperymentalna specyfikacja),
88 `subgrid`, a część pisma pionowego. Grid właściwy w piśmie poziomym to 562 testy:

| Stan | Zaliczone z 562 |
|---|---|
| przed pracą nad grid | 201 (36%) |
| po poprawkach z tej rundy | 230 (41%) |

Poprawione: kolejność `grid-area`, ujemne numery linii, marginesy `auto`, `justify-self` z podaną szerokością,
rozdział wolnego miejsca między ścieżki (`minmax()`), `auto-fill`/`auto-fit`, `grid-auto-flow: column`. Każda
była sprawdzona parą dokumentów o tym samym oczekiwanym obrazie. Trzy testy przestały przechodzić (nieprzejrzane).
Wg katalogów po poprawkach: elementy siatki 66 z 147, wyrównanie 42 z 136, pozycjonowanie absolutne w siatce
26 z 106, model 29 z 49, algorytm układu 19 z 38, definicja 12 z 35. Największe pozostałe grupy według
konstrukcji: wyrównanie do linii bazowej (nieobsługiwane), elementy pozycjonowane absolutnie względem obszaru
siatki, `inline-grid`.

## 3. Paniki: 658 plików, kilka przyczyn

| Miejsce | Pliki | Co to jest |
|---|---|---|
| zapis fontu w PDF (`pdflib/model/fonts.go:168`, wskaźnik pusty) | 282 | font użyty w dokumencie, ale bez żadnego zapisanego glifu, zostaje pustym obiektem |
| rysowanie (`html/document/draw.go:709`, „unexpected box InlineGridBox”) | 265 | `display: inline-grid` nie ma obsługi rysowania; panika zamiast ostrzeżenia |
| układ grid (`grid.go:526`, `218`, `193`: zakres wycinka) | ok. 90 | ujemne i wychodzące poza zakres indeksy, które w Pythonie są dozwolone |
| układ grid (`grid.go:317`, „expected span”) | 11 | nieobsłużony przypadek rozmieszczenia |
| inne | ok. 10 | |

Trzy pierwsze przyczyny odpowiadają za ponad 95% panik.

## 4. Układ grid: 37 oznaczonych braków

Plik `html/layout/grid.go`, 1599 linii; 23 testy źródłowe przechodzą. Braki wg rodzaju:

| Rodzaj | Liczba | Skutek dziś | Ocena kosztu (szacunek) |
|---|---|---|---|
| funkcje zgłaszane ostrzeżeniem: `subgrid`, `auto-fit`/`auto-fill` w `repeat()`, `grid-auto-flow: column`, wyrównanie do linii bazowej, `inline-grid` | 5 | ostrzeżenie, a dla `inline-grid` panika | `inline-grid` jako blok z ostrzeżeniem: godziny; `auto-fit`/`auto-fill`: 1–2 dni; `subgrid`: tydzień lub więcej |
| dokładność algorytmu rozmiarów ścieżek (ograniczenia min/max-content, rozkład nadmiaru, elementy rozpięte na ścieżkach elastycznych) | ok. 18 | proste siatki (`px`, `%`, `fr`) działają; siatki sterowane zawartością mogą mieć złe szerokości | 1–2 tygodnie na przejście algorytmu ze specyfikacją; ryzyko regresji małe dzięki 1593 reftestom |
| fragmentacja (siatka i jej wiersze nie łamią się między stronami) | 4 | siatka wyższa niż strona wystaje; zawieszenie już naprawione | 3–5 dni |
| wyrównanie (`auto` w marginesach, `safe`/`unsafe`) | 3 | drobne różnice położenia | 1–2 dni |
| pozostałe | 7 | | |

**Przydatność (opinia).** Grid jest dziś podstawowym narzędziem układu w nowych szablonach HTML, także faktur
i raportów pisanych współcześnie; starsze szablony używają tabel i float. Dokument, który ma wyglądać tak samo na
ekranie i w PDF, coraz częściej będzie go zawierał.

**Rekomendacja.** Nie wyłączać. Kolejność: (1) usunąć paniki, czyli `inline-grid` jako blok z ostrzeżeniem
i indeksy wycinków, co jest pracą na jeden dzień i zamienia 383 paniki w wyniki do oceny; (2) powtórzyć sondaż
grid i dopiero wtedy zdecydować o reszcie na podstawie tego, ile testów naprawdę nie przechodzi z powodu algorytmu.

## 5. Pozostałe obszary

| Obszar | Stan | Rekomendacja |
|---|---|---|
| flexbox | 54% reftestów (pomiar w 96 dpi), przekroczenia czasu usunięte, 17 oznaczonych braków, 90 testów źródłowych przechodzi; korpus (12 przypadków) poprawny | poprawić: najpierw przekroczenia czasu (zawieszenia), potem przegląd niezaliczonych |
| `clear` i float | 18% i 38%, 8 przekroczeń czasu | poprawić: zawieszenia najpierw |
| kolumny | 12%, 3 przekroczenia czasu | zostawić, po usunięciu zawieszeń; ocenić po znalezieniu wspólnych przyczyn |
| tabele | 71% (CSS 2.1) i 53% (CSS 3) | poprawić: druga po polach marginesowych luka w reftestach druku |
| selektory | 16%, ale 224 testy blokuje jedna panika | poprawić panikę zapisu fontu; potem ocenić |
| marginesy, `z-index` | 86% po poprawce metody pomiaru | zostawić |
| pozycjonowanie, tło, `linebox` | 42%, 28%, 10% | obejrzeć niezaliczone; możliwe kolejne wspólne przyczyny |
| przypisy dolne, `leader()` | 22 i 15 testów źródłowych przechodzi; brak danych z WPT | zostawić |
| słowniki dzielenia wyrazów (42 języki) | działają | zostawić wszystkie; dodać wybór języków przy kompilacji, gdy rozmiar będzie priorytetem |

## 6. Stan po pierwszej rundzie poprawek (2026-11-13)

Poprawione: zapis fontu bez rozpoznanych glifów, `inline-grid` w rysowaniu, indeksy i dwie odwrócone asercje
w układzie grid, rozwijanie `<div/>` w dokumentach XHTML (`xhtml.go`). Sondaż powtórzony tym samym skryptem:

| | Przed | Po |
|---|---|---|
| zaliczone | 3042 | 3100 |
| paniki (test i wzorzec) | 672 | 2 |
| przekroczenia czasu | 27 | 11 |
| niezaliczone | 5388 | 6016 |

Jedna regresja (test zaliczony wcześniej, teraz nie); nieprzejrzana. Grid po usunięciu panik: 200 z 1593 (13%),
flexbox 389 z 952 (41%). Paniki zamieniły się więc głównie w zwykłe niezaliczenia: przyczyną niskich wyników
grid są braki algorytmu albo kolejna wspólna przyczyna, nie same wyjątki.

### Koszt zagnieżdżenia kontenerów

Pozostałe przekroczenia czasu (kolumny i `clear` w kolumnach) oraz wcześniejsze we flex mają wspólne źródło:
czas układu rośnie wykładniczo z głębokością zagnieżdżenia. Pomiar na `<div>` zagnieżdżonych n razy, jeden znak
treści, macOS arm64:

| Kontener | 6 poziomów | 8 | 10 | 12 |
|---|---|---|---|---|
| flex | 8 ms | 42 ms | 342 ms | ponad 6 s |
| kolumny (`column-count: 2`) | 677 ms | ponad 6 s | | |
| grid | 8 ms | 9 ms | 15 ms | 35 ms |
| tabela, inline-block | 3 ms | 3 ms | 4 ms | 3 ms |

Flex: ok. 3 razy na poziom, kolumny: ok. 7 razy. Każdy poziom układa swoje dzieci kilkukrotnie (szerokości
wewnętrzne, próba, układ właściwy). Naprawa to zapamiętywanie wyniku układu pudełka dla danych wymiarów albo
ograniczenie liczby przejść; praca nad algorytmem, nie poprawka miejscowa. Dla flex to priorytet: 6–8 poziomów
w szablonach złożonych z komponentów jest realne, a 12 poziomów blokuje silnik (wymaganie D3).

### Flex: pierwsze ograniczenie (2026-11-13)

Układ flex układał każde dziecko trzy razy: dla automatycznej wysokości minimalnej, dla wysokości hipotetycznej
i ostatecznie. Pierwsze przejście w kontenerze wierszowym dotyczy osi poprzecznej, gdzie według CSS Flexbox 1
§ 4.5 automatyczne minimum wynosi zero; zostało ograniczone do osi głównej i do elementów zastępowanych.

| Poziomy flex | 8 | 10 | 12 | 14 | 16 |
|---|---|---|---|---|---|
| przed | 42 ms | 342 ms | ponad 6 s | | |
| po | 14 ms | 16 ms | 35 ms | 119 ms | 457 ms |

Koszt nadal rośnie ok. 2 razy na poziom (dwa pozostałe przejścia), więc problem jest odsunięty, nie usunięty.
Bez regresji: 90 testów źródłowych flex i 952 reftesty flexbox (390 zaliczonych, o jeden więcej). Pilnuje tego
`TestNestedFlex`. Kolumny nie były ruszane (6 poziomów: 677 ms).

## 7. Proponowana kolejność

1. ~~Trzy przyczyny panik z pkt 3~~ zrobione (pkt 6).
2. Koszt zagnieżdżenia flex i kolumn (pkt 6); z 27 przekroczeń czasu zostało 11, wszystkie w kolumnach.
3. Powtórka sondażu i szukanie kolejnych wspólnych przyczyn (jak CDATA), zaczynając od obszarów 0–21%.
4. Dopiero potem decyzje o dokańczaniu algorytmów (grid, flex, tabele) na podstawie oczyszczonych liczb.

## Porównanie korpusu z Chromium (od 2026-11-14)

Miara pomocnicza: dla każdego dokumentu z `testdata/documents` dopasowujemy słowa (strona, tekst) naszego PDF
do wydruku referencyjnego z Chromium i liczymy, ile leży w tym samym miejscu z dokładnością do 1,5 pt. Pierwszy
pomiar ujawnił dwa błędy, których reftesty W3C nie pokazują, bo test i wzorzec renderuje ten sam silnik:
rozmiar fontu zaokrąglany w górę do całych pikseli (tekst 10 pt o 5% za szeroki) i pola formularza jako bloki.
Po poprawkach np. `03-report-svg-chart` ma 127 słów zgodnych na 141 (wcześniej 64). Pozostałe różnice to
głównie przesunięcia w pionie o 1–8 pt (do zbadania: metryki wiersza, odstępy tabel) i łamanie wierszy.

## Pomiar zamykający etap, 2026-11-14

Pełny sondaż ekranowy (9135 testów, reguła z ADR 0003): **6460 zaliczonych (70,7%)**, w tym 6050 ściśle
i 410 z dokładnością do krawędzi; 2675 niezaliczonych; 0 błędów renderowania. Poprzedni pełny pomiar: 6325.
Od niego 137 testów zyskanych (tło 41, model pudełkowy 23, pozycjonowanie 21, przepływ normalny 13, `css1` 12)
i 2 stracone, oba naprawione po pomiarze (`max-height` przy podziale bloku, `overflow: clip` w elemencie flex).

Stan obszarów (zaliczone z regułą krawędziową / wszystkie): tło 318/336, pozycjonowanie 466/513, przepływ
normalny 629/697, wiersz 179/191, tekst CSS 2 362/382, tabele CSS 2 186/249, model pudełkowy 64/86, elementy
pływające 64/113, flexbox 598/952, tabele CSS 3 84/141, kolumny 151/400, listy CSS 3 74/155, treść generowana
CSS 3 31/51, grid 354/1593.

Reftesty druku (`testdata/wpt`, 340): 173 zaliczone. Binarka `cmd/gopdfgo`: 21,5 MB (macOS arm64, go1.26,
`CGO_ENABLED=0 -trimpath -ldflags='-s -w'`, pomiar 2026-11-14).

## Treść dłuższa niż strona (2026-11-14)

Stały test `content_loss_test.go` sprawdza 21 rodzajów kontenerów: osiem elementów i akapit końcowy muszą
leżeć w obrębie którejś strony. Powstał po tym, jak okazało się, że przejęty kod układał treść kontenerów
flex i grid poniżej dolnej krawędzi strony (ginęła także przy `body { display: flex }`); tabele, kolumny,
elementy pływające i bloki dzieliły się poprawnie. Od 2026-11-14 test obejmuje 24 przypadki, w tym treść
wylewającą się z bloku o stałej wysokości oraz `inline-block` i `inline-flex` wyższe niż strona (krojone
między strony). Gdy treść mimo wszystko zostanie w całości poniżej strony, silnik zgłasza ostrzeżenie; znany
przypadek to pudełko pozycjonowane bezwzględnie w całości poniżej strony.

Pełny sondaż po rundach stronicowania flex, grid i treści wylewającej się (2026-11-14, wieczór): 6468 z 9135
(6059 ściśle, 409 krawędziowo), 0 błędów renderowania; poprzedni pełny pomiar 6460. Jedna strata, w układzie
`grid-lanes`, którego silnik nie obsługuje. Reftesty druku: 179 z 340.

Dokument korpusu `19-flex-grid-pagination` (dodany 2026-11-14): pięć sekcji z flex i grid przechodzącymi przez
granice stron. Po poprawkach ma 13 stron jak w Chromium i ten sam rozkład treści na stronach; jedyna różnica
to karty o stałej wysokości na granicy strony, które Chromium przecina, a gopdfgo przenosi w całości (ADR 0004).

Dokumenty `20-invoice` (faktura, 3 strony, tabela 46 pozycji z powtarzanym nagłówkiem) i `21-mixed-report`
(obrazy opływane tekstem, listy zagnieżdżone, komórki scalone, kolumny; 3 strony) dodane 2026-11-14 zgadzają się
z Chromium od pierwszego renderowania: ta sama liczba stron i ta sama treść na każdej; w `21-mixed-report`
wszystkie 28 pasów tuszu pierwszej strony leżą w tych samych miejscach z dokładnością do 2 pikseli (96 dpi).

Pełny sondaż po rundach krojenia wierszy, kontynuacji elementów pływających i flex (2026-11-14, noc): bez
zmian w zaliczeniach względem poprzedniego pełnego pomiaru (6468 z 9135; 0 straconych, 0 zyskanych), ale
jeden wzorzec przekroczył limit czasu: krojenie wiersza o nieskończonej wysokości nie miało końca. Naprawione
tego samego dnia (`TestInfiniteHeightDoesNotLoop`); po poprawce plik renderuje się w 0,4 s.
Reftesty druku: 180 z 340. Korpus: 21 dokumentów, z czego trzy nowe porównane z Chromium.

Pełny sondaż po przeniesieniu pudełek pozycjonowanych poniżej strony i po poprawce krojenia (2026-11-14,
ostatni pomiar dnia): 6468 z 9135 (6059 ściśle, 409 krawędziowo), 0 straconych, 0 błędów renderowania
i 0 przekroczeń limitu czasu. Reftesty druku: 183 z 340.

## Czas i pamięć dużych dokumentów (2026-11-14)

Pomiar: `go test -run xxx -bench LargeTable -benchtime 5x .` (`benchmark_test.go`, tabela 4000 wierszy
i 7 kolumn, ok. 32 tys. elementów, 130 stron A4; macOS arm64, 10 rdzeni, go1.26.6). Liczby to średnia z pięciu
przebiegów; „pamięć przydzielona” to suma wszystkich przydziałów w trakcie renderowania, nie szczyt zajętości.

| stan | czas | pamięć przydzielona | przydziały |
|---|---|---|---|
| przed (commit `4320f84`) | 1,46 s | 2239 MB | 9,93 mln |
| po współdzieleniu kaskady i odchudzeniu stylów anonimowych | 1,32 s | 1676 MB | 9,36 mln |

Czas nie jest problemem (ok. 10 ms na stronę). Pamięć jest: ok. 0,4 MB przydziałów na wiersz tabeli, a szczyt zajętości
procesu dla tego dokumentu to ponad 1 GB (pomiar wstępny narzędziem wiersza poleceń przed zmianą: 2,0 GB RSS).
Profil przed zmianą wskazywał style: 38% przydziałów to tablice wartości obliczonych (`propsCache.setKnown`),
24% to mapy kaskady budowane osobno dla każdego elementu. Po zmianie mapy kaskady są wspólne dla elementów
o tym samym ciągu dopasowanych reguł, a style pudełek anonimowych nie trzymają własnej tablicy wartości.
Największą pozostałą pozycją są tablice wartości obliczonych elementów (ok. 37% przydziałów). Próba przydzielania
ich od razu w pełnym rozmiarze pogorszyła wynik (1756 MB wobec 1605 MB w pomiarze `runtime.MemStats`), bo
większość stylów czyta tylko właściwości o niskich numerach; zmiana została cofnięta. Dalszy zysk wymagałby
współdzielenia całych stylów obliczonych między elementami o tej samej kaskadzie i tym samym rodzicu, co
koliduje z tym, że układ modyfikuje style w miejscu.

Pełny sondaż ekranowy po tej zmianie i po rundzie legendy `fieldset`: 6468 z 9135 (6059 ściśle, 409 krawędziowo),
0 straconych, 0 zyskanych, 0 błędów renderowania i przekroczeń limitu czasu. Reftesty druku: 183 z 340.
