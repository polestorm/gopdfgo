package sign

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/polestorm/gopdfgo"
)

var signingTime = time.Date(2026, 11, 23, 10, 30, 0, 0, time.FixedZone("CET", 3600))

// testIdentity to klucz i samopodpisany certyfikat wygenerowane w teście (klucz wyłącznie testowy).
type testIdentity struct {
	key  crypto.Signer
	cert *x509.Certificate
}

var (
	identities   = map[string]testIdentity{}
	identitiesMu sync.Mutex
)

func identity(t testing.TB, kind string) testIdentity {
	t.Helper()
	identitiesMu.Lock()
	defer identitiesMu.Unlock()
	if id, ok := identities[kind]; ok {
		return id
	}
	var key crypto.Signer
	var err error
	switch kind {
	case "rsa":
		key, err = rsa.GenerateKey(rand.Reader, 2048)
	case "p256":
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case "p384":
		key, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	}
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "gopdfgo test signer " + kind, Organization: []string{"gopdfgo tests"}},
		NotBefore:             time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	id := testIdentity{key: key, cert: cert}
	identities[kind] = id
	return id
}

var renderMu sync.Mutex

func render(t testing.TB, html string, opts gopdfgo.Options) []byte {
	t.Helper()
	renderMu.Lock()
	defer renderMu.Unlock()
	font, err := os.ReadFile("../testdata/fonts/NotoSans-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	opts.Fonts = []gopdfgo.Font{{Name: "NotoSans-Regular.ttf", Data: font}}
	result, err := gopdfgo.Render(context.Background(), []byte(html), opts)
	if err != nil {
		t.Fatal(err)
	}
	return result.PDF
}

const documentHTML = `<!DOCTYPE html><html lang="pl"><head><title>Umowa</title></head>
<body><h1>Umowa</h1><p>Treść dokumentu do podpisu: zażółć gęślą jaźń.</p>
<p style="break-before: page">Druga strona.</p></body></html>`

func signOptions(id testIdentity) Options {
	return Options{Signer: id.key, Certificate: id.cert, Time: signingTime,
		Name: "Jan Kowalski", Reason: "Zatwierdzenie", Location: "Kraków", ContactInfo: "jan@example.com"}
}

// sourceDocuments to pliki z gopdfgo, na których sprawdzamy podpis.
func sourceDocuments(t *testing.T) map[string][]byte {
	t.Helper()
	invoice, err := os.ReadFile("../testdata/facturx/20-invoice-minimum.xml")
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]byte{
		"plain":   render(t, documentHTML, gopdfgo.Options{}),
		"pdfua":   render(t, documentHTML, gopdfgo.Options{Conformance: gopdfgo.PDFA2a, Accessibility: gopdfgo.PDFUA1}),
		"pdfa2b":  render(t, documentHTML, gopdfgo.Options{Conformance: gopdfgo.PDFA2b}),
		"pdfa3b":  render(t, documentHTML, gopdfgo.Options{Conformance: gopdfgo.PDFA3b}),
		"facturx": render(t, documentHTML, gopdfgo.Options{FacturX: &gopdfgo.FacturX{XML: invoice, ModTime: signingTime}}),
	}
}

func TestSignGopdfgoDocuments(t *testing.T) {
	docs := sourceDocuments(t)
	for name, pdf := range docs {
		for _, kind := range []string{"rsa", "p256"} {
			t.Run(name+"-"+kind, func(t *testing.T) {
				id := identity(t, kind)
				signed, err := SignPDF(t.Context(), pdf, signOptions(id))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.HasPrefix(signed, pdf) {
					t.Fatal("podpis zmienił bajty pliku wejściowego")
				}
				sigs := checkSignatures(t, signed, 1)
				checkWithTools(t, signed, sigs, id)
				saveForInspection(t, name+"-"+kind, signed)
			})
		}
	}
}

