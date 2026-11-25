package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/jobs"
)

// Pipeline -> Failed jobs: preview, analysis and search jobs that failed for
// good, and their re-runs.

type failedJobsResponse struct {
	Jobs []jobs.Failed `json:"jobs" doc:"The failed jobs, newest first."`
}

// staffFailedJobsInput filters the failed jobs by kind.
type staffFailedJobsInput struct {
	Kind string `query:"kind" enum:"preview,analysis,search" doc:"Keeps one kind of job; empty keeps every kind."`
}

type failedJobsOutput struct {
	Body failedJobsResponse
}

// staffJobRerunInput names the failed job to queue again.
type staffJobRerunInput struct {
	Kind     string `path:"kind" enum:"preview,analysis,search" doc:"The job's kind."`
	TargetID string `path:"targetID" doc:"The blueprint (preview, analysis) or entry (search) the job is for."`
	Body     staffReason
}

// listFailedJobs lists failed jobs newest first; ?kind= keeps one kind.
func (s *adminServer) listFailedJobs(ctx context.Context, in *staffFailedJobsInput) (*failedJobsOutput, error) {
	failed, err := s.jobs.ListFailed(ctx, in.Kind)
	if err != nil {
		return nil, err
	}
	return &failedJobsOutput{Body: failedJobsResponse{Jobs: failed}}, nil
}

// rerunJob queues one failed job again; 404 when it is not failed (any more).
func (s *adminServer) rerunJob(ctx context.Context, in *staffJobRerunInput) (*struct{}, error) {
	return nil, s.jobs.Rerun(ctx, staffUser(ctx), in.Kind, in.TargetID, in.Body.Reason)
}

// registerAdminJobs registers the Jobs operations.
func (s *adminServer) registerAdminJobs(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listFailedJobs", Method: http.MethodGet, Path: "/admin/jobs/failed",
		Tags:    []string{"Jobs"},
		Summary: "List failed jobs",
		Description: "Preview, analysis and search jobs that failed for good, newest first; " +
			"the kind filter is optional.",
	}), s.listFailedJobs)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "rerunJob", Method: http.MethodPost, Path: "/admin/jobs/{kind}/{targetID}/rerun",
		Tags:         []string{"Jobs"},
		Summary:      "Run a failed job again",
		Description:  "Queues one failed job again with a fresh retry budget.",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusNotFound},
	}), s.rerunJob)
}
