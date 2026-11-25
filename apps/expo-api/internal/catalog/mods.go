package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"factorio-expo/apps/expo-api/internal/blueprint"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/mods"
)

// Mod detection labels a blueprint with the mods its entity and recipe names
// need (internal/mods); it never rejects an upload and never changes what the
// blueprint is rendered or analysed against.

// detectMods matches entity and recipe stats against the name table.
func detectMods(table *mods.Table, entities []blueprint.EntityStat, recipes []blueprint.RecipeStat) mods.Detection {
	entityNames := make([]string, 0, len(entities))
	for _, entity := range entities {
		entityNames = append(entityNames, entity.Name)
	}
	recipeNames := make([]string, 0, len(recipes))
	for _, recipe := range recipes {
		recipeNames = append(recipeNames, recipe.Name)
	}
	return table.Detect(entityNames, recipeNames)
}

// Modsets lists the modsets the mod filter offers beyond the game's own
// mods, in table order.
func (s *Service) Modsets() []mods.Listed {
	return s.mods.Listed()
}

// redetectBatch is how many stored leaves one RedetectMods round updates.
const redetectBatch = 500

// RedetectMods re-detects every stored leaf whose detection was made against
// another name table (the API embeds a new one after expo-game-data reruns),
// and queues its entry for re-indexing, since search filters on it. It
// returns how many leaves it updated.
func (s *Service) RedetectMods(ctx context.Context) (int, error) {
	version := s.mods.Version()
	updated := 0
	for {
		var rows []db.ListStaleModDetectionsRow
		err := s.inChange(ctx, func(c *change) error {
			queries := c.queries
			var err error
			rows, err = queries.ListStaleModDetections(ctx, db.ListStaleModDetectionsParams{
				ModsVersion: version, RowLimit: redetectBatch,
			})
			if err != nil {
				return fmt.Errorf("list stale mod detections: %w", err)
			}
			entries := map[string]bool{}
			for _, row := range rows {
				if err := s.redetect(ctx, queries, row, version); err != nil {
					return err
				}
				entries[row.EntryID] = true
			}
			for entryID := range entries {
				if err := c.reindex(ctx, entryID); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return updated, err
		}
		updated += len(rows)
		if len(rows) < redetectBatch {
			if updated > 0 {
				slog.Info("re-detected mods", "leaves", updated, "table", version)
			}
			return updated, nil
		}
	}
}

func (s *Service) redetect(ctx context.Context, queries *db.Queries, row db.ListStaleModDetectionsRow, version string) error {
	entities, err := decodeEntityStats(row.Entities)
	if err != nil {
		return fmt.Errorf("decode entities of %s: %w", row.BlueprintID, err)
	}
	recipes, err := decodeRecipeStats(row.Recipes)
	if err != nil {
		return fmt.Errorf("decode recipes of %s: %w", row.BlueprintID, err)
	}
	encoded, err := json.Marshal(detectMods(s.mods, entities, recipes))
	if err != nil {
		return err
	}
	if err := queries.UpdateModDetection(ctx, db.UpdateModDetectionParams{
		BlueprintID: row.BlueprintID, Mods: encoded, ModsVersion: version,
	}); err != nil {
		return fmt.Errorf("store mod detection of %s: %w", row.BlueprintID, err)
	}
	return nil
}

// storedMods decodes a leaf's stored detection; nil when it has none yet.
func storedMods(stat db.BlueprintStat) (*mods.Detection, error) {
	if stat.ModsVersion == "" {
		return nil, nil
	}
	var detection mods.Detection
	if err := json.Unmarshal(stat.Mods, &detection); err != nil {
		return nil, err
	}
	return &detection, nil
}
