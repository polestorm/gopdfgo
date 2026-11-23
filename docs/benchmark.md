# Benchmark forka webrender na korpusie

Stan na 2026-11-12: dokumenty własne, próbki WeasyPrint i reftesty druku WPT zmierzone dla forka webrender;
folio i Chromium jeszcze nie.
Silnik: fork `benoitkugler/webrender` `6e769b1` bez Pango + `go-weasyprint` `e4c9509` (ADR 0002), z jedną poprawką
(wspólny obiekt fontu, pkt 3.1). Program pomiarowy i skrypt w `bench/` poza repozytorium (`bench/run.py`); fonty
wyłącznie z `testdata/fonts`, zasoby wyłącznie spod `testdata/`. Maszyna: macOS arm64, go1.26.6, jeden proces na
dokument. Czas to samo renderowanie bez startu procesu; pamięć to suma alokacji (`TotalAlloc`), nie szczyt.

## 1. Dokumenty własne (`testdata/documents`)

| Dokument | Strony fork / spec / Chromium | Czas [ms] | PDF [kB] | Wynik oględzin |
|---|---|---|---|---|
| 01 kopia zapisu | 3 / – / 3 | 45 | 465 | zgodny z opisem; `string-set` w polu marginesowym działa |
| 02 rejestr | 8 / – / 8 | 78 | 821 | `thead` powtórzony na 8 stronach |
| 03 raport SVG | 2 / – / 2 | 23 | 348 | **błąd**: gradient słupków liczony dla całego wykresu, nie dla słupka; reszta poprawna |
| 04 procedura SZJ | 10 / – / 10 | 50 | 506 | nagłówek bieżący z tytułem rozdziału działa |
| 05 dzielenie wyrazów | 1 / – / 1 | 55 | 168 | polskie, angielskie i niemieckie wzorce działają; `&shy;` i `none` poprawne |
| 06 pola marginesowe | **3 / 3** / 2 | 17 | 184 | 16 pól, `:first`, `:blank`, pusta strona; pola górne i dolne mają szerokość treści, nie wypełniają krawędzi (do sprawdzenia ze specyfikacją) |
| 07 obrazy | 6 / – / 6 | 15 | 157 | nie oglądano |
| 08 flex | 2 / – / 2 | 20 | 189 | wszystkie 12 przypadków poprawne, także `wrap`, `gap`, `order` |
| 09 listy i liczniki | 1 / – / 1 | 21 | 247 | nie oglądano |
| 10 białe znaki | 1 / – / 1 | 21 | 214 | `word-break: keep-all` odrzucone z ostrzeżeniem |
| 11 przypadki brzegowe | **brak** | >120 s | – | **zawieszenie** na 60 zagnieżdżonych `div` z obramowaniem i dopełnieniem (przypadek 6); pozostałe 8 przypadków renderuje się w 12–18 ms, nieznana właściwość, wartość `display` i błędny HTML obsłużone |
| 12 float, pozycjonowanie | 2 / – / 2 | 20 | 220 | zgodny z opisem |
| 13 formularze | **2** / – / 1 | 23 | 288 | **błędy**: pola formularza jako bloki, hasło jawnym tekstem, zwinięte `details` pokazuje treść |
| 14 selektory i kaskada | 1 / – / 1 | 55 | 174 | żaden pasek czerwony; `calc()`, `clamp()`, `:where()` odrzucone z ostrzeżeniem |
| 15 podziały stron | 13 / 13 / 13 | 13 | 164 | **błąd**: po stronie nazwanej treść domyślna (S6) zostaje na stronie poziomej; `orphans`, `widows`, `slice` poprawne |
| 16 typografia | 1 / – / 1 | 62 | 505 | rodziny generyczne z podanych fontów; brakujące glify (✓, 中, emoji) znikają bez ostrzeżenia; `ß` bez zamiany na `SS` |
| 17 tabele | 2 / – / 2 | 19 | 181 | `fixed`, `auto`, `caption-side`, `empty-cells`, `rowspan` poprawne |
| 18 spis treści („powinien”) | 6 / 6 / 6 | 32 | 430 | `target-counter`, `target-text`, `running()`, zakładki działają |

17 z 18 dokumentów renderuje się; liczba stron zgodna z oczekiwaną w 16 z 17.

### Próbki WeasyPrint (`testdata/samples/weasyprint`)

| Próbka | Strony fork / WeasyPrint | Czas [ms] | Wynik |
|---|---|---|---|
| invoice | 1 / 1 | 16 | niemal identyczna z PDF autorów; blok adresu po prawej łamie się węziej (różnica w szerokości preferowanej) |
| letter | 1 / 1 | 16 | nie oglądano |
| report | 8 / 8 | 46 | 5 obrazów tła SVG z arkusza CSS odrzuconych: „Relative URI reference without a base URI” (adres bazowy arkusza gubiony; fork albo program pomiarowy, do ustalenia) |
| ticket | 1 / 1 | 21 | nie oglądano |

### Reftesty druku WPT (`testdata/wpt`, warstwa `paged`)

Test przechodzi, gdy test i wzorzec wyrenderowane forkiem dają te same strony (`pdftoppm -r 48`, tolerancja
20 pikseli różniących się o więcej niż 40 na kanał). Domyślna strona 5 × 3 cale, margines 0,5 cala.

| Wynik | Testy | Udział |
|---|---|---|
| przeszło | 135 | 40% |
| nie przeszło, a test albo wzorzec używa CSS spoza `scope.md` (jednostki `vh`/`vw`, `contain`, właściwości logiczne `inline-size`/`margin-block-*`, pionowy `writing-mode`, `page-orientation`, grid, `iframe`, `canvas`) | 119 | 35% |
| nie przeszło bez żadnego ostrzeżenia silnika | 69 | 20% |
| panika silnika (`index out of range`, `slice bounds out of range`) | 15 | 4% |
| nie przeszło, inne odrzucone deklaracje | 2 | 1% |

| Temat | przeszło | poza zakresem | różnica bez ostrzeżeń | panika |
|---|---|---|---|---|
| strony nazwane | 36 | 6 | 11 | |
| podziały | 23 | 7 | 5 | |
| element niepodzielny | 19 | 16 | 3 | |
| rozmiar strony | 14 | 8 | 4 | |
| marginesy strony | 13 | 12 | 2 | |
| fragmentacja tabel | 11 | 18 | 15 | 11 |
| kontekst strony | 7 | 4 | 4 | |
| selektory strony | 5 | | 1 | |
| fragmentacja float | 3 | | 8 | |
| pozycjonowane | 2 | 11 | 3 | |
| pola marginesowe | 1 | 30 | 5 | |
| fragmentacja flex | | 2 | 8 | 4 |
| inne | 1 | 5 | 2 | |

Jak czytać: 40% to dolna granica, nie miara Paged Media. W obejrzanym teście `margin-boxes/alignment-001` fork
renderuje sam test poprawnie (litery A–P wokół strony), a nie przechodzi, bo wzorzec jest zbudowany z nowszego CSS,
którego fork nie zna. Samo dodanie jednostek `vh`/`vw` dotyczy 60 testów. Rzeczywiste słabości to fragmentacja
tabel, float i flex (31 różnic bez ostrzeżeń i wszystkie 15 panik) oraz 11 testów stron nazwanych, co zgadza się
z błędem z dokumentu 15.

## 2. Ostrzeżenia

Silnik zgłasza nieznane właściwości i wartości z nazwą i treścią deklaracji (`Ignored 'width: calc(...)', invalid
value`), bez pozycji w źródle. Zgłoszone w korpusie: `print-color-adjust`, `calc()`, `clamp()`, `:where()`,
`word-break: keep-all`, `text-shadow`, `repeating-linear-gradient`, `fill` w CSS dla SVG. Nie zgłasza brakujących
glifów.

## 3. Wady warstwy PDF i ich stan

| Wada | Przyczyna | Stan 2026-11-12 |
|---|---|---|
| font osadzany raz na każdy ciąg tekstu (dokument 01: 264 razy, 9,3 MB) | ścieżka go-text tworzyła nowy obiekt fontu dla każdego ciągu | poprawione w `internal/webrender/text/draw` |
| tekst niewyszukiwalny (`pdftotext` zwracał litery rozdzielone spacjami), wymaganie Y1 | szerokość glifu liczona z obrysu zamiast z posunięcia, więc tablica `/W` była błędna | poprawione tamże; `corpus_test.go` sprawdza wyodrębnianie tekstu |
| duże pliki | kompresja strumieni wyłączona stałą w źródle | włączona; dokument 01: 465 kB → 229 kB |
| brak determinizmu, wymaganie Y2 | (a) CMap ToUnicode zapisywany iteracją po mapie, (b) biblioteka `benoitkugler/pdf` zapisuje słowniki zasobów strony iteracją po mapach (`model/pages.go`), więc kolejność obiektów fontów jest losowa | poprawione 2026-11-12: własny zapis CMap, sortowanie kluczy w kopii biblioteki (`internal/pdflib`), oraz trzecia przyczyna znaleziona po drodze: kolejność rysowania bloków w webrender brała się z iteracji po mapie (`BoxTree`). 17 dokumentów daje identyczne bajty w kolejnych przebiegach i procesach; amd64 wobec arm64 niesprawdzone |

