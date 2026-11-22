# ADR 0040: Podzbiory fontów CFF i CFF2 jako CIDFontType0C

Status: przyjęta 2026-11-22.

## Kontekst

Fonty OpenType o konturach PostScript (tabela `CFF `, pliki `.otf`) przechodziły przez backend PDF przejęty
z go-weasyprint tak samo jak fonty TrueType: słownik `CIDFontType2`, plik fontu jako `/FontFile2`, a do
„podzbioru” trafiał cały font OpenType, bo podzbiór umiał tylko tabelę `glyf`. Skutki:

- pdffonts zgłaszał „Mismatch between font type and embedded font file”; ISO 32000-1 § 9.7.4 (tab. 117)
  i § 9.9 (tab. 126) wiążą `CIDFontType2` z programem TrueType, a kontury CFF z `CIDFontType0`;
- plik zawierał cały font: dwa znaki z Hiragino Sans (font systemowy, ADR 0039) dawały PDF 5,6 MB, próbka
  `ticket` z WeasyPrint (Barlow Condensed, `.otf`) 152 kB;
- fonty z tabelą `CFF2` (fonty zmienne `.otf`, np. Source Sans 3 VF) w ogóle nie trafiały do PDF:
  `isSupportedFont` ich nie uznawał, a tekst nimi złożony znikał bez ostrzeżenia. Indeks fontów
  systemowych (ADR 0039) pomijał je jako fonty bez konturów.

## Decyzja

