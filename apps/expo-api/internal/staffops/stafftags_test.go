package staffops_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/staffops"
)

// The tag tables hold the seed rows other tests rely on: these tests add
// their own rows and remove them afterwards.
func cleanupTagRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	cleanup := func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, "DELETE FROM tags WHERE slug LIKE 'staff-test-%'")
		_, _ = pool.Exec(ctx, "DELETE FROM tag_groups WHERE slug LIKE 'staff-test-%'")
		_, _ = pool.Exec(ctx, "DELETE FROM reserved_usernames WHERE name LIKE 'stafftest%'")
	}
	cleanup()
	t.Cleanup(cleanup)
}

func findTag(page staffops.StaffTags, slug string) *staffops.StaffTag {
	for i := range page.Tags {
		if page.Tags[i].Slug == slug {
			return &page.Tags[i]
		}
	}
	return nil
}

func TestStaffTagEditor(t *testing.T) {
	service, pool, owner, _ := newStaffFixtures(t)
	cleanupTagRows(t, pool)
	ctx := context.Background()

	if err := opsFor(service).CreateTagGroup(ctx, admin, staffops.TagFields{Slug: "staff-test-group", DisplayName: "Test group", Position: 99}); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).CreateTagGroup(ctx, admin, staffops.TagFields{Slug: "staff-test-group", DisplayName: "Again"}); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("duplicate group slug: %v", err)
	}
	if err := opsFor(service).CreateTagGroup(ctx, admin, staffops.TagFields{Slug: "Bad Slug", DisplayName: "x"}); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("bad slug: %v", err)
	}
	page, err := opsFor(service).ListStaffTags(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	var groupID string
	for _, group := range page.Groups {
		if group.Slug == "staff-test-group" {
			groupID = group.ID
		}
	}
	if groupID == "" {
		t.Fatal("the new group is not listed")
	}
	// An empty group stays off the public list.
	public, err := service.ListTagGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range public {
		if group.ID == groupID {
			t.Fatal("an empty group is on the public list")
		}
	}

	if err := opsFor(service).CreateTag(ctx, admin, staffops.TagFields{GroupID: groupID, Slug: "staff-test-tag", DisplayName: "Test tag", Position: 1}); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).CreateTag(ctx, admin, staffops.TagFields{GroupID: "AAAAAAAAAAAA", Slug: "staff-test-orphan", DisplayName: "x"}); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("tag in an unknown group: %v", err)
	}
	page, _ = opsFor(service).ListStaffTags(ctx, admin)
	tag := findTag(page, "staff-test-tag")
	if tag == nil || tag.GroupID != groupID {
		t.Fatalf("new tag = %+v", tag)
	}
	if err := opsFor(service).DeleteTagGroup(ctx, admin, groupID); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("deleting a group with tags: %v", err)
	}

	// Rules: names must be known prototypes.
	if err := opsFor(service).SetTagRule(ctx, admin, tag.ID, staffops.StaffTagRule{Kind: "entity", Name: "not-an-entity", MinCount: 1, Points: 1}); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("unknown entity: %v", err)
	}
	two := int32(2)
	if err := opsFor(service).SetTagRule(ctx, admin, tag.ID, staffops.StaffTagRule{Kind: "entity", Name: "transport-belt", MinCount: 3, MaxCount: &two, Points: 1}); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("max below min: %v", err)
	}
	if err := opsFor(service).SetTagRule(ctx, admin, tag.ID, staffops.StaffTagRule{Kind: "entity", Name: "transport-belt", MinCount: 1, Points: 2}); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).SetTagRule(ctx, admin, tag.ID, staffops.StaffTagRule{Kind: "entity", Name: "transport-belt", MinCount: 4, Points: 3}); err != nil {
		t.Fatal(err)
	}
	page, _ = opsFor(service).ListStaffTags(ctx, admin)
	if rules := findTag(page, "staff-test-tag").Rules; len(rules) != 1 || rules[0].MinCount != 4 || rules[0].Points != 3 {
		t.Fatalf("rules = %+v, want the one rule changed", rules)
	}

	// Rename and move keep the slug; an entry with the tag is reindexed when
	// it is deleted.
	if err := opsFor(service).UpdateTag(ctx, admin, tag.ID, staffops.TagFields{GroupID: groupID, DisplayName: "Renamed", Position: 2}); err != nil {
		t.Fatal(err)
	}
	entry := createOwnedEntry(t, service, owner, "public")
	if _, err := pool.Exec(ctx, "DELETE FROM entry_search_jobs"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO entry_tags (entry_id, tag_id) VALUES ($1, $2)", entry.ID, tag.ID); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).DeleteTagRule(ctx, admin, tag.ID, "entity", "transport-belt"); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).DeleteTag(ctx, admin, tag.ID); err != nil {
		t.Fatal(err)
	}
	assertSearchJob(t, pool, entry.ID, "index")
	if n := countWhere(t, pool, "SELECT count(*) FROM entry_tags WHERE tag_id = $1", tag.ID); n != 0 {
		t.Fatalf("entry_tags left = %d", n)
	}
	if err := opsFor(service).DeleteTagGroup(ctx, admin, groupID); err != nil {
		t.Fatal(err)
	}
	got := auditActions(t, pool)
	if len(got) != 8 || got[0] != "alice-staff tag_group.create "+groupID || got[7] != "alice-staff tag_group.delete "+groupID {
		t.Fatalf("audit = %v", got)
	}
}

