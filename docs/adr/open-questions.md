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
   w osobnych pakietach z własnym `LICENSE`, a rejestr pochodzenia w `THIRD_PARTY.md`. Wykluczone pozostaje LGPL.
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
    z sortowaniem kluczy (`../../THIRD_PARTY.md`); zgłoszenie upstream do zrobienia. Zapis rozważań: `benoitkugler/pdf` (MIT, ok. 110 tys. linii) zapisuje słowniki zasobów, iterując
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
