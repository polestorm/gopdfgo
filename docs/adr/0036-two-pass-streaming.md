# ADR 0036: Dokumenty z ponowną paginacją układane w dwóch przebiegach

Status: przyjęta 2026-11-21; dopisek trzeci 2026-11-23 (odkładanie stron zamiast drugiego przebiegu tam, gdzie się da);
dopisek czwarty 2026-11-23 (odwołania wstecz z `target-counter(…, pages)` odkładaniem).

## Kontekst

Od ADR 0027 strony są rysowane zaraz po ułożeniu, a ich drzewa pudełek zwalniane. Dokument, który wymaga
ponownej paginacji, wracał jednak do starej drogi: cały układ w pamięci, potem rysowanie. Ponowna
paginacja zachodzi przy `target-counter()` i `target-text()` do stron dalszych (spis treści na początku),
przy `counter(pages)` w treści stron oraz przy `string-set` i `bookmark-label` z liczbą stron albo
licznikiem celu. Książka z `testdata/books` ze spisem treści na początku i „Strona X z Y” przy każdym
rozdziale zajmowała 0,59–0,61 GB zamiast 0,44 GB.

Stara droga (`layoutDocument`) robi obiegi paginacji. Obieg pierwszy układa wszystkie strony; każdy
następny układa od nowa tylko strony oznaczone (`RemakeState.ContentChanged`, `PagesWanted`) i strony,
od których zmienił się stan wejściowy, a resztę bierze z obiegu wcześniejszego. Wynik to mieszanka stron
z różnych obiegów. ADR 0027 zapisał przeszkodę: znaczniki są stawiane bez porównania wartości, więc
ponowny układ wszystkich stron w obiegu drugim wywołałby obieg trzeci, którego stara droga nie robi.

Warunek: wynik bajt w bajt taki sam jak dotąd.

## Rozważone

- (a) Pierwszy przebieg tylko ustala stan paginacji, drugi układa i rysuje wszystkie strony z tym stanem.
  Koszt: jeden układ całego dokumentu więcej.
- (b) Zwalnianie stron w obiegach starej drogi. Obieg drugi bierze nieoznaczone strony z obiegu
  pierwszego, więc ich drzewa muszą żyć do końca paginacji; oszczędności nie ma.
- (c) Rysowanie stron obiegu pierwszego na próbę i podmiana tych, które obieg dalszy układa od nowa.
  Zmiana spisu treści przesuwa zwykle wszystkie dalsze strony, więc rysowanie szłoby do kosza; do tego
  kolejność stron i miejsca na pola marginesowe w celu PDF. Odrzucone jako złożone przy małym zysku.

## Decyzja

1. Wariant (a). Gdy przebieg strumieniowy (ADR 0027) trafi na potrzebę ponownej paginacji, jego wynik
   i ostrzeżenia idą do kosza (`restart` w `document.Stream`), a dokument jest układany od nowa w dwóch
   przebiegach (`layout.LayoutStreaming`, `streamer.paginate`). Przebieg strumieniowy wykrywa potrzebę
   zwykle na pierwszych stronach (spis treści), więc kosztuje mało.
2. Przebieg pierwszy to dokładnie obiegi paginacji z `layoutDocument` (wydzielone do
   `layoutContext.paginate`, wspólne dla obu dróg), ale drzewo każdej strony jest zwalniane zaraz po jej
   ułożeniu. Zostaje stan paginacji (`pageMaker`), numery stron i liczniki kotwic, liczba stron i treść
   pudełek przeliczona przez `ParseAgain`.
3. Przebieg drugi to jeszcze jeden obieg, w którym wszystkie strony są układane od nowa (jak w obiegu
   dalszym `layoutDocument`, od przypisów sprzed paginacji), obrabiane, rysowane i zwalniane jak
   w ADR 0027. Znaczniki ponownego układu nic w nim nie znaczą. W ich miejsce przebieg sprawdza, czy
   stan się nie zmienił (`layoutContext.verifying`, `drift`):
   - stan wejściowy następnej strony (`remakePage`) taki sam jak po przebiegu pierwszym;
   - kotwica na tej samej stronie i z tymi samymi licznikami stron (`checkAnchor`); liczba stron w liczniku
     kotwicy pochodzi z obiegu, w którym kotwicę ułożono ostatnio (w obiegu pierwszym 0), więc liczy się
     tylko, gdy któreś odwołanie używa `target-counter(…, pages)`;
   - żadna treść z licznikami stron nie jest przeliczana w trakcie układu strony (`callParseAgain`);
   - ta sama liczba stron.

   Każda różnica przerywa przebieg, a dokument idzie starą drogą. Stara droga kończy paginację, gdy obieg
   nie zmienia nic, co strony czytają; strona wzięta przez nią z obiegu wcześniejszego jest więc
   funkcją tego samego stanu co strona ułożona od nowa w przebiegu drugim.
