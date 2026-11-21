package tests

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo"
	"github.com/polestorm/gopdfgo/internal/webrender/images"
)

// Obraz SVG z pliku powtórzony w dokumencie (logo w polach marginesów, ikony w tabeli, tło) jest
// zapisywany w PDF raz, jako forma XObject, a każde wystąpienie tylko się do niej odwołuje (ADR 0028).
// Wygląd ma być taki sam jak przy rysowaniu każdego wystąpienia osobno.

func herbDataURL(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "svg", "commons-Herb_Polski.svg"))
	if err != nil {
		t.Fatal(err)
	}
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(data)
}

// renderSharing renderuje dokument ze współdzieleniem obrazów SVG albo bez niego.
func renderSharing(t *testing.T, html string, sharing bool, uncompressed bool) gopdfgo.Result {
	t.Helper()
	images.SVGSharingDisabled = !sharing
	defer func() { images.SVGSharingDisabled = false }()
	result, err := gopdfgo.Render(context.Background(), []byte(html), gopdfgo.Options{Fonts: testFonts(t), Uncompressed: uncompressed})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

var formXObject = regexp.MustCompile(`/Subtype\s*/Form`)

func TestSharedSVGImageAcrossPages(t *testing.T) {
	herb := herbDataURL(t)
	page := func(n int) string {
		var b strings.Builder
		b.WriteString(`<!DOCTYPE html><style>div { break-after: page }</style>`)
		for i := range n {
			fmt.Fprintf(&b, `<div><img src="%s" width="40"> Strona %d</div>`, herb, i+1)
		}
		return b.String()
	}
	one := renderSharing(t, page(1), true, false)
	many := renderSharing(t, page(41), true, false)
	separate := renderSharing(t, page(41), false, false)
	if many.Pages != 41 {
		t.Fatalf("%d stron, oczekiwano 41", many.Pages)
	}
	// każda kolejna strona to tylko treść strony z operatorem Do, bez rysunku herbu (ok. 70 kB
	// nieskompresowanego SVG); przed zmianą 41 stron dawało 1,5 MB
	if limit := len(one.PDF) + 40*1500; len(many.PDF) > limit {
		t.Errorf("41 stron z tym samym herbem: %d B, 1 strona: %d B, oczekiwano najwyżej %d B", len(many.PDF), len(one.PDF), limit)
	}
	if len(separate.PDF) < 10*len(many.PDF) {
		t.Errorf("bez współdzielenia %d B, ze współdzieleniem %d B: oczekiwano co najmniej 10 razy mniej", len(separate.PDF), len(many.PDF))
	}
	if n := len(formXObject.FindAll(renderSharing(t, page(41), true, true).PDF, -1)); n != 1 {
		t.Errorf("%d form XObject, oczekiwano jednej wspólnej dla 41 wystąpień", n)
	}
	again := renderSharing(t, page(41), true, false)
	if !bytes.Equal(many.PDF, again.PDF) {
		t.Error("dwa renderowania tego samego dokumentu dają różne bajty")
	}
}

// Wygląd z formą współdzieloną i bez niej jest ten sam: obrazy w treści, w polach marginesów
// (content: url(), element biegnący), w tle (także kafelkowanym), w <object> i w border-image,
// w kilku rozmiarach naraz.
func TestSharedSVGImageLooksTheSame(t *testing.T) {
	herb := herbDataURL(t)
	var b strings.Builder
	fmt.Fprintf(&b, `<!DOCTYPE html><style>
@page { size: 400px 300px; margin: 50px 30px;
  @top-left { content: url(%[1]s); width: 24px }
  @top-right { content: element(hdr) }
  @bottom-center { content: counter(page); background: url(%[1]s) no-repeat center / 16px 20px } }
header { position: running(hdr) } header img { width: 18px }
td div { width: 30px; height: 36px; background: url(%[1]s) 0 0 / 15px 18px }
p { border: 12px solid; border-image: url(%[1]s) 30%% round; height: 20px }
</style><header><img src="%[1]s"> Nagłówek</header><p></p><table>`, herb)
	for i := range 30 {
		fmt.Fprintf(&b, `<tr><td><img src="%[1]s" width="%[2]d"><td>Wiersz %[3]d<td><div></div><td><object data="%[1]s" width="14" height="16"></object></tr>`,
			herb, 12+i%3*4, i+1)
	}
	b.WriteString(`</table>`)
	html := b.String()
	shared := renderSharing(t, html, true, false)
	separate := renderSharing(t, html, false, false)
	if shared.Pages < 3 || shared.Pages != separate.Pages {
		t.Fatalf("stron %d ze współdzieleniem, %d bez", shared.Pages, separate.Pages)
	}
	if len(shared.PDF)*10 > len(separate.PDF) {
		t.Errorf("ze współdzieleniem %d B, bez %d B: oczekiwano co najmniej 10 razy mniej", len(shared.PDF), len(separate.PDF))
	}
	dir := t.TempDir()
	a, c := rasterize(t, shared.PDF, dir, "shared"), rasterize(t, separate.PDF, dir, "separate")
	for i := range a {
		if !bytes.Equal(a[i], c[i]) {
			t.Errorf("strona %d: %d różnych pikseli między formą współdzieloną a rysowaniem osobno", i+1, differentPixels(a[i], c[i]))
		}
	}
}

// SVG osadzony w HTML dziedziczy kaskadę dokumentu (tu currentColor) i jest rysowany wprost, bez formy
// współdzielonej: dwa takie same elementy <svg> w różnych kolorach zostają w różnych kolorach.
func TestInlineSVGNotShared(t *testing.T) {
	icon := `<svg width="20" height="20"><rect width="20" height="20" fill="currentColor"/></svg>`
	html := `<!DOCTYPE html><style>@page { size: 100px 50px; margin: 0 } body { margin: 0; display: flex }</style>` +
		`<span style="color: rgb(0, 128, 0)">` + icon + `</span><span style="color: rgb(0, 0, 255)">` + icon + `</span>`
	result := renderSharing(t, html, true, false)
	pages := rasterize(t, result.PDF, t.TempDir(), "inline")
	if len(pages) != 1 {
		t.Fatalf("%d stron", len(pages))
	}
	// 96 dpi: piksel CSS = piksel obrazu
	if got := pixelRGB(t, pages[0], 10, 10); got != [3]byte{0, 128, 0} {
		t.Errorf("pierwsza ikona %v, oczekiwano zielonej", got)
	}
	if got := pixelRGB(t, pages[0], 30, 10); got != [3]byte{0, 0, 255} {
		t.Errorf("druga ikona %v, oczekiwano niebieskiej", got)
	}
}
