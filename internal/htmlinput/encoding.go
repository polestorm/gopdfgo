package htmlinput

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
)

// DecodeByteOrderMark zwraca dokument w UTF-8 bez znacznika kolejności bajtów. Według normy HTML
// (§ 13.2.3.2, "BOM sniffing") znacznik na początku rozstrzyga o kodowaniu i nie należy do treści;
// bez tego kroku U+FEFF trafiał do dokumentu jako tekst przed pierwszym elementem.
func DecodeByteOrderMark(document []byte) []byte {
	switch {
	case bytes.HasPrefix(document, []byte{0xEF, 0xBB, 0xBF}):
		return document[3:]
	case bytes.HasPrefix(document, []byte{0xFF, 0xFE}):
		return decodeUTF16(document[2:], binary.LittleEndian)
	case bytes.HasPrefix(document, []byte{0xFE, 0xFF}):
		return decodeUTF16(document[2:], binary.BigEndian)
	}
	return document
}

func decodeUTF16(data []byte, order binary.ByteOrder) []byte {
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = order.Uint16(data[2*i:])
	}
	return []byte(string(utf16.Decode(units)))
}
