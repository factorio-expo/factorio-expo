package catalog_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// newCommentFixtures is newSocialFixtures with accounts older than a day, so the
// new-account comment cap stays out of the way unless a test wants it.
func newCommentFixtures(t *testing.T) (*catalog.Service, *pgxpool.Pool, string, string) {
	t.Helper()
	service, pool, userA, userB := newSocialFixtures(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "UPDATE users SET created_at = now() - interval '30 days'"); err != nil {
		t.Fatalf("age users: %v", err)
	}
	return service, pool, userA, userB
}

func addUser(t *testing.T, pool *pgxpool.Pool, id, name string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, username, created_at)
		VALUES ($1, $2, now() - interval '30 days')`, id, name); err != nil {
		t.Fatalf("insert user %s: %v", name, err)
	}
}

func ipHash(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func createOwnedEntry(t *testing.T, service *catalog.Service, owner, visibility string) catalog.Entry {
	t.Helper()
	entry, err := service.CreateEntry(context.Background(), catalog.Submission{
		Exchange: readFixture(t, "bp-starter-smelter.txt"), OwnerUserID: owner, Visibility: visibility,
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	return entry
}

func listComments(t *testing.T, service *catalog.Service, entryID string, auth catalog.Authorization) catalog.CommentPage {
	t.Helper()
	page, err := service.ListComments(context.Background(), entryID, auth, catalog.CommentPageRequest{})
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	return page
}

func TestCommentsAreNumberedEditedAndRemovedAsTombstones(t *testing.T) {
	service, _, owner, commenter := newCommentFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	as := catalog.Authorization{UserID: commenter}

	first, err := service.CreateComment(ctx, entry.ID, as, nil, catalog.NewComment{Body: "  first [item=iron-ore]  "})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	second, err := service.CreateComment(ctx, entry.ID, as, nil, catalog.NewComment{Body: "see #1"})
	if err != nil {
		t.Fatalf("create second comment: %v", err)
	}
	if first.Number != 1 || second.Number != 2 || first.Body != "first [item=iron-ore]" ||
		first.Author != "social-test-bob" || first.RevisionNumber != 1 || !first.CanEdit {
		t.Fatalf("comments = %+v, %+v", first, second)
	}

	edited, err := service.EditComment(ctx, entry.ID, 1, as, nil, "first, fixed")
	if err != nil {
		t.Fatalf("edit comment: %v", err)
	}
	if edited.Body != "first, fixed" || edited.EditedAt == nil {
		t.Fatalf("edited = %+v", edited)
	}
	if _, err := service.EditComment(ctx, entry.ID, 1, catalog.Authorization{UserID: owner}, nil, "hijack"); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("editing someone else's comment: %v, want ErrForbidden", err)
	}

	if err := service.RemoveComment(ctx, entry.ID, 1, catalog.Authorization{UserID: owner}); err != nil {
		t.Fatalf("owner removes comment: %v", err)
	}
	if err := service.RemoveComment(ctx, entry.ID, 2, as); err != nil {
		t.Fatalf("author removes comment: %v", err)
	}
	// Removing again is a no-op.
	if err := service.RemoveComment(ctx, entry.ID, 2, as); err != nil {
		t.Fatalf("remove again: %v", err)
	}

	page := listComments(t, service, entry.ID, as)
	if page.Total != 2 || page.LastNumber != 2 || len(page.Comments) != 2 {
		t.Fatalf("page = %+v", page)
	}
	for i, want := range []string{"owner", "author"} {
		got := page.Comments[i]
		if got.RemovedBy != want || got.Body != "" || got.CanEdit || got.CanRemove {
			t.Fatalf("comment %d = %+v, want a tombstone removed by %s", i+1, got, want)
		}
	}
	if !page.Viewer.CanComment || page.Viewer.CanModerate {
		t.Fatalf("commenter viewer = %+v", page.Viewer)
	}
	if viewer := listComments(t, service, entry.ID, catalog.Authorization{}).Viewer; viewer.CanComment || viewer.Blocked != "signed_out" {
		t.Fatalf("anonymous viewer = %+v", viewer)
	}
	if _, err := service.EditComment(ctx, entry.ID, 2, as, nil, "back"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("editing a removed comment: %v, want ErrNotFound", err)
	}
}

func TestCommentPagesFollowNumbers(t *testing.T) {
	service, pool, owner, commenter := newCommentFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	if _, err := pool.Exec(ctx, `
		INSERT INTO comments (id, entry_id, number, author_user_id, revision_number, body)
		SELECT lpad(n::text, 12, 'c'), $1, n, $2, 1, 'comment ' || n FROM generate_series(1, 7) AS n`,
		entry.ID, commenter); err != nil {
		t.Fatalf("seed comments: %v", err)
	}
	page, err := service.ListComments(ctx, entry.ID, catalog.Authorization{}, catalog.CommentPageRequest{After: 2, Limit: 3})
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if numbers(page) != "3,4,5" || !page.HasOlder || !page.HasNewer {
		t.Fatalf("after 2 = %s older=%v newer=%v", numbers(page), page.HasOlder, page.HasNewer)
	}
	page, err = service.ListComments(ctx, entry.ID, catalog.Authorization{}, catalog.CommentPageRequest{Before: 3, Limit: 3})
	if err != nil {
		t.Fatalf("list before: %v", err)
	}
	if numbers(page) != "1,2" || page.HasOlder || !page.HasNewer {
		t.Fatalf("before 3 = %s older=%v newer=%v", numbers(page), page.HasOlder, page.HasNewer)
	}
}

func numbers(page catalog.CommentPage) string {
	out := ""
	for i, comment := range page.Comments {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprint(comment.Number)
	}
	return out
}

func TestCommentsOnlyOnPublicEntries(t *testing.T) {
	service, _, owner, commenter := newCommentFixtures(t)
	ctx := context.Background()
	as := catalog.Authorization{UserID: commenter}

	unlisted := createOwnedEntry(t, service, owner, "unlisted")
	if _, err := service.CreateComment(ctx, unlisted.ID, as, nil, catalog.NewComment{Body: "hi"}); !errors.Is(err, catalog.ErrCommentsClosed) {
		t.Fatalf("comment on unlisted entry: %v, want ErrCommentsClosed", err)
	}
	if page := listComments(t, service, unlisted.ID, as); page.Viewer.Blocked != "closed" || page.Viewer.CanComment {
		t.Fatalf("unlisted viewer = %+v", page.Viewer)
	}

	preview, err := service.CreateQuickPreview(ctx, readFixture(t, "bp-starter-smelter.txt"), "", nil)
	if err != nil {
		t.Fatalf("create quick preview: %v", err)
	}
	if _, err := service.CreateComment(ctx, preview.ID, as, nil, catalog.NewComment{Body: "hi"}); !errors.Is(err, catalog.ErrCommentsClosed) {
		t.Fatalf("comment on quick preview: %v, want ErrCommentsClosed", err)
	}

	private := createOwnedEntry(t, service, owner, "private")
	if _, err := service.CreateComment(ctx, private.ID, as, nil, catalog.NewComment{Body: "hi"}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("comment on someone's private entry: %v, want ErrNotFound", err)
	}
}

// Leaving public deletes the thread; numbers are never handed out twice.
func TestLeavingPublicDeletesComments(t *testing.T) {
	service, pool, owner, commenter := newCommentFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	as := catalog.Authorization{UserID: commenter}
	first, err := service.CreateComment(ctx, entry.ID, as, nil, catalog.NewComment{Body: "first"})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if err := service.ReportComment(ctx, entry.ID, first.Number, catalog.Authorization{UserID: owner}, nil,
		catalog.CommentReport{Reason: "spam"}); err != nil {
		t.Fatalf("report: %v", err)
	}

	ownerAuth := catalog.Authorization{UserID: owner}
	for _, visibility := range []catalog.Visibility{catalog.VisibilityUnlisted, catalog.VisibilityPublic} {
		if _, err := service.UpdateEntryMetadata(ctx, entry.ID, ownerAuth,
			catalog.EntryMetadataUpdate{Visibility: &visibility}); err != nil {
			t.Fatalf("set %s: %v", visibility, err)
		}
	}
	var comments, reports int
	if err := pool.QueryRow(ctx,
		"SELECT (SELECT count(*) FROM comments), (SELECT count(*) FROM comment_reports)").Scan(&comments, &reports); err != nil {
		t.Fatalf("count: %v", err)
	}
	if comments != 0 || reports != 0 {
		t.Fatalf("comments=%d reports=%d after going unlisted, want none", comments, reports)
	}
	next, err := service.CreateComment(ctx, entry.ID, as, nil, catalog.NewComment{Body: "after"})
	if err != nil {
		t.Fatalf("comment after going public again: %v", err)
	}
	if next.Number != 2 {
		t.Fatalf("number = %d, want 2 (never reused)", next.Number)
	}
}

func TestPublishingAnonymousEntryAsPrivateDeletesComments(t *testing.T) {
	service, pool, owner, commenter := newCommentFixtures(t)
	ctx := context.Background()
	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if _, err := service.CreateComment(ctx, entry.ID, catalog.Authorization{UserID: commenter}, nil, catalog.NewComment{Body: "hi"}); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if _, err := service.PublishEntry(ctx, entry.ID, catalog.Authorization{UserID: owner, EditToken: entry.EditToken}, nil,
		catalog.EntryPublish{Title: "Mine", Visibility: "private"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	var comments int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM comments").Scan(&comments); err != nil {
		t.Fatalf("count: %v", err)
	}
	if comments != 0 {
		t.Fatalf("comments = %d after publishing as private, want 0", comments)
	}
}

func TestEditTokenModeratesAnonymousEntry(t *testing.T) {
	service, pool, other, commenter := newCommentFixtures(t)
	ctx := context.Background()
	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if _, err := service.CreateComment(ctx, entry.ID, catalog.Authorization{UserID: commenter}, nil, catalog.NewComment{Body: "hi"}); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if err := service.RemoveComment(ctx, entry.ID, 1, catalog.Authorization{UserID: other}); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("stranger removes: %v, want ErrForbidden", err)
	}
	token := catalog.Authorization{EditToken: entry.EditToken}
	if page := listComments(t, service, entry.ID, token); !page.Viewer.CanModerate || !page.Comments[0].CanRemove {
		t.Fatalf("edit-token viewer = %+v, comment = %+v", page.Viewer, page.Comments[0])
	}
	if err := service.RemoveComment(ctx, entry.ID, 1, token); err != nil {
		t.Fatalf("edit-token holder removes: %v", err)
	}
	var by string
	if err := pool.QueryRow(ctx, "SELECT deleted_by FROM comments").Scan(&by); err != nil || by != "owner" {
		t.Fatalf("deleted_by = %q (%v), want owner", by, err)
	}
}

func TestBansBlockCommentingByAccountAndAddress(t *testing.T) {
	service, pool, owner, banned := newCommentFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	home, cafe := ipHash("203.0.113.7"), ipHash("198.51.100.9")

	// An upload ban does not stop commenting, and commenting stores nothing
	// about the address.
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_bans (id, user_id, actions, reason_code, banned_by)
		VALUES ('banUpload001', $1, ARRAY['upload'], 'spam', 'admin@example.test')`, banned); err != nil {
		t.Fatalf("insert upload ban: %v", err)
	}
	if _, err := service.CreateComment(ctx, entry.ID, catalog.Authorization{UserID: banned}, home, catalog.NewComment{Body: "ok"}); err != nil {
		t.Fatalf("comment under an upload ban: %v", err)
	}
	if n := ipBanCount(t, pool); n != 0 {
		t.Fatalf("address bans after a comment = %d, want 0", n)
	}
	// Trying to upload bans the address for hours, which then blocks an
	// anonymous upload from it.
	upload := catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), OwnerUserID: banned, IPHash: home}
	if _, err := service.CreateEntry(ctx, upload); !isBan(err, catalog.BanUpload) {
		t.Fatalf("banned upload error = %v, want an upload ban", err)
	}
	expectIPBan(t, pool, home, "upload", 5*time.Hour, 6*time.Hour+time.Minute)
	if _, err := service.CreateQuickPreview(ctx, readFixture(t, "bp-starter-smelter.txt"), "", home); !isBan(err, catalog.BanUpload) {
		t.Fatalf("anonymous quick preview from a banned address error = %v, want an upload ban", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO user_bans (id, user_id, actions, reason_code, banned_by, expires_at)
		VALUES ('banComment01', $1, ARRAY['comment', 'report'], 'abuse', 'admin@example.test', now() + interval '3 days')`,
		banned); err != nil {
		t.Fatalf("insert comment ban: %v", err)
	}
	_, err := service.CreateComment(ctx, entry.ID, catalog.Authorization{UserID: banned}, cafe, catalog.NewComment{Body: "again"})
	if banErr, ok := errors.AsType[*catalog.BanError](err); !ok || banErr.Ban.ReasonCode != "abuse" || banErr.Ban.ExpiresAt == nil {
		t.Fatalf("banned account comments: %v, want a BanError", err)
	}
	// The address ban covers every action of the user ban, for hours only.
	expectIPBan(t, pool, cafe, "comment", 5*time.Hour, 6*time.Hour+time.Minute)
	expectIPBan(t, pool, cafe, "report", 5*time.Hour, 6*time.Hour+time.Minute)
	if page := listComments(t, service, entry.ID, catalog.Authorization{UserID: banned}); page.Viewer.Blocked != "banned" {
		t.Fatalf("banned viewer = %+v", page.Viewer)
	}
	if _, err := service.EditComment(ctx, entry.ID, 1, catalog.Authorization{UserID: banned}, nil, "edit"); err == nil {
		t.Fatal("a banned author edited their comment")
	}
	if err := service.RemoveComment(ctx, entry.ID, 1, catalog.Authorization{UserID: banned}); err != nil {
		t.Fatalf("a banned author removes their comment: %v", err)
	}

	// A fresh account on that address is blocked too (reading the thread
	// never looks at the address)...
	addUser(t, pool, "freshAccount", "fresh-account")
	fresh := catalog.Authorization{UserID: "freshAccount"}
	if _, err := service.CreateComment(ctx, entry.ID, fresh, cafe, catalog.NewComment{Body: "sock"}); err == nil {
		t.Fatal("a fresh account on a banned address commented")
	}
	if page := listComments(t, service, entry.ID, fresh); !page.Viewer.CanComment {
		t.Fatalf("fresh viewer = %+v, want can_comment", page.Viewer)
	}
	// ...even once the user ban is revoked: the address ban is separate and
	// runs out on its own.
	if _, err := pool.Exec(ctx, "UPDATE user_bans SET revoked_at = now(), revoked_by = 'admin'"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := service.CreateComment(ctx, entry.ID, fresh, cafe, catalog.NewComment{Body: "sock"}); err == nil {
		t.Fatal("a revoked user ban lifted its address ban")
	}
	if _, err := pool.Exec(ctx, "UPDATE ip_bans SET expires_at = now() - interval '1 second'"); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if _, err := service.CreateComment(ctx, entry.ID, fresh, cafe, catalog.NewComment{Body: "free"}); err != nil {
		t.Fatalf("comment after the address ban ran out: %v", err)
	}
	if _, err := service.PruneBanData(ctx); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n := ipBanCount(t, pool); n != 0 {
		t.Fatalf("address bans after prune = %d, want 0", n)
	}
}

// An address ban never outlasts the user ban that started it.
func TestIPBanEndsWithShortUserBan(t *testing.T) {
	service, pool, _, banned := newCommentFixtures(t)
	ctx := context.Background()
	address := ipHash("192.0.2.44")
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_bans (id, user_id, actions, reason_code, banned_by, expires_at)
		VALUES ('shortBan0001', $1, ARRAY['upload'], 'other', 'admin@example.test', now() + interval '10 minutes')`,
		banned); err != nil {
		t.Fatalf("insert ban: %v", err)
	}
	upload := catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), OwnerUserID: banned, IPHash: address}
	if _, err := service.CreateEntry(ctx, upload); !isBan(err, catalog.BanUpload) {
		t.Fatalf("banned upload error = %v, want an upload ban", err)
	}
	expectIPBan(t, pool, address, "upload", 9*time.Minute, 11*time.Minute)
}

func ipBanCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM ip_bans").Scan(&n); err != nil {
		t.Fatalf("count address bans: %v", err)
	}
	return n
}

// expectIPBan checks an address ban for action ends between from and to from now.
func expectIPBan(t *testing.T, pool *pgxpool.Pool, address []byte, action string, from, to time.Duration) {
	t.Helper()
	var left time.Duration
	var seconds float64
	if err := pool.QueryRow(context.Background(),
		"SELECT extract(epoch FROM expires_at - now())::float8 FROM ip_bans WHERE ip_hash = $1 AND action = $2",
		address, action).Scan(&seconds); err != nil {
		t.Fatalf("read %s address ban: %v", action, err)
	}
	left = time.Duration(seconds * float64(time.Second))
	if left < from || left > to {
		t.Fatalf("%s address ban ends in %s, want %s-%s", action, left, from, to)
	}
}

func TestCommentCaps(t *testing.T) {
	service, pool, owner, commenter := newCommentFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	as := catalog.Authorization{UserID: commenter}

	if _, err := service.CreateComment(ctx, entry.ID, as, nil, catalog.NewComment{Body: "same"}); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if _, err := service.CreateComment(ctx, entry.ID, as, nil, catalog.NewComment{Body: "same"}); !errors.Is(err, catalog.ErrDuplicateComment) {
		t.Fatalf("duplicate: %v, want ErrDuplicateComment", err)
	}

	addUser(t, pool, "newbieUser01", "newbie")
	if _, err := pool.Exec(ctx, "UPDATE users SET created_at = now() WHERE id = 'newbieUser01'"); err != nil {
		t.Fatalf("make account new: %v", err)
	}
	newbie := catalog.Authorization{UserID: "newbieUser01"}
	for i := range 5 {
		if _, err := service.CreateComment(ctx, entry.ID, newbie, nil, catalog.NewComment{Body: fmt.Sprintf("comment %d", i)}); err != nil {
			t.Fatalf("comment %d: %v", i, err)
		}
	}
	if _, err := service.CreateComment(ctx, entry.ID, newbie, nil, catalog.NewComment{Body: "one more"}); !errors.Is(err, catalog.ErrCommentLimit) {
		t.Fatalf("sixth comment of a new account: %v, want ErrCommentLimit", err)
	}

	for _, body := range []string{"   ", string(make([]rune, catalog.MaxCommentRunes+1))} {
		if _, err := service.CreateComment(ctx, entry.ID, as, nil, catalog.NewComment{Body: body}); !errors.Is(err, catalog.ErrInvalidComment) {
			t.Fatalf("invalid body: %v, want ErrInvalidComment", err)
		}
	}
}

