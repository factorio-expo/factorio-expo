package catalog_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"factorio-expo/apps/expo-api/internal/catalog"
)

//go:fix inline
func str(value string) *string { return new(value) }

func uploadAs(t *testing.T, service *catalog.Service, user string, mutate func(*catalog.Submission)) catalog.Entry {
	t.Helper()
	submission := catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), OwnerUserID: user}
	if mutate != nil {
		mutate(&submission)
	}
	entry, err := service.CreateEntry(context.Background(), submission)
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	return entry
}

func TestImportedEntryKeepsItsSourceURL(t *testing.T) {
	service, _, user, _ := newSocialFixtures(t)
	ctx := context.Background()
	entry := uploadAs(t, service, user, func(s *catalog.Submission) { s.SourceURL = "https://pastebin.com/aBcD1234" })
	if entry.SourceURL != "https://pastebin.com/aBcD1234" {
		t.Fatalf("created source = %q", entry.SourceURL)
	}
	read, err := service.GetEntry(ctx, entry.ID, "")
	if err != nil || read.SourceURL != entry.SourceURL {
		t.Fatalf("read source = %q, %v", read.SourceURL, err)
	}
	if plain := uploadAs(t, service, user, nil); plain.SourceURL != "" {
		t.Fatalf("an upload has source %q", plain.SourceURL)
	}
}

func TestRevisionNotes(t *testing.T) {
	service, _, user, _ := newSocialFixtures(t)
	ctx := context.Background()
	anonymous, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatal(err)
	}
	owned := uploadAs(t, service, user, nil)

	revise := func(entry catalog.Entry, auth catalog.Authorization, note string) (catalog.Entry, error) {
		return service.CreateRevision(ctx, entry.ID, auth, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), Note: note})
	}
	// Trimmed, kept on the revision, listed with it.
	revised, err := revise(owned, catalog.Authorization{UserID: user}, "  Fixed the belts\n")
	if err != nil || revised.Revision.Note != "Fixed the belts" {
		t.Fatalf("revision = %+v, %v", revised.Revision, err)
	}
	if first := owned.Revision.Note; first != "" {
		t.Fatalf("first revision note = %q", first)
	}
	listed, err := service.ListRevisions(ctx, owned.ID, user)
	if err != nil || len(listed) != 2 || listed[0].Note != "Fixed the belts" || listed[1].Note != "" {
		t.Fatalf("listed = %+v, %v", listed, err)
	}
	got, err := service.GetRevision(ctx, owned.ID, revised.Revision.ID, user)
	if err != nil || got.Note != "Fixed the belts" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	// An anonymous edit may add one too.
	if r, err := revise(anonymous, catalog.Authorization{EditToken: anonymous.EditToken}, "by token"); err != nil || r.Revision.Note != "by token" {
		t.Fatalf("anonymous revision = %+v, %v", r.Revision, err)
	}
	// 280 characters are fine (counted as characters, not bytes), 281 are not.
	if _, err := revise(owned, catalog.Authorization{UserID: user}, strings.Repeat("\u00e9", 280)); err != nil {
		t.Fatalf("280 characters: %v", err)
	}
	for name, note := range map[string]string{
		"too long": strings.Repeat("x", 281), "control": "bell\x07", "invalid utf-8": "a\xffb",
		"bidi override": "evil\u202egnp.exe", "zero width": "a\u200bb",
	} {
		if _, err := revise(owned, catalog.Authorization{UserID: user}, note); !errors.Is(err, catalog.ErrInvalidRevisionNote) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// A rejected note creates no revision.
	if listed, _ := service.ListRevisions(ctx, owned.ID, user); len(listed) != 3 {
		t.Fatalf("%d revisions after the rejected notes, want 3", len(listed))
	}
}

