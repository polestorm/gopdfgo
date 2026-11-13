package pdf

import (
	"bytes"
	"fmt"
	"sort"
	"unicode/utf16"

	"github.com/polestorm/gopdfgo/internal/pdflib/model"
	"github.com/polestorm/gopdfgo/internal/webrender/backend"
)

// Plik dodany w gopdfgo, nie pochodzi ze źródła go-weasyprint.

// newStream zwraca strumień PDF, skompresowany gdy compressStreams jest włączone.
func newStream(content []byte) model.Stream {
	if compressStreams {
		return model.NewCompressedStream(content)
	}
	return model.Stream{Content: content}
}

// maxCMapBlock to limit wpisów w jednym bloku bfchar (PDF 32000-1, 9.10.3; Adobe TN 5411).
const maxCMapBlock = 100

// writeToUnicodeCMap zapisuje CMap ToUnicode dla fontu z kodowaniem Identity-H.
// Zastępuje cmaps.WriteAdobeIdentityUnicodeCMap z benoitkugler/pdf, które iteruje po mapie
// (inne bajty przy każdym uruchomieniu) i zapisuje wszystkie wpisy w jednym bloku.
func writeToUnicodeCMap(glyphs map[backend.GID][]rune) []byte {
	ids := make([]backend.GID, 0, len(glyphs))
	for id := range glyphs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	var buf bytes.Buffer
	buf.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n" +
		"/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n" +
		"1 begincodespacerange\n<0000> <ffff>\nendcodespacerange\n")
	for start := 0; start < len(ids); start += maxCMapBlock {
		end := min(start+maxCMapBlock, len(ids))
		fmt.Fprintf(&buf, "%d beginbfchar\n", end-start)
		for _, id := range ids[start:end] {
			fmt.Fprintf(&buf, "<%04x> <", id)
			for _, unit := range utf16.Encode(glyphs[id]) {
				fmt.Fprintf(&buf, "%04x", unit)
			}
			buf.WriteString(">\n")
		}
		buf.WriteString("endbfchar\n")
	}
	buf.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend")
	return buf.Bytes()
}
