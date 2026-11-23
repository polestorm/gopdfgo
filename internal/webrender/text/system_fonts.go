package text

// gopdfgo: plik własny (ADR 0039). Punkt zaczepienia dla fontów systemowych: źródło fontów spoza
// Options.Fonts, używane dopiero wtedy, gdy fonty wywołującego nie mają rodziny albo znaku
// (CSS Fonts 4 § 5, font fallback). Samo źródło, czyli przeglądanie katalogów systemowych, jest
// w pakiecie internal/systemfonts, kompilowanym tylko ze znacznikiem systemfonts; bez niego pole
// FontConfigurationGotext.System zostaje puste i ścieżka poniżej nie jest używana.

import (
	"strings"
	"unicode"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
	"github.com/go-text/typesetting/shaping"
)

// SystemFonts to źródło fontów zastępczych (fonty systemowe). Implementacja zapamiętuje twarze,
// które zwróciła, i rozpoznaje je w Location, Content i Metadata.
type SystemFonts interface {
	// HasFamily mówi, czy źródło ma rodzinę family (wprost, przez zgodne metrycznie podstawienie
	// albo, dla rodziny generycznej, przez tablicę podstawień).
	HasFamily(family string) bool
	// FamilyFace zwraca twarz rodziny family najlepiej pasującą do aspect (CSS Fonts 4 § 5.2),
	// o ile zawiera znak r; w przeciwnym razie nil.
	FamilyFace(family string, aspect font.Aspect, r rune) *font.Face
	// RuneFace zwraca twarz zawierającą znak r: najpierw z rodzin z listy families, potem z dowolnej
	// rodziny źródła; nil, gdy żadna nie ma znaku.
	RuneFace(families []string, aspect font.Aspect, r rune) *font.Face
	// Location zwraca położenie twarzy zwróconej przez źródło; ok = false dla innych twarzy.
	Location(ft *font.Font) (location fontscan.Location, ok bool)
	// Content zwraca plik fontu (pojedynczy font, także wyjęty z kolekcji) do osadzenia.
	Content(location fontscan.Location) []byte
	// Metadata zwraca rodzinę i aspekt twarzy zwróconej przez źródło; ok = false dla innych twarzy.
	Metadata(ft *font.Font) (family string, aspect font.Aspect, ok bool)
}

// systemState (gopdfgo) to stan zapytania o fonty systemowe dla bieżącego zapytania fontmapy.
type systemState struct {
	lead     []string            // rodziny systemowe przed pierwszą rodziną dostępną u wywołującego
	families []string            // pełna lista rodzin zapytania
	aspect   font.Aspect         // aspekt zapytania
	leads    map[string][]string // pamięć lead według listy rodzin
}

// systemLayer (gopdfgo) to fontmapa dla segmentacji tekstu: fonty wywołującego (fontscan.FontMap),
// a za nimi fonty systemowe.
type systemLayer struct{ fc *FontConfigurationGotext }

// setQuery (gopdfgo) ustawia zapytanie fontmapy dla opisu fontu. Bez fontów systemowych to samo
// co fc.fm.SetQuery(fc.newQuery(fd)).
func (fc *FontConfigurationGotext) setQuery(fd FontDescription) {
	query := fc.newQuery(fd)
	if fc.System != nil {
		// przed fc.fm.SetQuery: sprawdzanie dostępności rodzin (familyAvailable) zmienia zapytanie fontmapy
		fc.systemQuery(query)
	}
	fc.fm.SetQuery(query)
}

// faces (gopdfgo) zwraca fontmapę do wyboru twarzy dla znaków: fc.fm albo, z fontami systemowymi,
// warstwę, która sięga po nie, gdy fonty wywołującego nie mają rodziny albo znaku.
func (fc *FontConfigurationGotext) faces() shaping.Fontmap {
	if len(fc.lazy) != 0 { // twarze bez pełnego fontu, zob. lazy_fonts.go
		if fc.System == nil {
			return lazyLayer{fc}
		}
		return lazySystemLayer{fc}
	}
	if fc.System == nil {
		return fc.fm
	}
	return systemLayer{fc}
}

// systemQuery wylicza rodziny systemowe, które według kolejności listy rodzin CSS poprzedzają
// pierwszą rodzinę dostępną w fontach wywołującego (CSS Fonts 4 § 5.1: używana jest pierwsza
// dostępna rodzina). Rodzina dostępna u wywołującego, także przez podstawienie zgodne metrycznie,
// ma zawsze pierwszeństwo przed systemową o tej samej nazwie.
func (fc *FontConfigurationGotext) systemQuery(query fontscan.Query) {
	state := &fc.system
	state.families, state.aspect = query.Families, query.Aspect
	key := strings.Join(query.Families, "\x00")
	if lead, ok := state.leads[key]; ok {
		state.lead = lead
		return
	}
	var lead []string
	callerFonts := len(fc.familyIndex.fontIDs) != 0 || len(fc.loadedFaces) != 0
	for _, family := range query.Families {
		lower := strings.ToLower(strings.TrimSpace(family))
		if lower == "" {
			continue
		}
		if callerFonts {
			if cssGenericFamilies[lower] || len(fc.GenericFamilies[lower]) != 0 || fc.familyAvailable(lower) {
				break
			}
		}
		if fc.System.HasFamily(family) {
			lead = append(lead, family)
		}
	}
	if !callerFonts && len(lead) == 0 {
		// bez fontów wywołującego i bez dostępnej rodziny: font domyślny, czyli rodzina serif
		lead = []string{"serif"}
	}
	if state.leads == nil {
		state.leads = map[string][]string{}
	}
	state.leads[key] = lead
	state.lead = lead
}

// systemEligible mówi, czy dla znaku warto szukać fontu systemowego: znaki widoczne, bez selektorów
// wariantu (należą do znaku przed nimi).
func systemEligible(r rune) bool {
	return unicode.IsGraphic(r) && !unicode.Is(unicode.Variation_Selector, r)
}

func (l systemLayer) ResolveFace(r rune) *font.Face {
	fc := l.fc
	state := &fc.system
	eligible := systemEligible(r)
	if eligible {
		for _, family := range state.lead {
			if face := fc.System.FamilyFace(family, state.aspect, r); face != nil {
				return face
			}
		}
	}
	face := fc.fm.ResolveFace(r)
	if !eligible {
		return face
	}
	if face != nil {
		if _, ok := face.NominalGlyph(r); ok {
			return face
		}
	}
	if system := fc.System.RuneFace(state.families, state.aspect, r); system != nil {
		return system
	}
	return face
}

// fontMetadata (gopdfgo) to fc.fm.FontMetadata, które zna też twarze fontów systemowych.
func (fc *FontConfigurationGotext) fontMetadata(ft *font.Font) (string, font.Aspect) {
	if fc.System != nil {
		if family, aspect, ok := fc.System.Metadata(ft); ok {
			return family, aspect
		}
	}
	return fc.fm.FontMetadata(ft)
}
