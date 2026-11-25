package staffops_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"factorio-expo/apps/expo-api/internal/accountdeletion"
	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/staffops"
)

func staffEntryIDs(page staffops.StaffEntryPage) []string {
	ids := make([]string, 0, len(page.Entries))
	for _, entry := range page.Entries {
		ids = append(ids, entry.ID)
	}
	return ids
}

func TestListStaffEntries(t *testing.T) {
	service, pool, owner, _ := newStaffFixtures(t)
	ctx := context.Background()
	public := createOwnedEntry(t, service, owner, "public")
	private := createOwnedEntry(t, service, owner, "private")
	anonymous := uploadFrom(t, service, "bp-starter-smelter.txt", addressA)
	deleted := createOwnedEntry(t, service, owner, "public")
	if err := opsFor(service).StaffDeleteEntry(ctx, admin, deleted.ID, "spam"); err != nil {
		t.Fatal(err)
	}
	failLeaf(t, pool, entryLeaves(t, pool, private.ID)[0], "render_timeout")

	list := func(query staffops.StaffEntryQuery) []string {
		t.Helper()
		page, err := opsFor(service).ListStaffEntries(ctx, admin, query)
		if err != nil {
			t.Fatal(err)
		}
		return staffEntryIDs(page)
	}
	same := func(got []string, want ...string) bool {
		return strings.Join(got, ",") == strings.Join(want, ",")
	}

	page, err := opsFor(service).ListStaffEntries(ctx, admin, staffops.StaffEntryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if !same(staffEntryIDs(page), deleted.ID, anonymous.ID, private.ID, public.ID) {
		t.Fatalf("all = %v, want newest first", staffEntryIDs(page))
	}
	if page.Counts != (staffops.StaffEntryCounts{All: 4, Live: 3, Deleted: 1}) {
		t.Fatalf("counts = %+v", page.Counts)
	}
	gone := page.Entries[0]
	if !gone.Gone || gone.DeletedAt == nil || gone.Expired || gone.Owner != "social-test-alice" || gone.Leaves != 1 {
		t.Fatalf("deleted row = %+v", gone)
	}
	if got := list(staffops.StaffEntryQuery{State: "deleted"}); !same(got, deleted.ID) {
		t.Fatalf("deleted = %v", got)
	}
	if got := list(staffops.StaffEntryQuery{State: "live", Author: "anonymous"}); !same(got, anonymous.ID) {
		t.Fatalf("live anonymous = %v", got)
	}
	if got := list(staffops.StaffEntryQuery{Failed: true}); !same(got, private.ID) {
		t.Fatalf("failed = %v", got)
	}
	if got := list(staffops.StaffEntryQuery{Visibility: "private", Search: "SOCIAL-test-alice"}); !same(got, private.ID) {
		t.Fatalf("private by owner = %v", got)
	}
	if got := list(staffops.StaffEntryQuery{Search: anonymous.ID}); !same(got, anonymous.ID) {
		t.Fatalf("by id = %v", got)
	}
	// The fixture needs the base game and nothing else.
	if got := list(staffops.StaffEntryQuery{Mods: "modded"}); len(got) != 0 {
		t.Fatalf("modded = %v, want none", got)
	}
	if got := list(staffops.StaffEntryQuery{Mods: "space_age"}); len(got) != 0 {
		t.Fatalf("space age = %v, want none", got)
	}
	if got := list(staffops.StaffEntryQuery{Mods: "vanilla", Author: "anonymous"}); !same(got, anonymous.ID) {
		t.Fatalf("vanilla anonymous = %v", got)
	}
	today := time.Now().UTC().Format(time.DateOnly)
	if got := list(staffops.StaffEntryQuery{CreatedFrom: today, CreatedTo: today, MinLeaves: 2}); len(got) != 0 {
		t.Fatalf("two leaves or more = %v, want none", got)
	}
	if got := list(staffops.StaffEntryQuery{CreatedTo: "2000-01-01"}); len(got) != 0 {
		t.Fatalf("before 2000 = %v", got)
	}
	for _, bad := range []staffops.StaffEntryQuery{
		{State: "hidden"}, {Visibility: "secret"}, {Author: "bot"}, {Mods: "some"},
		{CreatedFrom: "yesterday"}, {MinLeaves: -1}, {Cursor: "nope"},
	} {
		if _, err := opsFor(service).ListStaffEntries(ctx, admin, bad); !errors.Is(err, audit.ErrInvalidRequest) {
			t.Fatalf("query %+v: %v, want ErrInvalidStaffRequest", bad, err)
		}
	}
}

func TestGetStaffEntry(t *testing.T) {
	service, pool, owner, reporter := newStaffFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	if err := service.ReportEntry(ctx, entry.ID, catalog.Authorization{UserID: reporter}, nil,
		catalog.EntryReport{Reason: "stolen", Note: "mine"}); err != nil {
		t.Fatal(err)
	}
	leaf := entryLeaves(t, pool, entry.ID)[0]
	failLeaf(t, pool, leaf, "render_timeout")

	detail, err := opsFor(service).GetStaffEntry(ctx, admin, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Owner != "social-test-alice" || detail.OwnerID != owner || !detail.Listed || detail.CurrentRevision != 1 ||
		detail.Leaves != 1 || detail.FailedLeaves != 1 || detail.OpenReports != 1 || detail.RestoreBlocker != "" {
		t.Fatalf("detail = %+v", detail.StaffEntry)
	}
	if len(detail.Revisions) != 1 || !detail.Revisions[0].Current || detail.Revisions[0].StringBytes == 0 {
		t.Fatalf("revisions = %+v", detail.Revisions)
	}
	if len(detail.Blueprints) != 1 || detail.Blueprints[0].ID != leaf ||
		detail.Blueprints[0].PreviewFailureCode != "render_timeout" || detail.Blueprints[0].PreviewFailureDetail != "detail" {
		t.Fatalf("blueprints = %+v", detail.Blueprints)
	}
	if len(detail.Reports) != 1 || detail.Reports[0].Reason != "stolen" || detail.Reports[0].Reporter.Username != "social-test-bob" {
		t.Fatalf("reports = %+v", detail.Reports)
	}
	if detail.Search.Action != "index" {
		t.Fatalf("search = %+v, want the upload's index job", detail.Search)
	}

	raw, err := opsFor(service).StaffRevisionString(ctx, admin, entry.ID, detail.Revisions[0].ID)
	if err != nil || raw != strings.TrimSpace(readFixture(t, "bp-starter-smelter.txt")) && raw != readFixture(t, "bp-starter-smelter.txt") {
		t.Fatalf("revision string = %d bytes, %v", len(raw), err)
	}
	if _, err := opsFor(service).StaffRevisionString(ctx, admin, createOwnedEntry(t, service, owner, "public").ID,
		detail.Revisions[0].ID); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("revision of another entry: %v, want ErrNotFound", err)
	}
	if _, err := opsFor(service).GetStaffEntry(ctx, admin, "AAAAAAAAAAAA"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("missing entry: %v", err)
	}
}

func TestStaffRestoreEntry(t *testing.T) {
	service, pool, owner, _ := newStaffFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	if err := opsFor(service).StaffRestoreEntry(ctx, admin, entry.ID, ""); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("restore of a live entry: %v, want ErrInvalidStaffRequest", err)
	}
	if err := service.DeleteEntry(ctx, entry.ID, catalog.Authorization{UserID: owner}); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).StaffRestoreEntry(ctx, admin, entry.ID, "deleted by mistake"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetEntry(ctx, entry.ID, ""); err != nil {
		t.Fatalf("restored entry read: %v", err)
	}
	// The queued removal becomes an index job.
	assertSearchJob(t, pool, entry.ID, "index")

	// An expired entry comes back without its passed expiry.
	expired := createOwnedEntry(t, service, owner, "public")
	if _, err := pool.Exec(ctx, "UPDATE entries SET expires_at = now() - interval '1 hour', deleted_at = now() WHERE id = $1",
		expired.ID); err != nil {
		t.Fatal(err)
	}
	page, _ := opsFor(service).ListStaffEntries(ctx, admin, staffops.StaffEntryQuery{Search: expired.ID})
	if len(page.Entries) != 1 || !page.Entries[0].Expired {
		t.Fatalf("expired row = %+v", page.Entries)
	}
	if err := opsFor(service).StaffRestoreEntry(ctx, admin, expired.ID, ""); err != nil {
		t.Fatal(err)
	}
	if n := countWhere(t, pool, "SELECT count(*) FROM entries WHERE id = $1 AND expires_at IS NULL AND deleted_at IS NULL", expired.ID); n != 1 {
		t.Fatal("the restored entry keeps its passed expiry")
	}

	// A deleted account's entries stay deleted.
	if err := opsFor(service).StaffDeleteUser(ctx, admin, owner, "left"); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).StaffRestoreEntry(ctx, admin, entry.ID, ""); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("restore with the owner deleted: %v, want ErrInvalidStaffRequest", err)
	}
	detail, err := opsFor(service).GetStaffEntry(ctx, admin, entry.ID)
	if err != nil || detail.RestoreBlocker == "" {
		t.Fatalf("restore blocker = %q, %v", detail.RestoreBlocker, err)
	}
	got := auditActions(t, pool)
	if len(got) < 2 || got[0] != "alice-staff entry.restore "+entry.ID {
		t.Fatalf("audit = %v", got)
	}
}

