package shortid

import "testing"

func TestNewIsValidAndDistinct(t *testing.T) {
	seen := make(map[string]bool)
	for range 1000 {
		id := New()
		if !Valid(id) {
			t.Fatalf("New() = %q, not valid", id)
		}
		if seen[id] {
			t.Fatalf("New() repeated %q", id)
		}
		seen[id] = true
	}
}

func TestValid(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"aZ3k9QxP2mT7", true},
		{"000000000000", true},
		{"aZ3k9QxP2mT", false},
		{"aZ3k9QxP2mT7x", false},
		{"aZ3k9QxP2m-7", false},
		{"aZ3k9QxP2m_7", false},
		{"", false},
		{"3f2a9c1e-0000-4000-8000-000000000000", false},
	} {
		if got := Valid(tc.value); got != tc.want {
			t.Errorf("Valid(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}
