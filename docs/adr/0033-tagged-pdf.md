# ADR 0033: PDF z tagami (PDF/UA-1, PDF/A-2a, PDF/A-3a)

Status: przyjęta 2026-11-21 (decyzje w sekcji „Rozstrzygnięcia”); wdrażana rundami, runda 1 zrobiona
2026-11-21 (sekcja „Stan rundy 1”).

## Kontekst

gopdfgo nie tworzy dziś struktury logicznej: PDF nie ma `StructTreeRoot`, `MarkInfo` ani oznaczonej treści,
więc czytniki ekranu czytają strony jak obraz tekstu, a zgodność PDF/UA-1 (ISO 14289-1) oraz PDF/A-2a i 3a
(poziom „a” wymaga tagów) jest niedostępna. `docs/overview.md` wymienia to wśród braków, wymaganie Y6
w `docs/requirements.md` ma priorytet „O”. Chrome z wiersza poleceń pisze PDF z tagami domyślnie,
WeasyPrint (wzorzec przejętego kodu) obsługuje PDF/UA, webrender (port do Go) tego nie przeniósł.

Co już jest:
- `internal/pdflib/model/structure.go`: typy drzewa struktury (`StructureTree`, `StructureElement`,
  odwołania do treści oznaczonej `ContentItemMarkedReference` i do obiektów `ContentItemObjectReference`,
  `ParentTree`, `IDTree`, pole `Alt`) z zapisem do PDF;
- `internal/pdflib/contentstream`: operatory `BDC`/`BMC`/`EMC`;
- fonty osadzone z ToUnicode, metadane XMP i profil wyjściowy dla PDF/A-2b i 3b (ADR 0016, 0026);
- drzewo pudełek zna element HTML każdego pudełka (`BoxFields.Element`, `PseudoType`);
- odnośniki HTML i SVG jako adnotacje (ADR 0029 odkłada strukturę `Link` do tej decyzji).

## Propozycja

1. **Opcja.** `Options.Tagged` (i `-tagged` w CLI) oraz wartości `Conformance` „2a”, „3a”, „ua1”, które ją
   włączają. Domyślnie wyłączona: PDF bez tagów jest mniejszy, a dla dokumentów generowanych maszynowo
   (faktury) tagi bywają zbędne. Do rozstrzygnięcia: czy domyślnie włączyć, jak Chrome.
2. **Struktura z drzewa elementów, nie z kolejności rysowania.** Typy standardowe ISO 32000-1 § 14.8.4:
   `html`→`Document`, `p`→`P`, `h1`…`h6`→`H1`…`H6`, `ul`/`ol`→`L`, `li`→`LI` z `Lbl` (znacznik
   `::marker`) i `LBody`, `table`→`Table`, `thead`/`tbody`/`tfoot`→`THead`/`TBody`/`TFoot`, `tr`→`TR`,
   `th`→`TH` (atrybut `Scope`), `td`→`TD` (`RowSpan`, `ColSpan`), `a`→`Link` z `OBJR` do adnotacji, `img`,
   `svg`, `object` z obrazem→`Figure` z `Alt` (z `alt`, `aria-label`, `<title>` SVG), `figure`/`figcaption`
   →`Figure`/`Caption`, `blockquote`→`BlockQuote`, `code`→`Code`, `em`/`strong` i reszta liniowych→`Span`,
   `div`, `section`, `article` itd.→`Div`/`Sect`/`Art`. Kolejność czytania = kolejność w dokumencie.
   Element bez treści na stronach nie trafia do drzewa. Język z `lang` (`/Lang` dokumentu i elementów).
3. **Treść oznaczona przy rysowaniu.** Rysowanie pudełka otacza jego tekst, obrazy i linie `BDC /Typ
   <</MCID n>> … EMC`; numer MCID jest kolejny w obrębie strony. Element podzielony między strony
   (akapit, tabela) dostaje kilka odwołań. Rysowanie według kontekstów nakładania może przeplatać treść
   elementów, więc jeden element może mieć kilka odcinków na stronie; drzewo i tak wskazuje je w porządku
   logicznym.
4. **Artefakty.** Tła, obramowania i cienie (`/Artifact`), pola marginesów i elementy biegnące (`/Artifact
   <</Type /Pagination>>`), treść generowana `::before`/`::after` bez znaczenia (do ustalenia, CSS nie mówi).
5. **Rysowanie w trakcie układu (ADR 0027).** Pudełka strony są zwalniane po narysowaniu, więc przy
   rysowaniu zapamiętujemy tylko pary (element, strona, MCID) i odwołania do adnotacji; drzewo struktury
   powstaje na końcu z elementów HTML, które żyją do końca renderowania. Pola marginesów dopisywane później
   są artefaktami, więc numeracji MCID strony nie zaburzają.
6. **Formy współdzielone (ADR 0028).** Obraz SVG w formie jest jednym `Figure`: oznaczony jest operator
   `Do` na stronie, wnętrze formy nie ma tagów. SVG wstawione w HTML: na początek też jeden `Figure`;
   struktura wewnątrz SVG (tekst SVG jako `P`/`Span`) później, jeśli okaże się potrzebna.
