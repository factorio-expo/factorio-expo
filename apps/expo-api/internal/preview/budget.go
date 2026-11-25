package preview

import (
	"context"
	"fmt"
	"log/slog"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// Budget bounds the disk the preview files use. Only full files are evicted:
// thumbnails always stay, and an evicted full file is rendered again when
// someone opens it (RequestFull). The oldest written full file goes first, and
// a re-render writes it again, so a full file people still open comes back as
// the newest.
type Budget struct {
	// MaxBytes caps the recorded size of every preview file; 0 turns the cap
	// off.
	MaxBytes int64
	// MinFreeBytes is the free space the storage disk keeps, which PostgreSQL,
	// images and logs share; 0 turns the check off.
	MinFreeBytes int64
	// FreeBytes reports the storage disk's free space; nil skips the check.
	FreeBytes func() (int64, error)
}

const (
	// The sweep starts above budgetHigh of MaxBytes and stops below budgetLow,
	// so it does not run again on the next upload.
	budgetHigh = 0.9
	budgetLow  = 0.8
	// evictionBatch is how many candidates one query lists.
	evictionBatch = 200
)

// budgetQueries is the budget sweep's slice of the catalog; *db.Queries
// satisfies it.
type budgetQueries interface {
	PreviewStorageBytes(ctx context.Context) (int64, error)
	ListEvictablePreviewFiles(ctx context.Context, limit int32) ([]db.ListEvictablePreviewFilesRow, error)
	MarkPreviewFullEvicted(ctx context.Context, sha string) error
}

// enforceBudget evicts the oldest full files while usage is above the budget
// or free space below its minimum, until usage is below budgetLow of the
// budget and free space is back above the minimum.
func (f *Files) enforceBudget(ctx context.Context, queries budgetQueries, budget Budget) error {
	used, err := queries.PreviewStorageBytes(ctx)
	if err != nil {
		return fmt.Errorf("read preview storage size: %w", err)
	}
	free, freeKnown := int64(0), false
	if budget.MinFreeBytes > 0 && budget.FreeBytes != nil {
		if free, err = budget.FreeBytes(); err != nil {
			slog.Warn("read free storage space", "error", err)
		} else {
			freeKnown = true
		}
	}
	lowFree := func() bool { return freeKnown && free < budget.MinFreeBytes }
	overBudget := func(share float64) bool {
		return budget.MaxBytes > 0 && float64(used) > share*float64(budget.MaxBytes)
	}
	if !overBudget(budgetHigh) && !lowFree() {
		return nil
	}

	evicted, freed := 0, int64(0)
	defer func() {
		if evicted > 0 {
			slog.Info("evicted full preview files", "count", evicted, "bytes", freed,
				"used_bytes", used, "max_bytes", budget.MaxBytes)
		}
	}()
	for overBudget(budgetLow) || lowFree() {
		candidates, err := queries.ListEvictablePreviewFiles(ctx, evictionBatch)
		if err != nil {
			return fmt.Errorf("list evictable preview files: %w", err)
		}
		if len(candidates) == 0 {
			slog.Warn("preview storage is over budget with no full files left to evict",
				"used_bytes", used, "max_bytes", budget.MaxBytes,
				"free_bytes", free, "min_free_bytes", budget.MinFreeBytes)
			return nil
		}
		for _, candidate := range candidates {
			if !overBudget(budgetLow) && !lowFree() {
				return nil
			}
			if err := f.evictFull(ctx, queries, candidate.SourceSha256); err != nil {
				return err
			}
			evicted++
			freed += candidate.FullBytes
			used -= candidate.FullBytes
			free += candidate.FullBytes
		}
	}
	return nil
}

// evictFull deletes a source's full file, then records it as gone. Under the
// files lock, so an import never references a file being evicted. A crash
// between the two leaves a record of a missing file, which RequestFull finds
// on disk and renders again.
func (f *Files) evictFull(ctx context.Context, queries budgetQueries, sha string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ValidSource(sha) {
		if err := f.store.Delete(ctx, fileKey(sha, false)); err != nil {
			return err
		}
	}
	if err := queries.MarkPreviewFullEvicted(ctx, sha); err != nil {
		return fmt.Errorf("mark preview full file evicted: %w", err)
	}
	return nil
}
