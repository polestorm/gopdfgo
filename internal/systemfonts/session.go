//go:build systemfonts

package systemfonts

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/fontscan"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
)

var _ text.SystemFonts = (*Session)(nil)

// Session to fonty systemowe dla jednego renderowania: pamięta wybory, wczytane fonty i ostrzeżenia.
// Nie jest bezpieczna dla wielu wątków (renderowania są wykonywane po kolei).
type Session struct {
	idx *index

	// probe to fontmapa go-text z jednym sztucznym fontem na rodzinę systemową: rozstrzyga nazwy
	// rodzin generycznych i podstawienia zgodne metrycznie tą samą tablicą podstawień co dla fontów
	// wywołującego, bez wczytywania fontów
	probe       *fontscan.FontMap
	probeFamily map[*font.Font]string
	sansSerif   string // rodzina, którą daje sans-serif: tak kończy się podstawienie nieznanej rodziny

	families map[string]string  // nazwa rodziny z CSS → znormalizowana rodzina systemowa ("" = brak)
	chosen   map[faceKey]int    // rodzina i aspekt → wybrany font (-1 = żaden)
	byRune   map[runeKey]int    // znak i aspekt → font zastępczy (-1 = żaden)
	faces    map[int]*font.Face // wczytane fonty
	byFont   map[*font.Font]int
	contents map[string][]byte // pliki fontów do osadzenia według Location.File
	failed   map[int]bool

	warnings []string
}

type faceKey struct {
	family string
	aspect font.Aspect
}

type runeKey struct {
	r      rune
	aspect font.Aspect
}

// Open przygotowuje fonty systemowe dla jednego renderowania. Katalogi fontów systemu są przeglądane
// przy pierwszym wywołaniu w procesie (zob. loadIndex).
func Open() *Session {
	idx := loadIndex()
	s := &Session{
		idx:         idx,
		probe:       fontscan.NewFontMap(discard{}),
		probeFamily: map[*font.Font]string{},
		families:    map[string]string{},
		chosen:      map[faceKey]int{},
		byRune:      map[runeKey]int{},
		faces:       map[int]*font.Face{},
		byFont:      map[*font.Font]int{},
		contents:    map[string][]byte{},
		failed:      map[int]bool{},
	}
	normal := font.Aspect{Style: font.StyleNormal, Weight: font.WeightNormal, Stretch: font.StretchNormal}
	for _, key := range idx.familyOrder {
		ft := &font.Font{Cmap: probeCmap{}}
		s.probeFamily[ft] = key
		s.probe.AddFace(font.NewFace(ft), fontscan.Location{File: key}, font.Description{Family: idx.familyName[key], Aspect: normal})
	}
	s.sansSerif = s.probeResolve("sans-serif")
	return s
}

// Empty mówi, czy w katalogach systemowych nie ma żadnego fontu nadającego się do osadzenia.
func (s *Session) Empty() bool { return len(s.idx.entries) == 0 }

// Files to liczba przejrzanych plików fontów, Faces to liczba fontów w indeksie (do pomiarów).
func (s *Session) Files() int { return s.idx.files }
func (s *Session) Faces() int { return len(s.idx.entries) }

// Warnings zwraca ostrzeżenia w kolejności zdarzeń: użyte fonty systemowe (rodzina i plik) oraz
// fonty pominięte, bo licencja zabrania osadzania.
func (s *Session) Warnings() []string { return s.warnings }

func (s *Session) probeResolve(family string) string {
	s.probe.SetQuery(fontscan.Query{Families: []string{family},
		Aspect: font.Aspect{Style: font.StyleNormal, Weight: font.WeightNormal, Stretch: font.StretchNormal}})
	face := s.probe.ResolveFace('a')
	if face == nil {
		return ""
	}
	return s.probeFamily[face.Font]
}

// goTextGenerics to rodziny generyczne, które go-text rozwija tablicą podstawień (fontscan, match.go).
var goTextGenerics = map[string]bool{"serif": true, "sans-serif": true, "monospace": true, "cursive": true,
	"fantasy": true, "math": true, "emoji": true}

