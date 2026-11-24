package sign

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo/htmlpdf"
)

// Podpis w polu wskazanym w HTML atrybutem data-pdf-signature (ADR 0051): SignPDF z Options.FieldName
// wypełnia /V istniejącego pustego pola; widżet, strona i lista pól zostają bez zmian.

const visibleHTML = `<!DOCTYPE html><html lang="pl"><head><title>Umowa</title>
<style>.sig { width: 6cm; height: 2cm; border: 1px solid #000; margin-top: 1cm }</style></head>
<body><h1>Umowa</h1><p>Treść dokumentu do podpisu: zażółć gęślą jaźń.</p>
<div class="sig" data-pdf-signature="Wykonawca" title="Podpis wykonawcy">Wykonawca</div>
<p style="break-before: page">Druga strona.</p>
<div class="sig" data-pdf-signature="Zamawiający" aria-label="Podpis zamawiającego">Zamawiający</div>
</body></html>`

func visibleDocuments(t *testing.T) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		"plain":  render(t, visibleHTML, htmlpdf.Options{}),
		"pdfa2b": render(t, visibleHTML, htmlpdf.Options{Conformance: htmlpdf.PDFA2b}),
		"pdfua":  render(t, visibleHTML, htmlpdf.Options{Conformance: htmlpdf.PDFA2a, Accessibility: htmlpdf.PDFUA1}),
	}
}

// fieldState zwraca zapis pola o nazwie name bez /V, numer jego obiektu, liczbę pól /AcroForm i zapis
// wszystkich stron (do porównania przed podpisem i po nim).
func fieldState(t *testing.T, pdf []byte, name string) (field string, num, fields int, pages string) {
	t.Helper()
	f, err := readPDF(pdf)
	if err != nil {
		t.Fatal(err)
	}
	rootObj, _ := f.resolve(f.trailer.get("Root"))
	root := rootObj.(*pdfDict)
	formObj, _ := f.resolve(root.get("AcroForm"))
	form := formObj.(*pdfDict)
	list, _ := f.resolve(form.get("Fields"))
	for _, ref := range list.(pdfArray) {
		obj, _ := f.resolve(ref)
		d := obj.(*pdfDict)
		if decodeText(d.get("T").(pdfString)) != name {
			continue
		}
		copy := newDict()
		for _, key := range d.keys {
			if key != "V" {
				copy.set(key, d.values[key])
			}
		}
		var b bytes.Buffer
		writeObject(&b, copy)
		field, num = b.String(), ref.(pdfRef).num
	}
	var b bytes.Buffer
	pagesObj, _ := f.resolve(root.get("Pages"))
	kids, _ := f.resolve(pagesObj.(*pdfDict).get("Kids"))
	for _, kid := range kids.(pdfArray) {
		page, _ := f.resolve(kid)
		writeObject(&b, page)
	}
	return field, num, len(list.(pdfArray)), b.String()
}

func TestSignVisibleField(t *testing.T) {
	for name, pdf := range visibleDocuments(t) {
		t.Run(name, func(t *testing.T) {
			rsaID, ecID := identity(t, "rsa"), identity(t, "p256")
			field, num, fields, pages := fieldState(t, pdf, "Zamawiający")
			if field == "" {
				t.Fatal("brak pola Zamawiający w pliku z gopdfgo")
			}
			opts := signOptions(rsaID)
			opts.FieldName = "Zamawiający"
			signed, err := SignPDF(t.Context(), pdf, opts)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(signed, pdf) {
				t.Fatal("podpis zmienił bajty pliku wejściowego")
			}
			sigs := checkSignatures(t, signed, 1)
			if sigs[0].fieldName != "Zamawiający" && decodeText(pdfString(sigs[0].fieldName)) != "Zamawiający" {
				t.Errorf("podpis w polu %q", sigs[0].fieldName)
			}
			field2, num2, fields2, pages2 := fieldState(t, signed, "Zamawiający")
			if field2 != field || num2 != num || fields2 != fields || pages2 != pages {
				t.Errorf("podpis zmienił pole, listę pól albo strony:\n%s\n%s", field, field2)
			}
			checkWithTools(t, signed, sigs, rsaID.cert, true)

			opts = signOptions(ecID)
			opts.FieldName = "Wykonawca"
			twice, err := SignPDF(t.Context(), signed, opts)
			if err != nil {
				t.Fatal(err)
			}
			sigs = checkSignatures(t, twice, 2)
			checkWithTools(t, twice, sigs, rsaID.cert, true)
			if _, _, fields3, pages3 := fieldState(t, twice, "Wykonawca"); fields3 != fields || pages3 != pages {
				t.Error("drugi podpis zmienił listę pól albo strony")
			}
			saveForInspection(t, "visible-"+name, twice)
		})
	}
}

