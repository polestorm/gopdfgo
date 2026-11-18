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
- pozycjonowanie (runda 2026-11-14):
  - `html/layout/absolute.go` (`absoluteHeightMinMax`): `min-height` i `max-height` pudełka pozycjonowanego
    bezwzględnie są stosowane przed wyznaczeniem marginesów automatycznych (CSS 2.1 § 10.7), więc pudełko
    z `top`, `bottom` i `max-height` jest wyśrodkowane według wysokości ostatecznej;
  - `html/layout/absolute.go` (`absoluteHeight`, `_absoluteWidth`): jedyny margines automatyczny dostaje resztę
    po odjęciu drugiego marginesu (CSS 2.1 § 10.3.7 i § 10.6.4); źródło drugi margines pomijało;
  - `html/document/stacking.go` (`newStackingContextFromBox`, parametr `escape`): przodek z `overflow` innym niż
    `visible`, który nie jest pozycjonowany, nie przycina pudełek pozycjonowanych bezwzględnie, dla których nie
    jest bryłą zawierającą (CSS 2.1 § 11.1.1). Źródło robiło z niego pełny kontekst nakładania i przycinało
    wszystkich potomków, więc pudełko wyprowadzone poza taki element znikało;
- flex, procenty przy nieokreślonym rozmiarze głównym (runda 2026-11-14), `html/layout/flex.go`,
  `html/boxes/boxes.go` (`IndefiniteHeight`): w kontenerze kolumnowym bez podanej wysokości wysokość procentowa
  elementu działa jak `auto`, a procentowe `flex-basis` jak `content` (CSS Flexbox 1 § 7.2.3 i § 9.8); wysokość
  nadana elementowi przez rozciąganie w takim kontenerze nie jest podstawą procentów jego potomków. Źródło
  liczyło procent z nieskończoności i zapisywało w PDF `NaN` albo `Inf` (test `flex-basis-011` przechodził tylko
  dlatego, że tak samo zepsuty był wzorzec);
- `pdflib/model/writeutils.go` (`FmtFloat`, `NonFiniteNumbers`), `render.go`: wartości `NaN` i nieskończone są
  zapisywane jako 0 i zliczane, a `Render` zgłasza je ostrzeżeniem; źródło zapisywało napisy `NaN` i `+Inf`,
  które psują składnię strumienia strony. Licznik jest zmienną globalną, tak jak rejestrator ostrzeżeń
  (pytanie 15 w `docs/adr/open-questions.md`). Skan 9135 testów sondażu po poprawkach flex: 0 takich dokumentów;
- flex, druga część (runda 2026-11-14), `html/layout/flex.go`:
  - wysokość nadana kontenerowi z zewnątrz (rozciągnięcie w kontenerze nadrzędnym) pozostaje określona i jest
    podstawą procentów jego elementów (CSS Flexbox 1 § 9.4, krok 11); źródło wracało do wysokości ze stylu;
  - `justify-content`: `space-between` przy braku miejsca albo jednym elemencie działa jak `flex-start`,
    `space-around` i `space-evenly` jak „safe center” (CSS Box Alignment 3 § 4.3); `right` w kontenerze
    kolumnowym działa jak `start` (§ 4.1). Testy odziedziczone `TestFlexDirectionColumnJustify`, `…Gap`,
    `…GapWrap` i `…MarginPadding` oczekiwały dla `right` końca osi; wiersz `right` zmieniony na wartości `left`;
- `html/layout/block.go` (`narrowBesideFloats`): przy szukaniu miejsca obok elementu pływającego wliczane są
  marginesy czekające na złożenie (np. margines `body` nad pierwszym blokiem). Bez tego pudełko tworzące własny
  kontekst formatowania było zwężane tylko w dokumentach bez marginesów nad nim, a w pozostałych lądowało pod
  elementem pływającym (poprawka do wcześniejszej zmiany własnej);
- `html/layout/inline.go` (`splitInlineBox`): element pływający wewnątrz pudełka liniowego nie jest przesuwany
  o margines, obramowanie i odstęp tego pudełka (CSS 2.1 § 9.5.1); źródło przesuwało go razem z treścią;
- warstwy kaskady `@layer` (runda 2026-11-14), `html/tree/style.go` (`preprocessLayer`, `declareLayer`,
  `layerRanks`, `weight.layer`), `tree.go` (`match.layer`): reguły w warstwach są stosowane z kolejnością według
  CSS Cascade 5 § 6.4 (późniejsza warstwa wygrywa, reguły poza warstwami wygrywają z warstwami, dla `!important`
  odwrotnie, warstwa zagnieżdżona przed regułami rodzica), także dla `@page`. Źródło pomijało `@layer` razem
  z zawartością. Nie zrobione: `@import … layer(…)` i `revert-layer`. Numerator warstw bez nazwy jest zmienną
  globalną (służy tylko do odróżniania warstw);
- grid i kolejność nakładania (runda 2026-11-14):
  - `html/layout/grid.go` (`autoFitRange`, `collapseEmptyAutoFit`): puste ścieżki `repeat(auto-fit, …)` są
    zwijane razem z odstępami (CSS Grid 2 § 7.2.3.2); wcześniej `auto-fit` było układane jak `auto-fill`
    z ostrzeżeniem;
  - `html/document/stacking.go` (`dispatch`): element flex lub grid z `z-index` innym niż `auto` tworzy kontekst
    nakładania bez pozycjonowania (CSS Flexbox 1 § 5.4, CSS Grid 2 § 6.4); źródło wymagało `position`;
- odstępy `gap` (runda 2026-11-14):
  - `css/validation/validation.go`: `grid-gap`, `grid-row-gap` i `grid-column-gap` są aliasami `gap`, `row-gap`
    i `column-gap` (CSS Box Alignment 3 § 8.4), a odstęp może być procentem (§ 8.1); źródło odrzucało jedno
    i drugie. Test odziedziczony `TestColumnGap` oczekiwał odrzucenia `column-gap: 40%`; zmieniony na 40%
    szerokości kontenera;
  - `html/layout/grid.go`: odstęp procentowy liczy się od rozmiaru treści kontenera, a przy rozmiarze
    nieokreślonym wynosi zero; odstępy między kolumnami nie są wliczane do wolnego miejsca rozdawanego przez
    `justify-content` (w osi wierszy źródło już to robiło);
  - `html/layout/flex.go`: procentowy odstęp wierszy wynosi zero także wtedy, gdy wysokość kontenera pochodzi
    z rozciągania w kontenerze o nieokreślonej wysokości. Strata: `grid-lanes-contain-intrinsic-size-row-002`
    (układ `grid-lanes` nie jest obsługiwany; test przechodził przypadkiem);
- liczniki odwrócone (runda 2026-11-14):
  - `css/validation/validation.go` (`reversedCounters`), `css/properties/types.go` (`IntString.Reversed`,
    `AutoStart`): `counter-reset: reversed(nazwa) [liczba]` (CSS Lists 3 § 4.1); źródło odrzucało deklarację;
  - `html/boxes/build.go` (`reversedCounterStart`, `updateCounters`), `html/tree/target.go`
    (`PageState.CounterReversed`): wartość początkowa licznika odwróconego bez podanej liczby wynika z przyrostów
    i ustawień elementów w jego zasięgu (§ 4.4.2), a elementy listy zmniejszają taki licznik domyślnie o 1.
    Reguła sumowania (przyrosty z przeciwnym znakiem; koniec na pierwszym `counter-set`, inaczej ostatni
    niezerowy przyrost liczony dwukrotnie) jest dobrana do testów WPT `li-value-reversed` i `foo-counter-reversed`.
    Atrybut `reversed` listy `ol` jest teraz tłumaczony na `counter-reset: reversed(list-item)`, a dawna własna
    funkcja `reversedListInitial` usunięta. Nie zrobione: liczniki odwrócone zakładane przez pseudoelementy;
  - `html/tree/html5_ua.css`: `::marker { white-space: pre }` (CSS Lists 3 § 3.1), żeby spacja kończąca
    `content` znacznika nie ginęła;
- `html/layout/preferred.go` (`tableAndColumnsPreferredWidths`): kolumna procentowa bierze z szerokości komórki
  rozpinającej swój udział procentowy, zanim reszta trafi do pozostałych kolumn; źródło oddawało całość kolumnom
  bez procentu, więc kolumna `width: 90%` obok pustej kolumny kurczyła się do kilku pikseli, gdy tabela miała
  wiersz z `colspan` (test WPT `table-colspan-percent-auto`);
- położenie znaków w zapisanym tekście (runda 2026-11-14), `text/draw/draw_gotext.go`: korekta odstępu po
  znaku w operatorze `TJ` (liczba całkowita w tysięcznych firetu) jest zaokrąglana, a reszta przechodzi na
  następny znak; początek tekstu jest ustawiany na siatce tych jednostek liczonej od początku strony
  (przesunięcie do 0,0005 rozmiaru fontu). Źródło obcinało korektę w stronę zera przy każdym znaku, więc błąd
  narastał wzdłuż wiersza i tekst narysowany rozjeżdżał się z szerokością wyznaczoną w układzie (ok. 0,03 px
  na kilkanaście znaków). Skutek: ten sam tekst wygląda identycznie w jednym fragmencie i w kilku kolejnych
  pudełkach. Pliki PDF korpusu większe o 1,1% (3,24 MB → 3,27 MB łącznie dla 21 dokumentów), bo więcej
  znaków dostaje niezerową korektę. Odrzucone warianty (pełny sondaż 6779 wobec 6787 dla przyjętego):
  korekty ułamkowe bez siatki (pliki większe o 2,9%) oraz zaokrąglanie narastające w pomiarze tekstu (psuło
  zgodność tekstu dzielonego na fragmenty). Koszt przyjętego: cztery testy `CSS2/css1` z fontem Ahem, w których
  krawędź znaku leży dokładnie na granicy piksela, przeszły z klasy „z dokładnością do krawędzi” do niezaliczonych.
  Poprawka z 2026-11-15: początek tekstu jest zaokrąglany na siatce w górę, a korekta w dół, więc znak nigdy
  nie stoi przed swoim położeniem z układu (odchyłka od 0 do 0,001 rozmiaru fontu w prawo). Przy zaokrąglaniu
  do najbliższej znak z granicy piksela trafiał 0,005 px przed nią i rasteryzator zostawiał szczelinę między
  sąsiednimi znakami. Pełny sondaż: 5 testów zyskanych (w tym cztery stracone poprzednio), żadnego straconego,
  11 więcej zaliczeń ścisłych; rozmiar PDF korpusu bez zmian (+0,04%);
- układ blokowy i liniowy CSS 2 (runda 2026-11-15):
  - `html/layout/block.go`: gdy `min-height` albo `max-height` zmienia wysokość automatyczną pudełka, dolny
    margines ostatniego dziecka nie wychodzi poza pudełko i nie odsuwa następnego; źródło przenosiło go na
    zewnątrz. Zachowanie przeglądarek i nowszych testów WPT (`margin-collapse-min-height-001/002`,
    `min-height-separates-margin`, `max-height-separates-margin`). Starszy test `margin-collapse-038` oczekuje
    przeciwnie i teraz nie przechodzi; tekst CSS 2.2 § 10.7 („nie wpływają na zwijanie marginesów”) jest po jego
    stronie, zob. pytanie 18 w `docs/adr/open-questions.md`;
  - `html/layout/block.go`: wysokość podana wprost jest ograniczana przez `min-height` i `max-height` przed
    ułożeniem dzieci, więc procentowe wysokości dzieci liczą się od wysokości używanej (CSS 2 § 10.5, § 10.7);
    źródło ograniczało ją dopiero po ułożeniu dzieci;
  - `html/layout/block.go`, `boxes.BoxFields.CollapsedThrough`: marginesy pustego pudełka z `clear`, które
    dostało clearance, zwijają się ze sobą i z marginesami następnych pudełek, licząc od górnej krawędzi
    marginesu, a wynik nie zwija się z dolnym marginesem rodzica (CSS 2 § 8.3.1); źródło liczyło górny margines
    dwukrotnie i wypuszczało margines poza rodzica. Gdy clearance dostaje pierwsze dziecko w przepływie, pudełko
    rodzica nie jest już przesuwane o zwinięty margines drugi raz na końcu układu;
  - `html/layout/block.go` (`allCollapsedThrough`): marginesy pudełka zwijają się przez nie także wtedy, gdy
    ma dzieci w przepływie, o ile marginesy wszystkich tych dzieci też się zwijają (CSS 2 § 8.3.1); źródło
    uznawało to tylko dla pudełek bez dzieci, więc pusty blok w pustym bloku rozdzielał marginesy
    (test WPT `anonymous-table-cell-margin-collapsing`);
  - `html/boxes/boxes.go` (`EstablishesFormattingContext`): podpis tabeli zakłada kontekst formatowania
    blokowego (CSS 2 § 17.4), więc marginesy jego dzieci zostają w podpisie;
  - `html/layout/replaced.go`: przy szerokości automatycznej i podanej wysokości elementu zastępowanego
    najpierw stosowane są `min-height` i `max-height`, a szerokość wynika z wysokości już ograniczonej
    (CSS 2 § 10.7); źródło liczyło szerokość z wysokości sprzed ograniczenia;
  - `html/layout/inline.go` (`splitInlineBox`, `skipsOnlyLeadingSpaces`): pudełko liniowe, którego tekst
    zaczyna się od zwijanej spacji, zachowuje lewy margines, obramowanie i odstęp; źródło uznawało pominięcie
    spacji za kontynuację z poprzedniego wiersza i usuwało je;
  - `html/layout/inline.go` (`splitInlineBox`): lewy margines, obramowanie i odstęp pudełka liniowego
    zmniejszają miejsce na jego treść w wierszu; w źródle pierwszy wiersz takiego pudełka wystawał poza blok
    o ten odstęp (kierunek `ltr`; `rtl` bez zmian);
  - `html/boxes/build.go` (`innerBlockInInline`), `boxes.BoxFields.SplitBefore/SplitAfter`,
    `html/layout/inline.go`: fragmenty pudełka liniowego rozdzielonego przez blok w środku nie mają marginesu,
    obramowania ani odstępu od strony rozdzielenia (CSS 2 § 9.2.1.1); źródło rysowało je przy każdym fragmencie;
  - `html/layout/inline.go` (`isPhantomLinebox`): o tym, czy wiersz bez treści ma zerową wysokość, decydują
    tylko marginesy, obramowania i odstępy w osi wiersza (CSS Inline 3 § 2.1), i tylko od stron, których
    fragment nie stracił; w źródle wiersz przestawał być pusty także przez obramowanie górne lub dolne;
- `html/tree/html5_ua.css`, `html/tree/computed_values.go` (`textAlignAll`), `css/validation/validation.go`:
  komórka nagłówkowa `th` jest domyślnie wyśrodkowana, a gdy `text-align` jej rodzica ma wartość inną niż
  początkowa, dziedziczy wyrównanie (HTML, Rendering § 15.3.9). Arkusz przeglądarki używa do tego wewnętrznej
  wartości `-internal-center`, rozstrzyganej przy obliczaniu `text-align-all` i `text-align-last`. W źródle
  `th` nie było środkowane wcale (runda 2026-11-15, test WPT `th-text-align`);
- liczniki list (runda 2026-11-15):
  - `html/boxes/build.go` (`implicitListItemIncrement`, `updateCounters`, `reversedCounterStart`): element listy
    zwiększa licznik `list-item` zawsze, gdy `counter-increment` go nie wymienia, także przy `none` i przy innych
    licznikach (CSS Lists 3 § 4.6); źródło robiło to tylko przy braku deklaracji. Zmienione oczekiwania testów
    odziedziczonych `TestCounters2` (lista wewnętrzna 1, 2, 3 zamiast 1, 1, 2) i `TestCounters6` („1.” zamiast „0.”);
  - `html/tree/style.go`: elementy `li` w `<ol reversed>` nie dostają już deklaracji `counter-increment:
    list-item -1` z atrybutu; przyrost −1 wynika z tego, że licznik jest odwrócony, więc lista, której autor
    zastąpił `counter-reset` zwykłym licznikiem, liczy w górę (testy WPT `li-value-reversed-024`, `-025`);
  - `css/validation` (`isReservedCounterName`): nazwą licznika nie może być słowo kluczowe wspólne dla całego
    CSS, `default` ani `none`, bez rozróżniania wielkości liter, także w `counter()` i `counters()`; źródło
    sprawdzało tylko `counter-reset` i `counter-increment` i tylko trzy słowa (test WPT `counter-invalid`);
- `html/layout/tables.go` (`tableLayout`): tabela bez wierszy nie dostaje pionowego odstępu między komórkami,
  więc ma zerową wysokość treści; źródło dodawało jeden odstęp (runda 2026-11-15, test WPT
  `caption-relative-positioning`);
- `html/layout/preferred.go` (`tableAndColumnsPreferredWidths`, `definiteColumnWidth`, `percentageContribution`),
  runda 2026-11-15: kolumna zadeklarowana elementem `<col>` o szerokości wynikającej z długości w `width`,
  `min-width` i `max-width` istnieje także wtedy, gdy żaden wiersz nie ma w niej komórki, i dostaje swój odstęp
  (CSS 2 § 17.2.1; źródło liczyło kolumny tylko z komórek); `calc()` łączące procent z długością liczy się
  w szerokościach tabeli jak `auto` (CSS Tables 3 § 3.8.3; źródło brało sam procent i rozciągało tabelę).
  Testy WPT `col-definite-size-001`, `col-definite-min-size-001`, `col-definite-max-size-001`;
- `html/layout/tables.go` (`tableLayout`), runda 2026-11-15: `thead` albo `tfoot`, które nie mieszczą się
  w całości nawet na pustej stronie, przestają być grupą powtarzaną i płyną jak zwykła grupa wierszy,
  z podziałem między strony. Źródło po cichu pomijało taką grupę i jej treść znikała z dokumentu (także gdy
  tabela miała tylko nagłówek). Zmienione oczekiwania testu odziedziczonego `TestTableEmptyBody` (sześć
  przypadków z grupą o wysokości 110 px na stronie 100 px). Reftesty druku
  `rowgroup-page-break-inside-avoid-1` i `-3` zaliczone;
