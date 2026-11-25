package catalog

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/internal/shortid"
)

// Note bounds: the entry's editor pins them on a leaf's preview.
const (
	MaxNoteRunes     = 500
	MaxNoteLines     = 10
	MaxNotesPerLeaf  = 20
	notePinPrecision = 1e6
)

var (
	// ErrInvalidNote is returned for an empty or oversized note, or a position
	// outside the image.
	ErrInvalidNote = fault.New(fault.Invalid, "invalid_note", fmt.Sprintf(
		"a note must be 1-%d characters and at most %d lines, pinned inside the image", MaxNoteRunes, MaxNoteLines))
	// ErrNoteLimit is returned when the leaf already has MaxNotesPerLeaf notes.
	ErrNoteLimit = fault.New(fault.Conflict, "note_limit", fmt.Sprintf("a blueprint takes at most %d notes", MaxNotesPerLeaf))
	// ErrNoteRevision is returned when notes are changed on a leaf that is not
	// in the entry's live revision: older revisions keep theirs read-only.
	ErrNoteRevision = fault.New(fault.Conflict, "note_revision", "notes can only be changed on the live revision")
)

// Note is one pin on a leaf's preview. X and Y are fractions of the image.
type Note struct {
	ID string `json:"id" doc:"The note's id."`
	// Number is the pin's rank among the leaf's notes (1..N).
	Number    int32      `json:"number" doc:"The pin's rank among the leaf's notes (1 to N)."`
	X         float64    `json:"x" doc:"Horizontal position as a fraction of the preview image width (0 to 1)."`
	Y         float64    `json:"y" doc:"Vertical position as a fraction of the preview image height (0 to 1)."`
	Body      string     `json:"body" doc:"The note's text."`
	CreatedAt time.Time  `json:"created_at" doc:"When it was added."`
	EditedAt  *time.Time `json:"edited_at,omitempty" doc:"When it was last edited; absent if never."`
}

// NoteList is a leaf's notes and whether the viewer may change them.
type NoteList struct {
	Notes   []Note `json:"notes" doc:"The leaf's notes, in pin order."`
	CanEdit bool   `json:"can_edit" doc:"Whether the viewer may add, edit and remove notes."`
}

// NewNote is a note to pin.
type NewNote struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Body string  `json:"body"`
}

// ListNotes returns a leaf's notes to anyone who can see the entry.
func (s *Service) ListNotes(
	ctx context.Context, entryID, blueprintID string, auth Authorization,
) (NoteList, error) {
	entry, err := s.entries().Viewable(ctx, entryID, auth.UserID)
	if err != nil {
		return NoteList{}, err
	}
	leaf, err := s.noteLeaf(ctx, s.queries, entry, blueprintID)
	if err != nil {
		return NoteList{}, err
	}
	rows, err := s.queries.ListBlueprintNotes(ctx, leaf.ID)
	if err != nil {
		return NoteList{}, fmt.Errorf("list notes: %w", err)
	}
	list := NoteList{Notes: make([]Note, 0, len(rows))}
	for _, row := range rows {
		list.Notes = append(list.Notes, noteView(row))
	}
	// Only the live revision's leaves take notes, and never a quick preview.
	list.CanEdit = !entry.QuickPreview && leaf.RevisionID == entry.CurrentRevisionID.String &&
		authorize(entry, auth) == nil
	return list, nil
}

// CreateNote pins a note on a leaf of the live revision.
func (s *Service) CreateNote(
	ctx context.Context, entryID, blueprintID string, auth Authorization, input NewNote,
) (Note, error) {
	body, err := normalizedNoteBody(input.Body)
	if err != nil || !validPin(input.X) || !validPin(input.Y) {
		return Note{}, ErrInvalidNote
	}
	var note Note
	err = s.inTransaction(ctx, func(queries *db.Queries) error {
		_, leaf, err := s.editableLeaf(ctx, queries, entryID, blueprintID, auth)
		if err != nil {
			return err
		}
		count, err := queries.CountBlueprintNotes(ctx, leaf.ID)
		if err != nil {
			return fmt.Errorf("count notes: %w", err)
		}
		if count >= MaxNotesPerLeaf {
			return ErrNoteLimit
		}
		id, err := queries.CreateBlueprintNote(ctx, db.CreateBlueprintNoteParams{
			ID: shortid.New(), BlueprintID: leaf.ID,
			X: roundPin(input.X), Y: roundPin(input.Y), Body: body,
		})
		if err != nil {
			return fmt.Errorf("create note: %w", err)
		}
		note, err = noteByID(ctx, queries, leaf.ID, id)
		return err
	})
	return note, err
}

// NoteEdit is a change to a note: its text, its pin (X and Y together), or
// both. Absent fields stay as they are.
type NoteEdit struct {
	Body *string  `json:"body"`
	X    *float64 `json:"x"`
	Y    *float64 `json:"y"`
}

