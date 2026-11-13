# gopdfgo — instrukcje dla Claude Code

Projekt: silnik ogólnego użytku HTML + CSS + SVG → PDF w czystym Go, bez JavaScriptu, z obsługą CSS Paged Media.
Cele (`docs/vision.md`): zastąpić Chromium czymś co najmniej 10 razy mniejszym, mieć pełną kontrolę nad kodem,
renderować deterministycznie, pokrywać typowe dokumenty w wybranych ramach bez konkurowania z przeglądarką.
Przy sporze wygrywają rozmiar, przewidywalność, szybkość i czystość kodu, kosztem przypadków brzegowych.
Projekt jest agnostyczny wobec odbiorców; QMS i xml.pro to przyszli użytkownicy, nie źródło wymagań.

Zacznij od `README.md` (indeks), potem `docs/landscape.md` (co już istnieje, z pomiarami) i `docs/adr/open-questions.md`
(decyzje do podjęcia). Etapy 0 (rozpoznanie) i 1 (korpus testowy, `testdata/README.md`) są zamknięte; etap 2 to próba forka i benchmark
(`docs/benchmark.md`, `docs/roadmap.md`). Decyzja o strategii zapadła: ADR 0002, fork webrender w `internal/`,
stan przejętego kodu i testów w `THIRD_PARTY.md`.

Zasady:
- nazwy plików, katalogów, pakietów i identyfikatorów po angielsku; treść dokumentów po polsku;
- wzorcem są normy (W3C, ISO), nie biblioteki: gdy przejęty kod albo jego testy różnią się od specyfikacji,
  poprawiamy według specyfikacji i odnotowujemy to w `THIRD_PARTY.md`;
- każda decyzja = plik w `docs/adr/`; pytania bez decyzji w `docs/adr/open-questions.md`;
- kod to moduł `github.com/polestorm/gopdfgo` (jak moduły w `../controls/*`); układ pochodzi z forka webrender
  w `internal/` (ADR 0002), każda zmiana w przejętym kodzie trafia do `THIRD_PARTY.md`;
- liczby (gwiazdki, linie kodu, rozmiary binarek) są datowane i podpisane sposobem pomiaru; szacunki oznaczone jako szacunki;
- kod przejęty do `internal/` jest nasz: wolno go budować, testować i uruchamiać. Innych pobranych repozytoriów
  (folio, go-webengine, cokolwiek nowego) nie uruchamiaj bez zgody użytkownika; ich klony i programy pomiarowe
  trzymamy poza gitem w `bench/`;
- renderowanie w testach i pomiarach czyta zasoby wyłącznie spod `testdata/`, fonty wyłącznie z `testdata/fonts`;
- zależności tylko na licencjach MIT, BSD, Apache-2.0, OFL (fonty); LGPL/GPL wykluczone, bo binarka jest statyczna;
- commit i push tylko na polecenie użytkownika.
