package htmlinput

import "testing"

// Znacznik kolejności bajtów w UTF-16 jest zamieniany na UTF-8 bez znacznika.
func TestByteOrderMarkDecoding(t *testing.T) {
	const document = "<!doctype html><p>zażółć gęślą jaźń</p>"
	utf16LE := []byte{0xFF, 0xFE}
	utf16BE := []byte{0xFE, 0xFF}
	for _, r := range document {
		utf16LE = append(utf16LE, byte(r), byte(r>>8))
		utf16BE = append(utf16BE, byte(r>>8), byte(r))
	}
	for name, encoded := range map[string][]byte{"UTF-16LE": utf16LE, "UTF-16BE": utf16BE, "UTF-8": []byte("\xEF\xBB\xBF" + document)} {
		if got := DecodeByteOrderMark(encoded); string(got) != document {
			t.Errorf("%s: po zdekodowaniu %q", name, got)
		}
	}
}
