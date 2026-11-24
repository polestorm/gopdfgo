package sign

// Dane o ważności certyfikatów dla PAdES B-LT (ETSI EN 319 142-1 § 5.4, 6.3): dla każdego certyfikatu
// na ścieżce od podpisującego i od TSA do certyfikatu samopodpisanego odpowiedź OCSP (RFC 6960)
// albo lista CRL (RFC 5280 § 5) wystawcy. Dane podane w Options.ValidationData mają pierwszeństwo;
// brakujące pobiera jawnie podany RevocationFetcher (OCSP spod adresów z rozszerzenia
// authorityInfoAccess, CRL spod cRLDistributionPoints). Każda odpowiedź jest sprawdzana: dotyczy
// certyfikatu, jest podpisana przez wystawcę albo upoważniony responder (RFC 6960 § 4.2.2.2),
// a status certyfikatu nie jest „revoked”. Świeżości odpowiedzi i zaufania do korzenia pakiet nie
// ocenia: to zadanie walidatora podpisu.

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// ValidationData is validation data given directly (e.g. fetched earlier or offline).
type ValidationData struct {
	// Certificates are additional certificates: the issuers (up to the root) of the TSA certificates, the root
	// of the signer's chain, OCSP responder certificates. They go into /Certs if they lie on the path.
	Certificates []*x509.Certificate
	// OCSPResponses are OCSPResponse messages in DER (RFC 6960 § 4.2.1).
	OCSPResponses [][]byte
	// CRLs are CertificateList structures in DER (RFC 5280 § 5.1) signed by the certificate's issuer.
	CRLs [][]byte
}

// RevocationFetcher fetches validation data at the transport level; SignPDF checks it.
// RevocationClient is the implementation over HTTP.
type RevocationFetcher interface {
	// FetchOCSP sends an OCSPRequest (DER) to the responder's address and returns the OCSPResponse (DER).
	FetchOCSP(ctx context.Context, url string, request []byte) ([]byte, error)
	// FetchCRL fetches a CRL (DER) from the address url.
	FetchCRL(ctx context.Context, url string) ([]byte, error)
}

// RevocationClient fetches OCSP responses with POST (RFC 6960 appendix A.1) and CRLs with GET
// through HTTPClient (required). The addresses come from the certificates; only http and https are supported.
type RevocationClient struct {
	HTTPClient *http.Client
}

// FetchOCSP implements RevocationFetcher.
func (c RevocationClient) FetchOCSP(ctx context.Context, url string, request []byte) ([]byte, error) {
	return postDER(ctx, c.HTTPClient, url, "application/ocsp-request", request)
}

// FetchCRL implements RevocationFetcher.
func (c RevocationClient) FetchCRL(ctx context.Context, url string) ([]byte, error) {
	return getDER(ctx, c.HTTPClient, url)
}

// validationSet to dane słownika DSS w kolejności dodania, bez powtórzeń.
type validationSet struct {
	certs, ocsps, crls [][]byte
}

func appendUnique(list [][]byte, der []byte) [][]byte {
	for _, item := range list {
		if bytes.Equal(item, der) {
			return list
		}
	}
	return append(list, der)
}

// collector zbiera dane o ważności dla ścieżek certyfikatów.
type collector struct {
	ctx     context.Context
	pool    []*x509.Certificate
	given   ValidationData
	fetcher RevocationFetcher
	done    map[string]bool
	out     validationSet
}

// collectValidation zbiera dane o ważności dla certyfikatu podpisującego i certyfikatu TSA wraz
// z ich wystawcami.
func collectValidation(ctx context.Context, opts Options, ts *timestampResult) (*validationSet, error) {
	c := &collector{ctx: ctx, given: opts.ValidationData, fetcher: opts.Revocation, done: map[string]bool{}}
	c.pool = append(c.pool, opts.Certificate)
	c.pool = append(c.pool, opts.Chain...)
	targets := []*x509.Certificate{opts.Certificate}
	if ts != nil {
		c.pool = append(c.pool, ts.certificates...)
		targets = append(targets, ts.signer)
	}
	c.pool = append(c.pool, opts.ValidationData.Certificates...)
	for _, cert := range targets {
		if err := c.cover(cert); err != nil {
			return nil, err
		}
	}
	return &c.out, nil
}

