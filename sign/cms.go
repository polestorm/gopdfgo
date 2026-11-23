package sign

// Podpis CMS SignedData w formie odłączonej (RFC 5652) z atrybutami podpisanymi PAdES B-B
// (ETSI EN 319 142-1 § 5.3 i 6.3, ETSI EN 319 122-1 § 5.2): content-type, message-digest,
// signing-certificate-v2 (RFC 5035). Bez signing-time: PAdES podaje czas podpisu w /M słownika
// podpisu. Zapis DER własnymi funkcjami na encoding/asn1 (biblioteka standardowa Go nie ma CMS).

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
)

// Identyfikatory obiektów.
var (
	oidData                 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}         // RFC 5652 § 4
	oidSignedData           = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}         // RFC 5652 § 5.1
	oidContentType          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}         // RFC 5652 § 11.1
	oidMessageDigest        = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}         // RFC 5652 § 11.2
	oidSigningCertificateV2 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47} // RFC 5035 § 3
	oidSHA256               = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}     // RFC 5754 § 2.2
	oidSHA256WithRSA        = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}        // RFC 5754 § 3.2
	oidECDSAWithSHA256      = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}          // RFC 5754 § 3.3
)

// Kodowanie DER (X.690 § 8, 10).

func derTLV(tag byte, content ...[]byte) []byte {
	n := 0
	for _, c := range content {
		n += len(c)
	}
	out := []byte{tag}
	switch {
	case n < 0x80:
		out = append(out, byte(n))
	default:
		var length []byte
		for v := n; v > 0; v >>= 8 {
			length = append([]byte{byte(v)}, length...)
		}
		out = append(out, 0x80|byte(len(length)))
		out = append(out, length...)
	}
	for _, c := range content {
		out = append(out, c...)
	}
	return out
}

func derSequence(content ...[]byte) []byte { return derTLV(0x30, content...) }

