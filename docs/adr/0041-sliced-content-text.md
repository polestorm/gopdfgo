# ADR 0041: Treść krojona między strony: na stronie tylko jej wycinek

Status: przyjęta 2026-11-22.

## Kontekst

Treść niepodzielna wyższa niż strona jest krojona między strony (CSS Fragmentation 3 § 4.4: treść
monolityczna przechodzi na kolejne fragmentatory; tak też Chrome): obraz albo `<svg>` w przepływie
(`layout.sliceTallReplaced`), wiersz z wysokim elementem atomowym (`lineBoxLayout`, `LineBox.SliceOffset`)
i pudełko pozycjonowane z przycięciem (`BoxFields.SliceVisible`). Każdy wycinek rysuje całość przesuniętą
o część pokazaną wcześniej i przyciętą do pasa widocznego na tej stronie.

Rozbiór rozbieżności z Chromium (`docs/feature-audit.md`, 2026-11-22) zapisał, że w diagramach mermaid
(`bench/realdocs`: `mmd-er-real`, `mmd-p6`, `mmd-p16`) nasz tekst ma więcej słów niż Chromium, bo tekst spoza
przycięcia trafia do warstwy tekstu każdej strony. Pomiar 2026-11-22 (`pdftotext` na PDF gopdfgo z `30b6545`
i Chrome 157 z `bench/realdocs/out/chrome`) pokazał dwie przyczyny:

1. **Błąd: dalsza część `<svg>` w wierszu ginęła.** We wszystkich trzech dokumentach diagram to `<svg>`
   w wierszu (domyślnie `display: inline`). `overflow: hidden` z arkusza UA tworzy dla niego kontekst
   nakładania, rysowany poza pudełkiem wiersza, więc przesunięcie i przycięcie wycinka wiersza go nie
   obejmowały: każda strona pokazywała początek diagramu, a reszta znikała (w `mmd-er-real` encje
   `mitigated_by` i `CONTROL`, w `mmd-p6` `Kontrola`, w `mmd-p16` `N`, `O`, `Kontrola`). Nadmiarowe słowa
   to powtórzony pierwszy wycinek: 66 słów wobec 36 u Chrome (`mmd-er-real`), 67 wobec 52, 58 wobec 32.
2. **Tekst niewidoczny w warstwie tekstu.** Tekst wycinka leżący poza przycięciem, ale na stronie (w jej
   marginesach), `pdftotext` wydobywa; tekst poza stroną pomija, ale nadal jest w treści strony: inne
   narzędzia, wyszukiwanie w czytnikach i PDF z tagami (ADR 0033) widzą go na każdej stronie. W PDF z tagami
   cała treść tekstowa `Figure` była w treści oznaczonej każdej strony z wycinkiem (`mmd-er-real`: 142
   sprawdzenia veraPDF dla tekstu stron wobec 72 po zmianie).

**Co robi Chrome 157** (jego PDF z `bench/realdocs/out/chrome`): na stronie jest tylko tekst, który
przecina jej wycinek; etykieta przecinająca granicę wycinków jest na obu stronach (`RISK` w `mmd-er-real` na
stronie 1 u dołu i na stronie 2 u góry). Wynik pasuje do odrzucania całych operacji rysowania tekstu,
których ramka leży poza przycięciem (wniosek z wyniku, nie z kodu Chromium).

**Normy.** ISO 32000-1 nie wiąże wydobywania tekstu z przycięciem (§ 9.10 opiera je na treści strony), więc
tylko brak tekstu w treści daje pewność, że żaden czytnik go nie wydobędzie. Element struktury może mieć
treść na kilku stronach (§ 14.7.4.2, odwołanie MCR z `/Pg`), więc obraz krojony między strony to jeden
element `Figure`. W PDF/UA-1 (ISO 14289-1 § 7.1) treść rzeczywista jest oznaczona i czytana przez
technologie wspomagające; tekst powtórzony w niewidocznej części kolejnych stron byłby przeczytany kilka
razy. Prostokąt adnotacji odnośnika (`/Rect`, ISO 32000-1 § 12.5.2, § 12.5.6.5) to obszar strony, w którym
odnośnik działa; ma sens tylko tam, gdzie widać jego treść.

## Decyzja

1. **Kontekst nakładania z wnętrza wycinka wiersza** dostaje przesunięcie i przycięcie wiersza
   (`StackingContext.slicedLine`, `html/document/stacking.go`, `drawStackingContext`): konteksty utworzone
   przy rozdziale dzieci wycinka wiersza (np. `<svg>` z `overflow: hidden`, potomkowie pozycjonowani)
   są rysowane tak, jakby leżały w wierszu. To naprawa błędu z punktu 1 kontekstu.
2. **Tekst w całości poza wycinkiem nie trafia do PDF.** Nowe rozszerzenie płótna `backend.TextCuller`
   (`CullTextOutside(x, y, width, height)`): rysujący woła je razem z przycięciem do wycinka (`clipSlice`
   w `html/document/draw.go`: obraz krojony, wycinek wiersza, pudełko krojone jako całość, kontekst
   z wycinka wiersza). W PDF (`goweasyprint/pdf/cull.go`) obszar jest zapisywany w układzie bazowym grupy
   i należy do stanu graficznego (przywracany przez `OnNewStack`). `DrawText` pomija każdy tekst
   (`backend.TextDrawing`: kawałek tekstu jednego pudełka tekstu HTML albo fragmentu SVG), którego ramka nie
   przecina obszaru. Ramka jest ostrożna: obrysy glifów z tabeli `Extents` fontu w pionie, prostokąt fontu
   i posunięcie w poziomie, poszerzone o pochylenie, pogrubienie syntetyczne, szerokość obrysu i po
   przekształceniu o 1 pt (piksel na krawędzi przycięcia bywa częściowo pokryty treścią tuż za nią).
   Wygląd się nie zmienia: pomijany jest tylko tekst, który przycięcie i tak zasłania w całości.
