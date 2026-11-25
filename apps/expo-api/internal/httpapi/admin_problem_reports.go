package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/staffops"
)

// Problem reports (catalog/problemreports.go): the open queue or the
// resolved ones still kept, closing one with a status, its staff note, and
// banning its reporter from reporting.

// ProblemReportPath names a problem report by its id.
type ProblemReportPath struct {
	ReportID string `path:"reportID" doc:"The problem report's id."`
}

type listProblemReportsInput struct {
	Status string `query:"status" enum:"open,resolved" doc:"open (default, oldest first) or resolved (latest first)."`
}

type problemReportsOutput struct {
	Body staffops.ProblemReportPage
}

// listProblemReports lists ?status=open (default) or resolved.
func (s *adminServer) listProblemReports(ctx context.Context, in *listProblemReportsInput) (*problemReportsOutput, error) {
	page, err := s.ops.ListProblemReports(ctx, staffUser(ctx), in.Status != "resolved")
	if err != nil {
		return nil, err
	}
	return &problemReportsOutput{Body: page}, nil
}

// problemReportClose is the body of closing a problem report. The domain
// checks the status and answers its own code.
type problemReportClose struct {
	Status string `json:"status,omitempty" doc:"fixed, wont_fix, duplicate or spam."`
	Note   string `json:"note,omitempty" doc:"The staff note kept on the report."`
}

type resolveProblemReportInput struct {
	ProblemReportPath
	Body problemReportClose
}

func (s *adminServer) resolveProblemReport(ctx context.Context, in *resolveProblemReportInput) (*struct{}, error) {
	return nil, s.ops.ResolveProblemReport(ctx, staffUser(ctx), in.ReportID, in.Body.Status, in.Body.Note)
}

// problemReportNote is the body of setting a problem report's staff note.
type problemReportNote struct {
	Note string `json:"note,omitempty" doc:"The staff note kept on the report."`
}

type setProblemReportNoteInput struct {
	ProblemReportPath
	Body problemReportNote
}

func (s *adminServer) setProblemReportNote(ctx context.Context, in *setProblemReportNoteInput) (*struct{}, error) {
	return nil, s.ops.SetProblemReportNote(ctx, staffUser(ctx), in.ReportID, in.Body.Note)
}

type banProblemReporterInput struct {
	ProblemReportPath
	Body ReporterBanBody
}

func (s *adminServer) banProblemReporter(ctx context.Context, in *banProblemReporterInput) (*struct{}, error) {
	return nil, s.ops.BanProblemReporter(ctx, staffUser(ctx), in.ReportID, in.Body.ban())
}

// registerProblemReports registers this file's operations.
func (s *adminServer) registerProblemReports(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listProblemReports", Method: http.MethodGet, Path: "/admin/problem-reports",
		Tags:    []string{"Problem reports"},
		Summary: "List problem reports",
	}), s.listProblemReports)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "resolveProblemReport", Method: http.MethodPost, Path: "/admin/problem-reports/{reportID}/resolve",
		Tags:         []string{"Problem reports"},
		Summary:      "Close a problem report",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusBadRequest, http.StatusNotFound},
	}), s.resolveProblemReport)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "setProblemReportNote", Method: http.MethodPost, Path: "/admin/problem-reports/{reportID}/note",
		Tags:         []string{"Problem reports"},
		Summary:      "Set a problem report's staff note",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusBadRequest, http.StatusNotFound},
	}), s.setProblemReportNote)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "banProblemReporter", Method: http.MethodPost, Path: "/admin/problem-reports/{reportID}/ban-reporter",
		Tags:         []string{"Problem reports"},
		Summary:      "Ban a problem report's reporter",
		Description:  "Bans the reporter from reporting and closes that report.",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusBadRequest, http.StatusNotFound},
	}), s.banProblemReporter)
}
