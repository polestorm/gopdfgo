# ADR 0051: Podpis z linii poleceń (klucz z pliku) i puste pola podpisu wskazane w HTML

Status: przyjęta 2026-11-24 (decyzja użytkownika: klucz CLI z pliku PKCS#12); zrobione 2026-11-23.

## Kontekst

ADR 0050 zostawił dwie sprawy: flagę podpisu w CLI (czekała na decyzję, skąd brać klucz, bo pkt 3 ADR 0050
mówił, że gopdfgo kluczy z plików nie czyta) i podpis widoczny (pole wskazane elementem HTML przez zaczep
w rdzeniu). ADR 0049 pkt 4 kazał rozstrzygnąć przy pierwszym pakiecie opcjonalnym, jak zwalidowane CLI
ma się obejść bez jego kodu. Użytkownik zdecydował: CLI czyta klucz z pliku PKCS#12 wskazanego jawnie.

## Decyzja

### Część 1: polecenie `gopdfgo-sign`

1. **Osobny program** `cmd/gopdfgo-sign` (ADR 0049 pkt 4): podpisuje gotowy PDF. `cmd/gopdfgo` nadal nie
   importuje `sign` ani `sign/keyfile` (test `tests/optional_packages_test.go`: `go list -deps` i `go tool
   nm` binarki `cmd/gopdfgo`, a dla kontroli samego testu — symbole obu pakietów w binarce `gopdfgo-sign`).
   Osobny program zamiast znacznika kompilacji: bez wariantów jednej binarki, a renderowanie i podpis to
   i tak osobne kroki (podpis bywa robiony gdzie indziej niż renderowanie, np. na stanowisku z kartą).
   `gopdfgo-sign` nie importuje rdzenia: 8,9 MB (pomiar 2026-11-23, darwin/arm64, go1.26.6, `go build`
   bez flag, `ls -l`), binarka `gopdfgo` 25,2 MB.
2. **Flagi**: `-in`, `-out`; klucz z `-p12 plik.p12` z hasłem z `-password-env NAZWA` albo z pierwszego
   wiersza pliku `-password-file` (końcowe `\r` usuwane); hasła nie ma jako argumentu (widać je w `ps`
   i w historii powłoki), a `-password` daje błąd użycia; bez obu hasło jest puste (plik bez hasła), a złe
   hasło kończy się błędem z podpowiedzią o obu flagach. Albo `-key klucz.pem -cert certyfikat.pem`
   (ten sam plik może mieć klucz i certyfikat); `-chain łańcuch.pem` z oboma sposobami. `-level B-B|B-T|B-LT`
   (domyślnie B-B), `-tsa URL`, `-reason`, `-location`, `-name`, `-contact`, `-field`, `-timeout`
   (domyślnie minuta, limit całego podpisu i klienta HTTP).
3. **Sieć**: połączenia są możliwe tylko z podanym `-tsa`; B-T i B-LT bez niego to błąd użycia (kod 2)
   przed odczytem klucza. B-LT pobiera OCSP i CRL z adresów zapisanych w certyfikatach (`sign.RevocationClient`)
   tym samym klientem HTTP; danych o ważności podanych wprost CLI na razie nie przyjmuje.
4. **Zapis wyniku** przez plik tymczasowy w katalogu docelowym i `rename`: przerwany zapis nie zostawia
   uciętego pliku, a `-out` może wskazywać plik wejściowy.

### Część 2: odczyt kluczy — pakiet `sign/keyfile`

5. **Miejsce**: osobny pakiet `github.com/polestorm/gopdfgo/sign/keyfile`, nie `sign` i nie `main`
   polecenia. `sign` nadal nie czyta kluczy z plików (ADR 0050 pkt 3 w nowym brzmieniu): dostaje
   `crypto.Signer`, więc program z HSM albo podpisem zdalnym nie wciąga kodu PKCS#12. Pakiet, a nie kod
   w `main`, bo ten sam odczyt przyda się programom, które wołają `sign` z Go, i da się go testować
   osobno. API: `ParsePKCS12(data, password) (*Identity, error)`, `ParsePEM(key, certs, chain)`,
   `ParsePEMKey`, `ParsePEMCertificates`; `Identity{Signer, Certificate, Chain}` pasuje wprost do
   `sign.Options`. Błędy: `ErrPassword`, `ErrUnsupportedAlgorithm`, `ErrMalformed`, `ErrNoKey`,
   `ErrNoCertificate`, `ErrAmbiguous`, `ErrEncryptedPEM`.
6. **PKCS#12 kodem własnym** na bibliotece standardowej (`encoding/asn1`, `crypto/pbkdf2`, `crypto/aes`,
   `crypto/des`, `crypto/hmac`; ok. 570 linii z komentarzami), bez `software.sslmate.com/src/go-pkcs12`
   (BSD-3, dopuszczalny): ten moduł wnosi własny RC2 i odczyt formatów, których nie potrzebujemy, a sam
   format to kilka struktur ASN.1 i dwie funkcje wyprowadzania klucza. Obsługiwane:
   - integralność hasłem: HMAC z kluczem z KDF PKCS#12 (RFC 7292 dodatek B.2) dla SHA-1, SHA-256, SHA-384,
     SHA-512 oraz PBMAC1 z PBKDF2 (RFC 9579; OpenSSL 3.4+ `-pbmac1_pbkdf2`); porównanie w stałym czasie;
     puste hasło sprawdzane w obu zapisach (BMPString z zakończeniem i bez bajtów, dodatek B.1);
   - szyfrowanie PBES2 z PBKDF2 (RFC 8018; PRF HMAC-SHA1/256/384/512; AES-128/192/256-CBC i 3DES-CBC) —
     domyślne w OpenSSL 3 (`AES-256-CBC`, `hmacWithSHA256`, MAC SHA-256, 2048 iteracji; sprawdzone na
     OpenSSL 4.0.3) — oraz `pbeWithSHAAnd3-KeyTripleDES-CBC` i `…2-KeyTripleDES-CBC` (starsze programy,
     np. OpenSSL 1.x dla klucza);
   - torby `keyBag`, `pkcs8ShroudedKeyBag`, `certBag` z certyfikatem X.509; certyfikat klucza wybierany
     po `localKeyId`, a bez niego po kluczu publicznym; łańcuch uporządkowany od wystawcy w górę (nazwa
     i podpis), reszta certyfikatów za nim; więcej niż jeden klucz — `ErrAmbiguous`.
   - **RC2 i RC4** (`pbeWithSHAAnd40BitRC2-CBC` dla certyfikatów w plikach z OpenSSL 1.x i `-legacy`):
     bez obsługi, jasny błąd `ErrUnsupportedAlgorithm` z gotowym poleceniem konwersji (`openssl pkcs12 -legacy
     … -nodes`, potem `openssl pkcs12 -export … -certpbe AES-256-CBC -keypbe AES-256-CBC -macalg SHA256`).
     Powód: szyfr 40-bitowy nie chroni niczego (chroni tylko certyfikaty, które są publiczne), sam OpenSSL 3
     czyta go dopiero z `-legacy`, a własny RC2 (RFC 2268) to kod bez innego zastosowania.
   - Limit 10 mln iteracji (plik złośliwy nie zablokuje programu na godziny); integralność kluczem
     publicznym (`signedData`) — bez obsługi.