Wcześniejszy zapis „fonty bez podzbiorów” był błędny: fonty są osadzane jako podzbiory (34 kB w dokumencie 06),
a `pdffonts` pokazuje `sub no` tylko dlatego, że przedrostek nazwy nie jest sześcioliterowym znacznikiem.

Testy pikselowe backendu z Ghostscriptem: 403 przechodzą, 2 nie, 10 pominiętych w źródle (`../THIRD_PARTY.md`).

## 4. Poprawki odporności (2026-11-12)

- Zawieszenie na głębokim zagnieżdżeniu: przyczyną była podwójna rekurencja w `PageValues` (koszt 2^n).
  Po poprawce 60 poziomów trwa 2 ms, 400 poniżej 2 s (`TestDeepNesting`); dokument 11 renderuje się.
- 15 panik z reftestów: 12 przez komentarz przed `<html>`, 3 przez indeks wznowienia w komórce tabeli. Obie
  poprawione; `TestWPTNoPanic` renderuje 726 plików bez paniki.
- Liczby z tabeli reftestów w pkt 1 pochodzą sprzed poprawek; przebieg do powtórzenia (część błędów `error`
  i `ref-error` zamieni się w zaliczone albo w zwykłe różnice).

## 5. calc() (2026-11-12)

`calc()`, `min()`, `max()`, `clamp()` działają dla długości. `calc_test.go` porównuje 38 deklaracji z `calc()`
z równoważnymi wartościami wprost (identyczne bajty PDF) dla szerokości, wysokości, marginesów, dopełnień,
obramowań, fontu, interlinii, odstępów, pozycji, `flex-basis`, tła i kolumn, oraz sprawdza odrzucenie 8 wyrażeń
niepoprawnych. Ograniczenia:

- procenty wewnątrz `min()`, `max()`, `clamp()` są odrzucane z ostrzeżeniem (wyniku nie da się sprowadzić do sumy
  pikseli i procentu);
- tylko długości: `calc()` dla liczb (`opacity`, `z-index`, interlinia jako mnożnik), kątów i kolorów nie działa;
- przy liczeniu szerokości wewnętrznych (`html/layout/preferred.go` i ok. 30 miejsc sprawdzających jednostkę
  procentową wprost) dodatek w pikselach z `calc(50% - 10px)` jest pomijany; dotyczy to głównie komórek tabel
  o układzie automatycznym i elementów dopasowanych do treści;
- znak wyniku nie jest sprawdzany przy walidacji (`width: calc(1px - 5px)` nie jest odrzucane).

## 6. Reftesty po poprawkach (2026-11-13)

`TestWPTReftests` (`reftest_test.go`) renderuje reftesty przez `gopdfgo.Render` i porównuje wynik ze stanem
bazowym w `testdata/wpt/baseline.tsv`; regresja kończy test błędem. Kolejne stany:

| Stan | Zaliczone z 340 |
|---|---|
| pierwszy benchmark (program w `bench/`, przed poprawkami) | 135 |
| po poprawkach odporności i `calc()` | 140 |
| po `vh`/`vw` i naprawie zawieszenia w grid | 151 |
| po właściwościach logicznych | 154 |
| po naprawie stron nazwanych | 161 |

Dodanie `vh`/`vw` odebrało 6 zaliczeń, które były fałszywe: test i wzorzec ignorowały tę samą jednostkę, więc
wyglądały tak samo. Dodało też zawieszenie: siatka (`display: grid`) z dzieckiem wyższym niż strona tworzyła
strony bez końca; błąd istniał wcześniej, tylko był zasłonięty przez ignorowaną jednostkę.

Najsłabsze tematy w stanie bazowym: pola marginesowe 1 z 36 (wzorce nadal się nie renderują jak trzeba),
fragmentacja tabel 13 z 55, flex 3 z 14, float 3 z 11.

## 7. Pola marginesowe (2026-11-13)

Reftesty `css-page/margin-boxes`: 3 z 36 po poprawce (było 1). Oględziny pięciu niezaliczonych par pokazały dwie
różne rzeczy:

- **błąd silnika, poprawiony:** pola o szerokości `auto` kurczyły się do własnej treści zamiast zająć wyliczoną
  część krawędzi (CSS Page 3 § 5.3.2.2: szerokości sumują się do dostępnej). Po poprawce testy `dimensions-001`,
  `content-001`, `alignment-001` i dokument 06 korpusu renderują się zgodnie z opisem testu;
- **wzorce, których silnik nie renderuje poprawnie:** pliki wzorcowe odtwarzają wygląd pól zwykłym układem:
  elementami `position: fixed` z licznikami stron, wysokościami procentowymi we flex, siatką z `inset`. To braki
  poza polami marginesowymi (np. `content-004`: nagłówek ze wzorca nakłada się na treść).

Niski wynik tej grupy mierzy więc dziś głównie `position: fixed` w druku i procentowe wysokości we flex, a nie
same pola marginesowe.

## 8. Do zrobienia

- Przejrzeć 69 różnic bez ostrzeżeń; powtórzyć reftesty przez `gopdfgo.Render` zamiast programu w `bench/`.
- Dodać `vh`/`vw` i właściwości logiczne, powtórzyć przebieg, żeby oddzielić braki składni od błędów fragmentacji.
- folio i Chromium na tym samym programie jako punkty odniesienia.

## 9. Czas i rozmiar po paczkach kolumn (2026-11-16)

Sposób pomiaru: `go test -run '^$' -bench 'BenchmarkCorpus|BenchmarkLargeTable' -benchtime 10x .`
(`benchmark_test.go`), macOS arm64, 10 rdzeni, go1.26.6. Czas to jedno wywołanie `Render` razem
z czytaniem i parsowaniem fontów dokumentu, bez startu procesu; pamięć to suma przydziałów, nie szczyt.
Kolumna „przed” to ten sam pomiar (5 powtórzeń) na stanie `ef7c91b`, przed dwiema poprawkami opisanymi niżej.

| Dokument | Strony | Czas przed [ms] | Czas [ms] | Pamięć przydzielona [MB] |
|---|---|---|---|---|
| `01-record-copy` | 3 | 56 | 47 | 104 |
| `02-register-table` | 8 | 92 | 81 | 173 |
| `03-report-svg-chart` | 2 | 56 | 46 | 164 |
| `04-qms-procedure` | 10 | 70 | 65 | 153 |
| `05-hyphenation` | 1 | 53 | 43 | 88 |
| `06-margin-boxes` | 3 | 37 | 32 | 89 |
| `07-images` | 6 | 45 | 32 | 91 |
| `08-flex` | 2 | 38 | 32 | 85 |
| `09-lists-counters` | 1 | 43 | 36 | 90 |
| `10-preformatted` | 1 | 50 | 38 | 91 |
| `11-edge-cases` | 2 | 52 | 40 | 99 |
| `12-floats-positioning` | 2 | 37 | 33 | 85 |
| `13-forms-html-elements` | 1 | 45 | 37 | 99 |
| `14-cascade-selectors` | 1 | 41 | 35 | 85 |
| `15-page-breaks` | 13 | 40 | 37 | 107 |
| `16-typography-fonts` | 1 | 48 | 44 | 117 |
| `17-tables` | 2 | 39 | 32 | 85 |
| `18-toc-running-elements` | 6 | 49 | 43 | 107 |
| `19-flex-grid-pagination` | 13 | 94 | 90 | 153 |
| `20-invoice` | 3 | 57 | 50 | 105 |
| `21-mixed-report` | 3 | 56 | 52 | 118 |
| `22-multicol-pagination` | 11 | 100 | 101 | 160 |

Tabela 4000 wierszy (`BenchmarkLargeTable`, 130 stron): 1.10 s i 1298 MB; przed poprawkami 1,33 s
i 1391 MB; 15.11 było 1,06 s i 1239 MB, więc ok. 0,05 s i 60 MB przybyło od tamtego czasu z innych powodów
(nie ustalałam których).

Wnioski:

- **Kolumny nie spowolniły silnika.** Dokumenty `19`, `21` i pozostałe mają ten sam czas co na stanie sprzed
  paczek kolumn (`99006b1`); próby z kolumnami do 53 stron (1500 akapitów w trzech kolumnach) trwają tyle
  samo co przed zmianami (0,40 s wobec 0,38 s całego procesu). Równoważenie krokami po 1 px nie jest
  widoczne w czasie.
- **Stały koszt ok. 30 ms na dokument** nie zależy od treści. Profil dokumentu `17-tables` (32 ms): ok. 40%
  to parsowanie fontów podanych w `Options.Fonts` (biblioteka go-text parsuje od razu wszystkie glify),
  ok. 25% czytanie arkuszy i fontów z `@font-face`, reszta to układ, rysowanie i zapis PDF.
- **Dwie poprawki z tego pomiaru** (obie bez wpływu na wynik renderowania): reguła `@font-face` czytana
  drugi raz nie wczytuje fontu ponownie, a style policzone w przedbiegu dla `@media` (ADR 0012) służą
  właściwemu układowi, o ile arkusze nie muszą być czytane drugi raz. Małe dokumenty zyskały 10–25%,
  tabela 4000 wierszy 17%.
