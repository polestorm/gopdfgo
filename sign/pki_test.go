package sign

// Lokalna infrastruktura testowa: urząd certyfikacji, TSA (RFC 3161), responder OCSP (RFC 6960)
// i punkt CRL na httptest. Klucze wyłącznie testowe, generowane w teście; żadnych połączeń poza
// 127.0.0.1.

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Tryby pracy testowego TSA.
const (
	tsaOK         = ""
	tsaReject     = "reject"     // status rejection
	tsaBadImprint = "badImprint" // messageImprint innej wartości
	tsaBadNonce   = "badNonce"   // inny nonce
	tsaBadSig     = "badSig"     // zepsuty podpis tokenu
	tsaHang       = "hang"       // czeka na koniec żądania (ctx)
	tsaHuge       = "huge"       // token z dodatkowymi certyfikatami (duży)
)

type testPKI struct {
	root      testIdentity
	signers   map[string]testIdentity // "rsa", "p256": wystawione przez root, z AIA OCSP i CRL DP
	tsa       testIdentity            // extKeyUsage timeStamping (krytyczne), tylko CRL DP
	responder testIdentity            // upoważniony responder OCSP z id-pkix-ocsp-nocheck
	server    *httptest.Server

	mu         sync.Mutex
	tsaMode    string
	delegated  bool            // OCSP podpisuje responder zamiast urzędu
	revoked    map[string]bool // numery seryjne unieważnione
	requests   map[string]int  // liczba żądań: tsa, ocsp, crl
	serial     int64
	lastTSTime time.Time
}

func newTestPKI(t *testing.T) *testPKI {
	t.Helper()
	p := &testPKI{signers: map[string]testIdentity{}, revoked: map[string]bool{}, requests: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/tsa", p.serveTSA)
	mux.HandleFunc("/ocsp", p.serveOCSP)
	mux.HandleFunc("/crl", p.serveCRL)
	p.server = httptest.NewServer(mux)
	t.Cleanup(p.server.Close)

	rootKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p.root = p.issue(t, nil, rootKey, &x509.Certificate{
		Subject:               pkix.Name{CommonName: "gopdfgo test root CA", Organization: []string{"gopdfgo tests"}},
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	})
	for kind, gen := range map[string]func() (crypto.Signer, error){
		"rsa":  func() (crypto.Signer, error) { return rsa.GenerateKey(rand.Reader, 2048) },
		"p256": func() (crypto.Signer, error) { return ecdsa.GenerateKey(elliptic.P256(), rand.Reader) },
	} {
		key, err := gen()
		if err != nil {
			t.Fatal(err)
		}
		p.signers[kind] = p.issue(t, &p.root, key, &x509.Certificate{
			Subject:               pkix.Name{CommonName: "gopdfgo test signer " + kind, Organization: []string{"gopdfgo tests"}},
			KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment,
			OCSPServer:            []string{p.server.URL + "/ocsp"},
			CRLDistributionPoints: []string{p.server.URL + "/crl"},
		})
	}
	tsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ekuTimestamping, _ := asn1.Marshal([]asn1.ObjectIdentifier{{1, 3, 6, 1, 5, 5, 7, 3, 8}})
	p.tsa = p.issue(t, &p.root, tsaKey, &x509.Certificate{
		Subject:               pkix.Name{CommonName: "gopdfgo test TSA", Organization: []string{"gopdfgo tests"}},
		KeyUsage:              x509.KeyUsageDigitalSignature,
		CRLDistributionPoints: []string{p.server.URL + "/crl"},
		// RFC 3161 § 2.3: extKeyUsage timeStamping jako jedyne i krytyczne
		ExtraExtensions: []pkix.Extension{{Id: asn1.ObjectIdentifier{2, 5, 29, 37}, Critical: true, Value: ekuTimestamping}},
	})
	responderKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p.responder = p.issue(t, &p.root, responderKey, &x509.Certificate{
		Subject:         pkix.Name{CommonName: "gopdfgo test OCSP responder", Organization: []string{"gopdfgo tests"}},
		KeyUsage:        x509.KeyUsageDigitalSignature,
		ExtKeyUsage:     []x509.ExtKeyUsage{x509.ExtKeyUsageOCSPSigning},
		ExtraExtensions: []pkix.Extension{{Id: oidOCSPNoCheck, Value: []byte{0x05, 0x00}}},
	})
	return p
}

// issue wystawia certyfikat (parent == nil: samopodpisany).
func (p *testPKI) issue(t *testing.T, parent *testIdentity, key crypto.Signer, template *x509.Certificate) testIdentity {
	t.Helper()
	p.serial++
	template.SerialNumber = big.NewInt(1000 + p.serial)
	template.NotBefore = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	template.NotAfter = time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC)
	parentCert, parentKey := template, key
	if parent != nil {
		parentCert, parentKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parentCert, key.Public(), parentKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testIdentity{key: key, cert: cert}
}

func (p *testPKI) setTSAMode(mode string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tsaMode = mode
}

func (p *testPKI) revoke(cert *x509.Certificate) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.revoked[cert.SerialNumber.String()] = true
}