4. Ostrzeżenia z układu stron zgłasza przebieg pierwszy (te same co stara droga); w drugim są wyciszone
   na czas układu strony, a zgłaszane z obróbki, pól marginesowych i rysowania. Liczba ostrzeżeń jest
   taka sama jak w starej drodze.
5. Krótki dokument (najwyżej `layout.TwoPassMinPages` = 32 stron) przebieg pierwszy układa w całości,
   bez zwalniania stron, kończy jak `layoutDocument` i oddaje do rysowania jak `Render` i `Write`.
   Przy kilkudziesięciu stronach drugi przebieg kosztowałby czas, a oszczędził kilkanaście MB. Gdy
   dokument przekroczy ten próg, strony już ułożone są zwalniane, a dalej działa punkt 3.
6. Bez zmian wracają do starej drogi: fragment pudełka pozycjonowanego dokładany do strony wcześniejszej
   (`lateBoxes`, wykrywany już w przebiegu pierwszym), pudełko `fixed` pojawiające się po raz pierwszy na
   stronie dalszej, pole marginesowe o ujemnym z-index, cel bez wznawiania treści strony oraz
   `target-counter(…, pages)` (punkt 3). Pomniejszanie treści szerszej niż strona (ADR 0014) działa
   w obu przebiegach: przebieg w skali też jest dwuprzebiegowy.
7. `layout.TwoPassDisabled`, `layout.TwoPassMinPages` i `document.OnTwoPass` służą testom i pomiarom;
   API publiczne się nie zmienia.

## Wyniki

Pomiar 2026-11-21, Apple M4, wiersz poleceń, `/usr/bin/time -l`, po trzy przebiegi całego procesu;
porównanie z commitem `44f785b`. Warianty ze spisem treści robi skrypt (poza repozytorium) z
`1342-bench.htm` i `x4.htm`: na początku lista 61 odnośników do rozdziałów z
`a::after { content: " — strona " target-counter(attr(href), page) }` i `break-after: page`, a przy
każdym nagłówku rozdziału `h2::after { content: "Strona " counter(page) " z " counter(pages) }`.

| dokument | stron | szczyt RSS, przed | szczyt RSS, po | czas, przed | czas, po |
|---|---|---|---|---|---|
| książka bez spisu (ścieżka strumieniowa) | 256 | 445–467 MB | 440–483 MB | 1,59–1,61 s | 1,60–1,84 s |
| książka ze spisem i „Strona X z Y” | 259 | 598–613 MB | 472–479 MB | 2,24 s | 3,00–3,02 s |
| książka ×4 bez spisu | 1021 | 770–845 MB | 756–768 MB | 6,39–6,40 s | 6,40–6,42 s |
| książka ×4 ze spisem i „Strona X z Y” | 1027 | 1450–1531 MB | 802–839 MB | 8,93–8,94 s | 12,27–12,34 s |
| `18-toc-running-elements.html` (krótki, punkt 5) | 6 | 125–127 MB | 119–123 MB | 0,07 s | 0,06–0,07 s |

- Pamięć dokumentu ze spisem treści jest teraz o 3–8% wyższa niż bez spisu (dawniej o 35% i 85%);
  nie rośnie z liczbą stron. Czas rośnie o jeden układ: +0,8 s (35%) przy 259 stronach, +3,4 s (38%)
  przy 1027.
- Najwolniejszy przebieg „po” (1,84 s) to pierwsze uruchomienie nowej binarki.
- Wynik bajt w bajt ten sam: obie książki ze spisem, bez spisu, korpus W3C (11 689 dokumentów,
  46 756 renderowań, 0 różnic) z progiem z punktu 5 i z progiem 0 (drugi przebieg także dla dokumentów
  krótkich), `tests/twopass21_test.go` (15 dokumentów z tymi cechami, oba progi, z liczbą ostrzeżeń) i
  `tests/streamdraw18_test.go`.
- Pierwsza wersja odtwarzała ostrzeżenia w `pageDone` przez `defer`; kosztowało to 0,3 s (5%) także na
  ścieżce strumieniowej bez spisu. Wywołanie przez osobną funkcję (`finishPage`) tego kosztu nie ma.

