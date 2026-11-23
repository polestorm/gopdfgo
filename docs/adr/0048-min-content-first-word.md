# ADR 0048: Szerokość min-content słowo po słowie bez kształtowania reszty tekstu

Status: przyjęta 2026-11-23.

## Kontekst

Plan z `docs/benchmark.md` § 11 („Druga runda”, pkt 1): min-/max-content komórek tabel to 15–43% czasu
układu. Szerokość min-content tekstu `html/layout/preferred.go` (`inlineLineWidthsHang`) liczy pętlą
`SplitFirstLine(reszta, szerokość 0)`, a `splitFirstLineRaw` w kroku 1 kształtuje całą resztę (dla tekstów
do 128 znaków bez skrótu `shortTextHint`), więc tekst z n słowami kosztował ok. n²/2 słów kształtowania.
Plan zakładał liczenie obu miar z jednego kształtowania całego tekstu, z szerokościami słów odczytanymi
z pozycji glifów.

Rozbiór kodu pokazał, że dzisiejsza szerokość słowa przy min-content nie pochodzi z kształtowania
reszty: przy zwijanych odstępach i słowie zakończonym spacją krok 3 kształtuje pierwsze słowo
(do pierwszego miejsca podziału, bez spacji) osobno (`wrap(nextWord, 0)`) i ten wynik zwraca. Z kroku 1
bierze tylko jedno: czy pierwszy wiersz reszty przy szerokości 0 ma szerokość dodatnią
(`Width − Hang > 0`); od tego zależy, czy krok 3 w ogóle szuka słowa.

**Kiedy jedno kształtowanie dałoby inny wynik.** Sonda (2026-11-23, poza repozytorium) porównała
szerokość pierwszego słowa w kontekście reszty (pierwszy wiersz z kroku 1) z szerokością słowa
ukształtowanego osobno na `bench/realdocs` i `testdata/documents`: na ok. 19 700 słów 60 różni się
o 0,14–0,66 px, wszystkie w Tinos/Times New Roman przy słowach kończących się wielką literą („T”, „V”,
„W”, „P”, „SMOKA”, „CURRENT_TIMESTAMP”): font ma kerning między literą a spacją (zob. ADR 0044, Arimo
„a T”), który w kontekście zwęża słowo. Do tego zaokrąglenie z przenoszeniem błędu (ADR 0044) liczy się od
początku przebiegu, więc szerokość w górę do 1/64 px tego samego słowa w środku tekstu może różnić się o
1/64 px od słowa osobno. Szerokości z jednego kształtowania zmieniłyby więc wynik (najpewniej w stronę
Chromium, który mierzy słowo w kontekście; tego nie mierzyliśmy).

## Decyzja

1. Wynik zostaje ten sam co dotąd: szerokość słowa ze słowa ukształtowanego osobno. Zmienia się tylko
   sposób rozstrzygnięcia warunku z kroku 1: `zeroWidthFirstWord` (`text/engine_gotext.go`) dla
   wywołań z szerokością 0 i tekstem do 128 znaków kształtuje samo pierwsze słowo i, gdy ma ono szerokość
   dodatnią, przechodzi od razu do kroku 4 (dzielenie wyrazów) z tym, co dałby krok 3. Reszty tekstu nie
   kształtuje.
2. Droga skrócona tylko wtedy, gdy szerokość pierwszego wiersza reszty jest dodatnia na pewno: odstępy
   zwijane (`normal`, `nowrap`, `pre-line`), od lewej do prawej, bez `letter-spacing` i `word-spacing`
   (mogą być ujemne i zmieniają szerokość na końcu wiersza), bez odstępów innych niż spacja (wiszą,
   zmieniając `Hang`), tekst zaczyna się od litery, cyfry, znaku przestankowego albo symbolu (glif
   pierwszego znaku jest w pierwszym wierszu), pierwsze miejsce podziału wypada po spacji (inaczej krok 3
   nie kształtuje słowa osobno) i słowo ma szerokość dodatnią. W pozostałych przypadkach (ostatnie słowo
   tekstu, `pre-wrap`, podział bez spacji w piśmie CJK, przy ukośniku albo dywizie) kroki 1–3 jak dotąd.
3. Przełącznik `text.ZeroWidthShortcut` (domyślnie włączony) istnieje tylko dla testu, który porównuje
   obie drogi (`tests/mincontent23_test.go`).

Jednego kształtowania z szerokościami słów z pozycji glifów nie wprowadzamy: zmienia wynik w fontach
z kerningiem ze spacją, a zysk ponad tę decyzję byłby mały (słowa i tak trafiają w pamięć łamania,
ADR 0032).

## Skutki

Sprawdzenie różnicowe: wersja z sondą liczyła przy każdym użyciu drogi skróconej także krok 1 i
porównywała warunek; na korpusie W3C (11 689 dokumentów), `bench/realdocs`, `testdata/documents` i
książce, w trybie stronicowanym i sondażu, 0 niezgodności. Bajty bez zmian wobec `505fabd` (zob.
`docs/benchmark.md` § 11, dopisek „min-content bez kształtowania reszty tekstu”). Udział drogi skróconej
w wywołaniach przy szerokości 0, które nie trafiły w pamięć z ADR 0046: faktura 423 z 557, `rand-01`
1589 z 2302, `office-db-model` 524 z 1098, `esf-4` 42 z 59; pozostałe to głównie ostatnie słowa tekstów
(jedno kształtowanie słowa, jak w drodze skróconej).

Czas: zob. `docs/benchmark.md` § 11.
