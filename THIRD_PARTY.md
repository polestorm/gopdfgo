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
- `text/engine_gotext.go` (`SplitFirstLine`): przy `word-break: break-all` wiersz jest wypełniany do końca
  (każdy znak jest miejscem łamania, CSS Text 3 § 5.2); w źródle słowo było łamane dopiero wtedy, gdy samo nie
  mieściło się w wierszu;
- `html/layout/block.go`: podział bloku o podanej wysokości liczy wysokość po uwzględnieniu `min-height`
  i `max-height`; `css/validation`, `html/boxes/boxes.go`, `html/layout/flex.go` (`isScrollContainer`):
  `overflow: clip` jest osobną wartością, która przycina, ale nie tworzy kontenera przewijania ani kontekstu
  formatowania (CSS Overflow 3 § 3.1);
- `html/layout/block.go` (`narrowBesideFloats`): pudełko tworzące nowy kontekst formatowania, o szerokości
  auto, jest zwężane do miejsca obok elementów pływających, jeśli mieści się tam jego `min-width` (CSS 2.1
  § 9.5 dopuszcza zwężenie albo przeniesienie pod spód; przeglądarki zwężają). W źródle dostawało pełną
  szerokość i lądowało pod elementem pływającym. Szerokość jest dobierana według elementów pływających na
  wysokości górnej krawędzi pudełka;
- podział kontenera flex między strony, `html/layout/flex.go`: element, który nie mieści się na stronie,
  przechodzi na następną (w kolumnie podział między elementami, w wierszu między liniami); kontener, z którego
  nic się nie zmieściło, przechodzi cały; fragment kontenera kończy się za ostatnim elementem na stronie;
  układy próbne (`layoutContext.flexMeasuring`) nie dzielą się, żeby pomiar dał pełną wysokość. W źródle
  kontener flex nie sprawdzał, czy elementy wychodzą poza stronę: były układane poniżej jej krawędzi
  i ginęły, także przy `body { display: flex }`. Poprawiony też indeks wznowienia przy drugim podziale
  (doliczany dwukrotnie, co dawało indeks spoza listy). W wierszu elementy jednej linii dzielą się równolegle
  (CSS Flexbox 1 § 12): stan wznowienia linii jest niesiony w stosie wznowienia pod kluczami ujemnymi
  (`parallelLineState`), a element zakończony na wcześniejszej stronie zastępuje pusta, niewidoczna kopia
  (`finishedFlexItem`), żeby pozostałe zachowały miejsce i szerokość. W źródle układ linii kończył się na
  pierwszym podzielonym elemencie, a jego sąsiedzi trafiali dopiero na ostatnią stronę. Nie dotyczy kierunków
  `-reverse`. Sortowanie według `order` jest stabilne (`sort.SliceStable`; w źródle `sort.Slice`);
- podział elementów siatki między strony, `html/layout/grid.go`: wznowienie elementu, który nie mieści się na
  stronie, jest niesione w stosie jako {wiersz: {indeks elementu: wznowienie}}; następna strona układa dalszy
  ciąg tych elementów, a kolejne wiersze zaczynają się od nowej strony. W źródle („TODO: Support fragmentation
  in grid items”) wznowienie było porzucane i dalsza część elementu ginęła. Styl elementu jest kopiowany przed
  wpisaniem wyliczonej szerokości i wysokości (w źródle zmiana trafiała do stylu współdzielonego między
  stronami). `pages.go`: przejścia po stosie wznowienia nie schodzą w kontenery flex i grid, które zapisują
  w nim numer linii lub wiersza. Ograniczenie: po dalszym ciągu wiersza reszta siatki zaczyna się od nowej
  strony, nawet gdy zostało miejsce;
- treść wylewająca się z bloku o podanej wysokości, `html/layout/block.go`, `html/tree/target.go`
  (`OverflowResume`, `ResumeStack.Overflow`): gdy blok kończy się na stronie, a jego treść (przy `overflow:
  visible`) sięga dalej, treść jest kontynuowana na następnej stronie jako dzieci bloku o zerowej wysokości
  (CSS Fragmentation 3 § 5.1). W źródle była porzucana („forget overflowing children”). Nie dotyczy pudełek
  kolumn ani kontenerów kolumn; kontenery flex i grid o podanej wysokości z `overflow` innym niż `visible`
  nie przekazują dalszego ciągu elementów (`clipsBlockOverflow`);
- `html/layout/pages.go` (`makeAllPages`): bezpiecznik pętli stron. Trzy kolejne strony kończące się w tym
  samym miejscu dokumentu kończą renderowanie z ostrzeżeniem „układ nie robi postępu”; w źródle taki stan
  dawał nieskończenie wiele stron (zawieszenie);
