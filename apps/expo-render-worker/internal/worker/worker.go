package worker

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sync/atomic"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/artifacts"
	"factorio-expo/apps/expo-render-worker/internal/bridge"
	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

// zoomEpsilon absorbs the float-formatting drift introduced when zoom values
// round-trip through Lua's JSON encoder (fewer significant digits) and Go's
// float64 decoding, without masking genuinely different zoom values.
const zoomEpsilon = 1e-6

// Failure details the worker writes for its own codes.
const (
	timeoutDetail    = "Preview generation timed out"
	notStartedDetail = "Preview generation could not be started"
)

// ClientHeartbeatMaxAge is how old the capturing client's heartbeat file may
// be (written every second) before the client counts as gone.
const ClientHeartbeatMaxAge = 5 * time.Second

func zoomMatches(a, b float64) bool {
	return math.Abs(a-b) <= zoomEpsilon
}

type Store interface {
	ClaimNext(context.Context, string, time.Duration) (jobs.Job, bool, []string, error)
	CompletePhase(context.Context, string, string, jobs.Phase, *jobs.Artifact) error
	FailPhase(context.Context, string, string, jobs.Phase, string, string) error
}

type Worker struct {
	id             string
	store          Store
	bridge         *bridge.Client
	inbox          *artifacts.Inbox
	retained       *artifacts.Store
	captureTimeout time.Duration
	pollInterval   time.Duration
	wake           chan struct{}
	lastHeartbeat  atomic.Int64
	// waiting is whether the last iteration found the bridge not ready, so
	// the change is logged once rather than every second.
	waiting bool
}

func New(id string, store Store, bridgeClient *bridge.Client, inbox *artifacts.Inbox, retained *artifacts.Store,
	captureTimeout, pollInterval time.Duration) *Worker {
	worker := &Worker{
		id: id, store: store, bridge: bridgeClient, inbox: inbox, retained: retained,
		captureTimeout: captureTimeout, pollInterval: pollInterval,
		wake: make(chan struct{}, 1),
	}
	worker.lastHeartbeat.Store(time.Now().UnixNano())
	return worker
}

func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) Healthy(now time.Time) bool {
	last := time.Unix(0, w.lastHeartbeat.Load())
	return !last.IsZero() && now.Sub(last) < 2*time.Minute
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		w.lastHeartbeat.Store(time.Now().UnixNano())
		if err := w.runNext(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("preview worker iteration failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-ticker.C:
		}
	}
}

// runNext runs the next job once. Every failure is final for the job; the API
// retries the retryable ones (jobs.Retryable) with a new job.
func (w *Worker) runNext(ctx context.Context) error {
	// Claim only while the bridge can take a job: a Factorio restart then
	// delays jobs instead of failing them and spending the API's retry budget.
	if !w.bridgeReady(ctx) {
		return nil
	}
	job, claimed, interrupted, err := w.store.ClaimNext(ctx, w.id, w.captureTimeout+time.Minute)
	for _, id := range interrupted {
		w.cleanupInbox(id)
	}
	if err != nil || !claimed {
		return err
	}
	slog.Info("claimed preview job", "job_id", job.ID)

	jobCtx, cancel := context.WithTimeout(ctx, w.captureTimeout)
	defer cancel()
	submissionStarted := time.Now()
	accepted, err := w.bridge.Submit(jobCtx, job.ID, job.Blueprint, pendingPhases(job))
	if err == nil {
		slog.Info("bridge accepted job", "job_id", job.ID,
			"upload_duration_ms", time.Since(submissionStarted).Milliseconds())
	} else {
		var bridgeErr *bridge.Error
		if errors.As(err, &bridgeErr) && bridgeErr.Code == "blueprint_too_large" {
			return w.failPending(ctx, job, bridgeErr.Code, "blueprint could not be processed")
		}
		// A lost RCON response does not mean the start command failed.
		// Reconcile the serial bridge and its inbox before failing the job.
		reconciled, reconcileErr := w.waitForBridge(jobCtx, job.ID)
		switch {
		case reconcileErr != nil:
			return w.failPending(ctx, job, "capture_timeout", timeoutDetail)
		case reconciled.ActiveJob != nil && reconciled.ActiveJob.JobID == job.ID:
			accepted.Render = reconciled.ActiveJob.Render
		case reconciled.CompletedJob != nil && reconciled.CompletedJob.JobID == job.ID:
			accepted = *reconciled.CompletedJob
		case w.hasPhaseResults(job):
		default:
			slog.Warn("bridge did not start the job", "job_id", job.ID, "error", err)
			return w.failPending(ctx, job, "bridge_submission_failed", notStartedDetail)
		}
	}

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	invalidSince := newInvalidTracker()
	var watch runWatch
	for {
		invalidSince.tick()
		// An unanswered status (Factorio down) says nothing about the job:
		// the next answer, or the job timeout, decides.
		status, statusErr := w.bridge.Status(jobCtx)
		if statusErr == nil && accepted.Render == nil {
			accepted.Render = renderFor(status, job.ID)
		}
		for _, phase := range pendingPhases(job) {
			result := w.readPhase(jobCtx, job, phase, &accepted, invalidSince)
			if err := w.settlePhase(ctx, &job, phase, result); err != nil {
				return err
			}
		}
		if len(pendingPhases(job)) == 0 {
			w.cleanupInbox(job.ID)
			return nil
		}
		if statusErr == nil {
			hasResults := func() bool { return w.hasPhaseResults(job) }
			if code, detail, ended := watch.ended(job, status, time.Now(), hasResults); ended {
				return w.failPending(ctx, job, code, detail)
			}
		}
		select {
		case <-jobCtx.Done():
			return w.failPending(ctx, job, "capture_timeout", timeoutDetail)
		case <-ticker.C:
			w.lastHeartbeat.Store(time.Now().UnixNano())
		}
	}
}

