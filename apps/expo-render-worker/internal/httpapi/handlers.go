package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"factorio-expo/apps/expo-render-worker/internal/bridge"
	"factorio-expo/apps/expo-render-worker/internal/jobs"
	"factorio-expo/apps/expo-render-worker/internal/store"
	"factorio-expo/internal/runnerapi"
)

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Service: "expo-render-worker", Status: "ok"})
}

func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RCONTimeout)
	defer cancel()
	response, ready := s.readiness(ctx)
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, response)
}

// createPreviewJob validates and durably queues work before waking the worker.
// Factorio readiness is handled by the worker so temporary startup outages do
// not reject and lose otherwise valid jobs.
func (s *server) createPreviewJob(w http.ResponseWriter, r *http.Request) {
	var request runnerapi.CreateJobRequest
	if err := decodeJSON(w, r, s.cfg.MaxRequestBytes, &request); err != nil {
		status := http.StatusBadRequest
		code := "invalid_request"
		title := "Invalid request"
		if errors.Is(err, errUnsupportedMediaType) {
			status, code, title = http.StatusUnsupportedMediaType, "unsupported_media_type", "Unsupported media type"
		} else {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				status, code, title = http.StatusRequestEntityTooLarge, "request_too_large", "Request is too large"
			}
		}
		writeProblem(w, status, code, title, err.Error())
		return
	}
	if !validBlueprintExchangeString(request.Blueprint) {
		writeProblem(w, http.StatusUnprocessableEntity, "invalid_blueprint",
			"Invalid blueprint string", "A Factorio blueprint exchange string is required.")
		return
	}
	phases, err := validPhases(request.Phases)
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "invalid_phases", "Invalid phases", err.Error())
		return
	}

	job, err := s.store.CreateIdempotent(r.Context(), r.Header.Get(runnerapi.IdempotencyKeyHeader), request.Blueprint, phases)
	if err != nil {
		slog.Error("create preview job", "error", err)
		writeProblem(w, http.StatusServiceUnavailable, "queue_unavailable",
			"Preview queue is unavailable", "")
		return
	}

	s.worker.Wake()
	w.Header().Set("Location", runnerapi.JobPath(job.ID))
	writeJSON(w, http.StatusAccepted, jobResponse(job))
}

func validPhases(phases []jobs.Phase) ([]jobs.Phase, error) {
	if phases == nil {
		return []jobs.Phase{jobs.PhasePreview, jobs.PhaseAnalysis}, nil
	}
	if len(phases) == 0 {
		return nil, errors.New("phases must not be empty")
	}
	seen := make(map[jobs.Phase]bool, len(phases))
	for _, phase := range phases {
		if (phase != jobs.PhasePreview && phase != jobs.PhaseAnalysis) || seen[phase] {
			return nil, errors.New("phases contains an unknown or duplicate phase")
		}
		seen[phase] = true
	}
	return phases, nil
}

// validBlueprintExchangeString performs the cheap structural check the runner
// needs: a Factorio exchange string is a version marker byte followed by
// base64 data. Full decoding is the API's responsibility.
func validBlueprintExchangeString(value string) bool {
	if len(value) < 2 || value[0] != '0' {
		return false
	}
	for _, char := range value[1:] {
		if !((char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') ||
			(char >= '0' && char <= '9') || char == '+' || char == '/' || char == '=') {
			return false
		}
	}
	return true
}

func (s *server) getPreviewJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "job_not_found", "Preview job not found", "")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "job_lookup_failed",
			"Could not read preview job", "")
		return
	}
	writeJSON(w, http.StatusOK, jobResponse(job))
}

// getArtifact streams a rendered PNG. It distinguishes "unknown job", "job
// known but artifact not ready yet", and "artifact existed but is gone"
// (deleted or expired) so callers can retry appropriately.
func (s *server) getArtifact(w http.ResponseWriter, r *http.Request) {
	s.getPhaseArtifact(w, r, jobs.PhasePreview, bridge.PreviewImageFile)
}

func (s *server) getAnalysis(w http.ResponseWriter, r *http.Request) {
	s.getPhaseArtifact(w, r, jobs.PhaseAnalysis, bridge.AnalysisResultFile)
}

func (s *server) getPhaseArtifact(w http.ResponseWriter, r *http.Request, phase jobs.Phase, filename string) {
	jobID := r.PathValue("id")
	artifact, err := s.store.GetArtifact(r.Context(), jobID, phase)
	if errors.Is(err, store.ErrNotFound) {
		if _, jobErr := s.store.Get(r.Context(), jobID); errors.Is(jobErr, store.ErrNotFound) {
			writeProblem(w, http.StatusNotFound, "job_not_found", "Preview job not found", "")
		} else {
			writeProblem(w, http.StatusConflict, "artifact_not_ready", "Artifact is not ready", "")
		}
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "artifact_lookup_failed",
			"Could not read artifact", "")
		return
	}
	if artifact.State != jobs.ArtifactAvailable {
		writeProblem(w, http.StatusGone, "artifact_gone",
			"Artifact is no longer available", "")
		return
	}
	path := s.artifacts.Path(artifact)
	if err := ensureRegularFile(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeProblem(w, http.StatusGone, "artifact_gone",
				"Artifact is no longer available", "")
		} else {
			writeProblem(w, http.StatusInternalServerError, "artifact_unavailable",
				"Artifact could not be opened", "")
		}
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "artifact_unavailable",
			"Artifact could not be opened", "")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", artifact.MediaType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", artifact.Size))
	w.Header().Set("ETag", `"`+artifact.SHA256+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `inline; filename="`+filename+`"`)
	http.ServeContent(w, r, filename, artifact.CreatedAt, file)
}

// deleteArtifact releases the runner's temporary copy once the API has
// imported it into durable storage.
func (s *server) deleteArtifact(w http.ResponseWriter, r *http.Request) {
	s.deletePhaseArtifact(w, r, jobs.PhasePreview)
}

func (s *server) deleteAnalysis(w http.ResponseWriter, r *http.Request) {
	s.deletePhaseArtifact(w, r, jobs.PhaseAnalysis)
}

func (s *server) deletePhaseArtifact(w http.ResponseWriter, r *http.Request, phase jobs.Phase) {
	artifact, err := s.store.GetArtifact(r.Context(), r.PathValue("id"), phase)
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, http.StatusConflict, "artifact_not_ready",
			"Preview artifact is not ready", "")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "artifact_lookup_failed",
			"Could not read preview artifact", "")
		return
	}
	if err := s.removeArtifact(r.Context(), artifact, jobs.ArtifactDeleted); err != nil {
		writeProblem(w, http.StatusInternalServerError, "artifact_delete_failed",
			"Could not delete preview artifact", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
