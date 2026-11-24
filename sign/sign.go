package sign

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Errors returned by SignPDF.
var (
	// ErrNoSigner: Options.Signer or Options.Certificate is missing.
	ErrNoSigner = errors.New("sign: no signer or certificate")
	// ErrUnsupportedKey: the certificate's key is neither an RSA nor an ECDSA key.
	ErrUnsupportedKey = errors.New("sign: unsupported key type")
	// ErrKeyMismatch: the signer's key does not match the certificate, or the signature is not
	// an RSA PKCS#1 v1.5 / ECDSA signature of the SHA-256 digest.
	ErrKeyMismatch = errors.New("sign: signature does not match the certificate's public key")
	// ErrEncrypted: the input file is encrypted (round 1 does not sign encrypted files).
	ErrEncrypted = errors.New("sign: encrypted files are not supported")
	// ErrNoTSA: level B-T or B-LT without Options.TSA.
	ErrNoTSA = errors.New("sign: levels B-T and B-LT require Options.TSA")
	// ErrNoHTTPClient: a TSAClient or RevocationClient without HTTPClient.
	ErrNoHTTPClient = errors.New("sign: no HTTPClient (the package connects only through a client passed explicitly)")
	// ErrTimestamp: the TSA response is a refusal or is invalid (status, digest, nonce, token signature).
	ErrTimestamp = errors.New("sign: invalid timestamp")
	// ErrReserveTooSmall: the timestamp token does not fit in the space reserved in /Contents.
	ErrReserveTooSmall = errors.New("sign: signature does not fit in the reserved /Contents")
	// ErrNoValidationData: level B-LT without validation data for one of the certificates (neither given
	// in Options.ValidationData nor fetched through Options.Revocation).
	ErrNoValidationData = errors.New("sign: no validation data")
	// ErrMissingIssuer: level B-LT, and the issuer of one of the path's certificates is not among the certificates.
	ErrMissingIssuer = errors.New("sign: no issuer certificate")
	// ErrRevoked: the validation data reports the certificate as revoked.
	ErrRevoked = errors.New("sign: certificate revoked")
	// ErrUnknownField: Options.FieldName does not name a signature field in the file.
	ErrUnknownField = errors.New("sign: no signature field with this name")
	// ErrFieldNotEmpty: the field named by Options.FieldName is already signed.
	ErrFieldNotEmpty = errors.New("sign: signature field already signed")
)

// Level is the PAdES signature level (ETSI EN 319 142-1 § 6).
type Level int

const (
	// LevelBB (the default): a CMS signature with the signer's certificate, no network connections.
	LevelBB Level = iota
	// LevelBT: LevelBB plus an RFC 3161 timestamp of the signature value from Options.TSA.
	LevelBT
	// LevelBLT: LevelBT plus certificate validation data (a DSS dictionary in a second incremental update).
	LevelBLT
)

// String returns the name of the level according to ETSI: B-B, B-T, B-LT.
func (l Level) String() string {
	switch l {
	case LevelBB:
		return "B-B"
	case LevelBT:
		return "B-T"
	case LevelBLT:
		return "B-LT"
	}
	return "Level(" + strconv.Itoa(int(l)) + ")"
}

// DefaultTimestampReserve is the space for the timestamp token in /Contents when
// Options.TimestampReserve is zero: a token with a chain of three RSA 4096 certificates is about 7 kB
// (an estimate), so 12 KiB leaves a margin of about 70%.
const DefaultTimestampReserve = 12 << 10

// ContextSigner is a signer that accepts a context (e.g. a remote signature over the network). When
// Options.Signer implements it, SignPDF calls SignContext instead of Sign, so cancelling ctx
// aborts the signing in progress. The requirements for the result are the same as for Sign.
type ContextSigner interface {
	crypto.Signer
	SignContext(ctx context.Context, digest []byte, opts crypto.SignerOpts) ([]byte, error)
}

