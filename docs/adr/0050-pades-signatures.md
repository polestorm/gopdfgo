# ADR 0050: Podpis cyfrowy PAdES (pakiet `sign`)

Status: przyjęta 2026-11-24 (decyzja użytkownika: zacząć od PAdES); wdrażana rundami — runda 1 (B-B) zrobiona.

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
   kwalifikowany). gopdfgo nie przechowuje i nie odczytuje kluczy z plików sam.
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
