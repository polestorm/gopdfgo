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
     0,01 px, jak w kluczu pamięci fontów Blink; długości w em liczą się od pełnego font-size); od 2026-11-21
     długości układu w jednostkach LayoutUnit Blink (zob. „Uzupełnienie z 2026-11-21”) i górna krawędź
     obszaru strony (do najbliższego piksela);
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

## Uzupełnienie z 2026-11-21: LayoutUnit

Blink liczy układ w LayoutUnit, liczbach stałoprzecinkowych o kroku 1/64 px; wartość z arkusza (liczba
zmiennoprzecinkowa) zamienia na LayoutUnit z obcięciem w stronę zera. Ułamki pikseli z arkusza (`2.2px`,
`0.2rem`, mm, pt) i z `line-height` podanego liczbą sumują się więc w Chromium inaczej niż u nas: w tabeli
z `padding: 2.2px` każdy wiersz jest o 0,025 px niższy, a po kilkuset wierszach przesunięcie przekracza
wiersz i strony łamią się gdzie indziej. Pomiar w Chrome 157 (2026-11-21, pliki w `bench/realdocs/min/`
i strumienie treści PDF z Chrome): `padding: 2.2px` daje 2,1875 px, `margin-top: 0.33px` 0,328125 px,
`line-height: 1.3` przy `font-size: 8.9375px` 11,609375 px (od pełnego font-size, nie od rozmiaru obciętego
do 0,01 px); początek treści na stronie z `margin-top` 60.25px, 60.5px i 16mm wypada na 60, 61 i 60 px.

Przy `ExactLengths: false` dochodzą do listy:

- długości pudełka po rozwiązaniu procentów (marginesy, dopełnienia, `width`, `height`, `min-*`, `max-*`,
  `top`, `right`, `bottom`, `left`), obcinane do 1/64 px przed odjęciem `box-sizing`
  (`html/layout/percentages.go`, `pr.LayoutUnit`); pudełka strony i pól marginesów mają własne zasady;
- wysokość wiersza z podanego `line-height` (`text/text.go` `strutLayout`): liczba mnoży font-size obcięty
  do 1/64 px, wynik też jest obcinany;
- interlinia nad tekstem przy podanym `line-height` zaokrąglana w dół do całego piksela, reszta pod tekstem
  (jak przy `line-height: normal`; w Blink `CalculateLeadingSpace`); wysokość wiersza się nie zmienia,
  przesuwa się tylko linia bazowa;
- łamanie wierszy względem szerokości powiększonej o 1/64 px (Blink: `AvailableWidthToFit`); bez tego
  tekst równy szerokości pudełka w `ch` przestawał się mieścić po obcięciu tej szerokości;
- nadwyżka szerokości tabeli o układzie stałym rozdzielana z obcięciem udziałów do 1/64 px, reszta
  w ostatniej kolumnie;
- górna krawędź obszaru strony do najbliższego piksela (ADR 0021 mówiło „bez zmian”; pomiar na strumieniu
  treści PDF z Chrome pokazuje przesunięcie układu o całe piksele), dolny margines rośnie o tyle, o ile
  zmalał górny; pola marginesów dostają dokładny górny margines jak pozostałe (`exactPageGeometry`).

Nie naśladujemy przyciągania linii bazowej tekstu do całych pikseli przy rysowaniu: Chrome robi to dopiero
przy malowaniu, a w PDF obniżyłoby to dokładność. Z tego powodu pozostała mediana odchylenia słów w pionie
wobec Chrome wynosi ok. 0,15–0,25 pt (do pół piksela w każdą stronę), a nie zero.

Przy tej pracy wyszły dwie różnice niezależne od przełącznika, poprawione według norm:

- tabela o układzie stałym rozdzielała nadwyżkę szerokości po równo; CSS Tables 3 § 3.9.4 („Distributing
  excess width to columns”) każe rozdzielić ją proporcjonalnie do szerokości kolumn, tak robi Chrome;
- kontener flex, którego pierwszy element nie mieścił się na stronie, zostawiał na niej samo górne
  dopełnienie z tłem; teraz przechodzi cały, jak w gałęziach dla linii i kolumn.

Pomiar (2026-11-21, `bench/realdocs`, 9 prawdziwych dokumentów, `driftalign.py` wobec Chrome 157; mediana
odchylenia słów w pionie w pt / słowa odchylone o ponad 3 pt / słowa na innej stronie):

| Dokument | Przed | Po |
|---|---|---|
| e-SF 1 | 0,65 / 1 / 0 | 0,20 / 0 / 0 |
| e-SF 2 | 0,62 / 1491 / 39 | 0,19 / 3 / 0 |
| e-SF 3 | 0,53 / 71 / 0 | 0,20 / 0 / 0 |
| e-SF 4 | 0,64 / 1342 / 0 | 0,19 / 0 / 0 |
| e-SF 5 | 0,57 / 0 / 0 | 0,20 / 0 / 0 |
| faktura A | 0,33 / 0 / 0 | 0,23 / 0 / 0 |
| faktura B (60 pozycji) | 0,54 / 0 / 0 | 0,16 / 0 / 0 |
| faktura C | 0,40 / 0 / 0 | 0,14 / 0 / 0 |
| faktura D (140 pozycji) | 0,53 / 0 / 0 | 0,16 / 0 / 0 |

Liczba stron zgodna z Chrome we wszystkich dziewięciu przed i po. Trzy słowa e-SF 2 ponad 3 pt to wiersz,
który Chrome zapisuje w PDF w innej kolejności (położenia są równe, różni się dopasowanie słów).

Korpus własny (22 dokumenty): liczba stron bez zmian, mediana w pionie wobec wzorców Chromium spadła w 16,
została w 3, a w 3 wzrosła o 0,05–0,11 pt do poziomu pozostałych (tabela w `docs/progress.md`). Sondaż CSS
(tryb ciągły): 9502 z 10962, jedna strata: `css-text/white-space/white-space-pre-wrap-trailing-spaces-021`
(z dokładnością do krawędzi → niezaliczony) przez interlinię w dół do piksela: przy Noto Sans Mono 32px
obszar treści (43 px) jest wyższy od wiersza (40 px), tło `span` zachodzi o 2 px na górne obramowanie
bloku bez `overflow`, a wzorzec je przycina. Chrome 157 z tym samym fontem też nie zalicza tego testu
(313 pikseli różnicy), więc to cecha fontu, nie błąd zaokrąglenia. Reftesty druku 310 (ściśle 308,
krawędź 2), sondaż SVG 194 z 235, bez zmian.

## Uzupełnienie z 2026-11-22: szerokość tekstu i pomniejszanie (ADR 0043)

Przy `ExactLengths: false` dochodzą do listy:

- szerokość kawałka tekstu (część pudełka tekstu w wierszu, także przy min-/max-content): suma dokładnych
  szerokości znaków w górę do 1/64 px, jak `ShapeResult` w Blink; przy `true` suma dokładna;
- szerokość obszaru strony powiększonego przy pomniejszaniu treści (ADR 0014): w dół do całych pikseli
  (przybliżenie reguły Chromium, pytanie 29).

Poprawione obcięcie rozmiaru pisma do 0,01 px: mnożenie przez 100 w pojedynczej precyzji, jak w Blink
(0.88em przy 14 px daje 12,32 px, wcześniej 12,31 px).
