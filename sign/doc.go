// Package sign podpisuje gotowe pliki PDF podpisem cyfrowym PAdES (ETSI EN 319 142-1).
//
// Pakiet jest opcjonalny (ADR 0049, 0050): rdzeń gopdfgo go nie importuje, więc program, który
// go nie importuje, nie ma w binarce kodu podpisu. Runda 1 daje poziom PAdES B-B: podpis CMS
// (RFC 5652) odłączony, SHA-256, atrybuty podpisane content-type, message-digest
// i signing-certificate-v2, niewidoczne pole podpisu na pierwszej stronie. Podpis jest dopisywany
// aktualizacją przyrostową, więc działa na każdym poprawnym, niezaszyfrowanym PDF (także z tagami,
// PDF/A-2, PDF/A-3, Factur-X i już podpisanym) i nie narusza wcześniejszych podpisów. Pakiet
// nie łączy się z siecią i nie czyta kluczy z plików: klucz i certyfikat podaje wywołujący.
//
// Użycie:
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
// Podpisujący zewnętrzny (HSM, karta, zdalny podpis kwalifikowany) implementuje crypto.Signer:
// Public zwraca klucz publiczny z certyfikatu, a Sign(rand, digest, crypto.SHA256) dostaje skrót
// SHA-256 atrybutów podpisanych i zwraca podpis RSA PKCS#1 v1.5 albo ECDSA w DER. SignPDF sprawdza
// podpis kluczem z certyfikatu, zanim go zapisze.
package sign
