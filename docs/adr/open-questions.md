# Pytania otwarte

Stan na 2026-11-13. Każde pytanie ma wariant proponowany; decyzja trafia do osobnego ADR. Kryterium rozstrzygania
to cele z `../vision.md` (rozmiar, kontrola nad kodem, determinizm, pokrycie w wybranych ramach), nie potrzeby
konkretnego odbiorcy.

1. ~~**Strategia: własny silnik, adopcja, fork czy adapter nad kilkoma?**~~ Rozstrzygnięte 2026-11-12: ADR 0002,
   fork webrender jako układ, braki z folio. Poniżej zapis rozważań. Warianty A–D w `../estimates.md` pkt 3.
   Propozycja: D (interfejs + adaptery Chromium, folio, port WeasyPrint) od razu, wybór B/C po benchmarku z etapu 2,
   A tylko gdy benchmark wykluczy oba. Do rozstrzygnięcia po etapie 2.
   Dopisek 2026-11-12 po ocenie statycznej (`../engine-fit.md`): hipoteza zawęża się do C z mieszanką, czyli fork
   webrender jako układ, nasza lista wyświetlania na jego interfejsie `backend`, backend PDF wymienny (na start
   `go-weasyprint/pdf`, folio do sprawdzenia). Folio samodzielnie (B) ma luki w „musi”, których nie zamykają
   pojedyncze łatki: strony nazwane, `:blank`, 10 z 16 pól marginesowych, model układu inny niż pudełka CSS.
2. ~~**Czy silnik ma być „tylko PDF”, czy agnostyczny z listą wyświetlania?**~~ Rozstrzygnięte 2026-11-12: ADR 0001,
   lista wyświetlania zostaje; zwężeń szukamy w zakresie wejścia, nie wyjścia. Ocena w `../estimates.md` pkt 4.
3. **Polityka licencji.** Statyczna binarka wyklucza LGPL (`benoitkugler/textprocessing` w porcie WeasyPrint).
   Propozycja: tylko MIT/BSD/Apache-2.0/OFL; port WeasyPrint dopuszczalny po usunięciu `textprocessing`
   (autor to robi) albo w forku bez niego.
   Dopisek 2026-11-12: `textprocessing` importują już tylko `text/engine_pango.go` i `text/draw/draw_pango.go`
   (1215 linii) oraz narzędzia testowe; domyślny silnik tekstu to go-text. Fork bez tych plików jest zadaniem na
   godziny, nie tygodnie; do potwierdzenia kompilacją (etap 2a).
   Dopisek 2026-11-12, **przejmowanie obcego kodu z modyfikacjami**: Apache-2.0 (folio) i BSD-3 (webrender,
   go-weasyprint, go-webengine, go-pdfkit) pozwalają kopiować, zmieniać i wydawać pochodne, także na innej licencji,
   pod warunkiem zachowania nagłówków praw autorskich i tekstu licencji w przejętych plikach; Apache-2.0 dodatkowo
   wymaga oznaczenia zmienionych plików i przeniesienia pliku `NOTICE`, jeśli istnieje, oraz nie daje prawa do nazwy
   (nie nazywamy niczego „Folio”). BSD-3 zabrania użycia nazwisk autorów do promocji. Obie licencje dają jawną
   zgodę na użycie komercyjne; Apache-2.0 także licencję patentową. Pliki przejęte z różnych źródeł trzymamy
   w osobnych pakietach z własnym `LICENSE`, a rejestr pochodzenia w `docs/provenance.md`. Wykluczone pozostaje LGPL.
4. ~~**Wzorzec zgodności: Chromium czy specyfikacja?**~~ Rozstrzygnięte 2026-11-13 przez użytkownika: specyfikacja
   (`../vision.md`, „Wzorzec zgodności”); dotyczy też rozbieżności z WeasyPrint. Zapis rozważań: Przy sporze między tym, co robi Chrome, a tym, co mówi CSS,
   co wygrywa? Propozycja: podział stron i liczniki wg specyfikacji Paged Media (Chrome ma tu luki), reszta wg Chrome,
   bo ekran renderuje Chrome i autor szablonu patrzy na ekran.
5. **Fonty osadzone w binarce.** Który krój i ile odmian: Noto Sans (OFL, pełny polski, tabular numbers, 4 odmiany ≈ 2 MB)
   czy Inter? Czy dodatkowo serif i mono? Propozycja: Noto Sans 4 odmiany + Noto Sans Mono 1 odmiana; serif przez
   `@font-face` z profilu klienta.
