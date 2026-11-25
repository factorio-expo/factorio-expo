package catalog

import (
	"context"
	"fmt"

	"factorio-expo/apps/expo-api/internal/blueprint"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// EntryCards batch-loads the card of each entry id (search results), keyed by
// entry id. viewerUserID may be empty for an anonymous caller. Only listed
// entries get a card, as only they are searchable: entries that are gone or no
// longer listed (the search index briefly lagging behind PostgreSQL, say right
// after a visibility change) are omitted rather than erroring.
func (s *Service) EntryCards(ctx context.Context, entryIDs []string, viewerUserID string) (map[string]EntrySummary, error) {
	ids := make([]string, 0, len(entryIDs))
	for _, entryID := range entryIDs {
		id, err := parseID(entryID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	cards, err := s.loadEntryCards(ctx, ids, viewerUserID, true)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]EntrySummary, len(cards))
	for _, card := range cards {
		byID[card.ID] = card
	}
	return byID, nil
}

// entryCards is the one way to build entry cards: every listing picks and
// orders its entry ids with its own scoping query, then hands them here. Cards
// keep the order of ids; entries that are gone or that viewerUserID may not
// see are left out. IsFavorited and IsPinned are the viewer's.
func (s *Service) entryCards(ctx context.Context, ids []string, viewerUserID string) ([]EntrySummary, error) {
	return s.loadEntryCards(ctx, ids, viewerUserID, false)
}

// loadEntryCards is entryCards, keeping only listed entries when listedOnly.
func (s *Service) loadEntryCards(
	ctx context.Context, ids []string, viewerUserID string, listedOnly bool,
) ([]EntrySummary, error) {
	if len(ids) == 0 {
		return []EntrySummary{}, nil
	}
	rows, err := s.queries.ListEntryCards(ctx, db.ListEntryCardsParams{Ids: ids, ViewerUserID: pgText(viewerUserID)})
	if err != nil {
		return nil, fmt.Errorf("list entry cards: %w", err)
	}
	favorited, err := favoritedEntryIDs(ctx, s.queries, viewerUserID, ids)
	if err != nil {
		return nil, err
	}
	pinned, err := pinnedEntryIDs(ctx, s.queries, viewerUserID, ids)
	if err != nil {
		return nil, err
	}
	covers, err := s.bookCovers(ctx, rows)
	if err != nil {
		return nil, err
	}
	// One read of the board leaders for the whole page, never one per card.
	records, err := readCurrentRecords(ctx, s.queries)
	if err != nil {
		return nil, err
	}
	cards := make([]EntrySummary, 0, len(rows))
	for _, row := range rows {
		if listedOnly && !row.Listed {
			continue
		}
		card := entryCard(row)
		card.CoverURLs = covers[card.ID]
		card.IsFavorited = favorited[card.ID]
		card.IsPinned = pinned[card.ID]
		card.Records = records.ofEntry(card.ID)
		cards = append(cards, card)
	}
	return cards, nil
}

// bookCovers is the collage of each book among rows: thumbnails of its first
// rendered leaves, only when there are at least two (one is the plain cover).
func (s *Service) bookCovers(ctx context.Context, rows []db.ListEntryCardsRow) (map[string][]string, error) {
	var ids []string
	for _, row := range rows {
		if row.RootKind == "book" {
			ids = append(ids, row.ID)
		}
	}
	covers := make(map[string][]string, len(ids))
	if len(ids) == 0 {
		return covers, nil
	}
	found, err := s.queries.ListBookCoverBlueprintIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list book covers: %w", err)
	}
	for _, row := range found {
		if len(row.BlueprintIds) < 2 {
			continue
		}
		for _, blueprintID := range row.BlueprintIds {
			covers[row.EntryID] = append(covers[row.EntryID], ThumbnailURL(blueprintID))
		}
	}
	return covers, nil
}

// entryCard shapes one ListEntryCards row, deriving the cover image link from
// the cover blueprint id (or, without one, whether a preview is on its way).
func entryCard(row db.ListEntryCardsRow) EntrySummary {
	card := EntrySummary{
		ID:            row.ID,
		Title:         row.Title,
		OwnerUserID:   row.OwnerUserID.String,
		OwnerUsername: row.OwnerUsername.String,
		Visibility:    Visibility(row.Visibility),
		Spoiler:       row.Spoiler,
		CreatedAt:     row.CreatedAt.Time,
		UpdatedAt:     row.UpdatedAt.Time,

		RevisionID:    row.RevisionID,
		VersionNumber: row.VersionNumber,
		GameVersion:   row.GameVersion,
		Compat21:      Compat21(row.Compat21),

		Kind:        blueprint.Kind(row.RootKind),
		Label:       row.RootLabel,
		Description: row.RootDescription,
		Icons:       row.RootIcons,

		LeafCount:      row.LeafCount,
		PreviewCount:   row.PreviewCount,
		FavoritesCount: row.FavoritesCount,
		ViewCount:      row.ViewCount,
	}
	if row.ExpiresAt.Valid {
		expiresAt := row.ExpiresAt.Time
		card.ExpiresAt = &expiresAt
	}
	if row.CoverBlueprintID != "" {
		card.PreviewURL = ThumbnailURL(row.CoverBlueprintID)
	} else {
		card.PreviewPending = row.PreviewPending
	}
	return card
}

// LeafPreview is a search card's leaf image: its thumbnail, or, without one,
// whether it is still rendering.
type LeafPreview struct {
	PreviewURL     string
	PreviewPending bool
}

// LeafPreviews returns the preview state of the given leaves, keyed by
// blueprint id. A leaf that is not in its entry's current revision is left
// out.
func (s *Service) LeafPreviews(ctx context.Context, blueprintIDs []string) (map[string]LeafPreview, error) {
	ids := make([]string, 0, len(blueprintIDs))
	for _, blueprintID := range blueprintIDs {
		id, err := parseID(blueprintID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	previews := make(map[string]LeafPreview, len(ids))
	if len(ids) == 0 {
		return previews, nil
	}
	rows, err := s.queries.ListLeafCardPreviews(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list leaf card previews: %w", err)
	}
	for _, row := range rows {
		preview := LeafPreview{PreviewPending: row.PreviewPending}
		if row.HasPreview {
			preview = LeafPreview{PreviewURL: ThumbnailURL(row.BlueprintID)}
		}
		previews[row.BlueprintID] = preview
	}
	return previews, nil
}
