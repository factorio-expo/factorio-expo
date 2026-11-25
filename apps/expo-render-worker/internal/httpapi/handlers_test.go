package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"factorio-expo/apps/expo-render-worker/internal/bridge/bridgetest"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/artifacts"
	"factorio-expo/apps/expo-render-worker/internal/bridge"
	"factorio-expo/apps/expo-render-worker/internal/config"
	"factorio-expo/apps/expo-render-worker/internal/jobs"
	"factorio-expo/apps/expo-render-worker/internal/store"
	"factorio-expo/apps/expo-render-worker/internal/worker"
	"factorio-expo/internal/runnerapi"
)

func TestCreatePreviewJobWhileFactorioUnavailable(t *testing.T) {
	root := t.TempDir()
	databaseURL := os.Getenv("TEST_RUNNER_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_RUNNER_DATABASE_URL (migrated runner database) to run PostgreSQL runner handler tests")
	}
	jobStore, err := store.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = jobStore.Close() })
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `TRUNCATE runner_artifacts, runner_jobs RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	inbox, err := artifacts.NewInbox(filepath.Join(root, "inbox"), 1<<20, 100)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := artifacts.NewStore(filepath.Join(root, "artifacts"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	bridgeClient := bridge.NewClient(bridgetest.Commander(func(command string) (string, error) {
		if command != "/bp-preview-status" {
			t.Fatalf("unexpected command: %s", command)
		}
		return `{"status":"ok","protocol_version":1,"capture":{"connected":false},"active_job":null}`, nil
	}))
	cfg := config.Config{
		MaxRequestBytes: 1 << 20, RCONTimeout: time.Second,
		RetentionInterval: time.Hour,
	}
	previewWorker := worker.New(
		"test", jobStore, bridgeClient, inbox, retained, time.Minute, time.Millisecond,
	)
	srv := newServer(cfg, jobStore, bridgeClient, inbox, retained, previewWorker)
	request := httptest.NewRequest(http.MethodPost, "/v1/preview-jobs",
		bytes.NewBufferString(`{"blueprint":"0ZXhhbXBsZQ=="}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()

	srv.routes().ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.HasPrefix(response.Header().Get("Location"), "/v1/preview-jobs/pj_") {
		t.Fatalf("location = %q", response.Header().Get("Location"))
	}
	var job runnerapi.Job
	if err := json.NewDecoder(response.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	if job.State != "queued" {
		t.Fatalf("job = %#v", job)
	}
}

func TestCreatePreviewJobRequiresJSON(t *testing.T) {
	t.Parallel()
	srv := &server{cfg: config.Config{MaxRequestBytes: 1 << 20}}
	request := httptest.NewRequest(http.MethodPost, "/v1/preview-jobs", strings.NewReader("blueprint"))
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()

	srv.createPreviewJob(response, request)

	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestValidPhases(t *testing.T) {
	t.Parallel()
	defaults, err := validPhases(nil)
	if err != nil || len(defaults) != 2 {
		t.Fatalf("default phases = %#v, %v", defaults, err)
	}
	for _, phases := range [][]jobs.Phase{{}, {jobs.PhasePreview, jobs.PhasePreview}, {"unknown"}} {
		if _, err := validPhases(phases); err == nil {
			t.Fatalf("phases %#v were accepted", phases)
		}
	}
}

func TestJobResponseNeverExposesAnalysisAsPreviewArtifact(t *testing.T) {
	t.Parallel()
	job := jobs.Job{
		ID: "pj_test",
		PreviewArtifact: &jobs.Artifact{
			JobID: "pj_test", Phase: jobs.PhaseAnalysis,
			State: jobs.ArtifactDeleted, MediaType: "application/json",
		},
	}

	if response := jobResponse(job); response.Artifact != nil {
		t.Fatalf("artifact = %#v, want nil", response.Artifact)
	}
}

// Every failure says whether another run can succeed: the API retries only
// those, so a blueprint at fault fails at once and an outage is retried.
func TestJobResponseFlagsRetryableFailures(t *testing.T) {
	t.Parallel()
	for code, retryable := range map[string]bool{
		jobs.InterruptedCode:          true,
		"capture_timeout":             true,
		"bridge_submission_failed":    true,
		"factorio_client_unavailable": true,
		"result_missing":              true,
		"factorio_restarted":          false,
		"invalid_blueprint":           false,
		"blueprint_too_large":         false,
		"invalid_result":              false,
		"result_mismatch":             false,
		"analysis_failed":             false,
	} {
		job := jobs.Job{
			ID: "pj_test", State: jobs.StateFailed, FailureCode: code,
			PreviewPhase:  jobs.PhaseResult{Requested: true, Status: jobs.PhaseFailed, FailureCode: code},
			AnalysisPhase: jobs.PhaseResult{Requested: true, Status: jobs.PhaseSucceeded},
		}
		response := jobResponse(job)
		if response.Failure == nil || response.Failure.Retryable != retryable ||
			response.Phases.Preview.Failure == nil || response.Phases.Preview.Failure.Retryable != retryable {
			t.Errorf("%s: failure = %+v, preview = %+v; want retryable %v",
				code, response.Failure, response.Phases.Preview.Failure, retryable)
		}
		if response.Phases.Analysis.Failure != nil {
			t.Errorf("%s: succeeded analysis carries a failure", code)
		}
	}
}
