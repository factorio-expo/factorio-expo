// Package maintenance is the one table of the API's housekeeping: the sweeps
// that run on a schedule and the cleanups staff run by hand from the console
// (System -> Cleanups, docs/admin.md). Each Task names what it does once;
// the Registry runs the scheduled ones and gives staff the dry run and the
// run of the listed ones. The tasks themselves are wired in cmd/expo-api
// (maintenance.go), so this package knows no domain code.
package maintenance

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Task is one piece of housekeeping.
type Task struct {
	// Name identifies the task in logs and, for a staff cleanup, in the
	// console and the audit log.
	Name string
	// Every schedules the task: it runs once at startup and then this
	// often. With Every unset, AtStart runs it once at startup; with
	// neither, it only runs when staff start it.
	Every   time.Duration
	AtStart bool
	// Run does the work. target is the size a cleanup that TakesTarget
	// goes down to; other tasks get nil. The Result's Name is filled in.
	Run func(ctx context.Context, target *int64) (CleanupResult, error)
	// Staff lists the task as a cleanup in the console; nil keeps it out.
	Staff *StaffView
}

// scheduled reports whether the task runs without staff.
func (t Task) scheduled() bool { return t.Every > 0 || t.AtStart }

// Registry holds the tasks.
type Registry struct {
	tasks []Task
	staff StaffDeps
}

// New builds a registry of tasks, in the order the console lists them.
func New(tasks []Task, staff StaffDeps) *Registry {
	return &Registry{tasks: tasks, staff: staff}
}

// Schedule runs every scheduled task until ctx ends and returns once the
// running ones have finished, so a caller can join it before closing what
// the tasks use.
func (r *Registry) Schedule(ctx context.Context) {
	var running sync.WaitGroup
	for _, task := range r.tasks {
		if !task.scheduled() {
			continue
		}
		running.Go(func() { r.loop(ctx, task) })
	}
	running.Wait()
}

func (r *Registry) loop(ctx context.Context, task Task) {
	run := func() {
		if _, err := task.Run(ctx, nil); err != nil && ctx.Err() == nil {
			slog.Error("maintenance task failed", "task", task.Name, "error", err)
		}
	}
	run()
	if task.Every <= 0 {
		return
	}
	ticker := time.NewTicker(task.Every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
