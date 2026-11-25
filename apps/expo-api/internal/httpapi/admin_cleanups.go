package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/maintenance"
)

// Cleanups (System -> Cleanups): every cleanup's dry run, and running one.

// cleanupOverviewInput is the eviction target of the full_previews dry run.
type cleanupOverviewInput struct {
	TargetBytes string `query:"target_bytes" pattern:"^[0-9]{1,18}$" doc:"The size in bytes the full previews eviction goes down to; omit for no target."`
}

type cleanupOverviewOutput struct {
	Body maintenance.CleanupOverview
}

// cleanupRunInput names the cleanup to run now.
type cleanupRunInput struct {
	Name string `path:"name" doc:"The cleanup's name, as the overview lists it."`
	Body runCleanupRequest
}

type runCleanupRequest struct {
	// TargetBytes is the size full_previews evicts down to.
	TargetBytes *int64 `json:"target_bytes,omitempty" doc:"The size in bytes full_previews evicts down to; required for that cleanup."`
	Reason      string `json:"reason,omitempty" doc:"Why, recorded in the audit log."`
}

type cleanupRunOutput struct {
	Body maintenance.CleanupResult
}

// cleanupOverview is every cleanup's dry run; ?target_bytes= sets the size
// the full-preview eviction would go down to.
func (s *adminServer) cleanupOverview(ctx context.Context, in *cleanupOverviewInput) (*cleanupOverviewOutput, error) {
	var target *int64
	if in.TargetBytes != "" {
		// The pattern allows digits only, at most 18 of them: it fits an int64.
		value, _ := strconv.ParseInt(in.TargetBytes, 10, 64)
		target = &value
	}
	overview, err := s.cleanups.Overview(ctx, target)
	if err != nil {
		return nil, err
	}
	return &cleanupOverviewOutput{Body: overview}, nil
}

// runCleanup runs one cleanup now and answers what it did.
func (s *adminServer) runCleanup(ctx context.Context, in *cleanupRunInput) (*cleanupRunOutput, error) {
	result, err := s.cleanups.Run(ctx, staffUser(ctx), in.Name, in.Body.TargetBytes, in.Body.Reason)
	if err != nil {
		return nil, err
	}
	return &cleanupRunOutput{Body: result}, nil
}

// registerAdminCleanups registers the Cleanups operations.
func (s *adminServer) registerAdminCleanups(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getCleanupOverview", Method: http.MethodGet, Path: "/admin/cleanups",
		Tags:    []string{"Cleanups"},
		Summary: "Cleanup dry runs",
		Description: "What every cleanup would remove now, without removing anything. " +
			"target_bytes sets the size the full previews dry run evicts down to.",
	}), s.cleanupOverview)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "runCleanup", Method: http.MethodPost, Path: "/admin/cleanups/{name}",
		Tags:         []string{"Cleanups"},
		Summary:      "Run a cleanup",
		Description:  "Runs one cleanup now and answers what it removed.",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusNotFound},
	}), s.runCleanup)
}
