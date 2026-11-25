package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"factorio-expo/apps/expo-game-data/communitynames"
)

// communityNamesSet is the Typesense synonym set the blueprints collection
// uses: the hand-written community names (apps/expo-game-data/communitynames),
// so "red science" finds what "automation science pack" finds. It is loaded
// whenever the collections are created, so it is as disposable as the index.
const communityNamesSet = "community-names"

// synonymItem is one synonym of a set: a group of phrases that find the same
// results (Typesense's multi-way synonym).
type synonymItem struct {
	ID       string   `json:"id"`
	Synonyms []string `json:"synonyms"`
}

// communitySynonyms is the synonym set's items, one per named item.
func communitySynonyms() []synonymItem {
	entries := communitynames.Names()
	items := make([]synonymItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, synonymItem{ID: entry.Item, Synonyms: entry.Synonym()})
	}
	return items
}

// putSynonyms creates or replaces the community names synonym set; the
// collections reference it by name.
func (c *TypesenseClient) putSynonyms(ctx context.Context) error {
	body, err := json.Marshal(map[string]any{"items": communitySynonyms()})
	if err != nil {
		return err
	}
	status, respBody, err := c.do(ctx, http.MethodPut, "/synonym_sets/"+communityNamesSet, nil, body)
	if err != nil {
		return fmt.Errorf("put synonym set: %w", err)
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return fmt.Errorf("put synonym set: %s", describeError(status, respBody))
	}
	return nil
}
