# ADR 0035: Łamanie wiersza po ukośniku

Status: przyjęta 2026-11-21.

## Kontekst

Porównanie prawdziwych dokumentów z Chromium (`bench/realdocs`, sprawozdania e-SF z xml.pro) pokazało, że
gopdfgo łamie wiersz po ukośniku w „zyskiem/|stratą”, a Chromium nie. W wąskich komórkach noty podatkowej
każde takie złamanie dodawało wiersz i przesuwało resztę strony (e-SF 1: 411 słów przesuniętych o ponad
3 pt w pionie).

UAX #14 (rewizja 57, Unicode 18) daje po ukośniku (klasa SY) okazję do złamania, poza miejscem przed
cyfrą, ale w § 5.1 zaleca, by układ z niej nie korzystał, chyba że odległość do następnej okazji
przekracza minimum ustalone przez implementację; inaczej „w/o”, „A/S” łamią się w środku. CSS Text 3
§ 5.1 pozwala dostosowywać reguły UAX #14. Chromium 157 po ukośniku przed literą albo cyfrą nie łamie
wcale, także w adresach URL („and/or”, „abc/123”, „http://example.com/abc/def” przy szerokości 4ch).

## Decyzja

1. Ukośnik stojący między znakami niebiałymi traci okazję do złamania po nim, gdy następna okazja
   (biały znak, kolejny ukośnik, dywiz) jest bliżej niż 20 znaków (`slashBreakMinDistance`,
   `text/engine_gotext.go` `slashesWithinWords`). Dotyczy łamania wierszy i szerokości min-content.
2. Przy dłuższym ciągu za ukośnikiem okazja zostaje (zalecenie UAX #14): długa ścieżka albo adres łamie
   się po ukośniku zamiast wystawać poza blok; tu świadomie różnimy się od Chromium.
3. Przed ukośnikiem łamania nie ma jak dotąd (LB13); `overflow-wrap` i `word-break` działają bez zmian.

## Konsekwencje

- e-SF 1: słowa przesunięte o ponad 3 pt 411 → 1, e-SF 3: 398 → 71 (porównanie z Chromium 157).
- Próg 20 znaków jest wyborem implementacji; zmiana progu zmienia łamanie tekstów z długimi członami
  po ukośniku.
