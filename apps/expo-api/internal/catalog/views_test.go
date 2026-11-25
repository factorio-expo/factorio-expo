package catalog_test

import (
	"context"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// The view flush counts only live, non-private entries that are not quick
// previews; the entry and its cards report the total, and the search refresh
// sees each changed count once.
func TestViewCountsFlushAndRead(t *testing.T) {
	service, pool, owner, _ := newSocialFixtures(t)
	ctx := context.Background()
	queries := db.New(pool)

	create := func(visibility string) catalog.Entry {
		t.Helper()
		submission := catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), Visibility: visibility}
		if visibility != "public" {
			submission.OwnerUserID = owner
		}
		entry, err := service.CreateEntry(ctx, submission)
		if err != nil {
			t.Fatal(err)
		}
		return entry
	}
	public, unlisted, private := create("public"), create("unlisted"), create("private")
	preview, err := service.CreateQuickPreview(ctx, readFixture(t, "bp-starter-smelter.txt"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	deleted := create("public")
	if _, err := pool.Exec(ctx, "UPDATE entries SET deleted_at = now() WHERE id = $1", deleted.ID); err != nil {
		t.Fatal(err)
	}

	add := func(views map[string]int64) {
		t.Helper()
		params := db.AddEntryViewsParams{}
		for id, n := range views {
			params.EntryIds = append(params.EntryIds, id)
			params.Views = append(params.Views, n)
		}
		if err := queries.AddEntryViews(ctx, params); err != nil {
			t.Fatal(err)
		}
	}
	add(map[string]int64{
		public.ID: 3, unlisted.ID: 2, private.ID: 5, preview.ID: 1, deleted.ID: 1,
		"nosuchentry0": 9,
	})
	add(map[string]int64{public.ID: 4})

	want := map[string]int64{public.ID: 7, unlisted.ID: 2, private.ID: 0, preview.ID: 0}
	for id, views := range want {
		got, err := queries.GetEntryViewCount(ctx, id)
		if err != nil || got != views {
			t.Fatalf("views of %s = %d, %v; want %d", id, got, err, views)
		}
	}
	entry, err := service.GetEntry(ctx, public.ID, "")
	if err != nil || entry.ViewCount != 7 {
		t.Fatalf("entry view_count = %d, %v; want 7", entry.ViewCount, err)
	}
	cards, err := service.EntryCards(ctx, []string{public.ID}, "")
	if err != nil || cards[public.ID].ViewCount != 7 {
		t.Fatalf("cards = %+v, %v; want 7 views", cards, err)
	}

	// Both counted entries changed; only the public one has a search document.
	changed, err := queries.ListUnindexedViewCounts(ctx, 10)
	if err != nil || len(changed) != 2 {
		t.Fatalf("changed = %+v, %v; want 2 rows", changed, err)
	}
	for _, row := range changed {
		if row.Searchable != (row.EntryID == public.ID) {
			t.Fatalf("row %+v: searchable should be true only for the public entry", row)
		}
	}
	if err := queries.MarkViewCountsIndexed(ctx, db.MarkViewCountsIndexedParams{
		EntryIds: []string{public.ID, unlisted.ID}, Views: []int64{7, 2},
	}); err != nil {
		t.Fatal(err)
	}
	add(map[string]int64{public.ID: 1})
	changed, err = queries.ListUnindexedViewCounts(ctx, 10)
	if err != nil || len(changed) != 1 || changed[0].EntryID != public.ID || changed[0].Views != 8 {
		t.Fatalf("changed after a new view = %+v, %v; want the public entry at 8", changed, err)
	}
}

// Trending ranks listed entries by their views in a calendar period; days
// before the period, unlisted entries and the pruned months do not count.
func TestTrendingByPeriod(t *testing.T) {
	service, pool, owner, _ := newSocialFixtures(t)
	ctx := context.Background()
	queries := db.New(pool)

	create := func(visibility string) catalog.Entry {
		t.Helper()
		submission := catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), Visibility: visibility}
		if visibility != "public" {
			submission.OwnerUserID = owner
		}
		entry, err := service.CreateEntry(ctx, submission)
		if err != nil {
			t.Fatal(err)
		}
		return entry
	}
	first, second, unlisted := create("public"), create("public"), create("unlisted")
	if err := queries.AddEntryViews(ctx, db.AddEntryViewsParams{
		EntryIds: []string{first.ID, second.ID, unlisted.ID}, Views: []int64{2, 5, 9},
	}); err != nil {
		t.Fatal(err)
	}
	// A big day in an earlier month counts for no period.
	if _, err := pool.Exec(ctx, `INSERT INTO entry_view_days (day, entry_id, views)
		VALUES (date_trunc('month', now() AT TIME ZONE 'UTC')::date - 3, $1, 1000)`,
		first.ID); err != nil {
		t.Fatal(err)
	}

	ids := func(period catalog.TrendingPeriod) []string {
		t.Helper()
		cards, err := service.ListTrending(ctx, period, 12, "")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, card := range cards {
			out = append(out, card.ID)
		}
		return out
	}
	for _, period := range []catalog.TrendingPeriod{catalog.TrendingDay, catalog.TrendingWeek, catalog.TrendingMonth} {
		if got := ids(period); len(got) != 2 || got[0] != second.ID || got[1] != first.ID {
			t.Fatalf("%s = %v; want [%s %s]", period, got, second.ID, first.ID)
		}
	}
	if _, err := service.ListTrending(ctx, "year", 12, ""); err == nil {
		t.Fatal("an unknown period was accepted")
	}
	if pruned, err := service.PruneViewDays(ctx); err != nil || pruned != 1 {
		t.Fatalf("pruned = %d, %v; want the earlier month's row", pruned, err)
	}
}
