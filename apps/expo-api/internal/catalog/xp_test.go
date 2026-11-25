package catalog_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/staffops"
)

// progressOf reads a user's XP, level and badges from their profile.
func progressOf(t *testing.T, service *catalog.Service, username string) catalog.UserProgress {
	t.Helper()
	user, err := service.GetPublicUser(context.Background(), username)
	if err != nil {
		t.Fatalf("read profile %s: %v", username, err)
	}
	return user.Progress
}

// ageUser moves an account's creation two days back (past the new-account
// comment cap).
func ageUser(t *testing.T, pool *pgxpool.Pool, userID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"UPDATE users SET created_at = now() - interval '2 days' WHERE id = $1", userID); err != nil {
		t.Fatal(err)
	}
}

// publicUpload creates a public single-blueprint entry owned by userID (named
// after n, so the strings differ).
func publicUpload(t *testing.T, service *catalog.Service, userID string, n int) catalog.Entry {
	t.Helper()
	title := fmt.Sprintf("Upload %d", n)
	entry, err := service.CreateEntry(context.Background(), catalog.Submission{
		Exchange: readFixture(t, "bp-starter-smelter.txt"), Title: &title, OwnerUserID: userID, Visibility: "public",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	return entry
}

func TestXPUploadsAreCappedAndCountWhileLive(t *testing.T) {
	service, pool, userA, _ := newSocialFixtures(t)
	ctx := context.Background()

	if got := progressOf(t, service, "social-test-alice"); got.XP != 0 || got.Level != 1 || got.LevelXP != 0 ||
		got.NextLevelXP == nil || *got.NextLevelXP != 50 || len(got.Badges) != 0 {
		t.Fatalf("new account progress = %+v", got)
	}

	var entries []catalog.Entry
	for n := range 7 {
		entries = append(entries, publicUpload(t, service, userA, n))
	}
	// A private upload and an anonymous one earn nothing.
	if _, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readFixture(t, "bp-starter-smelter.txt"), OwnerUserID: userA, Visibility: "private",
	}); err != nil {
		t.Fatal(err)
	}
	// At most five uploads a day earn.
	got := progressOf(t, service, "social-test-alice")
	if got.XP != 50 || got.Level != 2 || got.LevelXP != 50 || *got.NextLevelXP != 150 {
		t.Fatalf("after 7 uploads = %+v, want 50 XP at level 2", got)
	}
	if !slices.Equal(got.Badges, []catalog.Badge{catalog.BadgeFirstUpload}) {
		t.Fatalf("badges = %v", got.Badges)
	}

	// Removed content takes its points back; the ledger keeps the row.
	if err := service.DeleteEntry(ctx, entries[0].ID, catalog.Authorization{UserID: userA}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := progressOf(t, service, "social-test-alice"); got.XP != 40 {
		t.Fatalf("after a delete XP = %d, want 40", got.XP)
	}
	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM xp_events WHERE user_id = $1", userA).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 5 {
		t.Fatalf("ledger rows = %d, want 5", rows)
	}

	// A capped upload made public the next day earns then; the cap counts
	// every row of the day, so a delete does not free a slot.
	if _, err := pool.Exec(ctx, "UPDATE xp_events SET created_at = created_at - interval '1 day' WHERE user_id = $1", userA); err != nil {
		t.Fatal(err)
	}
	private := catalog.VisibilityPrivate
	public := catalog.VisibilityPublic
	for _, visibility := range []*catalog.Visibility{&private, &public} {
		if _, err := service.UpdateEntryMetadata(ctx, entries[6].ID, catalog.Authorization{UserID: userA},
			catalog.EntryMetadataUpdate{Visibility: visibility}); err != nil {
			t.Fatalf("change visibility: %v", err)
		}
	}
	if got := progressOf(t, service, "social-test-alice"); got.XP != 50 {
		t.Fatalf("after making an entry public XP = %d, want 50", got.XP)
	}
}