- `html/layout/column.go`: wznowienie elementu rozpinającego (`column-span: all`) podzielonego między strony
  jest przekazywane do niego bez zmian; w źródle było mylone ze wznowieniem treści kolumn (indeks dziecka
  elementu rozpinającego brany za przesunięcie w liście dzieci kontenera);
- krojenie wiersza wyższego niż strona, `html/layout/block.go` (`lineBoxLayout`, `hasTallAtomicInline`),
  `html/tree/target.go` (`SliceResume`, `ResumeStack.Slice`), `html/document/draw.go`: wiersz zawierający
  pudełko niepodzielne wyższe niż strona (obraz w wierszu tekstu, `inline-block`, `inline-flex`,
  `inline-grid`) jest krojony między strony; każda strona pokazuje kolejny wycinek tego samego wiersza.
  W źródle wiersz wystawał poza stronę i dalsza część ginęła. Wiersz samego tekstu wyższy niż strona zostaje
  w całości. Testy odziedziczone dostosowane (wyższa strona, żeby pudełko się mieściło):
  `TestInlineBlockWithMargin`, `TestImagesAlt`;
- `html/layout/layout.go` (`warnContentBelowPage`): ostrzeżenie „treść elementu … leży poniżej dolnej
  krawędzi strony i nie jest widoczna”, gdy tekst albo obraz leży w całości poniżej strony; w źródle taka
  strata była cicha;
- `html/layout/block.go`, `replaced.go`, `layout.go` (`clipDepth`, `viewportClips`): wewnątrz pudełka
  przycinającego treść nic nie jest krojone ani dzielone między strony, a `overflow` inne niż `visible`
  przeniesione z elementu głównego na obszar strony wyłącza kontynuację wylewającej się treści. Podział bloku
  o podanej wysokości obejmuje też elementy pływające;
- `html/layout/block.go`: pudełko z własnym kontekstem formatowania i wysokością auto, w którym element
  pływający jest kontynuowany na następnej stronie, też jest dzielone (resztę wysokości bierze z elementu
  pływającego), więc jego tło i obramowanie sięgają tak daleko jak ten element. W źródle pudełko kończyło się
  na pierwszej stronie. Reftesty druku 179 → 180;
- `html/layout/flex.go`: w wierszu flex decyzja o przeniesieniu linii na następną stronę zapada po ułożeniu
  całej linii; element rozciągnięty (`stretch`) obok sąsiada, który dzieli się między strony, jest przycinany
  do strony zamiast przenosić cały kontener. Znalezione porównaniem z Chromium na nowym dokumencie korpusu
  `19-flex-grid-pagination`;
- `html/layout/flex.go` (`finishedFlexItem`, `isStretchedFlexItem`): element wiersza flex rozciągany do
  wysokości linii, którego treść skończyła się wcześniej, pozostaje widoczny na dalszych stronach (tło
  i boczne obramowanie, bez górnego), dopóki sąsiad z tej samej linii nadal się dzieli; na stronie, na której
  się nie kończy, nie ma dolnego obramowania;
- `html/layout/block.go` (`maxSliceableHeight`), `replaced.go`: wiersz, blok albo obraz o wysokości
  nieskończonej lub absurdalnie dużej nie jest dzielony między strony. Bez tego warunku wiersz z siatką
  o nieskończonej wysokości był krojony w nieskończoność (zawieszenie wykryte pełnym sondażem; bezpiecznik
  pętli stron go nie łapał, bo stan zmieniał się na każdej stronie);
- `html/layout/absolute.go`, `pages.go`, `layout.go` (`brokenBox.deferredPages`), `html/boxes/boxes.go`
  (`DeferredToLaterPage`): pudełko pozycjonowane bezwzględnie z podanym `top` albo `bottom`, leżące w całości
  poniżej obszaru strony, jest układane na tej stronie, na którą wypada jego pozycja. W źródle zostawało
  poniżej dolnej krawędzi strony i nie było widoczne. Nie dotyczy `position: fixed` (powtarzane na każdej
  stronie) ani pozycji statycznej. Reftesty druku 180 → 183 (`fixedpos-001`, `-003`, `firefox-bug-2026295`);
- legenda ramki `fieldset` (HTML § 15.3.13): `html/layout/block.go` (`prepareFieldsetLegend`, `markLegendGap`),
  `html/boxes/boxes.go` (`LegendPrepared`, `BorderGapX`, `BorderGapWidth`), `html/document/draw_border.go`,
  `html/tree/html5_ua.css` (`fieldset > legend` kurczy się do treści). Pierwszy element `legend` leży na górnym
  obramowaniu ramki, a obramowanie jest pod nim przerwane; w źródle legenda była zwykłym blokiem wewnątrz
  ramki. Uproszczenie: wysokość legendy przyjmowana jak dla jednego wiersza tekstu;
