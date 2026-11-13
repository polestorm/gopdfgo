package test

import (
	"sync"

	"github.com/go-text/typesetting/fontscan"

	"github.com/polestorm/gopdfgo/internal/webrender/text"
)

var lock sync.Mutex

// LoadTestFontConfig loads the font index in [cacheDir],
// creating it if needed.
func LoadTestFontConfig(cacheDir string) (text.FontConfiguration, error) {
	lock.Lock()
	defer lock.Unlock()

	fm := fontscan.NewFontMap(nil)
	err := fm.UseSystemFonts(cacheDir)
	if err != nil {
		return nil, err
	}
	return text.NewFontConfigurationGotext(fm), nil
}
