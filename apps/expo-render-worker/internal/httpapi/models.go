package httpapi

import (
	"factorio-expo/apps/expo-render-worker/internal/jobs"
	"factorio-expo/internal/runnerapi"
)

// The job request/response bodies are runnerapi's, shared with expo-api's
// client; only the runner-local health shapes live here.

type healthResponse struct {
	Service string `json:"service"`
	Status  string `json:"status"`
}

// jobResponse projects the internal job model onto the runner API shape,
// attaching failure and artifact details only when they exist and exposing a
// download href only while the artifact is still available.
func jobResponse(job jobs.Job) runnerapi.Job {
	response := runnerapi.Job{
		ID: job.ID, State: job.State,
		CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
		Phases: runnerapi.JobPhases{
			Preview:  phaseResponse(job.PreviewPhase),
			Analysis: phaseResponse(job.AnalysisPhase),
		},
		Failure: failure(job.FailureCode, job.FailureDetail),
	}
	if artifact := job.PreviewArtifact; artifact != nil && artifact.Phase == jobs.PhasePreview {
		response.Artifact = &runnerapi.Artifact{
			State: artifact.State, MediaType: artifact.MediaType, Size: artifact.Size,
			SHA256: artifact.SHA256, Width: artifact.Width, Height: artifact.Height,
			ExpiresAt: artifact.ExpiresAt,
		}
		if artifact.State == jobs.ArtifactAvailable {
			response.Artifact.Href = runnerapi.ArtifactPath(job.ID)
		}
	}
	return response
}

func phaseResponse(phase jobs.PhaseResult) runnerapi.PhaseResult {
	return runnerapi.PhaseResult{Status: phase.Status, Failure: failure(phase.FailureCode, phase.FailureDetail)}
}

func failure(code, detail string) *runnerapi.Failure {
	if code == "" {
		return nil
	}
	return &runnerapi.Failure{Code: code, Detail: detail, Retryable: jobs.Retryable(code)}
}
