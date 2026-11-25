package preview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/previewimage"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/apps/expo-api/internal/renderqueue"
	"factorio-expo/apps/expo-api/internal/runner"
	"factorio-expo/apps/expo-api/internal/storage"
	"factorio-expo/internal/analysisresult"
	"factorio-expo/internal/runnerapi"
)

// fakeQueries records every outbox write and Catalog call the dispatcher
// makes and serves one fixed claim, so a test reads the persisted outcome of a
// poll.
type fakeQueries struct {
	mu        sync.Mutex
	claim     []db.ClaimDispatchJobsRow
	calls     []string
	retry     map[string]db.RetryPhaseJobParams
	released  map[string]time.Time
	failed    map[string]string
	failedMsg map[string]string
	upserted  *analysisresult.Stored
	assignErr error
	// storeRefused makes StorePreview report that no row took the files.
	storeRefused bool
	queued       []string
	// reused is what ReuseWaitingResults reports once.
	reused []renderqueue.ReusedLeaf
}

func newFakeQueries(rows ...db.ClaimDispatchJobsRow) *fakeQueries {
	return &fakeQueries{
		claim: rows, retry: map[string]db.RetryPhaseJobParams{},
		released: map[string]time.Time{}, failed: map[string]string{}, failedMsg: map[string]string{},
	}
}

func (f *fakeQueries) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeQueries) called(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.calls, call)
}

func (f *fakeQueries) ClaimDispatchJobs(context.Context, db.ClaimDispatchJobsParams) ([]db.ClaimDispatchJobsRow, error) {
	return f.claim, nil
}

// phaseName is the capitalised phase the recorded call names use.
func phaseName(phase string) string {
	if phase == slotAnalysis {
		return "Analysis"
	}
	return "Preview"
}

func (f *fakeQueries) DeletePhaseJob(_ context.Context, arg db.DeletePhaseJobParams) error {
	f.record("Delete" + phaseName(arg.Phase) + "Job")
	return nil
}

func (f *fakeQueries) RetryPhaseJob(_ context.Context, arg db.RetryPhaseJobParams) error {
	f.record("Retry" + phaseName(arg.Phase) + "Job")
	f.retry[arg.Phase] = arg
	return nil
}

func (f *fakeQueries) ReleasePhaseJob(_ context.Context, arg db.ReleasePhaseJobParams) error {
	f.record("Release" + phaseName(arg.Phase) + "Job")
	f.released[arg.Phase] = arg.AvailableAt.Time
	return nil
}

func (f *fakeQueries) MarkPhaseJobFailed(_ context.Context, arg db.MarkPhaseJobFailedParams) error {
	f.record("Mark" + phaseName(arg.Phase) + "JobFailed")
	f.failed[arg.Phase], f.failedMsg[arg.Phase] = arg.FailureCode, arg.FailureDetail
	return nil
}

// AnalysisFailed is the catalog's one transaction: the job is marked failed
// together with the stale production.
func (f *fakeQueries) AnalysisFailed(_ context.Context, _, code, detail string) error {
	f.record("AnalysisFailed")
	return f.MarkPhaseJobFailed(context.Background(), db.MarkPhaseJobFailedParams{
		Phase: slotAnalysis, FailureCode: code, FailureDetail: detail,
	})
}

func (f *fakeQueries) RejectQuickPreview(context.Context, string) error {
	f.record("RejectQuickPreview")
	return nil
}

// LeafProgress applies the real rule to what the fake recorded, as the
// catalog does to the stored rows.
func (f *fakeQueries) LeafProgress(_ context.Context, blueprintID string) (progress.Leaf, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	facts := progress.LeafFacts{BlueprintID: blueprintID, HasProduction: f.upserted != nil}
	for _, call := range f.calls {
		if call == "StorePreview" && !f.storeRefused {
			facts.HasPreview = true
		}
	}
	if code, ok := f.failed["preview"]; ok {
		facts.PreviewJobState, facts.PreviewFailureCode = "failed", code
	}
	if code, ok := f.failed["analysis"]; ok {
		facts.AnalysisJobState, facts.AnalysisFailureCode = "failed", code
	}
	return progress.LeafOf(facts), true, nil
}

