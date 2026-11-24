# ADR 0043: Odstęp `border-spacing` przy podziale tabeli między strony

Status: przyjęta 2026-11-23.

## Kontekst

Dokumenty losowe (`bench/realdocs`, `rand-09`, `rand-03`) mają tabele w modelu rozdzielonych obramowań
(`border-spacing` 3–4 px), które dzielą się między strony. W gopdfgo wiersz, który mieścił się na stronie,
przechodził na następną, jeśli nie mieścił się **odstęp pod nim**; w Chromium zostaje. Ten sam pomiar
pokazał drugi błąd: powtarzana stopka (`tfoot`) stała po podziale między wierszami o dwa odstępy pod
ostatnim wierszem fragmentu zamiast o jeden.

**Normy.** CSS 2.1 § 17.6.1 definiuje `border-spacing` jako odległość między obramowaniami sąsiednich
komórek (i między komórkami a krawędzią tabeli), ale nie mówi, co dzieje się z odstępem na granicy
fragmentów. CSS Fragmentation 3 § 5.2 obcina przy podziale tylko marginesy przylegające do podziału;
CSS Tables 3 (szkic) nie opisuje fragmentacji odstępów. Norma zostawia więc wybór.

**Chrome 157** (pomiar 2026-11-23, wiersze po 26 px, odstęp 3 px, strona 200 px):

- wiersz, po którym jest następny wiersz, zostaje na stronie, gdy jego dół się mieści; odstęp pod nim
  przepada na podziale, a następna strona zaczyna się od odstępu nad kolejnym wierszem;
- ostatni wiersz tabeli zostaje, gdy mieści się on i dolne obramowanie tabeli; odstęp między nimi może
  wyjść poza stronę (obramowanie 5 px: wiersz kończący się na 195 px zostaje, na 197 px przechodzi dalej);
- powtarzana stopka stoi o jeden odstęp pod ostatnim wierszem fragmentu.

## Decyzja

Gdzie norma zostawia wybór, robimy jak Chromium (decyzja użytkownika z 2026-11-22):

1. o tym, czy wiersz się mieści, decyduje jego dół; za ostatnim wierszem grupy liczy się dodatkowo dolny
   odstęp wewnętrzny i obramowanie tabeli (przy `box-decoration-break: slice`);
2. odstęp pod ostatnim wierszem fragmentu, który wychodziłby poza stronę, jest obcinany (wysokość fragmentu
   kończy się na wierszu albo na dolnym obramowaniu tabeli);
3. powtarzana stopka po podziale stoi o jeden odstęp pod dołem ostatniego wiersza fragmentu.

Punkt 3 to poprawka błędu (podwójny odstęp nie wynika z żadnej reguły), punkty 1–2 to wybór.

## Skutki

- `rand-09`: tabela dzieli się w tym samym wierszu co w Chromium (słów przesuniętych o ponad 3 pt:
  1048 → 794); faktury, e-SF, dokumenty Markdown i biurowe bez zmian.
- Zmiany w `html/layout/tables.go` (`groupLayout`, koniec `tableLayout`, nowe `lastRowBottom`) są opisane
  w `docs/provenance.md`.
- Testy: `TestTableRowFitsWithoutTrailingSpacing`, `TestRepeatedFooterSpacing` w `tests/rand23_test.go`.