func TestXPFavoritesCollectionsAndComments(t *testing.T) {
	service, pool, userA, userB := newSocialFixtures(t)
	ctx := context.Background()
	ageUser(t, pool, userB)
	entry := publicUpload(t, service, userA, 1)

	// Own favorites earn nothing; another user's earn once.
	if err := service.AddFavorite(ctx, userA, entry.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := service.AddFavorite(ctx, userB, entry.ID); err != nil {
			t.Fatal(err)
		}
		if err := service.RemoveFavorite(ctx, userB, entry.ID); err != nil {
			t.Fatal(err)
		}
	}
	collection, err := service.CreateCollection(ctx, userB, "Keepers", "", "public")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := service.AddCollectionEntry(ctx, collection.ID, userB, entry.ID, 0); err != nil {
			t.Fatal(err)
		}
	}
	if got := progressOf(t, service, "social-test-alice"); got.XP != 10+2+3 ||
		!slices.Equal(got.Badges, []catalog.Badge{catalog.BadgeFirstUpload, catalog.BadgeFirstFavorite}) {
		t.Fatalf("alice = %+v, want 15 XP with the upload and favorite badges", got)
	}

	// Comments: at most five a day earn; a removed one stops counting.
	var first catalog.Comment
	for n := range 6 {
		comment, err := service.CreateComment(ctx, entry.ID, catalog.Authorization{UserID: userB}, nil,
			catalog.NewComment{Body: fmt.Sprintf("Nice build number %d", n)})
		if err != nil {
			t.Fatalf("comment %d: %v", n, err)
		}
		if n == 0 {
			first = comment
		}
	}
	if got := progressOf(t, service, "social-test-bob"); got.XP != 5 {
		t.Fatalf("bob XP = %d, want 5", got.XP)
	}
	if err := service.RemoveComment(ctx, entry.ID, first.Number, catalog.Authorization{UserID: userB}); err != nil {
		t.Fatal(err)
	}
	if got := progressOf(t, service, "social-test-bob"); got.XP != 4 {
		t.Fatalf("bob XP after a removal = %d, want 4", got.XP)
	}

	// A ban stops earning.
	if _, err := opsFor(service).CreateBan(ctx, audit.Staff{Username: "tester"}, staffops.NewBan{
		Username: "social-test-alice", Actions: []string{"comment"}, ReasonCode: "spam", Permanent: true,
	}); err != nil {
		t.Fatalf("ban: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET created_at = now() - interval '3 days' WHERE id = $1", userB); err != nil {
		t.Fatal(err)
	}
	another := publicUpload(t, service, userB, 2)
	if err := service.AddFavorite(ctx, userA, another.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.RemoveFavorite(ctx, userA, entry.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.AddFavorite(ctx, userB, entry.ID); err != nil {
		t.Fatal(err)
	}
	if got := progressOf(t, service, "social-test-alice"); got.XP != 15 {
		t.Fatalf("banned alice XP = %d, want 15", got.XP)
	}
	if got := progressOf(t, service, "social-test-bob"); got.XP != 4+10+2 {
		t.Fatalf("bob XP = %d, want 16", got.XP)
	}
}

func TestChallengeAwardsOnceWhenClosed(t *testing.T) {
	service, pool, userA, userB := newSocialFixtures(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "TRUNCATE challenges CASCADE"); err != nil {
		t.Fatal(err)
	}
	staff := audit.Staff{Username: "tester"}
	now := time.Now()
	challengeID, err := opsFor(service).CreateChallenge(ctx, staff, challengeFields(now))
	if err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).PublishChallenge(ctx, staff, challengeID, true); err != nil {
		t.Fatal(err)
	}
	exchange := readFixture(t, "bp-parametric-assembler-row.txt")
	for _, user := range []string{userA, userB} {
		entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange, OwnerUserID: user, Visibility: "public"})
		if err != nil {
			t.Fatal(err)
		}
		storeGoldenAnalysis(t, service, pool, entry.Revision.Root.ID)
		if err := service.SubmitChallengeEntry(ctx, "rare-circuits", user, entry.ID); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	// Nothing is awarded while the challenge runs.
	if awarded, err := service.AwardClosedChallenges(ctx); err != nil || awarded != 0 {
		t.Fatalf("open challenge awards = %d, %v", awarded, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE challenges SET starts_at = now() - interval '3 days',
		ends_at = now() - interval '2 days', voting_ends_at = now() - interval '1 day' WHERE id = $1`, challengeID); err != nil {
		t.Fatal(err)
	}
	// The scored revisions were made while it was open.
	if _, err := pool.Exec(ctx, "UPDATE revisions SET created_at = now() - interval '60 hours'"); err != nil {
		t.Fatal(err)
	}
	// The first run awards the challenge, a second one nothing.
	for run, want := range []int{1, 0} {
		awarded, err := service.AwardClosedChallenges(ctx)
		if err != nil || awarded != want {
			t.Fatalf("run %d awards = %d, %v; want %d", run, awarded, err, want)
		}
	}
	// Both scored the same: the earlier revision wins (alice's).
	alice, bob := progressOf(t, service, "social-test-alice"), progressOf(t, service, "social-test-bob")
	if alice.XP != 10+25+500 || !slices.Contains(alice.Badges, catalog.BadgeChallengeWinner) ||
		!slices.Contains(alice.Badges, catalog.BadgeFirstChallenge) {
		t.Fatalf("alice = %+v, want the win", alice)
	}
	if bob.XP != 10+25+300 || !slices.Contains(bob.Badges, catalog.BadgeChallengePodium) ||
		slices.Contains(bob.Badges, catalog.BadgeChallengeWinner) {
		t.Fatalf("bob = %+v, want second place", bob)
	}
}
