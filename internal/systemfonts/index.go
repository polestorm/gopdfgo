//go:build systemfonts

// Package systemfonts przegląda katalogi fontów systemu operacyjnego i podaje silnikowi fonty
// zastępcze dla rodzin i znaków, których nie mają fonty wywołującego (ADR 0039). Pakiet jest
// kompilowany wyłącznie ze znacznikiem systemfonts; binarka domyślna go nie zawiera.
package systemfonts

import (
	"encoding/binary"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
	"github.com/go-text/typesetting/fontscan"
)

var (
	os2Tag  = ot.MustNewTag("OS/2")
	cmapTag = ot.MustNewTag("cmap")
	glyfTag = ot.MustNewTag("glyf")
	locaTag = ot.MustNewTag("loca")
	cffTag  = ot.MustNewTag("CFF ")
	cff2Tag = ot.MustNewTag("CFF2")
)

// entry opisuje jeden font (twarz) z pliku w katalogu systemowym.
type entry struct {
	Path       string
	Index      int  // numer fontu w kolekcji (TTC/OTC)
	Collection bool // plik jest kolekcją: do osadzenia font trzeba z niej wyjąć
	Family     string
	Aspect     font.Aspect
	FSType     uint16    // OS/2 fsType: uprawnienia osadzania
	Outlines   bool      // kontury TrueType (glyf), CFF albo CFF2 (ADR 0040); bez nich font nie nadaje się do PDF
	Ranges     [][2]rune // znaki z cmap, przedziały domknięte, rosnąco
}

// covers mówi, czy font ma znak r.
func (e *entry) covers(r rune) bool {
	i := sort.Search(len(e.Ranges), func(i int) bool { return e.Ranges[i][1] >= r })
	return i < len(e.Ranges) && e.Ranges[i][0] <= r
}

// index to fonty systemowe gotowe do wyboru: tylko fonty z konturami, w kolejności ścieżek.
type index struct {
	entries     []entry
	byFamily    map[string][]int  // znormalizowana nazwa rodziny (font.NormalizeFamily) → fonty
	familyOrder []string          // znormalizowane nazwy rodzin w kolejności pierwszego fontu
	familyName  map[string]string // znormalizowana nazwa → nazwa z pliku
	files       int               // liczba przejrzanych plików fontów
}

var (
	indexLock sync.Mutex
	// indexes to indeksy zbudowane w tym procesie, według listy katalogów: katalogi przeglądamy raz
	// na proces (fonty doinstalowane w trakcie pracy procesu nie są widoczne)
	indexes = map[string]*index{}
	// testDirectories zastępuje katalogi systemowe w testach (SetDirectoriesForTest)
	testDirectories []string
)

// SetDirectoriesForTest każe szukać fontów systemowych w podanych katalogach zamiast katalogów
// systemu i czyści indeksy zbudowane w procesie; zwraca funkcję przywracającą katalogi systemu.
// Tylko dla testów (tests/systemfonts_test.go): pakiet jest wewnętrzny, poza modułem niedostępny.
func SetDirectoriesForTest(dirs []string) (restore func()) {
	indexLock.Lock()
	defer indexLock.Unlock()
	previous := testDirectories
	testDirectories = append([]string{}, dirs...)
	indexes = map[string]*index{}
	return func() {
		indexLock.Lock()
		defer indexLock.Unlock()
		testDirectories = previous
		indexes = map[string]*index{}
	}
}

// directories zwraca katalogi do przejrzenia: katalogi testowe albo katalogi systemu według go-text
// (fontscan.DefaultFontDirectories: na macOS /System/Library/Fonts, /Library/Fonts, ~/Library/Fonts
// i katalogi zasobów fontów systemu, na Linuksie katalogi z konfiguracji fontconfig, na Windows
// %WINDIR%\Fonts i katalog fontów użytkownika).
func directories() []string {
	if testDirectories != nil {
		return testDirectories
	}
	dirs, err := fontscan.DefaultFontDirectories(discard{})
	if err != nil {
		return nil // brak katalogów fontów: indeks pusty
	}
	return dirs
}

// loadIndex zwraca indeks fontów systemowych: z pamięci procesu albo po przejrzeniu katalogów.
// Indeks nie jest zapisywany na dysk: przejrzenie katalogów trwa na macOS z 449 plikami fontów
// 30-65 ms (pomiar 2026-11-22, ADR 0039), krócej niż wczytanie zapisanego indeksu w JSON.
func loadIndex() *index {
	indexLock.Lock()
	defer indexLock.Unlock()
	dirs := directories()
	key := strings.Join(dirs, "\x00")
	if idx, ok := indexes[key]; ok {
		return idx
	}
	idx := buildIndex(scan(dirs))
	indexes[key] = idx
	return idx
}

