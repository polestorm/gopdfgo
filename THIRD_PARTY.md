# Kod przejęty z obcych projektów

Stan na 2026-11-12. Decyzja: `docs/adr/0002-webrender-base.md`. Pliki korpusu testowego mają osobny rejestr
w `testdata/THIRD_PARTY.md`. Nagłówki autorów i pliki licencji w przejętych katalogach są zachowane.

| Katalog | Źródło | Commit | Licencja |
|---|---|---|---|
| `internal/webrender/` | https://github.com/benoitkugler/webrender | `6e769b1` (2026-10-01) | BSD-3-Clause, `internal/webrender/LICENSE` |
| `internal/goweasyprint/` | https://github.com/benoitkugler/go-weasyprint | `e4c9509` (2026-10-01) | BSD-3-Clause, `internal/goweasyprint/LICENSE` |
| `internal/pdflib/` | https://github.com/benoitkugler/pdf, pakiety `model`, `contentstream`, `fonts`, `fonts/cmaps`, `fonts/standardfonts` | wersja `v0.0.15` | MIT, `internal/pdflib/LICENSE` |

## Zmiany względem źródła

Wspólne dla obu katalogów:

- ścieżki importu `github.com/benoitkugler/webrender` i `github.com/benoitkugler/go-weasyprint` zamienione na
  `github.com/polestorm/gopdfgo/internal/webrender` i `…/internal/goweasyprint`; kolejność importów po `gofmt`;
- usunięte `go.mod`, `go.sum`; `README.md` przemianowane na `README.upstream.md`.

`internal/webrender/`:

- usunięty silnik tekstu Pango, jedyne miejsce użycia `github.com/benoitkugler/textprocessing` (LGPL-2.1):
  pliki `text/engine_pango.go`, `text/draw/draw_pango.go`; z `text/text.go` usunięte `getNextBreakPoint`
  i `getNextBreakPointFromText`; z `text/draw/draw.go` gałęzie Pango; z `utils/testutils/fonts/fonts.go` gałąź Pango;
- usunięte testy porównujące go-text z Pango jako wzorcem: `text/text_test.go` (23 funkcje), `text/fonts_test.go`
  (9 funkcji); z `html/layout/layout_test.go` gałąź `TextLayoutPango`. Do odtworzenia jako testy z zamrożonymi
  wartościami;
- `text/draw/draw_gotext.go`: jeden obiekt `gotextFont` na parę (krój, rozmiar) zamiast nowego dla każdego ciągu
  tekstu; bez tego backend PDF osadzał font raz na ciąg (`docs/benchmark.md` pkt 3.1);
- `text/draw/draw_gotext.go`: `GlyphExtents.Width` to posunięcie glifu w tysięcznych firetu liczone z jednostek
  fontu (`HorizontalAdvance / Upem`), a nie szerokość obrysu dzielona przez 1024; ścieżka Pango używała
  szerokości logicznej. Bez tego tablica `/W` była błędna i tekst nie dawał się wyodrębnić.

- `calc()`, `min()`, `max()`, `clamp()` dla długości: nowe pliki `css/properties/calc.go` (drzewo wyrażenia
  i jego liczenie) oraz `css/validation/calc.go` (parser); `pr.Dimension` dostało pola `Calc` i `Offset`;
  `getLength` i walidator `line-height` przyjmują te funkcje; `length_` w `html/tree/computed_values.go` sprowadza
  wyrażenie do pikseli albo do procentu z dodatkiem w pikselach; `pr.ResolvePercentage` dolicza dodatek;
- jednostki `vw`, `vh`, `vmin`, `vmax` względem obszaru pierwszej strony: `css/properties/datas.go`, `types.go`,
  `css/validation/validation.go` (`LENGTHUNITS`), `html/tree/style.go` (typ `viewport`, liczony leniwie ze stylu
  strony domyślnej), `html/tree/computed_values.go`;
- właściwości logiczne zamieniane na fizyczne przed walidacją (nowy plik `css/validation/logical.go`, wywołanie
  w `PreprocessDeclarationsPrelude`); `print-color-adjust: exact` przyjmowane bez ostrzeżenia;
