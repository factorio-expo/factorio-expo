package staffops_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/staffops"
)

var (
	admin     = audit.Staff{Username: "alice-staff"}
	colleague = audit.Staff{Username: "bob-staff"}
)

func newStaffFixtures(t *testing.T) (*catalog.Service, *pgxpool.Pool, string, string) {
	t.Helper()
	service, pool, owner, reporter := newCommentFixtures(t)
	if _, err := pool.Exec(context.Background(), "TRUNCATE admin_actions"); err != nil {
		t.Fatalf("truncate admin actions: %v", err)
	}
	return service, pool, owner, reporter
}

// auditActions lists the recorded staff actions as "staff action target",
// oldest first.
func auditActions(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		"SELECT staff_username || ' ' || action || ' ' || target_id FROM admin_actions ORDER BY created_at, id")
	if err != nil {
		t.Fatalf("read admin actions: %v", err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
	}
	return actions
}

func countWhere(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestEntryReportQueue(t *testing.T) {
	service, pool, owner, reporter := newStaffFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	quiet := createOwnedEntry(t, service, owner, "public")
	address := ipHash("203.0.113.7")
	if err := service.ReportEntry(ctx, entry.ID, catalog.Authorization{UserID: reporter}, nil,
		catalog.EntryReport{Reason: "stolen", Note: "mine"}); err != nil {
		t.Fatal(err)
	}
	if err := service.ReportEntry(ctx, entry.ID, catalog.Authorization{}, address, catalog.EntryReport{Reason: "spam"}); err != nil {
		t.Fatal(err)
	}

	groups, err := opsFor(service).ListEntryReports(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Entry.ID != entry.ID || len(groups[0].Reports) != 2 {
		t.Fatalf("queue = %+v, want the one entry with two reports", groups)
	}
	group := groups[0]
	if group.Entry.Owner != "social-test-alice" || group.Entry.Gone || group.Entry.CurrentRevision != 1 {
		t.Fatalf("entry = %+v", group.Entry)
	}
	signedIn, anonymous := group.Reports[0], group.Reports[1]
	if signedIn.Reason != "stolen" || signedIn.Reporter != (staffops.Reporter{Username: "social-test-bob"}) ||
		anonymous.Reporter != (staffops.Reporter{Anonymous: true}) || signedIn.TitleSnapshot != entry.Title {
		t.Fatalf("reports = %+v", group.Reports)
	}

	if !group.Takedown {
		t.Fatal("a stolen report does not mark the group a takedown")
	}

	// A takedown reported later still comes before older reports.
	if err := service.ReportEntry(ctx, quiet.ID, catalog.Authorization{UserID: reporter}, nil,
		catalog.EntryReport{Reason: "spam"}); err != nil {
		t.Fatal(err)
	}
	stolen := createOwnedEntry(t, service, owner, "public")
	if err := service.ReportEntry(ctx, stolen.ID, catalog.Authorization{UserID: reporter}, nil,
		catalog.EntryReport{Reason: "stolen", Note: "https://example.test/original"}); err != nil {
		t.Fatal(err)
	}
	if groups, _ = opsFor(service).ListEntryReports(ctx, admin); len(groups) != 3 || groups[0].Entry.ID != entry.ID ||
		groups[1].Entry.ID != stolen.ID || groups[2].Entry.ID != quiet.ID || groups[2].Takedown {
		t.Fatalf("queue order = %+v, want both takedowns, then the spam report", groups)
	}
	if err := opsFor(service).ResolveEntryReports(ctx, colleague, stolen.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).ResolveEntryReports(ctx, colleague, quiet.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).ResolveEntryReports(ctx, colleague, quiet.ID, ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("resolving an entry without reports: %v, want ErrNotFound", err)
	}
	if err := opsFor(service).ResolveEntryReports(ctx, colleague, entry.ID, "fine as it is"); err != nil {
		t.Fatal(err)
	}
	if groups, _ := opsFor(service).ListEntryReports(ctx, admin); len(groups) != 0 {
		t.Fatalf("queue after resolving = %+v, want empty", groups)
	}
	// Resolving drops the anonymous reporter's address hash.
	if n := countWhere(t, pool, "SELECT count(*) FROM entry_reports WHERE reporter_ip_hash IS NOT NULL OR resolved_by <> 'bob-staff'"); n != 0 {
		t.Fatalf("%d resolved reports keep an address hash or another resolver", n)
	}
	if got := auditActions(t, pool); len(got) != 3 || got[2] != "bob-staff entry_reports.resolve "+entry.ID {
		t.Fatalf("audit = %v", got)
	}
}

func TestStaffDeleteEntry(t *testing.T) {
	service, pool, owner, reporter := newStaffFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	if err := service.ReportEntry(ctx, entry.ID, catalog.Authorization{UserID: reporter}, nil, catalog.EntryReport{Reason: "offensive"}); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).StaffDeleteEntry(ctx, admin, entry.ID, " "); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("delete without a reason: %v, want ErrInvalidStaffRequest", err)
	}
	if err := opsFor(service).StaffDeleteEntry(ctx, admin, entry.ID, "offensive"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetEntry(ctx, entry.ID, owner); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("deleted entry read = %v, want ErrNotFound", err)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM entry_search_jobs WHERE entry_id = $1 AND action = 'delete'", entry.ID); n != 1 {
		t.Fatalf("search removals queued = %d, want 1", n)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM entry_reports WHERE resolved_at IS NULL"); n != 0 {
		t.Fatalf("open reports after the delete = %d, want 0", n)
	}
	if err := opsFor(service).StaffDeleteEntry(ctx, admin, entry.ID, "again"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("deleting twice: %v, want ErrNotFound", err)
	}
}

