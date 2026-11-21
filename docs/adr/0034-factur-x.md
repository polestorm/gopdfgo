# ADR 0034: Faktura hybrydowa Factur-X 1.0 / ZUGFeRD 2.x

Status: przyjęta 2026-11-21. Rozszerza ADR 0026 (PDF/A-3b i załączniki) i ADR 0033 (PDF/A-3a, PDF/UA-1).

## Kontekst

Factur-X (FNFE-MPE) i ZUGFeRD (FeRD) to jedna specyfikacja faktury hybrydowej: plik PDF/A-3 z czytelną
fakturą i osadzonym XML-em faktury w składni UN/CEFACT CII. Po ADR 0026 silnik umiał osadzić XML
z `/AFRelationship` i `/AF`, ale nie zapisywał metadanych XMP, po których czytniki i walidatory rozpoznają
fakturę, ani ich opisu w schemacie rozszerzeń PDF/A. Bez tego opisu plik z właściwościami `fx` nie jest
zgodny z PDF/A (ISO 19005-3 § 6.6.2.3).

Źródło: specyfikacja Factur-X 1.07.2 (ZUGFeRD 2.3.2) z 2024-11-15, wydanie niemieckie (poprawka wydania
1.0.07 = ZUGFeRD 2.3; w Factur-X 1.0 numer wersji instancji pozostaje 1.0, § 6.6). Oficjalny pakiet FNFE-MPE
wymaga podania adresu e-mail, więc czytaliśmy kopię publiczną wydania niemieckiego (easyfirma.net). Od
2026-08-04 obowiązuje wydanie 1.09.2 / ZUGFeRD 2.5.2; go nie czytaliśmy. Listy kodów Hybrid* (BR-HYBRID-06
do 09) pochodzą z list kodów ZUGFeRD 2.3.2 w postaci przepisanej do pakietu zugferd-code-lists (docs.rs),
opisy właściwości schematu rozszerzenia z przykładowego pliku XMP pakietu specyfikacji (PDFlib, kopia
w repozytorium akretion/factur-x). Żadnego z tych plików nie kopiujemy do repozytorium.

## Decyzja

1. `Options.FacturX *FacturX` z polami `XML`, `Level`, `Relationship`, `Description`, `Version`, `ModTime`;
   wiersz poleceń `-facturx plik.xml [-facturx-level EN16931]`. Profile to stałe `FacturXMinimum`,
   `FacturXBasicWL`, `FacturXBasic`, `FacturXEN16931`, `FacturXExtended`, `FacturXXRechnung`; wartością jest
   napis zapisywany w `fx:ConformanceLevel` (`MINIMUM`, `BASIC WL`, `BASIC`, `EN 16931`, `EXTENDED`,
   `XRECHNUNG`; § 6.3.1, lista HybridConformanceType). Faktura to osobna opcja, a nie wartość
   `Conformance`, bo łączy się z dwiema normami PDF/A-3 i z PDF/UA, tak jak `Accessibility` w ADR 0033.
2. Norma: Factur-X wymaga PDF/A-3 w dowolnym poziomie, zalecając 3a (§ 6, BR-HYBRID-02). Pusta
   `Conformance` daje PDF/A-3b, a gdy dokument i tak ma tagi (`Tagged`, `Accessibility`) — PDF/A-3a.
   `PDFA3b` i `PDFA3a` zostają, `PDFA2b` i `PDFA2a` kończą `Render` błędem `ErrInvalidFacturX`.
   PDF/A-4f (dopuszczony przez BR-HYBRID-02) jest poza zakresem, bo silnik nie pisze PDF 2.0.
3. Plik: XML trafia na początek listy załączników dokumentu, więc dostaje wszystko z ADR 0026 (drzewo
   `/EmbeddedFiles`, `/F`, `/UF`, `/AF` katalogu — związek z całym dokumentem, § 6.2.1) oraz:
   - nazwę `factur-x.xml`, a dla profilu XRECHNUNG `xrechnung.xml` (§ 6.2, § 7.7, BR-HYBRID-13; lista
     HybridDocumentFilename). Nazwy nie da się zmienić, bo każda inna łamie BR-HYBRID-13;
   - typ MIME `text/xml` (§ 6.2);
   - `/Desc` z pola `Description`, domyślnie „Factur-X invoice (profil)”;
   - `/AFRelationship` z tabeli § 6.2.2: `Data` dla MINIMUM i BASIC WL, `Alternative` dla BASIC, EN 16931,
     EXTENDED i XRECHNUNG (jedyna wartość dopuszczalna w Niemczech; § 7.7 dla XRECHNUNG). Pole
     `Relationship` przyjmuje `Data`, `Source` i `Alternative`; inne wartości to błąd, a wartość spoza
     tabeli dla danego profilu (Francja dopuszcza `Data`, `Source`, `Alternative` dla BASIC, EN 16931
     i EXTENDED) daje ostrzeżenie, bo BR-HYBRID-11 ma poziom „Warning”;
   - `/Params /ModDate` tylko z niezerowego `ModTime`. § 6.2 to zaleca („sollte”), ale silnik nie wstawia
     bieżącego czasu, żeby wynik był powtarzalny (ADR 0026, punkt 3).
   Drugi osadzony plik o tej samej nazwie jest błędem: z `Options.Attachments` — `ErrInvalidAttachment`,
   z `<link rel=attachment>` albo `<a rel=attachment>` — `ErrInvalidFacturX`.
