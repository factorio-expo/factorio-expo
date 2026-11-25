package preview

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/storage"
)

// Staff cleanups of the preview files (internal/cleanup): orphaned files the
// deletion queue never heard of (a crash between a write and its row), and
// evicting full files down to a size the staff member picks.

// CleanupQueries is the cleanups' slice of the catalog; *db.Queries
// satisfies it.
type CleanupQueries interface {
	fileDeletionQueries
	budgetQueries
	ListPreviewFileSources(ctx context.Context, arg db.ListPreviewFileSourcesParams) ([]string, error)
	ListReferencedPreviewSources(ctx context.Context, sources []string) ([]string, error)
}

const orphanBatch = 500

// Orphans are preview sources that no previews row references, found on
// disk or in preview_files, and the bytes of their files on disk.
type Orphans struct {
	Sources []string
	Bytes   int64
}

// FindOrphans lists the unreferenced sources. Some may be referenced again
// by the time they are deleted; DeleteOrphans checks each again.
func (f *Files) FindOrphans(ctx context.Context, queries CleanupQueries) (Orphans, error) {
	sizes := map[string]int64{}
	if err := f.store.Walk(ctx, "previews/", func(key string, info storage.Info) error {
		if sha, ok := sourceOfKey(key); ok {
			sizes[sha] += info.Size
		}
		return nil
	}); err != nil {
		return Orphans{}, err
	}
	for after := ""; ; {
		sources, err := queries.ListPreviewFileSources(ctx, db.ListPreviewFileSourcesParams{After: after, RowLimit: orphanBatch})
		if err != nil {
			return Orphans{}, fmt.Errorf("list preview file records: %w", err)
		}
		for _, sha := range sources {
			if _, ok := sizes[sha]; !ok {
				sizes[sha] = 0
			}
		}
		if len(sources) < orphanBatch {
			break
		}
		after = sources[len(sources)-1]
	}

	var orphans Orphans
	candidates := make([]string, 0, len(sizes))
	for sha := range sizes {
		candidates = append(candidates, sha)
	}
	for start := 0; start < len(candidates); start += orphanBatch {
		batch := candidates[start:min(start+orphanBatch, len(candidates))]
		referenced, err := queries.ListReferencedPreviewSources(ctx, batch)
		if err != nil {
			return Orphans{}, fmt.Errorf("check preview sources: %w", err)
		}
		used := make(map[string]bool, len(referenced))
		for _, sha := range referenced {
			used[sha] = true
		}
		for _, sha := range batch {
			if !used[sha] {
				orphans.Sources = append(orphans.Sources, sha)
				orphans.Bytes += sizes[sha]
			}
		}
	}
	return orphans, nil
}

// DeleteOrphans deletes the files and record of every unreferenced source,
// checking each again under the files lock (an import may have referenced it
// since). It returns how many sources it deleted and their bytes on disk.
func (f *Files) DeleteOrphans(ctx context.Context, queries CleanupQueries) (int, int64, error) {
	orphans, err := f.FindOrphans(ctx, queries)
	if err != nil {
		return 0, 0, err
	}
	deleted, freed := 0, int64(0)
	for _, sha := range orphans.Sources {
		size, err := f.storedBytes(ctx, sha)
		if err != nil {
			return deleted, freed, err
		}
		removed, err := f.deleteUnreferenced(ctx, queries, sha)
		if err != nil {
			return deleted, freed, err
		}
		if removed {
			deleted++
			freed += size
		}
	}
	if deleted > 0 {
		slog.Info("deleted orphaned preview files", "sources", deleted, "bytes", freed)
	}
	return deleted, freed, nil
}

// storedBytes is the size of a source's files on disk.
func (f *Files) storedBytes(ctx context.Context, sha string) (int64, error) {
	total := int64(0)
	for _, thumbnail := range []bool{false, true} {
		info, err := f.store.Stat(ctx, fileKey(sha, thumbnail))
		if err == nil {
			total += info.Size
		} else if !errors.Is(err, storage.ErrNotFound) {
			return 0, err
		}
	}
	return total, nil
}

// sourceOfKey reads the source out of a preview file key
// (previews/ab/<sha>.webp or .thumb.webp).
func sourceOfKey(key string) (string, bool) {
	name, ok := strings.CutPrefix(key, "previews/")
	if !ok {
		return "", false
	}
	_, name, ok = strings.Cut(name, "/")
	if !ok {
		return "", false
	}
	sha, ok := strings.CutSuffix(name, ".thumb.webp")
	if !ok {
		sha, ok = strings.CutSuffix(name, ".webp")
	}
	if !ok || !ValidSource(sha) || key != fileKey(sha, strings.HasSuffix(name, ".thumb.webp")) {
		return "", false
	}
	return sha, true
}

// EvictTo evicts full files, the oldest written first, until the recorded
// size of every preview file is at most target bytes or no full file is
// left. Thumbnails always stay; an evicted full file is rendered again when
// someone opens it. It returns how many files it evicted and their bytes.
func (f *Files) EvictTo(ctx context.Context, queries CleanupQueries, target int64) (int, int64, error) {
	used, err := queries.PreviewStorageBytes(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("read preview storage size: %w", err)
	}
	evicted, freed := 0, int64(0)
	for used > target {
		candidates, err := queries.ListEvictablePreviewFiles(ctx, evictionBatch)
		if err != nil {
			return evicted, freed, fmt.Errorf("list evictable preview files: %w", err)
		}
		if len(candidates) == 0 {
			break
		}
		for _, candidate := range candidates {
			if used <= target {
				break
			}
			if err := f.evictFull(ctx, queries, candidate.SourceSha256); err != nil {
				return evicted, freed, err
			}
			evicted++
			freed += candidate.FullBytes
			used -= candidate.FullBytes
		}
	}
	if evicted > 0 {
		slog.Info("evicted full preview files to a target", "count", evicted, "bytes", freed, "target_bytes", target)
	}
	return evicted, freed, nil
}
