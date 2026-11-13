package gopdfgo

import (
	"bytes"
	"testing"
)

// Znacznik kolejności bajtów nie jest treścią: dokument z nim i bez niego daje ten sam PDF.
func TestByteOrderMarkIsNotContent(t *testing.T) {
	const document = "<!doctype html><p>zażółć gęślą jaźń</p>"
	plain := renderString(t, document)
	utf8BOM := renderString(t, "\xEF\xBB\xBF"+document)
	if !bytes.Equal(plain.PDF, utf8BOM.PDF) {
		t.Error("UTF-8 ze znacznikiem: inny wynik niż bez znacznika")
	}
	utf16LE := []byte{0xFF, 0xFE}
	utf16BE := []byte{0xFE, 0xFF}
	for _, r := range document {
		utf16LE = append(utf16LE, byte(r), byte(r>>8))
		utf16BE = append(utf16BE, byte(r>>8), byte(r))
	}
	for name, encoded := range map[string][]byte{"UTF-16LE": utf16LE, "UTF-16BE": utf16BE} {
		if got := decodeByteOrderMark(encoded); string(got) != document {
			t.Errorf("%s: po zdekodowaniu %q", name, got)
		}
	}
}
