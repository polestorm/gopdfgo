# Usage

- [Rendering a document](#rendering-a-document)
- [Fonts](#fonts)
- [External resources](#external-resources)
- [PDF/A, tagged PDF and PDF/UA](#pdfa-tagged-pdf-and-pdfua)
- [Attachments and Factur-X invoices](#attachments-and-factur-x-invoices)
- [Digital signatures](#digital-signatures)
- [Command line](#command-line)
- [What is supported](#what-is-supported)
- [What is not supported](#what-is-not-supported)

## Rendering a document

```go
import "github.com/polestorm/gopdfgo/htmlpdf"

regular, _ := os.ReadFile("NotoSans-Regular.ttf")
bold, _ := os.ReadFile("NotoSans-Bold.ttf")
result, err := htmlpdf.Render(ctx, html, htmlpdf.Options{
	Fonts: []htmlpdf.Font{
		{Name: "NotoSans-Regular.ttf", Data: regular},
		{Name: "NotoSans-Bold.ttf", Data: bold},
	},
	GenericFamilies: map[string][]string{"sans-serif": {"Noto Sans"}},
})
// result.PDF, result.Pages, result.Warnings
```

`Render` takes the HTML document and returns the PDF bytes, the page count and warnings: CSS that is not
supported, missing glyphs, resources that could not be loaded. The same input and options always give the
same bytes.

Where the specifications leave a choice, gopdfgo prints like Chromium, so documents designed in a browser keep
their layout. One switch, `Options.ExactLengths`, turns off Chromium's pixel rounding for output that follows
the stylesheet exactly. Every such decision has an ADR in [`adr/`](adr/) (in Polish).

## Fonts

Fonts come only from the caller: `Options.Fonts` lists them, and `Options.GenericFamilies` says which families
stand for `serif`, `sans-serif`, `monospace` and the other generic names. `@font-face` rules in the document
load fonts through the resource resolver. CFF and CFF2 fonts (`.otf`) are embedded as CFF subsets, TrueType
fonts as TrueType subsets.

A program that renders many documents with the same fonts can keep the parsed fonts between calls (ADR 0045):
create one `htmlpdf.NewFontCache(256 << 20)` and pass it in `Options.FontCache`. Fonts are then parsed once, on
first use; the output stays the same byte for byte. A small invoice renders about a third faster in-process
with a warm cache.

System fonts are a build option (ADR 0039). A binary built with `go build -tags systemfonts ./cmd/gopdfgo`
accepts `Options.SystemFonts` (`-system-fonts` on the command line): installed fonts then fill in families and
characters that the caller's fonts lack, while the caller's fonts and `GenericFamilies` keep priority. Each
system font used produces a warning naming its file, so that it can be added to `Options.Fonts` to freeze the
output; fonts whose licence forbids embedding are skipped. The default build contains no code that scans
system font directories, and there `Options.SystemFonts` returns `ErrSystemFontsNotBuilt`.

## External resources

`Options.Resolver` loads stylesheets, images and other external resources. The default resolver refuses
everything, so nothing is read from the network or the disk unless the caller allows it; the command line
reads files under the directory of the input file (`-root`). `data:` URLs always work.

## PDF/A, tagged PDF and PDF/UA

- `Options.Conformance`: PDF/A-2b, PDF/A-3b, or the accessible levels PDF/A-2a and PDF/A-3a, which turn on tags.
- `Options.Tagged`: a structure tree built from the HTML elements in document order (headings, paragraphs,
  lists, tables with header scopes and spans, links, images with alternative text); backgrounds, borders and
  margin boxes are marked as artifacts (ADR 0033).
- `Options.Accessibility = htmlpdf.PDFUA1`: PDF/UA-1, also together with PDF/A-2a. Defects of the source
  document (a skipped heading level, an image without alternative text) are reported as warnings.

veraPDF accepts all 22 documents of the test corpus as PDF/A-2b, PDF/A-2a and PDF/UA-1.

## Attachments and Factur-X invoices

`Options.Attachments` (or `<link rel="attachment">` in the document) embeds files with their MIME type and
`AFRelationship`, for example the XML of an e-invoice inside a PDF/A-3 file.

`Options.FacturX` makes a Factur-X 1.0 / ZUGFeRD 2.x hybrid invoice (ADR 0034): the CII invoice XML embedded as
`factur-x.xml` (`xrechnung.xml` for XRECHNUNG) with the relationship the specification gives for the profile,
the `fx` XMP properties with their PDF/A extension schema, and PDF/A-3b (PDF/A-3a with tags, also with
PDF/UA-1). veraPDF and the Mustang validator accept the result. gopdfgo checks only that the XML is a CII
document and reads its profile; validate the invoice XML before rendering.

## Digital signatures

Signing lives in the optional package `github.com/polestorm/gopdfgo/sign` (ADR 0049, 0050). Programs that do
not import it, including the `gopdfgo` command, do not contain its code.

```go
signed, err := sign.SignPDF(ctx, result.PDF, sign.Options{
	Signer:      key,           // crypto.Signer: *rsa.PrivateKey, *ecdsa.PrivateKey, an HSM, a smart card…
	Certificate: cert,          // *x509.Certificate of the key
	Chain:       intermediates, // up to the root, needed for B-LT
	Reason:      "Approved",
	// optional: timestamp and validation data
	Level:      sign.LevelBLT,
	TSA:        sign.TSAClient{URL: tsaURL, HTTPClient: httpClient},
	Revocation: sign.RevocationClient{HTTPClient: httpClient}, // OCSP and CRL addresses from the certificates
})
```

- `SignPDF` signs any unencrypted PDF, from gopdfgo or not, as an incremental update: earlier signatures stay
  valid, and PDF/A and PDF/UA conformance is kept.
- Levels: PAdES B-B (default, no network), B-T (RFC 3161 timestamp of the signature value) and B-LT
  (certificates, OCSP responses and CRLs in a DSS dictionary). The network is used only through the TSA and
  revocation clients passed in `sign.Options`; validation data can also be passed directly, for offline signing.
- The key is a `crypto.Signer` (or `sign.ContextSigner`, which can be cancelled), so a remote signing service
  can sign too.
- Signature fields can be placed in HTML (ADR 0051): an element with `data-pdf-signature="Name"` gets an empty
  signature field over its box (first page, first fragment), tagged as `Form` in PDF/UA. The element's own HTML
  content is what the reader sees. `sign.Options.FieldName` (or `gopdfgo-sign -field`) signs into that field,
  and any other signing program can fill it too.
- Checked with `pdfsig`, `openssl cms`, `openssl ts`, veraPDF and the EU DSS validator, which recognises the
  test signatures as PAdES-BASELINE-B, -T and -LT (its only objection is the untrusted test certificate).
- Not yet: document timestamps (B-LTA), certification signatures (`/DocMDP`), signing encrypted files,
  verifying signatures.

## Command line

```
go install github.com/polestorm/gopdfgo/cmd/gopdfgo@latest
gopdfgo -fonts fonts/ -in invoice.html -out invoice.pdf
gopdfgo -fonts fonts/ -pdfa 2a -pdfua 1 -in report.html -out report.pdf
gopdfgo -fonts fonts/ -facturx invoice.xml -in invoice.html -out invoice.pdf
```

`gopdfgo -h` lists all flags. Signing is a separate command, so that the `gopdfgo` binary contains no signing
code:

```
go install github.com/polestorm/gopdfgo/cmd/gopdfgo-sign@latest
gopdfgo-sign -in invoice.pdf -out signed.pdf -p12 signer.p12 -password-file p12-password.txt \
	-field Signature1 -level B-T -tsa https://tsa.example/rfc3161
```

The key comes from a PKCS#12 file (password from an environment variable or a file, never from the command
line) or from unencrypted PEM files. PKCS#12 files from OpenSSL 3 (AES-256, SHA-256 MAC) and older files with
3DES and a SHA-1 MAC are read; RC2-encrypted files give an error with the `openssl` command that converts them.
B-T and B-LT need `-tsa`, the only way the command connects to the network.

## What is supported

- HTML5 parsing and the CSS cascade: selectors level 4 including `:is()`, `:where()`, `:not()`; custom
  properties; `calc()`, `min()`, `max()`, `clamp()`, `round()`, `mod()`, `rem()`; colour functions, relative
  colours, `color()` with predefined colour spaces, `color-mix()`; `transform` with 2D and 3D functions
  (flattened to the page); `filter` (colour functions exact, `drop-shadow()` and `blur()` as vector
  approximations).
- Layout: block, inline, floats and positioning; tables, including spanning cells split across pages; flexbox,
  grid and multi-column, fragmented across pages; lists and counters; generated content.
- Paged media: `@page` with sizes, margins and all 16 margin boxes; `counter(page)` and `counter(pages)`, also in
  the page content; running elements and `string-set`; named pages and `:first`, `:left`, `:right`, `:blank`;
  footnotes; `leader()`; `target-counter()` and `target-text()`; `orphans` and `widows`; repeated table headers.
- Text: OpenType shaping (go-text, a Go port of HarfBuzz), font fallback, `@font-face`, font features,
  `hyphens: auto` with dictionaries for 24 languages built in, justification, `white-space`, `word-break`,
  `overflow-wrap`; `text-wrap: balance`, and `text-wrap: pretty`, which in Polish, Czech and Slovak
  keeps one-letter conjunctions and prepositions ("w", "z", "i") from ending a line.
- SVG, inline and as files: text shaped like HTML text, `textPath`, `foreignObject`, markers, masks, patterns,
  gradients, clipping, drop shadow and blur filters as vectors; an SVG file used many times is written into the
  PDF once.
- PNG, JPEG and GIF images; documents embedded with `iframe`, `object` and `embed`.
- PDF output: font subsets, internal and external links, bookmarks from headings, PDF/A, tagged PDF, PDF/UA-1,
  attachments, Factur-X.

## What is not supported

- JavaScript, `video`, `audio`; `canvas` stays empty without scripts.
- Right-to-left and bidirectional text, vertical writing.
- Fillable form fields: HTML form elements are printed as they look (as in Chromium and folio); only signature
  fields can be placed in HTML. Barcodes as a feature (they can be drawn as SVG).
- `subgrid`, `grid-lanes`, `column-height`, CSS `zoom`, `mask` on HTML elements, animations.
- Filters that need rasterization: SVG `feImage`, `feTile`, `feMorphology`, `feTurbulence`, convolution,
  lighting and displacement (the element is drawn without the filter), `backdrop-filter`.
- Perspective: `perspective()` is accepted with a warning and drawn as an orthographic projection.
- Reading, merging or editing existing PDF files, apart from adding a signature.

The full list with its status is in [`overview.md`](overview.md) and, in Polish, [`progress.md`](progress.md).