4. XMP: osobny `rdf:Description` w przestrzeni `urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#`
   (prefiks `fx`, znak `#` na końcu wymagany; § 6.3.1) z `fx:DocumentType` = `INVOICE`,
   `fx:DocumentFileName` = nazwa z punktu 3 (równa `/F`, BR-HYBRID-14), `fx:Version` i
   `fx:ConformanceLevel`. Opis schematu w `pdfaExtension:schemas` (nazwa „Factur-X PDFA Extension Schema”,
   cztery właściwości typu `Text`, kategoria `external`, opisy jak w przykładzie z pakietu specyfikacji).
   Gdy plik jest też PDF/UA, opisy schematów `pdfuaid` i `fx` leżą w jednym `rdf:Bag` jednego bloku
   `pdfaExtension:schemas`. Plik bez faktury ma te same bajty XMP co dotąd.
5. `fx:Version`: domyślnie `1.0`; pole `Version` przyjmuje wartości z listy HybridDocumentVersion (`1.0`,
   `1p0`, `2p0`, `2p1`, `2p2`). § 7.7 pokazuje dla XRECHNUNG przykład `2p1` („wersja specyfikacji
   danych”), ale BR-HYBRID-10 zaleca `1.0` dla wszystkich profili, a § 6.6 mówi, że wersja instancji
   Factur-X 1.0 pozostaje 1.0; przyjmujemy więc `1.0` także dla XRECHNUNG.
6. Wejście: XML musi być poprawny składniowo (UTF-8, jeden element główny) z korzeniem
   `rsm:CrossIndustryInvoice` w przestrzeni `urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100`
   (CII D16B/D22B); inaczej `ErrInvalidFacturX`. Profil odczytujemy z identyfikatora specyfikacji BT-24
   (`rsm:ExchangedDocumentContext/ram:GuidelineSpecifiedDocumentContextParameter/ram:ID`, białe znaki
   na brzegach pomijane; załącznik 1, BT-24):
   - `urn:factur-x.eu:1p0:minimum` — MINIMUM, `urn:factur-x.eu:1p0:basicwl` — BASIC WL;
   - `urn:cen.eu:en16931:2017#compliant#urn:factur-x.eu:1p0:basic` — BASIC;
   - `urn:cen.eu:en16931:2017` — EN 16931;
   - `urn:cen.eu:en16931:2017#conformant#urn:factur-x.eu:1p0:extended` — EXTENDED;
   - `urn:cen.eu:en16931:2017#compliant#urn:xeinkauf.de:kosit:xrechnung_` z dowolną wersją — XRECHNUNG
     (identyfikator KoSIT; specyfikacja Factur-X go nie wypisuje).
   Pusty `Level` oznacza profil z XML; gdy go nie da się rozpoznać — błąd. Podany `Level` inny niż w XML,
   identyfikator nieznany albo jego brak dają ostrzeżenie, a w metadanych zostaje profil podany:
   BR-HYBRID-15 („fx:ConformanceLevel SOLLTE dem Profil … entsprechen”) ma poziom „Warning”.
   Treści faktury nie sprawdzamy (XSD, Schematron, zgodność z HTML) — to zadanie walidatora Factur-X.
7. Przestrzeń schematu: tabela reguł BR-HYBRID-04 w wydaniu niemieckim podaje
   `urn:factur-x:pdfa:CrossIndustryDocument:1p0#` (bez `invoice:`), sprzecznie z § 6.3.1 i § 7.7. Ta postać
   to przestrzeń Order-X; dla faktury trzymamy się § 6.3.1.

## Skutki

- Pomiar 2026-11-21, veraPDF 1.30.0 z /opt/homebrew/bin:
  - `documents/20-invoice.html` z `testdata/facturx/20-invoice-minimum.xml`: PDF/A-3b zgodny; z tagami
    PDF/A-3a zgodny; PDF/A-3a z PDF/UA-1 zgodny w profilach 3a i ua1;
  - kontrola, że veraPDF czyta opis schematu: plik z `valueType` zmienionym na nieznany typ albo
    z `namespaceURI` innym niż przestrzeń właściwości `fx` jest odrzucany.
- Bez opcji wynik bez zmian bajt w bajt: korpus 22 dokumentów w trybach zwykłym, 2b, 3b, 2a+UA-1, 3a,
  UA-1 i 3a+UA-1 (154 renderowania, 0 różnic) oraz testy W3C w trybie stronicowanym i ciągłym.
- Testy: `tests/facturx18_test.go`.
- Poza zakresem: Order-X, schematy XMP ZUGFeRD 1.0 i 2.0 (`zf`, `zugferd-invoice.xml`; § 6.3.2), PDF/A-4f,
  pliki stowarzyszone ze stroną zamiast dokumentu, walidacja treści faktury, kodowania XML inne niż UTF-8.
- Bez walidatora Factur-X (np. Mustang) nie sprawdzono reguł BR-HYBRID poza tym, co wynika z kodu i testów.

## Walidacja Mustang (2026-11-21)

Mustang CLI 2.26.0 (ZUGFeRD/mustangproject, Apache-2.0; poza repozytorium w `bench/tools/mustang/`,
uruchamiany Javą z Homebrew) na fakturze z `testdata/documents/20-invoice.html` i
`testdata/facturx/20-invoice-minimum.xml`:

- `-facturx` (PDF/A-3b): PDF zgodny (`flavour=3b`), XML w profilu `urn:factur-x.eu:1p0:minimum` przechodzi
  Schematron FACTUR-X_MINIMUM (25 reguł, 0 błędów), wynik ogólny `valid`;
- `-facturx -pdfua 1` (PDF/A-3a z tagami i PDF/UA-1): PDF zgodny (`flavour=3a`), XML 0 błędów, `valid`.

Pozostaje niesprawdzone: profile wyższe niż MINIMUM (brak własnych faktur CII w tych profilach w
`testdata/`) i zgodność danych XML z treścią HTML, której żaden walidator nie sprawdza.
