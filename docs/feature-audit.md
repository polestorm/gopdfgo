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

Dokument pomiarowy (`benchmark_test.go`): tabela 4000 wierszy i 7 kolumn z krótkim tekstem, obramowaniem
i co drugim wierszem z tłem; ok. 32 tys. elementów, 130 stron A4. To zwykły długi wykaz, nie przypadek
skrajny. Maszyna: macOS arm64, 10 rdzeni, go1.26.6.

Miary: czas i pamięć przydzielona (suma wszystkich przydziałów) z `go test -run xxx -bench LargeTable
-benchtime 5x .`; szczyt żywej sterty z próbnika, który co 25 ms wymusza odśmiecanie i czyta
`runtime.MemStats.HeapAlloc` (ten sam, którego używa `TestLargeTablePeakMemory`).

| stan | czas | pamięć przydzielona | szczyt żywej sterty |
|---|---|---|---|
| przejęty kod (commit `4320f84`) | 1,46 s | 2239 MB | nie mierzony |
| wspólna kaskada, lżejsze style anonimowe (`82e8b6b`) | 1,32 s | 1676 MB | 718 MB |
| style pseudoelementów na żądanie | nie mierzony | nie mierzona | 483 MB |
| wspólne tablice wartości obliczonych | 1,06 s | 1239 MB | 323 MB |

Co zajmowało pamięć (profil żywej sterty w szczycie, stan `82e8b6b`): ponad połowę style obliczone. Reguły
arkusza domyślnego `::marker`, `::footnote-call` i `::footnote-marker` nie mają selektora elementu, więc
pasują do wszystkiego, a przejęty kod obliczał od razu styl każdego pseudoelementu z kaskadą: cztery pełne
style na element, każdy z tablicą na 186 właściwości (ok. 3 kB). Teraz styl pseudoelementu powstaje przy
pierwszym pytaniu, a elementy o tej samej kaskadzie i tak samo wyliczonym rodzicu dzielą jedną tablicę
wartości; komórki tej tabeli mają cztery takie tablice zamiast 28 tysięcy. Wartości właściwe jednemu
elementowi (`attr()`, kotwice, język, zmiany wprowadzane przez układ) leżą w małej nakładce stylu.

Po zmianach zostaje ok. 80 kB żywej sterty na wiersz tabeli (ok. 11 kB na komórkę) i prawie wszystko to
pudełka: drzewo wejściowe i jego ułożona kopia istnieją jednocześnie dla całego dokumentu (kopie pudełek
ok. 105 MB, pudełka tekstowe i liniowe ok. 60 MB, wyniki kształtowania tekstu ok. 40 MB, strumienie stron
przed zapisem ok. 30 MB; szacunki z profilu). Dalsze zejście wymaga zmiany przepływu, a nie stylów:
zwalniania ułożonych stron po narysowaniu i poddrzew wejściowych po ułożeniu. Przeszkodą jest to, że
`counter(pages)` i odwołania `target-counter` każą dziś układać dokument ponownie od początku, więc drzewo
wejściowe musi przetrwać do końca. To osobna decyzja (pytanie w `docs/adr/open-questions.md`).

Nieudana próba: przydzielanie tablicy wartości od razu w pełnym rozmiarze zwiększyło sumę przydziałów
(1756 MB wobec 1605 MB w `runtime.MemStats.TotalAlloc`), więc zostało cofnięte.

Pełny sondaż ekranowy po zmianach w stylach (2026-11-14): 6468 z 9135 (6059 ściśle, 409 krawędziowo),
0 straconych, 0 zyskanych, 0 błędów renderowania i przekroczeń limitu czasu. Reftesty druku: 183 z 340.

## Runda składni, selektorów i kaskady (2026-11-14)

Pełny sondaż ekranowy: **6537 z 9135 (71,6%)**, w tym 6126 ściśle i 411 krawędziowo; poprzednio 6468.
Zyskanych 75 (składnia CSS 2: 47, selektory 10, przepływ normalny 8, kaskada 4, treść generowana 2, listy 2,
kolory 1, `css1` 1), straconych 6, 0 błędów renderowania i przekroczeń limitu czasu. Po pomiarze naprawiony
jeszcze `text-indent-intrinsic-001` (sprawdzony osobno; ćwiartka sondażu po tej poprawce bez zmian).
Reftesty druku: 183 z 340.

Straty nie są regresjami układu, tylko testami, które wcześniej przechodziły przez błąd:

- `at-import-004`, `media-dependency-008`: `@import url("…") print` nie ma działać na ekranie; silnik renderuje
  dla mediów drukowanych, więc arkusz stosuje. Dotąd ta postać `@import` była w ogóle pomijana;
- `text-indent-intrinsic-001`, `-003`, `-004`, `block-in-inline-whitespace-001a`: arkusz tych testów ma selektor
  zapisany jako `body &gt; div`. Przed rozwijaniem odwołań znakowych w `<style>` reguła odpadała zarówno w teście,
  jak i we wzorcu, więc obrazy były równe. Teraz reguła działa i widać rzeczywiste różnice (`-001` naprawiony,
  w `-003` i `-004` zostaje szerokość minimalna przy ujemnym wcięciu i znaku U+200B).

Najważniejsze poprawki z punktu widzenia zwykłych dokumentów (szczegóły w `THIRD_PARTY.md`):

- błędna wartość koloru czyniła tekst niewidocznym zamiast unieważniać deklarację;
- `style="…"` przegrywało z regułą `#id { … }`;
- `@import url("arkusz.css")` nie wczytywało arkusza;
- arkusze w kodowaniu innym niż UTF-8 (znacznik kolejności bajtów, `@charset`) były czytane jako UTF-8;
- spacja po znaku z fontu zastępczego miała szerokość z tego fontu.

Pozostałe niezaliczone w `CSS2/syntax` (28): 17 wymaga nagłówków HTTP albo kodowania dokumentu, reszta to
wielkość liter w XHTML, `declarations-009` (różnica między CSS 2.1 a CSS Syntax 3) i niedomknięte konstrukcje
na końcu arkusza. W `CSS2/selectors` zostaje głównie `::first-line`, którego silnik nie stosuje (13 testów).

Pełny sondaż po rundzie tabel (2026-11-14, `min/max-width`, `min/max-height`, tło grupy wierszy, wcięcie
w szerokości minimalnej): **6576 z 9135 (72,0%)**, w tym 6165 ściśle i 411 krawędziowo; 39 zyskanych (tło 15,
przepływ normalny 11, tabele CSS 2: 5, tekst dwukierunkowy 3, `clear` 3, tekst 1, tabele CSS 3: 1),
0 straconych, 0 błędów renderowania i przekroczeń limitu czasu. Reftesty druku: 183 z 340.

## Runda tabel i pozycjonowania (2026-11-14)

Pełny sondaż ekranowy: **6636 z 9135 (72,6%)**, w tym 6225 ściśle i 411 krawędziowo; poprzednio 6576.
Zyskanych 65 (tabele CSS 2: 41, pozycjonowanie 20, tabele CSS 3: 2, `abspos` 1, kolumny 1), straconych 5,
0 błędów renderowania i przekroczeń limitu czasu. Reftesty druku: 183 z 340.

Z pięciu strat dwie (`static-inside-float-inside-inline`, `margin-collapse-quirks-mode-001`) wynikały
z kolejności rysowania pudełek wyprowadzonych spod przycięcia i zostały naprawione po pomiarze (sprawdzone
osobno; ćwiartka sondażu po poprawce bez zmian). Trzy pozostałe to `css-tables/absolute-tables-008`, `-009`
i `-011`, oznaczone `tentative`: oczekują, że tabela pozycjonowana bezwzględnie ma do dyspozycji całą
szerokość bryły zawierającej niezależnie od `left` i `right`. CSS 2.1 § 10.3.7 mówi inaczej i na tej regule
opiera się 32 testów `table-anonymous-objects`, więc zostajemy przy normie.

Co doszło: szerokość tabeli pozycjonowanej bezwzględnie, procenty kolumn w układzie stałym, `visibility:
collapse` wierszy, `min-height`/`max-height` i pojedynczy margines automatyczny pudełek pozycjonowanych,
przycinanie przez `overflow` tylko tych pudełek pozycjonowanych, dla których element jest bryłą zawierającą.
Nie zrobione: `visibility: collapse` kolumn, `iframe`, wysokość elementów zastępowanych zależna od metryk
fontu wzorca (testy `*-replaced-height-002`: różnica 2 px wynika z fontu zastępującego Times).

Flex, procenty przy nieokreślonym rozmiarze głównym (2026-11-14): podzbiór `css-flexbox` i `css-grid`
(2545 testów) po zmianie daje 3 testy zyskane i 0 straconych. Ważniejszy od liczby jest skutek uboczny,
który zniknął: wysokość procentowa i procentowe `flex-basis` w kolumnie bez podanej wysokości zapisywały
w PDF wartości `NaN` i `Inf` (czytniki zgłaszały błąd składni strumienia). Pełnego sondażu po tej zmianie
nie było; ostatni pełny wynik to 6636 z 9135.

Pełny sondaż po rundzie flex (2026-11-14, procenty przy nieokreślonym rozmiarze, określona wysokość po
rozciągnięciu, wyrównania zastępcze `justify-content`): **6655 z 9135 (72,9%)**, w tym 6244 ściśle
i 411 krawędziowo; poprzednio 6636. Zyskanych 19 (flexbox 17 oraz dwa testy naprawione po poprzednim
pomiarze), 0 straconych, 0 błędów renderowania i przekroczeń limitu czasu. Reftesty druku: 183 z 340.

Elementy pływające (2026-11-14): podzbiór przepływu CSS 2 (`floats`, `floats-clear`, `normal-flow`,
`margin-padding-clear`, `visuren`, `box-display`, `linebox`, `text`; 2389 testów) po dwóch poprawkach daje
6 testów zyskanych i 0 straconych. Pełnego sondażu po nich nie było; ostatni pełny wynik to 6655 z 9135.
Pozostałe niezaliczone w `floats` i `floats-clear` (ok. 65) to głównie odstęp `clear` przy składaniu
marginesów, ujemne i bardzo duże marginesy obok elementów pływających oraz kolejność rysowania.

## Reftesty druku: znacznik `reftest-pages` i warstwy kaskady (2026-11-14)

Reftesty druku (`testdata/wpt`, 340): **192 zaliczone** (wszystkie ściśle), poprzednio 183.

- Pięć testów doszło po tym, jak harness zaczął uwzględniać `<meta name="reftest-pages">` (porównanie tylko
  wskazanych stron testu; konwencja WPT). To poprawka pomiaru, nie silnika: dziewięć testów ma ten znacznik,
  z czego trzy `page-orientation` nadal nie przechodzą (właściwość nieobsługiwana), a jeden przechodził
  już wcześniej.
- Cztery testy `layers-00x` doszły dzięki obsłudze `@layer`; wcześniej reguły w warstwach były pomijane
  w całości, co w zwykłym dokumencie oznaczało utratę wszystkich stylów zapisanych w warstwach.

Przegląd pozostałych 148: podział tabel (ok. 30) to pojedyncze przypadki brzegowe (tabela z samym nagłówkiem
albo stopką, wymuszony podział po `thead`), pola marginesowe (ok. 22) w większości używają `width` i `height`
w regule `@page`, których silnik nie zna, a część testów `page-size` i `page-rule-specificity` zakłada, że
`size: landscape` obraca rozmiar papieru urządzenia (5 × 3 cala w harnessie WPT), podczas gdy silnik obraca
swój rozmiar domyślny.

## Grid: co naprawdę zostało (2026-11-14)

Z 1240 niezaliczonych testów `css-grid` w ostatnim pełnym sondażu 735 leży w `grid-lanes` (układ murowany
z roboczego szkicu CSS Grid 3, `display: grid-lanes`), a 75 w `subgrid`. Żadnego z nich silnik nie obsługuje
i nie są to części CSS Grid 1 ani 2, które dotyczą zwykłych dokumentów. „Zwykły” grid to ok. 390
niezaliczonych: pozycjonowanie bezwzględne w siatce 110, elementy siatki 88, wyrównanie 109 (z tego linie
bazowe 42), definicja siatki 24, algorytm układu 18, model siatki 18, pozostałe 24.

Po obsłudze `z-index` elementów flex i grid oraz zwijania pustych ścieżek `auto-fit` podzbiór `css-flexbox`,
`css-grid`, `CSS2/zindex` i `CSS2/positioning` (3087 testów) daje 43 testy zyskane (grid 40, flexbox 3)
i 0 straconych. Pełnego sondażu po tej zmianie nie było; ostatni pełny wynik to 6655 z 9135.

Pełny sondaż po rundach elementów pływających, `@layer`, gridu i odstępów (2026-11-14): **6724 z 9135
(73,6%)**, w tym 6314 ściśle i 410 krawędziowo; poprzednio 6655. Zyskanych 70 (grid 59, elementy pływające 6,
flexbox 4, kolumny 1), stracony 1 (`grid-lanes-contain-intrinsic-size-row-002`: układ `grid-lanes` nie jest
obsługiwany, test przechodził przypadkiem), 0 błędów renderowania i przekroczeń limitu czasu.
Reftesty druku: 192 z 340.

Listy CSS 3 (2026-11-14): po obsłudze liczników odwróconych (`counter-reset: reversed(…)`, wartość początkowa
z zasięgu licznika, domyślny przyrost −1) podzbiór `css-lists`, `css-content`, `CSS2/generated-content`,
`CSS2/lists`, `CSS2/css1` i `CSS2/box-display` (811 testów) daje 38 testów zyskanych i 0 straconych; w samym
`css-lists` zostało ok. 41 niezaliczonych ze 155. Pełnego sondażu po tej zmianie nie było; ostatni pełny
wynik to 6724 z 9135.

