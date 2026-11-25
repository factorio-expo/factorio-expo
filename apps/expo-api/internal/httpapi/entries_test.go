package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	xwebp "golang.org/x/image/webp"

	"factorio-expo/apps/expo-api/internal/accountdeletion"
	"factorio-expo/apps/expo-api/internal/accounts"
	"factorio-expo/apps/expo-api/internal/auth"
	"factorio-expo/apps/expo-api/internal/banhold"
	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/httpapi"
	"factorio-expo/apps/expo-api/internal/preview"
	"factorio-expo/apps/expo-api/internal/previewimage"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/apps/expo-api/internal/runner"
	"factorio-expo/apps/expo-api/internal/sessions"
	"factorio-expo/apps/expo-api/internal/staffops"
	"factorio-expo/apps/expo-api/internal/storage"
)

// stubPNG is the stub runner's preview: a real 4x2 PNG, so the import can
// encode it.
func stubPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 31)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// stubRunner stands in for the preview runner: it accepts any job, reports it
// as completed immediately with the preview rendered and the analysis failed
// for good (so every phase settles), and serves stubPNG as the artifact.
func stubRunner(t *testing.T) *httptest.Server {
	t.Helper()
	image := stubPNG(t)
	digest := sha256.Sum256(image)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/preview-jobs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]any{
			"id": "job-1", "state": "queued",
		})
	})
	mux.HandleFunc("GET /v1/preview-jobs/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "job-1", "state": "succeeded",
			"phases": map[string]any{
				"preview": map[string]any{"status": "succeeded"},
				"analysis": map[string]any{"status": "failed", "failure": map[string]any{
					"code": "analysis_failed", "detail": "stub", "retryable": false,
				}},
			},
			"artifact": map[string]any{
				"state": "available", "href": "/v1/preview-jobs/job-1/artifact",
				"media_type": "image/png", "sha256": hex.EncodeToString(digest[:]),
				"width": 4, "height": 2,
			},
		})
	})
	mux.HandleFunc("GET /v1/preview-jobs/{id}/artifact",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Write(image)
		})
	mux.HandleFunc("DELETE /v1/preview-jobs/{id}/artifact",
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// TestMain serializes the database-backed packages against each other. Every
// test truncates the catalog tables, so two packages sharing TEST_DATABASE_URL
// would otherwise delete each other's rows mid-test. A session-level advisory
// lock held for the whole binary makes the packages take turns.
func TestMain(m *testing.M) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		os.Exit(m.Run())
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	lock, err := pool.Acquire(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "acquire: %v\n", err)
		os.Exit(1)
	}
	defer lock.Release()
	if _, err := lock.Exec(ctx, "SELECT pg_advisory_lock($1)", catalogTestLockID); err != nil {
		fmt.Fprintf(os.Stderr, "lock: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	_, _ = lock.Exec(ctx, "SELECT pg_advisory_unlock($1)", catalogTestLockID)
	lock.Release()
	pool.Close()
	os.Exit(code)
}

// catalogTestLockID is an arbitrary shared key; any package touching the
// catalog tables in tests must use this same value.
const catalogTestLockID = 8175021

// newTestAPI wires the real API handler against a throwaway database and the
// stub runner, returning the handler and the dispatcher so a test can drive
// render import without waiting on the background poller.
func newTestAPI(t *testing.T) (http.Handler, *preview.Dispatcher) {
	t.Helper()
	return newTestAPIWith(t, nil)
}

// newTestAPIWith is newTestAPI with a hook to adjust dependencies (rate
// limiter, cookie settings) before the handler is built.
func newTestAPIWith(
	t *testing.T, adjust func(*httpapi.Dependencies),
) (http.Handler, *preview.Dispatcher) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(context.Background(),
		"TRUNCATE entries, revisions, books, blueprints, previews, phase_jobs, preview_files, ip_bans, ban_holds CASCADE",
	); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	runnerClient := runner.NewClient(stubRunner(t).URL, 5*time.Second)
	hub := progress.NewHub()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := preview.NewFiles(store, db.New(pool))
	var dispatcher *preview.Dispatcher
	holds := banhold.New("test-ip-secret")
	catalogService := catalog.New(pool, catalog.Options{
		Progress: hub, WorkQueued: func() { dispatcher.Nudge() }, BanHolds: holds,
	})
	dispatcher = preview.New(pool, runnerClient, files, catalogService, preview.Options{
		PollInterval: time.Millisecond, Progress: hub,
	})
	deps := httpapi.Dependencies{
		Progress:        hub,
		AllowedOrigin:   testSite,
		SiteURL:         testSite,
		Runner:          runnerClient,
		Pool:            pool,
		Sessions:        sessions.New(pool, auth.NewTokenIssuer("test-secret", ""), time.Hour),
		Flows:           auth.NewFlowSigner("test-secret", ""),
		Accounts:        accounts.New(pool, holds),
		Identities:      testIdentities(t, nil),
		Passkeys:        testPasskeys(t),
		Catalog:         catalogService,
		AccountDeletion: accountdeletion.New(catalogService, holds),
		PreviewFiles:    files,
		Renders: preview.NewRenders(files,
			preview.NewRequeuer(db.New(pool), catalogService, hub, func() { dispatcher.Nudge() }), catalogService),
	}
	if adjust != nil {
		adjust(&deps)
	}
	return httpapi.New(deps), dispatcher
}