- **Do decyzji:** parsowanie `Options.Fonts` przy każdym wywołaniu. Program renderujący wiele dokumentów
  tymi samymi fontami płaci za nie za każdym razem; przygotowany raz zestaw fontów albo pamięć podręczna
  według skrótu treści skróciłyby mały dokument z ok. 32 do ok. 20 ms (szacunek z profilu). Wymaga to
  zmiany interfejsu albo stanu wspólnego między wywołaniami, więc nie zostało zrobione.

Cały proces (`time`, trzy powtórzenia, pierwsze pominięte), dla orientacji wobec Chromium 157 bezgłowego
na tej samej maszynie:

| Dokument | gopdfgo [s] | Chromium [s] |
|---|---|---|
| `17-tables` (2 strony) | 0,03 | 0,40 |
| `02-register-table` (8 stron) | 0,08 | 0,46 |
| `22-multicol-pagination` (11 stron) | 0,10 | 0,40 |

Czas Chromium to głównie start przeglądarki; w usłudze, która trzyma przeglądarkę uruchomioną, różnica
byłaby mniejsza. Tego nie mierzyłam.

Rozmiar binarki `cmd/gopdfgo` (macOS arm64, `CGO_ENABLED=0 -trimpath -ldflags='-s -w'`): **23,35 MB**
(14.11: 21,60 MB); linux/amd64 z `-s -w`: 24,3 MB. Wobec ok. 400 MB Chromium z bibliotekami to 17 razy
mniej, wobec 150–200 MB `chrome-headless-shell` 6–8 razy mniej.

## 10. Czas i rozmiar po paczkach piątej–ósmej (2026-11-18)

Sposób pomiaru jak w § 9 (`go test -run '^$' -bench … -benchmem .`, macOS arm64, go1.26.6, maszyna bez innej
pracy, obciążenie ok. 2); czasy na stanach z końca każdej paczki, 5 powtórzeń; pamięć to suma przydziałów
na jedno wywołanie.

| Stan | Tabela 4000 wierszy [ms / MB] | `19-flex-grid-pagination` [ms / MB] | `22-multicol-pagination` [ms / MB] |
|---|---|---|---|
| `5ef9e30` (przed piątą paczką) | 1189 / 1330 | 92 / 158 | 104 / 165 |
| `a9fb4a6` (po piątej) | 1198 / 1339 | 92 / 159 | 105 / 166 |
| `47b6ec0` (po szóstej) | 1250 / 1369 | 92 / 159 | 107 / 168 |
| `f04fa4f` (po siódmej) | 1286 / 1377 | 97 / 166 | 113 / 175 |
| `1930d0c` (po ósmej) | 1344 / 1406 | 104 / 167 | 116 / 175 |

Cały korpus (10 powtórzeń, `1930d0c`): 35–117 ms na dokument, czyli o 5–20% więcej niż w § 9.

Binarka (`CGO_ENABLED=0 -trimpath -ldflags='-s -w'`): **23,30 MB** (16.11: 23,35 MB).

Wnioski:

- Przez cztery paczki (ok. 450 nowych zaliczeń W3C) czas dużej tabeli wzrósł o 13%, a przydzielona pamięć
  o 6%; przyrost jest rozłożony, najwięcej dała ósma paczka (+58 ms).
- W profilu procesora nie widać żadnej z nowych funkcji (`text-autospace`, kapitaliki, dopasowanie nazw
  rodzin, `-webkit-line-clamp`); ok. 24% czasu to odśmiecanie. W profilu przydziałów wobec `f04fa4f` rośnie
  kształtowanie tekstu (`HarfbuzzShaper.shape` +54 MB, kopie glifów w `WrapWordBreak` +40 MB na 5 przebiegów),
  co odpowiada kształtowaniu w całości tekstów do 128 znaków przy łamaniu wierszy (ósma paczka). Kandydat do
  przyspieszenia, gdy czas zacznie mieć znaczenie; dziś 130 stron w 1,34 s.
- Rozmiar binarki stoi w miejscu: nowe funkcje to kod układu, bez nowych tablic danych.

## 11. Porównanie z folio i przyspieszenia z niego wynikające (2026-11-23)

Za zgodą użytkownika (2026-11-23) sklonowałam i uruchomiłam folio, `github.com/carlos7ags/folio` (Apache-2.0),
commit `29bca95` z 2026-11-18. Klon i programy pomiarowe są poza gitem w `bench/folio/` (opis w
`bench/folio/README.md`); folio nie jest zależnością gopdfgo, wejście nie opuszcza maszyny (bez sieci).

Sposób pomiaru: Apple M4, 10 rdzeni, macOS, go1.26.6, maszyna bez innej pracy. Oba silniki dostają ten
sam HTML, w którym `url(../fonts/…)` zamieniono na `url(fonts/…)` (folio odrzuca `..`), z tymi samymi
fontami z `testdata/fonts` przez `@font-face`. folio: `document.NewDocument(PageSizeA4)`, `AddHTML` z
`BaseFS` i fontem zastępczym Noto Sans, `WriteTo`. gopdfgo: `Render` z `Options.Fonts` jak w poleceniu
`gopdfgo -fonts testdata/fonts`, ale tylko katalog główny, `tinos/` i `arimo/` (14 plików), i
`GenericFamilies` z `families.json`. Książka (`1342-bench.htm`) dostaje w obu silnikach `@font-face` Tinos
i `html { font-family: Tinos }`, bo folio bez tego drukuje fontem standardowym Times.
- „W procesie”: mediana z 20 (korpus), 10 (`bench/realdocs`) i 3 (książka) kolejnych renderowań po jednym
  rozgrzewającym; kolumna „z `FontCache`” to gopdfgo z jedną `Options.FontCache` dla wszystkich wywołań
  (ADR 0045);
- „Cały proces”: mediana z 5 uruchomień programu z jednym renderowaniem (zegar ścienny, ze startem);
  RSS: szczyt z `/usr/bin/time -l`;
- zgodność z Chromium: `bench/survey/tools/driftalign.py` wobec `testdata/documents/reference/chromium` i
  `bench/realdocs/out/chrome`: odsetek słów wzorca znalezionych na tej samej stronie i mediana odchyleń
  pionowych słów wspólnych na tej samej stronie.

Kolumny gopdfgo to stan po zmianach z tej sekcji (bez nich patrz tabela „przed i po” niżej).

