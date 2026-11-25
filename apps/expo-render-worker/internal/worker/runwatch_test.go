package worker

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/bridge"
	"factorio-expo/apps/expo-render-worker/internal/bridge/bridgetest"
	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

const (
	idleStatus    = `{"status":"ok","protocol_version":1,"capture":{"connected":true}}`
	runningStatus = `{"status":"ok","protocol_version":1,"capture":{"connected":true},"active_job":{"job_id":"pj_test","phase":"stabilizing","requested_phases":["preview","analysis"],"phases":{"preview":"pending","analysis":"pending"}}}`
	acceptedBoth  = `{"status":"accepted","job_id":"pj_test","requested_phases":["preview","analysis"],"paths":{"preview_image":"previews/pj_test/preview.png","preview_result":"previews/pj_test/preview-result.json","analysis_result":"previews/pj_test/analysis-result.json"}}`
	completedBoth = `{"status":"ok","protocol_version":1,"capture":{"connected":true},"completed_job":` + acceptedBoth + `}`
)

// scriptedBridge accepts job pj_test, then answers each status command with
// the next of statuses (the last one repeats); "" is an unreachable bridge.
func scriptedBridge(statuses ...string) *bridge.Client {
	started, next := false, 0
	return bridge.NewClient(bridgetest.Commander(func(command string) (string, error) {
		switch {
		case strings.HasPrefix(command, bridge.CommandStart+" "):
			started = true
			return acceptedBoth, nil
		case command != bridge.CommandStatus:
			return `{"status":"accepted","job_id":"pj_test"}`, nil
		case !started:
			return idleStatus, nil
		}
		status := statuses[min(next, len(statuses)-1)]
		next++
		if status == "" {
			return "", errors.New("connection refused")
		}
		return status, nil
	}))
}

// statusOf decodes a status reply the way the worker gets it.
func statusOf(t *testing.T, reply string) bridge.Status {
	t.Helper()
	status, err := bridge.NewClient(bridgetest.Commander(func(string) (string, error) {
		return reply, nil
	})).Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func bothPhasesPending() jobs.Job {
	return jobs.Job{ID: "pj_test", Blueprint: "0a",
		PreviewPhase:  jobs.PhaseResult{Status: jobs.PhasePending},
		AnalysisPhase: jobs.PhaseResult{Status: jobs.PhasePending}}
}

// A Factorio server that restarts mid-job reloads its save and forgets the
// job: once it answers again, the job fails at once instead of waiting out the
// job timeout, and for good, since the blueprint almost always crashed it.
func TestFactorioRestartEndsJobForGood(t *testing.T) {
	w, _ := newPhaseWorker(t)
	store := w.store.(*recordingStore)
	store.job = bothPhasesPending()
	w.captureTimeout = time.Minute
	w.bridge = scriptedBridge(runningStatus, "", "", idleStatus)
	started := time.Now()
	if err := w.runNext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 10*time.Second {
		t.Fatalf("the lost job ended only after %v", time.Since(started))
	}
	if !slices.Equal(store.codes, []string{restartedCode, restartedCode}) {
		t.Fatalf("failure codes = %v, want factorio_restarted for both phases", store.codes)
	}
	if jobs.Retryable(restartedCode) {
		t.Fatal("factorio_restarted must not be retryable")
	}
}

// While Factorio does not answer, the job keeps waiting: it may come back
// with the job still running (a slow RCON reply), so only the job timeout ends it.
func TestUnreachableBridgeWaitsForJobTimeout(t *testing.T) {
	w, _ := newPhaseWorker(t)
	store := w.store.(*recordingStore)
	store.job = bothPhasesPending()
	w.captureTimeout = 50 * time.Millisecond
	w.bridge = scriptedBridge(runningStatus, "")
	if err := w.runNext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(store.codes, []string{"capture_timeout", "capture_timeout"}) {
		t.Fatalf("failure codes = %v, want capture_timeout", store.codes)
	}
}

// A client killed after the capture never writes the preview marker, and the
// server still counts it as connected for ~20 s, so the bridge completes the
// job: the missing result ends the run after missingResultGrace.
func TestCompletedJobWithoutResultEnds(t *testing.T) {
	status := statusOf(t, completedBoth)
	job := bothPhasesPending()
	noResults := func() bool { return false }
	var watch runWatch
	start := time.Now()
	if _, _, ended := watch.ended(job, status, start, noResults); ended {
		t.Fatal("a just completed job must wait for its results")
	}
	if _, _, ended := watch.ended(job, status, start.Add(missingResultGrace-time.Second), noResults); ended {
		t.Fatal("ended inside the grace window")
	}
	if _, _, ended := watch.ended(job, status, start.Add(missingResultGrace), func() bool { return true }); ended {
		t.Fatal("a result file on disk is the phase reader's to settle")
	}
	code, _, ended := watch.ended(job, status, start.Add(missingResultGrace), noResults)
	if !ended || code != missingCode || !jobs.Retryable(code) {
		t.Fatalf("after the grace window: ended=%v code=%q, want retryable result_missing", ended, code)
	}
}

func TestRunWatchKeepsRunningJobs(t *testing.T) {
	job := bothPhasesPending()
	running, idle := statusOf(t, runningStatus), statusOf(t, idleStatus)
	var watch runWatch
	later := time.Now().Add(time.Hour)
	if _, _, ended := watch.ended(job, running, later, func() bool { return false }); ended {
		t.Fatal("a job the bridge is running must not end")
	}
	// Results written before a restart are read, not discarded.
	if _, _, ended := watch.ended(job, idle, later, func() bool { return true }); ended {
		t.Fatal("a forgotten job with result files must be left to the phase reader")
	}
}

// After Factorio comes back (server restarted, client reconnected) the worker
// claims again on its own.
func TestClaimsAgainOnceBridgeRecovers(t *testing.T) {
	w, _ := newPhaseWorker(t)
	store := w.store.(*recordingStore)
	store.interrupted = []string{} // claim nothing, only count the attempts
	statuses := []string{"", `{"status":"ok","protocol_version":1,"capture":{"connected":false}}`, idleStatus}
	next := 0
	w.bridge = bridge.NewClient(bridgetest.Commander(func(string) (string, error) {
		status := statuses[next]
		next++
		if status == "" {
			return "", errors.New("connection refused")
		}
		return status, nil
	}))
	for range statuses {
		if err := w.runNext(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if store.claims != 1 {
		t.Fatalf("claims = %d, want 1 once the bridge is ready again", store.claims)
	}
}
