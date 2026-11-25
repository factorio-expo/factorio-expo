package catalog

import (
	"context"
	"fmt"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// AddFavorite marks an entry as favorited by a user. It is idempotent:
// favoriting an entry twice succeeds silently. ErrNotFound is returned when
// the entry does not exist.
func (s *Service) AddFavorite(ctx context.Context, userID, entryID string) error {
	user, entry, err := s.parseFavoriteIDs(userID, entryID)
	if err != nil {
		return err
	}
	return s.inTransaction(ctx, func(queries *db.Queries) error {
		row, err := (entryAccess{queries: queries}).Collectable(ctx, entryID, userID)
		if err != nil {
			return err
		}
		if _, err := queries.CreateFavorite(ctx, db.CreateFavoriteParams{
			UserID: user, EntryID: entry,
		}); err != nil {
			return fmt.Errorf("create favorite: %w", err)
		}
		_, err = recordXP(ctx, queries, xpReceived{reason: xpFavorite, entry: row, byUserID: user})
		return err
	})
}

// RemoveFavorite un-favorites an entry. It is idempotent: removing a favorite
// that does not exist succeeds silently.
func (s *Service) RemoveFavorite(ctx context.Context, userID, entryID string) error {
	user, entry, err := s.parseFavoriteIDs(userID, entryID)
	if err != nil {
		return err
	}
	if _, err := s.queries.DeleteFavorite(ctx, db.DeleteFavoriteParams{
		UserID: user, EntryID: entry,
	}); err != nil {
		return fmt.Errorf("delete favorite: %w", err)
	}
	return nil
}

// ListFavorites returns a user's favorited entries as cards, newest favorite
// first, and how many there are in all.
func (s *Service) ListFavorites(ctx context.Context, userID string, limit, offset int32) ([]EntrySummary, int64, error) {
	user, err := parseID(userID)
	if err != nil {
		return nil, 0, err
	}
	ids, err := s.queries.ListFavoriteEntryIDs(ctx, db.ListFavoriteEntryIDsParams{
		UserID: user, Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list favorites: %w", err)
	}
	total, err := s.queries.CountFavoriteEntries(ctx, user)
	if err != nil {
		return nil, 0, fmt.Errorf("count favorites: %w", err)
	}
	cards, err := s.entryCards(ctx, ids, userID)
	if err != nil {
		return nil, 0, err
	}
	return cards, total, nil
}

// IsFavorite reports whether a user has favorited an entry. ErrNotFound is
// returned when the entry does not exist or the user may not see it.
func (s *Service) IsFavorite(ctx context.Context, userID, entryID string) (bool, error) {
	_, entry, err := s.parseFavoriteIDs(userID, entryID)
	if err != nil {
		return false, err
	}
	if _, err := s.entries().Viewable(ctx, entryID, userID); err != nil {
		return false, err
	}
	favorited, err := favoritedEntryIDs(ctx, s.queries, userID, []string{entry})
	if err != nil {
		return false, err
	}
	return favorited[entry], nil
}

// favoritedEntryIDs reports which of ids viewerUserID has favorited, keyed by
// entry id. An anonymous viewer ("") has favorited nothing. One query for the
// whole listing, never one per row.
func favoritedEntryIDs(
	ctx context.Context, queries *db.Queries, viewerUserID string, ids []string,
) (map[string]bool, error) {
	if viewerUserID == "" || len(ids) == 0 {
		return map[string]bool{}, nil
	}
	viewer, err := parseID(viewerUserID)
	if err != nil {
		return nil, err
	}
	rows, err := queries.ListFavoritedEntryIDs(ctx, db.ListFavoritedEntryIDsParams{
		UserID: viewer, Ids: ids,
	})
	if err != nil {
		return nil, fmt.Errorf("list favorited entry ids: %w", err)
	}
	favorited := make(map[string]bool, len(rows))
	for _, id := range rows {
		favorited[id] = true
	}
	return favorited, nil
}

func (s *Service) parseFavoriteIDs(userID, entryID string) (string, string, error) {
	user, err := parseID(userID)
	if err != nil {
		return "", "", err
	}
	entry, err := parseID(entryID)
	if err != nil {
		return "", "", err
	}
	return user, entry, nil
}
