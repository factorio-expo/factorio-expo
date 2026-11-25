package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// RawString is an exchange string served as-is by the raw endpoints. Only
// entries a signed-out visitor may see have one (never private ones), so the
// response may sit in shared caches until ExpiresAt.
type RawString struct {
	// SourceID names the immutable row the string came from (the revision or
	// the blueprint), so it can serve as the response's ETag.
	SourceID       string
	ExchangeString string
	// ExpiresAt is when the entry expires; nil when it lives until deleted.
	ExpiresAt *time.Time
}

// LiveRevisionRaw returns the exact submitted string of an entry's live
// revision.
func (s *Service) LiveRevisionRaw(ctx context.Context, entryID string) (RawString, error) {
	id, err := parseID(entryID)
	if err != nil {
		return RawString{}, err
	}
	row, err := s.queries.GetLiveRevisionRaw(ctx, id)
	return rawString(row.ID, row.OriginalString, row.ExpiresAt, err)
}

// RevisionRaw returns the exact submitted string of one of an entry's
// revisions.
func (s *Service) RevisionRaw(ctx context.Context, entryID, revisionID string) (RawString, error) {
	entry, err := parseID(entryID)
	if err != nil {
		return RawString{}, err
	}
	revision, err := parseID(revisionID)
	if err != nil {
		return RawString{}, err
	}
	row, err := s.queries.GetRevisionRaw(ctx, db.GetRevisionRawParams{RevisionID: revision, EntryID: entry})
	return rawString(revision, row.OriginalString, row.ExpiresAt, err)
}

// BlueprintRaw returns a leaf's self-contained exchange string.
func (s *Service) BlueprintRaw(ctx context.Context, blueprintID string) (RawString, error) {
	id, err := parseID(blueprintID)
	if err != nil {
		return RawString{}, err
	}
	row, err := s.queries.GetBlueprintRaw(ctx, id)
	return rawString(id, row.ExchangeString, row.ExpiresAt, err)
}

func rawString(sourceID, exchange string, expiresAt pgtype.Timestamptz, err error) (RawString, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return RawString{}, ErrNotFound
	}
	if err != nil {
		return RawString{}, fmt.Errorf("read raw string: %w", err)
	}
	out := RawString{SourceID: sourceID, ExchangeString: exchange}
	if expiresAt.Valid {
		out.ExpiresAt = &expiresAt.Time
	}
	return out, nil
}
