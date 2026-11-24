# ADR 0027: Rysowanie stron w trakcie układu

Status: przyjęta 2026-11-20.

## Kontekst

Silnik odziedziczył po WeasyPrint kolejność: najpierw układ wszystkich stron (`layout.Layout`), potem
rysowanie (`document.Write`). Drzewa pudełek wszystkich stron żyją więc naraz, a pamięć rośnie z liczbą
stron. Pomiar z 2026-11-20 (żywa sterta po wymuszonym odśmiecaniu, książka z ilustracjami ×4, 1021 stron):
HTML i arkusze 85 MB, drzewo pudełek 191 MB, wszystkie strony ułożone 527 MB, wszystkie narysowane 859 MB.
Zwalnianie strony zaraz po narysowaniu (`ReleaseDrawnPages`, commit `edbcd9f`) obniżyło szczyt RSS do
1,34–1,36 GB, ale szczyt nadal przypadał na koniec układu, gdy wszystkie strony są jeszcze w pamięci.

Strona ułożona w pierwszym obiegu paginacji jest zwykle od razu ostateczna. Po paginacji silnik robi
jeszcze obróbkę wszystkich stron (`layoutDocument`): zakładki bez powtórzeń, wartości `string-set`, pudełka
`fixed` powtarzane na każdej stronie, fragmenty pudełek pozycjonowanych dokładane do stron wcześniejszych,
pola marginesowe, tła. Większość tych kroków dotyczy tylko bieżącej strony i stron poprzednich. Wyjątki to
pola marginesowe, bo `counter(pages)` zna dopiero koniec dokumentu, i odnośniki wewnętrzne, bo kotwica może
leżeć dalej.

Warunek: wynik ma być bajt w bajt taki sam jak dotąd.

## Decyzja

1. `htmlpdf.Render` rysuje każdą stronę zaraz po tym, jak jej obszar jest ostateczny, a potem zwalnia jej
   drzewo pudełek (`layout.LayoutStreaming`, `document.Stream`). Obróbka strony po paginacji jest wydzielona
   z `layoutDocument` do `pageFinisher` (`html/layout/stream.go`). Obie drogi wykonują te same kroki w tej
   samej kolejności dla każdej strony.
2. Pola marginesowe są układane i rysowane po ułożeniu wszystkich stron, gdy liczba stron, wartości
   `string-set` i elementy biegnące są znane.
   - W kontekście nakładania strony treść dokumentu ma z-index 0 (CSS Page 3 § 3.1), więc pola marginesowe
     o z-index 0 albo dodatnim rysują się po elemencie głównym i obszarze przypisów, na końcu strony.
   - Rysowanie obszaru strony zapamiętuje to miejsce w treści (`backend.ResumablePage`,
     `contentstream.GraphicStream.Mark`). Za nim są już tylko zamknięcia stanów graficznych (`Q`).
   - Po ułożeniu wszystkich stron strumień wraca do zapamiętanego miejsca razem ze stanem graficznym, a pola
     marginesowe dopisują się tam, gdzie narysowałaby je dotychczasowa droga. Zamknięcia stanów są odtwarzane
     za nimi.
   - Granica form masek miękkich zależy od rozmiaru strony (`maskLimit`), więc na czas dorysowania wraca
     do wartości tej strony.
3. Narysowana strona czeka na pola marginesowe w postaci skompresowanej (`GraphicStream.Flush`, zlib w
   stopniu 1). Operacje zajmują w pamięci kilkanaście razy więcej niż skompresowany tekst strumienia.
   - Separator po operacji zależy od jej numeru (`WriteOperations`), dlatego dalszy ciąg jest zapisywany
     od tego samego numeru.
   - Przy zapisie pliku tekst jest rozpakowywany i kompresowany zwykłym stopniem, więc bajty się nie zmieniają.
4. Odnośniki, kotwice (`/Dests`), zakładki, załączniki i metadane powstają na końcu, jak w `Write`. Model PDF
   to graf wskaźników numerowany przy zapisie pliku, więc kolejność wywołań nie zmienia numerów obiektów.
5. Pomniejszanie treści szerszej niż strona (ADR 0014):
   - pierwszy przebieg rysuje strony, dopóki nic nie wystaje;
   - od pierwszej wystającej strony przebieg już tylko mierzy (`contentOverflowScale` po stronach) i zwalnia
     strony bez rysowania;
   - drugi przebieg, w skali, rysuje wszystko od nowa na nowym celu.
6. Dokument wraca do dotychczasowej drogi, gdy strona albo strony wcześniejsze mogą się jeszcze zmienić.
   Wtedy cel i ostrzeżenia z odrzuconego przebiegu idą do kosza, a dokument jest układany od nowa przez
   `document.Render` i `Write`. Warunki:
   - drugi obieg paginacji jest przesądzony (`RemakeState.ContentChanged` albo `PagesWanted` na stronie już
     ułożonej): `target-counter()` i `target-text()` do stron dalszych (spis treści), `counter(pages)`
     w treści stron;
   - fragment pudełka pozycjonowanego ma trafić na stronę wcześniejszą (`lateBoxes`, `attachLateBoxes`);
   - pudełko `fixed` pojawia się po raz pierwszy na stronie dalszej, a powtarza się też na stronach
     wcześniejszych (`layoutFixedBoxes` z `pages[i+1:]`);
   - `string-set` albo `bookmark-label` z `counter(pages)` albo licznikiem celu;
   - pole marginesowe o ujemnym z-index, rysowane przed płótnem;
   - rysowanie strony nie skończyło się w kontekście nakładania strony.

   Pierwsze cztery warunki wykrywa strona, na której zachodzą, więc praca idzie do kosza tylko do tego
   miejsca. Pole o ujemnym z-index wychodzi dopiero na końcu, czyli kosztem całego przebiegu.
