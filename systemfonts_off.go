//go:build !systemfonts

package gopdfgo

// Wariant domyślny (ADR 0039): bez kodu przeglądania katalogów fontów systemu. Options.SystemFonts
// daje błąd ErrSystemFontsNotBuilt.

const systemFontsBuilt = false

func openSystemFonts() (systemFontSource, error) {
	return nil, ErrSystemFontsNotBuilt
}
