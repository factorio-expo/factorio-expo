package catalog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/previewimage"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/apps/expo-api/internal/renderqueue"
	"factorio-expo/internal/analysisresult"
)

// Result reuse: a leaf whose exchange string matches another leaf's (any
// entry, any visibility, deleted ones too: the copy is made from the
// uploader's own string) takes that leaf's stored preview, current analysis
// or final failure instead of rendering again. Uploads reuse on write (only
// what still needs work counts against the uploader's cap); the preview
// dispatcher reuses on every poll, which fills leaves that waited for an
// identical one already in the render worker (ReuseWaitingResults).

// reusableFailures are the final failure codes that are the blueprint's
// own: running the same string again ends the same way (or crashes the
// game server again). invalid_result and result_mismatch are final too but
// point at our side, so they are never copied.
var reusableFailures = []string{
	"invalid_blueprint", "blueprint_too_large", "request_too_large", "analysis_failed", "factorio_restarted",
}

// reuseResults copies results to the waiting leaves of revisionID (every
// live entry's when it is not valid), in the change's transaction.
func reuseResults(ctx context.Context, queries *db.Queries, revisionID pgtype.Text) ([]renderqueue.ReusedLeaf, error) {
	var reused []renderqueue.ReusedLeaf
	previews, err := queries.ReusePreviews(ctx, db.ReusePreviewsParams{
		RevisionID: revisionID, ThumbnailMax: previewimage.ThumbnailMax,
	})
	if err != nil {
		return nil, fmt.Errorf("reuse previews: %w", err)
	}
	for _, row := range previews {
		reused = append(reused, renderqueue.ReusedLeaf{EntryID: row.EntryID, BlueprintID: row.BlueprintID, Phase: progress.PhasePreview})
	}
	productions, err := queries.ReuseProduction(ctx, db.ReuseProductionParams{
		RevisionID: revisionID, SchemaVersion: analysisresult.SchemaVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("reuse production: %w", err)
	}
	for _, row := range productions {
		reused = append(reused, renderqueue.ReusedLeaf{EntryID: row.EntryID, BlueprintID: row.BlueprintID, Phase: progress.PhaseAnalysis})
	}
	previewFailures, err := queries.ReusePreviewFailures(ctx, db.ReusePreviewFailuresParams{
		RevisionID: revisionID, Codes: reusableFailures,
	})
	if err != nil {
		return nil, fmt.Errorf("reuse preview failures: %w", err)
	}
	for _, row := range previewFailures {
		reused = append(reused, renderqueue.ReusedLeaf{
			EntryID: row.EntryID, BlueprintID: row.BlueprintID, Phase: progress.PhasePreview, FailureCode: row.FailureCode,
		})
	}
	analysisFailures, err := queries.ReuseAnalysisFailures(ctx, db.ReuseAnalysisFailuresParams{
		RevisionID: revisionID, Codes: reusableFailures,
	})
	if err != nil {
		return nil, fmt.Errorf("reuse analysis failures: %w", err)
	}
	for _, row := range analysisFailures {
		reused = append(reused, renderqueue.ReusedLeaf{
			EntryID: row.EntryID, BlueprintID: row.BlueprintID, Phase: progress.PhaseAnalysis, FailureCode: row.FailureCode,
		})
	}
	return reused, nil
}

// ReuseWaitingResults gives every waiting leaf of a live entry (no runner
// job yet) a result another leaf with the same string has stored meanwhile,
// and queues the search sync of the entries that changed. The preview
// dispatcher calls it before each claim and tells open streams.
func (s *Service) ReuseWaitingResults(ctx context.Context) ([]renderqueue.ReusedLeaf, error) {
	var reused []renderqueue.ReusedLeaf
	err := s.inChange(ctx, func(c *change) error {
		var err error
		if reused, err = reuseResults(ctx, c.queries, pgtype.Text{}); err != nil {
			return err
		}
		seen := make(map[string]bool, len(reused))
		for _, leaf := range reused {
			if seen[leaf.EntryID] {
				continue
			}
			seen[leaf.EntryID] = true
			if err := c.reindex(ctx, leaf.EntryID); err != nil {
				return err
			}
		}
		return nil
	})
	return reused, err
}
