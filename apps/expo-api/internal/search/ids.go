package search

import (
	"errors"

	"factorio-expo/internal/shortid"
)

// parseID mirrors catalog's helper: this package intentionally doesn't
// depend on internal/catalog (it only reads rows through db.Queries, the
// same way the dispatcher already does), so it keeps its own tiny copy
// instead of exporting one from catalog just for this.
func parseID(value string) (string, error) {
	if !shortid.Valid(value) {
		return "", errors.New("malformed id")
	}
	return value, nil
}
