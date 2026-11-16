# Rejestr decyzji (ADR)

Jedna decyzja = jeden plik `NNNN-temat.md`: kontekst, decyzja, konsekwencje, status. Zmiana decyzji = nowy ADR, stary
oznaczony „zastąpiony przez”. Pytania bez decyzji: `open-questions.md`.

| # | Decyzja | Status |
|---|---|---|
| 0013 | Jeden przełącznik `Options.ExactLengths`: domyślnie zaokrąglenia długości jak w Chromium (obramowania, `border-spacing`), `true` daje długości dokładnie z arkusza; pomniejszania strony nie naśladujemy | przyjęta |
| 0012 | Cechy rozmiaru w `@media` (`width`, `height`, `orientation`) oceniane względem obszaru strony z `@page`, jak w Chromium i Media Queries 4 | przyjęta |
| 0011 | Element wiersza flex, który nie mieści się na stronie, przechodzi na następną sam (jak w przeglądarkach); gdy nie mieści się żaden, przechodzi cała linia | przyjęta |
| 0010 | Pismo pionowe (`writing-mode`) tylko jako poddrzewo obrócone i dopiero na potrzebę dokumentu; osie logiczne i pełne pismo pionowe poza projektem | przyjęta |
| 0009 | Szerokości obramowań, konturów i linii między kolumnami przyciągane do całych pikseli jak w Chromium (CSS Values 4 § 6.1); przełącznik w ADR 0013 | przyjęta; parametr zastąpiony przez ADR 0013 |
| 0008 | Pismo `monospace` bez podanego rozmiaru ma 13 px jak w przeglądarkach; `Options.MonospaceFontSize` zmienia albo wyłącza regułę | przyjęta |
| 0007 | Cechy w `@media` (`min-width`, `orientation`, …) oceniane względem stałego medium odniesienia: A4 w pionie, niezależnie od `@page` | przyjęta; rozmiar medium zastąpiony przez ADR 0012 |
| 0006 | Dolny margines dziecka przy `min-height`/`max-height` zostaje w pudełku jak w Chromium; `Options.MarginsEscapeHeightLimits` włącza zachowanie z dosłownego tekstu CSS 2.2 | przyjęta |
| 0005 | Fonty dla rodzin generycznych (`serif`, `sans-serif`, `monospace`) wskazuje wywołujący przez `Options.GenericFamilies`; bez wskazania bez zmian | przyjęta |
| 0004 | Blok o podanej wysokości: mieszczący się na stronie jest przenoszony w całości, wyższy niż strona dzielony; wariant przeglądarkowy ewentualnie jako opcja | przyjęta |
| 0003 | Reftesty: zgodność ścisła albo z dokładnością do krawędzi (`pass-edge`); Ahem tylko przez `@font-face` | przyjęta |
| 0002 | Układ na bazie forka webrender bez Pango; PDF/A, tagowanie i `calc()` uzupełniane z folio | przyjęta, do potwierdzenia benchmarkiem |
| 0001 | Lista wyświetlania jako warstwa niezależna od formatu wyjścia; backendy PDF, SVG, PNG czytają tylko ją | przyjęta |