func (p *testPKI) count(kind string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests[kind]
}

func (p *testPKI) tsaClient() TSAClient {
	return TSAClient{URL: p.server.URL + "/tsa", HTTPClient: p.server.Client()}
}

func (p *testPKI) revocationClient() RevocationClient {
	return RevocationClient{HTTPClient: p.server.Client()}
}

// options zwraca opcje podpisu certyfikatem z urzędu testowego.
func (p *testPKI) options(kind string, level Level) Options {
	id := p.signers[kind]
	opts := signOptions(id)
	opts.Chain = []*x509.Certificate{p.root.cert}
	opts.Level = level
	if level >= LevelBT {
		opts.TSA = p.tsaClient()
	}
	if level >= LevelBLT {
		opts.Revocation = p.revocationClient()
	}
	return opts
}

func generalizedTime(t time.Time) []byte {
	b, err := asn1.MarshalWithParams(t.UTC(), "generalized")
	if err != nil {
		panic(err)
	}
	return b
}

func rsaSign(key crypto.Signer, data []byte) []byte {
	sum := sha256.Sum256(data)
	sig, err := key.Sign(rand.Reader, sum[:], crypto.SHA256)
	if err != nil {
		panic(err)
	}
	return sig
}

func (p *testPKI) serveTSA(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.requests["tsa"]++
	mode := p.tsaMode
	p.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/timestamp-query" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var req struct {
		Version        int
		MessageImprint struct {
			Algorithm pkix.AlgorithmIdentifier
			Hashed    []byte
		}
		Nonce   *big.Int `asn1:"optional"`
		CertReq bool     `asn1:"optional"`
	}
	if _, err := asn1.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/timestamp-reply")
	switch mode {
	case tsaHang:
		<-r.Context().Done()
		return
	case tsaReject:
		status := derSequence(derInteger(big.NewInt(2)), derSequence(derTLV(0x0c, []byte("test rejection"))))
		w.Write(derSequence(status))
		return
	}
	imprint := req.MessageImprint.Hashed
	if mode == tsaBadImprint {
		imprint = append([]byte(nil), imprint...)
		imprint[0] ^= 0xff
	}
	nonce := req.Nonce
	if mode == tsaBadNonce {
		nonce = new(big.Int).Add(nonce, big.NewInt(1))
	}
	genTime := signingTime.Add(5 * time.Second)
	p.mu.Lock()
	p.lastTSTime = genTime
	p.mu.Unlock()
	tstInfo := derSequence(
		derInteger(big.NewInt(1)),
		derOID(asn1.ObjectIdentifier{1, 2, 3, 4, 1}),
		derSequence(sha256Identifier, derOctets(imprint)),
		derInteger(big.NewInt(time.Now().UnixNano())),
		generalizedTime(genTime),
		derInteger(nonce),
	)
	digest := sha256.Sum256(tstInfo)
	attrs := signedAttributesFor(oidTSTInfo, digest[:], p.tsa.cert)
	sig := rsaSign(p.tsa.key, attrs)
	if mode == tsaBadSig {
		sig[10] ^= 0xff
	}
	signerInfo := derSequence(
		derInteger(big.NewInt(1)),
		derSequence(p.tsa.cert.RawIssuer, derInteger(p.tsa.cert.SerialNumber)),
		sha256Identifier,
		append([]byte{0xa0}, attrs[1:]...),
		derSequence(derOID(oidSHA256WithRSA), derNull),
		derOctets(sig),
	)
	certs := [][]byte{p.tsa.cert.Raw}
	if mode == tsaHuge {
		for range 8 {
			certs = append(certs, p.root.cert.Raw)
		}
	}
	signedData := derSequence(
		derInteger(big.NewInt(3)),
		derSet(sha256Identifier),
		derSequence(derOID(oidTSTInfo), derTLV(0xa0, derOctets(tstInfo))),
		derTLV(0xa0, certs...),
		derSet(signerInfo),
	)
	token := derSequence(derOID(oidSignedData), derTLV(0xa0, signedData))
	w.Write(derSequence(derSequence(derInteger(big.NewInt(0))), token))
}