func TestAliasLifecycle(t *testing.T) {
	service, _, user, other := newSocialFixtures(t)
	ctx := context.Background()
	owner := catalog.Authorization{UserID: user}

	entry := uploadAs(t, service, user, func(s *catalog.Submission) { s.Alias = "  Iron-Smelter " })
	if entry.Alias != "iron-smelter" {
		t.Fatalf("alias = %q, want it lower-cased and trimmed", entry.Alias)
	}
	id, err := service.ResolveAlias(ctx, "SOCIAL-test-alice", "IRON-smelter", "")
	if err != nil || id != entry.ID {
		t.Fatalf("resolve = %q, %v", id, err)
	}
	// Another user's namespace is separate.
	if _, err := service.ResolveAlias(ctx, "social-test-bob", "iron-smelter", ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("other user's alias: %v", err)
	}
	bobs := uploadAs(t, service, other, func(s *catalog.Submission) { s.Alias = "iron-smelter" })
	if bobs.Alias != "iron-smelter" {
		t.Fatalf("bob's alias = %q", bobs.Alias)
	}

	// Unique per user.
	second := uploadAs(t, service, user, nil)
	if _, err := service.UpdateEntryMetadata(ctx, second.ID, owner, catalog.EntryMetadataUpdate{Alias: new("iron-smelter")}); !errors.Is(err, catalog.ErrAliasTaken) {
		t.Fatalf("duplicate alias: %v", err)
	}
	if _, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), OwnerUserID: user, Alias: "iron-smelter"}); !errors.Is(err, catalog.ErrAliasTaken) {
		t.Fatalf("duplicate alias at upload: %v", err)
	}

	// Changing it frees the old one at once: no history, no redirect.
	changed, err := service.UpdateEntryMetadata(ctx, entry.ID, owner, catalog.EntryMetadataUpdate{Alias: new("copper-line")})
	if err != nil || changed.Alias != "copper-line" {
		t.Fatalf("change = %+v, %v", changed, err)
	}
	if _, err := service.ResolveAlias(ctx, "social-test-alice", "iron-smelter", ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("old alias still resolves: %v", err)
	}
	if id, err := service.ResolveAlias(ctx, "social-test-alice", "copper-line", ""); err != nil || id != entry.ID {
		t.Fatalf("new alias: %q, %v", id, err)
	}
	if _, err := service.UpdateEntryMetadata(ctx, second.ID, owner, catalog.EntryMetadataUpdate{Alias: new("iron-smelter")}); err != nil {
		t.Fatalf("taking the freed alias: %v", err)
	}

	// Not changing metadata leaves it; an empty one removes it.
	if kept, err := service.UpdateEntryMetadata(ctx, entry.ID, owner, catalog.EntryMetadataUpdate{Title: new("Renamed")}); err != nil || kept.Alias != "copper-line" {
		t.Fatalf("kept = %+v, %v", kept, err)
	}
	if cleared, err := service.UpdateEntryMetadata(ctx, entry.ID, owner, catalog.EntryMetadataUpdate{Alias: new("")}); err != nil || cleared.Alias != "" {
		t.Fatalf("cleared = %+v, %v", cleared, err)
	}
	if _, err := service.ResolveAlias(ctx, "social-test-alice", "copper-line", ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("cleared alias resolves: %v", err)
	}
}