// B-LT w polu z HTML: DSS w drugiej aktualizacji, pole i strony bez zmian.
func TestVisibleFieldLongTerm(t *testing.T) {
	pki := newTestPKI(t)
	pdf := visibleDocuments(t)["pdfua"]
	field, num, fields, pages := fieldState(t, pdf, "Wykonawca")
	opts := pki.options("rsa", LevelBLT)
	opts.FieldName = "Wykonawca"
	signed, err := SignPDF(t.Context(), pdf, opts)
	if err != nil {
		t.Fatal(err)
	}
	sigs := checkSignatures(t, signed, 1)
	checkLongTerm(t, pki, signed, sigs[0], "rsa")
	checkWithTools(t, signed, sigs, pki.root.cert, false)
	if field2, num2, fields2, pages2 := fieldState(t, signed, "Wykonawca"); field2 != field || num2 != num || fields2 != fields || pages2 != pages {
		t.Error("podpis B-LT zmienił pole, listę pól albo strony")
	}
}

func TestVisibleFieldErrors(t *testing.T) {
	pdf := visibleDocuments(t)["plain"]
	opts := signOptions(identity(t, "rsa"))
	opts.FieldName = "Brak"
	if _, err := SignPDF(t.Context(), pdf, opts); !errors.Is(err, ErrUnknownField) {
		t.Errorf("nieznane pole: %v", err)
	}
	opts.FieldName = "Wykonawca"
	signed, err := SignPDF(t.Context(), pdf, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SignPDF(t.Context(), signed, opts); !errors.Is(err, ErrFieldNotEmpty) {
		t.Errorf("pole podpisane drugi raz: %v", err)
	}
	// bez FieldName: nowe niewidoczne pole obok pól z HTML
	signed, err = SignPDF(t.Context(), pdf, signOptions(identity(t, "rsa")))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, fields, _ := fieldState(t, signed, "Signature1"); fields != 3 {
		t.Errorf("pól po podpisie niewidocznym %d, oczekiwane 3", fields)
	}
}

// TestVeraPDFVisibleSignature sprawdza veraPDF-em PDF/A-2b, PDF/A-2a i PDF/UA-1 po podpisaniu obu pól,
// gdy GOPDFGO_VERAPDF wskazuje program veraPDF.
func TestVeraPDFVisibleSignature(t *testing.T) {
	verapdf := os.Getenv("GOPDFGO_VERAPDF")
	if verapdf == "" {
		t.Skip("GOPDFGO_VERAPDF nie ustawione")
	}
	docs := visibleDocuments(t)
	dir := t.TempDir()
	for _, c := range []struct{ doc, flavour string }{{"pdfa2b", "2b"}, {"pdfua", "2a"}, {"pdfua", "ua1"}} {
		pdf := docs[c.doc]
		for _, field := range []string{"Zamawiający", "Wykonawca"} {
			opts := signOptions(identity(t, "rsa"))
			opts.FieldName = field
			var err error
			if pdf, err = SignPDF(t.Context(), pdf, opts); err != nil {
				t.Fatal(err)
			}
		}
		path := filepath.Join(dir, c.doc+"-"+c.flavour+".pdf")
		os.WriteFile(path, pdf, 0o644)
		out, _ := exec.Command(verapdf, "-f", c.flavour, path).CombinedOutput()
		if !strings.Contains(string(out), `isCompliant="true"`) {
			t.Errorf("veraPDF -f %s, %s: niezgodny\n%s", c.flavour, c.doc, out)
		} else {
			t.Logf("veraPDF -f %s, %s: zgodny", c.flavour, c.doc)
		}
	}
}