## Konsekwencje

- Dokument ze spisem treści albo z `counter(pages)` w treści ma pamięć szczytową bliską dokumentowi bez
  nich, kosztem jednego układu więcej (+35–38%). Krótkie dokumenty (do 32 stron) bez zmian.
- Obiegi paginacji starej drogi powtarzają się bez potrzeby: kotwica ułożona od nowa oznacza stronę
  z odwołaniem bez porównania wartości (`TODO` w `CacheTargetPageCounters`), więc książka ze spisem
  treści robi 8 obiegów (limit), choć wartości ustalają się po drugim. To ok. 0,4 s w obu drogach.
  Porównanie wartości przed oznaczeniem skróciłoby oba przebiegi, ale zmienia algorytm przejętego kodu;
  osobna decyzja.
- `target-counter(…, pages)` pokazuje liczbę stron z obiegu, w którym kotwicę ułożono, czyli zwykle 0
  (dokument 3-stronicowy: „3/0”). CSS Generated Content 3 (`target-counter()`) każe podać wartość licznika
  w miejscu celu, a liczba stron jest wszędzie ta sama. Poprawka zmienia wynik, więc jest osobną decyzją;
  do tego czasu takie dokumenty idą starą drogą.

## Dopisek 2026-11-21: liczniki kotwic (pytanie 27)

Decyzja użytkownika z 2026-11-21: naprawić obie sprawy z ostatnich dwóch punktów „Konsekwencji”.

1. Strona z odwołaniem do kotwicy jest oznaczana do ponownego układu (i treść odwołania przeliczana) tylko
   wtedy, gdy liczniki stron kotwicy rzeczywiście się zmieniły (`CacheTargetPageCounters`). Treść odwołania
   zależy wyłącznie od tych liczników i od liczników elementu kotwicy, które są stałe, więc wynik jest bajt
   w bajt ten sam. Pozostałe znaczniki (`remakePage`: zmiana stanu następnej strony; `makePage`: zmiana
   liczników stron samego pudełka, `PagesWanted`) już porównywały wartości albo zależą od liczby stron
   porównywanej w `paginate`.
2. `target-counter(…, pages)` podaje liczbę stron dokumentu. Po każdym obiegu paginacji
   `TargetCollector.UpdateTargetPages` wpisuje ją do liczników wszystkich kotwic; odwołania z `pages`, których
   wartość się zmieniła, są przeliczane, a ich strony oznaczane do ponownego układu. Dokument z takim
   odwołaniem idzie teraz drogą dwuprzebiegową: `checkAnchor` porównuje wszystkie liczniki kotwicy, także
   `pages` (punkt 3 i 6 decyzji w tym zakresie nieaktualne).

Obiegi paginacji książki ze spisem treści i „Strona X z Y”: 8 (limit) → 3. Trzeci obieg jest potrzebny:
w drugim strony z `counter(pages)` są układane z treścią przeliczoną po pierwszym (liczba stron 0)
i dopiero po ułożeniu dostają liczbę stron (`ParseAgain` w `makePage`). Sam spis treści bez
`counter(pages)`: 2 obiegi przed i po.

Pomiar jak wyżej (Apple M4, `/usr/bin/time -l`, trzy przebiegi), porównanie z `f397bf0`:

| dokument | stron | obiegi, przed → po | szczyt RSS, przed | szczyt RSS, po | czas, przed | czas, po |
|---|---|---|---|---|---|---|
| książka bez spisu (ścieżka strumieniowa) | 256 | — | 431–452 MB | 439–469 MB | 1,61–1,63 s | 1,62–1,63 s |
| książka ze spisem i „Strona X z Y” | 259 | 8 → 3 | 467–478 MB | 461–493 MB | 3,04–3,06 s | 2,72–2,76 s |
| książka ×4 ze spisem i „Strona X z Y” | 1027 | 8 → 3 | 804–889 MB | 834–931 MB | 12,40–12,44 s | 10,88–10,92 s |

- Czas dokumentu ze spisem: −0,3 s (−10%) przy 259 stronach, −1,5 s (−12%) przy 1027; pamięć bez zmian
  (rozrzut między przebiegami większy niż różnica).
- Wynik bajt w bajt ten sam dla obu książek ze spisem, książki bez spisu, korpusu `testdata/documents`
  i korpusu W3C (11 689 dokumentów, 46 756 renderowań, 0 różnic). Zmieniają się tylko dokumenty z `target-counter(…, pages)`; w korpusach takich
  nie ma, przypadki są w `tests/twopass21_test.go` (`TestTargetCounterPages`, odwołania w przód i wstecz,
  w obu drogach).