func TestBanEntryReporter(t *testing.T) {
	service, pool, owner, reporter := newStaffFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	address := ipHash("198.51.100.9")
	if err := service.ReportEntry(ctx, entry.ID, catalog.Authorization{UserID: reporter}, nil, catalog.EntryReport{Reason: "spam"}); err != nil {
		t.Fatal(err)
	}
	if err := service.ReportEntry(ctx, entry.ID, catalog.Authorization{}, address, catalog.EntryReport{Reason: "spam"}); err != nil {
		t.Fatal(err)
	}
	groups, err := opsFor(service).ListEntryReports(ctx, colleague)
	if err != nil || len(groups) != 1 {
		t.Fatalf("queue = %+v, %v", groups, err)
	}
	signedIn, anonymous := groups[0].Reports[0], groups[0].Reports[1]

	if err := opsFor(service).BanEntryReporter(ctx, colleague, signedIn.ID, staffops.ReporterBan{ReasonCode: "spam", Days: 0}); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("0-day ban: %v, want ErrInvalidStaffRequest", err)
	}
	if err := opsFor(service).BanEntryReporter(ctx, colleague, signedIn.ID, staffops.ReporterBan{ReasonCode: "spam", Days: 7}); err != nil {
		t.Fatal(err)
	}
	// The account may no longer report; an anonymous reporter loses the
	// address for hours.
	if err := service.ReportEntry(ctx, entry.ID, catalog.Authorization{UserID: reporter}, nil, catalog.EntryReport{Reason: "spam"}); !isBan(err, catalog.BanReport) {
		t.Fatalf("report after the ban: %v, want a report ban", err)
	}
	if err := opsFor(service).BanEntryReporter(ctx, colleague, anonymous.ID, staffops.ReporterBan{ReasonCode: "abuse"}); err != nil {
		t.Fatal(err)
	}
	expectIPBan(t, pool, address, "report", 5*time.Hour, 6*time.Hour+time.Minute)
	if groups, _ := opsFor(service).ListEntryReports(ctx, admin); len(groups) != 0 {
		t.Fatalf("queue after banning both reporters = %+v, want empty", groups)
	}
	if err := opsFor(service).BanEntryReporter(ctx, colleague, anonymous.ID, staffops.ReporterBan{ReasonCode: "abuse"}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("banning a resolved report's reporter: %v, want ErrNotFound", err)
	}
}

