# ADR 0039: Fonty systemowe jako opcja kompilacji (znacznik `systemfonts`)

Status: przyjęta 2026-11-22 (decyzja użytkownika).

## Kontekst

Silnik używa wyłącznie fontów od wywołującego (`Options.Fonts`, `Options.GenericFamilies`, ADR 0005), żeby
wynik był powtarzalny między maszynami; „system fonts” stały w README i `docs/overview.md` na liście rzeczy
pominiętych z założenia. Bywa jednak potrzebne narzędzie, które wydrukuje dokument „jak przeglądarka na tej
maszynie”: z Arialem z systemu i z gwiazdką ★ z fontu, którego wywołujący nie podał. Binarka zwalidowana
(PDF/A, ISO) ma przy tym nie zawierać takiej ścieżki wcale, nie tylko mieć ją wyłączoną.

Biblioteka go-text ma przeglądanie katalogów systemu (`fontscan.FontMap.UseSystemFonts`), ale nie nadaje się
do tego wprost: zawsze zapisuje indeks na dysk (`font_index_v*.cache` w katalogu pamięci podręcznej
użytkownika), katalogi bierze tylko z systemu (globalny `sync.Once`, bez możliwości podstawienia katalogów
w testach), a fonty systemowe miesza w jednej bazie z fontami wywołującego, więc w pierwszym przebiegu
dopasowania (dokładna nazwa rodziny) i w podstawieniach dla rodzin generycznych wygrywałyby z nimi.

Sprawdzenie binarki domyślnej (`go tool nm`) pokazało, że kod przeglądania katalogów go-text już w niej był,
choć nieużywany: `fontscan.(*FontMap).UseSystemFonts`, `scanFontFootprints`, `DefaultFontDirectories`,
`filepath.WalkDir`. Powód: przejęty `html/document/draw.go` formatował znaczniki cięcia (`marks: crop cross`)
przez `text/template`, który wywołuje metody przez refleksję (`reflect.Value.MethodByName`), a wtedy linker
Go zachowuje wszystkie metody eksportowane typów używanych przez interfejsy.

## Decyzja

1. **Znacznik kompilacji.** Kod fontów systemowych jest w pakiecie `internal/systemfonts` (wszystkie pliki
   z `//go:build systemfonts`) i w `systemfonts_on.go`; wariant domyślny ma plik zastępczy
   `systemfonts_off.go` (`//go:build !systemfonts`). Budowanie: `go build -tags systemfonts ./cmd/gopdfgo`.
   W przejętym kodzie zostaje tylko punkt zaczepienia bez skanowania: interfejs `text.SystemFonts` i warstwa
   fontmapy w `text/system_fonts.go`, nieczynne przy pustym polu `FontConfigurationGotext.System`.
2. **`text/template` usunięty** z `formatSVG` (zastępowanie pól `{{ .Pole }}` wartościami z `fmt.Sprint`,
   jak w `text/template`; zgodność sprawdzona porównaniem z `text/template` na wartościach brzegowych). Bez
   refleksji linker usuwa nieużywane metody: z binarki domyślnej znika skanowanie go-text, a binarka maleje
   (niżej).
3. **API jednakowe w obu wariantach.** `Options.SystemFonts bool` i flaga CLI `-system-fonts` istnieją zawsze.
   W wariancie domyślnym `Render` z `SystemFonts: true` zwraca `ErrSystemFontsNotBuilt` przed kolejką
   renderowań, a nie ignoruje opcji. Z `SystemFonts` `Options.Fonts` może być puste (CLI: `-fonts` opcjonalne);
   `ErrNoFonts` dopiero wtedy, gdy także w katalogach systemu nie ma fontu nadającego się do osadzenia.
4. **Pierwszeństwo fontów wywołującego** (CSS Fonts 4 § 5, font fallback). Fontmapa z fontami wywołującego
   zostaje bez zmian; fonty systemowe są osobnym źródłem, po które warstwa `systemLayer` sięga:
   - dla rodzin z listy `font-family` stojących przed pierwszą rodziną dostępną u wywołującego; rodzina jest
     dostępna u wywołującego wprost, przez podstawienie zgodne metrycznie go-text (np. „Arial” przy podanym
     Arimo) albo jako rodzina generyczna (także przez `GenericFamilies`), gdy wywołujący podał jakiekolwiek
     fonty. Rodzina dostępna u wywołującego zawsze wygrywa z systemową, także o tej samej nazwie;
   - dla znaku, którego nie ma font wybrany przez fontmapę wywołującego: najpierw rodziny z listy
     `font-family` (w kolejności, z systemu), potem pierwsza rodzina systemowa w kolejności ścieżek plików,
     której font o żądanym aspekcie ma znak. Dotyczy znaków widocznych (`unicode.IsGraphic`), bez selektorów
     wariantu.
   W rodzinie systemowej wybierany jest jeden font według CSS Fonts 4 § 5.2 (szerokość, styl, grubość); gdy
   nie ma znaku, wybór przechodzi do następnej rodziny, nie do innego fontu tej rodziny. Nazwy rodzin
   generycznych i podstawienia zgodne metrycznie rozstrzyga ta sama tablica podstawień go-text co dla fontów
   wywołującego (fontmapa go-text ze sztucznym jednoznakowym fontem na rodzinę systemową, bez wczytywania
   fontów). Bez fontów wywołującego rodziną domyślną jest `serif`; `GenericFamilies` może wskazywać rodziny
   systemowe.
