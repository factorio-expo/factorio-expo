package staffops_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
)

func TestHomePicksAddCaptionRemoveAndLimits(t *testing.T) {
	service, pool, owner, _ := newStaffFixtures(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "TRUNCATE home_picks"); err != nil {
		t.Fatal(err)
	}
	ops := opsFor(service)
	upload := func(n int, visibility string) catalog.Entry {
		title := fmt.Sprintf("Pick %d", n)
		entry, err := service.CreateEntry(ctx, catalog.Submission{
			Exchange: readFixture(t, "bp-starter-smelter.txt"), Title: &title, OwnerUserID: owner, Visibility: visibility,
		})
		if err != nil {
			t.Fatalf("upload: %v", err)
		}
		return entry
	}

	private := upload(0, "private")
	if err := ops.AddHomePick(ctx, admin, private.Revision.Root.ID, ""); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("pick a private leaf: %v, want refused", err)
	}
	var picked []catalog.Entry
	for n := 1; n <= catalog.MaxHomePicks; n++ {
		entry := upload(n, "public")
		if err := ops.AddHomePick(ctx, admin, entry.Revision.Root.ID, fmt.Sprintf("Reason %d", n)); err != nil {
			t.Fatalf("pick %d: %v", n, err)
		}
		picked = append(picked, entry)
	}
	if err := ops.AddHomePick(ctx, admin, picked[0].Revision.Root.ID, ""); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("pick twice: %v, want refused", err)
	}
	if err := ops.AddHomePick(ctx, admin, upload(9, "public").Revision.Root.ID, ""); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("a seventh pick: %v, want refused", err)
	}
	if err := ops.SetHomePickCaption(ctx, admin, picked[0].Revision.Root.ID, "two\nlines"); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("caption with a line break: %v, want refused", err)
	}
	if err := ops.SetHomePickCaption(ctx, colleague, picked[0].Revision.Root.ID, "  Clean ratios  "); err != nil {
		t.Fatalf("caption: %v", err)
	}

	// The public read leaves out a pick whose entry went private.
	private2 := catalog.VisibilityPrivate
	if _, err := service.UpdateEntryMetadata(ctx, picked[1].ID, catalog.Authorization{UserID: owner},
		catalog.EntryMetadataUpdate{Visibility: &private2}); err != nil {
		t.Fatal(err)
	}
	shown, err := service.ListHomePicks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(shown) != catalog.MaxHomePicks-1 || shown[0].Leaf.EntryID != picked[len(picked)-1].ID {
		t.Fatalf("shown picks = %+v", shown)
	}
	for _, pick := range shown {
		if pick.Leaf.EntryID == picked[1].ID {
			t.Fatal("a private entry's pick is shown")
		}
		if pick.Leaf.EntryID == picked[0].ID && (pick.Caption != "Clean ratios" || pick.Leaf.Title != "Pick 1") {
			t.Fatalf("pick = %+v", pick)
		}
	}
	staffList, err := ops.ListHomePicks(ctx)
	if err != nil || len(staffList) != catalog.MaxHomePicks {
		t.Fatalf("staff list = %+v, %v", staffList, err)
	}
	hidden := 0
	for _, pick := range staffList {
		if !pick.Shown {
			hidden++
		}
	}
	if hidden != 1 {
		t.Fatalf("hidden picks = %d, want 1", hidden)
	}

	if err := ops.RemoveHomePick(ctx, admin, picked[1].Revision.Root.ID); err != nil {
		t.Fatal(err)
	}
	if err := ops.RemoveHomePick(ctx, admin, picked[1].Revision.Root.ID); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("remove twice: %v, want not found", err)
	}
	actions := auditActions(t, pool)
	if !slices.Contains(actions, "bob-staff home_pick.update "+picked[0].Revision.Root.ID) ||
		!slices.Contains(actions, "alice-staff home_pick.remove "+picked[1].Revision.Root.ID) {
		t.Fatalf("audit = %v", actions)
	}
}
