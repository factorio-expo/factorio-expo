package catalog

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// The entry lifecycle rules, for Go code that holds an entry row. Queries
// filter with their SQL twins in 000001_init (entry_live, entry_visible_to,
// entry_listed); TestLifecycleRulesMatchSQL checks the two agree. Add a rule
// here and there together, never a third copy in a query or a caller.

// entryLive reports whether the entry is neither deleted nor expired. Every
// other entry reads as missing.
func entryLive(entry db.Entry, now time.Time) bool {
	return !entry.DeletedAt.Valid && (!entry.ExpiresAt.Valid || entry.ExpiresAt.Time.After(now))
}

// visibleTo reports whether the viewer ("" when signed out) may see an entry
// with this visibility and owner: private entries only their owner. It takes
// the facts rather than a row, so callers that only joined those need none.
func visibleTo(visibility string, owner pgtype.Text, viewerUserID string) bool {
	if visibility != "private" {
		return true
	}
	return viewerUserID != "" && owner.Valid && owner.String == viewerUserID
}

// entryListed reports whether the entry is live, public, not a quick preview
// (those are stored public but never listed) and has a revision. Listed
// entries are the ones search indexes and public profiles show, and the only
// ones that take comments.
func entryListed(entry db.Entry, now time.Time) bool {
	return entry.Visibility == "public" && !entry.QuickPreview && entry.CurrentRevisionID.Valid &&
		entryLive(entry, now)
}

// BanActive reports whether a user ban is in force: neither revoked nor
// expired. SQL twin: ban_active.
func BanActive(ban db.UserBan, now time.Time) bool {
	return !ban.RevokedAt.Valid && (!ban.ExpiresAt.Valid || ban.ExpiresAt.Time.After(now))
}

// Visibility is who may see an entry: public (listed and searchable),
// unlisted (anyone with the link) or private (the owner only).
type Visibility string

// Entry visibilities.
const (
	VisibilityPublic   Visibility = "public"
	VisibilityUnlisted Visibility = "unlisted"
	VisibilityPrivate  Visibility = "private"
)

// Compat21 is the Factorio 2.1 compatibility of a submission made before 2.1:
// compatible or incompatible; empty for one made in 2.1.
type Compat21 string

// Factorio 2.1 compatibility verdicts.
const (
	Compat21Compatible   Compat21 = "compatible"
	Compat21Incompatible Compat21 = "incompatible"
)
