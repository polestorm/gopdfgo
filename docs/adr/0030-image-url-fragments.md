# ADR 0030: Fragmenty adresów obrazów: `#xywh=…`, `#svgView(…)`, `<view>`

Status: przyjęta 2026-11-21.

## Kontekst

Adres obrazu (`<img src>`, `background-image`, `content: url()`, `<object data>`, `<image href>` w SVG)
może mieć fragment, który zmienia to, co z dokumentu obrazu widać:

- Media Fragments URI 1.0 § 4.2.2, wymiar przestrzenny: `obraz#xywh=x,y,w,h`, `#xywh=pixel:…`,
  `#xywh=percent:…` wycina prostokąt obrazu (rastrowego i SVG);
- SVG 2, rozdział „Linking”, „Linking into SVG content: URL fragments and SVG views”:
  `#svgView(viewBox(…);preserveAspectRatio(…);transform(…))` ustawia widok dokumentu SVG, a nazwa
  elementu `<view>` bierze jego `viewBox` i `preserveAspectRatio` zamiast atrybutów elementu głównego.

Silnik fragmenty pomijał: obraz z fragmentem był pobierany i przetwarzany jak osobny adres, ale rysowany
w całości. W sondażu SVG W3C 26 testów `svg/linking/reftests/media-fragment-*`, `svgview-*`, `view-*`
i `svg/coordinate-systems/view-transform-viewBox` nie przechodziło (sondaż: 155 z 235).

Obrazy SVG z pliku są rysowane do formy współdzielonej (ADR 0028), której kluczem jest tożsamość
obrazu: dwa fragmenty tego samego pliku nie mogą dzielić formy ani rozmiaru naturalnego.

## Decyzja

1. **Dokument i obraz pochodny** (`images/fragment.go`). `GetImageFromUri` dzieli adres na dokument
   i fragment (zdekodowany z procentów, więc `%3B` to `;`, a `%20` to spacja). Dokument jest pobierany
   i przetwarzany raz, w pamięci podręcznej pod adresem bez fragmentu; fragment daje obraz pochodny,
   zapamiętany pod pełnym adresem. Obraz rastrowy ma identyfikator z adresu dokumentu, więc trafia do PDF
   raz dla wszystkich fragmentów. `<image href="plik.svg#…">` wewnątrz SVG korzysta z tego samego
   rozdzielenia (`loadImage`); `<use href="plik.svg#id">` ma własną ścieżkę (`fetchExternalUse`) i się nie
   zmienia.
2. **Adres `data:`.** Według normy URL fragment zaczyna się od pierwszego `#`. Treść SVG wpisana wprost
   w `data:` często zawiera niezakodowane kolory `#rrggbb`, więc tam fragmentem jest tylko końcówka po
   ostatnim `#`, i to tylko gdy wygląda jak fragment (nazwa, `svgView(…)`, pary `nazwa=wartość`). To
   odstępstwo na korzyść istniejących dokumentów: ścisłe czytanie psułoby ich obrazy.
3. **Rozbiór fragmentu.** Fragment zaczynający się od `svgView(` jest widokiem SVG i tylko nim: składnia
   `SVGViewSpec ( "&" timesegment )?`, więc `svgView(…)&xywh=…` jest niepoprawny w całości i pomijany
   (test `svgview-and-xywh`). `&t=…` jest przyjmowane i bez znaczenia, bo obraz jest statyczny. Nieznany
   atrybut albo niedomknięty nawias unieważnia cały widok. Spacje wokół elementów składni są dozwolone.
   Inny fragment to najpierw nazwa elementu `<view>` (tylko SVG), potem pary Media Fragments rozdzielone
   `&` z ostatnim poprawnym `xywh` (§ 5.1.1). Fragment wskazujący inny element niż `<view>` nie zmienia
   widoku.
4. **Widok SVG** (`svg/view.go`, `svg.View`, `SVGImage.ApplyView`). Na czas rysowania i wyznaczania
   rozmiaru naturalnego widok podmienia `viewBox` i `preserveAspectRatio` elementu głównego oraz dodaje
   `transform(…)` w układzie użytkownika po `viewBox` (pole `svg.viewTransforms`, jak Blink: najpierw
   odwzorowanie `viewBox`, potem przekształcenie widoku). Atrybut pominięty w widoku zostaje z elementu
   głównego. Niepoprawny `viewBox` (inna liczba liczb, ujemny wymiar) jest pomijany i zostaje `viewBox`
   elementu głównego (test `view-viewbox-invalid-fallback`); `viewBox` o zerowym wymiarze wyłącza
   rysowanie. Proporcje dokumentu bez `width` albo `height` pochodzą z `viewBox` widoku (test
   `view-viewbox-override`). Widok nie dotyka `width` i `height`, więc rozmiar naturalny z nich pozostaje.
5. **Forma współdzielona.** Obraz SVG z widokiem jest osobnym `images.SVGImage` z polem `view`, a klucz
   formy (`sharedSVGKey`) obejmuje widok: każdy adres z widokiem ma własną formę, wspólną dla swoich
   wystąpień. `SVG()` zwraca dla obrazu z widokiem nil, więc `<image>` w SVG rysuje go przez `Draw`.
6. **Wymiar przestrzenny** (`images.croppedImage`). Obraz źródłowy jest rysowany w rozmiarze
   odniesienia, przesunięty i przycięty do prostokąta, który staje się całym obrazem. Rozmiar odniesienia
   to rozmiar naturalny; SVG bez niego bierze rozmiar `viewBox`, a bez `viewBox` 300 na 150. Prostokąt
   wychodzący poza obraz jest obcinany do jego granic. Rozmiar naturalny wycinka to rozmiar prostokąta,
   a gdy źródło nie ma rozmiaru naturalnego, wycinek ma tylko proporcje. Piksele są liczbami całkowitymi
   (`xywh=100.0,…` jest niepoprawne i pomijane, test `media-fragment-spatial-decimal-values`), procenty
   mogą mieć część ułamkową; zerowa szerokość albo wysokość unieważnia wymiar. Obraz SVG pod wycinkiem
   rysuje się przez `SVGImage.Draw`, więc korzysta z formy całego pliku: wiele wycinków jednego pliku
   (sprite) to jedna forma i osobne przycięcia.
7. **Niepoprawny `viewBox` dowolnego elementu** (`svg/svg.go` `parseSharedAttributes`) działa jak brak
   atrybutu; źródło odrzucało przez niego cały dokument (plik z `<view viewBox="0 0 25">` nie rysował się).

## Ograniczenia

- Wycinek obrazu SVG traci rodzaj obrazu: w `<object>`/`<embed>` jest rysowany jak obraz (bez
  `AsDocument`) i bez odnośników.
- Fragment `:target` w arkuszu dokumentu SVG nie jest obsługiwany, więc `#id` nie zmienia stylu.
- Wymiar czasowy i inne wymiary Media Fragments (`t`, `track`, `id`) nie mają znaczenia dla obrazu
  statycznego i są pomijane.

## Wynik

Sondaż SVG W3C: 181 z 235 (było 155), bez strat; 26 nowych zaliczeń, wszystkie testy fragmentów z listy powyżej (w tym
`view-viewbox-override`: `<object>` z adresem `data:` i `#aspect`, oraz `svgview-timesegment`). Testy
własne: `tests/fragment18_test.go`. Korpus: piksele bez zmian.
