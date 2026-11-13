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
15. **Współbieżność i zmienne globalne.** `Render` wykonuje jedno renderowanie naraz, bo silnik zgłasza ostrzeżenia
    przez globalne `logger.WarningLogger` (80 wywołań w 25 plikach), a limit czasu nie przerywa pracy. Decyzja
    użytkownika 2026-11-12: równoległość nie jest teraz potrzebna; docelowo pełna przebudowa bez zmiennych
    globalnych (kontekst renderowania niosący ostrzeżenia jako strukturę i sygnał przerwania), bez rozwiązań
    przejściowych. Termin otwarty; nowy kod nie powinien dokładać wywołań globalnego rejestratora.
13. ~~**Testy pikselowe forka.**~~ Rozstrzygnięte 2026-11-12: Ghostscript zainstalowany jako narzędzie testowe,
    testów nie przepisujemy. Zapis rozważań: 349 testów `internal/goweasyprint/pdf` rasteryzuje PDF programem `gs`
    (Ghostscript, AGPL, tylko jako narzędzie testowe). Instalować, przepisać na `pdftoppm`, czy zastąpić testami
    listy wyświetlania po wymianie backendu? Propozycja: nie instalować; przepisać razem z nowym backendem.
12. **Fonty w testach WPT.** 1306 testów z manifestu wymaga fontu Ahem; jego licencja nie jest opisana osobno
    w WPT. Propozycja: używać w testach, nie osadzać w binarce, potwierdzić licencję przed publikacją repozytorium.
