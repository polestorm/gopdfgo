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
)

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
	// FieldName to nazwa pola podpisu (/T); pusta oznacza pierwszą wolną z Signature1, Signature2…
	FieldName string
}

// SignPDF podpisuje plik PDF podpisem PAdES B-B (ETSI EN 319 142-1) i zwraca plik podpisany.
//
// Podpis jest dopisywany jako aktualizacja przyrostowa (ISO 32000-1 § 7.5.6): bajty pliku wejściowego
// zostają bez zmian, więc wcześniejsze podpisy pozostają ważne. Aktualizacja dodaje niewidoczne pole
// podpisu na pierwszej stronie, słownik podpisu z /SubFilter /ETSI.CAdES.detached i /AcroForm
// z /SigFlags 3. ctx jest sprawdzany przed podpisaniem skrótu.
func SignPDF(ctx context.Context, pdf []byte, opts Options) ([]byte, error) {
	if opts.Signer == nil || opts.Certificate == nil {
		return nil, ErrNoSigner
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
	reserve := cmsSizeLimit(alg, opts.Certificate, opts.Chain) + 32
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
	signature, err := signCMS(digest.Sum(nil), opts.Signer, alg, opts.Certificate, opts.Chain)
	if err != nil {
		return nil, err
	}
	if len(signature) > reserve {
		return nil, errors.New("sign: podpis CMS dłuższy niż zarezerwowane /Contents")
	}
	hex.Encode(out[contentsAt+1:], signature)
	return out, nil
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
	flags := max(f.intValue(form.get("SigFlags")), 0)
	form.set("SigFlags", pdfNumber(strconv.Itoa(flags|3))) // SignaturesExist | AppendOnly (tabela 219)

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

	name := opts.FieldName
	for i := 1; name == ""; i++ {
		if candidate := "Signature" + strconv.Itoa(i); !names[candidate] {
			name = candidate
		}
	}
	if names[name] {
		return nil, fmt.Errorf("sign: pole %q już istnieje", name)
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

	// słownik podpisu (§ 12.8.1, tabela 252; ETSI EN 319 142-1 § 5.3)
	var sig bytes.Buffer
	sig.WriteString("<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /ETSI.CAdES.detached ")
	sig.WriteString(byteRangeKey + "0 0 0 0" + string(bytes.Repeat([]byte(" "), byteRangeWidth-7)) + "] ")
	sig.WriteString(contentsKey[:len(contentsKey)-1])
	sig.WriteByte('<')
	sig.Write(bytes.Repeat([]byte("0"), 2*reserve))
	sig.WriteString("> /M ")
	writeObject(&sig, pdfString(pdfDate(when)))
	for _, entry := range []struct{ key, value string }{
		{"Name", opts.Name}, {"Reason", opts.Reason}, {"Location", opts.Location}, {"ContactInfo", opts.ContactInfo},
	} {
		if entry.value != "" {
			sig.WriteString(" /" + entry.key + " ")
			writeObject(&sig, textString(entry.value))
		}
	}
	sig.WriteString(" >>")

	u.objects = append(u.objects, updateObject{ref: sigRef, body: sig.Bytes()})
	var b bytes.Buffer
	writeObject(&b, field)
	u.objects = append(u.objects, updateObject{ref: fieldRef, body: b.Bytes()})
	for num, ref := range changed {
		var b bytes.Buffer
		writeObject(&b, values[num])
		u.objects = append(u.objects, updateObject{ref: ref, body: b.Bytes()})
	}

	// trailer aktualizacji (§ 7.5.6): /Prev, te same /Root i /Info, /ID z nowym drugim elementem (§ 14.4)
	u.trailer = newDict()
	u.trailer.set("Size", pdfNumber("0"))
	u.trailer.set("Root", rootRef)
	if info := f.trailer.get("Info"); info != nil {
		u.trailer.set("Info", info)
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
	u.trailer.set("ID", pdfArray{first, pdfString(second[:16])})
	u.trailer.set("Prev", pdfNumber(strconv.FormatInt(f.startxref, 10)))
	return u, nil
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