// EditNote changes the text and/or moves the pin of one note of a leaf in the
// live revision. Only a text change marks the note edited.
func (s *Service) EditNote(
	ctx context.Context, entryID, blueprintID, noteID string, auth Authorization, edit NoteEdit,
) (Note, error) {
	params := db.UpdateBlueprintNoteParams{}
	if edit.Body != nil {
		body, err := normalizedNoteBody(*edit.Body)
		if err != nil {
			return Note{}, err
		}
		params.Body = pgtype.Text{String: body, Valid: true}
	}
	if (edit.X == nil) != (edit.Y == nil) || (edit.Body == nil && edit.X == nil) {
		return Note{}, ErrInvalidNote
	}
	if edit.X != nil {
		if !validPin(*edit.X) || !validPin(*edit.Y) {
			return Note{}, ErrInvalidNote
		}
		x, y := roundPin(*edit.X), roundPin(*edit.Y)
		params.X = pgtype.Float8{Float64: x, Valid: true}
		params.Y = pgtype.Float8{Float64: y, Valid: true}
	}
	var note Note
	err := s.inTransaction(ctx, func(queries *db.Queries) error {
		_, leaf, err := s.editableLeaf(ctx, queries, entryID, blueprintID, auth)
		if err != nil {
			return err
		}
		id, err := parseID(noteID)
		if err != nil {
			return ErrNotFound
		}
		params.ID, params.BlueprintID = id, leaf.ID
		changed, err := queries.UpdateBlueprintNote(ctx, params)
		if err != nil {
			return fmt.Errorf("update note: %w", err)
		}
		if changed == 0 {
			return ErrNotFound
		}
		note, err = noteByID(ctx, queries, leaf.ID, id)
		return err
	})
	return note, err
}

// DeleteNote removes one note of a leaf in the live revision.
func (s *Service) DeleteNote(
	ctx context.Context, entryID, blueprintID, noteID string, auth Authorization,
) error {
	return s.inTransaction(ctx, func(queries *db.Queries) error {
		_, leaf, err := s.editableLeaf(ctx, queries, entryID, blueprintID, auth)
		if err != nil {
			return err
		}
		id, err := parseID(noteID)
		if err != nil {
			return ErrNotFound
		}
		changed, err := queries.DeleteBlueprintNote(ctx, db.DeleteBlueprintNoteParams{
			ID: id, BlueprintID: leaf.ID,
		})
		if err != nil {
			return fmt.Errorf("delete note: %w", err)
		}
		if changed == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// editableLeaf locks an entry the caller may edit and finds the leaf in it,
// which must be in the live revision.
func (s *Service) editableLeaf(
	ctx context.Context, queries *db.Queries, entryID, blueprintID string, auth Authorization,
) (db.Entry, db.GetEntryBlueprintRow, error) {
	entry, err := (entryAccess{queries: queries}).Editable(ctx, entryID, auth)
	if err != nil {
		return db.Entry{}, db.GetEntryBlueprintRow{}, err
	}
	leaf, err := s.noteLeaf(ctx, queries, entry, blueprintID)
	if err != nil {
		return db.Entry{}, db.GetEntryBlueprintRow{}, err
	}
	if leaf.RevisionID != entry.CurrentRevisionID.String {
		return db.Entry{}, db.GetEntryBlueprintRow{}, ErrNoteRevision
	}
	return entry, leaf, nil
}

// noteLeaf finds a blueprint of a revision of the entry.
func (s *Service) noteLeaf(
	ctx context.Context, queries *db.Queries, entry db.Entry, blueprintID string,
) (db.GetEntryBlueprintRow, error) {
	id, err := parseID(blueprintID)
	if err != nil {
		return db.GetEntryBlueprintRow{}, ErrNotFound
	}
	row, err := queries.GetEntryBlueprint(ctx, db.GetEntryBlueprintParams{ID: entry.ID, ID_2: id})
	if err != nil {
		return db.GetEntryBlueprintRow{}, notFoundOr(err, "read note leaf")
	}
	return row, nil
}

func noteByID(ctx context.Context, queries *db.Queries, blueprintID, id string) (Note, error) {
	rows, err := queries.ListBlueprintNotes(ctx, blueprintID)
	if err != nil {
		return Note{}, fmt.Errorf("list notes: %w", err)
	}
	for _, row := range rows {
		if row.ID == id {
			return noteView(row), nil
		}
	}
	return Note{}, ErrNotFound
}

func noteView(row db.ListBlueprintNotesRow) Note {
	note := Note{
		ID: row.ID, Number: row.Number, X: row.X, Y: row.Y, Body: row.Body,
		CreatedAt: row.CreatedAt.Time,
	}
	if row.EditedAt.Valid {
		edited := row.EditedAt.Time
		note.EditedAt = &edited
	}
	return note
}

func validPin(v float64) bool { return v >= 0 && v <= 1 }

// roundPin keeps six decimals: far finer than any pixel.
func roundPin(v float64) float64 {
	return float64(int64(v*notePinPrecision+0.5)) / notePinPrecision
}

func normalizedNoteBody(body string) (string, error) {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if body == "" || !utf8.ValidString(body) || utf8.RuneCountInString(body) > MaxNoteRunes ||
		strings.Count(body, "\n") >= MaxNoteLines {
		return "", ErrInvalidNote
	}
	return body, nil
}
