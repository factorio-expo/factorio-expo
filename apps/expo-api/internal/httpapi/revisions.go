package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// revisionSubmissionInput is a new revision of an entry: its exchange string,
// with the edit token an anonymous entry's uploader presents.
type revisionSubmissionInput struct {
	EntryPath
	EditTokenInput
	Body revisionRequest
}

// revisionRequest is a new revision: the exchange string and an optional note.
type revisionRequest struct {
	Blueprint      string `json:"blueprint" minLength:"1" doc:"The exchange string of the new revision, as the game exports it."`
	Note           string `json:"note,omitempty" doc:"What changed, at most 280 characters of plain text; shown in the revision timeline and the owner's feed."`
	TurnstileToken string `json:"turnstile_token,omitempty" doc:"The human check's token, required when anonymous."`
}

// createRevision adds a new immutable revision to an existing entry and makes
// it the live one; the catalog wakes the dispatcher and the entry's streams.
func (s *server) createRevision(ctx context.Context, in *revisionSubmissionInput) (*entryOutput, error) {
	entry, err := s.catalog.CreateRevision(
		ctx, in.EntryID, catalog.Authorization{UserID: viewerFrom(ctx), EditToken: in.editToken()},
		catalog.Submission{Exchange: in.Body.Blueprint, Note: in.Body.Note, IPHash: s.ipHash(requestFrom(ctx))},
	)
	if err != nil {
		return nil, err
	}
	return &entryOutput{Body: entry}, nil
}

// RevisionPath is the revision of an entry an operation is about.
type RevisionPath struct {
	EntryPath
	RevisionID string `path:"revisionID" doc:"The revision's public id."`
}

// revisionListOutput is an entry's revisions, oldest first.
type revisionListOutput struct {
	Body revisionList
}

type revisionList struct {
	Revisions []catalog.Revision `json:"revisions" doc:"The entry's revisions, each with its tree."`
}

