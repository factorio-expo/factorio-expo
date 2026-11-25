package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/mods"
)

// The units below are the catalog's whole surface for code that lives beside
// it (the staff operations, account deletion): one transaction that carries
// the entry-change effects. An entry's lifecycle (lock, delete, restore, its
// reports) goes through the Unit's verbs, which bring the search and edge-cache
// effects with them; Queries serves everything else that code owns (bans,
// tags, settings, its audit rows).

// Unit is one open catalog change (see change): its transaction's queries and
// the entry-change vocabulary. Search intents go into the transaction; open
// streams, the dispatcher and the edge cache hear about the change only after
// it commits.
type Unit struct{ c *change }

// Queries are the generated queries bound to the change's transaction.
func (u *Unit) Queries() *db.Queries { return u.c.queries }

// LockEntry locks an entry row in any state (deleted, expired, a quick
// preview) for a staff change. ErrNotFound when there is none.
func (u *Unit) LockEntry(ctx context.Context, entryID string) (db.Entry, error) {
	entry, err := u.c.queries.GetEntryForStaffUpdate(ctx, entryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Entry{}, ErrNotFound
	}
	if err != nil {
		return db.Entry{}, fmt.Errorf("lock entry: %w", err)
	}
	return entry, nil
}

// DeleteEntry soft-deletes a live entry like its owner's delete: restorable,
// and removed from search and shared caches. ErrNotFound when it is gone.
func (u *Unit) DeleteEntry(ctx context.Context, entryID string) error {
	return u.c.deleteEntry(ctx, entryID)
}

// RestoreEntry undoes a soft delete (and clears a passed expiry), then queues
// a search sync that may replace the queued removal.
func (u *Unit) RestoreEntry(ctx context.Context, entryID string) (db.Entry, error) {
	restored, err := u.c.queries.RestoreEntry(ctx, entryID)
	if err != nil {
		return db.Entry{}, fmt.Errorf("restore entry: %w", err)
	}
	return restored, u.c.resync(ctx, entryID)
}

// ResolveEntryReports closes the entry's open reports in the name of a staff
// member and returns how many.
func (u *Unit) ResolveEntryReports(ctx context.Context, entryID, by string) (int64, error) {
	resolved, err := u.c.queries.ResolveEntryReports(ctx, db.ResolveEntryReportsParams{
		EntryID: entryID, ResolvedBy: pgText(by),
	})
	if err != nil {
		return 0, fmt.Errorf("resolve entry reports: %w", err)
	}
	return resolved, nil
}

// Resync is Reindex that may replace a queued removal (a staff restore or
// reindex).
func (u *Unit) Resync(ctx context.Context, entryID string) error { return u.c.resync(ctx, entryID) }

// ReindexTagged queues a reindex of every entry carrying the tag and returns
// how many.
func (u *Unit) ReindexTagged(ctx context.Context, tagID string) (int, error) {
	return u.c.reindexTagged(ctx, tagID)
}

// ChallengeChanged drops the challenge's cached board once the change
// commits: the console's rule changes, verdicts and voided votes.
func (u *Unit) ChallengeChanged(challengeID string) { u.c.challengeChanged(challengeID) }

// Hide queues only the edge-cache purge of an entry that left public view.
func (u *Unit) Hide(ctx context.Context, entryID string) error { return u.c.hide(ctx, entryID) }

// InChange runs fn in one transaction with the entry-change effects.
func (s *Service) InChange(ctx context.Context, fn func(*Unit) error) error {
	return s.inChange(ctx, func(c *change) error { return fn(&Unit{c: c}) })
}

// InTransaction runs fn in one transaction, for writes that change no entry.
func (s *Service) InTransaction(ctx context.Context, fn func(*db.Queries) error) error {
	return s.inTransaction(ctx, fn)
}

// Queries are the generated queries on the pool, for reads.
func (s *Service) Queries() *db.Queries { return s.queries }

// EdgePurgeEnabled reports whether a CDN purge is configured.
func (s *Service) EdgePurgeEnabled() bool { return s.entryHidden != nil }

// IPBanDuration is how long an address ban lasts.
func (s *Service) IPBanDuration() time.Duration { return s.ipBanDuration }

// Mods is the mod name table detections run against.
func (s *Service) Mods() *mods.Table { return s.mods }

// InvalidateSettings drops the cached global settings after a change.
func (s *Service) InvalidateSettings() { s.settings.Invalidate() }
