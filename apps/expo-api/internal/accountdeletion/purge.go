package accountdeletion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// Step two of account deletion: deleted
// accounts are hard-deleted with their entries. The API's daily sweep does it
// DeletedAccountHold after the deletion (PurgeDeletedAccounts); staff can do
// it sooner from System -> Cleanups (HardDeleteDeletedUsers).

// DeletedAccountHold is how long a deleted account (its row, username and
// soft-deleted entries) is kept before the daily sweep hard-deletes it.
const DeletedAccountHold = 7 * 24 * time.Hour

// hardDeleteUserBatch is how many accounts or entries one hard-delete
// statement removes.
const hardDeleteUserBatch = 200

// CountHardDeletableUsers counts the deleted accounts the deleted accounts
// cleanup may remove now: those that own no entry any more.
func (s *Service) CountHardDeletableUsers(ctx context.Context) (int64, error) {
	count, err := s.queries.CountHardDeletableUsers(ctx)
	if err != nil {
		return 0, fmt.Errorf("count deleted accounts: %w", err)
	}
	return count, nil
}

// HardDeleteDeletedUsers (the deleted accounts cleanup) removes deleted
// accounts that own no entry any more, however recently deleted, with
// everything that hangs off them (bans, reports filed, comment tombstones);
// their username can be registered again (a banned one's once its ban hold
// ends). It returns how many it removed.
func (s *Service) HardDeleteDeletedUsers(ctx context.Context) (int, error) {
	return s.hardDeleteUsers(ctx, time.Now())
}

// PurgeDeletedAccounts (the API's daily sweep) hard-deletes the accounts
// deleted before deletedBefore: first their entries (rows and everything
// under them; the preview files are queued for the file sweep by trigger,
// and their search documents went at the soft delete), then the account rows.
// An entry whose search removal is still pending, and so its account, waits
// for the next run. A banned account goes too: its ban holds (written at the
// soft delete) keep its username and provider accounts refused until the ban
// ends. It returns how many accounts and entries it removed.
func (s *Service) PurgeDeletedAccounts(ctx context.Context, deletedBefore time.Time) (accounts, entries int, err error) {
	cutoff := pgtype.Timestamptz{Time: deletedBefore, Valid: true}
	entries, err = inBatches(func() ([]string, error) {
		return s.queries.HardDeleteDeletedUserEntries(ctx, db.HardDeleteDeletedUserEntriesParams{
			DeletedBefore: cutoff, RowLimit: hardDeleteUserBatch,
		})
	})
	if err != nil {
		return 0, entries, fmt.Errorf("hard-delete deleted accounts' entries: %w", err)
	}
	if entries > 0 {
		slog.Info("hard-deleted deleted accounts' entries", "count", entries)
	}
	accounts, err = s.hardDeleteUsers(ctx, deletedBefore)
	return accounts, entries, err
}

// hardDeleteUsers removes the deleted accounts deleted before deletedBefore
// that own no entry.
func (s *Service) hardDeleteUsers(ctx context.Context, deletedBefore time.Time) (int, error) {
	var removed []string
	_, err := inBatches(func() ([]string, error) {
		ids, err := s.queries.HardDeleteDeletedUsers(ctx, db.HardDeleteDeletedUsersParams{
			DeletedBefore: pgtype.Timestamptz{Time: deletedBefore, Valid: true},
			RowLimit:      hardDeleteUserBatch,
		})
		removed = append(removed, ids...)
		return ids, err
	})
	// The ids go into the ledger even when a later batch failed: those rows
	// are gone.
	if ledgerErr := s.ledger.Record(removed); ledgerErr != nil {
		slog.Error("record hard-deleted accounts in the ledger", "count", len(removed), "error", ledgerErr)
		if err == nil {
			err = ledgerErr
		}
	}
	if len(removed) > 0 {
		slog.Info("hard-deleted deleted accounts", "count", len(removed))
	}
	if err != nil {
		return len(removed), fmt.Errorf("hard-delete deleted accounts: %w", err)
	}
	return len(removed), nil
}

// inBatches runs a batched delete until a batch comes back short, and returns
// how many rows it removed in all.
func inBatches(batch func() ([]string, error)) (int, error) {
	total := 0
	for {
		ids, err := batch()
		total += len(ids)
		if err != nil || len(ids) < hardDeleteUserBatch {
			return total, err
		}
	}
}

// ReplayDeleted re-applies the ledger after a restore from a dump older than
// some hard deletes: every listed account still in the database is deleted
// like any other (step one, so it stops working at once; the daily sweep hard
// deletes it after the hold and records it again). Ids that are gone or
// already deleted are skipped. It returns how many accounts it deleted.
func (s *Service) ReplayDeleted(ctx context.Context, ids []string) (int, error) {
	deleted := 0
	for _, id := range ids {
		err := s.catalog.InChange(ctx, func(c *catalog.Unit) error {
			user, err := catalog.LockLiveUser(ctx, c.Queries(), id)
			if err != nil {
				return err
			}
			_, err = s.Delete(ctx, c, user, ByStaff)
			return err
		})
		if errors.Is(err, catalog.ErrNotFound) {
			continue
		}
		if err != nil {
			return deleted, fmt.Errorf("replay deletion of %s: %w", id, err)
		}
		deleted++
	}
	return deleted, nil
}
