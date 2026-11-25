package preview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"time"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/outbox"
	"factorio-expo/apps/expo-api/internal/previewimage"
	"factorio-expo/apps/expo-api/internal/runner"
	"factorio-expo/internal/runnerapi"
)

// finishedImportTTL drops results nobody collected (the blueprint was deleted
// while its import ran).
const finishedImportTTL = 10 * time.Minute

// importer copies a finished preview from the runner into the API's file
// storage. Each import runs in its own goroutine so a slow encode never holds
// up dispatching; at most `slots` encode at once, because a large encode
// needs a few hundred MB. Results are collected by the next poll of the
// same runner job. Nothing here is durable: after a restart the job is still
// pending and its import simply starts again (the files are content-named, so
// work already stored is reused).
type importer struct {
	runner  runnerAPI
	catalog Catalog
	files   *Files
	slots   chan struct{}
	// done is called when an import finishes, so its result is collected
	// without waiting for the next poll tick.
	done func()

	mu   sync.Mutex
	runs map[string]*importRun
	// running joins the import goroutines (Dispatcher.Run waits for them).
	running sync.WaitGroup
}

type importRun struct {
	finished time.Time
	outcome  outbox.Outcome
}

// defaultEncoders is half the CPUs (at least one), leaving the rest for
// requests.
func defaultEncoders() int { return max(1, runtime.GOMAXPROCS(0)/2) }

func newImporter(
	runnerAPI runnerAPI, catalog Catalog, files *Files, encoders int,
) *importer {
	if encoders < 1 {
		encoders = defaultEncoders()
	}
	return &importer{
		runner: runnerAPI, catalog: catalog, files: files,
		slots: make(chan struct{}, encoders), runs: map[string]*importRun{},
	}
}

// Collect reports the import started for runner job jobID: waiting while it
// runs, then its outcome once. ok is false when none was started. The handler
// asks before it looks at the job's artifact, which a finished import has
// already released.
func (im *importer) Collect(jobID string) (outcome outbox.Outcome, ok bool) {
	im.mu.Lock()
	defer im.mu.Unlock()
	run, ok := im.runs[jobID]
	if !ok {
		return outbox.Outcome{}, false
	}
	if run.finished.IsZero() {
		return outbox.Waiting(), true
	}
	delete(im.runs, jobID)
	return run.outcome, true
}

// Import starts the import of job's preview; Collect reports its outcome.
func (im *importer) Import(ctx context.Context, item db.ClaimDispatchJobsRow, job runnerapi.Job) outbox.Outcome {
	im.mu.Lock()
	defer im.mu.Unlock()
	if _, ok := im.runs[job.ID]; ok {
		return outbox.Waiting()
	}
	now := time.Now()
	for id, run := range im.runs {
		if !run.finished.IsZero() && now.Sub(run.finished) > finishedImportTTL {
			delete(im.runs, id)
		}
	}
	run := &importRun{}
	im.runs[job.ID] = run
	im.running.Go(func() {
		outcome := im.importPreview(ctx, item, *job.Artifact, job.ID)
		im.mu.Lock()
		run.outcome, run.finished = outcome, time.Now()
		im.mu.Unlock()
		if im.done != nil {
			im.done()
		}
	})
	return outbox.Waiting()
}

func (im *importer) importPreview(
	ctx context.Context, item db.ClaimDispatchJobsRow, artifact runnerapi.Artifact, runnerJobID string,
) outbox.Outcome {
	blueprintID := item.BlueprintID
	sha := artifact.SHA256
	if !ValidSource(sha) || artifact.Width < 1 || artifact.Height < 1 {
		return outbox.Permanent("invalid_result", fmt.Sprintf(
			"runner artifact metadata is invalid (sha256 %q, %dx%d)", sha, artifact.Width, artifact.Height))
	}
	stored, err := im.store(ctx, item, sha, artifact, nil)
	if err == nil && !stored {
		// The files are not stored yet: download and encode outside the lock.
		encoded, outcome, ok := im.encode(ctx, artifact, sha, blueprintID)
		if !ok {
			return outcome
		}
		_, err = im.store(ctx, item, sha, artifact, &encoded)
	}
	if err != nil {
		slog.Error("store preview", "blueprint_id", blueprintID, "error", err)
		return outbox.WaitFor(importRetryDelay)
	}
	if err := im.runner.DeletePreviewArtifact(ctx, runnerJobID); err != nil {
		var runnerError *runner.Error
		gone := errors.As(err, &runnerError) &&
			(runnerError.Status == http.StatusNotFound || runnerError.Status == http.StatusConflict)
		if !gone {
			// The next import finds the files and the row, and only retries this.
			slog.Warn("release preview artifact", "blueprint_id", blueprintID, "error", err)
			return outbox.WaitFor(importRetryDelay)
		}
	}
	return outbox.Succeeded()
}