func pendingPhases(job jobs.Job) []jobs.Phase {
	phases := make([]jobs.Phase, 0, 2)
	if job.PreviewPhase.Status == jobs.PhasePending {
		phases = append(phases, jobs.PhasePreview)
	}
	if job.AnalysisPhase.Status == jobs.PhasePending {
		phases = append(phases, jobs.PhaseAnalysis)
	}
	return phases
}

// invalidTracker times how long a phase has been returning the same invalid
// result. The window restarts when the error changes or a poll in between
// read the phase without error.
type invalidTracker struct {
	poll    int
	windows map[jobs.Phase]invalidWindow
}

type invalidWindow struct {
	since time.Time
	poll  int
	msg   string
}

func newInvalidTracker() *invalidTracker {
	return &invalidTracker{windows: map[jobs.Phase]invalidWindow{}}
}

func (t *invalidTracker) tick() { t.poll++ }

// observe records an invalid read in the current poll and returns how long the
// same error has been seen on consecutive polls (zero for a fresh window).
func (t *invalidTracker) observe(phase jobs.Phase, err error, now time.Time) (time.Duration, bool) {
	window, ok := t.windows[phase]
	fresh := !ok || window.poll != t.poll-1 && window.poll != t.poll || window.msg != err.Error()
	if fresh {
		t.windows[phase] = invalidWindow{since: now, poll: t.poll, msg: err.Error()}
		return 0, true
	}
	window.poll = t.poll
	t.windows[phase] = window
	return now.Sub(window.since), false
}

// failPending fails every pending phase of job, which ends it.
func (w *Worker) failPending(ctx context.Context, job jobs.Job, code, detail string) error {
	for _, phase := range pendingPhases(job) {
		if err := w.store.FailPhase(ctx, w.id, job.ID, phase, code, detail); err != nil {
			return err
		}
	}
	w.cleanupInbox(job.ID)
	return nil
}

// bridgeReady reports whether the bridge can take a job now: it answers with a
// compatible protocol, runs no job, and has its Factorio client connected and
// writing its heartbeat for captures. Changes are logged once.
func (w *Worker) bridgeReady(ctx context.Context) bool {
	status, err := w.bridge.Status(ctx)
	reason := ""
	switch {
	case err != nil:
		reason = err.Error()
	case status.ActiveJob != nil:
		reason = "bridge is still running job " + status.ActiveJob.JobID
	case !status.Capture.Connected:
		reason = "factorio client is not connected"
	case !w.inbox.ClientAlive(ClientHeartbeatMaxAge):
		reason = "factorio client heartbeat is stale"
	}
	if reason != "" && !w.waiting {
		slog.Warn("waiting for the bridge before claiming jobs", "reason", reason)
	} else if reason == "" && w.waiting {
		slog.Info("bridge is ready; claiming jobs")
	}
	w.waiting = reason != ""
	return reason == ""
}

// cleanupInbox removes a finished job's inbox. A leftover directory is only
// wasted space, so a failure is logged, not returned.
func (w *Worker) cleanupInbox(jobID string) {
	if err := w.inbox.Cleanup(jobID); err != nil {
		slog.Warn("could not clean job inbox", "job_id", jobID, "error", err)
	}
}

func (w *Worker) waitForBridge(ctx context.Context, jobID string) (bridge.Status, error) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		status, err := w.bridge.Status(ctx)
		if errors.Is(err, bridge.ErrProtocolMismatch) {
			return bridge.Status{}, err
		}
		if err == nil && (status.ActiveJob == nil ||
			(status.ActiveJob.JobID == jobID && status.ActiveJob.Phase != "uploading")) {
			return status, nil
		}

		select {
		case <-ctx.Done():
			return bridge.Status{}, ctx.Err()
		case <-ticker.C:
			w.lastHeartbeat.Store(time.Now().UnixNano())
		}
	}
}

func (w *Worker) hasPhaseResults(job jobs.Job) bool {
	if job.PreviewPhase.Status == jobs.PhasePending {
		_, ready, err := w.inbox.PreviewMarker(job.ID)
		if ready || err != nil {
			return true
		}
	}
	if job.AnalysisPhase.Status == jobs.PhasePending {
		_, ready, err := w.inbox.Analysis(job.ID)
		if ready || err != nil {
			return true
		}
	}
	return false
}