func TestAliasRules(t *testing.T) {
	service, _, user, _ := newSocialFixtures(t)
	ctx := context.Background()
	entry := uploadAs(t, service, user, nil)
	owner := catalog.Authorization{UserID: user}
	for name, slug := range map[string]string{
		"too short": "ab", "too long": strings.Repeat("a", 61), "underscore": "iron_smelter",
		"space": "iron smelter", "slash": "iron/smelter", "dots": "iron.smelter", "leading hyphen": "-iron",
		"trailing hyphen": "iron-", "unicode": "iron\u00e9", "reserved": "new", "reserved feed": "feed",
		"entry id": "aZ3k9QxP2mT7", "entry id lower": "abcdefghijkl", "percent": "iron%2fsmelter",
	} {
		if _, err := service.UpdateEntryMetadata(ctx, entry.ID, owner, catalog.EntryMetadataUpdate{Alias: new(slug)}); !errors.Is(err, catalog.ErrInvalidAlias) {
			t.Errorf("%s (%q): err = %v", name, slug, err)
		}
	}
	for _, slug := range []string{"abc", "a-b", "iron-smelter-2", strings.Repeat("a", 57) + "-1", "123", "my-12-char-id"} {
		if _, err := service.UpdateEntryMetadata(ctx, entry.ID, owner, catalog.EntryMetadataUpdate{Alias: new(slug)}); err != nil {
			t.Errorf("%q: %v", slug, err)
		}
	}
	// Anonymous entries cannot have one, at upload or edit.
	if _, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), Alias: "mine-now"}); !errors.Is(err, catalog.ErrAliasNeedsAccount) {
		t.Fatalf("anonymous upload with alias: %v", err)
	}
	anonymous, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateEntryMetadata(ctx, anonymous.ID, catalog.Authorization{EditToken: anonymous.EditToken}, catalog.EntryMetadataUpdate{Alias: new("mine-now")}); !errors.Is(err, catalog.ErrAliasNeedsAccount) {
		t.Fatalf("anonymous edit with alias: %v", err)
	}
}

