# gopdfgo a Chromium w xml.pro: rozmiar, szybkość, jakość i pliki PDF

Pomiar z 2026-11-20. Wersja gopdfgo: commit `4eca5e5` z 2026-11-20
(`github.com/polestorm/gopdfgo v0.0.0-20261120100328-4eca5e52974e`, silnik `gopdfgo/webrender 0.1.0-dev`).
Pod tą wersją xml.pro działa na dev.xml.pro od 2026-11-20 (commit xml.pro `03f7a02c`).
Chromium:
- na komputerze pomiarowym Google Chrome 157.0.8103.79, sterowany przez `gohtmlpdf` (chromedp, `Page.printToPDF`);
- na serwerze dev Google Chrome 158.0.8127.36.

Wszystkie liczby dotyczą tych wersji. Po zmianach w silniku trzeba je zmierzyć ponownie.

## Co porównujemy

Dwie drogi druku w xml.pro, z ustawieniami takimi jak w kodzie xml.pro:

| | Chromium | gopdfgo |
|---|---|---|
| moduł | `gohtmlpdf` (chromedp) i przeglądarka na serwerze | `gopdfgo.Render`, czysty Go |
| faktura KSeF (`internal/shop`) | `Settle` 200 ms | fonty Noto Sans z `@font-face` |
| e-SF, walidacja, eksport sekcji (`internal/esfpdf`) | margines 9 mm, `Settle` domyślny 2 s, pas nagłówka z szablonu Chrome | ten sam dokument z `@page`, pas nagłówka w polach marginesu, fonty Noto Sans i Noto Sans Mono |

Dokumenty:
- faktury: cztery warianty faktury KSeF z `goksefgo/ksefview`, z próbki `sprzedaz.golden.xml`: 2, 60 i 140 pozycji, kilka stawek VAT z przelewem;
- e-SF: pięć sprawozdań finansowych (A–E) z publicznego Repozytorium Dokumentów Finansowych KRS, od 4 do 16 stron.

Pliki pomiarowe leżą poza repozytorium.

## Rozmiar

| | Chromium | gopdfgo |
|---|---|---|
| program Go wywołujący druk, linux/amd64, `-trimpath -ldflags "-s -w"` | 7,5 MB | 25,4 MB |
| przeglądarka na serwerze (`du -sh /opt/google/chrome`) | 460 MB, do tego biblioteki systemowe z pakietów | nie potrzeba |
| fonty | systemowe | osadzone w binarce xml.pro: 2,1 MB (5 plików Noto, OFL) |
| razem na serwerze | ok. 468 MB i biblioteki | ok. 27,5 MB |

Pomiar: minimalny program `main` z jednym wywołaniem `gohtmlpdf.RenderDocument` albo `gopdfgo.Render`, zbudowany Go 1.26.6;
rozmiar przeglądarki zmierzony przez `du` na serwerze dev. gopdfgo jest około 17 razy mniejszy od samej przeglądarki.
Nie potrzebuje też instalacji, aktualizacji ani pliku `HOME` z prawem zapisu dla procesu przeglądarki.

## Szybkość

Komputer pomiarowy: Apple M4, 10 rdzeni, 32 GB. Czas jednego dokumentu: mediana z kolejnych renderowań w tym samym procesie.
Chromium działa przez cały pomiar, więc jego czasy nie obejmują startu przeglądarki.

| Dokument | Strony | gopdfgo | Chromium jak w xml.pro | Chromium bez czekania (`Settle` 1 ms) |
|---|---|---|---|---|
| faktura, 2 pozycje | 1 | 44 ms | 614 ms | 418 ms |
| faktura, 60 pozycji | 3 | 68 ms | 598 ms | 432 ms |
| faktura, stawki VAT i przelew | 1 | 49 ms | 598 ms | 425 ms |
| faktura, 140 pozycji | 7 | 98 ms | 611 ms | 438 ms |
| e-SF A | 6 | 85 ms | 2395 ms | 427 ms |
| e-SF B | 11 | 154 ms | 2428 ms | 439 ms |
| e-SF C | 12 | 145 ms | 2425 ms | 443 ms |
| e-SF D | 16 | 188 ms | 2463 ms | 459 ms |
| e-SF E | 4 | 54 ms | 2400 ms | 417 ms |

Kolumna „jak w xml.pro” obejmuje czekanie przed drukiem (`Settle`): 200 ms dla faktury i 2 s dla e-SF.
Czekanie było potrzebne, gdy wydruk e-SF ładował style i ikony z sieci. Dziś dokument jest samowystarczalny,
więc xml.pro może je skrócić bez zmiany silnika. Sam Chromium bez czekania potrzebuje 0,42–0,46 s na dokument;
gopdfgo jest wtedy od 2,4 do 9,5 razy szybszy. Przy ustawieniach xml.pro różnica wynosi od 6 do 44 razy.

gopdfgo renderuje dokumenty po kolei, jeden naraz w procesie. Chromium może drukować kilka kart równolegle.

Pamięć:
- gopdfgo, osobny proces z wierszem poleceń, szczyt RSS (`/usr/bin/time -l`): faktura 50 MB, e-SF z 6 stron 96 MB, e-SF z 16 stron 156 MB;
- Chromium: szczyt sumy RSS procesów przeglądarki w czasie pomiaru 943–961 MB, a do tego 95 MB procesu Go. Liczbę zawyżają dwie instancje przeglądarki działające naraz: osobna dla faktur i osobna dla e-SF, tak jak w xml.pro.

## Jakość

Porównanie położenia słów: `pdftotext -bbox`, słowa dopasowane przez wyrównanie ciągów, liczone poniżej pasa nagłówka.
Oba silniki dostały te same pliki fontów: faktura ma je w `@font-face`, a do e-SF Chromium dostał je w kopii dokumentu.