- pamięć stylów, `html/tree/style.go` (pomiary w `docs/feature-audit.md`):
  1. `newStyleFor` buduje kaskadę raz dla każdego ciągu dopasowanych reguł (`sharedCascade`,
     `matchResult.appendSignature`) i dzieli ją między elementy; w źródle każdy element dostawał własną mapę.
     Elementy z atrybutem `style` lub atrybutami prezentacyjnymi mają kaskadę własną jak dotąd;
  2. style pseudoelementów powstają na żądanie (`StyleFor.Get` → `pseudoStyle`, kaskady w `pseudoCascades`).
     W źródle obliczane były wszystkie od razu, a reguły arkusza domyślnego bez selektora elementu (`::marker`,
     `::footnote-call`, `::footnote-marker`) pasują do każdego elementu, więc każdy niósł trzy dodatkowe pełne
     style. Skutek uboczny: zniknęła pętla po mapie kaskad, a kotwica pseudoelementu jest rejestrowana dopiero
     przy budowie jego pudełka;
  3. wartości obliczone w dwóch warstwach (`propsCache`): wspólna tablica `sharedValues` dla stylów o tej samej
     kaskadzie, pseudoelemencie i rodzicu z tą samą wspólną tablicą oraz nakładka `own` na wartości jednego
     stylu (ustawione przez `Set*`, obliczone z udziałem atrybutów elementu albo cudzej nakładki; pilnuje tego
     znacznik `shareContext.tainted` i `ComputedStyle.ownElement`). W źródle każdy styl miał własną tablicę na
     186 właściwości. Skutki uboczne: błędna deklaracja jest zgłaszana raz na grupę stylów, a nie raz na element
     (test odziedziczony `TestVariableInFunctionMissing`: 4 → 2 komunikaty); wartość zapisana we wspólnej tablicy
     nie zobaczy późniejszej zmiany rodzica przez `Set*`, jeśli inny styl z grupy odczytał ją wcześniej;
  4. `AnonymousStyle.Get` nie zapisuje wartości odziedziczonych i początkowych, a mapę zmiennych bierze od
     rodzica bez kopiowania; w źródle każde pudełko anonimowe (na przykład liniowe) trzymało własną tablicę;
- składnia CSS (runda 2026-11-14, testy `css/CSS2/syntax`: 189 → 236 z 264):
  - `utils/io.go` (`FetchStylesheet`, `decodeStylesheet`), `html/tree/tree.go`: kodowanie arkusza według
    CSS Syntax 3 § 3.2 (znacznik kolejności bajtów, protokół, dosłowna reguła `@charset`, potem UTF-8), także dla
    `@import`; źródło patrzyło tylko na protokół. Kodowanie środowiska (atrybut `charset`, kodowanie dokumentu)
    pominięte;
  - `css/validation/validation.go` (`color`, `outlineColor`): błędna wartość koloru unieważnia deklarację;
    źródło przyjmowało ją jako kolor zerowy i tekst stawał się niewidoczny (`color: rgb(255, 0)`, `color: redish`);
  - `css/parser/colors.go`: zapis `#rgba` i `#rrggbbaa` (CSS Color 4 § 5.2). Test odziedziczony
    `css-parsing-tests/color3.json` oczekiwał odrzucenia `#ffff` i `#ffffffff` (poziom 3), zmieniony na biały;
    `TestImagesTransparentText` używa `#0001` i przechodził dotąd tylko dzięki błędowi wyżej;
  - `css/validation/validation.go` (`PreprocessDeclarationsPrelude`): blok bez selektora wewnątrz reguły jest
    błędem, a nie regułą zagnieżdżoną; błędna reguła zagnieżdżona odpada sama, bez unieważniania rodzica;
  - `html/tree/style.go` (`preprocessStylesheet`): `@import` po pustej regule (`foo{}`) jest ignorowany;
  - `css/selector/parser.go` (`parseIDSelector`): `#` wymaga identyfikatora, `#6id` unieważnia regułę;
  - `css/parser/serialize.go`: cyfra na początku identyfikatora zapisywana jako ucieczka zakończona spacją;
  - `css/validation/utils.go` (`getUrl`): nazwy funkcji `url` i `attr` bez rozróżniania wielkości liter.
  Nie zmienione celowo: `declarations-009` oczekuje, że po regule `@media {…}` w bloku deklaracji przepada
  następna deklaracja (CSS 2.1); według CSS Syntax 3 reguła kończy się na bloku i deklaracja obowiązuje;
