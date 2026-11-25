package blueprint

import (
	"strings"
	"testing"
)

func TestCompat21Fixtures(t *testing.T) {
	for name, want := range map[string]string{
		"legacy-2x-compat-blocked.txt": Compat21Incompatible,
		"legacy-2x-compat-ok.txt":      Compat21Compatible,
	} {
		root, err := Parse(strings.TrimSpace(readFixture(t, name)), DefaultLimits())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := Compat21(root); got != want {
			t.Errorf("%s: compat %q, want %q", name, got, want)
		}
	}
}
