package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// runnerReadinessTimeout bounds the Overview's readiness call; a slow
// runner shows as unreachable instead of holding the page.
const runnerReadinessTimeout = 3 * time.Second

type adminOverviewResponse struct {
	Uploads      uploadCounts       `json:"uploads"`
	Reports      reportCounts       `json:"reports"`
	Jobs         jobCountsByKind    `json:"jobs"`
	RenderWorker renderWorkerStatus `json:"render_worker"`
}

type uploadCounts struct {
	Today int64 `json:"today"`
	Week  int64 `json:"week"`
}

// reportCounts are open (unresolved) reports and how many entries and
// comments they are about.
type reportCounts struct {
	Entries        int64 `json:"entries"`
	EntryReports   int64 `json:"entry_reports"`
	Comments       int64 `json:"comments"`
	CommentReports int64 `json:"comment_reports"`
}

type jobCounts struct {
	Pending int64 `json:"pending"`
	Failed  int64 `json:"failed"`
}

type jobCountsByKind struct {
	Previews jobCounts `json:"previews"`
	Analyses jobCounts `json:"analyses"`
	Search   jobCounts `json:"search"`
}

// renderWorkerStatus is the runner's readiness: status "ready", "not_ready"
// or "unreachable", and each component's status when it answered.
type renderWorkerStatus struct {
	Status     string            `json:"status"`
	Components map[string]string `json:"components"`
}

type adminOverviewOutput struct {
	Body adminOverviewResponse
}

// getOverview answers the Overview page: the upload, report and job counts
// and the render worker's readiness.
func (s *adminServer) getOverview(ctx context.Context, _ *struct{}) (*adminOverviewOutput, error) {
	counts, err := s.queries.AdminOverview(ctx)
	if err != nil {
		slog.Error("admin overview", "error", err)
		return nil, huma.Error500InternalServerError("could not load the overview")
	}
	return &adminOverviewOutput{Body: adminOverviewResponse{
		Uploads: uploadCounts{Today: counts.UploadsToday, Week: counts.UploadsWeek},
		Reports: reportCounts{
			Entries: counts.ReportedEntries, EntryReports: counts.OpenEntryReports,
			Comments: counts.ReportedComments, CommentReports: counts.OpenCommentReports,
		},
		Jobs: jobCountsByKind{
			Previews: jobCounts{Pending: counts.PendingPreviews, Failed: counts.FailedPreviews},
			Analyses: jobCounts{Pending: counts.PendingAnalyses, Failed: counts.FailedAnalyses},
			Search:   jobCounts{Pending: counts.PendingSearchSyncs, Failed: counts.FailedSearchSyncs},
		},
		RenderWorker: s.renderWorkerStatus(ctx),
	}}, nil
}

func (s *adminServer) renderWorkerStatus(ctx context.Context) renderWorkerStatus {
	ctx, cancel := context.WithTimeout(ctx, runnerReadinessTimeout)
	defer cancel()
	readiness, err := s.runner.Readiness(ctx)
	if err != nil {
		slog.Warn("render worker readiness", "error", err)
		return renderWorkerStatus{Status: "unreachable", Components: map[string]string{}}
	}
	components := make(map[string]string, len(readiness.Components))
	for name, component := range readiness.Components {
		components[name] = component.Status
	}
	return renderWorkerStatus{Status: readiness.Status, Components: components}
}

func (s *adminServer) registerAdminOverview(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getOverview", Method: http.MethodGet, Path: "/admin/overview",
		Tags:    []string{"Overview"},
		Summary: "The Overview page's counts and render worker readiness",
	}), s.getOverview)
}
