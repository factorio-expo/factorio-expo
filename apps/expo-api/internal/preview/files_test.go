package preview

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"factorio-expo/apps/expo-api/internal/previewimage"
	"factorio-expo/apps/expo-api/internal/storage"
)

// storeAndReference writes missing files only when given them, and always
// references files that are already complete.
func TestStoreAndReferenceWritesMissingFilesOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	files := testFiles(t)
	sha := strings.Repeat("c", 64)
	references := 0
	reference := func(context.Context) error { references++; return nil }
	encoded := &previewimage.Encoded{Full: []byte("full"), Thumbnail: []byte("thumb")}

	if stored, err := files.storeAndReference(ctx, sha, 900, 4, nil, reference); err != nil || stored || references != 0 {
		t.Fatalf("without files or an encode = %v, %v, %d references", stored, err, references)
	}
	if stored, err := files.storeAndReference(ctx, sha, 900, 4, encoded, reference); err != nil || !stored || references != 1 {
		t.Fatalf("with an encode = %v, %v, %d references", stored, err, references)
	}
	for _, thumbnail := range []bool{false, true} {
		if _, err := files.store.Stat(ctx, fileKey(sha, thumbnail)); err != nil {
			t.Fatalf("file (thumbnail %v): %v", thumbnail, err)
		}
	}
	// Complete files need no encode: an identical render reuses them.
	if stored, err := files.storeAndReference(ctx, sha, 900, 4, nil, reference); err != nil || !stored || references != 2 {
		t.Fatalf("with complete files = %v, %v, %d references", stored, err, references)
	}
	failed := errors.New("database down")
	if _, err := files.storeAndReference(ctx, sha, 900, 4, nil, func(context.Context) error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("reference error = %v, want it returned", err)
	}
}

// The sweep waits for a store in progress: it cannot delete files between
// the check that they exist and the row that references them.
func TestSweepWaitsForAStoreInProgress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	files := testFiles(t)
	sha := strings.Repeat("d", 64)
	encoded := &previewimage.Encoded{Thumbnail: []byte("thumb")}
	queue := &fakeDeletions{queued: []string{sha}, referenced: map[string]bool{}}
	swept := make(chan error, 1)

	if _, err := files.storeAndReference(ctx, sha, 6, 3, encoded, func(context.Context) error {
		go func() { swept <- files.sweep(ctx, queue) }()
		select {
		case err := <-swept:
			t.Errorf("the sweep ran during a store (%v)", err)
		case <-time.After(50 * time.Millisecond):
		}
		// The row now references the files.
		queue.referenced[sha] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := <-swept; err != nil {
		t.Fatal(err)
	}
	if _, err := files.store.Stat(ctx, fileKey(sha, true)); errors.Is(err, storage.ErrNotFound) {
		t.Fatal("the sweep deleted files a row references")
	}
}
