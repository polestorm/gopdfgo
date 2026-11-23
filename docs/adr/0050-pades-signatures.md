# ADR 0050: Podpis cyfrowy PAdES (pakiet `sign`)

Status: przyjęta 2026-11-24 (decyzja użytkownika: zacząć od PAdES); wdrażana rundami.

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
