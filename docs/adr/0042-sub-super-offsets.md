# ADR 0042: Przesunięcie `vertical-align: sub` i `super` według wartości domyślnych CSS Inline 3

Status: przyjęta 2026-11-22.

## Kontekst

Dokumenty biurowe przekonwertowane do HTML (`bench/realdocs`, źródło `office-*`, `textutil -convert html`)
mają dużo indeksów: m³, Q₁, Δp_dop. W tabelach z takimi indeksami wiersze w gopdfgo były wyższe niż
w Chromium i strony łamały się wcześniej (tabela parametrów, 2 strony: 3 strony w gopdfgo).

Przesunięcie linii bazowej przejęte ze źródła (WeasyPrint, za Pango) wynosiło ±0,5 em **własnego**
rozmiaru pisma pudełka. Przy `font-size: smaller` w arkuszu UA (`sub, sup`) daje to 0,417 rozmiaru rodzica
w dół i w górę.

**Normy.** CSS 2.2 § 10.8.1 mówi tylko o „właściwym położeniu” indeksu względem rodzica. CSS Inline 3
§ 4.2.3 (`baseline-shift`, na którą wskazuje skrót `vertical-align`): `sub` — „The UA may use the parent's
font metrics to find this offset; otherwise it defaults to dropping by one fifth of the parent's used
font-size”; `super` — tak samo, „one third”.

**Chrome 157** (pomiar 2026-11-22, Tinos 30 px i 60 px, `sub, sup { font-size: inherit }`): przesunięcie
`sub` = rozmiar rodzica / 5 + 1 px, `super` = rozmiar rodzica / 3 + 1 px (30 px: 7 i 11 px, 60 px: 13 px).
Dodatkowy piksel nie pochodzi ani z metryk fontu, ani z wartości domyślnej normy.

## Decyzja

`sub` obniża linię bazową o 1/5, `super` podnosi o 1/3 użytego rozmiaru pisma **rodzica** (wartości
domyślne CSS Inline 3; metryk indeksów z tabeli OS/2 nie używamy, Chromium też nie). Dodatkowego piksela
Chromium nie przejmujemy: norma rozstrzyga wartość domyślną (zasada „normy na pierwszym miejscu”,
decyzja użytkownika z 2026-11-22).

## Skutki

- Indeks dolny schodzi o połowę mniej niż wcześniej; wiersz z indeksem jest o ok. 1 px niższy niż
  w Chromium (wcześniej o 1–2 px wyższy). Tabela parametrów z Worda: 2 strony jak w Chromium, wiersze
  z indeksami po 0,75 pt niższe, więc na kolejnych stronach treść zaczyna się nieco wcześniej.
- Wcześniejsze przesunięcie zostaje opisane w `THIRD_PARTY.md` (zmiana w `html/tree/computed_values.go`).
- Gdyby kiedyś powstał tryb zgodności z Chromium (pytanie 28 w `docs/adr/open-questions.md`), „+1 px”
  może być jego częścią.
- Test: `TestSubSuperDefaultOffsets` w `tests/office22_test.go`.