| Dokument | Strony Chromium / folio / gopdfgo | W procesie [ms] folio / gopdfgo / gopdfgo z `FontCache` | Cały proces [ms] folio / gopdfgo | RSS [MB] folio / gopdfgo | PDF [kB] folio / gopdfgo | Słowa wzorca na tej samej stronie folio / gopdfgo | dy mediana [pt] folio / gopdfgo |
|---|---|---|---|---|---|---|---|
| `01-record-copy` | 3 / 2 / 3 | 18,9 / 44,6 / 30,8 | 25 / 58 | 29 / 89 | 35,7 / 28,2 | 48% / 100% | 36,7 / 0,2 |
| `02-register-table` | 8 / 7 / 8 | 63,8 / 73,2 / 59,9 | 71 / 88 | 45 / 116 | 111,9 / 81,3 | 15% / 100% | 116,0 / 0,2 |
| `03-report-svg-chart` | 2 / 2 / 2 | 30,0 / 30,2 / 17,2 | 36 / 43 | 30 / 82 | 57,9 / 27,1 | 81% / 100% | 18,8 / 0,2 |
| `04-qms-procedure` | 10 / 11 / 10 | 18,0 / 54,3 / 40,8 | 24 / 69 | 29 / 99 | 49,5 / 55,3 | 1% / 98% | 114,7 / 0,2 |
| `05-hyphenation` | 1 / 1 / 1 | 8,3 / 36,2 / 22,9 | 14 / 58 | 24 / 89 | 12,5 / 12,8 | 60% / 93% | 44,7 / 0,1 |
| `06-margin-boxes` | 2 / 1 / 3 | 8,2 / 24,6 / 11,1 | 14 / 37 | 26 / 79 | 14,0 / 18,6 | 30% / 47% | 9,8 / 0,2 |
| `07-images` | 6 / 6 / 6 | 13,4 / 26,1 / 12,6 | 19 / 39 | 24 / 82 | 62,0 / 42,7 | 94% / 100% | 94,7 / 0,2 |
| `08-flex` | 2 / 2 / 2 | 9,0 / 25,5 / 12,3 | 15 / 38 | 25 / 75 | 15,9 / 14,6 | 99% / 100% | 7,5 / 0,2 |
| `09-lists-counters` | 1 / 1 / 1 | 8,5 / 28,4 / 14,9 | 14 / 41 | 25 / 81 | 14,1 / 19,5 | 80% / 99% | 21,1 / 0,2 |
| `10-preformatted` | 1 / 1 / 1 | 8,7 / 29,9 / 16,7 | 14 / 43 | 25 / 79 | 14,4 / 17,2 | 68% / 98% | 130,9 / 0,1 |
| `11-edge-cases` | 2 / 4 / 2 | 15,8 / 32,4 / 18,9 | 22 / 46 | 32 / 87 | 23,4 / 23,1 | 75% / 99% | 232,7 / 0,1 |
| `12-floats-positioning` | 2 / 4 / 2 | 9,2 / 25,3 / 11,9 | 15 / 38 | 24 / 77 | 19,5 / 18,7 | 61% / 100% | 45,4 / 0,2 |
| `13-forms-html-elements` | 1 / 1 / 1 | 12,4 / 30,1 / 16,6 | 18 / 43 | 26 / 81 | 21,6 / 27,2 | 81% / 98% | 49,8 / 1,1 |
| `14-cascade-selectors` | 1 / 1 / 1 | 8,9 / 27,2 / 13,8 | 15 / 40 | 23 / 75 | 14,6 / 13,8 | 97% / 100% | 63,2 / 0,2 |
| `15-page-breaks` | 13 / 12 / 13 | 19,5 / 27,6 / 14,3 | 26 / 41 | 27 / 87 | 24,8 / 49,7 | 11% / 100% | 4,1 / 0,2 |
| `16-typography-fonts` | 1 / 1 / 1 | 23,2 / 35,2 / 21,6 | 29 / 48 | 37 / 89 | 38,9 / 46,9 | 96% / 97% | 10,8 / 0,2 |
| `17-tables` | 2 / 2 / 2 | 8,9 / 25,8 / 12,5 | 15 / 39 | 25 / 76 | 16,4 / 15,6 | 87% / 100% | 75,7 / 0,2 |
| `18-toc-running-elements` | 6 / 6 / 6 | 11,2 / 38,7 / 25,4 | 17 / 52 | 27 / 92 | 25,4 / 35,9 | 100% / 100% | 6,6 / 0,2 |
| `19-flex-grid-pagination` | 13 / 12 / 13 | 18,1 / 89,3 / 75,8 | 24 / 103 | 29 / 102 | 38,9 / 37,3 | 65% / 100% | 30,0 / 0,2 |
| `20-invoice` | 3 / 3 / 3 | 15,6 / 51,4 / 37,9 | 22 / 66 | 32 / 95 | 29,6 / 28,6 | 94% / 100% | 28,7 / 0,2 |
| `21-mixed-report` | 3 / 4 / 3 | 16,1 / 42,2 / 28,6 | 22 / 56 | 33 / 92 | 31,5 / 38,5 | 44% / 100% | 45,2 / 0,2 |
| `22-multicol-pagination` | 11 / 13 / 11 | 28,5 / 82,9 / 69,4 | 35 / 98 | 31 / 103 | 38,9 / 38,7 | 13% / 100% | 31,3 / 0,2 |
| `invoice-a-basic` | 1 / 1 / 1 | 11,2 / 26,9 / 13,5 | 17 / 39 | 26 / 72 | 25,6 / 25,0 | 98% / 100% | 16,9 / 0,2 |
| `invoice-d-140rows` | 7 / 7 / 7 | 35,5 / 77,5 / 64,2 | 42 / 93 | 40 / 102 | 55,7 / 51,1 | 88% / 100% | 53,2 / 0,2 |
| `esf-1` | 6 / 13 / 6 | 39,0 / 89,4 / 76,3 | 46 / 106 | 38 / 114 | 70,5 / 63,2 | 4% / 100% | 180,3 / 0,2 |
| `esf-4` | 16 / 31 / 16 | 83,6 / 178,8 / 165,5 | 90 / 198 | 53 / 162 | 140,8 / 123,7 | 2% / 100% | 222,2 / 0,2 |
| `md-gopdfgo-overview` | 11 / 11 / 11 | 33,2 / 102,0 / 88,4 | 40 / 117 | 42 / 116 | 90,9 / 118,3 | 50% / 100% | 329,6 / 0,2 |
| `md-gomdgo-testing` | 7 / 8 / 7 | 21,3 / 85,7 / 72,6 | 28 / 101 | 35 / 103 | 57,0 / 75,5 | 40% / 93% | 111,8 / 0,4 |
| `office-agreement` | 6 / 6 / 6 | 21,6 / 58,9 / 45,1 | 28 / 73 | 44 / 125 | 65,0 / 54,3 | 79% / 99% | 152,3 / 0,0 |
| `office-db-model` | 13 / 13 / 13 | 57,1 / 561,3 / 544,8 | 63 / 585 | 80 / 457 | 89,7 / 168,9 | 32% / 80% | 195,3 / 69,8 |
| `office-terminology` | 6 / 4 / 6 | 34,1 / 171,9 / 158,9 | 41 / 191 | 64 / 177 | 65,4 / 74,1 | 24% / 71% | 312,3 / 45,5 |
| `office-usb-protocol` | 10 / 8 / 10 | 24,6 / 90,6 / 77,7 | 31 / 107 | 65 / 141 | 99,5 / 89,5 | 77% / 100% | 103,2 / 0,0 |
| `mmd-p6` | 4 / 3 / 4 | 7,0 / 36,2 / 22,4 | 12 / 49 | 22 / 86 | 5,2 / 19,1 | 38% / 100% | 102,5 / 0,0 |
| `rand-01` | 17 / 19 / 15 | 58,1 / 286,5 / 272,4 | 65 / 306 | 46 / 154 | 105,3 / 116,7 | 5% / 34% | 95,7 / 0,0 |
| `rand-12` | 14 / 15 / 14 | 43,3 / 153,5 / 140,2 | 50 / 170 | 39 / 148 | 122,3 / 127,6 | 49% / 100% | 55,2 / 0,2 |
| `book-1342` | — / 309 / 254 | 461,7 / 1144,3 / 1130,5 | 480 / 1186 | 285 / 422 | 26744,6 / 26296,6 | — / — | — / — |

Podsumowanie:
- **Szybkość.** folio jest w procesie szybsze na każdym dokumencie: mediana stosunku czasów gopdfgo/folio
  2,9 (zakres 1,0–9,8), z ciepłą `FontCache` 1,9. Najbliżej są dokumenty z dużymi tabelami
  (`02-register-table` 73 wobec 64 ms) i SVG (`03-report-svg-chart` 30 wobec 30 ms); z `FontCache` gopdfgo jest szybsze na obu (60 i 17 ms); najdalej dokumenty z
  tysiącami atrybutów `style` (`office-db-model` 9,8 raza, `office-terminology` 5 razy) i dokumenty losowe
  (`rand-01` 4,9 raza). Cały proces: folio 12–90 ms, gopdfgo 37–585 ms; książka 0,48 wobec 1,19 s.
- **Pamięć.** RSS folio 22–80 MB, gopdfgo 72–457 MB (książka 285 wobec 422 MB).
- **Wierność.** gopdfgo ma liczbę stron Chromium w 33 z 35 dokumentów, folio w 17; słowa wzorca na tej
  samej stronie: gopdfgo 91,5%, folio 35,4%; mediana odchyleń pionowych gopdfgo 0,2 pt, folio 55 pt.
  folio nie zgłasza żadnych ostrzeżeń (nieznane właściwości pomija po cichu) i nie miało awarii ani błędów.
- **Rażące różnice folio** (rastry stron w `bench/folio/out/base/png/cmp-*.png`): e-SF `esf-1` i `esf-4`
  na 13 i 31 stronach zamiast 6 i 16 (pismo w tabelach wyraźnie większe niż w Chromium); w SVG
  (`03-report-svg-chart`) polskie litery jako „?” albo „Å‚”, tekst SVG fontem Times zamiast Noto, linia
  wykresu narysowana jako wypełniony wielokąt, filtr poświaty jako koncentryczne okręgi; `21-mixed-report`
  bez obu elementów pływających po prawej i bez treści sekcji dwukolumnowej na stronie 2; diagram mermaid
  `mmd-p6` w 5 kB zamiast 19 kB (brak większości rysunku); faktura `20-invoice` bliska wzorcowi
  (94% słów na miejscu stron), ale z siatką nagłówka i odstępami innymi niż w Chromium. Książka: 309 stron
  wobec 254 (gopdfgo) i 253 (Chromium, § README).

### Skąd przewaga folio

Profil procesora i przydziałów obu silników na `20-invoice` (pprof, 60 i 200 renderowań, `bench/folio/prof/`),
gopdfgo w stanie `cf2de18`; udziały w czasie procesora wątku renderowania (ok. 39 ms z 58 ms ściennych; reszta
to odśmiecanie w tle, ok. 15% próbek):

| Etap | gopdfgo `cf2de18` | uwagi |
|---|---|---|
| parsowanie fontów `Options.Fonts` (`fontscan.AddFont`, 14 plików) | 21% | go-text parsuje cały font: glify, nazwy glifów, BASE, GSUB/GPOS |
| wczytanie fontów z `@font-face` (6 plików, te same co w `Options.Fonts`) | 14% | drugie parsowanie tych samych plików |
| układ (`makeAllPages`) | 56% | w tym szerokości min-/max-content tabeli 30%: kształtowanie HarfBuzz każdej komórki |
| rysowanie stron | 6% | |
| kaskada CSS i budowa drzewa pudełek | ok. 2% | bez wczytywania fontów |
| zapis PDF, podzbiory fontów, kompresja zlib | ok. 4% | |