// Rodzaj sekcji odsyłaczy aktualizacji odpowiada rodzajowi w pliku wejściowym.
func TestXrefKindFollowsInput(t *testing.T) {
	docs := sourceDocuments(t)
	id := identity(t, "rsa")
	for name, pdf := range docs {
		in, err := readPDF(pdf)
		if err != nil {
			t.Fatal(err)
		}
		signed, err := SignPDF(t.Context(), pdf, signOptions(id))
		if err != nil {
			t.Fatal(err)
		}
		out, err := readPDF(signed)
		if err != nil {
			t.Fatal(err)
		}
		if in.xrefStream != out.xrefStream {
			t.Errorf("%s: odsyłacze wejścia strumieniem %v, aktualizacji %v", name, in.xrefStream, out.xrefStream)
		}
		if prev, _ := out.trailer.get("Prev").(pdfNumber); string(prev) != strconv.FormatInt(in.startxref, 10) {
			t.Errorf("%s: /Prev %s, oczekiwane %d", name, prev, in.startxref)
		}
		t.Logf("%s: strumień odsyłaczy %v", name, in.xrefStream)
	}
}

// Ręcznie zapisany PDF z tablicą odsyłaczy, istniejącym /AcroForm pośrednim i /Annots pośrednim.
func TestSignHandWrittenTable(t *testing.T) {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595.2756 841.8898] /Contents 4 0 R /Annots 6 0 R >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Fields [] /DA (/Helv 0 Tf 0 g) >>",
		"[]",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := []int{}
	for i, obj := range objects {
		offsets = append(offsets, b.Len())
		b.WriteString(strconv.Itoa(i+1) + " 0 obj\n" + obj + "\nendobj\n")
	}
	xref := b.Len()
	b.WriteString("xref\n0 " + strconv.Itoa(len(objects)+1) + "\n0000000000 65535 f\r\n")
	for _, off := range offsets {
		b.WriteString(leftPad(strconv.Itoa(off), 10) + " 00000 n\r\n")
	}
	b.WriteString("trailer\n<< /Size 7 /Root 1 0 R >>\nstartxref\n" + strconv.Itoa(xref) + "\n%%EOF")
	pdf := b.Bytes()
	id := identity(t, "p384")
	signed, err := SignPDF(t.Context(), pdf, signOptions(id))
	if err != nil {
		t.Fatal(err)
	}
	out, err := readPDF(signed)
	if err != nil {
		t.Fatal(err)
	}
	if out.xrefStream {
		t.Error("aktualizacja pliku z tablicą odsyłaczy zapisała strumień")
	}
	page, _ := out.resolve(pdfRef{num: 3})
	if mb := page.(*pdfDict).get("MediaBox").(pdfArray); mb[2] != pdfNumber("595.2756") {
		t.Errorf("MediaBox po przepisaniu strony: %v", mb)
	}
	annots, _ := out.resolve(pdfRef{num: 6})
	if len(annots.(pdfArray)) != 1 {
		t.Errorf("/Annots pośrednie: %v", annots)
	}
	form, _ := out.resolve(pdfRef{num: 5})
	if form.(*pdfDict).get("SigFlags") != pdfNumber("3") || len(form.(*pdfDict).get("Fields").(pdfArray)) != 1 {
		t.Errorf("/AcroForm pośredni: %v", form)
	}
	sigs := checkSignatures(t, signed, 1)
	checkWithTools(t, signed, sigs, id)
}

func leftPad(s string, n int) string { return strings.Repeat("0", n-len(s)) + s }

// Drugi podpis: pierwszy zostaje ważny, bo jego zakres bajtów się nie zmienia.
func TestSecondSignature(t *testing.T) {
	for _, name := range []string{"plain", "pdfua"} {
		t.Run(name, func(t *testing.T) {
			pdf := sourceDocuments(t)[name]
			first, err := SignPDF(t.Context(), pdf, signOptions(identity(t, "rsa")))
			if err != nil {
				t.Fatal(err)
			}
			opts := signOptions(identity(t, "p256"))
			opts.Name = "Anna Nowak"
			second, err := SignPDF(t.Context(), first, opts)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(second, first) {
				t.Fatal("drugi podpis zmienił bajty pliku")
			}
			sigs := checkSignatures(t, second, 2)
			if sigs[0].fieldName != "Signature1" || sigs[1].fieldName != "Signature2" {
				t.Errorf("nazwy pól: %q, %q", sigs[0].fieldName, sigs[1].fieldName)
			}
			checkWithTools(t, second, sigs, identity(t, "rsa"))
			saveForInspection(t, "second-"+name, second)
		})
	}
}