- `html/layout/grid.go`: pierwszy wiersz siatki niemieszczący się na pustej stronie zostaje na niej; wcześniej
  silnik tworzył strony w nieskończoność (TODO z oryginału);
- strony nazwane, `html/layout/block.go` i `html/tree/target.go`: `pageNameChanges` zamiast `blockLevelPageName`
  oraz pole `PageBreak.HasPage`. W źródle pusta nazwa znaczyła zarazem "nieustalona" i "strona domyślna", więc po
  stronie nazwanej treść domyślna nie zaczynała nowej strony albo dziedziczyła jej nazwę;
- `html/tree/style.go`: z treści elementu `style` zdejmowana jest sekcja `<![CDATA[ … ]]>` (dokumenty XHTML);
- `html/document/stacking.go`: `InlineGridBox` tworzy kontekst nakładania jak `InlineBlockBox` i `InlineFlexBox`;
  bez tego rysowanie `display: inline-grid` kończyło się paniką;
- `html/layout/grid.go`: funkcja `pySlice` (reguły wycinków Pythona) w miejscach, gdzie port polegał na nich
  niejawnie; współrzędne elementów przesuwane o początek ścieżek niejawnych przy pozycjonowaniu; dwie asercje
  „expected span” odwrócone względem WeasyPrint poprawione; zakres sprawdzany przy przypisywaniu elementów do
  ścieżek; `html/layout/block.go`: siatka przeniesiona w całości na następną stronę nie powoduje paniki;
- `html/layout/flex.go`: automatyczna wysokość minimalna elementu flex liczona tylko w osi głównej oraz dla
  elementów zastępowanych; w źródle zawsze, co wymagało trzeciego pełnego układu dziecka na każdym poziomie;
- `html/layout/flex.go` (CSS Flexbox 1): krok 9.7.5d przycina rozmiar docelowy do minimum i maksimum i zapisuje
  korektę ze znakiem, więc miejsce odebrane elementowi z `max-width` wraca do pozostałych (w źródle wykrywane było
  tylko naruszenie minimum); pętla zamrażania kończy się także przy nieskończonym wolnym miejscu (NaN);
  `wrap-reverse` zamienia początek i koniec osi poprzecznej przez odbicie pozycji elementów, przy czym elementy
  wyrównane do linii bazowej odbijane są jako grupa (w źródle odwracana była tylko kolejność linii);
  `flex-basis: content` pomija podany rozmiar w osi głównej (§ 7.2.1);
- `html/layout/flex.go`: automatyczne minimum elementu flex (§ 4.5) jest ograniczane przez określone maksimum
  i wynosi zero dla elementów z `overflow` innym niż `visible` (poza elementami zastępowanymi); minimum
  i maksimum wysokości przeniesione przez proporcje obrazu obowiązują w kroku 9.7.5d tylko w osi poziomej;
- słowa kluczowe rozmiaru wewnętrznego `min-content`, `max-content`, `fit-content` (CSS Sizing 3 § 2.1), w źródle
  odrzucane przy walidacji: `css/properties/types.go` (`TaggedDim.Intrinsic`, `IntrinsicSize`),
  `css/validation/validation.go` (`width`, `height`, `min-*`, `max-*`), nowy `html/layout/intrinsic.go`
  (`resolveSizes`, `resolveIntrinsicSizes`) wywoływany zamiast `resolvePercentages` w układzie blokowym, flex,
  grid, pływającym, bezwzględnym i inline-block; `html/layout/preferred.go` uwzględnia je przy pomiarze dzieci.
  Dla wysokości słowa te działają jak `auto`; funkcja `fit-content(<długość>)` poza siatką nie jest obsługiwana;
- `html/layout/column.go`, nowy `html/boxes/column_span.go`, `html/document/draw_border.go` (CSS Multicol 1
  § 6.1): blokiem zawierającym element z `column-span: all` jest kontener kolumn (w źródle blok nadrzędny
  kontenera, więc element miał złą szerokość); tabela z `column-span: all` rozpina kolumny (właściwość stoi na
  pudełku tabeli, a dzieckiem kontenera jest opakowanie); elementy pływające i pozycjonowane nie rozpinają;
