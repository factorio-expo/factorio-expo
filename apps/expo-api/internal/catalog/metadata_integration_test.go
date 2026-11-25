package catalog_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
)

func TestEntryMetadataUpdatePreservesRevisionAndTags(t *testing.T) {
	service, pool := newService(t)
	ctx := context.Background()
	title, description := "Original title", "Original description"
	entry, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readFixture(t, "book-starter-kit.txt"), Title: &title, Description: &description,
		Tags: []string{"starter-base", "mid-game"},
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := service.GetEntry(ctx, entry.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM entry_search_jobs WHERE entry_id = $1", entry.ID); err != nil {
		t.Fatal(err)
	}
	auth := catalog.Authorization{EditToken: entry.EditToken}
	title = "  Updated title  "
	updated, err := service.UpdateEntryMetadata(ctx, entry.ID, auth, catalog.EntryMetadataUpdate{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Updated title" || updated.Description != before.Description || updated.EditToken != "" {
		t.Fatalf("updated entry metadata = %+v", updated)
	}
	if !updated.UpdatedAt.After(before.UpdatedAt) {
		t.Fatal("metadata edit must advance updated_at")
	}
	if !reflect.DeepEqual(updated.Revision, before.Revision) || !reflect.DeepEqual(updated.Tags, before.Tags) {
		t.Fatal("metadata edit changed immutable revision or tags")
	}
	var action string
	if err := pool.QueryRow(ctx, "SELECT action FROM entry_search_jobs WHERE entry_id = $1", entry.ID).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if action != "index" {
		t.Fatalf("search action = %q", action)
	}
	description = "  **Markdown**\n\n[link](https://example.com)  "
	updated, err = service.UpdateEntryMetadata(ctx, entry.ID, auth, catalog.EntryMetadataUpdate{Description: &description})
	if err != nil || updated.Title != "Updated title" || updated.Description != strings.TrimSpace(description) {
		t.Fatalf("description-only update = %+v, %v", updated, err)
	}
	description = ""
	updated, err = service.UpdateEntryMetadata(ctx, entry.ID, auth, catalog.EntryMetadataUpdate{Description: &description})
	if err != nil || updated.Description != "" {
		t.Fatalf("clear description = %+v, %v", updated, err)
	}
	tags := []string{"mid-game"}
	updated, err = service.UpdateEntryMetadata(ctx, entry.ID, auth, catalog.EntryMetadataUpdate{Tags: &tags})
	if err != nil || !reflect.DeepEqual(updated.Tags, tags) || updated.Title != "Updated title" {
		t.Fatalf("replace tags = %+v, %v", updated, err)
	}
	unknown := []string{"no-such-tag"}
	if _, err := service.UpdateEntryMetadata(ctx, entry.ID, auth, catalog.EntryMetadataUpdate{Tags: &unknown}); !errors.Is(err, catalog.ErrInvalidTags) {
		t.Fatalf("unknown tag err = %v", err)
	}
	none := []string{}
	updated, err = service.UpdateEntryMetadata(ctx, entry.ID, auth, catalog.EntryMetadataUpdate{Tags: &none})
	if err != nil || len(updated.Tags) != 0 {
		t.Fatalf("clear tags = %+v, %v", updated, err)
	}
	revisions, err := service.ListRevisions(ctx, entry.ID, "")
	if err != nil || len(revisions) != 1 || revisions[0].ID != before.Revision.ID {
		t.Fatalf("revisions changed: %+v, %v", revisions, err)
	}
}

func TestEntryMetadataPermissionsAndMutations(t *testing.T) {
	service, _, owner, other := newSocialFixtures(t)
	ctx := context.Background()
	title := "Updated"
	for _, owned := range []bool{false, true} {
		submission := catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")}
		if owned {
			submission.OwnerUserID = owner
		}
		entry, err := service.CreateEntry(ctx, submission)
		if err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct {
			auth  catalog.Authorization
			allow bool
		}{
			{catalog.Authorization{}, false},
			{catalog.Authorization{UserID: other}, false},
			{catalog.Authorization{EditToken: "wrong"}, false},
			{catalog.Authorization{UserID: owner}, owned},
			{catalog.Authorization{EditToken: entry.EditToken}, !owned},
		} {
			allowed, err := service.CanEditEntry(ctx, entry.ID, test.auth)
			if err != nil || allowed != test.allow {
				t.Fatalf("permissions = %v, %v; want %v", allowed, err, test.allow)
			}
			_, err = service.UpdateEntryMetadata(ctx, entry.ID, test.auth, catalog.EntryMetadataUpdate{Title: &title})
			if test.allow && err != nil || !test.allow && !errors.Is(err, catalog.ErrForbidden) {
				t.Fatalf("metadata mutation allowed=%v: %v", test.allow, err)
			}
		}
		auth := catalog.Authorization{UserID: owner, EditToken: entry.EditToken}
		for _, invalid := range []catalog.EntryMetadataUpdate{
			{Title: new(" \n ")}, {Title: new(strings.Repeat("x", 161))},
			{Description: new(strings.Repeat("x", 5001))}, {},
		} {
			if _, err := service.UpdateEntryMetadata(ctx, entry.ID, auth, invalid); !errors.Is(err, catalog.ErrInvalidEntryMetadata) {
				t.Fatalf("invalid metadata error = %v", err)
			}
		}
		if err := service.DeleteEntry(ctx, entry.ID, auth); err != nil {
			t.Fatal(err)
		}
		if _, err := service.CanEditEntry(ctx, entry.ID, auth); !errors.Is(err, catalog.ErrNotFound) {
			t.Fatalf("deleted entry permissions = %v", err)
		}
		if _, err := service.UpdateEntryMetadata(ctx, entry.ID, auth, catalog.EntryMetadataUpdate{Title: &title}); !errors.Is(err, catalog.ErrNotFound) {
			t.Fatalf("deleted entry update = %v", err)
		}
	}
	if _, err := service.CanEditEntry(ctx, "000000000000", catalog.Authorization{}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("missing entry permissions = %v", err)
	}
}

func TestEntryMetadataSearchFailureRollsBack(t *testing.T) {
	service, pool := newService(t)
	ctx := context.Background()
	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatal(err)
	}
	before, err := service.GetEntry(ctx, entry.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM entry_search_jobs WHERE entry_id = $1", entry.ID); err != nil {
		t.Fatal(err)
	}
	// A failing outbox write must roll back metadata and updated_at together.
	if _, err := pool.Exec(ctx, "ALTER TABLE entry_search_jobs ADD CONSTRAINT reject_metadata_test CHECK (false) NOT VALID"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "ALTER TABLE entry_search_jobs DROP CONSTRAINT reject_metadata_test"); err != nil {
			t.Errorf("restore search outbox: %v", err)
		}
	})
	title := "Must roll back"
	if _, err := service.UpdateEntryMetadata(ctx, entry.ID, catalog.Authorization{EditToken: entry.EditToken},
		catalog.EntryMetadataUpdate{Title: &title}); err == nil {
		t.Fatal("expected search enqueue failure")
	}
	after, err := service.GetEntry(ctx, entry.ID, "")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed metadata transaction changed entry: %+v, %v", after, err)
	}
}

