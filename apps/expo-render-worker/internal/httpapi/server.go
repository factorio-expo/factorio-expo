// Package httpapi implements the preview runner's internal HTTP surface:
// health and readiness probes, the preview job queue endpoints, and temporary
// artifact serving. It is internal-only; no route here is exposed publicly.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/artifacts"
	"factorio-expo/apps/expo-render-worker/internal/bridge"
	"factorio-expo/apps/expo-render-worker/internal/config"
	"factorio-expo/apps/expo-render-worker/internal/jobs"
	"factorio-expo/apps/expo-render-worker/internal/store"
	"factorio-expo/apps/expo-render-worker/internal/worker"
	"factorio-expo/internal/runnerapi"
)

// Dependencies carries the collaborators the runner HTTP layer needs. It is
// assembled by cmd/expo-render-worker so that this package never reads
// configuration or opens resources itself.
type Dependencies struct {
	Config    config.Config
	Store     *store.Store
	Bridge    *bridge.Client
	Inbox     *artifacts.Inbox
	Artifacts *artifacts.Store
	Worker    *worker.Worker
}

// Service exposes the runner's HTTP handler together with the background
// retention loop, both of which operate on the same dependencies.
type Service struct {
	server *server
}

type server struct {
	cfg       config.Config
	store     *store.Store
	bridge    *bridge.Client
	inbox     *artifacts.Inbox
	artifacts *artifacts.Store
	worker    *worker.Worker
}

// New wires the runner HTTP service from its dependencies.
func New(deps Dependencies) *Service {
	return &Service{server: newServer(
		deps.Config, deps.Store, deps.Bridge, deps.Inbox, deps.Artifacts, deps.Worker,
	)}
}

// Handler returns the fully decorated runner HTTP handler.
func (s *Service) Handler() http.Handler {
	return s.server.routes()
}

// RunRetention runs the artifact retention loop until ctx is cancelled.
func (s *Service) RunRetention(ctx context.Context) {
	s.server.runRetention(ctx)
}

func newServer(cfg config.Config, jobStore *store.Store, bridgeClient *bridge.Client,
	inbox *artifacts.Inbox, retained *artifacts.Store, previewWorker *worker.Worker) *server {
	return &server{
		cfg: cfg, store: jobStore, bridge: bridgeClient,
		inbox: inbox, artifacts: retained, worker: previewWorker,
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET "+runnerapi.ReadyPath, s.ready)
	job := runnerapi.JobPath("{id}")
	mux.HandleFunc("POST "+runnerapi.JobsPath, s.createPreviewJob)
	mux.HandleFunc("GET "+job, s.getPreviewJob)
	mux.HandleFunc("GET "+runnerapi.ArtifactPath("{id}"), s.getArtifact)
	mux.HandleFunc("DELETE "+runnerapi.ArtifactPath("{id}"), s.deleteArtifact)
	mux.HandleFunc("GET "+runnerapi.AnalysisPath("{id}"), s.getAnalysis)
	mux.HandleFunc("DELETE "+runnerapi.AnalysisPath("{id}"), s.deleteAnalysis)
	return withRequestLogging(mux)
}

// readiness probes every dependency the runner needs before it can accept
// preview work (PostgreSQL store, artifact directories, worker liveness,
// bridge protocol, Factorio client) and reports a per-component status plus an
// overall ready flag.
func (s *server) readiness(ctx context.Context) (runnerapi.Readiness, bool) {
	response := runnerapi.Readiness{
		Service: "expo-render-worker",
		Status:  "ready",
		Components: map[string]runnerapi.ComponentStatus{
			"database":  {Status: "ready"},
			"artifacts": {Status: "ready"},
			"worker":    {Status: "ready"},
			"bridge":    {Status: "ready"},
			"capture":   {Status: "ready"},
		},
	}
	ready := true
	if err := s.store.Ready(ctx); err != nil {
		response.Components["database"] = runnerapi.ComponentStatus{Status: "unavailable"}
		ready = false
	}
	if err := errors.Join(s.inbox.Ready(), s.artifacts.Ready()); err != nil {
		response.Components["artifacts"] = runnerapi.ComponentStatus{Status: "unavailable"}
		ready = false
	}
	if !s.worker.Healthy(time.Now()) {
		response.Components["worker"] = runnerapi.ComponentStatus{Status: "unavailable"}
		ready = false
	}
	status, err := s.bridge.Status(ctx)
	if err != nil {
		bridgeStatus := "unavailable"
		if errors.Is(err, bridge.ErrProtocolMismatch) {
			bridgeStatus = "incompatible"
		}
		response.Components["bridge"] = runnerapi.ComponentStatus{Status: bridgeStatus}
		response.Components["capture"] = runnerapi.ComponentStatus{Status: "unknown"}
		ready = false
	} else {
		if status.ActiveJob != nil {
			response.Components["bridge"] = runnerapi.ComponentStatus{Status: "busy"}
		}
		if !status.Capture.Connected || !s.inbox.ClientAlive(worker.ClientHeartbeatMaxAge) {
			response.Components["capture"] = runnerapi.ComponentStatus{Status: "unavailable"}
			ready = false
		}
	}
	if !ready {
		response.Status = "not_ready"
	}
	return response, ready
}

// runRetention periodically deletes artifacts whose TTL has elapsed, removing
// the file first and only then marking the row expired so a crash between the
// two steps leaves a recoverable (file-missing) row rather than a dangling file.
func (s *server) runRetention(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.RetentionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			expired, err := s.store.ExpiredArtifacts(ctx, time.Now().UTC())
			if err != nil {
				slog.Error("list expired artifacts", "error", err)
				continue
			}
			for _, artifact := range expired {
				if err := s.artifacts.Delete(artifact); err != nil {
					slog.Error("delete expired artifact", "job_id", artifact.JobID, "error", err)
					continue
				}
				if err := s.store.MarkArtifactDeleted(
					ctx, artifact.JobID, artifact.Phase, jobs.ArtifactExpired,
				); err != nil {
					slog.Error("mark artifact expired", "job_id", artifact.JobID, "error", err)
				}
			}
			if _, err := s.store.CleanupSucceededJobs(ctx); err != nil {
				slog.Error("cleanup succeeded preview jobs", "error", err)
			}
		}
	}
}

// removeArtifact deletes the artifact file and records the resulting state
// transition. Artifacts that are not currently available are a no-op so
// repeated deletes stay idempotent.
func (s *server) removeArtifact(ctx context.Context, artifact jobs.Artifact, state jobs.ArtifactState) error {
	if artifact.State != jobs.ArtifactAvailable {
		return nil
	}
	if err := s.artifacts.Delete(artifact); err != nil {
		return err
	}
	if err := s.store.MarkArtifactDeleted(ctx, artifact.JobID, artifact.Phase, state); err != nil {
		return fmt.Errorf("record artifact deletion: %w", err)
	}
	if _, err := s.store.CleanupSucceededJobs(ctx); err != nil {
		return fmt.Errorf("cleanup completed job: %w", err)
	}
	return nil
}

// ensureRegularFile rejects anything that is not a plain file (directories,
// symlinks, devices) so artifact serving cannot be redirected out of the
// artifact directory.
func ensureRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("artifact is not a regular file")
	}
	return nil
}