7. `document.StreamingDisabled` wyłącza rysowanie w trakcie układu, a `document.OnStream` podaje przeszkodę.
   Oba służą testom i pomiarom. API publiczne się nie zmienia.
8. Wszystko, co pola marginesowe czytają z drzewa strony (słowo `start` w `string()` i `element()`),
   jest zapamiętywane przed zwolnieniem drzewa. Poprawność sprawdzamy porównaniem bajtowym z silnikiem
   sprzed zmiany na całym korpusie W3C (11 689 dokumentów w trybie stronicowanym i w trybie sondażu), bo
   liczba zaliczeń nie widzi testów bez wzorca; tak wykryto błąd słowa `start` (2026-11-21).

## Wyniki

Pomiar 2026-11-20 na tym samym komputerze, wiersz poleceń, książka `1342-bench.htm` (256 stron) i jej
czterokrotność `x4.htm` (1021 stron, 656 ilustracji). Porównanie z commitem `edbcd9f`, po dwa przebiegi:

| | książka, przed | książka, po | ×4, przed | ×4, po |
|---|---|---|---|---|
| szczyt RSS (`/usr/bin/time -l`) | 0,70–0,74 GB | 0,56–0,62 GB | 1,35–1,39 GB | 0,90–0,96 GB |
| czas | 1,56–1,64 s | 1,61–1,83 s | 4,59–4,60 s | 4,77 s |
| czas procesora (user) | 2,12–2,16 s | 2,12–2,15 s | 7,73–7,78 s | 7,63–7,67 s |
| szczyt żywej sterty (odśmiecanie co 20 ms) | 290–350 MB | 244–262 MB | 606–609 MB | 417–434 MB |
| żywa sterta po narysowaniu wszystkich stron | 65 MB | 41 MB | 146 MB | 49 MB |

- Bez kompresji treści oczekującej (punkt 3) szczyt żywej sterty ×4 wynosił 508–520 MB. Operacje tekstu
  wszystkich stron zajmowały wtedy 105–121 MB.
- Szczyt przypada teraz na układ i wyznacza go wejście: style obliczone, drzewo pudełek i obrazy. W profilu
  w chwili szczytu jest też 69–80 MB złożonych wierszy (`text.newLineStore`). Ich ilość nie rośnie z liczbą
  stron (książka 80 MB, ×4 69 MB), więc nie są to drzewa stron narysowanych. Skąd się biorą, nie jest
  ustalone.
- Wynik jest bajt w bajt taki sam jak z `edbcd9f` na 22 dokumentach i 23 plikach SVG korpusu, na książce,
  na ×4 i na 19 dokumentach z cechami z punktu 6 (`tests/streamdraw18_test.go`, `TestStreamDrawing*`).
  Wszystkie dokumenty korpusu oprócz `18-toc-running-elements.html` (spis treści z `target-counter`) są
  rysowane w trakcie układu.

## Konsekwencje

- Dokumenty z odnośnikami do kotwic, które nie istnieją, zgłaszają ostrzeżenie „No anchor” po ostrzeżeniach
  z rysowania, a nie przed nimi. Liczba i treść ostrzeżeń się nie zmieniają.
- Dokument z pomniejszaniem zgłasza ostrzeżenia z pól marginesowych pierwszego przebiegu raz, a nie dwa razy:
  przebieg mierzący ich nie układa.
- Spis treści z `target-counter()` do stron dalszych zawsze wraca do dotychczasowej drogi (od 2026-11-21
  już nie: ADR 0036 układa takie dokumenty w dwóch przebiegach). Wstępny przebieg,
  który zapisałby tylko numery stron kotwic, nie daje dziś tych samych bajtów. W drugim obiegu paginacji
  `layoutDocument` układa od nowa tylko strony oznaczone, a resztę bierze z pierwszego obiegu.
  `TargetCollector.CacheTargetPageCounters` oznacza stronę z odwołaniem przy każdym ułożeniu strony
  z kotwicą, bez porównania wartości. Gdyby układać od nowa wszystkie strony, zachodziłby trzeci obieg,
  którego dotychczasowa droga nie robi.
  - Droga do rozwiązania: porównywać wartości przed oznaczeniem (zmiana algorytmu przejętego kodu,
    `TODO` w źródle), a potem sprawdzić, że ponowny układ strony daje to samo co strona wzięta
    z poprzedniego obiegu.
  - Wtedy spis treści można rysować w trakcie drugiego obiegu, kosztem drugiego układu całego dokumentu.
- `document.Render` i `Write` zostają: są drogą zapasową i używają ich testy przejęte.
