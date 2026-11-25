package jobs

import (
	"time"

	"factorio-expo/internal/runnerapi"
)

// The job, phase and artifact vocabularies are the wire contract's
// (internal/runnerapi), so the runner's stored states and its API responses
// cannot disagree.
type (
	State         = runnerapi.JobState
	Phase         = runnerapi.Phase
	PhaseStatus   = runnerapi.PhaseStatus
	ArtifactState = runnerapi.ArtifactState
)

const (
	StateQueued    = runnerapi.JobQueued
	StateRunning   = runnerapi.JobRunning
	StateSucceeded = runnerapi.JobSucceeded
	StateFailed    = runnerapi.JobFailed

	PhasePreview  = runnerapi.PhasePreview
	PhaseAnalysis = runnerapi.PhaseAnalysis

	PhaseNotRequested = runnerapi.PhaseNotRequested
	PhasePending      = runnerapi.PhasePending
	PhaseSucceeded    = runnerapi.PhaseSucceeded
	PhaseFailed       = runnerapi.PhaseFailed

	ArtifactAvailable = runnerapi.ArtifactAvailable
	ArtifactDeleted   = runnerapi.ArtifactDeleted
	ArtifactExpired   = runnerapi.ArtifactExpired
)

type Job struct {
	ID               string
	Blueprint        string
	State            State
	FailureCode      string
	FailureDetail    string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	PreviewArtifact  *Artifact
	AnalysisArtifact *Artifact
	PreviewPhase     PhaseResult
	AnalysisPhase    PhaseResult
}

type PhaseResult struct {
	Requested     bool
	Status        PhaseStatus
	FailureCode   string
	FailureDetail string
}

type Artifact struct {
	JobID        string
	Phase        Phase
	RelativePath string
	State        ArtifactState
	MediaType    string
	Size         int64
	SHA256       string
	Width        int
	Height       int
	CreatedAt    time.Time
	ExpiresAt    time.Time
	DeletedAt    *time.Time
}