- liczniki (CSS Lists 3), `html/boxes/build.go` (`UpdateCounters`, nowy `PopNestedCounters`): kolejność
  `counter-reset`, `counter-increment`, `counter-set` (§ 4.5; w źródle `counter-set` przed `counter-increment`);
  licznik utworzony przez element, którego przodek ma już licznik o tej nazwie, obowiązuje tylko w tym elemencie
  i jego potomkach, a nie u następnego rodzeństwa (§ 4.3; w źródle zachowanie z CSS 2.1);
- atrybuty list, `html/tree/style.go`: `ol start` ustawia licznik bez zmiany `counter-increment` listy, nowy
  `ol reversed` (wartość początkowa według § 4.4.2, elementy odliczają w dół), `li value` przez `counter-set`
  (w źródle `value` był sprawdzany na `ul` zamiast na `li`, czyli nie działał);
- testy odziedziczone zmienione według specyfikacji: `TestCounters4` (`html/boxes/counters_test.go`, było 1, 2,
  2.1, 2.2, 3; jest 1–5) i `TestTargetCounters` (`html/layout/layout_target_test.go`, było 4.2, 3, b.a, 12; jest
  7, 5, c.a, 2), oba z powodu zasięgu licznika z § 4.3;
  Uwaga: testy W3C są tu ze sobą sprzeczne. `css-lists/counter-001…003` i `counters-001…` wymagają zasięgu
  opisanego wyżej (zmiana w CSS Lists 3 po 9 lipca 2020: liczniki brane najpierw od rodzica, od rodzeństwa tylko
  nowe nazwy), a starsze `CSS2/syntax/counters-010` i `css-lists/counter-list-item-2` zachowania z CSS 2.1.
  Przyjęte jest nowsze; dwa starsze testy nie przechodzą;
- `text/text.go` (`ContentAreaHeight`), `html/layout/inline.go`: wysokość pudełka liniowego to wydłużenie górne
  i dolne pierwszego dostępnego fontu (CSS Inline 3 § 4.1); w źródle rozmiar fontu, więc tło i obramowanie
  elementów liniowych były niższe niż wiersz (16 zamiast 22 px dla Noto Serif 16 px). Testy odziedziczone
  zmienione odpowiednio: `TestEmptyInlineAutoMargins`, `TestVerticalAlign7`, `TestVerticalAlign8`;
- `html/layout/inline.go` (`isPhantomLinebox`, nowy `isCollapsibleSpace`): wiersz zawierający tylko zwijalne
  odstępy ma zerową wysokość (CSS 2.1 § 9.4.2); w źródle taki wiersz między dwoma blokami wewnątrz elementów
  liniowych zajmował pełną wysokość wiersza;
- `html/layout/background.go`: tło płótna (przeniesione z `html` albo `body`) jest pozycjonowane względem
  elementu głównego (CSS Backgrounds 3 § 2.11.1); w źródle względem pudełka strony. Test odziedziczony
  `TestCanvasBackgroundSize` (`goweasyprint/pdf/draw_background_test.go`) przepisany: gradient na `html`
  powtarza się co wysokość elementu głównego, jak w przeglądarkach, zamiast rozciągać się na stronę;
- `html/layout/tables.go` (nowy `distributeExtraTableHeight`): tabela z `height` większą niż suma wierszy
  rozdziela nadmiar między wiersze (CSS Tables 3 § 3.10.5), komórki zachowują `vertical-align`; w źródle
  nadmiar zostawał pustym miejscem pod ostatnim wierszem. Tabela podzielona między strony nie jest rozciągana,
  a komórka z `rowspan` rośnie tylko o udział swojego pierwszego wiersza;
- `html/layout/float.go` (`avoidCollisions`): margines po stronie końca wiersza nie odbiera miejsca pudełku
  o podanej szerokości przy dopasowaniu obok elementu pływającego (CSS 2.1 § 10.3.3); w źródle pudełko
  spadało pod element pływający;
- `html/tree/computed_values.go` (`fontSize`): jednostki `ex`, `ch`, `lh` w `font-size` liczone z fontu rodzica
  (CSS Values 3 § 5.1.1); w źródle z fontu samego elementu, co dawało nieskończoną rekurencję i przepełnienie
  stosu (koniec procesu, nie do przechwycenia przez `recover`);
