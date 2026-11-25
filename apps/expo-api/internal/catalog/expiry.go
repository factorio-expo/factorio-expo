package catalog

import (
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// ExpiryDays are the lifetimes an upload (or a publish) may choose, in days;
// 0 means it never expires, the default.
var ExpiryDays = []int{1, 7, 14, 30}

// expiryAfter is the expiry for a chosen lifetime: none for 0, now plus that
// many days for one of ExpiryDays, ErrInvalidExpiry otherwise.
func expiryAfter(days int, now time.Time) (pgtype.Timestamptz, error) {
	if days == 0 {
		return pgtype.Timestamptz{}, nil
	}
	if !slices.Contains(ExpiryDays, days) {
		return pgtype.Timestamptz{}, ErrInvalidExpiry
	}
	return pgtype.Timestamptz{Time: now.Add(time.Duration(days) * 24 * time.Hour), Valid: true}, nil
}
