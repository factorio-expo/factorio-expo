package catalog

import (
	"context"
	"fmt"

	"factorio-expo/apps/expo-api/internal/progress"
)

// EntryProgress is the per-leaf preview and analysis state of the entry's
// live revision, under the same access rule as reading the entry.
func (s *Service) EntryProgress(ctx context.Context, entryID, viewerUserID string) (progress.Snapshot, error) {
	entry, err := s.entries().Viewable(ctx, entryID, viewerUserID)
	if err != nil {
		return progress.Snapshot{}, err
	}
	snapshot := progress.Snapshot{EntryID: entry.ID, Leaves: []progress.Leaf{}}
	if entry.CurrentRevisionID.Valid {
		snapshot.RevisionID = entry.CurrentRevisionID.String
		rows, err := s.queries.ListRevisionLeafProgress(ctx, entry.CurrentRevisionID.String)
		if err != nil {
			return progress.Snapshot{}, fmt.Errorf("read leaf progress: %w", err)
		}
		for _, row := range rows {
			snapshot.Leaves = append(snapshot.Leaves, leafOf(row))
		}
	}
	snapshot.Settle()
	return snapshot, nil
}
