# ADR 0037: Szybkość dużych dokumentów: zbędne rozkodowanie glifów, pule zlib, równoległe zamykanie stron

Status: przyjęta 2026-11-21.

## Kontekst

Pomiar z 2026-11-21 (`docs/overview.md`, commit `a655fcd`): książka 256 stron 1,62 s, czterokrotność
(1021 stron) 6,53 s, czterokrotność ze spisem treści (dwa przebiegi układu, ADR 0036) 10,9 s; Chromium
4,54–4,69 s na 1021 stronach. Profil CPU i alokacji (`runtime/pprof` w tymczasowej kopii wiersza poleceń,
poza repozytorium) dla czterokrotności, 9,1 s próbek CPU, 11,2 GB alokacji:

| koszt | CPU | alokacje |
|---|---|---|
| łamanie wierszy (`WrapWordBreak`: kształtowanie, segmentacja, zawijanie go-text) | 24% | 15% |
| rysowanie glifów: `drawEmojiGotext` → `Face.GlyphData` rozkodowuje kontur każdego glifu | 10% | 48% |
| odśmiecanie (praca znacznika w tle) | 24% (na innych rdzeniach) | — |
| zerowanie świeżej pamięci i `madvise` przy pierwszym użyciu stron sterty | 18% | — |
| kompresja zlib przy `Finalize` (stopień 6) | 5% | — |
| nowy kompresor zlib dla każdego strumienia (`zlib.NewWriterLevel`, ok. 1,2 MB) | — | 17% |
| style pseudoelementów strony: pętla po całej mapie `cascadedStyles` przy każdej stronie | 2–3% | — |

Czas był w przybliżeniu liniowy (1,62 s × 4 ≈ 6,5 s). Jedyny koszt nadliniowy to ostatni wiersz: mapa
`cascadedStyles` dostaje wpisy każdej strony (typ strony zawiera jej numer), a `SetPageComputedStylesT`
przeglądała ją całą dla każdej strony; przy 1000 stronach kilkanaście milionów porównań, w czterokrotności
ze spisem treści 0,34 s.

Warunek: wynik bajt w bajt taki sam, pamięć szczytowa bez wzrostu (ADR 0027, 0032, 0036).

## Decyzja

1. `drawEmojiGotext` pyta `GlyphDataColor` i `GlyphDataBitmap` zamiast `GlyphData`. `GlyphData` sprawdza
   kolejno COLR, mapy bitowe, SVG i kontury, a obraz powstaje tylko dla mapy bitowej PNG, więc warunek
   „brak opisu w COLR i jest mapa bitowa” daje ten sam wynik bez rozkodowania konturu.
2. `SetPageComputedStylesT` liczy style pseudoelementów strony po selektorach reguł `@page` pasujących do
   strony. Wpisy pseudoelementów strony tworzy tylko `addPageDeclarations`, po jednym dla każdego
   pasującego selektora, więc zbiór jest ten sam; kolejność obliczeń w źródle była kolejnością mapy, czyli
   losowa, i nie wpływała na wynik.
3. Kompresory i dekompresory zlib z puli (`model.Compress`, `model.Decompress`, `sync.Pool`, osobna pula
   dla każdego stopnia). `zlib.Writer.Reset` i `zlib.Resetter.Reset` dają stan taki jak nowy obiekt.
4. `Output.Finalize` zamyka strumienie stron równolegle, w `GOMAXPROCS` wątkach. Strona rozpakowuje swoją
   treść odłożoną przez `Flush`, dopisuje resztę operacji i kompresuje całość; czyta tylko własny stan
   i zapisuje tylko własne pola, a kolejność stron w drzewie stron jest ustalona przed zamykaniem. Wynik
   nie zależy od kolejności ani od liczby wątków. W chwili zamykania w pamięci jest najwyżej tyle
   rozpakowanych stron, ile wątków (kilka MB).

## Rozważone i odłożone

- `GOGC` wyższy niż 100: czterokrotność 5,23 → 5,21 s przy szczycie pamięci 0,79 → 1,07 GB. Odrzucone
  (pamięć).
- Kształtowanie mniej razy. Każdy znak jest kształtowany średnio 2,5 raza (120 tys. wywołań
  `WrapWordBreak`, 19% trafień w pamięci wierszy, 7,1 mln znaków kształtowanych na ok. 2,8 mln w tekście):
  wiersz próbny z krótkiego tekstu (`shortTextHint`), potem krok 3 z `splitFirstLineRaw` (pierwszy wiersz
  z następnym słowem), potem następny wiersz od nowa. Ponowne użycie wyniku kształtowania dla podciągu
  może się różnić na styku (kontekst kształtowania, ligatury przez spację), więc wymaga osobnego
  rozpoznania; szacunek zysku 15–25% czasu łamania, czyli ok. 5–8% całości.
- Pamięć wierszy (ADR 0032) większa, żeby drugi przebieg ADR 0036 trafiał w wyniki pierwszego: przy
  limicie dla całego dokumentu pamięć rośnie o setki MB. Odrzucone.
- Zerowanie i `madvise` świeżej pamięci (sterta rośnie do ok. 0,65 GB, nowy fragment sterty jest na
  macOS oddawany do użytku wywołaniem `madvise` dla każdego zakresu stron) maleje tylko z mniejszym
  szczytem sterty: to praca nad pamięcią, nie nad czasem.

## Wyniki

Pomiar 2026-11-21, Apple M4, `/usr/bin/time -l`, po trzy przebiegi całego procesu, porównanie z commitem
`4da5240` (po `a655fcd` tylko dokumentacja). Na maszynie działały w tym czasie inne zadania (obciążenie 4–5),
więc liczby są przybliżone; porównania bazowej i nowej binarki szły na przemian.

| dokument | stron | czas, przed | czas, po | szczyt RSS, przed | szczyt RSS, po |
|---|---|---|---|---|---|
| książka | 256 | 1,61–1,66 s | 1,17–1,22 s | 439–443 MB | 429–448 MB |
| książka ze spisem treści | 259 | 2,71–2,74 s | 2,23–2,31 s | 468–490 MB | 453–474 MB |
| książka ×4 | 1021 | 6,47–6,49 s | 4,57–4,69 s | 740–824 MB | 747–793 MB |
| książka ×4 ze spisem treści | 1027 | 10,9 s | 8,6–8,8 s | 795–928 MB | 801–816 MB |
| `02-register-table` | | 0,11 s | 0,10 s | 142–144 MB | 125–132 MB |
| `04-qms-procedure` | | 0,09 s | 0,07–0,08 s | 126–135 MB | 114–118 MB |
| `21-mixed-report` | | 0,07 s | 0,06 s | 117–123 MB | 99–107 MB |

Alokacje czterokrotności 11,2 → 3,6 GB. Bajty PDF: korpus W3C (11 689 dokumentów, 46 756 renderowań),
68 dokumentów korpusu (`testdata/documents`, `testdata/svg`) i cztery warianty książki bez różnic.