// resolveFamily zamienia nazwę rodziny z CSS na rodzinę systemową: tę samą nazwę (CSS Fonts 4 § 5.1,
// bez rozróżniania wielkości liter), rodzinę dla rodziny generycznej albo rodzinę zgodną metrycznie
// z tablicy podstawień go-text. Rodzina nieznana daje "" (go-text kończy wtedy na sans-serif).
func (s *Session) resolveFamily(family string) string {
	if resolved, ok := s.families[family]; ok {
		return resolved
	}
	key := font.NormalizeFamily(strings.TrimSpace(family))
	resolved := ""
	if _, ok := s.idx.byFamily[key]; ok {
		resolved = key
	} else if probed := s.probeResolve(family); probed != "" &&
		(goTextGenerics[strings.ToLower(strings.TrimSpace(family))] || probed != s.sansSerif) {
		resolved = probed
	}
	s.families[family] = resolved
	return resolved
}

// HasFamily: zob. text.SystemFonts.
func (s *Session) HasFamily(family string) bool { return s.resolveFamily(family) != "" }

// FamilyFace: zob. text.SystemFonts. Według CSS Fonts 4 § 5.2 z rodziny wybieramy jeden font według
// aspektu; gdy nie ma znaku, wybór przechodzi do następnej rodziny z listy, a nie do innego fontu
// tej samej rodziny.
func (s *Session) FamilyFace(family string, aspect font.Aspect, r rune) *font.Face {
	key := s.resolveFamily(family)
	if key == "" {
		return nil
	}
	i := s.faceFor(key, aspect)
	if i < 0 || !s.idx.entries[i].covers(r) {
		return nil
	}
	return s.load(i)
}

// RuneFace: zob. text.SystemFonts. Bez rodziny z listy, która ma znak, wybieramy pierwszą rodzinę
// systemową w kolejności ścieżek plików, której font o danym aspekcie ma znak: wynik zależy od
// zestawu fontów maszyny, ale przy tym samym zestawie jest powtarzalny.
func (s *Session) RuneFace(families []string, aspect font.Aspect, r rune) *font.Face {
	for _, family := range families {
		if face := s.FamilyFace(family, aspect, r); face != nil {
			return face
		}
	}
	key := runeKey{r, aspect}
	i, ok := s.byRune[key]
	if !ok {
		i = -1
		for _, family := range s.idx.familyOrder {
			if !s.familyCovers(family, r) {
				continue
			}
			if j := s.faceFor(family, aspect); j >= 0 && s.idx.entries[j].covers(r) {
				i = j
				break
			}
		}
		s.byRune[key] = i
	}
	if i < 0 {
		return nil
	}
	return s.load(i)
}

func (s *Session) familyCovers(family string, r rune) bool {
	for _, i := range s.idx.byFamily[family] {
		if s.idx.entries[i].covers(r) {
			return true
		}
	}
	return false
}

// faceFor wybiera font rodziny według aspektu; fonty, których licencja zabrania osadzania albo
// których nie udało się wczytać, są pomijane (z ostrzeżeniem), jakby ich nie było.
func (s *Session) faceFor(family string, aspect font.Aspect) int {
	key := faceKey{family, aspect}
	if i, ok := s.chosen[key]; ok {
		return i
	}
	var candidates []int
	for _, i := range s.idx.byFamily[family] {
		if !s.failed[i] {
			candidates = append(candidates, i)
		}
	}
	chosen := -1
	for len(candidates) > 0 {
		best := bestMatch(s.idx.entries, candidates, aspect)
		if reason := embeddingRestriction(s.idx.entries[best].FSType); reason != "" {
			s.failed[best] = true
			s.warnings = append(s.warnings, fmt.Sprintf("system font skipped: %s (%s): %s",
				s.idx.entries[best].Family, describePath(s.idx.entries[best]), reason))
			candidates = remove(candidates, best)
			continue
		}
		chosen = best
		break
	}
	s.chosen[key] = chosen
	return chosen
}

func remove(list []int, value int) []int {
	out := list[:0:0]
	for _, v := range list {
		if v != value {
			out = append(out, v)
		}
	}
	return out
}

// embeddingRestriction zwraca powód, dla którego fontu nie wolno osadzić w PDF w podzbiorze, albo "".
// Uprawnienia według pola fsType tabeli OS/2 (OpenType 1.9, OS/2): bit 1 (0x0002) to „restricted
// license embedding”, chyba że ustawiono też bit 2 albo 3 (starsze fonty mogą mieć kilka bitów,
// obowiązuje najmniej restrykcyjny); bit 8 (0x0100) zabrania podzbiorów, a silnik osadza zawsze
// podzbiory; bit 9 (0x0200) pozwala osadzić tylko mapy bitowe, nie kontury. ISO 19005-2 § 6.2.11.4.1
// wymaga fontów osadzonych zgodnie z prawem.
func embeddingRestriction(fsType uint16) string {
	switch {
	case fsType&0x0002 != 0 && fsType&0x000c == 0:
		return fmt.Sprintf("embedding not permitted by the font license (OS/2 fsType 0x%04x: restricted license embedding)", fsType)
	case fsType&0x0200 != 0:
		return fmt.Sprintf("only bitmap embedding permitted by the font license (OS/2 fsType 0x%04x)", fsType)
	case fsType&0x0100 != 0:
		return fmt.Sprintf("subsetting not permitted by the font license (OS/2 fsType 0x%04x)", fsType)
	}
	return ""
}

