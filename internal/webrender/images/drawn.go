package images

import (
	"github.com/polestorm/gopdfgo/internal/webrender/backend"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/text"
)

// Drawn (gopdfgo) to obraz rysowany przez funkcję podaną z zewnątrz pakietu: dokument osadzony
// w <iframe>, <object> albo <embed> (zob. boxes.NestedDocument). Interfejs Image ma metodę
// nieeksportowaną, więc pakiet układu dokumentu nie może go zrealizować sam.
type Drawn struct {
	// Width i Height to rozmiar wewnętrzny (dla <iframe> domyślnie 300 na 150, HTML § 15.4.1).
	Width, Height pr.Float
	// Paint rysuje zawartość w prostokącie (0, 0, width, height) płótna dst.
	Paint func(dst backend.Canvas, fonts text.FontConfiguration, width, height pr.Fl)
}

func (Drawn) isImage() {}

func (d Drawn) GetIntrinsicSize(_, _ pr.Float) (width, height, ratio pr.MaybeFloat) {
	return d.Width, d.Height, nil
}

func (d Drawn) Draw(dst backend.Canvas, context text.TextLayoutContext, width, height pr.Fl, _ string) {
	if d.Paint != nil {
		d.Paint(dst, context.Fonts(), width, height)
	}
}
