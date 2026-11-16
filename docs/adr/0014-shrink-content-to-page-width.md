# ADR 0014: treść szersza niż obszar strony jest pomniejszana, domyślnie najwyżej 1,5 raza

Status: przyjęta 2026-11-16 (decyzja użytkownika; zastępuje punkt 4 ADR 0013).

## Kontekst

Treść o sztywnej szerokości większej niż obszar strony (kontener `width: 1000px`, szeroka tabela, obraz
bez `max-width`, długi wiersz bez łamania) wystawała za prawy margines i była ucinana na krawędzi
kartki. Strony projektowane na ekran mają często 960–1200 px, a obszar A4 z domyślnymi marginesami to
ok. 644 px, więc „dowolnej strony z ekranu” nie dało się wydrukować bez ryzyka utraty treści.

Chromium w takiej sytuacji pomniejsza całą treść: skala to szerokość obszaru strony przez szerokość
treści, najwyżej 1,5 raza (pomiar 2026-11-16: treść 110%, 125%, 150%, 200% i 400% szerokości obszaru
daje skalę 0,909, 0,800, 0,667, 0,667, 0,667); pola marginesów zostają w skali 1. Żadna norma tego nie
opisuje. ADR 0013 zostawiał to jako funkcję na przyszłość.

PDF nie ma własnego „dopasuj do strony”: o skali decyduje twórca pliku. Samo pomniejszenie to jedna
macierz przekształcenia na stronę; tekst zostaje tekstem, grafika wektorowa.

## Decyzja

1. Po ułożeniu dokumentu silnik mierzy, o ile treść wystaje poza prawą krawędź obszaru strony
   (największy iloraz po stronach; liczą się pudełka treści, także pozycjonowane, bez zawartości
   pudełek przycinających i bez pól marginesów).
2. Jeśli wystaje, dokument jest układany drugi raz na obszarze strony powiększonym tyle razy (w obu
   wymiarach, więc podziały stron wypadają inaczej i na stronie mieści się więcej treści), a treść
   strony rysowana jest w odwrotnej skali względem początku obszaru strony. Strona, jej marginesy
   i pola marginesów zachowują rozmiar z arkusza.
3. Parametr `Options.MaxShrink` (CLI `-max-shrink`) ogranicza pomniejszenie: zero oznacza 1,5 jak
   w Chromium, 1 wyłącza pomniejszanie, `math.Inf(1)` pomniejsza bez limitu (pełna gwarancja, że treść
   zmieści się na szerokość, kosztem małego pisma).
4. Silnik wywołany bezpośrednio (testy przejętego kodu) nie pomniejsza: `tree.Behavior.MaxShrink`
   równe zero wyłącza funkcję; wartość domyślną 1,5 ustawia `gopdfgo.Render`.

## Skutki

- Dokument, w którym nic nie wystaje, jest układany raz, jak dotąd. Dokument z wystającą treścią jest
  układany dwa razy.
- Treść szersza niż 1,5 obszaru strony nadal wystaje (jak w Chromium), chyba że wywołujący poda większy
  limit albo `math.Inf(1)`.
- Element o szerokości procentowej większej niż 100% rośnie razem z powiększonym obszarem, więc nadal
  wystaje, tylko pomniejszony; tak samo jest w Chromium.
- Cechy `@media` (ADR 0012) i jednostki `vw`/`vh` liczą się nadal od obszaru strony z arkusza, nie od
  powiększonego.
- Uproszczenia: tło płótna z obrazem (tło `body` przeniesione na stronę) jest pozycjonowane względem
  strony w skali 1; własne `transform` elementu głównego jest przy pomniejszaniu pomijane.
- Korpus (porównanie z wydrukiem Chromium, mediana odchylenia słów w pionie): `07-images` 43,3 pt → 0,3 pt,
  `10-preformatted` 17,2 pt → 0,1 pt, `11-edge-cases` 4 strony → 2 strony jak w Chromium.
- Sondaż ekranowy W3C uruchamiamy z `-max-shrink 1`: testy zakładają ekran, na którym przeglądarka
  niczego nie pomniejsza.
