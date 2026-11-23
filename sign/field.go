package sign

// Słownik podpisu i wyszukiwanie istniejącego pustego pola podpisu (ADR 0051).

import (
	"bytes"
	"fmt"
	"time"
	"unicode/utf16"
)

// signatureDict zapisuje słownik podpisu (ISO 32000-1 § 12.8.1, tabela 252; ETSI EN 319 142-1 § 5.3)
// z miejscem na /ByteRange i /Contents.
func signatureDict(opts Options, when time.Time, reserve int) []byte {
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
	return sig.Bytes()
}

// findField szuka w drzewie pól /AcroForm pola o pełnej nazwie name (nazwy częściowe /T połączone
// kropkami, ISO 32000-1 § 12.7.3.2). Pole ma być obiektem pośrednim, polem podpisu (/FT /Sig, także
// dziedziczone po rodzicu) i nie mieć wartości /V; inaczej ErrUnknownField albo ErrFieldNotEmpty.
func findField(f *pdfFile, form *pdfDict, name string) (pdfRef, *pdfDict, error) {
	fieldsObj, err := f.resolve(form.get("Fields"))
	if err != nil {
		return pdfRef{}, nil, err
	}
	fields, _ := fieldsObj.(pdfArray)
	type entry struct {
		ref    pdfRef
		prefix string
		ft     pdfObject
		depth  int
	}
	var stack []entry
	for i := len(fields) - 1; i >= 0; i-- {
		if ref, ok := fields[i].(pdfRef); ok {
			stack = append(stack, entry{ref: ref})
		}
	}
	seen := map[int]bool{}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[e.ref.num] || e.depth > 32 {
			continue
		}
		seen[e.ref.num] = true
		obj, err := f.resolve(e.ref)
		if err != nil {
			return pdfRef{}, nil, err
		}
		field, ok := obj.(*pdfDict)
		if !ok {
			continue
		}
		full := e.prefix
		t, named := field.get("T").(pdfString)
		if named {
			if full != "" {
				full += "."
			}
			full += decodeText(t)
		}
		ft := e.ft
		if v := field.get("FT"); v != nil {
			ft = v
		}
		if named && full == name {
			if ft != pdfName("Sig") {
				return pdfRef{}, nil, fmt.Errorf("%w: pole %q nie jest polem podpisu", ErrUnknownField, name)
			}
			if v := field.get("V"); v != nil && v != (pdfNull{}) {
				return pdfRef{}, nil, fmt.Errorf("%w: pole %q", ErrFieldNotEmpty, name)
			}
			return e.ref, field, nil
		}
		kidsObj, err := f.resolve(field.get("Kids"))
		if err != nil {
			return pdfRef{}, nil, err
		}
		kids, _ := kidsObj.(pdfArray)
		for i := len(kids) - 1; i >= 0; i-- {
			if ref, ok := kids[i].(pdfRef); ok {
				stack = append(stack, entry{ref: ref, prefix: full, ft: ft, depth: e.depth + 1})
			}
		}
	}
	return pdfRef{}, nil, fmt.Errorf("%w: %q", ErrUnknownField, name)
}

// decodeText rozkodowuje ciąg tekstowy (ISO 32000-1 § 7.9.2.2): UTF-16BE ze znacznikiem BOM, UTF-8
// ze znacznikiem (ISO 32000-2) albo PDFDocEncoding, przybliżone tu przez Latin-1 (zgodne dla ASCII).
func decodeText(s pdfString) string {
	switch {
	case len(s) >= 2 && s[0] == 0xfe && s[1] == 0xff:
		units := make([]uint16, 0, len(s)/2)
		for i := 2; i+1 < len(s); i += 2 {
			units = append(units, uint16(s[i])<<8|uint16(s[i+1]))
		}
		return string(utf16.Decode(units))
	case len(s) >= 3 && s[0] == 0xef && s[1] == 0xbb && s[2] == 0xbf:
		return string(s[3:])
	}
	runes := make([]rune, len(s))
	for i, c := range s {
		runes[i] = rune(c)
	}
	return string(runes)
}
