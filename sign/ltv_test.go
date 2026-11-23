package sign

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTimestampLevel: B-T na plikach z gopdfgo, RSA i ECDSA; token sprawdzany w Go, przez
// openssl ts -verify i pdfsig.
func TestTimestampLevel(t *testing.T) {
	pki := newTestPKI(t)
	docs := sourceDocuments(t)
	for name, pdf := range docs {
		for _, kind := range []string{"rsa", "p256"} {
			t.Run(name+"-"+kind, func(t *testing.T) {
				signed, err := SignPDF(t.Context(), pdf, pki.options(kind, LevelBT))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.HasPrefix(signed, pdf) {
					t.Fatal("podpis zmienił bajty pliku wejściowego")
				}
				sigs := checkSignatures(t, signed, 1)
				if sigs[0].token == nil {
					t.Fatal("brak signature-time-stamp-token")
				}
				checkWithTools(t, signed, sigs, pki.root.cert, true)
				saveForInspection(t, "bt-"+name+"-"+kind, signed)
			})
		}
	}
	if n := pki.count("ocsp") + pki.count("crl"); n != 0 {
		t.Errorf("B-T pobrał dane o ważności (%d żądań)", n)
	}
}

// dssContents odczytuje słownik DSS: zawartość strumieni /Certs, /OCSPs, /CRLs i klucze /VRI.
func dssContents(t *testing.T, pdf []byte) (certs, ocsps, crls [][]byte, vri map[string]*pdfDict) {
	t.Helper()
	f, err := readPDF(pdf)
	if err != nil {
		t.Fatal(err)
	}
	rootObj, _ := f.resolve(f.trailer.get("Root"))
	dssObj, _ := f.resolve(rootObj.(*pdfDict).get("DSS"))
	dss, ok := dssObj.(*pdfDict)
	if !ok {
		t.Fatal("katalog bez /DSS")
	}
	read := func(key pdfName) [][]byte {
		arrObj, _ := f.resolve(dss.get(key))
		arr, _ := arrObj.(pdfArray)
		var out [][]byte
		for _, ref := range arr {
			obj, _ := f.resolve(ref)
			stream := obj.(*pdfStream)
			data, err := f.decodeStream(stream)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, data)
		}
		return out
	}
	certs, ocsps, crls = read("Certs"), read("OCSPs"), read("CRLs")
	vriObj, _ := f.resolve(dss.get("VRI"))
	vri = map[string]*pdfDict{}
	for _, key := range vriObj.(*pdfDict).keys {
		entry, _ := f.resolve(vriObj.(*pdfDict).get(key))
		vri[string(key)] = entry.(*pdfDict)
	}
	return certs, ocsps, crls, vri
}

func containsDER(list [][]byte, der []byte) bool {
	for _, item := range list {
		if bytes.Equal(item, der) {
			return true
		}
	}
	return false
}