- `html/boxes/build.go` (`clampCounter`): wartości liczników ograniczone do 32 bitów ze znakiem, zwiększenie
  wychodzące poza zakres pomijane (CSS Lists 3 § 4.2); w źródle wartość spoza zakresu wpędzała style liczników
  w nieskończoną rekurencję stylów zastępczych (przepełnienie stosu);
- `pdflib/contentstream/images.go` (`parsePNGWithFallback`): PNG z przeplotem albo 16-bitowy jest dekodowany
  i kodowany ponownie jako 8-bitowy bez przeplotu; w źródle taki obraz kończył się błędem i znikał z dokumentu.
  Ograniczenie: przy tej ścieżce ginie rozdzielczość zapisana w pliku (`pHYs`);
- `html/layout/layout.go`, `background.go`: tło płótna biegnie ciągle przez strony (pozycjonowane względem
  elementu głównego z pierwszej strony, przesuniętego o wysokość stron poprzednich); w źródle zaczynało się
  od nowa na każdej stronie. Ograniczenie: pozycje procentowe liczone są od pierwszego fragmentu elementu
  głównego, nie od jego pełnej wysokości;
- stan bazowy reftestów druku: `css-page/page-background-002-print` i `-003-print` przestały przechodzić po
  dodaniu obsługi PNG z przeplotem. Wcześniej przechodziły pozornie (obraz był pomijany i w teście, i we
  wzorcu). Test jest dziś renderowany poprawnie, a nie zgadza się wzorzec: silnik nie kroi między strony
  treści pozycjonowanej bezwzględnie, która wystaje poza stronę;
- `xhtml.go` (kod własny, `stripNamespacePrefixes`): w dokumentach XHTML znaczniki z przedrostkiem związanym
  z przestrzenią nazw XHTML, SVG lub MathML (`<svg:svg>`) są zamieniane na nazwy bez przedrostka, zanim trafią
  do parsera HTML5, który przedrostków nie zna;
- podział bloku o podanej wysokości między strony (CSS Fragmentation 3 § 5.1): `html/tree/target.go`
  (`RemainderResume`, `ResumeStack.Remainder`: pozostała wysokość zapisana w stosie wznowienia jako klucz
  ujemny), `html/layout/block.go` (`canSplitFixedHeight`, obsługa w `blockContainerLayout`), `pages.go`
  (przejścia po stosie wznowienia pomijają taki wpis), `layout.go` (`pageHeight`). W źródle blok wyższy niż
  strona wystawał poza nią i dalsza część ginęła. Zakres i ograniczenia: ADR 0004. Testy odziedziczone `TestDrawBorderRadius` i `TestImagesRepeatTransparent`
  dostały `overflow: hidden`, bo sprawdzają rysowanie jednej strony blokiem wyższym niż strona;
- stan bazowy reftestów druku po tej zmianie: 156 z 340 (było 162). Dziewięć testów przestało przechodzić,
  trzy doszły. W każdym z dziewięciu jedna strona pary (test albo wzorzec) to zwykły wysoki blok, teraz
  dzielony poprawnie, a druga wymaga podziału między strony treści pozycjonowanej bezwzględnie, treści
  monolitycznej albo elementu pływającego we flex lub tabeli, czego silnik nie robi. Wcześniej obie strony
  pary wystawały poza stronę jednakowo: `monolithic-overflow-015`, `-027`, `-028`, `-029`, `fixedpos-003`,
  `overflowing-block`, `break-nested-float-in-flex-item-001`, `-002`, `break-nested-float-in-table-001`;
- treść pozycjonowana bezwzględnie między stronami: `html/layout/pages.go` (`makePage` zwraca wznowienie
  „po ostatnim dziecku”, gdy treść w przepływie się skończyła, a zostały podzielone pudełka poza przepływem),
  `block.go` (podział bloku o podanej wysokości obejmuje też bloki pozycjonowane bezwzględnie), `absolute.go`
  (pudełko przycięte przez blok zawierający nie tworzy stron). W źródle dokument kończył się z ostatnią
  stroną treści w przepływie i dalszy ciąg treści pozycjonowanej ginął. Testy odziedziczone zmienione
  odpowiednio (obraz o stronę lub dwie dłuższy): `TestAbsoluteSplit_8`, `TestAbsoluteSplit_9`,
  `TestAbsolutePagesCounter`, `TestAbsolutePagesCounterOrphans`. Nieobsługiwane: pudełko pozycjonowane
  w całości poniżej dolnej krawędzi strony (np. `bottom: -100vh`) nie trafia na późniejszą stronę;
