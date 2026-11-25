package catalog

import (
	"context"
	"fmt"
	"time"
)

// HistoryStep is one revision of a single-blueprint entry with its render.
type HistoryStep struct {
	RevisionID    string    `json:"revision_id" doc:"The revision's id."`
	VersionNumber int32     `json:"version_number" doc:"The revision's number."`
	GameVersion   string    `json:"game_version" doc:"The Factorio version of the revision."`
	CreatedAt     time.Time `json:"created_at" doc:"When the revision was made."`
	Note          string    `json:"note" doc:"The uploader's note on the revision; empty for none."`
	BlueprintID   string    `json:"blueprint_id" doc:"The revision's blueprint."`
	// Preview is absent while the leaf has no stored render.
	Preview *Preview `json:"preview,omitempty" doc:"The revision's render; absent while it has none stored."`
}

// ListHistory returns the revisions of an entry that are one blueprint each,
// oldest first: what the preview viewer steps through. A book's revisions are
// left out, so a book (or an entry that is one) answers fewer than two steps
// and has no timeline.
func (s *Service) ListHistory(ctx context.Context, entryID, viewerUserID string) ([]HistoryStep, error) {
	entry, err := s.entries().Viewable(ctx, entryID, viewerUserID)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListSingleBlueprintRevisions(ctx, entry.ID)
	if err != nil {
		return nil, fmt.Errorf("list history: %w", err)
	}
	steps := make([]HistoryStep, 0, len(rows))
	for _, row := range rows {
		step := HistoryStep{
			RevisionID: row.RevisionID, VersionNumber: row.VersionNumber, GameVersion: row.GameVersion,
			CreatedAt: row.CreatedAt.Time, Note: row.Note, BlueprintID: row.BlueprintID,
		}
		previews, err := s.queries.ListPreviewsByRevision(ctx, row.RevisionID)
		if err != nil {
			return nil, fmt.Errorf("list history previews: %w", err)
		}
		for _, preview := range previews {
			if preview.BlueprintID == row.BlueprintID {
				step.Preview = previewOf(preview.BlueprintID, preview)
			}
		}
		steps = append(steps, step)
	}
	return steps, nil
}
