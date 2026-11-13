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

## 7. Do zrobienia

- Przejrzeć 69 różnic bez ostrzeżeń; powtórzyć reftesty przez `gopdfgo.Render` zamiast programu w `bench/`.
- Dodać `vh`/`vw` i właściwości logiczne, powtórzyć przebieg, żeby oddzielić braki składni od błędów fragmentacji.
- folio i Chromium na tym samym programie jako punkty odniesienia.