func (f *fakeQueries) AssignRunnerPhaseJob(_ context.Context, arg db.AssignRunnerPhaseJobParams) error {
	if arg.Phase == slotAnalysis {
		f.record("AssignRunnerAnalysisJob")
	} else {
		f.record("AssignRunnerPhaseJobs")
	}
	return f.assignErr
}

func (f *fakeQueries) SettleRunnerJob(_ context.Context, runnerJobID pgtype.Text) error {
	f.record("SettleRunnerJob " + runnerJobID.String)
	return nil
}

func (f *fakeQueries) ReuseWaitingResults(context.Context) ([]renderqueue.ReusedLeaf, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reused := f.reused
	f.reused = nil
	return reused, nil
}

// StorePreview records the call; with storeRefused no row takes the files,
// which the catalog then queues for the sweep.
func (f *fakeQueries) StorePreview(_ context.Context, _ string, sha string, _, _ int) error {
	f.record("StorePreview")
	if f.storeRefused {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.queued = append(f.queued, sha)
	}
	return nil
}

func (f *fakeQueries) StoreProduction(_ context.Context, _ string, production analysisresult.Stored) error {
	f.record("StoreProduction")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upserted = &production
	return nil
}

// fakeRunner is an httptest runner. Handlers are replaced per test.
type fakeRunner struct {
	mu          sync.Mutex
	create      func(w http.ResponseWriter, r *http.Request)
	get         func(w http.ResponseWriter, r *http.Request)
	analysis    func(w http.ResponseWriter, r *http.Request)
	createKeys  []string
	createCalls int
	deleted     bool
	// image is served as the preview artifact; previewDeleted records its
	// release.
	image          []byte
	imageFetches   int
	previewDeleted bool
}

func (f *fakeRunner) client(t *testing.T) *runner.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/preview-jobs":
			f.mu.Lock()
			f.createCalls++
			f.createKeys = append(f.createKeys, r.Header.Get("Idempotency-Key"))
			f.mu.Unlock()
			f.create(w, r)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/artifact"):
			f.mu.Lock()
			f.previewDeleted = true
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/artifact"):
			f.mu.Lock()
			f.imageFetches++
			image := f.image
			f.mu.Unlock()
			if image == nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"title": "not found"})
				return
			}
			_, _ = w.Write(image)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/analysis"):
			f.mu.Lock()
			f.deleted = true
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/analysis"):
			f.analysis(w, r)
		default:
			f.get(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return runner.NewClient(server.URL, 5*time.Second)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func idOf(n byte) string { return fmt.Sprintf("%012d", n) }

func text(value string) pgtype.Text { return pgtype.Text{String: value, Valid: true} }

func count(value int32) pgtype.Int4 { return pgtype.Int4{Int32: value, Valid: true} }

// freshRow is a newly enqueued leaf: both phases pending, due, no runner job.
func freshRow() db.ClaimDispatchJobsRow {
	return db.ClaimDispatchJobsRow{
		BlueprintID: idOf(1), PreviewClaimed: true, AnalysisClaimed: true,
		PreviewState: text("pending"), AnalysisState: text("pending"),
		PreviewRetryCount: count(0), AnalysisRetryCount: count(0), ExchangeString: "0eNqrVkrKKU0tKMrMTS1WskrKL0nNVQIAAA==",
	}
}

// inFlightRow has both phases submitted as runner job "rj".
func inFlightRow() db.ClaimDispatchJobsRow {
	row := freshRow()
	row.PreviewRunnerJobID, row.AnalysisRunnerJobID = text("rj"), text("rj")
	return row
}

func poll(t *testing.T, queries *fakeQueries, fake *fakeRunner) {
	t.Helper()
	dispatcher := newDispatcher(queries, queries, queries, fake.client(t), testFiles(t), Options{PollInterval: 3 * time.Second})
	if err := dispatcher.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// requireEncoder skips a test that encodes real previews when cwebp is not
// installed (the API image carries it; see the Dockerfile).
func requireEncoder(t *testing.T) {
	t.Helper()
	if err := previewimage.Check(); err != nil {
		t.Skipf("preview encoder unavailable: %v", err)
	}
}

func testFiles(t *testing.T) *Files {
	t.Helper()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewFiles(store, newFakeRecords())
}

// pollUntil polls one dispatcher until done reports true: a preview import
// runs in the background and is collected by a later poll.
func pollUntil(t *testing.T, dispatcher *Dispatcher, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("timed out polling")
		}
		if err := dispatcher.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// pngImage is a real 6x3 PNG and its artifact metadata as the runner reports it.
func pngImage(t *testing.T) ([]byte, *runnerapi.Artifact) {
	t.Helper()
	return pngImageSized(t, 6, 3)
}

func pngImageSized(t *testing.T, width, height int) ([]byte, *runnerapi.Artifact) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 17)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(out.Bytes())
	return out.Bytes(), &runnerapi.Artifact{
		State: runnerapi.ArtifactAvailable, Href: runnerapi.ArtifactPath("rj"),
		SHA256: hex.EncodeToString(digest[:]), MediaType: "image/png", Width: width, Height: height,
	}
}

