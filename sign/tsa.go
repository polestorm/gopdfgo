package sign

// Znacznik czasu RFC 3161 dla wartości podpisu (PAdES B-T: ETSI EN 319 142-1 § 6.3, ETSI EN 319 122-1
// § 5.3, atrybut niepodpisany signature-time-stamp-token). Żądanie zawiera skrót SHA-256 pola
// signature z SignerInfo, losowy nonce i certReq; odpowiedź jest sprawdzana: status, typ treści,
// skrót (messageImprint), nonce, podpis tokenu kluczem certyfikatu TSA i jego rozszerzenie
// extKeyUsage timeStamping (RFC 3161 § 2.3). Zaufania do certyfikatu TSA pakiet nie ocenia: to
// zadanie walidatora podpisu.

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	_ "crypto/sha512" // SHA-384 i SHA-512 w tokenach TSA i odpowiedziach OCSP
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"
)

// TimestampAuthority to dostawca znaczników czasu RFC 3161 na poziomie przesyłu: dostaje żądanie
// TimeStampReq w DER (RFC 3161 § 2.4.1) i zwraca odpowiedź TimeStampResp w DER (§ 2.4.2). Odpowiedź
// sprawdza SignPDF, więc własna implementacja (inny protokół, uwierzytelnianie, pamięć podręczna)
// odpowiada tylko za przesył. TSAClient to implementacja przez HTTP (RFC 3161 § 3.4).
type TimestampAuthority interface {
	Timestamp(ctx context.Context, request []byte) ([]byte, error)
}

// TSAClient wysyła żądanie znacznika czasu metodą POST z typem application/timestamp-query
// (RFC 3161 § 3.4) pod URL przez HTTPClient. Oba pola są wymagane: pakiet nie łączy się z siecią
// inaczej niż przez klienta i adres podane jawnie.
type TSAClient struct {
	URL        string
	HTTPClient *http.Client
}

// Timestamp implementuje TimestampAuthority.
func (c TSAClient) Timestamp(ctx context.Context, request []byte) ([]byte, error) {
	return postDER(ctx, c.HTTPClient, c.URL, "application/timestamp-query", request)
}

// maxResponse ogranicza długość odpowiedzi TSA, OCSP i CRL czytanej z sieci.
const maxResponse = 16 << 20

// postDER wysyła body metodą POST i zwraca treść odpowiedzi 200.
func postDER(ctx context.Context, client *http.Client, url, contentType string, body []byte) ([]byte, error) {
	if client == nil {
		return nil, ErrNoHTTPClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	return doRequest(client, req)
}

// getDER pobiera url metodą GET i zwraca treść odpowiedzi 200.
func getDER(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	if client == nil {
		return nil, ErrNoHTTPClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}
	return doRequest(client, req)
}

func doRequest(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sign: %s %s: HTTP %s", req.Method, req.URL.Redacted(), resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponse {
		return nil, fmt.Errorf("sign: %s: odpowiedź dłuższa niż %d B", req.URL.Redacted(), maxResponse)
	}
	return data, nil
}

// timestampResult to sprawdzony token znacznika czasu.
type timestampResult struct {
	token        []byte              // TimeStampToken (ContentInfo z SignedData) w DER
	signer       *x509.Certificate   // certyfikat TSA, którym podpisano token
	certificates []*x509.Certificate // certyfikaty zapisane w tokenie
	genTime      time.Time
}

// requestTimestamp pobiera i sprawdza znacznik czasu dla wartości podpisu. pool to certyfikaty, wśród
// których szukać certyfikatu TSA, gdy token go nie zawiera.
func requestTimestamp(ctx context.Context, tsa TimestampAuthority, signature []byte, pool []*x509.Certificate) (*timestampResult, error) {
	imprint := sha256.Sum256(signature)
	nonce, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 63))
	if err != nil {
		return nil, err
	}
	nonce.Add(nonce, big.NewInt(1)) // dodatni i niezerowy
	request := timestampRequest(imprint[:], nonce)
	response, err := tsa.Timestamp(ctx, request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("sign: znacznik czasu: %w", err)
	}
	return checkTimestampResponse(response, imprint[:], nonce, pool)
}

