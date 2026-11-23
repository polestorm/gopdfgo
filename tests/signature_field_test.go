package tests

import (
	"bytes"
	"context"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/polestorm/gopdfgo"
)

// Puste pola podpisu wskazane atrybutem data-pdf-signature (ADR 0051): pole /FT /Sig bez /V z widżetem
// na prostokącie obramowania pierwszego pudełka elementu, w PDF z tagami w elemencie Form; obie drogi
// rysowania dają te same bajty; bez atrybutu w pliku nie ma /AcroForm.

// signatureFieldPage: A4 bez marginesów, pole na drugiej stronie 2 cm od lewej i 3 cm od góry,
// 6 × 2 cm (obramowanie wliczone, box-sizing: border-box).
const signatureFieldPage = `<!doctype html><html lang="pl"><head><meta charset="utf-8"><title>Umowa</title>
<style>@page { size: A4; margin: 0 } body { margin: 0; font: 12pt sans-serif }
.sig { box-sizing: border-box; width: 6cm; height: 2cm; border: 1px solid #000 }</style></head><body>
<h1>Umowa</h1><p>Pierwsza strona.</p>
<div style="break-before: page; padding: 3cm 0 0 2cm">
<div class="sig" data-pdf-signature="Podpis1" aria-label="Podpis kierownika">Podpis</div></div>
</body></html>`

var (
	widgetRe   = regexp.MustCompile(`^<</Type/Annot /Rect \[([^\]]*)\]/AP <</N (\d+) 0 R>>/F (\d+) ?(/StructParent \d+ )?/Subtype/Widget /FT/Sig/T \(([^)]*)\)/TU \(([^)]*)\)>>$`)
	pageKidsRe = regexp.MustCompile(`<</Type/Pages/Count \d+/Kids \[([^\]]*)\]`)
)

// signatureWidget to pole podpisu odczytane z nieskompresowanego pliku z gopdfgo.
type signatureWidget struct {
	object      int
	rect        [4]float64
	flags       int
	name, label string
	page        int // numer strony od zera
	tagged      bool
}

func signatureWidgets(t *testing.T, pdf []byte) []signatureWidget {
	t.Helper()
	objects := pdfObjects(t, pdf)
	m := pageKidsRe.FindSubmatch(pdf)
	if m == nil {
		t.Fatal("brak drzewa stron")
	}
	var pages []int
	for _, ref := range refRe.FindAllStringSubmatch(string(m[1]), -1) {
		n, _ := strconv.Atoi(ref[1])
		pages = append(pages, n)
	}
	var out []signatureWidget
	for n, obj := range objects {
		w := widgetRe.FindStringSubmatch(obj)
		if w == nil {
			if strings.Contains(obj, "/Widget") {
				t.Errorf("obiekt %d: nieoczekiwany zapis widżetu %s", n, obj)
			}
			continue
		}
		field := signatureWidget{object: n, name: w[5], label: w[6], page: -1, tagged: w[4] != ""}
		for i, v := range strings.Fields(w[1]) {
			field.rect[i], _ = strconv.ParseFloat(v, 64)
		}
		field.flags, _ = strconv.Atoi(w[3])
		ap, _ := strconv.Atoi(w[2])
		if !strings.Contains(objects[ap], "/Subtype /Form") && !strings.Contains(objects[ap], "/Subtype/Form") {
			t.Errorf("wygląd /AP /N pola %s nie jest formą: %s", field.name, objects[ap])
		}
		for i, page := range pages {
			if regexp.MustCompile(`/Annots \[[^\]]*\b` + strconv.Itoa(n) + ` 0 R`).MatchString(objects[page]) {
				field.page = i
			}
		}
		out = append(out, field)
	}
	return out
}

// near: zgodność z dokładnością do zaokrągleń układu (jednostka układu 1/64 px)
func near(a, b float64) bool { return math.Abs(a-b) < 0.05 }

func TestSignatureFieldPlacement(t *testing.T) {
	for _, c := range []struct {
		name   string
		adjust func(*gopdfgo.Options)
	}{
		{"plain", nil},
		{"pdfa2b", func(o *gopdfgo.Options) { o.Conformance = gopdfgo.PDFA2b }},
		{"pdfua", func(o *gopdfgo.Options) { o.Conformance, o.Accessibility = gopdfgo.PDFA2a, gopdfgo.PDFUA1 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			opts := streamOptions(t)
			opts.Uncompressed = true
			if c.adjust != nil {
				c.adjust(&opts)
			}
			result, err := gopdfgo.Render(context.Background(), []byte(signatureFieldPage), opts)
			if err != nil {
				t.Fatal(err)
			}
			fields := signatureWidgets(t, result.PDF)
			if len(fields) != 1 {
				t.Fatalf("pól podpisu %d, oczekiwane 1", len(fields))
			}
			f := fields[0]
			const cm = 72 / 2.54
			want := [4]float64{2 * cm, 841.8898 - 5*cm, 8 * cm, 841.8898 - 3*cm}
			for i := range want {
				if !near(f.rect[i], want[i]) {
					t.Errorf("/Rect %v, oczekiwane %v", f.rect, want)
					break
				}
			}
			if f.page != 1 || f.flags != 4 || f.name != "Podpis1" || f.label != "Podpis kierownika" {
				t.Errorf("pole %+v: oczekiwane strona 1, /F 4, /T Podpis1, /TU Podpis kierownika", f)
			}
			if !bytes.Contains(result.PDF, []byte("/AcroForm")) || bytes.Contains(result.PDF, []byte("/SigFlags")) {
				t.Error("oczekiwany /AcroForm bez /SigFlags")
			}
			tagged := opts.Accessibility == gopdfgo.PDFUA1
			if f.tagged != tagged {
				t.Errorf("/StructParent widżetu: %v, oczekiwane %v", f.tagged, tagged)
			}
			if tagged {
				if dump := structureDump(t, result.PDF); !strings.Contains(dump, "Div(# Form(@))") {
					t.Errorf("drzewo struktury bez elementu Form z widżetem: %s", dump)
				}
			}
		})
	}
}