func jobResponse(preview, analysis runnerapi.PhaseStatus) runnerapi.Job {
	return runnerapi.Job{
		ID: "rj", State: "running",
		Phases: runnerapi.JobPhases{Preview: runnerapi.PhaseResult{Status: preview}, Analysis: runnerapi.PhaseResult{Status: analysis}},
	}
}

// A runner 422 (invalid_blueprint) can never succeed: both phases fail on the
// first attempt, with the runner's code and detail, instead of three retries.
func TestDispatchPermanentRunnerRejectionFailsWithoutRetry(t *testing.T) {
	t.Parallel()
	queries := newFakeQueries(freshRow())
	fake := &fakeRunner{create: func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"code": "invalid_blueprint", "title": "Unprocessable", "detail": "bad string",
		})
	}}
	poll(t, queries, fake)
	if queries.called("RetryPreviewJob") || queries.called("RetryAnalysisJob") {
		t.Fatalf("permanent rejection was retried: %v", queries.calls)
	}
	if queries.failed["preview"] != "invalid_blueprint" || queries.failed["analysis"] != "invalid_blueprint" ||
		!strings.Contains(queries.failedMsg["analysis"], "bad string") {
		t.Fatalf("failed = %v / %v", queries.failed, queries.failedMsg)
	}
	if !queries.called("AnalysisFailed") {
		t.Fatal("failed analysis must mark production stale")
	}
	if !queries.called("RejectQuickPreview") {
		t.Fatal("a failed phase must reject a quick preview")
	}
}

func TestDispatchTransientRunnerErrorRetriesWithinBudget(t *testing.T) {
	t.Parallel()
	queries := newFakeQueries(freshRow())
	fake := &fakeRunner{create: func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"title": "down"})
	}}
	poll(t, queries, fake)
	if !queries.called("RetryPreviewJob") || !queries.called("RetryAnalysisJob") ||
		queries.retry["preview"].FailureCode != "dispatch_failed" {
		t.Fatalf("calls = %v, retry = %v", queries.calls, queries.retry)
	}
}

func TestDispatchExhaustedBudgetFailsPhase(t *testing.T) {
	t.Parallel()
	row := freshRow()
	row.PreviewRetryCount = count(maxRetries)
	queries := newFakeQueries(row)
	fake := &fakeRunner{create: func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusBadGateway, map[string]string{"title": "bad gateway"})
	}}
	poll(t, queries, fake)
	if queries.failed["preview"] != "dispatch_failed" || !queries.called("RetryAnalysisJob") {
		t.Fatalf("calls = %v, failed = %v", queries.calls, queries.failed)
	}
}

func TestDispatchRecordsRunnerJobAndSendsStableIdempotencyKey(t *testing.T) {
	t.Parallel()
	queries := newFakeQueries(freshRow())
	fake := &fakeRunner{create: func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusAccepted, runnerapi.Job{ID: "rj"})
	}}
	poll(t, queries, fake)
	if !queries.called("AssignRunnerPhaseJobs") || !queries.called("AssignRunnerAnalysisJob") ||
		!queries.called("ReleasePreviewJob") || !queries.called("ReleaseAnalysisJob") {
		t.Fatalf("calls = %v", queries.calls)
	}
	// Same attempt repeated (record failed, then re-dispatched): same key.
	failing := newFakeQueries(freshRow())
	failing.assignErr = errors.New("database unavailable")
	poll(t, failing, fake)
	poll(t, failing, fake)
	if len(fake.createKeys) != 3 || fake.createKeys[0] == "" ||
		fake.createKeys[0] != fake.createKeys[1] || fake.createKeys[1] != fake.createKeys[2] {
		t.Fatalf("keys = %v", fake.createKeys)
	}
	if failing.called("RetryPreviewJob") || !failing.called("ReleasePreviewJob") {
		t.Fatalf("a failed record must wait, not spend budget: %v", failing.calls)
	}
	// A counted retry is a new attempt: new key.
	retried := freshRow()
	retried.PreviewRetryCount = count(1)
	poll(t, newFakeQueries(retried), fake)
	if fake.createKeys[3] == fake.createKeys[0] {
		t.Fatalf("key did not change after a retry: %v", fake.createKeys)
	}
}

