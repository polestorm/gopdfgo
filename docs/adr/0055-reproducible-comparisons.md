# ADR 0055: Powtarzalne porównania: narzędzia, wsad i surowe wyniki w repozytorium

Status: przyjęta 2026-11-26 (decyzja użytkownika; reguła globalna „Porównania z konkurencją” z tego dnia).

## Kontekst

README i `docs/comparison.md` porównują gopdfgo z Chromium i folio: czas, pamięć, rozmiar programu, rozmiar
PDF, wierność wobec Chromium, wyniki reftestów W3C, powtarzalność bajtów. Do 2026-11-26 wszystkie narzędzia
tych pomiarów leżały w `bench/` (poza gitem): `bench/folio/run.py` z programami `fharness` i `gharness`,
`bench/survey/tools/driftalign.py`, `bench/verify/{css,svg,corpus}.sh`, `w3cbytes.py`, `survey96e.py`, stany
bazowe sondaży. Skrypty miały wpisane ścieżki z katalogu domowego, a moduł folio był podpięty przez `replace`
do lokalnego klonu. Nikt z zewnątrz nie mógł powtórzyć żadnej liczby.

Audyt z 2026-11-26 wskazał też: 13 z 35 dokumentów testu wierności spoza repozytorium (część poufna), WPT
z lokalnej kopii, sprzeczne liczby w dokumentach (szybkość wobec Chromium, stosunek do folio, rozmiar programu),
pamięć Chromium podaną jako suma dwóch instancji, medal za rozmiar PDF faktur bez pokrycia w jednakowych
ustawieniach, wiersze tabeli z różnych dat i commitów.

## Decyzja

1. Każda liczba porównania z innymi programami prowadzi do wsadu i narzędzia w repozytorium oraz do opisu
   metody (`docs/benchmarks.md`, po angielsku); surowy wynik przebiegu, z którego pochodzą liczby README, jest
   w `docs/benchmarks/RRRR-MM-DD-*.json`. Liczba bez tej drogi nie trafia do README.
2. Narzędzia:
   - `tools/bench/`: `compare.py` (gopdfgo i folio na tych samych dokumentach, środowisko, polecenia, sumy
     SHA-256 programów i surowe wyniki w JSON), `gopdfgo-harness` (pakiet naszego modułu),
     `folio-harness` (osobny moduł z folio przypiętym w `go.mod`, więc folio nie jest zależnością gopdfgo),
     `wordalign.py` (położenia słów wobec Chromium), `summary.py` (liczby tabeli z JSON), `chromium.sh`
     (wzorce Chromium z zapisem wersji);
   - `tools/verify/`: `fetch-wpt.sh` (WPT z commita w `testdata/wpt/UPSTREAM`), `survey.py`, `css.sh`,
     `svg.sh`, `survey-diff.py` ze stanami bazowymi w `tools/verify/baselines/` (przebieg z 2026-11-24, z którego
     pochodzą liczby README), `w3cbytes.py` (powtarzalność bajtów, 46 756 renderowań), `corpus.sh`.
   Skrypty w Pythonie używają tylko biblioteki standardowej i biegną z `python3 -I`; poza Go potrzebny jest
   Poppler (`pdftotext`, `pdfinfo`, `pdftoppm`). Wyniki robocze idą do `bench/out/` (poza gitem).
3. Do porównania z konkurencją wchodzą tylko dokumenty publiczne: korpus `testdata/documents` z wzorcami
   Chromium, książka pobierana z manifestem SHA-256 (`testdata/books/fetch.sh`) i, w kolejnych krokach,
   publiczne odpowiedniki dokumentów spoza repozytorium (zmyślone dane o tej samej budowie, generatory
   deterministyczne). `compare.py -extra` dalej pozwala mierzyć dokumenty prywatne, ale oznacza je
   `"public": false`, a `summary.py` ich nie liczy.
4. Długie przebiegi (sondaż CSS, `w3cbytes.py`) idą według reguły globalnej o długich obciążeniach (Synology
   albo noc na zasilaniu); pomiar czasu i pamięci trzech programów robimy na jednej maszynie, w jednym przebiegu,
   na jednym commicie, i podajemy sprzęt.

## Konsekwencje

- Krok 1 (ten ADR): narzędzia w repozytorium, sprawdzone na korpusie (`compare.py` na dokumencie 01, sondaż SVG
  194 z 235 bez strat, `corpus.sh` fail=0). Stary `bench/folio`, `bench/verify` i `bench/survey/tools` zostają
  lokalnie jako archiwum.
- Następne kroki, po kolei:
  2. publiczne odpowiedniki 13 dokumentów spoza repozytorium (faktury, e-SF, dokumenty biurowe z danymi
     zmyślonymi; strony Markdown i mermaid z publicznych repozytoriów; `rand-*` z generatora w `tools/bench`)
     z wzorcami Chromium w `testdata/`;
  3. `docs/benchmarks.md` (metoda), jeden przebieg wszystkich programów na jednym commicie, surowy wynik
     w `docs/benchmarks/`, odświeżenie README i `docs/comparison.md`; pomiar czasu i pamięci Chromium
     dochodzi do narzędzi w tym kroku;
  4. uzgodnienie liczb w `docs/overview.md`, `docs/chromium-comparison.md`, `docs/benchmark.md`
     (starsze pomiary oznaczone datą jako historyczne), rozdzielenie zaliczeń ścisłych i krawędziowych
     w README (ADR 0003: 9020 + 464 z 10962, SVG 191 + 3 z 235).
- Do czasu kroku 3 liczby README pochodzą z pomiarów z 2026-11-20–24 opisanych w `docs/benchmark.md`.
- Krok 4 wykonany 2026-11-26: README podaje 20,5 MB, 3–14 razy szybciej od Chromium (całe procesy, 9 dokumentów
  spoza repozytorium, oznaczone w przypisie jako niepowtarzalne z zewnątrz do kroku 2), pamięć Chromium z dopiskiem
  o dwóch instancjach, bez medalu za rozmiar PDF faktur (sprzeczne pomiary 2026-11-20 i później), zaliczenia
  reftestów ścisłe i krawędziowe w przypisie; `docs/overview.md` z pomiarem folio z 2026-11-24 (`c0a9f7d`, 2,0×)
  i aktualnym rozmiarem programu; `docs/chromium-comparison.md` i `docs/benchmark.md` oznaczone jako historyczne.