func vriKey(contents []byte) string {
	sum := sha1.Sum(contents)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// TestLongTermLevel: B-LT na plikach z gopdfgo; OCSP dla podpisującego, CRL dla TSA, DSS z /VRI.
func TestLongTermLevel(t *testing.T) {
	pki := newTestPKI(t)
	docs := sourceDocuments(t)
	for name, pdf := range docs {
		for _, kind := range []string{"rsa", "p256"} {
			t.Run(name+"-"+kind, func(t *testing.T) {
				signed, err := SignPDF(t.Context(), pdf, pki.options(kind, LevelBLT))
				if err != nil {
					t.Fatal(err)
				}
				sigs := checkSignatures(t, signed, 1)
				checkLongTerm(t, pki, signed, sigs[0], kind)
				checkWithTools(t, signed, sigs, pki.root.cert, false)
				saveForInspection(t, "blt-"+name+"-"+kind, signed)
			})
		}
	}
}

// checkLongTerm sprawdza aktualizację z DSS: podpis kończy się tuż przed nią, DSS zawiera
// certyfikaty obu ścieżek, odpowiedź OCSP podpisującego i CRL urzędu, /VRI wskazuje podpis.
func checkLongTerm(t *testing.T, pki *testPKI, signed []byte, sig signatureInfo, kind string) {
	t.Helper()
	end := sig.byteRange[2] + sig.byteRange[3]
	if !bytes.HasSuffix(bytes.TrimRight(signed[:end], "\r\n"), []byte("%%EOF")) {
		t.Error("zakres podpisu nie kończy się na znaczniku EOF pierwszej aktualizacji")
	}
	if !bytes.Contains(signed[end:], []byte("/DSS")) {
		t.Error("po podpisie brak aktualizacji z /DSS")
	}
	certs, ocsps, crls, vri := dssContents(t, signed)
	for _, c := range []*x509.Certificate{pki.signers[kind].cert, pki.root.cert, pki.tsa.cert} {
		if !containsDER(certs, c.Raw) {
			t.Errorf("/Certs bez %q", c.Subject.CommonName)
		}
	}
	if len(ocsps) != 1 || len(crls) != 1 {
		t.Errorf("/OCSPs %d, /CRLs %d, oczekiwane 1 i 1", len(ocsps), len(crls))
	}
	for _, der := range ocsps {
		if status, _, err := checkOCSP(der, pki.signers[kind].cert, pki.root.cert); err != nil || status != ocspGood {
			t.Errorf("OCSP w DSS: %v %v", status, err)
		}
	}
	entry := vri[vriKey(sig.contents)]
	if entry == nil {
		t.Fatalf("/VRI bez klucza podpisu; klucze %v", vri)
	}
	for _, key := range []pdfName{"Cert", "OCSP", "CRL"} {
		if arr, _ := entry.get(key).(pdfArray); len(arr) == 0 {
			t.Errorf("/VRI bez /%s", key)
		}
	}
}

// TestLongTermOffline: dane o ważności podane wprost, bez klienta sieci.
func TestLongTermOffline(t *testing.T) {
	pki := newTestPKI(t)
	pdf := sourceDocuments(t)["pdfa2b"]
	ocsp, err := pki.ocspResponse(ocspRequest(pki.signers["rsa"].cert, pki.root.cert))
	if err != nil {
		t.Fatal(err)
	}
	crl, err := pki.crl(nil)
	if err != nil {
		t.Fatal(err)
	}
	opts := pki.options("rsa", LevelBLT)
	opts.Revocation = nil
	opts.ValidationData = ValidationData{OCSPResponses: [][]byte{ocsp}, CRLs: [][]byte{crl}}
	before := pki.count("ocsp") + pki.count("crl")
	signed, err := SignPDF(t.Context(), pdf, opts)
	if err != nil {
		t.Fatal(err)
	}
	if n := pki.count("ocsp") + pki.count("crl"); n != before {
		t.Errorf("podpis z danymi podanymi wprost łączył się z siecią (%d żądań)", n-before)
	}
	sigs := checkSignatures(t, signed, 1)
	checkLongTerm(t, pki, signed, sigs[0], "rsa")
	checkWithTools(t, signed, sigs, pki.root.cert, false)

	// sama lista CRL wystarcza też dla podpisującego
	opts.ValidationData.OCSPResponses = nil
	signed, err = SignPDF(t.Context(), pdf, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, ocsps, crls, _ := dssContents(t, signed); len(ocsps) != 0 || len(crls) != 1 {
		t.Errorf("tylko CRL: /OCSPs %d, /CRLs %d", len(ocsps), len(crls))
	}
}

// TestDelegatedResponder: OCSP podpisany przez upoważniony responder z id-pkix-ocsp-nocheck.
func TestDelegatedResponder(t *testing.T) {
	pki := newTestPKI(t)
	pki.delegated = true
	pdf := sourceDocuments(t)["plain"]
	signed, err := SignPDF(t.Context(), pdf, pki.options("p256", LevelBLT))
	if err != nil {
		t.Fatal(err)
	}
	certs, _, _, _ := dssContents(t, signed)
	if !containsDER(certs, pki.responder.cert.Raw) {
		t.Error("/Certs bez certyfikatu respondera")
	}
	checkWithTools(t, signed, checkSignatures(t, signed, 1), pki.root.cert, false)
}

// TestSecondSignatureWithDSS: drugi podpis B-LT na pliku z DSS uzupełnia słownik bez powtórzeń.
func TestSecondSignatureWithDSS(t *testing.T) {
	pki := newTestPKI(t)
	for _, name := range []string{"pdfa2b", "pdfua"} {
		t.Run(name, func(t *testing.T) {
			pdf := sourceDocuments(t)[name]
			first, err := SignPDF(t.Context(), pdf, pki.options("rsa", LevelBLT))
			if err != nil {
				t.Fatal(err)
			}
			opts := pki.options("p256", LevelBLT)
			opts.Name = "Anna Nowak"
			second, err := SignPDF(t.Context(), first, opts)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(second, first) {
				t.Fatal("drugi podpis zmienił bajty pliku")
			}
			sigs := checkSignatures(t, second, 2)
			certs, ocsps, crls, vri := dssContents(t, second)
			// root, TSA, dwóch podpisujących; po jednej odpowiedzi OCSP na podpisującego; jedna CRL
			if len(certs) != 4 || len(ocsps) != 2 || len(crls) != 1 {
				t.Errorf("DSS po drugim podpisie: certów %d, OCSP %d, CRL %d", len(certs), len(ocsps), len(crls))
			}
			for i, s := range sigs {
				if vri[vriKey(s.contents)] == nil {
					t.Errorf("/VRI bez podpisu %d", i+1)
				}
			}
			checkWithTools(t, second, sigs, pki.root.cert, false)
			saveForInspection(t, "blt-second-"+name, second)
		})
	}
}

// TestLevelBBDoesNotConnect: poziom domyślny ignoruje TSA i Revocation i nie łączy się z siecią.
func TestLevelBBDoesNotConnect(t *testing.T) {
	pki := newTestPKI(t)
	opts := pki.options("rsa", LevelBLT)
	opts.Level = LevelBB
	signed, err := SignPDF(t.Context(), sourceDocuments(t)["plain"], opts)
	if err != nil {
		t.Fatal(err)
	}
	if n := pki.count("tsa") + pki.count("ocsp") + pki.count("crl"); n != 0 {
		t.Errorf("B-B wysłał %d żądań", n)
	}
	if sigs := checkSignatures(t, signed, 1); sigs[0].token != nil {
		t.Error("B-B ma znacznik czasu")
	}
}

func TestTimestampErrors(t *testing.T) {
	pki := newTestPKI(t)
	pdf := sourceDocuments(t)["plain"]
	for _, c := range []struct {
		mode string
		want error
	}{
		{tsaReject, ErrTimestamp},
		{tsaBadImprint, ErrTimestamp},
		{tsaBadNonce, ErrTimestamp},
		{tsaBadSig, ErrTimestamp},
		{tsaHuge, ErrReserveTooSmall},
	} {
		pki.setTSAMode(c.mode)
		opts := pki.options("rsa", LevelBT)
		if c.mode == tsaHuge {
			opts.TimestampReserve = 3000
		}
		_, err := SignPDF(t.Context(), pdf, opts)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: błąd %v, oczekiwany %v", c.mode, err, c.want)
		} else {
			t.Logf("%s: %v", c.mode, err)
		}
	}
	pki.setTSAMode(tsaOK)

	// TSA zwraca błąd HTTP
	opts := pki.options("rsa", LevelBT)
	opts.TSA = TSAClient{URL: pki.server.URL + "/none", HTTPClient: pki.server.Client()}
	if _, err := SignPDF(t.Context(), pdf, opts); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("TSA 404: %v", err)
	}
	// TSAClient bez klienta HTTP
	opts.TSA = TSAClient{URL: pki.server.URL + "/tsa"}
	if _, err := SignPDF(t.Context(), pdf, opts); !errors.Is(err, ErrNoHTTPClient) {
		t.Errorf("TSAClient bez HTTPClient: %v", err)
	}
	// B-T bez TSA
	opts.TSA = nil
	if _, err := SignPDF(t.Context(), pdf, opts); !errors.Is(err, ErrNoTSA) {
		t.Errorf("B-T bez TSA: %v", err)
	}
	// nieznany poziom
	opts.Level = 7
	if _, err := SignPDF(t.Context(), pdf, opts); err == nil {
		t.Error("nieznany poziom przeszedł")
	}
	// przerwany ctx w trakcie oczekiwania na TSA
	pki.setTSAMode(tsaHang)
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	if _, err := SignPDF(ctx, pdf, pki.options("rsa", LevelBT)); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("ctx w trakcie TSA: %v", err)
	}
	pki.setTSAMode(tsaOK)
}

