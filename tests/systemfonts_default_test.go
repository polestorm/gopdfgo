//go:build !systemfonts

package tests

// Wariant domyślny, bez znacznika systemfonts (ADR 0039): fonty systemowe nie są dostępne, a binarka
// nie zawiera kodu przeglądania katalogów fontów systemu.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/polestorm/gopdfgo"
)

// Options.SystemFonts w wariancie domyślnym kończy Render jawnym błędem, także bez Options.Fonts.
func TestSystemFontsNotBuilt(t *testing.T) {
	for _, opts := range []gopdfgo.Options{
		{Fonts: testFonts(t), SystemFonts: true},
		{SystemFonts: true},
	} {
		if _, err := gopdfgo.Render(t.Context(), []byte("<p>tekst</p>"), opts); !errors.Is(err, gopdfgo.ErrSystemFontsNotBuilt) {
			t.Errorf("błąd: %v, oczekiwany ErrSystemFontsNotBuilt", err)
		}
	}
}

// systemScanSymbols to symbole kodu przeglądania katalogów fontów systemu: pakiet internal/systemfonts
// oraz skanowanie w go-text (fontscan), które linker dołączał, dopóki text/template wyłączał usuwanie
// nieużywanych metod (ADR 0039).
var systemScanSymbols = regexp.MustCompile(`gopdfgo/internal/systemfonts\.|fontscan\.\(\*FontMap\)\.UseSystemFonts|fontscan\.DefaultFontDirectories|fontscan\.scanFontFootprints|fontscan\.SystemFonts`)

// Binarka domyślna nie zawiera kodu przeglądania katalogów fontów systemu (go tool nm).
func TestDefaultBinaryWithoutSystemFontScan(t *testing.T) {
	if testing.Short() {
		t.Skip("buduje binarkę")
	}
	binary := filepath.Join(t.TempDir(), "gopdfgo")
	build := exec.Command("go", "build", "-o", binary, "../cmd/gopdfgo")
	build.Env = append(os.Environ(), "GOFLAGS=")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
	symbols, err := exec.Command("go", "tool", "nm", binary).Output()
	if err != nil {
		t.Fatalf("go tool nm: %v", err)
	}
	if found := systemScanSymbols.FindAll(symbols, 5); len(found) > 0 {
		t.Errorf("binarka domyślna zawiera kod fontów systemowych: %q", found)
	}
}
