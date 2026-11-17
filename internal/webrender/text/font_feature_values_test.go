package text

import (
	"reflect"
	"testing"

	"github.com/polestorm/gopdfgo/internal/webrender/css/validation"
)

// gopdfgo: przekład font-variant-alternates na cechy OpenType (CSS Fonts 4 § 6.8);
// cech swsh, cswh, ornm i nalt nie ma żaden font z testdata, więc sprawdzamy sam przekład.
func TestAlternatesFeatures(t *testing.T) {
	values := validation.FontFeatureValues{}
	values.Add(validation.FontFeatureValuesRule{
		Families: []string{"fwf"},
		Values: map[string]map[string][]int{
			"stylistic":         {"s": {3}},
			"styleset":          {"a": {1, 12}, "big": {100}},
			"character-variant": {"c": {2, 4}, "d": {7}},
			"swash":             {"w": {2}},
			"ornaments":         {"o": {5}},
			"annotation":        {"n": {0}},
		},
	})
	features := featureSet{}
	alternatesFeatures(features, "historical-forms stylistic(s) styleset(a,big,none) character-variant(c,d) swash(w) ornaments(o) annotation(n)",
		[]string{"Other", "FWF"}, values)
	want := featureSet{
		{'h', 'i', 's', 't'}: 1, {'s', 'a', 'l', 't'}: 3, {'s', 's', '0', '1'}: 1, {'s', 's', '1', '2'}: 1,
		{'c', 'v', '0', '2'}: 4, {'c', 'v', '0', '7'}: 1, {'s', 'w', 's', 'h'}: 2, {'c', 's', 'w', 'h'}: 2,
		{'o', 'r', 'n', 'm'}: 5, {'n', 'a', 'l', 't'}: 0,
	}
	if !reflect.DeepEqual(features, want) {
		t.Errorf("cechy %v, oczekiwane %v", features, want)
	}
}
