# ADR 0015: blok o podanej wysokości jest dzielony między kolumny

Status: przyjęta 2026-11-16 (decyzja robocza przy wdrożeniu, według zasady z ADR 0006: tam, gdzie norma
zostawia wybór, zachowanie jak w przeglądarkach). Uzupełnia ADR 0004, który dotyczy podziału między strony.

## Kontekst

ADR 0004 mówi, że blok o podanej wysokości, który zmieściłby się na pustej stronie, jest przenoszony
w całości na następną stronę, a nie przecinany. W kolumnach ta reguła nie obowiązywała wcale: treść kolumn
miała własny algorytm i bloków nie dzieliła, więc blok 200 px w dwóch kolumnach zostawał w pierwszej,
a druga była pusta.

Równoważenie kolumn (CSS Multicol 1 § 7.1) bez dzielenia bloków nie działa: przeglądarki rozkładają
taki blok po 100 px na kolumnę i tego oczekują reftesty W3C (`css-multicol`: `always-balancing-before-column-span`,
`multicol-span-all-012`, `-013`, testy `spanner-fragmentation-*` i kilkadziesiąt innych).

## Decyzja

W kolumnach blok o podanej wysokości dzielimy zawsze tam, gdzie kończy się kolumna, tak jak przeglądarki.
Nie dzielimy:

- bloku z `break-inside: avoid` (także `avoid-column`),
- pudełka niepodzielnego: `contain: size`, `overflow` inne niż `visible`, elementy zastępowane,
- bloku, który jest blokiem zawierającym elementy pozycjonowane (nie umiemy podzielić ich razem z nim).

Gdy kolumny same dzielą się między strony, obowiązuje to samo: kolumna jest fragmentem i blok kończy się
na jej dole.

Kod: `internal/webrender/html/layout/block.go` (`canSplitFixedHeight`, warunek `splittable`),
test `TestColumnsSpannersSlicingAndContain`.

## Skutki

- Ramka albo karta o stałej wysokości w układzie kolumnowym może zostać przecięta między kolumnami.
  Autor, który tego nie chce, używa `break-inside: avoid` (działa tak samo w przeglądarkach).
- Zachowanie różni się od podziału między strony z ADR 0004; tam domyślne pozostaje przenoszenie.
- Nie ma parametru wyłączającego dzielenie w kolumnach; do dodania, jeśli okaże się potrzebny.
- Dwa przejęte testy (`TestColumnsRegression1`, `TestColumnsRegression2`) oczekiwały przenoszenia bloków
  w całości; zostały przepisane (zob. `docs/provenance.md`).
