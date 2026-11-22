package cffsubset

import (
	"bytes"
	"os"
	"testing"

	ot "github.com/go-text/typesetting/font/opentype"
)

// TestWriteCIDFixture tworzy testdata/fonts/cff/noto/NotoSansCID-Regular.otf: Noto Sans z tabelą CFF
// przepisaną do postaci CID-keyed (ROS Adobe-Identity-0, CID = 2 × numer glifu, dwa słowniki FD: glify
// parzyste i nieparzyste, każdy z kopią Private DICT i lokalnych podprogramów, charstringi i podprogramy
// globalne bez zmian). Pozostałe tabele są kopiowane. Uruchomienie:
//
//	CFFSUBSET_WRITE_FIXTURE=1 go test ./internal/cffsubset/ -run TestWriteCIDFixture
//
// Bez zmiennej środowiska test jest pomijany: plik leży w repozytorium, a późniejsze zmiany zapisu
// podzbioru nie muszą dawać tych samych bajtów.
func TestWriteCIDFixture(t *testing.T) {
	if os.Getenv("CFFSUBSET_WRITE_FIXTURE") == "" {
		t.Skip("set CFFSUBSET_WRITE_FIXTURE=1 to regenerate the fixture")
	}
	font := readFont(t, notoOTF)
	ld, err := ot.NewLoader(bytes.NewReader(font))
	if err != nil {
		t.Fatal(err)
	}
	raw := rawTable(t, font, "CFF ")
	src, err := parseCFF(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	cid := *src
	cid.cid = true
	cid.fds = []fontDict{src.fds[0], src.fds[0]}
	n := len(src.charStrings)
	cid.fdSelect = make([]uint8, n)
	keep, cids := make([]int, n), make([]int, n)
	for g := range keep {
		cid.fdSelect[g] = uint8(g % 2)
		keep[g], cids[g] = g, 2*g
	}
	cffTable := writeSubset(&cid, keep, cids, nil, nil, nil, "NotoSans-Regular", ld)

	var tables []ot.Table
	for _, tag := range ld.Tables() {
		content, err := ld.RawTable(tag)
		if err != nil {
			t.Fatal(err)
		}
		if tag == tagCFF {
			content = cffTable
		}
		tables = append(tables, ot.Table{Tag: tag, Content: content})
	}
	out := ot.WriteOpentype(tables, ld.Type)
	if err := os.WriteFile(notoCIDOT, out, 0o644); err != nil {
		t.Fatal(err)
	}
}