- `text/engine_gotext.go` (`FontConfigurationGotext.GenericFamilies`, `newQuery`), 2026-11-15: rodziny fontów
  wskazane dla rodziny generycznej CSS są dopisywane do listy rodzin tuż przed nią; bez wskazania zapytanie
  o font jest takie jak w źródle (ADR 0005, `Options.GenericFamilies`);
- `html/tree/tree.go` (`Behavior`, `HTML.Behavior`), `html/layout/layout.go` (`layoutContext.behavior`),
  `html/layout/block.go`, 2026-11-15: przełącznik `MarginsEscapeHeightLimits` przywraca zachowanie źródła
  (dolny margines dziecka wychodzi poza pudełko ograniczone przez `min-height` lub `max-height`); domyślne
  pozostaje zachowanie Chromium (ADR 0006, `Options.MarginsEscapeHeightLimits`);
- `html/layout/tables.go` (`tableLayout`), podział tabel między strony, runda 2026-11-15:
  - podana wysokość tabeli nie jest stosowana do fragmentów tabeli podzielonej między strony (każdy fragment
    ma wysokość swoich wierszy); źródło stosowało ją do każdego fragmentu z osobna, więc fragment wystawał
    poza stronę, a tabela rosła z każdą stroną. Uproszczenie: nadmiar podanej wysokości ponad wiersze nie
    przechodzi na kolejne strony;
  - obramowanie i odstęp wewnętrzny tabeli nie są powtarzane na kolejnych stronach także wtedy, gdy tabela
    ma powtarzany nagłówek lub stopkę (`box-decoration-break: slice`); źródło w takim przypadku powtarzało
    je na każdej stronie bez rezerwowania miejsca. Wyjątek: przy `border-collapse: collapse` zostaje
    zachowanie źródła, bo tam „obramowanie” tabeli to połowa obramowania skrajnych komórek
    (testy odziedziczone `TestTables_9`, `TestTables_22`);
  - przy `box-decoration-break: clone` wiersze zostawiają miejsce na dolny odstęp i obramowanie tabeli
    na każdej stronie; źródło go nie rezerwowało i dekoracja wychodziła na margines;
  - po wierszu przerwanym na granicy strony odstęp między komórkami jest przed powtarzaną stopką, a nie
    za nią; źródło dosuwało stopkę do przerwanego wiersza.
  Reftesty druku `table-fragmentation-001b…003d` (dziesięć testów) zaliczone;
- `html/tree/html5_ua.css`, 2026-11-15: `caption { text-align: center }` (HTML, Rendering § 15.3.9); w źródle
  podpis tabeli był wyrównany do lewej (reftest druku `table-page-break-inside-avoid-8`);
- `utils/urls.go` (`DefaultUrlFetcher`, `unescape`), runda flex 2026-11-15: w adresie `data:` bez base64 spacje
  są częścią danych, a znak `%` bez dwóch cyfr szesnastkowych zostaje dosłownie (WHATWG URL, percent-decode);
  źródło usuwało wszystkie odstępy i odrzucało adres z gołym `%`, więc SVG wpisane wprost w `src`
  (np. z `width='100%'`) nie było wczytywane;
- `html/boxes/build.go` (`blockify`), runda flex 2026-11-15: pudełko liniowe będące elementem flex lub grid
  samo staje się blokiem (CSS Display 3 § 2.7), a jego dzieci przechodzą do tego bloku; źródło opakowywało
  je w blok o tym samym stylu, więc odstępy wewnętrzne, obramowanie i tło były stosowane dwukrotnie
  (`<span style="padding: 0 2em">` we flex miał tekst odsunięty o 4em). Zmienione oczekiwanie struktury
  w teście odziedziczonym `TestFlexGrowInFlexColumn`;
- `html/layout/flex.go`, runda flex 2026-11-15: rozmiar poprzeczny linii jednowierszowej i rozciąganie
  `align-content: stretch` biorą wysokość treści kontenera po rozwiązaniu `box-sizing` i procentów; źródło
  brało surową wartość `height` ze stylu, więc w kontenerze `border-box` elementy były rozciągane za wysoko;
- `html/layout/preferred.go` (`replacedMaxContentWidth`), runda flex 2026-11-15: szerokość procentowa elementu
  zastępowanego liczy się w szerokości max-content jak `auto` (CSS Sizing 3 § 5.2.1); źródło dawało zero;
- `html/layout/grid.go`, runda flex 2026-11-15: linia bazowa kontenera `inline-grid`, którego pierwszy element
  nie ma wiersza tekstu, jest syntetyzowana z dolnej krawędzi obramowania elementu (CSS Align 3 § 9.1);
  źródło przyjmowało górę kontenera;
- `html/boxes/boxes.go` (`PageValues`), 2026-11-15: nazwy stron (`page`) elementów kontenera flex lub grid nie
  przechodzą na kontener i nie wymuszają podziału (CSS Page 3 § 6.1); w źródle przechodziły (reftesty druku
  `page-name-flex-001`, `-002`, `-004`);
- podział flex i grid między strony, runda 2026-11-15:
  - `html/layout/flex.go` (`forcedBreakBetweenFlexLines`): `break-before` i `break-after` na elementach flex
    wymuszają podział strony między liniami kontenera wierszowego i między elementami kolumny;
  - `html/layout/grid.go`: to samo między wierszami siatki;
    źródło ignorowało te właściwości na elementach flex i grid (reftesty druku
    `multi-line-row-flex-fragmentation-081a…d`);
  - `html/layout/flex.go`: `flex-basis` podaje rozmiar pudełka wskazanego przez `box-sizing`
    (CSS Flexbox 1 § 7.2.1); źródło traktowało go zawsze jak rozmiar treści
    (`multi-line-row-flex-fragmentation-063`);
- tabele jako elementy flex, runda 2026-11-15:
  - `css/properties/datas.go` (`TableWrapperBoxProperties`): `flex-basis`, `flex-grow`, `flex-shrink`,
    `align-self`, `justify-self`, `order` i położenie w siatce (`grid-row-*`, `grid-column-*`) przechodzą
    z tabeli na pudełko opakowujące, bo to ono jest elementem kontenera; źródło zostawiało je na tabeli,
    więc dla tabel były ignorowane;
  - `html/layout/block.go`, `html/layout/tables.go` (`tableWrapperWidthForced`): tabela przyjmuje szerokość
    i wysokość wyznaczone przez układ flex (rozmiar docelowy, rozciągnięcie); źródło liczyło szerokość od
    nowa z samej tabeli, a wysokość dostawało tylko pudełko opakowujące;
  - `html/layout/flex.go`: o rozciąganiu tabeli w poprzek decyduje rozmiar podany w stylu tabeli, nie pudełka
    opakowującego;
- obrazy jako elementy flex, runda 2026-11-15:
  - `html/layout/flex.go`: automatyczne minimum (`min-width: auto`, `min-height: auto`) liczone jest tylko
    w osi głównej (CSS Flexbox 1 § 4.5); źródło liczyło je dla szerokości zawsze, a dla wysokości elementów
    zastępowanych także w wierszu, przez co obraz rozciągany w poprzek był skalowany odwrotnie do proporcji
    (np. 400×200 zamiast 100×100 w kontenerze o wysokości 100 px);
  - `html/layout/flex.go` (`ratioBaseSize`): element zastępowany z proporcjami, rozciągany do określonego
    rozmiaru poprzecznego kontenera jednowierszowego, ma rozmiar bazowy z proporcji (§ 9.2 krok 3B);
    w źródle krok był pominięty;
  - `html/tree/style.go`: `width` i `height` elementu `<svg>` w HTML są atrybutami prezentacyjnymi
    (SVG 2 § 8.3);
  - zmienione oczekiwania testów odziedziczonych `TestFlexItemIntrinsicHeightShrink`
    i `TestFlexItemIntrinsicWidthShrink` (renderują bez atrybutów prezentacyjnych, więc element o rozmiarze
    auto jest rozciągany: 10 zamiast 100);
- `box-shadow` (CSS Backgrounds 3 § 6.1), 2026-11-15: nowa właściwość: `css/properties` (`PBoxShadow`,
  `BoxShadows`, pliki generowane), `css/validation` (`boxShadow`), `html/tree/computed_values.go`
  (`boxShadows`), `html/boxes/boxes.go` (`RoundedBoxInset`, `RoundedPaddingBoxInset`),
  `html/document/draw_border.go` (`drawBoxShadows`) i wywołania w `draw.go` (pudełka blokowe, liniowe,
  komórki tabel). Cień zewnętrzny leży pod tłem i jest przycięty do obszaru poza pudełkiem obramowania,
  wewnętrzny leży nad tłem. Rozmycie jest przybliżane dwunastoma współśrodkowymi warstwami, bo PDF nie ma
  rozmycia; źródło tej właściwości nie znało i zgłaszało ostrzeżenie;
- `html/layout/grid.go`, `html/layout/absolute.go`, `html/boxes/boxes.go` (`GridArea`, `StaticAlign`),
  2026-11-15, CSS Grid 1 § 9 i § 6.2:
  - dzieci pozycjonowane bezwzględnie nie są elementami siatki (nie zajmują komórek, nie wpływają na
    ścieżki); źródło układało je w automatycznym rozmieszczeniu jak zwykłe elementy;
  - blok zawierający takiego dziecka to obszar wskazany przez `grid-row`/`grid-column` (linia `auto` oznacza
    krawędź dopełnienia kontenera), a pozycja statyczna uwzględnia `justify-self` i `align-self`;
    element bez podanych przesunięć ma rozmiar dopasowany do treści;
  - siatka z `position` innym niż `static` sama układa swoje pudełka pozycjonowane, po wyznaczeniu ścieżek;
  - rozciąganie (`stretch`) dotyczy tylko elementów o `width`/`height: auto`; źródło rozciągało także
    elementy o podanym rozmiarze;
  - `align-items: baseline` wyrównuje elementy wiersza do wspólnej linii bazowej (bez wpływu na rozmiar
    ścieżek, co norma przewiduje, a czego tu jeszcze nie ma);
- `css/parser/colors.go`, 2026-11-15, CSS Color 4: `rgb()`/`hsl()` w składni ze spacjami i alfą po ukośniku,
  składowe ułamkowe i procentowe, `none`, kąt barwy z jednostką, `rgba()`/`hsla()` jako aliasy; nowe
  funkcje `hwb()`, `lab()`, `lch()`, `oklab()`, `oklch()` sprowadzane do sRGB (barwy spoza gamy przycinane
  składowa po składowej, bez odwzorowania gamy z § 13). Usunięte `parseAlpha`, `parseRgb`, `parseHsl`.
  Zmienione oczekiwania 9 przypadków w `css-parsing-tests/color3.json`, które kodowały CSS Color 3:
  zapisy odrzucane tam jako błędne są poprawne w CSS Color 4 (np. `rgb(0, 0, 0, 0)`,
  `hsl(30deg, 100%, 100%)`, alfa w `rgb()`, kolor bez alfy w `rgba()`);
- `css/properties/calc.go` (`ErrCalcPercentComparison`, `Freeze`, `Evaluate`), `css/validation/calc.go`,
  `html/tree/computed_values.go` (`length_`), `css/properties/float.go` (`ResolvePercentage`), 2026-11-15,
  CSS Values 4 § 10: `min()`, `max()`, `clamp()` z procentami są przyjmowane i liczone w chwili, gdy znana
  jest podstawa procentu; źródło odrzucało taką deklarację w całości;
- `css/selector/parser.go`, `pseudo_classes.go`, 2026-11-15: `:where()` (Selectors 4 § 4.4), waga zerowa;
- `css/validation/validation.go`, 2026-11-15, wartości bez odpowiednika w druku albo równoważne w piśmie
  poziomym od lewej: `position: sticky` → `relative` (bez przewijania element zostaje na miejscu),
  `float`/`clear: inline-start|inline-end` → `left|right`, `word-break: keep-all` → `normal`,
  `white-space: break-spaces` → `pre-wrap`. Źródło odrzucało te deklaracje z ostrzeżeniem;
- `css/validation/logical.go` (plik własny), 2026-11-15: skróty `inset`, `place-items`, `place-content`,
  `place-self`; `overflow-x`/`overflow-y` (wartość inna niż `visible` ustawia `overflow` dla obu osi, bo silnik
  ma jedną właściwość); `border-start-start-radius` i pokrewne;
- `html/tree/style.go` (`evaluateSupports`), 2026-11-15, CSS Conditional 3 § 6: reguła `@supports`
  z deklaracjami, `selector()`, `not`, `and`, `or`; deklaracja jest obsługiwana, gdy przechodzi walidację.
  Źródło pomijało regułę z ostrzeżeniem o nieznanej regule;
- `html/boxes/build.go` i `boxes_test.go`, 2026-11-15: `%s` → `%v` dla `pr.Display`. Od zmiany `Display`
  na strukturę (`display: contents`) `go vet` odrzucał pakiet i jego testy się nie budowały; po poprawce
  przechodzą;
- `html/tree/media_query.go` (przepisany), `html/tree/style.go`, 2026-11-15, Media Queries 4, ADR 0007:
  zapytania o medium z cechami (`min-width`, `max-width`, zakresy `width >= …`, `orientation`,
  `prefers-color-scheme`, `resolution`, `hover` i inne), warunki `and`/`or`/`not`, `only`, zaprzeczenie typu.
  Cechy oceniane względem stałego medium odniesienia (A4 w pionie). Błędne albo nieznane zapytanie jest
  fałszywe samo, bez unieważniania listy. Atrybut `media` w `<link>` i `<style>` przechodzi tę samą ocenę.
  Źródło znało tylko typy medium i odrzucało całą regułę `@media` z cechą;
- `text-shadow` (CSS Text Decoration 3 § 4), 2026-11-15: nowa właściwość dziedziczona `PTextShadow`
  (`css/properties`, pliki generowane, `datas.go`), walidacja `textShadow`, rysowanie w
  `html/document/draw_text.go`. Cień ostry to kopia tekstu pod tekstem; rozmyty przybliżamy 17 kopiami
  o częściowym kryciu na dwóch okręgach. Cienia nie dostają dekoracje tekstu (podkreślenie);
- `css/validation/utils.go` (`parseColorStops`, `withoutInterpolationSpace`), 2026-11-15, CSS Images 4:
  przystanek gradientu o dwóch pozycjach, wskazówka przejścia (przybliżona przystankiem barwy pośredniej)
  i przestrzeń interpolacji `in …` (przyjmowana, interpolacja zostaje w sRGB). Źródło odrzucało cały gradient.
  Zmienione oczekiwanie w testach odziedziczonych `TestLinearGradient` i `TestRadialGradient`:
  `red 4px 2px` jest poprawne (dwie pozycje), odrzucane ma być `red 4px 2px 1px`;
- `css/parser/colors.go` (`parseColorMix`), 2026-11-15, CSS Color 5 § 3: `color-mix()` w przestrzeniach
  `srgb`, `srgb-linear`, `oklab`, `oklch`; bez `currentColor` w argumentach;
- `css/selector/parser.go`, `pseudo_classes.go`, `serialize.go`, 2026-11-15, Selectors 4:
  `:nth-child(an+b of S)` i `:nth-last-child(… of S)`; `:has()` z selektorem względnym (`> a`, `+ a`, `~ a`,
  tylko selektor złożony bez dalszych kombinatorów); `:any-link`; `:focus-visible` i `:focus-within`
  jako nigdy niepasujące, tak jak `:focus`;
- `text/draw/draw_gotext.go`, 2026-11-15: jeden osadzony font na krój zamiast jednego na parę (krój,
  rozmiar pisma); nazwa podzbioru nie zależy już od rozmiaru. W opisie fontu (ISO 32000-1 § 9.8.1,
  wartości w tysięcznych firetu) `Ascent`, `Descent` i `FontBBox` liczone przez `upem`; źródło dzieliło
  jednostki fontu przez rozmiar pisma (Ascent rzędu 50 000 zamiast 1069) i odwracało oś y obrysu.
  Skutek: korpus 21 dokumentów 3316 kB → 2020 kB (pomiar `du`, 2026-11-15), obraz bez zmian, a ramki
  słów z `pdftotext -bbox` zgadzają się z wydrukiem Chromium do 0,3 pt (wcześniej 2 pt w pionie);
- `html/layout/preferred.go` (`columnWrapContentWidth`), 2026-11-15, CSS Flexbox 1 § 9.9.1: szerokość
  wewnętrzna kontenera flex w kolumnie z zawijaniem i o podanej wysokości (albo `max-height`) to suma
  szerokości jego linii z odstępami; źródło brało szerokość najszerszego elementu. Działa, gdy każdy
  element ma podany rozmiar w osi głównej (`flex-basis` albo `height`); w pozostałych przypadkach
  zostaje zachowanie źródła;
- `html/layout/grid.go` (`gridLayout` → `gridLayoutPass`), 2026-11-15, CSS Grid 1 § 11.5, CSS Align 3 § 9.3:
  wyrównanie do linii bazowej powiększa wiersz o rozmiarze wyznaczanym z treści, gdy przesunięte
  elementy się w nim nie mieszczą. Robione drugim przebiegiem układu siatki z dodatkami do wierszy
  zmierzonymi w pierwszym;
- `text/text.go` (`strutLayout`), `text/engine_gotext.go` (`lineGap`), `text/fonts.go`, 2026-11-15,
  CSS Inline 3 § 4.2: `line-height: normal` obejmuje odstęp międzywierszowy fontu (lineGap);
  obszar treści pudełka liniowego go nie obejmuje. Odstęp dzieli `text.SplitLeading`: górna część
  zaokrąglona w dół do pikseli, reszta pod spodem, więc linia bazowa zostaje na siatce pikseli. Przy
  podanym `line-height` interlinia jest nadal dzielona dokładnie po połowie (zaokrąglanie oddalało tekst
  od wydruku Chromium o ok. 0,3 pt, pomiar na korpusie 2026-11-16). Źródło odstęp
  pomijało: tekst 14 px krojem Tinos miał wiersze co 15 px, w Chromium co 16 px (teraz zgodnie).
  Zmieniona asercja w `TestEmptyInlineAutoMargins` (połowa interlinii zależy od fontu);
