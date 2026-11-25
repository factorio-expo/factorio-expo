package preview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"factorio-expo/apps/expo-api/internal/storage"
	"factorio-expo/internal/runnerapi"
)

func succeededWith(artifact *runnerapi.Artifact) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		job := jobResponse(runnerapi.PhaseSucceeded, runnerapi.PhaseNotRequested)
		job.State = runnerapi.JobSucceeded
		job.Artifact = artifact
		writeJSON(w, http.StatusOK, job)
	}
}

func settled(queries *fakeQueries) func() bool {
	return func() bool {
		return queries.called("DeletePreviewJob") || queries.called("RetryPreviewJob") ||
			queries.called("MarkPreviewJobFailed")
	}
}

// A large preview stores a full file and a thumbnail; one that fits
// the thumbnail size stores the thumbnail alone, and its full URL serves it.
func TestImportStoresFilesAndReleasesTheRunnerCopy(t *testing.T) {
	requireEncoder(t)
	t.Parallel()
	for _, test := range []struct {
		name          string
		width, height int
		wantFull      bool
	}{
		{"small", 6, 3, false},
		{"large", 900, 4, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			row := inFlightRow()
			row.AnalysisClaimed = false
			queries := newFakeQueries(row)
			image, artifact := pngImageSized(t, test.width, test.height)
			fake := &fakeRunner{image: image, get: succeededWith(artifact)}
			files := testFiles(t)
			dispatcher := newDispatcher(queries, queries, queries, fake.client(t), files, Options{PollInterval: time.Millisecond})
			pollUntil(t, dispatcher, settled(queries))

			if !queries.called("StorePreview") || !queries.called("DeletePreviewJob") ||
				len(queries.queued) != 0 {
				t.Fatalf("calls = %v", queries.calls)
			}
			if !fake.previewDeleted {
				t.Fatal("the runner's preview artifact was not released")
			}
			if _, err := files.store.Stat(ctx, fileKey(artifact.SHA256, false)); (err == nil) != test.wantFull {
				t.Fatalf("full file stored = %v, want %v", err == nil, test.wantFull)
			}
			for _, thumbnail := range []bool{false, true} {
				object, servesThumbnail, err := files.Open(ctx, artifact.SHA256, test.width, test.height, thumbnail)
				if err != nil {
					t.Fatalf("thumbnail=%v: %v", thumbnail, err)
				}
				object.Close()
				if want := thumbnail || !test.wantFull; servesThumbnail != want {
					t.Fatalf("thumbnail=%v serves thumbnail = %v", thumbnail, servesThumbnail)
				}
			}

			// An identical render for another blueprint reuses the stored files.
			other := row
			other.BlueprintID = idOf(2)
			again := newFakeQueries(other)
			dispatcher = newDispatcher(again, again, again, fake.client(t), files, Options{PollInterval: time.Millisecond})
			pollUntil(t, dispatcher, settled(again))
			if !again.called("DeletePreviewJob") || fake.imageFetches != 1 {
				t.Fatalf("calls = %v, downloads = %d", again.calls, fake.imageFetches)
			}
		})
	}
}

func TestImportRedeliveryQueuesUnreferencedFiles(t *testing.T) {
	requireEncoder(t)
	t.Parallel()
	row := inFlightRow()
	row.AnalysisClaimed = false
	queries := newFakeQueries(row)
	queries.storeRefused = true
	image, artifact := pngImage(t)
	fake := &fakeRunner{image: image, get: succeededWith(artifact)}
	dispatcher := newDispatcher(queries, queries, queries, fake.client(t), testFiles(t), Options{PollInterval: time.Millisecond})
	pollUntil(t, dispatcher, settled(queries))
	if !queries.called("DeletePreviewJob") || len(queries.queued) != 1 || queries.queued[0] != artifact.SHA256 {
		t.Fatalf("calls = %v, queued = %v", queries.calls, queries.queued)
	}
}

func TestImportRejectsBadImages(t *testing.T) {
	requireEncoder(t)
	t.Parallel()
	notPNG := []byte("not a png at all")
	digest := sha256.Sum256(notPNG)
	image, valid := pngImage(t)
	for _, test := range []struct {
		name      string
		image     []byte
		artifact  runnerapi.Artifact
		wantFinal bool
	}{
		{"corrupt PNG", notPNG, runnerapi.Artifact{
			State: runnerapi.ArtifactAvailable, Href: runnerapi.ArtifactPath("rj"),
			SHA256: hex.EncodeToString(digest[:]), Width: 6, Height: 3,
		}, true},
		{"checksum mismatch", image, func() runnerapi.Artifact {
			artifact := *valid
			artifact.SHA256 = strings.Repeat("0", 64)
			return artifact
		}(), false},
		{"size mismatch", image, func() runnerapi.Artifact {
			artifact := *valid
			artifact.Width = 7
			return artifact
		}(), true},
		{"invalid metadata", image, func() runnerapi.Artifact {
			artifact := *valid
			artifact.SHA256 = "x"
			return artifact
		}(), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			row := inFlightRow()
			row.AnalysisClaimed = false
			queries := newFakeQueries(row)
			fake := &fakeRunner{image: test.image, get: succeededWith(&test.artifact)}
			dispatcher := newDispatcher(queries, queries, queries, fake.client(t), testFiles(t), Options{PollInterval: time.Millisecond})
			pollUntil(t, dispatcher, settled(queries))
			if test.wantFinal {
				if queries.failed["preview"] != "invalid_result" {
					t.Fatalf("calls = %v, failed = %v", queries.calls, queries.failed)
				}
			} else if queries.retry["preview"].FailureCode != "invalid_result" {
				t.Fatalf("calls = %v, retry = %v", queries.calls, queries.retry)
			}
			if queries.called("StorePreview") || fake.previewDeleted {
				t.Fatalf("a rejected image was stored: %v", queries.calls)
			}
		})
	}
}

// fakeDeletions is the sweep's queue: referenced sources stay on disk.
type fakeDeletions struct {
	queued     []string
	referenced map[string]bool
}

func (f *fakeDeletions) ListPreviewFileDeletions(context.Context, int32) ([]string, error) {
	return append([]string(nil), f.queued...), nil
}

func (f *fakeDeletions) PreviewSourceReferenced(_ context.Context, sha string) (bool, error) {
	return f.referenced[sha], nil
}

func (f *fakeDeletions) DeletePreviewFileDeletion(_ context.Context, sha string) error {
	for i, queued := range f.queued {
		if queued == sha {
			f.queued = append(f.queued[:i], f.queued[i+1:]...)
			break
		}
	}
	return nil
}

func TestSweepDeletesOnlyUnreferencedFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	files := testFiles(t)
	kept, gone := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for _, sha := range []string{kept, gone} {
		for _, thumbnail := range []bool{false, true} {
			if err := files.store.Put(ctx, fileKey(sha, thumbnail), strings.NewReader("x")); err != nil {
				t.Fatal(err)
			}
		}
	}
	queue := &fakeDeletions{queued: []string{kept, gone}, referenced: map[string]bool{kept: true}}
	if err := files.sweep(ctx, queue); err != nil {
		t.Fatal(err)
	}
	if len(queue.queued) != 0 {
		t.Fatalf("queue = %v", queue.queued)
	}
	for _, thumbnail := range []bool{false, true} {
		if _, err := files.store.Stat(ctx, fileKey(kept, thumbnail)); err != nil {
			t.Fatalf("referenced file deleted: %v", err)
		}
		if _, err := files.store.Stat(ctx, fileKey(gone, thumbnail)); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("unreferenced file kept: %v", err)
		}
	}
}