func describePath(e entry) string {
	if e.Collection {
		return fmt.Sprintf("%s, font %d", e.Path, e.Index)
	}
	return e.Path
}

// load wczytuje font z dysku (raz na renderowanie) i zapisuje ostrzeżenie o użyciu fontu systemowego.
func (s *Session) load(i int) *font.Face {
	if face, ok := s.faces[i]; ok {
		return face
	}
	e := s.idx.entries[i]
	face, content, err := readFace(e)
	if err != nil {
		s.failed[i] = true
		s.faces[i] = nil
		s.chosen = map[faceKey]int{} // wybory z tym fontem trzeba powtórzyć
		s.byRune = map[runeKey]int{}
		s.warnings = append(s.warnings, fmt.Sprintf("system font skipped: %s (%s): %v", e.Family, describePath(e), err))
		return nil
	}
	s.faces[i] = face
	s.byFont[face.Font] = i
	s.contents[locationFile(e)] = content
	s.warnings = append(s.warnings, fmt.Sprintf(
		"system font used: %s (%s); add this file to Options.Fonts to make the output independent of this machine",
		e.Family, describePath(e)))
	return face
}

func locationFile(e entry) string {
	if e.Collection {
		return fmt.Sprintf("%s#%d", e.Path, e.Index)
	}
	return e.Path
}

// readFace wczytuje font i zwraca go razem z plikiem do osadzenia: font z kolekcji jest z niej
// wyjmowany do osobnego pliku, bo osadzanie (podzbiory) obsługuje pojedyncze fonty.
func readFace(e entry) (*font.Face, []byte, error) {
	data, err := os.ReadFile(e.Path)
	if err != nil {
		return nil, nil, err
	}
	loaders, err := ot.NewLoaders(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	if e.Index >= len(loaders) {
		return nil, nil, fmt.Errorf("font %d not found, the file changed after the scan", e.Index)
	}
	ld := loaders[e.Index]
	ft, err := font.NewFont(ld)
	if err != nil {
		return nil, nil, err
	}
	content := data
	if e.Collection {
		tags := ld.Tables()
		list := make([]ot.Table, 0, len(tags))
		for _, tag := range tags {
			raw, err := ld.RawTable(tag)
			if err != nil {
				return nil, nil, err
			}
			list = append(list, ot.Table{Tag: tag, Content: raw})
		}
		content = ot.WriteOpentype(list, ld.Type)
	}
	return font.NewFace(ft), content, nil
}

// Location: zob. text.SystemFonts.
func (s *Session) Location(ft *font.Font) (fontscan.Location, bool) {
	i, ok := s.byFont[ft]
	if !ok {
		return fontscan.Location{}, false
	}
	e := s.idx.entries[i]
	return fontscan.Location{File: locationFile(e), Index: uint16(e.Index)}, true
}

// Content: zob. text.SystemFonts.
func (s *Session) Content(location fontscan.Location) []byte { return s.contents[location.File] }

// Metadata: zob. text.SystemFonts.
func (s *Session) Metadata(ft *font.Font) (string, font.Aspect, bool) {
	i, ok := s.byFont[ft]
	if !ok {
		return "", font.Aspect{}, false
	}
	e := s.idx.entries[i]
	return e.Family, e.Aspect, true
}

// probeCmap to cmap sztucznego fontu w fontmapie probe: jeden znak „a”, którym pytamy o rodzinę.
type probeCmap struct{}

func (probeCmap) Iter() font.CmapIter { return &probeIter{} }
func (probeCmap) Lookup(r rune) (font.GID, bool) {
	if r == 'a' {
		return 1, true
	}
	return 0, false
}
func (probeCmap) RuneRanges(dst [][2]rune) [][2]rune { return append(dst[:0], [2]rune{'a', 'a'}) }

type probeIter struct{ done bool }

func (it *probeIter) Next() bool {
	if it.done {
		return false
	}
	it.done = true
	return true
}
func (it *probeIter) Char() (rune, font.GID) { return 'a', 1 }
