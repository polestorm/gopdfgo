# Przegląd istniejących silników

Stan na 2026-11-12. Gwiazdki, daty i rozmiary repozytoriów z GitHub API z tego dnia. Linie kodu policzone na płytkich
klonach (`*.go` bez testów / testy). Rozmiary binarek zmierzone lokalnie: macOS arm64, go1.26.6,
`CGO_ENABLED=0 go build -trimpath -ldflags='-s -w'`, minimalny program wywołujący konwersję HTML → PDF; punkt
odniesienia: pusty `main` = 1,2 MB. Pomiar na linux/amd64 będzie zbliżony.

## 1. Silniki przeglądarkowe

| Silnik | HTML → PDF bez okna | Rozmiar | Uwagi |
|---|---|---|---|
| Chromium headless (`--headless=new`) | `Page.printToPDF` przez DevTools | ok. 400 MB z bibliotekami | to, co robiło xml.pro przez chromedp; pełna przeglądarka; pakiet w dystrybucjach, aktualizacje z systemem |
| `chrome-headless-shell` | ten sam `printToPDF` | 150–200 MB | stary tryb headless wydzielony od Chrome 132 jako osobna binarka Chrome for Testing; ten sam Blink i Skia, więc identyczny PDF; bez rozszerzeń, polityk, części API; bez nowych funkcji; nie ma go w `apt`; buduje go też `chromedp/headless-shell` i Playwright |
| Firefox (Gecko) | `firefox --headless --print-to-pdf`, WebDriver BiDi `browsingContext.print` | ok. 250 MB | brak pól marginesowych `@page`, więc „strona X z Y” nie wychodzi z CSS |
| Safari / WebKit | brak trybu headless; PDF tylko przez API macOS; WebKitGTK przez Cairo | | wkhtmltopdf (QtWebKit) martwy od 2020 |
| Opera, Edge, Brave, Vivaldi | Chromium | | ten sam wynik, co Chromium |

Wyodrębnienie renderera z Chromium nie jest możliwe: Blink, V8 (DOM jest związany z V8 nawet przy wyłączonym JS),
Skia i warstwa `content` są nierozdzielne. Własny build `headless_shell` z wyłączonymi flagami GN (bez GPU, FFmpeg,
WebRTC, PDFium, rozszerzeń, z przyciętym ICU) dałby szacunkowo 100–150 MB, kosztem utrzymywania forka konfiguracji
w rytmie wydań Google (co 4 tygodnie główna wersja, co tydzień łatki). Electron utrzymuje ponad sto łatek i zespół do
samych aktualizacji. Zysk ok. 30% rozmiaru wobec gotowego `chrome-headless-shell`. Odrzucone.

## 2. Silniki dokumentowe spoza Go

| Silnik | Język, rozmiar | Co ma | Czego nie ma | Uwagi |
|---|---|---|---|---|
| WeasyPrint (Kozea, 9,8 tys.★, BSD-3) | Python, ok. 30 tys. linii + Pango, HarfBuzz, Cairo, fontTools; ok. 100 MB z runtime | CSS 2.1 w całości, flex, grid (nowsze wersje), Paged Media z polami marginesowymi, `running()`, `string-set`, `target-counter`, przypisy, dzielenie wyrazów, bidi, SVG, PDF/A, PDF/UA | JS, animacje, `sticky` | wzorzec klasy „silnik dokumentowy”; 12+ lat testów regresji; runtime Pythona w IQ |
| Prince (YesLogic, komercyjny) | własny silnik, jedna binarka ok. 20 MB | najlepszy CSS Paged Media i GCPM na rynku, PDF/A, PDF/UA, JS opcjonalny | | licencja na serwer; dowód, że silnik dokumentowy mieści się w 20 MB |
| Typst | Rust, ok. 30 MB | deterministyczny PDF, własny język składu | HTML/CSS | drugi zestaw szablonów; nie dla nas |
| Delphi HTML Component Library 4.8 (Sviridenkov, komercyjna) | Pascal, rdzeń ok. 105 tys. linii, cała biblioteka 330 tys. | CSS 2.1, flex, kolumny, zmienne, `calc`, transformacje, gradienty, własny renderer SVG, `@page` z rozmiarem, sekcje z nagłówkiem i stopką, licznik stron w raportach, dzielenie wyrazów, PDF przez SynPDF/Skia/własny zapis, kanwy GDI/GDI+/Direct2D/Skia/Cairo | Grid, `sticky`, `:has`, pola marginesowe `@page`, `orphans/widows`, JS (skrypty w Pascalu), WOFF2 bez dodatku, PDF/A | przykład, że 100 tys. linii wystarcza na nasze dokumenty; fonty i kształtowanie z kanwy systemowej |
| Blitz (DioxusLabs, 4,4 tys.★, Apache-2.0) | Rust: Stylo (CSS Servo), Taffy (flex, grid), Parley (tekst), Vello | nowoczesny CSS bez JS, ekran | Paged Media, PDF | eksperymentalny; pokazuje, że „HTML + CSS bez przeglądarki” ma dziś zaplecze w Rust |

