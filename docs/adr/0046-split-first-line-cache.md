# ADR 0046: Pamięć wyniku podziału pierwszego wiersza (`splitFirstLine`) w pamięci łamania wierszy

Status: przyjęta 2026-11-23.

## Kontekst

Plan z `docs/benchmark.md` § 11 („Druga runda”, pkt 2): w układzie ok. połowa czasu to `SplitFirstLine`
(kształtowanie HarfBuzz i łamanie). Pamięć z ADR 0032 trzyma wyniki `WrapWordBreak` (łamanie przy danej
szerokości), ale nie wynik całego `splitFirstLineRaw` (kroki 1–5: krótki tekst, dołożenie słowa z drugiego
wiersza, dzielenie wyrazów, łamanie słowa).

Pomiar (2026-11-23, sonda poza repozytorium, pierwsze renderowanie z `FontCache`): wywołania `splitFirstLine`
z tymi samymi argumentami (tekst, styl, szerokość, `minimum`, `isLineStart`):

| dokument | wywołań | powtórzeń | czas powtórzeń / czas wszystkich wywołań |
|---|---|---|---|
| `20-invoice` | 2477 | 1292 | 1,7 / 17,1 ms |
| `02-register-table` | 1501 | 892 | 0,5 / 5,8 ms |
| `esf-4` | 2693 | 1238 | 1,4 / 40,6 ms |
| `office-db-model` | 19 914 | 16 744 | 7,8 / 38,9 ms |
| `office-terminology` | 8309 | 5023 | 10,0 / 62,2 ms |
| `rand-01` | 22 776 | 15 177 | 20,9 / 111,7 ms |
| `rand-12` | 8920 | 3485 | 4,9 / 58,6 ms |
| książka | 19 680 | 3145 | 7,0 / 616,0 ms |

Powtórzenia pochodzą z szerokości min- i max-content (komórki, elementy pływające, flex), z układu
właściwego tego samego tekstu i z ponownych prób podziału strony. Wewnętrzne wywołania `WrapWordBreak`
trafiały wtedy w pamięć, ale każde budowało klucz z tekstu i serializacji stylu, a kroki 3–5 liczyły się
od nowa. Samo kształtowanie powtórzonych tekstów było już zapamiętane, więc zysk jest ograniczony do tej
reszty.

## Decyzja

1. Wynik `splitFirstLine` (z przycięciem odstępu między literami na końcu wiersza) trafia do tej samej
   pamięci dwupokoleniowej co wyniki `WrapWordBreak` (ADR 0032), z tym samym limitem i szacunkiem kosztu.
   Klucz `textKey` ma pole `split`: zero dla wyników `WrapWordBreak`, dla `splitFirstLine` znacznik wpisu
   i flagi `minimum`, `isLineStart`, „bez szerokości” (`maxWidth == nil`). Osobnej pamięci nie ma.
2. Zapamiętywane są tylko pomiary szerokości wewnętrznych: wywołania z `minimum`, z szerokością 0 (min-content
   słowo po słowie) albo bez szerokości (max-content), i tylko teksty do 128 znaków (`splitCacheMaxText`).
   Podział wywołań według rodzaju (ta sama sonda; powtórzenia / wywołania): min-content przy szerokości 0
   `rand-01` 7856/10 158, `office-db-model` 9405/10 503, książka 615/2403; max-content `rand-01`
   1820/2480; wywołania układu przy danej szerokości `rand-01` 4435/8126, `esf-4` 1034/2260, książka
   1000/6775. Wariant z pamięcią także dla wywołań układu był w książce i w `esf-4` o 0,6–1% wolniejszy
   (koszt klucza i wpisu przy każdym chybieniu większy niż oszczędność na trafieniu: powtórzone wywołanie
   układu i tak trafiało w pamięć `WrapWordBreak`); bez progu długości książka była o 1,4% wolniejsza,
   bo klucz to cała pozostała treść akapitu (n²/2 znaków na akapit, jak w ADR 0032). Szerokość
   `pr.AutoF` (inna niż `nil` i `pr.Float`) nie jest zapamiętywana.
3. Wpis trzyma własny obiekt układu, wołający dostaje kopię (`privateLayout`), jak przy `WrapWordBreak`.
   Szacunek kosztu wpisu to klucz i stały narzut: glify są wspólne z wpisem `WrapWordBreak`, z którego wynik
   powstał.
4. `splitFirstLineRaw` liczy klucz stylu raz i przekazuje go do wewnętrznych wywołań łamania
   (`wrapWordBreak`).
5. `FontDescription.binary` rozdziela nazwy rodzin bajtem zerowym: rodziny `["ab", "c"]` i `["a", "bc"]`
   dawały ten sam klucz pamięci (błąd źródła, dotyczył też pamięci `WrapWordBreak` i proporcji fontu).

Wynik nie zależy od trafień: `splitFirstLineRaw` jest funkcją tekstu, pól stylu zawartych w `styleKey`,
szerokości i obu flag (słownik dzielenia wyrazów wynika z języka i limitów w stylu, `fc.residuals` po
każdym łamaniu jest zerowany), a mapy nie są przeglądane.

## Skutki

Bajty bez zmian wobec `c2d6fb1`: korpus W3C (11 689 dokumentów, 46 756 renderowań), `bench/verify/corpus.sh`
fail=0, `bench/realdocs` i książka (77 z 77), cztery warianty książki. Czas (w procesie, stosunek po/przed z
renderowań na zmianę, `docs/benchmark.md` § 11, dopisek „Pamięć wyniku podziału wiersza i kopie pudełek”):
`rand-01` 0,965, `office-terminology` 0,958, faktura 0,989, `office-db-model` 0,989, ×4 ze spisem 0,984,
pozostałe 0,998–1,010. Plan zakładał 3–5% w dokumentach z tabelami; tyle jest tylko tam, gdzie wiele
krótkich tekstów jest mierzonych wielokrotnie.
