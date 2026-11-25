package catalog_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
)

func TestNotesArePinnedNumberedAndEditedByTheOwnerOnTheLiveRevision(t *testing.T) {
	service, _, owner, other := newCommentFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	leaf := entry.Revision.Root.Leaves()[0].ID
	as := catalog.Authorization{UserID: owner}

	first, err := service.CreateNote(ctx, entry.ID, leaf, as, catalog.NewNote{X: 0.25, Y: 0.5, Body: "  overflow [item=iron-ore]  "})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	second, err := service.CreateNote(ctx, entry.ID, leaf, as, catalog.NewNote{X: 1, Y: 0, Body: "second"})
	if err != nil {
		t.Fatalf("create second note: %v", err)
	}
	if first.Number != 1 || second.Number != 2 || first.Body != "overflow [item=iron-ore]" ||
		first.X != 0.25 || first.Y != 0.5 {
		t.Fatalf("notes = %+v, %+v", first, second)
	}

	fixed := "fixed"
	edited, err := service.EditNote(ctx, entry.ID, leaf, first.ID, as, catalog.NoteEdit{Body: &fixed})
	if err != nil || edited.Body != "fixed" || edited.EditedAt == nil || edited.X != 0.25 {
		t.Fatalf("edit = %+v, %v", edited, err)
	}

	// Dragging a pin moves it without marking the text edited; half a move or
	// a pin outside the image is refused.
	x, y, outside := 0.75, 0.125, 1.5
	moved, err := service.EditNote(ctx, entry.ID, leaf, second.ID, as, catalog.NoteEdit{X: &x, Y: &y})
	if err != nil || moved.X != 0.75 || moved.Y != 0.125 || moved.EditedAt != nil || moved.Body != second.Body {
		t.Fatalf("move = %+v, %v", moved, err)
	}
	for _, bad := range []catalog.NoteEdit{{X: &x}, {X: &outside, Y: &y}, {}} {
		if _, err := service.EditNote(ctx, entry.ID, leaf, second.ID, as, bad); !errors.Is(err, catalog.ErrInvalidNote) {
			t.Fatalf("edit %+v err = %v, want ErrInvalidNote", bad, err)
		}
	}

	// Everyone who can see the entry reads them; only the owner may change them.
	list, err := service.ListNotes(ctx, entry.ID, leaf, catalog.Authorization{})
	if err != nil || len(list.Notes) != 2 || list.CanEdit {
		t.Fatalf("anonymous list = %+v, %v", list, err)
	}
	if list, err = service.ListNotes(ctx, entry.ID, leaf, as); err != nil || !list.CanEdit {
		t.Fatalf("owner list = %+v, %v", list, err)
	}
	stranger := catalog.Authorization{UserID: other}
	if _, err := service.CreateNote(ctx, entry.ID, leaf, stranger, catalog.NewNote{Body: "mine"}); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("stranger creates: %v, want ErrForbidden", err)
	}
	if err := service.DeleteNote(ctx, entry.ID, leaf, first.ID, stranger); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("stranger deletes: %v, want ErrForbidden", err)
	}

	// Deleting renumbers the rest.
	if err := service.DeleteNote(ctx, entry.ID, leaf, first.ID, as); err != nil {
		t.Fatalf("delete note: %v", err)
	}
	if list, _ = service.ListNotes(ctx, entry.ID, leaf, as); len(list.Notes) != 1 || list.Notes[0].Number != 1 {
		t.Fatalf("after delete = %+v", list)
	}
	if err := service.DeleteNote(ctx, entry.ID, leaf, first.ID, as); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("delete again: %v, want ErrNotFound", err)
	}

	// A new revision's leaf is the live one: the old leaf is read-only now.
	if _, err := service.CreateRevision(ctx, entry.ID, as,
		catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")}); err != nil {
		t.Fatalf("create revision: %v", err)
	}
	if _, err := service.CreateNote(ctx, entry.ID, leaf, as, catalog.NewNote{Body: "late"}); !errors.Is(err, catalog.ErrNoteRevision) {
		t.Fatalf("note on an old revision: %v, want ErrNoteRevision", err)
	}
	if list, err = service.ListNotes(ctx, entry.ID, leaf, as); err != nil || list.CanEdit || len(list.Notes) != 1 {
		t.Fatalf("old revision list = %+v, %v", list, err)
	}
}

func TestNotesAreBounded(t *testing.T) {
	service, _, owner, _ := newCommentFixtures(t)
	ctx := context.Background()
	entry := createOwnedEntry(t, service, owner, "public")
	leaf := entry.Revision.Root.Leaves()[0].ID
	as := catalog.Authorization{UserID: owner}

	for name, input := range map[string]catalog.NewNote{
		"empty":     {Body: "  "},
		"long":      {Body: strings.Repeat("x", catalog.MaxNoteRunes+1)},
		"lines":     {Body: strings.Repeat("x\n", catalog.MaxNoteLines+1)},
		"outside x": {X: 1.5, Body: "x"},
		"outside y": {Y: -0.1, Body: "x"},
	} {
		if _, err := service.CreateNote(ctx, entry.ID, leaf, as, input); !errors.Is(err, catalog.ErrInvalidNote) {
			t.Errorf("%s: %v, want ErrInvalidNote", name, err)
		}
	}
	for range catalog.MaxNotesPerLeaf {
		if _, err := service.CreateNote(ctx, entry.ID, leaf, as, catalog.NewNote{Body: "n"}); err != nil {
			t.Fatalf("create note: %v", err)
		}
	}
	if _, err := service.CreateNote(ctx, entry.ID, leaf, as, catalog.NewNote{Body: "n"}); !errors.Is(err, catalog.ErrNoteLimit) {
		t.Fatalf("over the cap: %v, want ErrNoteLimit", err)
	}
}

func TestNotesOfAPrivateEntryStayPrivate(t *testing.T) {
	service, _, owner, _ := newCommentFixtures(t)
	entry := createOwnedEntry(t, service, owner, "private")
	leaf := entry.Revision.Root.Leaves()[0].ID
	if _, err := service.ListNotes(context.Background(), entry.ID, leaf, catalog.Authorization{}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("anonymous read: %v, want ErrNotFound", err)
	}
}

func TestHistoryListsSingleBlueprintRevisionsOnly(t *testing.T) {
	service, _, owner, _ := newCommentFixtures(t)
	ctx := context.Background()
	as := catalog.Authorization{UserID: owner}
	entry := createOwnedEntry(t, service, owner, "public")
	if _, err := service.CreateRevision(ctx, entry.ID, as,
		catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")}); err != nil {
		t.Fatalf("create revision: %v", err)
	}
	if _, err := service.CreateRevision(ctx, entry.ID, as,
		catalog.Submission{Exchange: readFixture(t, "book-starter-kit.txt")}); err != nil {
		t.Fatalf("create book revision: %v", err)
	}

	steps, err := service.ListHistory(ctx, entry.ID, owner)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(steps) != 2 || steps[0].VersionNumber != 1 || steps[1].VersionNumber != 2 ||
		steps[0].BlueprintID == steps[1].BlueprintID {
		t.Fatalf("steps = %+v, want the two blueprint revisions and not the book", steps)
	}
}
