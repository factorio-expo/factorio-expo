package worker

import (
	"log/slog"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/bridge"
	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

// Failures the worker writes when the bridge ended a run without its results.
// factorio_restarted is final (jobs.Retryable): the blueprint almost always
// crashed the server. result_missing is retryable: a client killed after the
// capture is not the blueprint's fault.
const (
	restartedCode   = "factorio_restarted"
	restartedDetail = "Factorio stopped while processing this blueprint"
	missingCode     = "result_missing"
	missingDetail   = "Factorio finished preview generation without writing the result"
)

// missingResultGrace is how long a phase result may lag behind the bridge
// reporting the job completed. The preview marker comes from the capturing
// client, a few ticks behind the server; a client killed after the capture
// never writes it, and the server notices only ~20 s later.
const missingResultGrace = 30 * time.Second

// runWatch follows the bridge's view of one running job across polls, so a run
// that cannot produce its pending results ends now instead of at the job
// timeout.
type runWatch struct {
	completedSince time.Time
}

// ended reads one bridge status taken while job still has pending phases.
// Factorio reloads its save on every restart, so a bridge that answers without
// knowing the job has lost it; a completed job whose results stay missing past
// missingResultGrace will not get them. hasResults reports whether a pending
// phase already has a result file, which the phase reader settles instead.
func (r *runWatch) ended(job jobs.Job, status bridge.Status, now time.Time, hasResults func() bool) (string, string, bool) {
	switch {
	case status.ActiveJob != nil && status.ActiveJob.JobID == job.ID:
		return "", "", false
	case status.CompletedJob != nil && status.CompletedJob.JobID == job.ID:
		if r.completedSince.IsZero() {
			r.completedSince = now
		}
		if now.Sub(r.completedSince) < missingResultGrace || hasResults() {
			return "", "", false
		}
		slog.Warn("bridge completed the job without its results", "job_id", job.ID)
		return missingCode, missingDetail, true
	case hasResults():
		return "", "", false
	default:
		slog.Warn("bridge no longer knows the job; Factorio restarted", "job_id", job.ID)
		return restartedCode, restartedDetail, true
	}
}
