package catalog

import (
	"errors"
	"testing"
)

func TestEntryVisibility(t *testing.T) {
	tests := []struct {
		name       string
		visibility string
		owned      bool
		want       string
		err        error
	}{
		{"default", "", false, "public", nil},
		{"public anonymous", "public", false, "public", nil},
		{"private owned", "private", true, "private", nil},
		{"private anonymous", "private", false, "", ErrForbidden},
		{"unlisted owned", "unlisted", true, "unlisted", nil},
		{"unlisted anonymous", "unlisted", false, "", ErrForbidden},
		{"unknown", "hidden", true, "", ErrInvalidVisibility},
		{"wrong case", "Public", false, "", ErrInvalidVisibility},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := entryVisibility(test.visibility, test.owned)
			if got != test.want || !errors.Is(err, test.err) {
				t.Fatalf("entryVisibility(%q, %v) = %q, %v; want %q, %v",
					test.visibility, test.owned, got, err, test.want, test.err)
			}
		})
	}
}
