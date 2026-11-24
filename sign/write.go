package sign

// Zapis obiektów i sekcji odsyłaczy aktualizacji przyrostowej (ISO 32000-1 § 7.5.6).

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"unicode/utf16"
)

// writeObject zapisuje obiekt bezpośredni. Ciągi są zapisywane szesnastkowo (bajty bez zmian,
// bez kłopotu z ucieczkami), liczby w postaci źródłowej, słowniki w kolejności kluczy.
func writeObject(b *bytes.Buffer, obj pdfObject) {
	switch v := obj.(type) {
	case pdfName:
		writeName(b, v)
	case pdfNumber:
		b.WriteString(string(v))
	case pdfString:
		b.WriteByte('<')
		b.WriteString(hex.EncodeToString(v))
		b.WriteByte('>')
	case pdfBool:
		b.WriteString(strconv.FormatBool(bool(v)))
	case pdfNull, nil:
		b.WriteString("null")
	case pdfRef:
		fmt.Fprintf(b, "%d %d R", v.num, v.gen)
	case pdfRaw:
		b.WriteString(string(v))
	case pdfArray:
		b.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				b.WriteByte(' ')
			}
			writeObject(b, item)
		}
		b.WriteByte(']')
	case *pdfDict:
		b.WriteString("<<")
		for _, key := range v.keys {
			b.WriteByte(' ')
			writeName(b, key)
			b.WriteByte(' ')
			writeObject(b, v.values[key])
		}
		b.WriteString(" >>")
	default:
		panic(fmt.Sprintf("sign: unsupported object %T", obj))
	}
}

// writeName zapisuje nazwę z #xx dla znaków spoza zakresu zwykłych (§ 7.3.5).
func writeName(b *bytes.Buffer, name pdfName) {
	b.WriteByte('/')
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c < '!' || c > '~' || c == '#' || isDelim(c) {
			fmt.Fprintf(b, "#%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
}

// textString koduje ciąg tekstowy (§ 7.9.2.2): ASCII bez zmian, inne znaki jako UTF-16BE ze znacznikiem BOM.
func textString(s string) pdfString {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			ascii = false
			break
		}
	}
	if ascii {
		return pdfString(s)
	}
	out := []byte{0xfe, 0xff}
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u>>8), byte(u))
	}
	return out
}

// updateObject to obiekt zapisywany w aktualizacji.
type updateObject struct {
	ref  pdfRef
	body []byte // zapis obiektu bez „N G obj” i „endobj”
}

// appendUpdate dopisuje do data obiekty i sekcję odsyłaczy tego samego rodzaju co najnowsza
// w pliku. Zwraca nowy plik i przesunięcia początków treści obiektów w nim.
func appendUpdate(f *pdfFile, objects []updateObject, trailer *pdfDict) ([]byte, map[int]int) {
	sort.Slice(objects, func(i, j int) bool { return objects[i].ref.num < objects[j].ref.num })
	var b bytes.Buffer
	b.Grow(len(f.data) + 4096)
	b.Write(f.data)
	if last := f.data[len(f.data)-1]; last != '\n' && last != '\r' {
		b.WriteByte('\n')
	}
	offsets := map[int]int64{}
	bodies := map[int]int{}
	for _, obj := range objects {
		offsets[obj.ref.num] = int64(b.Len())
		fmt.Fprintf(&b, "%d %d obj\n", obj.ref.num, obj.ref.gen)
		bodies[obj.ref.num] = b.Len()
		b.Write(obj.body)
		b.WriteString("\nendobj\n")
	}
	gens := map[int]int{}
	for _, obj := range objects {
		gens[obj.ref.num] = obj.ref.gen
	}
	xrefAt := int64(b.Len())
	if f.xrefStream {
		// strumień odsyłaczy (§ 7.5.8) obejmuje też samego siebie
		self := f.size + countNew(objects, f.size)
		offsets[self] = xrefAt
		gens[self] = 0
		nums := sortedKeys(offsets)
		width := 1
		for v := xrefAt >> 8; v > 0; v >>= 8 {
			width++
		}
		width = max(width, 4)
		var rows []byte
		for _, n := range nums {
			rows = append(rows, 1)
			for k := width - 1; k >= 0; k-- {
				rows = append(rows, byte(offsets[n]>>(8*k)))
			}
			rows = append(rows, byte(gens[n]>>8), byte(gens[n]))
		}
		trailer.set("Type", pdfName("XRef"))
		trailer.set("Size", pdfNumber(strconv.Itoa(self+1)))
		trailer.set("W", pdfArray{pdfNumber("1"), pdfNumber(strconv.Itoa(width)), pdfNumber("2")})
		trailer.set("Index", subsections(nums))
		trailer.set("Length", pdfNumber(strconv.Itoa(len(rows))))
		fmt.Fprintf(&b, "%d 0 obj\n", self)
		writeObject(&b, trailer)
		b.WriteString("\nstream\n")
		b.Write(rows)
		b.WriteString("\nendstream\nendobj\n")
	} else {
		nums := sortedKeys(offsets)
		b.WriteString("xref\n")
		for i := 0; i < len(nums); {
			j := i + 1
			for j < len(nums) && nums[j] == nums[j-1]+1 {
				j++
			}
			fmt.Fprintf(&b, "%d %d\n", nums[i], j-i)
			for _, n := range nums[i:j] {
				fmt.Fprintf(&b, "%010d %05d n\r\n", offsets[n], gens[n])
			}
			i = j
		}
		trailer.set("Size", pdfNumber(strconv.Itoa(f.size+countNew(objects, f.size))))
		b.WriteString("trailer\n")
		writeObject(&b, trailer)
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "startxref\n%d\n%%%%EOF\n", xrefAt)
	return b.Bytes(), bodies
}

// countNew liczy obiekty o numerach od size wzwyż (nowe w aktualizacji).
func countNew(objects []updateObject, size int) int {
	n := 0
	for _, obj := range objects {
		if obj.ref.num >= size {
			n++
		}
	}
	return n
}

func sortedKeys(m map[int]int64) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// subsections zapisuje /Index strumienia odsyłaczy: pary (pierwszy numer, liczba) ciągłych numerów.
func subsections(nums []int) pdfArray {
	var index pdfArray
	for i := 0; i < len(nums); {
		j := i + 1
		for j < len(nums) && nums[j] == nums[j-1]+1 {
			j++
		}
		index = append(index, pdfNumber(strconv.Itoa(nums[i])), pdfNumber(strconv.Itoa(j-i)))
		i = j
	}
	return index
}
