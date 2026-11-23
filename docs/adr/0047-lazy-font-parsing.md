# ADR 0047: Fonty parsowane w całości przy pierwszym użyciu

Status: przyjęta 2026-11-23.

## Kontekst

Pamięć podręczna fontów (ADR 0045) usuwa koszt parsowania z kolejnych renderowań w jednym procesie, ale
pierwsze renderowanie i każde wywołanie polecenia `gopdfgo` parsowały w całości (`font.NewFont` z go-text:
`glyf`, `CFF`, `GSUB`, `GPOS`, `kern`, `post`…) wszystkie podane fonty, także nieużyte. Polecenie z
`-fonts testdata/fonts` dostaje 67 plików, a typowy dokument używa 2–5 z nich. Profil w procesie (faktura
`20-invoice` i `05-hyphenation`, bez `FontCache`, 2026-11-23): `parseFontFile` ok. 17% czasu procesora,
z tego `tables.ParseGlyf` ok. 15%. Do tego zapis PDF sprawdza dla każdego użytego fontu, czy ma kontury
(`isSupportedFont` w `goweasyprint/pdf`), parsując całą `glyf` drugi raz: ok. 3% czasu.

folio dekoduje od razu tylko tablice metryczne, a resztę przy pierwszym użyciu (`docs/benchmark.md` § 11,
plan pkt 3 i 4). go-text jest zależnością modułu (nie forkiem), więc nie zmieniamy jej parsera.

Rozważone drogi:

1. Własny indeks fontów (rodzina, styl, grubość, pokrycie `cmap`) i własny wybór fontu w miejscu
   `fontscan.FontMap`, wzorem indeksu fontów systemowych (ADR 0039). Odrzucone: wybór twarzy dla fontów
   wywołującego to dziś `fontscan.FontMap` (dopasowanie rodzin, tablica podstawień, pismo, kolejność
   fontów podanych); przepisanie go to duża zmiana wyniku do udowodnienia na całym korpusie.
2. `isSupportedFont` z samej tablicy `loca` zamiast całej `glyf`. Odrzucone: dla fontu z uszkodzonym glifem
   `tables.ParseGlyf` zwraca błąd i font nie jest osadzany jako TrueType, a `loca` tego nie widzi; zmiana
   wyniku.
3. Fontmapa go-text bez zmian, ale twarze rejestrowane z fontem „pustym” i wypełniane przy pierwszym użyciu.
   `FontMap.AddFace` potrzebuje z fontu tylko `Cmap` (odcisk pokrycia znaków) i opisu (rodzina, odmiana),
   a fontmapa i konfiguracja tekstu rozpoznają twarze po wskaźniku `*font.Font`. Wybrane.

## Decyzja

1. `text.FontCache` ma dla pliku dwa stopnie: streszczenie (`Summary`: `cmap` z `font.ProcessCmap` jak w
   `font.NewFont`, opis z `font.Describe`, oś `wght`), czytane z tablic `OS/2`, `cmap`, `head`, `maxp`,
   `name`, `fvar`, oraz pełne parsowanie (`Parse`) na żądanie. Streszczenie sprawdza te same tablice
   wymagane w tej samej kolejności co `font.NewFont`, więc plik odrzucany przy pełnym parsowaniu jest
   odrzucany przy rejestracji z tym samym błędem (`Options.Fonts` dają błąd `Render` jak dotąd).
2. Fonty z `Options.Fonts` (`FontConfigurationGotext.AddFontFile`) i z reguł `@font-face` (`loadOneFont`)
   trafiają do fontmapy jako twarze z fontem pustym: `&font.Font{Cmap: …}` (dla `unicode-range` w
   `rangeCmap`). Wybór twarzy do kształtowania i metryk idzie zawsze przez `faces()`; póki są twarze puste,
   `faces()` zwraca warstwę (`lazyLayer`), która przed zwróceniem twarzy parsuje jej plik przez pamięć fontów
   i nadpisuje strukturę fontu pod tym samym wskaźnikiem pełnym fontem (z zachowaniem `Cmap` z rejestracji),
   a twarz dostaje nowe pamięci glifów (`font.NewFace`). Warstwa przyjmuje pismo (`SetScript`) jak
   `fontscan.FontMap`: segmentacja tekstu go-text ustawia pismo tylko fontmapie, która je przyjmuje, a od
   pisma zależy wybór fontu zastępczego (pierwsza wersja bez `SetScript` zmieniała font trójkąta `summary`
   w `13-forms-html-elements`). Z fontami systemowymi warstwa jest nad `systemLayer`, która pisma nie
   przyjmuje, więc i ona go nie przyjmuje.
3. Wynik sprawdzenia `isSupportedFont` jest zapamiętywany w pamięci fontów według treści pliku
   (`FontCache.Supported`, `pdf.Output.SetFontSupportCache`); z `Options.FontCache` między renderowaniami,
   bez niej w obrębie renderowania (także dla drugiego przebiegu, który tworzy nowy `pdf.Output`).
4. `text.LazyFontsDisabled` przywraca parsowanie przy rejestracji (testy, pomiary), `text.OnFontParsed`
   zgłasza pełne parsowania.

## Konsekwencje

- Wynik bez zmian (sprawdzenia w `docs/benchmark.md` § 11, „Leniwe wczytywanie fontów”); test
  `tests/lazyfonts23_test.go` porównuje oba tryby na całym korpusie dokumentów, z pamięcią fontów i bez, oraz
  błędy fontów bez tablicy wymaganej.
- Polecenie z wieloma fontami płaci za nieużyte fonty tylko streszczenie (cmap i nazwy).
- Twarz pusta nie może wyjść poza fontmapę inną drogą niż `faces()`; jedyne inne odwołanie do
  `fc.fm.ResolveFace` (`familyAvailable`) czyta tylko położenie i opis twarzy. Nowy kod, który bierze twarze
  z `fc.fm` z pominięciem `faces()`, musi wywołać `loadFace`.
- Zmiany w przejętym kodzie (`internal/webrender/text`: `font_cache.go`, nowy `lazy_fonts.go`,
  `engine_gotext.go` `loadOneFont`, `system_fonts.go` `faces`; `internal/goweasyprint/pdf`: `AddFont`,
  `Output.SetFontSupportCache`) opisane w `THIRD_PARTY.md`.
