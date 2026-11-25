package worker

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

func TestInvalidTrackerWindow(t *testing.T) {
	start := time.Now()
	errA, errB := errors.New("a"), errors.New("b")
	tracker := newInvalidTracker()
	step := func(err error, at time.Duration) (time.Duration, bool) {
		tracker.tick()
		if err == nil {
			return 0, false
		}
		return tracker.observe(jobs.PhasePreview, err, start.Add(at))
	}
	if _, fresh := step(errA, 0); !fresh {
		t.Fatal("first error should open a window")
	}
	if elapsed, fresh := step(errA, 11*time.Second); fresh || elapsed < 10*time.Second {
		t.Fatalf("same error should age the window: %v %v", elapsed, fresh)
	}
	if _, fresh := step(errB, 12*time.Second); !fresh {
		t.Fatal("a different error must restart the window")
	}
	step(nil, 13*time.Second)
	if _, fresh := step(errB, 30*time.Second); !fresh {
		t.Fatal("a clean read in between must restart the window")
	}
}

func TestFailPendingCleansInbox(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	results, retained := testArtifacts(t, inbox, filepath.Join(root, "artifacts"))
	dir := filepath.Join(inbox, "pj_x")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	job := jobs.Job{ID: "pj_x", PreviewPhase: jobs.PhaseResult{Status: jobs.PhasePending}}
	w := New("test", &recordingStore{job: job}, nil, results, retained, time.Second, time.Millisecond)
	if err := w.failPending(t.Context(), job, "capture_timeout", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("inbox not cleaned: %v", err)
	}
}
