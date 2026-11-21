# ADR 0031: Obraz SVG w trybie statycznym nie wczytuje zasobów zewnętrznych

Status: przyjęta 2026-11-21.

## Kontekst

Dokument SVG trafia do PDF na trzy sposoby: jako `<svg>` osadzony w HTML, jako osobny dokument
w `<object>`/`<embed>` i jako obraz (`<img>`, `background-image` i inne obrazy CSS, `<image>` wewnątrz SVG).
SVG Integration (§ 4.2, tryby przetwarzania) i HTML (§ 4.8.4.4) wymagają, by obraz był wyświetlany
w trybie „secure static” (dla `<image>`: „secure animated”, bez animacji to samo): bez skryptów, bez
interakcji i bez wczytywania zasobów zewnętrznych, czyli innych niż adresy `data:`. Dokument w `<object>`
działa w trybie „dynamic interactive” i wczytuje je. Tak robią Chrome i Firefox (testy WPT
`svg/as-image/external-resource-inline-sheet`, `svg/text/reftests/text-font-face-load-image`).

Źródło (webrender) wczytywało zawsze wszystko, co dało się pobrać, a reguł `@font-face` i `background-image`
elementu głównego w samodzielnym SVG nie znało wcale. Ten sam przeanalizowany obraz SVG jest w pamięci
podręcznej wspólny dla `<img>` i `<object>` (`images.SVGImage.AsDocument`), więc tryb nie jest znany przy
wczytywaniu.

## Decyzja

1. **O użyciu zasobu decyduje rysowanie.** `svg.SVGImage` dostaje pole `documentMode` (ustawiane na czas
   `DrawAsDocument`) i `standalone`; zasób zewnętrzny jest używany, gdy SVG jest osadzone w HTML albo
   rysowane jako dokument. Zasoby nadal są pobierane przy wczytywaniu (koszt tylko czasu; wynik jest ten
   sam dla obu trybów, bo forma współdzielona ma klucz z trybem, ADR 0028).
2. **Objęte zasoby:** `<image href>` o adresie innym niż `data:`, obraz `background-image` elementu
   głównego (nowo obsługiwany: rozmiar własny, powtarzany od lewego górnego rogu; inne wartości
   `background-*` pominięte) i źródła `src` reguł `@font-face` (nowo obsługiwanych, `svg/fontfaces.go`).
   `local()` w `@font-face` obrazu SVG jest pomijane zawsze (fonty systemowe nie są zasobem dokumentu).
3. **Fonty obrazu są zawężone do obrazu.** Twarze z `@font-face` samodzielnego SVG trafiają do konfiguracji
   fontów gospodarza pod nazwą rodziny z numerem obrazu i trybem (`"nazwa (gopdfgo svg N image)"`), a skład
   tekstu tego obrazu zamienia na nie nazwy rodzin. Dokument HTML i inne obrazy ich nie widzą (CSS Fonts 4
   § 4.1: reguły dotyczą dokumentu, w którym stoją). Numer to kolejność wczytania, więc wynik jest
   deterministyczny.
4. **Poza zakresem:** `@import` w `<style>` samodzielnego SVG (nadal wczytywany zawsze, przy parsowaniu,
   domyślnym pobieraczem) i `<use href="inny.svg#id">` (wczytywany przy budowie drzewa). Oba wymagałyby
   przeniesienia rozwijania na czas rysowania; w korpusie i w sondażu nie występują w obrazach.

## Konsekwencje

- Wynik dla `<img src="plik.svg">` z `<image href="zdjecie.png">` zmienia się: zdjęcia nie ma, jak
  w przeglądarkach. Kto chce je pokazać, osadza SVG w HTML, używa `<object>` albo zapisuje obraz jako
  `data:`. Korpus `testdata/svg` takich odwołań nie ma.
- Pliki z edytorów (draw.io, Inkscape) z fontami osadzonymi jako `data:` w `@font-face` rysują tekst
  właściwym krojem także w `<img>`.