6. **PDF/A: w silniku czy w kroku końcowym?** Folio deklaruje PDF/A, port WeasyPrint nie. Propozycja: wymaganie
   Y5 jako „powinien”, realizacja w backendzie PDF jeśli silnik ma, inaczej pdfcpu dopisuje XMP i OutputIntent;
   walidacja veraPDF w CI jako narzędzie, nie zależność.
7. **Relacja do `xml.pro/gohtmlpdf`.** Czy `gohtmlpdf` staje się adapterem `engine/chromium` w gopdfgo, a xml.pro
   przechodzi na interfejs gopdfgo? Propozycja: tak, w etapie 4; xml.pro nic nie traci, zyskuje wybór silnika.
8. **Miejsce modułu.** `github.com/polestorm/gopdfgo` obok `controls/*` (wspólne moduły kilku produktów) czy wewnątrz
   jednego z odbiorców? Propozycja: polestorm, bo silnik jest ogólnego użytku. Prawa do kodu do ustalenia.
9. **Raport nieobsługiwanego CSS przy adopcji.** Folio ignoruje nieznane właściwości po cichu. Czy wystarczy własny
   walidator CSS przed renderowaniem (lista dozwolonych właściwości z `scope.md`), czy trzeba łatać silnik?
   Propozycja: walidator w gopdfgo, niezależny od silnika; łatka upstream jako wkład.
10. **Determinizm obcych silników.** Nie zmierzono (uruchomienie obcych binarek odłożone do etapu 2). Jeśli silnik
    wstawia daty lub losowe `/ID`, czy wystarczy nadpisanie w kroku końcowym (pdfcpu), czy to dyskwalifikuje?
    Propozycja: nadpisanie dopuszczalne, jeśli reszta bajtów jest stabilna; test w etapie 2.
11. ~~**Uruchamianie obcych silników.**~~ Rozstrzygnięte 2026-11-12 przez użytkownika: kod przejęty do `internal/`
    wolno uruchamiać; pozostałe obce repozytoria tylko za zgodą (reguła w `CLAUDE.md`). Zapis rozważań: `CLAUDE.md` zabrania uruchamiania kodu z pobranych repozytoriów, a etapy
    2a–2c tego wymagają (kompilacja i wykonanie webrender, folio, opcjonalnie go-webengine na plikach z `testdata/`).
    Propozycja: dopuścić wyłącznie w programie pomiarowym w `/bench/`, z modułami przypiętymi do commitów
    z `../engine-fit.md`, bez dostępu do sieci w trakcie renderowania (własny `Resolver`/`UrlFetcher` odmawiający
    wszystkiego poza `testdata/`), po przejrzeniu `go.mod` każdego silnika. go-webengine tylko z tagami
    `webengine_nojs webengine_nonet` albo wcale.
14. ~~**Determinizm zapisu PDF.**~~ Rozstrzygnięte 2026-11-12: wariant (b), kopia pięciu pakietów w `internal/pdflib`
    z sortowaniem kluczy (`docs/provenance.md`); zgłoszenie upstream do zrobienia. Zapis rozważań: `benoitkugler/pdf` (MIT, ok. 110 tys. linii) zapisuje słowniki zasobów, iterując
    po mapach. Warianty: (a) poprawka upstream i czekanie, (b) własna kopia pakietów `model` i `contentstream`
    z sortowaniem kluczy, (c) backend na pakietach folio, które mają determinizm w założeniach. Propozycja: (b)
    teraz jako mała łatka plus zgłoszenie upstream, (c) rozważyć przy PDF/A.