Pełny sondaż po rundzie list (2026-11-14): **6762 z 9135 (74,0%)**, w tym 6352 ściśle i 410 krawędziowo;
poprzednio 6724. Zyskanych 38 (wszystkie w `css-lists`), 0 straconych, 0 błędów renderowania i przekroczeń
limitu czasu. Reftesty druku: 192 z 340.

Kolumny (`css-multicol`, ok. 250 niezaliczonych z 400) po przeglądzie: 29 testów dotyczy nowej właściwości
`column-height` (CSS Multicol 2), ok. 25 zagnieżdżonych układów wielołamowych, ok. 45 elementów rozpinających
(`column-span: all`) w nietypowych położeniach (w przekształceniach, z ujemnymi marginesami, dzielonych między
kolumny), reszta to tryby pisma pionowego i pojedyncze przypadki. Napraw tam nie zaczynano: żadna z tych grup
nie odpowiada typowemu dokumentowi wielołamowemu (tekst w kilku łamach z nagłówkiem rozpinającym).

## Korpus po zmianach z 2026-11-14 oraz rozmiar kodu

**Korpus.** 21 dokumentów z `testdata/documents` wyrenderowano binarką ze stanu `4320f84` (początek prac
tego dnia nad pamięcią i testami W3C) i ze stanu `1cd9e2c`, po czym porównano strona po stronie w 96 dpi
(próg 40 na kanał) oraz ze słowami z wydruków Chromium (`pdftotext -bbox`). Liczba stron nie zmieniła się
w żadnym dokumencie, liczba słów na innej stronie niż w Chromium i liczba słów przesuniętych o ponad 3 pt
jest identyczna dla obu stanów we wszystkich 21 dokumentach. W 17 dokumentach obraz jest identyczny co do
piksela. W czterech (`09`, `13`, `16`, `21`) zmieniło się od 56 do 1557 pikseli na pierwszej stronie i we
wszystkich jest to ta sama, zamierzona zmiana: spacja po znaku złożonym fontem zastępczym (znacznik listy,
„▾” w `summary`, „→ ≤ ≥”) ma szerokość z fontu podstawowego, tak jak w Chromium.

**Rozmiar źródeł** (pliki `.go` w repozytorium, `wc -l`, 2026-11-14, stan `1cd9e2c`):

| część | wiersze | pliki | zmiana 2026-11-14 |
|---|---|---|---|
| kod przejęty (`internal/`, bez testów) | 70 921 | 150 | +2508 / −304 |
| testy przejęte | 44 788 | 113 | +155 / −47 |
| kod własny (korzeń, `cmd`, `tools`) | 528 | 6 | +80 / −1 |
| testy własne | 2 565 | 11 | +1666 / −8 |
| razem | 118 802 | 280 | |

**Ile zostało ze źródeł** (porównanie plik po pliku z czystymi kopiami: `bench/webrender`,
`bench/go-weasyprint`, `pdf@v0.0.15` z pamięci modułów; `git diff --no-index --numstat -w`, zmiany samych
odstępów pominięte; wiersze importów przepisane na nową ścieżkę modułu liczą się jako zmienione):

| pakiet | wiersze kodu | wspólne ze źródłem | dodane przez nas | usunięte ze źródła |
|---|---|---|---|---|
| `internal/webrender` | 54 183 | 50 239 (92,7%) | 3 944 | 699 |
| `internal/goweasyprint` | 1 486 | 1 400 (94,2%) | 86 | 61 |
| `internal/pdflib` | 15 252 | 15 032 (98,6%) | 220 | 94 |
| razem kod przejęty | 70 921 | 66 671 (94,0%) | 4 250 | 854 |

Z biblioteki `pdf` przejęto 38 z 105 plików kodu (pominięte 67 plików, ok. 95 tys. wierszy: czytanie PDF,
formularze, szyfrowanie i inne części niepotrzebne do zapisu). Kod produkcyjny napisany w projekcie to
zatem ok. 4 780 wierszy (4 250 w pakietach przejętych i 528 własnych), czyli ok. 6,7% z 71 449 wierszy kodu
bez testów. W testach przejętych zmieniono lub dodano ok. 414 wierszy.

**Rozmiar binarki** `cmd/gopdfgo` (macOS arm64, go1.26.6, `CGO_ENABLED=0 -trimpath -ldflags='-s -w'`):
21,60 MB; ze stanu `4320f84` 21,52 MB (+0,08 MB). Bez usuwania symboli: 28,16 MB.

## Położenie znaków w zapisanym tekście (2026-11-15)

Pełny sondaż ekranowy: **6787 z 9135 (74,3%)**, w tym 6404 ściśle i 383 krawędziowo; poprzednio 6762.
Zyskanych 30 (treść generowana 9, `box-display` 7, listy 6, elementy pływające 3, pozycjonowanie 2,
`css-content` 2, tabele 1), straconych 5, 0 błędów renderowania i przekroczeń limitu czasu. Wynik powtórzony
w dwóch przebiegach bez żadnej różnicy.

Przyczyna była jedna: korekty odstępów między znakami zapisywane w PDF były obcinane w tę samą stronę przy
każdym znaku, więc narysowany tekst rozjeżdżał się z szerokością wyznaczoną w układzie o ok. 0,03 px na
kilkanaście znaków. Układ był poprawny, ale wszystko, co stało za dłuższym tekstem (drugi element w wierszu,
treść `::after`, komórka tabeli), miało inaczej wygładzone krawędzie niż ten sam tekst zapisany jednym
ciągiem. Liczba testów zaliczonych ściśle wzrosła o 52, a zaliczonych tylko z dokładnością do krawędzi
spadła o 27.

Pięć strat: cztery testy `CSS2/css1` z fontem Ahem, w których krawędź znaku leży dokładnie na granicy piksela
i przesunięcie początku tekstu o ułamek jednostki zmienia wygładzanie (wcześniej zaliczane krawędziowo), oraz
`content-056` (21 różnych pikseli przy progu 20). Sprawdzone warianty i ich wyniki są w `THIRD_PARTY.md`.

W korpusie 21 dokumentów zmiana nie przesunęła żadnego słowa względem Chromium ani nie zmieniła liczby stron;
zmienia się wygładzanie krawędzi znaków na każdej stronie. Przy okazji naprawiono kolumnę procentową tabeli
przy komórce z `colspan` (osobny wpis w `THIRD_PARTY.md`).

## Druga runda podstaw CSS 2: marginesy, pudełka liniowe, `th`, listy (2026-11-15)

Pełny sondaż ekranowy: **6852 z 9135 (75,0%)**, w tym 6470 ściśle i 382 krawędziowo; poprzednio 6787
(6404 i 383). Zyskanych 66 (`CSS2/normal-flow` 15, `css1` 11, `floats-clear` 9, `css-tables` 7, pozostałe po kilka),
stracony 1, 0 błędów renderowania i przekroczeń limitu czasu. Reftesty druku
bez zmian: 192 z 340 (wszystkie ściśle). Pomiar: `tools/survey96e.py` poza repozytorium, 9135 reftestów WPT
z obszarów `CSS2`, `css-content`, `css-flexbox`, `css-grid`, `css-lists`, `css-multicol`, `css-tables`, strona
800×600 px, reguła z ADR 0003.

Poprawki, które dotyczą też zwykłych dokumentów do druku:

- **`th` nie było środkowane.** Komórki nagłówkowe tabel wychodziły wyrównane do lewej. Teraz są wyśrodkowane,
  a gdy wyżej ustawiono `text-align`, dziedziczą je, tak jak w przeglądarkach (HTML, Rendering § 15.3.9).
- **Lewy odstęp elementu liniowego.** Tekst w `<span>` z `padding-left`, `border-left` albo `margin-left`
  wystawał w pierwszym wierszu poza blok o szerokość tego odstępu; odstęp znikał też całkiem, gdy tekst
  elementu zaczynał się od spacji.
- **Element liniowy z blokiem w środku** rysował lewe i prawe obramowanie przy każdym fragmencie, a pusty
  fragment przed blokiem zajmował cały wiersz.
- **`min-height` i `max-height`:** procentowa wysokość dziecka liczy się od wysokości po ograniczeniu;
  szerokość obrazu z podaną wysokością wynika z wysokości po ograniczeniu; dolny margines ostatniego dziecka
  nie wychodzi poza pudełko, którego wysokość zmieniło ograniczenie (zob. pytanie 18).
- **Marginesy:** pusty blok z pustymi dziećmi przepuszcza marginesy; marginesy dzieci zostają w podpisie tabeli;
  marginesy pustego elementu z `clear` zwijają się według CSS 2 § 8.3.1; tabela bez wierszy ma zerową wysokość.
- **Listy:** `counter-increment` na `li` (także `none`) nie wyłącza już numeracji; `<ol reversed>` z własnym
  `counter-reset` liczy w górę; `counter(inherit)` i podobne nazwy są odrzucane.
- **Skróty logiczne obramowań:** `border-inline`, `border-block` oraz ich warianty `-width`, `-style`, `-color`.
- **Położenie znaków:** początek tekstu i korekty odstępów zaokrąglane w jedną stronę, dzięki czemu znaki
  stojące na granicy piksela nie zostawiają szczelin (wróciły cztery testy `CSS2/css1` stracone w poprzedniej
  rundzie).

Strata: `margin-collapse-038`, test z 2010 r. sprzeczny z czterema nowszymi testami zyskanymi w tej rundzie
(pytanie 18 w `docs/adr/open-questions.md`).

Korpus 21 dokumentów (`tools/corpuscmp.py`, binarka z 2026-11-14 wobec bieżącej): liczba stron bez zmian
w każdym dokumencie; liczba słów przesuniętych o ponad 3 pt względem wydruku Chromium spadła w `17-tables`
z 2 do 0 i w `21-mixed-report` z 4 do 2, w pozostałych bez zmian; łączny rozmiar PDF 3 271 726 → 3 272 939
bajtów (+0,04%).

Rozpoznane i odłożone: kolumny zadeklarowane przez `<col>` ponad liczbę komórek w wierszu
(`col-definite-size-001`), `calc()` z procentem w szerokości kolumny, pozycja statyczna elementu
pozycjonowanego przy `direction: rtl`, ujemny clearance (`negative-clearance-*`), znaczniki pseudoelementów
`::before` z `display: list-item`, zasięg licznika na następne rodzeństwo (`counter-list-item-2`, sprzeczny
z bieżącym tekstem CSS Lists 3 § 4.3). Część testów `CSS2` zależy od metryk fontu: wzorce zakładają wiersz
niższy niż 20 px przy foncie 16 px (Times), a Noto Serif z `testdata/fonts` daje 21,8 px
(np. `inlines-017`, `block-replaced-height-002`, `c5509-ipadn-l-003`); to cecha środowiska pomiaru, nie silnika.

## Tabele: kolumny z `<col>`, za wysoki nagłówek; font Tinos w testach (2026-11-15)

Pełny sondaż ekranowy: **6855 z 9135 (75,0%)**, w tym 6473 ściśle i 382 krawędziowo; poprzednio 6852
(6470 i 382); zyskane 3, stracone 0. Reftesty druku: **194 z 340** (wszystkie ściśle), poprzednio 192. Sposób pomiaru jak w poprzedniej
sekcji.

- **Nagłówek albo stopka tabeli wyższe niż strona znikały z dokumentu.** `thead` i `tfoot` są powtarzane na
  każdej stronie, a grupa, która nie mieściła się na pustej stronie, była po cichu pomijana, także wtedy, gdy
  tabela miała tylko nagłówek. Teraz taka grupa płynie jak zwykłe wiersze i jest dzielona między strony.
  Trzy przypadki doszły do `TestNoContentLostAcrossPages`.
- **Kolumny zadeklarowane przez `<col>`** o szerokości podanej długością istnieją także bez komórek,
  a `calc()` łączące procent z długością liczy się w szerokościach tabeli jak `auto` (trzy testy
  `col-definite-*`).

**Tinos.** Do repozytorium doszedł Tinos 1.340 (cztery odmiany, OFL-1.1; metryki Times New Roman),
w `testdata/fonts/tinos/`, zarejestrowany w `testdata/THIRD_PARTY.md`. Leży w podkatalogu, bo testy i pomiary
czytają tylko pliki z samego `testdata/fonts`, a Tinos położony obok fontów Noto zmieniał wyniki mimo tego,
że rodzina `serif` nadal wybierała Noto Serif: stawał się fontem zastępczym dla znaków spoza Noto Sans
(pięć testów sondażu mniej; w korpusie cztery dokumenty z innym fontem zastępczym i PDF większe o 0,6%,
bez zmiany liczby stron i położenia słów). Stan z tego akapitu obowiązywał kilka godzin: po decyzji
użytkownika fonty z podkatalogów są wczytywane zawsze, a ich rolę wyznacza `families.json` (następna sekcja).

Pełny sondaż z Tinos jako fontem domyślnym (arkusz użytkownika `html { font-family: Tinos }`, stan sprzed
poprawek tabel z tej sekcji) dał 6862 wobec 6852 z Noto Serif: 30 testów zyskanych, 20 straconych. To mniej,
niż sugerowały pojedyncze przypadki: testy CSS 2 nie są pisane pod jeden font, część przechodzi tylko z fontem
szerszym niż Times, a część strat wynika z samego sposobu ustawienia fontu. Sposób, w jaki silnik ma wybierać
font dla `serif`, jest pytaniem 19 w `docs/adr/open-questions.md`.

