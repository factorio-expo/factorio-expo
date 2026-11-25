package catalog

import (
	"context"
	"time"

	"factorio-expo/apps/expo-api/internal/blueprint"
)

// SearchResultCard is one result card: a blueprint, a planner or a top-level
// book. Title, Description, Kind and Icons are the node's own (the entry's for
// a book or an entry's only node); BookTitle is set when the node is inside a
// book, whose entry the card links in its header. Matching facts stay in the
// search index. PreviewURL and PreviewPending (a blueprint's own, a book's
// cover; a planner has none), and FavoritesCount, ViewCount, IsFavorited,
// OwnerUsername and ExpiresAt (the entry's) are read live
// (NodeCards), since they change independently
// of the facts worth reindexing on.
type SearchResultCard struct {
	EntryID string `json:"entry_id" doc:"The entry's public id."`
	// NodeID is the blueprint's or planner's id; empty for a book.
	NodeID      string `json:"node_id,omitempty" doc:"The matching blueprint's, planner's or book's id."`
	Title       string `json:"title" doc:"The result's title."`
	Description string `json:"description" doc:"The result's description."`
	BookTitle   string `json:"book_title,omitempty" doc:"The book the result belongs to, for a blueprint or planner inside one."`
	PreviewURL  string `json:"preview_url,omitempty" doc:"The cover image URL (the first rendered blueprint's thumbnail); empty until one has rendered."`
	// CoverURLs is a book's collage (see catalog.EntrySummary.CoverURLs).
	CoverURLs []string `json:"cover_urls,omitempty" doc:"A book's collage: the thumbnail URLs of its first rendered blueprints (at most four); only for a book with two or more."`
	// PreviewPending: no image yet, but the preview is still rendering.
	PreviewPending bool   `json:"preview_pending,omitempty" doc:"True while there is no cover yet and a preview is still rendering."`
	GameVersion    string `json:"game_version" doc:"The Factorio version that exported it."`
	// Compat21 is the entry's Factorio 2.1 compatibility ("compatible" or
	// "incompatible" for a submission made before 2.1; empty otherwise).
	Compat21 Compat21 `json:"compat_21" doc:"The Factorio 2.1 compatibility of a submission made before 2.1; empty for one made in 2.1."`
	Tags     []string `json:"tags" doc:"The tag slugs."`
	// ModBadges are the mods the result needs, as search filter values
	// ("space-age", then guessed modsets scoring at least mods.BadgeMinScore).
	ModBadges []string `json:"mod_badges" doc:"The mods the blueprint needs, as filter values."`
	// Kind is blueprint, book, upgrade_planner or deconstruction_planner;
	// Icons are that node's.
	Kind  blueprint.Kind `json:"kind" doc:"What the result is: a book, blueprint or planner."`
	Icons string         `json:"icons" doc:"Signal icon tokens shown with it, like [item=iron-plate]."`
	// Spoiler is the entry's spoiler/NSFW flag (read live, like the owner).
	Spoiler bool `json:"spoiler" doc:"Whether the entry is marked as a spoiler or NSFW: clients hide its preview."`
	// Records are the leaderboard boards the result leads now, read live
	// from board_leaders (never indexed): a blueprint's own, a book's of any
	// of its blueprints; a planner has none.
	Records []BoardRecord `json:"records" doc:"The leaderboard boards the result leads now (a book: any of its blueprints), in board order."`

	OwnerUserID    string `json:"owner_user_id,omitempty" doc:"The uploader's account id; absent for an anonymous entry."`
	OwnerUsername  string `json:"owner_username,omitempty" doc:"The uploader's username; absent for an anonymous entry."`
	FavoritesCount int64  `json:"favorites_count" doc:"How many users favorited it."`
	ViewCount      int64  `json:"view_count" doc:"The all-time page views."`
	IsFavorited    bool   `json:"is_favorited" doc:"Whether the viewer favorited it; false when signed out."`

	CreatedAt time.Time  `json:"created_at" doc:"When the entry was created."`
	UpdatedAt time.Time  `json:"updated_at" doc:"When the entry was last changed."`
	ExpiresAt *time.Time `json:"expires_at,omitempty" doc:"When the entry expires, if it has a lifetime."`
}

