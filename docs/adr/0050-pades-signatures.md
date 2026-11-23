# ADR 0050: Podpis cyfrowy PAdES (pakiet `sign`)

Status: przyjęta 2026-11-24 (decyzja użytkownika: zacząć od PAdES); wdrażana rundami — runda 1 (B-B) i runda 2 (B-T, B-LT) zrobione;
polecenie `gopdfgo-sign` (klucz z pliku) i podpis w polu wskazanym w HTML: ADR 0051. Pkt 3 zmieniony 2026-11-24.

## Kontekst

PAdES (ETSI EN 319 142-1, oparty na ISO 32000-1 § 12.8 i ISO 32000-2) to podpis zapisany w samym pliku
PDF: pole podpisu, słownik podpisu z zarezerwowanym miejscem `/Contents` i tablicą `/ByteRange`
(wszystkie bajty pliku poza tym miejscem), a w `/Contents` podpis CMS (RFC 5652) w formie odłączonej
z certyfikatem podpisującego. Poziomy: B-B (podpis z certyfikatem), B-T (ze znacznikiem czasu RFC 3161),
B-LT (z danymi o ważności certyfikatów w słowniku DSS: OCSP, CRL), B-LTA (ze znacznikiem czasu
dokumentu). Biblioteka standardowa Go nie ma zapisu CMS ani klienta RFC 3161. folio (Apache-2.0) ma
pakiet `sign` (ok. 3,2 tys. linii) z PAdES B-B, B-T, B-LT i zewnętrznym podpisującym.

## Decyzja

1. Pakiet `github.com/polestorm/gopdfgo/sign` według ADR 0049; rdzeń go nie importuje.
2. Podpisanie gotowego pliku PDF: `sign.SignPDF(ctx, pdf []byte, opts sign.Options) ([]byte, error)`.
   Działa na PDF z gopdfgo i na dowolnym poprawnym PDF: dopisuje aktualizację przyrostową (ISO 32000-1
   § 7.5.6) — pole podpisu, słownik podpisu, `/AcroForm` z `/SigFlags 3` — bez naruszania wcześniejszych
   bajtów, więc istniejące podpisy zostają ważne.
