package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
)

const (
	maxCommentRequestBytes = 32 << 10
	maxCommentReportBytes  = 8 << 10
)

// commentPageInput is one page of an entry's thread: `after` or `before` a
// number, and `limit`. Out-of-range values fall back to the page default
// (commentPageBound), as they always have.
type commentPageInput struct {
	EntryPath
	EditTokenInput
	After  int `query:"after" doc:"List the comments numbered after this one (oldest first); 0 starts at the first."`
	Before int `query:"before" doc:"List the comments numbered before this one; 0 when unused."`
	Limit  int `query:"limit" default:"50" doc:"How many comments to return, 1 to 100."`
}

type commentPageOutput struct {
	CacheControl string `header:"Cache-Control" doc:"Always no-store: the page depends on the viewer."`
	Body         catalog.CommentPage
}

// commentPageBound keeps a page parameter inside its range, or the fallback.
func commentPageBound(value, fallback, min, max int) int32 {
	if value < min || value > max {
		return int32(fallback)
	}
	return int32(value)
}

// listComments answers one page of an entry's thread (`after` or `before` a
// number, `limit`) together with what the viewer may do.
func (s *server) listComments(ctx context.Context, in *commentPageInput) (*commentPageOutput, error) {
	page, err := s.catalog.ListComments(
		ctx, in.EntryID, catalog.Authorization{UserID: viewerFrom(ctx), EditToken: in.editToken()},
		catalog.CommentPageRequest{
			After:  commentPageBound(in.After, 0, 0, 1<<30),
			Before: commentPageBound(in.Before, 0, 0, 1<<30),
			Limit:  commentPageBound(in.Limit, catalog.DefaultCommentPage, 1, catalog.MaxCommentPage),
		},
	)
	if err != nil {
		return nil, err
	}
	return &commentPageOutput{CacheControl: "no-store", Body: page}, nil
}

// commentCreateBody is the new comment. Body is optional on the wire so that
// an empty comment is refused by the catalog with its own code.
type commentCreateBody struct {
	Body       string `json:"body,omitempty" doc:"The comment's text (Factorio rich-text tags allowed)."`
	RevisionID string `json:"revision_id,omitempty" doc:"The revision the comment was written on; the live revision when absent."`
}

type commentCreateInput struct {
	EntryPath
	EditTokenInput
	Body commentCreateBody
}

type commentOutput struct {
	Body catalog.Comment
}

func (s *server) createComment(ctx context.Context, in *commentCreateInput) (*commentOutput, error) {
	comment, err := s.catalog.CreateComment(
		ctx, in.EntryID,
		catalog.Authorization{UserID: viewerFrom(ctx), EditToken: in.editToken()},
		s.ipHash(requestFrom(ctx)),
		catalog.NewComment{Body: in.Body.Body, RevisionID: in.Body.RevisionID},
	)
	if err != nil {
		return nil, err
	}
	return &commentOutput{Body: comment}, nil
}

// CommentNumberPath is the per-entry comment number (#N) of a comment
// operation. It is at least 1; a malformed one fails validation.
type CommentNumberPath struct {
	EntryPath
	Number int `path:"number" minimum:"1" maximum:"2147483647" doc:"The comment's number within the entry (#N)."`
}

// commentEditBody is the comment's new text. Body is optional on the wire so
// that an empty text is refused by the catalog with its own code.
type commentEditBody struct {
	Body string `json:"body,omitempty" doc:"The comment's new text."`
}

type commentEditInput struct {
	CommentNumberPath
	EditTokenInput
	Body commentEditBody
}

func (s *server) editComment(ctx context.Context, in *commentEditInput) (*commentOutput, error) {
	comment, err := s.catalog.EditComment(
		ctx, in.EntryID, int32(in.Number),
		catalog.Authorization{UserID: viewerFrom(ctx), EditToken: in.editToken()},
		s.ipHash(requestFrom(ctx)), in.Body.Body,
	)
	if err != nil {
		return nil, err
	}
	return &commentOutput{Body: comment}, nil
}

type commentRemoveInput struct {
	CommentNumberPath
	EditTokenInput
}

// removeComment leaves a "removed" tombstone; the author or the entry's
// owner (edit token for an anonymous entry) may do it.
func (s *server) removeComment(ctx context.Context, in *commentRemoveInput) (*struct{}, error) {
	auth := catalog.Authorization{UserID: viewerFrom(ctx), EditToken: in.editToken()}
	return nil, s.catalog.RemoveComment(ctx, in.EntryID, int32(in.Number), auth)
}

// commentReportBody is one report. Both fields are optional on the wire: the
// catalog validates the reason against its own list, with its own code.
type commentReportBody struct {
	Reason string `json:"reason,omitempty" doc:"Why: spam, abuse, off_topic or other."`
	Note   string `json:"note,omitempty" doc:"Anything the moderators should know."`
}

type commentReportInput struct {
	CommentNumberPath
	EditTokenInput
	Body commentReportBody
}

func (s *server) reportComment(ctx context.Context, in *commentReportInput) (*struct{}, error) {
	report := catalog.CommentReport{Reason: in.Body.Reason, Note: in.Body.Note}
	auth := catalog.Authorization{UserID: viewerFrom(ctx), EditToken: in.editToken()}
	return nil, s.catalog.ReportComment(ctx, in.EntryID, int32(in.Number), auth, s.ipHash(requestFrom(ctx)), report)
}

// registerComments registers this file's operations.
func (s *server) registerComments(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listComments", Method: http.MethodGet, Path: "/entries/{entryID}/comments",
		Tags:        []string{"Comments"},
		Summary:     "An entry's comment thread",
		Description: "One page of the thread (`after` or `before` a number, `limit`), with what the viewer may do.",
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, accessOptional, limitRead), s.listComments)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "createComment", Method: http.MethodPost, Path: "/entries/{entryID}/comments",
		Tags:          []string{"Comments"},
		Summary:       "Post a comment",
		Description:   "Signed-in users only, on public entries that take comments.",
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  maxCommentRequestBytes,
		Errors:        []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, accessRequired, limitComment, s.commentsGate()), s.createComment)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "editComment", Method: http.MethodPatch, Path: "/entries/{entryID}/comments/{number}",
		Tags:         []string{"Comments"},
		Summary:      "Edit a comment",
		Description:  "Only the author may edit their comment.",
		MaxBodyBytes: maxCommentRequestBytes,
		Errors:       []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, accessRequired, limitCommentEdit), s.editComment)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "removeComment", Method: http.MethodDelete, Path: "/entries/{entryID}/comments/{number}",
		Tags:        []string{"Comments"},
		Summary:     "Remove a comment",
		Description: "Leaves a removed tombstone. The author, the entry's owner, or the edit token holder of an anonymous entry may remove it.",
		Errors:      []int{http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, accessOptional, limitWrite), s.removeComment)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "reportComment", Method: http.MethodPost, Path: "/entries/{entryID}/comments/{number}/report",
		Tags:         []string{"Comments"},
		Summary:      "Report a comment",
		MaxBodyBytes: maxCommentReportBytes,
		Errors:       []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, accessRequired, limitReport), s.reportComment)
}
