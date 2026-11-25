package catalog

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

// CompareLeaf is one side of a blueprint comparison: the leaf's identity, its
// entry's, a thumbnail and the analysis the compare table reads.
type CompareLeaf struct {
	ID            string      `json:"id" doc:"The blueprint's id."`
	EntryID       string      `json:"entry_id" doc:"The entry the blueprint is in."`
	EntryTitle    string      `json:"entry_title" doc:"The entry's title."`
	OwnerUsername string      `json:"owner_username,omitempty" doc:"The uploader's username; absent for an anonymous entry."`
	Label         string      `json:"label" doc:"The blueprint's label."`
	Description   string      `json:"description" doc:"The blueprint's description."`
	Icons         string      `json:"icons" doc:"The blueprint's icons."`
	GameVersion   string      `json:"game_version" doc:"The Factorio version that exported it."`
	Spoiler       bool        `json:"spoiler" doc:"Whether the entry is marked as a spoiler or NSFW: clients hide its preview."`
	EntityCount   int32       `json:"entity_count" doc:"How many entities the blueprint has."`
	Preview       *Preview    `json:"preview,omitempty" doc:"The render; absent until one is stored."`
	ModBadges     []string    `json:"mod_badges" doc:"The mods it needs, as search filter values."`
	Production    *Production `json:"production" doc:"The analysis of what it makes and uses."`
}

// CompareLeaf reads one blueprint for a comparison: only a blueprint of the
// current revision of a listed entry (public, not a quick preview) qualifies,
// whoever asks. Anything else is ErrNotFound.
func (s *Service) CompareLeaf(ctx context.Context, blueprintID string) (CompareLeaf, error) {
	id, err := parseID(blueprintID)
	if err != nil {
		return CompareLeaf{}, ErrNotFound
	}
	entryID, err := s.queries.GetEntryIDByBlueprint(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return CompareLeaf{}, ErrNotFound
	}
	if err != nil {
		return CompareLeaf{}, fmt.Errorf("resolve blueprint entry: %w", err)
	}
	entry, listed, err := s.ListedEntry(ctx, entryID)
	if err != nil {
		return CompareLeaf{}, err
	}
	if !listed || entry.Revision == nil || entry.Revision.Root == nil ||
		!slices.ContainsFunc(entry.Revision.Root.Leaves(), func(leaf Node) bool { return leaf.ID == id }) {
		return CompareLeaf{}, ErrNotFound
	}
	detail, err := s.GetLeafDetail(ctx, entryID, id, "", "")
	if err != nil {
		return CompareLeaf{}, err
	}
	leaf := CompareLeaf{
		ID: detail.ID, EntryID: entryID, EntryTitle: entry.Title, OwnerUsername: entry.OwnerUsername,
		Label: detail.Label, Description: detail.Description, Icons: detail.Icons, Spoiler: entry.Spoiler,
		GameVersion: detail.Revision.GameVersion, EntityCount: detail.EntityCount, Preview: detail.Preview,
		ModBadges: []string{}, Production: &detail.Production,
	}
	if detail.Mods != nil {
		leaf.ModBadges = append(leaf.ModBadges, detail.Mods.Badges()...)
	}
	return leaf, nil
}