5. **Ostrzeżenia.** Każdy użyty font systemowy daje ostrzeżenie `system font used: <rodzina> (<ścieżka>[, font
   N]); add this file to Options.Fonts to make the output independent of this machine`. Plik z tej ścieżki
   dodany do `Options.Fonts` zamraża wynik. Ostrzeżenie dotyczy fontu wybranego dla tekstu (także tylko do
   pomiaru, np. spacji), raz na renderowanie.
6. **Licencje osadzania.** Wszystkie użyte fonty są osadzane jak dotąd (podzbiory); trybu bez osadzania nie ma.
   Font systemowy, którego pole `fsType` tabeli OS/2 nie pozwala na osadzenie podzbioru, jest pomijany, jakby
   go nie było, z ostrzeżeniem `system font skipped: <rodzina> (<ścieżka>): …`:
   - bit 1 (0x0002, restricted license embedding), chyba że ustawiono też bit 2 albo 3 (starsze fonty mogą
     mieć kilka bitów, obowiązuje najmniej restrykcyjny; OpenType 1.9, OS/2);
   - bit 9 (0x0200, bitmap embedding only): kontury nie mogą trafić do pliku;
   - bit 8 (0x0100, no subsetting): silnik osadza zawsze podzbiory.
   Preview & Print (0x0004) i Editable (0x0008) pozwalają osadzić font w PDF tylko do odczytu. ISO 19005-2
   § 6.2.11.4.1 wymaga fontów osadzonych zgodnie z prawem. Pomijane są też fonty bez konturów (`glyf`/`loca`
   albo `CFF `, np. Apple Color Emoji, same mapy bitowe sbix) i rodzina LastResort z macOS (ma „glif” dla
   każdego znaku Unicode: ramkę z numerem bloku, więc przejęłaby każdy brakujący znak).
7. **Bez zapisu na dysk.** Silnik nie zapisuje nic poza wynikiem. Katalogi (`fontscan.DefaultFontDirectories`:
   na macOS `/System/Library/Fonts`, `/Library/Fonts`, `~/Library/Fonts` i katalogi zasobów fontów systemu,
   na Linuksie katalogi z konfiguracji fontconfig, na Windows `%WINDIR%\Fonts` i katalog fontów użytkownika)
   są przeglądane przy pierwszym renderowaniu z `SystemFonts` w procesie; czytane są tylko tabele `name`,
   `OS/2`, `cmap` i nagłówki tabel, a indeks żyje w pamięci do końca procesu (fonty doinstalowane w trakcie
   nie są widoczne). Font wybrany do tekstu jest wczytywany z dysku raz na renderowanie; font z kolekcji
   (TTC/OTC) jest z niej wyjmowany do osobnego pliku, bo osadzanie obsługuje pojedyncze fonty.
   Pomiar 2026-11-22 (Apple M4, macOS, 449 plików fontów, 856 fontów; `bench/sf/scan`, `systemfonts.Open`
   w nowym procesie): 31–65 ms. Rozważany indeks na dysku we wskazanym jawnie katalogu
   (`Options.SystemFontsCacheDir`, JSON 4,7 MB) wczytywał się w 46–73 ms, dłużej niż przeglądanie, więc go nie
   ma. Gdy na maszynie z dużą liczbą fontów przeglądanie okaże się drogie, wróci jako osobna decyzja.