- `css/validation/validation.go`: `overflow: clip` przyjmowane i traktowane jak `hidden` (bez odwzorowania
  różnicy w kontekście formatowania); w źródle odrzucane;
- stan bazowy reftestów druku: 160 z 340. Względem 156 doszło 7 testów, a 3 przestały przechodzić
  (`fixedpos-001`, `-002`, `-004`): test jest renderowany poprawnie (trzy strony), a wzorzec używa pudełek
  pozycjonowanych poniżej strony;
- krojenie elementu zastępowanego między strony: `html/layout/replaced.go` (`sliceTallReplaced`),
  `html/boxes/boxes.go` (`SliceFullHeight`, `SliceOffset`), `html/document/draw.go` (fragment rysowany jako cały
  obraz przesunięty i przycięty). Dotyczy obrazów blokowych w przepływie wyższych niż obszar strony; w źródle
  obraz wystawał poza stronę, a treść za nim ginęła. `break-inside: avoid` wyłącza krojenie. Nie są krojone
  obrazy liniowe (w wierszu tekstu), pływające, pozycjonowane ani elementy flex i grid. Testy odziedziczone
  `TestSvgSizing` i `TestSvgResizing` dostały `break-inside: avoid`, bo sprawdzają rozmiar obrazu wyższego
  niż strona na jednej stronie;
- `html/layout/preferred.go` (`tableAndColumnsPreferredWidths`): podana szerokość tabeli uwzględnia
  `box-sizing` (dla tabel w HTML domyślnie `border-box`), czyli obejmuje obramowanie i odstęp wewnętrzny;
  w źródle była brana jako szerokość treści i tabela wychodziła za szeroka. Test odziedziczony `TestTables_20`
  zmieniony odpowiednio (10 px razem z obramowaniem zamiast 12);
- `html/layout/block.go`: blok o podanej wysokości, który jest pierwszą treścią na stronie i nie mieści się
  w jej reszcie, jest dzielony (ADR 0004, punkt 2); w źródle wystawał poza stronę. Reftesty druku 160 → 162
  (`table-fragmentation-001a`, `-002a`);
- `html/layout/tables.go` (`forcedBodyLayout` w `allGroupsLayout`): zanim tabela straci nagłówek lub stopkę,
  bo żaden wiersz się z nimi nie zmieścił, wiersze są układane jeszcze raz z dopuszczeniem podziału zawartości
  komórek; wynik jest brany tylko wtedy, gdy treść mieści się nad stopką. W źródle wiersz wyższy niż strona
  zawsze zabierał tabeli nagłówek i stopkę;
- `html/layout/layout.go` (`setBrokenOutOfFlow`, `orderedBroken`), `pages.go`: pudełka poza przepływem
  podzielone między strony są kontynuowane w kolejności zgłoszenia; w źródle w kolejności iteracji po mapie,
  czyli losowo (przy dwóch takich pudełkach wynik zmieniał się między uruchomieniami);
- `html/tree/computed_values.go` (`computeTrackBreadth`, `borderImageOutset`): długości w jednostkach zależnych
  od fontu (`em`, `ex`, `ch`) liczone z fontu elementu; w źródle z rozmiarem 0 (błąd przy przenoszeniu z Pythona:
  `None` zamienione na zero), więc ścieżki siatki podane w `em` miały zerowy rozmiar, a elementy nakładały się.
  Reftesty druku 164 → 173;
- `html/tree/html5_ua.css` (arkusz stylów agenta): reguła `:is(input, …)::before` odzyskała `::before`, zgubione
  przy przenoszeniu z WeasyPrint, przez co każde pole `input` było blokiem na całą szerokość; dodane reguły:
  tekst zastępczy pustego pola (`placeholder`), zasłonięte hasło (stałe osiem kropek, nie zdradza długości),
  `summary` jako element listy ze znacznikiem i ukrycie treści zwiniętego `details` (HTML § 15.5.5). Dokument
  korpusu `13-forms-html-elements` ma teraz jedną stronę jak w Chromium. Nieobsługiwane: `legend` na ramce
  `fieldset`;