func TestReportComment(t *testing.T) {
	service, pool, owner, commenter := newCommentFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	if _, err := service.CreateComment(ctx, entry.ID, catalog.Authorization{UserID: commenter}, nil, catalog.NewComment{Body: "rude"}); err != nil {
		t.Fatalf("comment: %v", err)
	}
	report := catalog.CommentReport{Reason: "abuse", Note: "  insults  "}
	if err := service.ReportComment(ctx, entry.ID, 1, catalog.Authorization{UserID: commenter}, nil, report); !errors.Is(err, catalog.ErrInvalidReport) {
		t.Fatalf("reporting own comment: %v, want ErrInvalidReport", err)
	}
	if err := service.ReportComment(ctx, entry.ID, 1, catalog.Authorization{UserID: owner}, nil,
		catalog.CommentReport{Reason: "nope"}); !errors.Is(err, catalog.ErrInvalidReport) {
		t.Fatalf("unknown reason: %v, want ErrInvalidReport", err)
	}
	for range 2 {
		if err := service.ReportComment(ctx, entry.ID, 1, catalog.Authorization{UserID: owner}, ipHash("198.51.100.1"), report); err != nil {
			t.Fatalf("report: %v", err)
		}
	}
	if _, err := service.EditComment(ctx, entry.ID, 1, catalog.Authorization{UserID: commenter}, nil, "nice"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	var count int
	var snapshot, note string
	if err := pool.QueryRow(ctx, "SELECT count(*), min(body_snapshot), min(note) FROM comment_reports").Scan(&count, &snapshot, &note); err != nil {
		t.Fatalf("read reports: %v", err)
	}
	if count != 1 || snapshot != "rude" || note != "insults" {
		t.Fatalf("reports = %d %q %q, want one report of %q", count, snapshot, note, "rude")
	}
	if n := ipBanCount(t, pool); n != 0 {
		t.Fatalf("address bans after a report = %d, want 0 (nothing stored about unbanned users)", n)
	}
}

// A comment is tagged with the revision its author was looking at.
func TestCommentRefersToTheViewedRevision(t *testing.T) {
	service, _, owner, commenter := newCommentFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	first := entry.Revision.ID
	if _, err := service.CreateRevision(ctx, entry.ID, catalog.Authorization{UserID: owner},
		catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")}); err != nil {
		t.Fatalf("create revision: %v", err)
	}
	other := createOwnedEntry(t, service, owner, "public")
	as := catalog.Authorization{UserID: commenter}

	for _, c := range []struct {
		revisionID string
		want       int32
	}{{"", 2}, {first, 1}} {
		comment, err := service.CreateComment(ctx, entry.ID, as, nil,
			catalog.NewComment{Body: "on " + c.revisionID, RevisionID: c.revisionID})
		if err != nil {
			t.Fatalf("comment on %q: %v", c.revisionID, err)
		}
		if comment.RevisionNumber != c.want {
			t.Fatalf("revision_number = %d for %q, want %d", comment.RevisionNumber, c.revisionID, c.want)
		}
	}
	if _, err := service.CreateComment(ctx, entry.ID, as, nil,
		catalog.NewComment{Body: "elsewhere", RevisionID: other.Revision.ID}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("another entry's revision: %v, want ErrNotFound", err)
	}
}

// isBan reports whether err is a ban of action.
func isBan(err error, action catalog.BanAction) bool {
	var ban *catalog.BanError
	return errors.As(err, &ban) && ban.Ban.Action == action
}