// cover dodaje certyfikat i dane o jego ważności, potem przechodzi do wystawcy.
func (c *collector) cover(cert *x509.Certificate) error {
	for depth := 0; cert != nil && depth < 16; depth++ {
		if c.done[string(cert.Raw)] {
			return nil
		}
		c.done[string(cert.Raw)] = true
		c.out.certs = appendUnique(c.out.certs, cert.Raw)
		if selfSigned(cert) {
			return nil
		}
		issuer := c.issuerOf(cert)
		if issuer == nil {
			return fmt.Errorf("%w: %q (add it to Options.Chain or Options.ValidationData.Certificates)", ErrMissingIssuer, cert.Issuer)
		}
		if err := c.revocation(cert, issuer); err != nil {
			return err
		}
		cert = issuer
	}
	return nil
}

func selfSigned(cert *x509.Certificate) bool {
	return bytes.Equal(cert.RawIssuer, cert.RawSubject) &&
		cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) == nil
}

func (c *collector) issuerOf(cert *x509.Certificate) *x509.Certificate {
	for _, candidate := range c.pool {
		if bytes.Equal(candidate.RawSubject, cert.RawIssuer) && cert.CheckSignatureFrom(candidate) == nil {
			return candidate
		}
	}
	return nil
}

// revocation znajduje dane o ważności certyfikatu: najpierw podane, potem pobrane.
func (c *collector) revocation(cert, issuer *x509.Certificate) error {
	for _, der := range c.given.OCSPResponses {
		ok, err := c.useOCSP(der, cert, issuer)
		if ok || err != nil {
			return err
		}
	}
	for _, der := range c.given.CRLs {
		ok, err := c.useCRL(der, cert, issuer)
		if ok || err != nil {
			return err
		}
	}
	var failures []string
	if c.fetcher != nil {
		for _, url := range httpURLs(cert.OCSPServer) {
			der, err := c.fetcher.FetchOCSP(c.ctx, url, ocspRequest(cert, issuer))
			if err == nil {
				var ok bool
				if ok, err = c.useOCSP(der, cert, issuer); ok {
					return nil
				}
				if err == nil {
					err = errors.New("response is not for this certificate")
				}
			}
			if errors.Is(err, ErrRevoked) || c.ctx.Err() != nil {
				return errors.Join(err, c.ctx.Err())
			}
			failures = append(failures, "OCSP "+url+": "+err.Error())
		}
		for _, url := range httpURLs(cert.CRLDistributionPoints) {
			der, err := c.fetcher.FetchCRL(c.ctx, url)
			if err == nil {
				var ok bool
				if ok, err = c.useCRL(der, cert, issuer); ok {
					return nil
				}
				if err == nil {
					err = errors.New("the list is not from the certificate's issuer")
				}
			}
			if errors.Is(err, ErrRevoked) || c.ctx.Err() != nil {
				return errors.Join(err, c.ctx.Err())
			}
			failures = append(failures, "CRL "+url+": "+err.Error())
		}
	}
	detail := ""
	if len(failures) > 0 {
		detail = " (" + strings.Join(failures, "; ") + ")"
	} else if c.fetcher != nil {
		detail = " (the certificate gives no OCSP or CRL address over http)"
	}
	return fmt.Errorf("%w for certificate %q%s", ErrNoValidationData, cert.Subject, detail)
}

func httpURLs(urls []string) []string {
	var out []string
	for _, u := range urls {
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			out = append(out, u)
		}
	}
	return out
}

// useOCSP sprawdza odpowiedź; ok, gdy dotyczy certyfikatu i podaje status good. Odpowiedź
// dla tego certyfikatu z błędnym podpisem albo statusem revoked daje błąd.
func (c *collector) useOCSP(der []byte, cert, issuer *x509.Certificate) (bool, error) {
	status, responder, err := checkOCSP(der, cert, issuer)
	if errors.Is(err, errNotForCert) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("sign: OCSP for %q: %w", cert.Subject, err)
	}
	switch status {
	case ocspRevoked:
		return false, fmt.Errorf("%w: %q (OCSP)", ErrRevoked, cert.Subject)
	case ocspUnknown:
		return false, nil
	}
	c.out.ocsps = appendUnique(c.out.ocsps, der)
	if responder != nil {
		// responder upoważniony: jego certyfikat do /Certs, a bez id-pkix-ocsp-nocheck także dane
		// o jego ważności (RFC 6960 § 4.2.2.2.1)
		c.pool = append(c.pool, responder)
		if hasExtension(responder, oidOCSPNoCheck) {
			c.out.certs = appendUnique(c.out.certs, responder.Raw)
		} else if err := c.cover(responder); err != nil {
			return false, err
		}
	}
	return true, nil
}