// timestampRequest koduje TimeStampReq (RFC 3161 § 2.4.1): wersja 1, messageImprint z SHA-256
// (AlgorithmIdentifier bez parametrów, RFC 5754 § 2), nonce, certReq TRUE.
func timestampRequest(imprint []byte, nonce *big.Int) []byte {
	return derSequence(
		derInteger(big.NewInt(1)),
		derSequence(sha256Identifier, derOctets(imprint)),
		derInteger(nonce),
		[]byte{0x01, 0x01, 0xff},
	)
}

// Struktury ASN.1 odczytu (RFC 3161, RFC 5652).
type (
	timeStampResp struct {
		Status pkiStatusInfo
		Token  asn1.RawValue `asn1:"optional"`
	}
	pkiStatusInfo struct {
		Status       int
		StatusString []string       `asn1:"optional"`
		FailInfo     asn1.BitString `asn1:"optional"`
	}
	contentInfoASN struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,tag:0"`
	}
	signedDataASN struct {
		Version          int
		DigestAlgorithms asn1.RawValue
		EncapContentInfo encapContentInfoASN
		Certificates     asn1.RawValue   `asn1:"optional,tag:0"`
		CRLs             asn1.RawValue   `asn1:"optional,tag:1"`
		SignerInfos      []signerInfoASN `asn1:"set"`
	}
	encapContentInfoASN struct {
		EContentType asn1.ObjectIdentifier
		EContent     []byte `asn1:"explicit,optional,tag:0"`
	}
	signerInfoASN struct {
		Version            int
		SID                asn1.RawValue
		DigestAlgorithm    pkix.AlgorithmIdentifier
		SignedAttrs        asn1.RawValue `asn1:"optional,tag:0"`
		SignatureAlgorithm pkix.AlgorithmIdentifier
		Signature          []byte
		UnsignedAttrs      asn1.RawValue `asn1:"optional,tag:1"`
	}
	attributeASN struct {
		Type   asn1.ObjectIdentifier
		Values asn1.RawValue `asn1:"set"`
	}
	issuerAndSerialASN struct {
		Issuer asn1.RawValue
		Serial *big.Int
	}
)

func timestampError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrTimestamp, fmt.Sprintf(format, args...))
}

// checkTimestampResponse sprawdza odpowiedź TSA i zwraca token.
func checkTimestampResponse(response, imprint []byte, nonce *big.Int, pool []*x509.Certificate) (*timestampResult, error) {
	var resp timeStampResp
	if rest, err := asn1.Unmarshal(response, &resp); err != nil || len(rest) > 0 {
		return nil, timestampError("odpowiedź nie jest TimeStampResp w DER")
	}
	// PKIStatus (RFC 3161 § 2.4.2): 0 granted, 1 grantedWithMods
	if resp.Status.Status != 0 && resp.Status.Status != 1 {
		return nil, timestampError("TSA odmówił: status %d %q, failInfo %x", resp.Status.Status, resp.Status.StatusString, resp.Status.FailInfo.Bytes)
	}
	if len(resp.Token.FullBytes) == 0 {
		return nil, timestampError("odpowiedź bez tokenu")
	}
	token := resp.Token.FullBytes
	sd, signer, certs, err := parseSignedToken(token, oidTSTInfo, pool)
	if err != nil {
		return nil, err
	}
	if !hasExtKeyUsage(signer, x509.ExtKeyUsageTimeStamping) {
		return nil, timestampError("certyfikat %q bez extKeyUsage timeStamping (RFC 3161 § 2.3)", signer.Subject)
	}
	info, err := parseTSTInfo(sd.EncapContentInfo.EContent)
	if err != nil {
		return nil, err
	}
	if !info.imprintAlgorithm.Equal(oidSHA256) || !bytes.Equal(info.imprint, imprint) {
		return nil, timestampError("messageImprint nie jest skrótem SHA-256 wartości podpisu")
	}
	if info.nonce == nil || info.nonce.Cmp(nonce) != 0 {
		return nil, timestampError("nonce odpowiedzi różny od nonce żądania")
	}
	return &timestampResult{token: token, signer: signer, certificates: certs, genTime: info.genTime}, nil
}

