package properties

// Plik dodany w gopdfgo, nie pochodzi ze źródła webrender.

// EffectiveTransforms zwraca listę funkcji przekształcenia elementu w kolejności z CSS Transforms 2 § 6:
// translate, rotate, scale, a po nich transform (przesunięcie o transform-origin dodaje rysowanie).
// Pusta lista oznacza brak przekształcenia, tzn. wszystkie cztery właściwości mają wartość none.
func EffectiveTransforms(style StyleAccessor) Transforms {
	translate, rotate, scale, transform := style.GetTranslate(), style.GetRotate(), style.GetScale(), style.GetTransform()
	if len(translate) == 0 && len(rotate) == 0 && len(scale) == 0 {
		return transform
	}
	out := make(Transforms, 0, len(translate)+len(rotate)+len(scale)+len(transform))
	out = append(out, translate...)
	out = append(out, rotate...)
	out = append(out, scale...)
	return append(out, transform...)
}

// HasTransform mówi, czy któraś z właściwości transform, translate, rotate albo scale ma wartość inną
// niż none; skutki uboczne (kontekst warstwowy, blok zawierający) są wtedy takie same jak dla
// transform (CSS Transforms 2 § 5.1).
func HasTransform(style StyleAccessor) bool {
	return len(style.GetTransform()) != 0 || len(style.GetTranslate()) != 0 ||
		len(style.GetRotate()) != 0 || len(style.GetScale()) != 0
}
