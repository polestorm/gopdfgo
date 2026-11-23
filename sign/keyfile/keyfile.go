// Package keyfile odczytuje klucz prywatny i certyfikaty do podpisu z plików: PKCS#12 (RFC 7292)
// oraz PEM (klucz PKCS#8, PKCS#1 albo SEC1 i certyfikaty X.509).
//
// Pakiet jest opcjonalny (ADR 0049, 0050 pkt 3, ADR 0051): pakiet sign nie czyta kluczy z plików,
// a polecenie gopdfgo-sign czyta je przez ten pakiet z pliku wskazanego jawnie. Wynik (Identity)
// przekazuje się do sign.Options: Signer, Certificate, Chain. Kod własny na bibliotece standardowej Go,
// bez zależności.
//
//	data, _ := os.ReadFile("podpis.p12")
//	id, err := keyfile.ParsePKCS12(data, password)
//	if err != nil {
//		return err
//	}
//	signed, err := sign.SignPDF(ctx, pdf, sign.Options{Signer: id.Signer, Certificate: id.Certificate, Chain: id.Chain})
//
// Zaszyfrowane pliki PEM (ENCRYPTED PRIVATE KEY, nagłówek Proc-Type: 4,ENCRYPTED) nie są obsługiwane:
// dają ErrEncryptedPEM z podpowiedzią; klucz chroniony hasłem przechowuje się w pliku PKCS#12.
package keyfile

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

// Błędy pakietu.
var (
	// ErrPassword: złe hasło (MAC albo dopełnienie po odszyfrowaniu się nie zgadza).
	ErrPassword = errors.New("keyfile: złe hasło albo uszkodzony plik")
	// ErrUnsupportedAlgorithm: algorytm szyfrowania albo integralności spoza obsługiwanych.
	ErrUnsupportedAlgorithm = errors.New("keyfile: nieobsługiwany algorytm")
	// ErrMalformed: plik niezgodny ze strukturą PKCS#12, PKCS#8 albo PEM.
	ErrMalformed = errors.New("keyfile: niepoprawny plik")
	// ErrNoKey: brak klucza prywatnego w pliku.
	ErrNoKey = errors.New("keyfile: brak klucza prywatnego")
	// ErrNoCertificate: brak certyfikatu odpowiadającego kluczowi.
	ErrNoCertificate = errors.New("keyfile: brak certyfikatu klucza")
	// ErrAmbiguous: plik zawiera więcej niż jeden klucz prywatny.
	ErrAmbiguous = errors.New("keyfile: więcej niż jeden klucz")
	// ErrEncryptedPEM: klucz PEM zaszyfrowany hasłem.
	ErrEncryptedPEM = errors.New("keyfile: zaszyfrowany klucz PEM nie jest obsługiwany " +
		"(użyj pliku PKCS#12: openssl pkcs12 -export -inkey klucz.pem -in certyfikat.pem -out podpis.p12)")
)

// Identity to klucz i certyfikaty do podpisu.
type Identity struct {
	// Signer to klucz prywatny (*rsa.PrivateKey, *ecdsa.PrivateKey albo ed25519.PrivateKey).
	Signer crypto.Signer
	// Certificate to certyfikat klucza.
	Certificate *x509.Certificate
	// Chain to pozostałe certyfikaty: najpierw ścieżka od wystawcy Certificate w górę (po nazwie
	// i podpisie), potem reszta w kolejności z pliku.
	Chain []*x509.Certificate
}

// ParsePEMKey odczytuje pierwszy klucz prywatny z danych PEM: PRIVATE KEY (PKCS#8), RSA PRIVATE KEY
// (PKCS#1) albo EC PRIVATE KEY (SEC1, RFC 5915). Bloki innych rodzajów (np. EC PARAMETERS,
// CERTIFICATE) są pomijane.
func ParsePEMKey(data []byte) (crypto.Signer, error) {
	for rest := data; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return nil, ErrNoKey
		}
		if block.Type == "ENCRYPTED PRIVATE KEY" || block.Headers["Proc-Type"] == "4,ENCRYPTED" {
			return nil, ErrEncryptedPEM
		}
		var key any
		var err error
		switch block.Type {
		case "PRIVATE KEY":
			key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		case "RSA PRIVATE KEY":
			key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		case "EC PRIVATE KEY":
			key, err = x509.ParseECPrivateKey(block.Bytes)
		default:
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w: klucz %s: %v", ErrMalformed, block.Type, err)
		}
		signer, ok := key.(crypto.Signer)
		if !ok {
			return nil, fmt.Errorf("%w: klucz %T", ErrUnsupportedAlgorithm, key)
		}
		return signer, nil
	}
}

// ParsePEMCertificates odczytuje wszystkie bloki CERTIFICATE z danych PEM w kolejności z pliku.
func ParsePEMCertificates(data []byte) ([]*x509.Certificate, error) {
	var out []*x509.Certificate
	for rest := data; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: certyfikat: %v", ErrMalformed, err)
		}
		out = append(out, cert)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: brak bloku CERTIFICATE", ErrNoCertificate)
	}
	return out, nil
}

// ParsePEM składa tożsamość z klucza PEM (ParsePEMKey), certyfikatów PEM klucza (pierwszy pasujący
// do klucza jest certyfikatem, reszta trafia do łańcucha) i opcjonalnego łańcucha PEM.
func ParsePEM(key, certs, chain []byte) (*Identity, error) {
	signer, err := ParsePEMKey(key)
	if err != nil {
		return nil, err
	}
	list, err := ParsePEMCertificates(certs)
	if err != nil {
		return nil, err
	}
	if len(chain) != 0 {
		extra, err := ParsePEMCertificates(chain)
		if err != nil {
			return nil, err
		}
		list = append(list, extra...)
	}
	return assemble(signer, nil, list)
}

// assemble wybiera certyfikat klucza (najpierw spośród preferred, potem z all, po kluczu publicznym)
// i porządkuje łańcuch.
func assemble(key any, preferred, all []*x509.Certificate) (*Identity, error) {
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("%w: klucz %T", ErrUnsupportedAlgorithm, key)
	}
	public, ok := signer.Public().(interface{ Equal(crypto.PublicKey) bool })
	if !ok {
		return nil, fmt.Errorf("%w: klucz %T", ErrUnsupportedAlgorithm, key)
	}
	var leaf *x509.Certificate
	for _, list := range [][]*x509.Certificate{preferred, all} {
		for _, cert := range list {
			if leaf == nil && public.Equal(cert.PublicKey) {
				leaf = cert
			}
		}
	}
	if leaf == nil {
		return nil, ErrNoCertificate
	}
	id := &Identity{Signer: signer, Certificate: leaf}
	used := map[*x509.Certificate]bool{leaf: true}
	for current := leaf; !bytes.Equal(current.RawIssuer, current.RawSubject); {
		var issuer *x509.Certificate
		for _, cert := range all {
			if !used[cert] && bytes.Equal(cert.RawSubject, current.RawIssuer) && current.CheckSignatureFrom(cert) == nil {
				issuer = cert
				break
			}
		}
		if issuer == nil {
			break
		}
		used[issuer] = true
		id.Chain = append(id.Chain, issuer)
		current = issuer
	}
	for _, cert := range all {
		if !used[cert] && !containsCert(id.Chain, cert) {
			used[cert] = true
			id.Chain = append(id.Chain, cert)
		}
	}
	return id, nil
}

func containsCert(list []*x509.Certificate, cert *x509.Certificate) bool {
	for _, c := range list {
		if c.Equal(cert) {
			return true
		}
	}
	return false
}