## 3. Silniki w czystym Go

To najważniejszy wynik rozpoznania: w listopadzie 2026 istnieją trzy silniki układu HTML/CSS w Go, dwa z nich młodsze
niż rok. Żaden nie był znany przed tym przeglądem.

| Projekt | Co to | Linie (kod / testy) | ★ | Powstał | Ostatni push | Licencja | cgo | Binarka (pomiar) |
|---|---|---|---|---|---|---|---|---|
| `benoitkugler/webrender` + `go-weasyprint` | port WeasyPrint (układ) + backend PDF na `benoitkugler/pdf` | 52 tys. / 31 tys.; backend 1,4 tys. | 50 / 13 | 2021-12 | 2026-10 | BSD-3; zależność `textprocessing` **LGPL-2.1** (port Fribidi/Pango/FontConfig), w trakcie usuwania („pango removal” w ostatnich commitach) | nie | **30,4 MB** |
| `carlos7ags/folio` | biblioteka PDF z własnym silnikiem układu i konwersją HTML → PDF | 100 tys. / 106 tys.; 3880 funkcji testowych | 880 | 2026-03 | 2026-10 | Apache-2.0 | nie (opcjonalny eksport C ABI w `export/`) | **9,0 MB** |
| `go-webengine/engine` + `go-pdfkit/html2pdf` | „headless web engine bez Chromium”: DOM, kaskada, układ, raster; PDF wektorowy przez `go-pdfkit` | repozytorium 694 MB (zawiera korpusy); nie liczone | 3 / 1 | 2026-08 | 2026-11 | BSD-3 | nie | nie mierzono; zależności obejmują goja (JS) i esbuild, więc duża |
| `boxesandglue` + `bagme` | składacz w modelu TeX (pudełka i klej), `bagme` renderuje fragmenty HTML | 263★ | | | 2026-11 | BSD-3 | nie | nie jest silnikiem CSS; źródło algorytmu łamania akapitów |
| `nathanstitt/omnidoc` (d. doctaculous) | „document toolkit”, własny silnik CSS 2.1, wyjście rastrowe | 6★ | | 2026-06 | 2026-11 | MIT | | za młody i za mały, by oceniać |

### 3.1 webrender / go-weasyprint

- Port WeasyPrint z zachowaniem struktury: `css` (16 tys. linii: parser, walidacja, selektory, liczniki),
  `html/tree`, `html/boxes`, `html/layout` (12 tys.: block, inline, float, tabele, flex, grid, kolumny, absolute),
  `html/document`, `svg` (4,5 tys.), `text` (go-text, dzielenie wyrazów ze słownikami w binarce), `backend` (interfejs
  wyjścia, 500 linii).
- Zakres Paged Media odziedziczony po WeasyPrint: `@page`, pola marginesowe, `orphans/widows`, `running()`,
  `string-set`, `target-counter`, przypisy. Brak `sticky`, brak WOFF2.
- Testy: 31 tys. linii, w dużej części przeniesione z WeasyPrint, co jest największą wartością tego portu.
- Autor pisze: „usable, but use carefully in production; breaking changes may be committed on the fly”. Dwóch
  kontrybutorów, jeden główny (współautor go-text/typesetting). Port nie deklaruje, której wersji WeasyPrint odpowiada.
- Rozmiar 30 MB wynika z tablic Unicode, słowników dzielenia wyrazów i `textprocessing`. Po usunięciu Pango powinien
  zmaleć.
