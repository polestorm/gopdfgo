# ADR 0045: Pamięć podręczna sparsowanych fontów (`Options.FontCache`)

Status: przyjęta 2026-11-23.

## Kontekst

Porównanie z folio (2026-11-23, `docs/benchmark.md` § 11) pokazało, że mały dokument kosztuje w gopdfgo
3–4 razy więcej czasu w procesie niż w folio, a profil procesora faktury `20-invoice` (14 fontów w
`Options.Fonts`, 6 reguł `@font-face` z tymi samymi plikami Noto) rozkładał się tak: ok. 21% czasu
procesora renderowania to `fontscan.FontMap.AddFont` dla `Options.Fonts`, ok. 14% to wczytywanie fontów
z `@font-face`, razem ok. 65% przydzielonej pamięci (ok. 50 MB z 80 MB na wywołanie). Biblioteka go-text
parsuje font w całości od razu (`font.NewFont`: wszystkie glify `glyf`, nazwy glifów z `post`, `BASE`,
`GSUB`/`GPOS`), a folio czyta tylko surowe tablice i dekoduje je przy użyciu. Program renderujący wiele
dokumentów tymi samymi fontami płacił za parsowanie przy każdym wywołaniu; już § 9 benchmarku zostawiał to
„do decyzji”, bo wymaga stanu wspólnego między wywołaniami albo zmiany interfejsu.

Sparsowany `*font.Font` jest po parsowaniu tylko czytany (zmienne pamięci podręczne go-text są w
`font.Face`, tworzonym osobno dla każdego renderowania). Jedyne miejsce, które zmienia font, to reguła
`@font-face` z `unicode-range` (podmiana `Cmap`), a mapy fontów i konfiguracji tekstu rozpoznają twarze po
wskaźniku `*font.Font`.

## Decyzja

1. Nowy typ `htmlpdf.FontCache` (`NewFontCache(maxBytes)`) i pole `Options.FontCache`. Pamięć podręczna
   przechowuje sparsowane fonty według treści pliku (skrót `maphash` i porównanie bajtów z kopią treści),
   więc trafiają w nią fonty z `Options.Fonts` i z reguł `@font-face` pobrane przez `Resolver`, niezależnie
   od nazw i adresów. Limit dotyczy łącznej długości plików; po przekroczeniu usuwane są fonty najdawniej
   użyte. Bezpieczna dla wielu wątków.
2. Pamięć jest jawna i opcjonalna: bez `FontCache` nic nie przeżywa wywołania `Render`, więc biblioteka nie
   trzyma w tle setek megabajtów fontów, o których wywołujący nie wie. Kto renderuje wiele dokumentów,
   tworzy jedną pamięć podręczną na proces albo na zestaw fontów.
3. Każde renderowanie dostaje płytką kopię struktury fontu (`ParsedFont.Copy`): własny wskaźnik dla map,
   wspólne tylko czytane tablice. `Options.Fonts` trafiają do mapy przez `FontMap.AddFace` z opisem
   `Font.Describe()` i położeniem `{plik, numer w kolekcji}`, co odpowiada temu, co robiło `AddFont`.
4. Bez `Options.FontCache` renderowanie używa pamięci podręcznej tylko na czas jednego wywołania
   (`text.NewTransientFontCache`, bez kopiowania treści): plik podany w `Options.Fonts` i wskazany przez
   `@font-face` jest parsowany raz, nie dwa razy.
5. Wynik nie zależy od pamięci podręcznej. Sprawdzenie 2026-11-23: porównanie bajtowe korpusu W3C (11 689
   dokumentów, 46 756 renderowań, polecenie z wszystkimi 67 fontami `testdata/fonts`), 76 prawdziwych
   dokumentów `bench/realdocs` i książka: 0 różnic wobec `cf2de18`; testy `tests/font_cache_test.go`
   porównują wyniki z ciepłą pamięcią i bez niej (także ten sam plik w dwóch regułach, jedna z
   `unicode-range`, i usuwanie po przekroczeniu limitu).

## Konsekwencje

- Faktura `20-invoice` w procesie z ciepłą pamięcią podręczną: ok. 58 → 38 ms (tabela w § 11 benchmarku);
  przydziały na wywołanie ok. 90 → 45 MB.
- Polecenie `gopdfgo` (jedno renderowanie na proces) korzysta tylko z punktu 4.
- Pozostały koszt fontów przy pierwszym użyciu to pełne parsowanie go-text. Leniwe parsowanie (jak w folio)
  wymagałoby zmian w go-text (zależność zewnętrzna, nie fork) albo własnego indeksu fontów; nie teraz. Dopisek
  2026-11-23: wdrożone bez zmian w go-text, ADR 0047.
- Zmiana w przejętym kodzie (`internal/webrender/text`): plik `font_cache.go`, pole
  `FontConfigurationGotext.Cache`, `loadOneFont` parsuje przez pamięć podręczną; opis w `docs/provenance.md`.
