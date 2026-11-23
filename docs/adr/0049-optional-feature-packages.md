# ADR 0049: Funkcje opcjonalne jako osobne pakiety modułu

Status: przyjęta 2026-11-24 (decyzja użytkownika).

## Kontekst

Do zrobienia są funkcje, których nie potrzebuje każdy wywołujący, a które zwiększają binarkę albo wnoszą
kod wrażliwy dla walidacji (kryptografia, połączenia sieciowe, czytanie cudzych plików PDF): podpis PAdES,
szyfrowanie z uprawnieniami, PDF jako tło lub szablon, formularze interaktywne. Użytkownik chce, żeby
binarka bez tych funkcji ich kodu nie zawierała, a walidacja oprogramowania (ISO) mogła ten kod pominąć.

Znaczniki kompilacji (ADR 0039, czcionki systemowe) to rozwiązują, ale każdy znacznik to osobny wariant
do testowania, kombinacje mnożą się, a wywołujący musi pamiętać o `-tags`. Linker Go nie dołącza do
binarki pakietów, których program nie importuje, więc ten sam skutek daje zwykły podział na pakiety.

## Decyzja

1. Funkcja opcjonalna leży w osobnym pakiecie modułu `github.com/polestorm/gopdfgo/<nazwa>`; rdzeń
   (`gopdfgo`, `internal/…`) jej nie importuje. Program, który nie importuje pakietu, nie ma jego kodu
   w binarce.
2. Funkcja działająca po renderowaniu ma w pakiecie własne wywołanie na gotowych bajtach PDF
   (np. `sign.SignPDF`). Funkcja działająca wewnątrz renderowania korzysta z małego zaczepu w rdzeniu
   (interfejs bez kodu funkcji), który pakiet wypełnia rejestracją przy imporcie
   (`import _ "github.com/polestorm/gopdfgo/<nazwa>"`, jak dekodery w `image/…`). Rejestracja jest
   deterministyczna i niezależna od kolejności importów; bez rejestracji rdzeń zachowuje się jak dziś
   i, gdy dokument wymaga funkcji, daje ostrzeżenie.
3. Każdy pakiet ma testy uruchamiane zwykłym `go test ./...`. Brak kodu pakietu w binarce domyślnej
   (`cmd/gopdfgo` i program bez importu) sprawdza test z `go tool nm`, jak w ADR 0039.
4. Polecenie `gopdfgo` (CLI) może importować pakiety opcjonalne za flagami; zwalidowany wariant CLI
   bez nich buduje się z osobnego `main` albo znacznikiem — decyzja przy pierwszym takim pakiecie.
5. Czcionki systemowe zostają przy znaczniku kompilacji (ADR 0039), bo zmieniają zachowanie samego
   rdzenia (wybór fontu), a nie dodają funkcji obok niego.

## Konsekwencje

- Zwalidowana binarka nie zawiera kodu funkcji, których program nie importuje; zaczepy w rdzeniu
  są małe i pokryte testami rdzenia.
- Kolejne funkcje: `sign` (PAdES, ADR 0050), potem szyfrowanie, PDF jako tło, formularze — każda
  z własnym ADR.