// scan przegląda katalogi (z podkatalogami, w kolejności leksykalnej) i zwraca fonty w kolejności
// przeglądania oraz liczbę plików. Plik widziany pod kilkoma ścieżkami (dowiązania) liczy się raz.
func scan(dirs []string) (faces []entry, files int) {
	seen := map[string]bool{}
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil // katalog bez prawa odczytu albo zniknięty plik: pomijamy
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".ttf", ".otf", ".ttc", ".otc":
			default:
				return nil
			}
			real, err := filepath.EvalSymlinks(path)
			if err != nil || seen[real] {
				return nil
			}
			seen[real] = true
			faces = append(faces, scanFile(path)...)
			files++
			return nil
		})
	}
	return faces, files
}

// scanFile czyta z pliku tylko tabele potrzebne do wyboru fontu: nazwę i aspekt, cmap, OS/2.
func scanFile(path string) []entry {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var magic [4]byte
	if _, err := file.ReadAt(magic[:], 0); err != nil {
		return nil
	}
	loaders, err := ot.NewLoaders(file)
	if err != nil {
		return nil
	}
	collection := string(magic[:]) == "ttcf"
	var out []entry
	for i, ld := range loaders {
		e := entry{Path: path, Index: i, Collection: collection}
		page := tables.FPNone
		if raw, err := ld.RawTable(os2Tag); err == nil {
			if len(raw) >= 10 {
				e.FSType = binary.BigEndian.Uint16(raw[8:])
			}
			if os2, _, err := tables.ParseOs2(raw); err == nil {
				page = os2.FontPage()
			}
		}
		raw, err := ld.RawTable(cmapTag)
		if err != nil {
			continue
		}
		table, _, err := tables.ParseCmap(raw)
		if err != nil {
			continue
		}
		cmap, _, err := font.ProcessCmap(table, page)
		if err != nil {
			continue
		}
		e.Ranges = runeRanges(cmap)
		desc, _ := font.Describe(ld, nil)
		e.Family, e.Aspect = desc.Family, desc.Aspect
		e.Aspect.SetDefaults()
		e.Outlines = (ld.HasTable(glyfTag) && ld.HasTable(locaTag)) || ld.HasTable(cffTag) || ld.HasTable(cff2Tag)
		out = append(out, e)
	}
	return out
}

// runeRanges zwraca znaki cmap (bez znaków przypisanych glifowi .notdef) jako przedziały.
func runeRanges(cmap font.Cmap) [][2]rune {
	if ranger, ok := cmap.(font.CmapRuneRanger); ok {
		return append([][2]rune{}, ranger.RuneRanges(nil)...)
	}
	var runes []rune
	for iter := cmap.Iter(); iter.Next(); {
		if r, gid := iter.Char(); gid != 0 {
			runes = append(runes, r)
		}
	}
	sort.Slice(runes, func(i, j int) bool { return runes[i] < runes[j] })
	var out [][2]rune
	for _, r := range runes {
		if n := len(out); n > 0 && r <= out[n-1][1]+1 {
			if r > out[n-1][1] {
				out[n-1][1] = r
			}
			continue
		}
		out = append(out, [2]rune{r, r})
	}
	return out
}

// lastResort to rodzina fontu, który na macOS przypisuje każdemu znakowi Unicode glif zastępczy
// (ramka z numerem bloku): „ma” każdy znak, więc wygrałby wybór fontu dla każdego brakującego znaku.
const lastResort = "lastresort"

func buildIndex(faces []entry, files int) *index {
	idx := &index{byFamily: map[string][]int{}, familyName: map[string]string{}, files: files}
	for _, e := range faces {
		key := font.NormalizeFamily(e.Family)
		if !e.Outlines || key == "" || key == lastResort {
			continue // np. Apple Color Emoji (tylko mapy bitowe sbix)
		}
		if _, ok := idx.byFamily[key]; !ok {
			idx.familyOrder = append(idx.familyOrder, key)
			idx.familyName[key] = e.Family
		}
		idx.byFamily[key] = append(idx.byFamily[key], len(idx.entries))
		idx.entries = append(idx.entries, e)
	}
	return idx
}

type discard struct{}

func (discard) Printf(string, ...interface{}) {}