- `text/engine_gotext.go` (`shapePrecisely`): tekst jest kształtowany w rozmiarze 64 razy większym, a wynik
  dzielony, bo `go-text/typesetting` zaokrągla rozmiar fontu w górę do całych pikseli. W źródle tekst
  o rozmiarze niecałkowitym w pikselach (np. 10 pt = 13,33 px) miał szerokości znaków jak dla rozmiaru
  zaokrąglonego w górę (14 px), czyli do 5% za szeroko, co zmieniało łamanie wierszy. Do zgłoszenia upstream
  (webrender i go-text);
- `text/engine_gotext.go` (`applyTabs`): gdy do pozycji tabulatora brakuje mniej niż ok. pół znaku, tabulator
  sięga następnej pozycji (CSS Text 3 § 4.2); w źródle tekst kończący się na pozycji tabulatora dostawał
  tabulator o szerokości bliskiej zera;
- `html/boxes/build.go` (`ProcessTextTransform`): `text-transform: uppercase` i `lowercase` używają pełnego
  odwzorowania wielkości liter z Unicode (`golang.org/x/text/cases`; CSS Text 3 § 2.1), np. ß → SS; w źródle
  odwzorowanie proste. `capitalize` bez zmian: norma zostawia granice wyrazów przeglądarkom, a te się różnią
  (po łączniku Chromium zaczyna nowy wyraz, przejęty kod jak Firefox nie);
- `encoding.go` (kod własny): znacznik kolejności bajtów na początku dokumentu HTML jest zdejmowany, a UTF-16
  dekodowany, zanim dokument trafi do parsera; przedtem U+FEFF stawał się tekstem przed pierwszym elementem
  i przesuwał treść o jeden wiersz;
- `html/layout/column.go`, `pages.go`: cztery komunikaty diagnostyczne z `%s` dla `PageBreak` zmienione na `%v`
  (po dodaniu pola `HasPage` `go vet` blokował kompilację testów pakietu);
- testy stron nazwanych: `TestPageNames4` i `TestPageNames7` mają oczekiwania zgodne z CSS Page 3 (treść o stronie
  domyślnej po stronie nazwanej zaczyna nową stronę; w źródle zostawała na stronie nazwanej), `TestPageGroups`
  jest wyłączony do przepisania z tego samego powodu (zasada: specyfikacja przed zgodnością ze źródłem,
  `docs/vision.md`);
- `html/layout/pages.go`, `setOuter`: pole marginesowe dostaje dokładnie wyliczony rozmiar; w źródle (jak
  w WeasyPrint) był przycinany do przedziału min-content–max-content, więc pole o szerokości `auto` kurczyło
  się do treści wbrew CSS Page 3 § 5.3.2.2. Test `TestPageStyle` (13 z 18 wierszy oczekuje przycięcia)
  wyłączony do przeliczenia;
- `text/engine_gotext.go`, `roundMetric`: wysokość nad i pod linią bazową zaokrąglana do całych pikseli CSS przy
  liczeniu wysokości wiersza i podpory (`spaceHeight`). Specyfikacja tego nie określa, przeglądarki tak robią;
  z metrykami ułamkowymi każdy blok pod akapitem zaczynał się w ułamku piksela;
- `html/layout/inline.go`, `firstLetterToBox`: pudełko `::first-letter` jest wstawiane przed dzieci pudełka
  nadrzędnego (w źródle trafiało do nowego pudełka tekstu i pierwsza litera znikała z dokumentu), a wiersz
  wewnątrz pływającej litery dostaje styl anonimowy (z `float` na wierszu układ kończył się paniką);
- grid, zgodność z CSS Grid 1 (`html/layout/grid.go`, `css/validation/expanders.go`):
  kolejność wartości `grid-area` (wiersz, kolumna, wiersz, kolumna; w źródle druga i trzecia zamienione, razem
  z nazwami pól w `TestGridArea`); ujemne numery linii liczone od końca siatki; marginesy `auto` elementów
  (pochłaniają dodatnie wolne miejsce, wyłączają rozciąganie); podana szerokość elementu wyrównanego
  `justify-self` nie jest nadpisywana szerokością treści; wolne miejsce rozdzielane między ścieżki mogące rosnąć
  aż do wyczerpania (§ 12.6); `repeat(auto-fill | auto-fit, …)` liczy liczbę powtórzeń (`auto-fit` bez zwijania
  pustych ścieżek, z ostrzeżeniem); `grid-auto-flow: column` przez zamianę osi wokół algorytmu rozmieszczania.
  `TestGridShorthandAutoFlowColumnsNoneDense` ma oczekiwania dla przepływu kolumnowego;
