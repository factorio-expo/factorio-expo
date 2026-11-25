package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// favoriteEntry marks an entry as favorited by the authenticated caller. It
// is idempotent: favoriting the same entry twice is not an error.
func (s *server) favoriteEntry(ctx context.Context, in *EntryPath) (*struct{}, error) {
	return nil, s.catalog.AddFavorite(ctx, viewerFrom(ctx), in.EntryID)
}

// favoriteStatus is GET /api/v1/entries/{entryID}/favorite.
type favoriteStatus struct {
	Favorited bool `json:"favorited" doc:"Whether the caller has favorited the entry."`
}

type favoriteStatusOutput struct {
	Body favoriteStatus
}

// getFavorite reports whether the authenticated caller has favorited an entry,
// for pages that show one entry without its full read.
func (s *server) getFavorite(ctx context.Context, in *EntryPath) (*favoriteStatusOutput, error) {
	favorited, err := s.catalog.IsFavorite(ctx, viewerFrom(ctx), in.EntryID)
	if err != nil {
		return nil, err
	}
	return &favoriteStatusOutput{Body: favoriteStatus{Favorited: favorited}}, nil
}

// unfavoriteEntry removes a favorite. It is idempotent: removing a favorite
// that was never set is not an error.
func (s *server) unfavoriteEntry(ctx context.Context, in *EntryPath) (*struct{}, error) {
	return nil, s.catalog.RemoveFavorite(ctx, viewerFrom(ctx), in.EntryID)
}

type favoritesInput struct {
	PageQuery
}

type favoriteList struct {
	Favorites []catalog.EntrySummary `json:"favorites" doc:"The favorited entries, as search cards, newest favorite first."`
	Total     int64                  `json:"total" doc:"How many entries the caller has favorited in all, for paging."`
}

type favoritesOutput struct {
	Body favoriteList
}

// listMyFavorites returns the authenticated caller's favorited entries, using
// the same flat card projection and pagination as the public entry listing.
func (s *server) listMyFavorites(ctx context.Context, in *favoritesInput) (*favoritesOutput, error) {
	favorites, total, err := s.catalog.ListFavorites(ctx, viewerFrom(ctx), int32(in.Limit), int32(in.Offset))
	if err != nil {
		return nil, err
	}
	return &favoritesOutput{Body: favoriteList{Favorites: favorites, Total: total}}, nil
}

func (s *server) registerFavorites(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "favoriteEntry", Method: http.MethodPost, Path: "/entries/{entryID}/favorite",
		Tags:        []string{"Favorites"},
		Summary:     "Favorite an entry",
		Description: "Idempotent: favoriting an entry twice is not an error.",
		Errors:      []int{http.StatusNotFound},
	}, accessRequired, limitWrite), s.favoriteEntry)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listMyFavorites", Method: http.MethodGet, Path: "/me/favorites",
		Tags:    []string{"Favorites"},
		Summary: "The caller's favorited entries",
	}, accessRequired, limitRead), s.listMyFavorites)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getFavorite", Method: http.MethodGet, Path: "/entries/{entryID}/favorite",
		Tags:    []string{"Favorites"},
		Summary: "Whether the caller favorited an entry",
		Errors:  []int{http.StatusNotFound},
	}, accessRequired, limitRead), s.getFavorite)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "unfavoriteEntry", Method: http.MethodDelete, Path: "/entries/{entryID}/favorite",
		Tags:        []string{"Favorites"},
		Summary:     "Unfavorite an entry",
		Description: "Idempotent: removing a favorite that was never set is not an error.",
		Errors:      []int{http.StatusNotFound},
	}, accessRequired, limitWrite), s.unfavoriteEntry)
}