// useCRL sprawdza listę; ok, gdy pochodzi od wystawcy certyfikatu i go nie wymienia.
func (c *collector) useCRL(der []byte, cert, issuer *x509.Certificate) (bool, error) {
	crl, err := x509.ParseRevocationList(der)
	if err != nil {
		return false, fmt.Errorf("sign: CRL: %w", err)
	}
	if !bytes.Equal(crl.RawIssuer, issuer.RawSubject) {
		return false, nil
	}
	if err := crl.CheckSignatureFrom(issuer); err != nil {
		return false, fmt.Errorf("sign: CRL of issuer %q: %w", issuer.Subject, err)
	}
	for _, entry := range crl.RevokedCertificateEntries {
		if entry.SerialNumber.Cmp(cert.SerialNumber) == 0 {
			return false, fmt.Errorf("%w: %q (CRL)", ErrRevoked, cert.Subject)
		}
	}
	c.out.crls = appendUnique(c.out.crls, der)
	return true, nil
}

func hasExtension(cert *x509.Certificate, oid asn1.ObjectIdentifier) bool {
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(oid) {
			return true
		}
	}
	return false
}

// ocspRequest koduje OCSPRequest (RFC 6960 § 4.1.1) z jednym CertID ze skrótem SHA-1: identyfikator,
// nie zabezpieczenie, a SHA-1 obsługuje każdy responder (RFC 5019 § 2.1.1). Bez nonce i podpisu.
func ocspRequest(cert, issuer *x509.Certificate) []byte {
	nameHash := sha1.Sum(issuer.RawSubject)
	keyHash := sha1.Sum(publicKeyBits(issuer))
	certID := derSequence(
		derSequence(derOID(oidSHA1), derNull),
		derOctets(nameHash[:]),
		derOctets(keyHash[:]),
		derInteger(cert.SerialNumber),
	)
	return derSequence(derSequence(derSequence(derSequence(certID))))
}

