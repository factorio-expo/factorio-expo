package preview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/previewimage"
	"factorio-expo/apps/expo-api/internal/storage"
)

// Files is where a preview's stored files live, named by the SHA-256 of the
// runner's PNG (`previews.source_sha256`): identical renders share them. A
// small preview (previewimage.HasFull false) has only the thumbnail, which
// also serves as its full image.
// Every source's files are recorded in preview_files (sizes, and whether the
// full file is stored) for the storage budget (budget.go).
// Its lock orders imports against the sweeps, so a sweep never deletes files
// an import is about to reference. That lock is in-process: one API process
// owns the files.
type Files struct {
	store   storage.Store
	records FileRecords
	mu      sync.Mutex
}

// FileRecords keeps preview_files in step with the files; *db.Queries
// satisfies it.
type FileRecords interface {
	RecordPreviewFiles(ctx context.Context, arg db.RecordPreviewFilesParams) error
	DeletePreviewFilesRecord(ctx context.Context, sha string) error
}

func NewFiles(store storage.Store, records FileRecords) *Files {
	return &Files{store: store, records: records}
}

// ValidSource reports whether sha is a lowercase hex SHA-256.
func ValidSource(sha string) bool {
	if len(sha) != 64 {
		return false
	}
	for _, r := range sha {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func fileKey(sha string, thumbnail bool) string {
	suffix := ".webp"
	if thumbnail {
		suffix = ".thumb.webp"
	}
	return "previews/" + sha[:2] + "/" + sha + suffix
}

// Open opens the full preview or the thumbnail of source sha, a width x
// height preview. It also reports whether the file opened is the thumbnail.
func (f *Files) Open(
	ctx context.Context, sha string, width, height int, thumbnail bool,
) (storage.Object, bool, error) {
	if !ValidSource(sha) {
		return nil, false, fmt.Errorf("invalid preview source %q", sha)
	}
	servesThumbnail := thumbnail || !previewimage.HasFull(width, height)
	object, err := f.store.Open(ctx, fileKey(sha, servesThumbnail))
	return object, servesThumbnail, err
}

// storeAndReference makes sha's files a blueprint's preview: under the files lock, so the
// sweep cannot delete them between the check and the row, it writes the
// encoded files if any are missing and then calls reference (which creates
// the row, or queues the files for the sweep when no row takes them). It
// reports false, doing nothing, when files are missing and encoded is nil:
// the caller encodes outside the lock and calls again.
func (f *Files) storeAndReference(
	ctx context.Context, sha string, width, height int, encoded *previewimage.Encoded,
	reference func(context.Context) error,
) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sizes, complete, err := f.complete(ctx, sha, width, height)
	if err != nil {
		return false, err
	}
	if !complete {
		if encoded == nil {
			return false, nil
		}
		if err := f.store.Put(ctx, fileKey(sha, true), bytes.NewReader(encoded.Thumbnail)); err != nil {
			return false, err
		}
		sizes = fileSizes{thumbnail: int64(len(encoded.Thumbnail))}
		if encoded.Full != nil {
			if err := f.store.Put(ctx, fileKey(sha, false), bytes.NewReader(encoded.Full)); err != nil {
				return false, err
			}
			sizes.full = pgtype.Int8{Int64: int64(len(encoded.Full)), Valid: true}
		}
	}
	if err := f.records.RecordPreviewFiles(ctx, db.RecordPreviewFilesParams{
		SourceSha256: sha, ThumbnailBytes: sizes.thumbnail, FullBytes: sizes.full, Written: !complete,
	}); err != nil {
		return false, fmt.Errorf("record preview files: %w", err)
	}
	return true, reference(ctx)
}

// fileSizes are the byte sizes of a source's files; full is null when it has
// no full file.
type fileSizes struct {
	thumbnail int64
	full      pgtype.Int8
}

// complete reports whether every file a width x height preview of sha has
// exists, and their sizes; callers hold f.mu.
func (f *Files) complete(ctx context.Context, sha string, width, height int) (fileSizes, bool, error) {
	var sizes fileSizes
	for _, thumbnail := range []bool{false, true} {
		if !thumbnail && !previewimage.HasFull(width, height) {
			continue
		}
		info, err := f.store.Stat(ctx, fileKey(sha, thumbnail))
		if errors.Is(err, storage.ErrNotFound) {
			return fileSizes{}, false, nil
		} else if err != nil {
			return fileSizes{}, false, err
		}
		if thumbnail {
			sizes.thumbnail = info.Size
		} else {
			sizes.full = pgtype.Int8{Int64: info.Size, Valid: true}
		}
	}
	return sizes, true, nil
}

// FullStored reports whether a width x height preview of sha can serve its
// full image: a small preview always can (its thumbnail is its full image).
func (f *Files) FullStored(ctx context.Context, sha string, width, height int) (bool, error) {
	if !previewimage.HasFull(width, height) {
		return true, nil
	}
	if !ValidSource(sha) {
		return false, fmt.Errorf("invalid preview source %q", sha)
	}
	_, err := f.store.Stat(ctx, fileKey(sha, false))
	if errors.Is(err, storage.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// fileDeletionQueries is the sweep's slice of the catalog; *db.Queries
// satisfies it.
type fileDeletionQueries interface {
	ListPreviewFileDeletions(ctx context.Context, limit int32) ([]string, error)
	PreviewSourceReferenced(ctx context.Context, sha string) (bool, error)
	DeletePreviewFileDeletion(ctx context.Context, sha string) error
}

const fileDeletionBatch = 500

// sweep deletes the files of every queued source that no previews row
// references any more, then its queue row. A source that is referenced again
// (an identical render was imported meanwhile) only loses its queue row.
func (f *Files) sweep(ctx context.Context, queries fileDeletionQueries) error {
	for {
		sources, err := queries.ListPreviewFileDeletions(ctx, fileDeletionBatch)
		if err != nil {
			return fmt.Errorf("list preview file deletions: %w", err)
		}
		removed := 0
		for _, sha := range sources {
			deleted, err := f.deleteUnreferenced(ctx, queries, sha)
			if err != nil {
				return err
			}
			if deleted {
				removed++
			}
		}
		if removed > 0 {
			slog.Info("deleted unreferenced preview files", "count", removed)
		}
		if len(sources) < fileDeletionBatch {
			return nil
		}
	}
}

func (f *Files) deleteUnreferenced(ctx context.Context, queries fileDeletionQueries, sha string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	referenced, err := queries.PreviewSourceReferenced(ctx, sha)
	if err != nil {
		return false, fmt.Errorf("check preview source: %w", err)
	}
	if !referenced && ValidSource(sha) {
		// A small preview never had a full file; deleting it is a no-op.
		for _, thumbnail := range []bool{false, true} {
			if err := f.store.Delete(ctx, fileKey(sha, thumbnail)); err != nil {
				return false, err
			}
		}
		if err := f.records.DeletePreviewFilesRecord(ctx, sha); err != nil {
			return false, fmt.Errorf("delete preview files record: %w", err)
		}
	}
	if err := queries.DeletePreviewFileDeletion(ctx, sha); err != nil {
		return false, fmt.Errorf("delete preview file deletion: %w", err)
	}
	return !referenced, nil
}
