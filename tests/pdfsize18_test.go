package tests

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"github.com/polestorm/gopdfgo/htmlpdf"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// Options.Uncompressed: strumienie treści i fontów bez filtra, tekst strony czytelny wprost w pliku.
func TestUncompressedStreams(t *testing.T) {
	html := []byte(`<p>Faktura VAT</p>`)
	plain, err := htmlpdf.Render(context.Background(), html, htmlpdf.Options{Fonts: testFonts(t), Uncompressed: true})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(plain.PDF, []byte("/FlateDecode")) {
		t.Error("Uncompressed: w pliku został filtr FlateDecode")
	}
	if !bytes.Contains(plain.PDF, []byte(" Tf")) {
		t.Error("Uncompressed: brak czytelnego operatora Tf w treści strony")
	}
	packed, err := htmlpdf.Render(context.Background(), html, htmlpdf.Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(packed.PDF, []byte("/FlateDecode")) || len(packed.PDF) >= len(plain.PDF) {
		t.Errorf("domyślnie: %d B z kompresją wobec %d B bez", len(packed.PDF), len(plain.PDF))
	}
}

var (
	fontNameTag = regexp.MustCompile(`/FontName /([^+\s/]+)\+`)
	fontFile    = regexp.MustCompile(`(?s)/Length1 \d+.*?stream\r?\n`)
)

// Podzbiór fontu: znacznik z sześciu wielkich liter (ISO 32000-1 § 9.6.4), krótki format loca, tabela name
// z rekordami 0-6, wyzerowane metryki glifów spoza podzbioru.
func TestFontSubsetTables(t *testing.T) {
	result, err := htmlpdf.Render(context.Background(), []byte(`<p>Faktura VAT nr 1/2026</p>`), htmlpdf.Options{Fonts: testFonts(t), Uncompressed: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range fontNameTag.FindAllSubmatch(result.PDF, -1) {
		if !regexp.MustCompile(`^[A-Z]{6}$`).Match(m[1]) {
			t.Errorf("znacznik podzbioru %q, oczekiwane sześć wielkich liter", m[1])
		}
	}
	loc := fontFile.FindIndex(result.PDF)
	if loc == nil {
		t.Fatal("brak pliku fontu w PDF")
	}
	font := result.PDF[loc[1]:]
	tables := map[string][]byte{}
	count := int(binary.BigEndian.Uint16(font[4:]))
	for i := 0; i < count; i++ {
		rec := font[12+16*i:]
		offset, length := binary.BigEndian.Uint32(rec[8:]), binary.BigEndian.Uint32(rec[12:])
		tables[string(rec[:4])] = font[offset : offset+length]
	}
	if format := binary.BigEndian.Uint16(tables["head"][50:]); format != 0 {
		t.Errorf("indexToLocFormat = %d, oczekiwany krótki (0)", format)
	}
	name := tables["name"]
	for i := 0; i < int(binary.BigEndian.Uint16(name[2:])); i++ {
		if id := binary.BigEndian.Uint16(name[6+12*i+6:]); id > 6 {
			t.Errorf("tabela name ma rekord %d", id)
		}
	}
	numGlyphs := int(binary.BigEndian.Uint16(tables["maxp"][4:]))
	metrics := int(binary.BigEndian.Uint16(tables["hhea"][34:]))
	nonZero := 0
	hmtx := tables["hmtx"]
	for gid := 0; gid < numGlyphs; gid++ {
		var entry []byte
		if gid < metrics {
			entry = hmtx[4*gid : 4*gid+4]
		} else {
			entry = hmtx[4*metrics+2*(gid-metrics) : 4*metrics+2*(gid-metrics)+2]
		}
		if !bytes.Equal(entry, make([]byte, len(entry))) {
			nonZero++
		}
	}
	// tekst ma 16 różnych znaków; z glifem .notdef i spacją niezerowych wpisów nie może być więcej
	if numGlyphs < 20 || nonZero > 20 {
		t.Errorf("hmtx: %d glifów, niezerowych wpisów %d", numGlyphs, nonZero)
	}
}

// Domyślna kompresja daje strumienie, które zlib rozpakowuje (sprawdzenie stopnia nie psuje formatu).
func TestCompressedStreamsDecode(t *testing.T) {
	result, err := htmlpdf.Render(context.Background(), []byte(`<p>Faktura VAT</p>`), htmlpdf.Options{Fonts: testFonts(t)})
	if err != nil {
		t.Fatal(err)
	}
	streams := regexp.MustCompile(`(?s)/FlateDecode.*?stream\r?\n`).FindAllIndex(result.PDF, -1)
	if len(streams) == 0 {
		t.Fatal("brak strumieni FlateDecode")
	}
	for _, s := range streams {
		r, err := zlib.NewReader(bytes.NewReader(result.PDF[s[1]:]))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadAll(r); err != nil && !strings.Contains(err.Error(), "unexpected EOF") {
			t.Fatal(err)
		}
	}
}

// Dywiz-minus po literze albo cyfrze i przed cyfrą jest miejscem podziału, jak w Chromium; minus na
// początku i znak po nawiasie nie są (pomiar w Chrome 157, 2026-11-20).
func TestHyphenBeforeDigitsBreaks(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("brak pdftotext")
	}
	cases := map[string]string{
		"03-228": "03-|228", "555-1234": "555-|1234", "a-1": "a-|1", "1.5-2.5": "1.5-|2.5",
		"-5": "-5", "(1)-2": "(1)-2", "abc-def": "abc-|def",
	}
	for in, want := range cases {
		html := `<style>@page{margin:0}body{margin:0;font-size:20px}div{width:0}</style><div>` + in + `</div>`
		result, err := htmlpdf.Render(context.Background(), []byte(html), htmlpdf.Options{Fonts: testFonts(t)})
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("pdftotext", "-layout", "-", "-")
		cmd.Stdin = bytes.NewReader(result.PDF)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for _, l := range strings.Split(string(out), "\n") {
			if l = strings.TrimSpace(strings.Trim(l, "\f")); l != "" {
				lines = append(lines, l)
			}
		}
		if got := strings.Join(lines, "|"); got != want {
			t.Errorf("%q: wiersze %q, oczekiwane %q", in, got, want)
		}
	}
}
