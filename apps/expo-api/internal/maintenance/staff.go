package maintenance

import (
	"context"
	"errors"
	"fmt"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/fault"
)

var (
	// ErrUnknown is returned for a cleanup name that is not a staff task.
	ErrUnknown = fault.New(fault.NotFound, "not_found", "unknown cleanup")
	// ErrTargetRequired is returned when running a cleanup that takes a target
	// size without one, or with a negative one.
	ErrTargetRequired = fault.New(fault.Invalid, "invalid_request", "target_bytes is required and must not be negative")
)

// StaffView is how a task shows in the console's Cleanups page: a title, what
// it does and its dry run.
type StaffView struct {
	Title       string
	Description string
	// TakesTarget marks the cleanup that needs target_bytes.
	TakesTarget bool
	// Count is the dry run: what a run would remove or update now. target is
	// the size a TakesTarget cleanup would go down to (nil: none given).
	Count func(ctx context.Context, target *int64) (Pending, error)
}

// Pending is what a dry run found.
type Pending struct {
	Count int64
	// Bytes they hold, where known.
	Bytes *int64
}

// CleanupStatus is one cleanup and its dry run.
type CleanupStatus struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Pending is how many items a run would remove or update now.
	Pending int64 `json:"pending"`
	// PendingBytes is the bytes they hold, where known.
	PendingBytes *int64 `json:"pending_bytes,omitempty"`
	// TakesTarget is set for the cleanup that needs target_bytes.
	TakesTarget bool `json:"takes_target"`
}

// CleanupPreviewTotals are the recorded preview files.
type CleanupPreviewTotals struct {
	TotalBytes int64 `json:"total_bytes"`
	FullBytes  int64 `json:"full_bytes"`
	FullFiles  int64 `json:"full_files"`
}

// CleanupOverview is every cleanup's dry run.
type CleanupOverview struct {
	Cleanups []CleanupStatus      `json:"cleanups"`
	Previews CleanupPreviewTotals `json:"previews"`
	// TargetBytes is the eviction target the dry run used; nil when none was
	// given (a TakesTarget cleanup's Pending is then 0).
	TargetBytes *int64 `json:"target_bytes"`
}

// CleanupResult is what a run did.
type CleanupResult struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
	// Bytes freed, where known.
	Bytes *int64 `json:"bytes,omitempty"`
}

// StaffDeps are what the staff view needs beside the tasks.
type StaffDeps struct {
	// Totals reads the recorded preview files, shown above the cleanups.
	Totals func(ctx context.Context) (CleanupPreviewTotals, error)
	// Audit records staff actions (*catalog.Service).
	Audit interface {
		Add(ctx context.Context, by audit.Staff, record audit.Record) error
	}
}

// staffTask finds a listed task by name.
func (r *Registry) staffTask(name string) (Task, bool) {
	for _, task := range r.tasks {
		if task.Staff != nil && task.Name == name {
			return task, true
		}
	}
	return Task{}, false
}

// CleanupOverview runs every listed task's dry run. targetBytes, when set, is the
// size the target-taking cleanup evicts down to.
func (r *Registry) Overview(ctx context.Context, targetBytes *int64) (CleanupOverview, error) {
	if targetBytes != nil && *targetBytes < 0 {
		return CleanupOverview{}, ErrTargetRequired
	}
	totals, err := r.staff.Totals(ctx)
	if err != nil {
		return CleanupOverview{}, fmt.Errorf("read preview totals: %w", err)
	}
	overview := CleanupOverview{Previews: totals, TargetBytes: targetBytes}
	for _, task := range r.tasks {
		view := task.Staff
		if view == nil {
			continue
		}
		status := CleanupStatus{Name: task.Name, Title: view.Title, Description: view.Description, TakesTarget: view.TakesTarget}
		if view.TakesTarget && targetBytes == nil {
			// No target: nothing to count (its Pending is 0).
			overview.Cleanups = append(overview.Cleanups, status)
			continue
		}
		pending, err := view.Count(ctx, targetBytes)
		if err != nil {
			return CleanupOverview{}, fmt.Errorf("count %s: %w", task.Name, err)
		}
		status.Pending, status.PendingBytes = pending.Count, pending.Bytes
		overview.Cleanups = append(overview.Cleanups, status)
	}
	return overview, nil
}

// Run runs one listed cleanup for staff. The audit log records the request
// before anything is removed, so no run goes unaudited, and the outcome
// after, even when the run failed part way.
func (r *Registry) Run(ctx context.Context, by audit.Staff, name string, targetBytes *int64, reason string) (CleanupResult, error) {
	reason, err := audit.CleanReason(reason)
	if err != nil {
		return CleanupResult{}, err
	}
	task, ok := r.staffTask(name)
	if !ok {
		return CleanupResult{}, ErrUnknown
	}
	if task.Staff.TakesTarget && (targetBytes == nil || *targetBytes < 0) {
		return CleanupResult{}, ErrTargetRequired
	}
	request := map[string]any{}
	if task.Staff.TakesTarget {
		request["target_bytes"] = *targetBytes
	}
	if err := r.staff.Audit.Add(ctx, by, audit.Record{
		Action: "cleanup.run", TargetType: "cleanup", TargetID: name, Reason: reason, After: request,
	}); err != nil {
		return CleanupResult{}, err
	}
	result, runErr := task.Run(ctx, targetBytes)
	result.Name = name
	outcome := map[string]any{"count": result.Count}
	if result.Bytes != nil {
		outcome["bytes"] = *result.Bytes
	}
	if runErr != nil {
		outcome["error"] = true
	}
	if err := r.staff.Audit.Add(ctx, by, audit.Record{
		Action: "cleanup.result", TargetType: "cleanup", TargetID: name, Reason: reason, After: outcome,
	}); err != nil {
		return result, errors.Join(runErr, err)
	}
	return result, runErr
}
