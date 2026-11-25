package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
)

type userFeedOutput struct {
	Body catalog.UserFeed
}

// getUserFeed is the data of a user's Atom feed (served by expo-web at
// /u/{username}/feed.atom): public entries only, whoever asks.
func (s *server) getUserFeed(ctx context.Context, in *ProfileUserPath) (*userFeedOutput, error) {
	feed, err := s.catalog.UserFeed(ctx, in.Username)
	if err != nil {
		return nil, err
	}
	return &userFeedOutput{Body: feed}, nil
}

// registerFeed registers this file's operations.
func (s *server) registerFeed(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getUserFeed", Method: http.MethodGet, Path: "/users/{username}/feed",
		Tags:    []string{"Users"},
		Summary: "A user's recent publications and revisions",
		Description: "The last 30 revisions of the user's live, public entries, newest first; a revision " +
			"after the first carries its note. Unlisted, private, quick preview and anonymous entries never " +
			"appear. The site renders it as an Atom feed at /u/{username}/feed.atom.",
		Errors: []int{http.StatusNotFound},
	}, accessPublic, limitRead), s.getUserFeed)
}
