# ADR 0033: PDF z tagami (PDF/UA-1, PDF/A-2a, PDF/A-3a)

Status: proponowana 2026-11-21, do decyzji użytkownika (zakres, wartość domyślna, kolejność rund).

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

## Pytania otwarte

- domyślnie włączone czy wyłączone;
- rola `div` bez znaczenia (`Div` czy pominięcie w drzewie jak `NonStruct`);
- czy PDF/UA ma być osobnym trybem z twardymi błędami (np. obraz bez `alt` przerywa renderowanie) czy
  tylko ostrzeżeniami.