// Options describes the signature.
type Options struct {
	// Signer signs the SHA-256 digest of the signed attributes: Sign(rand, digest, crypto.SHA256) must
	// return an RSA PKCS#1 v1.5 signature or an ECDSA one in DER (Ecdsa-Sig-Value), as *rsa.PrivateKey
	// and *ecdsa.PrivateKey do. An external signer (HSM, smart card, remote signature) implements
	// crypto.Signer: Public returns the certificate's public key, Sign sends the digest and returns the signature.
	Signer crypto.Signer
	// Certificate is the signer's certificate (RSA or ECDSA P-256, P-384, P-521).
	Certificate *x509.Certificate
	// Chain is the optional intermediate certificates (and possibly the root) attached to the signature.
	Chain []*x509.Certificate
	// Time is the signing time written in /M of the signature dictionary; zero means time.Now(). A fixed
	// time gives a reproducible output with a deterministic signature (RSA PKCS#1 v1.5).
	Time time.Time
	// Name, Reason, Location and ContactInfo are optional entries of the signature dictionary (ISO 32000-1
	// table 252): the signer, the reason, the place, the contact.
	Name, Reason, Location, ContactInfo string
	// FieldName is the full name of an existing empty signature field (e.g. from the data-pdf-signature
	// attribute in HTML, ADR 0051) that the signature will fill: the field gets /V, the widget and the page stay
	// unchanged. An unknown name gives ErrUnknownField, a signed field ErrFieldNotEmpty. Empty means a new
	// invisible field on the first page with the first free name among Signature1, Signature2…
	FieldName string

	// Level is the signature level; zero means LevelBB (no network connections).
	Level Level
	// TSA is the timestamp provider (e.g. a TSAClient); required from LevelBT up and unused at LevelBB.
	TSA TimestampAuthority
	// TimestampReserve is the number of bytes reserved in /Contents for the timestamp token; zero
	// means DefaultTimestampReserve. A larger token gives ErrReserveTooSmall.
	TimestampReserve int
	// Revocation fetches the missing validation data for LevelBLT (e.g. a RevocationClient); nil means
	// no connections: all the data must then be in ValidationData.
	Revocation RevocationFetcher
	// ValidationData is certificates, OCSP responses and CRLs given directly for LevelBLT.
	ValidationData ValidationData
}