Fonty to ok. 65% przydziałów (50 z 80 MB na wywołanie). folio czyta z fontu tylko surowe tablice i dekoduje
je przy użyciu, a jego własny czas procesora na wywołanie to ok. 2–3 ms: większość z 16 ms ściennych to
pobieranie pamięci od systemu (`madvise` w `mheap.allocSpan`; folio przydziela ok. 28 MB na wywołanie, z
tego 20% to nowe kompresory `flate` dla każdego strumienia, 15% tablica kerningu `GPOS` parsowana przy
każdym wywołaniu). Przewaga folio na małych dokumentach to więc w ok. połowie leniwe fonty, a w drugiej
połowie mniej pracy w układzie: jego układ nie kształtuje tekstu przez HarfBuzz przy liczeniu szerokości,
nie liczy osobno min- i max-content komórek, nie ma zaokrągleń Chromium ani ponownych prób podziału strony;
ta różnica przekłada się bezpośrednio na wierność z tabeli wyżej. Na `office-db-model` (13 stron, 43 tabele,
każdy element z atrybutem `style` z textutil) 31% czasu gopdfgo zabierało parsowanie atrybutów `style`
(w tym wyrażenie regularne liczb w tokenizerze ok. 10%), a ok. 22–37% to `madvise` przy wzroście sterty.

### Co przeniesione do gopdfgo

Wszystko bez zmiany wyniku: porównanie bajtowe W3C (11 689 dokumentów, 46 756 renderowań poleceniem z 67
fontami `testdata/fonts`) 0 różnic wobec `cf2de18`, 76 dokumentów `bench/realdocs` i książka 0 różnic.

1. **Pamięć podręczna fontów** (ADR 0045): `Options.FontCache` przechowuje sparsowane fonty między
   wywołaniami; bez niej renderowanie parsuje plik podany w `Options.Fonts` i w `@font-face` raz, nie dwa razy.
   Z folio wzięty tylko pomysł (koszt fontów nie powinien zależeć od liczby renderowań), bez kodu.
2. **Atrybuty `style`**: ten sam tekst atrybutu parsowany raz na renderowanie; liczby w tokenizerze CSS bez
   wyrażenia regularnego; `physicalName` z mapy wyliczonej raz; `AsciiLower` bez kopii dla napisów ASCII bez
   wielkich liter.
3. Wczytanie fontu z `@font-face` do bufora o znanej długości (bez wzrostu `io.ReadAll`).

Przed i po (w procesie, mediana; cały proces; RSS):

| Dokument | `cf2de18` [ms] | po zmianach [ms] | z `FontCache` [ms] | cały proces przed / po [ms] | RSS przed / po [MB] |
|---|---|---|---|---|---|
| `20-invoice` | 57,8 | 51,4 | 37,9 | 72 / 66 | 106 / 95 |
| `17-tables` | 31,2 | 25,8 | 12,5 | 46 / 39 | 88 / 76 |
| `02-register-table` | 78,2 | 73,2 | 59,9 | 96 / 88 | 128 / 116 |
| `22-multicol-pagination` | 85,0 | 82,9 | 69,4 | 100 / 98 | 106 / 103 |
| `invoice-a-basic` | 28,5 | 26,9 | 13,5 | 42 / 39 | 81 / 72 |
| `esf-4` (16 stron) | 184,4 | 178,8 | 165,5 | 205 / 198 | 170 / 162 |
| `office-agreement` | 93,9 | 58,9 | 45,1 | 112 / 73 | 190 / 125 |
| `office-usb-protocol` | 128,4 | 90,6 | 77,7 | 145 / 107 | 224 / 141 |
| `office-terminology` | 215,0 | 171,9 | 158,9 | 238 / 191 | 268 / 177 |
| `office-db-model` | 919,2 | 561,3 | 544,8 | 947 / 585 | 469 / 457 |
| książka (254 strony) | 1154 | 1144 | 1131 | 1190 / 1186 | 465 / 422 |

Na całym korpusie `testdata/documents` bez `FontCache` 2–20% szybciej (ok. 5,5 ms mniej na dokument: drugie
parsowanie fontów Noto), z `FontCache` mały dokument 35–60% szybciej (`17-tables` 31 → 12,5 ms).

### Co zostało (szacunki)

- **Leniwe parsowanie fontów** jak w folio (przy pierwszym renderowaniu i w poleceniu `gopdfgo`, gdzie
  `FontCache` nic nie daje): go-text parsuje cały font, a dokument używa kilku. Polecenie z `-fonts
  testdata/fonts` parsuje 67 plików. Szacunek: pierwsze renderowanie małego dokumentu −10–15 ms. Wymaga
  własnego indeksu fontów (rodzina, odmiana, pokrycie `cmap` z surowych tablic) i parsowania przy
  pierwszym użyciu, bo `fontscan.FontMap` przyjmuje tylko sparsowane twarze; go-text to zależność, nie fork.
- **Szerokości min-/max-content komórek tabel**: 30% czasu procesora faktury; każda komórka jest kształtowana
  przez HarfBuzz osobno dla min- i max-content (`inlineLineWidths` → `SplitFirstLine`). Pamięć szerokości słów
  według (tekst, styl) między obiema miarami i układem właściwym: szacunek −5–8 ms na fakturze, ryzyko
  średnie (zgodność zaokrągleń ADR 0044).
- **`isSupportedFont` w zapisie PDF** parsuje całą tablicę `glyf` tylko po to, by sprawdzić, że nie jest
  pusta (ok. 2 MB przydziałów na wywołanie); wystarczy `loca`. Szacunek < 1 ms.
- **Sterta**: na dużych dokumentach 20–37% czasu to `madvise` przy wzroście sterty (macOS); pomogłoby
  zmniejszenie przydziałów w kaskadzie (`newStyleFor`) i drzewie pudełek (`CopyWithChildren`). Bez szacunku.

### Druga runda: techniki folio, profil etapów i plan (2026-11-23)

Przeczytany kod folio (`ARCHITECTURE.md`, pakiety `html`, `layout`, `font`, `core`) w tym samym klonie
`29bca95`; bez przejmowania kodu, tylko pomysły. Zestawienie technik, które dają folio szybkość:

| Technika folio | Odpowiednik w gopdfgo | Uwagi |
|---|---|---|
| Indeks selektorów według najbardziej prawego selektora prostego (`html/css.go` `ruleIndex`: `byID`, `byClass`, `byTag`, reszta) | `tree.matcher.match` sprawdzał każdy selektor każdego arkusza przy każdym elemencie (arkusz domyślny ma kilkaset selektorów) | **wdrożone** (niżej) |
| Styl obliczony jako zwykła struktura o polach typowanych, liczony raz na element, dziedziczenie przez kopię struktury rodzica (`html/style.go` `computedStyle`) | Wartości obliczane na żądanie (`ComputedStyle.Get`), dwie warstwy: wspólna tablica `sharedValues` i nakładka `own`; `pr.Inherited.Has` przy każdym odczycie | nie przenosimy modelu (leniwe obliczanie jest potrzebne dla zależności między właściwościami i `var()`); **wdrożone**: tablica bitów zamiast mapy w `SetK`, indeks dużej nakładki, wspólne kaskady dla deklaracji własnych |
| Pomiar tekstu bez kształtowania: suma szerokości glifów z `hmtx` i par `kern`/`GPOS` (`font.EmbeddedFont.MeasureString`) | HarfBuzz (go-text) przy każdym `SplitFirstLine`, także przy min-/max-content | nie do przeniesienia bez utraty wierności (ligatury, znaki łączone, pisma złożone, ADR 0044); folio na tej różnicy traci zgodność z Chromium (§ 11) |
| min-/max-content akapitu: najdłuższe słowo i suma słów, każde słowo mierzone raz, wynik zapamiętany w akapicie (`layout/paragraph_measure.go`) | min-content komórki: `SplitFirstLine` z szerokością 0 dla każdego słowa, a dla tekstu do 128 znaków kształtowana jest cała reszta tekstu (O(słowa²)); szerokości tabeli zapamiętane na tabelę (`context.tables`) | plan, pkt 1 |
| Szerokości kolumn tabeli zapamiętane dla danej szerokości dostępnej (`layout/table.go` `resolveColWidths`) | jest (`context.tables`) | bez zmian |
| Font: tablice metryczne dekodowane od razu własnym parserem, `glyf`, `GSUB`, `GPOS`, `kern`, `CFF` jako surowe bajty dekodowane przy pierwszym użyciu (`font/truetype.go`, `sync.Once`) | go-text parsuje cały font (`font.NewFont`), także `glyf` w `isSupportedFont` przy zapisie | plan, pkt 3–4 |
| Jeden przebieg układu bez ponownych prób podziału strony; plan układu niezmienny (`LayoutPlan`) | `remakePage`/`makePage` z ponownymi próbami, przebieg drugi dla liczników stron (ADR 0036) | przebieg drugi: plan w ADR 0036, ostatnia sekcja |
| Kompresja: nowy `zlib.Writer` na każdy strumień | pula kompresorów (`model.Compress`, już jest) | gopdfgo tu lepsze |
| Zapis PDF strumieniem bez buforów całego dokumentu | `goweasyprint/pdf` buduje model i zapisuje na końcu; 4–9% czasu | bez zmian (mały udział) |

