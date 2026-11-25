package staffops_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/accountdeletion"
	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/staffops"
)

func TestTimeCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 11, 2, 10, 4, 5, 123456789, time.UTC)
	gotAt, gotID, ok := staffops.ParseTimeCursor(staffops.FormatTimeCursor(at, "abcDEF123456"))
	if !ok || !gotAt.Equal(at) || gotID != "abcDEF123456" {
		t.Fatalf("got %v %q %v", gotAt, gotID, ok)
	}
	for _, bad := range []string{"x", "2026-11-02T10:04:05Z_short", "nottime_abcDEF123456"} {
		if _, _, ok := staffops.ParseTimeCursor(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestListStaffUsers(t *testing.T) {
	service, _, owner, reporter := newStaffFixtures(t)
	ctx := context.Background()
	createOwnedEntry(t, service, owner, "public")
	expires := time.Now().Add(24 * time.Hour)
	if _, err := opsFor(service).CreateBan(ctx, admin, staffops.NewBan{
		Username: "social-test-bob", Actions: []string{"report", "comment"}, ReasonCode: "spam", ExpiresAt: &expires,
	}); err != nil {
		t.Fatal(err)
	}

	page, err := opsFor(service).ListStaffUsers(ctx, admin, staffops.StaffUserQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Counts.All != 2 || page.Counts.Active != 2 || page.Counts.Banned != 1 || page.Counts.Deleted != 0 {
		t.Fatalf("counts = %+v", page.Counts)
	}
	byID := map[string]staffops.StaffUser{}
	for _, user := range page.Users {
		byID[user.ID] = user
	}
	if byID[owner].Entries != 1 || len(byID[owner].BanActions) != 0 {
		t.Fatalf("owner = %+v", byID[owner])
	}
	if !slices.Equal(byID[reporter].BanActions, []string{"comment", "report"}) {
		t.Fatalf("reporter ban actions = %v", byID[reporter].BanActions)
	}

	// Search matches part of the username, any case.
	for _, c := range []struct {
		query staffops.StaffUserQuery
		want  []string
	}{
		{staffops.StaffUserQuery{Search: "BOB"}, []string{reporter}},
		{staffops.StaffUserQuery{Status: "banned"}, []string{reporter}},
		{staffops.StaffUserQuery{Status: "deleted"}, []string{}},
	} {
		page, err := opsFor(service).ListStaffUsers(ctx, admin, c.query)
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, user := range page.Users {
			ids = append(ids, user.ID)
		}
		if c.want != nil && !slices.Equal(ids, c.want) {
			t.Errorf("%+v: got %v, want %v", c.query, ids, c.want)
		}
	}
	if _, err := opsFor(service).ListStaffUsers(ctx, admin, staffops.StaffUserQuery{Status: "odd"}); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("unknown status: %v", err)
	}
}

// insertSessions gives each user a signed-in session.
func insertSessions(t *testing.T, pool *pgxpool.Pool, userIDs ...string) {
	t.Helper()
	for i, userID := range userIDs {
		if _, err := pool.Exec(context.Background(),
			"INSERT INTO user_sessions (id, user_id, expires_at) VALUES ($1, $2, now() + interval '1 day')",
			fmt.Sprintf("sessionTest%d", i), userID); err != nil {
			t.Fatalf("insert session: %v", err)
		}
	}
}

// insertSignInMethods gives the user a GitHub identity and a passkey.
func insertSignInMethods(t *testing.T, pool *pgxpool.Pool, userID, githubID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		"INSERT INTO auth_identities (provider, provider_user_id, user_id) VALUES ('github', $1, $2)",
		githubID, userID); err != nil {
		t.Fatalf("insert identity: %v", err)
	}
	if _, err := pool.Exec(ctx,
		"INSERT INTO user_passkeys (id, user_id, credential_id, credential) VALUES ($1, $1, $2, '{}')",
		userID, []byte(githubID)); err != nil {
		t.Fatalf("insert passkey: %v", err)
	}
}

func TestStaffLogoutUser(t *testing.T) {
	service, pool, owner, reporter := newStaffFixtures(t)
	ctx := context.Background()
	insertSessions(t, pool, owner, reporter)
	if err := opsFor(service).StaffLogoutUser(ctx, admin, owner, "lost laptop"); err != nil {
		t.Fatal(err)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM user_sessions WHERE user_id = $1", owner); n != 0 {
		t.Fatalf("sessions left = %d, want 0", n)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM user_sessions WHERE user_id = $1", reporter); n != 1 {
		t.Fatalf("another account's sessions = %d, want 1", n)
	}
	if got := auditActions(t, pool); !slices.Equal(got, []string{"alice-staff user.logout " + owner}) {
		t.Fatalf("audit = %v", got)
	}
}

func TestStaffDeleteUser(t *testing.T) {
	service, pool, owner, reporter := newStaffFixtures(t)
	ctx := context.Background()
	owned := createOwnedEntry(t, service, owner, "public")
	anonymous, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), Visibility: "public"})
	if err != nil {
		t.Fatal(err)
	}
	other := createOwnedEntry(t, service, reporter, "public")
	if _, err := service.CreateComment(ctx, other.ID, catalog.Authorization{UserID: owner}, nil, catalog.NewComment{Body: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := service.ReportComment(ctx, other.ID, 1, catalog.Authorization{UserID: reporter}, nil, catalog.CommentReport{Reason: "spam"}); err != nil {
		t.Fatal(err)
	}
	if err := service.AddFavorite(ctx, owner, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateCollection(ctx, owner, "Mine", "", "public"); err != nil {
		t.Fatal(err)
	}

	if err := opsFor(service).StaffDeleteUser(ctx, admin, owner, ""); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("delete without a reason: %v", err)
	}
	insertSessions(t, pool, owner)
	insertSignInMethods(t, pool, owner, "owner-gh")
	insertSignInMethods(t, pool, reporter, "reporter-gh")
	if err := opsFor(service).StaffDeleteUser(ctx, admin, owner, "spam account"); err != nil {
		t.Fatal(err)
	}

	if _, err := service.GetPublicUser(ctx, "social-test-alice"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("deleted profile: %v, want ErrNotFound", err)
	}
	if _, err := service.GetEntry(ctx, owned.ID, ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("owned entry: %v, want ErrNotFound", err)
	}
	if _, err := service.GetEntry(ctx, anonymous.ID, ""); err != nil {
		t.Fatalf("anonymous entry: %v", err)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM entry_search_jobs WHERE entry_id = $1 AND action = 'delete'", owned.ID); n != 1 {
		t.Fatalf("search removals = %d, want 1", n)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM comments WHERE author_user_id = $1 AND deleted_by = 'admin' AND body = ''", owner); n != 1 {
		t.Fatalf("tombstoned comments = %d, want 1", n)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM comment_reports WHERE resolved_at IS NULL"); n != 0 {
		t.Fatalf("open comment reports = %d, want 0", n)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM favorites WHERE user_id = $1", owner) +
		countWhere(t, pool, "SELECT count(*) FROM collections WHERE user_id = $1", owner); n != 0 {
		t.Fatalf("favorites and collections left = %d", n)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM user_sessions WHERE user_id = $1", owner); n != 0 {
		t.Fatalf("sessions left = %d, want 0", n)
	}
	// Its sign-in methods go at once (the provider account may sign up
	// anew); the username stays held; another account's methods stay.
	if n := countWhere(t, pool, "SELECT count(*) FROM auth_identities WHERE user_id = $1", owner) +
		countWhere(t, pool, "SELECT count(*) FROM user_passkeys WHERE user_id = $1", owner); n != 0 {
		t.Fatalf("sign-in methods left = %d, want 0", n)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM auth_identities WHERE user_id = $1", reporter) +
		countWhere(t, pool, "SELECT count(*) FROM user_passkeys WHERE user_id = $1", reporter); n != 2 {
		t.Fatalf("another account's sign-in methods = %d, want 2", n)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM users WHERE username = 'social-test-alice'"); n != 1 {
		t.Fatal("the username is no longer held")
	}
	// Not banned: no ban hold.
	if n := countWhere(t, pool, "SELECT count(*) FROM ban_holds"); n != 0 {
		t.Fatalf("ban holds = %d, want 0", n)
	}
	if err := opsFor(service).StaffDeleteUser(ctx, admin, owner, "again"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("deleting twice: %v", err)
	}
	detail, err := opsFor(service).GetStaffUser(ctx, admin, owner)
	if err != nil || detail.DeletedAt == nil || detail.DeletedEntries != 1 {
		t.Fatalf("detail = %+v, %v", detail, err)
	}

	// The row stays until its entries are hard-deleted.
	if count, err := deletionFor(service).CountHardDeletableUsers(ctx); err != nil || count != 0 {
		t.Fatalf("hard-deletable with entries left = %d, %v", count, err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM entry_search_jobs"); err != nil {
		t.Fatal(err)
	}
	if _, err := opsFor(service).HardDeleteDeletedEntries(ctx); err != nil {
		t.Fatal(err)
	}
	if removed, err := deletionFor(service).HardDeleteDeletedUsers(ctx); err != nil || removed != 1 {
		t.Fatalf("hard delete = %d, %v; want 1", removed, err)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM users WHERE id = $1", owner); n != 0 {
		t.Fatal("the account row is still there")
	}
}

func TestPurgeDeletedAccounts(t *testing.T) {
	service, pool, owner, reporter := newStaffFixtures(t)
	ctx := context.Background()
	recent := "recentCarol1"
	addUser(t, pool, recent, "social-test-carol")
	for _, userID := range []string{owner, reporter, recent} {
		createOwnedEntry(t, service, userID, "public")
	}
	// The reporter (social-test-bob) is banned for good.
	if _, err := opsFor(service).CreateBan(ctx, admin, staffops.NewBan{
		Username: "social-test-bob", Actions: []string{"comment"}, ReasonCode: "spam", Permanent: true,
	}); err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{owner, reporter, recent} {
		if err := opsFor(service).StaffDeleteUser(ctx, admin, userID, "spam account"); err != nil {
			t.Fatal(err)
		}
	}
	purge := func() (int, int) {
		t.Helper()
		accounts, entries, err := deletionFor(service).PurgeDeletedAccounts(ctx, time.Now().Add(-accountdeletion.DeletedAccountHold))
		if err != nil {
			t.Fatal(err)
		}
		return accounts, entries
	}
	ownedEntries := func(userID string) int {
		return countWhere(t, pool, "SELECT count(*) FROM entries WHERE owner_user_id = $1", userID)
	}

	// Deleted a week ago, but the search removals are still pending: nothing yet.
	if _, err := pool.Exec(ctx, "UPDATE users SET deleted_at = now() - interval '8 days' WHERE id = ANY($1)",
		[]string{owner, reporter}); err != nil {
		t.Fatal(err)
	}
	if accounts, entries := purge(); accounts != 0 || entries != 0 {
		t.Fatalf("with search removals pending: %d accounts, %d entries; want none", accounts, entries)
	}

	// Once they ran: the week-old accounts' entries and rows go, the banned
	// one's too: its ban hold (written at the deletion) keeps its username
	// refused for a year. The account deleted today waits for its week.
	if _, err := pool.Exec(ctx, "DELETE FROM entry_search_jobs"); err != nil {
		t.Fatal(err)
	}
	if accounts, entries := purge(); accounts != 2 || entries != 2 {
		t.Fatalf("purge = %d accounts, %d entries; want 2 and 2", accounts, entries)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM users WHERE id = ANY($1)", []string{owner, reporter}); n != 0 {
		t.Error("a week-old account is still there")
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM ban_holds WHERE hash = $1 AND expires_at > now() + interval '300 days'",
		testBanHolds.Username("social-test-bob")); n != 1 {
		t.Error("the permanently banned account's username is not held for a year")
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM users WHERE id = $1", recent); n != 1 || ownedEntries(recent) != 1 {
		t.Errorf("account deleted today: row %d, entries %d; want both kept", n, ownedEntries(recent))
	}

	// The staff cleanup removes deleted accounts without entries at once.
	if _, err := pool.Exec(ctx, "DELETE FROM entry_search_jobs"); err != nil {
		t.Fatal(err)
	}
	if _, err := opsFor(service).HardDeleteDeletedEntries(ctx); err != nil {
		t.Fatal(err)
	}
	if removed, err := deletionFor(service).HardDeleteDeletedUsers(ctx); err != nil || removed != 1 {
		t.Fatalf("staff cleanup = %d, %v; want the account deleted today", removed, err)
	}
}

// Clearing a profile empties its status and links (pins stay), records the
// audit row, and refuses an unknown account.
func TestStaffClearProfile(t *testing.T) {
	service, pool, owner, _ := newStaffFixtures(t)
	ctx := context.Background()
	status := "reported text"
	links := []catalog.SocialLinkInput{{Platform: "github", Handle: "octocat"}}
	if _, err := service.UpdateProfile(ctx, owner, catalog.ProfileUpdate{Status: &status, Links: &links}); err != nil {
		t.Fatal(err)
	}
	profile, err := service.GetPublicUser(ctx, "social-test-alice")
	if err != nil || profile.Status != status || len(profile.Links) != 1 {
		t.Fatalf("profile before = %+v, %v", profile, err)
	}

	if err := opsFor(service).StaffClearProfile(ctx, admin, owner, "offensive status"); err != nil {
		t.Fatal(err)
	}
	profile, err = service.GetPublicUser(ctx, "social-test-alice")
	if err != nil || profile.Status != "" || len(profile.Links) != 0 {
		t.Fatalf("profile after = %+v, %v", profile, err)
	}
	if actions := auditActions(t, pool); len(actions) != 1 {
		t.Fatalf("audit = %v, want one row", actions)
	}
	if err := opsFor(service).StaffClearProfile(ctx, admin, "AAAAAAAAAAAA", ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("unknown account: %v", err)
	}
}

// A hard delete records the account id in the ledger, and replaying the
// ledger after a restore deletes an account that came back with an older dump.
func TestLedgerRecordsHardDeletesAndReplaysThem(t *testing.T) {
	service, pool, owner, _ := newStaffFixtures(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ledger", "deleted-users.log")
	ledger, err := accountdeletion.NewLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	deletion := accountdeletion.New(service, testBanHolds).WithLedger(ledger)

	createOwnedEntry(t, service, owner, "public")
	if err := opsFor(service).StaffDeleteUser(ctx, admin, owner, "spam account"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM entry_search_jobs"); err != nil {
		t.Fatal(err)
	}
	if _, err := opsFor(service).HardDeleteDeletedEntries(ctx); err != nil {
		t.Fatal(err)
	}
	if removed, err := deletion.HardDeleteDeletedUsers(ctx); err != nil || removed != 1 {
		t.Fatalf("hard delete = %d, %v; want 1", removed, err)
	}
	ids, err := accountdeletion.ReadLedger(path)
	if err != nil || !slices.Equal(ids, []string{owner}) {
		t.Fatalf("ledger = %v, %v; want the deleted account", ids, err)
	}

	// A restore from an older dump brings the account back.
	addUser(t, pool, owner, "social-test-alice")
	deleted, err := deletion.ReplayDeleted(ctx, ids)
	if err != nil || deleted != 1 {
		t.Fatalf("replay = %d, %v; want the restored account deleted", deleted, err)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM users WHERE id = $1 AND deleted_at IS NOT NULL", owner); n != 1 {
		t.Fatal("the restored account is not deleted")
	}
	// Replaying again changes nothing; an id that never existed is skipped.
	if deleted, err := deletion.ReplayDeleted(ctx, append(ids, "zzzzzzzzzzzz")); err != nil || deleted != 0 {
		t.Fatalf("second replay = %d, %v; want 0", deleted, err)
	}
}