func TestDeterministicWithFixedTime(t *testing.T) {
	pdf := sourceDocuments(t)["pdfua"]
	id := identity(t, "rsa")
	a, err := SignPDF(t.Context(), pdf, signOptions(id))
	if err != nil {
		t.Fatal(err)
	}
	b, err := SignPDF(t.Context(), pdf, signOptions(id))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("dwa podpisy RSA ze stałym czasem dały różne bajty")
	}
	// ECDSA jest losowy, ale poza /Contents plik jest ten sam
	ec := identity(t, "p256")
	c, _ := SignPDF(t.Context(), pdf, signOptions(ec))
	d, _ := SignPDF(t.Context(), pdf, signOptions(ec))
	sc, sd := checkSignatures(t, c, 1)[0], checkSignatures(t, d, 1)[0]
	if !bytes.Equal(c[:sc.contentsStart], d[:sd.contentsStart]) || !bytes.Equal(c[sc.contentsEnd:], d[sd.contentsEnd:]) {
		t.Error("podpisy ECDSA różnią się poza /Contents")
	}
}

func TestSignErrors(t *testing.T) {
	pdf := sourceDocuments(t)["plain"]
	rsaID, ecID := identity(t, "rsa"), identity(t, "p256")
	cases := []struct {
		opts Options
		want error
	}{
		{Options{Certificate: rsaID.cert}, ErrNoSigner},
		{Options{Signer: rsaID.key}, ErrNoSigner},
		{Options{Signer: ecID.key, Certificate: rsaID.cert}, ErrKeyMismatch},
		{Options{Signer: pssSigner{rsaID.key.(*rsa.PrivateKey)}, Certificate: rsaID.cert}, ErrKeyMismatch},
	}
	for i, c := range cases {
		if _, err := SignPDF(t.Context(), pdf, c.opts); !errors.Is(err, c.want) {
			t.Errorf("przypadek %d: błąd %v, oczekiwany %v", i, err, c.want)
		}
	}
	if _, err := SignPDF(t.Context(), []byte("nie PDF"), signOptions(rsaID)); !errors.Is(err, errMalformed) {
		t.Errorf("nie PDF: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := SignPDF(ctx, pdf, signOptions(rsaID)); !errors.Is(err, context.Canceled) {
		t.Errorf("ctx anulowany: %v", err)
	}
	opts := signOptions(rsaID)
	opts.FieldName = "Signature1"
	signed, err := SignPDF(t.Context(), pdf, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SignPDF(t.Context(), signed, opts); err == nil {
		t.Error("drugie pole o tej samej nazwie przeszło")
	}
}

// pssSigner podpisuje schematem PSS zamiast PKCS#1 v1.5: SignPDF ma to wykryć.
type pssSigner struct{ key *rsa.PrivateKey }

func (s pssSigner) Public() crypto.PublicKey { return s.key.Public() }
func (s pssSigner) Sign(_ io.Reader, digest []byte, _ crypto.SignerOpts) ([]byte, error) {
	return rsa.SignPSS(rand.Reader, s.key, crypto.SHA256, digest, nil)
}

// signatureInfo to podpis odczytany z pliku.
type signatureInfo struct {
	fieldName                  string
	contentsStart, contentsEnd int
	byteRange                  [4]int
	cms                        []byte
}

// checkSignatures odczytuje pola podpisu własnym czytnikiem i sprawdza każdy podpis w Go:
// /ByteRange, skrót w message-digest i podpis atrybutów kluczem z certyfikatu w CMS.
func checkSignatures(t *testing.T, pdf []byte, want int) []signatureInfo {
	t.Helper()
	f, err := readPDF(pdf)
	if err != nil {
		t.Fatal(err)
	}
	rootObj, _ := f.resolve(f.trailer.get("Root"))
	root := rootObj.(*pdfDict)
	formObj, _ := f.resolve(root.get("AcroForm"))
	form := formObj.(*pdfDict)
	if f.intValue(form.get("SigFlags")) != 3 {
		t.Errorf("/SigFlags %v", form.get("SigFlags"))
	}
	fieldsObj, _ := f.resolve(form.get("Fields"))
	var sigs []signatureInfo
	for _, ref := range fieldsObj.(pdfArray) {
		fieldObj, _ := f.resolve(ref)
		field := fieldObj.(*pdfDict)
		if field.get("FT") != pdfName("Sig") {
			continue
		}
		sigObj, _ := f.resolve(field.get("V"))
		sig := sigObj.(*pdfDict)
		if sig.get("SubFilter") != pdfName("ETSI.CAdES.detached") || sig.get("Filter") != pdfName("Adobe.PPKLite") {
			t.Errorf("słownik podpisu: %v %v", sig.get("Filter"), sig.get("SubFilter"))
		}
		info := signatureInfo{fieldName: string(field.get("T").(pdfString))}
		for i, v := range sig.get("ByteRange").(pdfArray) {
			info.byteRange[i], _ = strconv.Atoi(string(v.(pdfNumber)))
		}
		br := info.byteRange
		info.contentsStart, info.contentsEnd = br[1], br[2]
		if br[0] != 0 || pdf[br[1]] != '<' || pdf[br[2]-1] != '>' {
			t.Errorf("/ByteRange %v nie wskazuje /Contents", br)
		}
		// ostatni podpis obejmuje cały plik
		if len(sigs) == want-1 && br[2]+br[3] != len(pdf) {
			t.Errorf("/ByteRange %v nie kończy się na końcu pliku (%d B)", br, len(pdf))
		}
		contents := sig.get("Contents").(pdfString)
		cms := trimDER(t, contents)
		info.cms = cms
		h := sha256.New()
		h.Write(pdf[br[0] : br[0]+br[1]])
		h.Write(pdf[br[2] : br[2]+br[3]])
		verifyCMS(t, cms, h.Sum(nil))
		if !regexp.MustCompile(`^D:\d{14}[+-]\d\d'\d\d'$`).Match(sig.get("M").(pdfString)) {
			t.Errorf("/M %q", sig.get("M"))
		}
		sigs = append(sigs, info)
	}
	if len(sigs) != want {
		t.Fatalf("podpisów %d, oczekiwane %d", len(sigs), want)
	}
	return sigs
}

// trimDER odcina zera dopełnienia po strukturze DER.
func trimDER(t *testing.T, contents []byte) []byte {
	t.Helper()
	var raw asn1.RawValue
	rest, err := asn1.Unmarshal(contents, &raw)
	if err != nil {
		t.Fatalf("CMS: %v", err)
	}
	if strings.Trim(string(rest), "\x00") != "" {
		t.Error("po CMS w /Contents są dane inne niż zera")
	}
	return raw.FullBytes
}

func children(t *testing.T, der []byte) []asn1.RawValue {
	t.Helper()
	var outer asn1.RawValue
	if _, err := asn1.Unmarshal(der, &outer); err != nil {
		t.Fatal(err)
	}
	var out []asn1.RawValue
	for rest := outer.Bytes; len(rest) > 0; {
		var v asn1.RawValue
		var err error
		if rest, err = asn1.Unmarshal(rest, &v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

// verifyCMS sprawdza strukturę SignedData, atrybuty podpisane i podpis.
func verifyCMS(t *testing.T, cms, digest []byte) {
	t.Helper()
	ci := children(t, cms)
	var oid asn1.ObjectIdentifier
	asn1.Unmarshal(ci[0].FullBytes, &oid)
	if !oid.Equal(oidSignedData) {
		t.Fatalf("ContentInfo: %v", oid)
	}
	sd := children(t, ci[1].FullBytes)
	sd = children(t, sd[0].FullBytes)
	// version, digestAlgorithms, encapContentInfo, certificates [0], signerInfos
	if len(sd) != 5 || sd[3].Tag != 0 || sd[3].Class != asn1.ClassContextSpecific {
		t.Fatalf("SignedData: %d elementów", len(sd))
	}
	if eci := children(t, sd[2].FullBytes); len(eci) != 1 {
		t.Error("encapContentInfo ma eContent: podpis nie jest odłączony")
	}
	certs := children(t, sd[3].FullBytes)
	cert, err := x509.ParseCertificate(certs[0].FullBytes)
	if err != nil {
		t.Fatal(err)
	}
	si := children(t, children(t, sd[4].FullBytes)[0].FullBytes)
	attrsRaw := append([]byte{0x31}, si[3].FullBytes[1:]...)
	attrs := children(t, attrsRaw)
	found := map[string][]byte{}
	for _, a := range attrs {
		parts := children(t, a.FullBytes)
		var id asn1.ObjectIdentifier
		asn1.Unmarshal(parts[0].FullBytes, &id)
		found[id.String()] = children(t, parts[1].FullBytes)[0].FullBytes
	}
	if len(found) != 3 || found[oidContentType.String()] == nil || found[oidSigningCertificateV2.String()] == nil {
		t.Errorf("atrybuty podpisane: %v", found)
	}
	var md []byte
	asn1.Unmarshal(found[oidMessageDigest.String()], &md)
	if !bytes.Equal(md, digest) {
		t.Error("message-digest różny od skrótu /ByteRange")
	}
	certHash := sha256.Sum256(cert.Raw)
	if !bytes.Contains(found[oidSigningCertificateV2.String()], certHash[:]) {
		t.Error("signing-certificate-v2 nie zawiera skrótu certyfikatu")
	}
	var signature []byte
	asn1.Unmarshal(si[5].FullBytes, &signature)
	sum := sha256.Sum256(attrsRaw)
	switch key := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		err = rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], signature)
	case *ecdsa.PublicKey:
		if !ecdsa.VerifyASN1(key, sum[:], signature) {
			err = errors.New("ECDSA")
		}
	}
	if err != nil {
		t.Errorf("podpis CMS niepoprawny: %v", err)
	}
}

// checkWithTools sprawdza wynik programami pdfsig (Poppler) i openssl cms, jeśli są dostępne.
func checkWithTools(t *testing.T, pdf []byte, sigs []signatureInfo, id testIdentity) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "signed.pdf")
	if err := os.WriteFile(path, pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	if pdfsig, err := exec.LookPath("pdfsig"); err == nil {
		out, err := exec.Command(pdfsig, "-nocert", path).CombinedOutput()
		if err != nil {
			t.Errorf("pdfsig: %v\n%s", err, out)
		}
		if n := strings.Count(string(out), "Signature Validation: Signature is Valid."); n != len(sigs) {
			t.Errorf("pdfsig: ważnych podpisów %d z %d\n%s", n, len(sigs), out)
		}
		if !strings.Contains(string(out), "Total document signed") {
			t.Errorf("pdfsig: ostatni podpis nie obejmuje całego dokumentu\n%s", out)
		}
		if !strings.Contains(string(out), "ETSI.CAdES.detached") {
			t.Errorf("pdfsig: brak ETSI.CAdES.detached\n%s", out)
		}
	} else {
		t.Log("brak pdfsig, sprawdzenie pominięte")
	}
	openssl, err := exec.LookPath("openssl")
	if err != nil {
		t.Log("brak openssl, sprawdzenie pominięte")
		return
	}
	for i, sig := range sigs {
		br := sig.byteRange
		content := append(append([]byte(nil), pdf[br[0]:br[0]+br[1]]...), pdf[br[2]:br[2]+br[3]]...)
		contentPath := filepath.Join(dir, "content"+strconv.Itoa(i))
		cmsPath := filepath.Join(dir, "sig"+strconv.Itoa(i)+".der")
		os.WriteFile(contentPath, content, 0o644)
		os.WriteFile(cmsPath, sig.cms, 0o644)
		args := []string{"cms", "-verify", "-inform", "DER", "-in", cmsPath, "-binary", "-content", contentPath, "-out", os.DevNull}
		out, err := exec.Command(openssl, append(args, "-noverify")...).CombinedOutput()
		if err != nil || !strings.Contains(string(out), "Verification successful") {
			t.Errorf("openssl cms -noverify, podpis %d: %v\n%s", i+1, err, out)
		}
		if i == 0 {
			caPath := filepath.Join(dir, "ca.pem")
			os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: id.cert.Raw}), 0o644)
			out, err = exec.Command(openssl, append(args, "-CAfile", caPath, "-purpose", "any")...).CombinedOutput()
			if err != nil || !strings.Contains(string(out), "Verification successful") {
				t.Errorf("openssl cms -CAfile: %v\n%s", err, out)
			}
		}
	}
}

