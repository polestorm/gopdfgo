# Szacunki: rozmiar, pamięć, pracochłonność

Stan na 2026-11-12. Pomiary oznaczone „pomiar”, reszta to szacunki z pamięci i analogii; do zweryfikowania w etapie 2.

## 1. Rozmiar binarki

| Wariant | Rozmiar | Źródło |
|---|---|---|
| pusty program Go | 1,2 MB | pomiar |
| folio, `AddHTML` + `Save` | 9,0 MB | pomiar |
| port WeasyPrint (`go-weasyprint.HtmlToPdf`, fonty przez go-text `fontscan`) | 30,4 MB | pomiar |
| własny silnik wg `building-blocks.md` | 20–25 MB | szacunek: runtime 2, `x/net` + `x/text` 3, go-text 4, kodeki i backendy 3, silnik 5–8, wzorce dzielenia <1, osadzony font 2–4 |
| `chrome-headless-shell` | 150–200 MB | wartość z Chrome for Testing |
| Chromium z bibliotekami | ok. 400 MB | pakiet dystrybucji |
| własny build `headless_shell` z przyciętymi flagami GN | 100–150 MB | szacunek; odrzucone w `landscape.md` |

Pomiar: macOS arm64, go1.26.6, `CGO_ENABLED=0 go build -trimpath -ldflags='-s -w'`. U odbiorcy silnik dolicza się do
binarki serwera, więc liczy się przyrost, nie wartość bezwzględna: folio dodaje ok. 8 MB, port WeasyPrint ok. 29 MB.

## 2. Pamięć i czas (szacunek)

- Silnik dokumentowy w Go: 30–80 MB RAM na dokument kilkudziesięciostronicowy, 50–300 ms na dokument pięciostronicowy
  z tabelą i wykresem. Zależy głównie od kształtowania tekstu i podzbiorów fontów.
- Chromium: 150–300 MB RAM na proces, zimny start 0,5–1 s, potem 100–300 ms na dokument.
- Do zmierzenia w etapie 2 na korpusie.

## 3. Pracochłonność trzech strategii

| Strategia | Co robimy | Praca do „korpus przechodzi” | Ryzyko | Co zyskujemy |
|---|---|---|---|---|
| **A. Własny silnik od zera** | kaskada, układ, fragmentacja, Paged Media, backendy; klocki z `building-blocks.md` | 40–60 tys. linii, **2–4 osobolata**, długi ogon przypadków brzegowych | harmonogram; samotny silnik bez społeczności; walidacja od zera | pełna kontrola, determinizm od projektu, dokładnie nasz zakres |
| **B. Adopcja folio** | adapter za interfejsem `gopdfgo`; łatanie luk w upstream lub forku; własna walidacja nieobsługiwanego CSS przed renderowaniem; osadzone fonty; test determinizmu | **80–200 h** zależnie od wyników benchmarku | 8-miesięczny projekt, zmiany łamiące, kierunek wyznacza ktoś inny; ignorowanie nieznanego CSS po cichu | 9 MB, Apache-2.0, 15 kontrybutorów, PDF/A i podpisy w komplecie, grid |
| **C. Adopcja portu WeasyPrint** | jak B, plus rozstrzygnięcie LGPL (usunięcie `textprocessing` albo zgoda na jego licencję) | **80–200 h** + licencja | jeden autor; „breaking changes on the fly”; 30 MB | najpełniejsze Paged Media i GCPM, testy odziedziczone po WeasyPrint, architektura z abstrakcyjnym backendem jak w naszej |
| **D. Adapter nad kilkoma** | interfejs `gopdfgo` + adaptery Chromium, folio, weasy; wybór konfiguracją; korpus jako test zgodności | **40–60 h** (etap 4 w `roadmap.md`) | utrzymanie kilku adapterów | odwracalność; odbiorcy mogą używać interfejsu bez decyzji o silniku |

D nie wyklucza A–C, tylko je poprzedza. Rekomendacja robocza: D teraz, B lub C po benchmarku, A tylko jeśli benchmark
wykaże, że oba silniki Go są daleko od zakresu „musi” i upstream nie przyjmuje łatek.

## 4. Czy „tylko PDF” zamiast listy wyświetlania skraca pracę

Pytanie: gdyby silnik rysował od razu do PDF, bez warstwy pośredniej niezależnej od urządzenia, czy byłoby prościej?

**Krótko: nie w sposób zauważalny.** Lista wyświetlania to ok. 3–5% kodu silnika (typy operacji i jeden interfejs),
a wszystko, co jest drogie, leży przed nią:

| Składnik własnego silnika | Udział pracy (szacunek) | Czy zależy od formatu wyjścia |
|---|---|---|
| kaskada i computed values | 15% | nie |
| układ block, inline, tabele, float, position | 35% | nie |
| flex | 5% | nie |
| fragmentacja i Paged Media | 20% | nie |
| tekst: fonty, kształtowanie, dzielenie wyrazów | 10% | nie |
| SVG | 5% | nie |
| lista wyświetlania | 3% | to jest ta warstwa |
| backend PDF (strony, podzbiory fontów, obrazy, linki, XMP) | 5% | tak, potrzebny i tak |
| backendy SVG i PNG | 2% | tak, jedyne, co odpada |

Odpada więc ok. 2% pracy, a traci się:

- **testowalność**: złote testy układu porównują tekstową listę wyświetlania, nie bajty PDF; bez niej testy
  wymagają zewnętrznego rasteryzatora PDF (pdftoppm, mutool) w CI;
- **podgląd wydruku** w przeglądarce jako SVG na stronę z identycznym podziałem stron jak PDF;
- **wymienność backendu PDF**: bez listy wyświetlania zmiana biblioteki PDF (np. na taką z PDF/A) oznacza
  przepisanie malowania.

Wszystkie trzy silniki Go i WeasyPrint mają tę warstwę (`backend.Document`, `paint.Canvas`, `Surface`), więc przy
adopcji dostajemy ją za darmo. Co naprawdę skraca pracę, to **zwężenie wejścia**, nie wyjścia:

| Cięcie zakresu | Oszczędność (szacunek) | Koszt dla autora dokumentów |
|---|---|---|
| bez float i `position: absolute` | 8–10% | szablony do druku łatwo bez nich, ale CSS z ekranu je zawiera; trzeba pilnować w szablonach |
| bez flex | 5% | nagłówki zapisów przez tabele; ekran i druk przestają dzielić CSS |
| bez bidi i pism złożonych | 3–5% | brak dla rynku polskiego |
| bez `running()`, stron nazwanych, `target-counter` | 5% | stopka tylko ze stałych pól marginesowych; spis treści bez numerów stron |
| bez SVG | 5% | wykresy jako PNG, gorsza jakość druku |
| tabele tylko `table-layout: fixed` | 10% | rejestry wymagają ręcznych szerokości kolumn |

Jest jeszcze radykalne „tylko PDF”: porzucić HTML/CSS jako wejście i rysować z Go API (model folio, maroto, SynPdf).
To skraca pracę dziesięciokrotnie, ale oznacza drugi zestaw szablonów obok templ, co jeden z przyszłych odbiorców (QMS) odrzucił w
`architecture.md` (każdy typ pola z profilu opisany dwa razy). Nie wraca.