3. Klucz: podpisujący to `crypto.Signer` z Go (klucz lokalny RSA lub ECDSA) albo interfejs
   zewnętrznego podpisującego, który dostaje skrót i zwraca podpis (HSM, karta, zdalny podpis
   kwalifikowany). Pakiet `sign` nie przechowuje i nie odczytuje kluczy z plików. Zmiana 2026-11-24
   (decyzja użytkownika, ADR 0051): polecenie `gopdfgo-sign` czyta klucz z pliku wskazanego jawnie
   (PKCS#12 z hasłem ze zmiennej środowiska albo z pliku, albo PEM bez szyfrowania) przez osobny pakiet
   `sign/keyfile`; biblioteka `sign` dalej dostaje tylko `crypto.Signer`.
4. Poziomy: runda 1 — PAdES B-B (SHA-256, `SubFilter /ETSI.CAdES.detached`, atrybut
   `signing-certificate-v2`), niewidoczny podpis; runda 2 — B-T (klient RFC 3161) i B-LT (DSS z OCSP
   i CRL); B-LTA i podpis widoczny (pole wskazane elementem HTML przez zaczep w rdzeniu) później.
5. Sieć: znacznik czasu i dane o ważności wymagają połączeń. Pakiet łączy się wyłącznie przez klienta
   HTTP i adresy podane jawnie w `sign.Options` (np. `TSA: sign.TSAClient{URL, HTTPClient}`); bez nich
   nie ma żadnego połączenia, a poziom wymagający sieci kończy się błędem. Rdzeń gopdfgo nadal nie
   łączy się z siecią.
6. Zgodność: podpis nie zmienia treści stron; PDF/A-2 i PDF/A-3 dopuszczają podpisy (ISO 19005-2
   § 6.4.3), więc `sign.SignPDF` zachowuje zgodność PDF/A pliku wejściowego; PDF/UA nie jest naruszany
   (podpis niewidoczny jest poza strukturą treści).
7. Sprawdzanie: `pdfsig` (Poppler) i `openssl cms -verify` na wyniku; veraPDF dla PDF/A po podpisaniu;
   certyfikaty i klucze testowe generowane w testach (bez plików z kluczami w repozytorium albo z kluczem
   testowym jawnie opisanym jako testowy); walidator EU DSS tylko za zgodą użytkownika.
8. Kod folio: wolno się wzorować (Apache-2.0); przejęcie kodu wprost — z atrybucją w `THIRD_PARTY_NOTICES`
   i `NOTICE` oraz wpisem w tym ADR.

## Konsekwencje

- Binarka i biblioteka bez importu `sign` nie zawierają kodu kryptograficznego podpisu ani klienta sieci.
- Podpis zależy od czasu (atrybut `signing-time`, znacznik czasu), więc podpisany PDF nie jest
  powtarzalny bajt w bajt; powtarzalność obowiązuje do chwili podpisania (opcja stałego czasu do testów).

## Stan rundy 1 (2026-11-23): PAdES B-B

Zrobione w pakiecie `sign` (ok. 1,6 tys. linii bez testów, wyłącznie biblioteka standardowa Go; kod
własny, folio tylko przeczytane — bez przejętego kodu, więc bez wpisów w `THIRD_PARTY_NOTICES` i `NOTICE`).

API: `sign.SignPDF(ctx, pdf, sign.Options{Signer, Certificate, Chain, Time, Name, Reason, Location,
ContactInfo, FieldName})`; błędy `ErrNoSigner`, `ErrUnsupportedKey`, `ErrKeyMismatch`, `ErrEncrypted`.

Decyzje szczegółowe:

1. **Czytnik struktury PDF własny** (`sign/parse.go`, ok. 830 linii), nie `github.com/benoitkugler/pdf/reader/file`
   (MIT, zależność modułu). Czytnik benoitkugler obsługuje strumienie odsyłaczy i obiektów, ale: liczby
   rzeczywiste zamienia na `float32` (ok. 7 cyfr znaczących, więc przepisana strona mogłaby dostać inne
   `/MediaBox` niż oryginał),
   ciągi rozkodowuje bez śladu formy, słowniki trzyma w mapie (kolejność kluczy ginie), wczytuje wszystkie
   obiekty pliku, a uszkodzone odsyłacze odbudowuje po cichu (aktualizacja z `/Prev` wskazującym złe
   miejsce). Własny czytnik czyta tylko odsyłacze (tablice, strumienie, pliki hybrydowe z `/XRefStm`,
   łańcuch `/Prev`), trailer i kilka obiektów, o które pyta podpis (katalog, drzewo stron do pierwszej
   strony, `/AcroForm`, `/Fields`, `/Annots`); obiekty w strumieniach obiektów rozkodowuje (FlateDecode
   z predyktorami PNG); liczby zachowuje w postaci źródłowej, słowniki w kolejności kluczy. Przepisane
   obiekty mają więc te same wartości co oryginał; ciągi są zapisywane szesnastkowo (te same bajty).
2. **Aktualizacja przyrostowa** (ISO 32000-1 § 7.5.6): nowy słownik podpisu i pole podpisu połączone
   z widżetem (`/FT /Sig`, `/Rect [0 0 0 0]`, `/F 132` — Print i Locked, bo PDF/A-2 § 6.3.2 wymaga Print;
   prostokąt zerowy zwalnia z `/AP`), przepisane: pierwsza strona (albo jej pośrednia tablica `/Annots`),
   katalog z `/AcroForm` (albo pośredni słownik `/AcroForm`, albo pośrednia tablica `/Fields`), w plikach
   z tagami strona dostaje `/Tabs /S` (PDF/UA-1 § 7.18.3), jeśli go nie ma. Sekcja odsyłaczy jest tego
   samego rodzaju co najnowsza w pliku: tablica (gopdfgo bez tagów, PDF/A-2b, 3b, Factur-X) albo
   strumień bez kompresji (gopdfgo z tagami, PDF/UA). Trailer: `/Prev`, te same `/Root` i `/Info`, `/ID`
   z pierwszym elementem bez zmian i nowym drugim (§ 14.4, skrót z pierwszego, długości pliku i czasu).
   Plik zaszyfrowany kończy się błędem `ErrEncrypted`.
3. **CMS** (`sign/cms.go`, DER własnymi funkcjami na `encoding/asn1`): ContentInfo/SignedData v1, odłączony
   (`encapContentInfo` bez `eContent`), `digestAlgorithms` SHA-256 bez parametrów (RFC 5754 § 2), certyfikat
   podpisującego, potem łańcuch, SignerInfo v1 z `IssuerAndSerialNumber`. Atrybuty podpisane w porządku
   DER (X.690 § 11.6): `content-type` = id-data, `message-digest`, `signing-certificate-v2` (RFC 5035;
   ESSCertIDv2 z SHA-256 certyfikatu — algorytm domyślny, więc pominięty w DER — i `issuerSerial`, który
   ETSI EN 319 122-1 § 5.2.2.3 zaleca). Bez `signing-time`: ETSI EN 319 142-1 § 6.3 każe podać czas
   w `/M`, a atrybutu nie używać. Algorytmy podpisu: `sha256WithRSAEncryption` (1.2.840.113549.1.1.11,
   parametry NULL) i `ecdsa-with-SHA256` (1.2.840.10045.4.3.2) dla P-256, P-384, P-521.
4. **Rezerwacja `/Contents`**: długość tej samej struktury CMS z podpisem najdłuższym możliwym dla klucza
   certyfikatu (RSA: rozmiar modułu; ECDSA: dwa całkowite DER) plus 32 B; dopełnienie zerami. Dla RSA
   2048 z certyfikatem samopodpisanym to ok. 1,5 kB (3 kB szesnastkowo).
5. **Podpisujący**: `crypto.Signer` wystarcza i dla klucza lokalnego, i dla zewnętrznego — `Sign` dostaje
   skrót SHA-256 atrybutów podpisanych z `crypto.SHA256` i zwraca podpis RSA PKCS#1 v1.5 albo ECDSA w DER.
   `SignPDF` porównuje `Signer.Public()` z kluczem certyfikatu i sprawdza zwrócony podpis kluczem
   z certyfikatu (wyłapuje np. podpis PSS albo zły klucz w HSM). `ctx` jest sprawdzany przed podpisem;
   `crypto.Signer` nie przyjmuje kontekstu, więc przerwanie zdalnego podpisu w trakcie to sprawa
   implementacji podpisującego (do rozważenia w rundzie 2 razem z klientem TSA).
6. **Powtarzalność**: przy stałym `Options.Time` podpis RSA daje te same bajty; ECDSA w Go jest losowy,
   więc tam powtarzalne są bajty poza `/Contents`.
7. **CLI**: `cmd/gopdfgo` nie importuje `sign` (test `tests/optional_packages_test.go`: `go list -deps`
   rdzenia i `go tool nm` binarki). Flaga podpisu w CLI (ADR 0049 pkt 4) czeka na decyzję, skąd CLI ma brać
   klucz (plik PKCS#8/PKCS#12 to odczyt kluczy z plików, którego punkt 3 tego ADR nie przewiduje).
   Rozstrzygnięte w ADR 0051: osobny program `cmd/gopdfgo-sign`, klucz z pliku przez `sign/keyfile`.

Sprawdzenie (2026-11-23, darwin/arm64, go1.26): `go test ./sign/` — RSA 2048, ECDSA P-256 i P-384 na PDF
z gopdfgo bez tagów, z tagami (PDF/A-2a + PDF/UA-1, strumień odsyłaczy), PDF/A-2b, PDF/A-3b, Factur-X
i na ręcznie zapisanym PDF z tablicą odsyłaczy i pośrednimi `/AcroForm` i `/Annots`; drugi podpis na pliku
podpisanym; powtarzalność; błędy. W każdym przypadku: własne sprawdzenie CMS w Go, `pdfsig -nocert`
(Poppler 26.10.0): „Signature is Valid.” dla każdego podpisu i „Total document signed” dla ostatniego,
`openssl cms -verify -binary -noverify` (OpenSSL 4.0.3) i z `-CAfile` certyfikatu testowego: „Verification
successful”. veraPDF: podpisane PDF/A-2b (`-f 2b`), PDF/A-3b i Factur-X (`-f 3b`), PDF/A-2a + PDF/UA-1
(`-f 2a`, `-f ua1`, także z dwoma podpisami) zgodne.

Uwaga do PDF/UA: widżet podpisu niewidocznego nie jest w drzewie struktury (veraPDF go nie wymaga
dla prostokąta zerowego). Podpis widoczny w rundzie 2 lub później musi dostać element `Form` z `OBJR`
i `/StructParent` (PDF/UA-1 § 7.18.4).

Runda 2: B-T (klient RFC 3161 przez `Options.TSA`, token w atrybucie niepodpisanym
`signature-time-stamp-token`), B-LT (słownik DSS z OCSP i CRL, aktualizacja przyrostowa po podpisie),
podpisujący z kontekstem; dalej B-LTA (znacznik czasu dokumentu, `/SubFilter /ETSI.RFC3161`), podpis
widoczny, pliki zaszyfrowane, `/DocMDP` (podpis certyfikujący) i poszanowanie `/DocMDP` istniejącego.

## Stan rundy 2 (2026-11-23): PAdES B-T i B-LT

Zrobione w pakiecie `sign` (ok. 1,2 tys. linii nowego kodu bez testów: `tsa.go`, `revocation.go`, `dss.go`
i zmiany w `sign.go`, `cms.go`; wyłącznie biblioteka standardowa Go, bez nowych zależności modułu; kod
własny, folio tylko przeczytane — bez przejętego kodu, więc bez wpisów w `THIRD_PARTY_NOTICES` i `NOTICE`).

API (nowe pola `sign.Options` i typy):

- `Level` (`sign.LevelBB` — wartość zerowa i domyślna, `LevelBT`, `LevelBLT`; `Level.String()` daje „B-B”,
  „B-T”, „B-LT”). Nazwy bez podkreśleń (`LevelBB`, nie `LevelB_B`), zgodnie z konwencją nazw w Go.
- `TSA sign.TimestampAuthority` — interfejs przesyłu `Timestamp(ctx, request []byte) ([]byte, error)`
  (żądanie TimeStampReq i odpowiedź TimeStampResp w DER); `sign.TSAClient{URL, HTTPClient}` to implementacja
  HTTP (RFC 3161 § 3.4, POST `application/timestamp-query`). `TimestampReserve int` — miejsce na token
  w `/Contents`, zero oznacza `sign.DefaultTimestampReserve` (12 KiB).
- `Revocation sign.RevocationFetcher` — interfejs przesyłu `FetchOCSP(ctx, url, request)`
  i `FetchCRL(ctx, url)`; `sign.RevocationClient{HTTPClient}` to implementacja HTTP (OCSP metodą POST według
  RFC 6960 dodatek A.1, CRL metodą GET; tylko adresy `http` i `https` z certyfikatów).
- `ValidationData sign.ValidationData{Certificates, OCSPResponses, CRLs}` — dane podane wprost (offline).
- `sign.ContextSigner` — `crypto.Signer` z `SignContext(ctx, digest, opts)`; gdy `Options.Signer` go
  implementuje, `SignPDF` woła `SignContext` zamiast `Sign`.
- Błędy: `ErrNoTSA`, `ErrNoHTTPClient`, `ErrTimestamp`, `ErrReserveTooSmall`, `ErrNoValidationData`,
  `ErrMissingIssuer`, `ErrRevoked`.

Decyzje szczegółowe:

1. **Poziom domyślny bez zmian**: `Level` zerowe to B-B, przy którym `TSA`, `Revocation`
   i `ValidationData` są pomijane i nie ma żadnego połączenia. Wynik B-B jest bajt w bajt taki sam jak
   w rundzie 1: test `TestLevelBBUnchangedSinceRoundOne` porównuje SHA-256 wyniku dla stałego klucza
   testowego (RSA 2048, w pliku testu, opisany jako wyłącznie testowy), stałego czasu i ręcznie zapisanego
   PDF ze skrótem policzonym kodem rundy 1 (commit f073a28).
2. **Klienci sieci jako interfejsy przesyłu**: `TimestampAuthority` i `RevocationFetcher` przenoszą tylko
   bajty; sprawdzanie odpowiedzi zostaje w pakiecie, więc własna implementacja (uwierzytelnianie TSA,
   pośrednik, pamięć podręczna, inny protokół) nie może pominąć sprawdzeń. `TSAClient` i `RevocationClient`
   wymagają `HTTPClient` (bez niego `ErrNoHTTPClient`, a nie `http.DefaultClient`), żeby połączenie
   zawsze szło przez klienta podanego jawnie (pkt 5 decyzji).
3. **Znacznik czasu** (RFC 3161, ETSI EN 319 122-1 § 5.3): żądanie z SHA-256 pola `signature` SignerInfo
   (AlgorithmIdentifier bez parametrów, RFC 5754 § 2), losowym 63-bitowym `nonce` i `certReq` TRUE.
   Sprawdzane: status `granted` albo `grantedWithMods`, ContentInfo z SignedData o treści id-ct-TSTInfo,
   jeden SignerInfo, atrybuty `content-type` i `message-digest` (skrót TSTInfo), podpis atrybutów kluczem
   certyfikatu TSA (z tokenu albo z `ValidationData.Certificates`), extKeyUsage `timeStamping` certyfikatu
   (RFC 3161 § 2.3), `messageImprint` = SHA-256 wartości podpisu, `nonce` = wysłany. Zaufania do
   certyfikatu TSA, czasu `genTime` ani polityki pakiet nie ocenia — to zadanie walidatora. Token trafia
   do atrybutu niepodpisanego `signature-time-stamp-token` (1.2.840.113549.1.9.16.2.14) jako jedyny
   atrybut `[1] IMPLICIT SET OF Attribute`.
4. **Rezerwacja `/Contents` na B-T**: rozmiar tokenu nie jest znany przed podpisem (podpis obejmuje
   rezerwację, a znacznik czasu powstaje po podpisie), więc rezerwacja to długość tej samej struktury CMS
   z atrybutem niepodpisanym o tokenie długości `TimestampReserve` plus 32 B. Domyślne 12 KiB to szacunek
   górny z zapasem: token z łańcuchem trzech certyfikatów RSA 4096 ma ok. 7 kB (szacunek; token testowego
   TSA z jednym certyfikatem RSA 2048 ma ok. 1,7 kB). Większy token daje `ErrReserveTooSmall` z długością
   tokenu i rezerwacji w komunikacie; pakiet nie ponawia podpisu sam, bo drugie wywołanie podpisującego
   (PIN, podpis zdalny) i drugie żądanie TSA (bywa płatne) to decyzja wywołującego. Żądanie próbne do TSA
   przed podpisem (jak w pyHanko) odrzucone z tego samego powodu.
5. **Dane o ważności** (ETSI EN 319 142-1 § 5.4): ścieżki od certyfikatu podpisującego i od certyfikatu
   TSA do certyfikatu samopodpisanego; wystawca szukany wśród `Certificate`, `Chain`, certyfikatów tokenu
   i `ValidationData.Certificates` (po nazwie i podpisie); bez wystawcy `ErrMissingIssuer` (OCSP i CRL
   wymagają klucza wystawcy; pobieranie wystawcy z AIA `caIssuers` — później, jeśli będzie potrzebne). Dla
   każdego certyfikatu poza samopodpisanym: najpierw odpowiedzi i listy podane wprost, potem pobrane
   (najpierw OCSP z AIA, potem CRL z CRL DP). OCSP: odpowiedź `successful`, typ basic, CertID certyfikatu
   (skrót SHA-1 albo SHA-2), podpis wystawcy albo respondera upoważnionego (extKeyUsage `OCSPSigning`,
   wystawiony przez wystawcę; RFC 6960 § 4.2.2.2), status `good`; certyfikat respondera upoważnionego
   trafia do `/Certs`, a bez rozszerzenia `id-pkix-ocsp-nocheck` potrzebuje też własnych danych. CRL:
   wystawca listy = wystawca certyfikatu, podpis listy, numer seryjny spoza listy (listy pośrednie
   i delta nieobsługiwane). Status `revoked` daje `ErrRevoked`; brak danych — `ErrNoValidationData`
   z nazwą certyfikatu i przyczynami. B-LT bez `Revocation` i bez żadnej odpowiedzi OCSP lub listy CRL
   w `ValidationData` kończy się `ErrNoValidationData` przed podpisem. Świeżości (`thisUpdate`,
   `nextUpdate` względem czasu podpisu) pakiet nie ocenia.
6. **OCSP bez `golang.org/x/crypto/ocsp`**: żądanie (CertID z SHA-1 — identyfikator, nie zabezpieczenie,
   obsługiwany przez każdy responder, RFC 5019 § 2.1.1; bez nonce i podpisu) i odczyt odpowiedzi to ok.
   200 linii na `encoding/asn1`; listy CRL czyta `x509.ParseRevocationList`. Zależność BSD-3 byłaby
   dopuszczalna, ale uprościłaby niewiele, a dodałaby moduł do `go.mod`.
7. **DSS** (ISO 32000-2 § 12.8.4.3, tabela 261): druga aktualizacja przyrostowa po aktualizacji
   z podpisem, więc nie zmienia zakresu `/ByteRange`; sekcja odsyłaczy tego samego rodzaju co w pliku.
   `/Certs`, `/OCSPs`, `/CRLs` to tablice strumieni bez filtra (DER), `/Type /DSS`; `/VRI` (§ 12.8.4.4,
   tabela 262) dostaje wpis dla nowego podpisu z kluczem = SHA-1 całego ciągu `/Contents` (z dopełnieniem
   zerami, jak iText i PDFBox), wielkie litery szesnastkowe, z `/Cert`, `/OCSP`, `/CRL` — bez `/TU`, żeby
   aktualizacja nie zależała od zegara. Istniejący DSS jest uzupełniany: pośredni przepisywany pod tym samym
   numerem, bezpośredni przenoszony do nowego obiektu; dotychczasowe strumienie i wpisy `/VRI` zostają,
   a nowe dane o tej samej treści (porównanie po rozkodowaniu strumieni) nie są powtarzane. Katalog nie
   dostaje rozszerzenia `/Extensions /ESIC` (ETSI TS 102 778-4 je zalecał; EN 319 142-1 nie wymaga).
8. **`ctx`**: przerywa podpis (`ContextSigner`), żądanie TSA i pobieranie OCSP/CRL; po `Sign` bez
   kontekstu `ctx` jest sprawdzany po powrocie. Błąd z anulowania jest zwracany jako `ctx.Err()`
   (`errors.Is(err, context.Canceled)` albo `DeadlineExceeded`).

Sprawdzenie (2026-11-23, darwin/arm64, go1.26.6): `go test ./sign/` — lokalny urząd testowy (RSA 2048),
TSA, responder OCSP i punkt CRL na `httptest` (127.0.0.1, klucze generowane w teście); podpisujący RSA 2048
i ECDSA P-256 wystawieni przez urząd; B-T i B-LT na PDF z gopdfgo bez tagów, PDF/A-2b, PDF/A-3b,
PDF/A-2a + PDF/UA-1 i Factur-X; B-LT z danymi pobranymi (OCSP dla podpisującego, CRL dla TSA),
z danymi podanymi wprost (bez żadnego żądania do serwera), z samą listą CRL i z upoważnionym responderem
OCSP (`id-pkix-ocsp-nocheck`); drugi podpis B-LT na pliku z DSS (DSS uzupełniony bez powtórzeń: 4
certyfikaty, 2 odpowiedzi OCSP, 1 CRL, 2 wpisy `/VRI`); B-B z podanymi klientami bez żadnego żądania; własne
sprawdzenie w Go podpisu CMS, tokenu (podpis TSA, imprint = SHA-256 wartości podpisu), DSS i `/VRI`; błędy:
TSA odmawia, zły imprint, zły nonce, zepsuty podpis tokenu, HTTP 404, `TSAClient` bez `HTTPClient`, B-T bez
TSA, zbyt mała rezerwacja (token 8121 B przy `TimestampReserve` 3000), `ctx` przerwany w czasie żądania TSA
i w czasie `SignContext`, B-LT bez danych, dane tylko dla części certyfikatów, odpowiedź OCSP innego
certyfikatu, zepsuty podpis odpowiedzi OCSP, brak wystawcy, certyfikat unieważniony (OCSP i CRL);
B-B bajt w bajt jak w rundzie 1.

Narzędzia na wynikach testów: `pdfsig -nocert` (Poppler 26.10.0) — „Signature Validation: Signature is
Valid.” dla każdego podpisu na każdym poziomie; po B-T „Total document signed”, po B-LT „Not total document
signed”, bo aktualizacja z DSS leży poza zakresem podpisu (pdfsig nie odróżnia dozwolonej aktualizacji DSS
od innych zmian; tak samo przy dwóch podpisach B-LT — oba ważne, oba „Not total”). Poppler nie pokazuje
znacznika czasu podpisu. `openssl cms -verify -binary` (OpenSSL 4.0.3) z `-noverify` i z `-CAfile`
urzędu testowego: „Verification successful”; `openssl ts -verify -token_in -data <wartość podpisu>
-CAfile <urząd>` na tokenie z każdego podpisu B-T i B-LT: „Verification: OK”. veraPDF (test
`TestVeraPDFAfterLongTerm`, uruchamiany ze zmienną `GOPDFGO_VERAPDF`): po B-T i po dwóch podpisach B-LT
zgodne PDF/A-2b (`-f 2b`), PDF/A-3b i Factur-X (`-f 3b`), PDF/A-2a i PDF/UA-1 (`-f 2a`, `-f ua1`).

Dalej: B-LTA (znacznik czasu dokumentu, `/SubFilter /ETSI.RFC3161`, i dane o ważności tego znacznika),
podpis widoczny (pole wskazane elementem HTML przez zaczep w rdzeniu; w PDF/UA element `Form` z `OBJR`),
`/DocMDP` (podpis certyfikujący) i poszanowanie istniejącego `/DocMDP`, pliki zaszyfrowane, osobne
wywołanie dodające DSS do już podpisanego pliku (przedłużenie istniejących podpisów do B-LT), pobieranie
wystawców z AIA `caIssuers`, flaga podpisu w CLI (czeka na decyzję o źródle klucza, pkt 7 rundy 1).

## Stan po ADR 0051 (2026-11-23)

Zrobione: polecenie `cmd/gopdfgo-sign` (PKCS#12 i PEM przez pakiet `sign/keyfile`, poziomy B-B, B-T, B-LT,
sieć tylko z `-tsa`) oraz podpis w istniejącym pustym polu: `Options.FieldName` wskazuje pole (np. utworzone
przez rdzeń z atrybutu `data-pdf-signature`), które dostaje `/V`; nazwa nieznana daje `ErrUnknownField`, pole
podpisane `ErrFieldNotEmpty`. Pole niewidoczne (bez `FieldName`) zawsze dostaje pierwszą wolną nazwę
`SignatureN`. W PDF/UA widżet pola z HTML jest w elemencie `Form` z `OBJR` (uwaga z rundy 1 załatwiona).
Szczegóły i sprawdzenie: ADR 0051. Dalej bez zmian: B-LTA, `/DocMDP`, pliki zaszyfrowane, przedłużanie
istniejących podpisów do B-LT, wystawcy z AIA, wygląd podpisu (`/AP` z nazwą i czasem).