7. **PEM**: klucz `PRIVATE KEY` (PKCS#8), `RSA PRIVATE KEY` (PKCS#1), `EC PRIVATE KEY` (SEC1, z pominięciem
   `EC PARAMETERS`); zaszyfrowany (`ENCRYPTED PRIVATE KEY` albo nagłówek `Proc-Type: 4,ENCRYPTED`) daje
   `ErrEncryptedPEM` z podpowiedzią, żeby użyć PKCS#12. Szyfrowanego PEM nie obsługujemy: format z nagłówkiem
   `DEK-Info` jest przestarzały (Go oznaczył `x509.DecryptPEMBlock` jako niebezpieczne), a PKCS#8 zaszyfrowany
   to te same PBES2, ale drugi sposób podawania hasła, a plik z hasłem i tak ma być PKCS#12.

### Część 3: puste pola podpisu w HTML

8. **Wskazanie**: atrybut `data-pdf-signature="Nazwa"` na dowolnym elemencie; wartość (po obcięciu spacji)
   to nazwa pola `/T`. Atrybut `data-*`, a nie właściwość CSS: nie zmienia kaskady, nie dziedziczy się, nie
   wymaga rozszerzeń parsera CSS i jest poprawnym HTML (HTML § 3.2.6.6). Nazwa pusta, z kropką (ISO 32000-1
   § 12.7.3.2: kropka rozdziela nazwy częściowe) albo powtórzona daje ostrzeżenie i pole jest pomijane
   (pierwsze z powtórzonych zostaje).