Rozpoznane i odłożone w podziale tabel: `rowgroup-page-break-inside-avoid-4`, `-8`
i `row-page-break-inside-avoid-2` (wzorce zakładają zachowanie jednej przeglądarki przy `break-inside: avoid`
grupy wyższej niż strona), rodzina `table-fragmentation-00x` (obramowania i odstępy tabeli przy podziale).

## Fonty dla rodzin generycznych i przełącznik marginesów (2026-11-15)

Dwie decyzje użytkownika z 2026-11-15, obie jako opcje bez nazw i zachowań zaszytych w programie:

- **ADR 0005, `Options.GenericFamilies`:** wywołujący wskazuje, których fontów użyć dla `serif`, `sans-serif`
  i `monospace`. Testy i pomiary biorą przypisanie z `testdata/fonts/families.json`: `serif` to Tinos
  (metryki Times New Roman), `sans-serif` to Arimo (metryki Arial); oba na OFL-1.1. `monospace` zostaje bez
  wpisu (Noto Sans Mono).
- **ADR 0006, `Options.MarginsEscapeHeightLimits`:** domyślnie dolny margines ostatniego dziecka zostaje
  w pudełku, którego wysokość zmieniło `min-height` albo `max-height` (jak w Chromium); parametr włącza
  zachowanie z dosłownego tekstu CSS 2.2 § 10.7.

Pełny sondaż ekranowy dla tego samego stanu kodu (sposób pomiaru jak wyżej):

| Konfiguracja fontów | Zaliczone | Ściśle | Krawędziowo |
|---|---|---|---|
| Noto Serif jako `serif` (dotychczasowa) | 6855 | 6473 | 382 |
| Tinos jako `serif` | 6862 | 6481 | 381 |
| Tinos jako `serif`, Arimo jako `sans-serif` (nowa domyślna pomiarów) | **6863 (75,1%)** | 6481 | 382 |

Względem konfiguracji z Noto nowa zyskuje 31 testów i traci 23. Zyski to głównie `CSS2/css1`
i `normal-flow` (wzorce zakładające wiersz niższy niż 20 px przy foncie 16 px). Straty to testy pisane pod
szerszy font (np. `position-relative-035`) oraz testy z pismami, których nie ma żaden font testowy
(`quotes-0xx` z bengalskim, tajskim itd.), gdzie o wyniku decyduje wybór fontu zastępczego dla znaków
sąsiadujących z brakującymi. Od tej sekcji liczby sondażu są liczone w nowej konfiguracji.

Reftesty druku w nowej konfiguracji: 194 z 340 (bez zmian). Korpus 21 dokumentów: liczba stron bez zmian;
słowa przesunięte o ponad 3 pt względem Chromium: `16-typography-fonts` 2 → 0, `03-report-svg-chart` 0 → 2
(podpisy wykresu w `sans-serif` są teraz w Arimo), pozostałe bez zmian; łączny rozmiar PDF 3 272 939 →
3 313 433 bajtów (+1,2%), bo w pięciu dokumentach doszedł osadzony Arimo albo Tinos.

## Testy z pismami spoza fontów testowych poza pomiarem (2026-11-15)

Decyzja użytkownika z 2026-11-15: biblioteka w tej wersji nie musi obsługiwać pism egzotycznych, więc testy,
które ich wymagają, nie wchodzą do pomiaru. Reguła jest mechaniczna: `tools/fontcoverage` zapisuje
w `testdata/fonts/coverage.txt` znaki, dla których którykolwiek font testowy (Noto Sans, Noto Serif,
Noto Sans Mono, Tinos, Arimo, Ahem) ma glif, a `testdata/wpt/select.py` (`uncovered_letters`) pomija test,
gdy w rysowanym tekście testu albo jego wzorców jest litera spoza tego zbioru (bengalski, tajski, chiński,
arabski, gruziński itd.; hebrajski, grecki i cyrylica zostają, bo fonty je mają). Liczy się tekst poza
znacznikami i napisy w arkuszach; nazwy klas i selektory w innych pismach nie są rysowane i testu nie
wykluczają. Po wymianie fontów wystarczy wygenerować `coverage.txt` ponownie.

Sondaż ekranowy: pominiętych 25 testów z 9135 (`css-content` 14, `CSS2/generated-content` 6, `css-lists` 5),
z których 15 było zaliczonych, a 10 nie. Wynik dla tego samego stanu kodu: **6848 z 9110 (75,2%)**, w tym
6466 ściśle i 382 krawędziowo (przed wyłączeniem 6863 z 9135). Wśród 340 reftestów druku nie ma ani jednego
takiego testu, więc ich lista się nie zmienia. Reguła nie obejmuje znaków generowanych przez style liczników
(`list-style-type: georgian` itp.), których nie ma w źródle testu.

## Podział tabel między strony (2026-11-15)

Reftesty druku: **204 z 340** (wszystkie ściśle), poprzednio 194. Sondaż ekranowy bez zmian (6848 z 9110;
sprawdzony na ćwiartce 1/4, 2287 testów, bez zysków i strat, bo zmiany dotyczą tylko podziału stron).

Cztery poprawki w `tableLayout`, wszystkie widoczne w zwykłych wydrukach z tabelą dłuższą niż strona:

- tabela z podaną wysokością (`height`) nie wystaje już poza stronę i nie rośnie z każdą stroną;
- obramowanie i odstęp wewnętrzny tabeli nie są powtarzane na każdej stronie, gdy tabela ma `thead` lub
  `tfoot` (dotąd dolne obramowanie wychodziło wtedy na margines strony);
- przy `box-decoration-break: clone` wiersze zostawiają miejsce na dolne obramowanie tabeli;
- powtarzana stopka stoi przy dolnej krawędzi strony, z odstępem od przerwanego wiersza.

W tematyce podziału tabel zostaje 20 niezaliczonych: 13 to testy flex zaliczone do tej grupy przez klasyfikację
(`multi-line-row-flex-fragmentation-*`, m.in. `break-before: page` na elemencie flex), 4 testy `CSS2/pagination`
z `break-inside: avoid` grupy wyższej niż strona, 2 testy wysokości procentowych komórek
(`table-cell-percent-height-*`) i `fixedpos-in-footer-forced-break`.

Uzupełnienie z tego samego dnia: `caption` jest domyślnie wyśrodkowany (HTML, Rendering § 15.3.9; dotąd
do lewej), co zaliczyło `table-page-break-inside-avoid-8`; reftesty druku **205 z 340**. Sondaż ekranowy bez
zmian na ćwiartce 2/4 (2302 testy). Poprawka do rundy: commit `36b81fc` pogorszył dwa testy odziedziczone
(`TestTables_9`, `TestTables_22`: obramowania zwinięte przy powtarzanym nagłówku), bo przed zapisem
uruchomione były tylko testy pakietu układu, a nie rysowania; następny commit przywraca tam zachowanie źródła.
W tematyce podziału tabel zostaje 19 niezaliczonych.

## Strony, pola marginesowe, `outline-offset`, `display: contents` (2026-11-15)

Reftesty druku: **210 z 340** (wszystkie ściśle), poprzednio 205. Sondaż ekranowy: **6851 z 9110 (75,2%)**,
w tym 6469 ściśle i 382 krawędziowo; poprzednio 6848 (6466 i 382); zyskane 3, stracone 0.

- **Liczniki stron** widzą liczniki założone przez `counter-reset` na elemencie głównym, więc
  `counter-increment` w `@page` kontynuuje licznik dokumentu zamiast liczyć od zera (`content-010`, `-011`).
- **Jednostki `vw`, `vh`:** obszar strony jest liczony bez obramowania i odstępu wewnętrznego strony, także
  podanych w procentach (`page-box-004`, `-007`).
- **`outline-offset`:** nowa właściwość, także dla obrysu strony (`page-box-010`).
- **`display: contents`:** element nie tworzy pudełka, a jego dzieci przechodzą do rodzica; dotąd deklaracja
  była odrzucana i element zostawał zwykłym blokiem. Zysk w sondażu to trzy testy `css-tables/display-contents-*`;
  pozostałe testy z tą wartością albo przechodziły już wcześniej, albo zależą od innych braków.

Pozostałe niezaliczone testy `css-page` (ok. 50) w większości nie dotyczą samych stron: wzorce pól
marginesowych są zbudowane z flex, grid i pozycjonowania (`alignment-001`, `dimensions-*`, `auto-margins-*`),
a reszta to `z-index` pól marginesowych, `page-orientation`, piony (`writing-mode`), elementy pływające
i pozycjonowane dzielone między strony o różnych rozmiarach oraz właściwości eksperymentalne
(`safe-printable-inset`). Licznik założony w `@page` i ograniczony do jednej strony (`content-012`)
nie jest obsługiwany.

## Flex: obrazy, tabele i elementy liniowe jako elementy kontenera (2026-11-15)

Sondaż ekranowy: **6895 z 9110 (75,7%)**, w tym 6515 ściśle i 380 krawędziowo; poprzednio 6851
(6469 i 382); zyskane 47 (`css-flexbox` 30, `css-grid` 7, `CSS2` 10), stracone 3. Reftesty druku: 210 z 340
(bez zmian). Korpus 21 dokumentów: liczba stron i położenie słów względem Chromium bez zmian.

Poprawki dotyczące zwykłych dokumentów:

- **Obraz `data:` z SVG wpisanym wprost** nie był wczytywany, gdy dane zawierały spacje albo goły znak `%`
  (np. `width='100%'`): spacje były usuwane, a adres z `%` bez dwóch cyfr szesnastkowych odrzucany.
- **`<span>` z `padding`, obramowaniem lub tłem jako element flex albo grid** dostawał je dwukrotnie,
  bo był opakowywany w blok o tym samym stylu.
- **Obraz jako element flex** był zniekształcany, gdy kontener miał podaną wysokość (np. 400×200 zamiast
  100×100): automatyczne minimum było liczone także w osi poprzecznej, a rozmiar z proporcji przy
  rozciągnięciu nie był liczony wcale.
- **Tabela jako element flex** ignorowała `flex-basis`, `flex-grow`, `align-self` i rozciąganie: te
  właściwości zostawały na tabeli zamiast na pudełku, które jest elementem kontenera.
- **Kontener flex z `box-sizing: border-box`** rozciągał elementy do wysokości pudełka obramowania zamiast
  do wysokości treści.
- `width` i `height` elementu `<svg>` podane liczbą albo długością działają jak właściwości CSS.

Trzy straty: `flex-aspect-ratio-cross-size-001` i `flex-abspos-inset-cross-size-001` porównują dwa obrazy,
które wcześniej były jednakowo błędne, a teraz jeden z nich jest poprawny (drugi wymaga `aspect-ratio`
albo wysokości kontenera wynikającej z `top` i `bottom`); `table-as-item-percent-width-cell-001`
to tabela z `flex-basis: content` i komórkami o szerokości 100%, która nie kurczy się do kontenera.

Rozpoznane i odłożone we flex: właściwość `aspect-ratio` (nieobsługiwana; `flex-aspect-ratio-cross-size-001`),
kontener flex pozycjonowany bezwzględnie z wysokością wynikającą z `top` i `bottom`, tabela z podaną
szerokością i mniejszym `flex-basis`, `break-before: page` na elemencie flex, wysokości procentowe wnuków
(`percentage-heights-*`), pismo pionowe (`flexbox-writing-mode-*`), `text-wrap: balance`.

## Podział flex i grid między strony (2026-11-15)

Reftesty druku: **224 z 340** (wszystkie ściśle), poprzednio 210. Sondaż ekranowy: **6891 z 9110 (75,6%)**,
w tym 6511 ściśle i 380 krawędziowo; poprzednio 6895 (6515 i 380); zyskane 0, stracone 4.

- **`break-before` i `break-after` na elementach flex i grid** wymuszają podział strony: między liniami
  kontenera wierszowego, między elementami kolumny bez zawijania i między wierszami siatki. Dotąd były
  ignorowane, więc np. sekcje raportu ułożone w kolumnie flex nie dało się zacząć od nowej strony.
- **`flex-basis` z `box-sizing: border-box`** obejmuje obramowanie i odstęp wewnętrzny; dotąd element był
  o nie szerszy.
- **Nazwy stron (`page`) elementów flex i grid** nie przechodzą na kontener i nie wymuszają podziału.

Cztery straty w sondażu ekranowym to testy `flexbox-break-request-*` (`horiz-001a/b`, `vert-002a/b`), które zakładają medium bez stron (żądanie
podziału ma tam tylko zacząć nową linię kontenera). Pomiar renderuje każdy test jako dokument stronicowany,
więc żądanie podziału przenosi treść na następną stronę, tak jak wymagają tego testy druku
`multi-line-row-flex-fragmentation-081a…d`; obu grup nie da się zaliczyć tym samym sposobem pomiaru.
Rozpoczynanie nowej linii przez żądanie podziału w kontenerze z zawijaniem nie jest zaimplementowane.

W podziale flex zostaje 13 niezaliczonych reftestów druku: przenoszenie `break-before` pierwszego elementu
na kontener (`082a…d`), marginesy i wyrównanie elementów przesuwanych na następną stronę (`075`, `076`, `042`),
`064`, dwa warianty `069` (kolumna z odwróconą kolejnością), elementy pływające w elemencie flex (2)
i jeden test pisma pionowego.

## `aspect-ratio`, `<canvas>`, wysokości procentowe (2026-11-15)

