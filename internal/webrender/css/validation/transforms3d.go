package validation

// Plik dodany w gopdfgo, nie pochodzi ze źródła webrender.

import (
	pa "github.com/polestorm/gopdfgo/internal/webrender/css/parser"
	pr "github.com/polestorm/gopdfgo/internal/webrender/css/properties"
	"github.com/polestorm/gopdfgo/internal/webrender/logger"
)

// Właściwości 3D z CSS Transforms 2. PDF jest płaski, więc rysujemy tylko rzut prostokątny
// (transform-style: flat bez perspektywy, zob. html/document getMatrix). Wartości, których nie
// umiemy narysować (perspektywa, preserve-3d), przyjmujemy z ostrzeżeniem, żeby deklaracja zachowała
// pozostałe skutki: kontekst nakładania i blok zawierający (§ 5, § 7).

// backface-visibility: visible | hidden (§ 11)
func backfaceVisibility(tokens []Token, _ string) pr.CssProperty {
	switch keyword := getSingleKeyword(tokens); keyword {
	case "visible", "hidden":
		return pr.String(keyword)
	}
	return nil
}

// transform-style: flat | preserve-3d (§ 5); preserve-3d działa jak flat
func transformStyle(tokens []Token, _ string) pr.CssProperty {
	switch keyword := getSingleKeyword(tokens); keyword {
	case "flat":
		return pr.String(keyword)
	case "preserve-3d":
		logger.WarningLogger.Println("Ignored 3D rendering context of `transform-style: preserve-3d`, not supported in PDF; drawn as flat.")
		return pr.String(keyword)
	}
	return nil
}

// perspective: none | <length [0,∞]> (§ 7); perspektywa jest pomijana
func perspectiveProperty(tokens []Token, _ string) pr.CssProperty {
	if getSingleKeyword(tokens) == "none" {
		return pr.TaggedDim{Tag: pr.None}
	}
	if len(tokens) != 1 {
		return nil
	}
	length := getLength(tokens[0], false, false)
	if length.IsNone() {
		return nil
	}
	logger.WarningLogger.Printf("Ignored perspective of `perspective: %s`, perspective projection is not supported in PDF; drawn without perspective. \n",
		pa.Serialize(tokens))
	return length.Tagged()
}

// perspective-origin: <position> (§ 8); bez skutku, bo perspektywę pomijamy
func perspectiveOrigin(tokens []Token, _ string) pr.CssProperty {
	return parse2dPosition(tokens)
}
