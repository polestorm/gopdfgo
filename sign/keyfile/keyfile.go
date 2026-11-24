// Package keyfile reads a private key and certificates for signing from files: PKCS#12 (RFC 7292)
// and PEM (a PKCS#8, PKCS#1 or SEC1 key and X.509 certificates).
//
// The package is optional (ADR 0049, 0050 item 3, ADR 0051): package sign does not read keys from files,
// and the gopdfgo-sign command reads them through this package from a file named explicitly. The result
// (Identity) is passed to sign.Options: Signer, Certificate, Chain. Own code on the Go standard library,
// with no dependencies.
//
//	data, _ := os.ReadFile("signer.p12")
//	id, err := keyfile.ParsePKCS12(data, password)
//	if err != nil {
//		return err
//	}
//	signed, err := sign.SignPDF(ctx, pdf, sign.Options{Signer: id.Signer, Certificate: id.Certificate, Chain: id.Chain})
//
// Encrypted PEM files (ENCRYPTED PRIVATE KEY, the header Proc-Type: 4,ENCRYPTED) are not supported:
// they give ErrEncryptedPEM with a hint; a password-protected key is kept in a PKCS#12 file.
package keyfile

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

// Errors of the package.
var (
	// ErrPassword: wrong password (the MAC or the padding after decryption does not match).
	ErrPassword = errors.New("keyfile: wrong password or damaged file")
	// ErrUnsupportedAlgorithm: an encryption or integrity algorithm outside the supported ones.
	ErrUnsupportedAlgorithm = errors.New("keyfile: unsupported algorithm")
	// ErrMalformed: the file does not follow the structure of PKCS#12, PKCS#8 or PEM.
	ErrMalformed = errors.New("keyfile: malformed file")
	// ErrNoKey: no private key in the file.
	ErrNoKey = errors.New("keyfile: no private key")
	// ErrNoCertificate: no certificate that matches the key.
	ErrNoCertificate = errors.New("keyfile: no certificate for the key")
	// ErrAmbiguous: the file contains more than one private key.
	ErrAmbiguous = errors.New("keyfile: more than one key")
	// ErrEncryptedPEM: a PEM key encrypted with a password.
	ErrEncryptedPEM = errors.New("keyfile: encrypted PEM keys are not supported " +
		"(use a PKCS#12 file: openssl pkcs12 -export -inkey key.pem -in certificate.pem -out signer.p12)")
)

// Identity is a key and the certificates for signing.
type Identity struct {
	// Signer is the private key (*rsa.PrivateKey, *ecdsa.PrivateKey or ed25519.PrivateKey).
	Signer crypto.Signer
	// Certificate is the certificate of the key.
	Certificate *x509.Certificate
	// Chain is the remaining certificates: first the path from the issuer of Certificate upwards (by
	// name and signature), then the rest in file order.
	Chain []*x509.Certificate
}

// ParsePEMKey reads the first private key from PEM data: PRIVATE KEY (PKCS#8), RSA PRIVATE KEY
// (PKCS#1) or EC PRIVATE KEY (SEC1, RFC 5915). Blocks of other kinds (for example EC PARAMETERS,
// CERTIFICATE) are skipped.
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
			return nil, fmt.Errorf("%w: key %s: %v", ErrMalformed, block.Type, err)
		}
		signer, ok := key.(crypto.Signer)
		if !ok {
			return nil, fmt.Errorf("%w: key %T", ErrUnsupportedAlgorithm, key)
		}
		return signer, nil
	}
}

// ParsePEMCertificates reads all CERTIFICATE blocks from PEM data in file order.
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
			return nil, fmt.Errorf("%w: certificate: %v", ErrMalformed, err)
		}
		out = append(out, cert)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no CERTIFICATE block", ErrNoCertificate)
	}
	return out, nil
}

// ParsePEM assembles an identity from a PEM key (ParsePEMKey), the PEM certificates of the key (the first
// one matching the key is the certificate, the rest go to the chain) and an optional PEM chain.
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
		return nil, fmt.Errorf("%w: key %T", ErrUnsupportedAlgorithm, key)
	}
	public, ok := signer.Public().(interface{ Equal(crypto.PublicKey) bool })
	if !ok {
		return nil, fmt.Errorf("%w: key %T", ErrUnsupportedAlgorithm, key)
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