- `goweasyprint/pdf/text_font_subset.go`, 2026-11-15, ISO 32000-1 § 9.9: podzbiór fontu TrueType zawiera
  tylko tabele potrzebne do rysowania (`head`, `hhea`, `maxp`, `hmtx`, `loca`, `glyf`, `post`, `name`,
  `OS/2`, `cvt `, `fpgm`, `prep`, `gasp`, `cmap`); `hmtx` i `maxp` są ucinane za ostatnim użytym glifem,
  a `cmap` jest budowana od nowa (format 12) z samych zostających glifów. Źródło kopiowało `hmtx`,
  `cmap` i `GDEF` w całości. Pusta `cmap` zmieniała rysowanie w CoreGraphics i Ghostscripcie, stąd
  odwzorowanie przeniesione z oryginału. Faktura z xml.pro: 199 kB → 32 kB (Chromium: 43 kB), obraz
  identyczny w poppler, Ghostscripcie i CoreGraphics;
- `html/tree/style.go` (`monospaceFontSize`, `fontSizeFromKeyword`), `html/tree/tree.go`
  (`Behavior.MonospaceFontSize`), 2026-11-16, ADR 0008: rozmiar pisma wywiedziony ze słowa kluczowego
  jest przeliczany 13/16 dla rodziny będącej samym `monospace`, jak w przeglądarkach; źródło dawało 16 px;
- `html/tree/computed_values.go` (`borderWidth`, `snapBorderWidth`), `html/tree/tree.go`
  (`Behavior.ExactLengths`, do ADR 0013 `ExactBorderWidths`), 2026-11-16, ADR 0009, CSS Values 4 § 6.1: `border-*-width`,
  `outline-width` i `column-rule-width` są przyciągane do całych pikseli CSS (poniżej 1 px rosną do 1 px,
  większe są obcinane w dół), jak w Chromium; źródło trzymało szerokości dokładne. Test odziedziczony
  `TestLinks` (obramowanie `1pt`) i pozostałe testy `goweasyprint/pdf` renderują z `ExactLengths`;
- `html/layout/block.go` (`blockContainerLayout`, `hasPlacedChild`), `html/layout/flex.go`,
  `html/boxes/boxes.go` (`MarginTopTruncated`), 2026-11-16, CSS Fragmentation 3 § 5.2: górny margines
  przylegający do zwykłego podziału strony jest obcinany do zera także dla kontenerów flex i siatki
  oraz elementów zastępowanych; w źródle robiły to tylko bloki i tabele, więc np. podsumowanie faktury
  w `display: flex` z `margin-top: 5mm` zaczynało stronę 14 pt niżej niż w Chromium;
- `html/layout/grid.go`, 2026-11-16, trzy poprawki siatki przy `row-gap` i podziale stron:
  - rozmieszczanie automatyczne (`growRows`): wiersze niejawne dopisywane w jednym miejscu; źródło
    dopisywało je w kilku i myliło się o jeden, więc siatka o n ≥ 3 wierszach miała 3n − 4 ścieżki,
    a każda pusta dokładała `row-gap` do wysokości kontenera (CSS Grid 1 § 8.5);
  - dalszy ciąg siatki po podziale strony: `skipHeight` liczy odstęp przed każdym pominiętym wierszem;
    źródło liczyło o jeden mniej i fragment zaczynał stronę niżej o `row-gap`;
  - ostatni wiersz, którego elementy mają podaną wysokość i który nie mieści się na stronie, przechodzi
    na następną (ADR 0004); źródło nie sprawdzało ostatniego wiersza i zostawiało go poza stroną
    (dla wierszy o wysokości auto działało to przypadkiem, dzięki nadmiarowym wierszom niejawnym);
- `html/layout/grid.go`, 2026-11-16, dalsze poprawki siatki:
  - `getColumnPlacement`: element z podanym wierszem i kolumną auto trafia do pierwszej kolumny, gdy
    wiersz jest pusty (CSS Grid 1 § 8.5); źródło dawało kolumnę drugą;
  - rozciąganie ścieżek (`align-content`/`justify-content: stretch`) dotyczy ścieżek o funkcji maksimum
    `auto` (§ 11.8); źródło patrzyło na funkcję minimum;
  - `gap` zapisany jako `calc()` z procentem zachowuje część w pikselach (także w `flex.go`); `row-gap`
    procentowy przy wysokości auto liczy się jak zero do rozmiaru kontenera i od wynikłej wysokości przy
    rozkładaniu wierszy; `column-gap` procentowy w `inline-grid` odnosi się do jego szerokości
    (CSS Box Alignment 3 § 8.3);
  - `gridContentWidth`, `gridMeasure` (nowe), `preferred.go`: szerokość min-content i max-content
    kontenera siatki to suma kolumn i odstępów (§ 5.2, § 11.1); źródło liczyło siatkę jak blok, więc
    `inline-grid` i siatka pływająca miały szerokość najszerszego elementu albo zero;
- paczka 2026-11-16 (trzecia): siatka, obrazy w siatce, pola marginesów:
  - `html/layout/grid.go` (`distributeExtraSpace`): rozdział miejsca dla elementu rozpiętego na kilka
    ścieżek według CSS Grid 1 § 11.5.1: po równo z zamrażaniem ścieżek na granicy wzrostu i dalszym
    rozdawaniem reszty (krok 2.2), potem ponad granice (krok 2.4, w źródle TODO). Źródło robiło jedną
    rundę, a ścieżce już przekraczającej granicę dawało przyrost ujemny;
  - `html/layout/grid.go` (`resolveTracksSizes`): o tym, czy element rozpięty obejmuje ścieżkę `fr`,
    decydują ścieżki, które zajmuje; źródło brało zakres od numeru elementu w kolejności iteracji po
    mapie, czyli losowo (niedeterminizm);
  - `html/layout/grid.go` (układ elementów): dla elementu zastępowanego `justify-self` i `align-self`
    o wartości `normal` działają jak `start` (§ 6.2); źródło rozciągało obraz na cały obszar siatki;
  - `html/layout/pages.go` (`computeVariableDimension`): pola marginesów dzielą krawędź strony według
    rozmiarów zewnętrznych (CSS Page 3 § 5.3.2.3); źródło podawało rozmiar samej treści jako zewnętrzny,
    więc pole z marginesem, obramowaniem albo odstępem wychodziło za wąskie;
  - `html/tree/style.go` (`viewport.size`): `vw` i `vh` przy `@page { width; height }` liczą się od
    podanego obszaru strony; wcześniej od domyślnego rozmiaru arkusza;
- paczka 2026-11-16 (czwarta): `::first-line`, elementy pozycjonowane w siatce, pola marginesów w pionie:
  - `html/tree/first_line.go` (nowy), `html/boxes/boxes.go` (`FirstLineStyle`), `html/boxes/build.go`,
    `html/layout/inline.go` (`firstLineBox`, `firstLineTarget`, `getNextLinebox`), `html/layout/block.go`:
    obsługa `::first-line` (CSS Pseudo-Elements 4 § 2.1). Źródło wyliczało styl pseudoelementu i nigdzie go
    nie używało. Pierwszy sformatowany wiersz bloku (także w pierwszym bloku zagnieżdżonym) układany jest
    z kopii pudełka wiersza: treść obejmuje pudełko liniowe pseudoelementu, tekst i pudełka liniowe
    dziedziczą po nim, a z kaskady pseudoelementu brane są tylko właściwości, które on przyjmuje (§ 2.1.1);
  - `html/layout/preferred.go` (`blockContentWidth`): w szerokości max-content kolejne elementy pływające
    sumują się, `clear` zaczyna nowy rząd, blok z własnym kontekstem formatowania staje obok nich
    (CSS Sizing 3 § 5.1). Źródło brało największe dziecko, więc kontener o szerokości z treści z dwoma
    elementami pływającymi miał szerokość jednego i drugi spadał niżej;
  - `html/layout/preferred.go` (`inlineLineWidths`): zachowane spacje na końcu złamanego wiersza przy
    `white-space: pre-wrap` nie liczą się do szerokości min-content (CSS Text 3 § 4.1.3: wiszą);
  - `html/layout/absolute.go` (`_absoluteWidth`): przy `left`, `right` i `width` równych `auto` szerokość
    dopasowana do treści ma do dyspozycji miejsce od pozycji statycznej do końca pudełka zawierającego
    (CSS 2.1 § 10.3.7, reguła 3); źródło dawało całą szerokość pudełka zawierającego;
  - `html/layout/grid.go`: dalszy potomek pozycjonowany, dla którego kontener siatki jest pudełkiem
    zawierającym, wskazuje obszar właściwościami `grid-column` i `grid-row` (CSS Grid 1 § 9); wcześniej
    tylko dziecko bezpośrednie. Prostokąt pozycji statycznej dziecka ze wskazanymi liniami to cały obszar
    siatki, także w osi bez wskazanych linii (CSS Box Alignment 3 § 4.2);
  - `html/layout/pages.go` (`verticalBox`, `makeMarginBoxes`): wysokość min-content i max-content pola
    marginesu to wysokość jego treści w ustalonej szerokości, a szerokość pól z lewej i z prawej strony
    jest ustalana przed wysokością (CSS Page 3 § 5.3.2.4). Źródło miało tu TODO i zwracało 0 oraz 1e6,
    więc pola dzieliły krawędź po równo, niezależnie od treści;
  - `html/layout/block.go`, `html/layout/tables.go`, `html/layout/layout.go` (`cellPositioned`): pudełka
    pozycjonowane względem komórki tabeli są układane ponownie, gdy komórka dostanie wysokość wiersza
    (CSS 2.1 § 10.1); źródło liczyło `bottom` i wysokości procentowe od wysokości treści komórki, więc
    np. obrócony podpis w nagłówku kolumny wystawał ponad tabelę;
- podział między strony, 2026-11-16 (piąta paczka):
  - `html/layout/flex.go` (`forcedBreakAtEdge`): żądanie podziału strony na pierwszym albo ostatnim
    dziecku w przepływie przechodzi na element flex (CSS Break 3 § 3.1), więc `break-before` na pierwszym
    elemencie zagnieżdżonego kontenera dzieli strony przed tym kontenerem;
  - `html/layout/flex.go` (`linePushed`, `pushedFlexItem`, `finishedFlexItem`), `html/layout/layout.go`
    (`flexPushOffset`): element wiersza, który nie mieści się na stronie, przechodzi na następną sam
    (ADR 0011); element zakończony na poprzedniej stronie nie wnosi już górnego marginesu;
  - `html/layout/block.go` (`fixedHeightRemaining`, `resumeKey`): blok o podanej wysokości, którego dzieci
    dzielą się między strony, zajmuje na stronie miejsce do jej dolnej krawędzi i przenosi dalej resztę
    wysokości (CSS Fragmentation 3 § 5.1). W źródle każdy fragment dostawał całą podaną wysokość, więc
    blok wyższy niż strona był przenoszony na następną, a jego ostatni fragment wypełniał całą stronę;
- `html/tree/media_query.go` (`SetMediaReference`, `MediaSizeSeen`), `html/tree/style.go` (`PageArea`),
  `html/layout/layout.go` (`MediaPageArea`), 2026-11-16: cechy `width`, `height`, `aspect-ratio`
  i `orientation` w `@media` oceniane względem obszaru strony z `@page` (Media Queries 4 § 4.1, ADR 0012);
  wcześniej względem stałej strony A4 (ADR 0007);
- paczka 2026-11-16 (szósta):
  - `html/tree/computed_values.go` (`borderSpacing`), `html/tree/tree.go` (`Behavior.ExactLengths`,
    wcześniej `ExactBorderWidths`), `html/tree/style.go` (`snapsLengths`): `border-spacing` obcinane do
    całych pikseli jak w Chromium; jeden przełącznik dla wszystkich takich zaokrągleń (ADR 0013);
  - `html/boxes/boxes.go` (`PageValues`): nazwa strony (`page`) jest brana tylko z dzieci blokowych
    (CSS Page 3 § 6.1); źródło schodziło przez wiersz do pudełek liniowych, więc `page` na obrazie albo
    pudełku `inline-block` zmieniało nazwę strony całego bloku;
  - `html/layout/grid.go` (`setDefiniteHeight`, `fixedRowsHeight`, `stretchesInBlockAxis`),
    `html/boxes/boxes.go` (`DefiniteHeight`), `html/layout/preferred.go`: element siatki w wierszach
    o stałym rozmiarze zna swoją wysokość już przy liczeniu kolumn, więc wysokość procentowa, rozciąganie
    i proporcje wchodzą do jego szerokości z treści (CSS Grid 2 § 12.1, CSS Sizing 4 § 5.1);
  - `html/document/stacking.go` (`dispatch`): elementy flex i grid rysowane są jak pudełka
    `inline-block`, każdy w całości i w kolejności dokumentu (CSS Flexbox 1 § 5.4, CSS Grid 1 § 9.5);
    źródło rysowało je jak zwykłe bloki, więc tekst wcześniejszego elementu przebijał przez tło
    późniejszego;
- `html/layout/layout.go` (`Layout`, `layoutScaled`, `contentOverflowScale`), `html/layout/pages.go`
  (`makePage`), `html/boxes/boxes.go` (`PageScale`), `html/document/document.go` (`getMatrix`),
  `html/document/stacking.go`, `html/tree/tree.go` (`Behavior.MaxShrink`), 2026-11-16, ADR 0014: treść
  szersza niż obszar strony jest układana drugi raz na powiększonym obszarze i rysowana pomniejszona;
  źródło zostawiało ją wystającą poza stronę;
- błędy znalezione 2026-11-16 przy porównaniu korpusu z wydrukiem Chromium:
  - `text/engine_gotext.go` (`privateLayout`, `ApplyJustification`): wynik z pamięci podręcznej układów
    tekstu dostaje własny obiekt układu, a odstęp justowania jest dodawany do kopii glifów i tylko raz.
    W źródle obiekt i glify były współdzielone: wiersz o tym samym tekście (albo to samo pudełko
    rysowane ponownie) dostawał odstępy justowania wielokrotnie i wystawał poza prawy margines
    (`04-qms-procedure`: 426 słów przesuniętych o ponad 3 pt);
  - `html/layout/block.go` (`blockContainerLayout`): wiersz bez treści (pusty element liniowy między
    blokami) nie przerywa zwijania marginesów ani zwijania przez pudełko (CSS 2.1 § 9.4.2);
  - `html/boxes/build.go` (`markerToBox`): znacznik-symbol (`disc`, `circle`, `square`) przy
    `list-style-position: inside` jest pudełkiem `inline-block` o szerokości 1,36 em (pomiar Chromium;
    CSS Lists 3 § 3.1 zostawia to implementacji); w źródle szerokością był glif z odstępem, ok. 0,6 em.
    Test odziedziczony `TestListsStyle` dostosowany do nowej budowy znacznika;
- kontrolki formularzy, 2026-11-16 (porównanie `13-forms-html-elements` z wydrukiem Chromium):
  - `html/tree/html5_ua.css`: pole tekstowe 12 em (ok. 20 znaków), wysokość pól 1.36em z odstępem 2px,
    przycisk i lista wyboru o szerokości treści, pole wielowierszowe domyślnie 20 znaków na 2 wiersze,
    pole wyboru 13 px z marginesami jak w arkuszu Chromium; w przejętym arkuszu każda kontrolka miała
    20 em szerokości. Odstępy ramki `fieldset` według WHATWG HTML § 15.3.13 (.35em .75em .625em; w źródle
    boki i dół zamienione);
  - `html/tree/style.go` (`findStyleAttributes`): atrybuty `cols` i `rows` elementu `textarea` wyznaczają
    jego rozmiar (WHATWG HTML § 15.5.14); źródło je pomijało;
  - `html/layout/block.go` (`prepareFieldsetLegend`): treść ramki zaczyna się pod legendą, w odległości
    górnego odstępu ramki; wcześniej o ten odstęp za wysoko;
- kolumny, 2026-11-16 (CSS Multicol 1, paczka pierwsza):
  - `html/layout/column.go` (`hoistSpanners`, `mayHoldSpanner`), `html/boxes/boxes.go`
    (`SpannerSplitBefore`, `SpannerSplitAfter`), `html/layout/block.go`: element z `column-span: all`
    rozpina kolumny także wtedy, gdy stoi głębiej niż dziecko kontenera (§ 6.1); przodek jest dzielony
    na fragmenty przed elementem i po nim, bez obramowań w miejscu podziału. Źródło obsługiwało tylko
    bezpośrednie dzieci kontenera;
  - `html/layout/block.go` (`canSplitFixedHeight`, warunek `splittable`): blok o podanej wysokości jest
    dzielony na dole kolumny (ADR 0015); w źródle treść kolumn nie była dzielona wcale. Pudełka
    niepodzielne w kolumnach nie dzielą też swojej treści;
  - `html/layout/column.go`: górny margines kontenera kolumn zwija się z marginesami sąsiednimi (CSS 2
    § 8.3.1); w źródle nie był doliczany, więc kontener z marginesem większym niż margines poprzednika
    stał za wysoko;
  - `html/layout/column.go`: wysokość równoważenia liczona z całej wysokości treści razem z dolnymi
    marginesami, a kolumna podzielona w środku pudełka liczy się do końca ostatniego wiersza
    (`fragmentBottom`); kontener ma wysokość z równoważenia, a dolny margines ostatniego pudełka, który
    się w niej nie mieści, jest obcinany (CSS Break 3 § 5.2). W źródle podzielone pudełko uchodziło za
    wypełniające kolumnę, a kontener rósł o margines; kolumny wychodziły o kilka punktów wyższe niż
    w przeglądarkach (dokument `21-mixed-report`);
  - `html/layout/column.go`: krok równoważenia uwzględnia grupę pudełek, których nie wolno rozdzielić
    (`break-before`/`break-after: avoid`), i liczy ostatnie z nich tylko do pierwszego możliwego podziału
    (`firstPieceHeight`); w źródle liczyło się tylko pierwsze pudełko następnej kolumny;
  - `html/layout/column.go`: kolumny nie są wyższe niż reszta podanej wysokości kontenera, a dodatkowe
    kolumny w kierunku wiersza powstają tylko wtedy, gdy ogranicza je ta wysokość, a nie koniec strony
    (§ 8.2); `context.inColumn` jest przywracany po wyjściu z kontenera zagnieżdżonego;
  - `html/document/draw_border.go` (`drawColumnRules`): style `inset` i `outset` linii między kolumnami
    to `ridge` i `groove` (CSS Multicol 1 § 4.2, model obramowań zwijanych); kolory połówek jak w lewym
    obramowaniu;
  - `html/layout/column_test.go`: `TestColumnsRegression1` i `TestColumnsRegression2` przepisane, bo
    oczekiwały przenoszenia bloków o podanej wysokości w całości (zob. ADR 0015);
