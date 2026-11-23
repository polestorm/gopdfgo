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

// Błędy zwracane przez SignPDF.
var (
	// ErrNoSigner: brak Options.Signer albo Options.Certificate.
	ErrNoSigner = errors.New("sign: brak podpisującego albo certyfikatu")
	// ErrUnsupportedKey: klucz certyfikatu nie jest kluczem RSA ani ECDSA.
	ErrUnsupportedKey = errors.New("sign: nieobsługiwany rodzaj klucza")
	// ErrKeyMismatch: klucz podpisującego nie odpowiada certyfikatowi albo podpis nie jest
	// podpisem RSA PKCS#1 v1.5 / ECDSA skrótu SHA-256.
	ErrKeyMismatch = errors.New("sign: podpis nie zgadza się z kluczem publicznym certyfikatu")
	// ErrEncrypted: plik wejściowy jest zaszyfrowany (runda 1 nie podpisuje plików zaszyfrowanych).
	ErrEncrypted = errors.New("sign: plik zaszyfrowany nie jest obsługiwany")
	// ErrNoTSA: poziom B-T albo B-LT bez Options.TSA.
	ErrNoTSA = errors.New("sign: poziom B-T i B-LT wymaga Options.TSA")
	// ErrNoHTTPClient: TSAClient albo RevocationClient bez HTTPClient.
	ErrNoHTTPClient = errors.New("sign: brak HTTPClient (pakiet łączy się tylko przez klienta podanego jawnie)")
	// ErrTimestamp: odpowiedź TSA odmowna albo niepoprawna (status, skrót, nonce, podpis tokenu).
	ErrTimestamp = errors.New("sign: niepoprawny znacznik czasu")
	// ErrReserveTooSmall: token znacznika czasu nie mieści się w miejscu zarezerwowanym w /Contents.
	ErrReserveTooSmall = errors.New("sign: podpis nie mieści się w zarezerwowanym /Contents")
	// ErrNoValidationData: poziom B-LT bez danych o ważności dla któregoś certyfikatu (ani podanych
	// w Options.ValidationData, ani pobranych przez Options.Revocation).
	ErrNoValidationData = errors.New("sign: brak danych o ważności")
	// ErrMissingIssuer: poziom B-LT, a wśród certyfikatów nie ma wystawcy któregoś certyfikatu ścieżki.
	ErrMissingIssuer = errors.New("sign: brak certyfikatu wystawcy")
	// ErrRevoked: dane o ważności podają certyfikat jako unieważniony.
	ErrRevoked = errors.New("sign: certyfikat unieważniony")
	// ErrUnknownField: Options.FieldName nie wskazuje pola podpisu w pliku.
	ErrUnknownField = errors.New("sign: brak pola podpisu o tej nazwie")
	// ErrFieldNotEmpty: pole wskazane przez Options.FieldName jest już podpisane.
	ErrFieldNotEmpty = errors.New("sign: pole podpisu już podpisane")
)

// Level to poziom podpisu PAdES (ETSI EN 319 142-1 § 6).
type Level int

const (
	// LevelBB (domyślny): podpis CMS z certyfikatem podpisującego, bez połączeń sieciowych.
	LevelBB Level = iota
	// LevelBT: LevelBB i znacznik czasu RFC 3161 wartości podpisu od Options.TSA.
	LevelBT
	// LevelBLT: LevelBT i dane o ważności certyfikatów (słownik DSS w drugiej aktualizacji przyrostowej).
	LevelBLT
)

// String zwraca nazwę poziomu według ETSI: B-B, B-T, B-LT.
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

// DefaultTimestampReserve to miejsce na token znacznika czasu w /Contents, gdy
// Options.TimestampReserve jest zerem: token z łańcuchem trzech certyfikatów RSA 4096 ma ok. 7 kB
// (szacunek), więc 12 KiB zostawia zapas ok. 70%.
const DefaultTimestampReserve = 12 << 10

// ContextSigner to podpisujący, który przyjmuje kontekst (np. podpis zdalny przez sieć). Gdy
// Options.Signer go implementuje, SignPDF woła SignContext zamiast Sign, więc anulowanie ctx
// przerywa podpis w trakcie. Wymagania co do wyniku są te same co dla Sign.
type ContextSigner interface {
	crypto.Signer
	SignContext(ctx context.Context, digest []byte, opts crypto.SignerOpts) ([]byte, error)
}

