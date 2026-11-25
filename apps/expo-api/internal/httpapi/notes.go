package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// noteRequestBytes caps a note request body.
const noteRequestBytes = 8 << 10

// NotePath is one note of a blueprint of an entry's live revision.
type NotePath struct {
	EntryBlueprintPath
	NoteID string `path:"noteID" doc:"The note's id."`
}

// noteBody is a note to pin. Absent fields are left to the catalog, which
// rejects an empty body or a pin outside the image with its own code.
type noteBody struct {
	X    float64 `json:"x,omitempty" doc:"The pin's horizontal position, as a fraction of the image (0 to 1)."`
	Y    float64 `json:"y,omitempty" doc:"The pin's vertical position, as a fraction of the image (0 to 1)."`
	Body string  `json:"body,omitempty" doc:"The note's text: 1 to 500 characters, at most 10 lines."`
}

// noteEditBody changes a note's text, its pin (x and y together), or both.
// Absent fields stay as they are.
type noteEditBody struct {
	Body *string  `json:"body,omitempty" doc:"The note's new text, same limits as a new note."`
	X    *float64 `json:"x,omitempty" doc:"The pin's new horizontal position (with y)."`
	Y    *float64 `json:"y,omitempty" doc:"The pin's new vertical position (with x)."`
}

type noteListInput struct {
	EntryBlueprintPath
	EditTokenInput
}

type noteListOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         catalog.NoteList
}

type noteCreateInput struct {
	EntryBlueprintPath
	EditTokenInput
	Body noteBody
}

type noteEditInput struct {
	NotePath
	EditTokenInput
	Body noteEditBody
}

type noteDeleteInput struct {
	NotePath
	EditTokenInput
}

type noteOutput struct {
	Body catalog.Note
}

// listNotes answers a leaf's notes and whether the viewer may change them.
func (s *server) listNotes(ctx context.Context, in *noteListInput) (*noteListOutput, error) {
	auth := catalog.Authorization{UserID: viewerFrom(ctx), EditToken: in.editToken()}
	list, err := s.catalog.ListNotes(ctx, in.EntryID, in.BlueprintID, auth)
	if err != nil {
		return nil, err
	}
	return &noteListOutput{CacheControl: "no-store", Body: list}, nil
}

// createNote pins a note; the entry's owner (edit token for an anonymous
// entry) only.
func (s *server) createNote(ctx context.Context, in *noteCreateInput) (*noteOutput, error) {
	auth := catalog.Authorization{UserID: viewerFrom(ctx), EditToken: in.editToken()}
	note, err := s.catalog.CreateNote(ctx, in.EntryID, in.BlueprintID, auth, catalog.NewNote{
		X: in.Body.X, Y: in.Body.Y, Body: in.Body.Body,
	})
	if err != nil {
		return nil, err
	}
	return &noteOutput{Body: note}, nil
}

// editNote changes a note's text and/or pin.
func (s *server) editNote(ctx context.Context, in *noteEditInput) (*noteOutput, error) {
	auth := catalog.Authorization{UserID: viewerFrom(ctx), EditToken: in.editToken()}
	note, err := s.catalog.EditNote(ctx, in.EntryID, in.BlueprintID, in.NoteID, auth, catalog.NoteEdit{
		Body: in.Body.Body, X: in.Body.X, Y: in.Body.Y,
	})
	if err != nil {
		return nil, err
	}
	return &noteOutput{Body: note}, nil
}

// deleteNote removes a note.
func (s *server) deleteNote(ctx context.Context, in *noteDeleteInput) (*struct{}, error) {
	auth := catalog.Authorization{UserID: viewerFrom(ctx), EditToken: in.editToken()}
	return nil, s.catalog.DeleteNote(ctx, in.EntryID, in.BlueprintID, in.NoteID, auth)
}

// registerNotes registers this file's operations.
func (s *server) registerNotes(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listNotes", Method: http.MethodGet,
		Path:    "/entries/{entryID}/blueprints/{blueprintID}/notes",
		Tags:    []string{"Notes"},
		Summary: "List a leaf's notes",
		Description: "The notes pinned on a blueprint's preview, and whether the viewer may change them. " +
			"Anyone who can see the entry may read them.",
		Errors: []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.listNotes)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "createNote", Method: http.MethodPost,
		Path:    "/entries/{entryID}/blueprints/{blueprintID}/notes",
		Tags:    []string{"Notes"},
		Summary: "Pin a note on a blueprint",
		Description: "The entry's owner, or anyone with the edit token of an anonymous entry, on the " +
			"live revision only.",
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  noteRequestBytes,
		Errors: []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound,
			http.StatusConflict, http.StatusRequestEntityTooLarge},
	}, accessOptional, limitWrite), s.createNote)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "editNote", Method: http.MethodPatch,
		Path:    "/entries/{entryID}/blueprints/{blueprintID}/notes/{noteID}",
		Tags:    []string{"Notes"},
		Summary: "Change a note's text or pin",
		Description: "Absent fields keep their value. Only a text change marks the note edited. " +
			"The same people as creating a note may edit it.",
		MaxBodyBytes: noteRequestBytes,
		Errors: []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound,
			http.StatusConflict, http.StatusRequestEntityTooLarge},
	}, accessOptional, limitWrite), s.editNote)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteNote", Method: http.MethodDelete,
		Path:        "/entries/{entryID}/blueprints/{blueprintID}/notes/{noteID}",
		Tags:        []string{"Notes"},
		Summary:     "Delete a note",
		Description: "The same people as creating a note may delete it.",
		Errors:      []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}, accessOptional, limitWrite), s.deleteNote)
}
