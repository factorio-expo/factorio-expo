package worker

import (
	"context"
	"log/slog"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/bridge"
	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

// invalidResultGrace is how long a phase may keep returning the same unreadable
// result before it is failed as invalid_result: Factorio may still be writing.
const invalidResultGrace = 10 * time.Second

type phaseState int

const (
	// phasePending: no usable result yet; poll again.
	phasePending phaseState = iota
	phaseFailed
	phaseSucceeded
)

// phaseResult is what one poll of a phase's inbox files decided. Reading the
// inbox, the invalid-result grace window and ingestion all happen behind
// readPhase, so the worker loop only maps a result to a store call.
type phaseResult struct {
	State    phaseState
	Code     string
	Message  string
	Artifact *jobs.Artifact
}

func pendingResult() phaseResult { return phaseResult{State: phasePending} }

func failedResult(code, message string) phaseResult {
	return phaseResult{State: phaseFailed, Code: code, Message: message}
}

// readPhase polls one pending phase of job. An unreadable or unvalidatable
// result stays pending until the same error has persisted for
// invalidResultGrace, then becomes failed(invalid_result).
func (w *Worker) readPhase(
	ctx context.Context, job jobs.Job, phase jobs.Phase, accepted *bridge.Accepted, tracker *invalidTracker,
) phaseResult {
	var (
		result phaseResult
		err    error
	)
	switch phase {
	case jobs.PhasePreview:
		result, err = w.readPreview(ctx, job, accepted)
	default:
		result, err = w.readAnalysis(ctx, job)
	}
	if err == nil {
		return result
	}
	elapsed, fresh := tracker.observe(phase, err, time.Now())
	if fresh {
		slog.Warn("phase result is not ready for ingestion", "job_id", job.ID, "phase", phase, "error", err)
		return pendingResult()
	}
	if elapsed < invalidResultGrace {
		return pendingResult()
	}
	return failedResult("invalid_result", "Factorio produced an invalid phase result")
}

// readPreview checks a success marker against accepted.Render, which the
// worker loop fills from the bridge status once the bridge reports it.
func (w *Worker) readPreview(ctx context.Context, job jobs.Job, accepted *bridge.Accepted) (phaseResult, error) {
	marker, ready, err := w.inbox.PreviewMarker(job.ID)
	if err != nil || !ready {
		return pendingResult(), err
	}
	if marker.Status == "failed" {
		failure, err := w.inbox.PreviewFailure(job.ID)
		if err != nil {
			return pendingResult(), err
		}
		return failedResult(failure.Code, failure.Message), nil
	}
	if accepted.Render == nil {
		// Successful previews must also match the independently reported
		// render profile, not just their PNG marker.
		return pendingResult(), nil
	}
	if marker.Resolution != accepted.Render.Resolution || !zoomMatches(marker.Zoom, accepted.Render.Zoom) ||
		marker.SpacePlatform != accepted.Render.SpacePlatform {
		slog.Error("result mismatch",
			"marker.resolution", marker.Resolution, "accepted.Resolution", accepted.Render.Resolution,
			"marker.Zoom", marker.Zoom, "accepted.Zoom", accepted.Render.Zoom,
			"marker.SpacePlatform", marker.SpacePlatform, "accepted.SpacePlatform", accepted.Render.SpacePlatform,
		)
		return failedResult("result_mismatch", "Factorio produced invalid preview metadata"), nil
	}
	image, err := w.inbox.OpenPreviewImage(marker)
	if err != nil {
		return pendingResult(), err
	}
	defer image.Close()
	artifact, err := w.retained.RetainPreview(ctx, image)
	if err != nil {
		return pendingResult(), err
	}
	return phaseResult{State: phaseSucceeded, Artifact: &artifact}, nil
}

func (w *Worker) readAnalysis(ctx context.Context, job jobs.Job) (phaseResult, error) {
	analysis, ready, err := w.inbox.Analysis(job.ID)
	if err != nil || !ready {
		return pendingResult(), err
	}
	if failure := analysis.Failure; failure != nil {
		return failedResult(failure.Code, failure.Message), nil
	}
	artifact, err := w.retained.RetainAnalysis(ctx, job.ID, analysis.Data)
	if err != nil {
		return pendingResult(), err
	}
	return phaseResult{State: phaseSucceeded, Artifact: &artifact}, nil
}

// renderFor returns the render profile the bridge reports for job, whether the
// job is still active or already completed.
func renderFor(status bridge.Status, jobID string) *bridge.Render {
	if status.ActiveJob != nil && status.ActiveJob.JobID == jobID {
		return status.ActiveJob.Render
	}
	if status.CompletedJob != nil && status.CompletedJob.JobID == jobID {
		return status.CompletedJob.Render
	}
	return nil
}

func phaseStatus(job *jobs.Job, phase jobs.Phase) *jobs.PhaseStatus {
	if phase == jobs.PhasePreview {
		return &job.PreviewPhase.Status
	}
	return &job.AnalysisPhase.Status
}

// settlePhase persists a non-pending phase result and records it on job.
func (w *Worker) settlePhase(ctx context.Context, job *jobs.Job, phase jobs.Phase, result phaseResult) error {
	switch result.State {
	case phaseFailed:
		if err := w.store.FailPhase(ctx, w.id, job.ID, phase, result.Code, result.Message); err != nil {
			return err
		}
		*phaseStatus(job, phase) = jobs.PhaseFailed
	case phaseSucceeded:
		if err := w.store.CompletePhase(ctx, w.id, job.ID, phase, result.Artifact); err != nil {
			return err
		}
		*phaseStatus(job, phase) = jobs.PhaseSucceeded
	}
	return nil
}
