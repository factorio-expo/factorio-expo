package preview

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/outbox"
	"factorio-expo/apps/expo-api/internal/runner"
	"factorio-expo/internal/analysisresult"
	"factorio-expo/internal/runnerapi"
)

const (
	missingArtifactGrace = 30 * time.Second
	// importRetryDelay spaces re-attempts of an import that failed on I/O
	// (download, database); it does not spend retry budget.
	importRetryDelay = 15 * time.Second
)

var errInvalidAnalysis = errors.New("invalid analysis result")

// runnerAPI is the runner surface the handler uses; *runner.Client satisfies it.
type runnerAPI interface {
	CreatePreviewJob(ctx context.Context, blueprint string, phases []runnerapi.Phase, idempotencyKey string) (runnerapi.Job, error)
	GetPreviewJob(ctx context.Context, id string) (runnerapi.Job, error)
	DownloadAnalysis(ctx context.Context, id string) (*http.Response, error)
	DeleteAnalysisArtifact(ctx context.Context, id string) error
	FetchArtifact(ctx context.Context, path string) (*http.Response, error)
	DeletePreviewArtifact(ctx context.Context, id string) error
}

// importQueries is the slice of the job table the handler writes;
// *db.Queries satisfies it. Catalog rows go through Catalog.
type importQueries interface {
	AssignRunnerPhaseJob(ctx context.Context, arg db.AssignRunnerPhaseJobParams) error
	SettleRunnerJob(ctx context.Context, runnerJobID pgtype.Text) error
}

// handler is the outbox.Handler for one blueprint: it dispatches phases that
// have no runner job yet, polls the ones that do, and reports an outcome per
// phase so a failure in one never blocks the other.
type handler struct {
	runner       runnerAPI
	queries      importQueries
	catalog      Catalog
	imports      *importer
	pollInterval time.Duration
}

func (h *handler) Handle(ctx context.Context, item db.ClaimDispatchJobsRow) []outbox.Result {
	var results []outbox.Result
	previewDue := item.PreviewClaimed && phasePending(item.PreviewState)
	analysisDue := item.AnalysisClaimed && phasePending(item.AnalysisState)

	var dispatch []string
	if previewDue && !item.PreviewRunnerJobID.Valid {
		dispatch = append(dispatch, slotPreview)
	}
	if analysisDue && !item.AnalysisRunnerJobID.Valid {
		dispatch = append(dispatch, slotAnalysis)
	}
	if len(dispatch) > 0 {
		results = append(results, h.dispatch(ctx, item, dispatch)...)
	}

	jobs := make(map[string]runnerapi.Job, 2)
	lost := make(map[string]bool, 2)
	fetch := func(id string) (runnerapi.Job, outbox.Outcome, bool) {
		if job, ok := jobs[id]; ok {
			return job, outbox.Outcome{}, true
		}
		if lost[id] {
			return runnerapi.Job{}, outbox.Transient("job_lost", "runner job "+id+" was lost"), false
		}
		job, err := h.runner.GetPreviewJob(ctx, id)
		if err == nil {
			jobs[id] = job
			h.settle(ctx, job)
			return job, outbox.Outcome{}, true
		}
		var runnerError *runner.Error
		if errors.As(err, &runnerError) && runnerError.Status == http.StatusNotFound {
			lost[id] = true
			return runnerapi.Job{}, outbox.Transient("job_lost", "runner job "+id+" was lost"), false
		}
		return runnerapi.Job{}, outbox.WaitFor(importRetryDelay), false
	}

	// Analysis first, as before; each slot reports on its own, so an analysis
	// import error no longer stops the preview import.
	if analysisDue && item.AnalysisRunnerJobID.Valid {
		if job, outcome, ok := fetch(item.AnalysisRunnerJobID.String); ok {
			outcome = h.importAnalysis(ctx, item, job)
			results = append(results, outbox.Result{Slot: slotAnalysis, Outcome: outcome})
		} else {
			results = append(results, outbox.Result{Slot: slotAnalysis, Outcome: outcome})
		}
	}
	if previewDue && item.PreviewRunnerJobID.Valid {
		// An import already started for this job decides first, before the
		// runner is asked: a finished import has released the artifact, so
		// the runner shows it deleted or, with nothing else retained, has
		// dropped the job (404, which would read as lost).
		if outcome, ok := h.imports.Collect(item.PreviewRunnerJobID.String); ok {
			results = append(results, outbox.Result{Slot: slotPreview, Outcome: outcome})
		} else if job, outcome, ok := fetch(item.PreviewRunnerJobID.String); ok {
			outcome = h.importPreview(ctx, item, job)
			results = append(results, outbox.Result{Slot: slotPreview, Outcome: outcome})
		} else {
			results = append(results, outbox.Result{Slot: slotPreview, Outcome: outcome})
		}
	}
	return results
}

