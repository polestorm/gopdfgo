# ADR 0024: słowniki dzielenia wyrazów tylko na licencjach liberalnych, pozostałe języki od wywołującego

Status: przyjęta 2026-11-20 (decyzja użytkownika).

## Kontekst

`hyphens: auto` dzieli wyrazy według wzorców dla języka z atrybutu `lang`. Przejęty kod (port Pyphen)
wbudowywał w binarkę 42 słowniki z LibreOffice, razem 5,8 MB. Sprawdzone 2026-11-20 w plikach README
słowników Pyphen:
- polski, niemiecki, francuski, włoski, duński, grecki, serbski i inne są na LGPL;
- czeski, galicyjski i portugalski (PT) są na GPL;
- tylko angielskie są na licencji w stylu BSD.

Zasady projektu wykluczają LGPL i GPL, bo binarka jest statyczna.

Słowniki LibreOffice to przeróbki wzorców z TeX-a. Oryginały prowadzi projekt hyph-utf8
(https://github.com/hyphenation/tex-hyphen). Ich licencje z nagłówków plików `.tex` (2026-11-20):

| Licencja | Języki |
|---|---|
| MIT (sama albo do wyboru) | af, be, da, de (1996), el (monotoniczny), en-gb, es, et, fr, it, lt, mn, nl, pl, sk, sl, sq, te, uk |
| BSD-3-Clause albo w stylu BSD | pt, bg |
| Unlicense | gl |
| FSF all-permissive („Copying and distribution … are permitted”) | en-us, nb, nn |
| MPL, GPL i LGPL do wyboru | hu |
| tylko LPPL | eo, hr, is, ru, sh-latn, sv, th |
| tylko GPL albo LGPL | cs, id, lv, sr-cyrl |
| bez licencji | ro |

## Decyzja

1. W binarce są tylko wzorce hyph-utf8 na licencjach MIT, BSD, Unlicense i FSF all-permissive (24 języki,
   1,3 MB). Generator `internal/webrender/text/hyphen/gen` buduje je z plików `.pat.txt` i `.hyp.txt`.
   Wyjątki zapisuje jako wzorce całych słów. Każdy plik `.dic` zaczyna się nagłówkiem źródła z autorami
   i pełnym tekstem licencji. Wykaz jest w `dictionaries/NOTICES.txt` i w `docs/third-party-notices.txt`.
2. Języki na LPPL, MPL, GPL, LGPL albo bez licencji nie są wbudowane. Węgierski ma MPL do wyboru, ale MPL
   każe każdemu, kto rozpowszechnia binarkę, wskazywać źródło pliku. Takiego obowiązku nie przerzucamy na
   użytkowników gopdfgo. LPPL nie ma na liście dopuszczonych licencji projektu, a zamiana wzorców na inny
   format może być zmianą w jej rozumieniu.
3. `Options.Hyphenation` (CLI `-hyphenation język=plik`) przyjmuje słowniki od wywołującego. Mają
   pierwszeństwo przed wbudowanymi i obowiązują tylko w danym wywołaniu. Za ich licencję odpowiada
   wywołujący.

## Skutki

- Binarka mniejsza o 4,5 MB słowników.
- Dzielenie polskie, niemieckie i francuskie idzie według oryginalnych wzorców TeX-a, a nie przeróbek
  LibreOffice. Pojedyncze wyrazy mogą dzielić się inaczej niż dotąd.
- Bez słownika od wywołującego nie dzielą się wyrazy w językach: cs, hr, hu, id, is, lv, ro, ru, sr, sv,
  th, eo i zu.
- Przejęte testy `TestAlternative` (węgierskie wzorce z wymianą liter) i `TestFallback` (serbski) zostały
  przepisane na słownik w pamięci i inne języki.
