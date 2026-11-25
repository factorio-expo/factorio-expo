package search

import (
	"context"
	"fmt"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// viewBatch is how many changed view counts one refresh round sends.
const viewBatch = 500

// viewQueries are the entry_view_counts reads and writes of the view refresh.
type viewQueries interface {
	ListUnindexedViewCounts(ctx context.Context, limit int32) ([]db.ListUnindexedViewCountsRow, error)
	MarkViewCountsIndexed(ctx context.Context, arg db.MarkViewCountsIndexedParams) error
}

// viewCount is one entry's view_count, written to all its documents.
type viewCount struct {
	ID        string
	ViewCount int64
}

// viewIndex writes view counts into the entries' documents.
type viewIndex interface {
	updateViewCounts(ctx context.Context, counts []viewCount) error
}

// refreshViews sends every view count that changed since it was last sent,
// as partial document updates, in batches. A view is never an
// entry_search_jobs row: that would put one outbox row per page view.
// Counts of entries without a search document are only marked sent; an entry
// that becomes searchable later is projected with its current count.
func refreshViews(ctx context.Context, queries viewQueries, index viewIndex) error {
	for {
		rows, err := queries.ListUnindexedViewCounts(ctx, viewBatch)
		if err != nil {
			return fmt.Errorf("list changed view counts: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		counts := make([]viewCount, 0, len(rows))
		sent := db.MarkViewCountsIndexedParams{
			EntryIds: make([]string, 0, len(rows)),
			Views:    make([]int64, 0, len(rows)),
		}
		for _, row := range rows {
			if row.Searchable {
				counts = append(counts, viewCount{ID: row.EntryID, ViewCount: row.Views})
			}
			sent.EntryIds = append(sent.EntryIds, row.EntryID)
			sent.Views = append(sent.Views, row.Views)
		}
		if len(counts) > 0 {
			if err := index.updateViewCounts(ctx, counts); err != nil {
				return fmt.Errorf("update view counts: %w", err)
			}
		}
		if err := queries.MarkViewCountsIndexed(ctx, sent); err != nil {
			return fmt.Errorf("mark view counts sent: %w", err)
		}
		if len(rows) < viewBatch {
			return nil
		}
	}
}
