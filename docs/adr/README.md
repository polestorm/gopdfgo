# Rejestr decyzji (ADR)

Jedna decyzja = jeden plik `NNNN-temat.md`: kontekst, decyzja, konsekwencje, status. Zmiana decyzji = nowy ADR, stary
oznaczony „zastąpiony przez”. Pytania bez decyzji: `open-questions.md`.

| # | Decyzja | Status |
|---|---|---|
| 0003 | Reftesty: zgodność ścisła albo z dokładnością do krawędzi (`pass-edge`); Ahem tylko przez `@font-face` | przyjęta |
| 0002 | Układ na bazie forka webrender bez Pango; PDF/A, tagowanie i `calc()` uzupełniane z folio | przyjęta, do potwierdzenia benchmarkiem |
| 0001 | Lista wyświetlania jako warstwa niezależna od formatu wyjścia; backendy PDF, SVG, PNG czytają tylko ją | przyjęta |