16. ~~**Blok o podanej wysokości, który nie mieści się w reszcie strony.**~~ Rozstrzygnięte 2026-11-14
    (ADR 0004): przenoszenie w całości; zachowanie przeglądarek ewentualnie jako opcja w przyszłości. Zapis rozważań: Od 2026-11-14 blok wyższy niż cały
    obszar strony jest dzielony między strony (wcześniej wystawał poza stronę i jego dalsza część ginęła).
    Blok, który zmieściłby się na pustej stronie, jest nadal przenoszony w całości na następną, jak
    w WeasyPrint. Przeglądarki dzielą także taki blok w miejscu końca strony, o ile nie ma
    `break-inside: avoid`; CSS Fragmentation 3 nie rozstrzyga tego jednoznacznie (punkt podziału klasy C
    jest zdefiniowany przez odstęp od treści dzieci, a pusty blok dzieci nie ma). Do decyzji: zostawić
    przenoszenie (mniej niespodzianek w dokumentach z ramkami o stałej wysokości) czy dzielić jak
    przeglądarki. Ograniczenia obecnej wersji: nie są dzielone elementy flex i grid, treść kolumn, bloki
    z `overflow` innym niż `visible` ani bloki, których dzieci same zostały podzielone między strony.
17. **Pamięć dużych dokumentów: układ i zapis strona po stronie.** Po odchudzeniu stylów (2026-11-14) tabela
    4000 wierszy trzyma w szczycie 323 MB żywej sterty, prawie wyłącznie w pudełkach: drzewo wejściowe i ułożone
    strony całego dokumentu istnieją jednocześnie, a rysowanie zaczyna się po ułożeniu ostatniej strony
    (`docs/feature-audit.md`, „Czas i pamięć dużych dokumentów”). Warianty: (a) zostawić, bo 80 kB na wiersz
    wystarcza dla dokumentów do kilkuset stron; (b) rysować i zwalniać strony na bieżąco, gdy dokument nie używa
    `counter(pages)`, `target-counter` ani pól marginesowych zależnych od całości, a w pozostałych przypadkach
    działać jak dziś; (c) przebudować układ na strumieniowy także dla tych przypadków (dwa przebiegi).
    Propozycja: (b), po pomiarze na dokumencie 1000 stron i rozpoznaniu, jak rozdzielić układ, rysowanie i zapis PDF.
    Decyzja użytkownika 2026-11-14: odłożone, pomysł zanotowany na przyszłość; pierwszeństwo mają reftesty W3C.
    Stan 2026-11-21: (b) w ADR 0027, (c) dla dokumentów z ponowną paginacją w ADR 0036; zostają przypadki
    z punktu 6 ADR 0036.
18. ~~**Dolny margines dziecka w pudełku z `min-height` lub `max-height`.**~~ Rozstrzygnięte 2026-11-15 przez
    użytkownika: domyślnie jak w Chromium, drugie zachowanie pod jednym parametrem (ADR 0006). Zapis rozważań: Tekst CSS 2.2 § 10.7 mówi, że
    `min-height` i `max-height` nie wpływają na zwijanie marginesów, więc dolny margines ostatniego dziecka
    powinien zwijać się z marginesem rodzica i odsuwać następne pudełko; tak oczekuje test `margin-collapse-038`
    (2010). Przeglądarki i cztery nowsze testy WPT (`margin-collapse-min-height-001/002`,
    `min-height-separates-margin`, `max-height-separates-margin`) oczekują odwrotnie: gdy ograniczenie zmienia
    wysokość, margines nie wychodzi poza pudełko. Norma i testy są tu sprzeczne. Przyjęte roboczo 2026-11-15:
    zachowanie przeglądarek i nowszych testów (zysk 4 testy, strata 1), bo dokumenty powstają pod podglądem
    w przeglądarce. Do potwierdzenia przez użytkownika; odwrócenie to jeden warunek w `html/layout/block.go`.
19. ~~**Font dla rodzin generycznych (`serif`, `sans-serif`, `monospace`).**~~ Rozstrzygnięte 2026-11-15 przez
    użytkownika: wariant (b), ADR 0005. Zapis rozważań: Silnik wybiera font dla rodziny
    generycznej według tablicy podstawień biblioteki go-text (odpowiednik fontconfig): przy fontach z
    `testdata/fonts` i Tinos (`testdata/fonts/tinos`) `serif` daje Noto Serif, a bez niego Noto Sans, nigdy Tinos. Przeglądarki dają dla `serif`
    font o metrykach Times i pod to pisana jest część testów WPT. Pomiar 2026-11-15 (pełny sondaż ekranowy,
    Tinos 1.340 ustawiony jako font domyślny arkuszem użytkownika `html { font-family: Tinos }`): 6862 wobec
    6852 z Noto Serif, 30 testów zyskanych i 20 straconych. Część strat to skutek samego sposobu ustawienia
    (test z `* { font: inherit }` albo jawnym `serif` dostaje Noto, a jego wzorzec Tinos), część to testy pisane
    pod szerszy font. Warianty: (a) zostawić Noto Serif jako font domyślny pomiarów; (b) dodać do `Options`
    wskazanie fontów dla rodzin generycznych (przydatne też użytkownikom biblioteki, którzy chcą sami
    zdecydować, czym jest `serif`) i mierzyć z Tinos; (c) zmienić kolejność podstawień w silniku na stałe.
    Propozycja: (b). Do decyzji użytkownika; do tego czasu pomiary zostają na Noto Serif, żeby liczby były
    porównywalne z poprzednimi.

