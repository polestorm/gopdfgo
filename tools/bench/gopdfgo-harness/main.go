// gopdfgo-harness renders one HTML file with gopdfgo for the comparison with folio and Chromium
// (tools/bench/compare.py, docs/benchmarks.md).
//
//	gopdfgo-harness -in doc.html -root dir -fonts dir -out doc.pdf [-n 20] [-serif-tinos] [-cpuprofile f] [-memprofile f] [-cache]
//
// Wejście przygotowane tak samo jak w folio-harness: url(../fonts/...) -> url(fonts/...), "fonts/" mapowane na -fonts,
// reszta ścieżek na -root. Fonty w Options.Fonts jak w poleceniu gopdfgo z -fonts testdata/fonts, ograniczone do
// katalogu głównego, tinos i arimo (rodziny generyczne z families.json).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"time"

	"github.com/polestorm/gopdfgo/htmlpdf"
)

type mapFS struct{ root, fonts fs.FS }

func (m mapFS) Open(name string) (fs.File, error) {
	if rest, ok := strings.CutPrefix(name, "fonts/"); ok {
		return m.fonts.Open(rest)
	}
	return m.root.Open(name)
}

const tinosCSS = `<style>
@font-face { font-family: "Tinos"; src: url("fonts/tinos/Tinos-Regular.ttf"); font-weight: 400; font-style: normal; }
@font-face { font-family: "Tinos"; src: url("fonts/tinos/Tinos-Bold.ttf"); font-weight: 700; font-style: normal; }
@font-face { font-family: "Tinos"; src: url("fonts/tinos/Tinos-Italic.ttf"); font-weight: 400; font-style: italic; }
@font-face { font-family: "Tinos"; src: url("fonts/tinos/Tinos-BoldItalic.ttf"); font-weight: 700; font-style: italic; }
html { font-family: "Tinos"; }
</style>`

func prepare(html string, serif bool) string {
	for _, q := range []string{`"`, `'`, ``} {
		html = strings.ReplaceAll(html, "url("+q+"../fonts/", "url("+q+"fonts/")
	}
	if serif {
		if i := strings.Index(strings.ToLower(html), "<head>"); i >= 0 {
			html = html[:i+6] + tinosCSS + html[i+6:]
		}
	}
	return html
}

func main() {
	in := flag.String("in", "", "input HTML file")
	root := flag.String("root", "", "directory for relative resources")
	fonts := flag.String("fonts", "", "font directory (testdata/fonts)")
	out := flag.String("out", "", "output PDF")
	n := flag.Int("n", 0, "number of warm renders to time (0 = one render)")
	serif := flag.Bool("serif-tinos", false, "add @font-face Tinos and use it for the whole document")
	cpu := flag.String("cpuprofile", "", "write a CPU profile of the warm renders")
	mem := flag.String("memprofile", "", "write an allocation profile")
	cache := flag.Bool("cache", false, "keep parsed fonts between renders (htmlpdf.FontCache, ADR 0045)")
	flag.Parse()
	data, err := os.ReadFile(*in)
	if err != nil {
		panic(err)
	}
	html := prepare(string(data), *serif)
	fsys := mapFS{os.DirFS(*root), os.DirFS(*fonts)}
	var fontList []htmlpdf.Font
	for _, pattern := range []string{"*.ttf", "tinos/*.ttf", "arimo/*.ttf"} {
		files, _ := filepath.Glob(filepath.Join(*fonts, pattern))
		for _, file := range files {
			b, _ := os.ReadFile(file)
			fontList = append(fontList, htmlpdf.Font{Name: filepath.Base(file), Data: b})
		}
	}
	generic := map[string][]string{}
	if b, err := os.ReadFile(filepath.Join(*fonts, "families.json")); err == nil {
		json.Unmarshal(b, &generic)
	}
	resolver := func(target string) (htmlpdf.Resource, error) {
		u, err := url.Parse(target)
		if err != nil || u.Scheme != "file" || !strings.HasPrefix(u.Path, "/virtual/") {
			return htmlpdf.DenyAll(target)
		}
		b, err := fs.ReadFile(fsys, strings.TrimPrefix(u.Path, "/virtual/"))
		if err != nil {
			return htmlpdf.Resource{}, err
		}
		return htmlpdf.Resource{Data: b}, nil
	}
	var warns int64
	var msgs []string
	pages := 0
	input := []byte(html)
	opts := htmlpdf.Options{Fonts: fontList, Resolver: resolver,
		BaseURL: "file:///virtual/" + filepath.Base(*in), GenericFamilies: generic}
	if *cache {
		opts.FontCache = htmlpdf.NewFontCache(256 << 20)
	}
	render := func() ([]byte, error) {
		r, err := htmlpdf.Render(context.Background(), input, opts)
		if err != nil {
			return nil, err
		}
		warns = int64(len(r.Warnings))
		pages = r.Pages
		msgs = msgs[:0]
		for i, w := range r.Warnings {
			if i < 5 {
				msgs = append(msgs, w.Message)
			}
		}
		return r.PDF, nil
	}
	start := time.Now()
	pdf, err := render()
	if *cache && *n > 0 {
		// kontrola: wynik z ciepłej pamięci podręcznej bajt w bajt taki sam jak z pierwszego renderowania
		again, err := render()
		if err != nil || string(again) != string(pdf) {
			fmt.Fprintln(os.Stderr, "cache: different output", err)
			os.Exit(3)
		}
	}
	first := time.Since(start)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	firstWarns := warns
	firstMsgs := append([]string(nil), msgs...)
	var times []float64
	if *n > 0 {
		if *cpu != "" {
			f, _ := os.Create(*cpu)
			pprof.StartCPUProfile(f)
		}
		for i := 0; i < *n; i++ {
			t := time.Now()
			if _, err := render(); err != nil {
				panic(err)
			}
			times = append(times, float64(time.Since(t).Microseconds())/1000)
		}
		if *cpu != "" {
			pprof.StopCPUProfile()
		}
		if *mem != "" {
			f, _ := os.Create(*mem)
			pprof.Lookup("allocs").WriteTo(f, 0)
			f.Close()
		}
	}
	sort.Float64s(times)
	med := 0.0
	if len(times) > 0 {
		med = times[len(times)/2]
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	if *out != "" {
		os.WriteFile(*out, pdf, 0o644)
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"bytes": len(pdf), "pages": pages, "first_ms": float64(first.Microseconds()) / 1000,
		"median_ms": med, "warnings": firstWarns, "samples": firstMsgs, "total_alloc_mb": float64(ms.TotalAlloc) / 1e6})
}
