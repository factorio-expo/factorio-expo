package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// UserPath is the user (by username) an operation is about.
type UserPath struct {
	Username string `path:"username" doc:"The user's username, case-insensitive."`
}

// userEntriesInput pages a user's public entries.
type userEntriesInput struct {
	UserPath
	PageQuery
}

// userProfileOutput is a user's public profile header.
type userProfileOutput struct {
	Body catalog.PublicUser
}

// entryPage is one page of entry cards with the total for pagination.
type entryPage struct {
	Entries []catalog.EntrySummary `json:"entries" doc:"The entries on this page, as search cards, newest first."`
	Total   int64                  `json:"total" doc:"How many entries the listing holds in all."`
}

type entryPageOutput struct {
	Body entryPage
}

// userCollections is a user's public collections.
type userCollections struct {
	Collections []catalog.Collection `json:"collections" doc:"The user's public collections, newest first."`
}

type userCollectionsOutput struct {
	Body userCollections
}

// myEntriesInput pages the caller's own entries.
type myEntriesInput struct {
	PageQuery
}

// getUser is the public profile header: username and join date only.
func (s *server) getUser(ctx context.Context, in *UserPath) (*userProfileOutput, error) {
	user, err := s.catalog.GetPublicUser(ctx, in.Username)
	if err != nil {
		return nil, err
	}
	return &userProfileOutput{Body: user}, nil
}

// listUserEntries lists a user's public entries, paged like the other listings.
func (s *server) listUserEntries(ctx context.Context, in *userEntriesInput) (*entryPageOutput, error) {
	entries, total, err := s.catalog.ListUserEntries(ctx, in.Username, viewerFrom(ctx), int32(in.Limit), int32(in.Offset))
	if err != nil {
		return nil, err
	}
	return &entryPageOutput{Body: entryPage{Entries: entries, Total: total}}, nil
}

// listMyEntries lists the caller's own entries, public and private, paged like
// the other listings; each card carries its visibility.
func (s *server) listMyEntries(ctx context.Context, in *myEntriesInput) (*entryPageOutput, error) {
	entries, total, err := s.catalog.ListMyEntries(ctx, viewerFrom(ctx), int32(in.Limit), int32(in.Offset))
	if err != nil {
		return nil, err
	}
	return &entryPageOutput{Body: entryPage{Entries: entries, Total: total}}, nil
}

// listUserCollections lists a user's public collections.
func (s *server) listUserCollections(ctx context.Context, in *UserPath) (*userCollectionsOutput, error) {
	collections, err := s.catalog.ListUserPublicCollections(ctx, in.Username)
	if err != nil {
		return nil, err
	}
	return &userCollectionsOutput{Body: userCollections{Collections: collections}}, nil
}

// registerUsers registers this file's operations.
func (s *server) registerUsers(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getUser", Method: http.MethodGet, Path: "/users/{username}",
		Tags:        []string{"Users"},
		Summary:     "A user's public profile",
		Description: "The profile header: username, join date, status line, links and numbers over public entries.",
		Errors:      []int{http.StatusNotFound},
	}, accessPublic, limitRead), s.getUser)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listUserEntries", Method: http.MethodGet, Path: "/users/{username}/entries",
		Tags:        []string{"Users"},
		Summary:     "A user's public entries",
		Description: "Paged public entries, newest first. A signed-in viewer sees whether they favorited each one.",
		Errors:      []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.listUserEntries)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listUserCollections", Method: http.MethodGet, Path: "/users/{username}/collections",
		Tags:        []string{"Users"},
		Summary:     "A user's public collections",
		Description: "The user's public collections with their entry counts and covers.",
		Errors:      []int{http.StatusNotFound},
	}, accessPublic, limitRead), s.listUserCollections)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listMyEntries", Method: http.MethodGet, Path: "/me/entries",
		Tags:        []string{"Entries"},
		Summary:     "The caller's own entries",
		Description: "Paged entries the caller owns, public and private, newest first; each card carries its visibility.",
	}, accessRequired, limitRead), s.listMyEntries)
}