// Element dzielony między strony: pole na pierwszej stronie, na prostokącie pierwszego fragmentu.
// Pusta nazwa, nazwa z kropką i nazwa powtórzona dają ostrzeżenie i nie tworzą pola.
func TestSignatureFieldFragmentsAndWarnings(t *testing.T) {
	html := `<!doctype html><html lang="pl"><head><title>T</title><style>@page { size: 400px 300px; margin: 0 }
body { margin: 0 }</style></head><body>
<div style="height: 100px"></div>
<div data-pdf-signature="Duzy" style="height: 500px; margin-left: 10px; width: 100px">tekst</div>
<p><span data-pdf-signature="  ">a</span> <span data-pdf-signature="a.b">b</span>
<span data-pdf-signature="Duzy">c</span></p></body></html>`
	opts := streamOptions(t)
	opts.Uncompressed = true
	result, err := gopdfgo.Render(context.Background(), []byte(html), opts)
	if err != nil {
		t.Fatal(err)
	}
	fields := signatureWidgets(t, result.PDF)
	if len(fields) != 1 {
		t.Fatalf("pól podpisu %d, oczekiwane 1", len(fields))
	}
	f := fields[0]
	// 300px strony = 225 pt; fragment od 100px do dołu strony, 10–110px w poziomie
	if f.page != 0 || !near(f.rect[0], 7.5) || !near(f.rect[2], 82.5) || !near(f.rect[1], 0) || !near(f.rect[3], 150) {
		t.Errorf("pole %+v: oczekiwane strona 0, /Rect [7.5 0 82.5 150]", f)
	}
	var warnings []string
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "ignature field") {
			warnings = append(warnings, w.Message)
		}
	}
	if len(warnings) != 3 {
		t.Errorf("ostrzeżeń o polach %d, oczekiwane 3: %q", len(warnings), warnings)
	}
}

// Obie drogi rysowania (ADR 0036) dają te same bajty; bez atrybutu nie ma /AcroForm.
func TestSignatureFieldStreamingAndAbsence(t *testing.T) {
	for _, c := range []struct {
		name   string
		adjust func(*gopdfgo.Options)
	}{
		{"plain", nil},
		{"pdfa2b", func(o *gopdfgo.Options) { o.Conformance = gopdfgo.PDFA2b }},
		{"pdfua", func(o *gopdfgo.Options) { o.Conformance, o.Accessibility = gopdfgo.PDFA2a, gopdfgo.PDFUA1 }},
	} {
		opts := streamOptions(t)
		if c.adjust != nil {
			c.adjust(&opts)
		}
		streamed, classic, fallback := streamPair(t, []byte(signatureFieldPage), opts)
		if fallback != "" {
			t.Errorf("%s: rysowanie po ułożeniu: %s", c.name, fallback)
		}
		if !bytes.Equal(streamed, classic) {
			t.Errorf("%s: drogi rysowania dają różne bajty (%d B i %d B)", c.name, len(streamed), len(classic))
		}
		plain := strings.Replace(signatureFieldPage, ` data-pdf-signature="Podpis1"`, "", 1)
		result, err := gopdfgo.Render(context.Background(), []byte(plain), opts)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(result.PDF, []byte("/AcroForm")) {
			t.Errorf("%s: /AcroForm bez atrybutu data-pdf-signature", c.name)
		}
	}
}

// veraPDF: PDF/A-2b, PDF/A-2a i PDF/UA-1 z polem podpisu (przed podpisaniem; po podpisaniu sprawdza
// pakiet sign).
func TestSignatureFieldVeraPDF(t *testing.T) {
	for _, c := range []struct {
		flavour string
		adjust  func(*gopdfgo.Options)
	}{
		{"2b", func(o *gopdfgo.Options) { o.Conformance = gopdfgo.PDFA2b }},
		{"2a", func(o *gopdfgo.Options) { o.Conformance, o.Accessibility = gopdfgo.PDFA2a, gopdfgo.PDFUA1 }},
		{"ua1", func(o *gopdfgo.Options) { o.Conformance, o.Accessibility = gopdfgo.PDFA2a, gopdfgo.PDFUA1 }},
	} {
		t.Run(c.flavour, func(t *testing.T) {
			opts := streamOptions(t)
			c.adjust(&opts)
			result, err := gopdfgo.Render(context.Background(), []byte(signatureFieldPage), opts)
			if err != nil {
				t.Fatal(err)
			}
			verapdf(t, result.PDF, c.flavour)
		})
	}
}