15. **Współbieżność i zmienne globalne.** `Render` wykonuje jedno renderowanie naraz, bo silnik zgłasza ostrzeżenia
    przez globalne `logger.WarningLogger` (80 wywołań w 25 plikach), a limit czasu nie przerywa pracy. Decyzja
    użytkownika 2026-11-12: równoległość nie jest teraz potrzebna; docelowo pełna przebudowa bez zmiennych
    globalnych (kontekst renderowania niosący ostrzeżenia jako strukturę i sygnał przerwania), bez rozwiązań
    przejściowych. Termin otwarty; nowy kod nie powinien dokładać wywołań globalnego rejestratora.
13. ~~**Testy pikselowe forka.**~~ Rozstrzygnięte 2026-11-12: Ghostscript zainstalowany jako narzędzie testowe,
    testów nie przepisujemy. Zapis rozważań: 349 testów `internal/goweasyprint/pdf` rasteryzuje PDF programem `gs`
    (Ghostscript, AGPL, tylko jako narzędzie testowe). Instalować, przepisać na `pdftoppm`, czy zastąpić testami
    listy wyświetlania po wymianie backendu? Propozycja: nie instalować; przepisać razem z nowym backendem.
12. ~~**Fonty w testach WPT.**~~ Rozstrzygnięte 2026-11-13 (ADR 0003): Ahem zostaje w `testdata/wpt/fonts`
    i jest ładowany przez `@font-face`; licencja wg pliku fontu to domena publiczna z zastrzeżeniem CC0 1.0. Zapis rozważań: 1306 testów z manifestu wymaga fontu Ahem; jego licencja nie jest opisana osobno
    w WPT. Propozycja: używać w testach, nie osadzać w binarce, potwierdzić licencję przed publikacją repozytorium.
    Pomiar 2026-11-13: w sondażu ekranowym Ahem występuje w 1703 z 9135 testów; 1088 z nich przechodzi mimo
    braku fontu, 610 nie przechodzi (5 błędów). Sprostowanie z 2026-11-14: font nie był brakujący, testy
    ładują go przez `@font-face`; te 610 testów to rzeczywiste różnice albo różnice krawędziowe.

## 20. Względem czego oceniać `width` i `height` w `@media`?

Rozstrzygnięte 2026-11-16: ADR 0012 (obszar strony z `@page`; bez parametru w `Options`).

## 21. Czy naśladować Chromium w obcinaniu `border-spacing` i w pomniejszaniu strony?

Rozstrzygnięte 2026-11-16: ADR 0013 (`border-spacing` obcinane domyślnie, pod wspólnym przełącznikiem
`ExactLengths`; pomniejszania strony nie naśladujemy, fit-to-page osobno w przyszłości).

## 22. Czy i w jakim zakresie obsłużyć pismo pionowe (`writing-mode`)?

Rozstrzygnięte 2026-11-16: ADR 0010 (tylko poddrzewo obrócone, i dopiero na potrzebę dokumentu).

## 23. Zestaw fontów przygotowany raz dla wielu dokumentów (odłożone)

Odłożone 2026-11-16 (decyzja użytkownika: zapisać na przyszłość). Pomiar z `docs/benchmark.md` § 9:
parsowanie fontów z `Options.Fonts` powtarza się przy każdym wywołaniu `Render` i dla sześciu fontów Noto
trwa ok. 10 ms z ok. 32 ms małego dokumentu; sparsowany font zajmuje ok. 1,1–1,5 MB (plik 380–470 kB).

Propozycja: `htmlpdf.PrepareFonts(fonts)` zwraca zestaw tylko do odczytu, przekazywany w `Options.FontSet`;
`Options.Fonts` zostaje. Zestaw trzyma program wywołujący (bez bufora i czasu retencji po stronie silnika,
binarka się nie zmienia), fonty z `@font-face` dokumentu nie są nim objęte. Odrzucona alternatywa:
wewnętrzna pamięć podręczna według skrótu treści (stan wspólny między wywołaniami).