// saveForInspection zapisuje podpisany plik do katalogu z GOPDFGO_SIGN_OUT (sprawdzenie veraPDF).
func saveForInspection(t *testing.T, name string, pdf []byte) {
	if dir := os.Getenv("GOPDFGO_SIGN_OUT"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, name+".pdf"), pdf, 0o644); err != nil {
			t.Error(err)
		}
	}
}

func TestParseLiteralAndHexStrings(t *testing.T) {
	lx := &lexer{data: []byte(`<< /A (a\(b\)\n\101) /B <48 65 6c6C 6> /C [1 0 R -2.5 /N#20x] /D true /E null >>`)}
	obj, err := lx.object()
	if err != nil {
		t.Fatal(err)
	}
	d := obj.(*pdfDict)
	if string(d.get("A").(pdfString)) != "a(b)\nA" || hex.EncodeToString(d.get("B").(pdfString)) != "48656c6c60" {
		t.Errorf("ciągi: %q %x", d.get("A"), d.get("B"))
	}
	arr := d.get("C").(pdfArray)
	if arr[0] != (pdfRef{1, 0}) || arr[1] != pdfNumber("-2.5") || arr[2] != pdfName("N x") {
		t.Errorf("tablica: %v", arr)
	}
	var b bytes.Buffer
	writeObject(&b, d)
	if want := "<< /A <612862290a41>"; !strings.HasPrefix(b.String(), want) || !strings.Contains(b.String(), "/N#20x") {
		t.Errorf("zapis: %s", b.String())
	}
}

