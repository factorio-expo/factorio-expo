package staffops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// Voting oversight (docs/admin.md#challenges): the console lists who voted
// and flags the usual signs of vote farming, and staff void single votes or
// all of a voter's votes. Nothing here punishes anyone: a voided vote only
// stops counting.

// Oversight thresholds.
const (
	// newAccountAge: a voter whose account is younger than this at its first
	// vote, or was made after the challenge started, is flagged.
	newAccountAge = 7 * 24 * time.Hour
	// clusterWindow and clusterMin: this many voters whose accounts were made
	// within the window of the previous one are a signup cluster.
	clusterWindow = time.Hour
	clusterMin    = 3
	// spikeMin and spikeShare: an entry that got at least spikeMin votes in
	// one hour, and at least spikeShare of all its votes, has a spike.
	spikeMin   = 5
	spikeShare = 0.5
)

// Voter flags.
const (
	FlagNewAccount    = "new_account"
	FlagSignupCluster = "signup_cluster"
)

// Entry flags.
const (
	FlagSpike         = "spike"
	FlagFlaggedVoters = "flagged_voters"
)

// OversightVote is one vote of a voter.
type OversightVote struct {
	EntryID    string    `json:"entry_id" doc:"The entry voted for."`
	VotedAt    time.Time `json:"voted_at" doc:"When the vote was given."`
	Voided     bool      `json:"voided" doc:"Whether staff voided it (it no longer counts)."`
	VoidReason string    `json:"void_reason" doc:"Why staff voided it."`
}

// OversightVoter is one voter with the signs that make staff look at them.
type OversightVoter struct {
	UserID           string          `json:"user_id" doc:"The voter's account id."`
	Username         string          `json:"username" doc:"The voter's username."`
	AccountCreatedAt time.Time       `json:"account_created_at" doc:"When the account was made."`
	Votes            []OversightVote `json:"votes" doc:"Their votes on active entries, oldest first."`
	Flags            []string        `json:"flags" doc:"The signs found: new_account (a new account) or signup_cluster (one of several accounts made around the same time)."`
}

// VoteBucket is the votes an entry got in one hour (UTC).
type VoteBucket struct {
	Hour  time.Time `json:"hour" doc:"The start of the hour."`
	Votes int       `json:"votes" doc:"Counted votes given in it."`
}

// OversightEntry is one entry's vote picture.
type OversightEntry struct {
	EntryID      string       `json:"entry_id" doc:"The entry."`
	Votes        int          `json:"votes" doc:"Counted votes."`
	Voided       int          `json:"voided" doc:"Voided votes."`
	FlaggedVotes int          `json:"flagged_votes" doc:"Counted votes from flagged voters."`
	Hours        []VoteBucket `json:"hours" doc:"Counted votes per hour, oldest first."`
	Flags        []string     `json:"flags" doc:"The signs found: spike (many votes in one hour) or flagged_voters (most votes from flagged voters)."`
}

// VoteOversight is the console's voting oversight of one challenge.
type VoteOversight struct {
	Voters  []OversightVoter `json:"voters" doc:"Everyone who voted, flagged voters first."`
	Entries []OversightEntry `json:"entries" doc:"Every voted-for entry, flagged ones first."`
}

// analyzeVotes flags the voters and entries of a challenge's votes, which
// come oldest first. A pure function of the rows.
func analyzeVotes(votes []db.ListChallengeVotesForOversightRow, startsAt time.Time) VoteOversight {
	byVoter := map[string]*OversightVoter{}
	firstVote := map[string]time.Time{}
	var order []string
	for _, vote := range votes {
		voter, ok := byVoter[vote.UserID]
		if !ok {
			voter = &OversightVoter{
				UserID: vote.UserID, Username: vote.Username, AccountCreatedAt: vote.UserCreatedAt.Time,
				Votes: []OversightVote{}, Flags: []string{},
			}
			byVoter[vote.UserID] = voter
			firstVote[vote.UserID] = vote.CreatedAt.Time
			order = append(order, vote.UserID)
		}
		voter.Votes = append(voter.Votes, OversightVote{
			EntryID: vote.EntryID, VotedAt: vote.CreatedAt.Time, Voided: vote.VoidedAt.Valid, VoidReason: vote.VoidReason,
		})
	}
	for id, voter := range byVoter {
		if voter.AccountCreatedAt.After(startsAt) || firstVote[id].Sub(voter.AccountCreatedAt) < newAccountAge {
			voter.Flags = append(voter.Flags, FlagNewAccount)
		}
	}
	flagSignupClusters(byVoter, order)

	flagged := map[string]bool{}
	for id, voter := range byVoter {
		flagged[id] = len(voter.Flags) > 0
	}
	entries := map[string]*OversightEntry{}
	var entryOrder []string
	hours := map[string]map[time.Time]int{}
	for _, vote := range votes {
		entry, ok := entries[vote.EntryID]
		if !ok {
			entry = &OversightEntry{EntryID: vote.EntryID, Hours: []VoteBucket{}, Flags: []string{}}
			entries[vote.EntryID] = entry
			entryOrder = append(entryOrder, vote.EntryID)
			hours[vote.EntryID] = map[time.Time]int{}
		}
		if vote.VoidedAt.Valid {
			entry.Voided++
			continue
		}
		entry.Votes++
		if flagged[vote.UserID] {
			entry.FlaggedVotes++
		}
		hours[vote.EntryID][vote.CreatedAt.Time.UTC().Truncate(time.Hour)]++
	}
	for id, entry := range entries {
		peak := 0
		for hour, count := range hours[id] {
			entry.Hours = append(entry.Hours, VoteBucket{Hour: hour, Votes: count})
			peak = max(peak, count)
		}
		sort.Slice(entry.Hours, func(i, j int) bool { return entry.Hours[i].Hour.Before(entry.Hours[j].Hour) })
		if peak >= spikeMin && float64(peak) >= spikeShare*float64(entry.Votes) {
			entry.Flags = append(entry.Flags, FlagSpike)
		}
		if entry.Votes >= clusterMin && float64(entry.FlaggedVotes) >= spikeShare*float64(entry.Votes) {
			entry.Flags = append(entry.Flags, FlagFlaggedVoters)
		}
	}

	out := VoteOversight{Voters: make([]OversightVoter, 0, len(order)), Entries: make([]OversightEntry, 0, len(entryOrder))}
	for _, id := range order {
		out.Voters = append(out.Voters, *byVoter[id])
	}
	for _, id := range entryOrder {
		out.Entries = append(out.Entries, *entries[id])
	}
	sort.SliceStable(out.Voters, func(i, j int) bool { return len(out.Voters[i].Flags) > len(out.Voters[j].Flags) })
	sort.SliceStable(out.Entries, func(i, j int) bool {
		if len(out.Entries[i].Flags) != len(out.Entries[j].Flags) {
			return len(out.Entries[i].Flags) > len(out.Entries[j].Flags)
		}
		return out.Entries[i].Votes > out.Entries[j].Votes
	})
	return out
}

