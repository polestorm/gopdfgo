// folio-harness renders one HTML file with folio (github.com/carlos7ags/folio) for the comparison with
// gopdfgo (tools/bench/compare.py, docs/benchmarks.md). It is a separate module, so folio is not a
// dependency of gopdfgo.
//
//	folio-harness -in doc.html -root dir -fonts dir -out doc.pdf [-n 20] [-serif-tinos] [-cpuprofile f] [-memprofile f]
//
// Wejście przygotowane tak samo jak w gopdfgo-harness: url(../fonts/...) -> url(fonts/...), "fonts/" mapowane na -fonts,
// reszta ścieżek na -root. Bez sieci (AllowRemoteFetch false).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/carlos7ags/folio/document"
	foliohtml "github.com/carlos7ags/folio/html"
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

type countHandler struct {
	n    *atomic.Int64
	msgs *[]string
}

func (h countHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h countHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		h.n.Add(1)
		if len(*h.msgs) < 5 {
			s := r.Message
			r.Attrs(func(a slog.Attr) bool { s += " " + a.String(); return true })
			*h.msgs = append(*h.msgs, s)
		}
	}
	return nil
}
func (h countHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h countHandler) WithGroup(string) slog.Handler      { return h }

func main() {
	in := flag.String("in", "", "input HTML file")
	root := flag.String("root", "", "directory for relative resources")
	fonts := flag.String("fonts", "", "font directory (testdata/fonts)")
	out := flag.String("out", "", "output PDF")
	n := flag.Int("n", 0, "number of warm renders to time (0 = one render)")
	serif := flag.Bool("serif-tinos", false, "add @font-face Tinos and use it for the whole document")
	cpu := flag.String("cpuprofile", "", "write a CPU profile of the warm renders")
	mem := flag.String("memprofile", "", "write an allocation profile")
	flag.Parse()
	data, err := os.ReadFile(*in)
	if err != nil {
		panic(err)
	}
	html := prepare(string(data), *serif)
	fsys := mapFS{os.DirFS(*root), os.DirFS(*fonts)}
	var warns atomic.Int64
	var msgs []string
	render := func() ([]byte, error) {
		opts := &foliohtml.Options{BaseFS: fsys, FallbackFontPath: "fonts/NotoSans-Regular.ttf",
			PageWidth: 595.28, PageHeight: 841.89, Logger: slog.New(countHandler{&warns, &msgs})}
		doc := document.NewDocument(document.PageSizeA4)
		if err := doc.AddHTML(html, opts); err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if _, err := doc.WriteTo(&buf); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	start := time.Now()
	pdf, err := render()
	first := time.Since(start)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	firstWarns := warns.Load()
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
	json.NewEncoder(os.Stdout).Encode(map[string]any{"bytes": len(pdf), "first_ms": float64(first.Microseconds()) / 1000,
		"median_ms": med, "warnings": firstWarns, "samples": msgs, "total_alloc_mb": float64(ms.TotalAlloc) / 1e6})
}
