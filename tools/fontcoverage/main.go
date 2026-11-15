// Wypisuje zakresy znaków Unicode, dla których którykolwiek z podanych fontów (plików albo katalogów
// z plikami *.ttf i *.otf, także w podkatalogach) ma glif. Wynik, wiersze "od-do" szesnastkowo, służy
// do wyboru testów: test z literami spoza tych zakresów wymaga fontu, którego zestaw testowy nie ma.
//
//	go run ./tools/fontcoverage testdata/fonts testdata/wpt/fonts/Ahem.ttf > testdata/fonts/coverage.txt
package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-text/typesetting/font"
)

func main() {
	covered := map[rune]bool{}
	for _, root := range os.Args[1:] {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			if ext := strings.ToLower(filepath.Ext(path)); ext != ".ttf" && ext != ".otf" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			face, err := font.ParseTTF(bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			for iter := face.Cmap.Iter(); iter.Next(); {
				r, _ := iter.Char()
				covered[r] = true
			}
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "fontcoverage:", err)
			os.Exit(1)
		}
	}
	runes := make([]rune, 0, len(covered))
	for r := range covered {
		runes = append(runes, r)
	}
	sort.Slice(runes, func(i, j int) bool { return runes[i] < runes[j] })
	for i := 0; i < len(runes); {
		j := i
		for j+1 < len(runes) && runes[j+1] == runes[j]+1 {
			j++
		}
		fmt.Printf("%04X-%04X\n", runes[i], runes[j])
		i = j + 1
	}
}
