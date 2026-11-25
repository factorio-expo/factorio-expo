package catalog

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/internal/shortid"
)

// An entry's vanity alias: /@{username}/{slug} (docs/api.md#vanity-aliases).
// Set by the owner of a signed-in entry at upload or in the metadata edit,
// unique per user; changing or clearing it frees the old slug at once (no
// history, no redirect).

var (
	// ErrInvalidAlias is a slug that breaks the rules; the message says which.
	ErrInvalidAlias = fault.New(fault.Invalid, "invalid_alias", "invalid alias")
	// ErrAliasTaken is a slug the user already uses on another entry.
	ErrAliasTaken = fault.New(fault.Conflict, "alias_taken", "you already use that alias on another entry")
	// ErrAliasNeedsAccount is an alias on an entry without an account.
	ErrAliasNeedsAccount = fault.New(fault.Forbidden, "alias_needs_account", "only signed-in uploaders can set an alias")
)

// Alias slug bounds, twinned in entry_aliases' CHECK.
const (
	MinAliasLength = 3
	MaxAliasLength = 60
)

// aliasSlug is lowercase letters and digits in hyphen-separated runs of
// MinAliasLength to MaxAliasLength characters, starting and ending with a
// letter or digit.
var aliasSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,58}[a-z0-9]$`)

// reservedAliases are words a slug may not be: the site's own routes below a
// profile, so a future page there never meets somebody's alias.
var reservedAliases = map[string]bool{
	"new": true, "edit": true, "feed": true, "revisions": true, "blueprints": true, "entries": true,
	"collections": true, "comments": true, "favorites": true, "api": true, "admin": true, "raw": true,
	"upload": true, "preview": true, "settings": true, "account": true, "login": true, "signup": true,
	"users": true, "about": true, "browse": true, "compare": true, "challenges": true,
}

// normalizeAlias lower-cases and checks a slug. A slug the size and shape of
// an entry id is refused too, so nobody poses as another entry's /e/{id}.
func normalizeAlias(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case len(slug) < MinAliasLength || len(slug) > MaxAliasLength:
		return "", ErrInvalidAlias.Withf("an alias must be %d to %d characters", MinAliasLength, MaxAliasLength)
	case !aliasSlug.MatchString(slug):
		return "", ErrInvalidAlias.Withf("an alias takes lowercase letters, digits and hyphens, and starts and ends with a letter or digit")
	case reservedAliases[slug]:
		return "", ErrInvalidAlias.Withf("that alias is reserved")
	case shortid.Valid(slug):
		return "", ErrInvalidAlias.Withf("an alias may not look like an entry id")
	}
	return slug, nil
}

// newEntryAlias is an upload's alias: empty for none, refused without an owner.
func newEntryAlias(raw, ownerID string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	if ownerID == "" {
		return "", ErrAliasNeedsAccount
	}
	return normalizeAlias(raw)
}

// setAlias gives the entry its slug (replacing its old one). A slug of the
// owner's that belongs to an entry no longer live is taken over.
func setAlias(ctx context.Context, queries *db.Queries, entry db.Entry, slug string) error {
	if !entry.OwnerUserID.Valid {
		return ErrAliasNeedsAccount
	}
	owner := entry.OwnerUserID.String
	if _, err := queries.DeleteStaleAlias(ctx, db.DeleteStaleAliasParams{UserID: owner, Slug: slug}); err != nil {
		return fmt.Errorf("free stale alias: %w", err)
	}
	err := queries.SetEntryAlias(ctx, db.SetEntryAliasParams{EntryID: entry.ID, UserID: owner, Slug: slug})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
		return ErrAliasTaken
	}
	if err != nil {
		return fmt.Errorf("set alias: %w", err)
	}
	return nil
}

// entryAlias is the entry's slug, "" for none.
func entryAlias(ctx context.Context, queries *db.Queries, entryID string) (string, error) {
	slug, err := queries.GetEntryAlias(ctx, entryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read alias: %w", err)
	}
	return slug, nil
}

// ResolveAlias is the entry /@{username}/{slug} names, if the viewer may see
// it (a missing, deleted or invisible entry is ErrNotFound). Both parts are
// matched as written after lower-casing the slug, so no other spelling
// reaches an entry.
func (s *Service) ResolveAlias(ctx context.Context, username, slug, viewerUserID string) (string, error) {
	id, err := s.queries.GetEntryIDByAlias(ctx, db.GetEntryIDByAliasParams{Username: username, Slug: strings.ToLower(slug)})
	if err != nil {
		return "", notFoundOr(err, "resolve alias")
	}
	if _, err := s.entries().Viewable(ctx, id, viewerUserID); err != nil {
		return "", err
	}
	return id, nil
}