- kolumny, 2026-11-16 (paczka druga, porównanie `22-multicol-pagination` z wydrukiem Chromium):
  - `html/layout/block.go` (podział bloku o podanej wysokości): miejsce do dołu strony albo kolumny
    liczone od położenia po zwinięciu marginesów; pudełko bez górnego obramowania dostaje je dopiero
    na końcu układu, więc po przesunięciu wystawało i było odrzucane w całości;
  - `html/layout/column.go`: przed wymuszonym podziałem kolumny dolny margines ostatniego pudełka
    zostaje i liczy się do wysokości kolumn (CSS Break 3 § 5.2);
  - `html/layout/column.go`: kontener kolumn o podanej wysokości, którego treść przechodzi na następną
    stronę, zajmuje miejsce do dołu strony i przenosi resztę wysokości (`context.fixedHeightRemaining`);
    w źródle każdy fragment miał całą wysokość i kontener był przenoszony w całości;
  - `html/layout/column.go`: wysokości procentowe dzieci liczone od podanej wysokości kontenera
    (`AnonymousBaseHeight` pudełka kolumny); w źródle od auto;
  - `html/layout/column.go` (`resetSpannerSplitUsed`, `addSpannerSplitUsed`), `html/boxes/boxes.go`
    (`SpannerSplitUsed`), `html/layout/block.go`: przodek o podanej wysokości podzielony elementem
    rozpinającym rozkłada ją na fragmenty (przed elementem wysokość treści, na końcu reszta);
  - `html/layout/column_test.go`: `TestColumnsRegression3` przepisany (oczekiwał bloku 60px w całości
    w pierwszej kolumnie; zob. ADR 0015);
- `html/layout/tables.go`, 2026-11-16 (faktura KSeF z 60 pozycjami wobec Chromium): wiersz tabeli,
  w którym jedna komórka nie zmieściłaby na stronie żadnej treści, a inna by zmieściła, przechodzi na
  następną stronę cały, o ile nie jest pierwszym wierszem grupy na stronie (CSS Break 3 § 3.1: podział
  między wierszami przed podziałem łamiącym `orphans`/`widows`). W źródle komórki jednowierszowe
  zostawały na dole strony, a wielowierszowa przechodziła dalej;
- łamanie wierszy, 2026-11-16 (CSS Text 3 § 5; testy W3C `css-text`):
  - nowa właściwość `line-break` (`css/properties`, `css/validation` `lineBreak`, `text/style.go`
    `WBAnywhere`): `anywhere` łamie między dowolnymi znakami; `loose`, `normal`, `strict` są przyjmowane
    i działają jak `auto`. Źródło właściwości nie znało;
  - `text/engine_gotext.go` (`breakAllAllowed`): `word-break: break-all` łamie między literami i cyframi,
    ale zachowuje reguły UAX 14 dla znaków przestankowych (wiersz nie zaczyna się od kropki ani od
    U+00A0); wcześniej łamało w dowolnym miejscu;
  - `html/layout/inline.go`: między U+00A0 a elementem atomowym (obraz, `inline-block`) wiersz wolno
    złamać (§ 5.1, wyjątek dla zgodności ze stronami WWW);
  - `html/layout/inline.go` (`splitTextBox`): przy podanym `line-height` linia bazowa i interlinia tekstu
    liczą się z metryk pierwszego dostępnego fontu (CSS Inline 3 § 4.2); w źródle z fontu, z którego
    pochodził glif, więc znak z fontu zastępczego powiększał wiersz;
  - `text/engine_gotext.go` (`isPreservedSpace`): spacja ideograficzna U+3000 wisi na końcu wiersza
    i przechodzi do następnego przy `break-spaces` jak zwykła spacja;
- wiszące odstępy i tabulatory, 2026-11-17 (CSS Text 3 § 4.1.3, § 4.2, § 6.4; testy W3C `css-text`):
  - `text/engine_gotext.go` (`LineWrap`, `trimTrailingSpace`): przycinanie końcowych odstępów w bibliotece
    go-text jest wyłączone, bo odbierało szerokość każdemu ostatniemu glifowi bez obrysu, także spacji
    nierozdzielającej (U+00A0) i ideograficznej (U+3000). Szerokość traci tylko zwykła spacja. Wiersz,
    który nie mieści się przez końcową spację nierozdzielającą, jest łamany wcześniej (`WrapWordBreak`);
  - `text/text.go` (`FirstLine.Hang`), `html/layout/preferred.go` (`trailingWhitespaceSize` z parametrem
    `ideographic`, `inlineLineWidths`): spacja ideograficzna na końcu wiersza wisi: zostaje w wierszu
    z szerokością i tłem, ale nie liczy się do przepełnienia ani do szerokości wewnętrznych;
  - `html/layout/inline.go` (`trailingPreservedWidth`, `textAlign` z parametrem `hang`),
    `text/engine_gotext.go` (`TrailingSpacesWidth`, `SetHangingSpaces`): przy `pre-wrap` spacje
    i tabulatory na końcu wiersza zachowują szerokość i tło, a nie liczą się do wyrównania ani justowania,
    także gdy podział wypada na granicy elementów. W wierszu ostatnim albo zakończonym wymuszonym
    podziałem wisi tylko część, która się nie mieści. Zastępuje to zerowanie szerokości z 2026-11-16;
  - `html/layout/inline.go` (`textAlign`, `justifyLine`, `lineHasTab`): tekst `pre-wrap` i `break-spaces`
    jest justowany (norma zostawia wybór, tak robi Chromium; źródło justowało tylko tekst ze zwijanymi
    spacjami); wiersz z tabulatorem nie jest justowany;
  - `text/engine_gotext.go` (`WrapWordBreak`): przy `break-spaces` z `line-break: anywhere` wiersz wolno
    złamać także przed zachowaną spacją;
  - `text/engine_gotext.go` (`IsOtherSpaceSeparator`): wiszą wszystkie odstępy kategorii Zs poza spacją
    i spacją nierozdzielającą (U+1680, U+2000–U+200A, U+202F, U+205F, U+3000);
  - `text/engine_gotext.go` (`LineWrap`, `shapeWithResiduals`, `contentWidth`): szerokości znaków są
    zaokrąglane do 1/64 px każda z osobna, więc N znaków fontu o stałej szerokości bywało szersze niż
    pudełko `N ch` i ostatni znak spadał do następnego wiersza. Silnik pamięta reszty zaokrągleń i wiersz
    leżący na granicy (do pół piksela ponad szerokość zaokrągloną) przyjmuje wtedy, gdy jego dokładna
    szerokość mieści się w dostępnej;
  - łamanie w dowolnym miejscu między elementami liniowymi (§ 5.5): `html/layout/inline.go`
    (`splitInlineBox`, `breaksAnywhere`, `context.forceWordBreak`): gdy w wierszu nie ma miejsca
    podziału, a styl pozwala łamać w dowolnym miejscu, pudełko, które się nie mieści, jest łamane tak,
    żeby dopełnić wiersz, a jeśli się nie da, wiersz łamie się przed nim. `html/layout/preferred.go`
    (`inlineLineWidths`): granica fragmentów tekstu jest wtedy miejscem podziału dla min-content.
    W źródle podział był możliwy tylko wewnątrz jednego fragmentu tekstu na początku wiersza;
  - `css/validation` (`wordBreak`), `text/style.go`: `word-break: break-word` działa jak
    `overflow-wrap: anywhere` (§ 5.2, dawna wartość); źródło ją odrzucało;
  - `word-break: break-all` na granicy elementów zachowuje regułę dla znaków przestankowych: wiersz
    nie łamie się przed kropką (`breakAllOnly`);
  - tabulatory: `html/layout/inline.go` i `layout.go` (`lineStartX`, `lineTabUnit`, `splitTextBoxAt`),
    `text/text.go` (`SplitFirstLineAt`, `TabUnit`), `text/style.go` (`TabOffset`, `TabUnit`): pozycje
    tabulacji liczą się od początku wiersza, a nie od początku fragmentu tekstu, więc tabulator po
    elemencie liniowym trafia na właściwą pozycję; odstęp tabulacji to wielokrotność szerokości spacji
    bloku powiększonej o `letter-spacing` i `word-spacing`; tabulator nie dostaje odstępu między literami;
    próg „mniej niż pół znaku do pozycji tabulacji” liczony z jednostki `ch` (było: 1/16 odstępu);
    `tab-size` może być ułamkowy (`css/validation` `tabSize`, `text/style.go` `TabSize.Width`; źródło
    przyjmowało tylko liczby całkowite i obcinało długości do całych pikseli);
- listy i treść generowana, 2026-11-18 (CSS Lists 3, CSS Content 3, CSS Pseudo 4; praca pomocnika):
  - `html/boxes/build.go`, `reversedCounterStart`: wartość początkowa licznika odwróconego dokładnie według
    CSS Lists 3 § 4.4.2 (liczone są też `::before` i `::after`; przy `counter-set` dolicza się ustawianą
    wartość bez przyrostu tego elementu, a ostatni niezerowy przyrost dolicza się zawsze); licznik
    zagnieżdżony w liczniku przodka nie obejmuje rodzeństwa;
  - `html/tree/computed_values.go`: `::marker { content: none }` zostaje `none` i nie tworzy znacznika
    (§ 3.1; źródło zamieniało `none` na `inhibit`); element z `display: contents` nie zmienia liczników
    (§ 4.5); `counter-*` działa w `::marker`, a licznik założony przez znacznik widzi reszta elementu listy;
  - `css/selector/parser.go`, `html/tree/style.go` (`nestedMarkerStyle`): `::before::marker`
    i `::after::marker` (CSS Pseudo 4 § 4.2) dziedziczą po swoim pseudoelemencie, a samo `::marker` ich nie
    wybiera; test przejęty `TestCounters8` oczekuje nazwy `p::before::marker`;
  - `html/boxes/build.go` (`replacementImage`, `contentImage`), `css/validation/utils.go`,
    `css/properties/utils.go` (`ContentImage`): pojedynczy `<image>` w `content` zastępuje zawartość
    elementu (CSS Content 3 § 1, także `<img>` i element główny); gradienty i `image-set()` (CSS Images 4
    § 2.2, wariant 1x; działa też w tłach) w `content`; obraz będący całą treścią `::before`/`::after` ma
    `width` i `height` pseudoelementu (model Chromium); `images/gradients.go`: gradient przycinany do
    swojego rozmiaru (źródło malowało cieniowanie bez przycięcia);
  - `quotes: match-parent` (CSS Content 3 § 4.1; `keywords_src.go` i wygenerowany `keywords_gen.go`);
  - `html/layout/absolute.go`, `block.go` (`shiftOutsideMarkerBesideFloat`): strona znacznika zewnętrznego
    według kierunku elementu listy, nie `::marker`; znacznik przesuwa się w poziomie na początek pierwszego
    wiersza leżącego bezpośrednio w elemencie listy, gdy wiersz skraca element pływający spoza elementu
    (bez przesunięcia przy elementach pływających wewnątrz, `text-indent` i `text-align` center/right, jak
    w Chromium). Nadal sprzeczne testy W3C o dziedziczeniu liczników od rodzeństwa
    (`counter-reset-reversed-siblings-003`, `counters-scope-002/003`, `counter-list-item-2`,
    `CSS2/content-counter-006…008`, `counters-010` przeciw `counter-001…003`, `li-value-reversed-007`);
- ogon CSS Text, 2026-11-18 (CSS Text 3 i 4; praca pomocnika):
  - odstęp między literami (CSS Text 3 § 7.2): na końcu wiersza go nie ma; odstęp za ostatnim znakiem
    tekstu dodawany po zawinięciu i wiszący przy sprawdzaniu mieszczenia (`removeLastLetterSpacing`,
    `TrailingLetterSpacing`); znaki Cf, znaki klasy BK i znak przed nimi oraz znak za tabulatorem bez
    odstępu; ciąg elementów atomowych liczy się jak jeden znak. Źródło dodawało pełny odstęp za ostatnim
    znakiem przed zawijaniem; test przejęty `TestLetterSpacing1` oczekuje 33 odstępów zamiast 34;
  - `white-space-trim` (CSS Text 4 § 4.2; nowy `boxes/whitespace_trim.go`) i `white-space-collapse`
    (§ 4.1) jako długa forma części `white-space` (bez `discard` i `preserve-spaces`); skrót `white-space`
    ustawia `white-space-trim` na `none`;
  - wiszące białe znaki (CSS Text 3 § 4.1.3): odstępy Zs przy zwijanych białych znakach wiszą
    bezwarunkowo, także przez kilka pudełek tekstu; ciąg pre-wrap przed wymuszonym złamaniem wisi
    warunkowo w całości; także dla min-content;
  - `hyphens: auto` dla wyrazów rozciętych granicami elementów (nowy `boxes/hyphenate_across.go`,
    `TextBox.ManualHyphens`); puste pudełko liniowe nie blokuje podziału, a znak dzielenia pojawia się także
    przed takim pudełkiem;
  - `html/layout/block.go`: pozycja statyczna liniowego elementu absolutnego stojącego przed pierwszym
    wierszem bloku uwzględnia `text-indent` (CSS 2 § 10.3.7; tylko ltr, bez `hanging`);
- ogon CSS 2 i tła, 2026-11-18 (praca pomocnika): `:lang()` porównuje wielkość liter tylko w ASCII
  (`selector/parser.go`, `pseudo_classes.go`); element główny z `position: relative` jest przesuwany,
  a pływający układany jak element pływający (`layout/pages.go`); położenie statyczne bloku pozycjonowanego
  w pudełku liniowym bez treści przed nim to góra wiersza, a pudełka bezwzględne względem pudełka
  liniowego układane po wyrównaniu wiersza w pionie (`layout/inline.go`, `absolute.go`, `layout.go`);
  `clear` na `<br>` przenosi następny wiersz pod elementy pływające, element pływający o niedodatniej
  wysokości pudełka marginesowego nachodzi na element obejmujący jego wysokość, dopasowanie szerokości
  z tolerancją 0,01 px (`layout/float.go`); usuwanie spacji z początku wiersza przechodzi przez pustą resztę
  pudełka liniowego po wydzieleniu bloku, a `vertical-align: top/bottom` przesuwa też treść zagnieżdżonych
  pudełek (`layout/inline.go`); blok wydzielony z pudełka liniowego przejmuje jego `opacity` i `z-index`,
  `::first-letter` z literą w `::before` dziedziczy po `::before` (`boxes/build.go`, `tree/style.go`);
  `::first-letter` nie usuwa znaku nowego wiersza z `<br>`; podpis tabeli rysowany po tabeli
  (`document/stacking.go`, CSS 2.1 dodatek E); min/max obrazu bez proporcji ograniczają wymiary niezależnie
  (`layout/replaced.go`, § 10.4); `conic-gradient` i `repeating-conic-gradient` (`properties/types.go`,
  `validation/utils.go`, `images/gradients.go`, nowy `goweasyprint/pdf/conic.go`: cieniowanie funkcyjne typu 1
  z funkcją kalkulatora PostScript typu 4, przezroczystość przez maskę miękką); `background-attachment:
  local` w pudełku przewijanym pozycjonuje względem całej treści, `fixed` w elemencie przekształconym działa
  jak `scroll` (`layout/background.go`); tło elementu głównego SVG wyświetlanego jako obraz (`svg/svg.go`);
  maska `background-clip: text` obejmuje dekoracje tekstu i uwzględnia `text-overflow` (`document/draw.go`,
  `draw_text.go`). Poza przejętym kodem: `xhtml.go` zachowuje znak nowego wiersza po `<pre>` w XHTML;
- ogon flex i siatki, 2026-11-18 (praca pomocnika):
  - flex (`preferred.go`, `flex.go`): wkład elementu do szerokości wewnętrznej kontenera wierszowego
    ograniczony przez `flex-basis` (od góry bez wzrostu, od dołu bez kurczenia; `flex-basis: auto` bierze
    `width` w px), dla max-content większa z wartości treść i podana szerokość, bez minimum automatycznego
    dla elementów kurczliwych (CSS Flexbox 1 § 9.9.1); wysokość kontenera kolumnowego z
    `height: min-/max-/fit-content` jako suma wkładów; rozciągnięty rozmiar poprzeczny obrazu jest określony
    (§ 9.8, § 4.5); `width: max-content` nie jest rozciągane w kroku 11; rozmiar hipotetyczny respektuje
    `min-width`; obraz z samymi proporcjami ma rozmiar bazowy z dostępnej szerokości (CSS 2 § 10.3.2); obraz
    bez rozmiaru i proporcji zachowuje wysokość 150px w kolumnie; podpisy tabeli wchodzą do `flex-basis`
    w kolumnie; wysokość procentowa przy nieokreślonym bloku zawierającym nie jest określona, a wysokość
    kontenera wynika wtedy z linii;
  - wysokości procentowe przy szerokościach z treści (`boxes.go`, `grid.go`, `preferred.go`, `intrinsic.go`):
    nowe pole `BoxFields.HeightBase` (podstawa procentów wysokości znana przed układem),
    `primeFlexDefiniteHeights`, `setChildrenDefiniteHeight` w `resolveIntrinsicSizes`; szerokość z treści
    obrazu ograniczona przez `min-/max-height` przeniesione proporcjami (CSS 2.1 § 10.4);
  - siatka (`grid.go`): przy rozmiarze nieokreślonym rozmiary bazowe rosną do granic wzrostu (CSS Grid 1
    § 12.6); `auto-fill` z `min-height` przeniesionym przez `aspect-ratio` i bez ograniczenia pomiaru
    (§ 7.2.3.2); nieskończona granica wzrostu liczona jak rozmiar bazowy i ścieżki „nieskończenie rosnące”
    przy elementach rozpiętych (§ 11.5.1); `safe` w `justify-self`/`align-self` elementów;
  - `css/validation/logical.go`: skróty `place-*` dzielone według gramatyki wartości (`safe`/`unsafe`,
    `first`/`last baseline`); `place-content` z samym `baseline` daje `justify-content: start` (CSS Box
    Alignment 3); źródło nie dzieliło wartości wielowyrazowych;
  - `utils/urls.go`: usuwanie tabulacji i nowych wierszy z adresu URL (URL Standard, basic URL parser);
  - `layout/replaced.go`: element zastępowany bez wysokości własnej ma wysokość auto zamiast nil (panika
    przy `min-/max-height`);
