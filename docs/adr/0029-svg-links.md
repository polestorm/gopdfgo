# ADR 0029: Odnośniki `<a>` wewnątrz SVG jako adnotacje PDF

Status: przyjęta 2026-11-21.

## Kontekst

Odnośniki HTML (`<a href>`) dają w PDF adnotacje `/Link` z akcją `/URI` albo celem nazwanym. Element `a`
w SVG (SVG 2 § 16.1) był rysowany jak grupa, bez adnotacji, więc klikalne diagramy, schematy i wykresy
prowadzące do sekcji dokumentu traciły odnośniki. Pomiar 2026-11-21: `<svg width="200" height="40"><a
href="https://example.com"><rect …/><text …>Kliknij</text></a></svg>` nie dawał żadnej adnotacji.

Odnośniki HTML są zbierane z drzewa pudełek przed rysowaniem (`gatherAnchors`). Obszar odnośnika SVG
zależy od składu tekstu SVG, a ten odbywa się dopiero przy rysowaniu (`layoutText`, z płótnem docelowym:
skala gospodarza, rejestracja fontów w PDF), więc nie da się go wyznaczyć wcześniej bez zmiany wyniku.

## Decyzja

1. **Cel.** `href` ma pierwszeństwo przed `xlink:href` (SVG 2 § 16.1.3, już tak czytane w `tree.go`).
   Cel zaczynający się od `#` to odnośnik wewnętrzny do kotwicy dokumentu HTML (jak `parseURLToken` dla
   HTML), inny jest rozwiązywany względem adresu dokumentu do adresu bezwzględnego. Odnośnik do
   nieistniejącej kotwicy jest usuwany z ostrzeżeniem, jak w HTML.
2. **Obszar.** Prostokąt otaczający (AABB) narysowanej treści elementu w układzie strony, po wszystkich
   przekształceniach: `transform` (z `transform-origin`), `viewBox` i `preserveAspectRatio` elementu
   głównego i zagnieżdżonych `<svg>`, przesunięcie `<use>`, położenie obrazu (`object-fit`,
   `object-position`) i przekształcenia CSS przodków. Kształty liczą się z obrysem, tekst ramką składu,
   `<image>` i `foreignObject` swoim prostokątem. Obszar jest przycinany do okna widoku obrazu,
   zagnieżdżonych `<svg>` z `overflow: hidden` i pola treści elementu HTML (we fragmencie pokrojonym między
   strony do części widocznej na tej stronie).
3. **Obrót.** Przy obrocie i pochyleniu obszarem jest prostokąt otaczający, jak dla odnośników HTML
   (`rectangleAabb`). `QuadPoints` (ISO 32000-1 § 12.5.6.5) pominięte: interfejs `backend.Page` przyjmuje
   tylko prostokąt, a przeglądarki PDF różnie je honorują. Do ponownego rozważenia razem z HTML.
4. **Przynależność.** Treść należy do najbliższego `a`, który ją zawiera. Elementy są wypisywane
   w kolejności otwarcia, więc zagnieżdżony `a` leży w `/Annots` za zewnętrznym, czyli nad nim, i wygrywa.
   Odnośniki SVG strony idą za odnośnikami HTML tej strony, więc `<a>` SVG wygrywa też z `<a>` HTML wokół
   `<svg>`. Element z `display: none` albo `visibility: hidden` (bez widocznych potomków) i element bez
   treści nie mają obszaru. Każde użycie `<use>` z `a` daje osobny obszar.
5. **Sposób osadzenia** (HTML § 4.8.4.4, 4.8.6, 4.8.7; SVG Integration, tryby odwołań):
   - SVG osadzone w HTML (`<svg>` w treści): odnośniki zewnętrzne i wewnętrzne;
   - SVG w `<img>` i jako obraz CSS (`background-image`, `list-style-image`, `content`): „secure static
     mode”, bez interakcji, więc bez odnośników;
   - SVG w `<object>` i `<embed>`: osobny dokument w zagnieżdżonym kontekście przeglądania, interaktywny
     („dynamic interactive mode”), więc odnośniki zewnętrzne (rozwiązywane względem adresu pliku SVG)
     działają; `#nazwa` wskazuje miejsce w samym pliku SVG, którego w PDF nie ma, więc nie trafia do PDF;
   - `<iframe>` z plikiem SVG jest dziś rysowany jak obraz (`handleIframe`), bez odnośników; poza zakresem.
6. **Kiedy.** Obszary są zbierane po narysowaniu strony (`gatherSVGLinks` w `html/document/svg_links.go`),
   w obu drogach rysowania: w `Stream` po narysowaniu obszaru strony i po dorysowaniu pól marginesowych,
   w `Write` po `Page.Paint`. Ramki tekstu pochodzą ze składu tego rysowania; samo zbieranie
   (`svg.SVGImage.LinkAreas`, `svg/links.go`) nie rysuje i nie zmienia stanu obrazu. Kolejność jest
   kolejnością drzewa pudełek i dokumentu SVG, bez kluczy po adresach, więc wynik obu dróg jest taki sam
   bajt w bajt.
7. **Forma współdzielona (ADR 0028).** SVG w `<object>`/`<embed>` z odnośnikami nie korzysta z formy
   współdzielonej, bo rysowane tylko przy pierwszym wystąpieniu nie składałoby tekstu dla kolejnych;
   pozostałe obrazy z pliku (bez odnośników albo w `<img>` i tłach) są współdzielone jak dotąd.

## Ograniczenia

- `transform-box: fill-box` albo `stroke-box` z `transform-origin` na grupie albo tekście: punkt
  odniesienia liczony bez płótna, z ramki samego kształtu (grupa i tekst mają wtedy pole 0 0 0 0).
- Odnośniki w treści `foreignObject` (HTML w SVG) i w zawartości znaczników (`marker`) nie dają adnotacji.
- Ten sam plik SVG w `<img>` (forma współdzielona) i w `<object>` różnej wielkości na jednej stronie: ramki
  tekstu pochodzą z ostatniego składu, więc obszar tekstu w `<object>` może być przybliżony.
- Przycięcie `clip-path`, maska i pas pudełka krojonego jako całość (`SliceVisible`) nie zmniejszają obszaru.
- PDF z tagami (PDF/UA): silnik nie tworzy drzewa struktury, więc odnośniki HTML też nie mają elementu
  `Link` z odwołaniem `OBJR`; pytanie otwarte 26 w `open-questions.md`.

## Sprawdzenie

`tests/svglinks18_test.go`: prostokąty przy `viewBox`, obrocie, zagnieżdżonym `<svg>` z przycięciem,
`<use>`, zagnieżdżonych `<a>`, elementach niewidocznych i tekście; brak odnośników z `<img>`, tylko
zewnętrzne z `<object>`; każdy przypadek w obu drogach rysowania z porównaniem bajtów.
