package svg

import (
	"fmt"
	"strings"
	"sync/atomic"

	pa "github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/css/validation"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

// Reguły @font-face samodzielnego dokumentu SVG (gopdfgo).
//
// Dokument SVG rysowany jako obraz albo przez <object> jest osobnym dokumentem: jego reguły @font-face
// dotyczą tylko jego tekstu (CSS Fonts 4 § 4.1), a w trybie obrazu (<img>, obraz CSS) dokument nie
// wczytuje zasobów zewnętrznych, więc działają tylko źródła data: (SVG Integration § 4.2, tryb
// „secure static”; HTML § 4.8.4.4; test WPT text-font-face-load-image). Źródło pomijało reguły
// @font-face w SVG. Twarze trafiają do konfiguracji fontów gospodarza pod nazwami rodzin
// zawężonymi do obrazu i trybu (scopedFamily), więc nie są widoczne w dokumencie HTML ani w innych
// obrazach; tekst obrazu przy składzie zamienia nazwy rodzin na zawężone (mapFontFamilies).

// svgFontScopeCounter numeruje dokumenty SVG z regułami @font-face (deterministycznie w kolejności
// wczytywania)
var svgFontScopeCounter atomic.Int64

// svgFontFaces to reguły @font-face dokumentu z identyfikatorem zawężenia.
type svgFontFaces struct {
	scope int64
	faces []validation.FontFaceDescriptors
	// families: rodziny opisane regułami (małymi literami)
	families utils.Set
	// registered: konfiguracje fontów i tryby, w których twarze są już dodane
	registered map[registeredFonts]bool
	urlFetcher utils.UrlFetcher
}

type registeredFonts struct {
	fonts    text.FontConfiguration
	document bool
}

// collectFontFaces zbiera reguły @font-face z arkuszy dokumentu SVG.
func collectFontFaces(stylesheets [][]byte, baseURL string, urlFetcher utils.UrlFetcher) *svgFontFaces {
	var out svgFontFaces
	for _, css := range stylesheets {
		for _, rule := range pa.ParseStylesheetBytes(css, true, true) {
			at, ok := rule.(pa.AtRule)
			if !ok || utils.AsciiLower(at.AtKeyword) != "font-face" || at.Content == nil {
				continue
			}
			desc := validation.PreprocessFontFaceDescriptors(baseURL, pa.ParseBlocksContents(at.Content, false))
			if desc.Src == nil || desc.FontFamily == "" {
				continue
			}
			out.faces = append(out.faces, desc)
		}
	}
	if len(out.faces) == 0 {
		return nil
	}
	out.scope = svgFontScopeCounter.Add(1)
	out.families = utils.Set{}
	for _, f := range out.faces {
		out.families.Add(strings.ToLower(string(f.FontFamily)))
	}
	out.registered = map[registeredFonts]bool{}
	out.urlFetcher = urlFetcher
	return &out
}

// scopedFamily to nazwa rodziny family w zawężeniu obrazu i trybu.
func (ff *svgFontFaces) scopedFamily(family string, document bool) string {
	mode := "image"
	if document {
		mode = "document"
	}
	return fmt.Sprintf("%s (gopdfgo svg %d %s)", strings.ToLower(family), ff.scope, mode)
}

// register dodaje twarze do konfiguracji fontów (raz dla konfiguracji i trybu). W trybie obrazu
// pomija źródła inne niż data:.
func (ff *svgFontFaces) register(fonts text.FontConfiguration, document bool) {
	key := registeredFonts{fonts, document}
	if fonts == nil || ff.registered[key] {
		return
	}
	ff.registered[key] = true
	for _, face := range ff.faces {
		var src []pr.TaggedString
		for _, s := range face.Src {
			if s.Tag != pr.External {
				continue // local(): fonty systemowe nie są zasobem dokumentu
			}
			if !document && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(s.S)), "data:") {
				continue
			}
			src = append(src, s)
		}
		if len(src) == 0 {
			continue
		}
		face.Src = src
		face.FontFamily = pr.String(ff.scopedFamily(string(face.FontFamily), document))
		fetcher := ff.urlFetcher
		if fetcher == nil {
			fetcher = utils.DefaultUrlFetcher
		}
		fonts.AddFontFace(face, fetcher)
	}
}

// mapFontFamilies zamienia w liście rodzin nazwy opisane regułami @font-face na zawężone.
func (ff *svgFontFaces) mapFontFamilies(families pr.Strings, document bool) pr.Strings {
	var out pr.Strings
	for i, f := range families {
		name := strings.Trim(strings.TrimSpace(f), `"'`)
		if !ff.families.Has(strings.ToLower(name)) {
			continue
		}
		if out == nil {
			out = append(pr.Strings(nil), families...)
		}
		out[i] = ff.scopedFamily(name, document)
	}
	if out == nil {
		return families
	}
	return out
}