func postJSON(t *testing.T, handler http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestSubmitAndRenderEntry(t *testing.T) {
	handler, dispatcher := newTestAPI(t)

	exchange, err := os.ReadFile("../../../../test-data/book-starter-kit.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	created := postJSON(t, handler, "/api/v1/entries", map[string]string{
		"blueprint": string(exchange),
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create entry status = %d, body = %s", created.Code, created.Body)
	}
	var entry catalog.Entry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatalf("decode entry: %v", err)
	}

	pollUntilImported(t, dispatcher)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/entries/"+entry.ID, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("get entry status = %d", recorder.Code)
	}
	var reloaded catalog.Entry
	if err := json.Unmarshal(recorder.Body.Bytes(), &reloaded); err != nil {
		t.Fatalf("decode reloaded entry: %v", err)
	}

	leaves := reloaded.Revision.Root.Leaves()
	if len(leaves) == 0 {
		t.Fatal("expected renderable leaves")
	}
	for _, test := range []struct {
		url           string
		width, height int
	}{
		{catalog.PreviewURL(leaves[0].ID), 4, 2},
		{catalog.ThumbnailURL(leaves[0].ID), 4, 2},
	} {
		imageRequest := httptest.NewRequest(http.MethodGet, test.url, nil)
		imageRecorder := httptest.NewRecorder()
		handler.ServeHTTP(imageRecorder, imageRequest)
		if imageRecorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", test.url, imageRecorder.Code, imageRecorder.Body)
		}
		if got := imageRecorder.Header().Get("Content-Type"); got != "image/webp" {
			t.Fatalf("%s content type = %q", test.url, got)
		}
		if got := imageRecorder.Header().Get("Cache-Control"); got != "public, max-age=86400" {
			t.Fatalf("%s cache control = %q", test.url, got)
		}
		config, err := xwebp.DecodeConfig(imageRecorder.Body)
		if err != nil || config.Width != test.width || config.Height != test.height {
			t.Fatalf("%s = %+v, %v", test.url, config, err)
		}

		// A revalidation with the ETag is answered without the body.
		revalidate := httptest.NewRequest(http.MethodGet, test.url, nil)
		revalidate.Header.Set("If-None-Match", imageRecorder.Header().Get("ETag"))
		revalidated := httptest.NewRecorder()
		handler.ServeHTTP(revalidated, revalidate)
		if revalidated.Code != http.StatusNotModified {
			t.Fatalf("%s revalidation status = %d", test.url, revalidated.Code)
		}
	}
}

func TestDeleteEntryRequiresToken(t *testing.T) {
	handler, _ := newTestAPI(t)

	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	created := postJSON(t, handler, "/api/v1/entries", map[string]string{
		"blueprint": string(exchange),
	})
	var entry catalog.Entry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatalf("decode entry: %v", err)
	}

	unauthorized := httptest.NewRequest(http.MethodDelete, "/api/v1/entries/"+entry.ID, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, unauthorized)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("delete without token status = %d, want 403", recorder.Code)
	}

	authorized := httptest.NewRequest(http.MethodDelete, "/api/v1/entries/"+entry.ID, nil)
	authorized.Header.Set("X-Edit-Token", entry.EditToken)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete with token status = %d, want 204", recorder.Code)
	}
}

// TestCreateEntryRejectsUnknownVisibility checks that a visibility the entries
// CHECKs would refuse is a 400, not a constraint violation surfacing as 500.
func TestCreateEntryRejectsUnknownVisibility(t *testing.T) {
	handler, _ := newTestAPI(t)

	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	created := postJSON(t, handler, "/api/v1/entries", map[string]string{
		"blueprint": string(exchange), "visibility": "hidden",
	})
	if created.Code != http.StatusBadRequest {
		t.Fatalf("create with visibility hidden = %d, want 400: %s", created.Code, created.Body)
	}
}

// TestCreateEntryRequiresBlueprint checks that an empty exchange string is a
// schema failure (422 validation_failed naming the body field) on every
// upload route, not a hand-written 400.
func TestCreateEntryRequiresBlueprint(t *testing.T) {
	handler, _ := newTestAPI(t)

	for _, path := range []string{"/api/v1/entries", "/api/v1/previews"} {
		recorder := postJSON(t, handler, path, map[string]string{"blueprint": ""})
		var problem struct {
			Code   string `json:"code"`
			Errors []struct {
				Location string `json:"location"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
			t.Fatalf("%s decode problem: %v, %s", path, err, recorder.Body)
		}
		if recorder.Code != http.StatusUnprocessableEntity || problem.Code != "validation_failed" ||
			len(problem.Errors) != 1 || problem.Errors[0].Location != "body.blueprint" {
			t.Fatalf("%s empty blueprint = %d %+v, want 422 validation_failed on body.blueprint", path, recorder.Code, problem)
		}
	}
}

// pollUntilImported drives the dispatcher until no preview or analysis intent
// is pending: the first poll submits the runner jobs, a later one starts the
// background preview import and the next collects it. The test dispatcher
// uses a 1 ms interval, so a short pause makes the rows due again.
func pollUntilImported(t *testing.T, dispatcher *preview.Dispatcher) {
	t.Helper()
	if err := previewimage.Check(); err != nil {
		t.Skipf("preview encoder unavailable: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := dispatcher.PollOnce(context.Background()); err != nil {
			t.Fatalf("poll: %v", err)
		}
		var pending int
		if err := pool.QueryRow(context.Background(), `
			SELECT count(*) FROM phase_jobs WHERE state = 'pending'`,
		).Scan(&pending); err != nil {
			t.Fatalf("count pending jobs: %v", err)
		}
		if pending == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d phases still pending", pending)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// opsFor builds the staff operations over a test catalog (built with the
// "test-ip-secret" ban holds), for tests that change settings or delete
// accounts as staff.
func opsFor(service *catalog.Service) *staffops.Service {
	return staffops.New(service, accountdeletion.New(service, banhold.New("test-ip-secret")))
}