3. **Tekst przecinający granicę zostaje na obu stronach w całości**, jak w Chrome. Odrzucone: dzielenie
   tekstu na glify albo przypisanie go jednej stronie. Część glifów na każdej stronie jest widoczna, więc
   tekst jest treścią obu; dzielenie dawałoby „con” i „sumes” zamiast słowa, a przypisanie do jednej
   strony wymagałoby zgadywania, która część jest „ważniejsza”.
4. **Forma współdzielona obrazu SVG (ADR 0028).** Forma zawiera cały obraz, więc wycinek odwołujący się do
   niej niesie cały tekst. Przy pomijaniu tekstu forma, której prostokąt nie mieści się w obszarze, jest
   rysowana dla wycinka osobno, bez tekstu spoza niego, jeśli obraz w ogóle ma tekst i jakiś tekst odpadł;
   obraz bez tekstu i wycinek, w którym cały tekst przecina obszar, dalej używają wspólnej formy. Koszt:
   grafika obrazu z tekstem jest w pliku raz na stronę wycinka (zob. skutki). Odrzucone: zostawienie
   wspólnej formy z całym tekstem (cel tej decyzji) oraz pomijanie także ścieżek poza obszarem (mniejszy
   plik, ale znaczniki, przerywane linie i łączenia ostre wymagają ostrożnych ramek; możliwe rozszerzenie).
5. **Grupy pośrednie** (`NewGroup`: krycie, maski, wzorce, filtry) zaczynają bez pomijania: ich układ
   współrzędnych bywa niezależny od miejsca użycia (wzorce, maski). Tekst w elemencie z `opacity` < 1
   wewnątrz krojonego obrazu zostaje w całości na każdej stronie wycinka (znane ograniczenie).
6. **PDF z tagami.** Obraz krojony to jeden element `Figure` z treścią oznaczoną na kolejnych stronach
   (tak było; teraz każda strona niesie tylko swój tekst). Odnośniki SVG są przycinane do widocznej części
   (ADR 0029, bez zmian); nowe: jeden element `<a>` obrazu na kilku stronach to jeden element struktury
   `Link` z adnotacją na każdej stronie (`svg.LinkArea.Index`, `tagger.svgLinkNodes`). Wcześniej każda
   adnotacja dostawała własny element `Link`.
7. Testy porównujące wygląd z rysowaniem całego tekstu wyłączają pomijanie przez
   `pdf.TextCullingDisabled`.

## Skutki

Pomiary 2026-11-22, `gopdfgo` z `cmd/gopdfgo` (macOS arm64), `pdftotext` (poppler), dokumenty
z `bench/realdocs` (fonty `bench/realdocs/fonts`):

| dokument | słowa przed | słowa po | Chrome 157 | rozmiar przed | rozmiar po |
|---|---|---|---|---|---|
| `mmd-er-real` | 66 | 36 | 36 | 28 377 B | 26 689 B |
| `mmd-p6` | 67 | 52 | 52 | 19 461 B | 19 038 B |
| `mmd-p16` | 58 | 33 | 32 | 20 700 B | 20 155 B |

- Tekst stron `mmd-er-real` i `mmd-p6` jest taki sam jak u Chrome (strona po stronie). W `mmd-p16` Chrome
  zostawia pustą pierwszą stronę (`docs/feature-audit.md`, pytanie 28), więc granice wycinków są gdzie
  indziej; jedno słowo więcej to etykieta „Korekta”, której ramka sięga 1 pt w głąb wycinka strony 3
  (ostrożny zapas, punkt 2).
- Rastry stron (pdftoppm, 150 dpi) z pomijaniem i bez niego identyczne na tych dokumentach.
- Obraz SVG z tekstem w `<img>` krojony między strony (diagram ER z `mmd-er-real` jako plik, szerokość
  strony A4): plik rośnie o grafikę obrazu na każdą stronę wycinka. 2 strony 14 697 → 21 390 B, 3 strony
  15 199 → 28 607 B, 5 stron 16 203 → 42 933 B (ok. 7 kB na stronę). Przed ADR 0028 tak samo kosztowało
  każde wystąpienie obrazu; `<svg>` w HTML (diagramy mermaid) nie jest współdzielony i po zmianie maleje.
- PDF/UA-1 (veraPDF) dla `mmd-*` z `-pdfua 1`: te same reguły niespełnione co przed zmianą (wady samych
  dokumentów: brak `lang`, tytułu, tekstu alternatywnego), o połowę mniej sprawdzeń tekstu stron.
- Osobne znalezisko: strona bez zasobów (bez tekstu i obrazów) nie ma słownika `/Resources`, który
  ISO 32000-1 § 7.7.3.3 (tab. 30) wymaga (dziedziczony albo własny); veraPDF zgłasza ostrzeżenie. Do
  poprawienia osobno (zmienia bajty tysięcy stron testów W3C).
