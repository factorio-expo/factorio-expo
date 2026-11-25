package staffops

import (
	"context"
	"fmt"
	"log/slog"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// The catalog side of the staff cleanups (internal/cleanup): what each would
// remove, and the hard delete of soft-deleted entries, which only staff run.

// hardDeleteBatch is how many entries one hard-delete transaction removes.
const hardDeleteBatch = 200

// CountSweepableDeletedEntries counts the soft-deleted entries a hard delete
// may remove now: those whose search removal has finished.
func (s *Service) CountSweepableDeletedEntries(ctx context.Context) (int64, error) {
	count, err := s.queries.CountSweepableDeletedEntries(ctx)
	if err != nil {
		return 0, fmt.Errorf("count deleted entries: %w", err)
	}
	return count, nil
}

// HardDeleteDeletedEntries removes soft-deleted entries for good, with
// everything that hangs off them (revisions, nodes, previews, comments,
// reports, favorites, collection items); their preview files are queued for
// the file sweep by trigger. An entry whose search removal is still pending
// waits for the next run. It returns how many it removed.
func (s *Service) HardDeleteDeletedEntries(ctx context.Context) (int, error) {
	total := 0
	for {
		var ids []string
		if err := s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
			var err error
			ids, err = queries.HardDeleteDeletedEntries(ctx, hardDeleteBatch)
			return err
		}); err != nil {
			return total, fmt.Errorf("hard-delete deleted entries: %w", err)
		}
		total += len(ids)
		if len(ids) < hardDeleteBatch {
			if total > 0 {
				slog.Info("hard-deleted deleted entries", "count", total)
			}
			return total, nil
		}
	}
}

// CountExpiredEntries counts the expired entries the minute sweep has not
// removed yet.
func (s *Service) CountExpiredEntries(ctx context.Context) (int64, error) {
	count, err := s.queries.CountExpiredEntries(ctx)
	if err != nil {
		return 0, fmt.Errorf("count expired entries: %w", err)
	}
	return count, nil
}

// CountExpiredIPBans counts the address bans and ban holds PruneBanData
// would delete.
func (s *Service) CountExpiredIPBans(ctx context.Context) (int64, error) {
	count, err := s.queries.CountExpiredIPBans(ctx)
	if err != nil {
		return 0, fmt.Errorf("count expired address bans: %w", err)
	}
	return count, nil
}

// CountStaleModDetections counts the leaves RedetectMods would update.
func (s *Service) CountStaleModDetections(ctx context.Context) (int64, error) {
	count, err := s.queries.CountStaleModDetections(ctx, s.catalog.Mods().Version())
	if err != nil {
		return 0, fmt.Errorf("count stale mod detections: %w", err)
	}
	return count, nil
}