Sondaż ekranowy: **6902 z 9110 (75,8%)**, w tym 6520 ściśle i 382 krawędziowo; poprzednio 6891
(6511 i 380); zyskane 15, stracone 4. Reftesty druku: **225 z 340**, poprzednio 224 (doszedł `page-name-canvas-001-print.html`).

- **`aspect-ratio`:** nowa właściwość. Blok o wysokości `auto` dostaje wysokość z szerokości (z uwzględnieniem
  `box-sizing`, `min-height`, `max-height`; wyższa treść powiększa pudełko), pudełko dopasowujące się do
  treści dostaje szerokość z podanej wysokości, a na obrazie proporcja podana bez `auto` zastępuje proporcję
  obrazu.
- **`<canvas>`** jest przezroczystym elementem zastępowanym o rozmiarze z atrybutów (domyślnie 300×150);
  dotąd nie zajmował miejsca.
- **Wysokość procentowa obrazu stojącego obok bloku** liczy się od rodzica, a nie od anonimowego bloku
  (dotąd `height: 100%` działało wtedy jak `auto`).
- **Wnuki elementu flex:** wysokość elementu wyznaczona przez `flex-basis` w kontenerze o nieokreślonej
  wysokości nie jest podstawą procentów, chyba że element jest całkowicie nieelastyczny i ma `flex-basis`
  podane długością.

Z 70 niezaliczonych testów używających `aspect-ratio` większość (55) to testy grid, które zależą jeszcze od
innych braków siatki (`grid-lanes`, rozmiary ścieżek z proporcji). Cztery straty to testy grid: dwa z `grid-lanes`
(nieobsługiwane; pusty element z `aspect-ratio` ma teraz wysokość i odsłania czerwone tło), obraz
z `aspect-ratio` jako element zagnieżdżonej siatki (szerokość ograniczana do ścieżki) oraz `<canvas>`
z rozmiarem procentowym przy wyrównaniu do linii bazowej.

## Tabele: kolejność rysowania obramowań zwiniętych, procenty w komórkach (2026-11-15)

Sondaż ekranowy: **6916 z 9110 (75,9%)**, w tym 6534 ściśle i 382 krawędziowo; poprzednio 6902
(6520 i 382); zyskane 14, stracone 0, błędów renderowania 0. Reftesty druku: 225 z 340 (bez zmian).

- **Obramowania zwinięte** (`border-collapse: collapse`) są rysowane nad blokami i tabelami leżącymi
  w komórkach, a pod pudełkami następującymi po tabeli; dotąd blok w komórce zasłaniał obramowanie.
  Tabela z `visibility: hidden` nie rysuje już obramowań zwiniętych.
- **Komórka o podanej wysokości** jest podstawą wysokości procentowych swoich dzieci; dotąd `height: 100%`
  w komórce działało jak `auto`.

W trakcie rundy pierwsza wersja zmiany kolejności rysowania wywoływała panikę dla tabeli z obramowaniami
zwiniętymi, która ma w komórce `inline-block` albo element pozycjonowany. Testy własne tego nie wykryły;
wykrył to dopiero sondaż ograniczony do tabel (9 błędów renderowania). Przypadek jest teraz
w `TestSecondCSS2RoundFixes`, a pełny sondaż po poprawce nie zgłasza błędów renderowania.

W `css-tables` zostaje ok. 30 niezaliczonych: `box-shadow` (właściwość nieobsługiwana), tła kolumn i wierszy
z obrazem, pismo pionowe, kilka testów linii bazowej i obramowań o ułamkowej szerokości.

Uzupełnienie (2026-11-15): pusty kontener `inline-flex` stoi na linii bazowej wiersza (linia bazowa
syntetyzowana z dolnej krawędzi marginesu), co zalicza trzy kolejne testy flex. Sondaż ekranowy:
**6919 z 9110 (75,9%)**, ściśle 6537, krawędziowo 382; zmierzone sondażem obszarów `css-flexbox`
i `css-grid/alignment` (1108 testów, bez strat), pozostałe obszary z poprzedniego pełnego przebiegu.
We flex zostaje w wyrównaniu do linii bazowej: `last baseline` (nieobsługiwane), połączenie z `wrap-reverse`
i pismo pionowe.

## Paczka: `box-shadow`, siatka z elementami pozycjonowanymi, nowsza składnia CSS (2026-11-15)

Pierwsza runda prowadzona paczką: kilka poprawek z rozłącznych obszarów, celowane testy po każdej,
jeden pełny sondaż na końcu.

| Obszar | Co doszło | Norma |
|---|---|---|
| `box-shadow` | cień zewnętrzny i wewnętrzny, wiele cieni, promienie narożników, rozmycie przybliżone 12 warstwami | CSS Backgrounds 3 § 6.1 |
| siatka | dzieci pozycjonowane bezwzględnie poza siatką, blok zawierający z `grid-row`/`grid-column`, pozycja statyczna z `justify-self`/`align-self` | CSS Grid 1 § 9 |
| siatka | rozciąganie tylko elementów o rozmiarze auto, wspólna linia bazowa w wierszu | CSS Grid 1 § 6.2, CSS Align 3 § 9 |
| barwy | `rgb()`/`hsl()` ze spacjami i alfą po ukośniku, `hwb()`, `lab()`, `lch()`, `oklab()`, `oklch()` | CSS Color 4 |
| wartości | `min()`, `max()`, `clamp()` z procentami | CSS Values 4 § 10 |
| selektory | `:where()` | Selectors 4 § 4.4 |
| reguły | `@supports` z `selector()`, `not`, `and`, `or` | CSS Conditional 3 § 6 |
| skróty | `inset`, `place-items`, `place-content`, `place-self`, `overflow-x`/`-y`, logiczne promienie narożników | CSS Logical 1, CSS Align 3, CSS Overflow 3 |
| wartości zastępcze | `position: sticky`, `float: inline-start`, `word-break: keep-all`, `white-space: break-spaces` | zob. `THIRD_PARTY.md` |

Ograniczenia, o których trzeba wiedzieć:

- barwy `lab()`/`oklch()` spoza gamy sRGB są przycinane składowa po składowej, bez odwzorowania gamy;
  w PDF trafiają jako DeviceRGB;
- rozmycie cienia to 12 warstw o stałej przezroczystości, nie rozkład Gaussa; przy dużych promieniach
  widać schodki, a plik rośnie o 12 ścieżek na cień;
- linia bazowa w siatce przesuwa elementy, ale nie powiększa ścieżek;
- `overflow-x`/`overflow-y` ustawiają `overflow` dla obu osi (silnik ma jedną właściwość), więc
  `overflow-x: auto` na opakowaniu szerokiej tabeli przycina ją tak samo jak `overflow: auto`;
- `overflow-y: clip` na elemencie wyższym niż strona przycina jak `overflow: hidden` (test druku
  `monolithic-overflow-028` oczekuje czterech stron, dostaje jedną; nie był zaliczany także wcześniej);
- `@supports` uznaje deklarację za obsługiwaną, gdy przechodzi walidację, czyli także dla wartości
  przyjmowanych zastępczo (`position: sticky`).

Przy okazji wyszło, że testy pakietu `html/boxes` nie budowały się od rundy z `display: contents`
(`go vet` odrzucał `%s` dla struktury `Display`), więc od tamtej pory nie były uruchamiane. Po poprawce
przechodzą w komplecie; od teraz w wyniku `go test ./internal/...` sprawdzam także wiersze `[build failed]`.

Wynik (2026-11-15, pełny sondaż ekranowy 9110 reftestów WPT, reguła ADR 0003): **6976 z 9110 (76,6%)**,
ściśle 6593, krawędziowo 383, niezaliczone 2134; poprzednio 6919. Zyskane 63 (siatka 59, flex 3, tabele 1),
stracone 6:

- `grid-baseline-align-cycles-001`: linia bazowa elementu o wysokości procentowej zależnej od ścieżki
  (cykl z § 11.5 CSS Grid, którego nie rozwiązujemy);
- `grid-items-relative-positioned-containing-block-003`: blok zawierający potomka pozycjonowanego
  względem elementu siatki z `position: relative`;
- dwa testy `grid-lanes` (funkcja nieobsługiwana; zaliczały się przypadkiem);
- `column-pseudo-background-color`: test sprawdza `@supports not selector(::column)` i teraz uczciwie
  pokazuje, że `::column` nie obsługujemy; wcześniej reguła `@supports` była pomijana w całości;
- `percentage-sizing-of-table-cell-007`: kontener przewijania o wysokości procentowej w komórce tabeli
  o wysokości auto; zaliczał się, dopóki `overflow-y: scroll` było ignorowane w teście i we wzorcu.

Pierwszy sondaż tej paczki pokazał 17 strat; 11 z nich to były błędy paczki, poprawione przed zapisem:
element pozycjonowany bezwzględnie bez podanych przesunięć był rozciągany na cały obszar siatki (norma
każe dopasować go do treści), rozciąganie elementu siatki gubiło informację, że szerokość była auto,
a `overflow-y: scroll` było pomijane, choć `overflow: scroll` przycina.

Reftesty druku (`testdata/wpt`, 340 testów, porównanie ścisłe): **228 zaliczonych** (poprzednio 225), bez strat.

## Paczka: `@media` z cechami, `text-shadow`, gradienty, `color-mix()`, selektory (2026-11-15)

Paczka wzięła się z sondy ostrzeżeń: jeden arkusz z ok. 150 konstrukcjami spotykanymi w zwykłych
dokumentach, renderowany z wypisaniem wszystkiego, co silnik odrzucił. Najpoważniejszy brak nie był na
liście kandydatów: reguła `@media` z jakąkolwiek cechą była odrzucana w całości.

| Obszar | Co doszło | Norma |
|---|---|---|
| `@media` | cechy z `min-`/`max-`, zakresy, `and`/`or`/`not`, `only`; atrybut `media`; rozmiar medium to obszar strony z `@page` (od 2026-11-16) | Media Queries 4, ADR 0007 i 0012 |
| `text-shadow` | cień ostry i rozmyty (17 kopii), wiele cieni, dziedziczenie | CSS Text Decoration 3 § 4 |
| gradienty | przystanek o dwóch pozycjach, wskazówka przejścia, `in <przestrzeń>` | CSS Images 4 |
| barwy | `color-mix()` w `srgb`, `srgb-linear`, `oklab`, `oklch` | CSS Color 5 § 3 |
| selektory | `:nth-child(… of S)`, `:has(> a)`, `:has(+ a)`, `:has(~ a)`, `:any-link`, `:focus-visible`, `:focus-within` | Selectors 4 |

Zagnieżdżanie reguł (`.a { & p { … } }`, także bez `&`) działało już wcześniej; doszły testy, które to pilnują.

Ograniczenia:

- `width` i `height` w `@media` nie zależą od `@page { size }` (pytanie 20 w `open-questions.md`);
- wskazówka przejścia w gradiencie daje przejście odcinkami liniowe, a przestrzeń interpolacji jest
  pomijana (zawsze sRGB);
- `:has()` z kombinatorem na początku przyjmuje tylko selektor złożony bez dalszych kombinatorów
  (`:has(> a)`, nie `:has(> a b)`);
- `color-mix()` nie przyjmuje `currentColor` ani przestrzeni `hsl`, `lab`, `lch`, `xyz`;
- cień tekstu nie obejmuje podkreślenia.

Nadal odrzucane z ostrzeżeniem (z tej samej sondy): `conic-gradient()`, składnia barw względnych
(`rgb(from …)`), `round()`/`abs()`, `translate`/`rotate`/`scale` jako osobne właściwości, `clip-path`,
`text-wrap`, `background-blend-mode`, `@container`, `@property`, `::selection`.

Wynik (2026-11-15): sondaż ekranowy bez zmian, **6976 z 9110** (ściśle 6593, krawędziowo 383), zero strat
i zero zysków; mierzone obszary WPT tych konstrukcji nie ćwiczą. Reftesty druku bez zmian: 228 z 340.
Paczka jest sprawdzana testami własnymi: `TestMediaQueriesWithFeatures` (32 zapytania, każde także jako
atrybut `media`), `TestModernSyntaxEquivalents` (38 par), `TestColorFunctionsConvertToSRGB`,
`TestTextShadowOffsetCopy`.

## Paczka: faktura z xml.pro wobec Chromium, fonty w PDF, interlinia, `monospace` (2026-11-16)

Pierwszy prawdziwy dokument spoza korpusu: strona faktury KSeF z `goksefgo/ksefview`, którą xml.pro
drukuje przez Chromium (i opcjonalnie przez gopdfgo). Obraz był zgodny z Chromium od razu; porównanie
plików i ramek słów odsłoniło trzy błędy niewidoczne w pikselach.

| Co | Przed | Po | Chromium |
|---|---|---|---|
| rozmiar pliku faktury | 199 kB | 32 kB | 43 kB |
| osadzone kopie Noto Sans | 8 (jedna na rozmiar pisma) | 2 (zwykły, gruby) | 2 |
| `Ascent` w opisie fontu | 50 905 | 1069 | 1069 |
| ramka słowa „Faktura”, `yMin` | 36,82 pt | 34,91 pt | 34,65 pt |
| rozstaw wierszy, Tinos 14 px, `line-height: normal` | 15 px | 16 px | 16 px |
| szerokość słowa „code” w `<code>` | 38,7 px | 32,0 px | 32,0 px |
| korpus 21 dokumentów, suma rozmiarów | 3316 kB | 992 kB | — |

