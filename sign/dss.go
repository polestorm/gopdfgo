package sign

// Słownik DSS (Document Security Store, ISO 32000-2 § 12.8.4.3; ETSI EN 319 142-1 § 5.4) w osobnej
// aktualizacji przyrostowej po podpisie (PAdES B-LT). Certyfikaty, odpowiedzi OCSP i listy CRL są
// strumieniami bez filtra; /VRI (§ 12.8.4.4) dostaje wpis dla nowego podpisu z kluczem będącym
// skrótem SHA-1 ciągu /Contents (wielkie litery szesnastkowe). Istniejący DSS (np. z wcześniejszego
// podpisu) jest uzupełniany: dotychczasowe strumienie i wpisy /VRI zostają, nowe dane bez powtórzeń.
// Bez /TU w /VRI, żeby aktualizacja nie zależała od zegara.

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// appendDSS dopisuje do podpisanego pliku aktualizację ze słownikiem DSS. contents to bajty ciągu
// /Contents nowego podpisu (podpis CMS z dopełnieniem zerami).
func appendDSS(signed, contents []byte, data *validationSet, when time.Time) ([]byte, error) {
	f, err := readPDF(signed)
	if err != nil {
		return nil, err
	}
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

	var objects []updateObject
	next := f.size
	add := func(body []byte) pdfRef {
		ref := pdfRef{num: next}
		next++
		objects = append(objects, updateObject{ref: ref, body: body})
		return ref
	}

	// istniejący DSS: pośredni (przepisywany pod tym samym numerem) albo bezpośredni w katalogu
	dss := newDict()
	dssRef := pdfRef{}
	switch v := root.get("DSS").(type) {
	case pdfRef:
		obj, err := f.resolve(v)
		if err != nil {
			return nil, err
		}
		if d, ok := obj.(*pdfDict); ok {
			dss, dssRef = d, v
		}
	case *pdfDict:
		dss = v
	}
	if dss.get("Type") == nil {
		dss.set("Type", pdfName("DSS"))
	}

	// strumienie: dotychczasowe (rozpoznane po treści, żeby nie powtarzać danych) i nowe
	merge := func(key pdfName, items [][]byte) (pdfArray, error) {
		obj, err := f.resolve(dss.get(key))
		if err != nil {
			return nil, err
		}
		existing, _ := obj.(pdfArray)
		known := map[string]pdfRef{}
		for _, item := range existing {
			ref, ok := item.(pdfRef)
			if !ok {
				continue
			}
			if s, err := f.resolve(ref); err == nil {
				if stream, ok := s.(*pdfStream); ok {
					if decoded, err := f.decodeStream(stream); err == nil {
						known[string(decoded)] = ref
					}
				}
			}
		}
		all := append(pdfArray(nil), existing...)
		var refs pdfArray
		for _, der := range items {
			ref, ok := known[string(der)]
			if !ok {
				ref = add(streamBody(der))
				known[string(der)] = ref
				all = append(all, ref)
			}
			refs = append(refs, ref)
		}
		if len(all) > 0 {
			dss.set(key, all)
		}
		return refs, nil
	}
	certs, err := merge("Certs", data.certs)
	if err != nil {
		return nil, err
	}
	ocsps, err := merge("OCSPs", data.ocsps)
	if err != nil {
		return nil, err
	}
	crls, err := merge("CRLs", data.crls)
	if err != nil {
		return nil, err
	}

	// /VRI: słownik podpisów; wpis nowego podpisu wskazuje jego dane (tabela 262)
	vri := newDict()
	if obj, err := f.resolve(dss.get("VRI")); err == nil {
		if d, ok := obj.(*pdfDict); ok {
			vri = d
		}
	}
	entry := newDict()
	for _, part := range []struct {
		key  pdfName
		refs pdfArray
	}{{"Cert", certs}, {"OCSP", ocsps}, {"CRL", crls}} {
		if len(part.refs) > 0 {
			entry.set(part.key, part.refs)
		}
	}
	sum := sha1.Sum(contents)
	vri.set(pdfName(strings.ToUpper(hex.EncodeToString(sum[:]))), entry)
	dss.set("VRI", vri)

	var b bytes.Buffer
	writeObject(&b, dss)
	if dssRef.num != 0 {
		objects = append(objects, updateObject{ref: dssRef, body: b.Bytes()})
	} else {
		root.set("DSS", add(b.Bytes()))
		var rb bytes.Buffer
		writeObject(&rb, root)
		objects = append(objects, updateObject{ref: rootRef, body: rb.Bytes()})
	}
	out, _ := appendUpdate(f, objects, updateTrailer(f, rootRef, when))
	return out, nil
}

// streamBody zapisuje strumień bez filtra (§ 7.3.8).
func streamBody(data []byte) []byte {
	var b bytes.Buffer
	b.WriteString("<< /Length " + strconv.Itoa(len(data)) + " >>\nstream\n")
	b.Write(data)
	b.WriteString("\nendstream")
	return b.Bytes()
}
