# ADR 0003: reguła porównania obrazów w reftestach; font Ahem

Status: przyjęta 2026-11-13 (decyzja użytkownika). Rozstrzyga pytanie 12 z `open-questions.md` i zastrzeżenie
o tolerancji z `../feature-audit.md`.

## Kontekst

Reftest W3C to para dokumentów: test i wzorzec, które mają wyglądać tak samo, choć są zbudowane inaczej (np. tło
zamiast obrazka, obramowanie zamiast wypełnienia, element pozycjonowany zamiast przepływu). Oba renderujemy
gopdfgo do PDF, rasteryzujemy `pdftoppm -r 96` (jeden piksel CSS = jeden piksel obrazu) i porównujemy piksele.

Do 2026-11-13 obowiązywała jedna miara: strony są zgodne, gdy najwyżej 20 pikseli różni się o więcej niż 40 na
kanał. Pomiar pomocniczy pokazał, że 366 z 3276 niezaliczonych testów sondażu ekranowego różni się wyłącznie
liniami grubości jednego piksela wzdłuż krawędzi kształtów.

Skąd te różnice. PDF jest wektorowy i gopdfgo zapisuje współrzędne dokładnie, bez dosuwania do siatki pikseli.
Pozycje bywają ułamkowe, bo wynikają z metryk fontu (wiersz tekstu ma np. 18,4 px), procentów i dzielenia
wolnego miejsca. Krawędź, która wypada w środku piksela, rasteryzator rysuje pikselem częściowo pokrytym, a jego
kolor zależy od tego, jak kształt zbudowano i co leży pod spodem:

- ten sam prostokąt narysowany jako wypełnienie, jako obramowanie, jako obraz i jako wzór tła daje na krawędzi
  inne pokrycie piksela (obrazy są próbkowane, wypełnienia wygładzane);
- gdy test przykrywa czerwony kształt zielonym o tej samej geometrii, a wzorzec rysuje sam zielony, na krawędzi
  częściowo pokrytego piksela w teście prześwituje czerwień, we wzorcu biel;
- dwa stykające się prostokąty i jeden prostokąt o sumie ich wymiarów różnią się szwem.

Sprawdzone na `css/CSS2/backgrounds/background-position-004.xht`: niebieski kwadrat ma w teście i we wzorcu te
same granice co do piksela, a 193 różniące się piksele leżą na dwóch jego krawędziach; wyłączenie wygładzania
(`pdftoppm -aa no -aaVector no`) nie zmienia tej liczby, bo obrazy są próbkowane niezależnie od wygładzania.

Przeglądarka nie ma tego kłopotu, bo przy malowaniu dosuwa krawędzie do pikseli urządzenia, a test i wzorzec
rasteryzuje tym samym kodem. Dla silnika PDF dosuwanie byłoby błędem: wynik trafia na urządzenia o innej
rozdzielczości (druk 600–2400 dpi, powiększenie na ekranie), gdzie zaokrąglenie do 1/96 cala byłoby widocznym
przesunięciem. Zgodność co do piksela wymagałaby więc albo psucia geometrii w PDF, albo własnego rasteryzatora
naśladującego przeglądarkę; żadne z nich nie służy celom projektu.

## Decyzja

1. Strona testu zgadza się ze stroną wzorca, gdy spełnia jeden z warunków:
   - **ściśle** (`pass`): najwyżej 20 pikseli różni się o więcej niż 40 na kanał;
   - **z dokładnością do krawędzi** (`pass-edge`): różniące się piksele nigdzie nie tworzą bloku 2×2, czyli leżą
     wyłącznie na liniach grubości jednego piksela.
2. Wynik `pass-edge` jest zapisywany osobno w `testdata/wpt/baseline.tsv` i liczony osobno w sondażu; każda
   podawana liczba zaliczeń rozróżnia obie miary.
3. Testy `mismatch` (wzorzec ma wyglądać inaczej) ocenia tylko miara ścisła.
4. Font Ahem zostaje tam, gdzie był: w `testdata/wpt/fonts/Ahem.ttf`. Testy W3C ładują go same regułą
   `@font-face` z `/fonts/ahem.css`, więc był używany od początku. Nie dodajemy go do `testdata/fonts`:
   próba z 2026-11-13 pokazała, że staje się wtedy fontem zastępczym dla znaków spoza Noto i psuje 7 testów
   (`quotes-*`, `bidi-*`). Licencja wg tabeli `name` w pliku: domena publiczna, a tam, gdzie prawo jej nie
   uznaje, CC0 1.0; font służy wyłącznie testom i nie trafia do binarki.

Kod: `reftest_test.go` (`onlyEdgeDifferences`, `resultPassEdge`).

## Dlaczego wolno tak założyć

Linia grubości jednego piksela przy 96 dpi to najwyżej 1/96 cala (0,26 mm) i dotyczy koloru piksela na granicy
kształtu, nie położenia treści. W PDF geometria obu dokumentów w takich przypadkach jest ta sama lub różni się
o ułamek piksela; na wydruku i przy powiększeniu różnica nie jest widoczna jako inny układ.

## Konsekwencje i ryzyko

- Reguła przepuszcza też prawdziwe błędy o dokładnie jeden piksel: kształt przesunięty albo powiększony o 1 px
  daje różnice grubości jednego piksela. Takie błędy trzeba łapać testami równoważności na liczbach (pozycje
  i rozmiary z układu), nie obrazem.
- Reguła nie przepuszcza przesunięć o dwa piksele i więcej, zgubionej treści, innego łamania wierszy ani innego
  koloru wypełnienia: wszystkie dają bloki 2×2.
- Cienkie elementy (linia włosowa, obramowanie 1 px), które są w teście, a brak ich we wzorcu, też dają różnicę
  grubości jednego piksela i przejdą. Dla testów obramowań i linii miarodajna pozostaje miara ścisła.
- Liczby sprzed 2026-11-13 w `../feature-audit.md` są w mierze ścisłej i bez fontu Ahem; nie porównywać ich
  wprost z późniejszymi.