8. **Testy.** `go test ./tests/` (bez znacznika): `TestSystemFontsNotBuilt` (`ErrSystemFontsNotBuilt`, także
   bez `Fonts`) i `TestDefaultBinaryWithoutSystemFontScan` (buduje `cmd/gopdfgo` i szuka w `go tool nm`
   symboli `internal/systemfonts.`, `fontscan.(*FontMap).UseSystemFonts`, `fontscan.DefaultFontDirectories`,
   `fontscan.scanFontFootprints`, `fontscan.SystemFonts`; na binarce sprzed tej zmiany cztery z nich są).
   `go test -tags systemfonts ./tests/ -run SystemFonts`: katalogi systemu zastąpione katalogami z
   `testdata/fonts` przez `systemfonts.SetDirectoriesForTest` (pakiet wewnętrzny, spoza modułu niedostępny,
   więc punkt wstrzyknięcia nie jest publicznym API); sprawdzane: rodzina spoza `Fonts` z systemu i wybór
   fontu według grubości i stylu, pierwszeństwo `Fonts` i `GenericFamilies` (wynik bajt w bajt jak bez
   fontów systemowych), brakujący znak z fontu systemowego, same fonty systemowe, `ErrNoFonts` przy pustych
   katalogach, pominięcie fontu z `fsType` 0x0002 (`testdata/fonts/system/restricted/`, kopia Noto Sans Mono
   ze zmienionym `fsType`). `go test -tags systemfonts ./internal/systemfonts/`: bity `fsType` i dopasowanie
   aspektu.
9. **Audyt wdrożenia.** `go version -m <binarka>` wypisuje ustawienia budowania; binarka z fontami
   systemowymi ma wiersz `build -tags=systemfonts`, binarka zwalidowana go nie ma. To sprawdzenie, że
   wdrożono wariant domyślny; dodatkowo `go tool nm <binarka> | grep internal/systemfonts` nic nie wypisuje.

## Skutki

- Rozmiary binarek (2026-11-22, `go build`, linux/amd64 `CGO_ENABLED=0 -trimpath -ldflags "-s -w"`):
  przed zmianą 22 184 098 B; wariant domyślny 19 144 866 B (−13,7%, skutek usunięcia `text/template`);
  wariant `systemfonts` 19 271 842 B (+127 kB wobec domyślnego). darwin/arm64 bez `-s -w`: 29 170 466,
  24 882 850 i 24 993 474 B.
- Wariant domyślny daje te same bajty co przed zmianą (sondaż W3C: 11 689 dokumentów, 46 756 renderowań,
  0 różnic; korpus `bench/verify/corpus.sh`: bez różnic).
- Wynik z fontami systemowymi zależy od maszyny: inny zestaw fontów, inna wersja fontu, inna kolejność
  katalogów. Przy tym samym zestawie jest powtarzalny (kolejność ścieżek, bez zależności od czasu).
  Ostrzeżenia mówią, które pliki dodać do `Options.Fonts`, żeby wynik zamrozić.
- Wybór fontu dla brakującego znaku według kolejności ścieżek jest prostszy niż w przeglądarkach (listy
  kaskadowe CoreText, fontconfig): ★ na macOS pochodzi z Arial Unicode MS, `monospace` bez fontów
  wywołującego to Andale Mono (pierwsza rodzina z tablicy podstawień go-text obecna w systemie), nie Menlo
  ani Courier jak w Chrome. `GenericFamilies` pozwala to zmienić.
- Fonty o konturach CFF (OTF, np. Hiragino z kolekcji TTC na macOS) były osadzane w całości, bez podzbioru, a
  pdffonts zgłaszał „Mismatch between font type and embedded font file”; tak samo działo się z fontem CFF
  podanym w `Options.Fonts` (sprawdzone na STIXGeneral.otf), więc było to ograniczenie osadzania, nie fontów
  systemowych. Z fontami systemowymi łatwiej było na nie trafić: strona z dwoma znakami Hiragino Sans miała
  5,6 MB. Usunięte 2026-11-22 przez ADR 0040 (podzbiory CIDFontType0C, ta sama strona ma 2,7 kB); od tej pory
  indeks fontów systemowych uznaje też fonty z tabelą `CFF2`.
- `local()` w `@font-face` dalej nie znajduje fontów systemowych (szuka w bazie fontmapy wywołującego); to
  możliwe rozszerzenie.
- Synteza pogrubienia i pochylenia (CSS Fonts 4 § 7.2, 7.3) zna aspekt fontu systemowego
  (`FontConfigurationGotext.fontMetadata`); kształtowanie i osadzanie idą tą samą ścieżką co dla fontów
  wywołującego.

Kod: `internal/systemfonts/` (`index.go`: przeglądanie i indeks, `session.go`: wybór i wczytywanie, ostrzeżenia,
`match.go`: CSS Fonts 4 § 5.2), `systemfonts_on.go`, `systemfonts_off.go`, `render.go` (`Options.SystemFonts`,
`ErrSystemFontsNotBuilt`), `internal/webrender/text/system_fonts.go`, `text/engine_gotext.go`,
`text/family_names.go`, `html/document/draw.go` (`formatSVG`), `cmd/gopdfgo/main.go` (`-system-fonts`); testy
`tests/systemfonts_test.go`, `tests/systemfonts_default_test.go`, `internal/systemfonts/match_test.go`.
