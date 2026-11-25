package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// PublicUser is what a profile page shows about someone: name, join date,
// the status line and links they set, and numbers over their public entries.
type PublicUser struct {
	UserID    string       `json:"user_id" doc:"The account's id."`
	Username  string       `json:"username" doc:"The account's username."`
	CreatedAt time.Time    `json:"created_at" doc:"When the account was made."`
	Status    string       `json:"status" doc:"The user's status line."`
	Links     []SocialLink `json:"links" doc:"The user's social links."`
	Stats     ProfileStats `json:"stats" doc:"The profile's counts."`
	Progress  UserProgress `json:"progress" doc:"The user's XP, level, badges and held records."`
}

// findUser looks a live user up by username, case-insensitively.
func (s *Service) findUser(ctx context.Context, username string) (db.User, error) {
	row, err := s.queries.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.User{}, ErrNotFound
		}
		return db.User{}, fmt.Errorf("read user: %w", err)
	}
	return row, nil
}

// GetPublicUser reads a user's profile by username, case-insensitively.
func (s *Service) GetPublicUser(ctx context.Context, username string) (PublicUser, error) {
	row, err := s.findUser(ctx, username)
	if err != nil {
		return PublicUser{}, err
	}
	links, err := ReadSocialLinks(ctx, s.queries, row.ID)
	if err != nil {
		return PublicUser{}, err
	}
	stats, err := readProfileStats(ctx, s.queries, row.ID)
	if err != nil {
		return PublicUser{}, err
	}
	progress, err := readUserProgress(ctx, s.queries, row.ID)
	if err != nil {
		return PublicUser{}, err
	}
	return PublicUser{
		UserID: row.ID, Username: row.Username, CreatedAt: row.CreatedAt.Time,
		Status: row.Status, Links: links, Stats: stats, Progress: progress,
	}, nil
}

// ListUserEntries returns a user's public, non-deleted entries (newest first)
// and the total count for pagination. IsFavorited is viewerUserID's ("" for
// anonymous viewers).
func (s *Service) ListUserEntries(
	ctx context.Context, username, viewerUserID string, limit, offset int32,
) ([]EntrySummary, int64, error) {
	user, err := s.findUser(ctx, username)
	if err != nil {
		return nil, 0, err
	}
	ids, err := s.queries.ListOwnerPublicEntryIDs(ctx, db.ListOwnerPublicEntryIDsParams{
		OwnerUserID: pgText(user.ID), Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list user entries: %w", err)
	}
	total, err := s.queries.CountOwnerPublicEntries(ctx, pgText(user.ID))
	if err != nil {
		return nil, 0, fmt.Errorf("count user entries: %w", err)
	}
	summaries, err := s.entryCards(ctx, ids, viewerUserID)
	if err != nil {
		return nil, 0, err
	}
	return summaries, total, nil
}

// ListMyEntries returns every live entry the user owns, public and private,
// newest first, with the total count for pagination. It is the owner's way back
// to a private entry: the public profile and search list public entries only.
func (s *Service) ListMyEntries(ctx context.Context, userID string, limit, offset int32) ([]EntrySummary, int64, error) {
	owner, err := parseID(userID)
	if err != nil {
		return nil, 0, err
	}
	ids, err := s.queries.ListOwnerEntryIDs(ctx, db.ListOwnerEntryIDsParams{
		OwnerUserID: pgText(owner), Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list my entries: %w", err)
	}
	total, err := s.queries.CountOwnerEntries(ctx, pgText(owner))
	if err != nil {
		return nil, 0, fmt.Errorf("count my entries: %w", err)
	}
	summaries, err := s.entryCards(ctx, ids, userID)
	if err != nil {
		return nil, 0, err
	}
	return summaries, total, nil
}

// ListChallengeCandidates returns the user's public single blueprints, newest
// first, whose title contains search: what they may enter in a challenge. The
// total counts every match, for paging.
func (s *Service) ListChallengeCandidates(
	ctx context.Context, userID, search string, limit, offset int32,
) ([]EntrySummary, int64, error) {
	owner, err := parseID(userID)
	if err != nil {
		return nil, 0, err
	}
	ids, err := s.queries.ListOwnerBlueprintEntryIDs(ctx, db.ListOwnerBlueprintEntryIDsParams{
		OwnerUserID: pgText(owner), Search: search, PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list challenge candidates: %w", err)
	}
	total, err := s.queries.CountOwnerBlueprintEntries(ctx, db.CountOwnerBlueprintEntriesParams{
		OwnerUserID: pgText(owner), Search: search,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("count challenge candidates: %w", err)
	}
	summaries, err := s.entryCards(ctx, ids, userID)
	if err != nil {
		return nil, 0, err
	}
	return summaries, total, nil
}

// ListUserPublicCollections returns a user's public collections, newest first,
// with their entry counts and covers but without hydrating their entries.
func (s *Service) ListUserPublicCollections(ctx context.Context, username string) ([]Collection, error) {
	user, err := s.findUser(ctx, username)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListPublicCollectionsByUser(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("list public collections: %w", err)
	}
	collections := make([]Collection, 0, len(rows))
	for _, row := range rows {
		collection := collectionFromRow(row)
		collection.OwnerUsername = user.Username
		collections = append(collections, collection)
	}
	// A public listing counts what a signed-out visitor sees.
	if err := s.withSummaries(ctx, collections, ""); err != nil {
		return nil, err
	}
	return collections, nil
}