// derSet koduje SET OF z elementami w porządku DER (X.690 § 11.6): rosnąco jako ciągi bajtów,
// krótszy dopełniony zerami na końcu.
func derSet(elements ...[]byte) []byte {
	sorted := append([][]byte(nil), elements...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && derLess(sorted[j], sorted[j-1]); j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return derTLV(0x31, sorted...)
}

func derLess(a, b []byte) bool {
	n := max(len(a), len(b))
	for i := range n {
		var x, y byte
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func derOID(oid asn1.ObjectIdentifier) []byte {
	b, err := asn1.Marshal(oid)
	if err != nil {
		panic(err)
	}
	return b
}

func derInteger(v *big.Int) []byte {
	b, err := asn1.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func derOctets(b []byte) []byte { return derTLV(0x04, b) }

var derNull = []byte{0x05, 0x00}

// algorithm opisuje parę skrót–podpis wybraną według klucza publicznego certyfikatu.
type algorithm struct {
	signatureOID asn1.ObjectIdentifier
	withNull     bool // parametry NULL w AlgorithmIdentifier (RSA, RFC 4055 § 5)
	maxSignature int  // górne ograniczenie długości podpisu w bajtach
}

func algorithmFor(pub crypto.PublicKey) (algorithm, error) {
	switch key := pub.(type) {
	case *rsa.PublicKey:
		return algorithm{signatureOID: oidSHA256WithRSA, withNull: true, maxSignature: key.Size()}, nil
	case *ecdsa.PublicKey:
		size := (key.Curve.Params().BitSize + 7) / 8
		// Ecdsa-Sig-Value ::= SEQUENCE { r INTEGER, s INTEGER }: dwa całkowite z bajtem znaku
		return algorithm{signatureOID: oidECDSAWithSHA256, maxSignature: 2*(size+3) + 3}, nil
	}
	return algorithm{}, fmt.Errorf("%w: %T", ErrUnsupportedKey, pub)
}

func (a algorithm) identifier() []byte {
	if a.withNull {
		return derSequence(derOID(a.signatureOID), derNull)
	}
	return derSequence(derOID(a.signatureOID))
}

// sha256Identifier to AlgorithmIdentifier SHA-256 bez parametrów (RFC 5754 § 2).
var sha256Identifier = derSequence(derOID(oidSHA256))

// signedAttributes koduje atrybuty podpisane jako SET OF Attribute (RFC 5652 § 5.3). Ten zapis,
// ze znacznikiem SET, jest podpisywany; w SignerInfo stoi ten sam z niejawnym znacznikiem [0].
func signedAttributes(digest []byte, cert *x509.Certificate) []byte {
	attribute := func(oid asn1.ObjectIdentifier, value []byte) []byte {
		return derSequence(derOID(oid), derSet(value))
	}
	certHash := sha256.Sum256(cert.Raw)
	// ESSCertIDv2 (RFC 5035 § 4): hashAlgorithm ma wartość domyślną SHA-256, więc w DER go nie ma;
	// issuerSerial z GeneralNames [4] directoryName (ETSI EN 319 122-1 § 5.2.2.3 zaleca jego obecność).
	issuerSerial := derSequence(derSequence(derTLV(0xa4, cert.RawIssuer)), derInteger(cert.SerialNumber))
	essCertID := derSequence(derOctets(certHash[:]), issuerSerial)
	signingCertificate := derSequence(derSequence(essCertID))
	return derSet(
		attribute(oidContentType, derOID(oidData)),
		attribute(oidMessageDigest, derOctets(digest)),
		attribute(oidSigningCertificateV2, signingCertificate),
	)
}

// buildSignedData składa ContentInfo z SignedData (RFC 5652 § 5.1). certificates zachowuje kolejność
// podaną przez wywołującego (najpierw podpisujący): DER obowiązuje tylko atrybuty podpisane (§ 5.4).
func buildSignedData(signedAttrs, signature []byte, alg algorithm, cert *x509.Certificate, chain []*x509.Certificate) []byte {
	implicitAttrs := append([]byte{0xa0}, signedAttrs[1:]...)
	signerInfo := derSequence(
		derInteger(big.NewInt(1)),
		derSequence(cert.RawIssuer, derInteger(cert.SerialNumber)),
		sha256Identifier,
		implicitAttrs,
		alg.identifier(),
		derOctets(signature),
	)
	certificates := [][]byte{cert.Raw}
	for _, c := range chain {
		if !bytes.Equal(c.Raw, cert.Raw) {
			certificates = append(certificates, c.Raw)
		}
	}
	signedData := derSequence(
		derInteger(big.NewInt(1)),
		derSet(sha256Identifier),
		derSequence(derOID(oidData)),
		derTLV(0xa0, certificates...),
		derSet(signerInfo),
	)
	return derSequence(derOID(oidSignedData), derTLV(0xa0, signedData))
}

// signCMS podpisuje skrót SHA-256 zakresu /ByteRange i zwraca podpis CMS w DER.
func signCMS(digest []byte, signer crypto.Signer, alg algorithm, cert *x509.Certificate, chain []*x509.Certificate) ([]byte, error) {
	attrs := signedAttributes(digest, cert)
	attrsDigest := sha256.Sum256(attrs)
	signature, err := signer.Sign(rand.Reader, attrsDigest[:], crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("sign: podpisujący: %w", err)
	}
	// sprawdzenie podpisu kluczem z certyfikatu wyłapuje niezgodny klucz albo zły schemat (np. PSS)
	switch key := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		if rsa.VerifyPKCS1v15(key, crypto.SHA256, attrsDigest[:], signature) != nil {
			return nil, ErrKeyMismatch
		}
	case *ecdsa.PublicKey:
		if !ecdsa.VerifyASN1(key, attrsDigest[:], signature) {
			return nil, ErrKeyMismatch
		}
	}
	if len(signature) > alg.maxSignature {
		return nil, errors.New("sign: podpis dłuższy, niż pozwala klucz")
	}
	return buildSignedData(attrs, signature, alg, cert, chain), nil
}

// cmsSizeLimit to górne ograniczenie długości podpisu CMS dla danego certyfikatu i łańcucha:
// ta sama struktura z podpisem najdłuższym możliwym dla klucza.
func cmsSizeLimit(alg algorithm, cert *x509.Certificate, chain []*x509.Certificate) int {
	attrs := signedAttributes(make([]byte, sha256.Size), cert)
	return len(buildSignedData(attrs, make([]byte, alg.maxSignature), alg, cert, chain))
}