Do sprawdzenia przed wdrożeniem: czy go-text pozwala dzielić sparsowany `font.Font` między równoległymi
renderowaniami bez blokady; wynik z zestawem ma być bajt w bajt taki sam jak bez niego (test).

## 24. Pismo od prawej do lewej: algorytm dwukierunkowy (odłożone)

Odłożone 2026-11-17 (decyzja użytkownika: nie potrzebujemy). Silnik obsługuje `direction: rtl`
w uproszczeniu: odwraca kolejność pudełek w wierszu, bez algorytmu dwukierunkowego Unicode (UAX 9)
działającego w poprzek elementów. Tekst łaciński w akapicie `rtl` oraz mieszany tekst arabski lub
hebrajski z liczbami i nawiasami bywa przez to w złej kolejności.

Koszt: szacunek 2–3 paczki. Zysk w sondażu W3C: ok. 55 testów w CSS 2 (`bidi-*`, `unicode-bidi-*`)
i kilkadziesiąt w innych obszarach (flex, siatka, tekst: `pre-wrap-align-*`, `letter-spacing-bidi-*`).
Do czasu decyzji testy wymagające pisma od prawej do lewej pomijamy przy wyborze poprawek.

## 25. Czy dokumenty XHTML parsować parserem XML?

Otwarte od 2026-11-17. Dokument z przestrzenią nazw XHTML jest dziś poprawiany tekstowo (`xhtml.go`:
rozwijanie `<div/>`, prefiksy przestrzeni nazw) i czytany parserem HTML5. Parser HTML5 stosuje własne
naprawy drzewa, których XML nie zna: tekst bezpośrednio w `<table>` wynosi przed tabelę, `<p>` zamyka
się przed elementem blokowym, zagnieżdżone `<a>` są rozdzielane. Dokument XHTML, który z tego korzysta
(np. wynik XSLT z blokiem w akapicie), dostaje inne drzewo niż w przeglądarce.

W sondażu W3C widać to we wzorcach kilku testów flex (`flexbox-align-self-baseline-horiz-*`: tekst
w `<table>`); szacunek zysku to kilka do kilkunastu testów. Koszt: zbudowanie drzewa węzłów z
`encoding/xml` (encje XHTML, CDATA, przestrzenie nazw SVG i MathML) i ryzyko dla ok. 5800 testów CSS 2
w `.xht`, które dziś przechodzą ścieżką HTML5. Możliwy wariant ostrożny: parser XML, a przy błędzie
składni powrót do obecnej ścieżki.

## 26. PDF z tagami (PDF/UA): struktura odnośników

Otwarte od 2026-11-21. Silnik nie tworzy drzewa struktury (`StructTreeRoot`, `/MarkInfo`), więc odnośniki
HTML i SVG (ADR 0029) są tylko adnotacjami `/Link` bez elementu struktury `Link` z odwołaniem `OBJR`
i bez `/StructParent` (ISO 32000-1 § 14.7.4.4; PDF/UA-1, ISO 14289-1 § 7.18.5, wymaga też `/Contents`
z opisem odnośnika). ADR 0002 przewiduje tagowanie z folio; gdy powstanie, odnośniki SVG powinny dostać
element `Link` w miejscu elementu `a` z tekstem alternatywnym z `title`/`aria-label` albo treści tekstowej.

## 27. Zbędne obiegi paginacji i `target-counter(…, pages)`

Rozstrzygnięte 2026-11-21 przez użytkownika: naprawić oba; dopisek w ADR 0036. Strony z odwołaniami do
kotwicy są oznaczane do ponownego układu tylko przy zmianie liczników stron kotwicy (książka ze spisem
treści i „Strona X z Y”: 8 obiegów → 3, czas −10–12%, wynik bajt w bajt ten sam), a `target-counter(…, pages)`
podaje liczbę stron dokumentu (wynik zmienia się tylko w dokumentach z takim odwołaniem; idą teraz drogą
dwuprzebiegową).

## 28. Dywiz na początku słowa (UAX #14 LB20a) i wdowy w krótkich blokach