func TestStaffReindexAndPurge(t *testing.T) {
	var purged []string
	service, pool := newServiceWith(t, catalog.Options{
		EntryHidden: func(id string, revisions, blueprints []string) {
			purged = append(purged, id)
			if len(revisions) != 1 || len(blueprints) != 1 {
				t.Errorf("purge of %s: %d revisions, %d blueprints", id, len(revisions), len(blueprints))
			}
		},
	})
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "TRUNCATE admin_actions"); err != nil {
		t.Fatal(err)
	}
	entry := uploadFrom(t, service, "bp-starter-smelter.txt", addressA)
	if err := opsFor(service).StaffDeleteEntry(ctx, admin, entry.ID, "spam"); err != nil {
		t.Fatal(err)
	}
	purged = nil
	if err := opsFor(service).StaffReindexEntry(ctx, admin, entry.ID, ""); err != nil {
		t.Fatal(err)
	}
	// An index job removes an entry that is not listed, so it may replace
	// the delete.
	assertSearchJob(t, pool, entry.ID, "index")
	if err := opsFor(service).StaffPurgeEntryCache(ctx, admin, entry.ID, "stale image"); err != nil {
		t.Fatal(err)
	}
	if len(purged) != 1 || purged[0] != entry.ID {
		t.Fatalf("purged = %v", purged)
	}
	if got := auditActions(t, pool); len(got) != 3 || got[2] != "alice-staff entry.purge_cache "+entry.ID {
		t.Fatalf("audit = %v", got)
	}

	plain, _ := newService(t)
	if err := opsFor(plain).StaffPurgeEntryCache(ctx, admin, entry.ID, ""); !errors.Is(err, staffops.ErrEdgePurgeOff) {
		t.Fatalf("purge without Cloudflare: %v, want ErrEdgePurgeOff", err)
	}
}

func TestDeletionQueue(t *testing.T) {
	service, _, owner, other := newStaffFixtures(t)
	ctx := context.Background()
	createOwnedEntry(t, service, owner, "public")
	if err := opsFor(service).StaffDeleteUser(ctx, admin, owner, "asked"); err != nil {
		t.Fatal(err)
	}
	queue, err := opsFor(service).ListDeletionQueue(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 || queue[0].ID != owner || queue[0].Entries != 1 || queue[0].ID == other ||
		!queue[0].PurgeAt.Equal(queue[0].DeletedAt.Add(accountdeletion.DeletedAccountHold)) {
		t.Fatalf("queue = %+v", queue)
	}
}
