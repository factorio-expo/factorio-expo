package catalog

import (
	"context"
	"errors"
	"fmt"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// DeleteEntry soft-deletes an entry: it stamps deleted_at rather than removing
// the row, so its revisions, book/blueprint nodes, and previews stay intact,
// and it enqueues an entry_search_jobs "delete" so the Typesense
// projection removes the entry from the index.
func (s *Service) DeleteEntry(ctx context.Context, entryID string, auth Authorization) error {
	return s.inChange(ctx, func(c *change) error {
		entry, err := entryAccess{queries: c.queries}.Editable(ctx, entryID, auth)
		if err != nil {
			return err
		}
		return c.deleteEntry(ctx, entry.ID)
	})
}

// DeleteRevision removes one revision of an entry. Deleting the live revision
// promotes the most recent remaining one; an entry never ends up with no
// revision, so the last remaining revision cannot be deleted on its own.
func (s *Service) DeleteRevision(
	ctx context.Context, entryID, revisionID string, auth Authorization,
) (*Revision, error) {
	target, err := parseID(revisionID)
	if err != nil {
		return nil, err
	}

	var entry db.Entry
	err = s.inChange(ctx, func(c *change) error {
		queries := c.queries
		var err error
		entry, err = entryAccess{queries: queries}.Editable(ctx, entryID, auth)
		if err != nil {
			return err
		}
		revisions, err := queries.ListRevisionsByEntry(ctx, entry.ID)
		if err != nil {
			return fmt.Errorf("list revisions: %w", err)
		}
		if !containsRevision(revisions, target) {
			return ErrNotFound
		}
		if len(revisions) == 1 {
			return ErrLastRevision
		}
		if entry.CurrentRevisionID.String == target {
			successor := latestRevisionExcept(revisions, target)
			updated, err := queries.SetEntryCurrentRevision(ctx, db.SetEntryCurrentRevisionParams{
				ID:                entry.ID,
				CurrentRevisionID: pgText(successor),
			})
			if err != nil {
				return fmt.Errorf("promote previous revision: %w", err)
			}
			entry = updated
			// The live revision changed, so the entry's searchable facts
			// (label, description, icons, entity/leaf counts, ...) may have
			// changed with it.
			if err := c.reindex(ctx, entry.ID); err != nil {
				return err
			}
		}
		affected, err := queries.DeleteRevision(ctx, db.DeleteRevisionParams{
			ID: target, EntryID: entry.ID,
		})
		if err != nil {
			return fmt.Errorf("delete revision: %w", err)
		}
		if affected == 0 {
			return ErrNotFound
		}
		// Streams follow the live revision; an older one going changes nothing.
		c.touch(entry.ID)
		return nil
	})
	if err != nil {
		return nil, err
	}

	promoted, err := s.hydrateRevision(ctx, entry.CurrentRevisionID.String)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	return promoted, nil
}

func containsRevision(revisions []db.Revision, id string) bool {
	for _, revision := range revisions {
		if revision.ID == id {
			return true
		}
	}
	return false
}

// latestRevisionExcept picks the highest version number that is not the one
// being deleted. The input is already ordered newest first.
func latestRevisionExcept(revisions []db.Revision, excluded string) string {
	for _, revision := range revisions {
		if revision.ID != excluded {
			return revision.ID
		}
	}
	return ""
}