9. **Zawsze**: rdzeń tworzy pole za każdym razem, gdy atrybut jest w HTML, bez rejestracji pakietu
   (inaczej niż przewiduje ADR 0049 pkt 2 dla funkcji z kodem opcjonalnym). Puste pole to zwykła struktura
   formularza PDF (`/AcroForm`), bez kryptografii; dokument z polem można podpisać później dowolnym programem
   (`sign.SignPDF`, `gopdfgo-sign -field`, Acrobat). Rdzeń nie importuje `sign`. Bez atrybutu wynik jest bajt
   w bajt ten sam co przed zmianą (pkt 13).
10. **Miejsce**: prostokąt obramowania (`border box`, jak obszar odnośnika) pierwszego pudełka elementu
    w dokumencie: pierwsza strona, na której element ma pudełko, i pierwszy fragment w kolejności drzewa
    pudełek (element dzielony między strony albo wiersze dostaje pole tylko na pierwszym fragmencie; jedno
    pole ma jeden widżet). Pudełka pseudoelementów, tekstu i wierszy pomijane; przekształcenia `transform`
    jak przy odnośnikach (prostokąt otaczający). Element powtarzany na stronach (np. biegnący) — pole na
    pierwszej stronie.
11. **Zapis** (ISO 32000-1 § 12.7.3.1, § 12.7.4.5): pole i widżet w jednym słowniku: `/Type /Annot
    /Subtype /Widget /FT /Sig /T … /TU …`, `/Rect`, `/F 4` (Print; ISO 19005-2 § 6.3.2), bez `/V`;
    `/AP << /N forma >>` — pusta forma o `/BBox` wielkości prostokąta. PDF/A-2 § 6.3.3 wymaga `/AP` dla
    widżetu o niezerowym prostokącie; treść elementu jest już narysowana na stronie, więc forma nic nie
    dorysowuje (forma z treścią pudełka narysowałaby ją drugi raz). `/AcroForm` z `/Fields` w kolejności
    stron i `/NeedAppearances false` (zapis przejęty z pdflib); bez `/SigFlags`, bo pole bez podpisu nie
    ustawia `SignaturesExist` (tabela 219) — ustawia je podpis. `/TU` (opis pola): `aria-label`, `title`,
    tekst elementu, a bez nich nazwa pola.
12. **PDF z tagami** (PDF/UA-1 § 7.18.4, ISO 32000-1 § 14.8.4.5): widżet ma `/StructParent` i własny
    element struktury `Form` z `OBJR`, dzieckiem elementu struktury pola (np. `Div(MCR Form(OBJR))`);
    element pola bez innej treści też trafia do drzewa. Strona ma `/Tabs /S` jak przy odnośnikach. Widżet
    bez elementu w drzewie dostaje `Form` w korzeniu (jak `Link` przy odnośnikach).
13. **Podpis w istniejącym polu**: `sign.Options.FieldName` to teraz pełna nazwa istniejącego pustego pola
    podpisu (nazwy częściowe połączone kropkami, `/FT` także dziedziczone). Aktualizacja przyrostowa
    przepisuje tylko słownik pola (dochodzi `/V`) i `/AcroForm` (`/SigFlags 3`); widżet, strona i lista pól
    zostają bez zmian. Nazwa nieznana albo pole innego rodzaju — `ErrUnknownField`; pole podpisane —
    `ErrFieldNotEmpty`. **Zmiana wobec rund 1 i 2**: wcześniej niepusta `FieldName` nadawała nazwę nowemu
    polu niewidocznemu; teraz pole niewidoczne zawsze dostaje pierwszą wolną nazwę `SignatureN`, a nazwa
    wskazuje pole istniejące. Bez `FieldName` wynik jest bajt w bajt jak w rundzie 1
    (`TestLevelBBUnchangedSinceRoundOne`).