7. **Poziom dokumentu.** `/MarkInfo <</Marked true>>`, `/StructTreeRoot` z `ParentTree` i `RoleMap`
   tylko dla typów niestandardowych, `/Lang`, `/ViewerPreferences <</DisplayDocTitle true>>`, tytuł
   w metadanych (PDF/UA go wymaga; bez `<title>` ostrzeżenie), w XMP `pdfuaid:part 1` dla „ua1”,
   `pdfaid:conformance A` dla „2a”/„3a”; `/Tabs /S` na stronach z adnotacjami.
8. **Sprawdzanie.** veraPDF z profilami PDF/UA-1 i PDF/A-2a na korpusie `testdata/documents`, testy
   własne na strukturę (typy, kolejność, `Alt`, odwołania `OBJR`), determinizm bajtowy z tagami i bez.
   Wynik bez opcji pozostaje bajt w bajt ten sam (porównanie korpusu W3C).
9. **Wzorzec algorytmu.** Moduł tagów WeasyPrint (BSD-3) jako wzorzec odwzorowania, bez kopiowania kodu
   wprost; normy (ISO 32000-1 § 14.7–14.8, ISO 14289-1, PDF Association „Tagged PDF Best Practice”)
   wygrywają przy rozbieżności.

## Koszt (szacunek)

- Runda 1: opcja, drzewo z elementów, MCID dla tekstu, obrazów i linii, artefakty dla tła, obramowań i pól
  marginesów, `Link` z `OBJR`, poziom dokumentu, test veraPDF PDF/A-2a na prostych dokumentach.
- Runda 2: listy (`Lbl`/`LBody`), tabele (nagłówki, `Scope`, scalone komórki), `Figure` z `Alt`, `lang`
  na elementach, PDF/UA-1 i Matterhorn (kryteria sprawdzalne maszynowo), korpus `testdata/documents` bez
  błędów veraPDF.
- Runda 3 (opcjonalna): struktura wewnątrz SVG, przypisy i odsyłacze (`Note`, `Reference`), tekst
  alternatywny dla treści generowanej, rozszerzenia PDF 2.0 (przestrzeń nazw, `Artifact` z `Subtype`).

Szacowany rozmiar: 1500–2500 linii z testami, głównie w `html/document` (rysowanie) i backendzie PDF.
Ryzyko: kontekst nakładania i elementy pozycjonowane rysowane poza kolejnością dokumentu; liczba operatorów
`BDC`/`EMC` zwiększy treść stron (szacunek: +5–15% rozmiaru strumieni stron, do pomiaru).

## Rozstrzygnięcia (decyzja użytkownika 2026-11-21)

- **Domyślnie wyłączone.** Tagi włącza `Options.Tagged` albo `Conformance` „2a”, „3a”, „ua1”.
- **`div` i inne elementy grupujące bez znaczenia** dają element `Div` (typ standardowy ISO 32000-1
  § 14.8.4.2, dopuszczalny w PDF/UA); drzewo zachowuje grupowanie dokumentu. Elementy bez treści na
  stronach nie trafiają do drzewa.
- **Braki dostępności (obraz bez `alt`, dokument bez tytułu) dają ostrzeżenia**, nie błąd: renderowanie
  kończy się, plik powstaje, a ostrzeżenia mówią, co poprawić, żeby veraPDF przyjął plik jako PDF/UA.

## Stan rundy 1 (2026-11-21)

Działa:
- `Options.Tagged` i flaga `-tagged`; `Conformance` `PDFA2a` i `PDFA3a` (`-pdfa 2a`, `-pdfa 3a`) włączają tagi
  i piszą `pdfaid:conformance A`. „ua1” zostaje na rundę 2 (veraPDF z profilem PDF/UA-1 odrzuca dziś każdy
  plik korpusu, niżej lista reguł).
- Drzewo z elementów HTML w kolejności dokumentu według pkt 2: `Document`, `Div`, `Sect`, `Art`,
  `BlockQuote`, `P` (także `pre`, `address`), `H1`–`H6`, `L`/`LI`, `Table`/`THead`/`TBody`/`TFoot`/`TR`/`TH`/
  `TD`, `Caption`, `Figure` (`img`, `svg`, `object`, `embed`, `iframe`, `figure`) z `Alt` z `alt`,
  `aria-label`, `<title>` SVG albo `title`, `Link` (`a` i `area` z `href`) z `OBJR` do adnotacji, odnośniki
  SVG jako `Link` wewnątrz `Figure` obrazu, `Quote`, `Code`, `Formula` (MathML), `Ruby`/`RB`/`RT`/`RP`,
  `Span` dla pozostałych liniowych; `body` i `picture` są przezroczyste; element bez treści na stronach nie
  trafia do drzewa; `lang` elementu różny od rodzica daje `/Lang` elementu. Odcinki treści elementu stoją
  między potomkami według kolejności rysowania, potomkowie według kolejności dokumentu.
