// Package sign podpisuje gotowe pliki PDF podpisem cyfrowym PAdES (ETSI EN 319 142-1).
//
// Pakiet jest opcjonalny (ADR 0049, 0050): rdzeń gopdfgo go nie importuje, więc program, który
// go nie importuje, nie ma w binarce kodu podpisu. Podpis CMS (RFC 5652) jest odłączony, SHA-256,
// z atrybutami podpisanymi content-type, message-digest i signing-certificate-v2, w niewidocznym
// polu podpisu na pierwszej stronie albo w istniejącym pustym polu wskazanym przez Options.FieldName.
// Podpis jest dopisywany aktualizacją przyrostową, więc działa na każdym poprawnym, niezaszyfrowanym PDF
// (także z tagami, PDF/A-2, PDF/A-3, Factur-X i już podpisanym) i nie narusza wcześniejszych podpisów.
// Pakiet nie czyta kluczy z plików: klucz i certyfikat podaje wywołujący; odczyt z plików PKCS#12
// i PEM daje osobny pakiet sign/keyfile (z niego korzysta polecenie gopdfgo-sign, ADR 0051).
//
// Pole widoczne: element HTML z atrybutem data-pdf-signature="Nazwa" dostaje przy renderowaniu puste
// pole podpisu na prostokącie swojego pudełka (rdzeń gopdfgo, bez kodu tego pakietu; w PDF z tagami
// widżet ma element struktury Form). Podpis w tym polu:
//
//	opts.FieldName = "Nazwa"
//	signed, err := sign.SignPDF(ctx, result.PDF, opts)
//
// Pole dostaje /V, a widżet, strona i wygląd pola zostają bez zmian: to, co widać w miejscu podpisu,
// jest treścią HTML elementu. Nazwa nieznana daje ErrUnknownField, pole już podpisane ErrFieldNotEmpty.
//
// Poziomy (Options.Level):
//
//   - LevelBB (domyślny): sam podpis; bez połączeń sieciowych.
//   - LevelBT: podpis i znacznik czasu RFC 3161 wartości podpisu (atrybut niepodpisany
//     signature-time-stamp-token) od Options.TSA.
//   - LevelBLT: jak LevelBT, a do tego druga aktualizacja przyrostowa ze słownikiem DSS
//     (ISO 32000-2 § 12.8.4.3): certyfikaty, odpowiedzi OCSP i listy CRL dla certyfikatu
//     podpisującego, certyfikatu TSA i ich wystawców, z wpisem /VRI podpisu.
//
// Pakiet łączy się z siecią tylko przez klientów podanych jawnie: Options.TSA (TimestampAuthority,
// np. TSAClient z adresem i *http.Client) i Options.Revocation (RevocationFetcher, np.
// RevocationClient, który bierze adresy OCSP i CRL z certyfikatów). Odpowiedzi sprawdza sam pakiet
// (status, skrót, nonce, podpisy TSA i respondera, status certyfikatu), więc własna implementacja
// tych interfejsów odpowiada tylko za przesył. ctx przerywa połączenia i podpis (ContextSigner).
//
// Podpis PAdES B-B:
//
//	result, err := gopdfgo.Render(ctx, html, gopdfgo.Options{Fonts: fonts, Conformance: gopdfgo.PDFA2b})
//	if err != nil {
//		return err
//	}
//	// key: *rsa.PrivateKey albo *ecdsa.PrivateKey (albo dowolny crypto.Signer, np. HSM)
//	// cert: *x509.Certificate odpowiadający kluczowi; chain: certyfikaty pośrednie
//	signed, err := sign.SignPDF(ctx, result.PDF, sign.Options{
//		Signer:      key,
//		Certificate: cert,
//		Chain:       chain,
//		Reason:      "Zatwierdzenie dokumentu",
//		Location:    "Warszawa",
//	})
//
// PAdES B-T (znacznik czasu):
//
//	client := &http.Client{Timeout: 30 * time.Second}
//	signed, err := sign.SignPDF(ctx, result.PDF, sign.Options{
//		Signer:      key,
//		Certificate: cert,
//		Chain:       chain,
//		Level:       sign.LevelBT,
//		TSA:         sign.TSAClient{URL: "https://tsa.example/rfc3161", HTTPClient: client},
//	})
//
// PAdES B-LT z danymi o ważności pobranymi z sieci (OCSP z authorityInfoAccess, CRL
// z cRLDistributionPoints). Chain powinien sięgać korzenia, a certyfikaty wystawców certyfikatu TSA
// podaje ValidationData.Certificates:
//
//	signed, err := sign.SignPDF(ctx, result.PDF, sign.Options{
//		Signer:         key,
//		Certificate:    cert,
//		Chain:          []*x509.Certificate{intermediate, root},
//		Level:          sign.LevelBLT,
//		TSA:            sign.TSAClient{URL: "https://tsa.example/rfc3161", HTTPClient: client},
//		Revocation:     sign.RevocationClient{HTTPClient: client},
//		ValidationData: sign.ValidationData{Certificates: []*x509.Certificate{tsaCA}},
//	})
//
// PAdES B-LT offline: odpowiedzi OCSP i listy CRL pobrane wcześniej (DER) zamiast Revocation;
// brakujące dane dla któregoś certyfikatu dają ErrNoValidationData:
//
//	opts.Revocation = nil
//	opts.ValidationData = sign.ValidationData{
//		Certificates:  []*x509.Certificate{tsaCA},
//		OCSPResponses: [][]byte{signerOCSP},
//		CRLs:          [][]byte{caCRL},
//	}
//
// Podpisujący zewnętrzny (HSM, karta, zdalny podpis kwalifikowany) implementuje crypto.Signer:
// Public zwraca klucz publiczny z certyfikatu, a Sign(rand, digest, crypto.SHA256) dostaje skrót
// SHA-256 atrybutów podpisanych i zwraca podpis RSA PKCS#1 v1.5 albo ECDSA w DER. Podpisujący,
// który implementuje też ContextSigner, dostaje ctx w SignContext. SignPDF sprawdza podpis kluczem
// z certyfikatu, zanim go zapisze.
//
// Na poziomie B-LT ostatnia aktualizacja pliku (DSS) leży poza zakresem /ByteRange podpisu, jak
// przewiduje ISO 32000-2 § 12.8.4.3; walidatory PAdES traktują ją jako dozwoloną, a pdfsig (Poppler)
// pokazuje przy podpisie „Not total document signed”.
package sign
