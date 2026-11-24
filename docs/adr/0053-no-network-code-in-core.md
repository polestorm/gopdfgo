# ADR 0053: Rdzeń bez kodu sieciowego

Status: przyjęta 2026-11-24 (analiza rozmiaru binarki wobec folio na prośbę użytkownika).

## Kontekst

Binarka `cmd/gopdfgo` miała 25,2 MB, a folio 11,7 MB (`go build`, macOS arm64). Rozbicie symboli (`go tool nm
-size`) pokazało, że ok. 4,6 MB to `net/http`, `crypto/tls`, `crypto/x509` i zależne od nich pakiety, wciągane przez
`DefaultUrlFetcher` z przejętego webrender (`internal/webrender/utils/urls.go`). gopdfgo deklaruje, że rdzeń nie
łączy się z siecią: zasoby spoza `data:` dostarcza wywołujący przez `Options.Resolver` (domyślnie `DenyAll`).
Kod HTTP był więc martwy na drogach `htmlpdf`, ale jedna droga omijała `Resolver`: `@import` w arkuszu wewnątrz
obrazu SVG (`svg/css.go` `fetchURL`) wołał wprost `DefaultUrlFetcher`, który czytał `file:` z dysku i pobierał
`http(s):` z sieci.

## Decyzja

1. `DefaultUrlFetcher` obsługuje tylko `data:` i `file:` (to drugie dla testów przejętych z webrender); inne
   schematy kończą się błędem „no network access”. Klient HTTP i dekompresja odpowiedzi usunięte.
2. `@import` w arkuszu obrazu SVG idzie przez fetcher dokumentu (w gopdfgo: `Options.Resolver`), jak każdy inny
   zasób; `parseStylesheets` i `findStylesheetsRules` dostają fetcher.
3. Test `TestCoreHasNoNetworkCode` (`tests/optional_packages_test.go`): `htmlpdf` i `cmd/gopdfgo` nie zależą od
   `net`, `net/http` ani `crypto/tls`. Sieć ma tylko pakiet opcjonalny `sign` (znacznik czasu, OCSP; ADR 0050).

## Konsekwencje

- Binarka 25,2 → 20,5 MB (−18%); bez symboli (`-ldflags "-s -w"`) 18,3 → 15,1 MB (folio 8,1 MB).
- Wywołujący, który chce pobierać zasoby z sieci, robi to w swoim `Resolver` (jak dotąd).
- Rozważone i odłożone: słowniki dzielenia wyrazów skompresowane w binarce (1,3 → 0,6 MB, −0,7 MB; nagłówki
  licencji w plikach przestałyby być czytelne bez rozpakowania), usunięcie kodowań CJK z `golang.org/x/text`
  (ok. 0,8 MB; WHATWG Encoding wymaga ich dla dokumentów HTML w tych kodowaniach).
