# ADR 0018: fonty testowe CSSTest w `testdata/fonts/csstest`

Status: przyjęta 2026-11-18 (decyzja użytkownika: „przejdź zatem do punktu 1”).

## Kontekst

Część testów W3C (`css-fonts/font-family-name-*`, `font-weight-{bolder,lighter,normal}-001`,
`test-synthetic-italic*`) odwołuje się do rodzin po nazwie (`font-family: CSSTest Verify`) bez `@font-face`.
Zakładają, że przed uruchomieniem przeglądarki w systemie zainstalowano zestaw fontów CSSTest, który leży
w samym repozytorium WPT (`fonts/CSSTest`, przerobiony Gentium Basic, SIL OFL 1.1). Nasza zasada brzmi:
fonty wyłącznie z `testdata/fonts`. Silnik nie ma pojęcia fontów systemowych; fonty z katalogu `-fonts`
(i jego podkatalogów pierwszego poziomu) pełnią ich rolę.

## Decyzja

53 pliki `*.ttf` z `fonts/CSSTest` (WPT, commit z `testdata/wpt/UPSTREAM`) kopiujemy bez zmian do
`testdata/fonts/csstest/`, z licencją jako `OFL.txt`. Służą tylko testom i pomiarom; nie trafiają do binarki.

## Skutki

- Sondaż (2026-11-18): 15 testów `css-fonts` zaczyna przechodzić (`font-family-name-*` 12,
  `font-weight-bolder/lighter/normal-001`).
- Trzy testy tracą przypadkowe zaliczenia:
  - `test-synthetic-italic` (test „mismatch”): wymaga syntezy kursywy, której nie mamy; dotąd różnica
    brała się z braku fontu CSSTest Verify;
  - `font-face-weight-auto-variable`, `font-face-weight-default-variable`: font testu ma tylko znaki
    „e”, „t”, „x”, bez spacji; spacja i metryki wiersza pochodzą z fontu zastępczego dobieranego według
    grubości, którym są teraz fonty CSSTest Weights 100–900 o różnych metrykach. To skutek środowiska
    testowego (przeglądarka sięgnęłaby po font zastępczy systemu), nie błąd obsługi `font-weight: auto`.
- Korpus: 22 dokumenty identyczne co do piksela z fontami CSSTest i bez nich (ta sama binarka, 48 dpi);
  pozostałe obszary sondażu (4789 testów) bez zmian.
- Dobór fontu zastępczego spośród wszystkich wczytanych fontów według grubości zostaje otwartą sprawą:
  w zwykłym użyciu katalog fontów zawiera tylko fonty dokumentu, więc nie ma tu konfliktu.