Otwarte od 2026-11-22 (rozbiór dokumentacji Markdown wobec Chromium, `docs/feature-audit.md`).

1. UAX #14 od Unicode 15.1 nie łamie po dywizie na początku słowa (LB20a: „-update”, „-safe”,
   „-refalign=style”); Chromium 157 łamie („-|update”), także we wstawce kodu na początku komórki. To
   główna przyczyna pozostałych różnic w dokumentach `md-gomdgo-testing` i `md-gomdgo-differences` (opcje
   wiersza poleceń w wąskich kolumnach). ADR 0038 zostawia normę; przejście na zachowanie Chromium to jedna
   zamiana w `breakAfterSpaces` (dywiz po spacji albo na początku tekstu przed literą).
2. Blok o mniejszej liczbie wierszy niż `orphans + widows` (domyślnie 3 wiersze przy 2 + 2): Chromium dzieli
   go 2 + 1, łamiąc `widows`, choć przerwa przed blokiem spełnia wszystkie reguły; gopdfgo przenosi blok
   w całości (CSS Fragmentation 3 § 4.4, reguła 3, i § 4.5). W `md-gopdfgo-benchmark` to samo w komórce
   tabeli przesuwa resztę dokumentu (273 słowa na innej stronie). Zostajemy przy normie, chyba że użytkownik
   zdecyduje inaczej.

Rozstrzygnięte 2026-11-22 przez użytkownika: w obu punktach zostajemy przy normie („normy na pierwszym
miejscu”). Gdyby zgodność z Chromium kosztem norm okazała się potrzebna, wejdzie jako jawna opcja opt-in
(np. tryb zgodności z Chromium), domyślnie wyłączona i opisana w osobnym ADR; obecny `Options.ExactLengths`
działa w odwrotnym kierunku (domyślne zaokrąglenia jak w Chromium tam, gdzie norma zostawia wybór).

## 29. Szerokość tekstu w górę do LayoutUnit a sondaż CSS; szerokość układu przy pomniejszaniu

Otwarte od 2026-11-22 (ADR 0044).

1. Szerokość kawałka tekstu zaokrąglana w górę do 1/64 px (jak w Chromium) daje w sondażu CSS 17 strat
   (reftesty, w których test i wzorzec dzielą tekst na inne pudełka; przesunięcie o ułamek piksela).
   Szerokość dokładna zostawia 10 z nich, ale przywraca dwa złamania wierszy niezgodne z Chromium
   w `bench/realdocs`. Do decyzji: zostać przy LayoutUnit (wierność Chromium) czy przy szerokości
   dokładnej (zgodność sondażu); zmiana to jeden warunek w `preciseLineWidth`.
2. Chromium układa pomniejszany dokument na szerokości całkowitej, ale
reguła zaokrąglenia nie jest ustalona: na obszarze A5 (409 px) każdy pomiar to obcięcie w dół (450,9 px
daje 450, 480,5 px daje 480), na A4 (643 px) próg ułamka rośnie z szerokością (700,5 px daje 700,
700,6 px daje 701; 912,6 px daje 912, 912,7 px daje 913). gopdfgo obcina w dół, co na `bench/realdocs`
daje najmniej rozbieżności; dokument z treścią szeroką na 912,86 px (`rand-08`) ma przez to o 1 px węższe
wiersze niż w Chromium. Pomiary: `bench/lw/m10.py` (poza gitem). Do ustalenia, czy reguła wynika
z przeliczania na punkty drukarki, czy z innego miejsca; norma tego nie opisuje.

Punkt 1 rozstrzygnięty 2026-11-22 przez użytkownika: zostajemy przy szerokości w górę do LayoutUnit
(wierność Chromium), sondaż CSS 9484. Sprawdzenie w Chrome 157 (`bench/lw/chromesv.py`): z 20 utraconych
testów 9 Chrome też nie zalicza, 9 ma układ jak w Chrome i różni się tylko rastrem (przesunięcie
0,01–0,03 px, które raster Chrome ukrywa, a `pdftoppm` pokazuje), 1 (`hyphens-auto-004`) przechodził
przypadkiem (wiszenie odstępów innych niż spacja), 1 (`trailing-other-space-separators-002`) nierozebrany,
Chrome też go nie zalicza. Norma nie określa precyzji obliczeń (ADR 0013), `Options.ExactLengths` daje
wartości dokładne. Punkt 2 pozostaje otwarty.
