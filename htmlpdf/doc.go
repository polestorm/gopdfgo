// Package htmlpdf renders static HTML + CSS + SVG to PDF without a browser (the gopdfgo project).
//
// The entry point is Render. The layout engine comes from a fork of webrender taken over into internal/
// (docs/adr/0002-webrender-base.md); fonts and resources are supplied only by the caller. PAdES signing of
// the finished file lives in the separate package sign (docs/adr/0049-optional-feature-packages.md).
package htmlpdf