Profil po zmianach z tej rundy (pprof, `-cache`, ciepłe renderowania, udział w próbkach procesora całego
procesu; „kaskada” to `GetAllComputedStyles`, „pudełka” `BuildFormattingStructure`, „układ” `makeAllPages`,
w tym min-/max-content tabel; „rysowanie” `drawPage`; „zapis” finalizacja stron i `Output.Finalize`;
`madvise` to pobieranie pamięci od systemu, GC to odśmiecanie w tle):

| Etap (udział w czasie procesora `render`) | `office-db-model` przed / po | `rand-01` przed / po | `20-invoice` przed / po | książka przed / po |
|---|---|---|---|---|
| kaskada CSS | 56% / 33% | 8% / 5% | 8% / 9% | 4% / 2% |
| drzewo pudełek | 11% / 18% | 6% / 6% | 7% / 1% | 7% / 8% |
| układ | 31% / 45% | 86% / 89% | 78% / 83% | 87% / 88% |
| — w tym min-/max-content tabel | 9% / 15% | 21% / 27% | 40% / 43% | 1% / 1% |
| — w tym `SplitFirstLine` (kształtowanie i łamanie) | 8% / 17% | 55% / 54% | 50% / 56% | 58% / 55% |
| rysowanie stron | 3% / 5% | 7% / 8% | 8% / 9% | 7% / 9% |
| zapis PDF i kompresja | 2% / 6% | 5% / 7% | 7% / 5% | 14% / 20% |
| `madvise` (udział we wszystkich próbkach) | 30% / 24% | 4% / 6% | 2% / 2% | 15% / 10% |

Czas procesora `render` na renderowanie po zmianach (profil, maszyna obciążona innymi procesami, więc liczby
są orientacyjne): `office-db-model` ok. 260 ms, `rand-01` 270 ms, `20-invoice` 43 ms, książka 830 ms. Po
zmianach we wszystkich czterech dokumentach (poza `office-db-model`, gdzie zostały jeszcze kaskada i `madvise`)
ponad 80% czasu to układ, a w nim połowa to `SplitFirstLine`: kształtowanie HarfBuzz i łamanie wierszy.
To jest główna różnica wobec folio, które tekstu nie kształtuje (zob. tabela technik).

Gdzie gopdfgo traci najwięcej wobec folio (folio na tych dokumentach: 57, 58, 16 ms i 0,46 s):
- `office-db-model` (43 tabele, 4144 komórki z `valign` i `cellpadding`): kaskada była ok. 30% czasu, z tego
  dopasowanie selektorów 40% i walidacja tych samych atrybutów prezentacyjnych przy każdej komórce 30%;
  reszta to `madvise` przy wzroście sterty. Po zmianach kaskada to ok. 33% krótszego czasu.
- `rand-01`: układ ok. 70% (wiersze i elementy pływające w tabelach, ponowne próby podziału strony);
  odczyt stylów (`propsCache.get`, `SetK.Has`) był ok. 14% próbek.
- `20-invoice`: min-/max-content komórek ok. 30% (kształtowanie każdej reszty tekstu komórki).
- książka: układ 59%, kompresja zlib 9%, `madvise` 15%.

Wdrożone w tej rundzie (wszystko bez zmiany wyniku, opis zmian w `THIRD_PARTY.md`):
1. **Indeks selektorów** (`css/selector/index.go`, `tree.matcherIndex`): kubełki według identyfikatora,
   klasy albo nazwy elementu z najbardziej prawego selektora złożonego; kandydaci sortowani według pozycji w
   arkuszu, więc kolejność dopasowań jest ta sama. Test `TestMatcherIndexSameAsLinear` porównuje z
   dopasowaniem liniowym na arkuszach domyślnych i arkuszach korpusu.
2. **Wspólne kaskady elementów z deklaracjami własnymi**: ten sam tekst atrybutu `style` i atrybutów
   prezentacyjnych (w tej samej kolejności i szczególności) plus te same reguły → jedna kaskada i jedna tablica
   wartości obliczonych; walidacja raz na tekst, ostrzeżenia powtarzane (porównane: 4732 wiersze ostrzeżeń na
   `bench/realdocs` i `testdata/documents` identyczne).
3. **Odczyt stylów**: `SetK` jako tablica bitów, indeks nakładki `own` od 16 wartości, `sharedValues` od razu
   na wszystkie właściwości.

Przed i po (w procesie, mediana; `-cache` to `Options.FontCache`; cały proces: mediana z 5, RSS: szczyt;
binarki i harness z `65c52c3` i z tej gałęzi, mierzone na zmianę w tej samej chwili):

| Dokument | `65c52c3` [ms] | po [ms] | z `FontCache` przed / po [ms] | cały proces przed / po [ms] | RSS przed / po [MB] |
|---|---|---|---|---|---|
| `20-invoice` | 52,4 | 50,0 | 38,5 / 35,8 | 66 / 63 | 95 / 96 |
| `17-tables` | 26,1 | 25,7 | 12,6 / 12,0 | 39 / 39 | 75 / 74 |
| `02-register-table` | 73,9 | 66,9 | 60,8 / 53,1 | 89 / 83 | 113 / 115 |
| `22-multicol-pagination` | 84,3 | 76,7 | 70,8 / 62,9 | 100 / 92 | 100 / 98 |
| `invoice-a-basic` | 27,2 | 26,6 | 13,6 / 12,9 | 40 / 40 | 74 / 72 |
| `esf-4` (16 stron) | 181,8 | 140,8 | 168,5 / 128,8 | 204 / 159 | 154 / 142 |
| `office-agreement` | 59,9 | 57,8 | 46,1 / 43,9 | 74 / 72 | 127 / 127 |
| `office-usb-protocol` | 92,1 | 83,4 | 78,6 / 69,5 | 109 / 100 | 152 / 142 |
| `office-terminology` | 175,0 | 167,9 | 161,5 / 146,9 | 193 / 180 | 177 / 158 |
| `office-db-model` | 785,7 | 340,3 | 574,5 / 464,3 | 618 / 483 | 437 / 247 |
| `rand-01` | 308,6 | 281,7 | 324,4 / 267,9 | 327 / 301 | 160 / 154 |
| `rand-12` | 167,1 | 152,8 | 153,8 / 138,3 | 189 / 172 | 142 / 136 |

Pomiar z 2026-11-23 ok. 10:05 na maszynie, na której równolegle szły testy innego pomocnika (obciążenie 6–60),
więc czasy dużych dokumentów skaczą między powtórzeniami o kilkadziesiąt procent. Dla nich ważniejszy jest
stosunek z renderowań na zmianę (stara, nowa, stara, nowa; po 5 par, mediana stosunków po/przed):
`office-db-model` 0,55 (−45%), `office-terminology` 0,79 (−21%), `rand-01` 0,87 (−13%), książka ok. 0,94
(−6%; rozrzut większy niż efekt). Dokumenty bez atrybutów prezentacyjnych zyskują 2–15% (indeks selektorów i
odczyt stylów), e-SF z dużymi tabelami 23%, `office-db-model` prawie połowę, a jego RSS spada z 437 do 247 MB.
Liczby w tabeli porównania z folio wyżej i w `docs/comparison.md` są sprzed tej rundy; do odświeżenia
pomiarem na wolnej maszynie (`bench/folio/run.py`).

Plan reszty (szacunki; kolejność według stosunku zysku do ryzyka):