- Treść oznaczona przy rysowaniu, leniwie: `BDC` przed pierwszym operatorem malującym (ścieżka tylko
  przycinająca go nie otwiera), kolejne malowanie tego samego elementu w tym samym ciągu, zamknięcie przed
  każdym `q` i `Q`, więc ciągi i stany graficzne zagnieżdżają się poprawnie; MCID kolejne w obrębie strony.
  Grupy (krycie, `mix-blend-mode`, filtry) i formy współdzielone (ADR 0028) są oznaczone w miejscu `Do` jako
  treść elementu tworzącego kontekst albo obrazu; wnętrza form zostają bez znaczników.
- Artefakty: tła, obramowania (także zwinięte), cienie, obrysy, linie kolumn, cienie tekstu (`/Artifact`);
  pola marginesów z elementami biegnącymi i powtórzone na kolejnych stronach nagłówki i stopki tabel
  (`/Artifact <</Type/Pagination>>`); obraz z pustym `alt` (ozdobny według HTML) i obraz treści generowanej.
- Poziom dokumentu: `/MarkInfo <</Marked true>>`, `/StructTreeRoot` z `ParentTree` (korzeń bez `/Limits`),
  `/Lang` z `lang` elementu głównego, `/ViewerPreferences <</DisplayDocTitle true>>`, `/StructParents` na
  stronach z treścią oznaczoną, `/StructParent` na adnotacjach, `/Tabs /S` na stronach z adnotacjami.
- Ostrzeżenia: „Tagged PDF: image without alternative text …” (raz na element) i „Tagged PDF: document
  without a title (<title>)”.
- Obie drogi rysowania (ADR 0027 i `document.StreamingDisabled`) dają te same bajty: ciąg treści oznaczonej
  jest zamykany przed miejscem na pola marginesowe, a pola marginesów są artefaktami, więc nie zmieniają
  numeracji MCID strony.
- Rozmiar: drzewo struktury to tysiące małych obiektów (rejestr z tabelą: 1466 elementów), które w zwykłym
  zapisie powiększały plik o 23–171%. Z tagami obiekty niebędące strumieniami trafiają do skompresowanych
  strumieni obiektów, a tabela odsyłaczy jest strumieniem (ISO 32000-1 § 7.5.7–7.5.8; PDF/A-2 je dopuszcza);
  `Uncompressed` zostawia zapis zwykły do czytania w edytorze.

Pomiar 2026-11-21 (macOS arm64, cały proces, mediana 5 przebiegów, książka 3; bez tagów → z tagami):

| Dokument | Rozmiar | Czas |
|---|---|---|
| `02-register-table` (8 stron) | 81 052 → 105 987 B (+30,8%; bez strumieni obiektów 219 670 B, +171%) | 0,114 → 0,119 s (+4%) |
| `20-invoice` (3 strony) | 28 525 → 34 680 B (+21,6%; bez strumieni obiektów +136%) | 0,080 → 0,081 s (+1,5%) |
| `21-mixed-report` (3 strony) | 37 665 → 35 128 B (−6,7%; bez strumieni obiektów +23%) | 0,071 → 0,071 s |
| książka `1342-bench.htm` | 26 247 339 → 26 232 456 B (−0,1%; bez strumieni obiektów +1,8%) | 1,610 → 1,629 s (+1,2%) |

Sprawdzenia: veraPDF (profil PDF/A-2a) przyjmuje 22 z 22 dokumentów `testdata/documents` z `-pdfa 2a`;
rastry stron z tagami i bez są identyczne (22 dokumenty, 48 dpi); porównanie bajtowe całego korpusu W3C bez
opcji: 0 różnic. veraPDF z profilem PDF/UA-1 (na rundę 2; reguła: liczba plików z błędem na 22): 5-1
(brak schematu `pdfuaid`) 22; 7.5-1 (`TH` bez `Scope`) 4; 7.4.2-1 (pierwszy nagłówek nie `H1` albo pominięty
poziom w dokumencie bez `H`) 4; 7.3-1 (`Figure` bez `Alt`, np. element `figure`) 3; 7.2-42 i 7.2-43 (wiersze
tabeli o różnej liczbie kolumn: brak `ColSpan`/`RowSpan`) 2; 7.2-20 (`LI` może zawierać tylko `Lbl` i `LBody`)
2; 7.18.5-2 (odnośnik bez `/Contents`) 2; 7.18.1-2 (adnotacja bez `/Contents` i bez `Alt` elementu) 2; 7.2-14
(`Table` z `THead` bez `TBody`) 1.

Przesunięte na rundę 2: `Lbl`/`LBody` (znacznik `::marker` stoi dziś w `LI` za treścią), `Scope`, `RowSpan`,
`ColSpan`, `Headers` w tabelach, `/Contents` i `Alt` odnośników, `pdfuaid:part 1` i „ua1”, `figure` z `Alt`
z `figcaption` albo jako grupa, treść generowana jako artefakt, struktura wewnątrz SVG, `RoleMap`
(dziś nie jest potrzebna: wszystkie typy są standardowe).