func (s *server) listRevisions(ctx context.Context, in *EntryPath) (*revisionListOutput, error) {
	revisions, err := s.catalog.ListRevisions(ctx, in.EntryID, viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &revisionListOutput{Body: revisionList{Revisions: revisions}}, nil
}

// historyOutput is the single-blueprint revisions of an entry.
type historyOutput struct {
	Body historyList
}

type historyList struct {
	Steps []catalog.HistoryStep `json:"steps" doc:"The revisions of a single-blueprint entry, oldest first, with their renders."`
}

// listHistory answers the single-blueprint revisions with their renders, the
// steps of the preview viewer's timeline.
func (s *server) listHistory(ctx context.Context, in *EntryPath) (*historyOutput, error) {
	steps, err := s.catalog.ListHistory(ctx, in.EntryID, viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &historyOutput{Body: historyList{Steps: steps}}, nil
}

// revisionOutput is one revision of an entry.
type revisionOutput struct {
	Body catalog.Revision
}

func (s *server) getRevision(ctx context.Context, in *RevisionPath) (*revisionOutput, error) {
	revision, err := s.catalog.GetRevision(ctx, in.EntryID, in.RevisionID, viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &revisionOutput{Body: revision}, nil
}

// exportRevision returns the exchange string of a revision's root, re-encoding
// books from their stored leaves.
func (s *server) exportRevision(ctx context.Context, in *RevisionPath) (*exchangeStringOutput, error) {
	exchange, err := s.catalog.ExportRevisionString(ctx, in.EntryID, in.RevisionID, viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &exchangeStringOutput{Body: ExchangeString{ExchangeString: exchange}}, nil
}

// NodePath is the book, blueprint or planner an operation is about.
type NodePath struct {
	NodeID string `path:"nodeID" doc:"The node's id: a book, blueprint or planner of a revision's tree."`
}

// exportNode returns the exchange string of one book, blueprint or planner.
func (s *server) exportNode(ctx context.Context, in *NodePath) (*exchangeStringOutput, error) {
	exchange, err := s.catalog.ExportNodeString(ctx, in.NodeID, viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &exchangeStringOutput{Body: ExchangeString{ExchangeString: exchange}}, nil
}

// deleteRevisionInput is a revision to delete, with the edit token an
// anonymous entry's uploader presents.
type deleteRevisionInput struct {
	EntryPath
	RevisionID string `path:"revisionID" doc:"The revision's public id."`
	EditTokenInput
}

// deleteRevisionBody answers the deletion of the live revision: the revision
// promoted in its place.
type deleteRevisionBody struct {
	CurrentRevision *catalog.Revision `json:"current_revision" doc:"The revision promoted to live in place of the deleted one."`
}

// deleteRevision removes one revision; the response carries the revision that
// was promoted in its place when the live revision was the one deleted.
//
// The answer is 204 with no body, or 200 with the promoted revision. A Huma
// output cannot leave out its body, so the operation streams its answer.
func (s *server) deleteRevision(ctx context.Context, in *deleteRevisionInput) (*huma.StreamResponse, error) {
	promoted, err := s.catalog.DeleteRevision(
		ctx, in.EntryID, in.RevisionID, catalog.Authorization{UserID: viewerFrom(ctx), EditToken: in.editToken()},
	)
	if err != nil {
		return nil, err
	}
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		if promoted == nil {
			hctx.SetStatus(http.StatusNoContent)
			return
		}
		hctx.SetHeader("Content-Type", "application/json")
		// The status is 200 by default; a write error here means the client
		// went away, and there is nothing left to answer.
		_ = json.NewEncoder(hctx.BodyWriter()).Encode(deleteRevisionBody{CurrentRevision: promoted})
	}}, nil
}

func (s *server) registerRevisions(api huma.API) {
	registerHumanChecked(api, s, s.operation(huma.Operation{
		OperationID: "createRevision", Method: http.MethodPost, Path: "/entries/{entryID}/revisions",
		Tags:          []string{"Revisions"},
		Summary:       "Add a revision to an entry and make it the live one",
		Description:   "The owner, or anyone with the edit token of an anonymous entry.",
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  maxSubmissionBytes,
		Errors: []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound,
			http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, accessOptional, limitUpload, s.uploadsGate()), turnstileActionRevision, s.createRevision)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listRevisions", Method: http.MethodGet, Path: "/entries/{entryID}/revisions",
		Tags:        []string{"Revisions"},
		Summary:     "The revisions of an entry",
		Description: "Every revision of the entry, oldest first, each with its tree.",
		Errors:      []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.listRevisions)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listHistory", Method: http.MethodGet, Path: "/entries/{entryID}/history",
		Tags:        []string{"Revisions"},
		Summary:     "The revision timeline of a single-blueprint entry",
		Description: "The revisions of an entry that is one blueprint each, with their renders. A book has no timeline.",
		Errors:      []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.listHistory)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getRevision", Method: http.MethodGet, Path: "/entries/{entryID}/revisions/{revisionID}",
		Tags:        []string{"Revisions"},
		Summary:     "One revision of an entry",
		Description: "The revision's tree with its exchange strings and preview state.",
		Errors:      []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.getRevision)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "exportRevision", Method: http.MethodGet,
		Path:        "/entries/{entryID}/revisions/{revisionID}/export",
		Tags:        []string{"Revisions"},
		Summary:     "The exchange string of a revision's root",
		Description: "Books are re-encoded from their stored leaves.",
		Errors:      []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.exportRevision)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "exportNode", Method: http.MethodGet, Path: "/nodes/{nodeID}/export",
		Tags:        []string{"Entries"},
		Summary:     "The exchange string of one book, blueprint or planner",
		Description: "Books are re-encoded from their stored leaves.",
		Errors:      []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.exportNode)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteRevision", Method: http.MethodDelete,
		Path:        "/entries/{entryID}/revisions/{revisionID}",
		Tags:        []string{"Revisions"},
		Summary:     "Delete a revision",
		Description: "Deleting the live revision promotes the most recent remaining one; the last revision cannot be deleted.",
		Responses: map[string]*huma.Response{
			"200": {
				Description: "The live revision was deleted; the answer is the revision promoted in its place.",
				Content: map[string]*huma.MediaType{"application/json": {
					Schema: api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[deleteRevisionBody](), true, "deleteRevision"),
				}},
			},
			"204": {Description: "The revision was deleted; it was not the live one."},
		},
		Errors: []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}, accessOptional, limitWrite), s.deleteRevision)
}
