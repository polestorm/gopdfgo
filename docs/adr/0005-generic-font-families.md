# ADR 0005: fonty dla rodzin generycznych wskazuje wywołujący

Status: przyjęta 2026-11-15 (decyzja użytkownika). Rozstrzyga pytanie 19 z `open-questions.md`.

## Kontekst

Silnik używa wyłącznie fontów podanych w `Options.Fonts`. Którego z nich użyć dla rodziny generycznej CSS
(`serif`, `sans-serif`, `monospace`), rozstrzygała tablica podstawień biblioteki go-text (odpowiednik
fontconfig), bez wpływu wywołującego: przy fontach Noto i Tinos `serif` dawał Noto Serif, a bez Noto Serif
Noto Sans, nigdy Tinos. Dotyczy to też fontu domyślnego dokumentu, bo wartością początkową `font-family`
jest `serif`. Przeglądarki dają dla `serif` font o metrykach Times i pod to powstają dokumenty oglądane
w podglądzie przeglądarki oraz część testów WPT.

## Decyzja

1. `Options.GenericFamilies` (mapa: nazwa rodziny generycznej → nazwy rodzin fontów w kolejności prób)
   wskazuje fonty dla rodzin generycznych. Wskazane rodziny wchodzą na listę rodzin tuż przed rodziną
   generyczną, więc rodzina nazwana w dokumencie wprost nadal ma pierwszeństwo.
2. Rodzina generyczna bez wpisu, albo z wpisem, którego rodzin nie ma w `Options.Fonts`, jest rozstrzygana
   jak dotąd. Bez `GenericFamilies` zachowanie silnika się nie zmienia.
3. Kolejności podstawień w silniku nie zmieniamy na stałe: wybór fontu jest decyzją wdrożenia, nie silnika.
4. CLI przyjmuje `-serif`, `-sans-serif` i `-monospace` (nazwy rodzin po przecinku) oraz czyta plik
   `families.json` z katalogu fontów (ta sama mapa w JSON); flagi mają pierwszeństwo przed plikiem.
5. Nazwy fontów nie są zapisane w kodzie programu ani testów. Testy i pomiary biorą przypisanie
   z `testdata/fonts/families.json`: od 2026-11-15 `serif` to Tinos (metryki Times New Roman), `sans-serif`
   to Arimo (metryki Arial), oba na OFL-1.1, w podkatalogach `testdata/fonts`. Wymiana fontu to podmiana
   plików i jednego wpisu w tym pliku (decyzja użytkownika z 2026-11-15).
6. Liczby pomiarów sprzed tej zmiany były liczone z Noto Serif jako `serif`; przy zmianie konfiguracji
   audyt podaje obie liczby dla tego samego stanu kodu.

## Skutki

- Wdrożenie, które ma oddawać wygląd z przeglądarki, podaje font o metrykach Times dla `serif` i font
  o metrykach Arial lub Helvetica dla `sans-serif`.
- Nazwy rodzin nie są sprawdzane: literówka w nazwie daje po cichu zachowanie domyślne. Zgłaszanie
  ostrzeżenia o nieznanej rodzinie jest możliwym rozszerzeniem.

Kod: `htmlpdf/render.go` (`Options.GenericFamilies`), `internal/webrender/text/engine_gotext.go`
(`FontConfigurationGotext.GenericFamilies`, `newQuery`), `cmd/gopdfgo/main.go`, test `TestGenericFamilies`.