func TestValidationErrors(t *testing.T) {
	pki := newTestPKI(t)
	pdf := sourceDocuments(t)["plain"]
	// B-LT bez klienta i bez danych
	opts := pki.options("rsa", LevelBLT)
	opts.Revocation = nil
	if _, err := SignPDF(t.Context(), pdf, opts); !errors.Is(err, ErrNoValidationData) {
		t.Errorf("B-LT bez danych: %v", err)
	}
	// dane podane wprost, ale tylko dla podpisującego (brak dla TSA)
	ocsp, _ := pki.ocspResponse(ocspRequest(pki.signers["rsa"].cert, pki.root.cert))
	opts.ValidationData.OCSPResponses = [][]byte{ocsp}
	if _, err := SignPDF(t.Context(), pdf, opts); !errors.Is(err, ErrNoValidationData) || !strings.Contains(err.Error(), "TSA") {
		t.Errorf("B-LT bez danych dla TSA: %v", err)
	}
	// brak certyfikatu wystawcy
	opts = pki.options("rsa", LevelBLT)
	opts.Chain = nil
	if _, err := SignPDF(t.Context(), pdf, opts); !errors.Is(err, ErrMissingIssuer) {
		t.Errorf("bez wystawcy: %v", err)
	}
	// odpowiedź OCSP innego certyfikatu podana wprost nie wystarcza
	other, _ := pki.ocspResponse(ocspRequest(pki.signers["p256"].cert, pki.root.cert))
	opts = pki.options("rsa", LevelBLT)
	opts.Revocation = nil
	crl, _ := pki.crl(nil)
	opts.ValidationData = ValidationData{OCSPResponses: [][]byte{other}}
	if _, err := SignPDF(t.Context(), pdf, opts); !errors.Is(err, ErrNoValidationData) {
		t.Errorf("OCSP innego certyfikatu: %v", err)
	}
	// zepsuty podpis odpowiedzi OCSP
	broken := append([]byte(nil), ocsp...)
	broken[len(broken)-5] ^= 0xff
	opts.ValidationData = ValidationData{OCSPResponses: [][]byte{broken}, CRLs: [][]byte{crl}}
	if _, err := SignPDF(t.Context(), pdf, opts); err == nil || !strings.Contains(err.Error(), "OCSP") {
		t.Errorf("zepsuty OCSP: %v", err)
	}
	// certyfikat unieważniony: OCSP i CRL
	pki.revoke(pki.signers["rsa"].cert)
	if _, err := SignPDF(t.Context(), pdf, pki.options("rsa", LevelBLT)); !errors.Is(err, ErrRevoked) {
		t.Errorf("unieważniony (OCSP): %v", err)
	}
	revokedCRL, _ := pki.crl([]x509.RevocationListEntry{{SerialNumber: pki.signers["rsa"].cert.SerialNumber, RevocationTime: signingTime}})
	opts.ValidationData = ValidationData{CRLs: [][]byte{revokedCRL}}
	if _, err := SignPDF(t.Context(), pdf, opts); !errors.Is(err, ErrRevoked) {
		t.Errorf("unieważniony (CRL): %v", err)
	}
	// RevocationClient bez klienta HTTP
	opts = pki.options("p256", LevelBLT)
	opts.Revocation = RevocationClient{}
	if _, err := SignPDF(t.Context(), pdf, opts); !errors.Is(err, ErrNoValidationData) || !strings.Contains(err.Error(), ErrNoHTTPClient.Error()) {
		t.Errorf("RevocationClient bez HTTPClient: %v", err)
	}
}

