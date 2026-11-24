# ADR 0002: silnik układu na bazie forka webrender, braki uzupełniane z folio

Status: przyjęta 2026-11-12 (decyzja użytkownika po ocenie w `../engine-fit.md`). Rozstrzyga pytanie 1
z `open-questions.md`. Warunek rewizji: benchmark z etapu 2b.

## Kontekst

Ocena statyczna trzech silników Go (`../engine-fit.md`) pokazała, że port WeasyPrint (`benoitkugler/webrender`,
BSD-3) ma poprawny model pudełek CSS i komplet Paged Media, a jego luki leżą na brzegach: `calc()`, PDF/A, tagowanie,
ustrukturyzowane ostrzeżenia. Folio (Apache-2.0) ma odwrotny profil: pełną warstwę PDF i luki w stronicowaniu
wynikające z modelu układu. Własny silnik od zera to 2–4 osobolata (`../estimates.md`).

Próba z 2026-11-12 (`bench/`, poza repozytorium): po usunięciu `text/engine_pango.go`, `text/draw/draw_pango.go`
i trzech odwołań do nich webrender `6e769b1` kompiluje się, `go list -deps` nie pokazuje żadnego pakietu
`benoitkugler/textprocessing` (LGPL), a binarka z go-weasyprint ma 26,8 MB (przed: 30,4 MB; macOS arm64, go1.26.6,
`CGO_ENABLED=0 -trimpath -ldflags='-s -w'`). Zostają zależności: `benoitkugler/pdf`, `pstokenizer`,
`go-text/typesetting`, `hhrutter/lzw`, `golang.org/x/{image,net,text}`; ich licencje do potwierdzenia plik po pliku.
Poprawność renderowania forka nie została jeszcze sprawdzona.

## Decyzja

1. Układ (`css`, `html/tree`, `html/boxes`, `html/layout`, `svg`, `text`) pochodzi z forka webrender bez silnika
   Pango. Fork przejmujemy do własnego modułu z zachowaniem nagłówków i pliku licencji BSD-3; zmiany względem
   upstream opisujemy w `docs/provenance.md`, poprawki ogólne zgłaszamy upstream.
2. Szwem jest interfejs `webrender/backend` (`Document`, `Page`, `Canvas`). Nasza lista wyświetlania (ADR 0001)
   implementuje go jako rejestrator; backendy czytają listę.
3. Z folio przejmujemy to, czego webrender nie ma, w tej kolejności sprawdzania:
   - **PDF/A i tagowanie**: `document/pdfa.go` (XMP, OutputIntent, profil sRGB) i `document/structtree.go` zależą
     tylko od pakietu `core`; `font.EmbeddedFont.EncodeGIDs` i `font.Subset` przyjmują identyfikatory glifów, a
     `content.Stream` ma `BeginMarkedContentWithID`. Wariant preferowany: backend PDF na pakietach folio
     (`core`, `content`, `font`, `document`) zamiast `benoitkugler/pdf`. Tagowanie wymaga rozszerzenia interfejsu
     `backend` o znaczniki struktury i przekazania ich z `html/document`.
   - **`calc()`, `min()`, `max()`, `clamp()`**: model wartości webrender (tokeny i walidatory z WeasyPrint) różni
     się od parsera folio (napisy), więc z folio bierzemy algorytm i testy, nie kod wprost; najpierw sprawdzamy,
     czy nowszy WeasyPrint ma `calc()` do przeniesienia.
   - **Determinizm i brak cichego ignorowania**: zasady z `ARCHITECTURE.md` folio jako wzorzec; ostrzeżenia
     webrender (`logger.WarningLogger`) zastępujemy zbieraniem do `Result.Warnings`.
4. Kod przejęty z folio trzymamy w osobnych pakietach z `LICENSE` i `NOTICE` Apache-2.0 oraz oznaczeniem
   zmienionych plików (pytanie 3 w `open-questions.md`).
5. Folio i Chromium zostają w benchmarku jako punkty odniesienia; go-webengine nie.

## Zmiana 2026-11-12 (po testach pikselowych)

Punkt 3 zakładał backend PDF na pakietach folio jako wariant preferowany. Po uruchomieniu testów (403 z 415
pikselowych przechodzi) i naprawieniu trzech wad miejscowych (`../benchmark.md` pkt 3) nie ma dowodu, że backend
`go-weasyprint/pdf` trzeba wymieniać. Obowiązuje: backend zostaje, z folio bierzemy PDF/A i tagowanie jako
uzupełnienia. Otwarty pozostaje determinizm, zależny od biblioteki `benoitkugler/pdf` (pytanie 14).
Przejęty kod traktujemy jak własny: podlega przeglądowi, refaktoryzacji i przepisaniu tam, gdzie tego wymaga.

## Konsekwencje

- Strategia A (od zera) i B (folio jako całość) odpadają, o ile benchmark nie pokaże, że fork nie renderuje
  korpusu w stopniu naprawialnym.
- Utrzymujemy fork: upstream ma jednego autora i zastrzega zmiany łamiące, więc synchronizacja jest ręczna.
- Rozmiar binarki ok. 27 MB przed optymalizacją; cel 20–25 MB z `estimates.md` wymaga przeglądu słowników
  dzielenia wyrazów (42 języki) i `net/http` wciąganego przez domyślny `UrlFetcher`.
- Reguła „do decyzji z etapu 3 repozytorium zawiera tylko dokumentację i korpus” przestaje blokować kod po
  potwierdzeniu tej decyzji benchmarkiem.