// Options opisuje podpis.
type Options struct {
	// Signer podpisuje skrót SHA-256 atrybutów podpisanych: Sign(rand, digest, crypto.SHA256) ma
	// zwrócić podpis RSA PKCS#1 v1.5 albo ECDSA w DER (Ecdsa-Sig-Value), jak *rsa.PrivateKey
	// i *ecdsa.PrivateKey. Podpisujący zewnętrzny (HSM, karta, podpis zdalny) implementuje
	// crypto.Signer: Public zwraca klucz publiczny certyfikatu, Sign wysyła skrót i zwraca podpis.
	Signer crypto.Signer
	// Certificate to certyfikat podpisującego (RSA albo ECDSA P-256, P-384, P-521).
	Certificate *x509.Certificate
	// Chain to opcjonalne certyfikaty pośrednie (i ewentualnie główny) dołączane do podpisu.
	Chain []*x509.Certificate
	// Time to czas podpisu zapisany w /M słownika podpisu; zero oznacza time.Now(). Stały czas
	// daje powtarzalny wynik przy podpisie deterministycznym (RSA PKCS#1 v1.5).
	Time time.Time
	// Name, Reason, Location i ContactInfo to opcjonalne wpisy słownika podpisu (ISO 32000-1
	// tabela 252): podpisujący, powód, miejsce, kontakt.
	Name, Reason, Location, ContactInfo string
	// FieldName to pełna nazwa istniejącego pustego pola podpisu (np. z atrybutu data-pdf-signature
	// w HTML, ADR 0051), które podpis wypełni: pole dostaje /V, widżet i strona zostają bez zmian.
	// Nazwa nieznana daje ErrUnknownField, pole podpisane ErrFieldNotEmpty. Pusta oznacza nowe
	// niewidoczne pole na pierwszej stronie o pierwszej wolnej nazwie z Signature1, Signature2…
	FieldName string

	// Level to poziom podpisu; zero oznacza LevelBB (bez połączeń sieciowych).
	Level Level
	// TSA to dostawca znaczników czasu (np. TSAClient); wymagany od LevelBT, a przy LevelBB nieużywany.
	TSA TimestampAuthority
	// TimestampReserve to liczba bajtów rezerwowanych w /Contents na token znacznika czasu; zero
	// oznacza DefaultTimestampReserve. Większy token daje ErrReserveTooSmall.
	TimestampReserve int
	// Revocation pobiera brakujące dane o ważności dla LevelBLT (np. RevocationClient); nil oznacza
	// brak połączeń: wtedy wszystkie dane muszą być w ValidationData.
	Revocation RevocationFetcher
	// ValidationData to certyfikaty, odpowiedzi OCSP i listy CRL podane wprost dla LevelBLT.
	ValidationData ValidationData
}

// SignPDF podpisuje plik PDF podpisem PAdES (ETSI EN 319 142-1) na poziomie Options.Level
// i zwraca plik podpisany.
//
// Podpis jest dopisywany jako aktualizacja przyrostowa (ISO 32000-1 § 7.5.6): bajty pliku wejściowego
// zostają bez zmian, więc wcześniejsze podpisy pozostają ważne. Aktualizacja dodaje niewidoczne pole
// podpisu na pierwszej stronie (albo wypełnia /V istniejącego pustego pola Options.FieldName), słownik
// podpisu z /SubFilter /ETSI.CAdES.detached i /AcroForm z /SigFlags 3. Od LevelBT podpis CMS ma atrybut niepodpisany signature-time-stamp-token z tokenem
// od Options.TSA; LevelBLT dopisuje drugą aktualizację ze słownikiem DSS (certyfikaty, OCSP, CRL),
// której podpis nie obejmuje (tak przewiduje ISO 32000-2 § 12.8.4.3). ctx przerywa podpis
// (ContextSigner) i połączenia sieciowe.
func SignPDF(ctx context.Context, pdf []byte, opts Options) ([]byte, error) {
	if opts.Signer == nil || opts.Certificate == nil {
		return nil, ErrNoSigner
	}
	if opts.Level < LevelBB || opts.Level > LevelBLT {
		return nil, fmt.Errorf("sign: nieznany poziom %v", opts.Level)
	}
	if opts.Level >= LevelBT && opts.TSA == nil {
		return nil, ErrNoTSA
	}
	if opts.Level == LevelBLT && opts.Revocation == nil &&
		len(opts.ValidationData.OCSPResponses) == 0 && len(opts.ValidationData.CRLs) == 0 {
		return nil, fmt.Errorf("%w: poziom B-LT wymaga Options.Revocation albo odpowiedzi OCSP lub list CRL w Options.ValidationData", ErrNoValidationData)
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
		return nil, errors.New("sign: plik za duży na zarezerwowane /ByteRange")
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
			return nil, fmt.Errorf("%w: token znacznika czasu ma %d B, zarezerwowano %d B; zwiększ Options.TimestampReserve",
				ErrReserveTooSmall, len(ts.token), tokenReserve)
		}
		unsigned = unsignedAttributes(ts.token)
	}
	signature := buildSignedData(signed.attrs, signed.signature, alg, opts.Certificate, opts.Chain, unsigned)
	if len(signature) > reserve {
		return nil, fmt.Errorf("%w: podpis CMS ma %d B, zarezerwowano %d B", ErrReserveTooSmall, len(signature), reserve)
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
		return nil, malformed("trailer bez /Root")
	}
	rootObj, err := f.resolve(rootRef)
	if err != nil {
		return nil, err
	}
	root, ok := rootObj.(*pdfDict)
	if !ok {
		return nil, malformed("katalog nie jest słownikiem")
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
			return pdfRef{}, nil, malformed("węzeł drzewa stron nie jest obiektem pośrednim")
		}
		obj, err := f.resolve(ref)
		if err != nil {
			return pdfRef{}, nil, err
		}
		dict, ok := obj.(*pdfDict)
		if !ok {
			return pdfRef{}, nil, malformed("węzeł drzewa stron nie jest słownikiem")
		}
		if dict.get("Type") == pdfName("Page") {
			return ref, dict, nil
		}
		kids, _ := dict.get("Kids").(pdfArray)
		if len(kids) == 0 {
			return pdfRef{}, nil, malformed("dokument bez stron")
		}
		node = kids[0]
	}
	return pdfRef{}, nil, malformed("drzewo stron za głębokie")
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
