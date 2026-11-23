package tree

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"unsafe"

	"github.com/polestorm/gopdfgo/internal/webrender/utils"
)

var styleElement = regexp.MustCompile(`(?s)<style[^>]*>(.*?)</style>`)

// gopdfgo: indeks selektorów (matcherIndex) daje te same dopasowania w tej samej kolejności co
// sprawdzanie wszystkich selektorów po kolei (matcher.match), na arkuszach domyślnych i arkuszach
// dokumentów korpusu.
func TestMatcherIndexSameAsLinear(t *testing.T) {
	files, _ := filepath.Glob("../../../../testdata/documents/*.html")
	if len(files) == 0 {
		t.Skip("brak testdata/documents")
	}
	sheet, err := NewCSSDefault(utils.InputString(`
		#a, .b .c, div.d > p#e, span.f.g, :is(td, th).h, *.i, [lang] j, k:first-child, ::before, td::marker {}
		table tr > td, svg, foreignObject, rect.r {}
	`))
	if err != nil {
		t.Fatal(err)
	}
	sheets := []CSS{Html5UAStylesheet, Html5PHStylesheet, sheet}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := newHtml(utils.InputString(string(content) + `<p id="a" class="b  c i d" lang="pl"><j class="i i"></j></p><svg><foreignObject><rect class="r"/></foreignObject></svg>`))
		if err != nil {
			t.Fatal(err)
		}
		docSheets := sheets[:3:3]
		for _, m := range styleElement.FindAllStringSubmatch(string(content), -1) {
			css, err := NewCSSDefault(utils.InputString(m[1]))
			if err != nil {
				t.Fatal(err)
			}
			docSheets = append(docSheets, css)
		}
		iter := doc.Root.Iter()
		for iter.HasNext() {
			element := iter.Next().AsHtmlNode()
			for _, sh := range docSheets {
				want, got := sh.matcher.match(element), sh.match(element)
				if len(want) != len(got) {
					t.Fatalf("%s <%s>: %d dopasowań zamiast %d", filepath.Base(file), element.Data, len(got), len(want))
				}
				for i := range want {
					w, g := want[i], got[i]
					if w.specificity != g.specificity || w.pseudoType != g.pseudoType || w.layer != g.layer ||
						unsafe.SliceData(w.payload) != unsafe.SliceData(g.payload) {
						t.Fatalf("%s <%s>: dopasowanie %d różne", filepath.Base(file), element.Data, i)
					}
				}
			}
		}
	}
}