func TestReservedUsernameEditor(t *testing.T) {
	service, pool, _, _ := newStaffFixtures(t)
	cleanupTagRows(t, pool)
	ctx := context.Background()
	if err := opsFor(service).ReserveUsername(ctx, admin, " StaffTest_Name-1 ", "brand"); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).ReserveUsername(ctx, admin, "stafftest-name1", "trademark"); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).ReserveUsername(ctx, admin, "no spaces", ""); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("bad name: %v", err)
	}
	addUser(t, pool, "StaffTst0001", "stafftest_taken")
	if err := opsFor(service).ReserveUsername(ctx, admin, "stafftesttaken", ""); err != nil {
		t.Fatal(err)
	}
	names, err := opsFor(service).ListReservedUsernames(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]staffops.ReservedUsername{}
	for _, name := range names {
		found[name.Name] = name
	}
	if found["stafftestname1"].Reason != "trademark" || found["stafftesttaken"].TakenBy != "stafftest_taken" {
		t.Fatalf("reserved = %+v", found)
	}
	if err := opsFor(service).ReleaseUsername(ctx, admin, "StaffTest-Name1"); err != nil {
		t.Fatal(err)
	}
	if err := opsFor(service).ReleaseUsername(ctx, admin, "stafftestname1"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("releasing twice: %v", err)
	}
	got := auditActions(t, pool)
	if len(got) != 4 || got[0] != "alice-staff reserved_username.add stafftestname1" ||
		got[1] != "alice-staff reserved_username.update stafftestname1" {
		t.Fatalf("audit = %v", got)
	}
}

// A rule edit applies to the next inspection at once: no restart reloads the
// rules, the staff write does.
func TestTagRuleEditsApplyWithoutARestart(t *testing.T) {
	service, pool, _, _ := newStaffFixtures(t)
	cleanupTagRows(t, pool)
	ctx := context.Background()
	ops := opsFor(service)
	if err := ops.CreateTagGroup(ctx, admin, staffops.TagFields{Slug: "staff-test-group", DisplayName: "Test group", Position: 99}); err != nil {
		t.Fatal(err)
	}
	page, err := ops.ListStaffTags(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	var groupID string
	for _, group := range page.Groups {
		if group.Slug == "staff-test-group" {
			groupID = group.ID
		}
	}
	if err := ops.CreateTag(ctx, admin, staffops.TagFields{GroupID: groupID, Slug: "staff-test-tag", DisplayName: "Test tag", Position: 1}); err != nil {
		t.Fatal(err)
	}
	page, _ = ops.ListStaffTags(ctx, admin)
	tag := findTag(page, "staff-test-tag")

	exchange := readFixture(t, "bp-starter-smelter.txt")
	suggested := func() bool {
		inspection, err := service.InspectBlueprint(ctx, exchange)
		if err != nil {
			t.Fatal(err)
		}
		return slices.Contains(inspection.SuggestedTags, "staff-test-tag")
	}
	inspection, err := service.InspectBlueprint(ctx, exchange)
	if err != nil || len(inspection.Entities) == 0 {
		t.Fatalf("inspect the fixture: %v, %+v", err, inspection.Entities)
	}
	entity := inspection.Entities[0].Name
	if suggested() {
		t.Fatal("a tag without rules is suggested")
	}
	if err := ops.SetTagRule(ctx, admin, tag.ID, staffops.StaffTagRule{Kind: "entity", Name: entity, MinCount: 1, Points: 100}); err != nil {
		t.Fatal(err)
	}
	if !suggested() {
		t.Fatal("the new rule does not apply")
	}
	if err := ops.DeleteTagRule(ctx, admin, tag.ID, "entity", entity); err != nil {
		t.Fatal(err)
	}
	if suggested() {
		t.Fatal("the deleted rule still applies")
	}
}

// A tag group is renamed and moved by staff; the audit row records it, an
// unknown group is not found and a bad name is refused.
func TestUpdateTagGroup(t *testing.T) {
	service, pool, _, _ := newStaffFixtures(t)
	cleanupTagRows(t, pool)
	ctx := context.Background()
	ops := opsFor(service)
	if err := ops.CreateTagGroup(ctx, admin, staffops.TagFields{Slug: "staff-test-group", DisplayName: "Test group", Position: 99}); err != nil {
		t.Fatal(err)
	}
	page, err := ops.ListStaffTags(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	var group *staffops.StaffTagGroup
	for i := range page.Groups {
		if page.Groups[i].Slug == "staff-test-group" {
			group = &page.Groups[i]
		}
	}
	if group == nil {
		t.Fatal("the new group is not listed")
	}
	if err := ops.UpdateTagGroup(ctx, admin, group.ID, staffops.TagFields{DisplayName: "Renamed group", Position: 7}); err != nil {
		t.Fatal(err)
	}
	page, _ = ops.ListStaffTags(ctx, admin)
	for _, got := range page.Groups {
		if got.ID == group.ID && (got.DisplayName != "Renamed group" || got.Position != 7 || got.Slug != "staff-test-group") {
			t.Fatalf("group = %+v, want renamed and moved with its slug kept", got)
		}
	}
	if err := ops.UpdateTagGroup(ctx, admin, "AAAAAAAAAAAA", staffops.TagFields{DisplayName: "x"}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("unknown group: %v", err)
	}
	if err := ops.UpdateTagGroup(ctx, admin, group.ID, staffops.TagFields{DisplayName: " "}); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("blank name: %v", err)
	}
	if actions := auditActions(t, pool); len(actions) == 0 || actions[len(actions)-1] != "alice-staff tag_group.update "+group.ID {
		t.Fatalf("audit = %v", actions)
	}
}
