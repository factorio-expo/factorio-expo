package catalog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/internal/analysisresult"
)

// The catalog writes the preview dispatcher asks for (preview.Catalog): it
// reports results and failures, the catalog changes the entry, preview and
// production rows, and their search intents and stream notices follow from
// the same change (change.go).

// expiredSweepBatch is the LIMIT of DeleteExpiredPreviews and ExpireEntries.
const expiredSweepBatch = 500

// StorePreview references a finished preview's stored files (named by
// sourceSHA256) from the blueprint's previews row and queues the entry's
// search sync. A re-render of an evicted full file that produced a different
// image points the row at the new files. Files no row takes (the same preview
// was already stored, or the blueprint is gone) are queued for the file sweep
// instead, in the same transaction. The caller holds the files lock (preview.Files).
func (s *Service) StorePreview(ctx context.Context, blueprintID, sourceSHA256 string, width, height int) error {
	return s.inChange(ctx, func(c *change) error {
		inserted, err := c.queries.StorePreview(ctx, db.StorePreviewParams{
			BlueprintID: blueprintID, SourceSha256: sourceSHA256,
			Width: int32(width), Height: int32(height),
		})
		if err != nil {
			return fmt.Errorf("create preview: %w", err)
		}
		if inserted == 0 {
			if err := c.queries.QueuePreviewFileDeletion(ctx, sourceSHA256); err != nil {
				return fmt.Errorf("queue preview file deletion: %w", err)
			}
			return nil
		}
		// The preview count and cover are searchable facts.
		return reindexBlueprintEntry(ctx, c, blueprintID)
	})
}

// StoreProduction stores a leaf's analysed production snapshot and queues the
// entry's search sync: production and quality facts are searchable.
func (s *Service) StoreProduction(ctx context.Context, blueprintID string, production analysisresult.Stored) error {
	return s.inChange(ctx, func(c *change) error {
		if err := c.queries.UpsertProduction(ctx, db.UpsertProductionParams{
			BlueprintID: blueprintID, Production: production.Snapshot,
			ProductionSchemaVersion: pgtype.Int4{Int32: production.SchemaVersion, Valid: true},
			ProductionState:         pgtype.Text{String: production.State, Valid: true},
		}); err != nil {
			return fmt.Errorf("store production: %w", err)
		}
		return reindexBlueprintEntry(ctx, c, blueprintID)
	})
}

// AnalysisFailed records that a leaf's analysis failed for good, in one
// transaction: the job is marked failed with its code and detail, the stored
// production goes stale and the entry's search sync is queued, which leaves
// stale rates out of the index.
func (s *Service) AnalysisFailed(ctx context.Context, blueprintID, code, detail string) error {
	return s.inChange(ctx, func(c *change) error {
		if err := c.queries.MarkPhaseJobFailed(ctx, db.MarkPhaseJobFailedParams{
			BlueprintID: blueprintID, Phase: progress.PhaseAnalysis, FailureCode: code, FailureDetail: detail,
		}); err != nil {
			return fmt.Errorf("mark analysis job failed: %w", err)
		}
		if err := c.queries.MarkProductionStale(ctx, blueprintID); err != nil {
			return fmt.Errorf("mark production stale: %w", err)
		}
		return reindexBlueprintEntry(ctx, c, blueprintID)
	})
}

// SweepExpiredEntries removes expired entries, which already read as missing
// everywhere: unpublished quick previews are hard-deleted (the cascade takes
// their jobs with them), expired uploads are soft-deleted like any delete,
// with their search removal queued. Open streams of either are told. It
// returns how many entries it removed.
func (s *Service) SweepExpiredEntries(ctx context.Context) (int, error) {
	sweeps := []struct {
		name string
		run  func(context.Context, *change) ([]string, error)
	}{
		{"expired quick previews", func(ctx context.Context, c *change) ([]string, error) {
			ids, err := c.queries.DeleteExpiredPreviews(ctx)
			for _, id := range ids {
				c.touch(id)
			}
			return ids, err
		}},
		{"expired entries", func(ctx context.Context, c *change) ([]string, error) {
			ids, err := c.queries.ExpireEntries(ctx)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				if err := c.unindex(ctx, id); err != nil {
					return nil, err
				}
			}
			return ids, nil
		}},
	}
	total := 0
	for _, sweep := range sweeps {
		for {
			var removed int
			if err := s.inChange(ctx, func(c *change) error {
				ids, err := sweep.run(ctx, c)
				removed = len(ids)
				return err
			}); err != nil {
				return total, fmt.Errorf("remove %s: %w", sweep.name, err)
			}
			total += removed
			if removed > 0 {
				slog.Info("removed "+sweep.name, "count", removed)
			}
			if removed < expiredSweepBatch {
				break
			}
		}
	}
	return total, nil
}

// reindexBlueprintEntry queues the search sync of the live entry a blueprint
// belongs to (through any of its revisions). A gone entry is left alone: a
// late import must never bring it back.
func reindexBlueprintEntry(ctx context.Context, c *change, blueprintID string) error {
	entryID, err := c.queries.GetEntryIDByBlueprint(ctx, blueprintID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve entry of blueprint: %w", err)
	}
	return c.reindex(ctx, entryID)
}
