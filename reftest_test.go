package gopdfgo

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Reftesty druku WPT: test przechodzi, gdy on i jego wzorzec wyrenderowane silnikiem dają te same strony.
// Wyniki są porównywane z testdata/wpt/baseline.tsv; test nie przechodzi, gdy coś, co było zaliczone,
// przestaje być zaliczone. Poprawa nie jest błędem, ale wymaga odświeżenia stanu bazowego:
//
//	GOPDFGO_UPDATE_BASELINE=1 go test -run TestWPTReftests .
const (
	baselinePath = "testdata/wpt/baseline.tsv"
	// 96 dpi: jeden piksel CSS to jeden piksel obrazu. Przy niższej rozdzielczości (pierwsze pomiary
	// robiono w 48 dpi) kształty o tej samej geometrii różnią się wygładzaniem krawędzi i testy
	// zgodne wizualnie nie przechodziły (docs/feature-audit.md).
	reftestDPI        = "96"
	channelTolerance  = 40
	maxDifferentPixel = 20
)

// Reguła porównania obrazów (docs/adr/0003-reftest-comparison.md). Strona testu zgadza się ze stroną
// wzorca, gdy różni się najwyżej maxDifferentPixel pikselami ("pass") albo gdy różniące się piksele
// nigdzie nie tworzą bloku 2x2, czyli leżą wyłącznie na liniach grubości jednego piksela wzdłuż
// krawędzi kształtów ("pass-edge"). Wynik "pass-edge" jest zapisywany osobno, żeby zawsze było widać,
// które testy przechodzą dzięki temu założeniu.
const (
	resultPass     = "pass"
	resultPassEdge = "pass-edge"
)

func passed(result string) bool { return result == resultPass || result == resultPassEdge }

// onlyEdgeDifferences mówi, czy różnice między stronami nigdzie nie tworzą bloku 2x2 piksele.
func onlyEdgeDifferences(a, b []byte) bool {
	headerA, dataA := splitPPM(a)
	headerB, dataB := splitPPM(b)
	if headerA != headerB || len(dataA) != len(dataB) {
		return false
	}
	fields := strings.Fields(headerA)
	if len(fields) < 2 {
		return false
	}
	width, err := strconv.Atoi(fields[1])
	if err != nil || width <= 0 {
		return false
	}
	pixels := len(dataA) / 3
	differs := make([]bool, pixels)
	for p := 0; p < pixels; p++ {
		for c := 0; c < 3; c++ {
			d := int(dataA[3*p+c]) - int(dataB[3*p+c])
			if d > channelTolerance || d < -channelTolerance {
				differs[p] = true
				break
			}
		}
	}
	for p := 0; p+width+1 < pixels; p++ {
		if differs[p] && p%width != width-1 && differs[p+1] && differs[p+width] && differs[p+width+1] {
			return false
		}
	}
	return true
}

type rendered struct {
	pages [][]byte // strony jako PPM
	err   error
}