## Dopisek 2026-11-23: regresja po 438a2ef i dalsze ciągi pudełek poza przepływem

Od `438a2ef` (przerwany układ zamyka kontekst formatowania pudełka, które go otworzyło) książka ze spisem
treści wracała do starej drogi: przebieg drugi zgłaszał `anchor "page_216" moved to page 129`. Sama zmiana
z `438a2ef` jest poprawna; przesunęła tylko elementy pływające tak, że na stronach z `counter(pages)`
(układanych od nowa w obiegu drugim i trzecim) inicjał rozdziału przechodził na następną stronę (`pushFloat`).
To odsłoniło trzy usterki starszych obiegów paginacji, wspólne dla obu dróg:

1. Dalsze ciągi pudełek poza przepływem (`brokenOutOfFlow`) są wejściem następnej strony, ale nie należały do
   stanu w `pageMaker`. Strona ułożona od nowa po stronie wziętej z obiegu wcześniejszego dostawała je od
   ostatniej strony ułożonej w tym obiegu: inicjał przeniesiony ze strony 125 stawał drugi raz na górze strony
   129. Do tego strona, która teraz przenosiła element pływający, nie oznaczała następnej do ponownego
   układu, bo miejsce wznowienia się nie zmieniało; element ginął.
2. `pushedFloatsPlaced` zachowywał wpisy strony z jej wcześniejszego układu, więc przepływ pomijał element jako
   już ustawiony.
3. Przeniesiony obraz był układany w miejscu, na pudełku ze źródła; późniejszy układ poprzedniej strony
   przestawiał go na stronie już ułożonej (obraz na dole strony, tekst opływał puste miejsce u góry).

Przebieg drugi układa strony po kolei, więc te usterki go nie dotyczą; różnicę względem przebiegu pierwszego
wykrywał `checkAnchor` albo `remakePage`. Poprawka (`remakePage`, `makeAllPages`, `sameBrokenOutOfFlow`,
`makePage`): stan zostawiony przez stronę jest zapamiętywany (`brokenAfterPage`), porównywany przy ponownym
układzie (zmiana oznacza następną stronę) i odtwarzany za stroną wziętą z obiegu wcześniejszego; wpisy
`pushedFloatsPlaced` strony są czyszczone przed jej układem; przeniesiony element pływający jest układany jako
kopia. Strona ułożona od nowa jest teraz funkcją tego samego stanu co w układzie po kolei.

Wynik zmienia się w dokumentach z ponowną paginacją i elementami pływającymi przeniesionymi na następną stronę,
na korzyść: w książce ze spisem treści 18 stron (inicjały podwójne albo zgubione, tekst nachodzący na
ilustracje). Test `tests/twopassfloats23_test.go`: dokument ponad 32 stron ze spisem `target-counter`,
„strona X z Y”, przenoszonymi obrazami, tabelą dzieloną obok elementu pływającego i kontenerem kolumn obok
niego idzie drogą dwuprzebiegową bez przeszkody i daje te same bajty co układ całości przed rysowaniem; przed
poprawką kończył się przeszkodą `anchor "r1" moved to page 6`.

Pomiar 2026-11-23, Apple M4, wiersz poleceń, `os.wait4` w Pythonie (szczyt RSS), trzy przebiegi, porównanie
z `de62591` (dla porządku: według pomiaru koordynatora `b16fea8` miał 2,66 s i 0,44 GB dla książki ze spisem):

| dokument | stron | szczyt RSS, przed | szczyt RSS, po | czas, przed | czas, po |
|---|---|---|---|---|---|
| książka bez spisu (ścieżka strumieniowa) | 254 | 451–465 MB | 430–458 MB | 1,24–1,29 s | 1,23–1,27 s |
| książka ze spisem i „Strona X z Y” | 258 | 574–604 MB | 462–488 MB | 3,37–3,60 s | 2,52–2,91 s |
| książka ×4 bez spisu | 1013 | 789–805 MB | 795–809 MB | 4,81–5,07 s | 5,01–6,13 s |
| książka ×4 ze spisem i „Strona X z Y” | 1023 | 1434–1521 MB | 825–833 MB | 11,35–13,04 s | 8,96–9,08 s |