func (p *testPKI) serveOCSP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.requests["ocsp"]++
	p.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	der, err := p.ocspResponse(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/ocsp-response")
	w.Write(der)
}

// ocspResponse buduje OCSPResponse dla żądania z jednym CertID.
func (p *testPKI) ocspResponse(request []byte) ([]byte, error) {
	var req struct {
		TBS struct {
			RequestList []struct {
				CertID asn1.RawValue
			}
		}
	}
	if _, err := asn1.Unmarshal(request, &req); err != nil {
		return nil, err
	}
	var id certIDASN
	if _, err := asn1.Unmarshal(req.TBS.RequestList[0].CertID.FullBytes, &id); err != nil {
		return nil, err
	}
	p.mu.Lock()
	revoked := p.revoked[id.SerialNumber.String()]
	delegated := p.delegated
	p.mu.Unlock()
	status := []byte{0x80, 0x00} // good [0] IMPLICIT NULL
	if revoked {
		status = derTLV(0xa1, generalizedTime(signingTime.Add(-time.Hour)))
	}
	signer := p.root
	if delegated {
		signer = p.responder
	}
	keyHash := sha1.Sum(publicKeyBits(signer.cert))
	single := derSequence(req.TBS.RequestList[0].CertID.FullBytes, status,
		generalizedTime(signingTime.Add(time.Minute)), derTLV(0xa0, generalizedTime(signingTime.Add(24*time.Hour))))
	tbs := derSequence(derTLV(0xa2, derOctets(keyHash[:])), generalizedTime(signingTime.Add(time.Minute)), derSequence(single))
	sum := sha256.Sum256(tbs)
	sig, err := signer.key.Sign(rand.Reader, sum[:], crypto.SHA256)
	if err != nil {
		return nil, err
	}
	sigAlg := derSequence(derOID(oidSHA256WithRSA), derNull)
	if _, ok := signer.key.(*ecdsa.PrivateKey); ok {
		sigAlg = derSequence(derOID(oidECDSAWithSHA256))
	}
	parts := [][]byte{tbs, sigAlg, derTLV(0x03, append([]byte{0}, sig...))}
	if delegated {
		parts = append(parts, derTLV(0xa0, derSequence(signer.cert.Raw)))
	}
	basic := derSequence(parts...)
	return derSequence([]byte{0x0a, 0x01, 0x00}, derTLV(0xa0, derSequence(derOID(oidOCSPBasic), derOctets(basic)))), nil
}

func (p *testPKI) serveCRL(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.requests["crl"]++
	var entries []x509.RevocationListEntry
	for serial := range p.revoked {
		n, _ := new(big.Int).SetString(serial, 10)
		entries = append(entries, x509.RevocationListEntry{SerialNumber: n, RevocationTime: signingTime.Add(-time.Hour)})
	}
	p.mu.Unlock()
	der, err := p.crl(entries)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/pkix-crl")
	w.Write(der)
}

func (p *testPKI) crl(entries []x509.RevocationListEntry) ([]byte, error) {
	return x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		Number:                    big.NewInt(1),
		ThisUpdate:                signingTime.Add(time.Minute),
		NextUpdate:                signingTime.Add(24 * time.Hour),
		RevokedCertificateEntries: entries,
	}, p.root.cert, p.root.key)
}
