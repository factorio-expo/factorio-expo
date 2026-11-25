package catalog_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/catalog"
)

func entryReportCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM entry_reports").Scan(&count); err != nil {
		t.Fatalf("count entry reports: %v", err)
	}
	return count
}

func TestReportEntry(t *testing.T) {
	service, pool, owner, reporter := newCommentFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	signedIn := catalog.Authorization{UserID: reporter}
	home, cafe := ipHash("203.0.113.7"), ipHash("198.51.100.9")

	for _, c := range []struct {
		name   string
		auth   catalog.Authorization
		report catalog.EntryReport
		want   error
	}{
		{"own entry", catalog.Authorization{UserID: owner}, catalog.EntryReport{Reason: "spam"}, catalog.ErrInvalidEntryReport},
		{"unknown reason", signedIn, catalog.EntryReport{Reason: "ugly"}, catalog.ErrInvalidEntryReport},
		{"stolen without a note", signedIn, catalog.EntryReport{Reason: "stolen", Note: "  "}, catalog.ErrInvalidEntryReport},
		{"missing entry", signedIn, catalog.EntryReport{Reason: "spam"}, catalog.ErrNotFound},
	} {
		entryID := entry.ID
		if c.want == catalog.ErrNotFound {
			entryID = "AAAAAAAAAAAA"
		}
		if err := service.ReportEntry(ctx, entryID, c.auth, home, c.report); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v, want %v", c.name, err, c.want)
		}
	}

	// One open report per reporter: an account, or an address when anonymous.
	stolen := catalog.EntryReport{Reason: "stolen", Note: "  mine: https://example.test/original \r\n "}
	for range 2 {
		if err := service.ReportEntry(ctx, entry.ID, signedIn, home, stolen); err != nil {
			t.Fatalf("signed-in report: %v", err)
		}
		if err := service.ReportEntry(ctx, entry.ID, catalog.Authorization{}, home, catalog.EntryReport{Reason: "offensive"}); err != nil {
			t.Fatalf("anonymous report: %v", err)
		}
	}
	if err := service.ReportEntry(ctx, entry.ID, catalog.Authorization{}, cafe, catalog.EntryReport{Reason: "spam"}); err != nil {
		t.Fatalf("anonymous report from another address: %v", err)
	}
	if n := entryReportCount(t, pool); n != 3 {
		t.Fatalf("entry reports = %d, want 3", n)
	}
	var note, title string
	var revision int32
	var hashed bool
	if err := pool.QueryRow(ctx, `
		SELECT note, title_snapshot, revision_number, reporter_ip_hash IS NOT NULL
		FROM entry_reports WHERE reporter_user_id = $1`, reporter).Scan(&note, &title, &revision, &hashed); err != nil {
		t.Fatalf("read report: %v", err)
	}
	if note != "mine: https://example.test/original" || title != entry.Title || revision != 1 || hashed {
		t.Fatalf("report = %q %q rev %d hashed %v; want the trimmed note, the title, rev 1, no address", note, title, revision, hashed)
	}
	if n := ipBanCount(t, pool); n != 0 {
		t.Fatalf("address bans after reports = %d, want 0", n)
	}

	// A report ban covers entry reports, and its address ban then blocks
	// anonymous reports from there.
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_bans (id, user_id, actions, reason_code, banned_by)
		VALUES ('banReport001', $1, ARRAY['report'], 'spam', 'admin@example.test')`, reporter); err != nil {
		t.Fatalf("insert report ban: %v", err)
	}
	if err := service.ReportEntry(ctx, entry.ID, signedIn, cafe, catalog.EntryReport{Reason: "spam"}); !isBan(err, catalog.BanReport) {
		t.Fatalf("banned report: %v, want a report ban", err)
	}
	other := createOwnedEntry(t, service, owner, "public")
	if err := service.ReportEntry(ctx, other.ID, catalog.Authorization{}, cafe, catalog.EntryReport{Reason: "spam"}); !isBan(err, catalog.BanReport) {
		t.Fatalf("anonymous report from a banned address: %v, want a report ban", err)
	}
}

func TestReportEntryVisibility(t *testing.T) {
	service, pool, owner, reporter := newCommentFixtures(t)
	ctx := context.Background()
	as := catalog.Authorization{UserID: reporter}
	report := catalog.EntryReport{Reason: "offensive"}

	if err := service.ReportEntry(ctx, createOwnedEntry(t, service, owner, "unlisted").ID, as, nil, report); err != nil {
		t.Fatalf("unlisted entry: %v", err)
	}
	if err := service.ReportEntry(ctx, createOwnedEntry(t, service, owner, "private").ID, as, nil, report); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("someone else's private entry: %v, want ErrNotFound", err)
	}

	// The anonymous uploader (edit token) cannot report their own entry.
	anonymous, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatalf("create anonymous entry: %v", err)
	}
	if err := service.ReportEntry(ctx, anonymous.ID, catalog.Authorization{EditToken: anonymous.EditToken}, nil, report); !errors.Is(err, catalog.ErrInvalidEntryReport) {
		t.Fatalf("edit-token holder: %v, want ErrInvalidEntryReport", err)
	}

	// The report names the revision the reporter was looking at.
	first := anonymous.Revision.ID
	if _, err := service.CreateRevision(ctx, anonymous.ID, catalog.Authorization{EditToken: anonymous.EditToken},
		catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")}); err != nil {
		t.Fatalf("create revision: %v", err)
	}
	if err := service.ReportEntry(ctx, anonymous.ID, as, nil, catalog.EntryReport{Reason: "spam", RevisionID: first}); err != nil {
		t.Fatalf("report an older revision: %v", err)
	}
	var revision int32
	if err := pool.QueryRow(ctx, "SELECT revision_number FROM entry_reports WHERE entry_id = $1", anonymous.ID).Scan(&revision); err != nil {
		t.Fatalf("read report: %v", err)
	}
	if revision != 1 {
		t.Fatalf("revision_number = %d, want 1", revision)
	}
}
