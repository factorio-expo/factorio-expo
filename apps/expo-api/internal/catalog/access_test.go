package catalog_test

import (
	"context"
	"errors"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// TestEntryAccessMatrix drives the entry-access module through the public
// service calls: who may view and who may edit, per visibility and liveness.
func TestEntryAccessMatrix(t *testing.T) {
	service, pool, owner, other := newSocialFixtures(t)
	ctx := context.Background()

	type shape struct {
		name       string
		owned      bool
		visibility string
	}
	for _, entryShape := range []shape{
		{"anonymous public", false, "public"},
		{"owned public", true, "public"},
		{"owned unlisted", true, "unlisted"},
		{"owned private", true, "private"},
	} {
		for _, deleted := range []bool{false, true} {
			submission := catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), Visibility: entryShape.visibility}
			if entryShape.owned {
				submission.OwnerUserID = owner
			}
			entry, err := service.CreateEntry(ctx, submission)
			if err != nil {
				t.Fatal(err)
			}
			if deleted {
				if _, err := pool.Exec(ctx, "UPDATE entries SET deleted_at = now() WHERE id = $1", entry.ID); err != nil {
					t.Fatal(err)
				}
			}
			for _, viewer := range []struct {
				name string
				auth catalog.Authorization
			}{
				{"anonymous", catalog.Authorization{}},
				{"edit token", catalog.Authorization{EditToken: entry.EditToken}},
				{"owner", catalog.Authorization{UserID: owner}},
				{"other user", catalog.Authorization{UserID: other}},
			} {
				isOwner := entryShape.owned && viewer.auth.UserID == owner
				isTokenEditor := !entryShape.owned && viewer.auth.EditToken != ""
				canView := !deleted && (entryShape.visibility != "private" || isOwner)
				canEdit := canView && (isOwner || isTokenEditor)

				name := entryShape.name + "/deleted=" + map[bool]string{true: "y", false: "n"}[deleted] + "/" + viewer.name
				t.Run(name, func(t *testing.T) {
					_, err := service.GetEntry(ctx, entry.ID, viewer.auth.UserID)
					if canView != (err == nil) || (!canView && !errors.Is(err, catalog.ErrNotFound)) {
						t.Fatalf("view = %v, want allowed=%v", err, canView)
					}
					allowed, err := service.CanEditEntry(ctx, entry.ID, viewer.auth)
					switch {
					case !canView:
						if !errors.Is(err, catalog.ErrNotFound) {
							t.Fatalf("edit check on invisible entry = %v, want not found", err)
						}
					case err != nil || allowed != canEdit:
						t.Fatalf("can edit = %v, %v; want %v", allowed, err, canEdit)
					}
				})
			}
		}
	}
}