// Pliki innych programów z testdata: Chromium (Skia) i WeasyPrint.
func TestSignOtherProducers(t *testing.T) {
	paths, _ := filepath.Glob("../testdata/documents/reference/chromium/*.pdf")
	weasy, _ := filepath.Glob("../testdata/samples/weasyprint/*/*.pdf")
	paths = append(paths, weasy...)
	if len(paths) == 0 {
		t.Skip("brak plików PDF w testdata")
	}
	id := identity(t, "p256")
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			pdf, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			signed, err := SignPDF(t.Context(), pdf, signOptions(id))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(signed, pdf) {
				t.Fatal("podpis zmienił bajty pliku wejściowego")
			}
			checkWithTools(t, signed, checkSignatures(t, signed, 1), id)
		})
	}
}

// Uszkodzone pliki (ucięte, z podmienionymi bajtami) dają błąd albo podpis, nigdy panikę.
func TestDamagedInputDoesNotPanic(t *testing.T) {
	docs := sourceDocuments(t)
	id := identity(t, "p256")
	for _, name := range []string{"plain", "pdfua"} {
		pdf := docs[name]
		for cut := 0; cut < len(pdf); cut += 53 {
			SignPDF(t.Context(), pdf[:cut], signOptions(id))
			damaged := append([]byte(nil), pdf...)
			for k := cut; k < len(damaged) && k < cut+8; k++ {
				damaged[k] ^= 0x5a
			}
			SignPDF(t.Context(), damaged, signOptions(id))
		}
	}
}