func TestOnlyClaimedPhasesAreDispatched(t *testing.T) {
	t.Parallel()
	row := freshRow()
	row.AnalysisClaimed = false // analysis is in backoff
	queries := newFakeQueries(row)
	fake := &fakeRunner{create: func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Phases []string `json:"phases"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Phases) != 1 || body.Phases[0] != "preview" {
			t.Errorf("requested phases = %v", body.Phases)
		}
		writeJSON(w, http.StatusAccepted, runnerapi.Job{ID: "rj"})
	}}
	poll(t, queries, fake)
	if queries.called("AssignRunnerAnalysisJob") || queries.called("ReleaseAnalysisJob") {
		t.Fatalf("unclaimed analysis phase was touched: %v", queries.calls)
	}
}

// Analysis failures retry like preview failures: the API retries exactly what
// the runner flags retryable, and a final failure keeps its code and detail.
func TestAnalysisPhaseFailureFollowsTheRetryableFlag(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		code      string
		wantRetry bool
	}{
		{"capture_timeout", true},
		{"interrupted", true},
		{"analysis_failed", false},
		{"invalid_blueprint", false},
	} {
		queries := newFakeQueries(inFlightRow())
		fake := &fakeRunner{get: func(w http.ResponseWriter, _ *http.Request) {
			job := jobResponse(runnerapi.PhasePending, runnerapi.PhaseFailed)
			job.Phases.Analysis.Failure = &runnerapi.Failure{Code: test.code, Detail: "boom", Retryable: test.wantRetry}
			writeJSON(w, http.StatusOK, job)
		}}
		poll(t, queries, fake)
		if got := queries.called("RetryAnalysisJob"); got != test.wantRetry {
			t.Fatalf("%s: retried = %v (calls %v)", test.code, got, queries.calls)
		}
		if !test.wantRetry && (queries.failed["analysis"] != test.code || queries.failedMsg["analysis"] != "boom") {
			t.Fatalf("%s: failed = %v / %v", test.code, queries.failed, queries.failedMsg)
		}
	}
}

// An analysis import failing on I/O must not stop the preview import, and must
// not spend retry budget.
func TestAnalysisImportErrorDoesNotBlockPreviewImport(t *testing.T) {
	requireEncoder(t)
	t.Parallel()
	queries := newFakeQueries(inFlightRow())
	image, artifact := pngImage(t)
	fake := &fakeRunner{
		image: image,
		get: func(w http.ResponseWriter, _ *http.Request) {
			job := jobResponse(runnerapi.PhaseSucceeded, runnerapi.PhaseSucceeded)
			job.State = runnerapi.JobSucceeded
			job.Artifact = artifact
			writeJSON(w, http.StatusOK, job)
		},
		analysis: func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
	}
	dispatcher := newDispatcher(queries, queries, queries, fake.client(t), testFiles(t), Options{PollInterval: 3 * time.Second})
	pollUntil(t, dispatcher, func() bool { return queries.called("DeletePreviewJob") })
	if !queries.called("StorePreview") || !queries.called("DeletePreviewJob") {
		t.Fatalf("preview not imported: %v", queries.calls)
	}
	if !queries.called("ReleaseAnalysisJob") || queries.called("RetryAnalysisJob") || queries.called("MarkAnalysisJobFailed") {
		t.Fatalf("analysis import error must wait without spending budget: %v", queries.calls)
	}
	if !queries.released["analysis"].After(time.Now().Add(10 * time.Second)) {
		t.Fatalf("analysis release has no backoff: %v", queries.released["analysis"])
	}
}

// A finished import releases the runner's artifact before the next poll
// collects it; the runner then drops the job (404), which must not read as a
// lost job (a retry, so a re-render).
func TestFinishedImportIsCollectedAfterTheRunnerDroppedTheJob(t *testing.T) {
	requireEncoder(t)
	t.Parallel()
	row := inFlightRow()
	row.AnalysisClaimed = false
	queries := newFakeQueries(row)
	image, artifact := pngImage(t)
	fake := &fakeRunner{image: image}
	fake.get = func(w http.ResponseWriter, _ *http.Request) {
		fake.mu.Lock()
		dropped := fake.previewDeleted
		fake.mu.Unlock()
		if dropped {
			writeJSON(w, http.StatusNotFound, map[string]string{"title": "not found"})
			return
		}
		job := jobResponse(runnerapi.PhaseSucceeded, runnerapi.PhaseSucceeded)
		job.State = runnerapi.JobSucceeded
		job.Artifact = artifact
		writeJSON(w, http.StatusOK, job)
	}
	dispatcher := newDispatcher(queries, queries, queries, fake.client(t), testFiles(t), Options{PollInterval: 3 * time.Second})
	pollUntil(t, dispatcher, func() bool {
		return queries.called("DeletePreviewJob") || queries.called("RetryPreviewJob")
	})
	if !queries.called("DeletePreviewJob") || queries.called("RetryPreviewJob") {
		t.Fatalf("finished import not collected: %v", queries.calls)
	}
}

// The same with the runner still holding the job: it shows the artifact
// deleted, which must not read as a succeeded job that lost its artifact.
func TestFinishedImportIsCollectedAfterTheArtifactIsReleased(t *testing.T) {
	requireEncoder(t)
	t.Parallel()
	row := inFlightRow()
	row.AnalysisClaimed = false
	queries := newFakeQueries(row)
	image, artifact := pngImage(t)
	fake := &fakeRunner{image: image}
	fake.get = func(w http.ResponseWriter, _ *http.Request) {
		job := jobResponse(runnerapi.PhaseSucceeded, runnerapi.PhaseSucceeded)
		job.State = runnerapi.JobSucceeded
		// Long past the missing-artifact grace.
		job.UpdatedAt = time.Now().Add(-time.Hour)
		released := *artifact
		fake.mu.Lock()
		if fake.previewDeleted {
			released.State = runnerapi.ArtifactDeleted
		}
		fake.mu.Unlock()
		job.Artifact = &released
		writeJSON(w, http.StatusOK, job)
	}
	dispatcher := newDispatcher(queries, queries, queries, fake.client(t), testFiles(t), Options{PollInterval: 3 * time.Second})
	pollUntil(t, dispatcher, func() bool {
		return queries.called("DeletePreviewJob") || queries.called("RetryPreviewJob")
	})
	if !queries.called("DeletePreviewJob") || queries.called("RetryPreviewJob") {
		t.Fatalf("finished import not collected: %v", queries.calls)
	}
}

func TestAnalysisImportStoresGoldenResult(t *testing.T) {
	t.Parallel()
	golden, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "test-data", "analysis", "analysis-succeeded.json"))
	if err != nil {
		t.Fatal(err)
	}
	row := inFlightRow()
	row.AnalysisRunnerJobID = text("pj_golden")
	row.PreviewClaimed = false
	queries := newFakeQueries(row)
	fake := &fakeRunner{
		get: func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, jobResponse(runnerapi.PhasePending, runnerapi.PhaseSucceeded))
		},
		analysis: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(golden) },
	}
	poll(t, queries, fake)
	if queries.upserted == nil || queries.upserted.State != "complete" ||
		!queries.called("DeleteAnalysisJob") || !fake.deleted {
		t.Fatalf("calls = %v, upserted = %+v", queries.calls, queries.upserted)
	}
}

func TestInvalidAnalysisResultIsPermanent(t *testing.T) {
	t.Parallel()
	row := inFlightRow()
	row.PreviewClaimed = false
	queries := newFakeQueries(row)
	fake := &fakeRunner{
		get: func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, jobResponse(runnerapi.PhasePending, runnerapi.PhaseSucceeded))
		},
		analysis: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"schema_version":1}`)) },
	}
	poll(t, queries, fake)
	if queries.failed["analysis"] != "invalid_result" || queries.called("RetryAnalysisJob") {
		t.Fatalf("calls = %v, failed = %v", queries.calls, queries.failed)
	}
}