// parseSignedToken odczytuje ContentInfo z SignedData o treści typu contentType, sprawdza jedyny
// SignerInfo (atrybuty content-type i message-digest, podpis kluczem certyfikatu) i zwraca
// SignedData, certyfikat podpisującego i certyfikaty z tokenu.
func parseSignedToken(token []byte, contentType asn1.ObjectIdentifier, pool []*x509.Certificate) (*signedDataASN, *x509.Certificate, []*x509.Certificate, error) {
	var ci contentInfoASN
	if rest, err := asn1.Unmarshal(token, &ci); err != nil || len(rest) > 0 || !ci.ContentType.Equal(oidSignedData) {
		return nil, nil, nil, timestampError("token nie jest ContentInfo z SignedData")
	}
	var sd signedDataASN
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return nil, nil, nil, timestampError("SignedData: %v", err)
	}
	if !sd.EncapContentInfo.EContentType.Equal(contentType) || len(sd.EncapContentInfo.EContent) == 0 {
		return nil, nil, nil, timestampError("treść tokenu nie jest typu %v", contentType)
	}
	if len(sd.SignerInfos) != 1 {
		return nil, nil, nil, timestampError("token ma %d SignerInfo zamiast jednego", len(sd.SignerInfos))
	}
	var certs []*x509.Certificate
	if len(sd.Certificates.Bytes) > 0 {
		parsed, err := x509.ParseCertificates(sd.Certificates.Bytes)
		if err != nil {
			return nil, nil, nil, timestampError("certyfikaty tokenu: %v", err)
		}
		certs = parsed
	}
	si := sd.SignerInfos[0]
	signer := findSigner(si.SID, append(append([]*x509.Certificate(nil), certs...), pool...))
	if signer == nil {
		return nil, nil, nil, timestampError("brak certyfikatu podpisującego token (w tokenie ani w Options.ValidationData.Certificates)")
	}
	if len(si.SignedAttrs.Bytes) == 0 {
		return nil, nil, nil, timestampError("SignerInfo bez atrybutów podpisanych")
	}
	hash, ok := hashFor(si.DigestAlgorithm.Algorithm)
	if !ok {
		return nil, nil, nil, timestampError("nieobsługiwany skrót %v", si.DigestAlgorithm.Algorithm)
	}
	attrs, err := parseAttributes(si.SignedAttrs.Bytes)
	if err != nil {
		return nil, nil, nil, err
	}
	var ct asn1.ObjectIdentifier
	if _, err := asn1.Unmarshal(attrs[oidContentType.String()], &ct); err != nil || !ct.Equal(contentType) {
		return nil, nil, nil, timestampError("atrybut content-type różny od typu treści")
	}
	var md []byte
	if _, err := asn1.Unmarshal(attrs[oidMessageDigest.String()], &md); err != nil {
		return nil, nil, nil, timestampError("brak atrybutu message-digest")
	}
	h := hash.New()
	h.Write(sd.EncapContentInfo.EContent)
	if !bytes.Equal(md, h.Sum(nil)) {
		return nil, nil, nil, timestampError("message-digest różny od skrótu treści")
	}
	signed := append([]byte{0x31}, si.SignedAttrs.FullBytes[1:]...)
	alg := x509Algorithm(si.SignatureAlgorithm.Algorithm, si.DigestAlgorithm.Algorithm, signer.PublicKey)
	if alg == x509.UnknownSignatureAlgorithm {
		return nil, nil, nil, timestampError("nieobsługiwany algorytm podpisu %v", si.SignatureAlgorithm.Algorithm)
	}
	if err := signer.CheckSignature(alg, signed, si.Signature); err != nil {
		return nil, nil, nil, timestampError("podpis tokenu niepoprawny: %v", err)
	}
	return &sd, signer, certs, nil
}

