package worker

import (
	"context"
	"factorio-expo/apps/expo-render-worker/internal/bridge/bridgetest"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/bridge"
	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

type recordingStore struct {
	job               jobs.Job
	claims            int
	completed, failed int
	codes             []string // FailPhase codes, in order
	interrupted       []string // when set, ClaimNext fails these and claims nothing
}

func (s *recordingStore) ClaimNext(context.Context, string, time.Duration) (jobs.Job, bool, []string, error) {
	s.claims++
	if s.interrupted != nil {
		return jobs.Job{}, false, s.interrupted, nil
	}
	return s.job, true, nil, nil
}
func (s *recordingStore) CompletePhase(context.Context, string, string, jobs.Phase, *jobs.Artifact) error {
	s.completed++
	return nil
}
func (s *recordingStore) FailPhase(_ context.Context, _, _ string, _ jobs.Phase, code, _ string) error {
	s.failed++
	s.codes = append(s.codes, code)
	return nil
}

func TestAmbiguousStartResumesWithoutResubmission(t *testing.T) {
	for _, completed := range []bool{false, true} {
		t.Run(fmt.Sprint(completed), func(t *testing.T) {
			root, err := os.MkdirTemp(".", ".worker-test-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			inbox := filepath.Join(root, "inbox")
			results, retained := testArtifacts(t, inbox, filepath.Join(root, "artifacts"))
			store := &recordingStore{job: jobs.Job{ID: "pj_test", Blueprint: "0a",
				PreviewPhase: jobs.PhaseResult{Status: jobs.PhasePending}}}
			started, submissions, statuses := false, 0, 0
			client := bridge.NewClient(bridgetest.Commander(func(command string) (string, error) {
				if command == "/bp-preview-status" {
					statuses++
					if statuses == 2 {
						return "", fmt.Errorf("transient timeout")
					}
					if started && completed {
						return `{"status":"ok","protocol_version":1,"capture":{"connected":true},"completed_job":{"status":"accepted","job_id":"pj_test","requested_phases":["preview"],"paths":{"preview_result":"previews/pj_test/preview-result.json","preview_image":"previews/pj_test/preview.png"}}}`, nil
					}
					if started {
						return `{"status":"ok","protocol_version":1,"capture":{"connected":true},"active_job":{"job_id":"pj_test","phase":"preparing","requested_phases":["preview"],"phases":{"preview":"pending","analysis":"not_requested"}}}`, nil
					}
					return `{"status":"ok","protocol_version":1,"capture":{"connected":true}}`, nil
				}
				if strings.HasPrefix(command, "/bp-preview-start ") {
					submissions++
					started = true
					dir := filepath.Join(inbox, "pj_test")
					if err := os.MkdirAll(dir, 0755); err != nil {
						t.Fatal(err)
					}
					err := os.WriteFile(filepath.Join(dir, "preview-result.json"), []byte(`{"schema_version":1,"status":"failed","phase":"preview","job_id":"pj_test","error":{"code":"test_failure","message":"test failure"}}`), 0644)
					if err != nil {
						t.Fatal(err)
					}
					return "", fmt.Errorf("read RCON: timeout")
				}
				return `{"status":"accepted","job_id":"pj_test"}`, nil
			}))
			w := New("test", store, client, results, retained, time.Second, time.Millisecond)
			if err := w.runNext(context.Background()); err != nil {
				t.Fatal(err)
			}
			if submissions != 1 || store.failed != 1 {
				t.Fatalf("submissions=%d failures=%d", submissions, store.failed)
			}
		})
	}
}

// A job the worker cannot finish now would fail and spend the API's retry
// budget, so nothing is claimed until the bridge is ready.
func TestNoClaimUntilBridgeIsReady(t *testing.T) {
	for name, status := range map[string]string{
		"unreachable":            "",
		"busy":                   `{"status":"ok","protocol_version":1,"capture":{"connected":true},"active_job":{"job_id":"pj_old","phase":"preparing","requested_phases":["preview"],"phases":{"preview":"pending"}}}`,
		"client disconnected":    `{"status":"ok","protocol_version":1,"capture":{"connected":false}}`,
		"incompatible":           `{"status":"ok","protocol_version":99,"capture":{"connected":true}}`,
		"client heartbeat stale": `{"status":"ok","protocol_version":1,"capture":{"connected":true}}`,
	} {
		t.Run(name, func(t *testing.T) {
			w, inbox := newPhaseWorker(t)
			if name == "client heartbeat stale" {
				stale := time.Now().Add(-time.Minute)
				if err := os.Chtimes(filepath.Join(inbox, bridge.ClientHeartbeatFile), stale, stale); err != nil {
					t.Fatal(err)
				}
			}
			store := w.store.(*recordingStore)
			w.bridge = bridge.NewClient(bridgetest.Commander(func(string) (string, error) {
				if status == "" {
					return "", fmt.Errorf("connection refused")
				}
				return status, nil
			}))
			if err := w.runNext(t.Context()); err != nil {
				t.Fatal(err)
			}
			if store.claims != 0 {
				t.Fatalf("claimed %d jobs while the bridge was not ready", store.claims)
			}
		})
	}
}

// Jobs ClaimNext fails as interrupted lose their inboxes.
func TestInterruptedJobsRemoveInbox(t *testing.T) {
	w, inbox := newPhaseWorker(t)
	w.bridge = bridge.NewClient(bridgetest.Commander(func(string) (string, error) {
		return `{"status":"ok","protocol_version":1,"capture":{"connected":true}}`, nil
	}))
	store := w.store.(*recordingStore)
	if err := os.MkdirAll(filepath.Join(inbox, "pj_interrupted"), 0755); err != nil {
		t.Fatal(err)
	}
	store.interrupted = []string{"pj_interrupted"}
	if err := w.runNext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(inbox, "pj_interrupted")); !os.IsNotExist(err) {
		t.Fatalf("inbox of an interrupted job was kept: %v", err)
	}
}
