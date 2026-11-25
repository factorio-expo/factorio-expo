package catalog_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/staffops"
)

func newProblemFixtures(t *testing.T) (*catalog.Service, func(query string, args ...any) int, string) {
	t.Helper()
	service, pool, _, reporter := newStaffFixtures(t)
	if _, err := pool.Exec(context.Background(), "TRUNCATE problem_reports"); err != nil {
		t.Fatal(err)
	}
	count := func(query string, args ...any) int { return countWhere(t, pool, query, args...) }
	return service, count, reporter
}

func TestReportProblem(t *testing.T) {
	service, count, reporter := newProblemFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, reporter, "public")
	report := catalog.ProblemReport{
		Body: " The preview is cut off. \r\n", Email: "me@example.org",
		Page: "/e/" + entry.ID + "?token=secret#top", EntryID: entry.ID,
		Context: map[string]string{"game_version": "2.0.72", "viewport": "1280x720", "empty": " "},
	}
	if err := service.ReportProblem(ctx, catalog.Authorization{UserID: reporter}, nil, "Firefox", report); err != nil {
		t.Fatal(err)
	}
	if err := service.ReportProblem(ctx, catalog.Authorization{}, ipHash("192.0.2.4"), strings.Repeat("u", 900),
		catalog.ProblemReport{Body: "Search is slow"}); err != nil {
		t.Fatal(err)
	}
	// The page keeps its path only (an edit token never reaches the table),
	// empty context values are dropped, a long user agent is cut.
	if n := count(`SELECT count(*) FROM problem_reports WHERE page_path = $1 AND body = 'The preview is cut off.'
		AND context = '{"game_version": "2.0.72", "viewport": "1280x720"}' AND user_agent = 'Firefox'
		AND email = 'me@example.org' AND reporter_user_id IS NOT NULL`, "/e/"+entry.ID); n != 1 {
		t.Fatal("the signed-in report was not stored as sent")
	}
	if n := count("SELECT count(*) FROM problem_reports WHERE reporter_ip_hash IS NOT NULL AND char_length(user_agent) = 500"); n != 1 {
		t.Fatal("the anonymous report keeps no address hash or an uncut user agent")
	}

	for name, bad := range map[string]catalog.ProblemReport{
		"empty":       {Body: "  "},
		"long":        {Body: strings.Repeat("x", catalog.MaxProblemReportRunes+1)},
		"named email": {Body: "x", Email: "Me <me@example.org>"},
		"bad email":   {Body: "x", Email: "not an address"},
		"page":        {Body: "x", Page: "https://elsewhere.example/e/1"},
		"entry id":    {Body: "x", EntryID: "../etc"},
		"context key": {Body: "x", Context: map[string]string{"Bad Key": "x"}},
		"context len": {Body: "x", Context: map[string]string{"k": strings.Repeat("x", 201)}},
	} {
		if err := service.ReportProblem(ctx, catalog.Authorization{UserID: reporter}, nil, "", bad); !errors.Is(err, catalog.ErrInvalidProblemReport) {
			t.Fatalf("%s: %v, want ErrInvalidProblemReport", name, err)
		}
	}

	// A report ban blocks it.
	if _, err := opsFor(service).CreateBan(ctx, admin, staffops.NewBan{
		Username: "social-test-bob", Actions: []string{"report"}, ReasonCode: "spam", Permanent: true,
	}); err != nil {
		t.Fatal(err)
	}
	var ban *catalog.BanError
	if err := service.ReportProblem(ctx, catalog.Authorization{UserID: reporter}, nil, "",
		catalog.ProblemReport{Body: "x"}); !errors.As(err, &ban) {
		t.Fatalf("banned reporter: %v, want a BanError", err)
	}
}