func rasterize(t *testing.T, pdf []byte, dir, name string) [][]byte {
	t.Helper()
	file := filepath.Join(dir, name+".pdf")
	if err := os.WriteFile(file, pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(dir, name)
	if out, err := exec.Command("pdftoppm", "-r", reftestDPI, file, prefix).CombinedOutput(); err != nil {
		t.Fatalf("pdftoppm: %v: %s", err, out)
	}
	files, _ := filepath.Glob(prefix + "-*.ppm")
	sort.Slice(files, func(i, j int) bool { return pageIndex(files[i]) < pageIndex(files[j]) })
	pages := make([][]byte, len(files))
	for i, f := range files {
		pages[i], _ = os.ReadFile(f)
		os.Remove(f)
	}
	os.Remove(file)
	return pages
}

func pageIndex(file string) int {
	base := strings.TrimSuffix(filepath.Base(file), ".ppm")
	n, _ := strconv.Atoi(base[strings.LastIndexByte(base, '-')+1:])
	return n
}

// differentPixels liczy piksele różniące się ponad tolerancję; -1 oznacza inny rozmiar strony.
func differentPixels(a, b []byte) int {
	headerA, dataA := splitPPM(a)
	headerB, dataB := splitPPM(b)
	if headerA != headerB || len(dataA) != len(dataB) {
		return -1
	}
	if bytes.Equal(dataA, dataB) {
		return 0
	}
	count := 0
	for i := 0; i+2 < len(dataA); i += 3 {
		for c := 0; c < 3; c++ {
			d := int(dataA[i+c]) - int(dataB[i+c])
			if d > channelTolerance || d < -channelTolerance {
				count++
				break
			}
		}
	}
	return count
}

// splitPPM oddziela nagłówek "P6 szer wys maks" od danych.
func splitPPM(ppm []byte) (string, []byte) {
	fields, pos := 0, 0
	for pos < len(ppm) && fields < 4 {
		for pos < len(ppm) && ppm[pos] != ' ' && ppm[pos] != '\n' {
			pos++
		}
		fields++
		pos++
	}
	if pos > len(ppm) {
		return "", nil
	}
	return string(ppm[:pos]), ppm[pos:]
}

func TestWPTReftests(t *testing.T) {
	if testing.Short() {
		t.Skip("pomijane w trybie -short")
	}
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("brak pdftoppm")
	}
	manifest, err := os.Open("testdata/wpt/manifest.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()
	reader := csv.NewReader(manifest)
	reader.Comma = '\t'
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	cache := map[string]rendered{}
	render := func(name string) rendered {
		if r, ok := cache[name]; ok {
			return r
		}
		pdf, err := renderFile(filepath.Join("testdata/wpt", name), "testdata/wpt", printPageCSS)
		r := rendered{err: err}
		if err == nil {
			r.pages = rasterize(t, pdf, dir, strconv.Itoa(len(cache)))
		}
		cache[name] = r
		return r
	}

	type outcome struct{ result, topic, detail string }
	results := map[string]outcome{}
	var names []string
	for _, row := range rows[1:] {
		tier, topic, name, kind, references := row[0], row[1], row[2], row[3], row[4]
		if tier != "paged" {
			continue
		}
		names = append(names, name)
		test := render(name)
		if test.err != nil {
			results[name] = outcome{"error", topic, firstLine(test.err)}
			continue
		}
		result, detail := resultPass, ""
		for _, reference := range strings.Split(references, ";") {
			ref := render(reference)
			if ref.err != nil {
				result, detail = "ref-error", firstLine(ref.err)
				break
			}
			equal := len(test.pages) == len(ref.pages)
			if !equal {
				detail = fmt.Sprintf("strony %d vs %d", len(test.pages), len(ref.pages))
			} else {
				for i := range test.pages {
					if n := differentPixels(test.pages[i], ref.pages[i]); n < 0 || n > maxDifferentPixel {
						// Dla testów "mismatch" obowiązuje tylko miara ścisła: różnica musi być wyraźna.
						if kind == "match" && n > 0 && onlyEdgeDifferences(test.pages[i], ref.pages[i]) {
							result = resultPassEdge
							continue
						}
						equal, detail = false, fmt.Sprintf("strona %d: różne piksele %d", i+1, n)
						break
					}
				}
			}
			if equal != (kind == "match") {
				result = "fail"
				break
			}
			detail = ""
		}
		results[name] = outcome{result, topic, detail}
	}

	baseline := map[string]string{}
	if raw, err := os.ReadFile(baselinePath); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n")[1:] {
			if fields := strings.Split(line, "\t"); len(fields) >= 3 {
				baseline[fields[2]] = fields[0]
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}

	totals := map[string]int{}
	var regressions, improvements []string
	for _, name := range names {
		got := results[name].result
		totals[got]++
		switch was := baseline[name]; {
		case passed(was) && !passed(got):
			regressions = append(regressions, name+" ("+results[name].detail+")")
		case was != "" && !passed(was) && passed(got):
			improvements = append(improvements, name)
		}
	}
	t.Logf("zaliczone %d (ściśle %d, z dokładnością do krawędzi %d), niezaliczone %d, błąd %d, błąd wzorca %d, razem %d",
		totals[resultPass]+totals[resultPassEdge], totals[resultPass], totals[resultPassEdge],
		totals["fail"], totals["error"], totals["ref-error"], len(names))

	if os.Getenv("GOPDFGO_UPDATE_BASELINE") != "" {
		var out bytes.Buffer
		out.WriteString("result\ttopic\ttest\tdetail\n")
		for _, name := range names {
			o := results[name]
			fmt.Fprintf(&out, "%s\t%s\t%s\t%s\n", o.result, o.topic, name, o.detail)
		}
		if err := os.WriteFile(baselinePath, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("zapisano %s", baselinePath)
		return
	}
	if len(baseline) == 0 {
		t.Fatalf("brak %s; utwórz przez GOPDFGO_UPDATE_BASELINE=1", baselinePath)
	}
	if len(improvements) != 0 {
		t.Logf("%d testów zaliczonych ponad stan bazowy; odśwież %s. Np. %s", len(improvements), baselinePath,
			strings.Join(improvements[:min(5, len(improvements))], ", "))
	}
	if len(regressions) != 0 {
		t.Errorf("%d testów przestało przechodzić:\n\t%s", len(regressions), strings.Join(regressions, "\n\t"))
	}
}

func firstLine(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return line
}
