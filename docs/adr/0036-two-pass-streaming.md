# ADR 0036: Dokumenty z ponowną paginacją układane w dwóch przebiegach

Status: przyjęta 2026-11-21.

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
