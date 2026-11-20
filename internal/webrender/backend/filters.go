package backend

// FilterCanvas (gopdfgo) to opcjonalne rozszerzenie Canvas, z którego korzysta wektorowe przybliżenie
// filtrów SVG (Filter Effects 1): cień i rozmycie powstają z wielu kopii tej samej grupy, więc grupa
// musi być zapisywana raz i wskazywana wielokrotnie, a rozmyta sylwetka jest maską miękką alfa
// z funkcją przejścia (PDF 32000-1 § 11.6.5.2). Canvas bez tego rozszerzenia rysuje element bez filtra.
type FilterCanvas interface {
	// DrawSharedGroup rysuje grupę jak DrawWithOpacity, ale kolejne wywołania z tą samą grupą
	// wskazują jeden obiekt; grupy nie wolno zmieniać po pierwszym narysowaniu.
	DrawSharedGroup(opacity Fl, group Canvas)

	// SetSoftMask ustawia maskę miękką z grupy `mask`: alpha = true bierze jej kanał alfa (/Alpha),
	// false luminancję (/Luminosity). `transfer` to próbki funkcji przejścia na przedziale [0, 1]
	// (równomiernie rozłożone, co najmniej dwie); nil oznacza tożsamość.
	SetSoftMask(mask Canvas, alpha bool, transfer []Fl)
}