// SignPDF signs a PDF file with a PAdES signature (ETSI EN 319 142-1) at the level Options.Level
// and returns the signed file.
//
// The signature is appended as an incremental update (ISO 32000-1 § 7.5.6): the bytes of the input file
// stay unchanged, so earlier signatures remain valid. The update adds an invisible signature field
// on the first page (or fills /V of the existing empty field Options.FieldName), a signature dictionary
// with /SubFilter /ETSI.CAdES.detached and an /AcroForm with /SigFlags 3. From LevelBT up the CMS signature has
// the unsigned attribute signature-time-stamp-token with a token from Options.TSA; LevelBLT appends a second
// update with the DSS dictionary (certificates, OCSP, CRL), which the signature does not cover (as
// ISO 32000-2 § 12.8.4.3 provides). ctx aborts the signing (ContextSigner) and the network connections.
func SignPDF(ctx context.Context, pdf []byte, opts Options) ([]byte, error) {
	if opts.Signer == nil || opts.Certificate == nil {
		return nil, ErrNoSigner
	}
	if opts.Level < LevelBB || opts.Level > LevelBLT {
		return nil, fmt.Errorf("sign: unknown level %v", opts.Level)
	}
	if opts.Level >= LevelBT && opts.TSA == nil {
		return nil, ErrNoTSA
	}
	if opts.Level == LevelBLT && opts.Revocation == nil &&
		len(opts.ValidationData.OCSPResponses) == 0 && len(opts.ValidationData.CRLs) == 0 {
		return nil, fmt.Errorf("%w: level B-LT requires Options.Revocation, or OCSP responses or CRLs in Options.ValidationData", ErrNoValidationData)
	}
	alg, err := algorithmFor(opts.Certificate.PublicKey)
	if err != nil {
		return nil, err
	}
	if pub, ok := opts.Signer.Public().(interface{ Equal(crypto.PublicKey) bool }); !ok || !pub.Equal(opts.Certificate.PublicKey) {
		return nil, ErrKeyMismatch
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	when := opts.Time
	if when.IsZero() {
		when = time.Now()
	}
	f, err := readPDF(pdf)
	if err != nil {
		return nil, err
	}
	if f.trailer.get("Encrypt") != nil {
		return nil, ErrEncrypted
	}
	tokenReserve := 0
	if opts.Level >= LevelBT {
		tokenReserve = opts.TimestampReserve
		if tokenReserve <= 0 {
			tokenReserve = DefaultTimestampReserve
		}
	}
	reserve := cmsSizeLimit(alg, opts.Certificate, opts.Chain, tokenReserve) + 32
	update, err := prepareUpdate(f, opts, when, reserve)
	if err != nil {
		return nil, err
	}
	out, bodies := appendUpdate(f, update.objects, update.trailer)

	// położenie /Contents i /ByteRange w zapisanym słowniku podpisu
	sigStart := bodies[update.sigRef.num]
	contentsAt := sigStart + bytes.Index(out[sigStart:], []byte(contentsKey)) + len(contentsKey) - 1
	contentsEnd := contentsAt + 2 + 2*reserve
	rangeAt := sigStart + bytes.Index(out[sigStart:], []byte(byteRangeKey)) + len(byteRangeKey)
	byteRange := fmt.Sprintf("0 %d %d %d", contentsAt, contentsEnd, len(out)-contentsEnd)
	if len(byteRange) > byteRangeWidth {
		return nil, errors.New("sign: file too large for the reserved /ByteRange")
	}
	copy(out[rangeAt:], byteRange)

	digest := sha256.New()
	digest.Write(out[:contentsAt])
	digest.Write(out[contentsEnd:])
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	signed, err := signAttributes(ctx, digest.Sum(nil), opts.Signer, alg, opts.Certificate)
	if err != nil {
		return nil, err
	}
	var ts *timestampResult
	var unsigned []byte
	if opts.Level >= LevelBT {
		pool := append(append([]*x509.Certificate(nil), opts.ValidationData.Certificates...), opts.Chain...)
		if ts, err = requestTimestamp(ctx, opts.TSA, signed.signature, pool); err != nil {
			return nil, err
		}
		if len(ts.token) > tokenReserve {
			return nil, fmt.Errorf("%w: timestamp token has %d B, %d B reserved; increase Options.TimestampReserve",
				ErrReserveTooSmall, len(ts.token), tokenReserve)
		}
		unsigned = unsignedAttributes(ts.token)
	}
	signature := buildSignedData(signed.attrs, signed.signature, alg, opts.Certificate, opts.Chain, unsigned)
	if len(signature) > reserve {
		return nil, fmt.Errorf("%w: CMS signature has %d B, %d B reserved", ErrReserveTooSmall, len(signature), reserve)
	}
	hex.Encode(out[contentsAt+1:], signature)
	if opts.Level < LevelBLT {
		return out, nil
	}
	data, err := collectValidation(ctx, opts, ts)
	if err != nil {
		return nil, err
	}
	contents := make([]byte, reserve)
	copy(contents, signature)
	return appendDSS(out, contents, data, when)
}

const (
	contentsKey    = "/Contents <"
	byteRangeKey   = "/ByteRange ["
	byteRangeWidth = 4*11 + 3 // cztery liczby do 11 cyfr ze spacjami
)

// update to obiekty i trailer aktualizacji.
type update struct {
	objects []updateObject
	trailer *pdfDict
	sigRef  pdfRef
}

// prepareUpdate buduje obiekty aktualizacji: słownik podpisu, pole podpisu z adnotacją widżetu,
// przepisaną stronę (albo jej tablicę /Annots), katalog (albo słownik /AcroForm i tablicę /Fields).
func prepareUpdate(f *pdfFile, opts Options, when time.Time, reserve int) (*update, error) {
	rootRef, ok := f.trailer.get("Root").(pdfRef)
	if !ok {
		return nil, malformed("trailer without /Root")
	}
	rootObj, err := f.resolve(rootRef)
	if err != nil {
		return nil, err
	}
	root, ok := rootObj.(*pdfDict)
	if !ok {
		return nil, malformed("catalog is not a dictionary")
	}
	pageRef, page, err := firstPage(f, root)
	if err != nil {
		return nil, err
	}
	u := &update{}
	changed := map[int]pdfRef{} // obiekty przepisywane, numer → odwołanie
	values := map[int]pdfObject{}
	rewrite := func(ref pdfRef, obj pdfObject) {
		changed[ref.num] = ref
		values[ref.num] = obj
	}

	sigRef := pdfRef{num: f.size}
	fieldRef := pdfRef{num: f.size + 1}
	u.sigRef = sigRef

	// /AcroForm: bezpośredni w katalogu, pośredni albo nowy (ISO 32000-1 § 12.7.2)
	var form *pdfDict
	formHolder := pdfRef{}
	switch v := root.get("AcroForm").(type) {
	case pdfRef:
		obj, err := f.resolve(v)
		if err != nil {
			return nil, err
		}
		if d, ok := obj.(*pdfDict); ok {
			form, formHolder = d, v
		}
	case *pdfDict:
		form = v
	}
	if form == nil {
		form = newDict()
		root.set("AcroForm", form)
	}
	if formHolder.num == 0 {
		rewrite(rootRef, root)
	} else {
		rewrite(formHolder, form)
	}
	// SignaturesExist | AppendOnly (tabela 219), za /Fields nowego słownika /AcroForm
	setSigFlags := func() {
		flags := max(f.intValue(form.get("SigFlags")), 0)
		form.set("SigFlags", pdfNumber(strconv.Itoa(flags|3)))
	}

	// pole wskazane nazwą: istniejące puste pole podpisu (np. z atrybutu data-pdf-signature, ADR 0051)
	// dostaje /V, a jego widżet i strona zostają bez zmian
	if opts.FieldName != "" {
		ref, field, err := findField(f, form, opts.FieldName)
		if err != nil {
			return nil, err
		}
		setSigFlags()
		field.set("V", sigRef)
		rewrite(ref, field)
		u.objects = append(u.objects, updateObject{ref: sigRef, body: signatureDict(opts, when, reserve)})
		for num, ref := range changed {
			var b bytes.Buffer
			writeObject(&b, values[num])
			u.objects = append(u.objects, updateObject{ref: ref, body: b.Bytes()})
		}
		u.trailer = updateTrailer(f, rootRef, when)
		return u, nil
	}

	names := map[string]bool{}
	var fields pdfArray
	switch v := form.get("Fields").(type) {
	case pdfRef:
		obj, err := f.resolve(v)
		if err != nil {
			return nil, err
		}
		fields, _ = obj.(pdfArray)
		fields = append(fields, fieldRef)
		rewrite(v, fields)
	case pdfArray:
		fields = v
		form.set("Fields", append(v, fieldRef))
	default:
		form.set("Fields", pdfArray{fieldRef})
	}
	for _, field := range fields {
		obj, _ := f.resolve(field)
		if d, ok := obj.(*pdfDict); ok {
			if t, ok := d.get("T").(pdfString); ok {
				names[string(t)] = true
			}
		}
	}
	setSigFlags()

	// /Annots strony: bezpośrednia tablica, pośrednia albo nowa
	switch v := page.get("Annots").(type) {
	case pdfRef:
		obj, err := f.resolve(v)
		if err != nil {
			return nil, err
		}
		annots, _ := obj.(pdfArray)
		rewrite(v, append(annots, fieldRef))
	case pdfArray:
		page.set("Annots", append(v, fieldRef))
		rewrite(pageRef, page)
	default:
		page.set("Annots", pdfArray{fieldRef})
		rewrite(pageRef, page)
	}
	// strona z adnotacjami w pliku z tagami: kolejność tabulacji według struktury (PDF/UA-1 § 7.18.3)
	if root.get("StructTreeRoot") != nil && page.get("Tabs") == nil {
		page.set("Tabs", pdfName("S"))
		rewrite(pageRef, page)
	}

	name := ""
	for i := 1; name == ""; i++ {
		if candidate := "Signature" + strconv.Itoa(i); !names[candidate] {
			name = candidate
		}
	}

	// pole podpisu połączone z adnotacją widżetu (§ 12.7.4.5, 12.5.6.19); prostokąt zerowy oznacza
	// podpis niewidoczny, flagi Print i Locked (PDF/A-2 § 6.3.2 wymaga Print)
	field := newDict()
	field.set("Type", pdfName("Annot"))
	field.set("Subtype", pdfName("Widget"))
	field.set("FT", pdfName("Sig"))
	field.set("T", textString(name))
	field.set("V", sigRef)
	field.set("F", pdfNumber("132"))
	field.set("Rect", pdfArray{pdfNumber("0"), pdfNumber("0"), pdfNumber("0"), pdfNumber("0")})
	field.set("P", pageRef)

	u.objects = append(u.objects, updateObject{ref: sigRef, body: signatureDict(opts, when, reserve)})
	var b bytes.Buffer
	writeObject(&b, field)
	u.objects = append(u.objects, updateObject{ref: fieldRef, body: b.Bytes()})
	for num, ref := range changed {
		var b bytes.Buffer
		writeObject(&b, values[num])
		u.objects = append(u.objects, updateObject{ref: ref, body: b.Bytes()})
	}

	u.trailer = updateTrailer(f, rootRef, when)
	return u, nil
}

// updateTrailer buduje trailer aktualizacji (§ 7.5.6): /Prev, te same /Root i /Info, /ID z nowym
// drugim elementem (§ 14.4) wyliczonym z pierwszego, długości pliku i czasu.
func updateTrailer(f *pdfFile, rootRef pdfRef, when time.Time) *pdfDict {
	trailer := newDict()
	trailer.set("Size", pdfNumber("0"))
	trailer.set("Root", rootRef)
	if info := f.trailer.get("Info"); info != nil {
		trailer.set("Info", info)
	}
	first := pdfString(nil)
	if id, ok := f.trailer.get("ID").(pdfArray); ok && len(id) == 2 {
		first, _ = id[0].(pdfString)
	}
	if len(first) == 0 {
		sum := sha256.Sum256(f.data)
		first = sum[:16]
	}
	second := sha256.Sum256(fmt.Appendf(append([]byte(nil), first...), "%d %s", len(f.data), pdfDate(when)))
	trailer.set("ID", pdfArray{first, pdfString(second[:16])})
	trailer.set("Prev", pdfNumber(strconv.FormatInt(f.startxref, 10)))
	return trailer
}

// firstPage zwraca pierwszą stronę drzewa stron (§ 7.7.3).
func firstPage(f *pdfFile, root *pdfDict) (pdfRef, *pdfDict, error) {
	node := root.get("Pages")
	for depth := 0; depth < 64; depth++ {
		ref, ok := node.(pdfRef)
		if !ok {
			return pdfRef{}, nil, malformed("page tree node is not an indirect object")
		}
		obj, err := f.resolve(ref)
		if err != nil {
			return pdfRef{}, nil, err
		}
		dict, ok := obj.(*pdfDict)
		if !ok {
			return pdfRef{}, nil, malformed("page tree node is not a dictionary")
		}
		if dict.get("Type") == pdfName("Page") {
			return ref, dict, nil
		}
		kids, _ := dict.get("Kids").(pdfArray)
		if len(kids) == 0 {
			return pdfRef{}, nil, malformed("document has no pages")
		}
		node = kids[0]
	}
	return pdfRef{}, nil, malformed("page tree too deep")
}

// pdfDate zapisuje czas w formie daty PDF (§ 7.9.4): D:RRRRMMDDGGmmSS+HH'mm'.
func pdfDate(t time.Time) string {
	_, offset := t.Zone()
	sign := '+'
	if offset < 0 {
		sign, offset = '-', -offset
	}
	return fmt.Sprintf("D:%s%c%02d'%02d'", t.Format("20060102150405"), sign, offset/3600, offset%3600/60)
}
