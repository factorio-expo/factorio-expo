package catalog

import (
	"context"
	"slices"
	"sync"
	"time"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// DefaultChallengeScoresTTL is how long a challenge's scored board is reused
// (API_CHALLENGE_SCORES_TTL): the board reads every submission's stored
// analysis, and a launch-day crowd reloads it constantly. Submitting,
// withdrawing, voting, console changes and any change to an entry in the
// challenge (visibility, a revision, an analysis) drop it at once, so the TTL
// only bounds changes made behind the catalog's back. Zero turns the cache off.
const DefaultChallengeScoresTTL = 10 * time.Second

// scoreCache keeps one scored board per challenge.
type scoreCache struct {
	// ttl is how long a board is reused; zero or less caches nothing.
	ttl   time.Duration
	mu    sync.Mutex
	items map[string]cachedScores
}

type cachedScores struct {
	at time.Time
	// updatedAt and open say which version of the challenge it scored: a rule
	// change, or the deadline passing (the scored revision changes), drops it.
	updatedAt time.Time
	open      bool
	entries   []scoredEntry
}

func (c *scoreCache) get(row db.Challenge, now time.Time) ([]scoredEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[row.ID]
	if !ok || now.Sub(item.at) >= c.ttl || now.Before(item.at) ||
		!item.updatedAt.Equal(row.UpdatedAt.Time) || item.open != now.Before(row.EndsAt.Time) {
		return nil, false
	}
	// A copy: callers re-sort it.
	return slices.Clone(item.entries), true
}

func (c *scoreCache) put(row db.Challenge, now time.Time, entries []scoredEntry) {
	if c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = map[string]cachedScores{}
	}
	c.items[row.ID] = cachedScores{
		at: now, updatedAt: row.UpdatedAt.Time, open: now.Before(row.EndsAt.Time), entries: slices.Clone(entries),
	}
}

func (c *scoreCache) drop(challengeID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, challengeID)
}

func (c *scoreCache) dropAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.items)
}

// dropEntries drops the boards of the challenges the changed entries are in
// (after a commit that changed them). It reads nothing when the cache is off
// or empty; if the lookup fails, every board goes.
func (s *Service) dropEntryBoards(ctx context.Context, entryIDs []string) {
	if s.scores.ttl <= 0 || len(entryIDs) == 0 {
		return
	}
	for _, entryID := range entryIDs {
		rows, err := s.queries.ListEntryChallenges(ctx, entryID)
		if err != nil {
			s.scores.dropAll()
			return
		}
		for _, row := range rows {
			s.scores.drop(row.ID)
		}
	}
}
