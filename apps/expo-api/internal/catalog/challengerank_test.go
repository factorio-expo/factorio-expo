package catalog

import (
	"testing"
	"time"

	"factorio-expo/apps/expo-api/internal/challenge"
)

// The board's order and places, without a database: accepted entries by the
// goal (ties: the earlier revision, then the earlier submission), then pending,
// then rejected; votes rank accepted entries that have any.
func TestRankScored(t *testing.T) {
	at := func(minutes int) time.Time { return time.Date(2026, 11, 1, 0, minutes, 0, 0, time.UTC) }
	score := func(value float64) *float64 { return &value }
	accepted := func(id string, value float64, revision, submitted int, votes int64) scoredEntry {
		return scoredEntry{
			entryID: id, verdict: challenge.Verdict{Status: challenge.StatusAccepted, Score: score(value)},
			revisionCreatedAt: at(revision), submittedAt: at(submitted), votes: votes,
		}
	}
	scored := []scoredEntry{
		{entryID: "rejected", verdict: challenge.Verdict{Status: challenge.StatusRejected}, votes: 9},
		accepted("low", 1, 0, 0, 2),
		{entryID: "pending", verdict: challenge.Verdict{Status: challenge.StatusPending}},
		accepted("tie-late-revision", 5, 20, 1, 0),
		accepted("tie-early-revision", 5, 10, 2, 2),
		accepted("best", 9, 30, 3, 1),
	}

	rankScored(challenge.DirectionMax, scored)

	want := []struct {
		id             string
		rank, voteRank int
	}{
		{"best", 1, 3},
		{"tie-early-revision", 2, 2},
		{"tie-late-revision", 3, 0},
		{"low", 4, 1},
		{"pending", 0, 0},
		{"rejected", 0, 0},
	}
	for i, w := range want {
		got := scored[i]
		if got.entryID != w.id || got.rank != w.rank || got.voteRank != w.voteRank {
			t.Fatalf("place %d = %s rank %d vote rank %d; want %s rank %d vote rank %d",
				i, got.entryID, got.rank, got.voteRank, w.id, w.rank, w.voteRank)
		}
	}

	rankScored(challenge.DirectionMin, scored)
	if scored[0].entryID != "low" || scored[0].rank != 1 {
		t.Fatalf("lowest wins: first = %s rank %d", scored[0].entryID, scored[0].rank)
	}
}