- selektory (runda 2026-11-14), `css/selector`:
  - `pseudo_classes.go` (`structuralParent`): element główny bez węzła dokumentu nad sobą jest jedynym dzieckiem
    dokumentu, więc pasuje do `:first-child`, `:only-child` i pokrewnych; źródło odmawiało dopasowania;
  - `pseudo_classes.go` (`langPseudoClassSelector.Match`, `elementLanguage`): język elementu według HTML
    § 3.2.6.2: najbliższy przodek z `xml:lang` albo `lang`, potem `<meta http-equiv="content-language">`;
    porównanie bez rozróżniania wielkości liter. Źródło dopasowywało `lang` dowolnego przodka;
  - `selector.go` (`matchInclude`): `[attr~=""]` i wartość z odstępem nie pasują do niczego;
  - `parser.go` (`parseSelector`): kombinator po pseudoelemencie unieważnia regułę.
  Nie zrobione: `::first-line` (styl jest odczytywany, ale układ go nie stosuje; tak samo w źródle);
- kaskada, `html/tree/style.go` (`weight.styleAttribute`): deklaracja z atrybutu `style` wygrywa z każdym
  selektorem tego samego pochodzenia i ważności (CSS 2.1 § 6.4.3). Źródło dawało jej wagę (1,0,0), równą jednemu
  selektorowi identyfikatora, więc `#id { color: red }` przesłaniało `style="color: green"`;
- `@import url("adres")`, `html/tree/style.go` (`preprocessStylesheet`): adres w cudzysłowie wewnątrz `url()`
  jest dla analizatora funkcją; źródło obsługiwało tylko `url(adres)` i `"adres"`, więc ta postać była pomijana
  (próbowało wczytać katalog arkusza);
- `xhtml.go` (kod własny, `decodeStyleEntities`): odwołania znakowe w `<style>` dokumentu XHTML są rozwijane
  (`.a &gt; .b`), poza sekcjami CDATA;
- font spacji, `text/engine_gotext.go` (`spacesInOwnFace`): spacja i spacja nierozdzielająca dostają pierwszy
  font z listy rodzin, który je zawiera (CSS Fonts 4 § 5.3), także po znaku złożonym fontem zastępczym. Segmentacja
  go-text dokleja odstępy do bieżącego przebiegu, więc „◦ ” miało spację o szerokości z fontu zastępczego;
- `html/layout/preferred.go` (`inlineLineWidths`): wcięcie pierwszego wiersza liczy się do szerokości minimalnej
  razem z obiektem stojącym na początku wiersza, a ujemne wcięcie nie daje ujemnej szerokości wiersza;
- tabele (runda 2026-11-14):
  - `html/layout/tables.go` (`tableWrapperWidth`), `preferred.go`: `min-width` i `max-width` tabeli (CSS 2.1
    § 10.4, CSS Tables 3 § 3.9.1); źródło je pomijało. Test odziedziczony `TestLayoutTableAuto35` oczekiwał, że
    tabela z `max-width: 300px` i pustą komórką `width: 400px` ma 400 px; zmieniony na 300 px;
  - `html/layout/tables.go` (`tableLayout`): `min-height` i `max-height` tabeli (CSS 2.1 § 10.7);
  - `html/layout/background.go` (`layoutBackgroundLayer`): tło grupy wierszy obejmuje całą grupę; źródło brało
    wysokość najwyższej komórki, więc tło kończyło się po pierwszym wierszu;
- tabele, druga część (runda 2026-11-14):
  - `html/layout/absolute.go` (`absoluteBlock`): tabela pozycjonowana bezwzględnie ma do dyspozycji bryłę
    zawierającą pomniejszoną o `left` i `right` (CSS 2.1 § 10.3.7); źródło dawało jej całą szerokość. Trzy testy
    `css-tables/absolute-tables-008`, `-009`, `-011` (oznaczone `tentative`, opisują zachowanie Chromium)
    oczekują szerokości bryły bez tej korekty i po zmianie nie przechodzą;
  - `html/layout/tables.go` (`fixedTableLayout`): procent szerokości kolumny w układzie stałym odnosi się do
    szerokości tabeli bez odstępów między komórkami (CSS Tables 3 § 3.9.3). Test odziedziczony
    `TestTableColumnWidth1` oczekiwał 10% z całych 10000 px; zmieniony na 10% z 9500 px;
  - `html/layout/tables.go` (`tableLayout`), `background.go`: wiersz z `visibility: collapse` nie zajmuje
    wysokości i nie jest rysowany (CSS 2.1 § 17.5.5). Kolumn z `visibility: collapse` to nie obejmuje;
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