1. **Format osadzenia.** Font złożony `Type0` z kodowaniem `Identity-H` i potomkiem `CIDFontType0`,
   program fontu w `/FontFile3` z `/Subtype /CIDFontType0C`: goły font CFF (bez opakowania OpenType)
   w postaci CID-keyed (Adobe Technical Note #5176, § 18), ROS `Adobe-Identity-0`, zgodny z
   `/CIDSystemInfo` słownika. Odrzucona możliwość: `/Subtype /OpenType` z całym plikiem OpenType
   (podzbiór tabeli `CFF ` w środku). Powody:
   - CIDFontType0C jest w PDF od wersji 1.3 i w każdej wersji PDF/A (ISO 19005-2 i -3 opierają się na
     ISO 32000-1, który dopuszcza oba podtypy `FontFile3`); OpenType wymaga PDF 1.6;
   - dla `/OpenType` z CFF nie-CID ISO 32000-1 nie mówi jednoznacznie, czy CID jest numerem glifu, czy
     przechodzi przez charset; w CID-keyed CFF przeliczenie CID → glif robi zawsze charset fontu
     (ISO 32000-1 § 9.7.4.2), więc wynik nie zależy od interpretacji czytnika;
   - goły CFF jest mniejszy: bez `hmtx`, `cmap`, `name`, `OS/2`, `GSUB`/`GPOS`, których PDF nie czyta
     (szerokości są w `/W`, tekst w ToUnicode);
   - sprawdzone: pdffonts „CID Type 0C … yes yes yes” bez komunikatów, veraPDF przyjmuje PDF/A-2b i 2a.
   `/CIDToGIDMap` dotyczy tylko `CIDFontType2` (tab. 117) i nie jest zapisywany.
2. **CID = numer glifu źródła.** Strumienie treści, `/W` i ToUnicode adresują glify tymi samymi liczbami co
   dla fontów TrueType (numerami z kształtowania go-text), więc reszta backendu się nie zmienia. Podzbiór ma
   glify w rosnącej kolejności numerów źródła, a jego charset przypisuje każdemu glifowi CID równy temu
   numerowi; `CIDCount` to liczba glifów źródła.
   - **CFF nie-CID** (zwykły Top DICT, np. Noto Sans `.otf`, STIXGeneral, Barlow): przepisany do postaci
     CID-keyed z jednym słownikiem FD, który dostaje Private DICT i lokalne podprogramy z Top DICT;
     FDSelect wskazuje go dla wszystkich glifów. Nie-domyślna `FontMatrix` idzie do Top DICT, a FD dostaje
     macierz jednostkową, bo czytniki mnożą obie (TN #5176 § 18; poppler nie dziedziczy macierzy Top DICT
     w FD bez własnej macierzy).
   - **Jednostki fontu.** CFF bez `FontMatrix` w pliku OpenType ma jednostki `unitsPerEm` z tabeli `head`,
     nie domyślne 1/1000: tak czyta go FreeType (`cff_face_init`: bez macierzy `units_per_em` z `head`,
     o ile font nie jest „gołym” CFF) i tak liczy posunięcia go-text. Goły CFF nie ma `head`, więc podzbiór
     takiego fontu dostaje jawną macierz 1/`unitsPerEm` w Top DICT i jednostkową w FD. Przypadek z sondażu:
     `FontWithFancyFeatures.otf` z WPT (`unitsPerEm` 1024, bez macierzy) bez tej reguły był rysowany 2,4%
     za mały.
   - **FontBBox** pochodzi z tabeli `head` (`xMin`, `yMin`, `xMax`, `yMax`), nie z Top DICT źródła: w pliku
     OpenType prostokąt fontu podaje `head`, a FontBBox w CFF bywa nieaktualny (fonty testowe WPT:
     `[0 -1 0 0]`); poppler wymiaruje według niego bufor glifów i przy małych rozmiarach (72–96 dpi) rysował
     taki podzbiór inaczej niż cały font. CFF2 nie ma FontBBox, więc podzbiór dostaje go z `head` też.
     Kodowanie (`Encoding`) i nazwy glifów znikają: font CID-keyed ich nie ma.
   - **CFF CID-keyed** (np. Hiragino, Noto CJK): ROS i CID źródła są zastępowane (CID = numer glifu, ROS
     Adobe-Identity-0), słowniki FD przepisane bez zmian treści, tylko te użyte przez glify podzbioru, z
     nowym FDSelect.
   - Z Top DICT zostają napisy `version`, `Notice`, `Copyright`, `FullName`, `FamilyName`, `Weight`
     (z nowymi numerami SID) i wartości liczbowe (`ItalicAngle`, `isFixedPitch`, `PaintType`,
     `Underline*`, `StrokeWidth`, `CIDFontVersion`, `CIDFontRevision`); znikają identyfikatory
     (`UniqueID`, `XUID`, `UIDBase`), których podzbiór nie może dzielić z pełnym fontem. Nazwa w Name
     INDEX to `/BaseFont` (znacznik podzbioru z sześciu wielkich liter i `+`, jak dla TrueType),
     ograniczona do znaków dozwolonych w nazwie PostScript.
3. **Podzbiór charstringów i podprogramów.** Zostają `.notdef` i glify użyte w dokumencie; CFF nie ma
   glifów złożonych, więc zależności między glifami nie trzeba zbierać (poza `seac`, niżej). Podprogramy
   (Global/Local Subrs) są obsługiwane interpreterem charstringów Type 2 (TN #5177), który śledzi stos i liczbę stem hintów (dla długości masek `hintmask`/`cntrmask`)
   i rozwija `callsubr`/`callgsubr` w miejscu. Zapisywane są dwa warianty i zostaje krótszy:
   - charstringi bez podprogramów (rozwinięte); dla małych podzbiorów zwykle kilkanaście razy mniejsze
     (Noto Sans, 6 glifów: 690 B wobec 4,6 kB);
   - charstringi źródła z podprogramami, w których nieużyte zastąpiono pojedynczym `return`, więc numery
     (i bias) się nie zmieniają, a charstringów nie trzeba przepisywać; wygrywa przy dużych podzbiorach
     (cały Noto Sans: 162 kB wobec 297 kB rozwiniętych).
   Porównywana jest długość przed kompresją strumienia; to przybliżenie, ale deterministyczne.
   Gdy interpreter natrafi na konstrukcję, której nie rozwija (operatory arytmetyczne i `put`/`get`, numer
   podprogramu wyliczony zamiast podanego wprost, `seac` – akcent złożony z glifów kodowania standardowego,
   niedozwolony w fontach CID-keyed), podzbiór zostaje przy podprogramach w całości i charstringach bez
   zmian. Glif z `seac` jest wtedy w podzbiorze bez swoich części i może się nie narysować; w fontach
   OpenType `seac` jest przestarzały i w sprawdzonych dokumentach nie wystąpił.
4. **CFF2** (OpenType 1.8+, fonty zmienne `.otf`) jest przepisywany na CFF w domyślnej instancji:
   `blend` zostawia wartości domyślne (w charstringach i w Private DICT), `vsindex` znika, każdy charstring
   dostaje szerokość z `hmtx` (z `nominalWidthX` = 0) i `endchar`, których CFF2 nie ma. Szerokość jest
   potrzebna, bo walidatory PDF/A porównują `/W` z programem fontu (ISO 19005-2 § 6.2.11.5). Operatory,
   którym po usunięciu delt zostaje więcej argumentów niż 48 (granica stosu Type 2; CFF2 dopuszcza 513),
   są dzielone, gdy to możliwe (`rlineto`, `rrcurveto`). Zmienność (`fvar`, `gvar`, `HVAR`) nie trafia do PDF,
   tak jak dotąd dla fontów zmiennych TrueType, z których podzbiór wycina `gvar`: rysowana jest instancja
   domyślna. CFF2 nie ma ścieżki z podprogramami, więc nieobsługiwana konstrukcja jest błędem podzbioru.
5. **Błąd podzbioru** (uszkodzona tabela, nieobsługiwana konstrukcja CFF2): font jest osadzany w całości
   jako `/FontFile3 /Subtype /OpenType` (nadal `CIDFontType0`), z ostrzeżeniem `CFF font subsetting failed
   for <rodzina>: …; embedding the whole font` w `Result.Warnings`. Bez paniki i bez gubienia tekstu.
6. **Kod.** Pakiet `internal/cffsubset` (własny): odczyt CFF i CFF2 (INDEX, DICT, FDSelect 0/3/4, Variation
   Store), interpreter charstringów i zapis CID-keyed CFF. Odczytu konturów z go-text (`font/cff`) używają
   tylko testy: jego parser nie udostępnia struktur potrzebnych do zapisu (DICT, Private, charset). Backend
   PDF (`internal/goweasyprint/pdf/text.go`, `writeCFFFont`) wybiera tę ścieżkę dla fontów z `CFF ` albo
   `CFF2` bez `glyf`; fonty TrueType idą jak dotąd (bajt w bajt). W `internal/pdflib` strumień `FontFile3`
   nie dostaje już `/Length1 0 /Length2 0 /Length3 0` (tab. 127: tylko dla Type 1 i TrueType).

## Skutki

Pomiary 2026-11-22 (Apple M4, macOS, PDF z kompresją, ten sam dokument przed i po):

| Dokument | Przed | Po |
|---|---|---|
| 2 znaki Hiragino Sans (font systemowy, CID-keyed CFF, `-tags systemfonts`) | 5 630 349 B | 2 659 B |
| 3 akapity japońskie, Hiragino Sans W4 i W7, Hiragino Mincho | 15 910 759 B | 25 483 B |
| próbka WeasyPrint `ticket` (3 × Barlow Condensed `.otf` + 2 × TrueType) | 151 928 B | 18 157 B |
| 2 akapity STIXGeneral.otf (`Options.Fonts`) | 254 884 B | 9 670 B |
| akapity Noto Sans `.otf` + Source Sans 3 VF (CFF2) | 172 017 B (bez tekstu CFF2) | 6 390 B |

- Rastry (pdftoppm, 72 i 150 dpi) z podzbiorem są identyczne piksel w piksel z rastrami tego samego
  dokumentu z całym fontem OpenType w `/FontFile3 /OpenType` na wszystkich dokumentach z tabeli (także CFF2
  i font CID-keyed) oraz z rastrami sprzed zmiany tam, gdzie poppler rysował cały font.
- Sondaż W3C (`bench/verify/w3cbytes.py`, 11 689 dokumentów, 46 756 renderowań): bajty zmieniły się w 88
  dokumentach (176 renderowań), wszystkie z fontami CFF przez `@font-face` (79 w `css-fonts`, 8
  `css-text/boundary-shaping`, 1 `text-transform-math`); rastry ich wydruków (96 dpi) identyczne z rastrami
  sprzed zmiany w 88 z 88. Pozostałe dokumenty bajt w bajt bez zmian; korpus (`corpus.sh`) fail=0.
- Fonty testowe: `testdata/fonts/cff/` (Noto Sans `.otf`, jego kopia przepisana na CID-keyed z dwoma
  słownikami FD i CID ≠ numer glifu, Source Sans 3 VF Italic z `CFF2`), dwa poziomy pod `testdata/fonts`,
  więc nie trafiają do fontów korpusu i sondażu wczytywanych wzorcami `*.ttf`, `*/*.ttf` i `*/*.otf`.
- Testy: `internal/cffsubset` (kontury każdego glifu podzbioru przez go-text równe konturom źródła dla CFF,
  CID-keyed i CFF2, w trzech wariantach zapisu; liczba glifów; charset; determinizm),
  `tests/cff22_test.go` (pdffonts bez komunikatów, „CID Type 0C … yes yes yes”, tekst z pdftotext,
  rastry CFF i CID-keyed równe, veraPDF PDF/A-2b i 2a), `tests/systemfonts_test.go`
  (`TestSystemFontsCFF`: font systemowy CFF2).
- Ograniczenie ADR 0039 („fonty CFF osadzane w całości”) przestaje obowiązywać.