func TestLostRunnerJobRetriesBothPhases(t *testing.T) {
	t.Parallel()
	queries := newFakeQueries(inFlightRow())
	fake := &fakeRunner{get: func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"title": "not found"})
	}}
	poll(t, queries, fake)
	if !queries.called("RetryPreviewJob") || !queries.called("RetryAnalysisJob") ||
		queries.retry["preview"].FailureCode != "job_lost" {
		t.Fatalf("calls = %v retry = %v", queries.calls, queries.retry)
	}
}

func TestRunnerPollErrorWaitsWithoutSpendingBudget(t *testing.T) {
	t.Parallel()
	queries := newFakeQueries(inFlightRow())
	fake := &fakeRunner{get: func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}}
	poll(t, queries, fake)
	if queries.called("RetryPreviewJob") || !queries.called("ReleasePreviewJob") || !queries.called("ReleaseAnalysisJob") {
		t.Fatalf("calls = %v", queries.calls)
	}
}

func TestPreviewFailureFollowsTheRetryableFlag(t *testing.T) {
	t.Parallel()
	for _, retryable := range []bool{false, true} {
		queries := newFakeQueries(inFlightRow())
		fake := &fakeRunner{get: func(w http.ResponseWriter, _ *http.Request) {
			job := jobResponse(runnerapi.PhaseFailed, runnerapi.PhasePending)
			job.Phases.Preview.Failure = &runnerapi.Failure{Code: "result_mismatch", Detail: "zoom differs", Retryable: retryable}
			writeJSON(w, http.StatusOK, job)
		}}
		poll(t, queries, fake)
		if retryable {
			if !queries.called("RetryPreviewJob") || queries.retry["preview"].FailureCode != "result_mismatch" {
				t.Fatalf("retryable: calls = %v retry = %v", queries.calls, queries.retry)
			}
			continue
		}
		if queries.failed["preview"] != "result_mismatch" || queries.failedMsg["preview"] != "zoom differs" ||
			queries.called("RetryPreviewJob") {
			t.Fatalf("final: calls = %v failed = %v", queries.calls, queries.failed)
		}
	}
}

