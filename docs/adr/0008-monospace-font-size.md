# ADR 0008: pismo `monospace` ma domyślnie 13 px, jak w przeglądarkach, z parametrem

Status: przyjęta 2026-11-16 (zastosowanie zasady z ADR 0006: przy rozbieżności norma–przeglądarki
domyślnie jak Chromium, z jednym parametrem).

## Kontekst

We wszystkich przeglądarkach `font-size: medium` oznacza 13 px, gdy `font-family` jest samym słowem
`monospace`, a 16 px w pozostałych przypadkach. Skutek: `<pre>`, `<code>`, `<kbd>`, `<samp>` i `<tt>`
mają pismo 13 px, a `font-family: monospace, serif` już 16 px. Żadna norma CSS tego nie opisuje;
CSS Fonts 4 § 3.5 mówi tylko, że `medium` to rozmiar preferowany przez użytkownika.

Przejęty kod dawał 16 px. Pomiar na próbce z `<code>`, `<pre>`, `<kbd>`, `<samp>`, `<tt>` i `div`
z `font-family: monospace` (2026-11-16, `pdftotext -bbox`, Chromium bezgłowy): słowo „code” ma w Chromium
32,0 px szerokości, w gopdfgo miało 38,7 px. Dokument z listingami wychodził przez to o jedną piątą
szerszy w blokach kodu, z innym łamaniem wierszy i inną liczbą stron. Zakładają to też testy W3C
(np. `CSS2/tables/table-anonymous-objects-079` do `-086`).

## Decyzja

1. Rozmiar pisma wywiedziony ze słowa kluczowego (`medium`, `large`…, także odziedziczony albo podany
   względem rodzica w `em` i `%`) jest przeliczany przy przejściu między rodziną `monospace` a każdą
   inną: 13/16 przy wejściu, 16/13 przy wyjściu. Rozmiar podany długością (`font: 16px monospace`)
   zostaje bez zmian.
2. Parametr `Options.MonospaceFontSize` (CLI `-monospace-size`): zero oznacza 13, wartość 16 wyłącza
   regułę. W silniku pole `tree.Behavior.MonospaceFontSize`.

## Skutki

- Po zmianie wszystkie pozycje próbki mają szerokości równe Chromium co do 0,1 px.
- Dokumenty, które dotąd miały kod pismem 16 px, zmienią wygląd; poprzedni dają `MonospaceFontSize: 16`.
- Reguła rozpoznaje tylko rodzinę zapisaną jako samo `monospace`; `ui-monospace` i nazwane kroje
  o stałej szerokości zostają przy 16 px, tak jak w przeglądarkach.