// flagSignupClusters flags the voters whose accounts were made within
// clusterWindow of the previous voter's, when there are at least clusterMin
// of them.
func flagSignupClusters(byVoter map[string]*OversightVoter, order []string) {
	voters := make([]*OversightVoter, 0, len(order))
	for _, id := range order {
		voters = append(voters, byVoter[id])
	}
	sort.SliceStable(voters, func(i, j int) bool { return voters[i].AccountCreatedAt.Before(voters[j].AccountCreatedAt) })
	for start := 0; start < len(voters); {
		end := start + 1
		for end < len(voters) && voters[end].AccountCreatedAt.Sub(voters[end-1].AccountCreatedAt) <= clusterWindow {
			end++
		}
		if end-start >= clusterMin {
			for _, voter := range voters[start:end] {
				if !slices.Contains(voter.Flags, FlagSignupCluster) {
					voter.Flags = append(voter.Flags, FlagSignupCluster)
				}
			}
		}
		start = end
	}
}

// ChallengeOversight lists a challenge's voters and entries with the signs of
// vote farming. The caller reads the challenge page for entry titles.
func (s *Service) ChallengeOversight(ctx context.Context, challengeID string) (VoteOversight, error) {
	id, err := parseID(challengeID)
	if err != nil {
		return VoteOversight{}, err
	}
	row, err := s.queries.GetChallenge(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return VoteOversight{}, catalog.ErrNotFound
	}
	if err != nil {
		return VoteOversight{}, fmt.Errorf("read challenge: %w", err)
	}
	votes, err := s.queries.ListChallengeVotesForOversight(ctx, id)
	if err != nil {
		return VoteOversight{}, fmt.Errorf("list challenge votes: %w", err)
	}
	return analyzeVotes(votes, row.StartsAt.Time), nil
}

// VoidChallengeVotes voids (void) or counts again (not void) a voter's votes
// in the challenge: the one on entryID, or every one when it is empty. A
// reason is required to void. It returns how many votes changed.
func (s *Service) VoidChallengeVotes(
	ctx context.Context, by audit.Staff, challengeID, userID, entryID string, void bool, reason string,
) (int64, error) {
	challengeID, err := parseID(challengeID)
	if err != nil {
		return 0, err
	}
	if userID, err = parseID(userID); err != nil {
		return 0, err
	}
	if entryID != "" {
		if entryID, err = parseID(entryID); err != nil {
			return 0, err
		}
	}
	if reason, err = audit.CleanReason(reason); err != nil {
		return 0, err
	}
	if void && reason == "" {
		return 0, invalidStaffRequest("give a reason")
	}
	var changed int64
	err = s.catalog.InChange(ctx, func(c *catalog.Unit) error {
		queries := c.Queries()
		c.ChallengeChanged(challengeID)
		if _, err := queries.LockChallenge(ctx, challengeID); errors.Is(err, pgx.ErrNoRows) {
			return catalog.ErrNotFound
		} else if err != nil {
			return fmt.Errorf("lock challenge: %w", err)
		}
		action := "challenge.unvoid_votes"
		if void {
			changed, err = queries.VoidChallengeVotes(ctx, db.VoidChallengeVotesParams{
				Reason: reason, ChallengeID: challengeID, UserID: userID, EntryID: pgText(entryID),
			})
			action = "challenge.void_votes"
		} else {
			changed, err = queries.UnvoidChallengeVotes(ctx, db.UnvoidChallengeVotesParams{
				ChallengeID: challengeID, UserID: userID, EntryID: pgText(entryID),
			})
		}
		if err != nil {
			return fmt.Errorf("change challenge votes: %w", err)
		}
		if changed == 0 {
			return catalog.ErrNotFound
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: action, TargetType: "challenge", TargetID: challengeID, Reason: reason,
			After: map[string]any{"user_id": userID, "entry_id": entryID, "votes": changed},
		})
	})
	if err != nil {
		return 0, err
	}
	return changed, nil
}