- kolumny (`html/layout/column.go`): każda kolejna kolumna jest układana jako pusty fragment, więc treść
  niepodzielna wyższa niż kolumna (np. float) zostaje w niej umieszczona; do tego bezpiecznik kończący tworzenie
  kolumn, gdy kolumna nie posunęła treści naprzód. W źródle takie dokumenty tworzyły kolumny bez końca
  (11 zawieszeń w sondażu). Dwa sprawdzenia pustego wskaźnika przy równoważeniu kolumn;
- floaty w wierszu (`html/layout/inline.go`): `inlineOutOfFlowLayout` zwraca pozycję x przesuniętą o float lewy
  (w źródle przesunięcie ginęło i tekst pierwszego wiersza nachodził na pływającą literę `::first-letter`),
  a szerokość floata lewego nie jest odejmowana drugi raz od prawej krawędzi wiersza;
- `html/boxes/boxes.go`, `PageValues`: jedyne dziecko w normalnym przepływie pytane raz zamiast dwa razy;
  wcześniej n zagnieżdżonych bloków kosztowało 2^n kroków (60 poziomów zawieszało silnik);
- `html/tree/tree.go`, `NewHTML`: korzeniem jest pierwszy element dokumentu, nie pierwszy węzeł po DOCTYPE;
  komentarz przed `<html>` kończył się paniką (12 plików WPT);
- `html/layout/block.go`: indeks wznowienia ograniczony do liczby dzieci, jak wycinek w Pythonie; komórka tabeli
  układana ponownie bez dzieci kończyła się paniką (3 pliki WPT);
- `text/engine_gotext.go`: nowa metoda `RegisterFontContent` dla fontów podanych z pamięci; bez niej treść fontu
  była czytana z dysku pod identyfikatorem i tekst znikał z PDF bez błędu;
- `html/document/stacking.go`, `draw.go`: `BoxTree` z mapy `map[bo.Box]BoxTree` zamienione na strukturę pamiętającą
  kolejność wstawiania (w WeasyPrint to słownik Pythona, który ją pamięta). Z mapą kolejność rysowania bloków
  była losowa.

`internal/pdflib/` (kopia pięciu pakietów, które dzielą typy `model`; pozostałe pakiety biblioteki zostają
zależnością modułu):

- nowy plik `model/sorted.go` z `SortedKeys`; 57 pętli po mapach w `model/` i jedna w `contentstream/commands.go`
  przepisane na iterację po posortowanych kluczach (zapis słowników zasobów, parametrów strumieni, drzew nazw);
- `model/acroform.go`, `model/model.go`: dwa wywołania `b.fmt(tekst)` zamienione na `b.fmt("%s", tekst)` (tekst
  ze znakiem `%` psułby zapis; `go vet` blokował testy pakietu);
- `fonts/encoding.go`: usunięta obsługa predefiniowanych CMap-ów (pakiet `standardcmaps`, 71 tys. linii tablic),
  potrzebna tylko przy odczycie cudzych PDF-ów;
- skopiowane dane testowe `model/test`, `fonts/cmaps/test`; pominięte `fonts/test` (9,7 MB), przez co testy
  `TestDefinedEnc` i `TestFullToUnicodeCMap` są wyłączone przez `t.Skip`.

`internal/goweasyprint/`:

- usunięte `Makefile`, `pdf/tmp.png`; z `pdf/test/utils.go` gałąź Pango;
- `pdf/pdf.go`: `compressStreams` włączone (w źródle `false`);
- `pdf/text.go`, `writeFonts`: font bez żadnego rozpoznanego glifu też dostaje słownik; w źródle był pomijany,
  a strona nadal się do niego odwoływała, co kończyło zapis paniką dla każdego znaku spoza podanych fontów;