Pomiary z 2026-11-16: rozmiary `ls`/`du`, opis fontu odczytany z pliku, ramki `pdftotext -bbox`,
Chromium bezgłowy z tymi samymi plikami fontów przez `@font-face`. Pliki do obejrzenia:
`bench/compare/invoice/` (poza gitem).

Co zmieniono:

- **Jeden font na krój.** Klucz pamięci fontów zawierał rozmiar pisma, więc każdy rozmiar osadzał
  własny podzbiór. Nazwa podzbioru nie zależy już od rozmiaru (wcześniej mogła zależeć od tego,
  co proces renderował wcześniej).
- **Podzbiór TrueType** zawiera tylko tabele potrzebne do rysowania; `hmtx` i `maxp` ucięte za ostatnim
  użytym glifem, `cmap` zbudowana od nowa z zostających glifów. Pusta `cmap` zmieniała rysowanie
  w CoreGraphics (1333 bajty obrazu) i w Ghostscripcie (3109 pikseli), brak wpisu dla ligatury „fi”
  jeszcze 37 pikseli w Ghostscripcie; z odwzorowaniem przeniesionym z oryginału obraz jest
  identyczny w poppler, Ghostscripcie i CoreGraphics.
- **Opis fontu** (`Ascent`, `Descent`, `FontBBox`) liczony w tysięcznych firetu. Nie zmienia obrazu,
  ale `pdftotext` i czytniki ekranowe liczą z niego ramki tekstu: po poprawce zgadzają się
  z Chromium do 0,3 pt.
- **`line-height: normal` z odstępem międzywierszowym fontu** (CSS Inline 3 § 4.2). Dotyczy krojów
  z niezerowym `lineGap`, czyli Tinos i Arimo; Noto Sans ma zero.
- **Pismo `monospace` 13 px** jak w przeglądarkach, z parametrem `MonospaceFontSize` (ADR 0008).
- **`quotes: auto`**: język dopasowany według BCP 47 i brany z rodzica; poprzednio wynik dla `fr-CH`
  zależał od losowej kolejności mapy.
- **Układ pod testy W3C**: szerokość wewnętrzna kontenera flex w kolumnie z zawijaniem; linia bazowa
  powiększająca wiersz siatki (drugi przebieg układu).

Wynik (2026-11-16, pełny sondaż ekranowy 9110 reftestów WPT, reguła ADR 0003): **7002 z 9110 (76,9%)**,
ściśle 6616, krawędziowo 386, niezaliczone 2108; poprzednio 6976 (ściśle 6593). Zyskane 27 (tabele CSS 2: 8,
flex 8, siatka 6, `css-content` 4, CSS 1: 1), stracony 1: `before-after-table-parts-001` (tabela
z treścią generowaną i obrazami; po zmianie wysokości wierszy różnica przekracza próg).
Reftesty druku: 228 z 340, bez zmian w zaliczonych.

Sprawdzone i odrzucone: zaokrąglanie górnej połowy interlinii także przy podanym `line-height`. Dawało
7 zaliczeń ścisłych więcej w sondażu, ale oddalało tekst od wydruku Chromium o ok. 0,3 pt w pięciu
z sześciu zmierzonych dokumentów korpusu, więc zaokrąglany jest tylko odstęp przy `line-height: normal`.

### Co pomiar ramek pokazał w korpusie

Dopóki `Ascent` w opisie fontu był błędny, miara „słowa przesunięte względem Chromium” mierzyła
częściowo ten błąd. Po poprawce (mediana odchylenia w pionie, pt; słowa przesunięte o ponad 3 pt):

| Dokument | Mediana | 90. centyl | Ponad 3 pt |
|---|---|---|---|
| `21-mixed-report` | 0,11 | 0,36 | 0 z 848 |
| `04-qms-procedure` | 0,74 | 13,19 | 8 z 2619 |
| `17-tables` | 1,32 | 2,59 | 13 z 175 |
| `20-invoice` | 2,52 | 5,42 | 350 z 826 |
| `08-flex` | 3,61 | 10,71 | 121 z 189 |
| `02-register-table` | 5,57 | 20,75 | 2015 z 2844 |
| `10-preformatted` | 16,95 | 127,37 | 37 z 149 |

Liczba stron zgadza się z Chromium w 19 z 21 dokumentów (wyjątki: `06-margin-boxes`, `11-edge-cases`,
oba znane). Odchylenia w tabelach, flex i tekście preformatowanym narastają w dół strony, czyli są
to różnice wysokości wierszy, nie pojedyncze przesunięcia. To następny cel: znaleźć, skąd bierze się
dryf w `02-register-table`, `10-preformatted` i `20-invoice`.

## Źródło odchyleń w pionie względem Chromium (2026-11-16)

Szukane metodą: 20 jednakowych pudełek jedno pod drugim, osobno dla wysokości wiersza, obramowania,
dopełnienia, marginesu i tabeli; położenie słowa za nimi w wydruku Chromium bezgłowego i gopdfgo
(`pdftotext -bbox`). Wysokości wierszy, dopełnienia i marginesy zgadzały się co do 0,02 px na pudełko.
Znalezione przyczyny:

1. **Szerokości obramowań.** Chromium przyciąga je do całych pikseli (CSS Values 4 § 6.1): `0.4pt`
   i `0.5pt` to 1 px, `1.2pt` też 1 px. Główna przyczyna dryfu w tabelach i flex. Naprawione,
   z parametrem `ExactBorderWidths` (ADR 0009; od ADR 0013 `ExactLengths`).
2. **Górny margines kontenera flex i siatki na początku strony** po zwykłym podziale nie był obcinany
   (CSS Fragmentation 3 § 5.2); bloki i tabele miały to dobrze. Stąd 14 pt na trzeciej stronie
   `20-invoice`. Naprawione.
3. **`border-spacing`** Chromium obcina do całych pikseli (`1mm` liczy jako 3 px). Bez zmian, pytanie 21.
4. **Pomniejszanie strony.** Gdy treść wystaje poza szerokość strony, Chromium skaluje całą stronę
   (w `07-images` i `10-preformatted` do 0,667). To nie dryf, tylko inna skala. Bez zmian, pytanie 21.

Odchylenie w pionie po poprawkach (pt; słowa dopasowane po kolei na tej samej stronie):

| Dokument | Mediana przed | Mediana po | Ponad 3 pt przed | Ponad 3 pt po |
|---|---|---|---|---|
| `02-register-table` | 5,57 | 0,34 | 2015 z 2844 | 0 |
| `08-flex` | 3,61 | 0,24 | 121 z 189 | 0 |
| `20-invoice` | 2,52 | 0,68 | 350 z 826 | 0 |
| `17-tables` | 1,32 | 1,84 | 13 z 175 | 0 |
| `19-flex-grid-pagination` | — | 0,26 | — | 310 z 2313 |

`17-tables` ma medianę wyższą przez `border-spacing` (punkt 3). W `19-flex-grid-pagination` zostają
inne miejsca podziału stron w kontenerach flex i siatki, nie dryf.

Wynik (pełny sondaż ekranowy 9110 reftestów WPT, reguła ADR 0003): **7004 z 9110 (76,9%)**, ściśle 6618,
krawędziowo 386, niezaliczone 2106; poprzednio 7002 (ściśle 6616). Zyskane 2 (`css-multicol`,
`css-tables`), stracone 0. Reftesty druku: 228 z 340, bez zmian.

## Siatka: nadmiarowe wiersze niejawne, odstęp po podziale strony, ostatni wiersz (2026-11-16)

Znalezione przy `19-flex-grid-pagination` (310 z 2313 słów odchylonych od Chromium o ponad 3 pt):

- **Nadmiarowe wiersze.** Rozmieszczanie automatyczne tworzyło 3n − 4 wierszy zamiast n (36 elementów
  w 3 kolumnach: 32 zamiast 12). Puste wiersze nie mają wysokości, ale każdy dokładał `row-gap`, więc
  każda siatka o co najmniej trzech wierszach z odstępem była za wysoka.
- **Odstęp na początku strony.** Dalszy ciąg siatki zaczynał się o `row-gap` poniżej góry strony.
- **Ostatni wiersz.** Wiersz z elementami o podanej wysokości, który nie mieścił się na stronie,
  zostawał na niej i wystawał poza stronę, jeśli był ostatni.

Po poprawkach dokument ma 25 słów odchylonych o ponad 3 pt. To karty o stałej wysokości, które
Chromium tnie na granicy strony, a gopdfgo przenosi w całości (ADR 0004).

Przy okazji, z testów W3C `css-grid` (`alignment`, `grid-items`):

- element z podanym wierszem i kolumną `auto` trafiał do drugiej kolumny pustego wiersza;
- `stretch` nie rozciągał ścieżek `minmax(25px, auto)`, a rozciągał `minmax(auto, 50px)`;
- `gap: calc(15% + 7px)` gubił część w pikselach albo procent (siatka i flex);
- `inline-grid` i siatka pływająca nie miały szerokości z treści: liczone były jak blok.
- ścieżka procentowa przy nieokreślonym rozmiarze kontenera liczyła się jak zero zamiast `auto`;
- siatka pływająca, która nie mieściła się na stronie, kończyła układ paniką (wynik `nil` przeznaczony
  dla układu blokowego).

Wynik (2026-11-16, pełny sondaż ekranowy 9110 reftestów WPT, reguła ADR 0003): **7048 z 9110 (77,4%)**,
ściśle 6661, krawędziowo 387, niezaliczone 2062; poprzednio 7004 (ściśle 6618). Zyskane 47, wszystkie
w `css-grid`; stracone 3: `grid-lanes/items/column-flex-spanning-items` i `subgrid/standalone-axis-size-005`
(funkcje nieobsługiwane, zaliczały się przypadkiem) oraz `grid-percent-cols-spanned-shrinkwrap-001`
(siatka pływająca z elementem rozpiętym na trzy kolumny: szerokość z treści liczona jest teraz z kolumn,
a rozkład miejsca dla elementu rozpiętego daje 73 px zamiast 100 px; znana słabość, następny cel
w siatce). Po sondażu doszły dwie osłony przed paniką, sprawdzone sondażem `css-grid` i `css-multicol`
bez zmian w zaliczeniach. Reftesty druku: **229 z 340**, poprzednio 228 (zyskany
`css-break/grid/monolithic-overflow-print`).

Zestawienie postępu od początku, z wykresami: `docs/progress.md`.

## Paczka: element siatki rozpięty na kolumny, obrazy w siatce, pola marginesów (2026-11-16)

Pierwsza paczka robiona według zasady „2–3 obszary, potem jeden pełny sondaż”.

- **Element rozpięty na kilka ścieżek.** Rozkład miejsca robił jedną rundę i nie miał kroku „ponad
  granice”, a ścieżce już przekraczającej granicę wzrostu dawał przyrost ujemny. Teraz według CSS Grid 1
  § 11.5.1: rozdział z zamrażaniem, wkład minimalny z `min-width`, wkład max-content ograniczony stałą
  funkcją maksimum. Przy okazji usunięta zależność od losowej kolejności mapy (o tym, czy element
  obejmuje ścieżkę `fr`, decydował numer w iteracji).
- **Obrazy i `svg` w siatce** zachowują rozmiar z proporcji (`normal` działa dla nich jak `start`).
- **Pola marginesów** (`@top-left`, `@top-right`…) dzielą krawędź według rozmiarów zewnętrznych;
  `vw`/`vh` przy `@page { width; height }` liczą się od podanego obszaru strony.
- Odłożone po obejrzeniu testów: linia bazowa w siatce (testy prawie w całości wymagają pisma pionowego),
  przenoszenie `break-before` z elementu zagnieżdżonego kontenera flex na kontener nadrzędny
  (`multi-line-row-flex-fragmentation-082a` do `-082d`), podział wierszy tabel z powtarzanym nagłówkiem.

Wynik (pełny sondaż ekranowy 9110 reftestów WPT, reguła ADR 0003; po sondażu jedna poprawka sprawdzona
sondażem `css-grid`): **7058 z 9110 (77,5%)**, ściśle 6671, krawędziowo 387, niezaliczone 2052; poprzednio
7048. Zyskane 11, stracony 1 (`grid-lanes/subgrid/…/row-auto-track-sizing-002`, funkcja nieobsługiwana).
Reftesty druku: **230 z 340**, poprzednio 229 (zyskany `margin-boxes/dimensions-003-print`).


## Paczka: `::first-line`, elementy pozycjonowane w siatce, pola marginesów w pionie (2026-11-16)

- **`::first-line`.** Silnik wyliczał styl pseudoelementu i nigdzie go nie używał. Teraz pierwszy
  sformatowany wiersz bloku (także w pierwszym bloku zagnieżdżonym) układany jest z kopii pudełka wiersza,
  w której treść obejmuje pudełko liniowe pseudoelementu i dziedziczy po nim (CSS Pseudo-Elements 4
  § 2.1). Z 20 niezaliczonych testów z `first-line` w nazwie zostały 4: `vertical-align` na pseudoelemencie
  z zagnieżdżonym większym pismem (2), element pływający w pierwszym wierszu, `text-indent` przy bloku
  w elemencie liniowym. Nieobsłużone: `text-transform` na `::first-line`.
- **Elementy pozycjonowane w siatce.** Dalszy potomek, dla którego siatka jest pudełkiem zawierającym,
  wskazuje obszar właściwościami `grid-column` i `grid-row`; prostokąt pozycji statycznej to cały obszar
  siatki. Zostają głównie testy z pismem pionowym, `direction: rtl` i `last baseline`.
