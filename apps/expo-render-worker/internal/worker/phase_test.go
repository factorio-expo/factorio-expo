package worker

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/artifacts"
	"factorio-expo/apps/expo-render-worker/internal/bridge"
	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

func newPhaseWorker(t *testing.T) (*Worker, string) {
	t.Helper()
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	results, retained := testArtifacts(t, inbox, filepath.Join(root, "artifacts"))
	return New("test", &recordingStore{}, nil, results, retained, time.Second, time.Millisecond), inbox
}

func TestReadPhasePendingWithoutResult(t *testing.T) {
	w, _ := newPhaseWorker(t)
	tracker := newInvalidTracker()
	tracker.tick()
	result := w.readPhase(t.Context(), jobs.Job{ID: "pj_a"}, jobs.PhaseAnalysis, nil, tracker)
	if result.State != phasePending {
		t.Fatalf("state = %v, want pending", result.State)
	}
}

func TestReadPhaseInvalidResultFailsAfterGrace(t *testing.T) {
	w, inbox := newPhaseWorker(t)
	dir := filepath.Join(inbox, "pj_b")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "analysis-result.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	job := jobs.Job{ID: "pj_b"}
	tracker := newInvalidTracker()
	tracker.tick()
	if r := w.readPhase(t.Context(), job, jobs.PhaseAnalysis, nil, tracker); r.State != phasePending {
		t.Fatalf("first invalid read = %v, want pending", r.State)
	}
	tracker.tick()
	if r := w.readPhase(t.Context(), job, jobs.PhaseAnalysis, nil, tracker); r.State != phasePending {
		t.Fatalf("inside grace = %v, want pending", r.State)
	}
	window := tracker.windows[jobs.PhaseAnalysis]
	window.since = time.Now().Add(-invalidResultGrace - time.Second)
	tracker.windows[jobs.PhaseAnalysis] = window
	tracker.tick()
	r := w.readPhase(t.Context(), job, jobs.PhaseAnalysis, nil, tracker)
	if r.State != phaseFailed || r.Code != "invalid_result" {
		t.Fatalf("after grace = %+v, want failed invalid_result", r)
	}
}

func TestSettlePhaseRecordsStatus(t *testing.T) {
	w, _ := newPhaseWorker(t)
	store := w.store.(*recordingStore)
	job := jobs.Job{ID: "pj_c", PreviewPhase: jobs.PhaseResult{Status: jobs.PhasePending},
		AnalysisPhase: jobs.PhaseResult{Status: jobs.PhasePending}}
	if err := w.settlePhase(t.Context(), &job, jobs.PhasePreview, failedResult("x", "y")); err != nil {
		t.Fatal(err)
	}
	if err := w.settlePhase(t.Context(), &job, jobs.PhaseAnalysis, phaseResult{State: phaseSucceeded, Artifact: &jobs.Artifact{}}); err != nil {
		t.Fatal(err)
	}
	if job.PreviewPhase.Status != jobs.PhaseFailed || job.AnalysisPhase.Status != jobs.PhaseSucceeded ||
		store.failed != 1 || store.completed != 1 {
		t.Fatalf("job %+v store %+v", job, store)
	}
}

// testArtifacts opens the inbox and retained store a worker test reads and
// writes.
func testArtifacts(t *testing.T, inboxPath, retainedPath string) (*artifacts.Inbox, *artifacts.Store) {
	t.Helper()
	inbox, err := artifacts.NewInbox(inboxPath, 1<<20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inboxPath, bridge.ClientHeartbeatFile), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	retained, err := artifacts.NewStore(retainedPath, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return inbox, retained
}