func TestHistoricalLeafDetailIsEntryScoped(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()
	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatal(err)
	}
	leaf := entry.Revision.Root.Leaves()[0]
	auth := catalog.Authorization{EditToken: entry.EditToken}
	if _, err := service.CreateRevision(ctx, entry.ID, auth, catalog.Submission{Exchange: readFixture(t, "book-starter-kit.txt")}); err != nil {
		t.Fatal(err)
	}
	detail, err := service.GetLeafDetail(ctx, entry.ID, leaf.ID, "", "")
	if err != nil || detail.Revision.ID != entry.Revision.ID || detail.Revision.VersionNumber != 1 {
		t.Fatalf("historical leaf = %+v, %v", detail, err)
	}
	other, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetLeafDetail(ctx, other.ID, leaf.ID, "", ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("cross-entry leaf read = %v", err)
	}
	if err := service.DeleteEntry(ctx, entry.ID, auth); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetLeafDetail(ctx, entry.ID, leaf.ID, "", ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("deleted entry leaf read = %v", err)
	}
}

// The spoiler flag is set on upload, read back on the entry and its card, and
// changed by a metadata edit; an edit without it keeps it.
func TestEntrySpoilerFlag(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()
	title := "Spoiler"
	entry, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readFixture(t, "bp-starter-smelter.txt"), Title: &title, Spoiler: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := service.GetEntry(ctx, entry.ID, "")
	if err != nil || !entry.Spoiler || !read.Spoiler {
		t.Fatalf("created spoiler = %v, read = %+v, %v", entry.Spoiler, read, err)
	}
	cards, err := service.EntryCards(ctx, []string{entry.ID}, "")
	if err != nil || !cards[entry.ID].Spoiler {
		t.Fatalf("card = %+v, %v", cards[entry.ID], err)
	}
	auth := catalog.Authorization{EditToken: entry.EditToken}
	title = "Renamed"
	updated, err := service.UpdateEntryMetadata(ctx, entry.ID, auth, catalog.EntryMetadataUpdate{Title: &title})
	if err != nil || !updated.Spoiler {
		t.Fatalf("title edit = %+v, %v; want the flag kept", updated, err)
	}
	off := false
	updated, err = service.UpdateEntryMetadata(ctx, entry.ID, auth, catalog.EntryMetadataUpdate{Spoiler: &off})
	if err != nil || updated.Spoiler || updated.Title != "Renamed" {
		t.Fatalf("spoiler-only edit = %+v, %v", updated, err)
	}
}