| # | Technika (folio) | Odpowiednik u nas | Szacunek zysku | Ryzyko dla wierności | Koszt |
|---|---|---|---|---|---|
| 1 | min-content z każdego słowa mierzonego raz | `inlineLineWidthsHang` przy `minimum`: zamiast `SplitFirstLine(reszta, 0)` dla każdego słowa jedno kształtowanie tekstu i odczyt szerokości słów z podziału na wiersze przy szerokości 0 | faktura −5–8 ms (30% → ok. 15%), `rand-01` −10–20% | średnie: szerokość słowa ukształtowanego w całości może różnić się od ukształtowanego osobno (kerning przez spację, ligatury przez granicę, zaokrąglenia ADR 0044); wymaga porównania bajtowego, a przy różnicach rozstrzygnięcia według CSS Text 3 § 5 (min-content = najszersza jednostka niełamliwa w kontekście akapitu) | 2–3 dni |
| 2 | — (u nas: ta sama treść w obu miarach) | wspólna pamięć szerokości wierszy/słów dla min-, max-content i układu właściwego według (tekst, styl, szerokość) już istnieje w `layoutCache` dla `wrap`; brakuje jej dla wyniku `splitFirstLineRaw` (kroki 3–4: łamanie słowa, dzielenie wyrazów) | −3–5% w dokumentach z tabelami | małe (czysta pamięć wyniku funkcji czystej; trzeba ująć w kluczu `isLineStart`, `minimum`, słownik dzielenia) | 1 dzień |
| 3 | leniwe tablice fontu | własny indeks fontów (rodzina, odmiana, pokrycie `cmap` z surowych tablic), parsowanie go-text przy pierwszym użyciu; pierwsze renderowanie i polecenie `gopdfgo` | pierwsze renderowanie małego dokumentu −10–15 ms; polecenie z `-fonts testdata/fonts` (67 plików) więcej | małe, jeśli wybór fontu daje te same twarze (porównanie bajtowe); go-text to zależność, nie fork | 3–4 dni |
| 4 | `glyf` dekodowany przy użyciu | `isSupportedFont`: wynik zapamiętany dla treści fontu (w `FontCache`) zamiast parsowania całej `glyf` przy każdym zapisie | ok. 1 ms na renderowanie (3% faktury) | żadne przy pamięci wyniku; zastąpienie przez sprawdzenie samej `loca` zmieniłoby wynik dla fontów z uszkodzonym glifem | 0,5 dnia |
| 5 | mniej przydziałów, mniej `madvise` | drzewo pudełek: `CopyWithChildren` w `dispatchChildren` (rysowanie) i w podziale bloków kopiuje każde pudełko; `NewTextBox` przy budowie | `madvise` i GC to 20–35% czasu dużych dokumentów; −5–10% | małe przy kopiowaniu tylko zmienionych pudełek, ale `CopyWithChildren` zeruje też `RemoveDecorationSides` oryginału, więc każda zmiana wymaga ostrożności | 2 dni |
| 6 | jeden przebieg układu | tańszy przebieg drugi: ponowne użycie stron z pierwszego przebiegu (ADR 0036, ostatnia sekcja; pliki `layout/pages.go`, `layout.go`, `stream.go`, w których trwa praca nad przypisami) | książka ze spisem 2,5 → ok. 1,5 s | średnie (stan wejścia strony musi być pełny) | według ADR 0036 |
| 7 | kaskada bez atrybutów prezentacyjnych w pętli po tabeli | `findStyleAttributes`: `cellpadding` dodaje wpis dla każdej komórki każdej tabeli przodka (także komórek tabel zagnieżdżonych, TODO ze źródła); ok. 10% przydziałów `office-db-model` | −2–3% na takich dokumentach | zmiana zachowania dla tabel zagnieżdżonych (HTML § 15.3.10: `cellpadding` dotyczy komórek tej tabeli) — osobna decyzja, zmienia wynik | 0,5 dnia |

Pozycje 1–5 nie dotykają plików przebiegu drugiego. Przebieg drugi powtarza układ, więc przyspieszenie układu
(pkt 1, 2, 5) działa w nim drugi raz i skraca też dokumenty ze spisem treści; pkt 6 warto mierzyć po nich.

### Pamięć wyniku podziału wiersza i kopie pudełek: plan pkt 2 i 5 (2026-11-23)

Profil wyjściowy (`c2d6fb1`, pprof, `-cache`, ciepłe renderowania): `splitFirstLine` 35% czasu procesora
faktury, `rand-01` i książki, w tym `WrapWordBreak` 32%, prawie w całości chybienia (segmentacja, kształtowanie
HarfBuzz, `LineWrap`); trafienia w pamięć z ADR 0032 ok. 2%. `CopyWithChildren` to 5–8% przydzielanych bajtów
(pudełko ma ok. 1 kB: `BoxFields` 1024 B, `LineBox` 1096 B), bez widocznego udziału w czasie procesora; z tego
w rysowaniu (`dispatchChildren`) 28% (`rand-01`) do 50% (`office-db-model`), reszta to kopie w układzie
(`splitInlineBox` 20–50%, `blockContainerLayout` 17–28%, tabele).

**Pkt 2, pamięć wyniku `splitFirstLine` — wdrożone (ADR 0046).** Sonda: 16–84% wywołań powtarza argumenty
(tekst, styl, szerokość, `minimum`, `isLineStart`), ale powtórzenie kosztowało tylko 0,5–2 µs, bo
kształtowanie było już w pamięci; czas wszystkich powtórzeń to 1–8% renderowania (`rand-01` 21 ms z ok. 250).
Wynik trafia do tej samej pamięci co wyniki `WrapWordBreak` (pole `split` w kluczu), tylko dla pomiarów
min-/max-content (`minimum`, szerokość 0 albo bez szerokości) i tekstów do 128 znaków; wariant ze wszystkimi
wywołaniami był w książce i `esf-4` o 0,6–1% wolniejszy, bez progu długości książka o 1,4% (klucz z całej
reszty akapitu). Styl serializowany raz na podział zamiast przy każdym wewnętrznym łamaniu.

Przed i po (`c2d6fb1` i ta gałąź, harness z `bench/folio/gharness` w dwóch kopiach, mierzone na zmianę: 5 par
w procesie, mediana z 10–20 ciepłych renderowań, 3 przy książce, 1 przy ×4 ze spisem; cały proces: mediana z
5 uruchomień, RSS: szczyt z `os.wait4`; 2026-11-23 12:45–13:10, obciążenie 3–20, więc ważniejszy jest stosunek):

| Dokument | w procesie z `FontCache` przed / po [ms] | stosunek | bez `FontCache` przed / po [ms] | stosunek | cały proces przed / po [ms] | RSS przed / po [MB] |
|---|---|---|---|---|---|---|
| `20-invoice` | 35,5 / 35,3 | 0,989 | 48,7 / 48,5 | 0,992 | 64 / 64 | 96 / 96 |
| `02-register-table` | 52,3 / 52,4 | 1,001 | 65,8 / 65,7 | 1,002 | 87 / 85 | 116 / 118 |
| `esf-4` | 126,0 / 127,3 | 1,010 | 139,1 / 139,8 | 1,005 | 163 / 164 | 147 / 146 |
| `office-db-model` | 296,4 / 294,6 | 0,989 | 309,7 / 308,3 | 0,995 | 339 / 331 | 258 / 249 |
| `office-terminology` | 127,5 / 122,2 | 0,958 | 141,8 / 136,7 | 0,965 | 160 / 159 | 161 / 163 |
| `rand-01` | 245,8 / 237,1 | 0,965 | 261,1 / 251,3 | 0,966 | 279 / 276 | 168 / 171 |
| `rand-12` | 127,0 / 127,0 | 0,998 | 141,2 / 140,6 | 0,994 | 159 / 159 | 143 / 148 |
| książka (254 strony) | 1085 / 1087 | 1,001 | 1099 / 1108 | 1,004 | 1173 / 1160 | 464 / 466 |
| książka ×4 ze spisem (`x4-toc.htm`) | 4987 / 4931 | 0,984 | — | — | 5119 / 5019 | 875 / 880 |

Zysk 3,5–4% w dokumentach z wieloma krótkimi tekstami w tabelach (`rand-01`, `office-terminology`), 1–1,5% w
fakturze, `office-db-model` i ×4 ze spisem, pozostałe bez zmiany w granicach rozrzutu (`esf-4` +0,5–1% we
wszystkich pomiarach, przyczyny nie ustaliłam). Szacunek z planu (−3–5% w dokumentach z tabelami) sprawdził się
tylko dla dokumentów z wieloma powtórzeniami min-content.

**Pkt 5, mniej kopii pudełek — pominięte.** Kopie w układzie (`splitInlineBox`, `blockContainerLayout`,
tabele) są potrzebne: pudełko źródłowe zostaje wzorcem dla ponownych prób podziału strony, stron odłożonych i
przebiegu drugiego (ADR 0036), a układ zmienia położenie i wymiary kopii. Kandydatem były tylko kopie w
rysowaniu: `dispatchChildren` kopiuje każdego rodzica, także gdy lista dzieci się nie zmieniła. Próba (bez
kopii, gdy dzieci są te same; bajty 7 dokumentów z § 11 bez zmian): `office-db-model` 0,980, `rand-01` 1,000,
faktura 0,997, książka 1,000 (stosunek po/przed, 4 pary). Zysk do 2% tylko w jednym dokumencie, a zmiana
usuwa dwie własności, na których może polegać reszta kodu: kopia zeruje `RemoveDecorationSides` oryginału
(dalsze użycie oryginału widzi czyste boki) i daje każdej stronie własne pudełka (pudełka `fixed` i
zastępowane bywają wspólne dla wielu stron, a `blocksAndCells` jest kluczowane pudełkiem). Przy takim zysku
ryzyko nie jest opłacalne. Udział kopii w czasie da się zmniejszyć raczej mniejszym pudełkiem (1 kB na
pudełko) niż mniejszą liczbą kopii; bez szacunku.

Sprawdzenie: porównanie bajtowe W3C (11 689 dokumentów, 46 756 renderowań) 0 różnic, `bench/verify/corpus.sh`
fail=0, `bench/realdocs` i książka 77 z 77 zgodnych, cztery warianty książki identyczne, `go test ./tests/`
bez błędów, testy przejętego kodu tylko ze znanymi niepowodzeniami.

### Ponowny pomiar 2026-11-24 (main `5961ab8`)

