package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// The profile's own routes (docs/api.md#profiles): the caller's status line
// and links, pins, and a user's pinned entries and comments.

const maxProfileRequestBytes = 8 << 10

// ProfileUserPath is the user (by username) a profile operation is about.
type ProfileUserPath struct {
	Username string `path:"username" doc:"The user's username, case-insensitive."`
}

// profileRequest is PATCH /api/v1/me/profile: a missing member leaves that part
// as it is; links replace the whole set.
type profileRequest struct {
	Status *string                    `json:"status,omitempty" doc:"The status line, one line of text. Omit to keep it."`
	Links  *[]catalog.SocialLinkInput `json:"links,omitempty" doc:"The whole set of social links. Omit to keep them."`
}

type profileOutput struct {
	Body catalog.PublicUser
}

// updateProfile changes the caller's status line and social links and
// answers the profile as it now reads.
func (s *server) updateProfile(ctx context.Context, in *profileInput) (*profileOutput, error) {
	user, err := s.catalog.UpdateProfile(ctx, viewerFrom(ctx), catalog.ProfileUpdate{
		Status: in.Body.Status, Links: in.Body.Links,
	})
	if err != nil {
		return nil, err
	}
	return &profileOutput{Body: user}, nil
}

type profileInput struct {
	Body profileRequest
}

// userPinsOutput lists a user's pinned entries in their order.
type userPinsOutput struct {
	Body userPins
}

type userPins struct {
	Entries []catalog.EntrySummary `json:"entries" doc:"The pinned entries, in the user's order; only those still listed."`
}

// listUserPins lists a user's pinned entries in their order.
func (s *server) listUserPins(ctx context.Context, in *ProfileUserPath) (*userPinsOutput, error) {
	entries, err := s.catalog.ListPinnedEntries(ctx, in.Username, viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &userPinsOutput{Body: userPins{Entries: entries}}, nil
}

// setPinsInput is the body of PUT /api/v1/me/pins.
type setPinsInput struct {
	Body setPinsRequest
}

type setPinsRequest struct {
	EntryIDs []string `json:"entry_ids,omitempty" doc:"The caller's pinned entries (public ids), in the order they should show. An empty list unpins everything."`
}

// setPins replaces the caller's pins with entry_ids, in that order.
func (s *server) setPins(ctx context.Context, in *setPinsInput) (*struct{}, error) {
	return nil, s.catalog.SetPins(ctx, viewerFrom(ctx), in.Body.EntryIDs)
}

// pinEntry pins one of the caller's public entries after their other pins;
// pinning it again changes nothing.
func (s *server) pinEntry(ctx context.Context, in *EntryPath) (*struct{}, error) {
	return nil, s.catalog.PinEntry(ctx, viewerFrom(ctx), in.EntryID)
}

// unpinEntry removes an entry from the caller's pins; idempotent.
func (s *server) unpinEntry(ctx context.Context, in *EntryPath) (*struct{}, error) {
	return nil, s.catalog.UnpinEntry(ctx, viewerFrom(ctx), in.EntryID)
}

// userCommentsInput pages a user's comments.
type userCommentsInput struct {
	ProfileUserPath
	PageQuery
}

// userCommentsOutput is one page of a user's comments and their total.
type userCommentsOutput struct {
	Body userComments
}

type userComments struct {
	Comments []catalog.ProfileComment `json:"comments" doc:"One page of the user's comments on listed entries, newest first."`
	Total    int64                    `json:"total" doc:"How many comments the user has on listed entries."`
}

// listUserComments lists a user's comments on public entries, newest first.
func (s *server) listUserComments(ctx context.Context, in *userCommentsInput) (*userCommentsOutput, error) {
	comments, total, err := s.catalog.ListUserComments(ctx, in.Username, int32(in.Limit), int32(in.Offset))
	if err != nil {
		return nil, err
	}
	return &userCommentsOutput{Body: userComments{Comments: comments, Total: total}}, nil
}

// registerProfile registers this file's operations.
func (s *server) registerProfile(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "pinEntry", Method: http.MethodPost, Path: "/entries/{entryID}/pin",
		Tags:        []string{"Profile"},
		Summary:     "Pin one of your entries",
		Description: "Pins one of the caller's public entries after their other pins; pinning it again changes nothing.",
		Errors:      []int{http.StatusNotFound, http.StatusConflict},
	}, accessRequired, limitWrite), s.pinEntry)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "unpinEntry", Method: http.MethodDelete, Path: "/entries/{entryID}/pin",
		Tags:        []string{"Profile"},
		Summary:     "Unpin an entry",
		Description: "Removes an entry from the caller's pins; idempotent.",
		Errors:      []int{http.StatusNotFound},
	}, accessRequired, limitWrite), s.unpinEntry)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "setPins", Method: http.MethodPut, Path: "/me/pins",
		Tags:         []string{"Profile"},
		Summary:      "Replace your pinned entries",
		Description:  "Pins, unpins and reorders in one: the caller's pins become entry_ids, in that order.",
		MaxBodyBytes: maxProfileRequestBytes,
		Errors:       []int{http.StatusConflict, http.StatusUnprocessableEntity},
	}, accessRequired, limitWrite), s.setPins)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "updateProfile", Method: http.MethodPatch, Path: "/me/profile",
		Tags:         []string{"Profile"},
		Summary:      "Update your profile",
		Description:  "Changes the caller's status line and social links and answers the profile as it now reads.",
		MaxBodyBytes: maxProfileRequestBytes,
		Errors:       []int{http.StatusUnprocessableEntity},
	}, accessRequired, limitWrite), s.updateProfile)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listUserPins", Method: http.MethodGet, Path: "/users/{username}/pins",
		Tags:    []string{"Users"},
		Summary: "A user's pinned entries",
		Errors:  []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.listUserPins)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listUserComments", Method: http.MethodGet, Path: "/users/{username}/comments",
		Tags:        []string{"Users"},
		Summary:     "A user's comments",
		Description: "Lists a user's comments on public entries, newest first.",
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, accessPublic, limitRead), s.listUserComments)
}
