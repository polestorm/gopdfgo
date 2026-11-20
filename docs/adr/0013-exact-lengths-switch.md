# ADR 0013: jeden przełącznik `ExactLengths`: zaokrąglenia jak w Chromium albo wierność arkuszowi

Status: przyjęta 2026-11-16 (decyzja użytkownika; zamyka pytanie 21, zastępuje parametr z ADR 0009).

## Kontekst

Chromium zaokrągla część długości do całych pikseli CSS. Dla wyświetlacza to rozsądne, bo i tak rysuje
w pikselach; PDF pozwala zapisać długości dokładnie. Dotąd znaliśmy dwa takie miejsca:

- szerokości obramowań, konturów i linii między kolumnami (ADR 0009; to akurat opisuje CSS Values 4
  § 6.1) – przyciągane domyślnie, z parametrem `ExactBorderWidths`;
- `border-spacing` – Chromium obcina w dół do całych pikseli (pomiar 2026-11-16: `1mm` daje 3 px,
  `0.6px` daje 0, `1.9px` daje 1 px); tego norma nie opisuje, u nas wartość była dokładna.

Osobny parametr na każde takie miejsce mnożyłby opcje, a wywołujący chce zwykle jednego z dwóch
wyników: „jak z Chromium” albo „dokładnie jak w arkuszu”.

Trzecia różnica z pytania 21, pomniejszanie całej strony, gdy treść wystaje poza jej szerokość
(najwyżej 1,5 raza), nie jest zaokrągleniem, tylko automatem zmieniającym rozmiar pisma.

## Decyzja

1. Jeden przełącznik `Options.ExactLengths` (CLI `-exact-lengths`, w silniku `tree.Behavior.ExactLengths`):
   - `false` (domyślnie): długości, które Chromium zaokrągla, są zaokrąglane tak samo. Dziś: szerokości
     obramowań, konturów i linii między kolumnami (reguła z ADR 0009) oraz `border-spacing` (obcinane
     w dół); od ADR 0021 także szerokość i wysokość obszaru strony (w górę do całych pikseli) i jego lewa
     krawędź (do najbliższego piksela); od 2026-11-20 także rozmiar pisma do składu tekstu (obcinany do
     0,01 px, jak w kluczu pamięci fontów Blink; długości w em liczą się od pełnego font-size);
   - `true`: wszystkie długości zostają takie, jak w arkuszu, z dokładnością, na jaką pozwala PDF.
2. `Options.ExactBorderWidths` i flaga `-exact-border-widths` znikają; zastępuje je przełącznik globalny.
3. Każde następne zaokrąglenie Chromium, które zdecydujemy się naśladować, podlega temu samemu
   przełącznikowi i jest dopisywane do listy w komentarzu `Options.ExactLengths`.
4. (Zmienione przez ADR 0014: pomniejszanie jest wdrożone i domyślnie włączone.) Pomniejszania strony nie naśladujemy. Dopasowanie wydruku do strony (fit-to-page) na życzenie
   wywołującego pozostaje osobną funkcją do zaprojektowania.

## Skutki

- Tabele z `border-spacing` podanym w mm, pt albo ułamkach piksela mają domyślnie odstępy jak
  w Chromium (w korpusie: `17-tables`).
- Wywołujący, który używał `ExactBorderWidths`, przechodzi na `ExactLengths`; dostaje przy tym także
  dokładne `border-spacing`.
- Dokument z treścią szerszą niż strona drukuje się w skali 1 i treść wystaje za margines, inaczej niż
  w Chromium (`07-images`, `10-preformatted` w korpusie).
