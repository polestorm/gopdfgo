package tests

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
}
