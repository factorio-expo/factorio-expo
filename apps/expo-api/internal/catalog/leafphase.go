package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/progress"
)

// leafOf is a stored leaf's phase states: progress.LeafOf over a row of the
// leaf_progress view. The leaf detail, the revision tree, progress snapshots
// and events, and the publish check all go through it.
func leafOf(row db.LeafProgress) progress.Leaf {
	return progress.LeafOf(progress.LeafFacts{
		BlueprintID:         row.BlueprintID,
		HasPreview:          row.HasPreview,
		PreviewJobState:     row.PreviewJobState,
		PreviewFailureCode:  row.PreviewFailureCode,
		HasProduction:       row.HasProduction,
		AnalysisJobState:    row.AnalysisJobState,
		AnalysisFailureCode: row.AnalysisFailureCode,
	})
}

// LeafProgress is one leaf's phase states as stored now, for the
// dispatcher's progress events (preview.Catalog); found is false once the
// leaf is gone.
func (s *Service) LeafProgress(ctx context.Context, blueprintID string) (progress.Leaf, bool, error) {
	row, err := s.queries.GetLeafProgress(ctx, blueprintID)
	if errors.Is(err, pgx.ErrNoRows) {
		return progress.Leaf{}, false, nil
	}
	if err != nil {
		return progress.Leaf{}, false, fmt.Errorf("read leaf progress: %w", err)
	}
	return leafOf(row), true, nil
}
