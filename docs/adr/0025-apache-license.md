# ADR 0025: licencja projektu Apache-2.0

Status: przyjęta 2026-11-20 (decyzja użytkownika). Repozytorium zostaje prywatne do osobnej decyzji
o upublicznieniu.

## Kontekst

Wymagania użytkownika: bezpłatne użycie, także komercyjne, z atrybucją autora (Michał Głębowski).
Rozważane licencje: MIT, BSD-3-Clause, Apache-2.0. Wszystkie pozwalają na użycie komercyjne i wymagają
zachowania informacji o prawach autorskich. Żadna licencja otwarta nie wymusza widocznego napisu
w interfejsie produktu.

## Decyzja

1. Licencja projektu: Apache License 2.0 (`LICENSE`, tekst z apache.org).
2. `NOTICE` z „gopdfgo, Copyright 2026 Michał Głębowski”. Apache-2.0 § 4(d) każe każdemu, kto
   rozpowszechnia program z gopdfgo, dołączyć treść `NOTICE` do informacji o licencjach. To najmocniejsza
   atrybucja dostępna w licencjach liberalnych. Apache-2.0 zawiera też udzielenie licencji patentowej.
3. `docs/third-party-notices.txt` zbiera pełne teksty licencji składników wbudowanych w binarkę:
   - webrender i go-weasyprint (BSD-3-Clause; to porty WeasyPrint, Kozea, BSD-3-Clause);
   - benoitkugler/pdf i pstokenizer (MIT);
   - go-text/typesetting (BSD-3-Clause, wybrane z „Unlicense OR BSD-3-Clause”);
   - biblioteka standardowa Go, golang.org/x/{image,net,text} i hhrutter/lzw (BSD-3-Clause, The Go Authors);
   - wzorce dzielenia wyrazów (ADR 0024).

   Licencje BSD wymagają powtórzenia tych informacji przy dystrybucji binarnej, więc użytkownicy gopdfgo
   mają je dołączać razem z `LICENSE` i `NOTICE`.
4. Wszystkie licencje składników (MIT, BSD-3-Clause, Unlicense, FSF all-permissive) są zgodne z Apache-2.0.
   Nazwisk autorów składników nie używamy do promocji (BSD-3-Clause, pkt 3). Wzmianka o pochodzeniu jest
   w `docs/third-party-notices.txt` i README.
5. `docs/provenance.md` zostaje rejestrem zmian w przejętym kodzie; `testdata/THIRD_PARTY.md` rejestrem danych
   testowych, które nie trafiają do binarki.

## Do zrobienia przed upublicznieniem

- Fonty próbek w `testdata/samples/weasyprint/` nie mają dołączonych plików licencji (OFL i Apache-2.0
  według katalogu Google Fonts). Trzeba je dołączyć albo usunąć próbki.
- Sprawdzić, czy w historii gita nie zostały pliki, których nie wolno publikować; dotyczy to także
  usuniętych słowników LibreOffice (LGPL i GPL wolno publikować w źródłach, ale warto to odnotować).
