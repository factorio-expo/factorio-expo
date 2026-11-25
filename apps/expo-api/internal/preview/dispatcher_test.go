package preview

import (
	"testing"
	"time"

	"factorio-expo/internal/runnerapi"
)

func TestMissingTerminalPreviewArtifact(t *testing.T) {
	t.Parallel()
	now := time.Now()
	completed := runnerapi.Job{
		State:     runnerapi.JobSucceeded,
		UpdatedAt: now.Add(-missingArtifactGrace),
		Phases: runnerapi.JobPhases{
			Preview: runnerapi.PhaseResult{Status: runnerapi.PhaseSucceeded},
		},
	}

	if !missingTerminalPreviewArtifact(completed, now) {
		t.Fatal("terminal job without an artifact was not considered stale")
	}

	completed.UpdatedAt = now.Add(-missingArtifactGrace + time.Second)
	if missingTerminalPreviewArtifact(completed, now) {
		t.Fatal("job inside artifact grace period was considered stale")
	}

	completed.UpdatedAt = now.Add(-missingArtifactGrace)
	completed.Artifact = &runnerapi.Artifact{State: runnerapi.ArtifactAvailable}
	if missingTerminalPreviewArtifact(completed, now) {
		t.Fatal("job with an available artifact was considered stale")
	}
}

func TestMissingTerminalPreviewArtifactIgnoresActiveJobs(t *testing.T) {
	t.Parallel()
	now := time.Now()
	job := runnerapi.Job{
		State:     runnerapi.JobRunning,
		UpdatedAt: now.Add(-time.Hour),
		Phases: runnerapi.JobPhases{
			Preview: runnerapi.PhaseResult{Status: runnerapi.PhasePending},
		},
	}

	if missingTerminalPreviewArtifact(job, now) {
		t.Fatal("active job was considered stale")
	}
}