- Ryzyko licencyjne: LGPL-2.1 w `textprocessing` przy statycznym linkowaniu. Do rozstrzygnięcia przed adopcją.
  Uzupełnienie 2026-11-12: zależność siedzi w dwóch plikach silnika Pango, obok jest już silnik go-text;
  szczegóły w `engine-fit.md` pkt 3.2.

### 3.2 folio

- Biblioteka PDF pierwsza, HTML drugie: własny model dokumentu (`layout.NewParagraph` itd.), a `AddHTML` konwertuje
  HTML na ten model. Własny stos fontów (parser CFF i TrueType, GSUB/GPOS, podzbiory, fallback), kształtowanie
  LTR/RTL/Indic/CJK, zależności tylko `x/image`, `x/net`, `x/text`. Wymaga Go 1.26.
- CSS: 139 właściwości w `docs/CSS_SUPPORT.md` (generowanym z kodu): typografia, tło, model pudełkowy, obramowania,
  układ, flex (11), grid (16), kolumny (9), tabele z colspan/rowspan, `orphans/widows`, `break-*`, liczniki,
  `string-set`, pola marginesowe `@page`, `bookmark-*`, cienie, gradienty, transformacje 2D, `calc/min/max/clamp`.
  Selektory: kombinatory, atrybuty z operatorami, `:root`, `:empty`, `:first/last-child` i dalsze; `::before/::after`.
- Braki deklarowane: WOFF2, `::first-letter`, `::first-line`, pseudoklasy stanu, `target-counter`, kolory poza sRGB,
  `conic-gradient`, transformacje 3D. Nieznane właściwości są **ignorowane po cichu** (dokumentacja mówi to wprost),
  co łamie nasze wymaganie Y8 i wymaga łatki lub własnej walidacji przed renderowaniem.
- Dodatki poza naszym zakresem, ale przydatne przy archiwizacji dokumentów: PDF/A, podpisy cyfrowe, formularze, kody kreskowe i QR,
  import stron, ZUGFeRD. SDK Java i plac zabaw WASM.
- Pomiar 2026-11-23 (`docs/benchmark.md` § 11, commit `29bca95`): na 36 dokumentach w procesie mediana 2,9 raza
  szybsze od gopdfgo, ale liczba stron jak w Chromium tylko w 17 z 35 (gopdfgo 33) i 35% słów na tej samej
  stronie co w Chromium (gopdfgo 92%); bez ostrzeżeń i awarii.
- Dynamika: 8 miesięcy istnienia, 15 kontrybutorów, 32 otwarte zgłoszenia, 58 forków. Szybki rozwój oznacza też
  zmiany łamiące (jest `MIGRATING.md`).

### 3.3 go-webengine / html2pdf

- Ambicja największa: pełny silnik przeglądarkowy w Go z JS (goja), flex, grid, tabelami, `position`, SVG,
  malowaniem do rastra, a od niedawna PDF wektorowy z tagowaniem (struktura logiczna z DOM). Paginacja z `orphans/widows`,
  `break-*`, `@page size/margin`; bez stron nazwanych i `:first`. Autor sprawdza podziały stron względem Chrome.
- Ekosystem ok. 10 modułów jednego autora (`go-gfx`, `go-images`, `go-opentype`, `go-widgets`, `go-pdfkit`), wszystkie
  z 2026 roku, 0–3 gwiazdki. Wymaga Go 1.27.
- Dla nas za młody i za szeroki (JS, sieć, esbuild w zależnościach), ale warto obserwować: jeśli dojrzeje, będzie
  najbliższy zgodności z Chromium.

## 4. Wnioski z przeglądu

1. „Napisać od zera” przestało być jedyną drogą do silnika w Go. Folio i port WeasyPrint pokrywają większość zakresu
   „musi” ze `scope.md`; które luki są realne, pokaże korpus (etap 1–2).
2. Rozmiar potwierdzony pomiarem: 9 MB i 30 MB wobec 150–400 MB Chromium. Szacunek 20–25 MB dla własnego silnika
   leży między nimi.
3. Żaden z silników Go nie ma dwóch lat produkcyjnego użycia. Adopcja oznacza współpracę z upstream albo fork,
   nie „go get i zapomnij”.
4. Interfejs z `architecture.md` i adapter Chromium pozwalają odłożyć wybór silnika bez blokowania odbiorców.
