# Rejestr decyzji (ADR)

Jedna decyzja = jeden plik `NNNN-temat.md`: kontekst, decyzja, konsekwencje, status. Zmiana decyzji = nowy ADR, stary
oznaczony „zastąpiony przez”. Pytania bez decyzji: `open-questions.md`.

| # | Decyzja | Status |
|---|---|---|
| 0014 | Treść szersza niż obszar strony jest pomniejszana jak w Chromium, domyślnie najwyżej 1,5 raza; `Options.MaxShrink` zmienia limit albo wyłącza | przyjęta |
| 0015 | Blok o podanej wysokości jest dzielony między kolumny jak w przeglądarkach (między strony nadal według ADR 0004); `break-inside: avoid` i `contain: size` wyłączają dzielenie | przyjęta (robocza) |
| 0016 | PDF/A-2b jako opcja pliku wynikowego (`Options.Conformance`): XMP, profil sRGB generowany w kodzie, identyfikator z treści; zgodność sprawdzana walidatorem veraPDF | przyjęta |
| 0017 | Tryb ciągły (`Options.Continuous`): jedna strona rosnąca do wysokości treści, bez podziałów; od 2026-11-16 w nim liczony jest sondaż ekranowy | przyjęta |
| 0018 | Fonty testowe CSSTest z WPT w `testdata/fonts/csstest` (zamiast instalacji w systemie) | przyjęta |
| 0019 | Cechy rozmiaru w `@media` oceniane względem obszaru domyślnego arkusza (A4 albo arkusz użytkownika), bez wpływu `@page` autora (CSS Page 3 § 7.1; sprawdzone w Chrome 157) | przyjęta |
| 0020 | Wiersze tabeli w druku według CSS Tables 3 § 6.1: niski wiersz w całości, wysoki dzielony; bez przełącznika, sterowanie przez `break-inside` (świadome odstępstwo od Chrome) | przyjęta, uzupełniona 2026-11-20 (wiersz wyższy niż strona dzielony w miejscu) |
| 0021 | Obszar strony zaokrąglany do całych pikseli CSS jak w Chrome 157 (szerokość i wysokość w górę, lewa krawędź do najbliższego), pod przełącznikiem `ExactLengths` z ADR 0013 | przyjęta |
| 0022 | Dywiz-minus po literze albo cyfrze i przed cyfrą jest miejscem podziału jak w Chromium (UAX #14 zostawia wybór; „03-228” dzielone po dywizie) | przyjęta |
| 0023 | Strumienie PDF kompresowane zlib w stopniu 6 (pomiar: stopień 9 wolniejszy o 8-13% za 0-2% rozmiaru); `Options.Uncompressed` do diagnostyki | przyjęta |
| 0024 | Słowniki dzielenia wyrazów tylko z hyph-utf8 na MIT, BSD, Unlicense, FSF all-permissive (24 języki); pozostałe przez `Options.Hyphenation` | przyjęta |
| 0025 | Licencja projektu Apache-2.0 z `NOTICE` (atrybucja: Michał Głębowski) i `THIRD_PARTY_NOTICES`; repozytorium na razie prywatne | przyjęta |
| 0026 | PDF/A-3b (`PDFA3b`, `-pdfa 3b`) i załączniki dokumentu (`Options.Attachments`, `/AFRelationship`, katalog `/AF`); w PDF/A-2b załącznik spoza PDF/A jest błędem | przyjęta |
| 0027 | Strony rysowane zaraz po ułożeniu, pola marginesowe dorysowywane po ułożeniu wszystkich stron; wynik bajt w bajt taki sam, dokumenty z ponowną paginacją (spis treści z `target-counter`) wracają do rysowania po ułożeniu całości | przyjęta |
| 0028 | Obraz SVG z pliku (`img`, `object`, `embed`, tło, `border-image`, `content: url()`) rysowany raz jako forma XObject (`backend.SharedDrawer`), kolejne wystąpienia to `Do`; klucz: obraz, tryb dokumentu, rozmiar; SVG inline bez zmian | przyjęta |
| 0029 | Odnośniki `<a>` w SVG jako adnotacje `/Link`: prostokąt otaczający narysowanej treści po przekształceniach; SVG w HTML z odnośnikami zewnętrznymi i wewnętrznymi, w `<object>`/`<embed>` tylko zewnętrznymi, w `<img>` i tłach bez odnośników | przyjęta |
| 0030 | Fragment adresu obrazu: dokument wspólny dla fragmentów, obraz pochodny; `#xywh=` (Media Fragments) wycina prostokąt obrazu rastrowego i SVG, `#svgView(…)` i nazwa `<view>` zmieniają widok SVG (własna forma współdzielona); `data:` tylko z końcówką po ostatnim `#` | przyjęta |
| 0031 | Obraz SVG (`img`, obraz CSS, `<image>`) w trybie statycznym nie używa zasobów zewnętrznych (innych niż `data:`): `<image>`, tło elementu głównego, `@font-face`; decyzja przy rysowaniu, bo obraz jest wspólny z `<object>`; fonty `@font-face` obrazu zawężone do obrazu | przyjęta |
| 0032 | Pamięć złamanych wierszy ograniczona do dwóch pokoleń po ok. 16 MB; wynik bez zmian, pamięć przy 1021 stronach 1,3–1,5 → 0,73 GB | przyjęta |
| 0033 | PDF z tagami (PDF/UA-1, PDF/A-2a/3a): struktura z drzewa elementów HTML, MCID przy rysowaniu, artefakty dla tła i pól marginesów, opcja `Options.Tagged` (domyślnie wyłączona), `div`→`Div`, braki dostępności jako ostrzeżenia; trzy rundy | przyjęta |
| 0034 | Faktura hybrydowa Factur-X 1.0 / ZUGFeRD 2.x (`Options.FacturX`, `-facturx`): XML CII jako `factur-x.xml` (`xrechnung.xml`), związek według profilu (Data albo Alternative), właściwości XMP `fx` z opisem schematu rozszerzenia, wymuszone PDF/A-3b (3a z tagami); profil niezgodny z XML jako ostrzeżenie | przyjęta |
| 0035 | Po ukośniku między znakami niebiałymi bez łamania, gdy następna okazja do złamania jest bliżej niż 20 znaków (UAX #14 § 5.1, jak Chromium dla krótkich członów) | przyjęta |
| 0036 | Dokument z ponowną paginacją (spis treści z `target-counter`, `counter(pages)` w treści) układany w dwóch przebiegach: pierwszy ustala stan paginacji bez zachowywania stron, drugi układa i rysuje strony od nowa; wynik bajt w bajt ten sam, pamięć jak bez spisu, czas +35–38%; do 32 stron bez zmian | przyjęta |
| 0037 | Szybkość dużych dokumentów: glif rysowany bez rozkodowania konturu, style pól marginesowych bez przeglądania stylów wszystkich stron, pule kompresorów zlib, strumienie stron zamykane równolegle; wynik bajt w bajt ten sam, książka 1021 stron 6,5 → 4,6 s | przyjęta |
| 0038 | Łamanie wiersza po każdej spacji U+0020, także przed „:”, „)”, „!” i po „(” (jak w Chromium; UAX #14 LB13–LB17 dostosowane według CSS Text 3 § 5), ukośnik na początku słowa bez okazji po nim (rozszerzenie ADR 0035); dywiz na początku słowa zostaje przy LB20a | przyjęta |
| 0039 | Fonty systemowe jako fonty zastępcze tylko w binarce ze znacznikiem `systemfonts` (`Options.SystemFonts`, `-system-fonts`; w wariancie domyślnym `ErrSystemFontsNotBuilt`); fonty wywołującego i `GenericFamilies` mają pierwszeństwo, każdy użyty font systemowy daje ostrzeżenie, fonty z zakazem osadzania (OS/2 `fsType`) pomijane; bez `text/template` binarka domyślna nie zawiera skanowania go-text | przyjęta |
| 0013 | Jeden przełącznik `Options.ExactLengths`: domyślnie zaokrąglenia długości jak w Chromium (obramowania, `border-spacing`), `true` daje długości dokładnie z arkusza; pomniejszania strony nie naśladujemy | przyjęta |
| 0012 | Cechy rozmiaru w `@media` (`width`, `height`, `orientation`) oceniane względem obszaru strony z `@page` | zastąpiona przez ADR 0019 |
| 0011 | Element wiersza flex, który nie mieści się na stronie, przechodzi na następną sam (jak w przeglądarkach); gdy nie mieści się żaden, przechodzi cała linia | przyjęta |
| 0010 | Pismo pionowe (`writing-mode`) tylko jako poddrzewo obrócone i dopiero na potrzebę dokumentu; osie logiczne i pełne pismo pionowe poza projektem | przyjęta |
| 0009 | Szerokości obramowań, konturów i linii między kolumnami przyciągane do całych pikseli jak w Chromium (CSS Values 4 § 6.1); przełącznik w ADR 0013 | przyjęta; parametr zastąpiony przez ADR 0013 |
| 0008 | Pismo `monospace` bez podanego rozmiaru ma 13 px jak w przeglądarkach; `Options.MonospaceFontSize` zmienia albo wyłącza regułę | przyjęta |
| 0007 | Cechy w `@media` (`min-width`, `orientation`, …) oceniane względem stałego medium odniesienia: A4 w pionie, niezależnie od `@page` | przyjęta; rozmiar medium zastąpiony przez ADR 0012 |
| 0006 | Dolny margines dziecka przy `min-height`/`max-height` zostaje w pudełku jak w Chromium; `Options.MarginsEscapeHeightLimits` włącza zachowanie z dosłownego tekstu CSS 2.2 | przyjęta |
| 0005 | Fonty dla rodzin generycznych (`serif`, `sans-serif`, `monospace`) wskazuje wywołujący przez `Options.GenericFamilies`; bez wskazania bez zmian | przyjęta |
| 0004 | Blok o podanej wysokości: mieszczący się na stronie jest przenoszony w całości, wyższy niż strona dzielony; wariant przeglądarkowy ewentualnie jako opcja | przyjęta |
| 0003 | Reftesty: zgodność ścisła albo z dokładnością do krawędzi (`pass-edge`); Ahem tylko przez `@font-face` | przyjęta |
| 0002 | Układ na bazie forka webrender bez Pango; PDF/A, tagowanie i `calc()` uzupełniane z folio | przyjęta, do potwierdzenia benchmarkiem |
| 0001 | Lista wyświetlania jako warstwa niezależna od formatu wyjścia; backendy PDF, SVG, PNG czytają tylko ją | przyjęta |