- ogon kolumn i tabel, 2026-11-18 (praca pomocnika):
  - `html/tree/html5_ua.css`: usunięte `break-after: avoid; break-inside: avoid` dla `h1`–`h6`
    i `break-before: avoid` dla `ol, ul` (nie ma ich w arkuszu z HTML, rozdz. 15 „Rendering”, ani
    w przeglądarkach; dodał je WeasyPrint); w korpusie nagłówek w `11-edge-cases` stoi teraz jak u Chromium;
  - `html/layout/tables.go`: nagłówek albo stopka zdegradowane do zwykłej grupy wierszy w kolumnach i przy
    próbie bez miejsca (bottomSpace = +Inf) zmieniają kopię tabeli, nie pudełko źródłowe (wcześniej
    nagłówek tracił powtarzanie na stałe); komórka rozpięta na wierszu z `visibility: collapse` kończy się na
    poprzednim wierszu i przycina treść; kolumny z `visibility: collapse` nie zajmują szerokości ani
    odstępu, tabela się zwęża, komórki na nich rozpięte są przycinane, a całkiem w nich leżące ukryte
    (CSS Tables 3 § 9.2–9.3; tylko ltr);
  - `html/boxes/boxes.go`, `html/layout/column.go`: element rozpinający kolumny zakłada kontekst formatowania
    blokowego (`IsSpanner`, CSS Multicol 1 § 6.1); marginesy przodków przylegające do wyniesionego elementu
    rozpinającego zwijają się z jego marginesami (`SpannerMarginsBefore/After`), a pusty fragment przodka
    traci ten margines;
  - `html/layout/absolute.go`, `block.go`, `column.go`, `layout.go`: pudełka pozycjonowane bezwzględnie
    podzielone w kolumnie ciągną się od góry następnej kolumny z zachowaniem odległości od jej lewej
    krawędzi (`brokenBox.columnX`); pudełko z pozycją statyczną na samym dole kolumny zaczyna się w całości
    w następnej (`brokenBox.nextColumn`); tabela pozycjonowana między `left`/`right` i `top`/`bottom` ma
    własny rozmiar, a marginesy automatyczne ją wyśrodkowują (CSS 2.1 § 10.3.7, § 17.4);
  - `html/document/stacking.go`: dalsze fragmenty tego samego pozycjonowanego elementu rysowane zaraz po
    wcześniejszych; `draw_border.go`: linie między kolumnami rysowane po obramowaniu kontenera
    (CSS Multicol 1 § 4);
  - `html/layout/marker.go` (nowy, `alignOutsideMarker`, `alignColumnsMarker`): znacznik
    `list-style-position: outside` stoi w pionie obok pierwszego wiersza elementu i przechodzi do fragmentu
    z tym wierszem (CSS Lists 3 § 3.1), także dla elementu listy będącego kontenerem kolumn;
- flex, 2026-11-18 (CSS Flexbox 1; testy W3C `css-flexbox`; praca pomocnika):
  - `html/layout/flex.go`: krok 10 (`visibility: collapse`) zaimplementowany: po pierwszym przebiegu element
    zwinięty staje się rozpórką o zerowym rozmiarze głównym i rozmiarze poprzecznym swojej linii, a układ
    zaczyna się od kroku 3; odstępy (`gap`) przylegające do rozpórki znikają (CSS Align 3 § 8.1); w źródle
    „TODO”. Krok 8.1: `align-self: auto` przyjmuje `align-items`, odległości linii bazowej mogą być ujemne,
    `last baseline` tworzy osobną grupę przy końcu linii, a krok 7 szuka wtedy ostatniej linii bazowej
    (w źródle pierwszej). Kontener „jednowierszowy” to `flex-wrap: nowrap` (§ 6), nie kontener z jedną linią;
    `align-content: stretch` w wielowierszowym tylko powiększa linie; szerokość auto ogranicza
    `max-width`/`min-width`; wysokość nadana z zewnątrz i z `aspect-ratio` kontenera jest określona; element
    nierozciągany ma wysokość nieokreśloną dla procentów dzieci (§ 9.8); `safe` w `align-self`/`align-items`/
    `align-content` (CSS Align 3 § 4.4); element kolumny nierozciągany mierzony przy szerokości fit-content;
    wysokość auto kontenera kolumnowego z zawijaniem to najdłuższa linia (test przejęty
    `TestFlexDirectionColumnMaxHeight` oczekuje 80 zamiast 100, zgodnie z WPT
    `flex-column-reverse-multiline-item-position`). Tabele jako elementy flex: `flex-basis` tabeli
    z `content-box` powiększany o jej obramowanie i odstępy, `min-height`/`max-height` tabeli pomijane przy
    rozmiarze bazowym, `min-width` tabeli ogranicza element;
  - `html/layout/tables.go`: szerokość wymuszona przez układ flex to zawsze pudełko obramowania tabeli;
    `block.go`: opakowanie tabeli rośnie, gdy treść tabeli jest wyższa niż rozmiar z układu flex;
  - `html/layout/absolute.go`: kontener flex pozycjonowany bezwzględnie liczy wysokość (z `top`/`bottom`) przed
    szerokością, a `stretchedRatioContribution` korzysta z niej (§ 9.9.1);
  - `html/layout/preferred.go`: elementy z `visibility: collapse` w kontenerze wierszowym nie wnoszą szerokości
    ani odstępów; `min-width: max-content` podnosi udział min-content bloku (CSS Sizing 3 § 5.2); tabela
    `table-layout: fixed` z szerokością procentową ma umowną szerokość max-content 1e5 (Gecko/Blink:
    nieskończona); pusty blok z `aspect-ratio` i `min-height` ma szerokość wewnętrzną z proporcji
    (CSS Sizing 4 § 5.1);
- siatka, 2026-11-18 (CSS Grid 1; testy W3C `css-grid` poza `grid-lanes` i `subgrid`; praca pomocnika):
  - `html/layout/grid.go`, `absolute.go`: pozycja statyczna dziecka pozycjonowanego, gdy siatka nie jest
    pudełkiem zawierającym, to krawędzie treści kontenera, a `grid-row`/`grid-column` są pomijane (§ 9.2);
    obszar siatki jest pudełkiem zawierającym także w siatce pozycjonowanej bezwzględnie; `last baseline`
    wyrównuje jak `safe end`, `safe` jak w flex, a pudełko wychodzące przed początek pudełka zawierającego jest
    dosuwane do jego krawędzi treści (jak Chromium; źródło wracało do początku prostokąta pozycji statycznej);
    równe linie usuwają linię końcową, odwrócone się zamieniają, liczbowe `span` liczy się od linii określonej
    (§ 8.3.1, § 9.1); zastępniki dzieci pozycjonowanych stoją w kolejności dokumentu (CSS 2.1 dodatek E);
  - ścieżki o minimum auto biorą wkład minimalny elementu (§ 11.5, § 6.6): ograniczony sumą stałych maksimów,
    zero dla kontenera przewijania (poza elementami zastępowanymi), element zastępowany z procentową
    szerokością lub `max-width` jest ściśliwy (CSS Sizing 3 § 5.2.2); przy rozmiarze kontenera z treści
    ograniczony wkład min-content; wiersz o maksimum auto dostaje granicę wzrostu max-content (w źródle
    nieustawioną); rozmiar fr (§ 11.7.1): ścieżki nieelastyczne nie oddają rozmiaru bazowego, przy rozmiarze
    nieokreślonym rozmiar bazowy dzielony przez współczynnik (źródło mnożyło);
  - linie nazwane: N-ta linia o nazwie, a przy braku linie niejawne (§ 8.3; źródło kończyło na pierwszej
    pasującej i wychodziło o linię za daleko); siatka jawna bez szablonów ma zero ścieżek (źródło: 1×1),
    a siatka bez ścieżek ma linię na początku treści; liczba powtórzeń `auto-fill`/`auto-fit` z szerokości
    bloku, z określonego `max-width` albo najmniejsza wypełniająca `min-width` (§ 7.2.3.2; źródło zawsze
    jedno powtórzenie dla bloku o szerokości auto);
  - procentowe marginesy i odstępy elementu odnoszą się do szerokości obszaru siatki; wiersze procentowe przy
    wysokości auto rozwiązywane drugi raz w wysokości treści (§ 7.2.1); procentowy `row-gap` po
    `min-height`/`max-height`; kolumny rozwiązywane ponownie po wierszach dla elementów z wysokością
    procentową (§ 11.1 kroki 3–4); `aspect-ratio` kontenera siatki o wysokości auto (CSS Sizing 4 § 5.1);
  - `html/tree/html5_ua.css`: `marquee` jako `inline-block` (HTML, Rendering). Trzy testy `grid-lanes/`
    tracą przypadkowe zaliczenia (opierały się na siatce 1×1 i starym wyszukiwaniu linii nazwanych);