// settle records that a runner job finished, so it no longer holds a
// dispatch window slot while its results are imported. A failed write only
// delays the next dispatch until a later poll records it.
func (h *handler) settle(ctx context.Context, job runnerapi.Job) {
	if job.State != runnerapi.JobSucceeded && job.State != runnerapi.JobFailed {
		return
	}
	if err := h.queries.SettleRunnerJob(ctx, pgtype.Text{String: job.ID, Valid: true}); err != nil {
		slog.Warn("record finished runner job", "runner_job_id", job.ID, "error", err)
	}
}

// dispatch submits the phases with no runner job as one runner job and
// records its id. The request carries an idempotency key derived from the
// row, so a retry after a lost response or a failed record asks for the same
// job instead of rendering twice.
func (h *handler) dispatch(
	ctx context.Context, item db.ClaimDispatchJobsRow, slots []string,
) []outbox.Result {
	outcomeFor := func(outcome outbox.Outcome) []outbox.Result {
		results := make([]outbox.Result, 0, len(slots))
		for _, slot := range slots {
			results = append(results, outbox.Result{Slot: slot, Outcome: outcome})
		}
		return results
	}
	phases := make([]runnerapi.Phase, 0, len(slots))
	for _, slot := range slots {
		// Outbox slots are named after the runner phases they dispatch.
		phases = append(phases, runnerapi.Phase(slot))
	}
	job, err := h.runner.CreatePreviewJob(ctx, item.ExchangeString, phases, idempotencyKey(item, slots))
	if err != nil {
		return outcomeFor(dispatchOutcome(err))
	}
	runnerJobID := pgtype.Text{String: job.ID, Valid: true}
	for _, slot := range slots {
		if assignErr := h.queries.AssignRunnerPhaseJob(ctx, db.AssignRunnerPhaseJobParams{
			BlueprintID: item.BlueprintID, Phase: slot, RunnerJobID: runnerJobID,
		}); assignErr != nil {
			// The runner job exists but is not recorded. Look again soon; the
			// same idempotency key makes the repeat request return this job.
			slog.Error("record runner job", "blueprint_id", item.BlueprintID, "phase", slot, "error", assignErr)
			return outcomeFor(outbox.WaitFor(importRetryDelay))
		}
	}
	return outcomeFor(outbox.Waiting())
}

// idempotencyKey identifies one dispatch attempt: the blueprint, the phases
// requested and how many retries each has used. It is stable if the attempt
// is repeated and changes after a counted retry.
func idempotencyKey(item db.ClaimDispatchJobsRow, slots []string) string {
	var key strings.Builder
	key.WriteString(item.BlueprintID)
	for _, slot := range slots {
		retries := item.PreviewRetryCount.Int32
		if slot == slotAnalysis {
			retries = item.AnalysisRetryCount.Int32
		}
		key.WriteString(fmt.Sprintf("/%s.%d", slot, retries))
	}
	return key.String()
}

