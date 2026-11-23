package text

// gopdfgo: plik dodany (ADR 0047). Fonty rejestrowane w fontmapie bez pełnego parsowania.
//
// Do rejestracji twarzy w fontscan.FontMap (AddFace) potrzebne są tylko cmap (pokrycie znaków w odcisku
// fontu, newFootprintFromFont) oraz rodzina i odmiana. Twarz dostaje więc najpierw font „pusty”: strukturę
// font.Font z samym polem Cmap ze streszczenia (FontCache.Summary). Pełny font (glyf, CFF, GSUB, GPOS,
// metryki) jest parsowany dopiero wtedy, gdy wybór twarzy dla znaku (faces) zwraca tę twarz: wtedy
// struktura fontu pod tym samym wskaźnikiem jest nadpisywana pełnym fontem, a twarz dostaje nowe pamięci
// podręczne glifów (font.NewFace), jak przy twarzy utworzonej od razu z pełnego fontu. Wskaźnik
// *font.Font się nie zmienia, więc mapy rozpoznające twarze po wskaźniku (fontmapa, familyIndex,
// fontsFeatures, fontsMetrics) zostają ważne.
//
// Twarz pusta wychodzi poza ten plik tylko przez fontmapę: wybór twarzy do kształtowania i metryk idzie
// zawsze przez faces (system_fonts.go), a jedyne inne odwołanie do fc.fm.ResolveFace (familyAvailable)
// czyta tylko położenie i opis twarzy, które fontmapa zna z rejestracji.

import (
	"fmt"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
)

// LazyFontsDisabled wyłącza odkładanie parsowania: font jest parsowany w całości przy rejestracji, jak
// przed ADR 0047 (testy porównujące wynik, pomiary).
var LazyFontsDisabled bool

// OnFontParsed, jeśli ustawione, jest wywoływane przy każdym pełnym parsowaniu pliku fontu (testy, pomiary).
var OnFontParsed func(content []byte)

// lazyFont to twarz zarejestrowana z fontem pustym: treść pliku i numer fontu w kolekcji.
type lazyFont struct {
	face    *font.Face
	content []byte
	index   int
}

// fontCache zwraca pamięć fontów konfiguracji; bez Cache (testy przejęte) własną na czas życia konfiguracji.
func (f *FontConfigurationGotext) fontCache() *FontCache {
	if f.Cache != nil {
		return f.Cache
	}
	if f.ownCache == nil {
		f.ownCache = NewTransientFontCache()
	}
	return f.ownCache
}

// AddFontFile (gopdfgo) dodaje do fontmapy wszystkie fonty pliku content (kilka dla kolekcji TTC) pod
// położeniem fileID, tak jak fontscan.FontMap.AddFace z fontem sparsowanym od razu: z rodziną i odmianą
// odczytanymi z fontu, w kolejności fontów pliku. Font jest parsowany w całości przy pierwszym użyciu.
// Zapamiętuje też treść do osadzenia (RegisterFontContent).
func (f *FontConfigurationGotext) AddFontFile(fileID string, content []byte) error {
	summaries, err := f.fontCache().Summary(content)
	if err != nil {
		return fmt.Errorf("unsupported font resource: %s", err)
	}
	for i, summary := range summaries {
		face := f.addLazyFace(content, i, summary.Cmap)
		f.fm.AddFace(face, fontscan.Location{File: fileID, Index: uint16(i)}, summary.Description)
	}
	f.RegisterFontContent(fileID, content)
	return nil
}

// addLazyFace tworzy twarz z fontem pustym o podanym cmap; pełny font to font index pliku content.
func (f *FontConfigurationGotext) addLazyFace(content []byte, index int, cmap font.Cmap) *font.Face {
	face := font.NewFace(&font.Font{Cmap: cmap})
	if f.lazy == nil {
		f.lazy = map[*font.Font]lazyFont{}
	}
	f.lazy[face.Font] = lazyFont{face: face, content: content, index: index}
	if LazyFontsDisabled {
		f.loadFace(face)
	}
	return face
}

// loadFace parsuje w całości font twarzy zarejestrowanej z fontem pustym (nic nie robi dla innych twarzy).
// Pole Cmap zostaje to z rejestracji: ma tę samą treść co cmap pełnego fontu (to samo wywołanie
// font.ProcessCmap), a dla reguły @font-face z unicode-range jest opakowane w rangeCmap.
func (f *FontConfigurationGotext) loadFace(face *font.Face) {
	lazy, ok := f.lazy[face.Font]
	if !ok {
		return
	}
	delete(f.lazy, face.Font)
	parsed, err := f.fontCache().Parse(lazy.content)
	if err != nil || lazy.index >= len(parsed) {
		// nie powinno się zdarzyć: streszczenie sprawdza te same tablice wymagane co pełne parsowanie
		logger.WarningLogger.Printf("font parsing failed after its summary was read: %v", err)
		return
	}
	shell := face.Font
	cmap := shell.Cmap
	*shell = *parsed[lazy.index].Font
	shell.Cmap = cmap
	*face = *font.NewFace(shell)
}

// lazyLayer to fontmapa dla wyboru twarzy, która przed zwróceniem twarzy parsuje jej font w całości
// (loadFace). Używana, dopóki są twarze z fontem pustym. Jak fontscan.FontMap przyjmuje pismo
// (shaping.FontmapScript): segmentacja tekstu ustawia je przed wyborem twarzy, a od pisma zależy wybór
// fontu zastępczego.
type lazyLayer struct{ fc *FontConfigurationGotext }

func (l lazyLayer) ResolveFace(r rune) *font.Face {
	face := l.fc.fm.ResolveFace(r)
	if face != nil {
		l.fc.loadFace(face)
	}
	return face
}

func (l lazyLayer) SetScript(script language.Script) { l.fc.fm.SetScript(script) }

// lazySystemLayer to lazyLayer nad warstwą fontów systemowych (systemLayer). Ta pisma nie przyjmuje, więc
// i lazySystemLayer go nie przyjmuje: segmentacja ma działać tak samo jak z samą systemLayer.
type lazySystemLayer struct{ fc *FontConfigurationGotext }

func (l lazySystemLayer) ResolveFace(r rune) *font.Face {
	face := systemLayer{l.fc}.ResolveFace(r)
	if face != nil {
		l.fc.loadFace(face)
	}
	return face
}

var (
	_ shaping.FontmapScript = lazyLayer{}
	_ shaping.Fontmap       = lazySystemLayer{}
)
