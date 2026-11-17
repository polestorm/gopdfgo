# ADR 0017: tryb ciągły, czyli jedna strona rosnąca do wysokości treści

Status: przyjęta 2026-11-16 (decyzja użytkownika: wdrożyć, żeby pomiar testów ekranowych nie zależał od
podziału na strony).

## Kontekst

Testy ekranowe W3C zakładają okno 800 x 600 px, w którym treść poniżej dolnej krawędzi po prostu jest
dalej, niepodzielona. Silnik jest stronicowy, więc w sondażu treść wyższa niż 600 px przechodziła na
następną stronę. Część testów nie przechodziła wyłącznie z tego powodu: wzorce, które przesuwają
pudełka w górę przez `position: relative` (`css-backgrounds/background-origin`), kontenery kolumn wyższe
niż strona, elementy o podanej wysokości na granicy stron. Taki wynik mówił o metodzie pomiaru, nie
o silniku.

Poza pomiarem tryb przydaje się tam, gdzie PDF ma być jedną długą stroną: paragony, podglądy, grafiki
z HTML.

## Decyzja

`Options.Continuous` (wiersz poleceń: `-continuous`) wyłącza podział na strony:

1. Powstaje jedna strona o szerokości i marginesach z `@page`. Rośnie w dół do dolnej krawędzi treści
   i nie jest niższa niż strona z arkusza.
2. Wysokość obszaru strony z arkusza pozostaje podstawą wysokości procentowych elementu głównego,
   jednostek `vh` i elementów pozycjonowanych względem strony, tak jak okno przeglądarki.
3. Wymuszone podziały (`break-before`, `break-after`, zmiana strony nazwanej) nie tworzą stron;
   `break-before: column` w kolumnach działa. `orphans`, `widows`, `break-inside` i powtarzanie
   nagłówków tabel nie mają zastosowania, bo nic się nie dzieli.
4. Pola marginesów są układane raz, dla tej jednej strony.
5. Kolumny (uzupełnienie z 2026-11-17, CSS Multicol 1 § 7.1 i § 8.2 dla mediów ciągłych): kontener,
   który sam nie leży w innej kolumnie, nie przenosi treści „na następną stronę”. Nadmiarowe kolumny
   (po wymuszonych podziałach albo przy za małej wysokości) powstają w osi wiersza, treść niemieszcząca
   się w kolumnie zostaje w niej i wystaje, a `column-fill: auto` bez ograniczonej wysokości równoważy
   kolumny. Bez tego strona rosła do umownej wysokości 2^22 px.

Kod: `internal/webrender/html/layout/pages.go` (`makePage`), `layout.go` (`contentBottomEdge`),
`block.go` (`forcePageBreak`); test `TestContinuousMode`.

## Skutki

- Sondaż ekranowy jest od 2026-11-16 uruchamiany w trybie ciągłym (`SURVEY_ARGS='-max-shrink 1
  -continuous'`); wynik w trybie stronicowym z tego samego dnia jest zapisany w `docs/progress.md` jako
  punkt przejścia. Reftesty druku (`testdata/wpt`) zostają stronicowe.
- Dół treści liczymy z krawędzi obramowań pudełek, tekstu i elementów pozycjonowanych; treść przycięta
  przez `overflow` się nie liczy, cienie i kontury też nie.
- Treść układana jest na stronie o umownej wysokości 2^22 px; dokument wyższy zostałby ucięty.
