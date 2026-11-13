# Architektura

Stan na 2026-11-12. Opisuje potok docelowy niezależnie od tego, czy silnik powstaje od zera, czy jest adoptowany.
W wariancie adopcji pakiety `css`, `layout`, `paginate` zastępuje obcy silnik za adapterem, a reszta zostaje.

## Potok

```
html []byte ──► dom (x/net/html) ──► cascade (computed values) ──► boxes (drzewo pudełek)
   ──► layout (block, inline, table, flex, float, position)
   ──► paginate (fragmentacja, @page, pola marginesowe, liczniki, nagłówki bieżące)
   ──► displaylist (strony × lista operacji rysowania)
   ──► backend: pdf | svg | png
```

Granicą jest **lista wyświetlania**: po układzie dokument jest ciągiem stron, a każda strona ciągiem operacji
(wypełnij ścieżkę, obrysuj, narysuj glify fontu F w pozycjach, wstaw obraz, przytnij, zmień przezroczystość,
zacznij/zakończ grupę z transformacją). Lista nie zna HTML ani PDF. Dzięki temu:

- backendy są małe (PDF ok. 2–3 tys. linii, SVG i PNG po kilkaset) i testowalne osobno;
- testy układu porównują listy wyświetlania, nie bajty PDF, więc są stabilne;
- podgląd wydruku w przeglądarce to SVG na stronę z tego samego układu, co PDF.

Projekty, które wykonują ten sam cięcie: WeasyPrint (`draw` na abstrakcyjnym `Surface`), jego port webrender
(`backend.Document`), go-webengine (`paint.Canvas`), biblioteka Delphi (`THtCanvas` i `THtPagedExporter`),
tdewolff/canvas (jeden cel rysowania, wiele wyjść).

## Pakiety (moduł `github.com/polestorm/gopdfgo`)

| Pakiet | Zadanie | Od zera / gotowe |
|---|---|---|
| `gopdfgo` | interfejs publiczny: `Render`, `Options`, `Result`, `Resolver`; rejestr silników | od zera, mały |
| `internal/dom` | drzewo z `x/net/html`, atrybuty, `lang`, przestrzeń SVG | gotowe + cienka warstwa |
| `internal/css` | tokenizer i parser (tdewolff/parse), selektory (cascadia), walidacja wartości, kaskada, computed values, `var()`, `calc()`, `@page`, `@font-face` | parser i selektory gotowe; kaskada od zera |
| `internal/boxes` | generowanie pudełek z DOM i stylów, anonimowe pudełka tabel i inline | od zera |
| `internal/layout` | block, inline (łamanie wierszy, bidi, dzielenie wyrazów), tabele, float, position, flex (kjk/flex lub własny) | od zera poza flex |
| `internal/paginate` | fragmentacja, `@page`, pola marginesowe, liczniki stron, `string-set`, `running()` | od zera |
| `internal/text` | dobór fontów, kształtowanie (go-text/typesetting), segmentacja, hyphenation (speedata) | gotowe + sklejka |
| `internal/svg` | SVG → lista wyświetlania (oksvg jako punkt wyjścia albo własny parser) | adaptacja |
| `internal/displaylist` | typy operacji, serializacja do testów (tekstowa, porównywalna diffem) | od zera, mały |
| `backend/pdf` | PDF 1.7: strony, podzbiory fontów (CFF i TrueType), `ToUnicode`, obrazy, linki, zakładki, XMP; opcja PDF/A | od zera albo tdewolff/canvas/benoitkugler/pdf/go-pdfkit |
| `backend/svg`, `backend/png` | SVG na stronę; raster przez `x/image/vector` | mały |
| `engine/chromium` | adapter do `gohtmlpdf` (chromedp) za tym samym interfejsem | przeniesienie z xml.pro |
| `engine/folio`, `engine/weasy` | adaptery do obcych silników na czas benchmarku i ewentualnej adopcji | cienkie |

## Interfejs

Stan 2026-11-12: w `render.go` istnieje pierwsza wersja, węższa niż szkic poniżej: `Options` ma `Fonts`, `Resolver`,
`BaseURL`, `UserCSS`; `Result` ma `PDF`, `Pages`, `Warnings`, nazwę i wersję silnika. Brakuje metadanych, limitów,
wyjść SVG/PNG i PDF/A. Szkic docelowy:

```go
type Options struct {
    Media        string            // "print" (domyślnie) lub "screen"
    Page         PageDefaults      // rozmiar i marginesy, gdy HTML nie ma @page
    Fonts        FontSource        // fonty osadzone + dodatkowe; nigdy fonty systemowe
    Resolver     Resolver          // zasoby url(...) poza data:; domyślnie odmawia
    Metadata     Metadata          // tytuł, autor, język, daty, /ID; wymagane dla determinizmu
    Outputs      []Output          // PDF, SVG, PNG; domyślnie PDF
    Limits       Limits            // maks. stron, rozmiar obrazu, czas przez ctx
    PDFA         PDFAProfile       // brak | A-2b
}

type Result struct {
    PDF      []byte
    Pages    int
    SVG      [][]byte          // gdy zamówione
    PNG      [][]byte
    Warnings []Warning         // nieobsługiwane właściwości i elementy, z pozycją w źródle
    Engine   EngineInfo        // nazwa i wersja silnika; odbiorca może zapisać przy dokumencie
}

func Render(ctx context.Context, html []byte, opts Options) (Result, error)
```

Ten sam interfejs ma adapter Chromium, więc odbiorca wybiera silnik konfiguracją, a dowody (hash HTML
w ścieżce audytu, hash PDF, wersja silnika) nie zależą od wyboru.

## Determinizm

- Fonty wyłącznie z `FontSource` (osadzone w binarce albo wskazane plikiem), nigdy z systemu.
- Brak dostępu do zegara w silniku; daty wyłącznie z `Metadata`.
- Identyfikatory obiektów PDF nadawane w kolejności przetwarzania; brak map iterowanych bez sortowania.
- Kompresja strumieni z ustaloną biblioteką i poziomem; `/ID` liczony z treści albo podany.
- Test w CI: korpus renderowany dwa razy, bajty identyczne; renderowany na amd64 i arm64, bajty identyczne.

## Testowanie

- **Testy listy wyświetlania**: HTML → tekstowa postać listy, porównanie z plikiem wzorcowym (golden). Stabilne,
  czytelne w diffie.
- **Testy stronicowania**: liczba stron i element rozpoczynający każdą stronę, porównanie z Chromium jako
  odniesieniem (tak robi go-pdfkit/html2pdf w `breakcheck`).
- **Testy wizualne**: PNG na stronę porównane z wzorcem z progiem różnic; wzorce z Chromium na start, potem
  z własnego silnika po akceptacji.
- **Fuzzing** parsera CSS, SVG i układu: brak paniki, limit czasu.
- **Zgodność ze specyfikacją**: wybrane testy z Web Platform Tests dla CSS 2.1 i Paged Media, bez JS.