- **Błędy ogólne znalezione przy okazji.** Szerokość max-content bloku sumuje stojące obok siebie
  elementy pływające (wcześniej brała największy, więc drugi spadał niżej). Szerokość dopasowana do treści
  elementu pozycjonowanego bez `left` i `right` ma do dyspozycji miejsce od pozycji statycznej do końca
  pudełka zawierającego. Spacje wiszące przy `pre-wrap` nie liczą się do min-content.
- **Pola marginesów z lewej i z prawej strony** dzielą krawędź według wysokości treści (źródło: po równo).
- **Pozycjonowanie względem komórki tabeli** liczy się od wysokości komórki w wierszu. Wyszło przy
  sprawdzaniu tekstu obróconego (`transform: rotate(-90deg)`) w nagłówkach kolumn: sam obrót działał
  i zgadzał się z Chromium, ale podpis wystawał ponad tabelę.
- **Pismo pionowe** (`writing-mode`): pomiar i trzy możliwe zakresy w pytaniu 22
  (`docs/adr/open-questions.md`); nic nie wdrożone.

Wynik (pełny sondaż ekranowy 9110 reftestów WPT, reguła ADR 0003; poprawka komórek tabeli doszła po
sondażu i została sprawdzona sondażem 1881 testów z tabel, pozycjonowania i flex, bez zmian):
**7094 z 9110 (77,9%)**, ściśle 6706, krawędziowo 388, niezaliczone 2016; poprzednio 7058. Zyskane 38
(siatka 12, selektory CSS 2 11, flex 6, kaskada CSS 2 4, inne 5), stracone 2:
`CSS2/floats-clear/table-among-floats-001` (tabela obok elementów pływających powinna zwęzić się, żeby
ominąć drugi z nich; wcześniej zaliczała się dzięki błędnej, za małej szerokości max-content) oraz
`css-grid/subgrid/line-names-007` (funkcja nieobsługiwana). Reftesty druku: **232 z 340**, poprzednio 230
(zyskane `margin-boxes/dimensions-005-print` i `-008-print`).

## Paczka: podział flex między strony, blok o podanej wysokości, `@media` względem `@page` (2026-11-16)

- **Wymuszony podział w zagnieżdżonym kontenerze flex.** `break-before` na pierwszym (i `break-after`
  na ostatnim) dziecku w przepływie przechodzi na element flex (CSS Break 3 § 3.1). Zostają warianty,
  w których żądanie stoi na innym elemencie pierwszej albo ostatniej linii kontenera wierszowego
  (`multi-line-row-flex-fragmentation-082b`, `-082c`): wymagają znajomości linii zagnieżdżonego kontenera.
- **Element wiersza flex przechodzi na następną stronę sam** (ADR 0011), gdy inne elementy linii
  mieszczą się na bieżącej. Wcześniej przechodziła cała linia.
- **Blok o podanej wysokości z dziećmi dzielonymi między strony** przenosi dalej tylko resztę wysokości.
  Wcześniej blok wyższy niż strona był przenoszony na następną, a jego ostatni fragment wypełniał całą
  stronę (na próbce 4 strony zamiast 2).
- **`@media` względem obszaru strony z `@page`** (ADR 0012, decyzja użytkownika): `width`, `height`,
  `aspect-ratio` i `orientation`. Dokument A4 z domyślnymi marginesami ma teraz medium o szerokości
  ok. 644 px, więc `(min-width: 768px)` jest fałszywe, jak w Chromium.
- **Pismo pionowe**: ADR 0010 (decyzja użytkownika): tylko poddrzewo obrócone, dopiero na potrzebę
  dokumentu.
- Korekta wcześniejszego opisu: `css-flexbox/balance` (28 niezaliczonych testów) to `flex-wrap: balance`,
  funkcja eksperymentalna, a nie podział między strony.
- Nierozwiązane: `break-nested-float-in-flex-item-001`, `-002` (wysoki element pływający w elemencie
  flex powinien ciągnąć się przez trzy strony), `multi-line-row-flex-fragmentation-064`.

Wynik: reftesty druku **240 z 340**, poprzednio 232 (zyskane: `single-line-column-flex-fragmentation-069b`,
`-069d`, `multi-line-row-flex-fragmentation-075`, `-076`, `-082a`, `-082d`,
`single-line-row-flex-fragmentation-042`, `monolithic-overflow-027`; straconych brak). Pełny sondaż
ekranowy (9110 reftestów WPT, reguła ADR 0003) bez zmian: **7094**, ściśle 6706, krawędziowo 388; zmiana
`@media` doszła po sondażu i została sprawdzona na 26 testach sondażu używających `@media`, bez zmian.
Korpus własnych dokumentów bez zmian.

## Paczka: przełącznik `ExactLengths`, `border-spacing`, siatka (wysokość z wiersza), kolejność rysowania (2026-11-16)

- **`Options.ExactLengths`** (ADR 0013, decyzja użytkownika): jeden przełącznik dla zaokrągleń długości
  jak w Chromium; zastępuje `ExactBorderWidths`. Nowe pod nim: `border-spacing` obcinane do całych
  pikseli (pomiar w Chromium: `1mm` daje 3 px, `0.6px` daje 0, `1.9px` daje 1 px). Pomniejszania strony
  nie naśladujemy.
- **Siatka: szerokość kolumny z wysokości wiersza.** Element w wierszach o stałym rozmiarze zna swoją
  wysokość przy liczeniu kolumn, więc wysokość procentowa, rozciąganie i proporcje wchodzą do szerokości
  z treści.
- **Elementy flex i grid rysowane jak `inline-block`**: każdy w całości, w kolejności dokumentu.
- **Strony nazwane**: `page` na elemencie liniowym nie zmienia nazwy strony bloku.
- Wycofane po próbie: pomijanie zmiany nazwy strony na samej górze strony i wewnątrz pudełka
  pozycjonowanego. Testy `page-name-zero-height-001` i `page-name-display-none-child` oraz
  `page-name-abspos-002` i `page-name-003` wymagają przy prostej regule sprzecznych wyników; mechanizm
  przeglądarek do rozpoznania.
- Nierozpoznane w programie testowym: reftesty druku z zakresem stron (`subpixel-page-size-001`, `-002`).

Wynik (pełny sondaż ekranowy 9110 reftestów WPT, reguła ADR 0003): **7112 z 9110 (78,1%)**, ściśle 6724,
krawędziowo 388, niezaliczone 1998; poprzednio 7094. Zyskane 18 (siatka 17, flex 1), straconych brak.
Reftesty druku: **242 z 340**, poprzednio 240 (zyskane `page-name-img-002-print`,
`page-name-canvas-002-print`). Korpus porównany po pikselach z poprzednią wersją: różnice tylko
w `17-tables` (`border-spacing`) i 7 pikseli wygładzania w `10-preformatted`.

## Pomniejszanie treści szerszej niż strona (2026-11-16)

Decyzja użytkownika, ADR 0014: treść wystająca poza prawą krawędź obszaru strony jest układana drugi raz
na powiększonym obszarze i rysowana pomniejszona, domyślnie najwyżej 1,5 raza jak w Chromium;
`Options.MaxShrink` (CLI `-max-shrink`) zmienia limit, 1 wyłącza, `math.Inf(1)` znosi limit. Pola
marginesów zostają w skali 1; prostokąty odnośników trafiają w pomniejszoną treść.

Porównanie z wydrukiem Chromium (headless, 2026-11-16; mediana odchylenia słów w pionie): `07-images`
43,3 pt → 0,3 pt, `10-preformatted` 17,2 pt → 0,1 pt; `11-edge-cases` 4 strony → 2 strony jak w Chromium.
Liczba stron zgadza się z Chromium w 20 z 21 dokumentów korpusu (wyjątek: `06-margin-boxes`).

Zmiana metody sondażu: od tej paczki sondaż ekranowy uruchamiamy z `-max-shrink 1`, bo testy W3C
zakładają ekran, na którym przeglądarka niczego nie pomniejsza. Wynik przy tym ustawieniu (pełny sondaż
9110 reftestów WPT, reguła ADR 0003): **7112**, ściśle 6724, krawędziowo 388, bez zmian względem
poprzedniej paczki. Reftesty druku (pomniejszanie włączone, jak w `gopdfgo.Render`): **242 z 340**,
bez zmian.

## Runda na dokumentach: cały korpus wobec wydruku Chromium (2026-11-16)

Metoda: 21 dokumentów z `testdata/documents` wydrukowanych przez Chromium (headless, `--print-to-pdf`,
2026-11-16) i przez gopdfgo; słowa z `pdftotext -bbox` dopasowane przez wyrównanie ciągów (wcześniejsza
miara parowała słowa po kolei i gubiła się przy różnej liczbie słów). Liczone są słowa wspólne, mediana
odchylenia w pionie i liczba słów odchylonych o ponad 3 pt w pionie i w poziomie.

Znalezione i naprawione:

- **Justowanie wystające za prawy margines.** Układ tekstu z pamięci podręcznej był współdzielony, więc
  wiersz o tym samym tekście (albo pudełko rysowane ponownie) dostawał odstępy justowania wielokrotnie.
  `04-qms-procedure`: 426 słów przesuniętych w poziomie o ponad 3 pt, teraz 0.
- **Pusty element liniowy między blokami** rozrywał zwijanie marginesów (`11-edge-cases`: 84 słowa
  o ponad 3 pt, teraz 9).
- **Znacznik listy przy `list-style-position: inside`** był węższy niż w Chromium, więc tekst łamał się
  inaczej (`09-lists-counters`: 30 słów, teraz 0).
- **Kontrolki formularzy**: szerokości i wysokości pól, przycisk i lista wyboru o szerokości treści,
  `cols` i `rows` pola wielowierszowego, pole wyboru, odstępy ramki `fieldset`, treść pod legendą
  (`13-forms-html-elements`: mediana 21,5 pt, teraz 2,7 pt).

Stan po rundzie:

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
| `21-mixed-report` | 845 | 0,29 | 193 | 26 | 0 |

Pozostałe różnice i ich przyczyny:

- `21-mixed-report` (193 słowa): treść w kolumnach (`column-count`) rozkłada się inaczej; obszar kolumn
  jest następny w kolejce.
- `04-qms-procedure` (36 słów na innej stronie): akapit pięciowierszowy przy `orphans: 3; widows: 3`.
  Chromium dzieli go 3 + 2, czyli łamie `widows`; gopdfgo przenosi cały akapit, zgodnie z CSS
  Fragmentation 3 § 4.3. Zostaje.
- `19-flex-grid-pagination`: 25 słów w pionie to karty o stałej wysokości (ADR 0004); 54 w poziomie to
  trzy wiersze, w których ostatnie słowo mieści się w Chromium o ułamek piksela (zaokrąglenia szerokości
  tekstu), a u nas przechodzi do następnego wiersza.
- `18-toc-running-elements` (25 słów) i `06-margin-boxes` (inna liczba stron): elementy bieżące
  (`position: running()`) i pola marginesów, których Chromium nie obsługuje tak jak norma; różnica jest
  po stronie Chromium.
- `13-forms-html-elements` (52 słowa do ok. 4 pt): margines ramki z legendą nie zwija się z marginesem
  poprzedniego bloku tak jak w Chromium; drobne różnice w wysokości wiersza z polem wyboru.
- `11-edge-cases` (9 słów): nagłówek przed blokiem `break-inside: avoid` wyższym niż strona; Chromium
  zostawia nagłówek na poprzedniej stronie, gopdfgo trzyma go z blokiem (`break-after: avoid`).

Wynik sondażu (pełny sondaż ekranowy 9110 reftestów WPT z `-max-shrink 1`, reguła ADR 0003; po sondażu
dwie poprawki sprawdzone sondażami obszarów: 2479 testów CSS 2 i list, 953 testy flex): **7119 z 9110
(78,1%)**, ściśle 6732, krawędziowo 387, niezaliczone 1991; poprzednio 7112. Zyskane 8 (elementy blokowe
w liniowych i marginesy), stracony 1 (`css-flexbox/flexitem-stretch-range`: suwak `input type=range`
po zmianie rozmiarów pól). Reftesty druku: **242 z 340**, bez zmian.

## Kolumny, paczka pierwsza: element rozpinający, dzielenie bloków, równoważenie, `contain` (2026-11-16)

Punkt wyjścia: 253 niezaliczone testy `css-multicol` i największa różnica w korpusie (`21-mixed-report`,
193 słowa ponad 3 pt od wydruku Chromium).

Co zrobione (szczegóły w `THIRD_PARTY.md`):

- element z `column-span: all` rozpina kolumny także w głębi drzewa; przodek jest dzielony na fragmenty;
- blok o podanej wysokości jest dzielony na dole kolumny (ADR 0015), więc równoważenie działa dla treści
  złożonej z bloków, a nie tylko z wierszy;
- górny margines kontenera kolumn zwija się z marginesem poprzednika (samo to dało ok. 20 testów);
- wysokość równoważenia: cała wysokość treści przez liczbę kolumn, kolumna podzielona w środku akapitu
  liczona do ostatniego wiersza, dolny margines ostatniego pudełka obcinany. To była przyczyna różnicy
  w `21-mixed-report`: kontener wychodził o 3 pt wyższy niż w Chromium i akapit po kolumnach spadał na
  następną stronę. Po poprawce 4 słowa ponad 3 pt (było 193);
- krok równoważenia dla grup pudełek z `break-before`/`break-after: avoid`;
- kolumny o podanej wysokości wystające poza stronę przechodzą na następną stronę zamiast tworzyć
  dodatkową kolumnę;