// parseAttributes zwraca pierwszą wartość każdego atrybutu (DER) według OID.
func parseAttributes(set []byte) (map[string][]byte, error) {
	out := map[string][]byte{}
	for rest := set; len(rest) > 0; {
		var a attributeASN
		var err error
		if rest, err = asn1.Unmarshal(rest, &a); err != nil {
			return nil, timestampError("atrybuty podpisane: %v", err)
		}
		var first asn1.RawValue
		if _, err := asn1.Unmarshal(a.Values.Bytes, &first); err == nil {
			out[a.Type.String()] = first.FullBytes
		}
	}
	return out, nil
}

// findSigner szuka certyfikatu według SignerIdentifier (RFC 5652 § 5.3): IssuerAndSerialNumber
// albo [0] SubjectKeyIdentifier.
func findSigner(sid asn1.RawValue, certs []*x509.Certificate) *x509.Certificate {
	if sid.Class == asn1.ClassContextSpecific && sid.Tag == 0 {
		for _, c := range certs {
			if len(c.SubjectKeyId) > 0 && bytes.Equal(c.SubjectKeyId, sid.Bytes) {
				return c
			}
		}
		return nil
	}
	var ias issuerAndSerialASN
	if _, err := asn1.Unmarshal(sid.FullBytes, &ias); err != nil || ias.Serial == nil {
		return nil
	}
	for _, c := range certs {
		if bytes.Equal(c.RawIssuer, ias.Issuer.FullBytes) && c.SerialNumber.Cmp(ias.Serial) == 0 {
			return c
		}
	}
	return nil
}

// tstInfo to pola TSTInfo (RFC 3161 § 2.4.2) potrzebne do sprawdzenia.
type tstInfo struct {
	imprintAlgorithm asn1.ObjectIdentifier
	imprint          []byte
	genTime          time.Time
	nonce            *big.Int
}

// parseTSTInfo odczytuje TSTInfo ręcznie: pola opcjonalne accuracy, ordering i nonce nie mają
// znaczników kontekstowych, więc rozpoznaje je znacznik uniwersalny.
func parseTSTInfo(der []byte) (*tstInfo, error) {
	var seq asn1.RawValue
	if rest, err := asn1.Unmarshal(der, &seq); err != nil || len(rest) > 0 || seq.Tag != asn1.TagSequence {
		return nil, timestampError("TSTInfo nie jest SEQUENCE")
	}
	var fields []asn1.RawValue
	for rest := seq.Bytes; len(rest) > 0; {
		var v asn1.RawValue
		var err error
		if rest, err = asn1.Unmarshal(rest, &v); err != nil {
			return nil, timestampError("TSTInfo: %v", err)
		}
		fields = append(fields, v)
	}
	if len(fields) < 5 {
		return nil, timestampError("TSTInfo ma %d pól", len(fields))
	}
	var version int
	if _, err := asn1.Unmarshal(fields[0].FullBytes, &version); err != nil || version != 1 {
		return nil, timestampError("TSTInfo w wersji innej niż 1")
	}
	var imprint struct {
		Algorithm pkix.AlgorithmIdentifier
		Hashed    []byte
	}
	if _, err := asn1.Unmarshal(fields[2].FullBytes, &imprint); err != nil {
		return nil, timestampError("messageImprint: %v", err)
	}
	info := &tstInfo{imprintAlgorithm: imprint.Algorithm.Algorithm, imprint: imprint.Hashed}
	if _, err := asn1.UnmarshalWithParams(fields[4].FullBytes, &info.genTime, "generalized"); err != nil {
		return nil, timestampError("genTime: %v", err)
	}
	for _, f := range fields[5:] {
		if f.Class == asn1.ClassUniversal && f.Tag == asn1.TagInteger {
			info.nonce = new(big.Int)
			if _, err := asn1.Unmarshal(f.FullBytes, &info.nonce); err != nil {
				return nil, timestampError("nonce: %v", err)
			}
		}
	}
	return info, nil
}