// contextSigner podpisuje kluczem lokalnym przez SignContext; z hang czeka na koniec ctx.
type contextSigner struct {
	key   crypto.Signer
	hang  bool
	calls *int
}

func (s contextSigner) Public() crypto.PublicKey { return s.key.Public() }
func (s contextSigner) Sign(r io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	return nil, errors.New("Sign zamiast SignContext")
}
func (s contextSigner) SignContext(ctx context.Context, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	*s.calls++
	if s.hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return s.key.Sign(nil, digest, opts)
}

func TestContextSigner(t *testing.T) {
	pdf := sourceDocuments(t)["plain"]
	id := identity(t, "p256")
	calls := 0
	opts := signOptions(id)
	opts.Signer = contextSigner{key: id.key, calls: &calls}
	signed, err := SignPDF(t.Context(), pdf, opts)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("SignContext wołany %d razy", calls)
	}
	checkSignatures(t, signed, 1)
	opts.Signer = contextSigner{key: id.key, hang: true, calls: &calls}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := SignPDF(ctx, pdf, opts); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("przerwany podpis zdalny: %v", err)
	}
}

// TestVeraPDFAfterLongTerm sprawdza veraPDF-em zgodność PDF/A i PDF/UA po B-T i B-LT, gdy
// GOPDFGO_VERAPDF wskazuje program veraPDF (sprawdzenie długie, poza zwykłym go test).
func TestVeraPDFAfterLongTerm(t *testing.T) {
	verapdf := os.Getenv("GOPDFGO_VERAPDF")
	if verapdf == "" {
		t.Skip("GOPDFGO_VERAPDF nie ustawione")
	}
	pki := newTestPKI(t)
	docs := sourceDocuments(t)
	dir := t.TempDir()
	cases := []struct{ doc, flavour string }{
		{"pdfa2b", "2b"}, {"pdfa3b", "3b"}, {"facturx", "3b"}, {"pdfua", "2a"}, {"pdfua", "ua1"},
	}
	for _, c := range cases {
		for _, level := range []Level{LevelBT, LevelBLT} {
			signed, err := SignPDF(t.Context(), docs[c.doc], pki.options("rsa", level))
			if err != nil {
				t.Fatal(err)
			}
			if level == LevelBLT {
				if signed, err = SignPDF(t.Context(), signed, pki.options("p256", level)); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(dir, c.doc+"-"+level.String()+".pdf")
			os.WriteFile(path, signed, 0o644)
			out, _ := exec.Command(verapdf, "-f", c.flavour, path).CombinedOutput()
			if !strings.Contains(string(out), `isCompliant="true"`) {
				t.Errorf("veraPDF -f %s, %s %s: niezgodny\n%s", c.flavour, c.doc, level, out)
			} else {
				t.Logf("veraPDF -f %s, %s %s: zgodny", c.flavour, c.doc, level)
			}
		}
	}
}