func TestCommentReportQueueAndStaffRemoval(t *testing.T) {
	service, pool, owner, reporter := newStaffFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	comment, err := service.CreateComment(ctx, entry.ID, catalog.Authorization{UserID: owner}, nil, catalog.NewComment{Body: "rude words"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ReportComment(ctx, entry.ID, comment.Number, catalog.Authorization{UserID: reporter}, nil,
		catalog.CommentReport{Reason: "abuse", Note: "rude"}); err != nil {
		t.Fatal(err)
	}
	groups, err := opsFor(service).ListCommentReports(ctx, colleague)
	if err != nil || len(groups) != 1 {
		t.Fatalf("queue = %+v, %v", groups, err)
	}
	group := groups[0]
	if group.Comment.Body != "rude words" || group.Comment.Author != "social-test-alice" || group.Comment.EntryID != entry.ID ||
		group.Reports[0].BodySnapshot != "rude words" || group.Reports[0].Reporter.Username != "social-test-bob" {
		t.Fatalf("queue = %+v", group)
	}

	if err := opsFor(service).StaffRemoveComment(ctx, colleague, group.Comment.ID, "abuse"); err != nil {
		t.Fatal(err)
	}
	page := listComments(t, service, entry.ID, catalog.Authorization{})
	if len(page.Comments) != 1 || page.Comments[0].RemovedBy != "admin" || page.Comments[0].Body != "" {
		t.Fatalf("thread after the removal = %+v, want a staff tombstone", page.Comments)
	}
	if groups, _ := opsFor(service).ListCommentReports(ctx, admin); len(groups) != 0 {
		t.Fatalf("queue after the removal = %+v, want empty", groups)
	}
	if err := opsFor(service).StaffRemoveComment(ctx, colleague, group.Comment.ID, ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("removing twice: %v, want ErrNotFound", err)
	}
	if err := opsFor(service).ResolveCommentReports(ctx, colleague, group.Comment.ID, ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("resolving without open reports: %v, want ErrNotFound", err)
	}
	// The audit log keeps the comment's place, never its text.
	if n := countWhere(t, pool, "SELECT count(*) FROM admin_actions WHERE action = 'comment.remove' AND before::text NOT LIKE '%rude%'"); n != 1 {
		t.Fatalf("comment.remove audit rows without the body = %d, want 1", n)
	}
}

func TestStaffBans(t *testing.T) {
	service, pool, owner, banned := newStaffFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	inDays := func(days int) *time.Time {
		at := time.Now().Add(time.Duration(days) * 24 * time.Hour)
		return &at
	}
	ban := func(by audit.Staff, actions []string, expires *time.Time) (staffops.StaffBan, error) {
		return opsFor(service).CreateBan(ctx, by, staffops.NewBan{
			Username: "SOCIAL-test-bob", Actions: actions, ReasonCode: "abuse", Note: "note",
			ExpiresAt: expires, Permanent: expires == nil,
		})
	}

	for _, c := range []struct {
		name    string
		actions []string
		expires *time.Time
		want    error
	}{
		{"in the past", []string{"comment"}, inDays(-1), audit.ErrInvalidRequest},
		{"no actions", nil, inDays(1), audit.ErrInvalidRequest},
		{"unknown action", []string{"vote"}, inDays(1), audit.ErrInvalidRequest},
	} {
		if _, err := ban(colleague, c.actions, c.expires); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	if _, err := opsFor(service).CreateBan(ctx, admin, staffops.NewBan{
		Username: "nobody", Actions: []string{"comment"}, ReasonCode: "spam", Permanent: true,
	}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("banning an unknown user: %v, want ErrNotFound", err)
	}
	if _, err := opsFor(service).CreateBan(ctx, admin, staffops.NewBan{
		Username: "social-test-bob", Actions: []string{"comment"}, ReasonCode: "spam",
	}); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("a ban without an end or permanent: %v, want ErrInvalidStaffRequest", err)
	}

	commentBan, err := ban(colleague, []string{"report", "comment", "comment"}, inDays(3))
	if err != nil {
		t.Fatal(err)
	}
	if commentBan.Username != "social-test-bob" || len(commentBan.Actions) != 2 || commentBan.Actions[0] != "comment" ||
		!commentBan.Active || commentBan.BannedBy != "bob-staff" {
		t.Fatalf("ban = %+v", commentBan)
	}
	if _, err := service.CreateComment(ctx, entry.ID, catalog.Authorization{UserID: banned}, nil, catalog.NewComment{Body: "hi"}); !isBan(err, catalog.BanComment) {
		t.Fatalf("comment under the ban: %v, want a comment ban", err)
	}
	uploadBan, err := ban(admin, []string{"upload"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := opsFor(service).ChangeBanEnd(ctx, colleague, uploadBan.ID, staffops.BanEnd{ExpiresAt: inDays(-1)}); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("moving a ban's end into the past: %v, want ErrInvalidStaffRequest", err)
	}
	if err := opsFor(service).ChangeBanEnd(ctx, colleague, commentBan.ID, staffops.BanEnd{ExpiresAt: inDays(10), Reason: "again"}); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).RevokeBan(ctx, colleague, commentBan.ID, "appeal"); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).RevokeBan(ctx, admin, commentBan.ID, ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("revoking an ended ban: %v, want ErrNotFound", err)
	}
	if _, err := service.CreateComment(ctx, entry.ID, catalog.Authorization{UserID: banned}, nil, catalog.NewComment{Body: "back"}); err != nil {
		t.Fatalf("comment after the revoke: %v", err)
	}

	active, err := opsFor(service).ListBans(ctx, "social-test-BOB", true)
	if err != nil || len(active) != 1 || active[0].ID != uploadBan.ID || active[0].ExpiresAt != nil {
		t.Fatalf("active bans = %+v, %v; want the permanent upload ban", active, err)
	}
	ended, err := opsFor(service).ListBans(ctx, "", false)
	if err != nil || len(ended) != 1 || ended[0].ID != commentBan.ID || ended[0].RevokedBy != "bob-staff" || ended[0].Active {
		t.Fatalf("ended bans = %+v, %v; want the revoked comment ban", ended, err)
	}
	if got := auditActions(t, pool); len(got) != 4 {
		t.Fatalf("audit = %v, want two creates, a change and a revoke", got)
	}
}

func TestLiftIPBan(t *testing.T) {
	service, pool, _, _ := newStaffFixtures(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO ip_bans (ip_hash, action, reason_code, expires_at)
		VALUES ($1, 'comment', 'spam', now() + interval '1 hour'), ($1, 'report', 'spam', now() + interval '1 hour'),
		       ($2, 'upload', 'spam', now() - interval '1 hour')`, ipHash("a"), ipHash("b")); err != nil {
		t.Fatal(err)
	}
	bans, err := opsFor(service).ListIPBans(ctx)
	if err != nil || len(bans) != 2 || len(bans[0].Hash) != 64 {
		t.Fatalf("active address bans = %+v, %v; want the two active ones", bans, err)
	}
	hash := bans[0].Hash
	if err := opsFor(service).LiftIPBan(ctx, colleague, hash, "report", ""); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).LiftIPBan(ctx, colleague, hash, "report", ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("lifting twice: %v, want ErrNotFound", err)
	}
	if err := opsFor(service).LiftIPBan(ctx, colleague, "zz", "", ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("lifting a malformed hash: %v, want ErrNotFound", err)
	}
	if err := opsFor(service).LiftIPBan(ctx, admin, hash, "", ""); err != nil {
		t.Fatal(err)
	}
	if count, err := opsFor(service).CountExpiredIPBans(ctx); err != nil || count != 1 {
		t.Fatalf("expired address bans = %d, %v; want 1", count, err)
	}
	if pruned, err := service.PruneBanData(ctx); err != nil || pruned != 1 || ipBanCount(t, pool) != 0 {
		t.Fatalf("prune = %d, %v; want the expired ban gone and none left", pruned, err)
	}
}

func TestHardDeleteDeletedEntries(t *testing.T) {
	service, pool, owner, _ := newStaffFixtures(t)
	ctx := context.Background()
	kept := createOwnedEntry(t, service, owner, "public")
	gone := createOwnedEntry(t, service, owner, "public")
	if _, err := service.CreateComment(ctx, gone.ID, catalog.Authorization{UserID: owner}, nil, catalog.NewComment{Body: "hi"}); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteEntry(ctx, gone.ID, catalog.Authorization{UserID: owner}); err != nil {
		t.Fatal(err)
	}
	// Its search removal is still pending: the row must stay until it ran.
	if count, err := opsFor(service).CountSweepableDeletedEntries(ctx); err != nil || count != 0 {
		t.Fatalf("sweepable with a pending removal = %d, %v; want 0", count, err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM entry_search_jobs"); err != nil {
		t.Fatal(err)
	}
	if count, err := opsFor(service).CountSweepableDeletedEntries(ctx); err != nil || count != 1 {
		t.Fatalf("sweepable = %d, %v; want 1", count, err)
	}
	if removed, err := opsFor(service).HardDeleteDeletedEntries(ctx); err != nil || removed != 1 {
		t.Fatalf("hard delete = %d, %v; want 1", removed, err)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM entries WHERE id = $1", gone.ID); n != 0 {
		t.Fatal("the deleted entry's row is still there")
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM comments"); n != 0 {
		t.Fatalf("comments left = %d, want 0", n)
	}
	if _, err := service.GetEntry(ctx, kept.ID, ""); err != nil {
		t.Fatalf("live entry after the sweep: %v", err)
	}
}