// CardNode is a node to show as a card, with the facts its source holds on
// it: a search hit's from the index, a board place's or a home pick's from the
// catalog (LeafRef.CardNode). GameVersion, CreatedAt and UpdatedAt left zero
// take the entry's.
type CardNode struct {
	EntryID string
	// NodeID is the blueprint's or planner's id; empty for a book.
	NodeID      string
	Kind        blueprint.Kind
	Title       string
	Description string
	BookTitle   string
	Icons       string
	GameVersion string
	Tags        []string
	ModBadges   []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CardNode is the leaf as a card's node.
func (ref LeafRef) CardNode() CardNode {
	return CardNode{
		EntryID: ref.EntryID, NodeID: ref.BlueprintID, Kind: blueprint.KindBlueprint,
		Title: ref.Title, Description: ref.Description, BookTitle: ref.BookTitle, Icons: ref.Icons,
	}
}

// NodeCards builds the nodes' cards, in their order, with the live facts of
// their entries (EntryCards) and blueprints (LeafPreviews) as the viewer sees
// them. A node whose entry has been deleted, expired or stopped being listed,
// or a blueprint a newer revision replaced, has no card: whoever ranked or
// indexed the nodes never decides who may see an entry.
func (s *Service) NodeCards(ctx context.Context, nodes []CardNode, viewerUserID string) ([]SearchResultCard, error) {
	cards := make([]SearchResultCard, 0, len(nodes))
	if len(nodes) == 0 {
		return cards, nil
	}
	entryIDs := make([]string, 0, len(nodes))
	blueprintIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		entryIDs = append(entryIDs, node.EntryID)
		if node.Kind == blueprint.KindBlueprint {
			blueprintIDs = append(blueprintIDs, node.NodeID)
		}
	}
	entries, err := s.EntryCards(ctx, entryIDs, viewerUserID)
	if err != nil {
		return nil, err
	}
	previews, err := s.LeafPreviews(ctx, blueprintIDs)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		entry, ok := entries[node.EntryID]
		if !ok {
			continue
		}
		card := SearchResultCard{
			EntryID: node.EntryID, NodeID: node.NodeID, Kind: node.Kind,
			Title: node.Title, Description: node.Description, BookTitle: node.BookTitle, Icons: node.Icons,
			GameVersion: orElse(node.GameVersion, entry.GameVersion),
			Tags:        nonNil(node.Tags), ModBadges: nonNil(node.ModBadges),
			CreatedAt: timeOrElse(node.CreatedAt, entry.CreatedAt), UpdatedAt: timeOrElse(node.UpdatedAt, entry.UpdatedAt),
			Records: []BoardRecord{},
			Spoiler: entry.Spoiler, Compat21: entry.Compat21,
			OwnerUserID: entry.OwnerUserID, OwnerUsername: entry.OwnerUsername,
			FavoritesCount: entry.FavoritesCount, ViewCount: entry.ViewCount, IsFavorited: entry.IsFavorited,
			ExpiresAt: entry.ExpiresAt,
		}
		switch node.Kind {
		case blueprint.KindBlueprint:
			preview, ok := previews[node.NodeID]
			if !ok {
				continue
			}
			card.PreviewURL, card.PreviewPending = preview.PreviewURL, preview.PreviewPending
			card.Records = LeafRecords(entry.Records, node.NodeID)
		case blueprint.KindBook:
			card.PreviewURL, card.PreviewPending = entry.PreviewURL, entry.PreviewPending
			card.CoverURLs = entry.CoverURLs
			card.Records = entry.Records
		}
		cards = append(cards, card)
	}
	return cards, nil
}

func orElse(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func timeOrElse(value, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value
}

// nonNil keeps a JSON list an array, never null.
func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