- nowy plik `pdf/tounicode.go`: własny zapis CMap ToUnicode (posortowany, bloki po 100 wpisów) i kompresja
  strumieni fontów i CMap; zastępuje `cmaps.WriteAdobeIdentityUnicodeCMap`, które iteruje po mapie.

## Ustawienia silnika w `render.go`

- atrybuty prezentacyjne HTML (`width` i `height` na `img`, `bgcolor`, `align`, `border` i inne) są stosowane:
  `document.Render` jest wołane z `presentationalHints = true`. W WeasyPrint i w porcie to opcja domyślnie
  wyłączona; standard HTML (WHATWG, sekcja Rendering) każe je uwzględniać.

## Zależności modułu

Z `go.mod`, licencje z plików `LICENSE` w pamięci modułów; do potwierdzenia przed publikacją:

| Moduł | Licencja | Użycie |
|---|---|---|
| `github.com/go-text/typesetting` | BSD-3-Clause / Unlicense (plik `LICENSE` modułu) | kształtowanie tekstu, fonty |
| `github.com/benoitkugler/pdf` | MIT | zapis PDF (tymczasowo, do wymiany wg ADR 0002) |
| `github.com/benoitkugler/pstokenizer`, `github.com/hhrutter/lzw` | do sprawdzenia | pośrednie przez `benoitkugler/pdf` |
| `github.com/benoitkugler/textlayout` | MIT | tylko w testach (`html/layout/layout_fonts_test.go`) |
| `golang.org/x/{image,net,text}` | BSD-3-Clause | obrazy, parser HTML, kodowania |

## Stan testów (2026-11-12, macOS arm64, go1.26.6, Ghostscript 10.08.0)

`go test ./...`: 7 testów przejętego kodu nie przechodzi stale i jeden losowo (te same co przed zmianami;
stan potwierdzony 2026-11-13 po naprawie kompilacji testów pakietu `html/layout`);
testy własne w korzeniu modułu przechodzą wszystkie.

- `internal/goweasyprint/pdf`: 2 z 415 testów pikselowych (`TestRoundedRect`, `TestTextSubsetComposite`: jeden
  piksel inny niż we wzorcu; przyczyna nieustalona). Testy rasteryzują PDF programem `gs` (AGPL, narzędzie
  testowe, nie zależność binarki).
- `internal/webrender/html/layout`: 3 różnice zaokrągleń (`15` wobec `15.000004`): `TestFlexItemPercentage`,
  `TestFloats2`, `TestLayoutTableAuto20`. Podejrzenie: arytmetyka `float32` na arm64; ważne dla wymagania
  o identycznych bajtach na amd64 i arm64.
- `internal/webrender/css/selector`: `TestSelectors`, `TestMatchers`: inna kolejność atrybutów w wypisywanym HTML,
  prawdopodobnie przez nowszy `golang.org/x/net` (0.62 wobec 0.42 w źródle).
- `internal/webrender/matrix`: `TestTranslate` używa losowych macierzy i czasem nie przechodzi.
- testy własne w korzeniu (`corpus_test.go`, `robustness_test.go`, `render_test.go`), wszystkie przez
  `gopdfgo.Render`: 18 dokumentów renderuje się, liczby stron zgodne (2 różnice opisane w teście), tekst daje się
  wyodrębnić, dwa przebiegi dają identyczne bajty; 726 plików WPT (testy druku i wzorce) renderuje się bez paniki
  i deterministycznie; 400 poziomów zagnieżdżenia poniżej 2 s.

## Pętle po mapach

`go run ./tools/maprange <katalogi>` wypisuje każdą pętlę `range` po mapie. W `internal/webrender`
i `internal/goweasyprint` jest ich 72 (stan 2026-11-12). Nie wszystkie wpływają na wynik, ale każda jest
kandydatem na niedeterminizm albo błąd kolejności, jak `BoxTree`. Na 18 dokumentach i 726 plikach WPT żadna
się nie ujawnia (trzy pełne przebiegi z porównaniem bajtów); podejrzane pozostają układ grid (`layout/grid.go`,
mapa pozycji dzieci), kotwice (`document.go`), liczniki docelowe (`tree/target.go`) i `defs` w SVG.