## Konsekwencje

- Zwalidowana binarka `gopdfgo` ma tylko zaczep pola (ok. 200 linii w rdzeniu, +37 kB binarki: 25 128 978
  → 25 166 418 B, pomiar jak w pkt 1) i bez atrybutu daje te same bajty; kod kryptograficzny i odczyt kluczy
  są w `gopdfgo-sign`.
- Pole widoczne zależy od układu: przesunięcie elementu przesuwa widżet. `sign.SignPDF` nie zmienia wyglądu
  pola ani strony; to, co ma być widać w miejscu podpisu (np. „Podpis kierownika”), jest treścią HTML elementu.
- Dalej: dane o ważności podane wprost w CLI (pliki OCSP i CRL), wygląd podpisu (`/AP` z nazwą
  i czasem) jako opcja `sign`, `/Lock` i `/SV` pola z HTML, B-LTA.

## Sprawdzenie (2026-11-23, darwin/arm64, go1.26.6)

- `go test ./sign/keyfile/`: pliki PKCS#12 z własnego zapisu tylko do testu (PBES2 + PBKDF2-HMAC-SHA256 +
  AES-256-CBC z MAC SHA-256 oraz pbeWithSHAAnd3-KeyTripleDES-CBC z MAC SHA-1; RSA 2048 i ECDSA P-256; hasło
  z polskimi znakami i puste; złe hasło), z `openssl pkcs12 -export` (OpenSSL 4.0.3): domyślny, 3DES z MAC
  SHA-1, PBMAC1, `-legacy` (błąd z poleceniem konwersji); PEM: PKCS#8, PKCS#1, SEC1 z `EC PARAMETERS`,
  zaszyfrowane (błąd), certyfikat innego klucza (błąd).
- `go test ./cmd/gopdfgo-sign/`: PDF/A-2b z gopdfgo z polem `Podpis1` podpisany kluczem PEM i z plików
  PKCS#12 (OpenSSL 3 domyślny i 3DES/SHA-1, hasło z `-password-env` i z `-password-file`), drugi podpis
  w nowym polu niewidocznym; `pdfsig -nocert` (Poppler 26.10.0): „Signature is Valid.” dla każdego podpisu;
  zbudowana binarka: kod wyjścia 2 dla `-password`; błędy użycia; B-T z lokalnym TSA odmawiającym (jedno
  żądanie, tylko pod adres `-tsa`).
- `go test ./tests/ -run SignatureField`: pole na stronie 2 w miejscu pudełka (A4, 2 cm od lewej, 3 cm od
  góry, 6 × 2 cm, zgodność do 0,05 pt), `/F 4`, `/TU`, bez `/SigFlags`; element dzielony między strony —
  pole na pierwszej stronie na pierwszym fragmencie; trzy ostrzeżenia (pusta nazwa, kropka, powtórzenie);
  `Div(# Form(@))` w PDF/UA; obie drogi rysowania (`document.StreamingDisabled`, ADR 0036) dają te same
  bajty w zwykłym PDF, PDF/A-2b i PDF/A-2a + PDF/UA-1; veraPDF `-f 2b`, `-f 2a`, `-f ua1`: zgodne.
- `go test ./sign/` (z `GOPDFGO_VERAPDF`): podpis w polu z HTML w zwykłym PDF, PDF/A-2b i PDF/A-2a +
  PDF/UA-1, potem drugi podpis w drugim polu; pole, lista pól i strony bez zmian; `pdfsig`: oba podpisy
  ważne, ostatni „Total document signed”; `openssl cms -verify`: poprawne; veraPDF po podpisaniu obu pól:
  `-f 2b`, `-f 2a`, `-f ua1` zgodne.
- Bez atrybutu wynik bez zmian: `bench/verify/w3cbytes.py` (binarka sprzed zmiany, commit ded0dd5, wobec
  nowej) — 11 689 dokumentów, 46 756 renderowań, 0 różnic bajtowych; `bench/verify/corpus.sh` — `fail=0`.
