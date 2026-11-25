package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// entryAccess is the single answer to "is this entry live and visible to this
// viewer, and may they edit it?". Every entry read and write in the catalog
// goes through it. Missing, deleted and invisible entries are all ErrNotFound,
// so a private entry's existence is never revealed.
type entryAccess struct {
	queries *db.Queries
}

func (s *Service) entries() entryAccess {
	return entryAccess{queries: s.queries}
}

// Viewable loads a live entry the viewer (empty for anonymous) may see:
// public and unlisted entries are visible to everyone (unlisted ones are just
// never listed), private ones only to their owner.
func (a entryAccess) Viewable(ctx context.Context, entryID, viewerUserID string) (db.Entry, error) {
	id, err := parseID(entryID)
	if err != nil {
		return db.Entry{}, err
	}
	entry, err := a.queries.GetEntry(ctx, id)
	if err != nil {
		return db.Entry{}, notFoundOr(err, "read entry")
	}
	if !visibleTo(entry.Visibility, entry.OwnerUserID, viewerUserID) {
		return db.Entry{}, ErrNotFound
	}
	return entry, nil
}

// Editable loads a live entry and checks the caller may modify it: the owner,
// or the edit token for an anonymous entry. It takes the row lock, so it must
// run on a transaction-scoped query set and the caller's writes happen under
// the same lock; a concurrent delete or revision cannot slip in between the
// check and the write.
func (a entryAccess) Editable(ctx context.Context, entryID string, auth Authorization) (db.Entry, error) {
	id, err := parseID(entryID)
	if err != nil {
		return db.Entry{}, err
	}
	entry, err := a.queries.GetEntryForUpdate(ctx, id)
	if err != nil {
		return db.Entry{}, notFoundOr(err, "read entry for update")
	}
	if err := authorize(entry, auth); err != nil {
		return db.Entry{}, err
	}
	if entry.QuickPreview {
		// A quick preview is read-only: it can only be published or expire.
		return db.Entry{}, ErrQuickPreview
	}
	return entry, nil
}

// Publishable is Editable for publishing: it locks a live entry the caller
// may edit (owner, or edit token of an anonymous one) that is a quick preview
// or still anonymous.
func (a entryAccess) Publishable(ctx context.Context, entryID string, auth Authorization) (db.Entry, error) {
	id, err := parseID(entryID)
	if err != nil {
		return db.Entry{}, err
	}
	entry, err := a.queries.GetEntryForUpdate(ctx, id)
	if err != nil {
		return db.Entry{}, notFoundOr(err, "read entry for update")
	}
	if err := authorize(entry, auth); err != nil {
		return db.Entry{}, err
	}
	if !entry.QuickPreview && entry.OwnerUserID.Valid {
		return db.Entry{}, ErrAlreadyPublished
	}
	return entry, nil
}

// Collectable loads a live entry the viewer may see and add to their
// favorites or collections; a quick preview cannot be.
func (a entryAccess) Collectable(ctx context.Context, entryID, viewerUserID string) (db.Entry, error) {
	entry, err := a.Viewable(ctx, entryID, viewerUserID)
	if err != nil {
		return db.Entry{}, err
	}
	if entry.QuickPreview {
		return db.Entry{}, ErrQuickPreview
	}
	return entry, nil
}

// Commentable locks a live entry the viewer may see that takes comments
// (a listed one). The lock orders comment writes against a visibility change,
// which purges the thread in its own transaction.
func (a entryAccess) Commentable(ctx context.Context, entryID, viewerUserID string) (db.Entry, error) {
	id, err := parseID(entryID)
	if err != nil {
		return db.Entry{}, err
	}
	entry, err := a.queries.GetEntryForUpdate(ctx, id)
	if err != nil {
		return db.Entry{}, notFoundOr(err, "read entry for update")
	}
	if !visibleTo(entry.Visibility, entry.OwnerUserID, viewerUserID) {
		return db.Entry{}, ErrNotFound
	}
	if !entryListed(entry, time.Now()) {
		return db.Entry{}, ErrCommentsClosed
	}
	return entry, nil
}

// CanEdit reports edit permission without locking, for read-only checks. For
// a quick preview it means the caller may publish it; for an anonymous entry,
// edit it or (signed in) publish it to their account.
func (a entryAccess) CanEdit(ctx context.Context, entryID string, auth Authorization) (bool, error) {
	entry, err := a.Viewable(ctx, entryID, auth.UserID)
	if err != nil {
		return false, err
	}
	return authorize(entry, auth) == nil, nil
}

func notFoundOr(err error, action string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", action, err)
}

// authorize allows a mutation when the caller owns the entry, or when the
// entry is still anonymous and the caller presents its edit token.
func authorize(entry db.Entry, auth Authorization) error {
	if !visibleTo(entry.Visibility, entry.OwnerUserID, auth.UserID) {
		return ErrNotFound
	}
	if entry.OwnerUserID.Valid {
		if auth.UserID != "" && auth.UserID == entry.OwnerUserID.String {
			return nil
		}
		return ErrForbidden
	}
	if entry.EditTokenHash.Valid && auth.EditToken != "" &&
		subtleEqual(entry.EditTokenHash.String, hashEditToken(auth.EditToken)) {
		return nil
	}
	return ErrForbidden
}
