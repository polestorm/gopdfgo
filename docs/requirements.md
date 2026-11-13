# Wymagania wobec renderera

Stan na 2026-11-13. Wymagania silnika ogólnego użytku wobec typowych dokumentów (`vision.md`); pierwsza wersja
z 2026-11-12 powstała z potrzeb konkretnych odbiorców i została uogólniona. Priorytety: **M** musi, **P** powinien,
**O** opcjonalnie. Gdy wymaganie kłóci się z rozmiarem, przewidywalnością, szybkością albo czystością kodu,
obowiązuje kolejność z `vision.md`.

## Wejście

| # | Wymaganie | Prio | Uwagi |
|---|---|---|---|
| W1 | Samowystarczalny HTML: CSS inline lub w `<style>`, obrazy i fonty jako `data:` albo dostarczone przez wywołującego | M | Żadnego dostępu do sieci ani systemu plików z wnętrza silnika; zasoby przez interfejs `Resolver` |
| W2 | HTML5 parsowany algorytmem WHATWG (tolerancja błędów jak w przeglądarce) | M | `golang.org/x/net/html` |
| W3 | SVG inline i jako obraz (`<img src="data:image/svg+xml">`) | M | Wykresy z gochartgo, ikony z katalogu produktu |
| W4 | Obrazy PNG, JPEG; GIF statyczny, WebP | M / P | Zdjęcia z załączników |
| W5 | Fonty TTF/OTF; WOFF2 | M / O | W binarce osadzony co najmniej jeden krój z pełnym polskim zestawem (OFL) |
| W6 | `@media print` jako nośnik domyślny, `@media screen` ignorowane | M | Ten sam arkusz dla ekranu i druku |

## Układ i stronicowanie

| # | Wymaganie | Prio | Uwagi |
|---|---|---|---|
| U1 | CSS 2.1: block, inline, inline-block, tabele (auto i fixed), float, clear, position relative/absolute, `z-index`, `overflow: hidden` | M | Szczegóły zakresu w `scope.md` |
| U2 | Flexbox (jednowierszowy i z zawijaniem) | M | Nagłówki zapisów, wiersze etykieta/wartość |
| U3 | CSS Grid | O | Nie używamy w szablonach do druku, dopóki silnik nie ma |
| U4 | `@page`: `size` (A4, A3, letter, orientacja), `margin`, `:first`, `:left/:right`, strony nazwane | M | |
| U5 | Pola marginesowe `@top-left` … `@bottom-right` z `content`, `counter(page)`, `counter(pages)`, `string()` | M | „Strona X z Y”, numer i wersja dokumentu w nagłówku każdej strony |
| U6 | Nagłówki bieżące: `string-set`, `position: running()` z `element()` | M / P | Tytuł rozdziału w nagłówku |
| U7 | `break-before/after/inside` (i aliasy `page-break-*`), `orphans`, `widows` | M | |
| U8 | Tabele łamane między stronami z powtórzonym `thead`, bez cięcia wiersza | M | Listy, rejestry |
| U9 | Liczniki CSS, `content` z `attr()`, listy numerowane i wypunktowane | M | |
| U10 | Kolumny (`column-count`) | P | |
| U11 | Transformacje 2D, `border-radius`, cienie, gradienty | P | Wygląd, nie treść |
| U12 | Dzielenie wyrazów ze wzorcami (pl, en, de) przy `hyphens: auto` | P | Wąskie kolumny tabel |
| U13 | Tekst dwukierunkowy i pisma złożone | O | Poza rynkiem docelowym; przyjmujemy, jeśli shaper daje za darmo |
| U14 | `target-counter()` (odsyłacz do numeru strony), przypisy dolne | O | Spisy treści w dokumentach SZJ |

## Wyjście

| # | Wymaganie | Prio | Uwagi |
|---|---|---|---|
| Y1 | PDF 1.7 z osadzonymi podzbiorami fontów, tekstem do zaznaczania i przeszukiwania (`ToUnicode`) | M | |
| Y2 | **Determinizm**: identyczne wejście, fonty i wersja silnika → identyczne bajty; `CreationDate`, `ModDate`, `/ID` i XMP ustawiane przez wywołującego | M | Hash PDF odtwarzalny; przewaga nad Chromium |
| Y3 | Metadane: tytuł, autor, temat, słowa kluczowe, język dokumentu | M | |
| Y4 | Zakładki (outline) z nagłówków, linki wewnętrzne i zewnętrzne | P | |
| Y5 | PDF/A-2b (osadzone wszystkie fonty, XMP, OutputIntent, bez przezroczystości lub z dozwoloną) | P | Archiwum 15 lat; może być krok końcowy w osobnym narzędziu |
| Y6 | Tagged PDF (struktura logiczna) | O | Dostępność; PDF/UA w przyszłości |
| Y7 | SVG na stronę i PNG na stronę z tego samego układu | P | Podgląd wydruku, testy porównawcze; z listy wyświetlania prawie za darmo |
| Y8 | Raport renderowania: liczba stron, ostrzeżenia z nazwą nieobsługiwanej właściwości/elementu i miejscem w źródle | M | Warunek walidacji; silnik nie może ignorować po cichu |

## Działanie

| # | Wymaganie | Prio | Uwagi |
|---|---|---|---|
| D1 | Czyste Go, `CGO_ENABLED=0`, linux/amd64 i arm64, macOS do pracy lokalnej | M | |
| D2 | Zależności MIT/BSD/Apache-2.0/OFL; brak LGPL/GPL | M | Statyczne linkowanie |
| D3 | Ograniczenia zasobów: limit czasu przez `context`, limit stron i rozmiaru obrazów; brak paniki na dowolnym wejściu (fuzzing) | M | Dane z profilu klienta mogą być błędne |
| D4 | Zapis strumieniowy wielostronicowych dokumentów (setki stron rejestru) bez trzymania wszystkiego w pamięci | P | |
| D5 | Czas: dokument 5 stron z tabelą i wykresem poniżej 200 ms na jednym rdzeniu | P | Orientacyjnie; Chromium potrzebuje ok. 1 s z zimnym startem |
| D6 | Wersjonowanie: zmiana wyniku na korpusie testowym = zmiana wersji, odnotowana w CHANGELOG | M | Odbiorca może zapisać wersję silnika przy dokumencie |
| D7 | Interfejs stabilny i niezależny od silnika: `Render(ctx, html, Options) (Result, error)`, tak samo dla adaptera Chromium | M | Patrz `architecture.md` |