- Książka ze spisem treści znów idzie drogą dwuprzebiegową; pamięć wraca do poziomu sprzed `438a2ef`.
- Bajty bez zmian: książka bez spisu i jej czterokrotność, korpus `testdata/documents` i `testdata/svg`
  (`bench/verify/corpus.sh`, fail=0), korpus W3C (11 689 dokumentów, 46 756 renderowań, 0 różnic).

## Dopisek drugi 2026-11-23: przypisy w obiegach paginacji

Przypisy przeniesione na następną stronę (`reportedFootnotes`, CSS GCPM § 2) to drugi stan wejściowy strony
spoza `pageMaker`, obok `brokenOutOfFlow` z dopisku wyżej, i miał tę samą usterkę: strona ułożona od nowa po
stronie wziętej z obiegu wcześniejszego dostawała przypisy przeniesione przez ostatnią stronę ułożoną w tym
obiegu. Krótki dokument (18 stron) ze spisem `target-counter`, „strona X z Y” i długimi przypisami rósł
z obiegu na obieg (18, 17, 20 stron), aż `makeAllPages` kończył się paniką „index out of range”; dłuższe
gubiły albo powtarzały przypisy. Poprawka jak dla `brokenOutOfFlow`: przypisy przeniesione przez każdą stronę
są zapamiętywane (`reportedAfterPage`), porównywane w `remakePage` (zmiana oznacza następną stronę) i
odtwarzane w `makeAllPages` za stroną wziętą z obiegu wcześniejszego, także jako ostatnią (wtedy powstaje
strona z przeniesionymi przypisami).

Przy sprawdzaniu treści (`pdftotext`: każdy przypis dokładnie raz, w kolejności, znacznik i wywołanie o tym
samym numerze) wyszły trzy usterki niezależne od paginacji, obecne także w układzie po kolei: drugi i dalsze
przypisy wiersza, w którym pierwszy przeszedł na następną stronę, ginęły (`break` poza gałęziami
`footnote-policy`); z kilku przeniesionych przypisów, z których drugi lub dalszy już się nie mieścił, ginęły
następne albo układ kończył się paniką (wycinek złej listy); przypis przeniesiony, żeby zmieścił się wiersz,
stawał za późniejszymi (kolejność 27, 25, 28). Do tego kilka długich przypisów przeniesionych razem wychodziło
nad górną krawędź strony (obszar bez `max-height` nie zgłaszał przepełnienia); teraz przechodzą dalej.
Szczegóły w `THIRD_PARTY.md`.

Test `tests/footnotes23_test.go`: dokument krótki (stara droga i droga dwuprzebiegowa z progiem 0) i ponad
32 stron (droga dwuprzebiegowa bez przeszkody) z krótkimi przypisami, długimi i po trzy długie w wierszu;
obie drogi dają te same bajty, treść przypisów jest pełna i w kolejności, a tekst każdej strony jest równy
dokumentowi bez ponownej paginacji (wartości spisu i `counter(pages)` wpisane wprost). Przed poprawką krótki
dokument kończył się paniką. Porównania w `remakePage` test nie wymusza (zmiana przenoszonych przypisów przy tym samym
miejscu wznowienia następnej strony w nim nie występuje; bez porównania test przechodzi, bez odtwarzania nie);
to zabezpieczenie na wzór `sameBrokenOutOfFlow`.

Sprawdzenia 2026-11-23, wobec `18fa545`: korpus W3C 11 689 dokumentów, 46 756 renderowań, 0 różnic (w nim
nie ma dokumentów, w których przypisy przechodzą na następną stronę); `bench/verify/corpus.sh` fail=0
(w `testdata/documents` przypisów nie ma); reftesty 310 z 340 (ściśle 308, z dokładnością do krawędzi 2), bez
zmian.

## Plan: tańszy przebieg drugi (2026-11-23; wykonany inaczej, zob. dopisek trzeci)

Przebieg drugi układa dziś od nowa wszystkie strony, choć część z nich ma wynik znany już z pierwszego:
strony przed pierwszą, na której liczniki stron albo odsyłaczy dają inną treść niż w obiegu, z którego
pochodzą. Koszt: książka ze spisem treści 2,5 s wobec 1,2 s bez spisu, ×4 8,8 s wobec 4,7 s.

1. Pomiar: ile stron przebiegu drugiego daje układ identyczny z pierwszym (stan wejścia strony, stan
   wyjścia, treść zależna od liczników); osobno dla spisu na początku dokumentu i dla „strona X z Y”
   w treści każdej strony.
