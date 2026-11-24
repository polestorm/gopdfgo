//go:build systemfonts

package htmlpdf

// Wariant ze znacznikiem systemfonts (ADR 0039): fonty systemowe jako fonty zastępcze.

import "github.com/polestorm/gopdfgo/internal/systemfonts"

const systemFontsBuilt = true

func openSystemFonts() (systemFontSource, error) {
	return systemfonts.Open(), nil
}
