// Package gopdfgo renderuje statyczny HTML + CSS + SVG do PDF bez przeglądarki.
//
// Punkt wejścia to Render. Silnik układu pochodzi z forka webrender przejętego do internal/
// (docs/adr/0002-webrender-base.md); fonty i zasoby dostarcza wyłącznie wywołujący.
package gopdfgo