| Dokument | Strony (Chromium / gopdfgo) | Słowa dopasowane | Na innej stronie | Przesunięte o ponad 3 pt |
|---|---|---|---|---|
| faktura, 2 pozycje | 1 / 1 | 134 z 134 | 0 | 0 (najwyżej 1,5 pt) |
| faktura, 60 pozycji | 3 / 3 | 1023 z 1023 | 0 | 0 (najwyżej 1,5 pt) |
| faktura, stawki VAT i przelew | 1 / 1 | 251 z 251 | 0 | 0 (najwyżej 1,5 pt) |
| faktura, 140 pozycji | 7 / 7 | 2296 z 2296 | 0 | 0 (najwyżej 1,5 pt) |
| e-SF A | 6 / 6 | 3246 z 3248 | 0 | 442 (14%) |
| e-SF B | 11 / 11 | 6029 z 6257 | 39 | 1499 (25%) |
| e-SF C | 12 / 12 | 5919 z 5954 | 72 | 802 (14%) |
| e-SF D | 16 / 16 | 8622 z 8749 | 27 | 1370 (16%) |
| e-SF E | 4 / 4 | 1977 z 1977 | 0 | 17 (1%) |

Faktury wychodzą jak w Chromium: te same strony, każde słowo najwyżej 1,5 pt od miejsca w Chromium.

Sprawozdania e-SF mają tyle samo stron i te same kolumny, łącznie z szeroką Notą podatkową na całą szerokość strony.
Przesunięcia słów mają jedną główną przyczynę. Przy niektórych rozmiarach pisma gopdfgo liczy szerokość tekstu
inaczej niż Chromium o około 0,1%. Na przykład przy 9,625 px, czyli `text-sm` przy korzeniu 11 px, słowo „Sprawozdanie” jest u nas o 0,1% szersze.
Przy 9 px i 100 px szerokości zgadzają się co do 0,001 pt. W długich akapitach komórek ta różnica czasem przenosi
ostatnie słowo do następnego wiersza. Wiersz tabeli rośnie wtedy o jedną linię i wszystko poniżej przesuwa się w pionie.
Treść niczego nie traci. To zadanie dla gopdfgo: dojść, skąd biorą się te różnice szerokości przy rozmiarach ułamkowych.

Różnice wynikające z braku JavaScriptu w gopdfgo, rozwiązane w `internal/esfpdf/engine.go` po stronie xml.pro:
- Pas nagłówka na każdej stronie. Chrome rysuje go z własnego szablonu (`displayHeaderFooter`), gopdfgo z pól marginesu `@page`.
  Numer strony stoi po prawej, przed datą wydruku. Przy polu środkowym CSS Paged Media dzieli szerokość symetrycznie
  i długa nazwa podmiotu łamała się na dwa wiersze, także w Chrome z tym samym arkuszem.
- Zwinięte sekcje `<details>`. W Chrome otwiera je skrypt; dla gopdfgo dokument dostaje atrybut `open`.
- Nota podatkowa szersza niż kolumna raportu. W Chrome skrypt wysuwa ją na szerokość strony; dla gopdfgo robi to reguła CSS.
  Tabela szersza niż cała strona: Chrome pomniejsza samą tabelę (`zoom`), gopdfgo całą stronę, najwyżej 1,5 raza.

## Rozmiar plików PDF

| Dokument | Chromium | gopdfgo |
|---|---|---|
| faktura, 2 pozycje | 26,8 kB | 32,1 kB |
| faktura, 60 pozycji | 37,5 kB | 47,9 kB |
| faktura, stawki VAT i przelew | 28,2 kB | 38,0 kB |
| faktura, 140 pozycji | 52,9 kB | 68,4 kB |
| e-SF A, fonty systemowe w Chrome / Noto w gopdfgo | 115,6 kB | 90,1 kB |
| e-SF B | 163,0 kB | 143,5 kB |
| e-SF C | 172,2 kB | 140,1 kB |
| e-SF D | 193,1 kB | 185,6 kB |
| e-SF E | 78,3 kB | 67,2 kB |
| e-SF A–E, oba silniki z Noto | 182–735 kB | 65–184 kB |

Pomiar: rozmiary plików z powyższych renderowań.

Faktury z Chromium są dziś mniejsze o 17–26%. 2026-11-16 było odwrotnie (31 kB wobec 41 kB). Tamten pomiar
podawał fonty plikami, a ten, tak jak sklep xml.pro, przez `data:` w `@font-face`. Przypuszczamy, że to jest
przyczyna, ale tego nie sprawdziliśmy. Przy e-SF z tymi samymi fontami pliki gopdfgo
są 2,8–4,3 razy mniejsze. Z fontami systemowymi w Chrome, czyli tak jak w xml.pro, są mniejsze o 4–22%.

gopdfgo potrafi też zapisać PDF/A-2b (`Options.Conformance`); xml.pro jeszcze z tego nie korzysta.

## Podsumowanie

- Faktury KSeF: wynik jak w Chromium, 6–14 razy szybciej przy ustawieniach xml.pro, pliki nieco większe.
- Sprawozdania e-SF: te same strony i układ, 13–44 razy szybciej przy ustawieniach xml.pro, pliki mniejsze.
  Część akapitów łamie się o jedno słowo inaczej (różnica szerokości tekstu ok. 0,1%, zadanie dla gopdfgo).
- Wdrożenie: program z gopdfgo ma 25 MB i 2 MB fontów. Chromium wymaga przeglądarki, która zajmuje 460 MB i trzeba ją aktualizować. Pamięć na dokument od 50 do 156 MB.