- linie między kolumnami w stylach `inset`, `outset`, `groove`, `ridge`;
- nowa właściwość `contain` (CSS Containment 1) w zakresie wpływającym na układ: `size` (pudełko
  niepodzielne, wysokość auto jak bez treści), `layout` i `paint` (niezależny kontekst formatowania).
  Korzysta z niej 99 testów W3C z mierzonych obszarów, głównie jako ze sposobu na pudełko niepodzielne.

Wynik: sondaż ekranowy 7189 z 9110 (ściśle 6795, krawędziowo 394), zyskane 74 (kolumny 73, siatka 1),
stracone 4; `css-multicol` bez `column-height` 221 z 370 (było 153). Reftesty druku 243 z 340 (zyskany
`break-inside-avoid-multicol-001-print`).

Straty i ich przyczyny:

- `multicol-contained-absolute`: blok zawierający element pozycjonowany nie jest dzielony między kolumny,
  a wzorzec (zwykły blok) już tak; potrzebny podział elementów pozycjonowanych;
- `multicol-span-all-rule-001`: kontener wyższy niż strona sondażu (600 px); test i wzorzec dzielą się
  teraz między strony inaczej na drugiej stronie, pierwsza jest zgodna;
- `multicol-fill-balance-032`: sam element pływający z `break-inside: avoid` jako treść równoważonych
  kolumn trafia na następną stronę (próba równoważenia uznaje go za mieszczący się, właściwy układ nie);
- `grid-lanes-contain-intrinsic-size-015`: układ `grid-lanes` nie jest obsługiwany, test przechodził
  przypadkiem, dopóki `contain` było pomijane.

Co zostało w kolumnach (149 testów): kolumny zagnieżdżone i ich podział między kolumny zewnętrzne
(ok. 45), podział elementu rozpinającego (`spanner-fragmentation-*`, 13), wysokości procentowe dzieci
kontenera o podanej wysokości (`multicol-span-all-children-height-*`), elementy pozycjonowane po elemencie
rozpinającym, kolumny w komórce tabeli, pismo pionowe.

## Kolumny, paczka druga: podział między strony, wysokości (2026-11-16)

Nowy dokument korpusu `22-multicol-pagination` (11 stron): dwie i trzy kolumny dłuższe niż strona,
nagłówki rozpięte, karty z `break-inside: avoid`, bloki o stałej wysokości, tabela i lista w kolumnach,
`break-before: column`, kontener kolumn o podanej wysokości na granicy stron. Pierwsze porównanie
z wydrukiem Chromium: ta sama liczba stron, 98 słów ponad 3 pt w pionie i 26 na innej stronie. Po
poprawkach: żadnego słowa ponad 3 pt w żadnej osi.

Co różniło (szczegóły w `THIRD_PARTY.md`):

- blok bez obramowania przecinany na dole kolumny liczył miejsce przed zwinięciem marginesu poprzednika,
  więc po przesunięciu wystawał i był przenoszony w całości; kolumny na końcu strony zostawały niedopełnione;
- przed wymuszonym podziałem kolumny (`break-before: column`) dolny margines zostaje; kolumny były o ten
  margines za niskie;
- kontener kolumn o podanej wysokości, który nie mieścił się w reszcie strony, był przenoszony w całości;
  teraz zaczyna się w reszcie strony, a na następnej ma resztę wysokości;
- wysokości procentowe dzieci liczą się od podanej wysokości kontenera kolumn (wcześniej od auto);
- przodek o podanej wysokości podzielony elementem rozpinającym: fragmenty przed elementem mają wysokość
  treści, ostatni dostaje resztę.

Wynik: sondaż ekranowy 7209 z 9110 (ściśle 6815, krawędziowo 394), zyskane 20, bez strat; `css-multicol`
bez `column-height` 241 z 370. Reftesty druku 243 z 340 (bez zmian; wśród niezaliczonych nie ma testów
kolumn poza dwoma z elementem pływającym w elemencie flex). Przejęty `TestColumnsRegression3` przepisany
według ADR 0015.

Znane ograniczenia po tej paczce: równoważenie przy `orphans`/`widows` zbliża się do wyniku krokami po
1 px (wolne dla długich kolumn z samymi krótkimi akapitami), kontener kolumn o podanej wysokości bez
treści przechodzi na następną stronę w całości (jak pusty blok, ADR 0004).

## Faktura KSeF z xml.pro w czterech wariantach wobec Chromium (2026-11-16)

Strona faktury z `goksefgo/ksefview` (ta, którą drukuje xml.pro), z próbki `sprzedaz.golden.xml`
i trzech wariantów wygenerowanych z niej: 60 pozycji, 140 pozycji, kilka stawek VAT z przelewem i bez
numeru KSeF. Pozycje mają nazwy od jednego do trzech wierszy. Oba silniki dostały te same pliki fontów
przez `@font-face`. Pliki do obejrzenia: `bench/compare/invoice/2026-11-16-variants/` (poza gitem);
w xml.pro i goksefgo nic nie zmieniono.

| Wariant | Strony (gopdfgo / Chromium) | Słowa ponad 3 pt przed poprawką | Po poprawce | PDF gopdfgo | PDF Chromium |
|---|---|---|---|---|---|
| podstawowa, 2 pozycje | 1 / 1 | 0 | 0 | 31 kB | 41 kB |
| 60 pozycji | 3 / 3 | 303 | 0 | 46 kB | 178 kB |
| stawki 23, 8, 5, zw; przelew | 1 / 1 | 0 | 0 | 37 kB | 61 kB |
| 140 pozycji | 7 / 7 | 1390 (143 na innej stronie) | 0 | 66 kB | 374 kB |

Pomiar: `pdftotext -bbox`, słowa dopasowane przez wyrównanie ciągów; rozmiary z `ls`.

Znaleziony błąd: wiersz tabeli na granicy stron, w którym nazwa pozycji ma trzy wiersze. Komórki
jednowierszowe (numer, jednostka, kwoty) zostawały na dole strony, a nazwa, której przy `orphans: 2`
i `widows: 2` nie da się podzielić, przechodziła w całości na następną. Chromium przenosi cały wiersz
i tak robimy teraz: gdy któraś komórka nie zmieściłaby na stronie żadnej treści, a inna by zmieściła,
podział wypada przed wierszem (o ile nie jest to pierwszy wiersz grupy na stronie).

Pozostała różnica, bez wpływu na strony: Chromium zaokrągla wysokość wiersza tabeli do całych pikseli
(30 albo 31 px na przemian), u nas każdy wiersz ma 30,33 px; położenia różnią się najwyżej o 0,9 pt.

Sondaż ekranowy i reftesty druku: bez zmian w zaliczonych (7209 z 9110; 243 z 340).

## PDF/A-2b (2026-11-16)

Decyzje użytkownika: poziom 2b, veraPDF jako walidator testowy, własny minimalny profil sRGB (ADR 0016).

Stan wyjściowy według veraPDF 1.30: zwykły plik łamał 4–5 reguł (metadane XMP, identyfikator pliku,
kolory bez `OutputIntent`, `CIDToGIDMap`, interpolacja obrazów, flaga drukowania adnotacji). Po wdrożeniu
`Options.Conformance = PDFA2b` (wiersz poleceń: `-pdfa 2b`): **22 z 22 dokumentów korpusu zgodne**, bez
ostrzeżeń walidatora; pomiar `verapdf -f 2b` na plikach z bieżącej binarki.

Przy okazji walidator wskazał błąd w podzbiorze fontu TrueType niezależny od PDF/A: tabela `post` miała
inną liczbę glifów niż `maxp`. Podzbiór zapisuje teraz `post` w wersji 3.0.

Rozmiar korpusu: 1,0 MB jako zwykły PDF, 1,1 MB jako PDF/A (ok. 3–4 kB na plik). Wygląd stron bez zmian
(test porównuje piksele obu trybów); wyjątek opisany w ADR: powiększany obraz rastrowy bez wygładzania.

Czego tryb nie obejmuje: załączniki (PDF/A-2 ich zabrania, silnik ich nie odrzuca ani nie zgłasza),
PDF/A-2a i PDF/UA (tagged PDF), PDF/A-3b.

## Sondaż rozszerzony o tekst, tła i fonty (2026-11-16)

Dotychczasowy sondaż ekranowy obejmuje siedem obszarów W3C. Trzy kolejne dotyczą każdego dokumentu,
a nie były mierzone: `css-text` (łamanie wierszy, białe znaki, justowanie), `css-backgrounds` (tła,
obramowania, cienie) i `css-fonts`. Pomiar tą samą metodą (strona 800 x 600 px, 96 dpi, reguła ADR 0003,
`-max-shrink 1`); 240 testów z pismami spoza fontów testowych pominięto. Wynik rozszerzony podajemy
osobno, żeby szereg dotychczasowy (9110 testów) pozostał porównywalny.

| Obszar | Zaliczone | W tym krawędziowo | Największe braki |
|---|---|---|---|
| `css-text` | 381 z 887 (43,0%) | 8 | `white-space` 274 (z 397), `line-break` 33, `word-break` 25, `hyphens` 22, `line-breaking` 19, `letter-spacing` 18, `text-align` 17, `overflow-wrap` 16 |
| `css-backgrounds` | 421 z 600 (70,2%) | 28 | `background-size` 60 (z 217), `background-clip` 37, `border-image` ok. 25, `background-origin` 10, `box-shadow` 10 |
| `css-fonts` | 168 z 281 (59,8%) | 0 | `font-family-name` 21, `font-variant-alternates` 17, `font-palette` 13, `font-size-adjust` ok. 10 |
| razem | **970 z 1768 (54,9%)** | 36 | |

Pierwszy przebieg pokazał dziesięć awarii silnika na poprawnych dokumentach, wszystkie w `css-text`,
z trzech przyczyn (naprawione, test `TestTextCrashesFromW3CSurvey`):

- **Przepełnienie stosu przy `tab-size` w jednostce `ch`.** Obliczenie wartości pytało o nią samą.
  Takiej awarii nie da się przechwycić: ginął cały proces, nie tylko renderowanie. Teraz każda wartość,
  która w trakcie obliczania pyta o samą siebie, dostaje na ten czas wartość początkową.
- **Panika przy justowaniu** tekstu od prawej do lewej i przy `text-align-last` (indeks poza tekstem
  wiersza w bibliotece go-text).
- **Panika przy znakach podziału wiersza** U+000B i U+000C: przejęty kod sprawdzał listę znanych znaków
  i przerywał pracę, gdy trafił na inny.

Po poprawkach: 0 awarii w 1768 testach, 5 testów zyskanych. Dotychczasowy sondaż bez zmian (7209 z 9110),
reftesty druku 243 z 340.

## Białe znaki: znaki sterujące, wiszące spacje, `break-spaces` (2026-11-16)

Grupa `white-space` w `css-text` była największą pojedynczą grupą niezaliczonych testów (274 z 397).
Trzy przyczyny i co zrobiono:

- **Znaki sterujące** (ok. 62 testy) nie były rysowane. Norma każe je pokazać; rysujemy znak zastępczy
  U+FFFD. W tekście wydobytym z PDF w tym miejscu też jest U+FFFD.
- **`pre-wrap`**: spacje na końcu zawiniętego wiersza liczyły się do jego szerokości, więc tekst
  wyrównany do prawej albo do środka był przesunięty. Teraz wiszą.
- **`break-spaces`** było przybliżane przez `pre-wrap`. Jest osobną wartością: spacje zajmują miejsce
  i przechodzą do następnego wiersza.

Wynik `css-text`: 493 z 887 (było 381), zyskane 112, bez strat względem stanu wyjściowego. Trzy testy
`break-spaces-before-first-char` (010, 011, 017) przechodziły w wersji pośredniej i nie przechodzą
w końcowej; przyczyny nie ustalałam.

Co zostało w `white-space` (ok. 160): spacja ideograficzna i inne odstępy Unicode na końcu wiersza
(ok. 20), `text-wrap: balance` (ok. 19), próg pozycji tabulatora, rozmiary wewnętrzne przy `pre-wrap`,
`textarea`, pismo od prawej do lewej.

## Tła i odstępy między znakami: co pokazały małe grupy testów (2026-11-16)

Trzy grupy z rozszerzonego sondażu miały bardzo niski wynik, co sugerowało pojedyncze przyczyny.
Sprawdzenie dało trzy różne odpowiedzi:

- **`background-origin` (10 niezaliczonych z 12): artefakt pomiaru.** Silnik rysuje te testy poprawnie.
  Wzorzec W3C układa cztery pudełka jedno pod drugim i przesuwa je `position: relative` o 288 i 576 px
  w górę; u nas trzecie i czwarte pudełko wypada przed przesunięciem na drugą stronę (strona sondażu ma
  600 px), więc wzorzec jest niepełny. Bez trybu ciągłego (bez podziału na strony) tych testów nie da się
  uczciwie zmierzyć.
- **`letter-spacing` (18 niezaliczonych z 21): norma i Chromium się różnią.** Testy wymagają, żeby odstęp
  nie był dodawany po ostatnim znaku elementu ani na końcu wiersza (CSS Text 3 § 8.2). Chromium 157
  dodaje go po każdym znaku (sprawdzone zrzutem ekranu 2026-11-16) i sam tych testów nie przechodzi.
  Zachowujemy się jak Chromium; zmiana wymagałaby decyzji i parametru (zasada z ADR 0006).