2. Ponowne użycie wyniku pierwszego przebiegu dla stron, których wejście (stan `pageMaker`, `brokenAfterPage`,
   przypisy, wartości liczników użyte na stronie) jest takie samo: zamiast układu — rysowanie zachowanego
   drzewa strony. Wymaga zachowania drzew takich stron do przebiegu drugiego, czyli kompromisu z pamięcią
   (ADR 0027); próg albo okno ostatnich stron do ustalenia pomiarem.
3. Ponowny układ tylko akapitów z treścią zależną od liczników (`content: counter(pages)`, `target-counter`),
   gdy ich szerokość się nie zmienia (ta sama liczba cyfr): wiersz z liczbą, bez reszty strony.
4. Kryterium: bajty bez zmian (porównanie korpusu W3C i książek), pamięć szczytowa nie większa niż dziś
   o więcej niż 10%, czas książki ×4 ze spisem bliżej 4,7 s niż 8,8 s.

## Dopisek trzeci 2026-11-23: odkładanie stron zamiast drugiego przebiegu

### Pomiar (krok 1 planu)

Przebieg drugi daje z założenia te same strony co mieszanka stron z obiegów przebiegu pierwszego (każdą
różnicę zgłasza `drift`), więc pytanie z planu sprowadza się do tego, ile stron końcowego wyniku pochodzi już
z obiegu pierwszego, czyli z układu po kolei bez znajomości liczników. Policzone z numerów stron układanych
od nowa w każdym obiegu (`makeAllPages`, sonda poza repozytorium), 2026-11-23. Warianty „tylko spis” i „tylko
strona X z Y” to `1342-toc.htm` i `x4-toc.htm` bez reguły `h2::after` albo bez `nav.toc` (skrypt poza
repozytorium):

| dokument | stron | ułożone od nowa w obiegu 2 / 3 | strony końcowe z obiegu 1 |
|---|---|---|---|
| książka, tylko spis | 256 | 2 / — | 254 (99,2%) |
| książka, tylko „strona X z Y” | 256 | 63 / 63 | 193 (75,4%) |
| książka, spis i „strona X z Y” | 258 | 65 / 63 | 193 (74,8%) |
| ×4, tylko spis | 1015 | 2 / — | 1013 (99,8%) |
| ×4, tylko „strona X z Y” | 1021 | 252 / 252 | 769 (75,3%) |
| ×4, spis i „strona X z Y” | 1023 | 254 / 252 | 769 (75,2%) |

Stron ułożonych od nowa jest zawsze tyle, ile stron z odwołaniem (strony spisu, strony z nagłówkiem
rozdziału); żadna z nich nie zmienia stanu następnej strony, więc dalsze strony nigdy nie są układane od nowa.

Profil czasu ×4 ze spisem (9,0 s): przebieg strumieniowy przerwany na stronie 11 0,5 s, budowa drzewa pudełek
od nowa 0,45 s, obieg 1 przebiegu pierwszego 3,1 s, obiegi 2 i 3 0,7 s, przebieg drugi (układ i rysowanie)
4,0 s. W układzie 80% czasu to łamanie wierszy (`splitFirstLine`, kształtowanie tekstu).

### Rozważone

- Krok 2 planu, zachowanie drzew stron z przebiegu pierwszego do rysowania w drugim. Drzewa całego dokumentu
  ×4 to 274 MB żywej pamięci (558 wobec 284 MB po GC, profil sterty na końcu przebiegu pierwszego), 1,30 GB
  wobec 0,83 GB szczytu RSS. Próg 10% pamięci mieści ok. 150 stron, czyli oszczędza ok. 15% przebiegu drugiego
  przy ×4; za mało.
- Pamięć wyników łamania wierszy między przebiegami (`text.layoutCache` bez limitu): klucz to cała pozostała
  treść akapitu, więc pamięć rośnie z kwadratem długości akapitu; ×4 bez spisu 15 s i 0,89 GB zamiast 4,7 s.
  Odrzucone, do tego to obszar `internal/webrender/text`.
- Krok 3 planu, ponowny układ tylko akapitów z licznikami: dotyczy obiegów 2 i 3 (0,7 s z 9 s), nie przebiegu
  drugiego. Odłożone.
- Rysowanie w obiegu pierwszym stron, które są już ostateczne, i odłożenie pozostałych. Strony ostateczne to
  75–99,8% (tabela); ich treść narysowana to kilka MB, nie drzewo pudełek. Wybrane.

### Decyzja