func (h *handler) importPreview(
	ctx context.Context, item db.ClaimDispatchJobsRow, job runnerapi.Job,
) outbox.Outcome {
	switch {
	case job.Artifact != nil && job.Artifact.State == runnerapi.ArtifactAvailable:
		// Encoding runs in the background; Collect reports its outcome on a
		// later poll.
		return h.imports.Import(ctx, item, job)
	case job.Phases.Preview.Status == runnerapi.PhaseFailed ||
		(job.State == runnerapi.JobFailed && job.Phases.Preview.Status == ""):
		return failureOutcome(previewFailure(job), "render_failed")
	case missingTerminalPreviewArtifact(job, time.Now()):
		return outbox.Transient("artifact_missing",
			"runner completed the preview without an available artifact")
	}
	return outbox.Waiting()
}

func missingTerminalPreviewArtifact(job runnerapi.Job, now time.Time) bool {
	if job.State != runnerapi.JobSucceeded || job.Phases.Preview.Status != runnerapi.PhaseSucceeded {
		return false
	}
	if job.Artifact != nil && job.Artifact.State == runnerapi.ArtifactAvailable {
		return false
	}
	return !job.UpdatedAt.IsZero() && now.Sub(job.UpdatedAt) >= missingArtifactGrace
}

func (h *handler) importAnalysis(
	ctx context.Context, item db.ClaimDispatchJobsRow, job runnerapi.Job,
) outbox.Outcome {
	switch job.Phases.Analysis.Status {
	case runnerapi.PhaseFailed:
		return failureOutcome(job.Phases.Analysis.Failure, "analysis_failed")
	case runnerapi.PhaseSucceeded:
		return h.storeAnalysis(ctx, item)
	}
	return outbox.Waiting()
}

func (h *handler) storeAnalysis(ctx context.Context, item db.ClaimDispatchJobsRow) outbox.Outcome {
	runnerJobID := item.AnalysisRunnerJobID.String
	response, err := h.runner.DownloadAnalysis(ctx, runnerJobID)
	if err != nil {
		var runnerError *runner.Error
		if errors.As(err, &runnerError) && runnerError.Status == http.StatusNotFound {
			return outbox.Transient("job_lost", "analysis artifact is gone: "+err.Error())
		}
		return h.importFailed("download analysis", item, err)
	}
	defer response.Body.Close()
	body, err := analysisresult.ReadAll(response.Body)
	if errors.Is(err, analysisresult.ErrTooLarge) {
		return outbox.Permanent("invalid_result", err.Error())
	} else if err != nil {
		return h.importFailed("read analysis", item, err)
	}
	parsed, bridgeFailure, err := analysisresult.Parse(body, runnerJobID)
	if err == nil && bridgeFailure != nil {
		err = fmt.Errorf("analysis result reports failure %q", bridgeFailure.Code)
	}
	if err != nil {
		return outbox.Permanent("invalid_result", err.Error())
	}
	stored, err := parsed.Stored()
	if err != nil {
		return outbox.Permanent("invalid_result", err.Error())
	}
	if err := h.catalog.StoreProduction(ctx, item.BlueprintID, stored); err != nil {
		return h.importFailed("store production", item, err)
	}
	if err := h.runner.DeleteAnalysisArtifact(ctx, runnerJobID); err != nil {
		return h.importFailed("release analysis artifact", item, err)
	}
	return outbox.Succeeded()
}

// importFailed reports an I/O failure of an import step. The phase is not
// failed (its result is fine); it is looked at again shortly.
func (h *handler) importFailed(step string, item db.ClaimDispatchJobsRow, err error) outbox.Outcome {
	slog.Warn("import analysis", "step", step, "blueprint_id", item.BlueprintID, "error", err)
	return outbox.WaitFor(importRetryDelay)
}

// previewFailure is the preview phase's failure, or the job's when the phase
// has none.
func previewFailure(job runnerapi.Job) *runnerapi.Failure {
	if job.Phases.Preview.Failure != nil {
		return job.Phases.Preview.Failure
	}
	return job.Failure
}

func phasePending(value pgtype.Text) bool {
	return value.Valid && value.String == "pending"
}