- **`background-clip` (37 niezaliczonych z 47): dwie brakujące wartości.** `text` (20 testów) wdrożone:
  tło w kształcie liter, przez maskę przezroczystości; poprawne w poppler i Ghostscripcie, plik przechodzi
  walidację PDF/A-2b. `border-area` (18 testów, CSS Backgrounds 4, nowa wartość) nie jest obsługiwana.

Wynik `css-backgrounds`: 434 z 600 (było 421): zyskane 14, stracony 1 (`clip-text-transform`: tekst
z tłem przez maskę ma inne wygładzenie krawędzi niż tekst we wzorcu).

## Tryb ciągły i przejście sondażu na ten tryb (2026-11-16)

`Options.Continuous` (ADR 0017): jedna strona, która rośnie do wysokości treści; wysokość z `@page`
zostaje podstawą procentów, `vh` i elementów pozycjonowanych; wymuszone podziały nie tworzą stron.
Wszystkie 22 dokumenty korpusu wychodzą w tym trybie jako jedna strona.

Po co: testy ekranowe W3C porównują zrzut okna 800 x 600 px, a silnik dzielił treść wyższą niż 600 px
na strony. Sondaż renderuje teraz w trybie ciągłym i przycina obraz do okna (`SURVEY_CROP=600`).

| Pomiar tej samej binarki | Stronicowo | Tryb ciągły |
|---|---|---|
| siedem dotychczasowych obszarów (9110) | 7209 | 7229 (ściśle 6832, krawędziowo 397) |
| tekst, tła, fonty (1768) | 1095 | 1148 (ściśle 1110, krawędziowo 38) |
| razem (10878) | 8304 | 8377 |

Zyskane 73 (tła 53, flex 7, CSS 2: 6, kolumny 3, siatka 2, tabele 2), stracone 0. Wniosek: artefakty
podziału na strony były małą częścią niezaliczonych (73 z 2574); pozostałe 2501 to realne różnice.
Reftesty druku (`testdata/wpt`) zostają stronicowe: 243 z 340.

## Tekst, paczka druga: łamanie wierszy (2026-11-16)

- **`line-break: anywhere`** (nowa właściwość): 21 testów.
- **Metryki fontu podstawowego przy podanym `line-height`**: znak z fontu zastępczego (symbol, łącznik
  nierozdzielający U+2011) nie powiększa wiersza. To błąd widoczny w zwykłych dokumentach: wiersz
  z jednym znakiem spoza fontu podstawowego był wyższy od sąsiednich mimo stałego `line-height`. 13 testów.
- **`word-break: break-all`** zachowuje reguły dla znaków przestankowych: 10 testów.
- **Spacja ideograficzna U+3000** traktowana jak zachowana spacja: 18 testów.
- **U+00A0 przy elemencie atomowym** nie blokuje podziału: 3 testy.

Wynik `css-text`: 552 z 887 (było 493). Straty: `line-break-anywhere-and-white-space-006`,
`wrap-inside-avoid-line-break-anywhere`, `line-break-loose-hyphens-002` (dwa pierwsze łączą `anywhere`
z właściwościami, których nie obsługujemy; trzeci wymaga `line-break: loose`, które działa jak `auto`).

Co zostało w `css-text` (ok. 335): `text-wrap: balance` (ok. 19), `letter-spacing` (16, różnica wobec
Chromium, pomijane), próg pozycji tabulatora (7), `word-space-transform` (7, eksperymentalne),
`text-transform` z regułami językowymi (6), dzielenie wyrazów w innych językach (8, brak słowników),
rozmiary wewnętrzne przy `pre-wrap` i `break-spaces`, pismo od prawej do lewej.

## Tekst, paczka trzecia, i pierwsza paczka flex (2026-11-17)

Pomiar: pełny sondaż dziesięciu obszarów w trybie ciągłym (`bench/survey`, 10 962 testy), ta sama metoda
co stan bazowy z 17.11, z jedną poprawką miary: kilka odnośników `rel="match"` to alternatywy (13 zaliczeń
z samej zmiany miary: 9 w tekście, 4 w CSS 2).

| Miara | Przed | Po |
|---|---|---|
| siedem dotychczasowych obszarów (9123) | 7240 | 7270 (ściśle 6871, krawędziowo 399) |
| tekst, tła, fonty (1839) | 1240 | 1333 (ściśle 1293, krawędziowo 40) |
| `css-text` (909) | 565 | 658 |
| `css-flexbox` (952) | 680 | 705 |

Stracone względem stanu bazowego: 0. Reftesty druku bez zmian: 243 z 340.

Tekst:

- **Odstępy na końcu wiersza.** Biblioteka go-text odbierała szerokość każdemu końcowemu glifowi bez
  obrysu. Teraz traci ją tylko zwykła spacja; spacja nierozdzielająca zachowuje szerokość i nie wisi,
  a pozostałe odstępy (ideograficzny i inne kategorii Zs) wiszą z szerokością i tłem. Widoczne w zwykłych
  dokumentach: tło albo podkreślenie elementu kończącego się spacją nierozdzielającą było za krótkie.
- **`pre-wrap`**: wiszące spacje nie liczą się do wyrównania i justowania także wtedy, gdy podział wypada
  na granicy elementów; tekst `pre-wrap` jest justowany (wybór normy, jak Chromium).
- **Tabulatory**: pozycje tabulacji od początku wiersza, a nie fragmentu tekstu (tabulator po `<b>` w `<pre>`
  trafiał w złe miejsce), jednostka z fontu bloku, próg pół znaku, ułamkowy `tab-size`.
- **Łamanie w dowolnym miejscu między elementami liniowymi** (`overflow-wrap: anywhere`, `break-word`,
  `word-break: break-all`, `line-break: anywhere`) i `word-break: break-word`.
- **Tolerancja zaokrągleń przy zawijaniu**: N znaków fontu o stałej szerokości mieści się w pudełku `N ch`.

Flex:

- kontener o podanej szerokości ma ją jako szerokość wewnętrzną (10 testów `flexbox_stf-*`); `column-gap`
  i `flex-basis` elementów nieelastycznych liczą się do szerokości wewnętrznej;
- marginesy `auto` w kierunku kolumny, przy przepełnieniu i przy `align-content`;
- suma współczynników flex poniżej 1, odrzucanie ujemnych;
- automatyczne minimum obrazu z proporcjami liczone z podanego rozmiaru poprzecznego.

Pominięte zgodnie z przyjętymi regułami: pismo od prawej do lewej (pytanie 24), pismo pionowe (ADR 0010),
`visibility: collapse` na elementach flex (Chromium nie obsługuje), `letter-spacing` z tabulatorami
(model odstępów go-text: połowa przed znakiem i połowa po nim), `white-space-trim`, `text-fit`,
`hanging-punctuation` (Chromium nie obsługuje). Nowe pytanie 25: parser XML dla dokumentów XHTML.

Co zostało: `css-text` 251 (z tego ok. 60 pismo od prawej do lewej i `letter-spacing`, 19 `text-wrap: balance`,
15 `white-space-trim`, 10 `text-fit`, 10 `hanging-punctuation`), `css-flexbox` 247 (ok. 40 pismo pionowe
i od prawej do lewej, ok. 20 linia bazowa, 8 tabela jako element, reszta pojedyncze przypadki).

Uzupełnienie z tego samego dnia: linia bazowa kontenera flex (przesunięcie przez `align-content`,
`wrap-reverse`, blokowy kontener jako element innego kontenera) dała 3 testy: siedem obszarów 7273 z 9123
(ściśle 6874, krawędziowo 399), `css-flexbox` 708 z 952; pełny sondaż, strat brak.

## Siatka i dalszy flex; dokładna szerokość wiersza na granicy (2026-11-17)

Pomiar: pełny sondaż dziesięciu obszarów, tryb ciągły, `bench/survey`.

| Miara | Przed | Po |
|---|---|---|
| siedem dotychczasowych obszarów (9123) | 7273 | 7304 (ściśle 6907, krawędziowo 397) |
| tekst, tła, fonty (1839) | 1333 | 1339 (ściśle 1299, krawędziowo 40) |
| `css-grid` bez `grid-lanes` i `subgrid` (640) | 441 | 460 |
| `css-flexbox` (952) | 708 | 717 |

Strat brak. Korpus 22 dokumentów jest co do piksela taki sam jak przed paczkami z 17.11.

Siatka: szerokość elementu bez rozciągania (była liczona z marginesami), `min-content`/`max-content`
jako rozmiar elementu wyłącza rozciąganie, kontener rośnie razem z wierszem powiększonym przez linię
bazową, `min-height` kontenera rozciąga wiersze `auto`, `repeat(auto-fill, minmax(a, b))` z `b < a`,
niejawne nazwy linii z `grid-template-areas`.

Flex i przepływ blokowy: `inline-flex` o szerokości auto dopasowuje się do treści (w źródle zajmował cały
wiersz; błąd widoczny w zwykłych dokumentach), kontenery flex i grid o podanej szerokości omijają elementy
pływające.

Zawijanie wierszy, korekta paczki tekstu: tolerancja „pół jednostki na znak” z poprzedniej paczki dawała
fałszywe dopasowania i psuła zwykłe dokumenty (w `19-flex-grid-pagination` wiersz łamał się po pogrubionym
numerze punktu; wykryte porównaniem korpusu, nie sondażem). Zastąpiona dokładnym sprawdzeniem: silnik
pamięta, ile zaokrąglenie do 1/64 px odjęło każdemu znakowi, i wiersz leżący na granicy przyjmuje tylko
wtedy, gdy jego dokładna szerokość mieści się w dostępnej. Próbowane i odrzucone: zaokrąglanie narastające
(ten sam tekst podzielony inaczej na fragmenty dostawał pozycje różne o 1/64 px; 10 strat w sondażu).

Wniosek dla sposobu pracy: po paczce zmieniającej łamanie wierszy porównywać korpus z poprzednią binarką
(`bench/survey/tools/corpuscmp.py`), bo sondaż W3C takich regresji nie pokazuje.

## `border-image` i kolumny w trybie ciągłym (2026-11-17)

Pomiar: pełny sondaż dziesięciu obszarów, tryb ciągły, `bench/survey`.

| Miara | Przed | Po |
|---|---|---|
| siedem dotychczasowych obszarów (9123) | 7304 | 7319 (ściśle 6922, krawędziowo 397) |
| tekst, tła, fonty (1839) | 1339 | 1351 (ściśle 1313, krawędziowo 38) |
| `css-backgrounds` (603) | 490 | 502 |
| `css-multicol` bez `column-height` (370) | 244 | 257 |

Stracone: `border-image-image-type-001` i `border-image-space-001`, oba wcześniej zaliczane tylko
krawędziowo (wzorce rysują to samo inną techniką; różnice to jednopikselowe szwy). Korpus bez zmian.

- **`border-image`**: powiększany wycinek obrazu rastrowego jest osobnym, wyciętym obrazem (bez smugi
  koloru z sąsiedniego obszaru), kafelki `repeat` są wyśrodkowane, zbyt szerokie obramowania są
  zmniejszane proporcjonalnie. Test przejęty `TestBorderImageRepeat` poprawiony według normy.
- **Kolumny w trybie ciągłym** (ADR 0017 pkt 5): nadmiarowe kolumny w osi wiersza, treść zostaje
  w kolumnie, `column-fill: auto` bez ograniczonej wysokości równoważy. Wcześniej strona rosła do umownej
  wysokości 2^22 px i test odpadał na rasteryzacji.

Rozpoznane i pominięte: `css-fonts` `font-family-name-*` (21, wymagają fontów testowych zainstalowanych
w systemie), `font-palette` (13, fonty kolorowe). Do rozważenia jako osobne funkcje: `font-size-adjust`
(ok. 13 testów) i `@font-feature-values` z `font-variant-alternates` (17).

## CSS 2: ogon grup (2026-11-17)

Pomiar: pełny sondaż dziesięciu obszarów w trybie ciągłym; obszary flex i siatki powtórzone po ostatniej
poprawce (elementy zastępowane we flex).

| Miara | Przed | Po |
|---|---|---|
| siedem dotychczasowych obszarów (9123) | 7319 | 7367 (ściśle 6972, krawędziowo 395) |
| CSS 2 (5846) | 5506 | 5548 |
| `css-flexbox` (952) | 717 | 722 |

Strat brak; korpus co do piksela bez zmian.

- element główny z `position: absolute` (8 testów) i tło tabeli będącej elementem głównym na płótnie;
- `overflow` na tabeli przycina pudełko tabeli (podpis poza przycięciem), nie dotyczy wierszy i kolumn;
  tabela tworząca własny kontekst nakładania rysuje tła komórek (10 testów);
- `<iframe>` jako element zastępowany 300 na 150 (14 testów);
- `clear` a zwijanie marginesów z pierwszym dzieckiem (7 testów);
- linia bazowa `inline-block` z `overflow` z ostatniego wiersza, gdy leży w pudełku (4 testy); wyjątek
  dla `textarea`, bo bez niego dokument `13-forms-html-elements` odsuwał się od Chromium o 3 pt
  (wykryte porównaniem korpusu);
- elementy zastępowane obok siebie we flex nie nakładają się (błąd niezależny od `iframe`, widoczny dla
  dwóch `canvas`);
- łańcuch znaków jako styl w `counter()` odrzucany.

Pominięte: `content-counter-006`–`008` (zakres licznika według CSS 2.1, sprzeczny z CSS Lists 3 § 4.3,
którą silnik stosuje), `content-counter-013`, `-014` (kerning ponad granicą elementów liniowych),
`margin-bottom-applies-to-012`–`015` (wzorzec niezgodny z testem), testy z pismem od prawej do lewej.