func TestProblemReportQueue(t *testing.T) {
	service, count, reporter := newProblemFixtures(t)
	ctx := context.Background()
	if err := service.ReportProblem(ctx, catalog.Authorization{UserID: reporter}, nil, "",
		catalog.ProblemReport{Body: "first", Email: "me@example.org"}); err != nil {
		t.Fatal(err)
	}
	if err := service.ReportProblem(ctx, catalog.Authorization{}, ipHash("192.0.2.9"), "",
		catalog.ProblemReport{Body: "second"}); err != nil {
		t.Fatal(err)
	}
	page, err := opsFor(service).ListProblemReports(ctx, admin, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Reports) != 2 || page.Counts.Open != 2 || page.Reports[0].Body != "first" ||
		page.Reports[0].Reporter.Username != "social-test-bob" || !page.Reports[1].Reporter.Anonymous {
		t.Fatalf("open queue = %+v", page)
	}
	first, second := page.Reports[0], page.Reports[1]

	if err := opsFor(service).ResolveProblemReport(ctx, admin, first.ID, "open", ""); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("bad status: %v", err)
	}
	if err := opsFor(service).ResolveProblemReport(ctx, admin, first.ID, "fixed", "in 1.2"); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).ResolveProblemReport(ctx, admin, first.ID, "fixed", ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("resolving twice: %v, want ErrNotFound", err)
	}
	if err := opsFor(service).SetProblemReportNote(ctx, admin, first.ID, "shipped in 1.2"); err != nil {
		t.Fatal(err)
	}
	// Resolving drops the contact address.
	if n := count("SELECT count(*) FROM problem_reports WHERE id = $1 AND email IS NULL AND status = 'fixed' AND staff_note = 'shipped in 1.2' AND resolved_by = 'alice-staff'", first.ID); n != 1 {
		t.Fatal("the resolved report keeps its address or lost its status")
	}

	if err := opsFor(service).BanProblemReporter(ctx, admin, second.ID, staffops.ReporterBan{ReasonCode: "spam"}); err != nil {
		t.Fatal(err)
	}
	if n := count("SELECT count(*) FROM ip_bans WHERE action = 'report'"); n != 1 {
		t.Fatalf("address bans = %d, want 1", n)
	}
	if n := count("SELECT count(*) FROM problem_reports WHERE id = $1 AND status = 'spam' AND reporter_ip_hash IS NULL", second.ID); n != 1 {
		t.Fatal("the banned reporter's report is not closed as spam")
	}
	resolved, err := opsFor(service).ListProblemReports(ctx, admin, false)
	if err != nil || len(resolved.Reports) != 2 || resolved.Counts.Open != 0 {
		t.Fatalf("resolved = %+v, %v", resolved, err)
	}
}

func TestPruneProblemReports(t *testing.T) {
	service, count, reporter := newProblemFixtures(t)
	ctx := context.Background()
	for _, body := range []string{"old resolved", "new resolved", "old open", "new open"} {
		if err := service.ReportProblem(ctx, catalog.Authorization{UserID: reporter}, nil, "",
			catalog.ProblemReport{Body: body, Email: "me@example.org"}); err != nil {
			t.Fatal(err)
		}
	}
	page, _ := opsFor(service).ListProblemReports(ctx, admin, true)
	for _, report := range page.Reports[:2] {
		if err := opsFor(service).ResolveProblemReport(ctx, admin, report.ID, "fixed", ""); err != nil {
			t.Fatal(err)
		}
	}
	if count(`WITH aged AS (
			UPDATE problem_reports SET resolved_at = now() - interval '8 days' WHERE body = 'old resolved' RETURNING 1
		), sent AS (
			UPDATE problem_reports SET created_at = now() - interval '31 days' WHERE body = 'old open' RETURNING 1
		) SELECT (SELECT count(*) FROM aged) + (SELECT count(*) FROM sent)`) != 2 {
		t.Fatal("could not age the reports")
	}
	deleted, cleared, err := service.PruneProblemReports(ctx)
	if err != nil || deleted != 1 || cleared != 1 {
		t.Fatalf("prune = %d deleted, %d cleared, %v; want 1, 1", deleted, cleared, err)
	}
	if n := count("SELECT count(*) FROM problem_reports WHERE email IS NOT NULL"); n != 1 {
		t.Fatalf("reports with an address = %d, want only the new open one", n)
	}
}