func hasExtKeyUsage(cert *x509.Certificate, usage x509.ExtKeyUsage) bool {
	for _, u := range cert.ExtKeyUsage {
		if u == usage {
			return true
		}
	}
	return false
}

// Identyfikatory skrótów i podpisów rozpoznawane przy sprawdzaniu tokenów TSA i odpowiedzi OCSP.
var (
	oidSHA1            = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	oidSHA384          = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512          = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
	oidRSAEncryption   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	oidSHA1WithRSA     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 5}
	oidRSAPSS          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 10}
	oidSHA384WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 12}
	oidSHA512WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 13}
	oidECPublicKey     = asn1.ObjectIdentifier{1, 2, 840, 10045, 2, 1}
	oidECDSAWithSHA384 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
	oidECDSAWithSHA512 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 4}
	oidEd25519         = asn1.ObjectIdentifier{1, 3, 101, 112}
	oidOCSPBasic       = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 1} // RFC 6960 § 4.2.1
	oidOCSPNoCheck     = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 5} // RFC 6960 § 4.2.2.2.1
)

func hashFor(oid asn1.ObjectIdentifier) (crypto.Hash, bool) {
	switch {
	case oid.Equal(oidSHA1):
		return crypto.SHA1, true
	case oid.Equal(oidSHA256):
		return crypto.SHA256, true
	case oid.Equal(oidSHA384):
		return crypto.SHA384, true
	case oid.Equal(oidSHA512):
		return crypto.SHA512, true
	}
	return 0, false
}

// x509Algorithm zamienia algorytm podpisu CMS albo OCSP na x509.SignatureAlgorithm. CMS często podaje
// sam algorytm klucza (rsaEncryption, id-ecPublicKey) i osobno skrót (RFC 5754 § 3).
func x509Algorithm(sigOID, digestOID asn1.ObjectIdentifier, pub crypto.PublicKey) x509.SignatureAlgorithm {
	switch {
	case sigOID.Equal(oidSHA1WithRSA):
		return x509.SHA1WithRSA
	case sigOID.Equal(oidSHA256WithRSA):
		return x509.SHA256WithRSA
	case sigOID.Equal(oidSHA384WithRSA):
		return x509.SHA384WithRSA
	case sigOID.Equal(oidSHA512WithRSA):
		return x509.SHA512WithRSA
	case sigOID.Equal(oidECDSAWithSHA256):
		return x509.ECDSAWithSHA256
	case sigOID.Equal(oidECDSAWithSHA384):
		return x509.ECDSAWithSHA384
	case sigOID.Equal(oidECDSAWithSHA512):
		return x509.ECDSAWithSHA512
	case sigOID.Equal(oidEd25519):
		return x509.PureEd25519
	}
	hash, ok := hashFor(digestOID)
	if !ok {
		return x509.UnknownSignatureAlgorithm
	}
	byHash := func(sha1, sha256, sha384, sha512 x509.SignatureAlgorithm) x509.SignatureAlgorithm {
		switch hash {
		case crypto.SHA1:
			return sha1
		case crypto.SHA256:
			return sha256
		case crypto.SHA384:
			return sha384
		}
		return sha512
	}
	switch {
	case sigOID.Equal(oidRSAEncryption):
		return byHash(x509.SHA1WithRSA, x509.SHA256WithRSA, x509.SHA384WithRSA, x509.SHA512WithRSA)
	case sigOID.Equal(oidRSAPSS):
		return byHash(x509.UnknownSignatureAlgorithm, x509.SHA256WithRSAPSS, x509.SHA384WithRSAPSS, x509.SHA512WithRSAPSS)
	case sigOID.Equal(oidECPublicKey):
		return byHash(x509.ECDSAWithSHA1, x509.ECDSAWithSHA256, x509.ECDSAWithSHA384, x509.ECDSAWithSHA512)
	}
	return x509.UnknownSignatureAlgorithm
}