1. Pierwszy przebieg strumieniowy (ADR 0027) nie kończy się przy pierwszej potrzebie ponownej paginacji, tylko
   odkłada strony, które mogą się jeszcze zmienić (`streamer.deferPage`): stronę oznaczoną do ponownego układu
   (`ContentChanged`, `PagesWanted`) i stronę z treścią z `target-counter()` (`RemakeState.ContentLookups`
   z odwołaniami do kotwic). Pozostałe strony rysuje od razu. Strona odłożona dostaje miejsce w celu w kolejności
   stron (`DeferringSink.Reserve`, `streamSink.Reserve`), a jej drzewo jest zwalniane, gdy wiadomo, że następny
   obieg ułoży ją od nowa.
2. Po obiegu pierwszym przebieg wykonuje dalsze obiegi paginacji jak `paginate` (`streamer.finishDeferred`),
   zachowując ostatni układ każdej strony odłożonej, a potem rysuje strony odłożone w kolejności stron w ich
   miejscach w celu. Pola marginesowe, odnośniki i zakładki jak dotąd po wszystkich stronach. Wynik to ta sama
   mieszanka stron co w starej drodze (`layoutDocument`), bez drugiego układu całego dokumentu i bez budowy
   drzewa pudełek od nowa.
3. Przeszkody, przy których przebieg kończy się potrzebą ponownej paginacji i dokument idzie dwoma przebiegami,
   jak dotąd (punkt 1 decyzji): strona narysowana, którą dalszy obieg układa od nowa albo oznacza; zmiana liczby
   stron; strona odłożona o innej wysokości albo innych wymiarach strony niż przy rezerwacji (granica form masek
   miękkich zależy od wymiarów ostatnio dodanej strony, `pdf.Output.AddPage`); inna geometria elementu głównego
   pierwszej strony (początek tła płótna); pudełka `fixed`; treść szersza niż strona (ADR 0014); `string-set`
   i `bookmark-label` z `counter(pages)` albo `target-counter()`; zakładka elementu, którą wzięła strona dalsza
   (`pageFinisher.outOfOrder`); PDF z tagami (drzewo struktury zależy od kolejności rysowania); więcej
   zachowanych drzew niż `64 + strony/3` (`maxHeldPages`, dokument z odwołaniami na każdej stronie).
4. Kolejność rysowania stron wpływa na wynik w jednym miejscu: znaki przypisane glifowi w ToUnicode bierze pierwsze
   rysowanie glifu (`draw_gotext.go`). Rysowanie zlicza glify rysowane ze znakami innymi niż już przypisane
   (`backend.FontChars.Conflicts`, `pdf.Output.GlyphConflicts`); wzrost licznika przy rysowaniu stron
   odłożonych to przeszkoda z punktu 3. Pozostałe skutki rysowania (numery obiektów, zasoby, formy wspólne,
   obrazy, szerokości glifów) nie zależą od kolejności: numery obiektów nadaje zapis pliku w kolejności stron.
5. Ostrzeżenia: te same co dotąd i tyle samo; ostrzeżenia z rysowania stron odłożonych przychodzą po ostrzeżeniach
   stron dalszych.
6. Dwa przebiegi zostają dla przypadków z punktu 3 i dla przebiegu w skali (ADR 0014). `layout.DeferDisabled`
   wyłącza odkładanie (testy, pomiary), `layout.OnRepaginate` i `document.OnDeferred` zgłaszają drogę.
   Odkładanie działa też dla dokumentów krótkich (do `TwoPassMinPages`), które dotąd były układane w całości
   przed rysowaniem.

### Wyniki

Bajty bez zmian wobec `e0a2934`: cztery warianty książki, korpus W3C (11 689 dokumentów, 46 756 renderowań,
0 różnic; w nim także krótkie dokumenty z ponowną paginacją idą teraz odkładaniem), `bench/verify/corpus.sh`
fail=0, `tests/twopass21_test.go` (każdy przypadek odkładaniem i dwoma przebiegami, z liczbą ostrzeżeń),
`tests/twopassfloats23_test.go`, `tests/footnotes23_test.go`, `tests/streamdraw18_test.go`. Reftesty 310 z 340
(ściśle 308, z dokładnością do krawędzi 2), bez zmian.

Pomiar 2026-11-23, Apple M4, wiersz poleceń, `os.wait4` w Pythonie, 12 uruchomień każdej binarki na przemian.
Komputer był obciążony innymi programami (średnie obciążenie 5–60), więc czasy bezwzględne są o 20–100% wyższe
niż na komputerze bez innej pracy (wtedy 1,24 / 2,34 / 4,69 / 9,06 s przed zmianą); miarodajny jest stosunek
czasu wariantu ze spisem do wariantu bez spisu w tej samej rundzie:

| dokument | szczyt RSS, przed | szczyt RSS, po | czas, przed | czas, po | ze spisem / bez spisu, przed → po |
|---|---|---|---|---|---|
| książka bez spisu | 437–462 MB | 429–470 MB | 1,43–2,62 s | 1,53–2,73 s | — |
| książka ze spisem i „Strona X z Y” | 463–486 MB | 440–478 MB | 2,74–5,61 s | 1,71–3,49 s | 1,8–2,4 → 1,0–1,3 |
| ×4 bez spisu | 775–808 MB | 766–813 MB | 5,66–10,13 s | 5,50–10,43 s | — |
| ×4 ze spisem i „Strona X z Y” | 781–841 MB | 780–842 MB | 11,08–20,45 s | 6,42–13,10 s | 1,8–2,0 → 1,1–1,3 (mediana 1,17) |

- Czas ×4 ze spisem na nieobciążonym komputerze wynosi według stosunku z tabeli ok. 5,5 s (szacunek: 4,69 s
  × 1,17) zamiast 9,1 s; książka ze spisem ok. 1,4 s zamiast 2,3 s. Zostaje koszt obiegów 2 i 3 oraz
  ponownego układu stron odłożonych (25% stron przy „strona X z Y”).
- Pamięć szczytowa bez zmian (rozrzut między uruchomieniami większy niż różnica); ×4 ze spisem trzyma naraz do
  254 drzew stron odłożonych.

### Konsekwencje

- Drogą dwuprzebiegową idą już tylko dokumenty z punktu 3; testy sprawdzają obie drogi.
- Krok 3 planu (ponowny układ tylko akapitów z licznikami) i skrócenie obiegów 2 i 3 to dalsze możliwe
  oszczędności (ok. 0,7 s przy ×4); bez decyzji.
- Przypadek `target-counter-pages-backward` z `tests/twopass21_test.go` (odwołania wstecz z
  `target-counter(…, pages)`): `UpdateTargetPages` oznacza po obiegu pierwszym stronę 1, choć odwołania są na
  ostatniej; strona 1 jest już narysowana, więc dokument idzie dwoma przebiegami. Prawdopodobnie nieaktualny
  `PageMakerIndex` odwołania w przejętym kodzie; do zbadania osobno.

## Dopisek czwarty 2026-11-23: odwołania wstecz z `target-counter(…, pages)`

Przyczyna (przypadek `target-counter-pages-backward` z sekcji „Konsekwencje” dopisku trzeciego): nie
nieaktualny `PageMakerIndex`, tylko krok 3 rozwiązywania liczników w `makePage` (`html/layout/pages.go`, w
WeasyPrint `layout/page.py`, „targeted counters”). Dla odwołania z `pages` do kotwicy już ułożonej oznaczał on
`PagesWanted` na stronie **kotwicy**, nie odwołania: ponowny układ strony kotwicy po zmianie liczby stron miał
wpisać nową liczbę do jej liczników stron, a `CacheTargetPageCounters` oznaczyć wtedy strony odwołań. U nas
liczbę stron wpisuje do liczników wszystkich kotwic po każdym obiegu `UpdateTargetPages` (dopisek z
2026-11-21) i sama oznacza strony odwołań (`ContentChanged`), więc ponowny układ strony kotwicy niczego nie
zmieniał. Przy odkładaniu stron oznaczał jednak stronę już narysowaną (tu stronę 1 z `#r0`), co jest
przeszkodą z punktu 3 decyzji dopisku trzeciego.

Decyzja: krok 3 nie oznacza strony kotwicy. Warunek ponownego obiegu się nie zmienia: `PagesWanted` strony
kotwicy dawał obieg wtedy, gdy zmieniła się liczba stron, a wtedy `UpdateTargetPages` oznacza stronę odwołania
(liczba stron w licznikach kotwicy jest po każdym obiegu liczbą stron dokumentu). Strona kotwicy nie jest już
układana od nowa; jej stan wejścia się nie zmienia, więc układ byłby ten sam.

Wyniki: bajty bez zmian wobec `505fabd` (korpus W3C 11 689 dokumentów, 46 756 renderowań, 0 różnic);
przypadek `target-counter-pages-backward` i nowy `target-counter-pages-backward-middle` (odwołanie wstecz w
środku dokumentu, z dalszymi stronami narysowanymi przed nim) idą odkładaniem (`deferWant = ""`; przed zmianą
oba kończyły się przeszkodą „content of page 1 changed after it was drawn”).
