// Package pdfa (gopdfgo) zawiera części trybów PDF/A, które da się sprawdzać osobno: profil barw sRGB
// dla OutputIntent (ADR 0016) i rozpoznanie załącznika PDF/A-1 albo PDF/A-2 (ADR 0026).
package pdfa

import (
	"bytes"
	"encoding/binary"
	"math"
	"regexp"
)

// pdfaPart matches the PDF/A part declared in an unfiltered XMP packet, in element or attribute form.
var pdfaPart = regexp.MustCompile(`pdfaid:part\s*(?:>\s*|=\s*["'])([0-9])`)

// DeclaresPDFA12 rozstrzyga, czy plik jest plikiem PDF/A-1 albo PDF/A-2 w rozumieniu ISO 19005-2 § 6.8:
// zaczyna się nagłówkiem PDF i deklaruje w metadanych XMP część 1 albo 2. PDF/A wymaga, żeby strumień
// metadanych nie był filtrowany (ISO 19005-1 § 6.7.2, ISO 19005-2 § 6.6.2.1), więc deklarację da się
// znaleźć w surowych bajtach. To sprawdzenie deklaracji, nie walidacja: zgodność samego załącznika
// pozostaje odpowiedzialnością wywołującego.
func DeclaresPDFA12(data []byte) bool {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return false
	}
	for _, match := range pdfaPart.FindAllSubmatch(data, -1) {
		if part := string(match[1]); part == "1" || part == "2" {
			return true
		}
	}
	return false
}

// SRGBProfile buduje minimalny profil ICC w wersji 2.1 dla przestrzeni sRGB (IEC 61966-2-1): profil
// monitora typu macierz i krzywa, z barwami podstawowymi przeliczonymi do punktu bieli D50 i jedną
// krzywą przenoszenia wspólną dla trzech kanałów. Profil powstaje z liczb z normy, więc nie niesie
// żadnej obcej licencji; wynik jest zawsze ten sam.
func SRGBProfile() []byte {
	fixed := func(v float64) []byte { // s15Fixed16Number
		out := make([]byte, 4)
		binary.BigEndian.PutUint32(out, uint32(int32(math.Round(v*65536))))
		return out
	}
	xyz := func(x, y, z float64) []byte {
		out := append([]byte("XYZ \x00\x00\x00\x00"), fixed(x)...)
		return append(append(out, fixed(y)...), fixed(z)...)
	}
	// krzywa sRGB próbkowana w 1024 punktach
	const samples = 1024
	curve := append([]byte("curv\x00\x00\x00\x00"), 0, 0, samples>>8, samples&0xff)
	for i := range samples {
		v := float64(i) / (samples - 1)
		linear := v / 12.92
		if v > 0.04045 {
			linear = math.Pow((v+0.055)/1.055, 2.4)
		}
		value := uint16(math.Round(linear * 65535))
		curve = append(curve, byte(value>>8), byte(value))
	}
	description := "sRGB IEC61966-2.1"
	desc := append([]byte("desc\x00\x00\x00\x00"), 0, 0, 0, byte(len(description)+1))
	desc = append(desc, description...)
	desc = append(desc, make([]byte, 1+4+4+2+1+67)...) // koniec napisu, puste opisy Unicode i ScriptCode
	text := append([]byte("text\x00\x00\x00\x00"), "No copyright, use freely\x00"...)

	type tag struct {
		signature string
		data      []byte
	}
	tags := []tag{
		{"desc", desc},
		{"cprt", text},
		{"wtpt", xyz(0.9642, 1.0, 0.8249)},
		{"rXYZ", xyz(0.4360747, 0.2225045, 0.0139322)},
		{"gXYZ", xyz(0.3850649, 0.7168786, 0.0971045)},
		{"bXYZ", xyz(0.1430804, 0.0606169, 0.7141733)},
		{"rTRC", curve},
		{"gTRC", nil}, // ta sama krzywa co rTRC
		{"bTRC", nil},
	}

	const headerSize = 128
	offset := headerSize + 4 + 12*len(tags)
	table := make([]byte, 4, 4+12*len(tags))
	binary.BigEndian.PutUint32(table, uint32(len(tags)))
	var body []byte
	var curveOffset, curveSize int
	for _, t := range tags {
		entry := make([]byte, 12)
		copy(entry, t.signature)
		if t.data == nil {
			binary.BigEndian.PutUint32(entry[4:], uint32(curveOffset))
			binary.BigEndian.PutUint32(entry[8:], uint32(curveSize))
		} else {
			if t.signature == "rTRC" {
				curveOffset, curveSize = offset, len(t.data)
			}
			binary.BigEndian.PutUint32(entry[4:], uint32(offset))
			binary.BigEndian.PutUint32(entry[8:], uint32(len(t.data)))
			padded := append([]byte(nil), t.data...)
			for len(padded)%4 != 0 {
				padded = append(padded, 0)
			}
			body = append(body, padded...)
			offset += len(padded)
		}
		table = append(table, entry...)
	}

	header := make([]byte, headerSize)
	binary.BigEndian.PutUint32(header[0:], uint32(offset)) // rozmiar profilu
	binary.BigEndian.PutUint32(header[8:], 0x02100000)     // wersja 2.1
	copy(header[12:], "mntr")                              // klasa: monitor
	copy(header[16:], "RGB ")                              // przestrzeń danych
	copy(header[20:], "XYZ ")                              // przestrzeń połączenia profili
	// data utworzenia: stała, żeby profil był zawsze ten sam (2026-01-01 00:00:00)
	binary.BigEndian.PutUint16(header[24:], 2026)
	binary.BigEndian.PutUint16(header[26:], 1)
	binary.BigEndian.PutUint16(header[28:], 1)
	copy(header[36:], "acsp")
	copy(header[68:], fixed(0.9642)) // oświetlenie przestrzeni połączenia: D50
	copy(header[72:], fixed(1.0))
	copy(header[76:], fixed(0.8249))
	return append(append(header, table...), body...)
}
