# ADR 0009: szerokości obramowań przyciągane do całych pikseli, jak w Chromium, z parametrem

Status: przyjęta 2026-11-16 (zastosowanie zasady z ADR 0006: przy rozbieżności norma–przeglądarki
domyślnie jak Chromium, z jednym parametrem).

## Kontekst

CSS Values 4 § 6.1 („snap a length as a border width”) każe przyciągać `border-width`, `outline-width`
i `column-rule-width` do pikseli urządzenia: długość między zerem a jednym pikselem urządzenia rośnie
do jednego piksela, większa jest obcinana w dół do całych pikseli. Norma nie mówi, czym jest piksel
urządzenia w PDF. Chromium przy druku przyjmuje piksel urządzenia równy pikselowi CSS (1/96 cala),
więc `0.4pt` i `0.5pt` rysuje i układa jako 1 px (0,75 pt), a `1pt` i `1.2pt` też jako 1 px.

Przejęty kod trzymał szerokości dokładne. Pomiar (2026-11-16, `pdftotext -bbox`, Chromium bezgłowy,
20 pudełek jedno pod drugim): na jedno pudełko z `border: 0.4pt` Chromium jest wyższe o 0,937 px,
z `0.5pt` o 0,671 px, z `1.2pt` niższe o 1,196 px, czyli dokładnie dwa obramowania po 1 px. Wysokości
wierszy, dopełnienia i marginesy zgadzały się co do 0,02 px. W korpusie to była główna przyczyna
narastającego odchylenia tekstu w pionie: `02-register-table` (obramowania `0.4pt`) 5,57 pt w medianie
i 2015 z 2844 słów przesuniętych o ponad 3 pt.

## Decyzja

1. Wartość wyliczona `border-*-width`, `outline-width` i `column-rule-width` jest przyciągana do całych
   pikseli CSS: `(0, 1)` → 1 px, powyżej 1 px w dół do liczby całkowitej. Dotyczy układu i rysowania.
2. Parametr `Options.ExactBorderWidths` (CLI `-exact-border-widths`) wyłącza przyciąganie. W silniku
   pole `tree.Behavior.ExactBorderWidths`.

## Skutki

- Po zmianie `02-register-table` ma medianę 0,34 pt i żadnego słowa przesuniętego o ponad 3 pt;
  `08-flex` 0,24 pt (było 3,61), `20-invoice` 0,68 pt (było 2,52).
- Cienkie linie grubieją: `0.25pt`, `0.4pt`, `0.5pt` dają 0,75 pt. Linie między całymi pikselami
  chudną: `1pt` daje 0,75 pt, `2pt` (2,67 px) daje 1,5 pt. Kto potrzebuje szerokości dokładnych
  w druku, ustawia `ExactBorderWidths: true`.
- Chromium obcina też `border-spacing` do całych pikseli (`1mm` = 3,78 px liczy jako 3 px). Tego
  norma nie opisuje i gopdfgo tego nie robi; zob. pytanie 21 w `open-questions.md`.