// publicKeyBits zwraca wartość pola subjectPublicKey (BIT STRING bez bajtu nieużytych bitów).
func publicKeyBits(cert *x509.Certificate) []byte {
	var spki struct {
		Algorithm pkix.AlgorithmIdentifier
		PublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(cert.RawSubjectPublicKeyInfo, &spki); err != nil {
		return nil
	}
	return spki.PublicKey.Bytes
}

// Struktury odpowiedzi OCSP (RFC 6960 § 4.2.1).
type (
	ocspResponseASN struct {
		Status asn1.Enumerated
		Bytes  ocspResponseBytesASN `asn1:"explicit,tag:0,optional"`
	}
	ocspResponseBytesASN struct {
		Type     asn1.ObjectIdentifier
		Response []byte
	}
	basicOCSPResponseASN struct {
		TBS                asn1.RawValue
		SignatureAlgorithm pkix.AlgorithmIdentifier
		Signature          asn1.BitString
		Certs              []asn1.RawValue `asn1:"explicit,tag:0,optional"`
	}
	responseDataASN struct {
		Version     int `asn1:"optional,explicit,tag:0,default:0"`
		ResponderID asn1.RawValue
		ProducedAt  time.Time `asn1:"generalized"`
		Responses   []singleResponseASN
		Extensions  []pkix.Extension `asn1:"explicit,tag:1,optional"`
	}
	singleResponseASN struct {
		CertID     certIDASN
		Status     asn1.RawValue
		ThisUpdate time.Time        `asn1:"generalized"`
		NextUpdate time.Time        `asn1:"generalized,explicit,tag:0,optional"`
		Extensions []pkix.Extension `asn1:"explicit,tag:1,optional"`
	}
	certIDASN struct {
		HashAlgorithm  pkix.AlgorithmIdentifier
		IssuerNameHash []byte
		IssuerKeyHash  []byte
		SerialNumber   *big.Int
	}
)

type ocspStatus int

const (
	ocspGood ocspStatus = iota
	ocspRevoked
	ocspUnknown
)

var errNotForCert = errors.New("response is not for this certificate")

// checkOCSP sprawdza odpowiedź OCSP dla certyfikatu: status odpowiedzi successful, typ basic,
// CertID certyfikatu, podpis wystawcy albo respondera upoważnionego przez wystawcę (extKeyUsage
// OCSPSigning). Zwraca status certyfikatu i certyfikat respondera upoważnionego (nil, gdy podpisał
// wystawca).
func checkOCSP(der []byte, cert, issuer *x509.Certificate) (ocspStatus, *x509.Certificate, error) {
	var resp ocspResponseASN
	if rest, err := asn1.Unmarshal(der, &resp); err != nil || len(rest) > 0 {
		return 0, nil, errors.New("response is not a DER OCSPResponse")
	}
	if resp.Status != 0 {
		return 0, nil, fmt.Errorf("status odpowiedzi %d (RFC 6960 § 4.2.1)", resp.Status)
	}
	if !resp.Bytes.Type.Equal(oidOCSPBasic) {
		return 0, nil, fmt.Errorf("typ odpowiedzi %v zamiast id-pkix-ocsp-basic", resp.Bytes.Type)
	}
	var basic basicOCSPResponseASN
	if _, err := asn1.Unmarshal(resp.Bytes.Response, &basic); err != nil {
		return 0, nil, fmt.Errorf("BasicOCSPResponse: %v", err)
	}
	var data responseDataASN
	if _, err := asn1.Unmarshal(basic.TBS.FullBytes, &data); err != nil {
		return 0, nil, fmt.Errorf("ResponseData: %v", err)
	}
	var single *singleResponseASN
	for i := range data.Responses {
		if certIDMatches(data.Responses[i].CertID, cert, issuer) {
			single = &data.Responses[i]
			break
		}
	}
	if single == nil {
		return 0, nil, errNotForCert
	}
	signature := basic.Signature.RightAlign()
	verify := func(signer *x509.Certificate) bool {
		alg := x509Algorithm(basic.SignatureAlgorithm.Algorithm, nil, signer.PublicKey)
		return alg != x509.UnknownSignatureAlgorithm && signer.CheckSignature(alg, basic.TBS.FullBytes, signature) == nil
	}
	var responder *x509.Certificate
	if !verify(issuer) {
		for _, raw := range basic.Certs {
			candidate, err := x509.ParseCertificate(raw.FullBytes)
			if err != nil || !hasExtKeyUsage(candidate, x509.ExtKeyUsageOCSPSigning) ||
				candidate.CheckSignatureFrom(issuer) != nil || !verify(candidate) {
				continue
			}
			responder = candidate
			break
		}
		if responder == nil {
			return 0, nil, errors.New("the response is signed neither by the issuer nor by an authorized responder")
		}
	}
	// CertStatus (RFC 6960 § 4.2.1): good [0], revoked [1], unknown [2]
	if single.Status.Class != asn1.ClassContextSpecific {
		return 0, nil, errors.New("invalid CertStatus")
	}
	switch single.Status.Tag {
	case 0:
		return ocspGood, responder, nil
	case 1:
		return ocspRevoked, responder, nil
	}
	return ocspUnknown, responder, nil
}

// certIDMatches porównuje CertID odpowiedzi z certyfikatem (skrót dowolny z SHA-1, SHA-2).
func certIDMatches(id certIDASN, cert, issuer *x509.Certificate) bool {
	if id.SerialNumber == nil || id.SerialNumber.Cmp(cert.SerialNumber) != 0 {
		return false
	}
	hash, ok := hashFor(id.HashAlgorithm.Algorithm)
	if !ok || !hash.Available() {
		return false
	}
	name := hash.New()
	name.Write(issuer.RawSubject)
	key := hash.New()
	key.Write(publicKeyBits(issuer))
	return bytes.Equal(id.IssuerNameHash, name.Sum(nil)) && bytes.Equal(id.IssuerKeyHash, key.Sum(nil))
}
