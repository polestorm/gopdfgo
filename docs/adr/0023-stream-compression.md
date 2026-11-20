# ADR 0023: kompresja strumieni PDF w stopniu 6 i przełącznik `Uncompressed`

Status: przyjęta 2026-11-20 (decyzja użytkownika: najwyższy stopień, jeśli nie spowolni; przełącznik do diagnostyki).

## Kontekst

Pliki gopdfgo były większe niż z Chrome: faktura 32,1 kB wobec 26,8 kB. Rozbiór pliku pokazał, że treść
strony jest u nas mniejsza (12,1 kB wobec 13,2 kB). Nadwyżka siedziała w fontach i w kompresji. Przejęta
biblioteka PDF kompresowała każdy strumień zlib w stopniu 1 (`BestSpeed`).

Pomiar 2026-11-20 (Apple M4, cały proces CLI, najkrótszy z 5 przebiegów):

| Dokument | stopień 1 | stopień 6 | stopień 9 |
|---|---|---|---|
| faktura KSeF | 32,2 kB, 47 ms | 29,2 kB, 49 ms | 29,3 kB, 49 ms |
| e-SF, 6 stron | 88,9 kB, 117 ms | 68,7 kB, 121 ms | 67,6 kB, 131 ms |
| e-SF, 16 stron | 183,9 kB, 225 ms | 125,0 kB, 233 ms | 122,4 kB, 254 ms |

## Decyzja

1. Strumienie są kompresowane zlib w stopniu 6. Stopień 9 dawał tylko 0–2% mniejszy plik, a wydłużał
   renderowanie o kolejne 8–13%, więc warunek „jeśli nie spowolni” spełnia stopień 6.
2. `Options.Uncompressed` (CLI `-uncompressed`) zapisuje strumienie treści stron, fontów i map ToUnicode
   bez filtra, do czytania w edytorze tekstu. Obrazy z przezroczystością mają filtr FlateDecode zapisany
   na stałe, więc ich dane zostają w strumieniu zlib bez kompresji (stopień 0).
3. Stopień ustawia `Render` na czas renderowania (`model.SetCompressionLevel`). Renderowania idą po kolei
   (`engineSlot`), więc zmienna pakietu nie miesza ustawień dwóch wywołań.

## Skutki

- Razem z poprawkami podzbiorów fontów z tej samej paczki (krótki format `loca`, tabela `name` z rekordami
  0–6, wyzerowane metryki glifów spoza podzbioru) faktura ma 24,8 kB, mniej niż 26,8 kB z Chrome.
  Sprawozdania e-SF zmalały o 32–37%.
- PDF/A-2b: 22 z 22 dokumentów korpusu zgodnych według veraPDF.
