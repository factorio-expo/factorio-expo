package search

import (
	"reflect"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
)

func TestVersionGroups(t *testing.T) {
	for _, tt := range []struct {
		version string
		compat  catalog.Compat21
		want    []string
	}{
		{"0.17.79", "", []string{"pre-2.0"}},
		{"1.1.110", catalog.Compat21Compatible, []string{"pre-2.0", "v2.1-compatible"}},
		{"1.1.110", catalog.Compat21Incompatible, []string{"pre-2.0"}},
		{"2.0.55", "", []string{"v2.0"}},
		{"2.0.55", catalog.Compat21Compatible, []string{"v2.0", "v2.1-compatible"}},
		{"2.1.3", "", []string{"v2.1", "v2.1-compatible"}},
		{"2.2.0", "", []string{"v2.2"}},
		{"2", "", []string{}},
		{"x.y.z", "", []string{}},
		{"", "", []string{}},
	} {
		if got := versionGroups(tt.version, tt.compat); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("versionGroups(%q, %q) = %v, want %v", tt.version, tt.compat, got, tt.want)
		}
	}
}

func TestTopProduct(t *testing.T) {
	if got := topProduct(map[string]float64{"iron-plate": 2, "iron-gear-wheel": 7, "copper-plate": 7}); got != "copper-plate" {
		t.Errorf("topProduct = %q, want the lexically first of the highest", got)
	}
	if got := topProduct(nil); got != "" {
		t.Errorf("topProduct(nil) = %q", got)
	}
}
