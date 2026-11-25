package staffops

import (
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

func stamp(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func vote(user, entry string, accountMade, voted time.Time, voided bool) db.ListChallengeVotesForOversightRow {
	row := db.ListChallengeVotesForOversightRow{
		UserID: user, EntryID: entry, Username: "name-" + user,
		UserCreatedAt: stamp(accountMade), CreatedAt: stamp(voted),
	}
	if voided {
		row.VoidedAt = stamp(voted.Add(time.Hour))
		row.VoidReason = "farmed"
	}
	return row
}

func voterFlags(t *testing.T, oversight VoteOversight, user string) []string {
	t.Helper()
	for _, voter := range oversight.Voters {
		if voter.UserID == user {
			return voter.Flags
		}
	}
	t.Fatalf("voter %s missing", user)
	return nil
}

func TestAnalyzeVotesFlagsNewAccountsAndSignupClusters(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	old := start.AddDate(0, -3, 0)
	var votes []db.ListChallengeVotesForOversightRow
	// An old account that votes normally.
	votes = append(votes, vote("old", "entryA", old.AddDate(0, -2, 0), start.AddDate(0, 0, 5), false))
	// A fresh account made a day before its first vote, but before the start.
	votes = append(votes, vote("fresh", "entryA", start.AddDate(0, 0, -1), start.AddDate(0, 0, 2), false))
	// Three accounts made minutes apart, long before: a signup cluster.
	for i, user := range []string{"c1", "c2", "c3"} {
		made := old.Add(time.Duration(i) * 10 * time.Minute)
		votes = append(votes, vote(user, "entryB", made, start.AddDate(0, 0, 6), false))
	}
	// An account made after the start.
	votes = append(votes, vote("late", "entryB", start.AddDate(0, 0, 1), start.AddDate(0, 0, 30), false))

	out := analyzeVotes(votes, start)
	if got := voterFlags(t, out, "old"); len(got) != 0 {
		t.Errorf("old account flags = %v, want none", got)
	}
	if got := voterFlags(t, out, "fresh"); !slices.Equal(got, []string{FlagNewAccount}) {
		t.Errorf("fresh account flags = %v, want new_account", got)
	}
	for _, user := range []string{"c1", "c2", "c3"} {
		if got := voterFlags(t, out, user); !slices.Equal(got, []string{FlagSignupCluster}) {
			t.Errorf("%s flags = %v, want signup_cluster", user, got)
		}
	}
	if got := voterFlags(t, out, "late"); !slices.Contains(got, FlagNewAccount) {
		t.Errorf("late account flags = %v, want new_account", got)
	}
	// Flagged voters come first.
	if len(out.Voters[0].Flags) == 0 {
		t.Errorf("first voter %s is not flagged", out.Voters[0].UserID)
	}
	var entryB OversightEntry
	for _, entry := range out.Entries {
		if entry.EntryID == "entryB" {
			entryB = entry
		}
	}
	if entryB.Votes != 4 || entryB.FlaggedVotes != 4 || !slices.Contains(entryB.Flags, FlagFlaggedVoters) {
		t.Errorf("entryB = %+v, want 4 flagged votes and the flagged_voters sign", entryB)
	}
}

func TestAnalyzeVotesFlagsSpikesAndIgnoresVoidedVotes(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	old := start.AddDate(0, -6, 0)
	burst := start.AddDate(0, 0, 3).Add(30 * time.Minute)
	var votes []db.ListChallengeVotesForOversightRow
	for i := range 6 {
		// Six old accounts, spread apart, vote within the same hour.
		made := old.AddDate(0, 0, i*10)
		votes = append(votes, vote(string(rune('a'+i)), "spiky", made, burst.Add(time.Duration(i)*time.Minute), false))
	}
	// A voided vote does not count and is not in the buckets.
	votes = append(votes, vote("v", "spiky", old.AddDate(0, 0, 90), burst, true))
	// Another entry's votes are spread over days.
	for i := range 6 {
		made := old.AddDate(0, 0, i*10)
		votes = append(votes, vote(string(rune('k'+i)), "calm", made, start.AddDate(0, 0, i+1), false))
	}
	out := analyzeVotes(votes, start)
	spiky, calm := OversightEntry{}, OversightEntry{}
	for _, entry := range out.Entries {
		switch entry.EntryID {
		case "spiky":
			spiky = entry
		case "calm":
			calm = entry
		}
	}
	if spiky.Votes != 6 || spiky.Voided != 1 {
		t.Errorf("spiky votes = %d voided = %d, want 6 and 1", spiky.Votes, spiky.Voided)
	}
	if !slices.Contains(spiky.Flags, FlagSpike) {
		t.Errorf("spiky flags = %v, want a spike", spiky.Flags)
	}
	if slices.Contains(calm.Flags, FlagSpike) {
		t.Errorf("calm flags = %v, want no spike", calm.Flags)
	}
	if out.Entries[0].EntryID != "spiky" {
		t.Errorf("first entry = %s, want the flagged one first", out.Entries[0].EntryID)
	}
}
