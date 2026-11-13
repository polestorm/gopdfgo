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
(limit 15 s), strona 800 × 600 px bez marginesów, `pdftoppm -r 40`, tolerancja jak w `reftest_test.go`. Skrypt
i wyniki poza repozytorium (katalog roboczy sesji).

Ogółem: **3042 zaliczone (33%)**, 5388 niezaliczonych, 672 paniki silnika (225 w teście, 447 we wzorcu),
27 przekroczeń czasu, 6 innych błędów.

| Obszar | Zaliczone | Paniki i przekroczenia czasu |
|---|---|---|
| CSS 2.1: listy | 149 / 153 (97%) | |
| CSS 2.1: efekty wizualne (`visufx`) | 47 / 49 (95%) | |
| CSS 2.1: treść generowana | 175 / 202 (86%) | 4 paniki |
| CSS 2.1: fonty | 129 / 159 (81%) | |
| CSS 2.1: tekst | 272 / 382 (71%) | 7 panik |
| CSS 2.1: składnia | 160 / 264 (60%) | |
| CSS 2.1: przepływ normalny | 361 / 697 (51%) | 1 panika |
| CSS 2.1: obramowania | 243 / 504 (48%) | |
| CSS 2.1: tabele | 95 / 249 (38%) | |
| CSS 2.1: float | 44 / 113 (38%) | |
| flexbox | 370 / 952 (38%) | 5 panik, 16 przekroczeń czasu |
| tabele (CSS 3) | 47 / 141 (33%) | |
| CSS 2.1: pozycjonowanie | 110 / 513 (21%) | |
| CSS 2.1: `clear` | 40 / 211 (18%) | 8 przekroczeń czasu |
| CSS 2.1: selektory | 78 / 468 (16%) | 224 paniki we wzorcu |
| CSS 2.1: tło | 52 / 336 (15%) | |
| kolumny | 51 / 400 (12%) | 3 paniki, 3 przekroczenia czasu |
| grid | 185 / 1593 (11%) | 383 paniki |
| CSS 2.1: marginesy i dopełnienia | 66 / 683 (9%) | |
| CSS 2.1: `z-index` | 0 / 29 (0%) | |

**Jak to czytać.** To dolna granica mocno zaniżona przez kilka wspólnych przyczyn, nie miara jakości układu:

- pierwsza wersja sondażu dała 2490 zaliczeń; jedna poprawka (zdejmowanie `<![CDATA[` z arkuszy w XHTML)
  podniosła wynik do 3042. Takich wspólnych przyczyn jest zapewne więcej: proste testy marginesów (9%)
  i `z-index` (0%) nie mogą zawodzić z powodu samego układu, skoro dokumenty korpusu z tymi funkcjami wychodzą
  poprawnie;
- reftest przechodzi tylko wtedy, gdy silnik poprawnie renderuje i test, i wzorzec; jeden błąd we wspólnym
  wzorcu odbiera setki zaliczeń (224 testy selektorów mają ten sam wzorzec, który kończy się paniką).

Wartość sondażu: pokazuje, gdzie jedna poprawka daje najwięcej.

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
| flexbox | 38% reftestów, 16 przekroczeń czasu, 17 oznaczonych braków, 90 testów źródłowych przechodzi; korpus (12 przypadków) poprawny | poprawić: najpierw przekroczenia czasu (zawieszenia), potem przegląd niezaliczonych |
| `clear` i float | 18% i 38%, 8 przekroczeń czasu | poprawić: zawieszenia najpierw |
| kolumny | 12%, 3 przekroczenia czasu | zostawić, po usunięciu zawieszeń; ocenić po znalezieniu wspólnych przyczyn |
| tabele | 33–38% | poprawić: druga po polach marginesowych luka w reftestach druku |
| selektory | 16%, ale 224 testy blokuje jedna panika | poprawić panikę zapisu fontu; potem ocenić |
| marginesy, pozycjonowanie, `z-index`, tło | 0–21% przy poprawnym korpusie | szukać wspólnej przyczyny (wzorce, obrazy, font Ahem); to najpewniej artefakty, nie błędy układu |
| przypisy dolne, `leader()` | 22 i 15 testów źródłowych przechodzi; brak danych z WPT | zostawić |
| słowniki dzielenia wyrazów (42 języki) | działają | zostawić wszystkie; dodać wybór języków przy kompilacji, gdy rozmiar będzie priorytetem |

## 6. Proponowana kolejność

1. Trzy przyczyny panik z pkt 3 (zapis fontu bez glifów, `inline-grid`, indeksy w grid).
2. 27 przekroczeń czasu: flex (16), `clear` (8), kolumny (3). Każde to zawieszenie silnika.
3. Powtórka sondażu i szukanie kolejnych wspólnych przyczyn (jak CDATA), zaczynając od obszarów 0–21%.
4. Dopiero potem decyzje o dokańczaniu algorytmów (grid, flex, tabele) na podstawie oczyszczonych liczb.
