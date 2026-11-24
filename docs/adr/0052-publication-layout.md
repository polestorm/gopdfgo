# ADR 0052: Układ repozytorium do publikacji; pakiet `htmlpdf`

Status: przyjęta 2026-11-24 (decyzja użytkownika: przygotować repozytorium do upublicznienia, w katalogu
głównym najwyżej kilka plików, jak w gomermaidgo).

## Kontekst

Katalog główny zawierał 18 plików: pakiet `gopdfgo` (osiem plików `.go`), `CLAUDE.md`, `THIRD_PARTY.md`,
`THIRD_PARTY_NOTICES` oraz pliki modułu i licencji. Użytkownik chce, żeby po otwarciu repozytorium na GitHubie
widać było przede wszystkim README, a pliki pomocnicze leżały w katalogach.

## Decyzja

1. Pakiet API przechodzi z katalogu głównego do `htmlpdf/`: import `github.com/polestorm/gopdfgo/htmlpdf`,
   wywołanie `htmlpdf.Render(ctx, html, htmlpdf.Options{…})`. Nazwa mówi, co pakiet robi, i nie zderza się
   ze zmiennymi `pdf` w kodzie wywołujących ani z `golang.org/x/net/html`. Typy, funkcje i zachowanie bez zmian.
2. `THIRD_PARTY.md` → `docs/provenance.md` (rodowód przejętego kodu i rejestr zmian w nim),
   `THIRD_PARTY_NOTICES` → `docs/third-party-notices.txt` (wskazuje go `NOTICE`), `CLAUDE.md` →
   `.claude/CLAUDE.md`. W katalogu głównym zostają `go.mod`, `go.sum`, `LICENSE`, `NOTICE`, `README.md`
   (`go.sum` jest konieczny, bo moduł ma zależności).
3. README po angielsku na wzór gomermaidgo: po co, porównanie z Chromium i folio (zależności, rozmiar,
   szybkość, pliki wynikowe, wierność, funkcje; przypisy z datą i sposobem pomiaru), przykłady stron, stan,
   dokumentacja. Szczegóły użycia w `docs/usage.md` (po angielsku).
4. CI (`.github/workflows/test.yml`): `gofmt`, `go vet` i budowanie na Linuksie i macOS; testy własne
   (`./tests/`, `./sign/...`, `./cmd/...`) na macOS arm64, gdzie powstały wzorce. Testy przejętego kodu
   w `internal/` mają znane niepowodzenia (lista w `.claude/CLAUDE.md`), więc CI ich nie uruchamia.

## Konsekwencje

- Zmiana ścieżki importu łamie wywołujących (xml.pro): trzeba zmienić import i nazwę pakietu
  (`gopdfgo.` → `htmlpdf.`), zachowanie się nie zmienia.
- Starsze ADR i notatki wspominają `render.go`, `pdfa.go` itd.; odwołania w tekstach zamieniono na
  `htmlpdf/…`, a `THIRD_PARTY.md` na `docs/provenance.md`.
- Wyniki na Linuksie amd64 mogą różnić się od wzorców w ułamkach jednostek (brak łączenia mnożenia
  z dodawaniem jak na arm64); sprawdzimy to w CI po publikacji, zanim testy pójdą tam także.
