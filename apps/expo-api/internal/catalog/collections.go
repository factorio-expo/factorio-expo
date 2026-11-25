package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/internal/shortid"
)

// Collection is a user-curated, ordered set of saved entries. Unlike an
// entry, a collection always belongs to a logged-in user; there is no
// anonymous equivalent.
type Collection struct {
	ID          string    `json:"id" doc:"The collection's id."`
	UserID      string    `json:"user_id" doc:"The owner's account id."`
	Name        string    `json:"name" doc:"The collection's name."`
	Description string    `json:"description" doc:"The collection's description."`
	Visibility  string    `json:"visibility" doc:"Who may see the collection."`
	CreatedAt   time.Time `json:"created_at" doc:"When the collection was created."`
	// OwnerUsername is only populated by GetCollection, for public share pages.
	OwnerUsername string `json:"owner_username,omitempty" doc:"The owner's username; only filled when reading a public collection."`
	// Entries is only populated by GetCollection; list/create/update responses
	// omit it so a collections index costs one flat query.
	Entries []EntrySummary `json:"entries,omitempty" doc:"The entries; only filled by reading one collection."`
	// EntryCount and CoverURLs are set by the listings (collection cards):
	// the entries the viewer may see, and the thumbnails of the first four
	// with a preview (no spoilers).
	EntryCount int64    `json:"entry_count" doc:"How many entries it has (set by the listings)."`
	CoverURLs  []string `json:"cover_urls,omitempty" doc:"Thumbnail URLs of its first entries (set by the listings)."`
}

// withSummaries fills each collection's EntryCount and CoverURLs as
// viewerUserID ("" for a signed-out viewer) sees them.
func (s *Service) withSummaries(ctx context.Context, collections []Collection, viewerUserID string) error {
	if len(collections) == 0 {
		return nil
	}
	ids := make([]string, 0, len(collections))
	for _, collection := range collections {
		ids = append(ids, collection.ID)
	}
	rows, err := s.queries.ListCollectionSummaries(ctx, db.ListCollectionSummariesParams{
		Ids: ids, ViewerUserID: pgText(viewerUserID),
	})
	if err != nil {
		return fmt.Errorf("list collection summaries: %w", err)
	}
	byID := make(map[string]db.ListCollectionSummariesRow, len(rows))
	for _, row := range rows {
		byID[row.CollectionID] = row
	}
	for i := range collections {
		row := byID[collections[i].ID]
		collections[i].EntryCount = row.EntryCount
		for _, blueprintID := range row.CoverBlueprintIds {
			collections[i].CoverURLs = append(collections[i].CoverURLs, ThumbnailURL(blueprintID))
		}
	}
	return nil
}

// CreateCollection creates a new collection owned by userID.
func (s *Service) CreateCollection(
	ctx context.Context, userID, name, description, visibility string,
) (Collection, error) {
	user, err := parseID(userID)
	if err != nil {
		return Collection{}, err
	}
	name, description, visibility, err = validateCollectionFields(name, description, visibility)
	if err != nil {
		return Collection{}, err
	}

	row, err := s.queries.CreateCollection(ctx, db.CreateCollectionParams{
		ID: shortid.New(), UserID: user, Name: name, Description: description, Visibility: visibility,
	})
	if err != nil {
		return Collection{}, fmt.Errorf("create collection: %w", err)
	}
	return collectionFromRow(row), nil
}

