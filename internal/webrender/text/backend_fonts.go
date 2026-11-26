package text

// gopdfgo: plik dodany. Obiekty fontów backendu na czas życia konfiguracji fontów.

import (
	"sync"

	"github.com/go-text/typesetting/font"
)

// backendFontSet trzyma obiekty fontów backendu utworzone dla twarzy tej konfiguracji. Zabezpieczony
// zamkiem, bo rysowanie tekstu nie musi biec w wątku układu.
type backendFontSet struct {
	lock  sync.Mutex
	fonts map[*font.Face]any
}

// BackendFont (gopdfgo) zwraca obiekt fontu backendu dla twarzy face: utworzony przez create przy
// pierwszym wywołaniu, potem ten sam. Backend rozpoznaje fonty po wartości interfejsu, więc jeden
// obiekt na twarz to jeden font osadzony w PDF. Obiekty żyją tak długo jak konfiguracja (w gopdfgo
// jedno renderowanie); dawna mapa globalna w pakiecie draw nigdy ich nie zwalniała, a twarze są nowe
// w każdym renderowaniu, więc proces tracił sparsowane fonty każdego dokumentu.
func (f *FontConfigurationGotext) BackendFont(face *font.Face, create func() any) any {
	set := f.backendFonts
	set.lock.Lock()
	defer set.lock.Unlock()
	if v, ok := set.fonts[face]; ok {
		return v
	}
	v := create()
	set.fonts[face] = v
	return v
}