Ten sam sposób pomiaru co wyżej (`bench/folio/run.py`, wyniki `bench/folio/out/2026-11-24/`), maszyna wolna
(średnie obciążenie ok. 3), po dwóch rundach przyspieszeń (indeks selektorów i wspólna kaskada, pamięć
wyników łamania wierszy, ADR 0046) i tańszym drugim przebiegu (ADR 0036). Kolumna z `FontCache` nie była
mierzona. Mediana stosunku czasów gopdfgo/folio w procesie: 2,8 (było 2,9), zakres 1,0–5,4 (było 1,0–9,8);
`office-db-model` 309 wobec 58 ms (było 561), książka 1,10 wobec 0,46 s.

| Dokument | Strony Chromium / folio / gopdfgo | W procesie [ms] folio / gopdfgo / gopdfgo z `FontCache` | Cały proces [ms] folio / gopdfgo | RSS [MB] folio / gopdfgo | PDF [kB] folio / gopdfgo | Słowa na miejscu folio / gopdfgo | dy mediana [pt] folio / gopdfgo |
|---|---|---|---|---|---|---|---|
| `01-record-copy` | 3 / 2 / 3 | 19,2 / 40,9 / — | 26 / 55 | 29 / 89 | 35,7 / 28,2 | 48% / 100% | 36,7 / 0,2 |
| `02-register-table` | 8 / 7 / 8 | 64,4 / 66,6 / — | 71 / 81 | 52 / 111 | 111,9 / 81,3 | 15% / 100% | 116,0 / 0,2 |
| `03-report-svg-chart` | 2 / 2 / 2 | 30,2 / 29,9 / — | 36 / 44 | 31 / 83 | 57,9 / 27,1 | 81% / 100% | 18,8 / 0,2 |
| `04-qms-procedure` | 10 / 11 / 10 | 18,4 / 54,2 / — | 25 / 67 | 28 / 101 | 49,5 / 55,3 | 1% / 98% | 114,7 / 0,2 |
| `05-hyphenation` | 1 / 1 / 1 | 8,2 / 36,2 / — | 14 / 58 | 25 / 92 | 12,5 / 12,8 | 60% / 93% | 44,7 / 0,1 |
| `06-margin-boxes` | 2 / 1 / 3 | 8,4 / 24,3 / — | 14 / 37 | 25 / 76 | 14,0 / 18,6 | 30% / 47% | 9,8 / 0,2 |
| `07-images` | 6 / 6 / 6 | 13,3 / 25,6 / — | 19 / 39 | 24 / 83 | 62,0 / 42,7 | 94% / 100% | 94,7 / 0,2 |
| `08-flex` | 2 / 2 / 2 | 9,1 / 25,2 / — | 15 / 38 | 25 / 75 | 15,9 / 14,6 | 99% / 100% | 7,5 / 0,2 |
| `09-lists-counters` | 1 / 1 / 1 | 8,6 / 27,9 / — | 14 / 41 | 24 / 77 | 14,1 / 19,5 | 80% / 99% | 21,1 / 0,2 |
| `10-preformatted` | 1 / 1 / 1 | 9,0 / 29,7 / — | 15 / 43 | 25 / 78 | 14,4 / 17,2 | 68% / 98% | 130,9 / 0,1 |
| `11-edge-cases` | 2 / 4 / 2 | 15,9 / 31,5 / — | 22 / 44 | 32 / 82 | 23,4 / 23,1 | 75% / 99% | 232,7 / 0,1 |
| `12-floats-positioning` | 2 / 4 / 2 | 9,3 / 25,7 / — | 15 / 38 | 24 / 76 | 19,5 / 18,7 | 61% / 100% | 45,4 / 0,2 |
| `13-forms-html-elements` | 1 / 1 / 1 | 12,4 / 29,6 / — | 18 / 42 | 27 / 80 | 21,6 / 27,2 | 81% / 98% | 49,8 / 1,1 |
| `14-cascade-selectors` | 1 / 1 / 1 | 9,1 / 26,8 / — | 15 / 40 | 24 / 75 | 14,6 / 13,8 | 97% / 100% | 63,2 / 0,2 |
| `15-page-breaks` | 13 / 12 / 13 | 19,4 / 26,9 / — | 26 / 41 | 27 / 85 | 24,8 / 49,7 | 11% / 100% | 4,1 / 0,2 |
| `16-typography-fonts` | 1 / 1 / 1 | 22,7 / 35,3 / — | 29 / 47 | 37 / 90 | 38,9 / 46,9 | 96% / 97% | 10,8 / 0,2 |
| `17-tables` | 2 / 2 / 2 | 9,3 / 25,2 / — | 15 / 39 | 24 / 76 | 16,4 / 15,6 | 87% / 100% | 75,7 / 0,2 |
| `18-toc-running-elements` | 6 / 6 / 6 | 11,3 / 34,0 / — | 17 / 47 | 27 / 86 | 25,4 / 35,9 | 100% / 100% | 6,6 / 0,2 |
| `19-flex-grid-pagination` | 13 / 12 / 13 | 18,3 / 75,6 / — | 25 / 89 | 33 / 101 | 38,9 / 37,3 | 65% / 100% | 30,0 / 0,2 |
| `20-invoice` | 3 / 3 / 3 | 15,6 / 48,7 / — | 22 / 62 | 30 / 94 | 29,6 / 28,6 | 94% / 100% | 28,7 / 0,2 |
| `21-mixed-report` | 3 / 4 / 3 | 16,2 / 41,4 / — | 22 / 55 | 33 / 93 | 31,5 / 38,5 | 44% / 100% | 45,2 / 0,2 |
| `22-multicol-pagination` | 11 / 13 / 11 | 28,8 / 76,6 / — | 35 / 92 | 32 / 102 | 38,9 / 38,7 | 13% / 100% | 31,3 / 0,2 |
| `invoice-a-basic` | 1 / 1 / 1 | 11,3 / 26,7 / — | 17 / 39 | 25 / 73 | 25,6 / 25,0 | 98% / 100% | 16,9 / 0,2 |
| `invoice-d-140rows` | 7 / 7 / 7 | 35,9 / 67,0 / — | 42 / 83 | 36 / 107 | 55,7 / 51,1 | 88% / 100% | 53,2 / 0,2 |
| `esf-1` | 6 / 13 / 6 | 39,9 / 74,4 / — | 47 / 90 | 40 / 109 | 70,5 / 63,2 | 4% / 100% | 180,3 / 0,2 |
| `esf-4` | 16 / 31 / 16 | 84,8 / 140,6 / — | 91 / 159 | 55 / 140 | 140,8 / 123,7 | 2% / 100% | 222,2 / 0,2 |
| `md-gopdfgo-overview` | 11 / 11 / 11 | 33,2 / 96,4 / — | 40 / 112 | 41 / 117 | 90,9 / 118,3 | 50% / 100% | 329,6 / 0,2 |
| `md-gomdgo-testing` | 7 / 8 / 7 | 21,7 / 82,1 / — | 28 / 96 | 34 / 103 | 57,0 / 75,5 | 40% / 93% | 111,8 / 0,4 |
| `office-agreement` | 6 / 6 / 6 | 21,8 / 57,8 / — | 28 / 72 | 45 / 125 | 65,0 / 54,3 | 79% / 99% | 152,3 / 0,0 |
| `office-db-model` | 13 / 13 / 13 | 57,7 / 308,6 / — | 65 / 349 | 81 / 255 | 89,7 / 168,9 | 32% / 80% | 195,3 / 69,8 |
| `office-terminology` | 6 / 4 / 6 | 36,0 / 136,6 / — | 41 / 154 | 64 / 166 | 65,4 / 74,1 | 24% / 71% | 312,3 / 45,5 |
| `office-usb-protocol` | 10 / 8 / 10 | 25,0 / 82,5 / — | 31 / 98 | 65 / 138 | 99,5 / 89,5 | 77% / 100% | 103,2 / 0,0 |
| `mmd-p6` | 4 / 3 / 4 | 7,0 / 36,0 / — | 12 / 48 | 22 / 85 | 5,2 / 19,1 | 38% / 100% | 102,5 / 0,0 |
| `rand-01` | 17 / 19 / 15 | 59,6 / 253,3 / — | 65 / 276 | 54 / 167 | 105,3 / 116,7 | 5% / 34% | 95,7 / 0,0 |
| `rand-12` | 14 / 15 / 14 | 44,0 / 143,4 / — | 51 / 159 | 40 / 142 | 122,3 / 127,6 | 49% / 100% | 55,2 / 0,2 |
| `book-1342` | — / 309 / 254 | 463,7 / 1103,3 / — | 483 / 1137 | 281 / 469 | 26744,6 / 26296,6 | — / — | — / — |

mediana gopdfgo/folio w procesie: 2,77, zakres 0,99–5,35
strony zgodne z Chromium: folio 17, gopdfgo 33 z 35
folio: słowa wzorca dopasowane na tej samej stronie 20898 z 59068 (35.4%), mediana dy median 55.24
gopdfgo: słowa wzorca dopasowane na tej samej stronie 54045 z 59068 (91.5%), mediana dy median 0.19