// A runner job frees its dispatch window slot once the runner reports it
// finished, before its results are imported; a running one keeps it.
func TestFinishedRunnerJobIsSettled(t *testing.T) {
	t.Parallel()
	for _, state := range []runnerapi.JobState{runnerapi.JobState("running"), runnerapi.JobSucceeded, runnerapi.JobFailed} {
		queries := newFakeQueries(inFlightRow())
		fake := &fakeRunner{get: func(w http.ResponseWriter, _ *http.Request) {
			job := jobResponse(runnerapi.PhasePending, runnerapi.PhasePending)
			job.State = state
			writeJSON(w, http.StatusOK, job)
		}}
		poll(t, queries, fake)
		if settled := queries.called("SettleRunnerJob rj"); settled != (state != "running") {
			t.Fatalf("%s: settled = %v (%v)", state, settled, queries.calls)
		}
	}
}

// Before each claim, leaves that took an identical leaf's result are told to
// open streams; a copied final failure deletes a quick preview.
func TestClaimPublishesReusedLeaves(t *testing.T) {
	t.Parallel()
	queries := newFakeQueries()
	queries.failed["preview"] = "invalid_blueprint"
	queries.reused = []renderqueue.ReusedLeaf{
		{EntryID: "entry", BlueprintID: "b", Phase: progress.PhasePreview, FailureCode: "invalid_blueprint"},
	}
	hub := progress.NewHub()
	sub := hub.Subscribe("entry")
	defer sub.Close()
	dispatcher := newDispatcher(queries, queries, queries, (&fakeRunner{}).client(t), testFiles(t),
		Options{PollInterval: 3 * time.Second, Progress: hub})
	if err := dispatcher.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if change := <-sub.Changes(); change.BlueprintID != "b" || change.State.Status != progress.StatusFailed ||
		change.State.Code != "invalid_blueprint" {
		t.Fatalf("published %+v", change)
	}
	if !queries.called("RejectQuickPreview") {
		t.Fatalf("a quick preview with a copied failure must be deleted: %v", queries.calls)
	}
}