func TestAliasFollowsTheEntryLifecycle(t *testing.T) {
	service, pool, user, other := newSocialFixtures(t)
	ctx := context.Background()
	owner := catalog.Authorization{UserID: user}

	// A private entry's alias is the owner's alone; deleting frees the slug.
	private := uploadAs(t, service, user, func(s *catalog.Submission) { s.Alias = "secret-base"; s.Visibility = "private" })
	if id, err := service.ResolveAlias(ctx, "social-test-alice", "secret-base", user); err != nil || id != private.ID {
		t.Fatalf("owner resolves: %q, %v", id, err)
	}
	for _, viewer := range []string{"", other} {
		if _, err := service.ResolveAlias(ctx, "social-test-alice", "secret-base", viewer); !errors.Is(err, catalog.ErrNotFound) {
			t.Fatalf("viewer %q: %v", viewer, err)
		}
	}
	if err := service.DeleteEntry(ctx, private.ID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveAlias(ctx, "social-test-alice", "secret-base", user); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("deleted entry's alias: %v", err)
	}
	if again := uploadAs(t, service, user, func(s *catalog.Submission) { s.Alias = "secret-base" }); again.Alias != "secret-base" {
		t.Fatalf("reused alias = %q", again.Alias)
	}

	// An expired entry no longer holds its slug either.
	expiring := uploadAs(t, service, user, func(s *catalog.Submission) { s.Alias = "short-lived"; s.ExpiresInDays = 1 })
	if _, err := pool.Exec(ctx, "UPDATE entries SET expires_at = now() - interval '1 minute' WHERE id = $1", expiring.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveAlias(ctx, "social-test-alice", "short-lived", ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("expired entry's alias: %v", err)
	}
	if again := uploadAs(t, service, user, func(s *catalog.Submission) { s.Alias = "short-lived" }); again.Alias != "short-lived" {
		t.Fatalf("alias after expiry = %q", again.Alias)
	}
}

func TestUserFeed(t *testing.T) {
	service, pool, user, other := newSocialFixtures(t)
	ctx := context.Background()
	owner := catalog.Authorization{UserID: user}
	revise := func(id, note string) catalog.Entry {
		t.Helper()
		entry, err := service.CreateRevision(ctx, id, owner, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), Note: note})
		if err != nil {
			t.Fatal(err)
		}
		return entry
	}
	stamp := func(entryID string, minutesAgo int) {
		t.Helper()
		if _, err := pool.Exec(ctx, "UPDATE revisions SET created_at = now() - make_interval(mins => $2 - version_number) WHERE entry_id = $1", entryID, minutesAgo); err != nil {
			t.Fatal(err)
		}
	}

	listed := uploadAs(t, service, user, func(s *catalog.Submission) { s.Title = new("Smelter"); s.Description = new("Iron plates") })
	revise(listed.ID, "Fixed the belts")
	revise(listed.ID, "")
	stamp(listed.ID, 30)
	// Everything below must stay out of the feed.
	unlisted := uploadAs(t, service, user, func(s *catalog.Submission) { s.Visibility = "unlisted" })
	private := uploadAs(t, service, user, func(s *catalog.Submission) { s.Visibility = "private" })
	deleted := uploadAs(t, service, user, nil)
	if err := service.DeleteEntry(ctx, deleted.ID, owner); err != nil {
		t.Fatal(err)
	}
	anonymous, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatal(err)
	}
	uploadAs(t, service, other, nil)
	preview, err := service.CreateQuickPreview(ctx, readFixture(t, "bp-starter-smelter.txt"), "", nil)
	if err != nil {
		t.Fatalf("quick preview: %v", err)
	}
	for _, id := range []string{unlisted.ID, private.ID, deleted.ID, anonymous.ID, preview.ID} {
		stamp(id, 1)
	}

	feed, err := service.UserFeed(ctx, "SOCIAL-test-ALICE")
	if err != nil {
		t.Fatal(err)
	}
	if feed.Username != "social-test-alice" || len(feed.Items) != 3 {
		t.Fatalf("feed = %+v", feed)
	}
	// Newest first: the third revision (no note), the second (a note), the first (the entry).
	newest, middle, first := feed.Items[0], feed.Items[1], feed.Items[2]
	if newest.VersionNumber != 3 || newest.Note != "" || newest.FirstRevision || newest.Description != "" ||
		middle.VersionNumber != 2 || middle.Note != "Fixed the belts" || middle.FirstRevision ||
		first.VersionNumber != 1 || !first.FirstRevision || first.Description != "Iron plates" || first.Note != "" {
		t.Fatalf("items = %+v", feed.Items)
	}
	for _, item := range feed.Items {
		if item.EntryID != listed.ID || item.Title != "Smelter" || item.RevisionID == "" || item.CreatedAt.IsZero() || item.CreatedAt.After(time.Now()) {
			t.Fatalf("item = %+v", item)
		}
	}

	// A private entry that turns public appears; one that turns private leaves.
	public := catalog.Visibility("public")
	if _, err := service.UpdateEntryMetadata(ctx, unlisted.ID, owner, catalog.EntryMetadataUpdate{Visibility: &public}); err != nil {
		t.Fatal(err)
	}
	if feed, _ := service.UserFeed(ctx, "social-test-alice"); len(feed.Items) != 4 {
		t.Fatalf("after publishing: %d items", len(feed.Items))
	}
	hide := catalog.Visibility("private")
	if _, err := service.UpdateEntryMetadata(ctx, listed.ID, owner, catalog.EntryMetadataUpdate{Visibility: &hide}); err != nil {
		t.Fatal(err)
	}
	if feed, _ := service.UserFeed(ctx, "social-test-alice"); len(feed.Items) != 1 {
		t.Fatalf("after hiding: %d items", len(feed.Items))
	}

	if _, err := service.UserFeed(ctx, "nobody-here"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}

func TestUserFeedIsCappedAtThirtyItems(t *testing.T) {
	service, _, user, _ := newSocialFixtures(t)
	ctx := context.Background()
	entry := uploadAs(t, service, user, nil)
	for range catalog.FeedLimit + 2 {
		if _, err := service.CreateRevision(ctx, entry.ID, catalog.Authorization{UserID: user}, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")}); err != nil {
			t.Fatal(err)
		}
	}
	feed, err := service.UserFeed(ctx, "social-test-alice")
	if err != nil || len(feed.Items) != catalog.FeedLimit {
		t.Fatalf("items = %d, %v", len(feed.Items), err)
	}
}

func TestDeletedAccountHasNoFeed(t *testing.T) {
	service, pool, user, _ := newSocialFixtures(t)
	ctx := context.Background()
	uploadAs(t, service, user, nil)
	if _, err := pool.Exec(ctx, "UPDATE users SET deleted_at = now() WHERE id = $1", user); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UserFeed(ctx, "social-test-alice"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("deleted account's feed: %v", err)
	}
}
