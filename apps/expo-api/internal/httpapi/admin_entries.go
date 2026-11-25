package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/staffops"
)

// Entries (catalog/staffentries.go): every entry whatever its state, one
// entry's page, restore, search sync, edge cache purge and phase re-runs.
// The staff delete is in admin_moderation.go.

// staffEntriesInput filters the Entries list; every parameter is optional.
type staffEntriesInput struct {
	Search     string `query:"q" doc:"Matches the id, part of the title or the owner's username."`
	State      string `query:"state" doc:"The state tab: all, live, deleted or quick_preview."`
	Visibility string `query:"visibility" doc:"public, unlisted or private."`
	Author     string `query:"author" doc:"anonymous or account."`
	Failed     bool   `query:"failed" doc:"Keeps entries with a blueprint whose preview or analysis failed for good."`
	Mods       string `query:"mods" doc:"vanilla (built-in mods only), space_age or modded (another mod, or unknown names)."`
	From       string `query:"from" doc:"The first creation day, inclusive (YYYY-MM-DD, UTC)."`
	To         string `query:"to" doc:"The last creation day, inclusive (YYYY-MM-DD, UTC)."`
	MinLeaves  int    `query:"min_leaves" doc:"Keeps entries with at least this many blueprints."`
	Cursor     string `query:"cursor" doc:"The next_cursor of the previous page."`
}

type staffEntriesOutput struct {
	Body staffops.StaffEntryPage
}

type staffEntryOutput struct {
	Body staffops.StaffEntryDetail
}

// listEntries lists entries newest first, whatever their state.
func (s *adminServer) listEntries(ctx context.Context, in *staffEntriesInput) (*staffEntriesOutput, error) {
	page, err := s.ops.ListStaffEntries(ctx, staffUser(ctx), staffops.StaffEntryQuery{
		Search: in.Search, State: in.State, Visibility: in.Visibility,
		Author: in.Author, Failed: in.Failed, Mods: in.Mods,
		CreatedFrom: in.From, CreatedTo: in.To, MinLeaves: in.MinLeaves,
		Cursor: in.Cursor,
	})
	if err != nil {
		return nil, err
	}
	return &staffEntriesOutput{Body: page}, nil
}

func (s *adminServer) getEntry(ctx context.Context, in *EntryPath) (*staffEntryOutput, error) {
	entry, err := s.ops.GetStaffEntry(ctx, staffUser(ctx), in.EntryID)
	if err != nil {
		return nil, err
	}
	return &staffEntryOutput{Body: entry}, nil
}

type revisionPath struct {
	EntryPath
	RevisionID string `path:"revisionID" doc:"The revision's id."`
}

// getRevisionString answers a revision's original exchange string as text.
func (s *adminServer) getRevisionString(ctx context.Context, in *revisionPath) (*huma.StreamResponse, error) {
	raw, err := s.ops.StaffRevisionString(ctx, staffUser(ctx), in.EntryID, in.RevisionID)
	if err != nil {
		return nil, err
	}
	return textStream(raw), nil
}

// staffEntryReasonInput is an entry write that takes only a reason.
type staffEntryReasonInput struct {
	EntryPath
	Body staffReason
}

func (s *adminServer) restoreEntry(ctx context.Context, in *staffEntryReasonInput) (*struct{}, error) {
	return nil, s.ops.StaffRestoreEntry(ctx, staffUser(ctx), in.EntryID, in.Body.Reason)
}

func (s *adminServer) reindexEntry(ctx context.Context, in *staffEntryReasonInput) (*struct{}, error) {
	return nil, s.ops.StaffReindexEntry(ctx, staffUser(ctx), in.EntryID, in.Body.Reason)
}

func (s *adminServer) purgeEntryCache(ctx context.Context, in *staffEntryReasonInput) (*struct{}, error) {
	return nil, s.ops.StaffPurgeEntryCache(ctx, staffUser(ctx), in.EntryID, in.Body.Reason)
}

// entryRerun is the body of an entry's phase re-run: one blueprint, or every
// one of the current revision when BlueprintID is empty.
type entryRerun struct {
	Phase       string `json:"phase,omitempty" doc:"The phase to run again: preview or analysis."`
	BlueprintID string `json:"blueprint_id,omitempty" doc:"One blueprint of the current revision; empty runs every one."`
	Reason      string `json:"reason,omitempty" doc:"Why, recorded in the audit log."`
}

type staffEntryRerunInput struct {
	EntryPath
	Body entryRerun
}

func (s *adminServer) rerunEntry(ctx context.Context, in *staffEntryRerunInput) (*struct{}, error) {
	return nil, s.jobs.RerunEntry(ctx, staffUser(ctx), in.EntryID, in.Body.Phase, in.Body.BlueprintID, in.Body.Reason)
}

func (s *adminServer) registerAdminEntries(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listAdminEntries", Method: http.MethodGet, Path: "/admin/entries",
		Tags:    []string{"Entries"},
		Summary: "List entries",
		Description: "Every entry whatever its state, newest first. Filters are optional; " +
			"the counts are for the state tabs.",
	}), s.listEntries)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getAdminEntry", Method: http.MethodGet, Path: "/admin/entries/{entryID}",
		Tags:    []string{"Entries"},
		Summary: "Get an entry",
		Description: "One entry's page: its revisions, blueprints, search sync, reports and comments, " +
			"whatever its state.",
		Errors: []int{http.StatusNotFound},
	}), s.getEntry)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getRevisionString", Method: http.MethodGet,
		Path:      "/admin/entries/{entryID}/revisions/{revisionID}/raw",
		Tags:      []string{"Entries"},
		Summary:   "A revision's original exchange string",
		Responses: downloadResponse("text/plain; charset=utf-8", "The exchange string as plain text."),
		Errors:    []int{http.StatusNotFound},
	}), s.getRevisionString)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "restoreEntry", Method: http.MethodPost, Path: "/admin/entries/{entryID}/restore",
		Tags:         []string{"Entries"},
		Summary:      "Restore a deleted entry",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusNotFound},
	}), s.restoreEntry)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "reindexEntry", Method: http.MethodPost, Path: "/admin/entries/{entryID}/reindex",
		Tags:         []string{"Entries"},
		Summary:      "Resync an entry's search document",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusNotFound},
	}), s.reindexEntry)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "purgeEntryCache", Method: http.MethodPost, Path: "/admin/entries/{entryID}/purge-cache",
		Tags:         []string{"Entries"},
		Summary:      "Purge an entry's edge cache",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusNotFound},
	}), s.purgeEntryCache)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "rerunEntry", Method: http.MethodPost, Path: "/admin/entries/{entryID}/rerun",
		Tags:         []string{"Entries"},
		Summary:      "Run an entry's preview or analysis again",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusNotFound, http.StatusConflict},
	}), s.rerunEntry)
}
