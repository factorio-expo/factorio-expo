package staffops_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/staffops"
)

func TestDeleteOwnAccount(t *testing.T) {
	service, pool, owner, reporter := newStaffFixtures(t)
	ctx := context.Background()
	owned := createOwnedEntry(t, service, owner, "public")
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
	// The owner is banned from commenting for three days.
	expires := time.Now().Add(72 * time.Hour)
	if _, err := opsFor(service).CreateBan(ctx, admin, staffops.NewBan{
		Username: "social-test-alice", Actions: []string{"comment"}, ReasonCode: "spam", ExpiresAt: &expires,
	}); err != nil {
		t.Fatal(err)
	}
	insertSessions(t, pool, owner)
	insertSignInMethods(t, pool, owner, "owner-gh")

	if err := deletionFor(service).DeleteOwn(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetEntry(ctx, owned.ID, ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("owned entry: %v, want ErrNotFound", err)
	}
	if _, err := service.GetPublicUser(ctx, "social-test-alice"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("profile: %v, want ErrNotFound", err)
	}
	for query, want := range map[string]int{
		"SELECT count(*) FROM user_sessions WHERE user_id = $1":                                                                            0,
		"SELECT count(*) FROM auth_identities WHERE user_id = $1":                                                                          0,
		"SELECT count(*) FROM user_passkeys WHERE user_id = $1":                                                                            0,
		"SELECT count(*) FROM favorites WHERE user_id = $1":                                                                                0,
		"SELECT count(*) FROM users WHERE id = $1 AND deleted_at IS NOT NULL":                                                              1,
		"SELECT count(*) FROM comments WHERE author_user_id = $1 AND deleted_by = 'account'":                                               1,
		"SELECT count(*) FROM entry_search_jobs WHERE entry_id IN (SELECT id FROM entries WHERE owner_user_id = $1) AND action = 'delete'": 1,
	} {
		if n := countWhere(t, pool, query, owner); n != want {
			t.Errorf("%s = %d, want %d", query, n, want)
		}
	}
	// Reports stay open for staff, and it is no staff action.
	if n := countWhere(t, pool, "SELECT count(*) FROM comment_reports WHERE resolved_at IS NULL"); n != 1 {
		t.Errorf("open comment reports = %d, want 1", n)
	}
	if got := auditActions(t, pool); len(got) != 1 || got[0] != "alice-staff ban.create "+owner {
		t.Errorf("audit = %v, want only the ban", got)
	}
	// The ban holds its username and its GitHub account until it ends.
	for _, hash := range [][]byte{testBanHolds.Username("social-test-alice"), testBanHolds.Identity("github", "owner-gh")} {
		if n := countWhere(t, pool, "SELECT count(*) FROM ban_holds WHERE hash = $1 AND abs(extract(epoch FROM expires_at) - $2) < 1",
			hash, float64(expires.Unix())); n != 1 {
			t.Errorf("ban hold missing or not until the ban's end")
		}
	}
	if err := deletionFor(service).DeleteOwn(ctx, owner); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("deleting twice: %v, want ErrNotFound", err)
	}
}
