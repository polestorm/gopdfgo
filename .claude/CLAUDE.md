# gopdfgo — instrukcje dla Claude Code

Projekt: silnik ogólnego użytku HTML + CSS + SVG → PDF w czystym Go, bez JavaScriptu, z obsługą CSS Paged Media.
Cele (`docs/vision.md`): zastąpić Chromium czymś co najmniej 10 razy mniejszym, mieć pełną kontrolę nad kodem,
renderować deterministycznie, pokrywać typowe dokumenty w wybranych ramach bez konkurowania z przeglądarką.
Przy sporze wygrywają rozmiar, przewidywalność, szybkość i czystość kodu, kosztem przypadków brzegowych.
Projekt jest agnostyczny wobec odbiorców; QMS i xml.pro to przyszli użytkownicy, nie źródło wymagań.

Zacznij od `README.md` (indeks), potem `docs/landscape.md` (co już istnieje, z pomiarami) i `docs/adr/open-questions.md`
(decyzje do podjęcia). Etapy 0 (rozpoznanie) i 1 (korpus testowy, `testdata/README.md`) są zamknięte; etap 2 to próba forka i benchmark
(`docs/benchmark.md`, `docs/roadmap.md`). Decyzja o strategii zapadła: ADR 0002, fork webrender w `internal/`,
stan przejętego kodu i testów w `docs/provenance.md`.

Zasady:
- nazwy plików, katalogów, pakietów i identyfikatorów po angielsku; treść dokumentów po polsku;
- wzorcem są normy (W3C, ISO), nie biblioteki: gdy przejęty kod albo jego testy różnią się od specyfikacji,
  poprawiamy według specyfikacji i odnotowujemy to w `docs/provenance.md`;
- każda decyzja = plik w `docs/adr/`; pytania bez decyzji w `docs/adr/open-questions.md`;
- w katalogu głównym tylko `go.mod`, `go.sum`, `LICENSE`, `NOTICE`, `README.md` (ADR 0052); API to pakiet `htmlpdf`,
  funkcje opcjonalne w osobnych pakietach (ADR 0049);
- kod to moduł `github.com/polestorm/gopdfgo` (jak moduły w `../controls/*`); układ pochodzi z forka webrender
  w `internal/` (ADR 0002), każda zmiana w przejętym kodzie trafia do `docs/provenance.md`;
- liczby (gwiazdki, linie kodu, rozmiary binarek) są datowane i podpisane sposobem pomiaru; szacunki oznaczone jako szacunki;
- kod przejęty do `internal/` jest nasz: wolno go budować, testować i uruchamiać. Innych pobranych repozytoriów
  (folio, go-webengine, cokolwiek nowego) nie uruchamiaj bez zgody użytkownika; ich klony i programy pomiarowe
  trzymamy poza gitem w `bench/`;
- renderowanie w testach i pomiarach czyta zasoby wyłącznie spod `testdata/`, fonty wyłącznie z `testdata/fonts`
  (z podkatalogami; fonty dla rodzin generycznych wskazuje `testdata/fonts/families.json`, ADR 0005);
- zależności tylko na licencjach MIT, BSD, Apache-2.0, OFL (fonty); LGPL/GPL wykluczone, bo binarka jest statyczna;
  zasoby wyłącznie testowe (nie trafiają do binarki) mogą być też w domenie publicznej lub na CC0 (font Ahem);
- porównania z innymi programami są powtarzalne (ADR 0055, reguła globalna z 2026-11-26): narzędzia w `tools/bench`
  i `tools/verify`, wsad w repozytorium albo z manifestem, metoda w `docs/benchmarks.md`, surowy wynik przebiegu
  w `docs/benchmarks/`; dokumenty prywatne (`compare.py -extra`) nie wchodzą do porównania; liczby w README
  odświeżamy nowym przebiegiem tych narzędzi, nie ręcznie;
- reftesty porównujemy według ADR 0003: zgodność ścisła albo z dokładnością do krawędzi (`pass-edge`), a każda
  podawana liczba zaliczeń rozróżnia obie miary;
- komunikaty programów (`gopdfgo -h`, `gopdfgo-sign`), ostrzeżenia w `Result.Warnings` i błędy API po angielsku
  (decyzja użytkownika z 2026-11-24, przed publikacją); komentarze w kodzie dalej po polsku;
  wyjątek: komentarze dokumentacyjne publicznego API (opis pakietu, eksportowane typy, funkcje, metody, stałe,
  zmienne i pola struktur w `htmlpdf/`, `sign/`, `sign/keyfile/`) piszemy po angielsku, bo pokazuje je
  pkg.go.dev (decyzja użytkownika z 2026-11-24); komentarze wewnętrzne zostają po polsku;
- opis commita kończy się jedną linią `Made with the help of Claude Code` (po pustym wierszu); nie dopisuj
  `Co-Authored-By` ani `Claude-Session` (decyzja użytkownika z 2026-11-24; tego dnia przepisano opisy wszystkich
  commitów, więc identyfikatory commitów sprzed tej daty zmieniły się, a w dokumentach są już nowe);
- opisy commitów po angielsku (decyzja użytkownika z 2026-11-20; wcześniejsza historia zostaje po polsku do decyzji
  o upublicznieniu repozytorium); README.md, docs/overview.md, docs/usage.md i docs/comparison.md po angielsku, reszta dokumentacji po polsku;
- commit i push po każdej zakończonej rundzie pracy, bez pytania (decyzja użytkownika z 2026-11-13); przed commitem
  muszą przechodzić testy własne (`go test ./tests/`, katalog `tests/`), a wynik testów przejętego kodu nie może się pogorszyć.
