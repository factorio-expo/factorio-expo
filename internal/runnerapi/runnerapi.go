// Package runnerapi is the wire contract of the runner (expo-render-worker)
// HTTP API: the routes and the JSON bodies that expo-api encodes as the client
// and the runner decodes as the server, and back. Both sides import these
// types, so the two can no longer drift. Wire shape only: no runner or catalog
// imports. Imported by `expo-api` and `expo-render-worker` (both Dockerfiles
// copy `internal/`).
package runnerapi

import "time"

// JobsPath is the job collection; the other paths name one job's resources.
const JobsPath = "/v1/preview-jobs"

func JobPath(id string) string      { return JobsPath + "/" + id }
func ArtifactPath(id string) string { return JobPath(id) + "/artifact" }
func AnalysisPath(id string) string { return JobPath(id) + "/analysis" }

// IdempotencyKeyHeader makes a repeated create return the job the first
// request made instead of queueing a second one.
const IdempotencyKeyHeader = "Idempotency-Key"

// JobState is the state of the whole runner job.
type JobState string

const (
	JobQueued    JobState = "queued"
	JobRunning   JobState = "running"
	JobSucceeded JobState = "succeeded"
	JobFailed    JobState = "failed"
)

// Phase is one unit of work a job can be asked for.
type Phase string

const (
	PhasePreview  Phase = "preview"
	PhaseAnalysis Phase = "analysis"
)

// PhaseStatus is the state of one phase of a job.
type PhaseStatus string

const (
	PhaseNotRequested PhaseStatus = "not_requested"
	PhasePending      PhaseStatus = "pending"
	PhaseSucceeded    PhaseStatus = "succeeded"
	PhaseFailed       PhaseStatus = "failed"
)

// ArtifactState is whether a retained artifact can still be downloaded.
type ArtifactState string

const (
	ArtifactAvailable ArtifactState = "available"
	ArtifactDeleted   ArtifactState = "deleted"
	ArtifactExpired   ArtifactState = "expired"
)

// CreateJobRequest is the POST JobsPath body. Omitted phases means both; an
// empty list is rejected.
type CreateJobRequest struct {
	Blueprint string  `json:"blueprint"`
	Phases    []Phase `json:"phases,omitempty"`
}

// Job is the body of a create (202) and of GET JobPath. A job runs once: a
// failed job stays failed, and the API retries a retryable failure with a new
// job.
type Job struct {
	ID        string    `json:"id"`
	State     JobState  `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Failure   *Failure  `json:"failure,omitempty"`
	// Artifact is the preview image, once one was retained.
	Artifact *Artifact `json:"artifact,omitempty"`
	Phases   JobPhases `json:"phases"`
}

type JobPhases struct {
	Preview  PhaseResult `json:"preview"`
	Analysis PhaseResult `json:"analysis"`
}

type PhaseResult struct {
	Status  PhaseStatus `json:"status"`
	Failure *Failure    `json:"failure,omitempty"`
}

type Failure struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
	// Retryable says whether running the same blueprint again can succeed
	// (the runner's one classification of codes); the API retries only these.
	Retryable bool `json:"retryable"`
}

type Artifact struct {
	State ArtifactState `json:"state"`
	// Href is ArtifactPath(job id), present only while State is available.
	Href      string    `json:"href,omitempty"`
	MediaType string    `json:"media_type"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Problem is every runner error body (RFC 7807 style, `application/problem+json`)
// with the runner's stable machine-readable Code.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// ReadyPath is the readiness probe: 200 when ready, 503 with the same body
// when not. expo-api reads it for the staff console's Overview.
const ReadyPath = "/readyz"

// Readiness is the readiness body: an overall status ("ready" or
// "not_ready") and one status per component (database, artifacts, worker,
// bridge, capture), each "ready", "busy", "unavailable", "incompatible" or
// "unknown".
type Readiness struct {
	Service    string                     `json:"service"`
	Status     string                     `json:"status"`
	Components map[string]ComponentStatus `json:"components"`
}

// ComponentStatus is one readiness component.
type ComponentStatus struct {
	Status string `json:"status"`
}
