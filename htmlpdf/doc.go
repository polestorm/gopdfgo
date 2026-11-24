// Package htmlpdf renderuje statyczny HTML + CSS + SVG do PDF bez przeglądarki (projekt gopdfgo).
//
// Punkt wejścia to Render. Silnik układu pochodzi z forka webrender przejętego do internal/
// (docs/adr/0002-webrender-base.md); fonty i zasoby dostarcza wyłącznie wywołujący. Podpis PAdES gotowego
// pliku jest w osobnym pakiecie sign (docs/adr/0049-optional-feature-packages.md).
package htmlpdf