// ListCollections returns every collection owned by a user, newest first,
// without hydrating each collection's entries.
func (s *Service) ListCollections(ctx context.Context, userID string) ([]Collection, error) {
	user, err := parseID(userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListCollectionsByUser(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	collections := make([]Collection, 0, len(rows))
	for _, row := range rows {
		collections = append(collections, collectionFromRow(row))
	}
	if err := s.withSummaries(ctx, collections, user); err != nil {
		return nil, err
	}
	return collections, nil
}

// CollectionMembership is one of the caller's collections and whether it
// holds a given entry: what the "Add to collection" picker shows.
type CollectionMembership struct {
	Collection
	Contains bool `json:"contains" doc:"Whether the entry is in the collection."`
}

// ListCollectionsForEntry returns every collection owned by userID, newest
// first, each flagged with whether it holds entryID. ErrNotFound when the
// entry does not exist or the user may not see it.
func (s *Service) ListCollectionsForEntry(ctx context.Context, userID, entryID string) ([]CollectionMembership, error) {
	user, err := parseID(userID)
	if err != nil {
		return nil, err
	}
	entry, err := s.entries().Viewable(ctx, entryID, userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListCollectionMembershipByUser(ctx, db.ListCollectionMembershipByUserParams{
		UserID: user, EntryID: entry.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("list collection membership: %w", err)
	}
	memberships := make([]CollectionMembership, 0, len(rows))
	for _, row := range rows {
		memberships = append(memberships, CollectionMembership{
			Collection: collectionFromRow(row.Collection), Contains: row.Contains,
		})
	}
	return memberships, nil
}

// GetCollection returns one collection hydrated with its entries. A private
// collection is only visible to its owner; viewerUserID may be empty for an
// anonymous caller. Unauthorized access is reported as ErrNotFound rather
// than ErrForbidden so a private collection's existence is not leaked.
func (s *Service) GetCollection(ctx context.Context, collectionID, viewerUserID string) (Collection, error) {
	row, err := s.loadCollection(ctx, collectionID)
	if err != nil {
		return Collection{}, err
	}
	if row.Visibility != "public" && row.UserID != viewerUserID {
		return Collection{}, ErrNotFound
	}

	// Entries the viewer may not see (deleted, or someone else's private
	// entry) are hidden so a shared collection cannot leak them.
	ids, err := s.queries.ListCollectionEntryIDs(ctx, db.ListCollectionEntryIDsParams{
		CollectionID: row.ID, ViewerUserID: pgText(viewerUserID),
	})
	if err != nil {
		return Collection{}, fmt.Errorf("list collection entries: %w", err)
	}
	collection := collectionFromRow(row)
	owner, err := s.queries.GetUserByID(ctx, row.UserID)
	if err != nil {
		return Collection{}, fmt.Errorf("read collection owner: %w", err)
	}
	collection.OwnerUsername = owner.Username
	if collection.Entries, err = s.entryCards(ctx, ids, viewerUserID); err != nil {
		return Collection{}, err
	}
	return collection, nil
}

// UpdateCollection replaces a collection's name, description, and visibility.
// Only the owner may update it.
func (s *Service) UpdateCollection(
	ctx context.Context, collectionID, userID, name, description, visibility string,
) (Collection, error) {
	existing, err := s.loadCollection(ctx, collectionID)
	if err != nil {
		return Collection{}, err
	}
	if existing.UserID != userID {
		return Collection{}, ErrForbidden
	}
	name, description, visibility, err = validateCollectionFields(name, description, visibility)
	if err != nil {
		return Collection{}, err
	}

	row, err := s.queries.UpdateCollection(ctx, db.UpdateCollectionParams{
		ID: existing.ID, Name: name, Description: description, Visibility: visibility,
	})
	if err != nil {
		return Collection{}, fmt.Errorf("update collection: %w", err)
	}
	return collectionFromRow(row), nil
}

// DeleteCollection removes a collection and, by cascade, its saved-entry
// rows. Only the owner may delete it.
func (s *Service) DeleteCollection(ctx context.Context, collectionID, userID string) error {
	existing, err := s.loadCollection(ctx, collectionID)
	if err != nil {
		return err
	}
	if existing.UserID != userID {
		return ErrForbidden
	}
	affected, err := s.queries.DeleteCollection(ctx, existing.ID)
	if err != nil {
		return fmt.Errorf("delete collection: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// AddCollectionEntry saves an entry into a collection at the given position.
// Re-adding an entry already present just moves it to the new position. Only
// the owner may modify a collection.
func (s *Service) AddCollectionEntry(
	ctx context.Context, collectionID, userID, entryID string, position int32,
) error {
	collection, err := s.loadCollection(ctx, collectionID)
	if err != nil {
		return err
	}
	if collection.UserID != userID {
		return ErrForbidden
	}
	return s.inTransaction(ctx, func(queries *db.Queries) error {
		row, err := (entryAccess{queries: queries}).Collectable(ctx, entryID, userID)
		if err != nil {
			return err
		}
		if _, err := queries.AddCollectionEntry(ctx, db.AddCollectionEntryParams{
			CollectionID: collection.ID, EntryID: row.ID, Position: position,
		}); err != nil {
			return fmt.Errorf("add collection entry: %w", err)
		}
		_, err = recordXP(ctx, queries, xpReceived{reason: xpCollection, entry: row, byUserID: collection.UserID})
		return err
	})
}

// ReorderCollectionEntries stores the owner's drag-and-drop order: entryIDs in
// the order to show. Members left out keep their relative place after the
// listed ones; ids that are not members are ignored (never added).
func (s *Service) ReorderCollectionEntries(
	ctx context.Context, collectionID, userID string, entryIDs []string,
) error {
	collection, err := s.loadCollection(ctx, collectionID)
	if err != nil {
		return err
	}
	if collection.UserID != userID {
		return ErrForbidden
	}
	return s.inTransaction(ctx, func(queries *db.Queries) error {
		for i, entryID := range entryIDs {
			entry, err := parseID(entryID)
			if err != nil {
				return err
			}
			if _, err := queries.SetCollectionEntryPosition(ctx, db.SetCollectionEntryPositionParams{
				CollectionID: collection.ID, EntryID: entry, Position: int32(i),
			}); err != nil {
				return fmt.Errorf("reorder collection entries: %w", err)
			}
		}
		return nil
	})
}

// RemoveCollectionEntry removes an entry from a collection. Only the owner
// may modify a collection.
func (s *Service) RemoveCollectionEntry(ctx context.Context, collectionID, userID, entryID string) error {
	collection, err := s.loadCollection(ctx, collectionID)
	if err != nil {
		return err
	}
	if collection.UserID != userID {
		return ErrForbidden
	}
	entry, err := parseID(entryID)
	if err != nil {
		return err
	}
	if _, err := s.queries.RemoveCollectionEntry(ctx, db.RemoveCollectionEntryParams{
		CollectionID: collection.ID, EntryID: entry,
	}); err != nil {
		return fmt.Errorf("remove collection entry: %w", err)
	}
	return nil
}

// loadCollection fetches the raw collection row, translating a missing row
// into ErrNotFound.
func (s *Service) loadCollection(ctx context.Context, collectionID string) (db.Collection, error) {
	id, err := parseID(collectionID)
	if err != nil {
		return db.Collection{}, err
	}
	collection, err := s.queries.GetCollection(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Collection{}, ErrNotFound
		}
		return db.Collection{}, fmt.Errorf("read collection: %w", err)
	}
	return collection, nil
}

func collectionFromRow(row db.Collection) Collection {
	return Collection{
		ID:          row.ID,
		UserID:      row.UserID,
		Name:        row.Name,
		Description: row.Description,
		Visibility:  row.Visibility,
		CreatedAt:   row.CreatedAt.Time,
	}
}

// ErrInvalidCollection is returned when a collection's name or visibility
// fails validation.
var ErrInvalidCollection = fault.New(fault.Invalid, "invalid_collection", fmt.Sprintf(
	"name is required (at most %d characters), description is at most %d characters, visibility must be public or private",
	maxCollectionName, maxCollectionDescription))

const (
	maxCollectionName        = 100
	maxCollectionDescription = 2000
)

// validateCollectionFields trims and validates a collection's name and
// visibility, defaulting visibility to "private" when omitted.
// Lengths mirror the collections table CHECKs.
func validateCollectionFields(name, description, visibility string) (string, string, string, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" || utf8.RuneCountInString(name) > maxCollectionName ||
		utf8.RuneCountInString(description) > maxCollectionDescription {
		return "", "", "", ErrInvalidCollection
	}
	if visibility == "" {
		visibility = "private"
	}
	if visibility != "public" && visibility != "private" {
		return "", "", "", ErrInvalidCollection
	}
	return name, description, visibility, nil
}
