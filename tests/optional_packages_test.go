package tests

// Pakiety opcjonalne (ADR 0049): rdzeń ich nie importuje, a binarka cmd/gopdfgo nie zawiera ich kodu.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// optionalPackages to pakiety funkcji opcjonalnych modułu (z podpakietami, np. sign/keyfile).
var optionalPackages = []string{"github.com/polestorm/gopdfgo/sign"}

// Rdzeń i polecenie gopdfgo nie zależą od pakietów opcjonalnych (go list -deps).
func TestCoreDoesNotImportOptionalPackages(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "../htmlpdf", "../cmd/gopdfgo").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	deps := strings.Fields(string(out))
	for _, pkg := range optionalPackages {
		for _, dep := range deps {
			if dep == pkg || strings.HasPrefix(dep, pkg+"/") {
				t.Errorf("rdzeń zależy od pakietu opcjonalnego %s", dep)
			}
		}
	}
}

// Binarka cmd/gopdfgo nie zawiera kodu pakietu sign ani jego podpakietów (go tool nm); podpis z wiersza
// poleceń to osobny program cmd/gopdfgo-sign (ADR 0051).
func TestDefaultBinaryWithoutSignPackage(t *testing.T) {
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
	if found := regexp.MustCompile(`gopdfgo/sign[./]`).FindAll(symbols, 5); len(found) > 0 {
		t.Errorf("binarka cmd/gopdfgo zawiera kod pakietu sign: %q", found)
	}
	// kontrola samego testu: binarka gopdfgo-sign kod podpisu zawiera
	signBinary := filepath.Join(t.TempDir(), "gopdfgo-sign")
	build = exec.Command("go", "build", "-o", signBinary, "../cmd/gopdfgo-sign")
	build.Env = append(os.Environ(), "GOFLAGS=")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
	symbols, err = exec.Command("go", "tool", "nm", signBinary).Output()
	if err != nil {
		t.Fatalf("go tool nm: %v", err)
	}
	for _, pkg := range []string{"gopdfgo/sign.", "gopdfgo/sign/keyfile."} {
		if !bytes.Contains(symbols, []byte(pkg)) {
			t.Errorf("binarka gopdfgo-sign bez symboli %s: test nie wykryłby kodu podpisu", pkg)
		}
	}
}
