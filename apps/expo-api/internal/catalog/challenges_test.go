package catalog_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/challenge"
	"factorio-expo/apps/expo-api/internal/staffops"
	"factorio-expo/internal/analysisresult"
)

// storeGoldenAnalysis stores test-data/analysis/analysis-succeeded.json (rare
// electronic circuits at 0.5/s, a 12x7 footprint) as the leaf's analysis and
// closes its job, as the dispatcher's import does.
func storeGoldenAnalysis(t *testing.T, service *catalog.Service, pool *pgxpool.Pool, blueprintID string) {
	t.Helper()
	data, err := os.ReadFile("../../../../test-data/analysis/analysis-succeeded.json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	result, _, err := analysisresult.Parse(data, envelope.JobID)
	if err != nil || result == nil {
		t.Fatalf("parse golden analysis: %v", err)
	}
	stored, err := result.Stored()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.StoreProduction(context.Background(), blueprintID, stored); err != nil {
		t.Fatalf("store production: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM phase_jobs WHERE phase = 'analysis' AND blueprint_id = $1", blueprintID); err != nil {
		t.Fatal(err)
	}
}

func challengeFields(now time.Time) staffops.ChallengeFields {
	return staffops.ChallengeFields{
		Slug: "rare-circuits", Title: "Rare circuits",
		Rules: challenge.Rules{
			Goal: challenge.Goal{
				Param: "rate_out", Direction: challenge.DirectionMax,
				Target: challenge.Target{Type: "item", Name: "electronic-circuit", Quality: "rare"},
			},
		},
		StartsAt: now.Add(-time.Hour), EndsAt: now.Add(24 * time.Hour), VotingEndsAt: now.Add(48 * time.Hour),
		RequireNewRevisions: true, VotersBeforeStart: true, VotingEnabled: true, VotesPerUser: 3,
		LiveLeaderboard: true,
	}
}

func onlyEntry(t *testing.T, detail catalog.ChallengeDetail) catalog.ChallengeEntry {
	t.Helper()
	if len(detail.Entries) != 1 {
		t.Fatalf("entries = %+v, want one", detail.Entries)
	}
	return detail.Entries[0]
}

func TestChallengeSubmissionScoringVotesAndWithdrawal(t *testing.T) {
	service, pool, userA, userB := newSocialFixtures(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "TRUNCATE challenges CASCADE"); err != nil {
		t.Fatal(err)
	}
	staff := audit.Staff{Username: "tester"}
	now := time.Now()

	challengeID, err := opsFor(service).CreateChallenge(ctx, staff, challengeFields(now))
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	// A draft is not public.
	if _, err := service.GetChallenge(ctx, "rare-circuits", ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("draft read: %v, want not found", err)
	}
	if err := opsFor(service).PublishChallenge(ctx, staff, challengeID, true); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// A single 2.1 blueprint.
	exchange := readFixture(t, "bp-parametric-assembler-row.txt")
	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange, OwnerUserID: userA, Visibility: "public"})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	private, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange, OwnerUserID: userB, Visibility: "private"})
	if err != nil {
		t.Fatalf("create private entry: %v", err)
	}

	for _, refused := range []struct {
		user, entry string
	}{{userB, entry.ID}, {userB, private.ID}} {
		if err := service.SubmitChallengeEntry(ctx, "rare-circuits", refused.user, refused.entry); !errors.Is(err, catalog.ErrChallengeRefused) {
			t.Fatalf("submit %s by %s: %v, want refused", refused.entry, refused.user, err)
		}
	}
	if err := service.SubmitChallengeEntry(ctx, "rare-circuits", userA, entry.ID); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// Entering the same entry again changes nothing.
	if err := service.SubmitChallengeEntry(ctx, "rare-circuits", userA, entry.ID); err != nil {
		t.Fatalf("submit again: %v", err)
	}

	detail, err := service.GetChallenge(ctx, "rare-circuits", userA)
	if err != nil {
		t.Fatalf("read challenge: %v", err)
	}
	if got := onlyEntry(t, detail); got.Status != challenge.StatusPending || got.Entry.ID != entry.ID {
		t.Fatalf("before analysis = %+v, want pending", got)
	}
	if detail.Viewer == nil || detail.Viewer.EntryID != entry.ID || !detail.Viewer.CanSubmit {
		t.Fatalf("viewer = %+v", detail.Viewer)
	}

	// The picker's candidates: the owner's public single blueprints, by title.
	if cards, total, err := service.ListChallengeCandidates(ctx, userA, "", 20, 0); err != nil || total != 1 ||
		len(cards) != 1 || cards[0].ID != entry.ID {
		t.Fatalf("candidates = %+v (total %d), %v, want the entry", cards, total, err)
	}
	if cards, total, err := service.ListChallengeCandidates(ctx, userA, "no such title", 20, 0); err != nil ||
		total != 0 || len(cards) != 0 {
		t.Fatalf("candidates for no match = %+v (total %d), %v", cards, total, err)
	}
	if cards, _, err := service.ListChallengeCandidates(ctx, userB, "", 20, 0); err != nil || len(cards) != 0 {
		t.Fatalf("another account's candidates = %+v, %v, want none", cards, err)
	}

	storeGoldenAnalysis(t, service, pool, entry.Revision.Root.ID)
	// A finished analysis is an entry change: it drops the board at once.
	detail, err = service.GetChallenge(ctx, "rare-circuits", "")
	if err != nil {
		t.Fatalf("read challenge: %v", err)
	}
	got := onlyEntry(t, detail)
	if got.Status != challenge.StatusAccepted || got.Score == nil || *got.Score != 0.5 || got.Rank != 1 ||
		got.ScoreText != "0.5/s" {
		t.Fatalf("after analysis = %+v, want accepted at 0.5/s", got)
	}
	if got.Votes != nil {
		t.Fatalf("votes shown while open: %+v", got)
	}

	// The console's test box checks any entry against the rules, line by line.
	tested, err := opsFor(service).TestChallengeEntry(ctx, challengeID, entry.ID)
	if err != nil {
		t.Fatalf("test entry: %v", err)
	}
	if tested.Status != challenge.StatusAccepted || tested.ScoreText != "0.5/s" || len(tested.Lines) == 0 ||
		!tested.Lines[len(tested.Lines)-1].Goal || tested.Lines[len(tested.Lines)-1].Value != "0.5/s" {
		t.Fatalf("test entry = %+v, want accepted at 0.5/s with the goal measured", tested)
	}
	if _, err := opsFor(service).TestChallengeEntry(ctx, challengeID, "zzzzzzzzzzzz"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("test unknown entry: %v, want not found", err)
	}

	// A threshold the blueprint misses rejects it, once the rescore is confirmed.
	fields := challengeFields(now)
	fields.Rules.Restrictions = []challenge.Restriction{{Param: "footprint_area", Op: challenge.OpAtMost, Value: 10}}
	if err := opsFor(service).UpdateChallenge(ctx, staff, challengeID, fields); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("unconfirmed rescore: %v, want refused", err)
	}
	fields.ConfirmRescore = true
	if err := opsFor(service).UpdateChallenge(ctx, staff, challengeID, fields); err != nil {
		t.Fatalf("update: %v", err)
	}
	detail, err = service.GetChallenge(ctx, "rare-circuits", "")
	if err != nil {
		t.Fatalf("read challenge: %v", err)
	}
	if len(detail.Entries) != 0 || detail.EntryCount != 0 {
		t.Fatalf("a rejected entry is public: %+v", detail.Entries)
	}
	detail, err = service.GetChallenge(ctx, "rare-circuits", userA)
	if err != nil {
		t.Fatalf("read challenge: %v", err)
	}
	if got := onlyEntry(t, detail); got.Status != challenge.StatusRejected || len(got.Reasons) != 1 {
		t.Fatalf("with threshold = %+v, want rejected for its owner", got)
	}
	if detail, err = service.GetChallenge(ctx, "rare-circuits", userB); err != nil || len(detail.Entries) != 0 {
		t.Fatalf("another account sees a rejected entry: %+v, %v", detail.Entries, err)
	}
	fields.Rules.Restrictions = nil
	if err := opsFor(service).UpdateChallenge(ctx, staff, challengeID, fields); err != nil {
		t.Fatalf("update: %v", err)
	}

	// Votes: only accounts made before the start, never on one's own entry.
	if err := service.VoteChallengeEntry(ctx, "rare-circuits", userB, entry.ID, true); !errors.Is(err, catalog.ErrChallengeForbidden) {
		t.Fatalf("vote by a new account: %v, want forbidden", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET created_at = now() - interval '1 day'"); err != nil {
		t.Fatal(err)
	}
	if err := service.VoteChallengeEntry(ctx, "rare-circuits", userA, entry.ID, true); !errors.Is(err, catalog.ErrChallengeRefused) {
		t.Fatalf("vote on own entry: %v, want refused", err)
	}
	if err := service.VoteChallengeEntry(ctx, "rare-circuits", userB, entry.ID, true); err != nil {
		t.Fatalf("vote: %v", err)
	}
	detail, err = service.GetChallenge(ctx, "rare-circuits", userB)
	if err != nil {
		t.Fatalf("read challenge: %v", err)
	}
	if !onlyEntry(t, detail).VotedByViewer || detail.Viewer.VotesLeft != 2 {
		t.Fatalf("after vote: entry %+v, viewer %+v", onlyEntry(t, detail), detail.Viewer)
	}

	// Staff void the vote: it stops counting but stays, so the voter cannot
	// take it back and give it again. Voiding needs a reason.
	if _, err := opsFor(service).VoidChallengeVotes(ctx, staff, challengeID, userB, "", true, ""); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("void without a reason: %v, want refused", err)
	}
	if changed, err := opsFor(service).VoidChallengeVotes(ctx, staff, challengeID, userB, "", true, "farmed"); err != nil || changed != 1 {
		t.Fatalf("void: %d, %v, want 1", changed, err)
	}
	oversight, err := opsFor(service).ChallengeOversight(ctx, challengeID)
	if err != nil || len(oversight.Entries) != 1 || oversight.Entries[0].Votes != 0 || oversight.Entries[0].Voided != 1 {
		t.Fatalf("oversight after void = %+v, %v, want one voided vote", oversight, err)
	}
	standings, err := opsFor(service).GetStaffChallenge(ctx, challengeID)
	if err != nil || len(standings.Entries) != 1 || standings.Entries[0].Votes != 0 {
		t.Fatalf("votes after void = %+v, %v, want 0", standings.Entries, err)
	}
	if err := service.VoteChallengeEntry(ctx, "rare-circuits", userB, entry.ID, false); err != nil {
		t.Fatalf("take back a voided vote: %v", err)
	}
	oversight, err = opsFor(service).ChallengeOversight(ctx, challengeID)
	if err != nil || len(oversight.Entries) != 1 || oversight.Entries[0].Voided != 1 {
		t.Fatalf("a voided vote was taken back: %+v, %v", oversight, err)
	}
	if changed, err := opsFor(service).VoidChallengeVotes(ctx, staff, challengeID, userB, "", false, ""); err != nil || changed != 1 {
		t.Fatalf("unvoid: %d, %v, want 1", changed, err)
	}
	if err := service.VoteChallengeEntry(ctx, "rare-circuits", userB, entry.ID, false); err != nil {
		t.Fatalf("take back vote: %v", err)
	}

	// Leaving public withdraws the entry for good.
	unlisted := catalog.VisibilityUnlisted
	if _, err := service.UpdateEntryMetadata(ctx, entry.ID, catalog.Authorization{UserID: userA},
		catalog.EntryMetadataUpdate{Visibility: &unlisted}); err != nil {
		t.Fatalf("make unlisted: %v", err)
	}
	public := catalog.VisibilityPublic
	if _, err := service.UpdateEntryMetadata(ctx, entry.ID, catalog.Authorization{UserID: userA},
		catalog.EntryMetadataUpdate{Visibility: &public}); err != nil {
		t.Fatalf("make public: %v", err)
	}
	detail, err = service.GetChallenge(ctx, "rare-circuits", userA)
	if err != nil {
		t.Fatalf("read challenge: %v", err)
	}
	if len(detail.Entries) != 0 || detail.Viewer.EntryID != "" {
		t.Fatalf("after leaving public: entries %+v, viewer %+v", detail.Entries, detail.Viewer)
	}
	read, err := service.GetEntry(ctx, entry.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Challenges) != 0 {
		t.Fatalf("entry challenges = %+v, want none", read.Challenges)
	}

	// Staff can disqualify; the challenge cannot be unpublished once started.
	if err := service.SubmitChallengeEntry(ctx, "rare-circuits", userA, entry.ID); err != nil {
		t.Fatalf("submit again: %v", err)
	}
	staffDetail, err := opsFor(service).GetStaffChallenge(ctx, challengeID)
	if err != nil || len(staffDetail.Entries) != 1 {
		t.Fatalf("staff detail: %+v, %v", staffDetail, err)
	}
	if err := opsFor(service).SetSubmissionVerdict(ctx, staff, challengeID, staffDetail.Entries[0].SubmissionID,
		challenge.StatusRejected, "copied"); err != nil {
		t.Fatalf("verdict: %v", err)
	}
	detail, err = service.GetChallenge(ctx, "rare-circuits", userA)
	if err != nil {
		t.Fatalf("read challenge: %v", err)
	}
	if got := onlyEntry(t, detail); got.Status != challenge.StatusRejected || got.Reasons[0] != "copied" {
		t.Fatalf("after verdict = %+v", got)
	}
	if err := opsFor(service).PublishChallenge(ctx, staff, challengeID, false); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("unpublish started: %v, want refused", err)
	}
}

func TestChallengeHiddenUntilStart(t *testing.T) {
	service, pool, _, _ := newSocialFixtures(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "TRUNCATE challenges CASCADE"); err != nil {
		t.Fatal(err)
	}
	staff := audit.Staff{Username: "tester"}
	now := time.Now()
	fields := challengeFields(now)
	fields.StartsAt, fields.EndsAt, fields.VotingEndsAt = now.Add(time.Hour), now.Add(2*time.Hour), now.Add(3*time.Hour)
	fields.HiddenUntilStart = true
	id, err := opsFor(service).CreateChallenge(ctx, staff, fields)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := opsFor(service).PublishChallenge(ctx, staff, id, true); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := service.GetChallenge(ctx, fields.Slug, ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("hidden read: %v, want not found", err)
	}
	listed, err := service.ListChallenges(ctx)
	if err != nil || len(listed) != 0 {
		t.Fatalf("hidden listed: %+v, %v", listed, err)
	}
	// Once it starts it shows.
	if _, err := pool.Exec(ctx, "UPDATE challenges SET starts_at = now() - interval '1 minute'"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetChallenge(ctx, fields.Slug, ""); err != nil {
		t.Fatalf("started read: %v", err)
	}
}
