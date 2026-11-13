// Polecenie gopdfgo zamienia plik HTML na PDF.
//
//	gopdfgo -in dokument.html -out dokument.pdf -fonts katalog-fontów [-root katalog-zasobów]
//
// Fonty bierze wyłącznie z katalogu -fonts (*.ttf, *.otf). Zasoby wskazane w dokumencie czyta tylko
// spod -root (domyślnie katalog pliku wejściowego); wszystko inne jest odrzucane z ostrzeżeniem.
// Adres bezwzględny w dokumencie (/style/a.css) liczy się od -root, jak na serwerze WWW.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/polestorm/gopdfgo"
)

func main() {
	in := flag.String("in", "", "plik HTML")
	out := flag.String("out", "", "plik PDF")
	fontDir := flag.String("fonts", "", "katalog z fontami (*.ttf, *.otf)")
	root := flag.String("root", "", "katalog, spod którego wolno czytać zasoby (domyślnie katalog pliku wejściowego)")
	timeout := flag.Duration("timeout", time.Minute, "limit czasu")
	userCSS := flag.String("css", "", "arkusz użytkownika, np. \"@page { size: A4 }\"")
	quiet := flag.Bool("quiet", false, "nie wypisuj ostrzeżeń")
	flag.Parse()
	if *in == "" || *out == "" || *fontDir == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*in, *out, *fontDir, *root, *userCSS, *timeout, *quiet); err != nil {
		fmt.Fprintln(os.Stderr, "gopdfgo:", err)
		os.Exit(1)
	}
}

func run(in, out, fontDir, root, userCSS string, timeout time.Duration, quiet bool) error {
	html, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(in)
	if err != nil {
		return err
	}
	if root == "" {
		root = filepath.Dir(absolute)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}

	var fonts []gopdfgo.Font
	for _, pattern := range []string{"*.ttf", "*.otf"} {
		files, _ := filepath.Glob(filepath.Join(fontDir, pattern))
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			fonts = append(fonts, gopdfgo.Font{Name: filepath.Base(file), Data: data})
		}
	}

	resolver := func(target string) (gopdfgo.Resource, error) {
		u, err := url.Parse(target)
		if err != nil || u.Scheme != "file" {
			return gopdfgo.DenyAll(target)
		}
		file := filepath.Clean(u.Path)
		if !strings.HasPrefix(file, root+string(filepath.Separator)) {
			file = filepath.Join(root, file) // adres bezwzględny liczony od -root
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return gopdfgo.Resource{}, err
		}
		return gopdfgo.Resource{Data: data}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := gopdfgo.Render(ctx, html, gopdfgo.Options{Fonts: fonts, Resolver: resolver, BaseURL: "file://" + absolute, UserCSS: userCSS})
	if err != nil {
		return err
	}
	if !quiet {
		for _, warning := range result.Warnings {
			fmt.Fprintln(os.Stderr, "ostrzeżenie:", warning.Message)
		}
	}
	fmt.Fprintf(os.Stderr, "%d stron, %d B, %s %s\n", result.Pages, len(result.PDF), result.EngineName, result.EngineVersion)
	return os.WriteFile(out, result.PDF, 0o644)
}