// store makes sha's files the blueprint's preview through Files (which
// orders it against the file sweep), writing encoded when files are missing.
// It reports false when they are missing and nothing was encoded.
func (im *importer) store(
	ctx context.Context, item db.ClaimDispatchJobsRow, sha string,
	artifact runnerapi.Artifact, encoded *previewimage.Encoded,
) (bool, error) {
	return im.files.storeAndReference(ctx, sha, artifact.Width, artifact.Height, encoded,
		func(ctx context.Context) error {
			return im.catalog.StorePreview(ctx, item.BlueprintID, sha, artifact.Width, artifact.Height)
		})
}

// encode downloads the runner's PNG, checks it against the artifact metadata
// and encodes both files, waiting for a free encoder slot.
func (im *importer) encode(
	ctx context.Context, artifact runnerapi.Artifact, sha, blueprintID string,
) (previewimage.Encoded, outbox.Outcome, bool) {
	select {
	case im.slots <- struct{}{}:
	case <-ctx.Done():
		return previewimage.Encoded{}, outbox.WaitFor(importRetryDelay), false
	}
	defer func() { <-im.slots }()

	response, err := im.runner.FetchArtifact(ctx, artifact.Href)
	if err != nil {
		var runnerError *runner.Error
		if errors.As(err, &runnerError) && runnerError.Status == http.StatusNotFound {
			return previewimage.Encoded{}, outbox.Transient("job_lost", "preview artifact is gone: "+err.Error()), false
		}
		slog.Warn("download preview", "blueprint_id", blueprintID, "error", err)
		return previewimage.Encoded{}, outbox.WaitFor(importRetryDelay), false
	}
	defer response.Body.Close()
	source, err := io.ReadAll(io.LimitReader(response.Body, previewimage.MaxBytes+1))
	if err != nil {
		slog.Warn("download preview", "blueprint_id", blueprintID, "error", err)
		return previewimage.Encoded{}, outbox.WaitFor(importRetryDelay), false
	}
	if len(source) > previewimage.MaxBytes {
		// Cut off by the read limit: it can never match the checksum.
		return previewimage.Encoded{}, outbox.Permanent("invalid_result", fmt.Sprintf(
			"preview exceeds %d bytes", previewimage.MaxBytes)), false
	}
	if digest := sha256.Sum256(source); hex.EncodeToString(digest[:]) != sha {
		return previewimage.Encoded{}, outbox.Transient("invalid_result",
			"downloaded preview does not match the artifact checksum"), false
	}

	started := time.Now()
	encoded, err := previewimage.Encode(ctx, source)
	if errors.Is(err, previewimage.ErrInvalidImage) {
		return previewimage.Encoded{}, outbox.Permanent("invalid_result", err.Error()), false
	} else if err != nil {
		return previewimage.Encoded{}, outbox.Transient("encode_failed", err.Error()), false
	}
	if encoded.Width != artifact.Width || encoded.Height != artifact.Height {
		return previewimage.Encoded{}, outbox.Permanent("invalid_result", fmt.Sprintf(
			"preview is %dx%d, the runner reported %dx%d",
			encoded.Width, encoded.Height, artifact.Width, artifact.Height)), false
	}
	slog.Info("encoded preview", "blueprint_id", blueprintID,
		"width", encoded.Width, "height", encoded.Height, "png_bytes", len(source),
		"full_bytes", len(encoded.Full), "thumbnail_bytes", len(encoded.Thumbnail),
		"duration", time.Since(started).Round(time.Millisecond))
	return encoded, outbox.Outcome{}, true
}