- tła, obramowania i cienie, 2026-11-18 (CSS Backgrounds 3 i 4; testy W3C `css-backgrounds`; praca pomocnika):
  `background-clip: border-area` i `border-area text` (CSS Backgrounds 4 § 3.6; pierścień przycinania dla
  obramowań pełnych i podwójnych, maska jasności dla pozostałych); każda warstwa tła przycinana do własnego
  `background-clip`, kolor do ostatniej (źródło przycinało wszystko do ostatniej); `goweasyprint/pdf/page.go`
  (`SetColorPattern`): wzorzec (obraz tła) zeruje stałą alfa wypełnienia i obrysu; `pdflib/contentstream`
  (`ForgetColors`, wołane z `NewGroup`): grupy (formy XObject) nie zakładają domyślnego koloru ani alfy, bo
  dziedziczą stan graficzny, a czarne tło maski miękkiej przejmowało bieżący kolor; `svg/elements.go`: SVG bez
  `viewBox` nie jest skalowane (SVG 2 § 8.2; źródło budowało viewBox z `width`/`height`); `clip: rect()`
  z `right`/`left` liczonymi od lewej krawędzi (CSS 2.2 § 11.1.2; źródło je zamieniało; test przejęty
  `TestClip` poprawiony); `space` z mniej niż dwiema kopiami rysuje jeden obraz; `round` zachowuje
  `background-position` (testy przejęte `TestBackgroundRepeatRound_2`, `_4` poprawione); `attachment: local`
  w pudełku przewijanym maluje do krawędzi odstępu; `currentcolor` w gradientach przyjmuje `color` pudełka
  przy rysowaniu (źródło: czerń); `border-image-width` z `em` i `calc()` liczone do pikseli; promienie cienia
  przy `spread` według CSS Backgrounds 3 § 7.1.1 (z wyjątkiem łuków stykających się na boku,
  csswg-drafts#7103); cień wewnętrzny o kształcie ujemnym wypełnia pudełko odstępu; cienie wierszy tabeli
  i samej tabeli; `box-decoration-break: slice` dla cieni fragmentów; tło `body` nie przechodzi na płótno przy
  zawieraniu malowania `html`/`body` (CSS Contain 2 § 3.3); element główny z `display: none` bez tła płótna;
  treść elementów zastępowanych przycinana do zaokrąglonej krawędzi treści; `::first-letter` pomijane przy
  tekście zaczynającym się od spacji nierozdzielającej; `html/layout/tables.go`: nadmiar wysokości tabeli
  trafia do wierszy bez wysokości własnej i bez komórek z wysokością (źródło czytało wysokości z już
  wyzerowanych stylów);
- fonty, 2026-11-18 (CSS Fonts 4 i 5; testy W3C `css-fonts`; praca pomocnika): właściwości `font-synthesis`,
  `font-synthesis-weight`/`-style`/`-small-caps`/`-position` (dotąd nieznane; synteza pogrubienia i pochylenia
  nadal nie jest wykonywana); deskryptor `unicode-range` w `@font-face` ogranicza tablicę cmap twarzy (był
  czytany, ale pomijany; nowy plik `text/font_face.go`); nowe deskryptory `size-adjust`, `ascent-override`,
  `descent-override`, `line-gap-override`; `font-size-adjust` liczony osobno dla każdego fontu (także
  zastępczego), z wartością `from-font` i z wysokością glifu x/H, gdy OS/2 jej nie podaje; klucz pamięci
  podręcznej układu tekstu obejmuje `font-size-adjust` i syntezę pozycji; `font-weight` jako dowolna liczba
  1–1000, `bolder`/`lighter` według tablicy CSS Fonts 4 (źródło: tylko wielokrotności 100); tekst o zerowym
  rozmiarze fontu zostaje w wierszu (źródło go usuwało; zmienione testy przejęte `TestTextFontSizeZero`
  i `TestAbsolutePositioning7`); jednostki `cap`, `ic`, `rex`, `rch`, `rcap`, `ric`; nazwa rodziny
  w cudzysłowie równa słowu kluczowemu rodziny generycznej jest zwykłą nazwą fontu; niezerowy
  `letter-spacing` wyłącza ligatury opcjonalne, ZWNJ/ZWJ bez własnego odstępu; synteza indeksu górnego
  i dolnego (`font-variant-position`, proporcje jak Firefox, bo Chromium syntezy nie robi);
  `goweasyprint/pdf/text.go`: przesunięcie pionowe glifu (YOffset z kształtowania) zapisywane jako wzniesienie
  tekstu `Ts` (źródło je pomijało, także dla znaków diakrytycznych z GPOS); przebieg tekstu może mieć własny
  rozmiar (`backend.TextRun.SizeScale`). Trzy testy tracą przypadkowe zaliczenia:
  `italic-oblique-fallback` (go-text utożsamia italic i oblique), `small-caps-letter-spacing-002` (brak syntezy
  kapitalików), `text-autospace-preformatted-001` (wzorzec używa `ic`, dotąd nieważnej jednostki);
- kolumny, 2026-11-18 (CSS Multicol 1, CSS Fragmentation 3; testy W3C `css-multicol`; praca pomocnika;
  `html/layout/column.go`, `block.go`, `absolute.go`, `layout.go`): wysokość kolumn to wysokość treści pudełka
  ograniczona przez `min-height` i `max-height`, także przy `box-sizing: border-box` i przy wysokości nadanej
  przez flex albo siatkę; `max-height` bez `height` też ogranicza kolumny (§ 8.2); `column-width: 0` daje
  kolumny o szerokości co najmniej 1px, a kolumny o zerowej wysokości mają co najmniej 1px; w kolumnach
  kontener przewijania jest niepodzielny (CSS Fragmentation 3 § 4.1), a równoważenie rośnie co najmniej do
  wysokości pierwszego pudełka, które nie mieści się w pustej kolumnie; wyniesiony element rozpinający
  dziedziczy krycie przodka; kontener kolumn może być pozycjonowany bezwzględnie; elementy pływające dzielą
  się między kolumny, a ich dalszy ciąg stoi u góry następnej kolumny (heurystyka: pozycja x poprzedniego
  fragmentu przesunięta o kolumnę; niesprawdzone dla elementów pływających w prawo i węższych pudełek
  zawierających); równoważenie liczy elementy pływające; zagnieżdżony kontener o podanej wysokości, którego
  treść skończyła się wcześniej, przenosi resztę wysokości do następnej kolumny zewnętrznej (§ 5.1); przy
  `box-decoration-break: clone` każdy fragment rezerwuje dolne obramowanie; pusty wiersz po końcowym `<br>` nie
  zawyża wysokości kolumn; wymuszony podział przed elementem rozpinającym albo za nim dzieli fragmentator
  zewnętrzny; w kolumnach fragment bloku o podanej wysokości zużywa co najmniej wysokość treści, która
  musiała się w nim zmieścić. W źródle tego nie było;
- białe znaki i zawijanie, 2026-11-17 (CSS Text 3 § 4, § 5; testy W3C `css-text/white-space`; praca pomocnika):
  `text/engine_gotext.go`, `html/layout/inline.go`, `html/layout/preferred.go`, `html/boxes/build.go`:
  - cały ciąg białych znaków na końcu wiersza (spacje i tabulatory przy `pre-wrap`, odstępy kategorii Zs
    zawsze, także rozłożony na kilka pudełek i fontów) wisi, a wiersz łamie się dopiero po nim: miejsca
    podziału wyznaczamy na tekście, w którym ciąg jest zamieniony na spacje, i zawijamy z zapasem jego
    szerokości; go-text pozwalał wystawać tylko ostatniemu glifowi, a glif znaku nowego wiersza (w Ahem
    z obrysem) wymuszał złamanie wcześniej. Przy `break-spaces` tabulator nie wisi i po każdym wolno złamać;
  - szerokości wewnętrzne odejmują wiszące odstępy i zwijalną spację z końca tekstu tylko tam, gdzie wiersz
    się kończy; min-content przy `pre-wrap` pomija spacje na końcu każdego wiersza; przy `pre` spacje na końcu
    wiersza nie liczą się, gdy wiersz szuka miejsca obok elementów pływających (jak Chromium);
  - zwijalna spacja przed pustym pudełkiem liniowym znika na końcu wiersza; CR z odwołania znakowego jest
    spacją (źródło zamieniało go na znak nowego wiersza); złamanie segmentu obok spacji zerowej szerokości
    znika, także przez granicę pudełek; zachowany znak nowego wiersza łamie wiersz także przy zerowym
    rozmiarze fontu; znak przed wymuszonym złamaniem dostaje pełny odstęp między literami; dopasowanie
    spacji nierozdzielających i spacji przy `break-spaces` uwzględnia błąd zaokrągleń szerokości glifów;
    tekst rodzica nie dokleja się do tekstu dziecka z `display: contents`. Test własny `white_space_test.go`;
- ogon układu CSS 2, 2026-11-17 (testy W3C `css/CSS2/normal-flow`, `positioning`, `margin-padding-clear`,
  `visudet`, `visuren`, `box-display`, `linebox`, `tables`; praca pomocnika):
  - `html/layout/block.go`: dolny margines pustego dziecka, który dołącza do łańcucha górnego marginesu
    rodzica, przesuwa dotychczasowe dzieci (z elementami pływającymi i pozycjonowanymi) razem z górną
    krawędzią rodzica (CSS 2.1 § 8.3.1); blok wydzielony z pudełka liniowego z `position: relative` przesuwa
    się razem z nim (§ 9.2.1.1, pole `BoxFields.RelativeInlineAncestors` ustawiane w `boxes/build.go`);
    procentowe `top`/`bottom` przy wysokości bloku zawierającego zależnej od treści liczą się jak `auto`;
  - `html/layout/preferred.go`: szerokość tabeli nie mniejsza niż szerokość minimalna treści podpisu (CAPMIN,
    CSS Tables 3 § 3.9.1); kolumny i grupy kolumn wnoszą samą szerokość, bez marginesów i odstępów (CSS 2.1
    § 17.3); procentowe marginesy i odstępy w udziale do rozmiaru wewnętrznego liczą się jako zero, poza
    komórkami tabeli (CSS Sizing 3 § 5.2.1); `shrinkToFit` rozwiązuje procentowe wysokości dzieci pudełka
    o znanej wysokości (`setChildrenDefiniteHeight` w `grid.go` pomija anonimowe bloki);
  - `html/layout/float.go`: `min-width`/`max-width` elementu pływającego o podanej szerokości (§ 10.4);
  - `html/layout/inline.go`: `vertical-align: bottom` wyższe niż wiersz powiększa go w górę, `top` w dół (jak
    Chromium); zagnieżdżone poddrzewa top/bottom przesuwane raz; linia bazowa tabeli inline liczona od
    krawędzi marginesu tabeli;
  - `html/layout/tables.go`: pusty wiersz nie wyznacza linii bazowej; przy szukaniu ostatniej linii bazowej
    blok z `overflow` bez wierszy daje dolną krawędź marginesu (poza elementem siatki i flex, których linię
    bazową syntetyzuje kontener); wiersz bez komórek wyrównanych do linii
    bazowej ma ją na najniższej dolnej krawędzi treści komórek po rozciągnięciu; wiersz bez komórek ma
    podaną wysokość (§ 17.5.3);
  - `html/layout/tables.go` (`cellHasInFlowContent`, `rowHasBaselineCells`): komórka bez treści w przepływie
    nie uczestniczy w wyrównaniu do linii bazowej wiersza, jak w Chromium (ADR 0006); dosłowne brzmienie
    CSS 2.1 § 17.5.3 ją włącza. Test przejęty `TestTableRowHeight1` (`html/layout/tables_test.go`) oczekiwał
    brzmienia dosłownego (odstępy drugiej komórki 5 i 15); poprawiony na 0 i 20. Testy WPT
    `*-applies-to-006`, `min-height-applies-to-014`, `table-vertical-align-baseline-008`;
  - `html/document/stacking.go`: element pływający i blok w przepływie z `overflow` innym niż `visible`
    (bez z-index, opacity, transform) rysowane w całości na warstwie elementów pływających zamiast jako
    kontekst o z-index 0 (przybliżenie CSS 2.1 Dodatek E); `position: fixed`/`sticky` zawsze tworzy kontekst
    nakładania (CSS Position 3);
  - `html/document/draw.go`: przy równej sile zwiniętych obramowań narożnik należy do odcinków komórki
    wyżej i na lewo (w korpusie `17-tables` zmienia 8 pikseli narożników). Test własny `css2_layout_test.go`;
- CSS Text poza `white-space`, 2026-11-17 (testy W3C `css-text/*`, `css/CSS2/text`; praca pomocnika):
  - `text-align`: skrót ustawia `text-align-last: auto` (źródło kopiowało wartość, a `justify` zamieniało
    na `start`); `auto` po `justify` wyrównuje ostatni wiersz do początku (CSS Text 3 § 6.1, § 6.3);
  - justowanie: w wierszu z tabulatorem dotyczy tylko części po ostatnim tabulatorze (źródło takiego wiersza
    nie justowało wcale); miejscami justowania są też separatory słów innych pism, jak w go-text; przebieg
    złożony z samych znaków sterujących dostaje font spacji, nie zastępczy;
  - `text-transform`: `capitalize` ciągnie słowo przez granice elementów i używa wielkiej litery tytułowej
    z odwzorowaniem pełnym; reguły językowe: niderlandzkie IJ, greckie wielkie litery jak ICU „el”
    (`text_transform.go`, `greekUpper`); nowa wartość `math-auto`;
  - nowe właściwości `wrap-inside` (`avoid`), `hyphenate-limit-lines`, `word-space-transform` (U+200B
    i `<wbr>` jako spacja; `auto-phrase` bez działania); znak dzielenia także po miękkim łączniku na końcu
    pudełka tekstu;
  - łamanie wierszy na granicy elementów: litery strony z `word-break: break-all` liczą się jak ideogramy;
    `overflow-wrap: anywhere` pozwala awaryjnie złamać tuż za elementem; przed elementem atomowym i po nim
    wolno łamać zawsze, poza znakami klas GL, WJ i ZWJ (CSS Text 3 § 5.1); `line-break: loose`
    z `break-all` pozwala łamać przed U+2010 i U+2013; U+000B, U+000C, U+0085 widoczne i wymuszają podział;
  - `text-indent`: przy ujemnym wcięciu pozycją tabulatora może być krawędź bloku; wiersz z samym
    elementem pływającym nie zużywa wcięcia;
  - odstępstwo od testu W3C: `word-break-break-all-inline-007.tentative` traci zaliczenie na rzecz
    nietentatywnego `-009` (testy są sprzeczne, zachowanie na granicy CSS Text 3 § 5 nie określa);
    `text-transform-upperlower-016` zostaje niezaliczony, bo wzorzec używa odwzorowania prostego, a norma
    wymaga pełnego. Test własny `text_misc_test.go`;
- parser i selektory, 2026-11-17 (testy W3C `css/CSS2/syntax`, `selectors`):
  - `css/parser/parser.go` (`parseDeclaration`, nowe `IsEOFError`), `html/tree/style.go` (`@import`): koniec
    arkusza wewnątrz łańcucha albo `url(` to tylko błąd parsowania, a token pozostaje ważny (CSS Syntax 3
    § 4.3.5, § 4.3.6); tokenizer dokłada za nim znacznik błędu, który w źródle unieważniał deklarację
    i regułę `@import`;
  - `html/tree/computed_values.go` (`computeAttrFunction`): nazwa atrybutu w `attr()` sprowadzana do małych
    liter dla elementów HTML, jak nazwy ich atrybutów (CSS Values 5 § 7.7); atrybuty MathML i SVG rozróżniają
    wielkość liter. Źródło szukało nazwy dosłownie. Test W3C `content-attr-case-002.xht` (XHTML) traci
    przypadkowe zaliczenie: wymaga parsera XML (pytanie 25);
  - `css/selector/parser.go` (`htmlCaseInsensitiveAttributes`): wartości atrybutów z listy HTML § 4.16.2
    (m.in. `lang`, `type`, `dir`) porównywane bez rozróżniania wielkości liter, flaga `s` to wyłącza
    (Selectors 4 § 6.3); źródło nie znało ani listy, ani flagi `s`. W `html/tree/html5_ph.css` reguły
    `ol[type=a]`, `[type=A]`, `[type=i]`, `[type=I]` dostały flagę `s`, jak w HTML § 15.3.8;
- elementy pływające i clearance, 2026-11-17 (CSS 2.1 § 8.3.1, § 9.5, § 9.5.1, § 9.5.2, § 17.4; testy W3C
  `css/CSS2/floats`, `floats-clear`):
  - `html/layout/block.go` (`blockLevelLayout`, `blockBoxLayout`, `blockContainerLayout`, `inFlowLayout`,
    `leadingChildMargins`, nowe `leadingChildChain`, `marginChainOpen`), `html/layout/float.go`
    (`avoidCollisions`, nowe `forcedClearanceFor`, `avoidsFloats`, `fitsBesideFloats`, `bandAvailable`,
    `relayoutBesideFloats`), `html/layout/layout.go` (`forcedClearance`, `separatedFromFloats`,
    `floatOnlyLine`, `discardBlockFormattingContext`): górny margines rodzica zwija się z marginesami
    dzieci za pustymi dziećmi, a przylegające elementy pływające przesuwają się z nim; clearance przy
    przylegającym elemencie pływającym jest wymuszone; pudełko z własnym kontekstem, które obok się nie
    mieści, oddziela margines; pozycja hipotetyczna liczy się z marginesami potomków; puste pudełko
    z własnym kontekstem nie przepuszcza marginesów, a pudełko z przechodzącymi marginesami ma wysokość 0;
    pudełko z własnym kontekstem i tabela o szerokości auto są układane ponownie w pasie wolnym od
    elementów pływających na całej wysokości, a marginesy auto dzielą ten pas; pudełko opakowujące tabelę
    zakłada kontekst formatowania. Reguła 3 dla elementu szerszego niż blok zawierający; element o zerowej
    szerokości nie zwęża pudełek ani wierszy; element o zerowej wysokości jest umieszczany normalnie
    (źródło dawało mu pozycję (0, 0) strony) i wyklucza tylko pasy obejmujące go w środku;
  - `html/layout/inline.go` (`getNextLinebox`, `splitInlineBox`, `inlineOutOfFlowLayout`): elementy pływające
    z cofniętej części wiersza znikają z listy wykluczeń; ponowny układ dziecka nie dubluje elementów
    pływających; element szerszy niż pusty wiersz stoi na jego wysokości, a treść zaczyna następny wiersz;
    wiersz liczy wolne miejsce i wyrównanie na całej swojej wysokości;
  - `html/layout/preferred.go` (`blockContentWidth`, `inlineLineWidths`): przy max-content wiersze opływające
    wcześniejsze elementy pływające stoją obok nich; zachowany znak nowego wiersza na końcu tekstu łamie
    wiersz, a przy pierwszym wierszu złamanie wewnątrz pudełka kończy liczenie;
  - `html/document/draw.go` (`drawBackground`): kolor tła o zerowym polu malowania nie jest rysowany
    (CSS Backgrounds 3 § 3.7); źródło wstawiało prostokąt zerowej wysokości widoczny jako linia.
    Test własny `floats_clearance_test.go`; przypadek `no-clearance-large-child-margin` w `regression_test.go`
    poprawiony (bez obramowania u rodzica clearance jest wymuszone; dopisany wariant z obramowaniem);
- wyrównanie do linii bazowej w siatce, 2026-11-17 (CSS Grid 1 § 10.6, § 10.7, § 11.5; CSS Align 3 § 9;
  testy W3C `css-grid/alignment/*baseline*`, `grid-container-baseline-synthesized-*`):
  - `html/layout/grid.go` (`gridLayout`, `gridLayoutPass`, `resolveTracksSizes`, `distributeExtraSpace`,
    nowe `gridItemBaseline`, `contentExtent`, `percentHeight`, `clampSize`): grupy first/last baseline
    w wierszach, także dla elementów rozpiętych na kilka wierszy; odsunięcia od wspólnej linii bazowej
    doliczane do wkładu elementu w rozmiar wierszy w drugim przebiegu (wcześniej wiersz powiększano po
    układzie); `align-content: baseline` na elemencie blokowym; element z wysokością procentową w wierszu
    o rozmiarze z treści dostaje wyrównanie zapasowe (cykl z § 10.6); pierwsza i ostatnia linia bazowa
    kontenera z pierwszego i ostatniego niepustego wiersza, syntetyzowane z krawędzi obramowania; pusty
    inline-grid stoi na dolnej krawędzi marginesu. Rozciąganie elementu respektuje `min-`/`max-width`
    i `-height` (CSS Align 3 § 6.1), tabela jako element siatki jest rozciągana do obszaru (§ 6.2);
    wysokość dostaje dopiero wtedy, gdy ułożona do treści jest niższa niż obszar, bo określona wysokość
    tabeli zmienia rozwiązywanie procentowych wysokości komórek;
  - `html/layout/tables.go` (`findBaseline`): kontener siatki zwraca zapisane linie bazowe zamiast schodzić
    do dzieci w kolejności dokumentu; kontener flex bez linii bazowej elementów ma ją syntetyzowaną
    z dolnej krawędzi obramowania pierwszego elementu (CSS Flexbox 1 § 8.5);
  - `html/layout/absolute.go` (`absoluteHeight`, `absoluteBlock`, `absoluteBlockAlignment`): dziecko siatki
    pozycjonowane bezwzględnie z `align-self` innym niż auto/normal/stretch i z podanymi `top` i `bottom`
    ma wysokość fit-content i jest wyrównywane między nimi (CSS Position 3 § 4.1); źródło je rozciągało;
  - `html/boxes/boxes.go` (`GridFirstBaseline`, `GridLastBaseline`), `layout.go` (`gridShims`). Pismo
    pionowe i `justify-self: baseline` nieobsługiwane (ADR 0010). Test własny `grid_baseline_test.go`;
- kolumny w komórce tabeli i pudełku liniowo-blokowym, 2026-11-17 (CSS Multicol 1 § 2; testy W3C
  `css-multicol/table/*`, `intrinsic-size-001`, `-004`, `css-tables/table-cell-overflow-explicit-height-002`):
  - `html/boxes/multicol_wrap.go` (`MulticolWrappers`, wołane z `CreateAnonymousBox` w `build.go`): komórka
    tabeli i `inline-block` z `column-count` lub `column-width` są kontenerami kolumn; ich treść trafia przy
    budowie drzewa do anonimowego bloku z właściwościami kolumn, a pudełko dostaje dla nich wartości
    początkowe. Źródło układało w kolumnach tylko zwykłe bloki;
  - `html/layout/preferred.go` (`blockContentWidth`, `multicolContentWidth`): szerokość wewnętrzna kontenera
    kolumn o szerokości auto to liczba kolumn razy szerokość kolumny (co najmniej `column-width`) plus odstępy;
    elementy rozpinające wchodzą do niej bez mnożenia (tak liczą Blink i Gecko, norma tego nie definiuje);
  - `html/layout/tables.go`, `layout.go` (`cellHeightRemaining`): `max-height` nie ogranicza komórki tabeli
    (CSS 2.2 § 10.7: skutek nieokreślony, przeglądarki go pomijają); komórka o podanej wysokości dzielona
    między strony albo kolumny sięga najwyżej do końca fragmentatora, a resztę wysokości przenosi do dalszego
    ciągu (CSS Fragmentation 3 § 5.1). Test własny `TestMulticolTableCell`;
- kolumny zagnieżdżone, 2026-11-17 (CSS Multicol 1 § 8.1, CSS Fragmentation 3 § 4.4, § 5.1, § 5.2, § 5.4;
  testy W3C `css-multicol/multicol-nested-*`):
  - `html/layout/column.go` (`columnsLayout`): kontener kolumn zagnieżdżony w kolumnie, który nie zaczyna
    się u góry kolumny zewnętrznej, przenosi treść niemieszczącą się w kolejnej kolumnie wewnętrznej do
    następnej kolumny zewnętrznej, a reszta kolumn rzędu zostaje pusta; w źródle każda kolumna po pierwszej
    była układana jako pusta, więc np. blok z `break-inside: avoid` był dzielony. Dalszy ciąg kontenera nie
    ma górnego, a fragment przerwany dolnego marginesu, obramowania i dopełnienia (`box-decoration-break:
    slice`). `min-height` dotyczy sumy fragmentów (`layoutContext.minHeightRemaining`); w źródle każdy
    fragment miał całe `min-height`;
  - `layout.go` (`layoutContext.columnBreak`, `breakAtFragmentTop`), `block.go` (`blockLevelLayout`,
    `inFlowLayout`), `column.go`: marginesy przylegające do zwykłego podziału kolumny są obcinane jak na
    początku strony, także dla pierwszego pudełka kolumny i przy równoważeniu; po `break-before: column`
    margines zostaje. W źródle obcinanie działało tylko na stronach po pierwszej, więc w trybie ciągłym
    i na pierwszej stronie kolejne kolumny zaczynały się od marginesu. Dotyczy też kolumn niezagnieżdżonych;
  - `float.go` (`floatLayout`): element pływający z `columns` jest układany przez `columnsLayout`
    (CSS Multicol 1 § 2); w źródle jego treść była układana bez kolumn;
- `text-wrap-style` i `text-wrap: balance`, 2026-11-17 (CSS Text 4 § 5.2, § 5.4; testy W3C
  `css-text/white-space/text-wrap-balance-*`):
  - `css/properties` (nowa dziedziczona `PTextWrapStyle`: `auto | balance | stable | pretty`),
    `css/validation` (`textWrapStyle`, `_expandTextWrap`): skrót `text-wrap` przyjmuje
    `<'text-wrap-mode'> || <'text-wrap-style'>` w dowolnej kolejności; źródło nie znało żadnej z nich;
  - `html/layout/inline.go` (`lineBoxeIterator.balance`, `trialLines`, `getNextLinebox` z parametrem
    `shrink`), `block.go` (`lineBoxLayout` przekazuje `maxLines`): na początku każdego odcinka między
    wymuszonymi złamaniami treść jest układana próbnie; gdy daje 2–6 wierszy (granica Chromium), połowieniem
    wybieramy największe jednakowe zwężenie dostępnej szerokości, przy którym liczba wierszy się nie
    zmienia; `text-align` liczy się od pełnej szerokości; przy `overflow-wrap` innym niż `normal`
    zwężenie nie schodzi poniżej szerokości min-content; skutki uboczne układu próbnego (listy wykluczeń
    elementów pływających, `brokenOutOfFlow`, elementy biegnące, pudełka pozycjonowane) są przywracane.
    Bloku obcinanego przez `line-clamp` nie wyrównujemy (wielokropek bloku powstaje dopiero przy
    rysowaniu). `stable` i `pretty` łamią jak `auto`. W źródle wiersze łamano zawsze zachłannie;
- `@font-feature-values` i funkcje `font-variant-alternates`, 2026-11-17 (CSS Fonts 4 § 6.8 i § 6.9; testy
  W3C `css-fonts/font-variant-alternates-*`):
  - `css/validation/validation.go` (`fontVariantAlternates`) i nowy `css/validation/font_feature_values.go`
    (`fontVariantAlternatesValue`, `ParseFontVariantAlternates`, `ParseFontFeatureValues`,
    `FontFeatureValues.Add`, `Lookup`): źródło przyjmowało w `font-variant-alternates` tylko `normal`
    i `historical-forms`, a regułę `@font-feature-values` pomijało jako nieznaną. Teraz właściwość
    przyjmuje `stylistic()`, `styleset()`, `character-variant()`, `swash()`, `ornaments()` i `annotation()`
    (postać kanoniczna jako `pr.String`), a reguła daje tablicę rodzina → blok → nazwa → liczby; błędne
    deklaracje i nieznane bloki są pomijane;
  - `html/tree/style.go` (`preprocessLayer`, `fontFeatureValues`, `newStyleFor`), `tree.go`
    (`match.featureValues`): reguły zbierane z arkuszy razem z warstwą kaskady; przy powtórzonej nazwie
    wygrywa późniejsza warstwa, w tej samej warstwie późniejsza reguła, reguły poza warstwami po wszystkich
    warstwach (CSS Cascade 5 § 6.4);
  - `text/fonts.go` (`SetFontFeatureValues`, `fontFeatureValues`), `engine_gotext.go` (`featureValues`),
    `style.go` (`newTextStyle`, `alternatesFeatures`, `getFontFeatures`), `text.go`: stylistic → `salt N`,
    styleset → `ssNN`, character-variant → `cvNN`, swash → `swsh` i `cswh`, ornaments → `ornm`, annotation →
    `nalt`, według nazw dla rodzin z listy `font-family` (pierwsza rodzina definiująca nazwę wygrywa).
    Ograniczenie: nazwy szukane według listy rodzin elementu, nie fontu faktycznie użytego dla fragmentu;
- `text-justify`, `text-indent`, `word-spacing`, `text-wrap-mode`, 2026-11-17 (CSS Text 3 i 4; testy W3C
  `css-text`):
  - `text-justify` (§ 6.4): nowa dziedziczona właściwość `PTextJustify` (`css/properties`,
    `css/validation` `textJustify`); `html/layout/inline.go` (`countExpandableSpaces`): tekst
    z `text-justify: none` nie ma miejsc justowania, liczy się styl pudełka tekstu; `inter-word`
    i `inter-character` justują jak `auto`. Źródło właściwości nie znało;
  - `text-indent: <length-percentage> && hanging? && each-line?` (§ 7.1): nowy typ `pr.TextIndent`
    (`css/properties/types.go`), `css/validation` (`textIndent`), `html/tree/computed_values.go`
    (`textIndent`), `html/boxes` (`LineBox.AfterBlock`, `markAfterBlock` w `InlineInBlock`
    i `BlockInInline`), `html/layout/inline.go` (`lineBoxeIterator`, `iterLineBoxes`, `getNextLinebox`
    zwraca `forcedBreak`), `html/layout/preferred.go` (`inlineLineWidths`): wcięcie dostaje pierwszy
    sformatowany wiersz kontenera blokowego, z `each-line` także każdy wiersz po wymuszonym złamaniu,
    `hanging` odwraca wybór. Źródło przyjmowało samą długość (deklaracja ze słowem przepadała) i wcinało
    pierwszy wiersz każdego pudełka wiersza, także tekstu po pudełku blokowym. Ograniczenia: wiersz
    wznowiony po podziale strony liczy się jak zwykły; szerokości wewnętrzne nie odróżniają złamań
    wymuszonych od miękkich;
  - `word-spacing: <percentage>` (CSS Text 4 § 10.1): `css/validation` (`wordSpacing`, `spacingValue`),
    `text/style.go` (`WordSpacingPercent`, `WordSpacingCalc`), `text/engine_gotext.go` (`wordSpacing`):
    procent liczy się od szerokości znaku U+0020 w foncie elementu, także w `calc()`;
  - `text-wrap-mode` i `text-wrap: wrap | nowrap` (CSS Text 4 § 3, § 5): nowa dziedziczona właściwość
    `PTextWrapMode`; `white-space` stał się skrótem ustawiającym `white-space` i `text-wrap-mode`
    (`css/validation/expanders.go` `_expandWhiteSpace`, `_expandTextWrap`); `html/tree/style.go`
    (`effectiveWhiteSpace`) składa obie w jedno słowo przy obliczaniu stylu, więc układ czyta jedną
    wartość. Pozostałe wartości `text-wrap` (`balance`, `pretty`, `stable`) są odrzucane jak dotąd.
    Ograniczenie: `pre-line` z `nowrap` nadal się zawija;
- `font-size-adjust`, 2026-11-17 (CSS Fonts 5 § 2.7; testy W3C `css-fonts/font-size-adjust-*`). Źródło
  właściwości nie znało: deklaracja była odrzucana, a tekst składany zawsze w obliczonym rozmiarze fontu.
  - `css/properties` (`PFontSizeAdjust`, dziedziczona, wartość `pr.SBoolFloat`: String = metryka,
    Bool = from-font, Float = liczba), `css/validation/validation.go` (`fontSizeAdjust`): składnia
    `none | [ ex-height | cap-height | ch-width | ic-width | ic-height ]? [ from-font | <number> ]`,
    liczba także przez `calc()`;
  - `text/style.go` (`TextStyle.SizeAdjust`), `text/fonts.go` i `text/engine_gotext.go`
    (`sizeAdjustFactor`): mnożnik = podana liczba podzielona przez stosunek metryki pierwszego dostępnego
    fontu do firetu (wysokość x i wersalików z tablicy OS/2, `ch` z posunięcia „0”, `ic` z U+6C34 albo 1em);
  - `text/text.go` (`adjustedTextStyle`, `strutLayout`, `CharacterRatio`): tekst jest składany, mierzony
    i rysowany w rozmiarze używanym; `line-height` podany liczbą mnoży nadal rozmiar obliczony; jednostki
    `ex` i `ch` idą za rozmiarem używanym, `em` zostaje przy obliczonym;
  - ograniczenia: fonty zastępcze dostają mnożnik fontu podstawowego (norma każe liczyć go dla każdego
    fontu osobno), więc `from-font` nie zmienia rozmiaru; skrót `font` nie przywraca `none`; brak
    współpracy z deskryptorami metryk w `@font-face`. Test: `font_size_adjust_test.go`;
- CSS 2.1, ogon grup, 2026-11-17 (testy W3C `css/CSS2`):
  - `html/layout/pages.go` (`makePage`): element główny z `position: absolute` albo `fixed` jest układany
    jak pudełko pozycjonowane względem początkowego bloku zawierającego (§ 10.1 pkt 4); źródło ignorowało
    `position` na elemencie głównym;
  - `html/layout/background.go` (`layoutBackgrounds`, `findBodyBox`): tło na płótno przechodzi także
    z pudełka tabeli, gdy element główny ma `display: table`, oraz z `body` leżącego głębiej (pudełka
    anonimowe tabeli, `body` pozycjonowane) (§ 14.2);
  - `css/properties/datas.go` (`TableWrapperBoxProperties`): `overflow` zostaje na pudełku tabeli, a nie
    na pudełku opakowującym (errata s.11.1.1b: podpis leży poza przycięciem); `html/boxes/boxes.go`
    (`ClipsOverflow`), `html/document/stacking.go`, `draw.go`: `overflow` nie przycina na wierszach,
    grupach wierszy, kolumnach i grupach kolumn (§ 11.1.1);
  - `html/document/draw.go` (`drawStackingContext`): tabela, która sama tworzy kontekst nakładania,
    rysuje swoje tła (własne, kolumn, wierszy, komórek) i zwinięte obramowania; w źródle tabela była
    rysowana tylko jako dziecko cudzego kontekstu;
  - `css/validation/utils.go`: łańcuch znaków jako styl w `counter()` i `counters()` jest odrzucany
    (CSS Lists 3 § 4.7);
  - `html/boxes/html.go` (`handleIframe`), `images/crop.go` (`Blank`): `<iframe>` jest elementem
    zastępowanym o rozmiarze domyślnym 300 na 150 i bez proporcji własnych (§ 10.3.2); zawartości nie
    rysujemy. W źródle był pustym pudełkiem liniowym;
  - `html/layout/block.go` (`blockLevelLayout`, `leadingChildMargins`): pozycja hipotetyczna przy `clear`
    uwzględnia marginesy pierwszych dzieci zwijające się przez górną krawędź pudełka (§ 9.5.2, § 8.3.1);
    gdy clearance powstaje, wspólna górna krawędź staje równo z dołem elementu pływającego. Tylko dla
    marginesów dodatnich; źródło dodawało margines dziecka pod clearance;
  - `html/layout/replaced.go` (`blockReplacedBoxLayout`): element zastępowany będący elementem flex albo
    grid nie jest przestawiany przez omijanie elementów pływających; w źródle wracał do lewej krawędzi
    kontenera i dwa takie elementy obok siebie nakładały się;
  - `html/layout/inline.go` (`inlineBlockBaseline`): `inline-block` z `overflow` innym niż `visible`
    bierze linię bazową z ostatniego wiersza, gdy ten leży w pudełku, inaczej z dolnej krawędzi
    marginesu (CSS 2.1 § 10.8.1 każe zawsze brać krawędź marginesu; przyjęte zachowanie Chromium,
    zgodne z testami `inline-block-baseline-003` i `vertical-align-baseline-005a`); `textarea` zostaje
    przy krawędzi marginesu, jak w Chromium;
- kolumny w trybie ciągłym, 2026-11-17 (ADR 0017 pkt 5), `html/layout/column.go` (`unpaged`): kontener
  poza innymi kolumnami tworzy nadmiarowe kolumny w osi wiersza także przy wysokości auto, zostawia
  w kolumnie treść, która się w niej nie mieści, i równoważy kolumny przy `column-fill: auto` bez
  ograniczonej wysokości (CSS Multicol 1 § 7.1, § 8.2);
- `border-image`, 2026-11-17 (CSS Backgrounds 3 § 6; testy W3C `css-backgrounds`),
  `html/document/draw_border.go` (`drawBorderImage`) i nowy `images/crop.go` (`Crop`):
  - powiększany wycinek obrazu rastrowego jest rysowany jako osobny, wycięty obraz; źródło rysowało
    cały obraz przycięty maską, więc przy wygładzaniu na krawędzi wycinka pojawiała się smuga koloru
    z sąsiedniego obszaru obrazu (wycinki są skalowane niezależnie, § 6.2);
  - przy `repeat` kafelki są wyśrodkowane w swoim obszarze (§ 6.4); źródło zaczynało od lewej i górnej
    krawędzi. Odstęp przy `space` jest liczony w tej samej skali co kafelki;
    test źródłowy `TestBorderImageRepeat` (`goweasyprint/pdf/draw_box_test.go`) oczekiwał kafelków od
    krawędzi i został poprawiony według normy (szerokość 6 px zamiast 7, żeby środek wypadał na całych
    pikselach);
  - szerokości obrazu obramowania, które po przeciwnych stronach przekraczają obszar, są zmniejszane
    proporcjonalnie (§ 6.3);
  - środek z `fill`: skala pionowa brana z dolnej krawędzi, gdy górna ma zerową (w źródle pomyłka nazw
    zmiennych);
- siatka, 2026-11-17 (CSS Grid 1; testy W3C `css-grid`), `html/layout/grid.go`:
  - `fitContentItemWidth`: element, który nie jest rozciągany (`justify-self` inne niż `stretch`,
    marginesy `auto`), dostaje szerokość treści; źródło wpisywało szerokość zewnętrzną, więc był szerszy
    o sumę marginesów, obramowań i odstępów;
  - szerokość i wysokość `min-content`, `max-content`, `fit-content` nie są traktowane jak `auto`: taki
    element nie jest rozciągany (CSS Align 3 § 6.1);
  - `gridLayout`: drugi przebieg układu (powiększone wiersze po wyrównaniu do linii bazowej) zaczyna od
    pierwotnej wysokości kontenera; pierwszy wpisywał wysokość do pudełka i kontener nie rósł razem
    z wierszem;
  - `min-height` kontenera o wysokości auto: gdy treść jest niższa i jest ścieżka `auto`, wiersze są
    rozwiązywane ponownie w tej wysokości i ścieżki `auto` się rozciągają (§ 11.1, § 11.8);
  - `resolveTracksSizes` (`frContent`), `gridLayoutPass`: przy nieokreślonej wysokości ułamek `fr`
    wierszy wynika także z wysokości treści elementów (§ 11.7; w źródle TODO), a gdy suma współczynników
    jest mniejsza od 1, wiersze są rozwiązywane drugi raz w wysokości z pierwszego przebiegu (§ 11.1);
  - `resolveTracksSizes` (krok 1.2.2): przy pomiarze wkładu elementu w wiersz wysokość obszaru siatki
    jest auto, więc wysokości procentowe liczą się jak auto (CSS Grid 1 § 6.5); źródło podstawiało
    największą dotąd zmierzoną wysokość, na początku zero, i obraz z `height: 100%` miał wysokość zero;
  - `collapseEmptyAutoFit`, `gridLayoutPass` (`edges`): linie podane przez element pozycjonowany są
    przeliczane po zwinięciu pustych ścieżek `auto-fit` (CSS Grid 2 § 7.2.3.2); w źródle element
    wskazujący linię za zwiniętą ścieżką dostawał cały kontener jako pudełko zawierające;
  - wysokość siatki wyliczona z wierszy jest ograniczana przez `max-height` i `min-height` kontenera;
  - `definiteTrackSize`: przy liczeniu powtórzeń `repeat(auto-fill | auto-fit, …)` maksimum ścieżki nie
    schodzi poniżej jej minimum, gdy oba są określone (§ 7.2.3.2);
  - niejawne nazwy linii z `grid-template-areas` (§ 7.3.2): każdy obszar daje nazwy `<obszar>-start`
    i `<obszar>-end` liniom na swoich krawędziach, także gdy taka nazwa jest nadana jawnie innej linii;
    źródło wtedy ich nie dodawało, a linie końcowe liczyło od końca szablonu;
- `html/layout/block.go` (`avoidFloats`), 2026-11-17: kontener flex albo grid o podanej szerokości
  w przepływie blokowym nie nachodzi na elementy pływające (własny kontekst formatowania, CSS 2.1 § 9.5);
  źródło robiło to tylko dla bloków i tabel;
- `html/layout/inline.go` (`splitInlineLevel`), 2026-11-17: `inline-flex` o szerokości auto dopasowuje
  się do treści jak `inline-block` (CSS Flexbox 1 § 3); w źródle zajmował całą szerokość wiersza;
- flex, 2026-11-17 (CSS Flexbox 1; testy W3C `css-flexbox`):
  - `html/layout/preferred.go` (`flexMinContentWidth`, `flexMaxContentWidth`, `flexMainGaps`,
    `inflexibleBasis`): kontener o szerokości podanej długością ma ją także jako szerokość wewnętrzną
    (źródło liczyło ją zawsze z elementów, więc element pływający albo `inline-block` z takim kontenerem
    rozciągał się na całą szerokość); do szerokości wewnętrznej kontenera wierszowego liczą się odstępy
    `column-gap`; element z `flex: 0 0 <długość>` wnosi swoje `flex-basis` (§ 9.9.1);
  - `html/layout/flex.go` (krok 12 i 13, `marginIsAuto`): o tym, czy margines jest `auto`, rozstrzyga
    styl; w kierunku kolumny wcześniejszy układ elementu zerował marginesy i wolne miejsce szło do
    `justify-content` (§ 8.1). Marginesy `auto` dostają tylko dodatnie wolne miejsce (§ 9.5 krok 12.1;
    źródło dawało im wartość ujemną i elementy nachodziły na siebie). Element z marginesem `auto` w osi
    poprzecznej zaczyna się na początku swojej linii, a `align-content` przesuwa każdy element od jego
    własnej pozycji (źródło brało pozycję ostatniego elementu linii dla wszystkich);
  - `html/layout/flex.go` (automatyczne minimum, § 4.5): dla elementu zastępowanego z proporcjami
    rozmiar przeniesiony liczy się z podanego rozmiaru poprzecznego (np. `height: 30px`), a bez niego
    rozmiar treści ograniczają `min-height`/`max-height` (w kolumnie `min-width`/`max-width`)
    przeliczone proporcją; źródło brało zawsze rozmiar własny obrazu;
  - linia bazowa kontenera (§ 8.5, CSS Align 3 § 9.1), `html/layout/flex.go`, `html/layout/tables.go`
    (`findBaseline`): linia bazowa kontenera wierszowego uwzględnia przesunięcie pierwszej linii przez
    `align-content`; przy `wrap-reverse` kontener `inline-flex` bierze ją z linii leżącej najwyżej
    (źródło: z pierwszej linii flex, która po odwróceniu jest na dole); blokowy kontener flex w wierszu
    ma własną linię bazową, z której korzysta kontener nadrzędny, komórka albo `inline-block` (źródło
    szukało pierwszego wiersza tekstu w głębi); w kolumnie z `wrap-reverse` elementy wyrównane do linii
    bazowej są odbijane jako grupa i zachowują wspólną krawędź;
  - `html/layout/flex.go`, `html/layout/grid.go` (koniec `flexLayout` i `gridLayoutPass`): element flex
    i element siatki z `position: relative` jest przesuwany o `top`, `right`, `bottom`, `left` (CSS 2.1
    § 9.4.3); w źródle przesunięcie dostawały tylko dzieci bloków;
  - `html/layout/block.go` (`blockLevelLayout`): `clear` nie działa na elementach flex i grid;
    (`narrowedBesideFloats`, `boxes.NarrowedBesideFloats`): kontener flex o szerokości auto obok
    elementów pływających dostaje szerokość wolnego pasa i jest układany ponownie (CSS 2.1 § 9.5);
  - `html/layout/flex.go` (krok 15): rozmiar poprzeczny wyliczony z linii jest ograniczany przez min-
    i max- kontenera (w źródle TODO); odstęp między liniami nie jest ujemny przy braku linii;
  - `html/layout/preferred.go` (`inflexibleBasis`): do szerokości wewnętrznej kontenera wierszowego
    wchodzi `flex-basis` każdego elementu, który nie może się kurczyć (także gdy może rosnąć); przy
    `box-sizing: border-box` `flex-basis`, `min-width` i `max-width` opisują pudełko obramowania;
  - `html/layout/flex.go` (krok 3.E): marginesy, obramowania i odstępy elementu przy rozmiarze bazowym
    z treści liczone na kopii bez `max-width` (w źródle wartość ujemna, gdy treść była szersza niż
    `max-width`, i element nie brał udziału w kurczeniu); test źródłowy `TestFlexImageMinWidth`
    (`html/layout/flex_test.go`) oczekiwał 10 px, bo źródło doliczało `min-width` do marginesów przy
    liczeniu wolnego miejsca; poprawiony według § 9.7 na 11,5 px;
  - `html/layout/flex.go` (krok 12): `justify-content: start` i `end` nie zamieniają się przy kierunku
    odwróconym (CSS Align 3 § 4.1); słowo `safe` przy przepełnieniu daje wyrównanie do początku, także
    w `align-content` przy `wrap-reverse` (§ 4.4); źródło `safe` ignorowało;
  - tabela jako element flex: `html/layout/tables.go` (`autoTableLayout`, pole `gridMinWidth`
    w `tableContentWidths`): dolną granicą szerokości tabeli narzuconej przez układ flex jest szerokość
    minimalna treści i `min-width`, a nie podana szerokość (CSS Tables 3 § 3.9.1); `html/layout/block.go`:
    wysokość z `flex-basis` podanego długością zastępuje podaną wysokość tabeli (tabela rośnie od treści
    do tej wysokości); `html/layout/flex.go` (`tableWithAutoSize`): minimum automatyczne tabeli liczone
    z treści, bez jej `width` i `height`. W źródle podana szerokość i wysokość tabeli wygrywały z układem flex;
  - `html/layout/flex.go` (`itemRatio`, `ratioBaseSize`, `stretchedRatioContribution`) i `preferred.go`:
    element z `aspect-ratio` (nie tylko zastępowany) rozciągnięty w jednowierszowym kontenerze o podanej
    wysokości ma szerokość z proporcji, także w szerokości wewnętrznej kontenera; przy `box-sizing:
    border-box` proporcja dotyczy pudełka obramowania (CSS Sizing 4 § 5.1); element bez wiersza tekstu
    nie daje linii bazowej, więc kontener `inline-flex` syntetyzuje ją z krawędzi (§ 8.5; źródło brało zero);
  - `html/layout/block.go` (`blockContainerLayout`): dolny margines ostatniego dziecka zostaje w elemencie
    flex i siatki (własny kontekst formatowania, CSS Flexbox 1 § 3, CSS Grid 1 § 6.1); źródło zachowywało
    tylko górny;
  - `html/layout/preferred.go` (`flexibleItemContentWidth` w `flexMinContentWidth`): wkład min-content
    elementu flex, który może rosnąć albo się kurczyć, to większa z wartości: rozmiar min-content i podana
    szerokość, ograniczona przez `max-width` (CSS Flexbox 1 § 9.9.1); źródło brało samą podaną szerokość;
  - `html/layout/flex.go` (sortowanie według `order`): dzieci pozycjonowane bezwzględnie nie są elementami
    flex, więc `order` nie zmienia ich kolejności (CSS Flexbox 1 § 4.1, § 5.4); w źródle zmieniał kolejność
    ich rysowania;
  - `css/validation/validation.go` (`getKeywordT`): słowa `flow-start` i `flow-end` w właściwościach
    wyrównania działają jak `flex-start` i `flex-end` (CSS Align 3 § 4.3); źródło ich nie znało;
  - `css/validation/calc.go` (`calcValue`): stałe `infinity`, `-infinity`, `pi`, `e` w `calc()` (CSS Values 4
    § 10.7.1); `validation.go` (`_flexGrowShrink`) i `expanders.go` (`_expandFlex`): `flex-grow`
    i `flex-shrink` przyjmują `calc()` o wyniku liczbowym, także w skrócie `flex`; nieskończoność przycinana
    do największej skończonej wartości (§ 10.9). Źródło `calc()` w tych właściwościach odrzucało;
  - `html/layout/flex.go` (krok 2, krok 3.E): procentowy `row-gap`/`column-gap`, także w `calc()`, liczony
    w kontenerze o szerokości auto od szerokości dostępnej (źródło: od zera); marginesy, obramowania i odstępy
    elementu przy rozmiarze bazowym z treści brane z wartości rozwiązanych (źródło liczyło je przez szerokość
    wewnętrzną, w której marginesy procentowe są zerem, więc nie zmniejszały wolnego miejsca);
  - `html/boxes/build.go` (`flexChildren`, `gridChildren`): tekst złożony wyłącznie z białych znaków dokumentu
    (spacja, tabulator, znaki nowego wiersza) nie tworzy elementu flex ani siatki, niezależnie od `white-space`
    (CSS Flexbox 1 § 4, CSS Grid 1 § 6); źródło sprawdzało tylko spacje;
  - `html/boxes/boxes.go` (`EstablishesFormattingContext`): element flex i element siatki zakładają
    niezależny kontekst formatowania (CSS Flexbox 1 § 4, CSS Grid 1 § 6.1); w źródle elementy pływające
    w środku dzieliły listę wykluczeń z otoczeniem, nachodziły na siebie i nie liczyły się do wysokości;
  - `html/layout/flex.go` (koniec `flexLayout`), `absolute.go` (`AbsolutePlaceholder.Translate`): pozycja
    statyczna dziecka pozycjonowanego bezwzględnie w kontenerze flex to miejsce jedynego elementu flex jego
    rozmiaru, wyrównanego według `justify-content` i `align-self` (CSS Flexbox 1 § 4.1), przez
    `BoxFields.StaticAlign` jak w siatce; prostokąt przesuwa się razem z pudełkiem (kontener pływający);
    przy `safe` przepełnienie sprawdzamy wobec pudełka zawierającego, jak Chromium (zagadnienie CSSWG 11934).
    Źródło stawiało dziecko w lewym górnym rogu treści kontenera;
  - `html/layout/flex.go` (krok 3): `min-height` i `max-height` jako `min-content`, `max-content`,
    `fit-content` w kontenerze kolumnowym to wysokość treści elementu (CSS Sizing 3 § 2.1); źródło
    traktowało je jak auto;
  - `html/layout/flex.go` (§ 9.7 krok 4b): przy sumie współczynników mniejszej od 1 rozdzielany jest
    ten ułamek początkowego wolnego miejsca (źródło mnożyło je w każdym obrocie pętli i porównywało
    rzędy wielkości, tylko dla wartości dodatnich); `css/validation` (`_flexGrowShrink`): ujemne
    `flex-grow` i `flex-shrink` są odrzucane (§ 7.2);
- tryb ciągły, 2026-11-16 (ADR 0017): `html/tree/tree.go` (`Behavior.Continuous`),
  `html/layout/pages.go` (`makePage`: dół strony odsunięty na czas układu, potem strona rośnie do dołu
  treści), `html/layout/layout.go` (`contentBottomEdge`, `continuousPageHeight`),
  `html/layout/block.go` (`forcePageBreak` i zmiana strony nazwanej nie łamią strony);
- `background-clip: text`, 2026-11-16 (CSS Backgrounds 4 § 3.6): `css/validation/validation.go`
  (`backgroundClip`), `html/layout/background.go` i `html/boxes/boxes.go` (`Background.TextClip`),
  `html/document/draw.go` (`setTextMask`): tło elementu jest malowane przez maskę przezroczystości
  zbudowaną z jego tekstu (biały tekst na czarnym tle grupy, maska z jasności). Wartość na którejkolwiek
  warstwie przycina całe tło; tło elementu głównego nie jest przycinane. Źródło wartości nie znało
  i odrzucało całą deklarację;
- białe znaki, 2026-11-16 (CSS Text 3; testy W3C `css-text/white-space`):
  - `html/boxes/build.go` (`visibleControls`): znaki sterujące kategorii Cc inne niż tabulator i znak
    nowego wiersza są rysowane jako U+FFFD (§ 2.1: mają być widoczne); w źródle znikały;
  - `text/engine_gotext.go` (`WrapWordBreak`): przy `pre-wrap` spacje i tabulatory, po których wiersz
    został zawinięty, wiszą i nie liczą się do szerokości wiersza (§ 4.1.3); w źródle przesuwały
    wyrównanie do prawej i do środka. Tylko pismo od lewej do prawej;
  - `white-space: break-spaces` jako osobna wartość (`css/properties/keywords_src.go` `BreakSpaces`,
    `css/validation`, `text/style.go` `breakSpaces`, `text/engine_gotext.go`): spacje zajmują miejsce,
    wiersz łamie się po każdej z nich, a przed pierwszą tylko przy `overflow-wrap: anywhere` albo
    `break-word`. Wcześniej (od 2026-11-14) wartość była przybliżana przez `pre-wrap`;
  - `css/properties/cmd/main.go`: generator formatuje wynik programem `gofmt` zamiast `goimports`
    (którego może nie być) i sam dopisuje import; wcześniej przerywał pracę przed zapisaniem tablicy
    słów kluczowych;
- awarie znalezione w testach W3C `css-text`, 2026-11-16:
  - `html/tree/style.go` (`ComputedStyle.Get`, `inProgress`): wartość, której obliczenie pyta o nią samą
    (`tab-size: 0.4ch`: jednostka `ch` wymaga stylu tekstu, a ten czyta `tab-size`), dostaje na ten czas
    wartość początkową; w źródle nieskończona rekurencja i przepełnienie stosu;
  - `text/engine_gotext.go` (`ApplyJustification`): tekst przekazywany do `shaping.AddSpacing` jest
    dopełniany, gdy numer skupienia glifu wychodzi poza tekst wiersza; w źródle panika `index out of range`;
  - `html/layout/inline.go` (`splitTextBox`): znaki między wierszami spoza listy znanych podziałów
    (U+000B, U+000C) są traktowane jak zachowany podział wiersza; w źródle `panic`;
- PDF/A-2b, 2026-11-16 (ADR 0016; walidacja veraPDF 1.30):
  - `internal/pdflib/model/model.go`: katalog zapisuje strumień metadanych XMP (`Catalog.Metadata`)
    i deklaracje zamierzonego wyjścia z profilem ICC (`Catalog.OutputIntents`); `write.go`: identyfikator
    pliku wyliczany ze skrótu zapisanych bajtów (`Trailer.ContentID`), wcześniej `/ID` było zapisywane
    tylko przy szyfrowaniu;
  - `internal/goweasyprint/pdf/text.go`: fonty CID typu 2 mają `CIDToGIDMap /Identity` (ISO 32000-1
    § 9.7.4, tablica 117; źródło wpis pomijało);
  - `internal/goweasyprint/pdf/text_font_subset.go` (`writePost`): tabela `post` podzbioru w wersji 3.0,
    bez nazw glifów; lista nazw z wersji 2.0 po odrzuceniu glifów miała inną liczbę wpisów niż `maxp`,
    a nazwy przesuwały się względem numerów glifów. Usunięte `subsetPost20` i `writePostNames20`,
    `TestSubsetPost20` przepisany;
  - `internal/goweasyprint/pdf/page.go`: adnotacje linków i załączników mają flagę drukowania
    (ISO 19005-2 § 6.3.2); `pdf.go`: `DisableImageInterpolation` dla trybu PDF/A;
- wydajność, 2026-11-16 (pomiar w `docs/benchmark.md` § 9):
  - `text/engine_gotext.go` (`loadOneFont`, `ReuseLoadedFaces`): reguła `@font-face` przetwarzana drugi
    raz w tej samej konfiguracji fontów nie pobiera i nie parsuje fontu ponownie; włączone tylko
    w `gopdfgo.Render`, bo testy przejęte dzielą jedną konfigurację między dokumenty i zależą od
    kolejności dodawania twarzy;
  - `html/layout/layout.go` (`MediaPageArea`, `preparedContext`), `html/tree/tree.go` (`HTML.LayoutCache`):
    kontekst układu z policzonymi stylami zbudowany w przedbiegu dla `@media` (ADR 0012) jest używany
    przez właściwy układ, gdy arkusze nie są czytane ponownie; wcześniej style były liczone dwa razy;
- `contain` (CSS Containment 1), 2026-11-16: nowa właściwość (`css/properties`, `css/validation/validation.go`
  `contain`, `html/boxes/boxes.go` `HasContainment`). Obsługujemy to, co wpływa na układ: `size` czyni
  pudełko niepodzielnym (między strony jest krojone tylko wtedy, gdy nie mieści się na żadnej, jak inne
  wysokie bloki) i liczy wysokość auto jak dla pudełka bez treści; `layout` i `paint` zakładają
  niezależny kontekst formatowania. Bez przycinania treści przy `paint`, bez `contain-intrinsic-size`,
  szerokość wewnętrzna nie jest zerowana. Źródło właściwości nie znało. Pliki generowane
  (`properties_gen.go`, `accessors_gen.go`) odświeżone generatorem `css/properties/cmd`;
- `text/quotes.go` (`GetLangQuotes`), 2026-11-16: znacznik języka dopasowywany według BCP 47 (bez
  rozróżniania wielkości liter, łącznik równoważny podkreśleniu, odcinanie podznaczników od końca).
  Źródło po nieudanym dokładnym trafieniu przeglądało mapę w losowej kolejności i brało pierwszy klucz
  będący przedrostkiem, więc `fr-CH` dawało raz cudzysłowy francuskie, raz szwajcarskie (niedeterminizm);
- `html/boxes/build.go` (`quoteLanguage`), 2026-11-16, CSS Content 3 § 4.1: przy `quotes: auto` znaki
  cudzysłowu dobiera język rodzica elementu; źródło brało język samego elementu;
- `html/layout/flex.go`, 2026-11-15: kontener `inline-flex` bez elementu wyznaczającego linię bazową ma ją
  syntetyzowaną z dolnej krawędzi marginesu (CSS Align 3 § 9.1); źródło przyjmowało górę pudełka
  (testy WPT `flexbox-baseline-empty-001a`, `-001b`);
- tabele, runda 2026-11-15 (druga):
  - `html/document/draw.go` (`drawStackingContext`, `drawTable`): obramowania zwinięte tabeli są rysowane po
    tłach i obramowaniach pudełek blokowych leżących w jej komórkach (także tabel zagnieżdżonych), ale przed
    pudełkami następującymi po tabeli (CSS Tables 3 § 5.3.3); źródło rysowało je razem z tłem tabeli, więc
    blok albo tabela w komórce zasłaniały obramowanie. Tabela z `visibility: hidden` nie rysuje obramowań
    zwiniętych;
  - `html/layout/tables.go`: komórka o wysokości podanej długością jest podstawą wysokości procentowych
    swoich dzieci (CSS Tables 3 § 3.10.3); w źródle procenty w komórce liczyły się zawsze jak `auto`;
- wysokości procentowe i `<canvas>`, 2026-11-15:
  - `html/boxes/html.go` (`handleCanvas`): `<canvas>` jest przezroczystym elementem zastępowanym o rozmiarze
    z atrybutów `width` i `height`, domyślnie 300×150 (HTML § 4.12.5); w źródle był pustym pudełkiem liniowym;
  - `html/boxes/boxes.go` (`ContainingBlock`, `AnonymousBaseHeight`), `html/layout/block.go`: anonimowe pudełko
    blokowe jest pomijane przy rozwiązywaniu wysokości procentowych (CSS 2 § 9.2.1.1); w źródle obraz
    z `height: 100%` stojący obok bloku liczył procent od anonimowego bloku, czyli jak `auto`;
  - `html/layout/flex.go`, `html/boxes/boxes.go`: wysokość elementu flex wyznaczona przez `flex-basis`
    w kontenerze o nieokreślonej wysokości nie jest podstawą procentów jego dzieci (CSS Flexbox 1 § 9.8);
- `aspect-ratio` (CSS Sizing 4 § 5.1), 2026-11-15: nowa właściwość dla pudełek niezastępowanych:
  `css/properties` (`PAspectRatio`, pliki generowane), `css/validation` (`aspectRatio`),
  `html/layout/block.go` (wysokość z szerokości przy `height: auto`, z uwzględnieniem `box-sizing`,
  `min-height`/`max-height` i treści wyższej niż wynik), `html/layout/preferred.go` (`ratioContentWidth`:
  szerokość treści z podanej wysokości dla pudełek dopasowujących się do treści), `html/layout/replaced.go`
  (`applyPreferredRatio`: na elemencie zastępowanym proporcja podana bez `auto` zastępuje proporcję obrazu,
  a `auto <proporcja>` działa tylko dla obrazu bez własnej). Źródło tej właściwości nie znało;
- `display: contents` (CSS Display 3 § 2.5), runda 2026-11-15: `css/properties/types.go` (`Display.Contents`),
  `css/validation/validation.go` (`display`), `html/boxes/build.go` (`elementToBox`,
  `keepsBoxWithDisplayContents`): element nie tworzy pudełka, a jego dzieci i pseudoelementy przechodzą do
  rodzica; elementy zastępowane i formularzowe zachowują pudełko. Źródło odrzucało tę wartość jako nieznaną
  (testy WPT `css-tables/display-contents-001…003`);
- strony i pola marginesowe, runda 2026-11-15:
  - nowa właściwość `outline-offset` (CSS UI 4 § 3.2): `css/properties` (`POutlineOffset`, pliki generowane
    przez `css/properties/cmd`), `css/validation` (`outlineOffset`), `html/tree/computed_values.go`,
    `html/document/draw_border.go` (`drawOutline`); źródło jej nie znało i zgłaszało ostrzeżenie;
  - `html/tree/style.go` (`viewport.size`): obszar strony dla jednostek `vw`, `vh`, `vmin`, `vmax` to strona
    bez marginesów, obramowania i odstępu wewnętrznego, z procentami liczonymi od rozmiaru strony
    (CSS Page 3 § 5.3); wcześniejsza wersja odejmowała tylko marginesy podane długością;
  - `html/layout/layout.go`: liczniki założone przez `counter-reset` elementu głównego są widoczne
    w kontekście strony, więc `counter-increment` w `@page` zwiększa licznik dokumentu; w źródle kontekst
    strony zaczynał bez nich i liczył od zera;
  - `html/layout/pages.go`: `counter-reset` w polu marginesowym zastępuje licznik strony o tej samej nazwie
    zamiast zakładać licznik zagnieżdżony;
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
