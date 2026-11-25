package catalog

import (
	"context"
	"fmt"
	"slices"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// change is one catalog transaction plus the side effects its entry writes
// imply. Writes record what they did on it: search intents go into the same
// transaction (the outbox rule: never write, then call), and open entry
// streams and the preview dispatcher hear about it only after the commit, so
// nobody re-reads a change that rolled back. Every entry write goes through
// inChange, and the enqueue queries have no other caller in the catalog;
// nothing outside the catalog repeats these effects (the search module only
// re-queues its own failed rebuild items).
type change struct {
	queries *db.Queries
	// changed are the entries whose streams must re-read, in order.
	changed []string
	// workQueued is set when preview or analysis intents were written.
	workQueued bool
	// collectHidden: the service wants the ids of hidden entries' cached URLs.
	collectHidden bool
	// hidden are those entries with their ids, collected inside the transaction.
	hidden []hiddenEntry
	// challenges are the challenges whose cached boards go after the commit.
	challenges []string
}

// hiddenEntry is an entry that left public view with the ids its cached
// preview images and raw strings are keyed by.
type hiddenEntry struct {
	id          string
	revisionIDs []string
	blueprints  []string
}

// entryWritten applies what a write to an entry's row implies, from the row
// before the write (nil for a new entry) and after it. A quick preview only
// tells its open streams (it is never indexed and takes no comments or XP).
// Any other entry loses its comment thread once it is not listed, earns its
// owner's upload award once it is (once per entry: xpUploaded), has its
// search sync queued (which indexes a listed entry and removes any other, and
// decides who may keep watching its stream), and queues the edge-cache purge
// when it just became private. Every write to an entry row goes through here,
// so no write path picks its own subset of these effects.
func (c *change) entryWritten(ctx context.Context, before *db.Entry, after db.Entry) error {
	if after.QuickPreview {
		c.touch(after.ID)
		return nil
	}
	if err := purgeComments(ctx, c.queries, after); err != nil {
		return err
	}
	if _, err := recordXP(ctx, c.queries, xpUploaded{entry: after}); err != nil {
		return err
	}
	if err := c.reindex(ctx, after.ID); err != nil {
		return err
	}
	// A shared cache may still hold the previews of what is now private.
	if before != nil && before.Visibility != "private" && after.Visibility == "private" {
		return c.hide(ctx, after.ID)
	}
	return nil
}

// deleteEntry soft-deletes a live entry (the row stays, restorable) and
// queues its removal from search and its edge-cache purge. ErrNotFound when
// it is already gone.
func (c *change) deleteEntry(ctx context.Context, entryID string) error {
	affected, err := c.queries.DeleteEntry(ctx, entryID)
	if err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return c.unindex(ctx, entryID)
}

// reindex queues the entry's search sync, which indexes a listed entry and
// removes any other, and marks it changed.
func (c *change) reindex(ctx context.Context, entryID string) error {
	if err := c.queries.EnqueueSearchIndexJob(ctx, entryID); err != nil {
		return fmt.Errorf("enqueue search index job: %w", err)
	}
	c.touch(entryID)
	return nil
}

// resync queues the entry's search sync like reindex, but may replace a
// queued removal: a staff restore, or a staff reindex of a deleted entry.
func (c *change) resync(ctx context.Context, entryID string) error {
	if err := c.queries.EnqueueSearchResyncJob(ctx, entryID); err != nil {
		return fmt.Errorf("enqueue search resync job: %w", err)
	}
	c.touch(entryID)
	return nil
}

// reindexTagged queues a reindex of every entry carrying the tag (before the
// tag goes) and marks them changed. It returns how many were queued.
func (c *change) reindexTagged(ctx context.Context, tagID string) (int, error) {
	entryIDs, err := c.queries.EnqueueSearchIndexJobsForTag(ctx, tagID)
	if err != nil {
		return 0, fmt.Errorf("queue reindex of tagged entries: %w", err)
	}
	for _, entryID := range entryIDs {
		c.touch(entryID)
	}
	return len(entryIDs), nil
}

// unindex queues the entry's removal from search (it was deleted) and marks
// it changed. A removal is never overwritten by a later index intent.
func (c *change) unindex(ctx context.Context, entryID string) error {
	if err := c.queries.EnqueueSearchDeleteJob(ctx, entryID); err != nil {
		return fmt.Errorf("enqueue search delete job: %w", err)
	}
	c.touch(entryID)
	return c.hide(ctx, entryID)
}

// hide records the entry's revision and blueprint ids for the edge cache purge
// after the commit: its previews and raw strings must not outlive it in shared
// caches.
// It costs nothing when no purge is configured.
func (c *change) hide(ctx context.Context, entryID string) error {
	if !c.collectHidden {
		return nil
	}
	revisions, err := c.queries.ListRevisionsByEntry(ctx, entryID)
	if err != nil {
		return fmt.Errorf("list revisions to purge: %w", err)
	}
	hidden := hiddenEntry{id: entryID}
	for _, revision := range revisions {
		hidden.revisionIDs = append(hidden.revisionIDs, revision.ID)
		blueprints, err := c.queries.ListBlueprintsByRevision(ctx, revision.ID)
		if err != nil {
			return fmt.Errorf("list blueprints to purge: %w", err)
		}
		for _, blueprint := range blueprints {
			hidden.blueprints = append(hidden.blueprints, blueprint.ID)
		}
	}
	c.hidden = append(c.hidden, hidden)
	return nil
}

// touch marks the entry changed without a search sync: a quick preview,
// which is never indexed.
func (c *change) touch(entryID string) {
	if slices.Contains(c.changed, entryID) {
		return
	}
	c.changed = append(c.changed, entryID)
}

// challengeChanged drops the challenge's cached board once the change
// commits (a submission, withdrawal, vote, rule change, verdict or void).
func (c *change) challengeChanged(challengeID string) {
	c.challenges = append(c.challenges, challengeID)
}

// queuedWork records that preview or analysis intents were written.
func (c *change) queuedWork() { c.workQueued = true }

// inChange runs fn in a transaction and, once it commits, tells the progress
// hub which entries changed and the dispatcher that work was queued.
func (s *Service) inChange(ctx context.Context, fn func(*change) error) error {
	c := &change{collectHidden: s.entryHidden != nil}
	if err := s.inTransaction(ctx, func(queries *db.Queries) error {
		c.queries = queries
		return fn(c)
	}); err != nil {
		return err
	}
	s.dropEntryBoards(ctx, c.changed)
	for _, challengeID := range c.challenges {
		s.scores.drop(challengeID)
	}
	if s.progress != nil {
		for _, entryID := range c.changed {
			s.progress.EntryChanged(entryID)
		}
	}
	if c.workQueued && s.workQueued != nil {
		s.workQueued()
	}
	if s.entryHidden != nil {
		for _, hidden := range c.hidden {
			s.entryHidden(hidden.id, hidden.revisionIDs, hidden.blueprints)
		}
	}
	return nil
}
